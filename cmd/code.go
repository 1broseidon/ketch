package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/ketch/internal/code"
	"github.com/1broseidon/ketch/internal/config"
	"github.com/spf13/cobra"
)

var codeCmd = &cobra.Command{
	Use:   "code <query>",
	Short: "Search code across open-source repositories",
	Long:  `Search code using Sourcegraph. Supports language filtering and Sourcegraph query qualifiers.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runCode,
}

func init() {
	rootCmd.AddCommand(codeCmd)
	codeCmd.Flags().StringP("backend", "b", cfg.CodeBackend, "code search backend: "+strings.Join(config.AvailableCodeBackends(), ", "))
	codeCmd.Flags().String("lang", "", "language filter (appended to query)")
	codeCmd.Flags().IntP("limit", "l", cfg.Limit, "max number of results")
}

func runCode(cmd *cobra.Command, args []string) error {
	query := args[0]
	backend, _ := cmd.Flags().GetString("backend")
	lang, _ := cmd.Flags().GetString("lang")
	limit, _ := cmd.Flags().GetInt("limit")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	if lang != "" {
		query += " lang:" + lang
	}

	searcher, err := newCodeSearcher(backend)
	if err != nil {
		return err
	}

	results, err := searcher.Search(query, limit)
	if err != nil {
		return fmt.Errorf("code search failed: %w", err)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	fmt.Println("---")
	fmt.Printf("query: %s\n", query)
	fmt.Printf("backend: %s\n", backend)
	fmt.Printf("result_count: %d\n", len(results))
	fmt.Println("---")
	for _, r := range results {
		fmt.Printf("%s  %s  (line %d)\n", r.Repo, r.Path, r.Line)
		fmt.Printf("  %s\n", r.Snippet)
		fmt.Println()
	}
	return nil
}

func newCodeSearcher(backend string) (code.Searcher, error) {
	switch backend {
	case "sourcegraph":
		return code.NewSourcegraph(cfg.SourcegraphURL), nil
	default:
		return nil, fmt.Errorf("unknown code backend: %s", backend)
	}
}
