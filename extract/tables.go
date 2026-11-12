package extract

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Table cells. The table plugin writes a Markdown table only when every
// cell's content fits one row: a list, quote, rule, heading, code block
// or table inside a cell, a link or emphasis around the table, or
// role="presentation" on it, and it gives up and prints the rows' text
// with nothing between the cells ("NameDescription", "`delay`Line one").
// Option tables whose description cell lists the allowed values are the
// common case. So before conversion:
//
//   - a cell's blocks become lines of the cell, which the plugin keeps as
//     <br>: a list's items ("• x", "1. x"), a code block's lines as inline
//     code, a quote's text, a heading's text; a rule is a line break;
//   - a table that still cannot be one (a table inside a cell, a link or
//     emphasis around it, a layout table) has its own cells made blocks,
//     so each prints on its own line, the tables inside it as tables.
func tableCells(rawHTML string) string {
	if !strings.Contains(strings.ToLower(rawHTML), "<table") {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return rawHTML
	}
	doc.Find("table").Each(func(_ int, t *goquery.Selection) {
		if t.Find("table").Length() > 0 || unconvertibleTable(t) {
			ownCells(t).Each(func(_ int, c *goquery.Selection) { c.Nodes[0].Data = "div" })
			return
		}
		ownCells(t).Each(func(_ int, c *goquery.Selection) { cellLines(c) })
	})
	out, err := doc.Html()
	if err != nil {
		return rawHTML
	}
	return out
}

// unconvertibleTable reports a table the plugin never converts whatever
// its cells hold: one marked as layout, or inside a link or emphasis.
func unconvertibleTable(t *goquery.Selection) bool {
	if role, _ := t.Attr("role"); role == "presentation" {
		return true
	}
	return t.ParentsFiltered("a, strong, b, em, i, del, s, strike").Length() > 0
}

// ownCells are the cells of t itself, not of a table inside it.
func ownCells(t *goquery.Selection) *goquery.Selection {
	return t.Find("td, th").FilterFunction(func(_ int, c *goquery.Selection) bool {
		return c.Closest("table").IsSelection(t)
	})
}

// cellLines turns the blocks in a cell into lines of it, innermost first
// (a list in a list item is that item's lines).
func cellLines(c *goquery.Selection) {
	blocks := c.Find("ul, ol, pre, blockquote, hr, h1, h2, h3, h4, h5, h6")
	for i := blocks.Length() - 1; i >= 0; i-- {
		b := blocks.Eq(i)
		switch n := b.Nodes[0]; n.Data {
		case "ul", "ol":
			var items []string
			b.ChildrenFiltered("li").Each(func(k int, li *goquery.Selection) {
				mark := "• "
				if n.Data == "ol" {
					mark = strconv.Itoa(k+1) + ". "
				}
				inner, _ := li.Html()
				items = append(items, mark+strings.TrimSpace(inner))
			})
			b.ReplaceWithHtml("<br>" + strings.Join(items, "<br>") + "<br>")
		case "pre":
			var lines []string
			for _, l := range strings.Split(strings.Trim(b.Text(), "\n"), "\n") {
				if strings.TrimSpace(l) != "" {
					lines = append(lines, "<code>"+html.EscapeString(l)+"</code>")
				}
			}
			b.ReplaceWithHtml("<br>" + strings.Join(lines, "<br>") + "<br>")
		case "hr":
			n.Data = "br"
		default: // a quote or heading: its text, a block of the cell
			n.Data = "div"
		}
	}
}

// brRunRe is three or more line breaks in a row, as the table plugin
// writes a cell's paragraphs and nested blocks (eight for a <p> in a <div>).
var brRunRe = regexp.MustCompile(`(?:<br />[ \t]*){3,}`)

// cellBreaks collapses each run of line breaks in a table row to two, a
// paragraph break: the run says nothing more and costs a reader tokens.
func cellBreaks(md string) string {
	if !strings.Contains(md, "<br />") {
		return md
	}
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "|") {
			lines[i] = brRunRe.ReplaceAllString(l, "<br /><br />")
		}
	}
	return strings.Join(lines, "\n")
}
