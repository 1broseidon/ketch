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
	o := docs.Outcome{Status: docs.Found, Index: &docs.IndexInfo{Source: docs.SearchBackend, PagesRead: 3}, IndexedNear: near}
	out := fromOutcome(o)
	if len(out.IndexedNear) != 1 || out.IndexedNear[0] != near[0] || out.Hint != docs.NearHint(docs.HintMCP) {
		t.Errorf("out = near %v, hint %q", out.IndexedNear, out.Hint)
	}
	o.Results = []docs.Result{{URL: "https://x.example.test/a", Truncated: true}}
	if out := fromOutcome(o); !strings.HasPrefix(out.Hint, docs.AnswerHint(docs.HintMCP, 1)+"; ") || !strings.HasSuffix(out.Hint, docs.NearHint(docs.HintMCP)) {
		t.Errorf("hint with a cut result = %q", out.Hint)
	}
}

// An answer from a site ketch could not verify carries its origin, and
// the hint tells the caller how to present it.
func TestFromOutcomeOrigin(t *testing.T) {
	o := docs.Outcome{Status: docs.Found, Index: &docs.IndexInfo{Source: docs.SearchBackend, PagesRead: 3},
		Origin:  "unverified, ketch could not confirm tips.example.test as the official documentation of lib",
		Results: []docs.Result{{URL: "https://tips.example.test/a", Truncated: true}}}
	out := fromOutcome(o)
	want := `origin unverified: ketch could not confirm this site as the official documentation; name the site you cite, or pass the official docs with library="<docs URL>"`
	if out.Origin != o.Origin || out.Hint != docs.AnswerHint(docs.HintMCP, 1)+"; "+want || docs.OriginHint(docs.HintMCP, o) != want {
		t.Errorf("origin %q, hint %q", out.Origin, out.Hint)
	}
	o.Origin = "verified, the library value names this site"
	if out := fromOutcome(o); out.Origin != o.Origin || strings.Contains(out.Hint, "origin") {
		t.Errorf("verified: origin %q, hint %q", out.Origin, out.Hint)
	}
}
