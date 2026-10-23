---
id: task-7
title: "Scrape: escalate to the configured browser on HTTP 403 (#81)"
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
updatedAt: "2026-10-23T15:18:51.194Z"
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
    pickedUpAt: "2026-10-22T20:20:25.703Z"
    reworkCount: 2
    deliveredAt: "2026-10-22T20:21:08.698Z"
    duration: 43
dispatch:
  agent: claude
  model: opus
  effort: high
  by: chat
  at: "2026-10-22T20:20:15.924Z"
  tries: 3
  claim:
    host: Georges-Mac-Studio
    runner: cli
    pid: 62675
    at: "2026-10-22T20:20:18.276Z"
    ended: "2026-10-22T20:21:12.361Z"
    outcome: review
assignee: claude
parentId: epic-1
pullRequest:
  number: 92
  url: https://github.com/1broseidon/ketch/pull/92
  state: merged
  base: main
  head: dispatch/task-7
  at: "2026-10-22T05:51:21.551Z"
completedAt: "2026-10-23T15:18:51.194Z"
---

## Log
- 2026-10-13T06:18:37.554Z: [claude] Audit 2026-10-13: still current — scrape/scrape.go:555 fails on any non-200. Precedent: classifyForForcedRender (scrape/pipeline.go:321) already lets a 403 probe fall through to the browser under --force-browser. Coordinate 429 handling with task-6 item 7 (bounded retry + Retry-After).
- 2026-10-16T15:19:31.282Z: [claude] Owner scoped the design on #81: escalate to the configured browser only on HTTP 403 (not 429 — that's a backoff signal, re-hitting with a browser is wrong); only when a browser is configured; print a short stderr notice that the page came from the browser; if the browser also fails, return the original 403. PR welcome.
- 2026-10-22T05:48:31.821Z: [claude] Done on branch dispatch/task-7 (1cadcdd): typed *HTTPStatusError (same message); shared browserOnForbidden helper used by Scrape, scrapeConditional (raw/crawl) and the non-forced selector path — 403 only, browser required, stderr notice, original 403 on browser failure; force-browser path untouched. New scrape/scrape_test.go covers all five paths (escalate, browser failure, no browser, 429/401/404/503 never touch browser). MANUAL, CHANGELOG (credits @ArdaYILDIZ-DEV), ADR-0003 step 4 updated; skill reference left alone (it never advised retrying 403 with force_browser). go test ./... and golangci-lint pass. PR body should say Closes #81.
- 2026-10-22T05:51:21.662Z: Opened pull request #92 from dispatch/task-7 into main: https://github.com/1broseidon/ketch/pull/92
- 2026-10-22T05:52:39.494Z: Sent back from review: This failed CI. Fix and send back
- 2026-10-22T05:53:53.898Z: [claude] CI fix on branch dispatch/task-7 (c8c492d): the failing check was the security job (govulncheck), not the scrape change — GO-2026-6809 in crypto/x509, fixed in Go 1.26.10. Bumped go.mod's go directive 1.26.9 → 1.26.10 (CI/release take the toolchain from go.mod), the MANUAL version example, and added a CHANGELOG Unreleased entry. On Go 1.26.10: go build, go test ./..., make vulncheck (no vulnerabilities) and golangci-lint (0 issues) all pass. Not pushed; PR #92 needs the branch pushed to rerun CI.
- 2026-10-22T15:33:13.767Z: Pushed 1 commit to pull request #92: https://github.com/1broseidon/ketch/pull/92
- 2026-10-22T20:20:14.651Z: [claude] PR #92 (base release/v0.20.1) is DIRTY after #91 (search_order, task-8) merged. On dispatch/task-7: git fetch origin && git merge origin/release/v0.20.1 (no rebase, no force-push). Resolve conflicts keeping both changes — CHANGELOG.md Unreleased gets both entries (#80 and #81), MANUAL.md/README keep both the search_order docs and the 403-escalation docs; take go.mod/toolchain from the release branch (Go 1.26.10). Then go build ./... && go test ./... && golangci-lint run, and commit the merge.
- 2026-10-22T20:21:08.226Z: [claude] Merged origin/release/v0.20.1 into dispatch/task-7 (efd6d64, no rebase/force-push). Only conflict was CHANGELOG Unreleased: kept the release branch's Go 1.26.10 entry (dropped our duplicate), the search_order entry (#80) and the 403-escalation entry (#81); MANUAL/README auto-merged with both docs; go.mod is the release branch's go 1.26.10. Diff vs base is now only the #81 files. go build, go test ./..., golangci-lint (0 issues) pass. On its branch, not pushed — push to clear PR #92's DIRTY state.
- 2026-10-22T20:21:14.676Z: Pushed 5 commits to pull request #92: https://github.com/1broseidon/ketch/pull/92
- 2026-10-23T15:18:50.449Z: Pull request #92 was merged into main
