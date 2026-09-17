# ADR 0004: Local docs corpus (agent-managed, SQLite FTS5, crawl as a library)

**Status:** Accepted · **Date:** 2026-09-17

## Context

`ketch docs` proxies a hosted provider (Context7). That works when an operator
has a key and the library is one Context7 curates, and not otherwise. A
`local` backend has been stubbed behind `docs.Searcher` since the surface was
introduced, and [`ROADMAP.md`](../ROADMAP.md) names it the most concrete
near-term direction: index a library's documentation once, then query it with
no network and no API key.

Two design questions had to be settled before building it.

**Who creates the corpus.** The obvious framing — the operator runs an
`add` command the way they run `config set` — is wrong for how ketch is used.
The agent is the one that discovers a project depends on Tailwind 4, runs
`ketch search` to find the docs site, and wants those docs available for the
rest of the session. Choosing *which* source to pull is a judgement about
meaning; ketch's model-free non-goal puts that with the agent. Fetching,
normalising, and indexing the source is mechanical; that is ketch's job.

**How it squares with the non-goals.** [`DESIGN.md`](../DESIGN.md) says ketch
"does not maintain a search index of your history" and that `ketch crawl` is
"not a store you query later." A docs corpus is a store you query later, built
by crawling. The intent of those lines is to reject *implicit* state — memory
ketch accumulates as a side effect of normal use — and an ingestion engine
dressed as a research primitive. Neither describes a corpus an agent asked for
by name, bounded by page count, and fully rebuildable from a manifest.

## Decision

1. **The corpus is research output, not configuration.** `ketch docs add
   <name> <url>` is an agent-facing action on the CLI (and, in a later phase,
   an MCP tool), in the same family as `scrape` and `crawl`: the agent decided a
   source is worth having and pulled it. The operator configures the
   infrastructure around it — where the store lives (`docs_dir`), page caps,
   and whether the MCP server publishes the write tool at all via the existing
   `mcp_tools` allowlist. "Operator configures, agent consumes" holds at the
   level of infrastructure, not individual corpora.

2. **Explicit, bounded, rebuildable state is permitted.** The non-goal is
   amended: ketch keeps no *implicit* cross-invocation memory. A local docs
   library is created only by an explicit `add`, is bounded by `--max-pages`,
   and is a deterministic function of its recorded seed and options — a
   project's `.ketch/docs.json` manifest lets `ketch docs sync` rebuild the
   same corpus on another machine. `ketch docs remove` deletes it. Nothing is
   added, refreshed, or ranked differently as a side effect of searching.

3. **Discovery is ketch's, judgement is the agent's.** `docs add` accepts a
   docs landing page, a sitemap URL, or an `llms.txt`/`llms-full.txt` URL and
   deterministically finds the best source: `llms-full.txt` (one fetch, no
   crawl), then `llms.txt` as a link list, then `robots.txt` `Sitemap:`
   entries and `/sitemap.xml`, then a BFS crawl scoped to the given path
   prefix. `--dry-run` reports the plan and page counts without fetching so an
   agent can confirm scope before spending the budget. `docs add` never runs a
   web search to pick a site.

4. **`ketch crawl` the command is unchanged; `crawl.Crawl` the library is an
   ingestion dependency.** The docs pipeline reuses the scraper, the JS-shell
   escalation, the page cache, and the BFS/sitemap crawler exactly as `crawl`
   does. The crawl command keeps its non-goal: it streams a readable slice of a
   site and exits. Persistence lives only behind `docs`.

5. **Sections by heading are the unit of retrieval.** Pages are split at
   markdown headings into sections carrying a breadcrumb (`Page › H2 › H3`),
   with fenced code blocks kept whole and over-long sections split at
   paragraph boundaries. Sections map one-to-one onto the existing
   `docs.Result` (`Title`, `Breadcrumb`, `Snippet`, `URL` with a heading
   anchor), so `--json`, `--minimal`, and the MCP `docs` tool need no new
   shape. Chunking is deterministic; the same page always yields the same
   sections.

6. **SQLite FTS5 via `modernc.org/sqlite`, lexical only.** Retrieval is FTS5
   with the `porter unicode61` tokenizer and `bm25()` ranking, weighting
   heading and breadcrumb above body. `modernc.org/sqlite` is transpiled C,
   so `CGO_ENABLED=0` holds and the binary still cross-compiles everywhere.
   It costs roughly 8–10 MB of binary; the alternative — a hand-rolled
   inverted index over bbolt — would cost less binary and far more
   maintenance, and FTS5 is a tool the maintainers already operate elsewhere.
   No embeddings, no learned ranking: the model-free non-goal is unchanged.
   Local is "ranked grep over the docs you pulled," not a curated answer, and
   the documentation says so rather than promising Context7 parity.

7. **Project scoping is core, not an add-on.** `docs add` attaches the library
   to the nearest project — the directory holding `.ketch/docs.json`, or the
   enclosing git repository — unless `--global` is passed. Content is stored
   once per machine under `docs_dir`, keyed by library name; the project
   manifest is a small declaration that can be committed. Queries run inside a
   project default to its attached libraries; `--library <name>` narrows to
   one; outside a project every stored library is searched.

## Consequences

- `docs/local.go` implements `docs.Searcher` and `docs.LibraryResolver` over
  the new `docstore` package. `Source` is `"local"`, `Version` is populated
  for the first time, and `--resolve <name>` lists matching local libraries —
  offline, and immune to the resolve-path throttling that sank the withdrawn
  0.16.0 Read the Docs backend.
- `docs add`, `list`, `remove`, and `sync` are CLI subcommands of `docs`.
  `remove` stays CLI-only permanently (destructive, rare). An MCP `docs_add`
  tool with the same caps as the MCP `crawl` tool is the next phase; it is a
  separate tool so `mcp_tools` can prune it independently.
- `DESIGN.md`'s "does not manage state you didn't ask to" non-goal is
  narrowed to implicit state; the crawl non-goal gains the sentence that the
  crawler *library* may serve ingestion behind `docs`.
- The `docs_dir` setting joins config discovery (`ketch config`) and the
  `local` provider joins `ketch doctor` (store opens, manifests parse).
- Ingestion bounds are the agent's guard rails: `--max-pages` (default 500),
  a path-prefix allow filter derived from the seed, same-host only, and a
  structured summary (`pages`, `sections`, `skipped`, `errors`,
  `stopped: "max_pages"`) so the agent knows exactly what it got.
- Anchors on result URLs are GitHub-style heading slugs. They match most
  static-site generators and are documented as best-effort.
- `extract` gains a pure `Sections` function. It is reusable by any future
  surface that wants heading-delimited chunks and is tested independently of
  the store.
