package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/spf13/pflag"
)

// These tests run `ketch docs` in-process against the web backend's name
// resolution, with every request (package registries, GitHub, docs sites,
// Context7) answered from fixtures under the services' real hostnames, so
// nothing reaches the network. The recorded fixtures are the resolver's
// own (docs/testdata/resolve); testdata/docsresolve adds a synthetic
// "widget" that two unrelated projects publish, npm's and PyPI's.

// resolveFixtureDir is where the resolver keeps its recorded fixtures.
var resolveFixtureDir = filepath.Join("..", "docs", "testdata", "resolve")

// recordedFixtures serves the resolver's recorded registry, GitHub and site
// fixtures.
func recordedFixtures(t *testing.T) *testutil.FixtureTransport {
	t.Helper()
	ft := testutil.NewFixtureTransport()
	for _, dir := range []string{"registry", "github", "sites"} {
		path := filepath.Join(resolveFixtureDir, dir)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("recorded fixtures %s are missing (the resolver's tests record them): %v", path, err)
		}
		if err := ft.LoadDir(path); err != nil {
			t.Fatal(err)
		}
	}
	return ft
}

// widgetFixtures serves the synthetic widget: npm's (widgetjs/widget, 2100
// stars, docs at widgetjs.dev/docs/) and PyPI's (pywidget/widget, 900
// stars, docs on Read the Docs). Neither dominates, so the name is
// ambiguous; crates.io has no widget.
func widgetFixtures(t *testing.T) *testutil.FixtureTransport {
	t.Helper()
	return testutil.LoadFixtures(t, filepath.Join("testdata", "docsresolve"))
}

// fixtureFallback answers what no fixture covers the way the real services
// answer for a name they do not have: a search (GitHub repositories,
// pkg.go.dev) with no hits, anything else 404. The fixture transport still
// records the request as unmatched, and nothing reaches the network.
type fixtureFallback struct{ ft *testutil.FixtureTransport }

func (f fixtureFallback) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := f.ft.RoundTrip(req)
	if err == nil || !strings.HasPrefix(err.Error(), "testutil: no fixture") {
		return resp, err
	}
	switch req.URL.Host + req.URL.Path {
	case "api.github.com/search/repositories":
		return fixtureResponse(req, http.StatusOK, `{"total_count":0,"incomplete_results":false,"items":[]}`), nil
	case "pkg.go.dev/v1/search":
		return fixtureResponse(req, http.StatusOK, `{"items":[]}`), nil
	}
	return fixtureResponse(req, http.StatusNotFound, ""), nil
}

// statusTransport answers every request with one status: a service that is
// down.
type statusTransport int

func (s statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return fixtureResponse(req, int(s), ""), nil
}

func fixtureResponse(req *http.Request, status int, body string) *http.Response {
	h := http.Header{}
	if body != "" {
		h.Set("Content-Type", "application/json")
	}
	return &http.Response{
		Status: http.StatusText(status), StatusCode: status, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: h, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: req,
	}
}

// docsEnv isolates an in-process docs run: a fresh config home and page
// cache, the built-in config instead of the user's, a GitHub token from the
// environment (so `gh auth token` never runs) and rt behind every HTTP
// client. configure adjusts the config.
func docsEnv(t *testing.T, rt http.RoundTripper, configure ...func(*config.Config)) {
	t.Helper()
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_GITHUB_TOKEN", "fixture-token")
	prev := cfg
	cfg = config.Defaults()
	for _, apply := range configure {
		apply(&cfg)
	}
	t.Cleanup(func() { cfg = prev })
	testutil.SwapDefaultTransport(t, rt)
	prevCtx := docsCmd.Context()
	docsCmd.SetContext(context.Background())
	t.Cleanup(func() { docsCmd.SetContext(prevCtx) })
}

// resetDocsFlags puts the docs command's flags, and the global --json, back
// to their defaults, now and when the test ends.
func resetDocsFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		for _, fs := range []*pflag.FlagSet{docsCmd.Flags(), rootCmd.PersistentFlags()} {
			fs.VisitAll(func(f *pflag.Flag) {
				_ = f.Value.Set(f.DefValue)
				f.Changed = false
			})
		}
	}
	reset()
	t.Cleanup(reset)
}

// docsCLI runs `ketch docs args...` in-process, parsing args with the
// command's own flags, and returns its stdout and error.
func docsCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetDocsFlags(t)
	if err := docsCmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %q: %v", args, err)
	}
	var runErr error
	out := captureStdout(t, func() { runErr = runDocs(docsCmd, docsCmd.Flags().Args()) })
	return out, runErr
}

// exitCode is the process exit code main.go gives err.
func exitCode(err error) int {
	var exitErr *ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr):
		return exitErr.Code
	}
	return 1
}

// resolveJSON runs --json --resolve and decodes the matches twice: typed,
// and as raw objects for their keys.
func resolveJSON(t *testing.T, args ...string) ([]docs.LibraryMatch, []map[string]json.RawMessage) {
	t.Helper()
	out, err := docsCLI(t, append([]string{"--json", "--resolve"}, args...)...)
	if err != nil {
		t.Fatalf("docs --json --resolve %q: exit %d: %v", args, exitCode(err), err)
	}
	var matches []docs.LibraryMatch
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &matches); err != nil {
		t.Fatalf("resolve --json stdout is not a match array: %v\n%s", err, out)
	}
	_ = json.Unmarshal([]byte(out), &raw)
	return matches, raw
}

func matchIDs(matches []docs.LibraryMatch) []string {
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.ID
	}
	return ids
}

func hostOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		t.Fatalf("match ID %q is not an absolute URL: %v", rawURL, err)
	}
	return u.Host
}

// context7Keys are the keys of a Context7 match as --json has always
// printed them; the web backend's version, evidence and packages are
// omitted when empty, so they never appear.
var context7Keys = []string{"description", "id", "title", "totalSnippets", "trustScore", "versions"}

// Context7's resolve output is unchanged by the web backend's fields, in
// text and in JSON.
func TestDocsResolveContext7OutputUnchanged(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://context7.com/api/v1/search?query=react", testutil.Fixture{Body: `{"results":[` +
		`{"id":"/facebook/react","title":"React","description":"The library for web and native user interfaces.","totalSnippets":3215,"trustScore":9,"versions":["v18.3.1","v19.1.0"]},` +
		`{"id":"/reactjs/react.dev","title":"React docs","description":"React documentation website.","totalSnippets":2477,"trustScore":10}]}`})
	docsEnv(t, ft, func(c *config.Config) { c.SetProvider("context7_api_key", "test-key") })

	out, err := docsCLI(t, "-b", "context7", "--resolve", "react")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "/facebook/react  React  (snippets: 3215, trust: 9.0)\n/reactjs/react.dev  React docs  (snippets: 2477, trust: 10.0)\n"
	if out != want {
		t.Errorf("Context7 resolve output changed:\n got %q\nwant %q", out, want)
	}

	_, raw := resolveJSON(t, "-b", "context7", "react")
	if len(raw) != 2 {
		t.Fatalf("resolve --json returned %d matches, want 2", len(raw))
	}
	for i, m := range raw {
		if keys := slices.Sorted(maps.Keys(m)); !slices.Equal(keys, context7Keys) {
			t.Errorf("match %d keys = %v, want %v", i, keys, context7Keys)
		}
	}
	if u := ft.Unmatched(); len(u) > 0 {
		t.Errorf("requests with no fixture: %v", u)
	}
}

// --resolve on a library name prints the one library it is, with the
// evidence for it, and --json carries the same match with its version,
// evidence and package specs. click is the hard case: npm's click names a
// deleted repository and crates.io's is another project whose homepage is
// a GitHub wiki, so only PyPI's survives.
func TestDocsWebResolveFormatting(t *testing.T) {
	ft := recordedFixtures(t)
	docsEnv(t, fixtureFallback{ft})

	matches, raw := resolveJSON(t, "-b", "web", "click")
	if len(matches) != 1 {
		t.Fatalf("resolve click = %v, want exactly one match (PyPI's click)", matchIDs(matches))
	}
	m := matches[0]
	if host := hostOf(t, m.ID); host != "click.palletsprojects.com" {
		t.Errorf("click resolved to %s, want click.palletsprojects.com", m.ID)
	}
	if !slices.Contains(m.Packages, "pypi:click") {
		t.Errorf("packages = %v, want pypi:click among them", m.Packages)
	}
	if len(m.Evidence) == 0 || m.TrustScore < 3 || m.Version == "" {
		t.Errorf("match = %+v, want evidence, trust >= 3 and a version", m)
	}
	for _, key := range []string{"id", "title", "trustScore", "version", "evidence", "packages"} {
		if _, ok := raw[0][key]; !ok {
			t.Errorf("JSON match has no %q key: %v", key, slices.Sorted(maps.Keys(raw[0])))
		}
	}

	text, err := docsCLI(t, "-b", "web", "--resolve", "click")
	if err != nil {
		t.Fatalf("resolve click: %v", err)
	}
	t.Logf("resolve click:\n%s", text)
	if want := formatLibraryMatches(matches); text != want {
		t.Errorf("text output does not match the JSON match:\n got %q\nwant %q", text, want)
	}
	if !strings.HasPrefix(text, m.ID+"  "+m.Title+"  (trust: ") || !strings.Contains(text, "\n  packages: ") || !strings.Contains(text, "\n  evidence: ") {
		t.Errorf("text output is not in the evidence form:\n%s", text)
	}
}

// A name two unrelated projects publish is listed by --resolve, refused
// where one library is needed (--library, or a bare query naming it) with
// both candidates named, and settled by an ecosystem prefix.
func TestDocsWebResolveAmbiguousName(t *testing.T) {
	ft := widgetFixtures(t)
	docsEnv(t, fixtureFallback{ft})

	matches, _ := resolveJSON(t, "-b", "web", "widget")
	checkWidgetMatches(t, matches)
	text, err := docsCLI(t, "-b", "web", "--resolve", "widget")
	if err != nil {
		t.Fatalf("resolve widget: %v", err)
	}
	t.Logf("resolve widget:\n%s", text)
	if n := strings.Count(text, "\n  evidence: "); n != 2 {
		t.Errorf("text output has %d evidence lines, want 2:\n%s", n, text)
	}

	for _, args := range [][]string{
		{"-b", "web", "--library", "widget", "configure themes"},
		{"-b", "web", "widget configure themes"},
	} {
		_, err := docsCLI(t, args...)
		checkAmbiguous(t, args, err, matches)
	}

	one, _ := resolveJSON(t, "-b", "web", "pypi:widget")
	if len(one) != 1 || hostOf(t, one[0].ID) != "widget.readthedocs.io" {
		t.Errorf("resolve pypi:widget = %v, want PyPI's widget alone", matchIDs(one))
	}
}

// checkWidgetMatches asserts matches are the two widgets: npm's on
// widgetjs.dev and PyPI's on Read the Docs, each with its package spec and
// some evidence.
func checkWidgetMatches(t *testing.T, matches []docs.LibraryMatch) {
	t.Helper()
	if len(matches) != 2 {
		t.Fatalf("resolve widget = %v, want npm's and PyPI's", matchIDs(matches))
	}
	byHost := map[string]docs.LibraryMatch{}
	for _, m := range matches {
		byHost[hostOf(t, m.ID)] = m
	}
	for host, spec := range map[string]string{"widgetjs.dev": "npm:widget", "widget.readthedocs.io": "pypi:widget"} {
		m, ok := byHost[host]
		switch {
		case !ok:
			t.Errorf("no match on %s among %v", host, matchIDs(matches))
		case !slices.Contains(m.Packages, spec) || len(m.Evidence) == 0:
			t.Errorf("%s match = %+v, want %s among its packages and some evidence", host, m, spec)
		}
	}
}

// checkAmbiguous asserts err is the ambiguity error, exit 2, naming every
// candidate's ID and package spec and the flag to pass one with.
func checkAmbiguous(t *testing.T, args []string, err error, matches []docs.LibraryMatch) {
	t.Helper()
	if code := exitCode(err); code != ExitValidation {
		t.Errorf("docs %q: exit %d, want %d (ambiguous): %v", args, code, ExitValidation, err)
		return
	}
	var amb *docs.AmbiguousLibraryError
	if !errors.As(err, &amb) {
		t.Errorf("docs %q: %v, want an *AmbiguousLibraryError", args, err)
	}
	for _, want := range append(matchIDs(matches), "npm:widget", "pypi:widget", "--library") {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("docs %q: error %q does not name %q", args, err, want)
		}
	}
}

// Every way a docs run can fail maps to its exit code: input to fix (2), a
// library or version nobody offers (3), a source that is down (4).
func TestDocsWebResolveExitCodes(t *testing.T) {
	widget := func(t *testing.T) http.RoundTripper { return fixtureFallback{widgetFixtures(t)} }
	recorded := func(t *testing.T) http.RoundTripper { return fixtureFallback{recordedFixtures(t)} }
	nothing := func(*testing.T) http.RoundTripper { return fixtureFallback{testutil.NewFixtureTransport()} }
	down := func(*testing.T) http.RoundTripper { return statusTransport(http.StatusServiceUnavailable) }

	cases := []struct {
		name  string
		rt    func(*testing.T) http.RoundTripper
		args  []string
		code  int
		names []string // text the error must contain
	}{
		{"empty version", widget, []string{"--library", "widget@", "configure themes"}, ExitValidation, []string{"widget@"}},
		{"unknown ecosystem", widget, []string{"--library", "maven:widget", "configure themes"}, ExitValidation, []string{"maven"}},
		{"docs URL with a version", widget, []string{"--library", "https://widgetjs.dev/docs/@3", "configure themes"}, ExitValidation, []string{"version"}},
		{"bare query naming no library", nothing, []string{"how do i configure the themes"}, ExitValidation, []string{"--library"}},
		{"resolve a name nobody publishes", nothing, []string{"--resolve", "zzqxnosuchlib"}, ExitNotFound, []string{"zzqxnosuchlib"}},
		{"docs for a name nobody publishes", nothing, []string{"--library", "zzqxnosuchlib", "configure themes"}, ExitNotFound, []string{"zzqxnosuchlib"}},
		{"version not offered", recorded, []string{"--library", "django@4.0", "middleware"}, ExitNotFound, []string{"4.0", "5.2"}},
		{"resolve a version not offered", recorded, []string{"--resolve", "django@4.0"}, ExitNotFound, []string{"4.0", "5.2"}},
		{"every source down", down, []string{"--resolve", "widget"}, ExitUpstream, nil},
		{"bare query while every source is down", down, []string{"how do i configure the themes in starlight"}, ExitUpstream, []string{"starlight", "503"}},
		{"bare question naming no library, every source down", down, []string{"how do i configure the themes"}, ExitValidation, []string{"--library"}},
		{"docs while every source is down", down, []string{"--library", "widget", "configure themes"}, ExitUpstream, nil},
		{"github the only source, down", down, []string{"--resolve", "github:widgetjs/widget"}, ExitUpstream, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			docsEnv(t, tc.rt(t))
			out, err := docsCLI(t, append([]string{"-b", "web"}, tc.args...)...)
			if code := exitCode(err); code != tc.code {
				t.Fatalf("docs %q: exit %d, want %d: %v\nstdout: %s", tc.args, code, tc.code, err, out)
			}
			if out != "" {
				t.Errorf("a failed run printed to stdout: %q", out)
			}
			for _, want := range tc.names {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// registryDown answers one registry's host with 503 and everything else
// through rt.
type registryDown struct {
	rt   http.RoundTripper
	host string
}

func (d registryDown) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == d.host {
		return fixtureResponse(req, http.StatusServiceUnavailable, ""), nil
	}
	return d.rt.RoundTrip(req)
}

// With PyPI down, --resolve widget lists npm's widget alone, and says the
// list may be incomplete and why, on the evidence line and in --json.
func TestDocsWebResolveSaysWhenIncomplete(t *testing.T) {
	resetDocsFlags(t)
	docsEnv(t, registryDown{fixtureFallback{widgetFixtures(t)}, "pypi.org"})
	out, err := docsCLI(t, "-b", "web", "--resolve", "widget")
	if err != nil || !strings.Contains(out, "evidence: this list may be incomplete: pypi widget") || strings.Contains(out, "readthedocs") {
		t.Fatalf("--resolve widget with PyPI down: %v\n%s", err, out)
	}
	out, err = docsCLI(t, "--json", "-b", "web", "--resolve", "widget")
	var matches []docs.LibraryMatch
	if err != nil || json.Unmarshal([]byte(out), &matches) != nil || len(matches) != 1 ||
		!strings.HasPrefix(matches[0].Evidence[0], "this list may be incomplete: pypi widget") {
		t.Errorf("--json --resolve widget with PyPI down: %v\n%s", err, out)
	}
}
