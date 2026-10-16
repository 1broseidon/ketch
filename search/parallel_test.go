package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
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

func TestParallelSearchCollapsesWhitespaceInTitleAndDescription(t *testing.T) {
	t.Parallel()
	rawTitle := "\n         How to Handle Errors in Go\n    "
	rawExcerpt := "First line.\n\n* bullet\t\ttabbed\n  indented tail"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeParallelResponse(t, w, fmt.Sprintf(
			`{"results":[{"url":"https://example.com","title":%q,"excerpts":[%q]}]}`,
			rawTitle, rawExcerpt,
		))
	}))
	defer server.Close()

	backend := &Parallel{client: server.Client(), endpoint: server.URL}
	results, err := backend.Search(context.Background(), "q", 1)
	if err != nil {
		t.Fatal(err)
	}

	// --minimal prints "url\ttitle\tdescription\n", so neither field may carry
	// a newline or tab without breaking the one-result-per-line contract.
	for _, field := range []struct{ name, value string }{
		{"title", results[0].Title},
		{"description", results[0].Description},
	} {
		if strings.ContainsAny(field.value, "\n\t") {
			t.Errorf("%s = %q, want no newline or tab", field.name, field.value)
		}
		if field.value != strings.TrimSpace(field.value) {
			t.Errorf("%s = %q, want no leading/trailing space", field.name, field.value)
		}
	}
	if want := "How to Handle Errors in Go"; results[0].Title != want {
		t.Errorf("title = %q, want %q", results[0].Title, want)
	}
	if want := "First line. * bullet tabbed indented tail"; results[0].Description != want {
		t.Errorf("description = %q, want %q", results[0].Description, want)
	}
	// Content keeps the full, unflattened excerpt text.
	if results[0].Content != rawExcerpt {
		t.Errorf("content = %q, want the raw excerpt preserved", results[0].Content)
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

func TestParallelAuthorizationHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys []string
		want string
	}{
		{name: "keyless", want: ""},
		{name: "blank key stays keyless", keys: []string{"  "}, want: ""},
		{name: "keyed", keys: []string{"par-test"}, want: "Bearer par-test"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != tc.want {
					t.Errorf("Authorization = %q, want %q", got, tc.want)
				}
				writeParallelResponse(t, w, `{"results":[]}`)
			}))
			defer server.Close()

			backend := &Parallel{keys: newKeyPool(tc.keys), client: server.Client(), endpoint: server.URL}
			if _, err := backend.Search(context.Background(), "q", 1); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestParallelProviderReadsKeyPool(t *testing.T) {
	t.Parallel()
	var cfg config.Config
	cfg.SetProvider("parallel_api_key", "first")
	cfg.SetProvider("parallel_api_keys", []string{"second", "first"})
	searcher, err := parallelProvider().New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := searcher.(*Parallel).keys.size(); got != 2 {
		t.Fatalf("key pool size = %d, want 2", got)
	}
	searcher, err = parallelProvider().New(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := searcher.(*Parallel).keys.size(); got != 0 {
		t.Fatalf("keyless pool size = %d, want 0", got)
	}
}

func TestParallelKeyedStatusErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code int
		keys []string
		want string
	}{
		{name: "401 keyed", code: http.StatusUnauthorized, keys: []string{"par-secret"}, want: "parallel: invalid API key (key 1 of 1; set via: ketch config set parallel_api_key <key>)"},
		{name: "403 keyed keeps generic error", code: http.StatusForbidden, keys: []string{"par-secret"}, want: "parallel returned status 403: denied"},
		{name: "402 keyed", code: http.StatusPaymentRequired, keys: []string{"par-secret"}, want: "parallel: payment required (key 1 of 1"},
		{name: "429 keyed", code: http.StatusTooManyRequests, keys: []string{"par-secret"}, want: "parallel: rate limited (key 1 of 1)"},
		{name: "401 keyless keeps generic error", code: http.StatusUnauthorized, want: "parallel returned status 401: denied"},
		{name: "429 keyless keeps generic error", code: http.StatusTooManyRequests, want: "parallel returned status 429: denied"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "denied", tc.code)
			}))
			defer server.Close()

			backend := &Parallel{keys: newKeyPool(tc.keys), client: server.Client(), endpoint: server.URL}
			_, err := backend.Search(context.Background(), "q", 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "par-secret") {
				t.Fatalf("error leaked key value: %q", err)
			}
		})
	}
}

func TestProbeParallel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		key        string
		code       int
		wantStatus health.Status
		wantDetail string
	}{
		{name: "keyless ok", code: http.StatusOK, wantStatus: health.StatusOK},
		{name: "keyless 401 is unreachable", code: http.StatusUnauthorized, wantStatus: health.StatusUnreachable, wantDetail: "parallel returned status 401"},
		{name: "keyed ok", key: "par-secret", code: http.StatusOK, wantStatus: health.StatusOK},
		{name: "keyed 401 is misconfigured", key: "par-secret", code: http.StatusUnauthorized, wantStatus: health.StatusMisconfigured, wantDetail: "parallel_api_key"},
		{name: "keyed 402 is misconfigured", key: "par-secret", code: http.StatusPaymentRequired, wantStatus: health.StatusMisconfigured, wantDetail: "payment required"},
		{name: "keyed 429 is reachable", key: "par-secret", code: http.StatusTooManyRequests, wantStatus: health.StatusOK, wantDetail: "rate limited"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want := ""
				if tc.key != "" {
					want = "Bearer " + tc.key
				}
				if got := r.Header.Get("Authorization"); got != want {
					t.Errorf("Authorization = %q, want %q", got, want)
				}
				w.WriteHeader(tc.code)
			}))
			defer server.Close()

			status, detail := ProbeParallel(context.Background(), server.Client(), server.URL, tc.key)
			if status != tc.wantStatus || !strings.Contains(detail, tc.wantDetail) {
				t.Fatalf("probe = (%v, %q), want (%v, containing %q)", status, detail, tc.wantStatus, tc.wantDetail)
			}
			if strings.Contains(detail, "par-secret") {
				t.Fatalf("detail leaked key value: %q", detail)
			}
		})
	}
}

// parallelSecret is the key the adversarial servers below echo back.
const parallelSecret = "par-secret/+ key"

func newKeyedParallel(t *testing.T, endpoint string, client *http.Client) *Parallel {
	t.Helper()
	backend := newParallelWithKeys([]string{parallelSecret})
	backend.client = client
	backend.endpoint = endpoint
	return backend
}

// assertNoParallelSecret fails when text carries the key in any echoed form.
func assertNoParallelSecret(t *testing.T, text string) {
	t.Helper()
	for _, form := range []string{parallelSecret, url.QueryEscape(parallelSecret), url.PathEscape(parallelSecret), "par-secret"} {
		if strings.Contains(text, form) {
			t.Fatalf("text leaked key form %q: %q", form, text)
		}
	}
}

func TestParallelKeyedErrorsRedactEchoedKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "status body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "bad upstream for "+r.Header.Get("Authorization")+" ("+parallelSecret+")", http.StatusInternalServerError)
			},
			want: "parallel returned status 500: bad upstream for Bearer [redacted] ([redacted])",
		},
		{
			name: "json-rpc error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": 1,
					"error": map[string]any{"code": -32000, "message": "key " + parallelSecret + " is not allowed"},
				})
			},
			want: "parallel JSON-RPC error -32000: key [redacted] is not allowed",
		},
		{
			name: "redirect location",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/loop?key="+url.QueryEscape(parallelSecret), http.StatusFound)
			},
			want: "stopped after 10 redirects",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			_, err := newKeyedParallel(t, server.URL, server.Client()).Search(context.Background(), "q", 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
			assertNoParallelSecret(t, err.Error())
		})
	}
}

func TestParallelKeylessErrorsAreNotRedacted(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom par-secret", http.StatusInternalServerError)
	}))
	defer server.Close()

	backend := newParallelWithKeys(nil)
	backend.client = server.Client()
	backend.endpoint = server.URL
	_, err := backend.Search(context.Background(), "q", 1)
	if err == nil || err.Error() != "parallel returned status 500: boom par-secret" {
		t.Fatalf("error = %v, want the keyless error unchanged", err)
	}
}

func TestParallelKeyedRedactionKeepsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	client := &http.Client{Transport: parallelRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	// The key in the endpoint puts it in net/http's error text, so the error
	// is rewritten; cancellation must still classify.
	endpoint := "https://example.com/mcp?k=" + url.QueryEscape(parallelSecret)
	_, err := newKeyedParallel(t, endpoint, client).Search(ctx, "q", 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error = %q, want the key redacted", err)
	}
	assertNoParallelSecret(t, err.Error())
}

func TestProbeParallelRedactsEchoedKey(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop?key="+url.QueryEscape(parallelSecret), http.StatusFound)
	}))
	defer server.Close()

	status, detail := ProbeParallel(context.Background(), server.Client(), server.URL, parallelSecret)
	if status != health.StatusUnreachable {
		t.Fatalf("status = %v, want unreachable (detail %q)", status, detail)
	}
	assertNoParallelSecret(t, detail)
}

func TestParallelKeyedForbiddenDoesNotRotate(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	backend := &Parallel{keys: deterministicPool("first", "second"), client: server.Client(), endpoint: server.URL}
	_, err := backend.Search(context.Background(), "q", 1)
	if err == nil || err.Error() != "parallel returned status 403" {
		t.Fatalf("error = %v, want the generic status error", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (403 is not a credential retry, as for exa and keenable)", got)
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
