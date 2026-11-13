package cmd

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/testutil"
)

// These run `ketch docs -b context7` in-process against keyless Context7,
// every request answered from Context7's recorded keyless answers
// (docs/testdata/context7).

// context7Requests are the requests a run sent to Context7.
func context7Requests(ft *testutil.FixtureTransport) []testutil.FixtureCall {
	var out []testutil.FixtureCall
	for _, c := range ft.Calls() {
		if strings.Contains(c.URL, "://context7.com/") {
			out = append(out, c)
		}
	}
	return out
}

// Context7 with no key: --backend context7 answers from the recorded
// keyless response, with no Authorization header and no other host asked.
func TestDocsContext7KeylessRecorded(t *testing.T) {
	for _, backend := range []string{"context7"} {
		t.Run(backend, func(t *testing.T) {
			ft := testutil.LoadFixtures(t, filepath.Join("..", "docs", "testdata", "context7"))
			docsEnv(t, ft)

			out, err := docsCLI(t, "-b", backend, "--library", "/reactjs/react.dev", "--tokens", "300", "useState")
			if err != nil {
				t.Fatalf("exit %d: %v", exitCode(err), err)
			}
			if !strings.Contains(out, "backend: context7\n") || !strings.Contains(out, "[useState(initialState)]") {
				t.Errorf("output:\n%s", out)
			}
			calls := ft.Calls()
			if len(calls) != 1 || len(context7Requests(ft)) != 1 {
				t.Fatalf("requests = %v, want the one Context7 docs request", calls)
			}
			if _, ok := calls[0].Header["Authorization"]; ok {
				t.Errorf("keyless request sent Authorization %q", calls[0].Header.Get("Authorization"))
			}
		})
	}
}

// A spent quota is exit 4 naming the reset and the key fix. The key is
// this test's own: the process remembers a spent quota per key until it
// resets, and other tests here run keyless or with "test-key".
func TestDocsContext7RateLimitedExitsUpstream(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://context7.com/api/v1/search?query=react+hooks", testutil.Fixture{
		Status: http.StatusTooManyRequests,
		Header: http.Header{"Ratelimit-Limit": {"200"}, "Ratelimit-Remaining": {"0"}, "Ratelimit-Reset": {"1796083200"}},
	})
	docsEnv(t, ft, func(c *config.Config) { c.SetProvider("context7_api_key", "rate-limit-fixture-key") })

	_, err := docsCLI(t, "-b", "context7", "react hooks")
	if code := exitCode(err); code != ExitUpstream {
		t.Fatalf("exit %d, want %d: %v", code, ExitUpstream, err)
	}
	var rl *docs.RateLimitError
	if !errors.As(err, &rl) || !strings.Contains(err.Error(), "resets 2026-12-01 00:00 UTC") || !strings.Contains(err.Error(), "ketch config set context7_api_key <key>") {
		t.Errorf("error %v should name the reset and the key fix", err)
	}
}
