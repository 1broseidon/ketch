package docs

import (
	"cmp"
	"slices"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// plan is what a section prints: whole, or cut to pieces (in page
// order), and what that costs as rendered.
type plan struct {
	sec    int
	whole  bool
	pieces []int
	cost   int
	// clip is the text printed in place of the pieces' when one of them
	// is larger than what was left (clipped); "" for none.
	clip string
}

// ranked is Rank's output: the terms' weights and which are distinctive,
// every piece's score, rank and fused value, the candidates in fused
// order, and the plans in the order their sections opened, with why
// sections were passed over.
type ranked struct {
	terms    []evidenceTerm
	weights  []float64
	distinct []bool
	scores   []scored  // by piece id
	fused    []float64 // by piece id, 0 for none
	walk     []int     // the candidates in fused order
	plans    []plan
	dropped  []plan
	passed   map[int]string // by section: why the walk passed it over ("budget", "limit")
	budget   int            // tokens the answer was planned within
	limit    int            // the most groups, 0 for no limit
	used     int
	stop     string // "budget" when a piece was passed over for room, else "end"
	// order is the exact pages' places, best first (pageOrder): the walk
	// takes every piece of one page before the next's; nil for none.
	order map[string]int
}

// rankPieces ranks the candidates (pieces not folded into another, that
// score) by score, densely (tied scores share a rank, the next score
// ranks one lower), and puts them in fused
// order: by 1/(rankFusionK + the piece's rank) + 1/(rankFusionK + its
// section's rank in Gather), then by that section rank, then by score,
// then in pool and page order.
func rankPieces(s *sorted, r *ranked) {
	var cands []int
	for id, p := range s.pieces {
		if p.into < 0 && r.scores[id].score > 0 {
			cands = append(cands, id)
		}
	}
	slices.SortStableFunc(cands, func(a, b int) int { return cmp.Compare(r.scores[b].score, r.scores[a].score) })
	r.fused = make([]float64, len(s.pieces))
	for i, id := range cands {
		sc := &r.scores[id]
		sc.rank = 1
		if i > 0 {
			sc.rank = r.scores[cands[i-1]].rank
			if sc.score != r.scores[cands[i-1]].score {
				sc.rank++
			}
		}
		r.fused[id] = 1/float64(rankFusionK+sc.rank) + 1/float64(rankFusionK+sectionRank(s, id))
	}
	r.walk = slices.Clone(cands)
	slices.SortStableFunc(r.walk, func(a, b int) int {
		if c := cmp.Compare(r.fused[b], r.fused[a]); c != 0 {
			return c
		}
		if c := cmp.Compare(sectionRank(s, a), sectionRank(s, b)); c != 0 {
			return c
		}
		return cmp.Compare(r.scores[b].score, r.scores[a].score)
	})
}

// sectionRank is piece id's section's rank in Gather's whole-question
// ordering, 1 the first.
func sectionRank(s *sorted, id int) int {
	return max(s.secs[s.pieces[id].sec].order, 1)
}

// planner is the state of the walk.
type planner struct {
	lib   docsindex.Library
	s     *sorted
	r     *ranked
	left  int
	shown []bool          // by piece: printed, or a copy of a printed piece
	pages map[string]bool // the pages a group is printed of
}

// printSections walks the candidates in fused order within the budget. A
// piece whose section is not printed yet opens it: the section prints
// whole when it fits the budget left, else cut to the piece, and a later
// piece of a cut section joins it when it fits (a long section prints its
// best pieces by rank). A piece that does not fit, and one that would
// open a group past --limit's, is passed over and the walk goes on: a
// later, smaller one may fit. The first section always prints something,
// so an answer is never empty for want of room.
//
// With the exact pages' order (r.order), everything read of them prints
// whole when it fits the budget; otherwise the pages are walked one at a
// time, best first, each its candidates in fused order: the best page gets
// the budget, the next what is left. The candidates of other pages (those
// a widening read) are walked after them, in fused order.
func printSections(lib docsindex.Library, s *sorted, r *ranked) {
	p := &planner{lib: lib, s: s, r: r, left: r.budget, shown: make([]bool, len(s.pieces)), pages: map[string]bool{}}
	r.passed, r.stop = map[int]string{}, "end"
	at := map[int]int{} // section: its plan
	if r.order == nil {
		p.walk(r.walk, at)
		return
	}
	secs := exactPageSections(s, r.order)
	walks := make([][]int, len(secs)+1) // the exact pages', then the rest's
	for _, id := range r.walk {
		k, ok := r.order[pageKeyOf(s.secs[s.pieces[id].sec].Page)]
		if !ok || k >= len(secs) {
			k = len(secs)
		}
		walks[k] = append(walks[k], id)
	}
	if p.wholeFits(secs, at) {
		walks = walks[len(secs):]
	}
	for _, ids := range walks {
		p.walk(ids, at)
	}
}

// exactPageSections are the sections of each exact page (by its place in
// order), in page order.
func exactPageSections(s *sorted, order map[string]int) [][]int {
	out := make([][]int, len(order))
	for i, sec := range s.secs {
		if k, ok := order[pageKeyOf(sec.Page)]; ok && k < len(out) {
			out[k] = append(out[k], i)
		}
	}
	for _, list := range out {
		slices.SortStableFunc(list, func(a, b int) int { return cmp.Compare(s.secs[a].Order, s.secs[b].Order) })
	}
	return out
}

// wholeFits prints every section of pages whole when they all fit the
// budget left, and reports whether it did.
func (p *planner) wholeFits(pages [][]int, at map[int]int) bool {
	total := 0
	for _, secs := range pages {
		for _, i := range secs {
			total += p.s.secs[i].tokens
		}
	}
	if total == 0 || total > p.left {
		return false
	}
	var plans []plan
	cost := 0
	seen := map[string]bool{}
	for _, secs := range pages {
		for _, i := range secs {
			pl := plan{sec: i, whole: true}
			pl.cost = p.render(pl) + 1
			if page := p.s.secs[i].Page; !seen[page] {
				seen[page] = true
				pl.cost += pageHeadingTokens(p.s.secs[i].Section)
			}
			plans, cost = append(plans, pl), cost+pl.cost
		}
	}
	if cost > p.left {
		return false
	}
	for _, pl := range plans {
		at[pl.sec] = len(p.r.plans)
		p.take(pl)
	}
	return true
}

// walk is the walk over ids (candidates, in the order taken), opening and
// growing plans (at: section to its plan).
func (p *planner) walk(ids []int, at map[int]int) {
	s, r := p.s, p.r
	for _, id := range ids {
		i := s.pieces[id].sec
		if p.shown[id] {
			continue
		}
		k, open := at[i]
		switch {
		case open:
			if !p.grow(&r.plans[k], id) {
				r.stop = "budget"
			}
		case r.limit > 0 && len(r.plans) >= r.limit:
			r.passed[i] = "limit"
		default:
			pl, ok := p.open(i, id)
			if !ok {
				r.passed[i], r.stop = "budget", "budget"
				continue
			}
			delete(r.passed, i)
			at[i] = len(r.plans)
			p.take(pl)
		}
	}
}

// open is what section i prints when piece id opens it, within the
// budget left: whole when it fits, else cut to the piece; false when that
// does not fit either. The first section always opens: when its piece does
// not fit, cut inside the piece (clipped).
func (p *planner) open(i, id int) (plan, bool) {
	if t := p.s.secs[i].tokens; t <= p.left {
		pl := plan{sec: i, whole: true}
		if pl.cost = p.render(pl) + p.overhead(pl); pl.cost <= p.left {
			return pl, true
		}
	}
	pl := plan{sec: i, pieces: unitOf(p.s, id)}
	pl.cost = p.render(pl) + p.overhead(pl)
	if pl.cost > p.left && len(p.r.plans) == 0 {
		return p.clipped(pl), true // the first section prints something, within the budget
	}
	return pl, pl.cost <= p.left
}

// overhead is what opening a section as pl adds to the answer beyond its
// group: its page's title and address when no group of the page is
// printed yet.
func (p *planner) overhead(pl plan) int {
	if sec := p.s.secs[pl.sec]; !p.pages[sec.Page] {
		return pageHeadingTokens(sec.Section)
	}
	return 0
}

// pageHeadingTokens is what a page's heading costs: its title and its
// address, with the blank line after them.
func pageHeadingTokens(sec docsindex.Section) int {
	return estimateTokens("## "+pageTitle(sec)+"\n"+sec.Page+"\n\n") + 1
}

// grow adds piece id to pl, cut, when the answer stays within the budget
// with it; false when it does not fit.
func (p *planner) grow(pl *plan, id int) bool {
	if pl.whole || pl.clip != "" {
		return pl.whole
	}
	add := []int{id}
	for _, w := range unitOf(p.s, id) {
		if w != id && !slices.Contains(pl.pieces, w) {
			add = append(add, w)
		}
	}
	grown := plan{sec: pl.sec, pieces: append(slices.Clone(pl.pieces), add...)}
	slices.Sort(grown.pieces)
	grown.cost = pl.cost + p.render(grown) - p.render(*pl)
	if grown.cost-pl.cost > p.left {
		return false
	}
	p.spend(grown.cost - pl.cost)
	*pl = grown
	p.show(add)
	return true
}

// render is what printing pl's group costs, as the answer shows it under
// its page's title, with the blank line after it.
func (p *planner) render(pl plan) int {
	printed := printedSections(p.r.plans)
	printed[pl.sec] = true
	res := renderGroup(p.lib, p.s, group{sec: pl.sec, pieces: pl.pieces, whole: pl.whole, clip: pl.clip, printed: printed})
	res.URL = shownURL(p.lib, res.URL)
	return estimateTokens(RenderGroup(res, false)) + 1
}

// take prints a plan: its pieces (every piece of a whole section) are
// shown, and its cost is spent.
func (p *planner) take(pl plan) {
	ids := pl.pieces
	if pl.whole {
		ids = p.s.secs[pl.sec].pieces
	}
	p.show(ids)
	p.spend(pl.cost)
	p.r.plans = append(p.r.plans, pl)
	p.pages[p.s.secs[pl.sec].Page] = true
}

func (p *planner) spend(n int) {
	p.left -= n
	p.r.used += n
}

// show marks pieces printed, and their copies with them.
func (p *planner) show(ids []int) {
	for _, id := range ids {
		p.shown[id] = true
		pc := p.s.pieces[id]
		if pc.into >= 0 {
			p.shown[pc.into] = true
			for _, f := range p.s.pieces[pc.into].folded {
				p.shown[f] = true
			}
		}
		for _, f := range pc.folded {
			p.shown[f] = true
		}
	}
}
