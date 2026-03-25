# Search Backends

ketch supports three search backends. Set the default with `ketch config set backend <name>`.

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
