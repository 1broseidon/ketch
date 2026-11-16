package docs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// mdExport is a raw Markdown export as a docs site serves it: YAML
// frontmatter with the page's title, Markdoc heading ids ({% #id %}),
// Markdoc tag lines around a code block whose "# comment" is no heading.
const mdExport = "---\ntitle: \"Agent Configuration\"\ndescription: How to configure the agent.\n---\n\n" +
	"# Agent configuration\n\nThe agent reads datadog.yaml.\n\n" +
	"## Proxy settings{% #proxy-settings %}\n\nSet proxy.https to route traffic.\n\n" +
	"{% tab title=\"Linux\" %}\n\n```shell\n# not a heading\nsudo systemctl restart agent\n```\n\n{% /tab %}\n\n" +
	"{% image\n   src=\"x.png\"\n   alt=\"a diagram\" /%}\n\n" +
	"### Proxy exceptions{% #no-proxy %}\n\nList hosts in proxy.no_proxy.\n\n" +
	"## Log collection{% #log-collection %}\n\nSet logs_enabled: true.\n"

// A raw Markdown page read by the docs fetch path, by its .md address or
// as a page the scrape pipeline returns, is sectioned by its own headings,
// with the Markdoc ids as anchors, its title from the frontmatter and no
// frontmatter in its text; CRLF line endings change nothing.
func TestMarkdownExportSections(t *testing.T) {
	const page = "https://dd.example.test/docs/agent/configuration"
	for _, c := range []struct{ name, body string }{{"LF", mdExport}, {"CRLF", strings.ReplaceAll(mdExport, "\n", "\r\n")}} {
		for _, viaRaw := range []bool{true, false} {
			f := &fakeFetcher{raw: map[string]fakeRaw{}, pages: map[string]fakePage{}}
			addr := page
			if viaRaw {
				addr = page + ".md"
				f.raw[addr] = md(c.body)
			} else {
				f.pages[addr] = fakePage{title: "configuration", markdown: c.body}
			}
			w := NewWeb(f)
			p, err := w.fetchOne(context.Background(), rootOf("https://dd.example.test/docs/"), webLink{url: mustURL(t, addr)})
			if err != nil {
				t.Fatalf("%s raw=%v: %v", c.name, viaRaw, err)
			}
			if p.Title != "Agent Configuration" || strings.Contains(p.Markdown, "description:") {
				t.Errorf("%s raw=%v: title %q, text:\n%s", c.name, viaRaw, p.Title, p.Markdown)
			}
			checkMarkdownSections(t, fmt.Sprintf("%s raw=%v", c.name, viaRaw), splitSections(*p, 0))
		}
	}
}

// checkMarkdownSections: the export's sections, by their Markdoc ids, no
// Markdoc, frontmatter or CR left in their cleaned text, the code block's
// "# comment" no heading.
func checkMarkdownSections(t *testing.T, name string, secs []webSection) {
	t.Helper()
	var anchors []string
	for _, s := range secs {
		anchors = append(anchors, s.anchor)
		clean := cleanText(s.text)
		if strings.Contains(clean, "{%") || strings.Contains(clean, "%}") || strings.Contains(clean, "description:") || strings.Contains(clean, "\r") {
			t.Errorf("%s: section %q keeps Markdoc, frontmatter or CR:\n%q", name, s.heading, clean)
		}
		if strings.Contains(s.heading, "{%") || s.heading == "not a heading" {
			t.Errorf("%s: heading %q", name, s.heading)
		}
	}
	for _, want := range []string{"proxy-settings", "no-proxy", "log-collection"} {
		if !slices.Contains(anchors, want) {
			t.Errorf("%s: anchors %q, want %s", name, anchors, want)
		}
	}
	at := slices.Index(anchors, "proxy-settings")
	if at < 0 || !strings.Contains(secs[at].text, "sudo systemctl restart agent") || !strings.EqualFold(secs[at].crumbs[0], "Agent Configuration") {
		t.Errorf("%s: proxy section %+v", name, secs[max(at, 0)])
	}
}

// A Markdoc heading id is the heading's anchor, its annotation out of the
// heading text; Markdoc tag lines go from the text, code keeps them.
func TestMarkdocHeadingsAndTags(t *testing.T) {
	for in, want := range map[string][2]string{
		"Proxy settings{% #proxy-settings %}":     {"Proxy settings", "proxy-settings"},
		"Proxy settings {% #proxy-settings .x %}": {"Proxy settings", "proxy-settings"},
		"Install {#install}":                      {"Install", "install"},
		"Parameters {/*parameters*/}":             {"Parameters", "parameters"},
		"Plain heading":                           {"Plain heading", ""},
	} {
		h := headingAnchor(in)
		if h.text != want[0] || h.id != want[1] {
			t.Errorf("headingAnchor(%q) = %q #%q, want %q #%q", in, h.text, h.id, want[0], want[1])
		}
	}
	for _, c := range []struct{ in, want string }{
		{"{% tab title=\"Linux\" %}\nText.\n{% /tab %}", "Text."},
		{"Before.\n{% image\n   src=\"x.png\" /%}\nAfter.", "Before.\nAfter."},
		{"```\n{% raw %}\n```", "```\n{% raw %}\n```"},
		{"Use {% x %} inline.", "Use {% x %} inline."},
	} {
		if got := cleanText(c.in); got != c.want {
			t.Errorf("cleanText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
