package docs

import (
	"fmt"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Contents (design/docs-evidence.md, Rank): a group that is cut ends with
// one line naming what it left out, so that the next call can be exact
// when the question's words are not the docs' words: the keys (first
// cells) of its option tables' rows it does not print, and the headings
// of its section's direct subsections, each with its own option table's
// keys. The line is at most about contentsTokens; a list cut short ends
// with "…" and how many it leaves out. It is counted in the budget (a
// plan costs what its group renders to) and carried by Result.Contents.

// contentsTokens is the most tokens a contents line takes.
const contentsTokens = 40

// subsection is a direct subsection of a pool section, as its contents
// line names it: its heading and its option tables' keys.
type subsection struct {
	heading string
	keys    []string
}

// outline gives each of s's sections its direct subsections, read from
// the index: the sections after it on its page whose heading path is its
// own and one heading more, a docs platform's boilerplate left out.
func (l *Local) outline(lib docsindex.Library, s *sorted) error {
	if l.store == nil || len(s.secs) == 0 {
		return nil
	}
	var pages []string
	seen := map[string]bool{}
	for _, sec := range s.secs {
		if !seen[sec.Page] {
			seen[sec.Page] = true
			pages = append(pages, sec.Page)
		}
	}
	all, err := l.store.PageOutlines(lib.Key, pages)
	if err != nil {
		return err
	}
	at := make(map[int64]int, len(all))
	for i, o := range all {
		at[o.ID] = i
	}
	kids := make([][]int64, len(s.secs))
	var ids []int64
	for i, sec := range s.secs {
		k, ok := at[sec.ID]
		if !ok {
			continue
		}
		kids[i] = directSubsections(all, k)
		ids = append(ids, kids[i]...)
	}
	if len(ids) == 0 {
		return nil
	}
	got, err := l.store.SectionsByID(ids)
	if err != nil {
		return err
	}
	text := make(map[int64]string, len(got))
	for _, g := range got {
		text[g.ID] = g.Text
	}
	for i := range s.secs {
		for _, id := range kids[i] {
			o := all[at[id]]
			ps, tables := splitText(cleanText(text[id]))
			s.secs[i].subs = append(s.secs[i].subs, subsection{heading: cleanLine(o.Heading), keys: optionKeys(ps, tables, nil)})
		}
	}
	return nil
}

// directSubsections are the IDs of the direct subsections of the section
// at k in all (a page's sections in order, among others).
func directSubsections(all []docsindex.Section, k int) []int64 {
	parent := all[k]
	var out []int64
	for _, o := range all[k+1:] {
		if o.Page != parent.Page || len(o.Crumbs) <= len(parent.Crumbs) || !samePath(o.Crumbs, parent.Crumbs) {
			break
		}
		if len(o.Crumbs) == len(parent.Crumbs)+1 && !boilerplateSection(o.Crumbs) {
			out = append(out, o.ID)
		}
	}
	return out
}

// samePath reports a heading path that starts with prefix.
func samePath(path, prefix []string) bool {
	for i, p := range prefix {
		if path[i] != p {
			return false
		}
	}
	return true
}

// optionKeys are the keys (first cells, code marks out) of the rows of
// the option tables among tables whose pieces (ids into ps by the
// tables' rows) shown does not hold; a table is an option table when
// most of its rows are option rows.
func optionKeys(ps []piece, tables []table, shown map[int]bool) []string {
	var out []string
	for _, tb := range tables {
		n := 0
		for _, id := range tb.rows {
			if ps[id].kind == kindOptionRow {
				n++
			}
		}
		if 2*n <= len(tb.rows) {
			continue
		}
		for _, id := range tb.rows {
			if k := strings.Trim(ps[id].def, "`* "); k != "" && !shown[id] {
				out = append(out, k)
			}
		}
	}
	return out
}

// sectionKeys are the keys of section i's option-table rows that ids (a
// group's printed pieces) leave out.
func sectionKeys(s *sorted, i int, ids []int) []string {
	sec := s.secs[i]
	if len(sec.tables) == 0 {
		return nil
	}
	shown := map[int]bool{}
	for _, id := range ids {
		shown[id] = true
	}
	// The tables' rows are ids into s.pieces; optionKeys reads them there.
	return optionKeys(s.pieces, sec.tables, shown)
}

// contentsLine names what a cut group left out: keys, its option rows'
// keys, and subs, its section's direct subsections; "" for nothing. The
// subsections' headings come first, then keys from each list in turn,
// while the line is within contentsTokens.
func contentsLine(keys []string, subs []subsection) string {
	if len(keys) == 0 && len(subs) == 0 {
		return ""
	}
	lists := make([][]string, 0, 1+len(subs))
	lists = append(lists, keys)
	for _, sub := range subs {
		lists = append(lists, sub.keys)
	}
	c := contents{keys: keys, subs: subs, shown: make([]int, len(lists))}
	fits := func() bool { return estimateTokens(c.render()) <= contentsTokens }
	for c.n < len(subs) {
		c.n++
		if !fits() {
			c.n--
			break
		}
	}
	for more := true; more; {
		more = false
		for k := 0; k <= c.n; k++ {
			if c.shown[k] == len(lists[k]) {
				continue
			}
			c.shown[k]++
			if fits() {
				more = true
			} else {
				c.shown[k]--
			}
		}
	}
	return c.render()
}

// contents is a contents line being fitted: the first n subsections are
// named, and shown[k] keys of each list (the option rows' first, then
// each named subsection's).
type contents struct {
	keys  []string
	subs  []subsection
	n     int
	shown []int
}

func (c contents) render() string {
	var parts []string
	if len(c.keys) > 0 {
		parts = append(parts, "options: "+cutList(c.keys, c.shown[0]))
	}
	for i, sub := range c.subs[:c.n] {
		part := sub.heading
		if len(sub.keys) > 0 {
			part += " (options: " + cutList(sub.keys, c.shown[i+1]) + ")"
		}
		parts = append(parts, part)
	}
	if rest := len(c.subs) - c.n; rest > 0 {
		noun := "sections"
		if rest == 1 {
			noun = "section"
		}
		parts = append(parts, fmt.Sprintf("… +%d %s", rest, noun))
	}
	return "also in this section: " + strings.Join(parts, "; ")
}

// cutList is the first n of list, then "…" and how many it leaves out.
func cutList(list []string, n int) string {
	if n >= len(list) {
		return strings.Join(list, ", ")
	}
	rest := fmt.Sprintf("… +%d", len(list)-n)
	if n == 0 {
		return rest
	}
	return strings.Join(list[:n], ", ") + ", " + rest
}
