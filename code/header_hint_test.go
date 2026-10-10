package code_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
)

const (
	hintID     = "id-value"
	hintSecret = "s3cret-value"
)

func accessHeaders(origin string) map[string]map[string]string {
	return map[string]map[string]string{origin: {"CF-Access-Client-Secret": hintSecret, "CF-Access-Client-Id": hintID}}
}

func probeSourcegraph(t *testing.T, instanceURL string, headers map[string]map[string]string) (health.Status, string) {
	t.Helper()
	cfg := config.Defaults()
	cfg.SetProvider("sourcegraph_url", instanceURL)
	cfg.HTTPHeaders = headers
	p, _ := code.Lookup("sourcegraph")
	return p.Probe(context.Background(), httpx.Default(), &cfg)
}

// An auth proxy rejecting a self-hosted Sourcegraph fails doctor only when
// http_headers are configured for its origin; otherwise the reachability
// probe still passes, as it always has.
func TestSourcegraphProbeHintsAtHeadersWhenAuthProxyRejects(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			// The proxy echoes the secret it was sent, as some error pages do.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.Header.Get("CF-Access-Client-Secret")
				w.Header().Set("X-Echo", got)
				w.WriteHeader(status)
				fmt.Fprintf(w, "<html>denied: CF-Access-Client-Secret=%s</html>", got)
			}))
			defer srv.Close()

			got, detail := probeSourcegraph(t, srv.URL, accessHeaders(srv.URL))
			want := fmt.Sprintf("rejected (HTTP %d); http_headers are set for %s (Cf-Access-Client-Id, Cf-Access-Client-Secret), check they are current", status, srv.URL)
			if got != health.StatusMisconfigured || detail != want {
				t.Fatalf("probe = %s (%s), want misconfigured (%s)", got, detail, want)
			}
			if strings.Contains(detail, hintSecret) || strings.Contains(detail, hintID) {
				t.Fatalf("detail exposed a header value: %q", detail)
			}

			for name, headers := range map[string]map[string]map[string]string{
				"none":         nil,
				"other origin": accessHeaders("https://other.example"),
			} {
				if got, detail := probeSourcegraph(t, srv.URL, headers); got != health.StatusOK || detail != "" {
					t.Errorf("http_headers %s: probe = %s (%s), want ok with no detail", name, got, detail)
				}
			}
		})
	}
}

// A redirect to a login page on another origin is followed without the
// headers and answers 200, which must not pass for a healthy instance.
func TestSourcegraphProbeHintsAtHeadersWhenRedirectedToAnotherOrigin(t *testing.T) {
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>sign in</html>"))
	}))
	defer login.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, login.URL+"/login", http.StatusFound)
	}))
	defer srv.Close()
	loginURL, _ := url.Parse(login.URL)

	got, detail := probeSourcegraph(t, srv.URL, accessHeaders(srv.URL))
	want := fmt.Sprintf("redirected to %s (an auth login page?) — check http_headers for %s", loginURL.Host, srv.URL)
	if got != health.StatusMisconfigured || detail != want {
		t.Fatalf("probe = %s (%s), want misconfigured (%s)", got, detail, want)
	}
	if got, detail := probeSourcegraph(t, srv.URL, nil); got != health.StatusOK || detail != "" {
		t.Fatalf("probe without http_headers = %s (%s), want ok with no detail", got, detail)
	}
}
