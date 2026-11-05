# Docs retrieval and answer lifecycle (after a root is known)

Repo: ketch, feat/web-docs at 537d96f, read-only. Two things were checked by running code outside the repo (/tmp/sm-audit/probe): a Go `http.Client` timeout satisfies `errors.Is(err, context.DeadlineExceeded)`, and `json.Marshal` of a nil slice is `null`.

## Summary
9 machines: 3 MUST, 5 SHOULD, 1 COULD. The worst is the answer/refusal lifecycle. "No answer" has three encodings today: an error, a nil error with zero results, and a nil error with low-relevance results. Only the first one steers the auto fallthrough, sets a non-zero exit code, or tells the agent anything. The planned refusal floor has to pick one of these, and decision-13 ("an empty answer is an answer") currently picks the wrong one.
Second is the discovery ladder. Deadlines are layered, and a missed deadline or a transient probe failure looks the same as "file absent". The ladder then silently descends to a weaker source, and the answer is non-deterministic and unflagged.
Third is the Context7 quota machine, which still has descendants of the "spent quota lived in one process, not the next" bug: a headerless 429 is never remembered, and the in-process and persisted verdicts are decided by different rules.

## Machines

### 1. Answer / refusal lifecycle (the terminal states of one docs request) — MUST
- Where:
  - Entry: `docs/web.go:154` (Search), `:446` (GetDocs), `:495` (docsForURL).
  - Transition sites:
    - `web.go:196-236` resolveQuery returns an ambiguity, ErrLibraryRequired or upstream error.
    - `web.go:684-693` collect returns ErrNotFound ("no docs pages under root").
    - `web_fetch.go:134-173` fetchPages returns a failure only if every candidate failed.
    - `web.go:667-677` callErr turns a deadline into a plain upstream error.
    - `web.go:555-556` rankPages returns a possibly empty slice with a nil error.
  - Reporting: `cmd/docs.go:85,91-95`, `mcp/docs.go:117-122`, `cmd/exit.go:60-69`, `mcp/errors.go:136-147`.
- States, as they exist but are never named:
  1. Refused: library not identified (ErrLibraryRequired, exit 2).
  2. Refused: ambiguous (`AmbiguousLibraryError`, exit 2).
  3. Refused: root has no pages, redirect off-root (`scopeError`) or common word not in use (`web.go:356`). All wrap ErrNotFound, exit 3.
  4. Failed: upstream or timeout, exit 4.
  5. Answered, partial: some candidate pages failed or the deadline cut the fetch, but at least one page arrived.
  6. Answered, empty: pages fetched, no section matched, `result_count: 0`.
  7. Answered, non-empty. The first section is always returned (`web_rank.go:1128-1137`) and the relevance cutoff is relative only.
  8. Auto only: answered by Context7; web and Context7 both refused or failed.
- Transitions: decided by error type, not by a state value.
  - 1-4: `errors.Is` on sentinels, in two copies (`cmd/exit.go:63-66`, `mcp/errors.go:139-144`).
  - 5-7: nothing. A nil error means all three.
  - Auto: `err == nil` at `auto.go:100,127` is the only test for "web answered".
- Today: nil/zero sentinels plus error values.
  - `rankPages` returns `[]Result` that is nil when empty (`web_rank.go:1126`). `cmd/docs.go:92` encodes it, so `--json` prints `null` (verified).
  - MCP omits `results` (`omitempty`) and sets no message. The agent sees an indistinguishable empty success.
  - The CLI text path prints `result_count: 0` with exit 0.
  - Decision-13 and `auto.go:51-56` state that web's empty answer is never replaced by Context7. So a refusal-floor result expressed as "empty results" would never fall through to Context7, and one expressed as an error would. That is a semantic fork hidden in a nil check.
  - `ResolveSelect` (`auto.go:158-160`) already breaks the rule the other two follow: it turns web's empty list into a not-found error and falls through.
- Risk:
  - An agent cannot tell "the docs don't cover this" from "ketch found nothing" from "ketch found something weak".
  - Partial answers (state 5) are reported as complete answers (machine 5).
  - The floor, once added, produces a fourth encoding unless the states are named first.
- Explicit form: one `Outcome` returned by the web backend and consumed by auto, CLI and MCP. Fields: `Status` (answered | empty | refused | failed), `Reason` (enum: no_library, ambiguous, no_pages, below_floor, no_match, upstream, timeout, quota), `Completeness` (complete | partial), `Results`, `Provider`, `Tried []Attempt`. A single `Classify(Outcome) (exitCode, mcpKind)` replaces the two switches.
- Agent-visible: `status` and `refused_because` on every result of `ketch docs --json` and in the MCP output (today the MCP `backend` and `errors` fields exist only for auto and only on success). Also `partial: true`, `pages_failed`, and a decision-13 rule that says which statuses Context7 may follow.

### 2. Auto fallthrough gate — SHOULD
- Where: `docs/auto.go:98-112` (SearchSelect), `:118-148` (GetDocsSelect), `:153-173` (ResolveSelect), `:177-200` (autoSecond, autoError), `:246-282` (context7Name, queryPinsLibrary), `:305-418` (webCandidates, candidateFor, namedBy).
- States: Skip (ctx dead | library pinned | docs URL | Context7 ID routed directly) → Ask Context7 with an acceptance check (web's candidates | query names the pick | exact owner/repo) → accepted | rejected | Context7 failed → web's error stands or both failed.
- Transitions: three methods, each with its own nested ifs.
  - Search: `ctx.Err() || queryPinsLibrary(query)` skips. `accept = webCandidates(err)`, else `namedBy(query)`.
  - GetDocs: `isContext7ID` routes straight to Context7. `context7Name` skips, same chain, and `exact != ""` overwrites `accept` entirely (`auto.go:138-145`), discarding the candidate rule.
  - Resolve: empty web list is a refusal (see machine 1).
  - The gate is therefore three predicates with different code, not one.
- Today: errors steer control. `webCandidates` does `errors.As(webErr, &amb)` to pick the rule. Any web error, including a transient timeout, is "web refused".
- Risk:
  - Quota burn on rejected picks. `docsFor` resolves first and checks `accept` after (`auto.go:206-217`). Every rejected pick, e.g. "useState in react", costs a request against the ~200 a month keyless cap and teaches nothing.
  - Because `accept` for an exact owner/repo replaces the ambiguity rule, an ambiguous web result for a github spec can be answered by any Context7 library with the same owner/repo, ignoring web's candidates. Narrow, but it is the same shape as the prior ordering bug (two checks, last one wins).
  - `autoError.Unwrap` returns web's error only (`auto.go:200`). A Context7 `RateLimitError` after a web not-found or ambiguity is reported as exit 3 or 2 ("stop / fix input"), though decision-13 says a spent quota is an upstream error. Direct `--backend context7` follows decision-13; auto does not.
  - On total failure `Selection.Failures` is built and then dropped (`servedDocsBackend` returns early when `sel.Backend == ""`; MCP drops `sel` on error). The failures survive only as a message string.
- Explicit form: `func decide(webErr error, req Request) Fallthrough` returning `Skip{Reason}` or `Ask{Accept}`, used by all three methods. Result: `AutoResult{Provider, Attempts []Attempt{Backend, Outcome, Reason}}` returned on success and on failure.
- Agent-visible: `attempts: [{backend:web,status:refused,reason:ambiguous},{backend:context7,status:quota_spent,resets:...}]`, so a retry decision doesn't depend on parsing text.

### 3. Context7 quota machine (in-process vs persisted) — SHOULD (borderline MUST)
- Where: `docs/context7_quota.go:57-65` (quota), `:71-84` (parse), `:118-128` (observe), `:136-152` (spent), `:163-172` (shared tracker); `docs/context7.go:283-305` (get); `docs/context7_state.go:13-16,79-122`; `ProbeContext7` (`context7.go:332-381`).
- States: Unknown | Available(n) | Spent(until reset) | Spent-without-reset | Expired (reset passed) | Persisted-stale (>24h).
- Transitions: `observe` (after each response), `spent` (before each request), the clock, and the state file.
  - Entry condition for the state file: `if !q.known && t.state != nil` (`:143`).
  - Trust window applies only to the persisted record (`:144`). In-process `last` has none.
  - Spent verdict: `q.known && remaining == 0 && !reset.IsZero() && now.Before(reset)` (`:148`). A spent quota with no reset time is treated as available, "asked again".
- Today: a struct of `known` bool, zero `reset` as sentinel, two stores (in-memory `last`, JSON file), one precedence rule.
- Risks (each is a concrete failure the encoding allows):
  1. A 429 or error status that lacks a `ratelimit-remaining` header gives `q.known == false`. `observe` ignores it (`:119`), so nothing is stored in-process or on disk. `get` returns a `RateLimitError` (`context7.go:300-302`) but the next call, or the next process, sends another request that counts against the quota. The only test (`context7_keyless_test.go:124`) checks the message, not that it is remembered. This is the "short-circuit lives in one process but not the next" class again.
  2. In-process `last` shadows the state file whenever it is known. A long-lived MCP server holding `remaining: 5` ignores another process's spent record. Conversely, one holding `remaining: 0` with a far reset blocks for up to a month, because the 24h trust window (`context7_state.go:13`, "a machine that moved networks should not wait out a month") exists only for the file. CLI and MCP therefore behave differently on the same data.
  3. Check-then-act across a two-request call. `docsFor` does resolve then docs. If resolve takes the last request, `spent` refuses docs and the call fails after burning the quota (a rejected `accept` has the same cost, see machine 2).
  4. Read-modify-write on the state file: acknowledged lost update (`context7_state.go:35-44`), benign. Concurrent MCP calls can also overwrite `last` with an older response (`:118-124`), also benign.
  5. A keyed `ProbeContext7` does not call `observe`, so the doctor view for keyed use and the runtime view can disagree.
- Explicit form: `type quotaState struct{ Kind (unknown|available|spent|expired); Left int; Reset time.Time; Seen time.Time; Source (process|file) }`. `Admit(now) error` and `Record(resp)` are the only two operations. A 429 with no remaining header is recorded as `spent` with whatever reset can be derived (Retry-After, else a short local backoff). Both stores go through the same trust window.
- Agent-visible: on quota failure, structured `{reason: quota_spent, resets: T, fix: context7_api_key}` in the attempt list from machine 2, even when web's refusal is the headline error. Today only the keyless doctor prints this.

### 4. Discovery ladder and the deadline — MUST
- Where:
  - Ladder: `docs/web_discover.go:64-84` (discover), `:90-105` (discoverSitemap), `:111-116` (widenLLMS), `:120-138` (discoverLLMS), `:206-217` (withSphinxLinks), `:256-278` (withHubLinks), `:945-972` (sitemapLinks), `:1001-1030` (walk.fetch).
  - Fetching: `docs/web_fetch.go:111-127` (probe), `docs/web_searchindex.go:72-84` (searchIndex), `docs/web_version.go:161-183` (siteVersions).
  - Deadlines: `docs/web.go:62-80` (constants), `:500,538` (opTimeout), `:617` (cite), `web_discover.go:945-950` (sitemap budget), `web_resolve.go:664` (resolve 20s).
- States: source = {llms-full, llms.txt, sitemap(+nav), root page links, + hub expansion, + Sphinx index, + search index}. Completeness = {complete, cut by sitemap budget, cut by op deadline, cut by a probe failure}. This is never recorded.
- Transitions: "first source that answers wins", in fixed order. Every probe error (timeout, 5xx, off-host redirect, HTML stand-in, absent) returns `ok == false` (`web_fetch.go:119-125`) and is the same event as "file does not exist". So the descent is decided by latency as much as by site structure.
- Today:
  - The shared mutable `webSite` is passed across phases. `widenLLMS` re-enters `discoverSitemap` after `discover` has returned (`:111-116`, called at `web.go:694`), and later code depends on `site.llms`, `site.pages` and `site.index` being in a particular combination.
  - There is no single owner of "stop". Stop conditions are the 10s probe timeout, 8s sitemap budget, 45s op deadline, 5s cite timeout, 20s resolution timeout, `ctx.Err()` checks in `walk.fetch`, `siteVersions`, `fetchPages` and `rootPageFailure`.
- Risks:
  - A sitemap walk that runs out of its 8s budget returns the links read so far (`sitemapLinks` returns `walk.links`). `usefulSitemap` accepts any non-root page, so a truncated sitemap, perhaps another section or locale, is used as the whole site, with no signal. A slow host can also yield no links, so the same site is served from llms.txt on one run and from root-page links on the next.
  - Timeouts are stacked, not nested. `Search` and spec-based `GetDocs` run resolution under a 20s context and then `docsAt` under a fresh 45s one (`web.go:164,454-458`). The `opTimeout` comment (`web.go:52`, "whole GetDocs/ResolveLibrary call") is not true. By the constants, the web part alone can run about 65s before any c7 fallback.
  - `docsForURL`'s retry after `versionRedirect`/`movedRoot` reuses the same `opCtx` (`web.go:503-510`), so the second `collect` gets whatever the first left.
  - The sum of sequential probe timeouts across llms.txt (2 locations), llms-full, robots, sitemap, root nav, search index and root page exceeds 45s, so the op deadline, not the probe budgets, ends a slow site. When it fires mid-fetch, `fetchPages` still succeeds with the pages that arrived (machine 5) and `citeChecked` (which shares the dead context) silently strips verified anchors.
  - Auto adds Context7 requests (30s client timeout each) after all of this, and nothing bounds the whole call. The CLI and MCP apply no timeout to docs.
  - `searchPages` swallows search errors (`web.go:795-797`), so "search failed" and "search found nothing" are one state, and the weak-coverage guesswork path is never reported. This is the descendant of "a search hit's failure counted as a package's failure".
- Explicit form: a `deadline` object owned by the request (one budget with `Remaining()` and named sub-budgets) and a `discovery` result `{Source, Completeness, Probes []{URL, Outcome}}`. Probe outcomes are an enum: ok | absent | timeout | error | html_stand_in | off_host. `discover` decides stop and fallthrough in one place.
- Agent-visible: `source: llms-full|llms|sitemap|root-links`, `coverage: complete|partial(reason)`, and the `tried` list that `ErrNotFound` messages already contain (`web.go:692`) on successful answers too. A tester or agent can then tell a truncated-site answer from a whole-site answer.

### 5. Candidate fetch and partial success — SHOULD
- Where: `docs/web_fetch.go:134-173` (fetchPages), `:181-218` (fetchOne); `docs/web.go:726-761` (fullTextPages), `:683-700` (collect).
- States: all candidates fetched | some failed | all failed | (fullText) fetch of missing pages failed.
- Transitions: the only failure is `len(out) == 0 && failure != nil` (`:169`). Which failure is reported is chosen by the `failure == nil || errors.Is(failure, ErrNotFound) && !errors.Is(errs[i], ErrNotFound)` expression (`:165`).
- Today: `fullTextPages` ignores the `fetchPages` error (`web.go:753`) and, if nothing was chosen, returns all of `site.pages` (`:757-759`), i.e. ranks the whole llms-full text instead of the chosen pages (a different corpus, see machine 6).
- Risk:
  - The top candidate, the page the link/index ranking trusted most, can be the one that timed out. The answer then comes from lower-ranked pages and looks confident. The user gets no sign that the best page was skipped.
  - The deadline in machine 4 produces the same shape.
- Explicit form: `fetchPages` returns `FetchReport{Pages, Failed []{Link, Cause}, Complete bool}`; collect passes it through to the outcome of machine 1.
- Agent-visible: `pages_considered`, `pages_failed`, `partial: true`. Rank-1-failed could also be a hard refusal reason.

### 6. Rank → cut → shape pipeline — SHOULD
- Where: `docs/web.go:550-560` (rankPages) composes `diversify(dedupe(relevant(rankSections(...))))`, then `budgetSections`, `citeChecked`, `labelResults`, then `Truncate` in the caller (`web.go:168`). Detail: `web_rank.go:733-772`, `:1033-1097`, `:1111-1153`.
- States: raw sections → scored → cut → deduped → diversified → budgeted → cited → labeled → truncated. Parallel state: `used []webSection` must be index-aligned with `results` (`web.go:577`).
- Transitions / order constraints, enforced only by call order:
  - `relevant` assumes a score-sorted input; `diversify` then breaks the sort (nothing downstream may assume it).
  - `citeChecked` must run after `budgetSections` and uses its `used` slice.
  - `Truncate(limit)` runs after `citeChecked`, so pages are fetched for results that are then thrown away.
  - `relevant` has only a relative floor (`web_rank.go:1079-1097`: 30% of the top score), and `ranked[0].score <= 0` returns everything.
  - A query with no terms (e.g. only the library name, remainder empty) keeps every section in page order (`web_rank.go:736-741`): an unnamed "overview" mode.
  - The first section is returned however weak (`web_rank.go:1128-1137`).
- Risk:
  - BM25 here is computed over the 1-6 fetched pages (`newSectionCorpus`, `web_rank.go:962`), and over a different corpus (all llms-full sections, or the whole search index) when picking pages (`web.go:727-744`, `web_searchindex.go:268`). The score is not comparable across queries or sites. Both the 0.3 ratio and any absolute threshold on this number are unsound; the embedding floor must be a separate gate on a calibrated scale, applied before `relevant`.
  - A weak top section is returned with no marker. Truncation is signalled only by text inside the snippet (`"\n[truncated]"`, `web_rank.go:1155`).
- Explicit form: `shape(sections, query, opts) Shaped{Kept []ScoredSection{Score, Reason}, Dropped []{Section, Why: cutoff|duplicate|budget|limit|below_floor}, Top float64}`, with the floor as the first stage. Limit is applied inside, before citation checks.
- Agent-visible: per-result `score` or `confidence` band and `truncated: bool`; call-level `dropped: {cutoff: n, budget: n}`; `mode: overview` for the no-terms case.

### 7. Citation / anchor knowledge — SHOULD
- Where: `docs/web.go:578-611` (citeChecked, unchecked), `web_sections.go:15-40` (anchorSource), `:926-963` (verifyAnchors, `if anchors == nil { return }`), `web_fetch.go:216` (markdown flag), `internal/docsfetch/docsfetch.go:108,122-131` and `cache/cache.go:56-66` (nil vs empty preserved on purpose).
- States: page-anchor knowledge = unknown (nil) | known-empty | known-with-ids; per section: guess | rule | fixed; per page: from-Markdown | from-HTML; plus whether the check fetch succeeded.
- Transitions: `unchecked(p)` is `p.markdown && p.Anchors == nil`. `citeChecked` re-splits the page with checked anchors and matches by `order` index (`web.go:601`), valid only because splitting drops sections independently of anchors.
- Today: a nil-vs-empty slice and a bool, held together by one comment and a cache field being a pointer to a slice. `verifyAnchors` sets every heading `anchorFixed` even when the anchor fell back to the parent's (`web_sections.go:959-961`), so "fixed" means "valid to cite", not "this section's own anchor".
- Risk:
  - If a fetcher returns nil anchors for an HTML page (fallback fetcher, or a cache layer that drops empty slices), `markdown` is false, `unchecked` is false, and guessed slugs are cited unchecked. The shipping `docsfetch` refreshes in that case, so this is latent, not live.
  - The cite check shares the call's context (machine 4), so a late call loses its anchors.
- Explicit form: `type anchorKnowledge {Unknown, None, Known}` on the page, and a `CitationQuality {Section, Parent, Page}` on the result.
- Agent-visible: `anchor: section|parent|page` (decision-9 requires verified citations; today the agent cannot tell a section-level citation from a page-level fallback).

### 8. Version source ladder — COULD (SHOULD at the resolution boundary)
- Where: `docs/web_version.go:161-183` (siteVersions), `:189-213` (versionSources), `:1554-1565` (labelResults).
- States: versions known from source X | no versions | source failed. Transitions: first source returning non-empty wins; a source error is skipped (`:170`) and, with `ctx.Err() == nil`, the next source is tried.
- Risk: "source failed" and "site publishes none" collapse to a zero `versionSet`. A pin like `@3.9` resolved against an empty or truncated list is the resolution area's problem, but the information needed (why it was empty) is lost here. `labelResults` stamps the root's label on every result without checking the page (`:1556`), so Version is a claim, not evidence. I read the ladder only, not the pin matching.
- Explicit form: `versionSet.Status {Listed(source), None, Unavailable(reason)}`.
- Agent-visible: `version_source` and `version_verified: bool` on results.

### 9. Error-class mapping (two copies) — SHOULD
- Where: `cmd/exit.go:60-69`, `mcp/errors.go:136-147`, `main.go:30`, `docs/web.go:667-677` (callErr), `docs/web_resolve.go:703-715` (finishErr), `docs/context7.go:296,202`.
- States: validation | not_found | upstream | cancelled, plus precondition for constructors.
- Transitions: `errors.Is` chain on sentinels, copied twice. Cancellation handled differently: CLI exit 6 only for `context.Canceled` (`main.go:30`); MCP also for `DeadlineExceeded` (`mcp/errors.go:139`).
- Risks:
  - Web strips the deadline chain from its own timeouts (`callErr`, `finishErr`) so a slow site is `[upstream]`. Context7 has no such step. Its client timeout (30s, `httpx/client.go:15`) satisfies `errors.Is(err, DeadlineExceeded)` (verified), so `--backend context7` timing out is `[cancelled]` on MCP and exit 4 on the CLI. That breaks ADR-0002's lockstep rule and the "retry" signal.
  - `ErrNotFound` covers genuinely different things: no registry match, a root with no pages, an off-root redirect, a common-word reading, a Context7 404. All are "stop". Some of them (a slow root page) deserve a retry.
  - An empty answer is exit 0 (machine 1).
- Explicit form: `docs.Classify(error) Kind`, implemented once in the docs package (the packages already say "detected here so the surfaces can't diverge", `docs/docs.go:10-14`), with the Context7 timeout normalised like web's.
- Agent-visible: the kind plus `retryable` and, for quota, `retry_after`.

## Already explicit
- Ambiguity: `AmbiguousLibraryError` (`docs/docs.go:34-65`) is a typed error with candidates; both surfaces classify it without a rule of their own.
- Quota error: `RateLimitError` (`docs/context7_quota.go:19-49`) carries Keyed, Limit, Reset.
- Provider selection: `Selection`/`BackendError`/`SelectingSearcher` (`auto.go:20-43`) name the answering provider. Incomplete only on failure (machine 2).
- Anchor provenance per section: `anchorSource` enum (`web_sections.go:34-40`).
- llms-full attribution: `chunkAttribution` enum (`web_discover.go:670-677`) and the `fullSplitter` page splitter (`:734-826`) are contained, local state machines.
- Out-of-scope responses: `scopeError` (`web.go:1116-1131`) is a typed refusal that wraps ErrNotFound.
- Per-host probe policy: `discoveryProbes` (`web_discover.go:162-178`).
- Sitemap caps: `sitemapWalk` file/URL/depth counters are bounded in one function.
- Fetch scope: `servedFrom` + `webRoot.contains` is the single scope check used by probe, fetchOne and pageAnchors.

## Cross-area edges
- resolveQuery/resolveOne (`web_resolve.go`) → `docsAt(root, query, tokens, version)`: hands over a `webMatch` reduced to a root URL and a version string. The ambiguity, veto and strict-miss states stay in the resolution area; this area sees only the resulting error or root. The root can also come from the resolution cache (`web_rescache.go`), so a cached wrong root is never re-verified here.
- `docs.Fetcher` → `internal/docsfetch` → `scrape`/`cache`: page cache TTL, browser fallback, and the nil-vs-empty anchor contract (machine 7) live outside this package. A JS-shell browser render runs under the same context deadline and is not budgeted separately.
- `docs.Auto` → `docs.Context7` and `context7State` (cache dir file); `ketch doctor` reads the same file via `ProbeContext7` / `context7KeylessStatus` without spending a request.
- `searchPages` → `search.Auto` (the web search providers): search failures become nil here (machine 4).
- `cmd/docs.go` / `mcp/docs.go` consume `Selection` and `error`; the refusal-floor outcome must be added to both and to `cmd/exit.go`, `mcp/errors.go`, ADR-0002.
- Board: decision-9 (verified citations, complete response) and decision-13 (auto rule, "empty is an answer") constrain the outcome design in machines 1, 2 and 7. Decision-14 and 16 constants (0.3 cutoff, 0.9 sibling tie, 0.25 coverage) are dev-tuned; the floor should not reuse them.
