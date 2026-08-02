---
name: ketch
description: "Research skill for ketch — a fast stateless CLI for web search, OSS code search, curated library docs, page scraping, and site crawling; an optional MCP server exists for operators who want it, but the CLI is the primary interface. Use when a question needs live sources: 'research X', 'what are people saying about Y', 'find docs or real-world examples for Z', 'scrape/crawl this site' — or when installing or configuring ketch backends. Routes search vs code vs docs vs scrape vs crawl, keeps every fetch inside a token budget, turns error prefixes into control flow, and produces cited syntheses. Not for local codebase search, private repos, or pages behind auth."
version: 0.1.0
---

# Ketch

Route every live-source question to one of ketch's five research surfaces — search, code, docs, scrape, crawl — over the transport the operator gave you, with a token budget on every fetch and a source URL on every claim. ketch is one stateless binary — call, result, exit — with web search, OSS code grep, curated library docs, and page/site extraction together, so a complete research pipeline needs no other tool and no daemon.

## Transport: stateless CLI by default, MCP when the operator wired it

The CLI is ketch's identity: call → result → exit, `--json` on every call, exit codes as control flow, zero daemon. That is the default transport and the zero-infrastructure path. The MCP server is a supported alternative for operators who want it — never a prerequisite.

Decide once per session, before the first call:

1. `which ketch` succeeds → the CLI is your transport: `--json` on every call, exit codes as control flow.
2. Also check for ketch's five MCP tools in your tool list — `search`, `code`, `docs`, `scrape`, `crawl` from a server named `ketch` (in Claude Code: `mcp__ketch__search`, …). Present → the operator wired them up on purpose, and using them for research calls is correct and good: structured output, per-URL errors, no shell round-trip. Do not shell out around tools the operator set up.
3. Both live → either transport serves research calls, but know the tradeoff: a running MCP server holds the single-process page-cache lock, so concurrent CLI scrapes silently run cache-disabled.
4. Neither CLI nor MCP tools → ketch is not installed. Offer `brew install 1broseidon/tap/ketch` or `go install github.com/1broseidon/ketch@latest` — an operator action: propose, wait for confirmation.

The rule: **use the transport the operator gave you** — when both are live, either is fine for research calls, and operator actions are always CLI.

Config discovery is CLI regardless of transport: `ketch config` prints effective settings and available backends as JSON; there is no config tool over MCP. Operator actions — `config set`, `cache`, `browser install`, `crawl --background`/`status`/`stop`, `doctor` — are deliberately not in MCP. They are always CLI.

## Glossary

Use only these terms in ketch output.

| Term | Meaning |
| --- | --- |
| **surface** | One of the five research operations: `search`, `code`, `docs`, `scrape`, `crawl` |
| **transport** | How a surface is called: the CLI binary (default) or the optional MCP tools |
| **backend** | The provider behind a surface: brave/ddg/searxng/exa/firecrawl/keenable/tavily (search), grepapp/sourcegraph/github (code), context7 (docs) |
| **operator action** | A system-managing or diagnostic command — `config set`, `cache`, `browser install`, background crawls, `doctor` — CLI-only by design |
| **error prefix** | The stable class on every ketch error: CLI exit codes 2–6, mirrored as the bracketed prefix opening every MCP tool error — `[validation]`, `[not_found]`, `[upstream]`, `[precondition]`, `[cancelled]` |
| **fan-out** | How many queries are searched and URLs scraped under one plan |
| **token budget** | The per-call output bound: `max_chars`/`trim` on scrapes, `tokens` on docs, `limit`/`--minimal` on lists |
| **probe** | One cheap read-only call that tests whether a surface is configured and reachable |

## How to use this skill

- Default: answer one question with one or two routed calls. Use the surface routing table, token budgets, and error control flow below.
- `ketch research <question>`: deep multi-source research — search fan-out → scrape top hits → optional code/docs corroboration → synthesized, cited answer. Read `references/verbs/ketch-research.md`.
- `ketch setup`: configure backends with the operator — probe current state, propose exact commands, mutate only on confirmation. Read `references/verbs/setup.md`. Enter this verb whenever any call returns `[precondition]` / exit 5.

One question = one plan. Escalate a default run into `ketch research` when the first search shows the answer is contested, multi-part, or needs corroboration.

## Non-negotiable disciplines

1. **Use the transport the operator gave you.** The CLI is the default; MCP tools in your list mean the operator opted in — use them for research rather than shelling out around them. When both are live, either serves research calls (a running MCP server holds the page-cache lock, so concurrent CLI scrapes run uncached); operator actions — config, cache, browser, background crawls, doctor — are always CLI.
2. **Bound every fetch.** `max_chars` 4000–8000 plus `trim` on any scrape of a page you have not seen — an unguarded page can cost ~25k tokens. Skipping the cap requires a stated one-line reason ("known ~200-word page").
3. **Cite every claim.** A research synthesis without source URLs is not a deliverable.
4. **Error prefixes are control flow.** Classify before reacting. Never retry `[validation]` or `[not_found]` unchanged.
5. **Propose, then mutate.** `config set`, `browser install`, docker runs, installs — only after the operator confirms the exact command. Never touch a value that is already configured and working.
6. **The binary outranks this file.** `ketch config` and `--help` are ground truth; where they disagree with a table here, trust the binary and flag the skill as drifted.

## Gold decision trace

```text
Request: "ketch research — do people actually use Go's iter.Seq in real projects, and what are the gotchas?"

Transport: operator wired mcp__ketch__* into this session → honor it; research calls go over MCP.
Plan: 2 queries · scrape top 3 · max_chars 6000 + trim · ≤8 calls

search {query: "Go iter.Seq real-world experience gotchas", limit: 5}
  → "[upstream] ddg rate limited" → rotate to next entry in available_backends,
    retry once: search {query: ..., backend: "brave", limit: 5} → ok
search {query: "Go range-over-func adoption production", backend: "brave", limit: 5}
  → 10 results, 8 unique hosts → picked 3: official blog post, one experience
    report, one issue thread (primary sources over aggregators)

scrape {urls: [u1, u2, u3], max_chars: 6000, trim: true}
  → isError=false; checked results[] one by one: u1, u2 ok;
    u3.error = "[upstream] … 503" → dropped, will be named in synthesis

code {query: "iter.Seq", lang: "go", limit: 3}      # corroborate real usage
  → 3 repos with file/line URLs

Synthesis: five claims, each cited to its URL; u3 listed as unretrieved;
one conflict between u1 and u2 stated and attributed, not averaged.
Budget: 5 of 8 calls (the rate-limited attempt counts).
```

## Surface routing

First match wins:

| The question needs | Surface | Not |
| --- | --- | --- |
| Current web pages, opinions, news, comparisons | `search` | `docs` — that is curated library docs only |
| How real projects call an API | `code` | `search` — blogs talk *about* code; `code` greps public OSS repos via grep.app |
| A library's own documentation, version-aware | `docs` | `scrape` of the docs site — `docs` is already extracted and token-budgeted |
| The content of a URL you already hold | `scrape` | `search` — never re-find a known URL |
| Many pages from one site | `crawl` | looped `scrape` — crawl dedupes, bounds, and streams |

In reverse: `search` finds URLs; `scrape` reads them; `crawl` reads a site; `code` reads public source; `docs` reads library docs. `search` with `scrape: true` fuses the first two when you will want full content from every hit — budget it like a scrape.

## Token budgets

| Call | Bound with | Measured cost |
| --- | --- | --- |
| `search`, limit 5 | `limit` | ~1.4 KB |
| `code`, limit 3 | `limit` | ~0.7 KB |
| `docs`, default budget | `tokens` (default 4000) | ~3.3 KB |
| `scrape`, unknown page | `max_chars` 4000–8000 + `trim` | unguarded: up to ~100 KB (~25k tokens) |
| `crawl` (MCP) | `max_pages` + per-page `max_chars` | 30 pages default, 100 cap, 3-min wall clock |
| Any CLI list | `--minimal` | roughly halves output |

## Error control flow

| Exit (CLI) | Prefix (MCP) | Meaning | Do |
| --- | --- | --- | --- |
| 2 | `[validation]` | Bad input | Fix the call; retrying unchanged can never succeed |
| 3 | `[not_found]` | Nothing matched | Change the query or selector; not an outage |
| 4 | `[upstream]` | Backend or network failure | Rotate backend (`available_backends` in `ketch config`) or retry once |
| 5 | `[precondition]` | Operator config missing | Stop researching; enter `ketch setup` |
| 6 | `[cancelled]` | Cancelled or timed out | Rerun with smaller scope |

Situations → class: unknown backend, `regexp` on github → `[validation]`. Selector matched nothing → `[not_found]`. ddg rate limit (it rate-limits readily under fan-out), DNS failure, grepapp's intermittent 504 → `[upstream]`, rotate or retry once. Missing API key, docs backend `local` (planned, unimplemented), `force_browser` with no browser configured → `[precondition]`. One asymmetry: a CLI `crawl` interrupted by SIGINT exits **0** with partial results, by design.

## Gotchas

Detail for each lives in `references/surfaces.md`.

- Scraping a **bare domain** auto-probes `/llms.txt` and may silently return that instead of the homepage — the `title` field reveals the swap; `no_llms_txt` opts out.
- `docs` is a two-step: `resolve` the name → **vet the matches** → fetch by `library` ID. Resolve never returns empty — garbage in gets confident fuzzy matches out, so check the name, not just the trust score.
- Batch scrape reports per-URL failures inside a successful call: `isError=false` with `results[].error` set. Check every entry.
- `regexp` works on grepapp and sourcegraph only; github rejects it with a pointer to those backends.
- Background crawls (`--background`, `status`, `stop`) are CLI-only; the MCP `crawl` is synchronous and capped.
- The page cache (bbolt, 72h default TTL) is single-process: a long-running MCP server holds the lock, so concurrent CLI scrapes silently run cache-disabled — `ketch doctor` reports the cache as locked by another process. Running the server degrades the CLI; prefer CLI-only when both would run long-term.

## BAD/GOOD contrasts

**BAD:** `scrape {url: "https://docs.example.com"}` — no bound; you get llms.txt or ~25k tokens, whichever is worse.
**GOOD:** `scrape {url: "https://docs.example.com/quickstart", max_chars: 6000, trim: true}` — plus `no_llms_txt: true` when you want the page itself, not the site's llms.txt.

**BAD:** Telling a user they must run an MCP server to use ketch with agents — the CLI plus a prompt block is the zero-infrastructure path, and a long-running server holds the page-cache lock against every CLI call.
**GOOD:** CLI by default; MCP when the operator wired it — and when `mcp__ketch__*` tools are in your list, use them for research instead of shelling out around the operator's setup.

**BAD:** `[upstream] ddg rate limited` → retry the identical call three times.
**GOOD:** Rotate — `backend: "brave"` (or the next entry in `available_backends`) — retry once, and note the swap.

**BAD:** Fetch docs from resolve's first match because its trust score is high, even though its name is not the library you asked about.
**GOOD:** Vet name + snippet count + trust; if no match names the intended library, say so instead of fetching junk docs.

## Reference loading

- `ketch research …` → read `references/verbs/ketch-research.md` before starting.
- `ketch setup`, any `[precondition]`/exit 5, or an install → read `references/verbs/setup.md`.
- Full flag/param tables, CLI↔MCP name mapping, backend/key matrix, or a surface behaving oddly → read `references/surfaces.md`.

## Scope

In scope: the five research surfaces over both transports, the research and setup verbs, token budgets, error-prefix control flow, backend configuration. Out of scope: local or private codebase search (use repo tools), pages behind auth or paywalls, bulk archival crawling beyond the caps, browser automation beyond ketch's headless-rendering fallback.

Bound every fetch; cite every claim.
