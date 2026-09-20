package main

import (
	"strings"
	"testing"
)

func TestCohortReportsKeepFailuresInDenominator(t *testing.T) {
	t.Parallel()
	pages := []pageResult{
		{ID: "original", Cohort: "regression", Deterministic: true, Accuracy: accuracy{TokenRecall: 1, TokenPrecision: 1}},
		{ID: "good", Cohort: "heldout", Deterministic: true, Accuracy: accuracy{TokenRecall: 1, TokenPrecision: 1,
			Checks: []checkResult{{ID: "fact", Passed: true, Critical: true}}}},
		{ID: "failed", Cohort: "heldout", Errors: []string{"timeout"}, Accuracy: accuracy{
			Checks: []checkResult{{ID: "fact", Critical: true}}}},
	}
	groups := summarizeCohorts(pages)
	held := groups["heldout"]
	if held.Pages != 2 || held.FailedPages != 1 || held.MacroRecall != .5 || held.CriticalPassed != 1 || held.CriticalTotal != 2 {
		t.Fatalf("cohort hid failed extraction: %+v", held)
	}
	var b strings.Builder
	writeCohortReport(&b, groups)
	if !strings.Contains(b.String(), "| heldout | 2 | 50.0% | 50.0% | 1/2 | 1/2 |") {
		t.Fatalf("cohort summary absent: %s", b.String())
	}
}
