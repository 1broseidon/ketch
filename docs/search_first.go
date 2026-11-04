package docs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/failure"
	"github.com/1broseidon/ketch/search"
)

// The search-first docs backend (`-b search`) answers a question about
// any documentation, not only a package's. The other backends start from
// a library name and look for its docs; this one starts from the
// question:
//
//	the local index covers it? ──yes──▶ answer offline (local)
//	        │no
//	web search (the question, and the question + "documentation")
//	        │
//	docs pages only; the site most of their weight agrees on
//	        │       (none: no_docs_found; split: ambiguous_site)
//	fetch the hit pages and close siblings from the site's own lists
//	        │       (≤ sfMaxPages, through the page cache)
//	section them, store them in the local index under the docs root,
//	embed the sections the question's words rank first (a window budget)
//	        │
//	rank with the local index's hybrid ranker, render as -b local does
//	        └──▶ partial: N pages read, and how to index the whole site
//
// What it reads stays in the index, so the next question those pages
// answer is answered offline. A library the request names (--library, or
// name@version in the question) is a preference among the hits' sites,
// never a requirement: a site its name or its resolved docs root matches
// weighs more, and the version it asks for picks the tree.

// SearchBackend is the search-first backend's ID.
const SearchBackend = "search"

// ReasonPartialSite is a search-first answer's reason: it read part of a
// docs site, found by search.
const ReasonPartialSite = "partial_site"

const (
	sfMaxPages       = 24 // pages read per question
	sfMaxHitPages    = 8  // of them, search hits
	sfNearPages      = 6  // of them, siblings of a hit in the site's nav
	sfSearchResults  = 10
	sfTimeout        = 45 * time.Second
	sfSearchTimeout  = 15 * time.Second
	sfResolveTimeout = 10 * time.Second
	sfDiscoverBudget = 6 * time.Second
	sfPageTimeout    = 15 * time.Second
	sfConcurrency    = 6
	// sfEmbedWindows is how many section windows one question embeds,
	// the sections its words rank first; the rest are stored as text and
	// embedded by later questions that rank them (about 20 windows a
	// second with bge-base).
	sfEmbedWindows = 96
	// sfSectionWindows caps one section's windows embedded per question.
	sfSectionWindows = 4
	// sfLocalFloor is the cosine a stored section must reach for the
	// local index to answer without searching.
	sfLocalFloor = 0.80
)

// SearchFirst is the search-first backend.
type SearchFirst struct {
	web   *Web
	local *Local
	// searchDocs runs one web search; web.sites unless a test sets it.
	maxPages, maxHits, embedWindows int
	localFloor                      float64
	timeout                         time.Duration
	trace                           io.Writer // nil: no trace
}

// NewSearchFirst builds the backend over a web backend (its fetcher,
// search and registries) and a local backend (its index and model).
func NewSearchFirst(w *Web, l *Local) *SearchFirst {
	w.concurrency = sfConcurrency
	w.sitemapBudget = sfDiscoverBudget / 2
	w.probeTimeout = sfDiscoverBudget
	return &SearchFirst{web: w, local: l, maxPages: sfMaxPages, maxHits: sfMaxHitPages, embedWindows: sfEmbedWindows,
		localFloor: sfLocalFloor, timeout: sfTimeout}
}

func newSearchFirstFromConfig(c *config.Config, deps Deps) *SearchFirst {
	s := NewSearchFirst(newWebFromConfig(c, deps), newLocalFromConfig(c))
	if os.Getenv("KETCH_DOCS_TRACE") != "" {
		s.trace = os.Stderr
	}
	return s
}

var _ SelectingSearcher = (*SearchFirst)(nil)

// Search answers a question; see SearchSelect.
func (s *SearchFirst) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	o := s.SearchSelect(ctx, query, limit)
	return o.Results, o.Err()
}

// GetDocs answers a question about a library; see GetDocsSelect.
func (s *SearchFirst) GetDocs(ctx context.Context, library, query string, tokens int) ([]Result, error) {
	o := s.GetDocsSelect(ctx, library, query, tokens)
	return o.Results, o.Err()
}

// ResolveLibrary resolves a name as the web backend does.
func (s *SearchFirst) ResolveLibrary(ctx context.Context, name string, limit int) ([]LibraryMatch, error) {
	return s.web.ResolveLibrary(ctx, name, limit)
}

// ResolveSelect resolves a name as the web backend does.
func (s *SearchFirst) ResolveSelect(ctx context.Context, name string, limit int) Outcome {
	matches, err := s.web.ResolveLibrary(ctx, name, limit)
	return Resolved("web", matches, err)
}

// SearchSelect answers a question from the local index when it covers
// it, else from the docs site its web search agrees on.
func (s *SearchFirst) SearchSelect(ctx context.Context, query string, limit int) Outcome {
	opts, _ := localOptionsFrom(ctx)
	if opts.Limit <= 0 {
		opts.Limit = limit
	}
	req := sfRequest{query: query, opts: opts}
	if spec, ok := pinnedSpec(query); ok {
		req.spec = spec
	}
	o := s.answer(ctx, req)
	o.Results = Truncate(o.Results, limit)
	return o
}

// GetDocsSelect answers a question about a library: a docs URL is the
// site (searched within, its lists read when search finds nothing
// there); a name or spec is a preference among the sites search finds.
func (s *SearchFirst) GetDocsSelect(ctx context.Context, library, query string, tokens int) Outcome {
	opts, ok := localOptionsFrom(ctx)
	if !ok {
		opts.Tokens = tokens
	}
	req := sfRequest{query: query, library: strings.TrimSpace(library), opts: opts}
	if req.library != "" && !isHTTPURL(req.library) {
		spec, err := parseLibSpec(req.library)
		if err != nil {
			spec = libSpec{raw: req.library, name: req.library}
		}
		req.spec = spec
	}
	return s.answer(ctx, req)
}

// sfRequest is one question and what it names.
type sfRequest struct {
	query   string
	library string  // as given: a docs URL, a spec or a name; "" for none
	spec    libSpec // the library as a spec (zero for a docs URL or none)
	opts    LocalOptions
}

// pinnedSpec is the library a question pins with a spec of its own
// (name@version, an ecosystem prefix), as queryPinsLibrary reads it.
func pinnedSpec(query string) (libSpec, bool) {
	for _, w := range strings.Fields(query) {
		w = cleanQueryWord(w)
		spec, ok := explicitSpec(w)
		if !ok || spec.url != "" {
			continue
		}
		if spec.version != "" || spec.eco != ecoAny && strings.Contains(w, ":") {
			return spec, true
		}
	}
	return libSpec{}, false
}

// answer is the whole state machine: the local index, else search.
func (s *SearchFirst) answer(ctx context.Context, req sfRequest) Outcome {
	start := time.Now()
	tr := &sfTrace{}
	defer func() { tr.write(s.trace, req, time.Since(start)) }()
	if o, ok := s.fromIndex(ctx, req, tr); ok {
		tr.path = "local"
		return o
	}
	tr.path = "search"
	return s.fromSearch(ctx, req, tr)
}

// fromSearch answers from the site the question's search agrees on.
func (s *SearchFirst) fromSearch(ctx context.Context, req sfRequest, tr *sfTrace) Outcome {
	ctx = withProfileMemo(withFetchMemo(ctx))
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	hits, prefs, err := s.searchHits(cctx, req)
	tr.hits = len(hits)
	if err != nil {
		return s.failed(ctx, cctx, err)
	}
	docs := docsHits(hits)
	tr.docsHits = len(docs)
	site, prof, err := s.chooseSite(cctx, chooseSites(docs, prefs), req, prefs)
	if err != nil {
		return s.failed(ctx, cctx, err)
	}
	tr.site, tr.siteHits = site.root.String(), len(site.hits)
	links := s.plan(cctx, site, prof, req.query)
	pages, failed := s.fetch(cctx, site.root, links)
	tr.planned, tr.fetched, tr.failed = len(links), len(pages), failed
	if len(pages) == 0 {
		return s.failed(ctx, cctx, fmt.Errorf("search: none of the %d pages of %s could be fetched: %w", len(links), site.root, ErrNotFound))
	}
	lib, st, err := s.keep(ctx, site, prof, pages, req.query)
	tr.stored = st
	if err != nil {
		return Answer(SearchBackend, nil, err)
	}
	o := s.local.rank(ctx, lib, req.query, req.opts)
	return searchAnswered(o, lib, len(pages), st)
}

// failed is a search-first call that ended before an answer, classified
// as the web backend classifies one (a timeout is upstream).
func (s *SearchFirst) failed(parent, op context.Context, err error) Outcome {
	if parent.Err() == nil && op.Err() != nil {
		err = fmt.Errorf("search: no answer within %s: %w", s.timeout, err)
	}
	return Answer(SearchBackend, nil, err)
}

// searchAnswered is the local ranker's answer as the search-first
// backend's: partial, saying how much of the site it read and how to
// index the rest.
func searchAnswered(o Outcome, lib docsindex.Library, read int, st sfStored) Outcome {
	if o.Status != Answered && o.Status != Partial {
		for i := range o.Attempts {
			o.Attempts[i].Backend = SearchBackend
		}
		return o
	}
	for i := range o.Results {
		o.Results[i].Source = SearchBackend
	}
	note := fmt.Sprintf("read %d pages of %s found by web search (%d new to the local index, %d sections embedded); the whole site: ketch docs index %s",
		read, lib.Root, st.newPages, st.embedded, lib.Root)
	return asPartial(o, SearchBackend, note)
}

// asPartial marks an answer partial with a note, as backend's.
func asPartial(o Outcome, backend, note string) Outcome {
	o.Status, o.Backend = Partial, backend
	if o.Reason == "" {
		o.Reason = ReasonPartialSite
	}
	if o.Note != "" {
		note = o.Note + "; " + note
	}
	o.Note = note
	for i := range o.Attempts {
		o.Attempts[i].Backend, o.Attempts[i].Status, o.Attempts[i].Reason = backend, Partial, o.Reason
	}
	return o
}

// searchHits runs the question's searches concurrently (and, for a
// library it names, its resolution, a preference) and fuses their hits.
// It fails only when every search failed.
func (s *SearchFirst) searchHits(ctx context.Context, req sfRequest) ([]sfHit, sitePrefs, error) {
	if s.web.sites == nil {
		return nil, sitePrefs{}, fmt.Errorf("%w: the search-first backend needs a web search", ErrLibraryRequired)
	}
	queries := searchQueries(req)
	lists := make([][]search.Result, len(queries))
	errs := make([]error, len(queries))
	var prefs sitePrefs
	var wg sync.WaitGroup
	sctx, cancel := context.WithTimeout(ctx, sfSearchTimeout)
	defer cancel()
	for i, q := range queries {
		wg.Go(func() { lists[i], errs[i] = s.web.sites.Search(sctx, q, sfSearchResults) })
	}
	wg.Go(func() { prefs = s.prefs(ctx, req) })
	wg.Wait()
	var firstErr error
	answered := false
	for i := range queries {
		answered = answered || errs[i] == nil
		firstErr = firstNonNil(firstErr, errs[i])
	}
	hits := fuseHits(lists)
	switch {
	case !answered:
		return nil, prefs, fmt.Errorf("search: web search failed: %w", firstErr)
	case len(hits) == 0:
		// Every search answered with nothing: for a question in words, the
		// search providers are throttling, not the web lacking a page.
		return nil, prefs, fmt.Errorf("search: %w", errSearchEmpty)
	}
	return hits, prefs, nil
}

// errSearchEmpty is web searches that all answered with no results.
var errSearchEmpty = failure.New(failure.Upstream, "search_empty", "the web search returned no results")

func firstNonNil(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// searchQueries are the searches a request runs: the question (led by
// the library it names, when it does not already say it), and the same
// asking for documentation. A docs URL's host leads both.
func searchQueries(req sfRequest) []string {
	q := strings.Join(strings.Fields(req.query), " ")
	lead := ""
	switch {
	case isHTTPURL(req.library):
		if r, err := parseWebRoot(req.library); err == nil {
			lead = strings.TrimSuffix(scopeHost(r.u)+r.prefix, "/")
		}
	case req.spec.name != "":
		lead = req.spec.name
		if req.spec.version != "" {
			lead += " " + req.spec.version
		}
	}
	if lead != "" && !strings.Contains(strings.ToLower(q), strings.ToLower(lead)) {
		q = lead + " " + q
	}
	return []string{q, q + " documentation"}
}

// prefs are the sites a request prefers: the docs URL it gives; for a
// library it names, sites its name labels, and the docs root a
// resolution finds within sfResolveTimeout (a pinned spec only: a bare
// name's registries are a guess the hits outvote anyway).
func (s *SearchFirst) prefs(ctx context.Context, req sfRequest) sitePrefs {
	var p sitePrefs
	if isHTTPURL(req.library) {
		if r, err := parseWebRoot(req.library); err == nil {
			p.roots = append(p.roots, r)
		}
		return p
	}
	if req.spec.name == "" {
		return p
	}
	p.version = req.spec.version
	for _, n := range []string{req.spec.name, lastPart(req.spec.name)} {
		if k := libraryKey(n); k != "" && !genericNames[k] && !languageWords[k] {
			p.names = append(p.names, k)
		}
	}
	if req.spec.version == "" && req.spec.eco == ecoAny || !s.web.resolvesNames() {
		return p
	}
	rctx, cancel := context.WithTimeout(ctx, sfResolveTimeout)
	defer cancel()
	if m, err := s.web.resolveOne(rctx, req.spec, false); err == nil {
		p.roots = append(p.roots, m.root)
	}
	return p
}

// lastPart is a scoped name's or path's last part (core in @angular/core).
func lastPart(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// chooseSite picks the site and reads its top hit's profile (its nav
// lists the siblings): a site with no docs signal in its hits must look
// like docs there (looksLikeDocs), or the next site is tried. A docs URL
// the request gives is the site, whatever the hits say.
func (s *SearchFirst) chooseSite(ctx context.Context, sites []sfSite, req sfRequest, prefs sitePrefs) (sfSite, *siteProfile, error) {
	if isHTTPURL(req.library) && len(prefs.roots) > 0 {
		site := sfSite{key: prefs.roots[0].String(), root: prefs.roots[0], docs: true}
		for _, c := range sites {
			for _, h := range c.hits {
				if site.root.contains(h.u) {
					site.hits = append(site.hits, h)
				}
			}
		}
		return site, s.hitProfile(ctx, site), nil
	}
	for tries := 0; tries < 3; tries++ {
		site, err := pickSite(sites)
		if err != nil {
			return sfSite{}, nil, err
		}
		prof := s.hitProfile(ctx, site)
		if site.docs || prof != nil && prof.served != nil && looksLikeDocs(prof.served, prof) {
			return site, prof, nil
		}
		sites = sites[1:]
	}
	return sfSite{}, nil, fmt.Errorf("search: %w", ErrNoDocsFound)
}

// hitProfile is the site's top hit's page profile, nil when it cannot be
// read.
func (s *SearchFirst) hitProfile(ctx context.Context, site sfSite) *siteProfile {
	u := site.root.u
	if len(site.hits) > 0 {
		u = site.hits[0].u
	}
	prof, err := s.web.cachedProfile(ctx, u)
	if err != nil {
		return nil
	}
	return prof
}

func probeSearchFirst(context.Context, *http.Client, *config.Config) (health.Status, string) {
	return health.StatusOK, "keyless: web search, then the docs site it finds, kept in the local index"
}

func searchFirstProvider() Provider {
	return Provider{
		ID:          SearchBackend,
		Name:        "Search-first",
		LibraryHelp: "a docs URL to search within, or a library name or name@version to prefer",
		Usable:      func(*config.Config) bool { return true },
		New:         func(c *config.Config, deps Deps) (Searcher, error) { return newSearchFirstFromConfig(c, deps), nil },
		Probe:       probeSearchFirst,
	}
}
