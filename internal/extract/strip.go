package extract

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reFenced  = regexp.MustCompile("(?s)```.*?```")
	reBold    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldAlt = regexp.MustCompile(`__(.+?)__`)
	// reItalic requires a non-space char immediately after the opening * so that
	// unordered-list markers ("* item * more") are not treated as italic delimiters.
	reItalic    = regexp.MustCompile(`\*([^\s*\n][^*\n]*?)\*`)
	reItalicAlt = regexp.MustCompile(`_([^_\n]+?)_`)
	reImage     = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink      = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reHeading   = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reCode      = regexp.MustCompile("`([^`]+)`")
)

// StripMarkdown removes markdown formatting syntax while preserving content text.
// Images are removed entirely; links keep their text; headings keep their text.
// Fenced code blocks (``` ... ```) are left intact — their content is code, not prose.
func StripMarkdown(s string) string {
	// Protect fenced code blocks from inline processing by replacing them with
	// sentinel tokens, then restoring after all inline rules are applied.
	var fenced []string
	s = reFenced.ReplaceAllStringFunc(s, func(m string) string {
		i := len(fenced)
		fenced = append(fenced, m)
		return fmt.Sprintf("\x00FC%d\x00", i)
	})

	s = reBold.ReplaceAllString(s, "$1")
	s = reBoldAlt.ReplaceAllString(s, "$1")
	s = reItalic.ReplaceAllString(s, "$1")
	s = reItalicAlt.ReplaceAllString(s, "$1")
	s = reImage.ReplaceAllString(s, "")
	s = reLink.ReplaceAllString(s, "$1")
	s = reHeading.ReplaceAllString(s, "$1")
	s = reCode.ReplaceAllString(s, "$1")

	for i, f := range fenced {
		s = strings.ReplaceAll(s, fmt.Sprintf("\x00FC%d\x00", i), f)
	}
	return s
}
