package extract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	stdhtml "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

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
			table.WithHeaderPromotion(true),
			table.WithSkipEmptyRows(true),
			table.WithNewlineBehavior(table.NewlineBehaviorPreserve),
			table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
		),
	),
)

var markdownLinkRE = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

type tableCandidate struct {
	fingerprint string
	html        string
}

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

	rawDoc, rawDocErr := goquery.NewDocumentFromReader(strings.NewReader(html))
	var tableCandidates []tableCandidate
	var rawTitle string
	if rawDocErr == nil {
		tableCandidates = collectDataTables(rawDoc)
		rawTitle = titleFromDocument(rawDoc)
	}

	// Try readability first — clean article extraction
	parser := readability.NewParser()
	article, err := parser.Parse(strings.NewReader(html), u)
	if err == nil {
		var buf bytes.Buffer
		if renderErr := article.RenderHTML(&buf); renderErr == nil {
			markdown, convErr := markdownConverter.ConvertString(buf.String())
			markdown = strings.TrimSpace(markdown)
			if convErr == nil && markdown != "" {
				markdown = reinjectLostTables(markdown, tableCandidates)
				return &Result{
					Title:    article.Title(),
					Markdown: markdown,
				}, nil
			}
		}
	}

	// Fallback: convert full HTML to markdown directly
	if rawTitle == "" {
		return extractRaw(html)
	}
	return extractRawWithTitle(html, rawTitle)
}

// extractRaw converts the full HTML to markdown without readability.
// Noisier output (includes nav, footer, etc.) but never fails on valid HTML.
func extractRaw(html string) (*Result, error) {
	return extractRawWithTitle(html, Title(html))
}

func extractRawWithTitle(html, title string) (*Result, error) {
	markdown, err := markdownConverter.ConvertString(html)
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

func reinjectLostTables(markdown string, candidates []tableCandidate) string {
	if len(candidates) == 0 {
		return markdown
	}

	surviving := markdownTableFingerprints(markdown)
	additions := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if count := surviving[candidate.fingerprint]; count > 0 {
			surviving[candidate.fingerprint] = count - 1
			continue
		}

		tableMarkdown, err := markdownConverter.ConvertString(candidate.html)
		if err != nil {
			continue
		}
		tableMarkdown = strings.TrimSpace(tableMarkdown)
		if tableMarkdown == "" {
			continue
		}
		additions = append(additions, tableMarkdown)
	}

	if len(additions) == 0 {
		return markdown
	}

	parts := append([]string{strings.TrimSpace(markdown)}, additions...)
	return strings.Join(parts, "\n\n")
}

func collectDataTables(doc *goquery.Document) []tableCandidate {
	var candidates []tableCandidate
	doc.Find("table").Each(func(_ int, table *goquery.Selection) {
		if isLayoutTable(table) || !isDataTable(table) {
			return
		}

		fingerprint := htmlTableFingerprint(table)
		if fingerprint == "" {
			return
		}

		tableHTML, err := goquery.OuterHtml(table)
		if err != nil || strings.TrimSpace(tableHTML) == "" {
			return
		}

		candidates = append(candidates, tableCandidate{
			fingerprint: fingerprint,
			html:        tableHTML,
		})
	})
	return candidates
}

func isDataTable(table *goquery.Selection) bool {
	rows, maxColumns, hasTheadTH := tableMetrics(table)
	return rows >= 10 || maxColumns > 4 || hasTheadTH
}

func tableMetrics(table *goquery.Selection) (rows, maxColumns int, hasTheadTH bool) {
	tableNode := table.Get(0)
	if tableNode == nil {
		return 0, 0, false
	}

	table.Find("tr").Each(func(_ int, row *goquery.Selection) {
		if closestTableNode(row) != tableNode {
			return
		}
		rows++

		columns := 0
		row.Find("th,td").Each(func(_ int, cell *goquery.Selection) {
			if closestTableNode(cell) != tableNode {
				return
			}
			columns += tableCellColspan(cell)
		})
		if columns > maxColumns {
			maxColumns = columns
		}
	})

	table.Find("thead th").EachWithBreak(func(_ int, cell *goquery.Selection) bool {
		if closestTableNode(cell) == tableNode {
			hasTheadTH = true
			return false
		}
		return true
	})

	return rows, maxColumns, hasTheadTH
}

func tableCellColspan(cell *goquery.Selection) int {
	value, ok := cell.Attr("colspan")
	if !ok {
		return 1
	}
	span, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || span < 1 {
		return 1
	}
	return span
}

func closestTableNode(selection *goquery.Selection) interface{} {
	closest := selection.ParentsFiltered("table").First()
	if closest.Length() == 0 {
		return nil
	}
	return closest.Get(0)
}

func isLayoutTable(table *goquery.Selection) bool {
	return hasLayoutMarker(table)
}

func hasLayoutMarker(selection *goquery.Selection) bool {
	if role, ok := selection.Attr("role"); ok && strings.EqualFold(strings.TrimSpace(role), "presentation") {
		return true
	}
	class, _ := selection.Attr("class")
	return hasLayoutClass(class)
}

func hasLayoutClass(class string) bool {
	class = strings.ToLower(class)
	for _, marker := range []string{"navbox", "sidebar", "toc", "ambox"} {
		if strings.Contains(class, marker) {
			return true
		}
	}
	return false
}

func htmlTableFingerprint(table *goquery.Selection) string {
	tableNode := table.Get(0)
	if tableNode == nil {
		return ""
	}

	var cells []string
	table.Find("th,td").Each(func(_ int, cell *goquery.Selection) {
		if closestTableNode(cell) != tableNode {
			return
		}
		cells = append(cells, cell.Text())
	})
	return fingerprintCells(cells)
}

func markdownTableFingerprints(markdown string) map[string]int {
	fingerprints := make(map[string]int)
	lines := strings.Split(markdown, "\n")
	for i := 1; i < len(lines); i++ {
		if !isMarkdownTableSeparator(lines[i]) || !isMarkdownTableRow(lines[i-1]) {
			continue
		}

		cells := splitMarkdownTableRow(lines[i-1])
		j := i + 1
		for ; j < len(lines) && isMarkdownTableRow(lines[j]); j++ {
			cells = append(cells, splitMarkdownTableRow(lines[j])...)
		}

		fingerprint := fingerprintCells(cells)
		if fingerprint != "" {
			fingerprints[fingerprint]++
		}
		i = j - 1
	}
	return fingerprints
}

func isMarkdownTableRow(line string) bool {
	return len(splitMarkdownTableRow(line)) > 1
}

func isMarkdownTableSeparator(line string) bool {
	cells := splitMarkdownTableRow(line)
	if len(cells) < 2 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" || strings.Count(cell, "-") < 3 {
			return false
		}
		if strings.Trim(cell, ":- ") != "" {
			return false
		}
	}
	return true
}

func splitMarkdownTableRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "|") {
		return nil
	}
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}

	var cells []string
	var cell strings.Builder
	escaped := false
	for _, r := range line {
		if escaped {
			cell.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '|' {
			cells = append(cells, cell.String())
			cell.Reset()
			continue
		}
		cell.WriteRune(r)
	}
	if escaped {
		cell.WriteRune('\\')
	}
	cells = append(cells, cell.String())
	return cells
}

func fingerprintCells(cells []string) string {
	var normalized strings.Builder
	for _, cell := range cells {
		text := normalizeFingerprintText(cell)
		if text == "" {
			continue
		}
		if normalized.Len() > 0 {
			normalized.WriteByte('\x1f')
		}
		normalized.WriteString(text)
	}
	if normalized.Len() == 0 {
		return ""
	}

	sum := sha256.Sum256([]byte(normalized.String()))
	return hex.EncodeToString(sum[:])
}

func normalizeFingerprintText(text string) string {
	text = markdownLinkRE.ReplaceAllString(text, "$1")
	text = stdhtml.UnescapeString(text)
	text = strings.ToLower(text)

	var normalized strings.Builder
	previousSpace := true
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			normalized.WriteRune(r)
			previousSpace = false
		case unicode.IsSpace(r):
			if !previousSpace {
				normalized.WriteByte(' ')
				previousSpace = true
			}
		}
	}
	return strings.TrimSpace(normalized.String())
}

func titleFromDocument(doc *goquery.Document) string {
	return strings.TrimSpace(doc.Find("title").First().Text())
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
