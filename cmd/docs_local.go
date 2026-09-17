package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/docstore"
	"github.com/1broseidon/ketch/docstore/ingest"
	"github.com/spf13/cobra"
)

// The local docs library subcommands. `add` is an agent-facing research
// action (the agent found the docs site with `ketch search` and wants it
// available offline); `list`, `remove`, and `sync` manage what `add` built.
// See ADR-0004.

var docsAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Pull a documentation site into a local library",
	Long: `Fetch a documentation site and index it for offline search with the local docs backend.

<url> may be the docs landing page, a sitemap, or an llms.txt / llms-full.txt. ketch
discovers the cheapest reliable source itself — llms-full.txt, then llms.txt as a link
list, then sitemaps from robots.txt or /sitemap.xml, then a same-host BFS crawl — and
scopes list sources and crawls to the URL's path prefix (override with --prefix, or
--prefix / for the whole host). Use --dry-run to see the plan and page counts first.

Inside a project (a directory tree with .ketch/docs.json or a git repository) the
library is attached to that project so bare searches default to it and
` + "`ketch docs sync`" + ` can rebuild it elsewhere. --global skips the attachment.

Re-adding a name replaces the library's contents.`,
	Example: `  ketch docs add tailwind https://tailwindcss.com/docs --dry-run
  ketch docs add tailwind https://tailwindcss.com/docs --version 4
  ketch docs add hono https://hono.dev/llms-full.txt
  ketch docs "container queries" -b local --library tailwind`,
	Args: exitArgs(cobra.ExactArgs(2)),
	RunE: runDocsAdd,
}

var docsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List local docs libraries",
	Args:  exitArgs(cobra.NoArgs),
	RunE:  runDocsList,
}

var docsRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Delete a local docs library",
	Long:  `Delete a library from the local store and detach it from the current project's .ketch/docs.json if it is listed there.`,
	Args:  exitArgs(cobra.ExactArgs(1)),
	RunE:  runDocsRemove,
}

var docsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Add every library the project's .ketch/docs.json declares",
	Long:  `Read the enclosing project's .ketch/docs.json and add each library that is not yet in the local store, using the options recorded when it was attached. --force re-adds every listed library.`,
	Args:  exitArgs(cobra.NoArgs),
	RunE:  runDocsSync,
}

func init() {
	docsCmd.AddCommand(docsAddCmd, docsListCmd, docsRemoveCmd, docsSyncCmd)

	f := docsAddCmd.Flags()
	f.String("version", "", "version label stored with the library (informational)")
	f.String("prefix", "", "path prefix pages must be under (default: the URL's path; / for the whole host)")
	f.Bool("sitemap", false, "treat <url> as a sitemap even if it is not named like one")
	f.Int("max-pages", ingest.DefaultMaxPages, "stop after this many pages")
	f.Int("depth", ingest.DefaultDepth, "max BFS depth when the source is a crawl")
	f.Int("concurrency", ingest.DefaultConcurrency, "max concurrent fetches")
	f.Bool("dry-run", false, "discover the source and report the plan without fetching or writing")
	f.Bool("global", false, "do not attach the library to the enclosing project")
	f.Bool("no-cache", false, "bypass the page cache")
	f.Bool("verbose", false, "print each fetched URL to stderr")
	f.String("cookie-file", "", "Netscape cookies.txt jar; matching cookies are sent with each fetch (overrides config cookie_file)")
	f.String("user-agent", "", "User-Agent override (overrides config user_agent)")

	docsSyncCmd.Flags().Bool("force", false, "re-add libraries that are already stored")
	docsSyncCmd.Flags().Bool("no-cache", false, "bypass the page cache")
	docsSyncCmd.Flags().Bool("verbose", false, "print each fetched URL to stderr")
	docsSyncCmd.Flags().String("cookie-file", "", "Netscape cookies.txt jar (overrides config cookie_file)")
	docsSyncCmd.Flags().String("user-agent", "", "User-Agent override (overrides config user_agent)")
}

// openDocStore opens the configured local store, creating it on first use.
func openDocStore() (*docstore.Store, error) {
	dir, err := docs.LocalDir(&cfg)
	if err != nil {
		return nil, &ExitError{Code: ExitPrecondition, Err: fmt.Errorf("resolve docs_dir: %w", err)}
	}
	s, err := docstore.Open(dir)
	if err != nil {
		return nil, &ExitError{Code: ExitPrecondition, Err: err}
	}
	return s, nil
}

// currentProject finds the project enclosing the working directory, or nil.
func currentProject() (*docstore.Project, error) {
	cwd, err := os.Getwd()
	if err != nil {
		// No working directory: behave as if outside any project.
		return nil, nil
	}
	p, ok, err := docstore.FindProject(cwd)
	if err != nil {
		return nil, &ExitError{Code: ExitPrecondition, Err: err}
	}
	if !ok {
		return nil, nil
	}
	return p, nil
}

type docsAddOutput struct {
	*ingest.Summary
	Attached string  `json:"attached,omitempty"`
	Seconds  float64 `json:"seconds"`
}

func runDocsAdd(cmd *cobra.Command, args []string) error {
	name, seed := args[0], args[1]
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	version, _ := cmd.Flags().GetString("version")
	prefix, _ := cmd.Flags().GetString("prefix")
	sitemap, _ := cmd.Flags().GetBool("sitemap")
	maxPages, _ := cmd.Flags().GetInt("max-pages")
	depth, _ := cmd.Flags().GetInt("depth")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	global, _ := cmd.Flags().GetBool("global")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	verbose, _ := cmd.Flags().GetBool("verbose")

	if err := docstore.ValidateName(name); err != nil {
		return &ExitError{Code: ExitValidation, Err: err}
	}
	opts := ingest.AddOptions{
		Name: name, Version: version, Seed: seed, Prefix: prefix, ForceSitemap: sitemap,
		MaxPages: maxPages, Depth: depth, Concurrency: concurrency, DryRun: dryRun,
		Progress: progressPrinter(verbose),
	}

	var project *docstore.Project
	if !global && !dryRun {
		var err error
		if project, err = currentProject(); err != nil {
			return err
		}
	}

	out, err := docsAdd(cmd, opts, noCache)
	if err != nil {
		return err
	}
	if project != nil && out.Library != nil {
		att := docstore.Attachment{Name: name, Version: version, URL: seed, Prefix: prefix, ForceSitemap: sitemap, MaxPages: maxPages, Depth: depth}
		if maxPages == ingest.DefaultMaxPages {
			att.MaxPages = 0
		}
		if depth == ingest.DefaultDepth {
			att.Depth = 0
		}
		if err := project.Attach(att); err != nil {
			return &ExitError{Code: ExitPrecondition, Err: fmt.Errorf("attach to project: %w", err)}
		}
		out.Attached = project.Path
	} else if !global && !dryRun && out.Library != nil {
		fmt.Fprintln(os.Stderr, "note: not inside a project (no .ketch/docs.json or git repository above the working directory); the library is stored but not attached — run from a project or pass --global to silence this")
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	printDocsAdd(out)
	return nil
}

// docsAdd runs one ingest with the CLI's scraper and cache and maps errors
// onto exit codes: bad seed/name → validation, nothing indexable →
// not_found, fetch/discovery failures → upstream.
func docsAdd(cmd *cobra.Command, opts ingest.AddOptions, noCache bool) (*docsAddOutput, error) {
	store, err := openDocStore()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	scraper, err := newScraper(cmd)
	if err != nil {
		return nil, err
	}
	defer scraper.Close()
	pc := newPageCache(noCache)
	defer pc.Close()

	start := time.Now()
	sum, err := ingest.Add(cmd.Context(), store, scraper, pc, opts)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return nil, err
		case errors.Is(err, docstore.ErrEmpty) && sum != nil && sum.Fetched == 0 && sum.Skipped == 0 && sum.Failed > 0:
			// Nothing was fetched at all: the site (or network) failed, not
			// the content — retrying may help.
			return nil, &ExitError{Code: ExitUpstream, Err: err}
		case errors.Is(err, docstore.ErrEmpty):
			return nil, &ExitError{Code: ExitNotFound, Err: err}
		case errors.Is(err, ingest.ErrBadSeed):
			return nil, &ExitError{Code: ExitValidation, Err: err}
		default:
			return nil, &ExitError{Code: ExitUpstream, Err: fmt.Errorf("docs add failed: %w", err)}
		}
	}
	return &docsAddOutput{Summary: sum, Seconds: time.Since(start).Seconds()}, nil
}

func progressPrinter(verbose bool) func(string, error) {
	return func(u string, err error) {
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "warn: %s: %s\n", u, err)
		case verbose:
			fmt.Fprintf(os.Stderr, "fetched %s\n", u)
		}
	}
}

func printDocsAdd(out *docsAddOutput) {
	p := out.Plan
	fmt.Println("---")
	if out.DryRun {
		fmt.Println("dry_run: true")
	}
	fmt.Printf("seed: %s\n", p.Seed)
	fmt.Printf("source: %s (%s)\n", p.Source, p.SourceURL)
	if p.Prefix != "" {
		fmt.Printf("prefix: %s\n", p.Prefix)
	}
	if out.DryRun {
		if p.Source == docstore.SourceCrawl {
			fmt.Println("pages: unknown until crawled (BFS from the seed, same host, under the prefix)")
		} else {
			fmt.Printf("candidates: %d\n", p.Candidates)
			fmt.Printf("in_scope: %d\n", len(p.URLs))
		}
		if out.Stopped != "" {
			fmt.Printf("stopped: %s (raise --max-pages to fetch more)\n", out.Stopped)
		}
		fmt.Println("---")
		return
	}
	if lib := out.Library; lib != nil {
		fmt.Printf("library: %s\n", lib.Name)
		if lib.Version != "" {
			fmt.Printf("version: %s\n", lib.Version)
		}
		fmt.Printf("pages: %d\n", lib.Pages)
		fmt.Printf("sections: %d\n", lib.Sections)
	}
	if out.Skipped > 0 || out.Failed > 0 {
		fmt.Printf("skipped: %d empty, %d failed\n", out.Skipped, out.Failed)
	}
	if out.Unrendered > 0 {
		fmt.Printf("unrendered: %d pages looked JS-rendered and no browser is configured; content may be partial (ketch browser install, then re-add)\n", out.Unrendered)
	}
	if out.Stopped != "" {
		fmt.Printf("stopped: %s\n", out.Stopped)
	}
	if out.Attached != "" {
		fmt.Printf("attached: %s\n", out.Attached)
	}
	fmt.Printf("duration: %.1fs\n", out.Seconds)
	fmt.Println("---")
}

type docsListEntry struct {
	docstore.Library
	Attached bool `json:"attached"`
}

type docsListOutput struct {
	Dir       string          `json:"dir"`
	Project   string          `json:"project,omitempty"`
	Libraries []docsListEntry `json:"libraries"`
	// Missing lists libraries the project manifest declares that are not in
	// the store — `ketch docs sync` adds them.
	Missing []string `json:"missing,omitempty"`
}

func runDocsList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	dir, err := docs.LocalDir(&cfg)
	if err != nil {
		return &ExitError{Code: ExitPrecondition, Err: err}
	}
	out := docsListOutput{Dir: dir, Libraries: []docsListEntry{}}

	attached := map[string]bool{}
	if project, err := currentProject(); err != nil {
		return err
	} else if project != nil && project.Exists {
		out.Project = project.Path
		for _, n := range project.Names() {
			attached[n] = true
		}
	}

	if docstore.Exists(dir) {
		store, err := openDocStore()
		if err != nil {
			return err
		}
		defer store.Close()
		libs, err := store.Libraries()
		if err != nil {
			return &ExitError{Code: ExitPrecondition, Err: err}
		}
		for _, lib := range libs {
			out.Libraries = append(out.Libraries, docsListEntry{Library: lib, Attached: attached[lib.Name]})
			delete(attached, lib.Name)
		}
	}
	for n := range attached {
		out.Missing = append(out.Missing, n)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	if len(out.Libraries) == 0 {
		fmt.Printf("no local docs libraries in %s\n", dir)
		fmt.Println("add one with: ketch docs add <name> <url>")
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tVERSION\tPAGES\tSECTIONS\tSOURCE\tUPDATED\tATTACHED")
		for _, e := range out.Libraries {
			mark := ""
			if e.Attached {
				mark = "yes"
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\t%s\t%s\n", e.Name, e.Version, e.Pages, e.Sections, e.Source, e.UpdatedAt.Local().Format("2006-01-02"), mark)
		}
		w.Flush()
	}
	if len(out.Missing) > 0 {
		fmt.Printf("\nattached in %s but not stored: %s (run: ketch docs sync)\n", out.Project, strings.Join(out.Missing, ", "))
	}
	return nil
}

func runDocsRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	store, err := openDocStore()
	if err != nil {
		return err
	}
	defer store.Close()

	removed := true
	if err := store.Remove(name); err != nil {
		if !errors.Is(err, docstore.ErrNotFound) {
			return &ExitError{Code: ExitPrecondition, Err: err}
		}
		removed = false
	}
	detached := false
	if project, err := currentProject(); err != nil {
		return err
	} else if project != nil && project.Exists {
		if detached, err = project.Detach(name); err != nil {
			return &ExitError{Code: ExitPrecondition, Err: fmt.Errorf("update %s: %w", project.Path, err)}
		}
	}
	if !removed && !detached {
		return exitErrf(ExitNotFound, "library %q is not stored and not attached to this project (see: ketch docs list)", name)
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"name": name, "removed": removed, "detached": detached})
	}
	switch {
	case removed && detached:
		fmt.Printf("removed %s and detached it from the project\n", name)
	case removed:
		fmt.Printf("removed %s\n", name)
	default:
		fmt.Printf("detached %s from the project (it was not in the store)\n", name)
	}
	return nil
}

type docsSyncEntry struct {
	Name     string `json:"name"`
	Action   string `json:"action"` // "added" | "skipped" | "failed"
	Pages    int    `json:"pages,omitempty"`
	Sections int    `json:"sections,omitempty"`
	Error    string `json:"error,omitempty"`
}

func runDocsSync(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	force, _ := cmd.Flags().GetBool("force")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	verbose, _ := cmd.Flags().GetBool("verbose")

	project, err := currentProject()
	if err != nil {
		return err
	}
	if project == nil || !project.Exists {
		return exitErrf(ExitPrecondition, "no %s in this project (attach libraries with: ketch docs add <name> <url>)", docstore.ManifestPath)
	}

	store, err := openDocStore()
	if err != nil {
		return err
	}
	stored := map[string]bool{}
	if libs, err := store.Libraries(); err == nil {
		for _, l := range libs {
			stored[l.Name] = true
		}
	}
	store.Close()

	var entries []docsSyncEntry
	failed := 0
	for _, a := range project.Manifest.Libraries {
		if stored[a.Name] && !force {
			entries = append(entries, docsSyncEntry{Name: a.Name, Action: "skipped"})
			continue
		}
		opts := ingest.AddOptions{
			Name: a.Name, Version: a.Version, Seed: a.URL, Prefix: a.Prefix, ForceSitemap: a.ForceSitemap,
			MaxPages: a.MaxPages, Depth: a.Depth, Progress: progressPrinter(verbose),
		}
		out, err := docsAdd(cmd, opts, noCache)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			failed++
			entries = append(entries, docsSyncEntry{Name: a.Name, Action: "failed", Error: err.Error()})
			continue
		}
		entries = append(entries, docsSyncEntry{Name: a.Name, Action: "added", Pages: out.Library.Pages, Sections: out.Library.Sections})
	}

	if asJSON {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"project": project.Path, "libraries": entries}); err != nil {
			return err
		}
	} else {
		fmt.Printf("project: %s\n", project.Path)
		for _, e := range entries {
			switch e.Action {
			case "added":
				fmt.Printf("added   %s (%d pages, %d sections)\n", e.Name, e.Pages, e.Sections)
			case "skipped":
				fmt.Printf("skipped %s (already stored; --force to re-add)\n", e.Name)
			default:
				fmt.Printf("failed  %s: %s\n", e.Name, e.Error)
			}
		}
	}
	if failed > 0 {
		return exitErrf(ExitUpstream, "%d of %d libraries failed to sync", failed, len(entries))
	}
	return nil
}
