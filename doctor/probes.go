package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/cookies"
	"github.com/1broseidon/ketch/scrape"
	bolterrors "go.etcd.io/bbolt/errors"
)

// checkBrowser verifies the configured browser binary actually resolves to a
// file on disk or PATH. No browser configured is a clean skip — rendering is
// optional.
func checkBrowser(configured string) (Status, string) {
	if configured == "" {
		return StatusSkipped, "not configured (browser rendering disabled; optional)"
	}
	bin, err := scrape.ResolveBrowserBin(configured)
	if err != nil {
		return StatusMisconfigured, err.Error()
	}
	return StatusOK, bin
}

// checkCookieFile loads the configured jar and reports counts only — cookie
// names and values never appear in doctor output.
func checkCookieFile(path string) (Status, string) {
	if path == "" {
		return StatusSkipped, "not configured (cookie injection disabled; optional)"
	}
	jar, err := cookies.Load(path)
	if err != nil {
		return StatusMisconfigured, fmt.Sprintf("cannot load cookie file: %v (fix the path or re-export cookies.txt)", err)
	}
	detail := fmt.Sprintf("configured (%d cookies, %d expired)", jar.Len(), jar.Expired)
	if cookiePermsLoose(path) {
		detail += "; file is group/world-readable — chmod 600"
	}
	return StatusOK, detail
}

func cookiePermsLoose(path string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	info, err := os.Stat(cookies.ExpandPath(path))
	return err == nil && info.Mode().Perm()&0o044 != 0
}

// checkCache verifies the cache directory is writable and reports entry
// count, size, and lock state via the existing read-only stats path. It never
// opens the database for writing and never touches cache entries.
func checkCache() (Status, string) {
	path, err := cache.DBPath()
	if err != nil {
		return StatusMisconfigured, fmt.Sprintf("cannot resolve cache path: %v", err)
	}
	// Writability check on the directory, not the database: a throwaway temp
	// file, removed immediately. cache.db itself is never opened for writing.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".doctor-*")
	if err != nil {
		return StatusMisconfigured, fmt.Sprintf("cache dir not writable: %v", err)
	}
	_ = tmp.Close()
	_ = os.Remove(tmp.Name())

	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return StatusOK, fmt.Sprintf("writable, empty (no cache database yet at %s)", path)
	}
	c := cache.NewReadOnly()
	if c == nil {
		return StatusMisconfigured, fmt.Sprintf("cannot prepare cache dir for %s", path)
	}
	entries, size, err := c.Usage()
	switch {
	case err == nil:
		return StatusOK, fmt.Sprintf("%d entries, %s", entries, formatBytes(size))
	case errors.Is(err, bolterrors.ErrTimeout):
		// Another process held the file for the whole wait: a long write, or a
		// ketch older than 0.18.1 that keeps it open. Healthy, just busy.
		return StatusOK, fmt.Sprintf("busy in another process (%s)", formatBytes(size))
	default:
		// The file exists but cannot be read: every scrape runs uncached.
		return StatusMisconfigured, fmt.Sprintf("unreadable: %v (remove %s to reset it)", err, path)
	}
}

// checkTags verifies the durable tag index — a file independent of the page
// cache — is openable and reports how much it holds. It never opens the
// file for writing. No bookmark ever having been saved is a clean skip:
// tagging is optional, and a fresh install has no tags.db at all.
func checkTags() (Status, string) {
	path, err := cache.TagsPath()
	if err != nil {
		return StatusMisconfigured, fmt.Sprintf("cannot resolve tag index path: %v", err)
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return StatusSkipped, fmt.Sprintf("not configured (no bookmarks saved yet; %s)", path)
	}
	tags, entries, err := cache.NewTagIndex(0, nil).TagCounts()
	if err != nil {
		return StatusMisconfigured, fmt.Sprintf("cannot open tag index: %v", err)
	}
	return StatusOK, fmt.Sprintf("%s: %d %s, %d %s", path, tags, plural(tags, "tag", "tags"), entries, plural(entries, "entry", "entries"))
}

// plural picks the singular or plural form, matching the CLI's own helper of
// the same name in cmd/ (small formatting helpers are duplicated per package
// rather than shared, the same as formatBytes below).
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// probeErrDetail compacts a transport error into a single-line detail.

// formatBytes renders a byte count in the same style as `ketch cache`.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
