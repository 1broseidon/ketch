package docs

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"testing"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestParseProfile(t *testing.T) {
	served := mustURL(t, "https://docs.example.dev/en/stable/")
	body := []byte(`<!doctype html><html><head>
<title> Welcome to  Gizmo — Gizmo 2.1 </title>
<base href="/en/stable/api/">
<meta name="Generator" content="Sphinx 8.1">
<meta property="og:description" content="Gizmo makes gadgets.">
<meta property="og:site_name" content="Gizmo">
<meta name="readthedocs-project-slug" content="gizmo">
<script src="_static/documentation_options.js?v=1"></script>
<script>var DOCUMENTATION_OPTIONS = {VERSION: "2.1"};</script>
</head><body>
<a href="intro.html#top">Intro</a>
<a href="intro.html">Intro again</a>
<a href="https://github.com/gizmo-org/gizmo" aria-label="GitHub"><svg></svg></a>
<a href="mailto:team@example.dev">Mail</a>
<a href="../%2e%2e/admin/">Up</a>
</body></html>`)
	p := parseProfile(body, mustURL(t, "https://docs.example.dev/"), served)
	if p.title != "Welcome to Gizmo — Gizmo 2.1" || p.description != "Gizmo makes gadgets." {
		t.Errorf("title %q, description %q", p.title, p.description)
	}
	if p.meta["generator"] != "Sphinx 8.1" || p.meta["og:site_name"] != "Gizmo" || p.meta["readthedocs-project-slug"] != "gizmo" {
		t.Errorf("meta = %v", p.meta)
	}
	var links []string
	for _, l := range p.links {
		links = append(links, l.url.String()+" "+l.text)
	}
	want := []string{"https://docs.example.dev/en/stable/api/intro.html Intro", "https://github.com/gizmo-org/gizmo GitHub", "https://docs.example.dev/en/admin/ Up"}
	if !slices.Equal(links, want) {
		t.Errorf("links = %q, want %q (base href, fragments dropped, one per URL, aria-label, web links only)", links, want)
	}
	if !slices.Equal(p.scripts, []string{"https://docs.example.dev/en/stable/api/_static/documentation_options.js?v=1"}) || p.inline == "" {
		t.Errorf("scripts %v, inline %q", p.scripts, p.inline)
	}
	if p.requested.String() != "https://docs.example.dev/" || p.served != served || p.fromMarkdown {
		t.Errorf("profile URLs = %s, %s", p.requested, p.served)
	}
}

func TestRepoLinks(t *testing.T) {
	p := &siteProfile{}
	for _, s := range []string{
		"https://github.com/Pallets/Click/", "https://github.com/pallets/click/issues/", "https://www.github.com/pallets/jinja.git",
		"https://github.com/pallets", "https://github.com/sponsors/pallets", "https://gitlab.com/pallets/click", "https://github.com/orgs/pallets/repositories",
	} {
		p.links = append(p.links, siteLink{url: mustURL(t, s)})
	}
	got := repoLinks(p)
	if want := []repoKey{repo("pallets", "click"), repo("pallets", "jinja")}; !slices.Equal(got, want) {
		t.Errorf("repoLinks = %v, want %v", got, want)
	}
}

func profileOf(t *testing.T, page string, links map[string]string) *siteProfile {
	t.Helper()
	p := &siteProfile{served: mustURL(t, page), meta: map[string]string{}}
	for href, text := range links {
		p.links = append(p.links, siteLink{url: mustURL(t, href), text: text})
	}
	slices.SortFunc(p.links, func(a, b siteLink) int { return compareStrings(a.url.String(), b.url.String()) })
	return p
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func TestDocsRootsFor(t *testing.T) {
	cases := []struct {
		name  string
		page  string
		links map[string]string
		meta  map[string]string
		want  []string
	}{
		{"a docs segment beats /learn", "https://nextjs.org/", map[string]string{
			"https://nextjs.org/docs": "Docs", "https://nextjs.org/learn": "Learn", "https://nextjs.org/blog": "Blog",
			"https://github.com/vercel/next.js": "Docs on GitHub",
		}, nil, []string{"https://nextjs.org/docs/"}},
		{"/learn and /reference mean the whole host", "https://react.dev/", map[string]string{
			"https://react.dev/learn": "Learn", "https://react.dev/reference/react": "Reference", "https://react.dev/community": "Community",
		}, nil, []string{"https://react.dev/"}},
		{"another host's root", "https://astro.build/", map[string]string{
			"https://docs.astro.build/en/getting-started/": "Read the docs →", "https://astro.build/blog/": "Blog",
		}, nil, []string{"https://docs.astro.build/"}},
		{"a docs. host on the domain, whatever its text", "https://www.djangoproject.com/", map[string]string{
			"https://docs.djangoproject.com/en/6.1/": "Overview", "https://www.djangoproject.com/download/": "Download",
		}, nil, []string{"https://docs.djangoproject.com/"}},
		{"a GitHub Pages project", "https://example.dev/", map[string]string{
			"https://gizmo-org.github.io/gizmo/guide/": "Documentation",
		}, nil, []string{"https://gizmo-org.github.io/gizmo/"}},
		{"a docs path on another domain is not this site's", "https://nextjs.org/", map[string]string{
			"https://vercel.com/docs": "Vercel platform",
		}, nil, nil},
		{"the homepage itself when it is docs", "https://click.palletsprojects.com/en/stable/", map[string]string{
			"https://click.palletsprojects.com/en/stable/api/": "API",
		}, map[string]string{"generator": "Sphinx 8"}, []string{"https://click.palletsprojects.com/en/stable/"}},
		{"a marketing page with no docs link", "https://gadget.example/", map[string]string{
			"https://gadget.example/pricing": "Pricing",
		}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := profileOf(t, tc.page, tc.links)
			for k, v := range tc.meta {
				p.meta[k] = v
			}
			var got []string
			for _, r := range docsRootsFor(mustURL(t, tc.page), p) {
				got = append(got, r.String())
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("docsRootsFor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRegistrableDomain(t *testing.T) {
	for host, want := range map[string]string{
		"docs.astro.build":        "astro.build",
		"www.djangoproject.com":   "djangoproject.com",
		"pallets.github.io":       "pallets.github.io",
		"click.readthedocs.io":    "click.readthedocs.io",
		"docs.python.org.uk":      "python.org.uk",
		"Docs.Example.COM.":       "example.com",
		"github.io":               "github.io",
		"192.0.2.1":               "192.0.2.1",
		"my-app.vercel.app":       "my-app.vercel.app",
		"api.docs.my-app.web.app": "my-app.web.app",
	} {
		if got := registrableDomain(host); got != want {
			t.Errorf("registrableDomain(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestIsNonDocsHost(t *testing.T) {
	for host, want := range map[string]bool{
		"github.com": true, "www.github.com": true, "gist.github.com": true, "gitlab.com": true, "pypi.org": true,
		"www.npmjs.com": true, "crates.io": true, "discord.gg": true, "x.com": true,
		"pallets.github.io": false, "docs.rs": false, "pkg.go.dev": false, "nextjs.org": false, "box.com": false,
	} {
		if got := isNonDocsHost(host); got != want {
			t.Errorf("isNonDocsHost(%q) = %v", host, got)
		}
	}
}

func TestIsPublicWebURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://docs.astro.build/":         true,
		"http://example.com:8080/docs":      true,
		"https://pallets.github.io/click/":  true,
		"ftp://docs.example.com/":           false,
		"https://localhost/":                false,
		"http://localhost:3000/":            false,
		"https://app.localhost/":            false,
		"http://127.0.0.1/":                 false,
		"http://[::1]:8080/":                false,
		"http://10.0.0.8/docs":              false,
		"http://169.254.169.254/latest/":    false,
		"http://127.1/":                     false,
		"http://0x7f.1/":                    false,
		"http://intranet/":                  false,
		"https://printer.local/":            false,
		"https://wiki.corp.internal/":       false,
		"https://nas.lan/":                  false,
		"https://user:pw@docs.example.com/": false,
		"https:docs.example.com":            false,
		// A zone breaks net.ParseIP, but Go still dials the IPv4 address.
		"http://[::ffff:169.254.169.254%25eth0]/": false,
		"http://[fe80::1%25en0]:8080/":            false,
		"http://localhost.localdomain/":           false,
		"http://localhost.:8080/":                 false,
		"https://localhostdocs.example.com/":      true,
	} {
		if got := isPublicWebURL(mustURL(t, raw)); got != want {
			t.Errorf("isPublicWebURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

// A JS shell's raw HTML has no links of its own; the rendered page's
// Markdown links stand in, and the raw page's title is kept.
func TestProfileJSShellFallsBackToMarkdown(t *testing.T) {
	f := &fakeFetcher{
		raw:   map[string]fakeRaw{"https://www.solid.example/": html(`<!doctype html><html><head><title>SolidJS</title></head><body><div id="app"></div><a href="/cdn-cgi/content?id=x"></a></body></html>`)},
		pages: map[string]fakePage{"https://www.solid.example/": {title: "Solid", markdown: "[Docs](https://docs.solid.example/)\n[Tutorial](/tutorial)\n![logo](/logo.svg)\n"}},
	}
	p, err := NewWeb(f).profile(context.Background(), mustURL(t, "https://www.solid.example/"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.fromMarkdown || p.title != "SolidJS" || len(p.links) != 2 || p.links[0].url.String() != "https://docs.solid.example/" || p.links[0].text != "Docs" {
		t.Errorf("profile = %+v", p)
	}
}

// A stub that refreshes elsewhere on its site is read where it refreshes
// to; a missing page is ErrNotFound and is not retried in the browser.
func TestProfileRefreshAndNotFound(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://docs.stub.example/":                    html(`<!DOCTYPE html><meta http-equiv="refresh" content="0;url=/en/getting-started/">`),
		"https://docs.stub.example/en/getting-started/": html(`<!doctype html><html><head><title>Getting started | Docs</title></head><body><a href="/en/install/">Install</a></body></html>`),
	}}
	w := NewWeb(f)
	p, err := w.profile(context.Background(), mustURL(t, "https://docs.stub.example/"))
	if err != nil || p.served.String() != "https://docs.stub.example/en/getting-started/" || p.title != "Getting started | Docs" || len(p.links) != 1 {
		t.Fatalf("profile = %+v, %v", p, err)
	}
	if _, err := w.profile(context.Background(), mustURL(t, "https://docs.stub.example/missing/")); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing page = %v", err)
	}
	assertNeverFetched(t, f, "page https://docs.stub.example/missing/")
}
