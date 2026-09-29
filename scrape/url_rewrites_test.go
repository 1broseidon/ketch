package scrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/1broseidon/ketch/urlrewrite"
)

func TestScraperRewriteNoRewriterIsIdentity(t *testing.T) {
	s := New()
	got := s.Rewrite("https://example.com/x")
	if got != "https://example.com/x" {
		t.Errorf("Scraper without rewriter must be identity, got %q", got)
	}
}

func TestScraperRewriteAppliesRule(t *testing.T) {
	rw, err := urlrewrite.NewRewriter([]urlrewrite.Rule{
		{Match: `^https?://www\.reddit\.com/(.*)$`, Replace: "https://old.reddit.com/$1"},
	})
	if err != nil {
		t.Fatalf("NewRewriter: %v", err)
	}

	s := NewWithRewriter("", rw)
	got := s.Rewrite("https://www.reddit.com/r/golang")
	if got != "https://old.reddit.com/r/golang" {
		t.Errorf("Rewrite applied wrong result: %q", got)
	}
}

func TestScraperRewriteNoMatchReturnsOriginal(t *testing.T) {
	rw, _ := urlrewrite.NewRewriter([]urlrewrite.Rule{
		{Match: `^https?://foo\.com/.*$`, Replace: "https://bar.com/x"},
	})
	s := NewWithRewriter("", rw)
	got := s.Rewrite("https://example.com/x")
	if got != "https://example.com/x" {
		t.Errorf("No-match should return original, got %q", got)
	}
}

func TestScrapeAppliesRewriteAndPopulatesFetchedURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/old" {
			t.Errorf("server hit on unexpected path %q (rewrite did not apply)", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Old</title></head><body><p>hello world from old</p></body></html>`))
	}))
	defer srv.Close()

	original := srv.URL + "/new"
	rewritten := srv.URL + "/old"

	rules := []urlrewrite.Rule{{
		Match:   `^` + regexp.QuoteMeta(srv.URL) + `/new$`,
		Replace: srv.URL + "/old",
	}}
	rw, err := urlrewrite.NewRewriter(rules)
	if err != nil {
		t.Fatalf("rewriter: %v", err)
	}

	s := NewWithRewriter("", rw)

	page, _, err := s.Scrape(context.Background(), original)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	if page.URL != original {
		t.Errorf("Page.URL = %q, want original %q", page.URL, original)
	}
	if page.FetchedURL != rewritten {
		t.Errorf("Page.FetchedURL = %q, want %q", page.FetchedURL, rewritten)
	}
}

func TestScrapeIdentityWhenNoRewriteMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><p>hello</p></body></html>`))
	}))
	defer srv.Close()

	s := New()
	page, _, err := s.Scrape(context.Background(), srv.URL+"/foo")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	if page.URL != srv.URL+"/foo" {
		t.Errorf("Page.URL = %q", page.URL)
	}
	if page.FetchedURL != "" {
		t.Errorf("Page.FetchedURL should be empty when no rewrite applied, got %q", page.FetchedURL)
	}
}

func TestScrapeConditionalAppliesRewrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dst" {
			t.Errorf("ScrapeConditional hit unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>X</title></head><body><p>hi</p></body></html>`))
	}))
	defer srv.Close()

	rules := []urlrewrite.Rule{{
		Match:   `^` + regexp.QuoteMeta(srv.URL) + `/src$`,
		Replace: srv.URL + "/dst",
	}}
	rw, _ := urlrewrite.NewRewriter(rules)
	s := NewWithRewriter("", rw)

	result, err := s.ScrapeConditional(context.Background(), srv.URL+"/src", "", "")
	if err != nil {
		t.Fatalf("scrape conditional: %v", err)
	}
	if result.Page.URL != srv.URL+"/src" {
		t.Errorf("Page.URL = %q, want original", result.Page.URL)
	}
	if result.Page.FetchedURL != srv.URL+"/dst" {
		t.Errorf("Page.FetchedURL = %q, want rewritten", result.Page.FetchedURL)
	}
}

func TestScrapeAcceptsInputRewrittenToHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><main>rewritten input works</main></body></html>`))
	}))
	t.Cleanup(srv.Close)

	rw, err := urlrewrite.NewRewriter([]urlrewrite.Rule{{Match: `^docs$`, Replace: srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := NewWithRewriter("", rw).ScrapeMarkdown(t.Context(), nil, "docs", false)
	if err != nil {
		t.Fatalf("ScrapeMarkdown after rewrite: %v", err)
	}
	if page.FetchedURL != srv.URL {
		t.Fatalf("FetchedURL = %q, want %q", page.FetchedURL, srv.URL)
	}
}

func TestFetchLLMSTxtUsesRewrittenTarget(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("source host was fetched")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("source"))
	}))
	t.Cleanup(source.Close)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/llms.txt" {
			t.Errorf("target path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("target"))
	}))
	t.Cleanup(target.Close)

	rw, err := urlrewrite.NewRewriter([]urlrewrite.Rule{{Match: `^` + regexp.QuoteMeta(source.URL) + `$`, Replace: target.URL}})
	if err != nil {
		t.Fatal(err)
	}
	s := NewWithRewriter("", rw)
	content, ok := s.FetchLLMSTxt(t.Context(), source.URL)
	if !ok || content != "target" {
		t.Fatalf("FetchLLMSTxt = %q, %v, want target", content, ok)
	}
}

func TestFetchLLMSTxtRejectsInvalidRewrittenTarget(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("source host was fetched")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("source"))
	}))
	t.Cleanup(source.Close)
	rw, err := urlrewrite.NewRewriter([]urlrewrite.Rule{{Match: `^` + regexp.QuoteMeta(source.URL) + `$`, Replace: "file:///etc/passwd"}})
	if err != nil {
		t.Fatal(err)
	}
	content, ok := NewWithRewriter("", rw).FetchLLMSTxt(t.Context(), source.URL)
	if ok || content != "" {
		t.Fatalf("FetchLLMSTxt = %q, %v, want no shortcut", content, ok)
	}
}

func TestFetchLLMSTxtRewritesProbeURL(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("source probe URL was fetched")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("source"))
	}))
	t.Cleanup(source.Close)
	rw, err := urlrewrite.NewRewriter([]urlrewrite.Rule{{Match: `^` + regexp.QuoteMeta(source.URL) + `/llms\.txt$`, Replace: "file:///etc/passwd"}})
	if err != nil {
		t.Fatal(err)
	}
	if content, ok := NewWithRewriter("", rw).FetchLLMSTxt(t.Context(), source.URL); ok || content != "" {
		t.Fatalf("FetchLLMSTxt = %q, %v, want no shortcut", content, ok)
	}
}
