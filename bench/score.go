package main

import (
	"bytes"
	stdhtml "html"
	"regexp"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	mdhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
)

// Scoring deliberately does not import ketch/extract or its converter. Goldmark
// parses the emitted Markdown; the reference is frozen source-DOM text.
func score(markdown, reference string, checks []assertion) (accuracy, error) {
	var rendered bytes.Buffer
	// Valid Markdown can contain HTML tables and <br>. Rendering those into a
	// detached Go DOM does not execute scripts or fetch resources; text scoring
	// below excludes script/style/template bodies.
	parser := goldmark.New(goldmark.WithExtensions(extension.Table), goldmark.WithRendererOptions(mdhtml.WithUnsafe()))
	if err := parser.Convert([]byte(markdown), &rendered); err != nil {
		return accuracy{}, err
	}
	doc, err := goquery.NewDocumentFromReader(&rendered)
	if err != nil {
		return accuracy{}, err
	}
	text := plainText(doc.Selection)
	a := tokenCoverage(text, reference)
	for _, check := range checks {
		passed := assertionPasses(doc, text, check)
		a.Checks = append(a.Checks, checkResult{ID: check.ID, Kind: check.Kind, Critical: check.Critical, Passed: passed})
	}
	return a, nil
}

func assertionPasses(doc *goquery.Document, text string, a assertion) bool {
	switch a.Kind {
	case "text":
		return containsPhrase(text, a.Text)
	case "absent":
		return !containsPhrase(text, a.Text)
	case "heading":
		return selectionHas(doc.Find("h1,h2,h3,h4,h5,h6"), func(s *goquery.Selection) bool {
			return normalized(plainText(s)) == normalized(a.Text)
		})
	case "code":
		return selectionHas(doc.Find("pre"), func(s *goquery.Selection) bool {
			return strings.Contains(codeText(s.Text()), codeText(a.Text))
		})
	case "link":
		return selectionHas(doc.Find("a[href],img[src]"), func(s *goquery.Selection) bool {
			link, _ := s.Attr("href")
			image, _ := s.Attr("src")
			return link == a.Text || image == a.Text
		})
	case "table":
		return selectionHas(doc.Find("table"), func(s *goquery.Selection) bool { return tableMatches(s, a) })
	}
	return false
}

func selectionHas(sel *goquery.Selection, predicate func(*goquery.Selection) bool) bool {
	found := false
	sel.EachWithBreak(func(_ int, s *goquery.Selection) bool {
		found = predicate(s)
		return !found
	})
	return found
}

func tableMatches(table *goquery.Selection, a assertion) bool {
	rows := table.Find("tr")
	if rows.Length() < 2 || !cellsMatch(rows.First().Find("th,td"), a.Headers) {
		return false
	}
	return selectionHas(rows.Slice(1, rows.Length()), func(s *goquery.Selection) bool {
		return cellsMatch(s.Find("th,td"), a.Cells)
	})
}

func cellsMatch(cells *goquery.Selection, expected []string) bool {
	if cells.Length() != len(expected) {
		return false
	}
	for i, text := range expected {
		if literalText(plainText(cells.Eq(i))) != literalText(text) {
			return false
		}
	}
	return true
}

func codeText(s string) string {
	return strings.Trim(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(stdhtml.UnescapeString(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func normalized(s string) string { return strings.Join(words(s), " ") }

func literalText(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

func containsPhrase(text, phrase string) bool {
	// Phrase assertions preserve punctuation: reversing >= to <= must fail even
	// though token overlap is unchanged. Case and layout whitespace may differ.
	pattern := `(?:^|[^\p{L}\p{N}])` + regexp.QuoteMeta(literalText(phrase)) + `(?:$|[^\p{L}\p{N}])`
	return regexp.MustCompile(pattern).MatchString(literalText(text))
}

func tokenCoverage(output, reference string) accuracy {
	out, ref := words(output), words(reference)
	counts := make(map[string]int)
	for _, word := range ref {
		counts[word]++
	}
	matched := 0
	for _, word := range out {
		if counts[word] > 0 {
			matched++
			counts[word]--
		}
	}
	a := accuracy{ReferenceWords: len(ref), OutputWords: len(out)}
	if len(ref) > 0 {
		a.TokenRecall = float64(matched) / float64(len(ref))
	}
	if len(out) > 0 {
		a.TokenPrecision = float64(matched) / float64(len(out))
	}
	if a.TokenRecall+a.TokenPrecision > 0 {
		a.TokenF1 = 2 * a.TokenRecall * a.TokenPrecision / (a.TokenRecall + a.TokenPrecision)
	}
	return a
}

func plainText(sel *goquery.Selection) string {
	var out strings.Builder
	for _, node := range sel.Nodes {
		writeVisibleText(&out, node)
	}
	return out.String()
}

func writeVisibleText(out *strings.Builder, node *html.Node) {
	if node.Type == html.TextNode {
		out.WriteString(node.Data)
		return
	}
	if node.Type == html.ElementNode && strings.Contains(" script style template ", " "+node.Data+" ") {
		return
	}
	block := strings.Contains(" p div section article main li dt dd tr td th h1 h2 h3 h4 h5 h6 pre br hr blockquote ", " "+node.Data+" ")
	if block {
		out.WriteByte('\n')
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		writeVisibleText(out, child)
	}
	if block {
		out.WriteByte('\n')
	}
}

func failedAccuracy(f fixture) accuracy {
	n := f.ReferenceWords
	if f.Reference != "" {
		n = len(words(f.Reference))
	}
	a := accuracy{ReferenceWords: n}
	for _, c := range f.Spec.Checks {
		a.Checks = append(a.Checks, checkResult{ID: c.ID, Kind: c.Kind, Critical: c.Critical})
	}
	return a
}
