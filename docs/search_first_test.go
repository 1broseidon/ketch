package docs

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/failure"
	"github.com/1broseidon/ketch/search"
)

// searchFirstOver is the backend over a fake site behind a fake page
// cache, a fake web search answering every query with hits, and a store.
func searchFirstOver(t *testing.T, f *fakeFetcher, hits []search.Result) (*SearchFirst, *docsindex.Store, *atomic.Int32) {
	t.Helper()
	s, _, store, searches := searchFirstCached(t, f, hits)
	return s, store, searches
}

// searchFirstCached is searchFirstOver with its page cache, whose clock
// is the backend's.
func searchFirstCached(t *testing.T, f *fakeFetcher, hits []search.Result) (*SearchFirst, *fakePageCache, *docsindex.Store, *atomic.Int32) {
	t.Helper()
	store := newStore(t)
	c := newFakePageCache(f, time.Hour)
	var searches atomic.Int32
	w := NewWeb(c)
	w.sites = siteSearchFunc(func(context.Context, string, int) ([]search.Result, error) {
		searches.Add(1)
		return hits, nil
	})
	s := NewSearchFirst(w, NewLocal(store))
	s.ttl, s.now = c.ttl, c.clock
	return s, c, store, &searches
}

func hit(u, title string) search.Result { return search.Result{URL: u, Title: title} }

// exampleHits are a search's hits for a middleware question: the docs
// pages, and pages that are never docs around them.
func exampleHits() []search.Result {
	return []search.Result{
		hit("https://github.com/example/example/issues/12", "middleware order issue"),
		hit(idxRoot+"guide/middleware", "Middleware - Example docs"),
		hit("https://stackoverflow.com/questions/1/example-middleware", "How to add middleware"),
		hit("https://docs.example.test/blog/2024/05/middleware-tips", "Middleware tips"),
		hit(idxRoot+"guide/routing", "Routing - Example docs"),
	}
}

// A question is answered from the docs site its hits agree on: the hit
// pages and their siblings are read (robots.txt, other locales and
// releases left out) and ranked as a library held in memory; the answer
// is found when its terms are there (the source line says a search read
// the pages) and says how to index the rest. Nothing is written to the
// docs index.
func TestSearchFirstAnswersFromTheSite(t *testing.T) {
	f := exampleSite()
	s, store, _ := searchFirstOver(t, f, exampleHits())
	o := s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if o.Status != Found || o.Reason != "" || o.Backend != SearchBackend {
		t.Fatalf("outcome = %s/%s from %q (%v), want found from search", o.Status, o.Reason, o.Backend, o.Err())
	}
	if len(o.Results) == 0 || !strings.HasPrefix(o.Results[0].URL, idxRoot+"guide/middleware") {
		t.Fatalf("top result = %+v, want the middleware page", o.Results)
	}
	if o.Note != "read 3 pages of "+idxRoot+" found by web search; the whole site: ketch docs index "+idxRoot {
		t.Errorf("note = %q, want the pages read and the command that indexes the whole site", o.Note)
	}
	if o.Index == nil || o.Index.Source != SearchBackend || o.Index.PagesRead != 3 {
		t.Errorf("index = %+v, want source: search, 3 pages read", o.Index)
	}
	checkNeverRead(t, f)
	checkNothingStored(t, store)
}

func checkNeverRead(t *testing.T, f *fakeFetcher) {
	t.Helper()
	for _, c := range f.called() {
		if strings.Contains(c, "/private/") || strings.Contains(c, "/es/") || strings.Contains(c, "/v1/") || strings.Contains(c, "github.com") {
			t.Errorf("fetched %s: robots.txt, other locales and releases, and non-docs hits are never read", c)
		}
	}
}

// checkNothingStored fails when a question wrote the docs index.
func checkNothingStored(t *testing.T, store *docsindex.Store) {
	t.Helper()
	if store.Exists() {
		libs, err := store.Libraries()
		t.Fatalf("a question wrote the docs index: %d libraries (%v)", len(libs), err)
	}
}

// A question asked again within cache_ttl (its terms the same, whatever
// the filler) reads what the first read from the cache: no web search,
// no page fetch, the same answer. One about something else, or the same
// words naming a library, searches; after the TTL the question searches
// and reads again.
func TestSearchFirstRepeatWithinTTL(t *testing.T) {
	f := exampleSite()
	s, c, store, searches := searchFirstCached(t, f, exampleHits())
	ctx := context.Background()
	first := s.SearchSelect(ctx, "add middleware to a route", 0)
	if first.Status != Found {
		t.Fatalf("first answer = %s (%v)", first.Status, first.Err())
	}
	searched, fetched := searches.Load(), len(f.called())
	again := s.SearchSelect(ctx, "How do I add middleware to a route?", 0)
	if searches.Load() != searched || len(f.called()) != fetched {
		t.Errorf("asked again: %d searches and %d fetches; want none", searches.Load()-searched, len(f.called())-fetched)
	}
	if again.Status != Found || again.Origin != first.Origin || again.Note != first.Note || !sameURLs(again.Results, first.Results) || again.Index.PagesRead != first.Index.PagesRead {
		t.Errorf("again = %s %q %v, want the first answer", again.Status, again.Note, resultURLs(again.Results))
	}
	checkCached(t, first, again)
	if s.SearchSelect(ctx, "fetch handler runtime", 0); searches.Load() == searched {
		t.Error("another question was answered without searching")
	}
	searched = searches.Load()
	if s.GetDocsSelect(ctx, idxRoot, "add middleware to a route", 0); searches.Load() == searched {
		t.Error("the same words naming a library were answered from the bare question's read")
	}
	c.advance(time.Hour + time.Minute)
	searched, fetched = searches.Load(), len(f.called())
	if o := s.SearchSelect(ctx, "add middleware to a route", 0); o.Status != Found || searches.Load() == searched || len(f.called()) == fetched {
		t.Errorf("after the TTL: %s, %d searches, %d fetches; want a search and pages read again", o.Status, searches.Load()-searched, len(f.called())-fetched)
	}
	checkNothingStored(t, store)
}

// A question the pages it read refuse (none of its distinctive terms in
// them) is kept like an answered one: asked again within cache_ttl it is
// refused again without a search or a fetch, its source line saying
// cached.
func TestSearchFirstRefusalWithinTTL(t *testing.T) {
	f := exampleSite()
	s, c, store, searches := searchFirstCached(t, f, exampleHits())
	ctx := context.Background()
	const q = "zookeeper quorum"
	first := s.SearchSelect(ctx, q, 0)
	if first.Status != Refused || !errors.Is(first.Err(), ErrTermsNotFound) || first.Index == nil || first.Index.Cached {
		t.Fatalf("first = %s (%v) index %+v, want refused for want of the terms", first.Status, first.Err(), first.Index)
	}
	searched, fetched := searches.Load(), len(f.called())
	again := s.SearchSelect(ctx, q, 0)
	if searches.Load() != searched || len(f.called()) != fetched {
		t.Errorf("asked again: %d searches and %d fetches; want none", searches.Load()-searched, len(f.called())-fetched)
	}
	if again.Status != Refused || !slices.Equal(again.NotFound, first.NotFound) || again.Index == nil || !again.Index.Cached {
		t.Errorf("again = %s not_found %v index %+v, want the same refusal, cached", again.Status, again.NotFound, again.Index)
	}
	c.advance(time.Hour + time.Minute)
	if s.SearchSelect(ctx, q, 0); searches.Load() == searched {
		t.Error("after the TTL the refusal was repeated without searching")
	}
	checkNothingStored(t, store)
}

// checkCached fails unless only the repeat says it was answered from the
// question cache: its source line ends with cached.
func checkCached(t *testing.T, first, again Outcome) {
	t.Helper()
	if first.Index.Cached || !again.Index.Cached || !strings.Contains(NewFrontmatter("q", SearchBackend, "", again).Render(), "\nsource: search, 3 pages read, cached\n") {
		t.Errorf("index %+v then %+v: want only the repeat to say cached", first.Index, again.Index)
	}
}

func resultURLs(rs []Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.URL)
	}
	return out
}

func sameURLs(a, b []Result) bool { return slices.Equal(resultURLs(a), resultURLs(b)) && len(a) > 0 }

// Hits that are never docs (forges, Q&A, posts) are no site: refused as
// no_docs_found. An empty search is the search failing, not the web
// lacking docs.
func TestSearchFirstRefusals(t *testing.T) {
	cases := []struct {
		name   string
		hits   []search.Result
		status Status
		reason string
		kind   failure.Kind
	}{
		{"no docs", []search.Result{
			hit("https://github.com/example/example/issues/12", "issue"),
			hit("https://stackoverflow.com/questions/1/x", "question"),
			hit("https://forum.example.test/t/middleware/123", "forum topic"),
		}, Refused, "no_docs_found", failure.NotFound},
		{"split between sites", []search.Result{
			hit("https://docs.one.test/guide/a", "A - One docs"),
			hit("https://docs.two.test/guide/a", "A - Two docs"),
			hit("https://docs.three.test/guide/a", "A - Three docs"),
		}, Refused, "ambiguous_site", failure.Validation},
		{"empty search", nil, Failed, "search_empty", failure.Upstream},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := searchFirstOver(t, exampleSite(), tc.hits)
			o := s.SearchSelect(context.Background(), "add middleware", 0)
			if o.Status != tc.status || o.Reason != tc.reason || o.Kind() != tc.kind {
				t.Fatalf("outcome = %s/%s kind %v (%v), want %s/%s kind %v", o.Status, o.Reason, o.Kind(), o.Err(), tc.status, tc.reason, tc.kind)
			}
		})
	}
}

// A request's library is a preference among the sites, not a gate: the
// site its name labels outweighs a slightly stronger one, and the version
// it asks for picks the tree.
func TestChooseSitesPreferences(t *testing.T) {
	hits := docsHits(fuseHits([][]search.Result{{
		hit("https://docs.other.test/guide/x", "X - Other docs"),
		hit("https://docs.lib.test/v2/guide/x", "X - Lib docs"),
		hit("https://docs.lib.test/v3/guide/x", "X - Lib docs"),
	}}))
	sites := chooseSites(hits, sitePrefs{})
	if sites[0].key != "lib.test" {
		t.Fatalf("without preferences the site is %s, want lib.test (two hits)", sites[0].key)
	}
	sites = chooseSites(hits, sitePrefs{names: []string{"lib"}, version: "2"})
	if got := sites[0].root.String(); got != "https://docs.lib.test/v2/" {
		t.Errorf("version 2 asked: root %s, want the v2 tree", got)
	}
	old := docsHits(fuseHits([][]search.Result{{
		hit("https://lib.test/v3/guide/x", "X - Lib docs"), hit("https://lib.test/v3/guide/y", "Y - Lib docs"),
		hit("https://lib.test/docs/v6/guide/x", "X - Lib docs"),
	}}))
	if got := chooseSites(old, sitePrefs{})[0].root.String(); got != "https://lib.test/docs/v6/" {
		t.Errorf("no version asked: root %s, want the newer release's tree", got)
	}
	sites = chooseSites(hits, sitePrefs{names: []string{"other"}})
	if sites[0].key != "other.test" {
		t.Errorf("a library named other: site %s, want other.test", sites[0].key)
	}
}

func TestDocsTree(t *testing.T) {
	cases := map[string]string{
		"https://duo.example.test/docs/sso-oidc":                   "https://duo.example.test/docs/",
		"https://lib.readthedocs.io/en/latest/user/advanced/":      "https://lib.readthedocs.io/en/latest/",
		"https://www.lib.test/lib3/docs/basic/usage.html":          "https://www.lib.test/lib3/docs/",
		"https://lib.test/guide/essentials/computed.html":          "https://lib.test/",
		"https://owner.github.io/project/guide/intro":              "https://owner.github.io/project/",
		"https://docs.lib.test/en/actions/writing-workflows/x":     "https://docs.lib.test/en/",
		"https://docs.rs/serde/latest/serde/de/trait.Visitor.html": "https://docs.rs/serde/latest/",
		"https://developer.lib.test/docs/guides/claims/main/":      "https://developer.lib.test/docs/",
		"https://www.lib.test/docs/3.9.1/configuration/tooltip":    "https://www.lib.test/docs/3.9.1/",
	}
	for in, want := range cases {
		u, _ := url.Parse(in)
		if got := docsTree(u).String(); got != want {
			t.Errorf("docsTree(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestNotDocsHit(t *testing.T) {
	cases := map[string]bool{
		"https://github.com/o/r/issues/1":              true,
		"https://stackoverflow.com/questions/1/x":      true,
		"https://superuser.com/questions/1/x":          true,
		"https://forum.lib.test/t/slug/42":             true,
		"https://lib.test/t/slug/42":                   true,
		"https://lib.test/blog/2024/05/post":           true,
		"https://community.lib.test/s/article/x":       true,
		"https://deepwiki.com/o/r":                     true,
		"https://lib.test/docs/guide/forum-plugin":     false,
		"https://docs.lib.test/reference/questions":    false,
		"https://lib.test/docs/discussions/settings":   true,
		"https://help.lib.test/articles/configure-sso": false,
		"https://docs.github.com/en/actions":           false,
		"https://api.github.com/repos/o/r":             true,
	}
	for in, want := range cases {
		u, _ := url.Parse(in)
		if got := notDocsHit(u); got != want {
			t.Errorf("notDocsHit(%s) = %v, want %v", in, got, want)
		}
	}
}

// A request that names nothing pinned never resolves a library: a bare
// question's registries are a guess the hits outvote.
func TestSearchFirstPinnedSpec(t *testing.T) {
	if _, ok := pinnedSpec("how do I add middleware"); ok {
		t.Error("a bare question pins no library")
	}
	spec, ok := pinnedSpec("vue-router@3 navigation guards")
	if !ok || spec.version != "3" {
		t.Errorf("pinnedSpec = %+v, %v; want vue-router at 3", spec, ok)
	}
	q := searchQueries(sfRequest{query: "navigation guards", spec: libSpec{name: "vue-router", version: "3"}})
	if q[0] != "vue-router 3 navigation guards" || q[1] != q[0]+" documentation" {
		t.Errorf("queries = %q", q)
	}
}

// errors.Is through the refusal: ambiguous_site names the candidates.
func TestPickSiteNamesCandidates(t *testing.T) {
	hits := docsHits(fuseHits([][]search.Result{{
		hit("https://docs.one.test/a", "A docs"), hit("https://docs.two.test/a", "A docs"),
	}}))
	_, err := pickSite(chooseSites(hits, sitePrefs{}))
	if !errors.Is(err, ErrAmbiguousSite) || !strings.Contains(err.Error(), "https://docs.one.test/") {
		t.Errorf("err = %v, want ambiguous_site naming the sites", err)
	}
}

func TestQuestionWords(t *testing.T) {
	got := questionWords("how do I use redux toolkit createSlice in python")
	for _, want := range []string{"redux", "toolkit", "reduxtoolkit", "createslice"} {
		if !slices.Contains(got, want) {
			t.Errorf("questionWords = %q, want %q among them", got, want)
		}
	}
	if slices.Contains(got, "python") || slices.Contains(got, "how") {
		t.Errorf("questionWords = %q: language names and stopwords name no site", got)
	}
}

func TestSiteChoiceRules(t *testing.T) {
	for host, want := range map[string]bool{
		"add-streaming.lib-py.pages.dev": true, "deploy-preview-12--lib.netlify.app": true,
		"lib.pages.dev": false, "docs.lib.test": false,
	} {
		if got := isPreviewHost(host); got != want {
			t.Errorf("isPreviewHost(%s) = %v, want %v", host, got, want)
		}
	}
	for root, want := range map[string]bool{
		"https://lib.test/zh-cn/docs/": true, "https://fr.docs.lib.test/": true, "https://lib.test/en/": false,
		"https://lib.test/docs/": false, "https://api.lib.test/": false, "https://lib.test/": false,
	} {
		r, _ := parseWebRoot(root)
		if got := otherLocale(r); got != want {
			t.Errorf("otherLocale(%s) = %v, want %v", root, got, want)
		}
	}
	for root, want := range map[string]int{
		"https://lib.test/docs/": 0, "https://lib.test/en/stable/": 0, "https://lib.test/docs/15/": 1,
		"https://v1-32.docs.lib.test/": 1, "https://lib.test/devdocs/": 2, "https://lib.test/en/main/": 2,
	} {
		r, _ := parseWebRoot(root)
		if got := releaseRank(r); got != want {
			t.Errorf("releaseRank(%s) = %d, want %d", root, got, want)
		}
	}
}

// A top hit served from elsewhere (its docs moved, or sit under a release
// directory) moves the site to where it is served.
func TestServedSite(t *testing.T) {
	old, _ := url.Parse("https://docs.lib.test/en/guide/tools")
	served, _ := url.Parse("https://docs.lib.test/v1.2/en/guide/tools")
	root, _ := parseWebRoot("https://docs.lib.test/en/")
	site, _ := servedSite(sfSite{root: root, hits: []sfHit{{u: old}}}, &siteProfile{served: served}, nil)
	if site.root.String() != "https://docs.lib.test/v1.2/" || site.hits[0].u.String() != served.String() {
		t.Errorf("servedSite = %s with top hit %s, want the served page's tree", site.root, site.hits[0].u)
	}
}

// A Context7 library ID, which ketch 0.20 sent to Context7 by default, is
// a validation error naming -b context7, before anything is searched.
func TestSearchRefusesContext7ID(t *testing.T) {
	s := NewSearchFirst(NewWeb(nil), NewLocal(nil))
	for _, id := range []string{"/vercel/next.js", "/reactjs/react.dev", "/websites/react_dev/v18"} {
		o := s.GetDocsSelect(context.Background(), id, "routing", 0)
		if o.Status != Refused || o.Reason != "context7_id" || !strings.Contains(o.Err().Error(), "-b context7") {
			t.Errorf("%s: %+v", id, o)
		}
	}
	for _, spec := range []string{"vercel/next.js", "next", "https://nextjs.org/docs"} {
		if isContext7ID(spec) {
			t.Errorf("%s read as a Context7 ID", spec)
		}
	}
}
