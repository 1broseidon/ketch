package docs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/failure"
	"github.com/1broseidon/ketch/search"
)

// Web searches one documentation site directly, with no API key: the library
// ID is the docs root URL. Each call discovers pages (llms.txt, llms-full.txt,
// sitemaps, then links on the root page), fetches the few whose URL, title or
// description best match the query, splits them into heading sections and
// ranks the sections in memory. Nothing is persisted between calls (ADR-0004
// rejected a stored docs corpus); repeat fetches are the page cache's job,
// through the injected Fetcher.
type Web struct {
	fetcher Fetcher

	// Name resolution sources (phase 2). A nil source is not consulted:
	// NewWeb leaves all three nil, newWebFromConfig sets them.
	pkgs  pkgSource    // package registries
	repos repoSource   // GitHub, consulted only while githubOn allows (gh)
	sites siteSearcher // web search: beside the registries keyless, the last resort with GitHub
	// githubOn reports whether GitHub may be asked: a token is configured.
	// Without one the backend is keyless and never calls GitHub at all, so
	// nothing waits on its 60-an-hour anonymous quota and nothing is vetoed
	// because it did not answer. nil means repos is always asked (tests).
	githubOn func() bool
	// popularity reads registry download counts, the keyless tie-break
	// between same-name projects; nil asks no one (counts unknown).
	popularity popularitySource
	// workdirHint names the ecosystems the working directory's project files
	// (package.json, pyproject.toml, Cargo.toml, go.mod) belong to; nil for
	// none. Read once per call, never at construction.
	workdirHint func() []ecosystem

	resolveTimeout time.Duration // whole name resolution, within opTimeout
	opTimeout      time.Duration // whole GetDocs/ResolveLibrary call
	probeTimeout   time.Duration // each discovery file (llms.txt, robots.txt, sitemap)
	sitemapBudget  time.Duration // all of one discovery's sitemaps together
	candidateWait  time.Duration // how long verification waits for every project's candidate roots
	maxFetch       int           // candidate pages fetched per query
	concurrency    int           // concurrent page fetches
	maxFullBytes   int           // llms-full.txt larger than this is ignored
	maxPageBytes   int           // page text beyond this is cut before splitting
}

const (
	webDefaultTokens  = 4000
	webCharsPerToken  = 4
	webOpTimeout      = 45 * time.Second
	webResolveTimeout = 20 * time.Second
	webProbeTimeout   = 10 * time.Second
	webSitemapBudget  = 8 * time.Second
	webMaxFetch       = 6
	webFallbackFetch  = 3
	webConcurrency    = 4
	webMaxFullBytes   = 5 << 20
	webMaxPageBytes   = 1 << 20
	webMaxRawBytes    = 8 << 20
	webMaxSitemaps    = 10
	webSitemapDepth   = 2
	webMaxURLs        = 5000
	webMinTrimBytes   = 200
	webMinSectionText = 20
)

// NewWeb creates the web docs backend. A nil fetcher falls back to a minimal
// HTTP fetcher that reads llms.txt, sitemaps and Markdown pages but cannot
// convert HTML; the CLI and MCP server inject the scrape pipeline instead.
// Construction performs no I/O. The backend it returns resolves no library
// names (no registry, GitHub or search sources); the docs provider builds
// one that does with newWebFromConfig.
func NewWeb(f Fetcher) *Web {
	if f == nil {
		f = newWebHTTPFetcher()
	}
	return &Web{
		fetcher:        f,
		resolveTimeout: webResolveTimeout,
		opTimeout:      webOpTimeout,
		probeTimeout:   webProbeTimeout,
		sitemapBudget:  webSitemapBudget,
		candidateWait:  webCandidateWait,
		maxFetch:       webMaxFetch,
		concurrency:    webConcurrency,
		maxFullBytes:   webMaxFullBytes,
		maxPageBytes:   webMaxPageBytes,
	}
}

// newWebFromConfig is NewWeb with the name resolution sources: the package
// registries and GitHub over httpx.Default, and web search from c's search
// providers. It copies c and stays side-effect free, as LibraryBackends
// requires: the GitHub token (which can run `gh auth token`) is resolved on
// the first GitHub request and the search chain built on the first search,
// each at most once per backend.
func newWebFromConfig(c *config.Config, deps Deps) *Web {
	var cfg config.Config
	if c != nil {
		cfg = *c
	}
	w := NewWeb(deps.Fetcher)
	pkgs := newPkgClient(httpx.Default())
	w.pkgs = pkgs
	token := sync.OnceValue(func() string {
		token, _ := cfg.ResolveGithubToken()
		return token
	})
	w.repos = newGHClient(httpx.Default(), token)
	w.githubOn = func() bool { return token() != "" }
	w.popularity = newPopularityClient(pkgs)
	w.workdirHint = sync.OnceValue(workdirEcosystems)
	auto := sync.OnceValues(func() (*search.Auto, error) { return search.NewAutoFromConfig(&cfg, "") })
	w.sites = siteSearchFunc(func(ctx context.Context, query string, limit int) ([]search.Result, error) {
		a, err := auto()
		if err != nil {
			return nil, err
		}
		return a.Search(ctx, query, limit)
	})
	return w
}

// siteSearchFunc adapts a function to siteSearcher.
type siteSearchFunc func(ctx context.Context, query string, limit int) ([]search.Result, error)

func (f siteSearchFunc) Search(ctx context.Context, query string, limit int) ([]search.Result, error) {
	return f(ctx, query, limit)
}

// Search answers a bare docs query: splitLibraryQuery reads it as at most
// three library candidates, resolved concurrently; the first in priority
// order that resolves to exactly one library is searched for the rest of
// the query, and the first that is ambiguous stops the search with its
// *AmbiguousLibraryError. With no candidate resolved the query is a
// validation error (ErrLibraryRequired) naming what was tried. A backend
// with no resolution sources (NewWeb) takes no bare queries at all. limit
// caps the results when positive; the token budget is the default.
func (w *Web) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if !w.resolvesNames() {
		return nil, fmt.Errorf("%w: the web backend searches one docs site; pass its URL with --library (CLI) or library (MCP), e.g. --library https://docs.astro.build", ErrLibraryRequired)
	}
	m, split, err := w.resolveQuery(withQueryHint(ctx, queryEcosystems(query)), query, splitLibraryQuery(query))
	if err != nil {
		return nil, err
	}
	// The root came from third-party metadata: its pages' redirects stay
	// public too.
	results, err := w.docsAt(publicHops(ctx), m.root, split.query, webDefaultTokens, m.version)
	if err != nil {
		return nil, err
	}
	return Truncate(results, limit), nil
}

// resolveQuery resolves a bare query's readings concurrently and picks one
// (decision-15):
//
//   - A reading inside the words of a more specific reading that answered
//     (react inside react-router) is settled by it, whatever it gave.
//   - A reading that is ambiguous stops the query with its ambiguity,
//     unless it is a common word and another reading answered.
//   - A failure that could have changed the pick (vetoes: a registry down,
//     a package whose docs could not be checked) stops it as an upstream
//     error, the first in priority order. A guess's never does, and a
//     common word's only when no other reading answered: "set" failing in
//     "set up logging in loguru" says nothing about loguru.
//   - Readings of different words that answered with different libraries
//     contest: one stands only when it outweighs every other by the
//     dominance margin (contest), else the query is ambiguous between
//     them. Which reading the query's syntax ranks first does not decide
//     which library it is about.
//   - A reading that named a real package whose docs did not verify
//     (strictMissError) keeps another from answering unless outweighed
//     (pastStrictMisses).
//   - A common word's pick must clear commonWordBar.
//
// With nothing answered the query is a validation error naming what was
// tried.
func (w *Web) resolveQuery(ctx context.Context, query string, splits []querySplit) (webMatch, querySplit, error) {
	if len(splits) == 0 {
		return webMatch{}, querySplit{}, fmt.Errorf("%w: no library named in %q; pass one with --library (CLI) or library (MCP): a docs URL, or a package name such as npm:astro", ErrLibraryRequired, query)
	}
	ctx = withProfileMemo(ctx)
	matches, errs := w.resolveReadings(ctx, splits)
	if ctx.Err() != nil {
		return webMatch{}, querySplit{}, fmt.Errorf("web: %w", ctx.Err())
	}
	var picks []int
	for i := range splits {
		if errs[i] == nil {
			picks = append(picks, i)
		}
	}
	picks = slices.DeleteFunc(slices.Clone(picks), func(i int) bool { return settledBy(i, picks, splits) })
	var tried []string
	var missed []readingMiss
	for i, s := range splits {
		if errs[i] == nil || settledBy(i, picks, splits) {
			continue
		}
		if miss, stop := readingStop(query, s, errs[i], len(picks) > 0, len(splits)); stop != nil {
			return webMatch{}, s, stop
		} else if miss != nil {
			missed = append(missed, readingMiss{miss, s.common})
		}
		tried = append(tried, fmt.Sprintf("%s: %v", s.spec.raw, errs[i]))
	}
	if len(picks) == 0 {
		return webMatch{}, querySplit{}, fmt.Errorf("%w: no library identified in %q; pass one with --library (CLI) or library (MCP), a docs URL or a package name such as npm:astro (tried %s)",
			ErrLibraryRequired, query, strings.Join(tried, "; "))
	}
	i, err := w.contest(ctx, query, splits, matches, picks)
	if err != nil {
		return webMatch{}, splits[picks[0]], err
	}
	if err := w.pastStrictMisses(ctx, query, matches[i], missed); err != nil {
		return webMatch{}, splits[i], err
	}
	return matches[i], splits[i], nil
}

// resolveReadings resolves every reading concurrently; a common word's
// pick that does not clear commonWordBar is its reading's error.
func (w *Web) resolveReadings(ctx context.Context, splits []querySplit) ([]webMatch, []error) {
	matches := make([]webMatch, len(splits))
	errs := make([]error, len(splits))
	var wg sync.WaitGroup
	for i, s := range splits {
		wg.Go(func() {
			matches[i], errs[i] = w.resolveReading(ctx, s)
			if errs[i] == nil && s.common {
				errs[i] = commonWordBar(s, matches[i])
			}
		})
	}
	wg.Wait()
	return matches, errs
}

// readingStop is what a reading's failure means for the query: an error
// that stops it (its ambiguity, a common word's only when nothing
// answered; an upstream failure that vetoes, a common word's only when
// nothing answered), or a miss that weighs against the pick.
func readingStop(query string, s querySplit, err error, answered bool, readings int) (*strictMissError, error) {
	var amb *AmbiguousLibraryError
	var miss *strictMissError
	switch {
	case errors.As(err, &amb) && (!s.common || !answered):
		return nil, err
	case (!s.common || !answered) && vetoes(err, s, readings):
		return nil, fmt.Errorf("web: reading %q as library %q: %s", query, s.spec.raw, err) // plain: upstream
	case errors.As(err, &miss):
		return miss, nil
	}
	return nil, nil
}

// settledBy reports reading i lying within the words of another reading
// in picks that is more specific (spans more words) or, over the same
// words, comes first.
func settledBy(i int, picks []int, splits []querySplit) bool {
	s := splits[i]
	for _, j := range picks {
		o := splits[j]
		if j == i || o.from > s.from || s.to > o.to {
			continue
		}
		if o.to-o.from > s.to-s.from || j < i {
			return true
		}
	}
	return false
}

// contest picks among readings that answered: the first in priority
// order when they all name one library; else the one that outweighs each
// other by the dominance margin (>= dominanceStars and dominanceRatio
// times, weights as the dominance rule reads them), and with none, an
// ambiguity listing them. A weight that could not be read outweighs
// nothing and is outweighed by nothing, and a common word's reading
// outweighs no reading of an uncommon word: npm's debug has far more
// downloads than quasar, but "debug output in quasar" is not therefore
// about debug.
func (w *Web) contest(ctx context.Context, query string, splits []querySplit, matches []webMatch, picks []int) (int, error) {
	var distinct []int
	seen := map[string]bool{}
	for _, i := range picks {
		k := cmp.Or(matches[i].project, matches[i].root.String())
		if !seen[k] {
			seen[k] = true
			distinct = append(distinct, i)
		}
	}
	if len(distinct) == 1 {
		return distinct[0], nil
	}
	for _, i := range distinct {
		if p := matches[i].proj; p != nil && matches[i].stars < 0 {
			w.readPopularity(ctx, []*project{p})
			matches[i].stars = p.stars()
		}
	}
	for _, i := range distinct {
		if !slices.ContainsFunc(distinct, func(j int) bool { return j != i && !readingOutweighs(splits, matches, i, j) }) {
			return i, nil
		}
	}
	amb := &AmbiguousLibraryError{Library: query}
	for _, i := range distinct {
		amb.Matches = append(amb.Matches, matches[i].libraryMatch())
	}
	return 0, amb
}

// readingOutweighs reports reading i's pick outweighing reading j's by the
// dominance margin, both weights known, and i no common word beside an
// uncommon j.
func readingOutweighs(splits []querySplit, matches []webMatch, i, j int) bool {
	if matches[j].stars < 0 || splits[i].common && !splits[j].common {
		return false
	}
	return outweighsBy(int64(matches[i].stars), int64(matches[j].stars))
}

// commonWordBar is why a common word's reading does not name the library
// it resolved to, nil when it does: the library must be in common use,
// with at least dominanceStars weekly downloads (stars, with GitHub), the
// weight the dominance rule asks of any pick. A registry has a package for
// nearly every word, and a word in a question is far more often the word.
// A weight that could not be read does not clear the bar: the reading is
// then not found, never an upstream failure.
func commonWordBar(s querySplit, m webMatch) error {
	if m.stars >= dominanceStars {
		return nil
	}
	weight := "its weight could not be read"
	if m.stars >= 0 {
		weight = fmt.Sprintf("%s has %s", m.root, weightText(m.proj, m.stars))
	}
	return fmt.Errorf("web: %q is a common word, which names a library only when that library is in common use (a weight of %d or more); %s: %w",
		s.spec.raw, dominanceStars, weight, ErrNotFound)
}

// vetoes reports a reading's failure that stops a bare query: an upstream
// one that could plausibly have changed which library was picked.
//
//   - Nothing vetoes a guessed (strict) reading (react-router from
//     "react router"), which only speculates that the words name a
//     package: a registry rate-limiting it (crates.io's 429), GitHub
//     failing or a site not answering counts as not found, so the readings
//     other sources settle still answer.
//   - A registry that could not be asked about the name vetoes any other
//     reading: the package may exist.
//   - For another reading, a library that verified but could not be
//     confirmed past a failed source (a registry, GitHub's search, or a
//     repository GitHub did not read for another project of the name)
//     vetoes: the reading names a library, so a weaker one must not answer
//     in its place. So does a homepage or docs root that did not answer
//     for a package a registry has; GitHub failing alone with nothing
//     verified (its search, a rate-limited repository, which hide only a
//     repository's homepage and stars) vetoes only the query's only
//     reading.
//
// Anything else reads as not found, so the next reading is tried: a
// rate-limited GitHub for the guess "fastapi-dependency", or for the junk
// npm package "production" in "… in production", does not stop the query.
func vetoes(err error, s querySplit, readings int) bool {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) {
		return false
	}
	if s.strict {
		return false
	}
	var up *upstreamError
	switch {
	case !errors.As(err, &up), up.registryFailed:
		return true
	case up.picked, up.nameKnown && up.siteFailed:
		return true
	}
	return readings == 1
}

// ResolveLibrary resolves a library spec to its docs roots. A docs URL
// gives one match, the normalized root (moved to the version root a
// redirect names), titled from the site's llms.txt (H1 and first
// blockquote) or the root page, with the site's versions and whatever
// evidence ties it to a repository; it is never refused for want of
// evidence. A name ([eco:]name[@version], owner/repo, a Go module) gives
// every verified match, best first, at most limit when positive: per
// library its docs site and its registry-hosted docs, each with trust,
// evidence, versions and the package specs to pass as the library.
func (w *Web) ResolveLibrary(ctx context.Context, name string, limit int) ([]LibraryMatch, error) {
	spec, err := w.parseLibrary(name)
	if err != nil {
		return nil, err
	}
	log := &resolveLog{}
	matches, err := w.resolveLogged(ctx, spec, limit, log)
	if err != nil {
		return nil, err
	}
	note, incomplete := incompleteNote(matches, log)
	out := make([]LibraryMatch, 0, len(matches))
	for _, m := range matches {
		if incomplete {
			// First, so the evidence line and the JSON lead with it.
			m.evidence = append([]evidence{note}, m.evidence...)
		}
		out = append(out, m.libraryMatch())
	}
	return out, nil
}

// GetDocs returns the sections of the library's docs that best match
// query, best first. libraryID is a docs root URL, searched as given (a
// root that redirects to a version root, such as /en/stable/ to /en/6.1/,
// is followed there), or a library spec, resolved to exactly one library
// first: an ambiguous name is an *AmbiguousLibraryError. Results carry the
// resolved root as their Library and the version their URL names.
//
// tokens bounds the total snippet text at about 4 bytes per token (a value
// <= 0 means 4000): sections are added in rank order until the budget is
// spent, and the last one is trimmed to fit, marked "[truncated]", rather
// than overshooting. GetDocs applies no count bound of its own; the surfaces
// apply a positive limit afterwards with Truncate, so limit caps how many of
// these sections are returned and never raises the text bound. Sections that
// match no query term are not returned, so a query the site does not cover
// yields an empty result rather than an error.
func (w *Web) GetDocs(ctx context.Context, libraryID, query string, tokens int) ([]Result, error) {
	spec, err := w.parseLibrary(libraryID)
	if err != nil {
		return nil, err
	}
	if spec.url != "" {
		return w.docsForURL(ctx, spec.url, query, tokens)
	}
	m, err := w.resolveOne(ctx, spec, false)
	if err != nil {
		return nil, err
	}
	return w.docsAt(publicHops(ctx), m.root, query, tokens, m.version)
}

// parseLibrary reads a library value. A docs URL is taken as given (phase
// 1), unless parseLibSpec finds it malformed (a URL with @version). A name
// needs resolution sources; without them (NewWeb) it is a validation
// error, as is a malformed spec.
func (w *Web) parseLibrary(raw string) (libSpec, error) {
	raw = strings.TrimSpace(raw)
	if isHTTPURL(raw) {
		spec, err := parseLibSpec(raw)
		switch {
		case errors.Is(err, ErrLibraryRequired):
			return libSpec{}, err
		case err == nil && spec.url != "":
			return spec, nil
		}
		return libSpec{raw: raw, url: raw}, nil
	}
	if !w.resolvesNames() {
		return libSpec{}, errNoNameSources(raw)
	}
	spec, err := parseLibSpec(raw)
	switch {
	case err != nil && !errors.Is(err, ErrLibraryRequired):
		return libSpec{}, fmt.Errorf("%w: %v", ErrLibraryRequired, err)
	case err != nil:
		return libSpec{}, err
	}
	return spec, nil
}

// docsForURL searches the docs root the caller passed. When discovery
// finds nothing there and the root redirects to a version root
// (canonicalRoot), it searches that root instead; when it redirects to
// another site that is corroborated as the same project's docs (movedRoot:
// docs.python-requests.org to requests.readthedocs.io), that site.
func (w *Web) docsForURL(ctx context.Context, raw, query string, tokens int) ([]Result, error) {
	root, err := parseWebRoot(raw)
	if err != nil {
		return nil, err
	}
	opCtx, cancel := context.WithTimeout(ctx, w.opTimeout)
	defer cancel()
	pages, err := w.collect(opCtx, root, query)
	if errors.Is(err, ErrNotFound) && opCtx.Err() == nil {
		if canon, ok := w.versionRedirect(opCtx, root); ok {
			root = canon
			pages, err = w.collect(opCtx, root, query)
		} else if moved, _, ok := w.movedRoot(opCtx, root, nil); ok {
			root = moved
			pages, err = w.collect(publicHops(opCtx), root, query)
		}
	}
	if err != nil {
		return nil, w.callErr(ctx, opCtx, root, err)
	}
	label, _ := rootVersion(root)
	return w.rankPages(opCtx, root, pages, query, tokens, label), nil
}

// versionRedirect reports the version root root redirects to, when it
// redirects only by swapping or adding a version segment.
func (w *Web) versionRedirect(ctx context.Context, root webRoot) (webRoot, bool) {
	pctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	defer cancel()
	raw, err := w.fetcher.FetchRaw(pctx, root.String())
	if err != nil || raw.URL == "" {
		return webRoot{}, false
	}
	served, err := url.Parse(raw.URL)
	if err != nil || linkKey(served) == linkKey(root.u) {
		return webRoot{}, false
	}
	return canonicalRoot(root, served)
}

// docsAt searches a resolved root, labelling results with version where
// their URLs name none.
func (w *Web) docsAt(ctx context.Context, root webRoot, query string, tokens int, version string) ([]Result, error) {
	opCtx, cancel := context.WithTimeout(ctx, w.opTimeout)
	defer cancel()
	pages, err := w.collect(opCtx, root, query)
	if err != nil {
		return nil, w.callErr(ctx, opCtx, root, err)
	}
	return w.rankPages(opCtx, root, pages, query, tokens, version), nil
}

// rankPages sections the pages, ranks the sections for query within the
// token budget, checks the anchors it cites (citeChecked) and labels each
// result's version.
func (w *Web) rankPages(ctx context.Context, root webRoot, pages []webPage, query string, tokens int, version string) []Result {
	var sections []webSection
	for i, p := range pages {
		sections = append(sections, splitSections(p, i)...)
	}
	ranked := diversify(dedupe(relevant(rankSections(sections, query))))
	results, used := budgetSections(root.String(), ranked, tokens)
	w.citeChecked(ctx, root, pages, results, used)
	labelResults(results, root, version)
	return results
}

// webMaxCiteChecks caps the pages citeChecked fetches for their anchors.
const webMaxCiteChecks = 4

// webCiteTimeout bounds citeChecked's fetches together.
const webCiteTimeout = 5 * time.Second

// citeChecked makes the anchors of results from pages read as Markdown (a
// page.md, an llms-full.txt chunk), whose targets were never seen, ones
// the rendered page carries. It fetches each such page that a result
// cites, at most webMaxCiteChecks, through FetchPage (the page cache's
// copy when warm) for its targets, and cites its sections as
// verifyAnchors would have had the targets been known. A page that could
// not be checked keeps only the anchors the Markdown itself fixed (an
// explicit {#id}, one link evidence proved): its slugged anchors are
// dropped, citing the page instead of a section that may not exist.
// used[i] is the section results[i] came from.
func (w *Web) citeChecked(ctx context.Context, root webRoot, pages []webPage, results []Result, used []webSection) {
	var check []int // pages to fetch, in order of first citation
	seen := map[int]bool{}
	for _, s := range used {
		if unchecked(pages[s.page]) && !seen[s.page] {
			seen[s.page] = true
			check = append(check, s.page)
		}
	}
	if len(check) == 0 {
		return
	}
	found := w.citedAnchors(ctx, root, pages, check[:min(len(check), webMaxCiteChecks)])
	resplit := map[int][]webSection{}
	for p, anchors := range found {
		page := pages[p]
		page.Anchors = anchors
		resplit[p] = splitSections(page, p)
	}
	for i, s := range used {
		if !unchecked(pages[s.page]) {
			continue
		}
		if again, ok := resplit[s.page]; ok && s.order < len(again) {
			s.anchor = again[s.order].anchor
		} else if s.source != anchorFixed {
			s.anchor = ""
		}
		results[i].URL = s.URL()
	}
}

// unchecked reports a page read from Markdown whose targets are unknown.
func unchecked(p webPage) bool { return p.markdown && p.Anchors == nil }

// citedAnchors reads the targets of the pages at the given indices,
// concurrently (w.concurrency at a time) within webCiteTimeout; a page
// that could not be read is left out.
func (w *Web) citedAnchors(ctx context.Context, root webRoot, pages []webPage, idx []int) map[int][]PageAnchor {
	cctx, cancel := context.WithTimeout(ctx, webCiteTimeout)
	defer cancel()
	found := make([][]PageAnchor, len(idx))
	sem := make(chan struct{}, max(1, w.concurrency))
	var wg sync.WaitGroup
	for i, p := range idx {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-cctx.Done():
				return
			}
			defer func() { <-sem }()
			found[i] = w.pageAnchors(cctx, root, pages[p].URL)
		})
	}
	wg.Wait()
	out := map[int][]PageAnchor{}
	for i, p := range idx {
		if found[i] != nil {
			out[p] = found[i]
		}
	}
	return out
}

// pageAnchors fetches the page at raw for the targets it carries; nil when
// it cannot say (a failed fetch, a redirect out of the docs root, a
// fetcher that does not read targets).
func (w *Web) pageAnchors(ctx context.Context, root webRoot, raw string) []PageAnchor {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	page, err := w.fetcher.FetchPage(ctx, raw)
	if err != nil {
		return nil
	}
	if _, err := servedFrom(u, page.URL, root.contains); err != nil {
		return nil
	}
	return page.Anchors
}

// callErr classifies a failed call (internal/failure). A cancelled caller is
// reported as such; every timeout short of that (the operation timeout, the
// per-probe timeout, the HTTP client's) is an upstream timeout: a slow site
// is worth retrying. A context cancelled inside the call, never by the
// caller, is an upstream interruption; its chain is cut so it cannot read as
// the caller cancelling.
func (w *Web) callErr(parent, op context.Context, root webRoot, err error) error {
	switch {
	case parent.Err() != nil:
		return fmt.Errorf("web: %w", parent.Err())
	case op.Err() != nil:
		return failure.Wrap(failure.Upstream, "timeout", fmt.Errorf("web: %s did not answer within %s", root, w.opTimeout))
	case errors.Is(err, context.DeadlineExceeded):
		return failure.Wrap(failure.Upstream, "timeout", errors.New(err.Error()))
	case errors.Is(err, context.Canceled):
		return failure.Wrap(failure.Upstream, "interrupted", errors.New(err.Error()))
	}
	return err
}

// collect discovers the site and returns the pages to section: the whole of
// llms-full.txt when it is usable, otherwise the best-matching candidates,
// led by a web search's pages on the site when its own page list names
// little of the query (searchPages).
func (w *Web) collect(ctx context.Context, root webRoot, query string) ([]webPage, error) {
	site, err := w.discover(ctx, root)
	if err != nil {
		return nil, err
	}
	if len(site.pages) > 0 {
		return w.fullTextPages(ctx, root, site, query), nil
	}
	if len(site.links) == 0 {
		return nil, fmt.Errorf("web: no docs pages under %s: %w (tried %s)", root, ErrNotFound, strings.Join(site.tried, ", "))
	}
	w.widenLLMS(ctx, root, &site, query)
	cands := w.linkCandidates(ctx, root, site, query)
	if len(site.index) > 0 {
		cands = interleaveLinks(indexPages(site.index, query, w.maxFetch), cands, w.maxFetch)
	}
	return w.fetchPages(ctx, root, cands)
}

// linkCandidates are the pages the site's page list offers for query, best
// first (pickCandidates), led by a web search's pages on the site when the
// list names little of the query (searchPages): its titles and paths are
// all the candidates have to go on, where a site's full text (its search
// index, llms-full.txt) is a ranking of its own (collect, fullTextPages).
func (w *Web) linkCandidates(ctx context.Context, root webRoot, site webSite, query string) []webLink {
	cands := pickCandidates(site.links, query, root, w.maxFetch)
	if linkCoverage(site.links, query, root) < webWeakCoverage && !registryDocsHosts[scopeHost(root.u)] {
		cands = withSearchPages(w.searchPages(ctx, root, query), cands, w.maxFetch)
	}
	return cands
}

// fullTextPages picks the pages to answer from when llms-full.txt holds
// the docs' text: as for any site with a full-text index (collect), the
// pages whose sections best match the query, interleaved one for one with
// the best llms.txt candidates by title and description, at most
// maxFetch. A page llms-full.txt holds is read from it; one it does not
// hold is fetched (GitBook's llms-full.txt is paginated, its first part a
// hundred pages of hundreds, while llms.txt lists them all). Ranking a
// few chosen pages rather than every page llms-full.txt holds keeps
// sections of pages that only mention the query's words (a large site's
// many reference pages) from crowding out the page about it. A page that
// cannot be fetched is left out.
func (w *Web) fullTextPages(ctx context.Context, root webRoot, site webSite, query string) []webPage {
	held := map[string][]webPage{}
	var sections []webSection
	for i, p := range site.pages {
		sections = append(sections, splitSections(p, i)...)
		if u, err := url.Parse(p.URL); err == nil {
			held[displayKey(u)] = append(held[displayKey(u)], p)
		}
	}
	var listed []webLink
	switch {
	case linkCoverage(site.links, query, root) > 0:
		listed = w.linkCandidates(ctx, root, site, query)
	case !registryDocsHosts[scopeHost(root.u)]:
		listed = w.searchPages(ctx, root, query) // no listed page to guess with
	}
	var out []webPage
	var missing []webLink
	for _, l := range interleaveLinks(indexPages(sections, query, w.maxFetch), listed, w.maxFetch) {
		if ps, ok := held[displayKey(l.url)]; ok {
			out = append(out, ps...)
			delete(held, displayKey(l.url)) // x and x.md are one page
		} else {
			missing = append(missing, l)
		}
	}
	if len(missing) > 0 {
		if fetched, err := w.fetchPages(ctx, root, missing); err == nil {
			out = append(out, fetched...)
		}
	}
	if len(out) == 0 {
		return site.pages
	}
	return out
}

// displayKey is linkKey of the page a URL is shown as (displayURL): a
// page and its Markdown variant share it.
func displayKey(u *url.URL) string {
	d, err := url.Parse(displayURL(u))
	if err != nil {
		return linkKey(u)
	}
	return linkKey(d)
}

// webWeakCoverage is the link coverage (linkCoverage) below which the
// site's own titles and paths are no guide to the query's pages, and a web
// search is asked for them.
const webWeakCoverage = 0.25

// webMaxSearchPages caps the pages a web search adds to the candidates.
const webMaxSearchPages = 3

// searchPages asks the web search for the query's pages on the docs site,
// for when the site's own page list names little of the query (its URLs
// are opaque, or its titles word the topic otherwise: Playwright's
// waitForLoadState lives on "class-page"). It returns the hits under the
// root, at most webMaxSearchPages, in the search's order: candidates, like
// every search hit, which the root's scope admits and ranking judges. A
// hit on a page's llms.txt stands for the page. Nil without a searcher,
// or when the search fails.
func (w *Web) searchPages(ctx context.Context, root webRoot, query string) []webLink {
	terms := queryTerms(query)
	if w.sites == nil || len(terms) == 0 {
		return nil
	}
	site := strings.TrimPrefix(strings.TrimPrefix(root.String(), "https://"), "http://")
	hits, err := w.searchHits(ctx, site+" "+strings.Join(strings.Fields(query), " "))
	if err != nil {
		return nil
	}
	var out []webLink
	seen := map[string]bool{}
	rootLabel := rootVersionLabel(root)
	for _, h := range hits {
		u, ok := resolveRef(root.u, strings.TrimSpace(h.URL))
		if !ok {
			continue
		}
		if isLLMSFile(path.Base(u.Path)) {
			u.Path, u.RawPath = path.Dir(u.Path), ""
		}
		if !root.contains(u) || !isPageURL(u) || !isResultLink(u) || seen[linkKey(u)] {
			continue
		}
		// The root's spelling of its host: a search engine may index
		// python-httpx.org for www.python-httpx.org, which need not resolve.
		u.Scheme, u.Host = root.u.Scheme, root.u.Host
		// Another release's copy of the page (/docs/14/… under an
		// unversioned root, /en/5.2/… under /en/4.2/) is the wrong version.
		if _, pref := variantKey(u, root, rootLabel); pref.class > 2 {
			continue
		}
		seen[linkKey(u)] = true
		out = append(out, webLink{url: u, title: h.Title})
		if len(out) == webMaxSearchPages {
			break
		}
	}
	return out
}

// withSearchPages puts the search's pages ahead of the picked candidates,
// keeping n in all and each page once.
func withSearchPages(hits, picked []webLink, n int) []webLink {
	if len(hits) == 0 {
		return picked
	}
	out := slices.Clone(hits)
	seen := map[string]bool{}
	for _, l := range hits {
		seen[linkKey(l.url)] = true
	}
	for _, l := range picked {
		if len(out) >= n {
			break
		}
		if !seen[linkKey(l.url)] {
			seen[linkKey(l.url)] = true
			out = append(out, l)
		}
	}
	return out
}

// webRoot is a normalized docs root and the scope every page must fall in:
// the same host, and a path under the root's prefix (the whole host when the
// prefix is "/"). A leading "www." is not a different host: sites redirect
// one to the other (mintlify.com → www.mintlify.com) and list their pages
// under the canonical one.
type webRoot struct {
	u      *url.URL // scheme://host/prefix/, no query or fragment
	prefix string   // always ends in "/"
}

// parseWebRoot normalizes a docs root URL: lower-case scheme and host, no
// default port, query, fragment or dot segments, and a path ending in "/".
// A URL naming a file (llms*.txt, a page such as index.html or guide.md) is
// taken as the directory holding it.
func parseWebRoot(raw string) (webRoot, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && isWebScheme(u.Scheme) && u.Hostname() != "" && cleanDots(u) {
		u = &url.URL{Scheme: strings.ToLower(u.Scheme), Host: hostKey(u), Path: u.Path, RawPath: u.RawPath}
		if base := path.Base(u.Path); isLLMSFile(base) || isPageFile(base) {
			u.Path, u.RawPath = path.Dir(u.Path), ""
		}
		if !strings.HasSuffix(u.Path, "/") {
			u.Path, u.RawPath = u.Path+"/", ""
		}
		return webRoot{u: u, prefix: u.Path}, nil
	}
	return webRoot{}, fmt.Errorf("%w: the web backend takes the docs site URL as the library (got %q), e.g. https://docs.astro.build", ErrLibraryRequired, raw)
}

// pageFileExts are the extensions that make a root's last path segment a
// file rather than a directory. Version segments (2.x, v3.1) have
// extensions too, so this is a list, not any dot.
var pageFileExts = map[string]bool{
	".html": true, ".htm": true, ".xhtml": true, ".shtml": true,
	".md": true, ".mdx": true, ".markdown": true,
	".php": true, ".asp": true, ".aspx": true, ".jsp": true,
}

// isPageFile reports a path segment naming a page file: one with a page
// extension, or any index.* (index.html, index.md, index.php).
func isPageFile(base string) bool {
	base = strings.ToLower(base)
	return pageFileExts[path.Ext(base)] || strings.HasPrefix(base, "index.")
}

func (r webRoot) String() string {
	if r.u == nil {
		return ""
	}
	return r.u.String()
}

// origin returns scheme://host with no trailing slash.
func (r webRoot) origin() string { return r.u.Scheme + "://" + r.u.Host }

// at returns the URL for a path relative to the root directory.
func (r webRoot) at(rel string) string { return r.u.String() + rel }

// contains reports whether u is in scope. The scheme may differ (sitemaps
// often list https for an http root); the host and path prefix may not. A
// path with a dot segment is never in scope: /docs/%2e%2e/admin/ reads as
// under /docs/ but a server resolves it to /admin/. resolve cleans links
// before they get here.
func (r webRoot) contains(u *url.URL) bool {
	if !r.sameHost(u) || hasDotSegment(u) {
		return false
	}
	if r.prefix == "/" {
		return true
	}
	p := u.Path
	return p == strings.TrimSuffix(r.prefix, "/") || strings.HasPrefix(p, r.prefix)
}

// sameHost reports a web URL on the root's host, "www." aside, whatever its
// path: where discovery files (robots.txt, sitemaps, an origin llms.txt)
// may live.
func (r webRoot) sameHost(u *url.URL) bool {
	return u != nil && isWebScheme(u.Scheme) && scopeHost(u) == scopeHost(r.u)
}

// resolve resolves href against base and returns it without its fragment
// when it is in scope.
func (r webRoot) resolve(base *url.URL, href string) (*url.URL, bool) {
	u, ok := resolveRef(base, href)
	if !ok || !r.contains(u) {
		return nil, false
	}
	return u, true
}

// resolveRef resolves href against base, drops the fragment and removes
// dot segments, so the URL that is scope-checked, fetched and shown is the
// one a server would serve. It fails for an unparsable href.
func resolveRef(base *url.URL, href string) (*url.URL, bool) {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return nil, false
	}
	u := base.ResolveReference(ref)
	u.Fragment, u.RawFragment = "", ""
	return u, cleanDots(u)
}

// dotSegment reports a path segment, already percent-decoded, that a server
// treats as "." or "..": also "..;x" (Tomcat drops path parameters first).
// A segment hiding one behind an encoded separator ("..%2Fadmin", or a
// backslash, which IIS reads as a slash) is reported as unresolvable:
// servers disagree on whether that is a separator.
func dotSegment(seg string) (dots string, unresolvable bool) {
	hidden := strings.ContainsAny(seg, "/\\")
	for _, part := range strings.FieldsFunc(seg, func(r rune) bool { return r == '/' || r == '\\' }) {
		part, _, _ = strings.Cut(part, ";")
		if part != "." && part != ".." {
			continue
		}
		if hidden {
			return "", true
		}
		return part, false
	}
	return "", false
}

// hasDotSegment reports a path with a dot segment once each segment is
// percent-decoded ("%2e%2e" is ".." to a server).
func hasDotSegment(u *url.URL) bool {
	for _, seg := range strings.Split(u.EscapedPath(), "/") {
		dec, err := url.PathUnescape(seg)
		if err != nil {
			dec = seg
		}
		if dots, bad := dotSegment(dec); dots != "" || bad {
			return true
		}
	}
	return false
}

// cleanDots removes u's dot segments the way RFC 3986 does, deciding what
// is one after percent-decoding each segment but splitting only on literal
// slashes, as servers do; an encoded slash stays inside its segment. It
// reports false for a path it cannot resolve safely.
func cleanDots(u *url.URL) bool {
	if !hasDotSegment(u) {
		return true
	}
	segs := strings.Split(u.EscapedPath(), "/")
	out := make([]string, 0, len(segs))
	for i, seg := range segs {
		dec, err := url.PathUnescape(seg)
		if err != nil {
			dec = seg
		}
		dots, bad := dotSegment(dec)
		switch {
		case bad:
			return false
		case dots == "":
			out = append(out, seg)
			continue
		case dots == ".." && len(out) > 1:
			out = out[:len(out)-1]
		}
		if i == len(segs)-1 {
			out = append(out, "") // a trailing dot segment leaves a directory
		}
	}
	escaped := strings.Join(out, "/")
	if !strings.HasPrefix(escaped, "/") {
		escaped = "/" + escaped
	}
	p, err := url.PathUnescape(escaped)
	if err != nil {
		return false
	}
	u.Path, u.RawPath = p, escaped
	return true
}

func isWebScheme(s string) bool {
	return strings.EqualFold(s, "http") || strings.EqualFold(s, "https")
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && isWebScheme(u.Scheme) && u.Hostname() != ""
}

// hostKey is the lower-case host with the scheme's default port dropped.
func hostKey(u *url.URL) string {
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if port == "" || (port == "443" && strings.EqualFold(u.Scheme, "https")) || (port == "80" && strings.EqualFold(u.Scheme, "http")) {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, port)
}

// scopeHost is hostKey without a leading "www.".
func scopeHost(u *url.URL) string { return strings.TrimPrefix(hostKey(u), "www.") }

// webPage is one page's Markdown ready to split into sections.
type webPage struct {
	URL      string // shown in results; the rendered page, not a .md variant
	Title    string
	Markdown string
	// Anchors are the fragment targets the page at URL carries, as the
	// fetcher read them from its HTML; nil when unknown (a Markdown source,
	// an llms-full.txt chunk). splitSections cites only these when known.
	Anchors []PageAnchor
	// markdown marks a page whose text came from a Markdown source (a
	// page.md, llms-full.txt) rather than the page at URL, whose targets
	// citeChecked may still read.
	markdown bool
}

// webLink is a candidate page found during discovery.
type webLink struct {
	url         *url.URL
	title       string
	description string
	page        *webPage // content already in hand (the root page), else nil
}

// displayURL is the address results point at. A Markdown variant
// (page.md, page.mdx, dir/index.md, page.html.md) is shown as the rendered
// page it stands for, extension stripped: llms.txt generators (Mintlify, the
// Docusaurus and VitePress plugins, Fumadocs) publish x.md beside x, and the
// heading anchors in a result only resolve on the rendered page. .txt files
// have no rendered counterpart and keep their own URL.
func displayURL(u *url.URL) string {
	out := *u
	out.Fragment, out.RawFragment = "", ""
	ext := strings.ToLower(path.Ext(out.Path))
	if ext == ".md" || ext == ".mdx" || ext == ".markdown" {
		out.Path = strings.TrimSuffix(out.Path, path.Ext(out.Path))
		if path.Base(out.Path) == "index" {
			out.Path = strings.TrimSuffix(out.Path, "index")
		}
		out.RawPath = ""
	}
	return out.String()
}

// isMarkdownURL reports a link that is already text and goes through
// FetchRaw rather than the HTML scrape pipeline.
func isMarkdownURL(u *url.URL) bool {
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".md", ".mdx", ".markdown", ".txt":
		return true
	}
	return false
}

// scopeError reports a response that came from outside the docs root: a
// redirect off the host or out of the root's path, or a fetcher that did
// not say where the response came from. It wraps ErrNotFound: the page the
// backend asked for is, for its purposes, not there, so one candidate that
// redirects away is skipped like a missing one and a root that does reads
// as not-found, with the redirect named.
type scopeError struct {
	requested string
	served    string // "" when the fetcher did not report it
}

func (e *scopeError) Error() string { return e.requested + " " + e.reason() }

// reason is the error without the requested URL.
func (e *scopeError) reason() string {
	if e.served == "" {
		return "came back without the URL it was served from"
	}
	return "redirects to " + e.served + ", outside the docs root"
}

func (e *scopeError) Unwrap() error { return ErrNotFound }

// servedFrom parses the URL a fetcher reported for a response to requested
// and checks it with inScope, returning it without a fragment.
func servedFrom(requested *url.URL, served string, inScope func(*url.URL) bool) (*url.URL, error) {
	u, err := url.Parse(served)
	if served == "" || err != nil {
		return nil, &scopeError{requested: requested.String()}
	}
	u.Fragment, u.RawFragment = "", ""
	if !inScope(u) {
		return nil, &scopeError{requested: requested.String(), served: u.String()}
	}
	return u, nil
}

// isLLMSFile matches llms.txt and its llms-full/llms-small siblings.
func isLLMSFile(base string) bool {
	base = strings.ToLower(base)
	return strings.HasPrefix(base, "llms") && strings.HasSuffix(base, ".txt")
}

// Probe needs no network: the backend is keyless and has no fixed endpoint,
// since every call names the site it reads.
func probeWeb(context.Context, *http.Client, *config.Config) (health.Status, string) {
	return health.StatusOK, "keyless; reads the docs site passed as the library"
}

func webProvider() Provider {
	return Provider{
		ID:          "web",
		Name:        "Web",
		LibraryHelp: "a docs URL, or a package name: name@version, npm:/pypi:/crates:/go:/github: prefixes, owner/repo",
		Usable:      func(*config.Config) bool { return true },
		New:         func(c *config.Config, deps Deps) (Searcher, error) { return newWebFromConfig(c, deps), nil },
		Probe:       probeWeb,
	}
}
