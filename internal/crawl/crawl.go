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

	c := &crawler{
		scraper:  scrape.New(),
		pc:       pc,
		opts:     opts,
		seedHost: seedURL.Hostname(),
		deny:     denyRegexps,
		visited:  make(map[string]bool),
		fn:       fn,
	}

	return c.run(seed, sitemap)
}

type crawler struct {
	scraper  *scrape.Scraper
	pc       *cache.Cache
	opts     Options
	seedHost string
	deny     []*regexp.Regexp

	visitMu sync.Mutex
	visited map[string]bool

	queueMu sync.Mutex
	queue   []queueItem
	cond    *sync.Cond
	active  int  // items currently being processed by workers
	done    bool // set when all work is complete
	fn      func(Result)
}

func (c *crawler) run(seed string, sitemap bool) error {
	c.cond = sync.NewCond(&c.queueMu)

	// Start workers
	var workerWg sync.WaitGroup
	for i := 0; i < c.opts.Concurrency; i++ {
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
	if !passesFilters(norm, c.seedHost, c.opts.Allow, c.deny) {
		return
	}
	c.queueMu.Lock()
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
	cached := c.pc.Get(item.url)
	var oldHash string
	if cached != nil {
		oldHash = cached.ContentHash
	}

	// Crawl always does a full fetch — we need the HTML for link extraction.
	// Change detection uses content hash comparison, not HTTP 304.
	page, rawHTML, _, err := c.scraper.ScrapeConditional(item.url, "", "")
	if err != nil {
		c.fn(Result{
			URL:    item.url,
			Depth:  item.depth,
			Source: item.source,
			Error:  err.Error(),
		})
		return
	}

	status := determineStatus(oldHash, page.ContentHash)
	c.pc.Put(item.url, page)
	c.fn(Result{
		Page:   page,
		Depth:  item.depth,
		Status: status,
		Source: item.source,
		URL:    item.url,
	})

	if item.depth < c.opts.Depth && rawHTML != "" {
		c.enqueueLinksFromHTML(item, rawHTML)
	}
}

func (c *crawler) enqueueLinksFromHTML(parent queueItem, html string) {
	links := extractLinksFromHTML(parent.url, html)
	for _, link := range links {
		c.enqueue(link, parent.depth+1, "link")
	}
}

func determineStatus(oldHash, newHash string) string {
	if oldHash == "" {
		return "new"
	}
	if oldHash == newHash {
		return "unchanged"
	}
	return "changed"
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
	resp, err := http.Get(rawURL) //nolint:errcheck
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}
	return io.ReadAll(resp.Body)
}
