# Backends

ketch has three search surfaces, each with its own backends: web search (`ketch search`), code search (`ketch code`), and library docs (`ketch docs`).

## Web Search Backends

ketch supports ten web-search backends. Set the default with `ketch config set backend <name>`. To query several at once, use `ketch search --multi` (rank-fused federation) or `--random` (one shuffled provider with fallback) — see the [command reference](/reference/commands#ketch-search).

Every keyed backend also accepts a pool of keys (`brave_api_keys`, `exa_api_keys`, `firecrawl_api_keys`, `keenable_api_keys`, `tavily_api_keys`, `serpbase_api_keys`, `youcom_api_keys`); ketch picks one at random per request and retries once with a different key on `401`/`429` (`402` for Firecrawl). See [multiple API keys](/guide/configuration#multiple-api-keys-per-provider).

## Brave (default)

Brave Search offers a free API tier — no scraping, proper JSON API, reliable.

**Setup:**

1. Get a free API key at [brave.com/search/api](https://brave.com/search/api/)
2. Set it: `ketch config set brave_api_key <your-key>`

**Free tier limits:** 2,000 queries/month, 1 query/second.

## DuckDuckGo

Zero-config HTML scraping of DuckDuckGo's search results. No API key needed.

**Setup:** None — works out of the box.

**Limitations:** DuckDuckGo aggressively rate-limits automated requests. You may see `ddg rate limited after retries` errors under heavy use. ketch retries up to 3 times with 500ms backoff.

## SearXNG

Self-hosted metasearch engine with a JSON API. The most reliable option for heavy use.

**Setup:**

1. Run a SearXNG instance (Docker is easiest):

```sh
docker run -d -p 8081:8080 searxng/searxng
```

2. Enable JSON format in SearXNG settings (required for the API).

3. Point ketch to it:

```sh
ketch config set backend searxng
ketch config set searxng_url http://localhost:8081
```

**Recommended for:** operators running agents that search frequently, or anyone who wants full control over their search infrastructure.

## Exa

AI-oriented web search via Exa's hosted MCP endpoint. It works without configuration by default, with an optional Exa API key for authenticated usage.

**Setup:** None for hosted MCP. Optional key:

```sh
ketch config set exa_api_key <your-key>
ketch config set backend exa
```

**Recommended for:** agent workflows that benefit from Exa's clean result snippets and content-oriented search output.

## Firecrawl

Web search via the [Firecrawl](https://firecrawl.dev) v2 [search API](https://docs.firecrawl.dev/api-reference/endpoint/search) — proper JSON API, no scraping. The hosted cloud API requires an API key; self-hosted instances often run without one.

**Setup (hosted):**

1. Get an API key at [firecrawl.dev](https://firecrawl.dev)
2. Set it: `ketch config set firecrawl_api_key <your-key>`
3. Make it the default: `ketch config set backend firecrawl`

**Setup (self-hosted):**

```sh
ketch config set firecrawl_url http://localhost:3002
ketch config set backend firecrawl
# optional if your instance requires auth:
# ketch config set firecrawl_api_key <your-key>
```

`firecrawl_url` is the API base (ketch appends `/v2/search`). Default is `https://api.firecrawl.dev`. Pasting the full endpoint works too — a trailing `/v2/search` is stripped rather than doubled.

**Recommended for:** operators already using Firecrawl for scraping who want a single provider for both search and page extraction. Pair with `--scrape` to fetch full content per result.

## Keenable

Web search built for AI agents, backed by the Keenable index. Keyless by default — it works with no account or key against the public endpoint (rate-limited); an optional API key lifts the hourly cap.

**Setup:** None. Optional key to lift the rate limit:

```sh
ketch config set keenable_api_key <your-key>
ketch config set backend keenable
```

Create a key at [keenable.ai/console](https://keenable.ai/console).

**Recommended for:** agent workflows that want a zero-config, agent-oriented search backend without provisioning a provider key up front.

## Tavily

Agent-oriented web search via the [Tavily](https://tavily.com) Search API. Returns extracted page text (not just SERP snippets), so ketch fills both `description` and `content` on each result. Requires an API key — no keyless mode. Uses `search_depth: basic` by default (1 credit per request).

**Setup:**

1. Get a free API key at [app.tavily.com](https://app.tavily.com) (1,000 credits/month, no credit card)
2. Set it: `ketch config set tavily_api_key <your-key>`
3. Make it the default: `ketch config set backend tavily`

**Recommended for:** agent workflows that want richer extracted content in search results without a separate scrape step.

## Parallel

Current web search through Parallel's hosted Search MCP endpoint. Ketch maps result titles, URLs, and excerpts into its standard search result fields.

**Setup:** None — the default endpoint is keyless. Select it explicitly:

```sh
ketch config set backend parallel
```

**Recommended for:** agent workflows that want a setup-free search backend with excerpt content.

## SerpBase

Google search results through the [SerpBase](https://serpbase.dev) REST API. Ketch maps organic result titles, links, and snippets into its standard search result fields.

**Setup:**

1. Get an API key at [serpbase.dev](https://serpbase.dev)
2. Set it: `ketch config set serpbase_api_key <your-key>`
3. Make it the default: `ketch config set backend serpbase`

**Recommended for:** agent workflows that need keyed Google search results through a structured API.

## You.com

Web search through the [You.com](https://you.com) Web Search API. Ketch maps result titles, URLs, page summaries, and keyword-centered snippets into its standard search result fields. Auth is `X-API-Key` header-only.

**Setup:**

1. Get an API key at [you.com/platform/api-keys](https://you.com/platform/api-keys)
2. Set it: `ketch config set youcom_api_key <your-key>`
3. Make it the default: `ketch config set backend youcom`

**Recommended for:** agent workflows that want cited, snippet-rich results from You.com's index alongside other backends in `--multi` federation.

## Code Search Backends

`ketch code` searches real source code across open-source repositories. Set the default with `ketch config set code_backend <name>`.

### Grep (default)

The Grep MCP server (`mcp.grep.app`) — literal or regex search over 1M+ public GitHub repos. Zero config, no token.

**Setup:** None — works out of the box.

Use `--regex` to interpret the query as a regular expression.

### Sourcegraph

Grep-style search across ~1M OSS repos with exact line matches over an SSE stream. Results are filtered to non-archived, non-fork repos by default. Supports `--regex`.

**Setup:** None for the public instance. For a self-hosted instance: `ketch config set sourcegraph_url <url>`.

### GitHub

GitHub Code Search (REST `/search/code`) with a batched GraphQL call for star counts.

**Setup:** A token is required. Resolution chain: `ketch config set github_token <tok>` → `$GITHUB_TOKEN` → `$GH_TOKEN` → `gh auth token` (if the `gh` CLI is installed).

**Limits:** 30 requests/minute. Token must have `repo` scope.

## Docs Backends

`ketch docs` fetches library documentation. Set the default with `ketch config set docs_backend <name>`.

### Context7 (default)

Curated, version-aware documentation snippets.

**Setup:** Free key: `ketch config set context7_api_key <key>`.

### Local

A planned FTS5 SQLite backend for offline/private docs. Not yet implemented.
