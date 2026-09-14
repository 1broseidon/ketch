package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/search"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// argDescriptions lists each published tool's input-schema property
// descriptions as an agent would see them over tools/list.
func argDescriptions(t *testing.T) map[string]map[string]string {
	t.Helper()
	testutil.SetIsolatedConfigHome(t)
	cfg := config.Defaults()
	srv, err := NewServer(&cfg, "test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)

	ctx := context.Background()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0"}, nil)
	ct, st := mcpsdk.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, st) }()
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	out := make(map[string]map[string]string)
	for _, tool := range res.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("decode %s schema: %v", tool.Name, err)
		}
		out[tool.Name] = make(map[string]string, len(schema.Properties))
		for name, p := range schema.Properties {
			out[tool.Name][name] = p.Description
		}
	}
	return out
}

// Argument descriptions are where an agent looks for valid values. They must
// list the registered provider IDs, not just the tool-level display names.
func TestToolArgumentDescriptionsFollowRegistries(t *testing.T) {
	args := argDescriptions(t)

	want := func(tool, arg string, needles ...string) {
		t.Helper()
		got, ok := args[tool][arg]
		if !ok {
			t.Fatalf("%s.%s: property missing from published schema", tool, arg)
		}
		for _, n := range needles {
			if !strings.Contains(got, n) {
				t.Errorf("%s.%s description %q does not mention %q", tool, arg, got, n)
			}
		}
	}

	want("search", "backend", search.AvailableBackends()...)
	want("search", "backend", "(default: the configured backend)")
	want("code", "backend", code.AvailableBackends()...)
	want("code", "regexp", code.RegexpBackends()...)
	want("docs", "backend", docs.AvailableBackends()...)
	want("docs", "library", docs.LibraryBackends()...)
	want("docs", "library", docs.LibraryProviderNames()...)
	want("docs", "tokens", docs.LibraryProviderNames()...)
	want("docs", "resolve", docs.LibraryProviderNames()...)

	// Reflected properties untouched by the overrides must survive intact.
	want("search", "query", "the search query")
	want("code", "lang", "language filter")
	want("docs", "limit", "max number of results")
}

// A description key that names no property is a programming error and must
// fail at registration, not publish silently.
func TestInputSchemaRejectsUnknownProperty(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("inputSchema accepted an unknown property name")
		}
	}()
	inputSchema[SearchInput](map[string]string{"no_such_arg": "x"})
}
