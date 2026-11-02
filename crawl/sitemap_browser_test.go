package crawl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/1broseidon/ketch/scrape"
)

type sitemapBrowser struct {
	bodies map[string]string
	status int
	calls  []string
}

func (*sitemapBrowser) Fetch(context.Context, string) (string, error) {
	return "", errors.New("rendered XML viewer is not sitemap content")
}
func (*sitemapBrowser) Close() {}
func (b *sitemapBrowser) FetchContent(_ context.Context, rawURL string) (*scrape.FetchedContent, int, error) {
	b.calls = append(b.calls, rawURL)
	return &scrape.FetchedContent{Body: []byte(b.bodies[rawURL]), ContentType: "application/xml"}, b.status, nil
}

func TestSitemapBrowserFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	index, child := server.URL+"/index.xml", server.URL+"/child.xml"
	browser := &sitemapBrowser{status: 200, bodies: map[string]string{
		index: `<sitemapindex><sitemap><loc>` + child + `</loc></sitemap></sitemapindex>`,
		child: `<urlset><url><loc>https://example.com/?a=1&amp;b=2</loc></url></urlset>`,
	}}
	s := scrape.NewWithBrowserConn(browser, nil)
	defer s.Close()
	urls, err := fetchSitemap(t.Context(), s, index)
	if err != nil || !reflect.DeepEqual(urls, []string{"https://example.com/?a=1&b=2"}) {
		t.Fatalf("urls=%v err=%v", urls, err)
	}
	if !reflect.DeepEqual(browser.calls, []string{index, child}) {
		t.Fatalf("browser requests=%v", browser.calls)
	}
}

func TestSitemapBrowserFailurePreservesForbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	browser := &sitemapBrowser{status: 403, bodies: map[string]string{server.URL: `<html><body>Forbidden</body></html>`}}
	s := scrape.NewWithBrowserConn(browser, nil)
	defer s.Close()
	urls, err := fetchSitemap(t.Context(), s, server.URL)
	var statusErr *scrape.HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != 403 || urls != nil {
		t.Fatalf("urls=%v err=%v; want original 403", urls, err)
	}
}
