package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/scrape"
	"github.com/1broseidon/ketch/urlrewrite"
	"github.com/spf13/cobra"
)

var scrapeCmd = &cobra.Command{
	Use:   "scrape [url...] | [file] | [json-array]",
	Short: "Scrape URLs and extract clean markdown",
	Long: `Fetch one or more URLs, extract the main content, and convert to clean markdown.

Input is detected automatically:
  Multiple args:  ketch scrape url1 url2 url3
  JSON array:     ketch scrape '["url1","url2"]'
  File:           ketch scrape urls.txt
  Stdin pipe:     echo "url1\nurl2" | ketch scrape
  Single URL:     ketch scrape url`,
	RunE: runScrape,
}

func init() {
	rootCmd.AddCommand(scrapeCmd)
	scrapeCmd.Flags().Bool("raw", false, "output raw HTML instead of markdown")
	scrapeCmd.Flags().Bool("no-cache", false, "bypass the page cache")
	scrapeCmd.Flags().Int("max-chars", 0, "truncate markdown output to N chars (0 = disabled)")
	scrapeCmd.Flags().Bool("trim", false, "strip markdown formatting, keep content text only")
	scrapeCmd.Flags().String("select", "", "CSS selector to extract specific elements (skips readability)")
	scrapeCmd.Flags().Bool("no-llms-txt", false, "disable automatic /llms.txt detection for bare domains")
	scrapeCmd.Flags().Int("concurrency", 5, "max concurrent requests for multi-URL scraping")
	scrapeCmd.Flags().Bool("force-browser", false, "always render via the configured browser, skipping JS-shell auto-detection")
}

func runScrape(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	maxChars, _ := cmd.Flags().GetInt("max-chars")
	trim, _ := cmd.Flags().GetBool("trim")
	selector, _ := cmd.Flags().GetString("select")
	noLLMSTxt, _ := cmd.Flags().GetBool("no-llms-txt")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	raw, _ := cmd.Flags().GetBool("raw")
	forceBrowser, _ := cmd.Flags().GetBool("force-browser")

	// --raw is an output mode over the canonical fetch result, so it is
	// incompatible with the extraction-oriented flags.
	if raw {
		if selector != "" {
			return exitErrf(ExitValidation, "--raw cannot be combined with --select (select is extraction-oriented)")
		}
		if trim {
			return exitErrf(ExitValidation, "--raw cannot be combined with --trim (trim is markdown-specific)")
		}
	}

	urls, err := resolveURLs(args)
	if err != nil {
		return err
	}

	scraper, err := newScraper()
	if err != nil {
		return err
	}
	defer scraper.Close()

	// --force-browser is a hard opt-in: never silently fall back to HTTP, or it
	// would reproduce the JS-shell confusion the flag exists to avoid.
	if forceBrowser && !scraper.HasBrowser() {
		return exitErrf(ExitPrecondition, "--force-browser requires a configured browser (set with: ketch config set browser chrome)")
	}

	pc := newPageCache(noCache)
	defer pc.Close()

	ctx := cmd.Context()
	if len(urls) == 1 {
		return scrapeSingle(ctx, scraper, pc, urls[0], asJSON, raw, trim, maxChars, selector, noLLMSTxt, forceBrowser)
	}
	return scrapeMultiple(ctx, scraper, pc, urls, asJSON, raw, trim, maxChars, selector, noLLMSTxt, concurrency, forceBrowser)
}

// resolveURLs detects the input mode and returns a list of URLs.
// Explicit args always take priority over stdin so that
// `ketch scrape url < file` uses the URL, not the file.
func resolveURLs(args []string) ([]string, error) {
	if len(args) > 1 {
		return args, nil
	}

	if len(args) == 1 {
		arg := args[0]
		if strings.HasPrefix(strings.TrimSpace(arg), "[") {
			var urls []string
			if err := json.Unmarshal([]byte(arg), &urls); err != nil {
				return nil, exitErrf(ExitValidation, "failed to parse JSON array: %w", err)
			}
			if len(urls) == 0 {
				return nil, exitErrf(ExitValidation, "JSON array is empty")
			}
			return urls, nil
		}
		if _, err := os.Stat(arg); err == nil {
			urls, err := readLinesFromFile(arg)
			if err != nil {
				return nil, exitErrf(ExitValidation, "%w", err)
			}
			if len(urls) == 0 {
				return nil, exitErrf(ExitValidation, "file %q contains no URLs", arg)
			}
			return urls, nil
		}
		return []string{arg}, nil
	}

	// No args — fall back to stdin if it's a pipe.
	if stdinIsPipe() {
		urls := readLines(os.Stdin)
		if len(urls) > 0 {
			return urls, nil
		}
	}

	return nil, exitErrf(ExitValidation, "provide a URL, file path, JSON array, or pipe URLs via stdin")
}

// stdinIsPipe returns true when stdin is a pipe (not a terminal).
func stdinIsPipe() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// readLines reads all non-empty, non-comment lines from r.
func readLines(r io.Reader) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// readLinesFromFile opens a file and returns non-empty, non-comment lines.
func readLinesFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return readLines(f), nil
}

// newScraper builds a Scraper from cfg: compiled URL rewriter + optional
// browser binary. Returned scraper must be Closed by the caller.
func newScraper() (*scrape.Scraper, error) {
	rw, err := urlrewrite.NewRewriter(cfg.URLRewrites)
	if err != nil {
		return nil, exitErrf(ExitPrecondition, "invalid url_rewrites: %w", err)
	}
	return scrape.NewWithRewriter(cfg.Browser, rw), nil
}

// newPageCache creates a cache from config, or nil if disabled.
func newPageCache(noCache bool) *cache.Cache {
	if noCache {
		return nil
	}
	ttl, err := time.ParseDuration(cfg.CacheTTL)
	if err != nil {
		ttl = time.Hour
	}
	return cache.New(ttl)
}

// cachedScrape checks the cache first, falls back to fetch+extract.
// Hits are bypassed when the entry was extracted from an unrendered JS shell
// and a browser is now available to do better, or when the entry predates
// source tracking (a one-time migration once a browser is configured).
// The cache is keyed by the rewritten URL so original and rewritten URLs
// share one cache entry.
func cachedScrape(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, url string) (*scrape.Page, error) {
	key := s.Rewrite(url)
	if page, source := pc.Get(key); page != nil && !scrape.CacheStaleForBrowser(source, s.HasBrowser()) {
		return page, nil
	}

	page, source, err := s.Scrape(ctx, url)
	if err != nil {
		return nil, err
	}

	pc.Put(key, page, source)
	return page, nil
}

// cachedScrapeRaw is the --raw path. It routes through ScrapeConditional so one
// fetch yields Page + RawHTML + Source (the markdown path's Scrape discards
// the body). Raw lookup is a hit only when RawHTML is non-empty — a
// markdown-only entry does not poison a --raw request. On a raw miss against
// an existing markdown entry, the refetch back-fills RawHTML while preserving
// the cached Page (one fetch, both representations cached). --no-cache (pc is
// nil) skips cache read/write and returns the fresh fetch result directly.
func cachedScrapeRaw(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, url string) (*scrape.Page, string, string, error) {
	key := s.Rewrite(url)
	if pc != nil {
		if rawHTML, source, page := pc.GetRaw(key); page != nil {
			return page, rawHTML, source, nil
		}
	}

	result, err := s.ScrapeConditional(ctx, url, "", "")
	if err != nil {
		return nil, "", "", err
	}
	if result.NotModified {
		return nil, "", "", exitErrf(ExitUpstream, "unexpected 304 Not Modified without cached ETag for %s", url)
	}

	if pc != nil {
		pc.PutRaw(key, result.Page, result.Source, result.RawHTML)
	}
	return result.Page, result.RawHTML, result.Source, nil
}

// cachedScrapeForce is the --force-browser markdown path. It always renders via
// the browser, reusing a cache entry only when that entry is itself a browser
// render (Position A: --force-browser selects the rendering pipeline, not cache
// freshness — use --no-cache for that). HTTP/shell/markdown-only entries never
// satisfy a forced request, which is precisely the #12 anti-poisoning guard.
func cachedScrapeForce(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, url string) (*scrape.Page, error) {
	key := s.Rewrite(url)
	if page, source := pc.Get(key); page != nil && source == scrape.SourceBrowser {
		return page, nil
	}
	page, _, err := s.BrowserScrape(ctx, url)
	if err != nil {
		return nil, err
	}
	pc.Put(key, page, scrape.SourceBrowser)
	return page, nil
}

// cachedScrapeRawForce is the --force-browser --raw path: render unconditionally
// and emit the rendered HTML. A cache hit is honored only for a prior browser
// render (GetRaw already requires non-empty RawHTML). BrowserScrape runs
// extractor.Extract internally; the resulting markdown Page is unused here, but
// that work is dwarfed by render cost — don't split the API to avoid it.
func cachedScrapeRawForce(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, url string) (*scrape.Page, string, string, error) {
	key := s.Rewrite(url)
	if pc != nil {
		if rawHTML, source, page := pc.GetRaw(key); page != nil && source == scrape.SourceBrowser {
			return page, rawHTML, source, nil
		}
	}
	page, html, err := s.BrowserScrape(ctx, url)
	if err != nil {
		return nil, "", "", err
	}
	if pc != nil {
		pc.PutRaw(key, page, scrape.SourceBrowser, html)
	}
	return page, html, scrape.SourceBrowser, nil
}

// scrapeMarkdown picks the markdown fetch path: forced browser render or the
// auto-detecting cachedScrape.
func scrapeMarkdown(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, url string, forceBrowser bool) (*scrape.Page, error) {
	if forceBrowser {
		return cachedScrapeForce(ctx, s, pc, url)
	}
	return cachedScrape(ctx, s, pc, url)
}

// scrapeRaw picks the --raw fetch path: forced browser render or the
// auto-detecting cachedScrapeRaw.
func scrapeRaw(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, url string, forceBrowser bool) (*scrape.Page, string, string, error) {
	if forceBrowser {
		return cachedScrapeRawForce(ctx, s, pc, url)
	}
	return cachedScrapeRaw(ctx, s, pc, url)
}

func scrapeSingle(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, rawURL string, asJSON, raw, trim bool, maxChars int, selector string, noLLMSTxt, forceBrowser bool) error {
	// --select: direct fetch + CSS extraction, bypasses cache and --raw.
	if selector != "" {
		return scrapeWithSelector(ctx, s, rawURL, asJSON, trim, maxChars, selector, forceBrowser)
	}

	// --raw bypasses the llms.txt probe and all markdown post-processing:
	// it is an output mode over the fetched HTML, not an extraction mode.
	if raw {
		page, rawHTML, source, err := scrapeRaw(ctx, s, pc, rawURL, forceBrowser)
		if err != nil {
			return exitErrf(ExitUpstream, "scrape failed: %w", err)
		}
		return emitRaw(os.Stdout, page, rawHTML, source, asJSON, maxChars)
	}

	// llms.txt auto-detection for bare domains. Skipped under --force-browser:
	// the caller explicitly wants the rendered page, not an /llms.txt shortcut.
	if !noLLMSTxt && !forceBrowser {
		if content, ok := fetchLLMSTxt(ctx, rawURL); ok {
			page := &scrape.Page{URL: rawURL, Title: "llms.txt", Markdown: content}
			page.Markdown = postProcess(page.Markdown, trim, maxChars)
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(page)
			}
			printPage(page)
			return nil
		}
	}

	page, err := scrapeMarkdown(ctx, s, pc, rawURL, forceBrowser)
	if err != nil {
		return exitErrf(ExitUpstream, "scrape failed: %w", err)
	}

	page.Markdown = postProcess(page.Markdown, trim, maxChars)

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(page)
	}

	printPage(page)
	return nil
}

func scrapeMultiple(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, urls []string, asJSON, raw, trim bool, maxChars int, selector string, noLLMSTxt bool, concurrency int, forceBrowser bool) error {
	type indexedResult struct {
		idx     int
		page    *scrape.Page
		rawHTML string
		source  string
		err     error
	}

	results := make([]indexedResult, len(urls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i, u := range urls {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, rawURL string) {
			defer wg.Done()
			defer func() { <-sem }()

			page, rawHTML, source, err := scrapeOneURL(ctx, s, pc, rawURL, raw, selector, noLLMSTxt, forceBrowser)
			results[idx] = indexedResult{idx: idx, page: page, rawHTML: rawHTML, source: source, err: err}
		}(i, u)
	}
	wg.Wait()

	if asJSON {
		if raw {
			out := make([]rawJSON, 0, len(results))
			for _, r := range results {
				if r.err != nil {
					fmt.Fprintf(os.Stderr, "warn: %v\n", r.err)
					continue
				}
				out = append(out, rawResultJSON(r.page, r.rawHTML, r.source, maxChars))
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}
		pages := make([]*scrape.Page, 0, len(results))
		for _, r := range results {
			if r.err != nil {
				fmt.Fprintf(os.Stderr, "warn: %v\n", r.err)
				continue
			}
			r.page.Markdown = postProcess(r.page.Markdown, trim, maxChars)
			pages = append(pages, r.page)
		}
		return json.NewEncoder(os.Stdout).Encode(pages)
	}

	for i, r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "warn: %v\n", r.err)
			continue
		}
		if raw {
			if i > 0 {
				fmt.Println()
			}
			_ = emitRaw(os.Stdout, r.page, r.rawHTML, r.source, false, maxChars)
			continue
		}
		r.page.Markdown = postProcess(r.page.Markdown, trim, maxChars)
		if i > 0 {
			fmt.Println()
		}
		printPage(r.page)
	}
	return nil
}

// scrapeOneURL handles a single URL within scrapeMultiple, applying selector,
// raw, and llms.txt detection the same way scrapeSingle does.
func scrapeOneURL(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, rawURL string, raw bool, selector string, noLLMSTxt, forceBrowser bool) (*scrape.Page, string, string, error) {
	if selector != "" {
		page, err := scrapeURLWithSelector(ctx, s, rawURL, selector, forceBrowser)
		return page, "", "", err
	}
	if raw {
		page, rawHTML, source, err := scrapeRaw(ctx, s, pc, rawURL, forceBrowser)
		return page, rawHTML, source, err
	}
	if !noLLMSTxt && !forceBrowser {
		if content, ok := fetchLLMSTxt(ctx, rawURL); ok {
			return &scrape.Page{URL: rawURL, Title: "llms.txt", Markdown: content}, "", "", nil
		}
	}
	page, err := scrapeMarkdown(ctx, s, pc, rawURL, forceBrowser)
	return page, "", "", err
}

// scrapeURLWithSelector fetches a URL and extracts content matching a CSS selector.
// Returns a Page without post-processing (caller applies trim/maxChars).
func scrapeURLWithSelector(ctx context.Context, s *scrape.Scraper, rawURL, selector string, forceBrowser bool) (*scrape.Page, error) {
	// Rewrite before fetch so --select shares the canonical URL-rewrite path
	// with Scrape/ScrapeConditional. Previously it fetched the unrewritten
	// URL, silently skipping configured rewrites.
	fetchURL := s.Rewrite(rawURL)
	html, err := fetchHTMLForSelector(ctx, s, rawURL, fetchURL, forceBrowser)
	if err != nil {
		return nil, err
	}
	markdown, err := extract.ExtractSelector(html, selector)
	if err != nil {
		return nil, exitErrf(ExitValidation, "selector extraction failed: %w", err)
	}
	if markdown == "" {
		return nil, exitErrf(ExitNotFound, "no elements matched selector %q", selector)
	}
	title := extract.Title(html)
	page := &scrape.Page{URL: rawURL, Title: title, Markdown: markdown}
	if fetchURL != rawURL {
		page.FetchedURL = fetchURL
	}
	return page, nil
}

// fetchHTMLForSelector returns the HTML to run a CSS selector against. Under
// --force-browser it renders via the browser (then selects the rendered DOM);
// otherwise it does the plain fetch with JS-shell auto-detection. rawURL is
// passed to BrowserScrape, which rewrites internally; fetchURL is the
// already-rewritten URL for the plain Fetch path.
func fetchHTMLForSelector(ctx context.Context, s *scrape.Scraper, rawURL, fetchURL string, forceBrowser bool) (string, error) {
	if forceBrowser {
		_, html, err := s.BrowserScrape(ctx, rawURL)
		if err != nil {
			return "", exitErrf(ExitUpstream, "browser fetch failed: %w", err)
		}
		return html, nil
	}
	html, err := s.Fetch(ctx, fetchURL)
	if err != nil {
		return "", exitErrf(ExitUpstream, "fetch failed: %w", err)
	}
	html, _ = s.MaybeBrowserFetch(ctx, fetchURL, html)
	return html, nil
}

func scrapeWithSelector(ctx context.Context, s *scrape.Scraper, rawURL string, asJSON bool, trim bool, maxChars int, selector string, forceBrowser bool) error {
	page, err := scrapeURLWithSelector(ctx, s, rawURL, selector, forceBrowser)
	if err != nil {
		return err
	}
	page.Markdown = postProcess(page.Markdown, trim, maxChars)
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(page)
	}
	printPage(page)
	return nil
}

// rawJSON is the --raw --json output shape. Plain --json must not include
// raw_html; only the --raw path emits this.
type rawJSON struct {
	URL        string `json:"url"`
	FetchedURL string `json:"fetched_url,omitempty"`
	Title      string `json:"title"`
	Source     string `json:"source"`
	RawHTML    string `json:"raw_html"`
}

// rawResultJSON builds the JSON object for one --raw result, truncating the
// HTML to maxChars Unicode code points when maxChars > 0.
func rawResultJSON(page *scrape.Page, rawHTML, source string, maxChars int) rawJSON {
	return rawJSON{
		URL:        page.URL,
		FetchedURL: page.FetchedURL,
		Title:      page.Title,
		Source:     source,
		RawHTML:    truncateContent(rawHTML, maxChars),
	}
}

// emitRaw writes a single --raw result to w. Plain output is bare HTML with
// no --- front matter so it pipes cleanly into pup/htmlq/a file. JSON output
// is {url, fetched_url, title, source, raw_html}.
func emitRaw(w io.Writer, page *scrape.Page, rawHTML, source string, asJSON bool, maxChars int) error {
	if asJSON {
		return json.NewEncoder(w).Encode(rawResultJSON(page, rawHTML, source, maxChars))
	}
	if _, err := fmt.Fprint(w, truncateContent(rawHTML, maxChars)); err != nil {
		return err
	}
	if !strings.HasSuffix(rawHTML, "\n") {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

// fetchLLMSTxt attempts to fetch /llms.txt from the given base URL.
// Returns the content and true if successful, empty string and false otherwise.
// All errors are silently swallowed — this is a best-effort check.
func fetchLLMSTxt(ctx context.Context, baseURL string) (string, bool) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", false
	}
	if u.Path != "" && u.Path != "/" {
		return "", false
	}

	// Cap llms.txt probes at 5s — they're best-effort and shouldn't delay
	// the real scrape if a host ignores the request.
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	llmsURL := u.Scheme + "://" + u.Host + "/llms.txt"
	req, err := http.NewRequestWithContext(probeCtx, "GET", llmsURL, nil)
	if err != nil {
		return "", false
	}
	resp, err := httpx.Default().Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		return "", false
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, scrape.MaxBodyBytes))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func printPage(p *scrape.Page) {
	words := len(strings.Fields(p.Markdown))
	fmt.Println("---")
	fmt.Printf("url: %s\n", p.URL)
	if p.FetchedURL != "" {
		fmt.Printf("fetched_url: %s\n", p.FetchedURL)
	}
	fmt.Printf("title: %s\n", p.Title)
	fmt.Printf("words: %d\n", words)
	fmt.Println("---")
	fmt.Println(p.Markdown)
}

// truncateContent caps s at maxChars Unicode code points, appending a truncation marker.
func truncateContent(s string, maxChars int) string {
	if maxChars <= 0 || utf8.RuneCountInString(s) <= maxChars {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxChars]) + "\n\n[truncated]"
}

// postProcess applies trim then truncate to markdown content.
func postProcess(s string, trim bool, maxChars int) string {
	if trim {
		s = extract.StripMarkdown(s)
	}
	return truncateContent(s, maxChars)
}

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
