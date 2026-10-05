package code_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
)

// A self-hosted Sourcegraph behind an auth proxy gets the operator's
// http_headers on searches and doctor probes.
func TestSourcegraphSendsOriginHeaders(t *testing.T) {
	seen := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("CF-Access-Client-Secret")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: done\ndata: {}\n\n"))
	}))
	defer srv.Close()
	cfg := config.Defaults()
	cfg.SetProvider("sourcegraph_url", srv.URL)
	cfg.HTTPHeaders = map[string]map[string]string{srv.URL: {"CF-Access-Client-Secret": "s3cret"}}

	s, err := code.NewFromConfig(&cfg, "sourcegraph")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search(context.Background(), code.Query{Term: "q", Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "s3cret" {
		t.Errorf("search sent secret %q", got)
	}

	p, _ := code.Lookup("sourcegraph")
	if status, detail := p.Probe(context.Background(), httpx.Default(), &cfg); status != health.StatusOK {
		t.Fatalf("probe = %s (%s)", status, detail)
	}
	if got := <-seen; got != "s3cret" {
		t.Errorf("doctor probe sent secret %q", got)
	}
}
