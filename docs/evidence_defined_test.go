package docs

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// definedPool is a pool where failmode is defined twice and only named
// in four other sections: a row's first cell (a), its own heading (b), a
// row's first cell on another page (c), a mention in prose under a
// heading path that names it (d), a first cell that holds it among other
// words (e) and one holding it in an identifier (f).
func definedPool() (*sorted, []writtenTerm) {
	s := sortPool([]docsindex.Section{
		evSection("unix", 0, "options", []string{"Duo Unix", "Options"}, "| Key | Description |\n|---|---|\n| `failmode` | Fail safe or secure. |\n| `https_timeout` | Seconds. |"),
		evSection("unix", 1, "failmode", []string{"Duo Unix", "Failmode"}, "What it does."),
		evSection("proxy", 0, "options", []string{"Proxy", "Options"}, "| Key | Description |\n|---|---|\n| **failmode** (optional) | The proxy's. |"),
		evSection("unix", 2, "examples", []string{"Duo Unix", "Failmode", "Examples"}, "Set failmode in the file."),
		evSection("faq", 0, "q", []string{"FAQ", "Q"}, "| Case | Answer |\n|---|---|\n| When failmode is safe | Access. |"),
		evSection("auth", 0, "keys", []string{"Auth", "Keys"}, "| Key | Description |\n|---|---|\n| `failmode_local` | Other. |"),
	}, evRoot)
	for i := range s.secs {
		s.secs[i].order = i + 1
	}
	wts := writtenTerms("failmode", evidenceTerms(newRankQuery("failmode")))
	wts[0].sections, wts[0].distinctive = 4, true
	return s, wts
}

// A section defines a term when a table row's first cell is the term,
// its markup and a parenthesis after it aside; not a heading, a mention,
// or a cell holding the term among other words or in an identifier.
func TestDefinesTerm(t *testing.T) {
	s, wts := definedPool()
	var got []int
	for i := range s.secs {
		if definesTerm(s, i, wts[0]) {
			got = append(got, i)
		}
	}
	if !slices.Equal(got, []int{0, 2}) {
		t.Errorf("defining sections %v, want [0 2]", got)
	}
	d := definitions(s, wts, 1000)
	if len(d) != 1 || d[0].term != "failmode" || !slices.Equal(d[0].defs, []int{0, 2}) {
		t.Errorf("definitions %+v", d)
	}
	// A term the frontmatter does not list (in a tenth of the sections or
	// more) has no definitions looked up.
	rare := slices.Clone(wts)
	rare[0].sections = 40
	if d := definitions(s, rare, 300); len(d) != 0 {
		t.Errorf("an unlisted term: %+v", d)
	}
}

// A line is printed only when no defining section is, whole or cut; it
// names a defining section on a page printed before one Gather ranks
// higher elsewhere, else the one Gather ranks highest, ties in pool
// order; at most definedMax terms have one.
func TestPickDefined(t *testing.T) {
	s, wts := definedPool()
	defs := definitions(s, wts, 1000)
	if got := pickDefined(s, defs, []plan{{sec: 2, whole: true}}); got[0].target != -1 || !got[0].printed {
		t.Errorf("a defining section printed: %+v", got[0])
	}
	if got := pickDefined(s, defs, []plan{{sec: 0, pieces: []int{s.secs[0].pieces[1]}}}); got[0].target != -1 {
		t.Errorf("a defining section printed cut: %+v", got[0])
	}
	s.secs[0].order, s.secs[2].order = 9, 1
	if got := pickDefined(s, defs, []plan{{sec: 3, whole: true}}); got[0].target != 0 {
		t.Errorf("on the printed page: target %d, want 0", got[0].target)
	}
	s.secs[0].order = 1
	if got := pickDefined(s, defs, nil); got[0].target != 0 {
		t.Errorf("ties in pool order: target %d, want 0", got[0].target)
	}
	s.secs[0].order = 9
	if got := pickDefined(s, defs, nil); got[0].target != 2 {
		t.Errorf("no page printed: target %d, want 2 (Gather's best)", got[0].target)
	}
	many := slices.Repeat(defs, 5)
	n := 0
	for _, d := range pickDefined(s, many, nil) {
		if d.target >= 0 {
			n++
		}
	}
	if n != definedMax {
		t.Errorf("%d lines, want %d", n, definedMax)
	}
	lines := definedLines(docsindex.Library{Root: evRoot}, s, pickDefined(s, defs, nil))
	if len(lines) != 1 || lines[0] != (DefinedIn{Term: "failmode", Section: evRoot + "proxy#options"}) {
		t.Errorf("lines %+v", lines)
	}
}

// defined_in is one frontmatter line after not_found, a flow mapping from
// each term to its section, and nothing follows the body; --json carries
// it as defined_in, and nothing for none.
func TestDefinedInOutput(t *testing.T) {
	defs := []DefinedIn{{Term: "failmode", Section: "https://duo.com/docs/duounix#duo-configuration-options"}, {Term: "x", Section: "u#a"}}
	f := Frontmatter{Query: "q", Backend: SearchBackend, Library: "L", Index: &IndexInfo{Kind: IndexFull, Pages: 2}, Status: Found,
		Terms: []TermCount{{"failmode", 74}}, NotFound: []string{}, DefinedIn: defs}
	want := "---\nquery: q\nbackend: search\nlibrary: L\nindex: full, 2 pages\nstatus: found\nterms: {failmode: 74}\nnot_found: []\n" +
		"defined_in: {failmode: https://duo.com/docs/duounix#duo-configuration-options, x: u#a}\n---\n"
	if got := f.Render(); got != want {
		t.Errorf("frontmatter:\n%s\nwant:\n%s", got, want)
	}
	f.DefinedIn = nil
	if got := f.Render(); strings.Contains(got, "defined") {
		t.Errorf("a line for none:\n%s", got)
	}
	if got := definedInLine([]DefinedIn{{Term: "a:b", Section: "https://x.test/a b#c,d"}}); got != `defined_in: {"a:b": "https://x.test/a b#c,d"}`+"\n" {
		t.Errorf("quoted: %s", got)
	}
	o := Outcome{Status: Found, Index: &IndexInfo{Kind: IndexFull}, DefinedIn: defs[1:]}
	b, _ := json.Marshal(NewFrontmatter("q", SearchBackend, "", o))
	if !strings.Contains(string(b), `"defined_in":[{"term":"x","section":"u#a"}]`) {
		t.Errorf("json: %s", b)
	}
	if b, _ := json.Marshal(NewFrontmatter("q", SearchBackend, "", Outcome{Status: Found, Index: &IndexInfo{Kind: IndexFull}})); strings.Contains(string(b), "defined_in") {
		t.Errorf("json without any: %s", b)
	}
}

// Through the pipeline: the options row's section is passed over for
// sections that take the budget, and the frontmatter names it; the
// answer stays within its budget, and the trace records it.
func TestEvidenceDefinedIn(t *testing.T) {
	filler := strings.Repeat("The zebra service restarts when the zebra daemon is told to, and logs it. ", 12)
	secs := []docsindex.Section{
		evSection("guide", 0, "one", []string{"Guide", "Zebra one"}, "Zebra quokka steps. "+filler),
		evSection("guide", 1, "two", []string{"Guide", "Zebra two"}, "Zebra quokka steps again. "+filler),
		evSection("guide", 2, "three", []string{"Guide", "Zebra three"}, "Zebra quokka steps once more. "+filler),
		evSection("ref", 0, "options", []string{"Reference", "Options"}, "| Key | Description |\n|---|---|\n| `quokka` | What to do when the service is down. |"),
	}
	for i := range 100 {
		secs = append(secs, evSection("other", i, fmt.Sprintf("o%d", i), []string{"Other", "Part"}, "Unrelated words about something else entirely."))
	}
	l, lib := evLocal(t, secs)
	tr := NewTrace("zebra quokka")
	tr.ranking(lib, LocalOptions{Tokens: 300})
	a, err := l.evidence(lib, "zebra quokka", LocalOptions{Tokens: 300}, tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range a.results {
		if strings.HasSuffix(r.URL, "ref#options") {
			t.Skipf("the options section was printed: %s", r.URL)
		}
	}
	if len(a.definedIn) != 1 || a.definedIn[0] != (DefinedIn{Term: "quokka", Section: evRoot + "ref#options"}) {
		t.Fatalf("defined_in %+v", a.definedIn)
	}
	total := estimateTokens(a.frontmatter(lib, "zebra quokka", "").Render() + RenderAnswer(a.results, HintNone))
	if total > 300 {
		t.Errorf("printed %d tokens, budget 300", total)
	}
	if d := tr.Terms.Defined; len(d) != 2 || d[0].Term != "zebra" || d[0].Line != "" || d[1].Line != evRoot+"ref#options" || d[1].Printed {
		t.Errorf("trace %+v", d)
	}
}
