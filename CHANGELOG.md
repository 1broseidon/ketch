# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
