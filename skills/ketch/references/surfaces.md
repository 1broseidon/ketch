# Surfaces — flags, params, and per-surface behavior

Verified against ketch v0.11.0 (main). Discipline 6 applies: `--help` and `ketch config` outrank this file.

---

## CLI ↔ MCP name mapping

The two transports expose the same options under different spellings. Both directions:

| CLI | MCP | Notes |
| --- | --- | --- |
| `--regex` | `regexp` | code; grepapp/sourcegraph only |
| `--select <css>` | `selector` | scrape; skips readability; incompatible with `raw` |
| positional URLs / JSON array / file / stdin | `url` (one) or `urls` (array) | CLI auto-detects the input form; MCP is explicit |
| `--searxng-url` | `searxng_url` | search, searxng backend only |
| `--multi[=list]` | `multi` (array; `["all"]` = every usable) | search; federated RRF search, mutually exclusive with `backend`; CLI needs the `=` form for a list |
| `--resolve` | `resolve` | docs |
| `--no-llms-txt` | `no_llms_txt` | scrape |
| `--force-browser` | `force_browser` | scrape |
| `--max-chars` | `max_chars` | scrape / search-with-scrape; crawl has it on MCP only |
| `--no-cache` | `no_cache` | scrape, crawl |
| `--concurrency` (scrape, default 5) | `concurrency` (capped at 16) | crawl's `--concurrency` (default 8) is CLI-only |
| — | `max_pages` | crawl, MCP only: default 30, cap 100; CLI crawl bounds with `--depth`/`--allow`/`--deny` |
| `--minimal`, `--json`, `--background` | — | CLI-only; MCP output is already structured |

---

## search

- Backends: `brave` (default when unconfigured; free API key), `ddg` (zero setup; rate-limits readily under fan-out), `searxng` (self-hosted; needs a JSON-enabled instance — see the setup verb), `exa` (zero config), `firecrawl` (Firecrawl v2 search API; hosted needs `firecrawl_api_key`, self-hosted via `firecrawl_url`), `keenable` (keyless by default; optional `keenable_api_key` lifts the rate limit), `tavily` (keyed; extracted content in results; `tavily_api_key`), `parallel` (zero config; hosted Search MCP).
- The effective default backend is operator-configured: **omit `backend` to use it**; `ketch config` shows which it is.
- `--scrape` / `scrape: true` fetches each result's full content — budget it exactly like a scrape (`max_chars`, `trim`).
- `--minimal` (CLI): one result per line, tab-separated url/title/snippet (a 4th backends column is appended under `--multi` for plain search; `--scrape --minimal` keeps 3 columns).
- `--multi` / `multi: [...]`: federated search — query several backends at once and rank-fuse with Reciprocal Rank Fusion (k=60), deduplicating by URL. Bare `--multi` / `["all"]` = every usable backend (key-presence rule); `--multi=brave,exa` / `["brave","exa"]` = a set (use the `=` form on the CLI). Each result gains a `backends` list (the engines that returned it — a consensus signal worth more than any single float). Backends that error or time out (10s each) are dropped: on the CLI they surface as `warn:` stderr lines + a `failed:` frontmatter key; on MCP as an additive `errors` map. The call fails ([upstream]/exit 4) only when every backend fails. Mutually exclusive with `backend`. Keys improve federation reliability (keyless `ddg`/`exa`/`keenable` rate-limit faster under fan-out).

## code

- Backends: `grepapp` (default; keyless, public OSS repos via grep.app), `sourcegraph` (keyless), `github` (auth via `gh auth login`, `$GITHUB_TOKEN`, or `ketch config set github_token <tok>`).
- `lang` is appended to the query as a language filter.
- `regexp` / `--regex`: grepapp and sourcegraph only. github rejects it — `[validation]` / exit 2 with a pointer to the backends that support it.
- grepapp intermittently returns 504 (`[upstream]`); an immediate single retry usually succeeds.

## docs

- Backend: `context7` — curated, version-aware snippets; free key via `ketch config set context7_api_key <key>`. A `local` backend is planned but unimplemented; selecting it is `[precondition]` / exit 5.
- Two-step usage: `resolve` → vet → `library`.
  - Resolve row shape: `/org/repo  Name  (snippets: N, trust: X)`.
  - **Resolve never returns empty.** A garbage query returns confident fuzzy matches, some with trust 8–10. Trust scores the source, not the match — vet that the *name* is the library you meant before fetching by ID.
- `tokens` (default 4000) is the docs token budget; the default returns ~3.3 KB.

## scrape

- **llms.txt:** a bare-domain URL is auto-probed for `/llms.txt` first and may silently return that file instead of the homepage. The `title` in the output reveals the swap. `no_llms_txt` / `--no-llms-txt` opts out.
- Input forms (CLI, auto-detected — no batch flag): single URL, multiple positional args, a JSON array string, a file of URLs, a stdin pipe. MCP: exactly one of `url` or `urls`.
- Batch scrapes run concurrently (CLI default 5; MCP `concurrency` capped at 16). Per-URL failures come back as `results[].error` **inside a successful call** (`isError=false`; CLI `--json` returns an array) — check every entry.
- `max_chars` truncates output and appends `[truncated]`. `trim` strips markdown syntax, keeps text (incompatible with `raw`). `--select`/`selector` extracts by CSS selector, skipping readability — no match is `[not_found]` / exit 3. `raw` returns HTML instead of markdown.
- JS-rendered pages: JS-shell detection falls back to the configured headless browser automatically, same output shape. `force_browser` skips detection and errors without a configured browser (`[precondition]`).
- Fetches are cached (see Cache below); `no_cache` bypasses.
- Already hold the HTML? `curl -L <url> | ketch extract` runs the same readability + markdown pipeline with no fetch, cache, or browser (CLI-only; supports `--url`, `--select`, `--trim`, `--max-chars`).

## crawl

- Same-host BFS from a seed URL (`--sitemap` treats the seed as a sitemap). Streams pages as found.
- **MCP:** synchronous and bounded — `max_pages` default 30, hard cap 100, 3-minute wall clock. Partial results return with `stopped: "max_pages" | "timeout"`. Per-page `max_chars` available.
- **CLI:** `--depth` (default 3), `--allow` path substrings, `--deny` regexes, `--concurrency` (default 8). No page-cap flag — bound with depth and filters.
- **Background mode is CLI-only:** `ketch crawl <url> --background` returns a crawl ID; `ketch crawl status [id]` and `ketch crawl stop <id>` manage it.
- A CLI crawl interrupted by SIGINT exits **0** with the partial results already streamed — by design, not an error.

---

## Backends and keys at a glance

| Surface | Keyless | Keyed | Set with |
| --- | --- | --- | --- |
| search | ddg, searxng (self-hosted), exa, keenable, parallel | brave, firecrawl, tavily | `ketch config set brave_api_key <key>` / `firecrawl_api_key` / `tavily_api_key` |
| code | grepapp, sourcegraph | github | `gh auth login` / `$GITHUB_TOKEN` / `ketch config set github_token <tok>` |
| docs | — | context7 (free key) | `ketch config set context7_api_key <key>` |

A missing-key call fails with `[precondition]` / exit 5 and an error message that names the fix (brave's includes the signup URL and the exact `config set` command).

---

## Config

- File: `~/.config/ketch/config.json`. Flags always override config values.
- `ketch config` is the one discovery call: effective settings plus `available_backends`, `available_code_backends`, `available_doc_backends`, as JSON. Never probe env vars instead.
- **Blind spots:** older builds do not report whether search/docs API keys are set (`github_token_source` is the exception; newer builds add key-presence booleans like `brave_api_key_set`), and no build reports reachability. To know a surface works, probe it — `ketch doctor` when available, else the setup verb's probe table.
- Keys (from README and `ketch config` output): `backend`, `code_backend`, `docs_backend`, `limit`, `searxng_url`, `sourcegraph_url`, `brave_api_key`, `context7_api_key`, `github_token`, `exa_api_key`, `firecrawl_api_key`, `firecrawl_url`, `keenable_api_key`, `tavily_api_key`, `browser`, `cache_ttl`, `url_rewrites`, `spa_markers`.
- `KETCH_CONFIG` is **not** supported. For test isolation, override `HOME` / `XDG_CONFIG_HOME`.

## Cache

- Page cache in bbolt at `~/.cache/ketch/cache.db`, default TTL 72h (`cache_ttl` overrides). `ketch cache` shows stats, `ketch cache clear` empties it; both take `--json`.
- Single-process lock: the bbolt DB admits one process at a time. A long-running MCP server holds the lock for its whole lifetime, so concurrent CLI scrapes silently run cache-disabled — every fetch goes to the network. Observed live: `ketch doctor` reports `cache … locked by another process` while the server runs, and `ketch cache` shows `locked: true`. Needing heavy CLI and MCP use long-term → prefer CLI-only, or accept the tradeoff knowingly.
