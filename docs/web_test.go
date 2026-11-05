package docs

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/search"
)

// fakeRaw is a FetchRaw response.
type fakeRaw struct {
	body, contentType string
}

// fakePage is a FetchPage response: what the scrape pipeline would return.
type fakePage struct {
	title, markdown string
	anchors         []PageAnchor
	shell           bool // an unrendered JS shell
}

// fakeFetcher serves fixed responses and records every call. Unknown URLs
// fail like a 404 (wrapping ErrNotFound); fail makes every call fail with
// a plain error; block makes calls wait for their context. redirects sends
// a URL on to another, as a fetcher that follows redirects would: the
// response is the target's, reported as served from the target.
type fakeFetcher struct {
	raw       map[string]fakeRaw
	pages     map[string]fakePage
	redirects map[string]string
	fail      error
	block     bool
	delay     time.Duration

	mu       sync.Mutex
	calls    []string
	inFlight atomic.Int32
	peak     atomic.Int32
}

func (f *fakeFetcher) enter(ctx context.Context, kind, url string) error {
	f.mu.Lock()
	f.calls = append(f.calls, kind+" "+url)
	f.mu.Unlock()
	n := f.inFlight.Add(1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return f.fail
}

// follow returns where url's redirects end.
func (f *fakeFetcher) follow(url string) string {
	for range 10 {
		next, ok := f.redirects[url]
		if !ok {
			break
		}
		url = next
	}
	return url
}

func (f *fakeFetcher) FetchRaw(ctx context.Context, url string) (RawContent, error) {
	defer f.inFlight.Add(-1)
	if err := f.enter(ctx, "raw", url); err != nil {
		return RawContent{}, err
	}
	served := f.follow(url)
	r, ok := f.raw[served]
	if !ok {
		return RawContent{}, fmt.Errorf("fake: %s: status 404: %w", served, ErrNotFound)
	}
	return RawContent{Body: []byte(r.body), ContentType: r.contentType, URL: served}, nil
}

func (f *fakeFetcher) FetchPage(ctx context.Context, url string) (PageContent, error) {
	defer f.inFlight.Add(-1)
	if err := f.enter(ctx, "page", url); err != nil {
		return PageContent{}, err
	}
	served := f.follow(url)
	p, ok := f.pages[served]
	if !ok {
		return PageContent{}, fmt.Errorf("fake: %s: status 404: %w", served, ErrNotFound)
	}
	return PageContent{Title: p.title, Markdown: p.markdown, URL: served, Anchors: p.anchors, Shell: p.shell}, nil
}

func (f *fakeFetcher) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// assertNeverFetched fails when any call touched a URL containing one of
// the given fragments.
func assertNeverFetched(t *testing.T, f *fakeFetcher, fragments ...string) {
	t.Helper()
	for _, c := range f.called() {
		for _, frag := range fragments {
			if strings.Contains(c, frag) {
				t.Errorf("fetched out-of-scope or unwanted URL: %s", c)
			}
		}
	}
}

func md(body string) fakeRaw    { return fakeRaw{body: body, contentType: "text/markdown; charset=utf-8"} }
func plain(body string) fakeRaw { return fakeRaw{body: body, contentType: "text/plain; charset=utf-8"} }
func html(body string) fakeRaw  { return fakeRaw{body: body, contentType: "text/html; charset=utf-8"} }
func xmlDoc(body string) fakeRaw {
	return fakeRaw{body: `<?xml version="1.0" encoding="UTF-8"?>` + body, contentType: "application/xml"}
}

const fence = "```"

// mintlifySite is a Mintlify-style site: an llms.txt listing .md pages.
func mintlifySite() *fakeFetcher {
	return &fakeFetcher{raw: map[string]fakeRaw{
		"https://mint.test/llms.txt": plain(`# Mint Docs

> Build things with Mint.

## Guides

- [Quickstart](https://mint.test/quickstart.md): Install and run your first build
- [Content collections](https://mint.test/guides/content-collections.md): Organize Markdown with typed collections
- [Deploy](/guides/deploy.md)

## Elsewhere

- [Announcing collections](https://blog.mint.test/collections.md): off-host
- [Logo](https://mint.test/logo.png)
`),
		"https://mint.test/quickstart.md": md("---\ntitle: Quickstart\n---\n# Quickstart\n\nInstall the CLI with npm and run your first build in minutes.\n\n## Install\n\n" + fence + "bash\nnpm install mint\n" + fence + "\n"),
		"https://mint.test/guides/content-collections.md": md(`# Content collections

Collections group related Markdown files under src/content.

## Defining a collection

Define each collection with a schema in ` + "`src/content/config.ts`" + ` so frontmatter is type-checked:

` + fence + `ts
const blog = defineCollection({ schema: z.object({ title: z.string() }) });
` + fence + `

## Querying collections

Use getCollection() to load every entry of a collection.
`),
		"https://mint.test/guides/deploy.md": md("# Deploy\n\nDeploy the built site to any static host.\n"),
	}, pages: map[string]fakePage{
		// The rendered page a result cites, read only for its targets.
		"https://mint.test/guides/content-collections": {title: "Content collections", markdown: "rendered",
			anchors: []PageAnchor{{ID: "defining-a-collection", Text: "Defining a collection"}, {ID: "querying-collections", Text: "Querying collections"}}},
	}}
}

func TestWebMintlifyLLMSTxtMarkdownPages(t *testing.T) {
	f := mintlifySite()
	results, err := NewWeb(f).GetDocs(context.Background(), "https://mint.test", "define a content collection schema", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	top := results[0]
	want := Result{
		Library:    "https://mint.test/",
		Title:      "Defining a collection",
		Breadcrumb: "Content collections > Defining a collection",
		URL:        "https://mint.test/guides/content-collections#defining-a-collection",
		Source:     "web",
	}
	if top.Library != want.Library || top.Title != want.Title || top.Breadcrumb != want.Breadcrumb || top.URL != want.URL || top.Source != want.Source || top.Version != "" {
		t.Errorf("top result = %+v\nwant %+v", top, want)
	}
	if !strings.Contains(top.Snippet, fence+"ts\nconst blog = defineCollection(") || !strings.HasSuffix(top.Snippet, fence) {
		t.Errorf("snippet lost its code block:\n%s", top.Snippet)
	}
	// Text comes from the .md files through FetchRaw; the rendered pages
	// results cite are read only to check their anchors.
	for _, c := range f.called() {
		if strings.HasPrefix(c, "page ") && strings.HasSuffix(c, ".md") {
			t.Errorf(".md links must go through FetchRaw, got %s", c)
		}
	}
	if !slices.Contains(f.called(), "page https://mint.test/guides/content-collections") {
		t.Errorf("the cited page's anchors were never checked: %v", f.called())
	}
	assertNeverFetched(t, f, "blog.mint.test", "logo.png")
}

// docusaurusSite is a Docusaurus-style site under /docs/: no llms.txt (the
// origin answers it with the SPA's HTML), a robots.txt pointing at a
// sitemap index, and HTML pages read through FetchPage.
func docusaurusSite() *fakeFetcher {
	return &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://docu.test/llms.txt":   html("<!DOCTYPE html><html><body><div id=root></div></body></html>"),
			"https://docu.test/robots.txt": plain("User-agent: *\nAllow: /\n\nSitemap: https://docu.test/sitemap.xml\n"),
			"https://docu.test/sitemap.xml": xmlDoc(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<sitemap><loc>https://docu.test/sitemap-blog.xml</loc></sitemap>
<sitemap><loc>https://docu.test/sitemap-docs.xml</loc></sitemap>
</sitemapindex>`),
			"https://docu.test/sitemap-docs.xml": xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url><loc>https://docu.test/docs/intro</loc></url>
<url><loc>https://docu.test/docs/guides/content-collections</loc></url>
<url><loc>https://docu.test/docs/api/config</loc></url>
<url><loc>https://other.test/docs/guides/collection-schema-options</loc></url>
</urlset>`),
			"https://docu.test/sitemap-blog.xml": xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url><loc>https://docu.test/blog/collection-schema-options</loc></url>
</urlset>`),
		},
		pages: map[string]fakePage{
			"https://docu.test/docs/guides/content-collections": {title: "Content Collections | Docu", markdown: `# Content Collections

Content collections organize your docs into typed groups.

## Defining collections` + "[\u200b](#defining-collections \"Direct link to Defining collections\")" + `

Create a collection with defineCollection and a schema.

` + fence + `js
# not a heading: shell-style comment in a code block
export const collections = { docs: defineCollection({}) };
` + fence + `

### Collection schema options {#schema-options}

Schemas validate frontmatter with zod before the build runs.
`},
			"https://docu.test/docs/intro":      {title: "Introduction | Docu", markdown: "# Introduction\n\nWelcome to Docu, a static site generator for documentation.\n"},
			"https://docu.test/docs/api/config": {title: "Config | Docu", markdown: "# Config\n\nThe docu.config.js file sets the site title and plugins.\n"},
		},
	}
}

func TestWebDocusaurusSitemapPages(t *testing.T) {
	f := docusaurusSite()
	results, err := NewWeb(f).GetDocs(context.Background(), "https://docu.test/docs", "collection schema", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	top := results[0]
	if top.URL != "https://docu.test/docs/guides/content-collections#schema-options" {
		t.Errorf("top URL = %q, want the explicit {#schema-options} anchor", top.URL)
	}
	if top.Breadcrumb != "Content Collections > Defining collections > Collection schema options" || top.Title != "Collection schema options" {
		t.Errorf("top breadcrumb/title = %q / %q", top.Breadcrumb, top.Title)
	}
	if top.Library != "https://docu.test/docs/" {
		t.Errorf("library = %q, want the normalized root", top.Library)
	}
	var defining *Result
	for i := range results {
		if results[i].Title == "Defining collections" {
			defining = &results[i]
		}
	}
	if defining == nil {
		t.Fatalf("missing the Defining collections section: %+v", results)
	}
	if defining.URL != "https://docu.test/docs/guides/content-collections#defining-collections" {
		t.Errorf("permalink anchor not used: %q", defining.URL)
	}
	if !strings.Contains(defining.Snippet, "# not a heading: shell-style comment") {
		t.Errorf("fenced # comment was split out as a heading:\n%s", defining.Snippet)
	}
	if !slices.Contains(f.called(), "page https://docu.test/docs/guides/content-collections") {
		t.Errorf("HTML pages must go through FetchPage: %v", f.called())
	}
	assertNeverFetched(t, f, "other.test", "/blog/")
}

// plainSite has no llms.txt and no sitemap: only links on the root page.
func plainSite() *fakeFetcher {
	return &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://plain.test/": html(`<!doctype html><html><head><title>Plain Docs | Plain</title>
<meta name="description" content="Docs for the Plain framework."></head>
<body><nav>
<a href="/guide/install.html">Install</a>
<a href="guide/database.html">Database setup</a>
<a href="https://elsewhere.test/guide/database.html">Configure the database elsewhere</a>
<a href="/style.css">Database styles</a>
<a href="#top">Top</a>
</nav><p>Welcome to Plain.</p></body></html>`),
		},
		pages: map[string]fakePage{
			"https://plain.test/":                    {title: "Plain Docs", markdown: "Welcome to Plain, the plain framework for plain apps.\n"},
			"https://plain.test/guide/install.html":  {title: "Install", markdown: "# Install\n\nRun the installer script to set up Plain on your machine.\n"},
			"https://plain.test/guide/database.html": {title: "Database setup", markdown: "# Database setup\n\nPlain stores application data in SQLite by default.\n\n## Configure the database\n\nSet DATABASE_URL in plain.toml to configure the database connection.\n"},
		},
	}
}

func TestWebPlainSiteRootLinks(t *testing.T) {
	f := plainSite()
	results, err := NewWeb(f).GetDocs(context.Background(), "https://plain.test/", "configure database", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) == 0 || results[0].URL != "https://plain.test/guide/database.html#configure-the-database" {
		t.Fatalf("top result = %+v, want the Configure the database section", results)
	}
	if results[0].Breadcrumb != "Database setup > Configure the database" {
		t.Errorf("breadcrumb = %q", results[0].Breadcrumb)
	}
	assertNeverFetched(t, f, "elsewhere.test", "style.css")
}

// fullSite publishes llms-full.txt beside llms.txt.
func fullSite() *fakeFetcher {
	return &fakeFetcher{raw: map[string]fakeRaw{
		"https://full.test/llms.txt": plain("# Full\n\n- [Alpha](/alpha.md)\n- [Beta](/beta.md)\n- [Gamma](/gamma.md)\n"),
		"https://full.test/llms-full.txt": plain(`# Alpha
Source: https://full.test/alpha

Alpha is the first module of the toolkit.

## Alpha widgets

Widgets in alpha are configured with widget.yaml files.

# Beta

Beta talks about gadgets in great detail.

---
url: /gamma.md
---
# Gamma

Gamma covers widgets too, but only briefly.

# Offsite
Source: https://other.test/offsite

Widgets offsite must never appear in results.
`),
		"https://full.test/alpha.md": md("# Alpha\n\nAlpha widgets fetched per page.\n"),
		"https://full.test/beta.md":  md("# Beta\n\nBeta gadgets fetched per page.\n"),
		"https://full.test/gamma.md": md("# Gamma\n\nGamma widgets fetched per page.\n"),
	}, pages: map[string]fakePage{
		"https://full.test/alpha": {title: "Alpha", markdown: "rendered", anchors: []PageAnchor{{ID: "alpha-widgets", Text: "Alpha widgets"}}},
	}}
}

func TestWebLLMSFullTextNeedsNoPageFetches(t *testing.T) {
	f := fullSite()
	results, err := NewWeb(f).GetDocs(context.Background(), "https://full.test", "widgets", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	var urls []string
	for _, r := range results {
		urls = append(urls, r.URL)
	}
	// Alpha's rendered page has the section's id; Gamma's could not be
	// read, and its intro cites the page anyway.
	want := []string{"https://full.test/alpha#alpha-widgets", "https://full.test/gamma"}
	if !slices.Equal(urls, want) {
		t.Errorf("result URLs = %v, want %v", urls, want)
	}
	assertNeverFetched(t, f, ".md", "other.test")
}

func TestWebLLMSFullTextOverCapFallsBackToPages(t *testing.T) {
	f := fullSite()
	w := NewWeb(f)
	w.maxFullBytes = 64
	results, err := w.GetDocs(context.Background(), "https://full.test", "widgets", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) == 0 || !strings.Contains(results[0].Snippet, "fetched per page") {
		t.Fatalf("oversized llms-full.txt should fall back to page fetches: %+v", results)
	}
}

func TestWebScopeIgnoresOffHostAndOutOfPrefixLinks(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://scope.test/llms.txt": plain(`# Scope
- [Routing in v2](https://scope.test/v2/routing.md)
- [Routing in v1](https://scope.test/v1/routing.md)
- [Routing elsewhere](https://evil.test/v2/routing.md)
- [Routing lookalike](https://scope.test/v2-beta/routing.md)
`),
		"https://scope.test/v2/routing.md":      md("# Routing\n\nRouting in version two uses file-based routes.\n"),
		"https://scope.test/v1/routing.md":      md("# Routing\n\nRouting in version one used a routes table.\n"),
		"https://scope.test/v2-beta/routing.md": md("# Routing\n\nRouting in the beta is experimental.\n"),
	}}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://scope.test/v2/", "routing", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	for _, r := range results {
		if !strings.HasPrefix(r.URL, "https://scope.test/v2/") {
			t.Errorf("out-of-scope result %s", r.URL)
		}
	}
	if len(results) != 1 {
		t.Errorf("got %d results, want only the v2 page", len(results))
	}
	assertNeverFetched(t, f, "/v1/", "evil.test", "v2-beta")
}

func TestWebRootScope(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://docs.astro.build", "https://docs.astro.build/"},
		{"HTTPS://Docs.Example.com:443/en?x=1#frag", "https://docs.example.com/en/"},
		{"http://localhost:8080/docs/llms.txt", "http://localhost:8080/docs/"},
		{"https://x.test/llms-full.txt", "https://x.test/"},
	} {
		root, err := parseWebRoot(tc.in)
		if err != nil || root.String() != tc.want {
			t.Errorf("parseWebRoot(%q) = %q, %v; want %q", tc.in, root.String(), err, tc.want)
		}
	}
	root, _ := parseWebRoot("https://x.test/en")
	for _, tc := range []struct {
		u    string
		want bool
	}{
		{"https://x.test/en", true}, {"https://x.test/en/", true}, {"https://x.test/en/guide/a", true},
		{"http://x.test/en/a", true}, {"https://X.test:443/en/a", true},
		{"https://x.test/english", false}, {"https://x.test/", false}, {"https://y.test/en/a", false},
		{"https://x.test:8443/en/a", false}, {"ftp://x.test/en/a", false},
		{"https://www.x.test/en/a", true}, {"https://docs.x.test/en/a", false},
	} {
		u, _ := root.u.Parse(tc.u)
		if got := root.contains(u); got != tc.want {
			t.Errorf("contains(%s) = %v, want %v", tc.u, got, tc.want)
		}
	}
}

// budgetSite has one page with many matching sections of about 300 bytes.
func budgetSite() *fakeFetcher {
	var b strings.Builder
	b.WriteString("# Budget\n\nIntro without the term.\n\n")
	for i := range 12 {
		fmt.Fprintf(&b, "## Widget part %d\n\n%s\n\n", i, strings.Repeat(fmt.Sprintf("widget detail %d. ", i), 18))
	}
	return &fakeFetcher{raw: map[string]fakeRaw{
		"https://budget.test/llms.txt":   plain("# Budget\n- [Widgets](/widgets.md)\n"),
		"https://budget.test/widgets.md": md(b.String()),
	}}
}

func snippetBytes(results []Result) int {
	n := 0
	for _, r := range results {
		n += len(r.Snippet)
	}
	return n
}

func TestWebTokenBudgetBoundsSnippetText(t *testing.T) {
	w := NewWeb(budgetSite())
	for _, tokens := range []int{30, 100, 130, 250, 600, 1000} {
		results, err := w.GetDocs(context.Background(), "https://budget.test", "widget", tokens)
		if err != nil {
			t.Fatalf("GetDocs: %v", err)
		}
		budget, used := tokens*webCharsPerToken, snippetBytes(results)
		if used > budget {
			t.Errorf("tokens %d: %d snippet bytes exceed the %d-byte budget", tokens, used, budget)
		}
		if len(results) == 0 {
			t.Fatalf("tokens %d: no results", tokens)
		}
		// Either everything fit, the last snippet was trimmed to the budget,
		// or what was left was too small to be worth a trimmed snippet.
		trimmed := strings.HasSuffix(results[len(results)-1].Snippet, "[truncated]")
		if len(results) < 12 && !trimmed && budget-used >= webMinTrimBytes {
			t.Errorf("tokens %d: %d bytes left unused without trimming the last snippet", tokens, budget-used)
		}
	}
	for _, tokens := range []int{30, 130, 600} {
		results, _ := w.GetDocs(context.Background(), "https://budget.test", "widget", tokens)
		if last := results[len(results)-1]; !strings.HasSuffix(last.Snippet, "[truncated]") {
			t.Errorf("tokens %d: last snippet should be trimmed to fit, got %q", tokens, last.Snippet)
		}
	}
}

func TestWebDefaultBudgetAndLimit(t *testing.T) {
	w := NewWeb(budgetSite())
	all, _ := w.GetDocs(context.Background(), "https://budget.test", "widget", 0)
	if len(all) != 12 || snippetBytes(all) > webDefaultTokens*webCharsPerToken {
		t.Errorf("tokens 0 should mean the 4000-token default: %d results, %d bytes", len(all), snippetBytes(all))
	}
	// Ties rank in page order, so the parts come back in document order.
	if all[0].Title != "Widget part 0" || all[11].Title != "Widget part 11" {
		t.Errorf("tied sections must keep page order: first %q, last %q", all[0].Title, all[11].Title)
	}
	// limit is the surfaces' Truncate on top of the budget: it caps the
	// count and keeps the best-ranked sections.
	limited := Truncate(all, 2)
	if len(limited) != 2 || limited[0].URL != all[0].URL || limited[1].URL != all[1].URL {
		t.Errorf("limit 2 = %+v", limited)
	}
}

func TestWebTrimSnippetClosesFence(t *testing.T) {
	text := "Intro line.\n\n" + fence + "go\n" + strings.Repeat("fmt.Println(\"x\")\n", 20) + fence
	got := trimSnippet(text, 120)
	if len(got) > 120 {
		t.Fatalf("trimmed to %d bytes, budget 120", len(got))
	}
	if strings.Count(got, fence) != 2 || !strings.HasSuffix(got, fence+"\n[truncated]") {
		t.Errorf("trim must close the open fence:\n%s", got)
	}
	if trimSnippet(text, 5) != "" {
		t.Error("a budget smaller than the marker leaves nothing")
	}
	ranked := []scoredSection{{webSection: webSection{heading: "Big", crumbs: []string{"Big"}, text: text, pageURL: "https://x.test/"}}}
	if res := budgetResults("https://x.test/", ranked, 10); len(res) != 1 || len(res[0].Snippet) > 40 {
		t.Errorf("an oversized first section is trimmed, not dropped: %+v", res)
	}
}

// A huge token budget means "no bound", not an overflow: tokens times the
// bytes per token wrapped negative for tokens >= 2^61 and returned nothing.
func TestWebHugeTokenBudgetDoesNotOverflow(t *testing.T) {
	ranked := []scoredSection{{webSection: webSection{heading: "Widgets", crumbs: []string{"Widgets"}, text: "Widgets are configured in widget.yaml.", pageURL: "https://x.test/"}}}
	for _, tokens := range []int{1 << 61, math.MaxInt / webCharsPerToken, math.MaxInt / webCharsPerToken * 3, math.MaxInt} {
		res := budgetResults("https://x.test/", ranked, tokens)
		if len(res) != 1 || res[0].Snippet != ranked[0].text {
			t.Errorf("tokens %d: results %+v, want the one section whole", tokens, res)
		}
	}
	results, err := NewWeb(budgetSite()).GetDocs(context.Background(), "https://budget.test", "widget", math.MaxInt)
	if err != nil || len(results) != 12 {
		t.Errorf("GetDocs with tokens MaxInt = %d results, %v; want all 12 sections", len(results), err)
	}
}

func TestWebErrLibraryRequired(t *testing.T) {
	w := NewWeb(&fakeFetcher{})
	_, err := w.Search(context.Background(), "content collections", 5)
	if !errors.Is(err, ErrLibraryRequired) || !strings.Contains(err.Error(), "--library") || !strings.Contains(err.Error(), "library (MCP)") {
		t.Errorf("Search without a library: %v", err)
	}
	for _, id := range []string{"/withastro/docs", "astro", "ftp://docs.test/", "https://"} {
		if _, err := w.GetDocs(context.Background(), id, "q", 100); !errors.Is(err, ErrLibraryRequired) {
			t.Errorf("GetDocs(%q) = %v, want ErrLibraryRequired", id, err)
		}
	}
	_, err = w.ResolveLibrary(context.Background(), "astro", 5)
	if !errors.Is(err, ErrLibraryRequired) || strings.Contains(strings.ToLower(err.Error()), "phase") {
		t.Errorf("ResolveLibrary(name) = %v", err)
	}
}

func TestWebNothingDiscoverableIsErrNotFound(t *testing.T) {
	f := &fakeFetcher{}
	_, err := NewWeb(f).GetDocs(context.Background(), "https://empty.test/docs/", "anything", 100)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	for _, tried := range []string{"https://empty.test/docs/llms.txt", "https://empty.test/llms.txt", "robots.txt", "https://empty.test/docs/sitemap.xml", "https://empty.test/sitemap.xml", "links on https://empty.test/docs/"} {
		if !strings.Contains(err.Error(), tried) {
			t.Errorf("not-found message should name %q: %v", tried, err)
		}
	}
}

func TestWebNetworkFailureIsPlainError(t *testing.T) {
	f := &fakeFetcher{fail: errors.New("dial tcp: connection refused")}
	_, err := NewWeb(f).GetDocs(context.Background(), "https://down.test/", "anything", 100)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) {
		t.Fatalf("network failure must be a plain error, got %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should carry the cause: %v", err)
	}

	pages := mintlifySite()
	pages.raw = map[string]fakeRaw{"https://mint.test/llms.txt": pages.raw["https://mint.test/llms.txt"]}
	_, err = NewWeb(&failingPages{pages}).GetDocs(context.Background(), "https://mint.test", "collections", 100)
	if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "fetching docs pages") {
		t.Errorf("every page failing must be a plain error, got %v", err)
	}
}

// When every candidate page fails, the call is not-found only if every page
// is missing: one that was down makes the whole failure retryable, whichever
// order the candidates ranked in.
func TestWebAllPagesFailingClassification(t *testing.T) {
	site := func() *fakeFetcher {
		return &fakeFetcher{raw: map[string]fakeRaw{
			"https://hooks.test/llms.txt": plain("# Hooks\n\n- [Webhooks](/webhooks.md)\n- [Webhook retries](/webhook-retries.md)\n"),
		}}
	}
	_, err := NewWeb(site()).GetDocs(context.Background(), "https://hooks.test/", "webhooks", 100)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("every listed page 404ing = %v, want ErrNotFound", err)
	}
	for _, down := range []string{"https://hooks.test/webhooks.md", "https://hooks.test/webhook-retries.md"} {
		f := &downPages{fakeFetcher: site(), down: down}
		_, err := NewWeb(f).GetDocs(context.Background(), "https://hooks.test/", "webhooks", 100)
		if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "status 503") {
			t.Errorf("one page 404ing and %s down = %v, want the plain 503", down, err)
		}
	}
}

// downPages fails one URL with a 503; the fake 404s the rest it lacks.
type downPages struct {
	*fakeFetcher
	down string
}

func (f *downPages) FetchRaw(ctx context.Context, url string) (RawContent, error) {
	if url == f.down {
		return RawContent{}, fmt.Errorf("fake: %s: status 503", url)
	}
	return f.fakeFetcher.FetchRaw(ctx, url)
}

// failingPages serves discovery files but fails every page with a 503.
type failingPages struct{ *fakeFetcher }

func (f *failingPages) FetchRaw(ctx context.Context, url string) (RawContent, error) {
	if strings.HasSuffix(url, ".md") {
		return RawContent{}, fmt.Errorf("fake: %s: status 503", url)
	}
	return f.fakeFetcher.FetchRaw(ctx, url)
}

func TestWebTimeoutBoundsTheCall(t *testing.T) {
	w := NewWeb(&fakeFetcher{block: true})
	w.opTimeout = 50 * time.Millisecond
	start := time.Now()
	_, err := w.GetDocs(context.Background(), "https://slow.test/", "anything", 100)
	if time.Since(start) > 5*time.Second {
		t.Fatalf("GetDocs took %s; the operation timeout must bound it", time.Since(start))
	}
	if err == nil || !strings.Contains(err.Error(), "did not answer within") || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a slow site is a plain (upstream) error, not a cancellation: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewWeb(&fakeFetcher{block: true}).GetDocs(ctx, "https://slow.test/", "anything", 100); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled caller must see context.Canceled, got %v", err)
	}
}

func TestWebFetchesFewPagesConcurrentlyBounded(t *testing.T) {
	raw := map[string]fakeRaw{}
	var list strings.Builder
	list.WriteString("# Many\n")
	for i := range 10 {
		fmt.Fprintf(&list, "- [Widget guide %d](/widget-%d.md)\n", i, i)
		raw[fmt.Sprintf("https://many.test/widget-%d.md", i)] = md(fmt.Sprintf("# Widget guide %d\n\nAll about widget number %d.\n", i, i))
	}
	list.WriteString("- [Gadgets](/gadgets.md)\n")
	raw["https://many.test/llms.txt"] = plain(list.String())
	f := &fakeFetcher{raw: raw, delay: 20 * time.Millisecond}
	w := NewWeb(f)
	w.concurrency = 2
	if _, err := w.GetDocs(context.Background(), "https://many.test/", "widget", 4000); err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	pageFetches := 0
	for _, c := range f.called() {
		if strings.HasSuffix(c, ".md") {
			pageFetches++
		}
	}
	if pageFetches != webMaxFetch {
		t.Errorf("fetched %d pages, want the top %d candidates", pageFetches, webMaxFetch)
	}
	if peak := f.peak.Load(); peak > 2 {
		t.Errorf("%d fetches in flight, concurrency bound is 2", peak)
	}
	assertNeverFetched(t, f, "gadgets")
}

func TestWebResolveLibrary(t *testing.T) {
	matches, err := NewWeb(mintlifySite()).ResolveLibrary(context.Background(), "https://mint.test/", 5)
	if err != nil || len(matches) != 1 {
		t.Fatalf("ResolveLibrary = %+v, %v", matches, err)
	}
	if m := matches[0]; m.ID != "https://mint.test/" || m.Title != "Mint Docs" || m.Description != "Build things with Mint." {
		t.Errorf("llms.txt match = %+v", m)
	}
	matches, err = NewWeb(plainSite()).ResolveLibrary(context.Background(), "https://plain.test", 5)
	if err != nil || len(matches) != 1 || matches[0].Title != "Plain Docs" || matches[0].Description != "Docs for the Plain framework." {
		t.Errorf("root page match = %+v, %v", matches, err)
	}
	if _, err := NewWeb(&fakeFetcher{fail: errors.New("no route to host")}).ResolveLibrary(context.Background(), "https://down.test", 5); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("unreachable site must be a plain error, got %v", err)
	}
}

func TestWebProviderIsSideEffectFree(t *testing.T) {
	p, ok := Lookup("web")
	if !ok || p.Name != "Web" || p.Hidden || len(p.Settings) != 0 {
		t.Fatalf("web descriptor = %+v, %v", p, ok)
	}
	f := &fakeFetcher{}
	client, err := p.New(&config.Config{}, Deps{Fetcher: f})
	if err != nil {
		t.Fatal(err)
	}
	if calls := f.called(); len(calls) != 0 {
		t.Errorf("construction fetched %v", calls)
	}
	if _, ok := client.(LibraryResolver); !ok {
		t.Error("web must implement LibraryResolver")
	}
	zero, err := p.New(&config.Config{}, Deps{})
	if err != nil || zero.(*Web).fetcher == nil {
		t.Errorf("a zero Deps must yield the fallback fetcher: %v", err)
	}
	if !p.Usable(&config.Config{}) || p.Required(&config.Config{}) || !p.Required(&config.Config{DocsBackend: "web"}) {
		t.Error("web is always usable and required only when selected")
	}
	if status, _ := p.Probe(context.Background(), nil, &config.Config{}); status != health.StatusOK {
		t.Errorf("probe = %s, want ok without network", status)
	}
}

func TestWebRegisteredAfterContext7(t *testing.T) {
	libs := LibraryBackends()
	if len(libs) == 0 || libs[0] != "context7" || !slices.Contains(libs, "web") || ResolveBackend("") != "context7" {
		t.Errorf("library backends = %v; context7 must stay first", libs)
	}
	if !slices.Contains(AvailableBackends(), "web") || !slices.Contains(ProviderNames(), "Web") {
		t.Error("web must be listed")
	}
}

// An Astro-style site: the root is a meta-refresh stub, and the only
// sitemap is sitemap-index.xml listing every page in every locale.
func astroSite() *fakeFetcher {
	return &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://astro.test/robots.txt": plain("User-agent: *\nAllow: /\n"),
			"https://astro.test/": html(`<!DOCTYPE html>
<meta http-equiv="refresh" content="0;url=/en/getting-started/">`),
			"https://astro.test/en/getting-started/": html(`<html><head><title>Getting started | Astro Docs</title></head><body>
<a href="/en/guides/content-collections/">Content collections</a><a href="/en/guides/routing/">Routing</a></body></html>`),
		},
		pages: map[string]fakePage{
			"https://astro.test/en/getting-started/":            {title: "Getting started", markdown: "# Getting started\n\nInstall Astro and create your first project.\n"},
			"https://astro.test/en/guides/content-collections/": {title: "Content collections", markdown: "# Content collections\n\nContent collections manage sets of content in an Astro project.\n"},
		},
	}
}

func TestWebFollowsRootMetaRefresh(t *testing.T) {
	f := astroSite()
	results, err := NewWeb(f).GetDocs(context.Background(), "https://astro.test", "content collections", 1000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) == 0 || results[0].URL != "https://astro.test/en/guides/content-collections/" {
		t.Fatalf("results = %+v", results)
	}
	matches, err := NewWeb(astroSite()).ResolveLibrary(context.Background(), "https://astro.test", 1)
	if err != nil || matches[0].Title != "Getting started" {
		t.Errorf("resolve through the refresh stub = %+v, %v", matches, err)
	}
}

func TestWebSitemapIndexDefaultAndVariants(t *testing.T) {
	var urls strings.Builder
	for _, p := range []string{"de/guides/routing/", "en/guides/routing/", "es/guides/routing/", "en/guides/content-collections/", "ja/guides/content-collections/"} {
		fmt.Fprintf(&urls, "<url><loc>https://astro.test/%s</loc><image:image><image:loc>https://astro.test/img/%s.png</image:loc></image:image></url>\n", p, strings.Trim(p, "/"))
	}
	f := &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://astro.test/sitemap-index.xml": xmlDoc(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>https://astro.test/sitemap-0.xml</loc></sitemap></sitemapindex>`),
			// A truncated tail must not lose the entries before it.
			"https://astro.test/sitemap-0.xml": xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">` + urls.String() + `<url><loc>https://astro.test/en/trunc`),
		},
		pages: map[string]fakePage{
			"https://astro.test/en/guides/routing/": {title: "Routing", markdown: "# Routing\n\nAstro uses file-based routing for pages.\n"},
		},
	}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://astro.test/", "routing", 1000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://astro.test/en/guides/routing/" {
		t.Fatalf("results = %+v", results)
	}
	var pages []string
	for _, c := range f.called() {
		if strings.HasPrefix(c, "page ") {
			pages = append(pages, c)
		}
	}
	if !slices.Equal(pages, []string{"page https://astro.test/en/guides/routing/"}) {
		t.Errorf("locale variants must collapse to the English page, fetched %v", pages)
	}
	assertNeverFetched(t, f, ".png")
}

func TestWebCollapseVersionVariants(t *testing.T) {
	root, _ := parseWebRoot("https://docu.test/docs/")
	var links []webLink
	for _, p := range []string{"2.x/sidebar/items", "3.0.1/sidebar/items", "sidebar/items", "next/sidebar/items", "fr/sidebar/items", "v2/sidebar/items"} {
		u, _ := url.Parse("https://docu.test/docs/" + p)
		links = append(links, webLink{url: u, title: "Sidebar items"})
	}
	got := pickCandidates(links, "sidebar items", root, 6)
	if len(got) != 1 || got[0].url.Path != "/docs/sidebar/items" {
		t.Errorf("version and locale variants should collapse to the current page, got %d: %v", len(got), got)
	}
}

func TestWebUnattributedFullTextPrefersPages(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://hono.test/llms.txt":                plain("# Hono\n\n- [CORS](https://hono.test/docs/middleware/cors.md)\n- [Basic Auth](https://hono.test/docs/middleware/basic-auth.md)\n"),
		"https://hono.test/llms-full.txt":           plain("# CORS Middleware\n\nCORS middleware sets the CORS headers.\n\n# Basic Auth Middleware\n\nBasic auth middleware checks credentials.\n"),
		"https://hono.test/docs/middleware/cors.md": md("# CORS Middleware\n\nThe cors middleware sets CORS headers for every route.\n"),
	}}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://hono.test", "cors middleware", 1000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) == 0 || results[0].URL != "https://hono.test/docs/middleware/cors" {
		t.Errorf("results should link to pages, not llms-full.txt: %+v", results)
	}
}

func TestWebEmptyRootIsErrNotFound(t *testing.T) {
	f := &fakeFetcher{
		raw:   map[string]fakeRaw{"https://blank.test/": html("<!doctype html><html><body><a href=\"https://elsewhere.test/\">away</a></body></html>")},
		pages: map[string]fakePage{"https://blank.test/": {}},
	}
	if _, err := NewWeb(f).GetDocs(context.Background(), "https://blank.test/", "anything", 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("an empty root with no in-scope links is not-found, got %v", err)
	}
}

// A result from a Markdown source whose rendered page cannot be read
// keeps an anchor the Markdown fixed ({#id}) and cites the page for a
// slug no one checked.
func TestWebUncheckedMarkdownCitesExplicitIDsOnly(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://md.test/llms.txt": plain("# MD\n\n- [Widgets](/widgets.md)\n"),
		"https://md.test/widgets.md": md("# Widgets\n\nAll about widgets here.\n\n## Install widgets {#setup}\n\nInstall widgets with the widget tool.\n\n" +
			"## Use widgets\n\nUse widgets by calling the widget function.\n"),
	}}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://md.test", "widgets", 4000)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range results {
		got[r.Title] = r.URL
	}
	if got["Install widgets"] != "https://md.test/widgets#setup" || got["Use widgets"] != "https://md.test/widgets" {
		t.Errorf("citations = %v", got)
	}
}

// One long section (a reference listing) takes at most half the budget,
// leaving room for the sections ranked after it.
func TestWebBudgetCapsOneSection(t *testing.T) {
	ranked := []scoredSection{
		{webSection: webSection{pageURL: "https://x.test/api", heading: "Validators", text: strings.Repeat("validator entry text\n", 2000)}, score: 3},
		{webSection: webSection{pageURL: "https://x.test/guide", heading: "Validators", text: "Validators check values when an instance is created."}, score: 2},
	}
	res := budgetResults("https://x.test/", ranked, 1000)
	if len(res) != 2 || len(res[0].Snippet) > 2000 || !strings.HasSuffix(res[0].Snippet, "[truncated]") {
		t.Fatalf("results = %d, first %d bytes; want the listing cut to half the budget and the guide after it", len(res), len(res[0].Snippet))
	}
}

// A site whose page list names none of the query (opaque paths, no
// titles) has its candidates found by a web search instead: the hits under
// the root are fetched first, at the root's spelling of its host; those
// elsewhere, or in another release's docs, never.
func TestWebWeakCandidatesAskTheSearch(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://opaque.test/docs/sitemap.xml": xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url><loc>https://opaque.test/docs/intro</loc></url>
<url><loc>https://opaque.test/docs/api/class-page</loc></url>
<url><loc>https://opaque.test/docs/api/class-frame</loc></url>
</urlset>`),
	}, pages: map[string]fakePage{
		"https://opaque.test/docs/intro":          {title: "Intro", markdown: "# Intro\n\nWelcome to the opaque docs site for testing.\n"},
		"https://opaque.test/docs/api/class-page": {title: "Page", markdown: "# Page\n\n## page.waitForLoadState(state)\n\nReturns when the required load state has been reached.\n"},
	}}
	w := NewWeb(f)
	var asked []string
	w.sites = siteSearchFunc(func(_ context.Context, q string, _ int) ([]search.Result, error) {
		asked = append(asked, q)
		return []search.Result{{URL: "https://elsewhere.test/blog/load-state"}, {URL: "https://opaque.test/docs/1.3.0/loading"},
			{URL: "https://www.opaque.test/docs/api/class-page#page-wait-for-load-state", Title: "Page"}}, nil
	})
	results, err := w.GetDocs(context.Background(), "https://opaque.test/docs/", "wait for load state", 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "opaque.test/docs/ wait for load state" {
		t.Errorf("searched %q", asked)
	}
	if len(results) == 0 || results[0].Title != "page.waitForLoadState(state)" {
		t.Fatalf("results = %+v", results)
	}
	// Another release's page is the wrong version; the root's own host
	// spelling is the one fetched.
	assertNeverFetched(t, f, "elsewhere.test", "1.3.0", "www.opaque.test")
}
