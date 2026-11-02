// Owner: chunk A (inputs and sources).

package docs

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// specPrefixes are the eco: prefixes a library spec may start with.
var specPrefixes = map[string]ecosystem{
	"npm": ecoNPM, "pypi": ecoPyPI, "crates": ecoCrates, "cargo": ecoCrates, "go": ecoGo, "github": ecoGitHub,
}

const specPrefixList = "npm:, pypi:, crates:, cargo:, go: or github:"

var (
	// npmNameRe is a valid npm package name, lower-cased first: an optional
	// @scope/ and a name of URL-safe characters.
	npmNameRe = regexp.MustCompile(`^(?:@[a-z0-9~-][a-z0-9._~-]*/)?[a-z0-9~-][a-z0-9._~-]*$`)
	// pypiNameRe is a valid PyPI project name (PEP 508).
	pypiNameRe = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)
	// crateNameRe is a valid crates.io crate name.
	crateNameRe = regexp.MustCompile(`(?i)^[a-z][a-z0-9_-]{0,63}$`)
	// bareNameRe is a name some registry could hold.
	bareNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// ghOwnerRe and ghRepoRe are a GitHub account and repository name.
	ghOwnerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,38}$`)
	ghRepoRe  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	// goElemRe is one element of a Go module path.
	goElemRe = regexp.MustCompile(`^[A-Za-z0-9._~+-]+$`)
	// specVersionRe is what a spec accepts after "@": a version, a range
	// prefix (^, ~) or a label such as latest.
	specVersionRe = regexp.MustCompile(`^[A-Za-z0-9._+~^*=-]{1,64}$`)
	// versionLikeRe is a version label as people write one in a sentence:
	// a number (optionally after v, ^, ~ or =) or a well-known alias. A
	// word@word token in a query is a spec only when the part after "@"
	// looks like this, so an email address is not.
	versionLikeRe = regexp.MustCompile(`(?i)^(?:[v^~=]*\d[0-9a-z.*+_-]*|latest|stable|current|next|dev|devel|main|master|canary|nightly|beta|alpha|rc|lts|unstable|edge)$`)
)

const maxSpecName = 214 // npm's limit, the longest any registry allows

// parseLibSpec parses a library value (plan §3): an HTTP(S) URL; else an
// optional eco: prefix (npm, pypi, crates or cargo, go, github); a version
// after the last "@" at index > 0; then the name's shape (@scope/pkg is
// npm, a path whose first element has a dot is a Go module, owner/repo is
// GitHub, anything else a bare name). Malformed input (an unknown prefix,
// an empty version, a URL with @version) is an error wrapping
// ErrLibraryRequired.
func parseLibSpec(s string) (libSpec, error) {
	raw := strings.TrimSpace(s)
	spec := libSpec{raw: raw}
	if raw == "" {
		return spec, specError(raw, "it is empty")
	}
	if isHTTPURL(raw) {
		if why := urlSpecProblem(raw); why != "" {
			return spec, specError(raw, why)
		}
		spec.url = raw
		return spec, nil
	}
	rest := raw
	if i := strings.Index(rest, ":"); i >= 0 {
		eco, ok := specPrefixes[strings.ToLower(rest[:i])]
		if !ok {
			return spec, specError(raw, fmt.Sprintf("unknown prefix %q: want %s, or a docs URL with its https:// scheme", rest[:i+1], specPrefixList))
		}
		spec.eco, rest = eco, rest[i+1:]
	}
	if i := strings.LastIndex(rest, "@"); i > 0 {
		rest, spec.version = rest[:i], rest[i+1:]
		if spec.version == "" {
			return spec, specError(raw, "the version after @ is empty")
		}
		if !specVersionRe.MatchString(spec.version) {
			return spec, specError(raw, fmt.Sprintf("%q is not a version", spec.version))
		}
	}
	eco, why := classifySpecName(rest, spec.eco)
	if why != "" {
		return spec, specError(raw, why)
	}
	spec.eco, spec.name = eco, rest
	return spec, nil
}

// specError is a malformed library spec: a validation error.
func specError(raw, why string) error {
	return fmt.Errorf("%w: invalid library %q: %s", ErrLibraryRequired, raw, why)
}

// urlSpecProblem says what is wrong with a URL given as a library, or "".
// A URL names its version already, so an appended @version (which parses
// as user info, or as a last path segment "@5.2") is refused. A last
// segment like cobra@v1.8.0 is left alone: pkg.go.dev's versioned URLs look
// like that, and a match's ID may be one.
func urlSpecProblem(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return err.Error()
	}
	const versioned = "a docs URL names its version already; pass that version's docs URL instead of appending @version"
	if u.User != nil {
		if versionLikeRe.MatchString(u.Host) {
			return versioned
		}
		return "a docs URL cannot carry a user name or password"
	}
	last := u.Path[strings.LastIndex(u.Path, "/")+1:]
	if v, ok := strings.CutPrefix(last, "@"); ok && versionLikeRe.MatchString(v) {
		return versioned
	}
	return ""
}

// classifySpecName checks name (prefix and version already removed)
// against the ecosystem the prefix gave, or infers one from its shape. why
// is "" when the name is valid.
func classifySpecName(name string, eco ecosystem) (ecosystem, string) {
	if name == "" {
		return eco, "the name is empty"
	}
	if len(name) > maxSpecName {
		return eco, "the name is too long"
	}
	switch eco {
	case ecoNPM:
		return eco, validName(npmNameRe.MatchString(strings.ToLower(name)), name, "npm package name")
	case ecoPyPI:
		return eco, validName(pypiNameRe.MatchString(name), name, "PyPI project name")
	case ecoCrates:
		return eco, validName(crateNameRe.MatchString(name), name, "crate name")
	case ecoGo:
		return eco, validName(isModulePath(name), name, "Go module path (such as github.com/spf13/cobra)")
	case ecoGitHub:
		return eco, validName(isOwnerRepo(name), name, "GitHub owner/repo")
	}
	return inferSpecName(name)
}

// inferSpecName classifies an unprefixed name by its shape.
func inferSpecName(name string) (ecosystem, string) {
	switch {
	case strings.HasPrefix(name, "@"):
		if !strings.Contains(name, "/") {
			return ecoNPM, fmt.Sprintf("an npm scope needs a package name: %s/<name>", name)
		}
		return ecoNPM, validName(npmNameRe.MatchString(strings.ToLower(name)), name, "scoped npm package name")
	case strings.Contains(name, "/"):
		first, _, _ := strings.Cut(name, "/")
		if strings.Contains(first, ".") {
			return ecoGo, validName(isModulePath(name), name, "Go module path")
		}
		if isOwnerRepo(name) {
			return ecoGitHub, ""
		}
		return ecoAny, fmt.Sprintf("%q is not a package name, owner/repo or Go module path", name)
	case !bareNameRe.MatchString(name):
		return ecoAny, fmt.Sprintf("%q is not a package name (letters, digits, '.', '-' and '_')", name)
	}
	return ecoAny, ""
}

func validName(ok bool, name, what string) string {
	if ok {
		return ""
	}
	return fmt.Sprintf("%q is not a valid %s", name, what)
}

// isModulePath reports a Go module path: slash-separated elements whose
// first has a dot (a host), none empty or a dot segment.
func isModulePath(p string) bool {
	elems := strings.Split(p, "/")
	if !strings.Contains(elems[0], ".") || strings.HasPrefix(elems[0], ".") || strings.HasPrefix(elems[0], "-") {
		return false
	}
	for _, e := range elems {
		if !goElemRe.MatchString(e) || e == "." || e == ".." {
			return false
		}
	}
	return true
}

// isOwnerRepo reports a GitHub owner/repo pair.
func isOwnerRepo(s string) bool {
	owner, repo, ok := strings.Cut(s, "/")
	return ok && ghOwnerRe.MatchString(owner) && ghRepoRe.MatchString(repo) && repo != "." && repo != ".."
}

// normalizeName returns name in eco's canonical form for lookups and
// comparison: PEP 503 for PyPI, lower case for npm, "-" for "_" for crates,
// the module path unchanged for go (normalizeName does not case-encode;
// the proxy client does), owner/repo lower-cased for github.
//
// crates.io names are also case-insensitive, so crates are lower-cased
// too; a bare name (ecoAny) is lower-cased.
func normalizeName(eco ecosystem, name string) string {
	name = strings.TrimSpace(name)
	switch eco {
	case ecoPyPI:
		return pep503(name)
	case ecoCrates:
		return strings.ReplaceAll(strings.ToLower(name), "_", "-")
	case ecoGo:
		return name
	}
	return strings.ToLower(name)
}

// pep503 is PyPI's normalized name: runs of "-", "_" and "." become one
// "-", lower case.
func pep503(name string) string {
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(name) {
		if r == '-' || r == '_' || r == '.' {
			sep = true
			continue
		}
		if sep && b.Len() > 0 {
			b.WriteByte('-')
		}
		sep = false
		b.WriteRune(r)
	}
	return b.String()
}

// nameVariants returns the normalized repository names a project called
// name may live under, name itself first: n, n+"js", n.js, n-js, py- and
// -py, -rs, go-. GitHub search results are kept only when their name is
// one of these.
//
// Only the last path element of a scoped npm name or module path counts.
// A name that already ends in .js or -js adds its stem's variants.
func nameVariants(name string) []string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = n[strings.LastIndex(n, "/")+1:]
	if n == "" {
		return nil
	}
	bases := []string{n}
	for _, suffix := range []string{".js", "-js"} {
		if stem, ok := strings.CutSuffix(n, suffix); ok && stem != "" {
			bases = append(bases, stem)
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, b := range bases {
		for _, v := range []string{b, b + "js", b + ".js", b + "-js", "py-" + b, b + "-py", b + "-rs", "go-" + b} {
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// registriesFor lists the registries spec can be looked up in, in
// lookupAll's order: the prefixed or implied one, or for a bare name each
// registry where it is a valid name. GitHub and URL specs name none.
func registriesFor(spec libSpec) []ecosystem {
	if spec.url != "" {
		return nil
	}
	switch spec.eco {
	case ecoNPM, ecoPyPI, ecoCrates, ecoGo:
		return []ecosystem{spec.eco}
	case ecoGitHub:
		return nil
	}
	var out []ecosystem
	if npmNameRe.MatchString(strings.ToLower(spec.name)) && len(spec.name) <= maxSpecName {
		out = append(out, ecoNPM)
	}
	if pypiNameRe.MatchString(spec.name) {
		out = append(out, ecoPyPI)
	}
	if crateNameRe.MatchString(spec.name) {
		out = append(out, ecoCrates)
	}
	return out
}

// Splitting bare queries (plan §3).

// queryFiller are words a question opens with before it names anything,
// beside webStopwords: question words, auxiliaries and pronouns.
var queryFiller = map[string]bool{
	"why": true, "when": true, "where": true, "which": true, "who": true, "whats": true, "hows": true,
	"does": true, "did": true, "should": true, "would": true, "could": true, "will": true, "shall": true,
	"am": true, "was": true, "were": true, "there": true, "way": true, "any": true, "some": true,
	"me": true, "we": true, "you": true, "use": true, "using": true, "need": true, "want": true, "please": true,
}

// queryLinkWords introduce the library at the end of a question: "... in
// requests".
var queryLinkWords = map[string]bool{"in": true, "with": true, "using": true, "for": true}

// plainWordRe is a word the hyphen-join guess may use.
var plainWordRe = regexp.MustCompile(`^[a-z0-9]+$`)

const maxQuerySplits = 3

// splitLibraryQuery reads a bare docs query as at most three library
// candidates, in priority order (plan §3): an explicit spec token alone;
// else a trailing "in|with|using|for <tok>" after leading filler; the first
// two remaining words hyphen-joined (strict); the first remaining word,
// also without a .js suffix. Each candidate leaves at least one
// non-stopword in its query; nil when no reading does.
//
// An unprefixed owner/repo token is the one weak explicit spec:
// "async/await" and "app/page.tsx" have its shape. It goes first, and the
// word readings of the query (which keep the token as a search term)
// follow it.
func splitLibraryQuery(q string) []querySplit {
	words := strings.Fields(q)
	for i, w := range words {
		spec, ok := explicitSpec(cleanQueryWord(w))
		if !ok {
			continue
		}
		var out []querySplit
		if rest := joinWithout(words, i, i+1); hasQueryTerms(rest) {
			out = append(out, querySplit{spec: spec, query: rest})
		}
		if spec.eco == ecoGitHub && !strings.Contains(spec.raw, ":") {
			out = append(out, wordSplits(words)...)
		}
		return capSplits(out)
	}
	return capSplits(wordSplits(words))
}

// explicitSpec parses a query word as a spec that is more than a bare
// name: a URL, a prefixed spec, name@version, @scope/pkg, a Go module path
// or owner/repo.
func explicitSpec(w string) (libSpec, bool) {
	if !strings.ContainsAny(w, ":/@") {
		return libSpec{}, false
	}
	spec, err := parseLibSpec(w)
	if err != nil {
		return libSpec{}, false
	}
	switch {
	case spec.url != "":
		return spec, true
	case spec.version != "":
		return spec, versionLikeRe.MatchString(spec.version)
	}
	return spec, spec.eco != ecoAny
}

// wordSplits are the readings of a query with no explicit spec: the
// trailing "in <tok>" after filler, the hyphen-joined first two words, the
// first word and its .js stem.
func wordSplits(words []string) []querySplit {
	start := 0
	for start < len(words) && isQueryFiller(words[start]) {
		start++
	}
	var out []querySplit
	add := func(name string, strict bool, from, to int) {
		if s, ok := wordSplit(words, name, strict, from, to); ok {
			out = append(out, s)
		}
	}
	n := len(words)
	if start > 0 && n-2 > start && queryLinkWords[strings.ToLower(words[n-2])] {
		add(cleanQueryWord(words[n-1]), false, n-2, n)
	}
	if name, ok := hyphenJoin(words, start); ok {
		add(name, true, start, start+2)
	}
	if start < n {
		first := cleanQueryWord(words[start])
		add(first, false, start, start+1)
		add(trimJSSuffix(first), false, start, start+1) // capSplits drops it when there is no suffix
	}
	return out
}

// wordSplit reads name as the library and the query without
// words[from:to]; false when name is not a bare name or nothing is left
// to search for.
func wordSplit(words []string, name string, strict bool, from, to int) (querySplit, bool) {
	spec, err := parseLibSpec(name)
	if err != nil || spec.eco != ecoAny || spec.version != "" {
		return querySplit{}, false
	}
	rest := joinWithout(words, from, to)
	if !hasQueryTerms(rest) {
		return querySplit{}, false
	}
	return querySplit{spec: spec, query: rest, strict: strict}, true
}

// hyphenJoin is the first two words from start joined with "-" ("react
// router" is react-router), when both are plain words that name something.
func hyphenJoin(words []string, start int) (string, bool) {
	if len(words)-start < 2 {
		return "", false
	}
	a, b := strings.ToLower(cleanQueryWord(words[start])), strings.ToLower(cleanQueryWord(words[start+1]))
	ok := plainWordRe.MatchString(a) && plainWordRe.MatchString(b) && !isQueryFiller(b) && !queryLinkWords[b]
	return a + "-" + b, ok
}

// capSplits drops repeated names and keeps the first maxQuerySplits.
func capSplits(splits []querySplit) []querySplit {
	var out []querySplit
	seen := map[string]bool{}
	for _, s := range splits {
		key := strings.ToLower(s.spec.raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
		if len(out) == maxQuerySplits {
			break
		}
	}
	return out
}

func trimJSSuffix(w string) string {
	if len(w) > 3 && strings.EqualFold(w[len(w)-3:], ".js") {
		return w[:len(w)-3]
	}
	return w
}

// queryQuotes are the quotes and brackets a word may be wrapped in.
const queryQuotes = "\"'`“”‘’()[]{}<>"

// cleanQueryWord strips the punctuation a word picks up in a sentence:
// quotes, backticks, brackets and trailing ",.;:!?".
func cleanQueryWord(w string) string {
	w = strings.Trim(w, queryQuotes)
	w = strings.TrimRight(w, ",.;:!?")
	return strings.Trim(w, queryQuotes)
}

// isQueryFiller reports a word that names nothing: a stopword or filler.
func isQueryFiller(w string) bool {
	k := strings.ToLower(cleanQueryWord(w))
	k = strings.NewReplacer("'", "", "’", "").Replace(k)
	return webStopwords[k] || webStopwords[stem(k)] || queryFiller[k]
}

// hasQueryTerms reports a query with a word worth searching for.
func hasQueryTerms(q string) bool {
	for _, t := range tokenize(q) {
		if !webStopwords[t] && !queryFiller[t] && (len(t) > 1 || unicode.IsDigit(rune(t[0]))) {
			return true
		}
	}
	return false
}

// joinWithout joins words, leaving out words[from:to].
func joinWithout(words []string, from, to int) string {
	kept := make([]string, 0, len(words))
	kept = append(kept, words[:from]...)
	kept = append(kept, words[to:]...)
	return strings.Join(kept, " ")
}
