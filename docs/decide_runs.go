package docs

import (
	"slices"
	"strings"
)

// Long blocks (the prototype's page_units). A piece is a block: one code
// block can be a whole long reference (a man page or a help file served as
// one <pre>, tens of thousands of tokens, each option a few lines of it).
// Asked about as one passage, the provider would see only its first
// passageSendChars and judge the page by its introduction. So a piece of
// more than bigBlockTokens with more than one line is cut by lines into
// runs of at most passageTokens, each a passage of its own: a code block's
// fence lines are left out of the runs and put back around each run
// printed.

// bigBlockTokens: a piece over this many tokens is asked about by runs of
// its lines.
const bigBlockTokens = 600

// lineRun is lines [from, to) of a piece's text.
type lineRun struct{ from, to int }

// runLines are a piece's lines, and the range of them runs are cut from
// (inside its fences for a fenced code block).
func runLines(text string) (lines []string, lo, hi int, fenced bool) {
	lines = strings.Split(text, "\n")
	hi = len(lines)
	if isFenceLine(lines[0]) {
		fenced, lo = true, 1
		if len(lines) > 1 && isFenceLine(lines[len(lines)-1]) {
			hi = len(lines) - 1
		}
	}
	return lines, lo, hi, fenced
}

func isFenceLine(line string) bool {
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// pieceRuns cuts a piece's text by lines into runs of at most
// passageTokens (a longer line is a run of its own), each ending at a
// blank line when one falls in its second half, so an entry of a help file
// or man page stays in one run; nil for a piece that is asked about whole.
func pieceRuns(p piece) []lineRun {
	if p.tokens <= bigBlockTokens || !strings.Contains(p.text, "\n") {
		return nil
	}
	lines, lo, hi, _ := runLines(p.text)
	sum := make([]int, hi+1) // sum[i]: tokens of lines lo..i-1
	for i := lo; i < hi; i++ {
		sum[i+1] = sum[i] + estimateTokens(lines[i]) + 1
	}
	var out []lineRun
	for start := lo; start < hi; {
		end := start + 1
		for end < hi && sum[end+1]-sum[start] <= passageTokens {
			end++
		}
		if end < hi {
			end = blankCut(lines, start, end, sum)
		}
		out = append(out, lineRun{start, end})
		start = end
	}
	return out
}

// blankCut is where a run from start that would end at end ends: after
// the last blank line in its second half, else at end.
func blankCut(lines []string, start, end int, sum []int) int {
	for b := end - 1; b > start; b-- {
		if 2*(sum[b+1]-sum[start]) < passageTokens {
			break
		}
		if strings.TrimSpace(lines[b]) == "" {
			return b + 1
		}
	}
	return end
}

// runsText prints runs of a piece's text: each run its lines, inside the
// block's fences again for a fenced block, "…" between runs that do not
// touch.
func runsText(text string, runs []lineRun) string {
	lines, _, _, fenced := runLines(text)
	var parts []string
	for _, r := range mergeRuns(runs) {
		body := strings.Join(lines[r.from:r.to], "\n")
		if fenced {
			body = lines[0] + "\n" + body + "\n" + strings.TrimLeft(lines[0], " \t")[:3]
		}
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n…\n\n")
}

// mergeRuns are runs in order, those that touch or overlap made one.
func mergeRuns(runs []lineRun) []lineRun {
	s := slices.Clone(runs)
	slices.SortFunc(s, func(a, b lineRun) int { return a.from - b.from })
	var out []lineRun
	for _, r := range s {
		if n := len(out); n > 0 && r.from <= out[n-1].to {
			out[n-1].to = max(out[n-1].to, r.to)
			continue
		}
		out = append(out, r)
	}
	return out
}

// secChoice is what a decided answer prints of one section: whole pieces,
// and runs of the lines of long ones.
type secChoice struct {
	pieces []int
	runs   map[int][]lineRun
}

// with is c with passage ps added (c is not changed).
func (c *secChoice) with(ps dPassage) *secChoice {
	out := &secChoice{runs: map[int][]lineRun{}}
	if c != nil {
		out.pieces = slices.Clone(c.pieces)
		for id, rs := range c.runs {
			out.runs[id] = slices.Clone(rs)
		}
	}
	if ps.run == nil {
		out.pieces = union(out.pieces, ps.pieces)
		return out
	}
	id := ps.pieces[0]
	out.runs[id] = append(out.runs[id], *ps.run)
	return out
}

// holds reports ps printed by c.
func (c *secChoice) holds(ps dPassage) bool {
	if c == nil {
		return false
	}
	if ps.run == nil {
		for _, id := range ps.pieces {
			if !slices.Contains(c.pieces, id) {
				return false
			}
		}
		return true
	}
	if slices.Contains(c.pieces, ps.pieces[0]) {
		return true
	}
	for _, r := range mergeRuns(c.runs[ps.pieces[0]]) {
		if r.from <= ps.run.from && ps.run.to <= r.to {
			return true
		}
	}
	return false
}

// plan is c as the planner's plan for section sec: its pieces; with runs,
// the text printed in their place (clip), its pieces and the runs' blocks
// in page order, "…" where text was left out.
func (c *secChoice) plan(s *sorted, sec int) plan {
	ids := slices.Clone(c.pieces)
	for id := range c.runs {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	pl := plan{sec: sec, pieces: ids}
	if len(c.runs) == 0 {
		return pl
	}
	var parts []string
	var whole []int
	flush := func() {
		if len(whole) > 0 {
			parts = append(parts, joinPieces(s, s.secs[sec], whole))
			whole = nil
		}
	}
	for _, id := range ids {
		rs, isRun := c.runs[id]
		if !isRun || slices.Contains(c.pieces, id) {
			if n := len(whole); n > 0 && s.pieces[id].ord != s.pieces[whole[n-1]].ord+1 {
				flush()
			}
			whole = append(whole, id)
			continue
		}
		flush()
		parts = append(parts, runsText(s.pieces[id].text, rs))
	}
	flush()
	pl.clip = strings.Join(parts, "\n\n…\n\n")
	return pl
}
