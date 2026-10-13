---
id: task-8
title: "feat: deterministic sequential provider fallback order"
column: todo
position: 5
description: |-
  ### Summary

  Support an explicit, ordered fallback sequence for search providers (e.g. `--fallback=exa,tinyfish,brave` or comma-separated `--backend`) so users can define the exact order backends are tried sequentially on failure.

  ### Problem

  Currently, search backend selection has four modes:
  - Explicit single backend: `--backend <id>`
  - Federated search: `--multi` (parallel fan-out with rank fusion)
  - Shuffled fallback: `--random` (sequential fallback, but candidates are shuffled via `rand.Perm`)
  - Automatic fallback: `--backend auto`

  In `auto` mode, fallback order is resolved by `AutoChain` (`search/auto.go`). Configured providers are ordered strictly by their static `AutoRank` (`search/registry.go`):
  - Brave: 30
  - TinyFish: 45
  - Exa: 80
  - DuckDuckGo: 120

  Because `AutoRank` is static, configuring an API key for Brave forces it ahead of TinyFish or Exa on every call. There is currently no way to enforce a deterministic sequence such as:
  1. Try Exa first (semantic search).
  2. Fall back to TinyFish if Exa errors or rate-limits.
  3. Fall back to Brave or DuckDuckGo only as a last resort.

  `--random` supports sequential error fallback, but it shuffles providers on every call.

  ### Proposed Change

  Add support for a deterministic fallback chain:

  ```bash
  # Option A: Dedicated flag
  ketch search --fallback=exa,tinyfish,brave "query"

  # Option B: Comma-separated list in --backend
  ketch search --backend=exa,tinyfish,brave "query"
  ```

  Correspondingly in the MCP tool:
  ```json
  {
    "fallback": ["exa", "tinyfish", "brave"]
  }
  ```

  ### Expected Behavior

  1. Execute the search against the first provider in the user-supplied list.
  2. If the request fails with a recoverable error or times out within the per-attempt budget, fall back to the next provider in sequence.
  3. Return the first successful result set, reporting which backend served it and any prior upstream errors (matching the current `SelectingSearcher` / `SearchOutput.Errors` behavior).
  4. Return an error only if all specified backends fail.
source: https://github.com/1broseidon/ketch/issues/80
createdAt: "2026-10-13T00:29:00.726Z"
---

## Log
- 2026-10-13T06:18:39.173Z: [claude] Audit 2026-10-13: still current; ranks quoted are right (Brave 30, TinyFish 45, Exa 80, DDG 120). Note that auto promotes configured providers first and uses rank as a tiebreak (search/auto.go:48-61). --random already takes an ordered list (cmd/search.go:44), so the fallback could be --random's list without the shuffle.
