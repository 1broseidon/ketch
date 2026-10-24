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
	if file, err := LoadFile(); err != nil || file.Limit != 3 || len(file.BraveKeys()) != 2 {
		t.Fatalf("LoadFile = %+v, %v", file, err)
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
	c := Config{ProviderSettings: map[string]any{"github_token": "from-file"}}
	token, source := c.ResolveGithubToken()
	if token != "from-ketch-env" || source != "env" {
		t.Fatalf("token, source = %q, %q", token, source)
	}

	os.Unsetenv("KETCH_GITHUB_TOKEN") //nolint:errcheck
	if token, source = c.ResolveGithubToken(); token != "from-file" || source != "config" {
		t.Fatalf("config should beat ambient GITHUB_TOKEN: %q, %q", token, source)
	}
	c.SetProvider("github_token", "")
	if token, source = c.ResolveGithubToken(); token != "ambient" || source != "env" {
		t.Fatalf("ambient fallback: %q, %q", token, source)
	}
}

func TestScrubbedEnviron(t *testing.T) {
	t.Setenv("KETCH_BRAVE_API_KEY", "secret")
	t.Setenv("KETCH_GITHUB_TOKEN", "secret")
	t.Setenv("KETCH_HTTP_HEADERS", `{"https://searx.example":{"X-A":"secret"}}`)
	t.Setenv("KETCH_LIMIT", "9")
	t.Setenv("UNRELATED_VAR", "keep")

	kept := map[string]bool{}
	for _, kv := range ScrubbedEnviron() {
		name, _, _ := strings.Cut(kv, "=")
		kept[name] = true
	}
	if kept["KETCH_BRAVE_API_KEY"] || kept["KETCH_GITHUB_TOKEN"] || kept["KETCH_HTTP_HEADERS"] {
		t.Fatal("secret KETCH_ vars leaked into subprocess environ")
	}
	if !kept["KETCH_LIMIT"] || !kept["UNRELATED_VAR"] {
		t.Fatal("non-secret vars were stripped")
	}
}

func TestLoadEnvMCPTools(t *testing.T) {
	clearKetchEnv(t)
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_MCP_TOOLS", "Scrape, crawl")

	res, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if tools := res.Config.MCPTools; strings.Join(tools, ",") != "scrape,crawl" {
		t.Fatalf("MCPTools = %v, want [scrape crawl]", tools)
	}
	found := false
	for _, o := range res.Overrides {
		if o.Key == "mcp_tools" && o.Var == "KETCH_MCP_TOOLS" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mcp_tools override not recorded: %+v", res.Overrides)
	}
}

func TestLoadEnvMCPToolsInvalidIsLoudButBestEffort(t *testing.T) {
	clearKetchEnv(t)
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_MCP_TOOLS", "wiki")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), `KETCH_MCP_TOOLS: unknown tool "wiki"`) {
		t.Fatalf("expected loud unknown-tool error, got: %v", err)
	}
}

func TestLoadEnvSearchOrder(t *testing.T) {
	clearKetchEnv(t)
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_SEARCH_ORDER", "Exa, tinyfish,brave")

	res, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if order := res.Config.SearchOrder; strings.Join(order, ",") != "exa,tinyfish,brave" {
		t.Fatalf("SearchOrder = %v, want [exa tinyfish brave]", order)
	}
	found := false
	for _, o := range res.Overrides {
		if o.Key == "search_order" && o.Var == "KETCH_SEARCH_ORDER" {
			found = true
		}
	}
	if !found {
		t.Fatalf("search_order override not recorded: %+v", res.Overrides)
	}
}

func TestLoadEnvSearchOrderInvalidIsLoud(t *testing.T) {
	for value, want := range map[string]string{
		"exa,auto":  `KETCH_SEARCH_ORDER: "auto"`,
		"google":    `KETCH_SEARCH_ORDER: unknown backend "google"`,
		"ddg, DDG ": `KETCH_SEARCH_ORDER: duplicate backend "ddg"`,
	} {
		t.Run(value, func(t *testing.T) {
			clearKetchEnv(t)
			testutil.SetIsolatedConfigHome(t)
			t.Setenv("KETCH_SEARCH_ORDER", value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %q, got: %v", want, err)
			}
		})
	}
}

func TestLoadEnvSearchOrderBlankInputPreservesFile(t *testing.T) {
	for _, tt := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "empty_is_unset", value: ""},
		{name: "leading_blank", value: ",exa", wantErr: true},
		{name: "trailing_blank", value: "exa,", wantErr: true},
		{name: "interior_blank", value: "exa,,ddg", wantErr: true},
		{name: "leading_whitespace", value: " \t,exa", wantErr: true},
		{name: "trailing_whitespace", value: "exa, \t", wantErr: true},
		{name: "interior_whitespace", value: "exa, \t,ddg", wantErr: true},
		{name: "comma_only", value: ",", wantErr: true},
		{name: "commas_only", value: ",,,", wantErr: true},
		{name: "whitespace_only", value: " \t\n ", wantErr: true},
		{name: "blank_fields_only", value: " , \t, ", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearKetchEnv(t)
			path := filepath.Join(t.TempDir(), "config.json")
			const fileConfig = `{"search_order": ["searxng"], "searxng_url": "http://127.0.0.1:8081"}`
			if err := os.WriteFile(path, []byte(fileConfig), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("KETCH_CONFIG", path)
			t.Setenv("KETCH_SEARCH_ORDER", tt.value)

			res, err := Load()
			if tt.wantErr {
				const want = "KETCH_SEARCH_ORDER: backend name is blank"
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("expected %q, got: %v", want, err)
				}
			} else if err != nil {
				t.Fatalf("empty env value should be unset, got: %v", err)
			}
			if want := []string{"searxng"}; !reflect.DeepEqual(res.Config.SearchOrder, want) {
				t.Fatalf("SearchOrder = %v, want file-pinned order %v", res.Config.SearchOrder, want)
			}
			if len(res.Overrides) != 0 {
				t.Fatalf("blank env input was recorded as an override: %+v", res.Overrides)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != fileConfig {
				t.Fatalf("config file changed: %s", data)
			}
		})
	}
}

func TestLoadEnvExtractMode(t *testing.T) {
	clearKetchEnv(t)
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_EXTRACT_MODE", " Clean ")

	res, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if res.Config.ExtractMode != "clean" {
		t.Fatalf("ExtractMode = %q, want clean", res.Config.ExtractMode)
	}
	found := false
	for _, o := range res.Overrides {
		if o.Key == "extract_mode" && o.Var == "KETCH_EXTRACT_MODE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extract_mode override not recorded: %+v", res.Overrides)
	}
}

func TestLoadEnvExtractModeInvalidIsLoud(t *testing.T) {
	clearKetchEnv(t)
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_EXTRACT_MODE", "fast")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), `KETCH_EXTRACT_MODE: unknown extract_mode "fast"`) {
		t.Fatalf("expected loud unknown-mode error, got: %v", err)
	}
}
