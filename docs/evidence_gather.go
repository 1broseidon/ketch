package docs

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Gather (design/docs-evidence.md): collect widely and cheaply, for Rank
// to be precise. The question's terms are one list for every stage,
// question filler dropped. The pool is the union of the best sections for
// the whole question (full-text candidates ordered by rerankHits' boosts)
// and, for each term on its own, the best sections that contain it (in
// their own heading, identifiers or text, not only a breadcrumb), ordered
// by the whole question's full-text score. There is no relevance cut, no
// family and no per-page cap: every pool section is split into pieces.
// Each term's count of the library's sections, and the library's section
// count, weigh the terms in Rank.

const (
	// gatherQuestion is how many of the whole question's best sections
	// the pool holds.
	gatherQuestion = 40
	// gatherTerm is how many of each term's best sections it holds.
	gatherTerm = 20
	// gatherPool is the most sections the pool holds.
	gatherPool = 200
	// gatherTermSlack is how many more full-text hits a term's search
	// asks for, for the sections the pool leaves out (a later version's,
	// a docs platform's boilerplate) to leave gatherTerm.
	gatherTermSlack = 10
)

// evidenceTerm is one term of the question.
type evidenceTerm struct {
	term     string
	word     bool // a term of the question's words (queryTerms)
	ident    bool // an identifier the question writes (queryIdents): it weighs double
	sections int  // the library's sections whose full-text row holds it
}

// poolSection is a section of the pool, cleaned, with how it got there.
type poolSection struct {
	docsindex.Section
	via   string  // "question", or the term whose own list brought it
	rank  int     // its place in that list, 1 the first
	score float64 // its whole-question score relative to the best section's (0 to 1)
	// order is its place in the whole question's ordering (rerankHits over
	// every section the question matches), 1 the first; 0 for a section
	// that ordering leaves out.
	order int
}

// gathered is Gather's output.
type gathered struct {
	query    rankQuery
	terms    []evidenceTerm
	written  []writtenTerm // the terms as the question writes them, with their counts
	sections int           // the library's sections
	ftsHits  int           // the whole question's full-text candidates
	ordered  int           // the sections the whole question's ordering holds
	pool     []poolSection
}

// evidenceTerms is the question's one term list: its words (queryTerms,
// question filler out), then the identifiers it writes (queryIdents) that
// are not among them.
func evidenceTerms(q rankQuery) []evidenceTerm {
	var out []evidenceTerm
	at := map[string]int{}
	for _, t := range q.terms {
		if _, ok := at[t]; !ok {
			at[t] = len(out)
			out = append(out, evidenceTerm{term: t, word: true})
		}
	}
	for _, id := range q.idents {
		i, ok := at[id]
		if !ok {
			i = len(out)
			at[id] = i
			out = append(out, evidenceTerm{term: id})
		}
		out[i].ident = true
	}
	return out
}

// gather collects the pool of lib's sections for query, with the terms'
// counts.
func (l *Local) gather(lib docsindex.Library, query string) (*gathered, error) {
	q := newRankQuery(query)
	g := &gathered{query: q, terms: evidenceTerms(q)}
	g.written = writtenTerms(query, g.terms)
	if len(q.terms) == 0 || l.store == nil {
		return g, nil
	}
	if _, err := l.store.EnsureFTS(lib.Key); err != nil {
		return nil, fmt.Errorf("local: rebuilding the full-text index: %w", err)
	}
	whole := ftsMatch(q.terms, q.identKeys)
	// The whole question's every hit: its best are its candidates, and the
	// rest score the sections the terms' lists bring past them (bm25 is
	// computed for every match to order them all the same).
	queries := []docsindex.FTSQuery{{Match: whole, Limit: -1}}
	counts := []string{""}
	for _, t := range g.terms {
		queries = append(queries, docsindex.FTSQuery{Match: termMatch(t.term, whole), Limit: gatherTerm + gatherTermSlack})
		counts = append(counts, ftsTerm(t.term))
	}
	// A written term counted as one of the terms is not counted again.
	countOf := map[string]int{}
	for i, m := range counts {
		countOf[m] = i
	}
	writtenAt := make([]int, len(g.written))
	for i, wt := range g.written {
		k, ok := countOf[wt.match]
		if !ok {
			k = len(counts)
			countOf[wt.match] = k
			counts = append(counts, wt.match)
		}
		writtenAt[i] = k
	}
	hits, err := l.store.SearchEach(lib.Key, queries, ftsWeights)
	if err != nil {
		return nil, err
	}
	n, err := l.store.Counts(lib.Key, counts)
	if err != nil {
		return nil, err
	}
	g.sections = n[0]
	for i := range g.terms {
		g.terms[i].sections = n[i+1]
	}
	for i := range g.written {
		g.written[i].sections = n[writtenAt[i]]
	}
	markDistinctive(g.written, g.sections)
	all := hits[0]
	hits[0] = all[:min(len(all), ftsCandidates)]
	g.ftsHits = len(hits[0])
	hits[0] = append(hits[0], scoredBeyond(hits, all)...)
	secs, err := l.sectionsOf(hits)
	if err != nil {
		return nil, err
	}
	hits = oneAddress(lib, secs, hits)
	g.pool, g.ordered = poolOf(lib, q, g.terms, secs, hits)
	for i := range g.pool {
		g.pool[i].Section = cleanSection(g.pool[i].Section)
	}
	return g, nil
}

// oneAddress makes a page the library stores at two addresses (x and
// x.html, a trailing slash, x/index, an llms/ mirror below the root, www.:
// pageIdentity, the planner's notion of one page) one page in the hits,
// before they become the pool: each section of it is cited at the address
// the library lists first (its lowest section ID among secs), and
// sections of one page with the same anchor and heading path
// (sectionPlace) are one section, that address's, in each list at the
// first place either holds. A section moved to that address has its page
// changed in secs.
func oneAddress(lib docsindex.Library, secs map[int64]docsindex.Section, hits [][]docsindex.Hit) [][]docsindex.Hit {
	root, rerr := parseWebRoot(lib.Root)
	ids := slices.Sorted(maps.Keys(secs))
	first := map[string]string{} // page identity: the address listed first
	key := make(map[int64]string, len(ids))
	for _, id := range ids {
		s := secs[id]
		u, err := url.Parse(s.Page)
		if err != nil {
			continue
		}
		page := pageIdentity(u, root, rerr == nil)
		if _, ok := first[page]; !ok {
			first[page] = s.Page
		}
		key[id] = page + "\x00" + sectionPlace(s)
	}
	rep := map[string]int64{} // section key: the ID that stands for it
	same := make(map[int64]int64, len(ids))
	moved := false
	for _, id := range ids {
		k, ok := key[id]
		if !ok {
			same[id] = id
			continue
		}
		r, seen := rep[k]
		if !seen {
			r, rep[k] = id, id
			s := secs[id]
			page, _, _ := strings.Cut(k, "\x00")
			if addr := first[page]; addr != s.Page {
				s.Page, moved = addr, true
				secs[id] = s
			}
		}
		if r != id {
			moved = true
		}
		same[id] = r
	}
	if !moved {
		return hits
	}
	out := make([][]docsindex.Hit, len(hits))
	for i, list := range hits {
		seen := map[int64]bool{}
		for _, h := range list {
			r, ok := same[h.ID]
			if !ok {
				r = h.ID
			}
			if !seen[r] {
				seen[r] = true
				out[i] = append(out[i], docsindex.Hit{ID: r, Score: h.Score})
			}
		}
	}
	return out
}

// sectionPlace is where a section sits on its page, as two copies of the
// page agree on it: its anchor and its heading path (a page may give
// several sections one anchor: nginx's #content).
func sectionPlace(s docsindex.Section) string {
	return s.Anchor + "\x00" + s.Heading + "\x00" + strings.Join(s.Crumbs, "\x00")
}

// termMatch is the full-text query for a term's own list: the sections
// whose heading, identifiers or text hold it, scored by the whole
// question (whole).
func termMatch(term, whole string) string {
	return "{heading ident body} : " + ftsTerm(term) + " AND (" + whole + ")"
}

// scoredBeyond is the whole question's hits (all, best first) for the
// sections the terms' lists (hits[1:]) hold past its candidates (hits[0]),
// in the lists' order, each once: so every pool section has its
// whole-question score.
func scoredBeyond(hits [][]docsindex.Hit, all []docsindex.Hit) []docsindex.Hit {
	seen := map[int64]bool{}
	for _, h := range hits[0] {
		seen[h.ID] = true
	}
	score := make(map[int64]float64, len(all))
	for _, h := range all[len(hits[0]):] {
		score[h.ID] = h.Score
	}
	var out []docsindex.Hit
	for _, list := range hits[1:] {
		for _, h := range list {
			if s, ok := score[h.ID]; ok && !seen[h.ID] {
				seen[h.ID] = true
				out = append(out, docsindex.Hit{ID: h.ID, Score: s})
			}
		}
	}
	return out
}

// sectionsOf reads the sections every list of hits names, by ID.
func (l *Local) sectionsOf(hits [][]docsindex.Hit) (map[int64]docsindex.Section, error) {
	var ids []int64
	seen := map[int64]bool{}
	for _, list := range hits {
		for _, h := range list {
			if !seen[h.ID] {
				seen[h.ID] = true
				ids = append(ids, h.ID)
			}
		}
	}
	got, err := l.store.SectionsByID(ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]docsindex.Section, len(got))
	for _, s := range got {
		out[s.ID] = s
	}
	return out, nil
}

// poolOf is the pool: the whole question's best gatherQuestion sections as
// rerankHits orders its candidates (hits[0]), then each term's best
// gatherTerm (hits[1:], in term order) taking turns, each section once, at
// most gatherPool. Each holds its whole-question score from rerankHits,
// relative to the best, and its place in that ordering: 0 for both when
// rerankHits leaves it out. It also returns how many sections that
// ordering holds.
func poolOf(lib docsindex.Library, q rankQuery, terms []evidenceTerm, secs map[int64]docsindex.Section, hits [][]docsindex.Hit) ([]poolSection, int) {
	cands := make([]docsindex.Section, 0, len(hits[0]))
	for _, h := range hits[0] {
		if s, ok := secs[h.ID]; ok {
			cands = append(cands, s)
		}
	}
	var out []poolSection
	in := map[int64]bool{}
	score := map[int64]float64{}
	order := map[int64]int{}
	ordered := rerankHits(lib, cands, hits[0], q)
	for i, r := range ordered {
		s := cands[r.sec]
		score[s.ID], order[s.ID] = r.score, i+1
		if i < gatherQuestion {
			in[s.ID] = true
			out = append(out, poolSection{Section: s, via: "question", rank: i + 1, score: r.score, order: i + 1})
		}
	}
	lists := termLists(q, terms, secs, hits[1:])
	for k := 0; k < gatherTerm && len(out) < gatherPool; k++ {
		for _, list := range lists {
			if k < len(list) && !in[list[k].ID] && len(out) < gatherPool {
				in[list[k].ID] = true
				p := list[k]
				p.score, p.order = score[p.ID], order[p.ID]
				out = append(out, p)
			}
		}
	}
	return out, len(ordered)
}

// termLists are each term's best gatherTerm sections, from its hits.
func termLists(q rankQuery, terms []evidenceTerm, secs map[int64]docsindex.Section, hits [][]docsindex.Hit) [][]poolSection {
	lists := make([][]poolSection, len(hits))
	for t, list := range hits {
		for _, h := range list {
			s, ok := secs[h.ID]
			if !ok || !poolKeeps(s, q) {
				continue
			}
			lists[t] = append(lists[t], poolSection{Section: s, via: terms[t].term, rank: len(lists[t]) + 1})
			if len(lists[t]) == gatherTerm {
				break
			}
		}
	}
	return lists
}

// poolKeeps reports a section the pool may hold: not about a later
// version than indexed unless the question asks about it, and not under a
// docs platform's boilerplate heading (as rerankHits leaves them out).
func poolKeeps(s docsindex.Section, q rankQuery) bool {
	return (s.CrossVersion == "" || asksAbout(q.query, s.CrossVersion)) && !boilerplateSection(s.Crumbs)
}

// cleanSection is a section with cleanText applied to its text, heading
// and heading path.
func cleanSection(s docsindex.Section) docsindex.Section {
	s.Text = cleanText(s.Text)
	s.Heading = cleanLine(s.Heading)
	if len(s.Crumbs) > 0 {
		crumbs := make([]string, len(s.Crumbs))
		for i, c := range s.Crumbs {
			crumbs[i] = cleanLine(c)
		}
		s.Crumbs = crumbs
	}
	return s
}
