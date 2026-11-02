package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/internal/testutil"
)

// These tests are the web docs backend's acceptance check: a Mintlify-style,
// a Docusaurus-style and a plain-HTML fixture site each answer a query with
// the right section, through the built ketch binary and through the MCP docs
// tool on a real server, with the real scrape pipeline and page cache doing
// the fetching. Everything is served from httptest; every run is isolated()
// so no test reads or writes the user's config, page cache or tag index.

// fixtureSite serves testdata/docsweb/<name> the way a static host would:
// a path ending in / serves its index.html, a clean URL falls back to
// <path>.html, the content type follows the extension, and a miss is a 404
// with the site's 404.html when it has one. {{ORIGIN}} in a file is the
// site's own URL and {{OFFSITE}} the URL of a second server whose links are
// out of scope. Every path requested from either server is recorded.
type fixtureSite struct {
	*httptest.Server
	offsite *httptest.Server

	mu       sync.Mutex
	hits     []string
	offsites []string
}

func newFixtureSite(t *testing.T, name string) *fixtureSite {
	t.Helper()
	dir := filepath.Join("testdata", "docsweb", name)
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	site := &fixtureSite{}
	site.offsite = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.offsites = append(site.offsites, r.URL.Path)
		site.mu.Unlock()
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte("# Off-site\n\nOut of scope: the web backend must not fetch this.\n"))
	}))
	t.Cleanup(site.offsite.Close)
	// Unstarted, so site.Server is set before any handler can read site.URL.
	site.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.hits = append(site.hits, r.URL.Path)
		site.mu.Unlock()
		body, file, ok := readFixture(dir, r.URL.Path)
		if !ok {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if page, err := os.ReadFile(filepath.Join(dir, "404.html")); err == nil {
				body = page
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
			} else {
				body = []byte("404 page not found\n")
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(body)
			return
		}
		w.Header().Set("Content-Type", fixtureContentType(file))
		_, _ = w.Write([]byte(site.expand(string(body))))
	}))
	site.Start()
	t.Cleanup(site.Close)
	return site
}

// readFixture maps a request path to a file under dir.
func readFixture(dir, urlPath string) ([]byte, string, bool) {
	p := path.Clean("/" + urlPath)
	if strings.HasSuffix(urlPath, "/") {
		p = path.Join(p, "index.html")
	}
	if p == "/404.html" {
		return nil, "", false
	}
	for _, candidate := range []string{p, p + ".html"} {
		file := filepath.Join(dir, filepath.FromSlash(candidate))
		info, err := os.Stat(file)
		if err != nil || info.IsDir() {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return nil, "", false
		}
		return body, file, true
	}
	return nil, "", false
}

func fixtureContentType(file string) string {
	switch filepath.Ext(file) {
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".xml":
		return "application/xml"
	}
	return "text/html; charset=utf-8"
}

func (s *fixtureSite) expand(body string) string {
	return strings.NewReplacer("{{ORIGIN}}", s.URL, "{{OFFSITE}}", s.offsite.URL).Replace(body)
}

func (s *fixtureSite) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.hits)
}

// checkScope fails when the run skipped a path it must request or fetched
// one it must not, or touched the off-site server at all.
func (s *fixtureSite) checkScope(t *testing.T, fx docsFixture) {
	t.Helper()
	got := s.requested()
	for _, p := range fx.fetched {
		if !slices.Contains(got, p) {
			t.Errorf("never requested %s; requests: %v", p, got)
		}
	}
	for _, p := range fx.never {
		if slices.Contains(got, p) {
			t.Errorf("requested %s, which the backend must not fetch; requests: %v", p, got)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.offsites) > 0 {
		t.Errorf("followed off-site links: %v", s.offsites)
	}
}

// docsFixture is one fixture site and the section its query must find.
type docsFixture struct {
	site    string // testdata/docsweb/<site>
	root    string // docs root path, the --library URL's path
	query   string
	page    string // path of the top result's page
	anchor  string // its fragment
	title   string
	crumb   string
	snippet []string // text the top snippet must contain
	fetched []string // paths the run must request
	never   []string // paths the run must not request
	// cached is whether the top result's page body ends up in the page
	// cache: HTML pages go through the scrape pipeline and its cache, and
	// so does the rendered page a raw Markdown source's results cite, read
	// for its anchors. A bookmark of page#anchor is warm when page is.
	cached bool
}

var docsFixtures = map[string]docsFixture{
	// llms.txt lists .md pages served as text/markdown; there is no
	// llms-full.txt. Pages are read raw and shown at their rendered URL,
	// which is fetched once to check the anchor a result cites.
	"mintlify": {
		site:    "mintlify",
		root:    "/",
		query:   "verify webhook signatures",
		page:    "/guides/webhooks",
		anchor:  "verify-webhook-signatures",
		title:   "Verify webhook signatures",
		crumb:   "Webhooks > Verify webhook signatures",
		snippet: []string{"Acme-Signature header", "```js\nconst valid = acme.webhooks.verify("},
		fetched: []string{"/llms.txt", "/llms-full.txt", "/guides/webhooks.md", "/guides/webhooks"},
		never:   []string{"/guides/webhooks.html", "/robots.txt", "/sitemap.xml"},
		cached:  true,
	},
	// No llms.txt (the host's 404 page answers it), a sitemap.xml that also
	// lists a blog post outside /docs/, and HTML pages whose headings carry
	// Docusaurus permalinks, one with a custom heading ID that differs from
	// the slug of its text.
	"docusaurus": {
		site:    "docusaurus",
		root:    "/docs/",
		query:   "collection schema options",
		page:    "/docs/guides/content-collections",
		anchor:  "schema-options",
		title:   "Collection schema options",
		crumb:   "Content collections > Defining a collection > Collection schema options",
		snippet: []string{"zod schema to validate", "z.object({ title: z.string()"},
		fetched: []string{"/docs/llms.txt", "/llms.txt", "/sitemap.xml", "/docs/guides/content-collections"},
		never:   []string{"/blog/collection-schema-options"},
		cached:  true,
	},
	// No llms.txt and no sitemap: the root page's links are all there is,
	// and two of them (another path on the host, another host) are out of
	// scope and titled to match the query.
	"plain": {
		site:    "plain",
		root:    "/manual/",
		query:   "environment variables",
		page:    "/manual/configuration.html",
		anchor:  "environment-variables",
		title:   "Environment variables",
		crumb:   "Configuration > Environment variables",
		snippet: []string{"Environment variables win over the file", "```\nWIDGET_LISTEN=0.0.0.0:9000 widget serve\n```"},
		fetched: []string{"/manual/llms.txt", "/llms.txt", "/robots.txt", "/manual/sitemap.xml", "/sitemap.xml", "/manual/", "/manual/configuration.html"},
		never:   []string{"/blog/environment-variables.html"},
		cached:  true,
	},
}

// docsResult mirrors docs.Result as the surfaces serialize it.
type docsResult struct {
	Library    string `json:"library"`
	Title      string `json:"title"`
	Breadcrumb string `json:"breadcrumb"`
	Snippet    string `json:"snippet"`
	URL        string `json:"url"`
	Source     string `json:"source"`
}

// wantResultKeys is the JSON shape of a web docs result. version is
// omitempty and the web backend never sets it.
var wantResultKeys = []string{"breadcrumb", "library", "snippet", "source", "title", "url"}

// checkTop asserts results lead with the fixture's section.
func checkTop(t *testing.T, site *fixtureSite, fx docsFixture, results []docsResult) {
	t.Helper()
	if len(results) == 0 {
		t.Fatal("no results")
	}
	top := results[0]
	want := docsResult{
		Library:    site.URL + fx.root,
		Title:      fx.title,
		Breadcrumb: fx.crumb,
		URL:        site.URL + fx.page + "#" + fx.anchor,
		Source:     "web",
	}
	got := top
	got.Snippet = ""
	if got != want {
		t.Errorf("top result:\n got %+v\nwant %+v", got, want)
	}
	for _, s := range fx.snippet {
		if !strings.Contains(top.Snippet, s) {
			t.Errorf("top snippet is missing %q:\n%s", s, top.Snippet)
		}
	}
	for i, r := range results {
		if r.Source != "web" || r.Library != want.Library {
			t.Errorf("result %d: source %q, library %q; want web, %q", i, r.Source, r.Library, want.Library)
		}
	}
}

// checkResultKeys asserts every result object has exactly the result keys.
func checkResultKeys(t *testing.T, raw []map[string]json.RawMessage) {
	t.Helper()
	for i, r := range raw {
		if keys := slices.Sorted(maps.Keys(r)); !slices.Equal(keys, wantResultKeys) {
			t.Errorf("result %d keys = %v, want %v", i, keys, wantResultKeys)
		}
	}
}

func decodeResults(t *testing.T, raw []map[string]json.RawMessage) []docsResult {
	t.Helper()
	out := make([]docsResult, len(raw))
	for i, r := range raw {
		b, _ := json.Marshal(r)
		if err := json.Unmarshal(b, &out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func showTag(t *testing.T, out string) cache.TagView {
	t.Helper()
	var view cache.TagView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("tag show output %q: %v", out, err)
	}
	return view
}

// checkBookmarkWarmth asserts a one-entry tag view reports the page body
// behind the bookmarked page#anchor as cached exactly when it is.
func checkBookmarkWarmth(t *testing.T, fx docsFixture, view cache.TagView) {
	t.Helper()
	wantCount := 0
	if fx.cached {
		wantCount = 1
	}
	if view.Pages[0].Cached != fx.cached || view.Cached != wantCount || view.CacheStatus != "available" {
		t.Errorf("bookmark %s: cached %v (%d of %d, %s); want %v: the fragment never reaches the server, so the page's cached body is the bookmark's",
			view.Pages[0].URL, view.Pages[0].Cached, view.Cached, view.Entries, view.CacheStatus, fx.cached)
	}
}

func TestDocsWebFixtureSitesCLI(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(docsFixtures)) {
		fx := docsFixtures[name]
		t.Run(name, func(t *testing.T) {
			isolated(t)
			site := newFixtureSite(t, fx.site)
			library := site.URL + fx.root

			// --json prints the bare result array, best first.
			out := mustCLI(t, "--json", "docs", "-b", "web", "--library", library, fx.query)
			var raw []map[string]json.RawMessage
			if err := json.Unmarshal([]byte(out), &raw); err != nil {
				t.Fatalf("docs --json stdout is not a JSON array: %v\n%s", err, out)
			}
			checkResultKeys(t, raw)
			results := decodeResults(t, raw)
			checkTop(t, site, fx, results)
			site.checkScope(t, fx)

			// The text output leads with the same section.
			text := mustCLI(t, "docs", "-b", "web", "--library", library, fx.query)
			for _, want := range []string{"backend: web\n", "library: " + library + "\n", "[" + fx.crumb + "]\n", "source: " + results[0].URL + "\n"} {
				if !strings.Contains(text, want) {
					t.Errorf("text output is missing %q:\n%s", want, text)
				}
			}
			if first := strings.Index(text, "\n["); first < 0 || !strings.HasPrefix(text[first+1:], "["+fx.crumb+"]") {
				t.Errorf("text output does not lead with [%s]:\n%s", fx.crumb, text)
			}

			// --tag records the returned result's URL, anchor included.
			mustCLI(t, "docs", "-b", "web", "--library", library, "--limit", "1", "--tag", "web-docs", fx.query)
			view := showTag(t, mustCLI(t, "tag", "show", "web-docs", "--json"))
			if view.Entries != 1 || len(view.Pages) != 1 || view.Pages[0].URL != results[0].URL {
				t.Fatalf("tag web-docs = %+v, want exactly %s", view, results[0].URL)
			}
			if !strings.Contains(view.Pages[0].Title, fx.title) || view.Pages[0].Description == "" {
				t.Errorf("bookmark title %q / description %q, want the section heading and snippet", view.Pages[0].Title, view.Pages[0].Description)
			}
			checkBookmarkWarmth(t, fx, view)
		})
	}
}

// mcpDocs calls the docs tool and decodes its structured output.
func mcpDocs(t *testing.T, args map[string]any) []docsResult {
	t.Helper()
	r := call(t, session(t), "docs", args)
	if r.IsError {
		t.Fatalf("docs tool: %s", output(r))
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output(r)), &payload); err != nil {
		t.Fatalf("docs tool output %q: %v", output(r), err)
	}
	if keys := slices.Sorted(maps.Keys(payload)); !slices.Equal(keys, []string{"results"}) {
		t.Errorf("docs tool output keys = %v, want [results]", keys)
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(payload["results"], &raw); err != nil {
		t.Fatal(err)
	}
	checkResultKeys(t, raw)
	return decodeResults(t, raw)
}

func TestDocsWebFixtureSitesMCP(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(docsFixtures)) {
		fx := docsFixtures[name]
		t.Run(name, func(t *testing.T) {
			isolated(t)
			site := newFixtureSite(t, fx.site)
			library := site.URL + fx.root

			results := mcpDocs(t, map[string]any{"query": fx.query, "backend": "web", "library": library})
			checkTop(t, site, fx, results)
			site.checkScope(t, fx)

			s := session(t)
			r := call(t, s, "docs", map[string]any{"query": fx.query, "backend": "web", "library": library, "limit": 1, "tag": "web-docs"})
			if r.IsError {
				t.Fatalf("tagged docs call: %s", output(r))
			}
			r = call(t, s, "tag", map[string]any{"operation": "show", "tag": "web-docs"})
			view := showTag(t, output(r))
			if r.IsError || view.Entries != 1 || len(view.Pages) != 1 || view.Pages[0].URL != results[0].URL {
				t.Fatalf("tag web-docs = %s, want exactly %s", output(r), results[0].URL)
			}
			checkBookmarkWarmth(t, fx, view)
		})
	}
}

// refusingProxy is an HTTP(S) proxy that refuses every request (403, CONNECT
// included) and records what was asked of it. A binary run with it as
// HTTP_PROXY and HTTPS_PROXY reaches no live service.
func refusingProxy(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Method+" "+r.Host)
		mu.Unlock()
		http.Error(w, "offline test", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(asked)
	}
}

// offlineCLI runs the binary like cli, with every proxy variable pointing
// at proxy (and none bypassed), so a request it makes goes nowhere live.
func offlineCLI(t *testing.T, proxy string, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy, "http_proxy="+proxy, "https_proxy="+proxy, "NO_PROXY=", "no_proxy=")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	return code, out.String(), stderr.String()
}

// notFoundTransport answers every request the way the registries, GitHub
// and docs sites answer for a name they do not have: searches with no
// hits, anything else 404. It counts the requests.
type notFoundTransport struct{ n *atomic.Int32 }

func (t notFoundTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.n.Add(1)
	status, body := http.StatusNotFound, ""
	switch req.URL.Host + req.URL.Path {
	case "api.github.com/search/repositories":
		status, body = http.StatusOK, `{"total_count":0,"incomplete_results":false,"items":[]}`
	case "pkg.go.dev/v1/search":
		status, body = http.StatusOK, `{"items":[]}`
	}
	return &http.Response{
		Status: http.StatusText(status), StatusCode: status, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)), Request: req,
	}, nil
}

// A web docs query that names no library is input the caller must fix: exit
// 2 on the CLI, [validation] over MCP, and no request leaves the process.
// The binary runs behind a proxy that refuses everything, and the MCP
// server's HTTP client is swapped for a fixture transport with no
// fixtures, so neither can reach the network whatever it tries. A query
// whose words do name candidates that no registry has is input to fix too;
// over MCP it is answered by notFoundTransport.
func TestDocsWebWithoutLibraryIsValidation(t *testing.T) {
	isolated(t)
	t.Setenv("KETCH_GITHUB_TOKEN", "fixture-token") // so `gh auth token` never runs
	proxy, asked := refusingProxy(t)
	code, out, stderr := offlineCLI(t, proxy.URL, "docs", "-b", "web", "how to verify")
	if code != 2 || out != "" || !strings.Contains(stderr, "--library") {
		t.Fatalf("docs -b web naming no library: exit %d, stdout %q, stderr %q; want exit 2 naming --library", code, out, stderr)
	}
	code, _, stderr = offlineCLI(t, proxy.URL, "--json", "docs", "-b", "web", "how to verify")
	if code != 2 {
		t.Fatalf("docs --json -b web naming no library: exit %d, stderr %q; want 2", code, stderr)
	}
	if got := asked(); len(got) > 0 {
		t.Errorf("a query naming no library made requests: %v", got)
	}

	ft := testutil.NewFixtureTransport()
	testutil.SwapDefaultTransport(t, ft)
	r := call(t, session(t), "docs", map[string]any{"query": "how to verify", "backend": "web"})
	if !r.IsError || !strings.HasPrefix(output(r), "[validation]") || !strings.Contains(output(r), "library") {
		t.Fatalf("MCP docs backend web naming no library: isError=%v %q; want [validation] naming library", r.IsError, output(r))
	}
	if calls := ft.Calls(); len(calls) > 0 {
		t.Errorf("a query naming no library made requests over MCP: %v", calls)
	}

	var n atomic.Int32
	testutil.SwapDefaultTransport(t, notFoundTransport{&n})
	r = call(t, session(t), "docs", map[string]any{"query": "verify webhook signatures", "backend": "web"})
	if !r.IsError || !strings.HasPrefix(output(r), "[validation]") || !strings.Contains(output(r), "library") {
		t.Fatalf("MCP docs backend web, no candidate published: isError=%v %q; want [validation] naming library", r.IsError, output(r))
	}
	if n.Load() == 0 {
		t.Error("the candidates were never looked up")
	}
}

// statusSite answers every request with status, recording the paths.
func statusSite(t *testing.T, status int) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte("<!doctype html><html><head><title>Error</title></head><body><h1>Error</h1><p>Nothing here.</p></body></html>"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(hits)
	}
}

// A docs root that does not exist — llms.txt, the sitemaps and the root page
// all 404 (or 410) — is not-found: exit 3, [not_found], not a retryable
// upstream failure. The missing llms.txt and sitemaps are only probes, so
// discovery still reaches the root page before giving up. A server that is
// failing (503) stays upstream.
func TestDocsWebMissingRootIsNotFound(t *testing.T) {
	cases := []struct {
		status int
		exit   int
		kind   string
	}{
		{http.StatusNotFound, 3, "[not_found]"},
		{http.StatusGone, 3, "[not_found]"},
		{http.StatusServiceUnavailable, 4, "[upstream]"},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			isolated(t)
			srv, requested := statusSite(t, tc.status)
			library := srv.URL + "/no-such-docs/"

			code, _, stderr := cli(t, "docs", "-b", "web", "--library", library, "configure logging")
			if code != tc.exit {
				t.Fatalf("docs --library %s on a %d site: exit %d, want %d; stderr: %s", library, tc.status, code, tc.exit, stderr)
			}
			for _, p := range []string{"/no-such-docs/llms.txt", "/llms.txt", "/robots.txt", "/no-such-docs/sitemap.xml", "/sitemap.xml", "/no-such-docs/"} {
				if !slices.Contains(requested(), p) {
					t.Errorf("discovery never tried %s before failing; requests: %v", p, requested())
				}
			}
			if tc.exit == 3 && !strings.Contains(stderr, "tried ") {
				t.Errorf("not-found message should say what was tried: %s", stderr)
			}

			code, _, stderr = cli(t, "docs", "-b", "web", "--resolve", library)
			if code != tc.exit {
				t.Errorf("docs --resolve %s on a %d site: exit %d, want %d; stderr: %s", library, tc.status, code, tc.exit, stderr)
			}

			r := call(t, session(t), "docs", map[string]any{"query": "configure logging", "backend": "web", "library": library})
			if !r.IsError || !strings.HasPrefix(output(r), tc.kind) {
				t.Fatalf("MCP docs on a %d site: isError=%v %q; want %s", tc.status, r.IsError, output(r), tc.kind)
			}
		})
	}
}

// redirectingDocsSite is an llms.txt docs site under /docs/ whose listed
// pages redirect away: off to another host (a Markdown page, an HTML page
// read through the scrape pipeline, and llms-full.txt naming in-scope
// pages) and, on the same host, out of /docs/. One more stays in /docs/.
// The off-site and out-of-prefix text says so.
func redirectingDocsSite(t *testing.T) *httptest.Server {
	t.Helper()
	var siteURL atomic.Value // set once the site is up; read by the off-site handler
	offsite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/keys":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>Signing keys</title></head><body><main><h1>Signing keys</h1><p>Off-site text: rotate the webhook signature signing keys every quarter, as this other host says.</p><p>More off-site webhook signature prose so the page is clearly a real article worth extracting.</p></main></body></html>`))
		case "/llms-full.txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("# Verify webhook signatures\nSource: " + siteURL.Load().(string) + "/docs/webhooks\n\nOff-site text: a full dump about webhook signatures.\n"))
		default:
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write([]byte("# Off-site\n\nOff-site text about webhook signature secrets.\n"))
		}
	}))
	t.Cleanup(offsite.Close)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/docs/llms.txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("# Acme\n\n- [Verify webhook signatures](/docs/webhooks.md)\n- [Webhook signature secrets](/docs/secrets.md)\n- [Webhook signature keys](/docs/keys)\n- [Webhook signature rotation](/docs/rotation.md)\n"))
		case "/docs/webhooks.md":
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write([]byte("# Verify webhook signatures\n\nCheck the Acme-Signature header on every webhook signature you receive.\n"))
		case "/docs/llms-full.txt":
			http.Redirect(w, r, offsite.URL+"/llms-full.txt", http.StatusFound)
		case "/docs/secrets.md":
			http.Redirect(w, r, offsite.URL+"/secrets.md", http.StatusFound)
		case "/docs/keys":
			http.Redirect(w, r, offsite.URL+"/keys", http.StatusFound)
		case "/docs/rotation.md":
			http.Redirect(w, r, "/private/rotation.md", http.StatusMovedPermanently)
		case "/private/rotation.md":
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write([]byte("# Rotation\n\nPrivate text: webhook signature rotation notes outside the docs.\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.Close)
	siteURL.Store(site.URL)
	return site
}

// The scraper follows redirects for `ketch scrape`, so the docs backend
// checks where every response came from: a page, llms-full.txt or HTML
// page that redirects off the host or out of the docs root is dropped, on
// both surfaces, rather than returned under the in-scope URL it was asked
// for.
func TestDocsWebRedirectsOutOfScopeAreDropped(t *testing.T) {
	check := func(t *testing.T, site *httptest.Server, results []docsResult) {
		t.Helper()
		if len(results) == 0 {
			t.Fatal("no results; want the page that stayed in /docs/")
		}
		for _, r := range results {
			if !strings.HasPrefix(r.URL, site.URL+"/docs/webhooks") || strings.Contains(r.Snippet, "Off-site") || strings.Contains(r.Snippet, "Private") {
				t.Errorf("result %s carries text from outside the docs root:\n%s", r.URL, r.Snippet)
			}
		}
	}
	t.Run("CLI", func(t *testing.T) {
		isolated(t)
		site := redirectingDocsSite(t)
		out := mustCLI(t, "--json", "docs", "-b", "web", "--library", site.URL+"/docs/", "webhook signature")
		var results []docsResult
		if err := json.Unmarshal([]byte(out), &results); err != nil {
			t.Fatalf("docs --json: %v\n%s", err, out)
		}
		check(t, site, results)
	})
	t.Run("MCP", func(t *testing.T) {
		isolated(t)
		site := redirectingDocsSite(t)
		check(t, site, mcpDocs(t, map[string]any{"query": "webhook signature", "backend": "web", "library": site.URL + "/docs/"}))
	})
}
