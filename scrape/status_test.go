package scrape

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/1broseidon/ketch/urlrewrite"
)

// A non-200 response is an *HTTPStatusError on every fetch entry point, so
// callers can read the status with errors.As, and its text is exactly the
// "HTTP <code> for <url>" every diagnostic printed before it was typed. The
// URL is the one fetched, after rewrites.
func TestNon200IsHTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone":
			w.WriteHeader(http.StatusGone)
		case "/down":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	rw, err := urlrewrite.NewRewriter([]urlrewrite.Rule{{Match: `^https://docs\.example\.test/(.*)$`, Replace: srv.URL + "/$1"}})
	if err != nil {
		t.Fatal(err)
	}
	s := NewWithRewriter("", rw)
	t.Cleanup(s.Close)
	ctx := context.Background()

	fetches := map[string]func(url string) error{
		"FetchContent": func(url string) error { _, err := s.FetchContent(ctx, s.Rewrite(url)); return err },
		"Scrape":       func(url string) error { _, _, err := s.Scrape(ctx, url); return err },
		"CachedScrape": func(url string) error { _, err := s.ScrapeMarkdown(ctx, nil, url, false); return err },
		"Conditional":  func(url string) error { _, err := s.ScrapeConditional(ctx, url, `"etag"`, ""); return err },
	}
	cases := []struct {
		path string
		code int
	}{{"/missing", http.StatusNotFound}, {"/gone", http.StatusGone}, {"/down", http.StatusServiceUnavailable}}
	for name, fetch := range fetches {
		for _, tc := range cases {
			t.Run(name+tc.path, func(t *testing.T) {
				err := fetch("https://docs.example.test" + tc.path)
				var status *HTTPStatusError
				if !errors.As(err, &status) {
					t.Fatalf("error = %T %v, want *HTTPStatusError", err, err)
				}
				wantURL := srv.URL + tc.path
				if status.StatusCode != tc.code || status.URL != wantURL {
					t.Errorf("status = %d for %q, want %d for %q", status.StatusCode, status.URL, tc.code, wantURL)
				}
				if want := "HTTP " + strconv.Itoa(tc.code) + " for " + wantURL; err.Error() != want {
					t.Errorf("error text = %q, want %q (unchanged from the untyped error)", err.Error(), want)
				}
			})
		}
	}
}
