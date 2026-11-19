package scrape

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// markdownExport is a docs site's raw Markdown export: YAML frontmatter
// with the page's title, Markdoc heading ids, a Markdoc tag line and a
// code block whose "# comment" is no heading.
const markdownExport = "---\ntitle: \"Agent Configuration\"\ndescription: How to configure the agent.\n---\n\n" +
	"# Agent configuration\n\nThe agent reads datadog.yaml.\n\n" +
	"## Proxy settings{% #proxy-settings %}\n\nSet proxy.https to route traffic.\n\n" +
	"{% tab title=\"Linux\" %}\n\n```shell\n# not a heading\nsudo systemctl restart agent\n```\n\n{% /tab %}\n\n" +
	"## Log collection{% #log-collection %}\n\nSet logs_enabled: true.\n"

// markdownServer serves body at every path with contentType.
func markdownServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// A text/markdown response is kept as Markdown, as written: its headings
// (with their Markdoc ids) and code blocks intact, the frontmatter's
// title as the page's title, no frontmatter in the body, CRLF line endings
// normalised; by Scrape and by ScrapeConditional alike.
func TestScrapeMarkdownExport(t *testing.T) {
	for _, c := range []struct{ name, contentType, body string }{
		{"text/markdown", "text/markdown; charset=utf-8", markdownExport},
		{"CRLF", "text/markdown", strings.ReplaceAll(markdownExport, "\n", "\r\n")},
		{"BOM", "text/x-markdown", "\ufeff" + markdownExport},
	} {
		t.Run(c.name, func(t *testing.T) {
			server := markdownServer(t, c.contentType, c.body)
			page, source, err := New().Scrape(t.Context(), server.URL+"/docs/agent/configuration")
			if err != nil || source != SourceHTTP {
				t.Fatalf("Scrape: %v (%s)", err, source)
			}
			checkMarkdownPage(t, page.Title, page.Markdown)
			result, err := New().ScrapeConditional(t.Context(), server.URL+"/docs/agent/configuration", "", "")
			if err != nil {
				t.Fatalf("ScrapeConditional: %v", err)
			}
			checkMarkdownPage(t, result.Page.Title, result.Page.Markdown)
			if result.Doc != nil || result.RawHTML != "" || result.Page.ContentHash == "" {
				t.Errorf("Markdown entered the HTML pipeline: %#v", result)
			}
		})
	}
}

func checkMarkdownPage(t *testing.T, title, md string) {
	t.Helper()
	if title != "Agent Configuration" {
		t.Errorf("title = %q, want the frontmatter's", title)
	}
	for _, want := range []string{"# Agent configuration\n", "\n## Proxy settings{% #proxy-settings %}\n", "\n## Log collection{% #log-collection %}\n",
		"```shell\n# not a heading\nsudo systemctl restart agent\n```"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "description:") || strings.Contains(md, "title:") || strings.HasPrefix(md, "---") || strings.Contains(md, "\r") || strings.HasPrefix(md, "\ufeff") {
		t.Errorf("frontmatter, CR or BOM left in the body:\n%q", md)
	}
}

// Plain text is Markdown only at a Markdown path; HTML never is.
func TestIsMarkdownResponse(t *testing.T) {
	for _, c := range []struct {
		contentType, url string
		want             bool
	}{
		{"text/markdown", "https://x.test/a", true},
		{"text/x-markdown", "https://x.test/a", true},
		{"text/plain", "https://x.test/docs/a.md", true},
		{"text/plain", "https://x.test/docs/a.MDX", true},
		{"text/plain", "https://x.test/docs/a.markdown?raw=1", true},
		{"text/plain", "https://x.test/docs/a.txt", false},
		{"text/plain", "https://x.test/docs/a", false},
		{"text/html", "https://x.test/docs/a.md", false},
	} {
		if got := isMarkdownResponse(c.contentType, c.url); got != c.want {
			t.Errorf("isMarkdownResponse(%q, %q) = %v, want %v", c.contentType, c.url, got, c.want)
		}
	}
}

// A page with no frontmatter title takes its first H1 outside code; an
// unclosed "---" block is text, not frontmatter.
func TestMarkdownPageTitle(t *testing.T) {
	for _, c := range []struct{ body, title, starts string }{
		{"```\n# comment\n```\n\n# Real title\n\nText.", "Real title", "```"},
		{"---\nlayout: x\n---\n# From H1 #\n", "From H1", "# From H1"},
		{"---\ntitle: 'Quoted'\n...\nBody.", "Quoted", "Body."},
		{"---\nnot closed\n\nBody.", "", "---"},
		{"Just text.", "", "Just text."},
	} {
		title, md := markdownPage([]byte(c.body))
		if title != c.title || !strings.HasPrefix(md, c.starts) {
			t.Errorf("markdownPage(%q) = %q, %q; want %q, starting %q", c.body, title, md, c.title, c.starts)
		}
	}
}

// Plain text at a .md path is kept as Markdown by Scrape too.
func TestScrapePlainTextAtMarkdownPath(t *testing.T) {
	server := markdownServer(t, "text/plain; charset=utf-8", "# Notes\n\n## First{% #first %}\n\nOne.\n")
	page, _, err := New().Scrape(t.Context(), server.URL+"/notes.md")
	if err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	if page.Title != "Notes" || !strings.Contains(page.Markdown, "\n## First{% #first %}\n") {
		t.Errorf("page %q:\n%s", page.Title, page.Markdown)
	}
}
