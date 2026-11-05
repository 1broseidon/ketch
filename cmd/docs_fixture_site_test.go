package cmd_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// The fixture docs sites the local index's end-to-end test reads
// (docs_index_e2e_test.go): a Mintlify-style, a Docusaurus-style and a
// plain-HTML site, each with a query and the section that answers it,
// fetched through the built ketch binary with the real scrape pipeline and
// page cache. Everything is served from httptest; every run is isolated()
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
