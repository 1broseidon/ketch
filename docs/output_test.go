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

// The frontmatter's fields print in one order: query, backend (only one
// other than search), library, scope, version, index, read, skipped,
// widened, not_on_pages, not_in_index, outside_scope, origin, stale,
// defined_in, indexed_near, note. No status, reason, terms or source line
// is printed: the frontmatter says what was read, never a verdict.
func TestFrontmatterOrder(t *testing.T) {
	f := Frontmatter{Query: "pam_duo failmode", Backend: "context7", Library: "https://duo.com/docs/", Scope: "https://duo.com/docs/unix/", Version: "2.1",
		Index:      &IndexInfo{Kind: IndexFull, Pages: 734, Built: "2026-11-10"},
		Read:       []PageRead{{URL: "https://duo.com/docs/a", Rank: 1}, {URL: "https://duo.com/docs/b", Via: "the library URL"}, {URL: "https://duo.com/docs/c"}},
		Skipped:    []PageSkipped{{URL: "https://duo.com/docs/d", Cause: "not found, HTTP 404"}},
		Widened:    &Widening{Pages: 4, Root: "https://duo.com/docs/", Terms: []string{"pam_sss", "x:y"}},
		NotOnPages: []string{"pam_sss"}, NotInIndex: []string{"x:y"}, OutsideScope: []string{"c.req"},
		Origin: "verified, x", Stale: "unverified, built 2026-11-01", DefinedIn: []DefinedIn{{Term: "t", Section: "u#a"}},
		IndexedNear: []NearLibrary{{Title: "Duo", Library: "https://duo.com/docs/"}}, Note: "a note"}
	want := "---\nquery: pam_duo failmode\nbackend: context7\nlibrary: https://duo.com/docs/\nscope: https://duo.com/docs/unix/\nversion: 2.1\n" +
		"index: full, 734 pages, built 2026-11-10\n" +
		"read:\n  - https://duo.com/docs/a (search rank 1)\n  - https://duo.com/docs/b (the library URL)\n  - https://duo.com/docs/c\n" +
		"skipped:\n  - https://duo.com/docs/d (not found, HTTP 404)\n" +
		"widened: read 4 more pages of https://duo.com/docs/ for: pam_sss, x:y\n" +
		"not_on_pages: [pam_sss]\nnot_in_index: [\"x:y\"]\noutside_scope: [c.req]\norigin: verified, x\nstale: unverified, built 2026-11-01\n" +
		"defined_in: {t: u#a}\nindexed_near: {Duo: https://duo.com/docs/}\nnote: a note\n---\n"
	if got := f.Render(); got != want {
		t.Errorf("frontmatter:\n%s\nwant:\n%s", got, want)
	}
	for _, bad := range []string{"status", "reason", "terms", "not_found", "source"} {
		if strings.Contains(f.Render(), "\n"+bad+":") {
			t.Errorf("frontmatter prints %s:\n%s", bad, f.Render())
		}
	}
	// A widening with no exact page read stands in their place.
	lists := Frontmatter{Widened: &Widening{Pages: 1, Root: "https://duo.com/docs/"}}
	if got := lists.Render(); got != "---\nwidened: read 1 page of https://duo.com/docs/, as no search hit on it could be read\n---\n" {
		t.Errorf("widened in place of the exact pages:\n%s", got)
	}
	// Empty lists print as [] when set; nil prints nothing.
	if got := (Frontmatter{Query: "q", NotOnPages: []string{}}).Render(); got != "---\nquery: q\nnot_on_pages: []\n---\n" {
		t.Errorf("an empty not_on_pages:\n%s", got)
	}
	if got := (Frontmatter{Query: "q"}).Render(); got != "---\nquery: q\n---\n" {
		t.Errorf("nothing set:\n%s", got)
	}
	capped := indexInfo(docsindex.Library{Pages: 400, Capped: true})
	if capped.Summary() != "capped, 400 pages" {
		t.Errorf("index summary %q", capped.Summary())
	}
	legacy := Frontmatter{Query: "q", Backend: "context7", Library: "/x/y"}
	if got := legacy.Render(); got != "---\nquery: q\nbackend: context7\nlibrary: /x/y\n---\n" {
		t.Errorf("legacy:\n%s", got)
	}
}

// NewFrontmatter names what a source lacks by where it looked: an indexed
// answer's not_in_index, an answer from pages read not_on_pages (always
// set, [] for none); neither for a results-only answer. backend is printed
// only for one other than search.
func TestNewFrontmatterNotFoundKey(t *testing.T) {
	for _, c := range []struct {
		name            string
		o               Outcome
		onPages, inIdx  []string
		backend, origin string
	}{
		{"indexed", Outcome{Index: &IndexInfo{Kind: IndexFull}, NotFound: []string{"a"}}, nil, []string{"a"}, "", ""},
		{"indexed, all found", Outcome{Index: &IndexInfo{Kind: IndexFull}}, nil, []string{}, "", ""},
		{"pages read", Outcome{Read: []PageRead{{URL: "https://x.test/a", Rank: 1}}, NotFound: []string{"b", "max_conns", "hang", "--force-with-lease", "2.7", "useEffect"}}, []string{"max_conns", "--force-with-lease", "useEffect"}, nil, "", ""},
		{"pages read, all found", Outcome{Read: []PageRead{{URL: "https://x.test/a", Rank: 1}}}, []string{}, nil, "", ""},
		{"widened only", Outcome{Widened: &Widening{Pages: 2, Root: "https://x.test/"}}, []string{}, nil, "", ""},
		{"results only", Outcome{OtherResults: []OtherResult{{Title: "T", URL: "https://x.test/t", Rank: 1}}}, nil, nil, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := NewFrontmatter("q", SearchBackend, "", c.o)
			if !slices.Equal(f.NotOnPages, c.onPages) || (f.NotOnPages == nil) != (c.onPages == nil) ||
				!slices.Equal(f.NotInIndex, c.inIdx) || (f.NotInIndex == nil) != (c.inIdx == nil) || f.Backend != "" {
				t.Errorf("not_on_pages %#v not_in_index %#v backend %q", f.NotOnPages, f.NotInIndex, f.Backend)
			}
		})
	}
	if f := NewFrontmatter("q", "context7", "", Outcome{}); f.Backend != "context7" {
		t.Errorf("context7 backend %q", f.Backend)
	}
}

// The origin line prints only when it says something: verified, with its
// evidence; unverified only for a request that named a library.
func TestShownOrigin(t *testing.T) {
	read := []PageRead{{URL: "https://x.test/a", Rank: 1}}
	for _, c := range []struct {
		origin, named, want string
	}{
		{"verified, the search results name it", "", "verified, the search results name it"},
		{"verified, x", "lib", "verified, x"},
		{"unverified, ketch could not confirm x.test", "", ""},
		{"unverified, ketch could not confirm x.test", "lib", "unverified, ketch could not confirm x.test"},
		{"", "lib", ""},
	} {
		o := Outcome{Origin: c.origin, named: c.named, Read: read}
		if got := ShownOrigin(o); got != c.want {
			t.Errorf("ShownOrigin(%q, named %q) = %q, want %q", c.origin, c.named, got, c.want)
		}
		fm := NewFrontmatter("q", SearchBackend, c.named, o).Render()
		if strings.Contains(fm, "\norigin:") != (c.want != "") {
			t.Errorf("origin %q, named %q: frontmatter\n%s", c.origin, c.named, fm)
		}
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
	if v := verdict(terms(5, 900, 7), 1000); v.refused || v.partial || len(v.notFound) != 0 {
		t.Errorf("found: %+v", v)
	}
	if v := verdict(terms(5, 0), 1000); v.refused || !v.partial || !slices.Equal(v.notFound, []string{"b"}) {
		t.Errorf("partial: %+v", v)
	}
	if v := verdict(terms(0, 0, 900), 1000); !v.refused || !slices.Equal(v.notFound, []string{"a", "b"}) {
		t.Errorf("refused: %+v", v)
	}
	if v := verdict(terms(900, 950), 1000); v.refused || v.partial {
		t.Errorf("no distinctive term: every term is: %+v", v)
	}
}

// A distinctive word in many sections (26 of 1,000) still answers: no
// refusal for a word the docs hold.
func TestTermsShown(t *testing.T) {
	terms := func(counts ...int) []writtenTerm {
		var out []writtenTerm
		for i, n := range counts {
			out = append(out, writtenTerm{text: string(rune('a' + i)), sections: n})
		}
		markDistinctive(out, 1000)
		return out
	}
	if v := verdict(terms(9, 25, 26, 50), 1000); v.refused || v.partial {
		t.Errorf("every word held: %+v", v)
	}
	if v := verdict(terms(50), 1000); v.refused {
		t.Errorf("a distinctive word in 50 sections still answers: %+v", v)
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
	} {
		o := l.answer(ctx, c.lib, c.query, LocalOptions{})
		refused := c.status == Refused
		if o.Status != c.status || o.Reason != c.reason || !slices.Equal(o.NotFound, c.notFound) || o.Index == nil || o.Index.Kind != c.kind ||
			o.Library != evRoot || (len(o.Results) == 0) != refused || errors.Is(o.Err(), ErrTermsNotFound) != refused {
			t.Errorf("%q: %s/%s not_found %v index %+v, %d results, err %v", c.query, o.Status, o.Reason, o.NotFound, o.Index, len(o.Results), o.Err())
		}
	}
}

// On an answer from pages a search read a common word missing from the
// pages makes nothing partial (a handful of pages makes every word look
// rare there); a rare word missing still is partial and named.
func TestSearchReadTerms(t *testing.T) {
	secs := []docsindex.Section{
		evSection("lists", 0, "", []string{"Lists and non-null"}, "A list field whose items cannot be null wraps the item type in `NonNull`; the list itself can still be null."),
		evSection("scalars", 0, "", []string{"Scalars"}, "Scalars are the leaves of a query."),
		evSection("enums", 0, "", []string{"Enums"}, "Enums restrict a field to a set of values."),
	}
	for i := range 30 {
		secs = append(secs, evSection(fmt.Sprintf("other/%d", i), 0, "", []string{fmt.Sprintf("Other %d", i)}, "Unrelated words about routing and caching."))
	}
	l, lib := evLocal(t, secs)
	ctx := context.Background()
	q := "define a list field whose items cannot be null but the list itself can with NonNull"
	if read := l.answer(ctx, lib, q, LocalOptions{live: true}); read.Status != Found || len(read.NotFound) != 0 || read.Index != nil {
		t.Errorf("pages read: %s not_on_pages %v index %+v, want found with no index line", read.Status, read.NotFound, read.Index)
	}
	if o := l.answer(ctx, lib, "list field whose items cannot be null at midnight with NonNull", LocalOptions{live: true}); o.Status != Partial || !slices.Equal(o.NotFound, []string{"midnight"}) {
		t.Errorf("a rare word missing: %s not_found %v", o.Status, o.NotFound)
	}
	if o := l.answer(ctx, lib, "list field items cannot be null anyway, NonNull", LocalOptions{live: true}); o.Status != Found || len(o.NotFound) != 0 {
		t.Errorf("a common word missing: %s not_found %v", o.Status, o.NotFound)
	}
	for w, want := range map[string]bool{"whose": true, "cannot": true, "itself": true, "add": true, "per": true, "message": true, "else": true,
		"start": true, "stored": true, "was": true, "hobby": false, "autosquash": false, "weekday": false, "GSM-7": false, "9am": false} {
		if commonTerm(w) != want {
			t.Errorf("commonTerm(%q) = %v", w, !want)
		}
	}
}

// A number the question writes matches the same number in the docs with
// its digits grouped or not, either way round: it counts for the term's
// sections (found, not partial) and a printed piece writing it names it.
// A decimal, version, date or address is no grouped number.
func TestNumberTerms(t *testing.T) {
	var secs []docsindex.Section
	for i := range 30 {
		secs = append(secs, evSection(fmt.Sprintf("other/%d", i), 0, "", []string{fmt.Sprintf("Other %d", i)}, "Unrelated words about routing and caching."))
	}
	ctx := context.Background()
	for _, c := range []struct{ docs, query string }{
		{"Analytics events: 10,000 included each month on the hobby plan.", "hobby plan includes 10000 analytics events"},
		{"Analytics events: 10 000 included each month on the hobby plan.", "hobby plan includes 10000 analytics events"},
		{"Analytics events: 10_000 included each month on the hobby plan.", "hobby plan includes 10000 analytics events"},
		{"Analytics events: 10000 included each month on the hobby plan.", "hobby plan includes 10,000 analytics events"},
		{"Analytics events: 1,000 included each month on the hobby plan.", "hobby plan includes 1000 analytics events"},
	} {
		l, lib := evLocal(t, append([]docsindex.Section{evSection("limits", 0, "", []string{"Limits"}, c.docs)}, secs...))
		o := l.answer(ctx, lib, c.query, LocalOptions{})
		if o.Status != Found || len(o.NotFound) != 0 || len(o.Results) == 0 || !strings.Contains(o.Results[0].Snippet, "included") {
			t.Errorf("%q over %q: %s not_found %v", c.query, c.docs, o.Status, o.NotFound)
		}
		wts := writtenTerms(c.query, evidenceTerms(newRankQuery(c.query)))
		i := slices.IndexFunc(wts, func(wt writtenTerm) bool { return strings.Contains(wt.text, "000") })
		if i < 0 || !namesTerm(pieceTokens(c.docs), wts[i]) {
			t.Errorf("%q: the docs' %q does not name its number: %+v", c.query, c.docs, wts)
		}
	}
	l, lib := evLocal(t, append([]docsindex.Section{evSection("limits", 0, "", []string{"Limits"}, "Analytics events: 100,000 included on the hobby plan.")}, secs...))
	if o := l.answer(ctx, lib, "hobby plan includes 10000 analytics events", LocalOptions{}); o.Status != Partial || !slices.Equal(o.NotFound, []string{"10000"}) {
		t.Errorf("100,000 is not 10000: %s not_found %v", o.Status, o.NotFound)
	}
}

// A decimal, version, date or address is no grouped number; a grouped
// number in the question is one term, its digits.
func TestNumberForms(t *testing.T) {
	for _, w := range []string{"3.14", "1.2.3", "2024-01-01", "10.0.0.1", "0100", "123", "10,00", "1,0000", "10,000.5"} {
		if n := numberTerm(w); n != "" {
			t.Errorf("numberTerm(%q) = %q, want none", w, n)
		}
	}
	for w, want := range map[string]string{"10000": "10000", "10,000": "10000", "10_000": "10000", "1,234,567": "1234567", "2024": "2024"} {
		if n := numberTerm(w); n != want {
			t.Errorf("numberTerm(%q) = %q, want %q", w, n, want)
		}
	}
	if got := groupedNumbers("v1.10,000, 10,000.5, 3.14, 2024-01-01, 10.0.0.1, 2024 100, 1,000,000 and 10 000."); !slices.Equal(got, []string{"1000000", "10000"}) {
		t.Errorf("groupedNumbers = %v", got)
	}
	if q := newRankQuery("limit of 10,000 events"); !slices.Contains(q.terms, "10000") || slices.Contains(q.terms, "000") || len(q.idents) != 0 {
		t.Errorf("a grouped number is one term: %v idents %v", q.terms, q.idents)
	}
}

// An answer prints a page's groups together under its title and address,
// pages in the order of their best group and groups in page order within
// a page; a group's heading is its path below the title (a lead section
// printed after another group of its page is "(top of page)"); a cut
// group's source says so; one hint line ends an answer with a cut group,
// in the surface's words, and none ends one without.
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
	want := "## Page A\n" + evRoot + "a\n\nLead of A.\n\nsource: " + evRoot + "a\n\n" +
		"### Part > Y\n\nA's Y.\n\nsource: " + evRoot + "a#y" + cut + "\n\n" +
		"## Page B\n" + evRoot + "b\n\n### X\n\nB's X.\n\nsource: " + evRoot + "b#x\n"
	if got := RenderAnswer(Outcome{Results: results}, HintNone); got != want {
		t.Errorf("answer:\n%s\nwant:\n%s", got, want)
	}
	if got := renderPassages(results); got != want {
		t.Errorf("passages:\n%s\nwant:\n%s", got, want)
	}
	for _, style := range []HintStyle{HintCLI, HintMCP} {
		o := Outcome{Results: results}
		got := RenderAnswer(o, style)
		if got != want+"\n"+Hint(style, o)+"\n" || strings.Count(got, "(cut") != 1 {
			t.Errorf("style %d:\n%s", style, got)
		}
		if whole := RenderAnswer(Outcome{Results: []Result{results[0], results[2]}}, style); strings.Contains(whole, "(cut") || strings.Contains(whole, "unread") {
			t.Errorf("no cut group, style %d:\n%s", style, whole)
		}
	}
	// A lead section printed after another group of its page.
	lead := RenderGroup(results[0], false)
	if !strings.HasPrefix(lead, "### "+topOfPage+"\n\nLead of A.") {
		t.Errorf("a lead section not first of its page:\n%s", lead)
	}
	if got := headingBelow(evSection("m", 0, "d", []string{"Module m", "Module m", "Directives"}, "x")); !slices.Equal(got, []string{"Directives"}) {
		t.Errorf("a heading repeating the title: %q", got)
	}
}

// After the groups an answer prints "## Not printed" (the outline, then
// a "+n more" line per page, as a --section call in the surface's words),
// "## Other results", then the hint line; each part only when it has
// something, a blank line between parts.
func TestRenderAnswerLists(t *testing.T) {
	page := "https://x.test/docs/a"
	res := Result{URL: page + "#one", PageTitle: "A", HeadingPath: []string{"One"}, Snippet: "One's text."}
	o := Outcome{
		Results:      []Result{res},
		Outline:      []OutlineEntry{{Heading: "Two > Deep", Section: page + "#deep", Tokens: 1640}, {Heading: "A", Section: page, Tokens: 12}},
		OutlineMore:  []OutlineMore{{Page: page, Count: 7}},
		OtherResults: []OtherResult{{Title: "B", URL: "https://x.test/docs/b", Snippet: "About b.", Rank: 2}, {URL: "https://x.test/docs/c", Rank: 3}},
	}
	body := "## A\n" + page + "\n\n### One\n\nOne's text.\n\nsource: " + page + "#one\n"
	outline := "\n## Not printed\n- Two > Deep — " + page + "#deep (1,640 tokens)\n- A — " + page + " (12 tokens)\n"
	others := "\n## Other results\n- B — https://x.test/docs/b — About b.\n- https://x.test/docs/c — https://x.test/docs/c\n"
	for _, c := range []struct {
		style HintStyle
		more  string
		hint  string
	}{
		{HintNone, "(+7 more: " + page + ")\n", ""},
		{HintCLI, "(+7 more: ketch docs --section " + page + ")\n",
			"\nunread until you read it: a cut or unprinted section with ketch docs --section <url#anchor>; another result with ketch scrape <url>\n"},
		{HintMCP, "(+7 more: docs(section=\"" + page + "\"))\n",
			"\nunread until you read it: a cut or unprinted section with docs(section=\"<url#anchor>\"); another result with scrape(url=\"<url>\")\n"},
	} {
		want := body + outline + c.more + others + c.hint
		if got := RenderAnswer(o, c.style); got != want {
			t.Errorf("style %d:\n%s\nwant:\n%s", c.style, got, want)
		}
	}
	// Results only: no passages, the other results first, no leading blank line.
	only := Outcome{OtherResults: o.OtherResults[:1]}
	if got := RenderAnswer(only, HintCLI); got != "## Other results\n- B — https://x.test/docs/b — About b.\n\nunread until you read it: another result with ketch scrape <url>\n" {
		t.Errorf("results only:\n%s", got)
	}
	if got := RenderAnswer(Outcome{}, HintCLI); got != "" {
		t.Errorf("nothing: %q", got)
	}
}

// The hint line in each surface's words: what is unread (a cut group, an
// outline entry, another result) and how to read it, and the indexed_near
// roots to pass; none with nothing to say or no surface.
func TestAnswerHint(t *testing.T) {
	cut := []Result{{Truncated: true}}
	outline := []OutlineEntry{{Heading: "H", Section: "u#a"}}
	more := []OutlineMore{{Page: "u", Count: 2}}
	others := []OtherResult{{Title: "T", URL: "https://x.test/t"}}
	near := []NearLibrary{{Library: "https://x.test/"}}
	const (
		cliSec = "a cut or unprinted section with ketch docs --section <url#anchor>"
		cliOth = "another result with ketch scrape <url>"
		mcpSec = `a cut or unprinted section with docs(section="<url#anchor>")`
		mcpOth = `another result with scrape(url="<url>")`
	)
	for _, c := range []struct {
		name  string
		style HintStyle
		o     Outcome
		want  string
	}{
		{"cut", HintCLI, Outcome{Results: cut}, "unread until you read it: " + cliSec},
		{"outline", HintCLI, Outcome{Outline: outline}, "unread until you read it: " + cliSec},
		{"outline more", HintCLI, Outcome{OutlineMore: more}, "unread until you read it: " + cliSec},
		{"others", HintCLI, Outcome{OtherResults: others}, "unread until you read it: " + cliOth},
		{"all", HintCLI, Outcome{Results: cut, OtherResults: others, IndexedNear: near},
			"unread until you read it: " + cliSec + "; " + cliOth + "; library not indexed under that name: pass one of the indexed_near roots as --library"},
		{"mcp", HintMCP, Outcome{Results: cut, OtherResults: others}, "unread until you read it: " + mcpSec + "; " + mcpOth},
		{"mcp near", HintMCP, Outcome{IndexedNear: near}, "library not indexed under that name: pass one of the indexed_near roots as library"},
		{"nothing unread", HintCLI, Outcome{Results: []Result{{}}}, ""},
		{"no surface", HintNone, Outcome{Results: cut, OtherResults: others, IndexedNear: near}, ""},
	} {
		if got := Hint(c.style, c.o); got != c.want {
			t.Errorf("%s: Hint = %q, want %q", c.name, got, c.want)
		}
	}
}

// --json carries the frontmatter's fields at the top, each group's parts
// as fields (page title, heading path, rows, same text, contents, section
// and the cut flag), the outline, the other results and the hint: no
// status, reason, terms, not_found or result_count.
func TestAnswerJSONShape(t *testing.T) {
	r := Result{Library: evRoot, Title: "Y", Breadcrumb: "Page A > Part > Y", Snippet: "A's Y.", URL: evRoot + "a#y", Source: SearchBackend,
		Section: evRoot + "a#y", Truncated: true, PageTitle: "Page A", HeadingPath: []string{"Part", "Y"},
		Rows: []RowCount{{Kind: "option row", Shown: 2, Of: 17}}, SameText: &SameText{Count: 1, Sections: []string{"Z"}}, Contents: "also in this section: Q"}
	o := Outcome{Status: Partial, Reason: ReasonTermsNotFound, Results: []Result{r}, Index: &IndexInfo{Kind: IndexFull, Pages: 3, Built: "2026-11-10"},
		NotFound: []string{"pam_sss"}, Outline: []OutlineEntry{{Heading: "Z", Section: evRoot + "a#z", Tokens: 9}},
		OutlineMore: []OutlineMore{{Page: evRoot + "a", Count: 2}}, OtherResults: []OtherResult{{Title: "B", URL: evRoot + "b", Rank: 2}}}
	b, err := json.Marshal(NewAnswerJSON("q", SearchBackend, "", o))
	if err != nil {
		t.Fatal(err)
	}
	top := checkKeys(t, b, []string{"query", "library", "index", "not_in_index", "results", "outline", "outline_more", "other_results", "hint"},
		[]string{"result_count", "status", "reason", "terms", "not_found", "backend", "not_on_pages"})
	var res []json.RawMessage
	_ = json.Unmarshal(top["results"], &res)
	checkKeys(t, res[0], []string{"page_title", "heading_path", "rows", "same_text", "contents", "section", "truncated", "snippet", "url"}, nil)
	var entries []json.RawMessage
	_ = json.Unmarshal(top["outline"], &entries)
	checkKeys(t, entries[0], []string{"heading", "section", "tokens"}, nil)
	if !strings.Contains(string(top["outline_more"]), `"page":`) || !strings.Contains(string(top["outline_more"]), `"count":2`) {
		t.Errorf("outline_more %s", top["outline_more"])
	}
	if !strings.Contains(string(top["other_results"]), `"title":"B"`) || !strings.Contains(string(top["other_results"]), `"rank":2`) {
		t.Errorf("other_results %s", top["other_results"])
	}
	if got := r.GroupLine(); got != "option rows, 2 of 17 · same text in 1 other section: Z" {
		t.Errorf("group line %q", got)
	}
	// An indexed answer with every term found still says not_in_index: [];
	// one from pages read not_on_pages: [], with its read list.
	empty, _ := json.Marshal(NewAnswerJSON("q", SearchBackend, "", Outcome{Status: Found, Index: &IndexInfo{Kind: IndexFull}}))
	if !strings.Contains(string(empty), `"not_in_index":[]`) || strings.Contains(string(empty), "not_on_pages") || !strings.Contains(string(empty), `"results":null`) && !strings.Contains(string(empty), `"results":[]`) {
		t.Errorf("empty lists: %s", empty)
	}
	live, _ := json.Marshal(NewAnswerJSON("q", SearchBackend, "", Outcome{Status: Found, Results: []Result{}, Read: []PageRead{{URL: "https://x.test/a", Rank: 1}},
		Skipped: []PageSkipped{{URL: "https://x.test/b", Cause: "not found"}}, Widened: &Widening{Pages: 2, Root: "https://x.test/", Terms: []string{"t"}}}))
	for _, k := range []string{`"not_on_pages":[]`, `"read":[{"url":"https://x.test/a","rank":1}]`, `"skipped":[{"url":"https://x.test/b","cause":"not found"}]`,
		`"widened":{"pages":2,"root":"https://x.test/","terms":["t"]}`} {
		if !strings.Contains(string(live), k) {
			t.Errorf("no %s in %s", k, live)
		}
	}
	if strings.Contains(string(live), "not_in_index") || strings.Contains(string(live), `"index"`) {
		t.Errorf("an answer from pages read with an index: %s", live)
	}
}

// checkKeys decodes the JSON object b and fails unless it has every key
// of has and none of lacks.
func checkKeys(t *testing.T, b []byte, has, lacks []string) map[string]json.RawMessage {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	for _, k := range has {
		if _, ok := top[k]; !ok {
			t.Errorf("no %q in %s", k, b)
		}
	}
	for _, k := range lacks {
		if _, ok := top[k]; ok {
			t.Errorf("%q in %s", k, b)
		}
	}
	return top
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
