package docs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/testutil"
)

// discoverWeb is a backend over the recorded discovery fixtures in
// testdata/discover: docs.djangoproject.com's robots.txt, its sitemap index
// of twelve per-locale sitemaps (only sitemap-en.xml recorded, trimmed),
// and the /en/5.2/ root page.
func discoverWeb(t *testing.T) (*Web, *testutil.FixtureTransport) {
	t.Helper()
	ft := testutil.LoadFixtures(t, filepath.Join("testdata", "discover"))
	return NewWeb(&fixtureFetcher{client: ft.Client()}), ft
}

// requested lists the URLs the transport was asked for.
func requested(ft *testutil.FixtureTransport) []string {
	var out []string
	for _, c := range ft.Calls() {
		out = append(out, c.String())
	}
	return out
}

// Django's sitemap index lists a sitemap per locale. A root under /en/
// reads sitemap-en.xml only: walking the others in turn (el, es, fr …, up
// to the file cap) is what made `django@5.2 …` take 30s while Django's
// CDN was cold, for pages outside the root anyway.
func TestWebSitemapSkipsOtherLocales(t *testing.T) {
	w, ft := discoverWeb(t)
	root, _ := parseWebRoot("https://docs.djangoproject.com/en/5.2/")
	site, err := w.discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(site.links) != 8 || !strings.HasPrefix(site.links[0].url.String(), "https://docs.djangoproject.com/en/5.2/ref/models/") {
		t.Errorf("links = %d (%v), want the 8 /en/5.2/ pages sitemap-en.xml lists", len(site.links), site.links)
	}
	calls := requested(ft)
	if !slices.Contains(calls, "GET https://docs.djangoproject.com/sitemap-en.xml") {
		t.Errorf("sitemap-en.xml was not read: %v", calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "/sitemap-") && !strings.HasSuffix(c, "/sitemap-en.xml") {
			t.Errorf("another locale's sitemap was read: %s", c)
		}
	}
}

// The sitemaps get sitemapBudget in all. A sitemap that does not arrive
// in time is given up, and the root page's links answer instead, long
// before the per-probe timeout.
func TestWebSitemapBudget(t *testing.T) {
	w, ft := discoverWeb(t)
	body, err := os.ReadFile(filepath.Join("testdata", "discover", "docs.djangoproject.com", "sitemap-en.xml"))
	if err != nil {
		t.Fatal(err)
	}
	ft.Handle("GET", "https://docs.djangoproject.com/sitemap-en.xml", testutil.Fixture{Body: string(body), Delay: time.Minute})
	w.sitemapBudget = 100 * time.Millisecond
	root, _ := parseWebRoot("https://docs.djangoproject.com/en/5.2/")
	start := time.Now()
	site, err := w.discover(context.Background(), root)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("discover took %s with a %s sitemap budget", elapsed, w.sitemapBudget)
	}
	if err != nil || len(site.links) < 2 || site.links[0].url.String() != "https://docs.djangoproject.com/en/5.2/" {
		t.Fatalf("discover = %v, %v; want the root page and its links", site.links, err)
	}
	if !slices.Contains(site.tried, "links on https://docs.djangoproject.com/en/5.2/") {
		t.Errorf("tried = %v, want the root page's links after the sitemaps", site.tried)
	}
}

func TestOwnLocale(t *testing.T) {
	index := []string{
		"https://docs.djangoproject.com/sitemap-el.xml", "https://docs.djangoproject.com/sitemap-en.xml",
		"https://docs.djangoproject.com/sitemap-pt-br.xml", "https://docs.djangoproject.com/sitemap-zh-hans.xml.gz",
		"https://docs.djangoproject.com/fr/sitemap.xml", "https://docs.djangoproject.com/sitemap-docs.xml",
	}
	cases := []struct {
		root string
		want []string
	}{
		{"https://docs.djangoproject.com/en/5.2/", []string{index[1], index[5]}},
		{"https://docs.djangoproject.com/fr/5.2/", []string{index[4], index[5]}},
		{"https://docs.djangoproject.com/pt-br/5.2/", []string{index[2], index[5]}},
		// No locale in the root, or none of the index's for it: all kept.
		{"https://docs.djangoproject.com/", index},
		{"https://docs.djangoproject.com/de/5.2/", index},
		{"https://docs.djangoproject.com/docs/", index},
	}
	for _, tc := range cases {
		root, _ := parseWebRoot(tc.root)
		if got := ownLocale(index, root); !slices.Equal(got, tc.want) {
			t.Errorf("ownLocale(%s) = %v, want %v", tc.root, got, tc.want)
		}
	}
}
