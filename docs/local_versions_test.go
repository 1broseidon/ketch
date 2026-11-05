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
)

func snippetFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "snippets", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(b), "\n")
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

func indexAs(t *testing.T, f *fakeFetcher, store *docsindex.Store, root string) IndexReport {
	t.Helper()
	r, err := IndexLibrary(context.Background(), &config.Config{}, Deps{Fetcher: f}, store, IndexRequest{Library: root, Names: []string{"vue-router"}})
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
	f, store := vueSite(t), newStore(t)
	v3 := indexAs(t, f, store, "https://v3.vr.example.test/")
	v4 := indexAs(t, f, store, "https://vr.example.test/")
	if v3.Library.Version != "v3" || v4.Library.Version != "" || v3.Library.Key == v4.Library.Key {
		t.Fatalf("versions %q and %q under keys %s, %s", v3.Library.Version, v4.Library.Version, v3.Library.Key, v4.Library.Key)
	}
	checkCrossVersionLabels(t, store, v3.Library.Key)
	l := NewLocal(store)
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
	secs, err := store.Sections(key)
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

// A library indexed in an older format is never read: a query fails with
// reason index_outdated naming the rebuild, and the next build rebuilds
// it.
func TestLocalOutdatedFormat(t *testing.T) {
	f, store := exampleSite(), newStore(t)
	r := buildIndex(t, f, store)
	old := r.Library
	old.Format = 1
	if err := store.PutLibrary(old); err != nil {
		t.Fatal(err)
	}
	l := NewLocal(store)
	o := l.GetDocsSelect(context.Background(), idxRoot, "middleware", 0)
	if o.Status != Failed || o.Reason != "index_outdated" || !strings.Contains(o.Err().Error(), "rebuild it with ketch docs index "+idxRoot) {
		t.Errorf("an old-format library = %s/%s: %v", o.Status, o.Reason, o.Err())
	}
	if again := buildIndex(t, f, store); again.New != 5 || again.Library.Format != docsindex.FormatVersion {
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
			kept, versions, _ := keepOneVariant(uniquePages(candidateLinks(site, root)), root, c.others)
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
