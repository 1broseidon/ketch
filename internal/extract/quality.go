package extract

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Quality struct {
	Indexable       bool   `json:"indexable"`
	ContentType     string `json:"content_type,omitempty"`
	NavigationHeavy bool   `json:"navigation_heavy,omitempty"`
	RedirectTarget  string `json:"redirect_target,omitempty"`
}

func assessQuality(pageURL, html, title, markdown string, headings []Heading, links []string) *Quality {
	quality := &Quality{
		Indexable:   true,
		ContentType: "article",
	}

	if redirectTarget := detectSoftRedirectTarget(pageURL, html); redirectTarget != "" {
		quality.Indexable = false
		quality.ContentType = "soft_redirect"
		quality.RedirectTarget = redirectTarget
		return quality
	}

	if isNavigationHeavy(pageURL, title, markdown, headings, links) {
		quality.ContentType = "hub"
		quality.NavigationHeavy = true
	}

	return quality
}

func detectSoftRedirectTarget(pageURL, html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}

	title := normalizeWhitespace(doc.Find("title").First().Text())
	if title == "" {
		title = normalizeWhitespace(doc.Find("h1").First().Text())
	}
	bodyText := normalizeWhitespace(doc.Find("main, article, body").First().Text())
	metaRefreshTarget := extractMetaRefreshTarget(doc, pageURL)
	targetURL := metaRefreshTarget
	if targetURL == "" {
		targetURL = extractBodyTarget(bodyText, pageURL)
	}
	if targetURL == "" {
		targetURL = extractAnchorTarget(doc, pageURL)
	}

	if metaRefreshTarget != "" {
		return targetURL
	}

	titleSignal := regexp.MustCompile(`^(redirect notice|page moved|page relocated)$`).MatchString(strings.ToLower(title))
	relocationSignal := regexp.MustCompile(`page you requested has been relocated to|has been relocated to|has moved to|redirecting to|redirected to`).MatchString(strings.ToLower(bodyText))
	if titleSignal && relocationSignal {
		return targetURL
	}

	return ""
}

func extractMetaRefreshTarget(doc *goquery.Document, pageURL string) string {
	var target string
	doc.Find("meta[http-equiv]").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		if strings.ToLower(normalizeWhitespace(selection.AttrOr("http-equiv", ""))) != "refresh" {
			return true
		}
		content := normalizeWhitespace(selection.AttrOr("content", ""))
		match := regexp.MustCompile(`url\s*=\s*(.+)$`).FindStringSubmatch(content)
		if len(match) > 1 {
			target = resolveURL(match[1], pageURL)
		}
		return false
	})
	return target
}

func extractBodyTarget(bodyText, pageURL string) string {
	if match := regexp.MustCompile(`(?:relocated|moved|redirect(?:ed|ing)?)\s+to\s+(https?://[^\s)]+)`).FindStringSubmatch(bodyText); len(match) > 1 {
		return resolveURL(match[1], pageURL)
	}
	if match := regexp.MustCompile(`https?://[^\s)]+`).FindStringSubmatch(bodyText); len(match) > 0 {
		return resolveURL(match[0], pageURL)
	}
	return ""
}

func extractAnchorTarget(doc *goquery.Document, pageURL string) string {
	currentURL := strings.TrimSpace(pageURL)
	var target string
	doc.Find("main a[href], article a[href], body a[href]").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		href := strings.TrimSpace(selection.AttrOr("href", ""))
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(strings.ToLower(href), "javascript:") {
			return true
		}
		resolved := resolveURL(href, pageURL)
		if resolved == "" || resolved == currentURL {
			return true
		}
		target = resolved
		return false
	})
	return target
}

func resolveURL(value, currentURL string) string {
	base, err := url.Parse(strings.TrimSpace(currentURL))
	if err != nil {
		return ""
	}
	ref, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

func isNavigationHeavy(pageURL, title, markdown string, headings []Heading, links []string) bool {
	wordCount := len(strings.Fields(markdown))
	linkCount := len(links)
	if linkCount < 15 {
		return false
	}
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return false
	}
	path := strings.Trim(strings.ToLower(parsedURL.Path), "/")
	lowerTitle := strings.ToLower(title)
	articleMarker := path == "blog" || strings.HasPrefix(path, "blog/") || strings.HasSuffix(lowerTitle, " blog")
	hubMarker := path == "" ||
		strings.HasSuffix(path, "docs") ||
		strings.Contains(path, "tutorial-list") ||
		strings.Contains(lowerTitle, "documentation") ||
		strings.Contains(lowerTitle, "resource center") ||
		strings.Contains(lowerTitle, "configuration guides")
	if articleMarker && wordCount >= 20 {
		return false
	}
	if hubMarker && linkCount >= 20 {
		return true
	}
	if wordCount <= 160 {
		return true
	}
	if linkCount*6 >= wordCount {
		return true
	}
	return len(headings) >= 4 && hubMarker
}
