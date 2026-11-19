package docs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/decide"
)

// A decided answer (the v5.1 prototype, on the bare-question path only):
// when a decision provider is configured, a question that names no
// library is answered from the search's top five results, each read, each
// labelled by the provider, and the passages of the best of them chosen by
// the provider's probabilities; code makes every decision.
//
//	the plain question's search, as today
//	        │
//	the top five read at once (threads, issues, source files not read;
//	almost empty pages passed over); the results after them labelled
//	        │
//	page pass: one request per page (documentation, makers, release, kind)
//	passage pass: every passage of the first three readable pages, then
//	of the other tier A or B pages
//	        │
//	the primary page (context, then tier, then rank), its passages by
//	probability within the budget, then strong passages of other pages
//	        │
//	read / passed_over / next, labels and confidence in words
//	        │       (no page read the makers' docs: the docs site two or
//	        │       more of them link to, read and labelled; decide_follow.go)
//
// Any provider error, refusal or timeout answers the whole call as today,
// with a note saying why; with no provider configured nothing here runs.

// decideTimeout bounds a call's decisions, so the answer without them
// still has time when they fail.
const decideTimeout = 25 * time.Second

// errNoDecision is a call decisions do not apply to (no result in the top
// five reads, or one is on an indexed site): it is answered as today,
// with no note.
var errNoDecision = errors.New("decisions do not apply")

// errDecisionsOff is a call that asks for decisions when the operator
// turned them off: the operator's setting is final, and the answer says so.
var errDecisionsOff = errors.New("decisions are off in the operator's config (docs_decisions)")

// decision is the provider a call asks, or why it cannot: err is set when
// the setting names a provider that is not usable; failed when asking it
// failed.
type decision struct {
	p      decide.Provider
	d      decide.Decider
	err    error
	failed error
}

// decisionFor is the provider req's call asks: the per-call setting
// (LocalOptions.Decisions), else the configured one; nil for none, or for
// a request decisions do not serve.
func (s *SearchFirst) decisionFor(req sfRequest) *decision {
	if s.decisions == nil || !bareRequest(req) {
		return nil
	}
	p, d, ok, err := s.decisions(req.opts.Decisions)
	switch {
	case ok:
		return &decision{p: p, d: d}
	case err != nil:
		return &decision{err: err}
	}
	return nil
}

// usable reports a provider to ask.
func (d *decision) usable() bool { return d != nil && d.err == nil && d.d != nil }

// note is what an answer made without decisions says of them: "" when
// none were asked for, or they did not apply.
func (d *decision) note() string {
	if d == nil {
		return ""
	}
	err := d.err
	if err == nil {
		err = d.failed
	}
	if err == nil || errors.Is(err, errNoDecision) {
		return ""
	}
	if errors.Is(err, errDecisionsOff) {
		return err.Error()
	}
	why := err.Error()
	if !errors.Is(err, decide.ErrUnavailable) {
		why = decide.ErrUnavailable.Error() + ": " + why
	}
	return oneLine(why, 240) + "; answered without them"
}

// withDecisionNote is o with note in its note, or for a failure its error.
func withDecisionNote(o Outcome, note string) Outcome {
	switch {
	case note == "":
	case answered(o):
		o.Note = joinNote(o.Note, note)
	case o.err != nil:
		o.err = fmt.Errorf("%w; %s", o.err, note)
	}
	return o
}

// dCall is one decided answer's state: the provider, its cut-offs, the
// question, and what its requests counted.
type dCall struct {
	s     *SearchFirst
	prov  decide.Provider
	d     decide.Decider
	cut   cutoffs
	query string
	mu    sync.Mutex
	use   DecisionUse
	// releaseNamed is the page pass's reading of whether the question
	// names a release (judge), for labelling a page read later (follow).
	releaseNamed bool
	// noMakers marks an answer whose note says no page read is the
	// makers' documentation (notMakers): a linked docs site may answer
	// instead (decide_follow.go).
	noMakers bool
	// budget is the decisions budget and what it spent and cut
	// (decide_budget.go).
	budget dBudget
}

// ask sends one request and counts what it used.
func (c *dCall) ask(ctx context.Context, req decide.Request) (decide.Response, error) {
	resp, err := c.d.Decide(ctx, req)
	if err != nil {
		return decide.Response{}, err
	}
	c.mu.Lock()
	c.use.Requests += resp.Requests
	c.use.InputTokens += resp.InputTokens
	c.mu.Unlock()
	return resp, nil
}

// answerDecided answers req from hits with dec's provider; errNoDecision
// when decisions do not apply, any other error when asking failed.
func (s *SearchFirst) answerDecided(ctx, cctx context.Context, req sfRequest, hits []sfHit, tr *sfTrace) (Outcome, error) {
	dec := req.dec
	c := &dCall{s: s, prov: dec.p, d: dec.d, cut: cutoffs(dec.p.Cutoffs), query: req.query, budget: dBudget{tokens: s.decisionsTokens}}
	c.use = DecisionUse{Provider: dec.p.ID, Model: dec.p.Model, Cutoffs: CutoffsJSON(dec.p.Cutoffs)}
	n := min(len(hits), decideReadTop)
	top, rest := hits[:n], hits[n:]
	if s.indexedAmong(top) {
		return Outcome{}, errNoDecision
	}
	dctx, cancel := context.WithTimeout(cctx, decideTimeout)
	defer cancel()
	others := c.labelOthers(dctx, rest)
	pages := s.readTop(cctx, top, req.query, tr)
	cands := candidates(pages)
	if len(cands) == 0 {
		return Outcome{}, errNoDecision
	}
	sent := sendable(cands)
	if len(sent) == 0 {
		return Outcome{}, errAllCookies
	}
	dl, err := buildDecidedLib(req.query, cands)
	if err != nil {
		return Outcome{}, err
	}
	defer dl.l.store.Close()
	r := &dRun{c: c, ctx: ctx, cctx: cctx, dctx: dctx, req: req, dl: dl, pages: pages, cands: cands, rest: rest, others: others, tr: tr}
	if c.fitsAll(dl, sent) {
		if err := c.judge(dctx, dl, sent); err != nil {
			return Outcome{}, err
		}
		return r.answer(nil)
	}
	return r.capped(sent)
}

// dRun is one decided call past reading its pages: its contexts (the
// answer's, the call's, the decisions'), the pages read and their library,
// the results after the top five and their labels to come.
type dRun struct {
	c               *dCall
	ctx, cctx, dctx context.Context
	req             sfRequest
	dl              *dLib
	pages, cands    []*dPage
	rest            []sfHit
	others          <-chan otherLabels
	tr              *sfTrace
}

// answer is the decided answer once the pages are judged: the follow when
// no page read is the makers' docs (fs, when the site was read already).
func (r *dRun) answer(fs *followState) (Outcome, error) {
	c := r.c
	labels := <-r.others
	if labels.err != nil {
		return Outcome{}, labels.err
	}
	r.tr.path = "decisions"
	o := c.answer(r.ctx, r.req, r.dl, r.pages, r.cands, r.rest, labels.list)
	switch {
	case !c.noMakers:
	case fs == nil:
		o = c.follow(r.cctx, r.req, r.pages, o, r.tr)
	default:
		c.followJudge(r.cctx, fs)
		o = c.followOutcome(r.req, r.pages, fs, o)
	}
	return c.budgeted(o), nil
}

// capped judges the pages that may be sent (sent) of a call that may not
// fit the budget, and answers.
func (r *dRun) capped(sent []*dPage) (Outcome, error) {
	c := r.c
	due, err := c.labelCapped(r.dctx, sent)
	if err != nil {
		return Outcome{}, err
	}
	// No page read is documentation: the makers' docs the pages link to
	// come before the articles' passages when both would not fit.
	var fs *followState
	if !documented(sent) {
		if fs = c.followRead(r.cctx, r.req, r.pages, r.tr); fs != nil {
			defer fs.close()
			if fs.note == "" && c.passageCost(r.dl, due)+c.passageCost(fs.dl, fs.docs) > c.left() {
				if o, ok := c.followFirst(r.cctx, r.req, r.pages, due, r.rest, r.others, fs, r.tr); ok {
					return o, nil
				}
			}
		}
	}
	if err := c.passagePasses(r.dctx, r.dl, due); err != nil {
		return Outcome{}, err
	}
	setPassageJudgments(sent)
	return r.answer(fs)
}

// followFirst is a capped call whose pages read are not documentation,
// answered from the linked site (fs) asked before the pages' passages:
// accepted, the pages due a passage pass are not asked and are passed
// over; false when the site is not used, the pages then asked from what
// is left.
func (c *dCall) followFirst(ctx context.Context, req sfRequest, pages, due []*dPage, rest []sfHit, others <-chan otherLabels, fs *followState, tr *sfTrace) (Outcome, bool) {
	c.followJudge(ctx, fs)
	if fs.note != "" {
		return Outcome{}, false
	}
	labels := <-others
	today := Outcome{}
	if labels.err == nil {
		today.OtherResults = labelledOthers(otherResults(rest, nil, nil, nil), rest, labels.list)
	}
	for _, p := range due {
		c.counted(len(p.passages), 0)
	}
	tr.path = "decisions"
	return c.budgeted(c.followOutcome(req, pages, fs, today)), true
}

// sendable are the candidates whose text may be sent to the provider:
// those not read with the user's cookies. A page read with them is
// unlabelled and says why.
func sendable(cands []*dPage) []*dPage {
	var out []*dPage
	for _, p := range cands {
		if p.page.cookies {
			p.tier, p.label, p.unscored = tierC, labelUnlabelled, readCookies
			continue
		}
		out = append(out, p)
	}
	return out
}

// readCookies is why a page read with the user's cookies (cookie_file) is
// not labelled; errCookies a follow whose pages all were, errAllCookies a
// call whose pages read all were: it is answered without decisions.
const readCookies = "read with your cookies: not sent to the decision provider"

var (
	errCookies    = errors.New(readCookies)
	errAllCookies = errors.New("every page read was read with your cookies, and none is sent to the decision provider")
)

// indexedAmong reports a result on a site the caller indexed: the index
// answers for it, as today.
func (s *SearchFirst) indexedAmong(hits []sfHit) bool {
	for _, h := range hits {
		if notDocument(h.u) {
			continue
		}
		if _, ok := s.indexedOver(docsTree(h.u)); ok {
			return true
		}
	}
	return false
}

// dPage is one of the top results: read or why not, its labels from the
// page pass and its passages' probabilities.
type dPage struct {
	hit      sfHit
	page     *webPage // nil when it was not read
	cause    string   // why it was not read, or is passed over unlabelled
	stub     bool
	secs     []int // its sections in the library's sorted pool, in page order
	passages []dPassage
	probs    []float64 // by passage; nil when not asked
	// unscored says why the passage pass could not judge the page (no
	// passage, or too much of its text cut from the questions); "" when
	// it did.
	unscored string
	judged   *PageJudgments
	label    string
	tier     string
	otherRel bool
	context  string
	reason   string // why it is passed over
}

// dPassage is consecutive pieces of one section, asked about as one, or
// a run of the lines of one long piece (run set, pieces that piece).
type dPassage struct {
	sec    int
	pieces []int
	run    *lineRun
}

// readTop reads the top results at once, as a bare question reads a
// result (no robots.txt check for a page a search returned): a result
// that is no document by its address is not read; a page that fails, is
// an unrendered shell, has no text or almost none (tinyPage) is passed
// over with its cause; a page under stubWords words is almost empty.
func (s *SearchFirst) readTop(ctx context.Context, top []sfHit, query string, tr *sfTrace) []*dPage {
	pages := make([]*dPage, len(top))
	var wg sync.WaitGroup
	for i, h := range top {
		pages[i] = &dPage{hit: h}
		if notDocument(h.u) {
			pages[i].cause = passedNoDoc
			continue
		}
		wg.Go(func() {
			pctx, cancel := context.WithTimeout(ctx, sfPageTimeout)
			defer cancel()
			pg, err := s.web.fetchIn(pctx, isPublicWebURL, webLink{url: h.u, title: h.title, description: h.desc})
			pages[i].page, pages[i].cause = readOutcome(pg, err, query)
		})
	}
	wg.Wait()
	for _, p := range pages {
		tr.planned++
		if p.page != nil {
			tr.fetched++
			p.stub = len(strings.Fields(p.page.Markdown)) < stubWords
		} else if p.cause != passedNoDoc {
			tr.failed++
		}
	}
	return pages
}

// readOutcome is a read page, or nil and why it gives no text.
func readOutcome(pg *webPage, err error, query string) (*webPage, string) {
	switch {
	case pg == nil:
		f := readFail{}
		f.cause, f.detail = readCause(err)
		return nil, causeText(f)
	case unreadShell(pg):
		return nil, readJS
	case len(strings.Fields(pg.Markdown)) == 0:
		return nil, readEmpty
	case tinyPage(*pg, query):
		return nil, readTiny
	}
	return pg, ""
}

// candidates are the pages read that are no stub: what the provider is
// asked about.
func candidates(pages []*dPage) []*dPage {
	var out []*dPage
	for _, p := range pages {
		if p.page != nil && !p.stub {
			out = append(out, p)
		}
	}
	return out
}

// firstReadable is the first page read, a stub included: the page the
// answer prints when no page has a passage on the question.
func firstReadable(pages []*dPage) *dPage {
	for _, p := range pages {
		if p.page != nil {
			return p
		}
	}
	return nil
}

// otherLabels are the labels of the results after the top five, or why
// they could not be had.
type otherLabels struct {
	list []otherLabel
	err  error
}

// otherLabel is one result's label and the answers behind it.
type otherLabel struct {
	label     string
	judgments ResultJudgments
}

// docsQuestionInput is the shared input of a request about the question
// alone.
type docsQuestionInput struct {
	DocsQuestion string `json:"docs_question"`
}

// searchResultItem is a search result as a question carries it.
type searchResultItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// labelOthers asks, in one request started before the pages are read,
// whether each result after the top five is the makers' and what kind of
// page it is.
func (c *dCall) labelOthers(ctx context.Context, rest []sfHit) <-chan otherLabels {
	out := make(chan otherLabels, 1)
	if len(rest) == 0 {
		out <- otherLabels{}
		return out
	}
	req := decide.Request{Input: docsQuestionInput{DocsQuestion: c.query}}
	for i, h := range rest {
		item := &decide.Item{Name: "search_result", Value: searchResultItem{Title: strings.TrimSpace(h.title), URL: h.u.String(), Snippet: cutWords(h.desc, snippetChars)}}
		req.Questions = append(req.Questions,
			decide.Question{Name: fmt.Sprintf("makers_%d", i), Kind: decide.YesNo, Instructions: otherMakersAsk, Item: item, TrueMeans: makersTrue, FalseMeans: makersFalse},
			decide.Question{Name: fmt.Sprintf("kind_%d", i), Kind: decide.Choice, Instructions: otherKindAsk, Item: item, Options: kindOptions})
	}
	if !c.fits(cost(req)) {
		c.budget.othersCut = true
		out <- otherLabels{}
		return out
	}
	go func() {
		resp, err := c.ask(ctx, req)
		if err != nil {
			out <- otherLabels{err: err}
			return
		}
		list := make([]otherLabel, len(rest))
		for i := range rest {
			m, k := resp.Answers[fmt.Sprintf("makers_%d", i)], resp.Answers[fmt.Sprintf("kind_%d", i)]
			list[i] = otherLabel{label: c.cut.otherLabel(m.Probability, k.Choice),
				judgments: ResultJudgments{Makers: m.Probability, Kind: k.Choice, KindProbability: k.Probabilities[k.Choice]}}
		}
		out <- otherLabels{list: list}
	}()
	return out
}

// cutWords is s on one line, cut at a word to about n bytes with " …".
func cutWords(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	cut := strings.ToValidUTF8(s[:n], "")
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return cut + " …"
}

// pageURL is p's address as read.
func (p *dPage) pageURL() string {
	if p.page != nil {
		return p.page.URL
	}
	return p.hit.u.String()
}

// parsedURL is pageURL parsed (the hit's when it does not parse).
func (p *dPage) parsedURL() *url.URL {
	if u, err := url.Parse(p.pageURL()); err == nil {
		return u
	}
	return p.hit.u
}
