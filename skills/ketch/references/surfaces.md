# Surfaces — flags, params, and per-surface behavior

Verified against ketch v0.18.0 (main). Discipline 6 applies: `--help` and `ketch config` outrank this file.

---

## CLI ↔ MCP name mapping

The two transports expose the same options under different spellings. Both directions:

In pi, the package's tools are the MCP tools under a `ketch_` prefix — `ketch_search`, `ketch_code`, `ketch_docs`, `ketch_scrape`, `ketch_crawl`, `ketch_tag` — with the MCP option names below; they shell out to the CLI, so `max_pages` is applied client-side there.

| CLI | MCP | Notes |
| --- | --- | --- |
| `--regex` | `regexp` | code; grepapp/sourcegraph only |
| `--select <css>` | `selector` | scrape; skips content selection; incompatible with `raw` |
| positional URLs / JSON array / file / stdin | `url` (one) or `urls` (array) | CLI auto-detects the input form; MCP is explicit |
| `--searxng-url` | `searxng_url` | search, searxng backend only |
| `--multi[=list]` | `multi` (array; `["all"]` = every usable) | search; federated RRF search, mutually exclusive with `backend`; CLI needs the `=` form for a list |
| `--resolve` | `resolve` | docs |
| `--section <url#anchor>` | `section` | docs; reads one section, no query: from the local index, or for a page a search read, through the page cache |
| `--page N` | `page` | docs, with `section`: the page of a section longer than `tokens` (pi: none) |
| `--offline` | `offline` | docs; no check of the cited pages of an indexed answer (pi: none) |
| `--no-llms-txt` | `no_llms_txt` | scrape |
| `--force-browser` | `force_browser` | scrape |
| `--max-chars` | `max_chars` | scrape / search-with-scrape; crawl has it on MCP only |
| `--no-cache` | `no_cache` | scrape, crawl |
| `--tag <name>` | `tag` | every surface: search, code, docs, scrape, crawl; composes with `--no-cache` |
| `ketch tag add/show/list/remove` | `tag` tool, `operation` enum | the CLI uses verbs, MCP takes `operation: add\|show\|list\|remove` |
| `--concurrency` (scrape, default 5) | `concurrency` (capped at 16) | crawl's `--concurrency` (default 8) is CLI-only |
| — | `max_pages` | crawl, MCP only: default 30, cap 100; CLI crawl bounds with `--depth`/`--allow`/`--deny` |
| `--minimal`, `--json`, `--background` | — | CLI-only; MCP output is already structured |

---

## search

- Backends: `auto` (**the default**; not a provider but a fallback chain — tries configured instances, then keyed providers, then the keyless ones `parallel` → `exa` → `keenable` → `youcom` → `firecrawl` → `ddg`, returning the first that answers. The reported `backend` names the provider that actually served, and providers that failed on the way appear as `warn:` stderr lines / the MCP `errors` map. Cannot be used inside `--multi`/`--random`), `brave` (free API key), `ddg` (zero setup; rate-limits readily under fan-out), `searxng` (self-hosted; needs a JSON-enabled instance — see the setup verb), `exa` (zero config), `firecrawl` (Firecrawl v2 search API; keyless by default, optional `firecrawl_api_key` lifts the cap; self-hosted via `firecrawl_url`), `keenable` (keyless by default; optional `keenable_api_key` lifts the rate limit), `tavily` (keyed; extracted content in results; `tavily_api_key`), `tinyfish` (keyed; titles, URLs, snippets; `tinyfish_api_key`), `parallel` (hosted Search MCP; keyless by default, optional `parallel_api_key` lifts the rate limit), `serpbase` (keyed Google results; `serpbase_api_key`), `serply` (keyed Google results, ten per page; `serply_api_key`), `youcom` (You.com web search; keyless by default, optional `youcom_api_key` lifts the rate limit).
- The effective default backend is operator-configured: **omit `backend` to use it**; `ketch config` shows which it is.
- `--scrape` / `scrape: true` fetches each result's full content — budget it exactly like a scrape (`max_chars`, `trim`).
- `--minimal` (CLI): one result per line, tab-separated url/title/snippet (a 4th backends column is appended under `--multi` for plain search; `--scrape --minimal` keeps 3 columns).
- `--multi` / `multi: [...]`: federated search — query several backends at once and rank-fuse with Reciprocal Rank Fusion (k=60), deduplicating by URL. Bare `--multi` / `["all"]` = every usable backend (key-presence rule); `--multi=brave,exa` / `["brave","exa"]` = a set (use the `=` form on the CLI). Each result gains a `backends` list (the engines that returned it — a consensus signal worth more than any single float). Backends that error or time out (10s each) are dropped: on the CLI they surface as `warn:` stderr lines + a `failed:` frontmatter key; on MCP as an additive `errors` map. The call fails ([upstream]/exit 4) only when every backend fails. Mutually exclusive with `backend`. Keys improve federation reliability (keyless `ddg`/`exa`/`firecrawl`/`keenable`/`parallel` rate-limit faster under fan-out).

## code

- Backends: `grepapp` (default; keyless, public OSS repos via grep.app), `sourcegraph` (keyless), `github` (auth via `gh auth login`, `$GITHUB_TOKEN`, or `ketch config set github_token <tok>`).
- `lang` is appended to the query as a language filter.
- `repo` / `--repo owner/name` (a GitHub URL also works) searches one repository, exact on every backend. Prefer it to writing `repo:` into the query: grepapp matches the query as literal code, so a typed `repo:`/`lang:` is searched as text and returns a `[validation]` warning (CLI: `warn:` on stderr, `warning.code: literal_qualifier` under `--json`).
- A repository the backend lacks: sourcegraph and github → `[not_found]` / exit 3 naming the other backends. grepapp indexes a subset of public repos, so an empty `repo` search there comes back with a `[not_found]` warning (`repo_may_be_unindexed`), not an error. Try another backend before concluding the code does not exist.
- `regexp` / `--regex`: grepapp and sourcegraph only. github rejects it — `[validation]` / exit 2 with a pointer to the backends that support it.
- grepapp intermittently returns 504 (`[upstream]`); an immediate single retry usually succeeds.

## docs

- Backends: `search` (default) — keyless; answers from the docs themselves. A library the operator indexed (`ketch docs index`) that the question or `library` names answers from the local index. Otherwise it web-searches the question (through the search surface's providers), picks the documentation site the hits agree on (a site the question names, or a spec body's site, first; a site's main docs over enterprise/legacy/pinned-release trees), reads up to 24 of its pages and answers from them, held in memory. A question saves nothing: the pages stay in the page cache for `cache_ttl`, and the same question again within it (same terms, same `library`), answered or refused, does no search and no fetch. Only `ketch docs index` adds libraries or pages. `context7` — curated, version-aware snippets; works with no key on Context7's anonymous quota (about 200 requests a month per IP, shared by everything on that address; a bare search spends two), and a free key lifts it: `ketch config set context7_api_key <key>`. A spent quota is `[upstream]` / exit 4 naming the reset time and that command — do not retry before the reset; drop `backend` to use search. A library Context7 has moved (`/facebook/react` → `/react/react`) is followed once and results carry the new ID. The operator picks the default (`docs_backend`); there is no `web`, `auto` or `local` backend — the local index is part of search.
- Search answers are evidence and a verdict, about 1,800 tokens in all, frontmatter included (or `tokens`); `limit` caps the groups. The fields, in the CLI frontmatter and as `--json` / MCP fields:
  - `index`: `full` or `capped`, pages, `built` date; for pages a search read, `source: search, N pages read` (`index.source`, `pages_read`), `, cached` (`cached: true`) on a repeat from the page cache.
  - `stale`: `unverified, built <date>` when a cited page older than `cache_ttl` could not be read again the way it was stored (no network, a timeout, a check that failed in the last 15 minutes, or `offline`). Absent otherwise.
  - `library`: the root the answer was made from; `scope`: the URL below it the answer was limited to, when `library` named one (see `library` below).
  - `status`: `found` (evidence matching the question; not a promise that it answers); `partial` with `reason` `terms_not_found`, `terms_outside_scope` and/or `partial_site` (pages a search read; `note` names `ketch docs index <root>`); `refused`, no results, CLI exit 3, over MCP a result, not a tool error: `terms_not_found` (`not_found` is the answer), `terms_outside_scope` (the library holds the terms, but not under `scope`), `scope_empty` (no indexed page is under `scope`; `note` names the root).
  - `terms`: the question's rare terms as written, with the library's sections holding each (the whole library's, with `scope` too); `not_found`: those no section of the library holds; `outside_scope`: those the library holds but no section under `scope` does.
  - `defined_in`: for a listed term that is the first cell of a table row in a section the answer does not print, and that section names another of the question's rare terms (when it has one), that section (CLI `defined_in: {term: url#anchor}`; JSON/MCP `[{term, section}]`; at most three).
  - `indexed_near`: when `library` named no indexed library, the indexed ones near it (one to five: those on the URL's host, else those its path names, so `github.com/owner/repo` lists the project's docs and not the code host's, else those whose name, title or root holds the value or one of its words), answered from none of them (CLI `indexed_near: {title: root}`, ending with a line saying to pass one as `--library`; JSON/MCP `[{title, library}]`, MCP `hint` says the same). A refusal or failure of such a call names them at the end of its error.
  - Results, one per section group, best first, a page's together: `page_title`, `heading_path`, `url`, `section` (the reference for `--section`), `rows`, `same_text`; a cut group has `truncated`, `shown_tokens` of `section_tokens` (CLI `(cut, N of M tokens)` on its `source:` line) and `contents` (CLI `also in this section: …`). A result names no backend: the answer's `backend` does, once. `…` in a group's text marks text left out. A cut group prints a code block with the sentence that introduces it (one ending in a colon), a sentence opening with `Or`, `Otherwise`, `This` or `The above` with the one before it, and a signature or a term with its type with the description under it.
  - `hint`, when a group was cut: how many are, that their hidden text is unread, and the call that reads it, to run before stating what it says. CLI text: last line `N groups above are cut and their hidden text is unread: run ketch docs --section <its source> before stating what it says`; `--json` `hint`: `N results are truncated and …: run ketch docs --section <its section> …`; MCP `hint`: `… read docs(section="<its section>") …`. With `indexed_near` the MCP and `--json` hint add its line after `; `.
  - Two rules of use: text behind `…` or in a cut group is unread (read it with `section` before stating what it says, or say you did not read it); whatever an answer says that no result prints is the caller's own knowledge, labelled so, never cited to the docs.
  - A code block is printed as stored: HTML in it is the example's own (a parser's output, a template), never stripped.
- **Read a cut result with `section`, don't re-ask with more tokens.** Pass a result's `section` (or a `defined_in` target) with no query: it returns the section and its subsections (an H2 is its whole family; a page URL alone, the page's sections in order) from the local index, or for a page a search read, through the page cache (read again from the site after `cache_ttl`). Whole when it fits `tokens` (default 4000), else a page of that size at a time, broken at a block boundary: `page: N of M`, then `next page: …` (`page=N`; a page past the last is `[validation]`). Its frontmatter is an answer's lines about the source (`backend`, `library`, `index:` or `source:`, `stale:`, `page:`), no verdict; `--json` and MCP give the same object as an answer, `results` holding the section. Its text is cleaned as an answer's is (`pam\_duo` prints `pam_duo`). An unknown page or anchor is `[not_found]`; a malformed reference or `backend: context7` with `section` is `[validation]`.
- `library` / `--library`: Context7 takes its `/org/repo` ID. Search takes one spec, as a preference among the sites search finds (a docs URL: that site is read):
  - a docs root as `resolve` lists it. A URL below an indexed root narrows the answer to the pages at or under it (a page URL: that page), printed as `scope:` with the root as `library`; no indexed page under it is `refused (scope_empty)`, never widened. To read one page whole, use `section`. A docs URL no index holds is read as given (`https://docs.astro.build`); it already names its version, so `<url>@x` is `[validation]`; one below the site's docs tree is searched within and printed as `scope:`. A URL without its scheme (`auth0.com/docs`) that no name or package matches is compared with the indexed roots as the URL it names;
  - an indexed library's name: its site's name (`Vue Router`), that name without words such as Project, Community, Web Framework or Developer (`django` for Django Project), its package names, or its host's one distinctive label (`duo` for duo.com). A library of one page (a specification indexed at its own address) also goes by its document's title, the acronym the document spells out for that title (`jwt`, `pkce`) and a name in it with its version (`oauth 2.0`, `oauth2`). One word of a longer name is not a name (`vue` is not Vue Router; `oauth` names neither of two OAuth RFCs), nor is a vendor's name for a product it documents under its own path (`hashicorp` is not Terraform);
  - `[eco:]name[@version]` — `click`, `django@5.2`, `next@14`, `pypi:click`, `npm:@astrojs/starlight@0.30`; prefixes `npm:` `pypi:` `crates:`/`cargo:` `go:` `github:`; a bare name is looked up in npm, PyPI and crates.io;
  - `owner/repo` (a GitHub repository), or a Go module path (`github.com/spf13/cobra`).
  - `@version`: exact label, alias (`latest`/`stable`, `dev`/`main`), or a prefix that picks the newest release under it (`5` → newest `5.x`), never across a major.
- Without `library`: search picks the site from the question's hits (and an indexed library the question names); Context7 resolves the whole query.
- `resolve`: Search answers from the local index first, with no network: the indexed libraries whose root, title (for a library of one page, its document's), site name, names, aliases or package specs hold the query (case aside; failing that, one of its words of three letters or more), every one with no query (`ketch docs --resolve`, MCP `resolve=true` alone): `<root>  <title>  (indexed: yes, full|capped, N pages, built <date>)`, `--json`/MCP `indexed: true` and `index`. Only a name none matches goes to the web resolution below, and only a name without spaces: a name of several words none matches (compared also as one key: `vercel ai sdk` holds AI SDK) is `[not_found]`, since registries take package names; its message says that `resolve` with no name lists every indexed library. Context7 — `/org/repo  Name  (snippets: N, trust: X)`. **Context7's resolve never returns empty.** A garbage query returns confident fuzzy matches, some with trust 8–10. Trust scores the source, not the match — vet that the *name* is the library you meant before fetching by ID. Search — `<docs root URL>  Name  (trust: X, version: V, versions: a b c)`, then indented `packages:` and `evidence:`; `--json` and MCP `matches` add `version`, `versions`, `evidence` and `packages`. Only verified matches (the registry entry or repository names the site, the package's docs.rs/pkg.go.dev docs, or a site on the project's domain linking back to its repository); several projects answering to a name are all listed. A GitHub rate limit shows up as an evidence note, not a failure.
- Search errors: `[not_found]` / exit 3 — no documentation among the hits (`no_docs_found`), none of the pages readable, `robots.txt` disallows them (`disallowed`), nothing matched (`no_results`). `[validation]` / exit 2 — the hits split between docs sites (`ambiguous_site`): pass the one you mean as `library` (a docs URL). `[upstream]` / exit 4 — every web search failed or came back empty (`search_empty`: the search providers are throttling), or no answer within 45 s. An empty answer is never a success, on either backend: it is `[not_found]` (`no_results`).
- `tokens`: leave it unset unless a result is too long to read. Search answers in about 1,800 tokens unless set (`tokens: 880` fits four or five compact results); Context7's default is 4000 (~3.3 KB).

## scrape

- **llms.txt:** a bare-domain URL is auto-probed for `/llms.txt` first and may silently return that file instead of the homepage. The `title` in the output reveals the swap. `no_llms_txt` / `--no-llms-txt` opts out.
- Input forms (CLI, auto-detected — no batch flag): single URL, multiple positional args, a JSON array string, a file of URLs, a stdin pipe. MCP: exactly one of `url` or `urls`.
- Batch scrapes run concurrently (CLI default 5; MCP `concurrency` capped at 16). Per-URL failures come back as `results[].error` **inside a successful call** (`isError=false`; CLI `--json` returns an array) — check every entry.
- `max_chars` truncates output and appends `[truncated]`. `trim` strips markdown syntax, keeps text (incompatible with `raw`). `--select`/`selector` extracts by CSS selector, skipping content selection — no match is `[not_found]` / exit 3. `raw` returns HTML instead of markdown.
- JS-rendered pages: JS-shell detection falls back to the configured headless browser automatically, same output shape. `force_browser` skips detection and errors without a configured browser (`[precondition]`).
- Fetches are cached (see Cache below); `no_cache` bypasses.
- Already hold the HTML? `curl -L <url> | ketch extract` runs the same structural extraction + markdown pipeline with no fetch, cache, or browser (CLI-only; supports `--url`, `--select`, `--trim`, `--max-chars`).

## crawl

- Same-host BFS from a seed URL (`--sitemap` treats the seed as a sitemap). Streams pages as found.
- **MCP:** synchronous and bounded — `max_pages` default 30, hard cap 100, 3-minute wall clock. Partial results return with `stopped: "max_pages" | "timeout"`. Per-page `max_chars` available.
- **CLI:** `--depth` (default 3), `--allow` path substrings, `--deny` regexes, `--concurrency` (default 8). No page-cap flag — bound with depth and filters.
- **Background mode is CLI-only:** `ketch crawl <url> --background` returns a crawl ID; `ketch crawl status [id]` and `ketch crawl stop <id>` manage it.
- A CLI crawl interrupted by SIGINT exits **0** with the partial results already streamed — by design, not an error.

---

## tag

Not a research surface: it answers "what did I already find?", not "what is out
there?". No network on any `tag` operation.

`--tag` / the `tag` option works on **every** surface, and one tag holds them
all. Each entry keeps a source URL, bounded title and description, and tagging
time. Descriptions derive from fetched content, code/docs snippets or search
results; full bodies stay in the separate page cache.

| Operation | CLI | MCP |
| --- | --- | --- |
| Tag what a call returned | `--tag <name>` on `search` / `code` / `docs` / `scrape` / `crawl` | `tag` option on those tools |
| Tag URLs directly (cached or not) | `ketch tag add <name> <url>...` | `operation: add`, `urls` |
| Read the index | `ketch tag show <name> --limit N` (`--minimal` for tab-separated) | `operation: show`, `limit` |
| List tags | `ketch tag list` | `operation: list` |
| Drop a tag, or pages from it | `ketch tag remove <name> [url...]` | `operation: remove`, optional `urls` |

`show` returns titles, URLs, one-line descriptions and a `cached` flag per page,
newest first. Both surfaces default to 50 entries; 0 selects all and negatives
are validation errors. `entries`/`cached` count the whole tag, `shown` counts
returned pages. A source can carry several tags. Membership uses the displayed
URL regardless of cookie, User-Agent or rewrite settings.

**The index outlives the cached bodies.** `cache_ttl` defaults to 72h; index
entries have no TTL. An entry with `cached: false` has no confirmed fresh body;
it does not imply a dead link. If `cache_status: unavailable`, warmth could not
be checked because the page cache was locked or unreadable. `scrape` the URL
when you need its content. `ketch cache clear`
drops bodies and keeps the index. Nothing expires the index, so `remove` is the
only way a tag ends.

`add` accepts a URL whose page was never fetched: it is indexed with no title
or description, listed as uncached, and both fill in the first time the page is
seen by any route. The returned `not_cached` array names those — scrape them if
you want the index to describe them now. `add` requires absolute `http(s)` URLs
with a host — anything else is `[validation]` / exit 2, and a batch with one bad
URL tags nothing. `remove` with URLs returns `missing` for the ones that were not
under the tag; a batch that removes nothing is `[not_found]` / exit 3.

`cached: false` is the normal state for entries from `code`, `docs` and
unscraped `search`: those surfaces return snippets, not fetched pages. The
snippet is kept as the entry's description, so the index is still useful
without a round trip.

Errors: `remove` that matches nothing is `[not_found]` / exit 3, including JSON
mode. Explicit tag storage failures are `[precondition]` / exit 5. Optional
bookmark write failures retain research output with CLI stderr diagnostics
(`warning.code: tag_write_failed` under `--json`) or MCP `warnings`.

Storage is `tags.db` under the native configuration directory (Linux XDG config,
macOS Application Support, Windows AppData), with `KETCH_TAGS_PATH` as a complete
filename override. Both tags.db and the page cache open per operation, so a
running MCP server or crawl never locks the CLI out. Cache clear deletes the
page-cache file and leaves bookmarks alone.

---

## Backends and keys at a glance

| Surface | Keyless | Keyed | Set with |
| --- | --- | --- | --- |
| search | **auto (default)**, ddg, searxng (self-hosted), exa, firecrawl, keenable, parallel, youcom | brave, tavily, tinyfish, serpbase, serply | `ketch config set brave_api_key <key>` / `tavily_api_key` / `tinyfish_api_key` / `serpbase_api_key` / `serply_api_key` (optional `firecrawl_api_key` / `parallel_api_key` / `youcom_api_key` lift the hosted caps) |
| code | grepapp, sourcegraph | github | `gh auth login` / `$GITHUB_TOKEN` / `ketch config set github_token <tok>` |
| docs | **search (default;** finds docs sites through the search surface, so search keys help it; `gh auth login` / `github_token` lifts GitHub's 60 req/h limit on name resolution**)**, context7 (anonymous quota, ~200 req/month per IP) | context7 (free key lifts the quota) | `ketch config set context7_api_key <key>`; `ketch config set docs_backend context7` makes Context7 the default |

Search needs no key at all on the default `auto` backend. Naming a keyed backend explicitly without its key fails with `[precondition]` / exit 5 and an error message that names the fix (brave's includes the signup URL and the exact `config set` command).

---

## Config

- File: `~/.config/ketch/config.json`. Flags always override config values.
- `ketch config` is the one discovery call: effective settings plus `available_backends`, `available_code_backends`, `available_doc_backends`, as JSON. Never probe env vars instead.
- **Blind spots:** older builds do not report whether search/docs API keys are set (`github_token_source` is the exception; newer builds add key-presence booleans like `brave_api_key_set`), and no build reports reachability. To know a surface works, probe it — `ketch doctor` when available, else the setup verb's probe table.
- Keys (from README and `ketch config` output): `backend`, `code_backend`, `docs_backend`, `limit`, `searxng_url`, `sourcegraph_url`, `brave_api_key`, `context7_api_key`, `github_token`, `exa_api_key`, `firecrawl_api_key`, `firecrawl_url`, `keenable_api_key`, `parallel_api_key`, `tavily_api_key`, `tinyfish_api_key`, `serpbase_api_key`, `serply_api_key`, `youcom_api_key`, `browser`, `cache_ttl`, `url_rewrites`, `spa_markers`, `cookie_file`, `user_agent`, `extract_mode` (`clean` default, drops furniture by name too; `complete` keeps everything structure does not condemn; `KETCH_EXTRACT_MODE`), `mcp_tools` (allowlist over the six MCP tools; `KETCH_MCP_TOOLS`).
- `KETCH_CONFIG` selects the config filename. Isolate bookmark labs with `KETCH_TAGS_PATH` too; on Linux, `XDG_CACHE_HOME` isolates page bodies. macOS and Windows use their native cache directories, so an XDG override alone is not portable isolation.

## Cache

- Page cache in bbolt at `~/.cache/ketch/cache.db`, default TTL 72h (`cache_ttl` overrides). `ketch cache` shows page-cache stats and the tag index (path, counts, lock state), `ketch cache clear` empties the page cache and keeps bookmarks; both take `--json`.
- Single-process lock: the bbolt DB admits one process at a time. A long-running MCP server holds the lock for its whole lifetime, so concurrent CLI scrapes silently run cache-disabled — every fetch goes to the network. Observed live: `ketch doctor` reports `cache … locked by another process` while the server runs, and `ketch cache` shows `locked: true`. Needing heavy CLI and MCP use long-term → prefer CLI-only, or accept the tradeoff knowingly.
