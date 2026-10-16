package search_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/search"
)

const (
	hintID     = "id-value"
	hintSecret = "s3cret-value"
)

// authProxy rejects every request with status, echoing the secret it was sent
// in a response header and the body, as some auth proxies' error pages do.
func authProxy(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("CF-Access-Client-Secret")
		w.Header().Set("X-Echo", got)
		w.WriteHeader(status)
		fmt.Fprintf(w, "<html>denied: CF-Access-Client-Secret=%s</html>", got)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func accessHeaders(origin string) map[string]map[string]string {
	return map[string]map[string]string{origin: {"CF-Access-Client-Secret": hintSecret, "CF-Access-Client-Id": hintID}}
}

func probeInstance(t *testing.T, backend, instanceURL string, headers map[string]map[string]string) (health.Status, string) {
	t.Helper()
	cfg := config.Defaults()
	cfg.SetProvider(backend+"_url", instanceURL)
	cfg.HTTPHeaders = headers
	p, ok := search.Lookup(backend)
	if !ok {
		t.Fatalf("no provider %q", backend)
	}
	return p.Probe(context.Background(), httpx.Default(), &cfg)
}

// What each probe says about a 401/403 when no http_headers apply to the
// instance. These strings predate the hint and must not change.
var rejectedWithoutHeaders = map[string]map[int]struct {
	status health.Status
	detail string
}{
	"searxng": {
		http.StatusUnauthorized: {health.StatusUnreachable, "returned status 401"},
		http.StatusForbidden:    {health.StatusMisconfigured, `format=json is blocked (HTTP 403) — enable it in the instance's settings.yml: search.formats must include "json", then restart SearXNG`},
	},
	"firecrawl": {
		http.StatusUnauthorized: {health.StatusMisconfigured, "instance requires an API key (ketch config set firecrawl_api_key <key>)"},
		http.StatusForbidden:    {health.StatusMisconfigured, "instance requires an API key (ketch config set firecrawl_api_key <key>)"},
	},
	"degoog": {
		http.StatusUnauthorized: {health.StatusMisconfigured, "returned status 401 — the instance requires an API key for /api/search, which ketch does not send"},
		http.StatusForbidden:    {health.StatusMisconfigured, "returned status 403 — the instance requires an API key for /api/search, which ketch does not send"},
	},
}

// A 401/403 from an instance with headers configured leads with the headers,
// naming the origin and the header names and never a value. The proxy and the
// instance are indistinguishable here, so the probe's own advice follows.
func TestProbeHintsAtHeadersWhenAuthProxyRejects(t *testing.T) {
	for backend, byStatus := range rejectedWithoutHeaders {
		for code, without := range byStatus {
			t.Run(fmt.Sprintf("%s/%d", backend, code), func(t *testing.T) {
				srv := authProxy(t, code)
				status, detail := probeInstance(t, backend, srv.URL, accessHeaders(srv.URL))
				want := fmt.Sprintf("rejected (HTTP %d); http_headers are set for %s (Cf-Access-Client-Id, Cf-Access-Client-Secret), check they are current", code, srv.URL)
				if without.status == health.StatusMisconfigured {
					want += "; otherwise: " + without.detail
				}
				if status != health.StatusMisconfigured || detail != want {
					t.Fatalf("probe = %s (%s), want misconfigured (%s)", status, detail, want)
				}
				if strings.Contains(detail, hintSecret) || strings.Contains(detail, hintID) {
					t.Fatalf("detail exposed a header value: %q", detail)
				}
			})
		}
	}
}

// Without http_headers for the instance's origin, a 401/403 reads exactly as
// it did before the hint existed.
func TestProbeRejectionUnchangedWithoutHeadersForOrigin(t *testing.T) {
	for backend, byStatus := range rejectedWithoutHeaders {
		for code, want := range byStatus {
			for name, headers := range map[string]map[string]map[string]string{
				"none":         nil,
				"other origin": accessHeaders("https://other.example"),
			} {
				t.Run(fmt.Sprintf("%s/%d/%s", backend, code, name), func(t *testing.T) {
					srv := authProxy(t, code)
					status, detail := probeInstance(t, backend, srv.URL, headers)
					if status != want.status || detail != want.detail {
						t.Fatalf("probe = %s (%s), want %s (%s)", status, detail, want.status, want.detail)
					}
				})
			}
		}
	}
}

// loginRedirect returns an instance that sends every request to an HTML login
// page on another origin, as Cloudflare Access does without valid credentials.
func loginRedirect(t *testing.T) (instance, login *httptest.Server) {
	t.Helper()
	login = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>sign in</html>"))
	}))
	t.Cleanup(login.Close)
	instance = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, login.URL+"/login", http.StatusFound)
	}))
	t.Cleanup(instance.Close)
	return instance, login
}

// A redirect to another origin lands without the headers; with headers
// configured the probe says so instead of doubting the backend.
func TestProbeHintsAtHeadersWhenRedirectedToAnotherOrigin(t *testing.T) {
	for backend, without := range map[string]struct {
		status health.Status
		detail string
	}{
		"searxng":   {health.StatusMisconfigured, "returned non-JSON response — is %s a SearXNG instance?"},
		"firecrawl": {health.StatusOK, "reachable (liveness only — a real search is not run)"},
		"degoog":    {health.StatusMisconfigured, "returned non-JSON response — is %s a degoog instance?"},
	} {
		t.Run(backend, func(t *testing.T) {
			instance, login := loginRedirect(t)
			loginURL, _ := url.Parse(login.URL)

			status, detail := probeInstance(t, backend, instance.URL, accessHeaders(instance.URL))
			want := fmt.Sprintf("redirected to %s (an auth login page?) — check http_headers for %s", loginURL.Host, instance.URL)
			if status != health.StatusMisconfigured || detail != want {
				t.Fatalf("probe = %s (%s), want misconfigured (%s)", status, detail, want)
			}

			status, detail = probeInstance(t, backend, instance.URL, nil)
			if strings.Contains(without.detail, "%s") {
				without.detail = fmt.Sprintf(without.detail, instance.URL)
			}
			if status != without.status || detail != without.detail {
				t.Fatalf("probe without http_headers = %s (%s), want %s (%s)", status, detail, without.status, without.detail)
			}
		})
	}
}
