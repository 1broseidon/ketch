package docs

import (
	"cmp"
	"slices"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Defined in (design/docs-evidence.md, Round 8): for a term the
// frontmatter's terms lists, the section that defines it is worth naming
// even when it did not win the budget. A pool section defines a term when
// a table row's first cell is the term (definesExactly: markup, a call's
// arguments and a flag's dashes aside). A cell that merely holds the term,
// a definition-list term and the section's own heading were measured as
// well (design/docs-evidence.md, Round 8): they won one case more on the
// tune half and none on confirm, and about half the lines they printed
// pointed at no definition. When no section that defines the term is
// printed, whole or cut, the frontmatter names one: a
// defining section on a page the answer prints, else the one Gather ranks
// highest, ties in pool order. One per term, at most definedMax, in the
// order the question writes them, on the frontmatter's defined_in line
// after not_found (definedInLine; --json and the docs tool carry them as
// defined_in):
//
//	defined_in: {failmode: https://duo.com/docs/duounix#duo-configuration-options}
//
// It is a lead, not a ranking rule: it adds no section to the walk and
// changes no group. It is counted in the budget.

// definedMax is the most terms an answer's defined_in line names.
const definedMax = 3

// DefinedIn is a section that defines a term of the question and that
// the answer does not print: Section is its url#anchor, the reference
// --section takes.
type DefinedIn struct {
	Term    string `json:"term"`
	Section string `json:"section"`
}

// definedTerm is what the pool says about where one listed term is
// defined: the pool sections defining it, in pool order, whether one of
// them is printed, and the one a defined_in line names (-1 for none).
type definedTerm struct {
	term    string
	defs    []int
	printed bool
	target  int
}

// definitions finds, for each written term the frontmatter's terms lists
// (listedTerms), the pool sections of s defining it.
func definitions(s *sorted, wts []writtenTerm, n int) []definedTerm {
	var out []definedTerm
	for _, wt := range listedTerms(wts, n) {
		d := definedTerm{term: wt.text, target: -1}
		for i := range s.secs {
			if definesTerm(s, i, wt) {
				d.defs = append(d.defs, i)
			}
		}
		out = append(out, d)
	}
	return out
}

// listedTerms are the written terms of a library of n sections that the
// frontmatter's terms lists (verdict).
func listedTerms(wts []writtenTerm, n int) []writtenTerm {
	var out []writtenTerm
	for _, wt := range wts {
		if wt.distinctive && shownTerm(wt, n) {
			out = append(out, wt)
		}
	}
	return out
}

// definesTerm reports section i of s defining wt: a table row's first
// cell is wt.
func definesTerm(s *sorted, i int, wt writtenTerm) bool {
	for _, id := range s.secs[i].pieces {
		if p := s.pieces[id]; p.table >= 0 && definesExactly(p.def, wt) {
			return true
		}
	}
	return false
}

// definesExactly reports a defining position's text that is wt itself:
// its markup, a call's argument list and a flag's dashes aside, the same
// text or the same words (rankTokens: Timeouts is timeout).
func definesExactly(text string, wt writtenTerm) bool {
	d, w := bareName(text), bareName(wt.text)
	if d == "" || w == "" {
		return false
	}
	words := rankTokens(w)
	return d == w || len(words) > 0 && slices.Equal(rankTokens(d), words)
}

// bareName is a name as written, lower case, without its markup (code,
// emphasis), what follows an opening parenthesis (a call's arguments, a
// type) and a flag's leading dashes.
func bareName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, "`*")
	if i := strings.IndexByte(s, '('); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(strings.TrimLeft(strings.Trim(s, "`* "), "-"))
}

// pickDefined chooses, for each term none of whose defining sections the
// plans print, the section its line names: one on a page a plan prints,
// else the best in Gather's ordering, ties in pool order; for at most
// definedMax terms.
func pickDefined(s *sorted, defs []definedTerm, plans []plan) []definedTerm {
	printed := printedSections(plans)
	pages := map[string]bool{}
	for _, pl := range plans {
		pages[s.secs[pl.sec].Page] = true
	}
	out := make([]definedTerm, len(defs))
	lines := 0
	for k, d := range defs {
		d.target, d.printed = -1, slices.ContainsFunc(d.defs, func(i int) bool { return printed[i] })
		if len(d.defs) > 0 && !d.printed && lines < definedMax {
			d.target = slices.MinFunc(d.defs, func(a, b int) int {
				if c := cmp.Compare(boolRank(!pages[s.secs[a].Page]), boolRank(!pages[s.secs[b].Page])); c != 0 {
					return c
				}
				if c := cmp.Compare(s.secs[a].order, s.secs[b].order); c != 0 {
					return c
				}
				return cmp.Compare(a, b)
			})
			lines++
		}
		out[k] = d
	}
	return out
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// definedLines are the defined_in entries of defs, at their shown
// addresses in lib.
func definedLines(lib docsindex.Library, s *sorted, defs []definedTerm) []DefinedIn {
	var out []DefinedIn
	for _, d := range defs {
		if d.target >= 0 {
			out = append(out, DefinedIn{Term: d.term, Section: shownURL(lib, webSectionOf(s.secs[d.target].Section, 0).URL())})
		}
	}
	return out
}
