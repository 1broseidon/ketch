package main

import (
	"math"
	"strings"
	"testing"
)

func TestScoringDetectsStructuralAndSemanticLoss(t *testing.T) {
	t.Parallel()
	markdown := "# Cleanup\n\nCancellation does not wait for completion.\n\n```yaml\npermissions:\n  id-token: write\n```\n\n" +
		"| Mode | Dirty read | Phantom read |\n|---|---|---|\n| Repeatable | No | Yes |\n\n[Guide](https://example.test/docs/guide)\n"
	checks := []assertion{
		{ID: "fact", Kind: "text", Text: "Cancellation does not wait for completion", Critical: true},
		{ID: "heading", Kind: "heading", Text: "Cleanup"},
		{ID: "code", Kind: "code", Text: "permissions:\n  id-token: write", Critical: true},
		{ID: "table", Kind: "table", Headers: []string{"Mode", "Dirty read", "Phantom read"}, Cells: []string{"Repeatable", "No", "Yes"}},
		{ID: "link", Kind: "link", Text: "https://example.test/docs/guide"},
		{ID: "noise", Kind: "absent", Text: "Subscribe now"},
	}
	cases := []struct{ name, from, to, fails string }{
		{"intact", "", "", ""},
		{"negation lost", "does not wait", "does wait", "fact"},
		{"heading flattened", "# Cleanup", "Cleanup", "heading"},
		{"lines flattened", "permissions:\n  id-token: write", "permissions: id-token: write", "code"},
		{"indentation lost", "  id-token", "id-token", "code"},
		{"columns swapped", "Repeatable | No | Yes", "Repeatable | Yes | No", "table"},
		{"relative link", "https://example.test/docs/guide", "guide", "link"},
		{"chrome leaked", "[Guide]", "Subscribe now\n\n[Guide]", "noise"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidate := markdown
			if tc.from != "" {
				candidate = strings.ReplaceAll(candidate, tc.from, tc.to)
			}
			a, err := score(candidate, "Cancellation does not wait for completion", checks)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range a.Checks {
				if want := c.ID != tc.fails; c.Passed != want {
					t.Errorf("%s passed=%v, want %v", c.ID, c.Passed, want)
				}
			}
		})
	}
}

func TestCodeInProseCannotPassCodeCheck(t *testing.T) {
	t.Parallel()
	a, err := score("Use `return false;` here.", "Use return false here", []assertion{{ID: "code", Kind: "code", Text: "return false;"}})
	if err != nil || a.Checks[0].Passed {
		t.Fatalf("inline text passed code structure check: %+v, %v", a, err)
	}
}

func TestTokenCoverageCountsRepetitionAndNoise(t *testing.T) {
	t.Parallel()
	a := tokenCoverage("alpha beta beta ad ad", "alpha beta beta beta")
	if a.TokenRecall != .75 || a.TokenPrecision != .6 || math.Abs(a.TokenF1-2.0/3) > 1e-9 {
		t.Fatalf("bad multiset scoring: %+v", a)
	}
	if got := tokenCoverage("", "not empty"); got.TokenRecall != 0 || got.TokenPrecision != 0 {
		t.Fatalf("empty output got credit: %+v", got)
	}
}

func TestPhrasesPreserveOperatorsAndWordBoundaries(t *testing.T) {
	t.Parallel()
	if containsPhrase("Valid when count <= 10.", "count >= 10") {
		t.Fatal("reversed comparison received credit")
	}
	if containsPhrase("unexpected success", "expected success") {
		t.Fatal("partial word received credit")
	}
	if !containsPhrase("Valid when COUNT >=\n10.", "count >= 10") {
		t.Fatal("case or layout whitespace rejected")
	}
}

func TestFailedPagesNeverPassExclusionChecks(t *testing.T) {
	t.Parallel()
	f := fixture{Reference: "expected content", Spec: pageSpec{Checks: []assertion{
		{ID: "noise", Kind: "absent", Text: "Subscribe now"},
	}}}
	a := failedAccuracy(f)
	if len(a.Checks) != 1 || a.Checks[0].Passed || a.ReferenceWords != 2 {
		t.Fatalf("failure must remain in denominator: %+v", a)
	}
}

func TestTableValuesCannotBeBorrowedFromAnotherTable(t *testing.T) {
	t.Parallel()
	md := "| Mode | Allowed |\n|---|---|\n| Other | No |\n\n| Wrong | Columns |\n|---|---|\n| Requested | Yes |"
	a, err := score(md, "", []assertion{{ID: "table", Kind: "table", Headers: []string{"Mode", "Allowed"}, Cells: []string{"Requested", "Yes"}}})
	if err != nil || a.Checks[0].Passed {
		t.Fatalf("table association lost: %+v, %v", a, err)
	}
}

func TestTableValuesPreserveOperators(t *testing.T) {
	t.Parallel()
	md := "| Count | Allowed |\n|---|---|\n| <= 10 | Yes |"
	a, err := score(md, "", []assertion{{ID: "table", Kind: "table", Headers: []string{"Count", "Allowed"}, Cells: []string{">= 10", "Yes"}}})
	if err != nil || a.Checks[0].Passed {
		t.Fatalf("reversed table comparison received credit: %+v, %v", a, err)
	}
}

func TestValidEmbeddedHTMLCountsWithoutScriptText(t *testing.T) {
	t.Parallel()
	md := "first<br>second\n\n<table><tr><th>Mode</th><th>Result</th></tr><tr><td>One</td><td>Yes</td></tr></table>\n\n<script>Unwanted script text</script>"
	a, err := score(md, "first second Mode Result One Yes", []assertion{
		{ID: "line-break", Kind: "text", Text: "first second"},
		{ID: "table", Kind: "table", Headers: []string{"Mode", "Result"}, Cells: []string{"One", "Yes"}},
		{ID: "script", Kind: "absent", Text: "Unwanted script text"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range a.Checks {
		if !c.Passed {
			t.Errorf("valid HTML or script exclusion failed: %s", c.ID)
		}
	}
	if a.TokenRecall != 1 || a.TokenPrecision != 1 {
		t.Fatalf("valid embedded HTML penalized: %+v", a)
	}
}
