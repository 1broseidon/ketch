package docs

import (
	"context"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text helpers the local index shares between building and answering:
// the caller's options (LocalOptions), a section's blocks (textSpans) and
// sentences (sentenceStarts), which answers cut pieces at, and the
// cross-version label a build gives a page about a later major release.

// LocalOptions are a caller's choices for the local backend's answer,
// carried in the context (WithLocalOptions) because the Searcher methods
// take only a token budget.
type LocalOptions struct {
	// Tokens is the budget the caller chose; 0 leaves the default
	// (localTokens), whatever token value the call passes.
	Tokens int
	// Limit caps the results, one per section (0: the budget decides).
	Limit int
	// Hint is how a surface that shows the answer as text points to the
	// full section of a result cut short (SectionHint); HintNone leaves
	// the pointer out and its tokens uncounted. It applies to answers and
	// to the cut notice of a section fetch.
	Hint HintStyle
	// note is the note the caller adds to the answer's frontmatter (a
	// partial answer's), which the budget covers; read, the pages a
	// search just read when the answer is from them (0 for an indexed
	// library), which its source line names.
	note string
	read int
}

type localOptionsKey struct{}

// WithLocalOptions carries o to the local index's answer, through the
// search backend.
func WithLocalOptions(ctx context.Context, o LocalOptions) context.Context {
	return context.WithValue(ctx, localOptionsKey{}, o)
}

func localOptionsFrom(ctx context.Context) (LocalOptions, bool) {
	o, ok := ctx.Value(localOptionsKey{}).(LocalOptions)
	return o, ok
}

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

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

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

// RenderSnippet is a section fetch's result as an agent reads it (an
// answer prints its groups with RenderAnswer): "### " and its heading
// path, its group line when it has one, its text, its contents line
// when it has one, and its source.
func RenderSnippet(r Result) string {
	path := r.Breadcrumb
	if path == "" {
		path = r.Title
	}
	head := "### " + path + "\n\n"
	if line := r.GroupLine(); line != "" {
		head = "### " + path + "\n" + line + "\n\n"
	}
	tail := "\n\nsource: " + r.URL + "\n"
	if r.Contents != "" {
		tail = "\n\n" + r.Contents + "\nsource: " + r.URL + "\n"
	}
	return head + r.Snippet + tail
}
