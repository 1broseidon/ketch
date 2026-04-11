# Ketch — Architecture

Fast, stateless CLI for agentic web search and scrape.

## Module Layout

```
main.go                      Thin entry point → cmd.Execute()
cmd/
  root.go                    Cobra root, global flags (--json, --backend)
  search.go                  Search command: query → results, optional --scrape
  scrape.go                  Scrape command: URLs → markdown, concurrent batch
  crawl.go                   Crawl command: BFS/sitemap crawl with streaming output
  crawl_bg.go                Background crawl: status, stop subcommands, worker mode
  code.go                    Code search command: query → snippet results, --lang qualifier
  docs.go                    Docs search command: query → docs/snippet results, --library, --resolve
  config.go                  Config command: discovery, init, set, path
  cache.go                   Cache command: stats, clear
  browser.go                 Browser command: install, status
  proc_unix.go               Unix process management (detach, signals)
  proc_windows.go            Windows process management stub
internal/
  search/
    search.go                Searcher interface + Result type
    brave.go                 Brave Search API backend (default)
    ddg.go                   DuckDuckGo HTML backend
    searxng.go               SearXNG JSON API backend
  code/
    code.go                  code.Searcher interface + Result type
    sourcegraph.go           Sourcegraph SSE streaming backend (no auth required)
  docs/
    docs.go                  docs.Searcher interface + Result type
    context7.go              Context7 two-step resolve+fetch backend (API key required)
    fts5.go                  Local FTS5 SQLite backend stub (planned)
  scrape/
    scrape.go                HTTP fetch chain + Page type, JS detection fallback
    browser_iface.go         BrowserConn interface + ResolveBrowserBin
    browser.go               Rod-based headless Chrome fetch
  extract/
    extract.go               readability + html-to-markdown pipeline
    detect.go                JS shell detection heuristic
  crawl/
    crawl.go                 BFS crawler: work queue, worker pool, per-host JS tracking
    status.go                Background crawl status: read/write/list JSON status files
  config/
    config.go                JSON config loading/saving (~/.config/ketch/)
  cache/
    cache.go                 Cache struct (TTL, Store interface), cacheKey hashing
    bbolt.go                 BBoltStore: embedded key-value cache backend
```

## Design Principles

- **Stateless**: no daemon, no queue. Call → result → done. Background crawls are detached processes, not a server.
- **Fast path first**: plain HTTP fetch; browser rendering kicks in automatically when JS shell is detected.
- **Interface-driven backends**: `Searcher` for search engines, `Store` for cache backends, `BrowserConn` for browser rendering.
- **Concurrent by default**: multiple URLs scraped in parallel via goroutines.
- **Operator configures, agent consumes**: config sets defaults (backend, browser, cache TTL) so agents don't need to know infrastructure.
- **Three search surfaces**: `ketch search` finds web pages, `ketch code` greps real OSS code, `ketch docs` fetches library documentation. Each has its own backend interface and Result type — they never share backends.

## Quality Standards

- `golangci-lint run` must pass (gocyclo max 15)
- `go test ./...` must pass
- Pre-commit hook enforces both
- CGO_ENABLED=0 — pure Go, cross-compile everywhere

## CLI Usage

```
ketch search "query"                        # search, return results
ketch search "query" --scrape               # search + fetch full content
ketch search "query" -b searxng             # use SearXNG backend
ketch scrape <url>                          # single URL → markdown
ketch scrape <url1> <url2> <url3>           # concurrent batch scrape
ketch crawl <url>                           # BFS crawl
ketch crawl <url> --sitemap                 # sitemap-based crawl
ketch crawl <url> --background              # run in background
ketch crawl status [id]                     # check crawl progress
ketch crawl stop <id>                       # stop a background crawl
ketch browser status                        # check browser config
ketch browser install                       # download Chromium
ketch code "query"                          # code search (sourcegraph)
ketch code "query" --lang go               # with language filter
ketch docs "query"                          # docs search (context7)
ketch docs "query" --library /org/repo     # skip resolve, fetch directly
ketch docs --resolve "library name"        # resolve library name → Context7 IDs
ketch config                                # show effective config + backends
ketch cache                                 # show cache stats
```

## Flags

| Flag | Scope | Default | Description |
|------|-------|---------|-------------|
| --json | global | false | JSON output |
| --backend, -b | global | brave | Search backend |
| --limit, -l | search | 5 | Max results |
| --scrape | search | false | Fetch full content |
| --searxng-url | search | http://localhost:8081 | SearXNG URL |
| --raw | scrape | false | Raw HTML output |
| --no-cache | scrape, crawl | false | Bypass page cache |
| --depth | crawl | 3 | Max BFS depth |
| --concurrency | crawl | 8 | Worker pool size |
| --sitemap | crawl | false | Treat seed URL as sitemap |
| --background | crawl | false | Run in background |
| --allow | crawl | — | Path substring filters |
| --deny | crawl | — | Regex deny patterns |
| --backend, -b | code, docs | cfg value | Code/docs backend |
| --lang | code | — | Language qualifier (appended to query) |
| --library | docs | — | Context7 library ID, skips resolve |
| --tokens | docs | 4000 | Context7 token budget |
| --resolve | docs | false | Resolve library name instead of searching |
