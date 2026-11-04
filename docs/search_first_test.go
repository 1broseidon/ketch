package docs

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
	"github.com/1broseidon/ketch/internal/failure"
	"github.com/1broseidon/ketch/search"
)

// searchFirstOver is the backend over a fake site, a fake web search
// answering every query with hits, and a store embedding with bow.
func searchFirstOver(t *testing.T, f *fakeFetcher, hits []search.Result) (*SearchFirst, *docsindex.Store, *atomic.Int32, *bowEmbedder) {
	t.Helper()
	store, emb := newStore(t), &bowEmbedder{id: "test/bow"}
	var searches atomic.Int32
	w := NewWeb(f)
	w.sites = siteSearchFunc(func(context.Context, string, int) ([]search.Result, error) {
		searches.Add(1)
		return hits, nil
	})
	l := NewLocal(store, func() (embed.Embedder, error) { return emb, nil })
	return NewSearchFirst(w, l), store, &searches, emb
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
// releases left out), kept in the local index under the site's docs root,
// and ranked there; the answer is partial and says how to index the rest.
// Asked again, the index answers without searching.
func TestSearchFirstAnswersFromTheSite(t *testing.T) {
	f := exampleSite()
	s, store, searches, _ := searchFirstOver(t, f, exampleHits())
	o := s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if o.Status != Partial || o.Reason != ReasonPartialSite || o.Backend != SearchBackend {
		t.Fatalf("outcome = %s/%s from %q (%v), want partial/%s from search", o.Status, o.Reason, o.Backend, o.Err(), ReasonPartialSite)
	}
	if len(o.Results) == 0 || !strings.HasPrefix(o.Results[0].URL, idxRoot+"guide/middleware") {
		t.Fatalf("top result = %+v, want the middleware page", o.Results)
	}
	if !strings.Contains(o.Note, "ketch docs index "+idxRoot) {
		t.Errorf("note = %q, want the command that indexes the whole site", o.Note)
	}
	checkNeverRead(t, f)
	checkSearchLibrary(t, store)
	before := searches.Load()
	again := s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if searches.Load() != before {
		t.Errorf("asked again, the question searched the web; the local index holds the answer")
	}
	if again.Status != Partial || again.Backend != "local" || len(again.Results) == 0 {
		t.Errorf("again = %s from %q with %d results, want a partial local answer", again.Status, again.Backend, len(again.Results))
	}
}

func checkNeverRead(t *testing.T, f *fakeFetcher) {
	t.Helper()
	for _, c := range f.called() {
		if strings.Contains(c, "/private/") || strings.Contains(c, "/es/") || strings.Contains(c, "/v1/") || strings.Contains(c, "github.com") {
			t.Errorf("fetched %s: robots.txt, other locales and releases, and non-docs hits are never read", c)
		}
	}
}

func checkSearchLibrary(t *testing.T, store *docsindex.Store) {
	t.Helper()
	lib, found, err := store.Library(idxRoot)
	if err != nil || !found || lib.Resolved != resolvedBySearch || lib.Pages == 0 || lib.Vectors == 0 {
		t.Fatalf("stored library = %+v (found %v, %v), want one built by search with pages and vectors", lib, found, err)
	}
}

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
			s, _, _, _ := searchFirstOver(t, exampleSite(), tc.hits)
			o := s.SearchSelect(context.Background(), "add middleware", 0)
			if o.Status != tc.status || o.Reason != tc.reason || o.Kind() != tc.kind {
				t.Fatalf("outcome = %s/%s kind %v (%v), want %s/%s kind %v", o.Status, o.Reason, o.Kind(), o.Err(), tc.status, tc.reason, tc.kind)
			}
		})
	}
}

// A question embeds at most its window budget, the sections its words
// rank first; the rest are kept as text for later questions.
func TestSearchFirstEmbedsWithinBudget(t *testing.T) {
	s, store, _, emb := searchFirstOver(t, exampleSite(), exampleHits())
	s.embedWindows = 2
	o := s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if o.Status != Partial {
		t.Fatalf("outcome = %s (%v)", o.Status, o.Err())
	}
	if n := emb.passages.Load(); n > 2 {
		t.Errorf("embedded %d windows, want at most the budget of 2", n)
	}
	secs, err := store.Sections(idxRoot, bowDims, true)
	if err != nil {
		t.Fatal(err)
	}
	var withVec, without int
	for _, sec := range secs {
		if len(sec.Vectors) > 0 {
			withVec++
			if !strings.Contains(strings.ToLower(sec.Text+sec.Heading), "middleware") {
				t.Errorf("embedded %q, which names none of the question's words", sec.Heading)
			}
		} else {
			without++
		}
	}
	if withVec == 0 || without == 0 {
		t.Errorf("%d sections embedded, %d not; want some of each", withVec, without)
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
