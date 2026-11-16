package docs

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// outlineSections are two pages of many sections: every seventh section
// of the first holds the question's term zebra, the rest do not.
func outlineSections() []docsindex.Section {
	var secs []docsindex.Section
	for i := range 90 {
		text := fmt.Sprintf("Section %d covers the gizmo settings of part %d at some length, with words to read.", i, i)
		if i%7 == 3 {
			text = fmt.Sprintf("Section %d says the zebra option sets the gizmo for part %d.", i, i)
		}
		secs = append(secs, evSection("big", i, fmt.Sprintf("s%d", i), []string{"Big page", fmt.Sprintf("Topic %d", i)}, text))
	}
	for i := range 40 {
		secs = append(secs, evSection("small", i, fmt.Sprintf("t%d", i), []string{"Small page", fmt.Sprintf("Other %d", i)}, fmt.Sprintf("Other part %d of the gizmo.", i)))
	}
	return secs
}

// The outline of an answer from pages read names their unprinted
// sections within outlineTokens, those holding the question's terms
// first, and counts the rest per page with the page's address; nothing is
// named twice, and nothing printed is named.
func TestOutlineCap(t *testing.T) {
	l, lib := evLocal(t, outlineSections())
	pages := []string{evRoot + "big", evRoot + "small"}
	o := l.answer(context.Background(), lib, "zebra option", LocalOptions{live: true, pages: pages, Tokens: 80})
	if o.Err() != nil || len(o.Results) == 0 {
		t.Fatalf("outcome %s (%v)", o.Status, o.Err())
	}
	if len(o.Outline) == 0 || len(o.OutlineMore) == 0 {
		t.Fatalf("outline %d entries, more %+v: want both", len(o.Outline), o.OutlineMore)
	}
	checkOutlineEntries(t, o)
	checkOutlineOrder(t, o.Outline)
	total := len(o.Results) + len(o.Outline)
	for _, m := range o.OutlineMore {
		if m.Page != evRoot+"big" && m.Page != evRoot+"small" || m.Count <= 0 {
			t.Errorf("outline_more %+v", m)
		}
		total += m.Count
	}
	if total != 130 {
		t.Errorf("printed, outlined and counted: %d sections, want the pages' 130", total)
	}
	body := RenderAnswer(o, HintCLI)
	for _, m := range o.OutlineMore {
		if !strings.Contains(body, fmt.Sprintf("(+%d more: ketch docs --section %s)\n", m.Count, m.Page)) {
			t.Errorf("no more line for %s:\n%s", m.Page, body)
		}
	}
	if !strings.Contains(body, "\n## Not printed\n- ") {
		t.Errorf("body:\n%s", body)
	}
}

// checkOutlineEntries: the outline fits outlineTokens and names nothing
// twice, nothing printed.
func checkOutlineEntries(t *testing.T, o Outcome) {
	t.Helper()
	used := 0
	seen := map[string]bool{}
	for _, r := range o.Results {
		seen[r.URL] = true
	}
	for _, e := range o.Outline {
		used += estimateTokens(outlineLine(e))
		if seen[e.Section] {
			t.Errorf("%s named twice, or printed and named", e.Section)
		}
		seen[e.Section] = true
	}
	if used > outlineTokens {
		t.Errorf("outline takes %d tokens, cap %d", used, outlineTokens)
	}
}

// checkOutlineOrder: the question's sections (those holding zebra) come
// first: once an entry holds no zebra, none after it does.
func checkOutlineOrder(t *testing.T, outline []OutlineEntry) {
	t.Helper()
	zebra := func(e OutlineEntry) bool {
		var n int
		_, _ = fmt.Sscanf(e.Heading, "Topic %d", &n)
		return strings.HasPrefix(e.Heading, "Topic ") && n%7 == 3
	}
	if !zebra(outline[0]) {
		t.Errorf("first outline entry %+v, want a section holding zebra", outline[0])
	}
	rest := false
	for _, e := range outline {
		if !zebra(e) {
			rest = true
		} else if rest {
			t.Errorf("%s holds zebra but is listed after a section that does not", e.Heading)
		}
	}
}

// An answer from an indexed library with no pages given outlines only the
// pages it printed from.
func TestOutlinePrintedPagesOnly(t *testing.T) {
	l, lib := evLocal(t, outlineSections())
	o := l.answer(context.Background(), lib, "zebra option", LocalOptions{Tokens: 80})
	if o.Err() != nil || len(o.Results) == 0 {
		t.Fatalf("outcome %s (%v)", o.Status, o.Err())
	}
	printed := map[string]bool{}
	for _, r := range o.Results {
		printed[pageOf(r)] = true
	}
	for _, e := range o.Outline {
		page, _, _ := strings.Cut(e.Section, "#")
		if !printed[page] {
			t.Errorf("outline names %s, a page nothing was printed from", e.Section)
		}
	}
}
