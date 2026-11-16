package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/decide"
	"github.com/1broseidon/ketch/search"
)

var testCut = cutoffs(decide.DefaultCutoffs)

// Labels and tiers as the prototype's v5.1 config sets them.
func TestDecidedLabelsAndTiers(t *testing.T) {
	cases := []struct {
		doc, makers float64
		kind        string
		named       bool
		other       float64
		label, tier string
	}{
		{0.9, 0.8, "reference_documentation", false, 0, "official docs", "A"},
		{0.7, 0.79, "guide_or_tutorial", false, 0, "docs", "B"},
		{0.7, 0.3, "reference_documentation", false, 0, "third-party docs", "B"},
		{0.69, 0.9, "blog_post_or_article", false, 0, "official article", "C"},
		{0.2, 0.95, "other", false, 0, "official page", "C"},
		{0.2, 0.5, "other", false, 0, "other", "C"},
		{0.2, 0.1, "forum_or_qa_thread", false, 0, "forum thread", "C"},
		{0.9, 0.9, "reference_documentation", true, 0.7, "official docs, other release", "B"},
		{0.9, 0.5, "reference_documentation", true, 0.9, "docs, other release", "C"},
		{0.9, 0.9, "reference_documentation", true, 0.69, "official docs", "A"},
		{0.9, 0.9, "reference_documentation", false, 0.99, "official docs", "A"},
	}
	for _, c := range cases {
		tier, label := testCut.tier(testCut.sourceLabel(c.doc, c.makers, c.kind), c.named, c.other)
		if label != c.label || tier != c.tier {
			t.Errorf("%+v: %q tier %s, want %q tier %s", c, label, tier, c.label, c.tier)
		}
	}
	if got := testCut.otherLabel(0.85, "guide_or_tutorial"); got != "official docs" {
		t.Errorf("other result label %q", got)
	}
	if got := testCut.otherLabel(0.85, "blog_post_or_article"); got != "official article" {
		t.Errorf("other result label %q", got)
	}
	for probs, want := range map[string]string{"0.7": confHigh, "0.31 0.1": confMedium, "0.3 0.2": confLow, "": confLow} {
		var ps []float64
		for _, f := range strings.Fields(probs) {
			ps = append(ps, map[string]float64{"0.7": 0.7, "0.31": 0.31, "0.1": 0.1, "0.3": 0.3, "0.2": 0.2}[f])
		}
		if got := testCut.context(ps); got != want {
			t.Errorf("context of %q = %s, want %s", probs, got, want)
		}
	}
	if lower(confHigh, confMedium) != confMedium || lower(tierBand[tierA], testCut.band(0.5)) != confMedium {
		t.Error("confidence is the lower of tier and passage")
	}
}

func choosePage(rank int, tier, context string, makers float64) *dPage {
	u, _ := url.Parse("https://p.test/" + string(rune('a'+rank)))
	return &dPage{hit: sfHit{u: u, rank: rank}, tier: tier, context: context, judged: &PageJudgments{Makers: makers}}
}

// The primary page: context high before medium; within it the best-ranked
// tier A page, unless it ranks below the top three and a tier B page
// ranks above it; tier C only without A or B, the makers' first.
func TestDecidedChoose(t *testing.T) {
	c := &dCall{cut: testCut}
	cases := []struct {
		name  string
		pages []*dPage
		want  int // rank; 0 for none
	}{
		{"tier A in the top three wins over a better-ranked tier B", []*dPage{choosePage(1, tierB, confHigh, 0.1), choosePage(3, tierA, confHigh, 0.9)}, 3},
		{"tier A below the top three loses to a better-ranked tier B", []*dPage{choosePage(2, tierB, confHigh, 0.1), choosePage(4, tierA, confHigh, 0.9)}, 2},
		{"tier A below the top three wins without a tier B above it", []*dPage{choosePage(4, tierA, confHigh, 0.9), choosePage(5, tierB, confHigh, 0.1)}, 4},
		{"context high before a tier A page of context medium", []*dPage{choosePage(1, tierA, confMedium, 0.9), choosePage(2, tierC, confHigh, 0.1)}, 2},
		{"tier C: the makers' page first", []*dPage{choosePage(1, tierC, confHigh, 0.1), choosePage(2, tierC, confHigh, 0.85)}, 2},
		{"tier B before tier C", []*dPage{choosePage(1, tierC, confHigh, 0.9), choosePage(5, tierB, confHigh, 0.2)}, 5},
		{"no page with context high or medium", []*dPage{choosePage(1, tierA, confLow, 0.9)}, 0},
	}
	for _, tc := range cases {
		got := c.choose(tc.pages)
		switch {
		case tc.want == 0 && got != nil:
			t.Errorf("%s: chose rank %d, want none", tc.name, got.hit.rank)
		case tc.want != 0 && (got == nil || got.hit.rank != tc.want):
			t.Errorf("%s: chose %+v, want rank %d", tc.name, got, tc.want)
		}
	}
}

// passed_over's reasons, in the prototype's order.
func TestDecidedPassedReasons(t *testing.T) {
	primary := choosePage(1, tierA, confHigh, 0.9)
	primary.page = &webPage{URL: "https://docs.test/a/page.html"}
	read := func(p *dPage, u string) *dPage { p.page = &webPage{URL: u}; return p }
	cases := []struct {
		p    *dPage
		want string
	}{
		{&dPage{cause: "not found (HTTP 404)"}, "not found (HTTP 404)"},
		{read(&dPage{stub: true}, "https://x.test/s"), passedStub},
		{read(choosePage(2, tierA, confHigh, 0.9), "https://www.docs.test/en-us/a/page"), passedSame},
		{read(&dPage{otherRel: true, context: confHigh}, "https://x.test/r"), passedRelease},
		{read(choosePage(3, tierA, confLow, 0.9), "https://x.test/l"), passedNothing},
		{read(choosePage(3, tierC, confHigh, 0.1), "https://x.test/h"), passedAlso},
		{read(choosePage(3, tierB, confMedium, 0.1), "https://x.test/m"), passedWeaker},
		{read(choosePage(3, tierB, "", 0.1), "https://x.test/n"), passedWeaker},
		{read(choosePage(3, tierA, confMedium, 0.9), "https://x.test/o"), passedNeedless},
	}
	for i, c := range cases {
		if c.p.hit.u == nil {
			c.p.hit.u, _ = url.Parse("https://x.test/" + string(rune('a'+i)))
		}
		if got := passedReason(c.p, primary); got != c.want {
			t.Errorf("case %d: %q, want %q", i, got, c.want)
		}
	}
}

// next: a better kind of source read and not printed (tier strictly
// better than the page read), and the cut section of the page read on the
// question. A result not read qualifies only when labelled official docs.
func TestDecidedNext(t *testing.T) {
	f, hits := decidedSite()
	// The official page ranks fifth now, below the mirror (tier B, rank 2),
	// so the mirror is read; the official page is the better source next.
	order := []string{pgBlog, pgMirror, pgGuide, pgStub, pgOfficial}
	byURL := map[string]search.Result{}
	for _, h := range hits {
		byURL[h.URL] = h
	}
	var reordered []search.Result
	for _, u := range order {
		reordered = append(reordered, byURL[u])
	}
	reordered = append(reordered, hits[5:]...)
	f.pages[pgMirror] = docsPage("Timeouts (mirror)", "## Read timeout\n\nThe read_timeout setting defaults to 10 seconds, the mirror says. MIRRORFACT.\n\n"+strings.Repeat("More about read timeouts: OFFICIALFACT and other notes on retries. ", 400))
	d := decidedJudgments()
	d.pages[pgGuide] = PageJudgments{Documentation: 0.95, Makers: 0.1, Kind: "guide_or_tutorial"}
	ctx := WithLocalOptions(context.Background(), LocalOptions{Tokens: 600})
	o := decidedOver(t, f, reordered, d).SearchSelect(ctx, widgetQuestion, 0)
	if len(o.Read) == 0 || o.Read[0].URL != pgMirror {
		t.Fatalf("read %+v, want the mirror first", o.Read)
	}
	out := renderAll(widgetQuestion, o)
	wantNext := "next:\n  - ketch docs --library " + pgOfficial + ` "<your question>" (official docs, search rank 5, read, not printed)` + "\n" +
		"  - ketch docs --section " + pgMirror + "#read-timeout (cut, more on the question in this section)\n"
	if !strings.Contains(out, wantNext) {
		t.Errorf("next lines missing:\n%s", out)
	}
	// The other result labelled official docs is not named: the official
	// page read is better ranked within the same tier.
	if strings.Contains(out, "--library "+pgOther1) {
		t.Errorf("next named the result not read over the page read:\n%s", out)
	}
}

// A question naming a release no page read is marked with: the build's own
// release check notes it, and confidence is at most medium.
func TestDecidedReleaseCapsConfidence(t *testing.T) {
	f, hits := decidedSite()
	d := decidedJudgments()
	q := "what is the default read_timeout of Widget 3.2"
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), q, 0)
	if !strings.Contains(o.Note, "the question names Widget 3.2 and no page read is marked 3.2") {
		t.Errorf("note %q lacks the release check", o.Note)
	}
	for _, r := range o.Read {
		if r.Confidence == confHigh {
			t.Errorf("read %s at confidence high under the release note", r.URL)
		}
	}
	for _, r := range o.Results {
		if r.Confidence == confHigh {
			t.Errorf("result %s at confidence high under the release note", r.URL)
		}
	}
}

// --tokens is a hard bound: a top passage larger than the budget prints
// its start, cut, and the docs text stays within the budget.
func TestDecidedBudgetIsHard(t *testing.T) {
	f, hits := decidedSite()
	big := "```\n" + strings.Repeat("read_timeout = 10  # OFFICIALFACT the default, in seconds, for every request\n", 120) + "```"
	f.pages[pgOfficial] = docsPage("Timeouts", "## Read timeout\n\n"+big)
	d := decidedJudgments()
	for _, budget := range []int{200, 800} {
		ctx := WithLocalOptions(context.Background(), LocalOptions{Tokens: budget})
		o := decidedOver(t, f, hits, d).SearchSelect(ctx, widgetQuestion, 0)
		if len(o.Results) == 0 || o.Read[0].URL != pgOfficial {
			t.Fatalf("budget %d: read %+v", budget, o.Read)
		}
		if n := estimateTokens(renderPassages(o.Results)); n > budget {
			t.Errorf("budget %d: the docs text is %d tokens", budget, n)
		}
		if !o.Results[0].Truncated {
			t.Errorf("budget %d: the top passage was not cut", budget)
		}
	}
}

// No page has a passage on the question: the first readable result prints
// as the evidence pipeline prints it, at confidence low, and says so.
func TestDecidedNoPassageFloor(t *testing.T) {
	f, hits := decidedSite()
	d := decidedJudgments()
	d.helps = func(string) float64 { return 0.1 }
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	if len(o.Read) != 1 || o.Read[0].URL != pgBlog || o.Read[0].Confidence != confLow || o.Read[0].Label != "article" {
		t.Fatalf("read %+v, want the first readable result at confidence low", o.Read)
	}
	if !strings.Contains(o.Note, noteNoPassage) || !strings.Contains(o.Note, noteNotOfficial+"an article") {
		t.Errorf("note %q", o.Note)
	}
	for _, r := range o.Results {
		if r.Confidence != confLow {
			t.Errorf("result %s at confidence %s", r.URL, r.Confidence)
		}
	}
}

// Twins: the same page at another host spelling, locale or extension.
func TestDecidedURLKey(t *testing.T) {
	a, _ := url.Parse("https://www.Docs.test/en-us/guide/x.html")
	b, _ := url.Parse("https://docs.test/guide/x")
	c, _ := url.Parse("https://docs.test/guide/y")
	if urlKey(a) != urlKey(b) || urlKey(b) == urlKey(c) {
		t.Errorf("urlKey: %s %s %s", urlKey(a), urlKey(b), urlKey(c))
	}
}

// Passages: consecutive pieces of a section up to passageTokens, a larger
// piece on its own, never across sections.
func TestDecidedPassages(t *testing.T) {
	para := strings.Repeat("word ", 90)
	f, _ := decidedSite()
	f.pages[pgOfficial] = docsPage("Timeouts", "## A\n\n"+para+"\n\n"+para+"\n\n"+para+"\n\n"+para+"\n\n## B\n\nshort")
	cands := []*dPage{{page: &webPage{URL: pgOfficial, Title: "Timeouts", Markdown: f.pages[pgOfficial].markdown}}}
	dl, err := buildDecidedLib(widgetQuestion, cands)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.l.store.Close()
	for _, ps := range cands[0].passages {
		n := 0
		for _, id := range ps.pieces {
			n += dl.s.pieces[id].tokens
			if dl.s.pieces[id].sec != ps.sec {
				t.Errorf("passage crosses sections")
			}
		}
		if n > passageTokens && len(ps.pieces) > 1 {
			t.Errorf("passage of %d tokens in %d pieces", n, len(ps.pieces))
		}
	}
	if len(cands[0].passages) < 3 {
		t.Errorf("%d passages, want the long section split", len(cands[0].passages))
	}
}

// --json carries the raw judgments, tiers, probabilities and token use
// under named fields; text output carries words only.
func TestDecidedJSON(t *testing.T) {
	f, hits := decidedSite()
	o := decidedOver(t, f, hits, decidedJudgments()).SearchSelect(context.Background(), widgetQuestion, 0)
	raw, err := json.Marshal(NewAnswerJSON(widgetQuestion, SearchBackend, "", o))
	if err != nil {
		t.Fatal(err)
	}
	var a decidedJSON
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	for what, ok := range a.checks() {
		if !ok {
			t.Errorf("%s: %s", what, raw)
		}
	}
}

// checks are what each field must hold.
func (a decidedJSON) checks() map[string]bool {
	d := a.Decisions
	out := map[string]bool{
		"decisions":   d.Provider == "fake" && d.Requests > 0 && d.InputTokens == 1000*d.Requests && d.Cutoffs["very_high"] == 0.9,
		"read":        len(a.Read) > 0 && a.Read[0].Tier == tierA && a.Read[0].Judgments["makers"] == 0.95 && a.Read[0].Judgments["passages"] != nil,
		"passed_over": len(a.PassedOver) > 0 && a.PassedOver[0]["reason"] != nil,
	}
	for k, v := range a.bodyChecks() {
		out[k] = v
	}
	return out
}

func (a decidedJSON) bodyChecks() map[string]bool {
	return map[string]bool{
		"results":       len(a.Results) > 0 && a.Results[0].Probability == 0.95 && a.Results[0].Confidence == confHigh && a.Results[0].SourceLabel == labelOfficialDocs,
		"other_results": len(a.OtherResults) > 0 && a.OtherResults[0].Label == labelOfficialDocs && a.OtherResults[0].Judgments["kind"] == "reference_documentation",
	}
}

// decidedJSON is the part of --json a decided answer adds.
type decidedJSON struct {
	Decisions struct {
		Provider    string             `json:"provider"`
		Requests    int                `json:"requests"`
		InputTokens int                `json:"input_tokens"`
		Cutoffs     map[string]float64 `json:"cutoffs"`
	} `json:"decisions"`
	Read []struct {
		Label, Confidence, Tier, Context string
		Judgments                        map[string]any `json:"judgments"`
	} `json:"read"`
	PassedOver []map[string]any `json:"passed_over"`
	Results    []struct {
		Confidence  string  `json:"confidence"`
		SourceLabel string  `json:"source_label"`
		Probability float64 `json:"probability"`
	} `json:"results"`
	OtherResults []struct {
		Label     string         `json:"label"`
		Judgments map[string]any `json:"judgments"`
	} `json:"other_results"`
}

// A source for next: strictly better tier than the page read; a result
// not read only when labelled official docs; a page read only when under
// dupShare of its text is on the page read; never a printed address.
func TestDecidedBetterSource(t *testing.T) {
	mk := func(u string, rank int, tier, label, text string) *dPage {
		p := choosePage(rank, tier, confHigh, 0.5)
		p.hit.u, _ = url.Parse(u)
		p.page, p.label, p.reason = &webPage{URL: u, Markdown: text}, label, passedAlso
		return p
	}
	words := func(seed string) string {
		var b strings.Builder
		for i := range 60 {
			b.WriteString(seed + " word" + string(rune('a'+i%26)) + " ")
		}
		return b.String()
	}
	primary := mk("https://blog.test/a", 1, tierC, "article", words("blog"))
	copyOf := mk("https://docs.test/copy", 2, tierA, labelOfficialDocs, words("blog"))
	rest := []sfHit{{u: parsed("https://docs.test/unread-docs"), rank: 6}, {u: parsed("https://docs.test/unread-official"), rank: 7}}
	others := []otherLabel{{label: labelDocs}, {label: labelOfficialDocs}}
	got := betterSource(primary, []*dPage{primary, copyOf}, rest, others, map[string]bool{})
	if got == nil || got.url != "https://docs.test/unread-official" || got.read != "not read" {
		t.Errorf("got %+v: the copy of the page read is dropped, the unread docs result does not qualify", got)
	}
	fresh := mk("https://docs.test/fresh", 3, tierB, labelDocs, words("fresh"))
	if got := betterSource(primary, []*dPage{primary, fresh}, rest, others, map[string]bool{}); got == nil || got.url != "https://docs.test/unread-official" {
		t.Errorf("got %+v: tier A not read before tier B read", got)
	}
	printed := map[string]bool{urlKey(parsed("https://docs.test/unread-official")): true}
	if got := betterSource(primary, []*dPage{primary, fresh}, rest, others, printed); got == nil || got.url != "https://docs.test/fresh" {
		t.Errorf("got %+v: a printed address is never suggested", got)
	}
	if got := betterSource(fresh, []*dPage{primary, fresh}, nil, nil, map[string]bool{}); got != nil {
		t.Errorf("got %+v: nothing better than tier B among tier C", got)
	}
}

func parsed(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}

// A long block is cut into runs of at most passageTokens lines, each
// ending at a blank line in its second half when there is one, so an
// entry stays in one run; the fences are left out of the runs and put
// back around each printed run.
func TestDecidedPieceRuns(t *testing.T) {
	var b strings.Builder
	b.WriteString("```\n")
	for i := range 200 {
		fmt.Fprintf(&b, "'opt%d'\tnumber\t(default %d)\n\tWhat option %d of the widget sets, in a line or two.\n\tA second line about it.\n\n", i, i, i)
	}
	b.WriteString("```")
	text := b.String()
	runs := pieceRuns(piece{text: text, tokens: estimateTokens(text)})
	lines := strings.Split(text, "\n")
	if len(runs) < 5 || runs[0].from != 1 || runs[len(runs)-1].to != len(lines)-1 {
		t.Fatalf("runs %v over %d lines", runs, len(lines))
	}
	for i, r := range runs {
		if why := runProblem(lines, runs, i); why != "" {
			t.Errorf("run %d %v: %s", i, r, why)
		}
	}
	got := runsText(text, []lineRun{runs[2], runs[0]})
	if !strings.HasPrefix(got, "```\n'opt0'") || !strings.Contains(got, "```\n\n…\n\n```\n") || !strings.HasSuffix(got, "```") {
		t.Errorf("runs printed: %.300q", got)
	}
	if pieceRuns(piece{text: "short", tokens: 5}) != nil {
		t.Error("a short piece is asked about whole")
	}
}

// runProblem says what is wrong with runs[i] over lines: too large, ending
// inside an entry, or leaving a gap after the one before.
func runProblem(lines []string, runs []lineRun, i int) string {
	r, n := runs[i], 0
	for _, l := range lines[r.from:r.to] {
		n += estimateTokens(l) + 1
	}
	switch {
	case n > passageTokens:
		return fmt.Sprintf("%d tokens", n)
	case i < len(runs)-1 && strings.TrimSpace(lines[r.to-1]) != "":
		return "ends inside an entry: " + lines[r.to-1]
	case i > 0 && r.from != runs[i-1].to:
		return "leaves a gap"
	}
	return ""
}
