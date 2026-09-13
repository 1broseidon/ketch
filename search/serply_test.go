package search

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/health"
)

func serplyBackend(target string, keys ...string) *Serply {
	if len(keys) == 0 {
		keys = []string{"serply-test"}
	}
	return &Serply{keys: deterministicPool(keys...), client: rewrittenClient(target)}
}

func TestSerplyHappyPath(t *testing.T) {
	t.Parallel()
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if key := r.Header.Get("X-Api-Key"); key != "serply-test" {
			t.Errorf("X-Api-Key = %q, want serply-test", key)
		}
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[
			{"title":"Go Docs","link":"https://go.dev/doc/","description":"The Go Programming Language","position":1},
			{"title":"Go Blog","link":"https://go.dev/blog/","description":"The Go Blog","position":2}
		]}`)
	}))
	t.Cleanup(server.Close)

	results, err := serplyBackend(server.URL).Search(context.Background(), "go docs", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Get("q") != "go docs" || got.Get("num") != "5" {
		t.Errorf("query params = %v, want q=go docs and num=5", got)
	}
	if got.Has("api_key") || got.Has("apikey") {
		t.Error("request URL must not carry the API key; auth is header-only")
	}
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
	want := Result{Title: "Go Docs", URL: "https://go.dev/doc/", Description: "The Go Programming Language"}
	if !reflect.DeepEqual(results[0], want) {
		t.Errorf("first result = %+v, want %+v", results[0], want)
	}
}

func TestSerplyHonorsLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		limit    int
		wantNum  string
		wantLen  int
		requests int
	}{
		{name: "below the page size", limit: 2, wantNum: "2", wantLen: 2, requests: 1},
		{name: "above the page size", limit: 50, wantNum: "10", wantLen: 3, requests: 1},
		{name: "zero", limit: 0, wantLen: 0},
		{name: "negative", limit: -1, wantLen: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var nums []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nums = append(nums, r.URL.Query().Get("num"))
				w.Header().Set("Content-Type", "application/json")
				// Serply treats num as approximate, so a page can exceed it.
				_, _ = io.WriteString(w, `{"results":[
					{"title":"A","link":"https://a.example","description":"a"},
					{"title":"B","link":"https://b.example","description":"b"},
					{"title":"C","link":"https://c.example","description":"c"}
				]}`)
			}))
			t.Cleanup(server.Close)

			results, err := serplyBackend(server.URL).Search(context.Background(), "q", tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(results), tc.wantLen)
			}
			if len(nums) != tc.requests {
				t.Fatalf("requests = %d, want %d", len(nums), tc.requests)
			}
			if tc.requests == 1 && nums[0] != tc.wantNum {
				t.Errorf("num = %q, want %q", nums[0], tc.wantNum)
			}
		})
	}
}

func TestSerplySkipsResultsWithoutALink(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[
			{"title":"No link","link":"  ","description":"skipped"},
			{"title":"Kept","link":"https://kept.example","description":"kept"}
		]}`)
	}))
	t.Cleanup(server.Close)

	results, err := serplyBackend(server.URL).Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].URL != "https://kept.example" {
		t.Fatalf("results = %+v, want only the linked row", results)
	}
}

func TestSerplyStatusErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     int
		body       string
		wantErrSub string
	}{
		{name: "401", status: http.StatusUnauthorized, body: `{"detail":"Invalid API key"}`, wantErrSub: "serply_api_key"},
		{name: "403", status: http.StatusForbidden, body: `{"detail":"Forbidden"}`, wantErrSub: "serply_api_key"},
		{name: "402", status: http.StatusPaymentRequired, body: `{"detail":"No credits"}`, wantErrSub: "credits exhausted"},
		{name: "429", status: http.StatusTooManyRequests, body: `{"detail":"Too many requests"}`, wantErrSub: "rate limited"},
		{name: "500", status: http.StatusInternalServerError, body: `boom`, wantErrSub: "status 500: boom"},
		{name: "503 with an empty body", status: http.StatusServiceUnavailable, wantErrSub: "status 503"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)

			_, err := serplyBackend(server.URL).Search(context.Background(), "q", 1)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("error %q should contain %q", err.Error(), tc.wantErrSub)
			}
			if strings.Contains(err.Error(), "serply-test") {
				t.Fatalf("error leaked key: %s", err.Error())
			}
		})
	}
}

func TestSerplyMalformedJSON(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{not-json`)
	}))
	t.Cleanup(server.Close)

	_, err := serplyBackend(server.URL).Search(context.Background(), "q", 1)
	if err == nil || !strings.Contains(err.Error(), "failed to decode serply response") {
		t.Fatalf("error = %v, want decode failure", err)
	}
}

func TestSerplyRotatesKeyOnRetryableStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			var got []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = append(got, r.Header.Get("X-Api-Key"))
				if len(got) == 1 {
					w.WriteHeader(status)
					return
				}
				_, _ = io.WriteString(w, `{"results":[]}`)
			}))
			t.Cleanup(server.Close)

			backend := serplyBackend(server.URL, "first-secret", "second-secret")
			if _, err := backend.Search(context.Background(), "q", 1); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []string{"first-secret", "second-secret"}) {
				t.Fatalf("keys used = %v, want first then a different key", got)
			}
		})
	}
}

func TestSerplySingleKeyDoesNotRetry(t *testing.T) {
	t.Parallel()
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	if _, err := serplyBackend(server.URL).Search(context.Background(), "q", 1); err == nil {
		t.Fatal("expected a rate-limit error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestSerplyHonorsCancellation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("cancelled request must not reach the server")
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := serplyBackend(server.URL).Search(ctx, "q", 1); err == nil {
		t.Fatal("expected a cancellation error")
	}
}

func TestProbeSerply(t *testing.T) {
	t.Parallel()
	// A blank key must be reported without contacting the service at all.
	if status, detail := ProbeSerply(context.Background(), nil, serplyEndpoint, "  "); status != health.StatusNoKey || !strings.Contains(detail, "serply_api_key") {
		t.Fatalf("blank key probe = %v / %q", status, detail)
	}
	tests := []struct {
		name   string
		status int
		want   health.Status
	}{
		{name: "200", status: http.StatusOK, want: health.StatusOK},
		{name: "401", status: http.StatusUnauthorized, want: health.StatusMisconfigured},
		{name: "403", status: http.StatusForbidden, want: health.StatusMisconfigured},
		{name: "402", status: http.StatusPaymentRequired, want: health.StatusOK},
		{name: "429", status: http.StatusTooManyRequests, want: health.StatusOK},
		{name: "500", status: http.StatusInternalServerError, want: health.StatusUnreachable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Api-Key") != "serply-test" {
					t.Errorf("probe did not send the key header")
				}
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(server.Close)

			status, _ := ProbeSerply(context.Background(), server.Client(), server.URL, "serply-test")
			if status != tc.want {
				t.Fatalf("status = %q, want %q", status, tc.want)
			}
		})
	}
}
