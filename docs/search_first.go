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
//	web search (the question, and the question + "documentation"),
//	beside it resolve of the library the request names (search_origin.go)
//	        │       (several verified, none settled: ambiguous, refused)
//	docs pages only; the site most of their weight agrees on, a host
//	that serves many projects split by project path
//	        │       (none: no_docs_found; split: ambiguous_site)
//	the root resolve verified first, unless that site is the project's own
//	        │
//	an indexed library holds the site? ──yes──▶ answer from the index
//	        │no
//	fetch the hit pages and close siblings from the site's own lists
//	        │       (≤ sfMaxPages, through the page cache; a site that
//	        │       gives no text says why and the next is read)
//	section them into a library held in memory for this call
//	        │
//	answer through the local index's evidence pipeline
//	        └──▶ found or partial (terms_not_found), its origin, N pages
//	             read, and how to index the whole site
//
// A question saves nothing (search_read.go): the pages it read stay in the
// page cache under cache_ttl, with what a repeat needs to skip the search,
// and `ketch docs index` is the only command that writes the index. An
// answer from the index checks the pages it cites first (index_fresh.go).
// A library the request names (--library, or name@version in the
// question; a bare question's first firm reading, strictly) is resolved
// while the question is searched: one docs root verified is the site
// read, unless the site the search agrees on is that project's own; else
// the name is a preference among the hits' sites, and the version it
// asks for picks the tree. Every answer from pages read says its origin:
// verified with its evidence, or unverified.

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
	if strings.TrimSpace(name) == "" {
		return Resolved(SearchBackend, nil, fmt.Errorf("search: no library is indexed (ketch docs index <docs-url>); give a library name to resolve its docs root: %w", ErrNotIndexed))
	}
	if strings.ContainsAny(strings.TrimSpace(name), " \t") {
		// A name of several words is a product's name, not a package
		// registries hold: the index is all that can answer it.
		return Resolved(SearchBackend, nil, fmt.Errorf("search: no indexed library is named %q, and a name with spaces is not a package name to resolve: give a package name or a docs URL; resolve with no name lists every indexed library: %w", name, ErrNotIndexed))
	}
	// The standard-library pages the name is, read beside the
	// registries' resolution and listed ahead of it.
	name = strings.TrimSpace(name)
	fctx := withFetchMemo(ctx)
	std := make([]*webMatch, 2)
	var wg sync.WaitGroup
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
	near := s.near(req)
	if o, ok := s.fromRead(ctx, req, tr); ok {
		return withNear(o, req.library, near)
	}
	return withNear(s.fromSearch(ctx, req, tr), req.library, near)
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

// fromSearch answers from the site the question's search agrees on, or
// from the docs root resolve verified for the library the request names
// when that site is not the project's own (Build 1: right source or say
// so). Every answer says its origin.
func (s *SearchFirst) fromSearch(ctx context.Context, req sfRequest, tr *sfTrace) Outcome {
	ctx = withProfileMemo(withFetchMemo(ctx))
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	hits, prefs, err := s.searchHits(cctx, req)
	tr.hits = len(hits)
	tr.resolveMs, tr.resolved = prefs.resolved.took.Milliseconds(), resolvedTrace(prefs.resolved)
	if err != nil && prefs.resolved.ambiguous == nil {
		return s.failed(ctx, cctx, err)
	}
	docs := docsHits(hits)
	tr.docsHits = len(docs)
	sites := chooseSites(docs, prefs)
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
	// keep is the outcome returned, its read kept in the question cache
	// for a repeat within cache_ttl when the outcome is one the evidence
	// pipeline decided (an answer, or a refusal for want of the terms).
	keep := func(o Outcome) Outcome {
		if read.Root != "" && kept(o) {
			s.saveRead(req, read)
		}
		return o
	}
	// unread names the sites read before the answer that gave no text, for
	// its note: an answer from the next site must say the first one failed.
	var unread []string
	keepAfter := func(o Outcome) Outcome {
		before := unread
		if o.unread != "" && len(before) > 0 {
			before = before[:len(before)-1] // the site this outcome is about
		}
		switch {
		case len(before) == 0:
		case answered(o):
			o.Note = joinNote("could not read "+strings.Join(before, "; ")+", so this answer is from the next site", o.Note)
		case o.err != nil && o.Index == nil:
			o.err = fmt.Errorf("%w; before it, could not read %s", o.err, strings.Join(before, "; "))
		}
		return keep(o)
	}
	if m := prefs.resolved.match; m != nil && !leadOwned(sites, m) {
		// The root resolve verified is the library, unless the site the
		// search agrees on is that project's own: read it first. Its
		// docs not holding the terms is the answer; only a root that gave
		// no text gives way to the search's sites, and the answer says so.
		site := givenSite(m.root, sites)
		site.page, site.named = m.root.u, true
		tr.domains++
		if m.page != nil {
			site.page = m.page
		}
		o, read = s.readSite(ctx, cctx, site, s.hitProfile(cctx, site), req, resolvedOrigin(m, prefs.lead), tr)
		if lead := prefs.lead; o.unread == "" && cctx.Err() == nil && termsRefused(o) && lead != nil && !m.root.contains(lead.u) && !ownSiteStrong(*lead, m) {
			// The resolved root holds none of the question's terms and the
			// search results favour another site: read that one in this
			// call; refuse only when neither holds them.
			fav := givenSite(*lead, sites)
			org := siteOrigin(fav, req, prefs)
			org.text += fmt.Sprintf("; %s, which resolve found for %s, holds none of the question's terms, so this answer is from the site the search results favour", m.root, prefs.resolved.name)
			o2, read2 := s.readSite(ctx, cctx, fav, s.hitProfile(cctx, fav), req, org, tr)
			if answered(o2) {
				read = read2
				return keep(o2)
			}
			if o.err != nil {
				o.err = fmt.Errorf("%w; nor does %s, the site the search results favour", o.err, lead)
			}
			return keep(o)
		}
		if o.unread == "" || cctx.Err() != nil {
			return keep(o)
		}
		unread = append(unread, site.root.String()+" ("+o.unread+")")
		sites = slices.DeleteFunc(sites, func(c sfSite) bool { return m.root.contains(c.root.u) })
	}
	for range sfMaxReads {
		var site sfSite
		var prof *siteProfile
		site, prof, sites, err = s.chooseSite(cctx, sites, req, prefs)
		if err != nil {
			if o.Status != "" {
				return keep(o) // the site read before says more than the rest's refusal
			}
			return s.failed(ctx, cctx, namedRefusal(err, req, prefs))
		}
		if !site.retry {
			tr.domains++
		}
		o, read = s.readSite(ctx, cctx, site, prof, req, siteOrigin(site, req, prefs), tr)
		if answered(o) || cctx.Err() != nil {
			return keepAfter(o)
		}
		if o.unread != "" {
			unread = append(unread, site.root.String()+" ("+o.unread+")")
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
			return keepAfter(o) // a site that closes its docs to robots is still the site
		}
	}
	return keepAfter(o)
}

// searchLead is the site the search results agree on with no resolved
// root preferred: pickSite over the docs hits' sites, nil when they agree
// on none.
func searchLead(docs []sfHit, prefs sitePrefs) *webRoot {
	plain := prefs
	if m := prefs.resolved.match; m != nil {
		plain.roots = slices.DeleteFunc(slices.Clone(prefs.roots), func(r webRoot) bool { return r.String() == m.root.String() })
	}
	site, err := pickSite(chooseSites(docs, plain))
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

// namedRefusal is the refusal of a question whose search agrees on no
// site, when the request names a library: the libraries resolve knows by
// that name, with their ecosystems and titles, never the sites the search
// happened to return; those only when resolve knows nothing of the name.
func namedRefusal(err error, req sfRequest, prefs sitePrefs) error {
	if req.library == "" || isHTTPURL(req.library) || len(prefs.resolved.candidates) == 0 ||
		!errors.Is(err, ErrAmbiguousSite) && !errors.Is(err, ErrNoDocsFound) {
		return err
	}
	return fmt.Errorf("search: the search results agree on no site for %q; resolve knows it as: %s; pass one as the library (--library): %w",
		req.library, matchList(prefs.resolved.candidates), ErrAmbiguousLibrary)
}

// leadOwned reports a leading site the search agrees on that is the site
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
// else from its pages, read and held in memory. It returns the read a
// repeat within cache_ttl would answer from (no Root when nothing was
// read), which fromSearch keeps.
func (s *SearchFirst) readSite(ctx, cctx context.Context, site sfSite, prof *siteProfile, req sfRequest, origin sfOrigin, tr *sfTrace) (Outcome, sfRead) {
	if tr.site == "" {
		tr.site, tr.siteHits = site.root.String(), len(site.hits)
	}
	tr.sites++
	if lib, ok := s.indexedOver(site.root); ok {
		tr.local = lib.Root
		return s.answerIndexed(ctx, lib, req.query, req.opts, ""), sfRead{Root: site.root.String()}
	}
	links, disallowed := s.plan(cctx, site, prof, req.query)
	if len(links) == 0 && disallowed > 0 {
		return Answer(SearchBackend, nil, fmt.Errorf("search: %s's robots.txt disallows the %d pages found: %w", site.root, disallowed, errDisallowed)), sfRead{}
	}
	if len(links) == 0 {
		why := "no search hit is under it, and its llms.txt, sitemap and links list none"
		o := s.failed(ctx, cctx, fmt.Errorf("search: found no page of %s to read: %s; %s: %w", site.root, why, appendNext(nil, site.root)[0], ErrNotFound))
		o.unread = "no page to read"
		return o, sfRead{}
	}
	pages, by, fails := s.fetch(cctx, site.root, links)
	tr.planned, tr.fetched, tr.failed = tr.planned+len(links), tr.fetched+len(pages), tr.failed+fails.n()
	if len(pages) == 0 {
		why := readFailNote(site.root, fails.list, fails.details)
		o := s.failed(ctx, cctx, fmt.Errorf("search: none of the %d pages of %s could be read: %s: %w", len(links), site.root, why, ErrNotFound))
		o.unread = strings.SplitN(why, "; ", 2)[0]
		return o, sfRead{}
	}
	title, name := readTitle(site.root, prof)
	r := readOf(site.root, title, name, pages, by)
	r.Origin, r.OriginRoots = origin.text, origin.roots
	return s.answerRead(ctx, site.root, title, name, pages, req, origin), r
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
	return fmt.Sprintf("read %s of %s found by web search; the whole site: ketch docs index %s", plural(read, "page"), root, root)
}

// searchAnswered is the local index's answer as the search-first
// backend's, with note (searchNote) and its origin. Its status is the
// evidence pipeline's: found when the question's distinctive terms are
// in the pages read, partial only for terms really missing (Build 1: the
// source line already says the pages came from a search).
func searchAnswered(o Outcome, note string, origin sfOrigin) Outcome {
	o.Origin, o.originRoots = origin.text, origin.roots
	if o.Status != Found && o.Status != Partial {
		for i := range o.Attempts {
			o.Attempts[i].Backend = SearchBackend
		}
		return o
	}
	for i := range o.Results {
		o.Results[i].Source = SearchBackend
	}
	o.Backend = SearchBackend
	o.Note = joinNote(o.Note, note)
	for i := range o.Attempts {
		o.Attempts[i].Backend = SearchBackend
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
// library it names, sites its name labels, and what resolve makes of the
// name within sfResolveTimeout (resolveNamed: one verified docs root, a
// preference here and the site fromSearch reads first, or several, which
// refuse the question). A bare question's first firm reading is resolved
// the same way, strictly. Resolving runs beside the web search, so it
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
	rctx, cancel := context.WithTimeout(ctx, sfResolveTimeout)
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

// chooseSite picks the site and reads its top hit's profile (its nav
// lists the siblings): a site with no docs signal in its hits must look
// like docs there (looksLikeDocs), or the next site is tried. A docs URL
// the request gives is the site, whatever the hits say.
func (s *SearchFirst) chooseSite(ctx context.Context, sites []sfSite, req sfRequest, prefs sitePrefs) (sfSite, *siteProfile, []sfSite, error) {
	if isHTTPURL(req.library) && len(prefs.roots) > 0 {
		site := givenSite(prefs.roots[0], sites)
		if u, err := url.Parse(req.library); err == nil && isPageFile(path.Base(u.Path)) {
			u.Fragment, u.RawQuery = "", ""
			site.page = u // the page the library names is read first, hits or none
		}
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
