package main

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/1broseidon/ketch/docs"
)

// trace is the part of ketch docs --trace's JSON (docs.Trace) the grading
// reads.
type trace struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
	Path   string `json:"path"`
	Terms  *struct {
		Sections int `json:"sections"`
		List     []struct {
			Term     string `json:"term"`
			Sections int    `json:"sections"`
		} `json:"list"`
	} `json:"terms"`
	Gather *struct {
		Pool []struct {
			Page   string `json:"page"`
			Anchor string `json:"anchor"`
			Via    string `json:"via"`
			Text   string `json:"text"`
		} `json:"pool"`
	} `json:"gather"`
	Sort *struct {
		Pieces []struct {
			ID         int    `json:"id"`
			Section    string `json:"section"`
			Kind       string `json:"kind"`
			Role       string `json:"role"`
			Text       string `json:"text"`
			Tokens     int    `json:"tokens"`
			FoldedInto *int   `json:"folded_into"`
		} `json:"pieces"`
	} `json:"sort"`
	Rank *struct {
		Stop *struct {
			Reason string `json:"reason"`
		} `json:"stop"`
		Sections []traceSection `json:"sections"`
		Pieces   []struct {
			Score  float64 `json:"score"`
			Chosen bool    `json:"chosen"`
			Order  int     `json:"order"`
		} `json:"pieces"`
	} `json:"rank"`
	Output struct {
		Text   string `json:"text"`
		Tokens int    `json:"tokens"`
		Budget int    `json:"budget"`
		Groups []struct {
			Section string `json:"section"`
			Cut     bool   `json:"cut"`
		} `json:"groups"`
	} `json:"output"`
}

// traceSection is what Rank did with a pool section (round 4 on): its
// rank in Gather, its best piece's rank, and whether it printed whole or
// cut, as which plan, or why it was passed over.
type traceSection struct {
	Section  string `json:"section"`
	Order    int    `json:"order"`
	BestRank int    `json:"best_rank"`
	Plan     int    `json:"plan"`
	Printed  string `json:"printed"`
	Passed   string `json:"passed"`
}

// caseResult is one case's run and its checks.
type caseResult struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Library  string `json:"library"`
	Kind     string `json:"kind,omitempty"`
	Split    string `json:"split,omitempty"`
	Budget   int    `json:"budget"`
	Exit     int    `json:"exit"`
	Error    string `json:"error,omitempty"`
	WallMS   int64  `json:"wall_ms"`
	Status   string `json:"status"`
	Path     string `json:"path"`
	// Stop is why Rank stopped choosing (the trace's rank.stop.reason).
	Stop     string           `json:"stop,omitempty"`
	Tokens   int              `json:"tokens"`
	InBudget bool             `json:"in_budget"`
	Trace    string           `json:"trace"`
	Terms    map[string]int   `json:"terms,omitempty"`
	Facts    []factResult     `json:"facts"`
	Leads    []leadResult     `json:"leads,omitempty"`
	NotFound []notFoundResult `json:"not_found"`
	// Answered is every fact printed (false for a case of none), Led every
	// lead printed (false for a case of none). LedByCut is every fact
	// printed or led by a cut group (its source section printed as a group
	// cut, with its source line; false for a case of none), LedEither Led
	// or LedByCut. BeforeFact is the tokens printed before the first fact
	// found in the output starts, nil when none is printed. Groups is how
	// many groups were printed.
	Answered   bool `json:"answered"`
	Led        bool `json:"led"`
	LedByCut   bool `json:"led_by_cut_group"`
	LedEither  bool `json:"led_either"`
	BeforeFact *int `json:"tokens_before_fact"`
	Groups     int  `json:"groups"`
}

// leadResult is whether the output prints a lead.
type leadResult struct {
	Lead    string `json:"lead"`
	Printed bool   `json:"printed"`
}

// factResult is one fact's four stage checks, with what explains them. For
// a fact with alternatives it is the alternative that got furthest
// (Alternative, from 1, of Alternatives); for an alternative of several
// quotes, Quote joins them, each check passes when every quote passes it,
// and what explains a check is the weakest quote's.
type factResult struct {
	Quote        string `json:"quote"`
	Source       string `json:"source"`
	Alternative  int    `json:"alternative,omitempty"`
	Alternatives int    `json:"alternatives,omitempty"`
	// Gather: the source section is in the pool. PoolRank is its place
	// there (1 the first, 0 absent), Via the list that brought it ("question"
	// or a term); QuoteInPool whether any pool section's text holds the
	// quote.
	Gather      bool   `json:"gather"`
	PoolRank    int    `json:"pool_rank"`
	Via         string `json:"via,omitempty"`
	QuoteInPool bool   `json:"quote_in_pool"`
	// Sort: one piece holds the quote (a chosen one, else the smallest
	// such: Piece).
	Sort  bool       `json:"sort"`
	Piece *pieceInfo `json:"piece,omitempty"`
	// Rank: a piece holding the quote is chosen. Section is what Rank did
	// with the reported piece's section, when the trace says (round 4 on):
	// "whole 2" or "cut 1" (printed, as the second or first section
	// opened), "passed budget", or "-" (never reached).
	Rank    bool   `json:"rank"`
	Section string `json:"section,omitempty"`
	// Output: the quote is printed and the printed tokens are within the
	// budget.
	Output  bool `json:"output"`
	Printed bool `json:"printed"`
	// CutGroup: every quote (of one alternative) is printed or its source
	// section is printed as a cut group.
	CutGroup bool `json:"cut_group"`
}

type pieceInfo struct {
	ID         int     `json:"id"`
	Section    string  `json:"section"`
	Kind       string  `json:"kind"`
	Role       string  `json:"role"`
	Tokens     int     `json:"tokens"`
	Score      float64 `json:"score"`
	Chosen     bool    `json:"chosen"`
	Order      int     `json:"order,omitempty"`
	FoldedInto *int    `json:"folded_into,omitempty"`
}

// notFoundResult is whether the output states a term the docs lack.
type notFoundResult struct {
	Term   string `json:"term"`
	Stated bool   `json:"stated"`
}

// grade checks c's facts and not_found terms against its trace.
func grade(r *caseResult, c qaCase, t *trace) {
	r.Status, r.Path, r.Tokens, r.Groups = statusName(t.Status), t.Path, t.Output.Tokens, len(t.Output.Groups)
	if t.Rank != nil && t.Rank.Stop != nil {
		r.Stop = t.Rank.Stop.Reason
	}
	r.InBudget = t.Output.Tokens > 0 && t.Output.Tokens <= c.Budget
	if t.Terms != nil {
		r.Terms = map[string]int{"(sections)": t.Terms.Sections}
		for _, term := range t.Terms.List {
			r.Terms[term.Term] = term.Sections
		}
	}
	printed, at := normalizeAt(t.Output.Text)
	r.Facts = []factResult{}
	r.Answered = len(c.Facts) > 0
	cut := cutGroups(t, c.Library)
	r.LedByCut = len(c.Facts) > 0
	for _, f := range c.Facts {
		fr := gradeFact(f, t, printed, r.InBudget)
		fr.CutGroup = ledByCut(f, printed, cut, c.Library)
		r.Facts = append(r.Facts, fr)
		r.Answered = r.Answered && fr.Printed
		r.LedByCut = r.LedByCut && fr.CutGroup
	}
	if first := firstFact(c.Facts, printed); first >= 0 {
		n := docs.EstimateTokens(t.Output.Text[:at[first]])
		r.BeforeFact = &n
	}
	r.Led = len(c.Leads) > 0
	for _, l := range c.Leads {
		ok := strings.Contains(printed, normalize(l))
		r.Leads = append(r.Leads, leadResult{Lead: l, Printed: ok})
		r.Led = r.Led && ok
	}
	r.LedEither = r.Led || r.LedByCut
	r.NotFound = []notFoundResult{}
	for _, term := range c.NotFound {
		r.NotFound = append(r.NotFound, notFoundResult{Term: term, Stated: statesNotFound(t.Output.Text, term)})
	}
}

// statusName is a status as the runner names it: "answered", which
// builds before round 7 printed for found, is read as "found".
func statusName(s string) string {
	if s == "answered" {
		return "found"
	}
	return s
}

// cutGroups are the sections the output prints as cut groups, by page
// identity in the library at root (docs.PageIdentity) and anchor.
func cutGroups(t *trace, root string) map[string]bool {
	out := map[string]bool{}
	for _, g := range t.Output.Groups {
		if g.Cut {
			out[sectionKey(g.Section, root)] = true
		}
	}
	return out
}

// sectionKey is a section reference (url#anchor) as one section of the
// library at root, however the library stores its page.
func sectionKey(ref, root string) string {
	page, anchor, _ := strings.Cut(ref, "#")
	return docs.PageIdentity(page, root) + "#" + anchor
}

// ledByCut reports a fact one of whose alternatives has every quote
// printed or its source section printed as a cut group (cut).
func ledByCut(f fact, printed string, cut map[string]bool, root string) bool {
	for _, alt := range f.alternatives() {
		ok := true
		for _, a := range alt {
			ok = ok && (strings.Contains(printed, normalize(a.Quote)) || cut[sectionKey(a.Source, root)])
		}
		if ok {
			return true
		}
	}
	return false
}

// firstFact is where the first quote of any of facts starts in printed
// (normalised), -1 for none.
func firstFact(facts []fact, printed string) int {
	first := -1
	for _, f := range facts {
		for _, alt := range f.alternatives() {
			for _, a := range alt {
				if i := strings.Index(printed, normalize(a.Quote)); i >= 0 && (first < 0 || i < first) {
					first = i
				}
			}
		}
	}
	return first
}

// gradeFact grades each of f's alternatives and keeps the one that got
// furthest, the first on a tie.
func gradeFact(f fact, t *trace, printed string, inBudget bool) factResult {
	alts := f.alternatives()
	var best factResult
	for i, alt := range alts {
		fr := gradeAlternative(alt, t, printed, inBudget)
		if len(alts) > 1 {
			fr.Alternative, fr.Alternatives = i+1, len(alts)
		}
		if i == 0 || fr.weight() > best.weight() {
			best = fr
		}
	}
	return best
}

// weight orders results by how far they got: printed, then output, rank,
// sort and gather.
func (fr factResult) weight() int {
	w := 0
	for i, ok := range []bool{fr.Gather, fr.Sort, fr.Rank, fr.Output, fr.Printed} {
		if ok {
			w += 1 << i
		}
	}
	return w
}

// gradeAlternative grades an alternative's quotes: a check passes when it
// passes for every quote, and the weakest quote explains it.
func gradeAlternative(alt []anchor, t *trace, printed string, inBudget bool) factResult {
	var out factResult
	quotes := make([]string, 0, len(alt))
	for i, a := range alt {
		fr := gradeQuote(a, t, printed, inBudget)
		quotes = append(quotes, a.Quote)
		if i == 0 {
			out = fr
			continue
		}
		weak := out
		if fr.weight() < out.weight() {
			weak = fr
		}
		poolRank := 0
		if out.Gather && fr.Gather {
			poolRank = max(out.PoolRank, fr.PoolRank)
		}
		out = factResult{
			Source: weak.Source, Gather: out.Gather && fr.Gather, PoolRank: poolRank, Via: weak.Via,
			QuoteInPool: out.QuoteInPool && fr.QuoteInPool, Sort: out.Sort && fr.Sort, Piece: weak.Piece, Section: weak.Section,
			Rank: out.Rank && fr.Rank, Output: out.Output && fr.Output, Printed: out.Printed && fr.Printed,
		}
	}
	out.Quote = strings.Join(quotes, " … ")
	return out
}

func gradeQuote(a anchor, t *trace, printed string, inBudget bool) factResult {
	fr := factResult{Quote: a.Quote, Source: a.Source}
	quote := normalize(a.Quote)
	gatherCheck(&fr, t, quote)
	pieceCheck(&fr, t, quote)
	sectionCheck(&fr, t)
	fr.Printed = strings.Contains(printed, quote)
	fr.Output = fr.Printed && inBudget
	return fr
}

// gatherCheck finds the fact's section in the pool, and its quote in any
// pool section's text: Gather passes when either is there. PoolRank is the
// source section's place, else the first section holding the quote.
func gatherCheck(fr *factResult, t *trace, quote string) {
	if t.Gather == nil {
		return
	}
	page, anchor, _ := strings.Cut(fr.Source, "#")
	holds := 0
	for i, s := range t.Gather.Pool {
		if s.Page == page && s.Anchor == anchor && fr.PoolRank == 0 {
			fr.PoolRank, fr.Via = i+1, s.Via
		}
		if holds == 0 && strings.Contains(normalize(s.Text), quote) {
			holds, fr.QuoteInPool = i+1, true
		}
	}
	if fr.PoolRank == 0 && holds > 0 {
		fr.PoolRank, fr.Via = holds, t.Gather.Pool[holds-1].Via
	}
	fr.Gather = fr.PoolRank > 0
}

// pieceCheck finds the pieces holding the quote (Sort) and whether one is
// chosen (Rank), and reports one: a chosen one first, then the smallest. A
// piece folded into another is chosen when that one is.
func pieceCheck(fr *factResult, t *trace, quote string) {
	if t.Sort == nil || t.Rank == nil {
		return
	}
	for i, p := range t.Sort.Pieces {
		if !strings.Contains(normalize(p.Text), quote) || i >= len(t.Rank.Pieces) {
			continue
		}
		rp := t.Rank.Pieces[i]
		if p.FoldedInto != nil && *p.FoldedInto < len(t.Rank.Pieces) {
			rp = t.Rank.Pieces[*p.FoldedInto]
		}
		chosen := rp.Chosen
		fr.Sort, fr.Rank = true, fr.Rank || chosen
		if q := fr.Piece; q != nil && (q.Chosen && !chosen || q.Chosen == chosen && q.Tokens <= p.Tokens) {
			continue
		}
		fr.Piece = &pieceInfo{ID: p.ID, Section: p.Section, Kind: p.Kind, Role: p.Role, Tokens: p.Tokens, Score: rp.Score, Chosen: chosen, Order: rp.Order, FoldedInto: p.FoldedInto}
	}
}

// sectionCheck records what Rank did with the reported piece's section.
func sectionCheck(fr *factResult, t *trace) {
	if t.Rank == nil || fr.Piece == nil {
		return
	}
	for _, sec := range t.Rank.Sections {
		if sec.Section != fr.Piece.Section {
			continue
		}
		switch {
		case sec.Plan > 0:
			fr.Section = fmt.Sprintf("%s %d", sec.Printed, sec.Plan)
		case sec.Passed != "":
			fr.Section = "passed " + sec.Passed
		default:
			fr.Section = "-"
		}
		return
	}
}

// statesNotFound reports whether the output's frontmatter (between its
// two "---" fences at the top) lists term under not_found, spelled
// exactly as the question writes it.
func statesNotFound(output, term string) bool {
	return slices.Contains(frontmatterList(output, "not_found"), term)
}

// frontmatterList is the YAML flow sequence ([a, "b, c"]) the output's
// frontmatter holds under key; nil when it has none.
func frontmatterList(output, key string) []string {
	rest, ok := strings.CutPrefix(output, "---\n")
	if !ok {
		return nil
	}
	head, _, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return nil
	}
	for _, line := range strings.Split(head, "\n") {
		v, ok := strings.CutPrefix(line, key+":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
			return nil
		}
		return flowItems(v[1 : len(v)-1])
	}
	return nil
}

// flowItems splits the inside of a YAML flow sequence into its items: plain
// scalars trimmed, double-quoted ones unquoted.
func flowItems(s string) []string {
	var out []string
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		if s[0] == '"' {
			q, err := strconv.QuotedPrefix(s)
			if err != nil {
				return append(out, s)
			}
			item, _ := strconv.Unquote(q)
			out = append(out, item)
			s = strings.TrimPrefix(strings.TrimSpace(s[len(q):]), ",")
			continue
		}
		item, rest, _ := strings.Cut(s, ",")
		out = append(out, strings.TrimSpace(item))
		s = rest
	}
	return out
}

// normalize is text as the checks compare it: Markdown backticks,
// emphasis markers (*, and _ at a word's edge) and backslash escapes out
// (pam\_duo is pam_duo, *memoized* is memoized), a link as its text
// ([text](url) is text), whitespace collapsed, lower case. The quote and
// the text it is looked for in are normalised alike.
func normalize(s string) string {
	n, _ := normalizeAt(s)
	return n
}

// normalizeAt is normalize with, for each byte of the normalized text,
// the offset in s of the character it came from.
func normalizeAt(s string) (string, []int) {
	var b []byte
	var at []int
	space := false
	linkEnd := map[int]int{} // a link's "](" offset: the offset after its ")"
	for i := 0; i < len(s); {
		if end, ok := linkEnd[i]; ok {
			i = end
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == '\\' && i+1 < len(s) && strings.IndexByte(asciiPunct, s[i+1]) >= 0 {
			i++
			r, n = rune(s[i]), 1
		}
		switch {
		case r == '`', r == '*', r == '_' && emphasisEdge(s, i):
			i += n
			continue
		case r == '[' && linkAt(s, i, linkEnd):
			i += n
			continue
		case unicode.IsSpace(r):
			space = len(b) > 0
			i += n
			continue
		}
		if space {
			b, at = append(b, ' '), append(at, i)
			space = false
		}
		for _, c := range []byte(string(unicode.ToLower(r))) {
			b, at = append(b, c), append(at, i)
		}
		i += n
	}
	return string(b), at
}

const asciiPunct = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

// emphasisEdge reports an underscore at s[i] at a word's edge (the
// character before it, an escape aside, or the one after it is no letter
// or digit): emphasis (_without_), not part of a name (pam_duo).
func emphasisEdge(s string, i int) bool {
	j := i
	if j > 0 && s[j-1] == '\\' {
		j--
	}
	before, _ := utf8.DecodeLastRuneInString(s[:j])
	after, _ := utf8.DecodeRuneInString(s[i+1:])
	return !isWordRune(before) || !isWordRune(after)
}

func isWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// linkAt reports a Markdown link or image opening at s[i] ("[text](url)"
// on one line) and records in linkEnd where its target starts and ends,
// so its text stays and its target is skipped. An image's "!" stays: it
// is punctuation alike on both sides.
func linkAt(s string, i int, linkEnd map[int]int) bool {
	text := strings.IndexAny(s[i+1:], "]\n")
	if text < 0 || s[i+1+text] != ']' {
		return false
	}
	mid := i + 1 + text
	if mid+1 >= len(s) || s[mid+1] != '(' {
		return false
	}
	target := strings.IndexAny(s[mid+2:], ")\n")
	if target < 0 || s[mid+2+target] != ')' {
		return false
	}
	linkEnd[mid] = mid + 2 + target + 1
	return true
}

// report is a run: the results and their totals per stage.
type report struct {
	Ketch   string       `json:"ketch"`
	Version string       `json:"version"`
	Sandbox string       `json:"sandbox"`
	Cases   string       `json:"cases"`
	Run     time.Time    `json:"run"`
	Results []caseResult `json:"results"`
	Totals  totals       `json:"totals"`
}

type totals struct {
	Gather   tally `json:"gather"`
	Sort     tally `json:"sort"`
	Rank     tally `json:"rank"`
	Output   tally `json:"output"`
	NotFound tally `json:"not_found"`
	// Answered counts the cases with facts, Led those with leads (led by
	// their words), LedByCut those with facts (led by cut groups),
	// LedEither those with either (led by words or cut groups), and
	// AnsweredOrLed those with either (answered, or led either way);
	// MeanTokens is the mean of every case's printed tokens.
	Answered      tally `json:"answered"`
	Led           tally `json:"led"`
	LedByCut      tally `json:"led_by_cut_group"`
	LedEither     tally `json:"led_either"`
	AnsweredOrLed tally `json:"answered_or_led"`
	MeanTokens    int   `json:"mean_tokens"`
	// Statuses counts the answered cases (every fact printed) by the
	// status the output states: a status the caller can trust says
	// answered when the answer is there.
	Statuses map[string]int `json:"statuses_of_answered"`
}

type tally struct {
	Passed int `json:"passed"`
	Checks int `json:"checks"`
}

func (t *tally) add(ok bool) {
	t.Checks++
	if ok {
		t.Passed++
	}
}

// total counts the checks per stage. A not_found term is an output check
// too: the output must say what the docs lack.
func (r *report) total() {
	for _, c := range r.Results {
		for _, f := range c.Facts {
			r.Totals.Gather.add(f.Gather)
			r.Totals.Sort.add(f.Sort)
			r.Totals.Rank.add(f.Rank)
			r.Totals.Output.add(f.Output)
		}
		for _, n := range c.NotFound {
			r.Totals.NotFound.add(n.Stated)
			r.Totals.Output.add(n.Stated)
		}
		if len(c.Facts) > 0 {
			r.Totals.Answered.add(c.Answered)
		}
		if c.Answered {
			if r.Totals.Statuses == nil {
				r.Totals.Statuses = map[string]int{}
			}
			r.Totals.Statuses[c.Status]++
		}
		if len(c.Leads) > 0 {
			r.Totals.Led.add(c.Led)
		}
		if len(c.Facts) > 0 {
			r.Totals.LedByCut.add(c.LedByCut)
		}
		if len(c.Facts) > 0 || len(c.Leads) > 0 {
			r.Totals.LedEither.add(c.Led || c.LedByCut)
			r.Totals.AnsweredOrLed.add(c.Answered || c.Led || c.LedByCut)
		}
		r.Totals.MeanTokens += c.Tokens
	}
	if len(r.Results) > 0 {
		r.Totals.MeanTokens /= len(r.Results)
	}
}

func printTable(w io.Writer, r *report) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	mark := func(ok bool) string {
		if ok {
			return "pass"
		}
		return "FAIL"
	}
	// pool: the source section's place in the pool.
	p("%-18s %-44s %-6s %-5s %-6s %-6s %-6s %7s %7s\n", "case", "check", "gather", "pool", "sort", "rank", "output", "tokens", "ms")
	for _, c := range r.Results {
		for _, f := range c.Facts {
			pool := "-"
			if f.PoolRank > 0 {
				pool = strconv.Itoa(f.PoolRank)
			}
			p("%-18s %-44s %-6s %-5s %-6s %-6s %-6s %7d %7d\n", c.ID, clip(f.Quote, 44), mark(f.Gather), pool, mark(f.Sort), mark(f.Rank), mark(f.Output), c.Tokens, c.WallMS)
		}
		for _, n := range c.NotFound {
			p("%-18s %-44s %-6s %-5s %-6s %-6s %-6s %7d %7d\n", c.ID, clip("not_found: "+n.Term, 44), "-", "-", "-", "-", mark(n.Stated), c.Tokens, c.WallMS)
		}
		if c.Error != "" {
			p("%-18s error (exit %d): %s\n", c.ID, c.Exit, clip(c.Error, 200))
		}
	}
	t := r.Totals
	p("%-18s %-44s %-6s %-5s %-6s %-6s %-6s\n", "totals", "", frac(t.Gather), "", frac(t.Sort), frac(t.Rank), frac(t.Output))
	p("\n%-18s %-8s %-8s %-8s %7s %13s %7s\n", "case", "answered", "led", "led cut", "tokens", "before fact", "ms")
	for _, c := range r.Results {
		answered, led, before := yesNo(c.Answered, len(c.Facts) > 0), yesNo(c.Led, len(c.Leads) > 0), "-"
		if c.BeforeFact != nil {
			before = strconv.Itoa(*c.BeforeFact)
		}
		p("%-18s %-8s %-8s %-8s %7d %13s %7d\n", c.ID, answered, led, yesNo(c.LedByCut, len(c.Facts) > 0), c.Tokens, before, c.WallMS)
	}
	p("%-18s %-8s %-8s %-8s %7d   led either way %s, answered or led %s\n", "totals", frac(t.Answered), frac(t.Led), frac(t.LedByCut), t.MeanTokens, frac(t.LedEither), frac(t.AnsweredOrLed))
	p("%-18s %s\n", "answered, status", statusShares(t.Statuses))
}

// statusShares is the answered cases' statuses with their counts, in the
// outcomes' order: "found 30, partial 2, refused 0".
func statusShares(m map[string]int) string {
	var parts []string
	for _, s := range []string{"found", "partial", "refused", "failed"} {
		parts = append(parts, fmt.Sprintf("%s %d", s, m[s]))
	}
	return strings.Join(parts, ", ")
}

// yesNo is ok as "yes" or "no", "-" when there is nothing to check.
func yesNo(ok, any bool) string {
	switch {
	case !any:
		return "-"
	case ok:
		return "yes"
	}
	return "no"
}

func frac(t tally) string { return fmt.Sprintf("%d/%d", t.Passed, t.Checks) }

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
