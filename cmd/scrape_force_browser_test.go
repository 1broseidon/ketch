package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/scrape"
)

// renderedHTML is what the (fake) browser returns: it carries the pricing table
// that JS injects at runtime. The static fetch (staticHTML) lacks it — that gap
// is exactly issue #12. Has an id="prices" element for the --select test.
const renderedHTML = `<!doctype html>
<html><head><title>Pricing</title></head>
<body><main><h1>Endpoint Pricing</h1>
<table id="prices"><tr><td>gpt-4-turbo</td><td>$10 / $30 per 1M tokens</td></tr></table>
</main></body></html>`

// fakeBrowser is a BrowserConn that returns canned HTML and counts renders.
type fakeBrowser struct {
	html  string
	calls atomic.Int32
}

func (f *fakeBrowser) Fetch(_ context.Context, _ string) (string, error) {
	f.calls.Add(1)
	return f.html, nil
}

func (f *fakeBrowser) Close() {}

// TestForceBrowserMarkdownRendersUnconditionally — --force-browser markdown path
// renders via the browser even though staticHTML would detect as "static".
func TestForceBrowserMarkdownRendersUnconditionally(t *testing.T) {
	fb := &fakeBrowser{html: renderedHTML}
	s := scrape.NewWithBrowserConn(fb, nil)
	pc := newTestCache(t, time.Hour)

	page, err := cachedScrapeForce(context.Background(), s, pc, "https://x.test/p")
	if err != nil {
		t.Fatalf("cachedScrapeForce: %v", err)
	}
	if fb.calls.Load() != 1 {
		t.Errorf("browser renders = %d, want 1", fb.calls.Load())
	}
	if !strings.Contains(page.Markdown, "gpt-4-turbo") {
		t.Errorf("rendered markdown missing pricing content, got %q", page.Markdown)
	}
}

// TestForceBrowserRawEmitsRenderedHTML — --raw --force-browser emits the
// rendered HTML (with the table), labeled SourceBrowser, not the static shell.
func TestForceBrowserRawEmitsRenderedHTML(t *testing.T) {
	fb := &fakeBrowser{html: renderedHTML}
	s := scrape.NewWithBrowserConn(fb, nil)
	pc := newTestCache(t, time.Hour)

	page, rawHTML, source, err := cachedScrapeRawForce(context.Background(), s, pc, "https://x.test/p")
	if err != nil {
		t.Fatalf("cachedScrapeRawForce: %v", err)
	}
	if source != scrape.SourceBrowser {
		t.Errorf("source = %q, want %q", source, scrape.SourceBrowser)
	}
	if !strings.Contains(rawHTML, `id="prices"`) {
		t.Errorf("rendered raw HTML missing the table, got %q", firstLine(rawHTML))
	}
	if page == nil || page.Title != "Pricing" {
		t.Errorf("page title = %q, want Pricing", pageOrEmpty(page))
	}
	if fb.calls.Load() != 1 {
		t.Errorf("browser renders = %d, want 1", fb.calls.Load())
	}
}

// TestForceBrowserSelectAppliesToRenderedDOM — --select --force-browser runs the
// CSS selector against the rendered DOM, so it can reach JS-injected elements.
func TestForceBrowserSelectAppliesToRenderedDOM(t *testing.T) {
	fb := &fakeBrowser{html: renderedHTML}
	s := scrape.NewWithBrowserConn(fb, nil)

	page, err := scrapeURLWithSelector(context.Background(), s, "https://x.test/p", "#prices", true)
	if err != nil {
		t.Fatalf("scrapeURLWithSelector force: %v", err)
	}
	if !strings.Contains(page.Markdown, "gpt-4-turbo") {
		t.Errorf("selector on rendered DOM lost table content, got %q", page.Markdown)
	}
	if fb.calls.Load() != 1 {
		t.Errorf("browser renders = %d, want 1", fb.calls.Load())
	}
}

// TestForceBrowserWithoutBrowserIsPrecondition — --force-browser with no
// configured browser is an ExitPrecondition error, never a silent HTTP fallback.
func TestForceBrowserWithoutBrowserIsPrecondition(t *testing.T) {
	prev := cfg.Browser
	cfg.Browser = ""
	t.Cleanup(func() { cfg.Browser = prev })

	srv, _ := staticServer(t)
	cmd := buildScrapeCmd([]string{"scrape", "--force-browser", srv.URL})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected precondition error, got nil")
	}
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != ExitPrecondition {
		t.Errorf("exit code = %d, want %d", ee.Code, ExitPrecondition)
	}
	if !strings.Contains(err.Error(), "--force-browser requires a configured browser") {
		t.Errorf("error %q missing guidance", err.Error())
	}
}

// TestForceBrowserIgnoresHTTPCacheEntry — the #12 anti-poisoning guard. A
// pre-existing SourceHTTP (markdown-only) cache entry must NOT satisfy a
// --force-browser request; it re-renders via the browser.
func TestForceBrowserIgnoresHTTPCacheEntry(t *testing.T) {
	fb := &fakeBrowser{html: renderedHTML}
	s := scrape.NewWithBrowserConn(fb, nil)
	pc := newTestCache(t, time.Hour)

	// Prime a stale SourceHTTP entry (the unrendered static page).
	pc.Put("https://x.test/p", &scrape.Page{URL: "https://x.test/p", Title: "Pricing", Markdown: "stale http content"}, scrape.SourceHTTP)

	page, err := cachedScrapeForce(context.Background(), s, pc, "https://x.test/p")
	if err != nil {
		t.Fatalf("cachedScrapeForce: %v", err)
	}
	if fb.calls.Load() != 1 {
		t.Errorf("browser renders = %d, want 1 (HTTP entry must not satisfy force)", fb.calls.Load())
	}
	if strings.Contains(page.Markdown, "stale http content") {
		t.Errorf("force-browser served the stale SourceHTTP entry: %q", page.Markdown)
	}
	if !strings.Contains(page.Markdown, "gpt-4-turbo") {
		t.Errorf("expected freshly rendered content, got %q", page.Markdown)
	}
}

// TestForceBrowserReusesBrowserCacheEntry — Position A: a forced result cached
// as SourceBrowser is re-read on a subsequent forced run without re-rendering.
func TestForceBrowserReusesBrowserCacheEntry(t *testing.T) {
	fb := &fakeBrowser{html: renderedHTML}
	s := scrape.NewWithBrowserConn(fb, nil)
	pc := newTestCache(t, time.Hour)

	if _, err := cachedScrapeForce(context.Background(), s, pc, "https://x.test/p"); err != nil {
		t.Fatalf("first cachedScrapeForce: %v", err)
	}
	if fb.calls.Load() != 1 {
		t.Fatalf("after first call renders = %d, want 1", fb.calls.Load())
	}

	if _, err := cachedScrapeForce(context.Background(), s, pc, "https://x.test/p"); err != nil {
		t.Fatalf("second cachedScrapeForce: %v", err)
	}
	if fb.calls.Load() != 1 {
		t.Errorf("second call re-rendered (renders = %d); SourceBrowser entry should be reused", fb.calls.Load())
	}
}

// TestForceBrowserSkipsLLMSTxtProbe — under --force-browser the bare-domain
// /llms.txt probe is skipped; the browser render is authoritative.
func TestForceBrowserSkipsLLMSTxtProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" {
			t.Errorf("--force-browser must skip the llms.txt probe, but it was requested")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(staticHTML))
	}))
	t.Cleanup(srv.Close)

	fb := &fakeBrowser{html: renderedHTML}
	s := scrape.NewWithBrowserConn(fb, nil)
	pc := newTestCache(t, time.Hour)

	page, _, _, err := scrapeOneURL(context.Background(), s, pc, srv.URL+"/", false, "", false, true)
	if err != nil {
		t.Fatalf("scrapeOneURL force: %v", err)
	}
	if !strings.Contains(page.Markdown, "gpt-4-turbo") {
		t.Errorf("expected rendered content, got %q", page.Markdown)
	}
	if fb.calls.Load() != 1 {
		t.Errorf("browser renders = %d, want 1", fb.calls.Load())
	}
}
