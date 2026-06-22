## Ketch

Fast, stateless CLI for agentic web search and scrape. Single Go binary, no daemon.

### Architecture

See [AGENTS.md](AGENTS.md) for full module layout and design principles.

- `cmd/` — Cobra CLI (root, search, code, docs, scrape, crawl, config, cache, browser, version)
- `code/` — `code.Searcher` interface with Grep (default), Sourcegraph, and GitHub backends
- `docs/` — `docs.Searcher` interface with Context7 backend (local FTS5 planned)
- `search/` — `Searcher` interface with Brave (default), DDG, SearXNG, and Exa backends
- `scrape/` — HTTP fetch + browser fallback via Rod for JS-rendered pages
- `extract/` — readability + html-to-markdown pipeline, JS shell detection heuristic
- `crawl/` — BFS/sitemap crawler with background execution and status tracking
- `config/` — JSON config at `~/.config/ketch/config.json`
- `cache/` — TTL-based page cache backed by bbolt (`~/.cache/ketch/cache.db`)

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

### Output Flags (all commands)
| Flag | Scope | Default | Description |
|------|-------|---------|-------------|
| `--max-chars N` | scrape, search --scrape | 0 (off) | Truncate markdown output to N chars, appends `[truncated]` |
| `--trim` | scrape, search --scrape | false | Strip markdown formatting syntax, keep content text only (~30-40% token reduction) |
| `--minimal` | search, code, docs | false | One result per line, tab-separated url/title/snippet, no frontmatter |
| `--select <css>` | scrape | — | Extract only elements matching CSS selector (skips readability) |
| `--no-llms-txt` | scrape | false | Disable automatic /llms.txt detection for bare domains |
| `--raw` | scrape | false | Output raw HTML instead of markdown (skips `/llms.txt`; incompatible with `--select`/`--trim`) |
| `--force-browser` | scrape | false | Always render via the configured browser, skipping JS-shell auto-detection (errors without a browser); composes with `--raw` and `--select` |

### Multi-URL Scraping
ketch scrape detects input mode automatically — no flags needed:
- Multiple args:  ketch scrape url1 url2 url3
- JSON array:     ketch scrape '["url1","url2"]'
- File:           ketch scrape urls.txt
- Stdin pipe:     echo "url1\nurl2" | ketch scrape
- Single:         ketch scrape url

Use --concurrency N (default 5) to control parallel request limit.

### Search Backends (ketch search)

| Backend | Setup | Notes |
|---------|-------|-------|
| `brave` (default) | Free API key from brave.com/search/api | Stable JSON API |
| `ddg` | Zero config | Rate-limited by DDG currently |
| `searxng` | Self-hosted instance | Most reliable for heavy use |
| `exa` | Zero config via hosted MCP; optional `ketch config set exa_api_key <key>` | AI-oriented search with snippets/content from Exa |

### Code Backends (ketch code)

| Backend | Setup | Notes |
|---------|-------|-------|
| `grepapp` (default) | Zero config | Grep MCP (`mcp.grep.app`), no token, literal/regex over 1M+ public repos |
| `sourcegraph` | Zero config | Grep-style, ~1M OSS repos, exact line matches, SSE stream |
| `github` | `gh auth login` or `ketch config set github_token <tok>` | REST `/search/code` + GraphQL stars batch, 30 req/min |

```bash
ketch code "http.NewRequestWithContext" --lang go
ketch code "NewRequestWith.*Context" --regex
ketch code "rate limit middleware" --lang go -b github --limit 10
ketch config set sourcegraph_url https://sourcegraph.com  # optional, for self-hosted
```

### Docs Backends (ketch docs)

| Backend | Setup | Notes |
|---------|-------|-------|
| `context7` (default) | Free key: `ketch config set context7_api_key <key>` | Curated snippets + prose, version-aware |
| `local` | _planned_ | FTS5 SQLite for offline/private docs (not yet implemented) |

```bash
ketch config set context7_api_key ctx7sk_...
ketch docs "how to render with word wrap" --library /charmbracelet/glamour
ketch docs "middleware authentication"          # context7 auto-resolves library
ketch docs --resolve "glamour"                 # list matching library IDs
```

### Browser Rendering

JS-rendered pages (React SPAs, Salesforce Lightning, etc.) are automatically detected and re-fetched via headless Chrome using [Rod](https://go-rod.github.io/). No build tags — Rod is a regular pure Go dependency.

- Detection: `extract/detect.go` — heuristic checks visible text, noscript tags, SPA framework markers, script-to-text ratio
- Browser: `scrape/browser.go` — Rod-based fetch with 30s timeout, WaitLoad + WaitStable
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

### URL Rewrites

Transparently rewrite request URLs before any fetch. Applied uniformly in `scrape`, `search --scrape`, and `crawl`. The original URL is preserved in output frontmatter as `url:`; the actually-fetched URL appears as `fetched_url:` when different. Cache entries are keyed by the rewritten URL so aliased URLs share content.

Rules are an ordered list of `{match, replace}` pairs. `match` is a Go regexp; `replace` may reference capture groups (`$1`, `$2`, …). First match wins.

```bash
# Reddit blocks www.reddit.com even with a browser; old.reddit.com renders as HTML.
ketch config set url_rewrites '[
  {"match":"^https?://www\\.reddit\\.com/(.*)$","replace":"https://old.reddit.com/$1"}
]'

# News sites: pull the RSS feed instead of the rendered landing page.
ketch config set url_rewrites '[
  {"match":"^(https://www\\.theguardian\\.com/uk)$","replace":"$1/rss"}
]'
```

Stored at `~/.config/ketch/config.json` under `url_rewrites`. View with `ketch config`.

### Page Cache

Single bbolt database at platform cache dir (`os.UserCacheDir()/ketch/cache.db`).

```bash
ketch cache               # stats
ketch cache clear         # wipe
ketch scrape --no-cache   # bypass
```

Default TTL: 72h. Configure via `ketch config set cache_ttl 4h`.

The `Store` interface (`cache/cache.go`) allows swapping backends. Default is bbolt; the interface is ready for future backends (redis, etc.).

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

1. Local FTS5 SQLite docs backend (`-b local`) for offline/private docs
