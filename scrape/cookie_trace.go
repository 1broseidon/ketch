package scrape

import (
	"context"
	"net/url"
	"sync/atomic"
)

// A cookie trace records whether a fetch sent any cookie from cookie_file:
// the HTTP transport marks it when it attaches a Cookie header to any hop
// of a redirect chain, and the browser when it sets cookies on a tab. A
// caller that must not pass a page read with the user's cookies on (ketch
// docs' decision provider) wraps its context with WithCookieTrace and asks
// CookiesSent after the fetch; a page from the cache has no trace, so it
// also asks CookiesFor of the addresses it requested and was served from.

type cookieTraceKey struct{}

// WithCookieTrace is ctx carrying a fresh cookie trace.
func WithCookieTrace(ctx context.Context) context.Context {
	return context.WithValue(ctx, cookieTraceKey{}, new(atomic.Bool))
}

// CookiesSent reports a request made under ctx's cookie trace that carried
// a cookie from the jar; false without a trace.
func CookiesSent(ctx context.Context) bool {
	t, _ := ctx.Value(cookieTraceKey{}).(*atomic.Bool)
	return t != nil && t.Load()
}

// markCookiesSent records a cookie sent under ctx's trace, if it has one.
func markCookiesSent(ctx context.Context) {
	if t, _ := ctx.Value(cookieTraceKey{}).(*atomic.Bool); t != nil {
		t.Store(true)
	}
}

// HasCookies reports a cookie_file with live cookies: without one no fetch
// sends a cookie, and nothing need be traced.
func (s *Scraper) HasCookies() bool { return s.jar.Len() > 0 }

// CookiesFor reports a jar cookie that a request to rawURL would carry,
// matched as the transport matches it (cookies.Jar.For).
func (s *Scraper) CookiesFor(rawURL string) bool {
	if !s.HasCookies() || rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	return err == nil && len(s.jar.For(u)) > 0
}
