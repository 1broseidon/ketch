---
id: task-9
title: "Official pi package: ketch skill + extension tools installable via `pi install git:github.com/1broseidon/ketch`"
column: in-progress
position: 4
description: |-
  ## Why

  Audit (2026-10-19): the repo has an official Claude Code plugin (`.claude-plugin/marketplace.json` → `plugins/ketch/`, which symlinks `skills/ketch` and registers `ketch mcp serve` via `.mcp.json`) but nothing for **pi** (badlogic/pi-mono, npm `@earendil-works/pi-coding-agent`). No board task, commit, or doc mentions it. pi does not speak MCP natively; its equivalent of our Claude Code plugin is a *pi package*: a `package.json` with a `pi` manifest exposing skills and extensions, installable with `pi install git:<repo>@<ref>` / `npm:<pkg>` / local path.

  Goal: `pi install git:github.com/1broseidon/ketch` gives a pi user the same thing `claude plugin install ketch@ketch` gives a Claude Code user — the bundled ketch research skill plus first-class tools over the five research surfaces + tag. The CLI on PATH stays the prerequisite; the package never bundles the binary (same stance as the Claude Code plugin).

  ## Shape

  pi git installs take a repository root, not a subdirectory, so the manifest lives at the repo root:

  1. **`package.json` (repo root)** — `"private": true`, `"name": "ketch-pi"` (or similar), `"keywords": ["pi-package", ...]`, `"pi": { "skills": ["./skills/ketch"], "extensions": ["./plugins/pi/extensions/ketch.ts"] }`. Host-provided packages (`@earendil-works/pi-coding-agent`, `typebox`) go in `peerDependencies` with `"*"` — never `dependencies` (pi warns on bundled copies). Dev-only deps (typebox for tests, a test runner or `node:test`) may sit in `devDependencies` — pi installs only `dependencies` on git install. Add `/node_modules/` to `.gitignore`. Confirm `npm/build.mjs` and goreleaser are unaffected by a root package.json.

  2. **`plugins/pi/extensions/ketch.ts`** — default-export factory `(pi: ExtensionAPI) => void` registering tools via `pi.registerTool` (fields: `name`, `label`, `description`, `promptSnippet`, `parameters` TypeBox schema, `execute(toolCallId, params, signal, onUpdate, ctx)` → `{ content, details }`; `isError: true` on failure). Each tool shells out with `pi.exec("ketch", [...args, "--json"], { signal, timeout })` and returns stdout as the tool content (parsed JSON in `details`/`structuredContent` where cheap).
     - Tools, mirroring the MCP option parity in `mcp/` (read `mcp/*.go` for the exact param sets and descriptions; keep them in step): `ketch_search` (query, limit, backend, multi, scrape, searxng_url, tag), `ketch_code` (query, lang, repo, regexp, backend, tag), `ketch_docs` (query, library, tokens, resolve, tag), `ketch_scrape` (url | urls, selector, raw, force_browser, no_llms_txt, trim, max_chars, no_cache, tag), `ketch_crawl` (url, depth, sitemap, allow, deny, max_pages, no_cache, tag — synchronous and bounded like the MCP tool: pass `--max-pages`/equivalent if the CLI has it, otherwise cap client-side and document it; no `--background`), `ketch_tag` (action add/show/list/remove, name, urls, limit).
     - Error taxonomy: map exit codes to the same stable prefixes the MCP server uses — 2 `[validation]`, 3 `[not_found]`, 4 `[upstream]`, 5 `[precondition]`, 6 `[cancelled]`; unknown non-zero → `[upstream]` with stderr; `ENOENT`/binary missing → `[precondition] ketch not on PATH: brew install ketch or go install github.com/1broseidon/ketch@latest`. Returned with `isError: true` so the model sees it.
     - Config-level settings (API keys, cache TTL, browser) are *not* tool params — same rule as MCP. Optionally register a `/ketch-doctor` slash command via `pi.registerCommand` that runs `ketch doctor` and notifies; keep it small.
     - Never hold state across calls; no startup network calls; a missing binary must not break pi startup.

  3. **Skill compatibility** — pi loads `skills/ketch/SKILL.md` directly. Its frontmatter carries `version: 0.1.0`, which pi lists as not allowed (warning, not fatal); check whether Claude Code needs it and either drop it or keep it with a note. Description is 686 chars (pi limit 1024) — fine. Name rules satisfied.

  4. **Docs** — `plugins/pi/README.md` (install, what it registers, prerequisites, how it relates to the CLI/MCP); README.md "Agent integration" gets a `### pi package` sibling to "Claude Code plugin"; MANUAL.md gets the same short section next to its "Claude Code plugin" section (site/ builds ketch.run from MANUAL.md); CHANGELOG.md entry under Unreleased/Added; `skills/ketch/SKILL.md` transport section mentions pi's tools (`ketch_search`, …) alongside the MCP tool names where it says "check for ketch's MCP tools".

  5. **Tests** — unit tests for the extension that stub `pi.exec`: argument construction per tool (flags appear only when params are set; `--json` always), exit-code → prefix mapping, binary-missing → `[precondition]`, batch `urls` → positional args, `signal` forwarded. Runnable with `npm test` from the root without pi installed. Do not add Go code; `go test ./...` and `golangci-lint run` must still pass unchanged.

  ## References
  - pi packages: https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/packages.md
  - pi extensions: https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/extensions.md (ToolDefinition in `src/core/extensions/types.ts`; `pi.exec(command, args, { signal, timeout, cwd })` → `{ stdout, stderr, code, killed }`)
  - pi skills: https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/skills.md
  - Existing Claude Code plugin: `.claude-plugin/marketplace.json`, `plugins/ketch/`
  - MCP tool params/descriptions to mirror: `mcp/`
tags:
  - integration
  - pi
  - plugin
relatedFiles:
  - package.json
  - plugins/pi/extensions/ketch.ts
  - plugins/pi/README.md
  - skills/ketch/SKILL.md
  - plugins/ketch/.claude-plugin/plugin.json
  - .claude-plugin/marketplace.json
  - mcp/
  - README.md
  - MANUAL.md
  - CHANGELOG.md
  - .gitignore
contract:
  status: in_progress
  deliverables:
    - type: file
      path: package.json
      description: Root pi package manifest (private, pi-package keyword, pi.skills → ./skills/ketch, pi.extensions → ./plugins/pi/extensions/ketch.ts, host packages as peerDependencies only)
    - type: file
      path: plugins/pi/extensions/ketch.ts
      description: pi extension registering ketch_search/ketch_code/ketch_docs/ketch_scrape/ketch_crawl/ketch_tag over `ketch … --json` via pi.exec, with the [validation]/[not_found]/[upstream]/[precondition]/[cancelled] exit-code mapping
    - type: docs
      path: plugins/pi/README.md
      description: Install and usage notes for the pi package
    - type: test
      path: plugins/pi/
      description: Unit tests with a stubbed pi.exec covering arg construction, exit-code prefixes, missing binary, batch urls, abort signal
    - type: docs
      path: README.md
      description: "### pi package section under Agent integration"
    - type: docs
      path: MANUAL.md
      description: pi package section beside the Claude Code plugin section
    - type: docs
      path: CHANGELOG.md
      description: Unreleased entry
    - type: docs
      path: skills/ketch/SKILL.md
      description: Transport section names the pi tools; frontmatter compatible with pi's allowed fields
  validation:
    commands:
      - npm install && npm test
      - node -e "const p=require('./package.json'); if(!p.private||!p.keywords.includes('pi-package')||!p.pi?.skills?.length||!p.pi?.extensions?.length||p.dependencies) process.exit(1)"
      - go test ./...
      - golangci-lint run
      - test -z "$(git diff --name-only main -- '*.go')"
      - grep -q 'pi install' README.md && grep -q 'pi install' MANUAL.md && grep -qi 'pi package' CHANGELOG.md
  constraints:
    - Never bundle or download the ketch binary; ketch on PATH is the prerequisite, same as the Claude Code plugin
    - No Go changes; existing CLI/MCP behavior untouched
    - "@earendil-works/* and typebox only in peerDependencies (\"*\"), never dependencies"
    - Tool param sets and descriptions mirror the MCP tools in mcp/; config-level settings are not tool params
    - A missing ketch binary must not break pi startup — it surfaces as a [precondition] tool error
    - Do not edit .brainfile/ by hand
  metrics:
    readyAt: "2026-10-19T21:50:55.443Z"
    pickedUpAt: "2026-10-19T23:49:48.267Z"
    reworkCount: 1
    deliveredAt: "2026-10-19T22:01:03.129Z"
    duration: 593
createdAt: "2026-10-19T21:50:55.444Z"
dispatch:
  agent: claude
  model: opus
  effort: high
  by: person
  at: "2026-10-19T23:49:22.728Z"
  tries: 2
  claim:
    host: Georges-Mac-Studio
    runner: cli
    pid: 38560
    at: "2026-10-19T23:49:32.101Z"
assignee: claude
updatedAt: "2026-10-19T23:49:48.267Z"
---

## Description
## Why

Audit (2026-10-19): the repo has an official Claude Code plugin (`.claude-plugin/marketplace.json` → `plugins/ketch/`, which symlinks `skills/ketch` and registers `ketch mcp serve` via `.mcp.json`) but nothing for **pi** (badlogic/pi-mono, npm `@earendil-works/pi-coding-agent`). No board task, commit, or doc mentions it. pi does not speak MCP natively; its equivalent of our Claude Code plugin is a *pi package*: a `package.json` with a `pi` manifest exposing skills and extensions, installable with `pi install git:<repo>@<ref>` / `npm:<pkg>` / local path.

Goal: `pi install git:github.com/1broseidon/ketch` gives a pi user the same thing `claude plugin install ketch@ketch` gives a Claude Code user — the bundled ketch research skill plus first-class tools over the five research surfaces + tag. The CLI on PATH stays the prerequisite; the package never bundles the binary (same stance as the Claude Code plugin).

## Shape

pi git installs take a repository root, not a subdirectory, so the manifest lives at the repo root:

1. **`package.json` (repo root)** — `"private": true`, `"name": "ketch-pi"` (or similar), `"keywords": ["pi-package", ...]`, `"pi": { "skills": ["./skills/ketch"], "extensions": ["./plugins/pi/extensions/ketch.ts"] }`. Host-provided packages (`@earendil-works/pi-coding-agent`, `typebox`) go in `peerDependencies` with `"*"` — never `dependencies` (pi warns on bundled copies). Dev-only deps (typebox for tests, a test runner or `node:test`) may sit in `devDependencies` — pi installs only `dependencies` on git install. Add `/node_modules/` to `.gitignore`. Confirm `npm/build.mjs` and goreleaser are unaffected by a root package.json.

2. **`plugins/pi/extensions/ketch.ts`** — default-export factory `(pi: ExtensionAPI) => void` registering tools via `pi.registerTool` (fields: `name`, `label`, `description`, `promptSnippet`, `parameters` TypeBox schema, `execute(toolCallId, params, signal, onUpdate, ctx)` → `{ content, details }`; `isError: true` on failure). Each tool shells out with `pi.exec("ketch", [...args, "--json"], { signal, timeout })` and returns stdout as the tool content (parsed JSON in `details`/`structuredContent` where cheap).
   - Tools, mirroring the MCP option parity in `mcp/` (read `mcp/*.go` for the exact param sets and descriptions; keep them in step): `ketch_search` (query, limit, backend, multi, scrape, searxng_url, tag), `ketch_code` (query, lang, repo, regexp, backend, tag), `ketch_docs` (query, library, tokens, resolve, tag), `ketch_scrape` (url | urls, selector, raw, force_browser, no_llms_txt, trim, max_chars, no_cache, tag), `ketch_crawl` (url, depth, sitemap, allow, deny, max_pages, no_cache, tag — synchronous and bounded like the MCP tool: pass `--max-pages`/equivalent if the CLI has it, otherwise cap client-side and document it; no `--background`), `ketch_tag` (action add/show/list/remove, name, urls, limit).
   - Error taxonomy: map exit codes to the same stable prefixes the MCP server uses — 2 `[validation]`, 3 `[not_found]`, 4 `[upstream]`, 5 `[precondition]`, 6 `[cancelled]`; unknown non-zero → `[upstream]` with stderr; `ENOENT`/binary missing → `[precondition] ketch not on PATH: brew install ketch or go install github.com/1broseidon/ketch@latest`. Returned with `isError: true` so the model sees it.
   - Config-level settings (API keys, cache TTL, browser) are *not* tool params — same rule as MCP. Optionally register a `/ketch-doctor` slash command via `pi.registerCommand` that runs `ketch doctor` and notifies; keep it small.
   - Never hold state across calls; no startup network calls; a missing binary must not break pi startup.

3. **Skill compatibility** — pi loads `skills/ketch/SKILL.md` directly. Its frontmatter carries `version: 0.1.0`, which pi lists as not allowed (warning, not fatal); check whether Claude Code needs it and either drop it or keep it with a note. Description is 686 chars (pi limit 1024) — fine. Name rules satisfied.

4. **Docs** — `plugins/pi/README.md` (install, what it registers, prerequisites, how it relates to the CLI/MCP); README.md "Agent integration" gets a `### pi package` sibling to "Claude Code plugin"; MANUAL.md gets the same short section next to its "Claude Code plugin" section (site/ builds ketch.run from MANUAL.md); CHANGELOG.md entry under Unreleased/Added; `skills/ketch/SKILL.md` transport section mentions pi's tools (`ketch_search`, …) alongside the MCP tool names where it says "check for ketch's MCP tools".

5. **Tests** — unit tests for the extension that stub `pi.exec`: argument construction per tool (flags appear only when params are set; `--json` always), exit-code → prefix mapping, binary-missing → `[precondition]`, batch `urls` → positional args, `signal` forwarded. Runnable with `npm test` from the root without pi installed. Do not add Go code; `go test ./...` and `golangci-lint run` must still pass unchanged.

## References
- pi packages: https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/packages.md
- pi extensions: https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/extensions.md (ToolDefinition in `src/core/extensions/types.ts`; `pi.exec(command, args, { signal, timeout, cwd })` → `{ stdout, stderr, code, killed }`)
- pi skills: https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/skills.md
- Existing Claude Code plugin: `.claude-plugin/marketplace.json`, `plugins/ketch/`
- MCP tool params/descriptions to mirror: `mcp/`

## Log
- 2026-10-19T22:01:02.647Z: [claude] Done on branch dispatch/task-9 (commit bd3cc21). Root package.json is the pi manifest (private, pi-package keyword, skills → ./skills/ketch, extensions → ./plugins/pi/extensions/ketch.ts; @earendil-works/pi-coding-agent + typebox as peerDependencies only, .npmrc legacy-peer-deps so npm install pulls just dev typebox). Extension registers ketch_search/code/docs/scrape/crawl/tag over `ketch … --json` via pi.exec, exit codes → [validation]/[not_found]/[upstream]/[precondition]/[cancelled], missing binary → [precondition] install hint (pi.exec swallows ENOENT as a silent exit 1, handled), plus /ketch-doctor. Tag tool uses the MCP param names (operation/tag/urls/limit) rather than action/name, per the mirror-MCP constraint. CLI crawl has no --max-pages: capped client-side (30/100, 3 min) with stopped=max_pages|timeout. 34 node:test cases pass with stubbed pi.exec; go test and golangci-lint (via go run v2.15.0) pass; no Go changes. SKILL.md version moved under metadata; README/MANUAL/CHANGELOG/AGENTS.md/skill transport notes updated. Not verified against a live pi install (pi not available here).
- 2026-10-19T23:49:19.415Z: Sent back from review: I’d fix three issues before merging. All are in `plugins/pi/extensions/ketch.ts`: - **P1 — CLI flag injection (lines 389–462).** User inputs are passed as positional arguments without `--`. A search for `--help` runs help instead of searching; a batch URL entry can introduce flags such as `--cookie-file=…` that aren’t exposed by the tool schema. Build options first, then append `--` and positional inputs. This isn’t shell injection, but it still lets input alter command behavior. - **P2 — URL-only scrape accepts local file input (lines 425–426, 582–588).** Passing `url: "./urls.txt"` triggers the CLI’s file autodetection: it reads that file as a URL list. A single-element `urls` array does the same. Explicitly serialize the URL list as the CLI’s JSON-array input so individual strings undergo URL validation rather than file autodetection. Adding `--` alone won’t fix this. - **P2 — Crawl filters change meaning when they contain commas (lines 280–282, 443–444).** Cobra parses `--allow` and `--deny` values as CSV. A regex like `/archive/[0-9]{1,4}/` gets split into two patterns. CSV-encode these values and test commas and quotes against actual CLI parsing.
- 2026-10-19T23:54:28.657Z: [claude] Review fixes on branch dispatch/task-9 (commit ee06429). P1: flag values are bound with =, and positional inputs follow --, so --help or --cookie-file=… are treated as input. P2: scrape always sends the URL list as one JSON-array arg, so ./urls.txt is validated as a URL and never read as a file. P2: crawl allow/deny are CSV-quoted, so commas and quotes survive. Also replaced cobra's "Error: " prefix with the taxonomy prefix. Added ketch.cli.test.ts, which checks all three against a freshly built binary and a local HTTP server; it skips without Go. npm test passes 39/39, as do go test, golangci-lint and the other contract checks.
