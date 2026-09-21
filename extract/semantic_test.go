package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/1broseidon/ketch/config"
)

// filler returns n sentences of fourteen words each.
func filler(n int) string {
	return strings.Repeat("The reference explains how the system behaves and why it was built this way. ", n)
}

// landmarkPage wraps extra in a <main> after a title and 210 words of prose,
// enough for every block rule to run.
func landmarkPage(extra string) string {
	return `<html><head><title>Guide</title></head><body><nav><a href="/">Home</a> <a href="/docs">Docs</a></nav>` +
		`<main><h1>Guide</h1><p>` + filler(15) + `</p>` + extra + `</main><footer>SITEFOOTER</footer></body></html>`
}

func extractBoth(t *testing.T, page string) (complete, clean string) {
	t.Helper()
	c, err := New().Extract("https://example.test/guide", page)
	if err != nil {
		t.Fatal(err)
	}
	k, err := NewWithMode(ModeClean).Extract("https://example.test/guide", page)
	if err != nil {
		t.Fatal(err)
	}
	return c.Markdown, k.Markdown
}

func TestParseMode(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]Mode{"": ModeComplete, "complete": ModeComplete, " Clean ": ModeClean, "clean": ModeClean} {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Fatalf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseMode("fast"); err == nil || !strings.Contains(err.Error(), `"complete" or "clean"`) {
		t.Fatalf("ParseMode(fast) err = %v", err)
	}
}

// The config package repeats the mode names because it cannot import this
// package; the two lists must agree.
func TestModesMatchConfig(t *testing.T) {
	t.Parallel()
	if got := strings.Join(config.ExtractModes(), ","); got != strings.Join(Modes, ",") {
		t.Fatalf("config.ExtractModes = %s, extract.Modes = %s", got, strings.Join(Modes, ","))
	}
	for _, m := range config.ExtractModes() {
		if _, err := ParseMode(m); err != nil {
			t.Fatalf("ParseMode(%q): %v", m, err)
		}
	}
}

func TestExtractorMode(t *testing.T) {
	t.Parallel()
	if New().Mode() != ModeComplete || NewWithMode("").Mode() != ModeComplete || NewWithMode(ModeClean).Mode() != ModeClean {
		t.Fatal("constructors do not report their mode")
	}
}

func TestSemanticRootTiers(t *testing.T) {
	t.Parallel()
	section := func(n string) string {
		return `<div class="sect1"><h2>Chapter ` + n + `</h2><p>` + filler(4) + `</p></div>`
	}
	cases := []struct{ name, html, kind, has, hasNot string }{
		{"landmark", landmarkPage(""), "landmark", "Guide", "SITEFOOTER"},
		{"uniform sections",
			`<html><head><title>Manual</title></head><body><div class="nav-bar"><a href="/">Home</a> <a href="/docs">Docs</a></div><div class="book">` + section("1") + section("2") + section("3") + `</div></body></html>`,
			"sections", "Chapter 3", "Home"},
		{"prose descent",
			`<html><head><title>Story</title></head><body><div class="top"><a href="/">Home</a> <a href="/about">About</a></div><div class="wrap"><div class="column"><h1>Story</h1><p>` + filler(8) + `</p><p>` + filler(2) + `</p></div><div class="side"><a href="/a">Link one</a> <a href="/b">Link two</a> <a href="/c">Link three</a></div></div></body></html>`,
			"prose", "Story", "Link one"},
		{"too little prose declines", `<html><body><div><p>` + filler(2) + `</p></div></body></html>`, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kind, outer := DebugSemanticRoot(tc.html, ModeComplete)
			if kind != tc.kind {
				t.Fatalf("kind = %q, want %q", kind, tc.kind)
			}
			if tc.has != "" && !strings.Contains(outer, tc.has) {
				t.Fatalf("root lacks %q:\n%s", tc.has, outer)
			}
			if tc.hasNot != "" && strings.Contains(outer, tc.hasNot) {
				t.Fatalf("root carries %q:\n%s", tc.hasNot, outer)
			}
		})
	}
}

// A page the semantic path declines still comes back through readability
// or the raw conversion.
func TestExtractFallsBackWhenSemanticDeclines(t *testing.T) {
	t.Parallel()
	result, err := New().Extract("https://example.test/short", `<html><head><title>Short</title></head><body><div><p>`+filler(2)+`</p></div></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, result.Markdown, "The reference explains how the system behaves")
}

func TestExtractCleansTitle(t *testing.T) {
	t.Parallel()
	result, err := New().Extract("https://example.test/guide", `<html><head><title>Guide | Example Site</title></head><body><main><h1>Guide</h1><p>`+filler(4)+`</p></main></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "Guide" {
		t.Fatalf("Title = %q, want Guide", result.Title)
	}
}

func TestNamedChromeDropsOnlyInClean(t *testing.T) {
	t.Parallel()
	complete, clean := extractBoth(t, landmarkPage(`<div class="related-posts"><p>RELATEDTEASER one two three four five six seven eight nine</p></div>`))
	assertContainsAll(t, complete, "RELATEDTEASER")
	assertContainsNone(t, clean, "RELATEDTEASER")
	assertContainsNone(t, complete, "SITEFOOTER", "Docs")
}

func TestRecircHeadingDropsOnlyInClean(t *testing.T) {
	t.Parallel()
	complete, clean := extractBoth(t, landmarkPage(`<h2>Related articles</h2><ul><li><a href="/a">Alpha story</a></li><li><a href="/b">Beta story</a></li><li><a href="/c">Gamma story</a></li></ul>`))
	assertContainsAll(t, complete, "Related articles", "Alpha story")
	assertContainsNone(t, clean, "Related articles", "Alpha story")
}

func TestCommentsDropOnlyInClean(t *testing.T) {
	t.Parallel()
	complete, clean := extractBoth(t, landmarkPage(`<section id="comments"><h2>Comments</h2><div class="comment"><p>READERCOMMENT one says the guide helped with the setup</p></div><div class="comment"><p>READERCOMMENT two disagrees about the second step</p></div></section>`))
	assertContainsAll(t, complete, "READERCOMMENT one", "READERCOMMENT two")
	assertContainsNone(t, clean, "READERCOMMENT")
}

func TestHiddenRules(t *testing.T) {
	t.Parallel()
	complete, clean := extractBoth(t, landmarkPage(`<div hidden>HIDDENBLOCK words</div>`+
		`<div hidden="until-found"><p>FOLDEDPROSE stays for the reader who opens it</p></div>`+
		`<span class="sr-only">SRLABEL</span>`+
		`<p class="sr-only">SRDESC describes the chart: sales rose in every quarter of the year and fell only in the last week of December</p>`+
		`<div style="display:none">STYLEHIDDEN</div>`+
		`<div aria-hidden="true">ARIAHIDDEN</div>`+
		`<div class="d-none">SHORTHIDDEN</div>`+
		`<div class="d-none"><p>CLASSHIDDENPROSE is a foldout of prose someone collapsed for later reading</p></div>`))
	for _, md := range []string{complete, clean} {
		assertContainsAll(t, md, "FOLDEDPROSE", "SRDESC", "CLASSHIDDENPROSE")
		assertContainsNone(t, md, "HIDDENBLOCK", "SRLABEL", "STYLEHIDDEN", "ARIAHIDDEN", "SHORTHIDDEN")
	}
}

func TestDisclosurePanelStays(t *testing.T) {
	t.Parallel()
	complete, _ := extractBoth(t, landmarkPage(`<button aria-expanded="false" aria-controls="panel1">Show details</button><div id="panel1" hidden><p>PANELPROSE is what the author folded away for later</p></div><button>COPYBUTTON</button>`))
	assertContainsAll(t, complete, "PANELPROSE")
	assertContainsNone(t, complete, "COPYBUTTON")
}

func TestTOCDropped(t *testing.T) {
	t.Parallel()
	page := `<html><head><title>Guide</title></head><body><main><h1>Guide</h1>` +
		`<ul><li><a href="#install">Install</a></li><li><a href="#configure">Configure</a></li><li><a href="#run">Run</a></li></ul>` +
		`<h2 id="install">Install</h2><p>` + filler(5) + `</p><h2 id="configure">Configure</h2><p>` + filler(5) + `</p><h2 id="run">Run</h2><p>` + filler(5) + `</p></main></body></html>`
	complete, _ := extractBoth(t, page)
	assertContainsAll(t, complete, "## Install", "## Configure", "## Run")
	assertContainsNone(t, complete, "](#install)", "](#configure)")
}

func TestLinkBlocksDroppedButHubKept(t *testing.T) {
	t.Parallel()
	t.Run("rail after an article", func(t *testing.T) {
		t.Parallel()
		complete, _ := extractBoth(t, landmarkPage(`<div class="tail"><div class="byline">Filed under</div><ul><li><a href="/t/one">Topic one</a></li><li><a href="/t/two">Topic two</a></li><li><a href="/t/three">Topic three</a></li><li><a href="/t/four">Topic four</a></li></ul></div>`))
		assertContainsNone(t, complete, "Topic one", "Topic four")
	})
	t.Run("hub page", func(t *testing.T) {
		t.Parallel()
		var groups strings.Builder
		for g := 1; g <= 4; g++ {
			groups.WriteString(`<div class="group"><div class="label">Group label</div><ul>`)
			for i := 1; i <= 10; i++ {
				groups.WriteString(`<li><a href="/g/` + string(rune('0'+g)) + `/` + string(rune('a'+i)) + `">Entry ` + string(rune('0'+g)) + string(rune('a'+i)) + ` words here now</a></li>`)
			}
			groups.WriteString(`</ul></div>`)
		}
		page := `<html><head><title>Index</title></head><body><main><h1>Index</h1><p>` + strings.Repeat("This index lists every entry. ", 6) + `</p>` + groups.String() + `</main></body></html>`
		complete, _ := extractBoth(t, page)
		assertContainsAll(t, complete, "Entry 1b words", "Entry 4k words")
	})
}

func TestLayoutTableUnwrapped(t *testing.T) {
	t.Parallel()
	complete, _ := extractBoth(t, `<html><head><title>Layout</title></head><body><main><table cellpadding="4"><tr><td><h1>Layout</h1><p>`+filler(10)+`</p></td><td><a href="/x">Side link</a></td></tr></table>`+
		`<table><tr><th>Plan</th><th>Price</th></tr><tr><td>Pro</td><td>$10</td></tr></table></main></body></html>`)
	assertContainsAll(t, complete, "# Layout", "|Plan|Price|", "|Pro|$10|")
	for _, line := range strings.Split(complete, "\n") {
		if strings.HasPrefix(line, "|") && strings.Contains(line, "Layout") {
			t.Fatalf("layout table came out as a pipe table:\n%s", complete)
		}
	}
}

func TestOrphanHeadingsDropped(t *testing.T) {
	t.Parallel()
	complete, _ := extractBoth(t, landmarkPage(`<h2>Notes</h2><p>NOTESTEXT</p><h2>Related articles</h2><nav><a href="/a">Alpha</a></nav>`))
	assertContainsAll(t, complete, "## Notes", "NOTESTEXT")
	assertContainsNone(t, complete, "Related articles")
}

func TestMathBecomesTeX(t *testing.T) {
	t.Parallel()
	complete, _ := extractBoth(t, landmarkPage(`<p>Energy: <math alttext="E=mc^2"><mi>E</mi></math></p><math display="block"><semantics><mrow><mi>x</mi></mrow><annotation encoding="application/x-tex">x+y=z</annotation></semantics></math>`))
	assertContainsAll(t, complete, "$E=mc^2$", "$$x+y=z$$")
}

func TestVideoEmbedBecomesLink(t *testing.T) {
	t.Parallel()
	complete, _ := extractBoth(t, landmarkPage(`<iframe src="https://www.youtube.com/embed/dQw4w9WgXcQ"></iframe><iframe src="https://player.vimeo.com/video/123456"></iframe><iframe src="https://ads.example.test/frame"></iframe>`))
	assertContainsAll(t, complete, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "https://vimeo.com/123456")
	assertContainsNone(t, complete, "ads.example.test")
}

func TestTidyMarkdown(t *testing.T) {
	t.Parallel()
	in := "a\n\n\n\nb\n- \n*\n[](x)c\n```\n\n\n\n```\n"
	want := "a\n\nb\nc\n```\n\n\n\n```"
	if got := tidyMarkdown(in); got != want {
		t.Fatalf("tidyMarkdown = %q, want %q", got, want)
	}
}

func TestCleanTitle(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, html, raw, want string }{
		{"h1 wins", `<h1>Getting Started</h1>`, "Getting Started | Ketch Docs", "Getting Started"},
		{"drop the site after the last separator", ``, "A Long Page Title Here - Site Name", "A Long Page Title Here"},
		{"site first", ``, "Site » Real Title", "Real Title"},
		{"too little either side", ``, "Home - Site", "Home - Site"},
		{"whitespace collapsed", ``, "  Two   words ", "Two words"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body>` + tc.html + `</body></html>`))
			if err != nil {
				t.Fatal(err)
			}
			if got := cleanTitle(doc, tc.raw); got != tc.want {
				t.Fatalf("cleanTitle(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestGridWidth(t *testing.T) {
	t.Parallel()
	for class, want := range map[string]int{"col-md-8": 8, "col-12 col-lg-3": 3, "span4": 4, "large-6 small-12": 6, "column": 0, "col-md-13": 0, "col-xs-4": 4, "": 0} {
		if got := gridWidth(class); got != want {
			t.Fatalf("gridWidth(%q) = %d, want %d", class, got, want)
		}
	}
}

func TestNameIsChrome(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"site-footer": true, "related-posts": true, "share-buttons": true, "ad-slot": true, "mdprint-only": true, "sidebar-toggle": true,
		"post-content": false, "article-body": false, "votelinks": false, "helpimprove": false, "mm-recipes-rate-print": false, "article__photo-ribbon": false,
	} {
		if got := nameIsChrome(name); got != want {
			t.Fatalf("nameIsChrome(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestWordsIn(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int{"one two three": 3, "": 0, "  a\tb\nc ": 3, "--- ... ---": 0, "x1 2y": 2, "naïve café": 2, "e.g. foo": 2} {
		if got := wordsIn(in); got != want {
			t.Fatalf("wordsIn(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestInPageLink(t *testing.T) {
	t.Parallel()
	page := "https://example.test/docs/page"
	cases := []struct {
		href string
		want bool
	}{
		{"#top", true}, {"/docs/page#x", true}, {"https://example.test/docs/page?lang=en#y", true},
		{"/docs/other#x", false}, {"https://other.test/docs/page#x", false}, {"/docs/page", false},
	}
	for _, tc := range cases {
		if got := inPageLink(tc.href, page); got != tc.want {
			t.Fatalf("inPageLink(%q) = %v, want %v", tc.href, got, tc.want)
		}
	}
	if inPageLink("other#x", "") {
		t.Fatal("a fragment on another path is not in-page without a page URL")
	}
}

func TestHiddenClasses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		class          string
		srOnly, hidden bool
	}{
		{"sr-only", true, false}, {"Visually-Hidden", true, false}, {"d-none", false, true}, {"!hidden", false, true},
		{"card hidden-xs", false, false}, {"", false, false}, {"sr-only hidden", true, true},
	}
	for _, tc := range cases {
		if sr, hid := hiddenClasses(tc.class); sr != tc.srOnly || hid != tc.hidden {
			t.Fatalf("hiddenClasses(%q) = %v, %v; want %v, %v", tc.class, sr, hid, tc.srOnly, tc.hidden)
		}
	}
}

func TestEnsureUTF8(t *testing.T) {
	t.Parallel()
	if got := ensureUTF8("caf\xe9"); got != "café" {
		t.Fatalf("windows-1252 bytes decoded as %q", got)
	}
	for _, s := range []string{"café", "", "plain"} {
		if got := ensureUTF8(s); got != s {
			t.Fatalf("valid UTF-8 %q changed to %q", s, got)
		}
	}
}

// A title set in a header band above the content block is put back when
// it is the page's own title.
func TestRescueTitle(t *testing.T) {
	t.Parallel()
	result, err := New().Extract("https://example.test/post", `<html><head><title>Rescued Title - Blog</title></head><body><header><h1>Rescued Title</h1></header><main><p>`+filler(6)+`</p></main></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Markdown, "# Rescued Title") || result.Title != "Rescued Title" {
		t.Fatalf("title not rescued: %q\n%s", result.Title, result.Markdown)
	}
}

func TestMathTeXIsNotEscaped(t *testing.T) {
	t.Parallel()
	complete, _ := extractBoth(t, landmarkPage(`<p>Display: <math display="block"><semantics><mrow><mi>x</mi></mrow><annotation encoding="application/x-tex">\int_0^1 x^2 dx = \frac{1}{3}</annotation></semantics></math></p><p>Inline <math alttext="{\displaystyle a_{n}}"><mi>a</mi></math> here.</p>`))
	assertContainsAll(t, complete, `$$\int_0^1 x^2 dx = \frac{1}{3}$$`, "Inline $a_{n}$ here.")
	assertContainsNone(t, complete, "ketchmath", `\\int`, `\_`)
}

func TestBlockquoteCodeKeepsQuotePrefix(t *testing.T) {
	t.Parallel()
	complete, _ := extractBoth(t, landmarkPage("<blockquote><pre>line one\nline two\nline three</pre></blockquote><p>after</p>"))
	assertContainsAll(t, complete, "> ```\n> line one\n> line two\n> line three\n> ```")
	if strings.Contains(complete, "\nline two") {
		t.Fatalf("quote lost its prefix:\n%s", complete)
	}
}

func TestQuoteFences(t *testing.T) {
	t.Parallel()
	in := "> ```\n> one\ntwo\n\nthree\n> ```\n\n> > ```go\n> > a\nb\n> > ```\n```\nplain\nfence\n```"
	want := "> ```\n> one\n> two\n> \n> three\n> ```\n\n> > ```go\n> > a\n> > b\n> > ```\n```\nplain\nfence\n```"
	if got := quoteFences(in); got != want {
		t.Fatalf("quoteFences:\n%s\nwant:\n%s", got, want)
	}
}

func TestListingOfArticlesKeepsEveryEntry(t *testing.T) {
	t.Parallel()
	entry := func(n string) string {
		return `<article><h2><a href="/p/` + n + `">Post ` + n + `</a></h2><p>ENTRY` + n + ` ` + filler(8) + `</p></article>`
	}
	page := `<html><head><title>Blog</title></head><body><nav><a href="/">Home</a></nav><div class="posts">` + entry("one") + entry("two") + entry("three") + `</div></body></html>`
	complete, clean := extractBoth(t, page)
	for _, md := range []string{complete, clean} {
		assertContainsAll(t, md, "ENTRYone", "ENTRYtwo", "ENTRYthree")
	}
	// A story with teaser articles beside it is still the story.
	story := `<html><head><title>Story</title></head><body><div><article><h1>Story</h1><p>STORYBODY ` + filler(30) + `</p></article>` +
		`<article><h3><a href="/t/1">Teaser</a></h3><p>TEASERONE ` + filler(3) + `</p></article><article><h3><a href="/t/2">Teaser</a></h3><p>TEASERTWO ` + filler(3) + `</p></article></div></body></html>`
	complete, _ = extractBoth(t, story)
	assertContainsAll(t, complete, "STORYBODY")
	assertContainsNone(t, complete, "TEASERONE", "TEASERTWO")
}

func TestLazyImagesKeepTheirSource(t *testing.T) {
	t.Parallel()
	complete, _ := extractBoth(t, landmarkPage(`<img data-src="https://x.test/lazy.png" alt="LAZYALT" class="lazyload">`+
		`<img src="data:image/gif;base64,R0lGODlhAQABAAAAACw=" data-src="https://x.test/swapped.png" alt="SWAPPEDALT">`+
		`<img src="/img/spacer.gif" data-srcset="https://x.test/set-400.png 400w, https://x.test/set-800.png 800w" alt="SETALT">`+
		`<img src="https://x.test/normal.png" alt="NORMALALT">`))
	assertContainsAll(t, complete, "![LAZYALT](https://x.test/lazy.png)", "![SWAPPEDALT](https://x.test/swapped.png)", "![SETALT](https://x.test/set-400.png)", "![NORMALALT](https://x.test/normal.png)")
	assertContainsNone(t, complete, "data-uri omitted")
}

func TestLinkListsInDataCellsAndCaptionsStay(t *testing.T) {
	t.Parallel()
	complete, clean := extractBoth(t, landmarkPage(`<table class="infobox"><tr><th>Born</th><td>1815</td></tr><tr><th>Known for</th><td><div class="plainlist"><ul><li><a href="/w/Engine">CELLLINKONE</a></li><li><a href="/w/NoteG">CELLLINKTWO</a></li><li><a href="/w/Bernoulli">CELLLINKTHREE</a></li></ul></div></td></tr></table>`+
		`<div class="thumb"><figure><a href="/wiki/File:x.jpg"><img src="https://x.test/x.jpg" alt=""></a><figcaption><a href="/w/Babbage">CAPLINKONE</a>, <a href="/w/Engine">CAPLINKTWO</a>, <a href="/w/Byron">CAPLINKTHREE</a></figcaption></figure></div>`+
		`<div class="tags"><a href="/t/1">TAGONE</a> <a href="/t/2">TAGTWO</a> <a href="/t/3">TAGTHREE</a></div>`))
	for _, md := range []string{complete, clean} {
		assertContainsAll(t, md, "CELLLINKONE", "CELLLINKTHREE", "CAPLINKONE", "CAPLINKTHREE")
		assertContainsNone(t, md, "TAGONE")
	}
}

func TestLineNumbersLeaveCode(t *testing.T) {
	t.Parallel()
	complete, _ := extractBoth(t, landmarkPage(`<div class="highlight"><pre><span></span><code><span class="linenos"> 1</span><span class="kn">import</span> <span class="nn">requests</span>
<span class="linenos"> 2</span><span class="n">print</span><span class="p">(</span><span class="n">x</span><span class="p">)</span>
</code></pre></div>`+
		`<table class="highlighttable"><tr><td class="linenos"><div class="linenodiv"><pre><span class="normal">1</span>
<span class="normal">2</span></pre></div></td><td class="code"><div class="highlight"><pre><span></span>def f():
    return 1
</pre></div></td></tr></table>`))
	assertContainsAll(t, complete, "import requests\nprint(x)", "def f():\n    return 1")
	assertContainsNone(t, complete, " 1import", "\n1\n2")
}

func TestAriaHiddenPanelWithControlStays(t *testing.T) {
	t.Parallel()
	complete, clean := extractBoth(t, landmarkPage(`<button aria-controls="adv" aria-expanded="false">Advanced options</button><div id="adv" aria-hidden="true"><p>PANELCONTENT is what the author folded away for later</p></div><span aria-hidden="true">DECORATIVE</span>`))
	for _, md := range []string{complete, clean} {
		assertContainsAll(t, md, "PANELCONTENT")
		assertContainsNone(t, md, "DECORATIVE")
	}
}

func TestHubDecisionIsTheSameInBothModes(t *testing.T) {
	t.Parallel()
	card := func(n string) string {
		return `<div class="story"><h3><a href="/s/` + n + `">Story ` + n + `</a></h3><p>` + n + ` blurb of a dozen words that says what the story is about today</p></div>`
	}
	rail := `<div class="rail">` + card("A1") + card("A2") + card("A3") + `</div>`
	popular := `<section><h2>Most popular</h2><div class="grid">` + card("B1") + card("B2") + card("B3") + card("B4") + card("B5") + card("B6") + `</div></section>`
	page := `<html><head><title>Front</title></head><body><main>` + rail + `<h1>Front</h1><p>` + filler(10) + `</p>` + popular + `</main></body></html>`
	complete, clean := extractBoth(t, page)
	for _, md := range []string{complete, clean} {
		assertContainsAll(t, md, "Story A1", "Story B6")
	}
}
