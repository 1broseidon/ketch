package code

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// grepBlock renders one repository block as mcp.grep.app returns it.
func grepBlock(repo, path, line string) string {
	return fmt.Sprintf("Repository: %s\nPath: %s\nURL: https://github.com/%s/blob/master/%s\nLicense: BSD-3-Clause\n\nSnippets:\n--- Snippet 1 (Line 12) ---\n%s\n",
		repo, path, repo, path, line)
}

// grepServer answers every searchGitHub call with blocks and records the
// arguments of the last one.
func grepServer(t *testing.T, blocks ...string) (*GrepApp, *map[string]any) {
	t.Helper()
	var args map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		args = req.Params.Arguments
		content := make([]map[string]string, 0, len(blocks))
		for _, b := range blocks {
			content = append(content, map[string]string{"type": "text", "text": b})
		}
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": content, "isError": false}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
	}))
	t.Cleanup(server.Close)
	return &GrepApp{endpoint: server.URL, client: server.Client()}, &args
}

// grep.app's repo argument matches every repository whose name contains the
// value: golang/go also returns golang/gofrontend. Only the named repository
// may come back, whatever case the caller used.
func TestGrepAppRepoFilterIsExact(t *testing.T) {
	g, args := grepServer(t,
		grepBlock("golang/go", "src/runtime/mgcpacer.go", "var gcController gcControllerState"),
		grepBlock("golang/gofrontend", "libgo/go/runtime/mgcpacer.go", "var gcController gcControllerState"),
		grepBlock("golang/go", "src/runtime/proc.go", "gcControllerState.findRunnableGCWorker"),
	)

	results, err := g.Search(context.Background(), Query{Term: "gcControllerState", Repo: "https://github.com/Golang/Go", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := (*args)["repo"]; got != "Golang/Go" {
		t.Errorf("repo argument = %v, want the normalized owner/name", got)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want the two golang/go hits", results)
	}
	for _, r := range results {
		if r.Repo != "golang/go" {
			t.Errorf("result from %s leaked through the golang/go filter", r.Repo)
		}
	}
}

func TestGrepAppWithoutRepoKeepsEveryRepository(t *testing.T) {
	g, args := grepServer(t,
		grepBlock("golang/go", "src/runtime/mgcpacer.go", "gcControllerState"),
		grepBlock("golang/gofrontend", "libgo/go/runtime/mgcpacer.go", "gcControllerState"),
	)

	results, err := g.Search(context.Background(), Query{Term: "gcControllerState", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, sent := (*args)["repo"]; sent {
		t.Errorf("repo argument sent without a filter: %v", *args)
	}
	if len(results) != 2 || !strings.HasSuffix(results[1].Repo, "gofrontend") {
		t.Fatalf("results = %+v, want both repositories", results)
	}
}
