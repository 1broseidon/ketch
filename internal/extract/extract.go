package extract

import (
	"bytes"
	"net/url"
	"strings"

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
// and converts it to markdown.
func (e *Extractor) Extract(pageURL, html string) (*Result, error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}

	parser := readability.NewParser()
	article, err := parser.Parse(strings.NewReader(html), u)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := article.RenderHTML(&buf); err != nil {
		return nil, err
	}

	markdown, err := md.ConvertString(buf.String())
	if err != nil {
		return nil, err
	}

	return &Result{
		Title:    article.Title(),
		Markdown: strings.TrimSpace(markdown),
	}, nil
}
