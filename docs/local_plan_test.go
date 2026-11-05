package docs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
)

func linksOf(t *testing.T, raws ...string) []webLink {
	t.Helper()
	var out []webLink
	for _, r := range raws {
		u, err := url.Parse(r)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, webLink{url: u})
	}
	return out
}

func urlsOf(links []webLink) []string {
	var out []string
	for _, l := range links {
		out = append(out, l.url.String())
	}
	return out
}

// Under the cap, the pages llms.txt names come first, then guides, then
// the rest (a gallery included), then generated API reference leaves and
// release notes; within a tier, top-level directories take turns.
func TestPlanPriority(t *testing.T) {
	root, _ := parseWebRoot("https://x.test/doc/stable/")
	b := "https://x.test/doc/stable/"
	links := linksOf(t,
		b+"reference/generated/x.a.html", b+"reference/generated/x.b.html", b+"api/_as_gen/x.c.html",
		b+"release/notes-2.0.html", b+"changelog.html", b+"gallery/axes/demo.html", b+"reference/arrays.html",
		b+"user/basics.broadcasting.html", b+"user/quickstart.html", b+"tutorials/intro.html", b+"index.html",
		b+"api/core/Component", b+"api/index.html")
	curated := map[string]bool{displayKey(links[10].url): true}
	got := urlsOf(prioritize(links, root, curated))
	want := []string{b + "index.html",
		b + "user/basics.broadcasting.html", b + "tutorials/intro.html", b + "user/quickstart.html",
		b + "release/notes-2.0.html", b + "gallery/axes/demo.html", b + "reference/arrays.html", b + "api/index.html",
		b + "reference/generated/x.a.html", b + "api/_as_gen/x.c.html", b + "changelog.html", b + "reference/generated/x.b.html", b + "api/core/Component"}
	if !slices.Equal(got, want) {
		t.Errorf("order:\n got %v\nwant %v", got, want)
	}
}

// One page served as x and x.html is one page, and an llms.txt text
// mirror under /llms/ is left out beside the rendered pages.
func TestPlanTwinsAndMirrors(t *testing.T) {
	root, _ := parseWebRoot("https://m.test/")
	links := uniquePages(linksOf(t, "https://m.test/guide/computed.html", "https://m.test/guide/computed", "https://m.test/llms/computed", "https://m.test/guide/watch"))
	got := urlsOf(dropMirrors(links, root))
	if !slices.Equal(got, []string{"https://m.test/guide/computed.html", "https://m.test/guide/watch"}) {
		t.Errorf("kept %v", got)
	}
	only := linksOf(t, "https://m.test/llms/a", "https://m.test/llms/b")
	if len(dropMirrors(only, root)) != 2 {
		t.Error("a site of mirrors only keeps them")
	}
}

// An unversioned root whose pages sit in release trees moves to the
// release llms.txt lists (or, with no list, the newest left out); a
// versioned root, or one whose own tree holds the docs, stays.
func TestRerootTree(t *testing.T) {
	pages := func(prefix string, n int) []string {
		var out []string
		for i := range n {
			out = append(out, prefix+"page-"+string(rune('a'+i%26))+strings.Repeat("x", i/26))
		}
		return out
	}
	check := func(name, rootURL string, urls []string, curatedPrefix, want string) {
		t.Helper()
		root, _ := parseWebRoot(rootURL)
		links := uniquePages(linksOf(t, urls...))
		curated := map[string]bool{}
		for _, l := range links {
			if curatedPrefix != "" && strings.HasPrefix(l.url.String(), curatedPrefix) {
				curated[displayKey(l.url)] = true
			}
		}
		kept, _, dropped := keepOneVariant(links, root, nil)
		got, _, ok := rerootTree(root, links, indexPlan{listed: len(links), links: kept, dropped: dropped}, curated)
		if ok != (want != "") || ok && got.String() != want {
			t.Errorf("%s: reroot = %v %v, want %q", name, got, ok, want)
		}
	}
	hf := "https://h.test/docs/lib/"
	check("only a release listed", hf, append(pages(hf+"v5.22.1/", 30), hf, hf+"index"), hf+"v5.22.1/", hf+"v5.22.1/")
	cw := "https://c.test/"
	crew := append(append(pages(cw+"edge/en/", 40), pages(cw+"v1.15.35/en/", 30)...), pages(cw+"v1.15.34/en/", 30)...)
	check("llms.txt names one release", cw, crew, cw+"v1.15.35/", cw+"v1.15.35/")
	check("newest release with no list", "https://d.test/docs/", append(append(pages("https://d.test/docs/2.0/", 20), pages("https://d.test/docs/1.5/", 20)...), "https://d.test/docs/", "https://d.test/docs/about"), "", "https://d.test/docs/2.0/")
	check("one release tree is the docs", "https://d.test/docs/", append(pages("https://d.test/docs/2.0/", 20), pages("https://d.test/docs/1.5/", 20)...), "", "")
	check("a versioned root stays", "https://d.test/docs/1.5/", pages("https://d.test/docs/1.5/", 20), "", "")
	check("its own tree holds the docs", "https://d.test/docs/", append(pages("https://d.test/docs/", 40), pages("https://d.test/docs/1.5/", 12)...), "", "")
}

// A channel or unversioned root whose page redirects, refreshes or names
// as canonical a numbered release's root is indexed there; a numbered
// root never moves (Read the Docs names the default version canonical).
func TestAliasRoot(t *testing.T) {
	stub := `<html><head><link rel="canonical" href="https://d.test/docs/2.0/"><meta http-equiv="refresh" content="0; url=https://d.test/docs/2.0/"></head></html>`
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://d.test/docs/stable/": html(stub),
		"https://d.test/docs/1.4/":    html(`<html><head><link rel="canonical" href="https://d.test/docs/stable/"></head></html>`),
		"https://r.test/en/latest/":   html(`<html><head><link rel="canonical" href="https://r.test/en/stable/"></head></html>`),
	}}
	w := NewWeb(f)
	for _, c := range []struct{ root, want string }{
		{"https://d.test/docs/stable/", "https://d.test/docs/2.0/"},
		{"https://d.test/docs/1.4/", ""},
		{"https://r.test/en/latest/", ""},
	} {
		root, _ := parseWebRoot(c.root)
		got, ok := w.aliasRoot(context.Background(), root)
		if ok != (c.want != "") || ok && got.String() != c.want {
			t.Errorf("aliasRoot(%s) = %v %v, want %q", c.root, got, ok, c.want)
		}
	}
}

// A version's own host borrows the current host's paths.
func TestSiblingHost(t *testing.T) {
	root, _ := parseWebRoot("https://v18.ng.test/")
	sib, ok := siblingHost(root)
	if !ok || sib.String() != "https://ng.test/" {
		t.Fatalf("sibling = %v %v", sib, ok)
	}
	moved := atHost(linksOf(t, "https://ng.test/guide/zoneless"), root.u.Host)
	if moved[0].url.String() != "https://v18.ng.test/guide/zoneless" {
		t.Errorf("moved = %v", moved[0].url)
	}
	if _, ok := siblingHost(mustRoot(t, "https://docs.ng.test/")); ok {
		t.Error("a host naming no version has no sibling")
	}
}

// Pages with the same text are one (a missing page served as the home
// page, a page at two addresses).
func TestUniqueContent(t *testing.T) {
	pages, copies := uniqueContent([]webPage{
		{URL: "https://x.test/", Markdown: "# Home\n\nWelcome."},
		{URL: "https://x.test/a", Markdown: "# A\n\nAbout a."},
		{URL: "https://x.test/missing", Markdown: "# Home\n\n Welcome."},
	})
	if copies != 1 || len(pages) != 2 || pages[1].URL != "https://x.test/a" {
		t.Errorf("pages %v, %d copies", pages, copies)
	}
}

// A re-rooted current release is shown under the unversioned addresses
// its pages name canonical; another release never is.
func TestDisplayRoot(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://h.test/docs/lib/v5.2/chat": html(`<html><head><link rel="canonical" href="https://h.test/docs/lib/chat"></head></html>`),
		"https://h.test/docs/lib/v4.9/chat": html(`<html><head><link rel="canonical" href="https://h.test/docs/lib/v4.9/chat"></head></html>`),
	}}
	w := NewWeb(f)
	from := mustRoot(t, "https://h.test/docs/lib/")
	sample, _ := url.Parse("https://h.test/docs/lib/v5.2/chat")
	if got := w.displayRootFor(context.Background(), from, mustRoot(t, "https://h.test/docs/lib/v5.2/"), sample); got != "https://h.test/docs/lib/" {
		t.Errorf("display root = %q", got)
	}
	other, _ := url.Parse("https://h.test/docs/lib/v4.9/chat")
	if got := w.displayRootFor(context.Background(), from, mustRoot(t, "https://h.test/docs/lib/v4.9/"), other); got != "" {
		t.Errorf("a release naming itself canonical = %q", got)
	}
	results := []Result{{URL: "https://h.test/docs/lib/v5.2/chat#templates"}}
	showUnder(results, docsindex.Library{Root: "https://h.test/docs/lib/v5.2/", DisplayRoot: "https://h.test/docs/lib/"})
	if results[0].URL != "https://h.test/docs/lib/chat#templates" {
		t.Errorf("shown at %s", results[0].URL)
	}
}

// fakeSeeder names pages for a library.
type fakeSeeder struct {
	urls []string
	err  error
}

func (s fakeSeeder) Seeds(_ context.Context, _ SeedLibrary) ([]SeedPage, error) {
	var out []SeedPage
	for _, u := range s.urls {
		out = append(out, SeedPage{URL: u, Weight: 1, Source: "test"})
	}
	return out, s.err
}

// A dry run plans without fetching a page or writing the index; seeded
// pages under the root lead the plan, ones outside it or in another
// version's tree never enter it, and a seeder that fails only says so.
func TestIndexDryRunWithSeeds(t *testing.T) {
	f, store := exampleSite(), newStore(t)
	seeder := fakeSeeder{urls: []string{idxRoot + "api/app", "https://elsewhere.test/docs/x", idxRoot + "v1/guide/legacy"}}
	r, err := IndexLibrary(context.Background(), &config.Config{}, Deps{Fetcher: f}, store, IndexRequest{Library: idxRoot, DryRun: true, Seeder: seeder})
	if err != nil {
		t.Fatal(err)
	}
	if r.Root != idxRoot || len(r.Planned) == 0 || r.Planned[0].URL != idxRoot+"api/app" || r.Planned[0].Tier != "curated" {
		t.Fatalf("plan = %+v", r.Planned)
	}
	for _, p := range r.Planned {
		if strings.Contains(p.URL, "elsewhere") || strings.Contains(p.URL, "/v1/") {
			t.Errorf("planned %s", p.URL)
		}
	}
	for _, c := range f.called() {
		if strings.HasPrefix(c, "page ") {
			t.Errorf("a dry run fetched %s", c)
		}
	}
	if libs, _ := store.Libraries(); len(libs) != 0 {
		t.Errorf("a dry run wrote %d libraries", len(libs))
	}
	failing := fakeSeeder{err: errors.New("search down")}
	if _, err := IndexLibrary(context.Background(), &config.Config{}, Deps{Fetcher: f}, store, IndexRequest{Library: idxRoot, DryRun: true, Seeder: failing}); err != nil {
		t.Errorf("a failing seeder stopped the build: %v", err)
	}
}

// A locale's copy of a page at x.html is a copy of the page llms.txt
// names at x, and is left out with its locale.
func TestPlanLocaleCopyAtHTMLAddress(t *testing.T) {
	root, _ := parseWebRoot("https://r.test/")
	links := uniquePages(linksOf(t, "https://r.test/guide/push", "https://r.test/guide/guards", "https://r.test/zh/guide/push.html", "https://r.test/zh/guide/guards.html"))
	kept, _, _ := keepOneVariant(links, root, nil)
	if got := urlsOf(kept); !slices.Equal(got, []string{"https://r.test/guide/push", "https://r.test/guide/guards"}) {
		t.Errorf("kept %v", got)
	}
}

// An unversioned root whose pages sit in two release trees below it is
// indexed at the one its root page links to most, else the newest final
// release; a REST reference's versions beside the guides, or one tree,
// is no split.
func TestSplitReleases(t *testing.T) {
	pages := func(prefix string, n int) []string {
		var out []string
		for i := range n {
			out = append(out, fmt.Sprintf("%spage-%d", prefix, i))
		}
		return out
	}
	root, _ := parseWebRoot("https://s.test/")
	both := linksOf(t, append(append(pages("https://s.test/docs/v7/", 30), pages("https://s.test/docs/v6/", 20)...), "https://s.test/v5/", "https://s.test/")...)
	home := []siteLink{{url: both[35].url}, {url: both[36].url}, {url: both[0].url}}
	if got, ok := splitReleases(root, both, home); !ok || got.String() != "https://s.test/docs/v6/" {
		t.Errorf("linked release = %v %v", got, ok)
	}
	if got, ok := splitReleases(root, both, nil); !ok || got.String() != "https://s.test/docs/v7/" {
		t.Errorf("newest release = %v %v", got, ok)
	}
	rest := linksOf(t, append(append(pages("https://s.test/api/v1/", 12), pages("https://s.test/api/v2/", 12)...), pages("https://s.test/guides/", 40)...)...)
	if got, ok := splitReleases(root, rest, nil); ok {
		t.Errorf("REST versions split the docs at %v", got)
	}
	versioned, _ := parseWebRoot("https://s.test/docs/v6/")
	if _, ok := splitReleases(versioned, both, nil); ok {
		t.Error("a versioned root split")
	}
}
