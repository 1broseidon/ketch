package scrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const shellHTML = `<!doctype html><html><head><title>App</title>
<script id="__NEXT_DATA__" type="application/json">{"buildId":"dev","page":"/","props":{"chunks":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}}</script>
</head><body><div id="root"></div></body></html>`

// navigatingBrowser renders canned HTML and reports landing somewhere else,
// as a script redirect would.
type navigatingBrowser struct{ landed string }

func (b *navigatingBrowser) Fetch(ctx context.Context, url string) (string, error) {
	html, _, err := b.FetchFinal(ctx, url)
	return html, err
}

func (b *navigatingBrowser) FetchFinal(context.Context, string) (string, string, error) {
	return renderedPage, b.landed, nil
}

func (*navigatingBrowser) Close() {}

// plainBrowser cannot say where it landed.
type plainBrowser struct{}

func (plainBrowser) Fetch(context.Context, string) (string, error) { return renderedPage, nil }
func (plainBrowser) Close()                                        {}

// Page.FinalURL is where the content came from: the HTTP response URL after
// redirects, or the browser's landing URL when a JS shell was rendered (the
// HTTP URL when the browser cannot report one). FetchContent reports the
// response URL too. Scrape output keeps its shape: FinalURL is not JSON.
func TestFinalURLFollowsRedirectsAndBrowserNavigation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/shell", http.StatusFound) })
	mux.HandleFunc("/shell", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(shellHTML))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := context.Background()

	content, err := New().FetchContent(ctx, srv.URL+"/start")
	if err != nil || content.URL != srv.URL+"/shell" {
		t.Errorf("FetchContent URL = %q, %v; want the redirect target", content.URL, err)
	}

	for _, tc := range []struct {
		name    string
		browser BrowserConn
		want    string // after the HTTP fetch and a fallback render
		forced  string // a forced render, with no HTTP fetch to fall back on
	}{
		{"navigating browser", &navigatingBrowser{landed: "https://elsewhere.test/landed"}, "https://elsewhere.test/landed", "https://elsewhere.test/landed"},
		{"browser without a landing URL", plainBrowser{}, srv.URL + "/shell", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewWithBrowserConn(tc.browser, nil)
			page, source, err := s.Scrape(ctx, srv.URL+"/start")
			if err != nil || source != SourceBrowser || page.FinalURL != tc.want {
				t.Errorf("Scrape = FinalURL %q, source %q, %v; want %q from the browser", page.FinalURL, source, err, tc.want)
			}
			result, err := s.ScrapeConditional(ctx, srv.URL+"/start", "", "")
			if err != nil || result.Page.FinalURL != tc.want {
				t.Errorf("ScrapeConditional FinalURL = %q, %v; want %q", result.Page.FinalURL, err, tc.want)
			}
			forced, _, err := s.BrowserScrape(ctx, srv.URL+"/start")
			if err != nil || forced.FinalURL != tc.forced {
				t.Errorf("BrowserScrape FinalURL = %q, %v; want %q", forced.FinalURL, err, tc.forced)
			}
		})
	}
	page, _, err := New().Scrape(ctx, srv.URL+"/start")
	if err != nil || page.FinalURL != srv.URL+"/shell" {
		t.Errorf("Scrape without a browser FinalURL = %q, %v; want the HTTP redirect target", page.FinalURL, err)
	}
}

// fullBrowser reports the HTML, status and landing URL from one render, as
// rodConn does.
type fullBrowser struct {
	status int
	landed string
}

func (b *fullBrowser) Fetch(ctx context.Context, url string) (string, error) {
	r, err := b.render(ctx, url)
	return r.html, err
}

func (b *fullBrowser) render(context.Context, string) (rendering, error) {
	return rendering{html: renderedPage, status: b.status, finalURL: b.landed}, nil
}

func (*fullBrowser) Close() {}

// A page fetched through the browser after an HTTP 403 reports where the
// browser landed, or "" when the browser cannot say: there is no HTTP response
// URL to fall back on. The status check still applies to a browser that
// reports the landing URL in the same render.
func TestFinalURLAfterForbiddenRetry(t *testing.T) {
	const landed = "https://elsewhere.test/landed"
	for _, tc := range []struct {
		name    string
		browser BrowserConn
		want    string
	}{
		{"one render with status and landing URL", &fullBrowser{status: http.StatusOK, landed: landed}, landed},
		{"navigating browser", &navigatingBrowser{landed: landed}, landed},
		{"status-only browser", &statusBrowser{status: http.StatusOK}, ""},
		{"plain browser", plainBrowser{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := statusServer(t, http.StatusForbidden)
			s := NewWithBrowserConn(tc.browser, nil)
			captureStderr(t, func() {
				page, source, err := s.Scrape(t.Context(), server.URL)
				if err != nil || source != SourceBrowser || page.FinalURL != tc.want {
					t.Errorf("Scrape = FinalURL %q, source %q, %v; want %q", page.FinalURL, source, err, tc.want)
				}
				result, err := s.ScrapeConditional(t.Context(), server.URL, "", "")
				if err != nil || result.Page.FinalURL != tc.want {
					t.Errorf("ScrapeConditional FinalURL = %q, %v; want %q", result.Page.FinalURL, err, tc.want)
				}
			})
		})
	}

	server := statusServer(t, http.StatusForbidden)
	s := NewWithBrowserConn(&fullBrowser{status: http.StatusForbidden, landed: landed}, nil)
	var err error
	captureStderr(t, func() { _, _, err = s.Scrape(t.Context(), server.URL) })
	assertStatusError(t, err, http.StatusForbidden, server.URL)
}
