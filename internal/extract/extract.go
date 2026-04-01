package extract

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	readability "codeberg.org/readeck/go-readability/v2"
	md "github.com/JohannesKaufmann/html-to-markdown/v2"
)

type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

// Result holds extracted content from a page.
type Result struct {
	Title            string
	Markdown         string
	Summary          string
	CanonicalURL     string
	Headings         []Heading
	Links            []string
	Quality          *Quality
	ExtractionSource string
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
	meta := extractMetadata(pageURL, html)

	// Try readability first — clean article extraction
	parser := readability.NewParser()
	article, err := parser.Parse(strings.NewReader(html), u)
	if err == nil {
		var buf bytes.Buffer
		if renderErr := article.RenderHTML(&buf); renderErr == nil {
			markdown, convErr := md.ConvertString(buf.String())
			if convErr == nil && strings.TrimSpace(markdown) != "" {
				title := strings.TrimSpace(article.Title())
				if title == "" {
					title = meta.Title
				}
				summary := meta.Summary
				if summary == "" {
					summary = summarizeMarkdown(markdown)
				}
				return &Result{
					Title:            title,
					Markdown:         strings.TrimSpace(markdown),
					Summary:          summary,
					CanonicalURL:     meta.CanonicalURL,
					Headings:         meta.Headings,
					Links:            meta.Links,
					Quality:          assessQuality(pageURL, html, title, strings.TrimSpace(markdown), meta.Headings, meta.Links),
					ExtractionSource: "readability",
				}, nil
			}
		}
	}

	// Fallback: convert full HTML to markdown directly
	return extractRaw(pageURL, html, meta)
}

// extractRaw converts the full HTML to markdown without readability.
// Noisier output (includes nav, footer, etc.) but never fails on valid HTML.
func extractRaw(pageURL, html string, meta pageMetadata) (*Result, error) {
	title := meta.Title
	if title == "" {
		title = extractTitle(html)
	}

	markdown, err := md.ConvertString(html)
	if err != nil {
		return nil, err
	}

	markdown = strings.TrimSpace(markdown)
	summary := meta.Summary
	if summary == "" {
		summary = summarizeMarkdown(markdown)
	}
	if markdown == "" {
		return &Result{
			Title:            title,
			Markdown:         "",
			Summary:          summary,
			CanonicalURL:     meta.CanonicalURL,
			Headings:         meta.Headings,
			Links:            meta.Links,
			Quality:          assessQuality(pageURL, html, title, "", meta.Headings, meta.Links),
			ExtractionSource: "raw_html",
		}, nil
	}

	return &Result{
		Title:            title,
		Markdown:         markdown,
		Summary:          summary,
		CanonicalURL:     meta.CanonicalURL,
		Headings:         meta.Headings,
		Links:            meta.Links,
		Quality:          assessQuality(pageURL, html, title, markdown, meta.Headings, meta.Links),
		ExtractionSource: "raw_html",
	}, nil
}

// extractTitle pulls the <title> tag content from raw HTML.
func extractTitle(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Find("title").First().Text())
}

type pageMetadata struct {
	Title        string
	Summary      string
	CanonicalURL string
	Headings     []Heading
	Links        []string
}

func extractMetadata(pageURL, html string) pageMetadata {
	meta := pageMetadata{}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return meta
	}

	meta.Title = strings.TrimSpace(doc.Find("title").First().Text())
	meta.Summary = summaryFromDocument(doc)
	meta.CanonicalURL = canonicalURL(pageURL, doc)
	meta.Headings = extractHeadings(doc)
	meta.Links = extractLinks(pageURL, doc)
	return meta
}

func summaryFromDocument(doc *goquery.Document) string {
	selectors := []string{
		`meta[name="description"]`,
		`meta[property="og:description"]`,
		"main p",
		"article p",
		"p",
	}
	for _, selector := range selectors {
		selection := doc.Find(selector).First()
		if selection.Length() == 0 {
			continue
		}
		candidate := strings.TrimSpace(selection.AttrOr("content", selection.Text()))
		candidate = normalizeWhitespace(candidate)
		if candidate == "" {
			continue
		}
		if len(candidate) > 280 {
			return strings.TrimSpace(candidate[:280])
		}
		return candidate
	}
	return ""
}

func canonicalURL(pageURL string, doc *goquery.Document) string {
	href, ok := doc.Find(`link[rel="canonical"]`).First().Attr("href")
	if !ok || strings.TrimSpace(href) == "" {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return strings.TrimSpace(href)
	}
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return strings.TrimSpace(href)
	}
	return base.ResolveReference(ref).String()
}

func extractHeadings(doc *goquery.Document) []Heading {
	headings := make([]Heading, 0, 12)
	doc.Find("h1, h2, h3, h4, h5, h6").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		text := normalizeWhitespace(selection.Text())
		if text == "" {
			return true
		}
		level := 0
		switch goquery.NodeName(selection) {
		case "h1":
			level = 1
		case "h2":
			level = 2
		case "h3":
			level = 3
		case "h4":
			level = 4
		case "h5":
			level = 5
		case "h6":
			level = 6
		}
		headings = append(headings, Heading{Level: level, Text: text})
		return len(headings) < 24
	})
	return headings
}

func extractLinks(pageURL string, doc *goquery.Document) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	links := make([]string, 0, 64)
	seen := make(map[string]struct{})
	doc.Find("a[href]").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		href := strings.TrimSpace(selection.AttrOr("href", ""))
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
			return true
		}
		ref, err := url.Parse(href)
		if err != nil {
			return true
		}
		resolved := base.ResolveReference(ref).String()
		if _, ok := seen[resolved]; ok {
			return true
		}
		seen[resolved] = struct{}{}
		links = append(links, resolved)
		return true
	})
	return links
}

func summarizeMarkdown(markdown string) string {
	trimmed := strings.TrimSpace(markdown)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "\n\n")
	for _, part := range parts {
		candidate := strings.TrimSpace(part)
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		candidate = normalizeWhitespace(candidate)
		if len(candidate) > 280 {
			return strings.TrimSpace(candidate[:280])
		}
		return candidate
	}
	if len(trimmed) > 280 {
		return strings.TrimSpace(trimmed[:280])
	}
	return normalizeWhitespace(trimmed)
}
