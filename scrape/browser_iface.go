package scrape

import (
	"context"
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
	// Fetch navigates to a URL and returns the rendered HTML. The context
	// bounds navigation and JS settling; cancellation unblocks the caller.
	Fetch(ctx context.Context, url string) (html string, err error)
	// Close releases browser resources.
	Close()
}

// StatusFetcher is implemented by a BrowserConn that can also report the HTTP
// status of the page's main document. Browsers render error pages like any
// other, so Fetch alone cannot tell a page from an access-denied wall; callers
// that must tell them apart check for this interface. A status of 0 means the
// browser could not determine it.
type StatusFetcher interface {
	FetchStatus(ctx context.Context, url string) (html string, status int, err error)
}

// ContentFetcher optionally exposes the original main-document response through
// the browser, with its HTTP status. Unlike Fetch/FetchStatus it must not return
// a rendered HTML/XML viewer DOM. Used for 403 retries of sitemap XML.
type ContentFetcher interface {
	FetchContent(ctx context.Context, url string) (*FetchedContent, int, error)
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
