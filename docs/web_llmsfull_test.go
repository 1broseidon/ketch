package docs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// End to end: a VitePress site's llms-full.txt answers from its pages,
// each result at its own page rather than at llms-full.txt.
func TestWebFrontmatterFullTextCitesPages(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://sinonjs.org/llms.txt":      plain(llmsFullFixture(t, "sinonjs.org", "llms.txt")),
		"https://sinonjs.org/llms-full.txt": plain(llmsFullFixture(t, "sinonjs.org", "llms-full.txt")),
	}}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://sinonjs.org", "clock tick", 2000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) == 0 || results[0].URL != "https://sinonjs.org/concepts/fake-timers/tick" {
		t.Fatalf("first result should be the tick page: %+v", results)
	}
	if slices.ContainsFunc(results, func(r Result) bool { return strings.Contains(r.URL, "llms-full.txt") }) {
		t.Errorf("a result cites llms-full.txt: %+v", results)
	}
}

// GitBook paginates llms-full.txt ("part 1 of 8 … Next part: …"); the
// first part holds a hundred pages, llms.txt lists them all. A page the
// query's candidates include that llms-full.txt does not hold is fetched;
// the ones it holds are not. Shaped like gitbook.com/docs.
func TestWebPartialFullTextFetchesUncoveredPages(t *testing.T) {
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://book.test/docs/llms.txt":                  plain("# Bookery\n\n- [Quickstart](https://book.test/docs/quickstart.md)\n- [Custom domains](https://book.test/docs/custom-domains.md)\n- [Visitor authentication](https://book.test/docs/visitor-authentication.md)\n"),
		"https://book.test/docs/llms-full.txt":             plain("> This is part 1 of 2 of the full documentation (pages 1–1 of 3).\n> Next part: https://book.test/docs/llms-full.txt/1\n\n# Quickstart\n\nCreate a space and publish it.\n"),
		"https://book.test/docs/visitor-authentication.md": md("# Visitor authentication\n\nVisitor authentication lets you publish docs behind a login with a JWT signing key.\n"),
	}}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://book.test/docs/", "visitor authentication jwt", 2000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) == 0 || results[0].URL != "https://book.test/docs/visitor-authentication" {
		t.Fatalf("results = %+v", results)
	}
	for _, c := range f.called() {
		if strings.Contains(c, "quickstart") {
			t.Errorf("a page llms-full.txt holds was fetched: %s", c)
		}
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

// A large llms-full.txt is read as the site's full text: its best-matching
// pages, interleaved with llms.txt's best by title, are the pages ranked,
// so many reference pages that each mention the query's words do not
// crowd out the guide llms.txt titles with them.
func TestWebFullTextRanksChosenPages(t *testing.T) {
	var full, links strings.Builder
	links.WriteString("# Widgets\n\n- [Retry policies](https://wid.test/guides/retry.md): How to retry failed requests with backoff\n")
	full.WriteString("---\nurl: /guides/retry.md\n---\n\n# Retry policies\n\nWrap a call in `withRetry(fn, {attempts: 3})` to retry it with exponential backoff.\n\n")
	for i := range 8 {
		fmt.Fprintf(&links, "- [Client option %d](https://wid.test/ref/opt%d.md)\n", i, i)
		fmt.Fprintf(&full, "---\nurl: /ref/opt%d.md\n---\n\n# Client option %d\n\n## retry\n\nThe retry retry option of client %d: retry count for failed requests, retry delay.\n\n", i, i, i)
	}
	f := &fakeFetcher{raw: map[string]fakeRaw{
		"https://wid.test/llms.txt":      plain(links.String()),
		"https://wid.test/llms-full.txt": plain(full.String()),
	}}
	results, err := NewWeb(f).GetDocs(context.Background(), "https://wid.test/", "retry policies backoff", 4000)
	if err != nil {
		t.Fatalf("GetDocs: %v", err)
	}
	if len(results) == 0 || results[0].URL != "https://wid.test/guides/retry" {
		t.Fatalf("first result = %+v", results)
	}
	var pages []string
	for _, r := range results {
		if p, _, _ := strings.Cut(r.URL, "#"); !slices.Contains(pages, p) {
			pages = append(pages, p)
		}
	}
	if len(pages) > webMaxFetch {
		t.Errorf("results come from %d pages, more than the %d chosen: %v", len(pages), webMaxFetch, pages)
	}
}
