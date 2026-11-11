package docs

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
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
	return l.SectionSelect(WithLocalOptions(context.Background(), LocalOptions{Hint: HintCLI}), library, ref, tokens, 0)
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
	if o.Status != Found || len(o.Results) != 1 || o.Backend != SearchBackend {
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
	if r2 := fetch(t, l, "", secRoot+"guide/forms/", 0); r2.Status != Found {
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

// A section longer than the page size is read in pages: each ends at a
// block boundary (never inside a code block), says which page of how many
// it is, and, but the last, gives the call for the next page, with the
// page size the caller set; read in order, the pages are the section.
// Each fits the page size as printed, header and next-page lines in.
func TestSectionFetchPages(t *testing.T) {
	l := sectionLocal(t, "")
	ref := secRoot + "guide/forms#long"
	whole := fetch(t, l, "", ref, 0).Results[0]
	if whole.Truncated || whole.Pages != 0 || whole.Next != "" || PageLine(whole) != "" || NextPageLine(whole) != "" {
		t.Fatalf("a section that fits one page: %+v", whole)
	}
	pages := readPages(t, l, ref, 90, HintCLI)
	if len(pages) < 3 {
		t.Fatalf("pages at 90 tokens: %d", len(pages))
	}
	var texts []string
	for n, r := range pages {
		checkPage(t, r, n+1, len(pages), ref, 90)
		texts = append(texts, r.Snippet)
	}
	if strings.Join(texts, "\n\n") != whole.Snippet {
		t.Errorf("the pages in order are not the section:\n%s", strings.Join(texts, "\n\n"))
	}
}

// A page past the last is a validation error naming how many there are;
// a page URL with no anchor pages through the page's sections in order;
// over MCP the next page is the docs call, with the library given; a code
// block is never cut, at any page size.
func TestSectionFetchPagesMore(t *testing.T) {
	l := sectionLocal(t, "")
	ref := secRoot + "guide/forms#long"
	pages := readPages(t, l, ref, 90, HintCLI)
	o := l.SectionSelect(WithLocalOptions(context.Background(), LocalOptions{Hint: HintCLI}), "", ref, 90, len(pages)+1)
	if o.Status != Refused || o.Reason != "invalid_page" || o.Kind() != failure.Validation || !strings.Contains(o.Err().Error(), fmt.Sprintf("of %d pages", len(pages))) {
		t.Errorf("past the end = %s/%s %v", o.Status, o.Reason, o.Err())
	}
	if o := l.SectionSelect(WithLocalOptions(context.Background(), LocalOptions{Hint: HintCLI}), "", secRoot+"guide/forms#rules", 0, 2); o.Reason != "invalid_page" || !strings.Contains(o.Err().Error(), "of 1 page ") {
		t.Errorf("page 2 of a section of one = %s/%s %v", o.Status, o.Reason, o.Err())
	}
	// A page URL with no anchor pages through the page's sections in order.
	page := readPages(t, l, secRoot+"guide/forms", 110, HintCLI)
	all := ""
	for _, r := range page {
		all += r.Snippet + "\n\n"
	}
	last := -1
	for _, want := range []string{"Forms collect input.", "#### Validation", "#### Rules", "#### Async", "#### Submitting", "#### Long", "last()"} {
		i := strings.Index(all, want)
		if i <= last {
			t.Errorf("%q out of order or missing across %d pages", want, len(page))
		}
		last = i
	}
	// The MCP wording is the docs call; the library given is named in it.
	mcp := l.SectionSelect(WithLocalOptions(context.Background(), LocalOptions{Hint: HintMCP}), secRoot, secRoot+"guide/forms", 110, 1).Results[0]
	if mcp.Next != `docs(section="`+secRoot+`guide/forms", library="`+secRoot+`", tokens=110, page=2)` {
		t.Errorf("MCP next = %q", mcp.Next)
	}
	// A code block is never cut inside: the fences stay balanced on every
	// page at every size.
	for tokens := 20; tokens < 400; tokens += 15 {
		for _, r := range readPages(t, l, secRoot+"guide/forms", tokens, HintCLI) {
			balancedFences(t, r)
		}
	}
}

// checkPage checks page n of pages of the section ref read at tokens a
// page: its page line, its next-page call, its size as printed, its
// fences.
func checkPage(t *testing.T, r Result, n, pages int, ref string, tokens int) {
	t.Helper()
	if !r.Truncated || r.Page != n || r.Pages != pages || PageLine(r) != fmt.Sprintf("page: %d of %d", n, pages) {
		t.Errorf("page %d: %+v", n, r)
	}
	want := ""
	if n < pages {
		want = fmt.Sprintf("ketch docs --section %s --tokens %d --page %d", ref, tokens, n+1)
	}
	if r.Next != want || NextPageLine(r) != "" && NextPageLine(r) != "next page: "+want {
		t.Errorf("page %d next = %q, want %q", n, r.Next, want)
	}
	if got := estimateTokens(RenderSnippet(r) + PageLine(r) + "\n" + NextPageLine(r) + "\n"); got > tokens {
		t.Errorf("page %d estimates %d tokens", n, got)
	}
	balancedFences(t, r)
}

// balancedFences fails when a page of a section fetch leaves a code
// fence open.
func balancedFences(t *testing.T, r Result) {
	t.Helper()
	if strings.Count(r.Snippet, "```")%2 != 0 {
		t.Errorf("page %d of %d leaves a fence open:\n%s", r.Page, r.Pages, r.Snippet)
	}
}

// checkTablePage checks a page of TestSectionPagesTable's section: rows
// only under their table's header (repeated on a page that continues the
// table), an item with its continuation line; seen counts the rows.
func checkTablePage(t *testing.T, r Result, header string, seen map[string]int) {
	t.Helper()
	lines := strings.Split(r.Snippet, "\n")
	for k, line := range lines {
		if strings.HasPrefix(line, "| `key_") {
			seen[line]++
			if k == 0 || !strings.HasPrefix(lines[k-1], "| `key_") && !strings.HasPrefix(lines[k-1], "|---") {
				t.Errorf("page %d: a row away from its table:\n%s", r.Page, r.Snippet)
			}
		}
		if strings.HasPrefix(line, "  and goes on") && (k == 0 || !strings.HasPrefix(lines[k-1], "- item")) {
			t.Errorf("page %d: an item cut from its continuation:\n%s", r.Page, r.Snippet)
		}
	}
	if strings.Contains(r.Snippet, "| `key_") && !strings.HasPrefix(r.Snippet, header+"\n") && !strings.HasPrefix(r.Snippet, "The options.") {
		t.Errorf("page %d: a table page not headed by the header:\n%s", r.Page, r.Snippet)
	}
}

// readPages reads every page of ref at tokens a page.
func readPages(t *testing.T, l *Local, ref string, tokens int, style HintStyle) []Result {
	t.Helper()
	var out []Result
	for n := 1; ; n++ {
		o := l.SectionSelect(WithLocalOptions(context.Background(), LocalOptions{Hint: style}), "", ref, tokens, n)
		if len(o.Results) != 1 {
			t.Fatalf("page %d: %s %v", n, o.Status, o.Err())
		}
		out = append(out, o.Results[0])
		if o.Results[0].Pages <= n {
			return out
		}
		if n > 200 {
			t.Fatal("no last page")
		}
	}
}

// A table longer than a page breaks between rows, and a page that
// continues it opens with its header row; a list breaks between items,
// an item's continuation lines and a fenced block whole.
func TestSectionPagesTable(t *testing.T) {
	page := "https://t.test/docs/options"
	var rows []string
	for i := range 40 {
		rows = append(rows, fmt.Sprintf("| `key_%d` | Optional | What key %d does, in a sentence or so. |", i, i))
	}
	header := "| Key | Required? | Description |\n|---|---|---|"
	var items []string
	for i := range 20 {
		items = append(items, fmt.Sprintf("- item %d says a thing\n  and goes on", i))
	}
	items = append(items, "```sh\nrun --this\nrun --that\n```")
	secs := []docsindex.Section{{Page: page, Anchor: "opts", Heading: "Options", Crumbs: []string{"Options"},
		Text: "The options.\n\n" + header + "\n" + strings.Join(rows, "\n") + "\n\n" + strings.Join(items, "\n")}}
	lib := docsindex.Library{Root: "https://t.test/docs/"}
	var got []Result
	for n := 1; ; n++ {
		o := fetchSection(lib, secs, "opts", "", 150, n, HintCLI)
		if len(o.Results) != 1 {
			t.Fatalf("page %d: %v", n, o.Err())
		}
		got = append(got, o.Results[0])
		if o.Results[0].Next == "" {
			break
		}
	}
	if len(got) < 3 {
		t.Fatalf("pages: %d", len(got))
	}
	seen := map[string]int{}
	for _, r := range got {
		checkTablePage(t, r, header, seen)
		balancedFences(t, r)
	}
	for _, row := range rows {
		if seen[row] != 1 {
			t.Errorf("row %q printed %d times", row, seen[row])
		}
	}
	if !strings.HasSuffix(got[len(got)-1].Snippet, "```sh\nrun --this\nrun --that\n```") {
		t.Errorf("last page:\n%s", got[len(got)-1].Snippet)
	}
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
	if o.Status != Found {
		t.Fatalf("outcome = %s %v", o.Status, o.Err())
	}
	return o.Results
}

// Results carry their section reference; a result cut short says so in
// JSON and, in an answer shown as text, its source line is marked
// "(cut, N of M tokens)" and the answer ends with the one line saying how to read it in
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
		if r.Truncated != strings.Contains(text, "source: "+r.URL+" (cut, ") || r.Truncated && !strings.Contains(text, "source: "+r.URL+cutNote(r)+"\n") {
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
	at := slices.IndexFunc(o.Results, func(r Result) bool { return r.URL == secRoot+"guide/forms#async" })
	if o.Status != Found || at < 0 {
		t.Fatalf("outcome = %+v", o)
	}
	if r := o.Results[at]; r.Truncated || r.Snippet != "Async rules return a promise." || strings.Contains(RenderGroup(r, true), "(cut") {
		t.Errorf("a whole result is marked cut:\n%s", RenderGroup(r, true))
	}
	if text := RenderAnswer(o.Results[at:at+1], HintCLI); strings.Contains(text, AnswerHint(HintCLI)) {
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
	if f := fetch(t, l, "", r.Section, 0); f.Status != Found || !strings.Contains(f.Results[0].Snippet, "export const actions") {
		t.Errorf("following the pointer: %s %v", f.Status, f.Err())
	}
}
