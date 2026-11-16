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
	c := &dCall{s: s, prov: dec.p, d: dec.d, cut: cutoffs(dec.p.Cutoffs), query: req.query}
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
	dl, err := buildDecidedLib(req.query, cands)
	if err != nil {
		return Outcome{}, err
	}
	defer dl.l.store.Close()
	if err := c.judge(dctx, dl, cands); err != nil {
		return Outcome{}, err
	}
	labels := <-others
	if labels.err != nil {
		return Outcome{}, labels.err
	}
	tr.path = "decisions"
	return c.answer(ctx, req, dl, pages, cands, rest, labels.list), nil
}

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
