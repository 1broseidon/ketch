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

// browserNavigator is implemented by a BrowserConn that can also say where
// a render ended up after HTTP redirects and script navigation; rodConn
// does. Callers that must know where content came from, such as the docs
// backend's scope check, read it from Page.FinalURL.
type browserNavigator interface {
	FetchFinal(ctx context.Context, url string) (html, finalURL string, err error)
}

// browserRender renders url and returns the HTML with the URL the browser
// ended on, "" when the BrowserConn cannot report it; callers then fall back
// to the URL the plain HTTP fetch ended on, which the browser's own
// navigation followed too.
func browserRender(ctx context.Context, b BrowserConn, url string) (string, string, error) {
	if nav, ok := b.(browserNavigator); ok {
		return nav.FetchFinal(ctx, url)
	}
	html, err := b.Fetch(ctx, url)
	return html, "", err
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
