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

## Search + Scrape

Combine both in one call:

```sh
ketch search "golang testing" --scrape
```

This searches, then scrapes each result — returning per-page frontmatter and full markdown content.

## JSON Output

All commands support `--json` for structured output:

```sh
ketch search "query" --json
ketch scrape https://example.com --json
```

## Next Steps

- [Configure your backend](/guide/configuration) — set a default search backend
- [Agent integration](/guide/agent-integration) — add ketch to your agent's system prompt
- [Command reference](/reference/commands) — full flag and usage details
