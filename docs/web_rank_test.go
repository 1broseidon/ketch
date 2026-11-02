package docs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// normTerm meets a query and a page that name one thing in different
// forms, and leaves short words and non-words alone.
func TestWebNormTerm(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"memoize", "memo"},
		{"memoized", "memo"},
		{"memoization", "memo"},
		{"loading", "load"},
		{"loaded", "load"},
		{"mapping", "map"},
		{"caching", "cache"},
		{"configured", "configure"},
		{"blocking", "block"},
		{"customizing", "custom"},
		{"spawned", "spawn"},
		{"handling", "handle"},
		{"resizing", "resize"},
	} {
		if normTerm(stem(tc.a)) != normTerm(stem(tc.b)) {
			t.Errorf("%q → %q and %q → %q should meet", tc.a, normTerm(stem(tc.a)), tc.b, normTerm(stem(tc.b)))
		}
	}
	for _, word := range []string{"string", "based", "need", "speed", "install", "pass", "size", "v2", "über"} {
		if got := normTerm(word); got != word {
			t.Errorf("normTerm(%q) = %q, want it unchanged", word, got)
		}
	}
	for _, tc := range []struct{ a, b string }{{"model", "mode"}, {"mapping", "maple"}, {"static", "state"}} {
		if normTerm(stem(tc.a)) == normTerm(stem(tc.b)) {
			t.Errorf("%q and %q should stay apart (%q)", tc.a, tc.b, normTerm(stem(tc.a)))
		}
	}
}

// Astro's sitemap names "image" in about twice as many pages as
// "component" (its error reference has a page per image error). That is
// no reason to drop the images guide for component pages on "Image
// component": only a term several times as common is discounted.
func TestWebPickCandidatesNearEqualTerms(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "discover", "docs.astro.build", "sitemap-0-en.xml"))
	if err != nil {
		t.Fatal(err)
	}
	root, _ := parseWebRoot("https://docs.astro.build/")
	_, locs := parseSitemap(body, webMaxURLs)
	var links []webLink
	for _, loc := range locs {
		if u, ok := root.resolve(root.u, loc); ok {
			links = append(links, webLink{url: u})
		}
	}
	got := linkURLs(pickCandidates(links, "Image component", root, 6))
	if !slices.Contains(got, "https://docs.astro.build/en/guides/images/") {
		t.Errorf("candidates = %v, want the images guide among them", got)
	}
}
