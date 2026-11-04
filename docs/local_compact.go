package docs

import (
	"math"
	"sort"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Compact answers: the section view within a budget the caller sets
// (ADR-0007, amendment 3). Whole sections are large, so at an agent's
// budget (about 880 tokens) only two or three fit, where a curated docs
// service fits four or five short snippets. The ranking is not the
// problem; the size is. So with a budget the same ranked families are
// rendered compactly: each result gets a share of the budget weighted by
// its rank (the first the most), a family that fits its share is shown
// whole, and one that does not keeps, within its share, its heading path,
// its first sentence, the code block that names the question best (or
// its first) whole with the sentence leading into it, the sentences that
// name the question best (three in all), then whatever else names it, by
// score. The question's words count by how few of the candidates' pieces
// name them, so the library's own name, which every piece repeats, counts
// little. Subsections of the family that did not rank are left out; code
// is never cut; "…" stands where sentences of a paragraph were skipped
// and one "[…]" ends a result that leaves anything out. What the results
// leave of their shares goes to the others, best first. Tokens are
// estimated as o200k counts them (estimateTokens), not as bytes.

const (
	// localShareDecay sets how a result's share weight falls with its
	// rank r (0 for the first): 1/(1+r/localShareDecay), so five results
	// take 38, 23, 16, 13 and 10% of the budget before any is capped at
	// its whole size.
	localShareDecay = 1.5
	// localMinShare is the least share a result is shown with: its
	// heading path, source and a sentence or a short block. Fewer results
	// are shown when the budget cannot give each this much (at 880
	// tokens, four or five).
	localMinShare = 90
	// localLeadSentences is the most sentences a compact result keeps
	// before the rest by score, its first sentence among them.
	localLeadSentences = 3
	// localFirstWeight is what a section's first sentence scores beyond
	// the question's words it names, when it names any: it usually says
	// what the section is about.
	localFirstWeight = 0.5
	// localTokenMargin: a compact answer is planned to 1/localTokenMargin
	// under its budget, about the estimate's error on a whole answer.
	localTokenMargin = 32
	// localCodeTie: a code block scoring at least this share of the best
	// one's score stands in for it when the best does not fit.
	localCodeTie = 0.75
)

// pieceKind is what a piece of a section's text is.
type pieceKind int

const (
	pieceSentence pieceKind = iota // a sentence of a paragraph
	pieceItem                      // a list item
	pieceCode                      // a fenced code block, with its tab label
	pieceBlock                     // a table or quote, shown whole
)

// piece is a verbatim byte range of a section's text that a compact
// result can keep or leave out.
type piece struct {
	part       int // the result's part (section) it belongs to
	group      int // the paragraph, list or block it is in, within its part
	kind       pieceKind
	start, end int
	text       string
	cost       int                // estimated tokens, with its separator
	terms      map[string]float64 // the query terms it names: 1, or prefixMatch's share
	first      bool               // the result's lead: its first sentence
	opens      bool               // its part's first sentence
	score      float64
}

// compactFamily is one family's candidate result: the sections it can
// show and their pieces.
type compactFamily struct {
	fam       *family
	parts     []int // section indices, page order
	pieces    []piece
	chosen    map[int]bool
	whole     Result // the family whole, as familyResult renders it
	wholeCost int
	title     string
	crumbs    []string
	useWhole  bool
	// overheadCost is overhead's estimate, set once; partHeadings what
	// each part's heading line costs (0 for the title's section).
	overheadCost int
	partHeadings []int
	used         int   // tokens chosen so far, overhead included
	pending      []int // pieces wanted first when more budget comes (a code block that did not fit)
}

// compactResults renders the families within tokens (estimated o200k
// tokens of the rendered results): rank-weighted shares, a family whole
// when it fits its share, else its compact form; at most len(fams).
func compactResults(lib docsindex.Library, secs []docsindex.Section, fams []*family, query string, tokens int) []Result {
	terms := queryTerms(query)
	idents := queryIdents(query)
	cands := make([]*compactFamily, len(fams))
	for i, f := range fams {
		cands[i] = newCompactFamily(lib, secs, f, terms)
	}
	weighTerms(cands, terms, idents)
	// The estimate runs a few percent either side of o200k's count;
	// planning to a little under the budget keeps the answer within it.
	tokens -= tokens / localTokenMargin
	n, shares := compactShares(cands, tokens)
	cands = cands[:n]
	used := 0
	for i, c := range cands {
		if c.wholeCost <= shares[i] {
			c.useWhole = true
			used += c.wholeCost
			continue
		}
		c.core(shares[i])
		c.extend(shares[i] - c.used)
		used += c.used
	}
	// What the shares left goes to the results that are cut, best first:
	// a code block that did not fit, then the rest by score.
	left := tokens - used
	for _, c := range cands {
		if !c.useWhole && left > 0 {
			left -= c.extend(left)
		}
	}
	out := make([]Result, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.result(lib, secs))
	}
	return fitTokens(out, cands, lib, secs, tokens)
}

// rankWeight is the share weight of the result at rank r (0 the first).
func rankWeight(r int) float64 { return 1 / (1 + float64(r)/localShareDecay) }

// fitTokens drops pieces from the last results until the rendered results'
// estimate is within tokens (the per-piece sums run a little under the
// joined text's), and results left with no piece.
func fitTokens(out []Result, cands []*compactFamily, lib docsindex.Library, secs []docsindex.Section, tokens int) []Result {
	total := func() int {
		t := 0
		for _, r := range out {
			t += estimateTokens(RenderSnippet(r))
		}
		return t
	}
	for len(out) > 1 && total() > tokens {
		i := len(out) - 1
		c := cands[i]
		if c.useWhole || !c.dropOne() {
			out = out[:i]
			continue
		}
		out[i] = c.result(lib, secs)
	}
	var kept []Result
	for i, r := range out {
		if cands[i].useWhole || len(cands[i].chosen) > 0 {
			kept = append(kept, r)
		}
	}
	return kept
}

// newCompactFamily cuts the sections a family result can show
// (familyParts: its best section, the H2's own text and its other ranked
// sections, in page order) into pieces; the first sentence among them
// leads.
func newCompactFamily(lib docsindex.Library, secs []docsindex.Section, f *family, terms []string) *compactFamily {
	c := &compactFamily{fam: f, chosen: map[int]bool{}}
	c.whole = familyResult(lib, secs, f, math.MaxInt/4, terms)
	c.wholeCost = estimateTokens(RenderSnippet(c.whole))
	c.parts = familyParts(secs, f, 0)
	head := secs[f.members[0]]
	c.title, c.crumbs = head.Heading, head.Crumbs
	if len(c.crumbs) > 2 {
		c.crumbs = c.crumbs[:2]
		c.title = c.crumbs[1]
	}
	c.overheadCost = c.overhead()
	c.partHeadings = make([]int, len(c.parts))
	for p, m := range c.parts {
		if secs[m].Heading != c.title {
			c.partHeadings[p] = estimateTokens("#### "+secs[m].Heading) + 1
		}
		opened := false
		for _, pc := range textPieces(secs[m].Text) {
			pc.part = p
			if pc.kind == pieceSentence && !opened {
				pc.opens, opened = true, true
			}
			pc.terms = pieceTerms(pc.text, terms)
			c.pieces = append(c.pieces, pc)
		}
	}
	for i := range c.pieces {
		if c.pieces[i].opens {
			c.pieces[i].first = true
			break
		}
	}
	return c
}

// pieceTerms are the query terms text names (camel-case words and
// letter-digit parts included), 1 each, or by prefix (sharex for share)
// at prefixMatch's share.
func pieceTerms(text string, terms []string) map[string]float64 {
	toks := map[string]bool{}
	for _, t := range textTokens(text) {
		toks[t] = true
		// load_in_8bit names 8 and bit, as "8-bit" does.
		for _, part := range digitSplit(t) {
			toks[normTerm(stem(part))] = true
		}
	}
	out := map[string]float64{}
	for _, t := range terms {
		if toks[t] {
			out[t] = 1
		} else if m := prefixMatch(toks, t); m > 0 {
			out[t] = m
		}
	}
	return out
}

// digitSplit splits a token at its letter-digit boundaries (8bit: 8, bit);
// nil for a token of letters or digits only.
func digitSplit(t string) []string {
	var parts []string
	from := 0
	for i := 1; i < len(t); i++ {
		if isDigitByte(t[i]) != isDigitByte(t[i-1]) {
			parts = append(parts, t[from:i])
			from = i
		}
	}
	if from == 0 {
		return nil
	}
	return append(parts, t[from:])
}

func isDigitByte(b byte) bool { return b >= '0' && b <= '9' }

// weighTerms scores every piece: the inverse document frequency, over all
// the candidates' pieces, of each query term it names (an identifier the
// query writes counts twice), so the library's own name, which every
// piece repeats, weighs little; a part's first sentence gains
// localFirstWeight.
func weighTerms(cands []*compactFamily, terms, idents []string) {
	df := map[string]int{}
	n := 0
	for _, c := range cands {
		for _, p := range c.pieces {
			n++
			for t := range p.terms {
				df[t]++
			}
		}
	}
	weight := map[string]float64{}
	for _, t := range terms {
		weight[t] = math.Log(1 + float64(n)/float64(1+df[t]))
	}
	for _, id := range idents {
		if w, ok := weight[id]; ok {
			weight[id] = 2 * w
		}
	}
	for _, c := range cands {
		for i := range c.pieces {
			p := &c.pieces[i]
			for t, m := range p.terms {
				p.score += m * weight[t]
			}
			if p.opens && len(p.terms) > 0 {
				p.score += localFirstWeight
			}
		}
	}
}

// compactShares is how many results to show and each one's share of
// tokens: weighted by rank (localShareDecay), none more than its whole
// size, the rest passed on to the others in proportion; the most results
// that each get at least localMinShare.
func compactShares(cands []*compactFamily, tokens int) (int, []int) {
	for n := len(cands); n > 1; n-- {
		shares := weightedShares(cands[:n], tokens)
		ok := true
		for i, sh := range shares {
			ok = ok && sh >= min(localMinShare, cands[i].wholeCost)
		}
		if ok {
			return n, shares
		}
	}
	return min(len(cands), 1), []int{tokens}
}

// weightedShares splits tokens among the candidates by rank weight,
// water-filling: a candidate whose whole size is under its share takes
// its size, and the rest is split again among the others.
func weightedShares(cands []*compactFamily, tokens int) []int {
	shares := make([]int, len(cands))
	capped := make([]bool, len(cands))
	left := tokens
	for {
		sum := 0.0
		for i := range cands {
			if !capped[i] {
				sum += rankWeight(i)
			}
		}
		if sum == 0 {
			return shares
		}
		changed, spent := false, 0
		for i, c := range cands {
			if capped[i] {
				continue
			}
			shares[i] = int(float64(left) * rankWeight(i) / sum)
			if c.wholeCost <= shares[i] {
				shares[i], capped[i], changed = c.wholeCost, true, true
				spent += c.wholeCost
			}
		}
		if !changed {
			return shares
		}
		left -= spent
	}
}

// overhead is a compact result's cost besides its pieces: heading path,
// source line and the elision marker.
func (c *compactFamily) overhead() int {
	r := c.whole
	r.Snippet = elidedMarker
	return estimateTokens(RenderSnippet(r))
}

// core chooses the result's first sentence and one code block, whole,
// with the sentence leading into it, within room tokens: the block naming
// the question best that fits (of those scoring within localCodeTie of
// the best; the smaller of equals), else the first. A block that does not
// fit waits for extend. It returns the tokens used, its overhead included.
func (c *compactFamily) core(room int) int {
	c.used = c.overheadCost
	add := func(i int) bool { return c.take(i, room) }
	for i, p := range c.pieces {
		if p.first {
			add(i)
		}
	}
	codes := c.byScore(func(p piece) bool { return p.kind == pieceCode })
	if len(codes) > 0 {
		best := c.pieces[codes[0]].score
		var near []int
		for _, i := range codes {
			if c.pieces[i].score >= best*localCodeTie {
				near = append(near, i)
			}
		}
		sort.SliceStable(near, func(a, b int) bool {
			pa, pb := c.pieces[near[a]], c.pieces[near[b]]
			return pa.score > pb.score || pa.score == pb.score && pa.cost < pb.cost
		})
		codes = near
	} else {
		codes = c.firstCode()
	}
	for _, i := range codes {
		if add(i) {
			c.addLeadIn(i, add)
			return c.used
		}
	}
	if len(codes) > 0 {
		c.pending = append(c.pending, codes[0])
	}
	return c.used
}

// extend adds pieces within left tokens: a code block core could not
// fit, the sentences naming the question best (up to localLeadSentences
// in all), then the rest by score. It returns the tokens used.
func (c *compactFamily) extend(left int) int {
	before := c.used
	limit := c.used + left
	add := func(i int) bool { return c.take(i, limit) }
	if !c.hasCode() {
		for _, i := range c.pending {
			if add(i) {
				c.addLeadIn(i, add)
			}
		}
	}
	for _, i := range c.byScore(func(p piece) bool { return p.kind == pieceSentence }) {
		if c.countKind(pieceSentence) >= localLeadSentences {
			break
		}
		add(i)
	}
	for _, i := range c.byScore(func(piece) bool { return true }) {
		add(i)
	}
	return c.used - before
}

// take chooses piece i when the result stays within limit tokens with it
// (and its part's heading, the first time the part shows).
func (c *compactFamily) take(i, limit int) bool {
	if c.chosen[i] {
		return false
	}
	cost := c.pieces[i].cost + c.headingCost(c.pieces[i].part)
	if c.used+cost > limit {
		return false
	}
	c.chosen[i] = true
	c.used += cost
	return true
}

// headingCost is what showing part p's heading costs: nothing when one of
// its pieces is already chosen or it is the title's own section.
func (c *compactFamily) headingCost(p int) int {
	if c.partHeadings[p] == 0 {
		return 0
	}
	for i := range c.chosen {
		if c.pieces[i].part == p {
			return 0
		}
	}
	return c.partHeadings[p]
}

// addLeadIn adds the sentence or item right before code piece i, when
// the paragraph it ends leads into the code.
func (c *compactFamily) addLeadIn(i int, add func(int) bool) {
	if i == 0 {
		return
	}
	prev := c.pieces[i-1]
	if prev.part == c.pieces[i].part && prev.kind != pieceCode && prev.kind != pieceBlock {
		add(i - 1)
	}
}

func (c *compactFamily) countKind(k pieceKind) int {
	n := 0
	for i := range c.chosen {
		if c.pieces[i].kind == k {
			n++
		}
	}
	return n
}

func (c *compactFamily) hasCode() bool {
	for i := range c.chosen {
		if c.pieces[i].kind == pieceCode {
			return true
		}
	}
	return false
}

// firstCode is the first code block of the best part, or of any part.
func (c *compactFamily) firstCode() []int {
	first := -1
	for i, p := range c.pieces {
		if p.kind != pieceCode {
			continue
		}
		if c.parts[p.part] == c.fam.best {
			return []int{i}
		}
		if first < 0 {
			first = i
		}
	}
	if first < 0 {
		return nil
	}
	return []int{first}
}

// byScore lists the pieces keep accepts that name the question, best
// first, ties in text order.
func (c *compactFamily) byScore(keep func(piece) bool) []int {
	var out []int
	for i, p := range c.pieces {
		if p.score > 0 && keep(p) {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return c.pieces[out[a]].score > c.pieces[out[b]].score })
	return out
}

// dropOne leaves out the chosen piece that scores least (the lead
// sentence last); false when nothing is left.
func (c *compactFamily) dropOne() bool {
	worst := -1
	for i := range c.chosen {
		if worst < 0 {
			worst = i
			continue
		}
		a, b := c.pieces[i], c.pieces[worst]
		switch {
		case a.first != b.first:
			if b.first {
				worst = i
			}
		case a.score < b.score || a.score == b.score && i > worst:
			worst = i
		}
	}
	if worst < 0 {
		return false
	}
	delete(c.chosen, worst)
	return len(c.chosen) > 0
}

// result renders the family: whole, or its chosen pieces in text order,
// each part's heading before its pieces (but the title's), "…" between
// sentences of one paragraph that skip some, and "[…]" at the end when
// anything was left out.
func (c *compactFamily) result(lib docsindex.Library, secs []docsindex.Section) Result {
	if c.useWhole {
		return c.whole
	}
	var b strings.Builder
	var anchors []string
	prev := -1
	for i, p := range c.pieces {
		if !c.chosen[i] {
			continue
		}
		s := secs[c.parts[p.part]]
		switch {
		case prev < 0 || c.pieces[prev].part != p.part:
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			if s.Heading != c.title {
				b.WriteString("#### " + s.Heading + "\n\n")
			}
			anchors = append(anchors, s.Anchor)
		case c.pieces[prev].group == p.group && p.kind == pieceSentence:
			if prev == i-1 {
				b.WriteString(s.Text[c.pieces[prev].end:p.start])
			} else {
				b.WriteString(" … ")
			}
		case c.pieces[prev].group == p.group && p.kind == pieceItem:
			b.WriteString("\n")
		default:
			b.WriteString("\n\n")
		}
		b.WriteString(p.text)
		prev = i
	}
	if len(c.chosen) < len(c.pieces) || len(c.parts) < len(c.fam.members) {
		b.WriteString("\n\n" + elidedMarker)
	}
	return Result{
		Library: lib.Root, Version: lib.Version, Title: c.title, Breadcrumb: strings.Join(c.crumbs, " > "),
		Snippet: b.String(), URL: webSectionOf(secs[c.fam.best], 0).URL(), Anchors: anchors, Source: "local",
	}
}

// textPieces cuts a section's text into pieces, in order: each fenced code
// block (with the tab label right before it), each sentence of a
// paragraph, each list item, and each table or quote whole. Headings
// inside the text are left out.
func textPieces(text string) []piece {
	var out []piece
	spans := textSpans(text)
	for g, sp := range spans {
		switch {
		case sp.code:
			start := sp.start
			if g > 0 && spans[g-1].label && !spans[g-1].code {
				start = spans[g-1].start
			}
			out = append(out, newPiece(text, pieceCode, g, start, sp.end))
		case sp.label && g+1 < len(spans) && spans[g+1].code:
			// joined to the code after it
		default:
			out = append(out, proseSpanPieces(text, g, sp.start, sp.end)...)
		}
	}
	return out
}

// proseSpanPieces cuts a paragraph run (which may hold list items, table
// rows, quotes or headings) into pieces by its lines' kinds: prose into
// sentences, each list item (with its continuation lines) whole, each run
// of table or quote lines whole; heading lines are left out.
func proseSpanPieces(text string, group, start, end int) []piece {
	var out []piece
	kind, from, runEnd := -1, start, start
	flush := func() {
		if kind < 0 || from >= runEnd {
			return
		}
		switch kind {
		case lineProse:
			for _, r := range sentenceRanges(text[from:runEnd]) {
				if !isLabelText(text[from+r[0] : from+r[1]]) {
					out = append(out, newPiece(text, pieceSentence, group, from+r[0], from+r[1]))
				}
			}
		case lineItem:
			out = append(out, newPiece(text, pieceItem, group, from, runEnd))
		case lineBlock:
			out = append(out, newPiece(text, pieceBlock, group, from, runEnd))
		}
	}
	for pos := start; pos <= end; {
		lineEnd := end
		if nl := strings.IndexByte(text[pos:end], '\n'); nl >= 0 {
			lineEnd = pos + nl
		}
		k, starts := lineKind(text[pos:lineEnd], kind == lineItem)
		if k != kind || starts {
			flush()
			kind, from = k, pos
		}
		runEnd = lineEnd
		if lineEnd >= end {
			break
		}
		pos = lineEnd + 1
	}
	flush()
	return out
}

// isLabelText reports a line that is not a sentence: under four words
// with no closing punctuation (a tab's or file's name, "Menu", a model
// picker's options left in the page's text).
func isLabelText(t string) bool {
	t = strings.TrimRight(t, " *_)")
	return len(strings.Fields(t)) < 4 && !strings.ContainsAny(t[max(len(t)-1, 0):], ".!?:")
}

// Line kinds, for proseSpanPieces.
const (
	lineProse = iota
	lineItem
	lineBlock // a table row or quote
	lineHeading
)

// lineKind is a line's kind, and whether it starts a run of its own (a
// list item, a heading); a line after an item that is none of these
// continues the item.
func lineKind(line string, inItem bool) (int, bool) {
	trimmed := strings.TrimLeft(line, " ")
	switch {
	case strings.HasPrefix(trimmed, "|"), strings.HasPrefix(trimmed, ">"):
		return lineBlock, false
	case strings.HasPrefix(trimmed, "#"):
		return lineHeading, true
	case isListLine(line):
		return lineItem, true
	case inItem:
		return lineItem, false
	}
	return lineProse, false
}

func newPiece(text string, kind pieceKind, group, start, end int) piece {
	t := text[start:end]
	return piece{group: group, kind: kind, start: start, end: end, text: t, cost: estimateTokens(t) + 1}
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
