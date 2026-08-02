# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Self-hosted Firecrawl** (#31). `firecrawl_url` (default `https://api.firecrawl.dev`) overrides the Firecrawl API base; ketch appends `/v2/search`. Hosted cloud still requires `firecrawl_api_key`; a non-default base allows keyless self-hosted instances. Wired through config set/discovery, `KETCH_FIRECRAWL_URL`, search `NewFromConfig`, and `ketch doctor`. A pasted full endpoint is normalized back to its base, so `.../v2/search` is neither doubled into `.../v2/search/v2/search` nor mistaken for a self-hosted instance when it points at the hosted API.

## [0.13.0] - 2026-07-25

### Added
- **Environment-variable configuration** (#26). Nearly every config key now has a `KETCH_*` env override (mechanical `KETCH_` + upper-snake naming, e.g. `KETCH_BRAVE_API_KEY`, `KETCH_LIMIT`), with precedence CLI flag > env > config file > default. Singular `*_API_KEY` vars accept comma-separated lists that replace the provider's key pool. `KETCH_CONFIG=<path>` selects an alternate config file (read and write); `KETCH_GITHUB_TOKEN` slots above the config file in the token chain. Invalid env values fail loud — listing every bad variable — but only on commands that consume config; `version`, `help`, `completion`, and `config init/set/path` keep working under a broken environment. `ketch config show` gains an `env_overrides` provenance section (previous secret values redacted), `config set` never persists env-derived values, and `KETCH_*` secrets are scrubbed from browser and PDF-converter subprocess environments. `url_rewrites`, `spa_markers`, and the plural `*_api_keys` fields remain file-only; see `docs/adr/0001-env-var-config.md`.
- **Configurable HTTP User-Agent** for scrape fetches. Override via `ketch config set user_agent <ua>`, `KETCH_USER_AGENT`, or `--user-agent` on `scrape` / `search` / `crawl` (flag > env > config > default). Empty clears back to the built-in default. `ketch config` always reports the effective UA so agents can diagnose bot-filter 403s without guessing.

### Changed
- Default scrape `User-Agent` is now an honest `ketch/<version> (+https://github.com/1broseidon/ketch)` instead of `Mozilla/5.0 (compatible; ketch/1.0)`. The old string matched Shield Security / similar fake-crawler rules (e.g. `https://jenson.org/ma/` returned HTTP 403 while bare `curl` succeeded). No browser impersonation — operators who need a custom UA set one explicitly. Release builds embed the semver; local/dirty builds collapse to `ketch/dev`.
- Headless browser fetches no longer inherit Rod's default `LaptopWithMDPIScreen` device emulation (a hardcoded macOS Chrome 114 UA that bot filters also blocklist). Cleared via `devices.Clear`, so the browser presents as the real installed Chrome, including `sec-ch-ua` client hints.

### Fixed
- Readability no longer silently drops data tables when a smaller table (e.g. a Wikipedia infobox) survives extraction (#28). The raw-table fallback now compares DOM data-table counts between the raw HTML and readability's output — ignoring layout/nav/footer/presentation/hidden tables — and only swaps to the noisier full-page conversion when readability actually lost tables. Relative links on the raw path are absolutized via `converter.WithDomain`, so recovered tables don't ship bare `/wiki/...` hrefs. Verified against `https://en.wikipedia.org/wiki/List_of_FIFA_World_Cup_finals`.
- `ketch browser install` no longer wedges after an interrupted download (#27). Extraction is not atomic, so a partial tree left by a cancelled or failed download broke every subsequent attempt — and broke it *differently* each time (`expected only one dir in ...` when the leftover confused the single-directory check, then `file exists` on the framework symlinks, which Rod reports as the misleading "can't find a browser binary for your OS"). The revision directory is now cleared before download, so each install starts from a known state, and a failed download reports the cache path plus the `ketch config set browser <path>` escape hatch instead of a bare Rod error. Affected macOS most visibly, where `Chromium.app` ships symlink-heavy frameworks.
- `--force-browser` no longer aborts when the preliminary HTTP classification probe is blocked (HTTP 403/etc.). The probe exists only to detect PDFs before a forced render; a failed probe now falls through to the browser instead of failing the scrape — which is the point of the flag against bot walls. Without a configured browser the probe error is still returned.

## [0.12.0] - 2026-07-15

### Added
- **BYO-cookie support** (#25) for session- and consent-gated pages (e.g. Nvidia NGC docs that render as a bare consent banner to an anonymous headless browser). ketch loads a **Netscape `cookies.txt`** jar — the format exported by browser cookies.txt extensions and consumed by `curl`/`yt-dlp` (no bespoke JSON schema) — via the new `cookies/` package, honoring the `#HttpOnly_` line prefix, rejecting malformed scope fields, and rechecking expiry before every request. Two ingestion surfaces feed one jar: a `--cookie-file <path>` flag on `scrape`, `search` (for `--scrape` fetches), and `crawl`, plus a persistent `ketch config set cookie_file <path>` key; the flag overrides config, and an explicit empty flag disables cookies for the run. Matching cookies are injected at **both** fetch layers — the HTTP request (including `/llms.txt`, sitemaps, and nested sitemap indexes) and the Rod browser path (cookies set before navigation, which is what unblocks consent-banner walls). HTTP redirects clear inherited cookies and re-match Domain, HostOnly, Path, and Secure scope for every hop, preventing HTTPS-to-HTTP, subdomain, and path-scope leaks. A short fingerprint of every live cookie in the configured jar is folded into the page-cache key, isolating the namespace even before a redirect reaches cookie scope, so authenticated content never collides with anonymous cached copies. Crawl inherits the same jar and cache-key behavior automatically. Cookie values are never printed anywhere (frontmatter, `--json`, errors, `doctor`): `ketch doctor` reports `cookies/jar: configured (N cookies, M expired)`, and a group/world-readable jar triggers a one-time stderr `chmod 600` warning. The cache directory/database use private `0700`/`0600` modes on POSIX and existing database permissions are tightened on open; authenticated responses remain cacheable under cookie-specific keys, so use `--no-cache` when they must not be stored. Non-goals: no password storage, no reading browser profile cookie DBs, no CDP attach to a running browser, no per-call MCP cookie parameter (config-level `cookie_file` still applies to MCP). Respecting site ToS and using only your own session cookies is the operator's responsibility.
- **Multiple API keys per provider** (#23). Plural config fields (`brave_api_keys`, `exa_api_keys`, `firecrawl_api_keys`, `keenable_api_keys`) alongside the existing singular fields; a random key is picked per request to spread rate limits. One retry on 401/429 (402 for Firecrawl) with a different key when the pool has more than one. Config set accepts JSON arrays; config show reports counts only; doctor probes effective key pools. Config file mode enforced to 0600; no key values in any output.
- **Random provider selection** (#24). `--random=brave,ddg,exa` shuffles providers, tries one, and falls back to the rest on failure — stops on the first successful response. Mirrors `--multi` flag semantics, mutually exclusive with `--backend`/`--multi`. MCP parity via `random` param. Output shows which backend answered and which failed.
- PDF text extraction for `scrape`, `search --scrape`, and `crawl` (#19). Text-based PDFs are detected by MIME type or `%PDF-` magic bytes and extracted by the always-available pure-Go `ledongthuc/pdf` implementation. Operators can instead configure `external_pdf_to_md_converter_command` (validated shlex syntax with exactly one `{input}` placeholder, Markdown on stdout capped at 10 MiB) and `external_pdf_to_md_converter_timeout_sec` (default 300 seconds); the external converter is authoritative and never silently falls back to the built-in parser. Response bodies retain the existing behavior of truncating at 20 MiB. `--raw` and `--select` reject PDFs as validation errors (exit 2 / `[validation]`); normal `--force-browser` PDF scrapes bypass Chromium and extract text directly. PDFs without a text layer are non-retryable precondition errors (exit 5 / `[precondition]`) with an OCR-converter hint. Crawl link discovery and browser rendering now run only for HTML responses.
- Renamed bundled skill verb `research.md` → `ketch-research.md` (#22) to avoid namespace conflicts for users who already have their own `research` skill installed.

### Changed
- The existing `scrape.NewBrowserConn(binPath string)` API remains source-compatible; `scrape.NewBrowserConnWithCookies(binPath string, jar *cookies.Jar)` adds cookie-aware browser construction. The `BrowserConn` interface itself is unchanged. Page-cache keys are now cookie-aware: a configured jar with live cookies gets an isolated namespace, including when only a redirect target matches.

## [0.11.0] - 2026-07-07

### Added
- **Federated multi-backend search** via `ketch search --multi` (and the MCP `search` tool's `multi` input): query several backends at once and fuse their rankings with [Reciprocal Rank Fusion](https://doi.org/10.1145/1571941.1572114) (RRF, k=60) so a page multiple engines rank highly rises to the top — better results, not just more. Bare `--multi` (or `--multi=all`) federates every *usable* backend using the same key-presence rule as the rest of ketch (`ddg`/`exa`/`keenable` always; `brave`/`firecrawl` with a key; `searxng` always, a dead instance just fails fast and is skipped), so a zero-config install still gets a real federation. `--multi=brave,exa` picks an explicit ordered set — an unknown name is a validation error (exit 2 / `[validation]`), a named-but-unconfigured backend a precondition error (exit 5 / `[precondition]`). Mutually exclusive with `--backend` (a set contradiction is rejected loudly, not silently resolved). Because `--multi` takes an optional value, a list needs the `=` form (`--multi=brave,exa`); `--multi brave,exa` is caught and rejected as a validation error with a hint to use `--multi=brave,exa`. Results are deduplicated by a new order-of-operations URL canonicalization (scheme/host lowercasing, `http`→`https` folding, default-port and `www.`/fragment/tracking-param stripping — canonical form is a merge key only, so the emitted URL is always an original backend URL). Each backend is fanned out concurrently with a 10s per-backend timeout; backends that error or time out are dropped and reported (CLI: `warn:` on stderr plus a `failed:` frontmatter key; MCP: an additive `errors` map), and the search only fails (`[upstream]` / exit 4) when every backend fails. Output is additive: `search.Result` gains a `backends` field (omitted for single-backend runs, so existing output is byte-identical), the plain-text frontmatter uses `backends:`/`failed:` with a per-result `found in:` line, and `--minimal` appends a 4th backends column under `--multi`. No result caching in v1 (a `--multi` query costs N live searches, exactly like N single searches today). Also refreshes the MCP `search` tool description, which had drifted to enumerate only four of the six backends.
- `firecrawl` web search backend via the [Firecrawl](https://docs.firecrawl.dev) v2 search API (`POST /v2/search`), configured with `ketch config set firecrawl_api_key <key>` and selected with `ketch config set backend firecrawl` or `ketch search -b firecrawl`. Uses the shared `httpx` client and slots into the existing `search.Searcher` interface / `NewFromConfig` switch like the other backends. `ketch config` discovery reports `firecrawl_api_key_set`, and `ketch doctor` gains a live search-backend probe (`ok` / `no_key` / `misconfigured` / `unreachable`). Same provider that powers Firecrawl scrape/crawl workflows, for operators who want one key for both search and page extraction.
- `keenable` web search backend over the Keenable index, built for AI agents. Keyless by default (public endpoint, rate-limited); an optional `keenable_api_key` lifts the rate limit. Wired into `ketch doctor` as a keyless reachability probe.
- `ketch extract` — a stdin-only Cobra subcommand that runs ketch's readability + HTML-to-markdown pipeline over piped HTML and emits frontmatter + markdown (or JSON via the root `--json`). Reads raw HTML from stdin (`curl -L https://example.com | ketch extract`, `cat page.html | ketch extract --select main`), rejects positional args and non-piped terminals with `ExitValidation`, and supports `--url` (metadata + relative-link resolution only; never printed as `about:blank`), `--select` (CSS selector path, exit 3 on no match, exit 2 on a bad selector), `--trim`, and `--max-chars`. Deliberately CLI-only and fetch-free: no cache, no `/llms.txt` probe, no browser rendering, no concurrency, and none of scrape's `--raw`/`--no-cache`/`--concurrency`/`--force-browser`/`--no-llms-txt` flags. The MCP tool surface is unchanged — `extract` is not exposed as a tool.
- Claude Code plugin + marketplace manifest: the repo now doubles as a Claude Code plugin marketplace (`.claude-plugin/marketplace.json`) hosting one plugin (`plugins/ketch/`) — `claude plugin marketplace add 1broseidon/ketch`, then `claude plugin install ketch@ketch`. The plugin is an optional convenience for Claude Code users, never a prerequisite (the stateless CLI remains the zero-infrastructure path): it wires up `ketch mcp serve` as a stdio MCP server (`.mcp.json`, expects the ketch binary >= v0.10.0 on PATH — the plugin does not vendor it) and ships the bundled agent skill via a symlink to the canonical copy at [`skills/ketch/`](./skills/ketch/), which stays where it is for non-Claude-Code agents.

## [0.10.0] - 2026-07-01

### Added
- Bundled agent skill at [`skills/ketch/`](./skills/ketch/) — a `SKILL.md` playbook (plus verb references) any skill-loading agent can install: surface routing (search vs code vs docs vs scrape vs crawl), token budgets with measured costs, error-prefix/exit-code control flow, a `ketch research` deep-research recipe (bounded fan-out, cited synthesis), and a `ketch setup` guided backend-configuration flow that prefers `ketch doctor --json` and includes the SearXNG `format: json` settings fix. CLI-first by design: the stateless CLI is the default transport; the MCP server is honored when the operator wired it up, never required.
- `ketch mcp serve` — runs ketch as an MCP (Model Context Protocol) server over stdio via `github.com/modelcontextprotocol/go-sdk`, exposing five tools: `search`, `code`, `docs`, `scrape`, and `crawl`. Tool handlers call the same underlying packages through the same config-driven constructors as the CLI and resolve backends/API keys from the same `~/.config/ketch/` config, so an agent talking MCP sees the same configured backends as a human using `ketch` directly.
  - **Tool surface**: `search` (backend/limit/searxng_url, plus `scrape`+`trim`+`max_chars` to inline extracted content per result); `code` (backend/lang/limit/regexp); `docs` (backend/library/tokens/limit/resolve); `scrape` (single `url` or batch `urls` with a bounded worker pool and per-URL error entries, selector/raw/force_browser/trim/max_chars/no_cache/no_llms_txt/concurrency, and the CLI's automatic `/llms.txt` probe for bare domains); `crawl` (synchronous bounded BFS: depth/sitemap/allow/deny/max_chars/no_cache, `max_pages` default 30 hard-capped at 100, 3-minute wall-clock budget, partial results returned with `stopped: "max_pages"|"timeout"`). Detached background crawls, cache admin, and config stay CLI-only.
  - **Lifecycle**: the SDK runs tool calls concurrently, so the headless-browser scraper, the bbolt page-cache handle, and the compiled URL rewriter are constructed once at server start, shared across calls, and closed on shutdown (no per-call Chrome launches or cache-lock contention).
  - **Agent contract**: every tool error carries a stable machine-readable kind prefix mirroring the CLI exit codes — `[validation]` / `[not_found]` / `[upstream]` / `[precondition]` / `[cancelled]` — and all tools declare `readOnlyHint`/`openWorldHint` annotations. Scrape/crawl descriptions note that the server fetches whatever URL it is given (no SSRF filtering).
- Shared config-driven constructors `search.NewFromConfig`, `code.NewFromConfig`, `docs.NewFromConfig`, `scrape.NewFromConfig`, and `cache.NewFromConfig`, plus the cache-aware scrape pipeline as `scrape.Scraper` methods (`CachedScrape`, `ScrapeRaw`, `ScrapeSelector`, `FetchLLMSTxt`, ...) and `extract.PostProcess`/`extract.Truncate`. `cmd/` and `mcp/` both call these; the duplicated backend switches and scrape-pipeline copies (which had already drifted) are gone.
- MIT `LICENSE` file. Resolves pkg.go.dev's "License: None detected" (which had hidden the package docs) and satisfies the awesome-go licensing requirement.
- The MCP server now returns concise `instructions` in the `initialize` result: tool routing (which of the five tools to use when), that backend defaults come from the operator's config, the error-prefix taxonomy with retry semantics, and advice to bound scrapes of unknown pages with `max_chars`/`trim`.
- `ketch doctor` — a deterministic live health check of every surface (like `brew doctor`). Concurrent read-only probes with a 3-second per-probe timeout cover the search backends (brave/ddg/searxng/exa), code backends (grepapp/sourcegraph/github, with the full token-resolution chain and a quota-free authed `/rate_limit` call when a token resolves), docs (context7), the configured browser binary (on disk/PATH), and the page cache (writable, entry count, size, lock state — via the existing read-only stats path; probes never write cache entries). Each check reports `ok` / `no_key` / `unreachable` / `misconfigured` (with a fix hint) / `skipped`; the classic SearXNG trap — stock instances return 403 for `format=json` until settings.yml enables it — is detected as its own `misconfigured` status with the settings.yml hint. Output is an aligned human report or a stable `--json` array of `{surface, backend, status, detail, latency_ms}`. Exit `0` when every applicable check is ok or cleanly skipped; exit `5` (precondition) when a *configured* surface is broken — the default backend of a surface, a backend with an API key explicitly set, the configured browser, or the cache. Optional backends merely lacking a key stay informational. Doctor is CLI-only by design (an operator action, like `config` and `cache`) and is not exposed over MCP.
- Key-presence booleans in the `ketch config` discovery payload: `brave_api_key_set`, `exa_api_key_set`, `context7_api_key_set`, and `github_token_set` report whether each credential is configured without ever printing the value. `github_token_set` follows the same resolution chain as the existing `github_token_source` field (config → `$GITHUB_TOKEN`/`$GH_TOKEN` → gh CLI): it is true iff the source is not `none`. Lets an agent distinguish "backend unconfigured" from "backend ready" in one call instead of firing a request and parsing the `[precondition]` error.

### Changed
- `ketch docs --library` with a non-context7 backend now fails with a clear validation error (exit 2) instead of silently ignoring `--library` and re-routing the query to the selected backend. Same rule on the MCP `docs` tool (`[validation] library requires the context7 backend`).
- `ketch docs -b local` (the unimplemented FTS5 stub) is now rejected up front with "not yet implemented" (exit 5, precondition) instead of failing at search time with exit 4; the MCP `docs` tool no longer advertises `local` in its schema.
- `local` is no longer advertised as a usable docs backend anywhere: `config.AvailableDocBackends` (and thus `ketch config` discovery output, the root summary, and `ketch docs --help`) now lists only `context7`. The name is still recognized and rejected with the precondition error above.
- `ketch code --regex` on a backend without regex support (`github`) is now a validation failure (exit 2, was 5): the request is wrong for that backend and no retry or operator action can make it succeed. Aligns the CLI with the MCP `code` tool's `[validation]` classification.

### Fixed
- A Context7 404 for a nonexistent library ID is now classified as not-found (exit 3 / `[not_found]`) instead of retryable-upstream (exit 4 / `[upstream]`) on both the CLI and MCP surfaces. The 404 is detected once in the docs package (`docs.ErrNotFound` sentinel), not by string matching at the edges.
- `ketch docs --resolve` now respects `--limit` (and the MCP `docs` tool's `limit`): resolve results are bounded in the shared `ResolveLibrary` layer, which previously returned every match regardless of the requested limit.
- `ketch cache --json` now emits a stable JSON object (`path`, `entries`, `size_bytes`, `size`, `ttl`, `locked`) instead of ignoring `--json`; `ketch cache clear --json` emits `{"cleared":true}`. `cache clear` on a locked cache now exits 5 (precondition) instead of 1.
- Unknown-backend errors now list the valid options, sourced from the config package's `Available*Backends` lists so they cannot drift, e.g. `unknown search backend "bogus" (available: brave, ddg, searxng, exa)`.
- Documentation drift: AGENTS.md claimed the `ketch code` default backend is `sourcegraph` (it is `grepapp`), and `search`/`code` help text claimed hardcoded defaults ("Brave (default)") when the effective default is config-driven — both now say so.
- Data race in `scrape.Scraper`: `HasBrowser` (and the browser-warning path) read `browserBin` unsynchronized while `getBrowser` can clear it under the mutex on failed resolution. All reads now take the mutex — relevant for concurrent multi-URL scrapes and concurrent MCP tool calls sharing one scraper.
- README badges: pkg.go.dev (Go Reference) and Go Report Card, plus GitHub stars and latest release.
- CI workflow (`.github/workflows/ci.yml`) running build, lint (golangci-lint), and test on pushes to `main` and pull requests, with a `make build-check` target (`go build ./...`) for the build job.

## [0.9.5] - 2026-06-29

### Fixed
- Tables now render as GFM pipe tables across readability, raw, and selector extraction paths via a shared `base` + `commonmark` + `table` Markdown converter. The table converter promotes header rows, preserves cell newlines, emits empty cells for spans, and skips empty rows; readability extraction now falls back to coarse raw-page conversion when readability drops a table that the raw conversion can render (#14).
- Brave searches now cap the API `count` parameter at Brave's current per-request maximum of 20, preventing HTTP 422 responses when `ketch search --limit` is set higher than Brave accepts (#17). Brave non-200 errors now include the response body so upstream validation failures identify the rejected parameter.
- Client-rendered SPA pages (e.g. Next.js App Router) are no longer misdetected as static. The JS-shell detector now recognizes modern framework and hydration markers (`__next_f`, `id="_r_"`, `data-v-app`, `__sveltekit`, `data-svelte`, `q:container`, `astro-island`) and escalates a content-bearing page to a browser render when a strong client-render marker is present and the inline script payload dwarfs the visible text (>8x), bounding needless browser renders. Adds a `spa_markers` config key to extend detection for additional frameworks without code changes (#15).

## [0.9.4] - 2026-06-22

### Added
- `exa` web search backend via Exa's hosted MCP endpoint, with optional `exa_api_key` config for authenticated usage.
- `ketch scrape --force-browser` — a deterministic escape hatch for JS-rendered pages whose meaningful content (e.g. pricing tables) is injected at runtime but whose static HTML still trips the "static" auto-detection (#12). Always renders via the configured browser, skipping JS-shell detection entirely; errors with `ExitPrecondition` when no browser is configured rather than silently falling back to HTTP. Composes with `--raw` (dump the rendered HTML) and `--select` (run the CSS selector against the rendered DOM), and skips the `/llms.txt` probe. A cache hit is honored only for a prior browser render (`source == browser`); HTTP/shell/markdown-only entries never satisfy a forced request. JS-shell *auto-detection* tuning is tracked separately. Documents the previously-undocumented `--raw` flag alongside it (#11).

## [0.9.3] - 2026-05-29

### Added
- `grepapp` code search backend (Grep MCP, `mcp.grep.app`) — keyless, JSON-RPC over SSE, literal/regex search across 1M+ public GitHub repos. It is now the default for `ketch code` (was `sourcegraph`).
- `ketch code --regex` interprets the query as a regular expression. Supported on `grepapp` (sets `useRegexp` on the MCP `searchGitHub` tool) and `sourcegraph` (appends `patterntype:regexp`); `github` rejects it with a clean `ExitPrecondition` error because REST code search is literal-only.

### Changed
- `code.Searcher` interface refactored from positional params to a `Query` struct so backend options can grow without signature churn.

### Fixed
- Documentation drift across README, CLAUDE.md, and the site reference. Corrected the `ketch code` default backend (`grepapp`, not `sourcegraph`), scoped `-b/--backend` to `search`/`code`/`docs` (it is not a global flag), documented the previously-missing flags (`--minimal`, `--trim`, `--max-chars`, `--select`, `--no-llms-txt`, scrape `--concurrency`, `--regex`, `--searxng-url`) and the `version` command, added code/docs backend sections to the site reference, synced the `ketch config` discovery JSON example with real output, and dropped completed "What's Next" items (`--raw` is implemented; unit tests exist).

## [0.9.2] - 2026-05-24

### Added
- Differentiated exit codes. Scripts and agents can now distinguish failure classes instead of treating every non-zero return as the same: `2` validation/bad input (missing arg, unknown backend, unknown config key, unparseable value), `3` not found (`crawl status <missing-id>`, `crawl stop <missing-id>`, `--select` with no matches), `4` upstream/network (scrape/search/code/docs/crawl fetch failures), `5` precondition (brave/context7 API key missing, github token missing, `config init` when file exists, `crawl stop` on a non-running crawl), `6` cancelled (SIGINT/SIGTERM during any operation, including crawls that previously swallowed cancellation as exit 0). Unwrapped errors continue to exit `1`. Implementation: small `cmd.ExitError` type wrapped via `cmd/exit.go` helpers (`exitErrf`, `exitArgs`); `main.go` maps it to `os.Exit`.

### Changed
- `ketch crawl` no longer swallows Ctrl+C as exit 0. SIGINT during a foreground crawl now exits `6` while still printing the summary of what was collected before shutdown. Background crawls (`crawl --background`) are unaffected — they continue to record "stopped" status.
- `-b/--backend` is no longer a persistent root flag. It now lives on `search` only (matching the existing `code` and `docs` local flags). User impact is negligible because cobra still resolves `-b` against the matching subcommand: `ketch -b ddg search "q"` and `ketch search -b ddg "q"` both continue to work via `search`'s local flag. The pre-cleanup behavior — where `-b` appeared (inert) in the `--help` of `scrape`, `crawl`, `cache`, `browser`, and `config` and rendered a per-machine default reflecting the user's config rather than the source default — is gone. Now: `ketch --help` lists only `--json`; each search-style command (`search`, `code`, `docs`) advertises its own `-b/--backend` with its own backend enum.

## [0.9.1] - 2026-05-22

### Added
- `url_rewrites` config: an ordered list of `{match, replace}` regex rules applied transparently before any fetch in `scrape`, `search --scrape`, and `crawl`. Lets users redirect URLs without touching the agent surface — e.g. `www.reddit.com` → `old.reddit.com` (the verification-wall workaround) or `theguardian.com/uk` → `/uk/rss` (RSS-over-rendered-page). Original URL is preserved in output frontmatter as `url:`; the actually-fetched URL appears as `fetched_url:` when different. JSON output exposes both via `url` and `fetched_url`. The page cache is keyed by the rewritten URL so original/rewritten aliases share one entry. Rules validated at `ketch config set url_rewrites '<json>'` time (JSON parse + regex compile); first-match-wins; capture groups (`$1`, `$2`) supported in `replace`. Closes #9.

### Changed
- `crawl.Crawl()` signature now takes `*scrape.Scraper` from the caller (was constructed internally from `Options.BrowserBin`); `Options.BrowserBin` removed. Only affects direct importers of the `crawl` package — the `ketch crawl` CLI is unchanged. Lets the cmd-layer `newScraper()` helper own scraper construction uniformly across `scrape`, `search`, and `crawl`.

### Fixed
- Broken example URLs in README (#8, thanks @abhmul).

## [0.9.0] - 2026-05-12

### Changed
- **BREAKING.** Reusable packages moved from `pkg/<pkg>` to the module root. Import paths change from `github.com/1broseidon/ketch/pkg/<pkg>` to `github.com/1broseidon/ketch/<pkg>` for `cache`, `code`, `config`, `crawl`, `docs`, `extract`, `httpx`, `scrape`, `search`, and `updatecheck`. The `pkg/` prefix is a community convention (golang-standards/project-layout) that the Go team has explicitly not endorsed; stdlib and most idiomatic libraries expose packages at the module root.
- VitePress documentation site moved from `docs/` to `site/` to free the `docs/` path for the docs-search Go package (context7 / FTS5 backends). The Deploy Docs workflow and `.gitignore` are updated. Site URL is unaffected (gh-pages serves from a separate branch).

## [0.8.1] - 2026-05-12

### Fixed
- Page cache no longer returns unrendered JS-shell garbage after the user configures a browser. Entries now record the fetch source (`http` / `http_shell` / `browser`); a cache hit is bypassed when the entry was an unrendered JS-shell extraction and a browser is now available. Plain HTTP entries are not churned by browser config changes. Pre-existing entries (no source recorded) are invalidated once when a browser is configured, migrating them in place. Fixes #7.

## [0.8.0] - 2026-05-02

### Changed
- Reusable packages moved from `internal/` to `pkg/`. Affected: `cache`, `code`, `config`, `crawl`, `docs`, `extract`, `httpx`, `scrape`, `search`, `updatecheck`. Pure rename, no behavior changes — exposes these packages for import by external Go programs. The `internal/` directory is removed; any out-of-tree code that imported `github.com/1broseidon/ketch/internal/<pkg>` (Go's visibility rules already prohibited this from outside the module) must switch to `github.com/1broseidon/ketch/pkg/<pkg>`.

## [0.7.1] - 2026-04-21

### Fixed
- `ketch docs --resolve <name>` was returning HTTP 400 "Query is required" after an upstream context7 API change. The query parameter was renamed (`?q=` → `?query=`), results moved into a `{"results": [...]}` envelope, and field names changed (`name`→`title`, `codeSnippets`→`totalSnippets`, `trust` string → `trustScore` float). `LibraryMatch` and the CLI print now track the current schema. `ketch docs <query>` and `--library` were unaffected.

## [0.7.0] - 2026-04-21

### Added
- `ketch version` command and `--version` flag. Reports build version, commit, and date injected by goreleaser; falls back to `debug.ReadBuildInfo()` for `go install` builds.
- Passive update reminder: when a newer release exists, a two-line hint is printed to stderr after command output. Cached for 24h; throttled so the same version is only announced once per 24h. Honors `KETCH_NO_UPDATE_NOTIFIER=1`, `CI`, `--json`, and non-TTY stderr. Install-type detection selects the right upgrade command (homebrew / `go install` / release URL).
- Ctrl+C (SIGINT) and SIGTERM now cancel the root context, so foreground `ketch crawl` drains gracefully: workers stop, in-flight HTTP aborts, summary prints, exit 0. Previously the default signal handler hard-killed the process.

### Changed
- HTTP stack tuned for crawling: shared `*http.Transport` with a 30s request Timeout, `MaxIdleConnsPerHost=16`, HTTP/2, and a keep-alive dialer (new `internal/httpx`). Every backend (brave, ddg, searxng, context7, sourcegraph, github, scraper) reuses it. Measured: 50 requests to one host in ~385ms; 20 mixed-host URLs at c=10 in ~300ms (down from ~9.7s at c=1).
- `context.Context` is now plumbed through `Scraper.Scrape/Fetch/ScrapeConditional/BrowserScrape/MaybeBrowserFetch`, `BrowserConn.Fetch` (via rod `Page.Context(ctx)`), `crawl.Crawl`, `fetchSitemap`, and `fetchLLMSTxt`. Cancellation reaches all the way into rod and `http.Client.Do`.
- `crawl.Options.StopCh` removed — cancellation is via the ctx passed to `crawl.Crawl`. `ketch crawl stop <id>` sends SIGTERM, which cancels the worker ctx and aborts in-flight requests mid-fetch.
- `DetectJSShell` rewritten: single DOM traversal for the static-page fast path, lazy corroborator phase. `DetectJSShellFromDoc` accepts a pre-parsed document so callers don't pay twice. `ScrapeConditional` parses HTML once and exposes the `*goquery.Document` via `FetchResult.Doc`; the crawler reuses it for link extraction instead of re-parsing.
- Crawl scheduler replaced: `sync.Cond` + growing slice → `chan queueItem` + `sync.WaitGroup` with goroutine-per-enqueue. The old pop pattern (`queue = queue[1:]`) never reclaimed the backing array.
- All HTTP response bodies are capped at 20 MiB via `io.LimitReader` so a misbehaving server cannot OOM the process.

## [0.6.0] - 2026-04-11

### Added
- `ketch scrape` smart input detection: multiple positional args, JSON array (`'["url1","url2"]'`), file (one URL per line), or stdin pipe — input mode is auto-detected, no extra flags needed.
- `--concurrency N` flag on `ketch scrape` (default 5) — replaces unbounded goroutine-per-URL with a semaphore-based worker pool.
- `--select` and `--no-llms-txt` flags now propagate to multi-URL scraping (previously only worked for single URL).
- Pipe chain support: `ketch search "query" --json | jq -r '.[].url' | ketch scrape --trim --max-chars 2000`.

### Fixed
- `resolveURLs` now checks explicit args before stdin — `ketch scrape url < file` uses the URL, not the pipe.
- `scrapeWithSelector` deduped: delegates to `scrapeURLWithSelector` instead of duplicating the fetch/browser-fallback/selector logic.
- `search_feature_test.go` updated for `search.Searcher.Search(ctx, ...)` interface change.

### Changed
- `search.Searcher.Search` and `docs.Searcher.Search` now take `context.Context` as first param, consistent with `code.Searcher`. All HTTP backends use `http.NewRequestWithContext` for proper cancellation propagation.

## [0.5.0] - 2026-04-11

### Added
- `ketch scrape --select <css>` — CSS selector extraction, bypasses readability and runs directly against fetched HTML (with browser fallback for JS-rendered pages).
- `ketch scrape --max-chars N` — truncate markdown output to N Unicode code points, appends `[truncated]` marker.
- `ketch scrape --trim` — strip markdown formatting syntax (bold, italic, links, headings, inline code) while preserving content text. Fenced code blocks are preserved. Typically 30-40% token reduction.
- `ketch search/code/docs --minimal` — one result per line, tab-separated (`url\ttitle\tsnippet`), no frontmatter. Pipe-friendly.
- llms.txt auto-detection: bare domain URLs (e.g. `ketch scrape https://example.com`) automatically check `/llms.txt` and return it directly if found (`Content-Type: text/plain`). Disable with `--no-llms-txt`.
- `internal/extract.Title(html)` exported for use across packages.
- Running `ketch` with no args now shows a compact, generated summary derived from the live command tree and `config.Available*Backends()` — always current, never drifts.

### Fixed
- `StripMarkdown`: fenced code blocks (` ``` ``` `) now protected via sentinel tokens so inline backtick stripping can't corrupt their content.
- `StripMarkdown`: italic regex tightened to require non-space after opening `*`, preventing unordered-list markers (`* item`) from being misread as italic delimiters.
- `truncateContent`: slices by Unicode rune instead of byte, preventing split of multibyte UTF-8 characters at the truncation point.
- `scrapeWithSelector` now calls `MaybeBrowserFetch` after raw fetch so CSS selectors run against rendered content, not JS shell HTML.
- Duplicate `extractTitleFromHTML` in `cmd/scrape` removed; both callers now use `extract.Title`.

### Changed
- `Scraper.maybeBrowserFetch` exported as `MaybeBrowserFetch` for use by the command layer.

## [0.4.0] - 2026-04-11

### Added
- `ketch code -b github` — GitHub Code Search backend. Token resolution chain: explicit config (`ketch config set github_token`) → `$GITHUB_TOKEN` → `$GH_TOKEN` → `gh auth token` (piggybacks on existing gh CLI login). Uses `text-match` media type for accurate line-level snippets via match indices.
- GitHub backend populates `stargazer_count` via a single batched GraphQL `nodes(ids:)` call (REST `/search/code` does not return stars). Non-fatal on failure.
- Rate-limit-aware error messages using `X-RateLimit-Reset`.
- `github_token_source` field in `ketch config` discovery payload (shows which resolution source is active; token itself is never printed).

### Changed
- `code.Searcher.Search` now takes `context.Context` as its first arg; both Sourcegraph and GitHub backends use `http.NewRequestWithContext` so cobra command cancellation propagates to in-flight requests.
- `config.ResolveGithubToken` wraps the `gh auth token` subprocess in `exec.CommandContext` with a 2s deadline so a hung `gh` can't block ketch startup.
- `Searcher.Search` interface now owns its own query dialect (per-backend `buildQuery`); callers pass plain user input and language separately. Sourcegraph applies `archived:no`/`fork:no` defaults; GitHub applies `language:` (archived/fork qualifiers are not valid on the code search endpoint).
- `Result` struct gains `Stars` field, populated by both backends.
- README documents both code backends, the GitHub auth chain, and dedicated sections for `ketch code` and `ketch docs`. AGENTS.md lists `internal/code/github.go`.

## [0.3.0] - 2026-04-10

### Added
- `ketch code` command — code search via Sourcegraph streaming SSE API with `--lang`, `--limit`, `--backend`, `--json` flags. Zero config.
- `ketch docs` command — library documentation search via Context7 with `--library`, `--resolve`, `--tokens`, `--limit`, `--backend`, `--json` flags. Requires API key.
- Config keys: `code_backend`, `docs_backend`, `context7_api_key`, `sourcegraph_url`.

### Changed
- Documentation updates (README, AGENTS.md, CLAUDE.md) for browser rendering and the new code/docs backends.
