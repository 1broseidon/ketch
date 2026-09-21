# ketch

[![GitHub Stars](https://img.shields.io/github/stars/1broseidon/ketch?style=social)](https://github.com/1broseidon/ketch/stargazers)
[![Go Reference](https://pkg.go.dev/badge/github.com/1broseidon/ketch.svg)](https://pkg.go.dev/github.com/1broseidon/ketch)
[![Latest Release](https://img.shields.io/github/v/release/1broseidon/ketch)](https://github.com/1broseidon/ketch/releases/latest)

A stateless CLI for web search, code search, library docs, and scraping — one binary, no daemon, no API server to run.

## Why ketch

Most research tooling for agents means wiring up several provider SDKs, each with its own auth and response shape. ketch collapses that into one binary with three research surfaces:

- `ketch search` — web search, no API key required (Brave, DuckDuckGo, SearXNG, Exa, Firecrawl, Keenable, Tavily, Parallel, SerpBase, Serply, or You.com)
- `ketch code` — grep real OSS source across public repos (Grep, Sourcegraph, or GitHub Code Search)
- `ketch docs` — curated, version-aware library documentation (Context7)

Plus `ketch scrape` and `ketch crawl` to turn HTML pages and text-based PDFs into clean markdown.

It's built for two audiences at once:

- **Humans**, who want a fast terminal tool for the same job `curl | pandoc` or a browser tab would otherwise do.
- **AI agents**, who want structured, predictable output (`--json` everywhere), documented exit codes for control flow, and a single `ketch config` call to discover what backends are active — no environment probing, no per-provider glue code.

An operator configures the backend once (`ketch config set backend searxng`); every agent invocation afterward just calls `ketch search` or `ketch scrape` without knowing or caring which provider is behind it.

## Install

```sh
# macOS / Linux, any of x86_64 or arm64
curl -fsSL https://ketch.run/install | sh

# Homebrew
brew install ketch

# npm
npm install -g ketch-cli

# go install
go install github.com/1broseidon/ketch@latest

# Or download a prebuilt binary (linux/darwin/windows, amd64/arm64)
# from https://github.com/1broseidon/ketch/releases
```

The install script picks the right build for your OS and architecture, verifies it
against the release's `checksums.txt`, and drops the binary in `/usr/local/bin` when
that's writable, otherwise `~/.local/bin`. Read it first if you'd rather not pipe to a
shell: [`install.sh`](https://github.com/1broseidon/ketch/blob/main/install.sh). Pin a
version or change the target directory with
`sh -s -- --version v0.17.0 --bin-dir ~/bin`.

## Quickstart

```sh
$ ketch scrape https://go.dev/doc/effective_go
---
url: https://go.dev/doc/effective_go
title: Effective Go - The Go Programming Language
words: 16582
---
1. [Documentation](https://go.dev/doc/)
2. [Effective Go](https://go.dev/doc/effective_go)
...
## Introduction

Go is an open-source programming language that focuses on simplicity, reliability, and efficiency...
```

Search real OSS code with zero configuration:

```sh
$ ketch code "http.NewRequestWithContext" --lang go --limit 2
---
query: http.NewRequestWithContext
lang: go
backend: grepapp
result_count: 2
---
harness/harness  registry/app/remote/clients/registry/client.go  (line 207)
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildPingURL(c.url), nil)
  https://github.com/harness/harness/blob/main/registry/app/remote/clients/registry/client.go
...
```

Web search works with zero configuration too — the default backend (`auto`)
falls back through the keyless providers, so there is no key to set first:

```sh
ketch search "golang error handling"
ketch search "golang error handling" --scrape   # fetch + extract full content per result
ketch search "golang error handling" --multi    # federate across every usable backend, rank-fused
ketch search "golang error handling" --random  # pick one random backend, fallback to rest on failure
```

`auto` tries providers in a fixed order and returns the first that answers,
reporting which one served in the `backend:` field. It prefers whatever you
have actually configured — your own SearXNG or Degoog instance first, then any
provider you have set a key for — and only then the keyless hosted providers.
So setting a key is still how you get a specific provider and higher limits,
and you do not have to also set `backend` for it to take effect:

```sh
ketch config set brave_api_key <key>   # auto now prefers Brave
ketch search "golang error handling" -b ddg   # or pick a provider explicitly
```

`--multi` queries several backends at once and fuses their rankings with
Reciprocal Rank Fusion (a page several engines rank highly floats to the top),
deduplicating by URL and tagging each result with the engines that returned it.
`--random` shuffles the backend list, tries one, and falls back to the rest —
ideal when you want one provider's results without wasting rate limits on all
of them. Both support bare (all usable backends) or `=brave,exa` explicit lists, and both
are mutually exclusive with `--backend` and each other.
See the [command reference](https://ketch.run/#commands).

Every command takes `--json` for structured output:

```sh
ketch scrape https://example.com --json
# {"url":"https://example.com","title":"Example Domain","markdown":"..."}
```

Pipe any HTML through ketch's extraction pipeline without a fetch:

```sh
curl -L https://chain.sh/ketch | ketch extract
cat page.html | ketch extract --select article --max-chars 4000
```

### Extraction

Extraction reads the page's own structure: the content landmark it declares (`main`, `article`), a document assembled from uniform sections, or the smallest element holding its prose. Site furniture is removed by what it is — navigation, hidden and collapsed controls, link rails, tables of contents — and readability is the fallback for a page that declares no structure. The `extract_mode` config key sets what pruning may drop: `clean` (the default) also drops blocks by name and phrase — related-post rails, comment threads, share bars, "was this helpful?" boxes — for the leanest markdown, and `complete` keeps everything the structure does not condemn, trading a little precision for the last fraction of recall. Pages served in a legacy encoding are decoded before extraction.

### PDF extraction

`ketch scrape` detects PDFs from the response MIME type or `%PDF-` signature and extracts their text with a built-in pure-Go parser:

```sh
ketch scrape https://example.com/report.pdf
```

Scanned/image-only PDFs need OCR and return a precondition error with an OCR-converter hint in the built-in path. Operators can configure an external PDF-to-Markdown converter that writes Markdown to stdout (capped at 10 MiB); its shlex-parsed command must contain exactly one `{input}` placeholder:

```sh
ketch config set external_pdf_to_md_converter_command 'pdftotext "{input}" -'
ketch config set external_pdf_to_md_converter_timeout_sec 300
```

When configured, the external converter is authoritative: failures are returned rather than silently falling back to the built-in parser. PDF binary output is never emitted: `--raw` and `--select` reject PDFs as validation errors. With `--force-browser`, PDF markdown still uses text extraction and never opens Chromium's PDF viewer.

### Tags — bookmarks for agent workflows

Save useful sources while you research a project, then find them again in a
later session. Tags group documentation, code references, search results and
write-ups under the same project or topic. One source can belong to several tags.

```sh
ketch search "guacamole ldap authentication" --scrape --tag remote-access
ketch code "guacamole ldap" --tag remote-access
ketch docs "apache guacamole" --tag remote-access
ketch tag add remote-access https://example.com/read-this-later  # no network

ketch tag show remote-access
ketch tag show remote-access --limit 10 --json
ketch tag show remote-access --limit 0       # all entries
ketch tag remove remote-access https://example.com/read-this-later
```

`--tag` works on `search`, `code`, `docs`, `scrape` and `crawl`, including
`--no-cache`. A bookmark keeps the source URL, a bounded title and description,
and the time it was tagged. Descriptions come from search results, snippets or
fetched content; full bodies belong to the separate page cache.

`tag show` returns the newest **50 entries by default**, ordered by tagging time
and then URL for ties. Use `--limit N` to change that, or `--limit 0` for all.
Text output says `showing N of M` when limited; `--minimal` keeps its TSV output
and puts that notice on stderr. JSON and MCP return `entries` and `cached` totals
for the whole tag, `shown` for the returned pages, and `cache_status`. The MCP
`tag` tool accepts the same `limit` on `operation: show`.

Read the index, choose a source, and `scrape` its URL. `cached: false` usually
means there is no fresh local body, not that the link is broken. If the page
cache is locked or unreadable, bookmarks still work: `cache_status: unavailable`
means the `cached: false` flags are unverified. Fetches can proceed without the
page cache. A bookmark does not guarantee an upstream page still exists.

Bookmarks survive cache expiry and `cache clear`. Clear removes page bodies
and frees their space for reuse; it **does not shrink the database file**.
`tag add` accepts absolute `http(s)` URLs without fetching them (anything else
is bad input, exit `2`), preserves existing metadata on a cold re-add, and fills
missing titles and descriptions on a later fetch.
Removing a bookmark uses the displayed URL, regardless of cookies, User-Agent
or URL rewrite settings. Nothing expires bookmarks; use `tag remove` to tidy up — with URLs, it reports
the ones that were not under the tag as `missing`.

The independent `tags.db` is opened only for short index operations, so an idle
MCP server or a background crawl does not monopolize it. Its default location
uses the operating system's configuration directory:

| Platform | Default tag index |
| --- | --- |
| Linux | `$XDG_CONFIG_HOME/ketch/tags.db`, or `~/.config/ketch/tags.db` |
| macOS | `~/Library/Application Support/ketch/tags.db` |
| Windows | `%AppData%\ketch\tags.db` |

Set **`KETCH_TAGS_PATH`** to override the complete filename. Isolated labs must
set it as well as their page-cache override; `XDG_CACHE_HOME` alone no longer
isolates bookmarks. This replaces the unreleased in-cache tag layout; existing
experimental tag buckets are left untouched and are not automatically imported.

A failed explicit `tag` write returns exit 5 / MCP `[precondition]`. If saving
a bookmark fails during research, the research result is retained: the CLI emits
a stderr warning (`warning.code: tag_write_failed` with `--json`), and MCP adds
`warnings` to the result. Research success alone does not confirm bookmark persistence.

## Commands

| Command | What it does |
|---|---|
| `search` | Web search — Brave, DuckDuckGo, SearXNG, Exa, Firecrawl, Keenable, Tavily, Parallel, SerpBase, Serply, or You.com |
| `code` | Grep real OSS source — Grep (default), Sourcegraph, or GitHub Code Search |
| `docs` | Library/framework docs — Context7 (curated, version-aware snippets) |
| `scrape` | Fetch HTML or PDF URL(s) and extract clean markdown; concurrent batch, JSON array, file, or stdin input |
| `extract` | Convert piped HTML to clean markdown (`curl ... \| ketch extract`) — no fetch, no cache, no browser |
| `crawl` | BFS or sitemap crawl with optional background execution and status tracking |
| `browser` | Manage headless Chrome for JS-rendered pages (`install`, `status`) |
| `config` | Show effective config as JSON, or `init` / `set` / `path` |
| `cache` | Show page-cache stats, or `clear` |
| `tag` | Bookmark research sources and revisit them (`add`, `show`, `list`, `remove`) |
| `doctor` | Live health check of every backend, the browser, the cache and the tag index (informational) — exit `0` healthy, `5` when a configured surface is broken |
| `mcp` | Run ketch as an MCP server over stdio (`mcp serve`) — the research surfaces plus `tag`, as tools |
| `version` | Print version, commit, build date |

Every command supports `-h/--help` for its full flag list; `--json` is the only flag global to every command. Full flag reference lives at [ketch.run](https://ketch.run/).

### Backends

| Surface | Default | Also available | Setup |
|---|---|---|---|
| `search` | `auto` | `brave`, `ddg`, `searxng`, `exa`, `firecrawl`, `keenable`, `tavily`, `parallel`, `serpbase`, `degoog`, `serply`, `youcom` | Nothing — `auto` falls back through the keyless providers (`parallel` → `exa` → `keenable` → `youcom` → `firecrawl` → `ddg`) and needs no key. Brave, Tavily, SerpBase, and Serply need a free key (`ketch config set brave_api_key <key>` / `tavily_api_key` / `serpbase_api_key` / `serply_api_key`) and `auto` prefers them once set; `firecrawl_api_key`, `exa_api_key`, `keenable_api_key`, and `youcom_api_key` are optional and lift the hosted caps. `degoog` needs a self-hosted instance (`ketch config set degoog_url <url>`), `searxng` an instance URL (`ketch config set searxng_url <url>`); both are preferred over hosted APIs once configured |
| `code` | `grepapp` | `sourcegraph`, `github` | Grep and Sourcegraph need nothing; GitHub uses `gh auth login`, `$GITHUB_TOKEN`, or `ketch config set github_token <tok>` |
| `docs` | `context7` | `local` (planned, not yet implemented) | Free key: `ketch config set context7_api_key <key>` |

## Why it works well for agents

- **Stateless, single binary.** No daemon, no server to keep alive — call, get a result, exit.
- **Documented exit codes**, not just stderr text, for scripted control flow: `2` bad input, `3` not found, `4` upstream/network failure, `5` missing precondition (e.g. no API key), `6` cancelled (SIGINT/SIGTERM).
- **Automatic JS-rendering fallback.** `ketch scrape` and `ketch crawl` detect JS-shell pages (React/Vue/Svelte SPAs, streaming hydration frameworks) and transparently re-fetch via headless Chrome when needed — same output shape either way.
- **Smart input detection on `scrape`.** Single URL, multiple positional args, a JSON array, a file of URLs, or stdin — no `--batch` flag required.
- **Page cache.** Fetches are cached (bbolt, default TTL 72h); repeat scrapes and crawls return instantly. `--no-cache` bypasses it.
- **One discovery call.** `ketch config` returns the full effective configuration and active backends as JSON, so an agent can inspect capabilities without parsing `--help` text.

## Configuration

ketch reads defaults from `~/.config/ketch/config.json`. Flags always override config values.

```sh
ketch config init                          # write a default config file
ketch config set backend searxng           # set a default backend
ketch config set searxng_url http://my-searxng:8080
ketch config set browser chrome            # enable browser fallback for JS-rendered pages
ketch config                               # print effective config + available backends
ketch config path                          # print the config file path
```

### Environment variables

Every scalar config key can also be set through the environment as `KETCH_` + the upper-snake key name — handy for containers and CI where writing a config file is awkward:

```sh
KETCH_BRAVE_API_KEY=<key> ketch search "query"     # keys/tokens without touching disk
KETCH_BACKEND=ddg KETCH_LIMIT=10 ketch search "query"
KETCH_CONFIG=/etc/ketch/config.json ketch config    # point at an alternate config file
```

Precedence is **CLI flag > `KETCH_*` env > config file > built-in default**. Notes:

- Per-provider key vars (`KETCH_BRAVE_API_KEY`, `KETCH_EXA_API_KEY`, `KETCH_FIRECRAWL_API_KEY`, `KETCH_KEENABLE_API_KEY`) accept a comma-separated list and replace the provider's whole key pool (there are no `*_API_KEYS` plural vars).
- `KETCH_GITHUB_TOKEN` beats the config file, which beats an ambient `$GITHUB_TOKEN`/`$GH_TOKEN`.
- `url_rewrites` and `spa_markers` are file-only (their JSON/regex values don't survive env quoting).
- `ketch config` (show) reports an `env_overrides` section, so you can always see which effective values came from the environment; `ketch config set` writes only file values and never persists env-derived ones.
- Invalid env values (e.g. `KETCH_LIMIT=abc`) fail loudly on commands that use config, naming the offending variable; `ketch version` and `ketch config set/path` still work.
- Secret `KETCH_*` vars are stripped from the environment of spawned subprocesses (headless browser, external PDF converter).

Other configurable keys include per-backend API keys (`brave_api_key`, `brave_api_keys` for multi-key rotation, `exa_api_key`, `firecrawl_api_key`, `keenable_api_key`, `tavily_api_key`, `serpbase_api_key`, `serply_api_key`, `youcom_api_key`, `context7_api_key`, `github_token`), `firecrawl_url` / `sourcegraph_url` / `degoog_url` (self-hosted overrides), `cache_ttl`, `url_rewrites` (regex rewrite rules applied before fetch), `spa_markers` (extra JS-shell detection tokens), `cookie_file` (see below), `user_agent` (User-Agent override for HTTP and browser fetches; setting one scopes cached pages to it, so entries cached under the default stay valid), `extract_mode` (`clean` by default, or `complete`; see [extraction](#extraction) — a non-default mode scopes cached pages the same way), and the optional external PDF converter command/timeout. Multiple keys per provider are picked randomly per request to spread rate limits. See the [config reference](https://ketch.run/) for the full list.

### Cookies (BYO cookies.txt)

Some pages only render their real content once a session or consent cookie is present — a logged-in dashboard, or a consent-banner wall (e.g. Nvidia NGC docs) that shows only the banner to an anonymous headless browser. ketch can attach your own cookies to every fetch, on both the HTTP and browser paths.

Supply a jar in the **Netscape `cookies.txt` format** — the same format exported by browser "cookies.txt" extensions and consumed by `curl` and `yt-dlp`:

```sh
ketch scrape https://catalog.ngc.nvidia.com/... --cookie-file ~/cookies.txt
ketch search "query" --scrape --cookie-file ~/cookies.txt
ketch crawl https://internal.example.com --cookie-file ~/cookies.txt
ketch config set cookie_file ~/cookies.txt          # persist as a default
```

- The `--cookie-file` flag overrides the `cookie_file` config value; an explicit empty flag (`--cookie-file ""`) disables cookies for that run.
- Only cookies whose Domain, HostOnly, Path, and Secure scope matches the request URL are sent; scope is re-evaluated on every redirect, and expired entries are rechecked before every request.
- Cookie **values are never printed** — not in output frontmatter, `--json`, errors, or `ketch doctor`. `ketch doctor` reports `configured (N cookies, M expired)` and nothing more. Keep the jar at `chmod 600`; ketch warns on stderr if it is group/world-readable.
- A configured jar uses a jar-specific page-cache namespace, including before redirects, so authenticated content cannot collide with anonymous cached copies. Authenticated content is still cached locally: on POSIX, ketch protects the cache directory/database as `0700`/`0600`; use `--no-cache` if it must not be stored.

**Responsibility:** respecting a site's Terms of Service and using only your own session cookies is entirely the operator's responsibility.

## Agent integration

Point an agent's system prompt at ketch instead of teaching it individual search/scrape APIs:

```markdown
Use `ketch` for external research — web pages, OSS code, library docs.
- `ketch search "query"` / `ketch search "query" --scrape` for web results with optional full content (add `--multi` to federate across backends and rank-fuse)
- `ketch scrape <url> [url...]` for clean markdown from one or more URLs
- `ketch extract` for already-fetched/piped HTML (`curl ... | ketch extract`) — no fetch, no cache, no browser
- `ketch code "query" --lang go` for real OSS code with repo/line context
- `ketch docs "query" --library /org/repo` for version-aware library docs
- All commands support `--json`. `ketch config` reports active backends.
```

The operator configures backends once; the agent's prompt never needs to mention which provider is behind `ketch search`.

For a fuller agent playbook — surface routing, token budgets, error-code control flow, a deep-research recipe, and guided backend setup — install the bundled skill from [`skills/ketch/`](./skills/ketch/) (works with any agent that loads `SKILL.md`-style skills, e.g. Claude Code).

### MCP server

For agents that speak MCP instead of shelling out, `ketch mcp serve` runs the same surfaces — `search`, `code`, `docs`, `scrape`, `crawl`, plus `tag` — as MCP tools over stdio, using the same config and backends as the CLI. To register it with Claude Code:

```sh
claude mcp add ketch -- ketch mcp serve

# or, with no install step at all:
claude mcp add ketch -- npx -y ketch-cli mcp serve
```

The npm package carries the binary in a per-platform dependency, so `npx` runs
it without a postinstall download — which keeps the server's cold start quick
when a client relaunches it.

Tool errors carry the exit-code taxonomy as stable message prefixes: `[validation]`, `[not_found]`, `[upstream]`, `[precondition]`, `[cancelled]`.

### Claude Code plugin

The CLI on PATH is all ketch needs — but if you want Claude Code to wire up the MCP server and the [bundled skill](./skills/ketch/) in one step, this repo doubles as a plugin marketplace:

```sh
claude plugin marketplace add 1broseidon/ketch
claude plugin install ketch@ketch
```

The plugin registers `ketch mcp serve` as an MCP server and installs the ketch research skill. It does not bundle the binary: `ketch` >= v0.10.0 must be on PATH (`brew install ketch` or `go install github.com/1broseidon/ketch@latest`).

## Contributing

Bug reports, documentation, and improvements are welcome. See the
[contribution guide](./CONTRIBUTING.md) for pull request guidelines and provider
admission criteria. Please discuss new providers in an issue before implementing
them.

The [extraction benchmark](https://github.com/1broseidon/ketch-bench) measures content
preservation and CLI latency across 500 pinned pages from 70 websites; it lives in its
own repository and builds a sibling ketch checkout. Known extraction misses remain
visible; passing its regression check is not a release approval.

## License

[MIT](./LICENSE)
