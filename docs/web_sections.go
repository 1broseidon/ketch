package docs

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"slices"
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
	crumbs  []string // page title > H2 > H3 …
	anchor  string   // fragment id; "" for the intro, which links to the page
	// source says where the anchor came from: the page or a fixed
	// generator rule (never second-guessed), a generator's slug rule, or
	// github-slugger's as a guess. The last two may be corrected by the
	// fragment links the fetched pages carry (adoptLinkedAnchors).
	source anchorSource
	text   string // body under the heading, code blocks intact, links reduced to their labels
	// links is the body as the page had it, links and all: the fragment
	// links in it are evidence for anchors (adoptLinkedAnchors).
	links string
}

// anchorSource is where a section's anchor came from.
type anchorSource int

const (
	anchorGuess anchorSource = iota // github-slugger's slug: no generator recognised
	anchorRule                      // the recognised generator's slug rule (VitePress, Sphinx)
	anchorFixed                     // an id the page carries, or one the generator fixes (rustdoc, pkg.go.dev)
)

// URL is the section's address: the page plus its heading anchor,
// percent-encoded as a fragment, so that decoding it gives the id back
// (rustdoc's impl-From%3CFile%3E-for-Body is #impl-From%253CFile%253E-for-Body;
// a browser finds the element either way).
func (s webSection) URL() string {
	if s.anchor == "" {
		return s.pageURL
	}
	return s.pageURL + "#" + (&url.URL{Fragment: s.anchor}).EscapedFragment()
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
	raw   string // the heading's Markdown, as written after the #s
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
	h.level, h.raw = level, text
	return h, true
}

// listItemRe matches a list item's marker: "- ", "* ", "+ ", "1. ", "1) ".
var listItemRe = regexp.MustCompile(`^ {0,3}(?:[-*+]|\d{1,9}[.)])[ \t]+`)

// parseHeadingLine parses an ATX heading, also one that opens a list item:
// serde.rs puts each attribute in a list, "- ##### `#[serde(rename)]`".
func parseHeadingLine(line string) (atxHeading, bool) {
	if h, ok := parseATX(line); ok {
		return h, true
	}
	if m := listItemRe.FindString(line); m != "" && strings.HasPrefix(line[len(m):], "#") {
		return parseATX(line[len(m):])
	}
	return atxHeading{}, false
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
			h.id = fragmentID(id)
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
		if r == '\uE000' || r == '\uE001' || isEscapeSlot(r) { // reserved for codeSlotRe and escapes
			return -1
		}
		return dropInvisible(r)
	}, s)
	var spans []string
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) {
			// A backslash escape is the character itself, and starts no
			// link, image, code span or tag ("\![Freeze](…)" on docs.rs).
			b.WriteRune(escapeSlot + rune(s[i+1]))
			i += 2
			continue
		}
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
	// What a renderer shows for "get\_mut" and "&lt;T&gt;" (docs.rs
	// headings carry both), outside code spans, where they are literal.
	text = htmlstd.UnescapeString(text)
	text = strings.Map(func(r rune) rune {
		if isEscapeSlot(r) {
			return r - escapeSlot
		}
		return r
	}, text)
	text = codeSlotRe.ReplaceAllStringFunc(text, func(slot string) string {
		i, _ := strconv.Atoi(codeSlotRe.FindStringSubmatch(slot)[1])
		return spans[i]
	})
	return collapseSpace(text)
}

// escapeSlot is where cleanInline keeps a backslash-escaped character
// while it cleans the text: a private-use rune per ASCII byte.
const escapeSlot = '\uF700'

func isEscapeSlot(r rune) bool { return r >= escapeSlot && r < escapeSlot+0x80 }

// isASCIIPunct reports the characters a Markdown backslash can escape.
func isASCIIPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
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

// anchorStyle is a docs generator's rule for heading ids, where it is not
// github-slugger's.
type anchorStyle int

const (
	styleGitHub    anchorStyle = iota // github-slugger: Docusaurus, Starlight, Fumadocs, most others
	styleVitePress                    // VitePress: punctuation runs to "-" (build.lib → build-lib)
	styleSphinx                       // docutils make_id (select_related() → select-related)
	styleRustdoc                      // rustdoc: method.x, impl-Trait-for-Type, variant.X, fixed section ids
	stylePkgsite                      // pkg.go.dev: pkg-overview, readme-x, hdr-X_Y, Func, Type.Method
)

var (
	// vitepressHeadingRe matches a VitePress heading as the scrape pipeline
	// leaves it: its header-anchor link unwrapped to a space and a
	// zero-width space after the text. (Docusaurus's hash link leaves the
	// zero-width space with no space before it.)
	vitepressHeadingRe = regexp.MustCompile(`(?m)^ {0,3}#{1,6} .*\S \x{200b}[ \t]*$`)
	// sphinxPermalinkRe matches a Sphinx headerlink's title, on a heading,
	// a definition, a code caption or a table that kept its permalink.
	sphinxPermalinkRe = regexp.MustCompile(`#[^)\s]+ "(?:Link to this (?:heading|definition|code|table|term|image|equation)|Permalink to this (?:headline|heading|definition|code|table))"\)`)
)

// pageAnchorStyle recognises the generator whose id rule a page follows:
// by host for the registry docs (docs.rs, pkg.go.dev), else by the traces
// its Markdown keeps.
func pageAnchorStyle(p webPage) anchorStyle {
	if u, err := url.Parse(p.URL); err == nil {
		switch scopeHost(u) {
		case "docs.rs", "doc.rust-lang.org":
			return styleRustdoc
		case "pkg.go.dev":
			return stylePkgsite
		}
	}
	switch {
	case vitepressHeadingRe.MatchString(p.Markdown):
		return styleVitePress
	case sphinxPermalinkRe.MatchString(p.Markdown):
		return styleSphinx
	}
	return styleGitHub
}

// anchor claims h's anchor under the style; ancestors are the texts of
// the headings enclosing it, outermost first.
func (st anchorStyle) anchor(slugs slugger, h atxHeading, ancestors []string) (string, anchorSource) {
	if h.id != "" {
		return slugs.claim(h.id), anchorFixed
	}
	switch st {
	case styleVitePress:
		return slugs.claim(vitepressSlug(h.text)), anchorRule
	case styleSphinx:
		return slugs.claim(docutilsID(h.text)), anchorRule
	case styleRustdoc:
		if id := rustdocID(h, ancestors); id != "" {
			return slugs.claim(id), anchorFixed
		}
	case stylePkgsite:
		if id := pkgsiteID(h, ancestors); id != "" {
			return slugs.claim(id), anchorFixed
		}
	}
	return slugs.slug(h.text), anchorGuess
}

// vitepressSpecialRe is VitePress's slugify set of characters that become
// a hyphen (@mdit-vue/shared).
var vitepressSpecialRe = regexp.MustCompile("[\\s~`!@#$%^&*()\\-_+=\\[\\]{}|\\\\;:\"'“”‘’<>,.?/]+")

// vitepressSlug is VitePress's default heading slug: runs of whitespace
// and punctuation become one hyphen, trimmed at the ends; a leading digit
// gets an underscore; lower case. (Its Unicode normalisation is skipped:
// an accented letter stays as it is.)
func vitepressSlug(text string) string {
	s := strings.Map(func(r rune) rune {
		if r < 0x20 {
			return -1
		}
		return r
	}, text)
	s = strings.Trim(vitepressSpecialRe.ReplaceAllString(s, "-"), "-")
	if s != "" && s[0] >= '0' && s[0] <= '9' {
		s = "_" + s
	}
	return strings.ToLower(s)
}

// hexoSlug is Hexo's heading id (hexo-util slugize): VitePress's rule
// without lower-casing or the digit prefix, so ids keep the heading's
// case (v2.vuejs.org's #Customizing-Component-v-model).
func hexoSlug(text string) string {
	return strings.Trim(vitepressSpecialRe.ReplaceAllString(text, "-"), "-")
}

// slugRules are the slug rules link evidence can show a site uses,
// github-slugger's first.
var slugRules = []func(string) string{
	func(t string) string { return slugger{}.slug(t) },
	vitepressSlug,
	docutilsID,
	hexoSlug,
}

// hexoRule is hexoSlug's index in slugRules.
const hexoRule = 3

// adoptLinkedAnchors corrects slugged anchors with the ids the fetched
// pages' own links use: a link to page#Customizing-Component-v-model is
// the site saying what the section headed "Customizing Component v-model"
// is called. A section whose heading matches one linked fragment, letters
// and digits compared, takes that id. When those matches show the site
// slugs by a rule other than github-slugger's (Hexo keeps case; VitePress
// turns dots into hyphens, shown twice), the guessed anchors on the same
// host are re-slugged by it. Ids the page carried or the generator fixes
// are never touched.
func adoptLinkedAnchors(sections []webSection) []webSection {
	linked := linkedFragments(sections)
	if len(linked) == 0 {
		return sections
	}
	out := slices.Clone(sections)
	headings := map[string]int{} // page and normalised heading → sections with it
	for _, s := range out {
		headings[pageKey(s.pageURL)+"#"+idWords(s.heading)]++
	}
	votes := make([]int, len(slugRules))
	adopted := map[string]bool{} // hosts whose anchors link evidence corrected
	for i := range out {
		s := &out[i]
		key := pageKey(s.pageURL)
		if s.source != anchorFixed && s.anchor != "" && headings[key+"#"+idWords(s.heading)] == 1 && adoptFragment(s, linked[key], votes) {
			adopted[hostOf(s.pageURL)] = true
		}
	}
	rule := provenRule(votes)
	if rule == nil {
		return out
	}
	for i := range out {
		s := &out[i]
		if s.source == anchorGuess && s.anchor != "" && adopted[hostOf(s.pageURL)] {
			s.anchor = rule(s.heading) + dedupSuffix(s.anchor, slugger{}.slug(s.heading))
		}
	}
	return out
}

// provenRule returns the slug rule the votes prove a site uses instead of
// github-slugger's, or nil. One id that keeps its heading's case is proof
// of a case-keeping rule: no lower-casing rule makes it. A punctuation
// difference may be one hand-written id (react.dev's root.unmount() is
// #root-unmount, its other headings are not slugged that way), so those
// rules need two.
func provenRule(votes []int) func(string) string {
	best := 0
	for r := range slugRules {
		if votes[r] > votes[best] {
			best = r
		}
	}
	if best == 0 || best != hexoRule && votes[best] < 2 {
		return nil
	}
	return slugRules[best]
}

// adoptFragment gives s the fragment among frags that names its heading,
// counting in votes the slug rules that would have produced it.
func adoptFragment(s *webSection, frags []string, votes []int) bool {
	words := idWords(s.heading)
	for _, frag := range frags {
		if words == "" || idWords(frag) != words {
			continue
		}
		for r, rule := range slugRules {
			if rule(s.heading) == frag {
				votes[r]++
			}
		}
		s.anchor, s.source = frag, anchorFixed
		return true
	}
	return false
}

// linkedFragments maps each page (pageKey) to the fragments the sections'
// links point at on it.
func linkedFragments(sections []webSection) map[string][]string {
	out := map[string][]string{}
	for _, s := range sections {
		base, err := url.Parse(s.pageURL)
		if err != nil {
			continue
		}
		for _, m := range fragmentLinkRe.FindAllStringSubmatch(s.links, -1) {
			target, err := base.Parse(m[2])
			if err != nil || isPermalinkLabel(m[1]) {
				continue
			}
			key, frag := pageKey(target.String()), fragmentID(m[3])
			if !slices.Contains(out[key], frag) {
				out[key] = append(out[key], frag)
			}
		}
	}
	return out
}

// pageKey identifies a page however a link spells it: host without www.,
// path without a trailing slash or .html.
func pageKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	p := strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), ".html")
	return scopeHost(u) + p
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return scopeHost(u)
	}
	return ""
}

// idWords is text's letters and digits, lower-cased: what an id and the
// heading it names have in common under any slug rule.
func idWords(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, text)
}

// dedupSuffix returns the "-N" a slugger added to base to make anchor.
func dedupSuffix(anchor, base string) string {
	if rest, ok := strings.CutPrefix(anchor, base); ok && len(rest) > 1 && rest[0] == '-' && strings.Trim(rest[1:], "0123456789") == "" {
		return rest
	}
	return ""
}

// rustdocSectionIDs are the ids rustdoc gives its own section headings.
var rustdocSectionIDs = map[string]string{
	"implementations": "implementations", "trait implementations": "trait-implementations",
	"auto trait implementations": "synthetic-implementations", "blanket implementations": "blanket-implementations",
	"implementations on foreign types": "foreign-impls", "implementors": "implementors",
	"required methods": "required-methods", "provided methods": "provided-methods",
	"required associated types": "required-associated-types", "provided associated types": "provided-associated-types",
	"required associated constants": "required-associated-consts", "provided associated constants": "provided-associated-consts",
	"dyn compatibility": "dyn-compatibility", "fields": "fields", "variants": "variants", "re-exports": "reexports",
	"modules": "modules", "macros": "macros", "structs": "structs", "enums": "enums", "functions": "functions",
	"traits": "traits", "type aliases": "types", "constants": "constants", "statics": "statics", "unions": "unions",
}

// rustdocQualifierRe matches the qualifiers before an item's keyword.
var rustdocQualifierRe = regexp.MustCompile(`^(?:pub(?:\([^)]*\))?|const|async|unsafe|default|extern(?:\s+"[^"]*")?)\s+`)

// rustdocID returns the id rustdoc gives a heading on a docs.rs page, or
// "" for an ordinary doc heading (slugged as github-slugger would).
func rustdocID(h atxHeading, ancestors []string) string {
	text := h.text
	if h.level == 2 {
		return rustdocSectionIDs[strings.ToLower(text)]
	}
	if text == "impl" || strings.HasPrefix(text, "impl ") || strings.HasPrefix(text, "impl<") || strings.HasPrefix(text, "impl!") {
		return rustdocImplID(text)
	}
	if len(ancestors) > 0 && strings.EqualFold(ancestors[len(ancestors)-1], "variants") {
		if name := leadingIdent(text); name != "" {
			return "variant." + name
		}
	}
	kind, name := rustdocItem(text)
	required := slices.ContainsFunc(ancestors, func(a string) bool { return strings.HasPrefix(strings.ToLower(a), "required ") })
	switch {
	case name == "":
		return ""
	case kind == "fn" && required:
		return "tymethod." + name
	case kind == "fn":
		return "method." + name
	case kind == "type":
		return "associatedtype." + name
	case kind == "const":
		return "associatedconstant." + name
	}
	return ""
}

// rustdocItem reads an item heading, "pub const fn new() -> Self", as its
// keyword and name ("fn", "new"), qualifiers aside.
func rustdocItem(text string) (kind, name string) {
	for {
		loc := rustdocQualifierRe.FindStringIndex(text)
		if loc == nil || strings.HasPrefix(text, "const ") && !strings.HasPrefix(text, "const fn ") {
			break
		}
		text = text[loc[1]:]
	}
	kind, rest, ok := strings.Cut(text, " ")
	if !ok {
		return "", ""
	}
	return kind, leadingIdent(rest)
}

// rustdocImplID is rustdoc's id for an impl block's heading:
// "impl<T> From<T> for Body where …" is impl-From%3CT%3E-for-Body, the
// impl's own generics, a negative impl's "!" and the where clause left
// out, and the rest URL-encoded the way rustdoc's small_url_encode does.
func rustdocImplID(text string) string {
	s := strings.TrimSpace(strings.TrimPrefix(text, "impl"))
	if strings.HasPrefix(s, "<") {
		s = s[closingAngle(s):]
	}
	s = strings.TrimSpace(s)
	if i := topLevelIndex(s, " where "); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(strings.TrimPrefix(s, "!"))
	if i := topLevelIndex(s, " for "); i >= 0 {
		return "impl-" + rustdocEncode(s[:i]) + "-for-" + rustdocEncode(strings.TrimSpace(s[i+5:]))
	}
	return "impl-" + rustdocEncode(s)
}

// closingAngle returns the index just past the ">" closing the "<" at
// s[0], or len(s).
func closingAngle(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			if depth--; depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

// topLevelIndex is strings.Index outside angle brackets and parentheses.
func topLevelIndex(s, sub string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			depth--
		}
		if depth == 0 && strings.HasPrefix(s[i:], sub) {
			return i
		}
	}
	return -1
}

// rustdocEncode is rustdoc's small_url_encode: URL code points that need
// no escaping stay, a space becomes "+", anything else is %XX per byte.
func rustdocEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.,~!'()*/;:?=", c) >= 0:
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// leadingIdent returns the identifier s starts with.
func leadingIdent(s string) string {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (s[i] == '_' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || i > 0 && s[i] >= '0' && s[i] <= '9') {
		i++
	}
	return s[:i]
}

// pkgsiteSections are pkg.go.dev's documentation sections (pkg-<name>).
var pkgsiteSections = map[string]bool{"overview": true, "index": true, "examples": true, "constants": true, "variables": true, "functions": true, "types": true, "notes": true}

// pkgsiteTopIDs are the ids of pkg.go.dev's top-level headings.
var pkgsiteTopIDs = map[string]string{"README": "section-readme", "Documentation": "section-documentation", "Source Files": "section-sourcefiles", "Directories": "section-directories", "Links": "links-heading"}

var (
	// goDeclRe matches a pkg.go.dev declaration heading: "func Name",
	// "func (r *Recv[T]) Name", "type Name", each perhaps "added in v1.2.0".
	goDeclRe     = regexp.MustCompile(`^(?:func\s+(?:\(\s*(?:\w+\s+)?\*?\s*(\w+)(?:\[[^\]]*\])?\s*\)\s*)?|type\s+)(\w+)`)
	goAddedInRe  = regexp.MustCompile(`\s+added in v\S+$`)
	readmeDashRe = regexp.MustCompile(`-{2,}`)
)

// pkgsiteID returns the id pkg.go.dev gives a heading, or "".
func pkgsiteID(h atxHeading, ancestors []string) string {
	if h.level == 2 {
		return pkgsiteTopIDs[h.text]
	}
	section := ""
	for _, a := range ancestors {
		switch a {
		case "README":
			section = "readme"
		case "Documentation":
			section = "doc"
		}
	}
	text := goAddedInRe.ReplaceAllString(h.text, "")
	switch section {
	case "readme":
		return "readme-" + readmeSlug(text)
	case "doc":
		if l := strings.ToLower(text); h.level == 3 && pkgsiteSections[l] {
			return "pkg-" + l
		}
		if m := goDeclRe.FindStringSubmatch(text); m != nil {
			if m[1] != "" {
				return m[1] + "." + m[2]
			}
			return m[2]
		}
		return goHeadingID(text)
	}
	return ""
}

// readmeSlug is pkg.go.dev's README heading id after its "readme-"
// prefix: ASCII letters and digits lower-cased, spaces, hyphens and
// underscores as single hyphens, anything else dropped.
func readmeSlug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(readmeDashRe.ReplaceAllString(b.String(), "-"), "-")
}

// goHeadingID is go/doc's heading id: "hdr-", then the text with every
// character but an ASCII letter, digit or underscore as "_".
func goHeadingID(text string) string {
	var b strings.Builder
	b.WriteString("hdr-")
	for _, r := range strings.TrimSpace(text) {
		if r < 0x80 && (r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Sectioning thresholds. A section whose text, subsections included,
// runs past webSplitBytes is split at its deeper headings too; a subsection
// named like part of an API entry (Parameters, Returns, Errors) stays
// attached while under webAttachBytes.
const (
	webSplitBytes  = 4000
	webAttachBytes = 3000
)

// headingBlock is one heading and the lines under it up to the next
// heading of any level; block 0 is the page's intro.
type headingBlock struct {
	level  int // 0 for the intro
	h      atxHeading
	line   string // the heading line as written
	anchor string
	source anchorSource
	parent int // the enclosing block; -1 for the intro
	lines  []string
	size   int  // bytes of the heading, its lines and every subsection
	split  bool // starts a section of its own
}

// splitSections splits a page into sections at ATX headings outside code
// fences (a heading may open a list item). A leading H1 is the page title
// (it beats an HTML <title> that carries a site suffix), and the text
// before the first heading is the intro, linked to the page itself.
//
// H1–H3 always start a section. A deeper heading starts one when it is an
// API entry (a code or signature heading: Django's `select_related()`,
// pkg.go.dev's func and type entries, docs.rs methods, serde.rs
// attributes) or when the section holding it is larger than webSplitBytes,
// except a small Parameters/Returns-style subsection, which stays with its
// entry, and anything under a heading that stayed inline. A section's text
// is its own lines plus those of every subsection that did not split off,
// headings included, in page order; its breadcrumbs are the page title and
// every enclosing heading. Near-empty sections are dropped.
//
// Anchors follow the site: an id the Markdown carries (an {#id}, a
// permalink in or just before the heading) wins, else the page's generator
// rule (pageAnchorStyle), else github-slugger. Every heading claims its
// anchor in page order, so duplicates get the suffixes the site gives
// them. Permalink and source-link decorations are stripped from the text.
func splitSections(p webPage, pageIndex int) []webSection {
	title, blocks := parseBlocks(p)
	markSplits(blocks)
	verifyAnchors(blocks, p.Anchors)
	texts := make([][]string, len(blocks))
	owner := make([]int, len(blocks))
	for i := range blocks {
		b := &blocks[i]
		owner[i] = i
		if !b.split {
			owner[i] = owner[b.parent]
			texts[owner[i]] = append(texts[owner[i]], b.line)
		}
		texts[owner[i]] = append(texts[owner[i]], b.lines...)
	}
	var out []webSection
	for i, b := range blocks {
		if !b.split {
			continue
		}
		raw := strings.TrimSpace(strings.Join(cleanSectionLines(texts[i]), "\n"))
		text := linkLabels(raw)
		if len(strings.Join(strings.Fields(text), "")) < webMinSectionText {
			continue
		}
		sec := webSection{page: pageIndex, order: len(out), pageURL: p.URL, heading: title, crumbs: []string{title}, text: text, links: raw}
		if i > 0 {
			sec.heading, sec.anchor, sec.source = b.h.text, b.anchor, b.source
			sec.crumbs = append(sec.crumbs, blockCrumbs(blocks, i)...)
		}
		if boilerplateSection(sec.crumbs) {
			continue
		}
		out = append(out, sec)
	}
	return out
}

// verifyAnchors makes every heading's anchor one the page carries, when
// the fetcher read the page's targets (anchors non-nil); a page read from
// Markdown alone keeps the anchors its rules gave. A heading takes, in
// order: its own anchor when the page has that id for a heading with the
// same words (or for no heading at all); else the id of the page heading
// whose words it shares, matched in page order so a repeated heading
// ("Note", "Parameters") takes its own occurrence; else its own anchor
// when the page has the id at all; else its enclosing heading's anchor,
// so a heading the page gives no target (a callout's "Pitfall", a
// badge-prefixed option whose id the generator slugs differently and
// whose text differs) still cites the nearest section that exists, and
// at worst the page itself. Every anchor is then fixed: link evidence
// (adoptLinkedAnchors) cannot improve on the page's own ids.
func verifyAnchors(blocks []headingBlock, anchors []PageAnchor) {
	if anchors == nil {
		return
	}
	ids := make(map[string]int, len(anchors)) // id → its first position
	byWords := map[string][]int{}             // heading words → positions in anchors
	for i, a := range anchors {
		if _, ok := ids[a.ID]; !ok {
			ids[a.ID] = i
		}
		if words := idWords(a.Text); words != "" {
			byWords[words] = append(byWords[words], i)
		}
	}
	used := map[int]bool{}
	cursor := -1
	for i := 1; i < len(blocks); i++ {
		b := &blocks[i]
		words := idWords(b.h.text)
		at, have := ids[b.anchor]
		own := ""
		if have {
			own = idWords(anchors[at].Text)
		}
		switch pos := matchHeading(byWords[words], used, cursor); {
		case have && own == words && words != "":
			used[at], cursor = true, at
		case have && own == "":
		case pos >= 0:
			used[pos], cursor = true, pos
			b.anchor = anchors[pos].ID
		case have:
		default:
			b.anchor = blocks[b.parent].anchor // the intro's is "": the page
		}
		b.source = anchorFixed
	}
}

// matchHeading picks the page heading a Markdown heading stands for among
// candidates (positions in page order): the first unused one after cursor,
// the last match, else the first unused one; -1 when none is left.
func matchHeading(candidates []int, used map[int]bool, cursor int) int {
	first := -1
	for _, pos := range candidates {
		if used[pos] {
			continue
		}
		if pos > cursor {
			return pos
		}
		if first < 0 {
			first = pos
		}
	}
	return first
}

// parseBlocks reads a page's title and its heading blocks, anchors
// assigned.
func parseBlocks(p webPage) (string, []headingBlock) {
	title := p.Title
	style := pageAnchorStyle(p)
	slugs := slugger{}
	blocks := []headingBlock{{parent: -1}}
	stack := []int{0}
	var fence fenceState
	seenHeading := false
	for _, line := range strings.Split(strings.ReplaceAll(p.Markdown, "\r\n", "\n"), "\n") {
		cur := &blocks[stack[len(stack)-1]]
		if fence.step(line) {
			cur.lines = append(cur.lines, line)
			continue
		}
		h, ok := parseHeadingLine(line)
		if !ok || h.text == "" {
			cur.lines = append(cur.lines, line)
			continue
		}
		if h.id == "" {
			cur.lines, h.id = takePermalinkLine(cur.lines)
		}
		if h.level == 1 && !seenHeading {
			seenHeading = true
			title = h.text
			claimAnchor(slugs, h)
			continue
		}
		seenHeading = true
		for len(stack) > 1 && blocks[stack[len(stack)-1]].level >= h.level {
			stack = stack[:len(stack)-1]
		}
		ancestors := make([]string, 0, len(stack))
		for _, i := range stack[1:] {
			ancestors = append(ancestors, blocks[i].h.text)
		}
		b := headingBlock{level: h.level, h: h, line: line, parent: stack[len(stack)-1]}
		b.anchor, b.source = style.anchor(slugs, h, ancestors)
		blocks = append(blocks, b)
		stack = append(stack, len(blocks)-1)
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		b := &blocks[i]
		b.size += len(b.line)
		for _, l := range b.lines {
			b.size += len(l) + 1
		}
		if b.parent >= 0 {
			blocks[b.parent].size += b.size
		}
	}
	return title, blocks
}

// markSplits decides which blocks start a section; parents come before
// their subsections, so each decision can read its parent's.
func markSplits(blocks []headingBlock) {
	blocks[0].split = true
	for i := 1; i < len(blocks); i++ {
		b := &blocks[i]
		parent := blocks[b.parent]
		holder := b.parent
		for !blocks[holder].split {
			holder = blocks[holder].parent
		}
		switch {
		case b.level <= 3:
			b.split = true
		case !parent.split:
			b.split = false // a subsection of an inline subsection stays inline
		case isAttachment(b.h.text) && b.size < webAttachBytes:
			b.split = false
		default:
			b.split = isAPIEntry(b.h) || blocks[holder].size > webSplitBytes
		}
	}
}

// blockCrumbs returns the headings enclosing block i, outermost first,
// ending with its own.
func blockCrumbs(blocks []headingBlock, i int) []string {
	var crumbs []string
	for ; i > 0; i = blocks[i].parent {
		crumbs = append(crumbs, blocks[i].h.text)
	}
	slices.Reverse(crumbs)
	return crumbs
}

// attachmentHeadings name the parts of one API entry or example: kept
// with the entry when small.
var attachmentHeadings = map[string]bool{
	"parameters": true, "params": true, "arguments": true, "args": true, "returns": true, "return": true,
	"return value": true, "return values": true, "return type": true, "raises": true, "throws": true,
	"exceptions": true, "errors": true, "panics": true, "safety": true, "example": true, "examples": true,
	"note": true, "notes": true, "caveats": true, "see also": true, "yields": true, "type parameters": true,
	"options": true, "optional": true, "usage": true, "signature": true, "deprecated": true, "warning": true,
	"since": true, "props": true, "properties": true, "fields": true, "attributes": true, "default": true,
	"defaults": true, "type": true, "returns value": true, "output": true, "result": true, "results": true,
}

func isAttachment(text string) bool {
	return attachmentHeadings[strings.ToLower(strings.TrimRight(strings.TrimSpace(text), ":"))]
}

// apiKeywordRe matches a heading that opens with a declaration keyword:
// pkg.go.dev's "func Name" and "type Name", docs.rs's "pub fn" and "impl".
var apiKeywordRe = regexp.MustCompile(`^(?:pub(?:\([a-z]+\))?|fn|func|type|impl|def|class|method|function|const|var|let|struct|enum|trait|interface|async|static|unsafe|extern|macro|property|attribute|module|package|union|mod)\b[ <(]`)

// apiNameRe matches a heading that is a call or a qualified name:
// select_related(), useEffect(setup, deps?), vi.mock, QuerySet.filter.
var apiNameRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*(?:(?:\.|::|#|->)[A-Za-z_$][\w$]*)*\s*(\(.*\))?\??$`)

// isAPIEntry reports a heading that names one API entry: a heading that
// is code (`select_related()`, `#[serde(rename)]`), opens with a
// declaration keyword, or is a call or qualified, snake or camel case
// name.
func isAPIEntry(h atxHeading) bool {
	if strings.HasPrefix(strings.TrimSpace(h.raw), "`") || apiKeywordRe.MatchString(h.text) {
		return true
	}
	m := apiNameRe.FindStringSubmatch(h.text)
	if m == nil {
		return false
	}
	return m[1] != "" || strings.ContainsAny(h.text, "._:#") || isCamel(h.text)
}

// isCamel reports a lower-case letter followed by an upper-case one.
func isCamel(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] >= 'a' && s[i-1] <= 'z' && s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}

// claimAnchor returns the heading's anchor, registering it so a later
// heading with the same text gets the -1 suffix the site would give it.
func claimAnchor(slugs slugger, h atxHeading) string {
	if h.id != "" {
		return slugs.claim(h.id)
	}
	return slugs.slug(h.text)
}

// permalinkLineRe matches a line made only of a heading's decorations,
// as docs.rs renders the line before each impl and variant heading:
// "[Source](…/src/…)[§](…#impl-Body)", or "[§](…#variant.Null)".
var permalinkLineRe = regexp.MustCompile(`^\s*(?:\[(?:Source|source|\\\[source\\\])\]\([^)\s]*\)\s*)?\[(?:§|¶|#|🔗)\]\([^)\s#]*#([^)\s]+)(?:\s+"[^"]*")?\)\s*$`)

// takePermalinkLine removes a permalink-only line (blank lines aside) from
// the end of lines and returns the id it links to, or "".
func takePermalinkLine(lines []string) ([]string, string) {
	i := len(lines) - 1
	for i >= 0 && strings.TrimSpace(lines[i]) == "" {
		i--
	}
	if i < 0 {
		return lines, ""
	}
	m := permalinkLineRe.FindStringSubmatch(lines[i])
	if m == nil {
		return lines, ""
	}
	return lines[:i], fragmentID(m[1])
}

// fragmentID is the element id a link's fragment names. The scrape
// pipeline percent-encodes a non-ASCII id when it resolves the link, so
// those bytes are decoded; an ASCII escape is left as it is, since it may
// be part of the id itself (rustdoc's impl-From%3CFile%3E-for-Body).
func fragmentID(frag string) string {
	if !strings.Contains(frag, "%") {
		return frag
	}
	var b strings.Builder
	for i := 0; i < len(frag); i++ {
		if frag[i] == '%' && i+2 < len(frag) {
			if v, err := strconv.ParseUint(frag[i+1:i+3], 16, 8); err == nil && v >= 0x80 {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(frag[i])
	}
	return b.String()
}

var (
	// glyphLinkRe matches a permalink: a link whose label is only a glyph
	// (¶, §, #, 🔗, a zero-width space) or nothing, with an optional title
	// ("Link to this definition", "Go to Example").
	glyphLinkRe = regexp.MustCompile(` ?\[(?:¶|§|#|🔗|\x{200b}|)\]\([^)\s]*(?:\s+"[^"]*")?\)`)
	// sourceLinkRe matches a link to a source listing, labelled source or
	// [source] (Sphinx viewcode, rustdoc's Source).
	sourceLinkRe = regexp.MustCompile(`(?i) ?\[(?:\\?\[)?source(?:\\?\])?\]\(([^)\s]*)(?:\s+"[^"]*")?\)`)
	// codeMarkerRe matches VitePress/Shiki line annotations in code:
	// "// [!code ++]", "# [!code focus]", "<!-- [!code hl] -->".
	codeMarkerRe = regexp.MustCompile(`\s*(?://|#|<!--|/\*)\s*\[!code ([^\]]*)\]\s*(?:-->|\*/)?`)
)

// cleanSectionLines strips decorations from a section's lines: permalink
// glyph links and source-listing links outside code (a line left empty by
// that is dropped), and VitePress line annotations inside it, where a line
// marked removed ([!code --]) goes with its marker, so the code reads as
// the page recommends it. Real links are kept.
func cleanSectionLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	var fence fenceState
	for _, line := range lines {
		if fence.step(line) {
			if m := codeMarkerRe.FindStringSubmatch(line); m != nil {
				if strings.TrimSpace(m[1]) == "--" {
					continue
				}
				line = codeMarkerRe.ReplaceAllString(line, "")
			}
			out = append(out, line)
			continue
		}
		cleaned := glyphLinkRe.ReplaceAllString(line, "")
		cleaned = sourceLinkRe.ReplaceAllStringFunc(cleaned, func(link string) string {
			if isSourceListing(sourceLinkRe.FindStringSubmatch(link)[1]) {
				return ""
			}
			return link
		})
		if cleaned != line && strings.TrimSpace(cleaned) == "" {
			continue
		}
		out = append(out, strings.TrimRight(cleaned, " \t"))
	}
	return out
}

// isSourceListing reports a link target that is a source-code view.
func isSourceListing(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return false
	}
	segs := pathSegments(u.Path)
	return slices.Contains(segs, "_modules") || slices.Contains(segs, "src") || slices.Contains(segs, "blob") || slices.Contains(segs, "source")
}

// linkLabels reduces the Markdown links and images in md's prose to what
// a reader sees of them: a link [label](target "title") to its label, an
// image ![alt](src) to its alt text. Code (fenced blocks, code spans) is
// left as written. The targets are mostly absolute URLs to other pages:
// a result already cites its own page, and the URLs would cost an
// assistant tokens and pull their words into ranking. A label may hold
// brackets of its own ("[`#[command(subcommand)]` attribute](…)"), as
// CommonMark allows when they balance.
func linkLabels(md string) string {
	if !strings.Contains(md, "](") {
		return md
	}
	lines := strings.Split(md, "\n")
	var fence fenceState
	for i, line := range lines {
		if fence.step(line) || !strings.Contains(line, "](") {
			continue
		}
		lines[i] = lineLinkLabels(line)
	}
	return strings.Join(lines, "\n")
}

// lineLinkLabels is linkLabels for one line of prose, skipping code spans.
func lineLinkLabels(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		switch c := line[i]; {
		case c == '\\' && i+1 < len(line):
			b.WriteString(line[i : i+2])
			i += 2
			continue
		case c == '`':
			n := backtickRun(line, i)
			end := closingBackticks(line, i+n, n)
			if end < 0 {
				b.WriteString(line[i : i+n])
				i += n
				continue
			}
			b.WriteString(line[i : end+n])
			i = end + n
			continue
		case c == '[' || c == '!' && i+1 < len(line) && line[i+1] == '[':
			open := i
			if c == '!' {
				open++
			}
			if label, next, ok := inlineLink(line, open); ok {
				b.WriteString(label)
				i = next
				continue
			}
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

// inlineLink parses an inline link whose "[" is at line[open]: the label
// (brackets balanced, code spans opaque) and the index just past the
// destination's ")". The destination may hold balanced parentheses and a
// quoted title.
func inlineLink(line string, open int) (label string, next int, ok bool) {
	end := closingBracket(line, open)
	if end < 0 || end+1 >= len(line) || line[end+1] != '(' {
		return "", 0, false
	}
	if next = closingParen(line, end+1); next < 0 {
		return "", 0, false
	}
	return line[open+1 : end], next, true
}

// closingBracket returns the index of the "]" that balances the "[" at
// line[open], skipping escapes and code spans; -1 when none does.
func closingBracket(line string, open int) int {
	depth := 0
	for i := open; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '`':
			n := backtickRun(line, i)
			if end := closingBackticks(line, i+n, n); end >= 0 {
				i = end + n - 1
			} else {
				i += n - 1
			}
		case '[':
			depth++
		case ']':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// closingParen returns the index just past the ")" that balances the "("
// at line[open], with a quoted title (after a space) opaque; -1 when none
// does.
func closingParen(line string, open int) int {
	parens, quote := 0, byte(0)
	for j := open; j < len(line); j++ {
		c := line[j]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' && line[j-1] == ' ':
			quote = c
		case c == '(':
			parens++
		case c == ')':
			if parens--; parens == 0 {
				return j + 1
			}
		}
	}
	return -1
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

// boilerplateHeadingRe matches the headings docs platforms put on every
// page that are no part of it: instructions to AI agents, page-rating
// and edit prompts, cookie notices.
var boilerplateHeadingRe = regexp.MustCompile(`(?i)^\s*(?:agent instructions|instructions for (?:ai )?agents|querying this documentation|was this (?:page|article|doc|document|section|content) helpful\??|was this helpful\??|did this (?:page|article|doc) help(?: you)?\??|is this page helpful\??|rate this (?:page|article)|edit this page(?: on github)?|edit on github|suggest (?:an )?edits?|cookies?(?: notice| policy| preferences| settings| consent)?|we use cookies.*|manage cookies)\s*$`)

// boilerplateSection reports a section under such a heading (its own or
// one above it, below the page title).
func boilerplateSection(crumbs []string) bool {
	for _, c := range crumbs[min(1, len(crumbs)):] {
		if boilerplateHeadingRe.MatchString(c) {
			return true
		}
	}
	return false
}
