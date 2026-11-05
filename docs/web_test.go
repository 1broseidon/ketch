package docs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	config "github.com/1broseidon/ketch/internal/configbase"
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

func snippetBytes(results []Result) int {
	n := 0
	for _, r := range results {
		n += len(r.Snippet)
	}
	return n
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

// Search is the default docs backend and Context7 the other; the local
// index is part of search, and a docs_backend ketch 0.20 stored as
// "local" reads as search.
func TestSearchIsTheDefaultBackend(t *testing.T) {
	if got := AvailableBackends(); !slices.Equal(got, []string{SearchBackend, "context7"}) {
		t.Errorf("backends = %v, want search then context7", got)
	}
	if got := ProviderNames(); !slices.Equal(got, []string{"Search", "Context7"}) {
		t.Errorf("provider names = %v", got)
	}
	if libs := LibraryBackends(); !slices.Equal(libs, []string{SearchBackend, "context7"}) || ResolveBackend("") != SearchBackend {
		t.Errorf("library backends = %v; both resolve libraries, search first", libs)
	}
	for in, want := range map[string]string{"local": SearchBackend, SearchBackend: SearchBackend, "context7": "context7", "web": "web", "auto": "auto"} {
		if got := CanonicalBackend(in); got != want {
			t.Errorf("CanonicalBackend(%q) = %q, want %q", in, got, want)
		}
	}
	for _, gone := range []string{"web", "auto"} {
		if _, err := NewFromConfig(&config.Config{}, gone, Deps{}); !errors.Is(err, ErrUnknownBackend) || !strings.Contains(err.Error(), "ketch config set docs_backend search") {
			t.Errorf("%s: %v, want an unknown backend naming the fix", gone, err)
		}
	}
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
