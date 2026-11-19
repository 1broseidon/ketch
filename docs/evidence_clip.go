package docs

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"unicode"
)

// A hard bound on the docs text (design/docs-evidence.md, Final shape,
// Fourth pass). The walk prints a section whole or cut to its pieces, and
// a piece is a block: a paragraph, a list item, a table row, a code block.
// One block can be larger than the whole budget (a man page served as one
// <pre>, a page that is one paragraph of tens of thousands of characters).
// The first section always prints something, so before this such a block
// printed whole, whatever the budget. Now a block larger than what is left
// is cut inside the block: its lines that hold the question's terms, the
// best first, with clipContext lines around each, printed in page order
// with "…" between runs, a fenced code block's fences and a table's header
// and delimiter rows kept. With no line
// holding a term, its first lines. A line longer than what is left is cut
// at a word boundary around its first term. The group says (cut, N of M
// tokens), and --section reads the rest.

// clipContext is how many lines around a line holding a term are kept.
const clipContext = 2

// clipped is pl (a section cut to a unit that does not fit what is left)
// with its text cut inside its blocks to fit: the plan prints clip instead
// of its pieces' text.
func (p *planner) clipped(pl plan) plan {
	sec := p.s.secs[pl.sec]
	text := sec.Text
	if len(pl.pieces) < len(sec.pieces) {
		text = joinPieces(p.s, sec, pl.pieces)
	}
	empty := plan{sec: pl.sec, pieces: pl.pieces, clip: "…"}
	fixed := p.render(empty) + p.overhead(empty) - estimateTokens("…")
	room := p.left - fixed
	for range 4 {
		out := plan{sec: pl.sec, pieces: pl.pieces, clip: clipText(text, p.r.terms, p.r.weights, max(room, 1))}
		out.cost = p.render(out) + p.overhead(out)
		if out.cost <= p.left || room <= 1 {
			return out
		}
		room -= out.cost - p.left
	}
	out := plan{sec: pl.sec, pieces: pl.pieces, clip: clipText(text, p.r.terms, p.r.weights, 1)}
	out.cost = p.render(out) + p.overhead(out)
	return out
}

// clipText is text cut to at most room tokens (estimateTokens) at line
// boundaries: the lines holding the question's terms, best first
// (clipOrder), each with clipContext lines around it while they fit,
// printed in order with "…" where lines were left out; a fenced block's
// opening and closing fence lines and a table's header and delimiter rows
// are kept. With no line holding a term, its first lines. Text that fits
// is returned as it is.
func clipText(text string, terms []evidenceTerm, weights []float64, room int) string {
	if estimateTokens(text) <= room {
		return text
	}
	c := newClipper(text, room)
	if len(c.body) == 0 || estimateTokens(c.frame("…")) > room {
		return clipLine(strings.Join(c.lines, " "), terms, room)
	}
	order := clipOrder(c.body, terms, weights)
	if len(order) == 0 {
		// No line holds a term: the first lines, as many as fit.
		for i := range c.body {
			if !c.try(i, i+1) {
				break
			}
		}
		return c.render()
	}
	kept, misses := false, 0
	for _, i := range order {
		if c.try(i-clipContext, i+clipContext+1) || c.try(i, i+1) {
			kept, misses = true, 0
		} else if misses++; misses >= clipMisses {
			break
		}
	}
	if !kept {
		// Not one line holding a term fits: the best one, cut at words.
		return c.frame("…\n" + clipLine(c.body[order[0]], terms, room-estimateTokens(c.frame("…\n…"))) + "\n…")
	}
	return c.render()
}

// clipper is the state of clipText: the text's lines, its frame (head
// lines leading and tail lines closing it, kept), its body between them
// and which of the body's lines are kept.
type clipper struct {
	lines, body []string
	head, tail  int
	keep        []bool
	room        int
}

func newClipper(text string, room int) *clipper {
	c := &clipper{lines: strings.Split(text, "\n"), room: room}
	c.head, c.tail = clipFrame(c.lines)
	c.body = c.lines[c.head : len(c.lines)-c.tail]
	c.keep = make([]bool, len(c.body))
	return c
}

// frame is mid between the frame's lines.
func (c *clipper) frame(mid string) string {
	parts := slices.Clone(c.lines[:c.head])
	if mid != "" {
		parts = append(parts, mid)
	}
	return strings.Join(append(parts, c.lines[len(c.lines)-c.tail:]...), "\n")
}

// render is the kept lines in their frame, "…" where lines were left out.
func (c *clipper) render() string {
	var parts []string
	gap := false
	for i, l := range c.body {
		if !c.keep[i] {
			gap = true
			continue
		}
		if gap && (len(parts) == 0 || parts[len(parts)-1] != "…") {
			parts = append(parts, "…")
		}
		gap = false
		parts = append(parts, l)
	}
	if gap {
		parts = append(parts, "…")
	}
	return c.frame(strings.Join(parts, "\n"))
}

// try keeps the body's lines [from, to) when the text still fits with
// them, and reports whether it does.
func (c *clipper) try(from, to int) bool {
	var added []int
	for i := max(from, 0); i < min(to, len(c.body)); i++ {
		if !c.keep[i] {
			c.keep[i] = true
			added = append(added, i)
		}
	}
	if len(added) == 0 || estimateTokens(c.render()) <= c.room {
		return true
	}
	for _, i := range added {
		c.keep[i] = false
	}
	return false
}

// clipMisses is how many lines in a row may not fit before a cut stops
// trying the rest: the room is spent.
const clipMisses = 16

// clipOrder is the indexes of the lines holding a term, the best first:
// by the weight of the terms each holds, a term weighing more the fewer
// of the lines hold it (a word on every line of a man page says little of
// where the answer is), then in order; none under 1/clipShare of the
// best's.
func clipOrder(lines []string, terms []evidenceTerm, weights []float64) []int {
	held := make([][]int, len(lines)) // by line: the terms it holds
	freq := make([]int, len(terms))
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		toks := pieceTokens(l)
		for ti, t := range terms {
			if toks[t.term] {
				held[i] = append(held[i], ti)
				freq[ti]++
			}
		}
	}
	score := make([]float64, len(lines))
	var out []int
	for i, ts := range held {
		for _, ti := range ts {
			score[i] += max(weights[ti], 0.01) * math.Log(1+float64(len(lines))/float64(freq[ti]))
		}
		if score[i] > 0 {
			out = append(out, i)
		}
	}
	slices.SortStableFunc(out, func(a, b int) int { return cmp.Compare(score[b], score[a]) })
	for k, i := range out {
		if score[i] < score[out[0]]/clipShare {
			return out[:k]
		}
	}
	return out
}

// clipShare: a line scoring under 1/clipShare of the best line's is not
// kept for its terms (as the walk stops under a quarter of its first
// choice).
const clipShare = 4

// clipFrame is how many lines lead and close text that a cut keeps: a
// fenced block's opening line (with a label line before it) and closing
// fence; a table's header and delimiter rows.
func clipFrame(lines []string) (head, tail int) {
	if len(lines) >= 3 && strings.HasPrefix(strings.TrimSpace(lines[0]), "|") && delimiterRowRe.MatchString(lines[1]) {
		return 2, 0
	}
	for i := 0; i < len(lines) && i < 2; i++ {
		if _, _, _, ok := fenceMarker(lines[i]); ok {
			head = i + 1
			break
		}
	}
	if head > 0 && len(lines) > head {
		if _, _, info, ok := fenceMarker(lines[len(lines)-1]); ok && strings.TrimSpace(info) == "" {
			tail = 1
		}
	}
	return head, tail
}

// clipLine is line cut at word boundaries to at most room tokens, around
// its first word holding a term (from the start when none does), with "…"
// where it was cut.
func clipLine(line string, terms []evidenceTerm, room int) string {
	if estimateTokens(line) <= room {
		return line
	}
	words := strings.FieldsFunc(line, unicode.IsSpace)
	at := 0
	for i, w := range words {
		toks := pieceTokens(w)
		if slices.ContainsFunc(terms, func(t evidenceTerm) bool { return toks[t.term] }) {
			at = i
			break
		}
	}
	from := max(at-8, 0)
	out := ""
	for to := from + 1; to <= len(words); to++ {
		s := strings.Join(words[from:to], " ")
		if from > 0 {
			s = "… " + s
		}
		if to < len(words) {
			s += " …"
		}
		if estimateTokens(s) > room {
			break
		}
		out = s
	}
	if out == "" && room > 0 {
		// One word longer than room: its first characters.
		r := []rune(words[from])
		n := len(r)
		for n > 1 && estimateTokens(string(r[:n])+"…") > room {
			n /= 2
		}
		out = string(r[:n]) + "…"
	}
	return out
}
