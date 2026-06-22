# Commands

## ketch search

Search the web and return results.

```sh
ketch search <query> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--backend, -b` | `brave` | Search backend: `brave`, `ddg`, `searxng`, `exa` |
| `--limit, -l` | `5` | Max number of results |
| `--scrape` | `false` | Fetch full content from each result |
| `--minimal` | `false` | One result per line, tab-separated |
| `--trim` | `false` | Strip markdown formatting, keep text |
| `--max-chars` | `0` | Truncate markdown to N chars (0 = off) |
| `--searxng-url` | `http://localhost:8081` | SearXNG instance URL |

The global `--json` flag also applies.

**Examples:**

```sh
ketch search "golang error handling"
ketch search "rust async" --limit 10
ketch search "python web scraping" --scrape
ketch search "query" --backend searxng
ketch search "query" --backend exa
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
