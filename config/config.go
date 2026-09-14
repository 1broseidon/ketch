package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/search"
)

// Config is the public configuration model.
type Config = configbase.Config

func Defaults() Config { return configbase.Defaults().WithSettings(ProviderSettings()) }

func AvailableBackends() []string { return search.AvailableBackends() }

// AvailableCodeBackends returns the list of known code search backends.
func AvailableCodeBackends() []string { return code.AvailableBackends() }

// AvailableDocBackends returns the list of usable docs backends. The local
// FTS5 backend is planned but not implemented, so it is not advertised here;
// docs.NewFromConfig still recognizes "local" and rejects it with a clear
// precondition error.
func AvailableDocBackends() []string { return docs.AvailableBackends() }

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
	cfg = cfg.WithSettings(ProviderSettings())
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

const DefaultFirecrawlURL = configbase.DefaultFirecrawlURL

func FirecrawlSearchURL(base string) string { return configbase.FirecrawlSearchURL(base) }

func mergeKeys(single string, list []string) []string { return configbase.MergeKeys(single, list) }
