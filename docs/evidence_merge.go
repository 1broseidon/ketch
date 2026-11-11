package docs

import (
	"cmp"
	"slices"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Merge (design/docs-evidence.md, Rank): each plan is printed as one group
// under its section's heading path, a page's groups together in page
// order, pages in the order their first section opened. A
// whole section is its text. A cut one is its pieces in page order:
// neighbours joined by the text between them, "…" where text was skipped,
// rows of one table under its one header; it says how many rows of a
// table it shows, and where else a folded piece's text sits, on one line
// under the heading path (Result.GroupLine), and ends with its contents
// line.

// group is one section's plan as printed.
type group struct {
	sec    int
	pieces []int // printed when cut: chosen and their context, in page order
	whole  bool
	// printed are the sections the answer prints: the group line names
	// none of them.
	printed map[int]bool
}

// groupsOf are the plans as groups in printed order: the groups of one
// page together, pages in the order of their first group taken, and a
// page's groups in page order (a child section that outranks its parent
// prints after it, as the page reads).
func groupsOf(s *sorted, r *ranked) []group {
	out := make([]group, len(r.plans))
	first := map[string]int{} // page: its first plan
	printed := printedSections(r.plans)
	for i, pl := range r.plans {
		out[i] = group{sec: pl.sec, pieces: pl.pieces, whole: pl.whole, printed: printed}
		if _, ok := first[s.secs[pl.sec].Page]; !ok {
			first[s.secs[pl.sec].Page] = i
		}
	}
	slices.SortStableFunc(out, func(a, b group) int {
		if c := cmp.Compare(first[s.secs[a.sec].Page], first[s.secs[b.sec].Page]); c != 0 {
			return c
		}
		return cmp.Compare(s.secs[a.sec].Order, s.secs[b.sec].Order)
	})
	return out
}

// printedSections are the sections plans print.
func printedSections(plans []plan) map[int]bool {
	out := make(map[int]bool, len(plans))
	for _, pl := range plans {
		out[pl.sec] = true
	}
	return out
}

// renderGroup is a group as a result.
func renderGroup(lib docsindex.Library, s *sorted, g group) Result {
	sec := s.secs[g.sec]
	r := Result{
		Library: lib.Root, Version: lib.Version, Title: sec.Heading, Breadcrumb: strings.Join(sec.Crumbs, " > "),
		URL: webSectionOf(sec.Section, 0).URL(), Anchors: []string{sec.Anchor}, Source: SearchBackend,
	}
	r.PageTitle, r.HeadingPath = pageTitle(sec.Section), headingBelow(sec.Section)
	if g.whole || len(g.pieces) == len(sec.pieces) {
		// A group holding every piece of its section is the section: a
		// section longer than a page that is one code block prints it whole.
		r.Snippet = sec.Text
	} else {
		r.Snippet = joinPieces(s, sec, g.pieces)
		r.Truncated = true
		r.ShownTokens, r.SectionTokens = estimateTokens(r.Snippet), sec.tokens
		r.Contents = contentsLine(sectionKeys(s, g.sec, g.pieces), sec.subs)
		r.Rows = tableRows(s, sec, g.pieces)
	}
	r.SameText = foldLine(s, g.pieces, g.sec, g.printed)
	return r
}

// pageTitle is a section's page's title: the first of its heading path
// (its own heading for a page's lead section).
func pageTitle(s docsindex.Section) string {
	if len(s.Crumbs) > 0 {
		return s.Crumbs[0]
	}
	return s.Heading
}

// headingBelow is a section's heading path below its page's title: none
// for the page's lead section. A heading that repeats the title (a page
// whose first heading is its title) is the title, not below it.
func headingBelow(s docsindex.Section) []string {
	if len(s.Crumbs) > 1 {
		path := s.Crumbs[1:]
		for len(path) > 0 && strings.TrimSpace(path[0]) == strings.TrimSpace(s.Crumbs[0]) {
			path = path[1:]
		}
		return slices.Clone(path)
	}
	if len(s.Crumbs) == 0 && s.Anchor != "" && s.Heading != "" {
		return []string{s.Heading}
	}
	return nil
}

// joinPieces prints a section's pieces (ids, in page order): neighbours
// joined by the text between them; rows of one table one per line under
// the table's header; " … " between sentences of one paragraph and a line
// of "…" between blocks where text was skipped.
func joinPieces(s *sorted, sec sortedSection, ids []int) string {
	var b strings.Builder
	for i, id := range ids {
		p := s.pieces[id]
		newTable := p.table >= 0 && sec.tables[p.table].header != ""
		if i > 0 {
			q := s.pieces[ids[i-1]]
			switch {
			case p.table >= 0 && p.table == q.table:
				b.WriteString("\n")
				newTable = false
			case p.ord == q.ord+1:
				b.WriteString(sec.Text[q.end:p.start]) // a new table's header is in it
				newTable = false
			case p.span == q.span && p.kind == kindProse && q.kind == kindProse:
				b.WriteString(" … ")
			default:
				b.WriteString("\n\n…\n\n")
			}
		}
		if newTable {
			b.WriteString(sec.tables[p.table].header + "\n")
		}
		b.WriteString(p.text)
	}
	return b.String()
}

// tableRows say how many rows of each table a group shows, for the
// tables it shows only some rows of.
func tableRows(s *sorted, sec sortedSection, ids []int) []RowCount {
	var out []RowCount
	for t, tb := range sec.tables {
		shown, kind := 0, kindOptionRow
		for _, id := range ids {
			if p := s.pieces[id]; p.table == t {
				shown++
				if p.kind != kindOptionRow {
					kind = kindRow
				}
			}
		}
		if shown > 0 && shown < len(tb.rows) {
			out = append(out, RowCount{Kind: kind.String(), Shown: shown, Of: len(tb.rows)})
		}
	}
	return out
}

// foldMaxHeadings is how many of a folded piece's other sections its line
// names.
const foldMaxHeadings = 3

// foldLine says where else the text of the folded pieces among ids sits,
// in sections the answer does not print: "same text in N other sections:
// a; b; c" (sameLine); nil for none.
func foldLine(s *sorted, ids []int, own int, printed map[int]bool) *SameText {
	var others []int
	seen := map[int]bool{own: true}
	for i := range printed {
		seen[i] = true
	}
	for _, id := range ids {
		for _, f := range s.pieces[id].folded {
			if sec := s.pieces[f].sec; !seen[sec] {
				seen[sec] = true
				others = append(others, sec)
			}
		}
	}
	return sameLine(s, others, own)
}

// sameLine says that sections others hold the same text as section own:
// "same text in N other sections: a; b; c", naming up to foldMaxHeadings
// of them by heading (with their page's title when it is own's heading);
// nil for none.
func sameLine(s *sorted, others []int, own int) *SameText {
	if len(others) == 0 {
		return nil
	}
	others = slices.Clone(others)
	slices.Sort(others)
	var names []string
	for _, sec := range others[:min(len(others), foldMaxHeadings)] {
		o := s.secs[sec]
		name := o.Heading
		if name == s.secs[own].Heading && len(o.Crumbs) > 1 {
			name = o.Crumbs[0] + " > " + name
		}
		names = append(names, name)
	}
	return &SameText{Count: len(others), Sections: names}
}
