package docs

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/failure"
)

const secRoot = "https://docs.example.test/docs/"

// sectionLocal is a local backend over a hand-built index (text only, so
// ranking is by keywords): a forms page with an H2 holding two H3s and a
// long H2 of many paragraphs, and an actions page whose sections are too
// big for a small budget.
func sectionLocal(t *testing.T, display string) *Local {
	t.Helper()
	store := OpenIndex(filepath.Join(t.TempDir(), "docs-index.db"))
	filler := strings.Repeat("Unrelated prose about the history of the project and its many contributors. ", 30)
	code := func(s string) string { return "```js\n" + s + "\n```" }
	var long []string
	for i := range 12 {
		long = append(long, fmt.Sprintf("Paragraph %d of the long section explains one more detail of the feature at length, with words to count.", i))
	}
	forms := secRoot + "guide/forms"
	pages := []docsindex.PageData{
		{Page: docsindex.Page{URL: forms, Title: "Forms", Hash: "f"}, Sections: []docsindex.Section{
			{Page: forms, Order: 0, Heading: "Forms", Crumbs: []string{"Forms"}, Text: "Forms collect input."},
			{Page: forms, Order: 1, Anchor: "validation", Heading: "Validation", Crumbs: []string{"Forms", "Validation"}, Text: "Validate fields with rules.\n\n" + code("form.validate()")},
			{Page: forms, Order: 2, Anchor: "rules", Heading: "Rules", Crumbs: []string{"Forms", "Validation", "Rules"}, Text: "A rule is a function returning an error or null.\n\n" + code("name: (v) => (v.length < 2 ? 'Too short' : null),")},
			{Page: forms, Order: 3, Anchor: "async", Heading: "Async", Crumbs: []string{"Forms", "Validation", "Async"}, Text: "Async rules return a promise."},
			{Page: forms, Order: 4, Anchor: "submitting", Heading: "Submitting", Crumbs: []string{"Forms", "Submitting"}, Text: "Submit with handleSubmit."},
			{Page: forms, Order: 5, Anchor: "long", Heading: "Long", Crumbs: []string{"Forms", "Long"}, Text: strings.Join(long, "\n\n") + "\n\n" + code("last()")},
		}},
		{Page: docsindex.Page{URL: secRoot + "guide/actions", Title: "Actions", Hash: "a"}, Sections: compactSectionsAt(secRoot+"guide/actions", filler)},
	}
	if err := store.PutPages(secRoot, pages); err != nil {
		t.Fatal(err)
	}
	lib := docsindex.Library{Key: secRoot, Library: "example", Names: []string{"example"}, Root: secRoot, DisplayRoot: display, Format: docsindex.FormatVersion, Built: time.Now().UTC(), Pages: 2}
	if err := store.PutLibrary(lib); err != nil {
		t.Fatal(err)
	}
	return NewLocal(store)
}

// compactSectionsAt is compactSections on another page.
func compactSectionsAt(page, _ string) []docsindex.Section {
	secs := compactSections()
	for i := range secs {
		secs[i].Page = page
	}
	return secs
}

func fetch(t *testing.T, l *Local, library, ref string, tokens int) Outcome {
	t.Helper()
	return l.SectionSelect(WithLocalOptions(context.Background(), LocalOptions{Hint: HintCLI}), library, ref, tokens)
}

func mustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
}

// An H2 reference returns its family: the H2's text and the subsections
// under it, headed, code intact, under the reference it was asked by.
func TestSectionFetchFamily(t *testing.T) {
	l := sectionLocal(t, "")
	ref := secRoot + "guide/forms#validation"
	o := fetch(t, l, "", ref, 0)
	if o.Status != Answered || len(o.Results) != 1 || o.Backend != SearchBackend {
		t.Fatalf("outcome = %+v", o)
	}
	r := o.Results[0]
	if r.URL != ref || r.Section != ref || r.Source != SearchBackend || r.Library != secRoot || r.Truncated ||
		r.Title != "Validation" || r.Breadcrumb != "Forms > Validation" || strings.Join(r.Anchors, ",") != "validation,rules,async" {
		t.Errorf("result = %+v", r)
	}
	mustContain(t, r.Snippet, "Validate fields with rules.", "form.validate()", "#### Rules\n\nA rule is a function", "name: (v) => (v.length < 2 ? 'Too short' : null),", "#### Async")
	if strings.Contains(r.Snippet, "handleSubmit") || strings.Contains(r.Snippet, "Forms collect") {
		t.Errorf("family holds another section:\n%s", r.Snippet)
	}
}

// An H3 reference returns that section alone; a page's lead is itself.
func TestSectionFetchExactSection(t *testing.T) {
	l := sectionLocal(t, "")
	r := fetch(t, l, secRoot, secRoot+"guide/forms#rules", 0).Results[0]
	if !strings.Contains(r.Snippet, "A rule is a function") || strings.Contains(r.Snippet, "Async rules") || strings.Contains(r.Snippet, "####") || r.Breadcrumb != "Forms > Validation > Rules" {
		t.Errorf("rules = %+v", r)
	}
}

// A page URL with no anchor returns the page's sections in order.
func TestSectionFetchPage(t *testing.T) {
	l := sectionLocal(t, "")
	r := fetch(t, l, "", secRoot+"guide/forms", 0).Results[0]
	if r.URL != secRoot+"guide/forms" || r.Title != "Forms" || r.Truncated {
		t.Fatalf("page result = %+v", r)
	}
	last := -1
	for _, want := range []string{"Forms collect input.", "#### Validation", "#### Rules", "#### Async", "#### Submitting", "#### Long", "last()"} {
		i := strings.Index(r.Snippet, want)
		if i <= last {
			t.Errorf("%q out of order or missing (at %d after %d)", want, i, last)
		}
		last = i
	}
	// A trailing slash or .html is the same page.
	if r2 := fetch(t, l, "", secRoot+"guide/forms/", 0); r2.Status != Answered {
		t.Errorf("trailing slash = %s %v", r2.Status, r2.Err())
	}
}

// Unknown pages, anchors and libraries are not_found / not_indexed
// refusals through the Outcome machinery; a reference that is no URL is a
// validation error.
func TestSectionFetchRefusals(t *testing.T) {
	l := sectionLocal(t, "")
	o := fetch(t, l, "", secRoot+"guide/forms#nope", 0)
	if o.Status != Refused || o.Reason != "not_found" || !errors.Is(o.Err(), ErrSectionNotFound) || o.Kind() != failure.NotFound ||
		!strings.Contains(o.Err().Error(), "validation, rules, async") {
		t.Errorf("unknown anchor = %s/%s %v", o.Status, o.Reason, o.Err())
	}
	for _, c := range []struct {
		library, ref, reason string
		kind                 failure.Kind
	}{
		{"", secRoot + "guide/missing", "not_found", failure.NotFound},
		{"", secRoot + "guide/missing#x", "not_found", failure.NotFound},
		{"", "https://elsewhere.test/docs/guide/forms#rules", "not_found", failure.NotFound},
		{secRoot, secRoot + "guide/missing#x", "not_found", failure.NotFound},
		{"express", secRoot + "guide/forms#rules", "not_indexed", failure.NotFound},
		{"", "guide/forms#rules", "invalid_section", failure.Validation},
	} {
		if o := fetch(t, l, c.library, c.ref, 0); o.Status != Refused || o.Reason != c.reason || o.Kind() != c.kind {
			t.Errorf("%q %s = %s/%s %v", c.library, c.ref, o.Status, o.Reason, o.Err())
		}
	}
}

// A fetch is capped at a block boundary within the cap, ends with "[…]"
// and where to read on, and says so in Truncated: the next section when
// the cut falls after the first, a larger cap when it falls in the first.
func TestSectionFetchCap(t *testing.T) {
	l := sectionLocal(t, "")
	o := fetch(t, l, "", secRoot+"guide/forms#long", 90)
	r := o.Results[0]
	if !r.Truncated || !strings.Contains(r.Snippet, "Paragraph 0") || strings.Contains(r.Snippet, "Paragraph 11") ||
		!strings.Contains(r.Snippet, "\n\n"+elidedMarker+" cut at 90 tokens; the section is about ") || !strings.Contains(r.Snippet, ": raise --tokens") {
		t.Errorf("cut in the first section:\n%s", r.Snippet)
	}
	if got := estimateTokens(RenderSnippet(r)); got > 90 {
		t.Errorf("estimate %d over the cap", got)
	}
	if strings.Contains(r.Snippet, "Paragraph 5 of the long section explains one more detail of the feature at length, with words to count.\n\nParagraph 6 of the long section explains one more detail of the feature at length, with words to") {
		t.Errorf("cut inside a paragraph")
	}
	page := fetch(t, l, "", secRoot+"guide/forms", 110).Results[0]
	if !page.Truncated || !strings.HasSuffix(page.Snippet, "next section: ketch docs --section "+secRoot+"guide/forms#"+nextAnchor(page.Snippet)) {
		t.Errorf("cut after the first section:\n%s", page.Snippet)
	}
	if got := estimateTokens(RenderSnippet(page)); got > 110 {
		t.Errorf("page estimate %d over the cap", got)
	}
	// The MCP wording is the docs call.
	mcp := l.SectionSelect(WithLocalOptions(context.Background(), LocalOptions{Hint: HintMCP}), "", secRoot+"guide/forms", 110).Results[0]
	if !strings.Contains(mcp.Snippet, `next section: docs(section="`+secRoot+"guide/forms#") {
		t.Errorf("MCP notice:\n%s", mcp.Snippet)
	}
	// A code block is never cut inside: the fences stay balanced at every cap.
	for tokens := 20; tokens < 400; tokens += 15 {
		r := fetch(t, l, "", secRoot+"guide/forms", tokens).Results[0]
		if strings.Count(r.Snippet, "```")%2 != 0 {
			t.Errorf("cap %d left a fence open:\n%s", tokens, r.Snippet)
		}
	}
}

// nextAnchor is the anchor a "next section:" notice at the end of text names.
func nextAnchor(text string) string {
	_, a, _ := strings.Cut(text[strings.LastIndex(text, "next section:"):], "#")
	return a
}

// A library shown under unversioned addresses is fetched by the address
// its results show, and answers under it.
func TestSectionFetchDisplayRoot(t *testing.T) {
	l := sectionLocal(t, "https://docs.example.test/")
	ref := "https://docs.example.test/guide/forms#rules"
	r := fetch(t, l, "", ref, 0).Results[0]
	if r.URL != ref || r.Section != ref || !strings.Contains(r.Snippet, "A rule is a function") {
		t.Errorf("result = %+v", r)
	}
	// The stored address works too, and answers under the shown one.
	if r2 := fetch(t, l, "", secRoot+"guide/forms#rules", 0).Results[0]; r2.URL != ref {
		t.Errorf("stored address answers under %s", r2.URL)
	}
}

// askCompact answers the actions query from the fixture library.
func askCompact(t *testing.T, l *Local, opts LocalOptions) []Result {
	t.Helper()
	o := l.GetDocsSelect(WithLocalOptions(context.Background(), opts), secRoot, "default form action POST", 0)
	if o.Status != Answered {
		t.Fatalf("outcome = %s %v", o.Status, o.Err())
	}
	return o.Results
}

// Results carry their section reference; a result cut short says so in
// JSON and, in an answer shown as text, its source line is marked
// "(cut)" and the answer ends with the one line saying how to read it in
// full; the pointer is not part of the snippet.
func TestCompactAnswerPointsToFullSection(t *testing.T) {
	cut := askCompact(t, sectionLocal(t, ""), LocalOptions{Tokens: 300, Hint: HintCLI})
	if len(cut) == 0 || !cut[0].Truncated {
		t.Fatalf("the fixture's first result should be cut at 300 tokens: %+v", cut)
	}
	text := RenderAnswer(cut, HintCLI)
	for _, r := range cut {
		if r.Section != r.URL || r.Section == "" {
			t.Errorf("section = %q, url %q", r.Section, r.URL)
		}
		if r.Truncated != strings.Contains(text, "source: "+r.URL+" (cut)\n") {
			t.Errorf("truncated=%v but rendering is:\n%s", r.Truncated, text)
		}
		if strings.Contains(r.Snippet, elidedMarker) || strings.Contains(r.Snippet, "full section") {
			t.Errorf("truncated=%v, snippet:\n%s", r.Truncated, r.Snippet)
		}
	}
	if strings.Count(text, AnswerHint(HintCLI)) != 1 || !strings.HasSuffix(text, "\n"+AnswerHint(HintCLI)+"\n") {
		t.Errorf("hint line:\n%s", text)
	}
}

// A result shown whole (a section that fits the budget left) is not
// marked cut, and an answer with none cut, or with no style, has no hint
// line; the section reference is there either way.
func TestCompactAnswerPointerAbsent(t *testing.T) {
	l := sectionLocal(t, "")
	o := l.GetDocsSelect(WithLocalOptions(context.Background(), LocalOptions{Hint: HintCLI}), secRoot, "async rules promise", 0)
	if o.Status != Answered || len(o.Results) == 0 || o.Results[0].URL != secRoot+"guide/forms#async" {
		t.Fatalf("outcome = %+v", o)
	}
	if r := o.Results[0]; r.Truncated || r.Snippet != "Async rules return a promise." || strings.Contains(RenderGroup(r, true), "(cut)") {
		t.Errorf("a whole result is marked cut:\n%s", RenderGroup(r, true))
	}
	if text := RenderAnswer(o.Results[:1], HintCLI); strings.Contains(text, AnswerHint(HintCLI)) {
		t.Errorf("a hint with nothing cut:\n%s", text)
	}
	for _, opts := range []LocalOptions{{Tokens: 300}, {Hint: HintCLI}} {
		for _, r := range askCompact(t, l, opts) {
			if r.Section == "" || strings.Contains(r.Snippet, "full section") {
				t.Errorf("with %+v: section %q, snippet:\n%s", opts, r.Section, r.Snippet)
			}
		}
	}
	// An answer with a group cut, but no style, has no hint line.
	small := askCompact(t, l, LocalOptions{Tokens: 300})
	if !small[0].Truncated || strings.Contains(RenderAnswer(small, HintNone), "full text") {
		t.Errorf("small budget result: truncated=%v", small[0].Truncated)
	}
}

// Everything printed is counted: at every budget the frontmatter and the
// body, hint line included, stay within it by the estimate, unless the
// budget is too small for any group (the first still prints, alone).
func TestCompactBudgetCountsPointer(t *testing.T) {
	l := sectionLocal(t, "")
	const query = "default form action POST validation rules"
	for tokens := 150; tokens <= 1200; tokens += 75 {
		for _, style := range []HintStyle{HintCLI, HintMCP, HintNone} {
			o := l.GetDocsSelect(WithLocalOptions(context.Background(), LocalOptions{Tokens: tokens, Hint: style}), secRoot, query, 0)
			total := estimateTokens(NewFrontmatter(query, SearchBackend, secRoot, o).Render() + RenderAnswer(o.Results, style))
			if total > tokens && len(o.Results) > 1 {
				t.Errorf("style %d at %d tokens: answer estimates %d", style, tokens, total)
			}
		}
	}
}

// Over MCP the pointer is the docs call, said once for the answer
// (AnswerHint), never in a result's text; the section field names what
// it takes.
func TestCompactAnswerMCPPointer(t *testing.T) {
	l := sectionLocal(t, "")
	o := l.GetDocsSelect(WithLocalOptions(context.Background(), LocalOptions{Tokens: 300, Hint: HintMCP}), secRoot, "default form action POST", 0)
	r := o.Results[0]
	if !r.Truncated || r.Section != r.URL || strings.Contains(r.Snippet, "full section") || strings.Contains(r.Snippet, elidedMarker) {
		t.Errorf("MCP snippet:\n%s", r.Snippet)
	}
	if body := RenderAnswer(o.Results, HintMCP); strings.Count(body, `docs(section="<its source>")`) != 1 || !strings.HasSuffix(body, AnswerHint(HintMCP)+"\n") {
		t.Errorf("MCP answer:\n%s", body)
	}
	// The call it names fetches that section.
	if f := fetch(t, l, "", r.Section, 0); f.Status != Answered || !strings.Contains(f.Results[0].Snippet, "export const actions") {
		t.Errorf("following the pointer: %s %v", f.Status, f.Err())
	}
}
