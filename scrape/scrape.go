package scrape

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/urlrewrite"
	"github.com/PuerkitoBio/goquery"
)

// MaxBodyBytes caps how much of an HTTP response body we will read.
// Prevents a malicious or misconfigured server from OOMing the process.
const MaxBodyBytes = 20 << 20 // 20 MiB

// Source identifies how a Page was fetched. Stored in the cache so we can
// invalidate stale entries (notably JS-shell extractions cached before the
// user configured a browser) without churning entries for plain HTTP pages
// that don't need rendering.
const (
	// SourceHTTP — page is not a JS shell; plain HTTP fetch is authoritative.
	SourceHTTP = "http"
	// SourceHTTPShell — page is a JS shell but rendering wasn't possible
	// (no browser configured, or browser fetch failed). Always invalid as a
	// cache hit when a browser is now available, so we retry rendering.
	SourceHTTPShell = "http_shell"
	// SourceBrowser — page was rendered via the headless browser.
	SourceBrowser = "browser"
)

// Page represents a scraped web page.
type Page struct {
	URL          string `json:"url"`
	FetchedURL   string `json:"fetched_url,omitempty"`
	Title        string `json:"title"`
	Markdown     string `json:"markdown"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	ContentHash  string `json:"content_hash,omitempty"`
}

// FetchResult holds the output of a conditional scrape.
type FetchResult struct {
	Page        *Page
	RawHTML     string
	NotModified bool
	JSDetection string // "static", "likely_shell", "ambiguous"

	// Doc is the parsed document that ScrapeConditional used for JS-shell
	// detection. Downstream callers (e.g. link extraction during a crawl)
	// can reuse it to avoid re-parsing the same HTML. Nil when the page
	// was re-fetched via the browser — in that case RawHTML is the
	// rendered HTML and needs a fresh parse.
	Doc *goquery.Document

	// Source is the fetch path that produced Page (SourceHTTP or SourceBrowser).
	Source string
}

// Scraper fetches web pages and extracts content as markdown.
type Scraper struct {
	client     *http.Client
	extractor  *extract.Extractor
	browserBin string
	browserMu  sync.Mutex
	browser    BrowserConn
	rewriter   *urlrewrite.Rewriter
}

// NewWithRewriter creates a Scraper with an optional browser binary and
// optional URL rewriter. Pass "" for browserBin to disable browser fallback;
// pass nil rewriter to disable URL rewriting.
func NewWithRewriter(browserBin string, rw *urlrewrite.Rewriter) *Scraper {
	return &Scraper{
		client:     httpx.Default(),
		extractor:  extract.New(),
		browserBin: browserBin,
		rewriter:   rw,
	}
}

// New creates a Scraper with defaults.
func New() *Scraper { return NewWithRewriter("", nil) }

// NewWithBrowser creates a Scraper with browser fallback for JS-rendered pages.
func NewWithBrowser(browserBin string) *Scraper { return NewWithRewriter(browserBin, nil) }

// NewWithBrowserConn creates a Scraper backed by a pre-supplied BrowserConn,
// bypassing binary resolution. HasBrowser reports true and getBrowser returns
// conn directly. Used to drive browser code paths (e.g. --force-browser)
// without a real Chrome install.
func NewWithBrowserConn(conn BrowserConn, rw *urlrewrite.Rewriter) *Scraper {
	s := NewWithRewriter("chrome", rw)
	s.browser = conn
	return s
}

// HasBrowser reports whether this scraper has browser fallback configured.
func (s *Scraper) HasBrowser() bool {
	return s.browserBin != ""
}

// Rewrite returns the URL after applying configured rewrite rules, or the
// original URL if no rule matches or no rewriter is configured. Safe to
// call on a Scraper with no rewriter (nil-safe via urlrewrite.Rewriter).
func (s *Scraper) Rewrite(url string) string { return s.rewriter.Apply(url) }

// Close releases browser resources if any.
func (s *Scraper) Close() {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if s.browser != nil {
		s.browser.Close()
		s.browser = nil
	}
}

func (s *Scraper) getBrowser() BrowserConn {
	if s.browserBin == "" {
		return nil
	}
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if s.browser != nil {
		return s.browser
	}
	bin, err := ResolveBrowserBin(s.browserBin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: cannot resolve browser %q: %v\n", s.browserBin, err)
		s.browserBin = ""
		return nil
	}
	conn, err := NewBrowserConn(bin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: browser init failed: %v\n", err)
		s.browserBin = ""
		return nil
	}
	s.browser = conn
	return s.browser
}

// Scrape fetches a URL and returns extracted markdown content along with the
// fetch source (SourceHTTP or SourceBrowser). If the page appears JS-rendered
// and a browser is configured, automatically retries with the browser for full
// content extraction.
func (s *Scraper) Scrape(ctx context.Context, rawURL string) (*Page, string, error) {
	fetchURL := s.Rewrite(rawURL)

	body, err := s.Fetch(ctx, fetchURL)
	if err != nil {
		return nil, "", err
	}

	body, source := s.MaybeBrowserFetch(ctx, fetchURL, body)

	result, err := s.extractor.Extract(fetchURL, body)
	if err != nil {
		return nil, "", fmt.Errorf("extraction failed for %s: %w", fetchURL, err)
	}

	p := &Page{
		URL:      rawURL,
		Title:    result.Title,
		Markdown: result.Markdown,
	}
	if fetchURL != rawURL {
		p.FetchedURL = fetchURL
	}
	return p, source, nil
}

// ScrapeConditional fetches a URL with conditional headers and JS detection.
func (s *Scraper) ScrapeConditional(ctx context.Context, rawURL, etag, lastModified string) (*FetchResult, error) {
	fetchURL := s.Rewrite(rawURL)

	req, err := http.NewRequestWithContext(ctx, "GET", fetchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ketch/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return &FetchResult{NotModified: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, fetchURL)
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}

	html := string(b)

	// Parse once for JS-shell detection; downstream callers can reuse this
	// doc via FetchResult.Doc instead of paying to re-parse the same HTML.
	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(html))
	var detection string
	if parseErr != nil {
		detection = "ambiguous"
	} else {
		detection = extract.DetectJSShellFromDoc(doc, html)
	}

	source := SourceHTTP
	if detection == "likely_shell" {
		html, source = s.browserFetchOrWarn(ctx, fetchURL, html)
		doc = nil // rendered HTML needs a fresh parse downstream
	}

	result, err := s.extractor.Extract(fetchURL, html)
	if err != nil {
		return nil, fmt.Errorf("extraction failed for %s: %w", fetchURL, err)
	}

	page := &Page{
		URL:          rawURL,
		Title:        result.Title,
		Markdown:     result.Markdown,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		ContentHash:  ContentHash(result.Markdown),
	}
	if fetchURL != rawURL {
		page.FetchedURL = fetchURL
	}

	return &FetchResult{
		Doc:         doc,
		Page:        page,
		RawHTML:     html,
		JSDetection: detection,
		Source:      source,
	}, nil
}

// BrowserScrape fetches a URL using the browser directly.
// Used when a host is known to require browser rendering.
func (s *Scraper) BrowserScrape(ctx context.Context, rawURL string) (*Page, string, error) {
	browser := s.getBrowser()
	if browser == nil {
		return nil, "", ErrNoBrowser
	}
	fetchURL := s.Rewrite(rawURL)
	html, err := browser.Fetch(ctx, fetchURL)
	if err != nil {
		return nil, "", fmt.Errorf("browser fetch failed for %s: %w", fetchURL, err)
	}
	result, err := s.extractor.Extract(fetchURL, html)
	if err != nil {
		return nil, "", fmt.Errorf("extraction failed for %s: %w", fetchURL, err)
	}
	page := &Page{
		URL:         rawURL,
		Title:       result.Title,
		Markdown:    result.Markdown,
		ContentHash: ContentHash(result.Markdown),
	}
	if fetchURL != rawURL {
		page.FetchedURL = fetchURL
	}
	return page, html, nil
}

// MaybeBrowserFetch re-fetches rawURL via the browser if html looks JS-rendered.
// Returns the (possibly rendered) html and the fetch source actually used.
func (s *Scraper) MaybeBrowserFetch(ctx context.Context, rawURL, html string) (string, string) {
	detection := extract.DetectJSShell(html)
	if detection != "likely_shell" {
		return html, SourceHTTP
	}
	return s.browserFetchOrWarn(ctx, rawURL, html)
}

// browserFetchOrWarn returns the rendered html with SourceBrowser on success,
// or the original (shell) html with SourceHTTPShell if the browser is
// unavailable or fails. Called only when detection said "likely_shell", so
// returning SourceHTTP here would be a lie — the entry would round-trip as
// "plain page" and never get retried once a browser is configured.
func (s *Scraper) browserFetchOrWarn(ctx context.Context, rawURL, html string) (string, string) {
	browser := s.getBrowser()
	if browser != nil {
		rendered, err := browser.Fetch(ctx, rawURL)
		if err == nil {
			return rendered, SourceBrowser
		}
		fmt.Fprintf(os.Stderr, "warn: browser fallback failed for %s: %v\n", rawURL, err)
	} else if s.browserBin == "" {
		fmt.Fprintf(os.Stderr, "warn: %s appears JS-rendered; configure browser for full content\n", rawURL)
	}
	return html, SourceHTTPShell
}

// CacheStaleForBrowser reports whether a cache entry with the given source
// should be bypassed because rendering via the browser would do better.
// True when the cached entry is a known unrendered JS shell, or when it
// predates source tracking and a browser is now available (one-time migration
// for pre-source caches where the entry might be unrendered shell garbage).
func CacheStaleForBrowser(source string, hasBrowser bool) bool {
	if source == SourceHTTPShell {
		return true
	}
	if source == "" && hasBrowser {
		return true
	}
	return false
}

// ContentHash returns the first 16 hex chars of the sha256 of s.
func ContentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

// Fetch fetches the raw HTML for a URL without extraction or browser fallback.
func (s *Scraper) Fetch(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ketch/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read body failed: %w", err)
	}

	return string(b), nil
}
