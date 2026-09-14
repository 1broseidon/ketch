package docs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rewriteTransport rewrites request URLs to point to the test server.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.target[len("http://"):]
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// newTestContext7 returns a Context7 client whose requests are rewritten to
// the given handler.
func newTestContext7(t *testing.T, handler http.HandlerFunc) *Context7 {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Context7{
		apiKey: "test-key",
		client: &http.Client{Transport: &rewriteTransport{target: server.URL}},
	}
}

// resolveHandler serves n library matches from the resolve endpoint.
func resolveHandler(n int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		matches := make([]LibraryMatch, n)
		for i := range matches {
			matches[i] = LibraryMatch{ID: fmt.Sprintf("/org/lib%d", i), Title: fmt.Sprintf("lib%d", i)}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": matches}) //nolint:errcheck
	}
}

func TestFeatureResolveLibraryRespectsLimit(t *testing.T) {
	t.Parallel()
	c := newTestContext7(t, resolveHandler(20))

	matches, err := c.ResolveLibrary(context.Background(), "somelib", 3)
	if err != nil {
		t.Fatalf("ResolveLibrary error: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("got %d matches, want 3 (limit must bound resolve results)", len(matches))
	}
	if matches[0].ID != "/org/lib0" {
		t.Errorf("first match = %q, want /org/lib0 (limit must keep the top-ranked matches)", matches[0].ID)
	}
}

func TestFeatureResolveLibraryNoLimitReturnsAll(t *testing.T) {
	t.Parallel()
	c := newTestContext7(t, resolveHandler(20))

	matches, err := c.ResolveLibrary(context.Background(), "somelib", 0)
	if err != nil {
		t.Fatalf("ResolveLibrary error: %v", err)
	}
	if len(matches) != 20 {
		t.Fatalf("got %d matches, want all 20 for limit <= 0", len(matches))
	}
}

func TestFeatureGetDocs404IsErrNotFound(t *testing.T) {
	t.Parallel()
	c := newTestContext7(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "library not found", http.StatusNotFound)
	})

	_, err := c.GetDocs(context.Background(), "/no/such-lib", "query", 4000)
	if err == nil {
		t.Fatal("want error for 404 response")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("404 must wrap ErrNotFound so surfaces map it to not-found, got: %v", err)
	}
}

// A bare query honours the result limit; it used to return every snippet
// in the token budget regardless of --limit.
func TestFeatureSearchRespectsLimit(t *testing.T) {
	c := newTestContext7(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/search") {
			resolveHandler(1)(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"codeSnippets": []map[string]any{{"codeTitle": "A", "codeId": "a", "codeList": []map[string]string{{"code": "x"}}}, {"codeTitle": "B", "codeId": "b", "codeList": []map[string]string{{"code": "y"}}}},
			"infoSnippets": []map[string]any{{"pageId": "c", "breadcrumb": "C", "content": "z"}},
		})
	})
	for _, tc := range []struct{ limit, want int }{{0, 3}, {2, 2}, {5, 3}} {
		results, err := c.Search(context.Background(), "anything", tc.limit)
		if err != nil || len(results) != tc.want {
			t.Errorf("limit %d: got %d results, err %v; want %d", tc.limit, len(results), err, tc.want)
		}
	}
}
