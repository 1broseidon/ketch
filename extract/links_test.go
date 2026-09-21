package extract

import (
	"strings"
	"testing"
)

func TestExtractSelectorWithURLMatchesBeforeResolving(t *testing.T) {
	t.Parallel()
	page := `<article><a href="../reference#item">reference</a><a href="/root">root</a><a href="?lang=en">query</a><a href="#details">section</a><a href="//cdn.test/file">cdn</a><a href="https://other.test/page">external</a><img src="image.png" alt="diagram"></article>`
	md, err := ExtractSelectorWithURL("https://example.test/docs/chapter/page", page, "a[href^='../'], img")
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, md, "https://example.test/docs/reference#item", "https://example.test/docs/chapter/image.png")
	assertContainsNone(t, md, "external")
	md, err = ExtractSelectorWithURL("https://example.test/docs/chapter/page", page, "article")
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, md, "https://example.test/root", "https://example.test/docs/chapter/page?lang=en", "https://example.test/docs/chapter/page#details", "https://cdn.test/file", "https://other.test/page")
	legacy, err := ExtractSelector(page, "a[href^='../']")
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, legacy, "](../reference#item)")
}

// Sibling links resolve against the page's directory, not the site root,
// on every conversion path.
func TestExtractResolvesPageRelativeLinks(t *testing.T) {
	t.Parallel()
	page := `<html><head><title>Chapter</title></head><body><nav><a href="/docs">SITECHROME</a></nav><div><h1>Chapter</h1><p>` +
		strings.Repeat("The chapter describes the API and links to its reference from the table. ", 10) +
		`</p><table><tr><th>Reference</th><th>Type</th></tr><tr><td><a href="../reference">API</a></td><td>Guide</td></tr></table></div></body></html>`
	result, err := New().Extract("https://example.test/docs/chapter/page", page)
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, result.Markdown, "https://example.test/docs/reference")
	assertContainsNone(t, result.Markdown, "https://example.test/reference", "SITECHROME")
	raw, err := extractRaw("https://example.test/docs/chapter/page", page)
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, raw.Markdown, "https://example.test/docs/reference")
	assertContainsNone(t, raw.Markdown, "https://example.test/reference")
}

func TestSelectorEmptyAnchorsDoNotBecomePageLinks(t *testing.T) {
	t.Parallel()
	md, err := ExtractSelectorWithURL("https://example.test/docs/page", `<article><a id="section"></a><a href=""></a><p>Content</p></article>`, "article")
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, md, "Content")
	assertContainsNone(t, md, "https://example.test")
}
