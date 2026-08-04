# Ketch — Architecture

Fast, stateless CLI for agentic web search and scrape.

## Intent & design docs

This file is the *what and where* — module layout and the terse principle list.
Before making a non-trivial change, read the *why*:

- [`docs/DESIGN.md`](docs/DESIGN.md) — the mental model, core abstractions, and
  the reasoning behind each design principle. **Start here**, and read its
  [Non-Goals & Scope](docs/DESIGN.md#non-goals--scope) section before adding a
  feature — it's what keeps a change from pulling ketch in a direction it
  deliberately avoids (a server, a browser-automation framework, a scale
  crawler, an LLM wrapper).
- [`docs/ROADMAP.md`](docs/ROADMAP.md) — possible directions, in dependency
  order. Non-committal; use it to keep independent work compatible.
- [`docs/adr/`](docs/adr/) — Architecture Decision Records for choices already
  made (config overlay, error taxonomy, fast-path scraping).

**Workflow for any change:** read `DESIGN.md` (+ Non-Goals) → make the change
behind the right interface → run the verify loop before you're done:

```bash
make build && make lint && make test
```

`make lint` is `golangci-lint` (gocyclo max 15) and `make test` is
`go test ./...`; the pre-commit hook enforces both, so run them yourself first.
If a change genuinely needs to cross a Non-Goal boundary, that's an
[ADR](docs/adr/)-worthy decision — write down the context and trade-off.

## Module Layout

```
main.go                      Thin entry point → cmd.Execute()
cmd/
  root.go                    Cobra root, global flag (--json)
  search.go                  Search command: query → results, optional --scrape
  scrape.go                  Scrape command: URLs → markdown, concurrent batch
  extract.go                 Extract command: piped HTML → markdown (no fetch/cache/browser)
  crawl.go                   Crawl command: BFS/sitemap crawl with streaming output
  crawl_bg.go                Background crawl: status, stop subcommands, worker mode
  code.go                    Code search command: query → snippet results, --lang qualifier
  docs.go                    Docs search command: query → docs/snippet results, --library, --resolve
  config.go                  Config command: discovery, init, set, path
  cache.go                   Cache command: stats, clear
  browser.go                 Browser command: install, status
  doctor.go                  Doctor command: report formatting + exit-code gating over doctor.Run
  mcp.go                     MCP command: `mcp serve` runs the MCP server over stdio
  proc_unix.go               Unix process management (detach, signals)
  proc_windows.go            Windows process management stub
search/                      Searcher interface + Brave/DDG/SearXNG/EXA/Firecrawl/Keenable backends; NewFromConfig owns the backend switch for cmd/ and mcp/. multi.go adds federated --multi search (RRF fusion, NewMultiFromConfig), canonical.go the URL dedup keys
code/                        code.Searcher interface + GrepApp/Sourcegraph/GitHub backends; NewFromConfig owns the backend switch
docs/                        docs.Searcher interface + Context7 backend (FTS5 local is an unimplemented stub); NewFromConfig owns the backend switch
mcp/                         MCP server (search/code/docs/scrape/crawl tools) over the go-sdk mcp package; Server struct holds the shared scraper + cache, tools call the same NewFromConfig constructors as the CLI
scrape/                      HTTP fetch + Page type, JS detection fallback, Rod browser; pipeline.go has the cache-aware scrape pipeline (CachedScrape*, ScrapeSelector, FetchLLMSTxt) shared by cmd/ and mcp/
extract/                     readability + html-to-markdown pipeline, JS shell detection (Detector: built-in + config spa_markers, modern hydration/streaming frameworks)
crawl/                       BFS crawler, work queue + worker pool, background status
cookies/                     Netscape cookies.txt jar loader + RFC 6265 domain/path matching (Jar.For); nil-safe, values never logged
config/                      JSON config loading/saving (~/.config/ketch/)
doctor/                      Health checks: concurrent read-only probes per backend + browser + cache, status classification (ok/no_key/unreachable/misconfigured/skipped)
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

## MCP Server

`ketch mcp serve` runs an MCP (Model Context Protocol) server over stdio, exposing five tools: `search`, `code`, `docs`, `scrape`, and `crawl`. Tool handlers call the same packages as the Cobra commands, through the same config-driven constructors (`search.NewFromConfig` etc.), and resolve backends/API keys from the same `~/.config/ketch/` config — an agent talking MCP sees exactly what a human using the CLI sees.

- **Lifecycle**: the go-sdk dispatches tool calls concurrently, so process-lifetime resources — the headless-browser scraper, the bbolt page-cache handle, the compiled URL rewriter — are constructed once in `mcp.NewServer`, shared by all calls, and released by `Server.Close` when `serve` exits. Never construct these per call.
- **Option parity**: each tool exposes the per-invocation options of its CLI command (`scrape` gets `selector`/`raw`/`force_browser`/`no_llms_txt`/`trim`/`max_chars`/`no_cache` plus a `urls` batch input; `search` gets `searxng_url`, `scrape`, and `multi` (federated RRF search, with an additive `errors` map for per-backend failures); `crawl` gets `depth`/`sitemap`/`allow`/`deny`/`max_pages`). Config-level settings (API keys, cache TTL, browser binary) stay operator-configured and are never tool params.
- **Error taxonomy**: every tool error starts with a stable machine-readable prefix mirroring the CLI exit codes — `[validation]` (exit 2), `[not_found]` (3), `[upstream]` (4), `[precondition]` (5), `[cancelled]` (6) — so agents can tell "fix your input" from "retry later". MCP has no structured tool-error field; the prefix is the contract.
- **Bounded crawl**: the `crawl` tool is synchronous and capped (`max_pages` default 30, hard cap 100, 3-minute wall clock); partial results return with `stopped: "max_pages" | "timeout"`. Detached background crawls (`ketch crawl --background`, status/stop) remain CLI-only.
- **CLI-only operator commands**: `config`, `cache`, and `doctor` are deliberately not MCP tools. They are operator actions (change credentials, clear state, diagnose the installation), not research surfaces — an agent that needs to know whether a backend is ready reads `ketch config`'s `*_set` booleans or the operator runs `ketch doctor`. Don't add them to the server.
- **Annotations**: all tools are read-only network fetchers and declare `readOnlyHint: true` and `openWorldHint: true`.
- **Security note**: the server performs no URL filtering — `scrape` and `crawl` fetch whatever URL the client supplies, including private or internal addresses reachable from wherever the server runs (their descriptions say so). Run it with the network posture you'd give the agent itself; don't point an untrusted agent at a server inside a sensitive network.
- **Smoke test**: `go test -tags mcpsmoke ./mcp/... -v` exercises the real binary over stdio (live network; not part of `go test ./...`).

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
ketch search "query" -b firecrawl           # use Firecrawl v2 search API
ketch search "query" -b keenable            # use Keenable backend (keyless by default)
ketch search "query" -b tavily              # use Tavily search API (keyed; basic depth)
ketch search "query" --multi                # federate across every usable backend, RRF-fused
ketch search "query" --multi=brave,ddg,exa  # federate across a specific set (use the = form)
ketch scrape <url>                          # single URL → markdown
ketch scrape <url1> <url2> <url3>           # concurrent batch scrape
ketch scrape urls.txt                       # file with one URL per line
ketch scrape '["url1","url2"]'              # JSON array of URLs
echo "url1\nurl2" | ketch scrape            # stdin pipe
curl -L https://example.com | ketch extract # piped HTML → markdown (no fetch)
ketch crawl <url>                           # BFS crawl
ketch crawl <url> --sitemap                 # sitemap-based crawl
ketch crawl <url> --background              # run in background
ketch crawl status [id]                     # check crawl progress
ketch crawl stop <id>                       # stop a background crawl
ketch browser status                        # check browser config
ketch browser install                       # download Chromium
ketch code "query"                          # code search (grepapp)
ketch code "query" --lang go               # with language filter
ketch docs "query"                          # docs search (context7)
ketch docs "query" --library /org/repo     # skip resolve, fetch directly
ketch docs --resolve "library name"        # resolve library name → Context7 IDs
ketch config                                # show effective config + backends (incl. *_key_set presence booleans)
ketch cache                                 # show cache stats
ketch doctor                                # live health check of every backend + browser + cache (exit 5 if a configured surface is broken)
ketch mcp serve                             # run as an MCP server over stdio (search/code/docs/scrape/crawl tools)
```

## Flags

| Flag | Scope | Default | Description |
|------|-------|---------|-------------|
| --json | global | false | JSON output |
| --backend, -b | search | brave | Search backend (brave/ddg/searxng/exa/firecrawl/keenable/tavily) |
| --multi | search | — | Federated search: comma list or bare/`=all` for every usable backend; RRF-fused, dedup'd, mutually exclusive with --backend (use the `=` form for a list) |
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
| --backend, -b | code | grepapp | Code backend (grepapp/sourcegraph/github) |
| --backend, -b | docs | context7 | Docs backend (context7; local is planned, not implemented) |
| --lang | code | — | Language qualifier (appended to query) |
| --library | docs | — | Context7 library ID, skips resolve |
| --tokens | docs | 4000 | Context7 token budget |
| --resolve | docs | false | Resolve library name instead of searching |
| --max-chars N | scrape, search --scrape | 0 (off) | Truncate markdown output to N chars, appends `[truncated]` |
| --trim | scrape, search --scrape | false | Strip markdown formatting syntax, keep content text only |
| --minimal | search, code, docs | false | One result per line, tab-separated, no frontmatter (a 4th backends column is appended under `search --multi`, plain search only) |
| --select \<css\> | scrape | — | Extract only elements matching CSS selector (skips readability) |
| --url | extract | — | Source URL for metadata and relative-link resolution (no fetch) |
| --select \<css\> | extract | — | CSS selector to extract (skips readability) |
| --trim | extract | false | Strip markdown formatting, keep content text only |
| --max-chars N | extract | 0 (off) | Truncate markdown output to N chars, appends `[truncated]` |
| --no-llms-txt | scrape | false | Disable automatic /llms.txt detection for bare domains |
| --concurrency | scrape | 5 | Max concurrent requests for multi-URL scraping |
| --force-browser | scrape | false | Always render via the configured browser, skipping JS-shell auto-detection (composes with --raw/--select; errors without a browser) |
| --cookie-file <path> | scrape, search --scrape, crawl | config `cookie_file` or off | Netscape cookies.txt jar; flag overrides config and an explicit empty value disables cookies |
