# Ketch — Architecture

Fast, stateless CLI for agentic web search and scrape.

## Module Layout

```
main.go                      Thin entry point → cmd.Execute()
cmd/
  root.go                    Cobra root, global flags (--json, --backend)
  search.go                  Search command: query → results, optional --scrape
  scrape.go                  Scrape command: URLs → markdown, concurrent batch
  config.go                  Config command: discovery, init, set, path
  cache.go                   Cache command: stats, clear
internal/
  search/
    search.go                Searcher interface + Result type
    brave.go                 Brave Search API backend (default)
    ddg.go                   DuckDuckGo HTML backend
    searxng.go               SearXNG JSON API backend
  scrape/
    scrape.go                HTTP fetch chain + Page type
  extract/
    extract.go               readability + html-to-markdown pipeline
  config/
    config.go                JSON config loading/saving (~/.config/ketch/)
  cache/
    cache.go                 TTL page cache (platform cache dir)
```

## Design Principles

- **Stateless**: no database, no queue, no daemon. Call → result → done.
- **Fast path first**: plain HTTP fetch; browser rendering is a future build-tagged extension.
- **Interface-driven backends**: Searcher interface allows swapping search engines.
- **Concurrent by default**: multiple URLs scraped in parallel via goroutines.
- **Operator configures, agent consumes**: config sets defaults so agents don't need to know infrastructure.

## Quality Standards

- `golangci-lint run` must pass (gocyclo max 15)
- `go test ./...` must pass
- Pre-commit hook enforces both
- CGO_ENABLED=0 — pure Go, cross-compile everywhere

## CLI Usage

```
ketch search "query"                    # search, return results
ketch search "query" --scrape           # search + fetch full content
ketch search "query" -b searxng         # use SearXNG backend
ketch scrape <url>                      # single URL → markdown
ketch scrape <url1> <url2> <url3>       # concurrent batch scrape
ketch config                            # show effective config + backends
ketch cache                             # show cache stats
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
| --no-cache | scrape | false | Bypass page cache |
