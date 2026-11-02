package docs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
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

// resolveQuery resolves a bare query's library candidates concurrently and
// picks the first, in priority order, that resolves. One that is ambiguous,
// or whose failure could have changed the pick (vetoes: a registry down, a
// package whose docs could not be checked), stops the search rather than
// falling through to a weaker reading it might have outranked; one that
// names nothing, or whose only failure is a search that could not have
// found it, gives way to the next.
func (w *Web) resolveQuery(ctx context.Context, query string, splits []querySplit) (webMatch, querySplit, error) {
	if len(splits) == 0 {
		return webMatch{}, querySplit{}, fmt.Errorf("%w: no library named in %q; pass one with --library (CLI) or library (MCP): a docs URL, or a package name such as npm:astro", ErrLibraryRequired, query)
	}
	matches := make([]webMatch, len(splits))
	errs := make([]error, len(splits))
	ctx = withProfileMemo(ctx)
	var wg sync.WaitGroup
	for i, s := range splits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			matches[i], errs[i] = w.resolveOne(ctx, s.spec, s.strict)
		}()
	}
	wg.Wait()
	var tried []string
	var missed []*strictMissError
	for i, s := range splits {
		var amb *AmbiguousLibraryError
		var miss *strictMissError
		switch {
		case errs[i] == nil:
			if err := w.pastStrictMisses(ctx, query, matches[i], missed); err != nil {
				return webMatch{}, s, err
			}
			return matches[i], s, nil
		case errors.As(errs[i], &amb):
			return webMatch{}, s, errs[i]
		case ctx.Err() != nil:
			return webMatch{}, s, fmt.Errorf("web: %w", ctx.Err())
		case vetoes(errs[i], s, len(splits)):
			return webMatch{}, s, fmt.Errorf("web: reading %q as library %q: %s", query, s.spec.raw, errs[i]) // plain: upstream
		case errors.As(errs[i], &miss):
			missed = append(missed, miss)
		}
		tried = append(tried, fmt.Sprintf("%s: %v", s.spec.raw, errs[i]))
	}
	return webMatch{}, querySplit{}, fmt.Errorf("%w: no library identified in %q; pass one with --library (CLI) or library (MCP), a docs URL or a package name such as npm:astro (tried %s)",
		ErrLibraryRequired, query, strings.Join(tried, "; "))
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
	return w.rankPages(root, pages, query, tokens, label), nil
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
	return w.rankPages(root, pages, query, tokens, version), nil
}

// rankPages sections the pages, ranks the sections for query within the
// token budget and labels each result's version.
func (w *Web) rankPages(root webRoot, pages []webPage, query string, tokens int, version string) []Result {
	var sections []webSection
	for i, p := range pages {
		sections = append(sections, splitSections(p, i)...)
	}
	results := budgetResults(root.String(), rankSections(sections, query), tokens)
	labelResults(results, root, version)
	return results
}

// callErr classifies a failed call. A cancelled caller is reported as such;
// every timeout short of that (the operation timeout, the per-probe timeout,
// the HTTP client's) is a plain upstream failure. A slow site is worth
// retrying, not a cancellation, and an error that still wrapped
// context.DeadlineExceeded would read as one: [cancelled] over MCP while the
// CLI exits 4. So a fetch's own timeout keeps its text but loses its chain.
func (w *Web) callErr(parent, op context.Context, root webRoot, err error) error {
	switch {
	case parent.Err() != nil:
		return fmt.Errorf("web: %w", parent.Err())
	case op.Err() != nil:
		return fmt.Errorf("web: %s did not answer within %s", root, w.opTimeout)
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return errors.New(err.Error())
	}
	return err
}

// collect discovers the site and returns the pages to section: the whole of
// llms-full.txt when it is usable, otherwise the best-matching candidates.
func (w *Web) collect(ctx context.Context, root webRoot, query string) ([]webPage, error) {
	site, err := w.discover(ctx, root)
	if err != nil {
		return nil, err
	}
	if len(site.pages) > 0 {
		return site.pages, nil
	}
	if len(site.links) == 0 {
		return nil, fmt.Errorf("web: no docs pages under %s: %w (tried %s)", root, ErrNotFound, strings.Join(site.tried, ", "))
	}
	return w.fetchPages(ctx, root, pickCandidates(site.links, query, root, w.maxFetch))
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
