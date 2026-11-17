package docs

import (
	"cmp"
	"context"
	"net/url"
	"slices"
)

// answer is the decided answer: the primary page's passages and the extra
// ones, or, when no page has a passage on the question, the first readable
// page as the evidence pipeline prints it.
func (c *dCall) answer(ctx context.Context, req sfRequest, dl *dLib, pages, cands []*dPage, rest []sfHit, others []otherLabel) Outcome {
	var scored []*dPage
	for _, p := range cands {
		if p.probs != nil {
			scored = append(scored, p)
		}
	}
	primary := c.choose(scored)
	var o Outcome
	var notes []string
	var f *dFill
	switch u := c.unscoredBetter(cands, primary); {
	case u != nil:
		// A documentation page the passage pass could not judge never
		// loses to a weaker source: it prints as the evidence pipeline
		// prints it.
		primary = u
		o = c.floorAnswer(ctx, req, u)
		notes = append(notes, "the passages of "+u.pageURL()+" "+u.unscored+"; printed by matching the question's words")
	case primary != nil:
		budget := req.opts.Tokens
		if budget <= 0 {
			budget = localTokens
		}
		f = newFill(c, dl, budget)
		f.primary(primary)
		f.extras(primary, scored)
		o = c.printed(f, req)
	default:
		primary = firstReadable(pages)
		o = c.floorAnswer(ctx, req, primary)
		notes = append(notes, noteNoPassage)
	}
	if primary.otherRel {
		notes = append(notes, noteOtherRel)
	}
	if notMakers(primary) {
		c.noMakers = true
		notes = append(notes, noteNotOfficial+article(primary.label)+primary.label)
	}
	for _, n := range notes {
		o.Note = joinNote(o.Note, n)
	}
	return c.finish(o, req, primary, pages, rest, others, f)
}

// notMakers reports a primary page that is neither documentation nor
// the makers' (tier C, labelled): the answer says no page read is the
// makers' documentation (noteNotOfficial).
func notMakers(primary *dPage) bool { return primary.tier == tierC && primary.label != labelUnlabelled }

// unscoredBetter is the best documentation page (tier A or B) whose
// passages could not be scored, when its tier is better than primary's
// (or there is no primary); nil otherwise.
func (c *dCall) unscoredBetter(cands []*dPage, primary *dPage) *dPage {
	var best *dPage
	for _, p := range cands {
		if p.unscored == "" || (p.tier != tierA && p.tier != tierB) {
			continue
		}
		if best == nil || c.tierKey(p) < c.tierKey(best) {
			best = p
		}
	}
	if best == nil || primary != nil && !tierAfter(primary.tier, best.tier) {
		return nil
	}
	return best
}

// printed is the outcome of a fill: its groups as results, pages in the
// order they were first printed from, a page's groups by their passages'
// best probability (page order on ties), within the budget; what the
// pages read hold of the question's terms; the outline of the printed
// pages' other sections.
func (c *dCall) printed(f *dFill, req sfRequest) Outcome {
	results := f.results()
	for estimateTokens(renderPassages(results)) > f.pl.r.budget && len(f.pl.r.plans) > 1 {
		n := len(f.pl.r.plans) - 1
		delete(f.at, f.pl.r.plans[n].sec)
		f.pl.r.plans = f.pl.r.plans[:n]
		results = f.results()
	}
	o := Answer(SearchBackend, results, nil)
	o.Library = f.dl.lib.Root
	printedIDs := map[int64]bool{}
	var printedPages []string
	for _, pl := range f.pl.r.plans {
		printedIDs[f.dl.s.secs[pl.sec].ID] = true
	}
	for _, p := range f.printedPages() {
		printedPages = append(printedPages, p.page.URL)
		o.Read = append(o.Read, c.readLine(p))
	}
	o.NotFound = f.notFound(printedPages)
	if len(o.NotFound) > 0 {
		o.Status, o.Reason = Partial, ReasonTermsNotFound
		o.Attempts = []Attempt{{Backend: SearchBackend, Status: Partial, Reason: ReasonTermsNotFound}}
	}
	o.Outline, o.OutlineMore, _ = f.dl.l.outlineOf(f.dl.lib, f.dl.g, printedIDs, printedPages)
	return o
}

// printedPages are the pages with a plan, in printed order.
func (f *dFill) printedPages() []*dPage {
	var out []*dPage
	for _, p := range f.order {
		for _, pl := range f.pl.r.plans {
			if f.page[pl.sec] == p {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// results are the plans as results: each page's groups together, pages in
// printed order, a page's groups by best probability, then page order.
func (f *dFill) results() []Result {
	var out []Result
	printed := printedSections(f.pl.r.plans)
	for _, p := range f.printedPages() {
		var plans []plan
		for _, pl := range f.pl.r.plans {
			if f.page[pl.sec] == p {
				plans = append(plans, pl)
			}
		}
		slices.SortStableFunc(plans, func(a, b plan) int {
			return cmp.Or(cmp.Compare(f.best[b.sec], f.best[a.sec]), cmp.Compare(f.dl.s.secs[a.sec].Order, f.dl.s.secs[b.sec].Order))
		})
		for _, pl := range plans {
			r := renderGroup(f.dl.lib, f.dl.s, group{sec: pl.sec, pieces: pl.pieces, whole: pl.whole, clip: pl.clip, printed: printed})
			r.URL = shownURL(f.dl.lib, r.URL)
			r.Section, r.Library = r.URL, hostRoot(p.parsedURL()).String()
			r.SourceLabel, r.Probability = p.label, f.best[pl.sec]
			r.Confidence = lower(tierBand[p.tier], f.c.cut.band(f.best[pl.sec]))
			out = append(out, r)
		}
	}
	return out
}

// notFound are the question's distinctive terms as written that no
// section of the printed pages holds.
func (f *dFill) notFound(pages []string) []string {
	toks := map[string]bool{}
	for _, sec := range f.dl.s.secs {
		if !slices.Contains(pages, sec.Page) {
			continue
		}
		for t := range pieceTokens(sec.Text) {
			toks[t] = true
		}
		for t := range headingSet(append(slices.Clone(sec.Crumbs), sec.Heading)...) {
			toks[t] = true
		}
	}
	var out []string
	for _, wt := range f.dl.g.written {
		if wt.distinctive && !wt.common && !namesTerm(toks, wt) {
			out = append(out, wt.text)
		}
	}
	return out
}

// readLine is page p's read entry.
func (c *dCall) readLine(p *dPage) PageRead {
	return PageRead{URL: p.page.URL, Rank: p.hit.rank, Label: p.label, Tier: p.tier, Context: p.context,
		Confidence: lower(tierBand[p.tier], cmp.Or(p.context, confLow)), Judgments: p.judged}
}

// topProbs are the eight best of probs, best first.
func topProbs(probs []float64) []float64 {
	s := slices.Clone(probs)
	slices.SortFunc(s, func(a, b float64) int { return cmp.Compare(b, a) })
	return s[:min(len(s), 8)]
}

// floorAnswer is the first readable page as the evidence pipeline prints
// it, its sources at low confidence: no page has a passage on the
// question.
func (c *dCall) floorAnswer(ctx context.Context, req sfRequest, p *dPage) Outcome {
	if p.tier == "" {
		p.tier, p.label = tierC, labelUnlabelled
	}
	u := p.parsedURL()
	root := releaseSite(sfSite{root: docsTree(u)}, u).root
	title, name := readTitle(root, nil)
	sp := &sfPages{root: root, title: title, name: name, exact: []webPage{*p.page}, exactBy: []webLink{{url: p.hit.u, title: p.hit.title}},
		reads: []PageRead{{URL: p.page.URL, Rank: p.hit.rank}}}
	o := dressed(c.s.answerRead(ctx, sp, req, sfOrigin{}, nil), req, nil)
	for i := range o.Read {
		o.Read[i] = c.readLine(p)
		o.Read[i].Confidence = confLow
	}
	for i := range o.Results {
		o.Results[i].SourceLabel, o.Results[i].Confidence = p.label, confLow
	}
	return o
}

// finish gives a decided answer what it says beside its text: the
// results passed over and why, the other results with their labels, what
// to read next, the provider used, and the release check (an answer whose
// pages are not marked with the release the question names says so, and
// its confidence is at most medium).
func (c *dCall) finish(o Outcome, req sfRequest, primary *dPage, pages []*dPage, rest []sfHit, others []otherLabel, f *dFill) Outcome {
	printed := map[string]bool{}
	for _, r := range o.Read {
		if u, err := url.Parse(r.URL); err == nil {
			printed[urlKey(u)] = true
		}
	}
	for _, p := range pages {
		if p.page != nil && slices.ContainsFunc(o.Read, func(r PageRead) bool { return r.URL == p.page.URL }) {
			continue
		}
		p.reason = passedReason(p, primary)
		entry := PagePassed{URL: p.pageURL(), Rank: p.hit.rank, Reason: p.reason, Context: p.context, Judgments: p.judged}
		if p.page != nil && !p.stub {
			entry.Label, entry.Tier = p.label, p.tier
		}
		o.PassedOver = append(o.PassedOver, entry)
	}
	o.OtherResults = labelledOthers(otherResults(rest, nil, nil, nil), rest, others)
	o.Next = c.nextSteps(primary, pages, rest, others, printed, f)
	use := c.use
	o.Decisions = &use
	if r, ok := questionRelease(req.query); ok && answered(o) {
		capped := releaseUnmarked(o, r)
		o = markRelease(o, r)
		if capped {
			capConfidence(&o)
		}
	}
	return o
}

// labelledOthers is list (other results of rest) with each one's label.
func labelledOthers(list []OtherResult, rest []sfHit, others []otherLabel) []OtherResult {
	for i := range list {
		k := slices.IndexFunc(rest, func(h sfHit) bool { return h.rank == list[i].Rank })
		if k >= 0 && k < len(others) {
			j := others[k].judgments
			list[i].Label, list[i].Judgments = others[k].label, &j
		}
	}
	return list
}

// capConfidence lowers every high confidence of o to medium.
func capConfidence(o *Outcome) {
	for i := range o.Read {
		o.Read[i].Confidence = lower(o.Read[i].Confidence, confMedium)
	}
	for i := range o.Results {
		o.Results[i].Confidence = lower(o.Results[i].Confidence, confMedium)
	}
}
