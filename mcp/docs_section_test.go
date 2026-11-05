package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
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

// A compact local answer ends each result it cut with the docs call that
// reads its section, and carries the section reference and truncated.
func TestDocsToolLocalCompactPointer(t *testing.T) {
	session := localSession(t)
	res, out := callDocsOutput(t, session, map[string]any{"query": "validate a form with a rules object", "backend": "local", "library": sectionRoot, "tokens": 120})
	if res.IsError || len(out.Results) == 0 {
		t.Fatalf("compact answer: %s", toolText(res))
	}
	r := out.Results[0]
	call := "full section: docs(backend=\"local\", section=\"" + r.Section + "\")"
	if !r.Truncated || r.Section != r.URL || !strings.HasSuffix(r.Snippet, "\n\n"+call) {
		t.Fatalf("cut result = %+v", r)
	}
	for _, r := range out.Results {
		if r.Truncated != strings.Contains(r.Snippet, "full section: docs(") {
			t.Errorf("truncated=%v, snippet:\n%s", r.Truncated, r.Snippet)
		}
	}
	// Without tokens there is no pointer.
	_, plain := callDocsOutput(t, session, map[string]any{"query": "validate a form with a rules object", "backend": "local", "library": sectionRoot})
	for _, r := range plain.Results {
		if strings.Contains(r.Snippet, "full section") || r.Section == "" {
			t.Errorf("default view result: %+v", r)
		}
	}
}

// The call a pointer names fetches the section in full, with no query, in
// the shape of a result.
func TestDocsToolLocalSectionFetch(t *testing.T) {
	session := localSession(t)
	ref := sectionRoot + "guide/forms#rules"
	res, full := callDocsOutput(t, session, map[string]any{"section": ref, "backend": "local"})
	if res.IsError || len(full.Results) != 1 || full.Backend != "local" || full.Status != docs.Answered {
		t.Fatalf("section fetch: %s %+v", toolText(res), full)
	}
	got := full.Results[0]
	if got.URL != ref || got.Section != ref || got.Source != "local" || got.Library != sectionRoot || got.Truncated ||
		!strings.Contains(got.Snippet, "name: (v) => (v.length < 2 ? 'Too short' : null),") || strings.Contains(got.Snippet, "full section") {
		t.Errorf("section result = %+v", got)
	}
}

// A page URL returns the page's sections, cut at a block with the next
// section named.
func TestDocsToolLocalSectionPage(t *testing.T) {
	session := localSession(t)
	res, page := callDocsOutput(t, session, map[string]any{"section": sectionRoot + "guide/forms", "tokens": 100})
	if res.IsError || len(page.Results) != 1 || !page.Results[0].Truncated || !strings.Contains(page.Results[0].Snippet, `next section: docs(backend="local", section="`+sectionRoot+"guide/forms#") {
		t.Errorf("page fetch: %s %+v", toolText(res), page)
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
		for _, want := range []string{"section field", "cut short", "no network", "[not_found]"} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("description lacks %q", want)
			}
		}
		return
	}
	t.Fatal("no docs tool")
}
