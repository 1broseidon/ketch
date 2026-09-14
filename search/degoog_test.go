package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

const degoogFixture = `{"results":[
  {"title":"Go Docs","url":"https://golang.org/doc/","snippet":"Go documentation"},
  {"title":"blank url is skipped","url":"  ","snippet":"x"},
  {"title":"Go Blog","url":"https://go.dev/blog/","snippet":"The Go Blog"},
  {"title":"Go Packages","url":"https://pkg.go.dev/","snippet":"Package index"}
],"query":"test query","totalTime":12,"type":"web"}`

func degoogServer(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var seen http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestDegoogSearchParsesAndSkipsBlankURLs(t *testing.T) {
	srv, seen := degoogServer(t, http.StatusOK, degoogFixture)
	results, err := NewDegoog(srv.URL+"/").Search(context.Background(), "test query", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if seen.URL.Path != "/api/search" || seen.URL.Query().Get("q") != "test query" || seen.Method != http.MethodGet {
		t.Errorf("request = %s %s?%s, want GET /api/search?q=test+query", seen.Method, seen.URL.Path, seen.URL.RawQuery)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3 (blank URL skipped): %+v", len(results), results)
	}
	if results[0].Title != "Go Docs" || results[0].URL != "https://golang.org/doc/" || results[0].Description != "Go documentation" {
		t.Errorf("first result = %+v", results[0])
	}
	if results[0].Content != "" {
		t.Errorf("degoog has no extracted content; Content = %q", results[0].Content)
	}
}

func TestDegoogSearchRespectsLimit(t *testing.T) {
	srv, _ := degoogServer(t, http.StatusOK, degoogFixture)
	results, err := NewDegoog(srv.URL).Search(context.Background(), "q", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	zero, err := NewDegoog(srv.URL).Search(context.Background(), "q", 0)
	if err != nil || len(zero) != 0 {
		t.Fatalf("limit 0: results=%v err=%v, want empty and nil", zero, err)
	}
}

func TestDegoogSearchErrors(t *testing.T) {
	srv, _ := degoogServer(t, http.StatusBadGateway, "upstream down")
	if _, err := NewDegoog(srv.URL).Search(context.Background(), "q", 5); err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("HTTP 502: err = %v, want status in error", err)
	}
	srv2, _ := degoogServer(t, http.StatusOK, "<!doctype html><html>not json")
	if _, err := NewDegoog(srv2.URL).Search(context.Background(), "q", 5); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("non-JSON body: err = %v, want decode error", err)
	}
}

func TestProbeDegoog(t *testing.T) {
	ctx := context.Background()
	// Same contract as SearXNG: a selected backend with no URL must fail
	// doctor rather than pass with a skipped row.
	if status, detail := ProbeDegoog(ctx, http.DefaultClient, ""); status != health.StatusMisconfigured || !strings.Contains(detail, "degoog_url not set") {
		t.Errorf("unset URL: %s %q, want misconfigured with the config hint", status, detail)
	}
	cases := []struct {
		name   string
		code   int
		body   string
		want   health.Status
		detail string
	}{
		{"ok", http.StatusOK, degoogFixture, health.StatusOK, ""},
		{"not degoog", http.StatusOK, "<html>", health.StatusMisconfigured, "non-JSON"},
		{"api key required", http.StatusUnauthorized, "", health.StatusMisconfigured, "API key"},
		{"rate limited", http.StatusTooManyRequests, "", health.StatusMisconfigured, "rate limited"},
		{"server error", http.StatusInternalServerError, "", health.StatusUnreachable, "500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := degoogServer(t, tc.code, tc.body)
			status, detail := ProbeDegoog(ctx, srv.Client(), srv.URL+"/")
			if status != tc.want || !strings.Contains(detail, tc.detail) {
				t.Errorf("status=%s detail=%q, want %s containing %q", status, detail, tc.want, tc.detail)
			}
			if seen.URL.Path != "/api/search" {
				t.Errorf("probe path = %q, want /api/search", seen.URL.Path)
			}
		})
	}
}

// The registry owns the opt-in rule: no degoog_url means not usable, not a
// request to localhost.
func TestDegoogRegistryIsOptIn(t *testing.T) {
	cfg := config.Defaults()
	if _, err := NewFromConfig(&cfg, "degoog", ""); err == nil || !strings.Contains(err.Error(), "degoog_url") {
		t.Fatalf("unset degoog_url: err = %v, want setup hint", err)
	}
	p, ok := Lookup("degoog")
	if !ok || p.Usable(&cfg) || p.Required(&cfg) {
		t.Fatalf("unset: usable=%v required=%v, want both false", p.Usable(&cfg), p.Required(&cfg))
	}
	srv, seen := degoogServer(t, http.StatusOK, degoogFixture)
	cfg.SetProvider("degoog_url", srv.URL)
	if !p.Usable(&cfg) || !p.Required(&cfg) {
		t.Fatalf("configured: usable=%v required=%v, want both true", p.Usable(&cfg), p.Required(&cfg))
	}
	backend, err := NewFromConfig(&cfg, "degoog", "")
	if err != nil {
		t.Fatal(err)
	}
	results, err := backend.Search(context.Background(), "registry", 1)
	if err != nil || len(results) != 1 || seen.URL.Path != "/api/search" {
		t.Fatalf("registry-built backend: results=%v err=%v path=%q", results, err, seen.URL.Path)
	}
}
