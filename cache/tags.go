package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/scrape"
)

// Tags are durable bookmarks. Membership is keyed by source URL; page-cache
// keys are private lookup information and never determine membership.

// ErrTagsUnsupported is returned when the configured Store cannot hold tags.
// Store has no enumeration by design (Get/Put/Stats/Clear/Close), so tags
// need a capability the interface does not describe; BBoltStore has it.
var ErrTagsUnsupported = errors.New("the configured cache backend does not support tags")

// ErrNotCached is no longer returned: a URL can be tagged whether or not its
// body is cached. Kept so callers compiled against the old behaviour still
// build; it will be removed in a later release.
//
// Deprecated: tagging an uncached URL succeeds and yields an entry with no
// title or description until the page is fetched.
var ErrNotCached = errors.New("page is not in the cache")

// descriptionMax bounds a derived description. Long enough to tell two pages
// on the same site apart, short enough that a tag of fifty pages still reads
// as an index rather than a document.
const descriptionMax = 180

// DefaultTagLimit bounds CLI and MCP tag views. Zero explicitly requests all.
const DefaultTagLimit = 50

// MaxTagNameBytes leaves room for the separator and source hash in a bbolt key.
const MaxTagNameBytes = 32768 - 1 - 16

// ValidateTagName applies the same rules at the package, CLI, and MCP boundaries.
func ValidateTagName(name string) error {
	if name == "" || strings.TrimSpace(name) != name {
		return fmt.Errorf("tag name must not be empty or padded with spaces")
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("tag name must not contain control characters")
	}
	if len(name) > MaxTagNameBytes {
		return fmt.Errorf("tag name must not exceed %d bytes", MaxTagNameBytes)
	}
	return nil
}

// ValidateBookmarkURL applies the same rule at the `tag add` CLI and MCP
// boundaries: unlike scrape (which expands a bare domain like "example.com"
// before anything ever tags it) or a --tag on search/code/docs/crawl (whose
// URLs come from a live result, already absolute), tag add stores exactly
// what it is given as the re-fetch target with no fetch of its own to
// validate it. It must already be an absolute http(s) URL with a host — the
// same rule crawl and search apply when resolving links, which rejects
// javascript:, mailto:, ftp:, and bare words along with the empty string.
func ValidateBookmarkURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("bookmark URL must not be empty")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("bookmark URL must be an absolute http:// or https:// URL, got %q", raw)
	}
	return nil
}

// NewTagIndex creates an independent, lazy index. pages may be nil or unavailable;
// no database handle is opened or retained here. Close is unnecessary.
func NewTagIndex(ttl time.Duration, pages *Cache) *Cache {
	index := defaultTagDB()
	if pages != nil {
		if s, ok := pages.store.(*BBoltStore); ok {
			index = s.tags
		}
	}
	if pages != nil {
		reader := *pages
		reader.ttl = ttl
		pages = &reader
	}
	return &Cache{ttl: ttl, index: index, pages: pages}
}

// WithTagErrorHandler shares a page cache with a per-call backfill diagnostic
// handler. The owner of the original Cache remains responsible for Close.
func (c *Cache) WithTagErrorHandler(handler func(error)) *Cache {
	if c == nil {
		return nil
	}
	copy := *c
	copy.onTagError = handler
	return &copy
}

// TagView is a bounded index. Entries and Cached count the entire tag; Shown
// counts Pages. CacheStatus is unavailable when warmth could not be checked.
type TagView struct {
	Tag         string       `json:"tag"`
	Entries     int          `json:"entries"`
	Shown       int          `json:"shown"`
	Cached      int          `json:"cached"`
	CacheStatus string       `json:"cache_status"`
	Pages       []TaggedPage `json:"pages"`
}

// TagEntry is one page recorded under a tag. It carries everything needed to
// render the index and re-fetch the page, so it survives the expiry of the
// body it describes.
type TagEntry struct {
	// URL is the URL the operator or agent asked for — what gets displayed
	// and what a re-fetch is issued against.
	URL string `json:"u"`
	// Key is the composite page-cache key (rewritten URL plus any cookie and
	// user-agent namespace), stored so warmth can be checked later. It is not
	// the URL: a rewritten URL or a configured UA makes them differ.
	Key         string `json:"k"`
	Title       string `json:"t,omitempty"`
	Description string `json:"d,omitempty"`
	TaggedAt    int64  `json:"a"`
}

// TaggedPage is the public view of one indexed page: what `tag show --json`
// emits and what MCP returns. It is deliberately not TagEntry — the storage
// struct uses short keys to stay small on disk and carries the composite
// cache key, which encodes cookie and user-agent fingerprints and is no part
// of the contract. Cold entries are still returned: the index knows the URL,
// so the agent can re-fetch and re-warm.
type TaggedPage struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	TaggedAt    string `json:"tagged_at"`
	Cached      bool   `json:"cached"`
}

// TagSummary counts what is under a tag.
type TagSummary struct {
	Name        string `json:"name"`
	Entries     int    `json:"entries"`
	Cached      int    `json:"cached"`
	CacheStatus string `json:"cache_status"`
}

// tagStore is the enumeration capability tags need on top of Store.
// Implemented by BBoltStore; a backend without it simply has no tag support.
type tagStore interface {
	PutTagEntry(tag, pageKey string, value []byte) error
	TagEntries(tag string) ([][]byte, error)
	TagNames() ([]string, error)
	DeleteTag(tag string) (int, error)
	DeleteTagEntry(tag, pageKey string) (bool, error)
}

func (c *Cache) tags() (tagStore, error) {
	if c == nil {
		return nil, ErrTagsUnsupported
	}
	if c.index != nil {
		return c.index, nil
	}
	ts, ok := c.store.(tagStore)
	if !ok {
		return nil, ErrTagsUnsupported
	}
	return ts, nil
}

// TagPage records an already-fetched page under a tag. key is the composite
// cache key the page was stored under (scrape.Scraper.CacheKey of the
// rewritten URL); url is the URL to show and re-fetch.
func (c *Cache) TagPage(tag, key, url string, page *scrape.Page) error {
	entry := TagEntry{
		URL:         url,
		Key:         key,
		Title:       oneLine(page.Title, descriptionMax),
		Description: Describe(page.Markdown),
		TaggedAt:    time.Now().Unix(),
	}
	return c.tagEntry(tag, entry)
}

// tagEntry writes one index entry, replacing whatever was there for that page.
func (c *Cache) tagEntry(tag string, entry TagEntry) error {
	if err := ValidateTagName(tag); err != nil {
		return err
	}
	ts, err := c.tags()
	if err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return ts.PutTagEntry(tag, cacheKey(entry.URL), data)
}

// TagURL records a URL under a tag without any network access, using the
// cached page for title and description when there is one. It reports whether
// a body was available.
//
// A URL with no cached body is still indexed. The index is a durable record of
// what matters to a piece of work, not a view over the cache: naming a URL is
// a deliberate act, and refusing it because the body happens to be absent
// would make organising URLs depend on when they were last fetched. Such an
// entry lists as uncached and fills in its title and description the first
// time the page is fetched — see Backfill.
func (c *Cache) TagURL(tag, key, url string) (cached bool, err error) {
	var page *scrape.Page
	if c != nil {
		page, _ = c.Get(key)
		if page == nil && c.pages != nil {
			page, _ = c.pages.Get(key)
		}
	}
	if page == nil {
		return false, c.tagEntry(tag, TagEntry{URL: url, Key: key, TaggedAt: time.Now().Unix()})
	}
	return true, c.TagPage(tag, key, url, page)
}

// Tagged returns the complete index for package callers. CLI and MCP use
// ShowTag to apply their default limit. Reading never writes metadata.
func (c *Cache) Tagged(tag string) ([]TaggedPage, error) {
	view, err := c.ShowTag(tag, 0)
	return view.Pages, err
}

func readTagEntries(ts tagStore, tag string) ([]TagEntry, error) {
	values, err := ts.TagEntries(tag)
	if err != nil {
		return nil, err
	}
	entries := make([]TagEntry, 0, len(values))
	for _, value := range values {
		var entry TagEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return nil, fmt.Errorf("invalid bookmark in tag %q: %w", tag, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ShowTag lists newest bookmarks, breaking timestamp ties by source URL.
func (c *Cache) ShowTag(tag string, limit int) (TagView, error) {
	out := TagView{Tag: tag, Pages: []TaggedPage{}, CacheStatus: "available"}
	if err := ValidateTagName(tag); err != nil {
		return out, err
	}
	if limit < 0 {
		return out, fmt.Errorf("limit must be zero or greater")
	}
	ts, err := c.tags()
	if err != nil {
		return out, err
	}
	entries, err := readTagEntries(ts, tag)
	if err != nil {
		return out, err
	}
	slices.SortFunc(entries, func(a, b TagEntry) int {
		if a.TaggedAt > b.TaggedAt {
			return -1
		}
		if a.TaggedAt < b.TaggedAt {
			return 1
		}
		return strings.Compare(a.URL, b.URL)
	})
	out.Entries = len(entries)
	warm, status := c.tagWarmth(entries)
	out.CacheStatus = status
	for _, entry := range entries {
		if warm[entry.Key] {
			out.Cached++
		}
		if limit > 0 && len(out.Pages) >= limit {
			continue
		}
		out.Pages = append(out.Pages, TaggedPage{
			URL: entry.URL, Title: entry.Title, Description: entry.Description,
			TaggedAt: time.Unix(entry.TaggedAt, 0).UTC().Format(time.RFC3339), Cached: warm[entry.Key],
		})
	}
	out.Shown = len(out.Pages)
	return out, nil
}

// TagList counts bookmarks without sorting or decoding cached document bodies.
func (c *Cache) TagList() ([]TagSummary, error) {
	ts, err := c.tags()
	if err != nil {
		return nil, err
	}
	names, err := ts.TagNames()
	if err != nil {
		return nil, err
	}
	all := []TagEntry{}
	counts := make([]int, 0, len(names))
	for _, name := range names {
		entries, err := readTagEntries(ts, name)
		if err != nil {
			return nil, err
		}
		counts = append(counts, len(entries))
		all = append(all, entries...)
	}
	warm, status := c.tagWarmth(all)
	out := make([]TagSummary, 0, len(names))
	offset := 0
	for i, name := range names {
		summary := TagSummary{Name: name, Entries: counts[i], CacheStatus: status}
		for _, entry := range all[offset : offset+counts[i]] {
			if warm[entry.Key] {
				summary.Cached++
			}
		}
		offset += counts[i]
		out = append(out, summary)
	}
	return out, nil
}

// TagCounts reports how many tags and total entries the index holds, without
// decoding entries or checking page-cache warmth — cheaper than TagList and
// independent of the page cache's availability. Used by `ketch cache` and
// `ketch doctor`, which want a size, not a browsable view.
func (c *Cache) TagCounts() (tags int, entries int, err error) {
	ts, err := c.tags()
	if err != nil {
		return 0, 0, err
	}
	names, err := ts.TagNames()
	if err != nil {
		return 0, 0, err
	}
	for _, name := range names {
		values, err := ts.TagEntries(name)
		if err != nil {
			return 0, 0, err
		}
		entries += len(values)
	}
	return len(names), entries, nil
}

func (c *Cache) tagWarmth(entries []TagEntry) (map[string]bool, string) {
	if len(entries) == 0 {
		return nil, "available"
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	pc, closeReader, status := c.tagPageReader()
	defer closeReader()
	if pc == nil {
		return nil, status
	}
	if s, ok := pc.store.(*BBoltStore); ok {
		warm, err := s.cachedKeys(keys, c.ttl)
		if err != nil {
			return nil, "unavailable"
		}
		return warm, "available"
	}
	warm := map[string]bool{}
	for _, key := range keys {
		page, _ := pc.Get(key)
		warm[key] = page != nil
	}
	return warm, "available"
}

// Open at most one page-cache reader per index operation, after the tag file
// has been closed. A read-only bbolt open still contends with writers.
func (c *Cache) tagPageReader() (*Cache, func(), string) {
	noop := func() {}
	if c.store != nil {
		return c, noop, "available"
	}
	if c.pages != nil && c.pages.store != nil {
		return c.pages, noop, "available"
	}
	path, err := DBPath()
	if err != nil {
		return nil, noop, "unavailable"
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, noop, "available"
	}
	store, err := NewBBoltStoreReadOnly(path)
	if err != nil {
		return nil, noop, "unavailable"
	}
	pc := NewWithStore(store, c.ttl)
	return pc, pc.Close, "available"
}

// Backfill fills missing metadata for existing bookmarks of url. It does not
// create memberships, and it updates the cache lookup after fetch settings change.
func (c *Cache) Backfill(key, url string, page *scrape.Page) error {
	if c == nil || page == nil {
		return nil
	}
	ts, err := c.tags()
	if errors.Is(err, ErrTagsUnsupported) {
		return nil
	}
	if err != nil {
		return err
	}
	if store, ok := ts.(interface {
		Backfill(string, string, string, string) error
	}); ok {
		return store.Backfill(url, key, oneLine(page.Title, descriptionMax), Describe(page.Markdown))
	}
	return nil
}

// RemoveTag drops a whole tag and reports how many entries went with it.
// Nothing expires the index, so this is how a tag ends.
func (c *Cache) RemoveTag(tag string) (int, error) {
	if err := ValidateTagName(tag); err != nil {
		return 0, err
	}
	ts, err := c.tags()
	if err != nil {
		return 0, err
	}
	return ts.DeleteTag(tag)
}

// RemoveTagged drops single entries from a tag by their original source URLs.
// Returns the number actually removed and the given URLs that were not under
// the tag, in the order given, so the caller can report exactly which ones.
func (c *Cache) RemoveTagged(tag string, keys []string) (removed int, missing []string, err error) {
	if err := ValidateTagName(tag); err != nil {
		return 0, nil, err
	}
	ts, err := c.tags()
	if err != nil {
		return 0, nil, err
	}
	for _, k := range keys {
		ok, err := ts.DeleteTagEntry(tag, cacheKey(k))
		if err != nil {
			return removed, missing, err
		}
		if ok {
			removed++
		} else {
			missing = append(missing, k)
		}
	}
	return removed, missing, nil
}

// TagResult records something a surface returned that never went through the
// page cache — a code hit, a docs chunk, or a search result nobody scraped.
// The caller supplies title and description because there is no page to derive
// them from; the snippet a code or docs result already carries is exactly that
// metadata.
//
// The entry lists as uncached until the URL is actually fetched, which is
// honest rather than a defect: the index records where something is and what
// it was, and a scrape of that URL fills the body in.
func (c *Cache) TagResult(tag, key, url, title, description string) error {
	return c.tagEntry(tag, TagEntry{
		URL:         url,
		Key:         key,
		Title:       oneLine(title, descriptionMax),
		Description: oneLine(description, descriptionMax),
		TaggedAt:    time.Now().Unix(),
	})
}

// oneLine collapses arbitrary text — prose, or the indented lines of a code
// snippet — into a single bounded line fit for an index.
func oneLine(s string, max int) string {
	return truncate(strings.Join(strings.Fields(s), " "), max)
}

// Describe derives a one-line description from page markdown: the first real
// paragraph, with markdown syntax removed and headings skipped. Computed once
// at tag time and stored, because the body it came from will expire while the
// index entry does not.
func Describe(markdown string) string {
	stripped := extract.StripMarkdown(markdown)
	for _, block := range strings.Split(stripped, "\n\n") {
		line := strings.Join(strings.Fields(block), " ")
		// Skip fragments that carry no prose: nav crumbs, bullets, stray
		// punctuation left behind by stripping.
		if len([]rune(line)) < 24 || !strings.ContainsFunc(line, unicode.IsLetter) {
			continue
		}
		return truncate(line, descriptionMax)
	}
	return ""
}

// truncate cuts at the last word boundary inside the limit so a description
// never ends mid-word.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-") + "…"
}
