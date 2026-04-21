package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/ketch/internal/cache"
	"github.com/1broseidon/ketch/internal/scrape"
	"github.com/1broseidon/ketch/internal/search"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search the web and return results",
	Long:  `Search the web using Brave (default), DuckDuckGo, or SearXNG. Add --scrape to fetch and extract full content from results.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().IntP("limit", "l", cfg.Limit, "max number of results")
	searchCmd.Flags().Bool("scrape", false, "scrape full content from each result")
	searchCmd.Flags().String("searxng-url", cfg.SearxngURL, "SearXNG instance URL")
	searchCmd.Flags().Int("max-chars", 0, "truncate markdown output to N chars (0 = disabled)")
	searchCmd.Flags().Bool("trim", false, "strip markdown formatting, keep content text only")
	searchCmd.Flags().Bool("minimal", false, "one result per line, tab-separated (url/title/snippet)")
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]
	limit, _ := cmd.Flags().GetInt("limit")
	doScrape, _ := cmd.Flags().GetBool("scrape")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	backend, _ := cmd.Root().PersistentFlags().GetString("backend")
	maxChars, _ := cmd.Flags().GetInt("max-chars")
	trim, _ := cmd.Flags().GetBool("trim")
	minimal, _ := cmd.Flags().GetBool("minimal")

	searcher, err := newSearcher(cmd, backend)
	if err != nil {
		return err
	}

	results, err := searcher.Search(cmd.Context(), query, limit)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if doScrape {
		pc := newPageCache(false)
		return searchScrape(cmd.Context(), results, pc, asJSON, trim, maxChars, minimal)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	if minimal {
		for _, r := range results {
			fmt.Printf("%s\t%s\t%s\n", r.URL, r.Title, r.Description)
		}
		return nil
	}

	fmt.Println("---")
	fmt.Printf("query: %s\n", query)
	fmt.Printf("backend: %s\n", backend)
	fmt.Printf("result_count: %d\n", len(results))
	fmt.Println("---")
	for _, r := range results {
		fmt.Printf("%s\n  %s\n", r.Title, r.URL)
		if r.Description != "" {
			fmt.Printf("  %s\n", r.Description)
		}
		fmt.Println()
	}
	return nil
}

func searchScrape(ctx context.Context, results []search.Result, pc *cache.Cache, asJSON bool, trim bool, maxChars int, minimal bool) error {
	scraper := scrape.New()

	if asJSON {
		for i, r := range results {
			page, err := cachedScrape(ctx, scraper, pc, r.URL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: failed to scrape %s: %v\n", r.URL, err)
				continue
			}
			results[i].Content = postProcess(page.Markdown, trim, maxChars)
		}
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	if minimal {
		for _, r := range results {
			page, err := cachedScrape(ctx, scraper, pc, r.URL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: failed to scrape %s: %v\n", r.URL, err)
				continue
			}
			content := postProcess(page.Markdown, trim, maxChars)
			snippet := firstLine(content)
			fmt.Printf("%s\t%s\t%s\n", r.URL, page.Title, snippet)
		}
		return nil
	}

	for i, r := range results {
		page, err := cachedScrape(ctx, scraper, pc, r.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to scrape %s: %v\n", r.URL, err)
			continue
		}
		content := postProcess(page.Markdown, trim, maxChars)
		if i > 0 {
			fmt.Println()
		}
		words := len(strings.Fields(content))
		fmt.Println("---")
		fmt.Printf("url: %s\n", r.URL)
		fmt.Printf("title: %s\n", page.Title)
		fmt.Printf("words: %d\n", words)
		fmt.Println("---")
		fmt.Println(content)
	}
	return nil
}

func newSearcher(cmd *cobra.Command, backend string) (search.Searcher, error) {
	switch backend {
	case "brave":
		if cfg.BraveAPIKey == "" {
			return nil, fmt.Errorf("brave: API key not set (get one free at https://brave.com/search/api/ then: ketch config set brave_api_key <key>)")
		}
		return search.NewBrave(cfg.BraveAPIKey), nil
	case "searxng":
		url, _ := cmd.Flags().GetString("searxng-url")
		return search.NewSearXNG(url), nil
	case "ddg":
		return search.NewDDG(), nil
	default:
		return nil, fmt.Errorf("unknown backend: %s", backend)
	}
}
