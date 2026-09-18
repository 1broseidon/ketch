package mcp

import (
	"context"
	"errors"
	"sync"

	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/scrape"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Batch scrapes run a bounded worker pool over the shared scraper/cache.
const (
	defaultScrapeConcurrency = 5  // matches the CLI's --concurrency default
	maxScrapeConcurrency     = 16 // server-side cap
)

// ScrapeInput is the input schema for the "scrape" tool. Exactly one of url
// or urls must be provided. The options mirror the per-invocation flags of
// `ketch scrape`; the CLI's file/stdin input modes don't apply here.
type ScrapeInput struct {
	URL          string   `json:"url,omitempty" jsonschema:"the URL to scrape (exactly one of url or urls)"`
	URLs         []string `json:"urls,omitempty" jsonschema:"URLs to scrape concurrently; failures come back as per-URL error entries (exactly one of url or urls)"`
	Selector     string   `json:"selector,omitempty" jsonschema:"CSS selector to extract specific elements, skips readability (incompatible with raw)"`
	Trim         bool     `json:"trim,omitempty" jsonschema:"strip markdown formatting, keep content text only (incompatible with raw)"`
	MaxChars     int      `json:"max_chars,omitempty" jsonschema:"truncate markdown (or raw HTML) output to N characters per page (0 = disabled)"`
	NoCache      bool     `json:"no_cache,omitempty" jsonschema:"bypass the page cache"`
	Raw          bool     `json:"raw,omitempty" jsonschema:"return raw HTML in raw_html instead of extracted markdown"`
	ForceBrowser bool     `json:"force_browser,omitempty" jsonschema:"always render via the configured headless browser, skipping JS-shell auto-detection (requires a configured browser)"`
	NoLLMSTxt    bool     `json:"no_llms_txt,omitempty" jsonschema:"disable automatic /llms.txt detection for bare domain URLs"`
	Concurrency  int      `json:"concurrency,omitempty" jsonschema:"max concurrent fetches for multi-URL scrapes (default 5, capped at 16)"`
	Tag          string   `json:"tag,omitempty" jsonschema:"record each fetched page under this tag, retrievable later with the tag tool"`
}

// ScrapeResult is one scraped page. It embeds scrape.Page — the same object
// `ketch scrape --json` emits — plus the raw-mode fields (source, raw_html)
// and a per-URL error slot used by batch scrapes.
type ScrapeResult struct {
	scrape.Page
	Source  string `json:"source,omitempty" jsonschema:"fetch path that produced the page (http, http_shell, browser); raw mode only"`
	RawHTML string `json:"raw_html,omitempty" jsonschema:"raw page HTML; set when raw is true"`
	Error   string `json:"error,omitempty" jsonschema:"per-URL failure (batch scrapes only); other fields are empty when set"`
}

// ScrapeOutput is the output schema for the "scrape" tool: one entry per
// requested URL, in input order.
type ScrapeOutput struct {
	Results []ScrapeResult `json:"results"`
}

func (s *Server) registerScrapeTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name: "scrape",
		Description: "Fetch one or more URLs, extract the main content, and convert it to clean markdown (or raw HTML with raw=true). " +
			"Bare domains are auto-probed for /llms.txt first unless no_llms_txt is set. " +
			"Note: the server fetches whatever URL it is given, including private or internal addresses reachable from where it runs." +
			errTaxonomy,
		Annotations: readOnlyOpenWorld(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in ScrapeInput) (*mcpsdk.CallToolResult, ScrapeOutput, error) {
		if err := s.validateScrapeInput(in); err != nil {
			return nil, ScrapeOutput{}, err
		}

		if in.URL != "" {
			res, err := s.scrapeOne(ctx, in.URL, in)
			if err != nil {
				return nil, ScrapeOutput{}, err
			}
			return nil, ScrapeOutput{Results: []ScrapeResult{res}}, nil
		}
		return nil, ScrapeOutput{Results: s.scrapeBatch(ctx, in)}, nil
	})
}

// validateScrapeInput enforces the url/urls union and the CLI's flag
// compatibility rules. Browser availability is checked after response
// classification so forced PDF scrapes do not require a browser.
func (s *Server) validateScrapeInput(in ScrapeInput) error {
	if (in.URL == "") == (len(in.URLs) == 0) {
		return errf(kindValidation, "provide exactly one of url or urls")
	}
	if in.Raw && in.Selector != "" {
		return errf(kindValidation, "raw cannot be combined with selector (selector is extraction-oriented)")
	}
	if in.Raw && in.Trim {
		return errf(kindValidation, "raw cannot be combined with trim (trim is markdown-specific)")
	}
	return nil
}

// scrapeOne fetches a single URL, applying selector, raw, and llms.txt
// detection the same way `ketch scrape` does for a single argument.
func (s *Server) scrapeOne(ctx context.Context, rawURL string, in ScrapeInput) (ScrapeResult, error) {
	pc := s.pageCache(in.NoCache)

	if in.Selector != "" {
		page, err := s.scraper.ScrapeSelector(ctx, rawURL, in.Selector, in.ForceBrowser)
		if err != nil {
			return ScrapeResult{}, classifySelectorErr(err)
		}
		s.recordTag(in.Tag, rawURL, page)
		page.Markdown = extract.PostProcess(page.Markdown, in.Trim, in.MaxChars)
		return ScrapeResult{Page: *page}, nil
	}

	// raw bypasses the llms.txt probe and markdown post-processing: it is an
	// output mode over the fetched HTML, not an extraction mode.
	if in.Raw {
		page, rawHTML, source, err := s.scraper.ScrapeRaw(ctx, pc, rawURL, in.ForceBrowser)
		if err != nil {
			if errors.Is(err, scrape.ErrPDFRawUnsupported) {
				return ScrapeResult{}, errf(kindValidation, "%w", err)
			}
			return ScrapeResult{}, classifyScrapeFailure(err)
		}
		s.recordTag(in.Tag, rawURL, page)
		return ScrapeResult{Page: *page, Source: source, RawHTML: extract.Truncate(rawHTML, in.MaxChars)}, nil
	}

	// llms.txt auto-detection for bare domains. Skipped under force_browser:
	// the caller explicitly wants the rendered page, not an /llms.txt shortcut.
	if !in.NoLLMSTxt && !in.ForceBrowser {
		if content, ok := s.scraper.FetchLLMSTxt(ctx, rawURL); ok {
			page := scrape.Page{URL: rawURL, Title: "llms.txt", Markdown: content}
			s.recordTag(in.Tag, rawURL, &page)
			page.Markdown = extract.PostProcess(page.Markdown, in.Trim, in.MaxChars)
			return ScrapeResult{Page: page}, nil
		}
	}

	page, err := s.scraper.ScrapeMarkdown(ctx, pc, rawURL, in.ForceBrowser)
	if err != nil {
		return ScrapeResult{}, classifyScrapeFailure(err)
	}
	s.recordTag(in.Tag, rawURL, page)
	page.Markdown = extract.PostProcess(page.Markdown, in.Trim, in.MaxChars)
	return ScrapeResult{Page: *page}, nil
}

// scrapeBatch fetches in.URLs through a bounded worker pool over the shared
// scraper and cache. Failures land in that entry's Error field instead of
// failing the whole call; results keep input order.
func (s *Server) scrapeBatch(ctx context.Context, in ScrapeInput) []ScrapeResult {
	concurrency := in.Concurrency
	if concurrency <= 0 {
		concurrency = defaultScrapeConcurrency
	}
	if concurrency > maxScrapeConcurrency {
		concurrency = maxScrapeConcurrency
	}

	results := make([]ScrapeResult, len(in.URLs))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, u := range in.URLs {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, rawURL string) {
			defer wg.Done()
			defer func() { <-sem }()

			res, err := s.scrapeOne(ctx, rawURL, in)
			if err != nil {
				results[idx] = ScrapeResult{Page: scrape.Page{URL: rawURL}, Error: err.Error()}
				return
			}
			results[idx] = res
		}(i, u)
	}
	wg.Wait()
	return results
}

func classifyScrapeFailure(err error) error {
	switch {
	case errors.Is(err, extract.ErrPDFNoText):
		return errf(kindPrecondition, "scrape failed: %w; hint: configure external_pdf_to_md_converter_command with an OCR-capable converter", err)
	case errors.Is(err, scrape.ErrNoBrowser):
		return errf(kindPrecondition, "force_browser requires a configured browser (set with: ketch config set browser chrome): %w", err)
	default:
		return upstreamErrf(err, "scrape failed")
	}
}

// classifySelectorErr maps the scrape package's selector sentinels to error
// kinds: bad selector → validation, no match → not_found, anything else
// (fetch/browser failure) → upstream/cancelled.
func classifySelectorErr(err error) error {
	switch {
	case errors.Is(err, scrape.ErrBadSelector), errors.Is(err, scrape.ErrPDFSelectorUnsupported):
		return errf(kindValidation, "%w", err)
	case errors.Is(err, scrape.ErrSelectorNoMatch):
		return errf(kindNotFound, "%w", err)
	default:
		return classifyScrapeFailure(err)
	}
}
