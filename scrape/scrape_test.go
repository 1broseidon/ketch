package scrape

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const renderedPage = `<html><head><title>Rendered</title></head><body><main><h1>Past the wall</h1><p>Content the browser was allowed to see.</p></main></body></html>`

// renderBrowser is a fake BrowserConn that records calls and either renders
// renderedPage or fails with err.
type renderBrowser struct {
	calls int
	urls  []string
	err   error
}

func (b *renderBrowser) Fetch(_ context.Context, url string) (string, error) {
	b.calls++
	b.urls = append(b.urls, url)
	if b.err != nil {
		return "", b.err
	}
	return renderedPage, nil
}

func (*renderBrowser) Close() {}

func statusServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("<html><body>Access denied</body></html>"))
	}))
	t.Cleanup(server.Close)
	return server
}

// captureStderr redirects os.Stderr for the duration of run and returns what
// was written. Tests using it must not call t.Parallel.
func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	defer func() {
		os.Stderr = orig
	}()
	run()
	_ = w.Close()
	os.Stderr = orig
	return <-done
}

// escalationPaths drives every non-forced fetch path that escalates a 403.
// Each returns the source it reports and a marker of the content it got.
var escalationPaths = []struct {
	name string
	run  func(context.Context, *Scraper, string) (source, content string, err error)
}{
	{
		name: "Scrape",
		run: func(ctx context.Context, s *Scraper, url string) (string, string, error) {
			page, source, err := s.Scrape(ctx, url)
			if err != nil {
				return "", "", err
			}
			return source, page.Markdown, nil
		},
	},
	{
		name: "CachedScrape",
		run: func(ctx context.Context, s *Scraper, url string) (string, string, error) {
			pc := newMemCache()
			page, err := s.CachedScrape(ctx, pc, url)
			if err != nil {
				return "", "", err
			}
			_, source := pc.Get(s.CacheKey(url))
			return source, page.Markdown, nil
		},
	},
	{
		name: "ScrapeConditional",
		run: func(ctx context.Context, s *Scraper, url string) (string, string, error) {
			result, err := s.ScrapeConditional(ctx, url, "", "")
			if err != nil {
				return "", "", err
			}
			return result.Source, result.Page.Markdown, nil
		},
	},
	{
		name: "raw",
		run: func(ctx context.Context, s *Scraper, url string) (string, string, error) {
			_, html, source, err := s.ScrapeRaw(ctx, nil, url, false)
			return source, html, err
		},
	},
	{
		name: "selector",
		run: func(ctx context.Context, s *Scraper, url string) (string, string, error) {
			page, err := s.ScrapeSelector(ctx, url, "main", false)
			if err != nil {
				return "", "", err
			}
			return SourceBrowser, page.Markdown, nil
		},
	},
}

func TestForbiddenEscalatesToBrowser(t *testing.T) {
	for _, path := range escalationPaths {
		t.Run(path.name, func(t *testing.T) {
			server := statusServer(t, http.StatusForbidden)
			browser := &renderBrowser{}
			s := NewWithBrowserConn(browser, nil)
			var source, content string
			var err error
			stderr := captureStderr(t, func() {
				source, content, err = path.run(t.Context(), s, server.URL)
			})
			if err != nil {
				t.Fatalf("error = %v, want the browser render", err)
			}
			if browser.calls != 1 || browser.urls[0] != server.URL {
				t.Fatalf("browser calls = %v, want one fetch of %s", browser.urls, server.URL)
			}
			if source != SourceBrowser {
				t.Fatalf("source = %q, want %q", source, SourceBrowser)
			}
			if !strings.Contains(content, "Past the wall") {
				t.Fatalf("content = %q, want the rendered page", content)
			}
			want := "notice: HTTP 403 for " + server.URL + "; fetched with the browser\n"
			if stderr != want {
				t.Fatalf("stderr = %q, want %q", stderr, want)
			}
		})
	}
}

func TestForbiddenBrowserFailureReturnsOriginalError(t *testing.T) {
	for _, path := range escalationPaths {
		t.Run(path.name, func(t *testing.T) {
			server := statusServer(t, http.StatusForbidden)
			browser := &renderBrowser{err: errors.New("navigation blocked")}
			s := NewWithBrowserConn(browser, nil)
			var err error
			stderr := captureStderr(t, func() {
				_, _, err = path.run(t.Context(), s, server.URL)
			})
			assertStatusError(t, err, http.StatusForbidden, server.URL)
			if strings.Contains(err.Error(), "navigation blocked") {
				t.Fatalf("error = %v, want the original 403, not the browser error", err)
			}
			if browser.calls != 1 {
				t.Fatalf("browser calls = %d, want 1", browser.calls)
			}
			if !strings.Contains(stderr, "warn: browser retry after HTTP 403 failed for "+server.URL) ||
				!strings.Contains(stderr, "navigation blocked") {
				t.Fatalf("stderr = %q, want the browser failure warning", stderr)
			}
			if strings.Contains(stderr, "notice:") {
				t.Fatalf("stderr = %q, want no success notice", stderr)
			}
		})
	}
}

func TestForbiddenWithoutBrowserIsUnchanged(t *testing.T) {
	for _, path := range escalationPaths {
		t.Run(path.name, func(t *testing.T) {
			server := statusServer(t, http.StatusForbidden)
			s := New()
			var err error
			stderr := captureStderr(t, func() {
				_, _, err = path.run(t.Context(), s, server.URL)
			})
			assertStatusError(t, err, http.StatusForbidden, server.URL)
			if stderr != "" {
				t.Fatalf("stderr = %q, want nothing", stderr)
			}
		})
	}
}

func TestOtherStatusesNeverTouchBrowser(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusNotFound, http.StatusServiceUnavailable} {
		for _, path := range escalationPaths {
			t.Run(http.StatusText(status)+"/"+path.name, func(t *testing.T) {
				server := statusServer(t, status)
				browser := &renderBrowser{}
				s := NewWithBrowserConn(browser, nil)
				var err error
				stderr := captureStderr(t, func() {
					_, _, err = path.run(t.Context(), s, server.URL)
				})
				assertStatusError(t, err, status, server.URL)
				if browser.calls != 0 {
					t.Fatalf("browser calls = %d, want 0 for HTTP %d", browser.calls, status)
				}
				if stderr != "" {
					t.Fatalf("stderr = %q, want nothing", stderr)
				}
			})
		}
	}
}

func TestHTTPStatusErrorMessage(t *testing.T) {
	err := &HTTPStatusError{StatusCode: 403, URL: "https://example.com/page"}
	if got, want := err.Error(), "HTTP 403 for https://example.com/page"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func assertStatusError(t *testing.T, err error, status int, url string) {
	t.Helper()
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want *HTTPStatusError", err)
	}
	if statusErr.StatusCode != status || statusErr.URL != url {
		t.Fatalf("status error = %+v, want HTTP %d for %s", statusErr, status, url)
	}
	if want := statusErr.Error(); !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("error = %q, want it to end with %q", err.Error(), want)
	}
}

// memCache is a minimal in-memory PageCache.
type memCache struct {
	pages   map[string]*Page
	sources map[string]string
	raw     map[string]string
}

func newMemCache() *memCache {
	return &memCache{pages: map[string]*Page{}, sources: map[string]string{}, raw: map[string]string{}}
}

func (c *memCache) Get(url string) (*Page, string) { return c.pages[url], c.sources[url] }

func (c *memCache) Put(url string, page *Page, source string) {
	c.pages[url], c.sources[url] = page, source
}

func (c *memCache) GetRaw(url string) (string, string, *Page) {
	if c.raw[url] == "" {
		return "", "", nil
	}
	return c.raw[url], c.sources[url], c.pages[url]
}

func (c *memCache) PutRaw(url string, page *Page, source, rawHTML string) {
	c.pages[url], c.sources[url], c.raw[url] = page, source, rawHTML
}
