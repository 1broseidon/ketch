package docs

import (
	"net/url"
	"slices"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/failure"
)

// The evidence pipeline (design/docs-evidence.md): how the local index
// answers a question, from a library indexed in full or from the pages a
// search just read, held in memory, in four stages with narrow boundaries.
//
//	gather  (*Local).gather:  library, question  → *gathered: terms with counts, the pool of sections, cleaned
//	sort    sortPool:          pool sections      → *sorted: pieces, labelled, folded
//	rank    rank:              pieces, terms      → *ranked: scores, pieces in fused order, what each section prints
//	merge   groupsOf:          plans              → groups, rendered as Results (renderGroup)
//
// One view: the passages are fitted to the budget, 4,000 tokens unless the
// caller sets one; the frontmatter, the outline of what was not printed and
// the other search results are printed beside them, about 600 tokens at
// most. --limit, when given, caps the groups; otherwise the budget decides.
//
// There is no page size (design/docs-evidence.md, Final shape): a section
// prints whole when it fits the budget left; a longer one prints its best
// pieces by rank, shown in page order.
//
// Choose by rank, present in reading order: groups of one page print
// together under its title, a page's sections in page order. An answer
// from the exact pages a search ranked best (LocalOptions.pages) fills
// from the best page first: every piece of it is walked before any of the
// next page's, and when everything read fits the budget it all prints.

const (
	// localTokens is the passages' budget when the caller sets none.
	localTokens = 4000
	// outlineTokens is the most the outline of sections not printed
	// takes; the rest is counted per page.
	outlineTokens = 200
)

// localTokenMargin: an answer is planned to 1/localTokenMargin under its
// budget, about the estimate's error on a whole answer.
const localTokenMargin = 32

// evidenceAnswer is the pipeline's answer: the results in printed order,
// the distinctive terms of the question as written that the library holds
// nowhere, the status and reason the terms decide (refused when no
// distinctive term has a hit: no results; partial when one has none),
// where the question's rare terms are defined when the answer prints no
// section that defines them (evidence_defined.go), and the outline of the
// sections of the pages it read (or printed from) that it does not print.
type evidenceAnswer struct {
	results   []Result
	notFound  []string
	definedIn []DefinedIn
	status    Status
	reason    string
	index     *IndexInfo
	// scope is the URL the answer is limited to (local_scope.go), and
	// outside the distinctive terms the library holds only outside it.
	scope   string
	outside []string
	outline []OutlineEntry
	more    []OutlineMore
}

// ReasonTermsNotFound is the reason of an answer that names terms of the
// question the library holds nowhere: partial when some distinctive term
// has hits, refused when none has.
const ReasonTermsNotFound = "terms_not_found"

// ErrTermsNotFound is a question none of whose distinctive terms is in the
// library: a refusal whose not_found is the answer.
var ErrTermsNotFound = failure.New(failure.NotFound, ReasonTermsNotFound, "none of the question's distinctive terms is in the docs")

// evidence answers query from lib within opts, recording each stage in t
// (nil: no trace).
func (l *Local) evidence(lib docsindex.Library, query string, opts LocalOptions, t *Trace) (*evidenceAnswer, error) {
	var scope *docsindex.Scope
	if opts.scope != "" {
		sc, err := l.scopeSections(lib, opts.scope)
		if err != nil {
			return nil, err
		}
		scope = &sc
	}
	g, err := l.gather(lib, query, scope)
	if err != nil {
		return nil, err
	}
	if opts.live {
		markSearchRead(g.written) // pages a search read are no library
	}
	t.gathered(g)
	v := verdict(g.written, g.sections)
	a := &evidenceAnswer{notFound: v.notFound, status: Found, scope: opts.scope}
	if !opts.live {
		a.index = indexInfo(lib)
	}
	if a.judge(v, scope, g.written); a.status == Refused {
		if len(opts.pages) > 0 {
			a.outline, a.more, err = l.outlineOf(lib, g, nil, opts.pages)
		}
		return a, err
	}
	s, err := l.sortedPool(lib, g, opts.pages)
	if err != nil {
		return nil, err
	}
	t.sorted(s)
	budget := opts.Tokens
	if budget <= 0 {
		budget = localTokens
	}
	r := rank(lib, s, g, budget-budget/localTokenMargin, opts.Limit, pageOrder(opts.pages))
	var defs []definedTerm
	a.results, defs = fitResults(lib, s, r, definitions(s, g.written, g.sections))
	a.definedIn = definedLines(lib, s, defs)
	t.ranked(s, g, r)
	t.defined(s, defs)
	if t != nil {
		t.notShown(notShown(g.written, a.results))
	}
	printed := map[int64]bool{}
	for _, pl := range r.plans {
		printed[s.secs[pl.sec].ID] = true
	}
	pages := slices.Clone(opts.pages)
	for _, page := range printedPages(s, r) {
		if _, ok := r.order[pageKeyOf(page)]; !ok {
			pages = append(pages, page)
		}
	}
	a.outline, a.more, err = l.outlineOf(lib, g, printed, pages)
	return a, err
}

// sortedPool is Sort over Gather's pool and, for an answer from the exact
// pages a search ranked best (pages), every other section of those pages
// after it: any of them can print, and the outline names the rest.
func (l *Local) sortedPool(lib docsindex.Library, g *gathered, pages []string) (*sorted, error) {
	pool := make([]docsindex.Section, len(g.pool))
	order := make([]int, len(g.pool))
	in := map[int64]bool{}
	for i, p := range g.pool {
		pool[i], order[i], in[p.ID] = p.Section, sectionOrder(p.order, g.ordered), true
	}
	if len(pages) > 0 && l.store != nil {
		rest, err := l.store.SectionsOfPages(lib.Key, pages)
		if err != nil {
			return nil, err
		}
		for _, sec := range rest {
			if !in[sec.ID] {
				in[sec.ID] = true
				pool, order = append(pool, cleanSection(sec)), append(order, g.ordered+1)
			}
		}
	}
	s := sortPool(pool, lib.Root)
	for i := range s.secs {
		s.secs[i].order = order[i]
	}
	if err := l.outline(lib, s); err != nil {
		return nil, err
	}
	return s, nil
}

// pageOrder is each page's place among pages (the exact pages a search
// ranked best, best first) by its address (pageKeyOf); nil for none.
func pageOrder(pages []string) map[string]int {
	if len(pages) == 0 {
		return nil
	}
	out := make(map[string]int, len(pages))
	for i, p := range pages {
		if _, ok := out[pageKeyOf(p)]; !ok {
			out[pageKeyOf(p)] = i
		}
	}
	return out
}

// pageKeyOf is a page address as pageOrder keys it: a scheme, "www.", a
// trailing slash or .html aside.
func pageKeyOf(page string) string {
	if u, err := url.Parse(page); err == nil {
		return samePageKey(u)
	}
	return page
}

// judge sets a's status and reason from the terms' verdict v, the scope
// (nil for none) and its written terms' counts under it: refused when no
// distinctive term is in the library, no indexed page is under the scope,
// or no distinctive term is under it; else partial for each of terms not
// found and terms outside the scope, or found. An answer from pages a
// search read is judged the same way: its source line says where the
// pages came from (Build 1).
func (a *evidenceAnswer) judge(v termVerdict, scope *docsindex.Scope, written []writtenTerm) {
	inScope := true // some distinctive term is under the scope
	if scope != nil {
		a.outside, inScope = outsideScope(written)
	}
	var reasons []string
	if v.partial {
		reasons = append(reasons, ReasonTermsNotFound)
	}
	if len(a.outside) > 0 {
		reasons = append(reasons, ReasonTermsOutsideScope)
	}
	if len(reasons) > 0 {
		a.status, a.reason = Partial, strings.Join(reasons, ", ")
	}
	switch {
	case v.refused:
		a.status, a.reason = Refused, ReasonTermsNotFound
	case scope != nil && len(scope.IDs) == 0:
		a.status, a.reason, a.outside = Refused, ReasonScopeEmpty, nil
	case !inScope:
		a.status, a.reason = Refused, ReasonTermsOutsideScope
	}
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
// prints within budget tokens, at most limit groups when limit > 0; order
// is the exact pages' places (pageOrder), nil to walk by rank alone.
func rank(lib docsindex.Library, s *sorted, g *gathered, budget, limit int, order map[string]int) *ranked {
	r := &ranked{terms: g.terms, budget: max(budget, 0), limit: limit, order: order}
	r.weights, r.distinct, r.scores = scorePieces(s, g.terms, g.sections, asksChange(g.query.query))
	rankPieces(s, r)
	printSections(lib, s, r)
	return r
}

// fitResults renders the plans in printed order, with the defined_in
// lines they leave (pickDefined of defs); while the passages' estimate is
// over the planned budget (the per-group sums run a little under the
// joined text's), the last plan opened is dropped.
func fitResults(lib docsindex.Library, s *sorted, r *ranked, defs []definedTerm) ([]Result, []definedTerm) {
	for {
		groups := groupsOf(s, r)
		out := make([]Result, len(groups))
		shown := make([]Result, len(groups))
		for i, g := range groups {
			out[i] = renderGroup(lib, s, g)
			shown[i] = out[i]
			shown[i].URL = shownURL(lib, out[i].URL)
		}
		picked := pickDefined(s, defs, r.plans)
		if estimateTokens(renderPassages(shown)) <= r.budget || len(r.plans) <= 1 {
			return out, picked
		}
		n := len(r.plans) - 1
		r.dropped = append(r.dropped, r.plans[n])
		r.used -= r.plans[n].cost
		r.plans = r.plans[:n]
	}
}
