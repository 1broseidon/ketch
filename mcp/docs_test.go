package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/urlrewrite"
)

const docsPageHTML = `<html><head><title>Configuring authentication</title></head><body><main><article><h1>Configuring authentication</h1><p>This guide explains how to configure authentication for a project, including login sessions and application settings.</p><p>Sessions expire after an hour of inactivity unless the remember-me option is enabled.</p></article></main></body></html>`

// The docs backends' fetcher runs on the server's own scraper and page cache:
// the operator's URL rewrites apply, and a page fetched twice is fetched once.
func TestDocsDepsFetchThroughServerScraperAndCache(t *testing.T) {
	testutil.SetIsolatedConfigHome(t)
	var hits atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/llms.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("# Example docs\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPageHTML))
	}))
	t.Cleanup(site.Close)

	cfg := config.Defaults()
	cfg.URLRewrites = []urlrewrite.Rule{{Match: `^https://docs\.example\.test/(.*)$`, Replace: site.URL + "/$1"}}
	srv, err := NewServer(&cfg, "test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	if srv.cache == nil {
		t.Fatal("isolated server has no page cache")
	}
	ctx, _ := withTagDiagnostics(context.Background())
	fetcher := srv.docsDeps(ctx).Fetcher

	raw, err := fetcher.FetchRaw(ctx, "https://docs.example.test/llms.txt")
	if err != nil || string(raw.Body) != "# Example docs\n" || !strings.HasPrefix(raw.ContentType, "text/plain") || raw.URL != "https://docs.example.test/llms.txt" {
		t.Fatalf("FetchRaw = %+v, %v; want llms.txt through the configured rewrite, reported at the requested URL", raw, err)
	}

	before := hits.Load()
	for i := range 2 {
		page, err := srv.docsDeps(ctx).Fetcher.FetchPage(ctx, "https://docs.example.test/guide/auth")
		if err != nil {
			t.Fatalf("FetchPage %d: %v", i, err)
		}
		if page.Title != "Configuring authentication" || !strings.Contains(page.Markdown, "remember-me option") || page.URL != "https://docs.example.test/guide/auth" {
			t.Fatalf("FetchPage %d = %+v; want the extracted article at the requested URL", i, page)
		}
	}
	if got := hits.Load() - before; got != 1 {
		t.Fatalf("two FetchPage calls made %d requests, want 1 (the second a page-cache hit)", got)
	}
}

// A server whose cache directory could not be prepared has a nil page cache;
// docs fetches then run uncached instead of calling through a nil *cache.Cache.
func TestDocsDepsWithoutPageCache(t *testing.T) {
	testutil.SetIsolatedConfigHome(t)
	var hits atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPageHTML))
	}))
	t.Cleanup(site.Close)

	cfg := config.Defaults()
	srv, err := NewServer(&cfg, "test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	srv.cache = nil

	ctx := context.Background()
	for i := range 2 {
		if page, err := srv.docsDeps(ctx).Fetcher.FetchPage(ctx, site.URL+"/guide/auth"); err != nil || !strings.Contains(page.Markdown, "Sessions expire") {
			t.Fatalf("uncached FetchPage %d = %+v, %v", i, page, err)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("uncached FetchPage made %d requests for two calls, want 2", got)
	}
}
