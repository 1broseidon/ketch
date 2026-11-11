package docs

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

const evRoot = "https://x.test/docs/"

// evSection is a section of a page under evRoot.
func evSection(page string, order int, anchor string, crumbs []string, text string) docsindex.Section {
	return docsindex.Section{Page: evRoot + page, Order: order, Anchor: anchor, Heading: crumbs[len(crumbs)-1], Crumbs: crumbs, Text: text}
}

// evGathered is what Gather gives for query over a library of n sections,
// counts holding each term's sections (0 for a term not given).
func evGathered(query string, n int, counts map[string]int) *gathered {
	q := newRankQuery(query)
	g := &gathered{query: q, terms: evidenceTerms(q), sections: n}
	for i := range g.terms {
		g.terms[i].sections = counts[g.terms[i].term]
	}
	return g
}

// evRanked scores s's pieces for g and puts them in fused order, nothing
// printed yet; a section with no rank in Gather's ordering takes its place
// in the pool.
func evRanked(s *sorted, g *gathered, budget, limit int) *ranked {
	for i := range s.secs {
		if s.secs[i].order == 0 {
			s.secs[i].order = i + 1
		}
	}
	r := &ranked{budget: budget, limit: limit}
	r.weights, r.distinct, r.scores = scorePieces(s, g.terms, g.sections, false)
	rankPieces(s, r)
	return r
}

// Cleaning takes out what extraction leaked, and nothing else: tags keep
// their text, <br></br> is one line break (in a table row the break a cell
// holds), a code block loses only presentational markup and an HTML block
// none, badge and tracking image lines go, escapes that change nothing go
// but inline code and meaningful escapes stay. It is pure: cleaning twice
// is cleaning once.
func TestCleanText(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`Set <span class="toreplace">KEY</span> here.`, "Set KEY here."},
		{"one<br></br>two", "one\ntwo"},
		{"| a | x<br></br>y |", "| a | x<br>y |"},
		{"```\nikey = <span class=\"toreplace\">INTEGRATION_KEY</span>\n<span class=\"variable\">auth requisite\nauth sufficient pam_duo.so</span>\nend<br/>line\n```",
			"```\nikey = INTEGRATION_KEY\nauth requisite\nauth sufficient pam_duo.so\nend\nline\n```"},
		{"```html\n<span class=\"x\">a</span>\n```", "```html\n<span class=\"x\">a</span>\n```"},
		{"```\ngit clone <url>\nconst a = <div>x</div>\n```", "```\ngit clone <url>\nconst a = <div>x</div>\n```"},
		{"Run ketch docs <query> now.", "Run ketch docs <query> now."},
		{"Intro\n[![build](https://ci.test/b.svg)](https://ci.test) [![cov](https://c.test/c.svg)](https://c.test)\n![](https://t.test/p.gif)\nText", "Intro\nText"},
		{"![Network diagram](https://x.test/d.png)", "![Network diagram](https://x.test/d.png)"},
		{`the pam\_duo module, authlogin\_duo too`, "the pam_duo module, authlogin_duo too"},
		{`\_\_init\_\_ and 1\. first`, `\_\_init\_\_ and 1. first`},
		{"1\\. not a list\n\\- not an item\n\\# not a heading", "1\\. not a list\n\\- not an item\n\\# not a heading"},
		{"`pam\\_duo` stays", "`pam\\_duo` stays"},
		{`| a \| b | c\-d |`, `| a \| b | c\-d |`},
		{`see (a\) or b\! and x\-y`, `see (a) or b! and x-y`},
	} {
		if got := cleanText(c.in); got != c.want {
			t.Errorf("cleanText(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
		if again := cleanText(cleanText(c.in)); again != cleanText(c.in) {
			t.Errorf("cleaning %q twice changed it: %q", c.in, again)
		}
	}
	s := cleanSection(docsindex.Section{Heading: `pam\_duo`, Crumbs: []string{`Duo (pam\_duo)`, `pam\_duo`}, Text: `x <b>y</b>`})
	if s.Heading != "pam_duo" || s.Crumbs[0] != "Duo (pam_duo)" || s.Text != "x y" {
		t.Errorf("cleanSection = %+v", s)
	}
}

// A section splits into paragraphs, list items (ordered: steps), code
// blocks with their tab label, notes and table rows; a table is never one
// piece, each body row keeping its table (whose header row is its
// context), a row whose first cell is code an option row; a delimiter row
// in the body is no row; labels are no piece.
func TestSplitText(t *testing.T) {
	text := "Configure the module:\n\n| Key | Required? | Description |\n|---|---|---|\n| `failmode` | Optional | Fail safe or secure. |\n| Timeout | Optional | Seconds to wait. |\n|---|\n\n" +
		"Steps:\n\n1. Install it.\n2. Configure it.\n\n- one item\n  continued\n- two\n\nNote: Restart after changes.\n\n> **Warning:** Back up first.\n\n" +
		"Run this:\n\nshell\n\n```sh\nmake install\n```\n\nShort label\n\nA sentence here. Another one follows."
	pieces, tables := splitText(text)
	var got []string
	for _, p := range pieces {
		got = append(got, fmt.Sprintf("%s:%s", p.kind, p.text))
	}
	want := []string{
		"prose:Configure the module:",
		"option row:| `failmode` | Optional | Fail safe or secure. |",
		"row:| Timeout | Optional | Seconds to wait. |",
		"prose:Steps:",
		"step:1. Install it.",
		"step:2. Configure it.",
		"item:- one item\n  continued",
		"item:- two",
		"note:Note: Restart after changes.",
		"note:> **Warning:** Back up first.",
		"prose:Run this:",
		"example:shell\n\n```sh\nmake install\n```",
		"prose:A sentence here. Another one follows.",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("pieces:\n got %q\nwant %q", got, want)
	}
	if len(tables) != 1 || tables[0].header != "| Key | Required? | Description |\n|---|---|---|" || !slices.Equal(tables[0].rows, []int{1, 2}) {
		t.Errorf("tables = %+v", tables)
	}
	if pieces[1].table != 0 || pieces[1].def != "`failmode`" || pieces[2].def != "Timeout" {
		t.Errorf("rows = %+v %+v", pieces[1], pieces[2])
	}
}

// A table without a delimiter row has no header: all its lines are rows.
// A fence indented past a code block's (a list item's code) keeps its
// block one piece, verbatim.
func TestSplitTextEdges(t *testing.T) {
	stray := "1. Extract it.\n\n     ```\n    $ tar zxf x.tar.gz\n    ```\n2. Build it."
	if ps, _ := splitText(stray); len(ps) != 3 || ps[1].kind != kindExample || ps[1].text != "     ```\n    $ tar zxf x.tar.gz\n    ```" {
		t.Errorf("stray fence: %+v", ps)
	}
	if ps, tbs := splitText("| a | b |\n| c | d |"); len(ps) != 2 || tbs[0].header != "" {
		t.Errorf("headerless table: %+v %+v", ps, tbs)
	}
}

// A definition term opens a piece like a definition list's term: a term
// in bold or code, a type in parentheses or not, then a colon or a dash,
// in a list item (a parameter list) or a paragraph. A plain word before a
// colon, a bold or code word opening a sentence, and a bold sentence are
// none.
func TestDefinitionTerm(t *testing.T) {
	for in, want := range map[string]string{
		"- **autopush**: send a push":                           "autopush",
		"- **name** (*str* *| None*) – the name of the command": "name",
		"- `failmode` — fail safe or secure":                    "failmode",
		"* **context\\_settings** - defaults for the context":   "context\\_settings",
		"**https_timeout:** seconds to wait":                    "https_timeout",
		"`click.option` – attaches an option":                   "click.option",
		"- `pam_duo_permit_sshd` allows sshd to use pam_duo":    "",
		"Before:":                  "",
		"Example: run it twice":    "",
		"- plain words in an item": "",
		"**This is a whole bold sentence.** – then more": "",
	} {
		body := listBody(in)
		if got := definitionTerm(body); got != want {
			t.Errorf("definitionTerm(%q) = %q, want %q", body, got, want)
		}
	}
	if ps, _ := splitText("- **autopush**: send a push\n- plain words in an item\n\n**failmode**: what to do"); ps[0].def != "autopush" || ps[1].def != "" || ps[2].def != "failmode" {
		t.Errorf("definition terms: %q %q %q", ps[0].def, ps[1].def, ps[2].def)
	}
}

// A page's role comes from its path and headings: release notes,
// troubleshooting, FAQ, an API reference leaf, a guide.
func TestPageRole(t *testing.T) {
	root, _ := parseWebRoot(evRoot)
	for _, c := range []struct {
		page    string
		heading string
		want    pageRole
	}{
		{"changelog/", "2.0", roleReleaseNotes},
		{"guide/setup", "Troubleshooting", roleTroubleshooting},
		{"unix-faq", "Can I use it?", roleFAQ},
		{"api/core/router/push", "push", roleReference},
		{"guide/setup", "Install", roleGuide},
	} {
		s := evSection(c.page, 0, "x", []string{"Page", c.heading}, "text")
		if got := pageRoleOf(s, root, true); got != c.want {
			t.Errorf("%s / %s = %s, want %s", c.page, c.heading, got, c.want)
		}
	}
}

// Pieces with the same text, whitespace and case aside, fold into one:
// the copy from the best page role, then the earliest; it lists the
// others, and each of them names it.
func TestFold(t *testing.T) {
	s := sortPool([]docsindex.Section{
		evSection("guide/a", 0, "trouble", []string{"A", "Troubleshooting"}, "The shared sentence names alpha."),
		evSection("guide/b", 0, "b", []string{"B", "Setup"}, "The  shared sentence NAMES alpha."),
		evSection("guide/c", 0, "c", []string{"C", "Setup"}, "The shared sentence names alpha.\n\nUnique text."),
	}, evRoot)
	rep := s.secs[1].pieces[0]
	if s.pieces[rep].into != -1 || !slices.Equal(s.pieces[rep].folded, []int{s.secs[0].pieces[0], s.secs[2].pieces[0]}) {
		t.Fatalf("representative %+v", s.pieces[rep])
	}
	for _, sec := range []int{0, 2} {
		if p := s.pieces[s.secs[sec].pieces[0]]; p.into != rep {
			t.Errorf("section %d's copy folded into %d, want %d", sec, p.into, rep)
		}
	}
	if line := (Result{SameText: foldLine(s, []int{rep}, 1, nil)}).GroupLine(); line != "same text in 2 other sections: Troubleshooting; C > Setup" {
		t.Errorf("fold line = %q", line)
	}
	// A section the answer prints is not named, and the count says so;
	// with none left, there is no line.
	if line := (Result{SameText: foldLine(s, []int{rep}, 1, map[int]bool{1: true, 2: true})}).GroupLine(); line != "same text in 1 other section: Troubleshooting" {
		t.Errorf("fold line, C printed = %q", line)
	}
	if st := foldLine(s, []int{rep}, 1, map[int]bool{0: true, 1: true, 2: true}); st != nil {
		t.Errorf("fold line, all printed = %+v", st)
	}
}

// Through the walk: a cut group whose folded text sits in a section
// printed in the same answer does not name it.
func TestFoldLineSkipsPrinted(t *testing.T) {
	filler := strings.Repeat("Words of no interest at all here. ", 200)
	s := sortPool([]docsindex.Section{
		evSection("p", 0, "a", []string{"P", "One"}, filler+"\n\nThe alpha beta flag."),
		evSection("q", 0, "b", []string{"Q", "Two"}, "The alpha beta flag.\n\nAlpha beta in Q alone."),
	}, evRoot)
	g := evGathered("alpha beta", 1000, map[string]int{"alpha": 2, "beta": 2})
	lib := docsindex.Library{Root: evRoot}
	r := evRanked(s, g, 600, 0)
	printSections(lib, s, r, HintNone)
	res, _ := fitResults(lib, s, r, HintNone, nil)
	if len(res) != 2 || !res[0].Truncated || res[1].Truncated {
		t.Fatalf("want P cut and Q whole: %+v", res)
	}
	if res[0].SameText != nil {
		t.Errorf("P names Q, printed below it: %+v", res[0].SameText)
	}
	if st := foldLine(s, r.plans[0].pieces, 0, nil); st == nil || st.Count != 1 {
		t.Errorf("without Q printed, P names it: %+v", st)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// The page role scales a piece's score: release notes count in full only
// when the question asks about a change.
func TestRoleFactor(t *testing.T) {
	if roleFactor(roleReleaseNotes, false) != 0.6 || roleFactor(roleReleaseNotes, true) != 1 || roleFactor(roleTroubleshooting, false) != 0.8 {
		t.Error("role factors")
	}
	if !asksChange("what changed in 2.0") || asksChange("pam_duo failmode safe") {
		t.Error("asksChange")
	}
}

// evLocal is a local backend over sections stored under evRoot, one page
// each, and the library.
func evLocal(t *testing.T, secs []docsindex.Section) (*Local, docsindex.Library) {
	t.Helper()
	store := newStore(t)
	byPage := map[string][]docsindex.Section{}
	var order []string
	for _, s := range secs {
		if _, ok := byPage[s.Page]; !ok {
			order = append(order, s.Page)
		}
		byPage[s.Page] = append(byPage[s.Page], s)
	}
	var pages []docsindex.PageData
	for _, p := range order {
		pages = append(pages, docsindex.PageData{Page: docsindex.Page{URL: p, Hash: p}, Sections: byPage[p]})
	}
	if err := store.PutPages(evRoot, pages); err != nil {
		t.Fatal(err)
	}
	lib := docsindex.Library{Key: evRoot, Root: evRoot, Format: docsindex.FormatVersion, Sections: len(secs)}
	if err := store.PutLibrary(lib); err != nil {
		t.Fatal(err)
	}
	return NewLocal(store), lib
}

func poolURLs(g *gathered) []string {
	var out []string
	for _, p := range g.pool {
		out = append(out, strings.TrimPrefix(p.Page, evRoot))
	}
	return out
}

// Gather's pool puts the section naming the question's identifier first,
// leaves a later version's section out unless asked, finds an identifier
// by its words, and counts each term's sections in the library.
func TestGatherPool(t *testing.T) {
	l, lib := evLocal(t, []docsindex.Section{
		evSection("a", 0, "", []string{"Overview"}, "General words about the library and requests."),
		evSection("b", 0, "", []string{"Retries"}, "Set `max_retries` to retry failed requests."),
		{Page: evRoot + "c", Heading: "Retries in v5", Crumbs: []string{"Migrating to v5", "Retries in v5"}, Text: "Retry failed requests with max_retries.", CrossVersion: "5"},
		evSection("d", 0, "", []string{"Forms"}, "Call `form.validateField('name')` to check one field."),
	})
	g, err := l.gather(lib, "retry failed requests max_retries")
	if err != nil || len(g.pool) == 0 || poolURLs(g)[0] != "b" || g.pool[0].score != 1 || g.pool[0].via != "question" {
		t.Fatalf("pool = %v %v", poolURLs(g), err)
	}
	if slices.Contains(poolURLs(g), "c") {
		t.Errorf("the later version's section is in the pool: %v", poolURLs(g))
	}
	if g.sections != 4 {
		t.Errorf("library sections = %d", g.sections)
	}
	for _, term := range g.terms {
		if term.term == "maxretry" && (!term.ident || term.sections != 2) {
			t.Errorf("term %+v", term)
		}
	}
}

// An identifier is found by its whole name, its words, or qualified.
func TestGatherIdentifier(t *testing.T) {
	l, lib := evLocal(t, []docsindex.Section{
		evSection("a", 0, "", []string{"Overview"}, "General words about the library and fields."),
		evSection("d", 0, "", []string{"Forms"}, "Call `form.validateField('name')` to check one field."),
	})
	for _, q := range []string{"validateField", "validate field", "form.validateField"} {
		if g, err := l.gather(lib, q); err != nil || len(g.pool) == 0 || poolURLs(g)[0] != "d" {
			t.Errorf("%q: pool = %v %v", q, poolURLs(g), err)
		}
	}
}

// The whole question's list ranks a guide ahead of an API reference leaf
// naming the same words, and question filler matches nothing.
func TestGatherPriors(t *testing.T) {
	l, lib := evLocal(t, []docsindex.Section{
		evSection("api/core/router/guard", 0, "", []string{"guard"}, "Route guard: a function a route runs before navigation."),
		evSection("guide/route-guards", 0, "", []string{"Route guards"}, "Add a route guard to run a function before navigation."),
		evSection("guide/install", 0, "", []string{"Install"}, "Use the installer you want."),
	})
	g, err := l.gather(lib, "how do I use a route guard")
	if err != nil || len(g.pool) == 0 || poolURLs(g)[0] != "guide/route-guards" {
		t.Fatalf("pool = %v %v", poolURLs(g), err)
	}
	if slices.Contains(poolURLs(g), "guide/install") {
		t.Errorf("a section naming only the question's filler is in the pool: %v", poolURLs(g))
	}
}

// The pool is the union of the whole question's best sections and each
// term's own: a section naming only a term the whole question's list has
// no room for still enters, by that term's list.
func TestGatherTermLists(t *testing.T) {
	var secs []docsindex.Section
	for i := range gatherQuestion + 20 {
		secs = append(secs, evSection(fmt.Sprintf("both/%d", i), 0, "", []string{fmt.Sprintf("Both %d", i)}, "Alpha beta alpha beta, alpha and beta again."))
	}
	filler := strings.Repeat(" Many other words of no interest to anyone.", 20)
	for i := range 90 {
		secs = append(secs, evSection(fmt.Sprintf("gamma/%d", i), 0, "", []string{fmt.Sprintf("Other %d", i)}, "This long section names gamma once."+filler))
	}
	l, lib := evLocal(t, secs)
	g, err := l.gather(lib, "alpha beta gamma")
	if err != nil {
		t.Fatal(err)
	}
	vias := map[string]int{}
	for i, p := range g.pool {
		vias[p.via]++
		if i < gatherQuestion && (p.via != "question" || !strings.HasPrefix(poolURLs(g)[i], "both/")) {
			t.Errorf("pool[%d] = %s via %s", i, poolURLs(g)[i], p.via)
		}
	}
	if vias["question"] != gatherQuestion || vias["gamma"] != gatherTerm || len(g.pool) > gatherPool {
		t.Errorf("pool %d sections by list %v", len(g.pool), vias)
	}
}

// compactSections is a section too large for a small budget: an H2 with
// a long lead, a long prose section and a code example that names the
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

// Within a small budget the answer holds the code block naming the
// question whole, and not the large code block that names none of it; it
// is within the budget by the estimate, frontmatter included, and no fence
// is left open.
func TestEvidenceAnswer(t *testing.T) {
	secs := compactSections()
	for i := range secs {
		secs[i].Page = evRoot + "forms"
	}
	l, lib := evLocal(t, secs)
	const query = "default form action POST"
	a, err := l.evidence(lib, query, LocalOptions{Tokens: 300}, nil)
	if err != nil || len(a.results) == 0 {
		t.Fatalf("results = %v %v", a, err)
	}
	all, total := "", printedTokens(lib, query, a, LocalOptions{})
	for _, r := range a.results {
		all += r.Snippet + "\n"
	}
	if !strings.Contains(all, "```js\nexport const actions") || strings.Contains(all, "noise") {
		t.Errorf("answer:\n%s", all)
	}
	if strings.Count(all, "```")%2 != 0 || total > 300 {
		t.Errorf("open fence or %d tokens over the budget:\n%s", total, all)
	}
}

// A generous budget prints every section the question matches whole, in
// the order their best pieces come, and nothing is passed over.
func TestEvidenceGenerousBudget(t *testing.T) {
	l, lib := evLocal(t, compactSectionsUnder(evRoot+"forms"))
	const query = "default form action POST"
	tr := NewTrace(query)
	tr.ranking(lib, LocalOptions{Tokens: 100000})
	a, err := l.evidence(lib, query, LocalOptions{Tokens: 100000}, tr)
	if err != nil || len(a.results) != 2 || tr.Rank.Stop.Reason != "end" {
		t.Fatalf("a generous budget: %+v %v stop %+v", a, err, tr.Rank.Stop)
	}
	// Every section prints whole but one longer than a page, which shows a
	// page of its best pieces and says how much of it that is.
	for _, r := range a.results {
		if r.Truncated != overPage(r.SectionTokens) || r.Truncated && (r.ShownTokens > answerPageTokens || r.ShownTokens <= 0) {
			t.Errorf("a generous budget: truncated=%v, %d of %d tokens:\n%s", r.Truncated, r.ShownTokens, r.SectionTokens, r.Snippet)
		}
	}
	if !a.results[0].Truncated && tr.Rank.Sections[0].Printed != "whole" {
		t.Errorf("first section printed %q", tr.Rank.Sections[0].Printed)
	}
}

// A section longer than a page is never printed whole, whatever the
// budget: it opens cut and takes its best pieces at once up to a page,
// in page order; one at or under a page prints whole when it fits. The
// source line of a cut group says how much of its section it shows.
func TestEvidencePageCap(t *testing.T) {
	long := []string{}
	for i := range 40 {
		long = append(long, fmt.Sprintf("Paragraph %d says the alpha option matters, with more words around it to count.", i))
	}
	secs := []docsindex.Section{
		evSection("long", 0, "long", []string{"Long", "Alpha"}, strings.Join(long, "\n\n")),
		evSection("short", 0, "short", []string{"Short", "Alpha"}, "The alpha option, in short."),
	}
	l, lib := evLocal(t, secs)
	tr := NewTrace("alpha option")
	tr.ranking(lib, LocalOptions{Tokens: 100000})
	a, err := l.evidence(lib, "alpha option", LocalOptions{Tokens: 100000}, tr)
	if err != nil || len(a.results) != 2 {
		t.Fatalf("results = %+v %v", a, err)
	}
	cut, whole := cutAndWhole(a.results)
	if cut == nil || whole == nil || !strings.HasSuffix(cut.URL, "#long") || cut.SectionTokens <= answerPageTokens ||
		cut.ShownTokens > answerPageTokens || cut.ShownTokens < answerPageTokens/2 || cut.ShownTokens != estimateTokens(cut.Snippet) {
		t.Fatalf("cut %+v whole %+v", cut, whole)
	}
	inPageOrder(t, cut.Snippet, 40)
	want := fmt.Sprintf("source: %s (cut, %s of %s tokens)\n", cut.URL, groupDigits(cut.ShownTokens), groupDigits(cut.SectionTokens))
	if g := RenderGroup(*cut, true); !strings.HasSuffix(g, want) {
		t.Errorf("cut group's source line, want %q:\n%s", want, g)
	}
	if g := RenderGroup(*whole, true); !strings.HasSuffix(g, "source: "+whole.URL+"\n") {
		t.Errorf("whole group's source line:\n%s", g)
	}
	if tr.Rank.PageTokens != answerPageTokens {
		t.Errorf("trace page_tokens %d", tr.Rank.PageTokens)
	}
}

// cutAndWhole are the last cut result and the last whole one.
func cutAndWhole(results []Result) (cut, whole *Result) {
	for i := range results {
		if results[i].Truncated {
			cut = &results[i]
		} else {
			whole = &results[i]
		}
	}
	return cut, whole
}

// inPageOrder fails unless the "Paragraph N says" of text, of n, are in
// page order.
func inPageOrder(t *testing.T, text string, n int) {
	t.Helper()
	last := -1
	for i := range n {
		if at := strings.Index(text, fmt.Sprintf("Paragraph %d says", i)); at >= 0 {
			if at < last {
				t.Errorf("paragraph %d out of page order", i)
			}
			last = at
		}
	}
}

// A section longer than a page that is one piece (a code block, never
// cut) prints whole, not marked cut: nothing of it is left out.
func TestEvidencePageOnePiece(t *testing.T) {
	code := "```go\n" + strings.Repeat("alpha := option() // a line of code\n", 80) + "```"
	l, lib := evLocal(t, []docsindex.Section{evSection("code", 0, "code", []string{"Code", "Alpha"}, code)})
	a, err := l.evidence(lib, "alpha option", LocalOptions{Tokens: 100000}, nil)
	if err != nil || len(a.results) != 1 || !overPage(estimateTokens(code)) {
		t.Fatalf("results = %+v %v", a, err)
	}
	if r := a.results[0]; r.Truncated || r.Snippet != code || r.SectionTokens != 0 {
		t.Errorf("one piece over a page: truncated=%v, %d of %d", r.Truncated, r.ShownTokens, r.SectionTokens)
	}
}

// A cut group's size reads with thousands set apart; one with no size
// known is "(cut)".
func TestCutNote(t *testing.T) {
	for _, c := range []struct {
		r    Result
		want string
	}{
		{Result{Truncated: true, ShownTokens: 310, SectionTokens: 1640}, " (cut, 310 of 1,640 tokens)"},
		{Result{Truncated: true, ShownTokens: 1200, SectionTokens: 1234567}, " (cut, 1,200 of 1,234,567 tokens)"},
		{Result{Truncated: true}, " (cut)"},
	} {
		if got := cutNote(c.r); got != c.want {
			t.Errorf("cutNote(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
}

// The default budget applies when none is given, the limit caps the
// results, and the trace records the same answer as without one.
func TestEvidenceDefaults(t *testing.T) {
	l, lib := evLocal(t, compactSectionsUnder(evRoot+"forms"))
	ctx := context.Background()
	o := l.answer(ctx, lib, "default form action POST", LocalOptions{})
	if o.Status != Found || len(o.Results) == 0 {
		t.Fatalf("outcome = %+v", o)
	}
	total := estimateTokens(NewFrontmatter("default form action POST", SearchBackend, "", o).Render() + RenderAnswer(o.Results, HintNone) + DefinedInLines(o.DefinedIn))
	if total > localTokens {
		t.Errorf("default answer %d tokens", total)
	}
	if one := l.answer(ctx, lib, "default form action POST", LocalOptions{Limit: 1}); len(one.Results) != 1 {
		t.Errorf("limit 1: %d results", len(one.Results))
	}
}

func compactSectionsUnder(page string) []docsindex.Section {
	secs := compactSections()
	for i := range secs {
		secs[i].Page = page
	}
	return secs
}

// A paragraph is one piece; one past sortParagraphTokens is cut into its
// sentences, and a label paragraph is none.
func TestSplitParagraphs(t *testing.T) {
	short := "First sentence here. Second sentence here.\nSame paragraph, next line."
	if ps, _ := splitText(short); len(ps) != 1 || ps[0].kind != kindProse || ps[0].text != short {
		t.Errorf("short paragraph: %+v", ps)
	}
	long := strings.TrimSpace(strings.Repeat("This sentence has a few more words in it. ", 20))
	if estimateTokens(long) <= sortParagraphTokens {
		t.Fatalf("fixture: %d tokens", estimateTokens(long))
	}
	if ps, _ := splitText(long); len(ps) != 20 || ps[0].text != "This sentence has a few more words in it." {
		t.Errorf("long paragraph: %d pieces, first %q", len(ps), ps[0].text)
	}
	if ps, _ := splitText("Menu\n\nA real sentence follows."); len(ps) != 1 || ps[0].text != "A real sentence follows." {
		t.Errorf("label: %+v", ps)
	}
}

// Every pool section holds its whole-question score relative to the best
// and its place in the whole question's ordering, the ones only a term's
// list brought too, past the whole question's candidates (scoredBeyond).
func TestGatherScoresEveryPoolSection(t *testing.T) {
	var secs []docsindex.Section
	for i := range gatherQuestion + 20 {
		secs = append(secs, evSection(fmt.Sprintf("both/%d", i), 0, "", []string{fmt.Sprintf("Both %d", i)}, "Alpha beta alpha beta, alpha and beta again."))
	}
	filler := strings.Repeat(" Many other words of no interest to anyone.", 20)
	for i := range 90 {
		secs = append(secs, evSection(fmt.Sprintf("gamma/%d", i), 0, "", []string{fmt.Sprintf("Other %d", i)}, "This long section names gamma once."+filler))
	}
	l, lib := evLocal(t, secs)
	g, err := l.gather(lib, "alpha beta gamma")
	if err != nil {
		t.Fatal(err)
	}
	if g.pool[0].score != 1 {
		t.Errorf("best section's score %v", g.pool[0].score)
	}
	n := 0
	for i, p := range g.pool {
		if p.via == "question" && p.order != i+1 {
			t.Errorf("%s via the question: order %d, want %d", p.Page, p.order, i+1)
		}
		if p.via == "gamma" {
			n++
			if p.score <= 0 || p.score >= 1 || p.order <= gatherQuestion || p.order > g.ordered {
				t.Errorf("%s via gamma: score %v, order %d of %d", p.Page, p.score, p.order, g.ordered)
			}
		}
	}
	if n == 0 {
		t.Errorf("no section came by gamma's list: %v", poolURLs(g))
	}
	all := []docsindex.Hit{{ID: 1, Score: 9}, {ID: 2, Score: 8}, {ID: 4, Score: 3}, {ID: 3, Score: 2}, {ID: 5, Score: 1}}
	hits := [][]docsindex.Hit{all[:2], {{ID: 2}, {ID: 3}}, {{ID: 3}, {ID: 4}, {ID: 1}}}
	if got := scoredBeyond(hits, all); !slices.Equal(got, []docsindex.Hit{{ID: 3, Score: 2}, {ID: 4, Score: 3}}) {
		t.Errorf("scoredBeyond = %v", got)
	}
}

// The contents line names the option rows a group left out and its
// section's direct subsections with their option tables' keys, read from
// the index (not a grandchild, not the next section of the page); a long
// line is cut with "…" and how many it leaves out, within contentsTokens.
func TestContents(t *testing.T) {
	page := "unix"
	l, lib := evLocal(t, []docsindex.Section{
		evSection(page, 0, "config", []string{"Unix", "Config"}, "Edit the file to configure it."),
		evSection(page, 1, "options", []string{"Unix", "Config", "Options"}, "| Key | Description |\n|---|---|\n| `ikey` | The key. |\n| `skey` | The secret. |\n| `failmode` | Fail safe or secure. |"),
		evSection(page, 2, "deep", []string{"Unix", "Config", "Options", "Deep"}, "A deeper section."),
		evSection(page, 3, "proxy", []string{"Unix", "Config", "Proxy"}, "Set a proxy."),
		evSection(page, 4, "other", []string{"Unix", "Other"}, "Something else."),
	})
	stored, err := l.store.PageSections(lib.Key, evRoot+page)
	if err != nil || len(stored) != 5 {
		t.Fatalf("stored %v %v", stored, err)
	}
	s := sortPool([]docsindex.Section{stored[0], stored[1]}, evRoot)
	if err := l.outline(lib, s); err != nil {
		t.Fatal(err)
	}
	if got := contentsLine(sectionKeys(s, 0, nil), s.secs[0].subs); got != "also in this section: Options (options: ikey, skey, failmode); Proxy" {
		t.Errorf("parent's contents = %q", got)
	}
	row := s.secs[1].tables[0].rows[1]
	if got := contentsLine(sectionKeys(s, 1, []int{row}), s.secs[1].subs); got != "also in this section: options: ikey, failmode; Deep" {
		t.Errorf("own rows' contents = %q", got)
	}
	var keys []string
	for i := range 30 {
		keys = append(keys, fmt.Sprintf("option_name_%d", i))
	}
	subs := []subsection{{heading: "First Part"}, {heading: "Second Part", keys: keys}}
	line := contentsLine(keys, subs)
	if estimateTokens(line) > contentsTokens || !strings.HasPrefix(line, "also in this section: options: option_name_0, ") ||
		!strings.Contains(line, "; First Part; Second Part (options: option_name_0") || !strings.Contains(line, ", … +") {
		t.Errorf("long contents (%d tokens) = %q", estimateTokens(line), line)
	}
	if contentsLine(nil, nil) != "" {
		t.Error("contents of nothing")
	}
}

// A host name the question writes is one term: never an identifier, and
// its labels are no terms of their own; the full-text query asks for it
// as a phrase, and a piece writing it, or a host under it, names it.
func TestHostNameTerm(t *testing.T) {
	q := newRankQuery("pam_duo fallback when duosecurity.com cannot be reached")
	if !slices.Contains(q.terms, "duosecurity.com") || slices.Contains(q.terms, "com") || slices.Contains(q.terms, "duosecurity") {
		t.Errorf("terms %v", q.terms)
	}
	if !slices.Equal(q.idents, []string{"pamduo"}) || slices.Contains(q.identKeys, "com") {
		t.Errorf("idents %v keys %v", q.idents, q.identKeys)
	}
	if m := ftsMatch(q.terms, q.identKeys); !strings.Contains(m, `"duosecurity com"`) {
		t.Errorf("match %s", m)
	}
	for w, want := range map[string]bool{"duosecurity.com": true, "docs.python.org": true, "res.json": false, "os.path": false, "form.validatefield": false} {
		if isHostName(w) != want {
			t.Errorf("isHostName(%q) = %v", w, !want)
		}
	}
	if got := hostTerms("Reach api-x1.DuoSecurity.com, or example.org."); !slices.Equal(got, []string{"api-x1.duosecurity.com", "duosecurity.com", "example.org"}) {
		t.Errorf("hostTerms = %v", got)
	}
}

// Gather counts a host-name term's sections by its phrase, and a piece
// writing it, or a host under it, names it.
func TestHostNameGather(t *testing.T) {
	l, lib := evLocal(t, []docsindex.Section{
		evSection("a", 0, "", []string{"Network"}, "Allow outbound HTTPS to api-x1.duosecurity.com from the host."),
		evSection("b", 0, "", []string{"Domains"}, "Any .com domain will do, and com is a word."),
	})
	g, err := l.gather(lib, "duosecurity.com unreachable")
	if err != nil || len(g.terms) != 2 || g.terms[0].term != "duosecurity.com" || g.terms[0].sections != 1 || len(g.pool) != 1 {
		t.Fatalf("terms %+v pool %v %v", g.terms, poolURLs(g), err)
	}
	if !pieceTokens("Allow outbound HTTPS to api-x1.duosecurity.com.")["duosecurity.com"] {
		t.Error("a piece writing the host does not name it")
	}
}

// mergeText is a section of a long paragraph (cut into sentences), a
// short one, a table of three rows and a last paragraph.
var mergeText = "Alpha one. Filler" + strings.Repeat(" word", 130) + ". Alpha three. Alpha four.\n\nMiddle paragraph here.\n\n| Key | Value |\n|---|---|\n| `a` | x |\n| `b` | y |\n| `c` | z |\n\nLast alpha sentence."

// A cut section prints its pieces in page order: neighbours joined by the
// text between them, " … " between sentences of a paragraph and a "…"
// line between blocks where text was skipped, rows of one table under its
// one header with a line counting them, and a contents line naming the
// option rows left out.
func TestMerge(t *testing.T) {
	s := sortPool([]docsindex.Section{evSection("p", 0, "a", []string{"Page", "Sec"}, mergeText)}, evRoot)
	ids := s.secs[0].pieces
	// sentences 0-3, middle 4, rows 5-7, last 8
	lib := docsindex.Library{Root: evRoot}
	r := renderGroup(lib, s, group{sec: 0, pieces: []int{ids[0], ids[2], ids[3], ids[5], ids[7], ids[8]}})
	want := "Alpha one. … Alpha three. Alpha four.\n\n…\n\n| Key | Value |\n|---|---|\n| `a` | x |\n| `c` | z |\n\nLast alpha sentence."
	if r.Snippet != want || r.GroupLine() != "option rows, 2 of 3" || !r.Truncated || r.Breadcrumb != "Page > Sec" || r.URL != evRoot+"p#a" ||
		r.PageTitle != "Page" || !slices.Equal(r.HeadingPath, []string{"Sec"}) || !slices.Equal(r.Rows, []RowCount{{Kind: "option row", Shown: 2, Of: 3}}) {
		t.Errorf("merged:\n%s\nline %q truncated %v title %q path %q", r.Snippet, r.GroupLine(), r.Truncated, r.PageTitle, r.HeadingPath)
	}
	if r.Contents != "also in this section: options: b" || !strings.Contains(RenderSnippet(r), "Last alpha sentence.\n\nalso in this section: options: b\nsource: ") {
		t.Errorf("contents %q:\n%s", r.Contents, RenderSnippet(r))
	}
	// A row that is not the table's first still prints the header.
	r = renderGroup(lib, s, group{sec: 0, pieces: []int{ids[6]}})
	if r.Snippet != "| Key | Value |\n|---|---|\n| `b` | y |" {
		t.Errorf("one row:\n%s", r.Snippet)
	}
	// A whole section is its text, uncut.
	if r = renderGroup(lib, s, group{sec: 0, whole: true}); r.Snippet != mergeText || r.Truncated || r.Contents != "" {
		t.Errorf("whole:\n%s", r.Snippet)
	}
}

// A piece's score is its terms' rarity in the library, an identifier the
// question writes counting double; a term counts once for the piece's
// text and once more for its heading path, so a section about the
// question lifts every piece under it; the page role scales the sum.
func TestScoreParts(t *testing.T) {
	s := sortPool([]docsindex.Section{
		evSection("unix", 0, "failmode", []string{"Duo Unix (pam_duo)", "Failmode"}, "Set failmode to secure.\n\nRestart the daemon."),
		evSection("unix-faq", 0, "q", []string{"FAQ", "Question"}, "Use failmode secure."),
	}, evRoot)
	g := evGathered("pam_duo failmode secure", 10000, map[string]int{"pam": 71, "duo": 9900, "failmod": 74, "secur": 900, "pamduo": 48})
	r := evRanked(s, g, 1000, 0)
	w := map[string]float64{}
	for i, term := range g.terms {
		w[term.term] = r.weights[i]
	}
	if want := 2 * termWeight(48, 10000); !near(w["pamduo"], want) || termWeight(9900, 10000) > 0.05 {
		t.Fatalf("weights = %v", w)
	}
	path := w["pam"] + w["duo"] + w["pamduo"] + w["failmod"]
	for _, c := range []struct {
		name     string
		got      scored
		sum, mul float64
	}{
		{"text and heading path", r.scores[s.secs[0].pieces[0]], w["failmod"] + w["secur"] + path, 1},
		{"heading path only", r.scores[s.secs[0].pieces[1]], path, 1},
		{"FAQ", r.scores[s.secs[1].pieces[0]], w["failmod"] + w["secur"], 0.9},
	} {
		if !near(c.got.sum, c.sum) || !near(c.got.score, c.mul*c.sum) || c.got.role != c.mul {
			t.Errorf("%s: score %v (sum %v), want %v (%v) %+v", c.name, c.got.score, c.got.sum, c.mul*c.sum, c.sum, c.got.hits)
		}
	}
	if h := r.scores[s.secs[0].pieces[0]].hits; len(h) != 5 || h[2].count != 2 || !h[2].inText || !h[2].inPath {
		t.Errorf("hits = %+v", h)
	}
}

// Order: pieces rank densely by score (tied scores share a rank, the next
// score ranks one lower), and the walk takes them by rank fusion of that
// rank and their section's rank in Gather. A piece scoring a little less
// in the section Gather ranks first comes before the best-scoring piece
// of a section Gather ranks far down; a tie goes to the section Gather
// ranks higher; a section the whole question's ordering leaves out ranks
// after every one it holds.
func TestFusedOrder(t *testing.T) {
	s := sortPool([]docsindex.Section{
		evSection("a", 0, "a", []string{"A", "One"}, "Alpha beta here."),
		evSection("b", 0, "b", []string{"B", "Two"}, "Alpha beta gamma here."),
		evSection("c", 0, "c", []string{"C", "Three"}, "Alpha beta here too."),
		evSection("d", 0, "d", []string{"D", "Four"}, "Only alpha."),
	}, evRoot)
	s.secs[0].order, s.secs[1].order, s.secs[2].order, s.secs[3].order = 1, 30, sectionOrder(0, 40), 2
	g := evGathered("alpha beta gamma", 1000, map[string]int{"alpha": 5, "beta": 5, "gamma": 50})
	r := evRanked(s, g, 1000, 0)
	a, b, c, d := s.secs[0].pieces[0], s.secs[1].pieces[0], s.secs[2].pieces[0], s.secs[3].pieces[0]
	if r.scores[b].rank != 1 || r.scores[a].rank != 2 || r.scores[c].rank != 2 || r.scores[d].rank != 3 {
		t.Fatalf("ranks: a %d, b %d, c %d, d %d", r.scores[a].rank, r.scores[b].rank, r.scores[c].rank, r.scores[d].rank)
	}
	if want := 1.0/(rankFusionK+2) + 1.0/(rankFusionK+1); !near(r.fused[a], want) {
		t.Errorf("fused = %v, want %v", r.fused[a], want)
	}
	if !slices.Equal(r.walk, []int{a, d, b, c}) {
		t.Errorf("walk = %v, want %v", r.walk, []int{a, d, b, c})
	}
	if sectionOrder(7, 40) != 7 || sectionOrder(0, 40) != 41 {
		t.Error("sectionOrder")
	}
}

// walkSections is a short section, a long one whose best piece is its
// long second paragraph, and another short one, all naming alpha.
func walkSections() []docsindex.Section {
	filler := strings.Repeat("Words of no interest at all here. ", 40)
	return []docsindex.Section{
		evSection("a", 0, "a", []string{"A", "Short"}, "The alpha flag sets it."),
		evSection("b", 0, "b", []string{"B", "Long"}, filler+"\n\nThe alpha beta flag"+strings.Repeat(", in full", 30)+".\n\n"+filler+"\n\nAlpha once more."),
		evSection("c", 0, "c", []string{"C", "Other"}, "Alpha in another short section."),
	}
}

// The walk prints a section whole when it fits the budget left and cuts
// one that does not to its pieces in the walk's order; a later piece of a
// cut section joins it; the plans cost what they render to, within the
// budget.
func TestWalkWholeOrCut(t *testing.T) {
	s := sortPool(walkSections(), evRoot)
	g := evGathered("alpha beta", 1000, map[string]int{"alpha": 3, "beta": 1})
	lib := docsindex.Library{Root: evRoot}
	r := evRanked(s, g, 400, 0)
	printSections(lib, s, r, HintNone)
	if len(r.plans) != 3 || !r.plans[0].whole || r.plans[1].whole || !r.plans[2].whole {
		t.Fatalf("plans = %+v", r.plans)
	}
	long := r.plans[1]
	if long.sec != 1 || len(long.pieces) != 2 || !strings.HasPrefix(s.pieces[long.pieces[0]].text, "The alpha beta flag, in full") || s.pieces[long.pieces[1]].text != "Alpha once more." {
		t.Errorf("cut section's pieces: %+v", long.pieces)
	}
	checkRendered(t, lib, s, r)
}

// A piece that does not fit is passed over and the walk goes on: a
// budget too small for the long section's best piece lets its smaller
// piece open it.
func TestWalkPassesOver(t *testing.T) {
	s := sortPool(walkSections(), evRoot)
	g := evGathered("alpha beta", 1000, map[string]int{"alpha": 3, "beta": 1})
	lib := docsindex.Library{Root: evRoot}
	r := evRanked(s, g, 0, 0)
	pp := &planner{lib: lib, s: s, r: r, pages: map[string]bool{}}
	short := pp.render(plan{sec: 0, whole: true}) + pp.overhead(plan{sec: 0, whole: true})
	r.budget = 2*short + 8 // the cut group's source line says its size
	printSections(lib, s, r, HintNone)
	if len(r.plans) != 2 || r.plans[1].sec != 1 || len(r.plans[1].pieces) != 1 || s.pieces[r.plans[1].pieces[0]].text != "Alpha once more." || r.stop != "budget" {
		t.Errorf("tight budget: plans %+v passed %v stop %q", r.plans, r.passed, r.stop)
	}
}

// checkRendered checks that r's plans cost what they render to (the
// answer's estimate as one text runs a few tokens per group under the
// groups' sum), within its budget, with nothing dropped.
func checkRendered(t *testing.T, lib docsindex.Library, s *sorted, r *ranked) {
	t.Helper()
	used := 0
	for _, pl := range r.plans {
		used += pl.cost
	}
	res, _ := fitResults(lib, s, r, HintNone, nil)
	total := estimateTokens(RenderAnswer(res, HintNone))
	if used != r.used || total > used || used-total > 4*len(r.plans) || used > r.budget || len(r.dropped) != 0 {
		t.Errorf("used %d (%d), rendered %d, dropped %d", used, r.used, total, len(r.dropped))
	}
}

// The first section always prints something, so an answer is never empty
// for want of room; --limit caps the sections.
func TestWalkFirstAndLimit(t *testing.T) {
	s := sortPool(walkSections(), evRoot)
	g := evGathered("alpha beta", 1000, map[string]int{"alpha": 3, "beta": 1})
	lib := docsindex.Library{Root: evRoot}
	r := evRanked(s, g, 1, 0)
	printSections(lib, s, r, HintNone)
	if len(r.plans) != 1 || r.plans[0].sec != 0 {
		t.Errorf("tiny budget: plans %+v", r.plans)
	}
	r = evRanked(s, g, 10000, 1)
	printSections(lib, s, r, HintNone)
	if len(r.plans) != 1 || r.passed[1] != "limit" || r.passed[2] != "limit" {
		t.Errorf("limit 1: plans %+v passed %v", r.plans, r.passed)
	}
}

// A folded piece prints once: a section cut to a piece whose copy was
// printed already gains nothing from it.
func TestWalkFoldedOnce(t *testing.T) {
	filler := strings.Repeat("Words of no interest at all here. ", 40)
	s := sortPool([]docsindex.Section{
		evSection("p", 0, "a", []string{"P", "One"}, "The alpha flag."),
		evSection("q", 0, "b", []string{"Q", "Two"}, filler+"\n\nThe alpha flag.\n\nThe alpha flag, again."),
	}, evRoot)
	g := evGathered("alpha", 1000, map[string]int{"alpha": 2})
	lib := docsindex.Library{Root: evRoot}
	r := evRanked(s, g, 200, 0)
	printSections(lib, s, r, HintNone)
	if len(r.plans) != 2 || !r.plans[0].whole || len(r.plans[1].pieces) != 1 || s.pieces[r.plans[1].pieces[0]].text != "The alpha flag, again." {
		t.Errorf("plans = %+v", r.plans)
	}
}

// A quote is cut into its paragraphs, nested quotes too; a quoted code
// block stays whole in one example; an admonition is one note.
func TestSplitQuote(t *testing.T) {
	text := "> > none\n> >\n> > The server MUST NOT display a page.\n> >\n> > login\n\n> Run:\n> ```\n> a\n>\n> b\n> ```\n>\n> After."
	ps, _ := splitText(text)
	var got []string
	for _, p := range ps {
		got = append(got, fmt.Sprintf("%s:%s", p.kind, p.text))
	}
	want := []string{"prose:> > none", "prose:> > The server MUST NOT display a page.", "prose:> > login", "example:> Run:\n> ```\n> a\n>\n> b\n> ```", "prose:> After."}
	if !slices.Equal(got, want) {
		t.Errorf("quote pieces:\n got %q\nwant %q", got, want)
	}
	if ps, _ := splitText("> **Note:** one.\n>\n> two."); len(ps) != 1 || ps[0].kind != kindNote {
		t.Errorf("admonition: %+v", ps)
	}
}

// A long block of one entry per line (an index of names) is cut into its
// lines; a long paragraph whose lines end sentences is cut into sentences.
func TestSplitEntryLines(t *testing.T) {
	var names []string
	for i := range 80 {
		names = append(names, fmt.Sprintf("directive_name_%d", i))
	}
	block := strings.Join(names, "\n")
	if ps, _ := splitText(block); len(ps) != 80 || ps[3].text != "directive_name_3" {
		t.Errorf("entry block: %d pieces", len(ps))
	}
	prose := strings.Repeat("This sentence has a few more words in it,\nand it ends here.\n", 12)
	if ps, _ := splitText(prose); len(ps) != 12 || ps[0].text != "This sentence has a few more words in it,\nand it ends here." {
		t.Errorf("wrapped prose: %d pieces, first %q", len(ps), ps[0].text)
	}
	if entryLines("a\nb") != nil || entryLines("one\ntwo.\nthree") != nil {
		t.Error("entryLines")
	}
}

// printedTokens is what a's answer to query from lib prints within opts:
// its frontmatter, its body and its defined-in lines.
func printedTokens(lib docsindex.Library, query string, a *evidenceAnswer, opts LocalOptions) int {
	return estimateTokens(a.frontmatter(lib, query, opts.note).Render() + RenderAnswer(a.results, opts.Hint) + DefinedInLines(a.definedIn))
}

// One page, one address, in the pool: a page the library stores at two
// addresses (x and x.html, a trailing slash, x/index, an llms/ mirror
// below the root, www.) is one page, though its copies' text differ in
// a link (two fetches, two spellings): its sections print once, at the
// address the library lists first, under one page title; sections of one
// page that share an anchor but not a heading stay apart.
func TestGatherOneAddress(t *testing.T) {
	text := "Mock a module with the zorblat [API](/api/vi), and its factory."
	copyText := "Mock a module with the zorblat [API](/api/vi.html), and its factory."
	for _, second := range []string{"guide/modules.html", "guide/modules/", "llms/guide/modules", "guide/modules/index"} {
		first := "guide/modules"
		if second == "guide/modules/index" {
			first = "guide/modules/"
		}
		secs := []docsindex.Section{
			evSection(first, 0, "mocking", []string{"Mocking Modules", "Mocking a Module"}, text),
			evSection(first, 1, "browser", []string{"Mocking Modules", "Browser Mode"}, "In browser mode zorblat works [too](/x)."),
			evSection(second, 0, "mocking", []string{"Mocking Modules", "Mocking a Module"}, copyText),
			evSection(second, 1, "browser", []string{"Mocking Modules", "Browser Mode"}, "In browser mode zorblat works [too](/x.html)."),
			evSection("other", 0, "content", []string{"Other", "One"}, "Zorblat elsewhere, one."),
			evSection("other", 1, "content", []string{"Other", "Two"}, "Zorblat elsewhere, two."),
		}
		l, lib := evLocal(t, secs)
		a, err := l.evidence(lib, "zorblat module", LocalOptions{Tokens: 100000}, nil)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		titles := 0
		for _, r := range a.results {
			key := r.PageTitle + "\x00" + strings.Join(r.HeadingPath, ">")
			if seen[key] {
				t.Errorf("%s: %s printed twice", second, key)
			}
			seen[key] = true
			if r.PageTitle == "Mocking Modules" && !strings.HasPrefix(r.URL, evRoot+first+"#") {
				t.Errorf("%s: printed at %s, not at the address listed first", second, r.URL)
			}
		}
		for _, line := range strings.Split(RenderAnswer(a.results, HintNone), "\n") {
			if line == "## Mocking Modules" {
				titles++
			}
		}
		if titles != 1 || !seen["Other\x00One"] || !seen["Other\x00Two"] {
			t.Errorf("%s: page titles %d, results %v", second, titles, seen)
		}
	}
	// www. and the scheme are one host.
	secs := []docsindex.Section{
		evSection("guide/a", 0, "x", []string{"A", "X"}, "Zorblat once."),
		{Page: strings.Replace(evRoot, "https://", "http://www.", 1) + "guide/a", Anchor: "x", Heading: "X", Crumbs: []string{"A", "X"}, Text: "Zorblat [once](/o.html)."},
	}
	l, lib := evLocal(t, secs)
	if a, err := l.evidence(lib, "zorblat", LocalOptions{Tokens: 100000}, nil); err != nil || len(a.results) != 1 {
		t.Errorf("www. copy: %+v %v", a, err)
	}
}
