package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/scrape"
)

func TestApplyConfigSetAPIKeysRoundTrip(t *testing.T) {
	cfg := config.Defaults()
	if err := applyConfigSet(&cfg, "brave_api_keys", `["k1","k2"]`); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var loaded config.Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.BraveAPIKeys, []string{"k1", "k2"}) {
		t.Fatalf("round-tripped keys = %v", loaded.BraveAPIKeys)
	}
}

func TestApplyConfigSetAPIKeysInvalid(t *testing.T) {
	for _, value := range []string{`not-json`, `null`, `{"key":"value"}`} {
		t.Run(value, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.BraveAPIKeys = []string{"existing"}
			if err := applyConfigSet(&cfg, "brave_api_keys", value); err == nil {
				t.Fatal("expected JSON array validation error")
			}
			if !reflect.DeepEqual(cfg.BraveAPIKeys, []string{"existing"}) {
				t.Fatalf("invalid input modified keys: %v", cfg.BraveAPIKeys)
			}
		})
	}
}

func TestApplyConfigSetAPIKeysEmptyClears(t *testing.T) {
	cfg := config.Defaults()
	cfg.BraveAPIKeys = []string{"existing"}
	if err := applyConfigSet(&cfg, "brave_api_keys", `[]`); err != nil {
		t.Fatal(err)
	}
	if len(cfg.BraveAPIKeys) != 0 {
		t.Fatalf("keys = %v, want empty", cfg.BraveAPIKeys)
	}
}

func TestBuildConfigInfoReportsEffectiveKeyCountsWithoutValues(t *testing.T) {
	cfg := config.Defaults()
	cfg.BraveAPIKey = "singular-secret"
	cfg.BraveAPIKeys = []string{"plural-secret", "singular-secret"}
	info := buildConfigInfo(cfg, "/tmp/config.json")
	if !info.BraveAPIKeySet || info.BraveAPIKeysCount != 2 {
		t.Fatalf("key discovery = set:%v count:%d", info.BraveAPIKeySet, info.BraveAPIKeysCount)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"singular-secret", "plural-secret"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("config discovery exposed an API key value")
		}
	}
}

func TestRunConfigSetNeverEchoesSecrets(t *testing.T) {
	tests := []struct {
		key     string
		value   string
		secrets []string
		want    string
	}{
		{key: "brave_api_key", value: "brave-secret", secrets: []string{"brave-secret"}, want: "set brave_api_key (1 key)\n"},
		{key: "brave_api_keys", value: `["brave-one","brave-two"]`, secrets: []string{"brave-one", "brave-two"}, want: "set brave_api_keys (2 keys)\n"},
		{key: "exa_api_key", value: "exa-secret", secrets: []string{"exa-secret"}, want: "set exa_api_key (1 key)\n"},
		{key: "exa_api_keys", value: `["exa-one","exa-two"]`, secrets: []string{"exa-one", "exa-two"}, want: "set exa_api_keys (2 keys)\n"},
		{key: "firecrawl_api_key", value: "firecrawl-secret", secrets: []string{"firecrawl-secret"}, want: "set firecrawl_api_key (1 key)\n"},
		{key: "firecrawl_api_keys", value: `["firecrawl-one","firecrawl-two"]`, secrets: []string{"firecrawl-one", "firecrawl-two"}, want: "set firecrawl_api_keys (2 keys)\n"},
		{key: "keenable_api_key", value: "keenable-secret", secrets: []string{"keenable-secret"}, want: "set keenable_api_key (1 key)\n"},
		{key: "keenable_api_keys", value: `["keenable-one","keenable-two"]`, secrets: []string{"keenable-one", "keenable-two"}, want: "set keenable_api_keys (2 keys)\n"},
		{key: "tavily_api_key", value: "tavily-secret", secrets: []string{"tavily-secret"}, want: "set tavily_api_key (1 key)\n"},
		{key: "tavily_api_keys", value: `["tavily-one","tavily-two"]`, secrets: []string{"tavily-one", "tavily-two"}, want: "set tavily_api_keys (2 keys)\n"},
		{key: "context7_api_key", value: "context7-secret", secrets: []string{"context7-secret"}, want: "set context7_api_key (1 key)\n"},
		{key: "github_token", value: "github-secret", secrets: []string{"github-secret"}, want: "set github_token (1 token)\n"},
	}

	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			testutil.SetIsolatedConfigHome(t)
			output, err := captureStderr(t, func() error {
				return runConfigSet(nil, []string{test.key, test.value})
			})
			if err != nil {
				t.Fatal(err)
			}
			if output != test.want {
				t.Fatal("config set emitted an unexpected acknowledgement")
			}
			for _, secret := range test.secrets {
				if strings.Contains(output, secret) {
					t.Fatal("config set echoed a secret value")
				}
			}
		})
	}
}

func TestConfigSetAcknowledgementUsesEffectiveKeyCount(t *testing.T) {
	cfg := config.Defaults()
	cfg.BraveAPIKey = "first"
	cfg.BraveAPIKeys = []string{"first", "second", " "}
	if got := configSetAcknowledgement(cfg, "brave_api_keys", "unused"); got != "set brave_api_keys (2 keys)" {
		t.Fatalf("acknowledgement = %q, want effective de-duplicated count", got)
	}
}

func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = writer
	callErr := fn()
	os.Stderr = original
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data), callErr
}

func TestApplyConfigSetURLRewritesValidJSON(t *testing.T) {
	c := config.Defaults()
	err := applyConfigSet(&c, "url_rewrites", `[{"match":"^https?://www\\.reddit\\.com/(.*)$","replace":"https://old.reddit.com/$1"}]`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(c.URLRewrites) != 1 {
		t.Fatalf("want 1 rule, got %d", len(c.URLRewrites))
	}
	if c.URLRewrites[0].Replace != "https://old.reddit.com/$1" {
		t.Errorf("Replace mismatch: %q", c.URLRewrites[0].Replace)
	}
}

func TestApplyConfigSetFirecrawlURL(t *testing.T) {
	c := config.Defaults()
	if err := applyConfigSet(&c, "firecrawl_url", "http://localhost:3002"); err != nil {
		t.Fatal(err)
	}
	if c.FirecrawlURL != "http://localhost:3002" {
		t.Fatalf("FirecrawlURL = %q", c.FirecrawlURL)
	}
	info := buildConfigInfo(c, "/tmp/config.json")
	if info.FirecrawlURL != "http://localhost:3002" {
		t.Fatalf("discovery firecrawl_url = %q", info.FirecrawlURL)
	}
}

func TestApplyConfigSetURLRewritesInvalidJSON(t *testing.T) {
	c := config.Defaults()
	err := applyConfigSet(&c, "url_rewrites", `not json`)
	if err == nil {
		t.Fatalf("want JSON parse error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "json") {
		t.Errorf("error should mention JSON, got: %v", err)
	}
}

func TestApplyConfigSetURLRewritesInvalidRegex(t *testing.T) {
	c := config.Defaults()
	err := applyConfigSet(&c, "url_rewrites", `[{"match":"[","replace":"x"}]`)
	if err == nil {
		t.Fatalf("want regex compile error")
	}
}

func TestApplyConfigSetURLRewritesEmptyClears(t *testing.T) {
	c := config.Defaults()
	if err := applyConfigSet(&c, "url_rewrites", `[{"match":"^x$","replace":"y"}]`); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if len(c.URLRewrites) != 1 {
		t.Fatalf("setup failed: %d rules", len(c.URLRewrites))
	}
	if err := applyConfigSet(&c, "url_rewrites", `[]`); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(c.URLRewrites) != 0 {
		t.Errorf("want empty list after [] reset, got %d rules", len(c.URLRewrites))
	}
}

func TestApplyConfigSetCookieFile(t *testing.T) {
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte("example.com\tTRUE\t/\tFALSE\t0\tsid\tv\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("valid jar sets field", func(t *testing.T) {
		c := config.Defaults()
		if err := applyConfigSet(&c, "cookie_file", jar); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if c.CookieFile != jar {
			t.Fatalf("CookieFile = %q, want %q", c.CookieFile, jar)
		}
	})

	t.Run("bad path is a validation error", func(t *testing.T) {
		c := config.Defaults()
		err := applyConfigSet(&c, "cookie_file", "/nonexistent/jar.txt")
		if err == nil {
			t.Fatal("expected validation error")
		}
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitValidation {
			t.Fatalf("error = %v, want exit %d", err, ExitValidation)
		}
		if c.CookieFile != "" {
			t.Fatalf("bad path changed config to %q", c.CookieFile)
		}
	})

	t.Run("empty clears", func(t *testing.T) {
		c := config.Defaults()
		c.CookieFile = jar
		if err := applyConfigSet(&c, "cookie_file", ""); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if c.CookieFile != "" {
			t.Fatalf("CookieFile = %q, want empty", c.CookieFile)
		}
	})
}

func TestBuildConfigInfoShowsCookieFilePathNoValues(t *testing.T) {
	c := config.Defaults()
	c.CookieFile = "/home/user/cookies.txt"
	info := buildConfigInfo(c, "/tmp/config.json")
	if info.CookieFile != c.CookieFile {
		t.Fatalf("CookieFile = %q, want %q", info.CookieFile, c.CookieFile)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "cookie_file") {
		t.Fatal("discovery payload should carry the cookie_file path")
	}
}

func TestApplyConfigSetUserAgent(t *testing.T) {
	t.Run("sets override", func(t *testing.T) {
		c := config.Defaults()
		if err := applyConfigSet(&c, "user_agent", "ketch-test/1 (+https://example.com)"); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if c.UserAgent != "ketch-test/1 (+https://example.com)" {
			t.Fatalf("UserAgent = %q", c.UserAgent)
		}
	})

	t.Run("rejects control characters", func(t *testing.T) {
		c := config.Defaults()
		err := applyConfigSet(&c, "user_agent", "ketch/1\nX-Injected: 1")
		if err == nil {
			t.Fatal("expected validation error")
		}
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitValidation {
			t.Fatalf("error = %v, want exit %d", err, ExitValidation)
		}
		if c.UserAgent != "" {
			t.Fatalf("bad value changed config to %q", c.UserAgent)
		}
	})

	t.Run("empty clears", func(t *testing.T) {
		c := config.Defaults()
		c.UserAgent = "custom/1"
		if err := applyConfigSet(&c, "user_agent", ""); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if c.UserAgent != "" {
			t.Fatalf("UserAgent = %q, want empty", c.UserAgent)
		}
	})
}

func TestBuildConfigInfoShowsEffectiveUserAgent(t *testing.T) {
	t.Run("default when unset", func(t *testing.T) {
		info := buildConfigInfo(config.Defaults(), "/tmp/config.json")
		if info.UserAgent != scrape.DefaultUserAgent() {
			t.Fatalf("UserAgent = %q, want %q", info.UserAgent, scrape.DefaultUserAgent())
		}
	})

	t.Run("override when set", func(t *testing.T) {
		c := config.Defaults()
		c.UserAgent = "custom/1"
		info := buildConfigInfo(c, "/tmp/config.json")
		if info.UserAgent != "custom/1" {
			t.Fatalf("UserAgent = %q, want custom/1", info.UserAgent)
		}
	})
}

func TestApplyConfigSetUnknownKey(t *testing.T) {
	c := config.Defaults()
	err := applyConfigSet(&c, "no_such_key", "x")
	if err == nil {
		t.Fatalf("want unknown-key error")
	}
}

func TestApplyConfigSetSPAMarkersValid(t *testing.T) {
	c := config.Defaults()
	err := applyConfigSet(&c, "spa_markers", `["__next_f","data-v-app"]`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(c.SPAMarkers) != 2 {
		t.Fatalf("want 2 markers, got %d", len(c.SPAMarkers))
	}
	if c.SPAMarkers[0] != "__next_f" || c.SPAMarkers[1] != "data-v-app" {
		t.Errorf("markers mismatch: %v", c.SPAMarkers)
	}
}

func TestApplyConfigSetSPAMarkersInvalidJSON(t *testing.T) {
	c := config.Defaults()
	err := applyConfigSet(&c, "spa_markers", `not json`)
	if err == nil {
		t.Fatalf("want JSON parse error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "json") {
		t.Errorf("error should mention JSON, got: %v", err)
	}
}

func TestApplyConfigSetSPAMarkersRejectsBlank(t *testing.T) {
	c := config.Defaults()
	err := applyConfigSet(&c, "spa_markers", `["__next_f","  "]`)
	if err == nil {
		t.Fatalf("want blank-marker error")
	}
}

func TestApplyConfigSetSPAMarkersEmptyClears(t *testing.T) {
	c := config.Defaults()
	if err := applyConfigSet(&c, "spa_markers", `["__next_f"]`); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if len(c.SPAMarkers) != 1 {
		t.Fatalf("setup failed: %d markers", len(c.SPAMarkers))
	}
	if err := applyConfigSet(&c, "spa_markers", `[]`); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(c.SPAMarkers) != 0 {
		t.Errorf("want empty list after [] reset, got %d markers", len(c.SPAMarkers))
	}
}

func TestApplyConfigSetExternalPDFConverter(t *testing.T) {
	c := config.Defaults()
	if c.ExternalPDFToMDConverterTimeoutSec != 300 {
		t.Fatalf("default timeout = %d, want 300", c.ExternalPDFToMDConverterTimeoutSec)
	}
	if err := applyConfigSet(&c, "external_pdf_to_md_converter_command", "pdftotext {input} -"); err != nil {
		t.Fatalf("set command: %v", err)
	}
	if err := applyConfigSet(&c, "external_pdf_to_md_converter_timeout_sec", "45"); err != nil {
		t.Fatalf("set timeout: %v", err)
	}
	if c.ExternalPDFToMDConverterCommand != "pdftotext {input} -" || c.ExternalPDFToMDConverterTimeoutSec != 45 {
		t.Fatalf("config = %#v", c)
	}
}

func TestApplyConfigSetExternalPDFConverterRejectsInvalidCommand(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "invalid shlex", value: `converter "{input}`},
		{name: "missing placeholder", value: "converter input.pdf"},
		{name: "duplicate placeholder", value: "converter {input} --again={input}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := config.Defaults()
			c.ExternalPDFToMDConverterCommand = "existing {input}"
			err := applyConfigSet(&c, "external_pdf_to_md_converter_command", test.value)
			if err == nil {
				t.Fatal("expected validation error")
			}
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != ExitValidation {
				t.Fatalf("error = %v, want exit %d", err, ExitValidation)
			}
			if c.ExternalPDFToMDConverterCommand != "existing {input}" {
				t.Fatalf("invalid value changed config to %q", c.ExternalPDFToMDConverterCommand)
			}
		})
	}
}

func TestApplyConfigSetExternalPDFConverterEmptyClears(t *testing.T) {
	c := config.Defaults()
	c.ExternalPDFToMDConverterCommand = "converter {input}"
	if err := applyConfigSet(&c, "external_pdf_to_md_converter_command", ""); err != nil {
		t.Fatalf("clear command: %v", err)
	}
	if c.ExternalPDFToMDConverterCommand != "" {
		t.Fatalf("command = %q, want empty", c.ExternalPDFToMDConverterCommand)
	}
}

func TestApplyConfigSetExternalPDFConverterRejectsInvalidTimeout(t *testing.T) {
	for _, value := range []string{"0", "-1", "not-an-int"} {
		t.Run(value, func(t *testing.T) {
			c := config.Defaults()
			err := applyConfigSet(&c, "external_pdf_to_md_converter_timeout_sec", value)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
