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
  "cache_ttl": "1h",
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
```

## Config Keys

| Key | Default | Description |
|-----|---------|-------------|
| `backend` | `brave` | Default search backend |
| `brave_api_key` | — | Brave Search API key ([get one free](https://brave.com/search/api/)) |
| `searxng_url` | `http://localhost:8081` | SearXNG instance URL |
| `limit` | `5` | Default max search results |
| `cache_ttl` | `1h` | How long scraped pages stay cached |

## Page Cache

Scraped pages are cached locally to avoid redundant fetches. The cache uses platform-appropriate directories:

| OS | Path |
|----|------|
| Linux | `~/.cache/ketch/pages/` |
| macOS | `~/Library/Caches/ketch/pages/` |
| Windows | `%LocalAppData%/ketch/pages/` |

```sh
# View cache stats
ketch cache

# Clear all cached pages
ketch cache clear

# Bypass cache for a single scrape
ketch scrape https://example.com --no-cache
```
