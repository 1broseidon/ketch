package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/docsfetch"
	"github.com/1broseidon/ketch/scrape"
	"github.com/spf13/cobra"
)

var docsCmd = &cobra.Command{
	Use:   "docs <query>",
	Short: "Search library documentation",
	Long: `Search library documentation using ` + strings.Join(docs.ProviderNames(), ", ") + ` (default: the configured backend). Supports direct library ID lookup and library name resolution. ` +
		docs.WithLibraryHelp("A library is a library ID from --resolve") + `. Resolve a name once and pass the ID it returns as --library: a name is resolved again on every call. Context7 needs no key: without one it uses Context7's anonymous quota (about 200 requests a month per IP), and a key lifts it. The auto backend tries web first and asks Context7 only when web identifies no library, finds several, or fails; Context7's pick must be a library the query names, and for an ambiguous name one of web's candidates. Output names the provider that answered. A local FTS5 backend is planned but not yet implemented.`,
	Args: exitArgs(cobra.MinimumNArgs(1)),
	RunE: runDocs,
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.Flags().StringP("backend", "b", cfg.DocsBackend, "docs backend: "+strings.Join(config.AvailableDocBackends(), ", "))
	docsCmd.Flags().String("library", "", docs.WithLibraryHelp(strings.Join(docs.LibraryProviderNames(), ", ")+" library ID, skipping the resolve step"))
	docsCmd.Flags().Int("tokens", 4000, strings.Join(docs.LibraryProviderNames(), ", ")+" token budget")
	docsCmd.Flags().IntP("limit", "l", cfg.Limit, "max number of results")
	docsCmd.Flags().Bool("resolve", false, docs.WithLibraryHelp("resolve the query as a library name to "+strings.Join(docs.LibraryProviderNames(), ", ")+" library IDs instead of searching"))
	docsCmd.Flags().String("tag", "", "record each result under this tag (see `ketch tag`)")
	docsCmd.PreRunE = validateTagFlag
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
		// Library matches carry IDs, not URLs: there is nothing to bookmark,
		// so refuse rather than accept a tag and record nothing under it.
		if tag, _ := cmd.Flags().GetString("tag"); tag != "" {
			return exitErrf(ExitValidation, "--tag cannot be combined with --resolve: library matches have no URL to bookmark")
		}
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

	searcher, fetcher, err := newDocSearcher(cmd, backend)
	if err != nil {
		return err
	}
	defer fetcher.Close()

	var o docs.Outcome
	if selecting, ok := searcher.(docs.SelectingSearcher); ok {
		o = selecting.SearchSelect(cmd.Context(), query, limit)
	} else {
		results, err := searcher.Search(cmd.Context(), query, limit)
		o = docs.Answer(backend, results, err)
	}
	backend = servedDocsBackend(backend, o)
	if err := fetcher.classify(o.Err(), "docs search failed"); err != nil {
		return err
	}

	tagResults(cmd, docsTaggable(o.Results))

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(o.Results)
	}

	printDocsResults(query, backend, "", o, minimal)
	return nil
}

func runDocsResolve(cmd *cobra.Command, query string, limit int, asJSON bool) error {
	backend, _ := cmd.Flags().GetString("backend")
	backend = docs.ResolveBackend(backend)
	searcher, fetcher, err := newDocSearcher(cmd, backend)
	if err != nil {
		return err
	}
	defer fetcher.Close()
	resolver, ok := searcher.(docs.LibraryResolver)
	if !ok {
		return exitErrf(ExitValidation, "docs backend %q does not support library operations", backend)
	}
	var o docs.Outcome
	if selecting, ok := searcher.(docs.SelectingSearcher); ok {
		o = selecting.ResolveSelect(cmd.Context(), query, limit)
	} else {
		matches, err := resolver.ResolveLibrary(cmd.Context(), query, limit)
		if err == nil && len(matches) == 0 {
			err = fmt.Errorf("library %q %w", query, docs.ErrNotFound)
		}
		o = docs.Resolved(backend, matches, err)
	}
	servedDocsBackend(backend, o)
	if err := fetcher.classify(o.Err(), "resolve failed"); err != nil {
		return err
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(o.Matches)
	}

	fmt.Print(formatLibraryMatches(o.Matches))
	return nil
}

// formatLibraryMatches is --resolve's text output, one block per match. It
// goes by the fields a match carries, never by backend. A match with no
// evidence, version or package specs (every Context7 match) is the one
// line resolve has always printed, byte for byte. Any other says what it
// knows: its trust, version and versions on the ID line, then its package
// specs and evidence, each on an indented line when it has them. So a docs
// URL with no evidence still shows the site's versions.
func formatLibraryMatches(matches []docs.LibraryMatch) string {
	var b strings.Builder
	for _, m := range matches {
		if len(m.Evidence) == 0 && m.Version == "" && len(m.Packages) == 0 {
			fmt.Fprintf(&b, "%s  %s  (snippets: %d, trust: %.1f)\n", m.ID, m.Title, m.TotalSnippets, m.TrustScore)
			continue
		}
		var about []string
		if m.TotalSnippets > 0 {
			about = append(about, fmt.Sprintf("snippets: %d", m.TotalSnippets))
		}
		about = append(about, fmt.Sprintf("trust: %.1f", m.TrustScore))
		if m.Version != "" {
			about = append(about, "version: "+m.Version)
		}
		if len(m.Versions) > 0 {
			// Space-separated: the labels are a list inside a comma list.
			about = append(about, "versions: "+strings.Join(m.Versions, " "))
		}
		b.WriteString(m.ID + "  " + m.Title + "  (" + strings.Join(about, ", ") + ")\n")
		if len(m.Packages) > 0 {
			b.WriteString("  packages: " + strings.Join(m.Packages, ", ") + "\n")
		}
		if len(m.Evidence) > 0 {
			b.WriteString("  evidence: " + strings.Join(m.Evidence, "; ") + "\n")
		}
	}
	return b.String()
}

// runDocsWithLibrary fetches docs for a known library ID. tokens bounds what
// the provider returns; a positive limit additionally caps the result count.
func runDocsWithLibrary(cmd *cobra.Command, query, library string, tokens, limit int, asJSON bool, minimal bool) error {
	backend, _ := cmd.Flags().GetString("backend")
	searcher, fetcher, err := newDocSearcher(cmd, backend)
	if err != nil {
		return err
	}
	defer fetcher.Close()
	resolver, ok := searcher.(docs.LibraryResolver)
	if !ok {
		return exitErrf(ExitValidation, "docs backend %q does not support library operations", backend)
	}
	var o docs.Outcome
	if selecting, ok := searcher.(docs.SelectingSearcher); ok {
		o = selecting.GetDocsSelect(cmd.Context(), library, query, tokens)
	} else {
		results, err := resolver.GetDocs(cmd.Context(), library, query, tokens)
		o = docs.Answer(backend, results, err)
	}
	backend = servedDocsBackend(backend, o)
	if err := fetcher.classify(o.Err(), "docs fetch failed"); err != nil {
		return err
	}
	o.Results = docs.Truncate(o.Results, limit)

	tagResults(cmd, docsTaggable(o.Results))

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(o.Results)
	}

	printDocsResults(query, backend, library, o, minimal)
	return nil
}

// servedDocsBackend reports who answered: the backend that answered
// replaces "auto" as the backend the output names (each result's source
// says the same), and each backend it fell through is warned about on
// stderr with its status and reason, as search's auto does. With no answer
// it keeps backend and warns nothing: the error names every failure.
func servedDocsBackend(backend string, o docs.Outcome) string {
	if o.Backend == "" {
		return backend
	}
	for _, f := range o.Failures() {
		fmt.Fprintf(os.Stderr, "warn: %s %s (%s): %s\n", f.Backend, f.Status, f.Reason, f.Error)
	}
	return o.Backend
}

func printDocsResults(query, backend, library string, o docs.Outcome, minimal bool) {
	results := o.Results
	if minimal {
		for _, r := range results {
			snippet := firstLine(r.Snippet)
			fmt.Printf("%s\t%s\t%s\n", r.URL, minimalField(r.Library), minimalField(snippet))
		}
		return
	}

	fmt.Println("---")
	fmt.Printf("query: %s\n", query)
	fmt.Printf("backend: %s\n", backend)
	// The results' library is the ID the backend answered from: the one a
	// name or spec passed as --library resolved to, or the one a bare query
	// picked. Without results, the flag is all there is.
	if len(results) > 0 && results[0].Library != "" {
		fmt.Printf("library: %s\n", results[0].Library)
	} else if library != "" {
		fmt.Printf("library: %s\n", library)
	}
	if o.Status == docs.Partial {
		fmt.Printf("status: partial (%s)\n", o.Reason)
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
// maps constructor errors to CLI exit codes. The backend gets a lazy page
// fetcher; the caller closes it when the command ends and classifies the
// backend's calls through it.
func newDocSearcher(cmd *cobra.Command, backend string) (docs.Searcher, *docsFetcher, error) {
	fetcher := &docsFetcher{newScraper: func() (*scrape.Scraper, error) { return newDocsScraper(cmd) }}
	s, err := docs.NewFromConfig(&cfg, backend, docs.Deps{Fetcher: fetcher})
	if err != nil {
		return nil, nil, backendErr(err)
	}
	return s, fetcher, nil
}

// newDocsScraper builds the scraper behind a docs run's page fetches; tests
// swap it to observe whether a run builds one at all.
var newDocsScraper = newScraper

// errDocsFetcherClosed reports a fetch after the command released its
// scraper.
var errDocsFetcherClosed = errors.New("docs page fetcher used after close")

// docsFetcher is the CLI's docs.Fetcher. It builds the scraper and page cache
// on the first fetch, so a backend that answers from an API (Context7) never
// constructs a scraper, and Close releases whatever was built.
type docsFetcher struct {
	newScraper func() (*scrape.Scraper, error)

	mu      sync.Mutex
	closed  bool
	err     error // construction failure; sticky, the same for every fetch
	scraper *scrape.Scraper
	pages   *cache.Cache
	fetcher *docsfetch.Fetcher
}

func (f *docsFetcher) FetchRaw(ctx context.Context, url string) (docs.RawContent, error) {
	inner, err := f.get()
	if err != nil {
		return docs.RawContent{}, err
	}
	return inner.FetchRaw(ctx, url)
}

func (f *docsFetcher) FetchPage(ctx context.Context, url string) (docs.PageContent, error) {
	inner, err := f.get()
	if err != nil {
		return docs.PageContent{}, err
	}
	return inner.FetchPage(ctx, url)
}

// GetResolution reads a cached web docs resolution (docs.ResolutionCache)
// through the fetcher, building it as a fetch would.
func (f *docsFetcher) GetResolution(key string) (string, bool) {
	inner, err := f.get()
	if err != nil {
		return "", false
	}
	return inner.GetResolution(key)
}

// PutResolution caches a web docs resolution through the fetcher.
func (f *docsFetcher) PutResolution(key, value string) {
	if inner, err := f.get(); err == nil {
		inner.PutResolution(key, value)
	}
}

// get builds the fetcher on first use. Construction does no network I/O, so
// it runs under the lock and concurrent first fetches share one scraper.
func (f *docsFetcher) get() (*docsfetch.Fetcher, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.closed:
		return nil, errDocsFetcherClosed
	case f.fetcher != nil || f.err != nil:
		return f.fetcher, f.err
	}
	s, err := f.newScraper()
	if err != nil {
		f.err = err
		return nil, err
	}
	f.scraper = s
	f.pages = newPageCache(false)
	f.fetcher = docsfetch.New(f.scraper, f.pages)
	return f.fetcher, nil
}

// classify maps the outcome of a backend call to a CLI error. A scraper that
// could not be built (invalid url_rewrites, an unreadable cookie_file) is the
// run's real failure — newScraper's precondition, exit 5 — however the
// backend reported it, even if it swallowed the fetch error and answered
// empty. Otherwise a failure goes through upstreamErr. It never builds the
// scraper itself.
func (f *docsFetcher) classify(err error, msg string) error {
	f.mu.Lock()
	buildErr := f.err
	f.mu.Unlock()
	if buildErr != nil {
		return buildErr
	}
	if err != nil {
		return upstreamErr(err, "%s", msg)
	}
	return nil
}

// Close releases the scraper and page cache if a fetch built them; it never
// builds them. Later fetches fail with errDocsFetcherClosed.
func (f *docsFetcher) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scraper != nil {
		f.scraper.Close()
		f.pages.Close()
	}
	f.closed = true
	f.scraper, f.pages, f.fetcher = nil, nil, nil
}

// docsTaggable maps docs hits onto index entries: library and heading make the
// title, the documentation chunk the description.
func docsTaggable(results []docs.Result) []taggableResult {
	out := make([]taggableResult, 0, len(results))
	for _, r := range results {
		title := r.Title
		if r.Library != "" {
			title = r.Library + " " + title
		}
		out = append(out, taggableResult{URL: r.URL, Title: title, Description: r.Snippet})
	}
	return out
}
