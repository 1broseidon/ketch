package doctor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/internal/testutil"
)

// Doctor probes for every instance backend send the configured headers, and
// no check detail ever contains a header value — even when the instance
// echoes it back in its response, as some auth proxies' error pages do.
func TestDoctorNeverPrintsHTTPHeaderValues(t *testing.T) {
	const secret = "s3cret-value"
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		for _, tc := range []struct{ surface, backend string }{
			{"search", "searxng"}, {"search", "firecrawl"}, {"search", "degoog"}, {"code", "sourcegraph"},
		} {
			t.Run(fmt.Sprintf("%s/%d", tc.backend, status), func(t *testing.T) {
				received := make(chan string, 4)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					got := r.Header.Get("CF-Access-Client-Secret")
					received <- got
					w.Header().Set("X-Echo", got)
					w.WriteHeader(status)
					fmt.Fprintf(w, "<html>denied: CF-Access-Client-Secret=%s</html>", got)
				}))
				defer srv.Close()

				cfg := config.Defaults()
				cfg.SetProvider(tc.backend+"_url", srv.URL)
				cfg.HTTPHeaders = map[string]map[string]string{srv.URL: {"CF-Access-Client-Secret": secret}}
				status, detail := findSpec(t, buildSpecs(&cfg, httpx.Default()), tc.surface, tc.backend).probe(context.Background())
				if got := <-received; got != secret {
					t.Fatalf("probe sent %q, want the configured header", got)
				}
				if strings.Contains(detail, secret) || strings.Contains(string(status), secret) {
					t.Fatalf("doctor detail exposed a header value: %q", detail)
				}
			})
		}
	}
}

// An instance that echoes the secret it was sent, in a body, a header, a
// malformed redirect, or the URL of a redirect the client follows, must not
// get it into a doctor detail, for any instance backend.
func TestDoctorRedactsEverythingTheInstanceEchoes(t *testing.T) {
	const header = "CF-Access-Client-Secret"
	for _, tc := range []struct{ surface, backend string }{
		{"search", "searxng"}, {"search", "firecrawl"}, {"search", "degoog"}, {"code", "sourcegraph"},
	} {
		for i, secret := range []string{"s3cret-value", "Bearer abc/def+ghi=="} {
			for _, mode := range testutil.HostileModes {
				t.Run(fmt.Sprintf("%s/%s/%d", tc.backend, mode, i), func(t *testing.T) {
					srv := testutil.HostileInstance(t, header, mode)
					cfg := config.Defaults()
					cfg.SetProvider(tc.backend+"_url", srv.URL)
					cfg.HTTPHeaders = map[string]map[string]string{srv.URL: {header: secret}}

					_, detail := findSpec(t, buildSpecs(&cfg, httpx.Default()), tc.surface, tc.backend).probe(context.Background())
					for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
						if strings.Contains(detail, form) {
							t.Fatalf("doctor detail exposed a header value: %q", detail)
						}
					}
					// A redirect puts the value in the client's own error.
					if mode != testutil.EchoBody && !strings.Contains(detail, "[redacted]") {
						t.Fatalf("detail = %q, want the echoed value redacted", detail)
					}
				})
			}
		}
	}
}
