package cmd

import (
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
	Use:   "scrape <url> [urls...]",
	Short: "Scrape URLs and extract clean markdown",
	Long:  `Fetch one or more URLs, extract the main content, and convert to clean markdown. Multiple URLs are scraped concurrently.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runScrape,
}

func init() {
	rootCmd.AddCommand(scrapeCmd)
	scrapeCmd.Flags().Bool("raw", false, "output raw HTML instead of markdown")
	scrapeCmd.Flags().Bool("no-cache", false, "bypass the page cache")
	scrapeCmd.Flags().Int("max-chars", 0, "truncate markdown output to N chars (0 = disabled)")
	scrapeCmd.Flags().Bool("trim", false, "strip markdown formatting, keep content text only")
	scrapeCmd.Flags().String("select", "", "CSS selector to extract specific elements (skips readability)")
	scrapeCmd.Flags().Bool("no-llms-txt", false, "disable automatic /llms.txt detection for bare domains")
}

func runScrape(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	maxChars, _ := cmd.Flags().GetInt("max-chars")
	trim, _ := cmd.Flags().GetBool("trim")
	selector, _ := cmd.Flags().GetString("select")
	noLLMSTxt, _ := cmd.Flags().GetBool("no-llms-txt")

	var scraper *scrape.Scraper
	if cfg.Browser != "" {
		scraper = scrape.NewWithBrowser(cfg.Browser)
	} else {
		scraper = scrape.New()
	}
	defer scraper.Close()

	pc := newPageCache(noCache)
	defer pc.Close()

	if len(args) == 1 {
		return scrapeSingle(scraper, pc, args[0], asJSON, trim, maxChars, selector, noLLMSTxt)
	}
	return scrapeMultiple(scraper, pc, args, asJSON, trim, maxChars)
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

func scrapeMultiple(s *scrape.Scraper, pc *cache.Cache, urls []string, asJSON bool, trim bool, maxChars int) error {
	type indexedPage struct {
		idx  int
		page *scrape.Page
		err  error
	}

	results := make([]indexedPage, len(urls))
	var wg sync.WaitGroup

	for i, u := range urls {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			page, err := cachedScrape(s, pc, url)
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
