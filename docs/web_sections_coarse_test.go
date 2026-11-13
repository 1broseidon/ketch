package docs

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Coarse sections (web_sections_coarse.go): fixtures written for these
// tests, shaped like a directive reference, a definition-list API page and
// a plain-text specification.

// filler is n sentences of prose, each naming i so no two are alike.
func filler(word string, n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "The %s setting changes how the server handles request number %d when the client is slow. ", word, i)
	}
	return strings.TrimSpace(b.String())
}

// sectionsByAnchor indexes sections by their anchor.
func sectionsByAnchor(secs []webSection) map[string]webSection {
	out := map[string]webSection{}
	for _, s := range secs {
		out[s.anchor] = s
	}
	return out
}

func directivePage() webPage {
	md := "# Module demo\n\nThe demo module serves files.\n\n## Directives\n\n" +
		"[demo_beta](https://x.test/demo.html#demo_beta) is often set with the others.\n\n" +
		"- [demo_alpha](https://x.test/demo.html#demo_alpha)\n- [demo_beta](https://x.test/demo.html#demo_beta)\n\n" +
		"|Syntax:|`demo_alpha size;`|\n|---|---|\n|Default:|`demo_alpha 1m;`|\n\n" + filler("alpha", 40) + "\n\n" +
		"|Syntax:|`demo_alpha_extra on | off;`|\n|---|---|\n|Default:|`demo_alpha_extra off;`|\n\n" + filler("extra", 30) + "\n\n" +
		"|Syntax:|`demo_beta number;`|\n|---|---|\n|Default:|—|\n\n" + filler("beta", 30) + "\n\n" +
		"`reuse`\n\n" + filler("reuse", 10) + "\n"
	return webPage{URL: "https://x.test/demo.html", Title: "Module demo", Markdown: md, Anchors: []PageAnchor{
		{ID: "directives", Text: "Directives"},
		{ID: "reuse", Text: "reuse"}, // a parameter's term inside demo_beta: labelled, the entries are not
		{ID: "menu"}, {ID: "demo_alpha"}, {ID: "demo_alpha_extra"}, {ID: "demo_beta"}, {ID: "footer"},
	}}
}

func TestCoarseSectionSplitsAtEntryAnchors(t *testing.T) {
	secs := splitSections(directivePage(), 0)
	by := sectionsByAnchor(secs)
	for _, id := range []string{"demo_alpha", "demo_alpha_extra", "demo_beta"} {
		s, ok := by[id]
		if !ok {
			t.Fatalf("no section #%s; got %v", id, anchorsOf(secs))
		}
		want := []string{"Module demo", "Directives", id}
		if s.heading != id || !slices.Equal(s.crumbs, want) || s.source != anchorFixed {
			t.Errorf("#%s: heading %q crumbs %v source %v, want %q %v fixed", id, s.heading, s.crumbs, s.source, id, want)
		}
		if !strings.HasPrefix(s.text, "|Syntax:|`"+id+" ") {
			t.Errorf("#%s starts %q", id, s.text[:min(60, len(s.text))])
		}
	}
	if strings.Contains(by["demo_alpha"].text, "demo_alpha_extra") {
		t.Error("demo_alpha took demo_alpha_extra's entry: an id must end where the code's identifier does")
	}
	if _, ok := by["reuse"]; ok {
		t.Error("a labelled term inside an entry split it: a section's entries are marked one way")
	}
	if !strings.Contains(by["demo_beta"].text, "`reuse`") {
		t.Error("demo_beta lost its parameter's text")
	}
	lead, ok := by["directives"]
	if !ok || !strings.Contains(lead.text, "is often set with the others") {
		t.Errorf("the section's lead (its references to the entries) should stay the section itself: %+v", lead)
	}
}

func anchorsOf(secs []webSection) []string {
	var out []string
	for _, s := range secs {
		out = append(out, s.anchor)
	}
	return out
}

func TestCoarseSectionLeftWhole(t *testing.T) {
	page := directivePage()
	cases := []struct {
		name string
		page func(webPage) webPage
	}{
		{"anchors unknown (a Markdown source)", func(p webPage) webPage { p.Anchors = nil; return p }},
		{"no anchor names an entry", func(p webPage) webPage {
			p.Anchors = []PageAnchor{{ID: "directives", Text: "Directives"}, {ID: "menu"}, {ID: "footer"}}
			return p
		}},
		{"one entry only", func(p webPage) webPage {
			p.Anchors = []PageAnchor{{ID: "directives", Text: "Directives"}, {ID: "demo_beta"}}
			return p
		}},
		{"short section", func(p webPage) webPage {
			p.Markdown = "# Module demo\n\n## Directives\n\n|Syntax:|`demo_alpha size;`|\n|---|---|\n\nSets alpha.\n\n|Syntax:|`demo_beta number;`|\n|---|---|\n\nSets beta.\n"
			return p
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, s := range splitSections(c.page(page), 0) {
				if strings.HasPrefix(s.anchor, "demo_") {
					t.Errorf("split at #%s", s.anchor)
				}
			}
		})
	}
}

// definitionPage is a Sphinx-like API page: each member a definition term
// the page labels, its text the term's words.
func definitionPage(members int, body func(i int) string) webPage {
	var b strings.Builder
	b.WriteString("# API\n\n## Application Object\n\n")
	anchors := []PageAnchor{{ID: "application-object", Text: "Application Object"}}
	for i := range members {
		fmt.Fprintf(&b, "member\\_%d(*value*, *timeout=None*)[¶](https://x.test/api/#pkg.App.member_%d \"Link to this definition\")\n\n%s\n\n", i, i, body(i))
		anchors = append(anchors, PageAnchor{ID: fmt.Sprintf("pkg.App.member_%d", i), Text: fmt.Sprintf("member_%d(value, timeout=None)", i)})
	}
	return webPage{URL: "https://x.test/api/", Title: "API", Markdown: b.String(), Anchors: anchors}
}

func TestCoarseSectionSplitsAtDefinitionTerms(t *testing.T) {
	secs := splitSections(definitionPage(12, func(i int) string { return filler(fmt.Sprintf("member %d", i), 9) }), 0)
	by := sectionsByAnchor(secs)
	s, ok := by["pkg.App.member_3"]
	if !ok {
		t.Fatalf("no entry section; got %v", anchorsOf(secs))
	}
	if s.heading != "member_3(value, timeout=None)" || s.crumbs[len(s.crumbs)-1] != s.heading || s.crumbs[1] != "Application Object" {
		t.Errorf("entry: heading %q crumbs %v", s.heading, s.crumbs)
	}
	if !strings.Contains(s.text, "member 3 setting") || strings.Contains(s.text, "member 4 setting") {
		t.Errorf("entry holds another's text: %q", s.text[:120])
	}
}

func TestCoarseSectionShortEntries(t *testing.T) {
	t.Run("mostly one-line entries stay whole (an index, a list of attributes)", func(t *testing.T) {
		page := definitionPage(150, func(i int) string { return fmt.Sprintf("Value %d of the setting.", i) })
		for _, s := range splitSections(page, 0) {
			if strings.HasPrefix(s.anchor, "pkg.App.") {
				t.Fatalf("split at #%s", s.anchor)
			}
		}
	})
	t.Run("an entry too short to stand alone joins the one before", func(t *testing.T) {
		var lines []string
		for _, id := range []string{"alpha", "beta", "gamma"} {
			lines = append(lines, "|Syntax:|`"+id+" value;`|", "", filler(id, 9), "")
		}
		lines = append(lines, "|Syntax:|`b;`|", "", "|Syntax:|`delta on;`|", "", filler("delta", 9))
		anchors := []PageAnchor{{ID: "alpha"}, {ID: "beta"}, {ID: "gamma"}, {ID: "b"}, {ID: "delta"}}
		parts := splitEntries(lines, anchors, map[string]bool{})
		var got []string
		for _, p := range parts[1:] {
			got = append(got, p.anchor)
		}
		if !slices.Equal(got, []string{"alpha", "beta", "gamma", "delta"}) {
			t.Fatalf("parts %v: a near-empty entry stood alone", got)
		}
		if !slices.Contains(parts[3].lines, "|Syntax:|`b;`|") {
			t.Error("the short entry's text was lost")
		}
	})
}

func TestCoarseEntryGuards(t *testing.T) {
	body := filler("entry", 8)
	lines := strings.Split("Lead text before the entries.\n\n[1]\n\n"+body+"\n\n[RFC2616]\n\n"+body+"\n\n`fn1`\n\n"+body+"\n\n`fn2`\n\n"+body+
		"\n\nSee the entry.[¶](https://x.test/p#section-2-1)\n\n"+body+"\n\nAnother paragraph.[¶](https://x.test/p#section-2-2)\n\n"+body, "\n")
	anchors := []PageAnchor{
		{ID: "cite-1", Text: "[1]"}, {ID: "RFC2616", Text: "[RFC2616]"}, // citation labels
		{ID: "fn1"}, {ID: "fn2"}, // footnote ids
		{ID: "section-2-1"}, {ID: "section-2-2"}, // paragraph permalinks (xml2rfc)
	}
	if parts := splitEntries(lines, anchors, map[string]bool{}); parts != nil {
		t.Errorf("split at a citation, a footnote or a paragraph's permalink: %d parts", len(parts))
	}
	t.Run("a heading's anchor is the heading's", func(t *testing.T) {
		l := strings.Split("`alpha` x\n\n"+body+"\n\n`beta` y\n\n"+body, "\n")
		a := []PageAnchor{{ID: "alpha"}, {ID: "beta"}}
		if splitEntries(l, a, map[string]bool{"alpha": true, "beta": true}) != nil {
			t.Error("split at anchors the page's headings took")
		}
		if splitEntries(l, a, map[string]bool{}) == nil {
			t.Error("did not split at two entry anchors")
		}
	})
	t.Run("page order", func(t *testing.T) {
		l := strings.Split("`beta` y\n\n"+body+"\n\n`alpha` x\n\n"+body+"\n\n`gamma` z\n\n"+body, "\n")
		parts := splitEntries(l, []PageAnchor{{ID: "alpha"}, {ID: "beta"}, {ID: "gamma"}}, map[string]bool{})
		var got []string
		for _, p := range parts[1:] {
			got = append(got, p.anchor)
		}
		if !slices.Equal(got, []string{"beta", "gamma"}) {
			t.Errorf("marks %v, want beta then gamma (alpha comes before beta in the page's anchors)", got)
		}
	})
}

// specPage is a plain-text specification as a docs host renders it: one
// preformatted block per printed page, a running header and footer on
// each, numbered headings the page's anchors name.
func specPage() webPage {
	header := "RFC 9999                    Demo Token                      May 2026"
	footer := func(n int) string {
		return fmt.Sprintf("Author                       Standards Track                   [Page %d]", n)
	}
	para := func(word string, n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "   The %s rule %d applies to every token a server issues.\n", word, i)
		}
		return b.String()
	}
	pages := []string{
		"Internet Engineering Task Force                              A. Author\n\n                           Demo Token Format\n\nAbstract\n\n" + para("abstract", 3) +
			"\nTable of Contents\n\n   1.  Introduction  . . . . . . . . . . . . . . . . . . . .   2\n   2.  Claims  . . . . . . . . . . . . . . . . . . . . . . .   2\n     2.1.  \"exp\" Claim . . . . . . . . . . . . . . . . . . .   3\n\n\n" + footer(1),
		header + "\n\n\n1.  Introduction\n\n" + para("intro", 30) + "\n   The steps are:\n\n   1.  The token MUST be signed.\n\n   2.  The token MUST expire.\n\n2.  Claims\n\n2.1.  \"exp\" Claim\n\n" +
			para("expiry", 30) + "   The value is a number of seconds counted from the\n\n\n\n" + footer(2),
		header + "\n\n\n   epoch, ignoring leap seconds.\n\n" + para("leeway", 30) + "\n" + footer(3),
		header + "\n\n\n3.  Security Considerations\n\n" + para("security", 30) + "\n" + footer(4),
	}
	var md strings.Builder
	for _, p := range pages {
		md.WriteString("```\n" + p + "\n```\n")
	}
	return webPage{URL: "https://x.test/doc/rfc9999/", Title: "Demo Token Format", Markdown: md.String(), Anchors: []PageAnchor{
		{ID: "section-1", Text: "1. Introduction"}, {ID: "section-2", Text: "2. Claims"},
		{ID: "section-2.1", Text: "2.1. \"exp\" Claim"}, {ID: "section-3", Text: "3. Security Considerations"},
		{ID: "page-1"}, {ID: "page-2"},
	}}
}

func TestCoarseSectionSplitsAtNumberedHeadings(t *testing.T) {
	secs := splitSections(specPage(), 0)
	by := sectionsByAnchor(secs)
	if got := anchorsOf(secs); !slices.Equal(got, []string{"", "section-1", "section-2.1", "section-3"}) {
		t.Fatalf("sections %v; want the lead, 1, 2.1 and 3 (2 has no text of its own before 2.1)", got)
	}
	exp := by["section-2.1"]
	if want := []string{"Demo Token Format", "2. Claims", "2.1. \"exp\" Claim"}; !slices.Equal(exp.crumbs, want) || exp.heading != want[2] {
		t.Errorf("2.1: heading %q crumbs %v, want the numbering's path %v", exp.heading, exp.crumbs, want)
	}
	if !strings.Contains(exp.text, "counted from the\n   epoch, ignoring leap seconds.") {
		t.Errorf("the sentence the page break cut is not whole again:\n%s", exp.text)
	}
	for _, s := range secs {
		if strings.Contains(s.text, "[Page") || strings.Contains(s.text, "RFC 9999                    Demo Token") {
			t.Errorf("#%s keeps a running header or footer", s.anchor)
		}
	}
	if !strings.Contains(by["section-1"].text, "1.  The token MUST be signed.") {
		t.Error("a numbered list item was taken for a heading")
	}
	if !strings.Contains(by[""].text, "Table of Contents") {
		t.Error("the table of contents left the lead")
	}
	// Each paragraph of preformatted text is a code block of its own.
	if n := strings.Count(by["section-1"].text, "```"); n < 6 {
		t.Errorf("section 1 has %d fences: its paragraphs are not blocks of their own", n)
	}
}

func TestCoarseNumberedGuards(t *testing.T) {
	body := strings.Repeat("   A line of text that belongs to the section above it.\n", 40)
	md := "```\n1.  Introduction\n\n" + body + "\n2.  Something Else\n\n" + body + "\n3.  Claims\n\n" + body + "```\n"
	lines := strings.Split(md, "\n")
	anchors := []PageAnchor{{ID: "section-1", Text: "1. Introduction"}, {ID: "section-2", Text: "2. Terminology"}, {ID: "section-3", Text: "3. Claims"}}
	parts := splitNumbered(lines, anchors, map[string]bool{})
	var got []string
	for _, p := range parts[1:] {
		got = append(got, p.anchor)
	}
	if !slices.Equal(got, []string{"section-1", "section-3"}) {
		t.Errorf("marks %v: a heading whose title is not the anchor's is no mark", got)
	}
	if splitNumbered(lines, anchors[:1], map[string]bool{}) != nil {
		t.Error("split at one numbered heading")
	}
	out := strings.Split("```\n3.  Claims\n\n"+body+"\n1.  Introduction\n\n"+body+"```", "\n")
	if p := splitNumbered(out, []PageAnchor{{ID: "section-1", Text: "1. Introduction"}, {ID: "section-3", Text: "3. Claims"}}, map[string]bool{}); p != nil {
		t.Errorf("headings out of the anchors' order split the section: %d parts", len(p))
	}
}

func TestJoinPreformattedRunningLines(t *testing.T) {
	blocks := [][]string{
		{"Title page", "", "   text one.", "", "Author   Track   [Page 8]"},
		{"RFC 1  Demo  May", "", "   text two ends in the", "", "", "Author   Track   [Page 9]"},
		{"RFC 1  Demo  May", "", "   middle of a sentence.", "", "Author   Track   [Page 10]"},
		{"RFC 1  Demo  May", "", "   text four.", "Author   Track   [Page 11]"},
	}
	got := joinPages(blocks)
	want := []string{"Title page", "", "   text one.", "", "   text two ends in the", "   middle of a sentence.", "", "   text four."}
	if !slices.Equal(got, want) {
		t.Errorf("joined\n%q\nwant\n%q", got, want)
	}
	if h, f := pageRunning(blocks[:2]); h != "" || f != "" {
		t.Errorf("two pages named a running header %q or footer %q: three are needed", h, f)
	}
}

// A rebuild sections again a page whose content is unchanged when this
// build sections it into a different number of sections than the store
// holds (an index an earlier ketch built), and leaves alone one it
// sections alike.
func TestIndexBuildResectionsCoarsePages(t *testing.T) {
	coarse, plain := specPage(), webPage{URL: "https://x.test/doc/plain/", Title: "Plain", Markdown: "# Plain\n\nA page of one short section of text.\n"}
	stored := map[string]docsindex.Page{
		coarse.URL: {URL: coarse.URL, Hash: pageHash(coarse), Sections: 1},
		plain.URL:  {URL: plain.URL, Hash: pageHash(plain), Sections: 1, Source: "index html"},
	}
	b := &indexBuild{key: "https://x.test/doc/", say: func(string, ...any) {}}
	var report IndexReport
	todo := b.diff([]webPage{coarse, plain}, stored, &report)
	if len(todo) != 1 || todo[0].Page.URL != coarse.URL || report.Changed != 1 || report.Unchanged != 1 {
		t.Fatalf("todo %d, changed %d, unchanged %d: want the coarse page alone sectioned again", len(todo), report.Changed, report.Unchanged)
	}
	if n := len(todo[0].Sections); n < 3 {
		t.Errorf("the coarse page has %d sections", n)
	}
}
