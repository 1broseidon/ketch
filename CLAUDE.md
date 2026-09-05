## Ketch

Fast, stateless CLI for agentic web search and scrape. Single Go binary, no daemon.

### Architecture

See [AGENTS.md](AGENTS.md) for full module layout and design principles, and [design/DESIGN.md](design/DESIGN.md) for the reasoning behind them — read its [Non-Goals & Scope](design/DESIGN.md#non-goals--scope) before adding a feature.

- `cmd/` — Cobra CLI (root, search, code, docs, scrape, extract, crawl, config, cache, browser, doctor, mcp, version)
- `code/` — `code.Searcher` interface with Grep (built-in default), Sourcegraph, and GitHub backends
- `docs/` — `docs.Searcher` interface with Context7 backend (local FTS5 planned)
- `search/` — `Searcher` interface with Brave (built-in default), DDG, SearXNG, Exa, Firecrawl, Keenable, Tavily, Parallel, and SerpBase backends
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
- `ketch search` — frontmatter (query, backend, result_count) + result list. Under `--multi` the frontmatter uses `backends:` (plural, the engines that contributed) plus a `failed:` key for partial failures, and each result gains a `found in:` line / `backends` JSON field
- `--json` flag available on all commands for structured JSON output

### Output Flags (all commands)
| Flag | Scope | Default | Description |
|------|-------|---------|-------------|
| `--max-chars N` | scrape, search --scrape | 0 (off) | Truncate markdown output to N chars, appends `[truncated]` |
| `--trim` | scrape, search --scrape | false | Strip markdown formatting syntax, keep content text only (~30-40% token reduction) |
| `--minimal` | search, code, docs | false | One result per line, tab-separated url/title/snippet, no frontmatter (a 4th backends column is appended under `search --multi`, plain search only — `--scrape --minimal` keeps 3 columns) |
| `--multi[=list]` | search | — | Federated search across backends (RRF fusion, URL-dedup'd); bare/`=all` = every usable backend, `=brave,exa` = a set; mutually exclusive with `--backend` |
| `--select <css>` | scrape | — | Extract only elements matching CSS selector (skips readability) |
| `--no-llms-txt` | scrape | false | Disable automatic /llms.txt detection for bare domains |
| `--raw` | scrape | false | Output raw HTML instead of markdown (skips `/llms.txt`; incompatible with `--select`/`--trim`) |
| `--force-browser` | scrape | false | Always render via the configured browser, skipping JS-shell auto-detection (errors without a browser); composes with `--raw` and `--select` |
| `--cookie-file <path>` | scrape, search --scrape, crawl | off (config `cookie_file`) | Netscape cookies.txt jar; matching cookies attached at both fetch layers. Overrides `cookie_file`; empty value disables. Values never printed |

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
| `firecrawl` | API key: `ketch config set firecrawl_api_key <key>` (hosted); optional `firecrawl_url` for self-hosted | Firecrawl v2 search API; same provider as scrape/crawl workflows |
| `keenable` | Zero config (keyless public endpoint); optional `ketch config set keenable_api_key <key>` | Agent-oriented web search over the Keenable index; key lifts the rate limit |
| `tavily` | API key: `ketch config set tavily_api_key <key>` | Agent-oriented search; fills Content from extracted text; basic depth (1 credit) by default |
| `parallel` | Zero config | Current web search through Parallel's hosted Search MCP endpoint |
| `serpbase` | API key: `ketch config set serpbase_api_key <key>` | Google search results through the SerpBase REST API |

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
ketch config set firecrawl_url http://localhost:3002      # optional, self-hosted Firecrawl
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

- Detection: `extract/detect.go` — heuristic checks visible text, noscript tags, SPA framework markers, script-to-text ratio. Covers modern hydration/streaming frameworks (Next.js App Router `self.__next_f`, React 18 streaming, Vue 3 `data-v-app`, SvelteKit, Qwik, Astro islands, empty mount nodes). A content-is-client-rendered override escalates pages whose server-rendered chrome looks "static" when a strong framework marker is present **and** the script payload dwarfs the visible text. Operators can add substrings via `spa_markers` (see SPA Markers below).
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

### SPA Markers

Escape hatch for the JS-shell detector's long tail. A page whose HTML contains any of these substrings is treated as JS-rendered and re-fetched via the browser — matched (case-insensitively) alongside the built-in framework markers. Use it when a site renders content client-side via a framework or token ketch doesn't yet recognize, instead of hardcoding markers in Go.

```bash
# Treat pages carrying these tokens as JS-rendered.
ketch config set spa_markers '["__next_f","data-v-app"]'

# Clear the list.
ketch config set spa_markers '[]'
```

Stored at `~/.config/ketch/config.json` under `spa_markers`. Blank markers are rejected (a `""` would match every page). Markers feed the same detector path as the built-ins, including the content-is-client-rendered override. View with `ketch config`.

### Cookies

BYO cookies for session/consent-gated pages (issue #25). ketch loads a **Netscape `cookies.txt`** jar (browser-extension / curl / yt-dlp format) and attaches matching cookies at **both** fetch layers — the HTTP path (`scrape/scrape.go`) and the Rod browser path (`scrape/browser.go`, cookies set before navigation, which is what fixes consent-banner walls like Nvidia NGC docs).

```bash
# Per-invocation flag (scrape, search --scrape, crawl):
ketch scrape <url> --cookie-file ~/cookies.txt

# Persistent config key; flag overrides it (empty flag disables for the run):
ketch config set cookie_file ~/cookies.txt
```

- Matching enforces Domain, HostOnly, Path, and Secure scope against every request and redirect. Malformed scope fields are rejected, and expiry is rechecked per request; the `#HttpOnly_` line prefix is honored.
- The `cookies/` package (`cookies.Load`/`Parse`/`Jar.For`) owns parsing and matching; `Jar` methods are nil-safe so scrapers built without a jar behave exactly as before.
- **Cache keys**: a short jar fingerprint is folded into `Scraper.CacheKey` whenever the configured jar has live cookies (an explicitly configured `user_agent` is folded in the same way, as a digest, so switching UAs never serves a page fetched under another one). The namespace is isolated even when the initial URL has no match, because a redirect may land in cookie scope. Crawl uses the same key. Authenticated content is still stored locally; POSIX cache directory/database modes are `0700`/`0600`, and `--no-cache` prevents storage.
- **Hygiene**: cookie values are never printed anywhere (frontmatter, `--json`, errors, `ketch doctor`). `ketch doctor` reports `cookies/jar: configured (N cookies, M expired)`. A group/world-readable jar triggers a one-time stderr warning (`chmod 600` recommended). Respecting site ToS and using only your own cookies is the operator's responsibility.

Stored at `~/.config/ketch/config.json` under `cookie_file`. View with `ketch config` (path only, never values).

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
