package docs

import (
	"cmp"
	"math"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// webStopwords are dropped from queries (never from documents): they carry
// no topic and would otherwise favour long prose sections.
var webStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "to": true, "in": true, "on": true, "for": true,
	"and": true, "or": true, "is": true, "are": true, "be": true, "how": true, "do": true, "doe": true,
	"what": true, "with": true, "my": true, "can": true, "i": true, "it": true, "by": true, "from": true,
}

// rankStopwords are function words dropped from a query for ranking only,
// beside webStopwords (which name resolution shares): in a corpus of a few
// dozen sections a word like "between" is rare enough that its weight
// would decide the ranking, though it says nothing about the topic.
var rankStopwords = map[string]bool{
	"about": true, "after": true, "all": true, "also": true, "any": true, "at": true, "before": true, "between": true,
	"but": true, "doe": true, "each": true, "if": true, "into": true, "its": true, "me": true, "not": true,
	"should": true, "so": true, "some": true, "than": true, "that": true, "their": true, "them": true, "then": true,
	"there": true, "thi": true, "these": true, "they": true, "those": true, "through": true, "via": true, "vs": true,
	"versu": true, "wa": true, "way": true, "we": true, "when": true, "where": true, "which": true, "while": true,
	"who": true, "why": true, "will": true, "within": true, "without": true, "would": true, "you": true, "your": true,
}

// tokenize lower-cases s, splits it on anything but letters and digits, and
// applies a light plural stemmer so "collections" matches "collection".
// Name resolution (web_spec.go) reads its output; ranking normalizes it
// further with rankTokens.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, f := range fields {
		fields[i] = stem(f)
	}
	return fields
}

func stem(t string) string {
	switch {
	case len(t) > 4 && strings.HasSuffix(t, "ies"):
		return t[:len(t)-3] + "y"
	case len(t) > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss") && !strings.HasSuffix(t, "us"):
		return t[:len(t)-1]
	}
	return t
}

// rankTokens is tokenize with each token normalized by normTerm: the form
// candidate picking and section ranking compare.
func rankTokens(s string) []string {
	toks := tokenize(s)
	for i, t := range toks {
		toks[i] = normTerm(t)
	}
	return toks
}

// textTokens is rankTokens for a section's body: an identifier written in
// camel case also yields its words (PersistentFlags: persistentflag,
// persistent, flag; bearerAuth: bearerauth, bearer, auth), so prose words
// meet the API names that spell them. Snake case already splits.
func textTokens(s string) []string {
	return appendCamelWords(rankTokens(s), s)
}

// qualifierRe matches what qualifies an API heading's name rather than
// naming it: a Go method's receiver, "func (*SugaredLogger) ", and the
// owner in a qualified name, "SugaredLogger." or "tokio::task::".
var qualifierRe = regexp.MustCompile(`^func\s*\([^)]*\)\s*|[A-Za-z_$][\w$]*(?:\.|::|#|->)`)

// headingTokens is rankTokens for a heading, with the camel-case words of
// what the heading names: "func (*Command) PersistentFlags" gives
// persistent and flag, but "func (*SugaredLogger) Debugln" does not give
// sugared and logger, which name the type the method belongs to; every
// method of a type would otherwise read as about the type.
func headingTokens(s string) []string {
	return appendCamelWords(rankTokens(s), qualifierRe.ReplaceAllString(s, " "))
}

func appendCamelWords(toks []string, s string) []string {
	for _, w := range camelWords(s) {
		for _, part := range w {
			toks = append(toks, normTerm(stem(part)))
		}
	}
	return toks
}

// camelWords returns the words of each camel-case identifier in s, lower
// case: getServerSideProps → get, server, side, props; HTTPServer → http,
// server. Words without an inner capital give none.
func camelWords(s string) [][]string {
	var out [][]string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if parts := camelSplit(f); len(parts) > 1 {
			out = append(out, parts)
		}
	}
	return out
}

// camelSplit splits one word at its case changes, lower-casing the parts.
func camelSplit(w string) []string {
	rs := []rune(w)
	var parts []string
	start := 0
	for i := 1; i < len(rs); i++ {
		lowerToUpper := unicode.IsLower(rs[i-1]) && unicode.IsUpper(rs[i])
		acronymEnd := i+1 < len(rs) && unicode.IsUpper(rs[i-1]) && unicode.IsUpper(rs[i]) && unicode.IsLower(rs[i+1])
		if lowerToUpper || acronymEnd {
			parts = append(parts, strings.ToLower(string(rs[start:i])))
			start = i
		}
	}
	return append(parts, strings.ToLower(string(rs[start:])))
}

// normTerm folds the inflections tokenize leaves, deterministically and
// only on plain ASCII words, so a query and a page that name one thing
// in different forms meet: -ing and -ed (loading, loaded → load; a doubled
// final consonant undone, mapping → map), the -ize family (memoize,
// memoized, memoization → memo), -ation and -ional (configuration →
// configur, conditionally → conditional → condition) and a final e
// (cache, caching → cach; configure, configured → configur). Short words are left alone ("string",
// "based", "size"); what the rules miss, prefixMatch may still catch in
// candidate picking.
func normTerm(t string) string {
	for i := 0; i < len(t); i++ {
		if t[i] < 'a' || t[i] > 'z' {
			return t
		}
	}
	if full, ok := abbreviations[t]; ok {
		return full
	}
	t = trimSuffixRule(t, adverbs)
	t = trimSuffixRule(t, inflections)
	t = trimSuffixRule(t, izeSuffixes)
	t = trimSuffixRule(t, nominals)
	if len(t) >= 5 && t[len(t)-1] == 'e' {
		t = t[:len(t)-1]
	}
	return t
}

// abbreviations are the short forms docs and queries use for the same
// word, mapped to the long form's normTerm: QueryParams and "query
// parameters", "config" and "configuration".
var abbreviations = map[string]string{
	"param": "parameter", "arg": "argument", "config": "configur", "env": "environment",
	"dir": "directory", "repo": "repository", "info": "inform",
}

// suffixRule strips suffix from a word of at least minLen letters; vowel
// requires a vowel left in the stem, and undouble undoes a doubled final
// consonant after.
type suffixRule struct {
	suffix   string
	minLen   int
	vowel    bool
	undouble bool
	unless   string // a longer suffix that keeps the word whole ("eed": need, speed)
	keep     string // what of the suffix stays ("al" of -ally, "ion" of -ional)
}

var (
	inflections = []suffixRule{
		{suffix: "ing", minLen: 7, vowel: true, undouble: true},
		{suffix: "ed", minLen: 6, vowel: true, undouble: true, unless: "eed"},
	}
	// adverbs fold an -ally adverb into its adjective (conditionally →
	// conditional), which nominals may fold further.
	adverbs = []suffixRule{
		{suffix: "ally", minLen: 9, vowel: true, keep: "al"},
	}
	// nominals fold a noun or adjective in -ation or -ional into the word
	// it is made from: configuration and configure meet (configur),
	// conditional and condition, optional and option.
	nominals = []suffixRule{
		{suffix: "ation", minLen: 9, vowel: true},
		{suffix: "ional", minLen: 8, vowel: true, keep: "ion"},
	}
	izeSuffixes = []suffixRule{
		{suffix: "ization", minLen: 11},
		{suffix: "ize", minLen: 7},
		{suffix: "iz", minLen: 6}, // what -ized and -izing leave
	}
)

// trimSuffixRule applies the first rule that fits t.
func trimSuffixRule(t string, rules []suffixRule) string {
	for _, r := range rules {
		if len(t) < r.minLen || !strings.HasSuffix(t, r.suffix) || r.unless != "" && strings.HasSuffix(t, r.unless) {
			continue
		}
		stem := t[:len(t)-len(r.suffix)]
		if r.vowel && !hasVowel(stem) {
			continue
		}
		if r.undouble {
			stem = undouble(stem)
		}
		return stem + r.keep
	}
	return t
}

func hasVowel(s string) bool { return strings.ContainsAny(s, "aeiouy") }

// undouble drops one of a doubled final consonant (mapp → map), but not
// l, s or z, which double in the base word (install, pass, buzz).
func undouble(s string) string {
	n := len(s)
	if n >= 3 && s[n-1] == s[n-2] && strings.IndexByte("bdfgmnprt", s[n-1]) >= 0 {
		return s[:n-1]
	}
	return s
}

// queryTerms returns the query's distinct terms in order, normalized for
// ranking, without stopwords unless the query is nothing but stopwords.
func queryTerms(query string) []string {
	var all, terms []string
	seen := map[string]bool{}
	for _, t := range tokenize(query) {
		n := normTerm(t)
		if seen[n] {
			continue
		}
		seen[n] = true
		all = append(all, n)
		if !webStopwords[t] && !rankStopwords[t] && (len(t) > 1 || unicode.IsDigit(rune(t[0]))) {
			terms = append(terms, n)
		}
	}
	if len(terms) == 0 {
		return all
	}
	return terms
}

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, t := range rankTokens(s) {
		set[t] = true
	}
	return set
}

// prefixMatch scores a token that extends the term or that the term
// extends, for terms of four letters or more: 1/2 when they share five
// letters or more ("config" ~ "configuration"), 1/4 for four ("load" ~
// "loader", but also "memo" ~ "memory"), else 0.
func prefixMatch(set map[string]bool, term string) float64 {
	if len(term) < 4 {
		return 0
	}
	best := 0.0
	for tok := range set {
		if len(tok) < 4 || !strings.HasPrefix(tok, term) && !strings.HasPrefix(term, tok) {
			continue
		}
		if min(len(tok), len(term)) >= 5 {
			return 0.5
		}
		best = 0.25
	}
	return best
}

// linkFields are a candidate link's words, by field, for linkScore.
type linkFields struct {
	title, path, desc map[string]bool
	titleText, base   string // the title and the last path segment, for the exact-query bonus
}

func newLinkFields(l webLink, root webRoot) linkFields {
	rel := strings.TrimPrefix(l.url.Path, root.prefix)
	rel = strings.TrimSuffix(rel, path.Ext(rel))
	return linkFields{title: tokenSet(l.title), path: tokenSet(rel), desc: tokenSet(l.description), titleText: l.title, base: path.Base(rel)}
}

// matches reports whether term matches any of the link's fields exactly,
// or, with prefix set, by prefix.
func (f linkFields) matches(term string, prefix bool) bool {
	for _, set := range []map[string]bool{f.title, f.path, f.desc} {
		if set[term] || prefix && prefixMatch(set, term) > 0 {
			return true
		}
	}
	return false
}

// linkScore is the cheap pre-fetch relevance of a candidate: per query term,
// a title hit counts 3, a path hit 2 and a description hit 1 (prefix hits
// less, see prefixMatch), times the term's weight; plus 2 when every term of a multi-term
// query matches some field exactly (a prefix is a guess: "memo" in
// "memory"), and 3 when the title or last path segment is exactly the
// query.
func linkScore(f linkFields, terms []string, weights []float64) float64 {
	fields := []struct {
		set    map[string]bool
		weight float64
	}{{f.title, 3}, {f.path, 2}, {f.desc, 1}}
	score, matched := 0.0, 0
	for i, t := range terms {
		s, exact := 0.0, false
		for _, fl := range fields {
			if fl.set[t] {
				s += fl.weight
				exact = true
			} else {
				s += fl.weight * prefixMatch(fl.set, t)
			}
		}
		if exact {
			matched++
		}
		score += s * weights[i]
	}
	if len(terms) > 1 && matched == len(terms) {
		score += 2
	}
	// A page whose title or last path segment is exactly the query
	// (/guides/content-collections for "content collections") is the
	// topic's own page, not one that mentions it.
	if exactTerms(f.titleText, terms) || exactTerms(f.base, terms) {
		score += 3
	}
	return score
}

// termWeights weighs each query term by how rare it is among the links
// (inverse document frequency), scaled so the rarest matching term weighs
// 1. A word every page carries ("component" across React's docs) then
// counts for less than the one that names the topic ("memo"). Only a
// clear difference counts: a term in fewer than webRareRatio times as
// many links as the rarest keeps weight 1, so terms about as common
// (Astro's "image" and "component") keep the site's order between them.
func termWeights(fields []linkFields, terms []string) []float64 {
	n := float64(len(fields))
	df := make([]float64, len(terms))
	rarest := 0.0
	for i, t := range terms {
		for _, prefix := range []bool{false, true} {
			for _, f := range fields {
				if f.matches(t, prefix) {
					df[i]++
				}
			}
			if df[i] > 0 {
				break // a word's rarity is the word's; prefixes only when it is nowhere
			}
		}
		if df[i] > 0 && (rarest == 0 || df[i] < rarest) {
			rarest = df[i]
		}
	}
	idf := func(d float64) float64 { return math.Log(1 + (n-d+0.5)/(d+0.5)) }
	weights := make([]float64, len(terms))
	for i := range terms {
		weights[i] = 1
		if df[i] >= webRareRatio*rarest && rarest > 0 {
			weights[i] = idf(df[i]) / idf(rarest)
		}
	}
	return weights
}

// webRareRatio is how many times more links a term must be in than the
// rarest query term before termWeights discounts it.
const webRareRatio = 3

// exactTerms reports whether text, stopwords aside, is the query's terms
// and nothing else.
func exactTerms(text string, terms []string) bool {
	set := map[string]bool{}
	for _, t := range tokenize(text) {
		if !webStopwords[t] {
			set[normTerm(t)] = true
		}
	}
	if len(set) != len(terms) {
		return false
	}
	for _, t := range terms {
		if !set[t] {
			return false
		}
	}
	return true
}

type scoredLink struct {
	link  webLink
	score float64
}

// pickCandidates returns up to n links by linkScore, best first, ties in
// discovery order, then the root page when fewer matched, with locale and
// version variants collapsed and pages that are never answers
// (isResultLink) left out. When nothing matches (opaque URLs, no titles)
// it falls back to the first few links, the ones a site lists first.
func pickCandidates(links []webLink, query string, root webRoot, n int) []webLink {
	terms := queryTerms(query)
	eligible := make([]webLink, 0, len(links))
	for _, l := range links {
		if isResultLink(l.url) {
			eligible = append(eligible, l)
		}
	}
	fields := make([]linkFields, len(eligible))
	for i, l := range eligible {
		fields[i] = newLinkFields(l, root)
	}
	weights := termWeights(fields, terms)
	ranked := make([]scoredLink, 0, len(eligible))
	for i, l := range eligible {
		if s := linkScore(fields[i], terms, weights); s > 0 {
			ranked = append(ranked, scoredLink{l, s})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	// With fetches to spare, the root page comes last: an overview often
	// covers what the item pages' titles do not name (docs.rs's crate page
	// has the JSON section reqwest's Body and RequestBuilder titles hide).
	if len(ranked) > 0 && len(ranked) < n {
		isRoot := func(l webLink) bool { return linkKey(l.url) == linkKey(root.u) }
		if i := slices.IndexFunc(eligible, isRoot); i >= 0 && !slices.ContainsFunc(ranked, func(r scoredLink) bool { return isRoot(r.link) }) {
			ranked = append(ranked, scoredLink{link: eligible[i]})
		}
	}
	if len(ranked) == 0 {
		for _, l := range eligible {
			ranked = append(ranked, scoredLink{link: l})
		}
		n = min(n, webFallbackFetch)
	}
	ranked = collapseVariants(ranked, root)
	out := make([]webLink, 0, min(n, len(ranked)))
	for _, r := range ranked[:min(max(n, 1), len(ranked))] {
		out = append(out, r.link)
	}
	return out
}

// sourceListingDirs are path segments under which generators publish
// source code and machine-readable copies rather than docs pages: Sphinx's
// viewcode (_modules), its reST sources (_sources) and static files.
var sourceListingDirs = map[string]bool{"_modules": true, "_sources": true, "_static": true, "_images": true, "_downloads": true}

// isResultLink reports a link worth fetching as an answer. Source-code
// listings (Sphinx's _modules/ viewcode pages, docs.rs /src/ and /source/
// views) and index files (llms-style .txt sets, Sphinx's genindex and
// py-modindex) are not: an answer should be the docs page that explains
// the thing. Discovery may still read them for links.
func isResultLink(u *url.URL) bool {
	segs := pathSegments(u.Path)
	for _, s := range segs {
		if sourceListingDirs[s] {
			return false
		}
	}
	if scopeHost(u) == "docs.rs" && isDocsRSSource(segs) {
		return false
	}
	base := strings.ToLower(path.Base(u.Path))
	if strings.HasSuffix(base, ".txt") {
		return false
	}
	switch strings.TrimSuffix(base, path.Ext(base)) {
	case "genindex", "py-modindex", "modindex":
		return false
	}
	return !strings.HasPrefix(base, "genindex-")
}

// isDocsRSSource reports a docs.rs source view: /<crate>/<version>/src/…
// or /crate/<crate>/<version>/source/…, whatever the crate is called.
func isDocsRSSource(segs []string) bool {
	return len(segs) > 2 && segs[2] == "src" || len(segs) > 3 && segs[0] == "crate" && segs[3] == "source"
}

// localeRe matches a locale path segment: en, pt-br, zh-hans, en_US.
var localeRe = regexp.MustCompile(`(?i)^[a-z]{2}(?:[-_][a-z]{2,4})?$`)

// variantDepth is how many path segments below the root collapseVariants
// looks through for locale and version segments: /docs/29.7/timer-mocks
// and /en/5x/guide/x put them second or third.
const variantDepth = 3

// collapseVariants keeps one page per path once locale and version
// segments are set aside: sitemaps list /en/x, /es/x, /ja/x… and
// /docs/2.x/x, /docs/3.1/x beside /docs/x, and fetching one page six times
// would waste every fetch. A group sits at its best score, and the variant
// that represents it is the one variantRank prefers: English (or no
// locale), then the root's own version, then the current one. A root that
// already names a locale or version (/en/, /v2/) never collapses those,
// since its prefix is stripped first.
func collapseVariants(ranked []scoredLink, root webRoot) []scoredLink {
	rootLabel := rootVersionLabel(root)
	out := ranked[:0:0]
	pos := map[string]int{}
	pref := map[int]variantPref{}
	for _, r := range ranked {
		key, p := variantKey(r.link.url, root, rootLabel)
		i, seen := pos[key]
		switch {
		case !seen:
			pos[key], pref[len(out)] = len(out), p
			out = append(out, r)
		case p.better(pref[i]):
			out[i] = scoredLink{link: r.link, score: math.Max(out[i].score, r.score)}
			pref[i] = p
		}
	}
	return out
}

// variantPref is how much collapseVariants wants a variant: lower is
// better, compared locale first, then version class, then release order.
type variantPref struct {
	locale  int    // 0 English or none, 1 another locale
	class   int    // 0 the root's version, 1 none (current), 2 latest/stable, 3 a release, 4 a pre-release, 5 dev/next
	release []int  // the release's numbers, newest preferred
	label   string // the version label
}

func (a variantPref) better(b variantPref) bool {
	if a.locale != b.locale {
		return a.locale < b.locale
	}
	if a.class != b.class {
		return a.class < b.class
	}
	if c := compareRelease(a.release, b.release); c != 0 {
		return c > 0
	}
	return false
}

// variantKey returns u's path below the root with up to two locale or
// version segments among its first variantDepth removed (a locale only
// leading), and how much the variant is wanted.
func variantKey(u *url.URL, root webRoot, rootLabel string) (string, variantPref) {
	key, _, p := variantParts(u, root, rootLabel)
	return key, p
}

// variantParts is variantKey that also returns the locale and version
// segments it set aside, lower case, in path order: the variant tree the
// page belongs to (es, v2, en/5x), empty for the unmarked tree.
func variantParts(u *url.URL, root webRoot, rootLabel string) (string, []string, variantPref) {
	segs := pathSegments(strings.TrimPrefix(u.Path, root.prefix))
	p := variantPref{class: 1}
	// The last segment names the page, unless the path ends in a slash and
	// every segment before it was set aside too (/stable/, /en/5x/: a
	// version's landing page). /tutorial/1-setup/2/ and
	// /guides/upgrade-to/v5/ are pages of their own.
	last := len(segs) - 1
	slash := strings.HasSuffix(u.Path, "/")
	removed := 0
	kept := segs[:0:0]
	var tree []string
	for i, seg := range segs {
		if slash && i == len(segs)-1 && removed == i {
			last = len(segs)
		}
		if removed < 2 && i < variantDepth && i < last {
			if label, ok := variantLabel(seg); ok {
				p.class, p.release, p.label = versionClass(label, rootLabel)
				tree = append(tree, label)
				removed++
				continue
			}
			if localeRe.MatchString(seg) && i == removed {
				if !strings.EqualFold(seg[:2], "en") {
					p.locale = 1
				}
				tree = append(tree, strings.ToLower(seg))
				removed++
				continue
			}
		}
		kept = append(kept, seg)
	}
	// x.html is x: a sitemap's .html address of a page llms.txt names
	// without it (router.vuejs.org/zh/guide/….html beside /guide/…).
	return strings.TrimSuffix(strings.Join(kept, "/"), ".html"), tree, p
}

// variantLabel reports a path segment that names a version: one of
// versionLabelRe's labels (web_version.go), but no bare number of three
// or more digits, which is more likely an id than a release.
func variantLabel(seg string) (string, bool) {
	if !versionLabelRe.MatchString(seg) {
		return "", false
	}
	if strings.Trim(seg, "0123456789") == "" && len(seg) > 2 {
		return "", false
	}
	return strings.ToLower(seg), true
}

// versionClass ranks a version label for collapseVariants.
func versionClass(label, rootLabel string) (class int, release []int, _ string) {
	switch {
	case rootLabel != "" && strings.EqualFold(strings.TrimPrefix(label, "v"), strings.TrimPrefix(rootLabel, "v")):
		return 0, nil, label
	case label == "latest" || label == "stable" || label == "current" || label == "lts":
		return 2, nil, label
	}
	nums, pre := releaseNumbers(label)
	switch {
	case nums == nil:
		return 5, nil, label // next, main, dev, canary…
	case pre:
		return 4, nums, label
	}
	return 3, nums, label
}

// releaseNumbers parses "v3.1.2", "2.x", "4x" into their numbers (x as
// none) and reports a pre-release suffix (rc, beta, alpha).
func releaseNumbers(label string) ([]int, bool) {
	s := strings.TrimPrefix(label, "v")
	var nums []int
	for s != "" {
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 {
			break
		}
		n := 0
		for _, c := range s[:i] {
			n = n*10 + int(c-'0')
		}
		nums = append(nums, n)
		s = s[i:]
		if !strings.HasPrefix(s, ".") || len(s) < 2 || s[1] < '0' || s[1] > '9' {
			break
		}
		s = s[1:]
	}
	s = strings.TrimLeft(s, ".")
	return nums, s != "" && s != "x"
}

// compareRelease orders release numbers: 30.4 after 30.0 after 29.7.
func compareRelease(a, b []int) int {
	for i := range max(len(a), len(b)) {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return cmp.Compare(x, y)
		}
	}
	return 0
}

// rootVersionLabel is the version the root itself names, in its path or
// its host (v2.vuejs.org), lower case; "" when it names none.
func rootVersionLabel(root webRoot) string {
	if root.u == nil {
		return ""
	}
	for _, s := range pathSegments(root.prefix) {
		if l, ok := variantLabel(s); ok {
			return l
		}
	}
	if first, _, ok := strings.Cut(scopeHost(root.u), "."); ok {
		if l, ok := variantLabel(first); ok && strings.ContainsAny(l, "0123456789") {
			return l
		}
	}
	return ""
}

// sectionBoost is what multiplies a section's keyword score beyond BM25:
// the share of the query's terms it names (matched of them), a heading or
// page title that is exactly the query, a page lead whose title covers
// it, an API entry the query names, the query's terms in its code, and
// the query's words side by side in it (seq: its heading then body
// tokens, rankTokens).
func sectionBoost(s webSection, terms, idents []string, howTo bool, matched float64, seq []string) float64 {
	b := 0.5 + 0.5*matched/float64(len(terms))
	// A section, or a page, titled exactly the query is about it; BM25
	// alone favours short pages that merely mention every term.
	switch {
	case exactTerms(s.heading, terms):
		b *= 2
	case len(s.crumbs) > 0 && exactTerms(s.crumbs[0], terms):
		b *= 1.25
	}
	if s.anchor == "" && len(s.crumbs) == 1 {
		// A page's lead says what the page is about: the closer its
		// title to the query, the likelier the lead is the answer.
		b *= 1 + webLeadBoost*titleCoverage(s.heading, terms)
	}
	b *= 1 + entryMatch(s.heading, terms, idents, howTo)
	b *= 1 + webCodeBoost*codeCoverage(s.text, terms)
	b *= 1 + webPhraseBoost*phraseShare(seq, terms)
	return b
}

// entryMatch is the boost for a section headed by the API entry a query
// asks for, beyond what BM25 gives it: a section that documents the API
// answers ahead of prose that only mentions it. It is 1 when the entry's
// name is an identifier the query wrote (useEffect in "useEffect cleanup
// function", extraReducers), 1/2 when the name's words are exactly the
// query's terms (PersistentFlags for "persistent flags", waitForLoadState
// for "wait for load state") unless the query asks how to do something,
// which a guide answers better than a reference entry, and 0 otherwise,
// partial matches included: every method named Flags is not about
// "persistent flags".
func entryMatch(heading string, terms, idents []string, howTo bool) float64 {
	name := entryName(heading)
	if name == "" {
		return 0
	}
	joined := identKey(name)
	if slices.Contains(idents, joined) {
		return 1
	}
	if howTo {
		return 0
	}
	parts := map[string]bool{}
	for _, w := range camelSplit(name) {
		for _, t := range tokenize(w) {
			if !webStopwords[t] && !rankStopwords[t] {
				parts[normTerm(t)] = true
			}
		}
	}
	if len(parts) != len(terms) || len(terms) < 2 {
		return 0
	}
	for _, t := range terms {
		if !parts[t] {
			return 0
		}
	}
	return 0.5
}

// identKey is an identifier's form for comparing names: lower case, with
// no underscores, stemmed (extraReducers and extra_reducer meet).
func identKey(name string) string {
	return normTerm(stem(strings.ToLower(strings.ReplaceAll(name, "_", ""))))
}

// queryIdents returns the identifiers a query writes as code: a word with
// an inner capital (useEffect), an underscore (select_related) or a dot
// or call (res.sendFile, vi.mock()), each as identKey of its last name.
func queryIdents(query string) []string {
	var out []string
	for _, w := range strings.Fields(query) {
		w = strings.Trim(w, queryQuotes+",.;:!?")
		if w == "" || !isCamel(w) && !strings.ContainsAny(w, "_.(") {
			continue
		}
		w = qualifierRe.ReplaceAllString(w, "")
		if id := entryIdentRe.FindString(w); id != "" {
			out = append(out, identKey(id))
		}
	}
	return out
}

// isHowTo reports a query phrased as a how-to question.
func isHowTo(query string) bool {
	return slices.Contains(tokenize(query), "how")
}

// webPhraseBoost is the most adjacency can raise a section's score: when
// every pair of consecutive query terms appears together in it.
const webPhraseBoost = 0.25

// phraseShare is the share of consecutive query-term pairs ("share state",
// "url prefix") that a section's heading or body (seq) has within two words of
// each other, in order: terms that occur together are about one thing,
// where scattered occurrences may not be.
func phraseShare(seq, terms []string) float64 {
	if len(terms) < 2 {
		return 0
	}
	pos := map[string][]int{}
	for j, t := range seq {
		pos[t] = append(pos[t], j)
	}
	n := 0
	for k := 1; k < len(terms); k++ {
		a, b := pos[terms[k-1]], pos[terms[k]]
		found := false
		for _, x := range a {
			for _, y := range b {
				if y > x && y-x <= 2 {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			n++
		}
	}
	return float64(n) / float64(len(terms)-1)
}

// webLeadBoost is the most a page's lead section gains from a title that
// names every query term.
const webLeadBoost = 0.3

// titleCoverage is the share of query terms a title names.
func titleCoverage(title string, terms []string) float64 {
	set := tokenSet(title)
	n := 0
	for _, t := range terms {
		if set[t] {
			n++
		}
	}
	return float64(n) / float64(len(terms))
}

// webCodeBoost is the most a section's code can raise its score: when its
// code blocks hold every query term.
const webCodeBoost = 0.2

// codeCoverage is the share of query terms that appear in the section's
// fenced code: a section that shows the API at work answers ahead of one
// that only describes it (an import and a usage example ahead of an
// option's one-line description).
func codeCoverage(text string, terms []string) float64 {
	if !strings.Contains(text, "```") && !strings.Contains(text, "~~~") {
		return 0
	}
	var code strings.Builder
	var fence fenceState
	for _, line := range strings.Split(text, "\n") {
		if open := fence.n > 0; fence.step(line) && open && fence.n > 0 {
			code.WriteString(line)
			code.WriteByte('\n')
		}
	}
	toks := map[string]bool{}
	for _, t := range textTokens(code.String()) {
		toks[t] = true
	}
	n := 0
	for _, t := range terms {
		if toks[t] {
			n++
		}
	}
	return float64(n) / float64(len(terms))
}

// entryDeclRe matches the declaration keywords before an API entry's name.
var entryDeclRe = regexp.MustCompile(`^(?:(?:pub(?:\([a-z]+\))?|async|unsafe|const|static|extern|export|default|abstract|final|public|private|protected)\s+)*(?:fn|func|type|def|class|method|function|struct|enum|trait|interface|macro|property|attribute|module|mod|union|var|let)\s+`)

// entryIdentRe matches the identifier an API entry heading starts with.
var entryIdentRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*`)

// entryName returns the name an API entry heading documents, "" for a
// heading that is not one (isAPIEntry's reading, from the text alone):
// "func (*Command) PersistentFlags" → PersistentFlags, "root.render(node)"
// → render, "fn context<C>(…)" → context, "vi.mock" → mock.
func entryName(heading string) string {
	h := strings.TrimSpace(heading)
	if !isAPIEntry(atxHeading{text: h, raw: h}) {
		return ""
	}
	h = qualifierRe.ReplaceAllString(h, "")
	h = entryDeclRe.ReplaceAllString(h, "")
	return entryIdentRe.FindString(strings.TrimSpace(h))
}

// webRelevanceRatio is how far below the top section's score a section
// may fall and still be returned (relevant).
const webRelevanceRatio = 0.3

const truncatedMarker = "\n[truncated]"

// elidedMarker stands where trimForQuery left blocks out.
const elidedMarker = "[…]"

// blocksByTerms returns the indices of the blocks that name any of terms,
// those naming the most first, ties in block order.
func blocksByTerms(blocks, terms []string) []int {
	count := make([]int, len(blocks))
	var out []int
	for i, b := range blocks {
		toks := map[string]bool{}
		for _, t := range textTokens(b) {
			toks[t] = true
		}
		for _, t := range terms {
			if toks[t] {
				count[i]++
			}
		}
		if count[i] > 0 {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return count[out[a]] > count[out[b]] })
	return out
}

// joinKept joins the kept blocks in their order, elidedMarker standing
// where blocks were left out.
func joinKept(blocks []string, keep map[int]bool) string {
	var b strings.Builder
	last := -1
	for i, block := range blocks {
		if !keep[i] {
			continue
		}
		if last >= 0 {
			b.WriteString("\n\n")
			if i > last+1 {
				b.WriteString(elidedMarker + "\n\n")
			}
		}
		b.WriteString(block)
		last = i
	}
	if last < len(blocks)-1 {
		b.WriteString("\n\n" + elidedMarker)
	}
	return b.String()
}

// textBlocks splits Markdown at blank lines outside fenced code, so a
// code block, blank lines and all, is one block.
func textBlocks(s string) []string {
	var blocks []string
	var cur []string
	var fence fenceState
	flush := func() {
		if t := strings.Trim(strings.Join(cur, "\n"), "\n"); strings.TrimSpace(t) != "" {
			blocks = append(blocks, t)
		}
		cur = cur[:0]
	}
	for _, line := range strings.Split(s, "\n") {
		inFence := fence.n > 0
		opens := fence.step(line)
		if !inFence && !opens && strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return blocks
}

// trimSnippet cuts s to at most n bytes including the "[truncated]" marker,
// preferring a line or word boundary and closing a code fence the cut left
// open. It returns "" when n leaves no room for content.
func trimSnippet(s string, n int) string {
	limit := n - len(truncatedMarker)
	for range 3 {
		if limit <= 0 {
			return ""
		}
		cut := cutText(s, limit)
		var fence fenceState
		for _, line := range strings.Split(cut, "\n") {
			fence.step(line)
		}
		closer := ""
		if c := fence.closer(); c != "" {
			closer = "\n" + c
		}
		if cut != "" && len(cut)+len(closer)+len(truncatedMarker) <= n {
			return cut + closer + truncatedMarker
		}
		limit = n - len(truncatedMarker) - len(closer) - 1
	}
	return ""
}

// cutText returns at most n bytes of s, on a rune boundary, backed off to
// the last line break (or space) in the second half of the cut.
func cutText(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, '\n'); i > len(cut)/2 {
		cut = cut[:i]
	} else if i := strings.LastIndexByte(cut, ' '); i > len(cut)/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \t\n")
}
