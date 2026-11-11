package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/testutil"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const sectionRoot = "https://docs.example.test/docs/"

// localSession is an MCP session over a small local index (text only, so
// ranked by keywords): one page with an H2 and two H3s, one too long for a
// small budget.
func localSession(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()
	session := docsSession(t, fixtureFallback{testutil.NewFixtureTransport()})
	path := filepath.Join(t.TempDir(), "docs-index.db")
	t.Setenv("KETCH_DOCS_INDEX_PATH", path)
	store := docsindex.Open(path)
	page := sectionRoot + "guide/forms"
	filler := strings.Repeat("Unrelated prose about the history of the project and its many contributors. ", 30)
	pages := []docsindex.PageData{{Page: docsindex.Page{URL: page, Title: "Forms", Hash: "f"}, Sections: []docsindex.Section{
		{Page: page, Order: 0, Heading: "Forms", Crumbs: []string{"Forms"}, Text: "Forms collect input."},
		{Page: page, Order: 1, Anchor: "validation", Heading: "Validation", Crumbs: []string{"Forms", "Validation"}, Text: "Validate a form with a rules object. " + filler},
		{Page: page, Order: 2, Anchor: "rules", Heading: "Rules", Crumbs: []string{"Forms", "Validation", "Rules"}, Text: "A rule is a function. " + filler + "\n\n```js\nname: (v) => (v.length < 2 ? 'Too short' : null),\n```"},
	}}}
	if err := store.PutPages(sectionRoot, pages); err != nil {
		t.Fatal(err)
	}
	lib := docsindex.Library{Key: sectionRoot, Library: "example", Names: []string{"example"}, Root: sectionRoot, Format: docsindex.FormatVersion, Built: time.Now().UTC(), Pages: 1}
	if err := store.PutLibrary(lib); err != nil {
		t.Fatal(err)
	}
	return session
}

func callDocsOutput(t *testing.T, session *mcpsdk.ClientSession, args map[string]any) (*mcpsdk.CallToolResult, DocsOutput) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "docs", Arguments: args})
	if err != nil {
		t.Fatalf("CallTool docs %v: %v", args, err)
	}
	var out DocsOutput
	if !res.IsError {
		data, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("docs %v structured content: %v", args, err)
		}
	}
	return res, out
}

// A local answer says once, in its hint, how to read a result it cut in
// full (the docs call), never in a result's text; each result carries its
// section reference and truncated, and the answer its frontmatter's
// fields.
func TestDocsToolLocalCompactPointer(t *testing.T) {
	session := localSession(t)
	res, out := callDocsOutput(t, session, map[string]any{"query": "validate a form with a rules object", "backend": "search", "library": sectionRoot, "tokens": 120})
	if res.IsError || len(out.Results) == 0 {
		t.Fatalf("compact answer: %s", toolText(res))
	}
	r := out.Results[0]
	if !r.Truncated || r.Section != r.URL || out.Hint != docs.AnswerHint(docs.HintMCP) {
		t.Fatalf("cut result = %+v, hint %q", r, out.Hint)
	}
	checkAnswerFields(t, out)
	// At the default budget, the hint is there only when a result is cut.
	_, plain := callDocsOutput(t, session, map[string]any{"query": "validate a form with a rules object", "library": sectionRoot})
	if (plain.Hint != "") != docs.AnyCut(plain.Results) {
		t.Errorf("default view: hint %q, results %+v", plain.Hint, plain.Results)
	}
	for _, r := range plain.Results {
		if strings.Contains(r.Snippet, "full section") || r.Section == "" {
			t.Errorf("default view result: %+v", r)
		}
	}
}

// A question none of whose terms the docs hold is answered by its
// not_found: status refused, no results, not a tool error.
func TestDocsToolLocalTermsRefusal(t *testing.T) {
	session := localSession(t)
	res, none := callDocsOutput(t, session, map[string]any{"query": "zyzzyva quokka", "library": sectionRoot})
	if res.IsError || none.Status != docs.Refused || none.Reason != docs.ReasonTermsNotFound || !slices.Equal(none.NotFound, []string{"zyzzyva", "quokka"}) || len(none.Results) != 0 {
		t.Errorf("refusal: %s %+v", toolText(res), none)
	}
}

// checkAnswerFields checks an indexed library's answer carries its
// frontmatter's fields, and its results their page titles and no pointer.
func checkAnswerFields(t *testing.T, out DocsOutput) {
	t.Helper()
	if out.Status != docs.Found || out.Library != sectionRoot || out.Index == nil || out.Index.Kind != docs.IndexFull || len(out.Terms) == 0 || out.NotFound == nil {
		t.Errorf("answer fields = %+v", out)
	}
	for _, r := range out.Results {
		if strings.Contains(r.Snippet, "full section") || r.PageTitle == "" {
			t.Errorf("result: %+v", r)
		}
	}
}

// The call a pointer names fetches the section in full, with no query, in
// the shape of a result.
func TestDocsToolLocalSectionFetch(t *testing.T) {
	session := localSession(t)
	ref := sectionRoot + "guide/forms#rules"
	res, full := callDocsOutput(t, session, map[string]any{"section": ref, "backend": "search"})
	if res.IsError || len(full.Results) != 1 || full.Backend != docs.SearchBackend || full.Status != docs.Found {
		t.Fatalf("section fetch: %s %+v", toolText(res), full)
	}
	got := full.Results[0]
	if got.URL != ref || got.Section != ref || got.Source != docs.SearchBackend || got.Library != sectionRoot || got.Truncated ||
		!strings.Contains(got.Snippet, "name: (v) => (v.length < 2 ? 'Too short' : null),") || strings.Contains(got.Snippet, "full section") {
		t.Errorf("section result = %+v", got)
	}
}

// A section longer than tokens is read a page at a time: the result
// says which page of how many, and the hint gives the call for the next
// page (page), with the page size given; the last page has none.
func TestDocsToolLocalSectionPage(t *testing.T) {
	session := localSession(t)
	ref := sectionRoot + "guide/forms"
	res, first := callDocsOutput(t, session, map[string]any{"section": ref, "tokens": 100})
	if res.IsError || len(first.Results) != 1 {
		t.Fatalf("page fetch: %s", toolText(res))
	}
	r := first.Results[0]
	next := `docs(section="` + ref + `", tokens=100, page=2)`
	if !r.Truncated || r.Page != 1 || r.Pages < 2 || r.Next != next || first.Hint != "next page: "+next {
		t.Fatalf("page 1: %s", toolText(res))
	}
	res, last := callDocsOutput(t, session, map[string]any{"section": ref, "tokens": 100, "page": r.Pages})
	if res.IsError || last.Results[0].Page != r.Pages || last.Results[0].Next != "" || last.Hint != "" || last.Results[0].Snippet == r.Snippet {
		t.Errorf("last page: %s", toolText(res))
	}
}

// A page past the last, a negative page, and a page without a section
// are [validation].
func TestDocsToolLocalSectionPageErrors(t *testing.T) {
	session := localSession(t)
	ref := sectionRoot + "guide/forms"
	_, first := callDocsOutput(t, session, map[string]any{"section": ref, "tokens": 100})
	if len(first.Results) != 1 {
		t.Fatal("no page")
	}
	for _, args := range []map[string]any{
		{"section": ref, "tokens": 100, "page": first.Results[0].Pages + 1},
		{"section": ref, "page": -1},
		{"query": "forms", "page": 2},
	} {
		if res, _ := callDocsOutput(t, session, args); !res.IsError || !strings.HasPrefix(toolText(res), "[validation]") {
			t.Errorf("docs %v: isError=%v %q, want [validation]", args, res.IsError, toolText(res))
		}
	}
}

// Following a pointer that names nothing is [not_found]; the section
// parameter needs the local backend; a query is not required with it.
func TestDocsToolLocalSectionErrors(t *testing.T) {
	session := localSession(t)
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"section": sectionRoot + "guide/forms#nope"}, "[not_found]"},
		{map[string]any{"section": sectionRoot + "guide/missing"}, "[not_found]"},
		{map[string]any{"section": "guide/forms#rules"}, "[validation]"},
		{map[string]any{"section": sectionRoot + "guide/forms#rules", "backend": "web"}, "[validation]"},
		{map[string]any{"section": sectionRoot + "guide/forms#rules", "library": "express"}, "[not_found]"},
	} {
		res, _ := callDocsOutput(t, session, c.args)
		if text := toolText(res); !res.IsError || !strings.HasPrefix(text, c.want) {
			t.Errorf("docs %v: isError=%v %q, want %s", c.args, res.IsError, text, c.want)
		}
	}
	if res, _ := callDocsOutput(t, session, map[string]any{}); !res.IsError || !strings.Contains(toolText(res), "query is required") {
		t.Errorf("no query and no section: %q", toolText(res))
	}
}

// The tool's schema offers section (not required, with query optional)
// and its description tells the agent when to use it.
func TestDocsToolDescribesSection(t *testing.T) {
	session := docsSession(t, http.DefaultTransport)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "docs" {
			continue
		}
		data, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Required   []string `json:"required"`
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(schema.Properties["section"].Description, "url#anchor") || len(schema.Required) != 0 {
			t.Errorf("section property %q, required %v", schema.Properties["section"].Description, schema.Required)
		}
		if !strings.Contains(schema.Properties["page"].Description, "with section") {
			t.Errorf("page property %q", schema.Properties["page"].Description)
		}
		for _, want := range []string{"section field", "hint", "not_found", "no network", "[not_found]", "page=N"} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("description lacks %q", want)
			}
		}
		return
	}
	t.Fatal("no docs tool")
}
