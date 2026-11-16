package docs

import (
	"cmp"
	"slices"
)

// dFill fills a decided answer's budget with passages, through the
// evidence pipeline's planner: a passage opens its section cut to its
// pieces, a later passage of the section joins it, and costs are what the
// groups render to.
type dFill struct {
	c     *dCall
	dl    *dLib
	pl    *planner
	at    map[int]int     // section: its plan
	page  map[int]*dPage  // section: its page
	best  map[int]float64 // section: its passages' best probability
	order []*dPage        // the pages printed, in printed order
	text  printedText
	// choice is what each section prints, by passage; fixed marks a
	// section printed whole, or cut by the planner, that takes no more.
	choice map[int]*secChoice
	fixed  map[int]bool
	// clipped is the primary's top passage printed cut (it alone was
	// larger than the budget), and its probability; nil for none.
	clipped     *dPassage
	clippedProb float64
}

func newFill(c *dCall, dl *dLib, budget int) *dFill {
	r := &ranked{terms: dl.g.terms, budget: max(budget-budget/localTokenMargin, 0), passed: map[int]string{}, stop: "end"}
	r.weights, r.distinct = evidenceWeights(dl.g.terms, dl.g.sections)
	p := &planner{lib: dl.lib, s: dl.s, r: r, left: r.budget, shown: make([]bool, len(dl.s.pieces)), pages: map[string]bool{}}
	return &dFill{c: c, dl: dl, pl: p, at: map[int]int{}, page: map[int]*dPage{}, best: map[int]float64{},
		choice: map[int]*secChoice{}, fixed: map[int]bool{}}
}

// labelCost is what a decided group adds to its rendering: the confidence
// on its source line and, for a page's first group, the label on its
// address line.
func (f *dFill) labelCost(pl plan, p *dPage) int {
	n := estimateTokens(" (confidence medium)")
	if !f.pl.pages[f.dl.s.secs[pl.sec].Page] {
		n += estimateTokens(" (" + p.label + ")")
	}
	return n
}

// cost is what adding ps of page p costs; 0 when it is printed already.
func (f *dFill) cost(p *dPage, ps dPassage) int {
	if k, ok := f.at[ps.sec]; ok {
		if f.fixed[ps.sec] {
			return 0
		}
		return f.pl.render(f.choice[ps.sec].with(ps).plan(f.dl.s, ps.sec)) - f.pl.render(f.pl.r.plans[k])
	}
	pl := (*secChoice)(nil).with(ps).plan(f.dl.s, ps.sec)
	return f.pl.render(pl) + f.pl.overhead(pl) + f.labelCost(pl, p)
}

// commit prints ps of page p at cost t.
func (f *dFill) commit(p *dPage, ps dPassage, t int) {
	if f.fixed[ps.sec] {
		return
	}
	f.note(p, ps.sec)
	c := f.choice[ps.sec].with(ps)
	f.choice[ps.sec] = c
	next := c.plan(f.dl.s, ps.sec)
	if k, ok := f.at[ps.sec]; ok {
		pl := &f.pl.r.plans[k]
		pl.pieces, pl.clip = next.pieces, next.clip
		pl.cost += t
		f.pl.spend(t)
		f.pl.show(ps.pieces)
	} else {
		f.at[ps.sec] = len(f.pl.r.plans)
		next.cost = t
		f.pl.take(next)
	}
	f.text.add(passageText(f.dl.s, ps))
}

// note records section sec as printed from page p.
func (f *dFill) note(p *dPage, sec int) {
	f.page[sec] = p
	if !slices.Contains(f.order, p) {
		f.order = append(f.order, p)
	}
}

func union(a, b []int) []int {
	out := slices.Clone(a)
	for _, id := range b {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// bestOf records the best probability of each section of p's passages.
func (f *dFill) bestOf(p *dPage) {
	for k, ps := range p.passages {
		if k < len(p.probs) && p.probs[k] > f.best[ps.sec] {
			f.best[ps.sec] = p.probs[k]
		}
	}
}

// byProbability are p's passages at or above (strict: above) minimum, the most
// probable first, page order on ties.
func byProbability(p *dPage, minimum float64, strict bool) []int {
	var out []int
	for k, pr := range p.probs {
		if pr > minimum || !strict && pr == minimum {
			out = append(out, k)
		}
	}
	slices.SortStableFunc(out, func(a, b int) int { return cmp.Or(cmp.Compare(p.probs[b], p.probs[a]), cmp.Compare(a, b)) })
	return out
}

// primary fills from the primary page: the whole page when it fits;
// otherwise its passages above Low, the most probable first, each when it
// fits; when the most probable alone is larger than the budget, its
// start, cut, and nothing more.
func (f *dFill) primary(p *dPage) {
	f.bestOf(p)
	if len(p.secs) > 0 && f.pl.wholeFits([][]int{p.secs}, f.at) {
		for _, i := range p.secs {
			f.note(p, i)
			f.fixed[i] = true
			f.text.add(f.dl.s.secs[i].Text)
		}
		return
	}
	chosen := false
	for _, k := range byProbability(p, f.c.cut.Low, true) {
		ps := p.passages[k]
		if t := f.cost(p, ps); t <= f.pl.left {
			f.commit(p, ps, t)
			chosen = true
			continue
		}
		if !chosen && f.pl.left >= clipMinTokens {
			f.note(p, ps.sec)
			f.at[ps.sec], f.fixed[ps.sec] = len(f.pl.r.plans), true
			f.pl.take(f.pl.clipped((*secChoice)(nil).with(ps).plan(f.dl.s, ps.sec)))
			f.clipped, f.clippedProb = &p.passages[k], p.probs[k]
			return
		}
	}
}

// extras adds passages of other pages after the primary's, never padding:
// strong passages (High) of pages of the same tier or better; under a tier
// A page, passages of tier B pages at VeryHigh or more, at most
// lowerTokens together; never a tier C page under tier A or B. A page at
// the primary's address (urlKey) is skipped, and so is a passage dupShare
// or more of which is printed already.
func (f *dFill) extras(primary *dPage, scored []*dPage) {
	if f.pl.left <= addMinTokens {
		return
	}
	var others []*dPage
	for _, p := range scored {
		if p != primary && p.probs != nil && urlKey(p.parsedURL()) != urlKey(primary.parsedURL()) {
			others = append(others, p)
		}
	}
	slices.SortStableFunc(others, func(a, b *dPage) int {
		return cmp.Or(cmp.Compare(f.c.tierKey(a), f.c.tierKey(b)), cmp.Compare(a.hit.rank, b.hit.rank))
	})
	f.strongExtras(primary, others)
	if primary.tier == tierA {
		f.lowerExtras(ofTier(others, tierB))
	}
}

// strongExtras adds the strong passages of others of primary's tier or
// better.
func (f *dFill) strongExtras(primary *dPage, others []*dPage) {
	for _, p := range others {
		if tierAfter(p.tier, primary.tier) {
			continue
		}
		f.bestOf(p)
		for _, k := range byProbability(p, f.c.cut.High, false) {
			ps := p.passages[k]
			if t := f.cost(p, ps); f.pl.left > addMinTokens && t <= f.pl.left && !f.repeat(ps) {
				f.commit(p, ps, t)
			}
		}
	}
}

// lowerExtras adds passages at VeryHigh or more of lower-tier
// documentation pages, at most lowerTokens together.
func (f *dFill) lowerExtras(pages []*dPage) {
	lower := 0
	for _, p := range pages {
		f.bestOf(p)
		for _, k := range byProbability(p, f.c.cut.VeryHigh, false) {
			if f.pl.left <= addMinTokens {
				break
			}
			ps := p.passages[k]
			if t := f.cost(p, ps); t <= f.pl.left && lower+t <= lowerTokens && !f.repeat(ps) {
				f.commit(p, ps, t)
				lower += t
			}
		}
	}
}

// repeat reports a passage dupShare or more of whose text is printed.
func (f *dFill) repeat(ps dPassage) bool {
	return f.text.share(passageText(f.dl.s, ps)) >= dupShare
}

// cutSection is the address of the primary page's section holding its
// most probable strong passage not printed (or its top passage printed
// cut), when that section has an anchor; "" for none.
func (f *dFill) cutSection(p *dPage) string {
	if f == nil || p.probs == nil {
		return ""
	}
	best, at := -1.0, ""
	consider := func(ps dPassage, pr float64) {
		sec := f.dl.s.secs[ps.sec]
		if sec.Anchor != "" && pr > best {
			best, at = pr, shownURL(f.dl.lib, webSectionOf(sec.Section, 0).URL())
		}
	}
	for k, ps := range p.passages {
		if p.probs[k] >= f.c.cut.High && !f.allShown(ps) {
			consider(ps, p.probs[k])
		}
	}
	if f.clipped != nil {
		consider(*f.clipped, f.clippedProb)
	}
	return at
}

// allShown reports ps printed in full.
func (f *dFill) allShown(ps dPassage) bool {
	k, ok := f.at[ps.sec]
	if !ok {
		return false
	}
	if pl := f.pl.r.plans[k]; pl.whole {
		return true
	}
	return f.choice[ps.sec].holds(ps)
}
