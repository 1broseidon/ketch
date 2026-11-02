package docs

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// webSection is one heading's worth of a page.
type webSection struct {
	page    int      // page order, for deterministic ties
	order   int      // section order within the page
	pageURL string   // displayed page URL, no fragment
	heading string   // the section's own heading (the page title for the intro)
	crumbs  []string // page title > H2 > H3
	anchor  string   // fragment id; "" for the intro, which links to the page
	text    string   // body under the heading, code blocks intact
}

// URL is the section's address: the page plus its heading anchor.
func (s webSection) URL() string {
	if s.anchor == "" {
		return s.pageURL
	}
	return s.pageURL + "#" + s.anchor
}

// fenceState tracks fenced code blocks (``` or ~~~, CommonMark rules) so a
// "# comment" inside one is not taken for a heading.
type fenceState struct {
	char byte
	n    int
}

// step consumes a line and reports whether it belongs to a fence: the
// opening line, a line inside, or the closing line.
func (f *fenceState) step(line string) bool {
	char, n, info, ok := fenceMarker(line)
	if f.n > 0 {
		if ok && char == f.char && n >= f.n && strings.TrimSpace(info) == "" {
			f.n = 0
		}
		return true
	}
	if ok {
		f.char, f.n = char, n
		return true
	}
	return false
}

// closer returns the line that closes the open fence, or "" when none is.
func (f fenceState) closer() string {
	if f.n == 0 {
		return ""
	}
	return strings.Repeat(string(f.char), f.n)
}

// fenceMarker parses a fence line: up to three spaces of indent, then three
// or more backticks or tildes. A backtick fence's info string may not
// contain a backtick (that is inline code, not a fence).
func fenceMarker(line string) (char byte, n int, info string, ok bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, "", false
	}
	char = trimmed[0]
	for n < len(trimmed) && trimmed[n] == char {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	info = trimmed[n:]
	if char == '`' && strings.Contains(info, "`") {
		return 0, 0, "", false
	}
	return char, n, info, true
}

// atxHeading is a parsed "## Heading {#id}" line.
type atxHeading struct {
	level int
	text  string
	id    string // explicit anchor, "" when the slug applies
}

var (
	// explicitIDRe matches a trailing {#id} (Docusaurus, MyST, Pandoc
	// attributes; extra classes are ignored), or the MDX comment react.dev's
	// Markdown pages carry as a heading's id, {/*id*/}.
	explicitIDRe = regexp.MustCompile(`\s*\{(?:#([^\s}]+)[^}]*|/\*\s*([\w-]+)\s*\*/)\}\s*$`)
	// fragmentLinkRe matches a link with a fragment, [text](#id) or
	// [text](page#id), inside a heading: the permalink HTML-to-Markdown
	// leaves behind (Docusaurus: a zero-width space linked to #x, titled
	// "Direct link to X"; MkDocs and Sphinx: a pilcrow), or a heading wrapped
	// in its own anchor. The scrape pipeline resolves links against the page,
	// so a permalink's href="#x" arrives as an absolute page URL plus #x.
	fragmentLinkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s#]*)#([^)\s]+)(?:\s+"[^"]*")?\)`)
	inlineLinkRe   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	htmlTagRe      = regexp.MustCompile(`<[^>]+>`)
	// codeSlotRe matches the placeholder cleanInline puts where a code span
	// was while it cleans the text around it.
	codeSlotRe = regexp.MustCompile("\uE000([0-9]+)\uE001")
)

// parseATX parses an ATX heading line (CommonMark: up to three spaces of
// indent, 1–6 '#', then a space or the end of the line).
func parseATX(line string) (atxHeading, bool) {
	s := strings.TrimRight(line, " \t\r")
	trimmed := strings.TrimLeft(s, " ")
	if len(s)-len(trimmed) > 3 {
		return atxHeading{}, false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || (level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t') {
		return atxHeading{}, false
	}
	text := strings.TrimSpace(trimmed[level:])
	if closing := strings.TrimRight(text, "#"); closing == "" {
		text = ""
	} else if closing != text && strings.HasSuffix(closing, " ") {
		text = strings.TrimSpace(closing)
	}
	h := headingAnchor(text)
	h.level = level
	return h, true
}

// headingAnchor splits heading text into its words and its explicit anchor:
// a trailing {#id} or {/*id*/}, else the fragment of a [text](#id) link in
// the heading or of a permalink placeholder linking to any URL's #id. A
// worded link to another page's fragment is just a link: it keeps its words
// and names no anchor. Permalink placeholders are dropped.
func headingAnchor(text string) atxHeading {
	var h atxHeading
	if m := explicitIDRe.FindStringSubmatch(text); m != nil {
		h.id = firstNonEmpty(m[1], m[2])
		text = strings.TrimSuffix(text, m[0])
	}
	text = fragmentLinkRe.ReplaceAllStringFunc(text, func(link string) string {
		m := fragmentLinkRe.FindStringSubmatch(link)
		label, target, id := m[1], m[2], m[3]
		permalink := isPermalinkLabel(label)
		if h.id == "" && (target == "" || permalink) {
			h.id = id
		}
		if permalink {
			return ""
		}
		return label
	})
	h.text = cleanInline(text)
	return h
}

// isPermalinkLabel reports a permalink's placeholder text (empty, a zero
// width space, "#", "¶", "🔗") as opposed to real heading words.
func isPermalinkLabel(label string) bool {
	label = strings.TrimSpace(strings.Map(dropInvisible, label))
	switch label {
	case "", "#", "¶", "§", "🔗", "Link", "Permalink", "Direct link":
		return true
	}
	return false
}

// cleanInline reduces inline Markdown to the text it renders: code spans to
// their content verbatim, links to their labels, emphasis markers and HTML
// tags removed, invisible characters dropped, whitespace collapsed. Code
// spans are set aside first, so "`<Image />`" stays text instead of being
// stripped as a tag, and a link or emphasis inside one stays literal.
func cleanInline(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\uE000' || r == '\uE001' { // reserved for codeSlotRe
			return -1
		}
		return dropInvisible(r)
	}, s)
	var spans []string
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '`' {
			b.WriteByte(s[i])
			i++
			continue
		}
		n := backtickRun(s, i)
		end := closingBackticks(s, i+n, n)
		if end < 0 {
			b.WriteString(s[i : i+n]) // no matching run: literal backticks
			i += n
			continue
		}
		spans = append(spans, codeSpanText(s[i+n:end]))
		fmt.Fprintf(&b, "\uE000%d\uE001", len(spans)-1)
		i = end + n
	}
	text := inlineLinkRe.ReplaceAllString(b.String(), "$1")
	text = htmlTagRe.ReplaceAllString(text, "")
	text = strings.NewReplacer("**", "", "__", "").Replace(text)
	text = codeSlotRe.ReplaceAllStringFunc(text, func(slot string) string {
		i, _ := strconv.Atoi(codeSlotRe.FindStringSubmatch(slot)[1])
		return spans[i]
	})
	return collapseSpace(text)
}

// backtickRun returns the length of the run of backticks at s[i].
func backtickRun(s string, i int) int {
	n := 0
	for i+n < len(s) && s[i+n] == '`' {
		n++
	}
	return n
}

// closingBackticks returns where the code span opened by n backticks ends:
// the next run of exactly n backticks at or after from, or -1 (CommonMark).
func closingBackticks(s string, from, n int) int {
	for i := from; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		run := backtickRun(s, i)
		if run == n {
			return i
		}
		i += run
	}
	return -1
}

// codeSpanText is a code span's content as CommonMark renders it: line
// endings as spaces, and one space stripped from each end when both ends
// have one and the content is not all spaces.
func codeSpanText(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ").Replace(s)
	if len(s) >= 2 && s[0] == ' ' && s[len(s)-1] == ' ' && strings.Trim(s, " ") != "" {
		s = s[1 : len(s)-1]
	}
	return s
}

func dropInvisible(r rune) rune {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
		return -1
	}
	return r
}

// slugger produces GitHub-style heading anchors (github-slugger): lower
// case; letters, combining marks, decimal and letter numbers, connector
// punctuation ("_") and hyphens kept, everything else dropped; each space
// a hyphen; repeats suffixed -1, -2.
// Site slug rules vary — Docusaurus and Starlight follow github-slugger,
// while MkDocs, Sphinx, Mintlify and hand-written sites differ on
// punctuation, Unicode and duplicates — so a generated anchor is a best
// effort; an explicit {#id} or a permalink anchor in the heading wins.
type slugger map[string]int

func (s slugger) slug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.In(r, unicode.L, unicode.M, unicode.Nd, unicode.Nl, unicode.Pc) || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return s.claim(b.String())
}

// claim registers id, suffixing it the way github-slugger does when taken.
func (s slugger) claim(id string) string {
	result := id
	for {
		if _, taken := s[result]; !taken {
			break
		}
		s[id]++
		result = id + "-" + strconv.Itoa(s[id])
	}
	s[result] = 0
	return result
}

// splitSections splits a page into sections at ATX headings outside code
// fences. H1–H3 start sections; H4 and deeper stay inline in their section
// so API entries keep their Parameters/Returns together. A leading H1 is
// the page title (it beats an HTML <title> that carries a site suffix), and
// the text before the first H2/H3 is the intro, linked to the page itself.
// Near-empty sections are dropped.
func splitSections(p webPage, pageIndex int) []webSection {
	title := p.Title
	slugs := slugger{}
	var out []webSection
	var stack [4]string // H1..H3 text below the page title
	cur := webSection{page: pageIndex, pageURL: p.URL, heading: title, crumbs: []string{title}}
	var body []string
	var fence fenceState
	seenHeading := false

	flush := func() {
		cur.text = strings.TrimSpace(strings.Join(body, "\n"))
		if len(strings.Join(strings.Fields(cur.text), "")) >= webMinSectionText {
			cur.order = len(out)
			out = append(out, cur)
		}
		body = nil
	}

	for _, line := range strings.Split(strings.ReplaceAll(p.Markdown, "\r\n", "\n"), "\n") {
		if fence.step(line) {
			body = append(body, line)
			continue
		}
		h, ok := parseATX(line)
		if !ok || h.level > 3 || h.text == "" {
			body = append(body, line)
			continue
		}
		if h.level == 1 && !seenHeading {
			seenHeading = true
			title = h.text
			cur.heading, cur.crumbs = title, []string{title}
			claimAnchor(slugs, h)
			continue
		}
		seenHeading = true
		flush()
		stack[h.level] = h.text
		for l := h.level + 1; l < len(stack); l++ {
			stack[l] = ""
		}
		crumbs := []string{title}
		for l := 1; l <= h.level; l++ {
			if stack[l] != "" {
				crumbs = append(crumbs, stack[l])
			}
		}
		cur = webSection{page: pageIndex, pageURL: p.URL, heading: h.text, crumbs: crumbs, anchor: claimAnchor(slugs, h)}
	}
	flush()
	return out
}

// claimAnchor returns the heading's anchor, registering it so a later
// heading with the same text gets the -1 suffix the site would give it.
func claimAnchor(slugs slugger, h atxHeading) string {
	if h.id != "" {
		return slugs.claim(h.id)
	}
	return slugs.slug(h.text)
}

// stripFrontmatter removes a leading YAML frontmatter block and returns its
// title, if any, with the remaining Markdown.
func stripFrontmatter(md string) (string, string) {
	rest := strings.TrimLeft(strings.TrimPrefix(md, "\ufeff"), "\r\n")
	if !strings.HasPrefix(rest, "---\n") && !strings.HasPrefix(rest, "---\r\n") {
		return "", md
	}
	lines := strings.Split(rest, "\n")
	for i := 1; i < len(lines) && i < 100; i++ {
		switch strings.TrimSpace(lines[i]) {
		case "---", "...":
			return frontmatterTitle(lines[1:i]), strings.Join(lines[i+1:], "\n")
		}
	}
	return "", md
}

func frontmatterTitle(lines []string) string {
	for _, l := range lines {
		key, value, ok := strings.Cut(l, ":")
		if ok && strings.TrimSpace(key) == "title" {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

// firstH1 returns the text of the first H1 outside code fences.
func firstH1(md string) string {
	var fence fenceState
	for _, line := range strings.Split(md, "\n") {
		if fence.step(line) {
			continue
		}
		if h, ok := parseATX(line); ok && h.level == 1 {
			return h.text
		}
	}
	return ""
}
