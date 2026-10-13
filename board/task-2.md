---
id: task-2
title: Ask boneskull about the `ketch` npm package name
column: todo
position: 0
priority: low
tags:
  - bro-75
createdAt: "2026-10-11T21:01:29.930Z"
updatedAt: "2026-10-11T21:19:49.505Z"
---

## Description
**Parked deliberately** — raised, then set aside. Not blocking anything.

The bare `ketch` name on npm belongs to an unrelated package by boneskull, which is why we publish as `ketch-cli` with binaries under the `@ketch-cli` scope. That arrangement works fine, so this is upside, not a fix.

If picked up: draft a short outreach asking whether they'd be willing to transfer the name. Worth checking first whether the package is still maintained.

If it were ever transferred, the migration is non-trivial and should get its own issue — `ketch-cli` would need to stay published as an alias, and the six `@ketch-cli/*` binary packages are referenced by exact version from the root's `optionalDependencies`.

Migrated from Linear BRO-75: https://linear.app/1broseidon/issue/BRO-75/ask-boneskull-about-the-ketch-npm-package-name (2026-10-11).

## Status (2026-10-11 review)

boneskull's `ketch` npm package unchanged since 2022-06-19 — a transfer ask looks realistic.

## Log
- 2026-10-13T06:18:31.602Z: [claude] Audit 2026-10-13: still current — npm `ketch` 1.0.0, last modified 2022-06-19.
