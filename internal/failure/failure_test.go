package failure_test

import (
	"errors"
	"testing"

	"github.com/1broseidon/ketch/internal/failure"
	"github.com/1broseidon/ketch/internal/failure/failuretest"
)

func TestClassifyTable(t *testing.T) {
	for _, tc := range failuretest.Cases() {
		t.Run(tc.Name, func(t *testing.T) {
			got := failure.Classify(tc.Err, failure.Upstream)
			if got.Kind != tc.Kind || got.Reason != tc.Reason {
				t.Errorf("Classify = %v/%q, want %v/%q", got.Kind, got.Reason, tc.Kind, tc.Reason)
			}
		})
	}
}

func TestClassifyFallback(t *testing.T) {
	if got := failure.Classify(errors.New("no key"), failure.Precondition); got.Kind != failure.Precondition {
		t.Errorf("fallback not used: %v", got.Kind)
	}
	if got := failure.Classify(nil, failure.Upstream); got != (failure.Class{}) {
		t.Errorf("nil classified as %v", got)
	}
}

func TestSentinelsCompareByIdentity(t *testing.T) {
	a := failure.New(failure.NotFound, "x", "same text")
	b := failure.New(failure.NotFound, "x", "same text")
	if errors.Is(a, b) {
		t.Error("two sentinels with the same text must stay distinct")
	}
	if !errors.Is(failure.Wrap(failure.Upstream, "y", a), a) {
		t.Error("Wrap must keep the wrapped sentinel reachable")
	}
}

func TestKindsHaveNamesAndCodes(t *testing.T) {
	names, codes := map[string]bool{}, map[int]bool{}
	for _, k := range failure.Kinds() {
		if names[k.String()] || codes[k.ExitCode()] || k.String() == "unknown" || k.ExitCode() == 1 {
			t.Errorf("kind %d: name %q / code %d not unique and valid", k, k.String(), k.ExitCode())
		}
		names[k.String()], codes[k.ExitCode()] = true, true
	}
}
