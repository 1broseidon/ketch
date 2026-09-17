# Commands

## ketch search

Search the web and return results.

```sh
ketch search <query> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--backend, -b` | `auto` | Search backend: `auto`, `brave`, `ddg`, `searxng`, `exa`, `firecrawl`, `keenable`, `tavily`, `parallel`, `serpbase`, `degoog`, `serply`, `youcom`. `auto` is a fallback chain, not a provider — see [backends](/reference/backends#auto-default) |
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
  `firecrawl`, `keenable`, `parallel`, and `youcom` always; `brave`, `tavily`, `serpbase`, and `serply` only with a key; `searxng`
  always (a dead instance just fails fast and is skipped); `degoog` only when
  `degoog_url` is set.
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
ketch search "query" --backend serply
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
| `--backend, -b` | `context7` | Docs backend: `context7`, `local` |
| `--limit, -l` | `5` | Max number of results. With `--library`, applied only when passed explicitly |
| `--library` | — | Library ID (skip resolve step): a Context7 ID such as `/org/repo`, or a local library name |
| `--resolve` | `false` | Resolve library name instead of searching. With `-b local`, lists stored libraries whose name contains the query |
| `--tokens` | `4000` | Token budget (the only bound on `--library` output unless `--limit` is given). Local treats it as ~4 characters per token over the returned snippets |
| `--minimal` | `false` | One result per line, tab-separated |

**Examples:**

```sh
ketch docs "how to render with word wrap" --library /charmbracelet/glamour
ketch docs "middleware authentication"
ketch docs --resolve "glamour"
ketch docs "container queries" -b local --library tailwind
```

With `-b local`, a bare query run inside a project (a directory tree with
`.ketch/docs.json`) searches only that project's attached libraries; outside a
project it searches every stored library. Results carry `source: local`, the
library `version` if one was recorded, and a URL with a heading anchor.

### ketch docs add

Pull a documentation site into a local library for offline search.

```sh
ketch docs add <name> <url> [flags]
```

`<name>` is a short lower-case slug (`tailwind`, `hono`, `go1.25`). `<url>` may
be the docs landing page, a sitemap, or an `llms.txt` / `llms-full.txt`. ketch
picks the cheapest reliable source itself, in this order:

1. `llms-full.txt` at the site root (one fetch, no crawl);
2. `llms.txt` at the site root, treated as a list of page links;
3. sitemaps named in `robots.txt`, then `/sitemap.xml`;
4. a same-host BFS crawl from `<url>`.

List sources and crawls are scoped to `<url>`'s path prefix (`/docs` for
`https://example.com/docs`), so `https://tailwindcss.com/docs` never pulls the
blog. Re-adding a name replaces the library.

Inside a project (a directory tree holding `.ketch/docs.json`, or the nearest
enclosing git repository), the library is also attached to that project's
manifest, so bare `ketch docs -b local` queries default to it and
`ketch docs sync` can rebuild it on another machine.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--version` | — | Version label stored with the library (informational; shown in results) |
| `--prefix` | `<url>` path | Path prefix pages must be under; `/` for the whole host |
| `--sitemap` | `false` | Treat `<url>` as a sitemap even if it is not named like one |
| `--max-pages` | `500` | Stop after this many pages (`stopped: max_pages` in the summary) |
| `--depth` | `5` | Max BFS depth when the source is a crawl |
| `--concurrency` | `8` | Max concurrent fetches |
| `--dry-run` | `false` | Discover the source and print the plan and page counts; fetch and write nothing |
| `--global` | `false` | Store the library without attaching it to the enclosing project |
| `--no-cache` | `false` | Bypass the page cache |
| `--verbose` | `false` | Print each fetched URL to stderr (failures are always printed) |
| `--cookie-file` | config `cookie_file` or off | Netscape `cookies.txt` jar for the fetches |
| `--user-agent` | config `user_agent` | User-Agent override for the fetches |

**Examples:**

```sh
ketch docs add tailwind https://tailwindcss.com/docs --dry-run   # see the plan first
ketch docs add tailwind https://tailwindcss.com/docs --version 4
ketch docs add hono https://hono.dev/llms-full.txt
ketch docs add vitepress https://vitepress.dev/sitemap.xml --prefix /guide
ketch docs "container queries" -b local --library tailwind
```

The summary reports `source`, `prefix`, `pages`, `sections`, per-URL failures,
`stopped: max_pages` when the cap cut the fetch short, and `unrendered: N`
when pages looked JS-rendered but no browser is configured — their content is
whatever the server-side HTML carried, so configure a browser (`ketch browser
install`) and re-add to fill it in. Exit codes: `2` bad name or URL, `3`
nothing indexable was fetched, `4` the site or network failed.

### ketch docs list

```sh
ketch docs list
```

Lists every stored library (name, version, pages, sections, source, updated)
and marks the ones attached to the current project. Libraries the project
manifest declares but the store lacks are called out with a `ketch docs sync`
hint.

### ketch docs remove

```sh
ketch docs remove <name>
```

Deletes the library from the local store and detaches it from the current
project's manifest if it is listed there. Exit `3` when it is neither stored nor
attached.

### ketch docs sync

```sh
ketch docs sync [--force]
```

Reads the enclosing project's `.ketch/docs.json` and adds every library that is
not yet stored, using the options recorded when it was attached. `--force`
re-adds them all. Exit `5` when there is no manifest.

### Where local docs live

Libraries are stored in a single SQLite file under the docs directory:
`$XDG_DATA_HOME/ketch/docs` (default `~/.local/share/ketch/docs`) on Linux,
`~/Library/Application Support/ketch/docs` on macOS, `%LOCALAPPDATA%\ketch\docs`
on Windows. Override with `ketch config set docs_dir <path>` or
`KETCH_DOCS_DIR`. The project manifest is a small JSON file
(`.ketch/docs.json`) that records only how each library was added — commit it
and teammates can `ketch docs sync`.

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

`config set` validates values before writing: `backend`, `code_backend`, and
`docs_backend` must name a registered provider (the error lists the valid
names), and `mcp_tools`, `limit`, `cache_ttl`, `url_rewrites`, and
`spa_markers` are checked the same way.

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
(brave/ddg/searxng/exa/firecrawl/keenable/tavily/parallel/serpbase/degoog/serply/youcom), code backends (grepapp/sourcegraph/github), docs
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
