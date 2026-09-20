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
		if attr.Key == "style" && inlineCodeHidden(attr.Val) {
			return true
		}
	}
	return false
}

// Recognize simple inline visibility declarations, not substrings inside custom
// properties or quoted values. Complex CSS needs a browser; retain its text.
func inlineCodeHidden(style string) bool {
	if strings.ContainsAny(style, `"'()\`) || strings.Contains(style, "/*") {
		return false
	}
	values := make(map[string]string)
	priorities := make(map[string]bool)
	for _, declaration := range strings.Split(strings.ToLower(style), ";") {
		property, value, ok := strings.Cut(declaration, ":")
		property = strings.TrimSpace(property)
		if !ok || (property != "display" && property != "visibility") {
			continue
		}
		value = strings.TrimSpace(value)
		keyword, priority, hasPriority := strings.Cut(value, "!")
		important := strings.TrimSpace(priority) == "important"
		if hasPriority && !important {
			continue
		}
		if !priorities[property] || important {
			values[property] = strings.TrimSpace(keyword)
			priorities[property] = important
		}
	}
	return values["display"] == "none" || values["visibility"] == "hidden" || values["visibility"] == "collapse"
}
