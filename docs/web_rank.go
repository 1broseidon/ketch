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

// normTerm folds the inflections tokenize leaves, deterministically and
// only on plain ASCII words, so a query and a page that name one thing
// in different forms meet: -ing and -ed (loading, loaded → load; a doubled
// final consonant undone, mapping → map), the -ize family (memoize,
// memoized, memoization → memo) and a final e (cache, caching → cach;
// configure, configured → configur). Short words are left alone ("string",
// "based", "size"); what the rules miss, prefixMatch may still catch in
// candidate picking.
func normTerm(t string) string {
	for i := 0; i < len(t); i++ {
		if t[i] < 'a' || t[i] > 'z' {
			return t
		}
	}
	t = trimSuffixRule(t, inflections)
	t = trimSuffixRule(t, izeSuffixes)
	if len(t) >= 5 && t[len(t)-1] == 'e' {
		t = t[:len(t)-1]
	}
	return t
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
}

var (
	inflections = []suffixRule{
		{suffix: "ing", minLen: 7, vowel: true, undouble: true},
		{suffix: "ed", minLen: 6, vowel: true, undouble: true, unless: "eed"},
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
		return stem
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
		if !webStopwords[t] && (len(t) > 1 || unicode.IsDigit(rune(t[0]))) {
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
	segs := pathSegments(strings.TrimPrefix(u.Path, root.prefix))
	p := variantPref{class: 1}
	// The last segment names the page, unless the path ends in a slash
	// (/stable/, a version's landing page).
	last := len(segs) - 1
	if strings.HasSuffix(u.Path, "/") {
		last = len(segs)
	}
	removed := 0
	kept := segs[:0:0]
	for i, seg := range segs {
		if removed < 2 && i < variantDepth && i < last {
			if label, ok := variantLabel(seg); ok {
				p.class, p.release, p.label = versionClass(label, rootLabel)
				removed++
				continue
			}
			if localeRe.MatchString(seg) && i == removed {
				if !strings.EqualFold(seg[:2], "en") {
					p.locale = 1
				}
				removed++
				continue
			}
		}
		kept = append(kept, seg)
	}
	return strings.Join(kept, "/"), p
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

// BM25 parameters and field weights. A section's heading counts three times
// its body and its breadcrumb ancestors (page title, parent headings) once,
// so a section titled with the query outranks one that merely mentions it.
const (
	bm25K1        = 1.2
	bm25B         = 0.75
	headingWeight = 3.0
	crumbWeight   = 1.0
)

type scoredSection struct {
	webSection
	score float64
}

// rankSections scores sections against the query with BM25 (heading-boosted
// term frequencies, scaled by the share of query terms matched, boosted when
// the section or its page is titled exactly the query) and returns those
// that match in their own heading or text, best first. Ties keep page
// order, then section order, so equal input always ranks the same. A query
// with no terms keeps every section in page order. Anchors are first
// corrected by the ids the pages' links use (adoptLinkedAnchors).
func rankSections(sections []webSection, query string) []scoredSection {
	sections = adoptLinkedAnchors(sections)
	terms := queryTerms(query)
	if len(terms) == 0 {
		out := make([]scoredSection, len(sections))
		for i, s := range sections {
			out[i] = scoredSection{webSection: s}
		}
		return out
	}
	c := newSectionCorpus(sections, terms)
	var out []scoredSection
	for i, s := range sections {
		score, matched := c.bm25(i, terms)
		if matched == 0 || !c.own[i] {
			continue // matched by its breadcrumbs alone: every section of the page would be
		}
		score *= 0.5 + 0.5*float64(matched)/float64(len(terms))
		// A section, or a page, titled exactly the query is about it; BM25
		// alone favours short pages that merely mention every term.
		switch {
		case exactTerms(s.heading, terms):
			score *= 1.5
		case len(s.crumbs) > 0 && exactTerms(s.crumbs[0], terms):
			score *= 1.25
		}
		out = append(out, scoredSection{webSection: s, score: score})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

// sectionCorpus holds the per-query BM25 statistics, built in memory for
// one call and discarded with it.
type sectionCorpus struct {
	tf     []map[string]float64 // heading-weighted term frequency per section
	own    []bool               // a query term in the section's heading or body, not only its crumbs
	length []float64            // body length in tokens per section
	df     map[string]int       // sections containing each query term
	avg    float64              // mean body length
}

func newSectionCorpus(sections []webSection, terms []string) sectionCorpus {
	c := sectionCorpus{tf: make([]map[string]float64, len(sections)), own: make([]bool, len(sections)), length: make([]float64, len(sections)), df: map[string]int{}}
	total := 0.0
	for i, s := range sections {
		tf := map[string]float64{}
		body := rankTokens(s.text)
		for _, t := range body {
			tf[t]++
		}
		for _, t := range rankTokens(s.heading) {
			tf[t] += headingWeight
		}
		c.own[i] = slices.ContainsFunc(terms, func(t string) bool { return tf[t] > 0 })
		for _, crumb := range s.crumbs[:max(len(s.crumbs)-1, 0)] {
			for _, t := range rankTokens(crumb) {
				tf[t] += crumbWeight
			}
		}
		c.tf[i], c.length[i] = tf, float64(len(body))
		total += c.length[i]
		for _, t := range terms {
			if tf[t] > 0 {
				c.df[t]++
			}
		}
	}
	c.avg = math.Max(total/float64(max(len(sections), 1)), 1)
	return c
}

// bm25 scores section i and counts the query terms it contains.
func (c sectionCorpus) bm25(i int, terms []string) (float64, int) {
	n := float64(len(c.tf))
	score, matched := 0.0, 0
	for _, t := range terms {
		tf := c.tf[i][t]
		if tf == 0 {
			continue
		}
		matched++
		df := float64(c.df[t])
		idf := math.Log(1 + (n-df+0.5)/(df+0.5))
		score += idf * tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*c.length[i]/c.avg))
	}
	return score, matched
}

// budgetResults turns ranked sections into results within the token budget
// (see GetDocs): about webCharsPerToken bytes of snippet per token, the last
// snippet trimmed to fit. The first section is always returned, trimmed if
// it alone exceeds the budget; a later one is only trimmed when enough room
// is left for it to be useful.
func budgetResults(library string, ranked []scoredSection, tokens int) []Result {
	if tokens <= 0 {
		tokens = webDefaultTokens
	}
	remaining := math.MaxInt // a budget too large to multiply is no bound at all
	if tokens <= math.MaxInt/webCharsPerToken {
		remaining = tokens * webCharsPerToken
	}
	var out []Result
	for _, s := range ranked {
		snippet := s.text
		if len(snippet) > remaining {
			if len(out) > 0 && remaining < webMinTrimBytes {
				break
			}
			if snippet = trimSnippet(snippet, remaining); snippet == "" {
				break
			}
		}
		remaining -= len(snippet)
		out = append(out, Result{
			Library:    library,
			Title:      s.heading,
			Breadcrumb: strings.Join(s.crumbs, " > "),
			Snippet:    snippet,
			URL:        s.URL(),
			Source:     "web",
		})
		if remaining <= 0 {
			break
		}
	}
	return out
}

const truncatedMarker = "\n[truncated]"

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
