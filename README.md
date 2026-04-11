# ketch

Fast, stateless CLI for web search, code search, library docs, and scraping. Three search surfaces (web, code, docs), one binary, no daemon. Designed to be called by AI agents or directly from your terminal.

## Install

Homebrew:

```sh
brew install 1broseidon/tap/ketch
```

Or with Go:

```sh
go install github.com/1broseidon/ketch@latest
```

Or grab a binary from [releases](https://github.com/1broseidon/ketch/releases).

## Quick start

```sh
# Search the web
ketch search "golang error handling"

# Search and fetch full content from each result
ketch search "golang error handling" --scrape

# Search real OSS code (Sourcegraph by default, or GitHub)
ketch code "http.NewRequestWithContext" --lang go
ketch code "rate limit middleware" --lang go -b github

# Search library docs (Context7)
ketch docs "how to render with word wrap" --library /charmbracelet/glamour

# Scrape a URL to clean markdown
ketch scrape https://go.dev/doc/effective_go

# Scrape multiple URLs concurrently
ketch scrape https://example.com https://go.dev

# Crawl a site
ketch crawl https://docs.example.com --depth 2

# Crawl a sitemap in the background
ketch crawl https://example.com/sitemap.xml --sitemap --background

# JSON output for piping
ketch search "query" --json
ketch code "query" --json
ketch scrape https://example.com --json
```

## Commands

| Command | What it does |
|---------|-------------|
| `search` | Web search via Brave, DuckDuckGo, or SearXNG, optional `--scrape` for full content |
| `code` | Code search across OSS via Sourcegraph (default) or GitHub Code Search |
| `docs` | Library/framework docs via Context7 (curated, version-aware snippets) |
| `scrape` | Fetch URLs and extract clean markdown, concurrent batch support |
| `crawl` | BFS or sitemap crawl with background execution and status tracking |
| `browser` | Manage headless Chrome for JS-rendered pages |
| `config` | Show effective configuration and available backends as JSON |
| `cache` | Show cache stats or clear cached pages |

All commands support `--json` for structured output.

## Browser rendering

JS-rendered pages (React SPAs, Salesforce Lightning, etc.) are automatically detected and re-fetched via headless Chrome. No extra setup if Chrome is already installed:

```sh
# Point ketch to your Chrome installation
ketch config set browser chrome

# Or install Chromium to ketch's cache dir
ketch browser install

# Check browser status
ketch browser status
```

Once configured, browser rendering is transparent — `ketch scrape` and `ketch crawl` automatically detect JS-rendered pages and use the browser when needed. Static pages are always fetched via plain HTTP (fast path).

## Crawling

Crawl entire sites via BFS link discovery or sitemaps:

```sh
# BFS crawl from a seed URL
ketch crawl https://docs.example.com --depth 3

# Sitemap-based crawl
ketch crawl https://example.com/sitemap.xml --sitemap

# Run in background with status tracking
ketch crawl https://example.com/sitemap.xml --sitemap --background
ketch crawl status              # list all crawls
ketch crawl status c_a1b2c3d4   # check specific crawl
ketch crawl stop c_a1b2c3d4     # stop a running crawl
```

Crawled pages are cached — re-running the same crawl returns instantly from cache. Use `--no-cache` to force re-fetch.

## Code search

`ketch code` searches real source code across open-source repositories. Two backends:

```sh
# Sourcegraph (default) — zero config, ~1M OSS repos, exact line matches
ketch code "http.NewRequestWithContext" --lang go

# GitHub Code Search — uses your gh CLI token automatically if installed
ketch code "rate limit middleware" --lang go -b github --limit 10
```

Each result shows the matched line, repo, file path, star count, and a permalink. Results are filtered to non-archived, non-fork repos by default (Sourcegraph).

**GitHub auth resolution chain** (for `-b github`): explicit config (`ketch config set github_token <tok>`) → `$GITHUB_TOKEN` → `$GH_TOKEN` → `gh auth token` (if `gh` CLI is installed). Run `ketch config` to see which source is active. Stargazer counts come from a single batched GraphQL call after the REST search.

## Library docs

`ketch docs` fetches curated, version-aware documentation snippets from Context7:

```sh
ketch config set context7_api_key ctx7sk_...

# Auto-resolve library from query
ketch docs "middleware authentication"

# Skip resolve, fetch directly from a known library ID
ketch docs "how to render with word wrap" --library /charmbracelet/glamour

# List matching library IDs without fetching docs
ketch docs --resolve "glamour"
```

## Flags

| Flag | Scope | Default | Description |
|------|-------|---------|-------------|
| `--json` | global | false | JSON output |
| `--backend, -b` | global | brave | Search backend (`brave`, `ddg`, `searxng`) |
| `--limit, -l` | search | 5 | Max results |
| `--scrape` | search | false | Fetch full content from each result |
| `--raw` | scrape | false | Raw HTML instead of markdown |
| `--no-cache` | scrape, crawl | false | Bypass page cache |
| `--depth` | crawl | 3 | Max BFS depth |
| `--concurrency` | crawl | 8 | Worker pool size |
| `--sitemap` | crawl | false | Treat seed URL as sitemap |
| `--background` | crawl | false | Run in background, return crawl ID |
| `--allow` | crawl | — | Path substring filters (any match passes) |
| `--deny` | crawl | — | Regex deny patterns |
| `--backend, -b` | code, docs | cfg value | Code/docs backend |
| `--lang` | code | — | Language qualifier (appended to query) |
| `--library` | docs | — | Context7 library ID, skips resolve |
| `--tokens` | docs | 4000 | Context7 token budget |
| `--resolve` | docs | false | Resolve library name instead of searching |

## Configuration

ketch reads defaults from `~/.config/ketch/config.json`. Flags always override config values.

```sh
# Create a default config file
ketch config init

# Set a default backend
ketch config set backend searxng

# Set your SearXNG URL
ketch config set searxng_url http://my-searxng:8080

# Configure browser for JS-rendered pages
ketch config set browser chrome

# View effective config + available backends
ketch config
```

```json
{
  "config_path": "/home/user/.config/ketch/config.json",
  "backend": "brave",
  "searxng_url": "http://localhost:8081",
  "limit": 5,
  "cache_ttl": "72h",
  "browser": "chrome",
  "available_backends": ["brave", "ddg", "searxng"]
}
```

### Search Backends (ketch search)

| Backend | Setup | Notes |
|---------|-------|-------|
| `brave` (default) | Free API key from brave.com/search/api | Stable JSON API |
| `ddg` | Zero config | Rate-limited by DDG currently |
| `searxng` | Self-hosted instance | Most reliable for heavy use |

### Code Backends (ketch code)

| Backend | Setup | Notes |
|---------|-------|-------|
| `sourcegraph` (default) | Zero config | Grep-style, ~1M OSS repos, exact line matches, SSE stream, archived/fork filters |
| `github` | `gh auth login` _or_ `ketch config set github_token <tok>` _or_ `$GITHUB_TOKEN` | REST `/search/code` + GraphQL stars batch. 30 req/min cap. Token must have `repo` scope. |

```bash
ketch code "http.NewRequestWithContext" --lang go
ketch code "rate limit middleware" --lang go -b github --limit 10
ketch config set sourcegraph_url https://sourcegraph.com  # optional, for self-hosted
ketch config set github_token ghp_xxx                     # explicit token
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

### What's Next

1. Unit tests for extract, search, code, docs, and cache packages
2. `--raw` flag implementation in scrape command
3. Local FTS5 SQLite docs backend (`ketch docs index`) for offline/private docs

## Agent integration

ketch is built to be called by AI agents. The operator configures the backend once; the agent just calls `ketch search` and `ketch scrape` without needing to know the infrastructure details.

Add this to your agent's system prompt (`CLAUDE.md`, `AGENTS.md`, or equivalent):

```markdown
## Web, Code, and Docs Research

Use `ketch` CLI for all external research — web pages, OSS code, library docs.
- Web search: `ketch search "query"` — titles, URLs, snippets
- Web search + full content: `ketch search "query" --scrape`
- Scrape: `ketch scrape <url>` — fetches a URL and returns clean markdown
- Batch scrape: `ketch scrape <url1> <url2> ...` — concurrent fetch
- Crawl: `ketch crawl <url> --sitemap --background` — crawl a site, poll with `ketch crawl status`
- Code search: `ketch code "query" --lang go` — real OSS code with line + repo + stars
- Library docs: `ketch docs "query" --library /org/repo` — version-aware curated snippets
- JS-rendered pages are handled automatically — if a page returns a loading shell, ketch re-fetches it with a headless browser.
- All commands support `--json` for structured output.
- Discovery: `ketch config` — returns effective config and available backends as JSON.
- The operator has already configured the search/code/docs backends and browser. Do not override unless you have a specific reason.
```

### Why this works

An agent calling a web search API typically needs to know which provider to use, manage API keys, and handle provider-specific response formats. ketch collapses that: the operator runs `ketch config set backend searxng` (or `code_backend github`, `docs_backend context7`) once, and every agent invocation uses the right backend automatically. The agent's system prompt doesn't mention backends at all — it just says "use ketch."

`ketch config` returns the full discovery payload as JSON — including which search, code, and docs backends are active and which token source is in effect — so an agent that needs to inspect capabilities can do so in one call without parsing help text.

## License

[MIT](./LICENSE)
