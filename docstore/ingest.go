package docstore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/crawl"
	"github.com/1broseidon/ketch/scrape"
)

// Ingest defaults. MaxPages is the guard rail that keeps an agent-initiated
// add from turning into a site-scale crawl; Depth only applies to
// SourceCrawl, where list sources already know their page set.
const (
	DefaultMaxPages    = 500
	DefaultDepth       = 5
	DefaultConcurrency = 8
)

// StoppedMaxPages is Summary.Stopped when the page budget cut the fetch short.
const StoppedMaxPages = "max_pages"

// AddOptions describe one `docs add`.
type AddOptions struct {
	Name    string
	Version string
	Seed    string
	// Prefix overrides the derived path scope ("" derives from the seed,
	// "/" means the whole host).
	Prefix       string
	ForceSitemap bool
	MaxPages     int
	Depth        int
	Concurrency  int
	// DryRun discovers and plans but fetches nothing and writes nothing.
	DryRun bool
	// Progress, when set, is called once per page attempt with the URL and
	// nil or the fetch error. Calls may come from several goroutines.
	Progress func(url string, err error)
}

// PageError is one URL that could not be fetched. The add continues.
type PageError struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// Summary reports what an add did, in the shape an agent needs to decide
// whether the corpus is complete enough or should be re-run wider.
type Summary struct {
	Library  *Library      `json:"library,omitempty"`
	Plan     Plan          `json:"plan"`
	Fetched  int           `json:"fetched"`
	Skipped  int           `json:"skipped"` // fetched but empty after extraction
	Failed   int           `json:"failed"`
	Errors   []PageError   `json:"errors,omitempty"`
	Stopped  string        `json:"stopped,omitempty"`
	DryRun   bool          `json:"dry_run,omitempty"`
	Duration time.Duration `json:"-"`
}

// Add discovers, fetches, chunks, and indexes a library, replacing any
// existing library of the same name. It returns the summary even on partial
// success; a nil error means the library was written (or, for a dry run,
// planned). Errors wrapping ErrEmpty mean nothing indexable was fetched.
func Add(ctx context.Context, store *Store, s *scrape.Scraper, pc *cache.Cache, opts AddOptions) (*Summary, error) {
	start := time.Now()
	if err := ValidateName(opts.Name); err != nil {
		return nil, err
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = DefaultMaxPages
	}
	if opts.Depth <= 0 {
		opts.Depth = DefaultDepth
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = DefaultConcurrency
	}

	plan, err := Discover(ctx, s, opts.Seed, DiscoverOptions{Prefix: opts.Prefix, ForceSitemap: opts.ForceSitemap})
	if err != nil {
		return nil, err
	}
	sum := &Summary{Plan: *plan, DryRun: opts.DryRun}
	if len(plan.URLs) > opts.MaxPages {
		sum.Stopped = StoppedMaxPages
	}
	if opts.DryRun {
		sum.Duration = time.Since(start)
		return sum, nil
	}

	var pages []Page
	switch plan.Source {
	case SourceLLMSFull:
		pages, err = fetchLLMSFull(ctx, s, plan.SourceURL)
		if err != nil {
			sum.Failed++
			sum.Errors = append(sum.Errors, PageError{URL: plan.SourceURL, Error: err.Error()})
		}
	case SourceLLMS, SourceSitemap:
		pages = fetchList(ctx, s, pc, plan.URLs, opts, sum)
	case SourceCrawl:
		pages = fetchCrawl(ctx, s, pc, plan, opts, sum)
	default:
		return nil, fmt.Errorf("unknown source %q", plan.Source)
	}
	if ctx.Err() != nil && sum.Stopped == "" {
		return sum, ctx.Err()
	}

	lib, err := store.Replace(Library{
		Name: opts.Name, Version: opts.Version, Seed: plan.Seed,
		Source: plan.Source, SourceURL: plan.SourceURL, Prefix: plan.Prefix, MaxPages: opts.MaxPages,
	}, pages)
	sum.Duration = time.Since(start)
	if err != nil {
		if errors.Is(err, ErrEmpty) {
			return sum, fmt.Errorf("%w from %s (%s): %d fetched, %d empty, %d failed", ErrEmpty, plan.SourceURL, plan.Source, sum.Fetched, sum.Skipped, sum.Failed)
		}
		return sum, err
	}
	sum.Library = lib
	return sum, nil
}

func fetchLLMSFull(ctx context.Context, s *scrape.Scraper, raw string) ([]Page, error) {
	content, err := s.FetchContent(ctx, raw)
	if err != nil {
		return nil, err
	}
	body := string(content.Body)
	return []Page{{URL: raw, Title: markdownTitle(body, raw), Markdown: body, ContentHash: scrape.ContentHash(body)}}, nil
}

// fetchList scrapes a known URL list with a bounded worker pool, honouring
// MaxPages. Results are returned in URL order so the store is deterministic
// regardless of which worker finished first.
func fetchList(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, urls []string, opts AddOptions, sum *Summary) []Page {
	if len(urls) > opts.MaxPages {
		urls = urls[:opts.MaxPages]
	}
	var (
		mu    sync.Mutex
		pages []Page
		wg    sync.WaitGroup
		sem   = make(chan struct{}, opts.Concurrency)
	)
	for _, raw := range urls {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(raw string) {
			defer wg.Done()
			defer func() { <-sem }()
			page, err := fetchOne(ctx, s, pc, raw)
			if opts.Progress != nil {
				opts.Progress(raw, err)
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				sum.Failed++
				sum.Errors = append(sum.Errors, PageError{URL: raw, Error: err.Error()})
			case strings.TrimSpace(page.Markdown) == "":
				sum.Skipped++
			default:
				sum.Fetched++
				pages = append(pages, *page)
			}
		}(raw)
	}
	wg.Wait()
	sort.Slice(pages, func(i, j int) bool { return pages[i].URL < pages[j].URL })
	sort.Slice(sum.Errors, func(i, j int) bool { return sum.Errors[i].URL < sum.Errors[j].URL })
	return pages
}

// fetchOne fetches a single page. Markdown and plain-text URLs (the .md
// twins that llms.txt indexes point at) are taken verbatim; everything else
// goes through the cache-aware, JS-shell-aware scrape pipeline.
func fetchOne(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, raw string) (*Page, error) {
	if isTextURL(raw) {
		content, err := s.FetchContent(ctx, raw)
		if err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToLower(content.ContentType), "html") {
			return scrapePage(ctx, s, pc, raw)
		}
		body := string(content.Body)
		return &Page{URL: raw, Title: markdownTitle(body, raw), Markdown: body, ContentHash: scrape.ContentHash(body)}, nil
	}
	return scrapePage(ctx, s, pc, raw)
}

func scrapePage(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, raw string) (*Page, error) {
	var pageCache scrape.PageCache
	if pc != nil {
		pageCache = pc
	}
	p, err := s.ScrapeMarkdown(ctx, pageCache, raw, false)
	if err != nil {
		return nil, err
	}
	return fromScrape(p), nil
}

func fromScrape(p *scrape.Page) *Page {
	title := p.Title
	if strings.TrimSpace(title) == "" {
		title = markdownTitle(p.Markdown, p.URL)
	}
	return &Page{URL: p.URL, Title: title, Markdown: p.Markdown, ETag: p.ETag, LastModified: p.LastModified, ContentHash: p.ContentHash}
}

// fetchCrawl runs a same-host BFS from the seed, scoped to the plan prefix,
// and stops once MaxPages pages have been collected.
func fetchCrawl(ctx context.Context, s *scrape.Scraper, pc *cache.Cache, plan *Plan, opts AddOptions, sum *Summary) []Page {
	crawlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	host := hostOf(plan.Seed)

	var (
		mu     sync.Mutex
		pages  []Page
		capped bool
	)
	collect := func(r crawl.Result) {
		mu.Lock()
		defer mu.Unlock()
		if capped {
			return
		}
		if opts.Progress != nil {
			var err error
			if r.Error != "" {
				err = errors.New(r.Error)
			}
			opts.Progress(r.URL, err)
		}
		if r.Error != "" {
			sum.Failed++
			sum.Errors = append(sum.Errors, PageError{URL: r.URL, Error: r.Error})
			return
		}
		if r.Page == nil || !InScope(r.URL, host, plan.Prefix) {
			return
		}
		if strings.TrimSpace(r.Page.Markdown) == "" {
			sum.Skipped++
			return
		}
		sum.Fetched++
		pages = append(pages, *fromScrape(r.Page))
		if len(pages) >= opts.MaxPages {
			capped = true
			sum.Stopped = StoppedMaxPages
			cancel()
		}
	}

	copts := crawl.Options{Depth: opts.Depth, Concurrency: opts.Concurrency}
	if plan.Prefix != "" {
		copts.Allow = []string{plan.Prefix}
	}
	if err := crawl.Crawl(crawlCtx, plan.Seed, s, copts, pc, false, collect); err != nil && !capped && ctx.Err() == nil {
		mu.Lock()
		sum.Failed++
		sum.Errors = append(sum.Errors, PageError{URL: plan.Seed, Error: err.Error()})
		mu.Unlock()
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].URL < pages[j].URL })
	sort.Slice(sum.Errors, func(i, j int) bool { return sum.Errors[i].URL < sum.Errors[j].URL })
	return pages
}

func isTextURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".md", ".markdown", ".txt", ".mdx":
		return true
	}
	return false
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// markdownTitle returns the first H1 in body, else the last path segment of
// the URL (without extension), else the host.
func markdownTitle(body, raw string) string {
	for _, line := range strings.SplitN(body, "\n", 200) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimLeft(line, "# "))
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if seg := path.Base(strings.TrimRight(u.Path, "/")); seg != "" && seg != "." && seg != "/" {
		return strings.TrimSuffix(seg, path.Ext(seg))
	}
	return u.Hostname()
}
