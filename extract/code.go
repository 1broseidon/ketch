package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// normalizeCodeBlocks turns highlighted markup into literal code before
// readability can mistake syntax tokens (notably class="comment") for chrome.
// Some highlighters render lines as blocks without newline text nodes.
func normalizeCodeBlocks(rawHTML string) string {
	if !strings.Contains(strings.ToLower(rawHTML), "<pre") {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return rawHTML
	}
	unwrapLineNumberTables(doc)
	doc.Find("pre").Each(func(_ int, pre *goquery.Selection) {
		var text strings.Builder
		writeCodeText(&text, pre.Nodes[0])
		code := &html.Node{Type: html.ElementNode, Data: "code"}
		if language := codeLanguage(pre); language != "" {
			code.Attr = []html.Attribute{{Key: "class", Val: language}}
		}
		code.AppendChild(&html.Node{Type: html.TextNode, Data: text.String()})
		pre.Empty()
		pre.Nodes[0].AppendChild(code)
	})
	result, err := doc.Html()
	if err != nil {
		return rawHTML
	}
	return result
}

func codeLanguage(pre *goquery.Selection) string {
	for _, s := range []*goquery.Selection{pre.Find("code").First(), pre} {
		classes, _ := s.Attr("class")
		for _, class := range strings.Fields(classes) {
			if strings.HasPrefix(class, "language-") || strings.HasPrefix(class, "lang-") {
				return class
			}
		}
	}
	return ""
}

func writeCodeText(out *strings.Builder, node *html.Node) {
	if node.Type == html.TextNode {
		out.WriteString(node.Data)
		return
	}
	if node.Type != html.ElementNode || hiddenCodeNode(node) {
		return
	}
	if node.Data == "br" {
		out.WriteByte('\n')
		return
	}
	block := node.Data == "div" || node.Data == "p"
	if block && out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
		out.WriteByte('\n')
	}
	start := out.Len()
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		writeCodeText(out, child)
	}
	if block && (out.Len() == start || !strings.HasSuffix(out.String(), "\n")) {
		out.WriteByte('\n')
	}
}

func hiddenCodeNode(node *html.Node) bool {
	switch node.Data {
	case "script", "style", "button", "template":
		return true
	}
	for _, attr := range node.Attr {
		if attr.Key == "hidden" || (attr.Key == "aria-hidden" && strings.EqualFold(strings.TrimSpace(attr.Val), "true")) {
			return true
		}
		if attr.Key == "style" && inlineHidden(attr.Val) {
			return true
		}
		if attr.Key == "class" && lineNumberClass(attr.Val) && !codeContainer(node) {
			return true
		}
	}
	return false
}

// codeContainer reports whether node holds the code rather than a gutter
// beside it. Prism switches its plugin on with a line-numbers class on the
// pre or code element itself and draws the numbers in a span it adds at
// runtime, so the container's class never means its text is line numbers.
func codeContainer(node *html.Node) bool {
	return node.Data == "pre" || node.Data == "code"
}

// Highlighters that print line numbers put them in the markup: Pygments in
// span.linenos, Sphinx and Rouge in a gutter cell beside the code,
// highlight.js in td.hljs-ln-numbers. None of it is code. The same words on
// the pre or code element itself are a plugin switch, not a gutter.
var lineNumberClasses = tokenSet(`linenos lineno linenumber linenumbers line-number line-numbers line-numbers-rows linenodiv gutter rouge-gutter hljs-ln-numbers hljs-ln-n ln-num line-num`)

func lineNumberClass(class string) bool {
	for _, tok := range strings.Fields(class) {
		if lineNumberClasses[strings.ToLower(tok)] {
			return true
		}
	}
	return false
}

// unwrapLineNumberTables replaces a highlighter's two-cell table — a
// gutter of line numbers beside the code — with the code cell's content,
// so the numbers do not come out as a listing of their own.
func unwrapLineNumberTables(doc *goquery.Document) {
	doc.Find("table").Each(func(_ int, t *goquery.Selection) {
		cells := t.Find("td, th")
		if cells.Length() != 2 {
			return
		}
		var gutter, code *goquery.Selection
		cells.Each(func(_ int, c *goquery.Selection) {
			if class, _ := c.Attr("class"); lineNumberClass(class) {
				gutter = c
			} else if c.Find("pre").Length() > 0 {
				code = c
			}
		})
		if gutter == nil || code == nil {
			return
		}
		t.ReplaceWithSelection(code.Children())
	})
}

// inlineHidden reports whether an inline style hides the element: an
// effective display of none, or visibility hidden or collapse. Declarations
// are parsed, not pattern-matched, so a custom property such as
// --fallback-display:none, a quoted value, or a url() holding a semicolon
// never counts, and a later or !important declaration overrides an earlier
// one the way CSS does. Anything a browser needs a stylesheet for is left
// visible.
func inlineHidden(style string) bool {
	values := make(map[string]string)
	priorities := make(map[string]bool)
	for _, declaration := range splitDeclarations(stripCSSComments(style)) {
		property, value, ok := strings.Cut(declaration, ":")
		property = strings.ToLower(strings.TrimSpace(property))
		if !ok || (property != "display" && property != "visibility") {
			continue
		}
		keyword, priority, hasPriority := strings.Cut(strings.TrimSpace(value), "!")
		important := strings.EqualFold(strings.TrimSpace(priority), "important")
		if hasPriority && !important {
			continue
		}
		if !priorities[property] || important {
			values[property] = strings.ToLower(strings.TrimSpace(keyword))
			priorities[property] = important
		}
	}
	return values["display"] == "none" || values["visibility"] == "hidden" || values["visibility"] == "collapse"
}

// splitDeclarations splits an inline style on the semicolons that end a
// declaration, leaving those inside quotes or parentheses alone.
func splitDeclarations(style string) []string {
	var out []string
	var quote byte
	depth, start := 0, 0
	for i := 0; i < len(style); i++ {
		c := style[i]
		switch {
		case quote != 0:
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')' && depth > 0:
			depth--
		case c == ';' && depth == 0:
			out = append(out, style[start:i])
			start = i + 1
		}
	}
	return append(out, style[start:])
}

// stripCSSComments removes /* */ comments from an inline style.
func stripCSSComments(style string) string {
	for {
		open := strings.Index(style, "/*")
		if open < 0 {
			return style
		}
		end := strings.Index(style[open+2:], "*/")
		if end < 0 {
			return style[:open]
		}
		style = style[:open] + " " + style[open+2+end+2:]
	}
}
