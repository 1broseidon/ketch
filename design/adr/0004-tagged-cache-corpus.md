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
- `ketch tag add <name> <url>...` tags URLs directly, with no network access
  at all.

`ketch tag show <name>` answers the question the agent actually asks — *what do
I have under this tag that I can go back to?* — by emitting an llms.txt-shaped
index of titles, URLs and descriptions. The agent reads the index cheaply, then
fetches the one page it wants — returning without a network round trip when the
page is still cached, and re-fetching it when it is not. This mirrors
`FetchLLMSTxt`, which already consumes exactly this shape from upstream sites;
ketch now emits it for a corpus of the agent's own.

Every operation is a verb under `tag` — `add`, `show`, `list`, `remove` — rather
than a bare `tag <name>` whose meaning shifts with its arity. That follows the
grammar the CLI already uses for a command with stored state to operate on
(`crawl status`, `crawl stop`; likewise `cache clear`, `browser install`), and
it keeps every name available as a tag: a bare positional form would have had to
reserve `list` and `remove`, and would have made reading and writing differ only
by whether a URL followed.

Specifics that follow from the decision:

- **Implicit tagging records only what was fetched.** A bare `search --tag`
  records nothing: its results were never retrieved, and a map of pages nobody
  read is a map of guesses. `search --scrape --tag` records what it actually
  fetched.
- **Explicit tagging records whatever URL you name.** `tag add` does not
  require a cached body *(revised 2026-09-18 — it did at first, a rule carried
  over from the cache-scoped design)*. Naming a URL is a deliberate act, and
  the index is a record of what matters to a piece of work rather than a view
  over the cache; refusing a URL because its body happens to be absent would
  make organising URLs depend on when they were last fetched. Such an entry
  lists as uncached with no title or description, and both fill in the first
  time the page is seen — by any route, with no re-tagging.
- **A page may carry several tags.** Tags are a list, and the same URL under two
  tags remains one cached page.
- **Tags never own the page body.** They are stored in their own bbolt bucket
  alongside the existing `pages` keyspace. The `cache.Store` interface is
  unchanged.
- **The index is durable; the body is not.** A tag entry carries its own URL,
  title, description and the time it was tagged, so it survives the expiry of
  the page it describes. Reading a tag is a purely local render that never
  touches the network and never returns less than it was given.
- **An expired page is a cold entry, not a missing one.** The index marks which
  entries are still cached, so the agent knows which cost a round trip. Fetching
  a cold entry is an ordinary fetch that re-warms the cache.

## Consequences

The *sync* lifecycle disappears, which was the weight in the corpus prototype.
Sync existed because the corpus owned a copy of the content and content drifts
from upstream. A tag entry owns a URL and a title: the URL does not drift, and
the body still arrives through the normal cache path under the normal TTL. There
is nothing to reconcile, so there is no `sync` and no staleness to reason about.

What a tag retains is its history. `cache_ttl` defaults to 72h, so a tag scoped
to the page bodies would be empty by the Tuesday after a Friday of research —
which is precisely when the work resumes and the agent asks what it already
found. Because the index outlives the bodies it points at, tagging accrues a
durable record of what proved useful on a project, and `cache clear` gains a
sensible meaning: it reclaims the disk and keeps the map.

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
- **Nothing expires the index, so deletion must be explicit.** `ketch tag remove
  <name>` drops a whole tag and `ketch tag remove <name> <url>...` drops single
  entries; they are the price of durability, because no TTL
  is going to tidy up after a tag that has outlived its project. This is a
  deliberate trade — an entry is roughly 200 bytes against a page body's tens of
  kilobytes, so a thousand tagged pages is a rounding error on disk, and the
  alternative is a feature that silently forgets the thing it exists to
  remember.
- **A tagged URL can go away upstream.** The index records where a page was, not
  a promise that it still resolves; a dead link surfaces as a failed fetch, at
  the same moment and in the same way a bookmark's would.
- **`tag` is the first agent-facing command added to MCP.** Unlike `config`,
  `cache` and `doctor` — operator actions, deliberately CLI-only — the agent is
  both the writer and the reader here, so `tag` is published as an MCP tool and
  a `tag` option on the existing fetching tools. It is the first addition to the
  published tool set, and so the first change to the `mcp_tools` allowlist.

This record supersedes the unmerged local-docs-corpus prototype on
`cursor/local-docs-fts5-d87c`. The `local` docs provider stub in `docs/fts5.go`
is unaffected: it reserves a provider ID for a future local *docs backend* and
is unrelated to tagging.
