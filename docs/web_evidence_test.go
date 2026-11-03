package docs

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/search"
)

// Round 3B (task-24): keyless evidence a project's owners publish beyond
// its metadata URLs, and the picks that answered from the wrong project.
// Every library here is invented.

// docsShell is a docs framework page whose HTML carries extra links.
func docsShell(title, extra string) fakeRaw {
	return html(`<html><head><title>` + title + `</title><meta name="generator" content="Docusaurus v3"></head><body><a href="/guide">Guide</a>` + extra + `</body></html>`)
}

// A package whose metadata names only its GitHub page has a docs site a
// web search finds, which links back to the repository: one edge. The
// package's own README linking to the site is the second (the owner's
// word, published with the package), whether the registry sends it with
// the metadata (PyPI's description) or npm's published README.md is read.
func TestKeylessReadmeEdge(t *testing.T) {
	gh := "https://github.com/mosaic-org/mosaic"
	sites := func() *fakeSites {
		return &fakeSites{hits: []search.Result{{URL: "https://mosaic.example.dev/guide/intro"}}}
	}
	page := docsShell("Mosaic", `<a href="`+gh+`">GitHub</a>`)

	// No README: one edge, refused.
	f := &fakeFetcher{raw: map[string]fakeRaw{"https://mosaic.example.dev/": page}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"npm:mosaic": {eco: ecoNPM, name: "mosaic", version: "2.1.0", homepage: gh, repoURL: gh, repo: repo("mosaic-org", "mosaic")}}}
	w, _ := keylessWeb(f, pkgs, sites(), nil)
	log := &resolveLog{}
	if _, err := w.resolveLogged(context.Background(), mustSpec(t, "npm:mosaic"), 0, log); err == nil {
		t.Fatal("no README: resolved, want refused")
	}
	assertReasons(t, log, "https://mosaic.example.dev/ has 1 of the 2 independent edges")

	// npm: the package's published README links to the site.
	f = &fakeFetcher{raw: map[string]fakeRaw{
		"https://mosaic.example.dev/":                         page,
		"https://cdn.jsdelivr.net/npm/mosaic@2.1.0/README.md": md("# Mosaic\n\n![logo](https://mosaic.example.dev/logo.svg)\n\nRead the [docs](https://mosaic.example.dev/guide/intro).\n"),
	}}
	w, _ = keylessWeb(f, pkgs, sites(), nil)
	matches, err := w.resolve(context.Background(), mustSpec(t, "npm:mosaic"), 0)
	if err != nil || len(matches) != 1 || matches[0].root.String() != "https://mosaic.example.dev/" {
		t.Fatalf("npm README: resolve = %v, %v", matchIDs(matches), err)
	}
	if ev := matches[0].libraryMatch().Evidence; !slices.Contains(ev, "the npm mosaic README links to https://mosaic.example.dev/guide/intro") {
		t.Errorf("evidence = %v", ev)
	}
	if n := strings.Count(strings.Join(f.called(), " "), "cdn.jsdelivr.net"); n != 1 {
		t.Errorf("README fetched %d times, want once per resolution", n)
	}

	// PyPI: the description PyPI sends with the metadata; nothing fetched.
	f = &fakeFetcher{raw: map[string]fakeRaw{"https://mosaic.example.dev/": page}}
	pkgs = &fakePkgs{pkgs: map[string]pkgInfo{"pypi:mosaic": {eco: ecoPyPI, name: "mosaic", homepage: gh, repoURL: gh, repo: repo("mosaic-org", "mosaic"),
		readme: "Mosaic\n======\n\n`Documentation <https://mosaic.example.dev/>`_\n"}}}
	w, _ = keylessWeb(f, pkgs, sites(), nil)
	if matches, err := w.resolve(context.Background(), mustSpec(t, "pypi:mosaic"), 0); err != nil || len(matches) != 1 {
		t.Fatalf("PyPI README: resolve = %v, %v", matchIDs(matches), err)
	}
}

// A README links to its dependencies' docs too. Such a site does not link
// back to the project's repository, so the README's link stays one edge
// and the dependency's docs are never the project's.
func TestKeylessReadmeDependencyIsNotDocs(t *testing.T) {
	gh := "https://github.com/tessera-org/tessera"
	f := &fakeFetcher{raw: map[string]fakeRaw{
		// The dependency's site, on its own domain, linking to its own repository.
		"https://docs.grout.example.io/": docsShell("Grout", `<a href="https://github.com/grout-org/grout">GitHub</a>`),
	}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"pypi:tessera": {eco: ecoPyPI, name: "tessera", homepage: gh, repoURL: gh, repo: repo("tessera-org", "tessera"),
		readme: "Built on [Grout](https://docs.grout.example.io/) and [its API](https://docs.grout.example.io/api/).\n"}}}
	sites := &fakeSites{hits: []search.Result{{URL: "https://docs.grout.example.io/intro"}}}
	w, _ := keylessWeb(f, pkgs, sites, nil)
	log := &resolveLog{}
	if matches, err := w.resolveLogged(context.Background(), mustSpec(t, "pypi:tessera"), 0, log); err == nil {
		t.Fatalf("resolved %v from a dependency's docs", matchIDs(matches))
	}
	assertReasons(t, log, "https://docs.grout.example.io/ has 1 of the 2 independent edges")
}

// Which README links name a root: the root, a page in it, the site or
// docs tree it sits in; not an image, not another host, not the whole
// site when the README points into its docs tree, and not a host serving
// several projects' docs when the link is into this project's own tree.
func TestReadmeNames(t *testing.T) {
	names := []string{"loom"}
	for _, tc := range []struct {
		root  string
		links []string
		want  bool
	}{
		{"https://loom.example.dev/", []string{"https://loom.example.dev"}, true},
		{"https://loom.example.dev/", []string{"https://www.loom.example.dev/guide/start"}, true},
		{"https://loom.example.dev/docs/latest/", []string{"https://loom.example.dev/docs"}, true},
		{"https://loom.example.dev/docs/", []string{"https://loom.example.dev/docs/index.html"}, true},
		{"https://loom.example.dev/", []string{"https://loom.example.dev/logo.svg"}, false},
		{"https://loom.example.dev/", []string{"https://other.example.dev/"}, false},
		{"https://loom.example.dev/", []string{"https://loom.example.dev", "https://loom.example.dev/docs/intro"}, false},
		{"https://docs.loom.example.dev/", []string{"https://docs.loom.example.dev/docs/intro"}, true},
		{"https://hub.example.org/docs/", []string{"https://hub.example.org/docs/loom/index"}, false},
		{"https://hub.example.org/docs/loom/", []string{"https://hub.example.org/docs/loom/index"}, true},
		{"https://hub.example.org/docs/loom/", []string{"https://hub.example.org/docs/other/index"}, false},
	} {
		root, err := parseWebRoot(tc.root)
		if err != nil {
			t.Fatal(err)
		}
		if _, got := readmeNames(tc.links, root, names); got != tc.want {
			t.Errorf("readmeNames(%v, %s) = %v, want %v", tc.links, tc.root, got, tc.want)
		}
	}
	if got := readmeLinks("see <https://a.example/x>, [b](https://b.example/y). `c <https://c.example/z>`_ and https://a.example/x again."); !slices.Equal(got, []string{"https://a.example/x", "https://b.example/y", "https://c.example/z"}) {
		t.Errorf("readmeLinks = %q", got)
	}
}

// A search hit inside one library's tree on a host documenting several
// stands for that tree, not for every library under /docs/.
func TestSearchRootForNamedTree(t *testing.T) {
	for _, tc := range []struct{ hit, name, want string }{
		{"https://hub.example.org/docs/loom/main/en/index", "loom", "https://hub.example.org/docs/loom/"},
		{"https://hub.example.org/docs/loom", "loom", "https://hub.example.org/docs/loom/"},
		{"https://hub.example.org/docs/other/index", "loom", "https://hub.example.org/docs/"},
		{"https://hub.example.org/docs/loom/index", "", "https://hub.example.org/docs/"},
	} {
		u, _ := url.Parse(tc.hit)
		if got := searchRootFor(u, tc.name).String(); got != tc.want {
			t.Errorf("searchRootFor(%s, %q) = %s, want %s", tc.hit, tc.name, got, tc.want)
		}
	}
}

// A docs subdomain whose page is a shell with no links: its domain's home
// page, the homepage the package names, links to the repository, and
// whoever holds the domain holds the subdomain.
func TestKeylessDomainHomeLinks(t *testing.T) {
	gh := "https://github.com/beacon-org/beacon"
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://beaconlib.com/":      html(`<html><head><title>Beacon</title></head><body><a href="https://docs.beaconlib.com/">Docs</a><a href="` + gh + `">GitHub</a></body></html>`),
		"https://docs.beaconlib.com/": html(`<html><head><title>Beacon docs</title><meta name="generator" content="Docusaurus v3"></head><body><div id="root"></div></body></html>`),
	}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"npm:beacon": {eco: ecoNPM, name: "beacon", homepage: "https://beaconlib.com", repoURL: gh, repo: repo("beacon-org", "beacon")}}}
	w, _ := keylessWeb(f, pkgs, nil, nil)
	matches, err := w.resolve(context.Background(), mustSpec(t, "npm:beacon"), 0)
	if err != nil || len(matches) != 1 || matches[0].root.String() != "https://docs.beaconlib.com/" {
		t.Fatalf("resolve = %v, %v", matchIDs(matches), err)
	}
	if ev := matches[0].libraryMatch().Evidence; !slices.Contains(ev, "the home page of its domain beaconlib.com links to github.com/beacon-org/beacon") {
		t.Errorf("evidence = %v", ev)
	}
}

// A site that still links to its repository's old address: GitHub's
// address for the old repository lands on the one the package names, so
// the link is the project's own, not a same-named stranger's.
func TestKeylessMovedRepository(t *testing.T) {
	site := docsShell("Lattice", `<a href="https://github.com/olduser/lattice">GitHub</a><a href="https://www.npmjs.com/package/lattice">npm</a>`)
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"npm:lattice": {eco: ecoNPM, name: "lattice", homepage: "https://lattice.example.dev", repoURL: "https://github.com/lattice-org/lattice", repo: repo("lattice-org", "lattice")}}}

	f := &fakeFetcher{raw: map[string]fakeRaw{"https://lattice.example.dev/": site}}
	w, _ := keylessWeb(f, pkgs, nil, nil)
	log := &resolveLog{}
	if _, err := w.resolveLogged(context.Background(), mustSpec(t, "npm:lattice"), 0, log); err == nil {
		t.Fatal("unmoved: resolved, want the stranger's link to reject the root")
	}
	assertReasons(t, log, "links to github.com/olduser/lattice, not lattice-org/lattice")

	f = &fakeFetcher{
		raw:       map[string]fakeRaw{"https://lattice.example.dev/": site, "https://github.com/lattice-org/lattice": html(`<html></html>`)},
		redirects: map[string]string{"https://github.com/olduser/lattice": "https://github.com/lattice-org/lattice"},
	}
	w, _ = keylessWeb(f, pkgs, nil, nil)
	matches, err := w.resolve(context.Background(), mustSpec(t, "npm:lattice"), 0)
	if err != nil || len(matches) != 1 {
		t.Fatalf("moved: resolve = %v, %v", matchIDs(matches), err)
	}
	if ev := matches[0].libraryMatch().Evidence; !slices.Contains(ev, "links to github.com/olduser/lattice") {
		t.Errorf("evidence = %v", ev)
	}
}

// An old project hosting its own docs off GitHub: the homepage links to
// its docs below it (one edge), and another URL the metadata lists on the
// same domain makes two of the owner's URLs there (the DNS edge).
func TestKeylessProjectURLsWitnessDomain(t *testing.T) {
	home := "https://www.retro.example.com/software/Retro/"
	f := &fakeFetcher{raw: map[string]fakeRaw{
		home:          html(`<html><head><title>Retro</title></head><body><a href="doc/">Documentation</a></body></html>`),
		home + "doc/": html(`<html><head><title>Retro documentation</title><script src="_static/documentation_options.js"></script></head><body><a href="intro.html">Intro</a></body></html>`),
	}}
	pkg := pkgInfo{eco: ecoPyPI, name: "retro", homepage: home}
	w, _ := keylessWeb(f, &fakePkgs{pkgs: map[string]pkgInfo{"pypi:retro": pkg}}, nil, nil)
	if _, err := w.resolve(context.Background(), mustSpec(t, "pypi:retro"), 0); err == nil {
		t.Fatal("homepage alone: resolved, want one edge short")
	}
	pkg.moreURLs = home + "download/"
	w, _ = keylessWeb(f, &fakePkgs{pkgs: map[string]pkgInfo{"pypi:retro": pkg}}, nil, nil)
	matches, err := w.resolve(context.Background(), mustSpec(t, "pypi:retro"), 0)
	if err != nil || len(matches) != 1 || matches[0].root.String() != home+"doc/" {
		t.Fatalf("with a Download URL: resolve = %v, %v", matchIDs(matches), err)
	}
}

// A homepage links to the docs of every major the owner publishes; the
// package's documentation URL names its own. Evidence a search hit adds to
// the homepage's other link does not lift it past the documentation URL.
func TestPickRootsPrefersNamedDocs(t *testing.T) {
	old := webMatch{root: mustRoot(t, "https://ferro.example.org/docs/"), evidence: []evidence{
		{kind: evNavChain, detail: "homepage links to it"}, {kind: evRelatedDomain, detail: "domain"},
		{kind: evLinksRepo, detail: "its home page links to the repository"}, {kind: evSiteNames, detail: "title"},
		{kind: evLinksOwner, detail: "links to another repository of its owner"},
	}}
	cur := webMatch{root: mustRoot(t, "https://ferro.example.org/ferro3/docs/"), evidence: []evidence{
		{kind: evPkgDocs, detail: "listed as Documentation"}, {kind: evRelatedDomain, detail: "domain"}, {kind: evSiteNames, detail: "title"},
	}}
	old.trust, _ = score(old.evidence)
	cur.trust, _ = score(cur.evidence)
	if old.trust <= cur.trust {
		t.Fatalf("test setup: trust %.1f vs %.1f", old.trust, cur.trust)
	}
	if got := pickRoots([]webMatch{old, cur}); got[0].root.String() != cur.root.String() {
		t.Errorf("pickRoots picked %s, want the documentation URL's %s", got[0].root, cur.root)
	}
}

// A deprecated npm package of exactly the name is not verified, but it is
// a rival: in wide use, it shows the name means something popular, and a
// namesake whose only docs docs.rs hosts is refused, not answered. A
// deprecated package nobody uses does not stand in its way.
func TestKeylessDeprecatedNamesakeBlocks(t *testing.T) {
	for _, tc := range []struct {
		npm  int64
		want bool // docs.rs's prism answers
	}{{600_000, false}, {3, true}} {
		rw := newReadingWorld()
		root := rw.crate("prism", 12)
		rw.pkgs.pkgs["npm:prism"] = pkgInfo{eco: ecoNPM, name: "prism", homepage: "https://prism.example.org", deprecated: true}
		rw.pop.weekly_["npm:prism"] = tc.npm
		m, err := rw.web().resolveOne(context.Background(), mustSpec(t, "prism"), false)
		switch {
		case tc.want && (err != nil || m.root.String() != root):
			t.Errorf("npm prism at %d: resolveOne = %s, %v; want %s", tc.npm, m.root, err, root)
		case !tc.want:
			var amb *AmbiguousLibraryError
			if !errors.As(err, &amb) {
				t.Errorf("npm prism at %d: resolveOne = %s, %v; want an ambiguity", tc.npm, m.root, err)
			}
		}
	}
}

func mustRoot(t *testing.T, s string) webRoot {
	t.Helper()
	r, err := parseWebRoot(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The other way round: the metadata still names the old address, which
// GitHub moves to the one the site links.
func TestKeylessMetadataNamesMovedRepository(t *testing.T) {
	site := docsShell("Lattice", `<a href="https://github.com/lattice-org/lattice">GitHub</a>`)
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"npm:lattice": {eco: ecoNPM, name: "lattice", homepage: "https://lattice.example.dev", repoURL: "https://github.com/olduser/lattice", repo: repo("olduser", "lattice")}}}
	f := &fakeFetcher{
		raw:       map[string]fakeRaw{"https://lattice.example.dev/": site, "https://github.com/lattice-org/lattice": html(`<html></html>`)},
		redirects: map[string]string{"https://github.com/olduser/lattice": "https://github.com/lattice-org/lattice"},
	}
	w, _ := keylessWeb(f, pkgs, nil, nil)
	if matches, err := w.resolve(context.Background(), mustSpec(t, "npm:lattice"), 0); err != nil || len(matches) != 1 {
		t.Fatalf("resolve = %v, %v", matchIDs(matches), err)
	}
}

// The homepage links both the current docs and an archive of old majors
// that still links the repository's old address. Both are the project's;
// the archive, left behind by the move, comes second whatever its trust.
func TestKeylessArchiveAtOldAddressComesSecond(t *testing.T) {
	cur := "https://github.com/lattice-org/lattice"
	f := &fakeFetcher{
		raw: map[string]fakeRaw{
			"https://lattice.example.dev/":         html(`<html><head><title>Lattice</title></head><body><a href="/docs/intro">Docs</a><a href="https://archive.lattice.example.dev/">Archive docs</a><a href="` + cur + `">GitHub</a></body></html>`),
			"https://lattice.example.dev/docs/":    docsShell("Lattice docs", `<a href="`+cur+`">GitHub</a>`),
			"https://archive.lattice.example.dev/": docsShell("Lattice docs", `<a href="https://github.com/olduser/lattice">GitHub</a><a href="https://www.npmjs.com/package/lattice">npm</a>`),
			cur:                                    html(`<html></html>`),
		},
		redirects: map[string]string{"https://github.com/olduser/lattice": cur},
	}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"npm:lattice": {eco: ecoNPM, name: "lattice", homepage: "https://lattice.example.dev", repoURL: cur, repo: repo("lattice-org", "lattice")}}}
	w, _ := keylessWeb(f, pkgs, nil, nil)
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "npm:lattice"), 0, log)
	if err != nil || len(matches) == 0 || matches[0].root.String() != "https://lattice.example.dev/docs/" {
		t.Fatalf("resolve = %v, %v (rejected %v); want the current docs first", matchIDs(matches), err, log.rejected)
	}
}

// A package whose metadata lists only its repository: its README's links
// to docs named after it are put forward, and verify by the README's link
// and the site's link back to the package. A dependency's docs the README
// also links are tried and fail.
func TestKeylessReadmeCandidates(t *testing.T) {
	gh := "https://github.com/coach-labs/coachlib"
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://python.usecoachlib.example/": docsShell("Coachlib", `<a href="https://pypi.org/project/coachlib/">PyPI</a>`),
		"https://docs.helper.example/":        docsShell("Helper", `<a href="https://github.com/helper-org/helper">GitHub</a>`),
	}}
	pkgs := &fakePkgs{pkgs: map[string]pkgInfo{"pypi:coachlib": {eco: ecoPyPI, name: "coachlib", repoURL: gh, repo: repo("coach-labs", "coachlib"),
		readme: "# coachlib\n\n[Docs](https://python.usecoachlib.example/) · [Concepts](https://python.usecoachlib.example/concepts/) · built on [helper](https://docs.helper.example/)\n"}}}
	w, _ := keylessWeb(f, pkgs, nil, nil)
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "pypi:coachlib"), 0, log)
	if err != nil || len(matches) != 1 || matches[0].root.String() != "https://python.usecoachlib.example/" {
		t.Fatalf("resolve = %v, %v (rejected %v)", matchIDs(matches), err, log.rejected)
	}
}
