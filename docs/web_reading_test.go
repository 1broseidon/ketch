package docs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// readingWorld is a keyless world of invented libraries for decision-15's
// rules: a site library's package names its docs site, which links back
// to the repository the package names (two independent edges); a crate's
// docs are on docs.rs only; a readme package names only its GitHub page.
type readingWorld struct {
	f    *fakeFetcher
	pkgs *fakePkgs
	pop  *fakePop
}

func newReadingWorld() *readingWorld {
	return &readingWorld{
		f:    &fakeFetcher{raw: map[string]fakeRaw{}},
		pkgs: &fakePkgs{pkgs: map[string]pkgInfo{}, errs: map[string]error{}},
		pop:  &fakePop{weekly_: map[string]int64{}, errs: map[string]error{}},
	}
}

// site adds eco's name with its own docs site and weekly downloads.
func (rw *readingWorld) site(eco ecosystem, name string, weekly int64) string {
	root := "https://docs." + name + "-" + string(eco) + ".test/"
	gh := "https://github.com/" + name + "-" + string(eco) + "/" + name
	rw.f.raw[root] = html(`<html><head><title>` + name + ` documentation</title><meta name="generator" content="Docusaurus v3"></head><body>
<a href="/guide">Guide</a><a href="` + gh + `">GitHub</a></body></html>`)
	rw.pkgs.pkgs[string(eco)+":"+name] = pkgInfo{eco: eco, name: name, docsURL: root, repoURL: gh, repo: repo(name+"-"+string(eco), name)}
	rw.pop.weekly_[string(eco)+":"+name] = weekly
	return root
}

// crate adds a crate whose only docs are docs.rs's.
func (rw *readingWorld) crate(name string, weekly int64) string {
	root := "https://docs.rs/" + name + "/latest/" + name + "/"
	rw.f.raw[root] = html(`<html><head><title>` + name + ` - Rust</title><meta name="generator" content="rustdoc"></head><body><a href="all.html">All</a></body></html>`)
	rw.pkgs.pkgs["crates:"+name] = pkgInfo{eco: ecoCrates, name: name, registryDocs: root, downloads: weekly * cratesRecentWeeks}
	rw.pop.weekly_["crates:"+name] = weekly
	return root
}

// readme adds eco's name whose metadata names only its GitHub page.
func (rw *readingWorld) readme(eco ecosystem, name string, weekly int64) {
	gh := "https://github.com/" + name + "-readme/" + name
	rw.pkgs.pkgs[string(eco)+":"+name] = pkgInfo{eco: eco, name: name, homepage: gh, repoURL: gh, repo: repo(name+"-readme", name)}
	rw.pop.weekly_[string(eco)+":"+name] = weekly
}

func (rw *readingWorld) web() *Web {
	w, _ := keylessWeb(rw.f, rw.pkgs, nil, rw.pop)
	return w
}

// ambiguousOver fails unless err is an ambiguity listing every id.
func ambiguousOver(t *testing.T, what string, err error, ids ...string) {
	t.Helper()
	var amb *AmbiguousLibraryError
	if !errors.As(err, &amb) {
		t.Fatalf("%s = %v; want an ambiguity", what, err)
	}
	var got []string
	for _, m := range amb.Matches {
		got = append(got, m.ID)
	}
	for _, id := range ids {
		if !slices.Contains(got, id) {
			t.Errorf("%s: ambiguity lists %v, want %s among them", what, got, id)
		}
	}
}

// A namesake that shows no docs effort of its own (only a GitHub page, or
// only docs a package index hosts) blocks a pick only when at least as
// popular: npm's popular readme-only herald keeps docs.rs's herald from
// answering for the name; a few downloads do not. A site of its own that
// did not verify still needs the 50x margin.
func TestKeylessWeakRivals(t *testing.T) {
	for _, tc := range []struct {
		npm  int64
		want bool // docs.rs's herald answers
	}{{30_000_000, false}, {15, true}} {
		rw := newReadingWorld()
		root := rw.crate("herald", 20)
		rw.readme(ecoNPM, "herald", tc.npm)
		m, err := rw.web().resolveOne(context.Background(), mustSpec(t, "herald"), false)
		switch {
		case tc.want && (err != nil || m.root.String() != root):
			t.Errorf("npm herald at %d: resolveOne = %s, %v; want %s", tc.npm, m.root, err, root)
		case !tc.want:
			ambiguousOver(t, "resolveOne(herald)", err, "npm:herald")
		}
	}
	// Beside a pick with its own site, a readme package blocks only when
	// at least as popular.
	for _, tc := range []struct {
		npm  int64
		want bool
	}{{100_000, true}, {5_000_000, false}} {
		rw := newReadingWorld()
		root := rw.site(ecoPyPI, "arbor", 4_000_000)
		rw.readme(ecoNPM, "arbor", tc.npm)
		m, err := rw.web().resolveOne(context.Background(), mustSpec(t, "arbor"), false)
		switch {
		case tc.want && (err != nil || m.root.String() != root):
			t.Errorf("npm arbor at %d: resolveOne = %s, %v; want %s", tc.npm, m.root, err, root)
		case !tc.want:
			ambiguousOver(t, "resolveOne(arbor)", err, "npm:arbor")
		}
	}
}

// A popular library with a site of its own is not refused for a namesake
// whose only docs a package index hosts, unless the namesake is at least
// as popular (decision-15; MANUAL: such a package does not stand in a
// popular library's way).
func TestKeylessIndexOnlyNamesake(t *testing.T) {
	for _, tc := range []struct {
		crate int64
		want  bool
	}{{300_000, true}, {5_000_000, false}} {
		rw := newReadingWorld()
		root := rw.site(ecoPyPI, "arbor", 4_000_000)
		crate := rw.crate("arbor", tc.crate)
		m, err := rw.web().resolveOne(context.Background(), mustSpec(t, "arbor"), false)
		switch {
		case tc.want && (err != nil || m.root.String() != root):
			t.Errorf("crate at %d: resolveOne = %s, %v; want %s", tc.crate, m.root, err, root)
		case !tc.want:
			ambiguousOver(t, "resolveOne(arbor)", err, root, crate)
		}
	}
}

// A PyPI package that names no docs URL but publishes its changelog inside
// a versioned docs tree names that tree's root: with the site's link to
// the repository, two edges. A changelog elsewhere names no root.
func TestKeylessChangelogPageNamesRoot(t *testing.T) {
	for _, tc := range []struct {
		page string
		want bool
	}{
		{"https://sprocket.readthedocs.io/en/latest/changelog.html", true},
		{"https://sprocket.example/changes", false},
		{"https://github.com/sprocket-dev/sprocket/blob/main/CHANGELOG.md", false},
	} {
		rw := newReadingWorld()
		gh := "https://github.com/sprocket-dev/sprocket"
		rw.f.raw["https://sprocket.readthedocs.io/en/latest/"] = html(`<html><head><title>sprocket documentation</title><meta name="generator" content="Sphinx 7"></head><body>
<a href="quickstart.html">Quickstart</a><a href="` + gh + `">Source</a></body></html>`)
		rw.pkgs.pkgs["pypi:sprocket"] = pkgInfo{eco: ecoPyPI, name: "sprocket", homepage: gh, repoURL: gh, repo: repo("sprocket-dev", "sprocket"), pageURL: tc.page}
		rw.pop.weekly_["pypi:sprocket"] = 2_000_000
		m, err := rw.web().resolveOne(context.Background(), mustSpec(t, "sprocket"), false)
		switch got := err == nil && m.root.String() == "https://sprocket.readthedocs.io/en/latest/"; {
		case got != tc.want:
			t.Errorf("changelog %s: resolveOne = %s, %v; want verified %v", tc.page, m.root, err, tc.want)
		case got && !slices.ContainsFunc(m.libraryMatch().Evidence, func(e string) bool { return strings.Contains(e, "lists a page under it") }):
			t.Errorf("changelog %s: evidence %v lacks the page", tc.page, m.libraryMatch().Evidence)
		}
	}
}
