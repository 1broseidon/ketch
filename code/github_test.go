package code_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	// Use a native executable so the fixture also works without a Unix shell.
	source := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
)

func main() {
	f, err := os.OpenFile(%q, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil { panic(err) }
	if _, err := f.WriteString("call\n"); err != nil { panic(err) }
	if err := f.Close(); err != nil { panic(err) }
	if len(os.Args) != 3 || os.Args[1] != "auth" || os.Args[2] != "token" {
		os.Exit(1)
	}
	fmt.Println("fixture-token")
}
`, count)
	sourcePath := filepath.Join(dir, "gh.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	gh := filepath.Join(dir, "gh")
	if runtime.GOOS == "windows" {
		gh += ".exe"
	}
	build := exec.Command("go", "build", "-o", gh, sourcePath)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gh fixture: %v\n%s", err, out)
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
