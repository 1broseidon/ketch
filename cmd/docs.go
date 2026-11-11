package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
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
	Long: `Search library documentation. The default backend, search, answers from the docs themselves: a library indexed with ketch docs index answers offline from the local index; otherwise ketch searches the web for the question, picks the documentation site the results agree on, reads the matching pages and keeps them in the local index, so a repeated question answers offline. ` +
		`The answer's frontmatter says what it read (index: full or capped, its pages and build date; or source: search and the pages a search read), its status, the question's rare terms as written (identifiers it writes, words in under 1% of the library's sections or in at most 25 of them) with the number of sections holding each (terms), and those no section holds (not_found). Status is found (evidence matching the question, not a promise that it answers); partial when a term is in not_found or the library holds only pages searches read; refused, exit 3, when no distinctive term is in the docs: the body is empty and not_found is the answer. ` +
		`The body is the pieces of sections that name the question (paragraphs, list items, code blocks, table rows under their header), verbatim, grouped by section: a page's groups under "## <page title>", each under its heading path, ending with its source url#anchor, marked "(cut, N of M tokens)" with how much of its section it shows when the group was cut; a section longer than a page (` + strconv.Itoa(docs.AnswerPageTokens) + ` tokens) is never printed whole: its group shows its best pieces up to a page, in page order; a page's groups print in rank order, the page with the best group first; about 1,800 tokens in all, frontmatter included, or --tokens; --limit caps the groups. An answer with a group cut ends with one line saying how to read it in full: --section returns a section from the local index (no ranking, no network), whole when it fits --tokens (default 4000), else a page of that size at a time, broken at a block boundary: the output says page N of M and its last line is the command for the next page (--page N); a page URL with no anchor returns the page's sections in order, paged alike. When a term in terms is the first cell of a table row (an option's row) in a section the answer does not print, a last line names that section, for --section: "defined in: <term> → <url#anchor>" (at most three). --json prints the frontmatter's fields and the results as one object. ` +
		docs.WithLibraryHelp("--library names the library") + `.

-b context7 (or ketch config set docs_backend context7 to make it the default) uses Context7's curated snippets instead. Context7 needs no key: without one it uses Context7's anonymous quota (about 200 requests a month per IP), and a key lifts it. --resolve lists a name's library IDs (Context7) or docs roots (search); pass one as --library.`,
	Args: exitArgs(docsArgs),
	RunE: traceDocs(runDocs),
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.Flags().StringP("backend", "b", cfg.DocsBackend, "docs backend: "+strings.Join(config.AvailableDocBackends(), ", "))
	docsCmd.Flags().String("library", "", docs.WithLibraryHelp("the library to answer from"))
	docsCmd.Flags().Int("tokens", 4000, "token budget (search: 1,800 unless set, frontmatter included; context7: 4000; --section: the page size of the section returned, 4000)")
	docsCmd.Flags().IntP("limit", "l", cfg.Limit, "max number of results")
	docsCmd.Flags().Bool("resolve", false, docs.WithLibraryHelp("resolve the query as a library name instead of searching: library IDs (context7) or docs roots (search)"))
	docsCmd.Flags().String("tag", "", "record each result under this tag (see `ketch tag`)")
	docsCmd.PreRunE = validateTagFlag
	docsCmd.Flags().Bool("minimal", false, "one result per line, tab-separated (url/library/snippet)")
	docsCmd.Flags().String("section", "", "return one section from the local index, given as the url#anchor a group's source line carries (a page URL alone returns its sections in order; no query, no ranking, no network): whole when it fits --tokens (default 4000), else one page of that size at a time (--page)")
	docsCmd.Flags().Int("page", 0, "with --section: the page of a section longer than --tokens to return (default 1; the output says page N of M and gives the command for the next)")
	docsCmd.Flags().String("trace", "", "write how the answer was made to this file as JSON (docs.Trace; for QA)")
	_ = docsCmd.Flags().MarkHidden("trace")
}

// traceDocs runs a docs command and, with the hidden --trace <file>, writes
// how its answer was made (docs.Trace) to the file, whatever the outcome.
// Without it the command runs as it is.
func traceDocs(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("trace")
		if path == "" {
			return run(cmd, args)
		}
		ctx := cmd.Context()
		if ctx == nil { // a command run without Execute
			ctx = context.Background()
		}
		t := docs.NewTrace(strings.Join(args, " "))
		cmd.SetContext(docs.WithTrace(ctx, t))
		err := run(cmd, args)
		if werr := t.Write(path, err); werr != nil {
			fmt.Fprintf(os.Stderr, "warning: --trace: %v\n", werr)
		}
		return err
	}
}

// docsStdout is where a docs command prints: stdout, also copied into its
// trace when --trace is set.
func docsStdout(cmd *cobra.Command) io.Writer {
	ctx := cmd.Context()
	if ctx == nil {
		return os.Stdout
	}
	return docs.TraceFrom(ctx).Stdout(os.Stdout)
}

// docsArgs wants a query, unless --section names what to fetch.
func docsArgs(cmd *cobra.Command, args []string) error {
	if section, _ := cmd.Flags().GetString("section"); section != "" {
		return nil
	}
	return cobra.MinimumNArgs(1)(cmd, args)
}

func runDocs(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	backend := docsBackendFlag(cmd)
	library, _ := cmd.Flags().GetString("library")
	tokens, _ := cmd.Flags().GetInt("tokens")
	limit, _ := cmd.Flags().GetInt("limit")
	resolve, _ := cmd.Flags().GetBool("resolve")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	minimal, _ := cmd.Flags().GetBool("minimal")
	hint := docsHint(backend, asJSON || minimal)
	cmd.SetContext(withLocalDocsOptions(cmd, tokens, limit, hint))

	page, _ := cmd.Flags().GetInt("page")
	if section, _ := cmd.Flags().GetString("section"); section != "" {
		if resolve {
			return exitErrf(ExitValidation, "--section cannot be combined with --resolve")
		}
		if cmd.Flags().Changed("page") && page < 1 {
			return exitErrf(ExitValidation, "--page is a page number from 1 (got %d)", page)
		}
		return runDocsSection(cmd, section, library, tokens, page, asJSON, minimal)
	}
	if cmd.Flags().Changed("page") {
		return exitErrf(ExitValidation, "--page needs --section: it picks a page of the section returned")
	}

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
		return runDocsWithLibrary(cmd, query, library, tokens, limit, asJSON, minimal, hint)
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
	docs.TraceFrom(cmd.Context()).SetOutcome(backend, o)
	return finishDocs(cmd, fetcher, query, backend, "", o, asJSON, minimal, hint, "docs search failed")
}

// finishDocs prints an answer, or the refusal of a question none of whose
// distinctive terms the docs hold (its frontmatter, whose not_found is the
// answer, then the error: exit 3), and returns the error the command
// exits with.
func finishDocs(cmd *cobra.Command, fetcher *docsFetcher, query, backend, library string, o docs.Outcome, asJSON, minimal bool, hint docs.HintStyle, failMsg string) error {
	err := fetcher.classify(o.Err(), failMsg)
	if err != nil && !termsRefusal(o) {
		return err
	}
	if err == nil {
		tagResults(cmd, docsTaggable(o.Results))
	}
	w := docsStdout(cmd)
	switch {
	case asJSON:
		if jerr := json.NewEncoder(w).Encode(docs.AnswerJSON{Frontmatter: docs.NewFrontmatter(query, backend, library, o), Results: o.Results}); jerr != nil {
			return jerr
		}
	default:
		printDocsResults(w, query, backend, library, o, minimal, hint)
	}
	return err
}

// termsRefusal reports the local index's refusal of a question none of
// whose distinctive terms the docs hold: it still prints its frontmatter.
func termsRefusal(o docs.Outcome) bool {
	return o.Status == docs.Refused && o.Index != nil
}

// docsHint is how a text answer points to the full section of a result
// it cut: search's answers in text output; --json and --minimal carry
// results only (each result's section field is the reference).
func docsHint(backend string, bare bool) docs.HintStyle {
	if backend == docs.SearchBackend && !bare {
		return docs.HintCLI
	}
	return docs.HintNone
}

// docsBackendFlag is the backend --backend names, a legacy name read as
// its replacement (docs.CanonicalBackend).
func docsBackendFlag(cmd *cobra.Command) string {
	backend, _ := cmd.Flags().GetString("backend")
	return docs.CanonicalBackend(backend)
}

// runDocsSection returns one section of the local index in full: the
// follow-up to an answer that cut it short. The section is the
// url#anchor a result's source carries; --library names the indexed
// library, or the URL does. It reads the index only: no network, no
// ranking. A section the index lacks is not_found (exit 3). The index
// belongs to search, whichever backend is the configured default; only an
// explicit -b another backend is refused.
func runDocsSection(cmd *cobra.Command, section, library string, tokens, page int, asJSON, minimal bool) error {
	if backend := docsBackendFlag(cmd); cmd.Flags().Changed("backend") && backend != docs.SearchBackend {
		return exitErrf(ExitValidation, "--section reads the local docs index, which the %s backend does not use (drop -b %s)", backend, backend)
	}
	searcher, fetcher, err := newDocSearcher(cmd, docs.SearchBackend)
	if err != nil {
		return err
	}
	defer fetcher.Close()
	sections, ok := searcher.(docs.SectionFetcher)
	if !ok {
		return exitErrf(ExitValidation, "docs backend %q does not support --section", docs.SearchBackend)
	}
	if !cmd.Flags().Changed("tokens") {
		tokens = 0 // the default page size, which the next page's command need not name
	}
	o := sections.SectionSelect(cmd.Context(), library, section, tokens, page)
	backend := servedDocsBackend(docs.SearchBackend, o)
	docs.TraceFrom(cmd.Context()).SetOutcome(backend, o)
	if err := fetcher.classify(o.Err(), "section fetch failed"); err != nil {
		return err
	}
	tagResults(cmd, docsTaggable(o.Results))
	if asJSON {
		noteOnStderr(o)
		return json.NewEncoder(docsStdout(cmd)).Encode(o.Results)
	}
	printDocsResults(docsStdout(cmd), "", backend, library, o, minimal, docs.HintNone)
	return nil
}

// withLocalDocsOptions carries the budget and limit the caller chose (not
// the flags' defaults, which are Context7's), so search picks its own
// defaults, and how its answer points to the full section of a
// result it cut.
func withLocalDocsOptions(cmd *cobra.Command, tokens, limit int, hint docs.HintStyle) context.Context {
	o := docs.LocalOptions{Hint: hint}
	if cmd.Flags().Changed("tokens") {
		o.Tokens = tokens
	}
	if cmd.Flags().Changed("limit") {
		o.Limit = limit
	}
	ctx := cmd.Context()
	if ctx == nil { // a command run without Execute
		ctx = context.Background()
	}
	return docs.WithLocalOptions(ctx, o)
}

func runDocsResolve(cmd *cobra.Command, query string, limit int, asJSON bool) error {
	backend := docs.ResolveBackend(docsBackendFlag(cmd))
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
		return json.NewEncoder(docsStdout(cmd)).Encode(o.Matches)
	}

	_, _ = fmt.Fprint(docsStdout(cmd), formatLibraryMatches(o.Matches))
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
func runDocsWithLibrary(cmd *cobra.Command, query, library string, tokens, limit int, asJSON, minimal bool, hint docs.HintStyle) error {
	backend := docsBackendFlag(cmd)
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
	docs.TraceFrom(cmd.Context()).SetOutcome(backend, o)
	o.Results = docs.Truncate(o.Results, limit)
	return finishDocs(cmd, fetcher, query, backend, library, o, asJSON, minimal, hint, "docs fetch failed")
}

// servedDocsBackend is the backend the output names: the one that
// answered, else the one asked.
func servedDocsBackend(backend string, o docs.Outcome) string {
	if o.Backend == "" {
		return backend
	}
	return o.Backend
}

// noteOnStderr says an answer's note (a partial answer's, or the other
// indexed versions of a library) where output that carries results only
// (--minimal, and --section's --json) cannot, and under --minimal the
// question's terms the docs hold nowhere.
func noteOnStderr(o docs.Outcome) {
	if o.Note != "" {
		fmt.Fprintf(os.Stderr, "note: %s\n", o.Note)
	}
	if len(o.NotFound) > 0 {
		fmt.Fprintf(os.Stderr, "not_found: %s\n", strings.Join(o.NotFound, ", "))
	}
}

// printDocsResults prints the results to w: an answer from the local
// index as its frontmatter and its groups under their pages' titles
// (docs.RenderAnswer), a section fetch's sections as Markdown blocks,
// Context7's snippets as labelled blocks.
func printDocsResults(w io.Writer, query, backend, library string, o docs.Outcome, minimal bool, hint docs.HintStyle) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	results := o.Results
	if minimal {
		noteOnStderr(o)
		for _, r := range results {
			snippet := firstLine(r.Snippet)
			p("%s\t%s\t%s\n", r.URL, minimalField(r.Library), minimalField(snippet))
		}
		return
	}

	if o.Index != nil {
		// An answer from the local index: the frontmatter carries the
		// verdict, the body the evidence, a page's groups under its title.
		p("%s", docs.NewFrontmatter(query, backend, library, o).Render())
		p("%s", docs.RenderAnswer(results, hint))
		p("%s", docs.DefinedInLines(o.DefinedIn))
		return
	}
	printDocsHeader(w, query, backend, library, o)
	p("result_count: %d\n", len(results))
	p("---\n")
	if backend == docs.SearchBackend {
		// Search results read as Markdown: heading path, the section's text
		// with its code, source; a page of a section fetch ends with the
		// command for the next.
		for _, r := range results {
			p("%s\n", docs.RenderResult(r, hint))
			if next := docs.NextPageLine(r); next != "" {
				p("%s\n", next)
			}
		}
		return
	}
	for _, r := range results {
		label := r.Title
		if r.Breadcrumb != "" {
			label = r.Breadcrumb
		}
		p("[%s]\n", label)
		p("  %s\n", r.Snippet)
		p("  source: %s\n", r.URL)
		p("\n")
	}
}

// printDocsHeader prints the frontmatter before the results to w, up to
// the result count.
func printDocsHeader(w io.Writer, query, backend, library string, o docs.Outcome) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	results := o.Results
	p("---\n")
	if query != "" {
		p("query: %s\n", query)
	}
	p("backend: %s\n", backend)
	// The results' library is the ID the backend answered from: the one a
	// name or spec passed as --library resolved to, or the one a bare query
	// picked. Without results, the flag is all there is.
	if len(results) > 0 && results[0].Library != "" {
		p("library: %s\n", results[0].Library)
	} else if library != "" {
		p("library: %s\n", library)
	}
	if backend == docs.SearchBackend && len(results) > 0 && results[0].Version != "" {
		p("version: %s\n", results[0].Version)
	}
	if o.Status == docs.Partial {
		p("status: partial (%s)\n", o.Reason)
	}
	if o.Note != "" {
		p("note: %s\n", o.Note)
	}
	if len(results) == 1 {
		if line := docs.PageLine(results[0]); line != "" {
			p("%s\n", line)
		}
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
	// warnings are the scraper's warnings about single pages, printed on
	// stderr as one line per kind, with a count, when the command ends.
	warnings pageWarnings
	stderr   io.Writer // where Close prints them (os.Stderr when nil)
}

// pageWarnings counts the warnings a docs command's page fetches raise,
// by kind, keeping the first of each: a search that reads two dozen
// JS-rendered pages says so once.
type pageWarnings struct {
	mu    sync.Mutex
	kinds []string // in the order first seen
	count map[string]int
	first map[string]scrape.PageWarning
}

func (w *pageWarnings) add(pw scrape.PageWarning) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.count == nil {
		w.count, w.first = map[string]int{}, map[string]scrape.PageWarning{}
	}
	if w.count[pw.Kind] == 0 {
		w.kinds = append(w.kinds, pw.Kind)
		w.first[pw.Kind] = pw
	}
	w.count[pw.Kind]++
}

// flush prints one line per kind of warning to out: a single warning as
// the scraper words it, several with their count and the first.
func (w *pageWarnings) flush(out io.Writer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, k := range w.kinds {
		n, first := w.count[k], w.first[k]
		if n == 1 {
			_, _ = fmt.Fprintln(out, first.String())
			continue
		}
		var line string
		switch k {
		case scrape.WarnJSRendered:
			line = fmt.Sprintf("warn: %d pages appear JS-rendered (first: %s); configure browser for full content", n, first.URL)
		case scrape.WarnBrowserFailed:
			line = fmt.Sprintf("warn: browser fallback failed for %d pages (first: %s: %v)", n, first.URL, first.Err)
		case scrape.WarnBrowser403:
			line = fmt.Sprintf("notice: HTTP 403 for %d pages (first: %s); fetched with the browser", n, first.URL)
		case scrape.WarnBrowser403Failed:
			line = fmt.Sprintf("warn: browser retry after HTTP 403 failed for %d pages (first: %s: %v)", n, first.URL, first.Err)
		default:
			line = fmt.Sprintf("warn: %d pages: %s (first: %s)", n, k, first.URL)
		}
		_, _ = fmt.Fprintln(out, line)
	}
	w.kinds, w.count, w.first = nil, nil, nil
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
	s.OnPageWarning(f.warnings.add)
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
// builds them. Later fetches fail with errDocsFetcherClosed. The fetches'
// warnings about single pages go to stderr, one line per kind.
func (f *docsFetcher) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scraper != nil {
		f.scraper.Close()
		f.pages.Close()
	}
	out := f.stderr
	if out == nil {
		out = os.Stderr
	}
	f.warnings.flush(out)
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
