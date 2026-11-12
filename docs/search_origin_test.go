package docs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/failure"
	"github.com/1broseidon/ketch/search"
)

// Build 1, right source or say so (design/docs-evidence.md): the fixtures
// are a project's own docs (docs.lib.test), a third-party site about it
// (libtips.test) and a package host serving many projects.

const (
	ownRoot = "https://docs.lib.test/"
	fanRoot = "https://libtips.test/"
)

// originSite serves the project's docs (a root page and a sitemap), the
// third-party site's page, and a package host's pages for two projects.
func originSite() *fakeFetcher {
	return &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://docs.lib.test/sitemap.xml": xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://docs.lib.test/guide/pool</loc></url></urlset>`),
		},
		pages: map[string]fakePage{
			ownRoot:                       {title: "Lib", markdown: "# Lib\n\nLib is a database toolkit.\n"},
			ownRoot + "guide/pool":        {title: "Pool", markdown: "# Pool\n\n## Max connections\n\nSet max_connections on the pool builder to cap the open connections.\n"},
			fanRoot + "lib/max-conns":     {title: "Max connections guide", markdown: "# Max connections\n\n## Pool size\n\nA post on raising max_connections in a pool.\n"},
			fanRoot + "lib/pool-tuning":   {title: "Pool tuning guide", markdown: "# Pool tuning\n\n## Pool size\n\nTuning max_connections in a busy pool.\n"},
			"https://pkg.go.dev/net/http": {title: "http package - net/http - Go Packages", markdown: "# http\n\n## Client\n\nA Client is an HTTP client. Set Timeout on the Client to limit the time of a request.\n"},
			"https://pkg.go.dev/net/http/httptest": {title: "httptest package - net/http/httptest - Go Packages",
				markdown: "# httptest\n\n## Server\n\nA Server is an HTTP server for tests; its Client has a Timeout too.\n"},
			"https://pkg.go.dev/github.com/someone/tour/http": {title: "http package - github.com/someone/tour/http - Go Packages",
				markdown: "# http\n\n## Client timeout\n\nA tour of setting a Client Timeout.\n"},
		},
	}
}

func fanHits() []search.Result {
	return []search.Result{hit(fanRoot+"lib/max-conns", "Max connections guide"), hit(fanRoot+"lib/pool-tuning", "Pool tuning guide")}
}

// originSearch is the backend over f answering every search with hits,
// resolve answering r for any library a request names.
func originSearch(t *testing.T, f Fetcher, hits []search.Result, r sfResolved) *SearchFirst {
	t.Helper()
	w := NewWeb(newFakePageCache(asFake(f), time.Hour))
	if _, ok := f.(*fakeFetcher); !ok {
		w = NewWeb(f)
	}
	w.sites = siteSearchFunc(func(context.Context, string, int) ([]search.Result, error) { return hits, nil })
	s := NewSearchFirst(w, NewLocal(newStore(t)))
	s.resolveName = func(context.Context, sfRequest) sfResolved { return r }
	return s
}

func asFake(f Fetcher) *fakeFetcher {
	if ff, ok := f.(*fakeFetcher); ok {
		return ff
	}
	return &fakeFetcher{}
}

func verifiedMatch(root string, ev string, proj *project) *webMatch {
	return &webMatch{root: rootOf(root), trust: 8, evidence: []evidence{{kind: evPkgDocs, detail: ev}}, proj: proj, stars: -1, packages: []string{"npm:lib", "github:a/lib"}}
}

// A name resolve finds one docs root for is answered from that root, not
// from the third-party site the search agrees on; with only the registry
// name to support it, the origin says "by name" and names the site the
// search results favour, and the hint gives both roots. When the search
// results agree on the root, it is verified.
func TestResolvedRootOrigin(t *testing.T) {
	f := originSite()
	s := originSearch(t, f, fanHits(), sfResolved{name: "lib", match: verifiedMatch(ownRoot, "npm lib lists it as Documentation", nil)})
	o := s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if o.Status != Found || o.Library != ownRoot {
		t.Fatalf("outcome = %s from %q (%v), want found from %s", o.Status, o.Library, o.Err(), ownRoot)
	}
	if want := "by name, docs.lib.test documents the npm package lib; the search results favour " + fanRoot; o.Origin != want {
		t.Errorf("origin = %q, want %q", o.Origin, want)
	}
	if !OriginByName(o.Origin) || OriginUnverified(o.Origin) {
		t.Errorf("origin kind wrong: %q", o.Origin)
	}
	if want := "origin by name: only a name ties this site to what you asked; name the site you cite, or pass the docs you mean with --library " + ownRoot + " or --library " + fanRoot; OriginHint(HintCLI, o) != want {
		t.Errorf("hint = %q, want %q", OriginHint(HintCLI, o), want)
	}
	if h := OriginHint(HintMCP, o); !strings.HasSuffix(h, `library="`+ownRoot+`" or library="`+fanRoot+`"`) {
		t.Errorf("MCP hint = %q", h)
	}
	assertNeverFetched(t, f, "libtips.test")

	agree := []search.Result{hit(ownRoot+"guide/pool", "Pool guide"), hit(fanRoot+"lib/max-conns", "Max connections guide")}
	s = originSearch(t, originSite(), agree, sfResolved{name: "lib", match: verifiedMatch(ownRoot, "npm lib lists it as Documentation", nil)})
	o = s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if want := "verified, npm lib lists it as Documentation (resolve, trust 8.0), and the search results agree"; o.Origin != want {
		t.Errorf("agreeing search: origin = %q, want %q", o.Origin, want)
	}
	if OriginHint(HintCLI, o) != "" {
		t.Errorf("verified answer has a hint: %q", OriginHint(HintCLI, o))
	}
}

// The language's own docs page verifies a standard-library module.
func TestStdModuleOrigin(t *testing.T) {
	page := "https://docs.python.org/3/library/pathlib.html"
	m := &webMatch{root: rootOf(page), page: mustURL(t, page), std: true, trust: 8,
		evidence: []evidence{{kind: evRegistryDocs, detail: "docs.python.org documents pathlib in the Python standard library"}}}
	f := originSite()
	f.pages[page] = fakePage{title: "pathlib — Object-oriented filesystem paths", markdown: "# pathlib\n\n## Globbing\n\nPath.glob with ** matches files recursively.\n"}
	s := originSearch(t, f, fanHits(), sfResolved{name: "pathlib", match: m})
	o := s.GetDocsSelect(context.Background(), "pathlib", "glob recursively", 0)
	if o.Status != Found || o.Library != "https://docs.python.org/3/library/" || o.Origin != "verified, docs.python.org documents pathlib in the Python standard library (resolve, trust 8.0)" {
		t.Fatalf("outcome = %s from %q, origin %q (%v)", o.Status, o.Library, o.Origin, o.Err())
	}
}

func TestResolvePyStd(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://docs.python.org/3/library/pathlib.html": html(`<html><head><title>pathlib — Object-oriented filesystem paths — Python 3.14 documentation</title></head><body><a href="glob.html">glob</a></body></html>`),
	}}
	w := NewWeb(f)
	m, ok := w.resolvePyStd(withFetchMemo(context.Background()), "pathlib")
	if !ok || m.root.String() != "https://docs.python.org/3/library/" || m.page.String() != "https://docs.python.org/3/library/pathlib.html" || !m.std {
		t.Fatalf("pathlib: %v %+v", ok, m)
	}
	for _, name := range []string{"requests", "Path-lib", "a/b"} {
		if _, ok := w.resolvePyStd(withFetchMemo(context.Background()), name); ok {
			t.Errorf("%s verified as a standard-library module", name)
		}
	}
	for pk, want := range map[string]bool{"pypi:pathlib,github:a/pathlib": true, "npm:uuid": false, "pypi:x,crates:x": false} {
		if got := onlyPyPI(strings.Split(pk, ",")); got != want {
			t.Errorf("onlyPyPI(%s) = %t", pk, got)
		}
	}
}

// The leading site the search agrees on answers when it is the verified
// project's own: on the host of its Go module path, beside the package
// index's page resolve found.
func TestLeadOnTheProjectsSite(t *testing.T) {
	proj := &project{pkgs: []pkgInfo{{eco: ecoGo, name: "libtips.test/lib"}}}
	s := originSearch(t, originSite(), fanHits(), sfResolved{name: "lib", match: verifiedMatch("https://pkg.go.dev/libtips.test/lib/", "pkg.go.dev hosts the docs of go libtips.test/lib", proj)})
	o := s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if o.Status != Found || o.Library != fanRoot {
		t.Fatalf("outcome = %s from %q (%v), want found from the lead", o.Status, o.Library, o.Err())
	}
	if want := "verified, on libtips.test, the host of go module libtips.test/lib"; o.Origin != want {
		t.Errorf("origin = %q, want %q", o.Origin, want)
	}
}

// A site no evidence ties to the library answers with an unverified
// origin naming its host and the library asked for.
func TestUnverifiedOrigin(t *testing.T) {
	s := originSearch(t, originSite(), fanHits(), sfResolved{name: "lib"})
	o := s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if o.Status != Found {
		t.Fatalf("outcome = %s (%v)", o.Status, o.Err())
	}
	if want := "unverified, ketch could not confirm libtips.test as the official documentation of lib"; o.Origin != want {
		t.Errorf("origin = %q, want %q", o.Origin, want)
	}
	if !OriginUnverified(o.Origin) {
		t.Error("OriginUnverified = false")
	}
}

func ambiguity(ms ...LibraryMatch) sfResolved {
	return sfResolved{name: "lib", ambiguous: &AmbiguousLibraryError{Library: "lib", Matches: ms}}
}

var (
	crateLib = LibraryMatch{ID: "https://docs.rs/lib/latest/lib/", Title: "lib - Rust", Packages: []string{"crates:lib", "github:a/lib"}, TrustScore: 8}
	goLib    = LibraryMatch{ID: "https://pkg.go.dev/github.com/b/lib/", Title: "lib package - github.com/b/lib - Go Packages", Packages: []string{"go:github.com/b/lib", "github:b/lib"}, TrustScore: 8}
	tipsLib  = LibraryMatch{ID: "https://pkg.go.dev/libtips.test/lib/", Title: "lib package - libtips.test/lib - Go Packages", Packages: []string{"go:libtips.test/lib"}, TrustScore: 8}
	pypiLib  = LibraryMatch{ID: "pypi:lib", Title: "docs not verified", Packages: []string{"pypi:lib"}}
)

// Two libraries with verified docs that a package index hosts, in two
// ecosystems: the question is refused with both listed, each with its
// packages and title, never answered from what the search returned.
func TestAmbiguousNameRefused(t *testing.T) {
	s := originSearch(t, originSite(), fanHits(), ambiguity(crateLib, goLib, pypiLib))
	o := s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	var amb *AmbiguousLibraryError
	if o.Status != Refused || !errors.As(o.Err(), &amb) || failure.Classify(o.Err(), failure.Upstream).Kind != failure.Validation {
		t.Fatalf("outcome = %s (%v), want a validation refusal", o.Status, o.Err())
	}
	msg := o.Err().Error()
	for _, want := range []string{crateLib.ID, "crates:lib", "lib - Rust", goLib.ID, "go:github.com/b/lib", "pass one with --library"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error lacks %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "pypi:lib") || strings.Contains(msg, "libtips.test/lib/max") {
		t.Errorf("error lists a candidate with no verified docs, or a search hit: %s", msg)
	}
}

// The search agreeing on one candidate's own site settles the name: gorm.io
// is go:gorm.io/gorm's, beside an older module and a namesake crate. With
// a single verified candidate resolve was not sure, and the search's site
// answers.
func TestAmbiguitySettled(t *testing.T) {
	s := originSearch(t, originSite(), fanHits(), ambiguity(crateLib, tipsLib, goLib))
	o := s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if o.Status != Found || o.Library != fanRoot || !strings.HasPrefix(o.Origin, "verified, the search agrees on the own site of go:libtips.test/lib") {
		t.Fatalf("outcome = %s from %q, origin %q (%v)", o.Status, o.Library, o.Origin, o.Err())
	}
	s = originSearch(t, originSite(), fanHits(), ambiguity(crateLib, pypiLib))
	o = s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if o.Status != Found || o.Library != fanRoot || !OriginUnverified(o.Origin) {
		t.Fatalf("one verified: outcome = %s from %q, origin %q (%v)", o.Status, o.Library, o.Origin, o.Err())
	}
}

// A question's language word picks among the candidates.
func TestLanguagePick(t *testing.T) {
	ms := []LibraryMatch{crateLib, goLib}
	for q, want := range map[string]int{"lib pool in rust": 0, "golang lib pool": 1, "lib pool": -1, "rust or go lib": -1} {
		i, ok := languagePick(ms, q)
		if !ok {
			i = -1
		}
		if i != want {
			t.Errorf("%q: pick %d, want %d", q, i, want)
		}
	}
}

// A package host's hits belong to the projects whose paths they are on:
// the library name as a path is the root, only pages under it are read,
// and the library line shows it.
func TestSharedHostIsNotALibrary(t *testing.T) {
	f := originSite()
	hits := []search.Result{
		hit("https://pkg.go.dev/net/http", "http package - net/http - Go Packages"),
		hit("https://pkg.go.dev/github.com/someone/tour/http", "http package - github.com/someone/tour/http - Go Packages"),
		hit("https://pkg.go.dev/net/http/httptest", "httptest package - net/http/httptest - Go Packages"),
	}
	s := originSearch(t, f, hits, sfResolved{name: "net/http"})
	o := s.GetDocsSelect(context.Background(), "net/http", "client timeout", 0)
	if o.Status != Found || o.Library != "https://pkg.go.dev/net/http/" {
		t.Fatalf("outcome = %s from %q (%v), want found from pkg.go.dev/net/http/", o.Status, o.Library, o.Err())
	}
	if want := "verified, pkg.go.dev documents net/http in the Go standard library"; o.Origin != want {
		t.Errorf("origin = %q, want %q", o.Origin, want)
	}
	for _, r := range o.Results {
		if !strings.HasPrefix(r.URL, "https://pkg.go.dev/net/http") {
			t.Errorf("result from another project: %s", r.URL)
		}
	}
	assertNeverFetched(t, f, "someone")
}

func TestSharedProjectRoot(t *testing.T) {
	for in, want := range map[string]string{
		"https://pkg.go.dev/github.com/someone/tour/net/http": "https://pkg.go.dev/github.com/someone/tour/",
		"https://pkg.go.dev/go.uber.org/zap/zapcore":          "https://pkg.go.dev/go.uber.org/zap/",
		"https://pkg.go.dev/net/http":                         "https://pkg.go.dev/net/http/",
		"https://pkg.go.dev/net/http@go1.22.0":                "https://pkg.go.dev/net/http/",
		"https://godocs.test/github.com/owner/repo/sub":       "https://godocs.test/github.com/owner/repo/",
		"https://pythonhosted.org/lib/api.html":               "https://pythonhosted.org/lib/",
		"https://docs.lib.test/guide/pool":                    "",
		"https://docs.rs/lib/latest/lib/":                     "",
		"https://docs.lib.test/v1.2/guide":                    "",
		"https://docs.lib.test/index.html":                    "",
	} {
		u, _ := url.Parse(in)
		r, ok := sharedProjectRoot(u)
		got := ""
		if ok {
			got = r.String()
		}
		if got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
	// A single name is a path on a host the hits show serving several
	// projects, never the host's own name.
	for _, tc := range []struct {
		page, name string
		shared     bool
		want       string
	}{
		{"https://hexdocs.test/phoenix/Phoenix.Router.html", "phoenix", true, "https://hexdocs.test/phoenix/"},
		{"https://hexdocs.test/phoenix/Phoenix.Router.html", "phoenix", false, ""},
		{"https://react.test/react/hooks", "react", true, ""},
		{"https://pkg.go.dev/net/http/httptest", "net/http", false, "https://pkg.go.dev/net/http/"},
		{"https://pkg.go.dev/net/url", "net/http", false, ""},
	} {
		u, _ := url.Parse(tc.page)
		r, ok := namedProjectRoot(u, tc.name, tc.shared)
		got := ""
		if ok {
			got = r.String()
		}
		if got != tc.want {
			t.Errorf("%s as %q (shared %t): %q, want %q", tc.page, tc.name, tc.shared, got, tc.want)
		}
	}
}

// statusFetcher fails the URLs in status with that HTTP status.
type statusFetcher struct {
	*fakeFetcher
	status map[string]int
}

type statusErr int

func (e statusErr) Error() string   { return fmt.Sprintf("HTTP %d", int(e)) }
func (e statusErr) HTTPStatus() int { return int(e) }

func (f statusFetcher) FetchPage(ctx context.Context, u string) (PageContent, error) {
	if code, ok := f.status[u]; ok {
		return PageContent{}, statusErr(code)
	}
	return f.fakeFetcher.FetchPage(ctx, u)
}

func (f statusFetcher) FetchRaw(ctx context.Context, u string) (RawContent, error) {
	if code, ok := f.status[u]; ok {
		return RawContent{}, statusErr(code)
	}
	return f.fakeFetcher.FetchRaw(ctx, u)
}

// A site whose pages cannot be read says why, per cause, and what to do
// next; when the next site answers, its note names the one that failed.
func TestReadFailureSaysWhy(t *testing.T) {
	f := statusFetcher{fakeFetcher: originSite(), status: map[string]int{fanRoot + "lib/max-conns": 403, fanRoot + "lib/pool-tuning": 403}}
	s := originSearch(t, f, fanHits(), sfResolved{name: "lib"})
	o := s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	msg := fmt.Sprint(o.Err())
	if o.Status != Refused || !strings.Contains(msg, "none of the 2 pages of "+fanRoot+" could be read: 2 blocked (HTTP 403)") ||
		!strings.Contains(msg, "configure browser") {
		t.Fatalf("outcome = %s: %s", o.Status, msg)
	}

	shells := originSite()
	for _, p := range []string{fanRoot + "lib/max-conns", fanRoot + "lib/pool-tuning"} {
		shells.pages[p] = fakePage{title: "App", markdown: "Loading", shell: true}
	}
	o = originSearch(t, shells, fanHits(), sfResolved{name: "lib"}).GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if msg := fmt.Sprint(o.Err()); !strings.Contains(msg, "2 need JavaScript") || !strings.Contains(msg, "configure browser for full content") {
		t.Errorf("JS shells: %s", msg)
	}

	// The verified root blocked: the search's site answers, saying so.
	blocked := statusFetcher{fakeFetcher: originSite(), status: map[string]int{ownRoot: 403, ownRoot + "guide/pool": 403}}
	o = originSearch(t, blocked, fanHits(), sfResolved{name: "lib", match: verifiedMatch(ownRoot, "npm lib lists it as Documentation", nil)}).
		GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if o.Status != Found || o.Library != fanRoot || !strings.Contains(o.Note, "could not read "+ownRoot+" (2 blocked (HTTP 403))") || !OriginUnverified(o.Origin) {
		t.Errorf("after a blocked root: %s from %q, note %q, origin %q (%v)", o.Status, o.Library, o.Note, o.Origin, o.Err())
	}
}

// A library URL naming one page is read, hits under it or none; a root
// with no page to read says what was looked for.
func TestPageLibraryReadsThePage(t *testing.T) {
	f := originSite()
	f.pages["https://man.test/man3/printf.3.html"] = fakePage{title: "printf(3)", markdown: "# printf\n\n```\nThe precision of a floating point conversion is the number of digits after the point.\n```\n"}
	s := originSearch(t, f, fanHits(), sfResolved{})
	o := s.GetDocsSelect(context.Background(), "https://man.test/man3/printf.3.html", "precision floating point conversion", 0)
	if o.Status != Found || o.Origin != "verified, the library value names this site" {
		t.Fatalf("outcome = %s, origin %q (%v)", o.Status, o.Origin, o.Err())
	}
	o = s.GetDocsSelect(context.Background(), "https://man.test/man1/", "precision floating point conversion", 0)
	if msg := fmt.Sprint(o.Err()); !strings.Contains(msg, "found no page of https://man.test/man1/ to read") || !strings.Contains(msg, "https://man.test/") {
		t.Errorf("empty root: %s", msg)
	}
}

// pkg.go.dev's own page for a path with no host first verifies a
// standard-library package.
func TestResolveGoStd(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://pkg.go.dev/net/http": html(`<html><head><title>http package - net/http - Go Packages</title></head><body><a href="/net/http/httptest">httptest</a></body></html>`),
	}}
	w := NewWeb(f)
	m, ok := w.resolveGoStd(withFetchMemo(context.Background()), "net/http")
	if !ok || m.root.String() != "https://pkg.go.dev/net/http/" || !m.std || !strings.Contains(m.evidence[0].detail, "Go standard library") {
		t.Fatalf("net/http: %v %+v", ok, m)
	}
	for _, name := range []string{"facebook/react", "context", "Net/Http", "github.com/a/b"} {
		if _, ok := w.resolveGoStd(withFetchMemo(context.Background()), name); ok {
			t.Errorf("%s verified as a standard-library package", name)
		}
	}
}

func TestReadCause(t *testing.T) {
	for _, tc := range []struct {
		err   error
		cause string
	}{
		{statusErr(404), readNotFound},
		{statusErr(403), readBlocked},
		{statusErr(429), readBlocked},
		{statusErr(500), readOther},
		{fmt.Errorf("page: %w", context.DeadlineExceeded), readTimeout},
		{&scopeError{requested: "https://a.test/x", served: "https://b.test/x"}, readRedirected},
		{fmt.Errorf("gone: %w", ErrNotFound), readNotFound},
	} {
		if got, _ := readCause(tc.err); got != tc.cause {
			t.Errorf("%v: %q, want %q", tc.err, got, tc.cause)
		}
	}
}

// A domain that carries the name asked is a name match, never
// verification, with or without docs in its address: the official site
// and a fan site alike are by name.
func TestDomainNameIsByName(t *testing.T) {
	req := sfRequest{query: "raspberry pi enable ssh"}
	prefs := sitePrefs{words: questionWords(req.query)}
	official := sfSite{key: "raspberrypi.test", root: rootOf("https://www.raspberrypi.test/documentation/")}
	fan := sfSite{key: "raspberry.test", root: rootOf("https://raspberry.test/en/"), hits: []sfHit{{u: mustURL(t, "https://raspberry.test/en/tutorials/enable-ssh")}}}
	if got := siteOrigin(official, req, prefs).text; got != "by name, the domain raspberrypi.test carries the name raspberrypi; nothing else confirms it as the official documentation" {
		t.Errorf("official: %q", got)
	}
	lib := sfRequest{query: "enable ssh", library: "fly.io", spec: libSpec{raw: "fly.io", name: "fly.io"}}
	if got := siteOrigin(sfSite{root: rootOf("https://docs.fly.io/")}, lib, sitePrefs{names: []string{libraryKey("fly.io")}}).text; !OriginByName(got) {
		t.Errorf("docs.fly.io for fly.io: %q", got)
	}
	if got := siteOrigin(fan, req, prefs).text; !OriginByName(got) || strings.HasPrefix(got, "verified") {
		t.Errorf("fan site: %q", got)
	}
}

// A docs site that moved and answers at its old address with a stub (a
// script, a canonical link, a meta refresh in noscript) is read where it
// went.
func TestMovedSiteStubIsFollowed(t *testing.T) {
	f := originSite()
	stub := `<!DOCTYPE html><meta charset="utf-8"><title>Redirecting to new.lib.test</title>` +
		`<link rel="canonical" href="https://new.lib.test/manual/"><script>window.location.replace("https://new.lib.test/manual")</script>` +
		`<noscript><meta http-equiv="refresh" content="0; url=https://new.lib.test/manual/"></noscript>`
	f.raw["https://old.lib.test/manual/"] = html(stub)
	f.pages["https://old.lib.test/manual/"] = fakePage{title: "Redirecting", markdown: "Redirecting", shell: true}
	f.pages["https://new.lib.test/manual/"] = fakePage{title: "Manual", markdown: "# Manual\n\n## Select\n\nselect(f) keeps the objects for which f is true, such as select(.id == 1).\n"}
	hits := []search.Result{hit("https://old.lib.test/manual/", "Lib manual"), hit("https://forum.example.test/t/x/1", "x")}
	s := originSearch(t, f, hits, sfResolved{name: "lib"})
	o := s.GetDocsSelect(context.Background(), "lib", "select objects by id", 0)
	if o.Status != Found || !strings.HasPrefix(o.Library, "https://new.lib.test/") {
		t.Fatalf("outcome = %s from %q (%v), want the new site", o.Status, o.Library, o.Err())
	}
}

func TestNamedWithDescriptor(t *testing.T) {
	for _, tc := range []struct {
		domain string
		names  []string
		want   bool
	}{
		{"jqlang.org", []string{"jq"}, true},
		{"djangoproject.com", []string{"django"}, true},
		{"jqtips.org", []string{"jq"}, false},
		{"jq.org", []string{"jq"}, false},
		{"raspberry.tips", []string{"raspberrypi"}, false},
	} {
		if got := namedWithDescriptor(tc.domain, tc.names); got != tc.want {
			t.Errorf("%s for %v: %t, want %t", tc.domain, tc.names, got, tc.want)
		}
	}
}

// A standard-library module's own page is read first even when the
// search leads with another site of the language's domain.
func TestStdModuleNotDisplacedBySiblingSite(t *testing.T) {
	page := "https://docs.python.org/3/library/pathlib.html"
	m := &webMatch{root: rootOf(page), page: mustURL(t, page), std: true}
	if leadOwned([]sfSite{{root: rootOf("https://peps.python.org/")}}, m) {
		t.Error("peps.python.org stands in for the module's page")
	}
	if !leadOwned([]sfSite{{root: rootOf("https://docs.python.org/3/library/")}}, m) {
		t.Error("the library tree does not")
	}
}

// A domain named by the repository owner is a name match: the lead is
// read, labelled by name.
func TestOwnerNamedDomainIsByName(t *testing.T) {
	proj := &project{repo: repoKey{owner: "libtips", name: "lib"}}
	s := originSearch(t, originSite(), fanHits(), sfResolved{name: "lib", match: verifiedMatch("https://pkg.go.dev/github.com/libtips/lib/", "pkg.go.dev hosts the docs of go github.com/libtips/lib", proj)})
	o := s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if o.Library != fanRoot || o.Origin != "by name, the domain libtips.test carries the name of libtips, owner of github.com/libtips/lib; nothing else confirms it as the official documentation" {
		t.Fatalf("outcome %s from %q, origin %q (%v)", o.Status, o.Library, o.Origin, o.Err())
	}
}

// The resolved root holding none of the question's terms falls over to
// the site the search results favour, in the same call; only when that
// holds none either is the question refused.
func TestResolvedRootFallsOver(t *testing.T) {
	m := verifiedMatch(ownRoot, "npm lib homepage", nil)
	s := originSearch(t, originSite(), fanHits(), sfResolved{name: "lib", match: m})
	o := s.GetDocsSelect(context.Background(), "lib", "tuning when busy", 0)
	if o.Status != Found || o.Library != fanRoot || !strings.HasPrefix(o.Origin, "unverified, ketch could not confirm libtips.test") ||
		!strings.HasSuffix(o.Origin, "; "+ownRoot+", which resolve found for lib, holds none of the question's terms, so this answer is from the site the search results favour") {
		t.Fatalf("outcome %s from %q, origin %q (%v)", o.Status, o.Library, o.Origin, o.Err())
	}
	o = s.GetDocsSelect(context.Background(), "lib", "zeppelin quaternion", 0)
	if o.Status != Refused || !strings.Contains(fmt.Sprint(o.Err()), "nor does "+fanRoot+", the site the search results favour") {
		t.Fatalf("neither: %s (%v)", o.Status, o.Err())
	}
}

// A one-element Go standard-library package is verified by its
// pkg.go.dev page, and goes ahead of a Go module named like it; beside a
// library of another ecosystem it is one candidate of an ambiguity.
func TestGoStdOneElement(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://pkg.go.dev/context": html(`<html><head><title>context package - context - Go Packages</title></head><body></body></html>`),
	}}
	w := NewWeb(f)
	sm, ok := w.resolveGoStd(withFetchMemo(context.Background()), "context")
	if !ok || sm.root.String() != "https://pkg.go.dev/context/" {
		t.Fatalf("context: %v %+v", ok, sm)
	}
	module := webMatch{root: rootOf("https://pkg.go.dev/github.com/someone/context/"), packages: []string{"go:github.com/someone/context"}}
	out := combineStd(sfResolved{name: "context"}, []*webMatch{&sm}, module, nil, "pass a deadline")
	if out.match != &sm || len(out.candidates) != 2 {
		t.Fatalf("beside a Go module: match %+v, candidates %d", out.match, len(out.candidates))
	}
	crate := webMatch{root: rootOf("https://docs.rs/context/latest/context/"), packages: []string{"crates:context"}}
	out = combineStd(sfResolved{name: "context"}, []*webMatch{&sm}, crate, nil, "pass a deadline")
	if out.match != nil || out.ambiguous == nil || len(out.ambiguous.Matches) != 2 {
		t.Fatalf("beside a crate: %+v", out)
	}
	if out = combineStd(sfResolved{name: "context"}, []*webMatch{&sm}, crate, nil, "pass a deadline in golang"); out.match != &sm {
		t.Fatalf("a language word: %+v", out)
	}
	// The search agreeing on the standard-library page settles it.
	p := sitePrefs{resolved: combineStd(sfResolved{name: "context"}, []*webMatch{&sm}, crate, nil, "pass a deadline")}
	if refusal := settleAmbiguity(&p, []sfSite{{root: rootOf("https://pkg.go.dev/context/")}}); refusal != nil || p.resolved.match != &sm {
		t.Fatalf("settled: %v %+v", refusal, p.resolved.match)
	}
}

// A named library the search agrees on no site for is refused with what
// resolve knows by the name, never the sites the search returned; with
// nothing resolved, the sites are named as the search's, none tied to
// what was asked.
func TestNamedRefusalListsResolveCandidates(t *testing.T) {
	split := []search.Result{hit("https://one.test/a/guide", "A guide"), hit("https://two.test/b/guide", "B guide")}
	cands := []LibraryMatch{{ID: "npm:lib", Title: "docs not verified", Packages: []string{"npm:lib"}}, crateLib}
	s := originSearch(t, originSite(), split, sfResolved{name: "lib", candidates: cands})
	o := s.GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	msg := fmt.Sprint(o.Err())
	if o.Status != Refused || !strings.Contains(msg, `the search results agree on no site for "lib"; resolve knows it as: npm:lib (docs not verified; npm:lib), `+crateLib.ID) ||
		strings.Contains(msg, "one.test") || failure.Classify(o.Err(), failure.Upstream).Kind != failure.Validation {
		t.Fatalf("%s: %s", o.Status, msg)
	}
	o = originSearch(t, originSite(), split, sfResolved{name: "lib"}).GetDocsSelect(context.Background(), "lib", "pool max_connections", 0)
	if msg := fmt.Sprint(o.Err()); !strings.Contains(msg, "the search results are split between several sites, none ahead and none tied to what was asked: ") || strings.Contains(msg, "documentation sites") {
		t.Fatalf("nothing resolved: %s", msg)
	}
}

// A domain's main host goes ahead of a sibling subdomain the question
// does not name; a sibling it names is a project of its own.
func TestMainHostOverSibling(t *testing.T) {
	main, four := rootOf("https://lib.test/docs/"), rootOf("https://four.lib.test/docs/")
	words := questionWords("lib swap attributes")
	if !mainHostOver(main, four, words) || mainHostOver(four, main, words) {
		t.Error("main host not preferred")
	}
	if mainHostOver(main, four, questionWords("lib four swap attributes")) {
		t.Error("a named sibling lost to the main host")
	}
	if mainHostOver(rootOf("https://www.cloud.test/"), rootOf("https://developers.cloud.test/"), questionWords("cloud workers")) {
		t.Error("a docs host lost to the marketing apex")
	}
	if !treeBetter(0.1, 0.9, main, four, "", words) {
		t.Error("treeBetter ranks the sibling first")
	}
}

// A package index's whole host is not the tree of one package's path,
// and Python's page agrees with any tree of its docs host.
func TestSameTreeAndStdAgreement(t *testing.T) {
	if sameTree(rootOf("https://pkg.go.dev/"), rootOf("https://pkg.go.dev/fmt/")) {
		t.Error("pkg.go.dev/ holds pkg.go.dev/fmt/ as one tree")
	}
	if !sameTree(rootOf("https://docs.rs/lib/latest/"), rootOf("https://docs.rs/lib/latest/lib/")) {
		t.Error("a crate's version tree is not its tree")
	}
	py := &webMatch{root: rootOf("https://docs.python.org/3/library/json.html"), std: true}
	if !stdAgrees(py, rootOf("https://docs.python.org/3.11/")) || stdAgrees(py, rootOf("https://peps.python.org/")) {
		t.Error("Python's docs host agreement")
	}
	gostd := &webMatch{root: rootOf("https://pkg.go.dev/fmt/"), std: true}
	if stdAgrees(gostd, rootOf("https://pkg.go.dev/")) || !stdAgrees(gostd, rootOf("https://pkg.go.dev/fmt/")) {
		t.Error("Go's package path agreement")
	}
	if leadOwned([]sfSite{{root: rootOf("https://docs.python.org/")}}, py) {
		t.Error("a tree over the module's page stands in for it")
	}
}

// A package index's whole host is no site the search can favour.
func TestSearchLeadSkipsIndexHostRoot(t *testing.T) {
	docs := docsHits(fuseWeighted([][]search.Result{{hit("https://docs.rs/", "Docs.rs")}}, nil))
	if l := searchLead(docs, sitePrefs{}); l != nil {
		t.Errorf("lead = %s", l)
	}
}

// A namesake's root: a package of the name lib whose homepage is about
// something else, holding max_workers and job_timeout, not
// max_connections; the third-party site holds max_connections and
// max_workers.
const nameRoot = "https://namesake.test/"

func namesakeSite() *fakeFetcher {
	f := originSite()
	f.pages[nameRoot] = fakePage{title: "Namesake", markdown: "# Namesake\n\n## Workers\n\nNamesake sets max_workers and job_timeout for its jobs.\n"}
	f.pages[fanRoot+"lib/pool-tuning"] = fakePage{title: "Pool tuning guide", markdown: "# Pool tuning\n\n## Pool size\n\nTuning max_connections and max_workers in a busy pool.\n"}
	return f
}

func namesakeMatch() *webMatch {
	return &webMatch{root: rootOf(nameRoot), trust: 6, evidence: []evidence{{kind: evPkgDocs, detail: "npm lib lists it as Homepage"}}, stars: -1, packages: []string{"npm:lib"}}
}

// A name guessed from the question, tied to its root by a registry
// package alone, is no reason to read that root ahead of the site the
// search results favour when that site carries the name and the root does
// not: that site is read first and answers with its own origin. The root
// stays a candidate, read when the favoured site does not answer. A root
// that carries the name itself, or one the search agrees on, is read first.
func TestGuessedNameGivesWayToTheFavouredSite(t *testing.T) {
	bare := sfResolved{name: "libtips", match: namesakeMatch(), bare: true}
	o := originSearch(t, namesakeSite(), fanHits(), bare).SearchSelect(context.Background(), "max_connections max_workers", 0)
	if o.Status != Found || o.Library != fanRoot || !OriginUnverified(o.Origin) {
		t.Fatalf("guessed name: %s from %q, origin %q (%v), want found from %s", o.Status, o.Library, o.Origin, o.Err(), fanRoot)
	}

	// The favoured site without the terms: the root is read after it.
	o = originSearch(t, namesakeSite(), fanHits(), bare).SearchSelect(context.Background(), "job_timeout max_workers", 0)
	if o.Status != Found || o.Library != nameRoot || !OriginByName(o.Origin) {
		t.Fatalf("favoured site lacks the terms: %s from %q, origin %q (%v), want found from %s", o.Status, o.Library, o.Origin, o.Err(), nameRoot)
	}

	// Resolve's root verified by the search's agreement is read first.
	agree := append([]search.Result{hit(nameRoot+"guide", "Lib guide"), hit(nameRoot+"pool", "Lib pool"), hit(nameRoot+"tuning", "Lib tuning")}, fanHits()...)
	f := namesakeSite()
	for _, p := range []string{"guide", "pool", "tuning"} {
		f.pages[nameRoot+p] = fakePage{title: "Guide", markdown: "# Guide\n\n## Max connections\n\nSet max_connections and max_workers on the pool.\n"}
	}
	if o = originSearch(t, f, agree, bare).SearchSelect(context.Background(), "max_connections max_workers", 0); o.Library != nameRoot {
		t.Fatalf("agreeing search: %s from %q, origin %q", o.Status, o.Library, o.Origin)
	}

	// The root carries the guessed name and the favoured site does not:
	// the root is read first, and answering, alone.
	f = namesakeSite()
	own := sfResolved{name: "namesake", match: namesakeMatch(), bare: true}
	if o = originSearch(t, f, fanHits(), own).SearchSelect(context.Background(), "job_timeout max_workers", 0); o.Status != Found || o.Library != nameRoot {
		t.Fatalf("root named like the name: %s from %q, origin %q (%v)", o.Status, o.Library, o.Origin, o.Err())
	}
	assertNeverFetched(t, f, "libtips.test")
}

// A resolved root read first whose answer is partial for want of some of
// the question's terms gives way to the site the search results favour
// when that one answers better, the origin naming the missing terms; a tie
// keeps the root.
func TestPartialRootFallsOver(t *testing.T) {
	named := sfResolved{name: "lib", match: namesakeMatch()}
	o := originSearch(t, namesakeSite(), fanHits(), named).GetDocsSelect(context.Background(), "lib", "max_connections max_workers", 0)
	want := "; " + nameRoot + ", which resolve found for lib, lacks the question's term max_connections, so this answer is from the site the search results favour"
	if o.Status != Found || o.Library != fanRoot || !strings.HasSuffix(o.Origin, want) {
		t.Fatalf("partial root: %s from %q, origin %q (%v), want found from %s ending %q", o.Status, o.Library, o.Origin, o.Err(), fanRoot, want)
	}
	o = originSearch(t, namesakeSite(), fanHits(), named).GetDocsSelect(context.Background(), "lib", "max_connections job_timeout", 0)
	if o.Status != Partial || o.Library != nameRoot {
		t.Fatalf("tie: %s from %q (%v), want partial from %s", o.Status, o.Library, o.Err(), nameRoot)
	}
	if !betterRead(Outcome{Status: Partial, NotFound: []string{"a"}}, Outcome{Status: Partial, NotFound: []string{"a", "b"}}) ||
		betterRead(Outcome{Status: Partial, NotFound: []string{"a"}}, Outcome{Status: Partial, NotFound: []string{"b"}}) ||
		!betterRead(Outcome{Status: Partial}, Outcome{Status: Refused}) || betterRead(Outcome{Status: Partial}, Outcome{Status: Found}) {
		t.Error("betterRead order")
	}
}

// The site the search results favour tried and unreadable: the answer kept
// says so, naming the site and the cause, whichever was read first.
func TestUnreadFavouredSiteIsNamed(t *testing.T) {
	shells := func() *fakeFetcher {
		f := namesakeSite()
		for _, p := range []string{fanRoot + "lib/max-conns", fanRoot + "lib/pool-tuning"} {
			f.pages[p] = fakePage{title: "App", markdown: "Loading", shell: true}
		}
		return f
	}
	note := "could not read " + fanRoot + " (2 need JavaScript), the site the search results favour"
	for _, tc := range []struct {
		name string
		call func(*SearchFirst) Outcome
	}{
		{"guessed name", func(s *SearchFirst) Outcome {
			return s.SearchSelect(context.Background(), "max_connections max_workers", 0)
		}},
		{"library name", func(s *SearchFirst) Outcome {
			return s.GetDocsSelect(context.Background(), "lib", "max_connections max_workers", 0)
		}},
	} {
		r := sfResolved{name: "lib", match: namesakeMatch()}
		if tc.name == "guessed name" {
			r = sfResolved{name: "libtips", match: namesakeMatch(), bare: true}
		}
		s := originSearch(t, shells(), fanHits(), r)
		o := tc.call(s)
		if o.Status != Partial || o.Library != nameRoot || !strings.Contains(o.Note, note) {
			t.Errorf("%s: %s from %q, note %q (%v), want partial from %s noting %q", tc.name, o.Status, o.Library, o.Note, o.Err(), nameRoot, note)
		}
		// A repeat within cache_ttl answers from the same read, and says so too.
		s.web.sites = siteSearchFunc(func(context.Context, string, int) ([]search.Result, error) {
			t.Error("a repeat within cache_ttl searched again")
			return nil, nil
		})
		if again := tc.call(s); again.Library != nameRoot || !strings.Contains(again.Note, note) {
			t.Errorf("%s, repeated: %s from %q, note %q", tc.name, again.Status, again.Library, again.Note)
		}
	}
}

// An unverified origin names what was asked only when something says the
// question names it; else it names nothing, not a word of the question
// that looks like a name, nor the whole question.
func TestUnverifiedOriginNamesOnlyANamedThing(t *testing.T) {
	site := func(root string, titles ...string) sfSite {
		s := sfSite{root: rootOf(root)}
		for _, title := range titles {
			s.hits = append(s.hits, sfHit{u: mustURL(t, root+"page"), title: title})
		}
		return s
	}
	for _, tc := range []struct {
		query string
		site  sfSite
		r     sfResolved
		want  string
	}{
		{"opt in to cross-document view transitions on a multi-page site",
			site("https://developer.browser.test/docs/", "Cross-document view transitions for multi-page applications", "Cross-document view transitions are opt-in"),
			sfResolved{name: "opt", bare: true}, "unverified, ketch could not confirm developer.browser.test as the official documentation for this question"},
		{"Deno permission flag to allow network access to one host", site("https://docs.deno.test/", "Security and permissions"), sfResolved{},
			"unverified, ketch could not confirm docs.deno.test as the official documentation for this question"},
		{"S3 multipart upload maximum 10000 parts", site("https://docs.cloud.test/AmazonS3/latest/", "Amazon S3 multipart upload limits"), sfResolved{name: "S3", bare: true},
			"unverified, ketch could not confirm docs.cloud.test as the official documentation of S3"},
		{"container-type values size inline-size normal",
			func() sfSite {
				s := site("https://developer.web.test/en-US/docs/", "Container queries")
				s.alts = []sfSite{site("https://developer.web.test/docs/", "container-type CSS property")}
				return s
			}(), sfResolved{}, "unverified, ketch could not confirm developer.web.test as the official documentation of container-type"},
		{"lib pool max_connections", site(fanRoot, "Pool tuning guide"), sfResolved{name: "lib", match: namesakeMatch(), bare: true},
			"unverified, ketch could not confirm libtips.test as the official documentation of lib"},
	} {
		prefs := sitePrefs{words: questionWords(tc.query), resolved: tc.r}
		if got := siteOrigin(tc.site, sfRequest{query: tc.query}, prefs).text; got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.query, got, tc.want)
		}
	}
}
