package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

func TestJSONStringsUnescapes(t *testing.T) {
	got := jsonStrings(`{"a":"fail \"safe\" (allow <access>)","b":[{"c":"x\ny"}],"n":3}`)
	want := []string{"a", `fail "safe" (allow <access>)`, "b", "c", "x\ny", "n"}
	if !slices.Equal(got, want) {
		t.Fatalf("jsonStrings = %q, want %q", got, want)
	}
}

func TestEvidence(t *testing.T) {
	facts := []flowFact{
		{Statement: "one", fact: fact{Quote: `fail "safe" (allow access)`}},
		{Statement: "alts", fact: fact{Alternatives: [][]anchor{
			{{Quote: "never printed"}},
			{{Quote: "first half"}, {Quote: "second half"}},
		}}},
		{Statement: "missing", fact: fact{Quote: "nowhere"}},
	}
	texts := []string{
		"first half of it",
		"On errors, fail *\"safe\"* (allow access).\n`second half`",
	}
	ev, all := evidence(facts, texts)
	if all {
		t.Fatal("evidence reports every fact reached with one missing")
	}
	got := []int{ev[0].Call, ev[1].Call, ev[2].Call}
	if !slices.Equal(got, []int{2, 2, 0}) {
		t.Fatalf("evidence calls = %v, want [2 2 0]", got)
	}
	if !ev[0].Reached || !ev[1].Reached || ev[2].Reached {
		t.Fatalf("evidence reached = %+v", ev)
	}
}

func TestCitations(t *testing.T) {
	answer := "Set it ([core](http://nginx.org/en/docs/http/ngx_http_core_module.html#directives)).\n" +
		"See https://duo.com/docs/duounix/, and https://made.up/page.\n" +
		"```nginx\nproxy_pass http://upstream.example.net:8000;\n```\n" +
		"Run against http://localhost:4242/webhook. Also https://react.dev/learn#x."
	urls := []string{"https://nginx.org/en/docs/http/ngx_http_core_module.html#directives", "https://duo.com/docs/duounix"}
	texts := []string{"a link https://react.dev/learn#x in a snippet"}
	got := citations(answer, urls, texts)
	want := []citation{
		{URL: "http://nginx.org/en/docs/http/ngx_http_core_module.html#directives", OK: true},
		{URL: "https://duo.com/docs/duounix/", OK: true},
		{URL: "https://made.up/page"},
		{URL: "https://react.dev/learn#x", InText: true},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("citations = %+v\nwant %+v", got, want)
	}
}

func TestCallSource(t *testing.T) {
	libs := []docsindex.Library{
		{Key: "https://duo.com/docs/", Root: "https://duo.com/docs/"},
		{Key: "https://nginx.org/en/docs/", Root: "https://nginx.org/en/docs/"},
	}
	cases := []struct {
		in   callInput
		res  docsResult
		want string
	}{
		{callInput{Library: "https://duo.com/docs/"}, docsResult{}, "https://duo.com/docs/"},
		{callInput{Library: "nginx"}, docsResult{Library: "https://nginx.org/en/docs/"}, "https://nginx.org/en/docs/"},
		{callInput{Section: "http://nginx.org/en/docs/http/ngx_http_core_module.html#directives"}, docsResult{}, "https://nginx.org/en/docs/"},
		{callInput{Query: "no library"}, docsResult{}, ""},
	}
	for _, c := range cases {
		if got := callSource(c.in, c.res, libs); got != c.want {
			t.Errorf("callSource(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSignals(t *testing.T) {
	first, _ := json.Marshal(map[string]any{
		"library":    "https://duo.com/docs/",
		"defined_in": []map[string]string{{"term": "failmode", "section": "https://duo.com/docs/duounix#duo-configuration-options"}},
		"not_found":  []string{"pam_sss"},
		"results": []map[string]any{{
			"section": "https://duo.com/docs/duounix#red-hat", "truncated": true,
			"contents": "also in this section: https_timeout; Fedora 39",
		}},
	})
	before := []flowCall{{N: 1, Result: string(first), Source: "https://duo.com/docs/"}}
	q := "pam_duo fallback to pam_sss when Duo is unreachable"
	cases := []struct {
		in     callInput
		source string
		want   []string
	}{
		{callInput{Section: "https://duo.com/docs/duounix#duo-configuration-options"}, "https://duo.com/docs/", []string{"defined_in"}},
		{callInput{Section: "https://duo.com/docs/duounix#red-hat", Page: 2}, "https://duo.com/docs/", []string{"cut_section", "page"}},
		{callInput{Section: "https://duo.com/docs/other#x"}, "https://duo.com/docs/", []string{"section_other"}},
		{callInput{Query: "https_timeout failmode"}, "https://duo.com/docs/", []string{"contents"}},
		{callInput{Query: "pam_sss"}, "https://sssd.io/", []string{"not_found"}},
		{callInput{Query: "pam_sss again"}, "https://duo.com/docs/", nil},
		{callInput{Query: "sssd", Resolve: true}, "", []string{"resolve"}},
	}
	for _, c := range cases {
		if got := signals(before, c.in, c.source, q); !slices.Equal(got, c.want) {
			t.Errorf("signals(%+v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestContentNames(t *testing.T) {
	got := contentNames("also in this section: `failmode`, https_timeout; Fedora 39 …")
	want := []string{"failmode", "https_timeout", "fedora 39"}
	if !slices.Equal(got, want) {
		t.Fatalf("contentNames = %q, want %q", got, want)
	}
}

func TestJudgedRight(t *testing.T) {
	var j flowJudge
	if err := json.Unmarshal([]byte(`{"facts":[{"id":"f1","verdict":"stated"}],"not_covered":[{"id":"n1","verdict":"said"}],"invented":[]}`), &j); err != nil {
		t.Fatal(err)
	}
	if !judgedRight(&j, 1, 1) {
		t.Error("all stated, said and nothing invented is not right")
	}
	if judgedRight(&j, 2, 1) {
		t.Error("a verdict short of the facts is right")
	}
	j.NotCovered[0].Verdict = "not_said"
	if judgedRight(&j, 1, 1) {
		t.Error("a not-covered expectation not said is right")
	}
	j.NotCovered[0].Verdict = "said"
	j.Invented = append(j.Invented, struct {
		Claim  string `json:"claim"`
		Reason string `json:"reason"`
	}{Claim: "x"})
	if judgedRight(&j, 1, 1) {
		t.Error("an invented claim is right")
	}
	if judgedRight(nil, 0, 0) {
		t.Error("no verdict is right")
	}
}

func TestParseStream(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","tools":["mcp__ketch__docs"],"model":"claude-sonnet-5-5"}`,
		`{"type":"assistant","message":{"model":"claude-sonnet-5-5","content":[{"type":"tool_use","id":"t1","name":"mcp__ketch__docs","input":{"query":"q","library":"https://duo.com/docs/"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"{\"status\":\"found\"}"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"the answer"}]}}`,
		`{"type":"result","subtype":"success","result":"the answer","num_turns":2,"total_cost_usd":0.01,"modelUsage":{"claude-sonnet-5-5":{}}}`,
	}, "\n")
	var r flowRun
	parseStream(&r, []byte(stream))
	if len(r.Calls) != 1 || r.Calls[0].Result != `{"status":"found"}` || r.Calls[0].Tool != "mcp__ketch__docs" {
		t.Fatalf("calls = %+v", r.Calls)
	}
	if r.Answer != "the answer" || r.Subtype != "success" || r.NumTurns != 2 || r.CostUSD != 0.01 {
		t.Fatalf("result = %q %q %d %v", r.Answer, r.Subtype, r.NumTurns, r.CostUSD)
	}
	if !slices.Equal(r.ModelUsed, []string{"claude-sonnet-5-5"}) || !slices.Equal(r.InitTools, []string{"mcp__ketch__docs"}) {
		t.Fatalf("models %v, tools %v", r.ModelUsed, r.InitTools)
	}
}

func TestResultText(t *testing.T) {
	if got := resultText(json.RawMessage(`[{"type":"text","text":"a"},{"type":"image"},{"type":"text","text":"b"}]`)); got != "a\nb" {
		t.Fatalf("resultText = %q", got)
	}
}
