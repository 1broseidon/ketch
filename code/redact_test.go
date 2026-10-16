package code_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/internal/testutil"
)

// A Sourcegraph instance that echoes the secret it was sent, in a body, a
// header, a malformed redirect, or the URL of a redirect the client follows,
// must not get it into the error a search returns.
func TestSourcegraphErrorsRedactEverythingTheInstanceEchoes(t *testing.T) {
	const header = "CF-Access-Client-Secret"
	for i, secret := range []string{"s3cret-value", "Bearer abc/def+ghi=="} {
		for _, mode := range testutil.HostileModes {
			t.Run(fmt.Sprintf("%s/%d", mode, i), func(t *testing.T) {
				srv := testutil.HostileInstance(t, header, mode)
				cfg := config.Defaults()
				cfg.SetProvider("sourcegraph_url", srv.URL)
				cfg.HTTPHeaders = map[string]map[string]string{srv.URL: {header: secret}}

				s, err := code.NewFromConfig(&cfg, "sourcegraph")
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.Search(context.Background(), code.Query{Term: "q", Limit: 1})
				if err == nil {
					t.Fatal("search succeeded against a failing instance")
				}
				for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
					if strings.Contains(err.Error(), form) {
						t.Fatalf("error exposed a header value: %v", err)
					}
				}
				// A redirect puts the value in the client's own error; the 500 never
				// quotes the body.
				if mode != testutil.EchoBody && !strings.Contains(err.Error(), "[redacted]") {
					t.Fatalf("error = %v, want the echoed value redacted", err)
				}
			})
		}
	}
}
