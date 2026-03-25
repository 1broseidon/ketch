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

# JSON output for piping
ketch search "query" --json
ketch scrape https://example.com --json
```

## Commands

| Command | What it does |
|---------|-------------|
| `search` | Web search via DuckDuckGo or SearXNG, optional `--scrape` for full content |
| `scrape` | Fetch URLs and extract clean markdown, concurrent batch support |
| `config` | Show effective configuration and available backends as JSON |

All commands support `--json` for structured output.

## Flags

| Flag | Scope | Default | Description |
|------|-------|---------|-------------|
| `--json` | global | false | JSON output |
| `--backend, -b` | global | ddg | Search backend (`ddg`, `searxng`) |
| `--limit, -l` | search | 5 | Max results |
| `--scrape` | search | false | Fetch full content from each result |
| `--searxng-url` | search | http://localhost:8081 | SearXNG instance URL |
| `--raw` | scrape | false | Raw HTML instead of markdown |

## Configuration

ketch reads defaults from `~/.config/ketch/config.json`. Flags always override config values.

```sh
# Create a default config file
ketch config init

# Set a default backend
ketch config set backend searxng

# Set your SearXNG URL
ketch config set searxng_url http://my-searxng:8080

# View effective config + available backends
ketch config
```

```json
{
  "config_path": "/home/user/.config/ketch/config.json",
  "backend": "searxng",
  "searxng_url": "http://my-searxng:8080",
  "limit": 5,
  "available_backends": ["ddg", "searxng"]
}
```

### Search backends

| Backend | Description | Setup |
|---------|-------------|-------|
| `ddg` | DuckDuckGo HTML scraping (default, zero config) | None |
| `searxng` | SearXNG JSON API (self-hosted, more reliable) | Run a [SearXNG](https://docs.searxng.org/) instance with JSON format enabled |

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
- All commands support `--json` for structured output.
- Discovery: `ketch config` — returns effective config and available backends as JSON.
- The operator has already configured the default search backend. Do not pass `--backend` unless you have a specific reason to override it.
```

### Why this works

An agent calling a web search API typically needs to know which provider to use, manage API keys, and handle provider-specific response formats. ketch collapses that: the operator runs `ketch config set backend searxng` once, and every agent invocation uses the right backend automatically. The agent's system prompt doesn't mention backends at all — it just says "use ketch."

`ketch config` returns the full discovery payload as JSON, so an agent that needs to inspect capabilities can do so in one call without parsing help text.

## License

[MIT](./LICENSE)
