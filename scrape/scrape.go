package scrape

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/cookies"
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

// FetchedContent preserves the response body bytes and declared content type.
type FetchedContent struct {
	Body        []byte
	ContentType string
}

// FetchResult holds the output of a conditional scrape.
type FetchResult struct {
	Page        *Page
	RawHTML     string
	ContentType string
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
	client       *http.Client
	extractor    *extract.Extractor
	pdfExtractor extract.PDFExtractor
	detector     *extract.Detector
	browserBin   string
	browserMu    sync.Mutex
	browser      BrowserConn
	rewriter     *urlrewrite.Rewriter
	jar          *cookies.Jar
	userAgent    string
	// userAgentConfigured records that the operator explicitly set user_agent
	// (config, env, or flag) instead of riding the built-in default. Only an
	// explicitly configured UA reaches the headless browser; when unset, the
	// browser keeps its own User-Agent minus the HeadlessChrome token (see
	// NewBrowserConnOptions).
	userAgentConfigured bool
}

// NewWithRewriter creates a Scraper with an optional browser binary and
// optional URL rewriter. Pass "" for browserBin to disable browser fallback;
// pass nil rewriter to disable URL rewriting. The detector uses only the
// built-in SPA markers; use NewWithConfig to add operator-configured markers.
func NewWithRewriter(browserBin string, rw *urlrewrite.Rewriter) *Scraper {
	return &Scraper{
		client:       httpx.Default(),
		extractor:    extract.New(),
		pdfExtractor: extract.NewPDFExtractor(),
		detector:     extract.NewDetector(nil),
		browserBin:   browserBin,
		rewriter:     rw,
		userAgent:    DefaultUserAgent(),
	}
}

// NewWithConfig creates a Scraper like NewWithRewriter but folds operator-
// configured SPA markers (config spa_markers) into the JS-shell detector in
// addition to the built-in markers. Pass nil/empty spaMarkers for built-ins
// only.
func NewWithConfig(browserBin string, rw *urlrewrite.Rewriter, spaMarkers []string) *Scraper {
	s := NewWithRewriter(browserBin, rw)
	s.detector = extract.NewDetector(spaMarkers)
	return s
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
// Guarded by browserMu because getBrowser clears browserBin on failed
// resolution — concurrent callers (MCP tool calls, multi-URL scrapes) must
// not race that write.
func (s *Scraper) HasBrowser() bool {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
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
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if s.browserBin == "" {
		return nil
	}
	if s.browser != nil {
		return s.browser
	}
	bin, err := ResolveBrowserBin(s.browserBin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: cannot resolve browser %q: %v\n", s.browserBin, err)
		s.browserBin = ""
		return nil
	}
	conn, err := NewBrowserConnOptions(bin, s.jar, s.browserConnOptions()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: browser init failed: %v\n", err)
		s.browserBin = ""
		return nil
	}
	s.browser = conn
	return s.browser
}

// browserConnOptions returns the launch-time options for the headless browser.
// The operator's user_agent (config, env, or --user-agent flag) applies to the
// browser path exactly like the HTTP path; when nothing is configured, no UA
// option is passed and the launch keeps the browser's own User-Agent with the
// HeadlessChrome token stripped.
func (s *Scraper) browserConnOptions() []ConnOption {
	if !s.userAgentConfigured {
		return nil
	}
	return []ConnOption{WithUserAgent(s.userAgent)}
}

// Scrape fetches a URL and returns extracted markdown content along with the
// fetch source (SourceHTTP or SourceBrowser). If the page appears JS-rendered
// and a browser is configured, automatically retries with the browser for full
// content extraction.
func (s *Scraper) Scrape(ctx context.Context, rawURL string) (*Page, string, error) {
	fetchURL := s.Rewrite(rawURL)

	content, err := s.FetchContent(ctx, fetchURL)
	if err != nil {
		return nil, "", err
	}

	contentType := effectiveContentType(content.ContentType, content.Body)
	if contentType == "application/pdf" {
		markdown, err := s.pdfExtractor.Extract(ctx, content.Body)
		if err != nil {
			return nil, "", fmt.Errorf("PDF extraction failed for %s: %w", fetchURL, err)
		}
		page := &Page{URL: rawURL, Markdown: markdown}
		if fetchURL != rawURL {
			page.FetchedURL = fetchURL
		}
		return page, SourceHTTP, nil
	}

	html, source := s.MaybeBrowserFetch(ctx, fetchURL, string(content.Body))
	result, err := s.extractor.Extract(fetchURL, html)
	if err != nil {
		return nil, "", fmt.Errorf("extraction failed for %s: %w", fetchURL, err)
	}

	page := &Page{
		URL:      rawURL,
		Title:    result.Title,
		Markdown: result.Markdown,
	}
	if fetchURL != rawURL {
		page.FetchedURL = fetchURL
	}
	return page, source, nil
}

// ScrapeConditional fetches a URL with conditional headers and JS detection.
func (s *Scraper) ScrapeConditional(ctx context.Context, rawURL, etag, lastModified string) (*FetchResult, error) {
	return s.scrapeConditional(ctx, rawURL, etag, lastModified, nil)
}

func (s *Scraper) scrapeConditional(ctx context.Context, rawURL, etag, lastModified string, pdfUnsupported error) (*FetchResult, error) {
	fetchURL := s.Rewrite(rawURL)
	content, headers, notModified, err := s.fetchContent(ctx, fetchURL, etag, lastModified)
	if err != nil {
		return nil, err
	}
	if notModified {
		return &FetchResult{NotModified: true}, nil
	}

	contentType := effectiveContentType(content.ContentType, content.Body)
	if contentType == "application/pdf" {
		if pdfUnsupported != nil {
			return nil, pdfUnsupported
		}
		markdown, err := s.pdfExtractor.Extract(ctx, content.Body)
		if err != nil {
			return nil, fmt.Errorf("PDF extraction failed for %s: %w", fetchURL, err)
		}
		page := &Page{
			URL:          rawURL,
			Markdown:     markdown,
			ETag:         headers.Get("ETag"),
			LastModified: headers.Get("Last-Modified"),
			ContentHash:  ContentHash(markdown),
		}
		if fetchURL != rawURL {
			page.FetchedURL = fetchURL
		}
		return &FetchResult{Page: page, ContentType: contentType, Source: SourceHTTP}, nil
	}

	html := string(content.Body)
	var doc *goquery.Document
	var detection string
	source := SourceHTTP
	if isHTMLContentType(contentType) {
		// Parse once for JS-shell detection; downstream callers can reuse this
		// doc via FetchResult.Doc instead of re-parsing the same HTML.
		var parseErr error
		doc, parseErr = goquery.NewDocumentFromReader(strings.NewReader(html))
		if parseErr != nil {
			detection = "ambiguous"
		} else {
			detection = s.detector.DetectJSShellFromDoc(doc, html)
		}
		if detection == "likely_shell" {
			html, source = s.browserFetchOrWarn(ctx, fetchURL, html)
			doc = nil // rendered HTML needs a fresh parse downstream
			if source == SourceBrowser {
				contentType = "text/html"
			}
		}
	}

	result, err := s.extractor.Extract(fetchURL, html)
	if err != nil {
		return nil, fmt.Errorf("extraction failed for %s: %w", fetchURL, err)
	}

	page := &Page{
		URL:          rawURL,
		Title:        result.Title,
		Markdown:     result.Markdown,
		ETag:         headers.Get("ETag"),
		LastModified: headers.Get("Last-Modified"),
		ContentHash:  ContentHash(result.Markdown),
	}
	if fetchURL != rawURL {
		page.FetchedURL = fetchURL
	}

	return &FetchResult{
		Doc:         doc,
		Page:        page,
		RawHTML:     html,
		ContentType: contentType,
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
	detection := s.detector.DetectJSShell(html)
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
	} else if !s.HasBrowser() {
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

// CacheKey returns the page-cache key for an already-rewritten fetch URL.
// A configured jar with live cookies gets a jar-specific namespace even when
// no cookie matches the initial URL: a redirect may land on another host or
// path where a cookie does match. This prevents redirected authenticated
// content from colliding with an anonymous cache entry.
func (s *Scraper) CacheKey(fetchURL string) string {
	fingerprint := s.jar.Fingerprint()
	if fingerprint == "" {
		return fetchURL
	}
	return fetchURL + "\x00cookies:" + fingerprint
}

// FetchContent fetches a URL without extraction or browser fallback while
// preserving response bytes and the server-declared content type.
func (s *Scraper) FetchContent(ctx context.Context, rawURL string) (*FetchedContent, error) {
	content, _, notModified, err := s.fetchContent(ctx, rawURL, "", "")
	if err != nil {
		return nil, err
	}
	if notModified {
		return nil, fmt.Errorf("unexpected 304 Not Modified for unconditional fetch of %s", rawURL)
	}
	return content, nil
}

// Fetch is the compatibility wrapper for callers that expect a string body.
func (s *Scraper) Fetch(ctx context.Context, rawURL string) (string, error) {
	content, err := s.FetchContent(ctx, rawURL)
	if err != nil {
		return "", err
	}
	return string(content.Body), nil
}

// cookieRoundTripper clears any Cookie header inherited by net/http's
// redirect machinery and rebuilds it from the jar for the URL of every hop.
// The request is cloned so caller-owned headers are never mutated.
type cookieRoundTripper struct {
	base http.RoundTripper
	jar  *cookies.Jar
}

func (t *cookieRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	setCookieHeader(cloned, t.jar)
	return t.base.RoundTrip(cloned)
}

func clientWithCookies(client *http.Client, jar *cookies.Jar) *http.Client {
	cloned := *client
	base := cloned.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	cloned.Transport = &cookieRoundTripper{base: base, jar: jar}
	return &cloned
}

func setCookieHeader(req *http.Request, jar *cookies.Jar) {
	// Header.Del canonicalizes its argument but would miss a non-canonical map
	// key installed by another RoundTripper, so remove case-insensitively.
	for key := range req.Header {
		if strings.EqualFold(key, "Cookie") {
			delete(req.Header, key)
		}
	}
	matched := jar.For(req.URL)
	if len(matched) == 0 {
		return
	}

	// Build the header verbatim rather than via req.AddCookie, which sanitizes
	// values and strips bytes such as quotes in RFC 6265 quoted strings.
	var b strings.Builder
	for i, ck := range matched {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(ck.Name)
		b.WriteByte('=')
		b.WriteString(ck.Value)
	}
	req.Header.Set("Cookie", b.String())
}

func (s *Scraper) fetchContent(ctx context.Context, rawURL, etag, lastModified string) (*FetchedContent, http.Header, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, nil, false, err
	}
	ua := s.userAgent
	if ua == "" {
		ua = DefaultUserAgent()
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/pdf")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, false, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, resp.Header, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, false, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	body, err := readBoundedBody(resp.Body)
	if err != nil {
		return nil, nil, false, err
	}
	return &FetchedContent{Body: body, ContentType: resp.Header.Get("Content-Type")}, resp.Header, false, nil
}

// readBoundedBody preserves the historical scrape behavior: responses larger
// than MaxBodyBytes are silently truncated instead of failing the scrape.
func readBoundedBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, MaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}
	return body, nil
}

func effectiveContentType(declared string, body []byte) string {
	mediaType, _, err := mime.ParseMediaType(declared)
	if err != nil {
		mediaType = ""
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "application/pdf" || bytes.HasPrefix(body, []byte("%PDF-")) {
		return "application/pdf"
	}
	if mediaType == "" || mediaType == "application/octet-stream" || mediaType == "binary/octet-stream" {
		detected, _, detectErr := mime.ParseMediaType(http.DetectContentType(body))
		if detectErr == nil {
			return strings.ToLower(detected)
		}
	}
	return mediaType
}

func isHTMLContentType(contentType string) bool {
	return contentType == "text/html" || contentType == "application/xhtml+xml"
}
