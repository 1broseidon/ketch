package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/urlrewrite"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const docsPageHTML = `<html><head><title>Configuring authentication</title></head><body><main><article><h1>Configuring authentication</h1><p>This guide explains how to configure authentication for a project, including login sessions and application settings.</p><p>Sessions expire after an hour of inactivity unless the remember-me option is enabled.</p></article></main></body></html>`

// The docs backends' fetcher runs on the server's own scraper and page cache:
// the operator's URL rewrites apply, and a page fetched twice is fetched once.
func TestDocsDepsFetchThroughServerScraperAndCache(t *testing.T) {
	testutil.SetIsolatedConfigHome(t)
	var hits atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/llms.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("# Example docs\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPageHTML))
	}))
	t.Cleanup(site.Close)

	cfg := config.Defaults()
	cfg.URLRewrites = []urlrewrite.Rule{{Match: `^https://docs\.example\.test/(.*)$`, Replace: site.URL + "/$1"}}
	srv, err := NewServer(&cfg, "test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	if srv.cache == nil {
		t.Fatal("isolated server has no page cache")
	}
	ctx, _ := withTagDiagnostics(context.Background())
	fetcher := srv.docsDeps(ctx).Fetcher

	raw, err := fetcher.FetchRaw(ctx, "https://docs.example.test/llms.txt")
	if err != nil || string(raw.Body) != "# Example docs\n" || !strings.HasPrefix(raw.ContentType, "text/plain") || raw.URL != "https://docs.example.test/llms.txt" {
		t.Fatalf("FetchRaw = %+v, %v; want llms.txt through the configured rewrite, reported at the requested URL", raw, err)
	}

	before := hits.Load()
	for i := range 2 {
		page, err := srv.docsDeps(ctx).Fetcher.FetchPage(ctx, "https://docs.example.test/guide/auth")
		if err != nil {
			t.Fatalf("FetchPage %d: %v", i, err)
		}
		if page.Title != "Configuring authentication" || !strings.Contains(page.Markdown, "remember-me option") || page.URL != "https://docs.example.test/guide/auth" {
			t.Fatalf("FetchPage %d = %+v; want the extracted article at the requested URL", i, page)
		}
	}
	if got := hits.Load() - before; got != 1 {
		t.Fatalf("two FetchPage calls made %d requests, want 1 (the second a page-cache hit)", got)
	}
}

// A server whose cache directory could not be prepared has a nil page cache;
// docs fetches then run uncached instead of calling through a nil *cache.Cache.
func TestDocsDepsWithoutPageCache(t *testing.T) {
	testutil.SetIsolatedConfigHome(t)
	var hits atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPageHTML))
	}))
	t.Cleanup(site.Close)

	cfg := config.Defaults()
	srv, err := NewServer(&cfg, "test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	srv.cache = nil

	ctx := context.Background()
	for i := range 2 {
		if page, err := srv.docsDeps(ctx).Fetcher.FetchPage(ctx, site.URL+"/guide/auth"); err != nil || !strings.Contains(page.Markdown, "Sessions expire") {
			t.Fatalf("uncached FetchPage %d = %+v, %v", i, page, err)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("uncached FetchPage made %d requests for two calls, want 2", got)
	}
}

// The docs tests below call the tool over an in-memory session, with every
// request (package registries, GitHub, docs sites, Context7) answered from
// fixtures under the services' real hostnames, so nothing reaches the
// network. The widget fixtures are the CLI tests' (cmd/testdata/docsresolve):
// a name npm's and PyPI's unrelated projects both publish, neither with
// enough stars to dominate.

// widgetFixtureDir holds the synthetic widget fixtures.
var widgetFixtureDir = filepath.Join("..", "cmd", "testdata", "docsresolve")

// fixtureFallback answers what no fixture covers the way the real services
// answer for a name they do not have: a search (GitHub repositories,
// pkg.go.dev) with no hits, anything else 404.
type fixtureFallback struct{ ft *testutil.FixtureTransport }

func (f fixtureFallback) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := f.ft.RoundTrip(req)
	if err == nil || !strings.HasPrefix(err.Error(), "testutil: no fixture") {
		return resp, err
	}
	status, body := http.StatusNotFound, ""
	switch req.URL.Host + req.URL.Path {
	case "api.github.com/search/repositories":
		status, body = http.StatusOK, `{"total_count":0,"incomplete_results":false,"items":[]}`
	case "pkg.go.dev/v1/search":
		status, body = http.StatusOK, `{"items":[]}`
	}
	h := http.Header{}
	if body != "" {
		h.Set("Content-Type", "application/json")
	}
	return &http.Response{
		Status: http.StatusText(status), StatusCode: status, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: h, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: req,
	}, nil
}

// docsSession serves the MCP tools over an in-memory session, with rt
// behind every HTTP client, an isolated config home and page cache, the
// built-in config (adjusted by configure) and a GitHub token from the
// environment, so `gh auth token` never runs.
func docsSession(t *testing.T, rt http.RoundTripper, configure ...func(*config.Config)) *mcpsdk.ClientSession {
	t.Helper()
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_GITHUB_TOKEN", "fixture-token")
	testutil.SwapDefaultTransport(t, rt)
	cfg := config.Defaults()
	for _, apply := range configure {
		apply(&cfg)
	}
	srv, err := NewServer(&cfg, "test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ct, st := mcpsdk.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, st) }()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// callDocs calls the docs tool and returns the result's text and, for a
// successful call, its structured output's matches as raw objects.
func callDocs(t *testing.T, session *mcpsdk.ClientSession, args map[string]any) (*mcpsdk.CallToolResult, []map[string]json.RawMessage) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "docs", Arguments: args})
	if err != nil {
		t.Fatalf("CallTool docs %v: %v", args, err)
	}
	if res.IsError {
		return res, nil
	}
	var out struct {
		Matches []map[string]json.RawMessage `json:"matches"`
	}
	data, err := json.Marshal(res.StructuredContent)
	if err == nil {
		err = json.Unmarshal(data, &out)
	}
	if err != nil {
		t.Fatalf("docs %v structured content: %v", args, err)
	}
	return res, out.Matches
}

// toolText concatenates a tool result's text content.
func toolText(res *mcpsdk.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func decodeMatches(t *testing.T, raw []map[string]json.RawMessage) []docs.LibraryMatch {
	t.Helper()
	data, _ := json.Marshal(raw)
	var out []docs.LibraryMatch
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func matchHost(t *testing.T, m docs.LibraryMatch) string {
	t.Helper()
	u, err := url.Parse(m.ID)
	if err != nil || u.Host == "" {
		t.Fatalf("match ID %q is not an absolute URL", m.ID)
	}
	return u.Host
}

// The ambiguity error reaches the caller whole, under [validation]: every
// candidate's ID and package specs, after the tool's own prefix.
func TestDocsAmbiguousLibraryReachesCallerIntact(t *testing.T) {
	amb := fmt.Errorf("web: %w", &docs.AmbiguousLibraryError{Library: "widget", Matches: []docs.LibraryMatch{
		{ID: "https://widgetjs.dev/docs/", Title: "Widget", Packages: []string{"npm:widget", "github:widgetjs/widget"}},
		{ID: "https://widget.readthedocs.io/en/stable/", Title: "Widget", Packages: []string{"pypi:widget", "github:pywidget/widget"}},
	}})
	got := upstreamErrf(amb, "docs fetch failed").Error()
	if want := "[validation] docs fetch failed: " + amb.Error(); got != want {
		t.Errorf("tool error:\n got %q\nwant %q", got, want)
	}
}

// The library, resolve and query descriptions, and the tool's own, say what
// each library provider accepts as a library, from its descriptor.
func TestDocsDescriptionsJoinLibraryHelp(t *testing.T) {
	var helps []string
	for _, id := range docs.LibraryBackends() {
		if p, _ := docs.Lookup(id); p.LibraryHelp != "" {
			helps = append(helps, p.Name+": "+p.LibraryHelp)
		}
	}
	if len(helps) == 0 {
		t.Fatal("no library provider declares LibraryHelp; the web backend should")
	}
	args := argDescriptions(t)
	for _, arg := range []string{"query", "library", "resolve"} {
		for _, want := range helps {
			if !strings.Contains(args["docs"][arg], want) {
				t.Errorf("docs.%s description %q does not mention %q", arg, args["docs"][arg], want)
			}
		}
	}

	session := docsSession(t, fixtureFallback{testutil.NewFixtureTransport()})
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "docs" && !strings.Contains(tool.Description, helps[0]) {
			t.Errorf("docs tool description %q does not mention %q", tool.Description, helps[0])
		}
	}
}

// Context7's matches go out exactly as before: the web backend's version,
// evidence and packages are omitted when empty.
func TestDocsToolContext7MatchesUnchanged(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://context7.com/api/v1/search?query=react", testutil.Fixture{Body: `{"results":[` +
		`{"id":"/facebook/react","title":"React","description":"The library for web and native user interfaces.","totalSnippets":3215,"trustScore":9,"versions":["v18.3.1"]}]}`})
	session := docsSession(t, ft, func(c *config.Config) { c.SetProvider("context7_api_key", "test-key") })

	res, raw := callDocs(t, session, map[string]any{"query": "react", "backend": "context7", "resolve": true})
	if res.IsError {
		t.Fatalf("resolve: %s", toolText(res))
	}
	want := []string{"description", "id", "title", "totalSnippets", "trustScore", "versions"}
	if len(raw) != 1 {
		t.Fatalf("got %d matches, want 1", len(raw))
	}
	if keys := slices.Sorted(maps.Keys(raw[0])); !slices.Equal(keys, want) {
		t.Errorf("Context7 match keys = %v, want %v", keys, want)
	}
}

// A web match carries its version, evidence and package specs in matches.
func TestDocsToolWebResolveMatchFields(t *testing.T) {
	session := docsSession(t, fixtureFallback{testutil.LoadFixtures(t, widgetFixtureDir)})

	res, raw := callDocs(t, session, map[string]any{"query": "pypi:widget", "backend": "web", "resolve": true})
	if res.IsError {
		t.Fatalf("resolve pypi:widget: %s", toolText(res))
	}
	matches := decodeMatches(t, raw)
	if len(matches) != 1 || matchHost(t, matches[0]) != "widget.readthedocs.io" {
		t.Fatalf("resolve pypi:widget = %+v, want PyPI's widget alone", matches)
	}
	for _, key := range []string{"id", "title", "trustScore", "version", "versions", "evidence", "packages"} {
		if _, ok := raw[0][key]; !ok {
			t.Errorf("match has no %q key: %v", key, slices.Sorted(maps.Keys(raw[0])))
		}
	}
	m := matches[0]
	if !slices.Contains(m.Packages, "pypi:widget") || len(m.Evidence) == 0 || m.TrustScore < 3 || !slices.Contains(m.Versions, "2.4") {
		t.Errorf("match = %+v, want pypi:widget among its packages, evidence, trust >= 3 and 2.4 among its versions", m)
	}
}

// Where one library is needed, an ambiguous name is [validation] naming
// every candidate: its ID and its package specs.
func TestDocsToolAmbiguousLibraryNamesTheCandidates(t *testing.T) {
	session := docsSession(t, fixtureFallback{testutil.LoadFixtures(t, widgetFixtureDir)})

	res, raw := callDocs(t, session, map[string]any{"query": "widget", "backend": "web", "resolve": true})
	if res.IsError {
		t.Fatalf("resolve widget: %s", toolText(res))
	}
	matches := decodeMatches(t, raw)
	if len(matches) != 2 {
		t.Fatalf("resolve widget = %+v, want npm's and PyPI's", matches)
	}

	for _, args := range []map[string]any{
		{"query": "configure themes", "backend": "web", "library": "widget"},
		{"query": "widget configure themes", "backend": "web"},
	} {
		res, _ := callDocs(t, session, args)
		text := toolText(res)
		if !res.IsError || !strings.HasPrefix(text, "[validation]") {
			t.Errorf("docs %v: isError=%v %q, want [validation]", args, res.IsError, text)
			continue
		}
		for _, m := range matches {
			if !strings.Contains(text, m.ID) {
				t.Errorf("docs %v: %q does not name %s", args, text, m.ID)
			}
		}
		for _, spec := range []string{"npm:widget", "pypi:widget"} {
			if !strings.Contains(text, spec) {
				t.Errorf("docs %v: %q does not name %s", args, text, spec)
			}
		}
	}
}

// A name no registry or repository has is [not_found], resolved or fetched.
func TestDocsToolUnknownLibraryIsNotFound(t *testing.T) {
	session := docsSession(t, fixtureFallback{testutil.NewFixtureTransport()})
	for _, args := range []map[string]any{
		{"query": "zzqxnosuchlib", "backend": "web", "resolve": true},
		{"query": "configure themes", "backend": "web", "library": "zzqxnosuchlib"},
	} {
		res, _ := callDocs(t, session, args)
		if text := toolText(res); !res.IsError || !strings.HasPrefix(text, "[not_found]") || !strings.Contains(text, "zzqxnosuchlib") {
			t.Errorf("docs %v: isError=%v %q, want [not_found] naming the library", args, res.IsError, text)
		}
	}
}

// downTransport answers every request 503: every source is down.
type downTransport struct{}

func (downTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		Status: "503 Service Unavailable", StatusCode: http.StatusServiceUnavailable, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req,
	}, nil
}

// With every source down, a bare query is [upstream], worth retrying, not
// [validation]: the library it names may well exist. A question that
// names no library is [validation] whatever is down (decision-15: the
// verb after "how do i" is no library's name).
func TestDocsToolBareQueryWithSourcesDownIsUpstream(t *testing.T) {
	session := docsSession(t, downTransport{})
	res, _ := callDocs(t, session, map[string]any{"query": "how do i configure the themes in starlight", "backend": "web"})
	if text := toolText(res); !res.IsError || !strings.HasPrefix(text, "[upstream]") {
		t.Errorf("bare query, every source down: isError=%v %q, want [upstream]", res.IsError, text)
	}
	res, _ = callDocs(t, session, map[string]any{"query": "how do i configure the themes", "backend": "web"})
	if text := toolText(res); !res.IsError || !strings.HasPrefix(text, "[validation]") {
		t.Errorf("bare query naming no library, every source down: isError=%v %q, want [validation]", res.IsError, text)
	}
}

// pypiDown answers PyPI with 503 and everything else through rt.
type pypiDown struct{ rt http.RoundTripper }

func (d pypiDown) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "pypi.org" {
		return downTransport{}.RoundTrip(req)
	}
	return d.rt.RoundTrip(req)
}

// With PyPI down, resolve lists npm's widget alone and its evidence says
// the list may be incomplete and why.
func TestDocsToolResolveSaysWhenIncomplete(t *testing.T) {
	session := docsSession(t, pypiDown{fixtureFallback{testutil.LoadFixtures(t, widgetFixtureDir)}})
	res, raw := callDocs(t, session, map[string]any{"query": "widget", "backend": "web", "resolve": true})
	if res.IsError {
		t.Fatalf("resolve widget: %s", toolText(res))
	}
	matches := decodeMatches(t, raw)
	if len(matches) != 1 || len(matches[0].Evidence) == 0 || !strings.HasPrefix(matches[0].Evidence[0], "this list may be incomplete: pypi widget") {
		t.Errorf("resolve widget with PyPI down = %+v, want npm's widget noting PyPI's failure", matches)
	}
}

// The auto backend reports which provider answered and what it fell
// through: web finds "widget" ambiguous, Context7 (keyless) picks PyPI's,
// one of web's candidates.
func TestDocsToolAutoReportsProvider(t *testing.T) {
	ft := testutil.LoadFixtures(t, widgetFixtureDir)
	ft.Handle(http.MethodGet, "https://context7.com/api/v1/search?query=widget+configure+themes", testutil.Fixture{
		Body: `{"results":[{"id":"/pywidget/widget","title":"Widget"}]}`,
	})
	ft.Handle(http.MethodGet, "https://context7.com/api/v2/context?libraryId=/pywidget/widget&query=widget+configure+themes&type=json&tokens=4000", testutil.Fixture{
		Body: `{"codeSnippets":[{"codeTitle":"Configure themes","codeId":"https://widget.readthedocs.io/en/stable/themes/","codeList":[{"code":"x"}]}],"infoSnippets":[]}`,
	})
	session := docsSession(t, fixtureFallback{ft})

	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "docs", Arguments: map[string]any{"query": "widget configure themes", "backend": "auto"}})
	if err != nil || res.IsError {
		t.Fatalf("auto: %v %s", err, toolText(res))
	}
	var out DocsOutput
	data, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Backend != "context7" || len(out.Results) != 1 || out.Results[0].Source != "context7" || !strings.HasPrefix(out.Errors["web"], "ambiguous library") {
		t.Errorf("auto output = %+v", out)
	}
	for _, c := range ft.Calls() {
		if strings.Contains(c.URL, "://context7.com/") && c.Header.Get("Authorization") != "" {
			t.Errorf("keyless Context7 request sent Authorization: %s", c.URL)
		}
	}

	// A named provider reports neither field.
	res, err = session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "docs", Arguments: map[string]any{"query": "widget configure themes", "backend": "web"}})
	if err != nil || !res.IsError || !strings.HasPrefix(toolText(res), "[validation] ") || strings.Contains(toolText(res), "context7") {
		t.Errorf("web: %v %s", err, toolText(res))
	}
}
