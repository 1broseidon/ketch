package doctor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/httpx"
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
