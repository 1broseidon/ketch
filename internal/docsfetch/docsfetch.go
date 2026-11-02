// Package docsfetch implements docs.Fetcher over the shared scrape pipeline
// and page cache. The docs package cannot build one itself: config imports
// docs, and scrape, cache and extract all import config. The CLI and the MCP
// server construct a Fetcher over the scraper and cache they already own and
// pass it to docs.NewFromConfig in docs.Deps, so docs pages get the same URL
// rewrites, cookies, User-Agent, browser fallback and caching as scrape.
package docsfetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/scrape"
)

// Fetcher is a docs.Fetcher over a scraper and an optional page cache. It
// holds no state of its own, so it is as safe for concurrent use as the
// scraper is.
type Fetcher struct {
	scraper *scrape.Scraper
	pages   scrape.PageCache // nil interface when caching is off
}

var (
	_ docs.Fetcher         = (*Fetcher)(nil)
	_ docs.ResolutionCache = (*Fetcher)(nil)
)

// resolutionKeyPrefix keeps resolution entries apart from pages in the page
// cache: no page URL starts with it, so no scrape ever reads one.
const resolutionKeyPrefix = "ketch-docs-resolution:"

// resolutionSource is the fetch source recorded on a resolution entry.
const resolutionSource = "docs-resolution"

// GetResolution reads a web docs resolution from the page cache, under its
// normal TTL; ("", false) without a cache.
func (f *Fetcher) GetResolution(key string) (string, bool) {
	if f.pages == nil {
		return "", false
	}
	page, source := f.pages.Get(resolutionKeyPrefix + key)
	if page == nil || source != resolutionSource {
		return "", false
	}
	return page.Markdown, true
}

// PutResolution writes a web docs resolution to the page cache; nothing
// without a cache.
func (f *Fetcher) PutResolution(key, value string) {
	if f.pages == nil {
		return
	}
	f.pages.Put(resolutionKeyPrefix+key, &scrape.Page{URL: resolutionKeyPrefix + key, Markdown: value}, resolutionSource)
}

// New returns a Fetcher over s and pc. A nil pc fetches uncached. The Fetcher
// borrows both: the caller still owns s and pc and closes them.
func New(s *scrape.Scraper, pc *cache.Cache) *Fetcher {
	f := &Fetcher{scraper: s}
	// A nil *cache.Cache stored in the interface would compare non-nil, and
	// the pipeline would then call through it instead of skipping the cache.
	if pc != nil {
		f.pages = pc
	}
	return f
}

// FetchRaw fetches url after the scraper's URL rewrites and returns the body
// unextracted, with its declared content type and the URL it was served from
// (see callerURL). It is never cached and never renders in a browser: it is
// for llms.txt, sitemaps and Markdown sources, where the bytes are the
// answer. Like the pipeline's llms.txt probe, it rejects a rewritten URL that
// is not absolute HTTP(S). A 404 or 410 wraps docs.ErrNotFound (see
// notFound).
func (f *Fetcher) FetchRaw(ctx context.Context, url string) (docs.RawContent, error) {
	fetchURL := f.scraper.Rewrite(url)
	if err := scrape.ValidateWebURL(fetchURL); err != nil {
		return docs.RawContent{}, err
	}
	content, err := f.scraper.FetchContent(ctx, fetchURL)
	if err != nil {
		return docs.RawContent{}, notFound(err)
	}
	return docs.RawContent{
		Body:        content.Body,
		ContentType: content.ContentType,
		URL:         f.callerURL(url, fetchURL, content.URL),
	}, nil
}

// FetchPage scrapes url to Markdown through the cache-aware pipeline:
// rewrites, a page-cache hit when one is warm, JS-shell browser fallback, and
// a cache write after a fresh fetch. The page's URL is where it was finally
// served from, the browser's landing URL for a render. A cache entry written
// before that was recorded cannot say, so it is fetched again and replaced.
// A 404 or 410 wraps docs.ErrNotFound.
func (f *Fetcher) FetchPage(ctx context.Context, url string) (docs.PageContent, error) {
	page, err := f.scraper.ScrapeMarkdown(ctx, f.pages, url, false)
	if err == nil && page.FinalURL == "" && f.pages != nil {
		page, err = f.refresh(ctx, url)
	}
	if err != nil {
		return docs.PageContent{}, notFound(err)
	}
	return docs.PageContent{
		Title:    page.Title,
		Markdown: page.Markdown,
		URL:      f.callerURL(url, f.scraper.Rewrite(url), page.FinalURL),
	}, nil
}

// refresh fetches url past the page cache and writes the result back under
// the pipeline's key, as a cache miss would have.
func (f *Fetcher) refresh(ctx context.Context, url string) (*scrape.Page, error) {
	page, source, err := f.scraper.Scrape(ctx, url)
	if err != nil {
		return nil, err
	}
	f.pages.Put(f.scraper.CacheKey(f.scraper.Rewrite(url)), page, source)
	return page, nil
}

// callerURL reports where a response came from in the caller's terms.
// requested is the URL the backend asked for, fetchURL what the scraper
// fetched for it after URL rewrites, and served where the response finally
// came from, in fetch terms. Without a redirect that is requested itself;
// without a rewrite, served needs no translation. A redirect behind a
// rewrite is mapped back when the rewrite turns the mapped URL into exactly
// served (unrewrite); otherwise served is reported as fetched, which the
// backend's scope check will normally refuse. An unknown served ("") stays
// unknown.
func (f *Fetcher) callerURL(requested, fetchURL, served string) string {
	switch {
	case served == "":
		return ""
	case sameURL(served, fetchURL):
		return requested
	case fetchURL == requested:
		return served
	}
	if back, ok := f.unrewrite(requested, fetchURL, served); ok {
		return back
	}
	return served
}

// unrewrite maps served, a redirect target in fetch terms, back through the
// rewrite that turned requested into fetchURL. The two share a suffix the
// rewrite left alone, and the prefixes before it are what it swapped: when
// served starts with the swapped-in prefix, the swapped-out one replaces it.
// The result stands only if the rewrite really produces served from it, so a
// guess never vouches for a URL the rewrite would not have fetched.
func (f *Fetcher) unrewrite(requested, fetchURL, served string) (string, bool) {
	n := commonSuffix(requested, fetchURL)
	rest, ok := strings.CutPrefix(served, fetchURL[:len(fetchURL)-n])
	if !ok {
		return "", false
	}
	back := requested[:len(requested)-n] + rest
	return back, f.scraper.Rewrite(back) == served
}

func commonSuffix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}

// sameURL compares two URLs as the HTTP client sends them: a request URL is
// re-serialized after parsing, so "/ü" comes back as "/%C3%BC".
func sameURL(a, b string) bool {
	if a == b {
		return true
	}
	ua, errA := neturl.Parse(a)
	ub, errB := neturl.Parse(b)
	return errA == nil && errB == nil && ua.String() == ub.String()
}

// notFound marks a 404 or 410 from the scrape pipeline as docs.ErrNotFound,
// the contract docs.Fetcher implementations share: inside a backend a missing
// discovery file is just absent, and a docs root that is missing reaches the
// surfaces as not-found (exit 3, [not_found]) instead of an upstream failure
// worth retrying. The status is read from the typed error, never its text;
// every other failure is returned unchanged.
func notFound(err error) error {
	var status *scrape.HTTPStatusError
	if errors.As(err, &status) && (status.StatusCode == http.StatusNotFound || status.StatusCode == http.StatusGone) {
		return fmt.Errorf("%w: %w", err, docs.ErrNotFound)
	}
	return err
}
