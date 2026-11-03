package scrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// One rule decides whether a cached page may be served, for every source.
func TestSourceServeFromCache(t *testing.T) {
	cases := []struct {
		src        Source
		configured bool
		want       bool
	}{
		{SourceHTTP, false, true},
		{SourceHTTP, true, true},
		{SourceBrowser, true, true},
		{SourceHTTPShell, false, false},
		{SourceHTTPShell, true, false},
		{SourceUnknown, false, true},
		{SourceUnknown, true, false},
	}
	for _, tc := range cases {
		if got := tc.src.ServeFromCache(tc.configured); got != tc.want {
			t.Errorf("%q.ServeFromCache(configured=%v) = %v, want %v", tc.src, tc.configured, got, tc.want)
		}
	}
}

// The raw path applies the same rule as the markdown path: an unrendered
// shell in the cache is fetched again, never served. It used to be served.
func TestCachedScrapeRawSkipsShellEntry(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Plain</title></head><body><main><h1>Plain page</h1><p>Real content here for the reader.</p></main></body></html>`))
	}))
	t.Cleanup(server.Close)

	s := New()
	pc := newMemCache()
	key := s.CacheKey(s.Rewrite(server.URL))
	pc.PutRaw(key, &Page{URL: server.URL, Markdown: "shell"}, SourceHTTPShell, "<div id=app></div>")

	page, _, source, err := s.CachedScrapeRaw(context.Background(), pc, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || source != SourceHTTP || page.Markdown == "shell" {
		t.Errorf("hits=%d source=%q markdown=%q: the shell entry was served", hits.Load(), source, page.Markdown)
	}
	if page.Source != SourceHTTP {
		t.Errorf("page.Source = %q, want %q", page.Source, SourceHTTP)
	}
}

// Cache validity follows configuration, not whether a launch worked in this
// process: a browser that failed to start leaves HasBrowser false, but a
// legacy entry is still not served, exactly as in a process whose launch
// worked.
func TestServeFromCacheIgnoresFailedLaunch(t *testing.T) {
	s := NewWithBrowser("/nonexistent/ketch-test-browser")
	captureStderr(t, func() { _ = s.getBrowser() })
	if s.HasBrowser() {
		t.Fatal("the failed launch should have cleared HasBrowser")
	}
	if !s.BrowserConfigured() {
		t.Fatal("BrowserConfigured must survive a failed launch")
	}
	if s.ServeFromCache(SourceUnknown) {
		t.Error("a legacy entry was served because this process's launch failed")
	}
}
