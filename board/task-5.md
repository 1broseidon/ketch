---
id: task-5
title: Housekeeping from the 0.18 review (not gating)
column: todo
position: 3
priority: low
tags:
  - improvement
  - bro-92
createdAt: "2026-10-11T21:01:30.794Z"
updatedAt: "2026-10-11T21:19:51.780Z"
---

## Description
Small items noted during the RC review that do not affect the release. Batch them into one cleanup PR after 0.18.

* `extract.PruneLog`, `DebugSemanticRoot` and `DebugProseTrace` are exported with no consumer in the repo. Move the probe that uses them into `bench/` so they have a home, or unexport them.
* `cmd/readtest/main.go` is a stray debug main that imports go-readability and hits Wikipedia live; it ships in the module. Delete it or fold it into the bench probe.
* Charset: the `DecodeHTMLBytes` comment lists the BOM after Content-Type, but `charset.DetermineEncoding` gives the BOM priority; fix the comment. A wrong charset header is trusted over valid UTF-8 (browser-consistent); document it.
* Rowspan cells: `table.WithSpanCellBehavior(table.SpanBehaviorEmpty)` (`extract/extract.go:27`) emits an empty cell for the continuation row (`||120|`). Fine for data tables; note it in the extraction docs.
* Structural judgment calls to write down in `design/DESIGN.md` or the extract package doc: `<main hidden>` is extracted as is; `<dialog open>`, `<footer>` inside an article, `<noscript>`, inline SVG text and non-video iframes are removed.
* `mcp/server_test.go:100` has a weak assertion on the registered tool set; assert the six names.
* Clean-mode docs sentence: name matching is a heuristic layer that can drop content whose class names contain chrome words; that is the trade-off the mode buys.
* `.agents/`, `.anvil/` and `conventions.yaml` are untracked Anvil artifacts of Codex's tag work; add them to `.gitignore` or keep them out of the commit.

Migrated from Linear BRO-92: https://linear.app/1broseidon/issue/BRO-92/housekeeping-from-the-018-review-not-gating (2026-10-11).

## Status (2026-10-11 review)

Done: .agents/.anvil artifacts gone; MCP tool-set test covers the full set. Left: PruneLog/DebugSemanticRoot/DebugProseTrace still exported (extract/semantic.go ~594, 1868, 1893); cmd/readtest ships with the module; charset comment order (extract/charset.go ~10-15); rowspan behaviour and clean-mode heuristic undocumented; the error-text check at mcp/server_test.go ~100 omits `tag`.

## Log
- 2026-10-13T06:18:34.619Z: [claude] Audit 2026-10-13: all remaining items still open — PruneLog/DebugSemanticRoot/DebugProseTrace exported (extract/semantic.go:594,1868,1893); cmd/readtest/main.go present; DecodeHTML comment (extract/charset.go:10-15) still puts BOM after Content-Type; rowspan (extract/extract.go:27) and clean-mode heuristic undocumented; TestNewServerRejectsInvalidTools (mcp/server_test.go ~100) checks "valid: search, code, docs, scrape, crawl" without tag.
