package scrape

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/go-rod/rod/lib/proto"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type contentBrowser struct {
	content *FetchedContent
	status  int
	err     error
	calls   int
}

func (*contentBrowser) Fetch(context.Context, string) (string, error) {
	return "", errors.New("rendered DOM must not be used as response bytes")
}
func (*contentBrowser) Close() {}
func (b *contentBrowser) FetchContent(context.Context, string) (*FetchedContent, int, error) {
	b.calls++
	return b.content, b.status, b.err
}

func TestFetchContentWithBrowserForbidden(t *testing.T) {
	const body = `<?xml version="1.0"?><urlset><url><loc>https://example.com/?a=1&amp;b=2</loc></url></urlset>`
	for _, tc := range []struct {
		name                      string
		httpStatus, browserStatus int
		browserErr                error
		nilContent, wantError     bool
		wantCalls                 int
	}{
		{"plain success", 200, 200, nil, false, false, 0},
		{"browser success", 403, 200, nil, false, false, 1},
		{"browser forbidden", 403, 403, nil, false, true, 1},
		{"browser server error", 403, 500, nil, false, true, 1},
		{"browser unknown status", 403, 0, nil, false, true, 1},
		{"browser redirect not followed", 403, 302, nil, false, true, 1},
		{"browser error", 403, 200, errors.New("browser failed"), false, true, 1},
		{"browser nil content", 403, 200, nil, true, true, 1},
		{"rate limited", 429, 200, nil, false, true, 0},
		{"unauthorized", 401, 200, nil, false, true, 0},
		{"missing", 404, 200, nil, false, true, 0},
		{"server error", 503, 200, nil, false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(tc.httpStatus)
				_, _ = fmt.Fprint(w, body)
			}))
			defer srv.Close()
			b := &contentBrowser{content: &FetchedContent{Body: []byte(body), ContentType: "application/xml"}, status: tc.browserStatus, err: tc.browserErr}
			if tc.nilContent {
				b.content = nil
			}
			s := NewWithBrowserConn(b, nil)
			defer s.Close()
			content, err := s.FetchContentWithBrowser(t.Context(), srv.URL)
			if tc.wantError {
				var se *HTTPStatusError
				if !errors.As(err, &se) || se.StatusCode != tc.httpStatus || se.URL != srv.URL || content != nil {
					t.Fatalf("content=%v error=%v; want original HTTP %d", content, err, tc.httpStatus)
				}
			} else if err != nil || content == nil || string(content.Body) != body || content.ContentType != "application/xml" {
				t.Fatalf("content=%v error=%v; want original XML", content, err)
			}
			if b.calls != tc.wantCalls {
				t.Fatalf("browser calls=%d want=%d", b.calls, tc.wantCalls)
			}
		})
	}
}

func TestFetchContentWithBrowserWithoutContentCapability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }))
	defer srv.Close()
	for _, browser := range []BrowserConn{nil, &browserFetchSpy{}} {
		s := NewWithBrowserConn(browser, nil)
		_, err := s.FetchContentWithBrowser(t.Context(), srv.URL)
		s.Close()
		var se *HTTPStatusError
		if !errors.As(err, &se) || se.StatusCode != 403 {
			t.Fatalf("error=%v; want original 403", err)
		}
		if spy, ok := browser.(*browserFetchSpy); ok && spy.calls != 0 {
			t.Fatal("retried via lossy DOM interface")
		}
	}
}

func TestRodContentRejectsNonHTTPURL(t *testing.T) {
	_, _, err := (&rodConn{}).FetchContent(t.Context(), "file:///etc/passwd")
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("error=%v; want invalid URL", err)
	}
}

// Optional local integration test: KETCH_TEST_BROWSER=/path/to/chromium.
// Uses only a loopback fixture, including Chromium's built-in XML viewer and XSL.
func TestRealBrowserSitemapContent(t *testing.T) {
	bin := os.Getenv("KETCH_TEST_BROWSER")
	if bin == "" {
		t.Skip("set KETCH_TEST_BROWSER to run real-browser XML regression")
	}
	srv := httptest.NewServer(http.HandlerFunc(serveSitemapFixture))
	defer srv.Close()
	browser, err := NewBrowserConn(bin)
	if err != nil {
		t.Fatal(err)
	}
	s := NewWithBrowserConn(browser, nil)
	defer s.Close()
	for _, path := range []string{"/xml", "/redirect", "/styled", "/denied"} {
		t.Run(path, func(t *testing.T) {
			content, err := s.FetchContentWithBrowser(t.Context(), srv.URL+path)
			if path == "/denied" {
				var se *HTTPStatusError
				if !errors.As(err, &se) || se.StatusCode != 403 || content != nil {
					t.Fatalf("content=%v error=%v; want original 403", content, err)
				}
				return
			}
			want := sitemapXMLBody
			if path == "/styled" {
				want = sitemapStyledBody
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(content.Body) != want || !strings.Contains(content.ContentType, "xml") {
				t.Fatalf("got %q (%s), want original XML %q", content.Body, content.ContentType, want)
			}
		})
	}
}

const sitemapXMLBody = `<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://example.com/?a=1&amp;b=2</loc></url></urlset>`
const sitemapStyledBody = `<?xml version="1.0"?><?xml-stylesheet type="text/xsl" href="/style.xsl"?><urlset><url><loc>https://example.com/styled</loc></url></urlset>`

func serveSitemapFixture(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/style.xsl" {
		w.Header().Set("Content-Type", "text/xsl")
		_, _ = fmt.Fprint(w, `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform"><xsl:template match="/"><html><body>VIEWER, NOT SITEMAP XML</body></html></xsl:template></xsl:stylesheet>`)
		return
	}
	if r.Header.Get("Sec-Fetch-Dest") != "document" {
		w.WriteHeader(403)
		return
	}
	if r.URL.Path == "/redirect" {
		http.Redirect(w, r, "/xml", http.StatusFound)
		return
	}
	if r.URL.Path == "/denied" {
		w.WriteHeader(403)
		_, _ = fmt.Fprint(w, "denied")
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	if r.URL.Path == "/styled" {
		_, _ = fmt.Fprint(w, sitemapStyledBody)
	} else {
		_, _ = fmt.Fprint(w, sitemapXMLBody)
	}
}

func TestDecodeBrowserBody(t *testing.T) {
	for _, tc := range []struct {
		name, body       string
		encoded, wantErr bool
	}{
		{"raw", "<urlset/>", false, false},
		{"base64", base64.StdEncoding.EncodeToString([]byte("<urlset/>")), true, false},
		{"invalid base64", "!", true, true},
		{"oversized", strings.Repeat("x", MaxBodyBytes+1), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := decodeBrowserBody(&proto.FetchGetResponseBodyResult{Body: tc.body, Base64Encoded: tc.encoded})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && string(body) != "<urlset/>" {
				t.Fatalf("body=%q", body)
			}
		})
	}
}

func TestRealBrowserSitemapContentCancellation(t *testing.T) {
	bin := os.Getenv("KETCH_TEST_BROWSER")
	if bin == "" {
		t.Skip("set KETCH_TEST_BROWSER to run browser cancellation regression")
	}
	browser, err := NewBrowserConn(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	conn := browser.(*rodConn)
	before, err := conn.browser.Pages()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, _, err = conn.FetchContent(ctx, srv.URL)
	if err == nil {
		t.Fatal("cancelled navigation succeeded")
	}
	after, err := conn.browser.Pages()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("tabs before=%d after=%d", len(before), len(after))
	}
}
