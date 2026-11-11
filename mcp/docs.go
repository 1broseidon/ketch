package mcp

import (
	"context"
	"fmt"
	"strconv"
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
	Backend string `json:"backend,omitempty" jsonschema:"docs backend (default: the configured backend)"`
	Library string `json:"library,omitempty" jsonschema:"the library to answer from"`
	Tokens  int    `json:"tokens,omitempty" jsonschema:"token budget"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max number of results (default: the configured limit; with library set, unbounded unless given)"`
	Tag     string `json:"tag,omitempty" jsonschema:"record each result under this tag, retrievable later with the tag tool; not with resolve"`
	Resolve bool   `json:"resolve,omitempty" jsonschema:"resolve a library name to library IDs instead of searching docs"`
	// Section asks for one section of the local index in full.
	Section string `json:"section,omitempty" jsonschema:"read one section in full from the local index"`
	// Page picks the page of a section longer than tokens.
	Page int `json:"page,omitempty" jsonschema:"with section: the page to return"`
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
// result was cut, Hint: how to read a cut result in full.
type DocsOutput struct {
	Warnings []string         `json:"warnings,omitempty"`
	Library  string           `json:"library,omitempty"`
	Version  string           `json:"version,omitempty"`
	Index    *docs.IndexInfo  `json:"index,omitempty"`
	Status   docs.Status      `json:"status,omitempty"`
	Reason   string           `json:"reason,omitempty"`
	Terms    []docs.TermCount `json:"terms,omitzero"`
	// DefinedIn names, for a listed term no result defines, a section
	// that does.
	DefinedIn []docs.DefinedIn    `json:"defined_in,omitempty"`
	NotFound  []string            `json:"not_found,omitzero"`
	Note      string              `json:"note,omitempty"`
	Hint      string              `json:"hint,omitempty"`
	Results   []docs.Result       `json:"results,omitempty"`
	Matches   []docs.LibraryMatch `json:"matches,omitempty"`
	Backend   string              `json:"backend,omitempty"`
}

// fromOutcome renders a found or partial outcome, or the local
// index's refusal for want of any of the question's terms.
func fromOutcome(o docs.Outcome) DocsOutput {
	out := DocsOutput{Status: o.Status, Reason: o.Reason, Note: o.Note, Results: o.Results, Matches: o.Matches, Backend: o.Backend}
	if o.Index != nil {
		f := docs.NewFrontmatter("", docs.SearchBackend, "", o)
		out.Library, out.Version, out.Index, out.Terms, out.NotFound = f.Library, f.Version, f.Index, f.Terms, f.NotFound
		out.DefinedIn = f.DefinedIn
		out.Backend = docs.SearchBackend
		if docs.AnyCut(o.Results) {
			out.Hint = docs.AnswerHint(docs.HintMCP)
		}
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
		Description: "Search library documentation. The default backend, search, answers from the docs themselves: a library the operator indexed (ketch docs index) answers offline from the local index; otherwise ketch searches the web for the question, picks the documentation site the results agree on, reads the matching pages and keeps them in the local index, so a repeated question answers offline (status partial, with a note naming the site and how to index all of it)." +
			" An answer says what it read (library; index: kind full or capped, pages, built; or source search and pages_read), its status and reason, terms (the question's rare terms as written: identifiers it writes and words in under 1% of the library's sections or in at most 25 of them, each with the number of sections holding it) and not_found (those no section holds). Status is found (evidence matching the question, not a promise that it answers); partial when not_found is not empty (reason terms_not_found) or the library holds only pages searches read; refused when no distinctive term is in the docs: no results, not_found is the answer (not a tool error)." +
			" Results are the pieces of sections that name the question (paragraphs, list items, code blocks whole, table rows under their header), verbatim, one result per section in printed order, best first, a page's results together (page_title, heading_path below it, anchors, url, section; rows: how many rows of a table it shows; same_text: where else the same text sits; contents names what a cut result's section holds that it left out; truncated when it was cut, with shown_tokens of section_tokens: how much of its section it shows; a section longer than " + strconv.Itoa(docs.AnswerPageTokens) + " tokens is never shown whole: its result is its best pieces up to that, in page order), at most about 1,800 tokens in all unless tokens is set; limit caps the results. A library indexed at several versions answers from the newest (each result's version says which) unless library names one as name@version." +
			docs.WithLibraryHelp(" library names the library to answer from") + "." +
			" defined_in lists, for a term in terms that is the first cell of a table row (an option's row) in a section no result shows, that section (term, section: url#anchor to pass as section; at most three)." +
			" To read a result in full, pass section instead of a query: the url#anchor in the result's section field; when a result was cut, the answer's hint says so once. Use it when a cut result is the right section but the code or detail you need is missing; it reads the local index only (no network, no quota, milliseconds). It returns the section with the subsections under it (an H2 is its whole family), or a page's sections in order for a URL with no anchor: whole when it fits tokens (default 4000), else one page of that size, broken at a block boundary, the result saying page and pages and the hint giving the call for the next page (page=N; a page past the last is [validation]); library is optional (the URL names it); an unknown URL or anchor is [not_found]." +
			" Backend context7 (or the operator's docs_backend) returns Context7's curated snippets instead; resolve maps a name to " + strings.Join(docs.LibraryProviderNames(), " or ") + " library IDs (docs roots for search), and library takes one. Context7 needs no key: without one it runs on Context7's anonymous quota (about 200 requests a month per IP; a spent quota is an [upstream] error naming when it resets), and a key lifts it." +
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
		if err := validQuery(in); err != nil {
			return nil, DocsOutput{}, err
		}
		backend := docs.CanonicalBackend(in.Backend)
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
		// Search takes the budget and limit only when given, and its own
		// defaults otherwise.
		ctx = docs.WithLocalOptions(ctx, docs.LocalOptions{Tokens: max(in.Tokens, 0), Limit: max(in.Limit, 0), Hint: docs.HintMCP})

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
// needs a query, and takes no page.
func validQuery(in DocsInput) error {
	if in.Page != 0 {
		return errf(kindValidation, "page needs section: it picks a page of the section returned")
	}
	if in.Query == "" {
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
	ctx = docs.WithLocalOptions(ctx, docs.LocalOptions{Hint: docs.HintMCP})
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
	out := fromOutcome(o)
	if len(o.Results) == 1 {
		out.Hint = docs.NextPageLine(o.Results[0])
	}
	out.Warnings = diagnostics.values()
	return nil, out, nil
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
		"query":   docs.WithLibraryHelp("the docs question (search picks the library and its docs site from it unless library is set), or the library to resolve when resolve is true; not needed with section"),
		"backend": "docs backend: " + configbase.JoinNames(backends) + " (default: the configured backend, search unless the operator chose otherwise)",
		"library": docs.WithLibraryHelp("the library to answer from: for context7 a library ID from resolve"),
		"tokens":  "token budget for " + strings.Join(docs.LibraryProviderNames(), " and ") + ": search answers in 1,800 tokens unless set; context7 defaults to 4000; with section, the page size of the section returned (default 4000)",
		"section": "read one section in full from the local index instead of searching: the url#anchor of a result's section field (a page URL alone returns the page's sections in order); whole when it fits tokens (default 4000), else a page of that size at a time (page); library is optional",
		"page":    "with section: the page of a section longer than tokens to return (default 1); the hint gives the call for the next",
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
