package docs

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/1broseidon/ketch/internal/testutil"
)

// sphinxFixtureFetcher serves FetchRaw from recorded fixtures over the
// fixture transport, following their redirects, and FetchPage from pages:
// what the scrape pipeline would make of a page.
type sphinxFixtureFetcher struct {
	raw   webHTTPFetcher
	pages map[string]fakePage
}

func (f sphinxFixtureFetcher) FetchRaw(ctx context.Context, u string) (RawContent, error) {
	return f.raw.FetchRaw(ctx, u)
}

func (f sphinxFixtureFetcher) FetchPage(_ context.Context, u string) (PageContent, error) {
	p, ok := f.pages[u]
	if !ok {
		return PageContent{}, fmt.Errorf("fixture: %s: status 404: %w", u, ErrNotFound)
	}
	return PageContent{Title: p.title, Markdown: p.markdown, URL: u}, nil
}

// newFixtureSiteWeb returns a backend reading the fixture directories under
// testdata (sphinx, versions) and the transport, for its calls.
func newFixtureSiteWeb(t *testing.T, pages map[string]fakePage, dirs ...string) (*Web, *testutil.FixtureTransport) {
	t.Helper()
	ft := testutil.NewFixtureTransport()
	for _, d := range dirs {
		if err := ft.LoadDir(filepath.Join("testdata", d)); err != nil {
			t.Fatal(err)
		}
	}
	return NewWeb(sphinxFixtureFetcher{raw: webHTTPFetcher{client: ft.Client()}, pages: pages}), ft
}

// readFixture returns a fixture file's bytes.
func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// testHTMLProfile reads an HTML page into a siteProfile the way the resolve
// core's parseProfile does, for the version sources: meta by lower-cased
// name or property, every anchor (absolute, no fragment, one per URL),
// script URLs and inline script text.
func testHTMLProfile(t *testing.T, page string, body []byte) *siteProfile {
	t.Helper()
	u, err := url.Parse(page)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	p := &siteProfile{requested: u, served: u, meta: map[string]string{}}
	p.title = collapseSpace(doc.Find("title").First().Text())
	doc.Find("meta").Each(func(_ int, m *goquery.Selection) {
		if k := strings.ToLower(firstNonEmpty(m.AttrOr("name", ""), m.AttrOr("property", ""))); k != "" {
			p.meta[k] = m.AttrOr("content", "")
		}
	})
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if l, ok := resolveRef(u, a.AttrOr("href", "")); ok && !seen[l.String()] {
			seen[l.String()] = true
			p.links = append(p.links, siteLink{url: l, text: collapseSpace(a.Text())})
		}
	})
	p.scripts = scriptURLs(doc, u)
	doc.Find("script:not([src])").Each(func(_ int, s *goquery.Selection) { p.inline += s.Text() + "\n" })
	return p
}

func TestDetectSphinx(t *testing.T) {
	page, _ := url.Parse("https://click.palletsprojects.com/en/stable/api/")
	cases := []struct {
		name               string
		generator          string
		scripts            []string
		wantRoot, wantOpts string
	}{
		{"documentation_options below the page", "", []string{"https://click.palletsprojects.com/en/stable/_static/doctools.js?v=1", "https://click.palletsprojects.com/en/stable/_static/documentation_options.js?v=2a4eea78"},
			"https://click.palletsprojects.com/en/stable/", "https://click.palletsprojects.com/en/stable/_static/documentation_options.js"},
		{"generator only", "Sphinx 7.4.7", nil, "https://click.palletsprojects.com/en/stable/api/", ""},
		{"script on another host does not count", "", []string{"https://cdn.example.com/en/stable/_static/documentation_options.js"}, "", ""},
		{"not Sphinx", "Docusaurus v3.9.2", []string{"https://click.palletsprojects.com/assets/js/main.js"}, "", ""},
		{"docutils alone is not Sphinx", "Docutils 0.21.2: https://docutils.sourceforge.io/", nil, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, ok := detectSphinx(page, c.generator, c.scripts)
			if c.wantRoot == "" {
				if ok {
					t.Fatalf("detected %+v", s)
				}
				return
			}
			if !ok || s.root.String() != c.wantRoot || s.options != c.wantOpts {
				t.Fatalf("detectSphinx = %v, %v, %q", ok, s.root, s.options)
			}
		})
	}
}

func TestParseDocOptions(t *testing.T) {
	click := parseDocOptions(readFixture(t, "sphinx/click.palletsprojects.com/en/stable/_static/documentation_options.js"))
	if click != (sphinxOptions{builder: "dirhtml", version: "8.5.1.dev0", linkSuffix: ".html"}) {
		t.Errorf("click (dirhtml) = %+v", click)
	}
	pytest := parseDocOptions(readFixture(t, "sphinx/docs.pytest.org/en/stable/_static/documentation_options.js"))
	if pytest != (sphinxOptions{builder: "html", version: "9.1", linkSuffix: ".html"}) {
		t.Errorf("pytest (html) = %+v", pytest)
	}
	// Sphinx 4 wrote var DOCUMENTATION_OPTIONS with URL_ROOT first and no
	// BUILDER; SOURCELINK_SUFFIX must not be read as LINK_SUFFIX.
	old := parseDocOptions([]byte(`var DOCUMENTATION_OPTIONS = {
    URL_ROOT: document.getElementById("documentation_options").getAttribute('data-url_root'),
    VERSION: "2.27.1",
    SOURCELINK_SUFFIX: '.txt',
    LINK_SUFFIX: '',
};`))
	if old != (sphinxOptions{version: "2.27.1", linkSuffix: ""}) {
		t.Errorf("old options = %+v", old)
	}
}

func TestParseObjectsInvHeader(t *testing.T) {
	project, version, ok := parseObjectsInvHeader(readFixture(t, "sphinx/click.palletsprojects.com/en/stable/objects.inv"))
	if !ok || project != "Click" || version != "8.5.x" {
		t.Errorf("click objects.inv = %q %q %v", project, version, ok)
	}
	crlf := []byte("# Sphinx inventory version 2\r\n# Project: Requests\r\n# Version: 2.34.2\r\n# The remainder of this file is compressed using zlib.\r\nx\x9c")
	if p, v, ok := parseObjectsInvHeader(crlf); !ok || p != "Requests" || v != "2.34.2" {
		t.Errorf("CRLF header = %q %q %v", p, v, ok)
	}
	for _, bad := range []string{
		"# Sphinx inventory version 1\n# Project: Old\n# Version: 1.0\nmod module x\n",
		"<!doctype html><html><body>Not found</body></html>",
		"# Sphinx inventory version 2\n# Project: Short\n",
	} {
		if p, v, ok := parseObjectsInvHeader([]byte(bad)); ok {
			t.Errorf("%q read as %q %q", bad, p, v)
		}
	}
}

func TestParseSearchIndex(t *testing.T) {
	idx, err := parseSearchIndex(readFixture(t, "sphinx/click.palletsprojects.com/en/stable/searchindex.js"))
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.docnames) != 37 || len(idx.titles) != 37 {
		t.Fatalf("%d docnames, %d titles", len(idx.docnames), len(idx.titles))
	}
	doc := slices.Index(idx.docnames, "testing")
	if idx.title(doc) != "Testing Click Applications" {
		t.Errorf("title = %q", idx.title(doc))
	}
	if got := idx.sections[doc]; !slices.Equal(got, []string{"Basic Example", "Capture modes", "Context Settings"}) {
		t.Errorf("sections = %q", got)
	}
	api := slices.Index(idx.docnames, "api")
	if got := idx.objects[api]; len(got) != sphinxMaxObjects || got[0] != "click.Abort" {
		t.Errorf("objects = %q", got)
	}

	// Sphinx 5 and older: bare keys, objects keyed by name.
	old := `Search.setIndex({docnames:["api","index"],envversion:{"sphinx.domains.python":3,sphinx:56},filenames:["api.rst","index.rst"],` +
		`objects:{"":{click:[0,0,0,"module-click"]},click:{Context:[0,1,1,"click.Context"],echo:[0,2,1,"click.echo"],_private:[0,2,-1,"x"]}},` +
		`objnames:{"0":["py","module","Python module"]},objtypes:{"0":"py:module"},terms:{"a, b":[0,1]},titles:["API","Welcome to Click"],titleterms:{api:0}});`
	idx, err = parseSearchIndex([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(idx.docnames, []string{"api", "index"}) || idx.title(1) != "Welcome to Click" {
		t.Errorf("old index = %+v", idx)
	}
	if got := idx.objects[0]; !slices.Equal(got, []string{"click", "click.Context", "click.echo"}) {
		t.Errorf("old objects = %q", got)
	}

	for _, bad := range []string{"", "Search.setIndex({})", "var x = 1;", "Search.setIndex({docnames:[)"} {
		if _, err := parseSearchIndex([]byte(bad)); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// The same real search index gives docname/ URLs for the dirhtml builder
// (click) and docname.html for the html builder (pytest's options), index
// being the root, and only pages under the docs root.
func TestSphinxLinksBuilders(t *testing.T) {
	index := string(readFixture(t, "sphinx/click.palletsprojects.com/en/stable/searchindex.js"))
	for _, c := range []struct {
		builder, options, want string
	}{
		{"dirhtml", "sphinx/click.palletsprojects.com/en/stable/_static/documentation_options.js", "https://docs.test/en/stable/testing/"},
		{"html", "sphinx/docs.pytest.org/en/stable/_static/documentation_options.js", "https://docs.test/en/stable/testing.html"},
	} {
		t.Run(c.builder, func(t *testing.T) {
			f := &fakeFetcher{raw: map[string]fakeRaw{
				"https://docs.test/en/stable/searchindex.js":                   plain(index),
				"https://docs.test/en/stable/_static/documentation_options.js": plain(string(readFixture(t, c.options))),
			}}
			root, _ := parseWebRoot("https://docs.test/en/stable/")
			s, ok := detectSphinx(root.u, "", []string{"https://docs.test/en/stable/_static/documentation_options.js?v=1"})
			if !ok {
				t.Fatal("not detected")
			}
			links, err := NewWeb(f).sphinxLinks(context.Background(), root, s)
			if err != nil {
				t.Fatal(err)
			}
			if len(links) != 37 {
				t.Fatalf("%d links", len(links))
			}
			byURL := map[string]webLink{}
			for _, l := range links {
				byURL[l.url.String()] = l
			}
			got, ok := byURL[c.want]
			if !ok || got.title != "Testing Click Applications" || !strings.Contains(got.description, "Capture modes") {
				t.Errorf("testing page = %+v (have %v)", got, slices.Sorted(maps.Keys(byURL)))
			}
			if l, ok := byURL["https://docs.test/en/stable/"]; !ok || l.title != "Welcome to Click" {
				t.Errorf("index should be the root, titled from the index: %+v", l)
			}
		})
	}

	// A root below the build root keeps only its pages.
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://docs.test/searchindex.js":                   plain(`Search.setIndex({"docnames":["index","guide/index","guide/intro","api"],"titles":["Home","Guide","Intro","API"]})`),
		"https://docs.test/_static/documentation_options.js": plain(`const DOCUMENTATION_OPTIONS = {BUILDER: 'dirhtml'};`),
	}}
	root, _ := parseWebRoot("https://docs.test/guide/")
	links, err := NewWeb(f).sphinxLinks(context.Background(), root, sphinxSite{root: &url.URL{Scheme: "https", Host: "docs.test", Path: "/"}})
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	for _, l := range links {
		urls = append(urls, l.url.String())
	}
	if !slices.Equal(urls, []string{"https://docs.test/guide/", "https://docs.test/guide/intro/"}) {
		t.Errorf("scoped links = %v", urls)
	}
}

// Read the Docs sitemaps list version roots only: click's lists
// /en/stable/, the root itself. Discovery goes on to the root page and the
// Sphinx search index, so a page the root page does not link is found.
func TestWebSphinxRTDSitemapFallsThrough(t *testing.T) {
	pages := map[string]fakePage{
		"https://click.palletsprojects.com/en/stable/testing/": {title: "Testing Click Applications", markdown: "# Testing Click Applications\n\n## Capture modes\n\nThe CliRunner captures output in several capture modes: stdout, stderr and both.\n"},
	}
	w, ft := newFixtureSiteWeb(t, pages, "sphinx")
	root, _ := parseWebRoot("https://click.palletsprojects.com/en/stable/")
	site, err := w.discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	checkClickLinks(t, root, site)

	for _, c := range ft.Calls() {
		if strings.Contains(c.URL, "/_/addons/") {
			t.Errorf("discovery asked for versions: %s", c)
		}
	}
}

// checkClickLinks checks what discovery found on the click fixture: the
// root page's links merged with the 37 pages its search index lists.
func checkClickLinks(t *testing.T, root webRoot, site webSite) {
	t.Helper()
	keys := map[string]webLink{}
	for _, l := range site.links {
		keys[l.url.String()] = l
	}
	if len(site.links) != 37 {
		t.Errorf("%d links, want the root page's merged with the 37 indexed pages", len(site.links))
	}
	if l := site.links[0]; l.url.String() != root.String() || l.title != "Welcome to Click" || !strings.Contains(l.description, "API Reference") {
		t.Errorf("the root page leads and gains the index's sections: %+v", l)
	}
	if l := keys["https://click.palletsprojects.com/en/stable/quickstart/"]; l.title != "Quickstart" || l.description == "" {
		t.Errorf("a root-page link gains the index's description: %+v", l)
	}
	if _, ok := keys["https://click.palletsprojects.com/en/stable/testing/"]; !ok {
		t.Error("a page only the search index lists is missing")
	}
	if !slices.Contains(site.tried, "https://click.palletsprojects.com/en/stable/searchindex.js") {
		t.Errorf("tried = %v", site.tried)
	}
}

// pkg.go.dev and docs.rs publish no llms.txt and only global sitemaps:
// discovery reads their root page and nothing else.
func TestWebRegistryHostsSkipProbes(t *testing.T) {
	for _, root := range []string{"https://pkg.go.dev/github.com/spf13/cobra", "https://docs.rs/tokio/latest/tokio/"} {
		t.Run(root, func(t *testing.T) {
			r, _ := parseWebRoot(root)
			ft := testutil.NewFixtureTransport()
			ft.Handle("GET", r.String(), testutil.Fixture{
				Header: map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
				Body:   `<!doctype html><html><head><title>cobra package</title></head><body><a href="` + root + `#Command">Command</a><a href="./sub/">sub</a></body></html>`,
			})
			w := NewWeb(webHTTPFetcher{client: ft.Client()})
			site, err := w.discover(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if len(site.links) == 0 {
				t.Error("the root page's links are missing")
			}
			for _, c := range ft.Calls() {
				if c.URL != r.String() {
					t.Errorf("unwanted request %s", c)
				}
			}
		})
	}
}

func TestUsefulSitemap(t *testing.T) {
	links := func(paths ...string) []webLink {
		var out []webLink
		for _, p := range paths {
			u, _ := url.Parse("https://requests.readthedocs.io" + p)
			out = append(out, webLink{url: u})
		}
		return out
	}
	host, _ := parseWebRoot("https://requests.readthedocs.io/")
	latest, _ := parseWebRoot("https://requests.readthedocs.io/en/latest/")
	for _, c := range []struct {
		root  webRoot
		links []webLink
		want  bool
	}{
		{host, links("/en/latest/", "/en/stable/"), false},
		{latest, links("/en/latest/"), false},
		{host, links("/", "/latest/", "/en/"), false},
		{latest, links("/en/latest/", "/en/latest/user/quickstart/"), true},
		{host, links("/docs/intro/"), true},
		{host, nil, false},
	} {
		if got := usefulSitemap(c.links, c.root); got != c.want {
			t.Errorf("usefulSitemap(%v under %s) = %v", c.links, c.root, got)
		}
	}
}
