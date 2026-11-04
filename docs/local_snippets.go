package docs

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
)

// Snippets: the local index's code-first view (ADR-0007, amendment 1;
// LocalOptions.Snippets since amendment 2 made sections the default). Each
// section is cut, at index time, into units an agent can use as they are:
// every run of fenced code with the sentences just before it, and the
// prose no code claimed. Units are byte ranges of the section's text, so a
// snippet is always the docs' own words, never rewritten. A query ranks
// the units, keeps the best few that are not near-duplicates (at most
// localPerSection from one section) and renders each as its heading path,
// its explanation, its code and its source.

// LocalOptions are a caller's choices for the local backend's answer,
// carried in the context (WithLocalOptions) because the Searcher methods
// take only a token budget.
type LocalOptions struct {
	// Snippets asks for code-first snippet units (each code block with
	// the sentences before it, or a stretch of prose) instead of section
	// families.
	Snippets bool
	// Tokens is the budget the caller chose; 0 leaves the view's default
	// (localTokens for sections, localSnippetTokens for snippets),
	// whatever token value the call passes.
	Tokens int
	// Limit caps the results (0: the view's default).
	Limit int
	// Hint is how a surface that shows the answer as text points to the
	// full section of a result cut short (SectionHint); HintNone leaves
	// the pointer out and its tokens uncounted. It applies to compact
	// answers (Tokens set) and to the cut notice of a section fetch.
	Hint HintStyle
}

type localOptionsKey struct{}

// WithLocalOptions carries o to the local backend, directly or through the
// auto chain.
func WithLocalOptions(ctx context.Context, o LocalOptions) context.Context {
	return context.WithValue(ctx, localOptionsKey{}, o)
}

func localOptionsFrom(ctx context.Context) (LocalOptions, bool) {
	o, ok := ctx.Value(localOptionsKey{}).(LocalOptions)
	return o, ok
}

const (
	// localSnippetTokens is the snippet view's default budget: about what
	// Context7 returns (880 tokens on average in the fair-judge runs) and a
	// little more, room for five to eight snippets.
	localSnippetTokens = 1200
	// localMaxSnippets is the most snippets returned without a limit.
	localMaxSnippets = 8
	// localPerSection is the most snippets one section contributes, and
	// localPerPage one page: a reference page whose every entry mentions
	// the question's words must not crowd out the guide that answers it.
	localPerSection = 2
	localPerPage    = 3
	// localCodeBonus is added to a code unit's score: the view is code
	// first, and prose embeds closer to a question than code does. A
	// single block longer than unitMaxCode is a listing (an implementation,
	// a whole config file) more often than an example, and takes
	// localLongCodePenalty instead.
	localCodeBonus       = 0.03
	localLongCodePenalty = 0.03
	// localSnippetFloor is how far below the best unit's score a unit may
	// fall and still be returned (cosine); localMinSnippets are kept
	// whatever their score.
	localSnippetFloor = 0.12
	localMinSnippets  = 3
	// unitExplainChars bounds an explanation: one to three sentences.
	unitExplainChars = 400
	unitExplainMax   = 3
	// unitExplainLead is the longest one-sentence paragraph ("For
	// example:") that leads an explanation into the paragraph before it.
	unitExplainLead = 40
	// unitProseChars bounds a prose unit, cut at a sentence end.
	unitProseChars = 600
	// unitMinProse is the least uncovered prose, in a section with code,
	// that becomes a unit of its own.
	unitMinProse = 150
	// unitMaxCode bounds a run of code blocks joined into one unit; a
	// longer run is split between its blocks (a single long block stays
	// whole, and is trimmed only when shown).
	unitMaxCode = 2000
	// crossVersionPenalty is taken off the score of a section about a
	// later major version than the one indexed (a migration guide to it).
	crossVersionPenalty = 0.05
)

// textSpan is a block of a section's text: a fenced code block, or a
// paragraph (a run of non-blank lines outside fences).
type textSpan struct {
	start, end int // byte range, end before the last line's newline
	code       bool
	list       bool // a list, table or quote: not a sentence of prose
	// label is a short line right before a code block that does not end
	// like a sentence, a tab's or file's label (code groups put "ts",
	// "app.ts" or "Python 3.10+" there), or a paragraph of fewer than
	// three letters (a stray "!"): not prose.
	label bool
}

// textSpans splits text into code blocks and paragraphs with their byte
// ranges. A fence opening right after a paragraph line still starts a code
// block of its own.
func textSpans(text string) []textSpan {
	var out []textSpan
	var fence fenceState
	cur := -1 // index in out of the open span
	pos := 0
	for pos <= len(text) {
		nl := strings.IndexByte(text[pos:], '\n')
		end := len(text)
		if nl >= 0 {
			end = pos + nl
		}
		line := text[pos:end]
		inFence := fence.n > 0
		code := fence.step(line)
		switch {
		case code:
			if !inFence {
				out = append(out, textSpan{start: pos, code: true})
				cur = len(out) - 1
			}
			out[cur].end = end
			if fence.n == 0 { // the closing line
				cur = -1
			}
		case strings.TrimSpace(line) == "":
			cur = -1
		default:
			if cur < 0 || out[cur].code {
				out = append(out, textSpan{start: pos, list: isListLine(line)})
				cur = len(out) - 1
			}
			out[cur].end = end
		}
		if nl < 0 {
			break
		}
		pos = end + 1
	}
	markLabels(text, out)
	return out
}

// markLabels marks the paragraphs that are labels, not prose (textSpan).
func markLabels(text string, spans []textSpan) {
	for i := range spans {
		if spans[i].code {
			continue
		}
		t := text[spans[i].start:spans[i].end]
		tab := i+1 < len(spans) && spans[i+1].code && len(t) <= 30 && !strings.Contains(t, "\n") && !strings.ContainsAny(t[len(t)-1:], ".:!?,;") && !spans[i].list
		spans[i].label = tab || countLetters(t) < 3
	}
}

// countLetters counts the letters in s.
func countLetters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

// listLineRe matches a line that starts a list item, a table row, a quote
// or a heading: not a sentence of running prose.
var listLineRe = regexp.MustCompile(`^\s{0,3}(?:[-*+]\s|\d{1,3}[.)]\s|\||>|#)`)

func isListLine(line string) bool { return listLineRe.MatchString(line) }

// lineOf is s up to its first line break.
func lineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// extractUnits cuts a section's text into snippet units, in text order:
// each run of code blocks (only blank lines or tab labels between them) with the
// sentences just before it (explanationRange), split between blocks past
// unitMaxCode; the prose before an explanation that it left out, and the
// prose after the last code, when there is at least unitMinProse of it;
// and, for a section with no code, its first unitProseChars of prose.
func extractUnits(text string) []docsindex.Unit {
	spans := textSpans(text)
	var units []docsindex.Unit
	prose := func(run []textSpan, end int, min int) {
		if len(run) == 0 {
			return
		}
		start := run[0].start
		if end < 0 {
			end = run[len(run)-1].end
		}
		if r, ok := proseRange(text, start, end); ok && r[1]-r[0] >= min {
			units = append(units, docsindex.Unit{Kind: docsindex.UnitProse, Body: r})
		}
	}
	next := 0 // the first span after the last code run
	hasCode := false
	for i := 0; i < len(spans); {
		if !spans[i].code {
			i++
			continue
		}
		hasCode = true
		j := i
		for j+1 < len(spans) && (spans[j+1].code || spans[j+1].label && j+2 < len(spans) && spans[j+2].code) {
			j++ // the next block, or a tab's label and its block
		}
		run := spans[next:i]
		explain := explanationRange(text, run)
		if explain != [2]int{} {
			prose(run, explain[0], unitMinProse)
		}
		for _, g := range splitCode(spans[i : j+1]) {
			units = append(units, docsindex.Unit{Kind: docsindex.UnitCode, Explain: explain, Body: [2]int{g[0].start, g[len(g)-1].end}})
		}
		next, i = j+1, j+1
	}
	if hasCode {
		prose(spans[next:], -1, unitMinProse)
	} else {
		prose(spans, -1, 1)
	}
	return units
}

// splitCode groups a run of code blocks into units of at most unitMaxCode
// bytes, between blocks.
func splitCode(run []textSpan) [][]textSpan {
	var out [][]textSpan
	start := 0
	for k := 1; k <= len(run); k++ {
		if k == len(run) || run[k].end-run[start].start > unitMaxCode {
			out = append(out, run[start:k])
			start = k
		}
	}
	return out
}

// explanationRange is the explanation for the code after run (the prose
// spans since the previous code, tab labels aside): the nearest
// paragraph, and the one before it when the nearest is one short sentence
// such as "For example:", trimmed to its last unitExplainMax sentences within unitExplainChars
// (a single longer sentence is cut at a word, shown with "…"). The zero
// range for no prose.
func explanationRange(text string, run []textSpan) [2]int {
	for len(run) > 0 && run[len(run)-1].label {
		run = run[:len(run)-1]
	}
	if len(run) == 0 {
		return [2]int{}
	}
	k := len(run) - 1
	end := run[k].end
	start := run[k].start
	if k > 0 && !run[k].list && !run[k-1].list && !run[k-1].label && end-start < unitExplainLead && len(sentenceStarts(text, start, end)) == 1 {
		start = run[k-1].start // "For example:" says nothing on its own
	}
	starts := sentenceStarts(text, start, end)
	from := starts[len(starts)-1]
	for n := len(starts) - 2; n >= 0 && len(starts)-1-n < unitExplainMax && end-starts[n] <= unitExplainChars; n-- {
		from = starts[n]
	}
	if end-from > unitProseChars {
		from = wordAfter(text, end-unitProseChars)
	}
	return trimRange(text, from, end)
}

// proseRange is the prose from start up to end, cut at the last sentence
// end within unitProseChars (a first sentence longer than that is cut at
// a word, shown with "…"). ok is false when it holds no text.
func proseRange(text string, start, end int) ([2]int, bool) {
	if end-start > unitProseChars {
		starts := sentenceStarts(text, start, end)
		cut := 0
		for _, s := range starts[1:] {
			if s-start > unitProseChars {
				break
			}
			cut = s
		}
		if cut == 0 {
			cut = wordBefore(text, start+unitProseChars)
		}
		end = cut
	}
	r := trimRange(text, start, end)
	return r, r[1] > r[0]
}

// trimRange drops the whitespace at either end of text[start:end].
func trimRange(text string, start, end int) [2]int {
	for start < end && isSpaceByte(text[start]) {
		start++
	}
	for end > start && isSpaceByte(text[end-1]) {
		end--
	}
	return [2]int{start, end}
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// wordAfter is the start of the first word at or after i.
func wordAfter(text string, i int) int {
	for i < len(text) && !isSpaceByte(text[i]) {
		i++
	}
	for i < len(text) && isSpaceByte(text[i]) {
		i++
	}
	return i
}

// wordBefore is the end of the last word before i (a rune boundary).
func wordBefore(text string, i int) int {
	j := i
	for j > 0 && !isSpaceByte(text[j-1]) {
		j--
	}
	if j == 0 {
		for i > 0 && !utf8.RuneStart(text[i]) {
			i--
		}
		return i
	}
	return j
}

// sentenceAbbrevs are words a period does not end a sentence after.
var sentenceAbbrevs = map[string]bool{
	"e.g": true, "i.e": true, "etc": true, "vs": true, "cf": true, "approx": true, "incl": true,
	"no": true, "fig": true, "eg": true, "ie": true, "mr": true, "dr": true, "ca": true,
}

// sentenceStarts returns where the sentences of text[start:end] start,
// start first: after a sentence's final punctuation (. ! ?, or : at a
// line end) and the space after it, when the next word starts like a
// sentence; after a blank line; and at a line that starts a list item,
// table row or quote. Inline code (backticks) never ends a sentence.
func sentenceStarts(text string, start, end int) []int {
	out := []int{start}
	add := func(i int) {
		for i < end && isSpaceByte(text[i]) {
			i++
		}
		if i < end && i > out[len(out)-1] {
			out = append(out, i)
		}
	}
	inCode := false
	for i := start; i < end; i++ {
		switch c := text[i]; {
		case c == '`':
			inCode = !inCode
		case c == '\n':
			inCode = false
			if rest := text[i+1 : end]; strings.HasPrefix(strings.TrimLeft(rest, " \t"), "\n") || isListLine(lineOf(rest)) {
				add(i + 1)
			}
		case !inCode && endsSentence(text, start, end, i):
			add(i + 1)
		}
	}
	return out
}

// endsSentence reports whether the punctuation at text[i] ends a sentence
// of text[start:end]: . ! or ? before a space and a word that starts like
// a sentence (an abbreviation's period aside), or : at a line end.
func endsSentence(text string, start, end, i int) bool {
	if i+1 >= end {
		return false
	}
	switch text[i] {
	case '.', '!', '?':
		return (text[i+1] == ' ' || text[i+1] == '\n') && startsSentence(text[i+1:end]) && !abbrevBefore(text, start, i)
	case ':':
		return text[i+1] == '\n'
	}
	return false
}

// startsSentence reports whether s, after its leading space, starts like a
// sentence: a capital, a digit, code, a quote, a bracket or emphasis.
func startsSentence(s string) bool {
	s = strings.TrimLeft(s, " \t\n")
	r, _ := utf8.DecodeRuneInString(s)
	return r != utf8.RuneError && (unicode.IsUpper(r) || unicode.IsDigit(r) || strings.ContainsRune("`\"'([*_<", r))
}

// abbrevBefore reports whether the period at text[i] ends an abbreviation
// (e.g., etc.) or an initial rather than a sentence.
func abbrevBefore(text string, start, i int) bool {
	j := i
	for j > start && (unicode.IsLetter(rune(text[j-1])) || text[j-1] == '.') {
		j--
	}
	word := strings.ToLower(text[j:i])
	return sentenceAbbrevs[word] || len(word) == 1
}

// unitParts are a unit's explanation and body as shown: verbatim, with
// "…" where the explanation starts or a prose body ends inside a sentence.
func unitParts(text string, u docsindex.Unit) (explain, body string) {
	explain, body = u.Parts(text)
	if explain != "" && midSentenceStart(text, u.Explain[0]) {
		explain = "… " + explain
	}
	if u.Kind == docsindex.UnitProse && midSentenceEnd(text, u.Body[1]) {
		body += " …"
	}
	return explain, body
}

// midSentenceStart reports a range starting inside a sentence: the text
// before it, on the same paragraph, does not end one.
func midSentenceStart(text string, i int) bool {
	before := strings.TrimRight(text[:i], " \t")
	if before == "" || strings.HasSuffix(before, "\n") {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(before)
	return !strings.ContainsRune(".!?:", last)
}

// midSentenceEnd reports a range ending inside a sentence: neither at a
// sentence's final punctuation nor at the end of a paragraph or line.
func midSentenceEnd(text string, i int) bool {
	if i >= len(text) || text[i] == '\n' {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(text[:i])
	return !strings.ContainsRune(".!?:)", last)
}

// Cross-version content: a page in one version's tree about a later major
// (a v3 page "Migrating to v4"). It is kept, labelled, and demoted for
// questions that do not ask about it.

// migrationRe finds a migration or upgrade guide by its title, heading or
// path.
var migrationRe = regexp.MustCompile(`(?i)\b(?:migrat\w*|upgrad\w*|porting)\b`)

// versionMentionRe finds a version a title or heading names as one: v4,
// 4.x, 4.0, "version 4", "to 4".
var versionMentionRe = regexp.MustCompile(`(?i)(?:\bv(\d{1,3})(?:\.\d+)*\b|\b(\d{1,3})\.(?:\d+|x)\b|\b(?:version|release|to|into)\s+v?(\d{1,3})\b)`)

// crossVersion is the later major a section is about, when the library's
// indexed version has a major and the section's page title, path below the
// root, or headings name a migration or upgrade and a major above it
// (written as a version, or after a word of the library's title: "Vue
// Router 4"). "" otherwise: a guide with no version, or from an earlier
// one, is about the indexed version.
func crossVersion(indexed, libTitle string, texts []string) string {
	nums, _ := releaseNumbers(normLabel(indexed))
	if len(nums) == 0 {
		return ""
	}
	joined := strings.Join(texts, "\n")
	if !migrationRe.MatchString(joined) {
		return ""
	}
	mentions := versionMentionRe.FindAllStringSubmatch(joined, -1)
	for _, w := range strings.Fields(libTitle) {
		if len(w) >= 3 && isWordy(w) {
			re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\s+v?(\d{1,3})\b`)
			mentions = append(mentions, re.FindAllStringSubmatch(joined, -1)...)
		}
	}
	best := 0
	for _, m := range mentions {
		for _, g := range m[1:] {
			if n := atoiSmall(g); n > best {
				best = n
			}
		}
	}
	if best > nums[0] {
		return itoaSmall(best)
	}
	return ""
}

func isWordy(w string) bool {
	return strings.IndexFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && r != '.' && r != '-' }) < 0
}

func atoiSmall(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// pathWords is a URL path below the root as words, for crossVersion.
func pathWords(pageURL, root string) string {
	p := strings.TrimPrefix(pageURL, root)
	return strings.NewReplacer("/", " ", "-", " ", "_", " ").Replace(p)
}

// asksAbout reports whether a query asks about a later version's content:
// it names migrating or upgrading, or the version itself.
func asksAbout(query, major string) bool {
	if migrationRe.MatchString(query) {
		return true
	}
	for _, m := range versionMentionRe.FindAllStringSubmatch(query, -1) {
		for _, g := range m[1:] {
			if g == major {
				return true
			}
		}
	}
	for _, t := range strings.Fields(strings.ToLower(query)) {
		if strings.Trim(t, "v.,?") == major {
			return true
		}
	}
	return false
}

// scoredUnit is one unit of one section, scored for a query.
type scoredUnit struct {
	sec, unit int
	score     float64
}

// rankUnits scores every unit of secs for the query: with vectors, half
// its own cosine and half its section's best window's (the section-level
// match the retrieval measurements validated, sharpened by the unit's),
// or, for a unit indexed without a vector of its own, its section's
// cosine and the share of query terms it names; without vectors, its
// section's keyword score (rank, relative to the best) and
// the share of query terms it names. A unit gets a small boost by the
// share of the identifiers the query writes that it names (identBoost), a
// code unit localCodeBonus (a listing longer than unitMaxCode
// localLongCodePenalty instead), and a section
// about a later version than indexed (CrossVersion) is demoted unless the
// query asks about it.
func rankUnits(secs []docsindex.Section, qv []float32, keyword []scoredSection, query string) []scoredUnit {
	q := unitQuery{text: query, terms: queryTerms(query), idents: queryIdents(query), qv: qv}
	secScore, has := sectionScores(secs, qv, keyword)
	var out []scoredUnit
	for i, s := range secs {
		if !has[i] {
			continue
		}
		for j := range s.Units {
			if score, ok := q.score(s, j, secScore[i]); ok {
				out = append(out, scoredUnit{sec: i, unit: j, score: score})
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].score > out[b].score })
	return out
}

// sectionScores is each section's score for rankUnits: its best window's
// cosine with qv, or without qv its keyword score relative to the best;
// has is false for a section with neither.
func sectionScores(secs []docsindex.Section, qv []float32, keyword []scoredSection) ([]float64, []bool) {
	score, has := make([]float64, len(secs)), make([]bool, len(secs))
	if qv == nil {
		if len(keyword) > 0 && keyword[0].score > 0 {
			for _, k := range keyword {
				score[k.page], has[k.page] = k.score/keyword[0].score, true
			}
		}
		return score, has
	}
	for i, s := range secs {
		for _, v := range s.Vectors {
			if len(v) != len(qv) {
				continue
			}
			if c := float64(embed.Dot(v, qv)); !has[i] || c > score[i] {
				score[i], has[i] = c, true
			}
		}
	}
	return score, has
}

// unitQuery is a question as rankUnits scores units against it.
type unitQuery struct {
	text          string
	terms, idents []string
	qv            []float32
}

// score is unit j of s's score, given its section's.
func (q unitQuery) score(s docsindex.Section, j int, secScore float64) (float64, bool) {
	u := s.Units[j]
	explain, body := u.Parts(s.Text)
	var score float64
	switch {
	case q.qv == nil:
		score = secScore + 0.1*termShare(explain+"\n"+body, q.terms)
	case len(s.UnitVectors) != len(s.Units) || len(s.UnitVectors[j]) != len(q.qv):
		// Units are not embedded (since amendment 2): the section's cosine,
		// told apart by the share of the question's terms the unit names.
		score = secScore + 0.05*termShare(explain+"\n"+body, q.terms)
	default:
		score = 0.5*float64(embed.Dot(s.UnitVectors[j], q.qv)) + 0.5*secScore
	}
	score += identBoost(s.Heading, explain+"\n"+body, q.terms, q.idents)
	if u.Kind == docsindex.UnitCode {
		if len(body) > unitMaxCode {
			score -= localLongCodePenalty
		} else {
			score += localCodeBonus
		}
	}
	if s.CrossVersion != "" && !asksAbout(q.text, s.CrossVersion) {
		score -= crossVersionPenalty
	}
	return score, true
}

// termShare is the share of terms text names.
func termShare(text string, terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	toks := tokenSet(text)
	n := 0
	for _, t := range terms {
		if toks[t] {
			n++
		}
	}
	return float64(n) / float64(len(terms))
}

// identWordRe matches an identifier in code.
var identWordRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

// identBoost is the keyword nudge for a unit: up to 0.06 by the share of
// the identifiers the query writes as code (useEffect, res.json(),
// onComplete) that its text names, in code or inline, and up to 0.02 more
// when its heading is that API entry (entryMatch).
func identBoost(heading, text string, terms, idents []string) float64 {
	if len(idents) == 0 {
		return 0
	}
	named := map[string]bool{}
	for _, w := range identWordRe.FindAllString(text, -1) {
		named[identKey(w)] = true
	}
	n := 0
	for _, id := range idents {
		if named[id] {
			n++
		}
	}
	return 0.06*float64(n)/float64(len(idents)) + 0.02*entryMatch(heading, terms, idents, false)
}

// snippetResults picks and renders the ranked units within the budget:
// best first, never a near-duplicate of one already picked
// (sameSnippet), at most localPerSection from a section (copies of a
// section at other addresses count as that section) and localPerPage from
// a page, none scoring
// more than localSnippetFloor below the best once localMinSnippets are
// in; each whole, skipped when it does not fit what is left, except the
// first, whose code is trimmed to fit.
func snippetResults(lib docsindex.Library, secs []docsindex.Section, ranked []scoredUnit, dense bool, tokens, limit int) []Result {
	if tokens <= 0 {
		tokens = localSnippetTokens
	}
	if limit <= 0 {
		limit = localMaxSnippets
	}
	remaining := tokens
	var out []Result
	var picked [][]string      // token lists of the picked snippets
	perSec := map[string]int{} // by heading and text, so copies share a count
	perPage := map[string]int{}
	for _, su := range ranked {
		if len(out) >= limit || remaining <= 0 {
			break
		}
		if dense && len(out) >= localMinSnippets && su.score < ranked[0].score-localSnippetFloor {
			break
		}
		s := secs[su.sec]
		secKey := s.Heading + "\x00" + strings.Join(strings.Fields(s.Text), " ")
		if perSec[secKey] >= localPerSection || perPage[s.Page] >= localPerPage {
			continue
		}
		explain, body := unitParts(s.Text, s.Units[su.unit])
		toks := rankTokens(explain + " " + body)
		if slices.ContainsFunc(picked, func(p []string) bool { return sameSnippet(p, toks) }) {
			continue
		}
		r := Result{
			Library: lib.Root, Version: lib.Version, Title: s.Heading, Breadcrumb: strings.Join(s.Crumbs, " > "),
			Snippet: joinSnippet(explain, body), URL: webSectionOf(s, 0).URL(), Source: "local",
		}
		cost := SnippetTokens(r)
		if cost > remaining {
			if len(out) > 0 {
				continue
			}
			r.Snippet = fitSnippet(r, explain, body, remaining)
			cost = SnippetTokens(r)
		}
		remaining -= cost
		perSec[secKey]++
		perPage[s.Page]++
		picked = append(picked, toks)
		out = append(out, r)
	}
	return out
}

// joinSnippet is a snippet's text: the explanation, a blank line, the
// body.
func joinSnippet(explain, body string) string {
	if explain == "" {
		return body
	}
	return explain + "\n\n" + body
}

// fitSnippet trims a snippet's body (closing any fence the cut leaves
// open) so the rendered result costs about tokens.
func fitSnippet(r Result, explain, body string, tokens int) string {
	r.Snippet = joinSnippet(explain, "")
	room := (tokens - SnippetTokens(r)) * 3 // code is about three bytes a token
	if t := trimSnippet(body, max(room, webMinTrimBytes)); t != "" {
		body = t
	}
	return joinSnippet(explain, body)
}

// sameSnippet reports near-duplicates: the same words, or token sets
// overlapping by at least 85% (Jaccard) when both have a few.
func sameSnippet(a, b []string) bool {
	if slices.Equal(a, b) {
		return true
	}
	if len(a) < 5 || len(b) < 5 {
		return false
	}
	sa := map[string]bool{}
	for _, t := range a {
		sa[t] = true
	}
	sb := map[string]bool{}
	for _, t := range b {
		sb[t] = true
	}
	inter := 0
	for t := range sa {
		if sb[t] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	return union > 0 && float64(inter)/float64(union) >= 0.85
}

// RenderSnippet is a local snippet as an agent reads it: "### " and its
// heading path, the explanation and code, and its source.
func RenderSnippet(r Result) string {
	path := r.Breadcrumb
	if path == "" {
		path = r.Title
	}
	return "### " + path + "\n\n" + r.Snippet + "\n\nsource: " + r.URL + "\n"
}

// SnippetTokens estimates a rendered snippet's tokens: about 4.4 bytes a
// token for prose and 3.4 for code and URLs, which tokenize densely (a
// little over what o200k counts on docs snippets, so the budget holds).
func SnippetTokens(r Result) int {
	text := RenderSnippet(r)
	code := 0
	for _, sp := range textSpans(text) {
		if sp.code {
			code += sp.end - sp.start
		}
	}
	code += len(r.URL)
	prose := max(len(text)-code, 0)
	return (prose*10+43)/44 + (code*10+33)/34
}
