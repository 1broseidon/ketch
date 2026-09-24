package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/spf13/cobra"
)

var codeCmd = &cobra.Command{
	Use:   "code <query>",
	Short: "Search code across open-source repositories",
	Long:  `Search code using ` + code.DescriptionNames(true) + ` (default: the configured backend; grepapp if unset). --lang and --repo filter the same way on every backend; ` + strings.Join(code.QualifierBackends(), " and ") + ` also read their own query qualifiers.`,
	Args:  exitArgs(cobra.MinimumNArgs(1)),
	RunE:  runCode,
}

func init() {
	rootCmd.AddCommand(codeCmd)
	codeCmd.Flags().StringP("backend", "b", cfg.CodeBackend, "code search backend: "+strings.Join(config.AvailableCodeBackends(), ", "))
	codeCmd.Flags().String("lang", "", "language filter (appended to query)")
	codeCmd.Flags().String("repo", "", "search one repository, owner/name (exact on every backend)")
	codeCmd.Flags().Bool("regex", false, "interpret query as a regular expression ("+strings.Join(code.RegexpBackends(), ", ")+")")
	codeCmd.Flags().IntP("limit", "l", cfg.Limit, "max number of results")
	codeCmd.Flags().String("tag", "", "record each result under this tag (see `ketch tag`)")
	codeCmd.PreRunE = validateTagFlag
	codeCmd.Flags().Bool("minimal", false, "one result per line, tab-separated (url/repo/snippet)")
}

func runCode(cmd *cobra.Command, args []string) error {
	query := args[0]
	backend, _ := cmd.Flags().GetString("backend")
	lang, _ := cmd.Flags().GetString("lang")
	repoRef, _ := cmd.Flags().GetString("repo")
	limit, _ := cmd.Flags().GetInt("limit")
	regex, _ := cmd.Flags().GetBool("regex")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	minimal, _ := cmd.Flags().GetBool("minimal")

	repo, err := code.NormalizeRepo(repoRef)
	if err != nil {
		return exitErrf(ExitValidation, "--repo: %w", err)
	}
	searcher, err := newCodeSearcher(backend)
	if err != nil {
		return err
	}

	q := code.Query{
		Term:   query,
		Lang:   lang,
		Repo:   repo,
		Limit:  limit,
		Regexp: regex,
	}
	results, err := searcher.Search(cmd.Context(), q)
	if err != nil {
		return codeSearchErr(err, backend)
	}
	warnCodeSearch(asJSON, backend, q, results)

	tagResults(cmd, codeTaggable(results))

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	if minimal {
		for _, r := range results {
			snippet := firstLine(r.Snippet)
			fmt.Printf("%s\t%s\t%s\n", r.URL, r.Repo, minimalField(snippet))
		}
		return nil
	}

	printCodeResults(q, backend, results)
	return nil
}

// codeSearchErr classifies a failed search. A backend that cannot serve the
// request points at those that can: another backend may index a repository
// this one lacks (exit 3), and regex is backend-specific (exit 2).
func codeSearchErr(err error, backend string) error {
	switch {
	case errors.Is(err, code.ErrRegexpUnsupported):
		// Validation, not precondition: the request is wrong for this
		// backend and no operator action or retry can make it succeed.
		// Mirrors the MCP code tool's [validation] classification.
		return exitErrf(ExitValidation, "backend %q does not support --regex (try -b %s)", backend, strings.Join(code.RegexpBackends(), " or -b "))
	case errors.Is(err, code.ErrRepoNotFound):
		err = fmt.Errorf("%w (try -b %s)", err, strings.Join(code.Alternatives(backend), " or -b "))
	}
	return upstreamErr(err, "code search failed")
}

// warnCodeSearch flags results that may not mean what they appear to: a
// qualifier the backend matched as literal code instead of applying, or an
// empty repo-scoped search on a backend that indexes only some repositories.
func warnCodeSearch(asJSON bool, backend string, q code.Query, results []code.Result) {
	p, _ := code.Lookup(backend)
	if tokens := p.LiteralQualifiers(q.Term); len(tokens) > 0 {
		codeDiagnostic(asJSON, "literal_qualifier", backend, fmt.Sprintf("%s searched %s as literal code, not as a filter: use --repo/--lang, or a backend that reads qualifiers (-b %s)",
			backend, quoteAll(tokens), strings.Join(code.QualifierBackends(), ", -b ")))
	}
	if p.MayLackRepo(q, results) {
		codeDiagnostic(asJSON, "repo_may_be_unindexed", backend, fmt.Sprintf("no results in %s: %s indexes a subset of public repositories and may not have it (try -b %s)",
			q.Repo, backend, strings.Join(code.Alternatives(backend), " or -b ")))
	}
}

// codeDiagnostic reports a warning that leaves the results intact: a warn:
// line, or under --json a {"warning": {...}} object like the tag diagnostics.
func codeDiagnostic(asJSON bool, kind, backend, message string) {
	if asJSON {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"warning": map[string]string{"code": kind, "backend": backend, "message": message}})
		return
	}
	fmt.Fprintf(os.Stderr, "warn: %s\n", message)
}

// quoteAll renders tokens as a space-separated list of quoted strings.
func quoteAll(tokens []string) string {
	quoted := make([]string, len(tokens))
	for i, t := range tokens {
		quoted[i] = strconv.Quote(t)
	}
	return strings.Join(quoted, " ")
}

// printCodeResults writes the default frontmatter + result listing.
func printCodeResults(q code.Query, backend string, results []code.Result) {
	fmt.Println("---")
	fmt.Printf("query: %s\n", q.Term)
	if q.Lang != "" {
		fmt.Printf("lang: %s\n", q.Lang)
	}
	if q.Repo != "" {
		fmt.Printf("repo: %s\n", q.Repo)
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

// codeTaggable maps code hits onto index entries: the location is the title,
// the matching snippet the description.
func codeTaggable(results []code.Result) []taggableResult {
	out := make([]taggableResult, 0, len(results))
	for _, r := range results {
		title := r.Repo
		if r.Path != "" {
			title += " " + r.Path
		}
		if r.Line > 0 {
			title += fmt.Sprintf(":%d", r.Line)
		}
		out = append(out, taggableResult{URL: r.URL, Title: title, Description: r.Snippet})
	}
	return out
}
