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
	Long:  `Search code using Sourcegraph (default) or GitHub Code Search. Supports language filtering and per-backend query qualifiers.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runCode,
}

func init() {
	rootCmd.AddCommand(codeCmd)
	codeCmd.Flags().StringP("backend", "b", cfg.CodeBackend, "code search backend: "+strings.Join(config.AvailableCodeBackends(), ", "))
	codeCmd.Flags().String("lang", "", "language filter (appended to query)")
	codeCmd.Flags().IntP("limit", "l", cfg.Limit, "max number of results")
	codeCmd.Flags().Bool("minimal", false, "one result per line, tab-separated (url/repo/snippet)")
}

func runCode(cmd *cobra.Command, args []string) error {
	query := args[0]
	backend, _ := cmd.Flags().GetString("backend")
	lang, _ := cmd.Flags().GetString("lang")
	limit, _ := cmd.Flags().GetInt("limit")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	minimal, _ := cmd.Flags().GetBool("minimal")

	searcher, err := newCodeSearcher(backend)
	if err != nil {
		return err
	}

	results, err := searcher.Search(cmd.Context(), query, lang, limit)
	if err != nil {
		return fmt.Errorf("code search failed: %w", err)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	if minimal {
		for _, r := range results {
			snippet := firstLine(r.Snippet)
			fmt.Printf("%s\t%s\t%s\n", r.URL, r.Repo, snippet)
		}
		return nil
	}

	fmt.Println("---")
	fmt.Printf("query: %s\n", query)
	if lang != "" {
		fmt.Printf("lang: %s\n", lang)
	}
	fmt.Printf("backend: %s\n", backend)
	fmt.Printf("result_count: %d\n", len(results))
	fmt.Println("---")
	for _, r := range results {
		header := r.Repo + "  " + r.Path
		if r.Line > 0 {
			header = fmt.Sprintf("%s  (line %d)", header, r.Line)
		}
		if r.Stars > 0 {
			header = fmt.Sprintf("%s  ★ %d", header, r.Stars)
		}
		fmt.Println(header)
		if r.Snippet != "" {
			fmt.Printf("  %s\n", r.Snippet)
		}
		fmt.Printf("  %s\n", r.URL)
		fmt.Println()
	}
	return nil
}

func newCodeSearcher(backend string) (code.Searcher, error) {
	switch backend {
	case "sourcegraph":
		return code.NewSourcegraph(cfg.SourcegraphURL), nil
	case "github":
		token, source := cfg.ResolveGithubToken()
		if token == "" {
			return nil, fmt.Errorf(`github code search: no token found.
  - explicit:   ketch config set github_token <token>
  - env var:    export GITHUB_TOKEN=<token>
  - or run:     gh auth login`)
		}
		_ = source
		return code.NewGitHub(token), nil
	default:
		return nil, fmt.Errorf("unknown code backend: %s", backend)
	}
}
