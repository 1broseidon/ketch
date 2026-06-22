# Ketch — Architecture

Fast, stateless CLI for agentic web search and scrape.

## Module Layout

```
main.go                      Thin entry point → cmd.Execute()
cmd/
  root.go                    Cobra root, global flag (--json)
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
search/                      Searcher interface + Brave/DDG/SearXNG/EXA backends
code/                        code.Searcher interface + Sourcegraph/GitHub backends
docs/                        docs.Searcher interface + Context7/FTS5 backends
scrape/                      HTTP fetch + Page type, JS detection fallback, Rod browser
extract/                     readability + html-to-markdown pipeline, JS shell detection
crawl/                       BFS crawler, work queue + worker pool, background status
config/                      JSON config loading/saving (~/.config/ketch/)
cache/                       TTL page cache (Store interface, BBoltStore backend)
httpx/                       Shared tuned *http.Transport for all HTTP backends
updatecheck/                 "new release available" probe + throttled stderr hint
site/                        VitePress documentation site (deployed to gh-pages)
```

Reusable packages live at the module root so external programs can `import "github.com/1broseidon/ketch/<pkg>"`. Nothing is module-private right now — if something becomes CLI-only, move it under `internal/`.

## Design Principles

- **Stateless**: no daemon, no queue. Call → result → done. Background crawls are detached processes, not a server.
- **Fast path first**: plain HTTP fetch; browser rendering kicks in automatically when JS shell is detected.
- **Interface-driven backends**: `Searcher` for search engines, `Store` for cache backends, `BrowserConn` for browser rendering.
- **Concurrent by default**: multiple URLs scraped in parallel via goroutines.
- **Operator configures, agent consumes**: config sets defaults (backend, browser, cache TTL) so agents don't need to know infrastructure.
- **Three search surfaces**: `ketch search` finds web pages, `ketch code` greps real OSS code, `ketch docs` fetches library documentation. Each has its own backend interface and Result type — they never share backends.
- **Smart input detection on scrape**: single URL, multiple positional args, JSON array string, file path, or stdin pipe all work — ketch routes automatically. No --batch flag needed.
- **Context-aware interfaces**: all three Searcher interfaces (`search`, `code`, `docs`) take `context.Context` as first param for cancellation and timeout propagation.

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
ketch search "query" -b exa                 # use Exa hosted MCP backend
ketch scrape <url>                          # single URL → markdown
ketch scrape <url1> <url2> <url3>           # concurrent batch scrape
ketch scrape urls.txt                       # file with one URL per line
ketch scrape '["url1","url2"]'              # JSON array of URLs
echo "url1\nurl2" | ketch scrape            # stdin pipe
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
| --backend, -b | search | brave | Search backend (brave/ddg/searxng/exa) |
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
| --backend, -b | code | sourcegraph | Code backend (sourcegraph/github) |
| --backend, -b | docs | context7 | Docs backend (context7/local) |
| --lang | code | — | Language qualifier (appended to query) |
| --library | docs | — | Context7 library ID, skips resolve |
| --tokens | docs | 4000 | Context7 token budget |
| --resolve | docs | false | Resolve library name instead of searching |
| --max-chars N | scrape, search --scrape | 0 (off) | Truncate markdown output to N chars, appends `[truncated]` |
| --trim | scrape, search --scrape | false | Strip markdown formatting syntax, keep content text only |
| --minimal | search, code, docs | false | One result per line, tab-separated, no frontmatter |
| --select \<css\> | scrape | — | Extract only elements matching CSS selector (skips readability) |
| --no-llms-txt | scrape | false | Disable automatic /llms.txt detection for bare domains |
| --concurrency | scrape | 5 | Max concurrent requests for multi-URL scraping |
| --force-browser | scrape | false | Always render via the configured browser, skipping JS-shell auto-detection (composes with --raw/--select; errors without a browser) |
