//go:build mcpsmoke

// This smoke test exercises the real `ketch mcp serve` binary end to end
// over stdio, including live network calls (grepapp code search plus
// scrape/crawl against example.com). It is gated behind the mcpsmoke build
// tag so `go test ./...` stays hermetic in CI; run it explicitly with:
//
//	go test -tags mcpsmoke ./mcp/... -run TestMCPServerSmoke -v
//
// The server subprocess runs with HOME/XDG_CACHE_HOME/XDG_CONFIG_HOME pointed
// at a temp dir, so it sees default config (grepapp needs no key) and a fresh
// page cache — which lets the scrape subtest prove shared-cache reuse.
package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ketchmcp "github.com/1broseidon/ketch/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPServerSmoke(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "ketch-mcp-smoke")
	build := exec.Command("go", "build", "-o", bin, "..")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("failed to build ketch: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	isolated := t.TempDir()
	serve := exec.Command(bin, "mcp", "serve")
	serve.Env = append(os.Environ(),
		"HOME="+isolated,
		"XDG_CACHE_HOME="+isolated,
		"XDG_CONFIG_HOME="+isolated,
	)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "smoke-test", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.CommandTransport{Command: serve}, nil)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer session.Close()

	t.Run("InitializeInstructions", func(t *testing.T) {
		init := session.InitializeResult()
		if init == nil {
			t.Fatal("no initialize result on session")
		}
		if init.Instructions == "" {
			t.Fatal("initialize returned empty instructions")
		}
		// Pin the load-bearing content: tool routing, config-driven defaults,
		// the error taxonomy, and the output-size advice.
		for _, want := range []string{"search", "code", "docs", "scrape", "crawl", "ketch config", "[upstream]", "[precondition]", "max_chars"} {
			if !strings.Contains(init.Instructions, want) {
				t.Errorf("instructions missing %q", want)
			}
		}
		t.Logf("initialize instructions: %d bytes", len(init.Instructions))
	})

	t.Run("ToolsListWithAnnotations", func(t *testing.T) {
		toolsRes, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("list tools failed: %v", err)
		}
		byName := make(map[string]*mcpsdk.Tool, len(toolsRes.Tools))
		for _, tool := range toolsRes.Tools {
			byName[tool.Name] = tool
		}
		for _, want := range []string{"search", "code", "docs", "scrape", "crawl"} {
			tool, ok := byName[want]
			if !ok {
				t.Errorf("expected tool %q in tools/list, got: %v", want, toolNames(toolsRes.Tools))
				continue
			}
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Errorf("tool %q: expected ReadOnlyHint annotation, got %+v", want, tool.Annotations)
			}
			if tool.Annotations == nil || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
				t.Errorf("tool %q: expected OpenWorldHint=true annotation, got %+v", want, tool.Annotations)
			}
			if !strings.Contains(tool.Description, "[validation]") {
				t.Errorf("tool %q: description does not mention the error taxonomy", want)
			}
		}
		t.Logf("tools/list: %v (all with readOnlyHint + openWorldHint)", toolNames(toolsRes.Tools))
	})

	t.Run("CodeRoundTrip", func(t *testing.T) {
		// grep.app occasionally 504s; retry transient [upstream] failures a
		// couple of times before declaring the round-trip broken.
		var res *mcpsdk.CallToolResult
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			res, err = session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      "code",
				Arguments: map[string]any{"query": "func main", "lang": "go", "limit": 1},
			})
			if err != nil {
				t.Fatalf("call code tool failed: %v", err)
			}
			if !res.IsError || !strings.HasPrefix(resultText(res), "[upstream]") {
				break
			}
			t.Logf("attempt %d hit a transient upstream failure: %s", attempt, resultText(res))
			time.Sleep(2 * time.Second)
		}
		if res.IsError {
			t.Fatalf("code tool returned an error result: %s", resultText(res))
		}
		var out ketchmcp.CodeOutput
		mustStructured(t, res, &out)
		if len(out.Results) == 0 {
			t.Fatalf("code tool returned no results")
		}
		t.Logf("code result: %s %s", out.Results[0].Repo, out.Results[0].URL)
	})

	t.Run("ScrapeSharedCacheReuse", func(t *testing.T) {
		scrapeOnce := func() (ketchmcp.ScrapeOutput, time.Duration) {
			start := time.Now()
			res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      "scrape",
				Arguments: map[string]any{"url": "https://example.com"},
			})
			if err != nil {
				t.Fatalf("call scrape tool failed: %v", err)
			}
			if res.IsError {
				t.Fatalf("scrape tool returned an error result: %s", resultText(res))
			}
			var out ketchmcp.ScrapeOutput
			mustStructured(t, res, &out)
			return out, time.Since(start)
		}

		first, firstDur := scrapeOnce()
		if len(first.Results) != 1 || first.Results[0].Markdown == "" {
			t.Fatalf("first scrape: expected one result with markdown, got %+v", first)
		}

		// The server writes through its shared, server-lifetime cache handle;
		// with HOME/XDG_CACHE_HOME isolated, this file exists only if that handle
		// was actually opened and used.
		cachePath := filepath.Join(isolated, "ketch", "cache.db")
		info, err := os.Stat(cachePath)
		if err != nil {
			t.Fatalf("shared cache db not written after first scrape: %v", err)
		}
		t.Logf("first scrape: %v, cache db %d bytes", firstDur, info.Size())

		second, secondDur := scrapeOnce()
		if second.Results[0].Markdown != first.Results[0].Markdown {
			t.Fatalf("second scrape returned different markdown than first")
		}
		if secondDur >= firstDur {
			t.Errorf("second scrape (%v) not faster than first (%v) — expected a cache hit", secondDur, firstDur)
		}
		t.Logf("second scrape: %v (cache hit; first was %v)", secondDur, firstDur)
	})

	t.Run("CrawlBounded", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      "crawl",
			Arguments: map[string]any{"url": "https://example.com", "max_pages": 2},
		})
		if err != nil {
			t.Fatalf("call crawl tool failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("crawl tool returned an error result: %s", resultText(res))
		}
		var out ketchmcp.CrawlOutput
		mustStructured(t, res, &out)
		if len(out.Pages) < 1 || len(out.Pages) > 2 {
			t.Fatalf("crawl: expected 1-2 pages with max_pages=2, got %d", len(out.Pages))
		}
		t.Logf("crawl: %d page(s), stopped=%q, first: %s (%q)", len(out.Pages), out.Stopped, out.Pages[0].URL, out.Pages[0].Title)
	})

	t.Run("DocsLibraryWrongBackendValidation", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      "docs",
			Arguments: map[string]any{"query": "routing", "backend": "local", "library": "/vercel/next.js"},
		})
		if err != nil {
			t.Fatalf("call docs tool failed: %v", err)
		}
		if !res.IsError {
			t.Fatalf("docs with library + non-context7 backend: expected an error result")
		}
		text := resultText(res)
		if !strings.HasPrefix(text, "[validation]") {
			t.Errorf("docs error missing [validation] prefix: %q", text)
		}
		if !strings.Contains(text, "library requires the context7 backend") {
			t.Errorf("docs error missing clear message: %q", text)
		}
		t.Logf("docs validation error: %s", text)
	})
}

// mustStructured re-marshals a tool result's structured content into out.
func mustStructured(t *testing.T, res *mcpsdk.CallToolResult, out any) {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
}

// resultText concatenates the text content blocks of a tool result.
func resultText(res *mcpsdk.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func toolNames(tools []*mcpsdk.Tool) []string {
	out := make([]string, len(tools))
	for i, tool := range tools {
		out[i] = tool.Name
	}
	return out
}
