package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Anchor is a fragment target a page carries: an element id or an <a
// name>. Text is set for a heading's anchor (h1-h6, or a definition term
// such as Sphinx's API signatures): the heading's visible words, permalink
// glyphs aside, so a heading read from the page's Markdown can be matched
// to the id the page really gives it.
type Anchor struct {
	ID   string `json:"i"`
	Text string `json:"t,omitempty"`
}

// maxAnchors caps the anchors kept from one page.
const maxAnchors = 5000

// Anchors returns every fragment target in html, in document order, each
// once: headings first claim theirs with their text, then every other id
// and <a name> follows without text. A heading's anchor is its own id,
// else an <a id> or <a name> inside it, else the in-page permalink inside
// it whose target exists (Sphinx's headerlink), else the id of the
// element it opens (a Sphinx <section id>). A page with no targets gives
// an empty, non-nil slice: the page was read and carries none.
func Anchors(rawHTML string) []Anchor {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return []Anchor{}
	}
	return AnchorsOf(doc)
}

// AnchorsOf is Anchors over a parsed document.
func AnchorsOf(doc *goquery.Document) []Anchor {
	ids := map[string]bool{}
	var order []string
	add := func(id string) {
		if id = strings.TrimSpace(id); id != "" && !ids[id] {
			ids[id] = true
			order = append(order, id)
		}
	}
	doc.Find("[id], a[name]").Each(func(_ int, s *goquery.Selection) {
		if id, ok := s.Attr("id"); ok {
			add(id)
		}
		if goquery.NodeName(s) == "a" {
			name, _ := s.Attr("name")
			add(name)
		}
	})
	out := make([]Anchor, 0, min(len(order), maxAnchors))
	labelled := map[string]bool{}
	doc.Find("h1, h2, h3, h4, h5, h6, dt").Each(func(_ int, s *goquery.Selection) {
		if len(out) >= maxAnchors {
			return
		}
		id := headingID(s, ids)
		if id == "" {
			return
		}
		text := headingText(s)
		if text == "" {
			return
		}
		labelled[id] = true
		out = append(out, Anchor{ID: id, Text: text})
	})
	for _, id := range order {
		if len(out) >= maxAnchors {
			break
		}
		if !labelled[id] {
			out = append(out, Anchor{ID: id})
		}
	}
	return out
}

// headingID is the fragment that reaches heading s, or "".
func headingID(s *goquery.Selection, ids map[string]bool) string {
	if id, ok := s.Attr("id"); ok && strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	var found string
	s.Find("a[id], a[name]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		found = firstAttr(a, "id", "name")
		return found == ""
	})
	if found != "" {
		return found
	}
	s.Find(`a[href^="#"]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href")
		if target := strings.TrimPrefix(href, "#"); ids[target] {
			found = target
		}
		return found == ""
	})
	if found != "" {
		return found
	}
	// The heading opens its parent: <section id="x"><h2>…</h2>.
	if prev := s.PrevAllFiltered("*"); prev.Length() == 0 {
		if id, ok := s.Parent().Attr("id"); ok && goquery.NodeName(s.Parent()) != "body" {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

func firstAttr(s *goquery.Selection, names ...string) string {
	for _, n := range names {
		if v, ok := s.Attr(n); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// headingText is a heading's visible text with permalink decorations
// (isPermalink: a link holding only a glyph, or labelled as one) left
// out.
func headingText(s *goquery.Selection) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "svg", "button":
				return
			case "a":
				if isPermalink(goquery.NewDocumentFromNode(n).Selection) {
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range s.Nodes {
		walk(n)
	}
	text := strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		}
		return r
	}, b.String())
	return strings.Join(strings.Fields(text), " ")
}
