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

func TestYoucomHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("X-API-Key"); got != "ydc-test" {
			t.Errorf("X-API-Key = %q, want ydc-test", got)
		}
		if got := r.URL.Path; got != "/v1/search" {
			t.Errorf("path = %q, want /v1/search", got)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var req map[string]any
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req["query"] != "golang" {
			t.Errorf("query = %v, want golang", req["query"])
		}
		if req["count"] != float64(5) {
			t.Errorf("count = %v, want 5", req["count"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"results": {
				"web": [
					{"title":"Go Docs","url":"https://go.dev/doc/","description":"The Go Programming Language","snippets":["Go is an open-source language"]},
					{"title":"Go Blog","url":"https://go.dev/blog/","description":"","snippets":["The Go Blog","Second snippet"]}
				]
			}
		}`)
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool([]string{"ydc-test"}), client: rewrittenClient(server.URL)}
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
	if results[0].Description != "The Go Programming Language" || results[0].Content != "Go is an open-source language" {
		t.Errorf("Description/Content = %q / %q", results[0].Description, results[0].Content)
	}
	// Empty description falls back to the first snippet; content joins them.
	if results[1].Description != "The Go Blog" {
		t.Errorf("Description = %q, want first snippet", results[1].Description)
	}
	if results[1].Content != "The Go Blog\nSecond snippet" {
		t.Errorf("Content = %q, want joined snippets", results[1].Content)
	}
}

func TestYoucomHonorsLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req youcomRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Count != 2 {
			t.Errorf("count = %d, want 2", req.Count)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":{"web":[
			{"title":"A","url":"https://a.com","description":"a","snippets":[]},
			{"title":"B","url":"https://b.com","description":"b","snippets":[]},
			{"title":"C","url":"https://c.com","description":"c","snippets":[]}
		]}}`)
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool([]string{"ydc-test"}), client: rewrittenClient(server.URL)}
	results, err := backend.Search(context.Background(), "golang", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
}

func TestYoucomSkipsEmptyURLs(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":{"web":[
			{"title":"No URL","url":"","description":"dropped"},
			{"title":"A","url":"https://a.com","description":"a"}
		]}}`)
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool([]string{"ydc-test"}), client: rewrittenClient(server.URL)}
	results, err := backend.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1 (empty-URL result dropped)", len(results))
	}
}

func TestYoucomStatusErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     int
		body       string
		wantErrSub string
	}{
		{name: "401", status: http.StatusUnauthorized, body: `unauthorized`, wantErrSub: "youcom_api_key"},
		{name: "403", status: http.StatusForbidden, body: `forbidden`, wantErrSub: "youcom_api_key"},
		{name: "429", status: http.StatusTooManyRequests, body: `rate limited`, wantErrSub: "rate limited"},
		{name: "402", status: http.StatusPaymentRequired, body: `credits`, wantErrSub: "credits"},
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

			backend := &Youcom{keys: newKeyPool([]string{"ydc-test"}), client: rewrittenClient(server.URL)}
			_, err := backend.Search(context.Background(), "q", 1)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("error %q should contain %q", err.Error(), tc.wantErrSub)
			}
			if strings.Contains(err.Error(), "ydc-test") {
				t.Fatalf("error leaked key: %s", err.Error())
			}
		})
	}
}

func TestYoucomMalformedJSON(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{not-json`)
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool([]string{"ydc-test"}), client: rewrittenClient(server.URL)}
	_, err := backend.Search(context.Background(), "q", 1)
	if err == nil || !strings.Contains(err.Error(), "failed to decode youcom response") {
		t.Fatalf("error = %v, want decode failure", err)
	}
}

func TestYoucomZeroLimit(t *testing.T) {
	t.Parallel()
	backend := NewYoucom("ydc-test")
	results, err := backend.Search(context.Background(), "q", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}
