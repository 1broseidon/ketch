# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
