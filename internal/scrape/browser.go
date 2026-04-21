package scrape

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type rodConn struct {
	browser  *rod.Browser
	launcher *launcher.Launcher
}

// NewBrowserConn launches a headless browser and returns a connection.
func NewBrowserConn(binPath string) (BrowserConn, error) {
	l := launcher.New().Bin(binPath).Headless(true)
	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}
	b := rod.New().ControlURL(u)
	if err := b.Connect(); err != nil {
		l.Kill()
		return nil, fmt.Errorf("connect browser: %w", err)
	}
	return &rodConn{browser: b, launcher: l}, nil
}

// Fetch navigates to a URL in a new tab and returns the rendered HTML.
// The context bounds navigation and JS settling; if it's cancelled, the
// underlying Rod operations unblock with the ctx error.
func (r *rodConn) Fetch(ctx context.Context, rawURL string) (string, error) {
	page, err := r.browser.Context(ctx).Page(proto.TargetCreateTarget{URL: rawURL})
	if err != nil {
		return "", fmt.Errorf("create page: %w", err)
	}
	defer func() { _ = page.Close() }()

	timedPage := page.Timeout(30 * time.Second)
	_ = timedPage.WaitLoad()
	_ = timedPage.WaitStable(time.Second)

	return page.HTML()
}

// Close shuts down the browser and cleans up.
func (r *rodConn) Close() {
	if r.browser != nil {
		_ = r.browser.Close()
	}
	if r.launcher != nil {
		r.launcher.Kill()
	}
}

// InstallBrowser downloads Chromium to the ketch cache directory.
func InstallBrowser() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache dir: %w", err)
	}
	b := launcher.NewBrowser()
	b.RootDir = filepath.Join(cacheDir, "ketch", "browser")
	if err := b.Download(); err != nil {
		return "", err
	}
	return b.BinPath(), nil
}
