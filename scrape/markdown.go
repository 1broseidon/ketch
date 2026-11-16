package scrape

import (
	"net/url"
	"path"
	"strings"
)

// A raw Markdown response (a docs site's .md export, a README served as
// text/markdown) is already the page's text: running it through the HTML
// extractor flattens it into one paragraph, its headings and code blocks
// lost and its YAML frontmatter read as body text. It is kept as written,
// line endings normalised, with the frontmatter's title (else its first
// H1) as the page's title and the frontmatter itself dropped.

// isMarkdownResponse reports a response whose body is Markdown: declared
// text/markdown (or text/x-markdown), or plain text at a Markdown path.
func isMarkdownResponse(contentType, rawURL string) bool {
	switch contentType {
	case "text/markdown", "text/x-markdown":
		return true
	case "text/plain":
		if u, err := url.Parse(rawURL); err == nil {
			switch strings.ToLower(path.Ext(u.Path)) {
			case ".md", ".mdx", ".markdown":
				return true
			}
		}
	}
	return false
}

// markdownPage is a Markdown body as a page's title and text.
func markdownPage(body []byte) (title, md string) {
	md = strings.TrimPrefix(string(body), "\ufeff")
	md = strings.ReplaceAll(md, "\r\n", "\n")
	title, md = splitFrontmatter(md)
	if title == "" {
		title = firstMarkdownH1(md)
	}
	return title, strings.TrimLeft(md, "\n")
}

// splitFrontmatter is a leading YAML frontmatter block's title and the
// text after the block; "" and md when there is none.
func splitFrontmatter(md string) (string, string) {
	rest := strings.TrimLeft(md, "\n")
	if !strings.HasPrefix(rest, "---\n") {
		return "", md
	}
	lines := strings.Split(rest, "\n")
	for i := 1; i < len(lines) && i < 200; i++ {
		switch strings.TrimSpace(lines[i]) {
		case "---", "...":
			title := ""
			for _, l := range lines[1:i] {
				if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "title" {
					title = strings.Trim(strings.TrimSpace(v), `"'`)
					break
				}
			}
			return title, strings.Join(lines[i+1:], "\n")
		}
	}
	return "", md
}

// firstMarkdownH1 is the text of the first "# " heading outside code
// fences, "" for none.
func firstMarkdownH1(md string) string {
	fence := ""
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			switch {
			case fence == "":
				fence = t[:3]
			case strings.HasPrefix(t, fence):
				fence = ""
			}
			continue
		}
		if fence == "" && strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(t[2:]), "#"))
		}
	}
	return ""
}
