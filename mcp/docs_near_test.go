package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/docs"
)

// An answer to a library value that named no indexed library carries the
// indexed libraries near it, and the hint says to pass one as library.
func TestFromOutcomeIndexedNear(t *testing.T) {
	near := []docs.NearLibrary{{Title: "Cisco Duo", Library: "https://duo.example.test/docs/"}}
	o := docs.Outcome{Status: docs.Found, Read: []docs.PageRead{{URL: "https://x.example.test/a", Rank: 1}}, IndexedNear: near}
	out := fromOutcome(o)
	const pass = "pass one of the indexed_near roots as library"
	if len(out.IndexedNear) != 1 || out.IndexedNear[0] != near[0] || out.Hint != docs.Hint(docs.HintMCP, o) || !strings.Contains(out.Hint, pass) {
		t.Errorf("out = near %v, hint %q", out.IndexedNear, out.Hint)
	}
	o.Results = []docs.Result{{URL: "https://x.example.test/a", Truncated: true}}
	if out := fromOutcome(o); !strings.Contains(out.Hint, `docs(section="<url#anchor>")`) || !strings.HasSuffix(out.Hint, pass) || strings.Contains(out.Hint, "ketch docs") {
		t.Errorf("hint with a cut result = %q", out.Hint)
	}
}

// An answer from pages a search read carries its origin only when it says
// something (docs.ShownOrigin): verified; an unverified origin on a
// request that named no library is left out, and no hint speaks of it.
func TestFromOutcomeOrigin(t *testing.T) {
	o := docs.Outcome{Status: docs.Found, Read: []docs.PageRead{{URL: "https://tips.example.test/a", Rank: 1}},
		Origin:  "unverified, ketch could not confirm tips.example.test as the official documentation of lib",
		Results: []docs.Result{{URL: "https://tips.example.test/a", Truncated: true}}}
	out := fromOutcome(o)
	if out.Origin != "" || strings.Contains(out.Hint, "origin") || out.Hint != docs.Hint(docs.HintMCP, o) {
		t.Errorf("unverified: origin %q, hint %q", out.Origin, out.Hint)
	}
	o.Origin = "verified, the library value names this site"
	if out := fromOutcome(o); out.Origin != o.Origin || strings.Contains(out.Hint, "origin") {
		t.Errorf("verified: origin %q, hint %q", out.Origin, out.Hint)
	}
}

// The docs tool's output names an answer's fields as --json does: what
// was read (read, skipped, widened), the terms the source lacks
// (not_on_pages for pages read, not_in_index for an indexed library), the
// outline, the other results and the hint in the tool's words; no
// status, reason, terms or not_found, and no backend for search.
func TestDocsOutputFieldNames(t *testing.T) {
	o := docs.Outcome{Status: docs.Partial, Reason: docs.ReasonTermsNotFound,
		Read:         []docs.PageRead{{URL: "https://x.example.test/a", Rank: 1}},
		Skipped:      []docs.PageSkipped{{URL: "https://x.example.test/b", Cause: "not found"}},
		Widened:      &docs.Widening{Pages: 2, Root: "https://x.example.test/", Terms: []string{"zorp"}},
		NotFound:     []string{"zorp"},
		Origin:       "verified, the search results name it",
		Results:      []docs.Result{{URL: "https://x.example.test/a#s", Snippet: "Text.", Truncated: true}},
		Outline:      []docs.OutlineEntry{{Heading: "T", Section: "https://x.example.test/a#t", Tokens: 9}},
		OutlineMore:  []docs.OutlineMore{{Page: "https://x.example.test/a", Count: 3}},
		OtherResults: []docs.OtherResult{{Title: "C", URL: "https://x.example.test/c", Rank: 3}},
	}
	b, err := json.Marshal(fromOutcome(o))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(b, &top)
	for _, k := range []string{"read", "skipped", "widened", "not_on_pages", "origin", "results", "outline", "outline_more", "other_results", "hint"} {
		if _, ok := top[k]; !ok {
			t.Errorf("no %q in %s", k, b)
		}
	}
	for _, k := range []string{"status", "reason", "terms", "not_found", "not_in_index", "backend", "index"} {
		if _, ok := top[k]; ok {
			t.Errorf("%q in %s", k, b)
		}
	}
	if h := fromOutcome(o).Hint; !strings.Contains(h, `docs(section="<url#anchor>")`) || !strings.Contains(h, `docs(library="<url>", query="<your question>")`) || strings.Contains(h, "ketch ") {
		t.Errorf("hint %q, want the tool's words", h)
	}
	indexed := docs.Outcome{Status: docs.Found, Index: &docs.IndexInfo{Kind: docs.IndexFull, Pages: 3}}
	b, _ = json.Marshal(fromOutcome(indexed))
	if !strings.Contains(string(b), `"not_in_index":[]`) || strings.Contains(string(b), "not_on_pages") || !strings.Contains(string(b), `"index":{"kind":"full","pages":3}`) {
		t.Errorf("indexed: %s", b)
	}
	if out := fromOutcome(docs.Outcome{Status: docs.Found, Backend: "context7", Results: []docs.Result{{URL: "u"}}}); out.Backend != "context7" {
		t.Errorf("context7 backend %q", out.Backend)
	}
}
