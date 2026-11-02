package docs

import "context"

// Fetcher is how a docs backend reaches pages it does not get from an API.
// The docs package cannot import scrape, cache or extract (config imports
// docs, and all three import config), so the surfaces inject one built on the
// shared scraper and page cache; cookies, URL rewrites, the browser fallback
// and caching then apply to docs pages exactly as they do to scrape.
//
// A Fetcher may follow redirects, but it must say where each response
// finally came from: the web backend owns its scope (one docs root) and
// drops any response whose URL is outside it, so a page that redirects
// off-site or out of the root's path is never returned under the address
// that was asked for.
type Fetcher interface {
	// FetchRaw returns a response body and its declared content type,
	// unextracted: llms.txt, sitemaps, Markdown pages.
	FetchRaw(ctx context.Context, url string) (RawContent, error)
	// FetchPage returns a page converted to Markdown through the scrape
	// pipeline and the page cache.
	FetchPage(ctx context.Context, url string) (PageContent, error)
}

// RawContent is a FetchRaw response.
type RawContent struct {
	Body        []byte
	ContentType string
	// URL is where Body was finally served from, after any redirects, in the
	// caller's terms: the requested URL when nothing redirected, never a URL
	// rewrite's target. A fetcher that cannot tell leaves it empty, and the
	// web backend then discards the response as unverifiable.
	URL string
}

// PageContent is a FetchPage response.
type PageContent struct {
	Title    string
	Markdown string
	// URL is where the page was finally served from, as for RawContent.URL;
	// for a browser render, the address the browser ended on.
	URL string
}

// Deps carries what a provider may need from the surface that constructs it.
// A zero Deps is valid: factories must stay side-effect free and work without
// one, because LibraryBackends constructs every provider to probe capability.
type Deps struct {
	Fetcher Fetcher
}
