package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
	"github.com/1broseidon/ketch/scrape"
	"github.com/spf13/cobra"
)

var docsIndexCmd = &cobra.Command{
	Use:   "index [<library|docs-url>]",
	Short: "Index a library's docs site locally, for offline answers (ketch docs -b local)",
	Long: `Index a library's documentation once, then answer questions about it offline with ketch docs -b local (or -b auto, which asks the local index first).

The library is a docs URL, taken as the docs root, or a library name or spec (name, name@version, npm:/pypi:/crates:/go:/github: prefixes, owner/repo), resolved to its docs root exactly as the web backend resolves one: a name that is ambiguous or that nothing verifies is refused with its candidates, and the docs URL of the one you mean can then be passed instead.

One index holds one version of a library's docs. name@version (or --version) indexes that version's docs root, found with the site's own version list; with no version, the current docs. Either way only that version's tree is kept: other versions' trees below the root (/docs/2.x/, mike's version directories, a release channel such as /next/, or any tree the site's version list names) and their copies of pages are left out, as are other hosts (v3.router.vuejs.org beside router.vuejs.org) and paths outside a versioned root. Several versions of a library can be indexed side by side, each under its own docs root; a query names one with --library name@version, and without a version answers from the newest. A page in the indexed tree about a later major (a migration guide to it) is kept but labelled, and ranks lower unless the question asks about migrating.

ketch reads the site's own page lists (llms.txt and llms-full.txt, its sitemaps, its search index and nav links), keeps the pages under the docs root in its locale and version, honours robots.txt, fetches up to --max-pages pages --concurrency at a time through the page cache (llms.txt's pages first, then guides, then the rest, API reference leaves and release notes last), drops pages whose text is another page's, splits them into heading sections (never inside code blocks), cuts each section into snippet units (each fenced code block with the sentences just before it, and the prose no code claims), and indexes their text for full-text search (SQLite FTS5: headings weighted over body text, identifiers such as useForm, client_max_body_size or form.validateField findable by their whole name or their words). No model runs: queries rank by keywords. With hybrid ranking on (ketch config set docs_hybrid true, and a model from ketch model install) the sections are also embedded, which costs minutes of CPU on a large site. --dry-run prints the pages a build would index, in order, without fetching them.

A root that stands for a release (its page redirects or points as canonical to a numbered release's root, as duckdb.org/docs/stable/ does to /docs/2.0/) is indexed at that release, and so is an unversioned root whose pages or llms.txt sit in release directories right below it (the release llms.txt lists, else the newest; results show the unversioned addresses when the release's pages name them canonical). A version's own host whose page lists are an app shell is given its current host's paths to try.

Running it again refreshes the library: pages are fetched again (through the page cache) and only the ones whose content changed are sectioned (and, with hybrid ranking, embedded) again. --reembed embeds the stored text with the current model, without the network (hybrid ranking only).`,
	Args: exitArgs(cobra.MaximumNArgs(1)),
	RunE: runDocsIndex,
}

func init() {
	docsCmd.AddCommand(docsIndexCmd)
	f := docsIndexCmd.Flags()
	f.String("version", "", "the version's docs to index (a library name only; for a docs URL pass the version's URL)")
	f.Int("max-pages", docs.IndexDefaultMaxPages, "the most pages to index")
	f.Int("concurrency", docs.IndexDefaultConcurrency, "pages fetched at once")
	f.StringSlice("name", nil, "another name queries may call the library by (repeatable)")
	f.Bool("list", false, "list the indexed libraries")
	f.Bool("remove", false, "remove the named library from the index")
	f.Bool("reembed", false, "hybrid ranking (docs_hybrid): re-embed the named library's stored sections with the current model, without the network")
	f.Bool("no-model", false, "index text only, even with hybrid ranking (docs_hybrid) on")
	f.Bool("dry-run", false, "plan the build and print the pages it would index, in order, with their tier; fetch no page and write nothing")
}

func runDocsIndex(cmd *cobra.Command, args []string) error {
	list, _ := cmd.Flags().GetBool("list")
	remove, _ := cmd.Flags().GetBool("remove")
	reembed, _ := cmd.Flags().GetBool("reembed")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	path, err := docsindex.DefaultPath()
	if err != nil {
		return exitErrf(ExitPrecondition, "docs index: %v", err)
	}
	store := docs.OpenIndex(path)
	if note := docsindex.LegacyNote(path); note != "" {
		fmt.Fprintln(os.Stderr, "note: "+note)
	}
	modes := 0
	for _, on := range []bool{list, remove, reembed} {
		if on {
			modes++
		}
	}
	switch {
	case modes > 1:
		return exitErrf(ExitValidation, "--list, --remove and --reembed are separate operations")
	case list:
		if len(args) > 0 {
			return exitErrf(ExitValidation, "--list takes no library")
		}
		return runDocsIndexList(store, asJSON)
	case len(args) == 0:
		return exitErrf(ExitValidation, "docs index needs a library name or docs URL (or --list)")
	case remove:
		return runDocsIndexRemove(store, args[0], asJSON)
	case reembed:
		return runDocsIndexReembed(cmd, store, args[0], asJSON)
	}
	return runDocsIndexBuild(cmd, store, args[0], asJSON)
}

func runDocsIndexBuild(cmd *cobra.Command, store *docsindex.Store, library string, asJSON bool) error {
	req := docs.IndexRequest{Library: library, Progress: os.Stderr}
	req.Version, _ = cmd.Flags().GetString("version")
	req.MaxPages, _ = cmd.Flags().GetInt("max-pages")
	req.Concurrency, _ = cmd.Flags().GetInt("concurrency")
	req.Names, _ = cmd.Flags().GetStringSlice("name")
	req.DryRun, _ = cmd.Flags().GetBool("dry-run")
	if noModel, _ := cmd.Flags().GetBool("no-model"); !noModel && !req.DryRun && docs.Hybrid(&cfg) {
		emb, err := indexEmbedder()
		if err != nil {
			return err
		}
		if emb == nil {
			fmt.Fprintln(os.Stderr, "note: docs_hybrid is on but no embedding model is installed: indexing text only, ranked by keywords (ketch model install, then ketch docs index --reembed)")
		}
		req.Embedder = emb
	}
	fetcher := &docsFetcher{newScraper: func() (*scrape.Scraper, error) { return newDocsScraper(cmd) }}
	defer fetcher.Close()
	report, err := docs.IndexLibrary(cmd.Context(), &cfg, docs.Deps{Fetcher: fetcher}, store, req)
	if err := fetcher.classify(err, "docs index failed"); err != nil {
		return err
	}
	if req.DryRun {
		return printIndexPlan(report, asJSON)
	}
	return printIndexReport(report, asJSON)
}

// printIndexPlan prints a dry run: the docs root, then each planned page
// with its tier.
func printIndexPlan(r docs.IndexReport, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	root := r.Root
	if r.Library.Version != "" {
		root += " (version " + r.Library.Version + ")"
	}
	fmt.Printf("root: %s\nplanned: %d pages\n", root, len(r.Planned))
	for _, p := range r.Planned {
		fmt.Printf("%-9s %s\n", p.Tier, p.URL)
	}
	return nil
}

// indexEmbedder opens the model to index with: none when none is installed
// (text only), an error when model_path is set but is not a model.
func indexEmbedder() (embed.Embedder, error) {
	m, err := embed.Locate(cfg.String("model_path"))
	switch {
	case errors.Is(err, embed.ErrNotInstalled):
		return nil, nil
	case err != nil:
		return nil, exitErrf(ExitPrecondition, "%v", err)
	}
	return embed.Open(m), nil
}

func runDocsIndexReembed(cmd *cobra.Command, store *docsindex.Store, value string, asJSON bool) error {
	lib, err := indexedLibrary(store, value)
	if err != nil {
		return err
	}
	if !docs.Hybrid(&cfg) {
		return exitErrf(ExitPrecondition, "--reembed embeds sections for hybrid ranking, which is off: ketch config set %s true (and ketch model install)", docs.HybridKey)
	}
	emb, err := indexEmbedder()
	if err != nil {
		return err
	}
	if emb == nil {
		return exitErrf(ExitPrecondition, "--reembed needs an embedding model: ketch model install")
	}
	report, err := docs.Reembed(cmd.Context(), store, lib.Key, emb, os.Stderr)
	if err != nil {
		return upstreamErr(err, "docs index --reembed failed")
	}
	return printIndexReport(report, asJSON)
}

func runDocsIndexRemove(store *docsindex.Store, value string, asJSON bool) error {
	lib, err := indexedLibrary(store, value)
	if err != nil {
		return err
	}
	if _, err := store.Remove(lib.Key); err != nil {
		return upstreamErr(err, "docs index --remove failed")
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"removed": lib.Key})
	}
	fmt.Printf("removed: %s\n", lib.Key)
	return nil
}

// indexedLibrary is the one indexed library value names: not found (exit
// 3) when none, a validation error listing them when several.
func indexedLibrary(store *docsindex.Store, value string) (docsindex.Library, error) {
	libs, err := store.Libraries()
	if err != nil {
		return docsindex.Library{}, upstreamErr(err, "reading the docs index failed")
	}
	found := docs.MatchIndexed(libs, value)
	switch len(found) {
	case 0:
		return docsindex.Library{}, exitErrf(ExitNotFound, "%q is not indexed (ketch docs index --list)", value)
	case 1:
		return found[0], nil
	}
	var keys []string
	for _, l := range found {
		keys = append(keys, l.Key)
	}
	return docsindex.Library{}, exitErrf(ExitValidation, "%q names %d indexed libraries; pass one's docs root: %s", value, len(found), strings.Join(keys, ", "))
}

func runDocsIndexList(store *docsindex.Store, asJSON bool) error {
	libs, err := store.Libraries()
	if err != nil {
		return upstreamErr(err, "reading the docs index failed")
	}
	if asJSON {
		if libs == nil {
			libs = []docsindex.Library{}
		}
		return json.NewEncoder(os.Stdout).Encode(libs)
	}
	if len(libs) == 0 {
		fmt.Println("nothing indexed (ketch docs index <library|docs-url>)")
		return nil
	}
	for _, l := range libs {
		fmt.Println(indexLine(l))
	}
	fmt.Printf("index: %s (%s)\n", store.Path(), formatBytes(store.Size()))
	return nil
}

// indexLine is one library in --list and after a build.
func indexLine(l docsindex.Library) string {
	var about []string
	if l.Version != "" {
		about = append(about, "version "+l.Version)
	}
	about = append(about, fmt.Sprintf("%d pages, %d sections, %d snippets", l.Pages, l.Sections, l.Units))
	if l.Model != "" {
		about = append(about, fmt.Sprintf("%d vectors (%s)", l.Vectors, l.Model))
	}
	if l.OtherVersions > 0 {
		about = append(about, fmt.Sprintf("%d pages of other versions left out", l.OtherVersions))
	}
	if l.Capped {
		about = append(about, "capped")
	}
	if !l.Current() {
		about = append(about, fmt.Sprintf("index format %d, unreadable by this ketch: rebuild with ketch docs index %s", l.Format, l.Root))
	}
	about = append(about, "built "+l.Built.Format("2006-01-02 15:04 UTC"))
	line := l.Root + "  (" + strings.Join(about, ", ") + ")"
	if names := docs.IndexedNames(l); len(names) > 0 {
		line += "\n  names: " + strings.Join(names, ", ")
	}
	return line
}

func printIndexReport(r docs.IndexReport, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	fmt.Println(indexLine(r.Library))
	embedded := ""
	if r.Embedded > 0 {
		embedded = fmt.Sprintf("; %d windows embedded", r.Embedded)
	}
	fmt.Printf("  this run: %d new, %d changed, %d unchanged, %d removed, %d failed pages%s; %.0fs; index %s\n",
		r.New, r.Changed, r.Unchanged, r.Removed, r.Failed, embedded, r.Seconds, formatBytes(r.Bytes))
	return nil
}
