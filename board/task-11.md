---
id: task-11
title: "Cut release/v0.20.1 on origin and retarget PRs #91 and #92 to it"
column: review
position: 5
description: |-
  This is the git and GitHub setup for epic-1 (Release v0.20.1). It changes no code and makes no commits.

  1. Cut `release/v0.20.1` from the current `origin/main` and push it:
     `git fetch origin && git push origin origin/main:refs/heads/release/v0.20.1`
     If the branch already exists on origin, leave it as it is.
  2. Point both open PRs at the new branch:
     `gh pr edit 91 --base release/v0.20.1` (task-8, search_order)
     `gh pr edit 92 --base release/v0.20.1` (task-7, 403 browser escalation)
  3. Confirm each PR's diff still holds only its own task's commits plus the Go 1.26.10 bump.

  Leave the PR branches alone: no force-push, no rebase, no merge.
contract:
  status: delivered
  validation:
    commands:
      - git ls-remote --exit-code --heads origin release/v0.20.1
      - test "$(gh pr view 91 --json baseRefName -q .baseRefName)" = release/v0.20.1
      - test "$(gh pr view 92 --json baseRefName -q .baseRefName)" = release/v0.20.1
  constraints:
    - No commits, no code changes
    - Never force-push; don't touch dispatch/task-7 or dispatch/task-8
    - Don't merge or close either PR
  metrics:
    readyAt: "2026-10-22T15:40:39.486Z"
    pickedUpAt: "2026-10-22T15:40:48.836Z"
    reworkCount: 0
    deliveredAt: "2026-10-22T15:41:12.647Z"
    duration: 24
createdAt: "2026-10-22T15:40:39.488Z"
dispatch:
  agent: claude
  model: opus
  effort: high
  by: chat
  at: "2026-10-22T15:40:41.649Z"
  tries: 1
  claim:
    host: Georges-Mac-Studio
    runner: cli
    pid: 80319
    at: "2026-10-22T15:40:44.132Z"
    ended: "2026-10-22T15:41:17.891Z"
    outcome: review
assignee: claude
updatedAt: "2026-10-22T15:41:17.891Z"
---

## Description
This is the git and GitHub setup for epic-1 (Release v0.20.1). It changes no code and makes no commits.

1. Cut `release/v0.20.1` from the current `origin/main` and push it:
   `git fetch origin && git push origin origin/main:refs/heads/release/v0.20.1`
   If the branch already exists on origin, leave it as it is.
2. Point both open PRs at the new branch:
   `gh pr edit 91 --base release/v0.20.1` (task-8, search_order)
   `gh pr edit 92 --base release/v0.20.1` (task-7, 403 browser escalation)
3. Confirm each PR's diff still holds only its own task's commits plus the Go 1.26.10 bump.

Leave the PR branches alone: no force-push, no rebase, no merge.

## Log
- 2026-10-22T15:41:14.819Z: [claude] Pushed release/v0.20.1 to origin at origin/main 2ffbadf (it did not exist yet) and retargeted PRs #91 and #92 to it; validation commands pass. #91 holds 56e1f5d, f1928ff (search_order) and 99d8165 (Go 1.26.10); #92 holds 1cadcdd (403 browser escalation) and c8c492d (Go 1.26.10); each go.mod change is only 1.26.9→1.26.10. PR branches untouched. No code changes or commits, so dispatch/task-11 has nothing new.
