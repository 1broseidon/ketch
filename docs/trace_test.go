package docs

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// A trace of an answer from the index holds the terms with their section
// counts and weights, the pool, and every piece with its kind, role,
// score and choice: a chosen piece's text is in the printed answer. The
// answer is the same with a trace as without, at the default budget and a
// small one.
func TestTraceOfIndexedAnswer(t *testing.T) {
	store := newStore(t)
	lib := buildIndex(t, exampleSite(), store).Library
	l := NewLocal(store)
	const query = "add middleware to a route with app.use"
	for _, tokens := range []int{0, 300} {
		ctx := WithLocalOptions(context.Background(), LocalOptions{Tokens: tokens})
		plain := l.answer(ctx, lib, query, LocalOptions{Tokens: tokens})
		tr := NewTrace(query)
		o := l.answer(WithTrace(ctx, tr), lib, query, LocalOptions{Tokens: tokens})
		if !sameResults(plain.Results, o.Results) || len(o.Results) == 0 {
			t.Fatalf("tokens %d: the trace changed the answer:\n%+v\n%+v", tokens, plain.Results, o.Results)
		}
		var printed strings.Builder
		for _, r := range o.Results {
			printed.WriteString(RenderResult(r, HintNone) + "\n")
		}
		_, _ = tr.Stdout(&strings.Builder{}).Write([]byte(printed.String()))
		tr.SetOutcome(SearchBackend, o)

		path := filepath.Join(t.TempDir(), "trace.json")
		if err := tr.Write(path, nil); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got Trace
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		checkTrace(t, tokens, &got, printed.String())
	}
}

func checkTrace(t *testing.T, tokens int, got *Trace, printed string) {
	t.Helper()
	if got.Library != idxRoot || got.Status != Found || got.Output.Text != printed || got.Output.Tokens == 0 {
		t.Errorf("tokens %d: header = %q %q, output %d tokens", tokens, got.Library, got.Status, got.Output.Tokens)
	}
	checkTraceTerms(t, tokens, got)
	if got.Gather == nil || got.Gather.FTSHits == 0 || len(got.Gather.Pool) == 0 || got.Gather.Pool[0].Via != "question" || got.Gather.Pool[0].Score != 1 {
		t.Fatalf("tokens %d: gather = %+v", tokens, got.Gather)
	}
	checkTracePieces(t, tokens, got, printed)
}

// checkTraceTerms checks the terms: each counted and weighed.
func checkTraceTerms(t *testing.T, tokens int, got *Trace) {
	t.Helper()
	if got.Terms == nil || got.Terms.Sections == 0 {
		t.Fatalf("tokens %d: terms = %+v", tokens, got.Terms)
	}
	for _, term := range got.Terms.List {
		if term.Sections == 0 || term.Sections > got.Terms.Sections || len(term.Uses) == 0 || term.Weight <= 0 {
			t.Errorf("tokens %d: term %+v of %d sections", tokens, term, got.Terms.Sections)
		}
	}
}

// checkTracePieces checks the pieces and sections: Sort's and Rank's line
// up, a scoring piece has its parts and its fused value from its rank and
// its section's, every printed piece is in the answer and in a printed
// section, and the stop names its reason.
func checkTracePieces(t *testing.T, tokens int, got *Trace, printed string) {
	t.Helper()
	if got.Sort == nil || got.Rank == nil || len(got.Sort.Pieces) == 0 || len(got.Sort.Pieces) != len(got.Rank.Pieces) || len(got.Rank.Sections) != len(got.Gather.Pool) {
		t.Fatalf("tokens %d: sort %+v rank %+v", tokens, got.Sort, got.Rank)
	}
	order, plans := checkTraceSections(t, tokens, got)
	for i, c := range got.Rank.Pieces {
		checkTracePiece(t, tokens, got.Sort.Pieces[i], c, order, plans, printed)
	}
	if st := got.Rank.Stop.Reason; st != "budget" && st != "end" {
		t.Errorf("tokens %d: stop %q", tokens, st)
	}
}

// checkTracePiece checks one piece against its section's rank in Gather
// (order) and plan (plans), by address.
func checkTracePiece(t *testing.T, tokens int, p tracePiece, c traceChoice, order, plans map[string]int, printed string) {
	t.Helper()
	if p.ID != c.ID || p.Kind == "" || p.Role == "" {
		t.Errorf("tokens %d: piece %+v / %+v", tokens, p, c)
	}
	if c.Score > 0 && (c.Parts == nil || len(c.Parts.Terms) == 0 || c.Parts.RoleFactor <= 0 || c.Parts.Sum <= 0) {
		t.Errorf("tokens %d: piece %d scores %v with parts %+v", tokens, p.ID, c.Score, c.Parts)
	}
	if c.Walk > 0 && (c.Parts.Rank == 0 || math.Abs(c.Fused-1/float64(rankFusionK+c.Parts.Rank)-1/float64(rankFusionK+order[p.Section])) > 1e-12) {
		t.Errorf("tokens %d: piece %d fused %v, rank %d, section %d", tokens, p.ID, c.Fused, c.Parts.Rank, order[p.Section])
	}
	if c.Chosen && (!strings.Contains(printed, p.Text) || c.Order != plans[p.Section]) {
		t.Errorf("tokens %d: printed piece %d (plan %d of %d) is not in the answer: %q", tokens, p.ID, c.Order, plans[p.Section], p.Text)
	}
}

// checkTraceSections checks the sections: each printed one whole or cut,
// at a cost, passed over for no reason; their costs are what was used,
// within the budget. It returns each section's rank in Gather and its
// plan, by address.
func checkTraceSections(t *testing.T, tokens int, got *Trace) (map[string]int, map[string]int) {
	t.Helper()
	order, plans := map[string]int{}, map[string]int{}
	used := 0
	for _, sec := range got.Rank.Sections {
		order[sec.Section] = sec.Order
		if sec.Plan == 0 {
			continue
		}
		plans[sec.Section] = sec.Plan
		used += sec.Cost
		if sec.Printed != "whole" && sec.Printed != "cut" || sec.Cost <= 0 || sec.Passed != "" {
			t.Errorf("tokens %d: printed section %+v", tokens, sec)
		}
	}
	if len(plans) == 0 || used != got.Rank.Used || used > got.Rank.Budget {
		t.Errorf("tokens %d: %d sections printed, %d used of %d (%d)", tokens, len(plans), used, got.Rank.Budget, got.Rank.Used)
	}
	return order, plans
}

func sameResults(a, b []Result) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// Without a trace nothing is recorded, and its methods are no-ops.
func TestNilTrace(t *testing.T) {
	var tr *Trace
	if TraceFrom(context.Background()) != nil {
		t.Error("a trace from nowhere")
	}
	tr.SetOutcome(SearchBackend, Outcome{})
	tr.setPath("local")
	tr.ranking(docsindex.Library{}, LocalOptions{})
	tr.gathered(nil)
	tr.sorted(nil)
	tr.ranked(nil, nil, nil)
	if err := tr.Write(filepath.Join(t.TempDir(), "x"), nil); err != nil {
		t.Error(err)
	}
	if w := tr.Stdout(os.Stdout); w != os.Stdout {
		t.Error("stdout wrapped without a trace")
	}
}
