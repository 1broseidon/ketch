package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/docs"
	"github.com/spf13/cobra"
)

var docsCmd = &cobra.Command{
	Use:   "docs <query>",
	Short: "Search library documentation",
	Long:  `Search library documentation using ` + strings.Join(docs.ProviderNames(), ", ") + ` (default: the configured backend). Supports direct library ID lookup and library name resolution. A local FTS5 backend is planned but not yet implemented.`,
	Args:  exitArgs(cobra.MinimumNArgs(1)),
	RunE:  runDocs,
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.Flags().StringP("backend", "b", cfg.DocsBackend, "docs backend: "+strings.Join(config.AvailableDocBackends(), ", "))
	docsCmd.Flags().String("library", "", strings.Join(docs.LibraryProviderNames(), ", ")+" library ID (skip resolve step)")
	docsCmd.Flags().Int("tokens", 4000, strings.Join(docs.LibraryProviderNames(), ", ")+" token budget")
	docsCmd.Flags().IntP("limit", "l", cfg.Limit, "max number of results")
	docsCmd.Flags().Bool("resolve", false, "resolve library name instead of searching")
	docsCmd.Flags().Bool("minimal", false, "one result per line, tab-separated (url/library/snippet)")
}

func runDocs(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	backend, _ := cmd.Flags().GetString("backend")
	library, _ := cmd.Flags().GetString("library")
	tokens, _ := cmd.Flags().GetInt("tokens")
	limit, _ := cmd.Flags().GetInt("limit")
	resolve, _ := cmd.Flags().GetBool("resolve")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	minimal, _ := cmd.Flags().GetBool("minimal")

	if resolve {
		return runDocsResolve(cmd, query, limit, asJSON)
	}

	if library != "" {
		// Reject unsupported library operations instead of silently rerouting.
		if !docs.SupportsLibraries(backend) {
			return exitErrf(ExitValidation, "--library requires the %s backend (got %q)", strings.Join(docs.LibraryBackends(), ", "), backend)
		}
		if !cmd.Flags().Changed("limit") {
			limit = 0 // the token budget is the only bound unless --limit is explicit
		}
		return runDocsWithLibrary(cmd, query, library, tokens, limit, asJSON, minimal)
	}

	searcher, err := newDocSearcher(backend)
	if err != nil {
		return err
	}

	results, err := searcher.Search(cmd.Context(), query, limit)
	if err != nil {
		return upstreamErr(err, "docs search failed")
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	printDocsResults(query, backend, "", results, minimal)
	return nil
}

func runDocsResolve(cmd *cobra.Command, query string, limit int, asJSON bool) error {
	backend, _ := cmd.Flags().GetString("backend")
	backend = docs.ResolveBackend(backend)
	searcher, err := newDocSearcher(backend)
	if err != nil {
		return err
	}
	resolver, ok := searcher.(docs.LibraryResolver)
	if !ok {
		return exitErrf(ExitValidation, "docs backend %q does not support library operations", backend)
	}
	matches, err := resolver.ResolveLibrary(cmd.Context(), query, limit)
	if err != nil {
		return upstreamErr(err, "resolve failed")
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(matches)
	}

	for _, m := range matches {
		fmt.Printf("%s  %s  (snippets: %d, trust: %.1f)\n", m.ID, m.Title, m.TotalSnippets, m.TrustScore)
	}
	return nil
}

// runDocsWithLibrary fetches docs for a known library ID. tokens bounds what
// the provider returns; a positive limit additionally caps the result count.
func runDocsWithLibrary(cmd *cobra.Command, query, library string, tokens, limit int, asJSON bool, minimal bool) error {
	backend, _ := cmd.Flags().GetString("backend")
	searcher, err := newDocSearcher(backend)
	if err != nil {
		return err
	}
	resolver, ok := searcher.(docs.LibraryResolver)
	if !ok {
		return exitErrf(ExitValidation, "docs backend %q does not support library operations", backend)
	}
	results, err := resolver.GetDocs(cmd.Context(), library, query, tokens)
	if err != nil {
		return upstreamErr(err, "docs fetch failed")
	}
	results = docs.Truncate(results, limit)

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	printDocsResults(query, backend, library, results, minimal)
	return nil
}

func printDocsResults(query, backend, library string, results []docs.Result, minimal bool) {
	if minimal {
		for _, r := range results {
			snippet := firstLine(r.Snippet)
			fmt.Printf("%s\t%s\t%s\n", r.URL, r.Library, snippet)
		}
		return
	}

	fmt.Println("---")
	fmt.Printf("query: %s\n", query)
	fmt.Printf("backend: %s\n", backend)
	if library != "" {
		fmt.Printf("library: %s\n", library)
	} else if len(results) > 0 && results[0].Library != "" {
		fmt.Printf("library: %s\n", results[0].Library)
	}
	fmt.Printf("result_count: %d\n", len(results))
	fmt.Println("---")
	for _, r := range results {
		label := r.Title
		if r.Breadcrumb != "" {
			label = r.Breadcrumb
		}
		fmt.Printf("[%s]\n", label)
		fmt.Printf("  %s\n", r.Snippet)
		fmt.Printf("  source: %s\n", r.URL)
		fmt.Println()
	}
}

// newDocSearcher resolves the backend via the shared docs.NewFromConfig and
// maps constructor errors to CLI exit codes.
func newDocSearcher(backend string) (docs.Searcher, error) {
	s, err := docs.NewFromConfig(&cfg, backend)
	if err != nil {
		return nil, backendErr(err, docs.ErrUnknownBackend)
	}
	return s, nil
}
