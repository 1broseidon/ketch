---
id: task-7
title: "Scrape: escalate to the configured browser on HTTP 403 (#81)"
column: review
position: 0
description: |-
  Design settled by the owner on #81 (2026-10-16). Build it.

  ## What
  When the plain HTTP fetch answers **403** and a browser is configured (`s.HasBrowser()`), retry the page through the browser instead of failing. Bot walls (Cloudflare, Akamai, JA3 checks) commonly 403 Go's client but let Chromium through; today the user has to rerun with `--force-browser`, and an MCP agent spends an extra call doing so.

  Rules, exactly as agreed:
  - **403 only.** 429 is a backoff signal; hitting the site again with a browser is wrong. 429 and every other non-200 keep today's error.
  - **Only with a browser configured.** Without one, nothing changes: same `HTTP 403 for <url>` error, same exit code / MCP `[upstream]` prefix.
  - **Say so.** On a successful escalation print one short stderr notice, e.g. `notice: HTTP 403 for <url>; fetched with the browser`. Use the same stderr idiom as `browserFetchOrWarn` (scrape/scrape.go:386). Never write it to stdout or into page content, so `--json` output stays clean.
  - **If the browser fails too, return the original 403 error**, not the browser's error. The browser failure can go in a stderr `warn:` line.
  - The escalated page is `Source: SourceBrowser`, so it caches like any browser-rendered page.

  ## Where
  - `scrape/scrape.go:525` `fetchContent` returns `fmt.Errorf("HTTP %d for %s", ...)` for any non-200. Make that a typed error (e.g. `*HTTPStatusError{StatusCode, URL}`) whose `Error()` text is byte-for-byte the same, so callers can match it with `errors.As`. Check `cmd/` and `mcp/` exit-code and prefix classification still map it the same way.
  - Escalate in the non-forced paths that call the fetch: `scrapeConditional` (scrape.go:258; serves the cached pipeline and crawl), `Scrape` (scrape.go:215), and the non-forced branch of `fetchHTMLForSelector` (pipeline.go ~311). Build one shared helper rather than three copies, and reuse `BrowserScrape`/`getBrowser`. `--raw` (pipeline.go:152, `scrapeConditional` with `ErrPDFRawUnsupported`) should return the rendered HTML.
  - The `--force-browser` path (`classifyForForcedRender`, pipeline.go:329) already lets a 403 probe through and stays as it is.
  - Keep gocyclo ≤ 15.

  ## Docs
  MANUAL.md: the scrape/browser section says 403 escalates when a browser is configured. CHANGELOG.md: one entry under Unreleased. Update ADR-0003 (design/adr/0003-fast-path-first-scrape.md) only if its escalation list needs this added. A one-line mention in the skill's scrape reference if it tells agents to retry 403s with force_browser.

  Source: https://github.com/1broseidon/ketch/issues/81. Close the issue in the PR body (`Closes #81`), crediting the reporter.
source: https://github.com/1broseidon/ketch/issues/81
createdAt: "2026-10-13T00:28:57.388Z"
updatedAt: "2026-10-22T05:52:43.870Z"
relatedFiles:
  - scrape/scrape.go
  - scrape/pipeline.go
  - scrape/scrape_test.go
  - MANUAL.md
  - CHANGELOG.md
  - design/adr/0003-fast-path-first-scrape.md
contract:
  status: delivered
  deliverables:
    - type: file
      path: scrape/scrape.go
      description: typed HTTP status error (unchanged message) and a shared 403→browser escalation helper used by Scrape, scrapeConditional and the non-forced selector path
    - type: test
      path: scrape/scrape_test.go
      description: "httptest cases with a fake BrowserConn: 403+browser → SourceBrowser page and stderr notice; 403+browser failure → original 'HTTP 403 for <url>' error; 403 without browser → unchanged error; 429 → no browser attempt; --raw and selector paths escalate"
    - type: docs
      path: MANUAL.md
      description: scrape/browser section documents 403 escalation (403 only, browser required, stderr notice)
    - type: docs
      path: CHANGELOG.md
      description: "Unreleased entry crediting #81"
  validation:
    commands:
      - go build ./...
      - go test ./scrape/... ./crawl/... ./cmd/... ./mcp/...
      - go test ./...
      - go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
  constraints:
    - Escalate on 403 only; 429 and other non-200s keep today's error and never touch the browser
    - No browser configured → behaviour and error text byte-identical to today
    - Browser also fails → return the original 403 error, not the browser error
    - Notice goes to stderr only; stdout/--json output unchanged
    - The --force-browser path (classifyForForcedRender) is unchanged
    - gocyclo max 15; CGO_ENABLED=0
  metrics:
    readyAt: "2026-10-22T05:45:17.459Z"
    pickedUpAt: "2026-10-22T05:45:38.865Z"
    reworkCount: 0
    deliveredAt: "2026-10-22T05:48:29.402Z"
    duration: 171
dispatch:
  agent: claude
  model: opus
  effort: high
  by: person
  at: "2026-10-22T05:52:42.361Z"
  tries: 2
  claim:
    host: Georges-Mac-Studio
    runner: cli
    pid: 9615
    at: "2026-10-22T05:52:43.870Z"
assignee: claude
pullRequest:
  number: 92
  url: https://github.com/1broseidon/ketch/pull/92
  state: open
  base: main
  head: dispatch/task-7
  at: "2026-10-22T05:51:21.551Z"
---

## Log
- 2026-10-13T06:18:37.554Z: [claude] Audit 2026-10-13: still current — scrape/scrape.go:555 fails on any non-200. Precedent: classifyForForcedRender (scrape/pipeline.go:321) already lets a 403 probe fall through to the browser under --force-browser. Coordinate 429 handling with task-6 item 7 (bounded retry + Retry-After).
- 2026-10-16T15:19:31.282Z: [claude] Owner scoped the design on #81: escalate to the configured browser only on HTTP 403 (not 429 — that's a backoff signal, re-hitting with a browser is wrong); only when a browser is configured; print a short stderr notice that the page came from the browser; if the browser also fails, return the original 403. PR welcome.
- 2026-10-22T05:48:31.821Z: [claude] Done on branch dispatch/task-7 (1cadcdd): typed *HTTPStatusError (same message); shared browserOnForbidden helper used by Scrape, scrapeConditional (raw/crawl) and the non-forced selector path — 403 only, browser required, stderr notice, original 403 on browser failure; force-browser path untouched. New scrape/scrape_test.go covers all five paths (escalate, browser failure, no browser, 429/401/404/503 never touch browser). MANUAL, CHANGELOG (credits @ArdaYILDIZ-DEV), ADR-0003 step 4 updated; skill reference left alone (it never advised retrying 403 with force_browser). go test ./... and golangci-lint pass. PR body should say Closes #81.
- 2026-10-22T05:51:21.662Z: Opened pull request #92 from dispatch/task-7 into main: https://github.com/1broseidon/ketch/pull/92
- 2026-10-22T05:52:39.494Z: Sent back from review: This failed CI. Fix and send back
