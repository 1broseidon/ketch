package docs

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Coarse sections (design/docs-evidence.md, Round 13). A section is cut
// at its Markdown headings, and some pages have none where their entries
// start: nginx marks each directive with an anchor before its syntax
// table, a Sphinx API page each method with a definition term, and an
// RFC numbers its headings inside preformatted text, one block per
// printed page. Such a section runs to thousands of tokens, and an
// answer shows a sliver of it. A section longer than coarseSectionTokens
// whose page's anchors are known is split further at two structural
// marks, never by size:
//
//   - numbered headings in preformatted text that the page's anchors
//     name (splitNumbered), after the page blocks are joined and their
//     running headers and footers dropped;
//   - else the entries the page's other anchors name (splitEntries): a
//     block whose first code is the anchor's id, or whose whole line is
//     the anchor's text.
//
// Each part takes the mark's anchor, so a result's source lands on it and
// --section reads it alone; its breadcrumbs are the section's and the
// part's place below it.

// coarseSectionTokens is the size, in estimated tokens, past which a
// section is split at the marks inside it.
const coarseSectionTokens = 1500

// coarseMinMarks is how many marks a section needs before it is split:
// one match may be an accident, a run of them is the page's structure.
const coarseMinMarks = 2

// coarsePart is one part of a coarse section: its lines and, for every
// part but the lead, the mark that starts it.
type coarsePart struct {
	anchor  string   // the mark's anchor; "" for the lead, which stays the section
	heading string   // the part's heading
	path    []string // the headings between the section and the part, ending with heading
	lines   []string
}

// sectionParts returns a section's parts: the section whole, or split at
// its marks when it is coarse (longer than coarseSectionTokens) and the
// page's anchors are known. claimed are the anchors the page's Markdown
// headings already took.
func sectionParts(lines []string, text string, anchors []PageAnchor, claimed map[string]bool) []coarsePart {
	whole := []coarsePart{{lines: lines}}
	if anchors == nil || len(text) < coarseSectionTokens || estimateTokens(text) <= coarseSectionTokens {
		return whole
	}
	if parts := splitNumbered(lines, anchors, claimed); parts != nil {
		return parts
	}
	if parts := splitEntries(lines, anchors, claimed); parts != nil {
		return parts
	}
	return whole
}

// preLine is a line of a section and whether it is preformatted text
// (inside a code block).
type preLine struct {
	text string
	pre  bool
}

// numberedHeadingRe matches a numbered heading as an RFC prints it in
// plain text, "4.1.4.  "exp" (Expiration Time) Claim", "A.1.  Example",
// "Appendix A.  JWT Examples": the number, then the title.
var numberedHeadingRe = regexp.MustCompile(`^(?:Appendix\s+([A-Z])|((?:\d{1,3}|[A-Z])(?:\.\d{1,3})*))\.?\s+(\S.*)$`)

// dotLeaderRe matches a table of contents' dot leaders.
var dotLeaderRe = regexp.MustCompile(`\.\s?\.\s?\.`)

// numberedAnchor is an anchor whose text is a numbered heading.
type numberedAnchor struct {
	pos   int // in the page's anchors
	id    string
	text  string
	title string // idWords of the title after the number
}

// splitNumbered splits a section whose preformatted text holds numbered
// headings that the page's anchor texts name: anchor section-4.1.4, text
// `4.1.4. "exp" (Expiration Time) Claim`, and a line opening with
// "4.1.4." at the left margin of the preformatted text. The code blocks
// are joined first (joinPreformatted), so a sentence a page break cut is
// whole again. Guards: the heading line starts at the margin (a table of
// contents and a numbered list item are indented) and has no dot leaders;
// its title agrees with the anchor's text (one may be cut where the other
// wraps); the anchors are met in page order, each once. Fewer than
// coarseMinMarks headings leave the section whole (nil).
func splitNumbered(lines []string, anchors []PageAnchor, claimed map[string]bool) []coarsePart {
	byNumber := numberedAnchors(anchors, claimed)
	if len(byNumber) < coarseMinMarks {
		return nil
	}
	joined := joinPreformatted(lines)
	marks := numberedMarks(joined, byNumber)
	if len(marks) < coarseMinMarks {
		return nil
	}
	parts := []coarsePart{{lines: renderPreformatted(joined[:marks[0].line])}}
	headings := map[string]string{} // number → heading, of the marks met so far
	for k, m := range marks {
		end := len(joined)
		if k+1 < len(marks) {
			end = marks[k+1].line
		}
		headings[m.num] = m.a.text
		body := renderPreformatted(joined[m.line+1 : end])
		if !standsAlone(body) { // a heading right above its first subsection, or a line or two
			last := &parts[len(parts)-1]
			last.lines = append(last.lines, body...)
			continue
		}
		parts = append(parts, coarsePart{anchor: m.a.id, heading: m.a.text, path: numberedPath(m.num, headings), lines: body})
	}
	if len(parts)-1 < coarseMinMarks {
		return nil
	}
	return parts
}

// numberedAnchors are the page's anchors whose text is a numbered
// heading, by number, the headings' own aside.
func numberedAnchors(anchors []PageAnchor, claimed map[string]bool) map[string][]numberedAnchor {
	byNumber := map[string][]numberedAnchor{}
	for i, a := range anchors {
		if claimed[a.ID] || a.Text == "" {
			continue
		}
		text := collapseSpace(a.Text)
		if num, title, ok := numberedHeading(text); ok {
			byNumber[num] = append(byNumber[num], numberedAnchor{pos: i, id: a.ID, text: text, title: idWords(title)})
		}
	}
	return byNumber
}

// numberedMark is a heading line of joined preformatted text and the
// anchor it is.
type numberedMark struct {
	line int
	num  string
	a    numberedAnchor
}

// numberedMarks finds the heading lines of joined text, in the anchors'
// order, each anchor once.
func numberedMarks(joined []preLine, byNumber map[string][]numberedAnchor) []numberedMark {
	var marks []numberedMark
	last := -1
	for i, l := range joined {
		if !atMargin(l) {
			continue
		}
		num, title, ok := numberedHeading(strings.TrimSpace(l.text))
		if !ok {
			continue
		}
		if a, ok := nextNumbered(byNumber[num], last, idWords(title)); ok {
			marks = append(marks, numberedMark{line: i, num: num, a: a})
			last = a.pos
		}
	}
	return marks
}

// atMargin reports a line of preformatted text that starts at its left
// margin and has no dot leaders: where a heading stands, not a table of
// contents' entry or a numbered list item.
func atMargin(l preLine) bool {
	return l.pre && l.text != "" && l.text[0] != ' ' && l.text[0] != '\t' && !dotLeaderRe.MatchString(l.text)
}

// numberedHeading reads a numbered heading's number ("4.1.4", "A.1", "A"
// for an appendix) and its title.
func numberedHeading(s string) (num, title string, ok bool) {
	m := numberedHeadingRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	num = firstNonEmpty(m[1], m[2])
	if !strings.Contains(num, ".") && m[1] == "" && !strings.HasSuffix(strings.Fields(s)[0], ".") {
		return "", "", false // "4 JWT" or "A Claim" is no heading: a top-level number takes its dot
	}
	return num, m[3], true
}

// nextNumbered is the first of the anchors numbered alike past position
// last whose title agrees with the heading line's (title, as idWords):
// equal, or one the start of the other, where a long title wraps.
func nextNumbered(cands []numberedAnchor, last int, title string) (numberedAnchor, bool) {
	for _, a := range cands {
		if a.pos <= last || title == "" || a.title == "" {
			continue
		}
		if strings.HasPrefix(title, a.title) || strings.HasPrefix(a.title, title) {
			return a, true
		}
	}
	return numberedAnchor{}, false
}

// numberedPath is a numbered heading's place: the headings of its
// ancestors met so far (4 and 4.1 for 4.1.4; Appendix A for A.1), then
// its own.
func numberedPath(num string, headings map[string]string) []string {
	var path []string
	parts := strings.Split(num, ".")
	for i := 1; i < len(parts); i++ {
		if h, ok := headings[strings.Join(parts[:i], ".")]; ok {
			path = append(path, h)
		}
	}
	return append(path, headings[num])
}

// joinPreformatted reads a section's lines as text and preformatted
// text. Consecutive code blocks with only blank lines between them (an
// RFC's printed pages) are one run: their lines are joined, the running
// header and footer at each page break dropped (pageRunning), and the
// blank lines around the break with them, unless the line before it ends
// a sentence, when one blank line stays. A sentence a page break cut is
// one again.
func joinPreformatted(lines []string) []preLine {
	var out []preLine
	var run [][]string // the run's blocks so far
	var cur []string   // the open block's lines
	var fence fenceState
	blanks := 0 // blank lines since the run's last block
	flush := func() {
		for _, l := range joinPages(run) {
			out = append(out, preLine{text: l, pre: true})
		}
		if len(run) > 0 && blanks > 0 {
			out = append(out, preLine{})
		}
		run, blanks = nil, 0
	}
	for _, line := range lines {
		open := fence.n > 0
		if fence.step(line) {
			switch {
			case !open: // the opening line
				cur = []string{}
			case fence.n == 0: // the closing line
				run, cur, blanks = append(run, cur), nil, 0
			default:
				cur = append(cur, line)
			}
			continue
		}
		if strings.TrimSpace(line) == "" && len(run) > 0 {
			blanks++
			continue
		}
		flush()
		out = append(out, preLine{text: line})
	}
	if cur != nil { // a block left open at the end
		run = append(run, cur)
	}
	flush()
	return out
}

// joinPages joins a run's blocks into one text, page breaks removed.
func joinPages(blocks [][]string) []string {
	header, footer := pageRunning(blocks)
	var out []string
	for _, b := range blocks {
		lo, hi := trimBlank(b)
		if lo < hi && header != "" && runningKey(b[lo]) == header {
			lo++
		}
		if lo < hi && footer != "" && runningKey(b[hi-1]) == footer {
			hi--
		}
		body := b[lo:hi]
		if l, h := trimBlank(body); l < h {
			body = body[l:h]
		} else {
			continue
		}
		if len(out) > 0 && endsSentenceLine(out[len(out)-1]) {
			out = append(out, "")
		}
		out = append(out, body...)
	}
	return out
}

// trimBlank returns the range of b without its leading and trailing blank
// lines.
func trimBlank(b []string) (lo, hi int) {
	lo, hi = 0, len(b)
	for lo < hi && strings.TrimSpace(b[lo]) == "" {
		lo++
	}
	for hi > lo && strings.TrimSpace(b[hi-1]) == "" {
		hi--
	}
	return lo, hi
}

// pageRunning finds a run's running header and footer: the first and the
// last non-blank line of a block, digits aside (runningKey), when the
// same in at least three blocks and half of them ("RFC 7519  JSON Web
// Token (JWT)  May 2015" and "Jones, et al.  Standards Track  [Page 4]").
func pageRunning(blocks [][]string) (header, footer string) {
	firsts, lasts := map[string]int{}, map[string]int{}
	for _, b := range blocks {
		lo, hi := trimBlank(b)
		if lo < hi {
			firsts[runningKey(b[lo])]++
			lasts[runningKey(b[hi-1])]++
		}
	}
	pick := func(counts map[string]int) string {
		best, n := "", 0
		for k, c := range counts {
			if c > n || c == n && k < best {
				best, n = k, c
			}
		}
		if n < 3 || 2*n < len(blocks) {
			return ""
		}
		return best
	}
	return pick(firsts), pick(lasts)
}

// digitsRe matches a run of digits.
var digitsRe = regexp.MustCompile(`[0-9]+`)

// runningKey is a line with each run of digits as 0 and its spaces
// collapsed: what a running header or footer keeps from page to page
// ("[Page 9]" and "[Page 10]" alike).
func runningKey(line string) string {
	return collapseSpace(digitsRe.ReplaceAllString(line, "0"))
}

// endsSentenceLine reports a line that ends like a sentence or a clause,
// so a page break after it may end a paragraph.
func endsSentenceLine(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.ContainsAny(t[len(t)-1:], `.:;?!)]"'`)
}

// renderPreformatted writes lines back as Markdown: text as it was, and
// each paragraph of preformatted text (a run of non-blank lines) as a
// code block of its own, so an answer can show one paragraph of a long
// preformatted section.
func renderPreformatted(lines []preLine) []string {
	var out, para []string
	flush := func() {
		if len(para) == 0 {
			return
		}
		fence := codeFence(para)
		out = append(out, fence)
		out = append(out, para...)
		out = append(out, fence, "")
		para = nil
	}
	for _, l := range lines {
		switch {
		case l.pre && strings.TrimSpace(l.text) != "":
			para = append(para, l.text)
		case l.pre:
			flush()
		default:
			flush()
			out = append(out, l.text)
		}
	}
	flush()
	return out
}

// codeFence is a backtick fence longer than any run of backticks opening
// one of lines.
func codeFence(lines []string) string {
	n := 3
	for _, l := range lines {
		if t := strings.TrimLeft(l, " "); strings.HasPrefix(t, "```") {
			n = max(n, backtickRun(t, 0)+1)
		}
	}
	return strings.Repeat("`", n)
}

// entryAnchor is an anchor of the page that may name an entry.
type entryAnchor struct {
	pos      int
	id, text string
}

// citationRe matches a citation's label, "[RFC2616]", "[1]": a
// bibliography entry, which names a reference rather than an entry.
var citationRe = regexp.MustCompile(`^\s*\[[^\]]+\]\s*$`)

// footnoteIDRe matches the ids of footnotes, citations and back
// references, which name no entry.
var footnoteIDRe = regexp.MustCompile(`(?i)^(?:fn|fnref|footnote|footnote-ref|cite|cite_note|cite_ref|ref|note|back|backref|r)[-_:.]?\d+(?:[-_:.]\d+)*$`)

// splitEntries splits a section at the entries the page's anchors name,
// in page order, each anchor once: a block (after a blank line) whose
// first code span (the first cell's past its labels, in a table row) is
// the anchor's id followed by no other identifier character
// ("client_max_body_size size;"); or, for an anchor the page labels (a
// definition term, a heading), whose line, permalink glyphs aside, is
// the anchor's text (Sphinx's "run(host=None, port=None, …)¶" for
// #flask.Flask.run). Guards:
//
//   - a list item, a quote or a block that opens with a link is a
//     reference (a table of contents, a footnote), never an entry;
//   - an id that is a footnote's or citation's, or that has no letter,
//     and an anchor whose text is a citation's label ("[RFC2616]"), name
//     nothing; the anchors the page's headings took are left alone;
//   - a permalink is no mark: xml2rfc puts one on every paragraph, and a
//     signature that wraps carries it on its last line;
//   - anchors are met in page order; a section's entries are marked one
//     way, by labelled anchors or by unlabelled ones, whichever marks
//     more (nginx's parameters, labelled, inside its directives, not);
//   - an entry too short to stand alone joins the one before it, and a
//     section whose entries are mostly short (an index's references, a
//     list of one-line members, an API page's small methods) stays whole
//     (coarseMinEntryTokens): an answer already picks one piece of it
//     out, and a short entry printed whole as a group of its own crowds
//     the guide sections out of the budget.
//
// Fewer than coarseMinMarks entries leave the section whole (nil).
func splitEntries(lines []string, anchors []PageAnchor, claimed map[string]bool) []coarsePart {
	var labelled, bare []entryAnchor
	for i, a := range anchors {
		if claimed[a.ID] || !entryID(a.ID) || citationRe.MatchString(a.Text) {
			continue
		}
		e := entryAnchor{pos: i, id: a.ID, text: collapseSpace(a.Text)}
		if e.text != "" {
			labelled = append(labelled, e)
		} else {
			bare = append(bare, e)
		}
	}
	marks := entryMarks(lines, labelled)
	if other := entryMarks(lines, bare); len(other) > len(marks) {
		marks = other
	}
	if len(marks) < coarseMinMarks {
		return nil
	}
	parts := []coarsePart{{lines: lines[:marks[0].line]}}
	for k, m := range marks {
		end := len(lines)
		if k+1 < len(marks) {
			end = marks[k+1].line
		}
		if !standsAlone(lines[m.line:end]) {
			last := &parts[len(parts)-1]
			last.lines = append(slices.Clone(last.lines), lines[m.line:end]...)
			continue
		}
		h := entryHeading(m.a)
		parts = append(parts, coarsePart{anchor: m.a.id, heading: h, path: []string{h}, lines: lines[m.line:end]})
	}
	if len(parts)-1 < coarseMinMarks || medianTokens(parts[1:]) < coarseMinEntryTokens {
		return nil
	}
	return parts
}

// coarseMinEntryTokens is the median size, in estimated tokens, a
// section's entries need for it to be split at them. Measured on the
// check tier's tune half (design/docs-evidence.md, Round 13): at 40 and
// 60 Flask's and redis-py's API members displaced their guides; nginx's
// directives, at a median of about 115, are kept.
const coarseMinEntryTokens = 100

// entryMark is a line that opens the entry an anchor names.
type entryMark struct {
	line int
	a    entryAnchor
}

// entryMarks finds the lines that open the entries of cands (anchors of
// one kind), in page order, each anchor once.
func entryMarks(lines []string, cands []entryAnchor) []entryMark {
	var marks []entryMark
	next := 0 // cands before it are met or passed
	var fence fenceState
	prevBlank := true
	for i, line := range lines {
		if fence.step(line) {
			prevBlank = fence.n == 0 // a block may open right after a code block
			continue
		}
		blank := strings.TrimSpace(line) == ""
		start := prevBlank && !blank
		prevBlank = blank
		if !start || !entryLine(line) || next >= len(cands) {
			continue
		}
		code := leadCode(line)
		clean := idWords(strings.ReplaceAll(cleanInline(line), "¶", ""))
		for j := next; j < len(cands); j++ {
			if namesEntry(line, code, clean, cands[j]) {
				marks = append(marks, entryMark{line: i, a: cands[j]})
				next = j + 1
				break
			}
		}
	}
	return marks
}

// standsAlone reports an entry's lines that would make a section of their
// own: as much text as the smallest section a page keeps.
func standsAlone(lines []string) bool {
	text := linkLabels(strings.Join(cleanSectionLines(lines), "\n"))
	return len(strings.Join(strings.Fields(text), "")) >= webMinSectionText
}

// medianTokens is the median estimated size of parts' text.
func medianTokens(parts []coarsePart) int {
	sizes := make([]int, len(parts))
	for i, p := range parts {
		sizes[i] = estimateTokens(linkLabels(strings.Join(cleanSectionLines(p.lines), "\n")))
	}
	slices.Sort(sizes)
	return sizes[len(sizes)/2]
}

// entryID reports an anchor id that may name an entry: one with a letter
// that is no footnote's, citation's or back reference's.
func entryID(id string) bool {
	return strings.ContainsFunc(id, unicode.IsLetter) && !footnoteIDRe.MatchString(id)
}

// entryLine reports a line that may open an entry: not a list item, a
// quote or a heading, and not opening with a link (a reference to the
// entry, as a table of contents has).
func entryLine(line string) bool {
	t := strings.TrimSpace(line)
	if listItemRe.MatchString(line) || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "#") {
		return false
	}
	t = strings.TrimLeft(t, "|*_ ")
	return !strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "![")
}

// namesEntry reports whether line opens the entry anchor a names: its
// lead code is the id, or its words are the labelled anchor's text.
func namesEntry(line, code, clean string, a entryAnchor) bool {
	if code != "" && hasIdentPrefix(code, a.id) {
		return true
	}
	if a.text == "" {
		return false
	}
	return clean != "" && clean == idWords(a.text)
}

// hasIdentPrefix reports code that opens with id, followed by nothing or
// by a character no identifier holds ("client_max_body_size size;" opens
// with client_max_body_size, not with client_max_body).
func hasIdentPrefix(code, id string) bool {
	if !strings.HasPrefix(code, id) {
		return false
	}
	if len(code) == len(id) {
		return true
	}
	c := code[len(id)]
	return c != '_' && c != '-' && c != '.' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9')
}

// leadCode is the code a block opens with: its first code span, or in a
// table row the first cell's past the cells that are labels ("Syntax:");
// "" when the block opens with anything else.
func leadCode(line string) string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "|") {
		for _, cell := range rowCells(t) {
			c := strings.TrimSpace(cell)
			if c == "" || strings.HasSuffix(cleanInline(c), ":") {
				continue
			}
			t = c
			break
		}
	}
	t = strings.TrimLeft(t, "*_ ")
	if !strings.HasPrefix(t, "`") {
		return ""
	}
	n := backtickRun(t, 0)
	end := closingBackticks(t, n, n)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(t[n:end])
}

// rowCells splits a table row into its cells; a "|" inside a code span or
// escaped is part of its cell.
func rowCells(row string) []string {
	row = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(row), "|"), "|")
	var cells []string
	start := 0
	for i := 0; i < len(row); i++ {
		switch row[i] {
		case '\\':
			i++
		case '`':
			n := backtickRun(row, i)
			if end := closingBackticks(row, i+n, n); end >= 0 {
				i = end + n - 1
			} else {
				i += n - 1
			}
		case '|':
			cells = append(cells, row[start:i])
			start = i + 1
		}
	}
	return append(cells, row[start:])
}

// entryHeading is an entry part's heading: the anchor's text when it is
// short enough to read as a name, else its id.
func entryHeading(a entryAnchor) string {
	if a.text != "" && len(a.text) <= 80 {
		return a.text
	}
	return a.id
}
