package docs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
)

// The local index as the search-first backend's by-product. Pages it
// reads are stored under the library whose docs root holds the site's
// root (an indexed library, or one a search built), or a new library at
// that root, marked as built by search (Resolved "search": the local
// backend answers from it as partial). Nothing is removed: a library
// built by search only grows. Embedding is budgeted per question: the
// sections without vectors that the question's words rank first get
// their first windows embedded (sfEmbedWindows in all), so the cost of a
// cold question stays in seconds, and every later question that ranks
// other sections embeds those.

// resolvedBySearch is a library's Resolved when the search-first backend
// built it.
const resolvedBySearch = "search"

// errNoIndex is a search-first call with nowhere to keep pages.
var errNoIndex = errors.New("search: no local docs index (no cache directory)")

// keep stores the fetched pages, embeds within the budget, and returns
// the library as stored.
func (s *SearchFirst) keep(ctx context.Context, site sfSite, prof *siteProfile, pages []webPage, query string) (docsindex.Library, sfStored, error) {
	var st sfStored
	store := s.local.store
	if store == nil {
		return docsindex.Library{}, st, errNoIndex
	}
	lib, err := s.libraryFor(store, site, prof)
	if err != nil {
		return lib, st, err
	}
	emb := s.embedderFor(&lib)
	stored, err := store.Pages(lib.Key)
	if err != nil {
		return lib, st, err
	}
	b := &indexBuild{store: store, key: lib.Key, version: lib.Version, title: lib.Title}
	var report IndexReport
	todo := b.diff(pages, stored, &report)
	st.newPages, st.changed, st.unchanged = report.New, report.Changed, report.Unchanged
	if err := putPageBatches(store, lib.Key, todo); err != nil {
		return lib, st, err
	}
	secs, err := store.Sections(lib.Key, lib.Dims, true)
	if err != nil {
		return lib, st, err
	}
	if emb != nil {
		start := time.Now()
		if st.embedded, err = embedSome(ctx, emb, store, lib.Key, secs, query, s.embedWindows); err != nil {
			return lib, st, err
		}
		st.embedTime = time.Since(start)
	}
	if lib, err = withCounts(store, lib, secs); err != nil {
		return lib, st, err
	}
	return lib, st, store.PutLibrary(lib)
}

// libraryFor is the library the site's pages go to: the indexed one
// whose root holds the site's root (the narrowest), else a new one at it.
func (s *SearchFirst) libraryFor(store *docsindex.Store, site sfSite, prof *siteProfile) (docsindex.Library, error) {
	var libs []docsindex.Library
	if store.Exists() {
		var err error
		if libs, err = store.Libraries(); err != nil {
			return docsindex.Library{}, fmt.Errorf("search: reading the docs index: %w", err)
		}
	}
	var best *docsindex.Library
	for i, l := range libs {
		r, err := parseWebRoot(l.Root)
		if err != nil || !l.Current() || !r.contains(site.root.u) {
			continue
		}
		if best == nil || len(l.Root) > len(best.Root) {
			best = &libs[i]
		}
	}
	if best != nil {
		return *best, nil
	}
	root := site.root.String()
	label, _ := rootVersion(site.root)
	lib := docsindex.Library{
		Key: root, Library: root, Root: root, Version: label, Resolved: resolvedBySearch,
		Evidence: []string{"pages found by web search for a question, and their siblings"},
		Format:   docsindex.FormatVersion, Capped: true, Sources: []string{"web search"},
	}
	if prof != nil {
		lib.SiteName = prof.meta["og:site_name"]
	}
	lib.Title = firstNonEmpty(lib.SiteName, scopeHost(site.root.u))
	return lib, nil
}

// embedderFor is the model to embed the library's sections with: the
// installed one, when the library has no vectors yet or was embedded
// with it; nil otherwise (the library is then ranked by keywords).
func (s *SearchFirst) embedderFor(lib *docsindex.Library) embed.Embedder {
	if s.local.model == nil {
		return nil
	}
	emb, err := s.local.model()
	if err != nil || emb == nil {
		return nil
	}
	switch {
	case lib.Model == "" && lib.Vectors == 0:
		lib.Model, lib.Dims = emb.ID(), emb.Dim()
	case lib.Model != emb.ID():
		return nil
	}
	return emb
}

// embedSome embeds the first windows of the sections without vectors
// that the query ranks first by keywords, budget windows in all, and
// stores their vectors; secs is updated in place. It returns how many
// sections it embedded.
func embedSome(ctx context.Context, emb embed.Embedder, store *docsindex.Store, key string, secs []docsindex.Section, query string, budget int) (int, error) {
	var bare []int
	for i, sec := range secs {
		if len(sec.Vectors) == 0 {
			bare = append(bare, i)
		}
	}
	if len(bare) == 0 || budget <= 0 {
		return 0, nil
	}
	sub := make([]docsindex.Section, len(bare))
	for i, j := range bare {
		sub[i] = secs[j]
	}
	var texts []string
	var owner []int // the section each text is a window of
	var chosen []int
	for _, i := range embedOrder(sub, bare, query) {
		win := sectionWindows(secs[i].Crumbs, secs[i].Text)
		win = win[:min(len(win), sfSectionWindows)]
		if len(texts)+len(win) > budget {
			if len(texts) > 0 {
				break
			}
			win = win[:budget]
		}
		chosen = append(chosen, i)
		for _, t := range win {
			texts = append(texts, t)
			owner = append(owner, i)
		}
	}
	if len(texts) == 0 {
		return 0, nil
	}
	vecs, err := emb.EmbedPassages(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("search: embedding: %w", err)
	}
	for k, v := range vecs {
		secs[owner[k]].Vectors = append(secs[owner[k]].Vectors, v)
	}
	put := make([]docsindex.Section, 0, len(chosen))
	for _, i := range chosen {
		put = append(put, secs[i])
	}
	return len(chosen), store.PutVectors(key, put)
}

// embedOrder is the order sections without vectors (sub, at indices
// bare) are embedded in: those the query's words rank, best first, then
// the rest in page order, so a question none of whose words a section
// names still has its pages' sections ranked by meaning.
func embedOrder(sub []docsindex.Section, bare []int, query string) []int {
	var out []int
	ranked := map[int]bool{}
	for _, r := range rankSections(webSections(sub), query) {
		out = append(out, bare[r.page])
		ranked[r.page] = true
	}
	for k, i := range bare {
		if !ranked[k] {
			out = append(out, i)
		}
	}
	return out
}

// withCounts is the library with its stored totals: pages, sections,
// vectors, units.
func withCounts(store *docsindex.Store, lib docsindex.Library, secs []docsindex.Section) (docsindex.Library, error) {
	pages, err := store.Pages(lib.Key)
	if err != nil {
		return lib, err
	}
	lib.Pages, lib.Sections, lib.Vectors, lib.Units = len(pages), len(secs), 0, 0
	for _, s := range secs {
		lib.Vectors += len(s.Vectors)
		lib.Units += len(s.Units)
	}
	lib.Built = time.Now().UTC()
	if lib.Resolved == resolvedBySearch {
		lib.Discovered = lib.Pages
	}
	lib.Names = IndexedNames(lib)
	return lib, nil
}
