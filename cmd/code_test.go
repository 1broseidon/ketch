package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/code"
)

// setCodeFlag overrides one of the code command's flags for one test,
// restoring the previous value afterwards.
func setCodeFlag(t *testing.T, name, value string) {
	t.Helper()
	prev, _ := codeCmd.Flags().GetString(name)
	if err := codeCmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set %s flag: %v", name, err)
	}
	t.Cleanup(func() { codeCmd.Flags().Set(name, prev) }) //nolint:errcheck
}

// A malformed --repo is a validation failure (exit 2), caught before any
// backend is built or contacted.
func TestCodeRepoFlagRejectsMalformedValue(t *testing.T) {
	setCodeFlag(t, "backend", "grepapp")
	setCodeFlag(t, "repo", "https://github.com/golang/go/tree/master")

	exitErr := asExitError(t, runCode(codeCmd, []string{"gcControllerState"}))
	if exitErr.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d (validation)", exitErr.Code, ExitValidation)
	}
	if !strings.Contains(exitErr.Error(), "--repo") || !strings.Contains(exitErr.Error(), "owner/name") {
		t.Errorf("error should name the flag and the expected form, got: %v", exitErr)
	}
}

// A backend without the repository is a not-found (exit 3) that points at the
// other backends, not an upstream failure to retry.
func TestCodeSearchErrClassification(t *testing.T) {
	missing := fmt.Errorf("%w: facebook/react is not on https://sourcegraph.com", code.ErrRepoNotFound)
	got := asExitError(t, codeSearchErr(missing, "sourcegraph"))
	if got.Code != ExitNotFound || !strings.Contains(got.Error(), "try -b grepapp or -b github") {
		t.Errorf("missing repo: exit %d, %q; want exit %d pointing at the other backends", got.Code, got.Error(), ExitNotFound)
	}
	if got := asExitError(t, codeSearchErr(code.ErrRegexpUnsupported, "github")); got.Code != ExitValidation {
		t.Errorf("regexp unsupported: exit code = %d, want %d", got.Code, ExitValidation)
	}
	if got := asExitError(t, codeSearchErr(errors.New("grep.app returned status 504"), "grepapp")); got.Code != ExitUpstream {
		t.Errorf("backend failure: exit code = %d, want %d", got.Code, ExitUpstream)
	}
}

// grepapp matches the whole query as literal code, so a repo: qualifier finds
// nothing and the empty result reads as "no such code". Both that and an
// empty repo-scoped search on its partial index get a warning.
func TestWarnCodeSearch(t *testing.T) {
	literal := code.Query{Term: "repo:1broseidon/ketch NewFromConfig", Limit: 5}
	out, _ := captureStderr(t, func() error { warnCodeSearch(false, "grepapp", literal, nil); return nil })
	if !strings.HasPrefix(out, "warn: grepapp searched \"repo:1broseidon/ketch\" as literal code") || !strings.Contains(out, "--repo") {
		t.Errorf("literal qualifier warning = %q", out)
	}

	scoped := code.Query{Term: "reconcileChildFibers", Repo: "facebook/react", Limit: 5}
	out, _ = captureStderr(t, func() error { warnCodeSearch(true, "grepapp", scoped, nil); return nil })
	var diagnostic struct {
		Warning map[string]string `json:"warning"`
	}
	if err := json.Unmarshal([]byte(out), &diagnostic); err != nil {
		t.Fatalf("--json warning is not one JSON object: %q (%v)", out, err)
	}
	if w := diagnostic.Warning; w["code"] != "repo_may_be_unindexed" || w["backend"] != "grepapp" || !strings.Contains(w["message"], "facebook/react") {
		t.Errorf("empty repo warning = %v", w)
	}

	// Backends that apply qualifiers and report missing repositories
	// themselves have nothing to warn about.
	out, _ = captureStderr(t, func() error {
		warnCodeSearch(false, "sourcegraph", literal, nil)
		warnCodeSearch(false, "sourcegraph", scoped, nil)
		return nil
	})
	if out != "" {
		t.Errorf("sourcegraph warned: %q", out)
	}
}

func TestPrintCodeResultsShowsRepo(t *testing.T) {
	q := code.Query{Term: "gcControllerState", Lang: "go", Repo: "golang/go", Limit: 5}
	out := captureStdout(t, func() { printCodeResults(q, "grepapp", nil) })
	if !strings.Contains(out, "lang: go\nrepo: golang/go\nbackend: grepapp\n") {
		t.Errorf("frontmatter does not report the repository filter:\n%s", out)
	}
}
