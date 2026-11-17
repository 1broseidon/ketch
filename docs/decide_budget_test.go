package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// spentBy is the estimated input tokens of the requests d was sent, of
// one kind ("page", "passage", "others"; "" for all), and of those about
// the page at url when url is set.
func spentBy(d *fakeDecider, kind, url string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, r := range d.reqs {
		k, u := "", ""
		switch in := r.Input.(type) {
		case pageInput:
			k, u = "page", in.Page.URL
		case passageInput:
			k, u = "passage", in.Page.URL
		case docsQuestionInput:
			k = "others"
		}
		if (kind == "" || kind == k) && (url == "" || url == u) {
			n += cost(r)
		}
	}
	return n
}

// passageAsks are the passage questions d was asked about the page at
// url, by text.
func passageAsks(d *fakeDecider, url string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, r := range d.reqs {
		if in, ok := r.Input.(passageInput); ok && in.Page.URL == url {
			for _, q := range r.Questions {
				out = append(out, q.Item.Value.(passageItem).Text)
			}
		}
	}
	return out
}

// longTimeouts is the official page with n sections about other things
// before the one that answers the question.
func longTimeouts(n int) fakePage {
	var secs []string
	for i := range n {
		secs = append(secs, fmt.Sprintf("## Panel %d\n\nColours and themes for panel %d of the layout, with spacing, borders and margins.", i, i))
	}
	return docsPage("Timeouts", append(secs, "## Read timeout\n\nThe read_timeout setting defaults to 10 seconds. OFFICIALFACT.")...)
}

// decidedBudget is a backend over f and hits that asks d within tokens.
func decidedBudget(t *testing.T, tokens int, d *fakeDecider, mutate func(*fakeFetcher)) Outcome {
	t.Helper()
	f, hits := decidedSite()
	if mutate != nil {
		mutate(f)
	}
	s := decidedOver(t, f, hits, d)
	s.decisionsTokens = tokens
	return s.SearchSelect(context.Background(), widgetQuestion, 0)
}

const budgetHint = "; ketch config set docs_decisions_tokens to raise it)"

// (a) A page with many passages over the budget is cut: the passages that
// best match the question's words are sent, the rest unjudged and never
// given a probability; the decisions and the note say how many.
func TestDecidedBudgetCutsLongPage(t *testing.T) {
	long := func(f *fakeFetcher) { f.pages[pgOfficial] = longTimeouts(80) }
	free := decidedJudgments()
	decidedBudget(t, 0, free, long)
	all := len(passageAsks(free, pgOfficial))
	budget := spentBy(free, "", "") - spentBy(free, "passage", pgOfficial)/2

	d := decidedJudgments()
	o := decidedBudget(t, budget, d, long)
	if spent := spentBy(d, "", ""); spent > budget {
		t.Errorf("spent %d estimated tokens, budget %d", spent, budget)
	}
	asked := passageAsks(d, pgOfficial)
	if len(asked) == 0 || len(asked) >= all {
		t.Fatalf("asked %d of the official page's %d passages", len(asked), all)
	}
	if !strings.Contains(asked[0]+strings.Join(asked, ""), "OFFICIALFACT") {
		t.Errorf("the passage that matches the question's words was not sent: %q", asked)
	}
	if len(o.Read) == 0 || o.Read[0].URL != pgOfficial || o.Read[0].Context != confHigh {
		t.Fatalf("read %+v: want the partly judged official page, on the strength of its judged passages", o.Read)
	}
	if j := o.Read[0].Judgments; j == nil || j.Passages != len(asked) || len(j.TopPassages) > len(asked) {
		t.Errorf("judgments %+v: want %d passages judged, no probability for one not sent", j, len(asked))
	}
	b := o.Decisions.Budget
	if b == nil || b.Tokens != budget || b.JudgedPassages >= b.Passages || b.LabelledPages != b.Pages {
		t.Fatalf("budget %+v", b)
	}
	clause := fmt.Sprintf("decisions judged %d of %d passages (budget %d tokens", b.JudgedPassages, b.Passages, budget) + budgetHint
	if !strings.HasSuffix(o.Note, clause) {
		t.Errorf("note %q, want it to end %q", o.Note, clause)
	}
	out := renderAll(widgetQuestion, o)
	if want := fmt.Sprintf("decisions: fake (budget %d tokens, judged %d of %d passages)\n", budget, b.JudgedPassages, b.Passages); !strings.Contains(out, want) || !strings.Contains(out, "OFFICIALFACT") {
		t.Errorf("output lacks %q or the answer:\n%s", want, out)
	}
	js, _ := json.Marshal(NewAnswerJSON(widgetQuestion, SearchBackend, "", o))
	if want := fmt.Sprintf(`"budget":{"tokens":%d,"judged_passages":%d,"passages":%d,"labelled_pages":4,"pages":4}`, budget, b.JudgedPassages, b.Passages); !strings.Contains(string(js), want) {
		t.Errorf("JSON lacks %s:\n%s", want, js)
	}
}

// (b) Three pages asked, one huge and two small: the small ones are
// judged in full, the huge one gets what they leave.
func TestDecidedBudgetSharesPages(t *testing.T) {
	long := func(f *fakeFetcher) { f.pages[pgOfficial] = longTimeouts(120) }
	three := func() *fakeDecider {
		d := decidedJudgments()
		d.pages[pgGuide] = PageJudgments{Documentation: 0.1, Makers: 0.1, Kind: "blog_post_or_article"} // tier C at rank 4: not asked
		return d
	}
	free := three()
	decidedBudget(t, 0, free, long)
	budget := spentBy(free, "page", "") + spentBy(free, "others", "") + spentBy(free, "passage", pgBlog) + spentBy(free, "passage", pgMirror) + spentBy(free, "passage", pgOfficial)/3

	d := three()
	o := decidedBudget(t, budget, d, long)
	for _, u := range []string{pgBlog, pgMirror} {
		if got, want := len(passageAsks(d, u)), len(passageAsks(free, u)); got != want {
			t.Errorf("%s: asked %d of its %d passages, want all", u, got, want)
		}
	}
	got, all := len(passageAsks(d, pgOfficial)), len(passageAsks(free, pgOfficial))
	if got == 0 || got >= all {
		t.Errorf("the huge page: asked %d of %d passages", got, all)
	}
	spent := spentBy(d, "", "")
	if spent > budget {
		t.Errorf("spent %d, budget %d", spent, budget)
	}
	// The huge page used its share: what is left would not send one more
	// of its passages (each costs well over 100 tokens).
	if budget-spent > 200 {
		t.Errorf("left %d tokens unspent", budget-spent)
	}
	if d.count("passage") != 3 || o.Decisions.Budget == nil {
		t.Errorf("%d passage requests, budget %+v", d.count("passage"), o.Decisions.Budget)
	}
}

// (c) Nothing over the budget: the answer is the uncapped one, byte for
// byte (no budget in the decisions, no clause), whether everything fits
// at once or the pages after the first three had to be labelled first.
func TestDecidedBudgetUncutIsUnchanged(t *testing.T) {
	free := decidedJudgments()
	want := renderAll(widgetQuestion, decidedBudget(t, 0, free, nil))
	wantJSON, _ := json.Marshal(NewAnswerJSON(widgetQuestion, SearchBackend, "", decidedBudget(t, 0, decidedJudgments(), nil)))
	for name, tokens := range map[string]int{"default": DefaultDecisionsTokens, "exact": spentBy(free, "", "")} {
		t.Run(name, func(t *testing.T) {
			d := decidedJudgments()
			o := decidedBudget(t, tokens, d, nil)
			if got := renderAll(widgetQuestion, o); got != want {
				t.Errorf("a budget that cut nothing changed the answer:\n%s\n---\n%s", got, want)
			}
			if js, _ := json.Marshal(NewAnswerJSON(widgetQuestion, SearchBackend, "", o)); string(js) != string(wantJSON) {
				t.Errorf("JSON changed:\n%s\n---\n%s", js, wantJSON)
			}
			if len(d.reqs) != len(free.reqs) || o.Decisions.Budget != nil {
				t.Errorf("%d requests (uncapped %d), budget %+v", len(d.reqs), len(free.reqs), o.Decisions.Budget)
			}
		})
	}
	// Labelled first: the article at rank 4 is not asked, so the passage
	// passes of every page would not all fit, but those asked do.
	art := func() *fakeDecider {
		d := decidedJudgments()
		d.pages[pgGuide] = PageJudgments{Documentation: 0.1, Makers: 0.1, Kind: "blog_post_or_article"}
		return d
	}
	free = art()
	want = renderAll(widgetQuestion, decidedBudget(t, 0, free, nil))
	d := art()
	o := decidedBudget(t, spentBy(free, "", ""), d, nil)
	if got := renderAll(widgetQuestion, o); got != want || o.Decisions.Budget != nil {
		t.Errorf("labelled first, nothing cut, the answer changed:\n%s\n---\n%s", got, want)
	}
}

// (d) Unlimited sends every passage, however long the page.
func TestDecidedBudgetUnlimited(t *testing.T) {
	if n, err := ParseDecisionsTokens(DecisionsUnlimited); err != nil || n != 0 {
		t.Fatalf("ParseDecisionsTokens(unlimited) = %d, %v", n, err)
	}
	if n, err := ParseDecisionsTokens(""); err != nil || n != DefaultDecisionsTokens {
		t.Fatalf("ParseDecisionsTokens(\"\") = %d, %v", n, err)
	}
	for _, bad := range []string{"0", "-5", "lots"} {
		if _, err := ParseDecisionsTokens(bad); err == nil || !strings.Contains(err.Error(), "positive number") || !strings.Contains(err.Error(), "unlimited") {
			t.Errorf("ParseDecisionsTokens(%q) = %v", bad, err)
		}
	}
	d := decidedJudgments()
	o := decidedBudget(t, 0, d, func(f *fakeFetcher) { f.pages[pgOfficial] = longTimeouts(400) })
	if spentBy(d, "", "") <= DefaultDecisionsTokens {
		t.Fatalf("the page is not over the default budget: %d tokens", spentBy(d, "", ""))
	}
	if j := o.Read[0].Judgments; o.Read[0].URL != pgOfficial || j == nil || j.Passages != len(passageAsks(d, pgOfficial)) || j.Passages < 400 {
		t.Errorf("read %+v: want every passage of the official page judged", o.Read[0])
	}
	if o.Decisions.Budget != nil || strings.Contains(o.Note, "budget") {
		t.Errorf("budget %+v, note %q", o.Decisions.Budget, o.Note)
	}
}

// followRun is the article site's call within tokens (0: unlimited).
func followRun(t *testing.T, tokens int, d *fakeDecider) (Outcome, *fakeFetcher) {
	t.Helper()
	f, hits := articleSite(pgLinkDocs, pgLinkDocs)
	s := decidedOver(t, f, hits, d)
	s.decisionsTokens = tokens
	return s.SearchSelect(context.Background(), widgetQuestion, 0), f
}

// firstAsk is the index of d's first request of kind about url ("" any
// page); -1 for none.
func firstAsk(d *fakeDecider, kind, url string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, r := range d.reqs {
		switch in := r.Input.(type) {
		case pageInput:
			if kind == "page" && (url == "" || in.Page.URL == url) {
				return i
			}
		case passageInput:
			if kind == "passage" && (url == "" || in.Page.URL == url) {
				return i
			}
		}
	}
	return -1
}

// articleAsks is how many passage questions d was asked about the
// articles.
func articleAsks(d *fakeDecider) int {
	n := 0
	for _, u := range []string{pgArt1, pgArt2, pgArt3, pgArt4, pgArt5} {
		n += len(passageAsks(d, u))
	}
	return n
}

// (e) No page read is documentation and a linked site qualifies: under a
// cut, the makers' docs come before the articles' passages. Accepted, the
// site answers and the articles are not asked about; nothing left to label
// the site with, it is not read; rejected, the articles are judged from
// what is left; with room for everything, the call is the uncapped one.
func TestDecidedBudgetFollowFirst(t *testing.T) {
	free := articleJudgments()
	unlimited, _ := followRun(t, 0, free)
	follow := spentBy(free, "", pgLinkDocs)
	main := spentBy(free, "", "") - follow
	if follow == 0 || len(unlimited.Read) == 0 || unlimited.Read[0].URL != pgLinkDocs {
		t.Fatalf("the uncapped call did not follow: %+v", unlimited.Read)
	}

	t.Run("accepted", func(t *testing.T) {
		d := articleJudgments()
		o, _ := followRun(t, main, d)
		if len(o.Read) == 0 || o.Read[0].URL != pgLinkDocs || o.Read[0].Context != confHigh {
			t.Fatalf("read %+v: want the makers' docs", o.Read)
		}
		if n := articleAsks(d); n != 0 || spentBy(d, "", "") > main {
			t.Errorf("asked %d passage questions about the articles; spent %d of %d", n, spentBy(d, "", ""), main)
		}
		b := o.Decisions.Budget
		if b == nil || b.JudgedPassages >= b.Passages || !strings.Contains(o.Note, fmt.Sprintf("decisions judged %d of %d passages (budget %d tokens", b.JudgedPassages, b.Passages, main)) {
			t.Errorf("budget %+v, note %q", b, o.Note)
		}
		if len(o.PassedOver) != 5 || o.PassedOver[0].Reason != passedWeaker {
			t.Errorf("passed_over %+v: want the five articles, weaker sources", o.PassedOver)
		}
	})

	t.Run("not read", func(t *testing.T) {
		d := articleJudgments()
		tokens := spentBy(free, "page", "") - spentBy(free, "page", pgLinkDocs) + spentBy(free, "others", "") + 100
		o, f := followRun(t, tokens, d)
		if !strings.Contains(o.Note, "; did not read "+linkedRootURL+": the decisions budget was spent; ") {
			t.Errorf("note %q", o.Note)
		}
		if fetchedHost(f, "docs.widgetco.test") || spentBy(d, "", pgLinkDocs) != 0 || spentBy(d, "", "") > tokens {
			t.Errorf("read the site (%v), asked %d tokens about it, spent %d of %d", fetchedHost(f, "docs.widgetco.test"), spentBy(d, "", pgLinkDocs), spentBy(d, "", ""), tokens)
		}
	})

	t.Run("rejected", func(t *testing.T) {
		notDocs := func() *fakeDecider {
			d := articleJudgments()
			d.pages[pgLinkDocs] = PageJudgments{Documentation: 0.1, Makers: 0.1, Kind: "other"}
			return d
		}
		freeRej := notDocs()
		followRun(t, 0, freeRej)
		d := notDocs()
		tokens := spentBy(freeRej, "", "") // the articles' passages and the site's labels: room for no more
		o, _ := followRun(t, tokens, d)
		want := todayNote + "; tried " + linkedRootURL + ", which 2 of the 5 pages read link to: labelled other, not the makers' docs"
		if o.Note != want {
			t.Errorf("note %q\nwant %q", o.Note, want)
		}
		if site, art := firstAsk(d, "page", pgLinkDocs), firstAsk(d, "passage", ""); site < 0 || art < 0 || site > art {
			t.Errorf("the site was labelled at request %d, the articles' passages asked at %d: want the site first", site, art)
		}
		if got, all := articleAsks(d), articleAsks(freeRej); got != all || all == 0 {
			t.Errorf("asked %d passage questions about the articles, uncapped %d", got, all)
		}
		if o.Decisions.Budget != nil || renderAll(widgetQuestion, o) != renderAll(widgetQuestion, func() Outcome { o, _ := followRun(t, 0, notDocs()); return o }()) {
			t.Errorf("a call that cut nothing changed: budget %+v", o.Decisions.Budget)
		}
	})

	t.Run("room for all", func(t *testing.T) {
		d := articleJudgments()
		o, _ := followRun(t, spentBy(free, "", ""), d)
		if renderAll(widgetQuestion, o) != renderAll(widgetQuestion, unlimited) || len(d.reqs) != len(free.reqs) {
			t.Errorf("room for everything changed the call:\n%s\n---\n%s", renderAll(widgetQuestion, o), renderAll(widgetQuestion, unlimited))
		}
	})
}

// A budget too small for one page's labels answers without decisions,
// and says why.
func TestDecidedBudgetTooSmall(t *testing.T) {
	d := decidedJudgments()
	o := decidedBudget(t, 50, d, nil)
	if o.Decisions != nil || d.count("page") != 0 || d.count("passage") != 0 {
		t.Fatalf("decisions %+v, %d page and %d passage requests", o.Decisions, d.count("page"), d.count("passage"))
	}
	if !strings.Contains(o.Note, "decisions unavailable: "+errBudget.Error()+" (50 tokens); answered without them") {
		t.Errorf("note %q", o.Note)
	}
}

// A page pass that does not fit leaves it and every page after it
// unlabelled, passed over and saying why.
func TestDecidedBudgetUnlabelledPages(t *testing.T) {
	free := decidedJudgments()
	decidedBudget(t, 0, free, nil)
	pages := spentBy(free, "page", "")
	budget := spentBy(free, "others", "") + pages - spentBy(free, "page", pgGuide) - spentBy(free, "page", pgMirror) + 10
	d := decidedJudgments()
	o := decidedBudget(t, budget, d, nil)
	if d.count("page") != 2 || spentBy(d, "", "") > budget {
		t.Fatalf("%d page requests, spent %d of %d", d.count("page"), spentBy(d, "", ""), budget)
	}
	out := renderAll(widgetQuestion, o)
	for _, want := range []string{
		"  - " + pgMirror + " (unlabelled, search rank 3): not labelled: the decisions budget was spent\n",
		"  - " + pgGuide + " (unlabelled, search rank 4): not labelled: the decisions budget was spent\n",
		"decisions labelled 2 of 4 pages read",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
