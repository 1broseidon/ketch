package mcp

import (
	"context"
	"strings"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsfetch"
	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DocsInput is the input schema for the "docs" tool.
type DocsInput struct {
	Query   string `json:"query" jsonschema:"the docs search query, or the library to resolve when resolve is true"`
	Backend string `json:"backend,omitempty" jsonschema:"docs backend (default: the configured backend)"`
	Library string `json:"library,omitempty" jsonschema:"library ID to fetch docs from directly, skipping the resolve step; requires a backend with library operations"`
	Tokens  int    `json:"tokens,omitempty" jsonschema:"library token budget when library is set (default 4000)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max number of results (default: the configured limit; with library set, unbounded unless given)"`
	Tag     string `json:"tag,omitempty" jsonschema:"record each result under this tag, retrievable later with the tag tool; not with resolve"`
	Resolve bool   `json:"resolve,omitempty" jsonschema:"resolve a library name to library IDs instead of searching docs"`
}

// DocsOutput is the output schema for the "docs" tool. Results is populated
// for a normal or library-scoped search; Matches is populated when Resolve
// is set. Exactly one of the two is non-empty for a given call. The result
// objects match the CLI's `ketch docs --json` (which emits a bare array; MCP
// structured content needs the object wrapper).
//
// Backend and Errors are set only by the auto backend: the provider that
// answered ("web" or "context7") and the one it fell through, by provider,
// as the search tool reports its auto chain. Any other backend leaves both
// out, so its output is unchanged.
type DocsOutput struct {
	Warnings []string            `json:"warnings,omitempty"`
	Results  []docs.Result       `json:"results,omitempty"`
	Matches  []docs.LibraryMatch `json:"matches,omitempty"`
	Backend  string              `json:"backend,omitempty"`
	Errors   map[string]string   `json:"errors,omitempty"`
}

// withSelection records an auto call's selection on out.
func (out DocsOutput) withSelection(sel docs.Selection) DocsOutput {
	out.Backend = sel.Backend
	if len(sel.Failures) > 0 {
		out.Errors = make(map[string]string, len(sel.Failures))
		for _, f := range sel.Failures {
			out.Errors[f.Backend] = f.Err.Error()
		}
	}
	return out
}

func (s *Server) registerDocsTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "docs",
		InputSchema: docsInputSchema(),
		Description: "Search library documentation using " + strings.Join(docs.ProviderNames(), ", ") + ". Supports resolving a library name to a " + strings.Join(docs.LibraryProviderNames(), ", ") + " library ID (resolve), and fetching docs directly from a known library ID (library)." +
			docs.WithLibraryHelp(" A library is a library ID from resolve") + ". Resolve a name once and pass the returned ID as library on later calls: a name is resolved again on every call." +
			" Context7 needs no key: without one it runs on Context7's anonymous quota (about 200 requests a month per IP; a spent quota is an [upstream] error naming when it resets), and a key lifts it." +
			" Backend auto tries web first and asks Context7 only when web identifies no library, finds several, or fails; Context7's pick must be a library the query names, and for an ambiguous name one of web's candidates. It reports the provider that answered in backend (each result's source says the same) and what it fell through in errors." +
			errTaxonomy,
		Annotations: readOnlyOpenWorld(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in DocsInput) (*mcpsdk.CallToolResult, DocsOutput, error) {
		if err := validResearchTag(in.Tag); err != nil {
			return nil, DocsOutput{}, err
		}
		ctx, diagnostics := withTagDiagnostics(ctx)
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
			// Library matches carry IDs, not URLs: there is nothing to
			// bookmark, so refuse rather than record nothing under the tag.
			if in.Tag != "" {
				return nil, DocsOutput{}, errf(kindValidation, "tag cannot be combined with resolve: library matches have no URL to bookmark")
			}
			return s.docsResolve(ctx, docs.ResolveBackend(backend), in.Query, limit)
		}

		if in.Library != "" {
			// Reject unsupported library operations, matching the CLI.
			if !docs.SupportsLibraries(backend) {
				return nil, DocsOutput{}, errf(kindValidation, "library requires the %s backend (got %q)", strings.Join(docs.LibraryBackends(), ", "), backend)
			}
			// Like the CLI, an explicit limit caps library docs; otherwise the
			// token budget is the only bound, so in.Limit is passed unresolved.
			return s.docsForLibrary(ctx, backend, in.Query, in.Library, tokens, in.Limit, in.Tag)
		}

		searcher, err := docs.NewFromConfig(s.cfg, backend, s.docsDeps(ctx))
		if err != nil {
			return nil, DocsOutput{}, backendErrf(err)
		}

		var results []docs.Result
		var sel docs.Selection
		if selecting, ok := searcher.(docs.SelectingSearcher); ok {
			results, sel, err = selecting.SearchSelect(ctx, in.Query, limit)
		} else {
			results, err = searcher.Search(ctx, in.Query, limit)
		}
		if err != nil {
			return nil, DocsOutput{}, upstreamErrf(err, "docs search failed")
		}

		s.recordResults(ctx, in.Tag, docsTagged(results))
		return nil, DocsOutput{Results: results, Warnings: diagnostics.values()}.withSelection(sel), nil
	})
}

// docsResolve maps a library name or spec to library IDs, returning at most
// limit matches. Each match goes out as the backend gave it, with whatever
// version, evidence and package specs it carries.
func (s *Server) docsResolve(ctx context.Context, backend, query string, limit int) (*mcpsdk.CallToolResult, DocsOutput, error) {
	resolver, err := s.libraryResolver(ctx, backend)
	if err != nil {
		return nil, DocsOutput{}, err
	}
	var matches []docs.LibraryMatch
	var sel docs.Selection
	if selecting, ok := resolver.(docs.SelectingSearcher); ok {
		matches, sel, err = selecting.ResolveSelect(ctx, query, limit)
	} else {
		matches, err = resolver.ResolveLibrary(ctx, query, limit)
	}
	if err != nil {
		return nil, DocsOutput{}, upstreamErrf(err, "resolve failed")
	}
	return nil, DocsOutput{Matches: matches}.withSelection(sel), nil
}

// docsForLibrary fetches docs for a known library ID, capped at limit
// results when limit is positive.
func (s *Server) docsForLibrary(ctx context.Context, backend, query, library string, tokens, limit int, tag string) (*mcpsdk.CallToolResult, DocsOutput, error) {
	resolver, err := s.libraryResolver(ctx, backend)
	if err != nil {
		return nil, DocsOutput{}, err
	}
	var results []docs.Result
	var sel docs.Selection
	if selecting, ok := resolver.(docs.SelectingSearcher); ok {
		results, sel, err = selecting.GetDocsSelect(ctx, library, query, tokens)
	} else {
		results, err = resolver.GetDocs(ctx, library, query, tokens)
	}
	if err != nil {
		return nil, DocsOutput{}, upstreamErrf(err, "docs fetch failed")
	}
	results = docs.Truncate(results, limit)
	s.recordResults(ctx, tag, docsTagged(results))
	out := DocsOutput{Results: results}.withSelection(sel)
	if d, ok := ctx.Value(tagDiagnosticsKey{}).(*tagDiagnostics); ok {
		out.Warnings = d.values()
	}
	return nil, out, nil
}

// libraryResolver constructs an optional docs capability through the registry.
func (s *Server) libraryResolver(ctx context.Context, backend string) (docs.LibraryResolver, error) {
	if p, ok := docs.Lookup(backend); ok && !p.Usable(s.cfg) && p.LibrarySetup != "" {
		return nil, errf(kindPrecondition, "%s", p.LibrarySetup)
	}
	searcher, err := docs.NewFromConfig(s.cfg, backend, s.docsDeps(ctx))
	if err != nil {
		return nil, backendErrf(err)
	}
	resolver, ok := searcher.(docs.LibraryResolver)
	if !ok {
		return nil, errf(kindValidation, "docs backend %q does not support library operations", backend)
	}
	return resolver, nil
}

// docsDeps hands a docs backend a page fetcher over the server's shared
// scraper and page cache, so docs pages get scrape's rewrites, cookies,
// browser fallback and caching. Building it per call is free: it holds the
// two shared handles and fetches nothing until the backend asks. Bookmark
// backfill failures from its cache writes go to the call's tag diagnostics,
// as on the other research tools.
func (s *Server) docsDeps(ctx context.Context) docs.Deps {
	return docs.Deps{Fetcher: docsfetch.New(s.scraper, s.tagPageCache(ctx, false))}
}

// docsInputSchema names the registered docs providers in the argument
// descriptions: which backends exist, which of them own library resolution
// and direct library lookup, and what each accepts as a library (its
// descriptor's LibraryHelp), so no description names a provider itself.
func docsInputSchema() *jsonschema.Schema {
	backends := docs.SelectableBackends()
	implemented := "implemented backends: " + configbase.JoinNames(backends)
	if len(backends) == 1 {
		implemented = backends[0] + " is the only implemented backend"
	}
	libNames := strings.Join(docs.LibraryProviderNames(), ", ")
	libIDs := configbase.JoinNames(docs.LibraryBackends())
	return inputSchema[DocsInput](map[string]string{
		"query":   docs.WithLibraryHelp("the docs search query (without library, the library is picked from it), or the library to resolve when resolve is true"),
		"backend": "docs backend (default: the configured backend); " + implemented,
		"library": docs.WithLibraryHelp(libNames + " library ID to fetch docs from directly, skipping the resolve step; requires the " + libIDs + " backend"),
		"tokens":  libNames + " token budget when library is set (default 4000)",
		"resolve": docs.WithLibraryHelp("resolve the query as a library name to " + libNames + " library IDs instead of searching docs"),
	})
}

// docsTagged maps docs hits onto index entries: library and heading make the
// title, the documentation chunk the description.
func docsTagged(results []docs.Result) []taggedResult {
	out := make([]taggedResult, 0, len(results))
	for _, r := range results {
		title := r.Title
		if r.Library != "" {
			title = r.Library + " " + title
		}
		out = append(out, taggedResult{URL: r.URL, Title: title, Description: r.Snippet})
	}
	return out
}
