package docs

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Keyword ranking over the index's full-text table (ADR-0007, amendment
// 5): the default, with no model. Each section's full-text row is its
// heading, its breadcrumb above it, the parts of the identifiers it names
// and its body, every word normalized as the web backend's ranking
// normalizes a query's (tokenize, normTerm, camel-case words), so the
// table and the question meet on the same terms. FTS5's bm25, with the
// heading weighted over the body, picks the candidates; the boosts the
// web backend's ranker applies on top of BM25 (sectionBoost: the share of
// the question's terms named, an exact title, an API entry the question
// names, its terms in code and side by side) order them; families and
// rendering are the hybrid ranker's.

// ftsVersion names the normalization of the full-text rows: rows made by
// another version are rebuilt from the stored sections on first use.
const ftsVersion = "k1"

// ftsTokenizer makes the full-text rows.
type ftsTokenizer struct{}

func (ftsTokenizer) Version() string { return ftsVersion }

func (ftsTokenizer) Row(s docsindex.Section) [4]string {
	var crumbs []string
	for _, c := range s.Crumbs[:max(len(s.Crumbs)-1, 0)] {
		crumbs = append(crumbs, rankTokens(c)...)
	}
	return [4]string{
		strings.Join(headingTokens(s.Heading), " "),
		strings.Join(crumbs, " "),
		strings.Join(identParts(s.Heading+"\n"+s.Text), " "),
		strings.Join(rankTokens(s.Text), " "),
	}
}

// OpenIndex opens the local docs index at path with the docs backends'
// full-text normalization: what `docs index` and the local backends use.
func OpenIndex(path string) *docsindex.Store {
	return docsindex.Open(path, ftsTokenizer{})
}

// identRe matches an identifier as code writes it, qualified or not:
// useForm, client_max_body_size, form.validateField, std::fs::read,
// add_generation_prompt.
var identRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*(?:(?:\.|::|->|#)[A-Za-z_$][A-Za-z0-9_$]*)*`)

// identParts are the identifier terms a text names: for every
// identifier written as code (camel case, snake case, or qualified), each
// part's whole name as one term (validateField: validatefield,
// client_max_body_size: clientmaxbodysize) and its camel-case words
// (validate, field). A query naming the identifier, its whole name or its
// words then matches it; snake case's words are body terms already.
func identParts(text string) []string {
	var out []string
	for _, m := range identRe.FindAllString(text, -1) {
		if !isCodeIdent(m) {
			continue
		}
		for _, part := range identSplit(m) {
			if k := identKey(part); len(k) > 1 {
				out = append(out, k)
			}
			if words := camelSplit(part); len(words) > 1 {
				for _, w := range words {
					if t := normTerm(stem(strings.Trim(w, "_$"))); t != "" {
						out = append(out, t)
					}
				}
			}
		}
	}
	return out
}

// isCodeIdent reports a word written as code: an inner capital after a
// lower-case letter, an underscore between letters, or a qualifier.
func isCodeIdent(w string) bool {
	return isCamel(w) || strings.Contains(strings.Trim(w, "_"), "_") || strings.ContainsAny(w, ".#") || strings.Contains(w, "::") || strings.Contains(w, "->")
}

// identSplit splits a qualified identifier into its parts.
func identSplit(w string) []string {
	return strings.FieldsFunc(w, func(r rune) bool { return r == '.' || r == ':' || r == '-' || r == '>' || r == '#' })
}

// queryIdentKeys are the whole-name terms of the identifiers a query
// writes (identParts' form), which the index holds beside their words.
func queryIdentKeys(query string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(query) {
		w = strings.Trim(w, queryQuotes+",;:!?=")
		if !isCodeIdent(w) {
			continue
		}
		for _, m := range identRe.FindAllString(w, -1) {
			for _, part := range identSplit(m) {
				if k := identKey(part); len(k) > 1 && !seen[k] {
					seen[k] = true
					out = append(out, k)
				}
			}
		}
	}
	return out
}

// ftsQuote quotes a term for an FTS5 query.
func ftsQuote(t string) string { return `"` + strings.ReplaceAll(t, `"`, `""`) + `"` }

// ftsMatch is the full-text query for a question's terms: any of them
// (bm25 rewards the sections naming more, and rarer, ones), and the whole
// names of the identifiers it writes.
func ftsMatch(terms, idents []string) string {
	seen := map[string]bool{}
	var parts []string
	for _, t := range append(append([]string{}, terms...), idents...) {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		parts = append(parts, ftsQuote(t))
	}
	return strings.Join(parts, " OR ")
}

// ftsWeights are bm25's column weights (FTSColumns: heading, crumbs,
// ident, body): a term in a section's heading counts three times one in
// its body, as the web backend's BM25F counts it.
var ftsWeights = []float64{3, 1, 1, 1}

// ftsCandidates is how many full-text hits the boosts reorder.
const ftsCandidates = 200

// keywordRank ranks a library's sections for the query by the full-text
// table: the candidates' sections (every section of the pages they are
// on, so families are whole), and their ranking as indices into them,
// best first, scores relative to the best; with cut, the relevant ones
// only (within webRelevanceRatio of the best). A section about a later version than indexed is left out
// unless the question asks about it.
func (l *Local) keywordRank(lib docsindex.Library, query string, cut bool) ([]docsindex.Section, []rankedSection, error) {
	hits, err := l.ftsHits(lib, query)
	if err != nil || len(hits) == 0 {
		return nil, nil, err
	}
	var pages []string
	cands, err := l.store.SectionsByID(hitIDs(hits))
	if err != nil {
		return nil, nil, err
	}
	pageSet := map[string]bool{}
	for _, c := range cands {
		if !pageSet[c.Page] {
			pageSet[c.Page] = true
			pages = append(pages, c.Page)
		}
	}
	secs, err := l.store.SectionsOfPages(lib.Key, pages)
	if err != nil {
		return nil, nil, err
	}
	ranked := rerankHits(secs, hits, query)
	if cut {
		ranked = relevantRanked(ranked)
	}
	return secs, ranked, nil
}

// ftsHits are the full-text table's best sections for the query; the
// table is rebuilt first when another normalization made it.
func (l *Local) ftsHits(lib docsindex.Library, query string) ([]docsindex.Hit, error) {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	if _, err := l.store.EnsureFTS(lib.Key); err != nil {
		return nil, fmt.Errorf("local: rebuilding the full-text index: %w", err)
	}
	return l.store.Search(lib.Key, ftsMatch(terms, queryIdentKeys(query)), ftsWeights, ftsCandidates)
}

func hitIDs(hits []docsindex.Hit) []int64 {
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	return ids
}

// rerankHits orders the full-text hits (sections of secs by ID) by their
// bm25 score times sectionBoost, as the web backend orders BM25 scores,
// dropping a section that matched by its breadcrumb alone and one about a
// later version unless the question asks about it.
func rerankHits(secs []docsindex.Section, hits []docsindex.Hit, query string) []rankedSection {
	terms := queryTerms(query)
	idents, howTo := queryIdents(query), isHowTo(query)
	at := make(map[int64]int, len(secs))
	for i, s := range secs {
		at[s.ID] = i
	}
	var out []rankedSection
	for _, h := range hits {
		i, ok := at[h.ID]
		if !ok {
			continue
		}
		s := secs[i]
		if s.CrossVersion != "" && !asksAbout(query, s.CrossVersion) {
			continue
		}
		matched, own, seq := termMatches(s, terms)
		if !own {
			continue
		}
		score := h.Score * sectionBoost(webSectionOf(s, i), terms, idents, howTo, matched, seq)
		out = append(out, rankedSection{sec: i, score: score})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].score > out[b].score })
	if len(out) > 0 && out[0].score > 0 {
		best := out[0].score
		for i := range out {
			out[i].score /= best
		}
	}
	return out
}

// relevantRanked keeps the ranked sections within webRelevanceRatio of
// the best (scores relative to it), as relevant does.
func relevantRanked(ranked []rankedSection) []rankedSection {
	for i, r := range ranked {
		if r.score < webRelevanceRatio {
			return ranked[:i]
		}
	}
	return ranked
}

// termMatches counts the query terms a section names (heading, body with
// its identifiers' words, or breadcrumb), whether it names any itself
// (not by its breadcrumb alone), and its heading-then-body token sequence.
func termMatches(s docsindex.Section, terms []string) (matched int, own bool, seq []string) {
	head := headingTokens(s.Heading)
	body := rankTokens(s.Text)
	set := map[string]bool{}
	for _, t := range head {
		set[t] = true
	}
	for _, t := range body {
		set[t] = true
	}
	for _, t := range identParts(s.Text) {
		set[t] = true
	}
	crumbs := map[string]bool{}
	for _, c := range s.Crumbs[:max(len(s.Crumbs)-1, 0)] {
		for _, t := range rankTokens(c) {
			crumbs[t] = true
		}
	}
	for _, t := range terms {
		switch {
		case set[t]:
			matched++
			own = true
		case crumbs[t]:
			matched++
		}
	}
	seq = append(rankTokens(s.Heading), body...)
	return matched, own, seq
}
