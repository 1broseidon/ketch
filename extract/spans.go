package extract

import (
	"regexp"
	"strconv"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
)

// maxSpan is the largest colspan or rowspan a table cell keeps. Browsers
// clamp colspan to 1,000 (HTML's table model), and some sites write
// "span every column" as Number.MAX_SAFE_INTEGER (colSpan="9007199254740991");
// the table plugin lays such a cell out cell by cell, so one header row
// grew a process past 60 GB. No docs table is wider than this.
const maxSpan = 100

// spanAttrRe matches a colspan or rowspan attribute with a number of
// three or more digits.
var spanAttrRe = regexp.MustCompile(`(?i)\b(colspan|rowspan)(\s*=\s*["']?)(\d{3,})`)

// clampSpans caps every colspan and rowspan at maxSpan.
func clampSpans(html string) string {
	return spanAttrRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := spanAttrRe.FindStringSubmatch(m)
		if n, err := strconv.ParseUint(sub[3], 10, 64); err == nil && n <= maxSpan {
			return m
		}
		return sub[1] + sub[2] + strconv.Itoa(maxSpan)
	})
}

// toMarkdown converts HTML with the shared converter, spans clamped.
func toMarkdown(html string, opts ...converter.ConvertOptionFunc) (string, error) {
	return markdownConverter.ConvertString(clampSpans(html), opts...)
}
