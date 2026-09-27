package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/internal/testutil"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A malformed repo is a [validation] error, raised before any backend is
// built or contacted.
func TestCodeToolRejectsMalformedRepo(t *testing.T) {
	testutil.SetIsolatedConfigHome(t)
	cfg := config.Defaults()
	srv, err := NewServer(&cfg, "test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)

	ctx := context.Background()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0"}, nil)
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, serverTransport) }()
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "code",
		Arguments: map[string]any{"query": "gcControllerState", "repo": "golang/go/src/runtime"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			text += tc.Text
		}
	}
	if !res.IsError || !strings.HasPrefix(text, "[validation] repo:") || !strings.Contains(text, "owner/name") {
		t.Fatalf("error = %q (isError %v), want a [validation] error naming the expected form", text, res.IsError)
	}
}

// A backend without the repository is [not_found] and points at the others,
// in the MCP spelling.
func TestCodeSearchErrfClassification(t *testing.T) {
	missing := fmt.Errorf("%w: facebook/react is not on https://sourcegraph.com", code.ErrRepoNotFound)
	if got := codeSearchErrf(missing, "sourcegraph").Error(); !strings.HasPrefix(got, "[not_found] ") || !strings.Contains(got, "try backend=grepapp or backend=github") {
		t.Errorf("missing repo = %q", got)
	}
	if got := codeSearchErrf(code.ErrRegexpUnsupported, "github").Error(); !strings.HasPrefix(got, "[validation] ") {
		t.Errorf("regexp unsupported = %q", got)
	}
}

func TestCodeWarnings(t *testing.T) {
	literal := code.Query{Term: "repo:1broseidon/ketch NewFromConfig", Limit: 5}
	got := codeWarnings("grepapp", literal, nil)
	if len(got) != 1 || !strings.HasPrefix(got[0], `[validation] grepapp searched "repo:1broseidon/ketch" as literal code`) || !strings.Contains(got[0], "repo/lang options") {
		t.Errorf("literal qualifier warnings = %q", got)
	}

	scoped := code.Query{Term: "reconcileChildFibers", Repo: "facebook/react", Limit: 5}
	got = codeWarnings("grepapp", scoped, nil)
	if len(got) != 1 || !strings.HasPrefix(got[0], "[not_found] no results in facebook/react") || !strings.Contains(got[0], "backend=sourcegraph or backend=github") {
		t.Errorf("empty repo warnings = %q", got)
	}
	if got := codeWarnings("grepapp", scoped, []code.Result{{Repo: "facebook/react"}}); got != nil {
		t.Errorf("results found, yet warned: %q", got)
	}
	if got := codeWarnings("sourcegraph", literal, nil); got != nil {
		t.Errorf("sourcegraph applies qualifiers, yet warned: %q", got)
	}
}
