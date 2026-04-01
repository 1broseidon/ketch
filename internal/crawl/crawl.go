package crawl

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/internal/cache"
	"github.com/1broseidon/ketch/internal/scrape"
	"github.com/PuerkitoBio/goquery"
)

// Options configures the crawl behavior.
type Options struct {
	Depth       int             // max BFS depth, default 3
	Concurrency int             // worker pool size, default 8
	Allow       []string        // path substrings that URLs must contain (any match passes)
	Deny        []string        // regex patterns to reject URLs
	BrowserBin  string          // browser binary for JS-rendered page fallback; empty = disabled
	StopCh      <-chan struct{} // closed to signal graceful stop; nil = no external stop
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
func Crawl(seed string, opts Options, pc *cache.Cache, sitemap bool, fn func(Result)) error {
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
		scraper:   s,
		pc:        pc,
		opts:      opts,
		seedHost:  seedURL.Hostname(),
		profile:   profileForSeed(seed),
		deny:      denyRegexps,
		visited:   make(map[string]bool),
		fn:        fn,
		hostStats: make(map[string]*hostJSStats),
	}

	return c.run(seed, sitemap)
}

type crawler struct {
	scraper  *scrape.Scraper
	pc       *cache.Cache
	opts     Options
	seedHost string
	profile  *hostProfile
	deny     []*regexp.Regexp

	visitMu sync.Mutex
	visited map[string]bool

	queueMu sync.Mutex
	queue   []queueItem
	cond    *sync.Cond
	active  int  // items currently being processed by workers
	done    bool // set when all work is complete
	fn      func(Result)

	hostStatsMu sync.Mutex
	hostStats   map[string]*hostJSStats
}

func (c *crawler) run(seed string, sitemap bool) error {
	c.cond = sync.NewCond(&c.queueMu)

	// Watch for external stop signal
	if c.opts.StopCh != nil {
		go func() {
			<-c.opts.StopCh
			c.shutdown()
		}()
	}

	workerCount := c.opts.Concurrency
	if c.profile != nil && c.profile.MaxConcurrency > 0 && workerCount > c.profile.MaxConcurrency {
		workerCount = c.profile.MaxConcurrency
	}
	if workerCount < 1 {
		workerCount = 1
	}

	// Start workers
	var workerWg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			c.workerLoop()
		}()
	}

	// Enqueue seeds
	if sitemap {
		urls, sErr := fetchSitemap(seed)
		if sErr != nil {
			c.shutdown()
			workerWg.Wait()
			return fmt.Errorf("sitemap fetch failed: %w", sErr)
		}
		for _, u := range urls {
			c.enqueue(u, 0, "sitemap")
		}
	} else {
		c.enqueue(seed, 0, "seed")
		c.enqueueProfileSeeds(seed)
	}

	// Wait until there are no queued items and no active workers
	c.queueMu.Lock()
	for len(c.queue) > 0 || c.active > 0 {
		c.cond.Wait()
	}
	c.done = true
	c.queueMu.Unlock()
	c.cond.Broadcast()

	workerWg.Wait()
	return nil
}

func (c *crawler) shutdown() {
	c.queueMu.Lock()
	c.done = true
	c.queue = nil
	c.queueMu.Unlock()
	c.cond.Broadcast()
}

func (c *crawler) workerLoop() {
	for {
		c.queueMu.Lock()
		for len(c.queue) == 0 && !c.done {
			c.cond.Wait()
		}
		if c.done && len(c.queue) == 0 {
			c.queueMu.Unlock()
			return
		}
		item := c.queue[0]
		c.queue = c.queue[1:]
		c.active++
		c.queueMu.Unlock()

		c.processItem(item)

		c.queueMu.Lock()
		c.active--
		c.queueMu.Unlock()
		c.cond.Broadcast()
	}
}

func (c *crawler) enqueue(rawURL string, depth int, source string) {
	norm := normalizeURL(rawURL)
	if norm == "" {
		return
	}
	if !c.tryVisit(norm) {
		return
	}
	if !c.allowsURL(norm) {
		return
	}
	c.queueMu.Lock()
	if c.done {
		c.queueMu.Unlock()
		return
	}
	c.queue = append(c.queue, queueItem{url: norm, depth: depth, source: source})
	c.queueMu.Unlock()
	c.cond.Signal()
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
	lookup := c.pc.Lookup(item.url)
	var cached *scrape.Page
	if lookup != nil {
		cached = lookup.Page
	}

	// Cache hit: use cached page, skip fetch entirely.
	// Use --no-cache to force re-fetch for change detection.
	if lookup != nil && lookup.Fresh && cached != nil {
		c.enqueueLinksFromPage(item, cached)
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
	var err error

	// If >80% of pages on this host are JS shells (after 10+ samples),
	// skip detection and go straight to browser rendering for uncached pages.
	if cached == nil && c.shouldForceBrowser(item.url) && c.scraper.HasBrowser() {
		page, rawHTML, err = c.scraper.BrowserScrape(item.url)
	} else {
		var result *scrape.FetchResult
		etag := ""
		lastModified := ""
		if cached != nil {
			etag = cached.ETag
			lastModified = cached.LastModified
		}
		result, err = c.scraper.ScrapeConditional(item.url, etag, lastModified)
		if err == nil {
			if result.NotModified && cached != nil {
				c.pc.Put(item.url, cached)
				c.enqueueLinksFromPage(item, cached)
				c.fn(Result{
					Page:   cached,
					Depth:  item.depth,
					Status: "unchanged",
					Source: item.source,
					URL:    item.url,
				})
				return
			}
			page = result.Page
			rawHTML = result.RawHTML
			c.recordJSDetection(item.url, result.JSDetection)
			if shouldRejectPlaceholder(result, c.scraper.HasBrowser()) {
				c.fn(Result{
					URL:    item.url,
					Depth:  item.depth,
					Source: item.source,
					Error:  "page requires browser rendering for useful content",
				})
				return
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

	c.pc.Put(item.url, page)
	status := "new"
	if cached != nil {
		if cached.ContentHash == page.ContentHash {
			status = "unchanged"
		} else {
			status = "changed"
		}
	}
	c.fn(Result{
		Page:   page,
		Depth:  item.depth,
		Status: status,
		Source: item.source,
		URL:    item.url,
	})
	if redirectTarget := redirectTargetForPage(item.url, page); redirectTarget != "" {
		c.enqueue(redirectTarget, item.depth, "redirect")
		return
	}

	if item.depth < c.opts.Depth && rawHTML != "" {
		c.enqueueLinksFromHTML(item, rawHTML)
		if c.profile != nil && c.profile.DiscoverHTML != nil {
			for _, discovered := range c.profile.DiscoverHTML(rawHTML) {
				c.enqueue(discovered, item.depth+1, "profile")
			}
		}
	}
}

func redirectTargetForPage(currentURL string, page *scrape.Page) string {
	if page == nil || page.Quality == nil || page.Quality.ContentType != "soft_redirect" {
		return ""
	}
	target := normalizeURL(page.Quality.RedirectTarget)
	if target == "" || target == normalizeURL(currentURL) {
		return ""
	}
	return target
}

func shouldRejectPlaceholder(result *scrape.FetchResult, hasBrowser bool) bool {
	if result == nil || result.Page == nil || hasBrowser {
		return false
	}
	if result.JSDetection != "likely_shell" {
		return false
	}
	markdown := strings.TrimSpace(result.Page.Markdown)
	if markdown == "" {
		return true
	}
	words := strings.Fields(markdown)
	if len(words) > 5 {
		return false
	}
	combined := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		result.Page.Title,
		result.Page.Summary,
		markdown,
	}, " ")))
	return strings.Contains(combined, "loading") || strings.Contains(combined, "javascript")
}

func (c *crawler) enqueueProfileSeeds(seed string) {
	if c.profile == nil {
		return
	}
	for _, candidate := range c.profile.DefaultSeeds {
		if candidate == seed {
			continue
		}
		c.enqueue(candidate, 0, "profile")
	}
	for _, sitemapURL := range c.profile.SitemapSeedURLs {
		urls, err := fetchSitemap(sitemapURL)
		if err != nil {
			continue
		}
		for _, candidate := range urls {
			c.enqueue(candidate, 0, "sitemap")
		}
	}
	if c.profile.TOCURL != "" {
		body, err := fetchBody(c.profile.TOCURL)
		if err == nil {
			base := seed
			if len(c.profile.DefaultSeeds) > 0 {
				base = c.profile.DefaultSeeds[0]
			}
			if !strings.HasSuffix(base, "/") {
				base += "/"
			}
			for _, candidate := range discoverTOCURLs(string(body), base) {
				c.enqueue(candidate, 0, "profile")
			}
		}
	}
}

func (c *crawler) allowsURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	for _, re := range c.deny {
		if re.MatchString(rawURL) {
			return false
		}
	}
	if len(c.opts.Allow) > 0 {
		matched := false
		for _, sub := range c.opts.Allow {
			if strings.Contains(strings.ToLower(u.Path), strings.ToLower(sub)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if c.profile != nil && c.profile.AllowURL != nil {
		return c.profile.AllowURL(u)
	}
	return u.Hostname() == c.seedHost
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

func (c *crawler) enqueueLinksFromHTML(parent queueItem, html string) {
	links := extractLinksFromHTML(parent.url, html)
	for _, link := range links {
		if !c.allowsDiscoveredLink(parent.url, link) {
			continue
		}
		c.enqueue(link, parent.depth+1, "link")
	}
}

func (c *crawler) enqueueLinksFromPage(parent queueItem, page *scrape.Page) {
	if page == nil || parent.depth >= c.opts.Depth {
		return
	}
	for _, link := range page.Links {
		if !c.allowsDiscoveredLink(parent.url, link) {
			continue
		}
		c.enqueue(link, parent.depth+1, "link")
	}
}

func (c *crawler) allowsDiscoveredLink(parentURL, childURL string) bool {
	if c.profile == nil || c.profile.AllowDiscovered == nil {
		return true
	}
	parent, err := url.Parse(parentURL)
	if err != nil {
		return false
	}
	child, err := url.Parse(childURL)
	if err != nil {
		return false
	}
	return c.profile.AllowDiscovered(parent, child)
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

// extractLinksFromHTML parses HTML and returns all resolved href links.
func extractLinksFromHTML(pageURL, html string) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
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
func fetchSitemap(sitemapURL string) ([]string, error) {
	body, err := fetchBody(sitemapURL)
	if err != nil {
		return nil, err
	}

	// Try as sitemap index first
	var idx sitemapIndex
	if xml.Unmarshal(body, &idx) == nil && len(idx.Sitemaps) > 0 {
		return fetchSitemapIndex(idx)
	}

	// Try as regular urlset
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

func fetchSitemapIndex(idx sitemapIndex) ([]string, error) {
	var all []string
	for _, sm := range idx.Sitemaps {
		urls, err := fetchSitemap(sm.Loc)
		if err != nil {
			continue
		}
		all = append(all, urls...)
	}
	return all, nil
}

func fetchBody(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ketch/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml,application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}
	return io.ReadAll(resp.Body)
}
