# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
