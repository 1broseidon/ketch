package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/testutil"
)

// These run `ketch docs` in-process against the auto backend and keyless
// Context7, every request answered from fixtures: the synthetic widget
// world (testdata/docsresolve), whose name web finds ambiguous between
// npm's and PyPI's, and Context7's recorded keyless answers
// (docs/testdata/context7).

// widgetContext7 adds Context7 answering "widget configure themes" with
// pick, and docs for pick.
func widgetContext7(t *testing.T, pick string) *testutil.FixtureTransport {
	t.Helper()
	ft := widgetFixtures(t)
	ft.Handle(http.MethodGet, "https://context7.com/api/v1/search?query=widget+configure+themes", testutil.Fixture{
		Header: http.Header{"Ratelimit-Limit": {"200"}, "Ratelimit-Remaining": {"150"}, "Ratelimit-Reset": {"1796083200"}},
		Body:   `{"results":[{"id":"` + pick + `","title":"Widget","totalSnippets":12,"trustScore":7}]}`,
	})
	ft.Handle(http.MethodGet, "https://context7.com/api/v2/context?libraryId="+pick+"&query=widget+configure+themes&type=json&tokens=4000", testutil.Fixture{
		Body: `{"codeSnippets":[{"codeTitle":"Configure themes","codeId":"https://widget.readthedocs.io/en/stable/themes/","codeList":[{"code":"widget.theme('dark')"}]}],"infoSnippets":[]}`,
	})
	return ft
}

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

// Web finds "widget" ambiguous; auto asks Context7, whose pick is PyPI's
// widget (github:pywidget/widget, one of web's candidates), so Context7
// answers, keyless, and the output says so.
func TestDocsAutoAmbiguousSettledByWebCandidate(t *testing.T) {
	ft := widgetContext7(t, "/pywidget/widget")
	docsEnv(t, fixtureFallback{ft})

	var out string
	stderr, err := captureStderr(t, func() error {
		var runErr error
		out, runErr = docsCLI(t, "-b", "auto", "widget configure themes")
		return runErr
	})
	if err != nil {
		t.Fatalf("auto: exit %d: %v", exitCode(err), err)
	}
	if !strings.Contains(out, "backend: context7\n") || !strings.Contains(out, "library: /pywidget/widget\n") || !strings.Contains(out, "[Configure themes]") {
		t.Errorf("auto output does not show Context7's answer:\n%s", out)
	}
	if !strings.Contains(stderr, "warn: web refused (ambiguous): ambiguous library") {
		t.Errorf("stderr should warn about web's refusal, got %q", stderr)
	}
	reqs := context7Requests(ft)
	if len(reqs) != 2 {
		t.Fatalf("Context7 requests = %v, want resolve then docs", reqs)
	}
	for _, r := range reqs {
		if _, ok := r.Header["Authorization"]; ok {
			t.Errorf("keyless request %s sent Authorization", r.URL)
		}
	}

	_, err = captureStderr(t, func() error {
		var runErr error
		out, runErr = docsCLI(t, "--json", "-b", "auto", "widget configure themes")
		return runErr
	})
	var results []docs.Result
	if err != nil || json.Unmarshal([]byte(out), &results) != nil || len(results) != 1 || results[0].Source != "context7" {
		t.Errorf("--json: %v %s", err, out)
	}
}

// Context7 picking a project that is none of web's candidates leaves the
// ambiguity error standing (exit 2), naming the candidates and the pick,
// and no Context7 docs are fetched.
func TestDocsAutoAmbiguousForeignPickRefused(t *testing.T) {
	ft := widgetContext7(t, "/someone/widget")
	docsEnv(t, fixtureFallback{ft})

	_, err := docsCLI(t, "-b", "auto", "widget configure themes")
	if code := exitCode(err); code != ExitValidation {
		t.Fatalf("exit %d, want %d: %v", code, ExitValidation, err)
	}
	var amb *docs.AmbiguousLibraryError
	if !errors.As(err, &amb) || !strings.Contains(err.Error(), "context7 picked /someone/widget") || !strings.Contains(err.Error(), "pypi:widget") {
		t.Errorf("error %v should be the ambiguity, naming the candidates and Context7's pick", err)
	}
	if reqs := context7Requests(ft); len(reqs) != 1 {
		t.Errorf("Context7 requests = %v, want the resolve alone", reqs)
	}
}

// An explicit provider is never chained: web's ambiguity is the answer for
// -b web, and Context7 is not asked.
func TestDocsExplicitBackendNotChained(t *testing.T) {
	ft := widgetContext7(t, "/pywidget/widget")
	docsEnv(t, fixtureFallback{ft})

	_, err := docsCLI(t, "-b", "web", "widget configure themes")
	if code := exitCode(err); code != ExitValidation {
		t.Fatalf("-b web: exit %d: %v", code, err)
	}
	if strings.Contains(err.Error(), "context7") {
		t.Errorf("-b web error mentions Context7: %v", err)
	}
	if reqs := context7Requests(ft); len(reqs) != 0 {
		t.Errorf("-b web asked Context7: %v", reqs)
	}
}

// Context7 with no key: --backend context7 and an auto --library given a
// Context7 ID both answer from the recorded keyless response, with no
// Authorization header and no other host asked.
func TestDocsContext7KeylessRecorded(t *testing.T) {
	for _, backend := range []string{"context7", "auto"} {
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

// A library web cannot identify falls through to Context7, after web has
// had its turn: every Context7 request comes after the last web one.
func TestDocsAutoFallsThroughAfterWeb(t *testing.T) {
	ft := widgetFixtures(t)
	ft.Handle(http.MethodGet, "https://context7.com/api/v1/search?query=zzlib+hooks", testutil.Fixture{
		Body: `{"results":[{"id":"/zz/zzlib","title":"zzlib"}]}`,
	})
	ft.Handle(http.MethodGet, "https://context7.com/api/v2/context?libraryId=/zz/zzlib&query=zzlib+hooks&type=json&tokens=4000", testutil.Fixture{
		Body: `{"codeSnippets":[],"infoSnippets":[{"pageId":"https://zzlib.example/hooks","breadcrumb":"Hooks","content":"zzlib hooks"}]}`,
	})
	docsEnv(t, fixtureFallback{ft})

	var out string
	_, err := captureStderr(t, func() error {
		var runErr error
		out, runErr = docsCLI(t, "-b", "auto", "zzlib hooks")
		return runErr
	})
	if err != nil {
		t.Fatalf("exit %d: %v", exitCode(err), err)
	}
	if !strings.Contains(out, "backend: context7\n") || !strings.Contains(out, "[Hooks]") {
		t.Errorf("output:\n%s", out)
	}
	calls := ft.Calls()
	first := -1
	for i, c := range calls {
		isC7 := strings.Contains(c.URL, "://context7.com/")
		if isC7 && first < 0 {
			first = i
		}
		if !isC7 && first >= 0 {
			t.Errorf("web request %s after Context7 was asked", c.URL)
		}
	}
	if first <= 0 {
		t.Errorf("requests = %v, want web's first, then Context7's", calls)
	}
}
