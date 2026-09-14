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
	if c := cache.NewReadOnly(); c != nil {
		defer c.Close()
		entries, size := c.Stats()
		return StatusOK, fmt.Sprintf("%d entries, %s", entries, formatBytes(size))
	}
	// The database exists but a read-only open failed: another process (e.g. a
	// background crawl) holds the lock. That is healthy, just report it.
	var size int64
	if st, err := os.Stat(path); err == nil {
		size = st.Size()
	}
	return StatusOK, fmt.Sprintf("locked by another process (%s)", formatBytes(size))
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
