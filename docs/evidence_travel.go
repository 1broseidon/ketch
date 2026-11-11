package docs

import (
	"regexp"
	"slices"
	"strings"
)

// Travel (design/docs-evidence.md, Round 12): a cut keeps what makes the
// printed text readable. Pieces are ranked alone, but some are never
// printed without the piece before them; each names the pieces of its
// section it brings (piece.with), and a cut group that takes it takes
// them too. What it brings is context: it is charged to the budget but
// not to the page (answerPageTokens) a long section is cut to.
//
//   - A code block brings the piece right before it when that piece
//     introduces it, ending in a colon ("using the `terraform providers
//     lock` command:"). The lead-in does not bring the code: a long block
//     paid for by its lead-in costs more answers than it saves.
//   - A piece opening with a word that points back at the piece before it
//     ("Or", "Otherwise", "Alternatively", "In that case", "This",
//     "These", "The above") brings that piece.
//
// A definition head (headLine: a signature, a term with its type) and the
// paragraph under it are one piece (splitText), so a description never
// prints under another item's head.

// linkPieces sets, for each of a section's pieces (in order, ord their
// index) in text, the ords of the pieces it brings.
func linkPieces(ps []piece, text string) {
	for i := 1; i < len(ps); i++ {
		prev, cur := ps[i-1], ps[i]
		if !onlySpace(text[prev.end:cur.start]) {
			continue
		}
		lead := prev.kind != kindExample && prev.table < 0 && leadsIn(prev.text)
		if cur.kind == kindExample && lead || cur.kind != kindExample && cur.table < 0 && pointsBack(cur.text) {
			ps[i].with = append(ps[i].with, i-1)
		}
	}
}

// leadsIn reports text that introduces what follows it: it ends in a
// colon.
func leadsIn(t string) bool {
	return strings.HasSuffix(strings.TrimRight(t, " \t\n*_"), ":")
}

// connectiveRe matches text opening (after a list marker or emphasis) with
// a word that depends on what comes before it: an alternative to it, its
// case, or a pointer at it.
var connectiveRe = regexp.MustCompile(`(?i)^[\s>*_]*(?:[-*+]\s+|\d{1,3}[.)]\s+)?[*_]*(?:or|otherwise|alternatively|in (?:that|this) case|this|these|the above)[*_]*(?:[\s,:]|$)`)

// pointsBack reports text opening with a connective (connectiveRe).
func pointsBack(t string) bool {
	return connectiveRe.MatchString(t)
}

func onlySpace(s string) bool { return strings.TrimSpace(s) == "" }

// unitMaxPieces is the most pieces a piece is printed with, itself
// included.
const unitMaxPieces = 4

// unitOf is piece id with the pieces it brings, transitively, at most
// unitMaxPieces, in page order.
func unitOf(s *sorted, id int) []int {
	out := []int{id}
	for k := 0; k < len(out) && len(out) < unitMaxPieces; k++ {
		p := s.pieces[out[k]]
		for _, o := range p.with {
			if w := p.id - p.ord + o; !slices.Contains(out, w) && len(out) < unitMaxPieces {
				out = append(out, w)
			}
		}
	}
	slices.Sort(out)
	return out
}

// headMaxTokens is the longest line that heads a paragraph.
const headMaxTokens = 80

// headLine reports a paragraph that heads the one after it: one line of at
// most headMaxTokens with three letters or more, not code, a list, a
// table, a quote or a heading, that does not end like a sentence (. ! ? :
// ; ,): a signature (`PIL.ImageOps.fit(image, size, …) → Image`), a term
// and its type (`mode string (Optional, default: single)`), a name.
func headLine(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" || strings.Contains(t, "\n") || isListLine(t) || strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
		return false
	}
	if countLetters(t) < 3 || estimateTokens(t) > headMaxTokens {
		return false
	}
	return !strings.ContainsAny(t[len(t)-1:], ".!?:;,")
}

// headsNext reports span g of text a definition head (headLine) whose
// next span is a paragraph of prose: the two are one piece.
func headsNext(text string, spans []textSpan, g int) bool {
	if g+1 >= len(spans) || spans[g].code || spans[g].list || spans[g+1].code || spans[g+1].list || spans[g+1].label {
		return false
	}
	if !headLine(text[spans[g].start:spans[g].end]) {
		return false
	}
	k, _ := splitLineKind(lineOf(text[spans[g+1].start:spans[g+1].end]), false)
	return k == lineProse
}
