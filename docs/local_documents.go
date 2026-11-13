package docs

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Single-document libraries (design/docs-evidence.md, Round 13). A
// specification indexed at its own address (an RFC on datatracker, a
// draft) is a library of one page, and its stored title and site name are
// its host's ("IETF Datatracker"): a caller asking about JWT, PKCE or
// OAuth 2.0 named nothing indexed. Such a library also answers to its
// document's own names, read from the page's title and the start of its
// text (docsindex Library.Document and Lead, never stored):
//
//   - the title, and the title without a leading article or a
//     parenthesis ("JSON Web Token");
//   - an acronym the title or the document's lead spells out for the
//     title's first words: "JSON Web Token (JWT)", "Proof Key for Code
//     Exchange (PKCE, ...)" for the title "Proof Key for Code Exchange by
//     OAuth Public Clients";
//   - a name with its version in the title, whole and by major ("OAuth
//     2.0": oauth20, oauth2; "RFC 9110": rfc9110).
//
// A word the title shares with other documents ("OAuth" in two RFC
// titles) is no name of any: only resolve's loose match lists them all.
// A name the root's path rules out (a module path's owner: redis of
// github.com/redis/go-redis, whose package page is titled "redis") is
// none, as Round 11 decided.

var (
	// acronymRe matches an acronym opening a parenthesis: "(JWT)",
	// "(PKCE, pronounced ...)".
	acronymRe = regexp.MustCompile(`\(([A-Z][A-Z0-9]{1,9})[),;\s]`)
	// versionWordRe matches a version after a name: 2.0, 1.1, 9110, v3.
	versionWordRe = regexp.MustCompile(`^v?\d+(?:\.\d+)*$`)
)

// docFunctionWords are the words an acronym and a name skip.
var docFunctionWords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "for": true, "and": true, "to": true, "in": true,
	"on": true, "by": true, "with": true, "via": true, "over": true, "&": true,
}

// documentNames are the names, as keys, a library of one document answers
// to by that document's title and lead; none for any other library.
func documentNames(l docsindex.Library) []string {
	title := strings.TrimSpace(cleanPageTitle(l.Document))
	if title == "" {
		return nil
	}
	var out []string
	add := func(k string) {
		if len(k) >= 3 && !genericNames[k] && !languageWords[k] && !descriptorWords[k] && !docFunctionWords[k] {
			out = append(out, k)
		}
	}
	add(libraryKey(title))
	core := titleCore(title)
	add(libraryKey(core))
	for _, src := range []string{title, l.Lead} {
		for _, acr := range definedAcronyms(src, core) {
			add(libraryKey(acr))
		}
	}
	for _, n := range versionedNames(core) {
		add(n)
	}
	return withoutRuledOut(out, l.Root)
}

// titleCore is a title without a leading article and without what is in
// parentheses: "The OAuth 2.0 Authorization Framework" is "OAuth 2.0
// Authorization Framework", "JSON Web Token (JWT)" is "JSON Web Token".
func titleCore(title string) string {
	var b strings.Builder
	depth := 0
	for _, r := range title {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	words := strings.Fields(b.String())
	if len(words) > 1 && docFunctionWords[strings.ToLower(words[0])] {
		words = words[1:]
	}
	return strings.Join(words, " ")
}

// definedAcronyms are the acronyms text defines for the start of the
// title (core): an acronym in parentheses right after words whose
// initials spell it (skipping function words; a word may give its first
// letter or all its capitals, OpenID giving OID), when those words open
// the title.
func definedAcronyms(text, core string) []string {
	coreWords := idWords(core)
	var out []string
	for _, m := range acronymRe.FindAllStringSubmatchIndex(text, -1) {
		acr := text[m[2]:m[3]]
		before := strings.Fields(text[max(0, m[0]-200):m[0]])
		phrase, ok := spelledBy(before, acr)
		if ok && phrase != "" && strings.HasPrefix(coreWords, idWords(phrase)) {
			out = append(out, acr)
		}
	}
	return out
}

// spelledBy is the shortest run of words ending words whose initials
// spell acr, as a phrase: a function word ("for", "of") gives its first
// letter when the acronym has it there, else none.
func spelledBy(words []string, acr string) (string, bool) {
	n, ok := spells(words, len(words)-1, acr)
	if !ok {
		return "", false
	}
	return strings.Join(words[len(words)-n:], " "), true
}

// spells reports whether the words ending at i spell acr from its end,
// and how many words that takes.
func spells(words []string, i int, acr string) (int, bool) {
	if acr == "" {
		return 0, true
	}
	if i < 0 {
		return 0, false
	}
	w := strings.Trim(words[i], `"'“”‘’,.;:`)
	if docFunctionWords[strings.ToLower(w)] {
		if strings.HasSuffix(acr, strings.ToUpper(w[:1])) {
			if n, ok := spells(words, i-1, acr[:len(acr)-1]); ok {
				return n + 1, true
			}
		}
		n, ok := spells(words, i-1, acr)
		return n + 1, ok
	}
	for _, piece := range acronymPieces(w) {
		if strings.HasSuffix(acr, piece) {
			if n, ok := spells(words, i-1, acr[:len(acr)-len(piece)]); ok {
				return n + 1, true
			}
		}
	}
	return 0, false
}

// acronymPieces are what word w may give an acronym: all its capitals
// (OpenID: OID, JSON: JSON) and its first letter, upper-cased; none for a
// word that does not open with a capital.
func acronymPieces(w string) []string {
	if w == "" || !unicode.IsUpper([]rune(w)[0]) {
		return nil
	}
	var caps strings.Builder
	for _, r := range w {
		if unicode.IsUpper(r) || unicode.IsDigit(r) {
			caps.WriteRune(r)
		}
	}
	first := strings.ToUpper(string([]rune(w)[0]))
	if c := caps.String(); len(c) > 1 {
		return []string{c, first}
	}
	return []string{first}
}

// versionedNames are the names a title gives with a version: a name word
// followed by a version ("OAuth 2.0", "RFC 9110"), as keys, whole and by
// major (oauth20, oauth2).
func versionedNames(core string) []string {
	words := strings.Fields(core)
	var out []string
	for i := 0; i+1 < len(words); i++ {
		name, version := strings.Trim(words[i], ",:;"), strings.Trim(words[i+1], ",:;")
		if !versionWordRe.MatchString(version) || docFunctionWords[strings.ToLower(name)] || !strings.ContainsFunc(name, unicode.IsUpper) {
			continue
		}
		k := libraryKey(name)
		if k == "" || isBareInt(k) {
			continue
		}
		major, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), ".")
		out = append(out, k+libraryKey(version), k+major)
	}
	return out
}

// withoutRuledOut drops the names the root's path rules out: the
// segments pathNames leaves out (a module path's host and owner, docs
// directories, versions).
func withoutRuledOut(names []string, rootURL string) []string {
	root, err := parseWebRoot(rootURL)
	if err != nil {
		return names
	}
	kept := map[string]bool{}
	for _, p := range pathNames(root) {
		kept[libraryKey(p)] = true
	}
	ruled := map[string]bool{}
	for _, seg := range pathSegments(root.prefix) {
		if k := libraryKey(seg); !kept[k] {
			ruled[k] = true
		}
	}
	var out []string
	for _, n := range names {
		if !ruled[n] && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// documentTitle is the title a library shows as: its document's for a
// library of one, else its own.
func documentTitle(l docsindex.Library) string {
	return firstNonEmpty(cleanPageTitle(l.Document), l.Title, l.SiteName)
}
