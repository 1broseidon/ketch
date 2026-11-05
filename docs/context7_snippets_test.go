package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/testutil"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files under testdata/context7_golden")

// Recorded /api/v2/context responses (served from testdata/context7 by the
// fixture transport) whose snippets interleave code and info by rank, carry
// several code blocks in one snippet, or hold ``` fences inside code.
var context7SnippetCases = []struct {
	name, library, query string
}{
	// code 1, 2 (two blocks); info 3, 4; code 5
	{"celery-retry", "/websites/celeryq_dev_en_stable", "celery retry a task on exception"},
	// info 1; code 2-5, APIDOC code containing ``` fences
	{"chartjs-responsive-container", "/websites/chartjs", "responsive chart resize with container height"},
	// info 1; code 2-5, one with python and css blocks
	{"textual-key-bindings", "/websites/textual_textualize_io", "bind keys to actions"},
}

// rawContext7Snippet is one snippet of a recorded response, in either array.
type rawContext7Snippet struct {
	rank                    int
	title, description, url string
	content                 string // info snippets
	blocks, languages       []string
}

// recordedContext7Snippets reads the fixture for a case and returns its
// snippets sorted by rank: the order Context7 means.
func recordedContext7Snippets(t *testing.T, library, query string) []rawContext7Snippet {
	t.Helper()
	ft := testutil.LoadFixtures(t, context7FixtureDir)
	resp, err := ft.Client().Get(context7Base + "/api/v2/context?libraryId=" + library + "&query=" + strings.ReplaceAll(query, " ", "+") + "&type=json&tokens=4000")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var dr context7DocsResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		t.Fatal(err)
	}
	var out []rawContext7Snippet
	for _, cs := range dr.CodeSnippets {
		s := rawContext7Snippet{rank: cs.Rank, title: cs.CodeTitle, description: cs.CodeDescription, url: cs.CodeID}
		for _, e := range cs.CodeList {
			s.blocks = append(s.blocks, e.Code)
			s.languages = append(s.languages, e.Language)
		}
		out = append(out, s)
	}
	for _, is := range dr.InfoSnippets {
		out = append(out, rawContext7Snippet{rank: is.Rank, title: is.Breadcrumb, url: is.PageID, content: is.Content})
	}
	slices.SortFunc(out, func(a, b rawContext7Snippet) int { return a.rank - b.rank })
	return out
}

// Each Context7 snippet is one result, in Context7's rank order across code
// and info snippets, with its description and every code block. ketch used
// to keep only a code snippet's first block, drop its description, and list
// every code snippet before every info snippet.
func TestContext7SnippetsRecorded(t *testing.T) {
	for _, tc := range context7SnippetCases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := recordedContext7(t, "")
			results, err := c.GetDocs(context.Background(), tc.library, tc.query, 4000)
			if err != nil {
				t.Fatal(err)
			}
			raw := recordedContext7Snippets(t, tc.library, tc.query)
			if len(results) != len(raw) {
				t.Fatalf("%d results, want one per snippet: %d", len(results), len(raw))
			}
			for i, want := range raw {
				r := results[i]
				if r.Title != want.title || r.URL != want.url || r.Library != tc.library || r.Source != "context7" {
					t.Errorf("result %d = %q %s, want rank %d: %q %s", i, r.Title, r.URL, want.rank, want.title, want.url)
				}
				if want.content != "" {
					if r.Snippet != want.content || r.Breadcrumb != want.title {
						t.Errorf("result %d: info snippet content or breadcrumb changed", i)
					}
					continue
				}
				checkCodeSnippet(t, i, r, want)
			}
			checkGolden(t, tc.name, results)
		})
	}
}

// checkCodeSnippet asserts code result i opens with the snippet's
// description, then holds every block, in order, fenced with its language.
func checkCodeSnippet(t *testing.T, i int, r Result, want rawContext7Snippet) {
	t.Helper()
	if !strings.HasPrefix(r.Snippet, strings.TrimSpace(want.description)+"\n\n") {
		t.Errorf("result %d snippet does not open with the description:\n%s", i, r.Snippet)
	}
	rest := r.Snippet
	for j, code := range want.blocks {
		open := "```" + want.languages[j] + "\n"
		at := strings.Index(rest, open)
		if at < 0 {
			t.Fatalf("result %d block %d: no %q fence after the previous block", i, j, open)
		}
		rest = rest[at+len(open):]
		body := strings.TrimRight(code, "\r\n")
		if !strings.HasPrefix(rest, body+"\n") {
			t.Fatalf("result %d block %d: code missing or out of order", i, j)
		}
		rest = rest[len(body):]
	}
}

// The fixtures cover what the cases are chosen for.
func TestContext7SnippetCasesShape(t *testing.T) {
	raw := recordedContext7Snippets(t, context7SnippetCases[0].library, context7SnippetCases[0].query)
	if raw[2].content == "" || raw[4].content != "" || len(raw[1].blocks) != 2 {
		t.Errorf("celery-retry: want info at rank 3 between code, and two blocks at rank 2")
	}
	raw = recordedContext7Snippets(t, context7SnippetCases[1].library, context7SnippetCases[1].query)
	if raw[0].content == "" {
		t.Errorf("chartjs: want an info snippet ranked first")
	}
	fenced := false
	for _, s := range raw {
		for _, b := range s.blocks {
			fenced = fenced || strings.Contains(b, "```")
		}
	}
	if !fenced {
		t.Errorf("chartjs: want a code block containing a ``` fence")
	}
}

func checkGolden(t *testing.T, name string, results []Result) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	path := filepath.Join("testdata", "context7_golden", name+".json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./docs -run TestContext7SnippetsRecorded -update)", err)
	}
	if string(got) != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("results differ from %s (rerun with -update and review the diff):\n%s", path, got)
	}
}

// A ``` fence inside code gets a longer fence around it, so the block
// stays one block; a block with no language takes the snippet's.
func TestContext7CodeMarkdown(t *testing.T) {
	got := context7CodeMarkdown(context7CodeSnippet{
		CodeDescription: "  Two blocks.  ",
		CodeLanguage:    "APIDOC",
		CodeList: []context7CodeEntry{
			{Language: "python", Code: "x = 1\n"},
			{Code: "## call\n```js\nf()\n```"},
			{Language: "go", Code: "  \n"},
		},
	})
	want := "Two blocks.\n\n```python\nx = 1\n```\n\n````APIDOC\n## call\n```js\nf()\n```\n````"
	if got != want {
		t.Errorf("markdown =\n%s\nwant\n%s", got, want)
	}
	if got := context7CodeMarkdown(context7CodeSnippet{CodeList: []context7CodeEntry{{Language: "sh", Code: "ls"}}}); got != "```sh\nls\n```" {
		t.Errorf("no description = %q", got)
	}
}

// Rank orders results across both arrays; a snippet without a rank comes
// after the ranked ones, in response order (code, then info).
func TestContext7ResultsOrder(t *testing.T) {
	titles := func(dr context7DocsResponse) []string {
		var out []string
		for _, r := range context7Results("/l", dr) {
			out = append(out, r.Title)
		}
		return out
	}
	dr := context7DocsResponse{
		CodeSnippets: []context7CodeSnippet{{CodeTitle: "c4", Rank: 4}, {CodeTitle: "c1", Rank: 1}, {CodeTitle: "cx"}},
		InfoSnippets: []context7InfoSnippet{{Breadcrumb: "i2", Rank: 2}, {Breadcrumb: "ix"}, {Breadcrumb: "i3", Rank: 3}},
	}
	if got, want := titles(dr), []string{"c1", "i2", "i3", "c4", "cx", "ix"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	unranked := context7DocsResponse{
		CodeSnippets: []context7CodeSnippet{{CodeTitle: "a"}, {CodeTitle: "b"}},
		InfoSnippets: []context7InfoSnippet{{Breadcrumb: "c"}},
	}
	if got, want := titles(unranked), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("unranked order = %v, want %v", got, want)
	}
	if context7Results("/l", context7DocsResponse{}) != nil {
		t.Error("an empty response is no results")
	}
}

// --limit keeps Context7's top-ranked snippets, info or code.
func TestContext7LimitKeepsTopRanked(t *testing.T) {
	c, _, _ := recordedContext7(t, "")
	tc := context7SnippetCases[1]
	results, err := c.GetDocs(context.Background(), tc.library, tc.query, 4000)
	if err != nil {
		t.Fatal(err)
	}
	top := Truncate(results, 2)
	if len(top) != 2 || top[0].Breadcrumb == "" || top[1].Breadcrumb != "" {
		t.Errorf("limit 2 = %+v, want the rank-1 info snippet then the rank-2 code snippet", top)
	}
}
