package scrape

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestScrapeConditionalRetriesOnTooManyRequests(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `<html><head><title>Ping Page</title></head><body><main><h1>Ping Page</h1><p>Body copy for extraction.</p></main></body></html>`)
	}))
	defer server.Close()

	scraper := New()
	result, err := scraper.ScrapeConditional(server.URL, "", "")
	if err != nil {
		t.Fatalf("ScrapeConditional() error = %v", err)
	}
	if result == nil || result.Page == nil {
		t.Fatal("ScrapeConditional() returned no page")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if result.Page.Title != "Ping Page" {
		t.Fatalf("title = %q, want %q", result.Page.Title, "Ping Page")
	}
}

func TestRequestPolicyForHostUsesPingThrottle(t *testing.T) {
	policy := requestPolicyForHost("docs.pingidentity.com")
	if policy.MinInterval != 200*time.Millisecond {
		t.Fatalf("MinInterval = %s, want %s", policy.MinInterval, 200*time.Millisecond)
	}
	if policy.Max429Retries != 0 {
		t.Fatalf("Max429Retries = %d, want 0", policy.Max429Retries)
	}
	if policy.MaxRetryAfter != time.Second {
		t.Fatalf("MaxRetryAfter = %s, want %s", policy.MaxRetryAfter, time.Second)
	}

	other := requestPolicyForHost("help.okta.com")
	if other.MinInterval != 0 {
		t.Fatalf("MinInterval = %s, want 0 for non-Ping hosts", other.MinInterval)
	}
}

func TestRetryAfterDelayHonorsMaxDelay(t *testing.T) {
	got := retryAfterDelay("30", 0, 3*time.Second)
	if got != 3*time.Second {
		t.Fatalf("retryAfterDelay() = %s, want %s", got, 3*time.Second)
	}
}

func TestDoRequestSpacesPingRequests(t *testing.T) {
	var slept []time.Duration
	currentTime := time.Unix(0, 0)
	scraper := New()
	scraper.sleep = func(delay time.Duration) {
		slept = append(slept, delay)
		currentTime = currentTime.Add(delay)
	}
	scraper.now = func() time.Time {
		return currentTime
	}
	scraper.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	for range 2 {
		req, err := http.NewRequest(http.MethodGet, "https://docs.pingidentity.com/example", nil)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		resp, err := scraper.doRequest(req)
		if err != nil {
			t.Fatalf("doRequest() error = %v", err)
		}
		resp.Body.Close()
	}

	if !slices.Contains(slept, 200*time.Millisecond) {
		t.Fatalf("slept = %v, want a 200ms Ping throttle delay", slept)
	}
	if got := scraper.nextByHost["docs.pingidentity.com"]; !got.After(time.Unix(0, 0)) {
		t.Fatalf("nextByHost not updated for Ping host: %v", got)
	}
}
