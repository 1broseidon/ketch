package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDegoogSearchParses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [
				{"title": "Go Docs", "url": "https://golang.org/doc/", "snippet": "Go documentation"},
				{"title": "Go Blog", "url": "https://blog.golang.org/", "snippet": "The Go Blog"},
				{"title": "Go Playground", "url": "https://play.golang.org/", "snippet": "Run Go online"}
			],
			"query": "golang",
			"totalTime": 123,
			"type": "web"
		}`)
	}))
	defer server.Close()

	d := NewDegoog(server.URL)
	results, err := d.Search(context.Background(), "golang", 3)
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

func TestDegoogSearchRequestShape(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/search" {
			t.Errorf("path = %q, want /api/search", got)
		}
		if got := r.URL.Query().Get("q"); got != "test query" {
			t.Errorf("q = %q, want %q", got, "test query")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": []}`)
	}))
	defer server.Close()

	d := NewDegoog(server.URL)
	_, err := d.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
}

func TestDegoogSearchRespectsLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [
				{"title": "A", "url": "https://a.com", "snippet": "a"},
				{"title": "B", "url": "https://b.com", "snippet": "b"},
				{"title": "C", "url": "https://c.com", "snippet": "c"}
			]
		}`)
	}))
	defer server.Close()

	d := NewDegoog(server.URL)
	results, err := d.Search(context.Background(), "test", 2)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

func TestDegoogSearchHTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	d := NewDegoog(server.URL)
	_, err := d.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestDegoogSearchInvalidJSON(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html>not json</html>`)
	}))
	defer server.Close()

	d := NewDegoog(server.URL)
	_, err := d.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
