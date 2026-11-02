package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/search"
)

// Config is the public configuration model.
type Config = configbase.Config

func Defaults() Config { return configbase.Defaults().WithSettings(ProviderSettings()) }

// AvailableBackends returns the implemented search providers. It excludes the
// "auto" chain, which selects among these rather than being one of them — use
// SelectableBackends for anything a user may pass to --backend.
func AvailableBackends() []string { return search.AvailableBackends() }

// SelectableBackends returns every value --backend and the `backend` config key
// accept: the auto chain first, then the providers in registry order.
func SelectableBackends() []string { return search.SelectableBackends() }

// NormalizeSearchOrder validates an operator-configured search_order against
// the search provider registry, preserving the operator's order.
func NormalizeSearchOrder(list []string) ([]string, error) { return search.NormalizeSearchOrder(list) }

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
//
// A file that exists but cannot be read or decoded is an error, never a
// silent reset to defaults: a hand edit with one wrong type would otherwise
// drop every other setting, such as a private searxng_url, and send searches
// to a public backend. The error names the offending key and the type it
// needs, never the value. On error the returned config is Defaults(), for
// display only (flag defaults); nothing should act on it.
func LoadFile() (Config, error) {
	cfg := Defaults()

	path, err := Path()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Defaults(), fmt.Errorf("config file: %w", err)
	}

	// Unmarshal over defaults — only set fields get overwritten.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Defaults(), fmt.Errorf("config file %s: %s", path, decodeErrorDetail(data, err))
	}
	return cfg, nil
}

// decodeErrorDetail describes why the config file failed to decode without
// quoting any of its contents: values may be secrets, and encoding/json puts
// a number's literal in its type error.
func decodeErrorDetail(data []byte, err error) string {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		got, _, _ := strings.Cut(typeErr.Value, " ")
		if typeErr.Field == "" {
			return fmt.Sprintf("must be a JSON object, not %s", jsonKind(got))
		}
		return fmt.Sprintf("%s must be %s, not %s", fieldPath(typeErr.Field), expectedType(typeErr.Type), jsonKind(got))
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		line, col := position(data, syntaxErr.Offset)
		return fmt.Sprintf("invalid JSON at line %d, column %d", line, col)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "invalid JSON: unexpected end of file"
	}
	return "invalid JSON"
}

// fieldPath turns encoding/json's dotted field path back into config keys.
// Newer Go escapes "/" and "~" inside a map key (an http_headers origin) the
// JSON Pointer way.
func fieldPath(field string) string {
	return strings.NewReplacer("~1", "/", "~0", "~").Replace(field)
}

func expectedType(t reflect.Type) string {
	if t == nil {
		return "a different type"
	}
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "a whole number"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.String {
			return "an array of strings"
		}
		return "an array"
	case reflect.Map, reflect.Struct:
		return "an object"
	default:
		return "a different type"
	}
}

func jsonKind(kind string) string {
	switch kind {
	case "string", "number":
		return "a " + kind
	case "array", "object":
		return "an " + kind
	case "bool":
		return "true or false"
	default:
		return "a different type"
	}
}

// position converts a byte offset into a 1-based line and column.
func position(data []byte, offset int64) (line, col int) {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	before := data[:offset]
	line = bytes.Count(before, []byte("\n")) + 1
	col = int(offset) - (bytes.LastIndexByte(before, '\n') + 1)
	if col < 1 {
		col = 1
	}
	return line, col
}

// Load returns the effective config: file values overlaid with KETCH_*
// environment variables (precedence: env > file > default), plus the
// provenance of every env override. On invalid env values it returns a
// best-effort config (valid vars applied, invalid ones skipped) alongside a
// descriptive error; callers decide when to surface it. An invalid
// http_headers is reported the same way and dropped, so no request sends it.
// A config file that cannot be read or decoded is reported too, and none of
// it is applied.
func Load() (LoadResult, error) {
	cfg, fileErr := LoadFile()
	overrides, err := applyEnv(&cfg)
	err = errors.Join(fileErr, err)
	if headerErr := httpx.ValidateOriginHeaders(cfg.HTTPHeaders); headerErr != nil {
		cfg.HTTPHeaders = nil
		err = errors.Join(err, fmt.Errorf("http_headers: %w", headerErr))
	}
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
