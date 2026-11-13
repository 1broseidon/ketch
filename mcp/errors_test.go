package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/failure/failuretest"
)

// upstreamErrf and the CLI's upstreamErr are checked against the same table
// (internal/failure/failuretest), so the prefix here and the exit code there
// always name the same failure.Kind.
func TestUpstreamErrfClassification(t *testing.T) {
	for _, tc := range failuretest.Cases() {
		t.Run(tc.Name, func(t *testing.T) {
			got := upstreamErrf(tc.Err, "docs fetch failed")
			if want := "[" + tc.Kind.String() + "] "; !strings.HasPrefix(got.Error(), want) {
				t.Errorf("prefix of %q, want %q", got, want)
			}
			if !errors.Is(got, tc.Err) {
				t.Error("the original error must stay reachable")
			}
		})
	}
}
