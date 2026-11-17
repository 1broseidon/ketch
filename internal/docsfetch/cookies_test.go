package docsfetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/scrape"
)

// cookieSite serves a docs page at /guide/auth, and /login, which sends a
// request on to /guide/auth; it records whether each path got a Cookie.
type cookieSite struct {
	*httptest.Server
	mu  sync.Mutex
	got map[string]bool
}

func newCookieSite(t *testing.T) *cookieSite {
	t.Helper()
	site := &cookieSite{got: map[string]bool{}}
	site.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.got[r.URL.Path] = site.got[r.URL.Path] || r.Header.Get("Cookie") != ""
		site.mu.Unlock()
		if r.URL.Path == "/login" {
			http.Redirect(w, r, "/guide/auth", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docHTML))
	}))
	t.Cleanup(site.Close)
	return site
}

// cookieScraper is a scraper whose cookie_file holds one cookie for the
// test server's host under path ("" for no cookie_file).
func cookieScraper(t *testing.T, path string) *scrape.Scraper {
	t.Helper()
	cfg := config.Defaults()
	if path != "" {
		jar := filepath.Join(t.TempDir(), "cookies.txt")
		line := strings.Join([]string{"127.0.0.1", "FALSE", path, "FALSE", "0", "session", "secret"}, "\t")
		if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n"+line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg.CookieFile = jar
	}
	s, err := scrape.NewFromConfig(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// A page fetched with a cookie from cookie_file says so, from the fetch
// (any hop of a redirect) or, for a cached page, from the jar; a page no
// cookie matches, or any page without cookie_file, does not.
func TestFetchPageReportsCookies(t *testing.T) {
	site := newCookieSite(t)
	for _, tc := range []struct {
		name, jarPath, path string
		cached              bool // fetched, then served from the page cache
		want                bool
	}{
		{"matching cookie", "/guide", "/guide/auth", false, true},
		{"cached", "/guide", "/guide/auth", true, true},
		// The cookie matches only the hop that redirects: the page is still
		// one fetched with it.
		{"cookie on a redirect hop", "/login", "/login", false, true},
		{"no matching cookie", "/private", "/guide/auth", false, false},
		{"no cookie_file", "", "/guide/auth", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := New(cookieScraper(t, tc.jarPath), nil)
			fetches := 1
			if tc.cached {
				f, fetches = New(cookieScraper(t, tc.jarPath), newCache(t)), 2
			}
			for i := range fetches {
				page, err := f.FetchPage(context.Background(), site.URL+tc.path)
				if err != nil || page.Cookies != tc.want || !strings.HasSuffix(page.URL, "/guide/auth") {
					t.Errorf("fetch %d: Cookies %v (want %v), URL %s, err %v", i, page.Cookies, tc.want, page.URL, err)
				}
			}
		})
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if !site.got["/guide/auth"] {
		t.Error("no request carried the cookie")
	}
}

// A raw fetch with a matching cookie says so too.
func TestFetchRawReportsCookies(t *testing.T) {
	site := newCookieSite(t)
	raw, err := New(cookieScraper(t, "/guide"), nil).FetchRaw(context.Background(), site.URL+"/guide/auth")
	if err != nil || !raw.Cookies {
		t.Errorf("raw with a matching cookie: Cookies %v, err %v", raw.Cookies, err)
	}
}

// Without cookie_file no cookie trace is made: the context passes through.
func TestTracedWithoutCookiesIsTheContext(t *testing.T) {
	ctx := context.Background()
	if got := New(cookieScraper(t, ""), nil).traced(ctx); got != ctx {
		t.Error("a scraper without cookie_file traced the fetch")
	}
}
