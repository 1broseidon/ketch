package code

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

// Sourcegraph's repo: filter is an unanchored regexp over the full name, so a
// bare golang/go also matches golang/gofrontend and studygolang/gophers. The
// filter must be anchored and escaped, and the archived/fork defaults must
// not hide the repository the caller named.
func TestSourcegraphRepoFilterIsExact(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: done\ndata: {}\n\n")
	}))
	defer server.Close()

	if _, err := NewSourcegraph(server.URL).Search(context.Background(), Query{Term: "useRouter", Lang: "typescript", Repo: "github.com/vercel/next.js", Limit: 5}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if want := `useRouter lang:typescript repo:(^|/)vercel/next\.js$ archived:yes fork:yes`; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}

	if _, err := NewSourcegraph(server.URL).Search(context.Background(), Query{Term: "useRouter", Limit: 5}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if want := "useRouter archived:no fork:no"; got != want {
		t.Errorf("open search query = %q, want the safety defaults %q", got, want)
	}
}

// When no repository matches, Sourcegraph answers with an alert and no
// matches. For a named repository that is ErrRepoNotFound — another backend
// may have it — rather than an empty result that reads as "no such code".
func TestSourcegraphMissingRepoIsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: progress\ndata: {\"matchCount\":0}\n\n")
		fmt.Fprint(w, "event: alert\ndata: {\"title\":\"No repositories found\",\"description\":\"Try using a different `repo:<regexp>` filter to see results\"}\n\n")
		fmt.Fprint(w, "event: done\ndata: {}\n\n")
	}))
	defer server.Close()

	_, err := NewSourcegraph(server.URL).Search(context.Background(), Query{Term: "reconcileChildFibers", Repo: "facebook/react", Limit: 5})
	if !errors.Is(err, ErrRepoNotFound) || !strings.Contains(err.Error(), "facebook/react") || !strings.Contains(err.Error(), server.URL) {
		t.Fatalf("err = %v, want ErrRepoNotFound naming the repository and instance", err)
	}

	// Without a named repository the alert stays advisory: empty, no error.
	results, err := NewSourcegraph(server.URL).Search(context.Background(), Query{Term: "reconcileChildFibers", Limit: 5})
	if err != nil || len(results) != 0 {
		t.Fatalf("open search = %v, %v; want no results and no error", results, err)
	}
}
