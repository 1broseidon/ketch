package cmd

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/scrape"
)

func sampleArticleHTML() string {
	return `<!doctype html>
<html>
<head><title>Example Title</title></head>
<body>
<nav>Home About</nav>
<article>
<h1>Hello World</h1>
<p>This is the main article content with enough text for readability to pick it up.</p>
<p>A second paragraph adds more body so the article detector is satisfied.</p>
</article>
<footer>Copyright</footer>
</body>
</html>`
}

func TestExtract_ReadabilityMarkdown(t *testing.T) {
	page, err := extractFromHTML(sampleArticleHTML(), extractOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Title != "Example Title" {
		t.Errorf("title = %q, want %q", page.Title, "Example Title")
	}
	if !strings.Contains(strings.ToLower(page.Markdown), "main article content") {
		t.Errorf("markdown missing article body, got: %q", page.Markdown)
	}
}

func TestExtract_NoURLOmitsURLFrontmatter(t *testing.T) {
	page, err := extractFromHTML(sampleArticleHTML(), extractOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var buf bytes.Buffer
	if err := emitExtract(&buf, page, false); err != nil {
		t.Fatalf("emit error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "url:") {
		t.Errorf("plain output should not contain url line, got: %s", out)
	}
	if !strings.Contains(out, "title: Example Title") {
		t.Errorf("plain output missing title line, got: %s", out)
	}
}

func TestExtract_WithURLIncludesURLFrontmatter(t *testing.T) {
	page, err := extractFromHTML(sampleArticleHTML(), extractOptions{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var buf bytes.Buffer
	if err := emitExtract(&buf, page, false); err != nil {
		t.Fatalf("emit error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "url: https://example.com") {
		t.Errorf("plain output missing url line, got: %s", out)
	}
	if !strings.Contains(out, "title: Example Title") {
		t.Errorf("plain output missing title line, got: %s", out)
	}
}

func TestExtract_JSONShape(t *testing.T) {
	page, err := extractFromHTML(sampleArticleHTML(), extractOptions{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var buf bytes.Buffer
	if err := emitExtract(&buf, page, true); err != nil {
		t.Fatalf("emit error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"url":"https://example.com"`) {
		t.Errorf("json missing url, got: %s", out)
	}
	if !strings.Contains(out, `"title":"Example Title"`) {
		t.Errorf("json missing title, got: %s", out)
	}
	if !strings.Contains(out, `"markdown":`) {
		t.Errorf("json missing markdown, got: %s", out)
	}
	if !strings.Contains(out, `"words":`) {
		t.Errorf("json missing words, got: %s", out)
	}
	if strings.Contains(out, `"fetched_url"`) {
		t.Errorf("json must not include fetched_url, got: %s", out)
	}
	if strings.Contains(out, `"raw_html"`) {
		t.Errorf("json must not include raw_html, got: %s", out)
	}
}

func TestExtract_JSONOmitsURLWhenEmpty(t *testing.T) {
	page, err := extractFromHTML(sampleArticleHTML(), extractOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var buf bytes.Buffer
	if err := emitExtract(&buf, page, true); err != nil {
		t.Fatalf("emit error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `"url"`) {
		t.Errorf("json should omit empty url, got: %s", out)
	}
}

func TestExtract_SelectMatches(t *testing.T) {
	html := `<!doctype html><html><head><title>Selector Page</title></head>
<body><main><p>only this content</p></main><aside>sidebar noise</aside></body></html>`
	page, err := extractFromHTML(html, extractOptions{Selector: "main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(page.Markdown), "only this content") {
		t.Errorf("selector markdown missing selected content, got: %q", page.Markdown)
	}
	if strings.Contains(strings.ToLower(page.Markdown), "sidebar noise") {
		t.Errorf("selector markdown leaked aside content, got: %q", page.Markdown)
	}
}

func TestExtract_SelectWithURLResolvesRelativeLinks(t *testing.T) {
	html := `<!doctype html><html><head><title>Links</title></head>
<body><article><a href="/relative">relative link</a><img src="image.png" alt="local image"></article></body></html>`
	page, err := extractFromHTML(html, extractOptions{Selector: "article", URL: "https://example.com/base/page"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(page.Markdown, "https://example.com/relative") {
		t.Errorf("selector markdown did not resolve absolute href, got: %q", page.Markdown)
	}
	if !strings.Contains(page.Markdown, "https://example.com/base/image.png") {
		t.Errorf("selector markdown did not resolve relative src, got: %q", page.Markdown)
	}
}

func TestExtract_SelectNoMatchExitNotFound(t *testing.T) {
	_, err := extractFromHTML(sampleArticleHTML(), extractOptions{Selector: ".does-not-exist"})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitNotFound {
		t.Errorf("exit code = %d, want %d", exitErr.Code, ExitNotFound)
	}
}

func TestExtract_SelectURLAttributeUsesOriginalHTML(t *testing.T) {
	page, err := extractFromHTML(`<a href="../reference">reference</a>`, extractOptions{Selector: `a[href^="../"]`, URL: "https://example.test/docs/chapter/page"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Markdown, "https://example.test/docs/reference") {
		t.Fatalf("relative source link was not selected and resolved: %s", page.Markdown)
	}
}

func TestExtract_BadSelectorExitValidation(t *testing.T) {
	_, err := extractFromHTML(sampleArticleHTML(), extractOptions{Selector: "["})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d", exitErr.Code, ExitValidation)
	}
}

func TestExtract_TrimAndMaxChars(t *testing.T) {
	html := `<!doctype html><html><head><title>Trim Page</title></head>
<body><article><h1>Heading</h1><p><strong>bold</strong> and <em>italic</em> text here with enough words to exceed a small limit and trigger truncation behavior in the post processing step.</p></article></body></html>`
	page, err := extractFromHTML(html, extractOptions{Trim: true, MaxChars: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(page.Markdown, "**") {
		t.Errorf("trim should strip markdown bold, got: %q", page.Markdown)
	}
	if strings.Contains(page.Markdown, "*") {
		t.Errorf("trim should strip markdown italic, got: %q", page.Markdown)
	}
	if !strings.Contains(page.Markdown, "[truncated]") {
		t.Errorf("expected truncation marker under max-chars=20, got: %q", page.Markdown)
	}
}

func TestExtract_EmptyInputExitValidation(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n\t  \n"} {
		_, err := extractFromHTML(in, extractOptions{})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("input %q: expected *ExitError, got %T: %v", in, err, err)
		}
		if exitErr.Code != ExitValidation {
			t.Errorf("input %q: exit code = %d, want %d", in, exitErr.Code, ExitValidation)
		}
	}
}

func TestExtract_PDFInputExitValidation(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	if _, err := writer.Write([]byte("%PDF-1.7\n")); err != nil {
		t.Fatalf("write PDF input: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = reader.Close()
	})

	err = runExtract(extractCmd, nil)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d", exitErr.Code, ExitValidation)
	}
	const want = "PDF input detected. Use `ketch scrape <url>` for PDF extraction."
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestExtract_InputTooLargeExitValidation(t *testing.T) {
	_, err := readExtractInput(strings.NewReader(strings.Repeat("x", scrape.MaxBodyBytes+1)))
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d", exitErr.Code, ExitValidation)
	}
}

func TestExtract_RejectsArgs(t *testing.T) {
	err := extractCmd.Args(extractCmd, []string{"page.html"})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d", exitErr.Code, ExitValidation)
	}
}

func TestExtract_CommandDoesNotExposeScrapeOnlyFlags(t *testing.T) {
	excluded := []string{"raw", "no-cache", "concurrency", "force-browser", "no-llms-txt"}
	for _, name := range excluded {
		if extractCmd.Flags().Lookup(name) != nil {
			t.Errorf("extract command must not expose scrape-only flag %q", name)
		}
	}
}

// extract_mode from config reaches the extractor: the default (clean) drops a
// block that only its name condemns, complete keeps it.
func TestExtract_ModeFromConfig(t *testing.T) {
	html := `<html><head><title>Guide</title></head><body><main><h1>Guide</h1><p>` + strings.Repeat("Words about the guide and its behavior. ", 30) +
		`</p><div class="related-posts"><p>RELATEDTEASER one two three four five six seven eight nine</p></div></main></body></html>`
	clean, err := extractFromHTML(html, extractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(clean.Markdown, "RELATEDTEASER") {
		t.Fatalf("default (clean) mode kept a named block:\n%s", clean.Markdown)
	}
	complete, err := extractFromHTML(html, extractOptions{Mode: extract.ModeComplete})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(complete.Markdown, "RELATEDTEASER") {
		t.Fatalf("complete mode dropped a block by name:\n%s", complete.Markdown)
	}
}
