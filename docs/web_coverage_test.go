package docs

import (
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
