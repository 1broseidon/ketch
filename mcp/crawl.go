package mcp

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/1broseidon/ketch/crawl"
	"github.com/1broseidon/ketch/extract"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Crawl bounds. An MCP tool call can't stream results, so the synchronous
// crawl is capped server-side: a page budget plus a wall-clock timeout.
// Bigger crawls belong to the CLI's detached background mode
// (`ketch crawl --background`), which is intentionally not exposed here.
const (
	defaultCrawlDepth    = 3 // matches the CLI's --depth default
	defaultCrawlMaxPages = 30
	maxCrawlPages        = 100
	crawlConcurrency     = 8 // matches the CLI's --concurrency default
	crawlTimeout         = 3 * time.Minute
)

// Crawl stop reasons reported in CrawlOutput.Stopped.
const (
	stoppedMaxPages = "max_pages"
	stoppedTimeout  = "timeout"
)

// CrawlInput is the input schema for the "crawl" tool.
type CrawlInput struct {
	URL      string   `json:"url" jsonschema:"seed URL to crawl (BFS, same host only)"`
	Depth    int      `json:"depth,omitempty" jsonschema:"max BFS depth (default 3)"`
	Sitemap  bool     `json:"sitemap,omitempty" jsonschema:"treat the seed URL as a sitemap"`
	MaxPages int      `json:"max_pages,omitempty" jsonschema:"stop after this many pages (default 30, capped at 100)"`
	Allow    []string `json:"allow,omitempty" jsonschema:"path substring filters; a URL must contain at least one to be crawled"`
	Deny     []string `json:"deny,omitempty" jsonschema:"regex patterns; matching URLs are skipped"`
	MaxChars int      `json:"max_chars,omitempty" jsonschema:"truncate each page's markdown to N characters (0 = disabled)"`
	NoCache  bool     `json:"no_cache,omitempty" jsonschema:"bypass the page cache"`
	Tag      string   `json:"tag,omitempty" jsonschema:"record each crawled page under this tag, retrievable later with the tag tool"`
}

// CrawlPage is one crawled page in the "crawl" tool output.
type CrawlPage struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
}

// CrawlError is a per-URL crawl failure. The crawl keeps going; these are
// reported alongside the pages that did succeed.
type CrawlError struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// CrawlOutput is the output schema for the "crawl" tool. Stopped is set when
// the crawl was cut short by the server-side page budget ("max_pages") or
// wall-clock timeout ("timeout"); the collected pages are still returned.
type CrawlOutput struct {
	Warnings []string     `json:"warnings,omitempty"`
	Pages    []CrawlPage  `json:"pages"`
	Errors   []CrawlError `json:"errors,omitempty"`
	Stopped  string       `json:"stopped,omitempty"`
}

func (s *Server) registerCrawlTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name: "crawl",
		Description: "BFS-crawl a site from a seed URL (same host only) and return extracted markdown for each page. " +
			"Synchronous and bounded: at most max_pages pages (default 30, cap 100) within a 3-minute budget — large crawls belong to the CLI's background mode (ketch crawl --background), which is not exposed over MCP. " +
			"Note: the server fetches whatever URL it is given, including private or internal addresses reachable from where it runs." +
			errTaxonomy,
		Annotations: readOnlyOpenWorld(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in CrawlInput) (*mcpsdk.CallToolResult, CrawlOutput, error) {
		if err := validResearchTag(in.Tag); err != nil {
			return nil, CrawlOutput{}, err
		}
		ctx, diagnostics := withTagDiagnostics(ctx)
		if in.URL == "" {
			return nil, CrawlOutput{}, errf(kindValidation, "url is required")
		}
		depth := in.Depth
		if depth <= 0 {
			depth = defaultCrawlDepth
		}
		maxPages := in.MaxPages
		if maxPages <= 0 {
			maxPages = defaultCrawlMaxPages
		}
		if maxPages > maxCrawlPages {
			maxPages = maxCrawlPages
		}

		// Safety timeout: a tool call must terminate even if the site is a
		// tarpit. The collector below cancels early once maxPages is reached.
		crawlCtx, cancel := context.WithTimeout(ctx, crawlTimeout)
		defer cancel()

		col := &crawlCollector{maxPages: maxPages, maxChars: in.MaxChars, cancel: cancel, ctx: crawlCtx, srv: s, tag: in.Tag}
		opts := crawl.Options{
			Depth:       depth,
			Concurrency: crawlConcurrency,
			Allow:       in.Allow,
			Deny:        in.Deny,
		}
		err := crawl.Crawl(crawlCtx, in.URL, s.scraper, opts, s.tagPageCache(ctx, in.NoCache), in.Sitemap, col.collect)

		out := CrawlOutput{Pages: col.pages, Errors: col.errs, Stopped: col.stopped(), Warnings: diagnostics.values()}
		if err != nil && out.Stopped == "" {
			// A real failure (bad seed, sitemap fetch error, client cancel) —
			// not one of our own bounds firing.
			return nil, CrawlOutput{}, upstreamErrf(err, "crawl failed")
		}
		return nil, out, nil
	})
}

// crawlCollector accumulates crawl results up to maxPages, then cancels the
// crawl context. Results arriving after the budget is spent (in-flight
// workers observing cancellation) are dropped so the output stays clean.
type crawlCollector struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	maxPages int
	maxChars int
	pages    []CrawlPage
	errs     []CrawlError
	capped   bool

	// srv and tag are set when the call asked for tagging; the collector is
	// where a crawled page is seen whole, before truncation.
	srv *Server
	tag string
}

func (c *crawlCollector) collect(r crawl.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.capped || c.ctx.Err() != nil {
		return // budget spent or deadline hit; ignore stragglers
	}
	if r.Error != "" {
		c.errs = append(c.errs, CrawlError{URL: r.URL, Error: r.Error})
		return
	}
	if r.Page == nil {
		return
	}
	c.srv.recordTag(c.ctx, c.tag, r.URL, r.Page)
	c.pages = append(c.pages, CrawlPage{
		URL:      r.Page.URL,
		Title:    r.Page.Title,
		Markdown: extract.Truncate(r.Page.Markdown, c.maxChars),
	})
	if len(c.pages) >= c.maxPages {
		c.capped = true
		c.cancel()
	}
}

// stopped reports why the crawl ended early, or "" for a complete crawl.
func (c *crawlCollector) stopped() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.capped {
		return stoppedMaxPages
	}
	if errors.Is(c.ctx.Err(), context.DeadlineExceeded) {
		return stoppedTimeout
	}
	return ""
}
