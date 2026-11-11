package docs

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// pamSections is a configuration guide whose stored text escapes the
// underscores of its return values, as its HTML-to-Markdown extraction
// left them.
func pamSections() []docsindex.Section {
	page := evRoot + "sag-configuration-file.html"
	return []docsindex.Section{
		{Page: page, Anchor: "syntax", Heading: "4.1. Configuration file syntax", Crumbs: []string{"4.1. Configuration file syntax", "4.1. Configuration file syntax"},
			Text: "For the more complicated syntax valid *control* values have the form `[value1=action1 ...]`.\n\nIt is selected from one of these: *success*, *open\\_err*, *service\\_err*, *system\\_err*."},
		{Page: evRoot + "other.html", Heading: "Other", Crumbs: []string{"Other"}, Text: "Nothing to do with control values."},
	}
}

// oldTokens makes the full-text rows as ketch did before rows were made
// from the cleaned text: from the stored text as it is.
type oldTokens struct{}

func (oldTokens) Version() string { return "k1" }

func (oldTokens) Row(s docsindex.Section) [4]string {
	return [4]string{strings.Join(headingTokens(s.Heading), " "), "", strings.Join(identParts(s.Heading+"\n"+s.Text), " "), strings.Join(rankTokens(s.Text), " ")}
}

// A no-op escape in the stored text does not hide a term: the row is made
// from the text as printed, so service_err is counted and found.
func TestEscapedTermFound(t *testing.T) {
	l, lib := evLocal(t, pamSections())
	o := l.answer(context.Background(), lib, "control value1=action1 service_err", LocalOptions{})
	if o.Err() != nil || slices.Contains(o.NotFound, "service_err") || !slices.Contains(o.Terms, TermCount{Term: "service_err", Sections: 1}) {
		t.Fatalf("outcome = %s %v, terms %v, not_found %v", o.Status, o.Err(), o.Terms, o.NotFound)
	}
}

// An index whose rows an older tokenizer made is made again from its
// stored text the first time a question reads it, with no fetching; the
// term its escapes hid is then found.
func TestEscapedTermRetokenised(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs-index.db")
	old := docsindex.Open(path, oldTokens{})
	secs := pamSections()
	if err := old.PutPages(evRoot, []docsindex.PageData{
		{Page: docsindex.Page{URL: secs[0].Page, Hash: "a"}, Sections: secs[:1]},
		{Page: docsindex.Page{URL: secs[1].Page, Hash: "b"}, Sections: secs[1:]},
	}); err != nil {
		t.Fatal(err)
	}
	lib := docsindex.Library{Key: evRoot, Root: evRoot, Format: docsindex.FormatVersion, Sections: 2}
	if err := old.PutLibrary(lib); err != nil {
		t.Fatal(err)
	}
	if n, err := old.Count(evRoot, `"serviceerr"`); err != nil || n != 0 {
		t.Fatalf("old rows count service_err in %d sections (%v), want 0: the escape hid it", n, err)
	}
	store := OpenIndex(path)
	if ok, err := store.FTSCurrent(evRoot); err != nil || ok {
		t.Fatalf("rows current = %v (%v), want an older tokenizer's", ok, err)
	}
	o := NewLocal(store).answer(context.Background(), lib, "service_err", LocalOptions{})
	if o.Err() != nil || len(o.NotFound) != 0 || !slices.Contains(o.Terms, TermCount{Term: "service_err", Sections: 1}) {
		t.Fatalf("outcome = %s %v, terms %v, not_found %v", o.Status, o.Err(), o.Terms, o.NotFound)
	}
	if ok, err := store.FTSCurrent(evRoot); err != nil || !ok {
		t.Errorf("rows current after the question = %v (%v)", ok, err)
	}
}

// A section prints the frontmatter an answer says of its source (backend,
// library, index; its page line when read a page at a time), no verdict
// and no result_count; its heading path does not repeat a heading, and
// its text is cleaned as an answer's is.
func TestSectionFrontmatterAndHeading(t *testing.T) {
	l, _ := evLocal(t, pamSections())
	ref := evRoot + "sag-configuration-file.html#syntax"
	o := l.SectionSelect(context.Background(), "", ref, 0, 0)
	if o.Err() != nil || len(o.Results) != 1 {
		t.Fatalf("section: %v", o.Err())
	}
	r := o.Results[0]
	if strings.Contains(RenderResult(r, HintNone), "syntax > 4.1.") || !strings.HasPrefix(RenderResult(r, HintNone), "### 4.1. Configuration file syntax\n") {
		t.Errorf("heading repeats itself:\n%s", RenderResult(r, HintNone))
	}
	if !strings.Contains(r.Snippet, "*service_err*") {
		t.Errorf("section text not cleaned: %s", r.Snippet)
	}
	f := SectionFrontmatter(SearchBackend, "", o)
	if got := f.Render(); got != "---\nbackend: search\nlibrary: "+evRoot+"\nindex: full, 0 pages\n---\n" {
		t.Errorf("section frontmatter:\n%s", got)
	}
	if f.Terms != nil || f.NotFound != nil || f.Status != Found {
		t.Errorf("section fields = %+v, want no terms or not_found", f)
	}
	paged := l.SectionSelect(context.Background(), "", ref, 60, 2)
	if got := SectionFrontmatter(SearchBackend, "", paged).Render(); !strings.HasSuffix(got, "\npage: 2 of "+strconv.Itoa(paged.Results[0].Pages)+"\n---\n") {
		t.Errorf("paged section frontmatter:\n%s", got)
	}
}
