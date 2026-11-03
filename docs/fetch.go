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
	// Anchors are the fragment targets the served page carries (element
	// ids and <a name>s), a heading's with its text: what the page's URL
	// plus a fragment can reach. nil when the fetcher cannot tell, as for
	// a Markdown source; empty when the page has none.
	Anchors []PageAnchor
	// Shell is a page known to be an unrendered JS shell: no browser
	// rendered it, so its text is whatever the server sent before scripts
	// ran. Rendered is a page the browser rendered. Both false for a plain
	// HTTP page and when the fetcher cannot tell.
	Shell, Rendered bool
}

// PageAnchor is one fragment target on a page. Text is a heading's
// visible words when the target is a heading's, else "".
type PageAnchor struct {
	ID   string
	Text string
}

// Deps carries what a provider may need from the surface that constructs it.
// A zero Deps is valid: factories must stay side-effect free and work without
// one, because LibraryBackends constructs every provider to probe capability.
type Deps struct {
	Fetcher Fetcher
}

// ResolutionCache is an optional interface a Fetcher implements to keep the
// web backend's resolved names: a verified library[@version] and the docs
// root it resolved to, kept in the page cache under its normal TTL, so a
// repeat query (how assistants work) skips resolution. Values are small
// opaque strings; a miss, an expired entry or a fetcher without a cache is
// ("", false). This is a cache of answers, not an index: nothing is seeded
// and nothing outlives the TTL (ADR-0004).
type ResolutionCache interface {
	GetResolution(key string) (string, bool)
	PutResolution(key, value string)
}
