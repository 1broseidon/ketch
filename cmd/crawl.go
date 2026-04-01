package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/internal/cache"
	"github.com/1broseidon/ketch/internal/crawl"
	"github.com/1broseidon/ketch/internal/extract"
	"github.com/spf13/cobra"
)

var crawlCmd = &cobra.Command{
	Use:   "crawl <url>",
	Short: "Crawl a site and extract pages",
	Long:  `BFS crawl from a seed URL, extracting clean markdown from each discovered page. Streams results as they are found.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runCrawl,
}

func init() {
	rootCmd.AddCommand(crawlCmd)
	crawlCmd.Flags().Int("depth", 3, "max BFS depth")
	crawlCmd.Flags().Int("concurrency", 8, "worker pool size")
	crawlCmd.Flags().StringSlice("allow", nil, "path substring filters (any match passes)")
	crawlCmd.Flags().StringSlice("deny", nil, "regex deny patterns")
	crawlCmd.Flags().Bool("sitemap", false, "treat seed URL as sitemap")
	crawlCmd.Flags().Bool("no-cache", false, "bypass the page cache")
	crawlCmd.Flags().Bool("background", false, "run crawl in background, return immediately with crawl ID")
}

func runCrawl(cmd *cobra.Command, args []string) error {
	// Background worker mode (re-executed child process)
	if workerID := os.Getenv("KETCH_CRAWL_WORKER"); workerID != "" {
		return runCrawlWorker(cmd, args, workerID)
	}

	// Background launch mode
	background, _ := cmd.Flags().GetBool("background")
	if background {
		return runCrawlBackground(args)
	}

	seed := args[0]
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	depth, _ := cmd.Flags().GetInt("depth")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	allow, _ := cmd.Flags().GetStringSlice("allow")
	deny, _ := cmd.Flags().GetStringSlice("deny")
	sitemap, _ := cmd.Flags().GetBool("sitemap")
	noCache, _ := cmd.Flags().GetBool("no-cache")

	pc := newCrawlCache(noCache)
	defer pc.Close()

	opts := crawl.Options{
		Depth:       depth,
		Concurrency: concurrency,
		Allow:       allow,
		Deny:        deny,
		BrowserBin:  cfg.Browser,
	}

	var (
		mu        sync.Mutex
		count     int
		newCount  int
		changed   int
		unchanged int
		errCount  int
		first     = true
	)
	start := time.Now()

	fn := func(r crawl.Result) {
		mu.Lock()
		defer mu.Unlock()
		count++

		if r.Error != "" {
			errCount++
			if asJSON {
				printCrawlJSON(r)
				return
			}
			fmt.Fprintf(os.Stderr, "warn: %s: %s\n", r.URL, r.Error)
			return
		}

		switch r.Status {
		case "new":
			newCount++
		case "changed":
			changed++
		case "unchanged":
			unchanged++
		}

		if r.Page == nil {
			return
		}

		if asJSON {
			printCrawlJSON(r)
		} else {
			if !first {
				fmt.Println()
			}
			first = false
			printCrawlPage(r)
		}
	}

	err := crawl.Crawl(seed, opts, pc, sitemap, fn)

	duration := time.Since(start)
	printCrawlSummary(seed, count, newCount, changed, unchanged, errCount, duration)

	if err != nil {
		return fmt.Errorf("crawl failed: %w", err)
	}
	return nil
}

func printCrawlPage(r crawl.Result) {
	words := len(strings.Fields(r.Page.Markdown))
	fmt.Println("---")
	fmt.Printf("url: %s\n", r.Page.URL)
	fmt.Printf("title: %s\n", r.Page.Title)
	fmt.Printf("words: %d\n", words)
	fmt.Printf("status: %s\n", r.Status)
	fmt.Printf("source: %s\n", r.Source)
	fmt.Println("---")
	fmt.Println(r.Page.Markdown)
}

type crawlJSONResult struct {
	URL    string         `json:"url"`
	Depth  int            `json:"depth"`
	Status string         `json:"status"`
	Source string         `json:"source,omitempty"`
	Error  string         `json:"error,omitempty"`
	Page   *crawlJSONPage `json:"page,omitempty"`
}

type crawlJSONPage struct {
	URL              string            `json:"url"`
	FinalURL         string            `json:"final_url,omitempty"`
	Title            string            `json:"title"`
	Summary          string            `json:"summary,omitempty"`
	CanonicalURL     string            `json:"canonical_url,omitempty"`
	Headings         []extract.Heading `json:"headings,omitempty"`
	Links            []string          `json:"links,omitempty"`
	Quality          *extract.Quality  `json:"quality,omitempty"`
	ExtractionSource string            `json:"extraction_source,omitempty"`
	Words            int               `json:"words"`
	Markdown         string            `json:"markdown"`
	ETag             string            `json:"etag,omitempty"`
	LastModified     string            `json:"last_modified,omitempty"`
	ContentHash      string            `json:"content_hash,omitempty"`
}

func printCrawlJSON(r crawl.Result) {
	obj := buildCrawlJSONResult(r)
	data, err := json.Marshal(obj)
	if err != nil {
		return
	}
	fmt.Println(string(data))
}

func buildCrawlJSONResult(r crawl.Result) crawlJSONResult {
	obj := crawlJSONResult{
		URL:    r.URL,
		Depth:  r.Depth,
		Status: r.Status,
		Source: r.Source,
		Error:  r.Error,
	}
	if obj.Status == "" && obj.Error != "" {
		obj.Status = "error"
	}
	if r.Page == nil {
		return obj
	}
	summary := strings.TrimSpace(r.Page.Summary)
	if summary == "" {
		summary = summarizeMarkdown(r.Page.Markdown)
	}
	obj.URL = r.Page.URL
	obj.Page = &crawlJSONPage{
		URL:              r.Page.URL,
		FinalURL:         r.Page.FinalURL,
		Title:            r.Page.Title,
		Summary:          summary,
		CanonicalURL:     r.Page.CanonicalURL,
		Headings:         r.Page.Headings,
		Links:            r.Page.Links,
		Quality:          r.Page.Quality,
		ExtractionSource: r.Page.ExtractionSource,
		Words:            len(strings.Fields(r.Page.Markdown)),
		Markdown:         r.Page.Markdown,
		ETag:             r.Page.ETag,
		LastModified:     r.Page.LastModified,
		ContentHash:      r.Page.ContentHash,
	}
	return obj
}

func summarizeMarkdown(markdown string) string {
	trimmed := strings.TrimSpace(markdown)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "\n\n")
	for _, part := range parts {
		candidate := strings.TrimSpace(part)
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		candidate = strings.Join(strings.Fields(candidate), " ")
		if len(candidate) > 280 {
			return strings.TrimSpace(candidate[:280])
		}
		return candidate
	}
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	if len(trimmed) > 280 {
		return strings.TrimSpace(trimmed[:280])
	}
	return trimmed
}

func printCrawlSummary(seed string, total, newC, changed, unchanged, errors int, d time.Duration) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "---")
	fmt.Fprintf(os.Stderr, "seed: %s\n", seed)
	fmt.Fprintf(os.Stderr, "pages: %d\n", total-errors)
	fmt.Fprintf(os.Stderr, "new: %d\n", newC)
	fmt.Fprintf(os.Stderr, "changed: %d\n", changed)
	fmt.Fprintf(os.Stderr, "unchanged: %d\n", unchanged)
	fmt.Fprintf(os.Stderr, "errors: %d\n", errors)
	fmt.Fprintf(os.Stderr, "duration: %.1fs\n", d.Seconds())
	fmt.Fprintln(os.Stderr, "---")
}

// newCrawlCache creates a cache for crawl operations using the configured TTL.
func newCrawlCache(noCache bool) *cache.Cache {
	if noCache {
		return nil
	}
	return newPageCache(false)
}
