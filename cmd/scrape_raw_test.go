package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/scrape"
	"github.com/1broseidon/ketch/urlrewrite"
	"github.com/spf13/cobra"
)

// staticHTML is a non-JS-shell page: detect returns "static", so the browser
// is never invoked and Source is SourceHTTP.
const staticHTML = `<!doctype html>
<html><head><title>Pricing</title></head>
<body><main><h1>Endpoint Pricing</h1><p>OpenAI gpt-4-turbo $10/$30 per 1M tokens</p></main></body></html>`

// newTestCache returns a cache backed by an isolated temp bbolt file so tests
// never touch the user's real cache dir.
func newTestCache(t *testing.T, ttl time.Duration) *cache.Cache {
	t.Helper()
	dir := t.TempDir()
	store, err := cache.NewBBoltStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test cache: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return cache.NewWithStore(store, ttl)
}

// staticServer serves staticHTML from every path and counts fetches.
func staticServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(staticHTML))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestRawEmitBareHTMLNoFrontMatter — the #11 regression: --raw emits HTML,
// not markdown, and plain output has no ---/url:/title: header.
func TestRawEmitBareHTMLNoFrontMatter(t *testing.T) {
	page := &scrape.Page{URL: "https://x.test/p", Title: "Pricing"}
	var buf bytes.Buffer
	if err := emitRaw(&buf, page, staticHTML, scrape.SourceHTTP, false, 0); err != nil {
		t.Fatalf("emitRaw: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(strings.TrimSpace(out), "<!doctype html>") {
		t.Errorf("--raw plain output must be HTML, got: %q", firstLine(out))
	}
	if strings.Contains(out, "---\nurl:") {
		t.Errorf("--raw plain output must not include the --- front-matter header, got: %q", out)
	}
	if !strings.Contains(out, "<title>Pricing</title>") {
		t.Errorf("--raw plain output lost the <title>, got: %q", out)
	}
}

// TestRawJSONIncludesSourceAndRawHTML — --raw --json shape: {url, fetched_url,
// title, source, raw_html}.
func TestRawJSONIncludesSourceAndRawHTML(t *testing.T) {
	page := &scrape.Page{URL: "https://x.test/p", FetchedURL: "https://x.test/p", Title: "Pricing"}
	var buf bytes.Buffer
	if err := emitRaw(&buf, page, staticHTML, scrape.SourceHTTP, true, 0); err != nil {
		t.Fatalf("emitRaw: %v", err)
	}
	var got rawJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %q)", err, buf.String())
	}
	if got.Source != scrape.SourceHTTP {
		t.Errorf("source = %q, want %q", got.Source, scrape.SourceHTTP)
	}
	if got.RawHTML != staticHTML {
		t.Errorf("raw_html mismatch")
	}
	if got.Title != "Pricing" {
		t.Errorf("title = %q", got.Title)
	}
}

// TestCachedScrapeRawReturnsHTMLAndHTTPSource — one fetch via the canonical
// ScrapeConditional path yields HTML labeled SourceHTTP; a static page does
// NOT spin up a browser.
func TestCachedScrapeRawReturnsHTMLAndHTTPSource(t *testing.T) {
	srv, hits := staticServer(t)
	s := scrape.NewWithRewriter("", nil)
	pc := newTestCache(t, time.Hour)

	page, rawHTML, source, err := cachedScrapeRaw(context.Background(), s, pc, srv.URL+"/pricing")
	if err != nil {
		t.Fatalf("cachedScrapeRaw: %v", err)
	}
	if source != scrape.SourceHTTP {
		t.Errorf("static page source = %q, want %q (browser must not spin up)", source, scrape.SourceHTTP)
	}
	if !strings.Contains(rawHTML, "<title>Pricing</title>") {
		t.Errorf("rawHTML missing <title>, got: %q", firstLine(rawHTML))
	}
	if page == nil || page.Title != "Pricing" {
		t.Errorf("page title = %q, want Pricing", pageOrEmpty(page))
	}
	if hits.Load() != 1 {
		t.Errorf("expected exactly 1 fetch, got %d", hits.Load())
	}
}

// TestCachedScrapeRawNoCacheBypassesCache — --no-cache (pc nil) returns the
// fresh fetch result without reading or writing the cache.
func TestCachedScrapeRawNoCacheBypassesCache(t *testing.T) {
	srv, hits := staticServer(t)
	s := scrape.NewWithRewriter("", nil)

	_, rawHTML, source, err := cachedScrapeRaw(context.Background(), s, nil, srv.URL+"/p")
	if err != nil {
		t.Fatalf("cachedScrapeRaw nil cache: %v", err)
	}
	if source != scrape.SourceHTTP {
		t.Errorf("source = %q, want %q", source, scrape.SourceHTTP)
	}
	if !strings.Contains(rawHTML, "<title>") {
		t.Errorf("nil-cache rawHTML missing <title>")
	}
	if hits.Load() != 1 {
		t.Errorf("expected 1 fetch, got %d", hits.Load())
	}
}

// TestCachedScrapeRawMarkdownOnlyEntryDoesNotPoisonRaw — the critical
// correctness test. A markdown-only cached entry (via cachedScrape/Put, which
// omits RawHTML) must NOT satisfy a later --raw request. GetRaw must miss,
// trigger a refetch, and back-fill RawHTML while preserving the existing Page.
func TestCachedScrapeRawMarkdownOnlyEntryDoesNotPoisonRaw(t *testing.T) {
	srv, hits := staticServer(t)
	s := scrape.NewWithRewriter("", nil)
	pc := newTestCache(t, time.Hour)

	// 1. Prime a markdown-only entry via the markdown path.
	if _, err := cachedScrape(context.Background(), s, pc, srv.URL+"/p"); err != nil {
		t.Fatalf("prime cachedScrape: %v", err)
	}
	primeHits := hits.Load()

	// 2. GetRaw must miss on a markdown-only entry (RawHTML empty).
	if raw, _, _ := pc.GetRaw(srv.URL + "/p"); raw != "" {
		t.Fatalf("markdown-only entry must not serve raw HTML, got %q", raw)
	}

	// 3. --raw must refetch and back-fill, NOT serve empty HTML.
	page, rawHTML, source, err := cachedScrapeRaw(context.Background(), s, pc, srv.URL+"/p")
	if err != nil {
		t.Fatalf("cachedScrapeRaw after markdown prime: %v", err)
	}
	if rawHTML == "" {
		t.Fatalf("rawHTML empty — markdown-only entry poisoned the --raw request")
	}
	if source != scrape.SourceHTTP {
		t.Errorf("source = %q, want %q", source, scrape.SourceHTTP)
	}
	if page == nil || page.Title != "Pricing" {
		t.Errorf("back-filled page title = %q, want Pricing", pageOrEmpty(page))
	}
	if hits.Load() != primeHits+1 {
		t.Errorf("expected a refetch (%d+1), got %d", primeHits, hits.Load())
	}

	// 4. The refetched entry now round-trips: a second --raw is served from
	// cache without a further fetch.
	secondBefore := hits.Load()
	if _, raw2, _, err := cachedScrapeRaw(context.Background(), s, pc, srv.URL+"/p"); err != nil {
		t.Fatalf("second cachedScrapeRaw: %v", err)
	} else if raw2 == "" {
		t.Fatalf("second --raw served empty HTML — back-fill did not persist")
	}
	if hits.Load() != secondBefore {
		t.Errorf("second --raw should hit the raw cache, expected %d fetches got %d", secondBefore, hits.Load())
	}
}

// TestCachedScrapeRawOldEntryWithoutRawFieldDecodes — an entry written before
// RawHTML existed (no `r` field) remains readable via the markdown Get path,
// and GetRaw treats it as a miss rather than erroring.
func TestCachedScrapeRawOldEntryWithoutRawFieldDecodes(t *testing.T) {
	srv, _ := staticServer(t)
	pc := newTestCache(t, time.Hour)

	// Simulate a pre-raw entry: Put writes markdown-only with no RawHTML.
	page, _, err := scrape.New().Scrape(context.Background(), srv.URL+"/p")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	pc.Put(srv.URL+"/p", page, scrape.SourceHTTP)

	// Markdown read still works.
	if got, _ := pc.Get(srv.URL + "/p"); got == nil {
		t.Fatalf("markdown Get returned nil on legacy entry")
	}
	// Raw read is a clean miss, not an error/panic.
	if raw, _, _ := pc.GetRaw(srv.URL + "/p"); raw != "" {
		t.Errorf("legacy entry GetRaw should miss, got %q", raw)
	}
}

// TestRawValidationRejectsSelectAndTrim — --raw combined with --select or
// --trim is a validation error.
func TestRawValidationRejectsSelectAndTrim(t *testing.T) {
	cases := []struct {
		name    string
		flags   []string
		wantSub string
	}{
		{"select", []string{"--raw", "--select", "main"}, "--raw cannot be combined with --select"},
		{"trim", []string{"--raw", "--trim"}, "--raw cannot be combined with --trim"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := staticServer(t)
			args := append([]string{"scrape", srv.URL}, c.flags...)
			cmd := buildScrapeCmd(args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			var ee *ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("expected *ExitError, got %T: %v", err, err)
			}
			if ee.Code != ExitValidation {
				t.Errorf("exit code = %d, want %d", ee.Code, ExitValidation)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q does not mention %q", err.Error(), c.wantSub)
			}
		})
	}
}

// TestRawSkipsLLMSTxtProbe — --raw must not trigger the llms.txt probe. We
// assert via fetch count on a bare-domain-style server: only the page fetch
// should occur, never /llms.txt.
func TestRawSkipsLLMSTxtProbe(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/llms.txt" {
			t.Errorf("--raw must skip the llms.txt probe, but it was requested")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(staticHTML))
	}))
	t.Cleanup(srv.Close)

	cmd := buildScrapeCmd([]string{"scrape", "--raw", "--no-cache", srv.URL + "/"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if hits.Load() == 0 {
		t.Errorf("expected at least the page fetch, got 0")
	}
}

// TestRawSelectAppliesRewrite — regression: --select must Rewrite before
// Fetch. A rewrite rule maps /src -> /dst; --select on the /src URL must hit
// the /dst server path and populate FetchedURL.
func TestRawSelectAppliesRewrite(t *testing.T) {
	var gotPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body><main id="x">picked</main></body></html>`))
	}))
	t.Cleanup(srv.Close)

	rw, err := urlrewrite.NewRewriter([]urlrewrite.Rule{{
		Match:   `^` + regexp.QuoteMeta(srv.URL) + `/src$`,
		Replace: srv.URL + "/dst",
	}})
	if err != nil {
		t.Fatalf("rewriter: %v", err)
	}
	s := scrape.NewWithRewriter("", rw)

	page, err := scrapeURLWithSelector(context.Background(), s, srv.URL+"/src", "#x", false)
	if err != nil {
		t.Fatalf("scrapeURLWithSelector: %v", err)
	}
	if p := gotPath.Load(); p != "/dst" {
		t.Errorf("--select fetched path %v, want /dst (rewrite not applied)", p)
	}
	if page.FetchedURL != srv.URL+"/dst" {
		t.Errorf("FetchedURL = %q, want %s/dst", page.FetchedURL, srv.URL)
	}
	if !strings.Contains(page.Markdown, "picked") {
		t.Errorf("selector extraction lost content, got %q", page.Markdown)
	}
}

// TestRawJSONPlainJSONExcludesRawHTML — plain --json (non-raw) must NOT
// include raw_html. We verify by encoding a Page directly through the
// markdown JSON path and asserting no raw_html field appears.
func TestRawJSONPlainJSONExcludesRawHTML(t *testing.T) {
	page := &scrape.Page{URL: "https://x.test/p", Title: "T", Markdown: "body"}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(page); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(buf.String(), "raw_html") {
		t.Errorf("plain --json must not include raw_html, got %q", buf.String())
	}
}

// buildScrapeCmd constructs an isolated cobra command tree for one scrape
// invocation, with --json available on the root and scrape flags registered.
// It does NOT touch package globals so parallel tests are safe.
func buildScrapeCmd(args []string) *cobraCommandShim {
	root := &cobra.Command{Use: "ketch", SilenceUsage: true}
	root.PersistentFlags().Bool("json", false, "output as JSON")
	scrapeCmd := &cobra.Command{
		Use:   "scrape",
		Short: "Scrape URLs and extract clean markdown",
		RunE:  runScrape,
	}
	scrapeCmd.Flags().Bool("raw", false, "output raw HTML instead of markdown")
	scrapeCmd.Flags().Bool("no-cache", false, "bypass the page cache")
	scrapeCmd.Flags().Int("max-chars", 0, "truncate output to N chars")
	scrapeCmd.Flags().Bool("trim", false, "strip markdown formatting")
	scrapeCmd.Flags().String("select", "", "CSS selector")
	scrapeCmd.Flags().Bool("no-llms-txt", false, "disable llms.txt detection")
	scrapeCmd.Flags().Int("concurrency", 5, "max concurrent requests")
	scrapeCmd.Flags().Bool("force-browser", false, "force browser render")
	root.AddCommand(scrapeCmd)
	root.SetArgs(args)
	return &cobraCommandShim{root: root}
}

type cobraCommandShim struct{ root *cobra.Command }

func (s *cobraCommandShim) Execute() error { return s.root.Execute() }

func pageOrEmpty(p *scrape.Page) string {
	if p == nil {
		return "<nil>"
	}
	return p.Title
}
