# Configuration

ketch reads defaults from `~/.config/ketch/config.json`. Flags always override config values.

## Setup

```sh
# Create a default config file
ketch config init

# View effective config + available backends
ketch config
```

The discovery payload:

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

## Setting Values

```sh
ketch config set backend searxng
ketch config set brave_api_key BSA...
ketch config set searxng_url http://my-searxng:8080
ketch config set limit 10
ketch config set cache_ttl 4h
ketch config set browser chrome
```

## Config Keys

| Key | Default | Description |
|-----|---------|-------------|
| `backend` | `brave` | Default search backend |
| `brave_api_key` | — | Brave Search API key ([get one free](https://brave.com/search/api/)) |
| `searxng_url` | `http://localhost:8081` | SearXNG instance URL |
| `limit` | `5` | Default max search results |
| `cache_ttl` | `72h` | How long scraped pages stay cached |
| `browser` | — | Browser for JS-rendered pages: `chrome`, `chromium`, or absolute path |

## Browser Rendering

JS-rendered pages are automatically detected and re-fetched via headless Chrome. Configure once, then scrape and crawl commands use it transparently.

```sh
# Use Chrome from PATH
ketch config set browser chrome

# Or use an absolute path
ketch config set browser /usr/bin/google-chrome-stable

# Download Chromium to ketch's cache dir
ketch browser install

# Check browser config and availability
ketch browser status
```

When a browser is configured, ketch automatically detects JS-rendered pages (React SPAs, Angular apps, Salesforce Lightning, etc.) and falls back to headless rendering. Static pages are always fetched via plain HTTP for speed.

## Page Cache

Scraped and crawled pages are cached in a single bbolt database at the platform cache directory:

| OS | Path |
|----|------|
| Linux | `~/.cache/ketch/cache.db` |
| macOS | `~/Library/Caches/ketch/cache.db` |
| Windows | `%LocalAppData%/ketch/cache.db` |

```sh
# View cache stats
ketch cache

# Clear all cached pages
ketch cache clear

# Bypass cache for a single scrape
ketch scrape https://example.com --no-cache

# Bypass cache for a crawl (force re-fetch everything)
ketch crawl https://example.com --no-cache
```
