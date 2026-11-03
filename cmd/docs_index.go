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

ketch reads the site's own page lists (llms.txt and llms-full.txt, its sitemaps, its search index and nav links), keeps the pages under the docs root in its locale and version, honours robots.txt, fetches up to --max-pages pages --concurrency at a time through the page cache, splits them into heading sections (never inside code blocks), cuts each section into snippet units (each fenced code block with the sentences just before it, and the prose no code claims), and embeds the sections and the units with the model ketch model install downloaded. Without a model the text is indexed alone and ranked by keywords.

Running it again refreshes the library: pages are fetched again (through the page cache) and only the ones whose content changed are sectioned and embedded again. --reembed re-embeds the stored text with the current model, without the network.`,
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
	f.Bool("reembed", false, "re-embed the named library's stored sections with the current model, without the network")
	f.Bool("no-model", false, "index text only, ranked by keywords, even with a model installed")
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
	store := docsindex.Open(path)
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
	if noModel, _ := cmd.Flags().GetBool("no-model"); !noModel {
		emb, err := indexEmbedder()
		if err != nil {
			return err
		}
		req.Embedder = emb
	}
	if req.Embedder == nil {
		fmt.Fprintln(os.Stderr, "note: indexing text only (no embedding model): queries rank by keywords until ketch model install and ketch docs index --reembed")
	}
	fetcher := &docsFetcher{newScraper: func() (*scrape.Scraper, error) { return newDocsScraper(cmd) }}
	defer fetcher.Close()
	report, err := docs.IndexLibrary(cmd.Context(), &cfg, docs.Deps{Fetcher: fetcher}, store, req)
	if err := fetcher.classify(err, "docs index failed"); err != nil {
		return err
	}
	return printIndexReport(report, asJSON)
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
	} else {
		about = append(about, "no vectors (keyword ranking)")
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
	fmt.Printf("  this run: %d new, %d changed, %d unchanged, %d removed, %d failed pages; %d passages embedded; %.0fs; index %s\n",
		r.New, r.Changed, r.Unchanged, r.Removed, r.Failed, r.Embedded, r.Seconds, formatBytes(r.Bytes))
	return nil
}
