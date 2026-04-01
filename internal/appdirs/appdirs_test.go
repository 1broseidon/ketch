package appdirs

import (
	"path/filepath"
	"testing"
)

func TestConfigDirUsesPortableRoot(t *testing.T) {
	t.Setenv(EnvPortableRoot, filepath.Join("portable", "root"))
	t.Setenv(EnvConfigDir, "")

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}

	expected, _ := filepath.Abs(filepath.Join("portable", "root", "config"))
	if dir != expected {
		t.Fatalf("ConfigDir() = %q, want %q", dir, expected)
	}
}

func TestExplicitDirsOverridePortableRoot(t *testing.T) {
	t.Setenv(EnvPortableRoot, filepath.Join("portable", "root"))
	t.Setenv(EnvCacheDir, filepath.Join("custom", "cache"))
	t.Setenv(EnvBrowserDir, filepath.Join("custom", "browser"))
	t.Setenv(EnvStatusDir, filepath.Join("custom", "status"))

	cacheDir, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir() error = %v", err)
	}
	browserDir, err := BrowserDir()
	if err != nil {
		t.Fatalf("BrowserDir() error = %v", err)
	}
	statusDir, err := StatusDir()
	if err != nil {
		t.Fatalf("StatusDir() error = %v", err)
	}

	expectedCache, _ := filepath.Abs(filepath.Join("custom", "cache"))
	expectedBrowser, _ := filepath.Abs(filepath.Join("custom", "browser"))
	expectedStatus, _ := filepath.Abs(filepath.Join("custom", "status"))

	if cacheDir != expectedCache {
		t.Fatalf("CacheDir() = %q, want %q", cacheDir, expectedCache)
	}
	if browserDir != expectedBrowser {
		t.Fatalf("BrowserDir() = %q, want %q", browserDir, expectedBrowser)
	}
	if statusDir != expectedStatus {
		t.Fatalf("StatusDir() = %q, want %q", statusDir, expectedStatus)
	}
}
