package docs

import (
	"context"
	"errors"
	"hash/fnv"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
)

// bowEmbedder is a stand-in for the real model: a text's vector counts its
// (stemmed) words, hashed into dims buckets, so cosine is word overlap.
// It counts the passages it embeds.
type bowEmbedder struct {
	id       string
	passages atomic.Int32
}

const bowDims = 64

func (e *bowEmbedder) ID() string { return e.id }
func (e *bowEmbedder) Dim() int   { return bowDims }

func (e *bowEmbedder) EmbedQuery(_ context.Context, q string) ([]float32, error) {
	return bowVector(q), nil
}

func (e *bowEmbedder) EmbedPassages(_ context.Context, texts []string) ([][]float32, error) {
	e.passages.Add(int32(len(texts)))
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = bowVector(t)
	}
	return out, nil
}

func bowVector(s string) []float32 {
	v := make([]float32, bowDims)
	for _, t := range rankTokens(s) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(t))
		v[h.Sum32()%bowDims]++
	}
	var n float32
	for _, x := range v {
		n += x * x
	}
	if n > 0 {
		for i := range v {
			v[i] /= float32Sqrt(n)
		}
	}
	return v
}

func float32Sqrt(x float32) float32 {
	z := x
	for range 20 {
		z = (z + x/z) / 2
	}
	return z
}

const idxRoot = "https://docs.example.test/docs/"

// exampleSite is a docs site as the index sees it: a sitemap listing the
// pages, translated and old-release copies of some, a page robots.txt
// disallows, an asset and a page out of scope.
func exampleSite() *fakeFetcher {
	loc := func(paths ...string) string {
		var b strings.Builder
		for _, p := range paths {
			b.WriteString("<url><loc>https://docs.example.test" + p + "</loc></url>")
		}
		return b.String()
	}
	long := strings.Repeat("Middleware runs before the handler and can change the request. ", 30)
	return &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://docs.example.test/robots.txt": plain("User-agent: *\nDisallow: /docs/private/\n"),
			"https://docs.example.test/sitemap.xml": xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + loc(
				"/docs/", "/docs/guide/routing", "/docs/guide/middleware", "/docs/api/app", "/docs/api/v2/users",
				"/docs/es/guide/routing", "/docs/es/guide/middleware",
				"/docs/v1/guide/routing", "/docs/v1/guide/legacy",
				"/docs/private/secret", "/docs/logo.png", "/blog/post") + `</urlset>`),
		},
		pages: map[string]fakePage{
			idxRoot:                         {title: "Example", markdown: "# Example\n\nExample is a small web framework for building APIs.\n"},
			idxRoot + "guide/routing":       {title: "Routing", markdown: "# Routing\n\nDefine routes with app.get and app.post.\n\n## Path parameters\n\nCapture a path parameter with a colon: `/user/:id`, read it with c.req.param.\n"},
			idxRoot + "guide/middleware":    {title: "Middleware", markdown: "# Middleware\n\nMiddleware wraps handlers.\n\n## Add middleware to a route\n\nRegister middleware on a route with app.use before the handler.\n\n```ts\napp.use('/admin/*', auth())\n```\n\n## Writing middleware\n\n" + long + "\n\n```ts\nconst logger = async (c, next) => { await next() }\n```\n"},
			idxRoot + "api/app":             {title: "App API", markdown: "# App API\n\n## app.fetch\n\nThe fetch handler serves requests for a runtime.\n"},
			idxRoot + "api/v2/users":        {title: "Users endpoint", markdown: "# Users endpoint\n\nList users of the admin API with GET.\n"},
			idxRoot + "es/guide/routing":    {title: "Rutas", markdown: "# Rutas\n\nDefine rutas con app.get.\n"},
			idxRoot + "es/guide/middleware": {title: "Middleware (es)", markdown: "# Middleware\n\nEl middleware envuelve handlers.\n"},
			idxRoot + "v1/guide/routing":    {title: "Routing v1", markdown: "# Routing\n\nOld routing.\n\nMore old text about routes here.\n"},
			idxRoot + "v1/guide/legacy":     {title: "Legacy", markdown: "# Legacy\n\nA page only the old release has, about legacy adapters.\n"},
			idxRoot + "private/secret":      {title: "Secret", markdown: "# Secret\n\nNot for robots.\n"},
		},
	}
}

func buildIndex(t *testing.T, f *fakeFetcher, store *docsindex.Store, emb embed.Embedder) IndexReport {
	t.Helper()
	req := IndexRequest{Library: idxRoot, Embedder: emb}
	r, err := IndexLibrary(context.Background(), &config.Config{}, Deps{Fetcher: f}, store, req)
	if err != nil {
		t.Fatalf("IndexLibrary: %v", err)
	}
	return r
}

func storedURLs(t *testing.T, store *docsindex.Store) []string {
	t.Helper()
	pages, err := store.Pages(idxRoot)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for u := range pages {
		out = append(out, strings.TrimPrefix(u, idxRoot))
	}
	slices.Sort(out)
	return out
}

func newStore(t *testing.T) *docsindex.Store {
	return docsindex.Open(filepath.Join(t.TempDir(), "docs-index.db"))
}

// A build indexes the root's pages in its locale and version: other
// locales' and releases' copies are left out (and a release's tree whole,
// its unique pages too), but a path that only looks versioned stays;
// robots.txt, assets and out-of-scope pages are never fetched. Every
// section is embedded, a long one as several windows, each led by its
// heading path.
func TestIndexBuildKeepsOneVariantPerPage(t *testing.T) {
	f, store, emb := exampleSite(), newStore(t), &bowEmbedder{id: "test/bow"}
	r := buildIndex(t, f, store, emb)
	want := []string{"", "api/app", "api/v2/users", "guide/middleware", "guide/routing"}
	if got := storedURLs(t, store); !slices.Equal(got, want) {
		t.Fatalf("indexed pages = %v, want %v", got, want)
	}
	assertNeverFetched(t, f, "/private/", "/es/", "/v1/", "logo.png", "/blog/")
	lib := r.Library
	if lib.Root != idxRoot || lib.Model != "test/bow" || lib.Dims != bowDims || lib.Pages != 5 || lib.Resolved != "docs_url" || !slices.Contains(lib.Sources, "sitemap") {
		t.Errorf("library = %+v", lib)
	}
	if !slices.Contains(lib.Names, "example") {
		t.Errorf("names %v should include the host's distinctive label", lib.Names)
	}
	windows, units := checkWindows(t, store)
	if windows != lib.Vectors || units != lib.Units || units == 0 || int(emb.passages.Load()) != windows || r.Embedded != windows {
		t.Errorf("windows: stored %d, library %d; units: stored %d, library %d; embedded %d/%d", windows, lib.Vectors, units, lib.Units, emb.passages.Load(), r.Embedded)
	}
}

// The build records the index format and how many pages of other
// versions it left out (the v1 tree's two).
func TestIndexBuildCountsOtherVersions(t *testing.T) {
	r := buildIndex(t, exampleSite(), newStore(t), nil)
	if r.Library.OtherVersions != 2 || r.Library.Format != docsindex.FormatVersion || r.Library.Units == 0 {
		t.Errorf("library = %+v", r.Library)
	}
}

// checkWindows asserts every stored section has vectors, the long one
// several, and snippet units, which are not embedded, and returns how many
// windows and units there are.
func checkWindows(t *testing.T, store *docsindex.Store) (int, int) {
	t.Helper()
	secs, err := store.Sections(idxRoot, bowDims, true)
	if err != nil {
		t.Fatal(err)
	}
	windows, units := 0, 0
	for _, s := range secs {
		if len(s.Vectors) == 0 {
			t.Errorf("section %q has no vectors", s.Heading)
		}
		if len(s.Units) == 0 || len(s.UnitVectors) != 0 {
			t.Errorf("section %q has %d units and %d unit vectors", s.Heading, len(s.Units), len(s.UnitVectors))
		}
		windows += len(s.Vectors)
		units += len(s.Units)
		if s.Heading == "Writing middleware" && len(s.Vectors) < 2 {
			t.Errorf("a %d-byte section must be embedded as several windows, got %d", len(s.Text), len(s.Vectors))
		}
	}
	return windows, units
}

// Running a build again re-embeds only what changed: nothing when nothing
// did, one page when one page did; a page the sitemap dropped is removed.
func TestIndexRefreshEmbedsOnlyChangedPages(t *testing.T) {
	f, store, emb := exampleSite(), newStore(t), &bowEmbedder{id: "test/bow"}
	first := buildIndex(t, f, store, emb)
	r := buildIndex(t, f, store, emb)
	if r.Embedded != 0 || r.Unchanged != 5 || r.New+r.Changed+r.Removed != 0 || r.Library.Vectors != first.Library.Vectors {
		t.Errorf("unchanged re-run = %+v", r)
	}
	p := f.pages[idxRoot+"api/app"]
	p.markdown += "\n## app.request\n\nSend a test request to the app.\n"
	f.pages[idxRoot+"api/app"] = p
	sm := f.raw["https://docs.example.test/sitemap.xml"]
	sm.body = strings.Replace(sm.body, "<url><loc>https://docs.example.test/docs/api/v2/users</loc></url>", "", 1)
	f.raw["https://docs.example.test/sitemap.xml"] = sm
	r = buildIndex(t, f, store, emb)
	if r.Changed != 1 || r.Unchanged != 3 || r.Removed != 1 || r.Embedded != 2 { // two windows
		t.Errorf("one page changed, one dropped: %+v", r)
	}
	if got := storedURLs(t, store); slices.Contains(got, "api/v2/users") {
		t.Errorf("a page the site no longer lists stayed: %v", got)
	}
}

// A different model invalidates a library's vectors: the next build
// re-embeds every page, and Reembed does it from the stored text without
// fetching anything. Until then, the local backend ranks by keywords and
// says why.
func TestIndexModelChangeReembeds(t *testing.T) {
	f, store := exampleSite(), newStore(t)
	first := buildIndex(t, f, store, &bowEmbedder{id: "test/bow"})
	other := &bowEmbedder{id: "test/other"}
	l := NewLocal(store, func() (embed.Embedder, error) { return other, nil })
	o := l.GetDocsSelect(context.Background(), idxRoot, "add middleware to a route", 0)
	if o.Status != Answered || o.Reason != ReasonKeywordRanking || !strings.Contains(o.Note, "indexed with test/bow") {
		t.Errorf("a library embedded with another model = %s/%s %q", o.Status, o.Reason, o.Note)
	}
	r := buildIndex(t, f, store, other)
	passages := first.Library.Vectors
	if r.Changed != 5 || int(other.passages.Load()) != passages || r.Library.Model != "test/other" {
		t.Errorf("model change re-run = %+v (%d embedded)", r, other.passages.Load())
	}
	calls := len(f.called())
	third := &bowEmbedder{id: "test/third"}
	rr, err := Reembed(context.Background(), store, idxRoot, third, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.called()) != calls || rr.Library.Model != "test/third" || rr.Embedded != passages || rr.Library.Vectors != first.Library.Vectors {
		t.Errorf("reembed = %+v, %d fetches", rr, len(f.called())-calls)
	}
	if lib, _, _ := store.Library(idxRoot); lib.Model != "test/third" || lib.Vectors != first.Library.Vectors {
		t.Errorf("library after reembed = %+v", lib)
	}
}

// By default the local backend ranks a library's sections (cosine fused
// with BM25) and returns one result per family, best first: the section
// that matched under its address and heading path, its code inline, and
// the anchors of what it shows.
func TestLocalSectionResults(t *testing.T) {
	f, store, emb := exampleSite(), newStore(t), &bowEmbedder{id: "test/bow"}
	buildIndex(t, f, store, emb)
	l := NewLocal(store, func() (embed.Embedder, error) { return emb, nil })
	o := l.GetDocsSelect(context.Background(), "example", "add middleware to a route", 0)
	if o.Status != Answered || o.Backend != "local" || o.Reason != "" || len(o.Results) == 0 || len(o.Results) > localMaxResults {
		t.Fatalf("outcome = %+v", o)
	}
	checkTopSection(t, o.Results[0])
	seen := map[string]bool{}
	for _, r := range o.Results {
		if seen[r.URL] {
			t.Errorf("%s returned twice", r.URL)
		}
		seen[r.URL] = true
	}
	if total := snippetBytes(o.Results); total > localTokens*webCharsPerToken {
		t.Errorf("%d snippet bytes exceed the budget", total)
	}
}

func checkTopSection(t *testing.T, top Result) {
	t.Helper()
	if top.URL != idxRoot+"guide/middleware#add-middleware-to-a-route" || top.Source != "local" || top.Library != idxRoot ||
		top.Breadcrumb != "Middleware > Add middleware to a route" || top.Title != "Add middleware to a route" ||
		!slices.Equal(top.Anchors, []string{"add-middleware-to-a-route"}) {
		t.Errorf("top result = %+v", top)
	}
	if !strings.Contains(top.Snippet, "app.use('/admin/*', auth())") || strings.Contains(top.Snippet, "#### ") {
		t.Errorf("top snippet should be the section whole, its code inline:\n%s", top.Snippet)
	}
}

// A small budget trims every section result; a limit caps them.
func TestLocalSectionBudget(t *testing.T) {
	f, store, emb := exampleSite(), newStore(t), &bowEmbedder{id: "test/bow"}
	buildIndex(t, f, store, emb)
	l := NewLocal(store, func() (embed.Embedder, error) { return emb, nil })
	small := l.GetDocsSelect(WithLocalOptions(context.Background(), LocalOptions{Tokens: 100}), idxRoot, "writing middleware", 0)
	if total := snippetBytes(small.Results); total > 100*webCharsPerToken+len(small.Results)*webMinTrimBytes {
		t.Errorf("a 100-token budget returned %d bytes", total)
	}
	one := l.GetDocsSelect(WithLocalOptions(context.Background(), LocalOptions{Limit: 1}), idxRoot, "add middleware to a route", 0)
	if len(one.Results) != 1 {
		t.Errorf("limit 1 = %d results", len(one.Results))
	}
}

// Without a model the local backend ranks by keywords (BM25) and says so.
func TestLocalKeywordFallback(t *testing.T) {
	f, store := exampleSite(), newStore(t)
	buildIndex(t, f, store, &bowEmbedder{id: "test/bow"})
	l := NewLocal(store, func() (embed.Embedder, error) { return nil, embed.ErrNotInstalled })
	o := l.GetDocsSelect(context.Background(), idxRoot, "path parameter", 0)
	if o.Status != Answered || o.Reason != ReasonKeywordRanking || !strings.Contains(o.Note, "no embedding model installed") || o.Attempts[0].Reason != ReasonKeywordRanking {
		t.Fatalf("no model = %+v", o)
	}
	if o.Results[0].URL != idxRoot+"guide/routing#path-parameters" {
		t.Errorf("keyword top = %s", o.Results[0].URL)
	}
}

// A library the index does not hold, or a query nothing matches, is a
// refusal; a bare query answers from the indexed library it names.
func TestLocalRefusals(t *testing.T) {
	f, store := exampleSite(), newStore(t)
	buildIndex(t, f, store, &bowEmbedder{id: "test/bow"})
	l := NewLocal(store, func() (embed.Embedder, error) { return nil, embed.ErrNotInstalled })
	if o := l.GetDocsSelect(context.Background(), "express", "routing", 0); o.Status != Refused || o.Reason != "not_indexed" || !errors.Is(o.Err(), ErrNotIndexed) {
		t.Errorf("unindexed library = %s/%s", o.Status, o.Reason)
	}
	if o := l.GetDocsSelect(context.Background(), idxRoot, "zyzzyva quokka", 0); o.Status != Refused || o.Reason != "no_results" {
		t.Errorf("nothing matched = %s/%s", o.Status, o.Reason)
	}
	if o := l.SearchSelect(context.Background(), "how do I add middleware in express", 5); o.Reason != "not_indexed" {
		t.Errorf("a query naming an unindexed library = %s/%s", o.Status, o.Reason)
	}
	o := l.SearchSelect(context.Background(), "how do I add middleware in example", 1)
	if o.Status != Answered || len(o.Results) != 1 || !strings.HasPrefix(o.Results[0].URL, idxRoot+"guide/middleware") {
		t.Errorf("a query naming the indexed library = %s %d %v", o.Status, len(o.Results), o.Results)
	}
	empty := NewLocal(newStore(t), nil)
	if o := empty.SearchSelect(context.Background(), "anything in example", 5); o.Reason != "not_indexed" {
		t.Errorf("no index file = %s/%s", o.Status, o.Reason)
	}
}

// A library value names an indexed library by its root, a page under it,
// a name it was indexed by, or a spec with a version it holds.
func TestMatchIndexed(t *testing.T) {
	libs := []docsindex.Library{
		{Key: "https://hono.dev/docs/", Root: "https://hono.dev/docs/", Names: []string{"hono"}},
		{Key: "https://docs.example.test/v2/", Root: "https://docs.example.test/v2/", Names: []string{"widget"}, Version: "2.4", Packages: []string{"pypi:widget"}},
	}
	for value, want := range map[string]string{
		"https://hono.dev/docs/":                  "https://hono.dev/docs/",
		"https://hono.dev/docs/guides/middleware": "https://hono.dev/docs/",
		"https://hono.dev/":                       "https://hono.dev/docs/",
		"Hono":                                    "https://hono.dev/docs/",
		"widget@2":                                "https://docs.example.test/v2/",
		"pypi:widget":                             "https://docs.example.test/v2/",
		"widget@3":                                "",
		"npm:widget":                              "https://docs.example.test/v2/", // by name: indexed without npm's spec, a name still matches
		"express":                                 "",
	} {
		got := MatchIndexed(libs, value)
		switch {
		case want == "" && len(got) != 0:
			t.Errorf("%s matched %v", value, got[0].Key)
		case want != "" && (len(got) != 1 || got[0].Key != want):
			t.Errorf("%s = %v, want %s", value, got, want)
		}
	}
}

// robots.txt: the group for ketch, else for every agent; the longest
// match decides, Allow winning a tie; * and $ patterns.
func TestParseRobots(t *testing.T) {
	r := parseRobots([]byte("User-agent: other\nDisallow: /\n\nUser-agent: *\nDisallow: /docs/private/\nAllow: /docs/private/open\nDisallow: /*.pdf$\n"), "ketch")
	for p, want := range map[string]bool{
		"/docs/guide": true, "/docs/private/x": false, "/docs/private/open/page": true, "/docs/a.pdf": false, "/docs/a.pdf.html": true,
	} {
		u, _ := url.Parse("https://x.test" + p)
		if got := r.allowed(u); got != want {
			t.Errorf("%s allowed = %v, want %v", p, got, want)
		}
	}
	if own := parseRobots([]byte("User-agent: ketch\nDisallow: /a\n\nUser-agent: *\nDisallow: /\n"), "ketch"); !own.allowed(&url.URL{Path: "/b"}) {
		t.Error("ketch's own group must win over *")
	}
}

// Windows cover all of a section, at most indexWindowChars each, cut at
// block boundaries, a code block whole when it fits; each leads with the
// heading path.
func TestSectionWindows(t *testing.T) {
	para := strings.Repeat("word ", 150)                       // 750 bytes
	code := "```go\n" + strings.Repeat("x := 1\n", 60) + "```" // ~430 bytes
	w := sectionWindows([]string{"Page", "Part"}, para+"\n\n"+code+"\n\n"+para)
	if len(w) < 2 {
		t.Fatalf("windows = %d", len(w))
	}
	joined := strings.Join(w, "\n")
	if !strings.Contains(joined, code) {
		t.Error("a code block that fits a window must stay whole")
	}
	for _, x := range w {
		if !strings.HasPrefix(x, "Page > Part\n") || len(x) > indexWindowChars+len("Page > Part\n") {
			t.Errorf("window of %d bytes: %.40q", len(x), x)
		}
	}
	huge := sectionWindows([]string{"P"}, strings.Repeat("y", 5000))
	if len(huge) != 5 {
		t.Errorf("a 5000-byte line = %d windows", len(huge))
	}
}

// fairShares gives small items what they need and splits the rest.
func TestFairShares(t *testing.T) {
	got := fairShares([]int{100, 5000, 300, 5000}, 4000)
	if got[0] != 200 || got[2] != 300 || got[1] != got[3] || got[1] < 1700 {
		t.Errorf("shares = %v", got)
	}
}

// One variant per page: a page's other-locale and other-release copies go,
// and a whole old-release tree with them, a release channel's landing page
// right below the root too (stable/); but a page whose last segment only
// looks like a version (a tutorial's step 2, an upgrade guide to v5) is a
// page of its own.
func TestKeepOneVariant(t *testing.T) {
	root, _ := parseWebRoot("https://docs.example.test/en/")
	var links []webLink
	for _, p := range []string{
		"guides/routing/", "fr/guides/routing/", "v1/guides/routing/", "v1/guides/legacy/",
		"tutorial/1-setup/", "tutorial/1-setup/1/", "tutorial/1-setup/2/",
		"guides/upgrade-to/v4/", "guides/upgrade-to/v5/", "stable/",
	} {
		u, _ := url.Parse("https://docs.example.test/en/" + p)
		links = append(links, webLink{url: u})
	}
	var got []string
	kept, _, _ := keepOneVariant(links, root, nil)
	for _, l := range kept {
		got = append(got, strings.TrimPrefix(l.url.Path, "/en/"))
	}
	want := []string{"guides/routing/", "tutorial/1-setup/", "tutorial/1-setup/1/", "tutorial/1-setup/2/", "guides/upgrade-to/v4/", "guides/upgrade-to/v5/"}
	if !slices.Equal(got, want) {
		t.Errorf("kept %v\nwant %v", got, want)
	}
}

// A library's names come from what it was indexed by and its docs host,
// and --name aliases stay across builds that do not repeat them. The host
// label "example" is one word of the alias "Example Kit", so it is no
// longer a name of its own.
func TestIndexNamesKeepAliases(t *testing.T) {
	f, store := exampleSite(), newStore(t)
	req := IndexRequest{Library: idxRoot, Names: []string{"Example Kit"}}
	if _, err := IndexLibrary(context.Background(), &config.Config{}, Deps{Fetcher: f}, store, req); err != nil {
		t.Fatal(err)
	}
	r := buildIndex(t, f, store, nil)
	if !slices.Contains(r.Library.Names, "examplekit") || slices.Contains(r.Library.Names, "example") || !slices.Equal(r.Library.Aliases, []string{"Example Kit"}) {
		t.Errorf("names %v, aliases %v", r.Library.Names, r.Library.Aliases)
	}
	if lib, err := pickIndexed([]docsindex.Library{r.Library}, "example kit"); err != nil || lib.Root != idxRoot {
		t.Errorf("an alias names the library: %v", err)
	}
}
