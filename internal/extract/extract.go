package extract

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	readability "codeberg.org/readeck/go-readability/v2"
	md "github.com/JohannesKaufmann/html-to-markdown/v2"
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
	u, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}

	// Try readability first — clean article extraction
	parser := readability.NewParser()
	article, err := parser.Parse(strings.NewReader(html), u)
	if err == nil {
		var buf bytes.Buffer
		if renderErr := article.RenderHTML(&buf); renderErr == nil {
			markdown, convErr := md.ConvertString(buf.String())
			if convErr == nil && strings.TrimSpace(markdown) != "" {
				return &Result{
					Title:    article.Title(),
					Markdown: strings.TrimSpace(markdown),
				}, nil
			}
		}
	}

	// Fallback: convert full HTML to markdown directly
	return extractRaw(html)
}

// extractRaw converts the full HTML to markdown without readability.
// Noisier output (includes nav, footer, etc.) but never fails on valid HTML.
func extractRaw(html string) (*Result, error) {
	title := Title(html)

	markdown, err := md.ConvertString(html)
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

	html := strings.Join(parts, "\n\n")
	markdown, err := md.ConvertString(html)
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
