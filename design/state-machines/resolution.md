# Docs resolution (keyless web backend)

Audited at 537d96f (feat/web-docs, clean). Line numbers are for that commit.

Scope note: board decisions 17 and 18 are NOT encoded at this commit. `namedProjects`, `pickNamed`, `withMovedRepos` and `evPkgReadme` do not exist in the tree (grep finds none). Their code lives in the r3a/r3b worktrees, which I did not read per the brief. What is encoded is decision-15 (current at the commit) plus the decision-12 and decision-8 rules it kept. Decision-17 and 18 will add rival kinds (mapped package, deprecated namesake) and a redirect-GET budget to machines 1 and 9 below. Today deprecated npm namesakes are simply dropped (web_resolve.go:1241).

## Summary

14 machines found: 5 MUST, 7 SHOULD, 2 COULD. Most of the risk sits in one cluster: what happens to a project that did not become the pick. Its fate is spread over `resolveLog` side tables, `project` flags and three separate gates (`rivals`, `rivalOf`/`rivalBlocks`, `dominant`).

The worst one is machine 1 (rival verdict). The ecosystem-hint exemption in `rivalOf` (web_keyless.go:465) is the inverse of decision-12 rule 1 when the rival's download count could not be read. That is the same shape as the earlier hint-overrides-popularity bug, on the failure path.

Two more safety issues: a warm resolution cache changes contest outcomes (machine 2), and version filtering runs before the pick, so a transient version-root failure can hand the pick to a lesser project (machine 5). Three machines are agent-visible only as free-text evidence notes that `Search` and `GetDocs` drop (3, 5, 8).

## Machines

### 1. Rival / non-pick project verdict — MUST
- Where:
  - Collection: `project` (web_resolve.go:50-83), `resolveLog.missed/offeredSite/projects` (272-295), `consider/offer/failFor/failRepo` (360-376, 435-490).
  - Gate A, `rivals()` (410-423), called from `answerPastFailures` (922-966): "unanswered stronger project".
  - Gate B, `keylessRivals` / `rivalOf` / `rivalBlocks` / `outweighs` (web_keyless.go:372-469): "exact-name package that verified nothing".
  - Gate C, `unreadBeside` (web_resolve.go:445), token mode only.
  - Gate D, `dominant` (3139-3169) and `pruneDominated` (3091).
  - Gate E, `applyHint` (web_keyless.go:481-509).
  - Feeding it: `readTieBreak` (web_keyless.go:312-344).
- States of a non-pick project (nowhere named):
  - verified (in `matches`);
  - verified, index-hosted only;
  - offered a site of its own, site did not answer;
  - offered a site, rejected by the edge rule;
  - offered only index docs;
  - offered no docs;
  - repository unread by GitHub (token mode);
  - download count unread (`popErr`);
  - made from a search hit under another name (never a rival);
  - cut off by a budget (never recorded, see machine 9);
  - ecosystem-hinted out.
- Transitions: decided independently by A, B, C, D and E, each with its own threshold:
  - A: rival stars >= pick stars, or `popErr`, or any rival when `pick.indexOnly`. Result: upstream error.
  - B: `pick < 50x rival` for rivals with a site of their own, `pick <= rival` for weak rivals. Result: ambiguity.
  - D: >= 1000 and 50x, with `indexOnly` and `starsUnread` special cases.
- Today: bools and sentinels in combination: `exact`, `fromHits`, `fromSearch`, `repoGone`, `repoErr`, `popKnown`, `popErr`, `indexOnly`, `starsUnread`, plus membership in the `matched` map, `offeredSite` and `missed`.
- Risk, concrete:
  1. Hint exemption on an unread weight. `rivalOf` drops a rival outside the hint when `!p.popKnown || !outweighsBy(...)` (web_keyless.go:465). `!p.popKnown` is true exactly when the rival's download read failed (`readPopularity` sets `popErr`, web_popularity.go:188-190). So a failed read of a far more popular same-name package is exempted by the hint, and `outweighs()` (which would raise the upstream error) is never reached. Decision-12 rule 1 says the hint is "not applied when a weight is unknown", and `applyHint` does that (web_keyless.go:501). There is no test for hint plus unread rival (web_keyless_test.go:143, 688 cover only known weights).
  2. `rivals()` ignores the hint while `rivalOf` honours it. The same out-of-hint rival can block through one gate and not the other.
  3. One unanswered rival goes through two gates with different thresholds. If its stars are >= the pick's, the result is an upstream error (exit 4). If its stars are lower but the pick is under 50x, the result is an ambiguity titled "docs not verified" (web_keyless.go:411), although the site was down, not unverified.
  4. Gate E and gate D are order-sensitive (`applyHint` runs before `pruneDominated`, web_resolve.go:2976-2977), by convention only.
- Explicit form: `type Verdict` per considered project: `Picked | Verified | SiteDown{err} | RejectedByEdges{n} | OfferedNoSite | IndexOnlyOffer | RepoUnread | WeightUnread | NotChecked{budget} | OutOfHint | NotARival`. Plus one `func weighAgainst(pick Verdict+Weight, rival Verdict+Weight, hint) Outcome` returning `Stand | Note | Ambiguous | Upstream`. It owns the 50x, 1000 and hint rules and replaces A, B, C and E.
- Agent-visible: `rivals: [{id, verdict, weight, weight_state}]` on every ambiguity and upstream refusal, instead of free text ("docs not verified, N downloads a week"). The agent can then tell "popular but unverifiable" from "popular but its site was down".

### 2. Weight (stars / popularity) lifecycle — MUST
- Where:
  - `project.stars()` (web_resolve.go:155-165), `weightUnread` (172).
  - `webMatch.stars/starsUnread` snapshots (verifyRoot 2224; assemble 2957).
  - `readPopularity` (web_popularity.go:174-200), `weekly` memo (92-121).
  - `readTieBreak` (web_keyless.go:312), `weigh` (web_resolve.go:897), `contest` (web.go:313-318), `pickWeight` (web_keyless.go:756), `commonWordBar` (web.go:348).
  - `cachedResolution.Weight *int` (web_rescache.go:24, 86-93).
- States of a weight, all collapsed into `-1` or `0`:
  - not yet read (keyless reads lazily);
  - nothing to count (no repository, GitLab);
  - GitHub or registry failed (`popErr`, `repoErr`);
  - repository gone (counts 0);
  - registry 404 (counts 0, web_popularity.go:150);
  - known n.
- Transitions: reading happens in `readTieBreak` only if >= 2 projects, in `contest` only if `proj != nil`, in `weigh` only for common-word readings, in `pickWeight` only if `!popKnown && info == nil`. `webMatch.stars` is a snapshot taken at three different times.
- Today: `-1` plus a separate `starsUnread` bool that is recomputed in `assemble`. Order is enforced by a comment (web_resolve.go:2955): `readTieBreak` must precede `assemble`.
- Risk, concrete: cold and warm cache give different contest results. A cached pick comes back with `proj == nil` and `stars == -1` for any non-common reading (`weigh=false`, web.go:828; `loadResolution`, web_resolve.go:838; web_rescache.go:77-79). `contest` only reads popularity when `matches[i].proj != nil` (web.go:314), so it can never weigh a cached pick. `readingOutweighs` is then false (web.go:335) and the query is ambiguous between readings. The same query cold, with both weights read, could stand on 50x. `pastStrictMisses` has the same gap: `pickWeight` returns -1 for `proj == nil`. The cache therefore changes decisions and persists across processes (page cache with TTL, internal/docsfetch/docsfetch.go:42-60).
  - Smaller: a pypistats or npm 404 means "known 0" (web_popularity.go:150), indistinguishable from a real zero.
- Explicit form: `type Weight struct{ State {Unread, NoSource, Gone, Failed, Known}; N int64; Err error }` on `project`, copied (not recomputed) into `webMatch`, serialised into the cache entry. One `ensureWeight(ctx, *project)` owns reads. `dominant` takes `Weight`, not `int` plus two bools.
- Agent-visible: `weight: {state, value, unit: "stars"|"downloads/wk"}` on matches and on every ambiguity candidate.

### 3. Reading outcome and veto (errors as control flow) — MUST
- Where:
  - `resolveQuery` (web.go:195-236), `readingStop` (260-272), `vetoes` (383-398), `settledBy` (277), `contest` (300), `commonWordBar` (348).
  - `strictMissError` (web_keyless.go:662-712), `pastStrictMisses` (728-752), `upstreamError{registryFailed,nameKnown,siteFailed,picked}` (web_resolve.go:338-346).
  - `finishErr` (709).
- States of a reading's result: `Answered`, `Ambiguous`, `Miss{exact packages}`, `NotFound`, `UpstreamVeto`, `UpstreamNoVeto`, `ValidationNone`, `CommonWordBelowBar`.
- Transitions: decided by `errors.As` chains (`AmbiguousLibraryError`, `strictMissError`, `upstreamError`), the flags `strict` and `common`, `len(picks) > 0`, `readings == 1`, and four bools on the error. `vetoes` is a truth table over (strict, errtype, registryFailed, picked, nameKnown && siteFailed, readings == 1).
- Today: the type is the state, so every consumer re-derives it.
  - `siteFailed` is a union of five unlike things: homepage fetch failure, docs-root fetch failure, popularity-API failure (`readTieBreak` calls `failFor`, web_keyless.go:339-341), repo-package lookup failure (`addRepoPackage` calls `failFor`, web_resolve.go:1141), and a resolution timeout (`upstreamErr(..., timedOut)`).
  - The stopping error is rebuilt with `%s`, which drops the typed chain (web.go:267).
- Risk, concrete:
  1. A guessed (strict) reading that is ambiguous stops the whole query. `resolveOnce` returns the `pickUnique` ambiguity before the guess-proof gate runs (web_resolve.go:861-864 vs 873), and `readingStop` stops on any ambiguity unless the reading is common (web.go:264). The decision says a guess never vetoes, and `vetoes` enforces that for upstream errors only. Rare, because junk packages seldom verify two edges, but there is no test.
  2. Nothing answered collapses to `ErrLibraryRequired` (exit 2, "pass a library"), even when a non-vetoing reading timed out (web.go:224-227). The agent cannot tell "retry, a source was slow" from "the query names nothing". The failure text is embedded, but the class is lost.
  3. Veto depends on `nameKnown`, which is set by any registry hit in that one resolution. A timeout while verifying a site is therefore only a veto if some registry knew the name.
- Explicit form: `type ReadingResult struct{ Kind ReadingKind; Match *webMatch; Miss []*project; Ambiguous []LibraryMatch; Fault Fault }` and `Fault{Class: RegistryDown|PickedPastSource|SiteDownForKnownName|Timeout|Other; Source string}`. `func decide(results []ReadingResult, readings []querySplit) QueryOutcome` is the only place that applies veto, settle, contest and strict-miss rules, returning `Answer | Ambiguous | Refused{reason}`.
- Agent-visible: `refused_because: "ambiguous" | "no_library_named" | "source_unavailable{source}" | "unverified_namesake{id}" | "below_common_word_bar"`, plus the readings tried with each reading's result. Today `tried` is free text.

### 4. Resolution cache admission — MUST
- Where: `resolveOnce` (web_resolve.go:852-881; `cacheable = len(failed)==0`), `storeResolution` (web_rescache.go:86), `resolutionKey` (29), `resolutionCacheVersion` (11).
- States of a pick: `ClearPick`, `PickWithNote`, `PickDegraded`, `PickDegradedSilently`.
- Transitions: only `log.failed` (appended by `failSource`, `failFor`, `failRepo`) blocks caching.
- Today: swallowed failures never reach `failed`, so a pick made on a degraded candidate set is stored for the page-cache TTL:
  - `pkgsiteRepo` returns "" on any error (web_pkg.go:929-940), so the module silently has no repository.
  - `realGoModule` treats a pkg.go.dev error as "not a Go module" and logs only a reject (web_resolve.go:1166-1169).
  - `hitPackageProject` and `addRepoPackage` for hits log rejects only (web_keyless.go:297-301; web_resolve.go:1135-1139).
  - The keyless web search failing is `reject` only (web_keyless.go:42), as is `verifyHit` for a hit that did not answer (web_resolve.go:2837).
  - `linkedProjectKeyless` discards its log (2759).
- Risk: a transient pkg.go.dev blip drops the real Go module, a lesser project becomes the unique pick, and that is cached as "resolved earlier and cached". Also, `resolutionCacheVersion` is bumped by hand when rules change; a missed bump serves picks made under old rules.
- Explicit form: `type Completeness {Complete, Degraded{sources}}` returned by the resolution. Cache only `Complete`. Every "soft" drop must either set `Degraded` or be documented as non-material.
- Agent-visible: `Selection`/`Result` gains `resolution: {complete: bool, unchecked: [...]}`. The existing "not checked, so another library of this name may be missing" note (web_resolve.go:956) is currently lost on `Search` and `GetDocs`, because `docsAt` is given only `root` and `version` (web.go:164, 458).

### 5. Version selection and the version filter before the pick — MUST
- Where: `finish` (web_resolve.go:3174-3199), `describe` (3204-3224), `pickVersion` (3324-3347), `registryVersion` (3350), `adoptVersion` (3370), `assumeLatest` (3444), `probeVersion` (3471), `docsRSBuilt` (3643).
- States: requested version resolved by `Listed | DottedLabel | AssumedLatest | ProbedLayout | NearestBuilt | NotFound | FetchFailed`. Today a free-text `evNote` is the only record (3449, 3506, 3664).
- Transitions: a fixed ladder of nested ifs. `finish` runs inside `resolveRun`, before `pickUnique`, and keeps a match only if `errs[i] == nil`, with no distinction between "version not offered" and "fetch failed".
- Risk, concrete:
  1. A transient fetch failure of the stronger project's version root (`adoptVersion` returns `fmt.Errorf("fetching ... failed")`, 3381) removes its match. If another non-dominant project remains, it becomes the unique pick. This is decision-8's "a lesser project never wins because a stronger one's site was briefly down", reintroduced at version time. It is never logged to `resolveLog`, so `answerPastFailures` cannot see it, and the pick is cacheable.
  2. If the project's registry match survives (docs.rs has the version), the pick silently moves from the project's site to index-hosted docs (`pickUnique` takes the first kept match per project, 3796-3800), and that is cached.
  3. Substitutions (nearest built docs.rs release, assumed latest) are visible only as notes, which `Search` and `GetDocs` drop.
- Explicit form: `type VersionBasis {Listed, AssumedLatest, ProbedLayout, SubstitutedBuilt{asked, got}}` stored on `webMatch`; `finish` returns per-match `VersionResult` and reports `FetchFailed` through `resolveLog` so the rival gates see it.
- Agent-visible: `version: {requested, served, basis}`. `Result.Version` already exists (docs.go:80), but there is no way to tell exact from substituted.

### 6. Reading kind flags (`strict`, `common`, `lean`, plus the speculative context key) — SHOULD
- Where: `querySplit` (web_types.go:51-70), `leanCommon` (web_spec.go:429-439), `add` (644-657), `resolveReading` (web_resolve.go:824-829), `resolution.strict/lean` (612-631), `searches()` (692), `withSpeculative` (web_pkg.go:369, set at web_resolve.go:668-670), `resolutionKey` (web_rescache.go:36-44), `readingStop`/`vetoes` (web.go).
- States: 4 valid of 8 bool combinations: `Named`, `Common`, `LeanCommon`, `Guess`. The `speculative` context key is a 5th copy of "guess or lean".
- Today: `lean` implies `common`, and `strict` excludes `common` (`add` sets `common: !strict && ...`), but the types allow the invalid combinations. `lookupRegistries` additionally narrows a speculative reading to the hinted registries (web_resolve.go:1258-1261), including the working-directory hint.
- Risk: a new reading kind needs edits in about 9 places. The cache key does not carry `common` (benign now, since the weight is stored).
- Explicit form: `type ReadingKind int` with `Named, Common, LeanCommon, Guess` and methods `Searches() bool`, `Retries() bool`, `Vetoes() bool`, `MayStopQuery() bool`.
- Agent-visible: each reading tried, with its kind, in the refusal (see machine 3).

### 7. Keyless vs token mode, and upstream quota/backoff policy — SHOULD
- Where:
  - Mode bit `w.gh() == nil`, recomputed at 11 sites: web_resolve.go:671, 837, 856, 868, 888, 1020, 1061, 1559, 1651, 2206, 2719. `r.keyless` is a second copy (671, 728, 2836).
  - Quota/backoff: ghClient (web_github.go:132-195, no retry), `resolveLog.limit` (web_resolve.go:285, 499-512), `get` (web_pkg.go:406-440, 1 retry, 300ms to 2s), `getPatient` (295-325, 5 attempts, 1 to 5s), `turns` (334-362), `popularityClient.memo` (success-only), `ghRateLimited` heuristics (web_github.go:215-222).
- States: {keyless, token, token-with-GitHub-spent}. The third is not a mode; it yields upstream errors rather than degrading to keyless (enrichOne marks `repoErr`, web_resolve.go:1579-1583).
- Transitions: GitHub's spent-quota fact is per `resolveLog` (one per `resolveOnce`), so the 3 readings of a bare query and every later process re-probe GitHub until each hits its own 403. The client keeps nothing; `reset` is parsed but never used to skip. Context7 has the opposite: a process-wide tracker plus a cross-process state file (context7_quota.go:104-175). This is the class of the earlier "spent quota short-circuit lived in one process but not the next".
- Other policies:
  - Four retry policies for four hosts, chosen at call sites.
  - `getPatient` applies no per-call timeout (`get` does, 408-412), so turn-queue time and the 5-attempt backoff (up to about 12s while holding the slot) are charged to the caller's deadline: 8s for popularity, 15s for the Go search, 20s for the whole resolution.
  - The slot is held across retries, so one throttled pkg.go.dev request starves later ones into `popErr` or `failSource(registry)`. Both then veto or block the pick (web_resolve.go:927-936).
  - `ghRateLimitError.authed == false` branches (web_resolve.go:960, 3763) are unreachable in production now, because GitHub is only asked with a token.
- Risk: starvation is reported as "downloads could not be read" or "registry failed", indistinguishable from the service being down. It is the main source of pkg.go.dev 429 refusals noted in decision-17.
- Explicit form: `type Upstream struct{ name; policy RetryPolicy; budget; state QuotaState{Ok, Throttled{until}, Exhausted{reset}} }`, one per host, process-wide, optionally persisted like Context7's. Each call asks `u.Do(ctx, req)`. Resolution reads `Upstream.State()` instead of its own `rateLimited()`.
- Agent-visible: `sources: [{name, state: ok|throttled|exhausted, retry_after}]` on refusals; "mode: keyless|token" on the resolution.

### 8. Evidence regimes: edge count vs trust points — SHOULD
- Where: `score` and `verifyingKinds` (web_trust.go:30-61), `edges`/`keylessVerified` (95-145), `keylessEvidence` (web_identify.go:272-302), `verifyRoot` (web_resolve.go:2206-2222), `rootEvidence` (2416), `v4Verdict` (2501).
- States of a root: `Registry`, `TwoEdges`, `OneShort`, `Unverified`, `HitNeedsIdentity`.
- Transitions: keyless runs `keylessEvidence` (edges) and then `score` (trust points) on the same evidence. The result is the AND of two rules written for different modes.
- Risk: a search hit or guessed root on the project's own domain that links the registry package page (not GitHub) gets `evRelatedDomain` + `evLinksPackage` = 2 independent edges, but `score` requires `evLinksRepo` beside `evRelatedDomain` (web_trust.go:60), so `ok` is false and the root is rejected with "not verified (trust N)". Decisions 12 and 15 describe the package link as a valid site edge. I found no test for that shape (inferred from code; not run). The rejection reason names trust, not the missing edge.
- Explicit form: `Verify(evs, mode) Verdict{Verified{edges}, Short{have, need}}`, with trust points kept as display only.
- Agent-visible: `edges: {meta, site_link, domain, registry}` with which are missing, rather than a float.

### 9. Budgets and stop conditions — SHOULD
- Where:
  - Constants: web.go:62-80, web_resolve.go:29-46, web_keyless.go:27-28, 571-575, web_pkg.go:40.
  - Consumers: `resolveRun` (web_resolve.go:664, 20s per reading, not per query), `verifyAll`/`schedule`/`r.roots` (1722-1758, 1811), `candidateWait` 4s with a late-arrival share (1739-1755), `searchHits` 10s (2914), `goNameProjects` 15s (web_keyless.go:594), `readPopularity` 8s (web_popularity.go:184), `failure` (3740-3768), `finishErr` (709), `ctx.Err()` checks (web.go:201, web_resolve.go:3741, 3746, 3754), and a manual `r.roots = min(...)` reset (web_resolve.go:744).
- States: `WithinBudget | RootsExhausted | ProjectsTruncated (webMaxProjects 5) | DeadlinePartial | DeadlineEmpty`.
- Transitions: a deadline that fires with some matches already verified does not call `failure` (only `len(matches)==0` does, web_resolve.go:676). The pick proceeds on a partial candidate set; per-project failures are recorded via `failFor` for roots that were in flight, but roots never scheduled (budget) and projects beyond 5 are not recorded at all.
- Risk: "unverified because unchecked" and "unverified because rejected" look identical to machine 1, so a project cut by `webMaxRoots` or `webMaxProjects` can be reported as "docs not verified" in an ambiguity, or ignored entirely if it is not an exact-name package. `Search` has no whole-operation deadline; resolution (20s per reading) + contest + `pastStrictMisses` popularity reads (8s each) + `docsAt` (45s) are independent.
- Explicit form: a `Budget` object owned by the resolution (roots, projects, deadline, per-source slices), whose `Spend()` returns `OK | Exhausted{what}`, and which records `NotChecked{budget}` verdicts (machine 1) when it refuses.
- Agent-visible: `not_checked: [{id, reason: "budget"|"deadline"}]`.

### 10. Ecosystem hint — SHOULD
- Where: `queryEcosystems`/`workdirEcosystems`/`ecoHint` (web_hint.go:28-96), `searchQuery` (web_keyless.go:110-119), `lookupRegistries` (web_resolve.go:1256-1270), `applyHint` (web_keyless.go:481-509), `rivalOf` (459-469), `resolutionKey` (web_rescache.go:29-45).
- States: hint provenance is {explicit query word, ambient working directory}. They are merged into one `[]ecosystem`. Only `searchQuery` still separates them (reads `ecoHintKey` directly, honouring decision-12 rule 2). `applyHint` and `rivalOf` treat them equally.
- Transitions: three call sites each re-implement "settle a contest without overruling dominance" with different predicates (see machine 1, risk 1 and 2). The doc comment "A hint naming every registry is none" (web_hint.go:85-86) is not implemented.
- Risk: the earlier hint bug class. A working-directory hint with the same authority as the user's words is the cause of the original 4-wrong-answer regression; the code now avoids it for search only.
- Explicit form: `type Hint struct{ Eco ecosystem; Source {Query, Workdir} }` and a single `hint.Settles(pick, rival Weight) bool`.
- Agent-visible: `hint_applied: {ecosystem, source, set_aside: [...]}`. `applyHint` already writes it to the reject log, which is not user-visible.

### 11. Candidate root kind (`registry`, `search`, `guess`, `offsite`) and search-hit failure — SHOULD
- Where: `rootCandidate` (web_resolve.go:213-238), `candidateSet.add/addOffsite/addGuess` (1846-1933), `verifyRoot` (2182-2229), `rootEvidence` (2416-2447), `keylessEvidence` (web_identify.go:272-302), `verifyHit` (web_resolve.go:2829-2843), `webMatch.fromHit/registry`.
- States: 5 valid of 16 combinations: `Registry`, `Metadata`, `Offsite`, `Guess`, `SearchHit`; the sixth, `Metadata+namedBy`, is a field not a flag. `add` clears `guess` and `offsite` when something names the root (1853), a manual transition.
- Risk: decision-12 rule 3 ("a search hit that does not answer is a rejection, never a failed source of a package") is encoded as `r.keyless && !errors.As(err, &rej)` (web_resolve.go:2836). In token mode `lastResort` still calls `r.noteRejection(p, err)` (2840), which does `failFor(p, ...)`. A search hit's 403 or timeout therefore marks the project's site as failed, which can block the pick through gate A (machine 1). This is a descendant of the earlier "search hit's failure counted as a package's failure". It remains for token users.
- Explicit form: `type CandidateKind` plus a `Failure.Subject{Project, Candidate}` so a hit's failure cannot be attributed to a project.
- Agent-visible: minor; the reject log shows which candidate failed.

### 12. Pick-gate pipeline order — SHOULD
- Where: `resolveOnce` (web_resolve.go:852-881): `pickUnique` -> `answerPastFailures` -> `keylessRivals` -> strict-guess gate -> `weigh` -> cacheable. Keyless population: `verifyAll` -> `readTieBreak` -> `assemble` (`applyHint` -> `pruneDominated`) -> `finish` (web_keyless.go:55-78; web_resolve.go:2950-2983).
- States: `Candidates -> Verified -> Weighed -> Pruned -> Described -> Picked -> Gated -> Weighed-for-contest -> Cached`.
- Today: shared mutable `project` (pop, popErr, pkgs appended by `addRepoPackages`, exact) and a `resolveLog` used as a bus. Comments, not types, enforce the order (web_resolve.go:2955; web_keyless.go:306-311).
- Risk: reordering `readTieBreak` after `assemble` silently turns every keyless contest into `-1` and therefore ambiguity. The strict-guess gate runs after the rival gates, so a guess can be refused for rival reasons instead of proof reasons. This is why machine 3 risk 1 exists.
- Explicit form: `type Stage` with a `func (r *resolution) advance(Stage)` that asserts the previous one. The new answer lifecycle (local index -> web -> Context7) wants the same type one level up.
- Agent-visible: the stage at which a refusal happened (`refused_at: "identify" | "verify" | "weigh" | "pick" | "version"`).

### 13. Token-mode escalation ladder in `run` — COULD
- Where: `run` (web_resolve.go:727-754): registries -> GitHub search if `anyExact && (!hasSite || !settled)` -> web search if `!hasSite`.
- States: three rungs with entry predicates and a manual budget reset (744). The keyless path is a different, parallel strategy chosen by `if r.keyless` (728).
- Risk: low for correctness; the point is that this is an ad hoc ladder of the shape the answer lifecycle will formalise.
- Explicit form: `[]Rung{Run, Enter(state) bool}`.
- Agent-visible: `rungs_tried`.

### 14. `strictMissError` lazy weighing — COULD
- Where: web_keyless.go:662-712. `weighed bool` guards fields (`spec`, `repo`, `pop`) whose zero values mean "not yet" as well as a real 0; `weigh` must be called first and `pastStrictMisses` does (736).
- Explicit form: constructor-time or `Weight` type (machine 2).

## Already explicit
- `ecosystem` enum and `libSpec` parse (web_spec.go:55-92): closed set; validation errors typed (`ErrLibraryRequired`).
- `AmbiguousLibraryError` (docs package): a typed result carrying candidates; it is what auto-chaining already consumes (`webCandidates`, auto.go:306-318).
- `evidenceKind` / `edgeClass` tables (web_trust.go:30-123): enumerated and data-driven (the composition is machine 8).
- Query reading by syntax (web_spec.go:408-640): a pure function of the query to `[]querySplit`, order of rules fixed in `wordSplits`.
- `turns` slot (web_pkg.go:334-362) and single-flight memos (`profileMemo` web_resolve.go:556-609, `popCall`, `llmsCall`): correct local primitives; the gap is their policy, not their mechanics.
- `resolveLog` is append-only and mutex-guarded; the problem is its flags, not its concurrency.
- `dominant` is a pure function of `webMatch` (the inputs are machine 2).
- `cachedResolution` (web_rescache.go:14-26) is a versioned, typed record.

## Cross-area edges
- Decision vs encoding, checked:
  - Decision-8 rules (dominance, repo gone counts 0, unread blocks, failed-source pick, `--resolve` incomplete note) are encoded (web_resolve.go:3139-3169, 922-966, 995-1005). Decision-12 rules 1, 2, 4, 5 are encoded; rule 1 has the gap in machine 1, and rule 3 holds only in keyless (machine 11).
  - Decision-15 rules 1-4 are encoded (web.go, web_keyless.go:372-469 `rivalBlocks` weak branch, 728-752).
  - Decision-17 and 18 are not in this tree (see scope note).
- `Web.Search` / `GetDocs` -> `docsAt(root, query, version)` (web.go:164, 458; discovery and ranking area). Only `webRoot` and a version label cross; `m.evidence`, trust, completeness and version basis are dropped here. This is the single point where machines 4 and 5 lose their agent-visible form.
- `Web` -> `Auto` (docs/auto.go:100-125, 290-318): `Auto` routes on error type only: `AmbiguousLibraryError` restricts Context7 to web's candidates; every other web error, including a transient "could not verify past a failed source" upstream error, lets Context7 answer from `namedBy(query)`. The refusal reason (machine 3) is erased at this boundary, which is where the planned refusal floor and lifecycle will need a typed `Refusal`.
- `Web` -> `Fetcher`/`ResolutionCache` (web_rescache.go; cmd/docs.go:304-317; internal/docsfetch/docsfetch.go:42-60): persistent state shared across processes (machines 2 and 4).
- `pkgClient`/`popularityClient`/`ghClient` (web_pkg.go, web_popularity.go, web_github.go) -> external hosts: quota state is per-process (turns) or per-resolution (GitHub), and nothing is shared with `context7QuotaTracker` (context7_quota.go:104-175), which is the one place cross-process quota memory already exists (machine 7).
- `ResolveLibrary` (listing) bypasses the pick gates by design (web.go:409-429); the incomplete note is its only degrade signal.
