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
	"unicode/utf8"

	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
	"github.com/1broseidon/ketch/internal/failure"
)

// Building a local docs index (ADR-0007): resolve the docs root once,
// enumerate its pages (local_plan.go), fetch them through the injected
// Fetcher (the scrape pipeline and page cache), section them as the web
// backend does, embed each section's windows and store the lot.

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
	Embedded  int     `json:"embedded"` // windows embedded by this run
	Seconds   float64 `json:"seconds"`
	Bytes     int64   `json:"index_bytes"` // the index file's size afterwards
}

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
	site, err := w.enumerate(dctx, target.root)
	if errors.Is(err, ErrNotFound) && target.resolved == "docs_url" && dctx.Err() == nil {
		if moved, ok := w.indexRedirect(dctx, target.root); ok {
			target.root = moved
			say("docs root redirects to %s", moved)
			site, err = w.enumerate(dctx, target.root)
		}
	}
	if err != nil {
		return IndexReport{}, err
	}
	plan := w.plan(dctx, target.root, site, req.MaxPages)
	say("pages: %d listed (%s); %d other versions' or locales' copies and %d disallowed by robots.txt left out; indexing %d%s",
		plan.listed, strings.Join(site.sources, ", "), plan.variants, plan.disallowed, len(plan.links), capNote(plan.capped, req.MaxPages))
	pages, failed := w.fetchIndexPages(ctx, target.root, plan.links, say)
	if len(pages) == 0 {
		return IndexReport{}, fmt.Errorf("web: none of the %d pages under %s could be fetched: %w", len(plan.links), target.root, ErrNotFound)
	}
	b := &indexBuild{store: store, key: target.root.String(), emb: req.Embedder, say: say}
	report, err := b.write(ctx, pages, failed)
	if err != nil {
		return report, err
	}
	lib := docsindex.Library{
		Key: b.key, Library: req.Library, Title: target.title, Root: target.root.String(), Version: target.version,
		Resolved: target.resolved, Evidence: target.evidence, Trust: target.trust, Packages: target.packages,
		Model: b.model, Dims: b.dims, Format: docsindex.FormatVersion, Built: time.Now().UTC(),
		Pages: report.Library.Pages, Sections: report.Library.Sections, Vectors: report.Library.Vectors,
		Discovered: plan.listed - plan.variants, Capped: plan.capped, Failed: len(failed), Sources: site.sources,
	}
	lib.Aliases = mergeNames(b.prev.Aliases, req.Names)
	lib.Names = indexNames(lib.Aliases, target)
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
	m, err := w.resolveOne(ctx, spec, false)
	if err != nil {
		return indexTarget{}, err
	}
	lm := m.libraryMatch()
	return indexTarget{root: m.root, version: m.version, title: m.title, resolved: "resolved", evidence: lm.Evidence, trust: m.trust, packages: m.packages, spec: spec}, nil
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
	for start := 0; start < len(todo); start += indexWriteBatch {
		if err := b.store.PutPages(b.key, todo[start:min(start+indexWriteBatch, len(todo))]); err != nil {
			return report, err
		}
	}
	for _, pd := range todo {
		report.Library.Sections += pd.Page.Sections
		report.Library.Vectors += pd.Page.Windows
	}
	gone := b.unlisted(pages, failed, stored, &report)
	if err := b.store.DeletePages(b.key, gone); err != nil {
		return report, err
	}
	report.Removed, report.Failed = len(gone), len(failed)
	report.Library.Pages = len(pages) + len(failed) - countMissing(failed, stored)
	b.say("stored %d pages: %d new, %d changed, %d unchanged, %d removed; %d windows embedded", len(pages), report.New, report.Changed, report.Unchanged, report.Removed, report.Embedded)
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
			continue
		case had:
			report.Changed++
		default:
			report.New++
		}
		todo = append(todo, pageData(p, hash, byPage[i]))
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

// pageData is a page's record and sections, ready to embed and store.
func pageData(p webPage, hash string, secs []webSection) docsindex.PageData {
	pd := docsindex.PageData{Page: docsindex.Page{URL: p.URL, Title: p.Title, Hash: hash, Sections: len(secs)}}
	for i, s := range secs {
		pd.Sections = append(pd.Sections, docsindex.Section{
			Page: p.URL, Order: i, Anchor: s.anchor, Heading: s.heading, Crumbs: s.crumbs, Text: s.text,
		})
	}
	return pd
}

// embed embeds every window of the pages' sections, in batches with
// progress, and records each page's window count.
func (b *indexBuild) embed(ctx context.Context, pages []docsindex.PageData) (int, error) {
	if b.emb == nil {
		return 0, nil
	}
	type ref struct{ page, sec int }
	var texts []string
	var refs []ref
	for i := range pages {
		for j, s := range pages[i].Sections {
			for _, t := range sectionWindows(s.Crumbs, s.Text) {
				texts = append(texts, t)
				refs = append(refs, ref{i, j})
			}
		}
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
			sec := &pages[r.page].Sections[r.sec]
			sec.Vectors = append(sec.Vectors, v)
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

// indexNames are the names a query may call the library by, as
// comparison keys (libraryKey): the aliases asked for (--name, kept
// across builds), the name it was indexed by and its packages' names, its
// title, and its docs host's distinctive labels (fastapi in
// fastapi.tiangolo.com, hono in hono.dev).
func indexNames(aliases []string, t indexTarget) []string {
	var raw []string
	raw = append(raw, aliases...)
	if t.spec.name != "" {
		raw = append(raw, t.spec.name)
	}
	for _, p := range t.packages {
		name := p
		if _, after, ok := strings.Cut(p, ":"); ok {
			name = after
		}
		raw = append(raw, name)
	}
	if title, _, _ := strings.Cut(cleanPageTitle(t.title), " - "); title != "" && len(title) <= 40 {
		raw = append(raw, title) // "FastAPI - FastAPI" is FastAPI
	}
	raw = append(raw, hostNames(t.root.u)...)
	seen := map[string]bool{}
	var out []string
	add := func(k string) {
		if len(k) >= 2 && !genericNames[k] && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for _, r := range raw {
		add(libraryKey(r))
		if i := strings.LastIndexAny(r, "/"); i >= 0 { // @scope/pkg, owner/repo, a module path
			add(libraryKey(r[i+1:]))
		}
	}
	return out
}

// hostWords are host labels that name no library.
var hostWords = map[string]bool{
	"www": true, "docs": true, "doc": true, "api": true, "dev": true, "developer": true, "developers": true,
	"readthedocs": true, "github": true, "gitlab": true, "pages": true, "netlify": true, "vercel": true,
	"rs": true, "pkg": true, "go": true, "org": true, "com": true, "net": true, "io": true, "app": true,
	"site": true, "guide": true, "learn": true, "en": true, "stable": true, "latest": true,
}

// hostNames are a docs host's labels that may name its library: all but
// the top-level domain and the words every docs host has.
func hostNames(u *url.URL) []string {
	if u == nil {
		return nil
	}
	labels := strings.Split(scopeHost(u), ".")
	var out []string
	for _, l := range labels[:max(len(labels)-1, 0)] {
		if !hostWords[l] {
			out = append(out, l)
		}
	}
	return out
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
	secs, err := store.Sections(key, 0, false)
	if err != nil {
		return IndexReport{}, err
	}
	pages := map[string]*docsindex.PageData{}
	var order []string
	for _, s := range secs {
		pd := pages[s.Page]
		if pd == nil {
			pd = &docsindex.PageData{Page: docsindex.Page{URL: s.Page}}
			pages[s.Page] = pd
			order = append(order, s.Page)
		}
		pd.Sections = append(pd.Sections, s)
	}
	list := make([]docsindex.PageData, 0, len(order))
	for _, u := range order {
		list = append(list, *pages[u])
	}
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
	for start := 0; start < len(updated); start += indexWriteBatch {
		if err := store.PutPages(key, updated[start:min(start+indexWriteBatch, len(updated))]); err != nil {
			return IndexReport{}, err
		}
	}
	lib.Model, lib.Dims, lib.Vectors, lib.Built = b.model, b.dims, n, time.Now().UTC()
	if err := store.PutLibrary(lib); err != nil {
		return IndexReport{}, err
	}
	return IndexReport{Library: lib, Embedded: n, Unchanged: len(list), Seconds: time.Since(start).Seconds(), Bytes: store.Size()}, nil
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
