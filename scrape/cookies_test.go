package scrape

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/cookies"
	"github.com/go-rod/rod/lib/proto"
)

// writeJar writes a cookies.txt with the given lines and returns its path.
func writeJar(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func tabLine(fields ...string) string { return strings.Join(fields, "\t") }

func scraperWithJar(t *testing.T, jarPath string) *Scraper {
	t.Helper()
	s, err := NewFromConfig(&config.Config{CookieFile: jarPath, CacheTTL: "1h"})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	return s
}

func TestFetchSendsMatchingCookies(t *testing.T) {
	jar := writeJar(t,
		tabLine("127.0.0.1", "FALSE", "/", "FALSE", "0", "session", "secret123"),
		tabLine("other.example", "FALSE", "/", "FALSE", "0", "nope", "shouldnotappear"),
	)
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("<html><body>hi</body></html>"))
	}))
	defer srv.Close()

	s := scraperWithJar(t, jar)
	if _, _, err := s.Scrape(context.Background(), srv.URL); err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	if gotCookie != "session=secret123" {
		t.Fatalf("Cookie header = %q, want session=secret123", gotCookie)
	}
	if strings.Contains(gotCookie, "shouldnotappear") {
		t.Fatal("non-matching cookie leaked into request")
	}
}

// Real X cookies (e.g. personalization_id) use RFC 6265 quoted-string values.
// req.AddCookie would strip the quotes; the Cookie header must send them verbatim.
func TestFetchSendsQuotedCookieValueVerbatim(t *testing.T) {
	jar := writeJar(t,
		tabLine("127.0.0.1", "FALSE", "/", "FALSE", "0", "personalization_id", `"v1_abc123"`),
	)
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("<html><body>hi</body></html>"))
	}))
	defer srv.Close()

	s := scraperWithJar(t, jar)
	if _, _, err := s.Scrape(context.Background(), srv.URL); err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	if gotCookie != `personalization_id="v1_abc123"` {
		t.Fatalf("Cookie header = %q, want personalization_id=\"v1_abc123\" (quotes preserved)", gotCookie)
	}
}

func TestFetchSecureCookieNotSentOverHTTP(t *testing.T) {
	jar := writeJar(t, tabLine("127.0.0.1", "FALSE", "/", "TRUE", "0", "session", "secret123"))
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("<html><body>hi</body></html>"))
	}))
	defer srv.Close()

	s := scraperWithJar(t, jar)
	if _, _, err := s.Scrape(context.Background(), srv.URL); err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	if gotCookie != "" {
		t.Fatalf("Cookie header = %q, want empty (secure cookie over http)", gotCookie)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func redirectCookies(t *testing.T, jarText, start, target string) (string, string) {
	t.Helper()
	jar, err := cookies.Parse(strings.NewReader(jarText))
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]string)
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen[req.URL.String()] = req.Header.Get("Cookie")
		status := http.StatusOK
		header := make(http.Header)
		if req.URL.String() == start {
			status = http.StatusFound
			header.Set("Location", target)
		}
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})

	s := New()
	s.jar = jar
	s.client = clientWithCookies(&http.Client{Transport: base}, jar)
	if _, err := s.Fetch(context.Background(), start); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	return seen[start], seen[target]
}

func TestRedirectRescopesCookiesEveryHop(t *testing.T) {
	t.Run("HTTPS to HTTP removes Secure cookies", func(t *testing.T) {
		start := "https://example.com/private"
		target := "http://example.com/public"
		first, redirected := redirectCookies(t,
			tabLine("example.com", "FALSE", "/", "TRUE", "0", "secure", "secret"), start, target)
		if first != "secure=secret" {
			t.Fatalf("initial Cookie = %q", first)
		}
		if redirected != "" {
			t.Fatalf("downgrade Cookie = %q, want empty", redirected)
		}
	})

	t.Run("subdomain removes host-only cookies", func(t *testing.T) {
		start := "https://example.com/"
		target := "https://sub.example.com/"
		jar := strings.Join([]string{
			tabLine("example.com", "FALSE", "/", "FALSE", "0", "host", "only"),
			tabLine(".example.com", "TRUE", "/", "FALSE", "0", "domain", "wide"),
		}, "\n")
		first, redirected := redirectCookies(t, jar, start, target)
		if first != "host=only; domain=wide" {
			t.Fatalf("initial Cookie = %q", first)
		}
		if redirected != "domain=wide" {
			t.Fatalf("subdomain Cookie = %q, want only domain cookie", redirected)
		}
	})

	t.Run("path change removes narrower cookies", func(t *testing.T) {
		start := "https://example.com/private/start"
		target := "https://example.com/public"
		jar := strings.Join([]string{
			tabLine("example.com", "FALSE", "/private", "FALSE", "0", "private", "secret"),
			tabLine("example.com", "FALSE", "/", "FALSE", "0", "root", "ok"),
		}, "\n")
		first, redirected := redirectCookies(t, jar, start, target)
		if first != "private=secret; root=ok" {
			t.Fatalf("initial Cookie = %q", first)
		}
		if redirected != "root=ok" {
			t.Fatalf("changed-path Cookie = %q, want only root cookie", redirected)
		}
	})
}

func TestFetchLLMSTxtUsesCookies(t *testing.T) {
	jar := writeJar(t, tabLine("127.0.0.1", "FALSE", "/", "FALSE", "0", "session", "secret123"))
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# llms"))
	}))
	defer srv.Close()

	s := scraperWithJar(t, jar)
	content, ok := s.FetchLLMSTxt(context.Background(), srv.URL)
	if !ok || content != "# llms" {
		t.Fatalf("FetchLLMSTxt = %q, %v", content, ok)
	}
	if gotCookie != "session=secret123" {
		t.Fatalf("Cookie header = %q, want session cookie", gotCookie)
	}
}

func TestCacheKeyDivergence(t *testing.T) {
	u := "http://127.0.0.1:9999/page"

	t.Run("no jar returns url unchanged", func(t *testing.T) {
		s := New()
		if got := s.CacheKey(u); got != u {
			t.Fatalf("CacheKey = %q, want %q", got, u)
		}
	})

	t.Run("configured jar diverges and is stable", func(t *testing.T) {
		s := scraperWithJar(t, writeJar(t, tabLine("127.0.0.1", "FALSE", "/", "FALSE", "0", "session", "v1")))
		key := s.CacheKey(u)
		if key == u {
			t.Fatal("key should diverge with matching cookies")
		}
		if !strings.Contains(key, "\x00cookies:") {
			t.Fatalf("key %q missing cookie suffix", key)
		}
		if key != s.CacheKey(u) {
			t.Fatal("key should be stable across calls")
		}
	})

	t.Run("differing cookie value yields different key", func(t *testing.T) {
		s1 := scraperWithJar(t, writeJar(t, tabLine("127.0.0.1", "FALSE", "/", "FALSE", "0", "session", "aaa")))
		s2 := scraperWithJar(t, writeJar(t, tabLine("127.0.0.1", "FALSE", "/", "FALSE", "0", "session", "bbb")))
		if s1.CacheKey(u) == s2.CacheKey(u) {
			t.Fatal("different cookie values must produce different keys")
		}
	})

	t.Run("non-matching initial domain still isolates redirects", func(t *testing.T) {
		s := scraperWithJar(t, writeJar(t, tabLine("other.example", "FALSE", "/", "FALSE", "0", "session", "v")))
		if got := s.CacheKey(u); got == u {
			t.Fatal("configured jar must isolate the cache before a possible authenticated redirect")
		}
	})

}

// scraperFromConfig builds a Scraper via NewFromConfig and closes it when the
// test ends.
func scraperFromConfig(t *testing.T, cfg *config.Config) *Scraper {
	t.Helper()
	s, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// A configured user_agent must namespace the cache exactly like a cookie jar
// does: sites answer differently per UA, so an entry fetched under one must
// never satisfy a request under another.
func TestCacheKeyUserAgent(t *testing.T) {
	u := "http://127.0.0.1:9999/page"

	t.Run("default user agent keeps the bare key", func(t *testing.T) {
		s := scraperFromConfig(t, &config.Config{})
		if got := s.CacheKey(u); got != u {
			t.Fatalf("CacheKey = %q, want %q (unconfigured UA must not change the namespace)", got, u)
		}
	})

	t.Run("configured user agent diverges, is stable, and never leaks the UA", func(t *testing.T) {
		s := scraperFromConfig(t, &config.Config{UserAgent: "custom/1.2"})
		key := s.CacheKey(u)
		if key == u {
			t.Fatal("configured UA must change the cache namespace")
		}
		if !strings.Contains(key, "\x00ua:") {
			t.Fatalf("key %q missing ua suffix", key)
		}
		if strings.Contains(key, "custom/1.2") {
			t.Fatalf("key %q carries the UA text; want a digest", key)
		}
		if key != s.CacheKey(u) {
			t.Fatal("key should be stable across calls")
		}
	})

	t.Run("switching user agents yields different keys", func(t *testing.T) {
		s1 := scraperFromConfig(t, &config.Config{UserAgent: "agent-a/1"})
		s2 := scraperFromConfig(t, &config.Config{UserAgent: "agent-b/1"})
		if s1.CacheKey(u) == s2.CacheKey(u) {
			t.Fatal("different UAs must produce different keys")
		}
	})

	t.Run("cookie and user agent segments compose", func(t *testing.T) {
		jarPath := writeJar(t, tabLine("127.0.0.1", "FALSE", "/", "FALSE", "0", "session", "v1"))
		both := scraperFromConfig(t, &config.Config{CookieFile: jarPath, UserAgent: "custom/1.2"})
		jarOnly := scraperWithJar(t, jarPath)
		uaOnly := scraperFromConfig(t, &config.Config{UserAgent: "custom/1.2"})
		key := both.CacheKey(u)
		if !strings.Contains(key, "\x00cookies:") || !strings.Contains(key, "\x00ua:") {
			t.Fatalf("key %q must carry both segments", key)
		}
		if key == jarOnly.CacheKey(u) || key == uaOnly.CacheKey(u) {
			t.Fatal("jar+UA key must differ from either alone")
		}
	})
}

// mapPageCache is an in-memory PageCache for cache-divergence integration.
type mapPageCache struct {
	pages map[string]*Page
	src   map[string]string
}

func newMapPageCache() *mapPageCache {
	return &mapPageCache{pages: map[string]*Page{}, src: map[string]string{}}
}

func (m *mapPageCache) Get(url string) (*Page, string) { return m.pages[url], m.src[url] }
func (m *mapPageCache) Put(url string, p *Page, s string) {
	m.pages[url] = p
	m.src[url] = s
}
func (m *mapPageCache) GetRaw(url string) (string, string, *Page) {
	return "", m.src[url], m.pages[url]
}
func (m *mapPageCache) PutRaw(url string, p *Page, s, raw string) { m.Put(url, p, s) }

func TestCachedScrapeCookieCacheDoesNotCollide(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("<html><body>content</body></html>"))
	}))
	defer srv.Close()

	pc := newMapPageCache()
	withJar := scraperWithJar(t, writeJar(t, tabLine("127.0.0.1", "FALSE", "/", "FALSE", "0", "session", "v")))
	if _, err := withJar.CachedScrape(context.Background(), pc, srv.URL); err != nil {
		t.Fatalf("cookie scrape: %v", err)
	}

	anon := New()
	if _, err := anon.CachedScrape(context.Background(), pc, srv.URL); err != nil {
		t.Fatalf("anon scrape: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("handler hits = %d, want 2 (cookie and anon keys must not collide)", got)
	}
}

func requireLegacyBrowserConstructor(_ func(string) (BrowserConn, error)) {}

func TestNewBrowserConnLegacySignature(_ *testing.T) {
	requireLegacyBrowserConstructor(NewBrowserConn)
}

func TestRodCookieParams(t *testing.T) {
	t.Run("nil jar yields no params", func(t *testing.T) {
		if len(rodCookieParams(nil, "https://example.com/")) != 0 {
			t.Fatal("nil jar should yield no params")
		}
	})

	t.Run("unparseable url returns nil", func(t *testing.T) {
		jar, _ := cookies.Parse(strings.NewReader(tabLine("example.com", "TRUE", "/", "FALSE", "0", "s", "v")))
		if rodCookieParams(jar, "://bad url") != nil {
			t.Fatal("bad url should yield nil params")
		}
	})

	t.Run("host-only cookie sets URL not Domain", func(t *testing.T) {
		jar, _ := cookies.Parse(strings.NewReader(tabLine("example.com", "FALSE", "/app", "FALSE", "0", "s", "v")))
		params := rodCookieParams(jar, "http://example.com/app")
		if len(params) != 1 {
			t.Fatalf("got %d params, want 1", len(params))
		}
		p := params[0]
		if p.URL != "http://example.com/app" || p.Domain != "" {
			t.Fatalf("host-only param = %+v, want URL set, Domain empty", p)
		}
	})

	t.Run("domain cookie sets dotted Domain not URL", func(t *testing.T) {
		jar, _ := cookies.Parse(strings.NewReader(tabLine(".example.com", "TRUE", "/", "FALSE", "0", "s", "v")))
		params := rodCookieParams(jar, "https://sub.example.com/")
		if len(params) != 1 {
			t.Fatalf("got %d params, want 1", len(params))
		}
		if params[0].Domain != ".example.com" || params[0].URL != "" {
			t.Fatalf("domain param = %+v, want Domain=.example.com URL empty", params[0])
		}
	})

	t.Run("secure host-only uses https URL", func(t *testing.T) {
		jar, _ := cookies.Parse(strings.NewReader(tabLine("example.com", "FALSE", "/", "TRUE", "0", "s", "v")))
		params := rodCookieParams(jar, "https://example.com/")
		if !strings.HasPrefix(params[0].URL, "https://") || !params[0].Secure {
			t.Fatalf("secure host-only param = %+v", params[0])
		}
	})

	t.Run("expires and httponly carried", func(t *testing.T) {
		exp := fmt.Sprintf("%d", time.Now().Add(48*time.Hour).Unix())
		jar, _ := cookies.Parse(strings.NewReader("#HttpOnly_" + tabLine("example.com", "FALSE", "/", "FALSE", exp, "s", "v")))
		params := rodCookieParams(jar, "http://example.com/")
		if !params[0].HTTPOnly {
			t.Fatal("HTTPOnly not carried")
		}
		if params[0].Expires == proto.TimeSinceEpoch(0) {
			t.Fatal("persistent Expires should be non-zero")
		}
	})

	t.Run("session cookie has zero Expires", func(t *testing.T) {
		jar, _ := cookies.Parse(strings.NewReader(tabLine("example.com", "FALSE", "/", "FALSE", "0", "s", "v")))
		params := rodCookieParams(jar, "http://example.com/")
		if params[0].Expires != proto.TimeSinceEpoch(0) {
			t.Fatal("session cookie should have zero Expires")
		}
	})
}

func TestNewFromConfigBadCookieFile(t *testing.T) {
	_, err := NewFromConfig(&config.Config{CookieFile: "/no/such/jar.txt", CacheTTL: "1h"})
	if err == nil {
		t.Fatal("expected error for bad cookie file")
	}
	if !strings.Contains(err.Error(), "invalid cookie_file") {
		t.Fatalf("error = %q, want invalid cookie_file", err)
	}
}
