---
id: task-12
title: "Merge PRs #91 and #92 into release/v0.20.1"
column: review
position: 3
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
  status: in_progress
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
    pickedUpAt: "2026-10-22T15:48:50.758Z"
    reworkCount: 0
createdAt: "2026-10-22T15:48:39.202Z"
dispatch:
  agent: claude
  model: opus
  effort: high
  by: chat
  at: "2026-10-22T15:48:43.421Z"
  tries: 1
  claim:
    host: Georges-Mac-Studio
    runner: cli
    pid: 80319
    at: "2026-10-22T15:48:45.074Z"
assignee: claude
updatedAt: "2026-10-22T15:49:35.809Z"
---

## Description
Merge both open PRs into `release/v0.20.1` on origin. Both already target that branch, are MERGEABLE/CLEAN, and have every CI check green (build, lint, test ubuntu/macos/windows, pi, security).

Steps:
1. `gh pr merge 91 --merge` (task-8, search_order config key, #80), then confirm it merged.
2. Re-check #92 (task-7, scrape 403 → browser escalation, #81) against the updated base: `gh pr view 92 --json mergeable,mergeStateStatus`. If it conflicts with #91 (both likely touch CHANGELOG.md, MANUAL.md, config docs), resolve on its branch `dispatch/task-7` by merging `origin/release/v0.20.1` in (no force-push), run `go test ./...` and `make lint`, push, wait for CI green, then merge.
3. `gh pr merge 92 --merge`.
Use merge commits (main history shows "Merge PR #87" style). Do not delete the release branch; do not touch `main`. Do not open a release PR or tag anything.

## Log
- 2026-10-22T15:49:35.455Z: [claude] Merged PR #91 into release/v0.20.1 with a merge commit (75bb95c, state MERGED). Did NOT merge #92: it is now CONFLICTING/DIRTY against the updated base. Only CHANGELOG.md conflicts (MANUAL.md auto-merges). Both branches added their own Unreleased paragraph about the Go 1.26.10 / GO-2026-6809 toolchain bump, and each then adds its feature paragraph. Suggested resolution: keep the release branch's "Go toolchain patched to 1.26.10" paragraph (it has the vuln link) and drop #92's duplicate "Built with Go 1.26.10", then keep both the search_order paragraph (#80) and the 403 → browser paragraph (#81). This dispatch ran in a read-only checkout (no commits allowed), so nobody merged origin/release/v0.20.1 into dispatch/task-7. Someone with write access needs to do that merge with this resolution, run go test ./... and make lint, push without force, wait for CI, then `gh pr merge 92 --merge`. Contract not delivered: its #92 deliverable is unmet.
