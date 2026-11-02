package docs

import (
	"math"
	"net/url"
	"path"
	"regexp"
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

// queryTerms returns the query's distinct terms in order, without
// stopwords unless the query is nothing but stopwords.
func queryTerms(query string) []string {
	var all, terms []string
	seen := map[string]bool{}
	for _, t := range tokenize(query) {
		if seen[t] {
			continue
		}
		seen[t] = true
		all = append(all, t)
		if !webStopwords[t] && (len(t) > 1 || unicode.IsDigit(rune(t[0]))) {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return all
	}
	return terms
}

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, t := range tokenize(s) {
		set[t] = true
	}
	return set
}

// prefixMatch reports a token that extends the term or that the term
// extends ("config" ~ "configuration"), for terms of four letters or more.
func prefixMatch(set map[string]bool, term string) bool {
	if len(term) < 4 {
		return false
	}
	for tok := range set {
		if len(tok) >= 4 && (strings.HasPrefix(tok, term) || strings.HasPrefix(term, tok)) {
			return true
		}
	}
	return false
}

// linkScore is the cheap pre-fetch relevance of a candidate: per query term,
// a title hit counts 3, a path hit 2 and a description hit 1 (prefix hits
// half), plus 2 when a multi-term query is covered entirely and 3 when the
// title or last path segment is exactly the query.
func linkScore(l webLink, terms []string, root webRoot) float64 {
	rel := strings.TrimPrefix(l.url.Path, root.prefix)
	rel = strings.TrimSuffix(rel, path.Ext(rel))
	fields := []struct {
		set    map[string]bool
		weight float64
	}{{tokenSet(l.title), 3}, {tokenSet(rel), 2}, {tokenSet(l.description), 1}}
	score, matched := 0.0, 0
	for _, t := range terms {
		s := 0.0
		for _, f := range fields {
			switch {
			case f.set[t]:
				s += f.weight
			case prefixMatch(f.set, t):
				s += f.weight / 2
			}
		}
		if s > 0 {
			matched++
		}
		score += s
	}
	if len(terms) > 1 && matched == len(terms) {
		score += 2
	}
	// A page whose title or last path segment is exactly the query
	// (/guides/content-collections for "content collections") is the
	// topic's own page, not one that mentions it.
	if exactTerms(l.title, terms) || exactTerms(path.Base(rel), terms) {
		score += 3
	}
	return score
}

// exactTerms reports whether text, stopwords aside, is the query's terms
// and nothing else.
func exactTerms(text string, terms []string) bool {
	set := map[string]bool{}
	for _, t := range tokenize(text) {
		if !webStopwords[t] {
			set[t] = true
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
// discovery order, with locale and version variants collapsed. When nothing matches
// (opaque URLs, no titles) it falls back to the first few links, the ones a
// site lists first.
func pickCandidates(links []webLink, query string, root webRoot, n int) []webLink {
	terms := queryTerms(query)
	ranked := make([]scoredLink, 0, len(links))
	for _, l := range links {
		if s := linkScore(l, terms, root); s > 0 {
			ranked = append(ranked, scoredLink{l, s})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) == 0 {
		for _, l := range links {
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

var (
	// localeRe matches a locale path segment: en, pt-br, zh-hans, en_US.
	localeRe = regexp.MustCompile(`(?i)^[a-z]{2}(?:[-_][a-z]{2,4})?$`)
	// versionRe matches a docs version path segment: 2.x, 3.1.1, v4, next.
	versionRe = regexp.MustCompile(`(?i)^(?:v?\d+(?:\.(?:\d+|x))*|next|latest|stable)$`)
)

// collapseVariants keeps one page per path once leading locale and version
// segments are set aside: sitemaps list /en/x, /es/x, /ja/x… and /docs/2.x/x,
// /docs/3.1/x beside /docs/x, and fetching one page six times would waste
// every fetch. Within a group the best score wins; on a tie the current
// (unversioned) English or unprefixed variant beats the first-listed one.
// A root that already names a locale or version (/en/, /v2/) never
// collapses those, since its prefix is stripped first.
func collapseVariants(ranked []scoredLink, root webRoot) []scoredLink {
	out := ranked[:0:0]
	pos := map[string]int{}
	preferred := map[int]bool{}
	for _, r := range ranked {
		key, pref := variantKey(r.link.url, root)
		i, seen := pos[key]
		switch {
		case !seen:
			pos[key], preferred[len(out)] = len(out), pref
			out = append(out, r)
		case pref && !preferred[i] && r.score == out[i].score:
			out[i], preferred[i] = r, true
		}
	}
	return out
}

// variantKey returns u's path below the root with up to two leading locale
// or version segments removed, and whether the page is the preferred
// variant: no version segment, and no locale or an English one.
func variantKey(u *url.URL, root webRoot) (string, bool) {
	rel := strings.Trim(strings.TrimPrefix(u.Path, root.prefix), "/")
	preferred := true
	for range 2 {
		seg, rest, ok := strings.Cut(rel, "/")
		if !ok {
			break
		}
		switch {
		case versionRe.MatchString(seg):
			preferred = false
		case localeRe.MatchString(seg):
			preferred = preferred && strings.EqualFold(seg[:2], "en")
		default:
			return rel, preferred
		}
		rel = rest
	}
	return rel, preferred
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
// the section or its page is titled exactly the query) and returns
// those that match at all, best first. Ties keep page order, then section
// order, so equal input always ranks the same. A query with no terms keeps
// every section in page order.
func rankSections(sections []webSection, query string) []scoredSection {
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
		if matched == 0 {
			continue
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
	length []float64            // body length in tokens per section
	df     map[string]int       // sections containing each query term
	avg    float64              // mean body length
}

func newSectionCorpus(sections []webSection, terms []string) sectionCorpus {
	c := sectionCorpus{tf: make([]map[string]float64, len(sections)), length: make([]float64, len(sections)), df: map[string]int{}}
	total := 0.0
	for i, s := range sections {
		tf := map[string]float64{}
		body := tokenize(s.text)
		for _, t := range body {
			tf[t]++
		}
		for _, t := range tokenize(s.heading) {
			tf[t] += headingWeight
		}
		for _, crumb := range s.crumbs[:max(len(s.crumbs)-1, 0)] {
			for _, t := range tokenize(crumb) {
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
