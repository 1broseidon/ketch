package crawl

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/internal/cache"
	"github.com/1broseidon/ketch/internal/httpx"
	"github.com/1broseidon/ketch/internal/scrape"
	"github.com/PuerkitoBio/goquery"
)

// Options configures the crawl behavior.
type Options struct {
	Depth       int      // max BFS depth, default 3
	Concurrency int      // worker pool size, default 8
	Allow       []string // path substrings that URLs must contain (any match passes)
	Deny        []string // regex patterns to reject URLs
	BrowserBin  string   // browser binary for JS-rendered page fallback; empty = disabled
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
func Crawl(ctx context.Context, seed string, opts Options, pc *cache.Cache, sitemap bool, fn func(Result)) error {
	seedURL, err := url.Parse(seed)
	if err != nil {
		return fmt.Errorf("invalid seed URL: %w", err)
	}

	denyRegexps, err := compileDeny(opts.Deny)
	if err != nil {
		return err
	}

	var s *scrape.Scraper
	if opts.BrowserBin != "" {
		s = scrape.NewWithBrowser(opts.BrowserBin)
	} else {
		s = scrape.New()
	}
	defer s.Close()

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
		urls, sErr := fetchSitemap(c.ctx, seed)
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
	norm := normalizeURL(rawURL)
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
	cached := c.pc.Get(item.url)

	// Cache hit: use cached page, skip fetch entirely.
	// Use --no-cache to force re-fetch for change detection.
	if cached != nil {
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

	// If >80% of pages on this host are JS shells (after 10+ samples),
	// skip detection and go straight to browser rendering.
	if c.shouldForceBrowser(item.url) && c.scraper.HasBrowser() {
		page, rawHTML, err = c.scraper.BrowserScrape(c.ctx, item.url)
	} else {
		var result *scrape.FetchResult
		result, err = c.scraper.ScrapeConditional(c.ctx, item.url, "", "")
		if err == nil {
			page = result.Page
			rawHTML = result.RawHTML
			doc = result.Doc // may be nil when the page was re-fetched via browser
			c.recordJSDetection(item.url, result.JSDetection)
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

	c.pc.Put(item.url, page)
	c.fn(Result{
		Page:   page,
		Depth:  item.depth,
		Status: "new",
		Source: item.source,
		URL:    item.url,
	})

	if item.depth < c.opts.Depth && rawHTML != "" {
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

// fetchSitemap fetches a sitemap URL and returns all page URLs.
// Supports both sitemap index files and regular sitemaps.
func fetchSitemap(ctx context.Context, sitemapURL string) ([]string, error) {
	body, err := fetchBody(ctx, sitemapURL)
	if err != nil {
		return nil, err
	}

	var idx sitemapIndex
	if xml.Unmarshal(body, &idx) == nil && len(idx.Sitemaps) > 0 {
		return fetchSitemapIndex(ctx, idx)
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

func fetchSitemapIndex(ctx context.Context, idx sitemapIndex) ([]string, error) {
	var all []string
	for _, sm := range idx.Sitemaps {
		urls, err := fetchSitemap(ctx, sm.Loc)
		if err != nil {
			continue
		}
		all = append(all, urls...)
	}
	return all, nil
}

func fetchBody(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpx.Default().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}
	return io.ReadAll(io.LimitReader(resp.Body, scrape.MaxBodyBytes))
}
