package docs

import (
	"math"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Keyword ranking over the index's full-text table (ADR-0007, amendment
// 5): the default, with no model. Each section's full-text row is its
// heading, its breadcrumb above it, the parts of the identifiers it names
// and its body, every word normalized as the web backend's ranking
// normalizes a query's (tokenize, normTerm, camel-case words), so the
// table and the question meet on the same terms. FTS5's bm25, with the
// heading weighted over the body, picks the candidates; the boosts the
// web backend's ranker applies on top of BM25 (sectionBoost: the share of
// the question's terms named, an exact title, an API entry the question
// names, its terms in code and side by side) order them; families and
// rendering follow (local_rank.go).

// ftsVersion names the normalization of the full-text rows: rows made by
// another version are rebuilt from the stored sections on first use, with
// no fetching (Store.EnsureFTS, which Gather calls). k2: a row is made from
// the section as cleaned for printing (cleanSection), so a no-op escape
// (service\_err) no longer hides a term the printed text shows.
const ftsVersion = "k2"

// ftsTokenizer makes the full-text rows.
type ftsTokenizer struct{}

func (ftsTokenizer) Version() string { return ftsVersion }

// Row is a section's full-text row, made from the section as an answer
// prints it (cleanSection): counts, terms, not_found and the pool then
// read the same text the caller reads.
func (ftsTokenizer) Row(s docsindex.Section) [4]string {
	s = cleanSection(s)
	var crumbs []string
	for _, c := range s.Crumbs[:max(len(s.Crumbs)-1, 0)] {
		crumbs = append(crumbs, rankTokens(c)...)
	}
	return [4]string{
		strings.Join(headingTokens(s.Heading), " "),
		strings.Join(crumbs, " "),
		strings.Join(identParts(s.Heading+"\n"+s.Text), " "),
		strings.Join(rankTokens(s.Text), " "),
	}
}

// OpenIndex opens the local docs index at path with the docs backends'
// full-text normalization: what `docs index` and the local backends use.
func OpenIndex(path string) *docsindex.Store {
	return docsindex.Open(path, ftsTokenizer{})
}

// identRe matches an identifier as code writes it, qualified or not:
// useForm, client_max_body_size, form.validateField, std::fs::read,
// add_generation_prompt.
var identRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*(?:(?:\.|::|->|#)[A-Za-z_$][A-Za-z0-9_$]*)*`)

// identParts are the identifier terms a text names: for every
// identifier written as code (camel case, snake case, or qualified), each
// part's whole name as one term (validateField: validatefield,
// client_max_body_size: clientmaxbodysize) and its camel-case words
// (validate, field). A query naming the identifier, its whole name or its
// words then matches it; snake case's words are body terms already.
func identParts(text string) []string {
	var out []string
	for _, m := range identRe.FindAllString(text, -1) {
		if !isCodeIdent(m) {
			continue
		}
		for _, part := range identSplit(m) {
			if k := identKey(part); len(k) > 1 {
				out = append(out, k)
			}
			if words := camelSplit(part); len(words) > 1 {
				for _, w := range words {
					if t := normTerm(stem(strings.Trim(w, "_$"))); t != "" {
						out = append(out, t)
					}
				}
			}
		}
	}
	return out
}

// isCodeIdent reports a word written as code: an inner capital after a
// lower-case letter, an underscore between letters, or a qualifier.
func isCodeIdent(w string) bool {
	return isCamel(w) || strings.Contains(strings.Trim(w, "_"), "_") || strings.ContainsAny(w, ".#") || strings.Contains(w, "::") || strings.Contains(w, "->")
}

// identSplit splits a qualified identifier into its parts.
func identSplit(w string) []string {
	return strings.FieldsFunc(w, func(r rune) bool { return r == '.' || r == ':' || r == '-' || r == '>' || r == '#' })
}

// queryIdentKeys are the whole-name terms of the identifiers a query
// writes (identParts' form), which the index holds beside their words.
func queryIdentKeys(query string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(query) {
		w = strings.Trim(w, queryQuotes+",;:!?=")
		if !isCodeIdent(w) || isHostName(strings.ToLower(strings.TrimRight(w, "."))) || numberTerm(strings.TrimRight(w, ".")) != "" {
			continue
		}
		for _, m := range identRe.FindAllString(w, -1) {
			for _, part := range identSplit(m) {
				if k := identKey(part); len(k) > 1 && !seen[k] {
					seen[k] = true
					out = append(out, k)
				}
			}
		}
	}
	return out
}

// ftsQuote quotes a term for an FTS5 query.
func ftsQuote(t string) string { return `"` + strings.ReplaceAll(t, `"`, `""`) + `"` }

// ftsTerm is a query term as an FTS5 query: quoted, a host name as the
// phrase of its labels as the rows hold them (rankTokens), and a number
// (numberTerm) as its digits or its groups of three (10,000 is "10 000"
// in the rows).
func ftsTerm(t string) string {
	switch {
	case isHostName(t):
		return ftsQuote(strings.Join(rankTokens(t), " "))
	case numberTerm(t) == t:
		return "(" + ftsQuote(t) + " OR " + ftsQuote(strings.Join(digitGroups(t), " ")) + ")"
	}
	return ftsQuote(t)
}

// ftsMatch is the full-text query for a question's terms: any of them
// (bm25 rewards the sections naming more, and rarer, ones), and the whole
// names of the identifiers it writes.
func ftsMatch(terms, idents []string) string {
	seen := map[string]bool{}
	var parts []string
	for _, t := range append(append([]string{}, terms...), idents...) {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		parts = append(parts, ftsTerm(t))
	}
	return strings.Join(parts, " OR ")
}

const (
	// ftsCandidates is how many full-text hits the boosts reorder.
	ftsCandidates = 200
	// ftsReferencePrior and ftsGuidePrior weigh a section by where its
	// page sits in the site (planTier): an API reference leaf below a
	// guide, which answers how-to questions keywords alone cannot tell
	// apart from the entries that merely name the same words.
	ftsReferencePrior = 0.6
	ftsGuidePrior     = 1.2
	// ftsTitleBoost is the most a section gains from its page's title
	// naming the question's terms (titleCoverage).
	ftsTitleBoost = 0.5
	// ftsPageBoost is the most a section gains from its page's other
	// sections matching too: the sum of the page's candidate scores,
	// relative to the best page's. A page about the question answers
	// it more often than a stray section elsewhere that names its words.
	ftsPageBoost = 1.0
)

// ftsWeights are bm25's column weights (FTSColumns: heading, crumbs,
// ident, body): a term in a section's heading counts three times one in
// its body, as the web backend's BM25F counts it.
var ftsWeights = []float64{3, 1, 1, 1}

// rankQuery is a question as the keyword ranking reads it.
type rankQuery struct {
	query     string
	terms     []string // queryTerms, question filler out
	idents    []string // queryIdents
	identKeys []string // queryIdentKeys
	howTo     bool
	trouble   bool // troubleQuery
}

func newRankQuery(query string) rankQuery {
	return rankQuery{query: query, terms: withoutFiller(queryTerms(query)), idents: queryIdents(query), identKeys: queryIdentKeys(query), howTo: isHowTo(query), trouble: troubleQuery(query)}
}

// questionFiller are words questions are phrased with that say nothing
// of their topic ("how do I get X to work", "what do I need to use"),
// normalized as queryTerms normalizes them. They are dropped from the
// keyword query (unless nothing else is left), where every section that
// says "use" would otherwise match.
var questionFiller = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range []string{"need", "want", "use", "using", "used", "get", "getting", "make", "making",
		"possible", "able", "actually", "instead", "exactly", "happen", "happens", "work", "works", "working", "just",
		"here", "something", "anything", "thing", "things", "one", "ones", "like", "really", "still", "even",
		"correct", "correctly", "right", "proper", "properly", "best", "good", "ways"} {
		for _, t := range tokenize(w) {
			m[normTerm(t)] = true
		}
	}
	return m
}()

func withoutFiller(terms []string) []string {
	var out []string
	for _, t := range terms {
		if !questionFiller[t] {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return terms
	}
	return out
}

// rankedSection is a section (an index into the sections ranked) and
// its score.
type rankedSection struct {
	sec   int
	score float64
}

// rerankHits orders the full-text hits (sections of secs by ID): bm25
// times the web ranker's boosts (sectionBoost), its page's place in the
// site (sitePrior), its page title naming the question, a legacy edition
// of a page (legacyPages) down, a troubleshooting section up for a
// question about a failure, and its page's other matches (pageBoosts). A
// section that matched by its breadcrumb alone, about a later version
// unless the question asks about it, or under a docs platform's
// boilerplate heading, is left out. Scores are relative to the best.
func rerankHits(lib docsindex.Library, secs []docsindex.Section, hits []docsindex.Hit, q rankQuery) []rankedSection {
	at := make(map[int64]int, len(secs))
	for i, s := range secs {
		at[s.ID] = i
	}
	root, rootErr := parseWebRoot(lib.Root)
	legacy := newLegacyPages(secs, q.terms)
	var out []rankedSection
	for _, h := range hits {
		i, ok := at[h.ID]
		if !ok {
			continue
		}
		s := secs[i]
		if s.CrossVersion != "" && !asksAbout(q.query, s.CrossVersion) || boilerplateSection(s.Crumbs) {
			continue
		}
		matched, own, seq := termMatches(s, q.terms)
		if !own {
			continue
		}
		score := h.Score * sectionBoost(webSectionOf(s, i), q.terms, q.idents, q.howTo, matched, seq)
		if rootErr == nil {
			score *= sitePrior(s.Page, root)
		}
		score *= pagePriors(s, q, legacy)
		out = append(out, rankedSection{sec: i, score: score})
	}
	pageBoosts(secs, out)
	sort.SliceStable(out, func(a, b int) bool { return out[a].score > out[b].score })
	if len(out) > 0 && out[0].score > 0 {
		best := out[0].score
		for i := range out {
			out[i].score /= best
		}
	}
	return out
}

// pagePriors is a section's weight by its page: its title naming the
// question, a legacy edition (legacyPages), a troubleshooting section for
// a question about a failure.
func pagePriors(s docsindex.Section, q rankQuery, legacy legacyPages) float64 {
	w := 1.0
	if len(s.Crumbs) > 0 {
		w *= 1 + ftsTitleBoost*titleCoverage(s.Crumbs[0], q.terms)
	}
	if legacy[s.Page] {
		w *= ftsLegacyPrior
	}
	if q.trouble && troubleSection(s) {
		w *= ftsTroubleBoost
	}
	return w
}

// sitePrior is a section's weight by its page's tier (planTier).
func sitePrior(page string, root webRoot) float64 {
	u, err := url.Parse(page)
	if err != nil {
		return 1
	}
	switch planTier(u, root, nil) {
	case tierReference:
		return ftsReferencePrior
	case tierGuide:
		return ftsGuidePrior
	}
	return 1
}

// pageBoosts raises each ranked section by its page's share of the
// matches: the sum of its page's scores over the best page's sum, times
// ftsPageBoost.
func pageBoosts(secs []docsindex.Section, ranked []rankedSection) {
	sum := map[string]float64{}
	for _, r := range ranked {
		sum[secs[r.sec].Page] += r.score
	}
	top := 0.0
	for _, v := range sum {
		top = math.Max(top, v)
	}
	if top <= 0 {
		return
	}
	for i, r := range ranked {
		ranked[i].score *= 1 + ftsPageBoost*sum[secs[r.sec].Page]/top
	}
}

// termMatches counts the query terms a section names (heading, body with
// its identifiers' words, or breadcrumb), whether it names any itself
// (not by its breadcrumb alone), and its heading-then-prose token
// sequence: the words of the identifiers it writes are left out of the
// sequence, so one identifier whose parts spell the question
// (adminapi_read_log for "read logs ... admin api") does not count as the
// question's words side by side.
func termMatches(s docsindex.Section, terms []string) (matched float64, own bool, seq []string) {
	head := headingTokens(s.Heading)
	body := rankTokens(s.Text)
	set := map[string]bool{}
	for _, t := range head {
		set[t] = true
	}
	for _, t := range body {
		set[t] = true
	}
	for _, t := range identParts(s.Text) {
		set[t] = true
	}
	for _, t := range hostTerms(s.Heading + "\n" + s.Text) {
		set[t] = true
	}
	crumbs := map[string]bool{}
	for _, c := range s.Crumbs[:max(len(s.Crumbs)-1, 0)] {
		for _, t := range append(rankTokens(c), hostTerms(c)...) {
			crumbs[t] = true
		}
	}
	for _, t := range terms {
		switch {
		case set[t]:
			matched++
			own = true
		case crumbs[t]:
			matched++
		}
	}
	seq = append(rankTokens(s.Heading), rankTokens(withoutIdents(s.Text))...)
	return matched, own, seq
}

// withoutIdents is text with the identifiers it writes as code
// (isCodeIdent) blanked out: its prose.
func withoutIdents(text string) string {
	return identRe.ReplaceAllStringFunc(text, func(m string) string {
		if isCodeIdent(m) {
			return " "
		}
		return m
	})
}

const (
	// ftsLegacyPrior weighs a section of a legacy page (legacyPages) the
	// question does not ask about: the current page answers first.
	ftsLegacyPrior = 0.6
	// ftsTroubleBoost weighs a troubleshooting section (troubleSection)
	// for a question that describes a failure (troubleQuery).
	ftsTroubleBoost = 1.2
)

// legacyWords are words that mark a page, by its path or its title, as
// an older or other edition of one the site keeps current.
var legacyWords = map[string]bool{"legacy": true, "deprecated": true, "archived": true, "archive": true, "obsolete": true}

// legacyVersionRe matches a path segment naming a page's numbered
// edition: adminapi-v1, auth_v2.
var legacyVersionRe = regexp.MustCompile(`^(.+?)[-_]v(\d+)(?:\.\w+)?$`)

// legacyPages are the pages among a ranking's candidates that are a
// legacy edition the question does not ask about: their path or title
// says legacy or deprecated (variantWords, legacyWords), or their last
// path segment is a numbered edition (x-v1) of another candidate page
// (x, or a higher x-vN in the same directory).
type legacyPages map[string]bool

func newLegacyPages(secs []docsindex.Section, terms []string) legacyPages {
	asked := map[string]bool{}
	for _, t := range terms {
		asked[t] = true
	}
	type family struct {
		plain bool // a page with no edition number
		top   int  // the highest edition number
	}
	families := map[string]*family{} // host, directory and base name → its editions among the pages
	editions := map[string]int{}     // page → its edition number
	keys := map[string]string{}
	out := legacyPages{}
	for _, s := range secs {
		if _, seen := keys[s.Page]; seen {
			continue
		}
		u, err := url.Parse(s.Page)
		if err != nil {
			keys[s.Page] = ""
			continue
		}
		if markedLegacy(u, s.Crumbs, asked) {
			out[s.Page] = true
		}
		k, n := pageEdition(u, asked)
		keys[s.Page], editions[s.Page] = k, n
		f := families[k]
		if f == nil {
			f = &family{}
			families[k] = f
		}
		f.plain = f.plain || n == 0
		f.top = max(f.top, n)
	}
	for page, n := range editions {
		if f := families[keys[page]]; n > 0 && (f.plain || f.top > n) {
			out[page] = true
		}
	}
	return out
}

// markedLegacy reports a page whose path or title (crumbs[0]) says it is
// legacy, deprecated, archived or another variant the question does not
// ask about.
func markedLegacy(u *url.URL, crumbs []string, asked map[string]bool) bool {
	words := tokenize(strings.Join(pathSegments(u.Path), " "))
	if len(crumbs) > 0 {
		words = append(words, tokenize(crumbs[0])...)
	}
	for _, w := range words {
		if (legacyWords[w] || variantWords[w]) && !asked[normTerm(w)] {
			return true
		}
	}
	return false
}

// pageEdition is a page's family (host, directory and last segment
// without its edition) and its edition number: 1 for adminapi-v1, 0 for
// adminapi or a page whose edition the question asks about.
func pageEdition(u *url.URL, asked map[string]bool) (string, int) {
	segs := pathSegments(u.Path)
	last := ""
	if len(segs) > 0 {
		last = strings.ToLower(segs[len(segs)-1])
		segs = segs[:len(segs)-1]
	}
	base, n := strings.TrimSuffix(last, path.Ext(last)), 0
	if m := legacyVersionRe.FindStringSubmatch(last); m != nil && !asked["v"+m[2]] {
		base = m[1]
		n, _ = strconv.Atoi(m[2])
	}
	return u.Host + "/" + strings.Join(segs, "/") + "/" + base, n
}

// troubleWords are words a question describing a failure uses.
var troubleWords = map[string]bool{
	"error": true, "errors": true, "fail": true, "fails": true, "failed": true, "failing": true, "failure": true,
	"crash": true, "crashes": true, "crashing": true, "keeps": true, "broken": true, "stuck": true, "hang": true,
	"hangs": true, "troubleshoot": true, "troubleshooting": true, "debug": true, "debugging": true, "exception": true,
	"timeout": true, "refused": true, "denied": true,
}

// troubleQuery reports a question about something going wrong: a failure
// word, or "not working", "doesn't work", "won't start".
func troubleQuery(query string) bool {
	low := strings.ToLower(query)
	for _, p := range []string{"not working", "doesn't work", "does not work", "won't", "can't", "cannot", "isn't working"} {
		if strings.Contains(low, p) {
			return true
		}
	}
	for _, w := range strings.FieldsFunc(low, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' }) {
		if troubleWords[w] {
			return true
		}
	}
	return false
}

// troubleRe matches a path or heading that says it is about failures.
var troubleRe = regexp.MustCompile(`(?i)troubleshoot|debug|\berrors?\b|\bproblems?\b|\bcommon[- ]issues\b|\bknown[- ]issues\b|\bdiagnos`)

// troubleSection reports a section whose page path or headings say it
// helps with failures: troubleshooting, debugging, errors.
func troubleSection(s docsindex.Section) bool {
	if u, err := url.Parse(s.Page); err == nil && troubleRe.MatchString(u.Path) {
		return true
	}
	return troubleRe.MatchString(strings.Join(s.Crumbs, " ")) || troubleRe.MatchString(s.Heading)
}
