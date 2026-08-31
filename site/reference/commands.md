# Commands

## ketch search

Search the web and return results.

```sh
ketch search <query> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--backend, -b` | `brave` | Search backend: `brave`, `ddg`, `searxng`, `exa`, `firecrawl`, `keenable`, `tavily`, `parallel`, `serpbase`, `youcom` |
| `--multi` | — | Federated search across backends: comma-separated list, or bare/`=all` for every usable backend. Mutually exclusive with `--backend` and `--random`. |
| `--random` | — | Random provider with fallback: comma-separated list, or bare/`=all` for every usable backend. Mutually exclusive with `--backend` and `--multi`. |
| `--limit, -l` | `5` | Max number of results |
| `--scrape` | `false` | Fetch full content from each result |
| `--minimal` | `false` | One result per line, tab-separated |
| `--trim` | `false` | Strip markdown formatting, keep text |
| `--max-chars` | `0` | Truncate markdown to N chars (0 = off) |
| `--searxng-url` | `http://localhost:8081` | SearXNG instance URL |
| `--cookie-file` | config `cookie_file` or off | Netscape `cookies.txt` jar for `--scrape` fetches; an explicit empty value disables configured cookies |

The global `--json` flag also applies.

### Federated search (`--multi`)

`--multi` queries several backends at once and fuses their rankings with
[Reciprocal Rank Fusion](https://doi.org/10.1145/1571941.1572114)
(RRF, k=60): a page several engines rank highly floats to the top, so the
result is better than any single backend, not just longer. Results are
deduplicated by URL canonicalization, and each result gains a `backends` list
naming the engines that returned it.

- Bare `ketch search --multi "query"` (or `--multi=all`) uses every *usable*
  backend — the same key-presence rule ketch uses everywhere: `ddg`, `exa`,
  `keenable`, and `parallel` always; `brave`, `firecrawl`, `tavily`, `serpbase`, and `youcom` only with a key; `searxng`
  always (a dead instance just fails fast and is skipped).
- `ketch search --multi=brave,exa "query"` queries exactly those, in that order.
  An unknown name is a validation error (exit 2); a named-but-unconfigured
  backend is a precondition error (exit 5).
- Because `--multi` takes an optional value, pass a list with the `=` form
  (`--multi=brave,exa`); `--multi brave,exa` is rejected as a validation error
  (exit 2) with a hint to use `--multi=brave,exa`.
- Backends that error or time out (10s each) are dropped and reported on stderr
  as `warn:` lines (and in the plain-text `failed:` frontmatter key); the search
  only fails (exit 4) when every backend fails.

### Random provider (`--random`)

`--random` shuffles the candidate backends, queries **one**, and falls back to
the rest in shuffled order only if it fails — stopping at the first successful
response. Use it to spread load across providers without paying every
provider's rate limit on every query (unlike `--multi`, which queries them all).

- Value semantics mirror `--multi`: bare `--random` (or `--random=all`) draws
  from every usable backend; `--random=brave,exa` restricts the pool (the `=`
  form is required for a list — `--random brave,exa` is rejected with a hint).
- Mutually exclusive with both `--backend` and `--multi` (validation error,
  exit 2).
- Output names the backend that answered; backends that failed before it are
  reported (stderr `warn:` lines / `failed:` frontmatter). The search fails
  (exit 4) only when every candidate fails.
- Also available on the MCP `search` tool as the `random` input.

**Examples:**

```sh
ketch search "golang error handling"
ketch search "rust async" --limit 10
ketch search "python web scraping" --scrape
ketch search "query" --backend searxng
ketch search "query" --backend exa
ketch search "query" --backend firecrawl
ketch search "query" --backend keenable
ketch search "query" --backend tavily
ketch search "query" --backend parallel
ketch search "query" --backend serpbase
ketch search "query" --backend youcom
ketch search "rrf rank fusion" --multi                # every usable backend, rank-fused
ketch search "rrf rank fusion" --multi=brave,ddg,exa  # a specific set
ketch search "query" --random                         # one random usable backend, fallback on failure
ketch search "query" --random=brave,exa               # random pick restricted to these two
ketch search "query" --json
```

## ketch code

Search code across open-source repositories.

```sh
ketch code <query> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--backend, -b` | `grepapp` | Code backend: `grepapp`, `sourcegraph`, `github` |
| `--limit, -l` | `5` | Max number of results |
| `--lang` | — | Language filter (appended to query) |
| `--regex` | `false` | Interpret query as regex (`grepapp`, `sourcegraph`) |
| `--minimal` | `false` | One result per line, tab-separated |

**Examples:**

```sh
ketch code "http.NewRequestWithContext" --lang go
ketch code "NewRequestWith.*Context" --regex
ketch code "rate limit middleware" --lang go -b github --limit 10
```

## ketch docs

Search library documentation.

```sh
ketch docs <query> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--backend, -b` | `context7` | Docs backend: `context7`, `local` (not yet implemented) |
| `--limit, -l` | `5` | Max number of results |
| `--library` | — | Context7 library ID (skip resolve step) |
| `--resolve` | `false` | Resolve library name instead of searching |
| `--tokens` | `4000` | Context7 token budget |
| `--minimal` | `false` | One result per line, tab-separated |

**Examples:**

```sh
ketch docs "how to render with word wrap" --library /charmbracelet/glamour
ketch docs "middleware authentication"
ketch docs --resolve "glamour"
```

## ketch scrape

Fetch URLs and extract clean markdown.

```sh
ketch scrape <url> [urls...] [flags]
```

**Input forms** (auto-detected, no flag needed):

- Single URL: `ketch scrape https://example.com`
- Multiple args: `ketch scrape url1 url2 url3`
- JSON array: `ketch scrape '["url1","url2"]'`
- File (one URL per line): `ketch scrape urls.txt`
- Stdin pipe: `cat urls.txt | ketch scrape`

Explicit args take priority over stdin, so `ketch scrape url < file` uses the URL.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--raw` | `false` | Output raw HTML instead of markdown. Renders via the canonical fetch path (browser only if the page already needed it), is cached lazily, skips `/llms.txt`, and cannot be combined with `--select` or `--trim` |
| `--select` | — | CSS selector to extract (skips readability) |
| `--trim` | `false` | Strip markdown formatting, keep text |
| `--max-chars` | `0` | Truncate markdown to N chars (0 = off) |
| `--concurrency` | `5` | Max concurrent requests (multi-URL) |
| `--no-llms-txt` | `false` | Disable `/llms.txt` detection for bare domains |
| `--force-browser` | `false` | Always render via the configured browser, skipping JS-shell auto-detection. Errors if no browser is configured. Composes with `--raw` (dump rendered HTML) and `--select` (run the selector against the rendered DOM); skips `/llms.txt` |
| `--no-cache` | `false` | Bypass the page cache |
| `--cookie-file` | config `cookie_file` or off | Netscape `cookies.txt` jar; an explicit empty value disables configured cookies |

If a browser is configured and the page is detected as JS-rendered, ketch automatically re-fetches via headless Chrome. Matching cookies apply to the HTTP, `/llms.txt`, and browser paths and are re-scoped on every HTTP redirect.

**Examples:**

```sh
ketch scrape https://go.dev/doc/effective_go
ketch scrape https://example.com https://go.dev
ketch scrape https://example.com --json
ketch scrape https://example.com --no-cache
ketch scrape https://example.com/private --cookie-file ~/cookies.txt
```

Multiple URLs are scraped concurrently.

## ketch extract

Convert piped HTML to clean markdown. Reads raw HTML from stdin and runs
ketch's readability + HTML-to-markdown pipeline — no fetch, no cache, no
browser, no `/llms.txt` probe.

```sh
curl -L https://example.com | ketch extract
cat page.html | ketch extract
```

**Input:** stdin only. Positional args and a non-piped terminal are rejected
with exit `2`; for URLs use `ketch scrape <url>`.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | — | Source URL for metadata and relative-link resolution (never fetched) |
| `--select` | — | CSS selector to extract (skips readability) |
| `--trim` | `false` | Strip markdown formatting, keep content text only |
| `--max-chars` | `0` | Truncate markdown to N chars (0 = off), appends `[truncated]` |

The global `--json` flag also applies. The scrape-only flags (`--raw`,
`--no-cache`, `--concurrency`, `--force-browser`, `--no-llms-txt`) are not
exposed.

**Examples:**

```sh
curl -L https://chain.sh/ketch | ketch extract
curl -L https://example.com | ketch extract --url https://example.com
cat page.html | ketch extract --select article --max-chars 4000
xclip -selection clipboard -o | ketch extract --trim --json
```

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
| `--cookie-file` | config `cookie_file` or off | Netscape `cookies.txt` jar for page, sitemap, and nested sitemap-index fetches; an explicit empty value disables configured cookies |

**Examples:**

```sh
# BFS crawl, depth 2
ketch crawl https://docs.example.com --depth 2

# Authenticated sitemap crawl with high concurrency
ketch crawl https://example.com/sitemap.xml --sitemap --concurrency 20 --cookie-file ~/cookies.txt

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

Re-running a crawl uses cached pages. A configured jar with live cookies uses a jar-specific cache namespace, but authenticated content is still stored locally; the cache directory/database are private (`0700`/`0600` on POSIX). Use `--no-cache` when authenticated content must not be stored.

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

If an install fails partway through, rerun it — the download directory is cleared
on each attempt. On older versions a partial download had to be removed by hand
(`rm -rf ~/.cache/ketch/browser`, or `~/Library/Caches/ketch/browser` on macOS).
If the download is blocked entirely, point ketch at an existing Chrome instead:
`ketch config set browser <path>`.

## ketch config

Show or manage configuration.

```sh
ketch config              # show effective config as JSON
ketch config init         # create default config file
ketch config set <k> <v>  # set a config value
ketch config path         # print config file path
```

Every config key except `url_rewrites`, `spa_markers`, and the plural
`*_api_keys` pools can also be set through `KETCH_*` environment variables;
`ketch config` reports env-sourced values in an `env_overrides` section. See
[Configuration → Environment Variables](/guide/configuration#environment-variables).

## ketch cache

Show or manage the page cache.

```sh
ketch cache               # show cache stats (path, entries, size, TTL)
ketch cache clear         # remove all cached pages
```

## ketch doctor

Run live health checks against every surface: search backends
(brave/ddg/searxng/exa/firecrawl/keenable/tavily/parallel/serpbase/youcom), code backends (grepapp/sourcegraph/github), docs
(context7), the configured browser binary, and the page cache. Probes run
concurrently with a per-check timeout and are read-only (nothing is written
to the cache).

```sh
ketch doctor              # aligned human report, one line per check
ketch doctor --json       # stable schema: [{surface, backend, status, detail, latency_ms}]
```

Self-hosted backends are probed on their own terms: a self-hosted Firecrawl
instance is checked for liveness only — that `/v2/search` answers and whether it
demands a key — because running a real search through it takes seconds, and
SearXNG gets a longer budget for the same reason.

Each check reports `ok`, `no_key`, `unreachable`, `misconfigured` (with a fix
hint — e.g. a SearXNG instance that blocks `format=json` until settings.yml
enables it), or `skipped`. Exit code `0` means every applicable check is ok or
cleanly skipped; exit `5` means a configured surface is broken: the default
backend of a surface, a backend with an API key explicitly set, the configured
browser, or the cache. Optional backends that merely lack a key do not fail
the run.

## ketch mcp

Run ketch as an MCP (Model Context Protocol) server over stdio.

```sh
ketch mcp serve
```

Exposes the five research surfaces — `search`, `code`, `docs`, `scrape`,
`crawl` — as MCP tools, using the same config and backends as the CLI.
Tool errors carry the exit-code taxonomy as stable message prefixes:
`[validation]`, `[not_found]`, `[upstream]`, `[precondition]`, `[cancelled]`.
`extract`, `config`, `cache`, `doctor`, and background crawls stay CLI-only.

To register with Claude Code:

```sh
claude mcp add ketch -- ketch mcp serve
```

## ketch version

Print version, commit, and build date.

```sh
ketch version       # or: ketch --version
```

## Global Flags

`--json` is the only global flag. `-b/--backend` is local to `search`, `code`, and `docs`.

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output as JSON instead of YAML frontmatter + markdown |

## Exit Codes

ketch returns differentiated exit codes so scripts and agents can distinguish
failure classes:

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Unclassified error |
| `2` | Validation / bad input (missing arg, unknown backend, unknown config key, unparseable value) |
| `3` | Not found (missing crawl ID, `--select` with no matches) |
| `4` | Upstream / network failure (scrape, search, code, docs, or crawl fetch) |
| `5` | Precondition (missing API key/token, `config init` when file exists) |
| `6` | Interrupted (SIGINT/SIGTERM during a foreground crawl) |
