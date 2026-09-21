package extract

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Mode selects how much of a page the extractor removes. Both modes keep
// everything the page's structure marks as content and drop what structure
// marks as chrome; they differ on blocks that read like content but are
// named or phrased as site furniture.
type Mode string

const (
	// ModeComplete removes chrome by structure alone: hidden elements,
	// landmarks, controls, link-dense blocks, repeated cards. Nothing is
	// dropped for what it is called, so it holds on sites and in languages
	// nobody tuned for. The default.
	ModeComplete Mode = "complete"
	// ModeClean also drops blocks whose class, id or heading names them as
	// furniture: newsletter cards, related-link rails, in-page tables of
	// contents, author boxes, comment threads. Tidier on most sites; a site
	// that names its own content "sidebar" loses it.
	ModeClean Mode = "clean"
)

// Modes lists the accepted mode names, the default first.
var Modes = []string{string(ModeComplete), string(ModeClean)}

// ParseMode reads a configured mode name; empty selects ModeComplete.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", string(ModeComplete):
		return ModeComplete, nil
	case string(ModeClean):
		return ModeClean, nil
	}
	return "", fmt.Errorf("extract mode must be %q or %q, got %q", ModeComplete, ModeClean, s)
}

// semanticExtract trusts the page's own landmarks before scoring anything.
// A single <main>, [role=main] or <article> is what the author said the
// content is; readability's candidate scoring exists for pages that don't
// say. Inside that root only chrome is removed — by what it is, not by
// score — so nothing the author wrote is lost to a density heuristic. ok is
// false when the page declares no structure to trust, and the caller falls
// back to readability.
func semanticExtract(rawHTML, baseURL string, mode Mode) (*Result, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return nil, false
	}
	formulas := mathToText(doc)
	fixLazyImages(doc)
	rawTitle := strings.TrimSpace(doc.Find("title").First().Text())

	root := semanticRoot(doc)
	if root == nil {
		root = uniformSectionRoot(doc)
	}
	if root == nil {
		root = proseRoot(doc, mode)
	}
	if root == nil {
		return nil, false
	}
	pruneChrome(root, doc, baseURL, mode)
	rescueTitle(doc, root)

	// A cell re-parsed on its own loses its tag and keeps its content; hand
	// the content over directly rather than rely on that.
	var outer string
	if root.Is("td, th") {
		outer, err = root.Html()
	} else {
		outer, err = goquery.OuterHtml(root)
	}
	if err != nil {
		return nil, false
	}
	// The document was normalized before parsing; the root is a subtree of it.
	markdown, err := markdownConverter.ConvertString(outer, converter.WithDomain(baseURL))
	if err != nil {
		return nil, false
	}
	markdown = spliceMath(tidyMarkdown(markdown), formulas)
	if strings.TrimSpace(markdown) == "" {
		return nil, false
	}
	return &Result{Title: cleanTitle(doc, rawTitle), Markdown: markdown}, true
}

var rxTitleSep = regexp.MustCompile(`\s+[|\-–—»/>]\s+`)

// cleanTitle names the page the way its author did: the h1 when <title>
// starts with it, else <title> without the site name that follows the last
// separator — unless that leaves too little, when the site name came first.
func cleanTitle(doc *goquery.Document, raw string) string {
	raw = strings.Join(strings.Fields(raw), " ")
	want := normalizeText(raw)
	h1 := ""
	doc.Find("h1").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		t := strings.Join(strings.Fields(h.Text()), " ")
		if n := normalizeText(t); n != "" && len(strings.Fields(n)) <= 30 && strings.HasPrefix(want, n) {
			h1 = t
		}
		return h1 == ""
	})
	if h1 != "" {
		return h1
	}
	seps := rxTitleSep.FindAllStringIndex(raw, -1)
	if len(seps) == 0 {
		return raw
	}
	if before := strings.TrimSpace(raw[:seps[len(seps)-1][0]]); wordsIn(before) >= 3 {
		return before
	}
	if after := strings.TrimSpace(raw[seps[0][1]:]); wordsIn(after) >= 2 {
		return after
	}
	return raw
}

// semanticRoot returns the landmark the page declares as its content, or nil
// when the page declares none. Several <main> elements (a site that nests
// them, or reuses the tag for a hero) resolve to the wordiest; an <article> is
// trusted only when it carries a real share of the page, since teasers and
// comment threads use the tag too.
func semanticRoot(doc *goquery.Document) *goquery.Selection {
	bodyWords := wordCount(doc.Find("body"))
	for _, sel := range []string{"main", "[role=main]"} {
		if best := wordiest(doc.Find(sel)); best != nil && wordCount(best) >= 40 {
			return best
		}
	}
	if best := wordiest(doc.Find("article")); best != nil {
		if w := wordCount(best); w >= 40 && (bodyWords == 0 || float64(w)/float64(bodyWords) >= 0.3) && !oneOfMany(best, w) {
			return best
		}
	}
	return nil
}

// oneOfMany reports whether an article is one entry of a listing: sibling
// articles with substance of their own hold more of the words between them
// than the teasers beside a story would. A blog index, a section front or
// a changelog wraps each entry in <article>; the wordiest is not the page,
// and the section and prose rules find the container that is.
func oneOfMany(article *goquery.Selection, words int) bool {
	total := words
	article.Siblings().Filter("article").Each(func(_ int, s *goquery.Selection) {
		if w := wordCount(s); w >= 40 {
			total += w
		}
	})
	return total > words && float64(words)/float64(total) < 0.6
}

// uniformSectionRoot finds a document assembled from uniform sibling sections
// — asciidoc's div.sect1, DocBook's div.sect1, a hand-rolled <section> per
// chapter — each opening with a heading. Readability scores one section and
// merges siblings only above a threshold, so a manual with sixteen sections
// comes back as its longest one; a container whose children are all shaped
// the same way is the document, not a candidate.
func uniformSectionRoot(doc *goquery.Document) *goquery.Selection {
	bodyWords := wordCount(doc.Find("body"))
	var best *goquery.Selection
	bestWords := 0
	doc.Find("body *").Each(func(_ int, el *goquery.Selection) {
		if el.Is("nav, footer, header, aside, ul, ol, table, pre") {
			return
		}
		groups := map[string]int{}
		kids := el.Children()
		if kids.Length() < 3 {
			return
		}
		kids.Each(func(_ int, k *goquery.Selection) {
			if k.Find("h1, h2, h3, h4, h5, h6").Length() == 0 {
				return
			}
			class, _ := k.Attr("class")
			groups[goquery.NodeName(k)+"."+strings.Join(strings.Fields(class), ".")]++
		})
		uniform := false
		for _, n := range groups {
			if n >= 3 {
				uniform = true
			}
		}
		if !uniform {
			return
		}
		if w := wordCount(el); w > bestWords {
			best, bestWords = el, w
		}
	})
	if best == nil || bestWords < 100 || (bodyWords > 0 && float64(bestWords)/float64(bodyWords) < 0.4) {
		return nil
	}
	return best
}

// proseRoot finds the smallest element holding the page's paragraphs, for
// pages that declare no landmark and have no uniform sections. The descent
// leaves a sibling behind only when it looks like chrome — a link-dense
// band, a named block, a landmark, a few words — so a story is not dropped
// for its comments, nor a sidebar of facts for the column beside it. It
// stops at the first element with headings of its own: that is document
// structure, and everything under it belongs together.
func proseRoot(doc *goquery.Document, mode Mode) *goquery.Selection {
	body := doc.Find("body")
	if body.Length() == 0 || paragraphWords(body.Nodes[0]) < 100 {
		return nil
	}
	title := pageTitleOf(doc)
	root := body
	for {
		next, _ := proseStep(root, title, mode)
		if next == nil {
			break
		}
		root = next
	}
	if root.Nodes[0] == body.Nodes[0] && root.ChildrenFiltered("h1, h2, h3, h4, h5, h6").Length() == 0 {
		return nil
	}
	// A row on its own does not survive re-parsing; hand over the table.
	if root.Is("tr, tbody, thead, tfoot") {
		if t := root.Closest("table"); t.Length() > 0 {
			return t
		}
	}
	return root
}

// proseStep picks the child of root the descent continues into, or nil
// and the reason it stops at root.
func proseStep(root *goquery.Selection, title pageTitle, mode Mode) (*goquery.Selection, string) {
	if root.ChildrenFiltered("h1, h2, h3, h4, h5, h6").Length() > 0 {
		return nil, "heading children"
	}
	var next *goquery.Selection
	nextWords, childWords := 0, 0
	root.Children().Each(func(_ int, c *goquery.Selection) {
		w := paragraphWords(c.Nodes[0])
		childWords += w
		if w > nextWords {
			next, nextWords = c, w
		}
	})
	if next == nil || next.Is("p, pre, dd, dt, blockquote") {
		return nil, "leaf"
	}
	// An element with a paragraph of its own text is content, not a
	// wrapper: an essay set straight into a cell is not traded for the
	// footnote block inside it.
	if paragraphWords(root.Nodes[0]) > childWords {
		return nil, "own text"
	}
	kept, above := "", true
	root.Children().EachWithBreak(func(_ int, c *goquery.Selection) bool {
		if c.Nodes[0] == next.Nodes[0] {
			above = false
			return true
		}
		if !droppable(c, title, above, mode) {
			kept = goquery.NodeName(c)
		}
		return kept == ""
	})
	if kept != "" {
		return nil, "sibling kept: " + kept
	}
	return next, ""
}

// droppable reports whether a block left outside the prose root would be
// chrome anyway. A block above the content carrying the page's title is
// never chrome: the story line above a comment thread is the story. The
// same words below the content are a link back.
func droppable(c *goquery.Selection, title pageTitle, above bool, mode Mode) bool {
	if c.Is("nav, footer, header, aside, form, [role=navigation], [role=banner], [role=contentinfo], [role=complementary], [hidden], [aria-hidden=true]") || (mode == ModeClean && chromeNamed(c)) {
		return true
	}
	if above && holdsTitle(c, title) {
		return false
	}
	// A data table or a listing is content whatever its word count, and a
	// results table whose every cell is a link is not a menu.
	if c.Find("pre").Length() > 0 || hasDataTable(c) {
		return false
	}
	return paragraphWords(c.Nodes[0]) < 20 || linkDensity(c) > 0.5
}

func hasDataTable(c *goquery.Selection) bool {
	found := false
	c.Find("table").EachWithBreak(func(_ int, t *goquery.Selection) bool {
		found = isDataTable(t)
		return !found
	})
	return found
}

// pageTitle is the page's own title, normalized, as <title> and og:title
// give it.
type pageTitle struct{ full, og string }

func pageTitleOf(doc *goquery.Document) pageTitle {
	t := pageTitle{full: normalizeText(doc.Find("title").First().Text())}
	if og, ok := doc.Find("meta[property='og:title']").Attr("content"); ok {
		t.og = normalizeText(og)
	}
	return t
}

// holdsTitle reports whether some element in c reads as the page's title:
// equal to og:title, or the start of <title> and most of it — site names
// come last. Three words at least, so a stray "The" does not count.
func holdsTitle(c *goquery.Selection, t pageTitle) bool {
	if t.full == "" && t.og == "" {
		return false
	}
	titleWords := len(strings.Fields(t.full))
	found := false
	c.Find("*").AddSelection(c).EachWithBreak(func(_ int, e *goquery.Selection) bool {
		text := normalizeText(e.Text())
		n := len(strings.Fields(text))
		if n < 3 || n > 40 {
			return true
		}
		if (t.og != "" && text == t.og) || (t.full != "" && strings.HasPrefix(t.full, text) && n*2 >= titleWords) {
			found = true
		}
		return !found
	})
	return found
}

var paragraphTags = map[string]bool{"p": true, "pre": true, "dd": true, "dt": true, "td": true, "th": true, "blockquote": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "figcaption": true}

var inlineTags = map[string]bool{"a": true, "abbr": true, "b": true, "bdi": true, "bdo": true, "br": true, "cite": true, "code": true, "data": true, "dfn": true, "em": true, "font": true, "i": true, "img": true, "kbd": true, "mark": true, "q": true, "s": true, "samp": true, "small": true, "span": true, "strong": true, "sub": true, "sup": true, "time": true, "u": true, "var": true, "wbr": true}

// paragraphWords counts the words of prose under node: paragraph-like
// elements, bullets that are not menu items, and any run of inline text
// long enough to be a paragraph wherever it sits — old pages set prose
// straight into a cell or a font tag, broken only by links and emphasis.
// Navigation is short list items and links, and counts for nothing.
func paragraphWords(node *html.Node) int {
	if node.Type == html.TextNode {
		return 0
	}
	if node.Type == html.ElementNode {
		switch node.Data {
		case "script", "style", "template", "noscript", "svg", "nav", "footer":
			return 0
		case "li":
			w := countWords(node)
			if w >= 20 || (w > 0 && float64(linkWords(node))/float64(w) < 0.5) {
				return w
			}
			return 0
		}
		if paragraphTags[node.Data] {
			return countWords(node)
		}
	}
	n, run := 0, 0
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		inline := c.Type == html.TextNode || (c.Type == html.ElementNode && inlineTags[c.Data])
		if inline {
			run += countWords(c)
			continue
		}
		if run >= 20 {
			n += run
		}
		run = 0
		n += paragraphWords(c)
	}
	if run >= 20 {
		n += run
	}
	return n
}

// rescueTitle prepends the page's h1 when the chosen root lacks one and a
// single h1 elsewhere on the page is the page title: Drupal and WordPress
// themes put the title in a header band above the content block.
func rescueTitle(doc *goquery.Document, root *goquery.Selection) {
	if root.Find("h1").Length() > 0 {
		return
	}
	h1s := doc.Find("h1")
	if h1s.Length() != 1 {
		return
	}
	text := strings.Join(strings.Fields(h1s.Text()), " ")
	if text == "" || len(strings.Fields(text)) > 30 || !pageTitleMatches(doc, text) {
		return
	}
	root.PrependHtml("<h1>" + html.EscapeString(text) + "</h1>")
}

// pageTitleMatches reports whether text is the page's own title: equal to
// og:title, or the start of <title> (site names come last).
func pageTitleMatches(doc *goquery.Document, text string) bool {
	want := normalizeText(text)
	if og, ok := doc.Find("meta[property='og:title']").Attr("content"); ok && normalizeText(og) == want {
		return true
	}
	title := normalizeText(doc.Find("title").First().Text())
	return title != "" && strings.HasPrefix(title, want)
}

// wordiest picks the element carrying the most words.
func wordiest(sel *goquery.Selection) *goquery.Selection {
	var best *goquery.Selection
	bestWords := -1
	sel.Each(func(_ int, s *goquery.Selection) {
		if w := wordCount(s); w > bestWords {
			best, bestWords = s, w
		}
	})
	return best
}

func wordCount(sel *goquery.Selection) int {
	if sel == nil {
		return 0
	}
	n := 0
	for _, node := range sel.Nodes {
		n += countWords(node)
	}
	return n
}

func countWords(node *html.Node) int {
	if node.Type == html.TextNode {
		return wordsIn(node.Data)
	}
	if node.Type == html.ElementNode {
		switch node.Data {
		case "script", "style", "template", "noscript", "svg":
			return 0
		}
	}
	n := 0
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		n += countWords(c)
	}
	return n
}

// wordsIn counts the whitespace-separated tokens of s that carry a letter
// or a digit; a lone slash or bullet between links is punctuation, not a
// word. It allocates nothing: it runs on every text node of every page.
func wordsIn(s string) int {
	n, counted := 0, false
	for _, r := range s {
		if unicode.IsSpace(r) {
			counted = false
			continue
		}
		if !counted && (unicode.IsLetter(r) || unicode.IsNumber(r)) {
			counted = true
			n++
		}
	}
	return n
}

// linkWords counts the words inside links under node.
func linkWords(node *html.Node) int {
	if node.Type == html.ElementNode && node.Data == "a" {
		return countWords(node)
	}
	n := 0
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		n += linkWords(c)
	}
	return n
}

// wordCache holds the word and link-word counts of every element under a
// root, taken in one pass, so the rules can ask about any block in constant
// time. A block emptied by an earlier removal in the same rule looks a
// little larger than it is, which only ever keeps it.
type wordCache struct {
	m map[*html.Node]wordTally
}

type wordTally struct{ words, links int32 }

func newWordCache(root *html.Node) *wordCache {
	wc := &wordCache{m: make(map[*html.Node]wordTally, 1024)}
	wc.build(root, false)
	return wc
}

func (wc *wordCache) build(n *html.Node, inLink bool) (words, links int32) {
	if n.Type == html.TextNode {
		w := int32(wordsIn(n.Data))
		if inLink {
			return w, w
		}
		return w, 0
	}
	if n.Type == html.ElementNode {
		switch n.Data {
		case "script", "style", "template", "noscript", "svg":
			wc.m[n] = wordTally{}
			return 0, 0
		case "a":
			inLink = true
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w, l := wc.build(c, inLink)
		words += w
		links += l
	}
	if n.Type == html.ElementNode {
		wc.m[n] = wordTally{words, links}
	}
	return words, links
}

func (wc *wordCache) count(s *goquery.Selection) int {
	n := int32(0)
	for _, node := range s.Nodes {
		n += wc.m[node].words
	}
	return int(n)
}

func (wc *wordCache) linkCount(s *goquery.Selection) int {
	n := int32(0)
	for _, node := range s.Nodes {
		n += wc.m[node].links
	}
	return int(n)
}

// density is the share of a block's words that sit inside links.
func (wc *wordCache) density(s *goquery.Selection) float64 {
	total := wc.count(s)
	if total == 0 {
		return 0
	}
	return float64(wc.linkCount(s)) / float64(total)
}

// linkDensity is the share of an element's words that sit inside links.
func linkDensity(sel *goquery.Selection) float64 {
	total := wordCount(sel)
	if total == 0 {
		return 0
	}
	return float64(wordCount(sel.Find("a"))) / float64(total)
}

var (
	rxDisplayNoneStyle = regexp.MustCompile(`(?i)display\s*:\s*none`)
	rxHiddenStyle      = regexp.MustCompile(`(?i)visibility\s*:\s*hidden`)
	// Permalink anchors sit inside headings on most generated documentation:
	// pkg.go.dev's ¶, PostgreSQL's #, Sphinx's headerlink. Their text is a
	// glyph or an instruction, never part of the heading.
	rxPermalinkText  = regexp.MustCompile(`^[\s¶#§🔗⚓⚓︎🔗︎]*$|(?i)^(permalink|anchor|link to this (heading|section)|direct link.*)$`)
	rxPermalinkLabel = regexp.MustCompile(`(?i)^(permalink|anchor|link to|go to|direct link|copy link)`)
	// A heading that announces recirculation: the block of links or cards
	// under it is the site's, not the author's.
	rxRecircHeading = regexp.MustCompile(`(?i)^\W*(related|recommended|you (may|might) (also )?(like|enjoy|be interested)|(you'?ll|you will) also (like|love)|more (from|stories|like this|to explore|for you)|trending|(most )?popular|latest (news|posts|articles|stories)|explore more|read next|up next|keep (exploring|reading)|discover more|also of interest|similar (articles|posts|stories|recipes|products)|in case you missed|what to read next)`)
	// Furniture announces itself in its first words.
	rxChromePhrase = regexp.MustCompile(`(?i)^\W*(was this (page|article|content|section|information|guide|answer) (helpful|useful)|did you find (this|what you)|rate this (page|article)|help us improve|how (can|could) we (improve|make this)|thanks? (you )?for your feedback|send (us )?feedback|give feedback|sign up (for|to) (our |the )?(email|newsletter|updates)|subscribe (to|for) (our |the |receive |get )|(get|receive) (email )?notifications|get (the )?latest (news|updates)|stay (up to date|informed|connected)|share this (page|article|post)|cite this (page|work|article)|(next|previous|prev|up)\\s*:\\s*\\S)`)
	// Grid columns: Bootstrap's col-md-3 and span3, Foundation's large-3.
	rxGridCol     = regexp.MustCompile(`(?i)^(?:col-(?:([a-z]+)-)?(\d{1,2})|span(\d{1,2})|(small|medium|large|xlarge|xxlarge)-(\d{1,2}))$`)
	rxCommentName = regexp.MustCompile(`(?i)comment`)
)

// PruneLog, when set, is told about every removal pruneChrome makes.
// Measurement scaffolding for the bench harness.
var PruneLog func(rule string, s *goquery.Selection)

func drop(rule string, s *goquery.Selection) {
	s.Each(func(_ int, el *goquery.Selection) {
		if PruneLog != nil {
			PruneLog(rule, el)
		}
		el.Remove()
	})
}

// nameIsChrome reports whether a class or id list names site furniture.
// The vocabulary is what sites and the frameworks behind them — Bootstrap,
// MediaWiki, Sphinx, DocBook, Texinfo, Google's devsite — call the blocks a
// human reader skips; a name one publisher alone uses earns no place in
// it. Names that are unambiguous match anywhere (PromoBanner, ParkFooter);
// the rest must be whole tokens of the name — or two or three neighbouring
// tokens run together, so related-posts, related_posts and relatedPosts
// all count — and a paragraph whose generated id happens to end in "Ads"
// is left alone. A match alone never removes anything: the block must also
// be small or made of links. Only ModeClean consults it. Token lookups
// cost a microsecond per element where the equivalent alternation cost
// tens, which matters on a page with a hundred thousand class lists.
func nameIsChrome(name string) bool {
	return nameKind(name) != ""
}

// What a name can declare a block to be.
const (
	kindChrome  = "chrome"  // site furniture: a share bar, a cookie notice
	kindListing = "listing" // cards and rails: chrome beside a story, the page on a front
)

// nameKind classifies a class or id list: "" when it says nothing about
// the block, kindChrome or kindListing otherwise. Furniture wins when a
// name says both.
func nameKind(name string) string {
	name = strings.ToLower(name)
	toks := nameTokens(name)
	joined := strings.Join(toks, "")
	if containsAny(name, chromeAnywhere) || containsAny(joined, chromePhrases) || hasChromeToken(toks, chromeTokens) {
		return kindChrome
	}
	if containsAny(name, listingAnywhere) || containsAny(joined, listingPhrases) || hasChromeToken(toks, listingTokens) {
		return kindListing
	}
	return ""
}

func containsAny(s string, subs []string) bool {
	for _, w := range subs {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// hasChromeToken reports whether a token, or two or three neighbouring
// tokens run together, is in the set.
func hasChromeToken(toks []string, set map[string]bool) bool {
	for i, t := range toks {
		if set[t] {
			return true
		}
		if i+1 < len(toks) && set[t+toks[i+1]] {
			return true
		}
		if i+2 < len(toks) && set[t+toks[i+1]+toks[i+2]] {
			return true
		}
	}
	return false
}

func nameTokens(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == ':' || r == '.' })
}

var chromeAnywhere = []string{"footer", "navheader", "banner", "promo", "tooltip", "breadcrumb", "newsletter", "sidebar", "editsection", "headerlink", "navbox", "copiable", "nocontent"}

// Phrases that match anywhere in the name, with or without separators.
var chromePhrases = []string{"skiplink", "navpanel", "socialshare", "citethis", "citationinfo", "howtocite", "mdprint", "printmodal", "printbutton", "printlink", "printdialog"}

// Names for the cards and rails a site sets beside a story: related
// posts, most read, trending, a card grid. Beside a story they are the
// site's; on a section front or a topic index they are the page, and
// dropNamed keeps them when they hold it.
var listingAnywhere = []string{"recirc"}

var listingPhrases = []string{"relatedcontent", "latestposts", "contentlist"}

var listingTokens = tokenSet(`postrelated relatedpages relatedposts relatedarticles relatedstories relatedtopics relatedentries relatedlinks relatedproducts relateditems relatedrecipes recommended recommendation recommendations alsolike mostpopular mostread mostsaved mostviewed trending articlecard postcard teaser cards cardgrid cardlist`)

var tocTokens = tokenSet(`toc tableofcontents`)

// Custom elements name their role in the tag: <devsite-content-footer>.
var chromeTagTokens = tokenSet(`footer nav toc banner sidebar breadcrumb breadcrumbs feedback`)

func tagIsChrome(tag string) bool {
	toks := strings.Split(strings.ToLower(tag), "-")
	for _, t := range toks[1:] {
		if chromeTagTokens[t] {
			return true
		}
	}
	return false
}

var chromeTokens = tokenSet(`share sharing social pagination pager cookiebanner cookieconsent cookienotice cookiebar cookiepolicy cookiepopup cookiemodal cookiesettings cookiepreferences cookielaw gdpr feedback editthispage editongithub editpage editlink subscribe signup modal popup popover leftrail rightrail siderail lastmod pagemeta postbottom donate donation membership advert advertisement sponsor sponsored ad ads adslot adcontainer copyright authorinfo authorbio authorbox authorlist authorcontainer authorcard disqus rating ratings review reviews reviewbar quiz quizzes carousel ambox`)

func tokenSet(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

// chromeNamed reports whether an element's class or id names site furniture.
// Callers consult it only in ModeClean.
func chromeNamed(s *goquery.Selection) bool {
	return chromeNamedOn(s, "")
}

func chromeNamedOn(s *goquery.Selection, pageURL string) bool {
	return namedKind(s, pageURL) != ""
}

// namedKind is nameKind for an element: its class and id, its tag, and a
// table of contents by name that links only within the page.
func namedKind(s *goquery.Selection, pageURL string) string {
	class, _ := s.Attr("class")
	id, _ := s.Attr("id")
	name := class + " " + id
	if k := nameKind(name); k != "" {
		return k
	}
	if tagIsChrome(goquery.NodeName(s)) || (hasChromeToken(nameTokens(strings.ToLower(name)), tocTokens) && inPageLinksOnly(s, pageURL)) {
		return kindChrome
	}
	return ""
}

// inPageLinksOnly reports whether every link in a block points into the
// page.
func inPageLinksOnly(s *goquery.Selection, pageURL string) bool {
	only := true
	s.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href")
		only = inPageLink(href, pageURL)
		return only
	})
	return only
}

// inPageLink reports whether href points at a fragment of the page itself:
// "#intro", or "chapter.html#intro" on chapter.html — DocBook and PostgreSQL
// write their section tables of contents with the file name.
func inPageLink(href, pageURL string) bool {
	href = strings.TrimSpace(href)
	if strings.HasPrefix(href, "#") {
		return true
	}
	i := strings.IndexByte(href, '#')
	if i < 0 || pageURL == "" {
		return false
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return false
	}
	ref, err := url.Parse(href[:i])
	if err != nil {
		return false
	}
	abs := base.ResolveReference(ref)
	abs.Fragment, abs.RawQuery = "", ""
	page := *base
	page.Fragment, page.RawQuery = "", ""
	return abs.String() == page.String()
}

// pruneChrome removes site furniture from the chosen root by what it is,
// not by how it scores. Collapsed content stays: an accordion panel hidden
// behind a disclosure button, a code sample on an unselected tab, a
// hidden=until-found section — the author expects readers to open those,
// and an agent reading the page cannot click. The rules run in a fixed
// order: what renders as nothing, then controls, then hidden elements, then
// — on a fresh word count — the block rules, of which only ModeClean runs
// the ones that read a block's name or its first words.
func pruneChrome(root *goquery.Selection, doc *goquery.Document, pageURL string, mode Mode) {
	p := &pruner{root: root, doc: doc, pageURL: pageURL, mode: mode, title: pageTitleOf(doc), controlled: controlledIDs(doc)}
	p.wc = newWordCache(root.Nodes[0])
	p.rootWords = p.wc.count(root)

	p.dropEmbeds()
	p.dropForms()
	p.unwrapDisclosureButtons()
	p.dropSpacers()
	p.dropHidden()

	// The structural removals above are done; count what is left once,
	// for every block rule below.
	p.wc = newWordCache(root.Nodes[0])
	p.dropAsides()
	p.hub = p.hubPage()
	if mode == ModeClean {
		p.dropGridSidebars()
		p.dropComments()
		p.dropNamed()
		p.dropPhrases()
	}
	p.dropLinkBlocks()

	// Last, whatever the rules above removed: tables that only lay the page
	// out become blocks, and a heading left with nothing under it — its
	// form, its widget, its links were furniture — is dropped.
	unwrapLayoutTables(root)
	dropOrphanHeadings(root, p.title)
}

// pruner carries what every rule needs: the root and page, the ids that
// disclosure controls name, the word counts taken once, and the size of the
// page before anything was removed.
type pruner struct {
	root       *goquery.Selection
	doc        *goquery.Document
	pageURL    string
	mode       Mode
	title      pageTitle
	controlled map[string]bool
	wc         *wordCache
	rootWords  int
	hub        bool                // the page is made of links: link blocks are its content
	data       map[*html.Node]bool // links that are data, not menu: in captions, definitions, data-table cells
	threads    map[*html.Node]bool // comment threads found by dropComments
}

// bulk reports whether a block is the root or holds half its words. No rule
// may take the bulk of the page: a link directory is made of links, a
// wrapper named "content-footer" may hold the content.
func (p *pruner) bulk(s *goquery.Selection) bool {
	return s.Nodes[0] == p.root.Nodes[0] || (p.rootWords > 0 && float64(p.wc.count(s))/float64(p.rootWords) >= 0.5)
}

// chromeNamed is chromeNamed for the mode: names mean nothing in
// ModeComplete.
func (p *pruner) chromeNamed(s *goquery.Selection) bool {
	return p.mode == ModeClean && chromeNamed(s)
}

const nonContentTags = "script, style, template, noscript, link, meta, iframe, object, embed, canvas, svg, video, audio, map, input, select, textarea, progress, meter"

const landmarkChrome = "nav, footer, dialog, [role=navigation], [role=banner], [role=contentinfo], [role=search], [role=dialog], [role=alertdialog], [role=alert], [role=status], [role=menu], [role=menubar], [aria-modal=true]"

// dropEmbeds removes what renders as nothing in markdown, and what would
// duplicate what mathToText already wrote.
func (p *pruner) dropEmbeds() {
	// Formulas were rewritten as text by mathToText; their fallback images
	// and screen-reader copies would now duplicate them.
	drop("math-fallback", p.root.Find("img[alt^='{\\displaystyle'], .mwe-math-fallback-image-inline, .mwe-math-fallback-image-display"))
	// An embedded video is a reference the reader can follow; the frame
	// itself renders as nothing.
	p.root.Find("iframe[src]").Each(func(_ int, f *goquery.Selection) {
		if href, ok := videoLink(f); ok {
			f.ReplaceWithHtml(`<p><a href="` + html.EscapeString(href) + `">` + html.EscapeString(href) + `</a></p>`)
		}
	})
	drop("non-content", p.root.Find(nonContentTags))
	drop("landmark", p.root.Find(landmarkChrome))
}

// dropForms removes tab strips and the forms that are widgets.
func (p *pruner) dropForms() {
	// A tab strip is chrome; Bootstrap's accordion wraps its panels in the
	// same role, and those panels are the page.
	p.root.Find("[role=tablist]").Each(func(_ int, t *goquery.Selection) {
		if t.Find("[role=tabpanel], [aria-controls]").Length() == 0 {
			drop("tablist", t)
		}
	})
	p.root.Find("form").Each(func(_ int, f *goquery.Selection) {
		// Search boxes, newsletter signups and feedback widgets are chrome;
		// a form in a tutorial about forms is the lesson. Keep forms that
		// carry prose or labelled fields, drop the rest.
		if f.Find("input[type=search], input[type=email], input[type=password]").Length() > 0 || p.wc.count(f) < 15 {
			drop("form", f)
		}
	})
}

// unwrapDisclosureButtons keeps the heading text a disclosure button
// carries and drops every other button, which is a control.
func (p *pruner) unwrapDisclosureButtons() {
	p.root.Find("button").Each(func(_ int, b *goquery.Selection) {
		_, controls := b.Attr("aria-controls")
		_, expands := b.Attr("aria-expanded")
		if !controls && !expands {
			drop("button", b)
			return
		}
		// The block around a disclosure button is a foldout the author
		// collapsed; later rules leave it alone. A foldout is a block
		// within the page, never the page. The button's own chevron is
		// not part of the heading it carries.
		if parent := b.Parent(); parent.Length() > 0 && parent.Nodes[0] != p.root.Nodes[0] {
			parent.SetAttr("data-foldout", "")
		}
		b.Find("*").Each(func(_ int, c *goquery.Selection) {
			if wordsIn(c.Text()) == 0 {
				c.Remove()
			}
		})
		unwrap(b)
	})
}

// dropSpacers removes spacer pixels and script-driven links: controls, not
// content.
func (p *pruner) dropSpacers() {
	p.root.Find("img[width], img[height]").Each(func(_ int, img *goquery.Selection) {
		w, _ := img.Attr("width")
		h, _ := img.Attr("height")
		if w == "0" || w == "1" || h == "0" || h == "1" {
			drop("spacer", img)
		}
	})
	p.root.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(href)), "javascript:") {
			return
		}
		if p.wc.count(a) <= 2 {
			drop("script-link", a)
		} else {
			unwrap(a)
		}
	})
}

// dropHidden removes what the page hides from a sighted reader, unless the
// page's own signals say it is content someone collapsed.
func (p *pruner) dropHidden() {
	// aria-hidden hides from the reader we serve, except the panel a
	// disclosure or tab control names: that is content someone collapsed.
	p.root.Find("[aria-hidden=true]").Each(func(_ int, s *goquery.Selection) {
		if isCollapsedContent(s, "", false, p.controlled) {
			return
		}
		drop("aria-hidden", s)
	})
	p.root.Find("[hidden], [style], [class]").Each(func(_ int, s *goquery.Selection) {
		p.hiddenRule(s)
	})
}

// hiddenRule decides one element that carries a hidden attribute, a style
// or a class list.
func (p *pruner) hiddenRule(s *goquery.Selection) {
	style, _ := s.Attr("style")
	class, _ := s.Attr("class")
	hiddenAttr, hasHidden := s.Attr("hidden")
	srOnly, hiddenCls := hiddenClasses(class)
	if srOnly {
		// Screen-reader text is written for a reader who cannot see the
		// page — exactly the reader we serve. A label or a "skip to
		// content" duplicate is noise; a paragraph describing a hero
		// graphic is the only text the graphic has.
		if p.wc.count(s) < 15 || p.wc.density(s) > 0.5 {
			drop("sr-only", s)
		}
		return
	}
	invisible := hasHidden || hiddenByStyle(style)
	if !invisible && !hiddenCls {
		return
	}
	if isCollapsedContent(s, hiddenAttr, hasHidden, p.controlled) {
		return
	}
	if !invisible {
		// A class-hidden block is dropped only when it also looks like
		// chrome; a foldout of prose or code is content someone collapsed.
		if p.chromeNamed(s) || p.wc.count(s) < 8 || p.wc.density(s) > 0.5 {
			drop("hidden-class", s)
		}
		return
	}
	drop("hidden", s)
}

func hiddenByStyle(style string) bool {
	return style != "" && (rxDisplayNoneStyle.MatchString(style) || rxHiddenStyle.MatchString(style))
}

// dropAsides removes the asides that are sidebars: made of links, or of
// nothing much. Example boxes, notes and warnings are asides with substance.
func (p *pruner) dropAsides() {
	p.root.Find("aside, [role=complementary]").Each(func(_ int, a *goquery.Selection) {
		if p.bulk(a) || a.Find("pre, code, table, figure, img, dl").Length() > 0 {
			return
		}
		if p.wc.count(a) < 8 || p.chromeNamed(a) {
			drop("aside", a)
			return
		}
		if p.wc.density(a) >= 0.5 && !p.wc.hasProse(a, 15) {
			drop("aside-links", a)
		}
	})
}

// dropGridSidebars removes a narrow grid column beside a wide one, when the
// column is made of links or holds nothing much. A narrow column with a
// paragraph is the page's own callout — a fact box, a key-points panel.
func (p *pruner) dropGridSidebars() {
	p.root.Find("[class]").Each(func(_ int, c *goquery.Selection) {
		class, _ := c.Attr("class")
		if w := gridWidth(class); w == 0 || w > 4 || p.bulk(c) {
			return
		}
		if besideWideColumn(c) && (p.wc.count(c) < 20 || p.wc.density(c) >= 0.5) {
			drop("grid-sidebar", c)
		}
	})
}

func besideWideColumn(c *goquery.Selection) bool {
	wide := false
	c.Siblings().EachWithBreak(func(_ int, sib *goquery.Selection) bool {
		sc, _ := sib.Attr("class")
		wide = gridWidth(sc) >= 6
		return !wide
	})
	return wide
}

// dropComments leaves out a thread under an article; a page that is a
// thread — a forum topic, a Hacker News discussion — is kept whole.
func (p *pruner) dropComments() {
	var comments []candidate
	p.threads = map[*html.Node]bool{}
	p.root.Find("div, section, ol, ul, li, article, aside, table").Each(func(_ int, c *goquery.Selection) {
		class, _ := c.Attr("class")
		id, _ := c.Attr("id")
		if !rxCommentName.MatchString(class + " " + id) {
			return
		}
		p.threads[c.Nodes[0]] = true
		if !p.bulk(c) {
			comments = append(comments, candidate{"comments", c})
		}
	})
	// A comment block that is the bulk of the page makes the page a thread.
	if len(p.threads) == len(comments) && p.wc.hubShare(comments, p.rootWords) < 0.4 {
		for _, c := range comments {
			drop(c.rule, c.sel)
		}
		p.threads = nil
	}
}

// dropNamed removes named chrome — share bars, breadcrumbs, cookie
// notices, author boxes — only when it also looks like chrome, so an
// article about social media survives. Inline elements and headings are
// never chrome by name: a Sphinx heading is an <a class="toc-backref">, and
// a section on cookies is headed "Cookie overview".
func (p *pruner) dropNamed() {
	var listing []candidate
	p.root.Find("[class], [id]").Each(func(_ int, s *goquery.Selection) {
		if neverNamed[s.Nodes[0].Data] || p.bulk(s) {
			return
		}
		kind := namedKind(s, p.pageURL)
		// Whatever wraps the page's headline — an intro banner, a hero —
		// is the head of the content, whatever it is called.
		if kind == "" || s.Find("h1").Length() > 0 {
			return
		}
		ld := p.wc.density(s)
		// A foldout of prose under a widget's "footer" — the command
		// reference under a Redis example — is content someone collapsed.
		if ld <= 0.5 && (s.Is("[data-foldout]") || s.Find("[data-foldout]").Length() > 0) {
			return
		}
		if p.wc.count(s) >= 150 && ld <= 0.5 {
			return
		}
		if kind == kindListing {
			listing = append(listing, candidate{"named", s})
			return
		}
		drop("named", s)
	})
	// Cards that hold the page are the page — a section front, a topic
	// index — whatever they are called. Beside a story they are the site's.
	if p.wc.hubShare(listing, p.rootWords) >= 0.4 {
		for _, c := range listing {
			if PruneLog != nil {
				PruneLog("listing-keep", c.sel)
			}
		}
		return
	}
	for _, c := range listing {
		drop(c.rule, c.sel)
	}
}

// dropPhrases removes furniture that announces itself: "Was this page
// helpful?", "Sign up for our newsletter". Only short blocks, and never
// code or tables.
func (p *pruner) dropPhrases() {
	p.root.Find("div, section, p, aside").Each(func(_ int, b *goquery.Selection) {
		if p.bulk(b) || p.wc.count(b) >= 60 || b.Find("pre, table, h1").Length() > 0 {
			return
		}
		if b.Find("h2, h3").Length() > 0 && p.wc.count(b) >= 30 {
			return
		}
		if rxChromePhrase.MatchString(strings.TrimSpace(b.Text())) {
			drop("phrase", b)
		}
	})
}

// hubPage decides once, before any rule that reads names or headings,
// whether the page is made of links — a topic index, a chapter list, a
// section front. The candidates are counted the way ModeClean counts
// them, a recirculation heading introducing nothing, so both modes reach
// the same answer: a "Most read" rail is link material on a front page
// whichever mode reads it, and the mode meant to remove more must not be
// the one that keeps the top stories.
func (p *pruner) hubPage() bool {
	if p.rootWords < 200 {
		return false
	}
	mode := p.mode
	p.mode = ModeClean
	candidates := append(p.teaserGrids(), p.linkBlocks()...)
	p.mode = mode
	return p.wc.hubShare(candidates, p.rootWords) >= 0.4
}

// dropLinkBlocks takes the blocks made of links. A hub page — a topic index,
// a chapter list, a condition's overview — is made of links, and a short
// page has nothing to protect: both keep everything.
func (p *pruner) dropLinkBlocks() {
	if p.rootWords < 200 {
		return
	}
	p.dropTOCs()
	candidates := append(p.teaserGrids(), p.linkBlocks()...)
	if p.hub {
		for _, c := range candidates {
			if PruneLog != nil {
				PruneLog("hub-keep:"+c.rule, c.sel)
			}
		}
		return
	}
	for _, c := range candidates {
		drop(c.rule, c.sel)
	}
	p.dropPermalinks()
}

func nestedList(l *goquery.Selection) bool { return l.ParentsFiltered("ul, ol, dl").Length() > 0 }

// dropTOCs removes in-page tables of contents: a list of fragment links
// whose texts are the document's own headings. An index of function
// signatures also links within the page, but it says more than the
// headings do.
func (p *pruner) dropTOCs() {
	headings := headingTexts(p.root)
	p.root.Find("ul, ol, dl").Each(func(_ int, l *goquery.Selection) {
		if p.bulk(l) || nestedList(l) {
			return
		}
		links := l.Find("a[href]")
		if links.Length() < 3 || p.wc.hasProse(l, 10) {
			return
		}
		matched, fragment := 0, 0
		links.Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			if inPageLink(href, p.pageURL) {
				fragment++
			}
			if headings[normalizeText(a.Text())] {
				matched++
			}
		})
		if fragment == links.Length() && matched*10 >= links.Length()*6 {
			drop("toc", l)
		}
	})
}

// teaserGrids finds three or more sibling blocks, each headed by a link to
// another page and carrying little prose — related stories, "you'll also
// love", latest news. Article sections have headings that go nowhere.
func (p *pruner) teaserGrids() []candidate {
	var out []candidate
	p.root.Find("div, section, ul, ol").Each(func(_ int, c *goquery.Selection) {
		if p.bulk(c) {
			return
		}
		kids := c.Children()
		if kids.Length() < 3 {
			return
		}
		teasers := 0
		kids.Each(func(_ int, k *goquery.Selection) {
			if p.wc.isTeaser(k) {
				teasers++
			}
		})
		if teasers >= 3 && teasers*2 >= kids.Length() && !introduced(c, p.mode) {
			out = append(out, candidate{"teasers", c})
		}
	})
	return out
}

// linkBlocks finds blocks made of links: related-content rails, link-back
// lists, tag clouds. A list the author introduced with a heading or a
// sentence — "See also", "External links", an API index, a table of child
// pages — is kept, as is a section that opens with its own heading.
func (p *pruner) linkBlocks() []candidate {
	var out []candidate
	p.root.Find("div, section, ul, ol").Each(func(_ int, b *goquery.Selection) {
		if p.bulk(b) || p.menuLinks(b) < 3 || p.wc.density(b) < 0.8 {
			return
		}
		if b.Find("pre, table").Length() > 0 || introduced(b, p.mode) || (b.Is("ul, ol") && nestedList(b)) {
			return
		}
		if b.Is("div, section") && startsWithHeading(b, p.mode) {
			return
		}
		// On a page that is a thread, the links around each comment are
		// its byline; in a foldout, they are what the author collapsed.
		if insideAny(b, p.threads) || b.Closest("[data-foldout]").Length() > 0 || inControlledPanel(b, p.controlled) {
			return
		}
		out = append(out, candidate{"link-block", b})
	})
	return out
}

// menuLinks counts the links of a block that a menu could be made of.
// In a figure caption, a definition or a cell of a data table a list of
// links is the data — a figure's credits, a glossary entry, an infobox
// row — and is not counted, whether the block is the cell's content or
// the figure's wrapper.
func (p *pruner) menuLinks(b *goquery.Selection) int {
	if p.data == nil {
		p.data = map[*html.Node]bool{}
		mark := func(links *goquery.Selection) {
			links.Each(func(_ int, a *goquery.Selection) { p.data[a.Nodes[0]] = true })
		}
		mark(p.root.Find("figcaption a[href], dt a[href], dd a[href]"))
		p.root.Find("table").Each(func(_ int, t *goquery.Selection) {
			if isDataTable(t) {
				mark(t.Find("a[href]"))
			}
		})
	}
	n := 0
	b.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if !p.data[a.Nodes[0]] {
			n++
		}
	})
	return n
}

// dropPermalinks removes the anchor glyphs generated documentation puts in
// its headings, and unwraps a heading's link to itself.
func (p *pruner) dropPermalinks() {
	p.root.Find("h1, h2, h3, h4, h5, h6").Each(func(_ int, h *goquery.Selection) {
		h.Find("a").Each(func(_ int, a *goquery.Selection) {
			if isPermalink(a) {
				drop("permalink", a)
				return
			}
			// A heading that links to itself, its section or its panel is
			// a heading; a heading that links elsewhere is a reference.
			if href, _ := a.Attr("href"); href == "" || strings.HasPrefix(href, "#") {
				unwrap(a)
			}
		})
	})
}

func isPermalink(a *goquery.Selection) bool {
	label, _ := a.Attr("aria-label")
	title, _ := a.Attr("title")
	text := strings.TrimSpace(a.Text())
	return rxPermalinkText.MatchString(text) || (rxPermalinkLabel.MatchString(label) || rxPermalinkLabel.MatchString(title)) && len(text) <= 2
}

var rxVideoEmbed = regexp.MustCompile(`^(?:https?:)?//(?:www\.)?(?:youtube(?:-nocookie)?\.com/embed/([A-Za-z0-9_-]{6,})|player\.vimeo\.com/video/(\d+))`)

// videoLink turns a video host's embed frame into the page it embeds.
func videoLink(f *goquery.Selection) (string, bool) {
	src, _ := f.Attr("src")
	m := rxVideoEmbed.FindStringSubmatch(src)
	switch {
	case m == nil:
		return "", false
	case m[1] != "":
		return "https://www.youtube.com/watch?v=" + m[1], true
	default:
		return "https://vimeo.com/" + m[2], true
	}
}

// unwrapLayoutTables turns tables that only arrange the page — a comment
// thread's rows, a title bar, an old site's whole grid — into plain blocks,
// so they do not come out as pipe tables. Data tables keep their shape.
func unwrapLayoutTables(root *goquery.Selection) {
	root.Find("table").Each(func(_ int, t *goquery.Selection) {
		if isDataTable(t) {
			return
		}
		if PruneLog != nil {
			PruneLog("layout-table", t)
		}
		t.Find("thead, tbody, tfoot, tr, td, th, caption").AddSelection(t).Each(func(_ int, el *goquery.Selection) {
			for _, n := range el.Nodes {
				n.Data, n.DataAtom = "div", atom.Div
			}
		})
	})
}

var blockInCell = "div, p, ul, ol, dl, table, pre, h1, h2, h3, h4, h5, h6, blockquote, form, section, article, figure"

// isDataTable tells a table of data from a table used as a grid, by the
// page's own signals first — header cells, a caption, a summary — then by
// shape: nested tables, one row or column, cells holding blocks, or the
// border and padding attributes of the layout era mean a grid.
func isDataTable(t *goquery.Selection) bool {
	if isLayoutTable(t) {
		return false
	}
	if dt, _ := t.Attr("datatable"); dt == "1" {
		return true
	}
	if _, ok := t.Attr("summary"); ok || t.Find("caption, thead, tfoot, th, colgroup, col").Length() > 0 {
		return true
	}
	if t.Find("table").Length() > 0 || t.Find(blockInCell).Length() > 0 {
		return false
	}
	if border, _ := t.Attr("border"); border == "0" {
		return false
	}
	if _, ok := t.Attr("cellpadding"); ok {
		return false
	}
	if _, ok := t.Attr("cellspacing"); ok {
		return false
	}
	return t.Find("tr").Length() >= 2 && maxCols(t) >= 2
}

// insideAny reports whether s sits under one of the marked nodes.
func insideAny(s *goquery.Selection, marked map[*html.Node]bool) bool {
	for p := s.Nodes[0].Parent; p != nil; p = p.Parent {
		if marked[p] {
			return true
		}
	}
	return false
}

// gridWidth is the number of columns (of twelve) an element spans at the
// widest breakpoint it declares, or 0 when it declares none. Responsive
// grids stack on phones and split on desktops; the desktop layout is the
// one that says what stands beside what.
func gridWidth(class string) int {
	best, bestRank := 0, -1
	for _, tok := range strings.Fields(class) {
		if len(tok) < 5 || strings.IndexByte("cCsSmMlLxX", tok[0]) < 0 {
			continue
		}
		m := rxGridCol.FindStringSubmatch(tok)
		if m == nil {
			continue
		}
		bp, num := "", ""
		switch {
		case m[2] != "":
			bp, num = m[1], m[2]
		case m[3] != "":
			num = m[3]
		default:
			bp, num = m[4], m[5]
		}
		n, _ := strconv.Atoi(num)
		rank, ok := breakpointRank[strings.ToLower(bp)]
		if !ok {
			rank = 2
		}
		if n > 0 && n <= 12 && rank > bestRank {
			best, bestRank = n, rank
		}
	}
	return best
}

var breakpointRank = map[string]int{
	"": 0, "xs": 0, "mob": 0, "mobile": 0, "phone": 0, "small": 0,
	"sm": 1, "tab": 1, "tablet": 1, "medium": 1,
	"md": 2,
	"lg": 3, "desk": 3, "desktop": 3, "large": 3,
	"xl": 4, "xlarge": 4,
	"xxl": 5, "xxlarge": 5,
}

// dropOrphanHeadings removes the headings at the tail of the root that
// have nothing but other headings after them — "Read more", "Related
// articles", "Subscribe" — once what stood under them is gone. A heading
// with any content anywhere after it stays, whatever the levels: authors
// mis-level headings too often for a level to close a section. The page's
// title is never an orphan.
func dropOrphanHeadings(root *goquery.Selection, title pageTitle) {
	var flat []flatNode
	flatten(root.Nodes[0], &flat)
	var orphans []*html.Node
	for i, f := range flat {
		if headingLevel(f.n) == 0 && !trailingLabel(f.n) {
			continue
		}
		if h := goquery.NewDocumentFromNode(f.n).Selection; holdsTitle(h, title) {
			continue
		}
		content := false
		for j := f.end; j < len(flat) && !content; j++ {
			n := flat[j].n
			switch n.Type {
			case html.TextNode:
				content = wordsIn(n.Data) > 0
			case html.ElementNode:
				switch n.Data {
				case "img", "pre", "table", "video", "iframe", "picture", "figure":
					content = true
				}
			}
		}
		if !content {
			orphans = append(orphans, flat[i].n)
		}
	}
	for _, n := range orphans {
		drop("orphan-heading", goquery.NewDocumentFromNode(n).Selection)
	}
}

// trailingLabel recognises "Related:" — a bold or paragraph label of a few
// words ending in a colon, which introduces what follows it.
func trailingLabel(n *html.Node) bool {
	if n.Type != html.ElementNode || (n.Data != "b" && n.Data != "strong" && n.Data != "p") {
		return false
	}
	t := strings.TrimSpace(goquery.NewDocumentFromNode(n).Text())
	return strings.HasSuffix(t, ":") && wordsIn(t) <= 3
}

type flatNode struct {
	n   *html.Node
	end int // index just past the node's subtree
}

func flatten(n *html.Node, out *[]flatNode) {
	i := len(*out)
	*out = append(*out, flatNode{n: n})
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		flatten(c, out)
	}
	(*out)[i].end = len(*out)
}

func headingLevel(n *html.Node) int {
	if n.Type != html.ElementNode || len(n.Data) != 2 || n.Data[0] != 'h' || n.Data[1] < '1' || n.Data[1] > '6' {
		return 0
	}
	return int(n.Data[1] - '0')
}

// Screen-reader-only text is an alternative rendering, not hidden content.
var srOnlyClasses = tokenSet(`sr-only visually-hidden visuallyhidden screen-reader-text screen-reader-only`)

// Utility classes that hide an element. Ambiguous: Tailwind's "hidden"
// stacks with a breakpoint that shows it again, so a class-hidden block is
// only removed when it also looks like chrome.
var hideClasses = tokenSet(`hidden d-none is-hidden js-hidden u-hidden hide invisible`)

// hiddenClasses reports whether a class list marks an element
// screen-reader-only, and whether it hides it.
func hiddenClasses(class string) (srOnly, hidden bool) {
	for _, tok := range strings.Fields(class) {
		tok = strings.ToLower(strings.TrimPrefix(tok, "!"))
		if srOnlyClasses[tok] {
			srOnly = true
		} else if hideClasses[tok] {
			hidden = true
		}
	}
	return srOnly, hidden
}

// inControlledPanel reports whether a block is, or sits inside, a panel a
// disclosure button controls: a collapsed command list is content someone
// folded away, not a link directory.
func inControlledPanel(b *goquery.Selection, controlled map[string]bool) bool {
	for e := b; e.Length() > 0 && !e.Is("body"); e = e.Parent() {
		if id, ok := e.Attr("id"); ok && controlled[id] {
			return true
		}
	}
	return false
}

// Tags the named rule never removes: inline text, headings, and the
// landmarks that hold the page.
var neverNamed = tokenSet(`a code em strong b i cite abbr time label main article h1 h2 h3 h4 h5 h6`)

type candidate struct {
	rule string
	sel  *goquery.Selection
}

// hubShare is the share of the root's words held by the candidate blocks,
// counting nested candidates once.
func (wc *wordCache) hubShare(candidates []candidate, rootWords int) float64 {
	if rootWords == 0 {
		return 0
	}
	marked := map[*html.Node]bool{}
	for _, c := range candidates {
		marked[c.sel.Nodes[0]] = true
	}
	words := 0
	for _, c := range candidates {
		outer := true
		for p := c.sel.Nodes[0].Parent; p != nil; p = p.Parent {
			if marked[p] {
				outer = false
				break
			}
		}
		if outer {
			words += wc.count(c.sel)
		}
	}
	return float64(words) / float64(rootWords)
}

// hasProse reports whether a block contains a paragraph or list item of at
// least n words outside links — a sentence someone wrote, not a menu.
func (wc *wordCache) hasProse(s *goquery.Selection, n int) bool {
	found := false
	s.Find("p, li, dd, blockquote").EachWithBreak(func(_ int, p *goquery.Selection) bool {
		if wc.count(p)-wc.linkCount(p) >= n {
			found = true
		}
		return !found
	})
	return found
}

// isTeaser recognises a card: a block whose heading is a link to another
// page, or which is itself a link, with at most a blurb of prose.
func (wc *wordCache) isTeaser(k *goquery.Selection) bool {
	if wc.count(k)-wc.linkCount(k) >= 60 {
		return false
	}
	h := k.Find("h1, h2, h3, h4, h5, h6").First()
	if h.Length() == 0 {
		return false
	}
	var link *goquery.Selection
	if k.Is("a[href]") {
		link = k
	} else if a := h.Find("a[href]").First(); a.Length() > 0 && normalizeText(a.Text()) == normalizeText(h.Text()) {
		link = a
	} else {
		for p := h.Nodes[0].Parent; p != nil && p != k.Nodes[0].Parent; p = p.Parent {
			if p.Type == html.ElementNode && p.Data == "a" {
				link = goquery.NewDocumentFromNode(p).Selection
				break
			}
		}
	}
	if link == nil {
		return false
	}
	href, _ := link.Attr("href")
	return href != "" && !strings.HasPrefix(href, "#")
}

// introduced reports whether a block follows a heading or a sentence, the
// way an authored list does and a rail of links does not. What comes
// right before the block decides — at whatever level of wrapper: MDN puts
// a section's list in a div after the heading, OWID puts its chart list
// three wrappers deep under an h1 and a button. In ModeClean a heading that
// announces recirculation does not count as an introduction.
func introduced(b *goquery.Selection, mode Mode) bool {
	for e, depth := b, 0; e.Length() > 0 && depth < 8 && !e.Is("body, main, article"); e, depth = e.Parent(), depth+1 {
		prev := e.Prev()
		// A button or a rule between the heading and its list is not the
		// introduction.
		for prev.Length() > 0 && (prev.Is("a, button, hr, br, img, picture, figure, svg, span, script, style") || (wordCount(prev) == 0 && prev.Find("h1, h2, h3, h4, h5, h6").Length() == 0)) {
			prev = prev.Prev()
		}
		if prev.Length() == 0 {
			continue // first in its wrapper: what precedes the wrapper decides
		}
		if prev.Is("h1, h2, h3, h4, h5, h6") {
			return !recircHeading(prev, mode)
		}
		// A sentence, or a label: "Techniques:".
		if prev.Is("p") && (wordCount(prev) >= 3 || strings.HasSuffix(strings.TrimSpace(prev.Text()), ":")) {
			return true
		}
		// Wikipedia wraps its headings: <div class="mw-heading"><h2>…</h2>
		// <span class="mw-editsection">[edit]</span></div>.
		if h := prev.Find("h1, h2, h3, h4, h5, h6"); h.Length() > 0 && wordCount(prev)-wordCount(h) <= 2 {
			return !recircHeading(h.First(), mode)
		}
		return false
	}
	return false
}

// recircHeading reports whether a heading announces recirculation —
// "Related articles", "You might also like" — so the block of links under
// it is the site's, not the author's. Only ModeClean reads headings.
func recircHeading(h *goquery.Selection, mode Mode) bool {
	return mode == ModeClean && rxRecircHeading.MatchString(strings.TrimSpace(h.Text()))
}

// startsWithHeading reports whether a block's first words are a heading —
// however many wrappers the heading sits in.
func startsWithHeading(b *goquery.Selection, mode Mode) bool {
	first := firstText(b.Nodes[0])
	if first == nil {
		return false
	}
	for p := first.Parent; p != nil && p != b.Nodes[0]; p = p.Parent {
		if headingLevel(p) > 0 {
			return !recircHeading(goquery.NewDocumentFromNode(p).Selection, mode)
		}
	}
	return false
}

// firstText finds the first text node under n that carries a word.
func firstText(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			if wordsIn(c.Data) > 0 {
				return c
			}
			continue
		}
		if t := firstText(c); t != nil {
			return t
		}
	}
	return nil
}

// headingTexts collects the normalized text of every heading under root.
func headingTexts(root *goquery.Selection) map[string]bool {
	out := map[string]bool{}
	root.Find("h1, h2, h3, h4, h5, h6").Each(func(_ int, h *goquery.Selection) {
		out[normalizeText(h.Text())] = true
	})
	return out
}

var rxNonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

func normalizeText(s string) string {
	return strings.TrimSpace(rxNonWord.ReplaceAllString(strings.ToLower(s), " "))
}

// isCollapsedContent tells authored-but-collapsed content from invisible
// chrome. The signals are the page's own: the HTML `until-found` value exists
// for exactly this, a disclosure button names its panel in aria-controls, a
// tab names its panel by role, and <details> collapses by definition.
func isCollapsedContent(s *goquery.Selection, hiddenAttr string, hasHidden bool, controlled map[string]bool) bool {
	if hasHidden && strings.EqualFold(hiddenAttr, "until-found") {
		return true
	}
	if id, ok := s.Attr("id"); ok && controlled[id] {
		return true
	}
	if role, _ := s.Attr("role"); role == "tabpanel" {
		return true
	}
	if s.Closest("details, [data-foldout]").Length() > 0 {
		return true
	}
	return false
}

// controlledIDs collects every element id named by a disclosure or tab
// control anywhere on the page.
func controlledIDs(doc *goquery.Document) map[string]bool {
	ids := map[string]bool{}
	doc.Find("[aria-controls]").Each(func(_ int, s *goquery.Selection) {
		v, _ := s.Attr("aria-controls")
		for _, id := range strings.Fields(v) {
			ids[id] = true
		}
	})
	return ids
}

// mathToText rewrites MathML as its TeX source. A formula is content — the
// sentence around it is meaningless without it — and TeX is the notation a
// reader of markdown expects. MathML carries its own source in an
// annotation, or as alttext; the rendered fallback image is dropped later.
func mathToText(doc *goquery.Document) []string {
	var formulas []string
	doc.Find("math").Each(func(_ int, m *goquery.Selection) {
		tex := strings.TrimSpace(m.Find("annotation[encoding='application/x-tex']").First().Text())
		if tex == "" {
			tex, _ = m.Attr("alttext")
			tex = strings.TrimSpace(tex)
		}
		if tex == "" {
			return
		}
		// MediaWiki wraps its TeX: {\displaystyle …}. Only that wrapper's
		// closing brace goes; a formula's own last brace is the formula's.
		if inner, ok := strings.CutPrefix(tex, "{\\displaystyle "); ok {
			tex = strings.TrimSuffix(inner, "}")
		}
		// The TeX is spliced in after conversion, so the converter does not
		// escape its backslashes and underscores as prose; the placeholder
		// is a word it leaves alone, in a block of its own for display math.
		token := fmt.Sprintf("%s%dx", mathPlaceholder, len(formulas))
		markup := token
		if display, _ := m.Attr("display"); display == "block" {
			formulas = append(formulas, "$$"+tex+"$$")
			markup = "<div>" + token + "</div>"
		} else {
			formulas = append(formulas, "$"+tex+"$")
		}
		// Replace the nearest wrapper that exists only to hold the formula,
		// so a display:none MathML container does not take the text with it.
		target := m
		if p := m.Parent(); p.Length() > 0 && p.Children().Length() == 1 && p.Is("span, div") {
			style, _ := p.Attr("style")
			if rxDisplayNoneStyle.MatchString(style) {
				target = p
			}
		}
		target.ReplaceWithHtml(markup)
	})
	return formulas
}

// mathPlaceholder is the stem of the word mathToText leaves where a
// formula goes; spliceMath swaps the TeX back in after conversion.
const mathPlaceholder = "ketchmath"

// spliceMath puts the formulas mathToText set aside back into the markdown.
func spliceMath(md string, formulas []string) string {
	for i, f := range formulas {
		md = strings.ReplaceAll(md, fmt.Sprintf("%s%dx", mathPlaceholder, i), f)
	}
	return md
}

// unwrap replaces an element with its children.
func unwrap(s *goquery.Selection) {
	for _, el := range s.Nodes {
		parent := el.Parent
		if parent == nil {
			continue
		}
		for el.FirstChild != nil {
			c := el.FirstChild
			el.RemoveChild(c)
			parent.InsertBefore(c, el)
		}
		parent.RemoveChild(el)
	}
}

var (
	// An emptied link, but not an image without alt text: ![](src) keeps
	// its source.
	rxEmptyLink   = regexp.MustCompile(`(^|[^!])\[\]\([^)]*\)`)
	rxEmptyBullet = regexp.MustCompile(`^\s*[-*]\s*$`)
)

// tidyMarkdown removes what icon links and emptied wrappers leave behind.
// Fenced code is left byte-for-byte: trailing spaces and blank-line runs
// inside a listing are the listing.
func tidyMarkdown(md string) string {
	md = quoteFences(md)
	// Adjacent empty links share the character between them that the
	// pattern consumes; repeat until none is left.
	for {
		next := rxEmptyLink.ReplaceAllString(md, "$1")
		if next == md {
			break
		}
		md = next
	}
	var out []string
	inFence, blank := false, 0
	for _, line := range strings.Split(md, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		} else if !inFence {
			if rxEmptyBullet.MatchString(line) {
				continue
			}
			line = strings.TrimRight(line, " \t")
			if line == "" {
				if blank++; blank > 1 {
					continue
				}
			} else {
				blank = 0
			}
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// A fence opening inside a blockquote: the quote prefix, then the fence.
var rxQuotedFence = regexp.MustCompile("^((?:> ?)+)(```|~~~)")

// quoteFences restores the quote prefix on every line of a fenced block
// inside a blockquote. The converter prefixes the fence and the first line
// and leaves the rest bare, which ends the quote in the middle of the
// listing and leaves a fence that never closes.
func quoteFences(md string) string {
	lines := strings.Split(md, "\n")
	prefix, fence := "", ""
	for i, line := range lines {
		if fence == "" {
			if m := rxQuotedFence.FindStringSubmatch(line); m != nil {
				prefix, fence = m[1], m[2]
			}
			continue
		}
		if !strings.HasPrefix(line, prefix) && line != strings.TrimRight(prefix, " ") {
			line = prefix + line
			lines[i] = line
		}
		if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(line, prefix)), fence) {
			fence = ""
		}
	}
	return strings.Join(lines, "\n")
}

// DebugSemanticRoot returns the root kind — "landmark", "sections", "prose"
// or "" for a page the semantic path declines — and the pruned root's HTML,
// so a harness can see exactly what the converter is given. Measurement
// scaffolding for the bench tooling.
func DebugSemanticRoot(rawHTML string, mode Mode) (kind, outer string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return "", ""
	}
	mathToText(doc)
	root, kind := semanticRoot(doc), "landmark"
	if root == nil {
		root, kind = uniformSectionRoot(doc), "sections"
	}
	if root == nil {
		root, kind = proseRoot(doc, mode), "prose"
	}
	if root == nil {
		return "", ""
	}
	pruneChrome(root, doc, "", mode)
	rescueTitle(doc, root)
	outer, _ = goquery.OuterHtml(root)
	return kind, outer
}

// DebugProseTrace reports each step of proseRoot's descent: the children at
// every level with their prose words and whether they would be left behind.
// Measurement scaffolding for the bench tooling.
func DebugProseTrace(rawHTML string, mode Mode) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return err.Error()
	}
	body := doc.Find("body")
	if body.Length() == 0 {
		return "no body"
	}
	title := pageTitleOf(doc)
	var b strings.Builder
	root := body
	for depth := 0; depth < 40; depth++ {
		b.WriteString(sig(root) + "  pw=" + strconv.Itoa(paragraphWords(root.Nodes[0])) + "\n")
		next, reason := proseStep(root, title, mode)
		root.Children().Each(func(_ int, c *goquery.Selection) {
			mark := "  "
			if next != nil && c.Nodes[0] == next.Nodes[0] {
				mark = "* "
			}
			d := ""
			if droppable(c, title, true, mode) {
				d = " droppable"
			}
			t := strings.Join(strings.Fields(c.Text()), " ")
			if len(t) > 70 {
				t = t[:70]
			}
			b.WriteString("  " + mark + sig(c) + " pw=" + strconv.Itoa(paragraphWords(c.Nodes[0])) + " w=" + strconv.Itoa(wordCount(c)) + d + "  " + t + "\n")
		})
		if next == nil {
			b.WriteString("  stop: " + reason + "\n")
			break
		}
		root = next
	}
	return b.String()
}

func sig(s *goquery.Selection) string {
	out := goquery.NodeName(s)
	if id, ok := s.Attr("id"); ok && id != "" {
		out += "#" + id
	}
	if class, ok := s.Attr("class"); ok {
		for _, c := range strings.Fields(class) {
			out += "." + c
		}
	}
	return out
}
