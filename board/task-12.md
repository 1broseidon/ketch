---
id: task-12
title: "Merge PRs #91 and #92 into release/v0.20.1"
column: todo
position: 5
description: |-
  Merge both open PRs into `release/v0.20.1` on origin. Both already target that branch, are MERGEABLE/CLEAN, and have every CI check green (build, lint, test ubuntu/macos/windows, pi, security).

  Steps:
  1. `gh pr merge 91 --merge` (task-8, search_order config key, #80), then confirm it merged.
  2. Re-check #92 (task-7, scrape 403 → browser escalation, #81) against the updated base: `gh pr view 92 --json mergeable,mergeStateStatus`. If it conflicts with #91 (both likely touch CHANGELOG.md, MANUAL.md, config docs), resolve on its branch `dispatch/task-7` by merging `origin/release/v0.20.1` in (no force-push), run `go test ./...` and `make lint`, push, wait for CI green, then merge.
  3. `gh pr merge 92 --merge`.
  Use merge commits (main history shows "Merge PR #87" style). Do not delete the release branch; do not touch `main`. Do not open a release PR or tag anything.
relatedFiles:
  - CHANGELOG.md
contract:
  status: ready
  deliverables:
    - type: research
      path: release/v0.20.1
      description: "origin/release/v0.20.1 contains the merges of PR #91 and PR #92"
  validation:
    commands:
      - gh pr view 91 --json state --jq .state | grep -qx MERGED
      - gh pr view 92 --json state --jq .state | grep -qx MERGED
      - git fetch origin && git log --oneline origin/main..origin/release/v0.20.1 | grep -E '#9[12]'
  metrics:
    readyAt: "2026-10-22T15:48:39.200Z"
createdAt: "2026-10-22T15:48:39.202Z"
dispatch:
  agent: claude
  model: opus
  effort: high
  by: chat
  at: "2026-10-22T15:48:43.421Z"
  tries: 0
assignee: claude
updatedAt: "2026-10-22T15:48:43.533Z"
---

## Description
Merge both open PRs into `release/v0.20.1` on origin. Both already target that branch, are MERGEABLE/CLEAN, and have every CI check green (build, lint, test ubuntu/macos/windows, pi, security).

Steps:
1. `gh pr merge 91 --merge` (task-8, search_order config key, #80), then confirm it merged.
2. Re-check #92 (task-7, scrape 403 → browser escalation, #81) against the updated base: `gh pr view 92 --json mergeable,mergeStateStatus`. If it conflicts with #91 (both likely touch CHANGELOG.md, MANUAL.md, config docs), resolve on its branch `dispatch/task-7` by merging `origin/release/v0.20.1` in (no force-push), run `go test ./...` and `make lint`, push, wait for CI green, then merge.
3. `gh pr merge 92 --merge`.
Use merge commits (main history shows "Merge PR #87" style). Do not delete the release branch; do not touch `main`. Do not open a release PR or tag anything.
