package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/testutil"
)

// clearKetchEnv unsets every KETCH_* variable for the test so ambient
// developer environment can't leak into assertions.
func clearKetchEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, EnvPrefix) {
			t.Setenv(name, "")
			os.Unsetenv(name) //nolint:errcheck // t.Setenv registered the restore
		}
	}
}

func TestEnvVarNaming(t *testing.T) {
	if got := EnvVar("brave_api_key"); got != "KETCH_BRAVE_API_KEY" {
		t.Fatalf("EnvVar = %q", got)
	}
}

func TestLoadEnvOverlay(t *testing.T) {
	clearKetchEnv(t)
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_BACKEND", "ddg")
	t.Setenv("KETCH_LIMIT", "9")
	t.Setenv("KETCH_CACHE_TTL", "30m")
	t.Setenv("KETCH_BRAVE_API_KEY", "one, two ,two")

	res, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c := res.Config
	if c.Backend != "ddg" || c.Limit != 9 || c.CacheTTL != "30m" {
		t.Fatalf("config = %+v", c)
	}
	if got := c.BraveKeys(); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("BraveKeys = %v", got)
	}

	byKey := map[string]Override{}
	for _, o := range res.Overrides {
		byKey[o.Key] = o
	}
	if len(byKey) != 4 {
		t.Fatalf("overrides = %+v", res.Overrides)
	}
	if o := byKey["limit"]; o.Var != "KETCH_LIMIT" || o.Previous != "5" {
		t.Fatalf("limit override = %+v", o)
	}
	if o := byKey["brave_api_key"]; o.Previous != "" {
		t.Fatalf("secret override leaked previous value: %+v", o)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	clearKetchEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"limit": 3, "backend": "exa", "brave_api_keys": ["file-a", "file-b"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KETCH_CONFIG", path)
	t.Setenv("KETCH_LIMIT", "7")
	t.Setenv("KETCH_BRAVE_API_KEY", "env-only")

	res, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if res.Config.Limit != 7 {
		t.Fatalf("limit = %d, want env value 7", res.Config.Limit)
	}
	if res.Config.Backend != "exa" {
		t.Fatalf("backend = %q, want file value exa", res.Config.Backend)
	}
	// The singular env var replaces the whole effective pool.
	if got := res.Config.BraveKeys(); !reflect.DeepEqual(got, []string{"env-only"}) {
		t.Fatalf("BraveKeys = %v", got)
	}
	// LoadFile ignores env entirely.
	if file := LoadFile(); file.Limit != 3 || len(file.BraveKeys()) != 2 {
		t.Fatalf("LoadFile = %+v", file)
	}
}

func TestLoadInvalidEnvIsLoudButBestEffort(t *testing.T) {
	clearKetchEnv(t)
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_LIMIT", "abc")
	t.Setenv("KETCH_CACHE_TTL", "nope")
	t.Setenv("KETCH_BACKEND", "ddg")

	res, err := Load()
	if err == nil {
		t.Fatal("expected an error for invalid env values")
	}
	for _, want := range []string{"KETCH_LIMIT", "KETCH_CACHE_TTL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	// Valid vars still applied; invalid ones fell back to defaults.
	if res.Config.Backend != "ddg" || res.Config.Limit != 5 || res.Config.CacheTTL != "72h" {
		t.Fatalf("best-effort config = %+v", res.Config)
	}
}

func TestLoadEmptyEnvValueIsUnset(t *testing.T) {
	clearKetchEnv(t)
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_LIMIT", "")

	res, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if res.Config.Limit != 5 || len(res.Overrides) != 0 {
		t.Fatalf("empty env var was applied: %+v %+v", res.Config, res.Overrides)
	}
}

func TestKetchConfigPathEscapeHatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alt.json")
	t.Setenv("KETCH_CONFIG", path)
	got, err := Path()
	if err != nil || got != path {
		t.Fatalf("Path() = %q, %v", got, err)
	}
	// Save writes to the alternate path too.
	if err := Save(Defaults()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestResolveGithubTokenKetchEnvWinsOverConfig(t *testing.T) {
	t.Setenv("KETCH_GITHUB_TOKEN", "from-ketch-env")
	t.Setenv("GITHUB_TOKEN", "ambient")
	c := Config{GithubToken: "from-file"}
	token, source := c.ResolveGithubToken()
	if token != "from-ketch-env" || source != "env" {
		t.Fatalf("token, source = %q, %q", token, source)
	}

	os.Unsetenv("KETCH_GITHUB_TOKEN") //nolint:errcheck
	if token, source = c.ResolveGithubToken(); token != "from-file" || source != "config" {
		t.Fatalf("config should beat ambient GITHUB_TOKEN: %q, %q", token, source)
	}

	c.GithubToken = ""
	if token, source = c.ResolveGithubToken(); token != "ambient" || source != "env" {
		t.Fatalf("ambient fallback: %q, %q", token, source)
	}
}

func TestScrubbedEnviron(t *testing.T) {
	t.Setenv("KETCH_BRAVE_API_KEY", "secret")
	t.Setenv("KETCH_GITHUB_TOKEN", "secret")
	t.Setenv("KETCH_LIMIT", "9")
	t.Setenv("UNRELATED_VAR", "keep")

	kept := map[string]bool{}
	for _, kv := range ScrubbedEnviron() {
		name, _, _ := strings.Cut(kv, "=")
		kept[name] = true
	}
	if kept["KETCH_BRAVE_API_KEY"] || kept["KETCH_GITHUB_TOKEN"] {
		t.Fatal("secret KETCH_ vars leaked into subprocess environ")
	}
	if !kept["KETCH_LIMIT"] || !kept["UNRELATED_VAR"] {
		t.Fatal("non-secret vars were stripped")
	}
}
