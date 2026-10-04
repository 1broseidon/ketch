package search_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/search"
)

// instance answers like SearXNG, Firecrawl and Degoog at once and records the
// Cloudflare Access secret each request carried.
func instance(t *testing.T) (*httptest.Server, chan string) {
	t.Helper()
	seen := make(chan string, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("CF-Access-Client-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"web":[]},"results":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func TestSelfHostedProvidersSendOriginHeaders(t *testing.T) {
	for _, backend := range []string{"searxng", "firecrawl", "degoog"} {
		t.Run(backend, func(t *testing.T) {
			srv, seen := instance(t)
			cfg := config.Defaults()
			cfg.SetProvider(backend+"_url", srv.URL)
			cfg.HTTPHeaders = map[string]map[string]string{srv.URL: {"CF-Access-Client-Secret": "s3cret"}}

			s, err := search.NewFromConfig(&cfg, backend, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Search(context.Background(), "q", 1); err != nil {
				t.Fatal(err)
			}
			if got := <-seen; got != "s3cret" {
				t.Errorf("search sent secret %q", got)
			}

			p, _ := search.Lookup(backend)
			if status, detail := p.Probe(context.Background(), httpx.Default(), &cfg); status != health.StatusOK {
				t.Fatalf("probe = %s (%s)", status, detail)
			}
			if got := <-seen; got != "s3cret" {
				t.Errorf("doctor probe sent secret %q", got)
			}
		})
	}
}

// A per-call searxng_url (CLI --searxng-url, MCP searxng_url) pointing at
// another origin is searched without the configured instance's headers.
func TestSearxngOverrideToOtherOriginGetsNoHeaders(t *testing.T) {
	configured, _ := instance(t)
	other, seen := instance(t)
	cfg := config.Defaults()
	cfg.SetProvider("searxng_url", configured.URL)
	cfg.HTTPHeaders = map[string]map[string]string{configured.URL: {"CF-Access-Client-Secret": "s3cret"}}

	s, err := search.NewFromConfig(&cfg, "searxng", other.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search(context.Background(), "q", 1); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "" {
		t.Fatalf("override origin received secret %q", got)
	}
}
