package docs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

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
	opTimeout      time.Duration // whole ResolveLibrary call
	probeTimeout   time.Duration // each discovery file (llms.txt, robots.txt, sitemap)
	sitemapBudget  time.Duration // all of one discovery's sitemaps together
	candidateWait  time.Duration // how long verification waits for every project's candidate roots
	concurrency    int           // concurrent page fetches
	maxFullBytes   int           // llms-full.txt larger than this is ignored
	maxPageBytes   int           // page text beyond this is cut before splitting
}

const (
	webCharsPerToken  = 4
	webOpTimeout      = 45 * time.Second
	webResolveTimeout = 20 * time.Second
	webProbeTimeout   = 10 * time.Second
	webSitemapBudget  = 8 * time.Second
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
		fetcher:        callFetcher{f},
		resolveTimeout: webResolveTimeout,
		opTimeout:      webOpTimeout,
		probeTimeout:   webProbeTimeout,
		sitemapBudget:  webSitemapBudget,
		candidateWait:  webCandidateWait,
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
	ctx = withFetchMemo(ctx)
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

// displayKey is linkKey of the page a URL is shown as (displayURL): a
// page and its Markdown variant share it.
func displayKey(u *url.URL) string {
	d, err := url.Parse(displayURL(u))
	if err != nil {
		return linkKey(u)
	}
	return linkKey(d)
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
	return webRoot{}, fmt.Errorf("%w: a docs site URL is needed (got %q), e.g. https://docs.astro.build", ErrLibraryRequired, raw)
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
// before they get here. Nor is a post below the root (isPostPath: a blog,
// news, a dated post), unless the root is itself in such a section.
func (r webRoot) contains(u *url.URL) bool {
	if !r.sameHost(u) || hasDotSegment(u) {
		return false
	}
	if r.prefix != "/" {
		p := u.Path
		if p != strings.TrimSuffix(r.prefix, "/") && !strings.HasPrefix(p, r.prefix) {
			return false
		}
	}
	return !isPostPath(pathBelow(r, u))
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
	// shell marks a page known to be an unrendered JS shell (no browser
	// rendered it): its text is what the server sent before scripts ran.
	shell bool
	// via is where the text came from when it is not the page at URL read
	// through the page pipeline: "md <url>", a Markdown source (x.md, or
	// the raw text a server sent for the page's address), or "chunk <url>",
	// a chunk of an llms file (llms-full.txt, an llms.txt index file). The
	// index records it (pageSource), and its check reads a page the same
	// way (index_fresh.go).
	via string
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
