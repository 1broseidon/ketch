package scrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A page served in a legacy encoding is decoded before extraction: with
// the charset declared in Content-Type, and — as browsers do — as
// windows-1252 when nothing declares it and the bytes are not UTF-8.
func TestScrapeDecodesLegacyCharset(t *testing.T) {
	// "Café", "déjà vu" and an em dash in windows-1252.
	body := "<html><head><title>Caf\xe9</title></head><body><main><h1>Caf\xe9</h1><p>" +
		strings.Repeat("The caf\xe9 serves d\xe9j\xe0 vu \x97 every morning without fail. ", 10) + "</p></main></body></html>"
	for _, tc := range []struct{ name, contentType string }{
		{"declared", "text/html; charset=windows-1252"},
		{"undeclared", "text/html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(srv.Close)

			page, _, err := New().Scrape(context.Background(), srv.URL)
			if err != nil {
				t.Fatalf("Scrape: %v", err)
			}
			if page.Title != "Café" || !strings.Contains(page.Markdown, "déjà vu — every morning") || strings.Contains(page.Markdown, "\uFFFD") {
				t.Fatalf("page = %q / %q", page.Title, page.Markdown)
			}
			result, err := New().ScrapeConditional(context.Background(), srv.URL, "", "")
			if err != nil {
				t.Fatalf("ScrapeConditional: %v", err)
			}
			if !strings.Contains(result.Page.Markdown, "déjà vu — every morning") || !strings.Contains(result.RawHTML, "Café") {
				t.Fatalf("conditional page = %q raw = %q", result.Page.Markdown, result.RawHTML)
			}
		})
	}
}
