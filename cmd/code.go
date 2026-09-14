package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/spf13/cobra"
)

var codeCmd = &cobra.Command{
	Use:   "code <query>",
	Short: "Search code across open-source repositories",
	Long:  `Search code using ` + code.DescriptionNames(true) + ` (default: the configured backend; grepapp if unset). Supports language filtering and per-backend query qualifiers.`,
	Args:  exitArgs(cobra.MinimumNArgs(1)),
	RunE:  runCode,
}

func init() {
	rootCmd.AddCommand(codeCmd)
	codeCmd.Flags().StringP("backend", "b", cfg.CodeBackend, "code search backend: "+strings.Join(config.AvailableCodeBackends(), ", "))
	codeCmd.Flags().String("lang", "", "language filter (appended to query)")
	codeCmd.Flags().Bool("regex", false, "interpret query as a regular expression ("+strings.Join(code.RegexpBackends(), ", ")+")")
	codeCmd.Flags().IntP("limit", "l", cfg.Limit, "max number of results")
	codeCmd.Flags().Bool("minimal", false, "one result per line, tab-separated (url/repo/snippet)")
}

func runCode(cmd *cobra.Command, args []string) error {
	query := args[0]
	backend, _ := cmd.Flags().GetString("backend")
	lang, _ := cmd.Flags().GetString("lang")
	limit, _ := cmd.Flags().GetInt("limit")
	regex, _ := cmd.Flags().GetBool("regex")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	minimal, _ := cmd.Flags().GetBool("minimal")

	searcher, err := newCodeSearcher(backend)
	if err != nil {
		return err
	}

	results, err := searcher.Search(cmd.Context(), code.Query{
		Term:   query,
		Lang:   lang,
		Limit:  limit,
		Regexp: regex,
	})
	if err != nil {
		if errors.Is(err, code.ErrRegexpUnsupported) {
			// Validation, not precondition: the request is wrong for this
			// backend and no operator action or retry can make it succeed.
			// Mirrors the MCP code tool's [validation] classification.
			return exitErrf(ExitValidation, "backend %q does not support --regex (try -b %s)", backend, strings.Join(code.RegexpBackends(), " or -b "))
		}
		return exitErrf(ExitUpstream, "code search failed: %w", err)
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

// newCodeSearcher resolves the backend via the shared code.NewFromConfig and
// maps constructor errors to CLI exit codes.
func newCodeSearcher(backend string) (code.Searcher, error) {
	s, err := code.NewFromConfig(&cfg, backend)
	if err != nil {
		return nil, backendErr(err, code.ErrUnknownBackend)
	}
	return s, nil
}
