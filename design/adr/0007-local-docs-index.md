# ADR 0007: A local docs index, built once per library and queried offline

**Status:** Proposed · **Date:** 2026-11-02 · Supersedes the local-corpus
rejection in [ADR-0004](./0004-tagged-cache-corpus.md) (tags themselves are
unchanged)

## Context

The keyless `web` docs backend answers every query live: it identifies the
library, verifies its docs site, finds the version, picks about six of the
site's pages and ranks their sections, all within about two seconds and
against rate-limited registries, search providers and the site itself. Three
rounds of rules moved the sealed holdout from 27% to 33%. Almost every miss
is in the first three steps, which are judgments made from scratch on every
query, and safe answers mean refusing when unsure, so refusals were about
half of all misses.

Two offline measurements changed the picture:

- **Retrieval.** On 13 fully crawled docs sites (about 2,400 sections each,
  27 scorable dev cases), dense cosine with bge-base-en-v1.5 put the answer
  in the top 10 sections 89% of the time and in the top 3 70%; BM25 over the
  same sections managed 70% and 37%. bge-small scored 81%/63%, nomic 81%/70%,
  mxbai-large 85%/67% at three times bge-base's size. Keyword fusion (RRF),
  page-first two-stage ranking and a cross-encoder reranker added nothing at
  full-site scale. The remaining misses came from the quick crawl (other
  versions' and locales' copies of a page; long sections cut before their
  key part), not the model.
- **Runtime.** `github.com/rostamlabs/rembed` v0.3.0 runs bge-base in pure Go
  (CGO_ENABLED=0, cross-compiles to linux and windows) at parity with the
  Python reference: mean cosine 0.99999 over 24,000 texts and identical
  retrieval. On an 18-core arm64 laptop it embeds 33 sections/s (bge-small
  101/s), a single query in 6.5 ms at the median, and loads in 0.2 s. Its
  int8 mode is slower on arm64 (26/s) and is not used.

ADR-0004 rejected an earlier SQLite FTS5 corpus with `docs add/list/remove/
sync`: it was document management where the need was remembering sources.
That remains true for bookmarks. It is not true for retrieval: the
judgments that fail live are cheap to make once and expensive to make on
every call.

## Decision

**Index once per library@version; answer every query locally.**

- `ketch docs index <library|docs-url> [--version v]` resolves the docs root
  with the web backend's existing machinery (a docs URL skips resolution; an
  ambiguous or unverified name refuses with its candidates exactly as a
  query would, and the caller passes the URL instead), enumerates the site
  through the existing discovery (llms.txt, llms-full.txt, sitemaps scoped to
  the root, the site's search index, nav links), keeps only pages under the
  resolved version root and in the root's locale, caps the page count
  (1,500 by default), fetches through the scrape pipeline and page cache at a
  polite concurrency (robots.txt disallows are honoured), splits pages with
  the web backend's sectioning (never inside fenced code), embeds and stores.
  Re-running refreshes only pages whose content hash changed.
- A `local` docs backend answers from the index: it embeds the question
  (with bge's query prefix; passages get none), takes the cosine top 30
  sections by brute force, groups them by page, trims long sections to the
  blocks that name the query, and returns about 8–10 page results with page
  URL, anchor and breadcrumb. The agent calling ketch picks; there is no
  decision model.
- The auto chain becomes local → web → Context7. Local answers when the
  library is indexed; otherwise, or when it finds nothing, the existing chain
  runs unchanged. The default docs backend stays Context7 (decision-10).
- **The model is optional.** `ketch model install` downloads bge-base
  (`--small` for bge-small) into ketch's cache directory, pinned to a
  Hugging Face revision and verified by size and SHA-256; `model_path`
  points elsewhere. Without a model, indexing stores text only and the local
  backend ranks with the web backend's BM25, saying so in the outcome's
  reason (`keyword_ranking`). The model only embeds text: everything returned
  is a verbatim section of the indexed site.

### Why the lifecycle stays light

What ADR-0004 rejected was a corpus someone curates. Phase 2 syncs the index
from the project's lockfiles (package-lock.json, go.mod, Cargo.lock, uv.lock,
poetry.lock), so the index follows the dependencies already declared and
nobody keeps a list. `docs index` by hand is the exception, and `--list` and
`--remove` are its only management verbs. There are no tags, collections or
per-document edits, and the index never replaces the page cache: pages are
fetched and cached exactly as for `scrape`.

### Storage and versioning

- One bbolt file, `docs-index.db`, in ketch's cache directory, separate from
  `cache.db` and `tags.db` and opened per operation (ADR-0006), so queries
  from MCP servers and CLI calls share it with an indexing run. A build
  writes in short batches, never one long transaction.
- Per library: metadata (the library as given, the names it answers to, docs
  root, version, how it was resolved and the evidence, the model id and
  dimensions, the index format version, build time, page and section counts,
  discovery sources, whether the page cap cut it short), pages (URL, title,
  content hash) and sections (page URL, anchor, heading path, text, and one
  or more vectors).
- A long section is embedded as several windows of about 1,200 characters,
  each prefixed by its heading path, split at block boundaries, so nothing
  falls past the 512-token window; the section scores as its best window.
- Vectors are stored as float16 (768 dimensions: 1.5 KB per window). Cosine
  error from float16 is below 1e-3, far under any ranking margin.
- Changing the model id invalidates that library's vectors: the next
  `docs index` (or `--reembed`, without the network) re-embeds the stored
  text; nothing is re-crawled. A different index format version asks for a
  rebuild.

### Fallbacks

local (embeddings, or BM25 with no model) → web (the live keyless path,
unchanged) → Context7 (keyless quota or key, decision-13's gates). Every
attempt is recorded in the docs Outcome (decision-20): `not_indexed` when the
index does not hold the library, `no_results` when it holds it and nothing
matched. An index that holds the library but has no usable vectors (no model,
or another model) answers with BM25 rather than refusing.

## Measurement

Two suites replace fighting the live web, both in ketch-bench:

- **Index accuracy:** for each benchmark library, did `docs index` pick the
  right site and version? Gate: zero wrong roots; refusals are logged.
- **Offline retrieval:** frozen indexes, every dev and holdout case,
  deterministic. Primary metric: the answer is in what ketch returns (top
  10); top 3 reported too. The sealed holdout stays sealed.

The proposed target is the answer in the top 10 on the sealed holdout with
zero wrong roots. A refusal floor (a calibrated minimum cosine) is phase 2;
phase 1 returns the nearest sections.

## Consequences

- ketch gains a second durable store and a model download of 438 MB
  (bge-base, float32 safetensors as published) or 133 MB (bge-small). The
  binary stays static and CGO-free; the runtime adds about 1 MB.
- Indexing a 600-page site takes minutes (fetching at concurrency 4, then
  embedding); queries take tens of milliseconds and no network.
- Freshness is per build: pinned versions never change, and a re-run
  re-fetches through the page cache and re-embeds only changed pages.
- The live web path keeps its role as the fallback and as the source of the
  index's judgments; improvements there still pay off twice.
- The MCP `docs` tool can query the local backend; building an index is
  CLI-only for now, since it runs for minutes.
