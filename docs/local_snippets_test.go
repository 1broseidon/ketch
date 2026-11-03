package docs

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
)

func snippetFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "snippets", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(b), "\n")
}

// unitStrings renders units as kind, explanation and body, as shown.
func unitStrings(text string, units []docsindex.Unit) [][3]string {
	var out [][3]string
	for _, u := range units {
		e, b := unitParts(text, u)
		out = append(out, [3]string{u.Kind, e, b})
	}
	return out
}

// A section becomes: the prose before an explanation that it left out
// (long enough to stand alone); each run of code blocks (a TypeScript and
// a JavaScript tab are one run, with or without tab labels between them)
// with the sentences right before it, even with no blank line before the
// fence, a tab label aside; and the prose after the last code. Every part
// is the section's own text, and an explanation ends where its code (or
// its first tab label) starts.
func TestExtractUnitsCodeWithItsSentences(t *testing.T) {
	text := snippetFixture(t, "units.md")
	units := extractUnits(text)
	got := unitStrings(text, units)
	if len(got) != 5 {
		t.Fatalf("units = %d:\n%q", len(got), got)
	}
	// kind, explanation, the body's start and end
	want := [][4]string{
		{docsindex.UnitProse, "", "Routes map a method", "should not hide."},
		// three sentences; "e.g." ends none; both tabs in one unit
		{docsindex.UnitCode, "Use `app.get` to add a route. The handler receives the context, e.g. the request and its params. Return a response from it:", "```ts\napp.get('/'", "```js\napp.get('/', (c) => c.text('Hello'))\n```"},
		{docsindex.UnitCode, "Add a second route with a path parameter.", "```ts\napp.get('/user/:id'", "```"},
		{docsindex.UnitProse, "", "A route added after a catch-all", "wrap."},
		// tab labels between the blocks, not before the explanation's end
		{docsindex.UnitCode, "Run the app with your package manager. Pick one:", "```sh\nnpm run dev\n```\n\nbun\n\n```sh", "bun run dev\n```"},
	}
	for i, w := range want {
		if got[i][0] != w[0] || got[i][1] != w[1] || !strings.HasPrefix(got[i][2], w[2]) || !strings.HasSuffix(got[i][2], w[3]) {
			t.Errorf("unit %d = %q\nwant %q", i, got[i], w)
		}
	}
	assertVerbatimUnits(t, text, units)
}

// assertVerbatimUnits fails for a unit that is not the text's own words,
// or whose explanation does not end where its code (or a tab label before
// it) starts.
func assertVerbatimUnits(t *testing.T, text string, units []docsindex.Unit) {
	t.Helper()
	for _, u := range units {
		e, b := u.Parts(text)
		if !strings.Contains(text, b) || e != "" && !strings.Contains(text, e) {
			t.Errorf("unit %+v is not verbatim", u)
		}
		if u.Kind != docsindex.UnitCode || u.Explain == [2]int{} {
			continue
		}
		if between := strings.TrimSpace(text[u.Explain[1]:u.Body[0]]); strings.ContainsAny(between, " \n") {
			t.Errorf("explanation %q does not end where its code starts", e)
		}
	}
}

// A section with no code is one prose unit: its first unitProseChars,
// cut at a sentence end.
func TestExtractUnitsProseOnly(t *testing.T) {
	text := snippetFixture(t, "prose.md")
	units := extractUnits(text)
	if len(units) != 1 || units[0].Kind != docsindex.UnitProse {
		t.Fatalf("units = %+v", units)
	}
	_, body := unitParts(text, units[0])
	if len(body) > unitProseChars || !strings.HasSuffix(body, ".") || !strings.HasPrefix(text, body) || len(body) < unitProseChars/2 {
		t.Errorf("prose unit (%d bytes) = %q", len(body), body)
	}
	short := "Short section text, a single sentence."
	if u := extractUnits(short); len(u) != 1 || u[0].Body != [2]int{0, len(short)} {
		t.Errorf("a short section = %+v", u)
	}
}

// An explanation is the last three sentences before the code, within
// unitExplainChars; a single sentence longer than unitProseChars is cut
// at a word and shown starting with "…"; a list right before the code is
// its explanation, but a paragraph before the list is not added to it; a
// tab label ("Python 3.10+") is not an explanation, and a lead-in such as
// "For example:" reaches back to the paragraph before it.
func TestExplanationTrimming(t *testing.T) {
	five := "One is first. Two is second. Three is third. Four is fourth. Five is fifth:\n\n```sh\nrun\n```"
	if e, _ := unitParts(five, extractUnits(five)[0]); e != "Three is third. Four is fourth. Five is fifth:" {
		t.Errorf("last three sentences = %q", e)
	}
	long := strings.Repeat("word ", 200) + "ends here:\n\n```sh\nrun\n```"
	var code docsindex.Unit
	for _, u := range extractUnits(long) {
		if u.Kind == docsindex.UnitCode {
			code = u
		}
	}
	if e, _ := unitParts(long, code); !strings.HasPrefix(e, "… word") || len(e) > unitProseChars+len("… ") || !strings.HasSuffix(e, "ends here:") {
		t.Errorf("a long sentence's explanation = %q", e)
	}
	list := "Install it first.\n\n- npm: npm i x\n- yarn: yarn add x\n\n```sh\nnpm i x\n```"
	if e, _ := unitParts(list, extractUnits(list)[0]); e != "- npm: npm i x\n- yarn: yarn add x" {
		t.Errorf("a list before code = %q", e)
	}
	tab := "Use the middleware like this:\n\nPython 3.10+\n\n```py\napp.add_middleware(M)\n```"
	if e, _ := unitParts(tab, extractUnits(tab)[0]); e != "Use the middleware like this:" {
		t.Errorf("a tab label with spaces = %q", e)
	}
	lead := "Configure the client with a base URL and a timeout. The timeout is in seconds.\n\nFor example:\n\n```js\nnew Client({ timeout: 5 })\n```"
	if e, _ := unitParts(lead, extractUnits(lead)[0]); e != "Configure the client with a base URL and a timeout. The timeout is in seconds.\n\nFor example:" {
		t.Errorf("a lead-in reaches back a paragraph = %q", e)
	}
	none := "```sh\nnpm i x\n```"
	if u := extractUnits(none); len(u) != 1 || u[0].Explain != [2]int{} {
		t.Errorf("code with no prose = %+v", u)
	}
}

// A run of code longer than unitMaxCode splits between its blocks, each
// part keeping the explanation; one long block stays whole.
func TestExtractUnitsSplitsLongCodeRuns(t *testing.T) {
	block := "```py\n" + strings.Repeat("x = 1\n", 250) + "```" // ~1500 bytes
	text := "Two files:\n\n" + block + "\n\n" + block
	units := extractUnits(text)
	if len(units) != 2 || units[0].Explain != units[1].Explain || units[0].Explain == [2]int{} {
		t.Fatalf("units = %+v", units)
	}
	one := "Big:\n\n```py\n" + strings.Repeat("y = 2\n", 600) + "```"
	if u := extractUnits(one); len(u) != 1 {
		t.Errorf("one long block = %+v", u)
	}
}

// A section about a later major than the indexed one, by a migration or
// upgrade word and a version in its page title, path or headings, is
// labelled with that major; a guide from an earlier version, one naming
// no version, another product's version or an index with no version is
// not.
func TestCrossVersion(t *testing.T) {
	for _, c := range []struct {
		indexed, title string
		texts          []string
		want           string
	}{
		{"v3", "Vue Router", []string{"Migrating to Vue Router 4", "router.push"}, "4"},
		{"3.x", "", []string{"Upgrade guide", "Preparing for 4.0"}, "4"},
		{"2", "", []string{"guide upgrade to v3"}, "3"},
		{"v3", "", []string{"Migrating from 2.x to 3.x"}, ""},
		{"3", "", []string{"Upgrade guide"}, ""},
		{"1.8", "Poetry", []string{"Porting", "Migrating to Python 3"}, ""},
		{"3", "Vue Router", []string{"Vue Router 4 features"}, ""},
		{"", "Vue Router", []string{"Migrating to Vue Router 4"}, ""},
	} {
		if got := crossVersion(c.indexed, c.title, c.texts); got != c.want {
			t.Errorf("crossVersion(%q, %q, %q) = %q, want %q", c.indexed, c.title, c.texts, got, c.want)
		}
	}
	if !asksAbout("how to migrate push callbacks", "4") || !asksAbout("push in v4", "4") || asksAbout("router.push onComplete onAbort", "4") {
		t.Error("asksAbout")
	}
}

// vueSite is a Vue Router-like pair: v3's docs on their own host (which
// labels them v3), with a migration guide to v4 in the v3 tree, and v4's
// docs on the main host, each with a page about router.push.
func vueSite(t *testing.T) *fakeFetcher {
	push, migration := snippetFixture(t, "push-v3.md"), snippetFixture(t, "migration-v4.md")
	v4push := "# Programmatic Navigation\n\n## Navigate to a different location\n\nUse `router.push` to navigate. It returns a Promise, so wait for it to know when the navigation is done:\n\n```js\nawait router.push('/users/1')\n```\n"
	sitemap := func(host string, paths ...string) fakeRaw {
		var b strings.Builder
		for _, p := range paths {
			b.WriteString("<url><loc>https://" + host + p + "</loc></url>")
		}
		return xmlDoc(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + b.String() + `</urlset>`)
	}
	return &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://v3.vr.example.test/sitemap.xml": sitemap("v3.vr.example.test", "/guide/navigation", "/guide/migration-v4"),
			"https://vr.example.test/sitemap.xml":    sitemap("vr.example.test", "/guide/navigation"),
		},
		pages: map[string]fakePage{
			"https://v3.vr.example.test/guide/navigation":   {title: "Programmatic Navigation", markdown: push},
			"https://v3.vr.example.test/guide/migration-v4": {title: "Migrating to Vue Router 4", markdown: migration},
			"https://vr.example.test/guide/navigation":      {title: "Programmatic Navigation", markdown: v4push},
		},
	}
}

func indexAs(t *testing.T, f *fakeFetcher, store *docsindex.Store, emb embed.Embedder, root string) IndexReport {
	t.Helper()
	r, err := IndexLibrary(context.Background(), &config.Config{}, Deps{Fetcher: f}, store, IndexRequest{Library: root, Names: []string{"vue-router"}, Embedder: emb})
	if err != nil {
		t.Fatalf("IndexLibrary %s: %v", root, err)
	}
	return r
}

// Two versions of one library sit side by side, each under its own root
// and version: a query naming a version answers from that version's
// pages alone; a query naming none answers from the newest (the current
// docs, indexed without a version) and says so; a version not indexed is
// refused as not_indexed, naming the versions that are. The v3 tree's
// migration guide to v4 is kept, labelled, and ranks below v3's own
// answer for a question about v3's API.
func TestLocalVersionsSideBySide(t *testing.T) {
	f, store, emb := vueSite(t), newStore(t), &bowEmbedder{id: "test/bow"}
	v3 := indexAs(t, f, store, emb, "https://v3.vr.example.test/")
	v4 := indexAs(t, f, store, emb, "https://vr.example.test/")
	if v3.Library.Version != "v3" || v4.Library.Version != "" || v3.Library.Key == v4.Library.Key {
		t.Fatalf("versions %q and %q under keys %s, %s", v3.Library.Version, v4.Library.Version, v3.Library.Key, v4.Library.Key)
	}
	checkCrossVersionLabels(t, store, v3.Library.Key)
	l := NewLocal(store, func() (embed.Embedder, error) { return emb, nil })
	ctx := context.Background()
	q := "router.push onComplete onAbort callbacks"
	o := l.GetDocsSelect(ctx, "vue-router@3", q, 0)
	checkAnsweredFrom(t, o, "https://v3.vr.example.test/", "v3")
	if top := o.Results[0]; !strings.Contains(top.URL, "/guide/navigation") || !strings.Contains(top.Snippet, "onComplete") {
		t.Errorf("@3 top = %s:\n%s", top.URL, top.Snippet)
	}
	latest := l.GetDocsSelect(ctx, "vue-router", q, 0)
	checkAnsweredFrom(t, latest, "https://vr.example.test/", "current")
	if !strings.Contains(latest.Note, "also indexed: v3") {
		t.Errorf("no version: note %q", latest.Note)
	}
	missing := l.GetDocsSelect(ctx, "vue-router@5", q, 0)
	if missing.Status != Refused || missing.Reason != "not_indexed" || !errors.Is(missing.Err(), ErrNotIndexed) || !strings.Contains(missing.Err().Error(), "indexed versions: current, v3") {
		t.Errorf("@5 = %s/%s: %v", missing.Status, missing.Reason, missing.Err())
	}
}

// checkCrossVersionLabels fails unless exactly the migration guide's
// sections are labelled with the later major.
func checkCrossVersionLabels(t *testing.T, store *docsindex.Store, key string) {
	t.Helper()
	secs, err := store.Sections(key, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secs {
		if migration := strings.Contains(s.Page, "migration"); migration != (s.CrossVersion == "4") {
			t.Errorf("section %s %q labelled %q", s.Page, s.Heading, s.CrossVersion)
		}
	}
}

// checkAnsweredFrom fails unless o answered, every result from under
// prefix and labelled version.
func checkAnsweredFrom(t *testing.T, o Outcome, prefix, version string) {
	t.Helper()
	if o.Status != Answered || len(o.Results) == 0 {
		t.Fatalf("outcome = %+v", o)
	}
	for _, r := range o.Results {
		if !strings.HasPrefix(r.URL, prefix) || r.Version != version {
			t.Errorf("answered from %s (version %q), want %s (%s)", r.URL, r.Version, prefix, version)
		}
	}
}

// The demotion: of two sections equally near the question, the one about
// a later version ranks second, unless the question asks about it.
func TestRankUnitsDemotesCrossVersion(t *testing.T) {
	v := []float32{1, 0}
	sec := func(cross string) docsindex.Section {
		text := "Push a route:\n\n```js\nrouter.push('/x')\n```"
		return docsindex.Section{Heading: "push", Text: text, Units: extractUnits(text), CrossVersion: cross,
			Vectors: [][]float32{v}, UnitVectors: [][]float32{v}}
	}
	secs := []docsindex.Section{sec("4"), sec("")}
	if r := rankUnits(secs, v, nil, "push a route"); r[0].sec != 1 || r[0].score-r[1].score < crossVersionPenalty-1e-9 {
		t.Errorf("ranked %+v", r)
	}
	if r := rankUnits(secs, v, nil, "migrate push to v4"); r[0].sec != 0 || r[0].score != r[1].score {
		t.Errorf("a migration question ranked %+v", r)
	}
}

// The default answer is code-first snippets: each one unit, under its
// section's heading path and address, its explanation then its code,
// verbatim; within the budget and limit; rendered as "### path",
// snippet, "source: url".
func TestLocalSnippetsDefault(t *testing.T) {
	f, store, emb := exampleSite(), newStore(t), &bowEmbedder{id: "test/bow"}
	buildIndex(t, f, store, emb)
	l := NewLocal(store, func() (embed.Embedder, error) { return emb, nil })
	o := l.GetDocsSelect(context.Background(), "example", "add middleware to a route", 0)
	if o.Status != Answered || len(o.Results) == 0 || len(o.Results) > localMaxSnippets {
		t.Fatalf("outcome = %+v", o)
	}
	top := o.Results[0]
	want := "Register middleware on a route with app.use before the handler.\n\n```ts\napp.use('/admin/*', auth())\n```"
	if top.Snippet != want || top.URL != idxRoot+"guide/middleware#add-middleware-to-a-route" || top.Breadcrumb != "Middleware > Add middleware to a route" || top.Source != "local" {
		t.Errorf("top = %+v", top)
	}
	rendered := RenderSnippet(top)
	if rendered != "### Middleware > Add middleware to a route\n\n"+want+"\n\nsource: "+top.URL+"\n" {
		t.Errorf("rendered:\n%s", rendered)
	}
	total := 0
	for _, r := range o.Results {
		total += SnippetTokens(r)
		page, _, _ := strings.Cut(r.URL, "#")
		md := f.pages[page].markdown
		for _, part := range strings.Split(r.Snippet, "\n\n") {
			if part = strings.TrimSuffix(strings.TrimPrefix(part, "… "), " …"); !strings.Contains(md, part) {
				t.Errorf("snippet part not verbatim from %s: %q", page, part)
			}
		}
	}
	if total > localSnippetTokens {
		t.Errorf("%d tokens over the default budget", total)
	}
	ctx := WithLocalOptions(context.Background(), LocalOptions{Tokens: 40, Limit: 2})
	small := l.GetDocsSelect(ctx, "example", "add middleware to a route", 4000)
	if len(small.Results) != 1 {
		t.Errorf("a 40-token budget returned %d snippets", len(small.Results))
	}
	two := l.GetDocsSelect(WithLocalOptions(context.Background(), LocalOptions{Limit: 2}), "example", "middleware", 0)
	if len(two.Results) > 2 {
		t.Errorf("limit 2 returned %d", len(two.Results))
	}
}

// Diversity: at most localPerSection snippets from one section, and a
// near-duplicate of one already picked (the same docs at another
// address) is skipped.
func TestSnippetDiversity(t *testing.T) {
	text := "Do a:\n\n```sh\nrun a --fast\n```\n\nDo b:\n\n```sh\nrun b --slow\n```\n\nDo c:\n\n```sh\nrun c --other\n```"
	mk := func(page string) docsindex.Section {
		return docsindex.Section{Page: page, Heading: "Run", Crumbs: []string{"Run"}, Text: text, Units: extractUnits(text)}
	}
	other := "Configure it with a file named config.toml in the project directory:\n\n```toml\nname = 'x'\n```"
	secs := []docsindex.Section{mk("https://x.test/a"), mk("https://x.test/copy-of-a"),
		{Page: "https://x.test/b", Heading: "Config", Crumbs: []string{"Config"}, Text: other, Units: extractUnits(other)}}
	var ranked []scoredUnit
	score := 1.0
	for i, s := range secs {
		for j := range s.Units {
			ranked = append(ranked, scoredUnit{sec: i, unit: j, score: score})
			score -= 0.01
		}
	}
	got := snippetResults(docsindex.Library{Root: "https://x.test/"}, secs, ranked, false, 2000, 0)
	var pages []string
	for _, r := range got {
		pages = append(pages, r.URL)
	}
	if strings.Join(pages, " ") != "https://x.test/a https://x.test/a https://x.test/b" {
		t.Errorf("picked %v", pages)
	}
}

// A library indexed in an older format is never read: a query fails with
// reason index_outdated naming the rebuild, --reembed refuses, and the
// next build rebuilds it.
func TestLocalOutdatedFormat(t *testing.T) {
	f, store, emb := exampleSite(), newStore(t), &bowEmbedder{id: "test/bow"}
	r := buildIndex(t, f, store, emb)
	old := r.Library
	old.Format = 1
	if err := store.PutLibrary(old); err != nil {
		t.Fatal(err)
	}
	l := NewLocal(store, func() (embed.Embedder, error) { return emb, nil })
	o := l.GetDocsSelect(context.Background(), idxRoot, "middleware", 0)
	if o.Status != Failed || o.Reason != "index_outdated" || !strings.Contains(o.Err().Error(), "rebuild it with ketch docs index "+idxRoot) {
		t.Errorf("an old-format library = %s/%s: %v", o.Status, o.Reason, o.Err())
	}
	if _, err := Reembed(context.Background(), store, idxRoot, emb, nil); !errors.Is(err, ErrIndexOutdated) {
		t.Errorf("reembed of an old format = %v", err)
	}
	if again := buildIndex(t, f, store, emb); again.New != 5 || again.Library.Format != docsindex.FormatVersion {
		t.Errorf("rebuild = %+v", again)
	}
}

// Versions order: the current docs first, then releases by number,
// development docs last; equal ones by build time.
func TestNewerVersions(t *testing.T) {
	now := time.Now()
	cur := docsindex.Library{Key: "a", Root: "https://vr.test/", Version: ""}
	labelled := docsindex.Library{Key: "b", Root: "https://vr.test/", Version: "4.5"} // indexed as current
	v3 := docsindex.Library{Key: "c", Root: "https://v3.vr.test/", Version: "v3", Requested: "3"}
	v10 := docsindex.Library{Key: "d", Root: "https://vr.test/v10/", Version: "v10"}
	next := docsindex.Library{Key: "e", Root: "https://vr.test/next/", Version: "next", Requested: "next", Built: now}
	for _, c := range [][2]docsindex.Library{{cur, v3}, {labelled, v10}, {v10, v3}, {v3, next}} {
		if !newer(c[0], c[1]) || newer(c[1], c[0]) {
			t.Errorf("%s should be newer than %s", c[0].Key, c[1].Key)
		}
	}
	if lib, note := newestOf([]docsindex.Library{v3, labelled, v10}); lib.Key != "b" || note != "answered from version 4.5, the newest indexed; also indexed: v10, v3 (name@version picks one)" {
		t.Errorf("newest = %s, %q", lib.Key, note)
	}
}

// The plan keeps the indexed version's tree in every common layout: other
// hosts and paths outside a versioned root never qualify; below the root,
// a tree the site lists as another version, a release or channel tree
// right below the root, and a tree mostly of copies are left out, while
// trees that are no version (a REST /api/v1/, release notes) stay.
func TestPlanKeepsOneVersion(t *testing.T) {
	for _, c := range []struct {
		name, root string
		urls       []string
		others     map[string]bool
		want       []string
		versions   int
	}{
		{name: "versioned subdomain", root: "https://router.x.test/",
			urls: []string{"https://router.x.test/guide/push", "https://v3.router.x.test/guide/push", "https://v3.router.x.test/guide/old"},
			want: []string{"https://router.x.test/guide/push"}},
		{name: "versioned path root", root: "https://x.test/docs/1.8/",
			urls: []string{"https://x.test/docs/1.8/deps", "https://x.test/docs/deps", "https://x.test/docs/2.0/deps"},
			want: []string{"https://x.test/docs/1.8/deps"}},
		{name: "current tree beside a release with few copies", root: "https://x.test/docs/",
			urls: []string{"https://x.test/docs/deps", "https://x.test/docs/cli", "https://x.test/docs/1.8/deps", "https://x.test/docs/1.8/legacy-a", "https://x.test/docs/1.8/legacy-b"},
			want: []string{"https://x.test/docs/deps", "https://x.test/docs/cli"}, versions: 3},
		{name: "docs.rs", root: "https://docs.rs/serde/1.0.200/serde/",
			urls: []string{"https://docs.rs/serde/1.0.200/serde/de/", "https://docs.rs/serde/1.0.100/serde/de/", "https://docs.rs/serde/latest/serde/de/"},
			want: []string{"https://docs.rs/serde/1.0.200/serde/de/"}},
		{name: "rtd slug root", root: "https://u.readthedocs.io/en/stable/",
			urls: []string{"https://u.readthedocs.io/en/stable/user-guide", "https://u.readthedocs.io/en/1.26.x/user-guide", "https://u.readthedocs.io/en/latest/user-guide"},
			want: []string{"https://u.readthedocs.io/en/stable/user-guide"}},
		{name: "rtd host root", root: "https://u.readthedocs.io/",
			urls: []string{"https://u.readthedocs.io/en/stable/user-guide", "https://u.readthedocs.io/en/stable/advanced", "https://u.readthedocs.io/en/latest/user-guide", "https://u.readthedocs.io/en/latest/new-thing", "https://u.readthedocs.io/en/1.26.x/old-thing"},
			want: []string{"https://u.readthedocs.io/en/stable/user-guide", "https://u.readthedocs.io/en/stable/advanced"}, versions: 3},
		{name: "docusaurus", root: "https://d.test/docs/",
			urls: []string{"https://d.test/docs/intro", "https://d.test/docs/api", "https://d.test/docs/2.x/intro", "https://d.test/docs/2.x/old", "https://d.test/docs/next/intro", "https://d.test/docs/next/new-feature"},
			want: []string{"https://d.test/docs/intro", "https://d.test/docs/api"}, versions: 4},
		{name: "mike", root: "https://m.github.io/proj/",
			urls: []string{"https://m.github.io/proj/latest/usage/", "https://m.github.io/proj/latest/api/", "https://m.github.io/proj/1.0/usage/", "https://m.github.io/proj/0.9/legacy/"},
			want: []string{"https://m.github.io/proj/latest/usage/", "https://m.github.io/proj/latest/api/"}, versions: 2},
		{name: "a tree the site lists", root: "https://x.test/", others: map[string]bool{"3": true},
			urls: []string{"https://x.test/docs/guide", "https://x.test/docs/3/only-a", "https://x.test/docs/3/only-b"},
			want: []string{"https://x.test/docs/guide"}, versions: 2},
		{name: "the same tree unlisted", root: "https://x.test/",
			urls: []string{"https://x.test/docs/guide", "https://x.test/docs/3/only-a", "https://x.test/docs/3/only-b"},
			want: []string{"https://x.test/docs/guide", "https://x.test/docs/3/only-a", "https://x.test/docs/3/only-b"}},
		{name: "no versions at all", root: "https://x.test/docs/",
			urls: []string{"https://x.test/docs/guide", "https://x.test/docs/api/v2/users", "https://x.test/docs/api/v2/teams", "https://x.test/docs/releases/4.2/notes"},
			want: []string{"https://x.test/docs/guide", "https://x.test/docs/api/v2/users", "https://x.test/docs/api/v2/teams", "https://x.test/docs/releases/4.2/notes"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, err := parseWebRoot(c.root)
			if err != nil {
				t.Fatal(err)
			}
			var site webSite
			for _, raw := range c.urls {
				u, _ := url.Parse(raw)
				site.links = append(site.links, webLink{url: u})
			}
			kept, versions := keepOneVariant(uniquePages(candidateLinks(site, root)), root, c.others)
			var got []string
			for _, l := range kept {
				got = append(got, l.url.String())
			}
			if strings.Join(got, " ") != strings.Join(c.want, " ") || versions != c.versions {
				t.Errorf("kept %v (%d other versions)\nwant %v (%d)", got, versions, c.want, c.versions)
			}
		})
	}
}

// The site's version list names the other versions: every label and
// alias of every version but the one indexed (by its root or its label).
func TestOtherVersionLabels(t *testing.T) {
	root, _ := parseWebRoot("https://x.test/docs/")
	vs := versionSet{versions: []siteVersion{
		{label: "2.0", aliases: []string{"stable", "latest"}, root: "https://x.test/docs/", current: true},
		{label: "1.8", root: "https://x.test/docs/1.8/"},
		{label: "main", aliases: []string{"dev"}, root: "https://x.test/docs/main/"},
	}}
	got := otherVersionLabels(vs, root, "2.0")
	if len(got) != 3 || !got["1.8"] || !got["main"] || !got["dev"] || got["stable"] {
		t.Errorf("others = %v", got)
	}
	v18, _ := parseWebRoot("https://x.test/docs/1.8/")
	if got := otherVersionLabels(vs, v18, "1.8"); got["1.8"] || !got["2.0"] || !got["stable"] {
		t.Errorf("others of 1.8 = %v", got)
	}
}

// Of units equally near the question, code ranks before prose, and prose
// before a single listing longer than unitMaxCode.
func TestRankUnitsPrefersExampleCode(t *testing.T) {
	v := []float32{1, 0}
	sec := func(text string) docsindex.Section {
		units := extractUnits(text)
		vecs := make([][]float32, len(units))
		for i := range vecs {
			vecs[i] = v
		}
		return docsindex.Section{Heading: "x", Text: text, Units: units, Vectors: [][]float32{v}, UnitVectors: vecs}
	}
	prose := sec("Configure the client with a base URL and a timeout.")
	example := sec("Configure it:\n\n```js\nnew Client({ timeout: 5 })\n```")
	listing := sec("The source:\n\n```js\n" + strings.Repeat("const line = 1\n", 200) + "```")
	r := rankUnits([]docsindex.Section{prose, listing, example}, v, nil, "configure the client")
	if r[0].sec != 2 || r[1].sec != 0 || r[2].sec != 1 {
		t.Errorf("ranked %+v", r)
	}
}

// A unit naming more of the identifiers the question writes ranks higher,
// prose (inline code) as well as code: the paragraph naming router.push,
// onComplete and onAbort beats an example naming router.push alone, code
// bonus and all.
func TestRankUnitsIdentifiers(t *testing.T) {
	v := []float32{1, 0}
	sec := func(text string) docsindex.Section {
		units := extractUnits(text)
		vecs := make([][]float32, len(units))
		for i := range vecs {
			vecs[i] = v
		}
		return docsindex.Section{Heading: "push", Text: text, Units: units, Vectors: [][]float32{v}, UnitVectors: vecs}
	}
	example := sec("Push a path:\n\n```js\nrouter.push('home')\n```")
	callbacks := sec("Optionally provide `onComplete` and `onAbort` callbacks to `router.push` as the 2nd and 3rd arguments.")
	r := rankUnits([]docsindex.Section{example, callbacks}, v, nil, "router.push onComplete onAbort callbacks")
	if r[0].sec != 1 {
		t.Errorf("ranked %+v", r)
	}
}
