package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParallelSearchRequestAndResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json, text/event-stream" {
			t.Errorf("Accept = %q", got)
		}

		var body struct {
			JSONRPC string `json:"jsonrpc"`
			Method  string `json:"method"`
			Params  struct {
				Name      string `json:"name"`
				Arguments struct {
					Objective     string   `json:"objective"`
					SearchQueries []string `json:"search_queries"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.JSONRPC != "2.0" || body.Method != "tools/call" || body.Params.Name != "web_search" {
			t.Errorf("unexpected RPC envelope: %+v", body)
		}
		if body.Params.Arguments.Objective != "go context cancellation" {
			t.Errorf("objective = %q", body.Params.Arguments.Objective)
		}
		if want := []string{"go context cancellation"}; !reflect.DeepEqual(body.Params.Arguments.SearchQueries, want) {
			t.Errorf("search_queries = %v, want %v", body.Params.Arguments.SearchQueries, want)
		}

		writeParallelResponse(t, w, `{"results":[
			{"url":"https://go.dev/blog/context","title":"Go Concurrency Patterns","excerpts":[" First excerpt. ","Second excerpt."]},
			{"url":"https://pkg.go.dev/context","title":"context package","excerpts":[]}
		]}`)
	}))
	defer server.Close()

	backend := &Parallel{client: server.Client(), endpoint: server.URL}
	results, err := backend.Search(context.Background(), "go context cancellation", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Title: "Go Concurrency Patterns", URL: "https://go.dev/blog/context", Description: "First excerpt.", Content: "First excerpt.\nSecond excerpt."},
		{Title: "context package", URL: "https://pkg.go.dev/context"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("results = %#v, want %#v", results, want)
	}
}

func TestParallelSearchAppliesLimitAndSkipsInvalidResults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeParallelResponse(t, w, `{"results":[
			{"url":"","title":"missing URL","excerpts":["x"]},
			{"url":"https://example.com/1","title":"one","excerpts":["one"]},
			{"url":"https://example.com/2","title":"two","excerpts":["two"]}
		]}`)
	}))
	defer server.Close()

	backend := &Parallel{client: server.Client(), endpoint: server.URL}
	results, err := backend.Search(context.Background(), "q", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "one" {
		t.Fatalf("results = %#v, want first valid result", results)
	}
}

func TestParallelSearchBoundsDescriptionWithoutTruncatingContent(t *testing.T) {
	t.Parallel()
	excerpt := strings.Repeat("界", parallelDescriptionMaxRunes+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeParallelResponse(t, w, fmt.Sprintf(
			`{"results":[{"url":"https://example.com","title":"example","excerpts":[%q]}]}`,
			excerpt,
		))
	}))
	defer server.Close()

	backend := &Parallel{client: server.Client(), endpoint: server.URL}
	results, err := backend.Search(context.Background(), "q", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(results[0].Description)); got != parallelDescriptionMaxRunes {
		t.Fatalf("description length = %d runes, want %d", got, parallelDescriptionMaxRunes)
	}
	if !strings.HasSuffix(results[0].Description, "…") {
		t.Errorf("description = %q, want ellipsis suffix", results[0].Description)
	}
	if results[0].Content != excerpt {
		t.Errorf("content was truncated: got %d runes, want %d", len([]rune(results[0].Content)), len([]rune(excerpt)))
	}
}

func TestParallelSearchZeroLimitSkipsRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("zero limit must not make a request")
	}))
	defer server.Close()

	backend := &Parallel{client: server.Client(), endpoint: server.URL}
	results, err := backend.Search(context.Background(), "q", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want empty", results)
	}
}

func TestParallelSearchResponseErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed envelope", body: `{`, want: "failed to decode parallel response"},
		{name: "json-rpc error", body: `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad arguments"}}`, want: "JSON-RPC error -32602"},
		{name: "tool error", body: `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[]}}`, want: "tool returned an error"},
		{name: "missing text", body: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"image"}]}}`, want: "no text results"},
		{name: "malformed nested results", body: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{"}]}}`, want: "failed to decode parallel search results"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			backend := &Parallel{client: server.Client(), endpoint: server.URL}
			_, err := backend.Search(context.Background(), "q", 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestParallelSearchHTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	backend := &Parallel{client: server.Client(), endpoint: server.URL}
	_, err := backend.Search(context.Background(), "q", 1)
	if err == nil || !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("error = %v, want status 503", err)
	}
}

func TestParallelSearchPropagatesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	client := &http.Client{Transport: parallelRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	backend := &Parallel{client: client, endpoint: "https://example.com/mcp"}
	_, err := backend.Search(ctx, "q", 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

type parallelRoundTripFunc func(*http.Request) (*http.Response, error)

func (f parallelRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func writeParallelResponse(t *testing.T, w http.ResponseWriter, payload string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"content": []map[string]string{{"type": "text", "text": payload}},
		},
	}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
