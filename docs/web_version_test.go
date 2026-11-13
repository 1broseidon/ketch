package docs

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/testutil"
)

func TestVersionLabelRe(t *testing.T) {
	for _, s := range []string{"5.2", "v3.1.1", "2.x", "4x", "1.0rc1", "2.0.0-beta1", "3", "next", "Stable", "main", "devdocs", "lts", "9.0.x"} {
		if !versionLabelRe.MatchString(s) {
			t.Errorf("%q should be a version label", s)
		}
	}
	for _, s := range []string{"docs", "en", "guide", "v", "1.38.2.dev0", "x", "api-v2", "release-notes", ""} {
		if versionLabelRe.MatchString(s) {
			t.Errorf("%q should not be a version label", s)
		}
	}
	for _, s := range []string{"404", "500", "2024"} {
		if _, ok := segmentLabel(s); ok {
			t.Errorf("segment %q should never be a version", s)
		}
	}
	if l, ok := segmentLabel("~1.38"); !ok || l != "1.38" {
		t.Errorf("docs.rs ~1.38 = %q %v", l, ok)
	}
	if l, ok := segmentLabel("cobra@v1.8.0"); !ok || l != "v1.8.0" {
		t.Errorf("pkg.go.dev cobra@v1.8.0 = %q %v", l, ok)
	}
}

func TestRootVersion(t *testing.T) {
	for _, c := range []struct {
		root  string
		label string
		seg   int
	}{
		{"https://docs.djangoproject.com/en/6.1/", "6.1", 1},
		{"https://docs.djangoproject.com/en/stable/", "stable", 1},
		{"https://docs.python.org/3/library/", "3", 0},
		{"https://docs.rs/tokio/~1.38/tokio/", "1.38", 1},
		{"https://docs.rs/next/latest/next/", "latest", 1},
		{"https://pkg.go.dev/github.com/go-chi/chi/v5@v5.0.12", "v5.0.12", 3},
		{"https://pkg.go.dev/github.com/go-chi/chi/v5", "", -1},
		{"https://v2.vite.dev/", "v2", -1},
		{"https://docusaurus.io/docs/2.x/", "2.x", 1},
		{"https://expressjs.com/en/4x/api.html", "4x", 1},
		{"https://nextjs.org/docs/", "", -1},
		{"https://vite.dev/", "", -1},
		{"https://next.org/", "", -1},
	} {
		root, err := parseWebRoot(c.root)
		if err != nil {
			t.Fatal(err)
		}
		if label, seg := rootVersion(root); label != c.label || seg != c.seg {
			t.Errorf("rootVersion(%s) = %q, %d; want %q, %d", c.root, label, seg, c.label, c.seg)
		}
	}
}

// djangoVersions is the version list the Django fixture's switcher gives.
func djangoVersions() []siteVersion {
	var vs []siteVersion
	for _, l := range []string{"6.1", "dev", "6.0", "5.2", "5.1", "5.0", "4.2"} {
		vs = append(vs, siteVersion{label: l, root: "https://docs.djangoproject.com/en/" + l + "/", current: l == "6.1"})
	}
	return vs
}

func TestMatchVersion(t *testing.T) {
	django := djangoVersions()
	rtd := []siteVersion{
		{label: "9.1.1", aliases: []string{"stable"}, current: true},
		{label: "latest"}, {label: "9.0.x"}, {label: "8.4.x"}, {label: "8.3.x"},
	}
	express := []siteVersion{{label: "5x", current: true}, {label: "4x"}, {label: "3x"}}
	docusaurus := []siteVersion{{label: "3.10.2", current: true}, {label: "next"}, {label: "3.9.2"}, {label: "3.8.1"}, {label: "2.x"}}
	for _, c := range []struct {
		want     string
		versions []siteVersion
		label    string // "" for no match
	}{
		{"5.2", django, "5.2"},
		{"v5.2", django, "5.2"},
		{"4.0", django, ""}, // not offered; 4.2 is a different minor
		{"5", django, "5.2"},
		{"4", django, "4.2"},
		{"5.2.3", django, "5.2"},
		{"7", django, ""},
		{"stable", django, "6.1"},
		{"latest", django, "6.1"},
		{"main", django, "dev"},
		{"next", django, "dev"},
		{"stable", rtd, "9.1.1"}, // alias
		{"latest", rtd, "latest"},
		{"9", rtd, "9.1.1"},
		{"9.0", rtd, "9.0.x"},
		{"8.4.2", rtd, "8.4.x"},
		{"4", express, "4x"},
		{"4.17.1", express, "4x"},
		{"3", docusaurus, "3.10.2"},
		{"2.0.0", docusaurus, "2.x"},
		{"canary", docusaurus, "next"},
		{"", django, ""},
		{"nonsense", django, ""},
	} {
		got, ok := matchVersion(c.want, c.versions)
		if ok != (c.label != "") || got.label != c.label {
			t.Errorf("matchVersion(%q) = %q %v, want %q", c.want, got.label, ok, c.label)
		}
	}
}

func TestVersionHolds(t *testing.T) {
	u := func(s string) *url.URL { v, _ := url.Parse(s); return v }
	for _, c := range []struct {
		want, served string
		holds        bool
	}{
		{"1.38", "https://docs.rs/tokio/1.38.2/tokio/", true},
		{"1.38", "https://docs.rs/tokio/1.54.1/tokio/", false}, // docs.rs read 1.38 as ^1.38
		{"1.38.2", "https://docs.rs/tokio/1.38.2/tokio/", true},
		{"5.2", "https://docs.djangoproject.com/en/5.2/", true},
		{"5.2", "https://docs.djangoproject.com/en/stable/", false}, // an old version redirected to an alias
		{"5.2.3", "https://docs.djangoproject.com/en/5.2/", true},
		{"stable", "https://docs.djangoproject.com/en/6.1/", true},
		{"stable", "https://docs.djangoproject.com/", false},
		{"dev", "https://docs.djangoproject.com/en/6.1/", false},
		{"dev", "https://docs.djangoproject.com/en/dev/", true},
		{"main", "https://docs.djangoproject.com/en/dev/", true},
		{"stable", "https://requests.readthedocs.io/en/latest/", false}, // latest is the dev build on Read the Docs
		{"2", "https://v2.vite.dev/", true},
		{"3", "https://v2.vite.dev/", false},
		{"v1.8.0", "https://pkg.go.dev/github.com/spf13/cobra@v1.8.0", true},
	} {
		if got := versionHolds(c.want, u(c.served)); got != c.holds {
			t.Errorf("versionHolds(%q, %s) = %v", c.want, c.served, got)
		}
	}
	if versionHolds("1.0", nil) {
		t.Error("no served URL never holds")
	}
}

// The post-check for a matched version: click@8.5 is the stable root,
// labelled 8.5.x by its build, served where asked; a root that redirected
// must still carry want or one of the version's labels.
func TestVersionRootHolds(t *testing.T) {
	u := func(s string) *url.URL { v, _ := url.Parse(s); return v }
	click := siteVersion{label: "8.5.x", aliases: []string{"stable"}, root: "https://click.palletsprojects.com/en/stable/"}
	tokio := siteVersion{label: "1.38", root: "https://docs.rs/tokio/~1.38/tokio/"}
	floating := siteVersion{label: "1.38", root: "https://docs.rs/tokio/1.38/tokio/"}
	django := siteVersion{label: "5.2", root: "https://docs.djangoproject.com/en/5.2/"}
	for _, c := range []struct {
		want   string
		v      siteVersion
		served string
		holds  bool
	}{
		{"8.5", click, "https://click.palletsprojects.com/en/stable/", true},
		{"8.5", click, "https://click.palletsprojects.com/en/latest/", false},
		{"1.38", tokio, "https://docs.rs/tokio/1.38.2/tokio/", true},
		{"1.38", floating, "https://docs.rs/tokio/1.54.1/tokio/", false},
		{"5.2", django, "https://docs.djangoproject.com/en/5.2/", true},
		{"5.2", django, "https://docs.djangoproject.com/en/stable/", false},
	} {
		if got := versionRootHolds(c.want, c.v, u(c.served)); got != c.holds {
			t.Errorf("versionRootHolds(%q, %s, %s) = %v", c.want, c.v.root, c.served, got)
		}
	}
	if versionRootHolds("1", tokio, nil) {
		t.Error("no served URL never holds")
	}
}

func TestRegistryVersionRoot(t *testing.T) {
	tokio := pkgInfo{eco: ecoCrates, name: "tokio", registryDocs: "https://docs.rs/tokio/latest/tokio/"}
	cobra := pkgInfo{eco: ecoGo, name: "github.com/spf13/cobra", registryDocs: "https://pkg.go.dev/github.com/spf13/cobra"}
	goList := []string{"v1.10.1", "v1.10.0", "v1.9.1", "v1.9.0-rc.1", "v1.8.1", "v1.8.0", "v0.0.5"}
	for _, c := range []struct {
		p          pkgInfo
		want       string
		goVersions []string
		label      string
		root       string // "" for no root
	}{
		{tokio, "1.38", nil, "1.38", "https://docs.rs/tokio/~1.38/tokio/"},
		{tokio, "v1", nil, "1", "https://docs.rs/tokio/~1/tokio/"},
		{tokio, "1.38.2", nil, "1.38.2", "https://docs.rs/tokio/1.38.2/tokio/"},
		{tokio, "1.x", nil, "1", "https://docs.rs/tokio/~1/tokio/"},
		{tokio, "latest", nil, "latest", "https://docs.rs/tokio/latest/tokio/"},
		{tokio, "dev", nil, "", ""},
		{cobra, "1.8", goList, "v1.8.1", "https://pkg.go.dev/github.com/spf13/cobra@v1.8.1"},
		{cobra, "v1.8.0", goList, "v1.8.0", "https://pkg.go.dev/github.com/spf13/cobra@v1.8.0"},
		{cobra, "1", goList, "v1.10.1", "https://pkg.go.dev/github.com/spf13/cobra@v1.10.1"},
		{cobra, "1.9", goList, "v1.9.1", "https://pkg.go.dev/github.com/spf13/cobra@v1.9.1"},
		{cobra, "2", goList, "", ""},
		{cobra, "1.8.0", nil, "v1.8.0", "https://pkg.go.dev/github.com/spf13/cobra@v1.8.0"},
		{cobra, "stable", goList, "latest", "https://pkg.go.dev/github.com/spf13/cobra"},
		{pkgInfo{eco: ecoPyPI, name: "click"}, "8.1", nil, "", ""},
	} {
		got, ok := registryVersionRoot(c.p, c.want, c.goVersions)
		if ok != (c.root != "") || got.root != c.root || got.label != c.label {
			t.Errorf("registryVersionRoot(%s, %q) = %+v %v, want %q at %s", c.p.name, c.want, got, ok, c.label, c.root)
		}
	}
}

// tokio@1.38 asks docs.rs for ~1.38, which redirects to 1.38.2 and holds;
// a bare /1.38/ floats to 1.54.1, and the post-check rejects it.
func TestVersionsTokioPostCheck(t *testing.T) {
	w, _ := newFixtureSiteWeb(t, nil, "versions")
	ctx := context.Background()
	tokio := pkgInfo{eco: ecoCrates, name: "tokio", registryDocs: "https://docs.rs/tokio/latest/tokio/"}
	v, ok := registryVersionRoot(tokio, "1.38", nil)
	if !ok || v.root != "https://docs.rs/tokio/~1.38/tokio/" {
		t.Fatalf("registryVersionRoot = %+v %v", v, ok)
	}
	for _, c := range []struct {
		root, served string
		holds        bool
	}{
		{v.root, "https://docs.rs/tokio/1.38.2/tokio/", true},
		{"https://docs.rs/tokio/1.38/tokio/", "https://docs.rs/tokio/1.54.1/tokio/", false},
	} {
		raw, err := w.fetcher.FetchRaw(ctx, c.root)
		if err != nil {
			t.Fatal(err)
		}
		served, _ := url.Parse(raw.URL)
		if raw.URL != c.served || versionHolds("1.38", served) != c.holds {
			t.Errorf("%s served %s, holds %v", c.root, raw.URL, versionHolds("1.38", served))
		}
		requested, _ := parseWebRoot(c.root)
		if got, ok := canonicalRoot(requested, served); !ok || got.String() != c.served {
			t.Errorf("canonicalRoot(%s, %s) = %s %v", c.root, raw.URL, got, ok)
		}
	}
}

func TestCanonicalRoot(t *testing.T) {
	for _, c := range []struct {
		requested, served, want string // want "" for a rejected redirect
	}{
		{"https://docs.djangoproject.com/en/stable/", "https://docs.djangoproject.com/en/6.1/", "https://docs.djangoproject.com/en/6.1/"},
		{"https://requests.readthedocs.io/", "https://requests.readthedocs.io/en/latest/", "https://requests.readthedocs.io/en/latest/"},
		{"https://fastapi-users.github.io/fastapi-users/", "https://fastapi-users.github.io/fastapi-users/latest/", "https://fastapi-users.github.io/fastapi-users/latest/"},
		{"https://docs.test/en/stable/", "https://docs.test/en/stable/index.html", "https://docs.test/en/stable/"},
		{"https://docs.test/docs/", "https://www.docs.test/docs/", "https://www.docs.test/docs/"},
		{"https://docs.test/docs/", "https://docs.test/docs/", "https://docs.test/docs/"},
		{"https://docs.test/en/stable/", "https://docs.test/ja/stable/", ""},    // a locale, not a version
		{"https://docs.test/docs/", "https://docs.test/blog/", ""},              // another section
		{"https://docs.test/en/stable/", "https://docs.test/en/6.1/intro/", ""}, // a page, not a version root
		{"https://docs.test/en/stable/", "https://other.test/en/6.1/", ""},      // another host
		{"https://docs.test/", "https://docs.test/en/guide/", ""},               // a locale and a non-version
		{"https://docs.test/v1/api/", "https://docs.test/v2/reference/", ""},    // two segments changed
		{"https://docs.test/docs/", "https://docs.test/docs/next/", "https://docs.test/docs/next/"},
	} {
		requested, _ := parseWebRoot(c.requested)
		served, _ := url.Parse(c.served)
		got, ok := canonicalRoot(requested, served)
		if ok != (c.want != "") || ok && got.String() != c.want {
			t.Errorf("canonicalRoot(%s, %s) = %v %v, want %q", c.requested, c.served, got.u, ok, c.want)
		}
	}
	if _, ok := canonicalRoot(webRoot{}, nil); ok {
		t.Error("nothing served is no root")
	}
}

// django@5.2 and django@4.0: /en/stable/ redirects to /en/6.1/, the root
// the switcher's sibling links are read from. 5.2 is offered; 4.0 is not
// (the fixture lists the supported versions), and the list says what is.
func TestVersionsDjangoSiblings(t *testing.T) {
	w, _ := newFixtureSiteWeb(t, nil, "versions")
	ctx := context.Background()
	requested, _ := parseWebRoot("https://docs.djangoproject.com/en/stable/")
	raw, err := w.fetcher.FetchRaw(ctx, requested.String())
	if err != nil {
		t.Fatal(err)
	}
	served, _ := url.Parse(raw.URL)
	root, ok := canonicalRoot(requested, served)
	if !ok || root.String() != "https://docs.djangoproject.com/en/6.1/" {
		t.Fatalf("canonicalRoot = %s %v", root, ok)
	}
	set := w.siteVersions(ctx, root, testHTMLProfile(t, raw.URL, raw.Body))
	if set.source != versionsSiblings {
		t.Errorf("source = %q", set.source)
	}
	if got, want := set.labels(), []string{"6.1", "dev", "6.0", "5.2", "5.1", "5.0", "4.2"}; !slices.Equal(got, want) {
		t.Errorf("labels = %q, want %q (locale switcher links are not versions)", got, want)
	}
	if !set.versions[0].current {
		t.Error("the root's own version is current")
	}
	v, ok := matchVersion("5.2", set.versions)
	if !ok || v.root != "https://docs.djangoproject.com/en/5.2/" {
		t.Errorf("django@5.2 = %+v %v", v, ok)
	}
	if v, ok := matchVersion("4.0", set.versions); ok {
		t.Errorf("django@4.0 matched %+v", v)
	}
	if v, ok := matchVersion("dev", set.versions); !ok || v.root != "https://docs.djangoproject.com/en/dev/" {
		t.Errorf("django@dev = %+v %v", v, ok)
	}
}

// siteLinks builds profile links from href and text pairs.
func siteLinks(t *testing.T, base string, pairs ...string) []siteLink {
	t.Helper()
	b, _ := url.Parse(base)
	var out []siteLink
	for i := 0; i+1 < len(pairs); i += 2 {
		u, ok := resolveRef(b, pairs[i])
		if !ok {
			t.Fatalf("bad href %q", pairs[i])
		}
		out = append(out, siteLink{url: u, text: pairs[i+1]})
	}
	return out
}

func TestVersionsSiblingLinks(t *testing.T) {
	for _, c := range []struct {
		name, root string
		links      []string // href, text pairs
		meta       map[string]string
		want       []string // labels, current first; nil for none
		roots      map[string]string
	}{
		{
			name:  "express",
			root:  "https://expressjs.com/en/5x/",
			links: []string{"/en/5x/api.html", "API", "/en/4x/api.html", "4.x API", "/en/3x/api.html", "3.x API", "/de/5x/api.html", "Deutsch", "/en/guide/routing.html", "Routing"},
			want:  []string{"5x", "4x", "3x"},
			roots: map[string]string{"4x": "https://expressjs.com/en/4x/"},
		},
		{
			name: "docusaurus",
			root: "https://docusaurus.io/docs/",
			links: []string{"/docs/next", "Canary 🚧", "/docs", "3.10.2", "/docs/3.9.2", "3.9.2", "/docs/2.x", "2.x",
				"/docs/installation", "Installation", "/versions", "All versions"},
			want:  []string{"3.10.2", "next", "3.9.2", "2.x"},
			roots: map[string]string{"2.x": "https://docusaurus.io/docs/2.x/", "3.10.2": "https://docusaurus.io/docs/"},
		},
		{
			name:  "docusaurus version meta",
			root:  "https://docusaurus.io/docs/",
			links: []string{"/docs", "Docs", "/docs/next", "Canary 🚧", "/docs/3.9.2", "3.9.2"},
			meta:  map[string]string{"docusaurus_version": "3.10.2", "docsearch:version": "3.10.2"},
			want:  []string{"3.10.2", "next", "3.9.2"},
		},
		{
			name:  "vite",
			root:  "https://vite.dev/",
			links: []string{"https://v7.vite.dev", "Vite 7 Docs", "https://v6.vite.dev", "Vite 6 Docs", "https://v2.vite.dev", "Vite 2 Docs", "https://github.com/vitejs/vite", "GitHub", "https://chat.vite.dev", "Discord"},
			want:  []string{labelCurrent, "v7", "v6", "v2"},
			roots: map[string]string{"v2": "https://v2.vite.dev/"},
		},
		{
			name:  "an old vite version",
			root:  "https://v5.vite.dev/",
			links: []string{"https://vite.dev/", "Latest", "https://v4.vite.dev", "Vite 4 Docs"},
			want:  []string{labelCurrent, "v5", "v4"},
			roots: map[string]string{labelCurrent: "https://vite.dev/"},
		},
		{
			name:  "one bare integer is not a version",
			root:  "https://docs.test/",
			links: []string{"/2/intro/", "Intro", "/guide/", "Guide"},
		},
		{
			name:  "two bare integers are",
			root:  "https://docs.test/",
			links: []string{"/2/intro/", "v2", "/3/intro/", "v3"},
			want:  []string{labelCurrent, "2", "3"},
		},
		{
			name:  "bare integers at the root's own position",
			root:  "https://docs.python.org/3/",
			links: []string{"/2/library/", "Python 2"},
			want:  []string{"3", "2"},
		},
		{
			name:  "stable beside the newest release is that release",
			root:  "https://docs.djangoproject.com/en/6.1/",
			links: []string{"/en/stable/", "stable", "/en/6.0/", "6.0", "/en/dev/", "dev"},
			want:  []string{"6.1", "6.0", "dev"},
			roots: map[string]string{"stable": "https://docs.djangoproject.com/en/6.1/"},
		},
		{
			name:  "stable beside an older release is not",
			root:  "https://docs.djangoproject.com/en/5.2/",
			links: []string{"/en/stable/", "stable", "/en/6.1/", "6.1"},
			want:  []string{"5.2", "stable", "6.1"},
			roots: map[string]string{"stable": "https://docs.djangoproject.com/en/stable/"},
		},
		{
			name:  "a lone alias is a page, not a version list",
			root:  "https://go.dev/doc/",
			links: []string{"/doc/devel/release", "Release History", "/doc/tutorial/", "Tutorials"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, _ := parseWebRoot(c.root)
			got := siblingVersions(root, &siteProfile{links: siteLinks(t, c.root, c.links...), meta: c.meta})
			labels := versionSet{versions: got}.labels()
			if !slices.Equal(labels, c.want) {
				t.Fatalf("labels = %q, want %q", labels, c.want)
			}
			if len(got) > 0 && !got[0].current {
				t.Error("the first version is current")
			}
			for label, want := range c.roots {
				if v, ok := matchVersion(label, got); !ok || v.root != want {
					t.Errorf("%s root = %q, want %q", label, v.root, want)
				}
			}
		})
	}
	if siblingVersions(webRoot{}, nil) != nil {
		t.Error("no profile, no versions")
	}
}

// The Read the Docs addons API (pytest, recorded): active versions with the
// default (stable) first, labelled by the tag it builds, plus the project
// and its repository.
func TestVersionsRTDAddons(t *testing.T) {
	w, ft := newFixtureSiteWeb(t, nil, "versions")
	root, _ := parseWebRoot("https://docs.pytest.org/en/stable/")
	p := &siteProfile{served: root.u, meta: map[string]string{"readthedocs-project-slug": "pytest", "readthedocs-version-slug": "stable"}}
	set := w.siteVersions(context.Background(), root, p)
	if set.source != versionsRTD || set.project != "pytest" || set.repoURL != "https://github.com/pytest-dev/pytest" {
		t.Errorf("set = %q %q %q", set.source, set.project, set.repoURL)
	}
	if got, want := set.labels(), []string{"9.1.1", "latest", "9.0.x", "8.4.x", "8.3.x", "8.2.x"}; !slices.Equal(got, want) {
		t.Errorf("labels = %q, want %q", got, want)
	}
	if v := set.versions[0]; !v.current || !slices.Contains(v.aliases, "stable") || v.root != "https://docs.pytest.org/en/stable/" {
		t.Errorf("current = %+v", v)
	}
	if v, ok := matchVersion("8.4", set.versions); !ok || v.root != "https://docs.pytest.org/en/8.4.x/" {
		t.Errorf("pytest@8.4 = %+v %v", v, ok)
	}
	for _, c := range ft.Calls() {
		if !strings.Contains(c.URL, "/_/addons/") {
			t.Errorf("unexpected request %s", c)
		}
	}
}

// click's Read the Docs project has only "stable", a branch; its Sphinx
// build (objects.inv) says which release that is, and the site falls
// through to that label.
func TestVersionsRTDWithSphinxLabel(t *testing.T) {
	w, _ := newFixtureSiteWeb(t, nil, "sphinx")
	root, _ := parseWebRoot("https://click.palletsprojects.com/en/stable/")
	page := readFixture(t, "sphinx/click.palletsprojects.com/en/stable/__index__")
	set := w.siteVersions(context.Background(), root, testHTMLProfile(t, root.String(), page))
	if set.source != versionsRTD || set.project != "Click" || set.repoURL != "https://github.com/pallets/click" {
		t.Errorf("set = %q %q %q", set.source, set.project, set.repoURL)
	}
	if len(set.versions) != 1 || set.versions[0].label != "8.5.x" || !slices.Equal(set.versions[0].aliases, []string{"stable"}) || !set.versions[0].current {
		t.Errorf("versions = %+v", set.versions)
	}
	if v, ok := matchVersion("stable", set.versions); !ok || v.root != root.String() {
		t.Errorf("click@stable = %+v %v", v, ok)
	}
	if v, ok := matchVersion("8.5", set.versions); !ok || v.label != "8.5.x" {
		t.Errorf("click@8.5 = %+v %v", v, ok)
	}
}

// mike's versions.json (fastapi-users, recorded) sits beside the version
// directories; the "latest" alias is the current version.
func TestVersionsMike(t *testing.T) {
	w, _ := newFixtureSiteWeb(t, nil, "versions")
	root, _ := parseWebRoot("https://fastapi-users.github.io/fastapi-users/latest/")
	p := &siteProfile{served: root.u, meta: map[string]string{"generator": "mkdocs-1.6.1, mkdocs-material-9.7.6"}, inline: `{"base": ".", "version": {"provider": "mike"}}`}
	set := w.siteVersions(context.Background(), root, p)
	if set.source != versionsMike {
		t.Fatalf("source = %q", set.source)
	}
	labels := set.labels()
	if len(labels) != 13 || labels[0] != "15.0" || labels[1] != "dev" {
		t.Errorf("labels = %q", labels)
	}
	if v, ok := matchVersion("latest", set.versions); !ok || v.label != "15.0" || v.root != "https://fastapi-users.github.io/fastapi-users/15.0/" {
		t.Errorf("latest = %+v %v", v, ok)
	}
	if v, ok := matchVersion("10", set.versions); !ok || v.label != "10.4" {
		t.Errorf("10 = %+v %v", v, ok)
	}

	// The deploy root itself, hinted by the theme, is its own parent.
	top, _ := parseWebRoot("https://fastapi-users.github.io/fastapi-users/")
	vs, err := w.mikeVersions(context.Background(), top)
	if err != nil || len(vs) != 13 || vs[0].root != "https://fastapi-users.github.io/fastapi-users/15.0/" {
		t.Errorf("mike at the deploy root = %v, %v", vs, err)
	}
}

// numpy's pydata switcher JSON (recorded): labels from "version" (numpy
// writes "doc/1.26"), stable as an alias of the preferred 2.5.
func TestVersionsSwitcher(t *testing.T) {
	w, _ := newFixtureSiteWeb(t, nil, "versions")
	root, _ := parseWebRoot("https://numpy.org/doc/stable/")
	p := &siteProfile{served: root.u, inline: "DOCUMENTATION_OPTIONS.pagename = 'index';\nDOCUMENTATION_OPTIONS.theme_switcher_json_url = 'https://numpy.org/doc/_static/versions.json';\nDOCUMENTATION_OPTIONS.theme_switcher_version_match = '2.5';\n"}
	set := w.siteVersions(context.Background(), root, p)
	if set.source != versionsSwitcher {
		t.Fatalf("source = %q", set.source)
	}
	labels := set.labels()
	if len(labels) != 21 || labels[0] != "2.5" || labels[1] != "devdocs" || !slices.Contains(labels, "1.26") {
		t.Errorf("labels = %q", labels)
	}
	if v := set.versions[0]; !v.current || v.root != "https://numpy.org/doc/stable/" || !slices.Equal(v.aliases, []string{"stable"}) {
		t.Errorf("current = %+v", v)
	}
	if v, ok := matchVersion("dev", set.versions); !ok || v.root != "https://numpy.org/devdocs/" {
		t.Errorf("dev = %+v %v", v, ok)
	}
	if v, ok := matchVersion("1.26", set.versions); !ok || v.root != "https://numpy.org/doc/1.26/" {
		t.Errorf("1.26 = %+v %v", v, ok)
	}
}

// When the addons API does not answer, a Read the Docs site's sitemap
// (requests, recorded) still lists its version roots.
func TestVersionsRTDSitemap(t *testing.T) {
	w, _ := newFixtureSiteWeb(t, nil, "versions")
	root, _ := parseWebRoot("https://requests.readthedocs.io/en/latest/")
	p := &siteProfile{served: root.u, meta: map[string]string{"readthedocs-project-slug": "requests", "readthedocs-version-slug": "latest"}}
	set := w.siteVersions(context.Background(), root, p)
	if set.source != versionsSitemap || !slices.Equal(set.labels(), []string{"stable", "latest"}) {
		t.Errorf("set = %q %q", set.source, set.labels())
	}
}

// A Sphinx site with no version list still names its own version and
// project.
func TestVersionsSphinxOnly(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://docs.test/objects.inv": plain("# Sphinx inventory version 2\n# Project: Widget\n# Version: 2.3\n# The remainder of this file is compressed using zlib.\n"),
	}}
	root, _ := parseWebRoot("https://docs.test/")
	p := &siteProfile{served: root.u, scripts: []string{"https://docs.test/_static/documentation_options.js?v=1"}}
	set := NewWeb(f).siteVersions(context.Background(), root, p)
	if set.source != versionsSphinx || set.project != "Widget" || !slices.Equal(set.labels(), []string{"2.3"}) || !set.versions[0].current {
		t.Errorf("set = %+v", set)
	}

	// Without objects.inv, documentation_options.js's VERSION.
	f = &fakeFetcher{raw: map[string]fakeRaw{
		"https://docs.test/_static/documentation_options.js": plain("const DOCUMENTATION_OPTIONS = {VERSION: '2.3.1', BUILDER: 'html'};"),
	}}
	set = NewWeb(f).siteVersions(context.Background(), root, p)
	if set.source != versionsSphinx || set.project != "" || !slices.Equal(set.labels(), []string{"2.3.1"}) {
		t.Errorf("set = %+v", set)
	}
}

// Every source failing, or no profile at all, is a zero set, not an error.
func TestVersionsNeverFail(t *testing.T) {
	root, _ := parseWebRoot("https://docs.test/en/stable/")
	f := &fakeFetcher{fail: context.DeadlineExceeded}
	p := &siteProfile{served: root.u, meta: map[string]string{"readthedocs-project-slug": "x", "generator": "mkdocs-1.6"}, scripts: []string{"https://docs.test/en/stable/_static/documentation_options.js"},
		inline: `DOCUMENTATION_OPTIONS.theme_switcher_json_url = '/_static/versions.json';`}
	if set := NewWeb(f).siteVersions(context.Background(), root, p); set.versions != nil || set.source != "" || set.project != "" {
		t.Errorf("set = %+v", set)
	}
	if set := NewWeb(&fakeFetcher{}).siteVersions(context.Background(), root, nil); set.versions != nil || set.source != "" {
		t.Errorf("nil profile: %+v", set)
	}
	// A plain page with no version anything asks for nothing.
	quiet := &fakeFetcher{}
	if set := NewWeb(quiet).siteVersions(context.Background(), root, &siteProfile{served: root.u}); set.versions != nil {
		t.Errorf("set = %+v", set)
	}
	if calls := quiet.called(); len(calls) != 0 {
		t.Errorf("a page with no version source made requests: %v", calls)
	}
}

// versionSitesWeb is a backend over the sites recorded in testdata/versions;
// any other URL is a 404, as the site would answer.
func versionSitesWeb(t *testing.T) *Web {
	t.Helper()
	ft := testutil.LoadFixtures(t, filepath.Join("testdata", "versions"))
	return NewWeb(&fixtureFetcher{client: &http.Client{Transport: notFoundFallback{ft}}})
}

// describeAt verifies nothing but describes the recorded site at rootURL
// as finish does for a match of spec's project, whose latest release is
// latest: its versions, a version index's current docs, the version asked
// for.
func describeAt(t *testing.T, w *Web, rootURL, spec, latest string, registry bool) (webMatch, error) {
	t.Helper()
	root, err := parseWebRoot(rootURL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withProfileMemo(publicHops(context.Background()))
	prof, err := w.cachedProfile(ctx, root.u)
	if err != nil {
		t.Fatalf("profile %s: %v", root, err)
	}
	if root, err = adoptServed(root, prof.served, registry); err != nil {
		t.Fatalf("adoptServed %s: %v", rootURL, err)
	}
	s := mustSpec(t, spec)
	pkg := pkgInfo{eco: s.eco, name: s.name, version: latest}
	if registry {
		pkg.registryDocs = rootURL
	}
	m := webMatch{root: root, prof: prof, registry: registry, proj: &project{pkgs: []pkgInfo{pkg}}}
	r := &resolution{w: w, spec: s, log: &resolveLog{}}
	err = r.describe(ctx, &m)
	return m, err
}

// A version a site does not list is looked for where its own layout puts
// it: a version host label (18.react.dev, v3.tailwindcss.com), a version
// segment below the docs (nextjs.org/docs/13/), the listed releases'
// style and place (express lists 5x, so 4x is /en/4x/, read through the
// page of the 5x tree the root links to), or the root's own version
// segment (pydantic's /latest/ to /1.10/, whose root has no page, read
// through the page the current docs land on). Recorded live.
func TestPickVersionWhereTheSiteKeepsIt(t *testing.T) {
	cases := []struct{ root, spec, latest, want, label string }{
		{"https://react.dev/", "npm:react@18", "19.2.0", "https://18.react.dev/", "18"},
		{"https://nextjs.org/docs/", "npm:next@13", "16.0.1", "https://nextjs.org/docs/13/", "13"},
		{"https://tailwindcss.com/docs/", "npm:tailwindcss@3", "4.1.16", "https://v3.tailwindcss.com/docs/", "v3"},
		{"https://expressjs.com/", "npm:express@4", "5.1.0", "https://expressjs.com/en/4x/", "4x"},
		{"https://pydantic.dev/docs/validation/latest/", "pypi:pydantic@1.10", "2.14.1", "https://pydantic.dev/docs/validation/1.10/", "1.10"},
	}
	w := versionSitesWeb(t)
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			m, err := describeAt(t, w, tc.root, tc.spec, tc.latest, false)
			if err != nil || m.root.String() != tc.want || m.version != tc.label {
				t.Fatalf("%s at %s = %s version %q, %v; want %s version %q", tc.spec, tc.root, m.root, m.version, err, tc.want, tc.label)
			}
			if !slices.ContainsFunc(m.evidence, func(e evidence) bool {
				return e.kind == evNote && strings.Contains(e.detail, "its docs for it are at "+tc.want)
			}) {
				t.Errorf("evidence %v lacks the note naming %s", m.evidence, tc.want)
			}
		})
	}
	// A version the site does not have is still not found: 17.react.dev
	// and react.dev/17/ do not answer.
	if m, err := describeAt(t, w, "https://react.dev/", "npm:react@17", "19.2.0", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("react@17 = %s, %v; want not found", m.root, err)
	}
	// The latest major is the current docs, without probing.
	if m, err := describeAt(t, w, "https://react.dev/", "npm:react@19", "19.2.0", false); err != nil || m.root.String() != "https://react.dev/" || m.version != "19.2.0" {
		t.Errorf("react@19 = %s %q, %v; want react.dev as 19.2.0", m.root, m.version, err)
	}
}

// The current docs of a site with no version list are taken for a
// version only when it names the latest release: tokio@1 is tokio.rs's
// 1.54.1, tokio@1.38 is not (it shares only the major).
func TestNamesRelease(t *testing.T) {
	cases := []struct {
		want, release string
		names         bool
	}{
		{"1", "1.54.1", true}, {"1.54", "1.54.1", true}, {"1.54.1", "1.54.1", true}, {"v7.x", "7.1.2", true},
		{"1.38", "1.54.1", false}, {"2", "1.54.1", false}, {"1.54.1.4", "1.54.1", false}, {"stable", "1.54.1", false},
	}
	for _, tc := range cases {
		if got := namesRelease(tc.want, tc.release); got != tc.names {
			t.Errorf("namesRelease(%q, %q) = %v, want %v", tc.want, tc.release, got, tc.names)
		}
	}
}

// SQLAlchemy labels its versions 21, 20 and 14, the release without its
// dot: 1.4 is 14 once a label is known to be written so (21 is the latest
// release, 2.1.6). A site whose two-digit labels are not that keeps them
// as releases.
func TestDottedLabels(t *testing.T) {
	vs := []siteVersion{
		{label: "21", root: "https://docs.sqlalchemy.org/en/21/", current: true},
		{label: "20", root: "https://docs.sqlalchemy.org/en/20/"},
		{label: "14", root: "https://docs.sqlalchemy.org/en/14/"},
	}
	for want, root := range map[string]string{"1.4": "https://docs.sqlalchemy.org/en/14/", "2": "https://docs.sqlalchemy.org/en/21/", "2.0": "https://docs.sqlalchemy.org/en/20/"} {
		if _, ok := matchVersion(want, vs); ok && want == "1.4" {
			t.Errorf("1.4 matched the labels as written")
		}
		v, ok := matchVersion(want, dottedLabels(vs, "2.1.6"))
		if !ok || v.root != root {
			t.Errorf("sqlalchemy@%s = %s, %v; want %s", want, v.root, ok, root)
		}
	}
	if dottedLabels(vs, "14.2.0") != nil || dottedLabels(vs, "") != nil {
		t.Errorf("labels not known to be dotless releases were read as such")
	}
}

// docs.rs answers a version whose docs build failed (clap 3.2.25 and
// 3.2.24) with the crate's page: the nearest release that built (3.2.23)
// stands in, noted. Recorded live.
func TestResolveDocsRSFailedBuild(t *testing.T) {
	w := versionSitesWeb(t)
	for _, spec := range []string{"crates:clap@3", "crates:clap@3.2.25"} {
		m, err := describeAt(t, w, "https://docs.rs/clap/latest/clap/", spec, "4.6.7", true)
		if err != nil || m.root.String() != "https://docs.rs/clap/3.2.23/clap/" || m.version != "3.2.23" {
			t.Errorf("%s = %s %q, %v; want docs.rs's 3.2.23", spec, m.root, m.version, err)
			continue
		}
		if !slices.ContainsFunc(m.evidence, func(e evidence) bool {
			return strings.Contains(e.detail, "docs.rs failed to build clap 3.2.25; 3.2.23 is the nearest release")
		}) {
			t.Errorf("%s evidence %v lacks the failed-build note", spec, m.evidence)
		}
	}
}

// A version index is not docs: numpy.org/doc/ links its versions,
// matplotlib.org/ is a landing page linking /stable/. Each stands for the
// stable docs it links below itself. Recorded live.
func TestVersionIndexIsItsStableDocs(t *testing.T) {
	w := versionSitesWeb(t)
	for root, want := range map[string]string{"https://numpy.org/doc/": "https://numpy.org/doc/stable/", "https://matplotlib.org/": "https://matplotlib.org/stable/"} {
		m, err := describeAt(t, w, root, "pypi:x", "", false)
		if err != nil || m.root.String() != want {
			t.Errorf("%s = %s, %v; want %s", root, m.root, err, want)
		}
	}
	root, _ := parseWebRoot("https://expressjs.com/")
	if _, ok := indexChild(root, versionSet{versions: []siteVersion{{label: labelCurrent, root: "https://expressjs.com/", current: true}, {label: "5x", root: "https://expressjs.com/en/5x/"}}}); ok {
		t.Errorf("a root that is the current docs, beside an older tree, is no index")
	}
}

// Version switchers drawn as a <select>: eslint.org's options carry their
// docs root in data-url (read as links, so sibling versions), fastify.dev's
// carry only the version segment (selectVersions puts it where the root's
// own version is). Trimmed from the live pages.
func TestPickVersionFromSelectMenu(t *testing.T) {
	cases := []struct{ root, spec, latest, want, label string }{
		{"https://eslint.org/docs/latest/", "npm:eslint@8", "10.14.0", "https://eslint.org/docs/v8.x/", "v8.x"},
		{"https://fastify.dev/docs/latest/", "npm:fastify@4", "5.12.8", "https://fastify.dev/docs/v4.29.x/", "v4.29.x"},
		// /docs/ is a redirect stub (a meta refresh and a link to its target).
		{"https://fastify.dev/docs/", "npm:fastify@4", "5.12.8", "https://fastify.dev/docs/v4.29.x/", "v4.29.x"},
	}
	w := versionSitesWeb(t)
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			m, err := describeAt(t, w, tc.root, tc.spec, tc.latest, false)
			if err != nil || m.root.String() != tc.want || m.version != tc.label {
				t.Fatalf("%s at %s = %s version %q (versions %v), %v; want %s version %q", tc.spec, tc.root, m.root, m.version, m.versions, err, tc.want, tc.label)
			}
		})
	}
	// A version the menu does not offer is not found.
	if m, err := describeAt(t, w, "https://fastify.dev/docs/latest/", "npm:fastify@2", "5.12.8", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("fastify@2 = %s, %v; want not found", m.root, err)
	}
}

func TestSelectVersionsNeedsVersionValues(t *testing.T) {
	root, _ := parseWebRoot("https://site.test/docs/")
	lang := &siteProfile{selects: [][]selectOption{{{value: "en", text: "English", selected: true}, {value: "fr", text: "Français"}}}}
	if got := selectVersions(root, lang); got != nil {
		t.Errorf("a language menu read as versions: %+v", got)
	}
	vs := selectVersions(root, &siteProfile{selects: [][]selectOption{{{value: "latest", text: "latest (v5.1.0)", selected: true}, {value: "v4.x", text: "v4.9.2"}}}})
	if len(vs) != 2 || vs[0].root != "https://site.test/docs/" || vs[0].label != "v5.1.0" || vs[1].root != "https://site.test/docs/v4.x/" {
		t.Errorf("selectVersions = %+v", vs)
	}
}

// A pydata switcher whose JSON lives on another host (scipy keeps it with
// its development docs on github.io) still lists the root host's versions;
// the other host's own entry is no version of this root.
func TestVersionsSwitcherOnAnotherHost(t *testing.T) {
	w, _ := newFixtureSiteWeb(t, nil, "versions")
	root, _ := parseWebRoot("https://docs.scipy.org/doc/scipy/")
	p := &siteProfile{served: root.u, inline: "DOCUMENTATION_OPTIONS.theme_switcher_json_url = 'https://scipy.github.io/devdocs/_static/version_switcher.json';\nDOCUMENTATION_OPTIONS.theme_switcher_version_match = '1.18.0';\n"}
	set := w.siteVersions(context.Background(), root, p)
	if set.source != versionsSwitcher {
		t.Fatalf("source = %q", set.source)
	}
	if got, want := set.labels(), []string{"1.18.0", "1.17.0", "1.10.1"}; !slices.Equal(got, want) {
		t.Errorf("labels = %q, want %q", got, want)
	}
	if v, ok := matchVersion("1.10", set.versions); !ok || v.root != "https://docs.scipy.org/doc/scipy-1.10.1/" {
		t.Errorf("1.10 = %+v %v", v, ok)
	}
	// A switcher URL on a private address is never fetched.
	p.inline = "DOCUMENTATION_OPTIONS.theme_switcher_json_url = 'http://127.0.0.1/versions.json';"
	if set := w.siteVersions(context.Background(), root, p); set.source == versionsSwitcher {
		t.Errorf("a private switcher URL was read: %+v", set)
	}
}

// A <select>'s options that carry a URL are links (with the option's
// text); its menu is kept either way, linked or not.
func TestParseProfileSelectMenus(t *testing.T) {
	u, _ := url.Parse("https://eslint.org/docs/latest/")
	p := parseProfile([]byte(`<html><body><select id="v"><option value="10.14.0" data-url="/docs/latest/" selected>v10.14.0</option><option value="8.57.1" data-url="/docs/v8.x/">v8.57.1</option></select>`+
		`<select aria-label="Theme"><option value="light">Light</option><option value="dark">Dark</option></select></body></html>`), u, u)
	if len(p.selects) != 2 || !p.selects[0][0].linked || !p.selects[0][0].selected || p.selects[1][0].linked {
		t.Errorf("selects = %+v", p.selects)
	}
	if !slices.ContainsFunc(p.links, func(l siteLink) bool { return l.url.String() == "https://eslint.org/docs/v8.x/" && l.text == "v8.57.1" }) {
		t.Errorf("option URLs were not read as links: %+v", p.links)
	}
	if slices.ContainsFunc(p.links, func(l siteLink) bool { return strings.Contains(l.url.Path, "dark") }) {
		t.Errorf("a bare option value was read as a link: %+v", p.links)
	}
}
