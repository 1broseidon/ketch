# Getting Started

## Install

**Homebrew:**

```sh
brew install 1broseidon/tap/ketch
```

**Go:**

```sh
go install github.com/1broseidon/ketch@latest
```

Or grab a binary from the [releases page](https://github.com/1broseidon/ketch/releases).

## Search

```sh
ketch search "golang error handling"
```

Output:

```yaml
---
query: golang error handling
backend: brave
result_count: 5
---
Error handling and Go - The Go Programming Language
  https://go.dev/blog/error-handling-and-go
  The language's design and conventions encourage you to explicitly check...

Best Practices for Error Handling in Go
  https://www.jetbrains.com/guide/go/tutorials/handle_errors_in_go/
  How can a reader see that any of these functions might observe an error?
```

## Scrape

```sh
ketch scrape https://go.dev/blog/error-handling-and-go
```

Output:

```yaml
---
url: https://go.dev/blog/error-handling-and-go
title: Error handling and Go
words: 1693
---
## Introduction

If you have written any Go code you have probably encountered the built-in
`error` type...
```

## Crawl

```sh
# BFS crawl from a seed URL
ketch crawl https://docs.example.com --depth 2

# Sitemap-based crawl
ketch crawl https://example.com/sitemap.xml --sitemap

# Run in background
ketch crawl https://example.com/sitemap.xml --sitemap --background
ketch crawl status              # list all crawls
ketch crawl status c_a1b2c3d4   # check progress
ketch crawl stop c_a1b2c3d4     # stop a running crawl
```

Re-running a crawl uses cached pages and completes instantly. Use `--no-cache` to force re-fetch.

## Search + Scrape

Combine both in one call:

```sh
ketch search "golang testing" --scrape
```

This searches, then scrapes each result — returning per-page frontmatter and full markdown content.

## Browser Rendering

For JS-rendered pages, configure a browser:

```sh
ketch config set browser chrome
```

Once set, ketch automatically detects JS-rendered pages and uses headless Chrome. No changes needed to your scrape or crawl commands.

## JSON Output

All commands support `--json` for structured output:

```sh
ketch search "query" --json
ketch scrape https://example.com --json
ketch crawl status c_a1b2c3d4 --json
```

## Next Steps

- [Configure your backend](/guide/configuration) — set a default search backend and browser
- [Agent integration](/guide/agent-integration) — add ketch to your agent's system prompt
- [Command reference](/reference/commands) — full flag and usage details
