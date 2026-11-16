package docs

import (
	"cmp"
	"slices"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Not printed (design/docs-evidence.md, Final shape): an answer names the
// sections of the pages it read (the exact pages a search ranked best), or
// of the pages it printed from, that it did not print, each by its heading
// path, the address --section takes and its size. Those holding the
// question's terms come first, in Gather's order, then the rest in page
// order, within outlineTokens; what does not fit is counted per page, with
// the page's address, which --section reads whole. The outline is unread
// text: an agent reads an entry before stating what it says.

// OutlineEntry is a section of a page an answer read that it did not
// print.
type OutlineEntry struct {
	// Heading is its heading path below the page's title (the title for a
	// page's lead section).
	Heading string `json:"heading"`
	// Section is its url#anchor, what --section (the docs tool's section)
	// takes.
	Section string `json:"section"`
	Tokens  int    `json:"tokens"`
}

// OutlineMore counts a page's sections the outline had no room for.
type OutlineMore struct {
	Page  string `json:"page"`
	Count int    `json:"count"`
}

// outlineOf is the outline of pages' sections not among printed (by ID),
// the question's sections (Gather's pool) first.
func (l *Local) outlineOf(lib docsindex.Library, g *gathered, printed map[int64]bool, pages []string) ([]OutlineEntry, []OutlineMore, error) {
	if l.store == nil || len(pages) == 0 {
		return nil, nil, nil
	}
	secs, err := l.store.SectionsOfPages(lib.Key, pages)
	if err != nil {
		return nil, nil, err
	}
	var cands []docsindex.Section
	for _, sec := range secs {
		if !printed[sec.ID] {
			cands = append(cands, cleanSection(sec))
		}
	}
	outlineOrder(cands, g, pageOrder(pages))
	var out []OutlineEntry
	var more []OutlineMore
	seen := map[string]bool{}
	used := 0
	for _, sec := range cands {
		e := OutlineEntry{Heading: outlineHeading(sec), Section: shownURL(lib, webSectionOf(sec, 0).URL()), Tokens: estimateTokens(sec.Text)}
		if seen[e.Section] {
			continue
		}
		seen[e.Section] = true
		if n := estimateTokens(outlineLine(e)); used+n <= outlineTokens {
			used += n
			out = append(out, e)
			continue
		}
		page := shownURL(lib, sec.Page)
		if i := slices.IndexFunc(more, func(m OutlineMore) bool { return m.Page == page }); i >= 0 {
			more[i].Count++
		} else {
			more = append(more, OutlineMore{Page: page, Count: 1})
		}
	}
	return out, more, nil
}

// outlineOrder sorts sections for the outline: those in Gather's pool (the
// question's) first, in pool order, then by page (place) and page order.
func outlineOrder(cands []docsindex.Section, g *gathered, place map[string]int) {
	pool := map[int64]int{}
	for i, p := range g.pool {
		pool[p.ID] = i + 1
	}
	slices.SortStableFunc(cands, func(a, b docsindex.Section) int {
		pa, pb := pool[a.ID], pool[b.ID]
		switch {
		case pa > 0 && pb > 0:
			return cmp.Compare(pa, pb)
		case pa > 0 || pb > 0:
			return cmp.Compare(pb, pa) // the question's sections first
		}
		if c := cmp.Compare(place[pageKeyOf(a.Page)], place[pageKeyOf(b.Page)]); c != 0 {
			return c
		}
		return cmp.Compare(a.Order, b.Order)
	})
}

// outlineHeading is a section's heading path below its page's title, or
// the title for the page's lead section.
func outlineHeading(sec docsindex.Section) string {
	if path := headingBelow(sec); len(path) > 0 {
		return strings.Join(path, " > ")
	}
	return pageTitle(sec)
}

// outlineLine is an outline entry as printed.
func outlineLine(e OutlineEntry) string {
	return "- " + e.Heading + " — " + e.Section + " (" + groupDigits(e.Tokens) + " tokens)\n"
}
