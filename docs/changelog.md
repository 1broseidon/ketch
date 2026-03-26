# Changelog

## v0.2.0

Browser rendering, crawl, and cache overhaul.

- **Browser rendering**: JS-rendered pages (React, Angular, Salesforce Lightning) automatically detected and re-fetched via headless Chrome using Rod
  - `ketch config set browser chrome` — configure browser
  - `ketch browser install` — download Chromium
  - `ketch browser status` — check browser availability
  - Detection heuristic: visible text, noscript tags, SPA markers, script-to-text ratio, loading page detection
  - Transparent fallback — agents see the same output format
- **Crawl command**: BFS and sitemap-based site crawling
  - `ketch crawl <url>` — BFS crawl with configurable depth and concurrency
  - `ketch crawl <url> --sitemap` — sitemap-based crawl
  - `ketch crawl <url> --background` — detached process with status tracking
  - `ketch crawl status` / `ketch crawl stop` — monitor and control background crawls
  - Per-host JS shell tracking: auto-switches to browser mode when >80% of pages are JS-rendered
  - Cached re-crawls complete in under a second
- **Cache backend**: migrated from filesystem (one file per page) to embedded bbolt database
  - Single `cache.db` file instead of thousands of hashed files
  - `Store` interface for future backend support (redis, etc.)
  - Default TTL changed from 1h to 72h
  - Shared cache between scrape and crawl commands
- **Config**: added `browser` key for browser configuration

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
