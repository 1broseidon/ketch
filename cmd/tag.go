package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/scrape"
	"github.com/spf13/cobra"
)

// `tag` is the agent's own corpus: a durable index over pages the cache
// already holds, answering "what do I have under this tag that I can go back
// to?". Every operation is a verb (add/show/list/remove) rather than a bare
// `tag <name>` whose meaning would shift with its arity — that follows the
// grammar the CLI already uses for stored state (crawl status, crawl stop;
// cache clear) and keeps every name usable as a tag.

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Label cached pages so an agent can find them again",
	Long: `Tag pages the cache already holds, then ask what is under a tag.

The index is durable: it outlives the page bodies it points at, so a tag
revisited after cache_ttl still lists everything, marking which pages must be
re-fetched. Pages are tagged as they are fetched with --tag on search, scrape
and crawl, or afterwards with tag add.`,
}

var tagAddCmd = &cobra.Command{
	Use:   "add <tag> <url>...",
	Short: "Tag pages already in the cache",
	Long: `Tag pages already in the cache. Makes no network requests: a URL that
was never fetched cannot be tagged, because there is no title or description to
index without it. Fetch it first, or use --tag on the fetching command.`,
	Args: cobra.MinimumNArgs(2),
	RunE: runTagAdd,
}

var tagShowCmd = &cobra.Command{
	Use:   "show <tag>",
	Short: "List the pages under a tag",
	Long: `List the pages under a tag as an llms.txt-shaped index of titles, URLs
and descriptions. Reads only local state and makes no network requests. Pages
whose bodies have expired are still listed, marked "not cached" — the index
knows the URL, so re-fetching one restores it.`,
	Args: cobra.ExactArgs(1),
	RunE: runTagShow,
}

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List every tag",
	Args:  cobra.NoArgs,
	RunE:  runTagList,
}

var tagRemoveCmd = &cobra.Command{
	Use:   "remove <tag> [url...]",
	Short: "Drop a whole tag, or single pages from it",
	Long: `Drop a whole tag, or just the given pages from it.

Nothing expires the index, so this is how a tag ends. Removing a tag does not
touch the cached pages themselves, and re-tagging rebuilds the entry.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTagRemove,
}

func init() {
	rootCmd.AddCommand(tagCmd)
	tagCmd.AddCommand(tagAddCmd, tagShowCmd, tagListCmd, tagRemoveCmd)
	tagShowCmd.Flags().Bool("minimal", false, "one page per line, tab-separated")
}

// validateTagName keeps a tag renderable and keeps the storage key
// unambiguous — the index packs the tag and the page key into one bbolt key
// separated by NUL.
func validateTagName(name string) error {
	if strings.TrimSpace(name) != name || name == "" {
		return exitErrf(ExitValidation, "tag name must not be empty or padded with spaces")
	}
	if strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) }) {
		return exitErrf(ExitValidation, "tag name must not contain control characters")
	}
	return nil
}

// openTagCache opens the cache read-write along with the scraper whose
// rewrite rules and cookie/user-agent namespaces decide a page's cache key.
// Tagging has to reproduce that key exactly or it would index a page the
// fetch path never stored.
func openTagCache(cmd *cobra.Command) (*cache.Cache, *scrape.Scraper, error) {
	ttl, err := time.ParseDuration(cfg.CacheTTL)
	if err != nil {
		ttl = time.Hour
	}
	c := cache.New(ttl)
	if c == nil {
		return nil, nil, exitErrf(ExitPrecondition, "cannot open cache (may be in use by another process)")
	}
	scraper, err := newScraper(cmd)
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	return c, scraper, nil
}

func runTagAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validateTagName(name); err != nil {
		return err
	}
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	c, scraper, err := openTagCache(cmd)
	if err != nil {
		return err
	}
	defer c.Close()
	defer scraper.Close()

	var tagged, missing []string
	for _, url := range args[1:] {
		key := scraper.CacheKey(scraper.Rewrite(url))
		switch err := c.TagCached(name, key, url); {
		case err == nil:
			tagged = append(tagged, url)
		case errors.Is(err, cache.ErrNotCached):
			missing = append(missing, url)
		default:
			return err
		}
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Tag      string   `json:"tag"`
			Tagged   []string `json:"tagged"`
			NotCache []string `json:"not_cached"`
		}{name, orEmpty(tagged), orEmpty(missing)})
	}

	for _, url := range tagged {
		fmt.Fprintf(os.Stderr, "tagged %s\n", url)
	}
	if len(missing) > 0 {
		for _, url := range missing {
			fmt.Fprintf(os.Stderr, "not in cache, skipped: %s\n", url)
		}
		if len(tagged) == 0 {
			return exitErrf(ExitNotFound, "no pages tagged: fetch them first, or use --tag when you do")
		}
	}
	return nil
}

func runTagShow(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validateTagName(name); err != nil {
		return err
	}
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	minimal, _ := cmd.Flags().GetBool("minimal")

	c, scraper, err := openTagCache(cmd)
	if err != nil {
		return err
	}
	defer c.Close()
	defer scraper.Close()

	pages, err := c.Tagged(name)
	if err != nil {
		return exitErrf(ExitPrecondition, "%v", err)
	}
	cached := 0
	for _, p := range pages {
		if p.Cached {
			cached++
		}
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Tag     string             `json:"tag"`
			Entries int                `json:"entries"`
			Cached  int                `json:"cached"`
			Pages   []cache.TaggedPage `json:"pages"`
		}{name, len(pages), cached, pages})
	}

	if minimal {
		for _, p := range pages {
			fmt.Printf("%s\t%s\t%s\n", p.URL, minimalField(p.Title), minimalField(p.Description))
		}
		return nil
	}

	fmt.Println("---")
	fmt.Printf("tag: %s\n", name)
	fmt.Printf("entries: %d\n", len(pages))
	fmt.Printf("cached: %d\n", cached)
	fmt.Println("---")
	if len(pages) == 0 {
		return nil
	}
	fmt.Println()
	for _, p := range pages {
		title := p.Title
		if title == "" {
			title = p.URL
		}
		fmt.Printf("- [%s](%s)", title, p.URL)
		if p.Description != "" {
			fmt.Printf(": %s", p.Description)
		}
		if !p.Cached {
			fmt.Print(" (not cached)")
		}
		fmt.Println()
	}
	return nil
}

func runTagList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	c, scraper, err := openTagCache(cmd)
	if err != nil {
		return err
	}
	defer c.Close()
	defer scraper.Close()

	tags, err := c.TagList()
	if err != nil {
		return exitErrf(ExitPrecondition, "%v", err)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Tags []cache.TagSummary `json:"tags"`
		}{orEmptyTags(tags)})
	}

	fmt.Println("---")
	fmt.Printf("tags: %d\n", len(tags))
	fmt.Println("---")
	for _, t := range tags {
		fmt.Printf("%s\n  %d %s, %d cached\n", t.Name, t.Entries, plural(t.Entries, "entry", "entries"), t.Cached)
	}
	return nil
}

func runTagRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validateTagName(name); err != nil {
		return err
	}
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	c, scraper, err := openTagCache(cmd)
	if err != nil {
		return err
	}
	defer c.Close()
	defer scraper.Close()

	var removed int
	urls := args[1:]
	if len(urls) == 0 {
		removed, err = c.RemoveTag(name)
	} else {
		keys := make([]string, 0, len(urls))
		for _, url := range urls {
			keys = append(keys, scraper.CacheKey(scraper.Rewrite(url)))
		}
		removed, _, err = c.RemoveTagged(name, keys)
	}
	if err != nil {
		return exitErrf(ExitPrecondition, "%v", err)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Tag     string `json:"tag"`
			Removed int    `json:"removed"`
		}{name, removed})
	}

	if removed == 0 {
		if len(urls) == 0 {
			return exitErrf(ExitNotFound, "no tag named %q", name)
		}
		return exitErrf(ExitNotFound, "none of those pages are under %q", name)
	}
	fmt.Fprintf(os.Stderr, "removed %d %s from %s\n", removed, plural(removed, "entry", "entries"), name)
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// orEmpty keeps JSON arrays as [] rather than null, so consumers can iterate
// without a nil check.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmptyTags(s []cache.TagSummary) []cache.TagSummary {
	if s == nil {
		return []cache.TagSummary{}
	}
	return s
}

// tagWriter records fetched pages under a tag as a fetch command runs.
// A nil *tagWriter is a no-op, so call sites need no branching.
type tagWriter struct {
	tag string
	c   *cache.Cache
}

// newTagWriter prepares --tag for a fetching command, reusing that command's
// page cache handle — bbolt takes an exclusive lock, so a second handle on
// the same file would block rather than work. A nil cache yields a nil
// writer: --tag is mutually exclusive with --no-cache (an entry tagged while
// the body is discarded would be born permanently cold), and a cache that
// could not be opened at all already degrades the fetch to uncached.
func newTagWriter(tag string, pc *cache.Cache) *tagWriter {
	if tag == "" || pc == nil {
		return nil
	}
	return &tagWriter{tag: tag, c: pc}
}

// validateTagFlag is the PreRunE for every command carrying --tag. A bad tag
// name should stop the command before it fetches anything, and checking here
// keeps the run functions free of the branch.
func validateTagFlag(cmd *cobra.Command, _ []string) error {
	tag, _ := cmd.Flags().GetString("tag")
	if tag == "" {
		return nil
	}
	// Tagging a page whose body is deliberately not stored would index an
	// entry that is cold from birth. GetBool is safe on a command without the
	// flag (search has none): it reports false.
	if noCache, _ := cmd.Flags().GetBool("no-cache"); noCache {
		return exitErrf(ExitValidation, "--tag and --no-cache are mutually exclusive")
	}
	return validateTagName(tag)
}

// record indexes one fetched page. Failures are deliberately silent: tagging
// is a side effect of a fetch the caller asked for, and losing an index entry
// must not fail the scrape that produced it.
func (t *tagWriter) record(s *scrape.Scraper, rawURL string, page *scrape.Page) {
	if t == nil || page == nil {
		return
	}
	_ = t.c.TagPage(t.tag, s.CacheKey(s.Rewrite(rawURL)), rawURL, page)
}
