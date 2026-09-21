package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/scrape"
	"github.com/spf13/cobra"
)

// Anvil · target: ketch tag · kind: cli · scope: tool
// caller profile: agent,script,human-operator · surface pattern: Verb-Surface · risk class: R2
// contracts: conventions.yaml · obligations: bounded-output,errors,json,portable-storage

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Bookmark research sources for later sessions",
	Long: `Save sources under project or topic labels, then revisit them later.

Bookmarks keep their URL, title and description after page-cache expiry or clear.
Use --tag on search, code, docs, scrape or crawl, or tag add for known URLs.
The independent tags.db uses the native configuration directory; KETCH_TAGS_PATH
overrides its filename for labs and portable setups.

Example: ketch tag show my-project --limit 20`,
}

var tagAddCmd = &cobra.Command{
	Use:   "add <tag> <url>...",
	Short: "Bookmark URLs, whether cached or not",
	Long: `Bookmark one or more URLs without fetching them. Existing metadata is
preserved when a page is cold. Missing metadata fills in when the URL is fetched.

Example: ketch tag add my-project https://example.com/docs`,
	Args: exitArgs(cobra.MinimumNArgs(2)),
	RunE: runTagAdd,
}

var tagShowCmd = &cobra.Command{
	Use:   "show <tag>",
	Short: "List the pages under a tag",
	Long: `List the pages under a tag as an llms.txt-shaped index of titles, URLs
and descriptions. Reads only local state and makes no network requests. Pages
whose bodies have expired are still listed, marked "not cached" — the index
knows the URL, so re-fetching one restores it. The newest 50 entries are shown
by default; --limit 0 shows all.

Example: ketch tag show my-project --json | jq .pages`,
	Args: exitArgs(cobra.ExactArgs(1)),
	RunE: runTagShow,
}

var tagListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List every tag",
	Example: "  ketch tag list --json",
	Args:    cobra.NoArgs,
	RunE:    runTagList,
}

var tagRemoveCmd = &cobra.Command{
	Use:   "remove <tag> [url...]",
	Short: "Drop a whole tag, or single pages from it",
	Long: `Drop a whole tag, or just the given pages from it.

Nothing expires the index, so this is how a tag ends. Removing a tag does not
touch the cached pages themselves. URLs identify bookmarks regardless of
cookies or User-Agent settings.

Example: ketch tag remove my-project https://example.com/docs`,
	Args: exitArgs(cobra.MinimumNArgs(1)),
	RunE: runTagRemove,
}

func init() {
	rootCmd.AddCommand(tagCmd)
	tagCmd.AddCommand(tagAddCmd, tagShowCmd, tagListCmd, tagRemoveCmd)
	tagShowCmd.Flags().Bool("minimal", false, "one page per line, tab-separated")
	tagShowCmd.Flags().Int("limit", cache.DefaultTagLimit, "maximum entries, newest first (0 = all)")
}

func validateTagName(name string) error {
	if err := cache.ValidateTagName(name); err != nil {
		return exitErrf(ExitValidation, "%v", err)
	}
	return nil
}

func tagTTL() time.Duration {
	ttl, err := time.ParseDuration(cfg.CacheTTL)
	if err != nil {
		return time.Hour
	}
	return ttl
}

func runTagAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validateTagName(name); err != nil {
		return err
	}
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	pc := cache.NewReadOnly()
	defer pc.Close()
	index := cache.NewTagIndex(tagTTL(), pc)
	scraper, err := newScraper(cmd)
	if err != nil {
		tagDiagnostic(asJSON, "cache_unavailable", name, "", "cache lookup unavailable; bookmarks will still be saved")
	}
	if scraper != nil {
		defer scraper.Close()
	}
	var tagged, uncached []string
	for _, url := range args[1:] {
		cached, err := index.TagURL(name, tagCacheKey(scraper, url), url)
		if err != nil {
			return exitErrf(ExitPrecondition, "save bookmark: %v", err)
		}
		tagged = append(tagged, url)
		if !cached {
			uncached = append(uncached, url)
		}
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Tag       string   `json:"tag"`
			Tagged    []string `json:"tagged"`
			NotCached []string `json:"not_cached"`
		}{name, orEmpty(tagged), orEmpty(uncached)})
	}
	for _, url := range tagged {
		fmt.Fprintf(os.Stderr, "tagged %s\n", url)
	}
	return nil
}

func runTagShow(cmd *cobra.Command, args []string) error {
	if err := validateTagName(args[0]); err != nil {
		return err
	}
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 0 {
		return exitErrf(ExitValidation, "--limit must be zero or greater")
	}
	view, err := cache.NewTagIndex(tagTTL(), nil).ShowTag(args[0], limit)
	if err != nil {
		return exitErrf(ExitPrecondition, "read bookmarks: %v", err)
	}
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	minimal, _ := cmd.Flags().GetBool("minimal")
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(view)
	}
	if view.CacheStatus == "unavailable" {
		fmt.Fprintln(os.Stderr, "warn: page cache unavailable; cached flags could not be checked")
	}
	if minimal {
		if view.Shown < view.Entries {
			fmt.Fprintf(os.Stderr, "showing %d of %d; use --limit 0 for all\n", view.Shown, view.Entries)
		}
		for _, p := range view.Pages {
			fmt.Printf("%s\t%s\t%s\n", minimalField(p.URL), minimalField(p.Title), minimalField(p.Description))
		}
		return nil
	}
	fmt.Printf("---\ntag: %s\nentries: %d\nshown: %d\ncached: %d\ncache_status: %s\n---\n", view.Tag, view.Entries, view.Shown, view.Cached, view.CacheStatus)
	if view.Shown < view.Entries {
		fmt.Printf("showing %d of %d; use --limit 0 for all\n", view.Shown, view.Entries)
	}
	for _, p := range view.Pages {
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
	tags, err := cache.NewTagIndex(tagTTL(), nil).TagList()
	if err != nil {
		return exitErrf(ExitPrecondition, "list bookmarks: %v", err)
	}
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Tags []cache.TagSummary `json:"tags"`
		}{orEmptyTags(tags)})
	}
	fmt.Printf("---\ntags: %d\n---\n", len(tags))
	for _, tag := range tags {
		fmt.Printf("%s\n  %d %s, %d cached\n", tag.Name, tag.Entries, plural(tag.Entries, "entry", "entries"), tag.Cached)
	}
	for _, tag := range tags {
		if tag.CacheStatus == "unavailable" {
			fmt.Fprintln(os.Stderr, "warn: page cache unavailable; cached counts could not be checked")
			break
		}
	}
	return nil
}

func runTagRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validateTagName(name); err != nil {
		return err
	}
	index := cache.NewTagIndex(tagTTL(), nil)
	var removed int
	var err error
	if len(args) == 1 {
		removed, err = index.RemoveTag(name)
	} else {
		removed, _, err = index.RemoveTagged(name, args[1:])
	}
	if err != nil {
		return exitErrf(ExitPrecondition, "remove bookmarks: %v", err)
	}
	if removed == 0 {
		return exitErrf(ExitNotFound, "nothing removed: %q holds no such entries", name)
	}
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Tag     string `json:"tag"`
			Removed int    `json:"removed"`
		}{name, removed})
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

// tagWriter uses only brief index transactions, even when the page cache is locked.
type tagWriter struct {
	tag string
	c   *cache.Cache
}

func newTagWriter(tag string, pc *cache.Cache) *tagWriter {
	if tag == "" {
		return nil
	}
	return &tagWriter{tag: tag, c: cache.NewTagIndex(tagTTL(), pc)}
}
func (t *tagWriter) Close() {} // no process-lifetime index handle

func validateTagFlag(cmd *cobra.Command, _ []string) error {
	tag, _ := cmd.Flags().GetString("tag")
	if tag == "" && !cmd.Flags().Changed("tag") {
		return nil
	}
	return validateTagName(tag)
}

func tagCacheKey(s *scrape.Scraper, url string) string {
	if s == nil {
		return url
	}
	return s.CacheKey(s.Rewrite(url))
}

func (t *tagWriter) recordResult(s *scrape.Scraper, url, title, description string) {
	if t == nil || url == "" {
		return
	}
	if err := t.c.TagResult(t.tag, tagCacheKey(s, url), url, title, description); err != nil {
		warnTagWrite(t.tag, url, err)
	}
}

func (t *tagWriter) record(s *scrape.Scraper, url string, page *scrape.Page) {
	if page == nil {
		return
	}
	key := tagCacheKey(s, url)
	if t == nil {
		if err := cache.NewTagIndex(tagTTL(), nil).Backfill(key, url, page); err != nil {
			warnTagWrite("", url, err)
		}
		return
	}
	if err := t.c.TagPage(t.tag, key, url, page); err != nil {
		warnTagWrite(t.tag, url, err)
	}
}

type taggableResult struct{ URL, Title, Description string }

func tagDiagnostic(asJSON bool, code, tag, url, message string) {
	if asJSON {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"warning": map[string]string{"code": code, "tag": tag, "url": url, "message": message}})
		return
	}
	fmt.Fprintf(os.Stderr, "warn: tag %q: %s\n", tag, message)
}

func warnTagWrite(tag, url string, err error) {
	asJSON, _ := rootCmd.PersistentFlags().GetBool("json")
	tagDiagnostic(asJSON, "tag_write_failed", tag, url, fmt.Sprintf("bookmark update failed: %v", err))
}

func countResults(n int) string { return fmt.Sprintf("%d %s", n, plural(n, "result", "results")) }

func tagResults(cmd *cobra.Command, results []taggableResult) {
	tag, _ := cmd.Flags().GetString("tag")
	if tag == "" || len(results) == 0 {
		return
	}
	scraper, err := newScraper(cmd)
	if err != nil {
		scraper = nil
	} // bookmark identity does not require valid fetch configuration
	if scraper != nil {
		defer scraper.Close()
	}
	tw := newTagWriter(tag, nil)
	for _, r := range results {
		tw.recordResult(scraper, r.URL, r.Title, r.Description)
	}
}
