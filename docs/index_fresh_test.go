package docs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
)

// indexedSearchFirst is the backend over an index of exampleSite built now, its
// clock and page cache the test's, and the fetcher the network.
func indexedSearchFirst(t *testing.T) (*SearchFirst, *fakePageCache, *fakeFetcher, *docsindex.Store) {
	t.Helper()
	f := exampleSite()
	s, c, store, _ := searchFirstCached(t, f, exampleHits())
	buildIndex(t, f, store)
	return s, c, f, store
}

const freshQuestion = "add middleware to a route"

// pageCalls are the FetchPage calls made since before.
func pageCalls(f *fakeFetcher, before int) []string {
	var out []string
	for _, c := range f.called()[before:] {
		if strings.HasPrefix(c, "page ") {
			out = append(out, strings.TrimPrefix(c, "page "))
		}
	}
	return out
}

func fetchedAt(t *testing.T, store *docsindex.Store, page string) time.Time {
	t.Helper()
	pages, err := store.PagesOf(idxRoot, []string{page})
	if err != nil || len(pages) != 1 {
		t.Fatalf("PagesOf %s: %v %v", page, pages, err)
	}
	return pages[page].Fetched
}

// Cited pages read within the TTL are never read again.
func TestIndexCheckLeavesFreshPages(t *testing.T) {
	s, _, f, _ := indexedSearchFirst(t)
	before := len(f.called())
	o := s.GetDocsSelect(context.Background(), idxRoot, freshQuestion, 0)
	if o.Status != Found || o.Stale != "" || len(o.Results) == 0 {
		t.Fatalf("answer %s stale %q (%v)", o.Status, o.Stale, o.Err())
	}
	if calls := pageCalls(f, before); len(calls) != 0 {
		t.Errorf("fresh pages read again: %v", calls)
	}
}

// A cited page older than the TTL is read again; unchanged, only its
// fetch time moves, and pages the answer does not cite are not read.
func TestIndexCheckStaleUnchanged(t *testing.T) {
	s, c, f, store := indexedSearchFirst(t)
	page := idxRoot + "guide/middleware"
	c.advance(2 * time.Hour)
	before := len(f.called())
	o := s.GetDocsSelect(context.Background(), idxRoot, freshQuestion, 0)
	if o.Status != Found || o.Stale != "" {
		t.Fatalf("answer %s stale %q (%v)", o.Status, o.Stale, o.Err())
	}
	cited := citedPages(docsindex.Library{Root: idxRoot}, o.Results)
	calls := pageCalls(f, before)
	if len(calls) != len(cited) {
		t.Errorf("read %v, want the %d cited pages %v", calls, len(cited), cited)
	}
	for _, call := range calls {
		if strings.Contains(call, "api/") {
			t.Errorf("read %s, which the answer does not cite", call)
		}
	}
	if got := fetchedAt(t, store, page); !got.Equal(c.clock().UTC().Truncate(time.Second)) {
		t.Errorf("fetched_at %s, want the check's time %s", got, c.clock().UTC())
	}
	before = len(f.called())
	if s.GetDocsSelect(context.Background(), idxRoot, freshQuestion, 0); len(pageCalls(f, before)) != 0 {
		t.Error("pages just checked were read again")
	}
}

// A cited page that changed since the build is sectioned again in the
// index and the answer is ranked once more, from the new text.
func TestIndexCheckStaleChanged(t *testing.T) {
	s, c, f, store := indexedSearchFirst(t)
	page := idxRoot + "guide/middleware"
	before, _ := store.PagesOf(idxRoot, []string{page})
	f.pages[page] = fakePage{title: "Middleware", markdown: "# Middleware\n\nMiddleware wraps handlers.\n\n## Add middleware to a route\n\nRegister middleware on a route with app.route(path).use(handler), the new way.\n"}
	c.advance(2 * time.Hour)
	o := s.GetDocsSelect(context.Background(), idxRoot, freshQuestion, 0)
	if o.Status != Found || o.Stale != "" {
		t.Fatalf("answer %s stale %q (%v)", o.Status, o.Stale, o.Err())
	}
	text := ""
	for _, r := range o.Results {
		text += r.Snippet + "\n"
	}
	if !strings.Contains(text, "the new way") || strings.Contains(text, "auth()") {
		t.Errorf("answer printed the old text:\n%s", text)
	}
	after, _ := store.PagesOf(idxRoot, []string{page})
	if after[page].Hash == before[page].Hash || !after[page].Fetched.After(before[page].Fetched) {
		t.Errorf("stored page %+v, was %+v; want the new content", after[page], before[page])
	}
	lib, _, _ := store.Library(idxRoot)
	if secs, _ := store.Sections(idxRoot); lib.Sections != len(secs) {
		t.Errorf("library counts %d sections, the index holds %d", lib.Sections, len(secs))
	}
}

// A cited page that cannot be read again (no network, a timeout) leaves
// the answer as it is, marked stale with the build date.
func TestIndexCheckUnverified(t *testing.T) {
	for _, tc := range []struct {
		name string
		cut  func(*fakeFetcher)
	}{
		{"fails", func(f *fakeFetcher) { f.fail = errors.New("dial tcp: network is unreachable") }},
		{"times out", func(f *fakeFetcher) { f.block = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, f, store := indexedSearchFirst(t)
			s.checkTimeout = 50 * time.Millisecond
			lib, _, _ := store.Library(idxRoot)
			fresh := s.GetDocsSelect(context.Background(), idxRoot, freshQuestion, 0)
			c.advance(2 * time.Hour)
			tc.cut(f)
			start := time.Now()
			o := s.GetDocsSelect(context.Background(), idxRoot, freshQuestion, 0)
			want := "unverified, built " + lib.Built.UTC().Format("2006-01-02")
			if o.Status != Found || o.Stale != want || !sameURLs(o.Results, fresh.Results) {
				t.Errorf("answer %s stale %q, want %q and the same results", o.Status, o.Stale, want)
			}
			if d := time.Since(start); d > time.Second {
				t.Errorf("took %s, past the check's limit", d)
			}
			if f := NewFrontmatter("q", SearchBackend, "", o).Render(); !strings.Contains(f, "\nstale: "+want+"\n") {
				t.Errorf("frontmatter lacks the stale line:\n%s", f)
			}
		})
	}
}

// --offline reads nothing: an answer citing a page older than the TTL is
// marked stale, one citing only fresh pages is not.
func TestIndexCheckOffline(t *testing.T) {
	s, c, f, _ := indexedSearchFirst(t)
	ctx := WithLocalOptions(context.Background(), LocalOptions{Offline: true})
	if o := s.GetDocsSelect(ctx, idxRoot, freshQuestion, 0); o.Stale != "" {
		t.Errorf("fresh pages, offline: stale %q", o.Stale)
	}
	c.advance(2 * time.Hour)
	before := len(f.called())
	o := s.GetDocsSelect(ctx, idxRoot, freshQuestion, 0)
	if o.Status != Found || !strings.HasPrefix(o.Stale, "unverified, built ") {
		t.Errorf("stale pages, offline: %s stale %q", o.Status, o.Stale)
	}
	if calls := pageCalls(f, before); len(calls) != 0 {
		t.Errorf("offline read %v", calls)
	}
}

// A --section read from the index follows the same rule for its page.
func TestIndexCheckSection(t *testing.T) {
	s, c, f, _ := indexedSearchFirst(t)
	ref := idxRoot + "guide/middleware#add-middleware-to-a-route"
	ctx := context.Background()
	before := len(f.called())
	if o := s.SectionSelect(ctx, "", ref, 0, 0); o.Status != Found || len(pageCalls(f, before)) != 0 {
		t.Fatalf("fresh section: %s (%v), read %v", o.Status, o.Err(), pageCalls(f, before))
	}
	c.advance(2 * time.Hour)
	if o := s.SectionSelect(WithLocalOptions(ctx, LocalOptions{Offline: true}), "", ref, 0, 0); o.Status != Found || o.Stale == "" || len(pageCalls(f, before)) != 0 {
		t.Errorf("stale section, offline: %s stale %q, read %v", o.Status, o.Stale, pageCalls(f, before))
	}
	if o := s.SectionSelect(ctx, "", ref, 0, 0); o.Status != Found || o.Stale != "" || len(pageCalls(f, before)) != 1 {
		t.Errorf("stale section: %s stale %q, read %v; want its page read once", o.Status, o.Stale, pageCalls(f, before))
	}
}

// docs index replaces what earlier ketch builds stored for questions: a
// library at the same root is rebuilt from scratch, any other is removed;
// none is listed or read meanwhile.
func TestIndexReplacesSearchBuiltLibraries(t *testing.T) {
	f, store := exampleSite(), newStore(t)
	putSearchBuilt(t, store, idxRoot)
	putSearchBuilt(t, store, "https://other.example.test/docs/")
	r := buildIndex(t, f, store)
	libs, err := store.Libraries()
	if err != nil || len(libs) != 1 || libs[0].Key != idxRoot || BuiltBySearch(libs[0]) || r.Library.Resolved != "docs_url" {
		t.Fatalf("libraries after the build: %+v (%v)", libs, err)
	}
	secs, _ := store.Sections(idxRoot)
	for _, sec := range secs {
		if sec.Anchor == "stale-copy" {
			t.Error("the search-built library's section survived the build")
		}
	}
	pages, _ := store.Pages(idxRoot)
	for u, p := range pages {
		if p.Source != "index html" {
			t.Errorf("%s stored with source %q, want index html", u, p.Source)
		}
	}
}

// One library per docs root: http and https, "www." and a trailing slash
// are one root. A build under either form leaves one library, and a
// library value in either form finds it.
func TestIndexOneLibraryPerRoot(t *testing.T) {
	f, store := exampleSite(), newStore(t)
	httpRoot := "http://docs.example.test/docs/"
	for u, p := range exampleSite().pages {
		f.pages[strings.Replace(u, "https://", "http://", 1)] = p
	}
	for u, r := range exampleSite().raw {
		f.raw[strings.Replace(u, "https://", "http://", 1)] = r
	}
	if _, err := IndexLibrary(context.Background(), &config.Config{}, Deps{Fetcher: f}, store, IndexRequest{Library: httpRoot, Names: []string{"exemplar"}}); err != nil {
		t.Fatal(err)
	}
	buildIndex(t, f, store)
	libs, _ := store.Libraries()
	if len(libs) != 1 || libs[0].Root != idxRoot {
		t.Fatalf("libraries %+v, want one at %s", libs, idxRoot)
	}
	if !strings.Contains(strings.Join(libs[0].Aliases, ","), "exemplar") {
		t.Errorf("aliases %v: the replaced library's names were lost", libs[0].Aliases)
	}
	for _, v := range []string{httpRoot, idxRoot, "https://www.docs.example.test/docs", "http://docs.example.test/docs"} {
		if found := MatchIndexed(libs, v); len(found) != 1 || found[0].Key != idxRoot {
			t.Errorf("MatchIndexed(%s) = %d libraries", v, len(found))
		}
	}
}

// The pages a question read are a library in memory: the evidence
// pipeline answers from it as from the index, and nothing is written.
func TestReadLibraryIsInMemory(t *testing.T) {
	root, _ := parseWebRoot(idxRoot)
	l, lib, err := readLibrary(root, "Example", "", []webPage{{URL: idxRoot + "guide/a", Title: "A", Markdown: "# A\n\n## Install\n\nRun the installer with --fast.\n"}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.store.Close()
	if lib.Pages != 1 || lib.Sections == 0 || !lib.Current() || l.store.Size() != 0 {
		t.Fatalf("library %+v, store size %d", lib, l.store.Size())
	}
	o := l.answer(context.Background(), lib, "installer fast", LocalOptions{})
	if o.Status != Found || len(o.Results) == 0 || !strings.Contains(o.Results[0].Snippet, "--fast") {
		t.Fatalf("answer %s %+v (%v)", o.Status, o.Results, o.Err())
	}
}

// A page an index read from its Markdown source (llms.txt's x.md) is
// checked against that source, never the rendered page: unchanged, it
// stays as it is; changed, it is sectioned again from the source; when
// the source cannot be read, the answer is unverified and the page keeps
// its text, though the rendered page could be read.
func TestIndexCheckMarkdownSource(t *testing.T) {
	f := mintlifySite()
	s, c, store, _ := searchFirstCached(t, f, nil)
	const root = "https://mint.test/"
	if _, err := IndexLibrary(context.Background(), &config.Config{}, Deps{Fetcher: f}, store, IndexRequest{Library: root}); err != nil {
		t.Fatal(err)
	}
	page, source := root+"guides/content-collections", root+"guides/content-collections.md"
	before, _ := store.PagesOf(root, []string{page})
	if before[page].Source != "index md "+source {
		t.Fatalf("stored with source %q, want its Markdown source named", before[page].Source)
	}
	ctx := context.Background()
	const q = "define a collection schema"
	c.advance(2 * time.Hour)
	o := s.GetDocsSelect(ctx, root, q, 0)
	if o.Status != Found || o.Stale != "" || len(o.Results) == 0 {
		t.Fatalf("answer %s stale %q (%v)", o.Status, o.Stale, o.Err())
	}
	after, _ := store.PagesOf(root, []string{page})
	if after[page].Hash != before[page].Hash || !after[page].Fetched.After(before[page].Fetched) {
		t.Errorf("page %+v, was %+v: want the same text, checked now", after[page], before[page])
	}

	f.raw[source] = md(strings.Replace(f.raw[source].body, "is type-checked", "is type-checked at build time", 1))
	c.advance(2 * time.Hour)
	if o := s.GetDocsSelect(ctx, root, q, 0); o.Stale != "" || !strings.Contains(snippets(o), "at build time") {
		t.Errorf("changed source: stale %q, printed:\n%s", o.Stale, snippets(o))
	}

	delete(f.raw, source)
	c.advance(2 * time.Hour)
	o = s.GetDocsSelect(ctx, root, q, 0)
	if !strings.HasPrefix(o.Stale, "unverified, built ") || !strings.Contains(snippets(o), "at build time") {
		t.Errorf("source gone: stale %q, printed:\n%s", o.Stale, snippets(o))
	}
	if secs, _ := store.Sections(root); slices.ContainsFunc(secs, func(s docsindex.Section) bool { return strings.Contains(s.Text, "rendered") }) {
		t.Error("the page was replaced by its rendered text")
	}
}

func snippets(o Outcome) string {
	var b strings.Builder
	for _, r := range o.Results {
		b.WriteString(r.Snippet + "\n")
	}
	return b.String()
}

// fullSite is a site whose pages come from llms-full.txt chunks; its
// rendered pages say something else.
func fullSite() *fakeFetcher {
	return &fakeFetcher{raw: map[string]fakeRaw{
		"https://full.test/llms.txt": plain("# Full\n\n> Docs for Full.\n\n- [Setup](https://full.test/docs/setup)\n- [Config](https://full.test/docs/config)\n"),
		"https://full.test/llms-full.txt": plain("---\ntitle: Setup\nurl: \"https://full.test/docs/setup\"\n---\n\nInstall Full with the installer, then run full init.\n\n## Options\n\nPass --verbose to full init to see every step.\n\n" +
			"---\ntitle: Config\nurl: \"https://full.test/docs/config\"\n---\n\nConfigure Full in full.toml with the retries key.\n"),
	}, pages: map[string]fakePage{
		"https://full.test/docs/setup":  {title: "Setup", markdown: "# Setup\n\nrendered setup text, full init verbose\n"},
		"https://full.test/docs/config": {title: "Config", markdown: "# Config\n\nrendered config text\n"},
	}}
}

// A page an index took from an llms-full.txt chunk is checked against the
// file again, read once per check: unchanged, it stays; changed, it is
// sectioned again from its chunk; the file gone, the answer is unverified
// and the page is never replaced by its rendered text.
func TestIndexCheckLLMSFullChunk(t *testing.T) {
	f := fullSite()
	s, c, store, _ := searchFirstCached(t, f, nil)
	const root, full = "https://full.test/docs/", "https://full.test/llms-full.txt"
	if _, err := IndexLibrary(context.Background(), &config.Config{}, Deps{Fetcher: f}, store, IndexRequest{Library: root}); err != nil {
		t.Fatal(err)
	}
	page := root + "setup"
	stored, _ := store.PagesOf(root, []string{page})
	if stored[page].Source != "index chunk "+full {
		t.Fatalf("stored with source %q, want its llms-full.txt chunk named", stored[page].Source)
	}
	ctx := context.Background()
	const q = "full init verbose"
	c.advance(2 * time.Hour)
	before := len(f.called())
	o := s.GetDocsSelect(ctx, root, q, 0)
	if o.Status != Found || o.Stale != "" {
		t.Fatalf("answer %s stale %q (%v)", o.Status, o.Stale, o.Err())
	}
	for _, call := range f.called()[before:] {
		if strings.HasPrefix(call, "page ") {
			t.Errorf("read %s: a chunk is checked against its file, not the page", call)
		}
	}

	f.raw[full] = plain(strings.Replace(f.raw[full].body, "to see every step", "to print every step", 1))
	c.advance(2 * time.Hour)
	if o := s.GetDocsSelect(ctx, root, q, 0); o.Stale != "" || !strings.Contains(snippets(o), "print every step") {
		t.Errorf("changed chunk: stale %q, printed:\n%s", o.Stale, snippets(o))
	}

	delete(f.raw, full)
	c.advance(2 * time.Hour)
	o = s.GetDocsSelect(ctx, root, q, 0)
	if !strings.HasPrefix(o.Stale, "unverified, built ") || strings.Contains(snippets(o), "rendered") || !strings.Contains(snippets(o), "print every step") {
		t.Errorf("file gone: stale %q, printed:\n%s", o.Stale, snippets(o))
	}
}

// A page an older build stored without saying how it was read is the same
// when its page still holds the text stored, and otherwise unverified: it
// is never sectioned again from text that may have come another way.
func TestIndexCheckUnsaidSource(t *testing.T) {
	s, c, f, store := indexedSearchFirst(t)
	pages, _ := store.Pages(idxRoot)
	legacy := map[string]string{}
	for u := range pages {
		legacy[u] = "index"
	}
	if err := store.SetPageSources(idxRoot, legacy); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c.advance(2 * time.Hour)
	if o := s.GetDocsSelect(ctx, idxRoot, freshQuestion, 0); o.Status != Found || o.Stale != "" {
		t.Fatalf("unchanged: %s stale %q (%v)", o.Status, o.Stale, o.Err())
	}
	page := idxRoot + "guide/middleware"
	f.pages[page] = fakePage{title: "Middleware", markdown: "# Middleware\n\n## Add middleware to a route\n\nRegister middleware on a route, the new way.\n"}
	c.advance(2 * time.Hour)
	o := s.GetDocsSelect(ctx, idxRoot, freshQuestion, 0)
	if !strings.HasPrefix(o.Stale, "unverified, built ") || strings.Contains(snippets(o), "the new way") {
		t.Errorf("changed, source unsaid: stale %q, printed:\n%s", o.Stale, snippets(o))
	}
}

// A failed check backs off: inside checkBackoff the page is not read
// again and answers citing it are unverified; after it, it is read.
func TestIndexCheckBacksOff(t *testing.T) {
	s, c, f, _ := indexedSearchFirst(t)
	ctx := context.Background()
	c.advance(2 * time.Hour)
	f.fail = errors.New("status 429: too many requests")
	before := len(f.called())
	if o := s.GetDocsSelect(ctx, idxRoot, freshQuestion, 0); !strings.HasPrefix(o.Stale, "unverified") || len(pageCalls(f, before)) == 0 {
		t.Fatalf("failed check: stale %q, read %v", o.Stale, pageCalls(f, before))
	}
	f.fail = nil
	c.advance(checkBackoff - time.Minute)
	before = len(f.called())
	if o := s.GetDocsSelect(ctx, idxRoot, freshQuestion, 0); !strings.HasPrefix(o.Stale, "unverified") || len(pageCalls(f, before)) != 0 {
		t.Errorf("inside the back-off: stale %q, read %v; want unverified and nothing read", o.Stale, pageCalls(f, before))
	}
	if o := s.SectionSelect(ctx, "", idxRoot+"guide/middleware#add-middleware-to-a-route", 0, 0); !strings.HasPrefix(o.Stale, "unverified") || len(pageCalls(f, before)) != 0 {
		t.Errorf("a section inside the back-off: stale %q, read %v", o.Stale, pageCalls(f, before))
	}
	c.advance(2 * time.Minute)
	before = len(f.called())
	if o := s.GetDocsSelect(ctx, idxRoot, freshQuestion, 0); o.Stale != "" || len(pageCalls(f, before)) == 0 {
		t.Errorf("after the back-off: stale %q, read %v; want the pages read and verified", o.Stale, pageCalls(f, before))
	}
}

// A build that finds a page unchanged but stored without saying how it was
// read records how it is read now, and counts it unchanged.
func TestIndexBuildRecordsUnsaidSource(t *testing.T) {
	f, store := exampleSite(), newStore(t)
	buildIndex(t, f, store)
	pages, _ := store.Pages(idxRoot)
	legacy := map[string]string{}
	for u := range pages {
		legacy[u] = "index"
	}
	if err := store.SetPageSources(idxRoot, legacy); err != nil {
		t.Fatal(err)
	}
	r := buildIndex(t, f, store)
	if r.Changed != 0 || r.New != 0 || r.Unchanged != len(pages) {
		t.Errorf("report %+v, want every page unchanged", r)
	}
	after, _ := store.Pages(idxRoot)
	for u, p := range after {
		if p.Source != "index html" {
			t.Errorf("%s: source %q after the build, want index html", u, p.Source)
		}
	}
}
