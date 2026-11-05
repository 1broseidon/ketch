package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// resultURLs lists the results' URLs, failing on any snippet that carries
// one of the forbidden texts.
func resultURLs(t *testing.T, results []Result, forbidden ...string) []string {
	t.Helper()
	var urls []string
	for _, r := range results {
		urls = append(urls, r.URL)
		for _, f := range forbidden {
			if strings.Contains(r.Snippet, f) {
				t.Errorf("result %s carries out-of-scope text %q:\n%s", r.URL, f, r.Snippet)
			}
		}
	}
	return urls
}

// A fetcher follows redirects, so the backend must check where each
// response came from: a page that redirects off the host, or out of the
// root's path on the same host, is dropped rather than returned under the
// in-scope URL that was asked for, whether it was read raw (Markdown), through
// the scrape pipeline (HTML, the browser fallback) or as llms-full.txt. A
// redirect that stays in scope is followed, and the result points where the
// text was served from.
func TestWebRedirectsOutOfScopeAreDropped(t *testing.T) {
	f := &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://redir.test/docs/llms.txt": plain(`# Redir
- [Install webhooks](/docs/install.md)
- [Webhook install guide](/docs/guide.md)
- [Webhook install retries](/docs/retries)
- [Webhook install moved](/docs/old.md)
`),
			"https://redir.test/private/notes.md": md("# Notes\n\nPrivate webhook install notes from outside the docs.\n"),
			"https://evil.test/guide.md":          md("# Guide\n\nEvil webhook install text from another host.\n"),
			"https://redir.test/docs/new.md":      md("# Moved\n\nWebhook install instructions now live here.\n"),
			// llms-full.txt redirects off the host, naming in-scope pages.
			"https://evil.test/llms-full.txt": plain("# Install webhooks\nSource: https://redir.test/docs/install\n\nEvil webhook install text in a full dump.\n"),
		},
		pages: map[string]fakePage{
			"https://evil.test/retries": {title: "Retries", markdown: "# Retries\n\nEvil webhook install retries rendered by the browser.\n"},
		},
		redirects: map[string]string{
			"https://redir.test/docs/install.md":    "https://redir.test/private/notes.md",
			"https://redir.test/docs/guide.md":      "https://evil.test/guide.md",
			"https://redir.test/docs/retries":       "https://evil.test/retries",
			"https://redir.test/docs/old.md":        "https://redir.test/docs/new.md",
			"https://redir.test/docs/llms-full.txt": "https://evil.test/llms-full.txt",
		},
	}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://redir.test/docs/", "webhook install", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	urls := resultURLs(t, results, "Private", "Evil")
	if !slices.Equal(urls, []string{"https://redir.test/docs/new"}) {
		t.Errorf("result URLs = %v, want only the in-scope redirect target", urls)
	}
	for _, c := range f.called() {
		if strings.Contains(c, "page https://redir.test/docs/install.md") || strings.Contains(c, "page https://redir.test/docs/guide.md") {
			t.Errorf("a raw fetch that redirected away was retried through the pipeline: %s", c)
		}
	}
}

// Discovery files that redirect off the host are someone else's: their
// links are not followed, and a root page that does is reported as
// not-found with the redirect named.
func TestWebDiscoveryRedirectsOffHostAreIgnored(t *testing.T) {
	t.Run("llms.txt", func(t *testing.T) {
		f := &fakeFetcher{
			raw: map[string]fakeRaw{
				"https://evil.test/llms.txt": plain("# Evil\n- [Planted](/planted.md)\n"),
				"https://r1.test/planted.md": md("# Planted\n\nPlanted page the evil llms.txt points at.\n"),
			},
			redirects: map[string]string{"https://r1.test/llms.txt": "https://evil.test/llms.txt"},
		}
		_, _ = NewWeb(f).GetDocs(context.Background(), "https://r1.test/", "planted", 1000)
		assertNeverFetched(t, f, "planted")
	})
	t.Run("sitemap", func(t *testing.T) {
		f := &fakeFetcher{
			raw: map[string]fakeRaw{
				"https://evil.test/sitemap.xml": xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://r2.test/planted</loc></url></urlset>`),
			},
			pages:     map[string]fakePage{"https://r2.test/planted": {title: "Planted", markdown: "# Planted\n\nPlanted page the evil sitemap lists.\n"}},
			redirects: map[string]string{"https://r2.test/sitemap.xml": "https://evil.test/sitemap.xml"},
		}
		_, _ = NewWeb(f).GetDocs(context.Background(), "https://r2.test/", "planted", 1000)
		assertNeverFetched(t, f, "planted")
	})
	t.Run("root page", func(t *testing.T) {
		site := func() *fakeFetcher {
			return &fakeFetcher{
				raw:       map[string]fakeRaw{"https://evil.test/docs/": md("# Evil docs\n\nEvil text about configuring logging.\n")},
				pages:     map[string]fakePage{"https://evil.test/docs/": {title: "Evil", markdown: "# Evil docs\n\nEvil text about configuring logging.\n"}},
				redirects: map[string]string{"https://r3.test/docs/": "https://evil.test/docs/"},
			}
		}
		results, err := NewWeb(site()).GetDocs(context.Background(), "https://r3.test/docs/", "configure logging", 1000)
		if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "redirects to https://evil.test/docs/") {
			t.Errorf("GetDocs on a root that redirects off-host = %+v, %v; want not-found naming the redirect", results, err)
		}
		if _, err := NewWeb(site()).ResolveLibrary(context.Background(), "https://r3.test/docs/", 1); !errors.Is(err, ErrNotFound) {
			t.Errorf("ResolveLibrary on a root that redirects off-host = %v, want not-found", err)
		}
	})
	t.Run("root page in scope", func(t *testing.T) {
		f := &fakeFetcher{
			raw:       map[string]fakeRaw{"https://r4.test/docs/intro": md("# Intro\n\nConfigure logging in logging.toml.\n")},
			redirects: map[string]string{"https://r4.test/docs/": "https://r4.test/docs/intro"},
		}
		results, err := NewWeb(f).GetDocs(context.Background(), "https://r4.test/docs/", "configure logging", 1000)
		if err != nil || len(results) != 1 || results[0].URL != "https://r4.test/docs/intro" {
			t.Errorf("results = %+v, %v; want the page at the URL it was served from", results, err)
		}
	})
}

// A response a fetcher does not say the origin of cannot be scope-checked,
// so it is not used.
func TestWebResponseWithoutURLIsDropped(t *testing.T) {
	f := &noURLFetcher{mintlifySite()}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://mint.test", "content collections", 4000)
	if len(results) != 0 || !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDocs over a fetcher that reports no URLs = %+v, %v; want nothing (not-found)", results, err)
	}
}

type noURLFetcher struct{ *fakeFetcher }

func (f *noURLFetcher) FetchRaw(ctx context.Context, url string) (RawContent, error) {
	raw, err := f.fakeFetcher.FetchRaw(ctx, url)
	raw.URL = ""
	return raw, err
}

func (f *noURLFetcher) FetchPage(ctx context.Context, url string) (PageContent, error) {
	page, err := f.fakeFetcher.FetchPage(ctx, url)
	page.URL = ""
	return page, err
}

// The fallback fetcher refuses off-host redirects itself; a same-host
// redirect out of the root's path is followed, and the backend drops it.
func TestWebFallbackFetcherDropsOutOfPrefixRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/docs/llms.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# Local\n\n- [Install](/docs/install.md)\n- [Upgrade install](/docs/upgrade.md)\n"))
	})
	mux.HandleFunc("/docs/install.md", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/private/notes.md", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/private/notes.md", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# Notes\n\nPrivate install notes outside the docs.\n"))
	})
	mux.HandleFunc("/docs/upgrade.md", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# Upgrade\n\nTo upgrade, install the new release over the old one.\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	results, err := NewWeb(nil).GetDocs(context.Background(), srv.URL+"/docs/", "install", 1000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if urls := resultURLs(t, results, "Private"); !slices.Equal(urls, []string{srv.URL + "/docs/upgrade"}) {
		t.Errorf("result URLs = %v, want only the page that stayed in /docs/", urls)
	}
}

// A percent-encoded dot segment is a dot segment to the server:
// /docs/%2e%2e/admin/install.md is /admin/install.md, outside /docs/,
// however the undecoded path reads. Links are cleaned before the scope
// check, so an in-scope one is fetched and shown at its clean URL and the
// rest are never fetched.
func TestWebScopeRejectsEncodedDotSegments(t *testing.T) {
	admin := md("# Admin install\n\nSecret admin install steps.\n")
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://dots.test/docs/llms.txt": plain(`# Dots
- [Admin install](/docs/%2e%2e/admin/install.md)
- [Admin install upper](https://dots.test/docs/%2E%2E/admin/install.md)
- [Admin install mixed](/docs/.%2e/admin/install.md)
- [Admin install param](/docs/..;/admin/install.md)
- [Admin install backslash](/docs/..%5cadmin/install.md)
- [Admin install slash](/docs/..%2Fadmin/install.md)
- [Guide install](/docs/guide/%2e%2e/install.md)
`),
		// What a server that normalizes the path would answer for each.
		"https://dots.test/docs/%2e%2e/admin/install.md": admin,
		"https://dots.test/docs/%2E%2E/admin/install.md": admin,
		"https://dots.test/docs/.%2e/admin/install.md":   admin,
		"https://dots.test/docs/..;/admin/install.md":    admin,
		"https://dots.test/docs/..%5cadmin/install.md":   admin,
		"https://dots.test/docs/..%2Fadmin/install.md":   admin,
		"https://dots.test/admin/install.md":             admin,
		"https://dots.test/docs/install.md":              md("# Install\n\nInstall the tool with the install script.\n"),
	}}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://dots.test/docs/", "install", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if urls := resultURLs(t, results, "Secret"); !slices.Equal(urls, []string{"https://dots.test/docs/install"}) {
		t.Errorf("result URLs = %v, want only the cleaned in-scope page", urls)
	}
	assertNeverFetched(t, f, "admin", "%2e", "%2E")

	root, _ := parseWebRoot("https://x.test/en/")
	for _, raw := range []string{"https://x.test/en/%2e%2e/admin", "https://x.test/en/./a", "https://x.test/en/a/../../b", "https://x.test/en/..%5cb", "https://x.test/en/..%2Fb"} {
		if u, _ := url.Parse(raw); root.contains(u) {
			t.Errorf("contains(%s) = true, want false: a dot segment escapes the prefix", raw)
		}
	}
	u, ok := root.resolve(root.u, "guide/%2E%2E/a%2Fb/./c")
	if !ok || u.String() != "https://x.test/en/a%2Fb/c" {
		t.Errorf("resolve = %v, %v; want the dot segments removed and the encoded slash kept", u, ok)
	}
}

// Sitemaps are only fetched on the root's host (or its www twin): a
// robots.txt or sitemap index pointing elsewhere names another site's
// sitemap. Relative index entries resolve against the index.
func TestWebSitemapsStayOnTheRootsHost(t *testing.T) {
	urlset := func(locs ...string) fakeRaw {
		var b strings.Builder
		for _, l := range locs {
			fmt.Fprintf(&b, "<url><loc>%s</loc></url>", l)
		}
		return xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + b.String() + `</urlset>`)
	}
	f := &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://maps.test/robots.txt":  plain("Sitemap: https://evil.test/sitemap.xml\nSitemap: https://maps.test/sitemap-index.xml\n"),
			"https://evil.test/sitemap.xml": urlset("https://maps.test/docs/planted-routing"),
			"https://maps.test/sitemap-index.xml": xmlDoc(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<sitemap><loc>https://evil.test/sitemap-docs.xml</loc></sitemap>
<sitemap><loc>https://www.maps.test/sitemap-docs.xml</loc></sitemap>
<sitemap><loc>/sitemap-guides.xml</loc></sitemap>
</sitemapindex>`),
			"https://evil.test/sitemap-docs.xml":     urlset("https://maps.test/docs/planted-routing"),
			"https://www.maps.test/sitemap-docs.xml": urlset("https://maps.test/docs/routing"),
			"https://maps.test/sitemap-guides.xml":   urlset("https://maps.test/docs/guides/routing-advanced"),
		},
		pages: map[string]fakePage{
			"https://maps.test/docs/routing":                 {title: "Routing", markdown: "# Routing\n\nRoutes come from files in src/pages.\n"},
			"https://maps.test/docs/guides/routing-advanced": {title: "Advanced routing", markdown: "# Advanced routing\n\nDynamic routes use bracketed file names.\n"},
			"https://maps.test/docs/planted-routing":         {title: "Planted", markdown: "# Planted routing\n\nPlanted by another host's sitemap.\n"},
		},
	}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://maps.test/docs/", "routing", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	urls := resultURLs(t, results, "Planted")
	for _, want := range []string{"https://maps.test/docs/routing", "https://maps.test/docs/guides/routing-advanced"} {
		if !slices.Contains(urls, want) {
			t.Errorf("result URLs = %v, missing %s from an on-host sitemap", urls, want)
		}
	}
	assertNeverFetched(t, f, "evil.test", "planted")
}

// llms-full.txt chunks without URL lines are attributed by title only when
// the title is unambiguous: one llms.txt page has it and one chunk carries
// it. A chunk whose title names a page outside the root is that page's and
// is dropped. When too little can be attributed, the listed pages are
// fetched instead.
func TestWebFullTextTitleAttribution(t *testing.T) {
	t.Run("shared title falls back to pages", func(t *testing.T) {
		f := &fakeFetcher{raw: map[string]fakeRaw{
			"https://two.test/a/llms.txt": plain("# Products\n- [Introduction](/a/intro.md)\n- [Introduction](/b/intro.md)\n- [Setup](/b/setup.md)\n- [Usage](/a/usage.md)\n"),
			"https://two.test/a/llms-full.txt": plain("# Introduction\n\nAlpha widgets get you started.\n\n" +
				"# Introduction\n\nBeta widgets get you started.\n\n" +
				"# Setup\n\nBeta widgets need a setup step.\n\n" +
				"# Usage\n\nAlpha widgets are used daily.\n"),
			"https://two.test/a/intro.md": md("# Introduction\n\nAlpha widgets, fetched as a page.\n"),
			"https://two.test/a/usage.md": md("# Usage\n\nAlpha widgets in daily use, fetched as a page.\n"),
		}}
		results, err := NewWeb(f).GetDocs(context.Background(), "https://two.test/a/", "widgets", 4000)
		if err != nil {
			t.Fatalf("GetDocs: %v", err)
		}
		urls := resultURLs(t, results, "Beta")
		if !slices.Contains(urls, "https://two.test/a/intro") || !strings.Contains(results[0].Snippet, "fetched as a page") {
			t.Errorf("results = %+v, want product A's pages fetched one by one", results)
		}
	})
	t.Run("title of an out-of-scope page drops its chunk", func(t *testing.T) {
		f := &fakeFetcher{raw: map[string]fakeRaw{
			"https://two.test/a/llms.txt":      plain("# Products\n- [Setup](/b/setup.md)\n- [Intro](/a/intro.md)\n- [Usage](/a/usage.md)\n"),
			"https://two.test/a/llms-full.txt": plain("# Intro\n\nAlpha widgets get you started.\n\n# Usage\n\nAlpha widgets are used daily.\n\n# Setup\n\nBeta widgets need a setup step.\n"),
		}}
		results, err := NewWeb(f).GetDocs(context.Background(), "https://two.test/a/", "widgets", 4000)
		if err != nil {
			t.Fatalf("GetDocs: %v", err)
		}
		if urls := resultURLs(t, results, "Beta"); !slices.Equal(urls, []string{"https://two.test/a/intro", "https://two.test/a/usage"}) {
			t.Errorf("result URLs = %v, want A's two chunks", urls)
		}
		assertNeverFetched(t, f, ".md")
	})
	t.Run("title on two chunks attributes neither", func(t *testing.T) {
		f := &fakeFetcher{raw: map[string]fakeRaw{
			"https://two.test/a/llms.txt": plain("# Products\n- [Introduction](/a/intro.md)\n- [Usage](/a/usage.md)\n- [Config](/a/config.md)\n"),
			"https://two.test/a/llms-full.txt": plain("# Introduction\n\nAlpha widgets get you started.\n\n# Introduction\n\nGamma widgets from an unlisted page.\n\n" +
				"# Usage\n\nAlpha widgets are used daily.\n\n# Config\n\nAlpha widgets read widget.toml.\n"),
		}}
		results, err := NewWeb(f).GetDocs(context.Background(), "https://two.test/a/", "widgets", 4000)
		if err != nil {
			t.Fatalf("GetDocs: %v", err)
		}
		for _, r := range results {
			if r.URL == "https://two.test/a/intro" {
				t.Errorf("a chunk was attributed to /a/intro by a title two chunks share: %+v", r)
			}
			if strings.Contains(r.Snippet, "Gamma") && r.URL != "https://two.test/a/llms-full.txt" {
				t.Errorf("the unlisted page's chunk is shown at %s, not where its text came from", r.URL)
			}
		}
	})
}

// timeoutFetcher fails URLs the way their own timeouts do while the
// caller's context is live: hang waits for the context it was given (the
// per-probe timeout fires) and timeout fails at once like an http.Client
// timeout. Both wrap context.DeadlineExceeded. Keys are "raw <url>" and
// "page <url>"; every other URL goes to the fake.
type timeoutFetcher struct {
	*fakeFetcher
	hang, timeout map[string]bool
}

func (f *timeoutFetcher) fail(ctx context.Context, key string) error {
	switch {
	case f.hang[key]:
		<-ctx.Done()
		return fmt.Errorf("fetch failed: %w", ctx.Err())
	case f.timeout[key]:
		return fmt.Errorf("fetch failed: Get %q: net/http: request canceled (Client.Timeout exceeded): %w", key, context.DeadlineExceeded)
	}
	return nil
}

func (f *timeoutFetcher) FetchRaw(ctx context.Context, url string) (RawContent, error) {
	if err := f.fail(ctx, "raw "+url); err != nil {
		return RawContent{}, err
	}
	return f.fakeFetcher.FetchRaw(ctx, url)
}

func (f *timeoutFetcher) FetchPage(ctx context.Context, url string) (PageContent, error) {
	if err := f.fail(ctx, "page "+url); err != nil {
		return PageContent{}, err
	}
	return f.fakeFetcher.FetchPage(ctx, url)
}

// A fetch that runs out of its own time (the probe timeout, the HTTP
// client's) while the call has time left is an upstream failure worth
// retrying, not a cancellation: the error must not wrap
// context.DeadlineExceeded, which MCP reports as [cancelled] while the CLI
// exits 4.
func TestWebFetchTimeoutsAreUpstream(t *testing.T) {
	check := func(t *testing.T, what string, err error) {
		t.Helper()
		if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, ErrNotFound) {
			t.Errorf("%s = %v; want a plain upstream error, not a cancellation or not-found", what, err)
		} else if !strings.Contains(err.Error(), "deadline exceeded") {
			t.Errorf("%s = %v; want the timeout's text kept", what, err)
		}
	}
	t.Run("root page", func(t *testing.T) {
		f := &timeoutFetcher{
			fakeFetcher: &fakeFetcher{},
			hang:        map[string]bool{"raw https://slow.test/docs/": true},
			timeout:     map[string]bool{"page https://slow.test/docs/": true},
		}
		w := NewWeb(f)
		w.probeTimeout = 20 * time.Millisecond
		_, err := w.GetDocs(context.Background(), "https://slow.test/docs/", "anything", 100)
		check(t, "GetDocs", err)
		_, err = w.ResolveLibrary(context.Background(), "https://slow.test/docs/", 1)
		check(t, "ResolveLibrary", err)
	})
	t.Run("every candidate page", func(t *testing.T) {
		f := &timeoutFetcher{
			fakeFetcher: &fakeFetcher{raw: map[string]fakeRaw{"https://slow.test/llms.txt": plain("# Hooks\n\n- [Webhooks](/webhooks.md)\n- [Webhook retries](/webhook-retries.md)\n")}},
			timeout:     map[string]bool{"raw https://slow.test/webhooks.md": true, "raw https://slow.test/webhook-retries.md": true},
		}
		_, err := NewWeb(f).GetDocs(context.Background(), "https://slow.test/", "webhooks", 100)
		check(t, "GetDocs", err)
	})
}

// A root naming a file is scoped to the directory holding it, like one
// naming llms.txt; a segment with a version-like dot stays a directory.
func TestWebRootNamingAFileUsesItsDirectory(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://docs.example.com/guide/index.html", "https://docs.example.com/guide/"},
		{"https://docs.example.com/guide/intro.htm", "https://docs.example.com/guide/"},
		{"https://x.test/manual/install.md", "https://x.test/manual/"},
		{"https://x.test/index.php", "https://x.test/"},
		{"https://x.test/docs/INDEX.HTML", "https://x.test/docs/"},
		{"https://x.test/docs/2.x", "https://x.test/docs/2.x/"},
		{"https://x.test/docs/v3.1", "https://x.test/docs/v3.1/"},
		{"https://x.test/docs/%2e%2e/admin/", "https://x.test/admin/"},
	} {
		root, err := parseWebRoot(tc.in)
		if err != nil || root.String() != tc.want || root.prefix != strings.TrimPrefix(tc.want, "https://"+root.u.Host) {
			t.Errorf("parseWebRoot(%q) = %q (prefix %q), %v; want %q", tc.in, root.String(), root.prefix, err, tc.want)
		}
	}
}

// When both reads of the root page fail, the pipeline's error stands: it
// ran under the call's deadline, while the raw read ran under the probe's.
// A probe that timed out does not hide that the page is missing.
func TestWebRootPageErrorIsThePipelines(t *testing.T) {
	f := &timeoutFetcher{fakeFetcher: &fakeFetcher{}, hang: map[string]bool{"raw https://gone.test/docs/": true}}
	w := NewWeb(f)
	w.probeTimeout = 20 * time.Millisecond
	if _, err := w.GetDocs(context.Background(), "https://gone.test/docs/", "anything", 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDocs = %v; want not-found from the page fetch's 404", err)
	}
}
