package docs

import (
	"errors"
	"fmt"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// The local index as the search-first backend's by-product. Pages it
// reads are stored under the library whose docs root holds the site's
// root (an indexed library, or one a search built), or a new library at
// that root, marked as built by search (Resolved "search": the local
// backend answers from it as partial). Nothing is removed: a library
// built by search only grows.

// resolvedBySearch is a library's Resolved when the search-first backend
// built it.
const resolvedBySearch = "search"

// errNoIndex is a search-first call with nowhere to keep pages.
var errNoIndex = errors.New("search: no local docs index (no cache directory)")

// keep stores the fetched pages and returns the library as stored.
func (s *SearchFirst) keep(site sfSite, prof *siteProfile, pages []webPage) (docsindex.Library, sfStored, error) {
	var st sfStored
	store := s.local.store
	if store == nil {
		return docsindex.Library{}, st, errNoIndex
	}
	lib, err := s.libraryFor(store, site, prof)
	if err != nil {
		return lib, st, err
	}
	stored, err := store.Pages(lib.Key)
	if err != nil {
		return lib, st, err
	}
	b := &indexBuild{store: store, key: lib.Key, version: lib.Version, title: lib.Title, source: resolvedBySearch}
	var report IndexReport
	todo := b.diff(pages, stored, &report)
	st.newPages, st.changed, st.unchanged = report.New, report.Changed, report.Unchanged
	if err := putPageBatches(store, lib.Key, todo); err != nil {
		return lib, st, err
	}
	if lib, err = withCounts(store, lib); err != nil {
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

// withCounts is the library with its stored totals: pages and sections,
// from the pages' counts.
func withCounts(store *docsindex.Store, lib docsindex.Library) (docsindex.Library, error) {
	pages, err := store.Pages(lib.Key)
	if err != nil {
		return lib, err
	}
	lib.Pages, lib.Sections = len(pages), 0
	for _, p := range pages {
		lib.Sections += p.Sections
	}
	lib.Built = time.Now().UTC()
	if lib.Resolved == resolvedBySearch {
		lib.Discovered = lib.Pages
	}
	lib.Names = IndexedNames(lib)
	return lib, nil
}
