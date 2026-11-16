package docs

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/search"
)

// --tokens is a hard bound on the docs text, for any page shape: a block
// larger than the budget is cut inside, at lines (rows for a table, its
// header kept), keeping the lines that hold the question's terms, and
// marked cut (evidence_clip.go).

// boundedAnswer answers q from one page (markdown) at tokens and checks
// the passages stay within tokens and are marked cut; it returns the
// first group's text.
func boundedAnswer(t *testing.T, markdown, q string, tokens int) string {
	t.Helper()
	const page = "https://manual.example.test/tool"
	f := &fakeFetcher{pages: map[string]fakePage{page: {title: "tool", markdown: markdown}}}
	s, _, _ := searchFirstOver(t, f, []search.Result{hit(page, "tool")})
	ctx := WithLocalOptions(context.Background(), LocalOptions{Hint: HintCLI, Tokens: tokens})
	o := s.SearchSelect(ctx, q, 0)
	if o.Err() != nil || len(o.Results) == 0 {
		t.Fatalf("%s: %v, %d results", q, o.Err(), len(o.Results))
	}
	if n := estimateTokens(renderPassages(o.Results)); n > tokens {
		t.Errorf("%s: the docs text estimates %d tokens, over %d", q, n, tokens)
	}
	if !o.Results[0].Truncated || !strings.Contains(RenderGroup(o.Results[0], true), "(cut, ") {
		t.Errorf("%s: the first group is not marked cut", q)
	}
	return o.Results[0].Snippet
}

// lines are n numbered filler lines, with hit's text on line at.
func lines(n, at int, filler, hit string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf(filler, i)
	}
	out[at] = hit
	return out
}

func TestBoundOneHugeCodeBlock(t *testing.T) {
	body := lines(3000, 2200, "    step %d: copy the next chunk of the input buffer", "    frobnicate --level 3 sets the frobnication depth")
	got := boundedAnswer(t, "# tool\n\n```sh\n"+strings.Join(body, "\n")+"\n```\n", "frobnicate level", 4000)
	if !strings.HasPrefix(got, "```sh\n") || !strings.HasSuffix(got, "\n```") || !strings.Contains(got, "frobnicate --level 3") || !strings.Contains(got, "…") {
		t.Errorf("code block cut: fences, the line holding the terms and … between runs wanted:\n%.400s", got)
	}
	if !strings.Contains(got, "step 2199") || !strings.Contains(got, "step 2201") {
		t.Errorf("no context kept around the line holding the terms")
	}
}

func TestBoundOneHugeTable(t *testing.T) {
	rows := lines(2500, 1800, "| opt_%d | off | What option %d does, in a sentence or so. |", "| frobnicate_level | 3 | The frobnication depth. |")
	md := "# Options\n\n| Option | Default | Meaning |\n|---|---|---|\n" + strings.Join(rows, "\n") + "\n"
	got := boundedAnswer(t, md, "frobnicate_level default", 4000)
	if !strings.Contains(got, "frobnicate_level | 3") || !strings.HasPrefix(got, "| Option | Default | Meaning |") {
		t.Errorf("table cut: header row and the matching row wanted:\n%.400s", got)
	}
}

func TestBoundPageWithNoHeadings(t *testing.T) {
	paras := lines(1500, 1200, "Paragraph %d says something about buffers and copying.", "The frobnicate option sets the frobnication depth to 3 by default.")
	got := boundedAnswer(t, strings.Join(paras, "\n\n")+"\n", "frobnicate option default", 4000)
	if !strings.Contains(got, "frobnication depth to 3") {
		t.Errorf("no-heading page: the matching paragraph wanted:\n%.400s", got)
	}
}

func TestBoundManPage(t *testing.T) {
	var b strings.Builder
	b.WriteString("```\nRSYNC(1)                User Commands                RSYNC(1)\n\nNAME\n       rsync - a fast file-copying tool\n\n")
	for i := range 400 {
		fmt.Fprintf(&b, "       --option-%d\n              does thing %d to the files it copies, as the option says.\n\n", i, i)
	}
	b.WriteString("       --max-delete=NUM\n              don't delete more than NUM files; exit code 25 when it stops deletions.\n\n")
	for i := range 400 {
		fmt.Fprintf(&b, "       --other-%d\n              another option %d.\n\n", i, i)
	}
	b.WriteString("```\n")
	got := boundedAnswer(t, b.String(), "max-delete exit code", 4000)
	if !strings.Contains(got, "exit code 25") {
		t.Errorf("man page: the matching lines wanted:\n%.400s", got)
	}
}

func TestBoundOneHugeParagraph(t *testing.T) {
	words := make([]string, 12000)
	for i := range words {
		words[i] = fmt.Sprintf("word%d,", i)
	}
	words[9000] = "frobnicate"
	got := boundedAnswer(t, "# notes\n\n"+strings.Join(words, " ")+"\n", "frobnicate", 4000)
	if !strings.Contains(got, "frobnicate") || !strings.Contains(got, "…") {
		t.Errorf("one paragraph: cut at words around the term wanted:\n%.300s", got)
	}
}

// --section of a page that is one block pages it: every page within its
// bound, fences balanced, every line of the block on some page.
func TestSectionPagesOneBlock(t *testing.T) {
	const page = "https://manual.example.test/tool"
	body := lines(3000, 10, "    step %d: copy the next chunk of the input buffer", "    step 10: copy the next chunk of the input buffer")
	f := &fakeFetcher{pages: map[string]fakePage{page: {title: "tool", markdown: "# tool\n\n```sh\n" + strings.Join(body, "\n") + "\n```\n"}}}
	s, _, _ := searchFirstOver(t, f, nil)
	const tokens = 4000
	ctx := WithLocalOptions(context.Background(), LocalOptions{Hint: HintCLI})
	all := ""
	for n := 1; ; n++ {
		o := s.SectionSelect(ctx, "", page, tokens, n)
		if o.Err() != nil || len(o.Results) != 1 {
			t.Fatalf("page %d: %v", n, o.Err())
		}
		r := o.Results[0]
		if got := estimateTokens(RenderSnippet(r) + PageLine(r) + "\n" + NextPageLine(r) + "\n"); got > tokens {
			t.Errorf("page %d of %d estimates %d tokens", n, r.Pages, got)
		}
		balancedFences(t, r)
		all += r.Snippet + "\n"
		if r.Pages <= n {
			if n < 2 {
				t.Fatalf("one page for a block of %d tokens", estimateTokens(strings.Join(body, "\n")))
			}
			break
		}
	}
	for _, i := range []int{0, 1500, 2999} {
		if !strings.Contains(all, fmt.Sprintf("step %d:", i)) {
			t.Errorf("line %d on no page", i)
		}
	}
	// One line longer than a page pages at words.
	long := strings.Repeat("token ", 20000)
	f.pages[page] = fakePage{title: "tool", markdown: "# tool\n\n" + long + "\n"}
	s, _, _ = searchFirstOver(t, f, nil)
	for n := 1; n < 50; n++ {
		r := s.SectionSelect(ctx, "", page, tokens, n).Results[0]
		if got := estimateTokens(RenderSnippet(r) + PageLine(r) + "\n" + NextPageLine(r) + "\n"); got > tokens {
			t.Errorf("long line, page %d of %d estimates %d tokens", n, r.Pages, got)
		}
		if r.Pages <= n {
			break
		}
	}
}
