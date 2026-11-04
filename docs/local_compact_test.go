package docs

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// The estimate counts what bytes miss: code and symbols cost more tokens
// a byte than prose. Fixed counts are o200k_base's.
func TestEstimateTokens(t *testing.T) {
	for _, c := range []struct {
		text string
		want int // o200k_base
	}{
		{"A param is denoted by a colon `:`. When a route is matched, the value of its params will be exposed as `route.params` in every component.", 34},
		{"```js\nconst routes = [\n  // dynamic segments start with a colon\n  { path: '/users/:id', component: User },\n]\n```", 31},
		{"| pattern | matched path |\n| --- | --- |\n| /users/:username | /users/eduardo |", 23},
		{"quantization_config = BitsAndBytesConfig(load_in_8bit=True)", 15},
	} {
		got := estimateTokens(c.text)
		if diff := float64(got-c.want) / float64(c.want); diff < -0.15 || diff > 0.15 {
			t.Errorf("estimateTokens(%q) = %d, o200k %d", c.text, got, c.want)
		}
	}
	if estimateTokens("") != 0 {
		t.Error("empty text costs tokens")
	}
}

// Sentences split at their ends, never inside inline code or after an
// abbreviation; labels (a tab's name) are not sentences.
func TestTextPieces(t *testing.T) {
	text := "Use `x.y()` for this, e.g. in a loop. It returns a value.\n\napp.ts\n\n```ts\nx.y()\n```\n\n- one item\n  continued\n- two\n\n| a | b |\n| - | - |"
	var got []string
	for _, p := range textPieces(text) {
		got = append(got, fmt.Sprintf("%d:%s", p.kind, p.text))
	}
	want := []string{
		"0:Use `x.y()` for this, e.g. in a loop.", "0:It returns a value.",
		"2:app.ts\n\n```ts\nx.y()\n```", "1:- one item\n  continued", "1:- two", "3:| a | b |\n| - | - |",
	}
	if !slices.Equal(got, want) {
		t.Errorf("pieces:\n got %q\nwant %q", got, want)
	}
}

// compactSections is a family too large for its share: an H2 with a
// long lead, a long prose section and a code example that names the
// question, behind a filler code block that does not.
func compactSections() []docsindex.Section {
	filler := strings.Repeat("Unrelated prose about the history of the project and its many contributors. ", 30)
	return []docsindex.Section{
		{Page: "https://x.test/forms", Order: 0, Anchor: "actions", Heading: "Actions", Crumbs: []string{"Forms", "Actions"},
			Text: "An action handles a form POST on the server. " + filler + "\n\n```js\nconsole.log('" + strings.Repeat("noise ", 120) + "')\n```"},
		{Page: "https://x.test/forms", Order: 1, Anchor: "default-actions", Heading: "Default actions", Crumbs: []string{"Forms", "Actions", "Default actions"},
			Text: "In the simplest case a page declares a default action. " + filler + "\n\nFor example:\n\n```js\nexport const actions = {\n  default: async ({ request }) => { /* handle the form POST */ }\n};\n```\n\n" + filler},
	}
}

// With a budget, a family that does not fit its share keeps its lead
// sentence, the code block naming the question (whole) with the sentence
// leading into it, and ends with one elision marker; the answer is within
// the budget by the estimate, and no fence is left open.
func TestCompactResults(t *testing.T) {
	secs := compactSections()
	ranked := []rankedSection{{sec: 1, score: 0.9}, {sec: 0, score: 0.5}}
	fams := families(secs, ranked, 5, "https://x.test/")
	results := familyResults(docsindex.Library{Root: "https://x.test/"}, secs, fams, "default form action POST", 300)
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	r := results[0]
	if !strings.Contains(r.Snippet, "An action handles a form POST on the server.") ||
		!strings.Contains(r.Snippet, "For example:\n\n```js\nexport const actions") ||
		strings.Contains(r.Snippet, "noise") || strings.Contains(r.Snippet, "history of the project") ||
		!strings.HasSuffix(r.Snippet, elidedMarker) || strings.Contains(r.Snippet, "[truncated]") {
		t.Errorf("snippet:\n%s", r.Snippet)
	}
	if strings.Count(r.Snippet, "```")%2 != 0 {
		t.Errorf("an open fence:\n%s", r.Snippet)
	}
	if got := estimateTokens(RenderSnippet(r)); got > 300 {
		t.Errorf("estimate %d over the budget", got)
	}
	// A generous budget shows the family whole, as the default does.
	whole := familyResults(docsindex.Library{Root: "https://x.test/"}, secs, fams, "default form action POST", 100000)
	if !strings.Contains(whole[0].Snippet, "noise") || strings.HasSuffix(whole[0].Snippet, elidedMarker) {
		t.Errorf("whole family cut:\n%.300s", whole[0].Snippet)
	}
}

// Shares fall with rank, a family smaller than its share takes its size
// and gives the rest to the others, and fewer results are shown when the
// budget cannot give each localMinShare.
func TestWeightedShares(t *testing.T) {
	cands := []*compactFamily{{wholeCost: 1000}, {wholeCost: 50}, {wholeCost: 1000}}
	shares := weightedShares(cands, 600)
	if shares[1] != 50 || shares[0] <= shares[2] || shares[0]+shares[1]+shares[2] > 600 {
		t.Errorf("shares = %v", shares)
	}
	many := make([]*compactFamily, 5)
	for i := range many {
		many[i] = &compactFamily{wholeCost: 1000}
	}
	if n, _ := compactShares(many, 850); n != 4 {
		t.Errorf("results at 850 = %d, want 4", n)
	}
	if n, _ := compactShares(many, 1200); n != 5 {
		t.Errorf("results at 1200 = %d, want 5", n)
	}
}
