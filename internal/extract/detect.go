package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const (
	visibleTextSelector     = "p, article, main, section, h1, h2, h3, h4, h5, h6, li, td, th, dd, dt, blockquote"
	meaningfulBlockSelector = "p, li, h1, h2, h3, h4, h5, h6, td, blockquote, dd"
)

// DetectJSShell analyzes raw HTML and returns whether the page appears to be
// a JavaScript shell that needs browser rendering for content extraction.
// Returns: "static" (has content), "likely_shell" (needs browser), or "ambiguous"
func DetectJSShell(rawHTML string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return "ambiguous"
	}

	visibleText := collectVisibleText(doc)
	if len(visibleText) >= 200 {
		return "static"
	}

	if countMeaningfulBlocks(doc) <= 2 && hasCorroborator(doc, rawHTML, visibleText) {
		return "likely_shell"
	}

	return "ambiguous"
}

func collectVisibleText(doc *goquery.Document) string {
	parts := make([]string, 0, 16)

	doc.Find(visibleTextSelector).Each(func(_ int, selection *goquery.Selection) {
		text := normalizeWhitespace(selection.Text())
		if text != "" {
			parts = append(parts, text)
		}
	})

	return strings.Join(parts, " ")
}

func countMeaningfulBlocks(doc *goquery.Document) int {
	count := 0

	doc.Find(meaningfulBlockSelector).Each(func(_ int, selection *goquery.Selection) {
		if len(normalizeWhitespace(selection.Text())) > 20 {
			count++
		}
	})

	return count
}

func hasCorroborator(doc *goquery.Document, rawHTML, visibleText string) bool {
	return hasJavaScriptNoscript(doc) ||
		hasSPAShellMarker(rawHTML) ||
		hasLowTextAppShellMarker(rawHTML) ||
		hasHighScriptToTextRatio(doc, visibleText)
}

func hasJavaScriptNoscript(doc *goquery.Document) bool {
	found := false

	doc.Find("noscript").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		text := strings.ToLower(normalizeWhitespace(selection.Text()))
		if strings.Contains(text, "javascript") {
			found = true
			return false
		}
		return true
	})

	return found
}

func hasSPAShellMarker(rawHTML string) bool {
	lowerHTML := strings.ToLower(rawHTML)

	markers := []string{
		`id="__next"`,
		`id='__next'`,
		`id="__nuxt"`,
		`id='__nuxt'`,
		`data-reactroot`,
		`ng-version=`,
		`<app-root`,
		`id="___gatsby"`,
		`id='___gatsby'`,
		`__next_data__`,
		`__nuxt__`,
	}

	for _, marker := range markers {
		if strings.Contains(lowerHTML, marker) {
			return true
		}
	}

	return false
}

func hasLowTextAppShellMarker(rawHTML string) bool {
	lowerHTML := strings.ToLower(rawHTML)
	return strings.Contains(lowerHTML, `id="app"`) || strings.Contains(lowerHTML, `id='app'`)
}

func hasHighScriptToTextRatio(doc *goquery.Document, visibleText string) bool {
	scriptBytes := 0

	doc.Find("script").Each(func(_ int, selection *goquery.Selection) {
		scriptBytes += len(selection.Text())
	})

	visibleBytes := len(visibleText)
	if visibleBytes == 0 {
		visibleBytes = 1
	}

	return scriptBytes > visibleBytes*3
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
