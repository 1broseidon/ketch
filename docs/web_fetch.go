package docs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/httpx"
)

const webUserAgent = "ketch (+https://github.com/1broseidon/ketch)"

// errWebHTML is the fallback fetcher's refusal of a page that needs the
// scrape pipeline to become Markdown.
var errWebHTML = errors.New("HTML page needs the scrape pipeline to convert it to Markdown")

// webHTTPFetcher is the fallback used when no Fetcher is injected: plain GETs
// over the shared transport, with no cookies, rewrites, cache or browser.
// FetchPage returns Markdown and plain-text bodies as they are and refuses
// HTML, which only the injected scrape pipeline can convert.
type webHTTPFetcher struct {
	client *http.Client
}

func newWebHTTPFetcher() webHTTPFetcher {
	client := *httpx.Default()
	// Redirects may not leave the requested host (www. aside): a docs page
	// that redirects off-site is out of scope by definition.
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after %d redirects", len(via))
		}
		if scopeHost(req.URL) != scopeHost(via[0].URL) {
			return fmt.Errorf("redirect to %s leaves %s", req.URL.Host, via[0].URL.Host)
		}
		return httpx.CheckRedirect(req, via) // the context's check (publicHops)
	}
	return webHTTPFetcher{client: &client}
}

// FetchRaw GETs url and returns its body, content type and the URL it was
// served from after redirects (which never leave the host; the backend
// checks the path). 404 and 410 wrap ErrNotFound; other non-2xx statuses
// and bodies over 8 MiB are plain errors.
func (f webHTTPFetcher) FetchRaw(ctx context.Context, url string) (RawContent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return RawContent{}, err
	}
	req.Header.Set("User-Agent", webUserAgent)
	req.Header.Set("Accept", "text/markdown, text/plain;q=0.9, text/html;q=0.8, application/xml;q=0.8, */*;q=0.5")
	resp, err := f.client.Do(req)
	if err != nil {
		return RawContent{}, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return RawContent{}, fmt.Errorf("fetching %s: status %d: %w", url, resp.StatusCode, ErrNotFound)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return RawContent{}, fmt.Errorf("fetching %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, webMaxRawBytes+1))
	if err != nil {
		return RawContent{}, fmt.Errorf("reading %s: %w", url, err)
	}
	if len(body) > webMaxRawBytes {
		return RawContent{}, fmt.Errorf("fetching %s: body exceeds %d bytes", url, webMaxRawBytes)
	}
	return RawContent{Body: body, ContentType: resp.Header.Get("Content-Type"), URL: resp.Request.URL.String()}, nil
}

// FetchPage returns a Markdown or plain-text page as it is, titled by its
// first H1. HTML is an error: converting it needs the scrape pipeline.
func (f webHTTPFetcher) FetchPage(ctx context.Context, url string) (PageContent, error) {
	raw, err := f.FetchRaw(ctx, url)
	if err != nil {
		return PageContent{}, err
	}
	if looksLikeHTML(raw.Body, raw.ContentType) {
		return PageContent{}, fmt.Errorf("web: %s: %w (the CLI and MCP server provide one; this caller did not)", url, errWebHTML)
	}
	md := string(raw.Body)
	return PageContent{Title: firstH1(md), Markdown: md, URL: raw.URL}, nil
}

// looksLikeHTML reports an HTML body, including the 200 HTML fallback many
// single-page sites serve for every path (a missing llms.txt among them).
func looksLikeHTML(body []byte, contentType string) bool {
	head := bytes.ToLower(bytes.TrimSpace(body[:min(len(body), 512)]))
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	for _, p := range []string{"<!doctype html", "<html", "<head", "<body"} {
		if bytes.HasPrefix(head, []byte(p)) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(contentType), "html") && bytes.HasPrefix(head, []byte("<"))
}

// probe fetches a discovery file under the per-probe timeout and returns it
// with the URL it was served from. Any failure reports ok=false: a missing
// file, an HTML stand-in for one, or a response from another host (a
// redirect off the docs host makes the file someone else's).
func (w *Web) probe(ctx context.Context, root webRoot, loc string) ([]byte, *url.URL, bool) {
	requested, err := url.Parse(loc)
	if err != nil || !root.sameHost(requested) {
		return nil, nil, false
	}
	pctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	defer cancel()
	raw, err := w.fetcher.FetchRaw(pctx, loc)
	if err != nil || len(bytes.TrimSpace(raw.Body)) == 0 || looksLikeHTML(raw.Body, raw.ContentType) {
		return nil, nil, false
	}
	from, err := servedFrom(requested, raw.URL, root.sameHost)
	if err != nil {
		return nil, nil, false
	}
	return raw.Body, from, true
}

// fetchOne fetches a candidate: Markdown links through FetchRaw (falling
// back to FetchPage when the server answers with HTML), everything else
// through the scrape pipeline. The page is shown at the URL it was served
// from, and refused (a scopeError) when that is outside the docs root; a
// raw fetch that redirected away is not retried through the pipeline,
// which would only follow the same redirect.
func (w *Web) fetchOne(ctx context.Context, root webRoot, link webLink) (*webPage, error) {
	u := link.url.String()
	var from *url.URL
	title, md := "", ""
	var anchors []PageAnchor
	fromRaw, shell := false, false
	if isMarkdownURL(link.url) {
		raw, err := w.fetcher.FetchRaw(ctx, u)
		if err == nil {
			from, err = servedFrom(link.url, raw.URL, root.contains)
		}
		if err != nil {
			return nil, err
		}
		if !looksLikeHTML(raw.Body, raw.ContentType) {
			md, fromRaw = string(raw.Body), true
		}
	}
	if md == "" {
		page, err := w.fetcher.FetchPage(ctx, u)
		if err == nil {
			from, err = servedFrom(link.url, page.URL, root.contains)
		}
		if err != nil {
			return nil, err
		}
		title, md, anchors, shell = page.Title, page.Markdown, page.Anchors, page.Shell
	}
	md = capText(md, w.maxPageBytes)
	fmTitle, md := stripFrontmatter(md)
	via := ""
	if fromRaw {
		via = readMD + " " + from.String()
	}
	return &webPage{
		URL:      displayURL(from),
		Title:    firstNonEmpty(fmTitle, cleanPageTitle(title), link.title, pathTitle(from.Path)),
		Markdown: md,
		Anchors:  anchors,
		markdown: fromRaw,
		shell:    shell,
		via:      via,
	}, nil
}

// capText cuts s to at most n bytes at a line boundary.
func capText(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// pathTitle derives a last-resort title from a URL path.
func pathTitle(p string) string {
	base := path.Base(strings.TrimSuffix(p, "/"))
	base = strings.TrimSuffix(base, path.Ext(base))
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return strings.NewReplacer("-", " ", "_", " ").Replace(base)
}

// cleanPageTitle drops a trailing site-name suffix from an HTML title
// ("Content collections | Astro Docs" → "Content collections").
func cleanPageTitle(title string) string {
	title = strings.TrimSpace(strings.Map(dropInvisible, title))
	for _, sep := range []string{" | ", " – ", " — ", " · "} {
		if i := strings.Index(title, sep); i > 0 {
			return strings.TrimSpace(title[:i])
		}
	}
	return title
}

// callFetcher is the backend's Fetcher: the injected one, with the
// fetches of one call (a Search, GetDocs or ResolveLibrary; withFetchMemo)
// shared. Identification, version discovery and page discovery read many
// of the same files (a root page, its llms.txt, a 10 MB pkg.go.dev page)
// at different stages, and each read was a request: about one in seven of
// a call's requests repeated one it had made already, and a discovery file
// that never answers within its timeout (a 5 MB llms.txt generated on
// demand) cost that timeout every time it was asked for. Outside a call
// (no memo in the context) every fetch goes through.
type callFetcher struct{ inner Fetcher }

// fetchMemoKey is the context key of a call's fetchMemo.
type fetchMemoKey struct{}

// fetchMemo holds one call's fetches by URL, raw and page apart.
type fetchMemo struct {
	mu   sync.Mutex
	raw  map[string]*memoFetch[RawContent]
	page map[string]*memoFetch[PageContent]
}

// memoFetch is one fetch, in flight until done is closed. budget is the
// time the fetch had (until its context's deadline; 0 for none).
type memoFetch[T any] struct {
	done   chan struct{}
	val    T
	err    error
	budget time.Duration
}

// withFetchMemo returns ctx with a fetch memo for one call, unless it
// already carries one.
func withFetchMemo(ctx context.Context) context.Context {
	if _, ok := ctx.Value(fetchMemoKey{}).(*fetchMemo); ok {
		return ctx
	}
	return context.WithValue(ctx, fetchMemoKey{}, &fetchMemo{raw: map[string]*memoFetch[RawContent]{}, page: map[string]*memoFetch[PageContent]{}})
}

func (f callFetcher) FetchRaw(ctx context.Context, url string) (RawContent, error) {
	memo, _ := ctx.Value(fetchMemoKey{}).(*fetchMemo)
	if memo == nil {
		return f.inner.FetchRaw(ctx, url)
	}
	return memoized(ctx, &memo.mu, memo.raw, memoKey(ctx, url), url, f.inner.FetchRaw)
}

func (f callFetcher) FetchPage(ctx context.Context, url string) (PageContent, error) {
	memo, _ := ctx.Value(fetchMemoKey{}).(*fetchMemo)
	if memo == nil {
		return f.inner.FetchPage(ctx, url)
	}
	return memoized(ctx, &memo.mu, memo.page, memoKey(ctx, url), url, f.inner.FetchPage)
}

// GetResolution and PutResolution pass the injected fetcher's resolution
// cache through (ResolutionCache), when it has one.
func (f callFetcher) GetResolution(key string) (string, bool) {
	if rc, ok := f.inner.(ResolutionCache); ok {
		return rc.GetResolution(key)
	}
	return "", false
}

func (f callFetcher) PutResolution(key, value string) {
	if rc, ok := f.inner.(ResolutionCache); ok {
		rc.PutResolution(key, value)
	}
}

// memoized fetches url once per call: a caller asking for a URL another
// caller is fetching waits for that fetch and shares its answer. A fetch
// the caller's own context ended is not an answer about the URL: it is
// forgotten when cancelled, and when its deadline ran out it answers
// only a caller with no more time than it had (the same probe timeout
// gets the same timeout; a caller with longer to wait asks again).
func memoized[T any](ctx context.Context, mu *sync.Mutex, calls map[string]*memoFetch[T], key, url string, fetch func(context.Context, string) (T, error)) (T, error) {
	for {
		mu.Lock()
		c, ok := calls[key]
		if !ok {
			c = &memoFetch[T]{done: make(chan struct{}), budget: budgetOf(ctx)}
			calls[key] = c
			mu.Unlock()
			c.val, c.err = fetch(ctx, url)
			close(c.done)
			return c.val, c.err
		}
		mu.Unlock()
		select {
		case <-c.done:
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
		if reusable(c.err, c.budget, budgetOf(ctx)) {
			return c.val, c.err
		}
		if ctx.Err() != nil {
			var zero T
			return zero, ctx.Err()
		}
		mu.Lock()
		if calls[key] == c {
			delete(calls, key) // ended by its own context: ask again
		}
		mu.Unlock()
	}
}

// reusable reports whether a fetch that ended with err, having had budget
// to run, answers a caller with budget left to wait: any answer from the
// server does, a cancellation never does, and a timeout does unless the
// caller can wait at least twice as long as the fetch could (one cut
// short by a nearly spent outer deadline is asked again by a caller with
// a probe's full timeout; two probes under one timeout are one probe).
func reusable(err error, had, left time.Duration) bool {
	switch {
	case errors.Is(err, context.Canceled):
		return false
	case errors.Is(err, context.DeadlineExceeded):
		return had > 0 && left > 0 && left < 2*had
	}
	return true
}

// memoKey keys a fetch by its URL and by whether its redirects are
// checked (publicHops): an unchecked fetch never answers a checked one.
func memoKey(ctx context.Context, url string) string {
	if httpx.HasRedirectCheck(ctx) {
		return "checked\x00" + url
	}
	return url
}

// budgetOf is the time left until ctx's deadline; 0 when it has none.
func budgetOf(ctx context.Context) time.Duration {
	if d, ok := ctx.Deadline(); ok {
		return max(time.Until(d), time.Nanosecond)
	}
	return 0
}
