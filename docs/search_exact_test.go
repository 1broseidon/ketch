package docs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/search"
)

// Exact pages first (search_exact.go): the chosen site's search hits are
// read at their exact addresses, in rank order, of one release tree; only
// a distinctive term on none of them widens the read, in that tree.

const relRoot = "https://rel.example.test/docs/"

// releaseSiteFixture is a docs site with two releases (v6, v7) of the same
// pages, a sitemap listing both, and a reference page in each release that
// alone holds the term zorp_inate.
func releaseSiteFixture() *fakeFetcher {
	loc := func(paths ...string) string {
		var b strings.Builder
		for _, p := range paths {
			b.WriteString("<url><loc>" + relRoot + p + "</loc></url>")
		}
		return b.String()
	}
	return &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://rel.example.test/robots.txt": plain("User-agent: *\nAllow: /\n"),
			"https://rel.example.test/sitemap.xml": xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
				loc("v6/guide/alpha", "v6/guide/beta", "v6/guide/gamma", "v6/ref/zorp", "v7/guide/alpha", "v7/guide/cobalt", "v7/ref/zorp") + `</urlset>`),
		},
		pages: map[string]fakePage{
			relRoot + "v6/guide/alpha":  {title: "Alpha", markdown: "# Alpha\n\nAlpha widgets frobnicate on start.\n\n## Frobnicate options\n\nPass quux_mode to frobnicate a widget twice.\n"},
			relRoot + "v6/guide/beta":   {title: "Beta", markdown: "# Beta\n\nBeta widgets frobnicate on stop.\n\n## Teardown\n\nA beta widget is torn down after it frobnicates.\n"},
			relRoot + "v6/guide/gamma":  {title: "Gamma", markdown: "# Gamma\n\nGamma widgets never frobnicate.\n"},
			relRoot + "v6/ref/zorp":     {title: "Zorp reference", markdown: "# Zorp reference\n\n## zorp_inate\n\nCall zorp_inate after a widget frobnicates to reset it.\n"},
			relRoot + "v7/guide/alpha":  {title: "Alpha v7", markdown: "# Alpha\n\nIn v7 alpha widgets frobnicate lazily.\n"},
			relRoot + "v7/guide/cobalt": {title: "Cobalt", markdown: "# Cobalt\n\nCobalt widgets frobnicate in v7 only.\n"},
			relRoot + "v7/ref/zorp":     {title: "Zorp reference v7", markdown: "# Zorp reference\n\n## zorp_inate\n\nIn v7 zorp_inate is gone.\n"},
		},
	}
}

// releaseHits are a search's hits: a v6 page first, a v7 page second, a
// second v6 page third, and a forge issue that is never docs.
func releaseHits() []search.Result {
	return []search.Result{
		hit(relRoot+"v6/guide/alpha", "Alpha - Rel docs"),
		hit(relRoot+"v7/guide/cobalt", "Cobalt - Rel docs"),
		hit(relRoot+"v6/guide/beta", "Beta - Rel docs"),
		hit("https://github.com/rel/rel/issues/7", "frobnicate twice issue"),
	}
}

// readURLs are the pages an answer read, as its read list names them.
func readURLs(o Outcome) []string {
	var out []string
	for _, p := range o.Read {
		out = append(out, p.URL)
	}
	return out
}

// neverRead fails when f served a page (FetchPage), or a page list
// (llms.txt, a sitemap), whose address holds frag. The raw fetch of a
// site's top hit that profiles the site before it is read is no read.
func neverRead(t *testing.T, f *fakeFetcher, frag string) {
	t.Helper()
	for _, c := range f.called() {
		if strings.Contains(c, frag) && (strings.HasPrefix(c, "page ") || strings.Contains(c, "llms") || strings.Contains(c, "sitemap") || strings.HasSuffix(c, "/")) {
			t.Errorf("read %s", c)
		}
	}
}

// servedPages are the pages f served (FetchPage), in order.
func servedPages(f *fakeFetcher) []string {
	var out []string
	for _, c := range f.called() {
		if u, ok := strings.CutPrefix(c, "page "); ok {
			out = append(out, u)
		}
	}
	return out
}

func TestReleaseOf(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"https://x.test/docs/v6/guide/a", "https://x.test/docs/v6/ref/b", true},
		{"https://x.test/docs/v6/guide/a", "https://x.test/docs/v7/guide/a", false},
		{"https://expressjs.com/en/4x/api.html", "https://expressjs.com/en/api.html", false},
		{"https://expressjs.com/en/4x/api.html", "https://expressjs.com/en/4x/guide/routing.html", true},
		{"https://x.test/6.30.1/guide/x", "https://x.test/en/main/guide/y", false},
		{"https://x.test/en/main/guide/x", "https://x.test/en/main/api/y", true},
		{"https://x.test/en/guide/x", "https://x.test/fr/guide/x", false},
		{"https://x.test/docs/guide/a", "https://x.test/docs/api/b", true},
		{"https://x.test/docs/v2", "https://x.test/docs/guide", true}, // a page's own name is no release directory
		{"https://x.test/docs/v2/", "https://x.test/docs/guide", false},
		{"https://a.x.test/docs/a", "https://b.x.test/docs/a", false},
	} {
		a, b := mustURL(t, c.a), mustURL(t, c.b)
		if got := sameRelease(a, b); got != c.same {
			t.Errorf("sameRelease(%s, %s) = %v (%q, %q), want %v", c.a, c.b, got, releaseOf(a), releaseOf(b), c.same)
		}
	}
	for in, want := range map[string]string{
		"https://x.test/docs/v6/guide/a":       "x.test/v6",
		"https://expressjs.com/en/4x/api.html": "expressjs.com/en/4x",
		"https://x.test/EN/guide/a":            "x.test/en",
		"https://x.test/guide/a":               "x.test",
	} {
		if got := releaseOf(mustURL(t, in)); got != want {
			t.Errorf("releaseOf(%s) = %q, want %q", in, got, want)
		}
	}
}

// The site's hits are read at their exact addresses in rank order, and
// nothing else: no root page, no sitemap, no sibling, when the exact pages
// hold every distinctive term; read lists them with their ranks, and the
// answer prints the best page first.
func TestExactPagesFirst(t *testing.T) {
	f := exampleSite()
	s, _, _ := searchFirstOver(t, f, exampleHits())
	o := s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if o.Err() != nil || len(o.Results) == 0 {
		t.Fatalf("outcome %s (%v)", o.Status, o.Err())
	}
	want := []PageRead{{URL: idxRoot + "guide/middleware", Rank: 2}, {URL: idxRoot + "guide/routing", Rank: 5}}
	if !slices.Equal(o.Read, want) || o.Widened != nil || len(o.Skipped) != 0 || len(o.NotFound) != 0 {
		t.Errorf("read %+v widened %+v skipped %+v not_on_pages %v", o.Read, o.Widened, o.Skipped, o.NotFound)
	}
	if got := servedPages(f); !slices.Equal(got, []string{idxRoot + "guide/routing", idxRoot + "guide/middleware"}) &&
		!slices.Equal(got, []string{idxRoot + "guide/middleware", idxRoot + "guide/routing"}) {
		t.Errorf("pages read %v, want the two hits only", got)
	}
	assertNeverFetched(t, f, "sitemap", "llms", idxRoot+"api/", " "+idxRoot+"\n")
	for _, c := range f.called() {
		if strings.HasSuffix(c, " "+idxRoot) {
			t.Errorf("read the site root: %s", c)
		}
	}
	if !strings.HasPrefix(o.Results[0].URL, idxRoot+"guide/middleware") {
		t.Errorf("first result %s, want the best page's", o.Results[0].URL)
	}
	if len(o.OtherResults) == 0 || slices.ContainsFunc(o.OtherResults, func(r OtherResult) bool { return strings.Contains(r.URL, "guide/") }) {
		t.Errorf("other results %+v: want the hits not read, never a page read", o.OtherResults)
	}
}

// A /v7/ hit is never read beside a /v6/ first hit; the next hit of the
// first one's release is read in its place. With every term on the exact
// pages nothing more is read.
func TestExactSameRelease(t *testing.T) {
	f := releaseSiteFixture()
	s, _, _ := searchFirstOver(t, f, releaseHits())
	o := s.SearchSelect(context.Background(), "frobnicate widgets", 0)
	if o.Err() != nil || len(o.Results) == 0 {
		t.Fatalf("outcome %s (%v)", o.Status, o.Err())
	}
	want := []PageRead{{URL: relRoot + "v6/guide/alpha", Rank: 1}, {URL: relRoot + "v6/guide/beta", Rank: 3}}
	if !slices.Equal(o.Read, want) || o.Widened != nil {
		t.Errorf("read %+v widened %+v, want the v6 hits in rank order", o.Read, o.Widened)
	}
	neverRead(t, f, "/v7/")
	assertNeverFetched(t, f, "sitemap", "/ref/")
	if !slices.ContainsFunc(o.OtherResults, func(r OtherResult) bool { return r.URL == relRoot+"v7/guide/cobalt" && r.Rank == 2 }) {
		t.Errorf("other results %+v, want the v7 hit listed, not read", o.OtherResults)
	}
	for _, r := range o.Results {
		if strings.Contains(r.URL, "/v7/") {
			t.Errorf("printed %s from another release", r.URL)
		}
	}
}

// A hit that cannot be read is skipped with its cause, and the next hit
// read in its place.
func TestExactSkipsUnreadable(t *testing.T) {
	f := releaseSiteFixture()
	delete(f.pages, relRoot+"v6/guide/alpha")
	s, _, _ := searchFirstOver(t, f, append(releaseHits()[:3], hit(relRoot+"v6/guide/gamma", "Gamma - Rel docs")))
	o := s.SearchSelect(context.Background(), "frobnicate widgets", 0)
	if o.Err() != nil || len(o.Results) == 0 {
		t.Fatalf("outcome %s (%v)", o.Status, o.Err())
	}
	if len(o.Skipped) != 1 || o.Skipped[0].URL != relRoot+"v6/guide/alpha" || !strings.Contains(o.Skipped[0].Cause, "not found") {
		t.Errorf("skipped %+v, want the unreadable hit with its cause", o.Skipped)
	}
	if got := readURLs(o); len(got) == 0 || slices.Contains(got, relRoot+"v6/guide/alpha") || !slices.Contains(got, relRoot+"v6/guide/beta") {
		t.Errorf("read %v, want the next hits", got)
	}
	fm := NewFrontmatter("q", SearchBackend, "", o).Render()
	if !strings.Contains(fm, "\nskipped:\n  - "+relRoot+"v6/guide/alpha ("+o.Skipped[0].Cause+")\n") {
		t.Errorf("frontmatter:\n%s", fm)
	}
	for _, r := range o.OtherResults {
		if r.URL == relRoot+"v6/guide/alpha" {
			t.Errorf("a skipped hit listed among the other results: %+v", r)
		}
	}
}

// Widening: an identifier the question writes on none of the exact pages
// reads more pages of the first page's release (its sitemap's pages under
// /v6/), never another release, and says so: widened, with the terms.
func TestWidenOnlyWhenMissing(t *testing.T) {
	f := releaseSiteFixture()
	s, _, _ := searchFirstOver(t, f, releaseHits())
	tr := NewTrace("frobnicate widgets zorp_inate")
	o := s.SearchSelect(WithTrace(context.Background(), tr), "frobnicate widgets zorp_inate", 0)
	if o.Err() != nil || len(o.Results) == 0 {
		t.Fatalf("outcome %s (%v)", o.Status, o.Err())
	}
	if !slices.Contains(tr.Fallbacks, "widened") {
		t.Errorf("trace fallbacks %v, want widened", tr.Fallbacks)
	}
	if o.Widened == nil || len(o.Widened.Terms) != 1 || !strings.HasPrefix(o.Widened.Terms[0], "zorp_inate") || o.Widened.Pages == 0 || o.Widened.Root != relRoot+"v6/" {
		t.Fatalf("widened %+v, want more v6 pages for zorp_inate", o.Widened)
	}
	if !slices.Equal(readURLs(o), []string{relRoot + "v6/guide/alpha", relRoot + "v6/guide/beta"}) {
		t.Errorf("read %v: the exact pages stay the read list", readURLs(o))
	}
	neverRead(t, f, "/v7/")
	if !slices.ContainsFunc(o.Results, func(r Result) bool { return strings.HasPrefix(r.URL, relRoot+"v6/ref/zorp") }) {
		t.Errorf("results %v, want the widened page that holds zorp_inate", resultURLs(o.Results))
	}
	if len(o.NotFound) != 0 {
		t.Errorf("not_on_pages %v after widening found the term", o.NotFound)
	}
	checkWidenedPrint(t, o)
}

// checkWidenedPrint: the frontmatter says it widened, for which terms,
// and the exact pages print first, the widened pages after them.
func checkWidenedPrint(t *testing.T, o Outcome) {
	t.Helper()
	fm := NewFrontmatter("q", SearchBackend, "", o).Render()
	if !strings.Contains(fm, "\nwidened: read ") || !strings.Contains(fm, " more page") || !strings.Contains(fm, " of "+relRoot+"v6/ for: zorp_inate") {
		t.Errorf("frontmatter:\n%s", fm)
	}
	at := slices.IndexFunc(o.Results, func(r Result) bool { return strings.Contains(r.URL, "/ref/") })
	for _, r := range o.Results[:max(at, 0)] {
		if !strings.Contains(r.URL, "/guide/") {
			t.Errorf("results %v: a widened page before the exact pages", resultURLs(o.Results))
		}
	}
}

// Every term on the exact pages: nothing more is read; a plain word on
// none of them, beside one they hold: not widened (it is the docs' other
// wording), not listed in not_on_pages, and the pages still answer.
func TestNoWidenWhenAllHeld(t *testing.T) {
	f := releaseSiteFixture()
	s, _, _ := searchFirstOver(t, f, releaseHits())
	tr := NewTrace("frobnicate quux_mode")
	if o := s.SearchSelect(WithTrace(context.Background(), tr), "frobnicate quux_mode", 0); o.Widened != nil || o.Err() != nil || slices.Contains(tr.Fallbacks, "widened") {
		t.Errorf("all terms on the exact pages: widened %+v (%v), fallbacks %v", o.Widened, o.Err(), tr.Fallbacks)
	}
	assertNeverFetched(t, f, "sitemap", "/ref/")
	neverRead(t, f, "/v7/")

	// A plain word on no exact page, beside one they hold: not widened,
	// and named missing.
	f = releaseSiteFixture()
	s, _, _ = searchFirstOver(t, f, releaseHits())
	o := s.SearchSelect(context.Background(), "frobnicate quux_mode xylograph", 0)
	if o.Err() != nil || o.Widened != nil || !slices.Equal(o.NotFound, []string{"xylograph"}) || len(o.Results) == 0 {
		t.Errorf("a term nowhere: widened %+v not_on_pages %v, %d results (%v)", o.Widened, o.NotFound, len(o.Results), o.Err())
	}
	neverRead(t, f, "/v7/")
	// A plain word is the asker's, not the docs': not_on_pages lists only
	// identifiers, and none is missing.
	if fm := NewFrontmatter("q", SearchBackend, "", o).Render(); !strings.Contains(fm, "\nnot_on_pages: []\n") {
		t.Errorf("frontmatter:\n%s", fm)
	}
}

// A question whose only distinctive term is on no page of the first
// hit's release is answered from that release (read, widened,
// not_on_pages naming the term): no page of another release is read.
func TestTermNowhereStaysInRelease(t *testing.T) {
	f := releaseSiteFixture()
	s, _, _ := searchFirstOver(t, f, releaseHits())
	o := s.SearchSelect(context.Background(), "frobnicate widgets xylograph", 0)
	neverRead(t, f, "/v7/")
	if o.Err() != nil || !slices.Equal(readURLs(o), []string{relRoot + "v6/guide/alpha", relRoot + "v6/guide/beta"}) || !slices.Contains(o.NotFound, "xylograph") {
		t.Errorf("read %v not_on_pages %v (%v), want the v6 pages and the term named", readURLs(o), o.NotFound, o.Err())
	}
}

// A question with one word the exact pages hold (on one section of a
// few) and one they lack prints the section that holds it, and names the
// other: a handful of pages read makes a word in one of their sections
// look common, which must not leave the answer empty.
func TestLiveAnswerPrintsTheHeldWord(t *testing.T) {
	f := releaseSiteFixture()
	f.pages[relRoot+"v6/guide/alpha"] = fakePage{title: "Alpha", markdown: "# Alpha\n\nAlpha widgets frobnicate on start.\n\n## Options\n\nPass quux to frobnicate a widget twice.\n"}
	s, _, _ := searchFirstOver(t, f, []search.Result{releaseHits()[0], releaseHits()[2]})
	o := s.SearchSelect(context.Background(), "quux xylograph", 0)
	if o.Err() != nil || !slices.Equal(o.NotFound, []string{"xylograph"}) {
		t.Fatalf("outcome %s (%v) not_on_pages %v", o.Status, o.Err(), o.NotFound)
	}
	if !slices.ContainsFunc(o.Results, func(r Result) bool { return strings.Contains(r.Snippet, "quux") }) {
		t.Errorf("results %v, want the section holding quux", resultURLs(o.Results))
	}
}

// A site whose hits cannot be read, while the search returned other
// results, prints those results: no refusal, the skipped pages and why.
func TestUnreadableSiteResultsOnly(t *testing.T) {
	f := releaseSiteFixture()
	f.pages = map[string]fakePage{}
	s, _, _ := searchFirstOver(t, f, releaseHits())
	o := s.SearchSelect(context.Background(), "frobnicate widgets", 0)
	if o.Err() != nil || o.Reason != "results_only" || len(o.Results) != 0 || !o.Evidence() {
		t.Fatalf("outcome %s/%s (%v), want the search results alone", o.Status, o.Reason, o.Err())
	}
	if !strings.HasPrefix(o.Note, "no page was printed: ") || len(o.Skipped) == 0 {
		t.Errorf("note %q skipped %+v", o.Note, o.Skipped)
	}
	if !slices.ContainsFunc(o.OtherResults, func(r OtherResult) bool { return strings.Contains(r.URL, "github.com") }) {
		t.Errorf("other results %+v", o.OtherResults)
	}
	out := NewFrontmatter("q", SearchBackend, "", o).Render() + RenderAnswer(o, HintCLI)
	if !strings.Contains(out, "\nnote: no page was printed: ") || !strings.Contains(out, "## Other results\n") || strings.Contains(out, "not_on_pages") {
		t.Errorf("output:\n%s", out)
	}
}

// A second identical call within cache_ttl prints byte-identical output
// with the network down and the web search failing: the read, skipped
// and widened lines, the outline and the other results come from the
// question cache and the page cache.
func TestRepeatPrintsTheSameOffline(t *testing.T) {
	f := releaseSiteFixture()
	delete(f.pages, relRoot+"v6/guide/beta")
	hits := append(releaseHits(), hit(relRoot+"v6/guide/gamma", "Gamma - Rel docs"))
	s, _, _, searches := searchFirstCached(t, f, hits)
	for _, style := range []HintStyle{HintCLI, HintMCP} {
		ctx := WithLocalOptions(context.Background(), LocalOptions{Hint: style})
		const q = "frobnicate widgets zorp_inate"
		first := s.SearchSelect(ctx, q, 0)
		if first.Err() != nil || len(first.Skipped) == 0 || first.Widened == nil || len(first.OtherResults) == 0 {
			t.Fatalf("first: %s (%v) skipped %+v widened %+v others %+v", first.Status, first.Err(), first.Skipped, first.Widened, first.OtherResults)
		}
		down := errors.New("dial tcp: network is unreachable")
		f.fail = down
		s.web.sites = siteSearchFunc(func(context.Context, string, int) ([]search.Result, error) { return nil, down })
		searched, fetched := searches.Load(), len(f.called())
		again := s.SearchSelect(ctx, q, 0)
		if len(f.called()) != fetched || searches.Load() != searched {
			t.Errorf("the repeat touched the network: %v", f.called()[fetched:])
		}
		a := NewFrontmatter(q, SearchBackend, "", first).Render() + RenderAnswer(first, style)
		b := NewFrontmatter(q, SearchBackend, "", again).Render() + RenderAnswer(again, style)
		if a != b || again.Err() != nil {
			t.Errorf("style %d: the repeat (%v) prints\n%s\nthe first printed\n%s", style, again.Err(), b, a)
		}
		checkCached(t, first, again)
		f.fail = nil
		s.web.sites = siteSearchFunc(func(context.Context, string, int) ([]search.Result, error) { searches.Add(1); return hits, nil })
	}
}

// Every address an answer from pages read prints (each source line, each
// outline entry) resolves with --section: on the same state right after
// the answer, and on a fresh state with no question cache.
func TestPrintedAddressesResolve(t *testing.T) {
	f := releaseSiteFixture()
	s, _, _, _ := searchFirstCached(t, f, releaseHits())
	ctx := WithLocalOptions(context.Background(), LocalOptions{Hint: HintCLI, Tokens: 120})
	o := s.SearchSelect(ctx, "frobnicate widgets zorp_inate", 0)
	if o.Err() != nil || len(o.Results) == 0 {
		t.Fatalf("answer %s (%v)", o.Status, o.Err())
	}
	refs := resultURLs(o.Results)
	for _, e := range o.Outline {
		refs = append(refs, e.Section)
	}
	for _, m := range o.OutlineMore {
		refs = append(refs, m.Page)
	}
	for _, d := range o.DefinedIn {
		refs = append(refs, d.Section)
	}
	if len(o.Outline) == 0 && len(o.OutlineMore) == 0 {
		t.Fatalf("no outline at %d tokens: the test needs one", 120)
	}
	fresh, _, _, _ := searchFirstCached(t, releaseSiteFixture(), nil)
	for _, ref := range refs {
		if sec := s.SectionSelect(ctx, "", ref, 0, 0); sec.Err() != nil || len(sec.Results) == 0 {
			t.Errorf("--section %s right after: %s (%v)", ref, sec.Status, sec.Err())
		}
		if sec := fresh.SectionSelect(ctx, "", ref, 0, 0); sec.Err() != nil || len(sec.Results) == 0 {
			t.Errorf("--section %s on a fresh state: %s (%v)", ref, sec.Status, sec.Err())
		}
	}
}

// An exact page served from another host of its registrable domain (the
// docs moved from docs.x to x/docs/) is read and cited where it is served;
// one served from another site is not read.
func TestExactServedOnSite(t *testing.T) {
	moved := "https://example.test/docs/guide/middleware"
	f := exampleSite()
	f.pages[moved] = f.pages[idxRoot+"guide/middleware"]
	delete(f.pages, idxRoot+"guide/middleware")
	f.redirects = map[string]string{idxRoot + "guide/middleware": moved}
	s, _, _ := searchFirstOver(t, f, exampleHits())
	o := s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if o.Err() != nil || !slices.Contains(readURLs(o), moved) {
		t.Fatalf("read %v (%v), want the page where it is served", readURLs(o), o.Err())
	}
	if !slices.ContainsFunc(o.Results, func(r Result) bool { return strings.HasPrefix(r.URL, moved+"#") }) {
		t.Errorf("results %v, want the page cited where it is served", resultURLs(o.Results))
	}

	away := exampleSite()
	away.pages["https://elsewhere.test/middleware"] = away.pages[idxRoot+"guide/middleware"]
	away.redirects = map[string]string{idxRoot + "guide/middleware": "https://elsewhere.test/middleware"}
	s, _, _ = searchFirstOver(t, away, exampleHits())
	o = s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if slices.ContainsFunc(readURLs(o), func(u string) bool { return strings.Contains(u, "elsewhere.test") }) {
		t.Errorf("read %v: a page served from another site", readURLs(o))
	}
}

// exactCandidates puts the page the library URL names first, then the
// site's hits (every docs tree of it) by rank, then the root resolve found
// when it is no hit.
func TestExactCandidates(t *testing.T) {
	h := func(u string, rank int) sfHit { return sfHit{u: mustURL(t, u), rank: rank, title: "T"} }
	site := sfSite{root: rootOf(relRoot + "v6/"), hits: []sfHit{h(relRoot+"v6/b", 3), h(relRoot+"v6/a", 1)},
		alts: []sfSite{{root: rootOf(relRoot + "v7/"), hits: []sfHit{h(relRoot+"v7/c", 2)}}},
		page: mustURL(t, relRoot+"v6/named"), pageVia: "the library URL", fallback: mustURL(t, relRoot+"v6/")}
	var got []string
	for _, c := range exactCandidates(site) {
		got = append(got, strings.TrimPrefix(c.link.url.String(), relRoot)+"|"+c.via+"|"+string(rune('0'+c.rank)))
	}
	want := []string{"v6/named|the library URL|0", "v6/a||1", "v7/c||2", "v6/b||3", "v6/|the docs root resolve found|0"}
	if !slices.Equal(got, want) {
		t.Errorf("candidates\n got %q\nwant %q", got, want)
	}
	// A named page that is a hit keeps the hit's rank.
	site.page = mustURL(t, relRoot+"v6/b")
	if c := exactCandidates(site)[0]; c.rank != 3 || c.via != "" {
		t.Errorf("named page that is a hit: %+v", c)
	}
}
