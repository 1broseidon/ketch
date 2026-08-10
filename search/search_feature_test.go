package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

	b := &Brave{keys: newKeyPool([]string{"test-key"}), client: server.Client()}
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

func TestFeatureBraveRequestShape(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/res/v1/web/search" {
			t.Errorf("path = %q, want /res/v1/web/search", got)
		}
		q := r.URL.Query()
		if got := q.Get("q"); got != "test query" {
			t.Errorf("q = %q, want test query", got)
		}
		if got := q.Get("count"); got != "5" {
			t.Errorf("count = %q, want 5", got)
		}
		if got := q.Get("text_decorations"); got != "false" {
			t.Errorf("text_decorations = %q, want false", got)
		}
		if got := q.Get("result_filter"); got != "web" {
			t.Errorf("result_filter = %q, want web", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := r.Header.Get("X-Subscription-Token"); got != "key" {
			t.Errorf("X-Subscription-Token = %q, want key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web":{"results":[{"title":"A","url":"https://a.com","description":"a"}]}}`)
	}))
	defer server.Close()

	b := &Brave{keys: newKeyPool([]string{"key"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := b.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
}

func TestFeatureBraveSearch401(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	b := &Brave{keys: newKeyPool([]string{"bad-key"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
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

	b := &Brave{keys: newKeyPool([]string{"key"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := b.Search(context.Background(), "test", 2)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2 (limit)", len(results))
	}
}

func TestFeatureBraveLimitCappedToAPIMax(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("count"); got != "20" {
			http.Error(w, "count must be <= 20", http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web":{"results":[`)
		for i := 1; i <= 21; i++ {
			if i > 1 {
				fmt.Fprint(w, `,`)
			}
			fmt.Fprintf(w, `{"title":"%d","url":"https://example.com/%d","description":"%d"}`, i, i, i)
		}
		fmt.Fprint(w, `]}}`)
	}))
	defer server.Close()

	b := &Brave{keys: newKeyPool([]string{"key"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := b.Search(context.Background(), "test", 50)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 20 {
		t.Errorf("got %d results, want Brave API max 20", len(results))
	}
}

func TestFeatureBraveEmptyResults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web": {"results": []}}`)
	}))
	defer server.Close()

	b := &Brave{keys: newKeyPool([]string{"key"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
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

func TestFeatureEXASearchParses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `event: message
`)
		fmt.Fprint(w, `data: {"result":{"content":[{"type":"text","text":"Title: Rust Programming Language\nURL: https://www.rust-lang.org/\nHighlights:\nRust is a programming language.\n\n---\n\nTitle: Rust Releases\nURL: https://blog.rust-lang.org/\nHighlights:\nRelease notes for Rust."}]}}
`)
	}))
	defer server.Close()

	e := &EXA{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := e.Search(context.Background(), "rust", 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Rust Programming Language" {
		t.Errorf("title = %q, want %q", results[0].Title, "Rust Programming Language")
	}
	if results[0].URL != "https://www.rust-lang.org/" {
		t.Errorf("url = %q, want %q", results[0].URL, "https://www.rust-lang.org/")
	}
	if results[0].Description != "Rust is a programming language." {
		t.Errorf("description = %q, want %q", results[0].Description, "Rust is a programming language.")
	}
	if !strings.Contains(results[0].Content, "Rust is a programming language.") {
		t.Errorf("content = %q, want rust highlight", results[0].Content)
	}
}

func TestFeatureEXALimitRespected(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `event: message
`)
		fmt.Fprint(w, `data: {"result":{"content":[{"type":"text","text":"Title: A\nURL: https://a.com\nHighlights:\na\n\n---\n\nTitle: B\nURL: https://b.com\nHighlights:\nb\n\n---\n\nTitle: C\nURL: https://c.com\nHighlights:\nc"}]}}
`)
	}))
	defer server.Close()

	e := &EXA{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := e.Search(context.Background(), "rust", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2 (limit)", len(results))
	}
}

func TestFeatureEXAEmptyResults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `event: message
`)
		fmt.Fprint(w, `data: {"result":{"content":[{"type":"text","text":""}]}}
`)
	}))
	defer server.Close()

	e := &EXA{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := e.Search(context.Background(), "nothing", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestFeatureEXAServerError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	e := &EXA{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := e.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestFeatureEXARequestShape(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		params := body["params"].(map[string]any)
		arguments := params["arguments"].(map[string]any)
		if body["method"] != "tools/call" {
			t.Errorf("method = %q, want tools/call", body["method"])
		}
		if params["name"] != "web_search_exa" {
			t.Errorf("tool name = %q, want web_search_exa", params["name"])
		}
		if arguments["query"] != "rust release date" {
			t.Errorf("query = %q, want rust release date", arguments["query"])
		}
		if arguments["numResults"] != float64(5) {
			t.Errorf("numResults = %v, want 5", arguments["numResults"])
		}
		if arguments["livecrawl"] != "fallback" {
			t.Errorf("livecrawl = %q, want fallback", arguments["livecrawl"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"result":{"content":[{"type":"text","text":"Title: Rust\nURL: https://www.rust-lang.org/\nHighlights:\nRust"}]}}
`)
	}))
	defer server.Close()

	e := &EXA{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := e.Search(context.Background(), "rust release date", 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
}

func TestFeatureEXAAPIKeyAdded(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("exaApiKey"); got != "test-key" {
			t.Errorf("exaApiKey = %q, want test-key", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"result":{"content":[{"type":"text","text":"Title: Rust\nURL: https://www.rust-lang.org/\nHighlights:\nRust"}]}}
`)
	}))
	defer server.Close()

	key := "test-key"
	e := &EXA{keys: newKeyPool([]string{key}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := e.Search(context.Background(), "rust", 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
}

func TestFeatureFirecrawlSearchParses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"data":{"web":[
			{"url":"https://go.dev/blog/error-handling-and-go","title":"Error handling and Go","description":"Go code uses error values."},
			{"url":"https://go.dev/doc/","title":"Go Docs","description":"Go documentation"}
		]}}`)
	}))
	defer server.Close()

	f := &Firecrawl{keys: newKeyPool([]string{"k"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := f.Search(context.Background(), "golang error handling", 5)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Error handling and Go" {
		t.Errorf("title = %q, want %q", results[0].Title, "Error handling and Go")
	}
	if results[0].URL != "https://go.dev/blog/error-handling-and-go" {
		t.Errorf("url = %q, want blog URL", results[0].URL)
	}
	if results[0].Description != "Go code uses error values." {
		t.Errorf("description = %q, want snippet", results[0].Description)
	}
}

func TestFeatureFirecrawlRequestShape(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPost {
			t.Errorf("method = %q, want POST", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["query"] != "rust release date" {
			t.Errorf("query = %q, want rust release date", body["query"])
		}
		if body["limit"] != float64(5) {
			t.Errorf("limit = %v, want 5", body["limit"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"data":{"web":[{"url":"https://www.rust-lang.org/","title":"Rust","description":"Rust"}]}}`)
	}))
	defer server.Close()

	f := &Firecrawl{keys: newKeyPool([]string{"test-key"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := f.Search(context.Background(), "rust release date", 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
}

func TestFeatureFirecrawlSelfHostedEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"data":{"web":[{"url":"https://example.com","title":"Ex","description":"d"}]}}`)
	}))
	defer server.Close()

	f := newFirecrawlWithKeys(nil, server.URL)
	results, err := f.Search(context.Background(), "q", 1)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if gotPath != "/v2/search" {
		t.Errorf("path = %q, want /v2/search", gotPath)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty for keyless self-host", gotAuth)
	}
}

func TestFeatureFirecrawlLimitRespected(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"data":{"web":[
			{"url":"https://a.com","title":"A","description":"a"},
			{"url":"https://b.com","title":"B","description":"b"},
			{"url":"https://c.com","title":"C","description":"c"}
		]}}`)
	}))
	defer server.Close()

	f := &Firecrawl{keys: newKeyPool([]string{"k"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := f.Search(context.Background(), "test", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2 (limit)", len(results))
	}
}

func TestFeatureFirecrawlEmptyResults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"data":{"web":[]}}`)
	}))
	defer server.Close()

	f := &Firecrawl{keys: newKeyPool([]string{"k"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := f.Search(context.Background(), "nothing", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestFeatureFirecrawlInvalidKey(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	f := &Firecrawl{keys: newKeyPool([]string{"bad-key"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := f.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "firecrawl_api_key") {
		t.Errorf("error %q should carry the config hint", err.Error())
	}
}

func TestFeatureFirecrawlServerError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	f := &Firecrawl{keys: newKeyPool([]string{"k"}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := f.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestFeatureKeenableSearchParses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [
				{"title": "Go Docs", "url": "https://go.dev/doc/", "description": "Go documentation"},
				{"title": "Go Blog", "url": "https://go.dev/blog/", "description": "The Go Blog"}
			]
		}`)
	}))
	defer server.Close()

	k := &Keenable{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := k.Search(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Go Docs" {
		t.Errorf("title = %q, want %q", results[0].Title, "Go Docs")
	}
	if results[0].URL != "https://go.dev/doc/" {
		t.Errorf("url = %q, want %q", results[0].URL, "https://go.dev/doc/")
	}
	if results[0].Description != "Go documentation" {
		t.Errorf("description = %q, want %q", results[0].Description, "Go documentation")
	}
}

func TestFeatureKeenableKeylessRequestShape(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != "POST" {
			t.Errorf("method = %q, want POST", got)
		}
		// No key configured => keyless public endpoint, no X-API-Key.
		if got := r.URL.Path; got != "/v1/search/public" {
			t.Errorf("path = %q, want /v1/search/public", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Errorf("X-API-Key = %q, want empty for keyless", got)
		}
		if got := r.Header.Get("X-Keenable-Title"); got != "Ketch" {
			t.Errorf("X-Keenable-Title = %q, want Ketch", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["query"] != "test query" {
			t.Errorf("query = %q, want test query", body["query"])
		}
		if body["mode"] != "pro" {
			t.Errorf("mode = %q, want pro", body["mode"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"title":"A","url":"https://a.com","description":"a"}]}`)
	}))
	defer server.Close()

	k := &Keenable{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := k.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
}

func TestFeatureKeenableKeyedRequestShape(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A configured key switches to the authenticated endpoint + X-API-Key.
		if got := r.URL.Path; got != "/v1/search" {
			t.Errorf("path = %q, want /v1/search", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "keen_test" {
			t.Errorf("X-API-Key = %q, want keen_test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"title":"A","url":"https://a.com","description":"a"}]}`)
	}))
	defer server.Close()

	key := "keen_test"
	k := &Keenable{keys: newKeyPool([]string{key}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := k.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
}

func TestFeatureKeenableLimitRespected(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[
			{"title":"A","url":"https://a.com","description":"a"},
			{"title":"B","url":"https://b.com","description":"b"},
			{"title":"C","url":"https://c.com","description":"c"}
		]}`)
	}))
	defer server.Close()

	k := &Keenable{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := k.Search(context.Background(), "test", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2 (limit)", len(results))
	}
}

func TestFeatureKeenableEmptyResults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": []}`)
	}))
	defer server.Close()

	k := &Keenable{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	results, err := k.Search(context.Background(), "nothing", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestFeatureKeenable401(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	key := "bad-key"
	k := &Keenable{keys: newKeyPool([]string{key}), client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := k.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestFeatureKeenableServerError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	k := &Keenable{client: &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: server.URL}}}
	_, err := k.Search(context.Background(), "test", 5)
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
