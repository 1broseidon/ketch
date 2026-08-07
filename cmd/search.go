package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/scrape"
	"github.com/1broseidon/ketch/search"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search the web and return results",
	Long: `Search the web using Brave, DuckDuckGo, SearXNG, Exa, Firecrawl, Keenable, Tavily, or SerpBase (default: the configured backend; brave if unset).

Add --scrape to fetch and extract full content from results. Use --multi to query and rank-fuse several providers, or --random to shuffle providers and stop at the first successful response. --multi, --random, and --backend are mutually exclusive.`,
	Args: exitArgs(cobra.MinimumNArgs(1)),
	RunE: runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().StringP("backend", "b", cfg.Backend,
		"search backend: "+strings.Join(config.AvailableBackends(), ", "))
	searchCmd.Flags().IntP("limit", "l", cfg.Limit, "max number of results")
	searchCmd.Flags().Bool("scrape", false, "scrape full content from each result")
	searchCmd.Flags().String("searxng-url", cfg.SearxngURL, "SearXNG instance URL")
	searchCmd.Flags().Int("max-chars", 0, "truncate markdown output to N chars (0 = disabled)")
	searchCmd.Flags().Bool("trim", false, "strip markdown formatting, keep content text only")
	searchCmd.Flags().Bool("minimal", false, "one result per line, tab-separated (url/title/snippet)")
	// NoOptDefVal makes bare --multi mean "all usable backends". Cobra requires
	// the = form to pass a value to such a flag: --multi=brave,exa (not
	// --multi brave,exa, which would swallow the query as the value).
	searchCmd.Flags().String("multi", "",
		"federated search across backends: comma-separated list, or bare/=all for every usable backend (use the = form, e.g. --multi=brave,exa)")
	searchCmd.Flags().Lookup("multi").NoOptDefVal = "all"
	searchCmd.Flags().String("random", "",
		"random provider with fallback: comma-separated list, or bare/=all for every usable backend (use the = form, e.g. --random=brave,exa)")
	searchCmd.Flags().Lookup("random").NoOptDefVal = "all"
	searchCmd.Flags().String("cookie-file", "", "Netscape cookies.txt jar for --scrape fetches; matching cookies are sent with each fetch (overrides config cookie_file)")
	searchCmd.Flags().String("user-agent", "", "HTTP User-Agent override for --scrape fetches (overrides config user_agent; empty falls back to the built-in default)")
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]
	limit, _ := cmd.Flags().GetInt("limit")
	doScrape, _ := cmd.Flags().GetBool("scrape")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	backend, _ := cmd.Flags().GetString("backend")
	maxChars, _ := cmd.Flags().GetInt("max-chars")
	trim, _ := cmd.Flags().GetBool("trim")
	minimal, _ := cmd.Flags().GetBool("minimal")

	if cmd.Flags().Changed("multi") && cmd.Flags().Changed("random") {
		return exitErrf(ExitValidation, "--multi and --random are mutually exclusive")
	}
	if cmd.Flags().Changed("multi") {
		return runMultiSearch(cmd, query, limit, doScrape, asJSON, trim, maxChars, minimal)
	}
	if cmd.Flags().Changed("random") {
		return runRandomSearch(cmd, query, limit, doScrape, asJSON, trim, maxChars, minimal)
	}

	searcher, err := newSearcher(cmd, backend)
	if err != nil {
		return err
	}

	results, err := searcher.Search(cmd.Context(), query, limit)
	if err != nil {
		return exitErrf(ExitUpstream, "search failed: %w", err)
	}

	if doScrape {
		scraper, err := newScraper(cmd)
		if err != nil {
			return err
		}
		defer scraper.Close()
		pc := newPageCache(false)
		return searchScrape(cmd.Context(), results, scraper, pc, asJSON, trim, maxChars, minimal)
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

func searchScrape(ctx context.Context, results []search.Result, scraper *scrape.Scraper, pc *cache.Cache, asJSON bool, trim bool, maxChars int, minimal bool) error {
	if asJSON {
		for i, r := range results {
			page, err := scraper.CachedScrape(ctx, pc, r.URL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: failed to scrape %s: %v\n", r.URL, err)
				continue
			}
			if page.FetchedURL != "" {
				results[i].FetchedURL = page.FetchedURL
			}
			results[i].Content = extract.PostProcess(page.Markdown, trim, maxChars)
		}
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	if minimal {
		for _, r := range results {
			page, err := scraper.CachedScrape(ctx, pc, r.URL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: failed to scrape %s: %v\n", r.URL, err)
				continue
			}
			content := extract.PostProcess(page.Markdown, trim, maxChars)
			snippet := firstLine(content)
			fmt.Printf("%s\t%s\t%s\n", r.URL, page.Title, snippet)
		}
		return nil
	}

	for i, r := range results {
		page, err := scraper.CachedScrape(ctx, pc, r.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to scrape %s: %v\n", r.URL, err)
			continue
		}
		if page.FetchedURL != "" {
			results[i].FetchedURL = page.FetchedURL
		}
		content := extract.PostProcess(page.Markdown, trim, maxChars)
		if i > 0 {
			fmt.Println()
		}
		words := len(strings.Fields(content))
		fmt.Println("---")
		fmt.Printf("url: %s\n", r.URL)
		if page.FetchedURL != "" {
			fmt.Printf("fetched_url: %s\n", page.FetchedURL)
		}
		fmt.Printf("title: %s\n", page.Title)
		fmt.Printf("words: %d\n", words)
		fmt.Println("---")
		fmt.Println(content)
	}
	return nil
}

// newSearcher resolves the backend via the shared search.NewFromConfig and
// maps constructor errors to CLI exit codes.
func newSearcher(cmd *cobra.Command, backend string) (search.Searcher, error) {
	searxngURL, _ := cmd.Flags().GetString("searxng-url")
	s, err := search.NewFromConfig(&cfg, backend, searxngURL)
	if err != nil {
		return nil, backendErr(err, search.ErrUnknownBackend)
	}
	return s, nil
}

// looksLikeBackendList reports whether s is a comma-separated list of two or
// more known backend names (e.g. "brave,exa") — the shape of a --multi value
// accidentally passed without '=' and parsed as the query.
func looksLikeBackendList(s string) bool {
	if !strings.Contains(s, ",") {
		return false
	}
	known := map[string]bool{}
	for _, b := range config.AvailableBackends() {
		known[b] = true
	}
	parts := strings.Split(s, ",")
	for _, p := range parts {
		if !known[strings.TrimSpace(p)] {
			return false
		}
	}
	return len(parts) >= 2
}

// parseMultiNames splits a --multi value into trimmed, de-duplicated backend
// names (first occurrence wins). A bare/empty or "all" value yields the "all"
// sentinel handled by search.NewMultiFromConfig.
func parseMultiNames(val string) []string {
	var names []string
	seen := map[string]bool{}
	for _, part := range strings.Split(val, ",") {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// runMultiSearch handles `ketch search --multi`: it validates the flag
// combination, resolves the federated backend set, fans out, and renders the
// fused results (json / minimal / plain), warning about partial failures on
// stderr per the house convention.
func runMultiSearch(cmd *cobra.Command, query string, limit int, doScrape, asJSON, trim bool, maxChars int, minimal bool) error {
	names, err := resolveMultiNames(cmd, query)
	if err != nil {
		return err
	}

	searxngURL, _ := cmd.Flags().GetString("searxng-url")
	m, err := search.NewMultiFromConfig(&cfg, names, searxngURL)
	if err != nil {
		return backendErr(err, search.ErrUnknownBackend)
	}

	results, berrs, err := m.Search(cmd.Context(), query, limit)
	if err != nil {
		return exitErrf(ExitUpstream, "search failed: %w", err)
	}
	for _, be := range berrs {
		fmt.Fprintf(os.Stderr, "warn: %s: %v\n", be.Backend, be.Err)
	}

	if doScrape {
		scraper, err := newScraper(cmd)
		if err != nil {
			return err
		}
		defer scraper.Close()
		pc := newPageCache(false)
		return searchScrape(cmd.Context(), results, scraper, pc, asJSON, trim, maxChars, minimal)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	if minimal {
		for _, r := range results {
			fmt.Printf("%s\t%s\t%s\t%s\n", r.URL, r.Title, r.Description, strings.Join(r.Backends, ","))
		}
		return nil
	}

	printMultiPlain(query, results, berrs, m.Names())
	return nil
}

// runRandomSearch handles `ketch search --random`: each invocation shuffles
// the requested providers and falls back sequentially only when a provider
// errors. The chosen provider is reported explicitly.
func runRandomSearch(cmd *cobra.Command, query string, limit int, doScrape, asJSON, trim bool, maxChars int, minimal bool) error {
	names, err := resolveRandomNames(cmd, query)
	if err != nil {
		return err
	}

	searxngURL, _ := cmd.Flags().GetString("searxng-url")
	randomSearch, err := search.NewRandomFromConfig(&cfg, names, searxngURL)
	if err != nil {
		return backendErr(err, search.ErrUnknownBackend)
	}
	results, selected, failures, err := randomSearch.Search(cmd.Context(), query, limit)
	if err != nil {
		return exitErrf(ExitUpstream, "search failed: %w", err)
	}
	for _, failure := range failures {
		fmt.Fprintf(os.Stderr, "warn: %s: %v\n", failure.Backend, failure.Err)
	}

	if doScrape {
		scraper, err := newScraper(cmd)
		if err != nil {
			return err
		}
		defer scraper.Close()
		pc := newPageCache(false)
		return searchScrape(cmd.Context(), results, scraper, pc, asJSON, trim, maxChars, minimal)
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}
	if minimal {
		for _, result := range results {
			fmt.Printf("%s\t%s\t%s\n", result.URL, result.Title, result.Description)
		}
		return nil
	}

	printRandomPlain(query, selected, results, failures)
	return nil
}

// resolveMultiNames validates the --multi flag combination and resolves the
// requested backend-name list ("all" sentinel included).
func resolveMultiNames(cmd *cobra.Command, query string) ([]string, error) {
	if cmd.Flags().Changed("backend") {
		return nil, exitErrf(ExitValidation, "--multi and --backend are mutually exclusive")
	}
	if cmd.Flags().Changed("random") {
		return nil, exitErrf(ExitValidation, "--multi and --random are mutually exclusive")
	}

	multiVal, _ := cmd.Flags().GetString("multi")
	names := parseMultiNames(multiVal)
	for _, n := range names {
		if n == "all" && len(names) > 1 {
			return nil, exitErrf(ExitValidation, `--multi: "all" cannot be combined with other backend names`)
		}
	}
	if len(names) == 0 {
		names = []string{"all"}
	}

	// Defuse the NoOptDefVal trap: `--multi brave,exa <query>` parses as
	// multi="all" with "brave,exa" swallowed as the query, silently searching
	// the literal backend list. When bare --multi gets a query that is
	// exactly a comma-list of known backend names, the operator almost
	// certainly meant --multi=<list>.
	if multiVal == "all" && looksLikeBackendList(query) {
		return nil, exitErrf(ExitValidation, "--multi needs '=' for a backend list: did you mean --multi=%s?", query)
	}
	return names, nil
}

// resolveRandomNames mirrors --multi parsing while enforcing that random
// selection cannot be combined with a fixed backend or federated search.
func resolveRandomNames(cmd *cobra.Command, query string) ([]string, error) {
	if cmd.Flags().Changed("backend") {
		return nil, exitErrf(ExitValidation, "--random and --backend are mutually exclusive")
	}
	if cmd.Flags().Changed("multi") {
		return nil, exitErrf(ExitValidation, "--random and --multi are mutually exclusive")
	}

	randomValue, _ := cmd.Flags().GetString("random")
	names := parseMultiNames(randomValue)
	for _, name := range names {
		if name == "all" && len(names) > 1 {
			return nil, exitErrf(ExitValidation, `--random: "all" cannot be combined with other backend names`)
		}
	}
	if len(names) == 0 {
		names = []string{"all"}
	}
	if randomValue == "all" && looksLikeBackendList(query) {
		return nil, exitErrf(ExitValidation, "--random needs '=' for a backend list: did you mean --random=%s?", query)
	}
	return names, nil
}

// printMultiPlain renders the fused results with multi frontmatter:
// backends: lists the engines that contributed; failed: (only when partial)
// lists the ones that errored — together the resolved set.
func printRandomPlain(query, selected string, results []search.Result, failures []search.BackendError) {
	fmt.Println("---")
	fmt.Printf("query: %s\n", query)
	fmt.Printf("backend: %s\n", selected)
	if len(failures) > 0 {
		parts := make([]string, len(failures))
		for i, failure := range failures {
			parts[i] = fmt.Sprintf("%s (%v)", failure.Backend, failure.Err)
		}
		fmt.Printf("failed: %s\n", strings.Join(parts, ", "))
	}
	fmt.Printf("result_count: %d\n", len(results))
	fmt.Println("---")
	for _, result := range results {
		fmt.Printf("%s\n  %s\n", result.Title, result.URL)
		if result.Description != "" {
			fmt.Printf("  %s\n", result.Description)
		}
		fmt.Println()
	}
}

func printMultiPlain(query string, results []search.Result, berrs []search.BackendError, resolved []string) {
	failed := map[string]bool{}
	for _, be := range berrs {
		failed[be.Backend] = true
	}
	var succeeded []string
	for _, n := range resolved {
		if !failed[n] {
			succeeded = append(succeeded, n)
		}
	}

	fmt.Println("---")
	fmt.Printf("query: %s\n", query)
	fmt.Printf("backends: %s\n", strings.Join(succeeded, ", "))
	if len(berrs) > 0 {
		parts := make([]string, len(berrs))
		for i, be := range berrs {
			parts[i] = fmt.Sprintf("%s (%v)", be.Backend, be.Err)
		}
		fmt.Printf("failed: %s\n", strings.Join(parts, ", "))
	}
	fmt.Printf("result_count: %d\n", len(results))
	fmt.Println("---")
	for _, r := range results {
		fmt.Printf("%s\n  %s\n", r.Title, r.URL)
		if r.Description != "" {
			fmt.Printf("  %s\n", r.Description)
		}
		if len(r.Backends) > 0 {
			fmt.Printf("  found in: %s\n", strings.Join(r.Backends, ", "))
		}
		fmt.Println()
	}
}
