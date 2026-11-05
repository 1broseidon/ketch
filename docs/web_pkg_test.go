package docs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/testutil"
)

// Fixture roots: recorded registry and GitHub answers, trimmed, served
// under their real hostnames (testutil.LoadFixtures).
const (
	registryFixtureDir = "testdata/resolve/registry"
	githubFixtureDir   = "testdata/resolve/github"
)

// recordedPkgClient is a pkgClient over the recorded registry fixtures and
// the transport serving them.
func recordedPkgClient(t *testing.T) (*pkgClient, *testutil.FixtureTransport) {
	t.Helper()
	ft := testutil.LoadFixtures(t, registryFixtureDir)
	return newPkgClient(ft.Client()), ft
}

func mustLookupPkg(t *testing.T, c *pkgClient, eco ecosystem, name string) pkgInfo {
	t.Helper()
	info, err := c.lookup(context.Background(), eco, name)
	if err != nil {
		t.Fatalf("lookup(%s, %s): %v", eco, name, err)
	}
	return info
}

func TestPkgClientNPM(t *testing.T) {
	c, ft := recordedPkgClient(t)
	tests := []struct {
		name string
		want pkgInfo
	}{
		{"react", pkgInfo{eco: ecoNPM, name: "react", version: "19.3.1",
			summary:  "React is a JavaScript library for building user interfaces.",
			homepage: "https://react.dev/", repoURL: "git+https://github.com/react/react.git", repo: repoKey{"react", "react"}}},
		{"next", pkgInfo{eco: ecoNPM, name: "next", version: "16.4.3", summary: "The React Framework",
			homepage: "https://nextjs.org", repoURL: "git+https://github.com/vercel/next.js.git", repo: repoKey{"vercel", "next.js"}}},
		// No homepage; a git:// repository whose repo is gone (GitHub 404s).
		{"click", pkgInfo{eco: ecoNPM, name: "click", version: "0.1.0", summary: "Simulate mouse events",
			repoURL: "git://github.com/uggedal/click.git", repo: repoKey{"uggedal", "click"}}},
		// Deprecated packages are returned, flagged.
		{"request", pkgInfo{eco: ecoNPM, name: "request", version: "2.88.2", summary: "Simplified HTTP request client.",
			homepage: "https://github.com/request/request#readme", repoURL: "git+https://github.com/request/request.git",
			repo: repoKey{"request", "request"}, deprecated: true}},
		// No repository: a homepage that is not a GitHub repository stays a homepage.
		{"cobra", pkgInfo{eco: ecoNPM, name: "cobra", version: "1.0.1", summary: "A little JavaScript class library",
			homepage: "http://justintulloss.github.com/cobra"}},
	}
	for _, tt := range tests {
		if got := mustLookupPkg(t, c, ecoNPM, tt.name); got != tt.want {
			t.Errorf("npm %s\n got %+v\nwant %+v", tt.name, got, tt.want)
		}
	}
	for _, name := range []string{"solid-js", "solid", "astro"} {
		if got := mustLookupPkg(t, c, ecoNPM, name); got.repo.isZero() || got.version == "" {
			t.Errorf("npm %s = %+v, want a version and a repo", name, got)
		}
	}
	// npm is case-sensitive: a name is asked for as given, then lower-cased
	// when the registry has no such package (as for React, live).
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/React/latest", testutil.Fixture{Status: http.StatusNotFound, Body: `{"error":"Not found"}`})
	if got := mustLookupPkg(t, c, ecoNPM, "React"); got.name != "react" {
		t.Errorf("npm React (404) is then looked up lower-cased: got %+v", got)
	}
	// A legacy mixed-case package is found as given, not as the lower-case
	// placeholder beside it (recorded: jsonstream is deprecated, "use
	// JSONStream instead").
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/JSONStream/latest", testutil.Fixture{Body: `{"name":"JSONStream","version":"1.3.5",
		"description":"rawStream.pipe(JSONStream.parse()).pipe(streamOfObjects)","homepage":"http://github.com/dominictarr/JSONStream",
		"repository":{"type":"git","url":"git://github.com/dominictarr/JSONStream.git"}}`})
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/jsonstream/latest", testutil.Fixture{Body: `{"name":"jsonstream","version":"1.0.3",
		"deprecated":"use JSONStream instead","repository":{"url":"git://github.com/dominictarr/JSONStream.git","type":"git"}}`})
	if got := mustLookupPkg(t, c, ecoNPM, "JSONStream"); got.name != "JSONStream" || got.deprecated {
		t.Errorf("npm JSONStream = %+v, want the mixed-case package itself", got)
	}
	_, err := c.lookup(context.Background(), ecoNPM, "serde")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("npm serde (404) = %v, want ErrNotFound", err)
	}
	if err == nil || !strings.Contains(err.Error(), "npm serde") {
		t.Errorf("error %v does not name the lookup", err)
	}
	if u := ft.Unmatched(); len(u) != 0 {
		t.Errorf("unmatched requests: %v", u)
	}
}

// package.json's repository comes as an object, a string, the github:
// shorthand, the bare owner/repo shorthand, or in old packages a list.
func TestPkgClientNPMRepositoryShapes(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	c := newPkgClient(ft.Client())
	tests := []struct {
		repository string
		want       repoKey
	}{
		{`{"type":"git","url":"git+https://github.com/withastro/astro.git","directory":"packages/astro"}`, repoKey{"withastro", "astro"}},
		{`"github:solidjs/solid"`, repoKey{"solidjs", "solid"}},
		{`"solidjs/solid"`, repoKey{"solidjs", "solid"}},
		{`"https://github.com/solidjs/solid"`, repoKey{"solidjs", "solid"}},
		{`[{"type":"git","url":"git://github.com/old/pkg.git"}]`, repoKey{"old", "pkg"}},
		{`"gitlab:group/project"`, repoKey{}},
		{`{"type":"git","url":"https://gitlab.com/group/project.git"}`, repoKey{}},
		{`42`, repoKey{}},
	}
	for _, tt := range tests {
		ft.Handle(http.MethodGet, "https://registry.npmjs.org/pkg/latest", testutil.Fixture{
			Body: `{"name":"pkg","version":"1.0.0","homepage":["https://pkg.example.com"],"repository":` + tt.repository + `}`,
		})
		got := mustLookupPkg(t, c, ecoNPM, "pkg")
		if got.repo != tt.want {
			t.Errorf("repository %s: repo = %v, want %v", tt.repository, got.repo, tt.want)
		}
		if got.homepage != "https://pkg.example.com" {
			t.Errorf("homepage list: got %q", got.homepage)
		}
	}
	// A scoped name is requested with its @ and / unescaped.
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/@astrojs/starlight/latest", testutil.Fixture{
		Body: `{"name":"@astrojs/starlight","version":"0.42.7","homepage":"https://starlight.astro.build","deprecated":false,
			"repository":{"type":"git","url":"https://github.com/withastro/starlight","directory":"packages/starlight"}}`,
	})
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/@AstroJS/Starlight/latest", testutil.Fixture{Status: http.StatusNotFound})
	got := mustLookupPkg(t, c, ecoNPM, "@AstroJS/Starlight")
	if got.name != "@astrojs/starlight" || got.repo != (repoKey{"withastro", "starlight"}) || got.deprecated {
		t.Errorf("scoped package = %+v", got)
	}
	calls := ft.Calls()
	if last := calls[len(calls)-1].URL; last != "https://registry.npmjs.org/@astrojs/starlight/latest" {
		t.Errorf("scoped request URL = %s", last)
	}
}

func TestPkgClientPyPI(t *testing.T) {
	c, ft := recordedPkgClient(t)
	tests := []struct {
		name string
		want pkgInfo
	}{
		{"requests", pkgInfo{eco: ecoPyPI, name: "requests", version: "2.34.2", summary: "Python HTTP for Humans.",
			docsURL: "https://requests.readthedocs.io", repoURL: "https://github.com/psf/requests", repo: repoKey{"psf", "requests"}}},
		{"click", pkgInfo{eco: ecoPyPI, name: "click", version: "8.5.0", summary: "Composable command line interface toolkit",
			docsURL: "https://click.palletsprojects.com/", repoURL: "https://github.com/pallets/click/", repo: repoKey{"pallets", "click"},
			pageURL: "https://click.palletsprojects.com/page/changes/"}},
		{"fastapi", pkgInfo{eco: ecoPyPI, name: "fastapi", version: "0.162.0",
			summary:  "FastAPI framework, high performance, easy to learn, fast to code, ready for production",
			homepage: "https://github.com/fastapi/fastapi", docsURL: "https://fastapi.tiangolo.com/",
			repoURL: "https://github.com/fastapi/fastapi", repo: repoKey{"fastapi", "fastapi"}, pageURL: "https://fastapi.tiangolo.com/release-notes/"}},
		// The registry spells it Django; the request uses the normalized name.
		{"Django", pkgInfo{eco: ecoPyPI, name: "Django", version: "6.1.2",
			summary:  "A high-level Python web framework that encourages rapid development and clean, pragmatic design.",
			homepage: "https://www.djangoproject.com/", docsURL: "https://docs.djangoproject.com/",
			repoURL: "https://github.com/django/django", repo: repoKey{"django", "django"}, pageURL: "https://docs.djangoproject.com/en/stable/releases/"}},
		// No Source label: a GitHub homepage is the repository.
		{"next", pkgInfo{eco: ecoPyPI, name: "next", version: "1.0.1", summary: "required code for sae.news",
			homepage: "https://github.com/dheerajmpai/saenews/", repoURL: "https://github.com/dheerajmpai/saenews/",
			repo: repoKey{"dheerajmpai", "saenews"}}},
		// UNKNOWN placeholders are no URLs; the legacy docs_url still counts.
		{"clap", pkgInfo{eco: ecoPyPI, name: "Clap", version: "0.7", summary: "Command Line Applications for Python",
			docsURL: "https://pythonhosted.org/Clap/"}},
	}
	for _, tt := range tests {
		got := mustLookupPkg(t, c, ecoPyPI, tt.name)
		if tt.name == "Django" && got.moreURLs != "https://code.djangoproject.com/\nhttps://www.djangoproject.com/fundraising/" {
			t.Errorf("pypi Django: other project URLs %q", got.moreURLs)
		}
		got.moreURLs = "" // the project_urls no role takes (checked above)
		if got != tt.want {
			t.Errorf("pypi %s\n got %+v\nwant %+v", tt.name, got, tt.want)
		}
	}
	if _, err := c.lookup(context.Background(), ecoPyPI, "solid-js"); !errors.Is(err, ErrNotFound) {
		t.Errorf("pypi solid-js (404) = %v, want ErrNotFound", err)
	}
	if u := ft.Unmatched(); len(u) != 0 {
		t.Errorf("unmatched requests: %v", u)
	}
	for _, call := range ft.Calls() {
		if strings.Contains(call.URL, "Django") {
			t.Errorf("request %s did not normalize the name", call.URL)
		}
	}
}

// project_urls labels are free-form; PEP 753 normalizes them, and its
// aliases pick the documentation, homepage and source URLs.
func TestPyPIProjectURLs(t *testing.T) {
	got := pypiProjectURLs(map[string]string{
		"Docs":          "https://docs.example.com",
		"DOCUMENTATION": "https://example.com/docs",
		"Home-Page":     "https://example.com",
		"Repository":    "",
		"Source Code":   "https://github.com/o/src",
		"GitHub":        "https://github.com/o/gh",
		"Bug Tracker":   "https://github.com/o/src/issues",
		"Funding":       "https://github.com/sponsors/o",
		"Download":      "UNKNOWN",
	})
	want := map[string]string{
		"documentation": "https://example.com/docs", // the canonical label beats its alias
		"homepage":      "https://example.com",
		"source":        "https://github.com/o/src", // an empty Repository is skipped; sourcecode comes before github
	}
	if len(got) != len(want) {
		t.Errorf("roles = %v, want %v", got, want)
	}
	for role, w := range want {
		if got[role] != w {
			t.Errorf("%s = %q, want %q (all: %v)", role, got[role], w, got)
		}
	}
	for label, want := range map[string]string{
		"Source Code": "sourcecode", "Home-Page": "homepage", "Issue_Tracker": "issuetracker",
		"What's New?": "whatsnew", " Docs ": "docs", "Dokumentation": "dokumentation",
	} {
		if got := pep753Label(label); got != want {
			t.Errorf("pep753Label(%q) = %q, want %q", label, got, want)
		}
	}
}

// PyPI sends "info" first; the client reads it and stops, however much
// follows (numpy's response is 3.7 MB).
func TestPkgClientPyPIStopsAfterInfo(t *testing.T) {
	var read atomic.Int64
	var closed atomic.Bool
	rt := pkgScriptedTransport(func(req *http.Request) (*http.Response, error) {
		info := `{"info":{"name":"numpy","version":"2.4.0","summary":"Fundamental package","home_page":null,` +
			`"project_urls":{"documentation":"https://numpy.org/doc","source":"https://github.com/numpy/numpy"}},"releases":{`
		body := &endlessBody{prefix: info, filler: `"1.0":[{"filename":"numpy-1.0.tar.gz","size":1}],`, read: &read, closed: &closed}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body, Request: req}, nil
	})
	c := newPkgClient(&http.Client{Transport: rt})
	done := make(chan struct{})
	var info pkgInfo
	var err error
	go func() {
		defer close(done)
		info, err = c.lookup(context.Background(), ecoPyPI, "numpy")
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("lookup read the endless releases instead of stopping after info")
	}
	if err != nil {
		t.Fatal(err)
	}
	if info.docsURL != "https://numpy.org/doc" || info.repo != (repoKey{"numpy", "numpy"}) {
		t.Errorf("info = %+v", info)
	}
	if n := read.Load(); n > 1<<20 {
		t.Errorf("read %d bytes for a few hundred bytes of info", n)
	}
	if !closed.Load() {
		t.Error("the response body was not closed")
	}

	// info need not be first.
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://pypi.org/pypi/late/json", testutil.Fixture{
		Body: `{"last_serial":1,"releases":{"0.1":[]},"info":{"name":"late","version":"0.1","project_urls":null}}`,
	})
	ft.Handle(http.MethodGet, "https://pypi.org/pypi/noinfo/json", testutil.Fixture{Body: `{"releases":{}}`})
	ft.Handle(http.MethodGet, "https://pypi.org/pypi/array/json", testutil.Fixture{Body: `[1,2]`})
	c = newPkgClient(ft.Client())
	if got := mustLookupPkg(t, c, ecoPyPI, "late"); got.name != "late" || got.version != "0.1" {
		t.Errorf("late info = %+v", got)
	}
	for _, name := range []string{"noinfo", "array"} {
		_, err := c.lookup(context.Background(), ecoPyPI, name)
		if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "unreadable") {
			t.Errorf("pypi %s = %v, want an unreadable-response error", name, err)
		}
	}
}

type pkgScriptedTransport func(*http.Request) (*http.Response, error)

func (f pkgScriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// endlessBody is prefix followed by filler forever, counting what is read.
type endlessBody struct {
	prefix, filler string
	off            int
	read           *atomic.Int64
	closed         *atomic.Bool
}

func (b *endlessBody) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		src := b.filler
		if b.off < len(b.prefix) {
			src = b.prefix[b.off:]
		} else {
			src = src[(b.off-len(b.prefix))%len(b.filler):]
		}
		c := copy(p[n:], src)
		n += c
		b.off += c
	}
	b.read.Add(int64(n))
	return n, nil
}

func (b *endlessBody) Close() error { b.closed.Store(true); return nil }

func TestPkgClientCrates(t *testing.T) {
	c, ft := recordedPkgClient(t)
	tests := []struct {
		name string
		want pkgInfo
	}{
		{"serde", pkgInfo{eco: ecoCrates, name: "serde", version: "1.0.229", summary: "A generic serialization/deserialization framework",
			homepage: "https://serde.rs", docsURL: "https://docs.rs/serde", repoURL: "https://github.com/serde-rs/serde",
			repo: repoKey{"serde-rs", "serde"}, registryDocs: "https://docs.rs/serde/latest/serde/"}},
		// No documentation or homepage: docs.rs is implied.
		{"clap", pkgInfo{eco: ecoCrates, name: "clap", version: "4.6.7",
			summary: "A simple to use, efficient, and full-featured Command Line Argument Parser",
			repoURL: "https://github.com/clap-rs/clap", repo: repoKey{"clap-rs", "clap"}, registryDocs: "https://docs.rs/clap/latest/clap/"}},
		// A homepage on GitHub's wiki.
		{"click", pkgInfo{eco: ecoCrates, name: "click", version: "0.6.3",
			summary:  "A command-line REPL for Kubernetes that integrates into existing cli workflows",
			homepage: "https://github.com/databricks/click/wiki", repoURL: "https://github.com/databricks/click",
			repo: repoKey{"databricks", "click"}, registryDocs: "https://docs.rs/click/latest/click/"}},
		// A repository off GitHub.
		{"requests", pkgInfo{eco: ecoCrates, name: "requests", version: "0.0.30",
			summary:  "Rust HTTP client API styled after awesome Python requests - [WORK IN PROGRESS]",
			homepage: "https://gitlab.com/imp/requests-rs", docsURL: "https://docs.rs/requests",
			repoURL: "https://gitlab.com/imp/requests-rs.git", registryDocs: "https://docs.rs/requests/latest/requests/"}},
	}
	for _, tt := range tests {
		got := mustLookupPkg(t, c, ecoCrates, tt.name)
		if got.downloads <= 0 {
			t.Errorf("crates %s: recent downloads = %d", tt.name, got.downloads)
		}
		tt.want.downloads = got.downloads
		if got != tt.want {
			t.Errorf("crates %s\n got %+v\nwant %+v", tt.name, got, tt.want)
		}
	}
	if got := mustLookupPkg(t, c, ecoCrates, "tokio"); got.version != "1.54.1" || got.downloads != 274400094 || got.homepage != "https://tokio.rs" {
		t.Errorf("crates tokio = %+v", got)
	}
	if _, err := c.lookup(context.Background(), ecoCrates, "django"); !errors.Is(err, ErrNotFound) {
		t.Errorf("crates django (404) = %v, want ErrNotFound", err)
	}
	for _, call := range ft.Calls() {
		if ua := call.Header.Get("User-Agent"); ua != webUserAgent {
			t.Errorf("%s sent User-Agent %q, want %q (crates.io answers 403 without one)", call, ua, webUserAgent)
		}
	}
	if u := ft.Unmatched(); len(u) != 0 {
		t.Errorf("unmatched requests: %v", u)
	}
}

// crates.io treats - and _ alike; the request uses -, the answer the
// crate's own spelling, which docs.rs's URL keeps.
func TestPkgClientCrateNames(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://crates.io/api/v1/crates/serde-json?include=downloads", testutil.Fixture{
		Body: `{"crate":{"name":"serde_json","max_stable_version":"1.0.140","default_version":"1.0.140","max_version":"0.0.0",
			"documentation":"https://docs.rs/serde_json","repository":"https://github.com/serde-rs/json","recent_downloads":5}}`,
	})
	ft.Handle(http.MethodGet, "https://crates.io/api/v1/crates/pre-only?include=downloads", testutil.Fixture{
		Body: `{"crate":{"name":"pre-only","max_stable_version":null,"default_version":null,"max_version":"0.1.0-alpha.1"}}`,
	})
	ft.Handle(http.MethodGet, "https://crates.io/api/v1/crates/empty?include=downloads", testutil.Fixture{Body: `{"errors":[]}`})
	c := newPkgClient(ft.Client())
	got := mustLookupPkg(t, c, ecoCrates, "Serde_JSON")
	if got.name != "serde_json" || got.version != "1.0.140" || got.registryDocs != "https://docs.rs/serde_json/latest/serde_json/" {
		t.Errorf("serde_json = %+v", got)
	}
	if got := mustLookupPkg(t, c, ecoCrates, "pre-only"); got.version != "0.1.0-alpha.1" || got.registryDocs != "https://docs.rs/pre-only/latest/pre_only/" {
		t.Errorf("pre-only = %+v", got)
	}
	if _, err := c.lookup(context.Background(), ecoCrates, "empty"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("a crate-less answer = %v, want an unreadable-response error", err)
	}
}

func TestPkgClientGo(t *testing.T) {
	c, ft := recordedPkgClient(t)
	got := mustLookupPkg(t, c, ecoGo, "github.com/spf13/cobra")
	want := pkgInfo{eco: ecoGo, name: "github.com/spf13/cobra", version: "v1.10.2", repoURL: "https://github.com/spf13/cobra",
		repo: repoKey{"spf13", "cobra"}, registryDocs: "https://pkg.go.dev/github.com/spf13/cobra"}
	if got != want {
		t.Errorf("go cobra\n got %+v\nwant %+v", got, want)
	}
	for _, call := range ft.Calls() {
		if strings.Contains(call.URL, "pkg.go.dev") {
			t.Errorf("asked pkg.go.dev (%s) though the proxy recorded the origin", call)
		}
	}
	if _, err := c.lookup(context.Background(), ecoGo, "cobra"); !errors.Is(err, ErrNotFound) {
		t.Errorf("go cobra (no module path) = %v, want ErrNotFound", err)
	}
}

// The proxy needs upper case escaped; a module with no recorded origin
// gets its repository from pkg.go.dev, best effort.
func TestPkgClientGoEscapingAndPkgsite(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://proxy.golang.org/github.com/!burnt!sushi/toml/@latest", testutil.Fixture{
		Body: `{"Version":"v1.5.0","Time":"2025-03-01T00:00:00Z"}`,
	})
	ft.Handle(http.MethodGet, "https://pkg.go.dev/v1/module/github.com/BurntSushi/toml", testutil.Fixture{
		Body: `{"path":"github.com/BurntSushi/toml","version":"v1.5.0","repoUrl":"https://github.com/BurntSushi/toml"}`,
	})
	ft.Handle(http.MethodGet, "https://proxy.golang.org/example.com/old/@latest", testutil.Fixture{Body: `{"Version":"v0.1.0"}`})
	ft.Handle(http.MethodGet, "https://pkg.go.dev/v1/module/example.com/old", testutil.Fixture{Status: http.StatusInternalServerError})
	ft.Handle(http.MethodGet, "https://proxy.golang.org/example.com/gone/@latest", testutil.Fixture{Status: http.StatusGone})
	c := newPkgClient(ft.Client())

	got := mustLookupPkg(t, c, ecoGo, "github.com/BurntSushi/toml")
	if got.version != "v1.5.0" || got.repo != (repoKey{"burntsushi", "toml"}) || got.registryDocs != "https://pkg.go.dev/github.com/BurntSushi/toml" {
		t.Errorf("toml = %+v", got)
	}
	if got := mustLookupPkg(t, c, ecoGo, "example.com/old"); got.version != "v0.1.0" || got.repoURL != "" {
		t.Errorf("a failing pkg.go.dev must not fail the lookup: %+v", got)
	}
	if _, err := c.lookup(context.Background(), ecoGo, "example.com/gone"); !errors.Is(err, ErrNotFound) {
		t.Errorf("410 = %v, want ErrNotFound", err)
	}
	if got := goEscapePath("github.com/Azure/azure-SDK"); got != "github.com/!azure/azure-!s!d!k" {
		t.Errorf("goEscapePath = %q", got)
	}
}

func TestPkgClientGoVersions(t *testing.T) {
	c, _ := recordedPkgClient(t)
	got, err := c.goVersions(context.Background(), "github.com/spf13/cobra")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 27 || !slices.Equal(got[:5], []string{"v1.10.2", "v1.10.1", "v1.10.0", "v1.9.1", "v1.9.0"}) || got[len(got)-1] != "v0.0.1" {
		t.Errorf("goVersions = %v, want 27 newest first", got)
	}

	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://proxy.golang.org/example.com/m/@v/list", testutil.Fixture{
		Body: "v1.0.0-rc.1\nv1.0.0\nnot-a-version\nv0.9.0\n\nv2.0.0+incompatible\nv1.0.0-beta.11\nv1.0.0-beta.2\n",
	})
	ft.Handle(http.MethodGet, "https://proxy.golang.org/example.com/none/@v/list", testutil.Fixture{Status: http.StatusNotFound})
	c = newPkgClient(ft.Client())
	got, err = c.goVersions(context.Background(), "example.com/m")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v2.0.0+incompatible", "v1.0.0", "v1.0.0-rc.1", "v1.0.0-beta.11", "v1.0.0-beta.2", "v0.9.0"}
	if !slices.Equal(got, want) {
		t.Errorf("goVersions = %v, want %v", got, want)
	}
	if _, err := c.goVersions(context.Background(), "example.com/none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("goVersions of a missing module = %v, want ErrNotFound", err)
	}
}

func TestSemverLess(t *testing.T) {
	ordered := []string{"v0.0.1", "v0.9.0", "v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta",
		"v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0", "v1.2.0", "v1.10.0", "v10.0.0", "v100000000000000000000.0.0"}
	for i := range ordered {
		for j := range ordered {
			if got := semverLess(ordered[i], ordered[j]); got != (i < j) {
				t.Errorf("semverLess(%s, %s) = %v", ordered[i], ordered[j], got)
			}
		}
	}
	if !semverLess("garbage", "v0.0.1") || semverLess("v0.0.1", "garbage") || semverLess("garbage", "junk") {
		t.Error("versions that do not parse sort below those that do")
	}
	for _, bad := range []string{"1.0.0", "v1.0", "v1.0.0-", "v1.a.0", "v"} {
		if parseSemver(bad) != nil {
			t.Errorf("parseSemver(%q) parsed", bad)
		}
	}
}

func TestPkgClientSearchGo(t *testing.T) {
	c, _ := recordedPkgClient(t)
	got, err := c.searchGo(context.Background(), "cobra", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "github.com/spf13/cobra" || slices.Index(got[1:], "github.com/spf13/cobra") >= 0 {
		t.Errorf("searchGo(cobra, 3) = %v, want cobra first, once", got)
	}
	all, err := c.searchGo(context.Background(), "cobra", 0)
	if err != nil || len(all) < 5 {
		t.Errorf("searchGo(cobra, 0) = %v, %v", all, err)
	}

	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://pkg.go.dev/v1/search?q=gone", testutil.Fixture{Status: http.StatusNotFound})
	ft.Handle(http.MethodGet, "https://pkg.go.dev/v1/search?q=down", testutil.Fixture{Status: http.StatusBadGateway})
	c = newPkgClient(ft.Client())
	c.retryWait = time.Millisecond
	if got, err := c.searchGo(context.Background(), "gone", 5); got != nil || err != nil {
		t.Errorf("a 404 search = %v, %v; want nil, nil", got, err)
	}
	if _, err := c.searchGo(context.Background(), "down", 5); err == nil {
		t.Error("a 502 search succeeded")
	}
}

// A 404 is not found; anything else that goes wrong is a failed source,
// kept apart so the final error can say "upstream".
func TestPkgClientErrors(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/down/latest", testutil.Fixture{Status: http.StatusServiceUnavailable, Body: "busy"})
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/limited/latest", testutil.Fixture{Status: http.StatusTooManyRequests})
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/reset/latest", testutil.Fixture{Err: errors.New("connection reset by peer")})
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/garbled/latest", testutil.Fixture{Body: `{"name":`})
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/slow/latest", testutil.Fixture{Delay: 5 * time.Second, Body: `{}`})
	c := newPkgClient(ft.Client())
	c.timeout = 50 * time.Millisecond

	for name, want := range map[string]string{
		"down": "status 503", "limited": "status 429", "reset": "connection reset", "garbled": "unreadable", "missing": "no fixture",
	} {
		_, err := c.lookup(context.Background(), ecoNPM, name)
		if err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("npm %s = %v, want a failed-source error", name, err)
			continue
		}
		if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "npm "+name) {
			t.Errorf("npm %s = %q, want it to name the package and say %q", name, err, want)
		}
	}
	start := time.Now()
	_, err := c.lookup(context.Background(), ecoNPM, "slow")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Errorf("slow registry = %v after %s, want the per-call deadline", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.lookup(ctx, ecoNPM, "slow"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled lookup = %v, want context.Canceled", err)
	}
	for _, eco := range []ecosystem{ecoAny, ecoGitHub} {
		if _, err := c.lookup(context.Background(), eco, "x"); err == nil {
			t.Errorf("lookup(%q) succeeded", eco)
		}
	}
	if c := newPkgClient(nil); c.timeout != apiCallTimeout || c.ua != webUserAgent || c.crates != cratesBase {
		t.Errorf("newPkgClient = %+v", c)
	}
}

func TestPkgClientLookupAll(t *testing.T) {
	c, ft := recordedPkgClient(t)
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/Django/latest", testutil.Fixture{Status: http.StatusNotFound}) // npm asks as given first
	ctx := context.Background()
	spec := func(s string) libSpec {
		t.Helper()
		sp, err := parseLibSpec(s)
		if err != nil {
			t.Fatal(err)
		}
		return sp
	}
	type answer struct {
		eco      ecosystem
		name     string
		found    bool
		notFound bool
	}
	summarize := func(ls []pkgLookup) []answer {
		var out []answer
		for _, l := range ls {
			if l.err == nil && l.info.eco != l.eco {
				t.Errorf("%s answer has eco %q", l.eco, l.info.eco)
			}
			out = append(out, answer{l.eco, l.name, l.err == nil, errors.Is(l.err, ErrNotFound)})
		}
		return out
	}
	tests := []struct {
		spec string
		want []answer
	}{
		{"click", []answer{{ecoNPM, "click", true, false}, {ecoPyPI, "click", true, false}, {ecoCrates, "click", true, false}}},
		{"solid-js", []answer{{ecoNPM, "solid-js", true, false}, {ecoPyPI, "solid-js", false, true}, {ecoCrates, "solid-js", false, true}}},
		{"Django@5.2", []answer{{ecoNPM, "Django", true, false}, {ecoPyPI, "django", true, false}, {ecoCrates, "django", false, true}}},
		{"pypi:Django", []answer{{ecoPyPI, "django", true, false}}},
		{"cargo:tokio@1.38", []answer{{ecoCrates, "tokio", true, false}}},
		{"github.com/spf13/cobra", []answer{{ecoGo, "github.com/spf13/cobra", true, false}}},
		{"pallets/click", nil},
		{"https://click.palletsprojects.com/", nil},
	}
	for _, tt := range tests {
		if got := summarize(c.lookupAll(ctx, spec(tt.spec))); !slices.Equal(got, tt.want) {
			t.Errorf("lookupAll(%s)\n got %+v\nwant %+v", tt.spec, got, tt.want)
		}
	}
	// Deprecated packages are returned; the resolver drops them.
	for _, l := range c.lookupAll(ctx, spec("django")) {
		if l.eco == ecoNPM && !l.info.deprecated {
			t.Errorf("npm django = %+v, want it returned and flagged deprecated", l.info)
		}
	}
	if u := ft.Unmatched(); len(u) != 0 {
		t.Errorf("unmatched requests: %v", u)
	}
}

// lookupAll asks the registries at once: each answer waits until all
// three requests are in flight.
func TestPkgClientLookupAllIsConcurrent(t *testing.T) {
	var mu sync.Mutex
	arrived := 0
	all := make(chan struct{})
	rt := pkgScriptedTransport(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		arrived++
		if arrived == 3 {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
		case <-time.After(5 * time.Second):
			return nil, errors.New("requests were made one at a time")
		}
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})
	c := newPkgClient(&http.Client{Transport: rt})
	for _, l := range c.lookupAll(context.Background(), libSpec{raw: "widget", name: "widget"}) {
		if !errors.Is(l.err, ErrNotFound) {
			t.Errorf("%s widget = %v, want ErrNotFound", l.eco, l.err)
		}
	}
}

// flakyOnce answers each URL's first request with status, later ones
// through rt.
type flakyOnce struct {
	rt     http.RoundTripper
	status int
	mu     sync.Mutex
	seen   map[string]bool
}

func (f *flakyOnce) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	first := !f.seen[req.URL.String()]
	f.seen[req.URL.String()] = true
	f.mu.Unlock()
	if !first {
		return f.rt.RoundTrip(req)
	}
	return &http.Response{
		Status: http.StatusText(f.status), StatusCode: f.status, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Retry-After": {"0"}}, Body: io.NopCloser(strings.NewReader("")), Request: req,
	}, nil
}

// A registry briefly busy (crates.io answering a burst with 429, a 503) is
// asked once more before it counts as a failed source.
func TestPkgClientRetriesTransientStatus(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		ft := testutil.LoadFixtures(t, registryFixtureDir)
		c := newPkgClient(&http.Client{Transport: &flakyOnce{rt: ft, status: status, seen: map[string]bool{}}})
		c.retryWait = time.Millisecond
		if got, err := c.lookup(context.Background(), ecoCrates, "clap"); err != nil || got.name != "clap" {
			t.Errorf("crates clap after a %d = %+v, %v; want the retry's answer", status, got, err)
		}
	}
}
