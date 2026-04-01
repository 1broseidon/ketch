package scrape

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/internal/extract"
)

// Page represents a scraped web page.
type Page struct {
	URL              string            `json:"url"`
	FinalURL         string            `json:"final_url,omitempty"`
	Title            string            `json:"title"`
	Summary          string            `json:"summary,omitempty"`
	CanonicalURL     string            `json:"canonical_url,omitempty"`
	Headings         []extract.Heading `json:"headings,omitempty"`
	Links            []string          `json:"links,omitempty"`
	Quality          *extract.Quality  `json:"quality,omitempty"`
	ExtractionSource string            `json:"extraction_source,omitempty"`
	Markdown         string            `json:"markdown"`
	ETag             string            `json:"etag,omitempty"`
	LastModified     string            `json:"last_modified,omitempty"`
	ContentHash      string            `json:"content_hash,omitempty"`
}

// FetchResult holds the output of a conditional scrape.
type FetchResult struct {
	Page        *Page
	RawHTML     string
	NotModified bool
	JSDetection string // "static", "likely_shell", "ambiguous"
}

// Scraper fetches web pages and extracts content as markdown.
type Scraper struct {
	client     *http.Client
	extractor  *extract.Extractor
	browserBin string
	browserMu  sync.Mutex
	browser    BrowserConn
	pacingMu   sync.Mutex
	nextByHost map[string]time.Time
	now        func() time.Time
	sleep      func(time.Duration)
}

const max429Retries = 5

type hostRequestPolicy struct {
	MinInterval   time.Duration
	Max429Retries int
	MaxRetryAfter time.Duration
}

// New creates a Scraper with defaults.
func New() *Scraper {
	return &Scraper{
		client:    &http.Client{},
		extractor: extract.New(),
		nextByHost: make(map[string]time.Time),
		now:       time.Now,
		sleep:     time.Sleep,
	}
}

// NewWithBrowser creates a Scraper with browser fallback for JS-rendered pages.
func NewWithBrowser(browserBin string) *Scraper {
	return &Scraper{
		client:     &http.Client{},
		extractor:  extract.New(),
		browserBin: browserBin,
		nextByHost: make(map[string]time.Time),
		now:        time.Now,
		sleep:      time.Sleep,
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
func (s *Scraper) Scrape(rawURL string) (*Page, error) {
	body, err := s.fetch(rawURL)
	if err != nil {
		return nil, err
	}

	body = s.maybeBrowserFetch(rawURL, body)

	result, err := s.extractor.Extract(rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("extraction failed for %s: %w", rawURL, err)
	}

	return &Page{
		URL:              rawURL,
		Title:            result.Title,
		Summary:          result.Summary,
		CanonicalURL:     result.CanonicalURL,
		Headings:         result.Headings,
		Links:            result.Links,
		Quality:          result.Quality,
		ExtractionSource: result.ExtractionSource,
		Markdown:         result.Markdown,
	}, nil
}

// ScrapeConditional fetches a URL with conditional headers and JS detection.
func (s *Scraper) ScrapeConditional(rawURL, etag, lastModified string) (*FetchResult, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
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

	resp, err := s.doRequest(req)
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

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}

	html := string(b)
	detection := extract.DetectJSShell(html)

	if detection == "likely_shell" {
		html = s.browserFetchOrWarn(rawURL, html)
	}

	result, err := s.extractor.Extract(rawURL, html)
	if err != nil {
		return nil, fmt.Errorf("extraction failed for %s: %w", rawURL, err)
	}

	return &FetchResult{
		Page: &Page{
			URL:              rawURL,
			FinalURL:         resp.Request.URL.String(),
			Title:            result.Title,
			Summary:          result.Summary,
			CanonicalURL:     result.CanonicalURL,
			Headings:         result.Headings,
			Links:            result.Links,
			Quality:          result.Quality,
			ExtractionSource: result.ExtractionSource,
			Markdown:         result.Markdown,
			ETag:             resp.Header.Get("ETag"),
			LastModified:     resp.Header.Get("Last-Modified"),
			ContentHash:      ContentHash(result.Markdown),
		},
		RawHTML:     html,
		JSDetection: detection,
	}, nil
}

// BrowserScrape fetches a URL using the browser directly.
// Used when a host is known to require browser rendering.
func (s *Scraper) BrowserScrape(rawURL string) (*Page, string, error) {
	browser := s.getBrowser()
	if browser == nil {
		return nil, "", ErrNoBrowser
	}
	html, err := browser.Fetch(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("browser fetch failed for %s: %w", rawURL, err)
	}
	result, err := s.extractor.Extract(rawURL, html)
	if err != nil {
		return nil, "", fmt.Errorf("extraction failed for %s: %w", rawURL, err)
	}
	page := &Page{
		URL:              rawURL,
		FinalURL:         rawURL,
		Title:            result.Title,
		Summary:          result.Summary,
		CanonicalURL:     result.CanonicalURL,
		Headings:         result.Headings,
		Links:            result.Links,
		Quality:          result.Quality,
		ExtractionSource: result.ExtractionSource,
		Markdown:         result.Markdown,
		ContentHash:      ContentHash(result.Markdown),
	}
	return page, html, nil
}

func (s *Scraper) maybeBrowserFetch(rawURL, html string) string {
	detection := extract.DetectJSShell(html)
	if detection != "likely_shell" {
		return html
	}
	return s.browserFetchOrWarn(rawURL, html)
}

func (s *Scraper) browserFetchOrWarn(rawURL, html string) string {
	browser := s.getBrowser()
	if browser != nil {
		rendered, err := browser.Fetch(rawURL)
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

func (s *Scraper) fetch(rawURL string) (string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ketch/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.doRequest(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body failed: %w", err)
	}

	return string(b), nil
}

func (s *Scraper) doRequest(req *http.Request) (*http.Response, error) {
	host := ""
	if req != nil && req.URL != nil {
		host = req.URL.Hostname()
	}
	policy := requestPolicyForHost(host)
	retryLimit := max429Retries
	if policy.Max429Retries > 0 {
		retryLimit = policy.Max429Retries
	}
	var lastResp *http.Response
	for attempt := 0; attempt <= retryLimit; attempt++ {
		request := req.Clone(req.Context())
		s.waitForHostSlot(host)
		resp, err := s.client.Do(request)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		lastResp = resp
		if attempt == retryLimit {
			return resp, nil
		}

		waitFor := retryAfterDelay(resp.Header.Get("Retry-After"), attempt, policy.MaxRetryAfter)
		s.extendHostCooldown(host, waitFor)
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		s.sleep(waitFor)
	}

	return lastResp, nil
}

func (s *Scraper) waitForHostSlot(host string) {
	policy := requestPolicyForHost(host)
	if policy.MinInterval <= 0 {
		return
	}

	normalizedHost := normalizeHost(host)
	now := s.now()
	var waitFor time.Duration

	s.pacingMu.Lock()
	next := s.nextByHost[normalizedHost]
	if next.After(now) {
		waitFor = next.Sub(now)
		now = next
	}
	s.nextByHost[normalizedHost] = now.Add(policy.MinInterval)
	s.pacingMu.Unlock()

	if waitFor > 0 {
		s.sleep(waitFor)
	}
}

func (s *Scraper) extendHostCooldown(host string, delay time.Duration) {
	if delay <= 0 {
		return
	}
	normalizedHost := normalizeHost(host)
	if normalizedHost == "" {
		return
	}
	candidate := s.now().Add(delay)
	s.pacingMu.Lock()
	if current := s.nextByHost[normalizedHost]; current.Before(candidate) {
		s.nextByHost[normalizedHost] = candidate
	}
	s.pacingMu.Unlock()
}

func requestPolicyForHost(host string) hostRequestPolicy {
	normalizedHost := normalizeHost(host)
	switch {
	case normalizedHost == "docs.pingidentity.com", strings.HasSuffix(normalizedHost, ".docs.pingidentity.com"):
		return hostRequestPolicy{MinInterval: 200 * time.Millisecond, Max429Retries: 0, MaxRetryAfter: time.Second}
	default:
		return hostRequestPolicy{}
	}
}

func normalizeHost(host string) string {
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil {
			host = parsed.Hostname()
		}
	}
	return strings.ToLower(strings.TrimSpace(host))
}

func retryAfterDelay(header string, attempt int, maxDelay time.Duration) time.Duration {
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	trimmed := header
	if seconds, err := strconv.Atoi(trimmed); err == nil {
		if seconds < 1 {
			seconds = 1
		}
		delay := time.Duration(seconds) * time.Second
		if delay > maxDelay {
			return maxDelay
		}
		return delay
	}
	if when, err := http.ParseTime(trimmed); err == nil {
		delay := time.Until(when)
		if delay < time.Second {
			return time.Second
		}
		if delay > maxDelay {
			return maxDelay
		}
		return delay
	}
	base := time.Duration(1<<attempt) * time.Second
	if base > maxDelay {
		return maxDelay
	}
	return base
}
