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

// browserNavigator is implemented by a BrowserConn that can also say where
// a render ended up after HTTP redirects and script navigation. Callers that
// must know where content came from, such as the docs backend's scope check,
// read it from Page.FinalURL.
type browserNavigator interface {
	FetchFinal(ctx context.Context, url string) (html, finalURL string, err error)
}

// rendering is one browser render of a URL: the HTML, the main document's
// HTTP status (0 when the browser cannot say) and the URL the tab ended on
// ("" when the browser cannot say).
type rendering struct {
	html     string
	status   int
	finalURL string
}

// pageRenderer is implemented by a BrowserConn that yields the HTML, status
// and landing URL from a single navigation; rodConn does.
type pageRenderer interface {
	render(ctx context.Context, url string) (rendering, error)
}

// browserRender renders url through b with whatever b can report: one
// navigation for a pageRenderer, else the richest of FetchStatus, FetchFinal
// and Fetch. A status or URL b cannot report stays 0 or "", and callers then
// fall back safely: an unknown status is trusted, and an unknown landing URL
// falls back to the URL the plain HTTP fetch ended on (which the browser's own
// navigation followed too) or stays unknown when there was none.
func browserRender(ctx context.Context, b BrowserConn, url string) (rendering, error) {
	switch c := b.(type) {
	case pageRenderer:
		return c.render(ctx, url)
	case StatusFetcher:
		html, status, err := c.FetchStatus(ctx, url)
		return rendering{html: html, status: status}, err
	case browserNavigator:
		html, finalURL, err := c.FetchFinal(ctx, url)
		return rendering{html: html, finalURL: finalURL}, err
	}
	html, err := b.Fetch(ctx, url)
	return rendering{html: html}, err
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
