package testutil

import "testing"

// SetIsolatedConfigHome prevents tests from using the user's config directory.
func SetIsolatedConfigHome(t testing.TB) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}
