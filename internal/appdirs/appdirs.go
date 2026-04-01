package appdirs

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvPortableRoot = "KETCH_PORTABLE_ROOT"
	EnvConfigDir    = "KETCH_CONFIG_DIR"
	EnvCacheDir     = "KETCH_CACHE_DIR"
	EnvBrowserDir   = "KETCH_BROWSER_DIR"
	EnvStatusDir    = "KETCH_STATUS_DIR"
)

func PortableRoot() string {
	value := strings.TrimSpace(os.Getenv(EnvPortableRoot))
	if value == "" {
		return ""
	}
	return resolve(value)
}

func ConfigDir() (string, error) {
	if dir := envDir(EnvConfigDir); dir != "" {
		return dir, nil
	}
	if root := PortableRoot(); root != "" {
		return filepath.Join(root, "config"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ketch"), nil
}

func CacheDir() (string, error) {
	if dir := envDir(EnvCacheDir); dir != "" {
		return dir, nil
	}
	if root := PortableRoot(); root != "" {
		return filepath.Join(root, "cache"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ketch"), nil
}

func BrowserDir() (string, error) {
	if dir := envDir(EnvBrowserDir); dir != "" {
		return dir, nil
	}
	cacheDir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "browser"), nil
}

func StatusDir() (string, error) {
	if dir := envDir(EnvStatusDir); dir != "" {
		return dir, nil
	}
	cacheDir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "crawls"), nil
}

func envDir(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return ""
	}
	return resolve(value)
}

func resolve(pathValue string) string {
	abs, err := filepath.Abs(pathValue)
	if err != nil {
		return filepath.Clean(pathValue)
	}
	return abs
}
