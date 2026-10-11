---
id: task-6
title: "Crawl and scrape politeness: what to borrow from go-colly"
column: todo
position: 4
priority: medium
tags:
  - improvement
  - bro-93
createdAt: "2026-10-11T21:01:31.075Z"
---

## Description
go-colly ([https://github.com/gocolly/colly](<https://github.com/gocolly/colly>)) was evaluated on 2026-09-20 as a replacement fetch and crawl layer and rejected: it would add 16 modules (65 → 81, including the deprecated `google.golang.org/appengine` and `github.com/golang/protobuf`, three XPath libraries and a second charset detector), +2.8 MB on the stripped binary (21.3 → 24.1 MB), it has no browser or JS path, and its callback model has no place for ketch's detect-shell-then-render flow. It shipped one release between June 2020 and March 2025. What it does well is request politeness and crawl hygiene, and ketch's own fetch path is missing most of that. This issue lists what to borrow, where it lands, and what to leave. Nothing here gates v0.18.0.

## Gaps in ketch today (verified in the tree on 2026-09-20)

* `scrape/scrape.go` `cookieRoundTripper` injects cookies from the cookies.txt jar but never stores `Set-Cookie` from responses; there is no session jar anywhere. A site that sets a consent or session cookie on the first response and redirects back to itself serves that same first page, or a loop, to every worker.
* No `CheckRedirect` anywhere: redirect targets are never re-checked against the host, allow and deny filters, and are never marked visited. A crawl of `docs.example.com` follows a 301 to another host and fetches it; `a → b` plus a later direct link to `b` fetches `b` twice.
* `crawl.passesFilters` requires `u.Hostname() == seedHost`: a seed on `example.com` whose links point at `www.example.com` (or the reverse) crawls only the seed page.
* No robots.txt handling and no per-host delay or concurrency cap: `crawl` runs 8 workers against one host with no pause, `scrape` batch runs 5.
* No retry: a 429, 502, 503, 504 or a connection reset is a lost page in `crawl` and `search --scrape`.
* `fetchContent` reads up to 20 MiB before the content type is classified, so links to zips, images and videos found in `<a href>` are downloaded in full and then discarded.
* Sitemap fetch has no gzip handling: `sitemap.xml.gz` fails with "failed to parse sitemap XML"; errors in sitemap-index children are silently skipped.
* `normalizeURL` strips fragment, `utm_*` and trailing slash but does not lowercase scheme and host or drop default ports, so `HTTPS://Example.com:443/a` and `https://example.com/a` are two visited entries.
* The MCP page cap is enforced outside the crawler (`mcp/crawl.go` cancels the context when `max_pages` is reached); the CLI has no cap at all.
* Link-followed fetches send no `Referer`.

## Borrow (rough value order)

 1. **Session cookie jar.** Layer a per-Scraper `net/http/cookiejar` over the file jar: file cookies seed it, response cookies are retained for the Scraper's lifetime (one crawl, one MCP server process) and never written back to the file. This is colly's default. Include colly's redirect-cycle exemption from `checkRedirectFunc`: a redirect back to a URL already in the `via` chain is a cookie-setting bounce, not a revisit.
 2. **Redirect policy** (colly `checkRedirectFunc`): a `CheckRedirect` on the scrape client that re-applies host, allow and deny filters to the target, marks the final URL visited, and stops at 10 hops. The crawl result should carry the final URL; `Page.FetchedURL` is the natural slot.
 3. **Host equivalence and allow-list.** Treat `www.` and apex as one host by default; add `--allow-host` (colly `AllowedDomains`) for docs sites that span `docs.x.com` and `x.com`. MCP `crawl` gets `allow_hosts`.
 4. **Per-host politeness** (colly `LimitRule`: `Parallelism`, `Delay`, `RandomDelay` matched by domain): a per-host semaphore plus delay with jitter inside the Scraper so `scrape` batch, `search --scrape` and `crawl` all share it. Config keys (for example `crawl_delay`, `per_host_concurrency`) and crawl flags. Honour robots `Crawl-delay` when present.
 5. **robots.txt for crawl** (colly `checkRobots`: fetched once per host, cached in a map, `FindGroup(UA).Test(path)`): default on for `crawl` with `--ignore-robots`; `scrape` stays user-directed like a browser and never consults it. Colly itself defaults to ignoring robots, so this is stricter than colly. Bonus: `Sitemap:` lines in robots.txt let `--sitemap` work on a bare domain.
 6. **Header gate before the body** (colly's abort in the response-headers callback): after `client.Do`, skip when `Content-Type` is not html, xhtml, pdf, xml or plain text, or `Content-Length` exceeds `MaxBodyBytes`, without reading the body; report `skipped: content-type` in the crawl result.
 7. **Bounded retry for transient failures** (seeded by colly `Request.Retry`, which strips the Cookie header and resubmits): GET only, at most 2 attempts, for 429 (honour `Retry-After` up to a few seconds), 502, 503, 504, timeouts and resets; never for other 4xx. Keep it small so the fast path stays fast.
 8. **Referer on link-followed fetches** (colly `extensions.Referer`): pass the parent URL through `ScrapeConditional` for crawl links only.
 9. `MaxPages` **in** `crawl.Options` (colly `MaxRequests`): shared by the CLI (`--max-pages`) and MCP, stops enqueuing rather than cancelling in-flight work. Count and report skipped links by reason (filtered, robots, max-pages, content-type) in the summary and JSON so an agent understands a short crawl; colly's typed errors (`ErrRobotsTxtBlocked`, `ErrMaxDepth`, `ErrForbiddenDomain`) are the model.
10. **URL normalisation**: lowercase scheme and host, drop default ports. Colly moved to a WHATWG parser in 2.2; ketch only needs these two rules.
11. **gzip sitemaps**: check the gzip magic bytes (`1f 8b`) on sitemap bodies regardless of headers (colly's `.xml.gz` handling in `httpBackend.Do`); surface sitemap-index child errors as warnings instead of dropping them.
12. Optional: per-fetch timing (`fetch_ms`) in JSON results (colly `TraceHTTP`) for tuning.

## Skip

* The callback API, `CacheDir` disk cache and `CacheExpiration`: ketch's cache keys fold in UA, cookies and extract mode; colly's is a URL hash.
* `chardet` and `DetectCharset`: `DecodeHTMLBytes` already does BOM → header → meta prescan → UTF-8 validation.
* XPath (`OnXML`, htmlquery, xmlquery), the `queue` and `storage` backends and distributed crawling (ketch is not a crawler at scale, see design/DESIGN.md), `CheckHead` (an extra round trip; the header gate above is cheaper), `extensions.RandomUserAgent` (ketch identifies itself with a fixed UA by design), `COLLY_*` env config, the proxy switcher (the transport already honours `HTTP(S)_PROXY` through `http.ProxyFromEnvironment`).
* `Authorization` stripping on cross-host redirects: Go's client already does it.

## Implementation notes

* Every item is a behaviour change visible to agents: CHANGELOG entry, README, site and skill surfaces, and MCP parity for each new CLI flag.
* robots.txt default-on can shrink crawls that worked before; ship it with the flag and a one-line stderr note when pages were skipped for robots.
* Tests: an httptest fixture server in `crawl` for redirect to another host, www/apex links, a cookie bounce, 429 with `Retry-After`, a gzip sitemap and asset links.

Suggested order: 1–3 (correctness), then 4–5 (politeness), then 6–9 (efficiency and reporting), then 10–12.

Migrated from Linear BRO-93: https://linear.app/1broseidon/issue/BRO-93/crawl-and-scrape-politeness-what-to-borrow-from-go-colly (2026-10-11).

## Status (2026-10-11 review)

None of the 12 items in code yet; still accurate.
