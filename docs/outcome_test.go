package docs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/1broseidon/ketch/internal/failure"
)

// Every way a docs call can end maps to exactly one status and reason.
func TestAnswerStates(t *testing.T) {
	amb := &AmbiguousLibraryError{Library: "widget"}
	cases := []struct {
		name    string
		results []Result
		err     error
		status  Status
		reason  string
		kind    failure.Kind
	}{
		{"found", []Result{{Title: "x"}}, nil, Found, "", 0},
		{"empty is a refusal", nil, nil, Refused, "no_results", failure.NotFound},
		{"not found", nil, fmt.Errorf("web: library %q %w", "zz", ErrNotFound), Refused, "not_found", failure.NotFound},
		{"no library named", nil, fmt.Errorf("web: %w", ErrLibraryRequired), Refused, "library_required", failure.Validation},
		{"ambiguous", nil, fmt.Errorf("web: %w", amb), Refused, "ambiguous", failure.Validation},
		{"source down", nil, errors.New("registry: 503"), Failed, "upstream", failure.Upstream},
		{"timeout", nil, fmt.Errorf("fetch: %w", context.DeadlineExceeded), Failed, "timeout", failure.Upstream},
		{"cancelled", nil, fmt.Errorf("fetch: %w", context.Canceled), Failed, "cancelled", failure.Cancelled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := Answer("web", tc.results, tc.err)
			if o.Status != tc.status || o.Reason != tc.reason || o.Kind() != tc.kind {
				t.Errorf("Answer = %s/%q/%v, want %s/%q/%v", o.Status, o.Reason, o.Kind(), tc.status, tc.reason, tc.kind)
			}
			if (o.Err() == nil) != (tc.status == Found) {
				t.Errorf("Err() = %v for status %s", o.Err(), o.Status)
			}
			if o.Results == nil {
				t.Error("Results must never be nil")
			}
			if len(o.Attempts) != 1 || o.Attempts[0].Status != tc.status {
				t.Errorf("attempts %+v", o.Attempts)
			}
		})
	}
}

// The JSON form never says "null" for results, and names the backend only
// when one answered.
func TestOutcomeJSON(t *testing.T) {
	b, _ := json.Marshal(Answer("web", nil, nil))
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if r, ok := got["results"].([]any); !ok || len(r) != 0 {
		t.Errorf("results = %v, want []", got["results"])
	}
	if _, ok := got["backend"]; ok {
		t.Errorf("a refusal names no backend: %s", b)
	}
	if got["status"] != "refused" || got["reason"] != "no_results" {
		t.Errorf("status/reason = %v/%v", got["status"], got["reason"])
	}
}
