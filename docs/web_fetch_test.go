package docs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
