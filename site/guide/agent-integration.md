# Agent Integration

ketch is built to be called by AI agents. The operator configures the backend once; the agent calls `ketch search` and `ketch scrape` without needing to know the infrastructure details.

## System Prompt Snippet

Add this to your agent's system prompt (`CLAUDE.md`, `AGENTS.md`, or equivalent):

```markdown
## Web Search and Scrape

Use `ketch` CLI for web search and page fetching.
- Search: `ketch search "query"` — returns titles, URLs, and snippets
- Search + full content: `ketch search "query" --scrape` — fetches and extracts each result
- Federated search: `ketch search "query" --multi` — queries every usable backend and rank-fuses the results (`--random` picks one at random with fallback instead)
- Scrape: `ketch scrape <url>` — fetches a URL and returns clean markdown
- Batch scrape: `ketch scrape <url1> <url2> ...` — concurrent fetch
- Extract: `curl -L <url> | ketch extract` — pipe already-fetched HTML to clean markdown (no fetch)
- Code search: `ketch code "query" --lang go` — real OSS code snippets
- Library docs: `ketch docs "query" --library /org/repo` — library documentation
- Crawl: `ketch crawl <url> --sitemap --background` — crawl a site, poll with `ketch crawl status`
- JS-rendered pages are handled automatically — if a page returns a loading shell, ketch re-fetches it with a headless browser.
- All commands support `--json` for structured output.
- Discovery: `ketch config` — returns effective config plus available search, code, and docs backends as JSON.
- The operator has already configured the backends and browser. Do not override unless you have a specific reason.
```

## Why This Works

An agent calling a web search API typically needs to know which provider to use, manage API keys, and handle provider-specific response formats. ketch collapses that:

1. The operator runs `ketch config set backend searxng` once
2. Every agent invocation uses the right backend automatically
3. The agent's system prompt doesn't mention backends at all

The same applies to browser rendering — the operator runs `ketch config set browser chrome` once, and JS-rendered pages are handled transparently.

## Output Format

ketch uses YAML frontmatter + markdown body — the same format as [cymbal](https://chain.sh/cymbal/). This gives agents scannable metadata (URL, title, word count) before the full content:

```yaml
---
url: https://go.dev/blog/error-handling-and-go
title: Error handling and Go
words: 1693
---
## Introduction

If you have written any Go code...
```

An agent can read the frontmatter to decide whether to consume the full body, or skip to the next result.

## Crawl Workflow

For large sites, use background crawl + status polling:

```sh
# Agent starts a crawl
ketch crawl https://help.example.com/sitemap --sitemap --background
# → crawl_id: c_a1b2c3d4

# Agent polls for completion
ketch crawl status c_a1b2c3d4 --json
# → {"status": "running", "pages": 847, ...}

# Once complete, scrape individual pages from cache (instant)
ketch scrape https://help.example.com/s/article/1234
```

## Discovery

`ketch config` returns the full discovery payload as JSON, so an agent that needs to inspect capabilities can do so in one call:

```json
{
  "config_path": "/home/user/.config/ketch/config.json",
  "backend": "searxng",
  "searxng_url": "http://localhost:8081",
  "brave_api_key_set": false,
  "brave_api_keys_count": 0,
  "exa_api_key_set": false,
  "exa_api_keys_count": 0,
  "firecrawl_api_key_set": false,
  "firecrawl_api_keys_count": 0,
  "firecrawl_url": "https://api.firecrawl.dev",
  "keenable_api_key_set": false,
  "keenable_api_keys_count": 0,
  "tavily_api_key_set": false,
  "tavily_api_keys_count": 0,
  "limit": 5,
  "cache_ttl": "72h",
  "browser": "chrome",
  "code_backend": "grepapp",
  "docs_backend": "context7",
  "context7_api_key_set": true,
  "sourcegraph_url": "https://sourcegraph.com",
  "github_token_source": "none",
  "github_token_set": false,
  "available_backends": ["brave", "ddg", "searxng", "exa", "firecrawl", "keenable", "tavily", "parallel", "serpbase"],
  "available_code_backends": ["grepapp", "sourcegraph", "github"],
  "available_doc_backends": ["context7"]
}
```

The `*_set` booleans tell an agent whether a keyed backend is ready without
firing a call (key values are never printed); `*_keys_count` reports the size
of each provider's key pool. When `KETCH_*` environment variables override
config values, an additional `env_overrides` array reports which effective
values came from the environment. For a live health check of every
backend — reachability, key validity, browser, cache — run `ketch doctor
--json` (CLI-only, not an MCP tool).

## MCP Server

For agents that speak MCP instead of shelling out, `ketch mcp serve` exposes
the five research surfaces — `search`, `code`, `docs`, `scrape`, `crawl` — as
MCP tools over stdio, using the same config and backends as the CLI. See the
[command reference](/reference/commands#ketch-mcp). To register with Claude
Code:

```sh
claude mcp add ketch -- ketch mcp serve
```
