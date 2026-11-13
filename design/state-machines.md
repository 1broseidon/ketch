# Implicit state machines in ketch: audit and conversion plan

**Status:** Proposed · **Date:** 2026-11-02 · Tree audited: feat/web-docs at 537d96f (round 3 uncommitted in worktrees)

Four read-only audits (resolution, retrieval, fetching, cross-cutting) found 39
implicit state machines: 10 must convert, 23 should, 6 could. The full reports
with file:line evidence are in design/state-machines/. This document ranks them
and says what to convert before any further docs work, including the embedding
refusal floor and the local index.

An implicit machine is control flow that depends on which of several states a
value is in, where the state is never named: booleans read in combination, -1
and "" sentinels, errors used as control flow, pipelines whose order is only a
convention, stop conditions checked in several places. The bugs we have paid
for this week were all of this class: a hint that overrode popularity because
two booleans were read in the wrong order, a search hit's failure counted as a
package's failure, a spent quota remembered in one process and not the next.
Several of their descendants are still in the tree.

## The three machines everything else depends on

These come first because the refusal floor, the auto chain and the local index
cannot be built correctly until they exist.

### 1. One answer lifecycle for `ketch docs` — MUST

Today "no answer" has three encodings: an error, a nil error with zero results,
and a nil error with a weak result. Only the error steers the auto fallthrough,
the exit code, and what the agent sees. Empty results exit 0 and print
`result_count: 0`; `--json` prints `null`. Over MCP an empty answer is a
success with no message. Decision-13 says web's empty answer is never replaced
by Context7, so a refusal floor that returned "empty" would never reach
Context7, and one that returned an error would. `ResolveSelect` already breaks
the rule.

Explicit form: one `Outcome` type for every docs request.

```
status:     found | partial | refused | failed
reason:     (refused) no_library_named | ambiguous | not_verified | no_version |
            below_floor | nothing_indexed | prohibited
            (failed)  upstream | rate_limited | timeout | cancelled
complete:   bool            // every candidate fetched; no budget cut
attempts:   [{backend, status, reason, elapsed}]   // local, web, context7
confidence: float           // best similarity once the floor exists
```

One `classify(outcome)` maps to the exit code and the MCP error class, used by
both surfaces. The auto backend routes on `status` and `reason`, never on error
types. The agent sees `attempts` and `reason` on every call. This is machine 1
in retrieval.md and the cross-area edge named by all four audits.

### 2. One error classifier for the CLI and MCP — MUST

ADR-0002 describes one taxonomy. The code has two mirrored switches
(cmd/exit.go:42-69, mcp/errors.go:56-78) plus four mirrored per-command pairs,
with no parity test. They already disagree: a deadline exceeded is `[cancelled]`
over MCP and exit 4 ("retry") on the CLI (mcp/errors.go:59 vs main.go:29).
Two providers patch around it in opposite directions: docs/web.go:665-676 strips
the deadline chain, search/exa.go:190-199 preserves it. Context7's 30 s client
timeout is a deadline, so it is `[cancelled]` over MCP today.

Explicit form: `errclass.Classify(err) (Kind, Reason)` in one package, imported
by both surfaces, with a parity test that walks every sentinel. The Outcome in
machine 1 produces the Kind directly, so docs never goes through `Classify` at
all. Cross.md machine 1.

### 3. A typed fetch outcome at the docs boundary — MUST

A page's source (http / http_shell / browser / "") is a free string decided in
four places in scrape/scrape.go and read in three, and it is dropped at
docsfetch.FetchPage, so the docs backend re-derives JS-shell-ness with a
different heuristic (web_site.go:127). Concrete bugs: CachedScrapeRaw
(pipeline.go:147) skips the browser-staleness check that CachedScrape applies,
so a shell page is served from the raw cache after a browser is installed;
staleness depends on `HasBrowser()`, which any launch failure clears for the
life of the process, so the same entry is fresh in one process and stale in the
next; `FinalURL == ""` means both "legacy entry" and "browser could not say",
and the second is refetched on every call and then refused as out of scope.

Explicit form: `scrape.Source` becomes an enum carried on Page and the cache
entry, and `docsfetch` returns a `FetchOutcome{source, status, detection,
final_url_known}` to docs. The refusal floor's `reason` can then say
`unrendered_js_shell`, `forbidden` or `out_of_scope` instead of "low
similarity". Fetch.md machine 1, with machines 2 and 4 folded in.

## Must convert, in order after the three above

4. **Rival verdict in resolution** (resolution.md 1). A losing project's fate
   is spread over `resolveLog` side tables, `project` flags and three gates with
   different thresholds. `rivalOf` (web_keyless.go:465) exempts an out-of-hint
   rival whose downloads could not be read, which is the opposite of
   decision-12 rule 1 and is the open failure path of the hint-overrides-
   popularity bug. No test covers hint + unread rival. Explicit form: a
   `verdict` enum per project (`picked | dominated | rival_unweighed |
   rival_unverified | rival_unanswered | dropped_<why>`) decided in one
   function, listed in `attempts`.
5. **Weight lifecycle** (resolution.md 2). `-1` means not read, no source,
   failed and unknown. A cached pick has `proj == nil, stars == -1`, so a
   query that stands cold can be ambiguous warm, and the cache persists.
   Explicit form: `weight{state: unread|absent|failed|known, n}` stored in the
   resolution cache.
6. **Reading outcome and veto** (resolution.md 3). A strict (guessed) reading
   that is ambiguous stops the whole query because `pickUnique` runs before the
   guess-proof gate (web_resolve.go:861-873); when nothing answers, a timed-out
   reading collapses to `ErrLibraryRequired` (exit 2), so the agent cannot tell
   "retry" from "names nothing". Folds into machine 1's `reason`.
7. **Cache admission** (resolution.md 4) and **version filter before pick**
   (resolution.md 5). A pick is cached whenever `log.failed` is empty, but
   `pkgsiteRepo`, `realGoModule` and a failed web search only log rejects, so
   degraded picks are cached; `finish` drops a match whose version probe
   errored before `pickUnique` and does not log it, so a transient failure on
   the stronger project hands the pick to a lesser one, and that is cached.
   Explicit form: cache only `Outcome{status: found, complete: true}`.
8. **Discovery ladder and deadline** (retrieval.md 4). A probe timeout looks
   like a 404 (web_fetch.go:119-125), so latency decides which source answers;
   a sitemap cut at its 8 s budget is used as complete; timeouts stack
   (20 s resolution + 45 s docs + Context7) so nothing bounds a call end to end.
   Explicit form: `probe{hit|missing|timeout|error}` per rung and one deadline
   owned by the Outcome, which sets `complete=false` instead of degrading.
9. **Candidate fetch partial success** (retrieval.md 5). `fetchPages` fails
   only if every candidate failed; the best candidate can time out and the
   answer still looks complete. Becomes `status: partial`.
10. **Context7 quota** (retrieval.md 3). A 429 without a `ratelimit-remaining`
    header is never recorded (context7_quota.go:119), so the next call or
    process spends another request; the in-process verdict overrides the state
    file and has no 24 h window, so a long-running MCP server can stay blocked
    for a month while the CLI does not. Explicit form: one `quota{state:
    unknown|ok|spent, remaining, reset, seen_at}` with the file as the single
    source of truth and the 24 h rule in one place.

## Should convert (observability or a known bug class)

- **Auto fallthrough gate** (retrieval 2): three methods each with their own
  predicates; `docsFor` resolves before `accept`, spending keyless quota on
  rejected picks; `autoError.Unwrap` drops Context7's failure so a spent quota
  after a web not-found exits 3. Folds into machine 1's `attempts`.
- **Search auto chain** (cross 2): every provider error is an unclassified
  fall-through, so "all failed" is upstream even when every failure was a 401;
  no memory of a rate-limited provider even within one MCP process (DDG burns
  3 attempts + sleeps per call); `NewAutoFromConfig` silently drops a provider
  whose constructor fails, so doctor's chain can differ from runtime. Explicit
  form: per-provider `state{ok|rate_limited(until)|unauthorized|down}` kept
  for the process, and `attempts` in the result, same shape as docs.
- **Per-key rotation** (cross 3): 401 (permanent) and 429 (transient) take the
  same one-retry path; doctor marks one-bad-key-of-two as misconfigured for an
  install whose searches succeed.
- **Doctor status** (cross 4): the enum is explicit but has no `rate_limited`;
  a fully throttled chain reports "3 of 3 healthy" and exit 0 while every search
  exits 4.
- **403 → browser retry** (fetch 2): `fetchVerified` accepts status < 400 or 0,
  `FetchContentWithBrowser` accepts 2xx only, and `--force-browser` swallows
  the probe error, so an access-denied page is extracted and cached as
  `browser`. Crawl's heuristic force path has the same hole.
- **Crawl item lifecycle** (fetch 5): `page, rawHTML, err = BrowserScrape(...)`
  at crawl.go:231 overwrites a good HTTP page with nil on browser failure;
  `Status: changed` is never emitted; the background status file can stay
  "running" forever if the worker dies.
- **Rank/cut/shape pipeline** (retrieval 6): stage order enforced only by call
  order; the only cutoff is relative (30% of the top score); BM25 scores are not
  comparable across queries, so the embedding floor needs its own typed gate
  before `relevant`; truncation shows only as text in the snippet.
- **Citation knowledge** (retrieval 7): nil-vs-empty anchors plus a bool; the
  agent cannot tell a section citation from a page fallback. Becomes
  `citation{section|page|unverified}` on each result.
- **Reading kind flags, keyless/token mode, edge-vs-trust evidence, budgets,
  ecosystem hint, candidate root kind, pick-gate order** (resolution should
  list): each a small enum; the budgets one matters most because GitHub's
  spent quota lives per `resolveLog` and is re-probed per reading.
- **Repo-scoped code search** (cross 5): three backends decide "repo absent"
  by three different signals; MCP emits `[not_found]` as both a warning and an
  error.
- **updatecheck** (cross 6): a failed fetch with no cache persists nothing, so
  every command retries the network check.
- **Result shape** (resolution, agent-visible gap): trust, "not checked",
  version-substitution and "served from cache" notes are dropped before
  `Result`; the agent sees none of them.

## Could convert (tidiness)

Bench hook mode derived three times from env; `config.Load`'s display-only
sentinel guarded by a command allowlist; tag entry "unfilled" strings; the
token-mode escalation ladder; `strictMissError`'s lazy weighing; the version
source ladder's "failed" vs "none" collapse.

## Already explicit, leave alone

`health.Status`, `HTTPStatusError`, `rendering`/`browserRender`, `httpx`
redirect check, bench `Exchange`, `Auto.stop`, `Multi` outcomes, GitHub token
resolution with memoised source, `boltTx` open-per-operation (ADR-0006).

## Plan

Everything below is gated on the sealed holdout exactly as before, and nothing
changes the default backend.

| Step | Work | Done when |
|---|---|---|
| A | `docs.Outcome` + `attempts`, one `classify`, Auto routes on it, CLI/MCP render it | every docs path returns an Outcome; `--json` never prints `null`; parity test across sentinels |
| B | `errclass` package replaces both switches; parity test; the two provider patches removed | one classifier, deadline handled the same on both surfaces |
| C | `scrape.Source` enum on Page and cache entry; `docsfetch.FetchOutcome`; raw-cache staleness fixed | a shell page is never served after a browser exists; `FinalURL` states separated |
| D | Resolution: `verdict` per project, `weight` state, cache admission only on complete answers, version probe failures as verdicts | hint + unread rival test; warm == cold on the dev set |
| E | Discovery `probe` enum and one end-to-end deadline; `partial` status | a timeout never reads as a 404; p95 bounded |
| F | Context7 quota with the file as source of truth; search auto per-provider state; doctor `rate_limited` | 429 without headers is recorded; throttled chain reports as throttled |
| G | Round 3 rebased onto A–D and committed, or dropped per part | holdout re-run once |

Then the refusal floor is one new `reason` (`below_floor`) with a `confidence`
field, and the local index is one new backend that returns the same Outcome.

## Rejected

- Converting the "could" list now. None of them has caused a bug or hides a
  decision from the agent.
- A generic state-machine library. Enums plus one transition function per
  machine is enough and stays readable in Go.
