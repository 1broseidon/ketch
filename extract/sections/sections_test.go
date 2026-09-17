package sections

import (
	"reflect"
	"strings"
	"testing"
)

const nestedDoc = `Intro paragraph before any heading.

# Glamour

Glamour renders markdown.

## Installation

` + "```sh\ngo get github.com/charmbracelet/glamour\n# not a heading\n```" + `

## Usage

### Styles

Pick a style with WithStylePath.

### Word wrap

Set the width.

## FAQ
`

func TestSectionsNestingAndBreadcrumbs(t *testing.T) {
	got := Split("Glamour", nestedDoc, 0)

	want := []struct {
		level  int
		crumbs []string
		anchor string
		body   string
	}{
		{0, []string{"Glamour"}, "", "Intro paragraph before any heading."},
		{1, []string{"Glamour"}, "glamour", "Glamour renders markdown."},
		{2, []string{"Glamour", "Installation"}, "installation", "```sh\ngo get github.com/charmbracelet/glamour\n# not a heading\n```"},
		{3, []string{"Glamour", "Usage", "Styles"}, "styles", "Pick a style with WithStylePath."},
		{3, []string{"Glamour", "Usage", "Word wrap"}, "word-wrap", "Set the width."},
	}
	if len(got) != len(want) {
		for _, s := range got {
			t.Logf("%d %v %q", s.Level, s.Breadcrumb, s.Body)
		}
		t.Fatalf("got %d sections, want %d", len(got), len(want))
	}
	for i, w := range want {
		s := got[i]
		if s.Level != w.level || s.Anchor != w.anchor || s.Body != w.body || !reflect.DeepEqual(s.Breadcrumb, w.crumbs) {
			t.Errorf("section %d = level %d %v anchor %q body %q; want level %d %v anchor %q body %q",
				i, s.Level, s.Breadcrumb, s.Anchor, s.Body, w.level, w.crumbs, w.anchor, w.body)
		}
	}
}

// "## Usage" holds no text of its own and must not become a section, but it
// still appears in its children's breadcrumbs. "## FAQ" is heading-only at
// the very end and is dropped.
func TestSectionsDropHeadingOnlySplit(t *testing.T) {
	for _, s := range Split("Glamour", nestedDoc, 0) {
		if s.Heading == "Usage" || s.Heading == "FAQ" {
			t.Errorf("heading-only section %q should have been dropped", s.Heading)
		}
	}
}

func TestSectionsTitleNotDuplicatedWhenH1Matches(t *testing.T) {
	got := Split("Glamour", "# glamour\n\nbody\n\n## Sub\n\nmore", 0)
	if len(got) != 2 {
		t.Fatalf("got %d sections", len(got))
	}
	if !reflect.DeepEqual(got[1].Breadcrumb, []string{"glamour", "Sub"}) {
		t.Errorf("breadcrumb = %v, want title folded into the H1", got[1].Breadcrumb)
	}
}

func TestSectionsTitlePrefixedWhenH1Differs(t *testing.T) {
	got := Split("Tailwind CSS", "# Container queries\n\nbody", 0)
	if len(got) != 1 || !reflect.DeepEqual(got[0].Breadcrumb, []string{"Tailwind CSS", "Container queries"}) {
		t.Fatalf("sections = %+v", got)
	}
}

func TestSectionsPreambleOnly(t *testing.T) {
	got := Split("Notes", "just text\n\nno headings", 0)
	if len(got) != 1 || got[0].Level != 0 || got[0].Heading != "Notes" || got[0].Anchor != "" {
		t.Fatalf("sections = %+v", got)
	}
}

func TestSectionsEmptyInput(t *testing.T) {
	if got := Split("T", "", 0); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
	if got := Split("T", "# Only\n\n## Headings\n", 0); got != nil {
		t.Fatalf("want nil for heading-only docs, got %+v", got)
	}
}

func TestSectionsHeadingsInsideFencesAreNotHeadings(t *testing.T) {
	doc := "# Real\n\n~~~\n# fake\n~~~\n\nafter\n\n```go\n// # also fake\n```\n"
	got := Split("", doc, 0)
	if len(got) != 1 || got[0].Heading != "Real" {
		t.Fatalf("sections = %+v", got)
	}
	if !strings.Contains(got[0].Body, "# fake") || !strings.Contains(got[0].Body, "# also fake") {
		t.Errorf("fence content lost: %q", got[0].Body)
	}
}

func TestSectionsCleanInlineHeading(t *testing.T) {
	got := Split("", "# The `Config` [type](https://x) **matters**\n\nbody", 0)
	if len(got) != 1 {
		t.Fatalf("sections = %+v", got)
	}
	if got[0].Heading != "The Config type matters" {
		t.Errorf("heading = %q", got[0].Heading)
	}
	if got[0].Anchor != "the-config-type-matters" {
		t.Errorf("anchor = %q", got[0].Anchor)
	}
}

func TestSectionsStripInlineHTMLFromHeading(t *testing.T) {
	got := Split("", `### <Badge type="info" text="optional" /> origin: string | string[]`+"\n\nbody", 0)
	if len(got) != 1 || got[0].Heading != "origin: string | string[]" || got[0].Anchor != "origin-string-string" {
		t.Fatalf("sections = %+v", got)
	}
}

func TestSectionsClosingHashesAndTrailingSpace(t *testing.T) {
	got := Split("", "## Title ##   \n\nbody", 0)
	if len(got) != 1 || got[0].Heading != "Title" {
		t.Fatalf("sections = %+v", got)
	}
}

func TestSectionsSplitLongBodyAtParagraphs(t *testing.T) {
	para := strings.Repeat("word ", 20) // 100 chars
	doc := "# H\n\n" + para + "\n\n" + para + "\n\n" + para + "\n"
	got := Split("", doc, 150)
	if len(got) != 3 {
		t.Fatalf("got %d parts, want 3: %+v", len(got), got)
	}
	for i, s := range got {
		if s.Part != i || s.Heading != "H" || s.Anchor != "h" {
			t.Errorf("part %d = %+v", i, s)
		}
		if len(s.Body) > 150 {
			t.Errorf("part %d is %d chars", i, len(s.Body))
		}
	}
}

// A fenced block containing blank lines must not be split at those blank
// lines even when it exceeds the cap; it is hard-split at line boundaries
// only when it alone exceeds maxChars.
func TestSectionsSplitKeepsFencesTogether(t *testing.T) {
	fence := "```\nline one\n\nline two\n```"
	doc := "# H\n\nshort\n\n" + fence + "\n\nshort again\n"
	got := Split("", doc, 40)
	joined := ""
	for _, s := range got {
		joined += s.Body + "\n"
	}
	if !strings.Contains(joined, fence) {
		t.Fatalf("fence was split across parts:\n%s", joined)
	}
}

func TestSectionsHardSplitOversizedBlock(t *testing.T) {
	block := strings.Repeat("a line of code\n", 20)
	got := Split("", "# H\n\n"+block, 60)
	if len(got) < 4 {
		t.Fatalf("expected several parts, got %d", len(got))
	}
	for _, s := range got {
		if len(s.Body) > 60 {
			t.Errorf("part exceeds cap: %d chars", len(s.Body))
		}
	}
}

func TestSectionsDeterministic(t *testing.T) {
	a := Split("Glamour", nestedDoc, 80)
	b := Split("Glamour", nestedDoc, 80)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("Sections is not deterministic")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Container queries":        "container-queries",
		"  Hello,  World!  ":       "hello-world",
		"C++ & Go":                 "c-go",
		"snake_case_name":          "snake_case_name",
		"Ünïcödé Héading":          "ünïcödé-héading",
		"--already--hyphenated--":  "already-hyphenated",
		"v4.0 release (2026)":      "v40-release-2026",
		"What's new?":              "whats-new",
		"@container / @media":      "container-media",
		"Tabs\tand\nnewlines here": "tabs-and-newlines-here",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSectionsCRLF(t *testing.T) {
	got := Split("", "# A\r\n\r\nbody\r\n\r\n## B\r\n\r\nmore\r\n", 0)
	if len(got) != 2 || got[1].Body != "more" {
		t.Fatalf("sections = %+v", got)
	}
}
