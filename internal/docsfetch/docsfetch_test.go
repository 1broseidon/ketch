package docsfetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/scrape"
	"github.com/1broseidon/ketch/urlrewrite"
)

const (
	llmsTxt = "# Example\n\n- [Authentication](https://docs.example.test/guide/auth): configure login\n"
	sitemap = `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://docs.example.test/guide/auth</loc></url></urlset>`
	mdPage  = "# Authentication\n\nUse `ketch login` to start a session.\n"
	docHTML = `<html><head><title>Configuring authentication</title></head><body><nav><a href="/">Home</a></nav><main><article><h1>Configuring authentication</h1><p>This guide explains how to configure authentication for a project, including login sessions and application settings.</p><p>Sessions expire after an hour of inactivity unless the remember-me option is enabled in the project settings.</p></article></main></body></html>`
)

// docsSite serves a small docs site and counts requests per path, so tests
// can tell a cache hit from a fetch.
type docsSite struct {
	*httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func newDocsSite(t *testing.T) *docsSite {
	t.Helper()
	site := &docsSite{hits: map[string]int{}}
	routes := map[string]struct{ contentType, body string }{
		"/llms.txt":          {"text/plain; charset=utf-8", llmsTxt},
		"/sitemap.xml":       {"application/xml", sitemap},
		"/guide/auth.md":     {"text/markdown; charset=utf-8", mdPage},
		"/guide/auth":        {"text/html; charset=utf-8", docHTML},
		"/guide/auth-mirror": {"text/html; charset=utf-8", docHTML},
	}
	site.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.hits[r.URL.Path]++
		site.mu.Unlock()
		route, ok := routes[r.URL.Path]
		switch {
		case r.URL.Path == "/retired":
			w.WriteHeader(http.StatusGone)
			return
		case r.URL.Path == "/overloaded":
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		case !ok:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", route.contentType)
		_, _ = w.Write([]byte(route.body))
	}))
	t.Cleanup(site.Close)
	return site
}

func (s *docsSite) hitCount(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

// newScraper builds a browserless scraper with optional URL rewrites, the
// way scrape.NewFromConfig would from url_rewrites.
func newScraper(t *testing.T, rules ...urlrewrite.Rule) *scrape.Scraper {
	t.Helper()
	rw, err := urlrewrite.NewRewriter(rules)
	if err != nil {
		t.Fatal(err)
	}
	s := scrape.NewWithRewriter("", rw)
	t.Cleanup(s.Close)
	return s
}

func newCache(t *testing.T) *cache.Cache {
	t.Helper()
	store, err := cache.NewBBoltStore(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	pc := cache.NewWithStore(store, time.Hour)
	t.Cleanup(pc.Close)
	return pc
}

func TestFetchRawReturnsBodyAndContentType(t *testing.T) {
	site := newDocsSite(t)
	f := New(newScraper(t), nil)
	cases := []struct{ path, contentType, body string }{
		{"/llms.txt", "text/plain; charset=utf-8", llmsTxt},
		{"/sitemap.xml", "application/xml", sitemap},
		{"/guide/auth.md", "text/markdown; charset=utf-8", mdPage},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			raw, err := f.FetchRaw(context.Background(), site.URL+tc.path)
			if err != nil {
				t.Fatalf("FetchRaw: %v", err)
			}
			if string(raw.Body) != tc.body {
				t.Errorf("body = %q, want the response bytes unextracted %q", raw.Body, tc.body)
			}
			if raw.ContentType != tc.contentType {
				t.Errorf("content type = %q, want %q", raw.ContentType, tc.contentType)
			}
			if raw.URL != site.URL+tc.path {
				t.Errorf("URL = %q, want the requested %q", raw.URL, site.URL+tc.path)
			}
		})
	}
}

func TestFetchRawReportsHTTPFailure(t *testing.T) {
	site := newDocsSite(t)
	f := New(newScraper(t), nil)
	raw, err := f.FetchRaw(context.Background(), site.URL+"/llms-full.txt")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("FetchRaw of a missing file = %q, %v; want an HTTP 404 error", raw.Body, err)
	}
}

// A 404 or 410 from either fetch is docs.ErrNotFound, the sentinel the web
// backend reads as "absent" and both surfaces report as not-found; the
// scrape status error stays reachable underneath. Any other status is a
// plain failure and must not be mistaken for a missing page.
func TestHTTPStatusClassification(t *testing.T) {
	site := newDocsSite(t)
	f := New(newScraper(t), newCache(t))
	fetches := map[string]func(url string) error{
		"FetchRaw":  func(url string) error { _, err := f.FetchRaw(context.Background(), url); return err },
		"FetchPage": func(url string) error { _, err := f.FetchPage(context.Background(), url); return err },
	}
	cases := []struct {
		path     string
		code     int
		notFound bool
	}{
		{"/docs/missing", http.StatusNotFound, true},
		{"/retired", http.StatusGone, true},
		{"/overloaded", http.StatusServiceUnavailable, false},
	}
	for name, fetch := range fetches {
		for _, tc := range cases {
			t.Run(name+tc.path, func(t *testing.T) {
				err := fetch(site.URL + tc.path)
				if err == nil {
					t.Fatal("want an error")
				}
				if got := errors.Is(err, docs.ErrNotFound); got != tc.notFound {
					t.Errorf("errors.Is(%v, docs.ErrNotFound) = %v, want %v", err, got, tc.notFound)
				}
				var status *scrape.HTTPStatusError
				if !errors.As(err, &status) || status.Code != tc.code {
					t.Errorf("error = %v, want the scrape status error for %d underneath", err, tc.code)
				}
				if !strings.HasPrefix(err.Error(), status.Error()) {
					t.Errorf("error text = %q, want it to lead with the scrape error %q", err, status)
				}
			})
		}
	}
}

// Failures that are not HTTP statuses pass through untouched: a bad rewrite
// is invalid input, not a missing page.
func TestNonStatusFailuresAreNotNotFound(t *testing.T) {
	f := New(newScraper(t, urlrewrite.Rule{Match: `^https://local\.example\.test/(.*)$`, Replace: "file:///$1"}), nil)
	_, err := f.FetchRaw(context.Background(), "https://local.example.test/llms.txt")
	if !errors.Is(err, scrape.ErrInvalidURL) || errors.Is(err, docs.ErrNotFound) {
		t.Fatalf("FetchRaw rewritten to file:// = %v, want scrape.ErrInvalidURL and not docs.ErrNotFound", err)
	}
}

// FetchRaw rewrites before it fetches, like every other fetch entry point,
// and refuses a rewrite that leaves the web.
func TestFetchRawAppliesRewrites(t *testing.T) {
	site := newDocsSite(t)
	f := New(newScraper(t,
		urlrewrite.Rule{Match: `^https://docs\.example\.test/(.*)$`, Replace: site.URL + "/$1"},
		urlrewrite.Rule{Match: `^https://local\.example\.test/(.*)$`, Replace: "file:///$1"},
	), nil)

	raw, err := f.FetchRaw(context.Background(), "https://docs.example.test/llms.txt")
	if err != nil {
		t.Fatalf("FetchRaw through a rewrite: %v", err)
	}
	if string(raw.Body) != llmsTxt || site.hitCount("/llms.txt") != 1 {
		t.Fatalf("rewritten fetch = %q after %d hits; want llms.txt from the rewrite target", raw.Body, site.hitCount("/llms.txt"))
	}
	if raw.URL != "https://docs.example.test/llms.txt" {
		t.Errorf("URL = %q, want the requested URL, not the rewrite target", raw.URL)
	}

	if _, err := f.FetchRaw(context.Background(), "https://local.example.test/etc/passwd"); !errors.Is(err, scrape.ErrInvalidURL) {
		t.Fatalf("FetchRaw rewritten to file:// = %v, want scrape.ErrInvalidURL", err)
	}
}

// A disabled cache reaches New as a nil *cache.Cache. Stored as-is in the
// PageCache interface it would be non-nil and the pipeline would call
// through it; New must turn it into a true nil and fetch every time.
func TestFetchPageWithNilCache(t *testing.T) {
	site := newDocsSite(t)
	var disabled *cache.Cache
	f := New(newScraper(t), disabled)
	if f.pages != nil {
		t.Fatalf("pages = %#v, want a nil interface for a nil *cache.Cache", f.pages)
	}

	for i := range 2 {
		page, err := f.FetchPage(context.Background(), site.URL+"/guide/auth")
		if err != nil {
			t.Fatalf("FetchPage %d: %v", i, err)
		}
		if page.Title != "Configuring authentication" {
			t.Errorf("title = %q, want the page title", page.Title)
		}
		if !strings.Contains(page.Markdown, "Sessions expire after an hour") || strings.Contains(page.Markdown, "<p>") {
			t.Errorf("markdown = %q, want the article converted to Markdown", page.Markdown)
		}
		if page.URL != site.URL+"/guide/auth" {
			t.Errorf("URL = %q, want the requested URL", page.URL)
		}
	}
	if got := site.hitCount("/guide/auth"); got != 2 {
		t.Fatalf("uncached FetchPage made %d requests for two calls, want 2", got)
	}
}

// FetchPage reads and writes the page cache under the scraper's key for the
// rewritten URL, so a docs page warmed by docs is a cache hit for scrape and
// the other way round.
func TestFetchPageUsesPageCache(t *testing.T) {
	site := newDocsSite(t)
	s := newScraper(t, urlrewrite.Rule{Match: `^https://docs\.example\.test/(.*)$`, Replace: site.URL + "/$1"})
	pc := newCache(t)
	f := New(s, pc)

	const pageURL = "https://docs.example.test/guide/auth"
	for i := range 2 {
		page, err := f.FetchPage(context.Background(), pageURL)
		if err != nil {
			t.Fatalf("FetchPage %d: %v", i, err)
		}
		if page.Title != "Configuring authentication" || !strings.Contains(page.Markdown, "remember-me option") {
			t.Fatalf("FetchPage %d = %+v; want the extracted article", i, page)
		}
		if page.URL != pageURL {
			t.Fatalf("FetchPage %d URL = %q, want %q (a cache hit must still say where it came from)", i, page.URL, pageURL)
		}
	}
	if got := site.hitCount("/guide/auth"); got != 1 {
		t.Fatalf("cached FetchPage made %d requests for two calls, want 1", got)
	}
	if page, _ := pc.Get(s.CacheKey(s.Rewrite(pageURL))); page == nil {
		t.Fatal("page cache has no entry under the scrape pipeline's key")
	}

	// A page scraped first is a cache hit for docs.
	if _, err := s.CachedScrape(context.Background(), pc, site.URL+"/guide/auth-mirror"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.FetchPage(context.Background(), site.URL+"/guide/auth-mirror"); err != nil {
		t.Fatal(err)
	}
	if got := site.hitCount("/guide/auth-mirror"); got != 1 {
		t.Fatalf("FetchPage after scrape made %d requests in total, want 1 (a cache hit)", got)
	}
}

// redirectSite serves docs pages plus redirects: same-host moves and a
// redirect off to a second server. It counts requests per path.
func redirectSite(t *testing.T) (site, offsite *docsSite) {
	t.Helper()
	offsite = &docsSite{hits: map[string]int{}}
	offsite.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offsite.mu.Lock()
		offsite.hits[r.URL.Path]++
		offsite.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docHTML))
	}))
	t.Cleanup(offsite.Close)
	site = &docsSite{hits: map[string]int{}}
	site.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.hits[r.URL.Path]++
		site.mu.Unlock()
		switch r.URL.Path {
		case "/guide/moved":
			http.Redirect(w, r, "/guide/auth", http.StatusMovedPermanently)
		case "/llms-moved.txt":
			http.Redirect(w, r, "/llms.txt", http.StatusFound)
		case "/guide/away":
			http.Redirect(w, r, offsite.URL+"/elsewhere", http.StatusFound)
		case "/llms.txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(llmsTxt))
		case "/guide/auth":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(docHTML))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.Close)
	return site, offsite
}

// Both fetches report where a response was finally served from, so the web
// backend can drop one that redirected out of its scope: the redirect
// target, off-host included, and on a page-cache hit the target recorded
// when the page was cached.
func TestFetchReportsWhereRedirectsEnd(t *testing.T) {
	site, offsite := redirectSite(t)
	f := New(newScraper(t), newCache(t))
	ctx := context.Background()

	raw, err := f.FetchRaw(ctx, site.URL+"/llms-moved.txt")
	if err != nil || raw.URL != site.URL+"/llms.txt" {
		t.Errorf("FetchRaw same-host redirect = %q, %v; want %s/llms.txt", raw.URL, err, site.URL)
	}
	raw, err = f.FetchRaw(ctx, site.URL+"/guide/away")
	if err != nil || raw.URL != offsite.URL+"/elsewhere" {
		t.Errorf("FetchRaw off-host redirect = %q, %v; want the off-host target", raw.URL, err)
	}
	for i := range 2 {
		page, err := f.FetchPage(ctx, site.URL+"/guide/moved")
		if err != nil || page.URL != site.URL+"/guide/auth" {
			t.Errorf("FetchPage %d same-host redirect = %q, %v; want %s/guide/auth", i, page.URL, err, site.URL)
		}
		page, err = f.FetchPage(ctx, site.URL+"/guide/away")
		if err != nil || page.URL != offsite.URL+"/elsewhere" {
			t.Errorf("FetchPage %d off-host redirect = %q, %v; want the off-host target", i, page.URL, err)
		}
	}
	if n := site.hitCount("/guide/moved"); n != 1 {
		t.Errorf("the second FetchPage made %d requests in total, want 1 (a cache hit)", n)
	}
}

// Behind a URL rewrite, a redirect's target is reported in the caller's
// terms when the rewrite maps it back exactly; one the rewrite cannot
// account for is reported as fetched, which the backend's scope check
// refuses.
func TestFetchMapsRedirectsBackThroughRewrites(t *testing.T) {
	site, offsite := redirectSite(t)
	f := New(newScraper(t, urlrewrite.Rule{Match: `^https://docs\.example\.test/(.*)$`, Replace: site.URL + "/$1"}), newCache(t))
	ctx := context.Background()

	if raw, err := f.FetchRaw(ctx, "https://docs.example.test/llms-moved.txt"); err != nil || raw.URL != "https://docs.example.test/llms.txt" {
		t.Errorf("FetchRaw behind a rewrite = %q, %v; want the target in the caller's terms", raw.URL, err)
	}
	for i := range 2 {
		if page, err := f.FetchPage(ctx, "https://docs.example.test/guide/moved"); err != nil || page.URL != "https://docs.example.test/guide/auth" {
			t.Errorf("FetchPage %d behind a rewrite = %q, %v; want the target in the caller's terms", i, page.URL, err)
		}
	}
	if page, err := f.FetchPage(ctx, "https://docs.example.test/guide/away"); err != nil || page.URL != offsite.URL+"/elsewhere" {
		t.Errorf("FetchPage off-host behind a rewrite = %q, %v; want the off-host URL as fetched", page.URL, err)
	}
}

// A cache entry written before the final URL was recorded cannot say where
// it came from. FetchPage fetches it again rather than vouch for it, and
// writes the fresh page back so later calls are cache hits.
func TestFetchPageRefreshesEntryWithoutFinalURL(t *testing.T) {
	site := newDocsSite(t)
	s := newScraper(t)
	pc := newCache(t)
	pageURL := site.URL + "/guide/auth"
	pc.Put(s.CacheKey(s.Rewrite(pageURL)), &scrape.Page{URL: pageURL, Title: "Stale", Markdown: "stale body"}, scrape.SourceHTTP)

	f := New(s, pc)
	for i := range 2 {
		page, err := f.FetchPage(context.Background(), pageURL)
		if err != nil || page.URL != pageURL || !strings.Contains(page.Markdown, "remember-me option") {
			t.Fatalf("FetchPage %d = %+v, %v; want the page fetched fresh", i, page, err)
		}
	}
	if n := site.hitCount("/guide/auth"); n != 1 {
		t.Errorf("made %d requests, want 1: the refreshed entry serves the second call", n)
	}
}
