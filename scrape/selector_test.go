package scrape

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/urlrewrite"
)

func TestSelectorResolvesLinksAgainstRewrittenPage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/docs/chapter/page" {
			t.Errorf("unexpected fetch: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><title>Guide</title><article><a href="../reference#item">reference</a><img src="image.png" alt="diagram"></article></html>`))
	}))
	t.Cleanup(srv.Close)
	original := srv.URL + "/source"
	fetched := srv.URL + "/docs/chapter/page"
	rw, err := urlrewrite.NewRewriter([]urlrewrite.Rule{{Match: "^" + regexp.QuoteMeta(original) + "$", Replace: fetched}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := NewWithRewriter("", rw).ScrapeSelector(t.Context(), original, "article", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{srv.URL + "/docs/reference#item", srv.URL + "/docs/chapter/image.png"} {
		if !strings.Contains(page.Markdown, want) {
			t.Errorf("missing %q in %s", want, page.Markdown)
		}
	}
	if page.URL != original || page.FetchedURL != fetched {
		t.Fatalf("source identity changed: %#v", page)
	}
}
