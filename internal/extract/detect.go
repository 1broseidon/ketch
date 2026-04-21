package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// DetectJSShell analyzes raw HTML and returns whether the page appears to be
// a JavaScript shell that needs browser rendering for content extraction.
// Returns: "static" (has content), "likely_shell" (needs browser), or "ambiguous".
func DetectJSShell(rawHTML string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return "ambiguous"
	}
	return DetectJSShellFromDoc(doc, rawHTML)
}

// DetectJSShellFromDoc is the core detector given an already-parsed document
// and its raw source. Useful when callers have already paid for the parse.
func DetectJSShellFromDoc(doc *goquery.Document, rawHTML string) string {
	// Phase 1: collect visible text + meaningful block count in a single pass.
	// Most pages short-circuit here without touching script/noscript/body.
	vis := scanVisible(doc)

	if len(vis.text) >= 200 {
		// Pages that explicitly require JS AND say "loading" are shells,
		// even when they include a fallback description (e.g. draw.io).
		// isJSLoadingPage handles the lowercase internally and only pays
		// that cost on pages whose visible text mentions "loading".
		if isJSLoadingPage(vis.text) {
			return "likely_shell"
		}
		return "static"
	}

	if vis.meaningfulBlocks > 2 {
		return "ambiguous"
	}

	// Phase 2: low-text pages need corroborators. Cheap DOM-local checks
	// run first; the full-source lowercase is only paid for if they miss.
	if hasCorroborator(doc, vis, rawHTML) {
		return "likely_shell"
	}
	return "ambiguous"
}

type visibleStats struct {
	text             string
	meaningfulBlocks int
}

// scanVisible traverses the content selectors once. It's the only work we
// do on the hot "static" path.
func scanVisible(doc *goquery.Document) visibleStats {
	var s visibleStats
	visible := make([]string, 0, 16)
	doc.Find("p, article, main, section, h1, h2, h3, h4, h5, h6, li, td, th, dd, dt, blockquote").
		Each(func(_ int, sel *goquery.Selection) {
			text := normalizeWhitespace(sel.Text())
			if text == "" {
				return
			}
			visible = append(visible, text)
			switch goquery.NodeName(sel) {
			case "p", "li", "h1", "h2", "h3", "h4", "h5", "h6", "td", "blockquote", "dd":
				if len(text) > 20 {
					s.meaningfulBlocks++
				}
			}
		})
	s.text = strings.Join(visible, " ")
	return s
}

// hasCorroborator runs the expensive signals — script bytes, noscript text,
// body JS messages, and string markers against the HTML. Cheaper DOM-local
// checks run first, and the full-source lowercase is deferred until the
// cheap checks miss.
func hasCorroborator(doc *goquery.Document, vis visibleStats, rawHTML string) bool {
	if noscriptMentionsJS(doc) {
		return true
	}
	if bodyRequiresJavaScript(doc) {
		return true
	}
	lowerHTML := strings.ToLower(rawHTML)
	if hasSPAShellMarker(lowerHTML) || hasLowTextAppShellMarker(lowerHTML) {
		return true
	}
	return highScriptToTextRatio(doc, vis.text)
}

func noscriptMentionsJS(doc *goquery.Document) bool {
	found := false
	doc.Find("noscript").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if strings.Contains(strings.ToLower(sel.Text()), "javascript") {
			found = true
			return false
		}
		return true
	})
	return found
}

func bodyRequiresJavaScript(doc *goquery.Document) bool {
	return requiresJavaScript(strings.ToLower(doc.Find("body").Text()))
}

func highScriptToTextRatio(doc *goquery.Document, visibleText string) bool {
	scriptBytes := 0
	doc.Find("script").Each(func(_ int, sel *goquery.Selection) {
		scriptBytes += len(sel.Text())
	})
	visibleBytes := len(visibleText)
	if visibleBytes == 0 {
		visibleBytes = 1
	}
	return scriptBytes > visibleBytes*3
}

// isJSLoadingPage detects pages that have fallback content but are actually
// JS loading screens (e.g. draw.io's splash with marketing blurb). Requires
// BOTH a loading indicator AND an explicit JS requirement. Lowercases
// lazily — only when the first check passes.
func isJSLoadingPage(visibleText string) bool {
	lower := strings.ToLower(visibleText)
	return strings.Contains(lower, "loading") && requiresJavaScript(lower)
}

func requiresJavaScript(lower string) bool {
	return strings.Contains(lower, "enable javascript") ||
		strings.Contains(lower, "requires javascript") ||
		strings.Contains(lower, "ensure javascript") ||
		strings.Contains(lower, "javascript is required") ||
		strings.Contains(lower, "javascript is disabled")
}

// spaMarkers is a package-level slice so the strings aren't re-allocated
// on every call. All markers are ASCII; the input is pre-lowercased.
var spaMarkers = []string{
	`id="__next"`, `id='__next'`,
	`id="__nuxt"`, `id='__nuxt'`,
	`data-reactroot`,
	`ng-version=`,
	`<app-root`,
	`id="___gatsby"`, `id='___gatsby'`,
	`__next_data__`,
	`__nuxt__`,
}

func hasSPAShellMarker(lowerHTML string) bool {
	for _, marker := range spaMarkers {
		if strings.Contains(lowerHTML, marker) {
			return true
		}
	}
	return false
}

func hasLowTextAppShellMarker(lowerHTML string) bool {
	return strings.Contains(lowerHTML, `id="app"`) || strings.Contains(lowerHTML, `id='app'`)
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
