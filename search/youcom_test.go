package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// youcomToolResponse renders a you-search tools/call result as the hosted MCP
// server does: an SSE data: line whose text payload carries the JSON results.
func youcomToolResponse(results string) string {
	return fmt.Sprintf("data: {\"result\":{\"content\":[{\"type\":\"text\",\"text\":%s}]}}\n", quoteJSON(results))
}

func quoteJSON(v string) string {
	encoded, _ := json.Marshal(v)
	return string(encoded)
}

func TestYoucomKeylessHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertYoucomKeylessCall(t, r, "golang", 5)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, youcomToolResponse(`{
			"results": {
				"web": [
					{"title":"Go Docs","url":"https://go.dev/doc/","description":"The Go Programming Language","snippets":["Go is an open-source language"]},
					{"title":"Go Blog","url":"https://go.dev/blog/","description":"","snippets":["The Go Blog","Second snippet"]}
				]
			}
		}`))
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool(nil), client: rewrittenClient(server.URL)}
	results, err := backend.Search(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	assertYoucomHappyPathResults(t, results)
}

func assertYoucomKeylessCall(t *testing.T, r *http.Request, query string, count int) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", r.Method)
	}
	if got := r.URL.Query().Get("profile"); got != "free" {
		t.Errorf("profile param = %q, want free (keyless)", got)
	}
	if got := r.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want empty for keyless", got)
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if req["method"] != "tools/call" {
		t.Errorf("method field = %v, want tools/call", req["method"])
	}
	params, _ := req["params"].(map[string]any)
	if params == nil || params["name"] != "you-search" {
		t.Errorf("tool name = %v, want you-search", params["name"])
	}
	arguments, _ := params["arguments"].(map[string]any)
	if arguments["query"] != query {
		t.Errorf("query = %v, want %s", arguments["query"], query)
	}
	if arguments["count"] != float64(count) {
		t.Errorf("count = %v, want %d", arguments["count"], count)
	}
}

func assertYoucomHappyPathResults(t *testing.T, results []Result) {
	t.Helper()
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
	if results[0].Title != "Go Docs" || results[0].URL != "https://go.dev/doc/" {
		t.Errorf("first result = %+v", results[0])
	}
	if results[0].Description != "The Go Programming Language" || results[0].Content != "Go is an open-source language" {
		t.Errorf("Description/Content = %q / %q", results[0].Description, results[0].Content)
	}
	if results[1].Description != "The Go Blog" {
		t.Errorf("snippet fallback Description = %q, want first snippet", results[1].Description)
	}
	if results[1].Content != "The Go Blog\nSecond snippet" {
		t.Errorf("joined snippets Content = %q", results[1].Content)
	}
}

func TestYoucomKeyedSendsBearerHeader(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer ydc-test" {
			t.Errorf("Authorization = %q, want Bearer ydc-test", got)
		}
		if got := r.URL.Query().Get("profile"); got != "" {
			t.Errorf("profile param = %q, want empty (authenticated endpoint)", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, youcomToolResponse(`{"results":{"web":[]}}`))
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool([]string{"ydc-test"}), client: rewrittenClient(server.URL)}
	if _, err := backend.Search(context.Background(), "golang", 5); err != nil {
		t.Fatalf("Search: %v", err)
	}
}

func TestYoucomHonorsLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		params, _ := req["params"].(map[string]any)
		arguments, _ := params["arguments"].(map[string]any)
		if arguments["count"] != float64(2) {
			t.Errorf("count = %v, want 2", arguments["count"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, youcomToolResponse(`{"results":{"web":[
			{"title":"A","url":"https://a.com","description":"a"},
			{"title":"B","url":"https://b.com","description":"b"},
			{"title":"C","url":"https://c.com","description":"c"}
		]}}`))
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool(nil), client: rewrittenClient(server.URL)}
	results, err := backend.Search(context.Background(), "golang", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
}

func TestYoucomStatusErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		code     int
		body     string
		contains string
	}{
		{"401", http.StatusUnauthorized, "", "youcom_api_key"},
		{"429", http.StatusTooManyRequests, "", "rate limited"},
		{"402", http.StatusPaymentRequired, "", "credits exhausted"},
		{"500", http.StatusInternalServerError, "", "500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.body != "" {
					w.WriteHeader(tc.code)
					_, _ = io.WriteString(w, tc.body)
					return
				}
				w.WriteHeader(tc.code)
			}))
			t.Cleanup(server.Close)

			backend := &Youcom{keys: newKeyPool([]string{"ydc-test"}), client: rewrittenClient(server.URL)}
			_, err := backend.Search(context.Background(), "q", 1)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.contains)
			}
			if strings.Contains(err.Error(), "ydc-test") {
				t.Fatalf("error leaked key value: %q", err)
			}
		})
	}
}

func TestYoucomToolErrorIsReported(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"result":{"content":[{"type":"text","text":"Error: Failed to perform search. Error code: 401"}],"isError":true}}`+"\n")
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool(nil), client: rewrittenClient(server.URL)}
	_, err := backend.Search(context.Background(), "q", 1)
	if err == nil {
		t.Fatal("expected a tool error")
	}
	if !strings.Contains(err.Error(), "youcom") {
		t.Fatalf("error = %q, want youcom prefix", err)
	}
}

func TestYoucomJSONRPCErrorIsReported(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"error":{"code":-32000,"message":"Not Acceptable"}}`+"\n")
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool(nil), client: rewrittenClient(server.URL)}
	_, err := backend.Search(context.Background(), "q", 1)
	if err == nil || !strings.Contains(err.Error(), "JSON-RPC error") {
		t.Fatalf("error = %v, want JSON-RPC error", err)
	}
}

func TestYoucomNonJSONBodyFails(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: not-json\n")
	}))
	t.Cleanup(server.Close)

	backend := &Youcom{keys: newKeyPool(nil), client: rewrittenClient(server.URL)}
	if _, err := backend.Search(context.Background(), "q", 1); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestYoucomTransportErrorsNeverExposeKeyedURL(t *testing.T) {
	tests := []struct {
		name      string
		cause     error
		want      string
		wantCause error
	}{
		{name: "transport", cause: errors.New("dial failure"), want: "youcom: request failed: transport error"},
		{name: "cancelled", cause: context.Canceled, want: "youcom: request failed: context canceled", wantCause: context.Canceled},
		{name: "deadline", cause: context.DeadlineExceeded, want: "youcom: request failed: context deadline exceeded", wantCause: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: failingTransport{err: test.cause}}
			backend := &Youcom{keys: newKeyPool([]string{"secret-key"}), client: client}
			_, err := backend.Search(context.Background(), "q", 1)
			if err == nil {
				t.Fatal("expected a transport error")
			}
			if err.Error() != test.want {
				t.Fatalf("error = %q, want %q", err, test.want)
			}
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatalf("error %v does not wrap %v", err, test.wantCause)
			}
			if strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), "api.you.com") {
				t.Fatalf("error exposed keyed URL or key value: %q", err)
			}
		})
	}
}

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }
