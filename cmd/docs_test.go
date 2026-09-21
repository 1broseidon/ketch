package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/docs"
)

// setDocsBackend overrides the docs command's --backend flag for one test,
// restoring the previous value afterwards.
func setDocsBackend(t *testing.T, backend string) {
	t.Helper()
	prev, _ := docsCmd.Flags().GetString("backend")
	if err := docsCmd.Flags().Set("backend", backend); err != nil {
		t.Fatalf("set backend flag: %v", err)
	}
	t.Cleanup(func() { docsCmd.Flags().Set("backend", prev) }) //nolint:errcheck
}

// asExitError unwraps err into an *ExitError or fails the test. This is the
// exact classification main.go turns into the process exit code.
func asExitError(t *testing.T, err error) *ExitError {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("want *ExitError (mapped to a nonzero exit by main.go), got %T: %v", err, err)
	}
	return exitErr
}

// The unimplemented local docs backend must fail with a nonzero exit
// (precondition, 5) — never print an error and exit 0.
func TestDocsLocalBackendExitsPrecondition(t *testing.T) {
	setDocsBackend(t, "local")

	exitErr := asExitError(t, runDocs(docsCmd, []string{"test"}))
	if exitErr.Code != ExitPrecondition {
		t.Errorf("exit code = %d, want %d (precondition)", exitErr.Code, ExitPrecondition)
	}
	if exitErr.Code == 0 {
		t.Error("exit code must be nonzero")
	}
	if !strings.Contains(exitErr.Error(), "not yet implemented") {
		t.Errorf("error should say the backend is not implemented, got: %v", exitErr)
	}
}

// An unknown docs backend is a validation failure (exit 2) and the error must
// list the valid options.
func TestDocsUnknownBackendExitsValidationWithOptions(t *testing.T) {
	setDocsBackend(t, "bogus")

	exitErr := asExitError(t, runDocs(docsCmd, []string{"test"}))
	if exitErr.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d (validation)", exitErr.Code, ExitValidation)
	}
	if !strings.Contains(exitErr.Error(), "(available: context7)") {
		t.Errorf("error should list the available backends, got: %v", exitErr)
	}
}

// --resolve returns library IDs, not URLs, so a --tag has nothing to record:
// the combination is refused up front instead of succeeding and saving nothing.
func TestDocsResolveRejectsTag(t *testing.T) {
	if err := docsCmd.Flags().Set("resolve", "true"); err != nil {
		t.Fatal(err)
	}
	if err := docsCmd.Flags().Set("tag", "libs"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = docsCmd.Flags().Set("resolve", "false")
		_ = docsCmd.Flags().Set("tag", "")
	})

	exitErr := asExitError(t, runDocs(docsCmd, []string{"react"}))
	if exitErr.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d (validation)", exitErr.Code, ExitValidation)
	}
	if !strings.Contains(exitErr.Error(), "--tag cannot be combined with --resolve") {
		t.Errorf("error should name the conflicting flags, got: %v", exitErr)
	}
}

// upstreamErr must classify docs.ErrNotFound as exit 3 (permanently absent,
// not retryable) and everything else as exit 4 (upstream, retry may help).
func TestUpstreamErrClassification(t *testing.T) {
	notFound := fmt.Errorf("context7: library %q %w", "/no/such-lib", docs.ErrNotFound)
	if got := asExitError(t, upstreamErr(notFound, "docs fetch failed")); got.Code != ExitNotFound {
		t.Errorf("docs.ErrNotFound: exit code = %d, want %d (not found)", got.Code, ExitNotFound)
	}

	if got := asExitError(t, upstreamErr(errors.New("boom"), "docs fetch failed")); got.Code != ExitUpstream {
		t.Errorf("generic error: exit code = %d, want %d (upstream)", got.Code, ExitUpstream)
	}
}
