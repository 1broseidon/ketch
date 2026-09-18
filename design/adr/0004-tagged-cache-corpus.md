# ADR 0004: Tags over the page cache instead of a local docs corpus

**Status:** Accepted · **Date:** 2026-09-18

## Context

An agent working on a task repeatedly needs the same handful of pages: the
vendor's documentation, a couple of how-to write-ups, an issue thread that
explains the one undocumented flag. Today each of those is either re-fetched or
re-summarised into the model's context, and neither survives the end of a
session in a form the agent can go back to.

The obvious answer is a local documentation corpus, and it was prototyped: a
SQLite FTS5 store, a heading-based markdown chunker, source discovery, a project
manifest, and `docs add` / `list` / `remove` / `sync` to manage it. That
prototype worked, and it is why this record exists — the shape it took is what
argued against it.

It introduced a **second persistence mechanism** next to the existing bbolt page
cache, with its own schema, dependency, and failure modes. More importantly it
introduced a **lifecycle**: a corpus you add to, sync, and prune. That is in
direct tension with two of ketch's design principles. "Stateless: call → result
→ done" no longer holds for a tool you maintain between calls, and "operator
configures, agent consumes" breaks down when the agent's useful work depends on
someone having curated a corpus first.

It also solved the wrong half of the problem. The expensive part is not storing
documentation — ketch already caches every page it fetches. The expensive part
is *knowing what you already have*.

## Decision

Do not build a corpus. Add **tags as metadata over the pages the cache already
holds.**

A tag is a label attached to cache entries. It is applied two ways:

- `--tag <name>` on `search`, `scrape` and `crawl` tags pages as they are
  fetched.
- `ketch tag <name> <url>...` tags pages already in the cache, with no network
  access at all.

Reading a tag answers the question the agent actually asks — *what do I have
under this tag that I can go back to?* — by emitting an llms.txt-shaped index of
titles, URLs and descriptions. The agent reads the index cheaply, then fetches
the one page it wants, which is already local and returns without a network
round trip. This mirrors `FetchLLMSTxt`, which already consumes exactly this
shape from upstream sites; ketch now emits it for a corpus of the agent's own.

Specifics that follow from the decision:

- **Only fetched pages are tagged.** A bare `search --tag` records nothing: its
  results were never retrieved, and a map of pages nobody read is a map of
  guesses. `search --scrape --tag` records what it actually fetched.
- **A page may carry several tags.** Tags are a list, and the same URL under two
  tags remains one cached page.
- **Tags never own the data.** They are stored in their own bbolt bucket as
  references into the existing `pages` keyspace. The `cache.Store` interface is
  unchanged.
- **Tags are a view, not a store.** Reading a tag prunes references whose pages
  have expired and refreshes the ones that remain.

## Consequences

The lifecycle disappears. Because a tag references the cache rather than
duplicating it, an expired page cannot rot a tag into a lie — the tag simply
gets smaller. A project in active use keeps its pages warm by being read; an
abandoned one empties out on its own. There is no `sync`, no `prune`, and no
staleness to reason about.

Nothing new is persisted and no dependency is added. The feature is tags plus a
renderer over storage that already exists, which is a fraction of the corpus
prototype's surface, and it degrades harmlessly: if the index is wrong or empty,
the agent scrapes the way it always did.

Tags accrue as a byproduct of normal work rather than from a curation step, so
the corpus reflects what was genuinely useful instead of what someone predicted
would be.

The costs are real and accepted:

- **Retrieval is assembly, not search.** A tag yields an index to choose from,
  not ranked full-text results. Pages are found by title and URL. If ranked
  search over page bodies is ever wanted, it needs an index, and that is a
  separate decision — not something this design grows into by accident.
- **Tag reads are bounded by the cache TTL.** A tag is only as durable as
  `cache_ttl`. Refresh-on-read makes active use self-sustaining, but a tag left
  alone longer than the TTL will be empty when revisited. This is intended:
  ephemerality is the property that removes the lifecycle.
- **`tag` is the first agent-facing command added to MCP.** Unlike `config`,
  `cache` and `doctor` — operator actions, deliberately CLI-only — the agent is
  both the writer and the reader here, so `tag` is published as an MCP tool and
  a `tag` option on the existing fetching tools. It is the first addition to the
  published tool set, and so the first change to the `mcp_tools` allowlist.

This record supersedes the unmerged local-docs-corpus prototype on
`cursor/local-docs-fts5-d87c`. The `local` docs provider stub in `docs/fts5.go`
is unaffected: it reserves a provider ID for a future local *docs backend* and
is unrelated to tagging.
