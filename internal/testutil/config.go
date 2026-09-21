package testutil

import (
	"path/filepath"
	"testing"
)

// SetIsolatedConfigHome prevents tests from using the user's config directory.
func SetIsolatedConfigHome(t testing.TB) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("KETCH_TAGS_PATH", filepath.Join(dir, "tags.db"))
	return dir
}
