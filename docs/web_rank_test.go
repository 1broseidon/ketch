package docs

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// The cutoff is relative to the best section: what scores well below it
// is left out, however many sections that leaves.
func TestWebRelevantCutsFarBelowTheTop(t *testing.T) {
	ranked := []scoredSection{{score: 1.18}, {score: 1.12}, {score: 0.74}, {score: 0.027}, {score: 0.02}}
	if got := relevant(ranked); len(got) != 3 {
		t.Errorf("kept %d sections, want the 3 within reach of the top", len(got))
	}
	if got := relevant(nil); len(got) != 0 {
		t.Errorf("relevant(nil) = %v", got)
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

// Consecutive query terms found together count for more than the same
// terms scattered through a section.
func TestWebPhraseAdjacency(t *testing.T) {
	sections := []webSection{
		{heading: "Strategies", text: "There are a couple of ways to share state in Tokio: guard it with a mutex."},
		{heading: "Holding a guard", text: "State is not Send. You may share the guard with care, but state across tasks is hard."},
	}
	terms := queryTerms("share state")
	c := newSectionCorpus(sections, terms)
	if a, b := phraseShare(c.seq[0], terms), phraseShare(c.seq[1], terms); a != 1 || b != 0 {
		t.Errorf("phrases = %v, %v; want 1 for the adjacent pair, 0 for the scattered one", a, b)
	}
}

// A run of near-equal siblings gives way to a section from elsewhere that
// scores almost as well; a clearly better sibling keeps its place.
func TestWebDiversifySiblings(t *testing.T) {
	fn := func(name string, score float64) scoredSection {
		return scoredSection{webSection: webSection{pageURL: "https://x.test/require", heading: name, crumbs: []string{"require", "Functions", name}}, score: score}
	}
	overview := scoredSection{webSection: webSection{pageURL: "https://x.test/require", heading: "Overview", crumbs: []string{"require", "Overview"}}, score: 1.49}
	got := diversify([]scoredSection{fn("Panics", 1.59), fn("Contains", 1.58), fn("Greater", 1.55), fn("Less", 1.55), overview})
	var order []string
	for _, s := range got {
		order = append(order, s.heading)
	}
	if want := []string{"Panics", "Contains", "Overview", "Greater", "Less"}; !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
	clear := diversify([]scoredSection{fn("A", 3), fn("B", 3), fn("C", 3), {webSection: webSection{pageURL: "https://x.test/other", heading: "Other"}, score: 1}})
	if clear[2].heading != "C" {
		t.Errorf("a clearly better sibling lost its place: %v", clear[2].heading)
	}
}

// The same section at two addresses is one result, at the first.
func TestWebDedupeRepeatedSections(t *testing.T) {
	a := scoredSection{webSection: webSection{pageURL: "https://docs.rs/clap/struct.Arg.html", heading: "pub fn default_value", text: "Value for the argument\nwhen not present."}, score: 3}
	b := a
	b.pageURL, b.text = "https://docs.rs/clap/builder/struct.Arg.html", "Value for the argument when not present."
	c := scoredSection{webSection: webSection{pageURL: "https://docs.rs/clap/struct.Arg.html", heading: "pub fn required", text: "Sets whether the argument is required."}, score: 2}
	got := dedupe([]scoredSection{a, b, c})
	if len(got) != 2 || got[0].pageURL != a.pageURL || got[1].heading != "pub fn required" {
		t.Errorf("dedupe = %+v", got)
	}
}

// A section cut to fit keeps its lead and its blocks that name the query,
// whole and in order, rather than its first bytes: a reference entry's
// example comes after its parameter list.
func TestWebTrimForQueryKeepsMatchingBlocks(t *testing.T) {
	params := strings.Repeat("A parameter that has nothing to do with it, described at length.\n", 6)
	text := "Display a gauge widget.\n\n" + params + "\n" + params + "\n**Example**\n\n" + fence + "py\ng = gauge(\"Load\", maximum=100)\n\n" + "g.render()\n" + fence + "\n\nBuilt with the docs tool."
	terms := queryTerms("gauge maximum")
	got := trimForQuery(text, 400, terms)
	if len(got) > 400 || !strings.HasSuffix(got, truncatedMarker) {
		t.Fatalf("trimmed to %d bytes, want <= 400 and marked:\n%s", len(got), got)
	}
	if !strings.HasPrefix(got, "Display a gauge widget.") || !strings.Contains(got, "g = gauge(\"Load\", maximum=100)\n\ng.render()\n"+fence) {
		t.Errorf("want the lead and the whole example:\n%s", got)
	}
	if strings.Contains(got, "nothing to do with it") || !strings.Contains(got, elidedMarker) {
		t.Errorf("want the unmatched blocks left out, marked:\n%s", got)
	}
	if got := trimForQuery(text, 400, nil); got != trimSnippet(text, 400) {
		t.Errorf("without terms it is a prefix cut:\n%s", got)
	}
	if got := trimForQuery(text, 40, terms); got != trimSnippet(text, 40) {
		t.Errorf("a room too small for the lead is a prefix cut:\n%s", got)
	}
}
