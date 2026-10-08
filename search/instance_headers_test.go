package search_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// Firecrawl's own bearer token wins over an Authorization header configured
// for its origin; the other configured headers still go out alongside it.
func TestFirecrawlKeepsBearerTokenWithOriginHeaders(t *testing.T) {
	type sent struct{ auth, secret string }
	seen := make(chan sent, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- sent{r.Header.Get("Authorization"), r.Header.Get("CF-Access-Client-Secret")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"web":[]}}`))
	}))
	defer srv.Close()
	cfg := config.Defaults()
	cfg.SetProvider("firecrawl_url", srv.URL)
	cfg.SetProvider("firecrawl_api_key", "fc-key")
	cfg.HTTPHeaders = map[string]map[string]string{srv.URL: {
		"Authorization":           "Bearer from-http-headers",
		"CF-Access-Client-Secret": "s3cret",
	}}

	s, err := search.NewFromConfig(&cfg, "firecrawl", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search(context.Background(), "q", 1); err != nil {
		t.Fatal(err)
	}
	p, _ := search.Lookup("firecrawl")
	p.Probe(context.Background(), httpx.Default(), &cfg)
	for _, call := range []string{"search", "doctor probe"} {
		if got := <-seen; got.auth != "Bearer fc-key" || got.secret != "s3cret" {
			t.Errorf("%s sent Authorization %q and secret %q", call, got.auth, got.secret)
		}
	}
}

// A failing instance whose error page echoes the request (as some auth
// proxies do) must not put a header value into the error ketch returns.
func TestInstanceErrorsNeverContainHeaderValues(t *testing.T) {
	for _, backend := range []string{"searxng", "firecrawl", "degoog"} {
		t.Run(backend, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("denied: " + r.Header.Get("CF-Access-Client-Secret")))
			}))
			defer srv.Close()
			cfg := config.Defaults()
			cfg.SetProvider(backend+"_url", srv.URL)
			cfg.HTTPHeaders = map[string]map[string]string{srv.URL: {"CF-Access-Client-Secret": "s3cret"}}

			s, err := search.NewFromConfig(&cfg, backend, "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Search(context.Background(), "q", 1)
			if err == nil || strings.Contains(err.Error(), "s3cret") {
				t.Fatalf("error = %v, want one without the header value", err)
			}
		})
	}
}
