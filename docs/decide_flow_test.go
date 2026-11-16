package docs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/1broseidon/ketch/decide"
	"github.com/1broseidon/ketch/search"
)

// A fake decision provider: page judgments by page address, a passage's
// probability by its text, labels of results not read by address.
type fakeDecider struct {
	pages  map[string]PageJudgments
	helps  func(text string) float64
	others map[string]ResultJudgments
	err    error
	// failOn fails only requests whose input is of this kind ("page",
	// "passage", "others").
	failOn string

	mu   sync.Mutex
	reqs []decide.Request
}

func (f *fakeDecider) Decide(_ context.Context, req decide.Request) (decide.Response, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	resp := decide.Response{Answers: map[string]decide.Answer{}, InputTokens: 1000, Requests: 1}
	kind := ""
	switch in := req.Input.(type) {
	case pageInput:
		kind = "page"
		j := f.pages[in.Page.URL]
		resp.Answers[qDocumentation] = decide.Answer{Probability: j.Documentation}
		resp.Answers[qMakers] = decide.Answer{Probability: j.Makers}
		resp.Answers[qNamesRelease] = decide.Answer{Probability: j.NamesRelease}
		resp.Answers[qOtherRelease] = decide.Answer{Probability: j.OtherRelease}
		resp.Answers[qKind] = decide.Answer{Choice: firstNonEmpty(j.Kind, "other"), Probabilities: map[string]float64{firstNonEmpty(j.Kind, "other"): 0.9}}
	case passageInput:
		kind = "passage"
		for _, q := range req.Questions {
			resp.Answers[q.Name] = decide.Answer{Probability: f.helps(q.Item.Value.(passageItem).Text)}
		}
	case docsQuestionInput:
		kind = "others"
		for _, q := range req.Questions {
			r := f.others[q.Item.Value.(searchResultItem).URL]
			if q.Kind == decide.YesNo {
				resp.Answers[q.Name] = decide.Answer{Probability: r.Makers}
			} else {
				k := firstNonEmpty(r.Kind, "other")
				resp.Answers[q.Name] = decide.Answer{Choice: k, Probabilities: map[string]float64{k: 1}}
			}
		}
	}
	if f.err != nil && (f.failOn == "" || f.failOn == kind) {
		return decide.Response{}, f.err
	}
	return resp, nil
}

func (f *fakeDecider) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		switch r.Input.(type) {
		case pageInput:
			n += boolInt(kind == "page")
		case passageInput:
			n += boolInt(kind == "passage")
		case docsQuestionInput:
			n += boolInt(kind == "others")
		}
	}
	return n
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// docsPage is a page of filler with the sections given, long enough not
// to be a stub.
func docsPage(title string, sections ...string) fakePage {
	var b strings.Builder
	key := strings.ToLower(strings.Join(strings.FieldsFunc(title, func(r rune) bool { return r == ' ' || r == '(' || r == ')' }), ""))
	b.WriteString("# " + title + "\n\n")
	for i := range 40 {
		fmt.Fprintf(&b, "The %s widget note %d. ", key, i)
	}
	b.WriteString("\n")
	for _, s := range sections {
		b.WriteString("\n" + s + "\n")
	}
	return fakePage{title: title, markdown: b.String()}
}

const (
	pgOfficial = "https://widget.test/docs/timeouts"
	pgBlog     = "https://blog.test/posts/widget-timeouts"
	pgMirror   = "https://mirror.test/widget/timeouts"
	pgGuide    = "https://widget.test/docs/guide"
	pgStub     = "https://tiny.test/widget"
	pgOther1   = "https://widget.test/docs/retries"
	pgOther2   = "https://forum.test/t/widget-timeouts/12"
)

// decidedSite serves five readable results and two not read.
func decidedSite() (*fakeFetcher, []search.Result) {
	f := &fakeFetcher{pages: map[string]fakePage{
		pgBlog:     docsPage("Widget timeouts explained", "## My setup\n\nI set the read timeout to 30 seconds in my app, BLOGFACT."),
		pgOfficial: docsPage("Timeouts", "## Read timeout\n\nThe read_timeout setting defaults to 10 seconds. OFFICIALFACT.", "## Unrelated\n\nColours and themes."),
		pgMirror:   docsPage("Timeouts (mirror)", "## Read timeout\n\nThe read_timeout setting defaults to 10 seconds. MIRRORFACT."),
		pgGuide:    docsPage("Guide", "## Retry on timeout\n\nA request that times out is retried twice. GUIDEFACT."),
		pgStub:     {title: "Widget", markdown: "# Widget\n\nThe read_timeout setting, see the docs for the widget framework."},
	}}
	hits := []search.Result{
		hit(pgBlog, "Widget timeouts explained"), hit(pgOfficial, "Timeouts"), hit(pgMirror, "Timeouts (mirror)"),
		hit(pgGuide, "Guide"), hit(pgStub, "Widget"),
		{URL: pgOther1, Title: "Retries", Description: "How the widget retries requests."},
		{URL: pgOther2, Title: "Timeouts thread", Description: "A thread about widget timeouts."},
	}
	return f, hits
}

// decidedJudgments label the official page official docs, the guide the
// makers' docs, the mirror third-party docs and the blog an article.
func decidedJudgments() *fakeDecider {
	return &fakeDecider{
		pages: map[string]PageJudgments{
			pgOfficial: {Documentation: 0.95, Makers: 0.95, Kind: "reference_documentation"},
			pgGuide:    {Documentation: 0.9, Makers: 0.9, Kind: "guide_or_tutorial"},
			pgMirror:   {Documentation: 0.9, Makers: 0.1, Kind: "reference_documentation"},
			pgBlog:     {Documentation: 0.2, Makers: 0.1, Kind: "blog_post_or_article"},
		},
		helps: func(text string) float64 {
			switch {
			case strings.Contains(text, "OFFICIALFACT"), strings.Contains(text, "MIRRORFACT"), strings.Contains(text, "REFFACT"):
				return 0.95
			case strings.Contains(text, "BLOGFACT"), strings.Contains(text, "GUIDEFACT"):
				return 0.8
			}
			return 0.05
		},
		others: map[string]ResultJudgments{pgOther1: {Makers: 0.9, Kind: "reference_documentation"}, pgOther2: {Makers: 0.1, Kind: "forum_or_qa_thread"}},
	}
}

// decidedOver is a search-first backend over f and hits that asks d.
func decidedOver(t *testing.T, f *fakeFetcher, hits []search.Result, d decide.Decider) *SearchFirst {
	t.Helper()
	s, _, _ := searchFirstOver(t, f, hits)
	prov := decide.Provider{ID: "fake", Model: "fake-1", Cutoffs: decide.DefaultCutoffs}
	s.decisions = func(setting string) (decide.Provider, decide.Decider, bool, error) {
		if setting == decide.Off || d == nil {
			return decide.Provider{}, nil, false, nil
		}
		return prov, d, true, nil
	}
	return s
}

func renderAll(query string, o Outcome) string {
	return NewFrontmatter(query, SearchBackend, "", o).Render() + RenderAnswer(o, HintCLI)
}

const widgetQuestion = "what is the default read_timeout of the widget framework"

// The primary page is the official docs page (tier A, context high) though
// the search ranks an article first; the makers' guide adds its strong
// passage, the third-party mirror its very strong one (tier B under tier
// A); the article, on the question too, is passed over, and so is the
// stub, almost empty; every read line, heading and source line carries
// words, no probability.
func TestDecidedPicksOfficialDocs(t *testing.T) {
	f, hits := decidedSite()
	d := decidedJudgments()
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	if o.Err() != nil || o.Decisions == nil {
		t.Fatalf("outcome %v, decisions %+v", o.Err(), o.Decisions)
	}
	if len(o.Read) == 0 || o.Read[0].URL != pgOfficial || o.Read[0].Label != labelOfficialDocs || o.Read[0].Confidence != confHigh {
		t.Fatalf("read %+v, want the official page first, official docs, confidence high", o.Read)
	}
	out := renderAll(widgetQuestion, o)
	for _, want := range []string{
		"decisions: fake\n",
		"  - " + pgOfficial + " (official docs, confidence high, search rank 2)\n",
		"  - " + pgGuide + " (official docs, confidence high, search rank 4)\n",
		"  - " + pgMirror + " (third-party docs, confidence medium, search rank 3)\n",
		"  - " + pgBlog + " (article, search rank 1): also on the question, not printed\n",
		"  - " + pgStub + " (search rank 5): almost empty\n",
		pgOfficial + " (official docs)\n",
		"source: " + pgOfficial + "#read-timeout (confidence high)\n",
		"OFFICIALFACT",
		"## Other results\n- Retries — " + pgOther1 + " (official docs) — ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "MIRRORFACT") && strings.Contains(out, "0.95") {
		t.Errorf("output prints a probability:\n%s", out)
	}
	if d.count("page") != 4 || d.count("others") != 1 {
		t.Errorf("page requests %d (want 4: the stub is not asked), other-results requests %d", d.count("page"), d.count("others"))
	}
}

// A strong passage of a page of the same tier or better is added after the
// primary's; one whose text is printed already is skipped; a tier C page
// adds nothing under tier A.
func TestDecidedExtrasAndRepeats(t *testing.T) {
	f, hits := decidedSite()
	f.pages[pgMirror] = docsPage("Timeouts (copy)", "## Read timeout\n\nThe read_timeout setting defaults to 10 seconds. OFFICIALFACT.")
	d := decidedJudgments()
	d.pages[pgMirror] = PageJudgments{Documentation: 0.95, Makers: 0.95, Kind: "reference_documentation"}
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	out := renderAll(widgetQuestion, o)
	if !strings.Contains(out, "GUIDEFACT") {
		t.Errorf("the guide's strong passage (tier A, 0.8) was not added:\n%s", out)
	}
	if strings.Count(out, "OFFICIALFACT") != 1 {
		t.Errorf("a repeated passage was printed twice:\n%s", out)
	}
	if strings.Contains(out, "BLOGFACT") {
		t.Errorf("a tier C page added a passage under tier A:\n%s", out)
	}
}

// Any provider error answers the whole call as without decisions, with
// one note saying why; the exit status is the same.
func TestDecidedFallsBackOnError(t *testing.T) {
	for _, kind := range []string{"page", "passage", "others"} {
		t.Run(kind, func(t *testing.T) {
			f, hits := decidedSite()
			d := decidedJudgments()
			d.err, d.failOn = errors.New("fake: HTTP 529: overloaded"), kind
			o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
			head := decidedOver(t, f, hits, nil).SearchSelect(context.Background(), widgetQuestion, 0)
			if o.Decisions != nil || o.Err() != nil {
				t.Fatalf("decisions %+v, err %v: want the answer without them", o.Decisions, o.Err())
			}
			want := "decisions unavailable: fake: HTTP 529: overloaded; answered without them"
			if o.Note != joinNote(head.Note, want) {
				t.Errorf("note %q, want %q", o.Note, joinNote(head.Note, want))
			}
			o.Note = head.Note
			if renderAll(widgetQuestion, o) != renderAll(widgetQuestion, head) {
				t.Errorf("fallback differs from the answer without decisions:\n%s\n---\n%s", renderAll(widgetQuestion, o), renderAll(widgetQuestion, head))
			}
		})
	}
}

// With no provider (none configured, or off), the answer is today's, byte
// for byte, and no request is made.
func TestDecidedOffIsUnchanged(t *testing.T) {
	f, hits := decidedSite()
	head, _, _ := searchFirstOver(t, f, hits)
	want := renderAll(widgetQuestion, head.SearchSelect(context.Background(), widgetQuestion, 0))
	none := decidedOver(t, f, hits, nil).SearchSelect(context.Background(), widgetQuestion, 0)
	if got := renderAll(widgetQuestion, none); got != want {
		t.Errorf("no provider changed the answer:\n%s\n---\n%s", got, want)
	}
	d := decidedJudgments()
	ctx := WithLocalOptions(context.Background(), LocalOptions{Decisions: decide.Off})
	off := decidedOver(t, f, hits, d).SearchSelect(ctx, widgetQuestion, 0)
	if got := renderAll(widgetQuestion, off); got != want || len(d.reqs) != 0 {
		t.Errorf("off asked %d requests or changed the answer:\n%s", len(d.reqs), got)
	}
}

// A provider the setting names that is not usable answers without
// decisions, and the note says what to configure.
func TestDecidedUnusableProviderNote(t *testing.T) {
	f, hits := decidedSite()
	s, _, _ := searchFirstOver(t, f, hits)
	s.decisions = func(string) (decide.Provider, decide.Decider, bool, error) {
		return decide.Provider{}, nil, false, fmt.Errorf("%w: fake: API key not set (ketch config set fake_api_key <key>)", decide.ErrUnavailable)
	}
	o := s.SearchSelect(context.Background(), widgetQuestion, 0)
	if !strings.Contains(o.Note, "decisions unavailable: fake: API key not set (ketch config set fake_api_key <key>); answered without them") {
		t.Errorf("note %q", o.Note)
	}
}

// A request that names a library is never decided.
func TestDecidedOnlyBareQuestions(t *testing.T) {
	f, hits := decidedSite()
	d := decidedJudgments()
	s := decidedOver(t, f, hits, d)
	o := s.GetDocsSelect(context.Background(), "https://widget.test/docs/", widgetQuestion, 0)
	if o.Decisions != nil || len(d.reqs) != 0 {
		t.Errorf("a request with a library asked the provider (%d requests)", len(d.reqs))
	}
}

// longReference is a help file served as one preformatted block: one
// section, one piece of tens of thousands of tokens, each option a few
// lines of it (neo.vimhelp.org/options.txt.html).
func longReference() string {
	var b strings.Builder
	b.WriteString("```\noptions.txt\n\n\t\tWIDGET REFERENCE MANUAL\n\nOptions\t\t\t\toptions\n\n")
	for i := range 1500 {
		fmt.Fprintf(&b, "'opt%d'\t\tnumber\t(default %d)\n\t\t\tglobal\n\tSets how option %d of the widget behaves in its pane.\n\n", i, i*3, i)
		if i == 1234 {
			b.WriteString("'read_timeout' 'rt'\tnumber\t(default 10)\n\t\t\tglobal\n\tSeconds to wait for a read. REFFACT.\n\n")
		}
	}
	b.WriteString("```\n")
	return b.String()
}

// A long reference that is one block is asked about by runs of its lines:
// the run stating the answer, deep in the page, is found and printed, and
// the reference wins over an article though the search ranks it lower.
func TestDecidedLongReferenceBlock(t *testing.T) {
	f, hits := decidedSite()
	f.pages[pgOfficial] = fakePage{title: "Widget: options.txt", markdown: longReference()}
	d := decidedJudgments()
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	if len(o.Read) == 0 || o.Read[0].URL != pgOfficial || o.Read[0].Context != confHigh {
		t.Fatalf("read %+v, want the reference first, context high", o.Read)
	}
	if j := o.Read[0].Judgments; j == nil || j.Passages < 100 {
		t.Errorf("judgments %+v: the block was not cut into runs of lines", j)
	}
	out := renderAll(widgetQuestion, o)
	if !strings.Contains(out, "'read_timeout' 'rt'\tnumber\t(default 10)") || !strings.Contains(out, "REFFACT") {
		t.Errorf("the run stating the answer is not printed:\n%.3000s", out)
	}
	if n := estimateTokens(renderPassages(o.Results)); n > localTokens {
		t.Errorf("docs text %d tokens", n)
	}
	if !o.Results[0].Truncated || !strings.HasPrefix(o.Results[0].Snippet, "```") {
		t.Errorf("the run is not printed fenced and cut: %.200q", o.Results[0].Snippet)
	}
}

// A documentation page whose passages could not be scored (its text did
// not fit the questions) is never "nothing on the question", and never
// loses to an article: it prints by matching the question's words, and
// says why.
func TestDecidedUnscoredPage(t *testing.T) {
	f, hits := decidedSite()
	f.pages[pgOfficial] = fakePage{title: "Timeouts", markdown: "```\n" + strings.Repeat("read_timeout=10 OFFICIALFACT ", 2000) + "\n```\n"}
	d := decidedJudgments()
	d.helps = func(text string) float64 {
		if strings.Contains(text, "BLOGFACT") {
			return 0.8
		}
		return 0.05
	}
	delete(d.pages, pgGuide)
	delete(d.pages, pgMirror)
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	if len(o.Read) == 0 || o.Read[0].URL != pgOfficial {
		t.Fatalf("read %+v, want the unscored official page, not the article", o.Read)
	}
	if !strings.Contains(o.Note, "the passages of "+pgOfficial+" could not be scored") {
		t.Errorf("note %q", o.Note)
	}
	for _, p := range o.PassedOver {
		if p.URL == pgOfficial {
			t.Errorf("the page read is passed over: %+v", p)
		}
	}
}
