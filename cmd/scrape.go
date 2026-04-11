package cmd

import (
	"bufio"
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

	"github.com/1broseidon/ketch/internal/cache"
	"github.com/1broseidon/ketch/internal/extract"
	"github.com/1broseidon/ketch/internal/scrape"
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
}

func runScrape(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	maxChars, _ := cmd.Flags().GetInt("max-chars")
	trim, _ := cmd.Flags().GetBool("trim")
	selector, _ := cmd.Flags().GetString("select")
	noLLMSTxt, _ := cmd.Flags().GetBool("no-llms-txt")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	urls, err := resolveURLs(args)
	if err != nil {
		return err
	}

	var scraper *scrape.Scraper
	if cfg.Browser != "" {
		scraper = scrape.NewWithBrowser(cfg.Browser)
	} else {
		scraper = scrape.New()
	}
	defer scraper.Close()

	pc := newPageCache(noCache)
	defer pc.Close()

	if len(urls) == 1 {
		return scrapeSingle(scraper, pc, urls[0], asJSON, trim, maxChars, selector, noLLMSTxt)
	}
	return scrapeMultiple(scraper, pc, urls, asJSON, trim, maxChars, selector, noLLMSTxt, concurrency)
}

// resolveURLs detects the input mode and returns a list of URLs.
func resolveURLs(args []string) ([]string, error) {
	if len(args) > 1 {
		return args, nil
	}

	if stdinIsPipe() {
		urls := readLines(os.Stdin)
		if len(urls) > 0 {
			return urls, nil
		}
	}

	if len(args) == 1 {
		arg := args[0]
		if strings.HasPrefix(strings.TrimSpace(arg), "[") {
			var urls []string
			if err := json.Unmarshal([]byte(arg), &urls); err != nil {
				return nil, fmt.Errorf("failed to parse JSON array: %w", err)
			}
			if len(urls) == 0 {
				return nil, fmt.Errorf("JSON array is empty")
			}
			return urls, nil
		}
		if _, err := os.Stat(arg); err == nil {
			urls, err := readLinesFromFile(arg)
			if err != nil {
				return nil, err
			}
			if len(urls) == 0 {
				return nil, fmt.Errorf("file %q contains no URLs", arg)
			}
			return urls, nil
		}
		return []string{arg}, nil
	}

	return nil, fmt.Errorf("provide a URL, file path, JSON array, or pipe URLs via stdin")
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
func cachedScrape(s *scrape.Scraper, pc *cache.Cache, url string) (*scrape.Page, error) {
	if page := pc.Get(url); page != nil {
		return page, nil
	}

	page, err := s.Scrape(url)
	if err != nil {
		return nil, err
	}

	pc.Put(url, page)
	return page, nil
}

func scrapeSingle(s *scrape.Scraper, pc *cache.Cache, rawURL string, asJSON bool, trim bool, maxChars int, selector string, noLLMSTxt bool) error {
	// --select: direct fetch + CSS extraction, bypasses cache
	if selector != "" {
		return scrapeWithSelector(s, rawURL, asJSON, trim, maxChars, selector)
	}

	// llms.txt auto-detection for bare domains
	if !noLLMSTxt {
		if content, ok := fetchLLMSTxt(rawURL); ok {
			page := &scrape.Page{URL: rawURL, Title: "llms.txt", Markdown: content}
			page.Markdown = postProcess(page.Markdown, trim, maxChars)
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(page)
			}
			printPage(page)
			return nil
		}
	}

	page, err := cachedScrape(s, pc, rawURL)
	if err != nil {
		return fmt.Errorf("scrape failed: %w", err)
	}

	page.Markdown = postProcess(page.Markdown, trim, maxChars)

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(page)
	}

	printPage(page)
	return nil
}

func scrapeMultiple(s *scrape.Scraper, pc *cache.Cache, urls []string, asJSON, trim bool, maxChars int, selector string, noLLMSTxt bool, concurrency int) error {
	type indexedPage struct {
		idx  int
		page *scrape.Page
		err  error
	}

	results := make([]indexedPage, len(urls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i, u := range urls {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, rawURL string) {
			defer wg.Done()
			defer func() { <-sem }()

			page, err := scrapeOneURL(s, pc, rawURL, selector, noLLMSTxt)
			results[idx] = indexedPage{idx: idx, page: page, err: err}
		}(i, u)
	}
	wg.Wait()

	if asJSON {
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
		r.page.Markdown = postProcess(r.page.Markdown, trim, maxChars)
		if i > 0 {
			fmt.Println()
		}
		printPage(r.page)
	}
	return nil
}

// scrapeOneURL handles a single URL within scrapeMultiple, applying selector
// and llms.txt detection the same way scrapeSingle does.
func scrapeOneURL(s *scrape.Scraper, pc *cache.Cache, rawURL, selector string, noLLMSTxt bool) (*scrape.Page, error) {
	if selector != "" {
		return scrapeURLWithSelector(s, rawURL, selector)
	}
	if !noLLMSTxt {
		if content, ok := fetchLLMSTxt(rawURL); ok {
			return &scrape.Page{URL: rawURL, Title: "llms.txt", Markdown: content}, nil
		}
	}
	return cachedScrape(s, pc, rawURL)
}

// scrapeURLWithSelector fetches a URL and extracts content matching a CSS selector.
// Returns a Page without post-processing (caller applies trim/maxChars).
func scrapeURLWithSelector(s *scrape.Scraper, rawURL, selector string) (*scrape.Page, error) {
	html, err := s.Fetch(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	html = s.MaybeBrowserFetch(rawURL, html)
	markdown, err := extract.ExtractSelector(html, selector)
	if err != nil {
		return nil, fmt.Errorf("selector extraction failed: %w", err)
	}
	if markdown == "" {
		return nil, fmt.Errorf("no elements matched selector %q", selector)
	}
	title := extract.Title(html)
	return &scrape.Page{URL: rawURL, Title: title, Markdown: markdown}, nil
}

func scrapeWithSelector(s *scrape.Scraper, rawURL string, asJSON bool, trim bool, maxChars int, selector string) error {
	html, err := s.Fetch(rawURL)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	// Apply browser fallback for JS-rendered pages before running the selector,
	// otherwise the selector matches against the empty shell, not the real content.
	html = s.MaybeBrowserFetch(rawURL, html)

	markdown, err := extract.ExtractSelector(html, selector)
	if err != nil {
		return fmt.Errorf("selector extraction failed: %w", err)
	}
	if markdown == "" {
		return fmt.Errorf("no elements matched selector %q", selector)
	}

	title := extract.Title(html)
	page := &scrape.Page{URL: rawURL, Title: title, Markdown: markdown}
	page.Markdown = postProcess(page.Markdown, trim, maxChars)

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(page)
	}
	printPage(page)
	return nil
}

// fetchLLMSTxt attempts to fetch /llms.txt from the given base URL.
// Returns the content and true if successful, empty string and false otherwise.
// All errors are silently swallowed — this is a best-effort check.
func fetchLLMSTxt(baseURL string) (string, bool) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", false
	}
	if u.Path != "" && u.Path != "/" {
		return "", false
	}

	llmsURL := u.Scheme + "://" + u.Host + "/llms.txt"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(llmsURL) //nolint:noctx
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

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func printPage(p *scrape.Page) {
	words := len(strings.Fields(p.Markdown))
	fmt.Println("---")
	fmt.Printf("url: %s\n", p.URL)
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
