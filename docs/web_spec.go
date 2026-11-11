// Owner: chunk A (inputs and sources).

package docs

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
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

// Splitting bare queries (plan §3, decision-15).
//
// A bare query names its library somewhere among its words, and most of
// its words name no library: they are the question. Registry existence
// says nothing about which word is the name (npm has set, parse and add;
// crates.io has best), so a word becomes a reading only where the query's
// syntax puts a name:
//
//   - an explicit spec token (a URL, eco:name, name@version, @scope/pkg, a
//     module path, owner/repo);
//   - in a sentence (a question, or a query ending in "?"), the topic it
//     opens with ("django: how ...", "In Django, how ...", "pandas how to
//     ..."), the object of a closing "in/with/using/for/via X" ("... in
//     requests", "with the stripe api"), of "use X", a word before
//     "library", "package",
//     "framework" and the like, a possessive ("react's"), and, only when
//     none of those names one, the subject right after an auxiliary ("how
//     does tokio ...", "is httpx ..."). The first word after "how do I" or
//     "how to" is the verb of the question, never a name;
//   - in a keyword query (no question opener), its first word ("flask
//     blueprint url prefix"), unless the next word is an article or a
//     particle, which makes it an imperative verb ("parse a date ...", "set
//     up logging ..."); and a closing "in/with/... X" as in a sentence.
//
// Never a reading: a programming language or ecosystem word (python,
// rust, npm), which names a language the query is about, and a
// camel-case identifier such as useState or createSignal, which names an
// API. A reading whose name is a common English or programming word is
// marked common: it names a library only past commonWordBar.

// sentenceOpeners are the first words of a question or a first-person
// request: question words, auxiliaries and subject pronouns.
var sentenceOpeners = wordSet(`how hows what whats why when where which who whom whose
	can could should would will shall may might must is are am was were does do did doe
	i im ive id we lets`)

// linkWords introduce the library closing a query: "... in requests",
// "... with zod", "... using tenacity".
var linkWords = wordSet(`in with using for via`)

// kindLinkWords introduce a library only before a kind noun: "add
// middleware to an express app", "import it from the lodash package".
var kindLinkWords = wordSet(`to into on from inside`)

// kindNouns are the nouns that say the word before them names a software
// thing: "the stripe api", "a django project", "a vue component".
var kindNouns = wordSet(`library libraries lib libs package packages pkg module modules framework frameworks
	crate crates sdk api apis app apps application applications project projects component components
	server client plugin plugins docs documentation codebase cli`)

// isKindNoun reports a kind noun, singular or plural.
func isKindNoun(w string) bool {
	k := queryWordKey(w)
	return kindNouns[k] || kindNouns[stem(k)]
}

// libKindNouns are the kind nouns that name a library outright, wherever
// they stand: "the requests library", "the clap crate".
var libKindNouns = wordSet(`library lib package module framework crate sdk plugin`)

// queryDeterminers stand between a link word and its object, and after a
// verb: an imperative's first word is followed by one ("parse a date").
var queryDeterminers = wordSet(`a an the my your our their its this that these those some all each every
	any multiple several another`)

// queryParticles follow a phrasal verb: "set up", "spin off", "log out".
var queryParticles = wordSet(`up out off down over away back`)

// queryPronouns are subjects: the word after one is a verb.
var queryPronouns = wordSet(`i we you they he she it one`)

// useWords take the tool a question is about as their object: "use X".
var useWords = wordSet(`use using uses`)

// auxWords put a subject right after them: "how does tokio select work".
var auxWords = wordSet(`does do did is are was were can could should would will doe`)

// languageWords name a programming language, runtime or package ecosystem:
// a query names its library's language with them ("... in python"), never
// the library. queryEcoWords are among them.
var languageWords = wordSet(`python python3 py javascript js typescript ts node nodejs deno bun rust golang go
	java kotlin scala swift ruby php perl csharp cpp c html css sql bash shell zsh powershell lua elixir erlang
	haskell dart r julia npm pypi pip cargo crate crates`)

// apiIdentRe is a camel-case identifier with a lower-case word first
// (useState, createSignal, formatDistanceToNow): an API's name. A single
// lower-case letter first (jQuery, iOS) is a product's way of writing its
// name, not an identifier.
var apiIdentRe = regexp.MustCompile(`^[a-z]{2,}[a-z0-9]*[A-Z]`)

// plainWordRe is a word the hyphen-join guess may use.
var plainWordRe = regexp.MustCompile(`^[a-z0-9]+$`)

const maxQuerySplits = 3

// wordSet is a set of the whitespace-separated words of s.
func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// splitLibraryQuery reads a bare docs query as at most three library
// readings, in priority order: an explicit spec token alone; else the
// readings the query's syntax gives its words (see above). Each reading
// leaves at least one non-stopword in its query; nil when none does.
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
			out = append(out, querySplit{spec: spec, query: rest, from: i, to: i + 1})
		}
		if spec.eco == ecoGitHub && !strings.Contains(spec.raw, ":") {
			out = append(out, wordSplits(words)...)
		}
		return leanCommon(capSplits(out))
	}
	return leanCommon(capSplits(wordSplits(words)))
}

// leanCommon marks the common words' readings lean when another reading,
// not a guess, names a library by an uncommon word.
func leanCommon(splits []querySplit) []querySplit {
	if !slices.ContainsFunc(splits, func(s querySplit) bool { return !s.common && !s.strict }) {
		return splits
	}
	for i := range splits {
		splits[i].lean = splits[i].common
	}
	return splits
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

// queryReader collects a query's word readings.
type queryReader struct {
	words []string
	out   []querySplit
}

// wordSplits are the readings of a query with no explicit spec, in
// priority order: for a sentence the named ones; for a keyword query the
// hyphen-joined first two words, the first word and its .js stem, then a
// closing "in X".
func wordSplits(words []string) []querySplit {
	r := &queryReader{words: words}
	start := 0
	for start < len(words) && isQueryFiller(words[start]) {
		start++
	}
	if isSentence(words) {
		r.topic()
		r.linkObject()
		r.useObjects()
		r.kindNamed()
		r.possessives()
		if len(r.out) == 0 {
			r.auxSubject()
		}
		return r.out
	}
	r.head(start)
	r.linkObject()
	r.kindNamed()
	r.possessives()
	return r.out
}

// isSentence reports a query written as a question or a first-person
// request: it opens with a question word, an auxiliary or a subject
// pronoun, or ends in "?".
func isSentence(words []string) bool {
	if len(words) == 0 {
		return false
	}
	return strings.HasSuffix(words[len(words)-1], "?") || sentenceOpeners[queryWordKey(words[0])]
}

// queryWordKey is a word as the word lists hold it: lower case, without
// the punctuation and apostrophes it picks up in a sentence.
func queryWordKey(w string) string {
	k := strings.ToLower(cleanQueryWord(w))
	return strings.NewReplacer("'", "", "’", "").Replace(k)
}

// topic reads the words a question opens with before it asks: a label
// ("django: how do I ..."), a phrase "in/with/for X," ("In Django, how do
// I ..."), or a word right before the question word ("pandas how to merge
// ..."). The question that follows is about it.
func (r *queryReader) topic() {
	n := len(r.words)
	if n < 2 {
		return
	}
	first := r.words[0]
	switch {
	case linkWords[queryWordKey(first)] && nameLike(r.words[1]) && (strings.HasSuffix(r.words[1], ",") || n > 2 && sentenceOpeners[queryWordKey(r.words[2])]):
		r.add(cleanQueryWord(r.words[1]), false, 1, 2, 0, 2)
	case !sentenceOpeners[queryWordKey(first)] && nameLike(first) && (strings.HasSuffix(first, ":") || sentenceOpeners[queryWordKey(r.words[1])]):
		r.add(cleanQueryWord(first), false, 0, 1, 0, 1)
	}
}

// head reads a keyword query's first word (from start, past filler), and
// it joined with the next, unless the next word makes it a verb.
func (r *queryReader) head(start int) {
	n := len(r.words)
	if start >= n {
		return
	}
	if start+1 < n {
		next := queryWordKey(r.words[start+1])
		if queryDeterminers[next] || queryParticles[next] {
			return // "parse a date ...", "set up logging ...": an imperative verb
		}
	}
	if start+1 < n && nameLike(r.words[start]) && versionWordRe.MatchString(cleanQueryWord(r.words[start+1])) {
		// A name and its number ("RFC 7636", "OAuth 2.0") may be one name
		// (rfc7636, oauth20); a guess, so a miss reads on.
		r.add(cleanQueryWord(r.words[start])+"-"+cleanQueryWord(r.words[start+1]), true, start, start+2, start, start+2)
	}
	if name, ok := hyphenJoin(r.words, start); ok && nameLike(r.words[start]) && nameLike(r.words[start+1]) {
		r.add(name, true, start, start+2, start, start+2)
	}
	if first := cleanQueryWord(r.words[start]); nameLike(first) {
		r.add(first, false, start, start+1, start, start+1)
		r.add(trimJSSuffix(first), false, start, start+1, start, start+1) // capSplits drops it when there is no suffix
	}
}

// linkObject reads the object closing the query after a link word, an
// article between them and a kind noun after it allowed: "in requests",
// "with the stripe api", "to an express app". Two words there ("in react
// router") are a hyphen-joined guess.
func (r *queryReader) linkObject() {
	n := len(r.words)
	end := n
	kind := end > 0 && isKindNoun(r.words[end-1])
	if kind {
		end--
	}
	if i, ok := r.linkBefore(end-1, kind); ok && nameLike(r.words[end-1]) {
		r.add(cleanQueryWord(r.words[end-1]), false, end-1, end, i, end)
		r.add(trimJSSuffix(cleanQueryWord(r.words[end-1])), false, end-1, end, i, end)
		return
	}
	if i, ok := r.linkBefore(end-2, kind); ok && nameLike(r.words[end-2]) && nameLike(r.words[end-1]) {
		if name, ok := hyphenJoin(r.words, end-2); ok {
			r.add(name, true, end-2, end, i, end)
		}
	}
}

// linkBefore is the index of the link word governing the object at j,
// with an article between them allowed; a kind link word ("to") counts
// only before a kind noun.
func (r *queryReader) linkBefore(j int, kind bool) (int, bool) {
	i := j - 1
	if i >= 0 && queryDeterminers[queryWordKey(r.words[i])] {
		i--
	}
	if j < 1 || i < 0 {
		return 0, false
	}
	w := queryWordKey(r.words[i])
	return i, linkWords[w] || kind && kindLinkWords[w]
}

// useObjects reads the object of "use X" ("how do I use zod to ...").
func (r *queryReader) useObjects() {
	for i := 0; i+1 < len(r.words); i++ {
		if !useWords[queryWordKey(r.words[i])] {
			continue
		}
		j := i + 1
		if queryDeterminers[queryWordKey(r.words[j])] && j+1 < len(r.words) {
			j++
		}
		if nameLike(r.words[j]) {
			r.add(cleanQueryWord(r.words[j]), false, j, j+1, j, j+1)
		}
	}
}

// kindNamed reads the word before a noun that names a library: "the
// requests library", "the clap crate".
func (r *queryReader) kindNamed() {
	for j := 0; j+1 < len(r.words); j++ {
		if libKindNouns[queryWordKey(r.words[j+1])] && nameLike(r.words[j]) {
			r.add(cleanQueryWord(r.words[j]), false, j, j+1, j, j+1)
		}
	}
}

// possessives reads a possessive: "react's useEffect".
func (r *queryReader) possessives() {
	for j, w := range r.words {
		c := cleanQueryWord(w)
		for _, suffix := range []string{"'s", "’s"} {
			if stem, ok := strings.CutSuffix(c, suffix); ok && nameLike(stem) {
				r.add(stem, false, j, j+1, j, j+1)
			}
		}
	}
}

// auxSubject reads the subject right after an auxiliary near the start of
// a question: "how does tokio select work", "is httpx async". An article
// or pronoun there ("what is the best way", "how do I") means no name.
func (r *queryReader) auxSubject() {
	for i := 0; i+1 < len(r.words) && i < 3; i++ {
		if !auxWords[queryWordKey(r.words[i])] {
			continue
		}
		if next := r.words[i+1]; nameLike(next) {
			r.add(cleanQueryWord(next), false, i+1, i+2, i+1, i+2)
		}
		return
	}
}

// add reads name as the library over words [from, to), searching the
// query without words [cutFrom, cutTo); nothing when name is not a bare
// name or nothing is left to search for.
func (r *queryReader) add(name string, strict bool, from, to, cutFrom, cutTo int) {
	spec, err := parseLibSpec(name)
	if err != nil || spec.eco != ecoAny || spec.version != "" {
		return
	}
	rest := joinWithout(r.words, cutFrom, cutTo)
	if !hasQueryTerms(rest) {
		return
	}
	r.out = append(r.out, querySplit{spec: spec, query: rest, strict: strict, from: from, to: to,
		common: !strict && isCommonWord(name)})
}

// nameLike reports a query word that can be a library's name: a bare
// name of two or more characters that is not filler, an article, a
// pronoun, a language word, a number or an API identifier.
func nameLike(w string) bool {
	c := cleanQueryWord(w)
	k := queryWordKey(w)
	switch {
	case len(c) < 2 || isBareInt(c), !bareNameRe.MatchString(c):
		return false
	case isQueryFiller(c), queryDeterminers[k], queryPronouns[k], languageWords[k], languageWords[strings.ToLower(trimJSSuffix(c))]:
		return false
	case apiIdentRe.MatchString(c):
		return false
	}
	return true
}

// hyphenJoin is the first two words from start joined with "-" ("react
// router" is react-router), when both are plain words that name something.
func hyphenJoin(words []string, start int) (string, bool) {
	if len(words)-start < 2 {
		return "", false
	}
	a, b := strings.ToLower(cleanQueryWord(words[start])), strings.ToLower(cleanQueryWord(words[start+1]))
	ok := plainWordRe.MatchString(a) && plainWordRe.MatchString(b) && !isQueryFiller(b) && !linkWords[b]
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
	k := queryWordKey(w)
	return webStopwords[k] || webStopwords[stem(k)] || queryFiller[k]
}

// queryFiller are words a question opens with before it names anything,
// beside webStopwords: question words, auxiliaries and pronouns.
var queryFiller = map[string]bool{
	"why": true, "when": true, "where": true, "which": true, "who": true, "whats": true, "hows": true,
	"does": true, "did": true, "should": true, "would": true, "could": true, "will": true, "shall": true,
	"am": true, "was": true, "were": true, "there": true, "way": true, "any": true, "some": true,
	"me": true, "we": true, "you": true, "use": true, "using": true, "need": true, "want": true, "please": true,
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
