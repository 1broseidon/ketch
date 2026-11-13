package docs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/search"
)

// fakePop is an in-memory popularitySource: weekly downloads by spec, or
// an error by spec.
type fakePop struct {
	weekly_ map[string]int64
	errs    map[string]error

	mu    sync.Mutex
	calls []string
}

func (f *fakePop) weekly(_ context.Context, pkg pkgInfo) (int64, error) {
	f.mu.Lock()
	f.calls = append(f.calls, pkg.spec())
	f.mu.Unlock()
	if err := f.errs[pkg.spec()]; err != nil {
		return 0, err
	}
	return f.weekly_[pkg.spec()], nil
}

// keylessWeb is resolverWeb with GitHub configured but switched off, as a
// backend without a token is: repos must never be called.
func keylessWeb(f Fetcher, pkgs *fakePkgs, sites *fakeSites, pop *fakePop) (*Web, *fakeRepos) {
	repos := &fakeRepos{}
	w := resolverWeb(f, pkgs, repos, sites)
	w.githubOn = func() bool { return false }
	if pop != nil {
		w.popularity = pop
	}
	return w, repos
}

// The widget world: npm's widget has its docs on docs.widget.dev, linked
// from its homepage and linking back to its repository; crates' widget is
// a squatter with docs.rs docs and a handful of downloads.
func widgetWorld() (*fakeFetcher, *fakePkgs) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://widget.dev/": html(`<html><head><title>Widget</title></head><body>
<a href="https://docs.widget.dev/">Docs</a><a href="https://github.com/widget-org/widget">GitHub</a></body></html>`),
		"https://docs.widget.dev/": html(`<html><head><title>Widget docs</title><meta name="generator" content="Docusaurus v3"></head><body>
<a href="/intro">Intro</a><a href="https://github.com/widget-org/widget">GitHub</a></body></html>`),
		"https://docs.rs/widget/latest/widget/": html(`<html><head><title>widget - Rust</title><meta name="generator" content="rustdoc"></head><body><a href="all.html">All</a></body></html>`),
	}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:widget":    {eco: ecoNPM, name: "widget", homepage: "https://widget.dev", repoURL: "https://github.com/widget-org/widget", repo: repo("widget-org", "widget")},
		"crates:widget": {eco: ecoCrates, name: "widget", registryDocs: "https://docs.rs/widget/latest/widget/", downloads: 130},
	}}
	return f, pkgs
}

// Without a token GitHub is never asked; the registry popularity of the
// two same-name projects decides, under the dominance rule, and the docs
// root verifies by two independent edges: the homepage's docs link and the
// site's link to the repository npm names (and the shared domain).
func TestKeylessPopularityPicks(t *testing.T) {
	f, pkgs := widgetWorld()
	pop := &fakePop{weekly_: map[string]int64{"npm:widget": 500_000}}
	w, repos := keylessWeb(f, pkgs, nil, pop)
	m, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false)
	if err != nil {
		t.Fatalf("resolveOne(widget) = %v", err)
	}
	if m.root.String() != "https://docs.widget.dev/" {
		t.Errorf("picked %s, want docs.widget.dev", m.root)
	}
	lm := m.libraryMatch()
	for _, want := range []string{"links to github.com/widget-org/widget", "npm widget homepage widget.dev links to it"} {
		if !slices.Contains(lm.Evidence, want) {
			t.Errorf("evidence %v lacks %q", lm.Evidence, want)
		}
	}
	if len(repos.calls) != 0 {
		t.Errorf("GitHub was asked without a token: %v", repos.calls)
	}
	// --resolve lists the squatter too, below.
	matches, err := w.ResolveLibrary(context.Background(), "widget", 5)
	if err != nil || len(matches) != 1 || matches[0].ID != "https://docs.widget.dev/" {
		t.Errorf("ResolveLibrary(widget) = %+v, %v; the squatter is outweighed", matches, err)
	}
}

// A count that could not be read is a keyless source that did not answer:
// it blocks dominance, so the pick is refused rather than guessed.
func TestKeylessUnreadPopularityRefuses(t *testing.T) {
	for _, failing := range []string{"npm:widget", "crates:widget"} {
		f, pkgs := widgetWorld()
		pop := &fakePop{weekly_: map[string]int64{"npm:widget": 500_000}, errs: map[string]error{failing: errors.New("status 503")}}
		w, _ := keylessWeb(f, pkgs, nil, pop)
		_, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false)
		var amb *AmbiguousLibraryError
		var up *upstreamError
		if err == nil || !errors.As(err, &amb) && !errors.As(err, &up) {
			t.Errorf("%s unread: resolveOne = %v, want an ambiguity or upstream error, not a pick", failing, err)
		}
	}
}

// A same-name package far more popular than the pick, whose own docs did
// not verify, makes the pick ambiguous: a squatter's registry docs never
// answer for the library people mean. A negligible one does not.
func TestKeylessUnverifiedRivalRefuses(t *testing.T) {
	f, pkgs := widgetWorld()
	delete(f.raw, "https://docs.widget.dev/") // npm widget verifies nothing now
	pop := &fakePop{weekly_: map[string]int64{"npm:widget": 500_000, "crates:widget": 10}}
	w, _ := keylessWeb(f, pkgs, nil, pop)
	_, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false)
	var amb *AmbiguousLibraryError
	if !errors.As(err, &amb) || len(amb.Matches) != 2 || amb.Matches[1].ID != "npm:widget" {
		t.Fatalf("resolveOne = %v, want ambiguity naming npm:widget", err)
	}
	pop.weekly_["npm:widget"] = 100
	pop.weekly_["crates:widget"] = 1_000_000
	w, _ = keylessWeb(f, pkgs, nil, pop)
	if m, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false); err != nil || m.root.String() != "https://docs.rs/widget/latest/widget/" {
		t.Errorf("negligible rival: resolveOne = %s, %v; want the crate's docs", m.root, err)
	}
}

// One edge is not enough keyless: a docs site its package names, with
// nothing on it pointing back at the package or repository and no second
// host on the owner's domain, is rejected; a link back makes two.
func TestKeylessOneEdgeIsNotEnough(t *testing.T) {
	site := `<html><head><title>Lonely</title><meta name="generator" content="mkdocs-1.6"></head><body><a href="/guide/">Guide</a>%s</body></html>`
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"npm:lonely": {eco: ecoNPM, name: "lonely", homepage: "https://lonely.dev", repoURL: "https://github.com/lone/lonely", repo: repo("lone", "lonely")}}}
	f := &fakeFetcher{raw: map[string]fakeRaw{"https://lonely.dev/": html(strings.Replace(site, "%s", "", 1))}}
	w, _ := keylessWeb(f, pkgs, nil, nil)
	log := &resolveLog{}
	if _, err := w.resolveLogged(context.Background(), mustSpec(t, "npm:lonely"), 0, log); !errors.Is(err, ErrNotFound) {
		t.Fatalf("one edge: resolve = %v, want not found", err)
	}
	assertReasons(t, log, "has 1 of the 2 independent edges")

	f = &fakeFetcher{raw: map[string]fakeRaw{"https://lonely.dev/": html(strings.Replace(site, "%s", `<a href="https://www.npmjs.com/package/lonely">npm</a>`, 1))}}
	w, _ = keylessWeb(f, pkgs, nil, nil)
	matches, err := w.resolve(context.Background(), mustSpec(t, "npm:lonely"), 0)
	if err != nil || len(matches) != 1 || matches[0].root.String() != "https://lonely.dev/" {
		t.Fatalf("with a package link: resolve = %v, %v", matchIDs(matches), err)
	}
	if !slices.Contains(matches[0].libraryMatch().Evidence, "the site links to the npm lonely package page") {
		t.Errorf("evidence = %v", matches[0].libraryMatch().Evidence)
	}

	// The same link in the root's llms.txt counts as well.
	f = &fakeFetcher{raw: map[string]fakeRaw{
		"https://lonely.dev/":         html(strings.Replace(site, "%s", "", 1)),
		"https://lonely.dev/llms.txt": md("# Lonely\n\n> A lonely library.\n\n- [Source](https://github.com/lone/lonely)\n- [Guide](https://lonely.dev/guide/)\n"),
	}}
	w, _ = keylessWeb(f, pkgs, nil, nil)
	if matches, err := w.resolve(context.Background(), mustSpec(t, "npm:lonely"), 0); err != nil || len(matches) != 1 {
		t.Fatalf("with an llms.txt repository link: resolve = %v, %v", matchIDs(matches), err)
	} else if !slices.Contains(matches[0].libraryMatch().Evidence, "its llms.txt links to github.com/lone/lonely") {
		t.Errorf("evidence = %v", matches[0].libraryMatch().Evidence)
	}
}

// Search hits are candidates beside the registries from the start, and
// never evidence: a docs framework site on the package's own domain that
// links back verifies (two edges); a blog that links back is not
// documentation; a mirror that links back has one edge.
func TestKeylessSearchHits(t *testing.T) {
	docusaurus := func(title, extra string) fakeRaw {
		return html(`<html><head><title>` + title + `</title><meta name="generator" content="Docusaurus v3"></head><body><a href="intro">Intro</a>` + extra + `</body></html>`)
	}
	f := &fakeFetcher{raw: map[string]fakeRaw{
		// The homepage is a JS shell: no docs link to follow.
		"https://gizmo.example.org/":        html(`<html><head><title>Gizmo</title></head><body><div id="root"></div></body></html>`),
		"https://gizmo.example.org/docs/":   docusaurus("Gizmo docs", `<a href="https://github.com/gizmo-org/gizmo">GitHub</a>`),
		"https://blog.example.org/":         html(`<html><head><title>Blog</title></head><body><a href="/a">A</a><a href="https://github.com/gizmo-org/gizmo">gizmo</a></body></html>`),
		"https://gizmo-mirror.example.net/": docusaurus("Gizmo docs", `<a href="https://github.com/gizmo-org/gizmo">GitHub</a>`),
	}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"npm:gizmo": {eco: ecoNPM, name: "gizmo", homepage: "https://gizmo.example.org", repoURL: "https://github.com/gizmo-org/gizmo", repo: repo("gizmo-org", "gizmo")}}}
	sites := &fakeSites{hits: []search.Result{
		{URL: "https://blog.example.org/posts/gizmo-tips"},
		{URL: "https://gizmo-mirror.example.net/"},
		{URL: "https://gizmo.example.org/docs/getting-started"},
	}}
	w, _ := keylessWeb(f, pkgs, sites, nil)
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "gizmo"), 0, log)
	if err != nil || len(matches) != 1 || matches[0].root.String() != "https://gizmo.example.org/docs/" {
		t.Fatalf("resolve gizmo = %v, %v (rejected %v)", matchIDs(matches), err, log.rejected)
	}
	assertReasons(t, log, "search hit https://blog.example.org/ does not identify itself as documentation",
		"https://gizmo-mirror.example.net/ has 1 of the 2 independent edges")
	if len(sites.queries) != 1 || sites.queries[0] != "gizmo documentation" {
		t.Errorf("search queries = %q", sites.queries)
	}
	// A spec's ecosystem steers the search.
	sites.queries = nil
	if _, err := w.resolve(context.Background(), mustSpec(t, "npm:gizmo"), 0); err != nil {
		t.Fatal(err)
	}
	if len(sites.queries) != 1 || sites.queries[0] != "gizmo documentation npm" {
		t.Errorf("search queries = %q", sites.queries)
	}
}

// What a hit names becomes a project of its own: docs.sprocketjs.dev links
// to npm's sprocket-js, whose metadata names sprocketjs.dev and the
// repository the site links to. npm's sprocket is an unrelated package
// that verifies nothing.
func TestKeylessProjectFromHitLinks(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://docs.sprocketjs.dev/": html(`<html><head><title>Sprocket</title><meta name="generator" content="VitePress"></head><body>
<a href="/guide">Guide</a><a href="https://github.com/sprocketjs/sprocket">GitHub</a><a href="https://www.npmjs.com/package/sprocket-js">npm</a></body></html>`),
	}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:sprocket":    {eco: ecoNPM, name: "sprocket", homepage: "https://github.com/someone/sprocket", repoURL: "https://github.com/someone/sprocket", repo: repo("someone", "sprocket")},
		"npm:sprocket-js": {eco: ecoNPM, name: "sprocket-js", homepage: "https://sprocketjs.dev", repoURL: "https://github.com/sprocketjs/sprocket", repo: repo("sprocketjs", "sprocket")},
	}}
	sites := &fakeSites{hits: []search.Result{{URL: "https://docs.sprocketjs.dev/guide/start"}}}
	w, _ := keylessWeb(f, pkgs, sites, nil)
	m, err := w.resolveOne(context.Background(), mustSpec(t, "sprocket"), false)
	if err != nil || m.root.String() != "https://docs.sprocketjs.dev/" || !slices.Contains(m.packages, "npm:sprocket-js") {
		t.Fatalf("resolveOne(sprocket) = %s %v, %v", m.root, m.packages, err)
	}
}

// An owner/repo spec without GitHub takes the packages named like the
// repository that name it back, in every registry: here crates' axle.
func TestKeylessRepoSpecPackages(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://docs.rs/axle/latest/axle/": html(`<html><head><title>axle - Rust</title><meta name="generator" content="rustdoc"></head><body><a href="all.html">All</a></body></html>`),
	}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"crates:axle": {eco: ecoCrates, name: "axle", registryDocs: "https://docs.rs/axle/latest/axle/", repoURL: "https://github.com/axle-rs/axle", repo: repo("axle-rs", "axle")},
		"npm:axle":    {eco: ecoNPM, name: "axle", repoURL: "https://github.com/other/axle", repo: repo("other", "axle")},
	}}
	w, _ := keylessWeb(f, pkgs, nil, nil)
	m, err := w.resolveOne(context.Background(), mustSpec(t, "axle-rs/axle"), false)
	if err != nil || m.root.String() != "https://docs.rs/axle/latest/axle/" {
		t.Fatalf("resolveOne(axle-rs/axle) = %s, %v", m.root, err)
	}
}

// memoCache is an in-memory ResolutionCache over a fakeFetcher.
type memoCache struct {
	*fakeFetcher
	mu sync.Mutex
	m  map[string]string
}

func (c *memoCache) GetResolution(k string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *memoCache) PutResolution(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
}

// A verified pick is cached: a repeat resolves without a fetch or a
// registry call, and a different version or hint is a different entry.
func TestResolutionCache(t *testing.T) {
	f, pkgs := widgetWorld()
	c := &memoCache{fakeFetcher: f, m: map[string]string{}}
	w, _ := keylessWeb(c, pkgs, nil, &fakePop{weekly_: map[string]int64{"npm:widget": 500_000}})
	first, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.m) != 1 {
		t.Fatalf("cache entries = %v", c.m)
	}
	before := len(f.called())
	second, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false)
	if err != nil || second.root.String() != first.root.String() || len(f.called()) != before {
		t.Errorf("repeat = %s, %v with %d new fetches; want %s from the cache", second.root, err, len(f.called())-before, first.root)
	}
	// A hint is part of the key. (Here it does not change the pick: npm's
	// widget outweighs the crate by far more than the dominance margin.)
	w.workdirHint = func() []ecosystem { return []ecosystem{ecoCrates} }
	if m, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false); err != nil || m.root.String() != "https://docs.widget.dev/" || len(c.m) != 2 {
		t.Errorf("crates hint = %s, %v with %d entries; want docs.widget.dev resolved afresh", m.root, err, len(c.m))
	}
}

func TestRegistryPackageOf(t *testing.T) {
	cases := map[string]pkgRef{
		"https://www.npmjs.com/package/solid-js":           {ecoNPM, "solid-js"},
		"https://www.npmjs.com/package/@babel/core":        {ecoNPM, "@babel/core"},
		"https://pypi.org/project/Flask/":                  {ecoPyPI, "flask"},
		"https://crates.io/crates/serde_json":              {ecoCrates, "serde-json"},
		"https://docs.rs/tokio/latest/tokio/":              {ecoCrates, "tokio"},
		"https://pkg.go.dev/github.com/gin-gonic/gin@v1.9": {ecoGo, "github.com/gin-gonic/gin"},
		"https://github.com/pallets/click":                 {},
		"https://www.npmjs.com/~someone":                   {},
	}
	for raw, want := range cases {
		u, _ := url.Parse(raw)
		if got := registryPackageOf(u); got != want {
			t.Errorf("registryPackageOf(%s) = %+v, want %+v", raw, got, want)
		}
	}
}

// The popularity client reads each registry's keyless endpoint and puts
// the counts on one weekly scale; a package an endpoint has no row for
// counts zero.
func TestPopularityClient(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle("GET", "https://api.npmjs.org/downloads/point/last-week/react", testutil.Fixture{Body: `{"downloads":1234567,"package":"react"}`})
	ft.Handle("GET", "https://api.npmjs.org/downloads/point/last-week/@babel/core", testutil.Fixture{Body: `{"downloads":42,"package":"@babel/core"}`})
	ft.Handle("GET", "https://pypistats.org/api/packages/click/recent", testutil.Fixture{Body: `{"data":{"last_day":1,"last_month":4,"last_week":250000},"package":"click"}`})
	ft.Handle("GET", "https://pypistats.org/api/packages/nobody/recent", testutil.Fixture{Status: http.StatusNotFound, Body: `{}`})
	ft.Handle("GET", "https://pkg.go.dev/github.com/gin-gonic/gin", testutil.Fixture{Header: http.Header{"Content-Type": {"text/html"}}, Body: `<html><body><span class="go-Main-headerDetailItem" data-test-id="UnitHeader-importedby">
    <a href="/github.com/gin-gonic/gin?tab=importedby" aria-label="Imported By: 80"><span class="go-textSubtle">Imported by: </span>80</a></span></body></html>`})
	ft.Handle("GET", "https://pypistats.org/api/packages/flaky/recent", testutil.Fixture{Status: http.StatusServiceUnavailable})
	reg := newPkgClient(ft.Client())
	reg.retryWait = time.Millisecond
	c := newPopularityClient(reg)
	cases := []struct {
		pkg  pkgInfo
		want int64
	}{
		{pkgInfo{eco: ecoNPM, name: "react"}, 1234567},
		{pkgInfo{eco: ecoNPM, name: "@babel/core"}, 42},
		{pkgInfo{eco: ecoPyPI, name: "Click"}, 250000},
		{pkgInfo{eco: ecoPyPI, name: "nobody"}, 0},
		{pkgInfo{eco: ecoCrates, name: "serde", downloads: 1300}, 100},
		{pkgInfo{eco: ecoGo, name: "github.com/gin-gonic/gin"}, 80 * goImporterWeight},
	}
	for _, tc := range cases {
		if got, err := c.weekly(context.Background(), tc.pkg); err != nil || got != tc.want {
			t.Errorf("weekly(%s) = %d, %v; want %d", tc.pkg.spec(), got, err, tc.want)
		}
	}
	if _, err := c.weekly(context.Background(), pkgInfo{eco: ecoPyPI, name: "flaky"}); err == nil {
		t.Error("a 503 twice is an error, not zero downloads")
	}
}

// The recorded world without GitHub: the registries' metadata and the
// sites' own links verify the same docs the token run finds, and the
// squatters beside them lose on popularity.
func TestKeylessFixtureWorld(t *testing.T) {
	cases := []struct{ name, root string }{
		{"click", "https://click.palletsprojects.com/"},
		{"next", "https://nextjs.org/docs/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := fixtureWeb(t)
			w.githubOn = func() bool { return false }
			w.popularity = &fakePop{weekly_: map[string]int64{"pypi:click": 25_000_000, "npm:next": 30_000_000}}
			log := &resolveLog{}
			matches, err := w.resolveLogged(context.Background(), mustSpec(t, tc.name), 0, log)
			if err != nil || len(matches) == 0 || !strings.HasPrefix(matches[0].root.String(), tc.root) {
				t.Fatalf("resolve %s = %v, %v; want %s (rejected %v)", tc.name, matchIDs(matches), err, tc.root, log.rejected)
			}
			m, err := w.resolveOne(context.Background(), mustSpec(t, tc.name), false)
			if err != nil || !strings.HasPrefix(m.root.String(), tc.root) {
				t.Errorf("resolveOne(%s) = %s, %v", tc.name, m.root, err)
			}
		})
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// pypistats answers bursts with 429: a read waits and tries again, and
// each package is read once however many readings ask.
func TestPopularityPyPIRetries(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		status, body := http.StatusTooManyRequests, ""
		if n > 2 {
			status, body = http.StatusOK, `{"data":{"last_week":77}}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	c := newPopularityClient(&pkgClient{client: client, ua: webUserAgent, timeout: apiCallTimeout, retryWait: time.Millisecond})
	for range 2 {
		if n, err := c.weekly(context.Background(), pkgInfo{eco: ecoPyPI, name: "busy"}); err != nil || n != 77 {
			t.Fatalf("weekly = %d, %v; want 77 after two 429s", n, err)
		}
	}
	if calls != 3 {
		t.Errorf("%d requests, want 3 (two 429s, one answer, then the memo)", calls)
	}
}

// fakeGoPkgs adds pkg.go.dev's search to fakePkgs.
type fakeGoPkgs struct {
	*fakePkgs
	mods []goModule
	err  error
}

func (f *fakeGoPkgs) searchGoPage(context.Context, string, int) ([]goModule, error) {
	return f.mods, f.err
}

// Keyless, a bare name is a Go module's too: pkg.go.dev's search names
// spf13/cobra, whose importers outweigh PyPI's cobra (COBRApy). The search
// failing is a registry that could not be asked: no pick past it.
func TestKeylessGoByName(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://pkg.go.dev/github.com/spf13/cobra/": html(`<html><head><title>cobra package - github.com/spf13/cobra - Go Packages</title></head><body><a href="https://github.com/spf13/cobra">Repository</a><a href="#pkg-index">Index</a></body></html>`),
		"https://cobrapy.example.org/":               html(`<html><head><title>cobra</title><meta name="generator" content="Sphinx 7"></head><body><a href="io.html">IO</a><a href="https://github.com/opencobra/cobrapy">GitHub</a></body></html>`),
	}}
	pkgs := &fakeGoPkgs{fakePkgs: &fakePkgs{pkgs: map[string]pkgInfo{
		"go:github.com/few/cobra":   {eco: ecoGo, name: "github.com/few/cobra", registryDocs: "https://pkg.go.dev/github.com/few/cobra"},
		"pypi:cobra":                {eco: ecoPyPI, name: "cobra", docsURL: "https://cobrapy.example.org/", repoURL: "https://github.com/opencobra/cobrapy", repo: repo("opencobra", "cobrapy")},
		"go:github.com/spf13/cobra": {eco: ecoGo, name: "github.com/spf13/cobra", registryDocs: "https://pkg.go.dev/github.com/spf13/cobra", repoURL: "https://github.com/spf13/cobra", repo: repo("spf13", "cobra")},
		"go:github.com/tj/cobra":    {eco: ecoGo, name: "github.com/tj/cobra"},
	}}, mods: []goModule{{"github.com/spf13/cobra", 99_000}, {"github.com/few/cobra", 120}, {"github.com/muesli/mango-cobra", 169}, {"github.com/tj/cobra", 0}}}
	w, _ := keylessWeb(f, nil, nil, &fakePop{weekly_: map[string]int64{"pypi:cobra": 19_000}})
	w.pkgs = pkgs
	m, err := w.resolveOne(context.Background(), mustSpec(t, "cobra"), false)
	if err != nil || !slices.Contains(m.packages, "go:github.com/spf13/cobra") {
		t.Fatalf("resolveOne(cobra) = %s %v, %v; want spf13/cobra's docs", m.root, m.packages, err)
	}
	pkgs.err = errors.New("status 429")
	var up *upstreamError
	if _, err := w.resolveOne(context.Background(), mustSpec(t, "cobra"), false); !errors.As(err, &up) {
		t.Errorf("pkg.go.dev down: resolveOne = %v, want an upstream error, not COBRApy", err)
	}
}

// TestMain runs the package's tests without crates.io's request spacing:
// they read recorded fixtures, not crates.io (TestTurnsSpacing checks it).
func TestMain(m *testing.M) {
	cratesGap = 0
	// No test reads the user's local docs index: the auto chain asks it
	// first. Tests of the index set their own.
	dir, err := os.MkdirTemp("", "ketch-docs-tests-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("KETCH_DOCS_INDEX_PATH", filepath.Join(dir, "docs-index.db"))
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// turns spaces the starts of requests by its gap and lets a cancelled
// caller go.
func TestTurnsSpacing(t *testing.T) {
	tn := newTurns(30 * time.Millisecond)
	start := time.Now()
	for range 3 {
		release, err := tn.take(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	if el := time.Since(start); el < 60*time.Millisecond {
		t.Errorf("three turns took %v, want at least two gaps", el)
	}
	release, _ := tn.take(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tn.take(ctx); err == nil {
		t.Error("a cancelled caller got a turn")
	}
	release()
	if speculative(context.Background()) || !speculative(withSpeculative(context.Background())) {
		t.Error("speculative marks only what withSpeculative marked")
	}
}

// recordedWorld is a keyless backend over one case of a recorded live
// world (testdata/keyless/<case>: fixtures.json, the registry, search and
// site responses as they were served, trimmed; search-hits.txt, the web
// search's results in order), with a Go working directory as a benchmark
// harness inside a Go module has.
func recordedWorld(t *testing.T, name string) *Web {
	t.Helper()
	dir := filepath.Join("testdata", "keyless", name)
	raw, err := os.ReadFile(filepath.Join(dir, "fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		URL     string            `json:"url"`
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	ft := testutil.NewFixtureTransport()
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(dir, "bodies", e.Body))
		if err != nil {
			t.Fatal(err)
		}
		h := http.Header{}
		for k, v := range e.Headers {
			h.Set(k, v)
		}
		ft.Handle(http.MethodGet, e.URL, testutil.Fixture{Status: e.Status, Header: h, Body: string(body)})
	}
	hits, err := os.ReadFile(filepath.Join(dir, "search-hits.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var results []search.Result
	for _, u := range strings.Fields(string(hits)) {
		results = append(results, search.Result{URL: u})
	}
	client := ft.Client()
	pkgs := newPkgClient(client)
	pkgs.retryWait = time.Millisecond
	w := NewWeb(&fixtureFetcher{client: client})
	w.pkgs = pkgs
	w.popularity = newPopularityClient(pkgs)
	w.sites = &fakeSites{hits: results}
	w.githubOn = func() bool { return false }
	w.workdirHint = func() []ecosystem { return []ecosystem{ecoGo} }
	return w
}

// Regressions from a recorded keyless world, run from a Go working
// directory: the ecosystem hint is a tie-break, so a Go module named httpx
// with nine importers never stands in for PyPI's httpx; and a module a hit
// named go-django, whose pkg.go.dev page answered 429, is no unanswered
// rival to Django.
func TestKeylessRecordedRegressions(t *testing.T) {
	cases := []struct{ world, spec, root string }{
		{"httpx-timeouts", "httpx", "https://www.python-httpx.org/"},
		{"django-related-compare", "django", "https://docs.djangoproject.com/"},
	}
	for _, tc := range cases {
		t.Run(tc.world, func(t *testing.T) {
			w := recordedWorld(t, tc.world)
			m, err := w.resolveOne(context.Background(), mustSpec(t, tc.spec), false)
			if err != nil || !strings.HasPrefix(m.root.String(), tc.root) {
				t.Fatalf("resolveOne(%s) = %s, %v; want %s", tc.spec, m.root, err, tc.root)
			}
		})
	}
}

// downFetcher fails every fetch of one host as a 403 would.
type downFetcher struct {
	*fakeFetcher
	host string
}

func (f *downFetcher) FetchRaw(ctx context.Context, u string) (RawContent, error) {
	if strings.Contains(u, f.host) {
		return RawContent{}, errors.New("HTTP 403 for " + u)
	}
	return f.fakeFetcher.FetchRaw(ctx, u)
}

func (f *downFetcher) FetchPage(ctx context.Context, u string) (PageContent, error) {
	if strings.Contains(u, f.host) {
		return PageContent{}, errors.New("HTTP 403 for " + u)
	}
	return f.fakeFetcher.FetchPage(ctx, u)
}

// A search hit that does not answer is the search's candidate failing,
// never a package's site: it neither blocks the pick nor makes the
// resolution an upstream error (the recorded datacamp.com and labex.io
// 403s did both).
func TestKeylessHitFailureIsNoSourceFailure(t *testing.T) {
	f, pkgs := widgetWorld()
	sites := &fakeSites{hits: []search.Result{{URL: "https://www.tutorials.example/widget-guide"}}}
	w, _ := keylessWeb(&downFetcher{fakeFetcher: f, host: "tutorials.example"}, pkgs, sites, &fakePop{weekly_: map[string]int64{"npm:widget": 500_000}})
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "widget"), 0, log)
	if err != nil || len(matches) == 0 {
		t.Fatalf("resolve = %v, %v", matchIDs(matches), err)
	}
	if _, failed := log.snapshot(); len(failed) != 0 {
		t.Errorf("a hit's 403 counted as a failed source: %v", failed)
	}
	assertReasons(t, log, "search hit https://www.tutorials.example/ did not answer")
	if m, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false); err != nil || m.root.String() != "https://docs.widget.dev/" {
		t.Errorf("resolveOne = %s, %v", m.root, err)
	}
}

// The ecosystem hint breaks ties between comparable projects; one outside
// it that dominates (npm's widget beside a crate with a handful of
// downloads) is not set aside for it.
func TestKeylessHintDoesNotOverruleDominance(t *testing.T) {
	f, pkgs := widgetWorld()
	w, _ := keylessWeb(f, pkgs, nil, &fakePop{weekly_: map[string]int64{"npm:widget": 500_000}})
	w.workdirHint = func() []ecosystem { return []ecosystem{ecoCrates} }
	log := &resolveLog{}
	if _, err := w.resolveLogged(context.Background(), mustSpec(t, "widget"), 0, log); err != nil {
		t.Fatal(err)
	}
	m, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false)
	if err != nil || m.root.String() != "https://docs.widget.dev/" {
		t.Fatalf("crates hint beside a dominant npm widget: resolveOne = %s, %v", m.root, err)
	}
}
