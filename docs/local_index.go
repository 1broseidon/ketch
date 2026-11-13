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

	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/failure"
)

// Building a local docs index (ADR-0007): resolve the docs root once (a
// version's root when one is asked for), enumerate its pages
// (local_plan.go), keeping that version's tree alone, fetch them through
// the injected Fetcher (the scrape pipeline and page cache), section them
// as the web backend does, label the sections about a later major
// (crossVersion), and store them with their full-text rows. A
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
	// indexWriteBatch is how many pages one write transaction holds.
	indexWriteBatch  = 40
	indexPageTimeout = 45 * time.Second
)

// ErrIndexInput is a docs index request that cannot be honoured as asked.
var ErrIndexInput = failure.New(failure.Validation, "invalid_index_request", "invalid docs index request")

// IndexLibrary builds or refreshes the local index of one library, as
// `ketch docs index` does. Re-running it fetches the pages again (through
// the page cache) and re-sections only those whose content changed; pages
// discovery no longer lists are removed.
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
	b := &indexBuild{store: store, key: target.root.String(), say: say, version: target.version, title: target.title}
	report, err := b.write(pages, failed)
	if err != nil {
		return report, err
	}
	replaced, err := replacedBy(store, target.root, b.key)
	if err != nil {
		return report, err
	}
	lib := docsindex.Library{
		Key: b.key, Library: req.Library, Title: target.title, Root: target.root.String(), Version: target.version,
		Requested: target.spec.version, Resolved: target.resolved, Evidence: target.evidence, Trust: target.trust, Packages: target.packages,
		Format: docsindex.FormatVersion, Built: time.Now().UTC(),
		Pages: report.Library.Pages, Sections: report.Library.Sections,
		Discovered: plan.listed - plan.variants, OtherVersions: plan.otherVersions, Capped: plan.capped, Failed: len(failed), Sources: site.sources,
		DisplayRoot: target.display, Seeds: plan.seeds, SiteName: siteName,
	}
	lib.Aliases = mergeNames(b.prev.Aliases, req.Names)
	for _, r := range replaced {
		if !BuiltBySearch(r) {
			lib.Aliases = mergeNames(lib.Aliases, r.Aliases)
		}
	}
	lib.Names = IndexedNames(lib)
	if err := store.PutLibrary(lib); err != nil {
		return report, err
	}
	if err := removeReplaced(store, replaced, say); err != nil {
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
	say   func(string, ...any)
	// version and title are the indexed version's label and the library's
	// title, which label cross-version sections (crossVersion).
	version, title string
	// source is how the pages came in: "index" (a build) or "search" (the
	// pages a question read, held in memory). at is when they were read
	// (zero: now).
	source string
	at     time.Time
	// alone sections each page with anchors from its own links only, not
	// every page's (adoptLinkedAnchors): the pages a question read, whose
	// sections --section later finds from the one page alone.
	alone bool

	prev docsindex.Library // the library as stored before, if it was
	// same are the stored pages diff found unchanged: read again now;
	// resourced, those of them now read another way (url → pageSource).
	same      []string
	resourced map[string]string
}

// readAt is when the build's pages were read.
func (b *indexBuild) readAt() time.Time {
	if b.at.IsZero() {
		return time.Now().UTC()
	}
	return b.at.UTC()
}

// write sections the pages, stores the new and changed ones in batches
// and removes the pages no longer listed. Its report's Library carries
// the stored totals.
func (b *indexBuild) write(pages []webPage, failed []string) (IndexReport, error) {
	var report IndexReport
	stored, err := b.begin()
	if err != nil {
		return report, err
	}
	todo := b.diff(pages, stored, &report)
	if err := putPageBatches(b.store, b.key, todo); err != nil {
		return report, err
	}
	if err := b.store.TouchPages(b.key, b.same, b.readAt()); err != nil {
		return report, err
	}
	if err := b.store.SetPageSources(b.key, b.resourced); err != nil {
		return report, err
	}
	for _, pd := range todo {
		report.Library.Sections += pd.Page.Sections
	}
	gone := b.unlisted(pages, failed, stored, &report)
	if err := b.store.DeletePages(b.key, gone); err != nil {
		return report, err
	}
	report.Removed, report.Failed = len(gone), len(failed)
	report.Library.Pages = len(pages) + len(failed) - countMissing(failed, stored)
	b.say("stored %d pages: %d new, %d changed, %d unchanged, %d removed", len(pages), report.New, report.Changed, report.Unchanged, report.Removed)
	return report, nil
}

// begin reads what the store holds for the library: its metadata (a
// library in another index format is removed, to be rebuilt) and its
// pages.
func (b *indexBuild) begin() (map[string]docsindex.Page, error) {
	prev, found, err := b.store.Library(b.key)
	if err != nil {
		return nil, err
	}
	switch {
	case found && !prev.Current():
		b.say("index format changed: rebuilding")
		if _, err := b.store.Remove(b.key); err != nil {
			return nil, err
		}
	case found && BuiltBySearch(prev):
		b.say("replacing the pages earlier questions stored at this root")
		if _, err := b.store.Remove(b.key); err != nil {
			return nil, err
		}
		prev = docsindex.Library{}
	}
	b.prev = prev
	return b.store.Pages(b.key)
}

// diff sections every page (anchors corrected by the links of all of
// them, or with alone of each page's own) and returns the new and changed ones, ready to store; unchanged
// pages are counted with what they already hold. A page whose content is
// unchanged but which this build sections into a different number of
// sections than the store holds (an index an earlier ketch built, before
// coarse sections were split: Round 13) counts as changed and is stored
// again, so a rebuild needs no new format.
func (b *indexBuild) diff(pages []webPage, stored map[string]docsindex.Page, report *IndexReport) []docsindex.PageData {
	byPage := make([][]webSection, len(pages))
	if b.alone {
		for i, p := range pages {
			byPage[i] = pageSections(p)
		}
	} else {
		var sections []webSection
		for i, p := range pages {
			sections = append(sections, splitSections(p, i)...)
		}
		for _, s := range adoptLinkedAnchors(sections) {
			byPage[s.page] = append(byPage[s.page], s)
		}
	}
	var todo []docsindex.PageData
	for i, p := range pages {
		hash := pageHash(p)
		old, had := stored[p.URL]
		switch {
		case had && old.Hash == hash && old.Sections == len(byPage[i]):
			report.Unchanged++
			report.Library.Sections += old.Sections
			b.same = append(b.same, p.URL)
			if src := b.pageSource(p); old.Source != src {
				if b.resourced == nil {
					b.resourced = map[string]string{}
				}
				b.resourced[p.URL] = src
			}
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

// pageData is a page's record and sections, with their cross-version
// labels, ready to store. A page whose title or
// path names a migration to a later major labels every section; a page
// that does not, the sections whose own headings do.
func (b *indexBuild) pageData(p webPage, hash string, secs []webSection) docsindex.PageData {
	pd := docsindex.PageData{Page: docsindex.Page{URL: p.URL, Title: p.Title, Hash: hash, Sections: len(secs), Version: b.version, Fetched: b.readAt(), Source: b.pageSource(p)}}
	pageTexts := []string{p.Title, pathWords(p.URL, b.key)}
	for i, s := range secs {
		pd.Sections = append(pd.Sections, docsindex.Section{
			Page: p.URL, Order: i, Anchor: s.anchor, Heading: s.heading, Crumbs: s.crumbs, Text: s.text,
			CrossVersion: crossVersion(b.version, b.title, append(slices.Clone(pageTexts), s.crumbs...)),
		})
	}
	return pd
}

// How a page's text was read, as its stored source names it after where
// the page came in (pageSource): the freshness check reads it the same way.
const (
	readHTML  = "html"  // the page at its URL, through the page pipeline
	readMD    = "md"    // a Markdown source at the address that follows
	readChunk = "chunk" // a chunk of the llms file at the address that follows
)

// pageSource is the source a page is stored with: how it came in ("index"
// for a build, "search" for the pages a question read) and how its text
// was read: "index html", "index md https://x.test/a.md", "index chunk
// https://x.test/llms-full.txt".
func (b *indexBuild) pageSource(p webPage) string {
	source := b.source
	if source == "" {
		source = "index"
	}
	if p.via == "" {
		return source + " " + readHTML
	}
	return source + " " + p.via
}

// pageSections are a page's sections, its anchors corrected by its own
// links only: what --section finds from that page alone.
func pageSections(p webPage) []webSection { return adoptLinkedAnchors(splitSections(p, 0)) }

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
//
// Round 11 (design/docs-evidence.md) adds, from the same metadata: the one
// word a site name or title keeps once its descriptor words go (django of
// Django Project, gin of Gin Web Framework) and a site's name joined to a
// product's path (githubactions for GitHub at /en/actions/) as primary
// names; the word of a multi-word site name that is the host's one
// distinctive label (duo of Cisco Duo at duo.com) as a secondary name. A
// site-wide name (the site name, a host label) names a library documented
// under a path of its own only when the path names it too (namesRootPath),
// a host label several projects share names none of them, and neither a
// module path's owner (redis in /github.com/redis/go-redis/) nor a
// language label beside another (python in python.useinstructor.com) is a
// name.
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
	words := map[string]bool{}                                 // the words of every multi-word name
	title, _, _ := strings.Cut(cleanPageTitle(l.Title), " - ") // "FastAPI - FastAPI" is FastAPI
	addNameWords(words, title)
	addNameWords(words, l.SiteName)
	addNameWords(words, titleCore(cleanPageTitle(l.Document)))
	seen := map[string]bool{}
	root, _ := parseWebRoot(l.Root)
	paths := pathNames(root)
	var tails []string
	for _, n := range full {
		if k := libraryKey(n); n != l.SiteName || namesRootPath(k, paths) {
			addName(seen, &primary, k)
		}
		tail := n
		if i := strings.LastIndex(n, "/"); i >= 0 { // @scope/pkg, owner/repo, a module path
			tail = n[i+1:]
			tails = append(tails, tail)
		}
		addNameWords(words, tail)
	}
	for _, n := range []string{title, l.SiteName} {
		if k := descriptorName(n); k != "" && namesRootPath(k, paths) {
			addName(seen, &primary, k)
			delete(words, k)
		}
	}
	for _, k := range siteProductNames(title, l.SiteName, paths) {
		addName(seen, &primary, k)
	}
	if k := soleSiteWord(title, l.SiteName, root, paths); k != "" {
		addName(seen, &specific, k)
	}
	for _, n := range append(tails, paths...) {
		if k := libraryKey(n); !words[k] {
			addName(seen, &specific, k)
		}
	}
	for _, n := range hostNames(root.u) {
		if k := libraryKey(n); !words[k] && namesRootPath(k, paths) {
			addName(seen, &host, k)
		}
	}
	return primary, specific, host
}

// addNameWords adds to words the words of name when it has more than one.
func addNameWords(words map[string]bool, name string) {
	if parts := nameWords(name); len(parts) > 1 {
		for _, w := range parts {
			words[libraryKey(w)] = true
		}
	}
}

// addName appends k to into unless it is too short, generic or already
// seen in any tier.
func addName(seen map[string]bool, into *[]string, k string) {
	if len(k) >= 2 && !genericNames[k] && !seen[k] {
		seen[k] = true
		*into = append(*into, k)
	}
}

// siteProductNames are the names a site's name or title joined to each
// of its root's path names makes, in order, as keys.
func siteProductNames(title, siteName string, paths []string) []string {
	var out []string
	if len(paths) > 0 {
		// A site's name before a product's path names the product
		// (GitHub and /actions/: GitHub Actions; TanStack and /query/).
		for _, n := range []string{title, siteName} {
			site := libraryKey(n)
			if d := descriptorName(n); d != "" {
				site = d
			}
			if site != "" && !genericNames[site] {
				for _, p := range paths {
					if pk := libraryKey(p); pk != "" && !strings.HasPrefix(pk, site) {
						out = append(out, site+pk)
					}
				}
			}
		}
	}
	return out
}

// descriptorWords are the words a docs site's name adds to its product's
// name (Django Project, Gin Web Framework, Ansible Community, Heroku Dev
// Center, Grafana Labs, Temporal Platform, Polars user guide, Okta
// Developer). Library, SDK and API are not among them: Testing Library is
// not testing, AI SDK not ai.
var descriptorWords = map[string]bool{
	"doc": true, "docs": true, "documentation": true, "guide": true, "guides": true, "reference": true,
	"project": true, "community": true, "web": true, "framework": true, "dev": true, "developer": true,
	"developers": true, "center": true, "labs": true, "platform": true, "user": true, "official": true,
}

// descriptorName is the one word left of a multi-word site name or title
// once its descriptor words go (django of Django Project), as a key; ""
// when none or more than one is left: Vue Router stays two words.
func descriptorName(name string) string {
	parts := nameWords(name)
	if len(parts) < 2 {
		return ""
	}
	var kept []string
	for _, p := range parts {
		if k := libraryKey(p); !descriptorWords[k] {
			kept = append(kept, k)
		}
	}
	if len(kept) != 1 || len(kept[0]) < 2 {
		return ""
	}
	return kept[0]
}

// namesRootPath reports a site-wide name k that may name the library at
// the root with these path names: one whose path names nothing (the
// site's own docs), or names k (docs.ansible.com/projects/ansible/,
// grafana.com/docs/grafana/), k with a release number (/psycopg3/ for
// psycopg) or the end of k (/kit/ for SvelteKit). A site's name names
// a product documented under its own path only when it is the product's
// name: HashiCorp is not Terraform (developer.hashicorp.com/terraform/),
// TanStack not TanStack Query (tanstack.com/query/), Rust not one crate on
// docs.rs.
func namesRootPath(k string, paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, p := range paths {
		if pk := libraryKey(p); pk == k || strings.HasPrefix(pk, k) && isDigits(pk[len(k):]) || len(pk) >= 3 && strings.HasSuffix(k, pk) {
			return true
		}
	}
	return false
}

// soleSiteWord is the word of a multi-word site name or title that is
// the one distinctive label of the docs host, when the root's path names
// nothing (duo of Cisco Duo at duo.com/docs/, langchain of Docs by
// LangChain at docs.langchain.com); "" otherwise. Vue Router at
// router.vuejs.org has two such labels, Spring Boot at
// docs.spring.io/spring-boot/ and TanStack Query at tanstack.com/query/ a
// path naming the product: none gets one.
func soleSiteWord(title, siteName string, root webRoot, paths []string) string {
	if len(paths) > 0 {
		return ""
	}
	labels := map[string]bool{}
	for _, h := range hostNames(root.u) {
		if k := libraryKey(h); k != "" {
			labels[k] = true
		}
	}
	if len(labels) != 1 {
		return ""
	}
	for _, name := range []string{title, siteName} {
		parts := nameWords(name)
		if len(parts) < 2 {
			continue
		}
		for _, p := range parts {
			k := libraryKey(p)
			if len(k) < 3 || genericNames[k] || descriptorWords[k] || languageWords[k] {
				continue
			}
			if labels[k] {
				return k
			}
		}
	}
	return ""
}

// fullNames are a library's identities as written: its aliases, the name
// it was indexed by, its site's name and its packages (no ecosystem), and
// for a library of one document, that document's names.
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
	// A specification's own names, as keys (local_documents.go).
	full = append(full, documentNames(l)...)
	return full
}

// pathNoise are root path segments that name no library: docs
// directories, locales, versions, languages.
var pathNoise = map[string]bool{
	"docs": true, "doc": true, "documentation": true, "software": true, "en": true, "stable": true, "latest": true,
	"current": true, "oss": true, "python": true, "py": true, "js": true, "javascript": true, "typescript": true,
	"ts": true, "go": true, "golang": true, "rust": true, "java": true, "ruby": true, "php": true, "dotnet": true,
	"reference": true, "api": true, "guide": true, "manual": true, "html": true, "index": true, "home": true,
	"spec": true, "specs": true,
}

// pathNames are a root's path segments that may name its library (bs4 in
// /software/BeautifulSoup/bs4/doc/): all but docs directories, locales,
// versions and language names.
func pathNames(root webRoot) []string {
	if root.u == nil {
		return nil
	}
	var out []string
	segs := pathSegments(root.prefix)
	for i, seg := range segs {
		ls := strings.ToLower(seg)
		if _, version := segmentLabel(ls); pathNoise[ls] || version || localeRe.MatchString(ls) {
			continue
		}
		if modulePathOwner(segs, i) {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// modulePathOwner reports the segment at i a module path's host or owner
// (pkg.go.dev/github.com/redis/go-redis/v9/: github.com and redis): a
// segment that is a host, or follows one with more after it. Only the
// module's own name names it; redis is not go-redis.
func modulePathOwner(segs []string, i int) bool {
	for j := 0; j < i; j++ {
		if strings.Contains(segs[j], ".") {
			return i < lastNamed(segs)
		}
	}
	return strings.Contains(segs[i], ".") && i < lastNamed(segs)
}

// lastNamed is the index of the last segment that is not a version.
func lastNamed(segs []string) int {
	for i := len(segs) - 1; i >= 0; i-- {
		if _, version := segmentLabel(strings.ToLower(segs[i])); !version {
			return i
		}
	}
	return -1
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
	distinct := 0
	for _, l := range labels[:max(len(labels)-1, 0)] {
		if _, version := segmentLabel(l); !hostWords[l] && !version && !languageWords[l] {
			distinct++
		}
	}
	var out []string
	for _, l := range labels[:max(len(labels)-1, 0)] {
		if languageWords[l] && distinct > 0 {
			continue // python in python.useinstructor.com names the language, not Instructor
		}
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

// replacedBy lists the libraries a build at root (under key) replaces:
// another library at the same docs root, however it is written (a scheme,
// "www." or a trailing slash aside: sameRootURL), and every library an
// earlier ketch stored as a question's by-product (BuiltBySearch), which
// the first build after it removes.
func replacedBy(store *docsindex.Store, root webRoot, key string) ([]docsindex.Library, error) {
	libs, err := store.Libraries()
	if err != nil {
		return nil, err
	}
	var out []docsindex.Library
	for _, l := range libs {
		if l.Key != key && (BuiltBySearch(l) || sameRootURL(l.Root, root)) {
			out = append(out, l)
		}
	}
	return out, nil
}

// removeReplaced removes the libraries a build replaced, saying which.
func removeReplaced(store *docsindex.Store, libs []docsindex.Library, say func(string, ...any)) error {
	for _, l := range libs {
		if _, err := store.Remove(l.Key); err != nil {
			return err
		}
		if BuiltBySearch(l) {
			say("removed %s: pages earlier questions stored, which are no longer kept", l.Key)
		} else {
			say("removed %s: the same docs root as this library", l.Key)
		}
	}
	return nil
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
