package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/internal/cache"
	"github.com/1broseidon/ketch/internal/crawl"
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

	err := crawl.Crawl(cmd.Context(), seed, opts, pc, sitemap, fn)

	duration := time.Since(start)
	printCrawlSummary(seed, count, newCount, changed, unchanged, errCount, duration)

	// Ctrl+C is a deliberate user action, not a crawl failure. The summary
	// above already reports what was collected before shutdown.
	if err != nil && errors.Is(err, context.Canceled) {
		return nil
	}
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
	URL    string `json:"url"`
	Title  string `json:"title"`
	Words  int    `json:"words"`
	Status string `json:"status"`
	Source string `json:"source"`
	Body   string `json:"body"`
}

func printCrawlJSON(r crawl.Result) {
	obj := crawlJSONResult{
		URL:    r.Page.URL,
		Title:  r.Page.Title,
		Words:  len(strings.Fields(r.Page.Markdown)),
		Status: r.Status,
		Source: r.Source,
		Body:   r.Page.Markdown,
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return
	}
	fmt.Println(string(data))
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
