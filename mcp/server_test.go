package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/internal/testutil"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// allFiveInstructions is the static instructions text mcp/server.go shipped
// before tool pruning (v0.14.0). With every tool published, the generated
// text must reproduce it byte-for-byte.
const allFiveInstructions = `ketch provides five read-only research tools: search (web search), code (grep public OSS repos for real-world usage), docs (curated library/API documentation via Context7), scrape (fetch URLs as clean markdown), and crawl (bounded same-host multi-page crawl).
Prefer search for the open web, code for code examples, docs for library references, scrape when you already have the URL, and crawl only when one page is not enough.
Backend defaults and API keys come from the operator's ketch config; omit the backend argument to use them (on the CLI, ` + "`ketch config`" + ` shows the effective settings).
Tool errors start with a stable prefix: [validation] and [not_found] mean fix your input (retrying unchanged will not help); [upstream] is a backend/network failure where retrying may help; [precondition] means the operator must configure something (e.g. an API key or browser); [cancelled] means the call was cancelled or timed out.
When scraping unknown or potentially large pages, set max_chars (and optionally trim) to bound the response size.`

func TestBuildServerInstructionsAllFive(t *testing.T) {
	got := buildServerInstructions([]string{"search", "code", "docs", "scrape", "crawl"})
	if got != allFiveInstructions {
		t.Errorf("all-five instructions drifted from the v0.14.0 static text:\n got: %q\nwant: %q", got, allFiveInstructions)
	}
}

func TestBuildServerInstructionsPruned(t *testing.T) {
	cases := []struct {
		name   string
		tools  []string
		want   string
		absent []string
	}{
		{
			name:   "two tools route with an 'and', not an Oxford comma",
			tools:  []string{"search", "scrape"},
			want:   "Prefer search for the open web and scrape when you already have the URL",
			absent: []string{"code", "crawl", "docs"},
		},
		{
			name:   "one tool reads singular",
			tools:  []string{"search"},
			want:   "ketch provides one read-only research tool: search (web search).\nPrefer search for the open web.",
			absent: []string{"scrape", "max_chars"},
		},
		{
			name:   "scrape-only keeps the max_chars advice",
			tools:  []string{"scrape"},
			want:   "set max_chars (and optionally trim)",
			absent: []string{"search (web search)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildServerInstructions(tc.tools)
			if !strings.Contains(got, tc.want) {
				t.Errorf("instructions missing %q:\n%s", tc.want, got)
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Errorf("instructions for %v mention pruned tool: %q", tc.tools, absent)
				}
			}
		})
	}
}

func TestNewServerRejectsInvalidTools(t *testing.T) {
	cfg := config.Defaults()
	cfg.MCPTools = []string{"nope"}
	if _, err := NewServer(&cfg, "test"); err == nil {
		t.Fatal("expected NewServer to fail on an invalid mcp_tools config")
	} else if !strings.Contains(err.Error(), "valid: search, code, docs, scrape, crawl") {
		t.Fatalf("error should name the valid tools, got: %v", err)
	}
}

// newPublishedTools connects an in-memory client to a NewServer built from a
// modified config.Defaults() and returns the published tool names plus the
// initialize instructions. The isolated config home keeps the bbolt page
// cache off the developer's real cache directory.
func newPublishedTools(t *testing.T, mutate func(*config.Config)) ([]string, string) {
	t.Helper()
	testutil.SetIsolatedConfigHome(t)
	cfg := config.Defaults()
	if mutate != nil {
		mutate(&cfg)
	}
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

	toolsRes, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(toolsRes.Tools))
	for _, tool := range toolsRes.Tools {
		names = append(names, tool.Name)
	}
	init := session.InitializeResult()
	if init == nil {
		t.Fatal("no initialize result on session")
	}
	return names, init.Instructions
}

func TestNewServerPrunesTools(t *testing.T) {
	names, instructions := newPublishedTools(t, func(c *config.Config) {
		c.MCPTools = []string{"search", "crawl"}
	})
	joined := strings.Join(names, ",")
	if joined != "crawl,search" && joined != "search,crawl" {
		// tools/list order is the SDK's, not ours; the contract is the set.
		t.Errorf("published tools = [%s], want exactly {search, crawl}", joined)
	}
	// The instructions must route only to what exists.
	if !strings.Contains(instructions, "search (web search)") || !strings.Contains(instructions, "crawl (bounded same-host multi-page crawl)") {
		t.Errorf("instructions missing enabled tools:\n%s", instructions)
	}
	for _, absent := range []string{"code", "docs", "scrape", "max_chars"} {
		if strings.Contains(instructions, absent) {
			t.Errorf("instructions mention pruned tool text %q:\n%s", absent, instructions)
		}
	}
	// Every tool published (the default) keeps the server whole.
	namesAll, _ := newPublishedTools(t, nil)
	if len(namesAll) != 5 {
		t.Errorf("published tools with no allowlist = [%s], want all five", strings.Join(namesAll, ","))
	}
	set := map[string]bool{}
	for _, n := range namesAll {
		set[n] = true
	}
	for _, want := range []string{"search", "code", "docs", "scrape", "crawl"} {
		if !set[want] {
			t.Errorf("unconfigured server missing tool %q", want)
		}
	}
}
