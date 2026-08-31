package scrape

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/cookies"
	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/urlrewrite"
)

// This file owns the cache-aware scrape pipeline shared by the CLI (cmd/) and
// the MCP server (mcp/). It used to live as private helpers in cmd/scrape.go
// with a drifting copy in mcp/scrape.go; now both call these.

// PageCache is the subset of the cache API the scrape pipeline needs.
// *cache.Cache implements it (the cache package imports scrape, so the
// dependency points this way via an interface). Callers may pass nil to
// bypass caching entirely.
type PageCache interface {
	Get(url string) (*Page, string)
	Put(url string, page *Page, source string)
	GetRaw(url string) (rawHTML, source string, page *Page)
	PutRaw(url string, page *Page, source, rawHTML string)
}

// Sentinel errors for selector scrapes so callers can classify failures
// (CLI exit codes, MCP error kinds) without string matching.
var (
	// ErrBadSelector wraps selector-extraction failures (typically an invalid
	// CSS selector) — a caller-input problem, not an upstream fault.
	ErrBadSelector = errors.New("selector extraction failed")
	// ErrSelectorNoMatch reports that the selector matched no elements.
	ErrSelectorNoMatch = errors.New("no elements matched selector")
	// ErrPDFSelectorUnsupported reports that CSS selectors only apply to HTML.
	ErrPDFSelectorUnsupported = errors.New("CSS selector extraction is not supported for PDF documents")
	// ErrPDFRawUnsupported reports that returning PDF binary data is forbidden.
	ErrPDFRawUnsupported = errors.New("raw output is not supported for PDF documents")
)

// NewFromConfig builds a Scraper from cfg: compiled URL rewriter, optional
// browser binary, and operator-configured SPA markers. The rewriter's regexes
// are compiled once here — callers should construct one Scraper and reuse it.
// The returned Scraper is safe for concurrent use and must be Closed by the
// caller.
func NewFromConfig(cfg *config.Config) (*Scraper, error) {
	rw, err := urlrewrite.NewRewriter(cfg.URLRewrites)
	if err != nil {
		return nil, fmt.Errorf("invalid url_rewrites: %w", err)
	}
	scraper := NewWithConfig(cfg.Browser, rw, cfg.SPAMarkers)
	ua, err := normalizeUserAgent(cfg.UserAgent)
	if err != nil {
		return nil, fmt.Errorf("invalid user_agent: %w", err)
	}
	scraper.userAgent = ua
	scraper.userAgentConfigured = strings.TrimSpace(cfg.UserAgent) != ""
	if cfg.CookieFile != "" {
		jar, err := cookies.Load(cfg.CookieFile)
		if err != nil {
			return nil, fmt.Errorf("invalid cookie_file: %w", err)
		}
		scraper.jar = jar
		scraper.client = clientWithCookies(scraper.client, jar)
		warnLooseCookiePerms(cfg.CookieFile)
	}
	if cfg.ExternalPDFToMDConverterCommand != "" {
		pdfExtractor, err := extract.NewExternalPDFExtractor(
			cfg.ExternalPDFToMDConverterCommand,
			time.Duration(cfg.ExternalPDFToMDConverterTimeoutSec)*time.Second,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid external PDF converter: %w", err)
		}
		scraper.pdfExtractor = pdfExtractor
	}
	return scraper, nil
}

// warnLooseCookiePerms warns once (NewFromConfig runs once per process) when
// the jar is group/world-readable. Skipped on Windows, where POSIX permission
// bits are not meaningful.
func warnLooseCookiePerms(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	if info, err := os.Stat(cookies.ExpandPath(path)); err == nil && info.Mode().Perm()&0o044 != 0 {
		fmt.Fprintf(os.Stderr, "warn: cookie file %s is group/world-readable; chmod 600 recommended\n", path)
	}
}

// CachedScrape checks the cache first, falls back to fetch+extract.
// Hits are bypassed when the entry was extracted from an unrendered JS shell
// and a browser is now available to do better, or when the entry predates
// source tracking (a one-time migration once a browser is configured).
// The cache is keyed by the rewritten URL so original and rewritten URLs
// share one cache entry.
func (s *Scraper) CachedScrape(ctx context.Context, pc PageCache, url string) (*Page, error) {
	key := s.CacheKey(s.Rewrite(url))
	if pc != nil {
		if page, source := pc.Get(key); page != nil && !CacheStaleForBrowser(source, s.HasBrowser()) {
			return page, nil
		}
	}

	page, source, err := s.Scrape(ctx, url)
	if err != nil {
		return nil, err
	}

	if pc != nil {
		pc.Put(key, page, source)
	}
	return page, nil
}

// CachedScrapeRaw is the raw-HTML path. It routes through ScrapeConditional so
// one fetch yields Page + RawHTML + Source (the markdown path's Scrape
// discards the body). Raw lookup is a hit only when RawHTML is non-empty — a
// markdown-only entry does not poison a raw request. On a raw miss against an
// existing markdown entry, the refetch back-fills RawHTML while preserving the
// cached Page (one fetch, both representations cached). A nil pc skips cache
// read/write and returns the fresh fetch result directly.
func (s *Scraper) CachedScrapeRaw(ctx context.Context, pc PageCache, url string) (*Page, string, string, error) {
	key := s.CacheKey(s.Rewrite(url))
	if pc != nil {
		if rawHTML, source, page := pc.GetRaw(key); page != nil {
			return page, rawHTML, source, nil
		}
	}

	result, err := s.scrapeConditional(ctx, url, "", "", ErrPDFRawUnsupported)
	if err != nil {
		return nil, "", "", err
	}
	if result.NotModified {
		return nil, "", "", fmt.Errorf("unexpected 304 Not Modified without cached ETag for %s", url)
	}

	if pc != nil {
		pc.PutRaw(key, result.Page, result.Source, result.RawHTML)
	}
	return result.Page, result.RawHTML, result.Source, nil
}

// CachedScrapeForce is the forced-browser markdown path. It classifies a
// plain HTTP response before any browser render so PDFs bypass Chromium's PDF
// viewer and use the configured PDF extractor. For HTML, a cache entry is
// reused only when that entry is itself a browser render.
func (s *Scraper) CachedScrapeForce(ctx context.Context, pc PageCache, url string) (*Page, error) {
	fetchURL := s.Rewrite(url)
	key := s.CacheKey(fetchURL)
	content, isPDF, err := s.classifyForForcedRender(ctx, fetchURL)
	if err != nil {
		return nil, err
	}
	if isPDF {
		markdown, err := s.pdfExtractor.Extract(ctx, content.Body)
		if err != nil {
			return nil, fmt.Errorf("PDF extraction failed for %s: %w", fetchURL, err)
		}
		page := &Page{URL: url, Markdown: markdown}
		if fetchURL != url {
			page.FetchedURL = fetchURL
		}
		if pc != nil {
			pc.Put(key, page, SourceHTTP)
		}
		return page, nil
	}
	if !s.HasBrowser() {
		return nil, ErrNoBrowser
	}
	if pc != nil {
		if page, source := pc.Get(key); page != nil && source == SourceBrowser {
			return page, nil
		}
	}
	page, _, err := s.BrowserScrape(ctx, url)
	if err != nil {
		return nil, err
	}
	if pc != nil {
		pc.Put(key, page, SourceBrowser)
	}
	return page, nil
}

// CachedScrapeRawForce is the forced-browser raw path. It classifies the HTTP
// response before consulting the browser cache or rendering, rejecting PDFs
// rather than returning Chromium's PDF-viewer HTML.
func (s *Scraper) CachedScrapeRawForce(ctx context.Context, pc PageCache, url string) (*Page, string, string, error) {
	fetchURL := s.Rewrite(url)
	key := s.CacheKey(fetchURL)
	_, isPDF, err := s.classifyForForcedRender(ctx, fetchURL)
	if err != nil {
		return nil, "", "", err
	}
	if isPDF {
		return nil, "", "", ErrPDFRawUnsupported
	}
	if !s.HasBrowser() {
		return nil, "", "", ErrNoBrowser
	}
	if pc != nil {
		if rawHTML, source, page := pc.GetRaw(key); page != nil && source == SourceBrowser {
			return page, rawHTML, source, nil
		}
	}
	page, html, err := s.BrowserScrape(ctx, url)
	if err != nil {
		return nil, "", "", err
	}
	if pc != nil {
		pc.PutRaw(key, page, SourceBrowser, html)
	}
	return page, html, SourceBrowser, nil
}

// ScrapeMarkdown picks the markdown fetch path. Forced-browser HTML renders;
// forced-browser PDFs still use text extraction after content classification.
func (s *Scraper) ScrapeMarkdown(ctx context.Context, pc PageCache, url string, forceBrowser bool) (*Page, error) {
	if forceBrowser {
		return s.CachedScrapeForce(ctx, pc, url)
	}
	return s.CachedScrape(ctx, pc, url)
}

// ScrapeRaw picks the raw-HTML fetch path, rejecting PDFs before any forced
// browser render.
func (s *Scraper) ScrapeRaw(ctx context.Context, pc PageCache, url string, forceBrowser bool) (*Page, string, string, error) {
	if forceBrowser {
		return s.CachedScrapeRawForce(ctx, pc, url)
	}
	return s.CachedScrapeRaw(ctx, pc, url)
}

// ScrapeSelector fetches rawURL and returns only elements matching the CSS
// selector, converted to markdown — bypassing readability extraction and the
// page cache. Under forceBrowser it first classifies the response, rejects
// PDFs, then renders HTML and selects against the rendered DOM; otherwise it
// fetches plain HTTP with JS-shell auto-detection. The URL is rewritten before
// fetch so selector scrapes share
// the canonical URL-rewrite path with Scrape/ScrapeConditional.
func (s *Scraper) ScrapeSelector(ctx context.Context, rawURL, selector string, forceBrowser bool) (*Page, error) {
	fetchURL := s.Rewrite(rawURL)
	html, err := s.fetchHTMLForSelector(ctx, rawURL, fetchURL, forceBrowser)
	if err != nil {
		return nil, err
	}
	markdown, err := extract.ExtractSelector(html, selector)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadSelector, err)
	}
	if markdown == "" {
		return nil, fmt.Errorf("%w %q", ErrSelectorNoMatch, selector)
	}
	page := &Page{URL: rawURL, Title: extract.Title(html), Markdown: markdown}
	if fetchURL != rawURL {
		page.FetchedURL = fetchURL
	}
	return page, nil
}

// fetchHTMLForSelector returns the HTML to run a CSS selector against. It
// always classifies the plain HTTP response first so a forced browser render
// cannot turn a PDF into Chromium viewer HTML. rawURL is passed to
// BrowserScrape, which rewrites internally; fetchURL is already rewritten.
func (s *Scraper) fetchHTMLForSelector(ctx context.Context, rawURL, fetchURL string, forceBrowser bool) (string, error) {
	if forceBrowser {
		if _, isPDF, err := s.classifyForForcedRender(ctx, fetchURL); err != nil {
			return "", fmt.Errorf("fetch failed: %w", err)
		} else if isPDF {
			return "", ErrPDFSelectorUnsupported
		}
		_, html, err := s.BrowserScrape(ctx, rawURL)
		if err != nil {
			return "", fmt.Errorf("browser fetch failed: %w", err)
		}
		return html, nil
	}
	content, err := s.FetchContent(ctx, fetchURL)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	if effectiveContentType(content.ContentType, content.Body) == "application/pdf" {
		return "", ErrPDFSelectorUnsupported
	}
	html, _ := s.MaybeBrowserFetch(ctx, fetchURL, string(content.Body))
	return html, nil
}

// classifyForForcedRender probes the URL over plain HTTP purely to detect PDFs
// before a forced browser render, so Chromium's PDF viewer chrome never
// reaches the extractor. A failed probe is deliberately not fatal:
// --force-browser exists to get past walls that reject ketch's HTTP client
// (bot filters commonly answer 403), so a blocked probe falls through to the
// browser instead of aborting the scrape. Without a browser to fall through
// to, the probe error is the real error and is returned.
func (s *Scraper) classifyForForcedRender(ctx context.Context, fetchURL string) (*FetchedContent, bool, error) {
	content, err := s.FetchContent(ctx, fetchURL)
	if err != nil {
		if !s.HasBrowser() {
			return nil, false, err
		}
		return nil, false, nil
	}
	return content, effectiveContentType(content.ContentType, content.Body) == "application/pdf", nil
}

// FetchLLMSTxt attempts to fetch /llms.txt anonymously. It is kept for
// package compatibility; callers with a Scraper should use its method so the
// configured cookie jar and redirect re-scoping apply.
func FetchLLMSTxt(ctx context.Context, baseURL string) (string, bool) {
	return New().FetchLLMSTxt(ctx, baseURL)
}

// FetchLLMSTxt attempts to fetch /llms.txt from a bare-domain URL through the
// scraper's authenticated HTTP path. All errors are swallowed because this is
// only a best-effort shortcut before the requested page is scraped.
func (s *Scraper) FetchLLMSTxt(ctx context.Context, baseURL string) (string, bool) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Path != "" && u.Path != "/") {
		return "", false
	}

	// Cap probes at 5s so an unresponsive endpoint cannot delay the real scrape.
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	content, err := s.FetchContent(probeCtx, u.Scheme+"://"+u.Host+"/llms.txt")
	if err != nil || !strings.Contains(content.ContentType, "text/plain") {
		return "", false
	}
	return string(content.Body), true
}
