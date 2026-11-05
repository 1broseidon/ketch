package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// llmsFullFixture reads testdata/llmsfull/<site>/<file>.
func llmsFullFixture(t *testing.T, site, file string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "llmsfull", site, file))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type chunkWant struct {
	title, ref, bodyHas string
}

func checkChunks(t *testing.T, got []fullChunk, want []chunkWant) {
	t.Helper()
	if len(got) != len(want) {
		for _, c := range got {
			t.Logf("chunk %q refs=%v body=%q", c.title, c.refs, c.body)
		}
		t.Fatalf("got %d chunks, want %d", len(got), len(want))
	}
	for i, w := range want {
		c := got[i]
		ref := ""
		if len(c.refs) > 0 {
			ref = c.refs[0]
		}
		if c.title != w.title || ref != w.ref || !strings.Contains(c.body, w.bodyHas) {
			t.Errorf("chunk %d = %q ref %q body %q; want %q ref %q body containing %q", i, c.title, ref, c.body, w.title, w.ref, w.bodyHas)
		}
		for _, noise := range []string{"description:", "url:", "docs_index:"} {
			if strings.Contains(c.body, noise) {
				t.Errorf("chunk %q keeps frontmatter %q in its body", c.title, noise)
			}
		}
		if strings.HasSuffix(c.body, "---") {
			t.Errorf("chunk %q ends with the rule between pages", c.title)
		}
	}
}

// VitePress's llms plugin puts a YAML frontmatter block, with a folded
// multi-line description, before each page's H1.
func TestSplitFullTextFoldedFrontmatter(t *testing.T) {
	got := splitFullText(llmsFullFixture(t, "sinonjs.org", "llms-full.txt"))
	checkChunks(t, got, []chunkWant{
		{"Fake timers", "/concepts/fake-timers.md", "useFakeTimers"},
		{"clock.tick(time) / await clock.tickAsync(time)", "/concepts/fake-timers/tick.md", "Tick the clock ahead"},
		{"clock.restore()", "/concepts/fake-timers/restore.md", "Restore the faked methods"},
	})
}

// Pages delimited by frontmatter alone, with no H1: the frontmatter's
// title names the page and its quoted url locates it. A rule inside a page
// is not a page break.
func TestSplitFullTextFrontmatterWithoutH1(t *testing.T) {
	got := splitFullText(llmsFullFixture(t, "ai-sdk.dev", "llms-full.txt"))
	checkChunks(t, got, []chunkWant{
		{"AI SDK documentation", "", "TypeScript toolkit"},
		{"Backpressure", "https://ai-sdk.dev/docs/advanced/backpressure", "async function* integers"},
		{"Caching", "https://ai-sdk.dev/docs/advanced/caching", "A horizontal rule inside a page"},
	})
}

// A heading that is only a link to the page, then the page's own H1, is
// one page at that link, not an empty page and an unattributed one.
func TestSplitFullTextLinkedHeading(t *testing.T) {
	got := splitFullText(llmsFullFixture(t, "linked-h1.test", "llms-full.txt"))
	checkChunks(t, got, []chunkWant{
		{"Widgetry", "", "Widget validation"},
		{"Aliases", "https://linked-h1.test/docs/api/aliases/", "AliasPath"},
		{"Fields", "https://linked-h1.test/docs/api/fields/", "Create a field"},
	})
}

// Two pages each opened by an H1 stay two pages, even when the first has
// only metadata: an H1 joins a page only when that page waits for a title.
func TestSplitFullTextH1PagesStaySeparate(t *testing.T) {
	got := splitFullText("# A\nSource: https://x.test/a\n\n# B\nSource: https://x.test/b\n\nBody of B.\n")
	if len(got) != 2 || got[1].title != "B" || len(got[1].refs) != 1 || got[1].refs[0] != "https://x.test/b" {
		t.Fatalf("got %+v", got)
	}
}

// A rule then ordinary text is not frontmatter.
func TestFrontmatterEndNeedsKeys(t *testing.T) {
	lines := strings.Split("---\n\nJust text after a rule.\n---\n", "\n")
	if _, ok := frontmatterEnd(lines, 0); ok {
		t.Error("a rule followed by text was read as frontmatter")
	}
	lines = strings.Split("---\ntitle: X\ndescription: >-\n  folded\n  lines\nurl: /x.md\n---\n", "\n")
	if end, ok := frontmatterEnd(lines, 0); !ok || end != 6 {
		t.Errorf("frontmatterEnd = %d, %v; want 6, true", end, ok)
	}
}

// Fumadocs' default llms-full.txt writes each page's address after its
// title: "# Title (/docs/path)". Shaped like prisma.io/docs.
func TestSplitFullTextPathAfterTitle(t *testing.T) {
	got := splitFullText("# Prisma Documentation - Full Content Feed\n\nThis file contains every page.\n\n# Choose a setup path (/docs/getting-started)\n\nChoose the fastest path to try Prisma ORM.\n\n# Install on macOS (with Homebrew)\n\nA title whose parentheses name no address.\n")
	checkChunks(t, got, []chunkWant{
		{"Prisma Documentation - Full Content Feed", "", "every page"},
		{"Choose a setup path", "/docs/getting-started", "fastest path"},
		{"Install on macOS (with Homebrew)", "", "parentheses"},
	})
}
