package code_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/internal/configbase"
)

// Building the GitHub backend consults the gh CLI for a token. Eligibility
// and construction must share one answer and launch gh once.
func TestGitHubBuildRunsGHOnce(t *testing.T) {
	for _, k := range []string{"KETCH_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		t.Setenv(k, "")
	}
	dir := t.TempDir()
	count := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho call >> " + count + "\necho fixture-token\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	configbase.ResetGHCLICache()
	t.Cleanup(configbase.ResetGHCLICache)

	cfg := config.Defaults()
	if _, err := code.NewFromConfig(&cfg, "github"); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, err := os.ReadFile(count)
	if err != nil {
		t.Fatalf("gh was never invoked: %v", err)
	}
	if n := strings.Count(string(data), "call"); n != 1 {
		t.Fatalf("gh auth token ran %d times for one construction, want 1", n)
	}
}
