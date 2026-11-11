package docs

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/search"
)

// A docs URL written without its scheme is compared as the URL it names
// when no name or package matches it; a value of another shape is not.
func TestBareURL(t *testing.T) {
	for v, want := range map[string]string{
		"auth0.com/docs":           "https://auth0.com/docs",
		"react.dev":                "https://react.dev",
		"github.com/gin-gonic/gin": "https://github.com/gin-gonic/gin",
		"vue-router":               "",
		"@astrojs/starlight":       "",
		"npm:zod":                  "",
		"tanstack query":           "",
		"name@1.2":                 "",
		"v1.2":                     "",
		"https://react.dev/":       "",
	} {
		if got, _ := bareURL(v); got != want {
			t.Errorf("bareURL(%q) = %q, want %q", v, got, want)
		}
	}
	libs := []docsindex.Library{
		{Key: "https://auth0.example.test/docs/", Root: "https://auth0.example.test/docs/", Title: "Auth0", Format: docsindex.FormatVersion},
		{Key: "https://pkg.go.dev/go.uber.org/zap/", Root: "https://pkg.go.dev/go.uber.org/zap/", Title: "Go Packages", Format: docsindex.FormatVersion},
	}
	for v, want := range map[string]string{
		"auth0.example.test/docs":        libs[0].Root, // at the root
		"auth0.example.test":             libs[0].Root, // over it
		"auth0.example.test/docs/rules/": libs[0].Root, // under it: a scope
		"go.uber.org/zap":                libs[1].Root, // a module path, by its name
		"github.com/gin-gonic/gin":       "",
	} {
		got := ""
		if found := MatchIndexed(libs, v); len(found) > 0 {
			got = found[0].Root
		}
		if got != want {
			t.Errorf("MatchIndexed(%q) = %q, want %q", v, got, want)
		}
	}
	if got := scopeOf(libs[0], "auth0.example.test/docs/rules/"); got != "https://auth0.example.test/docs/rules/" {
		t.Errorf("scope of a scheme-less URL below the root = %q", got)
	}
	if got := scopeOf(libs[0], "auth0.example.test/docs"); got != "" {
		t.Errorf("scope of the root itself = %q", got)
	}
}

// A scoped package's last part names the framework it is for as often as
// the package: @testing-library/react is not React's docs, while
// @astrojs/starlight still names Starlight, whose host carries the scope.
func TestScopedPackageTail(t *testing.T) {
	react := docsindex.Library{Key: "https://react.dev/", Root: "https://react.dev/", Title: "React", SiteName: "React", Format: docsindex.FormatVersion}
	starlight := docsindex.Library{Key: "https://starlight.astro.build/", Root: "https://starlight.astro.build/", Title: "Starlight", Format: docsindex.FormatVersion}
	libs := []docsindex.Library{react, starlight}
	if found := MatchIndexed(libs, "@testing-library/react"); len(found) > 0 {
		t.Errorf("@testing-library/react names %s", found[0].Root)
	}
	if found := MatchIndexed(libs, "@astrojs/starlight"); len(found) != 1 || found[0].Key != starlight.Key {
		t.Errorf("@astrojs/starlight = %v", found)
	}
	if found := MatchIndexed(libs, "react"); len(found) != 1 || found[0].Key != react.Key {
		t.Errorf("react = %v", found)
	}
}

// nearSearchFirst is the search backend over two indexed libraries and a
// web search answering with hits.
func nearSearchFirst(t *testing.T, hits []search.Result) (*SearchFirst, func() int32) {
	t.Helper()
	s, store, searches := searchFirstOver(t, exampleSite(), hits)
	for _, lib := range []docsindex.Library{
		{Key: "https://duo.test/docs/", Library: "https://duo.test/docs/", Title: "Cisco Duo", SiteName: "Cisco Duo", Root: "https://duo.test/docs/", Pages: 1},
		{Key: "https://sdk.example.test/docs/", Library: "https://sdk.example.test/docs/", Title: "AI SDK", SiteName: "AI SDK", Root: "https://sdk.example.test/docs/", Pages: 1},
	} {
		lib.Format = docsindex.FormatVersion
		page := lib.Root + "intro"
		if err := store.PutPages(lib.Key, []docsindex.PageData{{Page: docsindex.Page{URL: page, Hash: "h"}, Sections: []docsindex.Section{{Page: page, Heading: "Intro", Crumbs: []string{"Intro"}, Text: "Intro."}}}}); err != nil {
			t.Fatal(err)
		}
		if err := store.PutLibrary(lib); err != nil {
			t.Fatal(err)
		}
	}
	return s, searches.Load
}

// A library value that names no indexed library, with indexed ones near
// it, is answered by whatever answers it, and the answer lists them
// (indexed_near) and says to pass one; a refusal names them in its
// error. A value that names one answers from it with none listed.
func TestIndexedNear(t *testing.T) {
	duo := NearLibrary{Title: "Cisco Duo", Library: "https://duo.test/docs/"}
	ctx := WithLocalOptions(context.Background(), LocalOptions{Hint: HintCLI})

	s, _ := nearSearchFirst(t, exampleHits())
	o := s.GetDocsSelect(ctx, "duo unix", "add middleware to a route", 0)
	if o.Status != Partial || o.Index == nil || !slices.Equal(o.IndexedNear, []NearLibrary{duo}) {
		t.Fatalf("answer from search = %s (%v), near %v; want partial with Cisco Duo near", o.Status, o.Err(), o.IndexedNear)
	}
	fm := NewFrontmatter("add middleware to a route", SearchBackend, "duo unix", o).Render()
	if !strings.Contains(fm, "indexed_near: {Cisco Duo: https://duo.test/docs/}\n") {
		t.Errorf("frontmatter lacks the indexed_near line:\n%s", fm)
	}

	s, _ = nearSearchFirst(t, nil) // the search answers nothing
	o = s.GetDocsSelect(ctx, "duo unix", "add middleware to a route", 0)
	if o.Err() == nil || !strings.Contains(o.Err().Error(), `indexed near "duo unix": Cisco Duo https://duo.test/docs/; pass one of these roots as library`) {
		t.Errorf("refusal = %v, want it to name the library near", o.Err())
	}

	s, searches := nearSearchFirst(t, exampleHits())
	o = s.GetDocsSelect(ctx, "duo", "intro", 0)
	if o.Library != duo.Library || len(o.IndexedNear) != 0 || searches() != 0 {
		t.Errorf("duo = %s near %v after %d searches, want the index of Cisco Duo", o.Library, o.IndexedNear, searches())
	}
	if o = s.GetDocsSelect(ctx, "zzz", "add middleware to a route", 0); len(o.IndexedNear) != 0 {
		t.Errorf("a value near nothing lists %v", o.IndexedNear)
	}
}

// Near libraries print as one flow mapping, a shared or missing title
// giving way to the root; the hint names the roots as the next library.
func TestNearLine(t *testing.T) {
	near := []NearLibrary{{Title: "Rust", Library: "https://docs.rs/a/"}, {Title: "Rust", Library: "https://docs.rs/b/"}, {Library: "https://x.example.test/"}, {Title: "Cisco Duo", Library: "https://duo.test/docs/"}}
	want := "indexed_near: {docs.rs/a/: https://docs.rs/a/, docs.rs/b/: https://docs.rs/b/, x.example.test/: https://x.example.test/, Cisco Duo: https://duo.test/docs/}\n"
	if got := nearLine(near); got != want {
		t.Errorf("nearLine = %q, want %q", got, want)
	}
	if nearLine(nil) != "" || NearHint(HintNone) != "" || !strings.Contains(NearHint(HintCLI), "--library") || !strings.Contains(NearHint(HintMCP), "as library") {
		t.Error("near line or hint wrong")
	}
}

// A resolve name with spaces is answered from the index, compared as a
// key (vercel ai sdk holds AI SDK); when nothing indexed matches it, the
// refusal says so without going to the registries, which take package
// names only.
func TestResolveNameWithSpaces(t *testing.T) {
	s, searches := nearSearchFirst(t, exampleHits())
	ctx := WithLocalOptions(context.Background(), LocalOptions{})
	if o := s.ResolveSelect(ctx, "vercel ai sdk", 5); o.Err() != nil || !slices.Equal(indexedIDs(o.Matches), []string{"https://sdk.example.test/docs/"}) {
		t.Errorf("vercel ai sdk = %v (%v)", indexedIDs(o.Matches), o.Err())
	}
	o := s.ResolveSelect(ctx, "zzz qqq", 5)
	if o.Status != Refused || o.Kind().String() != "not_found" || !strings.Contains(o.Err().Error(), "not a package name") {
		t.Errorf("unindexed name with spaces = %s %v", o.Status, o.Err())
	}
	if searches() != 0 {
		t.Errorf("resolve searched %d times", searches())
	}
}
