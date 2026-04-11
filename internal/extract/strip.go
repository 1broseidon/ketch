package extract

import "regexp"

var (
	reBold      = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldAlt   = regexp.MustCompile(`__(.+?)__`)
	reItalic    = regexp.MustCompile(`\*([^*\n]+?)\*`)
	reItalicAlt = regexp.MustCompile(`_([^_\n]+?)_`)
	reImage     = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink      = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reHeading   = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reCode      = regexp.MustCompile("`([^`]+)`")
)

// StripMarkdown removes markdown formatting syntax while preserving content text.
// Images are removed entirely; links keep their text; headings keep their text.
func StripMarkdown(s string) string {
	s = reBold.ReplaceAllString(s, "$1")
	s = reBoldAlt.ReplaceAllString(s, "$1")
	s = reItalic.ReplaceAllString(s, "$1")
	s = reItalicAlt.ReplaceAllString(s, "$1")
	s = reImage.ReplaceAllString(s, "")
	s = reLink.ReplaceAllString(s, "$1")
	s = reHeading.ReplaceAllString(s, "$1")
	s = reCode.ReplaceAllString(s, "$1")
	return s
}
