# ADR 0004: Tags as durable bookmarks for agent workflows

**Status:** Accepted · **Date:** 2026-09-18 · Cache-clear reclamation superseded by [0006](./0006-page-cache-per-operation-handles.md)

## Context

The product goal is a bookmark system at the speed and complexity of agentic
workflows: gather useful docs, code references and write-ups while researching,
organize them under project or topic labels, return in a later session, and
fetch only the sources needed.

An earlier prototype used SQLite FTS5, a markdown chunker, a project manifest
and docs add/list/remove/sync commands. That introduced a document-management
lifecycle where the immediate need was remembering useful sources. Tags retain
source metadata while content continues through the existing fetch/cache path.

The first tag implementation put a second bucket in cache.db. Although entries
survived page expiry, bbolt's exclusive file lock coupled every tag operation
to a crawl or MCP process holding the page cache. Increasing an open timeout
would not resolve a lock held for minutes. The layout is still unreleased, so
we change it before v0.18 rather than making it a shipped storage contract.

## Decision

### 1. Independent, durable storage

Store bookmarks in **tags.db**, separate from cache.db, using the existing bbolt
dependency. Open the index only for individual operations and close it before
checking the page cache. Neither an idle MCP server nor a background crawl
retains its lock. The cache.Store interface remains unchanged; tagStore is the
separate enumeration/write capability.

Use Go's os.UserConfigDir for the durable file: XDG config/home on Linux,
Application Support on macOS, and AppData on Windows. KETCH_TAGS_PATH overrides
the entire filename for tests and portable setups. The file is outside the
normal cache directory so cache cleanup does not erase bookmarks. Existing
experimental in-cache buckets are left untouched, without an automatic import;
no released version has a tag index to migrate.

The original source URL identifies membership. Cookies, User-Agent and URL
rewrites may alter the private page-cache lookup, but never duplicate a source
within a tag or prevent removal by the URL shown to the caller. One source can
belong to multiple tags. The private lookup key never appears in public output.

A bookmark contains the source URL, title, one-line description and tagging
time. Titles and descriptions are bounded to 180 characters plus an ellipsis.
Bodies stay in the page cache and follow cache_ttl (72h by default). Bookmarks
have no TTL. Reads return bookmarks even when cache.db cannot be opened, with
cache_status: unavailable and cached: false; those flags then mean warmth was
not checked. Missing or expired bodies do not make bookmarks disappear.

### 2. Reliable recording and reuse

--tag on search, code, docs, scrape and crawl records results under the same
labels. The MCP research tools expose the equivalent tag option. Search records
every hit before optional scraping; successful fetches enrich their own entries.
Direct-library docs calls and selector scrapes follow the same contract.

Tag add stores known URLs without network access. Re-adding a cold URL preserves
existing metadata. Fetches fill missing metadata on existing memberships, even
without --tag or with --no-cache. Backfill checks membership and updates it in
one transaction; reads never write, so they cannot resurrect a removed bookmark.

Explicit tag operations report storage failures as CLI exit 5 / MCP
[precondition]. Research keeps successful results if saving bookmarks fails:
CLI diagnostics go to stderr (a warning object with code tag_write_failed under
--json), and MCP research results include warnings. A successful fetch does not
by itself guarantee a successful bookmark write. The index still uses a bounded
one-second lock wait; short concurrent writes can succeed, while a separate
process holding tags.db too long produces a visible failure. Later operations
retry opening the file, so a temporary failure does not disable a server's index.

### 3. Bounded discovery

The verbs remain tag add/show/list/remove on CLI and operation on the MCP tag
tool. Tag operations make no network requests. Show returns the newest 50 entries
by default, with --limit N / limit and 0 explicitly selecting all. Equal tagging
timestamps are ordered by source URL for deterministic output.

Text reports showing N of M when limited. Minimal output remains three-column
TSV, with the notice on stderr. JSON/MCP expose entries and cached totals across
the whole tag, shown for returned pages, and cache_status. Negative limits are
validation failures (exit 2 / [validation]). Removing nothing returns exit 3 /
[not_found], including CLI JSON mode.

Sort only small index records. Warmth checks use a timestamp bucket alongside
pages instead of decoding document bodies; legacy cache entries without that
metadata have a read-only timestamp fallback. Tag list does not sort pages or
render their bodies. Show is bounded in output, not a pagination/search API.

## Consequences

Agents can discover a project's saved sources cheaply, select a URL and scrape
it using the normal cache/fetch behavior. There is no sync command, automatic
classification, ranked full-text retrieval or stored document corpus. Labels
are chosen by the user or agent, and deletion is explicit. A bookmark may point
to a dead or changed upstream source; stored metadata is recognition information,
not a freshness guarantee.

Cache clear removes page bodies and frees their bbolt pages for reuse. It does
**not shrink the file**. Unlinking an open database is unsafe: on Unix a holder
can keep writing the old inode while new processes open a replacement; Windows
also imposes open-file constraints. Physical reclamation requires a coordinated
maintenance design and is deferred. The storage split alone does not make
unlinking safe, so we correct the earlier reclamation claim rather than promise it.

The index is a second file with short-lived locks, not a daemon or another
storage dependency. A cache lock can make warmth unknown, but cannot block the
index itself. Very large tags still require scanning their small records for
totals; the default limit protects agent output size, not constant-time reads.
Tag list is a best-effort view across operations, not a cross-database snapshot.

Tag is the sixth MCP tool and is in the mcp_tools allowlist. It mutates local
state and declares readOnlyHint: false and openWorldHint: false. Config, cache
and doctor remain CLI-only operator commands.

This record supersedes the unmerged local-docs-corpus prototype on
cursor/local-docs-fts5-d87c. The local docs provider stub remains unrelated.
