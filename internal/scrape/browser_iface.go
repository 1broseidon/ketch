package scrape

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ErrNoBrowser is returned when browser rendering is needed but not configured.
var ErrNoBrowser = errors.New("no browser configured (set with: ketch config set browser chrome)")

// BrowserConn represents a connection to a headless browser for JS rendering.
type BrowserConn interface {
	// Fetch navigates to a URL and returns the rendered HTML.
	Fetch(url string) (html string, err error)
	// Close releases browser resources.
	Close()
}

// ResolveBrowserBin resolves a browser configuration value to an absolute path.
// Accepts "chrome", "chromium" (searched in PATH), or an absolute path.
func ResolveBrowserBin(configured string) (string, error) {
	if configured == "" {
		return "", errors.New("no browser configured")
	}
	if filepath.IsAbs(configured) {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("browser not found at %s: %w", configured, err)
		}
		return configured, nil
	}
	return exec.LookPath(configured)
}
