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
}

// ranked is Rank's output: the terms' weights and which are distinctive,
// every piece's score, rank and fused value, the candidates in fused
// order, and the plans in the order their sections opened, with why
// sections were passed over.
type ranked struct {
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
	hint  HintStyle
	s     *sorted
	r     *ranked
	left  int
	shown []bool          // by piece: printed, or a copy of a printed piece
	pages map[string]bool // the pages a group is printed of
	cut   bool            // a group is cut: the answer ends with the hint line
}

// printSections walks the candidates in fused order within the budget. A
// piece whose section is not printed yet opens it: the section prints
// whole when it fits the budget left, else cut to the piece; a later piece
// of a cut section joins it when it fits. A piece that does not fit, and
// one that would open a group past --limit's, is passed over and the
// walk goes on: a later, smaller one may fit. The first section always
// prints something, so an answer is never empty for want of room.
func printSections(lib docsindex.Library, s *sorted, r *ranked, hint HintStyle) {
	p := &planner{lib: lib, hint: hint, s: s, r: r, left: r.budget, shown: make([]bool, len(s.pieces)), pages: map[string]bool{}}
	r.passed, r.stop = map[int]string{}, "end"
	at := map[int]int{} // section: its plan
	for _, id := range r.walk {
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
// budget left: whole when it fits, else cut to the piece; false when
// that does not fit either (unless it is the first section).
func (p *planner) open(i, id int) (plan, bool) {
	if p.s.secs[i].tokens <= p.left {
		pl := plan{sec: i, whole: true}
		if pl.cost = p.render(pl) + p.overhead(pl); pl.cost <= p.left {
			return pl, true
		}
	}
	pl := plan{sec: i, pieces: []int{id}}
	pl.cost = p.render(pl) + p.overhead(pl)
	return pl, pl.cost <= p.left || len(p.r.plans) == 0
}

// overhead is what opening a section as pl adds to the answer beyond its
// group: its page's title when no group of the page is printed yet, and
// the answer's hint line when it is the first group cut.
func (p *planner) overhead(pl plan) int {
	n := 0
	if sec := p.s.secs[pl.sec]; !p.pages[sec.Page] {
		n += estimateTokens("## "+pageTitle(sec.Section)+"\n\n") + 1
	}
	if !pl.whole && !p.cut {
		n += hintTokens(AnswerHint(p.hint))
	}
	return n
}

// grow adds piece id to pl, cut, when the answer stays within the budget
// with it; false when it does not fit.
func (p *planner) grow(pl *plan, id int) bool {
	if pl.whole {
		return true
	}
	grown := plan{sec: pl.sec, pieces: append(slices.Clone(pl.pieces), id)}
	slices.Sort(grown.pieces)
	grown.cost = pl.cost + p.render(grown) - p.render(*pl)
	if grown.cost-pl.cost > p.left {
		return false
	}
	p.spend(grown.cost - pl.cost)
	*pl = grown
	p.show([]int{id})
	return true
}

// render is what printing pl's group costs, as the answer shows it under
// its page's title, with the blank line after it.
func (p *planner) render(pl plan) int {
	printed := printedSections(p.r.plans)
	printed[pl.sec] = true
	res := renderGroup(p.lib, p.s, group{sec: pl.sec, pieces: pl.pieces, whole: pl.whole, printed: printed})
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
	p.cut = p.cut || !pl.whole
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
