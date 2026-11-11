package docs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// The frontmatter's fields print in one order: query, backend, library,
// version, index (or the pages a search read), status with its reason,
// terms, not_found, note; result_count is gone. An answer from elsewhere
// (Context7, a section fetch: no Index) keeps its old lines.
func TestFrontmatterOrder(t *testing.T) {
	f := Frontmatter{Query: "pam_duo failmode", Backend: SearchBackend, Library: "https://duo.com/docs/", Version: "2.1",
		Index: &IndexInfo{Kind: IndexFull, Pages: 734, Built: "2026-11-10"}, Status: Partial, Reason: ReasonTermsNotFound,
		Terms: []TermCount{{"pam_duo", 48}, {"failmode", 74}}, NotFound: []string{"pam_sss"}, Note: "a note"}
	want := "---\nquery: pam_duo failmode\nbackend: search\nlibrary: https://duo.com/docs/\nversion: 2.1\n" +
		"index: full, 734 pages, built 2026-11-10\nstatus: partial (terms_not_found)\n" +
		"terms: {pam_duo: 48, failmode: 74}\nnot_found: [pam_sss]\nnote: a note\n---\n"
	if got := f.Render(); got != want {
		t.Errorf("frontmatter:\n%s\nwant:\n%s", got, want)
	}
	answered := Frontmatter{Query: "q", Backend: SearchBackend, Library: "L", Index: searchRead(12), Status: Found, Terms: []TermCount{}, NotFound: []string{}}
	if got := answered.Render(); got != "---\nquery: q\nbackend: search\nlibrary: L\nsource: search, 12 pages read\nstatus: found\nterms: {}\nnot_found: []\n---\n" {
		t.Errorf("answered from pages read:\n%s", got)
	}
	capped := indexInfo(docsindex.Library{Pages: 400, Capped: true})
	built := indexInfo(docsindex.Library{Pages: 9, Resolved: resolvedBySearch})
	if capped.line() != "index: capped, 400 pages" || built.line() != "index: search, 9 pages" {
		t.Errorf("index lines %q, %q", capped.line(), built.line())
	}
	legacy := Frontmatter{Query: "q", Backend: "context7", Library: "/x/y", Status: Found}
	if got := legacy.Render(); got != "---\nquery: q\nbackend: context7\nlibrary: /x/y\n---\n" {
		t.Errorf("legacy:\n%s", got)
	}
	if got := termsFlow([]TermCount{{"a:b", 1}, {"x, y", 2}}); got != `{"a:b": 1, "x, y": 2}` {
		t.Errorf("quoted terms %s", got)
	}
}

// Terms print as the question writes them, once each: an identifier by
// its whole name (pam_sss, never sss or pamsss), a host name as one term,
// a hyphenated word as one phrase; filler and stopwords are none; two
// spellings of one term are the first.
func TestWrittenTerms(t *testing.T) {
	for _, c := range []struct {
		query string
		want  []string // text=match
	}{
		{"Duo Unix pam_duo PAM configuration pam_sss fallback when duosecurity.com cannot be reached",
			[]string{`Duo="duo"`, `Unix="unix"`, `pam_duo="pamduo"`, `PAM="pam"`, `configuration="configur"`, `pam_sss="pamsss"`,
				`fallback="fallback"`, `duosecurity.com="duosecurity com"`, `cannot="cannot"`, `reached="reach"`}},
		{"pam_duo PAM_SERVICE_ERR return value", []string{`pam_duo="pamduo"`, `PAM_SERVICE_ERR="pamserviceerr"`, `return="return"`, `value="valu"`}},
		{"github actions retry-on-failure option for a step", []string{`github="github"`, `actions="action"`, `retry-on-failure="retry on failur"`, `option="option"`, `step="step"`}},
		{"configure the configuration", []string{`configure="configur"`}},
	} {
		var got []string
		for _, wt := range writtenTerms(c.query, evidenceTerms(newRankQuery(c.query))) {
			got = append(got, wt.text+"="+wt.match)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%q:\n got %q\nwant %q", c.query, got, c.want)
		}
	}
	if wts := writtenTerms("pam_sss", evidenceTerms(newRankQuery("pam_sss"))); len(wts) != 1 || !wts[0].ident {
		t.Errorf("pam_sss is an identifier: %+v", wts)
	}
}

// The terms decide the status: found when every distinctive term has
// a hit, partial when one has none (listed in not_found, as written),
// refused when none has; a common term is not listed.
func TestTermVerdict(t *testing.T) {
	terms := func(counts ...int) []writtenTerm {
		var out []writtenTerm
		for i, n := range counts {
			out = append(out, writtenTerm{text: string(rune('a' + i)), sections: n})
		}
		markDistinctive(out, 1000)
		return out
	}
	if v := verdict(terms(5, 900, 7), 1000); v.refused || v.partial || len(v.notFound) != 0 || !slices.Equal(v.found, []TermCount{{"a", 5}, {"c", 7}}) {
		t.Errorf("found: %+v", v)
	}
	if v := verdict(terms(5, 0), 1000); v.refused || !v.partial || !slices.Equal(v.notFound, []string{"b"}) {
		t.Errorf("partial: %+v", v)
	}
	if v := verdict(terms(0, 0, 900), 1000); !v.refused || !slices.Equal(v.notFound, []string{"a", "b"}) || len(v.found) != 0 {
		t.Errorf("refused: %+v", v)
	}
	if v := verdict(terms(900, 950), 1000); v.refused || len(v.found) != 0 {
		t.Errorf("no distinctive term: every term is, none is listed: %+v", v)
	}
}

// The frontmatter's terms list only identifiers and words in under 1% of
// the library's sections or in at most 25 of them: a distinctive word in
// 26 of 1,000 sections is not listed, and the refusal still reads it.
func TestTermsShown(t *testing.T) {
	terms := func(counts ...int) []writtenTerm {
		var out []writtenTerm
		for i, n := range counts {
			out = append(out, writtenTerm{text: string(rune('a' + i)), sections: n})
		}
		markDistinctive(out, 1000)
		return out
	}
	if v := verdict(terms(9, 25, 26, 50), 1000); v.refused || !slices.Equal(v.found, []TermCount{{"a", 9}, {"b", 25}}) {
		t.Errorf("words in 1%% or more and over 25 of the sections are not listed: %+v", v)
	}
	if v := verdict(terms(50), 1000); v.refused || len(v.found) != 0 {
		t.Errorf("an unlisted distinctive word still answers: %+v", v)
	}
	small := []writtenTerm{{text: "a", sections: 3}, {text: "b", sections: 20}, {text: "c", ident: true, sections: 40}}
	markDistinctive(small, 60)
	if v := verdict(small, 60); !slices.Equal(v.found, []TermCount{{"a", 3}, {"c", 40}}) {
		t.Errorf("small library: %+v", v)
	}
}

// Through the index: an answer whose terms are all there is found; one
// naming a term the docs lack is partial and says which; one naming none
// of theirs is refused with an empty body, its not_found the answer.
func TestEvidenceStatus(t *testing.T) {
	// Unrelated pages, so that the question's words are rare in the library.
	secs := compactSectionsUnder(evRoot + "forms")
	for i := range 30 {
		secs = append(secs, evSection(fmt.Sprintf("other/%d", i), 0, "", []string{fmt.Sprintf("Other %d", i)}, "Unrelated words about routing and caching."))
	}
	l, lib := evLocal(t, secs)
	ctx := context.Background()
	built := lib
	built.Resolved = resolvedBySearch
	for _, c := range []struct {
		lib      docsindex.Library
		query    string
		status   Status
		reason   string
		notFound []string
		kind     string
	}{
		{lib, "default form action", Found, "", nil, IndexFull},
		{lib, "default form action use_enhance", Partial, ReasonTermsNotFound, []string{"use_enhance"}, IndexFull},
		{lib, "use_enhance zyzzyva", Refused, ReasonTermsNotFound, []string{"use_enhance", "zyzzyva"}, IndexFull},
		// A library a search built is partial for that too, both reasons said.
		{built, "default form action use_enhance", Partial, ReasonPartialSite + ", " + ReasonTermsNotFound, []string{"use_enhance"}, IndexSearch},
	} {
		o := l.answer(ctx, c.lib, c.query, LocalOptions{})
		refused := c.status == Refused
		if o.Status != c.status || o.Reason != c.reason || !slices.Equal(o.NotFound, c.notFound) || o.Index == nil || o.Index.Kind != c.kind ||
			o.Library != evRoot || (len(o.Results) == 0) != refused || errors.Is(o.Err(), ErrTermsNotFound) != refused {
			t.Errorf("%q: %s/%s not_found %v index %+v, %d results, err %v", c.query, o.Status, o.Reason, o.NotFound, o.Index, len(o.Results), o.Err())
		}
	}
}

// An answer prints a page's groups together under its title, pages in
// the order of their best group and a page's groups in page order;
// a group's heading is its path below the title; a cut group's source
// says so; one hint line ends an answer with a cut group, in the
// surface's words, and none ends one without.
func TestRenderAnswer(t *testing.T) {
	secs := []docsindex.Section{
		evSection("a", 0, "", []string{"Page A"}, "Lead of A."),
		evSection("b", 0, "x", []string{"Page B", "X"}, "B's X."),
		evSection("a", 1, "y", []string{"Page A", "Part", "Y"}, "A's Y.\n\nMore of Y, left out."),
	}
	s := sortPool(secs, evRoot)
	r := &ranked{plans: []plan{{sec: 2, pieces: []int{s.secs[2].pieces[0]}}, {sec: 1, whole: true}, {sec: 0, whole: true}}}
	gs := groupsOf(s, r)
	if len(gs) != 3 || gs[0].sec != 0 || gs[1].sec != 2 || gs[2].sec != 1 {
		t.Fatalf("printed order %+v", gs)
	}
	lib := docsindex.Library{Root: evRoot}
	var results []Result
	for _, g := range gs {
		results = append(results, renderGroup(lib, s, g))
	}
	cut := fmt.Sprintf(" (cut, %d of %d tokens)", estimateTokens("A's Y."), estimateTokens("A's Y.\n\nMore of Y, left out."))
	want := "## Page A\n\nLead of A.\n\nsource: " + evRoot + "a\n\n" +
		"### Part > Y\n\nA's Y.\n\nsource: " + evRoot + "a#y" + cut + "\n\n" +
		"## Page B\n\n### X\n\nB's X.\n\nsource: " + evRoot + "b#x\n"
	if got := RenderAnswer(results, HintNone); got != want {
		t.Errorf("answer:\n%s\nwant:\n%s", got, want)
	}
	for _, style := range []HintStyle{HintCLI, HintMCP} {
		got := RenderAnswer(results, style)
		if got != want+"\n"+AnswerHint(style)+"\n" || strings.Count(got, "(cut") != 2 {
			t.Errorf("style %d:\n%s", style, got)
		}
		if whole := RenderAnswer(slices.Delete(slices.Clone(results), 1, 2), style); strings.Contains(whole, "(cut") {
			t.Errorf("no cut group, style %d:\n%s", style, whole)
		}
	}
	if got := headingBelow(evSection("m", 0, "d", []string{"Module m", "Module m", "Directives"}, "x")); !slices.Equal(got, []string{"Directives"}) {
		t.Errorf("a heading repeating the title: %q", got)
	}
	if !strings.Contains(AnswerHint(HintCLI), "ketch docs --section") || !strings.Contains(AnswerHint(HintMCP), "docs(section=") || AnswerHint(HintNone) != "" {
		t.Error("hint wording")
	}
}

// --json carries the frontmatter's fields at the top and each group's
// parts as fields: page title, heading path, rows, same text, contents,
// section and the cut flag.
func TestAnswerJSONShape(t *testing.T) {
	r := Result{Library: evRoot, Title: "Y", Breadcrumb: "Page A > Part > Y", Snippet: "A's Y.", URL: evRoot + "a#y", Source: SearchBackend,
		Section: evRoot + "a#y", Truncated: true, PageTitle: "Page A", HeadingPath: []string{"Part", "Y"},
		Rows: []RowCount{{Kind: "option row", Shown: 2, Of: 17}}, SameText: &SameText{Count: 1, Sections: []string{"Z"}}, Contents: "also in this section: Q"}
	o := Outcome{Status: Partial, Reason: ReasonTermsNotFound, Results: []Result{r}, Index: &IndexInfo{Kind: IndexFull, Pages: 3, Built: "2026-11-10"},
		Terms: []TermCount{{"y", 1}}, NotFound: []string{"pam_sss"}}
	b, err := json.Marshal(AnswerJSON{Frontmatter: NewFrontmatter("q", SearchBackend, "", o), Results: o.Results})
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(b, &top)
	for _, k := range []string{"query", "backend", "library", "index", "status", "reason", "terms", "not_found", "results"} {
		if _, ok := top[k]; !ok {
			t.Errorf("no %q in %s", k, b)
		}
	}
	if _, ok := top["result_count"]; ok {
		t.Errorf("result_count in %s", b)
	}
	var res []map[string]json.RawMessage
	_ = json.Unmarshal(top["results"], &res)
	for _, k := range []string{"page_title", "heading_path", "rows", "same_text", "contents", "section", "truncated", "snippet", "url"} {
		if _, ok := res[0][k]; !ok {
			t.Errorf("no %q in the result %s", k, top["results"])
		}
	}
	if got := r.GroupLine(); got != "option rows, 2 of 17 · same text in 1 other section: Z" {
		t.Errorf("group line %q", got)
	}
	// An answer with every term found still says not_found: [].
	empty, _ := json.Marshal(NewFrontmatter("q", SearchBackend, "", Outcome{Status: Found, Index: &IndexInfo{Kind: IndexFull}}))
	if !strings.Contains(string(empty), `"not_found":[]`) || !strings.Contains(string(empty), `"terms":[]`) {
		t.Errorf("empty lists: %s", empty)
	}
}

// MDX residue goes from prose: a line of only JSX component tags and MDX
// comments, never in code blocks or inline code; an HTML element's tag
// line is not a component's.
func TestCleanMDX(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"<Sandpack>\n\nText.\n\n</Sandpack>", "\nText.\n"},
		{"<Intro />\nHello.", "Hello."},
		{"<DeepDive title=\"Why\">\nBody.\n</DeepDive>", "Body."},
		{"### Parameters {/*parameters*/}\nA list.", "### Parameters\nA list."},
		{"Before {/* a note */} after.", "Before  after."},
		{"Text.\n{/* one\ntwo */}\nMore.", "Text.\nMore."},
		{"```jsx\n<Sandpack>\n{/* kept */}\n```", "```jsx\n<Sandpack>\n{/* kept */}\n```"},
		{"```\n<Intro />\n{/* kept */}\n```", "```\n<Intro />\n{/* kept */}\n```"},
		{"Use `{/* x */}` and `<Intro />` inline.", "Use `{/* x */}` and `<Intro />` inline."},
		{"<table>\nKept: lower case is HTML.\n</table>", "<table>\nKept: lower case is HTML.\n</table>"},
		{"Call <Button> in text.", "Call <Button> in text."},
	} {
		if got := cleanText(c.in); got != c.want {
			t.Errorf("cleanText(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	if got := cleanLine("Parameters {/*parameters*/}"); got != "Parameters" {
		t.Errorf("heading %q", got)
	}
}

// The trace's not_shown: the distinctive terms the library holds that no
// printed group names, in its text or heading path, by any of their forms
// (an identifier by its whole name, a host name whole); a term with no
// hit is not_found's, not this.
func TestNotShown(t *testing.T) {
	q := "pam_duo failmode duosecurity.com unreachable fallback zyzzyva"
	wts := writtenTerms(q, evidenceTerms(newRankQuery(q)))
	for i := range wts {
		wts[i].sections = 5
		if wts[i].text == "zyzzyva" {
			wts[i].sections = 0
		}
	}
	markDistinctive(wts, 1000)
	results := []Result{
		{Snippet: "Set `failmode` in pam_duo.conf.", PageTitle: "Duo Unix", HeadingPath: []string{"Options"}},
		{Snippet: "Requests go to api-x.duosecurity.com.", PageTitle: "Fallback", HeadingPath: nil},
	}
	if got := notShown(wts, results); !slices.Equal(got, []string{"unreachable"}) {
		t.Errorf("not shown = %q, want [unreachable]", got)
	}
}
