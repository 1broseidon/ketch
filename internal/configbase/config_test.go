package configbase

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func clearGitHubEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"KETCH_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		t.Setenv(k, "")
	}
	t.Setenv("PATH", t.TempDir())
	ResetGHCLICache()
	t.Cleanup(ResetGHCLICache)
}

// One CLI invocation asks for the token several times (eligibility,
// construction, discovery, doctor). They must all see one answer and pay for
// one subprocess.
func TestGHCLITokenIsResolvedOncePerWindow(t *testing.T) {
	clearGitHubEnv(t)
	calls := 0
	ghCLI.run = func() (string, error) { calls++; return "tok-" + string(rune('0'+calls)), nil }
	t.Cleanup(func() { ghCLI.run = nil })

	var c Config
	a, srcA := c.ResolveGithubToken()
	b, srcB := c.ResolveGithubToken()
	if calls != 1 || a != "tok-1" || b != "tok-1" || srcA != "gh-cli" || srcB != "gh-cli" {
		t.Fatalf("calls=%d a=%q b=%q sources=%q/%q; want one gh run and one consistent token", calls, a, b, srcA, srcB)
	}

	ghCLI.Lock()
	ghCLI.checked = time.Now().Add(-ghCLITokenTTL - time.Second)
	ghCLI.Unlock()
	if tok, _ := c.ResolveGithubToken(); calls != 2 || tok != "tok-2" {
		t.Fatalf("after TTL: calls=%d tok=%q, want a fresh gh run", calls, tok)
	}
}

func TestGHCLIFailureIsCachedAndEnvStillWins(t *testing.T) {
	clearGitHubEnv(t)
	calls := 0
	ghCLI.run = func() (string, error) { calls++; return "", errors.New("not logged in") }
	t.Cleanup(func() { ghCLI.run = nil })
	var c Config
	for i := 0; i < 3; i++ {
		if tok, src := c.ResolveGithubToken(); tok != "" || src != "none" {
			t.Fatalf("unexpected token %q from %s", tok, src)
		}
	}
	if calls != 1 {
		t.Fatalf("gh failure ran %d times, want 1 (cached)", calls)
	}
	t.Setenv("GITHUB_TOKEN", "env-tok")
	if tok, src := c.ResolveGithubToken(); tok != "env-tok" || src != "env" {
		t.Fatalf("env token must bypass the gh cache: %q %s", tok, src)
	}
}

// encoding/json matched struct tags case-insensitively, so hand-edited configs
// with upper- or mixed-case provider keys loaded before the registry and must
// keep loading. Exact matches win over case variants.
func TestProviderKeysMatchCaseInsensitively(t *testing.T) {
	schema := []Setting{KeyPool("brave_api_key", "brave_api_keys"), {Key: "searxng_url"}}
	cfg := Defaults().WithSettings(schema)
	in := `{"backend":"brave","BRAVE_API_KEY":"loud","Brave_API_Keys":["a","b"],"Searxng_URL":"http://fixture.local"}`
	if err := json.Unmarshal([]byte(in), &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.BraveKeys(); len(got) != 3 || got[0] != "loud" {
		t.Fatalf("BraveKeys = %v, want [loud a b]", got)
	}
	if cfg.String("searxng_url") != "http://fixture.local" {
		t.Fatalf("searxng_url = %q", cfg.String("searxng_url"))
	}
	cfg = Defaults().WithSettings(schema)
	if err := json.Unmarshal([]byte(`{"BRAVE_API_KEY":"loud","brave_api_key":"exact"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.String("brave_api_key") != "exact" {
		t.Fatalf("exact key must win over a case variant, got %q", cfg.String("brave_api_key"))
	}
}
