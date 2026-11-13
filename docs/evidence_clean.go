package docs

import (
	"regexp"
	"strings"
)

// Cleaning (design/docs-evidence.md, Sort): what extraction leaked into a
// section's text, taken out as the section enters the pool, so an index
// built before these rules is cleaned too. Otherwise the text is verbatim.
//
//   - HTML tags in prose keep their text: <span class="toreplace">X</span>
//     is X, and <br></br> is a line break (in a table row, where a line
//     break ends the row, the one a cell can hold: <br>). A code block is
//     never touched: HTML in it is the example's own (a parser's output, a
//     template), and a tag an extractor leaked there cannot be told from
//     one.
//   - A line that is only badge or tracking images (an image inside a
//     link, or one with no alt text) goes.
//   - A Markdown escape that changes nothing goes: pam\_duo is pam_duo.
//     Inline code and code blocks keep theirs.
//   - MDX residue in prose goes: a line that is only JSX component tags
//     (a capitalised name: <Sandpack>, </DeepDive>, <Intro />), and MDX
//     comments ({/* … */}, on one line or across lines). Code blocks and
//     inline code keep theirs.

// cleanText is text with the cleaning rules applied: a pure function.
func cleanText(s string) string {
	if !strings.ContainsAny(s, "<\\!{") {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	var fence fenceState
	inComment := false // inside an MDX comment that spans lines
	for _, line := range lines {
		inFence := fence.n > 0
		if !inFence && (inComment || strings.Contains(line, "{/*")) {
			var empty bool
			if line, inComment, empty = cutMDXComments(line, inComment); empty {
				continue
			}
		}
		switch isFence := fence.step(line); {
		case isFence: // a code block, its fences included, is code: it stays as it is
			out = append(out, line)
		case badgeLine(line), jsxTagLine(line):
		default:
			out = append(out, cleanLine(line))
		}
	}
	return strings.Join(out, "\n")
}

// cleanLine is one line of prose, a heading or a table row cleaned: its
// HTML tags and no-op escapes out, inline code kept as it is.
func cleanLine(line string) string {
	if strings.Contains(line, "{/*") {
		line, _, _ = cutMDXComments(line, false)
	}
	if !strings.ContainsAny(line, "<\\") {
		return line
	}
	row := strings.HasPrefix(strings.TrimSpace(line), "|")
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '`' {
			n := backtickRun(line, i)
			if end := closingBackticks(line, i+n, n); end >= 0 {
				b.WriteString(line[i : end+n])
				i = end + n
				continue
			}
			b.WriteString(line[i : i+n])
			i += n
			continue
		}
		next := strings.IndexByte(line[i:], '`')
		end := len(line)
		if next >= 0 {
			end = i + next
		}
		b.WriteString(cleanProse(line, i, end, row))
		i = end
	}
	return b.String()
}

// cleanProse cleans line[from:to], a stretch outside inline code: HTML
// tags, then escapes, which read the whole line for what precedes them.
func cleanProse(line string, from, to int, row bool) string {
	seg := line[from:to]
	if strings.IndexByte(seg, '<') >= 0 {
		seg = brPairRe.ReplaceAllString(seg, "<br>")
		seg = leakedTagRe.ReplaceAllStringFunc(seg, func(tag string) string {
			if brRe.MatchString(tag) {
				if row {
					return "<br>"
				}
				return "\n"
			}
			m := leakedTagRe.FindStringSubmatch(tag)
			name := strings.ToLower(m[2])
			if !proseTags[name] {
				return tag
			}
			if name == "img" {
				if alt := altRe.FindStringSubmatch(m[3]); alt != nil {
					return alt[1]
				}
				return ""
			}
			return ""
		})
	}
	if strings.IndexByte(seg, '\\') < 0 {
		return seg
	}
	var b strings.Builder
	for i := 0; i < len(seg); i++ {
		if seg[i] == '\\' && i+1 < len(seg) && noopEscape(seg, i, row) {
			continue
		}
		b.WriteByte(seg[i])
	}
	return b.String()
}

// noopEscape reports a backslash at s[i] whose escape changes nothing:
// an underscore inside a word (pam\_duo: an underscore there never opens
// emphasis); . - + # ! ( ) where they cannot start a list item, a
// heading, an image or a link's address. \| in a table row separates
// nothing and stays; so does every other escape.
func noopEscape(s string, i int, row bool) bool {
	c := s[i+1]
	lead := strings.TrimLeft(s[:i], " \t")
	switch c {
	case '_':
		return i > 0 && isWordByte(s[i-1]) && i+2 < len(s) && isWordByte(s[i+2])
	case '.':
		return strings.Trim(lead, "0123456789") != ""
	case '-', '+', '#':
		return lead != "" && !row
	case '!':
		return i+2 >= len(s) || s[i+2] != '['
	case '(', ')':
		return i == 0 || s[i-1] != ']'
	}
	return false
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

var (
	// leakedTagRe matches an HTML tag: its slash (closing), name and
	// attributes.
	leakedTagRe = regexp.MustCompile(`<(/?)([A-Za-z][A-Za-z0-9]*)((?:\s+[^<>]*)?)\s*/?>`)
	// brRe matches a line break tag: <br>, <br/>, </br>; brPairRe one
	// with the closing tag some pages put after it, <br></br>, which is
	// one break.
	brRe     = regexp.MustCompile(`(?i)^(?:<br\s*/?>|</br>)$`)
	brPairRe = regexp.MustCompile(`(?i)<br\s*/?>\s*</br>`)
	altRe    = regexp.MustCompile(`(?i)\balt\s*=\s*"([^"]*)"`)
	// imageRe matches a Markdown image, optionally the whole text of a
	// link: its opening bracket, alt text and the link's closing part.
	imageRe = regexp.MustCompile(`(\[)?!\[([^\]]*)\]\([^)]*\)(\]\([^)]*\))?`)
)

// proseTags are the HTML elements whose tags a page's prose leaks: inline
// formatting and blocks an extractor should have turned into Markdown.
// A word in angle brackets that names none of them (<url>, <name>) is a
// placeholder, and stays.
var proseTags = map[string]bool{
	"a": true, "abbr": true, "b": true, "big": true, "br": true, "center": true, "cite": true, "code": true,
	"del": true, "details": true, "div": true, "em": true, "figcaption": true, "figure": true, "font": true,
	"i": true, "img": true, "ins": true, "kbd": true, "mark": true, "nobr": true, "p": true, "s": true,
	"section": true, "small": true, "span": true, "strike": true, "strong": true, "sub": true, "summary": true,
	"sup": true, "tt": true, "u": true, "wbr": true,
}

// badgeLine reports a line that is only badge or tracking images: every
// image inside a link (a badge) or without alt text (a pixel).
func badgeLine(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.Contains(t, "![") {
		return false
	}
	ms := imageRe.FindAllStringSubmatchIndex(t, -1)
	if len(ms) == 0 || strings.TrimSpace(imageRe.ReplaceAllString(t, "")) != "" {
		return false
	}
	for _, m := range ms {
		linked := m[2] >= 0 && m[6] >= 0
		if !linked && strings.TrimSpace(t[m[4]:m[5]]) != "" {
			return false
		}
	}
	return true
}

// jsxTagRe matches a line that is only JSX component tags: opening,
// closing or self-closing, each a capitalised name (a component, never an
// HTML element), its attributes on the line.
var jsxTagRe = regexp.MustCompile(`^\s*(?:</?[A-Z][A-Za-z0-9_.]*(?:\s+[^<>]*?)?\s*/?>\s*)+$`)

// jsxTagLine reports a line of prose that is only JSX component tags.
func jsxTagLine(line string) bool {
	return strings.Contains(line, "<") && jsxTagRe.MatchString(line)
}

// cutMDXComments takes the MDX comments ({/* … */}) out of a line of
// prose, outside inline code; in is whether the line starts inside a
// comment an earlier line opened. It returns the line, whether a comment
// is still open at its end, and whether the line held nothing but
// comments (and space), so that it goes.
func cutMDXComments(line string, in bool) (string, bool, bool) {
	var b strings.Builder
	cut := in
	for i := 0; i < len(line); {
		if in {
			end := strings.Index(line[i:], "*/}")
			if end < 0 {
				return b.String(), true, strings.TrimSpace(b.String()) == ""
			}
			i += end + 3
			in = false
			continue
		}
		if line[i] == '`' {
			n := backtickRun(line, i)
			if end := closingBackticks(line, i+n, n); end >= 0 {
				b.WriteString(line[i : end+n])
				i = end + n
				continue
			}
		}
		if strings.HasPrefix(line[i:], "{/*") {
			in, cut = true, true
			i += 3
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	if !cut {
		return line, false, false
	}
	t := strings.TrimRight(b.String(), " \t")
	return t, false, strings.TrimSpace(t) == ""
}
