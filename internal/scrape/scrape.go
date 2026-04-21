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

	"github.com/1broseidon/ketch/internal/extract"
	"github.com/1broseidon/ketch/internal/httpx"
	"github.com/PuerkitoBio/goquery"
)

// MaxBodyBytes caps how much of an HTTP response body we will read.
// Prevents a malicious or misconfigured server from OOMing the process.
const MaxBodyBytes = 20 << 20 // 20 MiB

// Page represents a scraped web page.
type Page struct {
	URL          string `json:"url"`
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
}

// Scraper fetches web pages and extracts content as markdown.
type Scraper struct {
	client     *http.Client
	extractor  *extract.Extractor
	browserBin string
	browserMu  sync.Mutex
	browser    BrowserConn
}

// New creates a Scraper with defaults.
func New() *Scraper {
	return &Scraper{
		client:    httpx.Default(),
		extractor: extract.New(),
	}
}

// NewWithBrowser creates a Scraper with browser fallback for JS-rendered pages.
func NewWithBrowser(browserBin string) *Scraper {
	return &Scraper{
		client:     httpx.Default(),
		extractor:  extract.New(),
		browserBin: browserBin,
	}
}

// HasBrowser reports whether this scraper has browser fallback configured.
func (s *Scraper) HasBrowser() bool {
	return s.browserBin != ""
}

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

// Scrape fetches a URL and returns extracted markdown content.
// If the page appears JS-rendered and a browser is configured, automatically
// retries with the browser for full content extraction.
func (s *Scraper) Scrape(ctx context.Context, rawURL string) (*Page, error) {
	body, err := s.Fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	body = s.MaybeBrowserFetch(ctx, rawURL, body)

	result, err := s.extractor.Extract(rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("extraction failed for %s: %w", rawURL, err)
	}

	return &Page{
		URL:      rawURL,
		Title:    result.Title,
		Markdown: result.Markdown,
	}, nil
}

// ScrapeConditional fetches a URL with conditional headers and JS detection.
func (s *Scraper) ScrapeConditional(ctx context.Context, rawURL, etag, lastModified string) (*FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
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
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
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

	if detection == "likely_shell" {
		html = s.browserFetchOrWarn(ctx, rawURL, html)
		doc = nil // rendered HTML needs a fresh parse downstream
	}

	result, err := s.extractor.Extract(rawURL, html)
	if err != nil {
		return nil, fmt.Errorf("extraction failed for %s: %w", rawURL, err)
	}

	return &FetchResult{
		Doc: doc,
		Page: &Page{
			URL:          rawURL,
			Title:        result.Title,
			Markdown:     result.Markdown,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
			ContentHash:  ContentHash(result.Markdown),
		},
		RawHTML:     html,
		JSDetection: detection,
	}, nil
}

// BrowserScrape fetches a URL using the browser directly.
// Used when a host is known to require browser rendering.
func (s *Scraper) BrowserScrape(ctx context.Context, rawURL string) (*Page, string, error) {
	browser := s.getBrowser()
	if browser == nil {
		return nil, "", ErrNoBrowser
	}
	html, err := browser.Fetch(ctx, rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("browser fetch failed for %s: %w", rawURL, err)
	}
	result, err := s.extractor.Extract(rawURL, html)
	if err != nil {
		return nil, "", fmt.Errorf("extraction failed for %s: %w", rawURL, err)
	}
	page := &Page{
		URL:         rawURL,
		Title:       result.Title,
		Markdown:    result.Markdown,
		ContentHash: ContentHash(result.Markdown),
	}
	return page, html, nil
}

// MaybeBrowserFetch re-fetches rawURL via the browser if html looks JS-rendered.
// Returns the original html if no browser is needed or available.
func (s *Scraper) MaybeBrowserFetch(ctx context.Context, rawURL, html string) string {
	detection := extract.DetectJSShell(html)
	if detection != "likely_shell" {
		return html
	}
	return s.browserFetchOrWarn(ctx, rawURL, html)
}

func (s *Scraper) browserFetchOrWarn(ctx context.Context, rawURL, html string) string {
	browser := s.getBrowser()
	if browser != nil {
		rendered, err := browser.Fetch(ctx, rawURL)
		if err == nil {
			return rendered
		}
		fmt.Fprintf(os.Stderr, "warn: browser fallback failed for %s: %v\n", rawURL, err)
	} else if s.browserBin == "" {
		fmt.Fprintf(os.Stderr, "warn: %s appears JS-rendered; configure browser for full content\n", rawURL)
	}
	return html
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
