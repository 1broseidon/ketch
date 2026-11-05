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

// query resolves a bare query: the root picked, or the error.
func (rw *readingWorld) query(t *testing.T, q string) (string, error) {
	t.Helper()
	m, _, err := rw.web().resolveQuery(context.Background(), q, splitLibraryQuery(q))
	if err != nil {
		return "", err
	}
	return m.root.String(), nil
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

// Generic words in a question are never the library: the verb after "how
// do I", an imperative's first word, a language, an adjective after "is
// the". The library is the word the syntax names, though npm here has
// packages called parse and set with docs sites of their own.
func TestBareQueryVerbsAreNoLibrary(t *testing.T) {
	rw := newReadingWorld()
	tempora := rw.site(ecoNPM, "tempora", 20_000_000)
	lumber := rw.site(ecoPyPI, "lumberjack", 3_000_000)
	rw.site(ecoNPM, "parse", 180_000)
	rw.site(ecoNPM, "set", 50_000)
	rw.crate("best", 10)
	for _, tc := range []struct{ q, want string }{
		{"parse a date string with tempora", tempora},
		{"how do I parse dates in tempora", tempora},
		{"how does tempora handle timezones", tempora},
		{"set up file logging in lumberjack", lumber},
		{"How can I set the log level using lumberjack?", lumber},
	} {
		if got, err := rw.query(t, tc.q); err != nil || got != tc.want {
			t.Errorf("%q = %s, %v; want %s", tc.q, got, err, tc.want)
		}
	}
	// No library named: a validation error, never a pick or an upstream
	// failure.
	for _, q := range []string{"what is the best way to cache results in python", "how do I parse a config file", "best way to cache results"} {
		if got, err := rw.query(t, q); !errors.Is(err, ErrLibraryRequired) {
			t.Errorf("%q = %s, %v; want no library identified", q, got, err)
		}
	}
}

// A common word names a library only when that library is in common use:
// the namesake crate of "best" (ten downloads a week) is no answer, while
// a popular library called by a common word ("rich" here, invented) is.
// Named outright with --library, the crate still resolves.
func TestBareQueryCommonWordBar(t *testing.T) {
	rw := newReadingWorld()
	rw.crate("best", 10)
	richRoot := rw.site(ecoPyPI, "rich", 40_000_000)
	if got, err := rw.query(t, "best practices guide"); !errors.Is(err, ErrLibraryRequired) || !strings.Contains(err.Error(), "common word") {
		t.Errorf("best practices guide = %s, %v; want the common-word bar", got, err)
	}
	if got, err := rw.query(t, "rich progress bars"); err != nil || got != richRoot {
		t.Errorf("rich progress bars = %s, %v; want %s", got, err, richRoot)
	}
	if m, err := rw.web().resolveOne(context.Background(), mustSpec(t, "best"), false); err != nil || !strings.HasPrefix(m.root.String(), "https://docs.rs/best/") {
		t.Errorf("--library best = %s, %v; want docs.rs's best", m.root, err)
	}
	// Junk namesakes in three registries make the word ambiguous: a
	// refusal still, never a pick.
	rw.readme(ecoNPM, "best", 78)
	rw.readme(ecoPyPI, "best", 24)
	var amb *AmbiguousLibraryError
	if got, err := rw.query(t, "best practices guide"); !errors.Is(err, ErrLibraryRequired) && !errors.As(err, &amb) {
		t.Errorf("best practices guide, three namesakes = %s, %v; want a refusal", got, err)
	}
}

// Readings of different words that answer with different libraries
// contest: one stands only when it outweighs the other by the dominance
// margin. "mock fetch calls in vitality" may be about either.
func TestBareQueryReadingsContest(t *testing.T) {
	for _, tc := range []struct {
		vitality int64
		want     string // "" for an ambiguity
	}{
		{10_000_000, ""},
		{600_000_000, "https://docs.vitality-npm.test/"},
	} {
		rw := newReadingWorld()
		mock := rw.site(ecoNPM, "mock", 5_000_000)
		vitality := rw.site(ecoNPM, "vitality", tc.vitality)
		got, err := rw.query(t, "mock fetch calls in vitality")
		if tc.want == "" {
			ambiguousOver(t, "mock fetch calls in vitality", err, mock, vitality)
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("vitality at %d: = %s, %v; want %s", tc.vitality, got, err, tc.want)
		}
	}
}

// A common word's reading failing upstream does not stop the query when
// another reading answers, and stops it, as an upstream error, only when
// nothing else could.
func TestBareQueryCommonWordFailureDoesNotVeto(t *testing.T) {
	rw := newReadingWorld()
	quasar := rw.site(ecoNPM, "quasar", 900_000)
	rw.pkgs.errs["npm:debug"] = errors.New("npm debug: status 503")
	if got, err := rw.query(t, "debug output in quasar"); err != nil || got != quasar {
		t.Errorf("debug output in quasar = %s, %v; want %s", got, err, quasar)
	}
	_, err := rw.query(t, "debug output options")
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) {
		t.Errorf("debug output options = %v; want an upstream error, debug being the only reading", err)
	}
}

// A reading that names a real package whose docs do not verify keeps
// another reading from answering for it unless outweighed by the
// dominance margin, whichever comes first in the query.
func TestBareQueryUnverifiedReadingBlocks(t *testing.T) {
	for _, tc := range []struct {
		pinch int64
		want  bool // the hammer site answers
	}{{2_000_000, false}, {50, true}} {
		rw := newReadingWorld()
		hammer := rw.site(ecoNPM, "hammer", 5_000)
		rw.readme(ecoNPM, "pinchkit", tc.pinch)
		got, err := rw.query(t, "hammer gestures with pinchkit")
		switch {
		case tc.want && (err != nil || got != hammer):
			t.Errorf("pinchkit at %d: = %s, %v; want %s", tc.pinch, got, err, hammer)
		case !tc.want:
			ambiguousOver(t, "hammer gestures with pinchkit", err, "npm:pinchkit")
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

// A common word's reading beside one that names a library by an uncommon
// word is lean: the registries alone, so the shared web search and
// pkg.go.dev are spent on the reading that names the library. Alone, or
// beside a guess, a common word is resolved in full.
func TestBareQueryLeanCommonReadings(t *testing.T) {
	lean := func(q string) map[string]bool {
		out := map[string]bool{}
		for _, s := range splitLibraryQuery(q) {
			out[s.spec.raw] = s.lean
		}
		return out
	}
	if got := lean("debug output in quasar"); !got["debug"] || got["quasar"] {
		t.Errorf("debug output in quasar: lean %v, want debug lean and quasar not", got)
	}
	if got := lean("express error handling middleware"); got["express"] {
		t.Errorf("express error handling middleware: lean %v, want express in full", got)
	}
	rw := newReadingWorld()
	quasar := rw.site(ecoNPM, "quasar", 900_000)
	rw.site(ecoNPM, "debug", 300_000_000)
	w := rw.web()
	sites := &fakeSites{}
	w.sites = sites
	q := "debug output in quasar"
	m, _, err := w.resolveQuery(context.Background(), q, splitLibraryQuery(q))
	if err == nil {
		t.Fatalf("%q = %s; want an ambiguity, debug outweighing quasar", q, m.root)
	}
	ambiguousOver(t, q, err, quasar)
	for _, sq := range sites.queries {
		if strings.HasPrefix(sq, "debug ") {
			t.Errorf("the lean reading searched the web: %q", sq)
		}
	}
	if !slices.ContainsFunc(sites.queries, func(sq string) bool { return strings.HasPrefix(sq, "quasar ") }) {
		t.Errorf("web searches %v; want quasar's", sites.queries)
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
