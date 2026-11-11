package docs

import (
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/failure"
)

// The evidence pipeline (design/docs-evidence.md): how the local index
// answers a question, from a library indexed in full or from the pages a
// search just read and stored, in four stages with narrow boundaries.
//
//	gather  (*Local).gather:  library, question  → *gathered: terms with counts, the pool of sections, cleaned
//	sort    sortPool:          pool sections      → *sorted: pieces, labelled, folded
//	rank    rank:              pieces, terms      → *ranked: scores, pieces in fused order, what each section prints
//	merge   groupsOf:          plans              → groups, rendered as Results (renderGroup)
//
// One view: the answer is fitted to the budget, 1,800 tokens unless the
// caller sets one, which covers everything printed, frontmatter
// included. --limit, when given, caps the groups; otherwise the budget
// decides.

const (
	// localTokens is an answer's budget when the caller sets none.
	localTokens = 1800
	// localTokenMargin: an answer is planned to 1/localTokenMargin under
	// its budget, about the estimate's error on a whole answer.
	localTokenMargin = 32
)

// evidenceAnswer is the pipeline's answer: the results in printed
// order, the question's terms as written that the library holds and the
// frontmatter lists (shownTerm, with their counts), the distinctive ones
// it holds nowhere, and the status
// and reason the terms decide (refused when no distinctive term has a
// hit: no results; partial when one has none).
type evidenceAnswer struct {
	results  []Result
	terms    []TermCount
	notFound []string
	status   Status
	reason   string
	index    *IndexInfo
}

// ReasonTermsNotFound is the reason of an answer that names terms of the
// question the library holds nowhere: partial when some distinctive term
// has hits, refused when none has.
const ReasonTermsNotFound = "terms_not_found"

// ErrTermsNotFound is a question none of whose distinctive terms is in the
// library: a refusal whose not_found is the answer.
var ErrTermsNotFound = failure.New(failure.NotFound, ReasonTermsNotFound, "none of the question's distinctive terms is in the docs")

// frontmatter is the frontmatter a's answer to query from lib prints,
// with note (a partial answer's): what the budget is planned under.
func (a *evidenceAnswer) frontmatter(lib docsindex.Library, query, note string) Frontmatter {
	nf := a.notFound
	if nf == nil {
		nf = []string{}
	}
	return Frontmatter{Query: query, Backend: SearchBackend, Library: lib.Root, Version: lib.Version, Index: a.index,
		Status: a.status, Reason: a.reason, Terms: nonNil(a.terms), NotFound: nf, Note: note}
}

// evidence answers query from lib within opts, recording each stage in t
// (nil: no trace).
func (l *Local) evidence(lib docsindex.Library, query string, opts LocalOptions, t *Trace) (*evidenceAnswer, error) {
	g, err := l.gather(lib, query)
	if err != nil {
		return nil, err
	}
	t.gathered(g)
	v := verdict(g.written, g.sections)
	a := &evidenceAnswer{terms: v.found, notFound: v.notFound, status: Answered, index: indexInfo(lib)}
	if opts.read > 0 {
		a.index = searchRead(opts.read)
	}
	var reasons []string
	if opts.note != "" {
		reasons = append(reasons, ReasonPartialSite)
	}
	if v.partial {
		reasons = append(reasons, ReasonTermsNotFound)
	}
	if len(reasons) > 0 {
		a.status, a.reason = Partial, strings.Join(reasons, ", ")
	}
	if v.refused {
		a.status, a.reason = Refused, ReasonTermsNotFound
		return a, nil
	}
	pool := make([]docsindex.Section, len(g.pool))
	for i, p := range g.pool {
		pool[i] = p.Section
	}
	s := sortPool(pool, lib.Root)
	for i, p := range g.pool {
		s.secs[i].order = sectionOrder(p.order, g.ordered)
	}
	if err := l.outline(lib, s); err != nil {
		return nil, err
	}
	t.sorted(s)
	budget := opts.Tokens
	if budget <= 0 {
		budget = localTokens
	}
	plan := budget - budget/localTokenMargin - estimateTokens(a.frontmatter(lib, query, opts.note).Render())
	r := rank(lib, s, g, plan, opts.Limit, opts.Hint)
	a.results = fitResults(lib, s, r, opts.Hint)
	t.ranked(s, g, r)
	if t != nil {
		t.notShown(notShown(g.written, a.results))
	}
	return a, nil
}

// sectionOrder is a pool section's rank in the whole question's ordering
// for Rank's fusion: its place there (order, 1 the first), or for a
// section the ordering leaves out (0), the place after all ordered
// sections.
func sectionOrder(order, ordered int) int {
	if order > 0 {
		return order
	}
	return ordered + 1
}

// rank scores the pieces, puts the sections in order and plans what each
// prints within budget tokens, at most limit groups when limit > 0.
func rank(lib docsindex.Library, s *sorted, g *gathered, budget, limit int, hint HintStyle) *ranked {
	r := &ranked{budget: max(budget, 0), limit: limit}
	r.weights, r.distinct, r.scores = scorePieces(s, g.terms, g.sections, asksChange(g.query.query))
	rankPieces(s, r)
	printSections(lib, s, r, hint)
	return r
}

// fitResults renders the plans in printed order; while the printed
// answer's estimate is over the planned budget (the per-group sums run a
// little under the joined text's), the last plan opened is dropped.
func fitResults(lib docsindex.Library, s *sorted, r *ranked, hint HintStyle) []Result {
	for {
		groups := groupsOf(s, r)
		out := make([]Result, len(groups))
		shown := make([]Result, len(groups))
		for i, g := range groups {
			out[i] = renderGroup(lib, s, g)
			shown[i] = out[i]
			shown[i].URL = shownURL(lib, out[i].URL)
		}
		total := estimateTokens(RenderAnswer(shown, hint))
		if total <= r.budget || len(r.plans) <= 1 {
			return out
		}
		n := len(r.plans) - 1
		r.dropped = append(r.dropped, r.plans[n])
		r.used -= r.plans[n].cost
		r.plans = r.plans[:n]
	}
}
