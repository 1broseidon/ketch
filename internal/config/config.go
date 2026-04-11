package config

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config holds user-configurable defaults for ketch.
type Config struct {
	Backend        string `json:"backend"`
	SearxngURL     string `json:"searxng_url"`
	BraveAPIKey    string `json:"brave_api_key,omitempty"`
	Limit          int    `json:"limit"`
	CacheTTL       string `json:"cache_ttl"`
	Browser        string `json:"browser,omitempty"` // "chrome", "chromium", or absolute path; empty = disabled
	CodeBackend    string `json:"code_backend,omitempty"`
	DocsBackend    string `json:"docs_backend,omitempty"`
	Context7APIKey string `json:"context7_api_key,omitempty"`
	SourcegraphURL string `json:"sourcegraph_url,omitempty"`
	GithubToken    string `json:"github_token,omitempty"`
}

// ResolveGithubToken returns a token and the source it came from, walking the
// resolution chain: explicit config → $GITHUB_TOKEN → $GH_TOKEN → `gh auth token`.
// Source is one of: "config", "env", "gh-cli", "none". The token is never logged.
func (c Config) ResolveGithubToken() (token, source string) {
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
		Backend:        "brave",
		SearxngURL:     "http://localhost:8081",
		Limit:          5,
		CacheTTL:       "72h",
		CodeBackend:    "sourcegraph",
		DocsBackend:    "context7",
		SourcegraphURL: "https://sourcegraph.com",
	}
}

// AvailableBackends returns the list of known search backends.
func AvailableBackends() []string {
	return []string{"brave", "ddg", "searxng"}
}

// AvailableCodeBackends returns the list of known code search backends.
func AvailableCodeBackends() []string { return []string{"sourcegraph", "github"} }

// AvailableDocBackends returns the list of known docs backends.
func AvailableDocBackends() []string { return []string{"context7", "local"} }

// Path returns the config file path (~/.config/ketch/config.json).
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ketch", "config.json"), nil
}

// Load reads the config file, falling back to defaults for missing fields.
func Load() Config {
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

	return os.WriteFile(path, append(data, '\n'), 0o644)
}
