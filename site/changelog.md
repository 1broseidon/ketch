# Changelog

This page mirrors the canonical [`CHANGELOG.md`](https://github.com/1broseidon/ketch/blob/main/CHANGELOG.md) in the repo root. Versions follow [Semantic Versioning](https://semver.org/) and match the published git tags.

## v0.14.0 — 2026-08-07

**Added**

- **Parallel search backend**: keyless current-web search through Parallel's hosted Search MCP endpoint. Wired through `NewFromConfig`, config discovery, CLI/MCP selection, multi/random search, and `ketch doctor` without changing the Brave default or adding authentication configuration.
- **SerpBase search backend**: Google search results via the SerpBase REST API with query-param `api_key` auth (`serpbase_api_key` / `serpbase_api_keys`, `KETCH_SERPBASE_API_KEY`). Keyed only — no keyless mode. Wired through config set/discovery, `NewFromConfig`, multi/random (`--multi=all` includes it when a key is set), MCP, and `ketch doctor`.
- **Tavily search backend**: agent-oriented web search with Bearer auth (`tavily_api_key` / `tavily_api_keys`, `KETCH_TAVILY_API_KEY`). Keyed only. Default `search_depth` is `basic` (1 credit); results fill both `Description` and `Content` from Tavily's extracted text. Wired through config set/discovery, `NewFromConfig`, multi/random, MCP, and `ketch doctor`.
- **Self-hosted Firecrawl** (#31): `firecrawl_url` (default `https://api.firecrawl.dev`) overrides the Firecrawl API base; ketch appends `/v2/search`. Hosted cloud still requires `firecrawl_api_key`; a non-default base allows keyless self-hosted instances. A pasted full endpoint is normalized back to its base.

**Changed**

- Contributor-facing design documentation now lives in `design/`: `DESIGN.md` (mental model, core abstractions, and an explicit Non-Goals & Scope section), `ROADMAP.md` (non-committal directions), and `adr/` (Architecture Decision Records).

**Fixed**

- Parallel search results no longer break the one-result-per-line output contract. Titles and excerpts from extracted page text could carry newlines, so a 3-result `--minimal` query emitted 30 lines and corrupted row parsing. Both fields now collapse whitespace to a single space before bounding; `Content` keeps the complete text.
- `ketch doctor` no longer reports healthy self-hosted search instances as `unreachable`. Self-hosted Firecrawl is now probed for liveness instead of results, and SearXNG keeps its real `format=json` search on a 10s budget.

## v0.13.0 — 2026-07-25

**Added**

- **Environment-variable configuration** (#26): nearly every config key now has a `KETCH_*` env override (mechanical `KETCH_` + upper-snake naming, e.g. `KETCH_BRAVE_API_KEY`, `KETCH_LIMIT`), with precedence CLI flag > env > config file > default. Singular `*_API_KEY` vars accept comma-separated lists that replace the provider's key pool. `KETCH_CONFIG=<path>` selects an alternate config file (read and write); `KETCH_GITHUB_TOKEN` slots above the config file in the token chain. Invalid env values fail loud — listing every bad variable — but only on commands that consume config; `version`, `help`, `completion`, and `config init/set/path` keep working under a broken environment. `ketch config` gains an `env_overrides` provenance section (previous secret values redacted), `config set` never persists env-derived values, and `KETCH_*` secrets are scrubbed from browser and PDF-converter subprocess environments. `url_rewrites`, `spa_markers`, and the plural `*_api_keys` fields remain file-only.
- **Configurable HTTP User-Agent** for scrape fetches: override via `ketch config set user_agent <ua>`, `KETCH_USER_AGENT`, or `--user-agent` on `scrape` / `search` / `crawl` (flag > env > config > default). Empty clears back to the built-in default. `ketch config` always reports the effective UA so agents can diagnose bot-filter 403s without guessing.

**Changed**

- Default scrape `User-Agent` is now an honest `ketch/<version> (+https://github.com/1broseidon/ketch)` instead of `Mozilla/5.0 (compatible; ketch/1.0)`. The old string matched Shield Security and similar fake-crawler rules (e.g. `https://jenson.org/ma/` returned HTTP 403 while bare `curl` succeeded). No browser impersonation — operators who need a custom UA set one explicitly. Release builds embed the semver; local/dirty builds collapse to `ketch/dev`.
- Headless browser fetches no longer inherit Rod's default `LaptopWithMDPIScreen` device emulation (a hardcoded macOS Chrome 114 UA that bot filters also blocklist). Cleared via `devices.Clear`, so the browser presents as the real installed Chrome, including `sec-ch-ua` client hints.

**Fixed**

- Readability no longer silently drops data tables when a smaller table (e.g. a Wikipedia infobox) survives extraction (#28). The raw-table fallback now compares DOM data-table counts between the raw HTML and readability's output — ignoring layout/nav/footer/presentation/hidden tables — and only swaps to the noisier full-page conversion when readability actually lost tables. Relative links on the raw path are absolutized, so recovered tables don't ship bare `/wiki/...` hrefs.
- `ketch browser install` no longer wedges after an interrupted download (#27). Extraction is not atomic, so a partial tree left by a cancelled or failed download broke every subsequent attempt — and broke it *differently* each time, surfacing as Rod's misleading "can't find a browser binary for your OS". The revision directory is now cleared before download, so each install starts from a known state, and a failed download reports the cache path plus the `ketch config set browser <path>` escape hatch instead of a bare Rod error. Affected macOS most visibly, where `Chromium.app` ships symlink-heavy frameworks.
- `--force-browser` no longer aborts when the preliminary HTTP classification probe is blocked (HTTP 403 and similar). The probe exists only to detect PDFs before a forced render; a failed probe now falls through to the browser instead of failing the scrape — which is the point of the flag against bot walls. Without a configured browser the probe error is still returned.

## v0.12.0 — 2026-07-15

**Added**

- **BYO-cookie support** (#25): ketch loads a Netscape `cookies.txt` jar — the format exported by browser cookies.txt extensions and consumed by `curl`/`yt-dlp` — and injects matching cookies on both the HTTP and headless-browser fetch paths, unblocking session- and consent-gated pages. `--cookie-file <path>` on `scrape`, `search` (for `--scrape` fetches), and `crawl`, plus a persistent `cookie_file` config key (the flag overrides config; an explicit empty flag disables cookies for the run). Scope (Domain, HostOnly, Path, Secure) is re-matched on every request and redirect; a configured jar gets an isolated page-cache namespace; cookie values are never printed anywhere, and a group/world-readable jar triggers a `chmod 600` warning.
- **Multiple API keys per provider** (#23): plural config fields (`brave_api_keys`, `exa_api_keys`, `firecrawl_api_keys`, `keenable_api_keys`) alongside the singular keys. A random key is picked per request to spread rate limits, with one retry on 401/429 (402 for Firecrawl) when the pool holds more than one. `config set` accepts JSON arrays, `ketch config` reports `*_api_keys_count` only, and `ketch doctor` probes the effective pools.
- **Random provider selection** (#24): `ketch search --random` (or `--random=brave,exa`) shuffles the candidate backends, tries one, and falls back to the rest on failure — stopping at the first successful response. Mirrors `--multi` flag semantics, mutually exclusive with `--backend`/`--multi`, with MCP parity via the `search` tool's `random` input.
- **PDF text extraction** for `scrape`, `search --scrape`, and `crawl` (#19): text-based PDFs are detected by MIME type or `%PDF-` magic bytes and extracted with a built-in pure-Go parser. An optional external converter (`external_pdf_to_md_converter_command` with exactly one `{input}` placeholder, plus `external_pdf_to_md_converter_timeout_sec`, default 300) is authoritative when configured. `--raw` and `--select` reject PDFs (exit 2); PDFs without a text layer are precondition errors (exit 5) with an OCR hint.
- Bundled skill verb renamed `research.md` → `ketch-research.md` (#22) to avoid namespace conflicts.

## v0.11.0 — 2026-07-07

**Added**

- **Federated multi-backend search**: `ketch search --multi` (and the MCP `search` tool's `multi` input) queries several backends at once and fuses their rankings with [Reciprocal Rank Fusion](https://doi.org/10.1145/1571941.1572114), so a page multiple engines rank highly rises to the top. Bare `--multi` federates every usable backend (zero-config installs still get ddg + exa + keenable); `--multi=brave,exa` picks an explicit set (the `=` is required for a list). Results are deduplicated by URL canonicalization, each backend gets a 10s timeout with graceful degradation (`failed:` frontmatter / additive MCP `errors` map), and every fused result lists the engines that found it.
- `firecrawl` web search backend via the [Firecrawl](https://docs.firecrawl.dev) v2 search API, configured with `ketch config set firecrawl_api_key <key>` and selected with `-b firecrawl`. Reports `firecrawl_api_key_set` in `ketch config` discovery and is covered by a live `ketch doctor` probe.
- `keenable` web search backend over the Keenable index, built for AI agents. Keyless by default (public endpoint, rate-limited); an optional `keenable_api_key` lifts the rate limit.
- `ketch extract` — pipe HTML through ketch's readability + markdown pipeline with no fetch: `curl -L <url> | ketch extract`. Supports `--url` (metadata + relative-link resolution), `--select`, `--trim`, `--max-chars`, and the global `--json`; deliberately no cache, browser, or MCP surface.
- Claude Code plugin + marketplace manifest: `claude plugin marketplace add 1broseidon/ketch`, then `claude plugin install ketch@ketch` wires up `ketch mcp serve` and the bundled agent skill. Optional convenience — the stateless CLI remains the zero-infrastructure path.

## v0.10.0 — 2026-07-01

**Added**

- `ketch mcp serve` — ketch as an MCP server over stdio, exposing `search`, `code`, `docs`, `scrape`, and `crawl` with the same config-driven backends as the CLI, stable `[kind]` error prefixes mirroring the CLI exit codes, and concise server instructions in the initialize result.
- Bundled agent skill at `skills/ketch/` — a SKILL.md playbook any skill-loading agent can install: surface routing, token budgets, error-prefix control flow, a deep-research recipe, and a guided backend-setup flow.
- `ketch doctor` — deterministic live health checks for every surface (search/code/docs backends, browser, cache) with `ok` / `no_key` / `unreachable` / `misconfigured` statuses, fix hints (including the SearXNG `format: json` trap), aligned human output or stable `--json`.
- Key-presence booleans in `ketch config` discovery (`brave_api_key_set`, `exa_api_key_set`, `context7_api_key_set`, `github_token_set`) so agents can tell "unconfigured" from "ready" in one call.
- Shared config-driven constructors (`search/code/docs/scrape/cache.NewFromConfig`) used by both the CLI and MCP server, ending backend-switch drift. MIT LICENSE file.

**Changed**

- `ketch docs --library` on a non-context7 backend and `ketch code --regex` on GitHub are clear validation errors instead of silent re-routes; the unimplemented `local` docs backend is rejected up front and no longer advertised.

**Fixed**

- Context7 404s classify as not-found (exit 3) instead of retryable-upstream; `docs --resolve` respects `--limit`; `ketch cache --json` emits stable JSON; unknown-backend errors list the valid options; a data race in the scraper's browser-binary resolution.

## v0.9.5 — 2026-06-29

**Fixed**

- Tables render as GFM pipe tables across readability, raw, and selector extraction paths (#14).
- Brave searches cap the API `count` at Brave's per-request maximum of 20, preventing HTTP 422 when `--limit` is higher (#17).
- Client-rendered SPA pages (e.g. Next.js App Router) are no longer misdetected as static; adds a `spa_markers` config key to extend detection (#15).

## v0.9.4 — 2026-06-22

**Added**

- `exa` web search backend via Exa's hosted MCP endpoint, with optional `exa_api_key` config for authenticated usage.
- `ketch scrape --force-browser` — always render via the configured browser, skipping JS-shell auto-detection (#12); composes with `--raw` and `--select`. Documents the previously-undocumented `--raw` flag (#11).

## v0.9.3 — 2026-05-29

**Added**

- `grepapp` code search backend (Grep MCP, `mcp.grep.app`) — keyless, literal/regex search across 1M+ public GitHub repos. Now the default for `ketch code` (was `sourcegraph`).
- `ketch code --regex` interprets the query as a regular expression. Supported on `grepapp` and `sourcegraph`; `github` rejects it because REST code search is literal-only.

**Changed**

- `code.Searcher` interface refactored from positional params to a `Query` struct so backend options can grow without signature churn.

**Fixed**

- Documentation drift across README, CLAUDE.md, and the site reference: corrected the `ketch code` default backend, scoped `-b/--backend` to `search`/`code`/`docs`, documented previously-missing flags and the `version` command, and synced the `ketch config` discovery JSON example with real output.

## v0.9.2 — 2026-05-24

**Added**

- Differentiated exit codes so scripts and agents can distinguish failure classes: `2` validation/bad input, `3` not found, `4` upstream/network, `5` precondition (missing API key/token), `6` interrupted.

**Changed**

- `ketch crawl` no longer swallows Ctrl+C as exit 0. SIGINT during a foreground crawl exits `6` while still printing the summary collected before shutdown.
- `-b/--backend` is no longer a persistent root flag — it lives on `search` (matching the existing `code` and `docs` local flags). `ketch -b ddg search "q"` and `ketch search -b ddg "q"` both still work.

## v0.9.1 — 2026-05-22

**Added**

- `url_rewrites` config: an ordered list of `{match, replace}` regex rules applied transparently before any fetch in `scrape`, `search --scrape`, and `crawl`. Redirect URLs without touching the agent surface (e.g. `www.reddit.com` → `old.reddit.com`). The original URL is preserved in output as `url:`; the fetched URL appears as `fetched_url:` when different.

**Changed**

- `crawl.Crawl()` now takes a `*scrape.Scraper` from the caller (`Options.BrowserBin` removed). Affects only direct importers of the `crawl` package — the CLI is unchanged.

**Fixed**

- Broken example URLs in the README (#8, thanks @abhmul).

## v0.9.0 — 2026-05-12

**Changed**

- **Breaking.** Reusable packages moved from `pkg/<pkg>` to the module root. Import paths change from `github.com/1broseidon/ketch/pkg/<pkg>` to `github.com/1broseidon/ketch/<pkg>`.
- VitePress documentation site moved from `docs/` to `site/`, freeing `docs/` for the docs-search Go package. Site URL is unaffected.

## v0.8.1 — 2026-05-12

**Fixed**

- Page cache no longer returns unrendered JS-shell content after a browser is configured. Entries record their fetch source (`http` / `http_shell` / `browser`); JS-shell hits are bypassed once a browser is available, and pre-existing entries migrate in place. Fixes #7.

## v0.8.0 — 2026-05-02

**Changed**

- Reusable packages moved from `internal/` to `pkg/` (`cache`, `code`, `config`, `crawl`, `docs`, `extract`, `httpx`, `scrape`, `search`, `updatecheck`). Pure rename — exposes them for import by external Go programs.

## v0.7.1 — 2026-04-21

**Fixed**

- `ketch docs --resolve <name>` returned HTTP 400 after an upstream Context7 API change. Query param renamed (`?q=` → `?query=`), results moved into a `{"results": [...]}` envelope, and field names updated. `ketch docs <query>` and `--library` were unaffected.

## v0.7.0 — 2026-04-21

**Added**

- `ketch version` command and `--version` flag — reports build version, commit, and date.
- Passive update reminder when a newer release exists (cached 24h, throttled). Honors `KETCH_NO_UPDATE_NOTIFIER=1`, `CI`, `--json`, and non-TTY stderr.
- Ctrl+C (SIGINT) and SIGTERM cancel the root context, so foreground `ketch crawl` drains gracefully.

**Changed**

- HTTP stack tuned for crawling: a shared `*http.Transport` with a 30s timeout, `MaxIdleConnsPerHost=16`, HTTP/2, and a keep-alive dialer, reused by every backend.
- `context.Context` plumbed through the scraper, browser, crawler, and sitemap/llms.txt fetches — cancellation reaches into Rod and `http.Client.Do`.
- All HTTP response bodies capped at 20 MiB via `io.LimitReader`.

## v0.6.0 — 2026-04-11

**Added**

- `ketch scrape` smart input detection: multiple args, JSON array, file (one URL per line), or stdin pipe — auto-detected, no extra flags.
- `--concurrency N` on `ketch scrape` (default 5) — semaphore-based worker pool.
- `--select` and `--no-llms-txt` now propagate to multi-URL scraping.

**Changed**

- `search.Searcher.Search` and `docs.Searcher.Search` now take `context.Context` as the first param, consistent with `code.Searcher`.

## v0.5.0 — 2026-04-11

**Added**

- `ketch scrape --select <css>` — CSS selector extraction, bypasses readability (with browser fallback for JS-rendered pages).
- `ketch scrape --max-chars N` — truncate markdown output to N Unicode code points.
- `ketch scrape --trim` — strip markdown formatting while preserving content text (typically 30–40% token reduction).
- `ketch search/code/docs --minimal` — one result per line, tab-separated, pipe-friendly.
- llms.txt auto-detection: bare domain URLs check `/llms.txt` and return it directly when found. Disable with `--no-llms-txt`.

## v0.4.0 — 2026-04-11

**Added**

- `ketch code -b github` — GitHub Code Search backend. Token resolution: explicit config → `$GITHUB_TOKEN` → `$GH_TOKEN` → `gh auth token`.
- GitHub backend populates star counts via a single batched GraphQL call.
- `github_token_source` field in the `ketch config` discovery payload (the token itself is never printed).

**Changed**

- `code.Searcher.Search` takes `context.Context` as its first arg; backends use `http.NewRequestWithContext` for cancellation.

## v0.3.0 — 2026-04-10

**Added**

- `ketch code` command — code search via the Sourcegraph streaming SSE API. Zero config.
- `ketch docs` command — library documentation search via Context7. Requires an API key.
- Config keys: `code_backend`, `docs_backend`, `context7_api_key`, `sourcegraph_url`.

## v0.2.0

Browser rendering, crawl, and cache overhaul.

- **Browser rendering**: JS-rendered pages (React, Angular, Salesforce Lightning) automatically detected and re-fetched via headless Chrome using Rod.
  - `ketch config set browser chrome` — configure browser
  - `ketch browser install` — download Chromium
  - `ketch browser status` — check browser availability
  - Transparent fallback — agents see the same output format
- **Crawl command**: BFS and sitemap-based site crawling.
  - `ketch crawl <url>` — BFS crawl with configurable depth and concurrency
  - `ketch crawl <url> --sitemap` — sitemap-based crawl
  - `ketch crawl <url> --background` — detached process with status tracking
  - `ketch crawl status` / `ketch crawl stop` — monitor and control background crawls
- **Cache backend**: migrated from filesystem to an embedded bbolt database.
  - Single `cache.db` file; `Store` interface for future backends
  - Default TTL changed to 72h; shared cache between scrape and crawl

## v0.1.0

Initial release.

- Search via Brave, DuckDuckGo, or SearXNG
- Scrape URLs to clean markdown (readability + html-to-markdown)
- Concurrent batch scraping
- YAML frontmatter + markdown output format
- JSON config at `~/.config/ketch/config.json`
- TTL-based page cache with platform-correct paths
- `ketch config` discovery payload for agent introspection
- `--json` flag on all commands
- GoReleaser + Homebrew tap publishing
