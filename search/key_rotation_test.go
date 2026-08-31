package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func deterministicPool(keys ...string) keyPool {
	pool := newKeyPool(keys)
	pool.randIntN = func(int) int { return 0 }
	return pool
}

func rewrittenClient(target string) *http.Client {
	return &http.Client{Transport: &rewriteTransport{base: http.DefaultTransport, target: target}}
}

func TestBraveRotatesKeyOn429(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-Subscription-Token"))
		if len(got) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = fmt.Fprint(w, `{"web":{"results":[]}}`)
	}))
	defer server.Close()

	backend := &Brave{keys: deterministicPool("first-secret", "second-secret"), client: rewrittenClient(server.URL)}
	if _, err := backend.Search(context.Background(), "q", 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"first-secret", "second-secret"}) {
		t.Fatalf("keys used = %v, want first then a different key", got)
	}
}

func TestBraveSingleKeyDoesNotRetry(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	backend := &Brave{keys: deterministicPool("only-secret"), client: rewrittenClient(server.URL)}
	if _, err := backend.Search(context.Background(), "q", 1); err == nil {
		t.Fatal("expected a rate-limit error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestBrave401ReportsOrdinalWithoutKeyValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	backend := &Brave{keys: deterministicPool("first-secret", "second-secret"), client: rewrittenClient(server.URL)}
	_, err := backend.Search(context.Background(), "q", 1)
	if err == nil {
		t.Fatal("expected an authentication error")
	}
	message := err.Error()
	if !strings.Contains(message, "key 2 of 2") {
		t.Fatalf("error = %q, want final key ordinal", message)
	}
	for _, secret := range []string{"first-secret", "second-secret"} {
		if strings.Contains(message, secret) {
			t.Fatalf("error exposed key value %q: %s", secret, message)
		}
	}
}

func TestEXARotatesKeyOn401(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query().Get("exaApiKey"))
		if len(got) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"result":{"content":[]}}`+"\n")
	}))
	defer server.Close()

	backend := &EXA{keys: deterministicPool("first", "second"), client: rewrittenClient(server.URL)}
	if _, err := backend.Search(context.Background(), "q", 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("keys used = %v", got)
	}
}

func TestEXATransportErrorsNeverExposeKeyedURL(t *testing.T) {
	tests := []struct {
		name      string
		cause     error
		want      string
		wantCause error
	}{
		{name: "transport", cause: errors.New("dial failure"), want: "exa: request failed: transport error"},
		{name: "cancelled", cause: context.Canceled, want: "exa: request failed: context canceled", wantCause: context.Canceled},
		{name: "deadline", cause: context.DeadlineExceeded, want: "exa: request failed: context deadline exceeded", wantCause: context.DeadlineExceeded},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const secret = "exa-transport-secret"
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("transport failure for %s: %w", req.URL.String(), test.cause)
			})}
			backend := &EXA{keys: deterministicPool(secret), client: client}
			_, err := backend.Search(context.Background(), "q", 1)
			if err == nil {
				t.Fatal("expected a transport error")
			}
			if err.Error() != test.want {
				t.Fatal("Exa returned an unexpected sanitized error class")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "exaApiKey") || strings.Contains(err.Error(), "?") {
				t.Fatal("Exa transport error exposed request query data")
			}
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatal("sanitization lost the cancellation error class")
			}
		})
	}
}

func TestFirecrawlRotatesKeyOn402(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if len(got) == 1 {
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		_, _ = fmt.Fprint(w, `{"success":true,"data":{"web":[]}}`)
	}))
	defer server.Close()

	backend := &Firecrawl{keys: deterministicPool("first", "second"), client: rewrittenClient(server.URL)}
	if _, err := backend.Search(context.Background(), "q", 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("keys used = %v", got)
	}
}

func TestKeenableRotatesKeyOn429(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-API-Key"))
		if len(got) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = fmt.Fprint(w, `{"results":[]}`)
	}))
	defer server.Close()

	backend := &Keenable{keys: deterministicPool("first", "second"), client: rewrittenClient(server.URL)}
	if _, err := backend.Search(context.Background(), "q", 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("keys used = %v", got)
	}
}

func TestBackendsRetryEveryCredentialStatus(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		successBody string
		newBackend  func(*http.Client) Searcher
		requestKey  func(*http.Request) string
	}{
		{
			name:        "brave 401",
			status:      http.StatusUnauthorized,
			successBody: `{"web":{"results":[]}}`,
			newBackend: func(client *http.Client) Searcher {
				return &Brave{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.Header.Get("X-Subscription-Token") },
		},
		{
			name:        "exa 429",
			status:      http.StatusTooManyRequests,
			successBody: "data: {\"result\":{\"content\":[]}}\n",
			newBackend: func(client *http.Client) Searcher {
				return &EXA{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.URL.Query().Get("exaApiKey") },
		},
		{
			name:        "firecrawl 401",
			status:      http.StatusUnauthorized,
			successBody: `{"success":true,"data":{"web":[]}}`,
			newBackend: func(client *http.Client) Searcher {
				return &Firecrawl{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") },
		},
		{
			name:        "firecrawl 429",
			status:      http.StatusTooManyRequests,
			successBody: `{"success":true,"data":{"web":[]}}`,
			newBackend: func(client *http.Client) Searcher {
				return &Firecrawl{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") },
		},
		{
			name:        "keenable 401",
			status:      http.StatusUnauthorized,
			successBody: `{"results":[]}`,
			newBackend: func(client *http.Client) Searcher {
				return &Keenable{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.Header.Get("X-API-Key") },
		},
		{
			name:        "tavily 401",
			status:      http.StatusUnauthorized,
			successBody: `{"results":[]}`,
			newBackend: func(client *http.Client) Searcher {
				return &Tavily{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string {
				return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			},
		},
		{
			name:        "tavily 429",
			status:      http.StatusTooManyRequests,
			successBody: `{"results":[]}`,
			newBackend: func(client *http.Client) Searcher {
				return &Tavily{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string {
				return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			},
		},
		{
			name:        "serpbase 401",
			status:      http.StatusUnauthorized,
			successBody: `{"organic_results":[]}`,
			newBackend: func(client *http.Client) Searcher {
				return &SerpBase{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.URL.Query().Get("api_key") },
		},
		{
			name:        "serpbase 429",
			status:      http.StatusTooManyRequests,
			successBody: `{"organic_results":[]}`,
			newBackend: func(client *http.Client) Searcher {
				return &SerpBase{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.URL.Query().Get("api_key") },
		},
		{
			name:        "youcom 401",
			status:      http.StatusUnauthorized,
			successBody: `{"results":{"web":[]}}`,
			newBackend: func(client *http.Client) Searcher {
				return &Youcom{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.Header.Get("X-API-Key") },
		},
		{
			name:        "youcom 429",
			status:      http.StatusTooManyRequests,
			successBody: `{"results":{"web":[]}}`,
			newBackend: func(client *http.Client) Searcher {
				return &Youcom{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.Header.Get("X-API-Key") },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = append(got, tc.requestKey(r))
				if len(got) == 1 {
					w.WriteHeader(tc.status)
					return
				}
				_, _ = fmt.Fprint(w, tc.successBody)
			}))
			defer server.Close()

			backend := tc.newBackend(rewrittenClient(server.URL))
			if _, err := backend.Search(context.Background(), "q", 1); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []string{"first", "second"}) {
				t.Fatalf("keys used = %v, want first then second", got)
			}
		})
	}
}

func TestBraveDoesNotRetryNonCredentialFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{name: "server error", code: http.StatusInternalServerError, body: "upstream failed"},
		{name: "decode error", code: http.StatusOK, body: "not-json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(tc.code)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			backend := &Brave{keys: deterministicPool("first", "second"), client: rewrittenClient(server.URL)}
			if _, err := backend.Search(context.Background(), "q", 1); err == nil {
				t.Fatal("expected search failure")
			}
			if got := attempts.Load(); got != 1 {
				t.Fatalf("attempts = %d, want no retry", got)
			}
		})
	}

	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "transport error", err: errors.New("transport failed")},
		{name: "cancellation", err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				attempts.Add(1)
				return nil, tc.err
			})}
			backend := &Brave{keys: deterministicPool("first", "second"), client: client}
			if _, err := backend.Search(context.Background(), "q", 1); err == nil {
				t.Fatal("expected search failure")
			}
			if got := attempts.Load(); got != 1 {
				t.Fatalf("attempts = %d, want no retry", got)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
