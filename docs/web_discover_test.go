package docs

import (
	"context"
	"net/url"
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
	// The sitemap's pages lead; the root page's nav adds its own after them.
	if len(site.links) < 8 || !strings.HasPrefix(site.links[0].url.String(), "https://docs.djangoproject.com/en/5.2/ref/models/") {
		t.Errorf("links = %d (%v), want the 8 /en/5.2/ pages sitemap-en.xml lists first", len(site.links), site.links)
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

// linkURLs returns the links' URLs.
func linkURLs(links []webLink) []string {
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, l.url.String())
	}
	return out
}

// tokio.rs publishes no llms.txt or sitemap, and its home page links only
// to /tokio/tutorial and a blog post: the tutorial's sidebar is where the
// pages are. The root is a hub, so that one section is read for its links
// (the blog is not a docs section), and the topic pages become candidates:
// "graceful shutdown" used to come back empty, and "spawn blocking" with
// the tutorial's intro.
func TestWebHubRootReadsSectionLinks(t *testing.T) {
	w, ft := discoverWeb(t)
	root, _ := parseWebRoot("https://tokio.rs/")
	site, err := w.discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	urls := linkURLs(site.links)
	for _, want := range []string{"https://tokio.rs/tokio/topics/shutdown", "https://tokio.rs/tokio/tutorial/shared-state", "https://tokio.rs/tokio/topics/bridging"} {
		if !slices.Contains(urls, want) {
			t.Errorf("links lack %s: %v", want, urls)
		}
	}
	var pages []string
	for _, c := range requested(ft) {
		if !strings.Contains(c, "llms") && !strings.Contains(c, "sitemap") && !strings.Contains(c, "robots") {
			pages = append(pages, c)
		}
	}
	if !slices.Equal(pages, []string{"GET https://tokio.rs/", "GET https://tokio.rs/tokio/tutorial"}) {
		t.Errorf("pages read = %v, want the root and its one docs section", pages)
	}
	if got := pickCandidates(site.links, "graceful shutdown", root, 6); len(got) == 0 || got[0].url.String() != "https://tokio.rs/tokio/topics/shutdown" {
		t.Errorf("graceful shutdown candidates = %v", linkURLs(got))
	}
	if got := linkURLs(pickCandidates(site.links, "how to share state between tasks", root, 6)); len(got) == 0 || got[0] != "https://tokio.rs/tokio/tutorial/shared-state" {
		t.Errorf("shared state candidates = %v", got)
	}
}

// v2.vuejs.org's home page links to its guide, API, style guide, examples
// and cookbook. At most webMaxHubs of them are read, the best named
// first; the guide's sidebar reaches the component pages.
func TestWebHubRootReadsAtMostTwoSections(t *testing.T) {
	w, ft := discoverWeb(t)
	root, _ := parseWebRoot("https://v2.vuejs.org/")
	site, err := w.discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	calls := requested(ft)
	for _, want := range []string{"GET https://v2.vuejs.org/v2/guide/", "GET https://v2.vuejs.org/v2/api/"} {
		if !slices.Contains(calls, want) {
			t.Errorf("%s was not read: %v", want, calls)
		}
	}
	for _, c := range calls {
		if strings.Contains(c, "cookbook") || strings.Contains(c, "style-guide") || strings.Contains(c, "/eol/") {
			t.Errorf("read more than %d sections: %s", webMaxHubs, c)
		}
	}
	got := linkURLs(pickCandidates(site.links, "v-model on custom components", root, 6))
	for _, want := range []string{"https://v2.vuejs.org/v2/guide/components-custom-events", "https://v2.vuejs.org/v2/guide/components"} {
		if !slices.Contains(got, want) {
			t.Errorf("candidates lack %s: %v", want, got)
		}
	}
}

// reactrouter.com's home page links to /home ("Docs") and two upgrade
// guides. Reading /home's sidebar reaches the data loading pages, and
// "loaders" finds them by stem (loader ~ loading) where it used to land on
// the v7 upgrade guide.
func TestWebHubRootDocsLinkAndStemming(t *testing.T) {
	w, _ := discoverWeb(t)
	root, _ := parseWebRoot("https://reactrouter.com/")
	site, err := w.discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := linkURLs(pickCandidates(site.links, "loaders", root, 6))
	if len(got) < 2 || !slices.Contains(got[:2], "https://reactrouter.com/start/framework/data-loading") || !slices.Contains(got[:2], "https://reactrouter.com/start/data/data-loading") {
		t.Errorf("loaders candidates = %v", got)
	}
}

// expressjs.com's llms.txt lists only index files (guides, API, the
// whole), each a run of pages headed by "URL: …". They are read as
// discovery input, the "full" one alone, and the answer links to the
// page the text came from, never to the .txt file.
func TestWebIndexFilesAreDiscoveryInput(t *testing.T) {
	w, ft := discoverWeb(t)
	results, err := w.GetDocs(context.Background(), "https://expressjs.com/", "express error handling middleware", 4000)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture holds no rendered page to check a section's anchor
	// against, so the page itself is cited.
	if len(results) == 0 || results[0].URL != "https://expressjs.com/en/5x/guide/error-handling" {
		t.Fatalf("results = %+v", results)
	}
	for _, r := range results {
		if strings.Contains(r.URL, ".txt") {
			t.Errorf("a result links to an index file: %s", r.URL)
		}
	}
	for _, c := range requested(ft) {
		if strings.HasPrefix(c, "GET https://expressjs.com/llms/") && c != "GET https://expressjs.com/llms/full.txt" {
			t.Errorf("read %s besides the full index", c)
		}
	}
}

// react.dev's llms.txt names the page "memo"; the query says "memoize".
// The two meet by stem, and "component", in dozens of titles, weighs less
// than the rarer word: memo's page is picked first instead of three
// component pages.
func TestWebPickCandidatesStemsAndWeighs(t *testing.T) {
	w, _ := discoverWeb(t)
	root, _ := parseWebRoot("https://react.dev/")
	site, err := w.discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := linkURLs(pickCandidates(site.links, "how to memoize a component", root, 6))
	if len(got) == 0 || got[0] != "https://react.dev/reference/react/memo.md" {
		t.Errorf("candidates = %v", got)
	}
}

// Django's sitemap lists the viewcode pages (_modules/…/middleware/) of
// every module. They are source listings, not answers, and must not win
// "middleware ordering" over the middleware reference and topic guide.
func TestWebPickCandidatesSkipsSourceListings(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "discover", "docs.djangoproject.com", "sitemap-en-4.2.xml"))
	if err != nil {
		t.Fatal(err)
	}
	root, _ := parseWebRoot("https://docs.djangoproject.com/en/4.2/")
	_, locs := parseSitemap(body, webMaxURLs)
	var links []webLink
	for _, loc := range locs {
		if u, ok := root.resolve(root.u, loc); ok {
			links = append(links, webLink{url: u})
		}
	}
	got := linkURLs(pickCandidates(links, "middleware ordering", root, 6))
	for _, u := range got {
		if strings.Contains(u, "/_modules/") {
			t.Errorf("a source listing was picked: %s", u)
		}
	}
	if len(got) < 2 || !slices.Contains(got[:2], "https://docs.djangoproject.com/en/4.2/ref/middleware/") || !slices.Contains(got[:2], "https://docs.djangoproject.com/en/4.2/topics/http/middleware/") {
		t.Errorf("candidates = %v", got)
	}
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://docs.djangoproject.com/en/4.2/_modules/django/middleware/csrf/", false},
		{"https://click.palletsprojects.com/en/stable/_sources/testing.rst.txt", false},
		{"https://expressjs.com/llms/guides-5x.txt", false},
		{"https://docs.python.org/3/genindex.html", false},
		{"https://docs.rs/tokio/latest/src/tokio/task/blocking.rs.html", false},
		{"https://docs.rs/crate/tokio/latest/source/", false},
		{"https://docs.rs/tokio/latest/tokio/task/fn.spawn_blocking.html", true},
		{"https://docs.rs/source/latest/source/", true},
		{"https://docs.djangoproject.com/en/4.2/ref/middleware/", true},
		{"https://github.com/acme/src/docs/guide", true},
	} {
		u, _ := url.Parse(tc.url)
		if got := isResultLink(u); got != tc.want {
			t.Errorf("isResultLink(%s) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

// Jest's sitemap lists timer-mocks five times: under /docs/29.7/,
// /docs/30.0/, /docs/30.4/, /docs/next/ and /docs/ (the current version).
// They are one page; the current copy is the one fetched, where all three
// result slots used to go to versioned copies.
func TestWebCollapseVersionCopies(t *testing.T) {
	w, _ := discoverWeb(t)
	root, _ := parseWebRoot("https://jestjs.io/")
	site, err := w.discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var timers []string
	for _, u := range linkURLs(pickCandidates(site.links, "jest fake timers", root, 6)) {
		if strings.Contains(u, "timer-mocks") {
			timers = append(timers, u)
		}
	}
	if !slices.Equal(timers, []string{"https://jestjs.io/docs/timer-mocks"}) {
		t.Errorf("timer-mocks candidates = %v, want the current copy alone", timers)
	}
	for _, tc := range []struct {
		root  string
		links []string
		want  string
	}{
		// No current copy: the newest release, not the first listed.
		{"https://docs.test/", []string{"https://docs.test/docs/29.7/x", "https://docs.test/docs/30.4/x", "https://docs.test/docs/30.0/x", "https://docs.test/docs/next/x"}, "https://docs.test/docs/30.4/x"},
		// A stable alias beats a numbered release and the dev docs.
		{"https://docs.test/", []string{"https://docs.test/devdocs/x/", "https://docs.test/3.11.2/x/", "https://docs.test/stable/x/"}, "https://docs.test/stable/x/"},
		// The root's own version (its host names v2) beats the current copy.
		{"https://v2.docs.test/", []string{"https://v2.docs.test/x", "https://v2.docs.test/v3/x", "https://v2.docs.test/v2/x"}, "https://v2.docs.test/v2/x"},
		// English beats another locale whatever the version.
		{"https://docs.test/", []string{"https://docs.test/fr/x", "https://docs.test/en/2.x/x"}, "https://docs.test/en/2.x/x"},
	} {
		r, _ := parseWebRoot(tc.root)
		var in []scoredLink
		for _, l := range tc.links {
			u, _ := url.Parse(l)
			in = append(in, scoredLink{link: webLink{url: u}, score: 1})
		}
		out := collapseVariants(in, r)
		if len(out) != 1 || out[0].link.url.String() != tc.want {
			t.Errorf("collapse %v = %v, want %s", tc.links, out, tc.want)
		}
	}
}
