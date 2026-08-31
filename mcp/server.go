// Package mcp exposes ketch's search, code, docs, scrape, and crawl
// capabilities as Model Context Protocol (MCP) tools (prunable to a subset
// via the mcp_tools config key). Each tool adapter calls
// the same underlying packages (search, code, docs, scrape, crawl) the Cobra
// commands in cmd/ call, through the same config-driven constructors
// (search.NewFromConfig etc.), and resolves backends from the same
// *config.Config an agent's human counterpart configures via
// `ketch config set`.
//
// The go-sdk dispatches tool calls concurrently, so everything with process
// lifetime (the headless-browser scraper, the bbolt page cache, the compiled
// URL rewriter) is constructed once in NewServer, shared by all calls, and
// released in Close — never per call.
package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/scrape"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolMeta pairs a tool's prose with its name: entryClause builds the
// "<name> (<what>)" segment in the instructions' enumeration line, guidance
// builds the "<name> for …" clause in the routing sentence.
type toolMeta struct {
	desc     string
	guidance string
}

var toolProse = map[string]toolMeta{
	"search": {"web search", "search for the open web"},
	"code":   {"grep public OSS repos for real-world usage", "code for code examples"},
	"docs":   {"curated library/API documentation via Context7", "docs for library references"},
	"scrape": {"fetch URLs as clean markdown", "scrape when you already have the URL"},
	"crawl":  {"bounded same-host multi-page crawl", "crawl only when one page is not enough"},
}

// buildServerInstructions returns the initialize-result instructions for the
// published tool set. Every tool published reproduces the historic static
// instructions byte-for-byte; a pruned set describes only what exists, so
// agents are never routed to tools the server won't answer.
func buildServerInstructions(tools []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ketch provides %s read-only research tool%s: ", countWord(len(tools)), plural(len(tools), "", "s"))
	writeClauses(&b, tools, func(name string) string {
		return fmt.Sprintf("%s (%s)", name, toolProse[name].desc)
	})
	b.WriteString(".\nPrefer ")
	writeClauses(&b, tools, func(name string) string { return toolProse[name].guidance })
	b.WriteString(".\n")
	b.WriteString(`Backend defaults and API keys come from the operator's ketch config; omit the backend argument to use them (on the CLI, ` + "`ketch config`" + ` shows the effective settings).
Tool errors start with a stable prefix: [validation] and [not_found] mean fix your input (retrying unchanged will not help); [upstream] is a backend/network failure where retrying may help; [precondition] means the operator must configure something (e.g. an API key or browser); [cancelled] means the call was cancelled or timed out.
`)
	writeSizeAdvice(&b, tools)
	// The historic static text ended without a trailing newline.
	return strings.TrimSuffix(b.String(), "\n")
}

// has reports whether names contains name.
func has(names []string, name string) bool {
	return slices.Contains(names, name)
}

// writeSizeAdvice appends the output-bounding sentence for the enabled
// tools. scrape and search both fetch page content (scrape directly, search
// via its scrape option) and take max_chars and trim, so they share the full
// sentence; crawl takes a per-page max_chars but no trim, so it gets a
// variant. Tools that return bounded output (code, docs) get nothing — the
// historic static text did likewise.
func writeSizeAdvice(b *strings.Builder, tools []string) {
	if has(tools, "scrape") || has(tools, "search") {
		b.WriteString(`When scraping unknown or potentially large pages, set max_chars (and optionally trim) to bound the response size.
`)
		return
	}
	if has(tools, "crawl") {
		b.WriteString(`When crawling, set max_chars to bound the response size.
`)
	}
}

// writeClauses writes clauses joined English-style: "a, b, and c", "a and b",
// or just "a" — the all-five join shapes the static instructions text.
func writeClauses(b *strings.Builder, names []string, clause func(string) string) {
	for i, name := range names {
		switch {
		case i == 0:
			b.WriteString(clause(name))
		case i == len(names)-1 && len(names) == 2:
			fmt.Fprintf(b, " and %s", clause(name))
		case i == len(names)-1:
			fmt.Fprintf(b, ", and %s", clause(name))
		default:
			fmt.Fprintf(b, ", %s", clause(name))
		}
	}
}

// countWord and plural shape the first line's prose: "five research tools",
// but "one research tool".
func countWord(n int) string {
	return []string{"zero", "one", "two", "three", "four", "five"}[n]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Server bundles the SDK server with the shared, server-lifetime resources
// the tool handlers use. Construct with NewServer, run with Run, and always
// Close when done (it shuts down the headless browser and releases the cache
// file lock).
type Server struct {
	cfg     *config.Config
	tools   []string // canonical published set; every tool when unconfigured
	mcp     *mcpsdk.Server
	scraper *scrape.Scraper // one scraper (and lazy browser conn) for all calls
	cache   *cache.Cache    // one bbolt handle for all calls; nil if unavailable
}

// NewServer builds an MCP server named "ketch" exposing the search, code,
// docs, scrape, and crawl tools, backed by cfg for backend selection and API
// keys. Background crawls, cache admin, and config stay CLI-only.
//
// cfg.MCPTools — the mcp_tools config key — is an allowlist over the
// published tools: an empty list publishes all five, a configured list
// publishes exactly those tools and omits the rest from both tools/list and
// the initialize instructions.
//
// The returned error is a precondition failure (invalid url_rewrites or
// mcp_tools config).
// A nil cache (e.g. another long-lived process holds the bbolt lock) is not
// an error: the server runs with caching disabled, exactly like the CLI.
func NewServer(cfg *config.Config, version string) (*Server, error) {
	scraper, err := scrape.NewFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	tools, err := config.NormalizeMCPTools(cfg.MCPTools)
	if err != nil {
		return nil, err
	}
	if len(tools) == 0 {
		tools = config.MCPToolNames() // no allowlist: publish everything
	}

	s := &Server{
		cfg:     cfg,
		tools:   tools,
		scraper: scraper,
		cache:   cache.NewFromConfig(cfg),
		mcp: mcpsdk.NewServer(&mcpsdk.Implementation{
			Name:    "ketch",
			Version: version,
		}, &mcpsdk.ServerOptions{
			Instructions: buildServerInstructions(tools),
		}),
	}

	s.registerTools()

	return s, nil
}

// registerTools registers each published tool. Tools not in the operator's
// mcp_tools allowlist are never registered — pruned servers do not advertise
// (much less answer) them.
func (s *Server) registerTools() {
	for _, name := range s.tools {
		switch name {
		case "search":
			s.registerSearchTool()
		case "code":
			s.registerCodeTool()
		case "docs":
			s.registerDocsTool()
		case "scrape":
			s.registerScrapeTool()
		case "crawl":
			s.registerCrawlTool()
		}
	}
}

// Run runs the server over the given transport until the client disconnects
// or ctx is cancelled. Call Close afterwards to release shared resources.
func (s *Server) Run(ctx context.Context, t mcpsdk.Transport) error {
	return s.mcp.Run(ctx, t)
}

// Close releases the server-lifetime resources: kills the headless browser
// (if one was launched) and closes the page cache. Safe to call once Run has
// returned; both underlying Closes are nil-safe.
func (s *Server) Close() {
	s.scraper.Close()
	s.cache.Close()
}

// pageCache returns the shared cache handle, or nil when the caller asked to
// bypass caching for this call.
func (s *Server) pageCache(noCache bool) *cache.Cache {
	if noCache {
		return nil
	}
	return s.cache
}

// readOnlyOpenWorld marks a tool as a non-mutating fetcher that talks to the
// open web. All ketch tools are read-only network fetchers.
func readOnlyOpenWorld() *mcpsdk.ToolAnnotations {
	openWorld := true
	return &mcpsdk.ToolAnnotations{
		ReadOnlyHint:  true,
		OpenWorldHint: &openWorld,
	}
}
