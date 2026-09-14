package code

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Sourcegraph streams every match of a batch in one "matches" event. Popular
// symbols push that single data: line far past bufio.Scanner's 64KB default,
// which failed the whole query with "token too long" (provider audit,
// xcb_create_window).
func TestSourcegraphParsesOversizedMatchEvent(t *testing.T) {
	var matches []map[string]any
	line := strings.Repeat("x", 600)
	for i := 0; i < 400; i++ {
		matches = append(matches, map[string]any{
			"type": "content", "repository": fmt.Sprintf("github.com/org/repo%d", i), "path": "main.go",
			"language": "Go", "repoStars": i,
			"lineMatches": []map[string]any{{"line": line, "lineNumber": i + 1}},
		})
	}
	payload, err := json.Marshal(matches)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) < 2*bufio.MaxScanTokenSize {
		t.Fatalf("fixture too small to exercise the scanner limit: %d bytes", len(payload))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); !strings.Contains(q, "archived:no") || !strings.Contains(q, "xcb_create_window") {
			t.Errorf("query lost its term or safety qualifiers: %q", q)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: matches\ndata: %s\n\nevent: done\ndata: {}\n\n", payload)
	}))
	defer server.Close()

	results, err := NewSourcegraph(server.URL).Search(context.Background(), Query{Term: "xcb_create_window", Limit: 7})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 7 || results[0].Repo != "github.com/org/repo0" || results[0].Line != 1 || results[6].Repo != "github.com/org/repo6" {
		t.Fatalf("results = %d, first %+v", len(results), results[0])
	}
}
