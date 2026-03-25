package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds user-configurable defaults for ketch.
type Config struct {
	Backend     string `json:"backend"`
	SearxngURL  string `json:"searxng_url"`
	BraveAPIKey string `json:"brave_api_key,omitempty"`
	Limit       int    `json:"limit"`
	CacheTTL    string `json:"cache_ttl"`
}

// Defaults returns the built-in default configuration.
func Defaults() Config {
	return Config{
		Backend:    "brave",
		SearxngURL: "http://localhost:8081",
		Limit:      5,
		CacheTTL:   "1h",
	}
}

// AvailableBackends returns the list of known search backends.
func AvailableBackends() []string {
	return []string{"brave", "ddg", "searxng"}
}

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
