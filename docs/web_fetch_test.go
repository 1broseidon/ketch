package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1broseidon/ketch/httpx"
)

// The fallback fetcher (no injected Fetcher) reads llms.txt and Markdown
// pages end to end over plain HTTP.
func TestWebFallbackFetcherEndToEnd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/llms.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("# Local\n\n- [Routing](/guide/routing.md): File-based routes\n- [Home](/home)\n"))
	})
	mux.HandleFunc("/guide/routing.md", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.Write([]byte("# Routing\n\nRoutes come from files.\n\n## Dynamic routes\n\nUse [slug].md files for dynamic routes.\n"))
	})
	mux.HandleFunc("/home", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!doctype html><html><body>home</body></html>"))
	})
	mux.HandleFunc("/away", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://elsewhere.invalid/page", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	results, err := NewWeb(nil).GetDocs(context.Background(), srv.URL, "dynamic routes", 1000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	// The fallback fetcher cannot read the rendered page's targets, so the
	// section's slug is not vouched for: the result cites the page.
	if len(results) == 0 || results[0].URL != srv.URL+"/guide/routing" || results[0].Title != "Dynamic routes" {
		t.Fatalf("results = %+v", results)
	}

	f := newWebHTTPFetcher()
	if _, err := f.FetchRaw(context.Background(), srv.URL+"/missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("404 should wrap ErrNotFound: %v", err)
	}
	if _, err := f.FetchPage(context.Background(), srv.URL+"/home"); !errors.Is(err, errWebHTML) {
		t.Errorf("HTML through the fallback FetchPage must fail clearly: %v", err)
	}
	page, err := f.FetchPage(context.Background(), srv.URL+"/guide/routing.md")
	if err != nil || page.Title != "Routing" || !strings.HasPrefix(page.Markdown, "# Routing") || page.URL != srv.URL+"/guide/routing.md" {
		t.Errorf("Markdown page = %+v, %v", page, err)
	}
	if _, err := f.FetchRaw(context.Background(), srv.URL+"/away"); err == nil || !strings.Contains(err.Error(), "leaves") {
		t.Errorf("an off-host redirect must be refused: %v", err)
	}
}

func TestWebLooksLikeHTML(t *testing.T) {
	for _, tc := range []struct {
		body, ct string
		want     bool
	}{
		{"<!DOCTYPE html><html>", "text/plain", true},
		{"\ufeff<html lang=en>", "", true},
		{"<div>shell</div>", "text/html; charset=utf-8", true},
		{"# Title\n\ntext", "text/html", false},
		{"# Title", "text/markdown", false},
		{"<?xml version=\"1.0\"?><urlset/>", "application/xml", false},
	} {
		if got := looksLikeHTML([]byte(tc.body), tc.ct); got != tc.want {
			t.Errorf("looksLikeHTML(%q, %q) = %v, want %v", tc.body, tc.ct, got, tc.want)
		}
	}
}

// countingFetcher answers every URL after a delay, counting fetches per
// URL; a URL in slow waits for its context instead.
type countingFetcher struct {
	mu    sync.Mutex
	n     map[string]int
	slow  map[string]bool
	delay time.Duration
}

func (f *countingFetcher) count(url string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n[url]
}

func (f *countingFetcher) FetchRaw(ctx context.Context, url string) (RawContent, error) {
	f.mu.Lock()
	if f.n == nil {
		f.n = map[string]int{}
	}
	f.n[url]++
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return RawContent{}, fmt.Errorf("fetch %s: %w", url, err)
	}
	if f.slow[url] {
		<-ctx.Done()
		return RawContent{}, fmt.Errorf("fetch %s: %w", url, ctx.Err())
	}
	time.Sleep(f.delay)
	return RawContent{Body: []byte("body of " + url), ContentType: "text/plain", URL: url}, nil
}

func (f *countingFetcher) FetchPage(ctx context.Context, url string) (PageContent, error) {
	raw, err := f.FetchRaw(ctx, "page:"+url)
	return PageContent{Markdown: string(raw.Body), URL: url}, err
}

// Within one call a URL is fetched once, however many stages ask for it
// and however concurrently; outside a call every fetch goes through. A
// fetch whose redirects were not checked never answers one whose are.
func TestWebFetchMemoSharesOneCall(t *testing.T) {
	inner := &countingFetcher{delay: 20 * time.Millisecond}
	f := callFetcher{inner}
	ctx := withFetchMemo(context.Background())
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if raw, err := f.FetchRaw(ctx, "https://memo.test/llms.txt"); err != nil || string(raw.Body) != "body of https://memo.test/llms.txt" {
				t.Errorf("shared fetch = %q, %v", raw.Body, err)
			}
		})
	}
	wg.Wait()
	_, _ = f.FetchPage(ctx, "https://memo.test/guide")
	_, _ = f.FetchPage(ctx, "https://memo.test/guide")
	if n := inner.count("https://memo.test/llms.txt"); n != 1 {
		t.Errorf("8 concurrent asks made %d fetches, want 1", n)
	}
	if n := inner.count("page:https://memo.test/guide"); n != 1 {
		t.Errorf("2 page asks made %d fetches, want 1", n)
	}
	_, _ = f.FetchRaw(httpx.WithRedirectCheck(ctx, func(*url.URL) error { return nil }), "https://memo.test/llms.txt")
	if n := inner.count("https://memo.test/llms.txt"); n != 2 {
		t.Errorf("a checked fetch reused an unchecked one: %d fetches, want 2", n)
	}
	_, _ = f.FetchRaw(context.Background(), "https://memo.test/llms.txt")
	if n := inner.count("https://memo.test/llms.txt"); n != 3 {
		t.Errorf("a fetch outside a call was memoized: %d fetches, want 3", n)
	}
	if again := withFetchMemo(ctx); again != ctx {
		t.Error("a call's memo must not be replaced by a nested one")
	}
}

// A fetch that ran out of time answers a caller with no more than about
// the same time (two probes under one timeout are one probe), not one
// that can wait at least twice as long, and a cancelled fetch answers no
// one.
func TestWebFetchMemoTimeouts(t *testing.T) {
	inner := &countingFetcher{slow: map[string]bool{"https://memo.test/slow": true}}
	f := callFetcher{inner}
	ctx := withFetchMemo(context.Background())
	probe := func(d time.Duration) error {
		pctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		_, err := f.FetchRaw(pctx, "https://memo.test/slow")
		return err
	}
	if err := probe(30 * time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first probe: %v", err)
	}
	if err := probe(30 * time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || inner.count("https://memo.test/slow") != 1 {
		t.Errorf("a second probe under the same timeout fetched again (%d fetches): %v", inner.count("https://memo.test/slow"), err)
	}
	if err := probe(100 * time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || inner.count("https://memo.test/slow") != 2 {
		t.Errorf("a caller with longer to wait must ask again: %d fetches, %v", inner.count("https://memo.test/slow"), err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, _ = f.FetchRaw(cctx, "https://memo.test/cancelled")
	if raw, err := f.FetchRaw(ctx, "https://memo.test/cancelled"); err != nil || inner.count("https://memo.test/cancelled") != 2 || len(raw.Body) == 0 {
		t.Errorf("a cancelled fetch answered the next caller: %d fetches, %v", inner.count("https://memo.test/cancelled"), err)
	}
	if !reusable(nil, 0, 0) || !reusable(ErrNotFound, 0, time.Second) || reusable(context.Canceled, time.Second, time.Second) {
		t.Error("an answer is reusable, a cancellation never")
	}
}
