package docs

import (
	"math"
	"regexp"
)

// Rank (design/docs-evidence.md, Round 4): sections are printed, chosen by
// their pieces. Rank scores every piece, ranks the pieces, and walks them
// in one order (evidence_choose.go): the first piece of a section to come
// up opens it, whole when it fits the budget left, else cut to its pieces.
//
// Score. Each of the question's terms a piece holds counts by its rarity
// in the library (termWeight, from Gather's counts), an identifier the
// question writes twice. A term counts once for the piece's own text and
// once more for its heading path: a section about the question lifts
// every piece under it, since a how-to's body rarely repeats the
// question's words its heading holds. The page role scales the sum
// (roleFactor).
//
// Order. Pieces rank densely by score (tied scores share a rank, the next
// score ranks one lower). The walk takes them by rank fusion of that rank
// and their section's rank in Gather's whole-question ordering:
// 1/(rankFusionK + rank) + 1/(rankFusionK + section rank). A long
// reference section gets credit for its best row; a section Gather ranks
// high is not hidden by bunched piece scores.
//
// Print. In that order, a piece whose section is not printed yet opens
// it: the section prints whole when it fits the budget left, else cut to
// the piece; a later piece of a cut section joins it when it fits. A
// piece that does not fit is passed over and the walk goes on. A plan
// costs what its group renders to, frontmatter aside. The first section
// always prints something.
//
// Stop. The budget: the walk ends when the candidates do. The stop is
// "budget" when a piece was passed over for room, else "end".

const (
	// rankFusionK is rank fusion's k: how far down the two ranks a
	// section's place is still told apart.
	rankFusionK = 10
	// distinctiveShare: a term in under 1/distinctiveShare of the library's
	// sections is distinctive.
	distinctiveShare = 10
)

// distinctive reports a term that tells the question's answer apart: an
// identifier the question writes, or a term in under a tenth of the
// library's sections (design/docs-evidence.md, Output).
func distinctive(t evidenceTerm, sections int) bool {
	return t.ident || t.sections*distinctiveShare < sections
}

// roleFactor scales a piece's score by its page's role: reference and
// guide 1.0, FAQ 0.9, troubleshooting 0.8, release notes 0.6 unless the
// question asks about a version or a change.
func roleFactor(r pageRole, asksChange bool) float64 {
	switch r {
	case roleFAQ:
		return 0.9
	case roleTroubleshooting:
		return 0.8
	case roleReleaseNotes:
		if asksChange {
			return 1
		}
		return 0.6
	}
	return 1
}

// changeRe matches a question about a change: a release, what changed,
// a deprecation.
var changeRe = regexp.MustCompile(`(?i)\b(?:changelog|changes|changed|release[sd]?|deprecat\w*|breaking|what'?s new)\b`)

// asksChange reports a question about a version or a change.
func asksChange(query string) bool {
	return changeRe.MatchString(query) || migrationRe.MatchString(query) || versionMentionRe.MatchString(query)
}

// termWeight is a term's rarity in a library of n sections, df of which
// hold it: the inverse document frequency FTS5's bm25 uses, so a term
// every section holds (the product's name) counts next to nothing.
func termWeight(df, n int) float64 {
	if n <= 0 || df > n {
		return 0
	}
	return math.Log(1 + (float64(n-df)+0.5)/(float64(df)+0.5))
}

// termHit is one term a piece holds, and what it counts.
type termHit struct {
	term   int     // index into the terms
	inText bool    // in the piece's own text
	inPath bool    // in its heading path
	count  float64 // 1 for each of the two
}

// scored is a piece's score, its parts and its rank.
type scored struct {
	hits  []termHit
	role  float64
	sum   float64 // its terms' weights, each by what it counts
	score float64 // role × sum
	rank  int     // its rank by score among the candidates, 1 the best; 0 for none
}

// scorePieces scores every piece of s for the terms: the terms' weights,
// which terms are distinctive, and the scores.
func scorePieces(s *sorted, terms []evidenceTerm, sections int, change bool) ([]float64, []bool, []scored) {
	weights, distinct := evidenceWeights(terms, sections)
	paths := make([]map[string]bool, len(s.secs))
	for i, sec := range s.secs {
		paths[i] = headingSet(append(append([]string{}, sec.Crumbs...), sec.Heading)...)
	}
	out := make([]scored, len(s.pieces))
	for id, p := range s.pieces {
		sc := scored{role: roleFactor(s.secs[p.sec].role, change)}
		toks := pieceTokens(p.text)
		for ti, t := range terms {
			h := termHit{term: ti, inText: toks[t.term], inPath: paths[p.sec][t.term]}
			if h.inText {
				h.count++
			}
			if h.inPath {
				h.count++
			}
			if h.count == 0 {
				continue
			}
			sc.hits = append(sc.hits, h)
			sc.sum += weights[ti] * h.count // in term order: a float sum in map order differs run to run
		}
		sc.score = sc.role * sc.sum
		out[id] = sc
	}
	return weights, distinct, out
}

// evidenceWeights is each term's weight (termWeight, an identifier the
// question writes twice) and which terms are distinctive (every term when
// none is).
func evidenceWeights(terms []evidenceTerm, sections int) ([]float64, []bool) {
	weights := make([]float64, len(terms))
	distinct := make([]bool, len(terms))
	some := false
	for i, t := range terms {
		weights[i] = termWeight(t.sections, sections)
		if t.ident {
			weights[i] *= 2
		}
		distinct[i] = distinctive(t, sections)
		some = some || distinct[i]
	}
	for i := range terms {
		distinct[i] = distinct[i] || !some
	}
	return weights, distinct
}

// headingSet is the terms headings name: their words, camel-case words,
// identifiers' whole names and host names.
func headingSet(headings ...string) map[string]bool {
	set := map[string]bool{}
	for _, h := range headings {
		for _, t := range headingTokens(h) {
			set[t] = true
		}
		for _, t := range identParts(h) {
			set[t] = true
		}
		for _, t := range hostTerms(h) {
			set[t] = true
		}
	}
	return set
}

// pieceTokens is the terms a piece's text names: its words (camel-case
// words included), their letter-digit parts (load_in_8bit names 8 and
// bit), the whole names of the identifiers it writes (pam_duo: pamduo),
// the host names it writes (hostTerms) and the digits of the grouped
// numbers it writes (10,000: 10000).
func pieceTokens(text string) map[string]bool {
	toks := map[string]bool{}
	for _, t := range textTokens(text) {
		toks[t] = true
		for _, part := range digitSplit(t) {
			toks[normTerm(stem(part))] = true
		}
	}
	for _, n := range groupedNumbers(text) {
		toks[n] = true
	}
	for _, t := range identParts(text) {
		toks[t] = true
	}
	for _, t := range hostTerms(text) {
		toks[t] = true
	}
	return toks
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

// hintTokens is what a pointer line (SectionHint) costs, with its line
// break: "" for none. A pointer is a long URL and a command line, which
// the estimate undercounts by up to a quarter (o200k splits slugs and
// flags finely), so it is charged a quarter more.
func hintTokens(hint string) int {
	if hint == "" {
		return 0
	}
	return estimateTokens(hint)*5/4 + 1
}
