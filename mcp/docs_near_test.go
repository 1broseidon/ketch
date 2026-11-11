package mcp

import (
	"strings"
	"testing"

	"github.com/1broseidon/ketch/docs"
)

// An answer to a library value that named no indexed library carries the
// indexed libraries near it, and the hint says to pass one as library.
func TestFromOutcomeIndexedNear(t *testing.T) {
	near := []docs.NearLibrary{{Title: "Cisco Duo", Library: "https://duo.example.test/docs/"}}
	o := docs.Outcome{Status: docs.Partial, Reason: docs.ReasonPartialSite, Index: &docs.IndexInfo{Source: docs.SearchBackend, PagesRead: 3}, IndexedNear: near}
	out := fromOutcome(o)
	if len(out.IndexedNear) != 1 || out.IndexedNear[0] != near[0] || out.Hint != docs.NearHint(docs.HintMCP) {
		t.Errorf("out = near %v, hint %q", out.IndexedNear, out.Hint)
	}
	o.Results = []docs.Result{{URL: "https://x.example.test/a", Truncated: true}}
	if out := fromOutcome(o); !strings.HasPrefix(out.Hint, docs.AnswerHint(docs.HintMCP)+"; ") || !strings.HasSuffix(out.Hint, docs.NearHint(docs.HintMCP)) {
		t.Errorf("hint with a cut result = %q", out.Hint)
	}
}
