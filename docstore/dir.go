// Package docstore persists local documentation libraries: pages pulled from
// a docs site, split into heading-delimited sections, and indexed with
// SQLite FTS5 for offline lexical search. It is the storage layer behind the
// `local` docs backend and the `ketch docs add/list/remove/sync` commands
// (ADR-0004).
//
// The store is a single SQLite file under Dir. Libraries are keyed by name;
// re-adding a name replaces its contents atomically. A project's
// .ketch/docs.json manifest (see Project) records which libraries belong to
// it so the same corpus can be rebuilt elsewhere with `ketch docs sync`.
package docstore

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DBFile is the SQLite file name inside the docs directory.
const DBFile = "docs.db"

// DefaultDir returns the platform data directory for local docs libraries:
// $XDG_DATA_HOME/ketch/docs (or ~/.local/share/ketch/docs) on Linux and
// other Unixes, ~/Library/Application Support/ketch/docs on macOS, and
// %LOCALAPPDATA%\ketch\docs on Windows. Docs libraries are durable content,
// not a cache, so they do not live next to the page cache.
func DefaultDir() (string, error) {
	base, err := dataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ketch", "docs"), nil
}

// Dir resolves the effective docs directory: the configured value when
// non-empty (with a leading ~ expanded), otherwise DefaultDir.
func Dir(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return DefaultDir()
	}
	if configured == "~" || strings.HasPrefix(configured, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configured = filepath.Join(home, strings.TrimPrefix(configured, "~"))
	}
	return filepath.Clean(configured), nil
}

func dataHome() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
			return dir, nil
		}
		return os.UserConfigDir()
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return dir, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share"), nil
	}
}
