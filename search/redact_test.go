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
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/search"
)

const secretHeader = "CF-Access-Client-Secret"

// Two values: a plain token, and one that URL and path escaping both change.
var hostileSecrets = []string{"s3cret-value", "Bearer abc/def+ghi=="}

func leaks(s, secret string) bool {
	for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
		if strings.Contains(s, form) {
			return true
		}
	}
	return false
}

// An instance that echoes the secret it was sent in a body, a header, a
// malformed redirect, or the URL of a redirect the client follows must not
// get it into the error a search returns.
func TestInstanceErrorsRedactEverythingTheInstanceEchoes(t *testing.T) {
	for _, backend := range []string{"searxng", "firecrawl", "degoog"} {
		for i, secret := range hostileSecrets {
			for _, mode := range testutil.HostileModes {
				t.Run(fmt.Sprintf("%s/%s/%d", backend, mode, i), func(t *testing.T) {
					srv := testutil.HostileInstance(t, secretHeader, mode)
					cfg := config.Defaults()
					cfg.SetProvider(backend+"_url", srv.URL)
					cfg.HTTPHeaders = map[string]map[string]string{srv.URL: {secretHeader: secret}}

					s, err := search.NewFromConfig(&cfg, backend, "")
					if err != nil {
						t.Fatal(err)
					}
					_, err = s.Search(context.Background(), "q", 1)
					if err == nil {
						t.Fatal("search succeeded against a failing instance")
					}
					if leaks(err.Error(), secret) {
						t.Fatalf("error exposed a header value: %v", err)
					}
					// The client errors and Firecrawl's quoted body did hold the
					// value, so this exercises redaction rather than absence.
					if quoted := mode != testutil.EchoBody || backend == "firecrawl"; quoted && !strings.Contains(err.Error(), "[redacted]") {
						t.Fatalf("error = %v, want the echoed value redacted", err)
					}
				})
			}
		}
	}
}

// An instance can redirect to another configured origin, which is then sent
// its own headers. A value that origin echoes back is redacted too, though it
// was never configured for the instance itself.
func TestInstanceErrorsRedactValuesOfOriginsARedirectReaches(t *testing.T) {
	for _, backend := range []string{"searxng", "firecrawl", "degoog"} {
		for i, secret := range hostileSecrets {
			t.Run(fmt.Sprintf("%s/%d", backend, i), func(t *testing.T) {
				other := testutil.HostileInstance(t, secretHeader, testutil.EchoMalformedRedirect)
				instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, other.URL+"/", http.StatusFound)
				}))
				defer instance.Close()
				cfg := config.Defaults()
				cfg.SetProvider(backend+"_url", instance.URL)
				cfg.HTTPHeaders = map[string]map[string]string{
					instance.URL: {"X-Instance": "instance-value"},
					other.URL:    {secretHeader: secret},
				}

				s, err := search.NewFromConfig(&cfg, backend, "")
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.Search(context.Background(), "q", 1)
				if err == nil {
					t.Fatal("search succeeded against a failing instance")
				}
				if leaks(err.Error(), secret) || !strings.Contains(err.Error(), "[redacted]") {
					t.Fatalf("error = %v, want the other origin's value redacted", err)
				}
			})
		}
	}
}

// Firecrawl quotes the first 4096 bytes of an error body. A value that
// straddles that cut is redacted whole before it, not left as a fragment.
func TestFirecrawlRedactsBeforeTruncatingErrorBody(t *testing.T) {
	const secret = "s3cret-value"
	for _, offset := range []int{4096 - len(secret), 4096 - len(secret)/2, 4096 - 1} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(strings.Repeat("x", offset) + r.Header.Get(secretHeader) + " and more"))
			}))
			defer srv.Close()
			cfg := config.Defaults()
			cfg.SetProvider("firecrawl_url", srv.URL)
			cfg.HTTPHeaders = map[string]map[string]string{srv.URL: {secretHeader: secret}}

			s, err := search.NewFromConfig(&cfg, "firecrawl", "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Search(context.Background(), "q", 1)
			if err == nil {
				t.Fatal("search succeeded against a failing instance")
			}
			// What follows the padding is the redaction marker, possibly cut
			// short, and never the start of the value.
			msg := err.Error()
			tail := msg[strings.LastIndex(msg, "x")+1:]
			if !strings.HasPrefix(tail, "[redacted]") && !strings.HasPrefix("[redacted]", tail) {
				t.Fatalf("error continues %q after the padding, want the redaction marker", tail)
			}
		})
	}
}
