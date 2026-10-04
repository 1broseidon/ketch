package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/testutil"
)

const headersFile = `{"backend":"searxng","http_headers":{"https://searx.example":{"CF-Access-Client-Secret":"private-value"}}}`

func writeConfig(t *testing.T, contents string) {
	t.Helper()
	path := filepath.Join(testutil.SetIsolatedConfigHome(t), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KETCH_CONFIG", path)
}

func TestLoadHTTPHeadersFromFile(t *testing.T) {
	clearKetchEnv(t)
	writeConfig(t, headersFile)
	res, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Config.HTTPHeaders["https://searx.example"]["CF-Access-Client-Secret"]; got != "private-value" {
		t.Fatalf("header = %q", got)
	}
}

func TestLoadRejectsInvalidHTTPHeadersWithoutEchoingValues(t *testing.T) {
	clearKetchEnv(t)
	writeConfig(t, `{"backend":"searxng","http_headers":{"https://searx.example":{"X-A":"private-value\r\nInjected: yes"}}}`)
	res, err := Load()
	if err == nil || !strings.Contains(err.Error(), "http_headers") {
		t.Fatalf("error = %v, want one naming http_headers", err)
	}
	if strings.Contains(err.Error(), "private-value") {
		t.Fatal("error echoed a header value")
	}
	if res.Config.HTTPHeaders != nil || res.Config.Backend != "searxng" {
		t.Fatalf("want the rest of the file kept and the invalid headers dropped: %+v", res.Config)
	}
}

func TestLoadEnvHTTPHeadersReplacesFileAndRedactsProvenance(t *testing.T) {
	clearKetchEnv(t)
	writeConfig(t, headersFile)
	t.Setenv("KETCH_HTTP_HEADERS", `{"https://firecrawl.example":{"X-B":"env-value"}}`)
	res, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Config.HTTPHeaders) != 1 || res.Config.HTTPHeaders["https://firecrawl.example"]["X-B"] != "env-value" {
		t.Fatalf("HTTPHeaders = %v, want the env value only", res.Config.HTTPHeaders)
	}
	if len(res.Overrides) != 1 || res.Overrides[0].Var != "KETCH_HTTP_HEADERS" || res.Overrides[0].Previous != "" {
		t.Fatalf("overrides = %+v, want KETCH_HTTP_HEADERS with no previous value", res.Overrides)
	}
}

func TestLoadInvalidEnvHTTPHeadersKeepsFile(t *testing.T) {
	clearKetchEnv(t)
	writeConfig(t, headersFile)
	t.Setenv("KETCH_HTTP_HEADERS", `{"https://firecrawl.example/path":{"X-B":"env-private-value"}}`)
	res, err := Load()
	if err == nil || !strings.Contains(err.Error(), "KETCH_HTTP_HEADERS") || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("error = %v, want one naming KETCH_HTTP_HEADERS without values", err)
	}
	if res.Config.HTTPHeaders["https://searx.example"] == nil {
		t.Fatal("an invalid env value replaced the file's headers")
	}
}
