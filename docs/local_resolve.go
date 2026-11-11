package docs

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// What is indexed (design/docs-evidence.md, Round 10): resolve answers
// from the local index first, with no network. A query names the indexed
// libraries whose root (or the address one is shown under), title, site
// name, the value it was indexed by, its names, aliases or package specs
// hold it, case aside; when none holds the whole query, those holding one
// of its words (three letters or more, not a generic word such as docs or
// a URL part such as https or com: "Linux-PAM" names a root with "pam" in
// it). A docs URL names the libraries at, under or over it, as a library
// value does. An empty query names every library. Only when nothing
// indexed matches does resolve go to the network.

// resolveIndexed is the indexed libraries query names, as resolve's
// matches, in the index's order.
func (l *Local) resolveIndexed(query string) ([]LibraryMatch, error) {
	if l == nil {
		return nil, nil
	}
	libs, err := l.libraries()
	if err != nil {
		return nil, err
	}
	var current []docsindex.Library
	for _, lib := range libs {
		if lib.Current() {
			current = append(current, lib)
		}
	}
	var out []LibraryMatch
	for _, lib := range indexedNamed(current, strings.TrimSpace(query)) {
		out = append(out, indexedMatch(lib))
	}
	return out, nil
}

// indexedNamed is the libraries query names (see the top of this file).
func indexedNamed(libs []docsindex.Library, query string) []docsindex.Library {
	if query == "" {
		return libs
	}
	if isHTTPURL(query) {
		return matchSpec(libs, libSpec{raw: query, url: query})
	}
	return looseNamed(libs, query)
}

// looseNamed is the libraries a name holds loosely: those a field of
// which holds the whole query, else those whose name, title or site name
// the query holds compared as keys (vercel ai sdk holds AI SDK), else
// those a field of which holds one of its words.
func looseNamed(libs []docsindex.Library, query string) []docsindex.Library {
	q := strings.ToLower(strings.TrimSpace(query))
	qk := libraryKey(q)
	var whole, keyed, word []docsindex.Library
	words := resolveWords(q)
	for _, lib := range libs {
		fields := resolveFields(lib)
		switch {
		case anyHolds(fields, q):
			whole = append(whole, lib)
		case keyHeld(qk, lib):
			keyed = append(keyed, lib)
		case anyWordHeld(fields, words):
			word = append(word, lib)
		}
	}
	switch {
	case len(whole) > 0:
		return whole
	case len(keyed) > 0:
		return keyed
	}
	return word
}

// keyHeld reports a query key that holds the library's title, site name
// or one of its full names (libraryNameTiers) as a key of four letters or
// more: "vercel ai sdk" holds AI SDK. A secondary name (a host label, a
// path segment such as github.com) does not count.
func keyHeld(qk string, lib docsindex.Library) bool {
	if len(qk) < 4 {
		return false
	}
	primary, _, _ := libraryNameTiers(lib)
	for _, k := range append([]string{libraryKey(lib.Title), libraryKey(lib.SiteName)}, primary...) {
		if len(k) >= 4 && !genericNames[k] && strings.Contains(qk, k) {
			return true
		}
	}
	return false
}

// nearMax is the most indexed libraries an answer lists as near.
const nearMax = 5

// NearLibrary is an indexed library near a library value that names none:
// its title and its root, the value to pass as library.
type NearLibrary struct {
	Title   string `json:"title"`
	Library string `json:"library"`
}

// nearIndexed is the indexed libraries near a library value that names
// none (design/docs-evidence.md, Round 11), for the caller to pick from,
// never answered from: for a URL (with its scheme or without, a Go module
// path too), those on its host, else those its path names (pathNamed);
// else, or when none is, those the value holds loosely (looseNamed). A
// path that names a library keeps its host's name out: github.com/
// testcontainers/testcontainers-go is near Testcontainers, not GitHub's
// docs. None when more than nearMax are.
func nearIndexed(libs []docsindex.Library, value string) []NearLibrary {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var found []docsindex.Library
	u := value
	if b, ok := bareURL(value); ok {
		u = b
	}
	if isHTTPURL(u) {
		if r, err := parseWebRoot(u); err == nil {
			host := scopeHost(r.u)
			for _, l := range libs {
				if lr, err := parseWebRoot(l.Root); err == nil && scopeHost(lr.u) == host {
					found = append(found, l)
				}
			}
			if len(found) == 0 {
				found = pathNamed(libs, r.u.Path)
			}
		}
	}
	if len(found) == 0 {
		found = looseNamed(libs, value)
	}
	if len(found) == 0 || len(found) > nearMax {
		return nil
	}
	out := make([]NearLibrary, len(found))
	for i, l := range found {
		out[i] = NearLibrary{Title: firstNonEmpty(l.Title, l.SiteName), Library: l.Root}
	}
	return out
}

// pathNamed is the libraries a URL's path names: those one of whose
// names (libraryNameTiers' first two tiers), title or site name, as a key
// that is not a generic word, the path's key holds (four letters or more)
// or one of its segments is (three).
func pathNamed(libs []docsindex.Library, path string) []docsindex.Library {
	path = strings.ToLower(path)
	pk := libraryKey(path)
	if pk == "" {
		return nil
	}
	segs := map[string]bool{}
	for _, seg := range strings.Split(path, "/") {
		segs[libraryKey(seg)] = true
	}
	var out []docsindex.Library
	for _, lib := range libs {
		primary, secondary, _ := libraryNameTiers(lib)
		keys := append(append([]string{libraryKey(lib.Title), libraryKey(lib.SiteName)}, primary...), secondary...)
		for _, k := range keys {
			if len(k) >= 3 && !genericNames[k] && (len(k) >= 4 && strings.Contains(pk, k) || segs[k]) {
				out = append(out, lib)
				break
			}
		}
	}
	return out
}

// nearNote is the sentence a refusal ends with when indexed libraries
// are near the value it was given.
func nearNote(value string, near []NearLibrary) string {
	parts := make([]string, len(near))
	for i, n := range near {
		parts[i] = n.Library
		if n.Title != "" {
			parts[i] = n.Title + " " + n.Library
		}
	}
	return fmt.Sprintf("indexed near %q: %s; pass one of these roots as library", value, strings.Join(parts, ", "))
}

// resolveFields are what a library answers to, lower case.
func resolveFields(lib docsindex.Library) []string {
	out := []string{lib.Root, lib.DisplayRoot, lib.Title, lib.SiteName, lib.Library}
	out = append(out, lib.Names...)
	out = append(out, lib.Aliases...)
	out = append(out, lib.Packages...)
	for i := range out {
		out[i] = strings.ToLower(out[i])
	}
	return out
}

// resolveNoise are words of a query that name no library: URL parts.
var resolveNoise = map[string]bool{"http": true, "https": true, "www": true, "com": true, "org": true, "net": true, "html": true, "htm": true}

// resolveWords are a lower-case query's words a library may be named by:
// runs of letters and digits, three or more long, generic words and URL
// parts out.
func resolveWords(q string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) >= 3 && !genericNames[w] && !resolveNoise[w] {
			out = append(out, w)
		}
	}
	return out
}

// anyHolds reports a field holding s.
func anyHolds(fields []string, s string) bool {
	for _, f := range fields {
		if f != "" && strings.Contains(f, s) {
			return true
		}
	}
	return false
}

// anyWordHeld reports a field holding one of words.
func anyWordHeld(fields, words []string) bool {
	for _, w := range words {
		if anyHolds(fields, w) {
			return true
		}
	}
	return false
}

// indexedMatch is an indexed library as a resolve match: its root as the
// ID to pass as library, its title, version, and what its index holds.
func indexedMatch(lib docsindex.Library) LibraryMatch {
	return LibraryMatch{ID: lib.Root, Title: firstNonEmpty(lib.Title, lib.SiteName), Version: lib.Version, Indexed: true, Index: indexInfo(lib)}
}
