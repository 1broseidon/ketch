package docs

import (
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// familySections are a page with an H2 that has its own text and two H3s,
// another H2, the page's lead, and a copy of the page at a second address.
func familySections() []docsindex.Section {
	page, twin := "https://x.test/forms", "https://x.test/forms.html"
	mk := func(p string, order int, anchor string, crumbs []string, text string) docsindex.Section {
		return docsindex.Section{Page: p, Order: order, Anchor: anchor, Heading: crumbs[len(crumbs)-1], Crumbs: crumbs, Text: text}
	}
	var out []docsindex.Section
	for _, p := range []string{page, twin} {
		out = append(out,
			mk(p, 0, "", []string{"Forms"}, "Forms post data to the server."),
			mk(p, 1, "actions", []string{"Forms", "Actions"}, "An action handles a form POST on the server."),
			mk(p, 2, "actions-default", []string{"Forms", "Actions", "Default actions"}, "A default action:\n\n```js\nexport const actions = { default: async () => {} }\n```"),
			mk(p, 3, "actions-named", []string{"Forms", "Actions", "Named actions"}, "Name several actions."),
			mk(p, 4, "loading", []string{"Forms", "Loading data"}, "After an action runs, the page reloads its data."),
		)
	}
	return out
}

// A section that ranks brings its family (the sections under its H2) as
// one result, the H2's own text first and its H3s as parts; a page copied
// at another address is one result; a page's lead is a family of its own.
func TestFamiliesGroupUnderTheirH2(t *testing.T) {
	secs := familySections()
	ranked := []rankedSection{{sec: 2, score: 0.9}, {sec: 7, score: 0.89}, {sec: 4, score: 0.5}, {sec: 0, score: 0.4}}
	fams := families(secs, ranked, 5, "")
	if len(fams) != 3 {
		t.Fatalf("families = %d, want 3 (the copy collapsed)", len(fams))
	}
	if fams[0].best != 2 || !slices.Equal(fams[0].members, []int{1, 2, 3}) {
		t.Errorf("first family = %+v", fams[0])
	}
	results := familyResults(docsindex.Library{Root: "https://x.test/"}, secs, fams, "default action form post", 0)
	top := results[0]
	if top.Title != "Actions" || top.Breadcrumb != "Forms > Actions" || top.URL != "https://x.test/forms#actions-default" ||
		!slices.Equal(top.Anchors, []string{"actions", "actions-default", "actions-named"}) {
		t.Errorf("top = %+v", top)
	}
	if !strings.HasPrefix(top.Snippet, "An action handles") || !strings.Contains(top.Snippet, "#### Default actions\n\nA default action:") {
		t.Errorf("top snippet:\n%s", top.Snippet)
	}
	if results[2].Title != "Forms" || !slices.Equal(results[2].Anchors, []string{""}) {
		t.Errorf("lead result = %+v", results[2])
	}
}

// At most localFamiliesPerPage families of one page lead while another
// page's rank next.
func TestFamiliesPerPage(t *testing.T) {
	var secs []docsindex.Section
	for i, h := range []string{"A", "B", "C"} {
		secs = append(secs, docsindex.Section{Page: "https://x.test/p", Order: i, Anchor: strings.ToLower(h), Heading: h, Crumbs: []string{"P", h}, Text: "text " + h})
	}
	secs = append(secs, docsindex.Section{Page: "https://x.test/q", Anchor: "d", Heading: "D", Crumbs: []string{"Q", "D"}, Text: "text D"})
	fams := families(secs, []rankedSection{{0, 4}, {1, 3}, {2, 2}, {3, 1}}, 5, "")
	var order []string
	for _, f := range fams {
		order = append(order, secs[f.best].Heading)
	}
	if !slices.Equal(order, []string{"A", "B", "D", "C"}) {
		t.Errorf("order = %v", order)
	}
}

// Fusion puts a section the keywords name exactly ahead of one only near
// by cosine, and leaves a later version's section out of the keyword
// ranking unless the question asks about it.
func TestFuseRanks(t *testing.T) {
	near, far := []float32{1, 0}, []float32{0.8, 0.6}
	secs := []docsindex.Section{
		{Page: "https://x.test/a", Heading: "Overview", Crumbs: []string{"Overview"}, Text: "General words about the library.", Vectors: [][]float32{near}},
		{Page: "https://x.test/b", Heading: "Retries", Crumbs: []string{"Retries"}, Text: "Set max_retries to retry failed requests.", Vectors: [][]float32{far}},
		{Page: "https://x.test/c", Heading: "Retries in v5", Crumbs: []string{"Migrating to v5", "Retries in v5"}, Text: "Retry failed requests with max_retries.", CrossVersion: "5", Vectors: [][]float32{far}},
	}
	got := fuseRanks(secs, []float32{1, 0}, "retry failed requests max_retries")
	if got[0].sec != 1 {
		t.Errorf("fused order = %+v", got)
	}
	if got[len(got)-1].sec != 2 {
		t.Errorf("the later version's section should rank last: %+v", got)
	}
	kw := fuseRanks(secs, nil, "retry failed requests max_retries")
	if len(kw) == 0 || kw[0].sec != 1 && kw[0].sec != 2 || kw[0].score != 1 {
		t.Errorf("keyword-only order = %+v", kw)
	}
}

// A trimmed section keeps its lead prose and its first code block (cut,
// fence closed, when it does not fit), then blocks naming the question.
func TestTrimSection(t *testing.T) {
	lead := "Register a handler for the event."
	code := "```js\n" + strings.Repeat("app.on('x', handler)\n", 20) + "```"
	filler := strings.Repeat("Unrelated detail about configuration files. ", 10)
	tail := "Remove a handler with app.off and the same callback."
	text := lead + "\n\n" + code + "\n\n" + filler + "\n\n" + tail
	got := trimSection(text, len(lead)+len(code)+len(tail)+40, queryTerms("remove handler app.off"))
	if !strings.HasPrefix(got, lead+"\n\n"+code) || !strings.Contains(got, tail) || strings.Contains(got, "Unrelated") || !strings.Contains(got, elidedMarker) {
		t.Errorf("trimmed:\n%s", got)
	}
	small := trimSection(text, len(lead)+webMinTrimBytes+30, nil)
	if !strings.HasPrefix(small, lead) || strings.Count(small, "```") != 2 || len(small) > len(lead)+webMinTrimBytes+30 {
		t.Errorf("a cut code block must be closed and fit (%d bytes):\n%s", len(small), small)
	}
	if trimSection("short", 100, nil) != "short" {
		t.Error("a section that fits is whole")
	}
}

// One page indexed at two addresses (x and x.html, or an llms/ text
// mirror beside the rendered page, whose texts differ) gives one result
// for an H2; an API reference leaf gives one family while other pages
// rank next.
func TestFamiliesCollapseAddressesAndReferenceLeaves(t *testing.T) {
	mk := func(page, anchor, h2, text string) docsindex.Section {
		return docsindex.Section{Page: page, Anchor: anchor, Heading: h2, Crumbs: []string{"Page", h2}, Text: text}
	}
	secs := []docsindex.Section{
		mk("https://m.test/guide/params", "a", "Params", "A param is denoted by a colon."),
		mk("https://m.test/guide/params.html", "a", "Params", "js\n\nA param is denoted by a colon."),
		mk("https://m.test/llms/guide/params", "a", "Params", "Params: a colon."),
		mk("https://m.test/api/core/router/push", "description", "Description", "Pushes a route."),
		mk("https://m.test/api/core/router/push", "usage", "Usage", "router.push('/x')"),
		mk("https://m.test/guide/nav", "push", "Push", "Navigate with router.push."),
	}
	ranked := []rankedSection{{0, 6}, {1, 5}, {2, 4}, {3, 3}, {4, 2}, {5, 1}}
	var got []string
	for _, f := range families(secs, ranked, 3, "https://m.test/") {
		got = append(got, secs[f.best].Page+"#"+secs[f.best].Anchor)
	}
	want := []string{"https://m.test/guide/params#a", "https://m.test/api/core/router/push#description", "https://m.test/guide/nav#push"}
	if !slices.Equal(got, want) {
		t.Errorf("families = %v, want %v", got, want)
	}
}
