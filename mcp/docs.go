package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsfetch"
	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DocsInput is the input schema for the "docs" tool.
type DocsInput struct {
	Query   string `json:"query,omitempty" jsonschema:"the docs search query, or the library to resolve when resolve is true; not needed with section"`
	Backend string `json:"backend,omitempty" jsonschema:"docs backend (default: search)"`
	Library string `json:"library,omitempty" jsonschema:"the library to answer from"`
	Tokens  int    `json:"tokens,omitempty" jsonschema:"token budget"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max number of results (default: the configured limit; with library set, unbounded unless given)"`
	Tag     string `json:"tag,omitempty" jsonschema:"record each result under this tag, retrievable later with the tag tool; not with resolve"`
	Resolve bool   `json:"resolve,omitempty" jsonschema:"resolve a library name to library IDs instead of searching docs"`
	// Section asks for one section of the local index in full.
	Section string `json:"section,omitempty" jsonschema:"read one section in full from the local index"`
	// Page picks the page of a section longer than tokens.
	Page int `json:"page,omitempty" jsonschema:"with section: the page to return"`
	// Offline skips the check of an indexed answer's cited pages.
	Offline bool `json:"offline,omitempty" jsonschema:"answer from the local index without reading again the cited pages older than cache_ttl"`
}

// DocsOutput is the output schema for the "docs" tool. Results is populated
// for a normal or library-scoped search; Matches is populated when Resolve
// is set. The fields of an answer match the CLI's `ketch docs --json`
// (docs.AnswerJSON) and the frontmatter of its text output.
//
// Status is where the call ended (found or partial; a refused or failed
// call is a tool error, except a question none of whose distinctive terms
// the docs hold: status refused, no results, not_found the answer),
// Reason why, Note what it says in words (how much of a site search read,
// and how to index the rest), and Backend the backend that answered. An
// answer from the local index also says what it read (Library, Version,
// Index), the question's rare terms as written with the sections holding
// each (Terms) and the distinctive ones no section holds
// (NotFound), and when a
// result was cut, Hint: how to read a cut result in full. Stale says an
// answer from the index could not read again a cited page older than
// cache_ttl ("unverified, built <date>").
type DocsOutput struct {
	Warnings []string `json:"warnings,omitempty"`
	Library  string   `json:"library,omitempty"`
	// Scope is the URL below the library's root an answer was limited to,
	// and OutsideScope the distinctive terms the library holds that no
	// section under it does.
	Scope   string          `json:"scope,omitempty"`
	Version string          `json:"version,omitempty"`
	Index   *docs.IndexInfo `json:"index,omitempty"`
	// Origin says whether an answer from pages a search read is from the
	// documentation of what was asked: "verified, <evidence>" or
	// "by name, <host> documents the <ecosystem> package <name>[; the search
	// results favour <root>]" or "unverified, ketch could not confirm <host>
	// as the official documentation of <what>".
	Origin string           `json:"origin,omitempty"`
	Stale  string           `json:"stale,omitempty"`
	Status docs.Status      `json:"status,omitempty"`
	Reason string           `json:"reason,omitempty"`
	Terms  []docs.TermCount `json:"terms,omitzero"`
	// DefinedIn names, for a listed term no result defines, a section
	// that does.
	DefinedIn    []docs.DefinedIn `json:"defined_in,omitempty"`
	NotFound     []string         `json:"not_found,omitzero"`
	OutsideScope []string         `json:"outside_scope,omitempty"`
	// IndexedNear are the indexed libraries near a library value that
	// named none, the roots to pass as library next.
	IndexedNear []docs.NearLibrary  `json:"indexed_near,omitempty"`
	Note        string              `json:"note,omitempty"`
	Hint        string              `json:"hint,omitempty"`
	Results     []docs.Result       `json:"results,omitempty"`
	Matches     []docs.LibraryMatch `json:"matches,omitempty"`
	Backend     string              `json:"backend,omitempty"`
}

// fromOutcome renders a found or partial outcome, or the local
// index's refusal for want of any of the question's terms.
func fromOutcome(o docs.Outcome) DocsOutput {
	out := DocsOutput{Status: o.Status, Reason: o.Reason, Note: o.Note, Stale: o.Stale, Results: o.Results, Matches: o.Matches, Backend: o.Backend}
	if o.Index != nil {
		f := docs.NewFrontmatter("", docs.SearchBackend, "", o)
		out.Library, out.Version, out.Index, out.Terms, out.NotFound = f.Library, f.Version, f.Index, f.Terms, f.NotFound
		out.Scope, out.OutsideScope, out.DefinedIn = f.Scope, f.OutsideScope, f.DefinedIn
		out.Backend = docs.SearchBackend
		out.Hint = docs.AnswerHint(docs.HintMCP, docs.CutCount(o.Results))
		out.Origin = o.Origin
		if h := docs.OriginHint(docs.HintMCP, o); h != "" && len(o.Results) > 0 {
			if out.Hint != "" {
				out.Hint += "; "
			}
			out.Hint += h
		}
	}
	if len(o.IndexedNear) > 0 {
		out.IndexedNear = o.IndexedNear
		if out.Hint != "" {
			out.Hint += "; "
		}
		out.Hint += docs.NearHint(docs.HintMCP)
	}
	return out
}

// termsRefusal reports the local index's refusal of a question none of
// whose distinctive terms the docs hold: a result whose not_found is the
// answer, not a tool error.
func termsRefusal(o docs.Outcome) bool {
	return o.Status == docs.Refused && o.Index != nil
}

func (s *Server) registerDocsTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "docs",
		InputSchema: docsInputSchema(),
		Description: "Library documentation: evidence and a verdict, no model; you decide the next call. An indexed library answers from the local index; otherwise a web search picks a docs site and reads up to 24 of its pages, saving nothing." +
			" library: a docs root as resolve lists it, or a name (read from the root resolve verifies; one several libraries share is refused, listing them); a deeper URL narrows the answer to the pages under it (scope; outside_scope: your terms only elsewhere in the library). indexed_near: your library named no indexed one; pass one of these as library." +
			" Read the verdict first. status: found (evidence matching the question, not a promise that it answers); partial, reason terms_not_found (not_found: your terms nowhere in this source); refused (nothing here matches: stop asking this source; not a tool error). origin (pages a search read): verified; by name (only a package name) or unverified: name the site you cite, or pass the official docs as library. terms: your rare terms, with the sections holding each. defined_in: [{term, section}], where a term is defined when that section is not shown. index: kind, pages, built; stale: cited pages could not be checked (offline skips the check): say so if it matters." +
			" results: verbatim pieces of sections, best first (page_title, heading_path, url, section), about 1,800 tokens: leave tokens unset unless one is too long to read. truncated (shown_tokens of section_tokens) marks a cut one, contents names what it left out, … marks text left out." +
			" Two rules. Text behind … or in a truncated result is unread: read it with section before stating what it says, or say you did not read it. Whatever your answer says that no result prints is your own knowledge: label it so, never cite the docs for it." +
			" Lookup: docs(query=\"<question>\", library=\"<docs root>\"). The fact is printed: answer, citing each result's url. The result you need is truncated, or defined_in names it: docs(section=\"<its section field>\"), then answer; a long section comes a page at a time (the hint gives the next call). Otherwise Follow." +
			" Follow (the docs use other words): ask again in words the last call printed (a name in contents, a heading, a defined_in target), or read that section; never rephrase blind. After three calls to one source, stop: say what you tried and that it does not cover this." +
			" Survey (the question spans products): docs(resolve=true) lists the indexed sources (query narrows it): ask it before calling a source unknown. Split the question by owner; for every not_found term, ask the indexed source that owns it before saying it is not covered. One Lookup or Follow per source, run together; combine, cite each claim, say what no source covered." +
			" section: a url#anchor, or a page URL for its sections in order. backend context7 gives Context7's snippets instead, library a Context7 ID from resolve; its keyless quota is small." +
			errTaxonomy,
		Annotations: readOnlyOpenWorld(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in DocsInput) (*mcpsdk.CallToolResult, DocsOutput, error) {
		if err := validResearchTag(in.Tag); err != nil {
			return nil, DocsOutput{}, err
		}
		ctx, diagnostics := withTagDiagnostics(ctx)
		if in.Section != "" {
			return s.docsSection(ctx, in, diagnostics)
		}
		backend := docs.CanonicalBackend(in.Backend)
		if backend == "" {
			backend = docs.SearchBackend // docs_backend is retired: Context7 only when a call names it
		}
		if err := validQuery(in, backend); err != nil {
			return nil, DocsOutput{}, err
		}
		limit := in.Limit
		if limit <= 0 {
			limit = s.cfg.Limit
		}
		tokens := in.Tokens
		if tokens <= 0 {
			tokens = 4000
		}
		// Search takes the budget and limit only when given, and its own
		// defaults otherwise.
		ctx = docs.WithLocalOptions(ctx, docs.LocalOptions{Tokens: max(in.Tokens, 0), Limit: max(in.Limit, 0), Hint: docs.HintMCP, Offline: in.Offline})

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

		var o docs.Outcome
		if selecting, ok := searcher.(docs.SelectingSearcher); ok {
			o = selecting.SearchSelect(ctx, in.Query, limit)
		} else {
			results, err := searcher.Search(ctx, in.Query, limit)
			o = docs.Answer(backend, results, err)
		}
		if err := o.Err(); err != nil && !termsRefusal(o) {
			return nil, DocsOutput{}, upstreamErrf(err, "docs search failed")
		}

		s.recordResults(ctx, in.Tag, docsTagged(o.Results))
		out := fromOutcome(o)
		out.Warnings = diagnostics.values()
		return nil, out, nil
	})
}

// validQuery checks a docs call that asks a question (no section): it
// needs a query, and takes no page. Search's resolve takes none: it then
// lists every indexed library.
func validQuery(in DocsInput, backend string) error {
	if in.Page != 0 {
		return errf(kindValidation, "page needs section: it picks a page of the section returned")
	}
	listsIndexed := in.Resolve && docs.ResolveBackend(backend) == docs.SearchBackend
	if in.Query == "" && !listsIndexed {
		return errf(kindValidation, "query is required")
	}
	return nil
}

// docsSection returns one section of the local index in full (the
// follow-up to an answer that cut it), as the CLI's --section does.
// The index is search's, whatever the configured default; only another
// backend named in the call is refused.
func (s *Server) docsSection(ctx context.Context, in DocsInput, diagnostics *tagDiagnostics) (*mcpsdk.CallToolResult, DocsOutput, error) {
	if in.Resolve {
		return nil, DocsOutput{}, errf(kindValidation, "section cannot be combined with resolve")
	}
	if backend := docs.CanonicalBackend(in.Backend); backend != "" && backend != docs.SearchBackend {
		return nil, DocsOutput{}, errf(kindValidation, "section reads the local docs index, which the %s backend does not use (drop backend)", backend)
	}
	if in.Page < 0 {
		return nil, DocsOutput{}, errf(kindValidation, "page is a page number from 1 (got %d)", in.Page)
	}
	ctx = docs.WithLocalOptions(ctx, docs.LocalOptions{Hint: docs.HintMCP, Offline: in.Offline})
	searcher, err := docs.NewFromConfig(s.cfg, docs.SearchBackend, s.docsDeps(ctx))
	if err != nil {
		return nil, DocsOutput{}, backendErrf(err)
	}
	sections, ok := searcher.(docs.SectionFetcher)
	if !ok {
		return nil, DocsOutput{}, errf(kindValidation, "docs backend %q does not support section", docs.SearchBackend)
	}
	o := sections.SectionSelect(ctx, in.Library, in.Section, max(in.Tokens, 0), in.Page)
	if err := o.Err(); err != nil {
		return nil, DocsOutput{}, upstreamErrf(err, "section fetch failed")
	}
	s.recordResults(ctx, in.Tag, docsTagged(o.Results))
	out := fromSection(o)
	out.Warnings = diagnostics.values()
	return nil, out, nil
}

// fromSection renders a section fetch with the fields an answer says of
// its source (docs.SectionFrontmatter: library, version, index or source,
// stale), the section as the one result, and the next page's call as the
// hint.
func fromSection(o docs.Outcome) DocsOutput {
	f := docs.SectionFrontmatter(docs.SearchBackend, "", o)
	out := DocsOutput{Library: f.Library, Version: f.Version, Index: f.Index, Stale: f.Stale, Status: o.Status, Note: o.Note, Results: o.Results, Backend: f.Backend}
	if len(o.Results) == 1 {
		out.Hint = docs.NextPageLine(o.Results[0])
	}
	return out
}

// docsResolve maps a library name or spec to library IDs, returning at most
// limit matches. Each match goes out as the backend gave it, with whatever
// version, evidence and package specs it carries.
func (s *Server) docsResolve(ctx context.Context, backend, query string, limit int) (*mcpsdk.CallToolResult, DocsOutput, error) {
	resolver, err := s.libraryResolver(ctx, backend)
	if err != nil {
		return nil, DocsOutput{}, err
	}
	var o docs.Outcome
	if selecting, ok := resolver.(docs.SelectingSearcher); ok {
		o = selecting.ResolveSelect(ctx, query, limit)
	} else {
		matches, err := resolver.ResolveLibrary(ctx, query, limit)
		if err == nil && len(matches) == 0 {
			err = fmt.Errorf("library %q %w", query, docs.ErrNotFound)
		}
		o = docs.Resolved(backend, matches, err)
	}
	if err := o.Err(); err != nil {
		return nil, DocsOutput{}, upstreamErrf(err, "resolve failed")
	}
	out := fromOutcome(o)
	out.Results = nil
	return nil, out, nil
}

// docsForLibrary fetches docs for a known library ID, capped at limit
// results when limit is positive.
func (s *Server) docsForLibrary(ctx context.Context, backend, query, library string, tokens, limit int, tag string) (*mcpsdk.CallToolResult, DocsOutput, error) {
	resolver, err := s.libraryResolver(ctx, backend)
	if err != nil {
		return nil, DocsOutput{}, err
	}
	var o docs.Outcome
	if selecting, ok := resolver.(docs.SelectingSearcher); ok {
		o = selecting.GetDocsSelect(ctx, library, query, tokens)
	} else {
		results, err := resolver.GetDocs(ctx, library, query, tokens)
		o = docs.Answer(backend, results, err)
	}
	if err := o.Err(); err != nil && !termsRefusal(o) {
		return nil, DocsOutput{}, upstreamErrf(err, "docs fetch failed")
	}
	o.Results = docs.Truncate(o.Results, limit)
	s.recordResults(ctx, tag, docsTagged(o.Results))
	out := fromOutcome(o)
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
	backends := docs.AvailableBackends()
	libNames := strings.Join(docs.LibraryProviderNames(), ", ")
	return inputSchema[DocsInput](map[string]string{
		"query":   docs.WithLibraryHelp("the docs question (search picks the library and its docs site from it unless library is set), or the library to resolve when resolve is true (search: optional; none lists every indexed library); not needed with section"),
		"backend": "docs backend for this call: " + configbase.JoinNames(backends) + " (default: search; context7 only when named here)",
		"library": docs.WithLibraryHelp("the library to answer from: for context7 a library ID from resolve"),
		"tokens":  "token budget for " + strings.Join(docs.LibraryProviderNames(), " and ") + ": search answers in 1,800 tokens unless set (leave it unset unless a result is too long to read); context7 defaults to 4000; with section, the page size of the section returned (default 4000)",
		"section": "read one section in full instead of searching: the url#anchor of a result's section field or a defined_in entry (a page URL alone returns the page's sections in order: how to read one page), from the local index or, for a page a search read, the page through the page cache; whole when it fits tokens (default 4000), else a page of that size at a time (page); library is optional",
		"offline": "answer from the local index without reading again the cited pages older than cache_ttl (stale says so when one is)",
		"page":    "with section: the page of a section longer than tokens to return (default 1); the hint gives the call for the next",
		"resolve": docs.WithLibraryHelp("resolve the query as a library name to " + libNames + " library IDs instead of searching docs; search answers from the local index first (the indexed libraries the query names, every one with no query, each marked indexed), with no network, and resolves a name none matches to its docs roots"),
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
