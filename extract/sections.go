package extract

import (
	"regexp"
	"strings"
)

// Section is one heading-delimited slice of a markdown document. Sections
// are the unit of retrieval for the local docs backend, and the shape maps
// onto docs.Result: Heading is the title, Breadcrumb the path, Body the
// snippet, and Anchor the fragment for the page URL.
type Section struct {
	// Level is the heading level (1–6), or 0 for text that precedes the first
	// heading.
	Level int
	// Heading is the cleaned heading text (inline code, emphasis, and link
	// syntax removed). For the preamble it is the page title.
	Heading string
	// Breadcrumb is the page title followed by the ancestor headings down to
	// and including this one. When the first heading repeats the page title,
	// it is not duplicated.
	Breadcrumb []string
	// Anchor is a GitHub-style slug of the heading ("" for the preamble).
	// Static-site generators mostly agree on this scheme; it is best-effort.
	Anchor string
	// Body is the section text with the heading line removed and surrounding
	// whitespace trimmed. Never empty: heading-only sections are dropped
	// (their heading still appears in descendants' breadcrumbs).
	Body string
	// Part is the zero-based index when a long section was split at
	// paragraph boundaries to respect the caller's size cap. Every part
	// shares the same Heading, Breadcrumb, and Anchor.
	Part int
}

var (
	atxHeading  = regexp.MustCompile(`^(#{1,6})[ \t]+(.*?)[ \t]*#*[ \t]*$`)
	mdLink      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdImage     = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	slugStrip   = regexp.MustCompile(`[^\p{L}\p{N}\s_-]`)
	slugSpaces  = regexp.MustCompile(`\s+`)
	slugHyphens = regexp.MustCompile(`-{2,}`)
)

// Sections splits markdown at ATX headings (# through ######), tracking the
// heading hierarchy so each section carries a breadcrumb from the page title
// down. Fenced code blocks are never split and a "#" inside a fence is not a
// heading. Text before the first heading becomes a level-0 preamble section
// titled with the page title. Sections whose body exceeds maxChars are split
// at blank-line boundaries (falling back to line boundaries for a single
// oversized block); maxChars <= 0 disables splitting.
//
// The function is deterministic: the same input always yields the same
// sections in the same order.
func Sections(title, markdown string, maxChars int) []Section {
	title = strings.TrimSpace(title)
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")

	type frame struct {
		level   int
		heading string
	}
	var (
		stack    []frame
		out      []Section
		body     []string
		current  *Section
		inFence  bool
		fenceTok string
	)

	flush := func() {
		if current == nil {
			return
		}
		text := strings.TrimSpace(strings.Join(body, "\n"))
		body = body[:0]
		if text == "" {
			return
		}
		for i, part := range splitBody(text, maxChars) {
			s := *current
			s.Breadcrumb = append([]string(nil), current.Breadcrumb...)
			s.Body = part
			s.Part = i
			out = append(out, s)
		}
	}

	// The preamble is open from the start; it is dropped by flush if empty.
	current = &Section{Level: 0, Heading: title, Breadcrumb: crumbs(title, nil)}

	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if tok, ok := fenceToken(trimmed); ok {
			if !inFence {
				inFence, fenceTok = true, tok
			} else if strings.HasPrefix(tok, fenceTok) {
				inFence = false
			}
			body = append(body, line)
			continue
		}
		if inFence {
			body = append(body, line)
			continue
		}
		m := atxHeading.FindStringSubmatch(line)
		if m == nil {
			body = append(body, line)
			continue
		}
		flush()
		level := len(m[1])
		heading := cleanInline(m[2])
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, frame{level: level, heading: heading})
		path := make([]string, len(stack))
		for i, f := range stack {
			path[i] = f.heading
		}
		current = &Section{Level: level, Heading: heading, Breadcrumb: crumbs(title, path), Anchor: Slug(heading)}
	}
	flush()
	return out
}

// crumbs prefixes the heading path with the page title unless the first
// heading already is the title (case-insensitive), which is the common
// single-H1 layout.
func crumbs(title string, path []string) []string {
	if title == "" {
		if len(path) == 0 {
			return nil
		}
		return append([]string(nil), path...)
	}
	if len(path) > 0 && strings.EqualFold(path[0], title) {
		return append([]string(nil), path...)
	}
	return append([]string{title}, path...)
}

// fenceToken reports whether a (left-trimmed) line opens or closes a fenced
// code block, returning the fence marker (``` or ~~~, possibly longer).
func fenceToken(trimmed string) (string, bool) {
	for _, ch := range []byte{'`', '~'} {
		n := 0
		for n < len(trimmed) && trimmed[n] == ch {
			n++
		}
		if n >= 3 {
			return trimmed[:n], true
		}
	}
	return "", false
}

// cleanInline strips inline markdown from a heading: images and links keep
// their text, emphasis markers and backticks are removed.
func cleanInline(s string) string {
	s = mdImage.ReplaceAllString(s, "$1")
	s = mdLink.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("`", "", "**", "", "__", "", "*", "", "~~", "").Replace(s)
	return strings.TrimSpace(s)
}

// Slug converts heading text to a GitHub-style anchor: lower-case, letters,
// digits, hyphens, and underscores only, spaces collapsed to single hyphens.
func Slug(heading string) string {
	s := strings.ToLower(strings.TrimSpace(heading))
	s = slugStrip.ReplaceAllString(s, "")
	s = slugSpaces.ReplaceAllString(s, "-")
	s = slugHyphens.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// splitBody splits text into pieces no longer than maxChars where possible.
// It prefers blank-line boundaries outside code fences and falls back to
// line boundaries when one block alone exceeds the cap. A cap <= 0 returns
// the text whole.
func splitBody(text string, maxChars int) []string {
	if maxChars <= 0 || len(text) <= maxChars {
		return []string{text}
	}
	var parts []string
	var cur strings.Builder
	emit := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			parts = append(parts, s)
		}
		cur.Reset()
	}
	for _, block := range blocks(text) {
		if cur.Len() > 0 && cur.Len()+len(block)+2 > maxChars {
			emit()
		}
		if len(block) > maxChars {
			emit()
			parts = append(parts, splitLines(block, maxChars)...)
			continue
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(block)
	}
	emit()
	return parts
}

// blocks splits text at blank lines, keeping fenced code blocks intact even
// when they contain blank lines.
func blocks(text string) []string {
	var out []string
	var cur []string
	inFence := false
	var fenceTok string
	flush := func() {
		if s := strings.TrimSpace(strings.Join(cur, "\n")); s != "" {
			out = append(out, s)
		}
		cur = cur[:0]
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if tok, ok := fenceToken(trimmed); ok {
			if !inFence {
				inFence, fenceTok = true, tok
			} else if strings.HasPrefix(tok, fenceTok) {
				inFence = false
			}
			cur = append(cur, line)
			continue
		}
		if !inFence && strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

// splitLines hard-splits one oversized block at line boundaries.
func splitLines(block string, maxChars int) []string {
	var parts []string
	var cur strings.Builder
	for _, line := range strings.Split(block, "\n") {
		if cur.Len() > 0 && cur.Len()+len(line)+1 > maxChars {
			parts = append(parts, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}
