package docs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func coverageFixture(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "coverage", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fetchedPages lists the pages f rendered, in order.
func fetchedPages(f *fakeFetcher) []string {
	var out []string
	for _, c := range f.called() {
		if p, ok := strings.CutPrefix(c, "page "); ok {
			out = append(out, p)
		}
	}
	return out
}

// A sitemap names pages by path alone; the root page's sidebar titles
// them. Sequelize's associations page lives at /core-concepts/assocs/, a
// path that names none of "define a model association hasMany", and six
// model pages outscore it on paths; its sidebar title, Associations, is
// what finds it. Trimmed from the live Docusaurus site.
func TestWebSitemapTitledByRootNav(t *testing.T) {
	f := &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://sequelize.org/sitemap.xml": xmlDoc(strings.TrimPrefix(coverageFixture(t, "sequelize.org/sitemap.xml"), `<?xml version="1.0" encoding="UTF-8"?>`)),
			"https://sequelize.org/docs/v6/":    html(coverageFixture(t, "sequelize.org/docs-v6.html")),
		},
		pages: map[string]fakePage{
			"https://sequelize.org/docs/v6/core-concepts/assocs/":       {title: "Associations | Sequelize", markdown: "# Associations\n\nSequelize supports the standard associations.\n\n## The `hasMany` association\n\n```js\nTeam.hasMany(Player);\n```\n\nDefine a one-to-many association between models with hasMany.\n"},
			"https://sequelize.org/docs/v6/core-concepts/model-basics/": {title: "Model Basics", markdown: "# Model Basics\n\nModels can be defined in two equivalent ways: sequelize.define and extending Model.\n"},
		},
	}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://sequelize.org/docs/v6/", "define a model association hasMany", 2000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if !slices.Contains(fetchedPages(f), "https://sequelize.org/docs/v6/core-concepts/assocs/") {
		t.Fatalf("the associations page was never fetched: %v", fetchedPages(f))
	}
	if len(results) == 0 || !strings.HasPrefix(results[0].URL, "https://sequelize.org/docs/v6/core-concepts/assocs/") {
		t.Errorf("first result = %+v", results)
	}
}

// An MkDocs site's search index finds the page a section answers on: the
// sitemap and nav name "Usage", whose "Cross-references" section is what
// "cross references" asks for, while the API reference's path merely
// shares a word. Trimmed from the live mkdocstrings site.
func TestWebMkDocsSearchIndexFindsTheSection(t *testing.T) {
	base := "https://mkdocstrings.github.io/mkdocstrings/"
	f := &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://mkdocstrings.github.io/sitemap.xml": xmlDoc(strings.TrimPrefix(coverageFixture(t, "mkdocstrings.github.io/sitemap.xml"), `<?xml version="1.0" encoding="UTF-8"?>`)),
			base:                              html(coverageFixture(t, "mkdocstrings.github.io/index.html")),
			base + "search/search_index.json": {body: coverageFixture(t, "mkdocstrings.github.io/search_index.json"), contentType: "application/json"},
		},
		pages: map[string]fakePage{
			base + "usage/":         {title: "Usage - mkdocstrings", markdown: "# Usage\n\n## Cross-references\n\nCross-references are written as Markdown reference-style links: `[object][full.path.object]`.\n"},
			base + "reference/api/": {title: "API reference - mkdocstrings", markdown: "# API reference\n\n## Handlers\n\nA collection of handlers.\n"},
		},
	}
	// The page list alone does not lead to the usage page; the index does.
	root, _ := parseWebRoot(base)
	site, err := NewWeb(f).discover(context.Background(), root)
	if err != nil || len(site.index) == 0 {
		t.Fatalf("discover: %v, %d index sections", err, len(site.index))
	}
	if slices.ContainsFunc(pickCandidates(site.links, "cross references", root, 6), func(l webLink) bool { return l.url.String() == base+"usage/" }) {
		t.Fatal("the fixture no longer needs the index: the links alone pick the usage page")
	}
	if got := indexPages(site.index, "cross references", 6); len(got) == 0 || got[0].url.String() != base+"usage/" {
		t.Fatalf("indexPages = %+v", got)
	}
	f.calls = nil
	results, err := NewWeb(f).GetDocs(context.Background(), base, "cross references", 2000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if got := fetchedPages(f); !slices.Contains(got, base+"usage/") {
		t.Fatalf("the usage page was never fetched: %v", got)
	}
	if len(results) == 0 || !strings.HasPrefix(results[0].URL, base+"usage/") {
		t.Errorf("first result = %+v", results)
	}
}

// An mdBook's sidebar is drawn by a script, so its root page links almost
// nothing; its searchindex-<hash>.js (JSON in a JS string) names every
// section. Trimmed from the live wasm-bindgen guide.
func TestWebMdBookSearchIndexFindsThePage(t *testing.T) {
	base := "https://wasm-bindgen.github.io/wasm-bindgen/"
	f := &fakeFetcher{
		raw: map[string]fakeRaw{
			base:                             html(coverageFixture(t, "wasm-bindgen.github.io/index.html")),
			base + "searchindex-eaa72cb4.js": {body: coverageFixture(t, "wasm-bindgen.github.io/searchindex-eaa72cb4.js"), contentType: "application/javascript"},
		},
		pages: map[string]fakePage{
			base + "examples/import-js.html": {title: "Importing non-browser JS", markdown: "# Importing non-browser JS\n\nThe `#[wasm_bindgen]` attribute can import JavaScript functions from a module:\n\n```rust\n#[wasm_bindgen(module = \"/defined-in-js.js\")]\nextern \"C\" { fn name() -> String; }\n```\n"},
		},
	}
	if _, err := NewWeb(f).GetDocs(context.Background(), base, "import javascript functions", 2000); err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if got := fetchedPages(f); !slices.Contains(got, base+"examples/import-js.html") {
		t.Errorf("the import page was never fetched: %v", got)
	}
}

func TestParseSiteIndexFormats(t *testing.T) {
	root, _ := parseWebRoot("https://mkdocstrings.github.io/mkdocstrings/")
	from, _ := root.u.Parse("search/search_index.json")
	secs := parseSiteIndex([]byte(coverageFixture(t, "mkdocstrings.github.io/search_index.json")), from, root)
	i := slices.IndexFunc(secs, func(s webSection) bool { return s.anchor == "cross-references" })
	if i < 0 || secs[i].pageURL != "https://mkdocstrings.github.io/mkdocstrings/usage/" || secs[i].crumbs[0] != "Usage" || strings.HasPrefix(secs[i].text, "<") {
		t.Errorf("MkDocs sections = %+v", secs)
	}
	root, _ = parseWebRoot("https://wasm-bindgen.github.io/wasm-bindgen/")
	from, _ = root.u.Parse("searchindex-eaa72cb4.js")
	secs = parseSiteIndex([]byte(coverageFixture(t, "wasm-bindgen.github.io/searchindex-eaa72cb4.js")), from, root)
	i = slices.IndexFunc(secs, func(s webSection) bool { return strings.HasSuffix(s.pageURL, "examples/import-js.html") })
	if len(secs) != 10 || i < 0 || secs[i].crumbs[0] == "" {
		t.Errorf("mdBook sections = %d: %+v", len(secs), secs)
	}
	if got := parseSiteIndex([]byte("not an index"), from, root); got != nil {
		t.Errorf("garbage parsed as an index: %+v", got)
	}
}

func TestInterleaveLinks(t *testing.T) {
	mk := func(paths ...string) []webLink {
		var out []webLink
		for _, p := range paths {
			l := mustLink(t, "https://x.test/"+p)
			out = append(out, l)
		}
		return out
	}
	got := interleaveLinks(mk("a", "b", "c"), mk("b", "d"), 4)
	var paths []string
	for _, l := range got {
		paths = append(paths, l.url.Path)
	}
	if want := []string{"/a", "/b", "/d", "/c"}; !slices.Equal(paths, want) {
		t.Errorf("interleaveLinks = %v, want %v", paths, want)
	}
}

func mustLink(t *testing.T, raw string) webLink {
	t.Helper()
	u, ok := resolveRef(nil, raw)
	if !ok {
		t.Fatalf("bad URL %q", raw)
	}
	return webLink{url: u}
}

// A hand-written llms.txt lists a few pages; when they name little of the
// query, the sitemap, nav and search index the site also publishes are
// read too. Shaped like docs.astral.sh/ruff (MkDocs Material, a short
// curated llms.txt).
func TestWebCuratedLLMSTxtWidensToTheSite(t *testing.T) {
	base := "https://docs.astral.sh/ruff/"
	index := `{"config":{},"docs":[
 {"location":"","title":"Ruff","text":"<p>An extremely fast Python linter and code formatter.</p>"},
 {"location":"configuration/","title":"Configuring Ruff","text":"<p>Ruff can be configured through a pyproject.toml file.</p>"},
 {"location":"linter/","title":"The Ruff Linter","text":"<p>The Ruff Linter is an extremely fast Python linter.</p>"},
 {"location":"linter/#error-suppression","title":"Error suppression","text":"<p>To ignore an individual violation, add <code># noqa: {code}</code> to the end of the line.</p>"},
 {"location":"versioning/","title":"Versioning","text":"<p>Ruff uses a custom versioning scheme.</p>"}]}`
	f := &fakeFetcher{
		raw: map[string]fakeRaw{
			base + "llms.txt": plain("# Ruff\n\n> An extremely fast Python linter.\n\n- [Configuring Ruff](https://docs.astral.sh/ruff/configuration/index.md)\n- [Versioning](https://docs.astral.sh/ruff/versioning/index.md)\n"),
			"https://docs.astral.sh/ruff/configuration/index.md": md("# Configuring Ruff\n\nRuff can be configured through a pyproject.toml file.\n"),
			"https://docs.astral.sh/ruff/versioning/index.md":    md("# Versioning\n\nRuff uses a custom versioning scheme.\n"),
			base + "sitemap.xml":              xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://docs.astral.sh/ruff/</loc></url><url><loc>https://docs.astral.sh/ruff/configuration/</loc></url><url><loc>https://docs.astral.sh/ruff/linter/</loc></url><url><loc>https://docs.astral.sh/ruff/versioning/</loc></url></urlset>`),
			base:                              html(`<html><head><meta name="generator" content="mkdocs-1.6.1, mkdocs-material-9.6.14"><title>Ruff</title></head><body><nav><a href="configuration/">Configuring Ruff</a><a href="linter/">Linter</a></nav><script id="__config" type="application/json">{"base": "."}</script></body></html>`),
			base + "search/search_index.json": {body: index, contentType: "application/json"},
		},
		pages: map[string]fakePage{
			base + "linter/": {title: "The Ruff Linter", markdown: "# The Ruff Linter\n\n## Error suppression\n\nTo ignore an individual violation, add `# noqa: {code}` to the end of the line:\n\n```python\nx = 1  # noqa: F841\n```\n"},
		},
	}
	results, err := NewWeb(f).GetDocs(context.Background(), base, "ignore a rule for one line", 2000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if !slices.Contains(fetchedPages(f), base+"linter/") {
		t.Fatalf("the linter page was never fetched: %v", f.called())
	}
	if len(results) == 0 || !strings.HasPrefix(results[0].URL, base+"linter/") {
		t.Errorf("first result = %+v", results)
	}
}
