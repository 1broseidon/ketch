# Commands

## ketch search

Search the web and return results.

```sh
ketch search <query> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--limit, -l` | `5` | Max number of results |
| `--scrape` | `false` | Fetch full content from each result |
| `--searxng-url` | `http://localhost:8081` | SearXNG instance URL |

**Global flags** (`--json`, `--backend`) also apply.

**Examples:**

```sh
ketch search "golang error handling"
ketch search "rust async" --limit 10
ketch search "python web scraping" --scrape
ketch search "query" --backend searxng
ketch search "query" --json
```

## ketch scrape

Fetch URLs and extract clean markdown.

```sh
ketch scrape <url> [urls...] [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--raw` | `false` | Output raw HTML instead of markdown |
| `--no-cache` | `false` | Bypass the page cache |

If a browser is configured and the page is detected as JS-rendered, ketch automatically re-fetches via headless Chrome.

**Examples:**

```sh
ketch scrape https://go.dev/doc/effective_go
ketch scrape https://example.com https://go.dev
ketch scrape https://example.com --json
ketch scrape https://example.com --no-cache
```

Multiple URLs are scraped concurrently.

## ketch crawl

Crawl a site via BFS link discovery or sitemap.

```sh
ketch crawl <url> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--depth` | `3` | Max BFS depth |
| `--concurrency` | `8` | Worker pool size |
| `--sitemap` | `false` | Treat seed URL as sitemap |
| `--background` | `false` | Run in background, return crawl ID |
| `--no-cache` | `false` | Bypass the page cache |
| `--allow` | — | Path substring filters (any match passes) |
| `--deny` | — | Regex deny patterns |

**Examples:**

```sh
# BFS crawl, depth 2
ketch crawl https://docs.example.com --depth 2

# Sitemap crawl with high concurrency
ketch crawl https://example.com/sitemap.xml --sitemap --concurrency 20

# Background crawl
ketch crawl https://example.com/sitemap.xml --sitemap --background

# Filter to specific paths
ketch crawl https://docs.example.com --allow /guide/ --deny "\\?page="
```

**Subcommands:**

```sh
ketch crawl status              # list all background crawls
ketch crawl status <id>         # show progress for a specific crawl
ketch crawl stop <id>           # stop a running background crawl
```

Re-running a crawl uses cached pages. Use `--no-cache` to force re-fetch.

## ketch browser

Manage headless Chrome for JS-rendered pages.

```sh
ketch browser install           # download Chromium to cache dir
ketch browser status            # check browser config and availability
```

**Examples:**

```sh
# Configure browser
ketch config set browser chrome

# Check it works
ketch browser status
# → browser_config: chrome
# → browser_path: /usr/bin/google-chrome-stable
# → status: ok

# Or download Chromium
ketch browser install
# → Installed to: /home/user/.cache/ketch/browser/...
```

## ketch config

Show or manage configuration.

```sh
ketch config              # show effective config as JSON
ketch config init         # create default config file
ketch config set <k> <v>  # set a config value
ketch config path         # print config file path
```

## ketch cache

Show or manage the page cache.

```sh
ketch cache               # show cache stats (path, entries, size, TTL)
ketch cache clear         # remove all cached pages
```

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output as JSON instead of YAML frontmatter + markdown |
| `--backend, -b` | `brave` | Search backend: `brave`, `ddg`, `searxng` |
