# ADR 0006: Open the page cache per operation

**Status:** Accepted · **Date:** 2026-09-22 · Supersedes the cache-clear
reclamation paragraph of [ADR-0004](./0004-tagged-cache-corpus.md)

## Context

The page cache is one bbolt file, and bbolt lets a single process hold a
database file at a time: the exclusive file lock lasts as long as the handle
stays open. Through 0.18.0 the MCP server opened the cache once at startup and
kept it for its whole life, and background crawls did the same for theirs.

Stateless CLI calls never noticed, because they exit in seconds. Long-lived
processes made the lock permanent:

- With an MCP server running, `ketch cache` reported "cache in use by another
  process", `tag show` reported `cache_status: unavailable` with every page
  "not cached", and CLI scrapes ran uncached.
- A second MCP server (two agent hosts, or one host that restarted a server
  without killing the old one) failed its startup open and ran uncached for
  its entire life, even after the first server exited.
- An orphaned server from an old install held the lock indefinitely.

Nothing ever deleted expired pages either, so the file only grew, and
`cache clear` could not shrink it: ADR-0004 deferred reclamation because
unlinking the file under a long-lived holder would leave that holder writing a
ghost file.

Three fixes were considered:

- **A file per page**, written by atomic rename. No lock at all. Rejected:
  ketch shipped exactly this in 0.1 and replaced it with bbolt in 0.2. It puts
  thousands of files on a user's disk, needs its own cleanup, and needs
  separate handling on Windows, where rename over an open file is not atomic.
- **SQLite in WAL mode.** Many readers with one writer across processes.
  Rejected for the page cache: it is a large dependency for an exact-key
  blob store, and it still has one writer at a time.
- **bbolt opened per operation.** Keep the file and the format; stop holding
  the handle.

Measured on a real 16 MB cache: opening the file, reading one entry and
closing it takes 26 µs at the median and 50 µs at p99. A write with its fsync
takes 1.9 ms and 8.4 ms. Every cache operation sits beside a network fetch of
hundreds of milliseconds, so the open is noise and cross-process contention is
rare.

## Decision

**Every page-cache operation opens the bbolt file for one transaction and
closes it.** The tag index already worked this way (ADR-0004); both stores now
share one helper.

- The MCP server, crawls and CLI keep the cache *path*, never an open handle.
  A lock held elsewhere costs the current operation a miss or a dropped write,
  bounded by a one-second wait; the next operation tries again.
- Within one process, transactions on a file are serialised by an in-process
  reader/writer lock. Two handles in one process contend through the OS lock
  exactly as two processes do, and bbolt waits on that lock by polling every
  50 ms; the in-process lock turns the poll into a direct hand-off for
  concurrent MCP tool calls.
- **Expired entries are swept inside a write** the cache already makes: at
  most once an hour across all processes (the time is stored in the file),
  removing at most 1,000 entries per write. Entries without a freshness stamp
  predate stamps and go with them. The file therefore stays near its working
  set; bbolt reuses freed pages.
- **`cache clear` returns the space.** It empties the buckets in a write
  transaction, then deletes the file. Emptying first is what makes clear
  correct everywhere; the delete is what shrinks it, and is best effort:
  Windows refuses it while another process has the file open, leaving the
  emptied file. A write that opened the old file before the delete is lost
  with it, which is what a clear at that moment means. A process that still
  holds the file long-term (a pre-0.18.1 ketch) makes clear time out before
  the delete, so no process is ever left writing a ghost file.
  Clear also removes the one-file-per-page directory a pre-0.2 ketch left
  beside the database, touching only files named the way that cache named
  them.

## Consequences

- The CLI, any number of MCP servers and background crawls share one cache.
  `tag show` warmth, `ketch cache` stats and cached scrapes work while a
  server is running. "Cache in use by another process" now means a
  transaction was still running after a one-second wait.
- A server that starts while something else holds the file recovers on its
  next call instead of running uncached until restart.
- An orphaned ketch process holds nothing between calls, so it cannot lock
  anyone out.
- Each cache operation pays one open and close, and each write one fsync, as
  measured above.
- Readers and writers in different processes still serialise per transaction.
  If a workload ever makes that contention visible, the file-per-entry and
  SQLite options above are the next step; neither is needed today.
- A pre-0.18.1 process that holds the file still locks it until that process
  exits. The upgrade fixes the holder, not its older neighbours.
