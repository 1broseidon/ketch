package docs

import (
	"math"
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
		{"configuration", "configure"},
		{"conditionally", "condition"},
		{"optional", "option"},
		{"documentation", "documented"},
		{"params", "parameters"},
		{"config", "configuration"},
		{"args", "argument"},
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
	for _, tc := range []struct{ a, b string }{{"model", "mode"}, {"mapping", "maple"}, {"static", "state"}, {"station", "state"}, {"really", "real"}} {
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

func TestWebEntryMatch(t *testing.T) {
	for _, c := range []struct {
		heading, query string
		want           float64
	}{
		{"func (*Command) PersistentFlags", "persistent flags", 0.5},
		{"func (*Command) HasPersistentFlags", "persistent flags", 0},
		{"func (*Command) Flags", "persistent flags", 0},
		{"func (*SugaredLogger) Debugln", "sugared logger", 0},
		{"useEffect(setup, dependencies?)", "useEffect cleanup function", 1},
		{"root.render(reactNode)", "createRoot render", 0},
		{"extraReducers", "redux createSlice extraReducers", 1},
		{"res.sendFile(path [, options] [, fn])", "res.sendFile options", 1},
		{"Writing error handlers", "error handlers", 0},
		{"page.waitForLoadState(state, options)", "wait for load state", 0.5},
		{"class fastapi.UploadFile", "how to upload files", 0},
	} {
		if got := entryMatch(c.heading, queryTerms(c.query), queryIdents(c.query), isHowTo(c.query)); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("entryMatch(%q, %q) = %v, want %v", c.heading, c.query, got, c.want)
		}
	}
}

func TestWebCodeCoverage(t *testing.T) {
	text := "Use the middleware:\n\n```ts\nimport { bearerAuth } from 'hono/bearer-auth'\napp.use('/api/*', bearerAuth({ token }))\n```\n\nThe auth header is read."
	if got := codeCoverage(text, queryTerms("bearer auth")); got != 1 {
		t.Errorf("code coverage = %v, want 1", got)
	}
	if got := codeCoverage("The bearer token, in prose only.", queryTerms("bearer auth")); got != 0 {
		t.Errorf("prose coverage = %v, want 0", got)
	}
	if got := codeCoverage("```\ncurl -H 'Authorization: Bearer x'\n```\nauth", queryTerms("bearer auth")); got != 0.5 {
		t.Errorf("partial coverage = %v, want 0.5", got)
	}
}
