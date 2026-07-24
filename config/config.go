package config

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/1broseidon/ketch/urlrewrite"
)

// Config holds user-configurable defaults for ketch.
type Config struct {
	Backend                            string            `json:"backend"`
	SearxngURL                         string            `json:"searxng_url"`
	BraveAPIKey                        string            `json:"brave_api_key,omitempty"`
	BraveAPIKeys                       []string          `json:"brave_api_keys,omitempty"`
	ExaAPIKey                          string            `json:"exa_api_key,omitempty"`
	ExaAPIKeys                         []string          `json:"exa_api_keys,omitempty"`
	FirecrawlAPIKey                    string            `json:"firecrawl_api_key,omitempty"`
	FirecrawlAPIKeys                   []string          `json:"firecrawl_api_keys,omitempty"`
	KeenableAPIKey                     string            `json:"keenable_api_key,omitempty"`
	KeenableAPIKeys                    []string          `json:"keenable_api_keys,omitempty"`
	Limit                              int               `json:"limit"`
	CacheTTL                           string            `json:"cache_ttl"`
	Browser                            string            `json:"browser,omitempty"` // "chrome", "chromium", or absolute path; empty = disabled
	CodeBackend                        string            `json:"code_backend,omitempty"`
	DocsBackend                        string            `json:"docs_backend,omitempty"`
	Context7APIKey                     string            `json:"context7_api_key,omitempty"`
	SourcegraphURL                     string            `json:"sourcegraph_url,omitempty"`
	GithubToken                        string            `json:"github_token,omitempty"`
	URLRewrites                        []urlrewrite.Rule `json:"url_rewrites,omitempty"`
	SPAMarkers                         []string          `json:"spa_markers,omitempty"`
	CookieFile                         string            `json:"cookie_file,omitempty"` // Netscape cookies.txt path; empty = disabled
	UserAgent                          string            `json:"user_agent,omitempty"`  // HTTP User-Agent override; empty = built-in honest default
	ExternalPDFToMDConverterCommand    string            `json:"external_pdf_to_md_converter_command,omitempty"`
	ExternalPDFToMDConverterTimeoutSec int               `json:"external_pdf_to_md_converter_timeout_sec"`
}

// mergeKeys builds an effective key pool with the legacy singular key first.
// It trims whitespace, drops blank entries, de-duplicates while preserving
// order, and returns a fresh slice.
func mergeKeys(single string, list []string) []string {
	merged := make([]string, 0, len(list)+1)
	seen := make(map[string]struct{}, len(list)+1)
	for _, key := range append([]string{single}, list...) {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, key)
	}
	return merged
}

// BraveKeys returns an immutable copy of the effective Brave API key pool.
func (c Config) BraveKeys() []string { return mergeKeys(c.BraveAPIKey, c.BraveAPIKeys) }

// ExaKeys returns an immutable copy of the effective Exa API key pool.
func (c Config) ExaKeys() []string { return mergeKeys(c.ExaAPIKey, c.ExaAPIKeys) }

// FirecrawlKeys returns an immutable copy of the effective Firecrawl API key pool.
func (c Config) FirecrawlKeys() []string {
	return mergeKeys(c.FirecrawlAPIKey, c.FirecrawlAPIKeys)
}

// KeenableKeys returns an immutable copy of the effective Keenable API key pool.
func (c Config) KeenableKeys() []string { return mergeKeys(c.KeenableAPIKey, c.KeenableAPIKeys) }

// ResolveGithubToken returns a token and the source it came from, walking the
// resolution chain: $KETCH_GITHUB_TOKEN → explicit config → $GITHUB_TOKEN →
// $GH_TOKEN → `gh auth token`. github_token is deliberately excluded from the
// generic env overlay so this chain stays the single owner of its precedence.
// Source is one of: "config", "env", "gh-cli", "none". The token is never logged.
func (c Config) ResolveGithubToken() (token, source string) {
	if t := os.Getenv("KETCH_GITHUB_TOKEN"); t != "" {
		return t, "env"
	}
	if c.GithubToken != "" {
		return c.GithubToken, "config"
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t, "env"
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t, "env"
	}
	if _, err := exec.LookPath("gh"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
		if err == nil {
			if t := strings.TrimSpace(string(out)); t != "" {
				return t, "gh-cli"
			}
		}
	}
	return "", "none"
}

// Defaults returns the built-in default configuration.
func Defaults() Config {
	return Config{
		Backend:                            "brave",
		SearxngURL:                         "http://localhost:8081",
		Limit:                              5,
		CacheTTL:                           "72h",
		CodeBackend:                        "grepapp",
		DocsBackend:                        "context7",
		SourcegraphURL:                     "https://sourcegraph.com",
		ExternalPDFToMDConverterTimeoutSec: 300,
	}
}

// AvailableBackends returns the list of known search backends.
func AvailableBackends() []string {
	return []string{"brave", "ddg", "searxng", "exa", "firecrawl", "keenable"}
}

// AvailableCodeBackends returns the list of known code search backends.
func AvailableCodeBackends() []string { return []string{"grepapp", "sourcegraph", "github"} }

// AvailableDocBackends returns the list of usable docs backends. The local
// FTS5 backend is planned but not implemented, so it is not advertised here;
// docs.NewFromConfig still recognizes "local" and rejects it with a clear
// precondition error.
func AvailableDocBackends() []string { return []string{"context7"} }

// Path returns the config file path: $KETCH_CONFIG if set, otherwise
// ~/.config/ketch/config.json. KETCH_CONFIG redirects reads and writes
// (config set) alike.
func Path() (string, error) {
	if p := os.Getenv("KETCH_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ketch", "config.json"), nil
}

// LoadFile reads the config file only (no env overlay), falling back to
// defaults for missing fields. `ketch config set` reads and writes through
// LoadFile so env-derived values are never persisted into the file.
func LoadFile() Config {
	cfg := Defaults()

	path, err := Path()
	if err != nil {
		return cfg
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	// Unmarshal over defaults — only set fields get overwritten.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Defaults()
	}
	return cfg
}

// Load returns the effective config: file values overlaid with KETCH_*
// environment variables (precedence: env > file > default), plus the
// provenance of every env override. On invalid env values it returns a
// best-effort config (valid vars applied, invalid ones skipped) alongside a
// descriptive error; callers decide when to surface it.
func Load() (LoadResult, error) {
	cfg := LoadFile()
	overrides, err := applyEnv(&cfg)
	return LoadResult{Config: cfg, Overrides: overrides}, err
}

// Save writes the config to disk, creating the directory if needed.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// OpenFile preserves the mode of an existing file, so tighten it explicitly
	// before writing credentials.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
