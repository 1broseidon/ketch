package scrape

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/cookies"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/devices"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type rodConn struct {
	browser  *rod.Browser
	launcher *launcher.Launcher
	jar      *cookies.Jar
}

// ConnOption customizes a browser connection before the browser launches.
type ConnOption func(*launcher.Launcher)

// WithUserAgent launches the browser with the given User-Agent (Chrome's
// --user-agent switch), in effect for every page, frame, and worker the
// connection serves. An empty value is a no-op: the browser keeps its own
// User-Agent with only the Headless token removed (see stripHeadlessUA).
func WithUserAgent(ua string) ConnOption {
	return func(l *launcher.Launcher) {
		if ua != "" {
			l.Set("user-agent", ua)
		}
	}
}

// headlessToken is the product-name prefix headless Chromium advertises in
// its User-Agent (e.g. "HeadlessChrome/151.0.0.0"). It is a strong bot
// signal: Akamai and similar filters answer a hard 403 to it, which made
// --force-browser unusable on exactly the sites it exists for (#45).
const headlessToken = "HeadlessChrome/"

// stripHeadlessUA returns the browser's User-Agent as it would read in normal
// (non-headless) mode: the "Headless" prefix on the Chrome product token is
// removed and nothing else changes. Version, platform, and the sec-ch-ua
// client hints all stay truthful — the browser presents as itself, not as
// some other browser. The second return reports whether anything changed, so
// a UA without the token (a non-Chromium build, or one already overridden)
// is left alone.
func stripHeadlessUA(ua string) (string, bool) {
	if !strings.Contains(ua, headlessToken) {
		return ua, false
	}
	return strings.Replace(ua, headlessToken, "Chrome/", 1), true
}

// NewBrowserConn launches a headless browser without cookie injection. This
// legacy signature is preserved for external package callers.
func NewBrowserConn(binPath string) (BrowserConn, error) {
	return NewBrowserConnOptions(binPath, nil)
}

// NewBrowserConnWithCookies launches a headless browser and injects cookies
// from jar (which may be nil) before each navigation. The signature is kept
// exact (not variadic) so existing assignments to the function type keep
// compiling; use NewBrowserConnOptions for launch-time customization.
func NewBrowserConnWithCookies(binPath string, jar *cookies.Jar) (BrowserConn, error) {
	return NewBrowserConnOptions(binPath, jar)
}

// NewBrowserConnOptions launches a headless browser, injects cookies from jar
// (which may be nil) before each navigation, and applies opts to the launcher
// before launch.
//
// When no option sets a User-Agent, the browser's own UA is read after launch
// and, if it carries the HeadlessChrome token, the browser is relaunched once
// with that same UA minus the Headless prefix (stripHeadlessUA). Doing this
// through the launch switch rather than a per-page CDP override keeps the
// coverage identical to a configured user_agent: every page, frame, and
// worker. The extra launch costs a fraction of a second once per process.
func NewBrowserConnOptions(binPath string, jar *cookies.Jar, opts ...ConnOption) (BrowserConn, error) {
	l, b, err := launchBrowser(binPath, opts)
	if err != nil {
		return nil, err
	}
	if l.Has("user-agent") {
		return &rodConn{browser: b, launcher: l, jar: jar}, nil
	}

	ua, err := browserUserAgent(b)
	if err != nil {
		closeBrowser(b, l)
		return nil, err
	}
	stripped, changed := stripHeadlessUA(ua)
	if !changed {
		return &rodConn{browser: b, launcher: l, jar: jar}, nil
	}
	closeBrowser(b, l)
	l, b, err = launchBrowser(binPath, append(opts, WithUserAgent(stripped)))
	if err != nil {
		return nil, err
	}
	return &rodConn{browser: b, launcher: l, jar: jar}, nil
}

// launchBrowser starts a headless browser with opts applied and connects to
// it, returning the launcher and connected browser.
func launchBrowser(binPath string, opts []ConnOption) (*launcher.Launcher, *rod.Browser, error) {
	// Scrub KETCH_* secret vars (API keys, tokens) from the browser's
	// environment — the child process has no use for ketch credentials.
	l := launcher.New().Bin(binPath).Headless(true).Env(config.ScrubbedEnviron()...)
	for _, opt := range opts {
		opt(l)
	}
	u, err := l.Launch()
	if err != nil {
		return nil, nil, fmt.Errorf("launch browser: %w", err)
	}
	// Rod emulates devices.LaptopWithMDPIScreen by default, which overrides the
	// user agent with a hardcoded macOS Chrome 114 string. That advertises a
	// stale, widely-blocklisted build on a platform we're usually not running,
	// and bot filters answer 403. Clear the emulation so pages see the real
	// browser instead.
	b := rod.New().ControlURL(u).DefaultDevice(devices.Clear)
	if err := b.Connect(); err != nil {
		l.Kill()
		return nil, nil, fmt.Errorf("connect browser: %w", err)
	}
	return l, b, nil
}

// browserUserAgent asks the running browser for the User-Agent it will send.
func browserUserAgent(b *rod.Browser) (string, error) {
	v, err := b.Version()
	if err != nil {
		return "", fmt.Errorf("browser version: %w", err)
	}
	return v.UserAgent, nil
}

func closeBrowser(b *rod.Browser, l *launcher.Launcher) {
	_ = b.Close()
	l.Kill()
}

// Fetch navigates to a URL in a new tab and returns the rendered HTML.
// The context bounds navigation and JS settling; if it's cancelled, the
// underlying Rod operations unblock with the ctx error.
//
// Cookies are set BEFORE navigation so consent-banner walls (which read the
// cookie on first paint) render their real content. The page is created at
// about:blank (empty TargetCreateTarget URL) precisely so SetCookies lands
// before the first request. Cookies persist in the shared browser context
// across fetches within one process (CDP cookie storage is context-wide);
// per-fetch filtering still bounds what each navigation loads. Acceptable for
// a single-operator CLI.
func (r *rodConn) Fetch(ctx context.Context, rawURL string) (string, error) {
	page, err := r.browser.Context(ctx).Page(proto.TargetCreateTarget{}) // about:blank
	if err != nil {
		return "", fmt.Errorf("create page: %w", err)
	}
	defer func() { _ = page.Close() }()

	if params := rodCookieParams(r.jar, rawURL); len(params) > 0 {
		if err := page.SetCookies(params); err != nil {
			return "", fmt.Errorf("set cookies: %w", err)
		}
	}
	if err := page.Navigate(rawURL); err != nil {
		return "", fmt.Errorf("navigate: %w", err)
	}

	timedPage := page.Timeout(30 * time.Second)
	_ = timedPage.WaitLoad()
	_ = timedPage.WaitStable(time.Second)

	return page.HTML()
}

// rodCookieParams converts jar entries matching pageURL into CDP cookie params.
// Host-only cookies are keyed by URL (CDP derives a host-only cookie from it);
// domain cookies pass a dot-prefixed Domain so subdomains match.
func rodCookieParams(jar *cookies.Jar, pageURL string) []*proto.NetworkCookieParam {
	u, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	matched := jar.For(u)
	params := make([]*proto.NetworkCookieParam, 0, len(matched))
	for _, c := range matched {
		p := &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
		}
		if c.HostOnly {
			scheme := "http"
			if c.Secure {
				scheme = "https"
			}
			p.URL = scheme + "://" + c.Domain + c.Path
		} else {
			p.Domain = "." + c.Domain
		}
		if !c.Expires.IsZero() {
			p.Expires = proto.TimeSinceEpoch(c.Expires.Unix())
		}
		params = append(params, p)
	}
	return params
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
//
// The revision directory is removed before downloading. Extraction is not
// atomic: an interrupted download leaves a partial tree behind, and the next
// attempt fails on it rather than replacing it — either because the unzip hits
// existing symlinks ("file exists") or because the leftover confuses the
// single-directory check during extraction. Both surface as errors that look
// unrelated to the real cause, so retries appear to fail differently each time.
// Clearing first makes every install start from a known state.
func InstallBrowser() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache dir: %w", err)
	}
	b := launcher.NewBrowser()
	b.RootDir = filepath.Join(cacheDir, "ketch", "browser")

	if err := os.RemoveAll(b.Dir()); err != nil {
		return "", fmt.Errorf("clear browser cache %s: %w", b.Dir(), err)
	}

	if err := b.Download(); err != nil {
		return "", fmt.Errorf("download browser to %s: %w (remove that directory and retry, "+
			"or point ketch at an existing Chrome with: ketch config set browser <path>)", b.RootDir, err)
	}
	return b.BinPath(), nil
}
