package extract

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

var markdownConverter = converter.NewConverter(
	converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		table.NewTablePlugin(
			table.WithNewlineBehavior(table.NewlineBehaviorPreserve),
			table.WithHeaderPromotion(true),
			table.WithSkipEmptyRows(true),
			table.WithCellPaddingBehavior(table.CellPaddingBehaviorNone),
			table.WithSpanCellBehavior(table.SpanBehaviorEmpty),
		),
	),
)

// Result holds extracted content from a page.
type Result struct {
	Title    string
	Markdown string
}

// Extractor converts raw HTML into clean markdown.
type Extractor struct{}

// New creates an Extractor.
func New() *Extractor {
	return &Extractor{}
}

// Extract takes a URL and raw HTML, extracts the main content,
// and converts it to markdown. Falls back to direct HTML→markdown
// conversion if readability extraction fails.
func (e *Extractor) Extract(pageURL, html string) (*Result, error) {
	html = stripDataURIs(html)
	u, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	origin := originOf(u)

	// Try readability first — clean article extraction
	parser := readability.NewParser()
	article, err := parser.Parse(strings.NewReader(html), u)
	if err == nil {
		var buf bytes.Buffer
		if renderErr := article.RenderHTML(&buf); renderErr == nil {
			markdown, convErr := markdownConverter.ConvertString(buf.String())
			markdown = strings.TrimSpace(markdown)
			if convErr == nil && markdown != "" {
				if raw, ok := rawTableFallback(origin, html, buf.String()); ok {
					if title := article.Title(); title != "" {
						raw.Title = title
					}
					return raw, nil
				}
				return &Result{
					Title:    article.Title(),
					Markdown: markdown,
				}, nil
			}
		}
	}

	// Fallback: convert full HTML to markdown directly
	return extractRaw(origin, html)
}

// originOf renders the scheme://host prefix used to absolutize relative links
// on conversion paths that don't go through readability.
func originOf(u *url.URL) string {
	if u == nil || u.Host == "" {
		return ""
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + u.Host
}

// rawTableFallback swaps readability's output for the noisier full-page
// conversion when readability dropped data tables the page actually carries.
// Readability is inconsistent about tables: it keeps some (a Wikipedia
// infobox) while stripping the main data table on the same page, so a
// presence check is not enough — we compare counts. See issue #28.
func rawTableFallback(origin, html, readabilityHTML string) (*Result, bool) {
	if !strings.Contains(strings.ToLower(html), "<table") {
		return nil, false
	}
	// Counting on the DOM rather than on rendered pipe tables lets us ignore
	// navigation and layout tables, which would otherwise buy a page full of
	// site chrome in exchange for a prev/next bar.
	if countDataTables(html) <= countDataTables(readabilityHTML) {
		return nil, false
	}
	raw, err := extractRaw(origin, html)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// countDataTables counts tables carrying real tabular content, ignoring
// layout and navigation tables.
func countDataTables(rawHTML string) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return 0
	}
	n := 0
	doc.Find("table").Each(func(_ int, t *goquery.Selection) {
		if isLayoutTable(t) {
			return
		}
		if t.Find("tr").Length() >= 2 && maxCols(t) >= 2 {
			n++
		}
	})
	return n
}

func isLayoutTable(t *goquery.Selection) bool {
	if role, _ := t.Attr("role"); role == "presentation" || role == "none" {
		return true
	}
	if datatable, _ := t.Attr("datatable"); datatable == "0" {
		return true
	}
	if t.Closest("nav, header, footer").Length() > 0 {
		return true
	}
	// A table the reader can't see isn't page content — e.g. pkg.go.dev keeps
	// its keyboard-shortcut grid in a closed <dialog>.
	if t.Closest("dialog, template, [hidden], [aria-hidden='true']").Length() > 0 {
		return true
	}
	// DocBook-style generators label their prev/next chrome explicitly rather
	// than wrapping it in <nav>, e.g. <table summary="Navigation header">.
	summary, _ := t.Attr("summary")
	class, _ := t.Attr("class")
	return hasNavToken(summary) || hasNavToken(class)
}

func hasNavToken(s string) bool {
	s = strings.ToLower(s)
	for _, tok := range []string{"navigation", "navbar", "navbox", "navheader", "navfooter"} {
		if strings.Contains(s, tok) {
			return true
		}
	}
	return false
}

func maxCols(t *goquery.Selection) int {
	n := 0
	t.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		if cols := tr.Find("td, th").Length(); cols > n {
			n = cols
		}
	})
	return n
}

// extractRaw converts the full HTML to markdown without readability.
// Noisier output (includes nav, footer, etc.) but never fails on valid HTML.
// origin absolutizes relative links, which readability would otherwise have
// resolved for us.
func extractRaw(origin, html string) (*Result, error) {
	title := Title(html)

	markdown, err := markdownConverter.ConvertString(html, converter.WithDomain(origin))
	if err != nil {
		return nil, err
	}

	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return &Result{Title: title, Markdown: ""}, nil
	}

	return &Result{
		Title:    title,
		Markdown: markdown,
	}, nil
}

// ExtractSelector runs a CSS selector against raw HTML and returns the
// matched elements converted to markdown. If no elements match, returns
// an empty string and no error.
func ExtractSelector(rawHTML, selector string) (string, error) {
	// Select against the original DOM so attribute-value selectors (e.g.
	// img[src^="data:"]) still match; strip data URIs from the extracted
	// fragment afterward, before markdown conversion.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return "", err
	}

	sel := doc.Find(selector)
	if sel.Length() == 0 {
		return "", nil
	}

	var parts []string
	var outerErr error
	sel.Each(func(_ int, s *goquery.Selection) {
		if outerErr != nil {
			return
		}
		h, err := goquery.OuterHtml(s)
		if err != nil {
			outerErr = err
			return
		}
		parts = append(parts, h)
	})
	if outerErr != nil {
		return "", outerErr
	}

	html := stripDataURIs(strings.Join(parts, "\n\n"))
	markdown, err := markdownConverter.ConvertString(html)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(markdown), nil
}

// Title pulls the <title> tag content from raw HTML.
func Title(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Find("title").First().Text())
}

// stripDataURIs replaces data: URI img sources with compact markers before
// HTML→markdown conversion, preventing large base64 payloads from reaching
// markdown output (a single inline image can inject 100k+ tokens into context).
// The marker preserves MIME type and approximate size for debugging.
func stripDataURIs(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html
	}
	var replaced int
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if !exists || !strings.HasPrefix(strings.ToLower(src), "data:") {
			return
		}
		mime, size := dataURIInfo(src)
		s.SetAttr("src", "omitted")
		s.SetAttr("alt", fmt.Sprintf("data-uri omitted: %s, %d bytes", mime, size))
		replaced++
	})
	if replaced == 0 {
		return html
	}
	out, err := goquery.OuterHtml(doc.Selection)
	if err != nil {
		return html
	}
	return out
}

// dataURIInfo extracts the MIME type and approximate decoded byte size from a
// data: URI. Falls back to "image" and 0 on malformed input.
func dataURIInfo(src string) (mime string, size int) {
	mime = "image"
	rest := strings.TrimPrefix(src, "data:")
	if i := strings.IndexByte(rest, ';'); i != -1 {
		mime = rest[:i]
	} else if i := strings.IndexByte(rest, ','); i != -1 {
		mime = rest[:i]
	}
	if len(mime) > 64 {
		mime = mime[:64]
	}
	if comma := strings.IndexByte(src, ','); comma != -1 {
		size = len(src[comma+1:]) * 3 / 4
	}
	return
}
