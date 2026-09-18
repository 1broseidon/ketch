package cache

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/scrape"
)

// Tags are a durable index over the pages the cache already holds: a label,
// and enough metadata per page to answer "what do I have under this tag?"
// without the page body being present.
//
// The index deliberately outlives the bodies it points at. A body is tens of
// kilobytes and genuinely goes stale, so it expires under cache_ttl (72h by
// default); an index entry is a couple hundred bytes of URL, title and
// description, and a URL does not rot the way a body does. Tying the two
// together would empty a tag by the Tuesday after a Friday of research —
// exactly when the work resumes and the tag is asked what it found. So an
// entry is self-sufficient, and an expired page is reported as a cold entry
// rather than dropped. See design/adr/0004-tagged-cache-corpus.md.

// ErrTagsUnsupported is returned when the configured Store cannot hold tags.
// Store has no enumeration by design (Get/Put/Stats/Clear/Close), so tags
// need a capability the interface does not describe; BBoltStore has it.
var ErrTagsUnsupported = errors.New("the configured cache backend does not support tags")

// ErrNotCached reports that a page was never fetched, so there is nothing to
// tag. Only fetched pages are tagged — a map of pages nobody read is a map of
// guesses.
var ErrNotCached = errors.New("page is not in the cache")

// descriptionMax bounds a derived description. Long enough to tell two pages
// on the same site apart, short enough that a tag of fifty pages still reads
// as an index rather than a document.
const descriptionMax = 180

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

	// taggedAt is the raw timestamp, kept for ordering.
	taggedAt int64
}

// TagSummary counts what is under a tag.
type TagSummary struct {
	Name    string `json:"name"`
	Entries int    `json:"entries"`
	Cached  int    `json:"cached"`
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
	ts, err := c.tags()
	if err != nil {
		return err
	}
	entry := TagEntry{
		URL:         url,
		Key:         key,
		Title:       strings.TrimSpace(page.Title),
		Description: Describe(page.Markdown),
		TaggedAt:    time.Now().Unix(),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return ts.PutTagEntry(tag, cacheKey(key), data)
}

// TagCached records a page that is already in the cache, without any network
// access. Returns ErrNotCached when the page was never fetched or its body
// has expired — there is no title or description to index without it.
func (c *Cache) TagCached(tag, key, url string) error {
	page, _ := c.Get(key)
	if page == nil {
		return ErrNotCached
	}
	return c.TagPage(tag, key, url, page)
}

// Tagged returns everything under a tag, newest first, each marked with
// whether its body is still cached. An unknown tag yields an empty slice,
// not an error: asking what is under a tag nobody has used is a fair
// question with a short answer.
func (c *Cache) Tagged(tag string) ([]TaggedPage, error) {
	ts, err := c.tags()
	if err != nil {
		return nil, err
	}
	values, err := ts.TagEntries(tag)
	if err != nil {
		return nil, err
	}
	pages := make([]TaggedPage, 0, len(values))
	for _, v := range values {
		var e TagEntry
		if err := json.Unmarshal(v, &e); err != nil {
			// A single unreadable entry must not sink the whole index.
			continue
		}
		page, _ := c.Get(e.Key)
		pages = append(pages, TaggedPage{
			URL:         e.URL,
			Title:       e.Title,
			Description: e.Description,
			TaggedAt:    time.Unix(e.TaggedAt, 0).UTC().Format(time.RFC3339),
			Cached:      page != nil,
			taggedAt:    e.TaggedAt,
		})
	}
	sortByTaggedAtDesc(pages)
	return pages, nil
}

// TagList summarises every tag: how many pages it holds and how many of
// those are still cached.
func (c *Cache) TagList() ([]TagSummary, error) {
	ts, err := c.tags()
	if err != nil {
		return nil, err
	}
	names, err := ts.TagNames()
	if err != nil {
		return nil, err
	}
	out := make([]TagSummary, 0, len(names))
	for _, name := range names {
		pages, err := c.Tagged(name)
		if err != nil {
			return nil, err
		}
		s := TagSummary{Name: name, Entries: len(pages)}
		for _, p := range pages {
			if p.Cached {
				s.Cached++
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// RemoveTag drops a whole tag and reports how many entries went with it.
// Nothing expires the index, so this is how a tag ends.
func (c *Cache) RemoveTag(tag string) (int, error) {
	ts, err := c.tags()
	if err != nil {
		return 0, err
	}
	return ts.DeleteTag(tag)
}

// RemoveTagged drops single entries from a tag, keyed by composite cache key.
// Returns the number actually removed; keys that were not under the tag are
// counted in missing so the caller can report them.
func (c *Cache) RemoveTagged(tag string, keys []string) (removed int, missing int, err error) {
	ts, err := c.tags()
	if err != nil {
		return 0, 0, err
	}
	for _, k := range keys {
		ok, err := ts.DeleteTagEntry(tag, cacheKey(k))
		if err != nil {
			return removed, missing, err
		}
		if ok {
			removed++
		} else {
			missing++
		}
	}
	return removed, missing, nil
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

func sortByTaggedAtDesc(pages []TaggedPage) {
	// Insertion sort: a tag holds tens of entries, and this keeps the package
	// free of a sort import for one call site.
	for i := 1; i < len(pages); i++ {
		for j := i; j > 0 && pages[j].taggedAt > pages[j-1].taggedAt; j-- {
			pages[j], pages[j-1] = pages[j-1], pages[j]
		}
	}
}
