package docs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/failure"
	"github.com/1broseidon/ketch/search"
)

// The search docs backend (`search`, the default) answers a question
// about any documentation, not only a package's. Context7 starts from a
// library name and looks up its docs; this one starts from the question:
//
//	the request names an indexed library? ──yes──▶ answer from the index (local)
//	        │no
//	the same question read within cache_ttl? ──yes──▶ the same site and pages, from the page cache
//	        │no
//	web search (the question, and the question + "documentation")
//	        │
//	docs pages only; the site most of their weight agrees on
//	        │       (none: no_docs_found; split: ambiguous_site)
//	an indexed library holds the site? ──yes──▶ answer from the index
//	        │no
//	fetch the hit pages and close siblings from the site's own lists
//	        │       (≤ sfMaxPages, through the page cache)
//	section them into a library held in memory for this call
//	        │
//	answer through the local index's evidence pipeline
//	        └──▶ partial: N pages read, and how to index the whole site
//
// A question saves nothing (search_read.go): the pages it read stay in the
// page cache under cache_ttl, with what a repeat needs to skip the search,
// and `ketch docs index` is the only command that writes the index. An
// answer from the index checks the pages it cites first (index_fresh.go).
// A library the request names (--library, or name@version in the
// question) is a preference among the hits' sites, never a requirement: a
// site its name or its resolved docs root matches weighs more, and the
// version it asks for picks the tree.

// SearchBackend is the search backend's ID: the default docs backend.
const SearchBackend = "search"

// SelectingSearcher is a docs backend that reports each call as an
// Outcome, with the reason a refusal or a partial answer carries: the
// search backend.
type SelectingSearcher interface {
	Searcher
	LibraryResolver
	SearchSelect(ctx context.Context, query string, limit int) Outcome
	GetDocsSelect(ctx context.Context, libraryID, query string, tokens int) Outcome
	ResolveSelect(ctx context.Context, name string, limit int) Outcome
}

// libraryKey is a name reduced for comparison: lower case, letters and
// digits only, without a trailing "js" (Next.js, nextjs and next match).
func libraryKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	k := b.String()
	if trimmed := strings.TrimSuffix(k, "js"); len(trimmed) >= 2 {
		k = trimmed
	}
	return k
}

// genericNames are words a title or repository can be ("docs", "api")
// that name no library on their own.
var genericNames = map[string]bool{
	"doc": true, "docs": true, "documentation": true, "api": true, "apis": true, "reference": true,
	"guide": true, "guides": true, "website": true, "site": true, "sdk": true, "library": true,
}

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
)

// SearchFirst is the search-first backend.
type SearchFirst struct {
	web               *Web
	local             *Local
	maxPages, maxHits int
	timeout           time.Duration
	trace             io.Writer // nil: no trace
	// ttl is cache_ttl: how long a page read counts as current. An indexed
	// page last read longer ago is read again before an answer cites it.
	ttl time.Duration
	now func() time.Time
	// checkTimeout bounds the check of an indexed answer's cited pages
	// (freshCheckTimeout).
	checkTimeout time.Duration
}

// sfDefaultTTL is the page cache's TTL when cache_ttl does not parse, as
// the page cache takes it (cache.NewFromConfig).
const sfDefaultTTL = time.Hour

// NewSearchFirst builds the backend over the web internals (fetcher, web
// search, registries, discovery) and the local index.
func NewSearchFirst(w *Web, l *Local) *SearchFirst {
	w.concurrency = sfConcurrency
	w.sitemapBudget = sfDiscoverBudget / 2
	w.probeTimeout = sfDiscoverBudget
	return &SearchFirst{web: w, local: l, maxPages: sfMaxPages, maxHits: sfMaxHitPages, timeout: sfTimeout,
		ttl: sfDefaultTTL, now: time.Now, checkTimeout: freshCheckTimeout}
}

func newSearchFirstFromConfig(c *config.Config, deps Deps) *SearchFirst {
	s := NewSearchFirst(newWebFromConfig(c, deps), newLocalFromConfig())
	if ttl, err := time.ParseDuration(c.CacheTTL); err == nil && ttl > 0 {
		s.ttl = ttl
	}
	if os.Getenv("KETCH_DOCS_TRACE") != "" {
		s.trace = os.Stderr
	}
	return s
}

var (
	_ SelectingSearcher = (*SearchFirst)(nil)
	_ SectionFetcher    = (*SearchFirst)(nil)
)

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

// ResolveLibrary resolves a name to its docs root (Web.ResolveLibrary).
func (s *SearchFirst) ResolveLibrary(ctx context.Context, name string, limit int) ([]LibraryMatch, error) {
	return s.web.ResolveLibrary(ctx, name, limit)
}

// ResolveSelect resolves a name to its docs root (Web.ResolveLibrary).
func (s *SearchFirst) ResolveSelect(ctx context.Context, name string, limit int) Outcome {
	matches, err := s.web.ResolveLibrary(ctx, name, limit)
	return Resolved(SearchBackend, matches, err)
}

// SearchSelect answers a question from the local index when it covers
// it, else from the docs site its web search agrees on. A caller that
// carries its options (WithLocalOptions: the CLI, MCP) caps the results
// only by the limit they give, the budget deciding otherwise; any other
// caller's limit caps them.
func (s *SearchFirst) SearchSelect(ctx context.Context, query string, limit int) Outcome {
	opts, ok := localOptionsFrom(ctx)
	if !ok {
		opts.Limit = limit
	}
	req := sfRequest{query: query, opts: opts}
	if spec, ok := pinnedSpec(query); ok {
		req.spec = spec
	}
	o := s.answer(ctx, req)
	o.Results = Truncate(o.Results, opts.Limit)
	return o
}

// GetDocsSelect answers a question about a library: a docs URL is the
// site (searched within, its lists read when search finds nothing
// there); a name or spec is a preference among the sites search finds.
func (s *SearchFirst) GetDocsSelect(ctx context.Context, library, query string, tokens int) Outcome {
	if isContext7ID(strings.TrimSpace(library)) {
		return Answer(SearchBackend, nil, fmt.Errorf("search: %q is a Context7 library ID: ask Context7 with -b context7 (MCP backend \"context7\"), or give search a library name, spec or docs URL: %w", library, errContext7ID))
	}
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

// errContext7ID is a Context7 library ID given to search, which ketch
// 0.20 sent to Context7 by default: the caller names the backend.
var errContext7ID = failure.New(failure.Validation, "context7_id", "a Context7 library ID needs the context7 backend")

// isContext7ID reports a Context7 library ID: /owner/repo, optionally with
// a version segment. No web library has that shape (a docs URL has a
// scheme, a spec no leading slash).
func isContext7ID(s string) bool {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") {
		return false
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.ContainsAny(p, " \t?#:") {
			return false
		}
	}
	return true
}

// sfRequest is one question and what it names.
type sfRequest struct {
	query   string
	library string       // as given: a docs URL, a spec or a name; "" for none
	spec    libSpec      // the library as a spec (zero for a docs URL or none)
	opts    LocalOptions // as the caller gave them
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

// answer is the whole state machine: the local index, else the question's
// read within cache_ttl, else search.
func (s *SearchFirst) answer(ctx context.Context, req sfRequest) Outcome {
	start := time.Now()
	tr := &sfTrace{}
	defer func() { tr.write(s.trace, req, time.Since(start)) }()
	if o, ok := s.fromIndex(ctx, req, tr); ok {
		tr.path = "local"
		TraceFrom(ctx).setPath(tr.path)
		return o
	}
	tr.path = "search"
	TraceFrom(ctx).setPath(tr.path)
	if o, ok := s.fromRead(ctx, req, tr); ok {
		return o
	}
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
	sites := chooseSites(docs, prefs)
	tr.cands = siteList(sites)
	var o Outcome
	for range sfMaxReads {
		var site sfSite
		var prof *siteProfile
		site, prof, sites, err = s.chooseSite(cctx, sites, req, prefs)
		if err != nil {
			if o.Status != "" {
				return o // the site read before says more than the rest's refusal
			}
			return s.failed(ctx, cctx, err)
		}
		if !site.retry {
			tr.domains++
		}
		o = s.readSite(ctx, cctx, site, prof, req, tr)
		if answered(o) || cctx.Err() != nil {
			return o
		}
		if len(site.alts) > 0 {
			// Nothing readable in this docs tree (JS shells no browser
			// rendered, robots-closed, nothing matching): the domain's next
			// tree before another domain.
			next := site.alts[0]
			next.alts, next.retry = site.alts[1:], true
			sites = append([]sfSite{next}, sites...)
			continue
		}
		if o.Reason == reasonDisallowed || len(sites) == 0 || tr.domains >= sfMaxSites {
			return o // a site that closes its docs to robots is still the site
		}
	}
	return o
}

// sfMaxSites is how many sites (domains) one question reads: the next is
// read when the first gave nothing (its pages unreadable, a JS shell no
// browser rendered, or nothing on them matched). sfMaxReads bounds the
// docs trees read in all, a site's other trees included.
const (
	sfMaxSites = 2
	sfMaxReads = 4
)

// readSite answers from one site: from the indexed library that holds it,
// else from its pages, read and held in memory. An answer is kept in the
// question cache (saveRead) for a repeat within cache_ttl.
func (s *SearchFirst) readSite(ctx, cctx context.Context, site sfSite, prof *siteProfile, req sfRequest, tr *sfTrace) Outcome {
	if tr.site == "" {
		tr.site, tr.siteHits = site.root.String(), len(site.hits)
	}
	tr.sites++
	if lib, ok := s.indexedOver(site.root); ok {
		tr.local = lib.Root
		o := s.answerIndexed(ctx, lib, req.query, req.opts, "")
		if answered(o) {
			s.saveRead(req, sfRead{Root: site.root.String()})
		}
		return o
	}
	links, disallowed := s.plan(cctx, site, prof, req.query)
	if len(links) == 0 && disallowed > 0 {
		return Answer(SearchBackend, nil, fmt.Errorf("search: %s's robots.txt disallows the %d pages found: %w", site.root, disallowed, errDisallowed))
	}
	pages, by, failed := s.fetch(cctx, site.root, links)
	tr.planned, tr.fetched, tr.failed = tr.planned+len(links), tr.fetched+len(pages), tr.failed+failed
	if len(pages) == 0 {
		return s.failed(ctx, cctx, fmt.Errorf("search: none of the %d pages of %s could be read: %w", len(links), site.root, ErrNotFound))
	}
	title, name := readTitle(site.root, prof)
	o := s.answerRead(ctx, site.root, title, name, pages, req)
	if answered(o) {
		s.saveRead(req, readOf(site.root, title, name, pages, by))
	}
	return o
}

// failed is a search-first call that ended before an answer, classified
// as the web backend classifies one (a timeout is upstream).
func (s *SearchFirst) failed(parent, op context.Context, err error) Outcome {
	if parent.Err() == nil && op.Err() != nil {
		err = fmt.Errorf("search: no answer within %s: %w", s.timeout, err)
	}
	return Answer(SearchBackend, nil, err)
}

// searchNote says how much of the site at root a search read and how to
// index all of it.
func searchNote(root string, read int) string {
	return fmt.Sprintf("read %d pages of %s found by web search; the whole site: ketch docs index %s", read, root, root)
}

// withReason is reason followed by the reasons in rest (", "-joined),
// each once.
func withReason(reason, rest string) string {
	out := []string{reason}
	for _, r := range strings.Split(rest, ", ") {
		if r != "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return strings.Join(out, ", ")
}

// searchAnswered is the local index's answer as the search-first
// backend's: partial, with note (searchNote).
func searchAnswered(o Outcome, note string) Outcome {
	if o.Status != Found && o.Status != Partial {
		for i := range o.Attempts {
			o.Attempts[i].Backend = SearchBackend
		}
		return o
	}
	for i := range o.Results {
		o.Results[i].Source = SearchBackend
	}
	return asPartial(o, SearchBackend, note)
}

// asPartial marks an answer partial with a note, as backend's.
func asPartial(o Outcome, backend, note string) Outcome {
	o.Status, o.Backend = Partial, backend
	o.Reason = withReason(ReasonPartialSite, o.Reason)
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
	hits := fuseWeighted(append(lists, citedLists(lists)...), []float64{1, 1, sfCitedWeight, sfCitedWeight})
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

// errDisallowed is a site whose robots.txt keeps ketch from every page
// found: a refusal, the same next time.
var errDisallowed = failure.New(failure.NotFound, reasonDisallowed, "robots.txt disallows the pages")

const reasonDisallowed = "disallowed"

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
	p := sitePrefs{words: questionWords(req.query)}
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
func (s *SearchFirst) chooseSite(ctx context.Context, sites []sfSite, req sfRequest, prefs sitePrefs) (sfSite, *siteProfile, []sfSite, error) {
	if isHTTPURL(req.library) && len(prefs.roots) > 0 {
		site := givenSite(prefs.roots[0], sites)
		return site, s.hitProfile(ctx, site), nil, nil
	}
	for tries := 0; tries < 3; tries++ {
		site, err := pickSite(sites)
		if err != nil {
			return sfSite{}, nil, nil, err
		}
		sites = sites[1:]
		prof := s.hitProfile(ctx, site)
		if prof == nil && len(site.alts) > 0 && !site.retry {
			// Its top hit unreadable (a JS shell no browser renders, a
			// failed fetch): the domain's next tree first, this one last.
			sites = append([]sfSite{lastTree(site)}, sites...)
			continue
		}
		site, sites = servedSite(site, prof, sites)
		if site.docs || site.named || prof != nil && prof.served != nil && looksLikeDocs(prof.served, prof) {
			return site, prof, sites, nil
		}
	}
	return sfSite{}, nil, nil, fmt.Errorf("search: %w", ErrNoDocsFound)
}

// givenSite is the docs root a request gives as the site, with the hits
// under it from every site's trees.
func givenSite(root webRoot, sites []sfSite) sfSite {
	site := sfSite{key: root.String(), root: root, docs: true}
	for _, c := range sites {
		for _, t := range append([]sfSite{c}, c.alts...) {
			for _, h := range t.hits {
				if root.contains(h.u) {
					site.hits = append(site.hits, h)
				}
			}
		}
	}
	return site
}

// servedSite is the site where its top hit is served from, when that is
// outside its root (a docs host that moved, docs.x.com/en/ redirecting to
// /v1.2/en/): the served page's tree, its hits the top hit as served and
// the hits there of every other site (removed from sites), which are the
// same docs found under their new address.
func servedSite(site sfSite, prof *siteProfile, sites []sfSite) (sfSite, []sfSite) {
	if prof == nil || prof.served == nil || len(site.hits) == 0 || site.root.contains(prof.served) || !isPublicWebURL(prof.served) {
		return site, sites
	}
	root := docsTree(prof.served)
	top := site.hits[0]
	top.u = prof.served
	hits := []sfHit{top}
	for _, h := range site.hits[1:] {
		if root.contains(h.u) {
			hits = append(hits, h)
		}
	}
	moved, docs, rest := sitesUnder(root, top.u, sites)
	site.root, site.hits, site.key = root, append(hits, moved...), siteKey(prof.served, root)
	site.docs = site.docs || docs
	return site, rest
}

// sitesUnder takes from sites every one whose best tree's hits all sit
// under root: the hits under root of all its trees (but top, which the
// site has), whether any had a docs signal, and the sites left.
func sitesUnder(root webRoot, top *url.URL, sites []sfSite) ([]sfHit, bool, []sfSite) {
	var hits []sfHit
	var rest []sfSite
	docs := false
	for _, o := range sites {
		under := len(o.hits) > 0
		for _, h := range o.hits {
			under = under && root.contains(h.u)
		}
		if !under {
			rest = append(rest, o)
			continue
		}
		for _, t := range append([]sfSite{o}, o.alts...) {
			for _, h := range t.hits {
				if root.contains(h.u) && linkKey(h.u) != linkKey(top) {
					hits = append(hits, h)
				}
			}
		}
		docs = docs || o.docs
	}
	return hits, docs, rest
}

// lastTree is a site's next docs tree, with the site itself as its last
// alternative.
func lastTree(site sfSite) sfSite {
	rest := site.alts
	site.alts = nil
	next := rest[0]
	next.alts, next.retry = append(append([]sfSite{}, rest[1:]...), site), true
	return next
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

// probeSearch needs no network: search is keyless (it searches through
// the search surface's providers) and has no fixed endpoint. It reports
// what the local index holds.
func probeSearch(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
	const keyless = "keyless: web search, then the docs site it finds (pages kept in the page cache for cache_ttl)"
	status, index := probeIndex(ctx, client, c)
	if status == health.StatusMisconfigured {
		return status, index
	}
	return health.StatusOK, keyless + "; " + index
}

func searchProvider() Provider {
	return Provider{
		ID:          SearchBackend,
		Name:        "Search",
		LibraryHelp: "a docs URL to search within, or a library name or name@version to prefer; a library indexed with ketch docs index answers offline",
		Usable:      func(*config.Config) bool { return true },
		New:         func(c *config.Config, deps Deps) (Searcher, error) { return newSearchFirstFromConfig(c, deps), nil },
		Probe:       probeSearch,
	}
}
