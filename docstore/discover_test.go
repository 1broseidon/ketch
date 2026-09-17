package docstore

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/scrape"
)

// site is a fake docs host. Paths map to (content-type, body); anything
// else is a 404.
type site struct {
	routes map[string][2]string
	srv    *httptest.Server
}

func newSite(t *testing.T) *site {
	t.Helper()
	s := &site{routes: map[string][2]string{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := s.routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", route[0])
		_, _ = w.Write([]byte(strings.ReplaceAll(route[1], "{{origin}}", s.srv.URL)))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *site) text(p, body string) *site {
	s.routes[p] = [2]string{"text/plain; charset=utf-8", body}
	return s
}
func (s *site) xml(p, body string) *site { s.routes[p] = [2]string{"application/xml", body}; return s }
func (s *site) html(p, title, body string) *site {
	s.routes[p] = [2]string{"text/html; charset=utf-8", fmt.Sprintf(`<!doctype html><html><head><title>%s</title></head><body><main><h1>%s</h1>%s</main></body></html>`, title, title, body)}
	return s
}
func (s *site) url(p string) string { return s.srv.URL + p }

func sitemapXML(paths ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, p := range paths {
		b.WriteString("<url><loc>{{origin}}" + p + "</loc></url>")
	}
	b.WriteString("</urlset>")
	return b.String()
}

func testScraper() *scrape.Scraper { return scrape.NewWithRewriter("", nil) }

func TestDiscoverPrefersLLMSFull(t *testing.T) {
	s := newSite(t).
		text("/llms-full.txt", strings.Repeat("# Docs\n\nlots of text here\n\n## Section\n\nmore\n", 10)).
		text("/llms.txt", "# Index\n- [a]({{origin}}/docs/a.md)").
		xml("/sitemap.xml", sitemapXML("/docs/a", "/docs/b"))
	plan, err := Discover(context.Background(), testScraper(), s.url("/docs"), DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != SourceLLMSFull || plan.SourceURL != s.url("/llms-full.txt") || plan.Prefix != "/docs" {
		t.Fatalf("plan = %+v", plan)
	}
}

// A short or structureless llms-full.txt (a stub, an error string) is not
// trusted; discovery moves on.
func TestDiscoverRejectsStubLLMSFull(t *testing.T) {
	s := newSite(t).
		text("/llms-full.txt", "coming soon").
		xml("/sitemap.xml", sitemapXML("/docs/a", "/docs/b", "/blog/x"))
	plan, err := Discover(context.Background(), testScraper(), s.url("/docs"), DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != SourceSitemap || len(plan.URLs) != 2 || plan.Candidates != 3 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestDiscoverLLMSLinkList(t *testing.T) {
	s := newSite(t).
		text("/llms.txt", "# Site\n\n- [A]({{origin}}/docs/a.md): first\n- [B]({{origin}}/docs/b.md)\n- [Blog]({{origin}}/blog/post.md)\n- [Other](https://other.test/x.md)\n")
	plan, err := Discover(context.Background(), testScraper(), s.url("/docs"), DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != SourceLLMS || plan.Candidates != 4 || len(plan.URLs) != 2 {
		t.Fatalf("plan = %+v urls=%v", plan, plan.URLs)
	}
	for _, u := range plan.URLs {
		if !strings.Contains(u, "/docs/") {
			t.Errorf("out-of-scope url kept: %s", u)
		}
	}
}

// llms.txt often indexes a parallel .md tree that the HTML docs prefix
// doesn't cover; when the prefix excludes every link, fall back to the host.
func TestDiscoverLLMSListFallsBackToHostWhenPrefixExcludesAll(t *testing.T) {
	s := newSite(t).
		text("/llms.txt", "- [A]({{origin}}/md/a.md)\n- [B]({{origin}}/md/b.md)\n")
	plan, err := Discover(context.Background(), testScraper(), s.url("/docs"), DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != SourceLLMS || len(plan.URLs) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestDiscoverRobotsSitemap(t *testing.T) {
	s := newSite(t).
		text("/robots.txt", "User-agent: *\nAllow: /\nSitemap: {{origin}}/sitemaps/pages.xml\n").
		xml("/sitemaps/pages.xml", sitemapXML("/docs/a", "/docs/b/c", "/pricing"))
	plan, err := Discover(context.Background(), testScraper(), s.url("/docs/"), DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != SourceSitemap || plan.SourceURL != s.url("/sitemaps/pages.xml") || len(plan.URLs) != 2 {
		t.Fatalf("plan = %+v urls=%v", plan, plan.URLs)
	}
}

func TestDiscoverSitemapIndex(t *testing.T) {
	s := newSite(t).
		xml("/sitemap.xml", `<?xml version="1.0"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>{{origin}}/s1.xml</loc></sitemap><sitemap><loc>{{origin}}/s2.xml</loc></sitemap></sitemapindex>`).
		xml("/s1.xml", sitemapXML("/docs/a")).
		xml("/s2.xml", sitemapXML("/docs/b", "/docs/a"))
	plan, err := Discover(context.Background(), testScraper(), s.url("/docs"), DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != SourceSitemap || len(plan.URLs) != 2 || plan.Candidates != 3 {
		t.Fatalf("plan = %+v urls=%v", plan, plan.URLs)
	}
}

// A sitemap that exists but never mentions the docs prefix must not win:
// the crawl fallback is the only way to reach those pages.
func TestDiscoverFallsThroughToCrawl(t *testing.T) {
	s := newSite(t).
		xml("/sitemap.xml", sitemapXML("/blog/a", "/blog/b")).
		html("/docs", "Docs", "<p>hi</p>")
	plan, err := Discover(context.Background(), testScraper(), s.url("/docs"), DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != SourceCrawl || plan.SourceURL != s.url("/docs") || plan.Prefix != "/docs" || plan.URLs != nil {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestDiscoverNothingFoundIsCrawl(t *testing.T) {
	s := newSite(t)
	plan, err := Discover(context.Background(), testScraper(), s.url("/"), DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source != SourceCrawl || plan.Prefix != "" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestDiscoverExplicitSeeds(t *testing.T) {
	s := newSite(t).
		text("/x/llms-full.txt", "# whatever").
		text("/x/llms.txt", "- [A]({{origin}}/x/a.md)").
		xml("/x/map.xml", sitemapXML("/x/a", "/y/b")).
		xml("/weird", sitemapXML("/x/a"))

	plan, err := Discover(context.Background(), testScraper(), s.url("/x/llms-full.txt"), DiscoverOptions{})
	if err != nil || plan.Source != SourceLLMSFull || plan.Prefix != "/x" {
		t.Fatalf("llms-full seed: %+v %v", plan, err)
	}
	plan, err = Discover(context.Background(), testScraper(), s.url("/x/llms.txt"), DiscoverOptions{})
	if err != nil || plan.Source != SourceLLMS || len(plan.URLs) != 1 {
		t.Fatalf("llms seed: %+v %v", plan, err)
	}
	plan, err = Discover(context.Background(), testScraper(), s.url("/x/map.xml"), DiscoverOptions{})
	if err != nil || plan.Source != SourceSitemap || len(plan.URLs) != 1 || plan.Candidates != 2 {
		t.Fatalf(".xml seed: %+v %v", plan, err)
	}
	// ForceSitemap on a URL that doesn't look like one, with the whole host.
	plan, err = Discover(context.Background(), testScraper(), s.url("/weird"), DiscoverOptions{ForceSitemap: true, Prefix: "/"})
	if err != nil || plan.Source != SourceSitemap || len(plan.URLs) != 1 || plan.Prefix != "" {
		t.Fatalf("forced sitemap seed: %+v %v", plan, err)
	}
	// An explicit sitemap whose URLs are all out of scope is an error, not a
	// silent crawl: the caller asked for that sitemap specifically.
	if _, err := Discover(context.Background(), testScraper(), s.url("/x/map.xml"), DiscoverOptions{Prefix: "/nowhere"}); err == nil {
		t.Fatal("expected error for explicit sitemap with nothing in scope")
	}
	if _, err := Discover(context.Background(), testScraper(), s.url("/missing.xml"), DiscoverOptions{}); err == nil {
		t.Fatal("expected error for missing explicit sitemap")
	}
}

func TestDiscoverPrefixOverride(t *testing.T) {
	s := newSite(t).xml("/sitemap.xml", sitemapXML("/docs/a", "/guide/b", "/blog/c"))
	plan, err := Discover(context.Background(), testScraper(), s.url("/docs"), DiscoverOptions{Prefix: "/guide"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Prefix != "/guide" || len(plan.URLs) != 1 || !strings.HasSuffix(plan.URLs[0], "/guide/b") {
		t.Fatalf("plan = %+v urls=%v", plan, plan.URLs)
	}
}

func TestDiscoverRejectsBadSeeds(t *testing.T) {
	for _, seed := range []string{"", "not a url", "ftp://x.test/docs", "/relative/path", "example.com/docs"} {
		if _, err := Discover(context.Background(), testScraper(), seed, DiscoverOptions{}); err == nil {
			t.Errorf("seed %q accepted", seed)
		}
	}
}

func TestDerivePrefix(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"/":                  "",
		"/docs":              "/docs",
		"/docs/":             "/docs",
		"/docs/installation": "/docs/installation",
		"/docs/index.html":   "/docs",
		"/index.html":        "",
		"/a/b/c/":            "/a/b/c",
	}
	for in, want := range cases {
		if got := derivePrefix(in); got != want {
			t.Errorf("derivePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInScope(t *testing.T) {
	cases := []struct {
		url, host, prefix string
		want              bool
	}{
		{"https://x.test/docs/a", "x.test", "/docs", true},
		{"https://x.test/docs", "x.test", "/docs", true},
		{"https://x.test/docs-old/a", "x.test", "/docs", false},
		{"https://x.test/blog", "x.test", "/docs", false},
		{"https://X.TEST/docs/a", "x.test", "/docs", true},
		{"https://other.test/docs/a", "x.test", "/docs", false},
		{"https://x.test/anything", "x.test", "", true},
		{"mailto:a@x.test", "x.test", "", false},
	}
	for _, c := range cases {
		if got := InScope(c.url, c.host, c.prefix); got != c.want {
			t.Errorf("InScope(%q, %q, %q) = %v", c.url, c.host, c.prefix, got)
		}
	}
}
