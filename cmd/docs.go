package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/ketch/internal/config"
	"github.com/1broseidon/ketch/internal/docs"
	"github.com/spf13/cobra"
)

var docsCmd = &cobra.Command{
	Use:   "docs <query>",
	Short: "Search library documentation",
	Long:  `Search library documentation using Context7 or a local FTS5 backend. Supports direct library ID lookup and library name resolution.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runDocs,
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.Flags().StringP("backend", "b", cfg.DocsBackend, "docs backend: "+strings.Join(config.AvailableDocBackends(), ", "))
	docsCmd.Flags().String("library", "", "Context7 library ID (skip resolve step)")
	docsCmd.Flags().Int("tokens", 4000, "Context7 token budget")
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
		return runDocsResolve(cmd, query, asJSON)
	}

	if library != "" && backend == "context7" {
		return runDocsWithLibrary(cmd, query, library, tokens, asJSON, minimal)
	}

	searcher, err := newDocSearcher(backend)
	if err != nil {
		return err
	}

	results, err := searcher.Search(cmd.Context(), query, limit)
	if err != nil {
		return fmt.Errorf("docs search failed: %w", err)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	printDocsResults(query, backend, "", results, minimal)
	return nil
}

func runDocsResolve(cmd *cobra.Command, query string, asJSON bool) error {
	if cfg.Context7APIKey == "" {
		return fmt.Errorf("context7: API key not set (get one then: ketch config set context7_api_key <key>)")
	}

	c7 := docs.NewContext7(cfg.Context7APIKey)
	matches, err := c7.ResolveLibrary(cmd.Context(), query)
	if err != nil {
		return fmt.Errorf("resolve failed: %w", err)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(matches)
	}

	for _, m := range matches {
		fmt.Printf("%s  %s  (snippets: %d)\n", m.ID, m.Name, m.CodeSnippets)
	}
	return nil
}

func runDocsWithLibrary(cmd *cobra.Command, query, library string, tokens int, asJSON bool, minimal bool) error {
	if cfg.Context7APIKey == "" {
		return fmt.Errorf("context7: API key not set (get one then: ketch config set context7_api_key <key>)")
	}

	c7 := docs.NewContext7(cfg.Context7APIKey)
	results, err := c7.GetDocs(cmd.Context(), library, query, tokens)
	if err != nil {
		return fmt.Errorf("docs fetch failed: %w", err)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	printDocsResults(query, "context7", library, results, minimal)
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

func newDocSearcher(backend string) (docs.Searcher, error) {
	switch backend {
	case "context7":
		if cfg.Context7APIKey == "" {
			return nil, fmt.Errorf("context7: API key not set (get one then: ketch config set context7_api_key <key>)")
		}
		return docs.NewContext7(cfg.Context7APIKey), nil
	case "local":
		return docs.NewFTS5Local(), nil
	default:
		return nil, fmt.Errorf("unknown docs backend: %s", backend)
	}
}
