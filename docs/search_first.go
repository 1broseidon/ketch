package docs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/decide"
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
//	the same question read within cache_ttl? ──yes──▶ the same pages, the same answer (no network)
//	        │no
//	the request names no library? ──yes──▶ the plain question's search, its
//	        │                         first readable document and up to two
//	        │                         more of its site (search_bare.go)
//	        │no
//	web search (the question, and the question + "documentation"),
//	beside it resolve of the library the request names (search_origin.go)
//	        │       (several verified, none settled: ambiguous, refused)
//	docs pages only; the site the first three of them weigh most for (a
//	release the question names first); a host that serves many projects
//	split by project path
//	        │       (no docs hit, or none readable: the search results alone)
//	the root resolve verified first, unless the first site is the project's own
//	        │
//	an indexed library holds the site? ──yes──▶ answer from the index
//	        │no
//	read the site's search hits at their exact addresses, in rank order
//	(up to sfExactPages, one release tree; an unreadable one skipped)
//	        │       (an identifier the question writes on none of them:
//	        │       more pages of that release, printed after them)
//	section them into a library held in memory for this call
//	        │
//	answer through the local index's evidence pipeline, best page first
//	        └──▶ the pages read, what is not printed, the other results
//
// A question saves nothing (search_read.go): the pages it read stay in the
// page cache under cache_ttl, with what a repeat needs to skip the search,
// and `ketch docs index` is the only command that writes the index. An
// answer from the index checks the pages it cites first (index_fresh.go).
// A library the request names (--library, or a name@version the question
// writes) is resolved while the question is searched: one docs root
// verified is the site read, unless the first docs site of the search is
// that project's own; else the name is a preference among the hits'
// sites, and the version it asks for picks the tree. A name@version in a
// bare question that resolve cannot confirm is ordinary words. A call
// whose search returned results never refuses for want of a readable
// page: it prints the results (resultsOnly).

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

const (
	sfMaxPages       = 24 // pages read per question
	sfMaxHitPages    = 8  // of them, search hits
	sfNearPages      = 6  // of them, siblings of a hit in the site's nav
	sfSearchResults  = 10
	sfTimeout        = 45 * time.Second
	sfSearchTimeout  = 15 * time.Second
	sfResolveTimeout = 6 * time.Second
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
	// resolveName resolves the library a request names (resolveNamed
	// when nil); tests give a fixed answer.
	resolveName func(ctx context.Context, req sfRequest) sfResolved
	// decisions is the decision provider a setting selects ("" for the
	// configured docs_decisions; decide.Choose): nil asks none, as a
	// backend built without a config does.
	decisions func(setting string) (decide.Provider, decide.Decider, bool, error)
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
	dc := *c
	s.decisions = func(setting string) (decide.Provider, decide.Decider, bool, error) {
		if setting == "" {
			setting = dc.DocsDecisions
		}
		return decide.Choose(&dc, setting)
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

// ResolveLibrary resolves a name to its docs roots; see ResolveSelect.
func (s *SearchFirst) ResolveLibrary(ctx context.Context, name string, limit int) ([]LibraryMatch, error) {
	o := s.ResolveSelect(ctx, name, limit)
	return o.Matches, o.Err()
}

// ResolveSelect resolves a name to its docs roots: the indexed libraries
// it names, with no network (local_resolve.go; an empty name lists every
// one); when none, the roots a resolution finds (Web.ResolveLibrary). A
// caller that carries its options (the CLI, MCP) caps the indexed ones
// only by the limit it gives.
func (s *SearchFirst) ResolveSelect(ctx context.Context, name string, limit int) Outcome {
	indexed, err := s.local.resolveIndexed(name)
	if err != nil {
		return Resolved(SearchBackend, nil, err)
	}
	if len(indexed) > 0 {
		if opts, ok := localOptionsFrom(ctx); ok {
			limit = opts.Limit
		}
		if limit > 0 && len(indexed) > limit {
			indexed = indexed[:limit]
		}
		return Resolved(SearchBackend, indexed, nil)
	}
	if offline(ctx) {
		return Resolved(SearchBackend, nil, offlineMiss(name))
	}
	if strings.TrimSpace(name) == "" {
		return Resolved(SearchBackend, nil, fmt.Errorf("search: no library is indexed (ketch docs index <docs-url>); give a library name to resolve its docs root: %w", ErrNotIndexed))
	}
	if strings.ContainsAny(strings.TrimSpace(name), " \t") {
		// A name of several words is a product's name, not a package
		// registries hold: the index is all that can answer it. It may be
		// a question sent to resolve, so the refusal says how to ask one.
		return Resolved(SearchBackend, nil, spacedNameMiss(ctx, name))
	}
	// The standard-library pages the name is, read beside the
	// registries' resolution and listed ahead of it.
	name = strings.TrimSpace(name)
	fctx := withFetchMemo(ctx)
	std := make([]*webMatch, 2)
	var wg sync.WaitGroup
	s.startStdPages(fctx, name, &wg, std)
	matches, err := s.web.ResolveLibrary(ctx, name, limit)
	wg.Wait()
	var first []LibraryMatch
	for _, m := range std {
		if m != nil {
			first = append(first, m.libraryMatch())
		}
	}
	if len(first) > 0 {
		var amb *AmbiguousLibraryError
		if errors.As(err, &amb) {
			matches = amb.Matches
		}
		matches, err = append(first, matches...), nil
	}
	return Resolved(SearchBackend, matches, err)
}

// spacedNameMiss refuses a resolve name with spaces that no indexed
// library holds (not_found, not_indexed): resolve takes a package name or
// docs URL, and a question is asked without it, in the caller's words (a
// command line for the CLI, the query argument otherwise: MCP, and the CLI's
// --json that the pi plugin calls).
func spacedNameMiss(ctx context.Context, name string) error {
	ask := "send it as query without resolve"
	if opts, _ := localOptionsFrom(ctx); opts.Hint == HintCLI {
		ask = `ketch docs "<question>"`
	}
	return failure.Wrap(failure.NotFound, "not_indexed", fmt.Errorf(
		"search: no indexed library is named %q; resolve takes a package name or docs URL (no name lists the indexed libraries), not a question; to ask a question: %s", name, ask))
}

// startStdPages starts, on wg, the reads of the standard-library pages the
// name is into std: Go's package at 0, Python's module at 1.
func (s *SearchFirst) startStdPages(fctx context.Context, name string, wg *sync.WaitGroup, std []*webMatch) {
	if spec, perr := parseLibSpec(name); perr == nil && spec.version == "" {
		if goStdRe.MatchString(strings.TrimPrefix(name, "go:")) && (spec.eco == ecoAny || spec.eco == ecoGitHub || strings.HasPrefix(name, "go:")) {
			wg.Go(func() {
				if m, ok := s.web.resolveGoStd(fctx, name); ok {
					std[0] = &m
				}
			})
		}
		if spec.eco == ecoAny && pyStdRe.MatchString(name) {
			wg.Go(func() {
				if m, ok := s.web.resolvePyStd(fctx, name); ok {
					std[1] = &m
				}
			})
		}
	}
}

// SearchSelect answers a question from the local index when it covers
// it, else from the docs its web search ranks first. A caller that
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
		req.spec, req.guessed = spec, true
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
	version string       // the release verified by resolution, including aliases
	hits    []sfHit      // the question's search results, fused, best first
	// guessed marks a spec read from the question's words (name@version
	// in a bare question): used when resolve confirms its version, read as
	// ordinary words when it does not, never a refusal.
	guessed bool
	// dec is the decision provider a question that names no library asks
	// (decide_flow.go); nil for none.
	dec *decision
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
		return dressed(o, req, nil)
	}
	if req.opts.Offline {
		return Answer(SearchBackend, nil, offlineMiss(firstNonEmpty(req.library, req.query)))
	}
	tr.path = "search"
	TraceFrom(ctx).setPath(tr.path)
	near := s.near(req)
	req.dec = s.decisionFor(req)
	if !req.dec.usable() {
		// An answer with decisions is never kept: a repeat asks again.
		if o, ok := s.fromRead(ctx, req, tr); ok {
			return withNear(withDecisionNote(withRelease(o, req), req.dec.note()), req.library, near)
		}
	}
	o := withRelease(s.fromSearch(ctx, req, tr), req)
	if req.dec != nil && o.Decisions == nil {
		o = withDecisionNote(o, req.dec.note())
	}
	tr.fellOver = tr.domains > 1
	for _, f := range []struct {
		on   bool
		name string
	}{{tr.fellOver, "fell_over"}, {tr.resolvedFirst, "resolved_first"}} {
		if f.on {
			TraceFrom(ctx).fallback(f.name)
		}
	}
	return withNear(o, req.library, near)
}

// withRelease is o marked with the release a bare question names in
// words (markRelease); o as it is for a request that names a library or
// pins a version.
func withRelease(o Outcome, req sfRequest) Outcome {
	if req.library != "" || req.spec.version != "" && !req.guessed || o.Decisions != nil {
		return o
	}
	if r, ok := questionRelease(req.query); ok {
		return markRelease(o, r)
	}
	return o
}

// withNear gives o the indexed libraries near the library value: an
// answer carries them (indexed_near), a refusal or failure names them in
// its error, so the next call can name one.
func withNear(o Outcome, value string, near []NearLibrary) Outcome {
	if len(near) == 0 {
		return o
	}
	o.IndexedNear = near
	if o.err != nil && o.Index == nil && o.Status != Found && o.Status != Partial {
		o.err = fmt.Errorf("%w; %s", o.err, nearNote(value, near))
	}
	return o
}

// fromSearch answers from the docs the question's search ranks first, or
// from the docs root resolve verified for the library the request names
// when that site is not the project's own (Build 1: right source or say
// so). Every answer says its origin.
func (s *SearchFirst) fromSearch(ctx context.Context, req sfRequest, tr *sfTrace) Outcome {
	ctx = withProfileMemo(withFetchMemo(ctx))
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if bareRequest(req) {
		return s.answerBare(ctx, cctx, req, tr)
	}
	hits, prefs, err := s.searchHits(cctx, req)
	req.hits = hits
	o := s.readSearch(ctx, cctx, req, hits, prefs, err, tr)
	if answered(o) || req.spec.version != "" || errors.Is(o.err, ErrAmbiguousLibrary) {
		return o // a pin must match, and a name several libraries share is the caller's to settle
	}
	return resultsOnly(dressed(o, req, nil), hits)
}

// readSearch answers from the search's hits (fromSearch).
func (s *SearchFirst) readSearch(ctx, cctx context.Context, req sfRequest, hits []sfHit, prefs sitePrefs, err error, tr *sfTrace) Outcome {
	tr.hits = len(hits)
	tr.resolveMs, tr.resolved = prefs.resolved.took.Milliseconds(), resolvedTrace(prefs.resolved)
	if req.spec.version != "" {
		return s.readPinned(ctx, cctx, req, hits, prefs, tr)
	}
	if err != nil && prefs.resolved.ambiguous == nil {
		return s.failed(ctx, cctx, err)
	}
	docs := docsHits(hits)
	tr.docsHits = len(docs)
	if r, ok := questionRelease(req.query); ok {
		docs = releaseFirst(docs, r)
	}
	sites := rankSites(chooseSites(docs, prefs))
	tr.cands = siteList(sites)
	prefs.lead = searchLead(docs, prefs)
	if amb := prefs.resolved.ambiguous; amb != nil {
		// Several libraries go by this name (settleAmbiguity): which one
		// is the caller's to say, never a guess from whatever sites the
		// web search returned.
		if refusal := settleAmbiguity(&prefs, sites); refusal != nil {
			return Answer(SearchBackend, nil, fmt.Errorf("search: %w", refusal))
		}
		tr.resolved = resolvedTrace(prefs.resolved)
		if err != nil {
			return s.failed(ctx, cctx, err)
		}
		prefs.lead = searchLead(docs, prefs)
	}
	var o Outcome
	var read sfRead
	// unread names the sites read before the answer that gave no text, for
	// its note: an answer from the next site must say the first one failed.
	var unread []string
	if m := prefs.resolved.match; m != nil && !leadOwned(sites, m) {
		// The root resolve verified is the library, unless the site the
		// search agrees on is that project's own: read it first. Its
		// docs not holding the terms gives way to the site the search
		// results favour, the better answer kept; a root that gave no
		// text gives way to the search's sites, and the answer says so.
		site := resolvedSite(m, sites)
		tr.domains++
		tr.resolvedFirst = true
		o, read = s.readSite(ctx, cctx, site, s.hitProfile(cctx, site), req, resolvedOrigin(m, prefs.lead), tr)
		if lead := prefs.lead; favoursOtherSite(cctx, o, lead, m) {
			return s.fallOver(ctx, cctx, o, read, sites, req, prefs, tr)
		}
		if o.unread == "" || cctx.Err() != nil {
			return s.keep(req, read, o)
		}
		unread = append(unread, site.root.String()+" ("+o.unread+")")
		sites = slices.DeleteFunc(sites, func(c sfSite) bool { return m.root.contains(c.root.u) })
	}
	return s.readSites(ctx, cctx, sites, req, prefs, o, read, unread, tr)
}

// favoursOtherSite reports the resolved root's read, o, refused or partial
// for want of the question's terms while the search results favour lead,
// another site than m's own.
func favoursOtherSite(cctx context.Context, o Outcome, lead *webRoot, m *webMatch) bool {
	return o.unread == "" && cctx.Err() == nil && (termsRefused(o) || termsPartial(o)) && leadElsewhere(lead, m)
}

// leadElsewhere reports a site the search results favour, lead, that is
// neither under m's root nor that project's own site.
func leadElsewhere(lead *webRoot, m *webMatch) bool {
	return lead != nil && !m.root.contains(lead.u) && !ownSiteStrong(*lead, m)
}

// resolvedSite is the root resolve found for m as a site to read: the
// module's page first when resolve found one, its search hits, and the
// root's own page when no hit is under it.
func resolvedSite(m *webMatch, sites []sfSite) sfSite {
	site := givenSite(m.root, sites)
	site.named, site.fallback = true, m.root.u
	if m.page != nil {
		site.page, site.pageVia = m.page, "the module page resolve found"
	}
	return site
}

// fallOver reads the site the search results favour after the resolved
// root's read, o, fell short for want of the question's terms, and keeps
// the better answer (betterRead; a tie keeps the root's). A favoured site
// that gave no text leaves the root's answer, saying so. The read counts
// as the question's second site (sfMaxSites); no other is read after it.
func (s *SearchFirst) fallOver(ctx, cctx context.Context, o Outcome, read sfRead, sites []sfSite, req sfRequest, prefs sitePrefs, tr *sfTrace) Outcome {
	m, lead := prefs.resolved.match, *prefs.lead
	fav := givenSite(lead, sites)
	org := siteOrigin(fav, req, prefs)
	org.text = joinNote(org.text, fmt.Sprintf("%s, which resolve found for %s, %s, so this answer is from the site the search results favour", m.root, prefs.resolved.name, lacking(o)))
	tr.domains++
	o2, read2 := s.readSite(ctx, cctx, fav, s.hitProfile(cctx, fav), req, org, tr)
	switch {
	case o2.unread != "":
		return s.keepTried(req, read, o, lead, o2.unread, "the site the search results favour")
	case betterRead(o2, o):
		return s.keep(req, read2, o2)
	case o.err != nil:
		o.err = fmt.Errorf("%w; nor does %s, the site the search results favour", o.err, lead)
	}
	return s.keep(req, read, o)
}

// termsPartial reports an answer partial for want of some of the
// question's distinctive terms.
func termsPartial(o Outcome) bool {
	return o.Status == Partial && o.Reason == ReasonTermsNotFound && len(o.NotFound) > 0
}

// lacking says what of the question's terms a read that fell short lacks:
// all of them (a refusal), or those it names (a partial answer).
func lacking(o Outcome) string {
	if !termsPartial(o) {
		return "holds none of the question's terms"
	}
	noun := "term"
	if len(o.NotFound) > 1 {
		noun = "terms"
	}
	return "lacks the question's " + noun + " " + strings.Join(o.NotFound, ", ")
}

// betterRead reports a read's answer, a, better than b's: found over
// partial over neither; of two partial answers, the one missing fewer of
// the question's terms.
func betterRead(a, b Outcome) bool {
	rank := func(o Outcome) int {
		switch o.Status {
		case Found:
			return 2
		case Partial:
			return 1
		}
		return 0
	}
	if rank(a) != rank(b) {
		return rank(a) > rank(b)
	}
	return a.Status == Partial && len(a.NotFound) < len(b.NotFound)
}

// keepTried is keep for o, kept over a site that gave no text (root; why:
// its read's unread): o's note, or a refusal's error, names that site, and
// so does a repeat within cache_ttl (sfRead.Tried), so a caller sees the
// site was tried and why it failed.
func (s *SearchFirst) keepTried(req sfRequest, read sfRead, o Outcome, root webRoot, why, what string) Outcome {
	read.Tried = fmt.Sprintf("could not read %s (%s), %s", root, why, what)
	return s.keep(req, read, withTried(o, read.Tried))
}

// withTried is o with tried, the sites tried before that gave no text, in
// its note, or for a refusal its error.
func withTried(o Outcome, tried string) Outcome {
	switch {
	case tried == "":
	case answered(o):
		o.Note = joinNote(tried, o.Note)
	case o.err != nil:
		o.err = fmt.Errorf("%w; %s", o.err, tried)
	}
	return o
}

// readSites reads the search's sites in turn, after o and read from any
// site read before, until one answers or the reads run out.
func (s *SearchFirst) readSites(ctx, cctx context.Context, sites []sfSite, req sfRequest, prefs sitePrefs, o Outcome, read sfRead, unread []string, tr *sfTrace) Outcome {
	var err error
	skipped := o.Skipped // every page tried and unreadable, the earlier sites' too
	for range sfMaxReads {
		var site sfSite
		var prof *siteProfile
		site, prof, sites, err = s.chooseSite(cctx, sites, req, prefs)
		if err != nil {
			if o.Status != "" {
				return s.keep(req, read, o) // the site read before says more than the rest's refusal
			}
			return s.failed(ctx, cctx, namedRefusal(err, req, prefs))
		}
		if !site.retry {
			tr.domains++
		}
		o, read = s.readSite(ctx, cctx, site, prof, req, siteOrigin(site, req, prefs), tr)
		o.Skipped = append(slices.Clone(skipped), o.Skipped...)
		skipped, read.Skipped = o.Skipped, o.Skipped // a repeat prints them too
		if answered(o) || cctx.Err() != nil {
			return s.keepAfter(req, read, unread, o)
		}
		if o.unread != "" {
			unread = append(unread, site.root.String()+" ("+o.unread+")")
		}
		if len(site.alts) > 0 && !printableRefusal(o) {
			// Nothing readable in this docs tree (JS shells no browser
			// rendered, robots-closed): the domain's next tree before
			// another domain. Pages read that lack the question's terms are
			// not that: never another release's tree in their place.
			next := site.alts[0]
			next.alts, next.retry = site.alts[1:], true
			sites = append([]sfSite{next}, sites...)
			continue
		}
		if o.Reason == reasonDisallowed || len(sites) == 0 || tr.domains >= sfMaxSites {
			return s.keepAfter(req, read, unread, o) // a site that closes its docs to robots is still the site
		}
	}
	return s.keepAfter(req, read, unread, o)
}

// keep is the outcome returned, its read kept in the question cache
// for a repeat within cache_ttl when the outcome is one the evidence
// pipeline decided (an answer, or a refusal for want of the terms).
func (s *SearchFirst) keep(req sfRequest, read sfRead, o Outcome) Outcome {
	if !answered(o) && !printableRefusal(o) {
		return o
	}
	o = dressed(o, req, otherResults(req.hits, o.Read, read.widePages(), o.Skipped))
	read.Others = o.OtherResults
	if read.Root != "" && kept(o) {
		s.saveRead(req, read)
	}
	return o
}

// printableRefusal reports the evidence pipeline's refusal for want of the
// question's terms in pages it read or an indexed library: an answer that
// says so (not_on_pages, not_in_index), with what it read.
func printableRefusal(o Outcome) bool {
	return termsRefused(o) && (len(o.Read) > 0 || o.Widened != nil || o.Index != nil)
}

// dressed is an outcome as the search backend returns it: the library
// the request named, an answer from pages read (or an indexed library)
// that hold none of the question's terms printed as an answer (what was
// read, the outline, the other results; not_on_pages or not_in_index
// names every term), and others, the search results it did not read.
func dressed(o Outcome, req sfRequest, others []OtherResult) Outcome {
	o.named = req.library
	if printableRefusal(o) {
		if o.Index == nil {
			o.Note = joinNote("no section of the pages read holds the question's terms ("+strings.Join(o.NotFound, ", ")+")", o.Note)
		}
		o.Status, o.Reason, o.err, o.Results = Partial, ReasonTermsNotFound, nil, []Result{}
		o.Backend = SearchBackend
		o.Attempts = []Attempt{{Backend: SearchBackend, Status: Partial, Reason: ReasonTermsNotFound}}
	}
	if answered(o) {
		o.OtherResults = others
	}
	return o
}

// keepAfter is keep with o's note or error naming the sites read before
// it that gave no text (unread).
func (s *SearchFirst) keepAfter(req sfRequest, read sfRead, unread []string, o Outcome) Outcome {
	before := unread
	if o.unread != "" && len(before) > 0 {
		before = before[:len(before)-1] // the site this outcome is about
	}
	switch {
	case len(before) == 0:
	case answered(o):
		read.Tried = "could not read " + strings.Join(before, "; ") + ", so this answer is from the next site"
		o.Note = joinNote(read.Tried, o.Note)
	case o.err != nil && o.Index == nil:
		o.err = fmt.Errorf("%w; before it, could not read %s", o.err, strings.Join(before, "; "))
	}
	return s.keep(req, read, o)
}

// searchLead is the first docs site of the search with no resolved root
// preferred, nil when there is none.
func searchLead(docs []sfHit, prefs sitePrefs) *webRoot {
	plain := prefs
	if m := prefs.resolved.match; m != nil {
		plain.roots = slices.DeleteFunc(slices.Clone(prefs.roots), func(r webRoot) bool { return r.String() == m.root.String() })
	}
	site, err := pickSite(rankSites(chooseSites(docs, plain)))
	if err != nil || site.root.prefix == "/" && indexHosted(site.root.u) {
		return nil // a package index's whole host is no site to favour
	}
	return &site.root
}

// termsRefused reports the evidence pipeline's refusal: none of the
// question's distinctive terms in the pages, or nothing in them matched.
func termsRefused(o Outcome) bool {
	err := o.Err()
	return o.Status == Refused && (errors.Is(err, ErrTermsNotFound) || errors.Is(err, ErrNoResults))
}

// namedRefusal is the refusal of a question whose search found no docs
// site, when the request names a library: the libraries resolve knows by
// that name, with their ecosystems and titles, never the sites the search
// happened to return; those only when resolve knows nothing of the name.
func namedRefusal(err error, req sfRequest, prefs sitePrefs) error {
	if req.library == "" || isHTTPURL(req.library) || len(prefs.resolved.candidates) == 0 ||
		!errors.Is(err, ErrNoDocsFound) {
		return err
	}
	return fmt.Errorf("search: the search found no docs site for %q; resolve knows it as: %s; pass one as the library (--library): %w",
		req.library, matchList(prefs.resolved.candidates), ErrAmbiguousLibrary)
}

// leadOwned reports a first docs site of the search that is the site
// of the project resolve verified m for (siteOfProject): it then answers,
// with that as its origin.
func leadOwned(sites []sfSite, m *webMatch) bool {
	if len(sites) == 0 {
		return false
	}
	if m.std {
		// The language's own page for the module: only a lead at or under
		// it stands in for it, not another site of the language's domain
		// (peps.python.org beside docs.python.org) nor a tree over it.
		return m.root.contains(sites[0].root.u)
	}
	_, ok := siteOfProject(sites[0].root, m)
	return ok
}

// joinNote is two notes as one, "; "-joined, either may be empty.
func joinNote(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
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
// else from its exact pages (search_exact.go), widened when they lack a
// term of the question, read and held in memory. It returns the read a
// repeat within cache_ttl would answer from (no Root when nothing was
// read), which fromSearch keeps.
func (s *SearchFirst) readSite(ctx, cctx context.Context, site sfSite, prof *siteProfile, req sfRequest, origin sfOrigin, tr *sfTrace) (Outcome, sfRead) {
	if tr.site == "" {
		tr.site, tr.siteHits = site.root.String(), len(site.hits)
	}
	tr.sites++
	if lib, ok := s.indexedOver(site.root); ok && req.spec.version == "" {
		tr.local = lib.Root
		return s.answerIndexed(ctx, lib, req.query, req.opts, ""), sfRead{Root: site.root.String()}
	}
	p, fails, disallowed, tree := s.readExact(cctx, site, tr)
	tr.exact += len(p.exact)
	p.title, p.name = readTitle(p.root, prof)
	widen := s.widener(cctx, tree, prof, req, p, &fails, tr)
	found := len(p.exact) > 0 || widen(nil)
	selfFail := ""
	if !found && site.self != nil && disallowed == 0 && fails.n() == 0 {
		// Nothing under the library URL: the URL itself is the one page.
		one := sfSite{key: site.key, root: site.root, docs: site.docs, named: site.named, page: site.self, pageVia: "the library URL"}
		sp, sfails, sdis, stree := s.readExact(cctx, one, tr)
		tr.exact += len(sp.exact)
		if len(sp.exact) > 0 {
			p, fails, disallowed, tree = sp, sfails, sdis, stree
			p.title, p.name = readTitle(p.root, prof)
			widen = s.widener(cctx, tree, prof, req, p, &fails, tr)
			found = true
		} else {
			selfFail = "nor could the library URL itself be read"
			if len(sp.skipped) > 0 {
				selfFail += " (" + sp.skipped[0].Cause + ")"
			}
			p.skipped = append(p.skipped, sp.skipped...)
		}
	}
	if !found {
		// No exact page could be read, nor any the site's own lists name.
		if disallowed > 0 && fails.n() == 0 {
			o := Answer(SearchBackend, nil, fmt.Errorf("search: %s's robots.txt disallows the %d pages found: %w", site.root, disallowed, errDisallowed))
			o.Skipped = p.skipped
			return o, sfRead{}
		}
		if fails.n() == 0 {
			why := "no search hit is under it, and its llms.txt, sitemap and links list none"
			if selfFail != "" {
				why += ", " + selfFail
			}
			o := s.failed(ctx, cctx, fmt.Errorf("search: found no page of %s to read: %s; %s: %w", site.root, why, appendNext(nil, site.root)[0], ErrNotFound))
			o.unread, o.Skipped = "no page to read", p.skipped
			return o, sfRead{}
		}
		o := s.failed(ctx, cctx, noPageErr(site.root, fails.n(), fails))
		o.unread = strings.SplitN(readFailNote(site.root, fails.list, fails.details), "; ", 2)[0]
		o.Skipped = p.skipped
		return o, sfRead{}
	}
	o := s.answerRead(ctx, p, req, origin, widen)
	o.readPages = append(slices.Clone(p.exact), p.wide...)
	r := readOf(p)
	r.Version, r.Pin = req.version, req.spec.version
	r.Origin = origin.text
	return o, r
}

// failed is a search-first call that ended before an answer, classified
// as the web backend classifies one (a timeout is upstream).
func (s *SearchFirst) failed(parent, op context.Context, err error) Outcome {
	if parent.Err() == nil && op.Err() != nil {
		err = fmt.Errorf("search: no answer within %s: %w", s.timeout, err)
	}
	return Answer(SearchBackend, nil, err)
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
	for i := range hits {
		hits[i].rank = i + 1
	}
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

// errSearchEmpty is web searches that all answered with no results:
// nothing to print (exit 3).
var errSearchEmpty = failure.New(failure.NotFound, "search_empty", "the web search returned no results")

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
// library it names, sites its name labels, and what resolve makes of the
// name within sfResolveTimeout (resolveNamed: one verified docs root, a
// preference here and the site fromSearch reads first, or several, which
// refuse the question). A name@version a bare question writes is resolved
// the same way (answerBare). Resolving runs beside the web search, so it
// costs only the time it takes past the search's.
func (s *SearchFirst) prefs(ctx context.Context, req sfRequest) sitePrefs {
	p := sitePrefs{words: questionWords(req.query)}
	if isHTTPURL(req.library) {
		if r, err := parseWebRoot(req.library); err == nil {
			p.roots = append(p.roots, r)
		}
		return p
	}
	if req.spec.name != "" {
		p.version, p.library = req.spec.version, req.spec.name
		for _, n := range []string{req.spec.name, lastPart(req.spec.name)} {
			if k := libraryKey(n); k != "" && !genericNames[k] && !languageWords[k] {
				p.names = append(p.names, k)
			}
		}
	}
	if !s.web.resolvesNames() {
		return p
	}
	timeout := sfResolveTimeout
	if req.spec.version != "" && !req.guessed {
		timeout = s.timeout // an explicit pin must resolve, not degrade into a search preference
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resolve := s.resolveNamed
	if s.resolveName != nil {
		resolve = s.resolveName
	}
	p.resolved = resolve(rctx, req)
	if m := p.resolved.match; m != nil {
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

// chooseSite takes the first of sites (rankSites: the search's order)
// and reads its top hit's profile; a site whose top hit cannot be read
// tries the domain's next tree first. A docs URL the request gives is the
// site, whatever the hits say.
func (s *SearchFirst) chooseSite(ctx context.Context, sites []sfSite, req sfRequest, prefs sitePrefs) (sfSite, *siteProfile, []sfSite, error) {
	if isHTTPURL(req.library) && len(prefs.roots) > 0 {
		site := givenSite(prefs.roots[0], sites)
		if u, err := url.Parse(req.library); err == nil {
			u.Fragment, u.RawQuery = "", ""
			if isPageFile(path.Base(u.Path)) {
				site.page, site.pageVia = u, "the library URL" // read first, hits or none
			} else {
				site.self = u // read when nothing under it is (readSite)
			}
		}
		return site, s.hitProfile(ctx, site), nil, nil
	}
	for {
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
		prof = s.followMoved(ctx, prof)
		site, sites = servedSite(site, prof, sites)
		return site, prof, sites, nil
	}
}

// followMoved is prof, or where its site moved when its page says so.
func (s *SearchFirst) followMoved(ctx context.Context, prof *siteProfile) *siteProfile {
	if prof != nil && prof.movedTo != nil {
		// The site moved and its old address says so with a script or
		// meta refresh: read it where it went (after any HTTP redirect
		// there), as an HTTP redirect would have sent it.
		if to, err := s.web.cachedProfile(ctx, prof.movedTo); err == nil && to.served != nil && to.movedTo == nil {
			prof = to
		} else {
			moved := *prof
			moved.served = prof.movedTo
			prof = &moved
		}
	}
	return prof
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
	if r, ok := sharedProjectRoot(prof.served); ok {
		root = r // a host that serves many projects re-roots at the project's path, not the host
	}
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
		LibraryHelp: "a docs root as resolve lists it, or a library name or name@version (answered from the docs root resolve verifies for it; a name several libraries share is refused with them listed); a URL below an indexed root narrows the answer to the pages under it (scope), a docs URL not indexed is searched within; a library indexed with ketch docs index answers offline",
		Usable:      func(*config.Config) bool { return true },
		New:         func(c *config.Config, deps Deps) (Searcher, error) { return newSearchFirstFromConfig(c, deps), nil },
		Probe:       probeSearch,
	}
}
