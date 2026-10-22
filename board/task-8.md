---
id: task-8
title: "search_order config key: operator-set order for the auto chain (#80)"
column: in-progress
position: 1
description: |-
  Design settled by the owner on #80 (2026-10-16). Build it.

  ## What
  A `search_order` config key: an ordered list of search backend IDs, e.g. `["exa","tinyfish","brave"]`, plus a `KETCH_SEARCH_ORDER` env override (comma-separated). **When it is set, `auto` tries only those backends, in that order.** When it is unset or empty, `auto` behaves exactly as it does today (configured providers promoted first, then by AutoRank).

  Explicitly **not** in scope, as agreed: no `--fallback` flag and no comma-list `--backend`, because either would look like `--multi` but behave differently. A per-call option can come later on top. `--multi`, `--random` and an explicit `-b <id>` are unaffected. MCP gets the behaviour for free through `search.NewFromConfig`, so it needs no new tool param.

  ## Rules
  - Validate fail-loud at `config set`, on the env override, and when the chain is built: every entry is a registered search provider ID. Reject `auto`, unknown IDs and duplicates. Normalize case/whitespace the way `NormalizeMCPTools` does. Accept a JSON array or a comma-separated list, as `mcp_tools` does.
  - A listed provider that is not `Usable` (e.g. its key is missing) drops out of the chain, the same as today's chain drops unusable providers. If none are left, return the existing `no usable search backends for "auto"` error, naming search_order so the operator knows why.
  - Order is exactly the operator's. No promotion and no AutoRank re-sort. Providers with AutoRank 0 (keyed-only ones) may be listed.
  - The per-attempt timeout and 30s chain budget (search/auto.go) are unchanged.

  ## Where
  - `internal/configbase/config.go`: add the field next to `MCPTools`, with a new `order` value; keep existing orders.
  - `config/env.go`: KETCH_SEARCH_ORDER in the env-override table, modelled on mcp_tools (env.go:112).
  - `config/`: a `NormalizeSearchOrder` validator. It needs the search registry's IDs; take care over the import cycle (search imports internal/configbase). Validating in `search/` and calling it from cmd/config is fine.
  - `cmd/config.go`: `config set search_order`, shown in `ketch config` output; the effective auto chain display reflects it.
  - `search/auto.go`: `AutoChain` honours `cfg.SearchOrder`. `AutoChainNames` and doctor then follow automatically. Check that `doctor` reports the chain in the new order.
  - Regenerate fixtures with `UPDATE_REGISTRY_GOLDENS=1 go test ./cmd/ ./doctor/`. Only the new key's entries may change.

  ## Docs
  MANUAL.md (config keys table plus the auto backend section), README quickstart if it lists config keys, CLAUDE.md/AGENTS.md config notes if they enumerate keys, skills/ketch setup reference (search backend decision tree), and CHANGELOG.md under Unreleased.

  Source: https://github.com/1broseidon/ketch/issues/80. Put `Closes #80` in the PR body, crediting the reporter.
source: https://github.com/1broseidon/ketch/issues/80
createdAt: "2026-10-13T00:29:00.726Z"
updatedAt: "2026-10-22T05:53:29.370Z"
relatedFiles:
  - search/auto.go
  - search/auto_test.go
  - internal/configbase/config.go
  - config/env.go
  - cmd/config.go
  - doctor/
  - MANUAL.md
  - CHANGELOG.md
  - skills/ketch/references/verbs/setup.md
contract:
  status: in_progress
  deliverables:
    - type: file
      path: search/auto.go
      description: "AutoChain honours cfg.SearchOrder: only listed, usable providers, in the operator's order; unset = today's ordering"
    - type: file
      path: internal/configbase/config.go
      description: SearchOrder []string field (json search_order) with a new order value
    - type: file
      path: config/env.go
      description: KETCH_SEARCH_ORDER override, validated fail-loud
    - type: file
      path: cmd/config.go
      description: config set search_order (JSON array or comma list), shown in ketch config
    - type: test
      path: search/auto_test.go
      description: order respected exactly (incl. AutoRank-0 keyed provider), unusable entries drop out, all-unusable errors naming search_order, unset unchanged
    - type: test
      path: cmd/config_test.go
      description: set/validate search_order rejects auto, unknown IDs, duplicates; env override validated
    - type: docs
      path: MANUAL.md
      description: search_order in the config table and auto backend section
    - type: docs
      path: CHANGELOG.md
      description: "Unreleased entry crediting #80"
  validation:
    commands:
      - go build ./...
      - go test ./search/... ./config/... ./cmd/... ./doctor/... ./mcp/...
      - go test ./...
      - go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
      - git diff --stat -- cmd/testdata doctor/testdata
  constraints:
    - No --fallback flag and no comma-list --backend
    - "--multi, --random and explicit -b are unaffected"
    - Unset/empty search_order leaves auto's chain byte-identical to today
    - No new MCP tool params; MCP follows via search.NewFromConfig
    - Golden fixture changes limited to the new key's entries
    - gocyclo max 15; CGO_ENABLED=0
  metrics:
    readyAt: "2026-10-22T05:45:22.938Z"
    pickedUpAt: "2026-10-22T05:53:29.370Z"
    reworkCount: 1
    deliveredAt: "2026-10-22T05:49:46.793Z"
    duration: 250
dispatch:
  agent: claude
  model: opus
  effort: high
  by: person
  at: "2026-10-22T05:53:17.784Z"
  tries: 2
  claim:
    host: Georges-Mac-Studio
    runner: cli
    pid: 9615
    at: "2026-10-22T05:53:20.651Z"
assignee: claude
pullRequest:
  number: 91
  url: https://github.com/1broseidon/ketch/pull/91
  state: open
  base: main
  head: dispatch/task-8
  at: "2026-10-22T05:51:02.656Z"
---

## Log
- 2026-10-13T06:18:39.173Z: [claude] Audit 2026-10-13: still current; ranks quoted are right (Brave 30, TinyFish 45, Exa 80, DDG 120). Note that auto promotes configured providers first and uses rank as a tiebreak (search/auto.go:48-61). --random already takes an ordered list (cmd/search.go:44), so the fallback could be --random's list without the shuffle.
- 2026-10-16T15:19:31.604Z: [claude] Owner settled the design on #80: no --fallback flag and no comma-list --backend (would look like --multi but behave differently). Instead a `search_order` config key (list of backend IDs, e.g. ["exa","tinyfish","brave"]) plus KETCH_SEARCH_ORDER env override; when set, `auto` tries only those backends, in that order. MCP agents get it for free; a per-call flag can come later on top. Happy to review a PR.
- 2026-10-22T05:49:48.996Z: [claude] Implemented search_order + KETCH_SEARCH_ORDER on branch dispatch/task-8 (56e1f5d code+tests, f1928ff docs). auto uses exactly the listed usable providers in order; validation (auto/unknown/duplicate/blank) at config set, env, and chain build; doctor names search_order when the pinned chain is empty or invalid. ketch config shows search_order right after backend (omitempty, so default output is unchanged); goldens changed only by the new key in the configured fixtures. go test ./..., golangci-lint clean. PR body still needs "Closes #80".
- 2026-10-22T05:51:02.784Z: Opened pull request #91 from dispatch/task-8 into main: https://github.com/1broseidon/ketch/pull/91
- 2026-10-22T05:53:14.832Z: Sent back from review: This failed CI. Fix and send back
- 2026-10-22T05:54:16.435Z: [claude] CI failure was the security job: govulncheck flagged stdlib GO-2026-6809 (crypto/x509, fixed in Go 1.26.10), unrelated to search_order but new since main. Bumped go directive 1.26.9 -> 1.26.10 (+ MANUAL version example, CHANGELOG Unreleased entry) in 99d8165 on dispatch/task-8. Under go1.26.10: make vulncheck clean, go build/test ./... pass, golangci-lint 0 issues, no golden changes. Needs a push to rerun PR #91 CI.
