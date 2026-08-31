package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/scrape"
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
	scrapeCmd.Flags().String("cookie-file", "", "Netscape cookies.txt jar; matching cookies are sent with each fetch (overrides config cookie_file)")
	scrapeCmd.Flags().String("user-agent", "", "User-Agent override (overrides config user_agent; applies to HTTP and browser fetches; empty falls back to the built-in default)")
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

	scraper, err := newScraper(cmd)
	if err != nil {
		return err
	}
	defer scraper.Close()

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

// newScraper builds a Scraper from cfg, honoring --cookie-file / --user-agent
// flags when the invoking command defines them (flag overrides config; an
// explicit empty --cookie-file disables cookies for the run, an explicit
// empty --user-agent falls back to the built-in default). Construction
// failures — including a bad jar path — classify as precondition errors.
// Returned scraper must be Closed by the caller.
func newScraper(cmd *cobra.Command) (*scrape.Scraper, error) {
	c := cfg // shallow copy; only CookieFile / UserAgent are overridden
	if f := cmd.Flags().Lookup("cookie-file"); f != nil && f.Changed {
		c.CookieFile = f.Value.String()
	}
	if f := cmd.Flags().Lookup("user-agent"); f != nil && f.Changed {
		c.UserAgent = f.Value.String()
	}
	s, err := scrape.NewFromConfig(&c)
	if err != nil {
		return nil, &ExitError{Code: ExitPrecondition, Err: err}
	}
	return s, nil
}

// newPageCache creates a cache from config, or nil if disabled.
func newPageCache(noCache bool) *cache.Cache {
	if noCache {
		return nil
	}
	return cache.NewFromConfig(&cfg)
}

func scrapeSingle(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, rawURL string, asJSON, raw, trim bool, maxChars int, selector string, noLLMSTxt, forceBrowser bool) error {
	// --select: direct fetch + CSS extraction, bypasses cache and --raw.
	if selector != "" {
		return scrapeWithSelector(ctx, s, rawURL, asJSON, trim, maxChars, selector, forceBrowser)
	}

	// --raw bypasses the llms.txt probe and all markdown post-processing:
	// it is an output mode over the fetched HTML, not an extraction mode.
	if raw {
		page, rawHTML, source, err := s.ScrapeRaw(ctx, pc, rawURL, forceBrowser)
		if err != nil {
			if errors.Is(err, scrape.ErrPDFRawUnsupported) {
				return &ExitError{Code: ExitValidation, Err: err}
			}
			return classifyScrapeFailure(err)
		}
		return emitRaw(os.Stdout, page, rawHTML, source, asJSON, maxChars)
	}

	// llms.txt auto-detection for bare domains. Skipped under --force-browser:
	// the caller explicitly wants the rendered page, not an /llms.txt shortcut.
	if !noLLMSTxt && !forceBrowser {
		if content, ok := s.FetchLLMSTxt(ctx, rawURL); ok {
			page := &scrape.Page{URL: rawURL, Title: "llms.txt", Markdown: content}
			page.Markdown = extract.PostProcess(page.Markdown, trim, maxChars)
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(page)
			}
			printPage(page)
			return nil
		}
	}

	page, err := s.ScrapeMarkdown(ctx, pc, rawURL, forceBrowser)
	if err != nil {
		return classifyScrapeFailure(err)
	}

	page.Markdown = extract.PostProcess(page.Markdown, trim, maxChars)

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(page)
	}

	printPage(page)
	return nil
}

type indexedResult struct {
	idx     int
	page    *scrape.Page
	rawHTML string
	source  string
	err     error
}

func scrapeMultiple(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, urls []string, asJSON, raw, trim bool, maxChars int, selector string, noLLMSTxt bool, concurrency int, forceBrowser bool) error {
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

	if err := scrapeBatchTerminalError(results); err != nil {
		return err
	}

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
			r.page.Markdown = extract.PostProcess(r.page.Markdown, trim, maxChars)
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
		r.page.Markdown = extract.PostProcess(r.page.Markdown, trim, maxChars)
		if i > 0 {
			fmt.Println()
		}
		printPage(r.page)
	}
	return nil
}

func scrapeBatchTerminalError(results []indexedResult) error {
	for _, result := range results {
		if errors.Is(result.err, scrape.ErrPDFRawUnsupported) || errors.Is(result.err, scrape.ErrPDFSelectorUnsupported) {
			return &ExitError{Code: ExitValidation, Err: result.err}
		}
		if errors.Is(result.err, extract.ErrPDFNoText) || errors.Is(result.err, scrape.ErrNoBrowser) {
			return classifyScrapeFailure(result.err)
		}
	}
	return nil
}

func classifyScrapeFailure(err error) error {
	switch {
	case errors.Is(err, extract.ErrPDFNoText):
		return exitErrf(ExitPrecondition, "scrape failed: %w; hint: configure external_pdf_to_md_converter_command with an OCR-capable converter", err)
	case errors.Is(err, scrape.ErrNoBrowser):
		return exitErrf(ExitPrecondition, "--force-browser requires a configured browser (set with: ketch config set browser chrome): %w", err)
	default:
		return exitErrf(ExitUpstream, "scrape failed: %w", err)
	}
}

// scrapeOneURL handles a single URL within scrapeMultiple, applying selector,
// raw, and llms.txt detection the same way scrapeSingle does.
func scrapeOneURL(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, rawURL string, raw bool, selector string, noLLMSTxt, forceBrowser bool) (*scrape.Page, string, string, error) {
	if selector != "" {
		page, err := scrapeURLWithSelector(ctx, s, rawURL, selector, forceBrowser)
		return page, "", "", err
	}
	if raw {
		page, rawHTML, source, err := s.ScrapeRaw(ctx, pc, rawURL, forceBrowser)
		return page, rawHTML, source, err
	}
	if !noLLMSTxt && !forceBrowser {
		if content, ok := s.FetchLLMSTxt(ctx, rawURL); ok {
			return &scrape.Page{URL: rawURL, Title: "llms.txt", Markdown: content}, "", "", nil
		}
	}
	page, err := s.ScrapeMarkdown(ctx, pc, rawURL, forceBrowser)
	return page, "", "", err
}

// scrapeURLWithSelector runs the shared selector pipeline and classifies its
// sentinel errors into CLI exit codes: bad selector → validation, no match →
// not found, anything else (fetch/browser) → upstream.
func scrapeURLWithSelector(ctx context.Context, s *scrape.Scraper, rawURL, selector string, forceBrowser bool) (*scrape.Page, error) {
	page, err := s.ScrapeSelector(ctx, rawURL, selector, forceBrowser)
	if err != nil {
		switch {
		case errors.Is(err, scrape.ErrBadSelector), errors.Is(err, scrape.ErrPDFSelectorUnsupported):
			return nil, &ExitError{Code: ExitValidation, Err: err}
		case errors.Is(err, scrape.ErrSelectorNoMatch):
			return nil, &ExitError{Code: ExitNotFound, Err: err}
		default:
			return nil, classifyScrapeFailure(err)
		}
	}
	return page, nil
}

func scrapeWithSelector(ctx context.Context, s *scrape.Scraper, rawURL string, asJSON bool, trim bool, maxChars int, selector string, forceBrowser bool) error {
	page, err := scrapeURLWithSelector(ctx, s, rawURL, selector, forceBrowser)
	if err != nil {
		return err
	}
	page.Markdown = extract.PostProcess(page.Markdown, trim, maxChars)
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
		RawHTML:    extract.Truncate(rawHTML, maxChars),
	}
}

// emitRaw writes a single --raw result to w. Plain output is bare HTML with
// no --- front matter so it pipes cleanly into pup/htmlq/a file. JSON output
// is {url, fetched_url, title, source, raw_html}.
func emitRaw(w io.Writer, page *scrape.Page, rawHTML, source string, asJSON bool, maxChars int) error {
	if asJSON {
		return json.NewEncoder(w).Encode(rawResultJSON(page, rawHTML, source, maxChars))
	}
	if _, err := fmt.Fprint(w, extract.Truncate(rawHTML, maxChars)); err != nil {
		return err
	}
	if !strings.HasSuffix(rawHTML, "\n") {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
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
