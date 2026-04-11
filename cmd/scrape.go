package cmd

import (
	"encoding/json"
	"fmt"
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
}

func runScrape(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	maxChars, _ := cmd.Flags().GetInt("max-chars")
	trim, _ := cmd.Flags().GetBool("trim")

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
		return scrapeSingle(scraper, pc, args[0], asJSON, trim, maxChars)
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

func scrapeSingle(s *scrape.Scraper, pc *cache.Cache, url string, asJSON bool, trim bool, maxChars int) error {
	page, err := cachedScrape(s, pc, url)
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
