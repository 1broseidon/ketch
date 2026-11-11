package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/docs"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"place `pam_duo`\n  directly after":    "place pam_duo directly after",
		`place pam\_duo directly`:              "place pam_duo directly",
		`Set ` + "`verified_push=yes`" + ` in`: "set verified_push=yes in",
		`a \n b`:                               `a \n b`,
		"Ä\u00a0B  c":                          "ä b c",
		"a *memoized* version":                 "a memoized version",
		"**Middleware** allows":                "middleware allows",
		"_without_ any":                        "without any",
		"keep pam_duo and __init__":            "keep pam_duo and init",
		"see [the docs](https://x.test/a) now": "see the docs now",
		"a [b] (c) and arr[0]":                 "a [b] (c) and arr[0]",
		"features—_without_ it":                "features—without it",
	} {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// A fact is graded at each stage from the trace: the section in the pool,
// the quote in one piece, that piece chosen, the quote printed within the
// budget; a not_found term only when the frontmatter names it.
func TestGrade(t *testing.T) {
	raw := `{
	  "status": "answered",
	  "gather": {"pool": [{"page": "https://x.test/p", "anchor": "a", "via": "question", "text": "| ` + "`failmode`" + ` | fail \"safe\" or not |"}]},
	  "sort": {"pieces": [{"id": 0, "section": "https://x.test/p#a", "kind": "option row", "role": "reference", "text": "| ` + "`failmode`" + ` | fail \"safe\" or not |", "tokens": 40}]},
	  "rank": {"pieces": [{"score": 1.5, "chosen": false}]},
	  "output": {"text": "---\nquery: q\nnot_found: [pam_sss]\n---\nnothing here", "tokens": 20, "budget": 1800}
	}`
	var tr trace
	if err := json.Unmarshal([]byte(raw), &tr); err != nil {
		t.Fatal(err)
	}
	c := qaCase{ID: "x", Budget: 1800, Facts: []fact{{Quote: `fail "safe"`, Source: "https://x.test/p#a"}}, NotFound: []string{"pam_sss", "PAM_SERVICE_ERR"}}
	var r caseResult
	grade(&r, c, &tr)
	f := r.Facts[0]
	if !f.Gather || f.PoolRank != 1 || !f.QuoteInPool || !f.Sort || f.Rank || f.Output || f.Piece == nil || f.Piece.Tokens != 40 || f.Piece.Kind != "option row" || f.Via != "question" {
		t.Errorf("fact = %+v piece %+v", f, f.Piece)
	}
	if !r.InBudget || !r.NotFound[0].Stated || r.NotFound[1].Stated {
		t.Errorf("result = %+v", r)
	}
}

// A fact reports what Rank did with its piece's section, when the trace
// says.
func TestGradeSection(t *testing.T) {
	tr := &trace{}
	raw := `{"sort": {"pieces": [{"id": 0, "section": "https://x.test/p#a", "text": "fail safe", "tokens": 9}]},
	  "rank": {"sections": [{"section": "https://x.test/p#a", "order": 1, "plan": 2, "printed": "cut"}], "pieces": [{"score": 1, "chosen": true, "order": 2}]}}`
	if err := json.Unmarshal([]byte(raw), tr); err != nil {
		t.Fatal(err)
	}
	fr := factResult{Source: "https://x.test/p#a"}
	pieceCheck(&fr, tr, "fail safe")
	sectionCheck(&fr, tr)
	if fr.Section != "cut 2" {
		t.Errorf("section = %q", fr.Section)
	}
	tr.Rank.Sections[0].Plan, tr.Rank.Sections[0].Passed = 0, "budget"
	if sectionCheck(&fr, tr); fr.Section != "passed budget" {
		t.Errorf("section = %q", fr.Section)
	}
}

// A piece folded into another is chosen when that one is: the fact passes
// Rank by the copy that stands for it.
func TestGradeFolded(t *testing.T) {
	raw := `{
	  "gather": {"pool": [{"page": "https://x.test/p", "anchor": "a", "via": "failmod", "text": "fail safe"}]},
	  "sort": {"pieces": [{"id": 0, "section": "https://x.test/q#b", "kind": "option row", "text": "fail safe", "tokens": 9},
	                      {"id": 1, "section": "https://x.test/p#a", "kind": "option row", "text": "fail safe", "tokens": 9, "folded_into": 0}]},
	  "rank": {"pieces": [{"score": 3, "chosen": true, "order": 2}, {"score": 1, "chosen": false}]},
	  "output": {"text": "fail safe", "tokens": 5}
	}`
	var tr trace
	if err := json.Unmarshal([]byte(raw), &tr); err != nil {
		t.Fatal(err)
	}
	var r caseResult
	grade(&r, qaCase{ID: "x", Budget: 1800, Facts: []fact{{Quote: "fail safe", Source: "https://x.test/p#a"}}}, &tr)
	if f := r.Facts[0]; !f.Gather || f.Via != "failmod" || !f.Rank || !f.Output || f.Piece == nil || !f.Piece.Chosen || f.Piece.Order != 2 {
		t.Errorf("fact = %+v piece %+v", f, f.Piece)
	}
}

// A fact with alternatives passes by any one of them, and an alternative
// only when every one of its quotes passes; the result reports the
// alternative that got furthest.
func TestGradeAlternatives(t *testing.T) {
	tr := &trace{}
	tr.Output.Text, tr.Output.Tokens = "---\nquery: q\n---\nretries are off by default. Use max_retries.", 20
	f := fact{Alternatives: [][]anchor{
		{{Quote: "off by default"}, {Quote: "absent"}},
		{{Quote: "use max_retries"}},
	}}
	var r caseResult
	grade(&r, qaCase{ID: "x", Budget: 1800, Facts: []fact{f}}, tr)
	if fr := r.Facts[0]; !r.Answered || fr.Alternative != 2 || fr.Alternatives != 2 || fr.Quote != "use max_retries" {
		t.Errorf("result = %+v fact %+v", r, fr)
	}
	f.Alternatives = f.Alternatives[:1]
	grade(&r, qaCase{ID: "x", Budget: 1800, Facts: []fact{f}}, tr)
	if fr := r.Facts[0]; r.Answered || fr.Printed || fr.Quote != "off by default … absent" || r.BeforeFact == nil {
		t.Errorf("one quote of the only alternative missing: %+v fact %+v", r, fr)
	}
	f.Alternatives[0][1].Quote = "Use max_retries"
	grade(&r, qaCase{ID: "x", Budget: 1800, Facts: []fact{f}}, tr)
	if !r.Answered {
		t.Errorf("both quotes printed: %+v", r)
	}
}

// A case is answered when every fact is printed and led when every lead
// is; the tokens before the first fact count the printed text up to where
// it starts, and are nil when no fact is printed.
func TestGradeReader(t *testing.T) {
	out := "---\nquery: q\n---\n### A\n\nAlso in this section: failmode\n\nThe `pam\\_duo` row says fail safe."
	tr := &trace{}
	tr.Output.Text, tr.Output.Tokens = out, 30
	c := qaCase{ID: "x", Budget: 1800, Facts: []fact{{Quote: "pam_duo row says"}}, Leads: []string{"FAILMODE"}}
	var r caseResult
	grade(&r, c, tr)
	want := docs.EstimateTokens(out[:strings.Index(out, "pam\\_duo")])
	if !r.Answered || !r.Led || r.BeforeFact == nil || *r.BeforeFact != want {
		t.Errorf("result = %+v before %v, want %d", r, r.BeforeFact, want)
	}
	c.Facts = append(c.Facts, fact{Quote: "absent"})
	c.Leads = nil
	grade(&r, c, tr)
	if r.Answered || r.Led || r.BeforeFact == nil {
		t.Errorf("one fact missing, no leads: %+v", r)
	}
	c.Facts = []fact{{Quote: "absent"}}
	r = caseResult{}
	grade(&r, c, tr)
	if r.BeforeFact != nil {
		t.Errorf("no fact printed: before %v", *r.BeforeFact)
	}
	rep := &report{Results: []caseResult{{Answered: true, Facts: []factResult{{}}, Tokens: 10}, {Led: true, Leads: []leadResult{{}}, Facts: []factResult{{}}, Tokens: 30}, {Tokens: 20}}}
	rep.total()
	if rep.Totals.Answered != (tally{1, 2}) || rep.Totals.Led != (tally{1, 1}) || rep.Totals.AnsweredOrLed != (tally{2, 2}) || rep.Totals.MeanTokens != 20 {
		t.Errorf("totals = %+v", rep.Totals)
	}
}

// Gather passes when any pool section holds the quote, even when the
// source section is not in the pool.
func TestGatherByQuote(t *testing.T) {
	tr := &trace{}
	if err := json.Unmarshal([]byte(`{"gather": {"pool": [{"page": "https://x.test/q", "anchor": "b", "via": "fail", "text": "fail *safe* here"}]}}`), tr); err != nil {
		t.Fatal(err)
	}
	var fr factResult
	fr.Source = "https://x.test/p#a"
	gatherCheck(&fr, tr, normalize("fail safe"))
	if !fr.Gather || fr.PoolRank != 1 || !fr.QuoteInPool || fr.Via != "fail" {
		t.Errorf("fact = %+v", fr)
	}
}

// not_found is read from the printed frontmatter only, as a flow list,
// and a term matches only as the question spells it.
func TestStatesNotFound(t *testing.T) {
	out := "---\nquery: q\nnot_found: [pam_sss, \"a, b\"]\n---\n## T\n\nnot_found: [PAM_SERVICE_ERR]\n"
	for term, want := range map[string]bool{"pam_sss": true, "a, b": true, "PAM_SSS": false, "sss": false, "PAM_SERVICE_ERR": false} {
		if got := statesNotFound(out, term); got != want {
			t.Errorf("%q: %v, want %v", term, got, want)
		}
	}
	if got := frontmatterList("---\nnot_found: []\n---\n", "not_found"); len(got) != 0 {
		t.Errorf("empty list = %q", got)
	}
}
