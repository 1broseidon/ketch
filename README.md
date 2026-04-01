# ketch

Fast, stateless CLI for web search and scrape. Search the web, fetch pages, extract clean markdown — all from one binary. Designed to be called by AI agents or directly from your terminal.

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
ketch scrape https://example.com --json
```

## Commands

| Command | What it does |
|---------|-------------|
| `search` | Web search via Brave, DuckDuckGo, or SearXNG, optional `--scrape` for full content |
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

## Portable runtime

For no-install packaged workflows, ketch can keep all writable state under an app-local root instead of user profile directories.

Set `KETCH_PORTABLE_ROOT` before launch to relocate config, cache, browser downloads, and crawl status files together:

```powershell
$env:KETCH_PORTABLE_ROOT = "C:\apps\ketch-data"
ketch config --json
```

You can also override each writable location independently:

- `KETCH_CONFIG_DIR`
- `KETCH_CACHE_DIR`
- `KETCH_BROWSER_DIR`
- `KETCH_STATUS_DIR`

`ketch config --json` reports the effective resolved paths.

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
  "portable_root": "/portable/ketch",
  "cache_path": "/portable/ketch/cache/cache.db",
  "crawl_status_dir": "/portable/ketch/cache/crawls",
  "browser_install_dir": "/portable/ketch/cache/browser",
  "backend": "brave",
  "searxng_url": "http://localhost:8081",
  "limit": 5,
  "cache_ttl": "72h",
  "browser": "chrome",
  "available_backends": ["brave", "ddg", "searxng"]
}
```

### Search backends

| Backend | Description | Setup |
|---------|-------------|-------|
| `brave` | Brave Search JSON API (default) | Free API key from [brave.com/search/api](https://brave.com/search/api/) |
| `ddg` | DuckDuckGo HTML scraping (zero config) | None |
| `searxng` | SearXNG JSON API (self-hosted, most reliable) | Run a [SearXNG](https://docs.searxng.org/) instance |

## Agent integration

ketch is built to be called by AI agents. The operator configures the backend once; the agent just calls `ketch search` and `ketch scrape` without needing to know the infrastructure details.

Add this to your agent's system prompt (`CLAUDE.md`, `AGENTS.md`, or equivalent):

```markdown
## Web Search and Scrape

Use `ketch` CLI for web search and page fetching.
- Search: `ketch search "query"` — returns titles, URLs, and snippets
- Search + full content: `ketch search "query" --scrape` — fetches and extracts each result
- Scrape: `ketch scrape <url>` — fetches a URL and returns clean markdown
- Batch scrape: `ketch scrape <url1> <url2> ...` — concurrent fetch
- Crawl: `ketch crawl <url> --sitemap --background` — crawl a site, poll with `ketch crawl status`
- JS-rendered pages are handled automatically — if a page returns a loading shell, ketch re-fetches it with a headless browser.
- All commands support `--json` for structured output.
- Discovery: `ketch config` — returns effective config and available backends as JSON.
- The operator has already configured the search backend and browser. Do not override unless you have a specific reason.
```

### Why this works

An agent calling a web search API typically needs to know which provider to use, manage API keys, and handle provider-specific response formats. ketch collapses that: the operator runs `ketch config set backend searxng` once, and every agent invocation uses the right backend automatically. The agent's system prompt doesn't mention backends at all — it just says "use ketch."

`ketch config` returns the full discovery payload as JSON, so an agent that needs to inspect capabilities can do so in one call without parsing help text.

## License

[MIT](./LICENSE)
