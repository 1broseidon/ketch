package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/scrape"
	"github.com/spf13/cobra"
)

var docsIndexCmd = &cobra.Command{
	Use:   "index [<library|docs-url>]",
	Short: "Index a library's docs site locally, so ketch docs answers about it offline",
	Long: `Index a library's documentation once, so ketch docs answers questions
about it from the local index instead of searching the web.

The library is a docs URL, taken as the docs root, or a name: name,
name@version, an npm:, pypi:, crates:, go: or github: prefix, or
owner/repo. A name is resolved to its docs root (ketch docs --resolve
shows the pick); one that is ambiguous or that nothing verifies is
refused with its candidates, and you pass the docs URL you mean instead.

One index holds one version: name@version or --version picks it, else
the current docs. Other versions' trees, other locales and other hosts
are left out. Several versions can be indexed side by side.

ketch reads the site's own page lists (llms.txt, sitemaps, its search
index, nav links), honours robots.txt and fetches up to --max-pages
pages through the page cache. No model runs.

Running it again refreshes the library: only the pages that changed are
sectioned again. Only this command adds libraries or pages to the index.`,
	Example: `  ketch docs index https://docs.ollama.com/
  ketch docs index fastapi --dry-run
  ketch docs index --list
  ketch docs index --remove https://docs.ollama.com/`,
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
	f.Bool("dry-run", false, "print the pages a build would index, in order, with their tier; write nothing")
}

func runDocsIndex(cmd *cobra.Command, args []string) error {
	list, _ := cmd.Flags().GetBool("list")
	remove, _ := cmd.Flags().GetBool("remove")
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	path, err := docsindex.DefaultPath()
	if err != nil {
		return exitErrf(ExitPrecondition, "docs index: %v", err)
	}
	store := docs.OpenIndex(path)
	if note := docsindex.LegacyNote(path); note != "" {
		fmt.Fprintln(os.Stderr, "note: "+note)
	}
	switch {
	case list && remove:
		return exitErrf(ExitValidation, "--list and --remove are separate operations")
	case list:
		if len(args) > 0 {
			return exitErrf(ExitValidation, "--list takes no library")
		}
		return runDocsIndexList(store, asJSON)
	case len(args) == 0:
		return exitErrf(ExitValidation, "docs index needs a library name or docs URL (or --list)")
	case remove:
		return runDocsIndexRemove(store, args[0], asJSON)
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

// runDocsIndexList lists the indexed libraries: those ketch docs index
// built (a library an earlier ketch stored for a question is not one; the
// next build removes it, and --remove takes it by its root).
func runDocsIndexList(store *docsindex.Store, asJSON bool) error {
	libs, err := store.Libraries()
	if err != nil {
		return upstreamErr(err, "reading the docs index failed")
	}
	libs = docs.Readable(libs)
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
	about = append(about, fmt.Sprintf("%d pages, %d sections", l.Pages, l.Sections))
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
	fmt.Printf("  this run: %d new, %d changed, %d unchanged, %d removed, %d failed pages; %.0fs; index %s\n",
		r.New, r.Changed, r.Unchanged, r.Removed, r.Failed, r.Seconds, formatBytes(r.Bytes))
	return nil
}
