---
id: task-7
title: "feat: auto-escalate to headless browser on HTTP 403/429 scrape errors"
column: todo
position: 4
description: |-
  ### Summary

  Automatically retry scrapes via the configured headless browser when the initial HTTP fetch receives an HTTP 403 (Forbidden) or HTTP 429 (Too Many Requests), instead of immediately aborting and requiring a manual `--force-browser` re-run.

  ### Problem

  Currently, automatic browser fallback is gated strictly behind JS-shell detection on successful HTTP 200 responses (`scrape/scrape.go`):

  ```go
  if resp.StatusCode != http.StatusOK {
      return nil, nil, false, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
  }
  ```

  If a target website blocks Go's standard `net/http` transport (common with Cloudflare, Akamai, or WAF bot heuristics triggered by TLS/JA3 fingerprints or missing browser headers), the server responds with HTTP 403 or 429. Ketch immediately fails with `HTTP 403 for <url>`.

  In practice:
  - Many of these sites render successfully when fetched via Chromium (which provides a full browser TLS handshake and passes passive JS challenges).
  - Today, users in the CLI must manually re-run the command with `--force-browser`.
  - In automated MCP workflows, agents must inspect the error message and spend an extra tool call re-requesting the scrape with `force_browser: true`.

  ### Proposed Change

  When a browser binary is configured and available (`s.HasBrowser()`):

  1. If the fast HTTP fetch returns `HTTP 403` or `HTTP 429`, do not fail immediately.
  2. Escalate the request to the headless browser (`browserFetchOrWarn`).
  3. If the browser successfully loads and renders the document:
     - Return the extracted page with `Source: SourceBrowser`.
     - Optionally print a diagnostic notice on stderr (e.g., `notice: HTTP 403 on initial fetch; fell back to headless browser`).
  4. If no browser is configured, or if the browser fetch also fails/times out, return the upstream HTTP error as usual.

  ### Benefits

  - **Better CLI UX:** Fast HTTP path remains the default; difficult sites succeed transparently on the first attempt without manual flag tweaking.
  - **Resilient MCP Tooling:** Eliminates redundant agent turns caused by recoverable WAF/challenge 403 errors.
source: https://github.com/1broseidon/ketch/issues/81
createdAt: "2026-10-13T00:28:57.388Z"
---

## Log
- 2026-10-13T06:18:37.554Z: [claude] Audit 2026-10-13: still current — scrape/scrape.go:555 fails on any non-200. Precedent: classifyForForcedRender (scrape/pipeline.go:321) already lets a 403 probe fall through to the browser under --force-browser. Coordinate 429 handling with task-6 item 7 (bounded retry + Retry-After).
- 2026-10-16T15:19:31.282Z: [claude] Owner scoped the design on #81: escalate to the configured browser only on HTTP 403 (not 429 — that's a backoff signal, re-hitting with a browser is wrong); only when a browser is configured; print a short stderr notice that the page came from the browser; if the browser also fails, return the original 403. PR welcome.
