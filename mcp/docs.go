package mcp

import (
	"context"
	"strings"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/configbase"
	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DocsInput is the input schema for the "docs" tool.
type DocsInput struct {
	Query   string `json:"query" jsonschema:"the docs search query, or a library name when resolve is true"`
	Backend string `json:"backend,omitempty" jsonschema:"docs backend (default: the configured backend)"`
	Library string `json:"library,omitempty" jsonschema:"library ID to fetch docs from directly, skipping the resolve step; requires a backend with library operations"`
	Tokens  int    `json:"tokens,omitempty" jsonschema:"library token budget when library is set (default 4000)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max number of results (default: the configured limit; with library set, unbounded unless given)"`
	Resolve bool   `json:"resolve,omitempty" jsonschema:"resolve a library name to library IDs instead of searching docs"`
}

// DocsOutput is the output schema for the "docs" tool. Results is populated
// for a normal or library-scoped search; Matches is populated when Resolve
// is set. Exactly one of the two is non-empty for a given call. The result
// objects match the CLI's `ketch docs --json` (which emits a bare array; MCP
// structured content needs the object wrapper).
type DocsOutput struct {
	Results []docs.Result       `json:"results,omitempty"`
	Matches []docs.LibraryMatch `json:"matches,omitempty"`
}

func (s *Server) registerDocsTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "docs",
		InputSchema: docsInputSchema(),
		Description: "Search library documentation using " + strings.Join(docs.ProviderNames(), ", ") + ". Supports resolving a library name to a " + strings.Join(docs.LibraryProviderNames(), ", ") + " library ID, and fetching docs directly from a known library ID." +
			errTaxonomy,
		Annotations: readOnlyOpenWorld(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in DocsInput) (*mcpsdk.CallToolResult, DocsOutput, error) {
		if in.Query == "" {
			return nil, DocsOutput{}, errf(kindValidation, "query is required")
		}
		backend := in.Backend
		if backend == "" {
			backend = s.cfg.DocsBackend
		}
		limit := in.Limit
		if limit <= 0 {
			limit = s.cfg.Limit
		}
		tokens := in.Tokens
		if tokens <= 0 {
			tokens = 4000
		}

		if in.Resolve {
			return s.docsResolve(ctx, docs.ResolveBackend(backend), in.Query, limit)
		}

		if in.Library != "" {
			// Reject unsupported library operations, matching the CLI.
			if !docs.SupportsLibraries(backend) {
				return nil, DocsOutput{}, errf(kindValidation, "library requires the %s backend (got %q)", strings.Join(docs.LibraryBackends(), ", "), backend)
			}
			// Like the CLI, an explicit limit caps library docs; otherwise the
			// token budget is the only bound, so in.Limit is passed unresolved.
			return s.docsForLibrary(ctx, backend, in.Query, in.Library, tokens, in.Limit)
		}

		searcher, err := docs.NewFromConfig(s.cfg, backend)
		if err != nil {
			return nil, DocsOutput{}, backendErrf(err, docs.ErrUnknownBackend)
		}

		results, err := searcher.Search(ctx, in.Query, limit)
		if err != nil {
			return nil, DocsOutput{}, upstreamErrf(err, "docs search failed")
		}

		return nil, DocsOutput{Results: results}, nil
	})
}

// docsResolve maps a free-form library name to Context7 library IDs,
// returning at most limit matches.
func (s *Server) docsResolve(ctx context.Context, backend, query string, limit int) (*mcpsdk.CallToolResult, DocsOutput, error) {
	resolver, err := s.libraryResolver(backend)
	if err != nil {
		return nil, DocsOutput{}, err
	}
	matches, err := resolver.ResolveLibrary(ctx, query, limit)
	if err != nil {
		return nil, DocsOutput{}, upstreamErrf(err, "resolve failed")
	}
	return nil, DocsOutput{Matches: matches}, nil
}

// docsForLibrary fetches docs for a known library ID, capped at limit
// results when limit is positive.
func (s *Server) docsForLibrary(ctx context.Context, backend, query, library string, tokens, limit int) (*mcpsdk.CallToolResult, DocsOutput, error) {
	resolver, err := s.libraryResolver(backend)
	if err != nil {
		return nil, DocsOutput{}, err
	}
	results, err := resolver.GetDocs(ctx, library, query, tokens)
	if err != nil {
		return nil, DocsOutput{}, upstreamErrf(err, "docs fetch failed")
	}
	return nil, DocsOutput{Results: docs.Truncate(results, limit)}, nil
}

// libraryResolver constructs an optional docs capability through the registry.
func (s *Server) libraryResolver(backend string) (docs.LibraryResolver, error) {
	if p, ok := docs.Lookup(backend); ok && !p.Usable(s.cfg) && p.LibrarySetup != "" {
		return nil, errf(kindPrecondition, "%s", p.LibrarySetup)
	}
	searcher, err := docs.NewFromConfig(s.cfg, backend)
	if err != nil {
		return nil, backendErrf(err, docs.ErrUnknownBackend)
	}
	resolver, ok := searcher.(docs.LibraryResolver)
	if !ok {
		return nil, errf(kindValidation, "docs backend %q does not support library operations", backend)
	}
	return resolver, nil
}

// docsInputSchema names the registered docs providers in the argument
// descriptions: which backends exist, and which of them own library
// resolution and direct library lookup.
func docsInputSchema() *jsonschema.Schema {
	backends := docs.AvailableBackends()
	implemented := "implemented backends: " + configbase.JoinNames(backends)
	if len(backends) == 1 {
		implemented = backends[0] + " is the only implemented backend"
	}
	libNames := strings.Join(docs.LibraryProviderNames(), ", ")
	libIDs := configbase.JoinNames(docs.LibraryBackends())
	return inputSchema[DocsInput](map[string]string{
		"backend": "docs backend (default: the configured backend); " + implemented,
		"library": libNames + " library ID to fetch docs from directly, skipping the resolve step; requires the " + libIDs + " backend",
		"tokens":  libNames + " token budget when library is set (default 4000)",
		"resolve": "resolve a library name to " + libNames + " library IDs instead of searching docs",
	})
}
