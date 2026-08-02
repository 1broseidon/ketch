package mcp

import (
	"context"
	"strings"

	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/search"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchInput is the input schema for the "search" tool. It mirrors the
// per-invocation flags of `ketch search`; config-level settings (API keys,
// default backend/limit) stay operator-configured.
type SearchInput struct {
	Query      string   `json:"query" jsonschema:"the search query"`
	Backend    string   `json:"backend,omitempty" jsonschema:"search backend: brave, ddg, searxng, exa, firecrawl, keenable, tavily, or parallel (default: the configured backend)"`
	Multi      []string `json:"multi,omitempty" jsonschema:"federated search: backends to query and rank-fuse (reciprocal rank fusion), e.g. [\"brave\",\"ddg\"]; use [\"all\"] for every usable backend; mutually exclusive with backend and random; results gain a backends field showing which engines returned each"`
	Random     []string `json:"random,omitempty" jsonschema:"random provider selection with sequential fallback on errors, e.g. [\"brave\",\"ddg\"]; use [\"all\"] for every usable backend; mutually exclusive with backend and multi"`
	Limit      int      `json:"limit,omitempty" jsonschema:"max number of results (default: the configured limit)"`
	SearxngURL string   `json:"searxng_url,omitempty" jsonschema:"override the configured SearXNG instance URL (searxng backend only)"`
	Scrape     bool     `json:"scrape,omitempty" jsonschema:"also fetch each result URL and fill its content field with extracted markdown"`
	Trim       bool     `json:"trim,omitempty" jsonschema:"strip markdown formatting from scraped content, keep text only (with scrape)"`
	MaxChars   int      `json:"max_chars,omitempty" jsonschema:"truncate each result's scraped content to N characters (with scrape; 0 = disabled)"`
}

// SearchOutput is the output schema for the "search" tool. Results carries
// the same result objects as the CLI's `ketch search --json` (which emits
// them as a bare array; MCP structured content needs the object wrapper).
// Backend names the selected provider under random search. Errors maps a
// backend name to its failure message under multi or random search, so callers
// can see which providers were dropped or tried before the winner.
type SearchOutput struct {
	Results []search.Result   `json:"results"`
	Backend string            `json:"backend,omitempty"`
	Errors  map[string]string `json:"errors,omitempty"`
}

func (s *Server) registerSearchTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name: "search",
		Description: "Search the web using Brave, DuckDuckGo, SearXNG, Exa, Firecrawl, Keenable, Tavily, or Parallel (default: the configured backend) and return results (title, url, description). " +
			"Set scrape=true to also fetch each result and include its content as markdown. " +
			"Set multi to query several backends at once and rank-fuse the results, or random to shuffle providers and fall back sequentially on errors." + errTaxonomy,
		Annotations: readOnlyOpenWorld(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in SearchInput) (*mcpsdk.CallToolResult, SearchOutput, error) {
		out, err := s.runSearch(ctx, in)
		return nil, out, err
	})
}

// runSearch is the search tool's handler body, factored out so its validation
// and error classification can be unit-tested without a live transport.
func (s *Server) runSearch(ctx context.Context, in SearchInput) (SearchOutput, error) {
	if in.Query == "" {
		return SearchOutput{}, errf(kindValidation, "query is required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = s.cfg.Limit
	}

	if len(in.Multi) > 0 && len(in.Random) > 0 {
		return SearchOutput{}, errf(kindValidation, "multi and random are mutually exclusive")
	}
	if len(in.Multi) > 0 {
		return s.runMultiSearch(ctx, in, limit)
	}
	if len(in.Random) > 0 {
		return s.runRandomSearch(ctx, in, limit)
	}

	backend := in.Backend
	if backend == "" {
		backend = s.cfg.Backend
	}
	searcher, err := search.NewFromConfig(s.cfg, backend, in.SearxngURL)
	if err != nil {
		return SearchOutput{}, backendErrf(err, search.ErrUnknownBackend)
	}
	results, err := searcher.Search(ctx, in.Query, limit)
	if err != nil {
		return SearchOutput{}, upstreamErrf(err, "search failed")
	}
	if in.Scrape {
		s.scrapeSearchResults(ctx, results, in.Trim, in.MaxChars)
	}
	return SearchOutput{Results: results}, nil
}

// runMultiSearch handles federated (multi) search: it validates the flag
// combination, resolves the backend set, fuses, and reports partial failures
// in-band via SearchOutput.Errors (MCP has an object envelope and no stderr).
func (s *Server) runMultiSearch(ctx context.Context, in SearchInput, limit int) (SearchOutput, error) {
	if in.Backend != "" {
		return SearchOutput{}, errf(kindValidation, "multi and backend are mutually exclusive")
	}
	if len(in.Random) > 0 {
		return SearchOutput{}, errf(kindValidation, "multi and random are mutually exclusive")
	}

	names := cleanMultiNames(in.Multi)
	for _, n := range names {
		if n == "all" && len(names) > 1 {
			return SearchOutput{}, errf(kindValidation, `"all" cannot be combined with other backend names in multi`)
		}
	}
	if len(names) == 0 {
		names = []string{"all"}
	}

	m, err := search.NewMultiFromConfig(s.cfg, names, in.SearxngURL)
	if err != nil {
		return SearchOutput{}, backendErrf(err, search.ErrUnknownBackend)
	}

	results, berrs, err := m.Search(ctx, in.Query, limit)
	if err != nil {
		return SearchOutput{}, upstreamErrf(err, "search failed")
	}
	if in.Scrape {
		s.scrapeSearchResults(ctx, results, in.Trim, in.MaxChars)
	}

	out := SearchOutput{Results: results}
	if len(berrs) > 0 {
		out.Errors = make(map[string]string, len(berrs))
		for _, be := range berrs {
			out.Errors[be.Backend] = be.Err.Error()
		}
	}
	return out, nil
}

// runRandomSearch shuffles the requested providers and returns the first
// successful response, including the chosen backend and prior failures.
func (s *Server) runRandomSearch(ctx context.Context, in SearchInput, limit int) (SearchOutput, error) {
	if in.Backend != "" {
		return SearchOutput{}, errf(kindValidation, "random and backend are mutually exclusive")
	}
	if len(in.Multi) > 0 {
		return SearchOutput{}, errf(kindValidation, "random and multi are mutually exclusive")
	}

	names := cleanMultiNames(in.Random)
	for _, name := range names {
		if name == "all" && len(names) > 1 {
			return SearchOutput{}, errf(kindValidation, `"all" cannot be combined with other backend names in random`)
		}
	}
	if len(names) == 0 {
		names = []string{"all"}
	}

	randomSearch, err := search.NewRandomFromConfig(s.cfg, names, in.SearxngURL)
	if err != nil {
		return SearchOutput{}, backendErrf(err, search.ErrUnknownBackend)
	}
	results, selected, failures, err := randomSearch.Search(ctx, in.Query, limit)
	if err != nil {
		return SearchOutput{}, upstreamErrf(err, "search failed")
	}
	if in.Scrape {
		s.scrapeSearchResults(ctx, results, in.Trim, in.MaxChars)
	}

	out := SearchOutput{Results: results, Backend: selected}
	if len(failures) > 0 {
		out.Errors = make(map[string]string, len(failures))
		for _, failure := range failures {
			out.Errors[failure.Backend] = failure.Err.Error()
		}
	}
	return out, nil
}

// cleanMultiNames trims and de-duplicates the multi input (first occurrence
// wins), mirroring the CLI's parseMultiNames.
func cleanMultiNames(names []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// scrapeSearchResults fills each result's Content with extracted markdown,
// like `ketch search --scrape`. Individual fetch failures leave that
// result's content empty rather than failing the whole call.
func (s *Server) scrapeSearchResults(ctx context.Context, results []search.Result, trim bool, maxChars int) {
	pc := s.pageCache(false)
	for i, r := range results {
		page, err := s.scraper.CachedScrape(ctx, pc, r.URL)
		if err != nil {
			continue
		}
		if page.FetchedURL != "" {
			results[i].FetchedURL = page.FetchedURL
		}
		results[i].Content = extract.PostProcess(page.Markdown, trim, maxChars)
	}
}
