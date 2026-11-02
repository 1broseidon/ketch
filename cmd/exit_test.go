package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/docs"
)

// An ambiguous library name is input the caller fixes by picking one
// candidate, so upstreamErr exits 2 for it through docs.ErrLibraryRequired,
// with no rule of its own, and keeps the candidates in the message.
func TestUpstreamErrAmbiguousLibraryIsValidation(t *testing.T) {
	amb := &docs.AmbiguousLibraryError{Library: "widget", Matches: []docs.LibraryMatch{
		{ID: "https://widget.dev/", Title: "Widget", Packages: []string{"npm:widget"}},
		{ID: "https://widget.readthedocs.io/en/stable/", Title: "widget", Packages: []string{"pypi:widget"}},
	}}
	err := upstreamErr(fmt.Errorf("web: %w", amb), "docs search failed")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitValidation {
		t.Fatalf("upstreamErr(ambiguous) = %#v, want exit %d (validation)", err, ExitValidation)
	}
	for _, want := range []string{"https://widget.dev/", "pypi:widget", "--library"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err, want)
		}
	}
	if !errors.As(err, &amb) {
		t.Error("the candidates must stay reachable with errors.As")
	}
}
