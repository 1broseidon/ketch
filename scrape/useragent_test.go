package scrape

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
)

func TestDefaultUserAgentIsHonest(t *testing.T) {
	t.Parallel()

	ua := DefaultUserAgent()
	if !strings.HasPrefix(ua, "ketch/") {
		t.Fatalf("DefaultUserAgent = %q, want ketch/<version> prefix", ua)
	}
	if !strings.Contains(ua, "+https://github.com/1broseidon/ketch") {
		t.Fatalf("DefaultUserAgent = %q, want project URL", ua)
	}
	if strings.Contains(ua, "Mozilla") || strings.Contains(ua, "compatible") {
		t.Fatalf("DefaultUserAgent = %q impersonates a browser", ua)
	}
}

func TestFetchSendsConfiguredUserAgent(t *testing.T) {
	t.Parallel()

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("<html><body><p>hi</p></body></html>"))
	}))
	t.Cleanup(srv.Close)

	s, err := NewFromConfig(&config.Config{UserAgent: "ketch-test/1 (+https://example.com)"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	if _, err := s.FetchContent(t.Context(), srv.URL); err != nil {
		t.Fatalf("FetchContent: %v", err)
	}
	if got != "ketch-test/1 (+https://example.com)" {
		t.Fatalf("User-Agent = %q, want configured override", got)
	}
}

func TestFetchSendsDefaultUserAgentWhenUnset(t *testing.T) {
	t.Parallel()

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("<html><body><p>hi</p></body></html>"))
	}))
	t.Cleanup(srv.Close)

	s := New()
	if _, err := s.FetchContent(t.Context(), srv.URL); err != nil {
		t.Fatalf("FetchContent: %v", err)
	}
	if got != DefaultUserAgent() {
		t.Fatalf("User-Agent = %q, want %q", got, DefaultUserAgent())
	}
}

func TestNewFromConfigRejectsControlCharactersInUserAgent(t *testing.T) {
	t.Parallel()

	_, err := NewFromConfig(&config.Config{UserAgent: "ketch/1\r\nX-Injected: 1"})
	if err == nil {
		t.Fatal("expected error for control characters")
	}
	if !strings.Contains(err.Error(), "user_agent") {
		t.Fatalf("error = %q, want user_agent mention", err)
	}
}

func TestNormalizeUserAgentEmptyFallsBackToDefault(t *testing.T) {
	t.Parallel()

	got, err := normalizeUserAgent("   ")
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultUserAgent() {
		t.Fatalf("normalizeUserAgent(blank) = %q, want %q", got, DefaultUserAgent())
	}
}
