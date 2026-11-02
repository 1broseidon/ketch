package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/docs"
)

// upstreamErrf owns the [upstream]/[cancelled]/[not_found] split for every
// tool handler; these cases pin the prefix contract so the MCP surface cannot
// drift from the CLI exit codes.
func TestUpstreamErrfClassification(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantPrefix string
	}{
		{"generic upstream", errors.New("boom"), "[upstream]"},
		{"cancelled", context.Canceled, "[cancelled]"},
		{"deadline", context.DeadlineExceeded, "[cancelled]"},
		{"docs library required", fmt.Errorf("web: %w: pass the docs site URL as library", docs.ErrLibraryRequired), "[validation]"},
		// Ambiguity reaches [validation] through docs.ErrLibraryRequired, which
		// *docs.AmbiguousLibraryError also wraps: no MCP rule of its own.
		{"docs ambiguous library", fmt.Errorf("web: %w", &docs.AmbiguousLibraryError{Library: "widget", Matches: []docs.LibraryMatch{{ID: "https://widget.dev/"}, {ID: "https://widget.readthedocs.io/en/stable/"}}}), "[validation]"},
		{"docs not found", fmt.Errorf("context7: library %q %w", "/no/such-lib", docs.ErrNotFound), "[not_found]"},
		{"context7 quota spent", fmt.Errorf("context7 resolve failed: %w", &docs.RateLimitError{Limit: 200}), "[upstream]"},
		{"repo not found", fmt.Errorf("%w: facebook/react is not on https://sourcegraph.com", code.ErrRepoNotFound), "[not_found]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := upstreamErrf(tc.err, "docs fetch failed")
			if !strings.HasPrefix(got.Error(), tc.wantPrefix+" ") {
				t.Errorf("prefix = %q, want %q (full error: %v)", got.Error(), tc.wantPrefix, got)
			}
		})
	}
}
