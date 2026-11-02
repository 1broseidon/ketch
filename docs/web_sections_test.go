package docs

import (
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestWebSplitSectionsHeadingsOutsideFences(t *testing.T) {
	page := webPage{URL: "https://x.test/guide", Title: "Guide | Site", Markdown: `# Guide

Intro paragraph for the guide page.

## Install

` + fence + `bash
# this is a shell comment, not a heading
npm install guide
` + fence + `

~~~~
## also not a heading
` + fence + `
### nor this, inside a longer tilde fence
` + fence + `
~~~~

### From source {#build-from-source}

Clone the repository and run make.

#### Requirements

Go 1.26 and a C-free toolchain are enough.

## Install

The second Install heading gets a deduplicated slug.

## Empty

## Closing hashes ##

Closing sequences are not part of the heading text.
`}
	sections := splitSections(page, 3)
	type got struct{ heading, crumbs, anchor string }
	var have []got
	for _, s := range sections {
		have = append(have, got{s.heading, strings.Join(s.crumbs, " > "), s.anchor})
		if s.page != 3 {
			t.Errorf("section %q lost its page index", s.heading)
		}
	}
	want := []got{
		{"Guide", "Guide", ""},
		{"Install", "Guide > Install", "install"},
		{"From source", "Guide > Install > From source", "build-from-source"},
		{"Install", "Guide > Install", "install-1"},
		{"Closing hashes", "Guide > Closing hashes", "closing-hashes"},
	}
	if !slices.Equal(have, want) {
		t.Fatalf("sections =\n%v\nwant\n%v", have, want)
	}
	install := sections[1].text
	for _, inside := range []string{"# this is a shell comment", "## also not a heading", "### nor this"} {
		if !strings.Contains(install, inside) {
			t.Errorf("fenced line %q left its section:\n%s", inside, install)
		}
	}
	if !strings.Contains(sections[2].text, "#### Requirements") {
		t.Errorf("H4 should stay inline in its H3 section:\n%s", sections[2].text)
	}
	if sections[0].URL() != "https://x.test/guide" || sections[2].URL() != "https://x.test/guide#build-from-source" {
		t.Errorf("section URLs = %q, %q", sections[0].URL(), sections[2].URL())
	}
}

func TestWebParseATX(t *testing.T) {
	for _, tc := range []struct {
		line  string
		ok    bool
		level int
		text  string
		id    string
	}{
		{"## Hello world", true, 2, "Hello world", ""},
		{"   ### Indented three", true, 3, "Indented three", ""},
		{"    # four spaces is code", false, 0, "", ""},
		{"#hashtag", false, 0, "", ""},
		{"####### seven", false, 0, "", ""},
		{"## C#", true, 2, "C#", ""},
		{"## Title ##", true, 2, "Title", ""},
		{"## Custom {#my-id}", true, 2, "Custom", "my-id"},
		{"## With class {#my-id .note}", true, 2, "With class", "my-id"},
		{"## `useState` and [hooks](/hooks)", true, 2, "useState and hooks", ""},
		{"## Wrapped [Anchor](#wrapped-anchor)", true, 2, "Wrapped Anchor", "wrapped-anchor"},
		{"## Permalinked [#](#perma)", true, 2, "Permalinked", "perma"},
		// The scrape pipeline makes a permalink's href="#x" absolute.
		{"## Defining a collection[\u200b](https://docu.test/docs/guide#schema-options \"Direct link to Defining a collection\")", true, 2, "Defining a collection", "schema-options"},
		{"## Options[¶](https://docs.test/en/config.html#opts \"Permanent link\")", true, 2, "Options", "opts"},
		// A worded link to another page's section is not this heading's anchor.
		{"## See [Configuration](https://docs.test/config#options)", true, 2, "See Configuration", ""},
		{"## Read [the guide](guide.html#setup) first", true, 2, "Read the guide first", ""},
	} {
		h, ok := parseATX(tc.line)
		if ok != tc.ok || (ok && (h.level != tc.level || h.text != tc.text || h.id != tc.id)) {
			t.Errorf("parseATX(%q) = %+v, %v; want level %d text %q id %q ok %v", tc.line, h, ok, tc.level, tc.text, tc.id, tc.ok)
		}
	}
}

func TestWebSlugsAreGitHubStyle(t *testing.T) {
	s := slugger{}
	for _, tc := range []struct{ in, want string }{
		{"Hello, World!", "hello-world"},
		{"API: get()", "api-get"},
		{"Über café", "über-café"},
		{"snake_case and kebab-case", "snake_case-and-kebab-case"},
		{"Hello World", "hello-world-1"},
		{"Hello World", "hello-world-2"},
		{"hello-world-1", "hello-world-1-1"},
	} {
		if got := s.slug(tc.in); got != tc.want {
			t.Errorf("slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWebStripFrontmatter(t *testing.T) {
	title, body := stripFrontmatter("---\ntitle: \"Getting started\"\nsidebar: 2\n---\n# Heading\n")
	if title != "Getting started" || body != "# Heading\n" {
		t.Errorf("stripFrontmatter = %q, %q", title, body)
	}
	if title, body := stripFrontmatter("# No frontmatter\n---\n"); title != "" || body != "# No frontmatter\n---\n" {
		t.Errorf("no frontmatter changed the body: %q, %q", title, body)
	}
}

func TestWebParseLLMSTxt(t *testing.T) {
	root, _ := parseWebRoot("https://site.test/docs/")
	base, _ := url.Parse("https://site.test/llms.txt")
	got := parseLLMSTxt(`# Site

> One line summary
> continues here.

Some prose with [an inline link](/docs/prose.md).

## API reference

- [Client](/docs/api/client.md): The client object - and its options
- [Server](https://site.test/docs/api/server.md)
- ![badge](/docs/badge.md)
- [Blog](/blog/post.md): out of prefix
- [Full](/docs/llms-full.txt)
- [Client again](/docs/api/client.md#options)
`, base, root)
	if got.title != "Site" || got.description != "One line summary continues here." {
		t.Errorf("title/description = %q / %q", got.title, got.description)
	}
	var have []string
	for _, l := range got.links {
		have = append(have, l.url.String()+" | "+l.title+" | "+l.description)
	}
	want := []string{
		"https://site.test/docs/prose.md | an inline link | ",
		"https://site.test/docs/api/client.md | Client | The client object - and its options (API reference)",
		"https://site.test/docs/api/server.md | Server | (API reference)",
	}
	if !slices.Equal(have, want) {
		t.Errorf("links =\n%s\nwant\n%s", strings.Join(have, "\n"), strings.Join(want, "\n"))
	}
}

func TestWebDisplayURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://x.test/guide/start.md", "https://x.test/guide/start"},
		{"https://x.test/guide/index.md", "https://x.test/guide/"},
		{"https://x.test/page.html.md", "https://x.test/page.html"},
		{"https://x.test/api.mdx", "https://x.test/api"},
		{"https://x.test/_llms-txt/api.txt", "https://x.test/_llms-txt/api.txt"},
		{"https://x.test/guide/", "https://x.test/guide/"},
	} {
		u, _ := url.Parse(tc.in)
		if got := displayURL(u); got != tc.want {
			t.Errorf("displayURL(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestWebRankingIsDeterministic(t *testing.T) {
	sections := []webSection{
		{page: 0, order: 0, heading: "Alpha", crumbs: []string{"Alpha"}, text: "routing is covered here"},
		{page: 1, order: 0, heading: "Beta", crumbs: []string{"Beta"}, text: "routing is covered here"},
		{page: 1, order: 1, heading: "Routing", crumbs: []string{"Beta", "Routing"}, text: "routing is covered here"},
		{page: 2, order: 0, heading: "Gamma", crumbs: []string{"Gamma"}, text: "nothing relevant at all"},
	}
	for range 5 {
		ranked := rankSections(sections, "routing")
		var order []string
		for _, s := range ranked {
			order = append(order, s.heading)
		}
		if !slices.Equal(order, []string{"Routing", "Alpha", "Beta"}) {
			t.Fatalf("rank order = %v: heading match first, ties in page order, non-matches dropped", order)
		}
	}
}

func TestWebPickCandidates(t *testing.T) {
	root, _ := parseWebRoot("https://x.test/docs/")
	link := func(p, title, desc string) webLink {
		u, _ := url.Parse("https://x.test" + p)
		return webLink{url: u, title: title, description: desc}
	}
	links := []webLink{
		link("/docs/intro", "Introduction", ""),
		link("/docs/guides/configuration", "", ""),
		link("/docs/reference/cli", "CLI reference", "Every config flag"),
		link("/docs/guides/config-files", "Config files", "Where the config lives"),
	}
	var got []string
	for _, l := range pickCandidates(links, "config files", root, 2) {
		got = append(got, l.url.Path)
	}
	if !slices.Equal(got, []string{"/docs/guides/config-files", "/docs/guides/configuration"}) {
		t.Errorf("picked %v", got)
	}
	if fallback := pickCandidates(links, "zebra", root, 6); len(fallback) != webFallbackFetch || fallback[0].url.Path != "/docs/intro" {
		t.Errorf("no match should fall back to the first %d links, got %d", webFallbackFetch, len(fallback))
	}
}

// Heading text is what the page renders, and its anchor is github-slugger's
// slug of that text. A code span is text even when it looks like HTML or a
// generic ("<Image />", "<T>"), so it is read before tags are stripped; a
// tag outside code renders as nothing. Combining marks are part of a word
// (Devanagari vowel signs, a decomposed accent), so the slug keeps them.
// Expected anchors are github-slugger 2.0's output for the same text.
func TestWebHeadingCodeSpansAndCombiningMarks(t *testing.T) {
	for _, tc := range []struct{ line, text, anchor string }{
		{"## Using `<Image />` component", "Using <Image /> component", "using-image--component"},
		{"## `useState<T>()`", "useState<T>()", "usestatet"},
		{"## `[link](x)` stays code", "[link](x) stays code", "linkx-stays-code"},
		{"## ``a`b``", "a`b", "ab"},
		{"## Using <Image /> outside code", "Using outside code", "using-outside-code"},
		{"## [`Image`](/api/image) props", "Image props", "image-props"},
		{"## हिन्दी", "हिन्दी", "हिन्दी"},
		{"## Café au lait", "Café au lait", "café-au-lait"},
	} {
		h, ok := parseATX(tc.line)
		if !ok || h.text != tc.text {
			t.Errorf("parseATX(%q) text = %q, %v; want %q", tc.line, h.text, ok, tc.text)
			continue
		}
		if got := claimAnchor(slugger{}, h); got != tc.anchor {
			t.Errorf("anchor for %q = %q, want %q", tc.line, got, tc.anchor)
		}
	}
}
