package docs

import (
	"slices"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// specLib is a library of one page as docsindex.Libraries reads it: the
// host's chrome as its title, the page's title and lead as its document.
func specLib(root, title, lead string) docsindex.Library {
	return docsindex.Library{Key: root, Root: root, Library: root, Title: "Example Tracker", SiteName: "Example Tracker",
		Format: docsindex.FormatVersion, Pages: 1, Document: title, Lead: lead}
}

func TestDocumentNames(t *testing.T) {
	cases := []struct {
		name      string
		lib       docsindex.Library
		want, not []string
	}{
		{"title with its acronym", specLib("https://specs.test/doc/rfc1/", "Demo Web Token (DWT)", ""),
			[]string{"demowebtoken", "dwt"}, nil},
		{"a name with its version, whole and by major; no leading article", specLib("https://specs.test/doc/rfc2/", "The Demo 2.0 Access Framework", ""),
			[]string{"demo20", "demo2", "demo20accessframework"}, []string{"demo", "access", "framework", "the"}},
		{"an acronym the lead spells for the title's first words", specLib("https://specs.test/doc/rfc3/", "Proof of Demo Exchange by Public Clients",
			"This document defines the Proof of Demo\n   Exchange (PODE, said \"pode\") for public clients."),
			[]string{"pode"}, []string{"pdebpc"}},
		{"an acronym of other words is not the title's", specLib("https://specs.test/doc/rfc4/", "Demo Web Token",
			"It builds on the Message Authentication Code (MAC) and the Web Token (WT)."),
			nil, []string{"mac", "wt"}},
		{"initials that do not spell it", specLib("https://specs.test/doc/rfc5/", "Demo Web Token (XYZ)", ""), nil, []string{"xyz"}},
		{"a module path's owner is no name", specLib("https://pkg.go.test/example.com/acme/go-acme/v2/", "acme", ""), nil, []string{"acme"}},
		{"no document", docsindex.Library{Key: "https://x.test/", Root: "https://x.test/", Title: "Example", Pages: 1}, nil, []string{"example"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := documentNames(c.lib)
			for _, w := range c.want {
				if !slices.Contains(got, w) {
					t.Errorf("names %v lack %q", got, w)
				}
			}
			for _, n := range c.not {
				if slices.Contains(got, n) {
					t.Errorf("names %v hold %q", got, n)
				}
			}
		})
	}
}

// Two documents that share a word (OAuth in two titles) are both listed by
// resolve and named by neither for a library value; each answers to its
// own names.
func TestDocumentLibrariesMatch(t *testing.T) {
	framework := specLib("https://specs.test/doc/html/rfc6749/", "The OAuth 2.0 Authorization Framework", "")
	pkce := specLib("https://specs.test/doc/html/rfc7636/", "Proof Key for Code Exchange by OAuth Public Clients",
		"A client can use Proof Key for Code Exchange (PKCE) against this attack.")
	jwt := specLib("https://specs.test/doc/html/rfc7519/", "JSON Web Token (JWT)", "")
	libs := []docsindex.Library{framework, pkce, jwt}
	named := func(value string) []string {
		var roots []string
		for _, l := range MatchIndexed(libs, value) {
			roots = append(roots, l.Root)
		}
		return roots
	}
	for value, want := range map[string]string{
		"oauth 2.0": framework.Root, "OAuth2": framework.Root, "RFC 6749": framework.Root,
		"pkce": pkce.Root, "jwt": jwt.Root, "JSON Web Token": jwt.Root, "rfc7519": jwt.Root,
	} {
		if got := named(value); !slices.Equal(got, []string{want}) {
			t.Errorf("library %q names %v, want %s", value, got, want)
		}
	}
	for _, value := range []string{"oauth", "rfc", "token", "authorization", "oauth 2.1", "exchange", "example tracker"} {
		if got := named(value); len(got) > 0 {
			t.Errorf("library %q names %v", value, got)
		}
	}
	if near := nearIndexed(libs, "oauth"); len(near) != 2 || near[0].Title != framework.Document {
		t.Errorf("oauth is near %v, want both OAuth documents under their own titles", near)
	}
	var listed []string
	for _, l := range indexedNamed(libs, "oauth") {
		listed = append(listed, l.Root)
	}
	if !slices.Equal(listed, []string{framework.Root, pkce.Root}) {
		t.Errorf("resolve oauth lists %v, want both OAuth documents", listed)
	}
	if m := indexedMatch(jwt); m.Title != "JSON Web Token (JWT)" {
		t.Errorf("resolve shows the document as %q, want its own title", m.Title)
	}
	for q, want := range map[string]string{
		"RFC 7636 PKCE code_verifier 43 to 128 characters": pkce.Root,
		"OAuth 2.0 authorization code expiry how long":     framework.Root,
		"JWT exp claim seconds or milliseconds":            jwt.Root,
		"PKCE code_verifier length limit":                  pkce.Root,
	} {
		found, _ := libraryInQuery(libs, q)
		if len(found) != 1 || found[0].Root != want {
			t.Errorf("question %q names %v, want %s", q, found, want)
		}
	}
	if found, _ := libraryInQuery(libs, "OAuth 2.1 draft PKCE plain method"); len(found) > 0 {
		t.Errorf("a version no document has named %s", found[0].Root)
	}
}
