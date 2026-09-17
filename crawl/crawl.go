package crawl

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/scrape"
	"github.com/PuerkitoBio/goquery"
)

// Options configures the crawl behavior.
type Options struct {
	Depth       int      // max BFS depth, default 3
	Concurrency int      // worker pool size, default 8
	Allow       []string // path substrings that URLs must contain (any match passes)
	Deny        []string // regex patterns to reject URLs
}

// Result represents a single crawled page.
type Result struct {
	Page   *scrape.Page `json:"page,omitempty"`
	Depth  int          `json:"depth"`
	Status string       `json:"status"` // "new", "changed", "unchanged"
	Source string       `json:"source"` // "seed", "link", "sitemap"
	Error  string       `json:"error,omitempty"`
	URL    string       `json:"url"`
}

type queueItem struct {
	url    string
	depth  int
	source string
}

// hostJSStats tracks JS shell detection frequency per host.
type hostJSStats struct {
	total  int
	shells int
}

// Crawl performs a BFS crawl from the seed URL, calling fn for each page.
// The context bounds the entire crawl: cancelling it stops workers promptly
// and aborts in-flight HTTP/browser requests.
//
// URL rewrite rules on the Scraper apply twice along the crawl path: once in
// enqueue (so the visited-set and cache key are canonical) and again inside
// ScrapeConditional (which doesn't know its input was already rewritten).
// Rules must therefore be idempotent on their own output — a rule whose
// replacement re-matches its own pattern (e.g. `^/(.*)$ → /v2/$1`) will
// double-apply when crawled. The documented use cases (host swaps like
// www.reddit.com → old.reddit.com, path appends like /uk → /uk/rss) are
// idempotent and safe.
func Crawl(ctx context.Context, seed string, s *scrape.Scraper, opts Options, pc *cache.Cache, sitemap bool, fn func(Result)) error {
	seedURL, err := url.Parse(seed)
	if err != nil {
		return fmt.Errorf("invalid seed URL: %w", err)
	}

	denyRegexps, err := compileDeny(opts.Deny)
	if err != nil {
		return err
	}

	c := &crawler{
		ctx:       ctx,
		scraper:   s,
		pc:        pc,
		opts:      opts,
		seedHost:  seedURL.Hostname(),
		deny:      denyRegexps,
		visited:   make(map[string]bool),
		fn:        fn,
		hostStats: make(map[string]*hostJSStats),
	}

	return c.run(seed, sitemap)
}

type crawler struct {
	ctx      context.Context
	scraper  *scrape.Scraper
	pc       *cache.Cache
	opts     Options
	seedHost string
	deny     []*regexp.Regexp

	visitMu sync.Mutex
	visited map[string]bool

	// work carries queue items to workers. Producers spawn a goroutine
	// per send so recursive enqueue from inside processItem can't deadlock.
	work chan queueItem
	// wg counts in-flight items: incremented before spawning an enqueue
	// goroutine, decremented by the worker after processItem completes
	// (or by the enqueue goroutine itself if ctx cancels first).
	wg sync.WaitGroup
	fn func(Result)

	hostStatsMu sync.Mutex
	hostStats   map[string]*hostJSStats
}

func (c *crawler) run(seed string, sitemap bool) error {
	// Small buffer lets producer goroutines unblock promptly when a
	// worker is about to become free. Size doesn't affect correctness.
	c.work = make(chan queueItem, c.opts.Concurrency)

	var workerWg sync.WaitGroup
	for i := 0; i < c.opts.Concurrency; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for item := range c.work {
				c.processItem(item)
				c.wg.Done()
			}
		}()
	}

	if sitemap {
		urls, sErr := fetchSitemap(c.ctx, c.scraper, seed)
		if sErr != nil {
			close(c.work)
			workerWg.Wait()
			return fmt.Errorf("sitemap fetch failed: %w", sErr)
		}
		for _, u := range urls {
			c.enqueue(u, 0, "sitemap")
		}
	} else {
		c.enqueue(seed, 0, "seed")
	}

	// wg.Add for a child runs inside its parent's processItem, before the
	// parent's wg.Done — so the counter never drops to 0 while there's
	// work still in flight.
	c.wg.Wait()
	close(c.work)
	workerWg.Wait()
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	return nil
}

func (c *crawler) enqueue(rawURL string, depth int, source string) {
	rewritten := c.scraper.Rewrite(rawURL)
	norm := normalizeURL(rewritten)
	if norm == "" {
		return
	}
	if !c.tryVisit(norm) {
		return
	}
	if !passesFilters(norm, c.seedHost, c.opts.Allow, c.deny) {
		return
	}
	if c.ctx.Err() != nil {
		return
	}

	item := queueItem{url: norm, depth: depth, source: source}
	c.wg.Add(1)
	go func() {
		select {
		case c.work <- item:
			// Worker will call wg.Done after processItem.
		case <-c.ctx.Done():
			// Ctx cancelled before any worker received — balance the Add.
			c.wg.Done()
		}
	}()
}

func (c *crawler) tryVisit(u string) bool {
	c.visitMu.Lock()
	defer c.visitMu.Unlock()
	if c.visited[u] {
		return false
	}
	c.visited[u] = true
	return true
}

func (c *crawler) processItem(item queueItem) {
	// item.url was already passed through Scraper.Rewrite in enqueue, so it
	// is the canonical fetch URL. The cache key folds in any matching cookies
	// so cookie-bearing fetches don't collide with anonymous cached copies.
	cacheKey := c.scraper.CacheKey(item.url)
	cached, cachedSource := c.pc.Get(cacheKey)

	// Cache hit: use cached page, skip fetch entirely, unless the entry
	// is an unrendered JS-shell extraction and a browser is now available.
	// Use --no-cache to force re-fetch for change detection.
	if cached != nil && !scrape.CacheStaleForBrowser(cachedSource, c.scraper.HasBrowser()) {
		c.fn(Result{
			Page:   cached,
			Depth:  item.depth,
			Status: "unchanged",
			Source: item.source,
			URL:    item.url,
		})
		return
	}

	var page *scrape.Page
	var rawHTML string
	var doc *goquery.Document
	var err error
	fetchSource := scrape.SourceHTTP
	contentIsHTML := false

	// Even when the host heuristic says to render, classify the HTTP response
	// first. PDFs and other non-HTML content must never enter the browser path.
	forceBrowser := c.shouldForceBrowser(item.url) && c.scraper.HasBrowser()
	var result *scrape.FetchResult
	result, err = c.scraper.ScrapeConditional(c.ctx, item.url, "", "")
	if err == nil {
		page = result.Page
		rawHTML = result.RawHTML
		doc = result.Doc // may be nil when the page was re-fetched via browser
		fetchSource = result.Source
		contentIsHTML = result.ContentType == "text/html" || result.ContentType == "application/xhtml+xml"
		if contentIsHTML {
			c.recordJSDetection(item.url, result.JSDetection)
		}
		if forceBrowser && fetchSource != scrape.SourceBrowser && contentIsHTML {
			page, rawHTML, err = c.scraper.BrowserScrape(c.ctx, item.url)
			if err == nil {
				doc = nil
				fetchSource = scrape.SourceBrowser
			}
		}
	}

	if err != nil {
		c.fn(Result{
			URL:    item.url,
			Depth:  item.depth,
			Source: item.source,
			Error:  err.Error(),
		})
		return
	}

	c.pc.Put(cacheKey, page, fetchSource)
	c.fn(Result{
		Page:   page,
		Depth:  item.depth,
		Status: "new",
		Source: item.source,
		URL:    item.url,
	})

	if item.depth < c.opts.Depth && contentIsHTML && rawHTML != "" {
		c.enqueueLinks(item, doc, rawHTML)
	}
}

func (c *crawler) recordJSDetection(rawURL, detection string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	host := u.Hostname()
	c.hostStatsMu.Lock()
	defer c.hostStatsMu.Unlock()
	if c.hostStats[host] == nil {
		c.hostStats[host] = &hostJSStats{}
	}
	c.hostStats[host].total++
	if detection == "likely_shell" {
		c.hostStats[host].shells++
	}
}

func (c *crawler) shouldForceBrowser(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	c.hostStatsMu.Lock()
	defer c.hostStatsMu.Unlock()
	s := c.hostStats[host]
	if s == nil || s.total < 10 {
		return false
	}
	return float64(s.shells)/float64(s.total) > 0.8
}

// enqueueLinks reuses doc if non-nil (shared from ScrapeConditional) and
// otherwise parses html. Crawls of static pages skip the re-parse entirely.
func (c *crawler) enqueueLinks(parent queueItem, doc *goquery.Document, html string) {
	links := extractLinks(parent.url, doc, html)
	for _, link := range links {
		c.enqueue(link, parent.depth+1, "link")
	}
}

// normalizeURL strips fragment, utm_ params, and trailing slash.
func normalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.Fragment = ""

	q := u.Query()
	for key := range q {
		if strings.HasPrefix(key, "utm_") {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()

	s := u.String()
	s = strings.TrimRight(s, "/")
	return s
}

// passesFilters checks domain, allow, and deny filters.
func passesFilters(rawURL, seedHost string, allow []string, deny []*regexp.Regexp) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Hostname() != seedHost {
		return false
	}
	for _, re := range deny {
		if re.MatchString(rawURL) {
			return false
		}
	}
	if len(allow) > 0 {
		matched := false
		for _, sub := range allow {
			if strings.Contains(u.Path, sub) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func compileDeny(patterns []string) ([]*regexp.Regexp, error) {
	regexps := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid deny pattern %q: %w", p, err)
		}
		regexps = append(regexps, re)
	}
	return regexps, nil
}

// extractLinks returns all resolved href links on the page. If doc is
// non-nil, it is reused (shared from upstream parsing); otherwise html
// is parsed fresh.
func extractLinks(pageURL string, doc *goquery.Document, html string) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}

	if doc == nil {
		doc, err = goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return nil
		}
	}

	var links []string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" {
			return
		}
		resolved := resolveURL(base, href)
		if resolved != "" {
			links = append(links, resolved)
		}
	})
	return links
}

func resolveURL(base *url.URL, href string) string {
	if strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") ||
		strings.HasPrefix(href, "tel:") || strings.HasPrefix(href, "#") {
		return ""
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
}

// Sitemap XML structures

type sitemapIndex struct {
	XMLName  xml.Name     `xml:"sitemapindex"`
	Sitemaps []sitemapLoc `xml:"sitemap"`
}

type sitemapLoc struct {
	Loc string `xml:"loc"`
}

type urlSet struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []urlLoc `xml:"url"`
}

type urlLoc struct {
	Loc string `xml:"loc"`
}

// FetchSitemap fetches a sitemap URL and returns every page URL it lists,
// following sitemap index files into their child sitemaps. It is exported so
// the docs ingest pipeline can plan a fetch (count and filter URLs) before
// committing to a crawl.
func FetchSitemap(ctx context.Context, scraper *scrape.Scraper, sitemapURL string) ([]string, error) {
	return fetchSitemap(ctx, scraper, sitemapURL)
}

// fetchSitemap fetches a sitemap URL and returns all page URLs.
// Supports both sitemap index files and regular sitemaps.
func fetchSitemap(ctx context.Context, scraper *scrape.Scraper, sitemapURL string) ([]string, error) {
	body, err := fetchBody(ctx, scraper, sitemapURL)
	if err != nil {
		return nil, err
	}

	var idx sitemapIndex
	if xml.Unmarshal(body, &idx) == nil && len(idx.Sitemaps) > 0 {
		return fetchSitemapIndex(ctx, scraper, idx)
	}

	var us urlSet
	if err := xml.Unmarshal(body, &us); err != nil {
		return nil, fmt.Errorf("failed to parse sitemap XML: %w", err)
	}

	urls := make([]string, 0, len(us.URLs))
	for _, u := range us.URLs {
		if u.Loc != "" {
			urls = append(urls, u.Loc)
		}
	}
	return urls, nil
}

func fetchSitemapIndex(ctx context.Context, scraper *scrape.Scraper, idx sitemapIndex) ([]string, error) {
	var all []string
	for _, sm := range idx.Sitemaps {
		urls, err := fetchSitemap(ctx, scraper, sm.Loc)
		if err != nil {
			continue
		}
		all = append(all, urls...)
	}
	return all, nil
}

func fetchBody(ctx context.Context, scraper *scrape.Scraper, rawURL string) ([]byte, error) {
	content, err := scraper.FetchContent(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return content.Body, nil
}
