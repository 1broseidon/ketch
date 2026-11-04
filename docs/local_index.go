package docs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
	"github.com/1broseidon/ketch/internal/failure"
)

// Building a local docs index (ADR-0007): resolve the docs root once (a
// version's root when one is asked for), enumerate its pages
// (local_plan.go), keeping that version's tree alone, fetch them through
// the injected Fetcher (the scrape pipeline and page cache), section them
// as the web backend does, cut each section into snippet units
// (local_snippets.go), embed the sections' windows, and store the lot. A
// library's key is its docs root, so each version of a library (each its
// own root) is indexed side by side.

// IndexRequest asks for one library to be indexed.
type IndexRequest struct {
	// Library is a docs URL (taken as the root, no resolution) or a
	// library name or spec, resolved as the web backend resolves one: an
	// ambiguous or unverified name is refused with its candidates.
	Library string
	// Version asks for a version's docs (a name only).
	Version string
	// Names are extra names a query may call the library by.
	Names []string
	// MaxPages caps the pages indexed (IndexDefaultMaxPages when <= 0);
	// Concurrency is how many pages are fetched at once
	// (IndexDefaultConcurrency when <= 0).
	MaxPages, Concurrency int
	// Embedder embeds the sections; nil indexes text alone, which the
	// local backend ranks by keywords.
	Embedder embed.Embedder
	// Seeder names pages the web points to for the library, indexed first;
	// nil for none.
	Seeder PageSeeder
	// DryRun plans the build and stops before fetching any page: the
	// report's Planned lists the pages in the order they would be indexed.
	DryRun bool
	// Progress receives progress lines; nil for none.
	Progress io.Writer
}

// IndexReport says what a build did.
type IndexReport struct {
	Library docsindex.Library `json:"library"`
	// New, Changed and Unchanged count the pages fetched by whether their
	// content was stored before; Removed the stored pages discovery no
	// longer lists; Failed the pages that could not be fetched.
	New       int     `json:"new"`
	Changed   int     `json:"changed"`
	Unchanged int     `json:"unchanged"`
	Removed   int     `json:"removed"`
	Failed    int     `json:"failed"`
	Embedded  int     `json:"embedded"` // section windows embedded by this run
	Seconds   float64 `json:"seconds"`
	Bytes     int64   `json:"index_bytes"` // the index file's size afterwards
	// Planned is a dry run's plan: the docs root and the pages, in order,
	// each with its tier (curated, guide, other, reference).
	Root    string        `json:"root,omitempty"`
	Planned []PlannedPage `json:"planned,omitempty"`
}

// PlannedPage is a page a build would index.
type PlannedPage struct {
	URL  string `json:"url"`
	Tier string `json:"tier"`
}

// tierNames name the plan tiers.
var tierNames = [planTiers]string{"curated", "guide", "other", "reference"}

const (
	// IndexDefaultMaxPages bounds a build: a large reference site has tens
	// of thousands of pages, and the cap keeps a build to minutes.
	IndexDefaultMaxPages = 1500
	// IndexDefaultConcurrency is polite: four pages at a time from one site.
	IndexDefaultConcurrency = 4
	// indexWindowChars is how much of a section one vector covers:
	// about 300-400 tokens, well inside bge's 512-token window, so nothing
	// of a long section is cut off before it is embedded.
	indexWindowChars = 1200
	// indexMaxWindows bounds one section's vectors (a huge reference
	// listing); its text past them is still stored and shown.
	indexMaxWindows = 24
	// indexWriteBatch is how many pages one write transaction holds.
	indexWriteBatch = 40
	// indexEmbedBatch is how many windows are embedded between progress
	// reports.
	indexEmbedBatch  = 256
	indexPageTimeout = 45 * time.Second
)

// ErrIndexInput is a docs index request that cannot be honoured as asked.
var ErrIndexInput = failure.New(failure.Validation, "invalid_index_request", "invalid docs index request")

// IndexLibrary builds or refreshes the local index of one library, as
// `ketch docs index` does. Re-running it fetches the pages again (through
// the page cache) and re-sections and re-embeds only those whose content
// changed, or all of them when the model changed; pages discovery no
// longer lists are removed.
func IndexLibrary(ctx context.Context, c *config.Config, deps Deps, store *docsindex.Store, req IndexRequest) (IndexReport, error) {
	w := newWebFromConfig(c, deps)
	w.probeTimeout = 20 * time.Second
	w.sitemapBudget = 60 * time.Second
	w.resolveTimeout = 60 * time.Second
	w.maxFullBytes = 32 << 20
	w.concurrency = req.Concurrency
	if w.concurrency <= 0 {
		w.concurrency = IndexDefaultConcurrency
	}
	if req.MaxPages <= 0 {
		req.MaxPages = IndexDefaultMaxPages
	}
	return w.index(ctx, store, req)
}

// indexTarget is the docs root a request resolved to.
type indexTarget struct {
	root     webRoot
	version  string
	title    string
	resolved string // "docs_url" or "resolved"
	evidence []string
	trust    float64
	packages []string
	spec     libSpec // the name as given; zero for a docs URL
	// display is the unversioned root a re-rooted current release's
	// results are shown under ("" for the root itself).
	display string
}

// planIndex discovers the target's pages and plans the build, moving the
// root first to the release an alias stands for, and after planning to
// the release tree an unversioned root's pages all sit in; a version's
// own host with almost no pages listed borrows its current host's list.
func (w *Web) planIndex(ctx context.Context, target *indexTarget, req IndexRequest, say func(string, ...any)) (indexSite, indexPlan, error) {
	if moved, ok := w.aliasRoot(ctx, target.root); ok {
		say("docs root %s stands for %s", target.root, moved)
		w.moveRoot(target, moved)
	}
	seeds := w.seedPages(ctx, *target, req.Seeder, say)
	site, plan, err := w.discoverPlan(ctx, target, req.MaxPages, seeds, say)
	if err != nil {
		return site, plan, err
	}
	links := uniquePages(candidateLinks(site.site, target.root))
	if moved, byList, ok := rerootTree(target.root, links, plan, site.curated); ok {
		site, plan = w.reroot(ctx, target, moved, byList, site, plan, req.MaxPages, seeds, say)
	} else if target.spec.version == "" {
		var rootLinks []siteLink
		if prof, err := w.cachedProfile(ctx, target.root.u); err == nil && prof != nil {
			rootLinks = prof.links
		}
		if moved, ok := splitReleases(target.root, plan.links, rootLinks); ok {
			say("pages: %d planned in several releases' trees; indexing the default docs at %s", len(plan.links), moved)
			site, plan = w.reroot(ctx, target, moved, true, site, plan, req.MaxPages, seeds, say)
		}
	}
	if sib, ok := siblingHost(target.root); ok && len(plan.links) < siblingThin {
		if other, err := w.enumerate(ctx, sib); err == nil {
			borrowed := atHost(uniquePages(candidateLinks(other.site, sib)), target.root.u.Host)
			say("pages: %d listed on %s; probing %d paths of %s", plan.listed, target.root, len(borrowed), sib)
			site.site.links = append(site.site.links, borrowed...)
			site.sources = append(site.sources, "paths of "+sib.String())
			plan = w.plan(ctx, target.root, site, req.MaxPages, w.otherVersions(ctx, *target), seeds)
		}
	}
	say("pages: %d listed (%s); %d of other versions and %d of other locales left out, %d disallowed by robots.txt; indexing %d%s (%s)",
		plan.listed, strings.Join(site.sources, ", "), plan.otherVersions, plan.variants-plan.otherVersions, plan.disallowed, len(plan.links), capNote(plan.capped, req.MaxPages), tierNote(plan.tiers))
	return site, plan, nil
}

// reroot plans the build at the release tree rerootTree found, keeping
// the root as it was when a move the plan's thinness chose finds no more
// pages there than were kept, and shows a current release's pages under
// the unversioned addresses they name canonical.
func (w *Web) reroot(ctx context.Context, target *indexTarget, moved webRoot, byList bool, site indexSite, plan indexPlan, maxPages int, seeds []webLink, say func(string, ...any)) (indexSite, indexPlan) {
	before := *target
	w.moveRoot(target, moved)
	s2, p2, err := w.discoverPlan(ctx, target, maxPages, seeds, say)
	if err != nil || !byList && len(p2.links) <= len(plan.links) {
		*target = before
		return site, plan
	}
	say("pages: %d listed, %d kept: the docs sit in %s", plan.listed, len(plan.links), moved)
	if len(p2.links) > 0 && target.spec.version == "" {
		target.display = w.displayRootFor(ctx, before.root, target.root, p2.links[min(1, len(p2.links)-1)].url)
	}
	return s2, p2
}

// discoverPlan enumerates the target's root (following a docs URL that
// redirects to a version root or a new home when it lists nothing) and
// plans the build.
func (w *Web) discoverPlan(ctx context.Context, target *indexTarget, maxPages int, seeds []webLink, say func(string, ...any)) (indexSite, indexPlan, error) {
	site, err := w.enumerate(ctx, target.root)
	if errors.Is(err, ErrNotFound) && target.resolved == "docs_url" && ctx.Err() == nil {
		if moved, ok := w.indexRedirect(ctx, target.root); ok {
			target.root = moved
			say("docs root redirects to %s", moved)
			site, err = w.enumerate(ctx, target.root)
		}
	}
	if err != nil {
		return site, indexPlan{}, err
	}
	return site, w.plan(ctx, target.root, site, maxPages, w.otherVersions(ctx, *target), seeds), nil
}

// moveRoot points the target at another root of the same docs, labelled
// with that root's version.
func (w *Web) moveRoot(target *indexTarget, to webRoot) {
	target.evidence = append(target.evidence, fmt.Sprintf("%s holds the docs %s stands for", to, target.root))
	target.root = to
	if l, _ := rootVersion(to); l != "" {
		target.version = l
	}
}

// dryRunReport is a dry run's report: the root and the planned pages.
func dryRunReport(target indexTarget, site indexSite, plan indexPlan) IndexReport {
	r := IndexReport{Root: target.root.String()}
	for _, l := range plan.links {
		r.Planned = append(r.Planned, PlannedPage{URL: displayURL(l.url), Tier: tierNames[planTier(l.url, target.root, plan.curated)]})
	}
	r.Library = docsindex.Library{Root: target.root.String(), Version: target.version, Discovered: plan.listed - plan.variants, OtherVersions: plan.otherVersions, Capped: plan.capped, Sources: site.sources, DisplayRoot: target.display}
	return r
}

// tierNote counts the planned pages by tier.
func tierNote(t [planTiers]int) string {
	var parts []string
	for i, n := range t {
		parts = append(parts, fmt.Sprintf("%d %s", n, tierNames[i]))
	}
	return strings.Join(parts, ", ")
}

func (w *Web) index(ctx context.Context, store *docsindex.Store, req IndexRequest) (IndexReport, error) {
	start := time.Now()
	say := progressf(req.Progress)
	// Resolution and discovery share one fetch memo; the pages do not, so
	// a build never holds every page it fetched in memory twice.
	dctx := withFetchMemo(ctx)
	target, err := w.indexTarget(dctx, req)
	if err != nil {
		return IndexReport{}, err
	}
	say("docs root: %s%s (%s)", target.root, versionNote(target.version), target.resolved)
	site, plan, err := w.planIndex(dctx, &target, req, say)
	if err != nil {
		return IndexReport{}, err
	}
	if req.DryRun {
		return dryRunReport(target, site, plan), nil
	}
	pages, failed := w.fetchIndexPages(ctx, target.root, plan.links, say)
	pages, copies := uniqueContent(pages)
	if copies > 0 {
		say("%d pages left out as copies of others (the same text at another address, or a missing page served as the home page)", copies)
	}
	if len(pages) == 0 {
		return IndexReport{}, fmt.Errorf("web: none of the %d pages under %s could be fetched: %w", len(plan.links), target.root, ErrNotFound)
	}
	siteName, sitePkgs := w.siteIdentityNames(dctx, target.root)
	if target.title == "" {
		target.title = siteName
	}
	if len(target.packages) == 0 {
		target.packages = sitePkgs
	}
	b := &indexBuild{store: store, key: target.root.String(), emb: req.Embedder, say: say, version: target.version, title: target.title}
	report, err := b.write(ctx, pages, failed)
	if err != nil {
		return report, err
	}
	lib := docsindex.Library{
		Key: b.key, Library: req.Library, Title: target.title, Root: target.root.String(), Version: target.version,
		Requested: target.spec.version, Resolved: target.resolved, Evidence: target.evidence, Trust: target.trust, Packages: target.packages,
		Model: b.model, Dims: b.dims, Format: docsindex.FormatVersion, Built: time.Now().UTC(),
		Pages: report.Library.Pages, Sections: report.Library.Sections, Vectors: report.Library.Vectors, Units: report.Library.Units,
		Discovered: plan.listed - plan.variants, OtherVersions: plan.otherVersions, Capped: plan.capped, Failed: len(failed), Sources: site.sources,
		DisplayRoot: target.display, Seeds: plan.seeds, SiteName: siteName,
	}
	lib.Aliases = mergeNames(b.prev.Aliases, req.Names)
	lib.Names = IndexedNames(lib)
	if err := store.PutLibrary(lib); err != nil {
		return report, err
	}
	report.Library = lib
	report.Seconds = time.Since(start).Seconds()
	report.Bytes = store.Size()
	return report, nil
}

// indexTarget resolves the request to a docs root, as GetDocs would.
func (w *Web) indexTarget(ctx context.Context, req IndexRequest) (indexTarget, error) {
	spec, err := w.parseLibrary(req.Library)
	if err != nil {
		return indexTarget{}, err
	}
	if spec.url != "" {
		if req.Version != "" {
			return indexTarget{}, fmt.Errorf("%w: --version applies to a library name; for a docs URL, pass the version's docs URL", ErrIndexInput)
		}
		root, err := parseWebRoot(spec.url)
		if err != nil {
			return indexTarget{}, err
		}
		label, _ := rootVersion(root)
		return indexTarget{root: root, version: label, resolved: "docs_url", evidence: []string{"docs URL given"}}, nil
	}
	if req.Version != "" {
		if spec.version != "" && spec.version != req.Version {
			return indexTarget{}, fmt.Errorf("%w: %q already names version %s", ErrIndexInput, req.Library, spec.version)
		}
		spec.version = req.Version
	}
	// A build resolves afresh, past the resolution cache: it is a one-off
	// whose judgment lasts, and a cached pick lacks the package's releases.
	m, _, err := w.resolveOnce(ctx, spec, false, false, false)
	if err != nil {
		return indexTarget{}, err
	}
	lm := m.libraryMatch()
	t := indexTarget{root: m.root, version: m.version, title: m.title, resolved: "resolved", evidence: lm.Evidence, trust: m.trust, packages: m.packages, spec: spec}
	if latest := latestRelease(m.proj); spec.version == "" && !isNumericLabel(m.version) && latest != "" && !m.registry {
		// The current docs document the latest release when the site names
		// none (as -b web assumes for a version asked of them), so
		// name@major finds them beside an older version's index.
		t.version = latest
		t.evidence = append(t.evidence, fmt.Sprintf("the current docs name no release; taken as %s, the package's latest", latest))
	}
	return t, nil
}

// indexRedirect is where a docs URL with no pages of its own leads: a
// version root it redirects to, or a corroborated new home (docsForURL).
func (w *Web) indexRedirect(ctx context.Context, root webRoot) (webRoot, bool) {
	if canon, ok := w.versionRedirect(ctx, root); ok {
		return canon, true
	}
	moved, _, ok := w.movedRoot(ctx, root, nil)
	return moved, ok
}

// otherVersions are the labels of the versions the site publishes other
// than the one indexed (lower case, as normLabel compares them): a tree
// below the root named by one of them is another version's docs, however
// few of its pages the indexed tree shares (keepOneVariant). The site's
// own version list (siteVersions) says which they are; none when it
// publishes none.
func (w *Web) otherVersions(ctx context.Context, t indexTarget) map[string]bool {
	prof, err := w.cachedProfile(ctx, t.root.u)
	if err != nil {
		prof = nil
	}
	return otherVersionLabels(w.siteVersions(ctx, t.root, prof), t.root, t.version)
}

// otherVersionLabels are the labels of vs's versions other than the one
// at root or labelled version, normalized.
func otherVersionLabels(vs versionSet, root webRoot, version string) map[string]bool {
	own := map[string]bool{normLabel(rootVersionLabel(root)): true, normLabel(version): true}
	out := map[string]bool{}
	for _, v := range vs.versions {
		labels := versionLabels(v)
		mine := v.root != "" && sameRootURL(v.root, root)
		for _, l := range labels {
			mine = mine || l != "" && own[normLabel(l)]
		}
		if mine {
			continue
		}
		for _, l := range labels {
			if l = normLabel(l); l != "" && !own[l] {
				out[l] = true
			}
		}
	}
	return out
}

// fetchIndexPages fetches the planned pages, w.concurrency at a time, each
// within indexPageTimeout; pages in hand (llms-full.txt) are not fetched.
// It returns the pages in plan order, one per displayed URL, and the URLs
// that could not be fetched.
func (w *Web) fetchIndexPages(ctx context.Context, root webRoot, links []webLink, say func(string, ...any)) ([]webPage, []string) {
	pages := make([]*webPage, len(links))
	errs := make([]error, len(links))
	sem := make(chan struct{}, max(1, w.concurrency))
	var mu sync.Mutex
	done, fetched := 0, 0
	last := time.Now()
	var wg sync.WaitGroup
	for i, l := range links {
		if l.page != nil {
			pages[i] = l.page
			continue
		}
		fetched++
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			defer func() { <-sem }()
			pctx, cancel := context.WithTimeout(ctx, indexPageTimeout)
			pages[i], errs[i] = w.fetchOne(pctx, root, l)
			cancel()
			mu.Lock()
			done++
			if time.Since(last) > 10*time.Second {
				last = time.Now()
				say("fetched %d of %d pages", done, fetched)
			}
			mu.Unlock()
		})
	}
	wg.Wait()
	var out []webPage
	var failed []string
	seen := map[string]bool{}
	for i, l := range links {
		switch {
		case pages[i] == nil:
			failed = append(failed, displayURL(l.url))
		case !seen[pages[i].URL]:
			seen[pages[i].URL] = true
			out = append(out, *pages[i])
		}
	}
	if fetched > 0 {
		say("fetched %d pages (%d in hand from llms-full.txt), %d failed", fetched-len(failed), len(links)-fetched, len(failed))
	}
	return out, failed
}

// indexBuild writes one library's pages to the store.
type indexBuild struct {
	store *docsindex.Store
	key   string
	emb   embed.Embedder
	say   func(string, ...any)
	// version and title are the indexed version's label and the library's
	// title, which label cross-version sections (crossVersion).
	version, title string
	// source is how the pages came in: "index" (a build) or "search".
	source string

	prev        docsindex.Library // the library as stored before, if it was
	model       string
	dims        int
	reembedding bool // the model changed: every page is embedded again
}

// write sections the pages, embeds the windows of new and changed ones,
// stores them in batches and removes the pages no longer listed. Its
// report's Library carries the stored totals.
func (b *indexBuild) write(ctx context.Context, pages []webPage, failed []string) (IndexReport, error) {
	var report IndexReport
	stored, err := b.begin()
	if err != nil {
		return report, err
	}
	todo := b.diff(pages, stored, &report)
	if report.Embedded, err = b.embed(ctx, todo); err != nil {
		return report, err
	}
	if err := putPageBatches(b.store, b.key, todo); err != nil {
		return report, err
	}
	for _, pd := range todo {
		report.Library.Sections += pd.Page.Sections
		report.Library.Vectors += pd.Page.Windows
		report.Library.Units += pd.Page.Units
	}
	gone := b.unlisted(pages, failed, stored, &report)
	if err := b.store.DeletePages(b.key, gone); err != nil {
		return report, err
	}
	report.Removed, report.Failed = len(gone), len(failed)
	report.Library.Pages = len(pages) + len(failed) - countMissing(failed, stored)
	embedded := ""
	if b.emb != nil {
		embedded = fmt.Sprintf("; %d windows embedded", report.Embedded)
	}
	b.say("stored %d pages: %d new, %d changed, %d unchanged, %d removed%s", len(pages), report.New, report.Changed, report.Unchanged, report.Removed, embedded)
	return report, nil
}

// begin reads what the store holds for the library: its metadata (a
// library in another index format is removed, to be rebuilt) and its
// pages; and decides whether the model changed.
func (b *indexBuild) begin() (map[string]docsindex.Page, error) {
	prev, found, err := b.store.Library(b.key)
	if err != nil {
		return nil, err
	}
	if found && !prev.Current() {
		b.say("index format changed: rebuilding")
		if _, err := b.store.Remove(b.key); err != nil {
			return nil, err
		}
		found = false
	}
	b.prev = prev
	if b.emb != nil {
		b.model, b.dims = b.emb.ID(), b.emb.Dim()
	}
	stored, err := b.store.Pages(b.key)
	if err != nil {
		return nil, err
	}
	b.reembedding = found && prev.Model != b.model
	if b.reembedding && len(stored) > 0 {
		b.say("model changed (%s -> %s): re-embedding every page", modelName(prev.Model), modelName(b.model))
	}
	return stored, nil
}

// diff sections every page (anchors corrected by the links of all of
// them) and returns the new and changed ones, ready to embed; unchanged
// pages are counted with what they already hold.
func (b *indexBuild) diff(pages []webPage, stored map[string]docsindex.Page, report *IndexReport) []docsindex.PageData {
	var sections []webSection
	for i, p := range pages {
		sections = append(sections, splitSections(p, i)...)
	}
	sections = adoptLinkedAnchors(sections)
	byPage := make([][]webSection, len(pages))
	for _, s := range sections {
		byPage[s.page] = append(byPage[s.page], s)
	}
	var todo []docsindex.PageData
	for i, p := range pages {
		hash := pageHash(p)
		old, had := stored[p.URL]
		switch {
		case had && old.Hash == hash && !b.reembedding:
			report.Unchanged++
			report.Library.Sections += old.Sections
			report.Library.Vectors += old.Windows
			report.Library.Units += old.Units
			continue
		case had:
			report.Changed++
		default:
			report.New++
		}
		todo = append(todo, b.pageData(p, hash, byPage[i]))
	}
	return todo
}

// unlisted returns the stored pages this build neither fetched nor failed
// to fetch (a page that failed this time keeps what it had, and counts).
func (b *indexBuild) unlisted(pages []webPage, failed []string, stored map[string]docsindex.Page, report *IndexReport) []string {
	fetched := map[string]bool{}
	for _, p := range pages {
		fetched[p.URL] = true
	}
	kept := map[string]bool{}
	for _, u := range failed {
		kept[u] = true
	}
	var gone []string
	for u, p := range stored {
		switch {
		case fetched[u]:
		case kept[u]:
			report.Library.Sections += p.Sections
			report.Library.Vectors += p.Windows
			report.Library.Units += p.Units
		default:
			gone = append(gone, u)
		}
	}
	slices.Sort(gone)
	return gone
}

// countMissing counts failed pages the store did not have before (they
// are not in the index).
func countMissing(failed []string, stored map[string]docsindex.Page) int {
	n := 0
	for _, u := range failed {
		if _, ok := stored[u]; !ok {
			n++
		}
	}
	return n
}

// pageData is a page's record and sections, with their snippet units and
// cross-version labels, ready to embed and store. A page whose title or
// path names a migration to a later major labels every section; a page
// that does not, the sections whose own headings do.
func (b *indexBuild) pageData(p webPage, hash string, secs []webSection) docsindex.PageData {
	source := b.source
	if source == "" {
		source = "index"
	}
	pd := docsindex.PageData{Page: docsindex.Page{URL: p.URL, Title: p.Title, Hash: hash, Sections: len(secs), Version: b.version, Fetched: time.Now().UTC(), Source: source}}
	pageTexts := []string{p.Title, pathWords(p.URL, b.key)}
	for i, s := range secs {
		units := extractUnits(s.text)
		pd.Page.Units += len(units)
		pd.Sections = append(pd.Sections, docsindex.Section{
			Page: p.URL, Order: i, Anchor: s.anchor, Heading: s.heading, Crumbs: s.crumbs, Text: s.text,
			Units: units, CrossVersion: crossVersion(b.version, b.title, append(slices.Clone(pageTexts), s.crumbs...)),
		})
	}
	return pd
}

// embed embeds every window of the pages' sections, in batches with
// progress, and records each page's window count. Snippet units are not
// embedded (ADR-0007, amendment 2: the snippet view scores a unit by its
// section's windows). It returns how many windows it embedded.
func (b *indexBuild) embed(ctx context.Context, pages []docsindex.PageData) (int, error) {
	if b.emb == nil {
		return 0, nil
	}
	type ref struct{ page, sec int }
	var texts []string
	var refs []ref
	for i := range pages {
		for j, s := range pages[i].Sections {
			pages[i].Sections[j].Vectors, pages[i].Sections[j].UnitVectors = nil, nil
			for _, t := range sectionWindows(s.Crumbs, s.Text) {
				texts = append(texts, t)
				refs = append(refs, ref{page: i, sec: j})
			}
		}
		pages[i].Page.Windows = 0
	}
	if len(texts) == 0 {
		return 0, nil
	}
	b.say("embedding %d windows of %d sections with %s", len(texts), countSections(pages), b.model)
	start, last := time.Now(), time.Now()
	for s := 0; s < len(texts); s += indexEmbedBatch {
		vecs, err := b.emb.EmbedPassages(ctx, texts[s:min(s+indexEmbedBatch, len(texts))])
		if err != nil {
			return 0, fmt.Errorf("embedding: %w", err)
		}
		for k, v := range vecs {
			r := refs[s+k]
			pages[r.page].Sections[r.sec].Vectors = append(pages[r.page].Sections[r.sec].Vectors, v)
			pages[r.page].Page.Windows++
		}
		if time.Since(last) > 15*time.Second {
			last = time.Now()
			done := min(s+indexEmbedBatch, len(texts))
			b.say("embedded %d of %d windows (%.0f/s)", done, len(texts), float64(done)/time.Since(start).Seconds())
		}
	}
	return len(texts), nil
}

func countSections(pages []docsindex.PageData) int {
	n := 0
	for _, p := range pages {
		n += len(p.Sections)
	}
	return n
}

// sectionWindows is what a section's vectors embed: its heading path, a
// line break, then a part of its text, each part at most about
// indexWindowChars, cut at block boundaries (paragraphs, lists, whole code
// blocks; a block too long for one window at line breaks, a line too long
// at a rune boundary). The heading path leads every window, so each says
// where it is.
func sectionWindows(crumbs []string, text string) []string {
	head := strings.Join(crumbs, " > ")
	var out []string
	for _, chunk := range packBlocks(textBlocks(text), indexWindowChars) {
		out = append(out, head+"\n"+chunk)
		if len(out) == indexMaxWindows {
			break
		}
	}
	if len(out) == 0 {
		out = append(out, head)
	}
	return out
}

// packBlocks joins blocks into chunks of at most n bytes, splitting any
// block longer than n into lines, and any line longer than n.
func packBlocks(blocks []string, n int) []string {
	var parts []string
	for _, b := range blocks {
		if len(b) <= n {
			parts = append(parts, b)
			continue
		}
		for _, line := range strings.Split(b, "\n") {
			for len(line) > n {
				cut := n
				for cut > 0 && !utf8.RuneStart(line[cut]) {
					cut--
				}
				parts = append(parts, line[:cut])
				line = line[cut:]
			}
			parts = append(parts, line)
		}
	}
	var out []string
	var cur strings.Builder
	for _, p := range parts {
		if cur.Len() > 0 && cur.Len()+2+len(p) > n {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(p)
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}

// pageHash identifies a page's content as fetched.
func pageHash(p webPage) string {
	sum := sha256.Sum256([]byte(p.Title + "\x00" + p.Markdown))
	return hex.EncodeToString(sum[:])
}

// mergeNames is a followed by the names in b it lacks.
func mergeNames(a, b []string) []string {
	out := slices.Clone(a)
	for _, n := range b {
		if n = strings.TrimSpace(n); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// libraryNames are the names a query may call an indexed library by, as
// comparison keys (libraryKey), worked out from its stored metadata so a
// library indexed by an older build answers to the same names. Primary
// names are its full identity: the --name aliases, the name it was indexed
// by, its docs site's own name (Vue Router, Beautiful Soup), its package
// names (with or without the ecosystem prefix and separators: vue-router,
// vuerouter) and repositories (vuejs/router). Secondary names are the last
// part of a scoped package or repository (starlight in @astrojs/starlight,
// hono in honojs/hono), its docs host's distinctive labels (fastapi in
// fastapi.tiangolo.com) and its root's path segments (bs4 in
// /software/BeautifulSoup/bs4/doc/). A secondary name that is one word of
// a longer name the library goes by (router and vue for Vue Router, query
// for TanStack Query, rest for Django REST framework) is never a name: it
// names something else, often another library.
func libraryNames(l docsindex.Library) (primary, secondary []string) {
	primary, specific, host := libraryNameTiers(l)
	return primary, append(specific, host...)
}

// libraryNameTiers are libraryNames with the secondary names in two
// tiers: those of the library's own (the last part of a package or
// repository, its root's path segments) before its host's labels, which
// every library documented on the host shares (svelte.dev/docs/kit and
// svelte.dev/docs/svelte; the latter alone is named svelte by its path).
func libraryNameTiers(l docsindex.Library) (primary, specific, host []string) {
	full := fullNames(l)
	words := map[string]bool{} // the words of every multi-word name
	addWords := func(name string) {
		if parts := nameWords(name); len(parts) > 1 {
			for _, w := range parts {
				words[libraryKey(w)] = true
			}
		}
	}
	title, _, _ := strings.Cut(cleanPageTitle(l.Title), " - ") // "FastAPI - FastAPI" is FastAPI
	addWords(title)
	addWords(l.SiteName)
	seen := map[string]bool{}
	add := func(into *[]string, k string) {
		if len(k) >= 2 && !genericNames[k] && !seen[k] {
			seen[k] = true
			*into = append(*into, k)
		}
	}
	var tails []string
	for _, n := range full {
		add(&primary, libraryKey(n))
		tail := n
		if i := strings.LastIndex(n, "/"); i >= 0 { // @scope/pkg, owner/repo, a module path
			tail = n[i+1:]
			tails = append(tails, tail)
		}
		addWords(tail)
	}
	root, _ := parseWebRoot(l.Root)
	for _, n := range append(tails, pathNames(root)...) {
		if k := libraryKey(n); !words[k] {
			add(&specific, k)
		}
	}
	for _, n := range hostNames(root.u) {
		if k := libraryKey(n); !words[k] {
			add(&host, k)
		}
	}
	return primary, specific, host
}

// fullNames are a library's identities as written: its aliases, the name
// it was indexed by, its site's name and its packages (no ecosystem).
func fullNames(l docsindex.Library) []string {
	full := slices.Clone(l.Aliases)
	if !isHTTPURL(l.Library) {
		if spec, err := parseLibSpec(strings.TrimSpace(l.Library)); err == nil && spec.name != "" {
			full = append(full, spec.name)
		}
	}
	if l.SiteName != "" {
		full = append(full, l.SiteName)
	}
	for _, p := range l.Packages {
		if _, after, ok := strings.Cut(p, ":"); ok {
			p = after
		}
		full = append(full, p)
	}
	return full
}

// pathNoise are root path segments that name no library: docs
// directories, locales, versions, languages.
var pathNoise = map[string]bool{
	"docs": true, "doc": true, "documentation": true, "software": true, "en": true, "stable": true, "latest": true,
	"current": true, "oss": true, "python": true, "py": true, "js": true, "javascript": true, "typescript": true,
	"ts": true, "go": true, "golang": true, "rust": true, "java": true, "ruby": true, "php": true, "dotnet": true,
	"reference": true, "api": true, "guide": true, "manual": true, "html": true, "index": true, "home": true,
}

// pathNames are a root's path segments that may name its library (bs4 in
// /software/BeautifulSoup/bs4/doc/): all but docs directories, locales,
// versions and language names.
func pathNames(root webRoot) []string {
	if root.u == nil {
		return nil
	}
	var out []string
	for _, seg := range pathSegments(root.prefix) {
		ls := strings.ToLower(seg)
		if _, version := segmentLabel(ls); pathNoise[ls] || version || localeRe.MatchString(ls) {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// IndexedNames are the names a query may call an indexed library by
// (libraryNames), full names first, as comparison keys.
func IndexedNames(l docsindex.Library) []string {
	primary, secondary := libraryNames(l)
	return append(primary, secondary...)
}

// nameWords splits a name into its words at separators (TanStack Query,
// vue-router, my_lib, @scope/pkg); a camel-case word stays one (TanStack).
func nameWords(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// hostWords are host labels that name no library.
var hostWords = map[string]bool{
	"www": true, "docs": true, "doc": true, "api": true, "dev": true, "developer": true, "developers": true,
	"readthedocs": true, "github": true, "gitlab": true, "pages": true, "netlify": true, "vercel": true,
	"rs": true, "pkg": true, "go": true, "org": true, "com": true, "net": true, "io": true, "app": true,
	"site": true, "guide": true, "learn": true, "en": true, "stable": true, "latest": true,
}

// hostNames are a docs host's labels that may name its library: all but
// the top-level domain and the words every docs host has, and a label
// without the language word it joins (httpx for python-httpx).
func hostNames(u *url.URL) []string {
	if u == nil {
		return nil
	}
	labels := strings.Split(scopeHost(u), ".")
	var out []string
	for _, l := range labels[:max(len(labels)-1, 0)] {
		if _, version := segmentLabel(l); !hostWords[l] && !version {
			out = append(out, l) // v3 in v3.router.vuejs.org names a version, not the library
			if rest := withoutLanguageWord(l); rest != "" {
				out = append(out, rest) // httpx in python-httpx.org
			}
		}
	}
	return out
}

// withoutLanguageWord is a hyphenated host label without the language
// words (languageWords) it joins to a library's name (python-httpx:
// httpx, python-poetry: poetry), or "" when it has none or nothing else.
func withoutLanguageWord(label string) string {
	parts := strings.Split(label, "-")
	if len(parts) < 2 {
		return ""
	}
	var kept []string
	for _, p := range parts {
		if !languageWords[p] {
			kept = append(kept, p)
		}
	}
	if len(kept) == len(parts) || len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "-")
}

// Reembed re-embeds a stored library's sections with emb, from their
// stored text, without the network: for an index built without a model or
// with another one.
func Reembed(ctx context.Context, store *docsindex.Store, key string, emb embed.Embedder, progress io.Writer) (IndexReport, error) {
	start := time.Now()
	lib, found, err := store.Library(key)
	if err != nil || !found {
		return IndexReport{}, errors.Join(err, fmt.Errorf("docs index: %s %w", key, ErrNotIndexed))
	}
	if !lib.Current() {
		return IndexReport{}, outdatedErr(lib)
	}
	secs, err := store.Sections(key, 0, false)
	if err != nil {
		return IndexReport{}, err
	}
	list := sectionPages(secs)
	b := &indexBuild{store: store, key: key, emb: emb, say: progressf(progress), model: emb.ID(), dims: emb.Dim()}
	n, err := b.embed(ctx, list)
	if err != nil {
		return IndexReport{}, err
	}
	stored, err := store.Pages(key)
	if err != nil {
		return IndexReport{}, err
	}
	var updated []docsindex.PageData
	for _, pd := range list {
		if p, ok := stored[pd.Page.URL]; ok {
			p.Windows = pd.Page.Windows
			updated = append(updated, docsindex.PageData{Page: p, Sections: pd.Sections})
		}
	}
	if err := putPageBatches(store, key, updated); err != nil {
		return IndexReport{}, err
	}
	windows := 0
	for _, pd := range updated {
		windows += pd.Page.Windows
	}
	lib.Model, lib.Dims, lib.Vectors, lib.Built = b.model, b.dims, windows, time.Now().UTC()
	if err := store.PutLibrary(lib); err != nil {
		return IndexReport{}, err
	}
	return IndexReport{Library: lib, Embedded: n, Unchanged: len(list), Seconds: time.Since(start).Seconds(), Bytes: store.Size()}, nil
}

// sectionPages groups stored sections (in page order) into their pages.
func sectionPages(secs []docsindex.Section) []docsindex.PageData {
	var out []docsindex.PageData
	at := map[string]int{}
	for _, s := range secs {
		i, ok := at[s.Page]
		if !ok {
			i = len(out)
			at[s.Page] = i
			out = append(out, docsindex.PageData{Page: docsindex.Page{URL: s.Page}})
		}
		out[i].Sections = append(out[i].Sections, s)
	}
	return out
}

// putPageBatches writes pages indexWriteBatch to a transaction, in URL
// order: pages fetched concurrently arrive in any order, and the order
// they are stored in breaks ties in the keyword ranking.
func putPageBatches(store *docsindex.Store, key string, pages []docsindex.PageData) error {
	pages = slices.Clone(pages)
	slices.SortFunc(pages, func(a, b docsindex.PageData) int { return strings.Compare(a.Page.URL, b.Page.URL) })
	for start := 0; start < len(pages); start += indexWriteBatch {
		if err := store.PutPages(key, pages[start:min(start+indexWriteBatch, len(pages))]); err != nil {
			return err
		}
	}
	return nil
}

// progressf writes progress lines to w (nowhere when nil).
func progressf(w io.Writer) func(string, ...any) {
	if w == nil {
		return func(string, ...any) {}
	}
	var mu sync.Mutex
	return func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = fmt.Fprintf(w, format+"\n", args...)
	}
}

func versionNote(v string) string {
	if v == "" {
		return ""
	}
	return " version " + v
}

func capNote(capped bool, n int) string {
	if !capped {
		return ""
	}
	return fmt.Sprintf(" (capped at %d; raise with --max-pages)", n)
}

func modelName(id string) string {
	if id == "" {
		return "none"
	}
	return id
}
