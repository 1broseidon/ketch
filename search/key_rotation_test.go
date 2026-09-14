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

func TestSerpBaseTransportErrorsNeverExposeKey(t *testing.T) {
	tests := []struct {
		name      string
		cause     error
		wantCause error
	}{
		{name: "transport", cause: errors.New("dial failure")},
		{name: "cancelled", cause: context.Canceled, wantCause: context.Canceled},
		{name: "deadline", cause: context.DeadlineExceeded, wantCause: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const secret = "serpbase-transport-secret"
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("transport failure for %s: %w", req.URL.String(), test.cause)
			})}
			backend := &SerpBase{keys: deterministicPool(secret), client: client}
			_, err := backend.Search(context.Background(), "q", 1)
			if err == nil {
				t.Fatal("expected a transport error")
			}
			// The key rides in a header, so the URL echoed by the transport error
			// never carries it — unlike Exa, which needs safeEXARequestError.
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "X-API-Key") {
				t.Fatalf("SerpBase transport error exposed the API key: %q", err)
			}
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatal("transport error lost the cancellation error class")
			}
		})
	}
}

func TestSerpBaseErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		httpStatus int
		body       string
		want       string
	}{
		{"invalid key", http.StatusOK, `{"status":1001,"error":"unauthorized"}`, "serpbase: invalid API key (key 1 of 1"},
		{"credits", http.StatusOK, `{"status":1020,"error":"insufficient credits"}`, "serpbase: search credits exhausted"},
		{"rate limited", http.StatusOK, `{"status":1029,"error":"rate limited"}`, "serpbase: rate limited"},
		{"invalid request", http.StatusOK, `{"status":1000,"error":"invalid request"}`, "serpbase: invalid request: invalid request"},
		{"server", http.StatusOK, `{"status":1500,"error":"boom"}`, "serpbase returned status 1500: boom"},
		{"http 401", http.StatusUnauthorized, `unauthorized`, "serpbase: invalid API key (key 1 of 1"},
		{"http 402", http.StatusPaymentRequired, ``, "serpbase: search credits exhausted"},
		{"http 403", http.StatusForbidden, `forbidden`, "serpbase: invalid API key (key 1 of 1"},
		{"http 429", http.StatusTooManyRequests, ``, "serpbase: rate limited"},
		{"http 500", http.StatusInternalServerError, `boom`, "serpbase returned status 500: boom"},
		{"non-200 with status 0 is not success", http.StatusInternalServerError, `{"status":0}`, "serpbase returned status 500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.httpStatus != http.StatusOK {
					w.WriteHeader(tc.httpStatus)
				}
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			backend := &SerpBase{keys: deterministicPool("only"), client: rewrittenClient(server.URL)}
			_, err := backend.Search(context.Background(), "q", 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
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
		errorBody   string
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
			name:        "serpbase 1001",
			errorBody:   `{"status":1001,"error":"unauthorized"}`,
			successBody: `{"status":0,"organic":[]}`,
			newBackend: func(client *http.Client) Searcher {
				return &SerpBase{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.Header.Get("X-API-Key") },
		},
		{
			name:        "serpbase 1029",
			errorBody:   `{"status":1029,"error":"rate limited"}`,
			successBody: `{"status":0,"organic":[]}`,
			newBackend: func(client *http.Client) Searcher {
				return &SerpBase{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.Header.Get("X-API-Key") },
		},
		{
			name:        "serpbase 403",
			status:      http.StatusForbidden,
			successBody: `{"status":0,"organic":[]}`,
			newBackend: func(client *http.Client) Searcher {
				return &SerpBase{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return r.Header.Get("X-API-Key") },
		},
		{
			name:        "youcom 401",
			status:      http.StatusUnauthorized,
			successBody: "data: {\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"{\\\"results\\\":{\\\"web\\\":[]}}\"}]}}\n",
			newBackend: func(client *http.Client) Searcher {
				return &Youcom{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") },
		},
		{
			name:        "youcom 429",
			status:      http.StatusTooManyRequests,
			successBody: "data: {\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"{\\\"results\\\":{\\\"web\\\":[]}}\"}]}}\n",
			newBackend: func(client *http.Client) Searcher {
				return &Youcom{keys: deterministicPool("first", "second"), client: client}
			},
			requestKey: func(r *http.Request) string { return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = append(got, tc.requestKey(r))
				if len(got) == 1 {
					if tc.errorBody != "" {
						_, _ = fmt.Fprint(w, tc.errorBody)
						return
					}
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
