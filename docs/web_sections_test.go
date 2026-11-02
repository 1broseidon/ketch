package docs

import (
	"net/url"
	"os"
	"path/filepath"
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

// react.dev's Markdown pages (the .md links its llms.txt lists) carry each
// heading's id as a trailing MDX comment, {/*id*/}. It is the anchor, not
// words to slug: slugging it as text doubled every anchor
// (#my-cleanup-…-unmount-my-cleanup-…-unmount) and left the comment in the
// heading and breadcrumb. The fixture is the recorded page, trimmed.
func TestWebSplitSectionsMDXCommentIDs(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "sections", "react.dev", "useEffect.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, markdown := stripFrontmatter(string(body))
	sections := splitSections(webPage{URL: "https://react.dev/reference/react/useEffect", Title: "useEffect", Markdown: markdown}, 0)
	type got struct{ heading, crumbs, url string }
	var have []got
	for _, s := range sections {
		have = append(have, got{s.heading, strings.Join(s.crumbs, " > "), s.URL()})
	}
	want := []got{
		{"useEffect", "useEffect", "https://react.dev/reference/react/useEffect"},
		{"useEffect(setup, dependencies?)", "useEffect > Reference > useEffect(setup, dependencies?)", "https://react.dev/reference/react/useEffect#useeffect"},
		{"My Effect runs twice when the component mounts", "useEffect > Troubleshooting > My Effect runs twice when the component mounts",
			"https://react.dev/reference/react/useEffect#my-effect-runs-twice-when-the-component-mounts"},
		{"My cleanup logic runs even though my component didn't unmount", "useEffect > Troubleshooting > My cleanup logic runs even though my component didn't unmount",
			"https://react.dev/reference/react/useEffect#my-cleanup-logic-runs-even-though-my-component-didnt-unmount"},
	}
	if !slices.Equal(have, want) {
		t.Errorf("sections:\n got %q\nwant %q", have, want)
	}
	for _, tc := range []struct{ line, text, id string }{
		{"## Usage {/* usage */}", "Usage", "usage"},
		{"## Not an id {/* a comment with spaces */}", "Not an id {/* a comment with spaces */}", ""},
		{"## Pandoc {#pandoc .unnumbered}", "Pandoc", "pandoc"},
	} {
		h, ok := parseATX(tc.line)
		if !ok || h.text != tc.text || h.id != tc.id {
			t.Errorf("parseATX(%q) = %q #%q, want %q #%q", tc.line, h.text, h.id, tc.text, tc.id)
		}
	}
}

// fixtureSections splits a recorded page from testdata/sections: the
// scrape pipeline's Markdown for it, frontmatter and all, trimmed.
func fixtureSections(t *testing.T, file, pageURL string) []webSection {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "sections", file))
	if err != nil {
		t.Fatal(err)
	}
	title, markdown := stripFrontmatter(string(body))
	return splitSections(webPage{URL: pageURL, Title: title, Markdown: markdown}, 0)
}

// sectionNamed returns the section with the heading, failing t if none.
func sectionNamed(t *testing.T, sections []webSection, heading string) webSection {
	t.Helper()
	for _, s := range sections {
		if s.heading == heading {
			return s
		}
	}
	var have []string
	for _, s := range sections {
		have = append(have, s.heading)
	}
	t.Fatalf("no section %q among %q", heading, have)
	return webSection{}
}

// topAnchors returns the anchors of the n best sections for query.
func topAnchors(sections []webSection, query string, n int) []string {
	var out []string
	for _, s := range rankSections(sections, query) {
		if len(out) == n {
			break
		}
		out = append(out, s.anchor)
	}
	return out
}

// Django's reference puts each QuerySet method at H4. Split only at H1–H3,
// "Methods that return new QuerySets" swallowed every method (87,706
// characters on the live page) and its snippet showed only the intro. A
// code heading is an API entry and gets a section of its own, with the
// crumbs of every heading above it and the id Sphinx gives it
// (select-related, not github-slugger's select_related).
func TestWebSplitSectionsDjangoAPIEntries(t *testing.T) {
	const page = "https://docs.djangoproject.com/en/6.1/ref/models/querysets/"
	sections := fixtureSections(t, "docs.djangoproject.com/querysets.md", page)
	sr := sectionNamed(t, sections, "select_related()")
	if got := strings.Join(sr.crumbs, " > "); got != "QuerySet API reference > QuerySet API > Methods that return new QuerySets > select_related()" {
		t.Errorf("crumbs = %q", got)
	}
	if sr.URL() != page+"#select-related" {
		t.Errorf("URL = %s, want the page's own id #select-related", sr.URL())
	}
	if !strings.HasPrefix(sr.text, `select\_related(*\*fields*)`) || len(sr.text) > 6000 || !strings.Contains(sr.text, "Book.objects.select_related") {
		t.Errorf("select_related() should hold its own entry, signature first, %d bytes:\n%.300s", len(sr.text), sr.text)
	}
	if parent := sectionNamed(t, sections, "Methods that return new QuerySets"); len(parent.text) > 1000 || strings.Contains(parent.text, "select_related(") {
		t.Errorf("the methods' parent keeps only its intro, got %d bytes", len(parent.text))
	}
	if got := topAnchors(sections, "queryset select_related", 1); !slices.Equal(got, []string{"select-related"}) {
		t.Errorf("best section = %v, want select-related", got)
	}
}

// docs.rs puts each method at H4 inside one impl block, its Errors and
// Optional notes at H5. Methods split off with rustdoc's ids (method.json);
// their small notes stay with them; impl headings take the id the § link
// before them carries; the empty auto-trait sections that outranked the
// methods on "send a json body" are gone.
func TestWebSplitSectionsDocsRSMethods(t *testing.T) {
	const page = "https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html"
	sections := fixtureSections(t, "docs.rs/reqwest-RequestBuilder.md", page)
	jsonSec := sectionNamed(t, sections, "pub fn json<T: Serialize + ?Sized>(self, json: &T) -> RequestBuilder")
	if jsonSec.anchor != "method.json" || !strings.Contains(jsonSec.text, "##### Optional") || !strings.Contains(jsonSec.text, "##### Errors") {
		t.Errorf("json: #%s, notes attached: %v\n%s", jsonSec.anchor, strings.Contains(jsonSec.text, "Errors"), jsonSec.text)
	}
	if got := strings.Join(jsonSec.crumbs[:3], " > "); got != "Struct RequestBuilder > Implementations > impl RequestBuilder" {
		t.Errorf("crumbs = %q", got)
	}
	for _, s := range sections {
		if strings.HasPrefix(s.anchor, "impl") && strings.Contains(s.anchor, "lt") || strings.Contains(s.text, "[Source](") || strings.Contains(s.text, "[§](") {
			t.Errorf("section %q (#%s) keeps entity-slug anchors or source links:\n%.200s", s.heading, s.anchor, s.text)
		}
		if strings.HasPrefix(s.heading, "impl ") {
			t.Errorf("an impl heading with nothing under it should not be a section: %q (#%s)", s.heading, s.anchor)
		}
	}
	if got := topAnchors(sections, "send a json body", 1); !slices.Equal(got, []string{"method.json"}) {
		t.Errorf("best section = %v, want method.json", got)
	}
}

// pkg.go.dev's ids are its own: section-readme and readme-…, pkg-overview,
// hdr-Choosing_a_Logger for a doc heading, the bare name for a func or
// type and Type.Method for a method. Its Types block (every type and
// method at H4, 2,000 lines on zap's page) splits into entries.
func TestWebSplitSectionsPkgsite(t *testing.T) {
	const page = "https://pkg.go.dev/go.uber.org/zap"
	sections := fixtureSections(t, "pkg.go.dev/zap.md", page)
	anchors := map[string]string{}
	for _, s := range sections {
		anchors[s.anchor] = strings.Join(s.crumbs, " > ")
		if strings.Contains(s.text, "[¶](") {
			t.Errorf("section %q keeps an example permalink", s.heading)
		}
	}
	for anchor, crumbs := range map[string]string{
		"readme-quick-start":             "zap > README > ⚡ zap > Quick Start",
		"pkg-overview":                   "zap > Documentation > Overview",
		"hdr-Choosing_a_Logger":          "zap > Documentation > Overview > Choosing a Logger",
		"CombineWriteSyncers":            "zap > Documentation > Functions > func CombineWriteSyncers",
		"SugaredLogger":                  "zap > Documentation > Types > type SugaredLogger",
		"SugaredLogger.Debug":            "zap > Documentation > Types > func (*SugaredLogger) Debug",
		"Logger.Sugar":                   "zap > Documentation > Types > func (*Logger) Sugar",
		"NewStdLogAt":                    "zap > Documentation > Functions > func NewStdLogAt added in v1.7.0",
		"section-sourcefiles":            "zap > Source Files",
		"section-documentation":          "",
		"hdr-Frequently_Asked_Questions": "zap > Documentation > Overview > Frequently Asked Questions",
	} {
		got, ok := anchors[anchor]
		if !ok || crumbs != "" && got != crumbs {
			t.Errorf("#%s: crumbs %q (present %v), want %q", anchor, got, ok, crumbs)
		}
	}
	top := topAnchors(sections, "sugared logger", 3)
	if !slices.Contains(top, "hdr-Choosing_a_Logger") || !slices.Contains(top, "readme-quick-start") {
		t.Errorf("top 3 for sugared logger = %v", top)
	}
}

// serde.rs lists each field attribute as an H5 that opens a list item,
// "- ##### `#[serde(rename = "name")]`". Each is an entry of its own, so
// the one asked about is found instead of the whole page.
func TestWebSplitSectionsListItemHeadings(t *testing.T) {
	sections := fixtureSections(t, "serde.rs/field-attrs.md", "https://serde.rs/field-attrs.html")
	if len(sections) < 15 {
		t.Fatalf("got %d sections, want one per attribute", len(sections))
	}
	skip := sectionNamed(t, sections, `#[serde(skip_serializing_if = "path")]`)
	if !strings.Contains(skip.text, "Option::is_none") || strings.Contains(skip.text, "serialize_with") {
		t.Errorf("skip_serializing_if section:\n%s", skip.text)
	}
	ranked := rankSections(sections, "skip serializing a field if it is None")
	if len(ranked) == 0 || ranked[0].heading != skip.heading {
		t.Errorf("best section = %q", ranked[0].heading)
	}
	if ranked := rankSections(sections, "serde rename field"); ranked[0].heading != `#[serde(rename = "name")]` {
		t.Errorf("best section for rename = %q", ranked[0].heading)
	}
}

// Splitting deeper must not scatter one API entry: react.dev's
// useEffect(setup, dependencies?) keeps its Parameters, Returns and
// Caveats, while the long Usage section beside it splits into its H4
// examples, each with the crumbs above it.
func TestWebSplitSectionsKeepsEntryParts(t *testing.T) {
	sections := fixtureSections(t, "react.dev/useEffect-reference.md", "https://react.dev/reference/react/useEffect")
	entry := sectionNamed(t, sections, "useEffect(setup, dependencies?)")
	for _, part := range []string{"#### Parameters", "#### Returns", "#### Caveats"} {
		if !strings.Contains(entry.text, part) {
			t.Errorf("%s left the entry", part)
		}
	}
	for _, s := range sections {
		if s.heading == "Parameters" || s.heading == "Returns" || s.heading == "Caveats" {
			t.Errorf("%s became a section of its own", s.heading)
		}
	}
	chat := sectionNamed(t, sections, "Connecting to a chat server")
	if got := strings.Join(chat.crumbs, " > "); got != "useEffect > Usage > Connecting to an external system > Connecting to a chat server" || chat.anchor != "connecting-to-a-chat-server" {
		t.Errorf("chat example: %q #%s", got, chat.anchor)
	}
	if usage := sectionNamed(t, sections, "Connecting to an external system"); strings.Contains(usage.text, "Connecting to a chat server") {
		t.Error("the large section kept the example that split off")
	}
}

// VitePress slugs punctuation to hyphens (build.lib → #build-lib, where
// github-slugger's #buildlib is dead). Its headings are recognised by the
// space and zero-width space the unwrapped header anchor leaves; a
// Docusaurus heading leaves the zero-width space alone and keeps
// github-slugger's rule.
func TestWebAnchorsVitePress(t *testing.T) {
	sections := fixtureSections(t, "vite.dev/build-options.md", "https://vite.dev/config/build-options")
	for _, heading := range []string{"build.lib", "build.target", "build.modulePreload"} {
		want := strings.ToLower(strings.ReplaceAll(heading, ".", "-"))
		if s := sectionNamed(t, sections, heading); s.anchor != want {
			t.Errorf("%s: #%s, want #%s", heading, s.anchor, want)
		}
	}
	docusaurus := splitSections(webPage{URL: "https://jestjs.io/docs/jest-object", Title: "The Jest Object", Markdown: "## `jest.fn(implementation?)`\u200b\n\nReturns a new, unused mock function.\n"}, 0)
	if len(docusaurus) != 1 || docusaurus[0].anchor != "jestfnimplementation" {
		t.Errorf("Docusaurus heading anchor = %+v", docusaurus)
	}
}

// Generator id rules, each checked against ids read off the live pages.
func TestWebGeneratorIDs(t *testing.T) {
	for _, tc := range []struct {
		fn       func(string) string
		in, want string
	}{
		{docutilsID, "select_related()", "select-related"},
		{docutilsID, "Methods that return new QuerySets", "methods-that-return-new-querysets"},
		{docutilsID, "1. Getting started", "getting-started"},
		{vitepressSlug, "build.rollupOptions", "build-rollupoptions"},
		{vitepressSlug, "Env Variables and Modes", "env-variables-and-modes"},
		{vitepressSlug, "2.0 changes", "_2-0-changes"},
		{rustdocImplID, "impl<T> From<T> for T", "impl-From%3CT%3E-for-T"},
		{rustdocImplID, "impl<T> Subcommand for Box<T> where T: Subcommand", "impl-Subcommand-for-Box%3CT%3E"},
		{rustdocImplID, "impl !RefUnwindSafe for RequestBuilder", "impl-RefUnwindSafe-for-RequestBuilder"},
		{rustdocImplID, "impl Subcommand for ()", "impl-Subcommand-for-()"},
		{rustdocImplID, "impl Body", "impl-Body"},
		{readmeSlug, "⚡ zap", "zap"},
		{readmeSlug, "Development Status: Stable", "development-status-stable"},
		{goHeadingID, "Choosing a Logger", "hdr-Choosing_a_Logger"},
	} {
		if got := tc.fn(tc.in); got != tc.want {
			t.Errorf("%q → %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct {
		line      string
		ancestors []string
		want      string
	}{
		{"## Trait Implementations", nil, "trait-implementations"},
		{"## Auto Trait Implementations", nil, "synthetic-implementations"},
		{"#### pub fn get\\_mut&lt;I: Index&gt;(&mut self, index: I) -&gt; Option&lt;&mut Value&gt;", []string{"Implementations", "impl Value"}, "method.get_mut"},
		{"#### fn augment\\_subcommands(cmd: Command) -&gt; Command", []string{"Required Methods"}, "tymethod.augment_subcommands"},
		{"#### pub const fn new() -&gt; Self", []string{"Implementations", "impl Body"}, "method.new"},
		{"#### type Error = Infallible", []string{"Trait Implementations", "impl TryFrom<U> for T"}, "associatedtype.Error"},
		{"### Bool([bool](https://doc.rust-lang.org/std/primitive.bool.html))", []string{"Variants"}, "variant.Bool"},
		{"## Examples", nil, ""},
	} {
		h, _ := parseATX(tc.line)
		if got := rustdocID(h, tc.ancestors); got != tc.want {
			t.Errorf("rustdocID(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

// Permalink and source-link decorations are not content: Click's code
// captions ("hello.py[¶](…)"), Django's definitions and [source] links,
// VitePress line annotations (a line marked removed goes with its mark).
// Real links stay.
func TestWebSectionDecorationsStripped(t *testing.T) {
	click := fixtureSections(t, "click.palletsprojects.com/testing.md", "https://click.palletsprojects.com/en/stable/testing/")
	basic := sectionNamed(t, click, "Basic Example")
	if strings.Contains(basic.text, "¶") || !strings.Contains(basic.text, "hello.py") || !strings.Contains(basic.text, "[`CliRunner`](https://click.palletsprojects.com/en/stable/api/#click.testing.CliRunner") {
		t.Errorf("Basic Example:\n%s", basic.text)
	}
	django := fixtureSections(t, "docs.djangoproject.com/querysets.md", "https://docs.djangoproject.com/en/6.1/ref/models/querysets/")
	for _, s := range django {
		if strings.Contains(s.text, "[\\[source\\]]") || strings.Contains(s.text, "Link to this definition") {
			t.Errorf("%s keeps a decoration:\n%.400s", s.heading, s.text)
		}
	}
	vitest := fixtureSections(t, "vitest.dev/migration.md", "https://vitest.dev/guide/migration")
	clear := sectionNamed(t, vitest, "clearMocks is Enabled by Default")
	if strings.Contains(clear.text, "[!code") || strings.Contains(clear.text, "toHaveBeenCalledTimes(2)") || !strings.Contains(clear.text, "toHaveBeenCalledTimes(1)") || !strings.Contains(clear.text, "clearMocks: false,") {
		t.Errorf("clearMocks section:\n%s", clear.text)
	}
}

// v2.vuejs.org (Hexo) keeps the heading's case in its ids:
// #Customizing-Component-v-model, where github-slugger's lower-case slug
// is dead. Nothing in a page's Markdown says Hexo, but the pages' own
// links do: Props links to its #Prop-Validation. A heading named by a
// link takes the linked id, and the rule those ids follow re-slugs the
// site's other guessed anchors. Without the evidence, the guess stands.
func TestWebAnchorsFromLinkEvidence(t *testing.T) {
	events := fixtureSections(t, "v2.vuejs.org/components-custom-events.md", "https://v2.vuejs.org/v2/guide/components-custom-events.html")
	props := fixtureSections(t, "v2.vuejs.org/components-props.md", "https://v2.vuejs.org/v2/guide/components-props.html")
	for i := range props {
		props[i].page = 1
	}
	anchors := map[string]string{}
	for _, s := range rankSections(append(slices.Clone(events), props...), "v-model on custom components prop validation") {
		anchors[s.heading] = s.anchor
	}
	for heading, want := range map[string]string{
		"Customizing Component v-model":       "Customizing-Component-v-model",
		"Prop Validation":                     "Prop-Validation",
		"Binding Native Events to Components": "Binding-Native-Events-to-Components",
	} {
		if anchors[heading] != want {
			t.Errorf("%s: #%s, want #%s", heading, anchors[heading], want)
		}
	}
	for _, s := range rankSections(events, "v-model on custom components") {
		if s.heading == "Customizing Component v-model" && s.anchor != "customizing-component-v-model" {
			t.Errorf("with no link evidence the github-slugger guess stands, got #%s", s.anchor)
		}
	}
	// An id the generator fixes is never second-guessed.
	fixed := webSection{pageURL: "https://docs.rs/x/latest/x/struct.A.html", heading: "pub fn json()", anchor: "method.json", source: anchorFixed, text: "See [json](https://docs.rs/x/latest/x/struct.A.html#json) and json."}
	if got := adoptLinkedAnchors([]webSection{fixed}); got[0].anchor != "method.json" {
		t.Errorf("fixed anchor changed to %s", got[0].anchor)
	}
}

// vite.dev's llms.txt serves each page's VitePress source, which keeps no
// trace of the generator. Its links to its own headings do: #build-target
// for build.target and #build-minify for build.minify, two ids only
// VitePress's rule makes, so the page's other guessed anchors follow it
// (#build-lib, not github-slugger's #buildlib). One such id alone could be
// hand-written, and moves only its own heading.
func TestWebAnchorsVitePressSourceFromLinks(t *testing.T) {
	const page = "https://vite.dev/config/build-options"
	anchorsFor := func(sections []webSection) map[string]string {
		out := map[string]string{}
		for _, s := range rankSections(sections, "build") {
			out[s.heading] = s.anchor
		}
		return out
	}
	got := anchorsFor(fixtureSections(t, "vite.dev/build-options.src.md", page))
	for heading, want := range map[string]string{"build.target": "build-target", "build.minify": "build-minify", "build.lib": "build-lib", "build.assetsDir": "build-assetsdir"} {
		if got[heading] != want {
			t.Errorf("%s: #%s, want #%s", heading, got[heading], want)
		}
	}
	body, err := os.ReadFile(filepath.Join("testdata", "sections", "vite.dev", "build-options.src.md"))
	if err != nil {
		t.Fatal(err)
	}
	one := strings.ReplaceAll(string(body), "(#build-minify)", "(https://vite.dev/config/other#elsewhere)")
	got = anchorsFor(splitSections(webPage{URL: page, Title: "Build Options", Markdown: one}, 0))
	if got["build.target"] != "build-target" || got["build.lib"] != "buildlib" {
		t.Errorf("one punctuation id: build.target #%s (want build-target), build.lib #%s (want the guess buildlib)", got["build.target"], got["build.lib"])
	}
}
