package docs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// fakePageCache is the page cache in front of a fakeFetcher, as
// internal/docsfetch puts the real one in front of the network: pages
// (FetchPage) and resolution entries kept for ttl by a clock the test
// moves; FetchRaw never cached. The fakeFetcher's calls are the network's.
type fakePageCache struct {
	net *fakeFetcher
	ttl time.Duration

	mu    sync.Mutex
	now   time.Time
	pages map[string]cachedPage
	res   map[string]cachedValue
}

type cachedPage struct {
	page PageContent
	at   time.Time
}

type cachedValue struct {
	value string
	at    time.Time
}

func newFakePageCache(f *fakeFetcher, ttl time.Duration) *fakePageCache {
	return &fakePageCache{net: f, ttl: ttl, now: time.Now(), pages: map[string]cachedPage{}, res: map[string]cachedValue{}}
}

var _ ResolutionCache = (*fakePageCache)(nil)

func (c *fakePageCache) clock() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakePageCache) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *fakePageCache) fresh(at time.Time) bool { return c.clock().Sub(at) <= c.ttl }

func (c *fakePageCache) FetchRaw(ctx context.Context, url string) (RawContent, error) {
	return c.net.FetchRaw(ctx, url)
}

func (c *fakePageCache) FetchPage(ctx context.Context, url string) (PageContent, error) {
	c.mu.Lock()
	e, ok := c.pages[url]
	c.mu.Unlock()
	if ok && c.fresh(e.at) {
		return e.page, nil
	}
	p, err := c.net.FetchPage(ctx, url)
	if err == nil {
		c.mu.Lock()
		c.pages[url] = cachedPage{p, c.now}
		c.mu.Unlock()
	}
	return p, err
}

func (c *fakePageCache) GetResolution(key string) (string, bool) {
	c.mu.Lock()
	e, ok := c.res[key]
	c.mu.Unlock()
	if !ok || !c.fresh(e.at) {
		return "", false
	}
	return e.value, true
}

func (c *fakePageCache) PutResolution(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.res[key] = cachedValue{value, c.now}
}

// A page served at two addresses (x and x.html, a trailing slash) is one
// page: read once, at the address listed first, and cited once.
func TestSearchFirstReadsAPageOnce(t *testing.T) {
	f := exampleSite()
	f.pages[idxRoot+"guide/middleware.html"] = f.pages[idxRoot+"guide/middleware"]
	f.pages[idxRoot+"guide/routing/"] = f.pages[idxRoot+"guide/routing"]
	hits := append(exampleHits(), hit(idxRoot+"guide/middleware.html", "Middleware - Example docs"), hit(idxRoot+"guide/routing/", "Routing - Example docs"))
	s, _, _ := searchFirstOver(t, f, hits)
	o := s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if o.Status != Partial {
		t.Fatalf("outcome %s (%v)", o.Status, o.Err())
	}
	for _, c := range f.called() {
		if strings.HasSuffix(c, "middleware.html") || strings.HasSuffix(c, "routing/") {
			t.Errorf("read %s, a second address of a page already read", c)
		}
	}
	seen := map[string]bool{}
	for _, r := range o.Results {
		if seen[r.URL] {
			t.Errorf("%s cited twice", r.URL)
		}
		seen[r.URL] = true
	}
}

// putSearchBuilt stores a library as an earlier ketch's question stored
// one (Resolved "search") at root, holding the middleware page.
func putSearchBuilt(t *testing.T, store *docsindex.Store, root string) {
	t.Helper()
	page := root + "guide/middleware"
	err := store.PutPages(root, []docsindex.PageData{{
		Page: docsindex.Page{URL: page, Title: "Middleware", Hash: "old", Sections: 1, Fetched: time.Now().UTC(), Source: "search"},
		Sections: []docsindex.Section{{Page: page, Anchor: "stale-copy", Heading: "Stale copy", Crumbs: []string{"Middleware", "Stale copy"},
			Text: "Add middleware to a route the way a by-product library said."}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	lib := docsindex.Library{Key: root, Library: root, Root: root, Resolved: resolvedBySearch, Format: docsindex.FormatVersion, Built: time.Now().UTC(), Pages: 1, Sections: 1, Capped: true}
	if err := store.PutLibrary(lib); err != nil {
		t.Fatal(err)
	}
}

// A library an earlier ketch stored for a question is never read: not
// for a bare question, not for a request naming its root, not for a
// section of it.
func TestSearchBuiltLibrariesAreIgnored(t *testing.T) {
	f := exampleSite()
	s, store, searches := searchFirstOver(t, f, exampleHits())
	putSearchBuilt(t, store, idxRoot)
	ctx := context.Background()
	for _, o := range []Outcome{
		s.SearchSelect(ctx, "add middleware to a route", 0),
		s.GetDocsSelect(ctx, idxRoot, "add middleware to a route with auth", 0),
	} {
		if o.Status != Partial || o.Index == nil || o.Index.Source != SearchBackend {
			t.Errorf("outcome %s index %+v (%v), want the pages a search read", o.Status, o.Index, o.Err())
		}
		for _, r := range o.Results {
			if strings.Contains(r.URL, "stale-copy") {
				t.Errorf("cited the search-built library: %s", r.URL)
			}
		}
	}
	if searches.Load() == 0 {
		t.Error("answered without searching")
	}
	if o := s.SectionSelect(ctx, "", idxRoot+"guide/middleware#stale-copy", 0, 0); o.Status != Refused || !errors.Is(o.Err(), ErrSectionNotFound) {
		t.Errorf("a section only the search-built library holds: %s (%v), want not_found", o.Status, o.Err())
	}
	if libs, _ := s.local.libraries(); len(libs) != 0 {
		t.Errorf("libraries read: %d, want none", len(libs))
	}
}

// The pages of an answer from the pages a search read are not in any
// index: --section reads the page through the page cache (no network
// within the TTL), again from the site after it, and fails with the
// reason when neither can.
func TestSectionOfAPageASearchRead(t *testing.T) {
	f := exampleSite()
	s, c, store, _ := searchFirstCached(t, f, exampleHits())
	ctx := context.Background()
	o := s.SearchSelect(ctx, "add middleware to a route", 0)
	ref := ""
	for _, r := range o.Results {
		if strings.HasSuffix(r.Section, "#add-middleware-to-a-route") {
			ref = r.Section
		}
	}
	if ref == "" {
		t.Fatalf("answer %s: %v, want the route section", o.Status, resultURLs(o.Results))
	}
	before := len(f.called())
	sec := s.SectionSelect(ctx, "", ref, 0, 0)
	if sec.Status != Found || len(sec.Results) != 1 || !strings.Contains(sec.Results[0].Snippet, "app.use") || len(f.called()) != before {
		t.Fatalf("section %s from the cache: %s (%v), %d fetches, %+v", ref, sec.Status, sec.Err(), len(f.called())-before, sec.Results)
	}
	c.advance(2 * time.Hour)
	if again := s.SectionSelect(ctx, "", ref, 0, 0); again.Status != Found || len(f.called()) != before+1 {
		t.Errorf("after the TTL: %s (%v), %d fetches; want the page read again", again.Status, again.Err(), len(f.called())-before)
	}
	c.advance(2 * time.Hour)
	f.fail = errors.New("dial tcp: network is unreachable")
	cold := s.SectionSelect(ctx, "", ref, 0, 0)
	if cold.Status != Failed || !strings.Contains(cold.Err().Error(), "is in no indexed library, and reading it failed") || !strings.Contains(cold.Err().Error(), "network is unreachable") {
		t.Errorf("no network, cache cold: %s (%v)", cold.Status, cold.Err())
	}
	checkNothingStored(t, store)
}

// A question whose search lands on a site an indexed library holds is
// answered from that library, without reading the site's pages.
func TestSearchFirstAnswersFromTheIndexedSite(t *testing.T) {
	f := exampleSite()
	s, store, _ := searchFirstOver(t, f, exampleHits())
	buildIndex(t, f, store)
	before := len(f.called())
	o := s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if o.Status != Found || o.Index == nil || o.Index.Kind != IndexFull || len(o.Results) == 0 {
		t.Fatalf("outcome %s index %+v (%v)", o.Status, o.Index, o.Err())
	}
	for _, call := range f.called()[before:] {
		if strings.Contains(call, "sitemap") || strings.Contains(call, "/api/") {
			t.Errorf("read %s; the index holds the site", call)
		}
	}
}

// A page a question read at one address and was served at another (a
// redirect to a trailing slash) is cited at the address it was served
// at; --section reads it by the address it was read at, from the cache.
func TestSectionOfARedirectedPage(t *testing.T) {
	f := exampleSite()
	moved := idxRoot + "guide/middleware/"
	f.pages[moved] = f.pages[idxRoot+"guide/middleware"]
	delete(f.pages, idxRoot+"guide/middleware")
	f.redirects = map[string]string{idxRoot + "guide/middleware": moved}
	s, _, _, _ := searchFirstCached(t, f, exampleHits())
	ctx := context.Background()
	o := s.SearchSelect(ctx, "add middleware to a route", 0)
	ref := moved + "#add-middleware-to-a-route"
	if !slices.Contains(resultURLs(o.Results), ref) {
		t.Fatalf("answer %s cites %v, want %s", o.Status, resultURLs(o.Results), ref)
	}
	before := len(f.called())
	if sec := s.SectionSelect(ctx, "", ref, 0, 0); sec.Status != Found || len(f.called()) != before {
		t.Errorf("section of the redirected page: %s (%v), %v fetched", sec.Status, sec.Err(), f.called()[before:])
	}
}
