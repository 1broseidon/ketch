package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/search"
)

// fixtureFetcher is a Fetcher over an http.Client (the fixture transport),
// following redirects as the scrape pipeline does. FetchPage stands in for
// the pipeline: a page in rendered is what a browser would make of a JS
// shell; any other HTML becomes its title and links as Markdown.
type fixtureFetcher struct {
	client   *http.Client
	rendered map[string]PageContent // by the URL the page is served from
}

func (f *fixtureFetcher) FetchRaw(ctx context.Context, u string) (RawContent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return RawContent{}, err
	}
	client := *f.client
	if client.CheckRedirect == nil {
		client.CheckRedirect = httpx.CheckRedirect // as the scrape pipeline's client does
	}
	resp, err := client.Do(req)
	if err != nil {
		return RawContent{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return RawContent{}, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return RawContent{}, fmt.Errorf("fetching %s: status %d: %w", u, resp.StatusCode, ErrNotFound)
	case resp.StatusCode >= 300:
		return RawContent{}, fmt.Errorf("fetching %s: status %d", u, resp.StatusCode)
	}
	return RawContent{Body: body, ContentType: resp.Header.Get("Content-Type"), URL: resp.Request.URL.String()}, nil
}

func (f *fixtureFetcher) FetchPage(ctx context.Context, u string) (PageContent, error) {
	raw, err := f.FetchRaw(ctx, u)
	if err != nil {
		return PageContent{}, err
	}
	if p, ok := f.rendered[raw.URL]; ok {
		p.URL = raw.URL
		return p, nil
	}
	if !looksLikeHTML(raw.Body, raw.ContentType) {
		return PageContent{Markdown: string(raw.Body), URL: raw.URL}, nil
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw.Body))
	if err != nil {
		return PageContent{}, err
	}
	var md strings.Builder
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		fmt.Fprintf(&md, "[%s](%s)\n", collapseSpace(a.Text()), a.AttrOr("href", ""))
	})
	return PageContent{Title: doc.Find("title").Text(), Markdown: md.String(), URL: raw.URL}, nil
}

// renderedSites are the JS shells' rendered pages.
var renderedSites = map[string]PageContent{
	"https://www.solidjs.com/": {Title: "SolidJS", Markdown: `[Solid](/)

- [Get Started](/guides/getting-started)
- [Docs](https://docs.solidjs.com)
- [Tutorial](/tutorial/introduction_basics)
- [Examples](/examples)
- [Playground](https://playground.solidjs.com)
- [Ecosystem](/ecosystem)
- [GitHub](https://github.com/solidjs/solid)

# Simple and performant reactivity for building user interfaces.
`},
}

// fixtureWeb is a resolving backend over the recorded registries, GitHub
// and sites in testdata/resolve, with no web search.
func fixtureWeb(t *testing.T) (*Web, *testutil.FixtureTransport) {
	t.Helper()
	ft := testutil.NewFixtureTransport()
	for _, dir := range []string{"registry", "github", "sites"} {
		if err := ft.LoadDir(filepath.Join("testdata", "resolve", dir)); err != nil {
			t.Fatal(err)
		}
	}
	w := NewWeb(&fixtureFetcher{client: ft.Client(), rendered: renderedSites})
	w.pkgs = newPkgClient(ft.Client())
	w.repos = newGHClient(ft.Client(), nil)
	return w, ft
}

// notFoundFallback answers what no fixture covers the way the services
// answer for a name they do not have: a GitHub repository search with no
// hits, anything else 404. Nothing reaches the network.
type notFoundFallback struct{ ft *testutil.FixtureTransport }

func (f notFoundFallback) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := f.ft.RoundTrip(req)
	if err == nil || !strings.HasPrefix(err.Error(), "testutil: no fixture") {
		return resp, err
	}
	status, body := http.StatusNotFound, ""
	if req.URL.Host+req.URL.Path == "api.github.com/search/repositories" {
		status, body = http.StatusOK, `{"total_count":0,"incomplete_results":false,"items":[]}`
	}
	return &http.Response{
		Status: http.StatusText(status), StatusCode: status, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)), Request: req,
	}, nil
}

// mustSpec parses a spec in a test.
func mustSpec(t *testing.T, s string) libSpec {
	t.Helper()
	spec, err := parseLibSpec(s)
	if err != nil {
		t.Fatalf("parseLibSpec(%q): %v", s, err)
	}
	return spec
}

// resolveFixture resolves name over the fixtures and returns the matches
// and the log.
func resolveFixture(t *testing.T, name string) ([]webMatch, *resolveLog) {
	t.Helper()
	w, _ := fixtureWeb(t)
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, name), 0, log)
	if err != nil {
		t.Fatalf("resolve %s: %v (rejected: %v; failed: %v)", name, err, log.rejected, log.failed)
	}
	return matches, log
}

// assertReasons fails unless every want fragment is in one of the
// rejection reasons.
func assertReasons(t *testing.T, log *resolveLog, want ...string) {
	t.Helper()
	for _, frag := range want {
		if !slices.ContainsFunc(log.rejected, func(r string) bool { return strings.Contains(r, frag) }) {
			t.Errorf("no rejection reason mentions %q; reasons:\n  %s", frag, strings.Join(log.rejected, "\n  "))
		}
	}
}

// click: npm's click names a deleted repository and no homepage, crates'
// click is a Kubernetes CLI whose homepage is a GitHub wiki and which
// docs.rs has no library docs for; PyPI's click is the one.
func TestResolveClick(t *testing.T) {
	matches, log := resolveFixture(t, "click")
	if len(matches) != 1 {
		t.Fatalf("%d matches, want 1: %+v", len(matches), matchIDs(matches))
	}
	m := matches[0]
	if !strings.HasPrefix(m.root.String(), "https://click.palletsprojects.com/") || m.trust != 10 {
		t.Errorf("match = %s trust %.1f, want click.palletsprojects.com at 10", m.root, m.trust)
	}
	lm := m.libraryMatch()
	for _, want := range []string{"pypi click lists it as Documentation", "github pallets/click homepage", "links to github.com/pallets/click"} {
		if !slices.Contains(lm.Evidence, want) {
			t.Errorf("evidence %v lacks %q", lm.Evidence, want)
		}
	}
	if !slices.Contains(lm.Packages, "pypi:click") || !slices.Contains(lm.Packages, "github:pallets/click") {
		t.Errorf("packages = %v", lm.Packages)
	}
	assertReasons(t, log, "npm click: repo uggedal/click not found", "crates click: homepage https://github.com/databricks/click/wiki is a GitHub page",
		"crates click: https://docs.rs/click/latest/click/ redirects to https://docs.rs/crate/click/latest")
}

// next: PyPI's next points at an unrelated GitHub repository, crates'
// next is a zero-star crate whose docs.rs docs verify but which Next.js
// outweighs; npm's next leads from nextjs.org to its /docs.
func TestResolveNext(t *testing.T) {
	matches, log := resolveFixture(t, "next")
	if len(matches) != 1 {
		t.Fatalf("%d matches, want 1: %v", len(matches), matchIDs(matches))
	}
	if m := matches[0]; m.root.String() != "https://nextjs.org/docs/" || m.title != "Next.js Documentation" {
		t.Errorf("match = %s %q, want nextjs.org/docs/ titled from its llms.txt", m.root, m.title)
	}
	assertReasons(t, log, "pypi next: homepage https://github.com/dheerajmpai/saenews/ is a GitHub page", "github:seldom-se/next: outweighed by github:vercel/next.js")
}

// solid: npm's, PyPI's and crates' solid are small unrelated projects with
// GitHub homepages (and no docs.rs docs); GitHub search finds
// solidjs/solid, whose JS-rendered homepage leads to docs.solidjs.com.
// solid/solid, also named solid, is archived.
func TestResolveSolid(t *testing.T) {
	matches, log := resolveFixture(t, "solid")
	if len(matches) != 1 {
		t.Fatalf("%d matches, want 1: %v", len(matches), matchIDs(matches))
	}
	if m := matches[0]; m.root.String() != "https://docs.solidjs.com/" || m.project != "github:solidjs/solid" {
		t.Errorf("match = %s (%s), want docs.solidjs.com for solidjs/solid", m.root, m.project)
	}
	assertReasons(t, log, "npm solid: homepage https://github.com/sarenji/solid is a GitHub page",
		"pypi solid: homepage http://github.com/haaksmash/solid is a GitHub page",
		"crates solid: https://docs.rs/solid/latest/solid/ redirects to", "github solid/solid: archived")
}

// solid without a browser: solidjs.com is a JS shell over plain HTTP (no
// docs link, no link to the repository), so nothing names docs.solidjs.com.
// Guessing the docs. host of the homepage's domain finds it, verified by
// V4: the domain of solidjs/solid's homepage, and a link back to the
// repository.
func TestResolveSolidWithoutBrowser(t *testing.T) {
	w, ft := fixtureWeb(t)
	w.fetcher = &fixtureFetcher{client: ft.Client()} // nothing renders
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "solid"), 0, log)
	if err != nil || len(matches) != 1 {
		t.Fatalf("resolve solid = %v, %v; want 1 match (rejected: %v; failed: %v)", matchIDs(matches), err, log.rejected, log.failed)
	}
	m := matches[0]
	if m.root.String() != "https://docs.solidjs.com/" || m.project != "github:solidjs/solid" || m.trust < 4 || m.trust > 6 {
		t.Errorf("match = %s (%s, trust %.1f), want docs.solidjs.com for solidjs/solid at trust 4-6", m.root, m.project, m.trust)
	}
	lm := m.libraryMatch()
	for _, want := range []string{"on the domain of github solidjs/solid homepage solidjs.com", "links to github.com/solidjs/solid"} {
		if !slices.Contains(lm.Evidence, want) {
			t.Errorf("evidence %v lacks %q", lm.Evidence, want)
		}
	}
	assertReasons(t, log, "github solidjs/solid: homepage https://solidjs.com/ has no docs link")
	for _, c := range ft.Calls() {
		if strings.Contains(c.String(), "solidjs.com/docs") {
			t.Errorf("the homepage's /docs/ was tried: %s", c)
		}
	}
}

// The guess is the docs. host of the homepage's registrable domain, none
// on a hosting suffix; one that does not answer is a rejection, never a
// failed source.
func TestGuessedDocsRoot(t *testing.T) {
	cases := []struct{ home, want string }{
		{"https://www.solidjs.com/", "https://docs.solidjs.com/"},
		{"http://solidjs.com/about/", "https://docs.solidjs.com/"},
		{"https://widget.co.uk/", "https://docs.widget.co.uk/"},
		{"https://docs.widget.dev/", ""},
		{"https://owner.github.io/project/", ""},
		{"https://app.vercel.app/", ""},
		{"https://localhost/", ""},
	}
	for _, tc := range cases {
		u, _ := url.Parse(tc.home)
		got := ""
		if g, ok := guessedDocsRoot(u); ok {
			got = g.String()
		}
		if got != tc.want {
			t.Errorf("guessedDocsRoot(%s) = %q, want %q", tc.home, got, tc.want)
		}
	}

	w := resolverWeb(&fakeFetcher{fail: errors.New("connection reset by peer")}, nil, nil, nil)
	docs, _ := url.Parse("https://docs.solidjs.com/")
	p := &project{key: "github:solidjs/solid", repo: repo("solidjs", "solid")}
	var rej *rejectError
	if _, err := w.verifyRoot(context.Background(), p, rootCandidate{url: docs, guess: true}); !errors.As(err, &rej) {
		t.Errorf("an unreachable guess = %v, want a rejection", err)
	}
	if _, err := w.verifyRoot(context.Background(), p, rootCandidate{url: docs}); errors.As(err, &rej) {
		t.Errorf("an unreachable listed root = %v, want a failure", err)
	}
}

// Registry squatters, verified only by docs a package index hosts, do not
// make a popular name ambiguous (dominant): crates' fastapi names a
// repository GitHub no longer has (0 stars), crates' requests is on GitLab
// (stars unknown) with docs.rs docs alone, and PyPI's Clap names no
// repository (stars unknown) and has docs only on pythonhosted.org. Each
// bare query from the live run, and a named GetDocs, picks the real
// project.
func TestResolveSquattersDoNotBlockDominance(t *testing.T) {
	cases := []struct {
		name, query, root, outweighed string
	}{
		{"fastapi", "fastapi dependency injection", "https://fastapi.tiangolo.com/", "crates:fastapi: outweighed by github:fastapi/fastapi"},
		{"requests", "how do I set retries in requests", "https://requests.readthedocs.io/en/latest/", "crates:requests: outweighed by github:psf/requests"},
		{"clap", "clap derive subcommands", "https://docs.rs/clap/latest/clap/", "pypi:Clap: outweighed by github:clap-rs/clap"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches, log := resolveFixture(t, tc.name)
			if len(matches) == 0 || matches[0].root.String() != tc.root {
				t.Fatalf("resolve %s = %v, want %s first", tc.name, matchIDs(matches), tc.root)
			}
			assertReasons(t, log, tc.outweighed)

			// The query's other readings (fastapi-dependency, clap-derive)
			// are names no registry has: answered as such, not by a missing
			// fixture, which would read as a source down.
			w, ft := fixtureWeb(t)
			nothingElse := notFoundFallback{ft}
			w.pkgs = newPkgClient(&http.Client{Transport: nothingElse})
			w.repos = newGHClient(&http.Client{Transport: nothingElse}, nil)
			m, split, err := w.resolveQuery(context.Background(), tc.query, splitLibraryQuery(tc.query))
			if err != nil || m.root.String() != tc.root || split.spec.name != tc.name {
				t.Fatalf("resolveQuery(%q) = %s via %q, %v; want %s", tc.query, m.libraryMatch().ID, split.spec.raw, err, tc.root)
			}
			if _, err := w.resolveOne(context.Background(), mustSpec(t, tc.name), false); err != nil {
				t.Errorf("resolveOne(%s) (a named GetDocs) = %v, want no ambiguity", tc.name, err)
			}
		})
	}
}

func matchIDs(matches []webMatch) []string {
	var out []string
	for _, m := range matches {
		out = append(out, fmt.Sprintf("%s (%s, trust %.1f)", m.root, m.project, m.trust))
	}
	return out
}

// Plan §5's expected TrustScores, pinned on the recorded sites. next and
// astro score 5 rather than the plan's 7 and 8: their package and their
// repository name one homepage, whose docs link is one nav chain, not two;
// and docs.astro.build titles itself "Docs" (og:site_name too), so nothing
// on it names astro.
func TestTrustScoresFromFixtures(t *testing.T) {
	cases := []struct {
		name, root string
		trust      float64
	}{
		{"click", "https://click.palletsprojects.com/", 10},
		{"next", "https://nextjs.org/docs/", 5},
		{"astro", "https://docs.astro.build/", 5},
		{"solid", "https://docs.solidjs.com/", 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches, _ := resolveFixture(t, tc.name)
			if len(matches) == 0 || !strings.HasPrefix(matches[0].root.String(), tc.root) || matches[0].trust != tc.trust {
				t.Fatalf("%s = %v, want %s at trust %.1f", tc.name, matchIDs(matches), tc.root, tc.trust)
			}
			t.Logf("%s evidence: %v", tc.name, matches[0].libraryMatch().Evidence)
		})
	}
}

// fakePkgs is an in-memory pkgSource: packages by spec ("npm:widget"),
// failures by spec, or every lookup failing.
type fakePkgs struct {
	pkgs map[string]pkgInfo
	errs map[string]error
	all  error
}

func (f *fakePkgs) lookup(_ context.Context, eco ecosystem, name string) (pkgInfo, error) {
	key := string(eco) + ":" + name
	switch {
	case f.all != nil:
		return pkgInfo{}, f.all
	case f.errs[key] != nil:
		return pkgInfo{}, f.errs[key]
	}
	p, ok := f.pkgs[key]
	if !ok {
		return pkgInfo{}, fmt.Errorf("%s %s: %w", eco, name, ErrNotFound)
	}
	return p, nil
}

func (f *fakePkgs) lookupAll(ctx context.Context, spec libSpec) []pkgLookup {
	var out []pkgLookup
	for _, eco := range registriesFor(spec) {
		name := normalizeName(eco, spec.name)
		info, err := f.lookup(ctx, eco, name)
		out = append(out, pkgLookup{eco: eco, name: name, info: info, err: err})
	}
	return out
}

func (f *fakePkgs) goVersions(context.Context, string) ([]string, error) { return nil, nil }

func (f *fakePkgs) searchGo(context.Context, string, int) ([]string, error) { return nil, nil }

// fakeRepos is an in-memory repoSource: repositories by key, renames to
// follow, search hits by name, or every call failing.
type fakeRepos struct {
	repos   map[repoKey]repoInfo
	renames map[repoKey]repoKey
	hits    map[string][]repoInfo
	err     error

	mu    sync.Mutex
	calls []string
}

func (f *fakeRepos) note(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeRepos) repo(_ context.Context, key repoKey) (repoInfo, error) {
	f.note("repo " + key.String())
	if f.err != nil {
		return repoInfo{}, f.err
	}
	if to, ok := f.renames[key]; ok {
		key = to
	}
	info, ok := f.repos[key]
	if !ok {
		return repoInfo{}, fmt.Errorf("github repository %s: %w", key, ErrNotFound)
	}
	info.key = key
	return info, nil
}

func (f *fakeRepos) searchRepos(_ context.Context, name string, n int) ([]repoInfo, error) {
	f.note("search " + name)
	if f.err != nil {
		return nil, f.err
	}
	hits := f.hits[name]
	return hits[:min(n, len(hits))], nil
}

// fakeSites is a siteSearcher answering every query with hits.
type fakeSites struct {
	hits []search.Result
	err  error

	mu      sync.Mutex
	queries []string
}

func (f *fakeSites) Search(_ context.Context, q string, _ int) ([]search.Result, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	return f.hits, f.err
}

// resolverWeb is a backend over fakes; a nil source stays unset.
func resolverWeb(f Fetcher, pkgs *fakePkgs, repos *fakeRepos, sites *fakeSites) *Web {
	w := NewWeb(f)
	if pkgs != nil {
		w.pkgs = pkgs
	}
	if repos != nil {
		w.repos = repos
	}
	if sites != nil {
		w.sites = sites
	}
	return w
}

func repo(owner, name string) repoKey { return repoKey{owner: owner, name: name} }

// widgetSites are two unrelated projects called widget, each with its own
// docs site linking back to its repository.
func widgetSites() *fakeFetcher {
	return &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://widgetjs.dev/": html(`<!doctype html><html><head><title>Widget.js Docs</title><meta name="generator" content="Docusaurus v3"></head>
<body><a href="/docs/intro">Intro</a><a href="/docs/configuration">Configuration</a><a href="https://github.com/widget-js/widget">GitHub</a></body></html>`),
			"https://widgetjs.dev/llms.txt":              plain("# Widget\n\n> Widgets for the browser.\n\n- [Configuration](https://widgetjs.dev/docs/configuration.md): configure widgets\n"),
			"https://widgetjs.dev/docs/configuration.md": md("# Configuration\n\nWidget configuration lives in widget.config.js.\n"),
			"https://pywidget.readthedocs.io/": html(`<!doctype html><html><head><title>pywidget documentation</title><meta name="readthedocs-project-slug" content="pywidget"></head>
<body><a href="quickstart.html">Quickstart</a><a href="config.html">Configuration</a><a href="https://github.com/pywidget/widget">Source</a></body></html>`),
		},
	}
}

func widgetPkgs() *fakePkgs {
	return &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:widget":  {eco: ecoNPM, name: "widget", version: "3.1.0", homepage: "https://widgetjs.dev", repoURL: "https://github.com/widget-js/widget", repo: repo("widget-js", "widget")},
		"pypi:widget": {eco: ecoPyPI, name: "widget", version: "2.0.0", docsURL: "https://pywidget.readthedocs.io/", repoURL: "https://github.com/pywidget/widget", repo: repo("pywidget", "widget")},
	}}
}

func widgetRepos(jsStars, pyStars int) *fakeRepos {
	return &fakeRepos{repos: map[repoKey]repoInfo{
		repo("widget-js", "widget"): {homepage: "https://widgetjs.dev", stars: jsStars},
		repo("pywidget", "widget"):  {homepage: "https://pywidget.readthedocs.io", stars: pyStars},
	}}
}

// Two verified projects called widget: --resolve lists both, a bare query
// is ambiguous, and pypi:widget picks one.
func TestResolveWidgetAmbiguity(t *testing.T) {
	w := resolverWeb(widgetSites(), widgetPkgs(), widgetRepos(900, 300), nil)
	ctx := context.Background()

	matches, err := w.ResolveLibrary(ctx, "widget", 5)
	if err != nil || len(matches) != 2 {
		t.Fatalf("ResolveLibrary(widget) = %+v, %v; want 2 matches", matches, err)
	}
	ids := []string{matches[0].ID, matches[1].ID}
	if !slices.Contains(ids, "https://widgetjs.dev/") || !slices.Contains(ids, "https://pywidget.readthedocs.io/") {
		t.Errorf("IDs = %v", ids)
	}

	_, err = w.Search(ctx, "widget configuration", 5)
	var amb *AmbiguousLibraryError
	if !errors.As(err, &amb) || !errors.Is(err, ErrLibraryRequired) || len(amb.Matches) != 2 {
		t.Fatalf("Search(widget configuration) = %v, want an AmbiguousLibraryError over 2 libraries", err)
	}
	for _, want := range []string{"https://widgetjs.dev/", "npm:widget", "https://pywidget.readthedocs.io/", "pypi:widget"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ambiguity message lacks %q: %v", want, err)
		}
	}
	if _, err := w.GetDocs(ctx, "widget", "configuration", 1000); !errors.As(err, &amb) {
		t.Errorf("GetDocs(widget) = %v, want the ambiguity", err)
	}

	matches, err = w.ResolveLibrary(ctx, "pypi:widget", 5)
	if err != nil || len(matches) != 1 || matches[0].ID != "https://pywidget.readthedocs.io/" {
		t.Errorf("ResolveLibrary(pypi:widget) = %+v, %v", matches, err)
	}
}

// One widget with 40k stars, the other with 50: the popular one is picked
// without asking.
func TestResolveWidgetDominance(t *testing.T) {
	w := resolverWeb(widgetSites(), widgetPkgs(), widgetRepos(40000, 50), nil)
	m, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false)
	if err != nil || m.root.String() != "https://widgetjs.dev/" {
		t.Fatalf("resolveOne(widget) = %s, %v; want widgetjs.dev", m.libraryMatch().ID, err)
	}
	results, err := w.Search(context.Background(), "widget configuration", 5)
	if err != nil || len(results) == 0 || results[0].Library != "https://widgetjs.dev/" || !strings.Contains(results[0].URL, "/docs/configuration") {
		t.Fatalf("Search(widget configuration) = %+v, %v", results, err)
	}
	// An old PyPI package that names no repository, with a docs site of its
	// own: its stars are unknown, so it could be the library meant, and the
	// ambiguity stays. So it does for one whose repository is on GitLab.
	pkgs := widgetPkgs()
	pkgs.pkgs["pypi:widget"] = pkgInfo{eco: ecoPyPI, name: "widget", docsURL: "https://pywidget.readthedocs.io/"}
	w = resolverWeb(widgetSites(), pkgs, widgetRepos(40000, 50), nil)
	if _, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false); !errors.Is(err, ErrAmbiguousLibrary) {
		t.Errorf("a project with no repository and its own docs site must keep the ambiguity: %v", err)
	}
	pkgs.pkgs["pypi:widget"] = pkgInfo{eco: ecoPyPI, name: "widget", docsURL: "https://pywidget.readthedocs.io/", repoURL: "https://gitlab.com/pywidget/widget"}
	w = resolverWeb(widgetSites(), pkgs, widgetRepos(40000, 50), nil)
	if _, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false); !errors.Is(err, ErrAmbiguousLibrary) {
		t.Errorf("a project with unknown stars and its own docs site must keep the ambiguity: %v", err)
	}
	// The same package with docs only on pythonhosted.org, PyPI's retired
	// docs hosting, is the squatter signal: it no longer blocks.
	sites := widgetSites()
	sites.raw["https://pythonhosted.org/widget/"] = html(`<!doctype html><html><head><title>Welcome to widget's documentation!</title></head><body><a href="intro.html">Intro</a><a href="api.html">API</a></body></html>`)
	pkgs.pkgs["pypi:widget"] = pkgInfo{eco: ecoPyPI, name: "widget", docsURL: "https://pythonhosted.org/widget/"}
	w = resolverWeb(sites, pkgs, widgetRepos(40000, 50), nil)
	if m, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false); err != nil || m.root.String() != "https://widgetjs.dev/" {
		t.Errorf("a project with only index-hosted docs and unknown stars must not block dominance: %s, %v", m.libraryMatch().ID, err)
	}
}

// project.stars: 0 when GitHub says the repository does not exist, -1
// whenever no count was read.
func TestProjectStars(t *testing.T) {
	gh := pkgInfo{eco: ecoPyPI, name: "x", repoURL: "https://github.com/o/x", repo: repo("o", "x")}
	cases := []struct {
		name string
		p    project
		want int
	}{
		{"read", project{repo: repo("o", "x"), pkgs: []pkgInfo{gh}, info: &repoInfo{stars: 1234}}, 1234},
		{"repository gone", project{repo: repo("o", "x"), pkgs: []pkgInfo{gh}, repoGone: true}, 0},
		{"no repository", project{pkgs: []pkgInfo{{eco: ecoPyPI, name: "x", homepage: "https://x.dev"}}}, -1},
		{"repository on GitLab", project{pkgs: []pkgInfo{{eco: ecoCrates, name: "x", repoURL: "https://gitlab.com/o/x.git"}}}, -1},
		{"GitHub not asked or refused", project{repo: repo("o", "x"), pkgs: []pkgInfo{gh}}, -1},
	}
	for _, tc := range cases {
		if got := tc.p.stars(); got != tc.want {
			t.Errorf("%s: stars = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestPickUnique(t *testing.T) {
	m := func(project string, stars int, registry bool) webMatch {
		root, _ := parseWebRoot("https://" + strings.ReplaceAll(project, ":", "-") + ".dev/")
		return webMatch{root: root, project: project, stars: stars, registry: registry, indexOnly: registry, title: project}
	}
	cases := []struct {
		name    string
		matches []webMatch
		want    string // project picked; "" for an ambiguity
	}{
		{"one project", []webMatch{m("a", -1, false)}, "a"},
		{"a guide and its API reference", []webMatch{m("a", 10, false), m("a", 10, true)}, "a"},
		{"dominant", []webMatch{m("a", 50, false), m("b", 40000, false)}, "b"},
		{"just under the ratio", []webMatch{m("a", 40000, false), m("b", 801, false)}, ""},
		{"under the star floor", []webMatch{m("a", 999, false), m("b", 0, false)}, ""},
		{"stars unknown", []webMatch{m("a", 40000, false), m("b", -1, false)}, ""},
		{"three, all known", []webMatch{m("a", 100000, false), m("b", 10, false), m("c", 2000, false)}, "a"},
		{"repository gone", []webMatch{m("a", 15000, true), m("b", 0, false)}, "a"},
		{"index-hosted docs alone, stars unknown", []webMatch{m("a", 53000, false), m("b", -1, true)}, "a"},
		{"index-hosted top beside index-hosted docs alone", []webMatch{m("a", 16000, true), m("b", -1, true)}, "a"},
		{"index-hosted docs alone, stars known", []webMatch{m("a", 53000, false), m("b", 5000, true)}, ""},
		{"own site, stars unknown", []webMatch{m("a", 53000, true), m("b", -1, false)}, ""},
		{"top's stars unknown", []webMatch{m("a", -1, false), m("b", 0, true)}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickUnique("lib", tc.matches)
			if tc.want == "" {
				var amb *AmbiguousLibraryError
				if !errors.As(err, &amb) || amb.Library != "lib" {
					t.Errorf("pickUnique = %s, %v; want an ambiguity", got.project, err)
				}
				return
			}
			if err != nil || got.project != tc.want {
				t.Errorf("pickUnique = %s, %v; want %s", got.project, err, tc.want)
			}
		})
	}
	if _, err := pickUnique("lib", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("no matches = %v", err)
	}
}

// gizmoFetcher serves gizmo's real docs and a mirror of them, which links
// back to the real repository from another domain.
func gizmoFetcher() *fakeFetcher {
	return &fakeFetcher{raw: map[string]fakeRaw{
		"https://gizmo-mirror.net/": html(`<!doctype html><html><head><title>Gizmo Documentation</title><meta name="generator" content="Sphinx 8"></head>
<body><a href="guide.html">Guide</a><a href="api.html">API</a><a href="https://github.com/gizmo-org/gizmo">Source</a></body></html>`),
		"https://docs.gizmo.dev/": html(`<!doctype html><html><head><title>Gizmo Documentation</title><meta name="generator" content="Sphinx 8"></head>
<body><a href="guide.html">Guide</a><a href="api.html">API</a><a href="https://github.com/gizmo-org/gizmo">Source</a></body></html>`),
	}}
}

// A web search hit that links back to the real repository from a domain
// unrelated to the project is a mirror; the docs. host on the project's
// own domain is not.
func TestResolveRejectsMirror(t *testing.T) {
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"crates:gizmo": {eco: ecoCrates, name: "gizmo", homepage: "https://gizmo.dev", repo: repo("gizmo-org", "gizmo"), registryDocs: "https://docs.rs/gizmo/latest/gizmo/"},
	}}
	repos := &fakeRepos{repos: map[repoKey]repoInfo{repo("gizmo-org", "gizmo"): {homepage: "https://gizmo.dev", stars: 1200}}}
	f := gizmoFetcher()
	f.raw["https://docs.rs/gizmo/latest/gizmo/"] = html(`<!doctype html><html><head><title>gizmo - Rust</title><meta name="generator" content="rustdoc"></head><body><a href="https://github.com/gizmo-org/gizmo">Repository</a><a href="struct.Gizmo.html">Gizmo</a></body></html>`)
	sites := &fakeSites{hits: []search.Result{{URL: "https://gizmo-mirror.net/guide.html"}, {URL: "https://docs.gizmo.dev/api.html"}}}
	w := resolverWeb(f, pkgs, repos, sites)

	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "crates:gizmo"), 0, log)
	if err != nil {
		t.Fatal(err)
	}
	ids := matchIDs(matches)
	if len(matches) != 2 || matches[0].root.String() != "https://docs.gizmo.dev/" || matches[1].root.String() != "https://docs.rs/gizmo/latest/gizmo/" {
		t.Fatalf("matches = %v; want docs.gizmo.dev (by search, on gizmo.dev's domain) and docs.rs", ids)
	}
	if !hasEvidence(matches[0].evidence, evRelatedDomain) || matches[0].primary != true || matches[1].primary {
		t.Errorf("search match evidence = %v", matches[0].evidence)
	}
	assertReasons(t, log, "search hit https://gizmo-mirror.net/ links back to gizmo-org/gizmo but is not on the project's own domain: a mirror")
	if len(sites.queries) != 1 || sites.queries[0] != "gizmo documentation" {
		t.Errorf("queries = %v", sites.queries)
	}
}

// A package whose repository is a fork: the fork's homepage, a mirror of
// its upstream's docs, does not name the package, so the claim is not
// counted and the mirror is never read; and a package that lists the mirror
// itself gets nowhere, since the mirror links only to the upstream
// repository.
func TestResolveRejectsForkHomepage(t *testing.T) {
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"pypi:gizmo": {eco: ecoPyPI, name: "gizmo", repo: repo("copycat", "gizmo")},
	}}
	repos := &fakeRepos{repos: map[repoKey]repoInfo{
		repo("copycat", "gizmo"): {homepage: "https://gizmo-mirror.net", fork: true, parent: repo("gizmo-org", "gizmo"), stars: 3},
	}}
	f := gizmoFetcher()
	w := resolverWeb(f, pkgs, repos, nil)
	log := &resolveLog{}
	_, err := w.resolveLogged(context.Background(), mustSpec(t, "pypi:gizmo"), 0, log)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolve = %v, want not found", err)
	}
	if !strings.Contains(err.Error(), "pypi gizmo: github.com/copycat/gizmo does not name it back") {
		t.Errorf("error should give the reason: %v", err)
	}
	assertNeverFetched(t, f, "gizmo-mirror.net")

	// Its own package listing the mirror does not help: the mirror links
	// to a same-named repository of another owner.
	pkgs.pkgs["pypi:gizmo"] = pkgInfo{eco: ecoPyPI, name: "gizmo", docsURL: "https://gizmo-mirror.net/", repo: repo("copycat", "gizmo")}
	_, err = w.resolveLogged(context.Background(), mustSpec(t, "pypi:gizmo"), 0, &resolveLog{})
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "links to github.com/gizmo-org/gizmo, not copycat/gizmo") {
		t.Errorf("resolve = %v", err)
	}
}

// clickSite is click's docs for the synthetic sources below.
func clickSite() *fakeFetcher {
	return &fakeFetcher{raw: map[string]fakeRaw{
		"https://click.palletsprojects.com/": html(`<!doctype html><html><head><title>Welcome to Click — Click Documentation (8.5.x)</title></head>
<body><a href="quickstart/">Quickstart</a><a href="api/">API</a><a href="https://github.com/pallets/click/">Source Code</a></body></html>`),
	}}
}

func clickPkgs() *fakePkgs {
	return &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:click":  {eco: ecoNPM, name: "click", repo: repo("uggedal", "click")},
		"pypi:click": {eco: ecoPyPI, name: "click", docsURL: "https://click.palletsprojects.com/", repo: repo("pallets", "click")},
	}}
}

// GitHub refusing for its rate limit while PyPI's metadata still verifies
// the site: the match stands, with a note saying GitHub was not asked.
func TestResolveRateLimitedGitHubStillMatches(t *testing.T) {
	repos := &fakeRepos{err: &ghRateLimitError{}}
	w := resolverWeb(clickSite(), clickPkgs(), repos, nil)
	matches, err := w.ResolveLibrary(context.Background(), "click", 5)
	if err != nil || len(matches) != 1 {
		t.Fatalf("ResolveLibrary(click) = %+v, %v", matches, err)
	}
	m := matches[0]
	if m.ID != "https://click.palletsprojects.com/" || !slices.Contains(m.Evidence, "github: rate limited (set github_token or gh auth login)") {
		t.Errorf("match = %+v; want click's docs with the rate-limit note", m)
	}
	if m.TrustScore < minTrust || m.TrustScore >= 10 {
		t.Errorf("trust %.1f: verified, but without GitHub's homepage", m.TrustScore)
	}
	if n := len(repos.calls); n != 1 {
		t.Errorf("GitHub asked %d times after its first refusal: %v", n, repos.calls)
	}
}

// A GitHub repository spec has GitHub as its only source: a rate limit
// there is an upstream failure with the token hint, not a missing library.
func TestResolveGitHubOnlySourceRateLimited(t *testing.T) {
	w := resolverWeb(clickSite(), clickPkgs(), &fakeRepos{err: &ghRateLimitError{}}, nil)
	_, err := w.ResolveLibrary(context.Background(), "pallets/click", 5)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) {
		t.Fatalf("ResolveLibrary(pallets/click) = %v, want an upstream error", err)
	}
	if !strings.Contains(err.Error(), "set github_token or gh auth login") {
		t.Errorf("error lacks the token hint: %v", err)
	}
}

// Every source failing (5xx) is upstream; every source answering "no such
// package" is not-found, naming each source's answer.
func TestResolveSourceFailureClassification(t *testing.T) {
	down := errors.New("status 503")
	w := resolverWeb(&fakeFetcher{}, &fakePkgs{all: down}, &fakeRepos{err: errors.New("github: status 502")}, nil)
	_, err := w.ResolveLibrary(context.Background(), "click", 5)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) || !strings.Contains(err.Error(), "status 503") {
		t.Errorf("every source 5xx = %v, want a plain upstream error", err)
	}
	if strings.Contains(fmt.Sprint(err), "github_token") {
		t.Errorf("no token hint unless GitHub refused for its rate limit: %v", err)
	}

	w = resolverWeb(&fakeFetcher{}, &fakePkgs{}, &fakeRepos{}, &fakeSites{})
	_, err = w.ResolveLibrary(context.Background(), "zzyzx", 5)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("every source 404 = %v, want not found", err)
	}
	for _, want := range []string{"npm zzyzx: no such package", "pypi zzyzx: no such package", "crates zzyzx: no such package"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("not-found message lacks %q: %v", want, err)
		}
	}
	if _, err := w.GetDocs(context.Background(), "zzyzx", "q", 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDocs(zzyzx) = %v, want not found", err)
	}
}

// Metadata URLs come from third parties: private and local hosts are
// rejected with a reason and never fetched.
func TestResolveRejectsPrivateMetadataURLs(t *testing.T) {
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:gadget":  {eco: ecoNPM, name: "gadget", homepage: "http://192.168.1.20/docs/"},
		"pypi:gadget": {eco: ecoPyPI, name: "gadget", docsURL: "http://intranet.corp.internal/gadget", homepage: "localhost:8080"},
	}}
	f := &fakeFetcher{}
	w := resolverWeb(f, pkgs, nil, nil)
	_, err := w.ResolveLibrary(context.Background(), "gadget", 5)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolve = %v", err)
	}
	for _, want := range []string{"npm gadget: homepage http://192.168.1.20/docs/ is not a public web address",
		"pypi gadget: documentation URL http://intranet.corp.internal/gadget is not a public web address"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if calls := f.called(); len(calls) != 0 {
		t.Errorf("fetched %v", calls)
	}
}

// A name resolves, then the resolved root is searched; a malformed spec is
// a validation error before anything is fetched.
func TestGetDocsResolvesNames(t *testing.T) {
	w := resolverWeb(widgetSites(), widgetPkgs(), widgetRepos(900, 300), nil)
	results, err := w.GetDocs(context.Background(), "npm:widget", "configuration", 1000)
	if err != nil || len(results) == 0 || results[0].Library != "https://widgetjs.dev/" || results[0].URL != "https://widgetjs.dev/docs/configuration" {
		t.Fatalf("GetDocs(npm:widget) = %+v, %v", results, err)
	}
	for _, bad := range []string{"widget@", "maven:widget", "https://widgetjs.dev/@3"} {
		if _, err := w.GetDocs(context.Background(), bad, "q", 100); !errors.Is(err, ErrLibraryRequired) {
			t.Errorf("GetDocs(%q) = %v, want ErrLibraryRequired", bad, err)
		}
	}
}

// A bare query with no library in it, or none that resolves, asks for one.
func TestSearchNoLibraryIdentified(t *testing.T) {
	w := resolverWeb(&fakeFetcher{}, &fakePkgs{}, &fakeRepos{}, nil)
	_, err := w.Search(context.Background(), "verify webhook signatures", 5)
	if !errors.Is(err, ErrLibraryRequired) || !strings.Contains(err.Error(), "--library") || !strings.Contains(err.Error(), "library (MCP)") {
		t.Errorf("Search = %v, want ErrLibraryRequired naming --library", err)
	}
}

// A docs URL keeps phase 1's answer and gains evidence from the repository
// it links to when GitHub names the site as that repository's homepage.
func TestResolveURLEvidence(t *testing.T) {
	repos := &fakeRepos{repos: map[repoKey]repoInfo{repo("pallets", "click"): {homepage: "https://click.palletsprojects.com", stars: 17000}}}
	w := resolverWeb(clickSite(), nil, repos, nil)
	matches, err := w.ResolveLibrary(context.Background(), "https://click.palletsprojects.com/", 5)
	if err != nil || len(matches) != 1 {
		t.Fatalf("ResolveLibrary = %+v, %v", matches, err)
	}
	m := matches[0]
	if m.ID != "https://click.palletsprojects.com/" || m.Title != "Welcome to Click" {
		t.Errorf("match = %+v", m)
	}
	if !slices.Contains(m.Evidence, "github pallets/click homepage") || !slices.Contains(m.Evidence, "links to github.com/pallets/click") ||
		!slices.Equal(m.Packages, []string{"github:pallets/click"}) || m.TrustScore != 6 {
		t.Errorf("evidence %v, packages %v, trust %.1f", m.Evidence, m.Packages, m.TrustScore)
	}

	// No repository ties: trust 0, no evidence, still answered.
	matches, err = resolverWeb(plainSite(), nil, &fakeRepos{}, nil).ResolveLibrary(context.Background(), "https://plain.test/", 5)
	if err != nil || len(matches) != 1 || matches[0].TrustScore != 0 || len(matches[0].Evidence) != 0 {
		t.Errorf("unverified URL = %+v, %v", matches, err)
	}
}

// Caps: at most six roots are verified per pass, however many a project's
// metadata offers.
func TestResolveCapsVerifiedRoots(t *testing.T) {
	raw := map[string]fakeRaw{}
	var nav strings.Builder
	for i := range 9 {
		host := fmt.Sprintf("docs%d.manyroots.dev", i)
		fmt.Fprintf(&nav, `<a href="https://%s/">Documentation</a>`, host)
		raw["https://"+host+"/"] = html(`<!doctype html><html><head><title>Docs</title></head><body><a href="/a">A</a></body></html>`)
	}
	raw["https://manyroots.dev/"] = html(`<!doctype html><html><head><title>Many</title></head><body>` + nav.String() + `</body></html>`)
	f := &fakeFetcher{raw: raw}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"npm:many": {eco: ecoNPM, name: "many", homepage: "https://manyroots.dev"}}}
	w := resolverWeb(f, pkgs, nil, nil)
	if _, err := w.ResolveLibrary(context.Background(), "npm:many", 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nav links alone (trust 2) verify nothing: %v", err)
	}
	fetched := 0
	for _, c := range f.called() {
		if strings.HasPrefix(c, "raw https://docs") && strings.HasSuffix(c, ".manyroots.dev/") {
			fetched++
		}
	}
	if fetched != webMaxRoots {
		t.Errorf("verified %d roots, want %d", fetched, webMaxRoots)
	}
}

// A registry's docs host is a candidate only as a package's own registry
// docs: tokio.rs's nav link to docs.rs's front page is not tokio's docs.
func TestResolveSkipsRegistryHostNavLinks(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://tokio.example/": html(`<!doctype html><html><head><title>Tokio</title></head>
<body><a href="https://docs.rs/">API docs</a><a href="https://pkg.go.dev/">Reference</a><a href="/tokio/tutorial">Learn</a><a href="https://github.com/tokio-rs/tokio">GitHub</a></body></html>`),
	}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"crates:tokio": {eco: ecoCrates, name: "tokio", homepage: "https://tokio.example", repo: repo("tokio-rs", "tokio")}}}
	matches, err := resolverWeb(f, pkgs, nil, nil).resolve(context.Background(), mustSpec(t, "crates:tokio"), 0)
	if err != nil || len(matches) != 1 || matches[0].root.String() != "https://tokio.example/" {
		t.Fatalf("matches = %v, %v; want tokio.example alone", matchIDs(matches), err)
	}
	assertNeverFetched(t, f, "https://docs.rs/", "https://pkg.go.dev/")
}

// tokioDocs serves docs.rs's tokio: ~1.38 floats to 1.38.2, while a bare
// 1.38 floats to the newest release, 1.54.1.
func tokioDocs() *fakeFetcher {
	page := html(`<!doctype html><html><head><title>tokio - Rust</title><meta name="generator" content="rustdoc"></head><body><a href="https://github.com/tokio-rs/tokio">Repository</a><a href="runtime/index.html">runtime</a></body></html>`)
	return &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://docs.rs/tokio/latest/tokio/": page,
			"https://docs.rs/tokio/1.38.2/tokio/": page,
			"https://docs.rs/tokio/1.54.1/tokio/": page,
		},
		redirects: map[string]string{
			"https://docs.rs/tokio/~1.38/tokio/": "https://docs.rs/tokio/1.38.2/tokio/",
		},
	}
}

// docs.rs's ~1.38 is followed to the release it serves and labelled so; a
// version root that floats away from the version asked for fails the
// post-check.
func TestResolveRegistryVersion(t *testing.T) {
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"crates:tokio": {eco: ecoCrates, name: "tokio", version: "1.54.1", repo: repo("tokio-rs", "tokio"), registryDocs: "https://docs.rs/tokio/latest/tokio/"}}}
	matches, err := resolverWeb(tokioDocs(), pkgs, nil, nil).resolve(context.Background(), mustSpec(t, "crates:tokio@1.38"), 0)
	if err != nil || len(matches) != 1 {
		t.Fatalf("tokio@1.38 = %v, %v", matchIDs(matches), err)
	}
	if m := matches[0].libraryMatch(); m.ID != "https://docs.rs/tokio/1.38.2/tokio/" || m.Version != "1.38.2" {
		t.Errorf("tokio@1.38 = %s version %q, want docs.rs's 1.38.2", m.ID, m.Version)
	}

	f := tokioDocs()
	f.redirects["https://docs.rs/tokio/~1.38/tokio/"] = "https://docs.rs/tokio/1.54.1/tokio/"
	_, err = resolverWeb(f, pkgs, nil, nil).resolve(context.Background(), mustSpec(t, "crates:tokio@1.38"), 0)
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "serves https://docs.rs/tokio/1.54.1/tokio/") {
		t.Errorf("a version root serving another release = %v, want not found naming what it serves", err)
	}
}

// A site whose current docs carry no release of their own ("current",
// beside versioned ones) takes a version on the package's latest major,
// saying so; another major must be one the site lists.
func TestPickVersionOnUnlabelledCurrent(t *testing.T) {
	root, _ := parseWebRoot("https://vite.example/")
	p := &project{pkgs: []pkgInfo{{eco: ecoNPM, name: "vite", version: "7.1.2"}}}
	vs := versionSet{versions: []siteVersion{
		{label: labelCurrent, root: "https://vite.example/", current: true},
		{label: "v4", root: "https://v4.vite.example/"},
		{label: "v3", root: "https://v3.vite.example/"},
	}}
	r := &resolution{w: NewWeb(&fakeFetcher{}), spec: mustSpec(t, "npm:vite@7"), log: &resolveLog{}}
	m := webMatch{root: root, proj: p, prof: &siteProfile{served: root.u}}
	if err := r.pickVersion(context.Background(), &m, vs); err != nil || m.version != "7.1.2" || !hasEvidence(m.evidence, evNote) {
		t.Errorf("vite@7 = %q %v, %v; want the current docs as 7.1.2, noted", m.version, m.evidence, err)
	}
	r.spec = mustSpec(t, "npm:vite@5")
	m = webMatch{root: root, proj: p, prof: &siteProfile{served: root.u}}
	if err := r.pickVersion(context.Background(), &m, vs); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "v4") {
		t.Errorf("vite@5 = %v, want not found listing the versions", err)
	}
}

// The reported version is a release where any source says which one the
// current docs are, not the alias they are served under.
func TestCurrentLabelPrefersRelease(t *testing.T) {
	cases := []struct {
		name  string
		vs    []siteVersion
		title string
		want  string
	}{
		{"a concrete label", []siteVersion{{label: "6.1", aliases: []string{"stable"}, current: true}}, "", "6.1"},
		{"a concrete alias", []siteVersion{{label: "stable", aliases: []string{"8.5.x"}, current: true}}, "", "8.5.x"},
		{"a release at the same root", []siteVersion{{label: "stable", root: "https://w.example/en/stable/", current: true}, {label: "2.4", root: "https://w.example/en/stable"}}, "", "2.4"},
		{"a release the title names", []siteVersion{{label: "stable", root: "https://w.example/en/stable/", current: true}, {label: "latest"}, {label: "2.4"}, {label: "2.3"}}, "Widget 2.4 documentation", "2.4"},
		{"nothing says", []siteVersion{{label: "stable", current: true}, {label: "2.3"}}, "Widget documentation", "stable"},
		{"no versions", nil, "Widget 2.4", ""},
	}
	for _, tc := range cases {
		if got := currentLabel(versionSet{versions: tc.vs}, tc.title); got != tc.want {
			t.Errorf("%s: currentLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A package homepage that redirects into non-public address space (the
// cloud metadata endpoint) is never followed there: the hop is refused,
// the homepage is not fetched again another way (FetchPage), and the
// candidate is rejected rather than counted as a failed source.
func TestResolveRefusesRedirectIntoPrivateSpace(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://gizmo.dev/", testutil.Fixture{Status: http.StatusFound, Header: http.Header{"Location": {"http://169.254.169.254/latest/meta-data/"}}})
	ft.Handle(http.MethodGet, "https://gizmo.dev/redirect/", testutil.Fixture{Status: http.StatusFound, Header: http.Header{"Location": {"http://[::ffff:169.254.169.254%25eth0]/latest/"}}})
	ft.Handle(http.MethodGet, "http://169.254.169.254/latest/meta-data/", testutil.Fixture{Body: "iam/security-credentials/"})
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:gizmo":  {eco: ecoNPM, name: "gizmo", homepage: "https://gizmo.dev/", repoURL: "https://github.com/gizmo-org/gizmo", repo: repo("gizmo-org", "gizmo")},
		"pypi:gizmo": {eco: ecoPyPI, name: "gizmo", docsURL: "https://gizmo.dev/redirect"},
	}}
	repos := &fakeRepos{repos: map[repoKey]repoInfo{repo("gizmo-org", "gizmo"): {stars: 10}}}
	w := resolverWeb(&fixtureFetcher{client: ft.Client()}, pkgs, repos, nil)
	log := &resolveLog{}
	_, err := w.resolveLogged(context.Background(), mustSpec(t, "gizmo"), 0, log)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("resolve gizmo = %v, want not-found: a refused redirect is no failed source", err)
	}
	homepage := 0
	for _, c := range ft.Calls() {
		switch {
		case strings.Contains(c.String(), "169.254"):
			t.Errorf("requested %s", c)
		case c.String() == "GET https://gizmo.dev/":
			homepage++
		}
	}
	if homepage != 1 {
		t.Errorf("the homepage was requested %d times, want once: no FetchPage retry after a refused hop", homepage)
	}
	assertReasons(t, log, "npm gizmo: homepage https://gizmo.dev/: Get \"http://169.254.169.254/latest/meta-data/\": redirects to a non-public address",
		"pypi gizmo: https://gizmo.dev/redirect/: Get \"http://[::ffff:169.254.169.254%25eth0]/latest/\": redirects to a non-public address")
}

// A package that merely claims a repository does not take the project
// over. PyPI's gizmo lists an evil docs site and the real gizmo-org/gizmo
// as its source; the repository's homepage, gizmo.dev, names npm's gizmo
// and not it. So the evil site stays a project of its own, beside the real
// docs rather than in their place, and lends nothing from the repository.
func TestResolveUncorroboratedRepositoryClaim(t *testing.T) {
	f := gizmoFetcher()
	f.raw["https://gizmo.dev/"] = html(`<!doctype html><html><head><title>Gizmo</title></head><body><a href="https://docs.gizmo.dev/">Docs</a><a href="https://github.com/gizmo-org/gizmo">GitHub</a></body></html>`)
	f.raw["https://gizmo-docs.evil.net/"] = html(`<!doctype html><html><head><title>Gizmo Documentation</title><meta name="generator" content="Sphinx 8"></head>
<body><a href="guide.html">Guide</a><a href="https://github.com/gizmo-org/gizmo">Source</a></body></html>`)
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:gizmo":  {eco: ecoNPM, name: "gizmo", homepage: "https://gizmo.dev/", repoURL: "https://github.com/gizmo-org/gizmo", repo: repo("gizmo-org", "gizmo")},
		"pypi:gizmo": {eco: ecoPyPI, name: "gizmo", docsURL: "https://gizmo-docs.evil.net/", repoURL: "https://github.com/gizmo-org/gizmo", repo: repo("gizmo-org", "gizmo")},
	}}
	// The repository is named gizmo, but it is TypeScript, npm's language,
	// so the name does not make the PyPI package its own either.
	repos := &fakeRepos{repos: map[repoKey]repoInfo{repo("gizmo-org", "gizmo"): {homepage: "https://gizmo.dev", stars: 40000, language: "TypeScript"}}}
	w := resolverWeb(f, pkgs, repos, nil)
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "gizmo"), 0, log)
	if err != nil {
		t.Fatal(err)
	}
	byRoot := map[string]webMatch{}
	for _, m := range matches {
		byRoot[m.root.String()] = m
	}
	real, evil := byRoot["https://docs.gizmo.dev/"], byRoot["https://gizmo-docs.evil.net/"]
	if real.project != "github:gizmo-org/gizmo" || evil.project != "pypi:gizmo" {
		t.Fatalf("matches = %v; want docs.gizmo.dev for gizmo-org/gizmo and the evil site apart, as pypi:gizmo", matchIDs(matches))
	}
	if evil.stars != -1 || hasEvidence(evil.evidence, evLinksRepo) || hasEvidence(evil.evidence, evRepoAgrees) || slices.Contains(evil.packages, "github:gizmo-org/gizmo") {
		t.Errorf("the evil site borrowed from the repository: stars %d, evidence %v, packages %v", evil.stars, evil.evidence, evil.packages)
	}
	if !slices.Contains(real.packages, "npm:gizmo") || slices.Contains(real.packages, "pypi:gizmo") {
		t.Errorf("real packages = %v, want npm:gizmo and not pypi:gizmo", real.packages)
	}
	assertReasons(t, log, "pypi gizmo: github.com/gizmo-org/gizmo does not name it back")
	// Single-library contexts cannot pick the evil site: at worst the
	// name is ambiguous.
	if m, err := w.resolveOne(context.Background(), mustSpec(t, "gizmo"), false); err == nil && m.root.String() != "https://docs.gizmo.dev/" {
		t.Errorf("resolveOne(gizmo) = %s", m.root)
	}
}

// Borrowed stars: npm's widget claims react/react, whose 230k stars would
// outweigh the real PyPI widget. React's homepage does not name npm's
// widget, so the claim counts for nothing, and with no docs of its own the
// npm package matches nothing.
func TestResolveBorrowedStars(t *testing.T) {
	pkgs := widgetPkgs()
	pkgs.pkgs["npm:widget"] = pkgInfo{eco: ecoNPM, name: "widget", repoURL: "https://github.com/react/react", repo: repo("react", "react")}
	repos := widgetRepos(40000, 900)
	repos.repos[repo("react", "react")] = repoInfo{homepage: "https://react.dev", stars: 230000, language: "JavaScript"}
	sites := widgetSites()
	sites.raw["https://react.dev/"] = html(`<!doctype html><html><head><title>React</title></head><body><a href="/learn">Learn</a><a href="https://github.com/react/react">GitHub</a></body></html>`)
	w := resolverWeb(sites, pkgs, repos, nil)
	m, err := w.resolveOne(context.Background(), mustSpec(t, "widget"), false)
	if err != nil || m.root.String() != "https://pywidget.readthedocs.io/" {
		t.Fatalf("resolveOne(widget) = %s, %v; want PyPI's widget, its only verified match", m.libraryMatch().ID, err)
	}
	assertNeverFetched(t, sites, "react.dev")
}

// A claim is corroborated by the repository's homepage: the package's
// registry page, or one of the package's own URLs on the same site (a
// shared host such as docs.rs by path); by a repository named like the
// package in the registry's language, unless the package's own site is
// contradicted; and a Go module always is.
func TestCorroborated(t *testing.T) {
	gh := func(home string) *project {
		return &project{repo: repo("o", "r"), info: &repoInfo{homepage: home}}
	}
	lang := func(k repoKey, home, language string) *project {
		return &project{repo: k, info: &repoInfo{key: k, homepage: home, language: language}}
	}
	cases := []struct {
		name string
		pkg  pkgInfo
		p    *project
		want bool
	}{
		{"homepage on the repository's homepage domain", pkgInfo{eco: ecoPyPI, name: "django", homepage: "https://www.djangoproject.com/"}, gh("https://docs.djangoproject.com"), true},
		{"docs URL", pkgInfo{eco: ecoPyPI, name: "rich", docsURL: "https://rich.readthedocs.io/en/latest/"}, gh("https://rich.readthedocs.io/en/latest/"), true},
		{"the crate on docs.rs", pkgInfo{eco: ecoCrates, name: "regex", registryDocs: "https://docs.rs/regex/latest/regex/"}, gh("docs.rs/regex"), true},
		{"another crate on docs.rs", pkgInfo{eco: ecoCrates, name: "regexx", registryDocs: "https://docs.rs/regexx/latest/regexx/"}, gh("docs.rs/regex"), false},
		{"its crates.io page", pkgInfo{eco: ecoCrates, name: "rand"}, gh("https://crates.io/crates/rand"), true},
		{"its scoped npm page", pkgInfo{eco: ecoNPM, name: "@babel/core"}, gh("https://www.npmjs.com/package/@babel/core"), true},
		{"another readthedocs project", pkgInfo{eco: ecoPyPI, name: "x", docsURL: "https://evil.readthedocs.io/"}, gh("https://rich.readthedocs.io/"), false},
		{"homepage on GitHub", pkgInfo{eco: ecoNPM, name: "x", homepage: "https://github.com/o/r#readme"}, gh("https://github.com/o/r"), false},
		{"no repository homepage", pkgInfo{eco: ecoCrates, name: "anyhow", registryDocs: "https://docs.rs/anyhow/latest/anyhow/"}, gh(""), false},
		{"GitHub not asked", pkgInfo{eco: ecoPyPI, name: "x", homepage: "https://x.dev"}, &project{repo: repo("o", "r")}, false},
		{"a Go module", pkgInfo{eco: ecoGo, name: "github.com/o/r", repo: repo("o", "r")}, &project{repo: repo("o", "r")}, true},
		// Named like the package and in the registry's language.
		{"anyhow, no repository homepage", pkgInfo{eco: ecoCrates, name: "anyhow", docsURL: "https://docs.rs/anyhow"}, lang(repo("dtolnay", "anyhow"), "", "Rust"), true},
		{"serde_json in serde-rs/json", pkgInfo{eco: ecoCrates, name: "serde_json"}, lang(repo("serde-rs", "json"), "", "Rust"), true},
		{"webpack, homepage its README", pkgInfo{eco: ecoNPM, name: "webpack", homepage: "https://github.com/webpack/webpack"}, lang(repo("webpack", "webpack"), "https://webpack.js.org", "JavaScript"), true},
		{"commander in commander.js", pkgInfo{eco: ecoNPM, name: "commander"}, lang(repo("tj", "commander.js"), "", "JavaScript"), true},
		{"semver in node-semver", pkgInfo{eco: ecoNPM, name: "semver"}, lang(repo("npm", "node-semver"), "", "JavaScript"), true},
		{"a scope that is the repository", pkgInfo{eco: ecoNPM, name: "@babel/core"}, lang(repo("babel", "babel"), "", "TypeScript"), true},
		{"pyyaml", pkgInfo{eco: ecoPyPI, name: "PyYAML", homepage: "https://pyyaml.org/"}, lang(repo("yaml", "pyyaml"), "", "Python"), true},
		{"the registry's language, not the repository's", pkgInfo{eco: ecoPyPI, name: "gizmo"}, lang(repo("gizmo-org", "gizmo"), "", "TypeScript"), false},
		{"another name", pkgInfo{eco: ecoNPM, name: "widget"}, lang(repo("react", "react"), "https://react.dev", "JavaScript"), false},
		{"a prefix is not a name", pkgInfo{eco: ecoNPM, name: "react-widget"}, lang(repo("react", "react"), "", "JavaScript"), false},
		{"its own docs, contradicted by the repository's homepage", pkgInfo{eco: ecoCrates, name: "biome", docsURL: "https://biome-docs.evil.net/"},
			lang(repo("biomejs", "biome"), "https://biomejs.dev", "Rust"), false},
		{"no language", pkgInfo{eco: ecoCrates, name: "anyhow"}, lang(repo("dtolnay", "anyhow"), "", ""), false},
	}
	for _, tc := range cases {
		if got := corroborated(tc.pkg, tc.p); got != tc.want {
			t.Errorf("%s: corroborated = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A homepage's "Documentation" link to another domain names that site only
// when it links back to the repository: gadget.dev's points at a Google
// Doc, and docs.google.com is nobody's docs root. One that does link back
// (foo.readthedocs.io from foo.org) verifies. A package and its repository
// naming one homepage give one nav chain, not two.
func TestResolveOffsiteNavLinks(t *testing.T) {
	sites := &fakeFetcher{raw: map[string]fakeRaw{
		"https://gadget.dev/":                         html(`<!doctype html><html><head><title>Gadget</title></head><body><a href="https://docs.google.com/document/d/1abc/edit">Documentation</a><a href="https://github.com/gadget-org/gadget">GitHub</a></body></html>`),
		"https://docs.google.com/":                    html(`<!doctype html><html><head><title>Google Docs</title></head><body><a href="/document/">Docs</a><a href="https://www.google.com/">Google</a></body></html>`),
		"https://doohickey.org/":                      html(`<!doctype html><html><head><title>Doohickey</title></head><body><a href="https://doohickey.readthedocs.io/en/latest/">Documentation</a></body></html>`),
		"https://doohickey.readthedocs.io/en/latest/": html(`<!doctype html><html><head><title>Doohickey documentation</title><meta name="readthedocs-project-slug" content="doohickey"></head><body><a href="intro.html">Intro</a><a href="https://github.com/doohickey/doohickey">Source</a></body></html>`),
	}, redirects: map[string]string{"https://doohickey.readthedocs.io/": "https://doohickey.readthedocs.io/en/latest/"}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:gadget":     {eco: ecoNPM, name: "gadget", homepage: "https://gadget.dev/", repoURL: "https://github.com/gadget-org/gadget", repo: repo("gadget-org", "gadget")},
		"pypi:doohickey": {eco: ecoPyPI, name: "doohickey", homepage: "https://doohickey.org/", repoURL: "https://github.com/doohickey/doohickey", repo: repo("doohickey", "doohickey")},
	}}
	repos := &fakeRepos{repos: map[repoKey]repoInfo{
		repo("gadget-org", "gadget"):   {homepage: "https://gadget.dev", stars: 50},
		repo("doohickey", "doohickey"): {homepage: "https://doohickey.org", stars: 50},
	}}
	w := resolverWeb(sites, pkgs, repos, nil)
	log := &resolveLog{}
	if ms, err := w.resolveLogged(context.Background(), mustSpec(t, "npm:gadget"), 0, log); !errors.Is(err, ErrNotFound) {
		t.Errorf("resolve npm:gadget = %v, %v; want nothing verified", matchIDs(ms), err)
	}
	assertReasons(t, log, "https://docs.google.com/ is on another domain than the homepage linking to it as its docs, and does not link back to npm gadget")

	ms, err := w.resolve(context.Background(), mustSpec(t, "pypi:doohickey"), 0)
	if err != nil || len(ms) != 1 || ms[0].root.String() != "https://doohickey.readthedocs.io/en/latest/" {
		t.Fatalf("resolve pypi:doohickey = %v, %v", matchIDs(ms), err)
	}
	navs := 0
	for _, e := range ms[0].evidence {
		if e.kind == evNavChain {
			navs++
		}
	}
	if navs != 1 {
		t.Errorf("nav chains = %d in %v, want 1 for one homepage named twice", navs, ms[0].evidence)
	}
}

// Search hits never verify themselves (decision-3). With no package to go
// on, the repositories the hits link to become projects, but a hit counts
// for one only through the repository's own homepage, not because it is
// its owner's GitHub Pages site (the hit named the repository: circular),
// and never for a fork.
func TestResolveHitsCannotVouchForThemselves(t *testing.T) {
	page := func(title, repoURL string) fakeRaw {
		return html(`<!doctype html><html><head><title>` + title + `</title></head><body><a href="guide.html">Guide</a><a href="` + repoURL + `">GitHub</a></body></html>`)
	}
	sites := &fakeFetcher{raw: map[string]fakeRaw{
		"https://someone.github.io/widget/": page("Widget", "https://github.com/someone/widget"),
		"https://forker.github.io/widget/":  page("Widget", "https://github.com/forker/widget"),
		"https://widget.example/docs/":      page("Widget docs", "https://github.com/real/widget"),
	}}
	repos := &fakeRepos{repos: map[repoKey]repoInfo{
		repo("someone", "widget"): {stars: 5},
		repo("forker", "widget"):  {homepage: "https://forker.github.io/widget/", fork: true, parent: repo("real", "widget"), stars: 1},
		repo("real", "widget"):    {homepage: "https://widget.example", stars: 70},
	}}
	resolveWith := func(hits ...string) ([]webMatch, *resolveLog, error) {
		fs := &fakeSites{}
		for _, h := range hits {
			fs.hits = append(fs.hits, search.Result{URL: h})
		}
		w := resolverWeb(sites, &fakePkgs{}, repos, fs)
		log := &resolveLog{}
		ms, err := w.resolveLogged(context.Background(), mustSpec(t, "widget"), 0, log)
		return ms, log, err
	}

	if ms, log, err := resolveWith("https://someone.github.io/widget/guide.html", "https://forker.github.io/widget/guide.html"); !errors.Is(err, ErrNotFound) {
		t.Errorf("hits vouching for themselves = %v, %v; want nothing verified", matchIDs(ms), err)
	} else {
		assertReasons(t, log, "search hit https://someone.github.io/widget/ links back to someone/widget but is not on the project's own domain",
			"github forker/widget: a fork of real/widget, linked from a search hit")
	}
	ms, _, err := resolveWith("https://widget.example/docs/guide.html")
	if err != nil || len(ms) != 1 || ms[0].root.String() != "https://widget.example/docs/" {
		t.Errorf("a hit on the repository's own homepage domain = %v, %v", matchIDs(ms), err)
	}
}

// A source that fails must not change the answer. With PyPI down, widget
// looks like npm's alone, though healthy it is ambiguous: a single-library
// context answers with an upstream error naming what it would have picked,
// not with npm's docs. A known project whose repository (or site) did not
// answer is only unverified: the pick stands, with a note.
func TestResolveFailedSourceIsNoAnswer(t *testing.T) {
	ctx := context.Background()
	pkgs := widgetPkgs()
	pkgs.errs = map[string]error{"pypi:widget": errors.New("pypi widget: status 503")}
	w := resolverWeb(widgetSites(), pkgs, widgetRepos(900, 300), nil)
	_, err := w.GetDocs(ctx, "widget", "configuration", 1000)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) ||
		!strings.Contains(err.Error(), "https://widgetjs.dev/") || !strings.Contains(err.Error(), "503") {
		t.Errorf("GetDocs(widget) with PyPI down = %v; want an upstream error naming the pick and the failure", err)
	}
	if _, err := w.Search(ctx, "widget configuration", 5); err == nil || errors.Is(err, ErrLibraryRequired) {
		t.Errorf("Search(widget configuration) with PyPI down = %v; want an upstream error", err)
	}
	// --resolve lists what answered, and says the list may be incomplete.
	ms, err := w.ResolveLibrary(ctx, "widget", 5)
	if err != nil || len(ms) != 1 || len(ms[0].Evidence) == 0 ||
		!strings.HasPrefix(ms[0].Evidence[0], "this list may be incomplete: pypi widget") {
		t.Errorf("ResolveLibrary(widget) with PyPI down = %+v, %v; want npm's widget, noting PyPI's failure first", ms, err)
	}

}

// Another project whose repository GitHub did not read (a 502) is
// unresolved, not absent: GitHub decides its claim, its homepage and its
// stars, so the pick depended on GitHub and is an upstream error naming
// it. --resolve still lists what verified, noting the gap. (A site that
// did not answer only adds a note: TestResolveHangingHomepageDoesNotSinkOthers.)
func TestResolveUnreadRepositoryIsNoAnswer(t *testing.T) {
	ctx := context.Background()
	pkgs := widgetPkgs()
	delete(pkgs.pkgs, "pypi:widget")
	pkgs.pkgs["crates:widget"] = pkgInfo{eco: ecoCrates, name: "widget", repoURL: "https://github.com/rusty/widget", repo: repo("rusty", "widget")}
	w := resolverWeb(widgetSites(), pkgs, nil, nil)
	w.repos = &failingRepo{fakeRepos: widgetRepos(900, 300), bad: repo("rusty", "widget")}
	_, err := w.resolveOne(ctx, mustSpec(t, "widget"), false)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) ||
		!strings.Contains(err.Error(), "crates widget") || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("resolveOne(widget), another repository unreadable = %v; want an upstream error naming it", err)
	}
	ms, err := w.ResolveLibrary(ctx, "widget", 5)
	if err != nil || len(ms) != 1 || len(ms[0].Evidence) == 0 || !strings.HasPrefix(ms[0].Evidence[0], "this list may be incomplete: github rusty/widget") {
		t.Errorf("ResolveLibrary(widget), a repository unreadable = %+v, %v; want npm's widget noting the gap", ms, err)
	}
}

// githubLimited answers GitHub's repository requests (its core quota) with
// the unauthenticated rate-limit refusal, and its search too when search
// is set; everything else goes through rt. A fresh install spends its 60
// core requests an hour first; the search quota (10 a minute) is apart.
type githubLimited struct {
	rt     http.RoundTripper
	search bool
}

func (g githubLimited) RoundTrip(req *http.Request) (*http.Response, error) {
	limited := req.URL.Host == "api.github.com" && (strings.HasPrefix(req.URL.Path, "/repos/") || g.search)
	if !limited {
		return g.rt.RoundTrip(req)
	}
	return searchLimited{rt: g.rt}.limit(req), nil
}

// A rate-limited GitHub must never let a squatter win (the no-token
// benchmark run). Without GitHub, npm's express, zod and gin cannot have
// their repository claims corroborated, so they verify nothing, while
// crates' same-named crates verify by their docs.rs docs alone. Where one
// library is needed that is an upstream error naming the token fix, never
// docs.rs; a bare query stops there rather than falling through; --resolve
// lists what verified, saying the list may be incomplete. Recorded live.
func TestResolveRateLimitedGitHubLetsNoSquatterWin(t *testing.T) {
	queries := map[string]string{"express": "express error handling middleware", "zod": "zod discriminated union", "gin": "gin bind json request body"}
	for _, search := range []bool{false, true} {
		for name, query := range queries {
			t.Run(fmt.Sprintf("%s/search-limited=%v", name, search), func(t *testing.T) {
				rateLimitedSquatters(t, name, query, search)
			})
		}
	}

	// With GitHub answering, express is Express.
	w, ft := fixtureWeb(t)
	w.pkgs = newPkgClient(&http.Client{Transport: notFoundFallback{ft}})
	w.repos = newGHClient(&http.Client{Transport: notFoundFallback{ft}}, nil)
	w.fetcher = &fixtureFetcher{client: &http.Client{Transport: notFoundFallback{ft}}}
	m, err := w.resolveOne(context.Background(), mustSpec(t, "express"), false)
	if err != nil || m.root.String() != "https://expressjs.com/" || m.project != "github:expressjs/express" {
		t.Errorf("resolveOne(express) with GitHub = %s (%s), %v; want expressjs.com", m.libraryMatch().ID, m.project, err)
	}
}

// rateLimitedSquatters checks one name of TestResolveRateLimitedGitHubLetsNoSquatterWin.
func rateLimitedSquatters(t *testing.T, name, query string, search bool) {
	t.Helper()
	w, ft := fixtureWeb(t)
	w.pkgs = newPkgClient(&http.Client{Transport: notFoundFallback{ft}})
	w.repos = newGHClient(&http.Client{Transport: githubLimited{notFoundFallback{ft}, search}}, nil)
	w.fetcher = &fixtureFetcher{client: &http.Client{Transport: notFoundFallback{ft}}}
	ctx := context.Background()
	check := func(what string, err error) {
		t.Helper()
		if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) {
			t.Fatalf("%s = %v; want an upstream error", what, err)
		}
		for _, want := range []string{"set github_token or gh auth login", "rate limited"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error lacks %q: %v", what, want, err)
			}
		}
	}
	m, err := w.resolveOne(ctx, mustSpec(t, name), false)
	if err == nil {
		t.Fatalf("resolveOne(%s) picked %s with GitHub rate-limited", name, m.root)
	}
	check("resolveOne("+name+")", err)
	_, err = w.Search(ctx, query, 5)
	check("Search("+query+")", err)

	ms, err := w.ResolveLibrary(ctx, name, 5)
	if err != nil {
		return // nothing verified at all: an upstream error is as safe
	}
	for _, lm := range ms {
		if !strings.HasPrefix(lm.Evidence[0], "this list may be incomplete") {
			t.Errorf("ResolveLibrary(%s) lists %s without saying the list may be incomplete: %v", name, lm.ID, lm.Evidence)
		}
	}
}

// A project whose stars GitHub failed to report blocks dominance even
// when its only docs are index-hosted: the rate limit says nothing about
// the project, which may be the popular one.
func TestDominanceBlockedByUnreadStars(t *testing.T) {
	m := func(project string, stars int, indexOnly, unread bool) webMatch {
		root, _ := parseWebRoot("https://" + project + ".dev/")
		return webMatch{root: root, project: project, stars: stars, indexOnly: indexOnly, starsUnread: unread}
	}
	if _, ok := dominant([]webMatch{m("a", 53000, false, false), m("b", -1, true, false)}); !ok {
		t.Errorf("an index-hosted squatter with no stars to read must not block dominance")
	}
	if _, ok := dominant([]webMatch{m("a", 53000, false, false), m("b", -1, true, true)}); ok {
		t.Errorf("index-hosted docs whose stars GitHub did not report must block dominance")
	}
}

// failingRepo is fakeRepos with one repository's lookup failing (a 502).
type failingRepo struct {
	*fakeRepos
	bad repoKey
}

func (f *failingRepo) repo(ctx context.Context, key repoKey) (repoInfo, error) {
	if key == f.bad {
		return repoInfo{}, errors.New("github: status 502")
	}
	return f.fakeRepos.repo(ctx, key)
}

// A bare query's preferred reading whose sources failed stops the search
// with an upstream error: "how do I set retries in requests" with PyPI
// down must not fall through to the docs of npm's "set".
func TestResolveQueryStopsAtUpstreamFailure(t *testing.T) {
	sites := &fakeFetcher{raw: map[string]fakeRaw{
		"https://set.dev/": html(`<!doctype html><html><head><title>set docs</title><meta name="generator" content="Docusaurus"></head><body><a href="/docs/retries">Retries</a><a href="https://github.com/set-org/set">GitHub</a></body></html>`),
	}}
	pkgs := &fakePkgs{
		pkgs: map[string]pkgInfo{"npm:set": {eco: ecoNPM, name: "set", homepage: "https://set.dev/", repoURL: "https://github.com/set-org/set", repo: repo("set-org", "set")}},
		errs: map[string]error{"pypi:requests": errors.New("pypi requests: status 503")},
	}
	repos := &fakeRepos{repos: map[repoKey]repoInfo{repo("set-org", "set"): {homepage: "https://set.dev", stars: 3}}}
	w := resolverWeb(sites, pkgs, repos, nil)
	_, err := w.Search(context.Background(), "how do I set retries in requests", 5)
	if err == nil || errors.Is(err, ErrLibraryRequired) || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), `"requests"`) || !strings.Contains(err.Error(), "503") {
		t.Errorf("Search = %v; want an upstream error about the requests reading", err)
	}
}

// A version root that does not answer in time is an upstream failure, not
// a cancellation: the deadline's error must not reach the surfaces, where
// it would read as [cancelled].
func TestResolveVersionRootTimeoutIsUpstream(t *testing.T) {
	w, ft := fixtureWeb(t)
	ft.Handle(http.MethodGet, "https://docs.djangoproject.com/en/5.2/", testutil.Fixture{Delay: time.Minute})
	w.probeTimeout = 50 * time.Millisecond
	_, err := w.resolve(context.Background(), mustSpec(t, "django@5.2"), 0)
	if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, ErrNotFound) {
		t.Errorf("resolve django@5.2, its version root hanging = %v; want a plain upstream error", err)
	}
}

// slowFetcher is a fakeFetcher whose URLs containing hang block until the
// call's deadline and whose URL slow takes delay first.
type slowFetcher struct {
	*fakeFetcher
	hang, slow string
	delay      time.Duration
}

func (f *slowFetcher) pause(ctx context.Context, u string) error {
	switch {
	case strings.Contains(u, f.hang):
		<-ctx.Done()
		return ctx.Err()
	case u == f.slow:
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (f *slowFetcher) FetchRaw(ctx context.Context, u string) (RawContent, error) {
	if err := f.pause(ctx, u); err != nil {
		return RawContent{}, err
	}
	return f.fakeFetcher.FetchRaw(ctx, u)
}

func (f *slowFetcher) FetchPage(ctx context.Context, u string) (PageContent, error) {
	if err := f.pause(ctx, u); err != nil {
		return PageContent{}, err
	}
	return f.fakeFetcher.FetchPage(ctx, u)
}

// One hanging homepage of an unrelated package does not sink a good one:
// its profile costs one probe budget (not one for the raw fetch and one
// more for FetchPage), and the good project's roots are verified while it
// hangs instead of after.
func TestResolveHangingHomepageDoesNotSinkOthers(t *testing.T) {
	f := &slowFetcher{fakeFetcher: gizmoFetcher(), hang: "junk.example", slow: "https://docs.gizmo.dev/", delay: 1200 * time.Millisecond}
	f.raw["https://gizmo.dev/"] = html(`<!doctype html><html><head><title>Gizmo</title></head><body><a href="https://docs.gizmo.dev/">Docs</a><a href="https://github.com/gizmo-org/gizmo">GitHub</a></body></html>`)
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:gizmo":    {eco: ecoNPM, name: "gizmo", homepage: "https://gizmo.dev/", repoURL: "https://github.com/gizmo-org/gizmo", repo: repo("gizmo-org", "gizmo")},
		"crates:gizmo": {eco: ecoCrates, name: "gizmo", homepage: "https://junk.example/"},
	}}
	repos := &fakeRepos{repos: map[repoKey]repoInfo{repo("gizmo-org", "gizmo"): {homepage: "https://gizmo.dev", stars: 40000}}}
	w := resolverWeb(f, pkgs, repos, nil)
	// The good root's verification (1.2s) overlaps the hanging homepage's
	// probe (2s); one after the other they would overrun the 2.8s budget.
	w.probeTimeout, w.resolveTimeout, w.candidateWait = 2*time.Second, 2800*time.Millisecond, 100*time.Millisecond
	// Where one library is needed, the pick stands, noting what was not
	// checked: a dead junk homepage must not sink the name.
	start := time.Now()
	m, err := w.resolveOne(context.Background(), mustSpec(t, "gizmo"), false)
	for _, c := range f.called() {
		if c == "page https://junk.example/" {
			t.Errorf("the hanging homepage was tried again through FetchPage after its probe budget ran out")
		}
	}
	if err != nil || m.root.String() != "https://docs.gizmo.dev/" ||
		!slices.ContainsFunc(m.libraryMatch().Evidence, func(e string) bool { return strings.Contains(e, "junk.example") }) {
		t.Errorf("resolveOne(gizmo) = %s %v, %v (after %s); want docs.gizmo.dev noting the unchecked homepage", m.libraryMatch().ID, m.libraryMatch().Evidence, err, time.Since(start))
	}
}

// searchLimited answers GitHub's repository search, and the repositories
// in limited, with its rate-limit refusal and everything else through rt.
type searchLimited struct {
	rt      http.RoundTripper
	limited []string // "owner/repo"
}

func (s searchLimited) RoundTrip(req *http.Request) (*http.Response, error) {
	repo, isRepo := strings.CutPrefix(req.URL.Path, "/repos/")
	limited := req.URL.Host == "api.github.com" && (req.URL.Path == "/search/repositories" || isRepo && slices.Contains(s.limited, repo))
	if !limited {
		return s.rt.RoundTrip(req)
	}
	return s.limit(req), nil
}

// limit is GitHub's unauthenticated rate-limit refusal of req.
func (searchLimited) limit(req *http.Request) *http.Response {
	body := `{"message":"API rate limit exceeded"}`
	return &http.Response{
		Status: "403 Forbidden", StatusCode: http.StatusForbidden, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1893456000"}, "Content-Type": {"application/json"}},
		Body:   io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: req,
	}
}

// GitHub's search allows 10 unauthenticated requests a minute, and nearly
// every bare query has a guessed reading (fastapi-dependency). A guess
// needs an exact registry package, so it never searches, and a search that
// fails for a reading whose name no registry has gives way to the next
// reading: the query still answers from fastapi. So does a rate-limited
// repository of a junk package the reading names (npm's production, PyPI's
// fastapi-dependency, recorded live), which hides only its homepage.
func TestSearchRateLimitDoesNotVetoBareQuery(t *testing.T) {
	w, ft := fixtureWeb(t)
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/production/latest", testutil.Fixture{Body: `{"name":"production","version":"0.0.2",
		"homepage":"http://github.com/qawemlilo/production","repository":{"type":"git","url":"git://github.com/qawemlilo/production.git"}}`})
	ft.Handle(http.MethodGet, "https://pypi.org/pypi/fastapi-dependency/json", testutil.Fixture{Body: `{"info":{"name":"fastapi-dependency","version":"0.2.0",
		"home_page":null,"project_urls":{"Homepage":"https://github.com/Kludex/fastapi-dependency"}}}`})
	rt := searchLimited{notFoundFallback{ft}, []string{"qawemlilo/production", "kludex/fastapi-dependency"}}
	w.pkgs = newPkgClient(&http.Client{Transport: rt})
	w.repos = newGHClient(&http.Client{Transport: rt}, nil)
	for _, q := range []string{"fastapi dependency injection", "how do I use fastapi in production"} {
		m, split, err := w.resolveQuery(context.Background(), q, splitLibraryQuery(q))
		if err != nil || m.root.String() != "https://fastapi.tiangolo.com/" || split.spec.name != "fastapi" {
			t.Errorf("resolveQuery(%q) = %s via %q, %v; want fastapi.tiangolo.com", q, m.libraryMatch().ID, split.spec.raw, err)
		}
	}
	for _, c := range ft.Calls() {
		if strings.Contains(c.String(), "search/repositories?q=fastapi-dependency") {
			t.Errorf("the guessed reading searched GitHub: %s", c)
		}
	}
}

// vetoes: which failed readings stop a bare query.
func TestVetoes(t *testing.T) {
	guess := querySplit{strict: true}
	plain := querySplit{}
	searchDown := &upstreamError{msg: "github search failed"}
	cases := []struct {
		name     string
		err      error
		s        querySplit
		readings int
		want     bool
	}{
		{"not found", fmt.Errorf("x: %w", ErrNotFound), plain, 3, false},
		{"a registry down", &upstreamError{registryFailed: true}, plain, 3, true},
		{"a registry down for a guess", &upstreamError{registryFailed: true}, guess, 3, true},
		{"a known package's site down", &upstreamError{nameKnown: true, siteFailed: true}, plain, 3, true},
		{"a known package's site down, for a guess", &upstreamError{nameKnown: true, siteFailed: true}, guess, 3, false},
		{"a known package, GitHub rate-limited", &upstreamError{nameKnown: true}, plain, 3, false},
		{"a known package, GitHub rate-limited, the only reading", &upstreamError{nameKnown: true}, plain, 1, true},
		{"search down for a guess", searchDown, guess, 3, false},
		{"search down for a name no registry has", searchDown, plain, 3, false},
		{"search down for the only reading", searchDown, plain, 1, true},
		{"an unclassified failure", errors.New("boom"), plain, 3, true},
	}
	for _, tc := range cases {
		if got := vetoes(tc.err, tc.s, tc.readings); got != tc.want {
			t.Errorf("%s: vetoes = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A package whose homepage is its README (or absent) still gets its
// repository's docs site: npm's webpack and puppeteer name their
// repositories, named like them and in JavaScript and TypeScript, whose
// homepages are webpack.js.org and pptr.dev. Recorded live.
func TestResolveRepositoryHomepageOfReadmePackages(t *testing.T) {
	cases := []struct{ spec, root, project string }{
		{"npm:webpack", "https://webpack.js.org/", "github:webpack/webpack"},
		{"webpack", "https://webpack.js.org/", "github:webpack/webpack"},
		{"npm:puppeteer", "https://pptr.dev/", "github:puppeteer/puppeteer"},
	}
	for _, tc := range cases {
		w, ft := fixtureWeb(t)
		w.pkgs = newPkgClient(&http.Client{Transport: notFoundFallback{ft}})
		w.repos = newGHClient(&http.Client{Transport: notFoundFallback{ft}}, nil)
		m, err := w.resolveOne(context.Background(), mustSpec(t, tc.spec), false)
		if err != nil || m.root.String() != tc.root || m.project != tc.project || m.stars < 1000 {
			t.Errorf("resolveOne(%s) = %s (%s, %d stars), %v; want %s for %s", tc.spec, m.libraryMatch().ID, m.project, m.stars, err, tc.root, tc.project)
		}
	}
}

// crates' anyhow names dtolnay/anyhow, a Rust repository named anyhow that
// lists no homepage: its stars count. So a same-named npm package with a
// site of its own and fewer stars no longer wins outright (with anyhow's
// stars unknown it did); the name is ambiguous.
func TestResolveAnyhowKeepsItsStars(t *testing.T) {
	w, ft := fixtureWeb(t)
	w.pkgs = newPkgClient(&http.Client{Transport: notFoundFallback{ft}})
	w.repos = newGHClient(&http.Client{Transport: notFoundFallback{ft}}, nil)
	m, err := w.resolveOne(context.Background(), mustSpec(t, "crates:anyhow"), false)
	if err != nil || m.root.String() != "https://docs.rs/anyhow/latest/anyhow/" || m.project != "github:dtolnay/anyhow" || m.stars != 6685 {
		t.Fatalf("resolveOne(crates:anyhow) = %s (%s, %d stars), %v", m.libraryMatch().ID, m.project, m.stars, err)
	}

	ft.Handle(http.MethodGet, "https://registry.npmjs.org/anyhow/latest", testutil.Fixture{Body: `{"name":"anyhow","version":"4.0.0",
		"homepage":"https://anyhow.dev/","repository":{"type":"git","url":"git+https://github.com/igoramadas/anyhow.git"}}`})
	ft.Handle(http.MethodGet, "https://api.github.com/repos/igoramadas/anyhow", testutil.Fixture{Body: `{"full_name":"igoramadas/anyhow",
		"homepage":"https://anyhow.dev","language":"TypeScript","stargazers_count":1500,"fork":false,"archived":false}`})
	ft.Handle(http.MethodGet, "https://anyhow.dev/", testutil.Fixture{Header: http.Header{"Content-Type": {"text/html"}},
		Body: `<!doctype html><html><head><title>Anyhow</title><meta name="generator" content="Docusaurus"></head><body><a href="/docs/intro">Docs</a><a href="https://github.com/igoramadas/anyhow">GitHub</a></body></html>`})
	// Its docs root, /docs/, has no page of its own: it is read through its
	// nav's /docs/intro.
	ft.Handle(http.MethodGet, "https://anyhow.dev/docs/intro", testutil.Fixture{Header: http.Header{"Content-Type": {"text/html"}},
		Body: `<!doctype html><html><head><title>Intro | Anyhow</title><meta name="generator" content="Docusaurus"></head><body><a href="/docs/intro">Intro</a><a href="/docs/api">API</a><a href="https://github.com/igoramadas/anyhow">GitHub</a></body></html>`})
	w.fetcher = &fixtureFetcher{client: &http.Client{Transport: notFoundFallback{ft}}}
	_, err = w.resolveOne(context.Background(), mustSpec(t, "anyhow"), false)
	if !errors.Is(err, ErrAmbiguousLibrary) {
		t.Errorf("resolveOne(anyhow) = %v; want the ambiguity between 6685 and 1500 stars", err)
	}
}

// The last-resort web search runs on the operator's own search backends,
// such as a SearXNG on localhost behind a redirect: the resolution's
// redirect check (publicHops), for third-party URLs, does not apply to it.
func TestLastResortSearchIsNotHopChecked(t *testing.T) {
	instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			http.Redirect(w, r, "/search/", http.StatusMovedPermanently) // loopback: not a public address
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"url":"https://docs.gizmo.dev/guide.html"}]}`))
	}))
	defer instance.Close()
	client := httpx.New(0, 1)
	w := NewWeb(&fakeFetcher{})
	w.sites = siteSearchFunc(func(ctx context.Context, _ string, _ int) ([]search.Result, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, instance.URL+"/search", nil)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		_ = resp.Body.Close()
		return []search.Result{{URL: "https://docs.gizmo.dev/guide.html"}}, nil
	})
	cands := w.lastResortSearch(publicHops(context.Background()), "gizmo")
	if len(cands) != 1 || cands[0].url.String() != "https://docs.gizmo.dev/" {
		t.Errorf("last-resort search through a redirecting local instance = %v, want docs.gizmo.dev", cands)
	}
}

// fixtureWeb404 is fixtureWeb with what no fixture covers answered as the
// services answer a name they do not have (notFoundFallback), and the
// version fixtures loaded too.
func fixtureWeb404(t *testing.T) (*Web, *testutil.FixtureTransport) {
	t.Helper()
	w, ft := fixtureWeb(t)
	if err := ft.LoadDir(filepath.Join("testdata", "versions")); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: notFoundFallback{ft}}
	w.pkgs = newPkgClient(client)
	w.repos = newGHClient(client, nil)
	w.fetcher = &fixtureFetcher{client: client}
	return w, ft
}

// tokio@1.38: docs.rs has 1.38.2, while tokio.rs, the project's site,
// publishes no versions and documents the latest, 1.54.1. The site is not
// taken for 1.38 for sharing its major version, so the version root and
// the root searched are the same: docs.rs's 1.38.2. Recorded live.
func TestResolveVersionedRootIsTheOneSearched(t *testing.T) {
	w, _ := fixtureWeb404(t)
	for _, spec := range []string{"crates:tokio@1.38", "tokio@1.38"} {
		m, err := w.resolveOne(context.Background(), mustSpec(t, spec), false)
		if err != nil || m.root.String() != "https://docs.rs/tokio/1.38.2/tokio/" || m.version != "1.38.2" {
			t.Errorf("resolveOne(%s) = %s %q, %v; want docs.rs's 1.38.2", spec, m.root, m.version, err)
		}
	}
	m, err := w.resolveOne(context.Background(), mustSpec(t, "crates:tokio@1"), false)
	if err != nil || m.root.String() != "https://tokio.rs/" || m.version != "1.54.1" {
		t.Errorf("resolveOne(crates:tokio@1) = %s %q, %v; want tokio.rs as the latest, 1.54.1", m.root, m.version, err)
	}
}

// Roots neither too deep nor too broad. PyPI's pydantic names a page,
// .../validation/latest/get-started/, and its repository's homepage
// redirects there too: the root ends at the version segment, keeping the
// product segment before it (pydantic.dev/docs/ holds other products).
// playwright.dev is a landing page whose Docs and API lead into /docs/
// (itself a 404, read through /docs/intro), beside its MCP, CLI and other
// languages' docs: npm's playwright is /docs/. Recorded live.
func TestResolveRootDepth(t *testing.T) {
	w, ft := fixtureWeb404(t)
	ft.Handle(http.MethodGet, "https://pydantic.dev/docs/validation", testutil.Fixture{Status: http.StatusFound, Header: http.Header{"Location": {"/docs/validation/latest/get-started/"}}})
	cases := []struct{ spec, root string }{
		{"pypi:pydantic", "https://pydantic.dev/docs/validation/latest/"},
		{"npm:playwright", "https://playwright.dev/docs/"},
	}
	for _, tc := range cases {
		m, err := w.resolveOne(context.Background(), mustSpec(t, tc.spec), false)
		if err != nil || m.root.String() != tc.root {
			t.Errorf("resolveOne(%s) = %s, %v; want %s", tc.spec, m.root, err, tc.root)
		}
	}
	for raw, want := range map[string]string{
		"https://pydantic.dev/docs/validation/latest/get-started/": "https://pydantic.dev/docs/validation/latest/",
		"https://requests.readthedocs.io/en/latest/":               "https://requests.readthedocs.io/en/latest/",
		"https://docs.python.org/3/library/":                       "https://docs.python.org/3/",
		"https://docs.rs/tokio/latest/tokio/":                      "https://docs.rs/tokio/latest/tokio/",
		"https://example.com/blog/2024/05/post/":                   "https://example.com/blog/2024/05/post/",
	} {
		if got := docsRootOf(mustURL(t, raw)).String(); got != want {
			t.Errorf("docsRootOf(%s) = %s, want %s", raw, got, want)
		}
	}
}

// A repository GitHub names that is in no registry looked up gets its own
// package in its language's registry: tokio-rs/axum, with no homepage, is
// crates' axum, whose docs.rs docs it has; go-chi/chi, a GitHub search hit
// for chi (npm's, PyPI's and crates' chi verify nothing), is the Go module
// github.com/go-chi/chi, whose pkg.go.dev page leads to its highest major
// version, v5. Recorded live.
func TestResolveRepositoryOwnPackage(t *testing.T) {
	w, _ := fixtureWeb404(t)
	cases := []struct{ spec, root, project string }{
		{"tokio-rs/axum", "https://docs.rs/axum/latest/axum/", "github:tokio-rs/axum"},
		{"chi", "https://pkg.go.dev/github.com/go-chi/chi/v5/", "github:go-chi/chi"},
	}
	for _, tc := range cases {
		m, err := w.resolveOne(context.Background(), mustSpec(t, tc.spec), false)
		if err != nil || m.root.String() != tc.root || m.project != tc.project {
			t.Errorf("resolveOne(%s) = %s (%s), %v; want %s for %s", tc.spec, m.root, m.project, err, tc.root, tc.project)
		}
	}
}

// A package of the repository's language that names another repository is
// not the repository's: no package is borrowed by name alone.
func TestRepoPackagesNeedTheClaim(t *testing.T) {
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"crates:gizmo": {eco: ecoCrates, name: "gizmo", repo: repo("squatter", "gizmo"), registryDocs: "https://docs.rs/gizmo/latest/gizmo/"},
	}}
	repos := &fakeRepos{repos: map[repoKey]repoInfo{repo("gizmo-org", "gizmo"): {language: "Rust", stars: 900}}}
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://docs.rs/gizmo/latest/gizmo/": html(`<!doctype html><html><head><title>gizmo - Rust</title></head><body><a href="https://github.com/squatter/gizmo">Repository</a></body></html>`),
	}}
	log := &resolveLog{}
	_, err := resolverWeb(f, pkgs, repos, nil).resolveLogged(context.Background(), mustSpec(t, "gizmo-org/gizmo"), 0, log)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolve gizmo-org/gizmo = %v; want nothing: crates' gizmo is another repository's", err)
	}
	assertReasons(t, log, "github gizmo-org/gizmo: crates gizmo names github.com/squatter/gizmo, not it")
}

// When the registries leave a bare name unsettled (cobra: COBRApy beside
// a crate, neither dominant), GitHub's search widens it: spf13/cobra, a Go
// repository in no registry asked, outweighs them both, its module and
// homepage verified.
func TestResolveUnsettledNameSearchesGitHub(t *testing.T) {
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"pypi:cobra":   {eco: ecoPyPI, name: "cobra", docsURL: "https://cobrapy.example/", repo: repo("opencobra", "cobrapy")},
		"crates:cobra": {eco: ecoCrates, name: "cobra", registryDocs: "https://docs.rs/cobra/latest/cobra/"},
		"go:github.com/spf13/cobra": {eco: ecoGo, name: "github.com/spf13/cobra", repo: repo("spf13", "cobra"),
			registryDocs: "https://pkg.go.dev/github.com/spf13/cobra"},
	}}
	repos := &fakeRepos{
		repos: map[repoKey]repoInfo{repo("opencobra", "cobrapy"): {homepage: "https://cobrapy.example", language: "Python", stars: 591}},
		hits:  map[string][]repoInfo{"cobra": {{key: repo("spf13", "cobra"), homepage: "https://cobra.example", language: "Go", stars: 44727}}},
	}
	page := func(title, repoURL string) fakeRaw {
		return html(`<!doctype html><html><head><title>` + title + `</title><meta name="generator" content="Sphinx"></head><body><a href="guide.html">Guide</a><a href="` + repoURL + `">Source</a></body></html>`)
	}
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://cobrapy.example/":                   page("cobra documentation", "https://github.com/opencobra/cobrapy"),
		"https://docs.rs/cobra/latest/cobra/":        page("cobra - Rust", "https://crates.io/crates/cobra"),
		"https://cobra.example/":                     page("Cobra docs", "https://github.com/spf13/cobra"),
		"https://pkg.go.dev/github.com/spf13/cobra/": page("cobra package", "https://github.com/spf13/cobra"),
	}}
	m, err := resolverWeb(f, pkgs, repos, nil).resolveOne(context.Background(), mustSpec(t, "cobra"), false)
	if err != nil || m.root.String() != "https://cobra.example/" || m.project != "github:spf13/cobra" {
		t.Errorf("resolveOne(cobra) = %s (%s), %v; want cobra.example for spf13/cobra", m.root, m.project, err)
	}
	if !slices.Contains(repos.calls, "search cobra") {
		t.Errorf("GitHub was not searched: %v", repos.calls)
	}
}

// A docs URL that has moved to another site is followed there when that
// site is corroborated as a project's docs: docs.python-requests.org
// redirects to requests.readthedocs.io, which links to psf/requests, whose
// GitHub homepage it is. A move to a site nothing corroborates is refused
// as before. Recorded live.
func TestDocsURLMovedToCorroboratedSite(t *testing.T) {
	w, _ := fixtureWeb404(t)
	ms, err := w.ResolveLibrary(context.Background(), "https://docs.python-requests.org", 5)
	if err != nil || len(ms) != 1 || ms[0].ID != "https://requests.readthedocs.io/en/latest/" {
		t.Fatalf("ResolveLibrary(docs.python-requests.org) = %+v, %v; want requests.readthedocs.io/en/latest/", ms, err)
	}
	if !strings.HasPrefix(ms[0].Evidence[0], "https://docs.python-requests.org/ moved to https://requests.readthedocs.io/en/latest/") {
		t.Errorf("evidence %v does not say the docs moved", ms[0].Evidence)
	}
	root, _ := parseWebRoot("https://docs.python-requests.org/")
	if moved, _, ok := w.movedRoot(context.Background(), root, nil); !ok || moved.String() != "https://requests.readthedocs.io/en/latest/" {
		t.Errorf("movedRoot = %s, %v", moved, ok)
	}

	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://elsewhere.example/": html(`<!doctype html><html><head><title>Docs</title></head><body><a href="/guide">Guide</a><a href="https://github.com/psf/requests">GitHub</a></body></html>`),
	}, redirects: map[string]string{"https://old-docs.example/": "https://elsewhere.example/"}}
	w2 := resolverWeb(f, nil, &fakeRepos{repos: map[repoKey]repoInfo{repo("psf", "requests"): {homepage: "https://requests.readthedocs.io"}}}, nil)
	old, _ := parseWebRoot("https://old-docs.example/")
	if moved, _, ok := w2.movedRoot(context.Background(), old, nil); ok {
		t.Errorf("a move to a site no repository names was followed to %s", moved)
	}
	// Nor into non-public address space, however it is dressed.
	f.raw["http://169.254.169.254/latest/"] = f.raw["https://elsewhere.example/"]
	f.redirects["https://old-docs.example/"] = "http://169.254.169.254/latest/"
	if moved, _, ok := w2.movedRoot(context.Background(), old, nil); ok {
		t.Errorf("a move into non-public space was followed to %s", moved)
	}
}

// beautifulsoup4 names its homepage on a path, which redirects to
// crummy.com/software/BeautifulSoup/; its Documentation link is
// bs4/doc/, kept as that path rather than the whole host, and the docs'
// title names it ("Beautiful Soup"). Recorded live.
func TestResolveHomepageOnAPath(t *testing.T) {
	w, _ := fixtureWeb404(t)
	m, err := w.resolveOne(context.Background(), mustSpec(t, "pypi:beautifulsoup4"), false)
	if err != nil || m.root.String() != "https://www.crummy.com/software/BeautifulSoup/bs4/doc/" {
		t.Fatalf("resolveOne(pypi:beautifulsoup4) = %s, %v; want crummy.com's bs4/doc/", m.root, err)
	}
	if !hasEvidence(m.evidence, evSiteNames) {
		t.Errorf("evidence %v: the title Beautiful Soup should name beautifulsoup4", m.evidence)
	}
}

// With stars unknown on both sides (GitHub rate-limited), a project with a
// docs site of its own lists before one whose docs only a package index
// hosts, whatever their trust: flask@2.3 without a token is refused on
// Flask's own docs, not on crates' flask.
func TestOwnSiteBeforeIndexHostedDocs(t *testing.T) {
	r := &resolution{spec: mustSpec(t, "flask")}
	site, _ := parseWebRoot("https://flask.palletsprojects.com/en/stable/")
	reg, _ := parseWebRoot("https://docs.rs/flask/latest/flask/")
	own := matchGroup{proj: &project{exact: true}, matches: []webMatch{{root: site, project: "pypi:flask", stars: -1, trust: 5}}}
	idx := matchGroup{proj: &project{exact: true}, matches: []webMatch{{root: reg, project: "crates:flask", stars: -1, trust: 6, indexOnly: true, registry: true}}}
	if r.compareGroups(own, idx) >= 0 || r.compareGroups(idx, own) <= 0 {
		t.Errorf("index-hosted docs ordered before a docs site of its own")
	}
}

// ginSearch is GitHub's recorded search for gin, the URL it answers.
const ginSearch = "https://api.github.com/search/repositories?q=gin+in%3Aname&sort=stars"

// ginSearchWithout is the recorded search for gin as GitHub answers a
// search it cut short: drop's hits missing and incomplete_results set.
func ginSearchWithout(t *testing.T, drop string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "resolve", "github", "api.github.com", "search", "repositories%3Fq=gin+in%3Aname&sort=stars"))
	if err != nil {
		t.Fatal(err)
	}
	var sr struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &sr); err != nil {
		t.Fatal(err)
	}
	sr.Items = slices.DeleteFunc(sr.Items, func(it map[string]any) bool { return it["full_name"] == drop })
	out, err := json.Marshal(map[string]any{"incomplete_results": true, "items": sr.Items})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// gin, as the round 1 benchmark met "gin bind json request body" twice with
// one binary. Recorded live. The registries have npm's and PyPI's small
// gins and crates' reserved one (docs.rs only); GitHub's search finds
// gin-gonic/gin (89439 stars, gin-gonic.com) and codegangsta/gin (4240, a
// live-reload tool documented only on pkg.go.dev), each its own Go module.
// Where one library is needed, none of the runs below answers from
// codegangsta/gin.
const ginQuery = "gin bind json request body"

// ginNoAnswer fails unless err says the resolution gave no answer.
func ginNoAnswer(t *testing.T, what string, m webMatch, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s answered from %s (%s); want no answer", what, m.root, m.project)
	}
}

// wantUpstream fails unless err is an upstream error (not a not-found, a
// missing library or an ambiguity) containing every want fragment.
func wantUpstream(t *testing.T, what string, err error, want ...string) {
	t.Helper()
	var amb *AmbiguousLibraryError
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) || errors.As(err, &amb) {
		t.Fatalf("%s = %v; want an upstream error", what, err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("%s: error lacks %q: %v", what, w, err)
		}
	}
}

// round1-1: everything answered. The name is ambiguous: 89439 stars are
// not 50x 4240 (dominant), so the three verified projects are listed, as
// the live run listed them.
func TestResolveGinAllAnswered(t *testing.T) {
	ctx := context.Background()
	w, _ := fixtureWeb404(t)
	m, err := w.resolveOne(ctx, mustSpec(t, "gin"), false)
	ginNoAnswer(t, "resolveOne(gin)", m, err)
	var amb *AmbiguousLibraryError
	if !errors.As(err, &amb) {
		t.Fatalf("resolveOne(gin) = %v; want ambiguous", err)
	}
	var ids []string
	for _, lm := range amb.Matches {
		ids = append(ids, lm.ID)
	}
	want := []string{"https://gin-gonic.com/", "https://pkg.go.dev/github.com/codegangsta/gin/", "https://docs.rs/gin/latest/gin/"}
	if !slices.Equal(ids, want) {
		t.Errorf("ambiguous between %v, want %v", ids, want)
	}
	if _, err = w.Search(ctx, ginQuery, 5); !errors.As(err, &amb) {
		t.Errorf("Search(%q) = %v; want ambiguous", ginQuery, err)
	}
}

// round1-2: GitHub's search answered without gin-gonic/gin, as it does
// when it runs out of time (incomplete_results): no module lookup or
// homepage for it, and the web search's gin-gonic.com linked to no project
// known. codegangsta/gin, dominating crates' gin, was the answer: the wrong
// project. That search failed, so the pick is an upstream error, and
// --resolve says its list may be incomplete.
func TestResolveGinPastIncompleteSearch(t *testing.T) {
	ctx := context.Background()
	w, ft := fixtureWeb404(t)
	ft.Handle(http.MethodGet, ginSearch, testutil.Fixture{Body: ginSearchWithout(t, "gin-gonic/gin")})
	w.sites = &fakeSites{hits: []search.Result{{URL: "https://gin-gonic.com/"}, {URL: "https://go.dev/doc/"}}}
	m, err := w.resolveOne(ctx, mustSpec(t, "gin"), false)
	ginNoAnswer(t, "resolveOne(gin)", m, err)
	wantUpstream(t, "resolveOne(gin)", err, "https://pkg.go.dev/github.com/codegangsta/gin/", `github search "gin": incomplete results`)
	_, err = w.Search(ctx, ginQuery, 5)
	wantUpstream(t, "Search("+ginQuery+")", err, "incomplete results")
	for _, c := range ft.Calls() {
		if strings.Contains(c.URL, "gin-gonic/gin/@latest") {
			t.Errorf("gin-gonic/gin was looked up though the search did not return it: %s", c)
		}
	}
	ms, err := w.ResolveLibrary(ctx, "gin", 5)
	if err != nil || len(ms) == 0 {
		t.Fatalf("ResolveLibrary(gin) = %v, %v; want what verified", ms, err)
	}
	for _, lm := range ms {
		if !strings.HasPrefix(lm.Evidence[0], `this list may be incomplete: github search "gin": incomplete results`) {
			t.Errorf("ResolveLibrary(gin) lists %s without saying the search was incomplete: %v", lm.ID, lm.Evidence)
		}
	}
}

// gin-gonic's side down instead (its homepage and its module lookup 503):
// gin-gonic/gin verifies nothing, and codegangsta/gin, with fewer stars and
// only index-hosted docs, must not stand past it either.
func TestResolveGinWithGinGonicDown(t *testing.T) {
	ctx := context.Background()
	w, ft := fixtureWeb404(t)
	ft.Handle(http.MethodGet, "https://gin-gonic.com/", testutil.Fixture{Status: http.StatusServiceUnavailable})
	ft.Handle(http.MethodGet, "https://proxy.golang.org/github.com/gin-gonic/gin/@latest", testutil.Fixture{Status: http.StatusServiceUnavailable})
	m, err := w.resolveOne(ctx, mustSpec(t, "gin"), false)
	ginNoAnswer(t, "resolveOne(gin)", m, err)
	wantUpstream(t, "resolveOne(gin)", err, "https://gin-gonic.com/", "503")
	_, err = w.Search(ctx, ginQuery, 5)
	wantUpstream(t, "Search("+ginQuery+")", err, "503")
}

// failingSite is a fakeFetcher whose URLs containing bad answer 503: a
// site down for a while, not gone.
type failingSite struct {
	*fakeFetcher
	bad string
}

func (f *failingSite) FetchRaw(ctx context.Context, u string) (RawContent, error) {
	if strings.Contains(u, f.bad) {
		return RawContent{}, fmt.Errorf("fetching %s: status 503", u)
	}
	return f.fakeFetcher.FetchRaw(ctx, u)
}

func (f *failingSite) FetchPage(ctx context.Context, u string) (PageContent, error) {
	if strings.Contains(u, f.bad) {
		return PageContent{}, fmt.Errorf("fetching %s: status 503", u)
	}
	return f.fakeFetcher.FetchPage(ctx, u)
}

// A pick with a docs site of its own does not stand past a project of the
// same name that could have outranked it, had its site answered: npm's
// gizmo (1500 stars) verifies, PyPI's (big/pygizmo, 60000 stars) is down
// for a while. That is an upstream error naming both, never npm's gizmo.
// A project that could not have outranked it (30 stars) leaves a note, as
// a junk package's dead homepage does (TestResolveHangingHomepageDoesNotSinkOthers).
func TestResolveFailedRivalIsNoAnswer(t *testing.T) {
	ctx := context.Background()
	f := &failingSite{fakeFetcher: &fakeFetcher{raw: map[string]fakeRaw{
		"https://gizmo.dev/": html(`<!doctype html><html><head><title>Gizmo docs</title><meta name="generator" content="Docusaurus"></head><body><a href="/docs/intro">Intro</a><a href="https://github.com/small/gizmo">GitHub</a></body></html>`),
	}}, bad: "pygizmo.example"}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{
		"npm:gizmo":  {eco: ecoNPM, name: "gizmo", homepage: "https://gizmo.dev/", repoURL: "https://github.com/small/gizmo", repo: repo("small", "gizmo")},
		"pypi:gizmo": {eco: ecoPyPI, name: "gizmo", homepage: "https://pygizmo.example/", repoURL: "https://github.com/big/gizmo", repo: repo("big", "gizmo")},
	}}
	resolveWith := func(bigStars int) (webMatch, error) {
		repos := &fakeRepos{repos: map[repoKey]repoInfo{
			repo("small", "gizmo"): {homepage: "https://gizmo.dev", language: "TypeScript", stars: 1500},
			repo("big", "gizmo"):   {homepage: "https://pygizmo.example", language: "Python", stars: bigStars},
		}}
		return resolverWeb(f, pkgs, repos, nil).resolveOne(ctx, mustSpec(t, "gizmo"), false)
	}

	m, err := resolveWith(60000)
	if err == nil {
		t.Fatalf("resolveOne(gizmo) picked %s past a stronger project that did not answer", m.root)
	}
	var amb *AmbiguousLibraryError
	if errors.Is(err, ErrNotFound) || errors.As(err, &amb) {
		t.Fatalf("resolveOne(gizmo) = %v; want an upstream error", err)
	}
	for _, want := range []string{"https://gizmo.dev/ (1500 stars)", "pypi gizmo (60000 stars)", "status 503"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}

	m, err = resolveWith(30)
	if err != nil || m.root.String() != "https://gizmo.dev/" ||
		!slices.ContainsFunc(m.libraryMatch().Evidence, func(e string) bool {
			return strings.Contains(e, "not checked") && strings.Contains(e, "pygizmo.example")
		}) {
		t.Errorf("resolveOne(gizmo), the weaker project down = %s %v, %v; want gizmo.dev noting what was not checked", m.libraryMatch().ID, m.libraryMatch().Evidence, err)
	}
}

// rivals: which projects that did not answer the pick cannot stand past.
func TestRivals(t *testing.T) {
	gone := &project{key: "github:gone/x", repo: repo("gone", "x"), repoGone: true}
	starred := func(n int) *project { return &project{key: fmt.Sprintf("github:o/x%d", n), info: &repoInfo{stars: n}} }
	none := &project{key: "pypi:x"} // no repository: stars unknown
	cases := []struct {
		name      string
		pick      webMatch
		failed    *project
		wantRival bool
	}{
		{"more stars (gin-gonic beside codegangsta)", webMatch{stars: 4240}, starred(89439), true},
		{"as many stars", webMatch{stars: 1500}, starred(1500), true},
		{"fewer stars", webMatch{stars: 1500}, starred(30), false},
		{"fewer stars, though the pick would not dominate them", webMatch{stars: 4240}, starred(3000), false},
		{"a junk package with no repository beside a starred pick", webMatch{stars: 40000}, none, false},
		{"no repository beside a pick with none either", webMatch{stars: -1}, none, true},
		{"a repository that is gone", webMatch{stars: 12}, gone, false},
		{"any project beside index-hosted docs", webMatch{stars: 90000, indexOnly: true}, starred(3), true},
	}
	for _, tc := range cases {
		got := rivals(tc.pick, []missedSource{{project: tc.failed.key, proj: tc.failed, err: errors.New("status 503")}})
		if (len(got) > 0) != tc.wantRival {
			t.Errorf("%s: rival = %v, want %v", tc.name, len(got) > 0, tc.wantRival)
		}
	}
}
