package docs

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Sort (design/docs-evidence.md): the pool's sections, cleaned as they
// entered it, become labelled and deduplicated pieces. A piece is a
// paragraph (its sentences when it runs past sortParagraphTokens), a list
// item, a code block (with the tab label before it) or a table row: a
// table is never one piece, and each body row carries its header row as
// context; a quote is cut into its paragraphs, and a block of one entry
// per line (an index of names) into its lines. Its kind and its page's
// role come from structure alone. Pieces with the same text, whitespace and
// case aside, are folded into one: the copy from the best page role, then
// the earliest in the pool, listing where the others sit.

// pieceKind is what a piece is, by its structure.
type pieceKind int

const (
	kindProse     pieceKind = iota // a paragraph, or a sentence of a long one
	kindNote                       // an admonition, or a paragraph opening with Note, Warning or Important
	kindItem                       // an item of an unordered list
	kindStep                       // an item of an ordered list
	kindExample                    // a code block
	kindRow                        // a table row
	kindOptionRow                  // a table row whose first cell is an identifier or code
)

func (k pieceKind) String() string {
	return [...]string{"prose", "note", "item", "step", "example", "row", "option row"}[k]
}

// pageRole is what a section's page is to the site, from what the planner
// knows of its path (planTier) and the troubleshooting prior.
type pageRole int

const (
	roleReference pageRole = iota
	roleGuide
	roleFAQ
	roleTroubleshooting
	roleReleaseNotes
)

func (r pageRole) String() string {
	return [...]string{"reference", "guide", "faq", "troubleshooting", "release notes"}[r]
}

// piece is a verbatim byte range of a pool section's cleaned text.
type piece struct {
	id, sec    int // its index in sorted.pieces; its section's in sorted.secs
	ord        int // its place among its section's pieces
	kind       pieceKind
	start, end int
	text       string
	tokens     int    // estimated, with its separator
	span       int    // the paragraph or code block (textSpans) it is in
	table      int    // a row's table, in its section's tables; -1 for none
	def        string // its defining position: a row's first cell, the definition term (definitionTerm) opening an item or paragraph
	into       int    // the id of the piece it was folded into; -1 for none
	folded     []int  // the ids of the pieces folded into it
}

// table is a table of a section: its header and delimiter rows (empty for
// a table without), and its body rows' piece ids in order.
type table struct {
	header string
	rows   []int
}

// sortedSection is a pool section with its pieces.
type sortedSection struct {
	docsindex.Section
	role   pageRole
	pieces []int // ids, in order
	tables []table
	tokens int // its whole text's estimate
	// order is its rank in Gather's whole-question ordering, 1 the first;
	// a section that ordering leaves out ranks after all it holds.
	order int
	subs  []subsection
}

// sorted is Sort's output: the pool's sections in pool order, and every
// piece of them.
type sorted struct {
	secs   []sortedSection
	pieces []piece
}

// sortPool splits, labels and folds the pool's sections (cleaned, in pool
// order) of the library at root.
func sortPool(pool []docsindex.Section, root string) *sorted {
	r, rootErr := parseWebRoot(root)
	out := &sorted{secs: make([]sortedSection, len(pool))}
	for i, sec := range pool {
		ps, tables := splitText(sec.Text)
		ss := sortedSection{Section: sec, role: pageRoleOf(sec, r, rootErr == nil), tables: tables, tokens: estimateTokens(sec.Text)}
		base := len(out.pieces)
		for _, p := range ps {
			p.id, p.sec, p.into = base+p.ord, i, -1
			ss.pieces = append(ss.pieces, p.id)
			out.pieces = append(out.pieces, p)
		}
		for t := range ss.tables {
			for k := range ss.tables[t].rows {
				ss.tables[t].rows[k] += base
			}
		}
		out.secs[i] = ss
	}
	out.fold()
	return out
}

// fold folds pieces with the same text after whitespace and case into
// one: the copy from the best page role (roleFactor), then the earliest.
func (s *sorted) fold() {
	first := map[string]int{}
	for i := range s.pieces {
		key := foldKey(s.pieces[i].text)
		if key == "" {
			continue
		}
		rep, ok := first[key]
		if !ok {
			first[key] = i
			continue
		}
		if roleFactor(s.secs[s.pieces[i].sec].role, false) > roleFactor(s.secs[s.pieces[rep].sec].role, false) {
			// This copy's page role is better: it stands for the others.
			s.pieces[i].folded = append(append(s.pieces[i].folded, rep), s.pieces[rep].folded...)
			s.pieces[rep].folded = nil
			for _, f := range s.pieces[i].folded {
				s.pieces[f].into = i
			}
			first[key] = i
			continue
		}
		s.pieces[i].into = rep
		s.pieces[rep].folded = append(s.pieces[rep].folded, i)
	}
}

// foldKey is a piece's text as folding compares it: lower case,
// whitespace collapsed.
func foldKey(t string) string {
	return strings.ToLower(strings.Join(strings.Fields(t), " "))
}

// pageRoleOf is a section's page role: release notes by its path, then
// troubleshooting (troubleSection), FAQ by its path or title, else
// reference for an API reference leaf (planTier) and guide for the rest.
func pageRoleOf(s docsindex.Section, root webRoot, rootOK bool) pageRole {
	u, err := url.Parse(s.Page)
	if err != nil {
		return roleGuide
	}
	segs := pathSegments(strings.ToLower(u.Path))
	if rootOK {
		segs = pathSegments(strings.ToLower(strings.TrimPrefix(u.Path, root.prefix)))
	}
	for _, seg := range segs {
		if releaseSegs[strings.TrimSuffix(strings.TrimSuffix(seg, ".html"), ".md")] {
			return roleReleaseNotes
		}
	}
	if troubleSection(s) {
		return roleTroubleshooting
	}
	title := ""
	if len(s.Crumbs) > 0 {
		title = s.Crumbs[0]
	}
	if faqRe.MatchString(u.Path) || faqRe.MatchString(title) {
		return roleFAQ
	}
	if rootOK && planTier(u, root, nil) == tierReference {
		return roleReference
	}
	return roleGuide
}

// releaseSegs are the path segments of release notes among the planner's
// reference segments (referenceSegs).
var releaseSegs = map[string]bool{
	"changelog": true, "changelogs": true, "changes": true, "release-notes": true, "releases": true, "news": true,
	"whatsnew": true, "whats-new": true, "history": true, "prev_api_changes": true, "next_api_changes": true, "blog": true,
}

// faqRe matches a path or title that says it is a FAQ.
var faqRe = regexp.MustCompile(`(?i)(?:^|[^a-z])faqs?(?:[^a-z]|$)|frequently asked`)

// splitText cuts a section's text into pieces, in order, with its tables:
// each code block (with the tab label right before it), each paragraph
// (each sentence of a long one), each list item, each table body row, each
// quote; a paragraph opening with Note, Warning or Important is one note.
// Headings and labels (a tab's name, "Menu") inside the text are not
// pieces. A piece's table is an index among these tables.
func splitText(text string) ([]piece, []table) {
	sp := &splitter{text: text}
	spans := textSpans(text)
	for g, span := range spans {
		switch {
		case span.code:
			start := span.start
			if g > 0 && spans[g-1].label && !spans[g-1].code {
				start = spans[g-1].start
			}
			sp.add(kindExample, g, start, span.end)
		case span.label && g+1 < len(spans) && spans[g+1].code:
			// joined to the code after it
		default:
			sp.block(g, span.start, span.end)
		}
	}
	return sp.pieces, sp.tables
}

// splitter collects one section's pieces.
type splitter struct {
	text   string
	pieces []piece
	tables []table
}

func (sp *splitter) add(kind pieceKind, span, start, end int) *piece {
	t := sp.text[start:end]
	sp.pieces = append(sp.pieces, piece{ord: len(sp.pieces), kind: kind, start: start, end: end, text: t,
		tokens: estimateTokens(t) + 1, span: span, table: -1, into: -1})
	return &sp.pieces[len(sp.pieces)-1]
}

// Line kinds, for splitter.block.
const (
	lineProse = iota
	lineItem
	lineTable
	lineQuote
	lineHeading
)

// block cuts a paragraph run (which may hold list items, table rows,
// quotes or headings) into pieces by its lines' kinds.
func (sp *splitter) block(span, start, end int) {
	kind, from, runEnd, ordered := -1, start, start, false
	for pos := start; pos <= end; {
		lineEnd := end
		if nl := strings.IndexByte(sp.text[pos:end], '\n'); nl >= 0 {
			lineEnd = pos + nl
		}
		line := sp.text[pos:lineEnd]
		k, starts := splitLineKind(line, kind == lineItem)
		if k != kind || starts {
			sp.run(kind, span, from, runEnd, ordered)
			kind, from = k, pos
			ordered = k == lineItem && orderedItemRe.MatchString(line)
		}
		runEnd = lineEnd
		if lineEnd >= end {
			break
		}
		pos = lineEnd + 1
	}
	sp.run(kind, span, from, runEnd, ordered)
}

// run cuts one run of lines of a kind (block) into pieces.
func (sp *splitter) run(kind, span, from, to int, ordered bool) {
	if kind < 0 || from >= to {
		return
	}
	switch kind {
	case lineProse:
		sp.prose(span, from, to)
	case lineItem:
		k := kindItem
		if ordered {
			k = kindStep
		}
		p := sp.add(k, span, from, to)
		p.def = definitionTerm(listBody(p.text))
	case lineTable:
		sp.table(span, from, to)
	case lineQuote:
		if admonitionRe.MatchString(sp.text[from:to]) || noteRe.MatchString(strings.TrimLeft(sp.text[from:to], "> ")) {
			sp.add(kindNote, span, from, to)
			return
		}
		sp.quote(span, from, to)
	}
}

// quote cuts a quote (lines opening with ">") into its paragraphs, as the
// quote's own blank lines part them; a paragraph holding a quoted code
// block is one example, never cut inside the block.
func (sp *splitter) quote(span, from, to int) {
	start, code, fenced := from, false, false
	for pos := from; pos <= to; {
		lineEnd := to
		if nl := strings.IndexByte(sp.text[pos:to], '\n'); nl >= 0 {
			lineEnd = pos + nl
		}
		body := strings.TrimLeft(sp.text[pos:lineEnd], "> \t")
		if strings.HasPrefix(body, "```") || strings.HasPrefix(body, "~~~") {
			fenced, code = !fenced, true
		}
		if !fenced && strings.TrimSpace(body) == "" || lineEnd >= to {
			end := pos
			if lineEnd >= to {
				end = to
			}
			if t := strings.TrimRight(sp.text[start:end], " \n>"); strings.TrimSpace(strings.Trim(t, "> \n")) != "" {
				k := kindProse
				if code {
					k = kindExample
				}
				sp.add(k, span, start, start+len(t))
			}
			start, code = lineEnd+1, false
		}
		if lineEnd >= to {
			break
		}
		pos = lineEnd + 1
	}
}

// sortParagraphTokens is the most tokens a paragraph has as one piece; a
// longer one is cut into its sentences.
const sortParagraphTokens = 120

// prose cuts a run of prose lines (a paragraph) into one piece, or into
// sentences past sortParagraphTokens, labels left out; a run opening with
// Note, Warning or Important (or an admonition's marker) is one note; a
// run holding a fence indented past a code block's (a list item's code,
// which textSpans does not see as code) is one example, verbatim, so it
// is never cut or printed with one fence.
func (sp *splitter) prose(span, from, to int) {
	run := sp.text[from:to]
	if strayFenceRe.MatchString(run) {
		sp.add(kindExample, span, from, to)
		return
	}
	if noteRe.MatchString(run) || admonitionRe.MatchString(run) {
		sp.add(kindNote, span, from, to)
		return
	}
	ranges := sentenceRanges(run)
	if estimateTokens(run) <= sortParagraphTokens && len(ranges) > 0 {
		ranges = [][2]int{{ranges[0][0], ranges[len(ranges)-1][1]}}
	}
	for _, r := range ranges {
		if len(ranges) > 1 || estimateTokens(run) > sortParagraphTokens {
			if lines := entryLines(run[r[0]:r[1]]); lines != nil {
				for _, l := range lines {
					sp.add(kindProse, span, from+r[0]+l[0], from+r[0]+l[1])
				}
				continue
			}
		}
		if !isLabelText(run[r[0]:r[1]]) {
			p := sp.add(kindProse, span, from+r[0], from+r[1])
			p.def = definitionTerm(p.text)
		}
	}
}

// entryLines are the lines of a block of one entry per line (an index of
// names, a contents list): three lines or more, none ending as a sentence
// ends; nil for any other text. Each is a piece of its own.
func entryLines(t string) [][2]int {
	if strings.Count(t, "\n") < 2 {
		return nil
	}
	var out [][2]int
	for pos := 0; pos <= len(t); {
		end := len(t)
		if nl := strings.IndexByte(t[pos:], '\n'); nl >= 0 {
			end = pos + nl
		}
		line := strings.TrimSpace(t[pos:end])
		if line != "" && strings.ContainsAny(line[len(line)-1:], ".!?") {
			return nil
		}
		if line != "" {
			lead := len(t[pos:end]) - len(strings.TrimLeft(t[pos:end], " \t"))
			out = append(out, [2]int{pos + lead, pos + lead + len(line)})
		}
		pos = end + 1
	}
	return out
}

// table cuts a run of table lines into its body rows: the first line is
// the header when a delimiter row follows it; delimiter rows inside the
// body (a page's broken table) are not rows.
func (sp *splitter) table(span, from, to int) {
	t := table{}
	pos, line := from, 0
	for pos <= to {
		lineEnd := to
		if nl := strings.IndexByte(sp.text[pos:to], '\n'); nl >= 0 {
			lineEnd = pos + nl
		}
		row := sp.text[pos:lineEnd]
		switch {
		case line == 0 && lineEnd < to && delimiterRowRe.MatchString(lineOf(sp.text[lineEnd+1:to])):
			// the header: it and the delimiter row are the table's context
			next := lineEnd + 1 + len(lineOf(sp.text[lineEnd+1:to]))
			t.header = sp.text[pos:next]
			lineEnd = next
		case delimiterRowRe.MatchString(row) || strings.TrimSpace(strings.ReplaceAll(row, "|", "")) == "":
		default:
			cell := firstCell(row)
			k := kindRow
			if optionCell(cell) {
				k = kindOptionRow
			}
			p := sp.add(k, span, pos, lineEnd)
			p.def, p.table = cell, len(sp.tables)
			t.rows = append(t.rows, p.ord)
		}
		line++
		if lineEnd >= to {
			break
		}
		pos = lineEnd + 1
	}
	sp.tables = append(sp.tables, t)
}

// splitLineKind is a line's kind and whether it starts a run of its own (a
// list item, a heading); a line after an item that is none of these
// continues the item.
func splitLineKind(line string, inItem bool) (int, bool) {
	trimmed := strings.TrimLeft(line, " ")
	switch {
	case strings.HasPrefix(trimmed, "|"):
		return lineTable, false
	case strings.HasPrefix(trimmed, ">"):
		return lineQuote, false
	case strings.HasPrefix(trimmed, "#"):
		return lineHeading, true
	case isListLine(line):
		return lineItem, true
	case inItem:
		return lineItem, false
	}
	return lineProse, false
}

var (
	// orderedItemRe matches the first line of an ordered list's item.
	orderedItemRe = regexp.MustCompile(`^\s{0,3}\d{1,3}[.)]\s`)
	// listMarkerRe matches a list item's marker.
	listMarkerRe = regexp.MustCompile(`^\s{0,3}(?:[-*+]|\d{1,3}[.)])\s+`)
	// delimiterRowRe matches a table's delimiter row: | --- | :-: |.
	delimiterRowRe = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)*\|?\s*$`)
	// noteRe matches text opening with Note, Warning or Important as a
	// label: "Note:", "**Warning**", "Important -".
	noteRe = regexp.MustCompile(`(?i)^[>\s]*(?:[*_]{0,2}(?:note|warning|important)[*_]{0,2}\s*(?::|—|–|!|$|-\s)|(?:\*\*|__)(?:note|warning|important)(?:\*\*|__)\s)`)
	// strayFenceRe matches a line that is a code fence but for its
	// indentation.
	strayFenceRe = regexp.MustCompile("(?m)^[ \t]*(?:```|~~~)")
	// admonitionRe matches an admonition's opening marker: > [!NOTE],
	// !!! tip, ::: warning.
	admonitionRe = regexp.MustCompile(`^(?:>\s*\[![A-Za-z]+\]|!!!\s*[A-Za-z]+|:::\s*[A-Za-z]+)`)
	// leadTermRe matches a definition term: a term set in bold or code at
	// the start, a type in parentheses after it or not (a parameter list's
	// "**name** (*str*) – …"), then a colon or a dash; a colon may sit
	// inside the bold ("**name:** …").
	leadTermRe = regexp.MustCompile(`^(?:\*\*([^*]+?)\s*:\*\*(?:\s|$)|(?:\*\*([^*]+)\*\*|__([^_]+)__|` + "`([^`]+)`" + `)` +
		`(?:\s*\([^()\n]*\))?\s*(?::(?:\s|$)|—|–|\s-\s))`)
)

// listBody is a list item's text after its marker.
func listBody(item string) string {
	return listMarkerRe.ReplaceAllString(item, "")
}

// definitionTerm is the term a piece defines when it opens like a
// definition list's term: **term**: …, `term` – …, **term** (type) – …;
// "" for none. A term in bold is a name, not a sentence: one holding
// sentence punctuation is none. A plain word before a colon ("Before:")
// and a bold or code word opening a sentence are no definition.
func definitionTerm(s string) string {
	s = strings.TrimLeft(s, "> \t")
	m := leadTermRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	for k, g := range m[1:] {
		if g != "" && (k == 3 || !strings.ContainsAny(g, ".!?")) {
			return g
		}
	}
	return ""
}

// firstCell is a table row's first cell, trimmed.
func firstCell(row string) string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	for i := 0; i < len(row); i++ {
		switch row[i] {
		case '\\':
			i++
		case '|':
			return strings.TrimSpace(row[:i])
		}
	}
	return strings.TrimSpace(row)
}

// optionCell reports a first cell that is an identifier or code: code
// (`x`), an identifier as code writes it (isCodeIdent), or a flag (--x).
func optionCell(cell string) bool {
	switch {
	case cell == "":
		return false
	case strings.HasPrefix(cell, "`") && strings.HasSuffix(cell, "`") && len(cell) > 2:
		return true
	case flagRe.MatchString(cell):
		return true
	}
	return !strings.ContainsAny(cell, " \t") && isCodeIdent(strings.Trim(cell, "*_()"))
}

var flagRe = regexp.MustCompile(`^--?[A-Za-z][\w-]*(?:[ =]\S+)?$`)

// isLabelText reports a line that is not a sentence: under four words
// with no closing punctuation (a tab's or file's name, "Menu", a model
// picker's options left in the page's text).
func isLabelText(t string) bool {
	t = strings.TrimRight(t, " *_)")
	return len(strings.Fields(t)) < 4 && !strings.ContainsAny(t[max(len(t)-1, 0):], ".!?:")
}

// sentenceRanges splits prose into sentences (sentenceStarts), as byte
// ranges without the spaces between them.
func sentenceRanges(s string) [][2]int {
	starts := sentenceStarts(s, 0, len(s))
	var out [][2]int
	for i, st := range starts {
		end := len(s)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		end = st + len(strings.TrimRight(s[st:end], " \n\t"))
		if st := st + (len(s[st:end]) - len(strings.TrimLeft(s[st:end], " \n\t"))); end > st {
			out = append(out, [2]int{st, end})
		}
	}
	return out
}
