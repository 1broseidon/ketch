package docs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/decide"
)

// The decisions budget: a decided answer sends at most this many input
// tokens to the provider, estimated as the providers pack their requests
// (decide.Estimate), unless the operator sets docs_decisions_tokens to
// unlimited. The calling agent cannot lift it: it is a config key only.
//
//	the other results' labels (one small request, sent first)
//	        │
//	page passes, in search order while they fit; a page after the first
//	that does not fit is unlabelled, as is every page after it
//	        │
//	passage passes: the rest shared by the pages asked (the first three
//	labelled, then tier A or B): an equal share each, a page that needs
//	less returning the rest to those that need more, in search order;
//	within a page, the passages that best match the question's words
//	first (as the evidence pipeline scores a piece), each whole or not
//	        │
//	the follow (decide_follow.go) from what is left: not read when one
//	page pass cannot fit, not used when its pages' labels cannot
//
// A passage not sent is unjudged: it has no probability, is never
// printed by the fill (which prints judged passages above Low), and the
// page it is on is judged by the passages that were sent. When the budget
// cut something, the decisions say so (budget) and so does the note.
// When everything fits, the call is today's, request for request.

const (
	// DefaultDecisionsTokens is the decisions budget when
	// docs_decisions_tokens is unset: input tokens per ketch docs call.
	DefaultDecisionsTokens = 80000
	// DecisionsUnlimited is the docs_decisions_tokens value that lifts it.
	DecisionsUnlimited = "unlimited"
)

// ParseDecisionsTokens reads a docs_decisions_tokens value: the budget in
// input tokens, 0 for unlimited; "" is the default.
func ParseDecisionsTokens(value string) (int, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "":
		return DefaultDecisionsTokens, nil
	case DecisionsUnlimited:
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%q: want a positive number of input tokens per call, or %s", value, DecisionsUnlimited)
	}
	return n, nil
}

// BudgetUse is what the decisions budget cut from a call: its tokens, the
// passages judged of those the pages asked hold, the pages labelled of
// those read.
type BudgetUse struct {
	Tokens         int `json:"tokens"`
	JudgedPassages int `json:"judged_passages"`
	Passages       int `json:"passages"`
	LabelledPages  int `json:"labelled_pages"`
	Pages          int `json:"pages"`
}

// line is the budget as the decisions line says it.
func (b BudgetUse) line() string {
	parts := []string{fmt.Sprintf("budget %d tokens", b.Tokens)}
	if b.LabelledPages < b.Pages {
		parts = append(parts, fmt.Sprintf("labelled %d of %d pages", b.LabelledPages, b.Pages))
	}
	return strings.Join(append(parts, fmt.Sprintf("judged %d of %d passages", b.JudgedPassages, b.Passages)), ", ")
}

// The passed_over reason and unscored cause the budget gives.
const (
	budgetSpent      = "the decisions budget was spent"
	passedUnlabelled = "not labelled: " + budgetSpent
	unscoredBudget   = "could not be scored: " + budgetSpent
)

// errBudget is a budget too small for one page pass: the call is
// answered without decisions.
var errBudget = errors.New("the decisions budget is smaller than one page's labels")

// dBudget is a call's budget and what it spent and cut, guarded by the
// call's mutex.
type dBudget struct {
	tokens int // 0: unlimited
	spent  int
	// what it cut: pages read and labelled, passages of the pages asked
	// and judged, the other results not labelled, the follow not done
	pages, labelled    int
	passages, judged   int
	othersCut, follows bool
}

// cost is req's estimated input tokens.
func cost(req decide.Request) int {
	in, qs := decide.Estimate(req)
	for _, n := range qs {
		in += n
	}
	return in
}

// left is what the budget has left; MaxInt when unlimited.
func (c *dCall) left() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.budget.tokens == 0 {
		return math.MaxInt
	}
	return c.budget.tokens - c.budget.spent
}

// charge counts n tokens spent.
func (c *dCall) charge(n int) {
	c.mu.Lock()
	c.budget.spent += n
	c.mu.Unlock()
}

// fits reports n tokens within what is left, and charges them when so.
func (c *dCall) fits(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.budget.tokens > 0 && c.budget.spent+n > c.budget.tokens {
		return false
	}
	c.budget.spent += n
	return true
}

// counted records a page's passage pass: its passages and those judged.
func (c *dCall) counted(passages, judged int) {
	c.mu.Lock()
	c.budget.passages += passages
	c.budget.judged += judged
	c.mu.Unlock()
}

// cutAny reports the budget cut something.
func (c *dCall) cutAny() bool {
	b := c.budget
	return b.tokens > 0 && (b.judged < b.passages || b.labelled < b.pages || b.othersCut || b.follows)
}

// budgeted is o with what the budget cut in its decisions and note; o as
// it is when nothing was cut.
func (c *dCall) budgeted(o Outcome) Outcome {
	if !c.cutAny() || o.Decisions == nil {
		return o
	}
	b := c.budget
	use := c.use
	use.Budget = &BudgetUse{Tokens: b.tokens, JudgedPassages: b.judged, Passages: b.passages, LabelledPages: b.labelled, Pages: b.pages}
	o.Decisions = &use
	var did []string
	if b.labelled < b.pages {
		did = append(did, fmt.Sprintf("labelled %d of %d pages read", b.labelled, b.pages))
	}
	if b.othersCut {
		did = append(did, "did not label the other results")
	}
	if b.judged < b.passages {
		did = append(did, fmt.Sprintf("judged %d of %d passages", b.judged, b.passages))
	}
	hint := fmt.Sprintf("budget %d tokens; ketch config set docs_decisions_tokens to raise it", b.tokens)
	if len(did) == 0 {
		o.Note = joinNote(o.Note, "decisions stopped at their budget ("+hint+")")
	} else {
		o.Note = joinNote(o.Note, "decisions "+strings.Join(did, ", ")+" ("+hint+")")
	}
	return o
}

// labelCapped is the page passes of a call that may not fit the budget:
// in search order while they fit, the rest unlabelled. It labels the pages
// and returns those due a passage pass (the first firstPassagePages
// labelled, then tier A or B), which are asked separately (passagePasses).
func (c *dCall) labelCapped(ctx context.Context, cands []*dPage) ([]*dPage, error) {
	var reqs []decide.Request
	for _, p := range cands {
		req := c.pageRequest(p)
		if !c.fits(cost(req)) {
			break
		}
		reqs = append(reqs, req)
	}
	c.budget.pages, c.budget.labelled = len(cands), len(reqs)
	if len(reqs) == 0 {
		return nil, fmt.Errorf("%w (%d tokens)", errBudget, c.budget.tokens)
	}
	answers := make([]decide.Response, len(reqs))
	errs := make([]error, len(reqs))
	var wg sync.WaitGroup
	for i, req := range reqs {
		wg.Go(func() { answers[i], errs[i] = c.ask(ctx, req) })
	}
	wg.Wait()
	if err := firstError(errs); err != nil {
		return nil, err
	}
	c.releaseNamed = c.cut.hi(median(releaseAnswers(answers)))
	var due []*dPage
	for i, p := range cands {
		if i >= len(reqs) {
			p.tier, p.label, p.unscored = tierC, labelUnlabelled, passedUnlabelled
			continue
		}
		c.label(p, answers[i], c.releaseNamed)
		if i < firstPassagePages || p.tier == tierA || p.tier == tierB {
			due = append(due, p)
		}
	}
	return due, nil
}

// passageCost is the passage passes of pages, every passage sent.
func (c *dCall) passageCost(dl *dLib, pages []*dPage) int {
	n := 0
	for _, p := range pages {
		if len(p.passages) > 0 {
			req, _, _ := c.passageRequest(dl, p, nil)
			n += cost(req)
		}
	}
	return n
}

// documented reports a labelled page of tier A or B among pages.
func documented(pages []*dPage) bool {
	for _, p := range pages {
		if p.judged != nil && (p.tier == tierA || p.tier == tierB) {
			return true
		}
	}
	return false
}

// passagePasses asks the passage pass of pages at once, each within its
// share of what the budget has left (share).
func (c *dCall) passagePasses(ctx context.Context, dl *dLib, pages []*dPage) error {
	send := c.share(dl, pages)
	errs := make([]error, len(pages))
	var wg sync.WaitGroup
	for i, p := range pages {
		wg.Go(func() { errs[i] = c.passagePass(ctx, dl, p, send[i]) })
	}
	wg.Wait()
	return firstError(errs)
}

// share is, by page, the passages each passage pass sends within what the
// budget has left, in page order: every page an equal share, a page that
// needs less than its share sending all its passages and leaving the rest
// to the others, which then share it in search order, each sending the
// passages that best match the question's words (passageOrder) that fit
// its share. nil sends all of a page's passages.
func (c *dCall) share(dl *dLib, pages []*dPage) [][]int {
	out := make([][]int, len(pages))
	left := c.left()
	if left == math.MaxInt {
		return out
	}
	base := make([]int, len(pages))
	each := make([][]int, len(pages))
	need := make([]int, len(pages))
	full := make([]bool, len(pages))
	for i, p := range pages {
		if len(p.passages) == 0 {
			full[i] = true
			continue
		}
		req, _, _ := c.passageRequest(dl, p, nil)
		base[i], each[i] = decide.Estimate(req)
		need[i] = base[i]
		for _, n := range each[i] {
			need[i] += n
		}
	}
	open := func() int {
		n := 0
		for _, f := range full {
			if !f {
				n++
			}
		}
		return n
	}
	for n := open(); n > 0; n = open() {
		quota, more := left/n, false
		for i := range pages {
			if !full[i] && need[i] <= quota {
				full[i], more, left = true, true, left-need[i]
			}
		}
		if !more {
			break
		}
	}
	n := open()
	for i, p := range pages {
		if full[i] {
			continue
		}
		quota := left / n
		n--
		sel := []int{}
		used := base[i]
		for _, k := range passageOrder(dl, p) {
			if used+each[i][k] <= quota {
				sel, used = append(sel, k), used+each[i][k]
			}
		}
		if len(sel) > 0 {
			left -= used
		}
		slices.Sort(sel)
		out[i] = sel
	}
	return out
}

// passageOrder is p's passages, the best match for the question's words
// first: each scored as the evidence pipeline scores a piece (the
// question's terms its text and heading path hold, by their rarity, by
// its page role; scorePieces), page order on ties.
func passageOrder(dl *dLib, p *dPage) []int {
	weights, _ := evidenceWeights(dl.g.terms, dl.g.sections)
	change := asksChange(dl.g.query.query)
	score := make([]float64, len(p.passages))
	order := make([]int, len(p.passages))
	for k, ps := range p.passages {
		sec := dl.s.secs[ps.sec]
		path := headingSet(append(slices.Clone(sec.Crumbs), sec.Heading)...)
		_, sum := termSum(pieceTokens(passageText(dl.s, ps)), path, dl.g.terms, weights)
		score[k], order[k] = roleFactor(sec.role, change)*sum, k
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(score[b], score[a]) })
	return order
}

// unjudged is the probability of a passage not sent: below every
// cut-off, so no rule reads it as on the question, and never printed as
// a probability.
const unjudged = -1.0

// judgedProbs are probs without the passages not sent.
func judgedProbs(probs []float64) []float64 {
	var out []float64
	for _, p := range probs {
		if p != unjudged {
			out = append(out, p)
		}
	}
	return out
}

// setPassageJudgments gives each page asked its passage pass's judgments:
// how many passages were judged and the best of them.
func setPassageJudgments(pages []*dPage) {
	for _, p := range pages {
		if p.judged != nil && p.probs != nil {
			j := judgedProbs(p.probs)
			p.judged.Passages, p.judged.TopPassages = len(j), topProbs(j)
		}
	}
}
