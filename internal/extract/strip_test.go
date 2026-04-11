package extract

import "testing"

func TestStripMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold asterisk", "**bold**", "bold"},
		{"bold underscore", "__bold__", "bold"},
		{"italic asterisk", "*italic*", "italic"},
		{"italic underscore", "_italic_", "italic"},
		{"inline code", "`code`", "code"},
		{"link", "[text](https://example.com)", "text"},
		{"image removed", "![alt](img.png)", ""},
		{"heading", "# Heading", "Heading"},
		{"heading h3", "### Sub", "Sub"},
		{"bullet list not mangled", "* item one\n* item two", "* item one\n* item two"},
		{"bullet with inline italic", "* see *this* now", "* see this now"},
		{"fenced code left intact", "```\nfoo()\n```", "```\nfoo()\n```"},
		{"mixed", "**bold** and *italic* with [link](url)", "bold and italic with link"},
		{"nested bold-italic", "***both***", "both"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := StripMarkdown(c.in)
			if got != c.want {
				t.Errorf("\ninput: %q\ngot:   %q\nwant:  %q", c.in, got, c.want)
			}
		})
	}
}
