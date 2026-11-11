package docs

import (
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
	q := strings.ToLower(query)
	var whole, word []docsindex.Library
	words := resolveWords(q)
	for _, lib := range libs {
		fields := resolveFields(lib)
		switch {
		case anyHolds(fields, q):
			whole = append(whole, lib)
		case anyWordHeld(fields, words):
			word = append(word, lib)
		}
	}
	if len(whole) > 0 {
		return whole
	}
	return word
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
