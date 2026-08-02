package search

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTavilyHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tvly-test" {
			t.Errorf("Authorization = %q, want Bearer tvly-test", got)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var req map[string]any
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if _, ok := req["api_key"]; ok {
			t.Error("request body must not include api_key; auth is header-only")
		}
		if req["query"] != "golang" {
			t.Errorf("query = %v, want golang", req["query"])
		}
		if req["search_depth"] != "basic" {
			t.Errorf("search_depth = %v, want basic", req["search_depth"])
		}
		if req["max_results"] != float64(5) {
			t.Errorf("max_results = %v, want 5", req["max_results"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"results": [
				{"title":"Go Docs","url":"https://go.dev/doc/","content":"The Go Programming Language","score":0.9},
				{"title":"Go Blog","url":"https://go.dev/blog/","content":"The Go Blog","score":0.8}
			]
		}`)
	}))
	t.Cleanup(server.Close)

	backend := &Tavily{keys: newKeyPool([]string{"tvly-test"}), client: rewrittenClient(server.URL)}
	results, err := backend.Search(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
	if results[0].Title != "Go Docs" || results[0].URL != "https://go.dev/doc/" {
		t.Errorf("first result = %+v", results[0])
	}
	if results[0].Description != "The Go Programming Language" || results[0].Content != "The Go Programming Language" {
		t.Errorf("Description/Content = %q / %q", results[0].Description, results[0].Content)
	}
}

func TestTavilyHonorsLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req tavilyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.MaxResults != 2 {
			t.Errorf("max_results = %d, want 2", req.MaxResults)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[
			{"title":"A","url":"https://a.com","content":"a","score":1},
			{"title":"B","url":"https://b.com","content":"b","score":0.9},
			{"title":"C","url":"https://c.com","content":"c","score":0.8}
		]}`)
	}))
	t.Cleanup(server.Close)

	backend := &Tavily{keys: newKeyPool([]string{"tvly-test"}), client: rewrittenClient(server.URL)}
	results, err := backend.Search(context.Background(), "golang", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
}

func TestTavilyStatusErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     int
		body       string
		wantErrSub string
	}{
		{name: "401", status: http.StatusUnauthorized, body: `unauthorized`, wantErrSub: "tavily_api_key"},
		{name: "429", status: http.StatusTooManyRequests, body: `rate limited`, wantErrSub: "rate limited"},
		{name: "432", status: 432, body: `plan`, wantErrSub: "plan limit"},
		{name: "433", status: 433, body: `paygo`, wantErrSub: "pay-as-you-go"},
		{name: "500", status: http.StatusInternalServerError, body: `boom`, wantErrSub: "status 500"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)

			backend := &Tavily{keys: newKeyPool([]string{"tvly-test"}), client: rewrittenClient(server.URL)}
			_, err := backend.Search(context.Background(), "q", 1)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("error %q should contain %q", err.Error(), tc.wantErrSub)
			}
			if strings.Contains(err.Error(), "tvly-test") {
				t.Fatalf("error leaked key: %s", err.Error())
			}
		})
	}
}

func TestTavilyMalformedJSON(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{not-json`)
	}))
	t.Cleanup(server.Close)

	backend := &Tavily{keys: newKeyPool([]string{"tvly-test"}), client: rewrittenClient(server.URL)}
	_, err := backend.Search(context.Background(), "q", 1)
	if err == nil || !strings.Contains(err.Error(), "failed to decode tavily response") {
		t.Fatalf("error = %v, want decode failure", err)
	}
}

func TestTavilyRequiresAuthorizationHeader(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("Authorization header missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"title":"A","url":"https://a.com","content":"a"}]}`)
	}))
	t.Cleanup(server.Close)

	backend := &Tavily{keys: newKeyPool([]string{"tvly-test"}), client: rewrittenClient(server.URL)}
	if _, err := backend.Search(context.Background(), "q", 1); err != nil {
		t.Fatal(err)
	}
}

func TestTavilyZeroLimit(t *testing.T) {
	t.Parallel()
	backend := NewTavily("tvly-test")
	results, err := backend.Search(context.Background(), "q", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}
