package ingest

import (
	"context"
	"errors"
	"github.com/1broseidon/ketch/docstore"
	"strings"
	"sync"
	"testing"
)

func openTestStore(t *testing.T) *docstore.Store {
	t.Helper()
	s, err := docstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAddFromSitemap(t *testing.T) {
	s := newSite(t).
		xml("/sitemap.xml", sitemapXML("/docs/colors", "/docs/container-queries", "/docs/missing", "/blog/post")).
		html("/docs/colors", "Colors", "<p>Utilities for controlling color.</p><h2>Customizing</h2><p>Extend the palette.</p>").
		html("/docs/container-queries", "Container queries", "<p>Style based on the parent size.</p>")
	store := openTestStore(t)

	var mu sync.Mutex
	var seen []string
	sum, err := Add(context.Background(), store, testScraper(), nil, AddOptions{
		Name: "tw", Version: "4", Seed: s.url("/docs"),
		Progress: func(u string, _ error) { mu.Lock(); seen = append(seen, u); mu.Unlock() },
	})
	if err != nil {
		t.Fatalf("add: %v (summary %+v)", err, sum)
	}
	if sum.Plan.Source != docstore.SourceSitemap || sum.Fetched != 2 || sum.Failed != 1 || sum.Stopped != "" {
		t.Fatalf("summary = %+v errors=%v", sum, sum.Errors)
	}
	if len(seen) != 3 {
		t.Errorf("progress calls = %d, want 3", len(seen))
	}
	if sum.Library == nil || sum.Library.Pages != 2 || sum.Library.Version != "4" || sum.Library.Prefix != "/docs" {
		t.Fatalf("library = %+v", sum.Library)
	}
	hits, err := store.Search(context.Background(), docstore.Query{Text: "customizing palette"})
	if err != nil || len(hits) == 0 {
		t.Fatalf("search = %v %v", hits, err)
	}
	if hits[0].Title != "Customizing" || !strings.HasSuffix(hits[0].URL, "/docs/colors#customizing") {
		t.Errorf("top hit = %+v", hits[0])
	}
}

func TestAddDryRunWritesNothing(t *testing.T) {
	s := newSite(t).xml("/sitemap.xml", sitemapXML("/docs/a", "/docs/b", "/docs/c"))
	store := openTestStore(t)
	sum, err := Add(context.Background(), store, testScraper(), nil, AddOptions{Name: "x", Seed: s.url("/docs"), DryRun: true, MaxPages: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.DryRun || sum.Library != nil || sum.Fetched != 0 || len(sum.Plan.URLs) != 3 || sum.Stopped != StoppedMaxPages {
		t.Fatalf("summary = %+v", sum)
	}
	if libs, _ := store.Libraries(); len(libs) != 0 {
		t.Errorf("dry run stored %+v", libs)
	}
}

func TestAddHonoursMaxPagesOnLists(t *testing.T) {
	s := newSite(t).
		xml("/sitemap.xml", sitemapXML("/d/a", "/d/b", "/d/c")).
		html("/d/a", "A", "<p>alpha</p>").html("/d/b", "B", "<p>beta</p>").html("/d/c", "C", "<p>gamma</p>")
	store := openTestStore(t)
	sum, err := Add(context.Background(), store, testScraper(), nil, AddOptions{Name: "x", Seed: s.url("/d"), MaxPages: 2})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Fetched != 2 || sum.Stopped != StoppedMaxPages || sum.Library.Pages != 2 {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestAddFromLLMSFull(t *testing.T) {
	doc := "# Glamour\n\nRenders markdown.\n\n## Installation\n\ngo get glamour and then some more words to pass the size check for a real document body here.\n\n## Styles\n\nPick a style.\n" + strings.Repeat("\nfiller text\n", 20)
	s := newSite(t).text("/llms-full.txt", doc)
	store := openTestStore(t)
	sum, err := Add(context.Background(), store, testScraper(), nil, AddOptions{Name: "glamour", Seed: s.url("/")})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Plan.Source != docstore.SourceLLMSFull || sum.Fetched != 0 || sum.Library.Pages != 1 || sum.Library.Sections < 3 {
		t.Fatalf("summary = %+v lib=%+v", sum, sum.Library)
	}
	hits, _ := store.Search(context.Background(), docstore.Query{Text: "installation"})
	if len(hits) == 0 || hits[0].URL != s.url("/llms-full.txt")+"#installation" || hits[0].PageTitle != "Glamour" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestAddFromLLMSListTakesMarkdownVerbatim(t *testing.T) {
	s := newSite(t).
		text("/llms.txt", "- [A]({{origin}}/docs/a.md)\n- [B]({{origin}}/docs/b.md)\n").
		text("/docs/a.md", "# Alpha page\n\nThe **alpha** body with `code`.\n").
		text("/docs/b.md", "no heading here, just prose about beta\n")
	store := openTestStore(t)
	sum, err := Add(context.Background(), store, testScraper(), nil, AddOptions{Name: "x", Seed: s.url("/docs")})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Plan.Source != docstore.SourceLLMS || sum.Fetched != 2 {
		t.Fatalf("summary = %+v", sum)
	}
	hits, _ := store.Search(context.Background(), docstore.Query{Text: "alpha"})
	if len(hits) == 0 || hits[0].PageTitle != "Alpha page" || !strings.Contains(hits[0].Body, "**alpha**") {
		t.Fatalf("markdown was not kept verbatim: %+v", hits)
	}
	hits, _ = store.Search(context.Background(), docstore.Query{Text: "beta"})
	if len(hits) == 0 || hits[0].PageTitle != "b" {
		t.Fatalf("headingless page title should fall back to the path: %+v", hits)
	}
}

func TestAddFromCrawlScopedToPrefix(t *testing.T) {
	s := newSite(t).
		html("/docs", "Docs", `<p>root</p><a href="/docs/a">a</a> <a href="/docs/b">b</a> <a href="/blog/x">blog</a> <a href="/docs-old/z">old</a>`).
		html("/docs/a", "A", `<p>alpha content</p><a href="/docs/b">b</a>`).
		html("/docs/b", "B", `<p>beta content</p>`).
		html("/blog/x", "Blog", `<p>never fetched</p>`).
		html("/docs-old/z", "Old", `<p>substring trap</p>`)
	store := openTestStore(t)
	sum, err := Add(context.Background(), store, testScraper(), nil, AddOptions{Name: "x", Seed: s.url("/docs")})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Plan.Source != docstore.SourceCrawl || sum.Fetched != 3 || sum.Stopped != "" {
		t.Fatalf("summary = %+v errors=%v", sum, sum.Errors)
	}
	for _, q := range []string{"never", "substring"} {
		if hits, _ := store.Search(context.Background(), docstore.Query{Text: q}); len(hits) != 0 {
			t.Errorf("out-of-scope page indexed for %q: %+v", q, hits)
		}
	}
}

func TestAddCrawlStopsAtMaxPages(t *testing.T) {
	s := newSite(t)
	var links strings.Builder
	for i := 0; i < 10; i++ {
		p := "/d/p" + string(rune('a'+i))
		links.WriteString(`<a href="` + p + `">x</a>`)
		s.html(p, "P", "<p>page body</p>")
	}
	s.html("/d", "D", "<p>root</p>"+links.String())
	store := openTestStore(t)
	sum, err := Add(context.Background(), store, testScraper(), nil, AddOptions{Name: "x", Seed: s.url("/d"), MaxPages: 3, Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Stopped != StoppedMaxPages || sum.Library.Pages != 3 {
		t.Fatalf("summary = %+v lib=%+v", sum, sum.Library)
	}
}

// A JS-shell page that still ships server-side text (the Tailwind docs
// shape) is indexed from what the HTML carried, and the summary counts it so
// an agent knows the corpus may be partial without a browser.
func TestAddCountsUnrenderedShells(t *testing.T) {
	// App Router shell: a little server-side chrome in <main>, an empty mount
	// point beside it, and an RSC payload dwarfing the visible text.
	shell := `<!doctype html><html><head><title>A</title></head><body>` +
		`<nav><ul><li>Docs</li></ul></nav><main><p>alpha server-rendered summary text</p></main><div id="__next"></div><script>` +
		strings.Repeat(`self.__next_f.push([1,"a:[\"$\",\"tr\",null,{\"children\":\"row streamed via rsc\"}]"]);`, 120) +
		`</script></body></html>`
	s := newSite(t).
		xml("/sitemap.xml", sitemapXML("/d/a", "/d/b")).
		page("/d/a", shell).
		html("/d/b", "B", "<p>beta is a plain page with enough words to be indexed as content</p>")
	store := openTestStore(t)
	sum, err := Add(context.Background(), store, testScraper(), nil, AddOptions{Name: "x", Seed: s.url("/d")})
	if err != nil {
		t.Fatalf("add: %v (summary %+v)", err, sum)
	}
	if sum.Fetched != 2 || sum.Unrendered != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if hits, _ := store.Search(context.Background(), docstore.Query{Text: "alpha"}); len(hits) == 0 {
		t.Fatal("shell page's server-side text was not indexed")
	}

	// The crawl path reports the same count through crawl.Result.FetchSource.
	c := newSite(t).
		html("/c", "C", `<p>root</p><a href="/c/shell">s</a>`).
		page("/c/shell", shell)
	sum, err = Add(context.Background(), openTestStore(t), testScraper(), nil, AddOptions{Name: "y", Seed: c.url("/c")})
	if err != nil {
		t.Fatalf("crawl add: %v (summary %+v)", err, sum)
	}
	if sum.Plan.Source != docstore.SourceCrawl || sum.Fetched != 2 || sum.Unrendered != 1 {
		t.Fatalf("crawl summary = %+v", sum)
	}
}

func TestAddNothingIndexableIsErrEmpty(t *testing.T) {
	s := newSite(t).xml("/sitemap.xml", sitemapXML("/d/a", "/d/b"))
	store := openTestStore(t)
	sum, err := Add(context.Background(), store, testScraper(), nil, AddOptions{Name: "x", Seed: s.url("/d")})
	if !errors.Is(err, docstore.ErrEmpty) {
		t.Fatalf("err = %v, want docstore.ErrEmpty", err)
	}
	if sum == nil || sum.Failed != 2 {
		t.Fatalf("summary = %+v", sum)
	}
	if libs, _ := store.Libraries(); len(libs) != 0 {
		t.Errorf("empty add stored %+v", libs)
	}
}

func TestAddValidatesName(t *testing.T) {
	store := openTestStore(t)
	if _, err := Add(context.Background(), store, testScraper(), nil, AddOptions{Name: "Bad Name", Seed: "https://x.test"}); err == nil {
		t.Fatal("bad name accepted")
	}
}

func TestAddCancelled(t *testing.T) {
	s := newSite(t).xml("/sitemap.xml", sitemapXML("/d/a")).html("/d/a", "A", "<p>x</p>")
	store := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Add(ctx, store, testScraper(), nil, AddOptions{Name: "x", Seed: s.url("/d")})
	if err == nil {
		t.Fatal("cancelled add succeeded")
	}
}

func TestMarkdownTitle(t *testing.T) {
	cases := []struct{ body, url, want string }{
		{"# Hello\n\nbody", "https://x.test/a.md", "Hello"},
		{"intro\n\n# Later heading\n", "https://x.test/a.md", "Later heading"},
		{"no heading", "https://x.test/docs/getting-started.md", "getting-started"},
		{"no heading", "https://x.test/", "x.test"},
		{"## not h1\n", "https://x.test/p/q/", "q"},
	}
	for _, c := range cases {
		if got := markdownTitle(c.body, c.url); got != c.want {
			t.Errorf("markdownTitle(%q, %q) = %q, want %q", c.body, c.url, got, c.want)
		}
	}
}
