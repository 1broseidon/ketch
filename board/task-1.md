---
id: task-1
title: Track the winget submission through Microsoft moderation
column: in-progress
position: 0
priority: medium
tags:
  - bro-72
createdAt: "2026-10-11T21:01:29.644Z"
updatedAt: "2026-10-12T20:32:21.784Z"
assignee: claude
---

## Description
**Update 2026-09-18 \~16:45Z — full pipeline green, waiting only on a human admin.**

All ten numbered checks now SUCCESS, including the three that were queued at submission: 08 Installation Validation, 09 Installer Metadata Validation, 10 Validation Completed. Label `Azure-Pipeline-Passed` added. 05 Manifest Policy remains NEUTRAL, which is the route-to-manual-review outcome, not a failure. `mergeable: MERGEABLE`.

Microsoft's VM install independently confirms the Windows 11 test we ran locally.

Two labels/comments that look alarming but aren't:

* `Validation-Guide` + "There was an issue validating your pull request" is boilerplate attached alongside the policy-test routing. No check failed; there is no `Needs-Author-Feedback` or `Validation-Error` label.
* `Policy-Test-2.7` is the policy test's *version*, not policy §2.7 ("Adult Content" in the published policy doc). Verified: 193 open PRs carry the same label — subnet tools, text editors, chat apps.

No action required. Do not ping moderators; it slows things down on this repo.

---

First-ever submission of `1broseidon.ketch` to winget, for v0.17.1. Submitted manually as a one-off; from v0.18.0 GoReleaser opens these PRs automatically.

**PR:** [https://github.com/microsoft/winget-pkgs/pull/437139](<https://github.com/microsoft/winget-pkgs/pull/437139>)

As of submission: OPEN, MERGEABLE, labelled `New-Package`, CLA signed. Checks 01–07 all SUCCESS (05 NEUTRAL), including **07. Installers Scan** — the Defender/malware scan on an unsigned binary from a brand-new publisher, which was the main risk. 08–10 (installation validation) were still running.

A new package's first PR waits on a human moderator. Later versions usually merge automatically once validation passes.

**Verified before submitting:**

* `winget validate --manifest` passed on Windows 11
* `winget install --manifest` installed cleanly; `PortableCommandAlias: ketch` resolved and `ketch search` returned results with no config
* Both installer SHA256s checked against the release `checksums.txt`
* All three manifests validated against the official v1.12.0 schemas in `microsoft/winget-cli`

Interactive install was x64 only; arm64 rests on checksum + schema validation.

**Close when:** merged, or a moderator asks for changes.

## Folded in from BRO-73

On the next release (v0.18.0 went out already), check that the npm propagation gate (`11a8532`) actually ran: all six `@ketch-cli/*` platform packages became readable before the root was published, and `npm_config_cache=$(mktemp -d) npx -y ketch-cli version` works from a clean cache. Also check that GoReleaser opened the winget PR itself. Note that a winget failure blocks the npm publish, since `npm` depends on `release`.

Migrated from Linear BRO-72: https://linear.app/1broseidon/issue/BRO-72/track-the-winget-submission-through-microsoft-moderation (2026-10-11).

## Status (2026-10-11 review)

winget PR microsoft/winget-pkgs#437139 still open (validation passed, back in manual review; moderator pinged 10-09 — the "do not ping" line above is out of date). npm gate done: all 6 @ketch-cli/* at 0.19.0. Close condition: after the winget merge, drop `--skip=winget` (.github/workflows/release.yml ~82).

## Log
- 2026-10-12T20:32:39.920Z: [claude] 2026-10-12 check: winget-pkgs#437139 still OPEN, not merged, REVIEW_REQUIRED (labels Azure-Pipeline-Passed, Validation-Completed, New-Package). Last activity: our 10-09 ping to stephengillie, no moderator reply or change request since. Close condition not met, so left open. Note: release.yml says --skip=winget also covers the v0.18.0 403 when WINGET_TOKEN opened the PR, so after the merge, check the token's permissions before dropping the skip.
- 2026-10-20T05:23:20.117Z: [George Dikeakos] 2026-10-20 check: winget-pkgs#437139 (v0.17.1, first submission) still OPEN awaiting a moderator. v0.20.0 release workflow is running and should open its own winget PR via GoReleaser.
- 2026-10-20T05:32:06.140Z: [George Dikeakos] Correction to the 2026-10-20 note: release.yml skips winget (goreleaser --skip) until winget-pkgs#437139 is approved, so v0.20.0 did not open a winget PR.
