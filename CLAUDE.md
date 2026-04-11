## Ketch

Fast, stateless CLI for agentic web search and scrape. Single Go binary, no daemon.

### Architecture

See [AGENTS.md](AGENTS.md) for full module layout and design principles.

- `cmd/` — Cobra CLI (root, search, scrape, crawl, config, cache, browser)
- `internal/search/` — `Searcher` interface with Brave (default), DDG, and SearXNG backends
- `internal/scrape/` — HTTP fetch + browser fallback via Rod for JS-rendered pages
- `internal/extract/` — readability + html-to-markdown pipeline, JS shell detection heuristic
- `internal/crawl/` — BFS/sitemap crawler with background execution and status tracking
- `internal/config/` — JSON config at `~/.config/ketch/config.json`
- `internal/cache/` — TTL-based page cache backed by bbolt (`~/.cache/ketch/cache.db`)

### Build & Test

```bash
make build          # builds ./ketch
make lint           # golangci-lint (gocyclo max 15)
make test           # go test ./...
```

Pre-commit hook (`.githooks/pre-commit`) runs gofmt, vet, lint, and tests. Git is configured to use `.githooks/` as hooks path.

### Output Format

Default output uses YAML frontmatter + markdown (cymbal style):
- `ketch scrape` — frontmatter (url, title, words) + markdown body
- `ketch search` — frontmatter (query, backend, result_count) + result list
- `--json` flag available on all commands for structured JSON output

### Search Backends (ketch search)

| Backend | Setup | Notes |
|---------|-------|-------|
| `brave` (default) | Free API key from brave.com/search/api | Stable JSON API |
| `ddg` | Zero config | Rate-limited by DDG currently |
| `searxng` | Self-hosted instance | Most reliable for heavy use |

### Code Backends (ketch code)

| Backend | Setup | Notes |
|---------|-------|-------|
| `sourcegraph` (default) | Zero config | Grep-style, ~1M OSS repos, exact line matches, SSE stream |

```bash
ketch code "http.NewRequestWithContext" --lang go
ketch code "rate limit middleware" --lang go --limit 10
ketch config set sourcegraph_url https://sourcegraph.com  # optional, for self-hosted
```

### Docs Backends (ketch docs)

| Backend | Setup | Notes |
|---------|-------|-------|
| `context7` (default) | Free key: `ketch config set context7_api_key <key>` | Curated snippets + prose, version-aware |
| `local` | `ketch docs index <source>` | FTS5 SQLite, offline/private docs (planned) |

```bash
ketch config set context7_api_key ctx7sk_...
ketch docs "how to render with word wrap" --library /charmbracelet/glamour
ketch docs "middleware authentication"          # context7 auto-resolves library
ketch docs --resolve "glamour"                 # list matching library IDs
```

### Browser Rendering

JS-rendered pages (React SPAs, Salesforce Lightning, etc.) are automatically detected and re-fetched via headless Chrome using [Rod](https://go-rod.github.io/). No build tags — Rod is a regular pure Go dependency.

- Detection: `internal/extract/detect.go` — heuristic checks visible text, noscript tags, SPA framework markers, script-to-text ratio
- Browser: `internal/scrape/browser.go` — Rod-based fetch with 30s timeout, WaitLoad + WaitStable
- Config: `ketch config set browser chrome` (or `chromium`, or absolute path)
- Install: `ketch browser install` downloads Chromium to cache dir
- Transparent: agent never knows — same output format, browser is an automatic fallback

### Configuration

`~/.config/ketch/config.json` — JSON config, `encoding/json` from stdlib (no external config libs).

```bash
ketch config              # discovery payload (JSON)
ketch config init         # create default config
ketch config set key val  # update a value
```

### Page Cache

Single bbolt database at platform cache dir (`os.UserCacheDir()/ketch/cache.db`).

```bash
ketch cache               # stats
ketch cache clear         # wipe
ketch scrape --no-cache   # bypass
```

Default TTL: 72h. Configure via `ketch config set cache_ttl 4h`.

The `Store` interface (`internal/cache/cache.go`) allows swapping backends. Default is bbolt; the interface is ready for future backends (redis, etc.).

### Crawl

BFS or sitemap-based crawling with background execution.

```bash
ketch crawl <url>                          # BFS crawl
ketch crawl <url> --sitemap                # sitemap crawl
ketch crawl <url> --background             # detached process, returns crawl ID
ketch crawl status                         # list all crawls
ketch crawl status <id>                    # show progress
ketch crawl stop <id>                      # graceful stop
```

Per-host JS shell tracking: if >80% of pages on a host are JS-rendered (after 10+ samples), remaining pages skip detection and go straight to browser.

### Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/PuerkitoBio/goquery` — HTML parsing (DDG scraping, JS detection)
- `github.com/JohannesKaufmann/html-to-markdown/v2` — HTML→markdown
- `codeberg.org/readeck/go-readability/v2` — Mozilla readability content extraction
- `github.com/go-rod/rod` — Chrome DevTools Protocol for JS-rendered pages
- `go.etcd.io/bbolt` — Embedded key-value store for page cache
- CGO_ENABLED=0, pure Go, cross-compiles everywhere

### Release

GoReleaser + GitHub Actions (`.goreleaser.yaml`, `.github/workflows/release.yml`). Publishes to `1broseidon/homebrew-tap`.

### What's Next

1. Unit tests for extract, search, code, docs, and cache packages
2. `--raw` flag implementation in scrape command
3. Local FTS5 SQLite docs backend (`ketch docs index`) for offline/private docs
