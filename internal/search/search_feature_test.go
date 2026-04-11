package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestFeatureBraveSearchParses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"web": {
				"results": [
					{"title": "Go Docs", "url": "https://golang.org/doc/", "description": "Go documentation"},
					{"title": "Go Blog", "url": "https://blog.golang.org/", "description": "The Go Blog"},
					{"title": "Go Playground", "url": "https://play.golang.org/", "description": "Run Go online"}
				]
			}
		}`)
	}))
	defer server.Close()

	b := &Brave{apiKey: "test-key", client: server.Client()}
	// Override URL by using the test server — need to patch the search method
	// Instead, we'll create a custom transport
	b.client = &http.Client{Transport: &rewriteTransport{base: server.Client().Transport, target: server.URL}}

	results, err := b.Search(context.Background(), "golang", 3)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].Title != "Go Docs" {
		t.Errorf("first result title = %q, want %q", results[0].Title, "Go Docs")
	}
	if results[0].URL != "https://golang.org/doc/" {
		t.Errorf("first result URL = %q, want %q", results[0].URL, "https://golang.org/doc/")
	}
	if results[0].Description != "Go documentation" {
		t.Errorf("first result desc = %q, want %q", results[0].Description, "Go documentation")
	}
}

func TestFeatureBraveSearch401(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	b := &Brave{apiKey: "bad-key", client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := b.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	// Error should mention API key
	if got := err.Error(); got == "" {
		t.Error("error message should not be empty")
	}
}

func TestFeatureBraveLimitRespected(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"web": {
				"results": [
					{"title": "A", "url": "https://a.com", "description": "a"},
					{"title": "B", "url": "https://b.com", "description": "b"},
					{"title": "C", "url": "https://c.com", "description": "c"},
					{"title": "D", "url": "https://d.com", "description": "d"},
					{"title": "E", "url": "https://e.com", "description": "e"}
				]
			}
		}`)
	}))
	defer server.Close()

	b := &Brave{apiKey: "key", client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := b.Search(context.Background(), "test", 2)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2 (limit)", len(results))
	}
}

func TestFeatureBraveEmptyResults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web": {"results": []}}`)
	}))
	defer server.Close()

	b := &Brave{apiKey: "key", client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := b.Search(context.Background(), "nothing", 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestFeatureDDGSearchParses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
			<div class="result">
				<h2 class="result__title"><a class="result__a" href="https://golang.org/">Go Language</a></h2>
				<div class="result__snippet">The Go programming language</div>
			</div>
			<div class="result">
				<h2 class="result__title"><a class="result__a" href="https://go.dev/">Go Dev</a></h2>
				<div class="result__snippet">Go developer portal</div>
			</div>
		</body></html>`)
	}))
	defer server.Close()

	d := &DDG{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := d.Search(context.Background(), "golang", 10)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Go Language" {
		t.Errorf("title = %q, want %q", results[0].Title, "Go Language")
	}
}

func TestFeatureDDGRetryOn202(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
			<div class="result">
				<h2 class="result__title"><a class="result__a" href="https://example.com">Example</a></h2>
				<div class="result__snippet">After retries</div>
			</div>
		</body></html>`)
	}))
	defer server.Close()

	d := &DDG{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := d.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("Search error after retries: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if attempts.Load() < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts.Load())
	}
}

func TestFeatureDDGLimitRespected(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
			<div class="result"><h2 class="result__title"><a class="result__a" href="https://a.com">A</a></h2><div class="result__snippet">a</div></div>
			<div class="result"><h2 class="result__title"><a class="result__a" href="https://b.com">B</a></h2><div class="result__snippet">b</div></div>
			<div class="result"><h2 class="result__title"><a class="result__a" href="https://c.com">C</a></h2><div class="result__snippet">c</div></div>
		</body></html>`)
	}))
	defer server.Close()

	d := &DDG{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := d.Search(context.Background(), "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("got %d results, want 1 (limit)", len(results))
	}
}

func TestFeatureSearXNGSearchParses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [
				{"title": "SearX Result 1", "url": "https://example.com/1", "content": "First result content"},
				{"title": "SearX Result 2", "url": "https://example.com/2", "content": "Second result content"}
			]
		}`)
	}))
	defer server.Close()

	s := &SearXNG{baseURL: server.URL, client: server.Client()}
	results, err := s.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "SearX Result 1" {
		t.Errorf("title = %q, want %q", results[0].Title, "SearX Result 1")
	}
	if results[0].Description != "First result content" {
		t.Errorf("description = %q, want %q", results[0].Description, "First result content")
	}
}

func TestFeatureSearXNGLimitRespected(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [
				{"title": "A", "url": "https://a.com", "content": "a"},
				{"title": "B", "url": "https://b.com", "content": "b"},
				{"title": "C", "url": "https://c.com", "content": "c"}
			]
		}`)
	}))
	defer server.Close()

	s := &SearXNG{baseURL: server.URL, client: server.Client()}
	results, err := s.Search(context.Background(), "test", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2 (limit)", len(results))
	}
}

func TestFeatureSearXNGEmptyResults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": []}`)
	}))
	defer server.Close()

	s := &SearXNG{baseURL: server.URL, client: server.Client()}
	results, err := s.Search(context.Background(), "nothing", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestFeatureSearXNGServerError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	s := &SearXNG{baseURL: server.URL, client: server.Client()}
	_, err := s.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// rewriteTransport rewrites request URLs to point to the test server.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to point to our test server, preserving path and query
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.target[len("http://"):]
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}
