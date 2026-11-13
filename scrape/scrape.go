package scrape

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// Source is how a page was fetched, the state that decides whether a cached
// copy may be served. It is stored with every cache entry.
//
//	SourceHTTP       plain HTTP, not a JS shell: authoritative
//	SourceHTTPShell  a JS shell that could not be rendered (no browser
//	                 configured, or the render failed): never served from
//	                 cache, so a later call renders it
//	SourceBrowser    rendered by the headless browser
//	SourceUnknown    an entry written before sources were recorded
type Source string

const (
	SourceUnknown   Source = ""
	SourceHTTP      Source = "http"
	SourceHTTPShell Source = "http_shell"
	SourceBrowser   Source = "browser"
)

// Rendered reports a page the browser rendered.
func (s Source) Rendered() bool { return s == SourceBrowser }

// Shell reports a page known to be an unrendered JS shell.
func (s Source) Shell() bool { return s == SourceHTTPShell }

// ServeFromCache reports whether a cached page fetched this way may be
// served, given whether a browser is configured. This is the only cache
// validity rule for page sources; every cached read path applies it.
//
// It depends on configuration, never on whether a browser launched in this
// process: a launch that failed here would otherwise make the same entry
// fresh in this process and stale in the next. An unrendered shell is never
// served. An entry from before sources were recorded is served only when no
// browser is configured, since it may be an unrendered shell a browser could
// now do better.
func (s Source) ServeFromCache(browserConfigured bool) bool {
	switch s {
	case SourceHTTP, SourceBrowser:
		return true
	case SourceHTTPShell:
		return false
	default:
		return !browserConfigured
	}
}

// Page represents a scraped web page.
type Page struct {
	URL          string `json:"url"`
	FetchedURL   string `json:"fetched_url,omitempty"`
	Title        string `json:"title"`
	Markdown     string `json:"markdown"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	ContentHash  string `json:"content_hash,omitempty"`
	// FinalURL is where the content was finally served from: the response
	// URL after HTTP redirects or, for a browser render, the address the
	// browser ended on. It is in fetch terms (after URL rewrites), and empty
	// when the path that built the page does not track it or the page came
	// from a cache entry written before it was recorded. It is deliberately
	// left out of the page's JSON, so scrape output is unchanged; the page
	// cache stores it beside the page.
	FinalURL string `json:"-"`
	// Anchors are the fragment targets the page's HTML carries (element
	// ids, <a name>), headings' with their text: what a link to the page
	// plus a fragment can really reach. Empty but non-nil for a page with
	// none (a PDF); nil when unknown, as for a cache entry written before
	// anchors were recorded. Left out of the page's JSON like FinalURL; the
	// page cache stores it beside the page.
	Anchors []Anchor `json:"-"`
	// Source is how the page was fetched, from the fetch or the cache entry
	// it came from. Left out of the page's JSON; the raw mode reports it
	// separately.
	Source Source `json:"-"`
	// FinalURLUnknown marks a page whose FinalURL is empty because the
	// browser that rendered it could not say where it landed, as opposed to
	// a cache entry written before final URLs were recorded.
	FinalURLUnknown bool `json:"-"`
}

// Anchor is a fragment target on a page (extract.Anchor).
type Anchor = extract.Anchor

// FetchedContent preserves the response body bytes and declared content type.
type FetchedContent struct {
	Body        []byte
	ContentType string
	// URL is the URL the body was served from, after any redirects.
	URL string
}

// HTML returns the body as UTF-8 text, decoded from the charset the
// response declared or the document carries. A page served in a legacy
// encoding — windows-1252 with no declaration, Shift_JIS by <meta> —
// would otherwise reach the extractor as bytes that are not UTF-8 and come
// out as replacement characters.
func (c *FetchedContent) HTML() string {
	return extract.DecodeHTMLBytes(c.Body, c.ContentType)
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

	// Source is the fetch path that produced Page.
	Source Source
}

// Scraper fetches web pages and extracts content as markdown.
type Scraper struct {
	client       *http.Client
	extractor    *extract.Extractor
	pdfExtractor extract.PDFExtractor
	detector     *extract.Detector
	browserBin   string
	// browserConfigured is browserBin != "" at construction; never cleared.
	browserConfigured bool
	browserMu         sync.Mutex
	browser           BrowserConn
	rewriter          *urlrewrite.Rewriter
	jar               *cookies.Jar
	userAgent         string
	// userAgentConfigured records that the operator explicitly set user_agent
	// (config, env, or flag) instead of riding the built-in default. Only an
	// explicitly configured UA reaches the headless browser; when unset, the
	// browser keeps its own User-Agent minus the HeadlessChrome token (see
	// NewBrowserConnOptions).
	userAgentConfigured bool
	// pageWarn receives the warnings about single pages' fetches instead
	// of stderr when set (OnPageWarning).
	pageWarn func(PageWarning)
}

// Kinds of PageWarning.
const (
	WarnJSRendered       = "js_rendered"        // a JS shell, no browser configured
	WarnBrowserFailed    = "browser_failed"     // the browser fallback for a JS shell failed
	WarnBrowser403       = "browser_403"        // HTTP 403, fetched with the browser (a notice)
	WarnBrowser403Failed = "browser_403_failed" // HTTP 403, and the browser retry failed
)

// PageWarning is a warning about one page's fetch: its kind, the page
// and the error behind it (nil for none).
type PageWarning struct {
	Kind string
	URL  string
	Err  error
}

// String is the warning as a fetch prints it to stderr.
func (w PageWarning) String() string {
	switch w.Kind {
	case WarnJSRendered:
		return fmt.Sprintf("warn: %s appears JS-rendered; configure browser for full content", w.URL)
	case WarnBrowserFailed:
		return fmt.Sprintf("warn: browser fallback failed for %s: %v", w.URL, w.Err)
	case WarnBrowser403:
		return fmt.Sprintf("notice: HTTP 403 for %s; fetched with the browser", w.URL)
	case WarnBrowser403Failed:
		return fmt.Sprintf("warn: browser retry after HTTP 403 failed for %s: %v", w.URL, w.Err)
	}
	return fmt.Sprintf("warn: %s: %s", w.URL, w.Kind)
}

// OnPageWarning sends the warnings about single pages' fetches (a
// JS-rendered page, a browser fallback or retry) to f instead of stderr,
// so a caller fetching many pages can fold them into one line. f may be
// called from concurrent fetches. Set it before the first fetch.
func (s *Scraper) OnPageWarning(f func(PageWarning)) { s.pageWarn = f }

// warnPage reports a warning about one page's fetch: to the
// OnPageWarning handler, else on stderr.
func (s *Scraper) warnPage(w PageWarning) {
	if s.pageWarn != nil {
		s.pageWarn(w)
		return
	}
	fmt.Fprintln(os.Stderr, w.String())
}

// NewWithRewriter creates a Scraper with an optional browser binary and
// optional URL rewriter. Pass "" for browserBin to disable browser fallback;
// pass nil rewriter to disable URL rewriting. The detector uses only the
// built-in SPA markers; use NewWithConfig to add operator-configured markers.
func NewWithRewriter(browserBin string, rw *urlrewrite.Rewriter) *Scraper {
	return &Scraper{
		client:            httpx.Default(),
		extractor:         extract.New(),
		pdfExtractor:      extract.NewPDFExtractor(),
		detector:          extract.NewDetector(nil),
		browserBin:        browserBin,
		browserConfigured: browserBin != "",
		rewriter:          rw,
		userAgent:         DefaultUserAgent(),
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
// BrowserConfigured reports whether a browser was configured when the scraper
// was built. Unlike HasBrowser it never changes when a launch fails, so
// decisions that must agree across processes (cache validity) use it.
func (s *Scraper) BrowserConfigured() bool { return s.browserConfigured }

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
// content extraction; an HTTP 403 retries with the browser the same way.
func (s *Scraper) Scrape(ctx context.Context, rawURL string) (*Page, Source, error) {
	fetchURL := s.Rewrite(rawURL)

	content, err := s.FetchContent(ctx, fetchURL)
	if err != nil {
		rendered, ok := s.browserOnForbidden(ctx, fetchURL, err)
		if !ok {
			return nil, "", err
		}
		return s.extractPage(rawURL, fetchURL, rendered.html, SourceBrowser, rendered.finalURL)
	}

	contentType := effectiveContentType(content.ContentType, content.Body)
	if contentType == "application/pdf" {
		markdown, err := s.pdfExtractor.Extract(ctx, content.Body)
		if err != nil {
			return nil, "", fmt.Errorf("PDF extraction failed for %s: %w", fetchURL, err)
		}
		page := &Page{URL: rawURL, Markdown: markdown, FinalURL: content.URL, Anchors: []Anchor{}, Source: SourceHTTP}
		if fetchURL != rawURL {
			page.FetchedURL = fetchURL
		}
		return page, SourceHTTP, nil
	}

	html, source, rendered := s.maybeBrowserFetch(ctx, fetchURL, content.HTML())
	return s.extractPage(rawURL, fetchURL, html, source, finalURL(source, rendered, content.URL))
}

// extractPage runs the extractor over html fetched from fetchURL and builds
// the Page Scrape returns, tagged with source. served is where the content
// came from (Page.FinalURL), "" when unknown.
func (s *Scraper) extractPage(rawURL, fetchURL, html string, source Source, served string) (*Page, Source, error) {
	result, err := s.extractor.Extract(fetchURL, html)
	if err != nil {
		return nil, "", fmt.Errorf("extraction failed for %s: %w", fetchURL, err)
	}

	page := &Page{
		URL:      rawURL,
		Title:    result.Title,
		Markdown: result.Markdown,
		FinalURL: served,
		Anchors:  extract.Anchors(html),
		Source:   source,
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
		rendered, ok := s.browserOnForbidden(ctx, fetchURL, err)
		if !ok {
			return nil, err
		}
		return s.renderedResult(rawURL, fetchURL, rendered)
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
			FinalURL:     content.URL,
			Anchors:      []Anchor{},
			Source:       SourceHTTP,
		}
		if fetchURL != rawURL {
			page.FetchedURL = fetchURL
		}
		return &FetchResult{Page: page, ContentType: contentType, Source: SourceHTTP}, nil
	}

	html := content.HTML()
	var doc *goquery.Document
	var detection string
	source, rendered := SourceHTTP, ""
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
			html, source, rendered = s.browserFetchOrWarn(ctx, fetchURL, html)
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
		FinalURL:     finalURL(source, rendered, content.URL),
		Anchors:      pageAnchors(doc, html),
		Source:       source,
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

// renderedResult builds the FetchResult for a page the browser rendered after
// the plain HTTP fetch was refused. The rendered DOM carries no validators, so
// the page caches without an ETag or Last-Modified. Its FinalURL is where the
// browser landed, "" when the browser cannot say: there is no HTTP response
// URL to fall back on.
func (s *Scraper) renderedResult(rawURL, fetchURL string, rendered rendering) (*FetchResult, error) {
	html := rendered.html
	result, err := s.extractor.Extract(fetchURL, html)
	if err != nil {
		return nil, fmt.Errorf("extraction failed for %s: %w", fetchURL, err)
	}
	page := &Page{
		URL:             rawURL,
		Title:           result.Title,
		Markdown:        result.Markdown,
		ContentHash:     ContentHash(result.Markdown),
		FinalURL:        rendered.finalURL,
		Anchors:         extract.Anchors(html),
		Source:          SourceBrowser,
		FinalURLUnknown: rendered.finalURL == "",
	}
	if fetchURL != rawURL {
		page.FetchedURL = fetchURL
	}
	return &FetchResult{
		Page:        page,
		RawHTML:     html,
		ContentType: "text/html",
		Source:      SourceBrowser,
	}, nil
}

// BrowserScrape fetches a URL using the browser directly.
// Used when a host is known to require browser rendering.
func (s *Scraper) BrowserScrape(ctx context.Context, rawURL string) (*Page, string, error) {
	fetchURL := s.Rewrite(rawURL)
	if err := ValidateWebURL(fetchURL); err != nil {
		return nil, "", err
	}
	browser := s.getBrowser()
	if browser == nil {
		return nil, "", ErrNoBrowser
	}
	rendered, err := browserRender(ctx, browser, fetchURL)
	if err != nil {
		return nil, "", fmt.Errorf("browser fetch failed for %s: %w", fetchURL, err)
	}
	html := rendered.html
	result, err := s.extractor.Extract(fetchURL, html)
	if err != nil {
		return nil, "", fmt.Errorf("extraction failed for %s: %w", fetchURL, err)
	}
	page := &Page{
		URL:             rawURL,
		Title:           result.Title,
		Markdown:        result.Markdown,
		ContentHash:     ContentHash(result.Markdown),
		FinalURL:        rendered.finalURL,
		Anchors:         extract.Anchors(html),
		Source:          SourceBrowser,
		FinalURLUnknown: rendered.finalURL == "",
	}
	if fetchURL != rawURL {
		page.FetchedURL = fetchURL
	}
	return page, html, nil
}

// MaybeBrowserFetch re-fetches rawURL via the browser if html looks JS-rendered.
// Returns the (possibly rendered) html and the fetch source actually used.
func (s *Scraper) MaybeBrowserFetch(ctx context.Context, rawURL, html string) (string, Source) {
	html, source, _ := s.maybeBrowserFetch(ctx, rawURL, html)
	return html, source
}

// maybeBrowserFetch is MaybeBrowserFetch that also returns the URL a browser
// render ended on ("" when nothing was rendered).
func (s *Scraper) maybeBrowserFetch(ctx context.Context, rawURL, html string) (string, Source, string) {
	detection := s.detector.DetectJSShell(html)
	if detection != "likely_shell" {
		return html, SourceHTTP, ""
	}
	return s.browserFetchOrWarn(ctx, rawURL, html)
}

// browserFetchOrWarn returns the rendered html with SourceBrowser on success,
// or the original (shell) html with SourceHTTPShell if the browser is
// unavailable or fails. Called only when detection said "likely_shell", so
// returning SourceHTTP here would be a lie — the entry would round-trip as
// "plain page" and never get retried once a browser is configured. The third
// value is the URL the render ended on, "" when nothing was rendered.
func (s *Scraper) browserFetchOrWarn(ctx context.Context, rawURL, html string) (string, Source, string) {
	browser := s.getBrowser()
	if browser != nil {
		rendered, err := browserRender(ctx, browser, rawURL)
		if err == nil {
			return rendered.html, SourceBrowser, rendered.finalURL
		}
		s.warnPage(PageWarning{Kind: WarnBrowserFailed, URL: rawURL, Err: err})
	} else if !s.HasBrowser() {
		s.warnPage(PageWarning{Kind: WarnJSRendered, URL: rawURL})
	}
	return html, SourceHTTPShell, ""
}

// finalURL is where a page's content came from: the browser's landing URL
// when the page was rendered, else the HTTP response URL.
func finalURL(source Source, rendered, fetched string) string {
	if source == SourceBrowser && rendered != "" {
		return rendered
	}
	return fetched
}

// browserOnForbidden retries fetchURL through the configured browser when the
// plain HTTP fetch failed with HTTP 403. Bot walls commonly refuse Go's client
// but let a real browser through. Only a 403 escalates: a 429 asks the client
// to back off, and every other failure would fail the browser too. On success
// it returns the rendered HTML and prints a notice to stderr, so stdout stays
// clean. Otherwise it reports false and the caller returns the original error;
// a browser failure is only a stderr warning, never the reported error. The
// rendering's finalURL is where the browser landed, "" when it cannot say.
func (s *Scraper) browserOnForbidden(ctx context.Context, fetchURL string, fetchErr error) (rendering, bool) {
	var statusErr *HTTPStatusError
	if !errors.As(fetchErr, &statusErr) || statusErr.StatusCode != http.StatusForbidden {
		return rendering{}, false
	}
	browser := s.getBrowser()
	if browser == nil {
		return rendering{}, false
	}
	rendered, err := fetchVerified(ctx, browser, fetchURL)
	if err != nil {
		s.warnPage(PageWarning{Kind: WarnBrowser403Failed, URL: fetchURL, Err: err})
		return rendering{}, false
	}
	s.warnPage(PageWarning{Kind: WarnBrowser403, URL: fetchURL})
	return rendered, true
}

// fetchVerified renders fetchURL through browser and, when the browser can
// report the main document's status, fails on an HTTP error status: a wall
// that refuses the browser too renders an access-denied page, which must not
// pass for content. A browser that cannot report a status (0) is trusted.
func fetchVerified(ctx context.Context, browser BrowserConn, fetchURL string) (rendering, error) {
	rendered, err := browserRender(ctx, browser, fetchURL)
	if err != nil {
		return rendering{}, err
	}
	if rendered.status >= http.StatusBadRequest {
		return rendering{}, fmt.Errorf("browser also got HTTP %d", rendered.status)
	}
	return rendered, nil
}

// ServeFromCache reports whether a cached page with this source may be
// served by this scraper: Source.ServeFromCache under the scraper's browser
// configuration (BrowserConfigured, never whether a launch succeeded).
func (s *Scraper) ServeFromCache(source Source) bool {
	return source.ServeFromCache(s.BrowserConfigured())
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
//
// An explicitly configured user_agent likewise gets its own namespace: sites
// serve different content (or a bot wall) per User-Agent, so a page fetched
// under one UA must not satisfy a request made under another. Only a
// configured UA is folded in — the built-in default keeps the bare key, so
// existing cache entries stay valid for operators who never set one — and it
// is folded in as a short digest so the key never carries the UA text.
//
// A non-default extract_mode is folded in by name: the modes produce
// different markdown for the same fetch, and the default keeps the bare key
// so existing entries stay valid.
func (s *Scraper) CacheKey(fetchURL string) string {
	key := fetchURL
	if fingerprint := s.jar.Fingerprint(); fingerprint != "" {
		key += "\x00cookies:" + fingerprint
	}
	if s.userAgentConfigured {
		key += "\x00ua:" + ContentHash(s.userAgent)
	}
	if mode := s.extractor.Mode(); mode != extract.ModeClean {
		key += "\x00extract:" + string(mode)
	}
	return key
}

// HTTPStatusError reports a plain HTTP fetch that answered neither 200 nor
// 304. Callers match it with errors.As to act on the status code (the 403
// browser retry, the docs fetcher telling a missing page from a failing
// server) instead of matching the text, which is unchanged from when this was
// a plain error.
type HTTPStatusError struct {
	StatusCode int    // the response status code
	URL        string // the URL requested, after URL rewrites
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP %d for %s", e.StatusCode, e.URL)
}

// HTTPStatus is the response status code, for packages that classify a
// failed fetch without importing scrape (the docs backend's read errors).
func (e *HTTPStatusError) HTTPStatus() int { return e.StatusCode }

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

// FetchContentWithBrowser preserves the plain response and retries only HTTP
// 403 through a content-capable configured browser. Failed/unknown browser
// statuses retain the original 403; no rendered XML viewer HTML is accepted.
// Like FetchContent, rawURL must already have configured rewrites applied.
func (s *Scraper) FetchContentWithBrowser(ctx context.Context, rawURL string) (*FetchedContent, error) {
	content, err := s.FetchContent(ctx, rawURL)
	if err == nil {
		return content, nil
	}
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusForbidden {
		return nil, err
	}
	browser, ok := s.getBrowser().(ContentFetcher)
	if !ok {
		return nil, err
	}
	content, status, browserErr := browser.FetchContent(ctx, rawURL)
	if browserErr == nil && (status < http.StatusOK || status >= http.StatusMultipleChoices || content == nil) {
		browserErr = fmt.Errorf("browser returned unusable HTTP status %d", status)
	}
	if browserErr != nil {
		s.warnPage(PageWarning{Kind: WarnBrowser403Failed, URL: rawURL, Err: browserErr})
		return nil, err
	}
	s.warnPage(PageWarning{Kind: WarnBrowser403, URL: rawURL})
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
	if err := ValidateWebURL(rawURL); err != nil {
		return nil, nil, false, err
	}
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
		return nil, nil, false, &HTTPStatusError{StatusCode: resp.StatusCode, URL: rawURL}
	}

	body, err := readBoundedBody(resp.Body)
	if err != nil {
		return nil, nil, false, err
	}
	return &FetchedContent{Body: body, ContentType: resp.Header.Get("Content-Type"), URL: resp.Request.URL.String()}, resp.Header, false, nil
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

// pageAnchors reads html's anchors, from doc when it is already parsed.
func pageAnchors(doc *goquery.Document, html string) []Anchor {
	if doc != nil {
		return extract.AnchorsOf(doc)
	}
	return extract.Anchors(html)
}
