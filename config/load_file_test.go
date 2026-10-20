package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/testutil"
)

func TestLoadFileRejectsUndecodableFileWithoutEchoingValues(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     []string
	}{
		{
			name:     "http_headers value as a list",
			contents: `{"backend":"searxng","searxng_url":"https://private.example","http_headers":{"https://private.example":{"CF-Access-Client-Id":["secret-id"]}}}`,
			want:     []string{"http_headers", "must be a string, not an array"},
		},
		{
			name:     "http_headers as a list",
			contents: `{"searxng_url":"https://private.example","http_headers":["secret-id"]}`,
			want:     []string{"http_headers must be an object, not an array"},
		},
		{
			name:     "string where a number belongs",
			contents: `{"searxng_url":"https://private.example","limit":"secret-limit"}`,
			want:     []string{"limit must be a whole number, not a string"},
		},
		{
			name:     "fractional number keeps its literal out",
			contents: `{"searxng_url":"https://private.example","limit":31337.5}`,
			want:     []string{"limit must be a whole number, not a number"},
		},
		{
			name:     "number where a string belongs",
			contents: `{"searxng_url":"https://private.example","cache_ttl":31337}`,
			want:     []string{"cache_ttl must be a string, not a number"},
		},
		{
			name:     "provider key as a number",
			contents: `{"searxng_url":"https://private.example","brave_api_key":31337}`,
			want:     []string{"brave_api_key must be a string, not a number"},
		},
		{
			name:     "provider key pool as a string",
			contents: `{"searxng_url":"https://private.example","brave_api_keys":"secret-key"}`,
			want:     []string{"brave_api_keys must be an array of strings, not a string"},
		},
		{
			name:     "syntax error",
			contents: "{\n  \"searxng_url\": \"https://private.example\",\n  \"brave_api_key\": \"secret-key\",\n}\n",
			want:     []string{"invalid JSON at line 4, column 1"},
		},
		{
			name:     "not an object",
			contents: `["secret-key"]`,
			want:     []string{"must be a JSON object, not an array"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearKetchEnv(t)
			writeConfig(t, tc.contents)
			cfg, err := LoadFile()
			if err == nil {
				t.Fatal("LoadFile accepted an undecodable file")
			}
			path, _ := Path()
			if !strings.HasPrefix(err.Error(), "config file "+path+": ") {
				t.Fatalf("error = %q, want it to name the file", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %q, want it to contain %q", err, want)
				}
			}
			// An http_headers origin is a key, not a value, and may be named.
			detail := strings.TrimPrefix(err.Error(), "config file "+path+": ")
			for _, value := range []string{"secret", "31337"} {
				if strings.Contains(detail, value) {
					t.Fatalf("error = %q echoed a value", err)
				}
			}
			if cfg.String("searxng_url") != Defaults().String("searxng_url") || cfg.String("brave_api_key") != "" {
				t.Fatal("LoadFile applied part of an undecodable file")
			}
		})
	}
}

func TestLoadFileMissingFileIsDefaults(t *testing.T) {
	clearKetchEnv(t)
	t.Setenv("KETCH_CONFIG", filepath.Join(testutil.SetIsolatedConfigHome(t), "missing.json"))
	cfg, err := LoadFile()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backend != Defaults().Backend || cfg.Limit != Defaults().Limit {
		t.Fatalf("LoadFile = %+v, want defaults", cfg)
	}
}

func TestLoadFileRejectsUnreadableFile(t *testing.T) {
	clearKetchEnv(t)
	dir := filepath.Join(testutil.SetIsolatedConfigHome(t), "config.json")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KETCH_CONFIG", dir)
	if _, err := LoadFile(); err == nil || !strings.HasPrefix(err.Error(), "config file: ") {
		t.Fatalf("error = %v, want a config file read error", err)
	}
}

func TestLoadReportsUndecodableFileAndKeepsNoneOfIt(t *testing.T) {
	clearKetchEnv(t)
	writeConfig(t, `{"backend":"searxng","searxng_url":"https://private.example","limit":"7"}`)
	t.Setenv("KETCH_CACHE_TTL", "1h")
	res, err := Load()
	if err == nil || !strings.Contains(err.Error(), "limit must be a whole number, not a string") {
		t.Fatalf("error = %v, want the file's type error", err)
	}
	if res.Config.Backend != Defaults().Backend || res.Config.String("searxng_url") != Defaults().String("searxng_url") {
		t.Fatal("Load kept part of an undecodable file")
	}
	if res.Config.CacheTTL != "1h" {
		t.Fatalf("cache_ttl = %q, want the env value still applied", res.Config.CacheTTL)
	}
}
