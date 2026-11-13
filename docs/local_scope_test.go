package docs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// scopedSearchFirst is the search backend over the example site indexed.
func scopedSearchFirst(t *testing.T) *SearchFirst {
	t.Helper()
	f := exampleSite()
	s, store, _ := searchFirstOver(t, f, exampleHits())
	buildIndex(t, f, store)
	return s
}

// A library value is a root; a URL below an indexed root is a scope: the
// URL itself, its fragment dropped, and never the root, a URL over it, a
// name or a spec.
func TestScopeOf(t *testing.T) {
	lib := docsindex.Library{Root: idxRoot, DisplayRoot: "https://docs.example.test/latest/"}
	cases := map[string]string{
		idxRoot:                                     "",
		"https://docs.example.test/docs":            "",
		"https://docs.example.test/":                "",
		"example":                                   "",
		idxRoot + "guide/middleware":                idxRoot + "guide/middleware",
		idxRoot + "guide/middleware#x":              idxRoot + "guide/middleware",
		idxRoot + "guide/":                          idxRoot + "guide/",
		"https://www.docs.example.test/docs/guide/": "https://www.docs.example.test/docs/guide/",
		"https://docs.example.test/latest/guide":    "https://docs.example.test/latest/guide",
		"https://docs.example.test/docsx/guide":     "",
	}
	for in, want := range cases {
		if got := scopeOf(lib, in); got != want {
			t.Errorf("scopeOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// A page URL as library answers from that page alone, and says so: the
// library is the indexed root, scope the URL; the terms are counted in
// the whole library.
func TestScopedAnswerPage(t *testing.T) {
	s := scopedSearchFirst(t)
	page := idxRoot + "guide/middleware"
	o := s.GetDocsSelect(context.Background(), page, "register middleware with app.use", 0)
	if o.Err() != nil || len(o.Results) == 0 || o.Library != idxRoot || o.Scope != page {
		t.Fatalf("outcome = %s %v library %q scope %q, %d results", o.Status, o.Err(), o.Library, o.Scope, len(o.Results))
	}
	for _, r := range o.Results {
		if !strings.HasPrefix(r.URL, page+"#") && r.URL != page {
			t.Errorf("result %s is not on %s", r.URL, page)
		}
	}
	f := NewFrontmatter("q", SearchBackend, page, o).Render()
	if !strings.Contains(f, "\nlibrary: "+idxRoot+"\nscope: "+page+"\n") {
		t.Errorf("frontmatter, want library the root and scope the page:\n%s", f)
	}
	whole := s.GetDocsSelect(context.Background(), idxRoot, "register middleware with app.use", 0)
	if !slices.Equal(whole.Terms, o.Terms) || whole.Scope != "" {
		t.Errorf("terms %v scoped, %v whole; scope %q: want the library's counts and no scope", o.Terms, whole.Terms, whole.Scope)
	}
}

// A directory URL scopes to the pages under it.
func TestScopedAnswerDirectory(t *testing.T) {
	s := scopedSearchFirst(t)
	o := s.GetDocsSelect(context.Background(), idxRoot+"api/", "app.fetch handler", 0)
	if o.Err() != nil || len(o.Results) == 0 || o.Scope != idxRoot+"api/" {
		t.Fatalf("outcome = %s %v scope %q", o.Status, o.Err(), o.Scope)
	}
	for _, r := range o.Results {
		if !strings.HasPrefix(r.URL, idxRoot+"api/") {
			t.Errorf("result %s is not under api/", r.URL)
		}
	}
}

// A distinctive term the library holds only outside the scope is named
// in outside_scope, not not_found (nowhere in the library); the answer is
// partial for it, refused when no distinctive term is under the scope.
func TestScopedAnswerOutsideScope(t *testing.T) {
	s := scopedSearchFirst(t)
	page := idxRoot + "guide/middleware"
	o := s.GetDocsSelect(context.Background(), page, "app.use c.req.param pam_sss", 0)
	if o.Status != Partial || !strings.Contains(o.Reason, ReasonTermsOutsideScope) || !strings.Contains(o.Reason, ReasonTermsNotFound) ||
		!slices.Equal(o.OutsideScope, []string{"c.req.param"}) || !slices.Equal(o.NotFound, []string{"pam_sss"}) {
		t.Fatalf("outcome = %s (%s), outside %v, not_found %v", o.Status, o.Reason, o.OutsideScope, o.NotFound)
	}
	f := NewFrontmatter("q", SearchBackend, page, o).Render()
	if !strings.Contains(f, "\nnot_found: [pam_sss]\noutside_scope: [c.req.param]\n") {
		t.Errorf("frontmatter:\n%s", f)
	}
	r := s.GetDocsSelect(context.Background(), page, "c.req.param", 0)
	if r.Status != Refused || !errors.Is(r.Err(), ErrTermsOutsideScope) || len(r.Results) != 0 || !slices.Equal(r.OutsideScope, []string{"c.req.param"}) || r.Index == nil {
		t.Errorf("refusal = %s %v, outside %v", r.Status, r.Err(), r.OutsideScope)
	}
}

// A URL under the root with no indexed page under it is refused, with a
// note naming the root: never widened.
func TestScopedAnswerEmpty(t *testing.T) {
	s := scopedSearchFirst(t)
	o := s.GetDocsSelect(context.Background(), idxRoot+"no/such/page", "app.use", 0)
	if o.Status != Refused || !errors.Is(o.Err(), ErrScopeEmpty) || len(o.Results) != 0 || o.Index == nil || o.Library != idxRoot ||
		!strings.Contains(o.Note, "no indexed page of "+idxRoot+" is at or under "+idxRoot+"no/such/page") {
		t.Fatalf("outcome = %s %v library %q note %q", o.Status, o.Err(), o.Library, o.Note)
	}
	if f := NewFrontmatter("q", SearchBackend, idxRoot+"no/such/page", o).Render(); !strings.Contains(f, "status: refused (scope_empty)") || strings.Contains(f, "outside_scope") {
		t.Errorf("frontmatter:\n%s", f)
	}
}

// An answer from pages a search read under a library URL below its docs
// tree is scoped to the root read; one for the tree itself, a name or no
// library is not.
func TestReadScope(t *testing.T) {
	deep, _ := parseWebRoot("https://duo.example.test/docs/sso-oidc")
	tree, _ := parseWebRoot("https://duo.example.test/docs/")
	if got := readScope(deep, "https://duo.example.test/docs/sso-oidc"); got != "https://duo.example.test/docs/sso-oidc/" {
		t.Errorf("deep: %q", got)
	}
	for _, c := range []struct {
		root    webRoot
		library string
	}{{tree, "https://duo.example.test/docs/"}, {deep, "duo"}, {deep, ""}} {
		if got := readScope(c.root, c.library); got != "" {
			t.Errorf("readScope(%s, %q) = %q, want none", c.root, c.library, got)
		}
	}
}
