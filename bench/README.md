# Extraction benchmark

Measure extraction **content fidelity**: what intended text survives, what unwanted
content comes along, and whether important facts and structures remain intact.
The corpus contains **500 distinct pages on 70 sites, 28 content types and
3,573 source-backed assertions**, including 1,831 critical checks. Timing is a
secondary regression signal.

This follows Cymbal's pinned-corpus, explicit-expectation and baseline workflow.
It measures extraction, not an agent's prior knowledge or ability to guess a
correct answer. See [RESULTS.md](RESULTS.md) for the accepted measurement and
[REVIEW.md](REVIEW.md) for the comparison and remaining failures.

## Run from the repository root

```sh
go run ./bench setup
go run ./bench run
go run ./bench check

# Concurrent consistency: 1,500 measured invocations across 500 distinct pages.
go run ./bench run -workers 4 -iterations 3 -warmup 0 -out bench/.runs/stress500

# Diagnose recoverable selection losses separately.
go run ./bench run -mode selector -out bench/.runs/selector

# Measure the clean extraction mode (config extract_mode = clean) against its own baseline.
go run ./bench run -extract-mode clean
go run ./bench check -extract-mode clean

# Network availability, HTTP latency and candidate source capture.
go run ./bench live
```

`make bench`, `make bench-check` and `make bench-live` are shortcuts. Pass extra
flags with `BENCH_ARGS='-iterations 25'`. `go run ./bench run -h` lists all flags.
Go's flag parser accepts both `-flag` and `--flag`; flags follow the subcommand.

The runner builds the worktree binary with `CGO_ENABLED=0` once outside the measurement window.
`-binary /path/to/ketch` benchmarks another build. A custom `-dir` outside this
repository also needs `-binary`. Building the runner/binary may download missing
Go modules; **the accuracy workload itself uses no network**.

`-extract-mode complete|clean` selects the extraction mode under test by setting
`KETCH_EXTRACT_MODE` for every invocation. Reports record it, and the clean mode's
default output and baseline paths carry a `-clean` suffix
(`bench/.runs/check-default-clean/`, `bench/baseline-clean.json`), so the two
modes never share a baseline.

## Corpus and independent expectations

| Cohort | Pages | Sites | Purpose |
| --- | ---: | ---: | --- |
| Original regression | 20 | 20 | Original source, reference and assertions preserved |
| Original expansion | 60 | 30 | Historical 100-page cohort, preserved unchanged |
| Original held-out evaluation | 20 | 20 | Historical evaluation cases, now regression evidence |
| Scale, existing sites | 320 | 40 | Eight additional pages from each of 40 existing sites |
| Scale, new sites | 80 | 20 new sites | New hosts; annotation errata recorded below |

See [CORPUS.md](CORPUS.md) for all 500 URLs, scope decisions and capture
observations. The 80 new-site pages have no host overlap with the original 100.
Historical `heldout` labels describe the earlier evaluation; those sites may now
also appear in `scale`. Most new sites contribute four pages; NHS has six, while
Britannica and FDA have three each.

The [500-page freeze](reports/corpus500-freeze.json) records the manifest and
binary hashes before the accepted evaluation. Eight source-scope errors were
corrected after a provisional run, with [full errata](reports/annotation-errata500.json)
and both binaries rerun against the corrected corpus. No page was dropped and
no extractor behavior changed during expansion;
the candidate binary is identical to the one measured on 100 pages. After results
are inspected, these become regression evidence; future blind evaluation needs
fresh sites. Historical [100-page](reports/initial100-REVIEW.md) and
[20-page](reports/initial20-review.md) reports remain available.

`corpus.json` records URLs, capture times, SHA256 hashes, source content
selectors, exclusions, annotation notes and assertions. `testdata/*.html.gz`
contains unchanged HTTP HTML; `*.txt` contains frozen reference text from the
annotated source region with navigation/controls excluded. The annotations
were derived from source HTML with explicit scopes. A separate DOM inspection
script proposed prose anchors and structural checks; an agent reviewed their
source scope and provenance before the provisional evaluation; the eight
post-evaluation scope corrections are documented separately. This is
**mechanically assisted, agent-reviewed annotation**, without an independent
human gold audit. Assertions sample content, not every factual claim.

`setup` verifies hashes and source evidence. It rejects invented facts, stale
link targets, positive checks in excluded regions, duplicate final URLs or HTML,
overlapping or absent reference roots, and noise checks that also occur in the
reference. Code expectations were checked against source examples, including
line-wrapper boundaries; setup's code provenance check does not certify their
indentation. Source selectors and exclusions are annotation data only.
**Default extraction receives the HTML and source URL, with no site hints.**

Third-party source text and HTML retain their original rights and notices;
they are not relicensed under ketch's MIT license. Attribution is recorded above
and in the manifest. Keep notices intact when reviewing or replacing captures.

## What the numbers mean

- **Token recall:** retained reference words, with repeated words counted only
  as often as they occur in the reference. Missing sections lower recall.
- **Token precision:** matching words divided by all emitted words. Navigation,
  duplicated text and unrelated widgets lower precision. F1 combines the two.
- **Assertions:** factual phrases, headings, exact code fragments, absolute link
  destinations, table headers with their associated row, and unwanted phrases.
  Text assertions allow case/whitespace changes but preserve punctuation and
  operators. Code checks require a preformatted block, matching case, indentation
  and newlines. CRLF and outer newlines are normalized. Heading checks normalize
  punctuation and case; tables preserve punctuation and operators. A table row cannot borrow another table's
  headers. Critical checks encode selected task-relevant facts and examples.
- **Latency:** median and nearest-rank p95 of seven CLI invocations after one
  unmeasured warmup, per page. Samples include process startup, stdin transfer
  and extraction; build, fixture loading, networking and scoring are excluded.
  With seven samples, p95 is the slowest sample; use more iterations for a useful
  tail estimate. Report wall time includes on-demand fixture reads, warmups, scoring and artifact writes.
  Whole-harness elapsed time additionally includes initial corpus validation.

Macro averages give every page equal weight, so the full book cannot drown out
a short API page. Reports group by content type and corpus cohort. Sites contribute between one and eleven pages; these are page averages,
not site-balanced population estimates. Failed, empty, timed-out
or nondeterministic extractions remain in the denominator with failed checks;
an empty page cannot earn noise-removal credit. All measured samples and output
hashes are retained in JSON, alongside per-page Markdown for inspection.

Scores apply to the emitted Markdown body; page metadata is not a separate
accuracy track here. Word overlap is **not semantic accuracy**: reordered or contradictory prose can
retain its words. Assertions catch selected errors, not every possible fact.
The corpus is a regression sample, not a statistically representative estimate
of all websites. It currently measures English HTTP HTML, not PDFs, visual
diagrams, browser rendering, JavaScript hydration, CSS-generated text, freshness,
search quality, tagging durability or an agent's final answer. Images and
embedded video are outside its text reference. These require separate tracks.

## Regression gates and reports

Each run writes `results.json`, `RESULTS.md` and `markdown/<id>.md` under
`bench/.runs/<command>-<mode>/` unless `-out` is supplied. That directory is
ignored by Git. A command reuses its output path; give comparisons distinct
`-out` directories. JSON is the machine interface; stdout prints a short result,
and stderr carries diagnostics. There are no prompts.

`run` exits zero when measurement completes, even if it records known extraction
misses. `check` fails if any previously passing assertion fails, recall or
precision drops by more than 0.005 per page, or a median exceeds twice its
baseline **and** increases by more than 2 ms. Threshold flags are experiment
controls, not an SLA. A new pass cannot compensate for a different failed check.
Execution failures and nondeterminism also fail the command. Exit 1 means
invalid input, a failed measurement or a failed gate; diagnostics identify which.

`-strict` additionally fails every missing critical assertion, including known
baseline failures. **The accepted baselines do not pass strict mode: 9 critical checks fail in complete mode and 12 in clean mode.**
An accepted regression baseline is not a release approval or a claim of complete
extraction. Run selector recovery separately; its manually supplied content root
makes it an assisted result, and it can include considerably more noise. Thirty
reference roots use valid CSS selector groups that the current CLI rejects;
selector mode currently exits 1 with those failed cases retained in its scores.

```sh
# Deliberate acceptance after reviewing every accuracy and timing change.
go run ./bench update-baseline
go run ./bench update-baseline -extract-mode clean

# Keep the checked-in readable snapshots in sync when accepting them.
cp bench/.runs/update-baseline-default/RESULTS.md bench/RESULTS.md
cp bench/.runs/update-baseline-default-clean/RESULTS.md bench/RESULTS-clean.md

# Calibrate elsewhere without replacing the shared baseline.
go run ./bench update-baseline -baseline bench/.runs/my-machine.json
go run ./bench check -baseline bench/.runs/my-machine.json
```

Comparisons require matching corpus/schema, mode, extraction mode, iterations, warmups, workers,
OS/architecture, logical CPU count, Go version and CGO setting. Different CPU models, power settings,
build flags and background load still affect timings; calibrate on the same
machine with other workloads quiet. Review the binary hash in each report.

Ctrl-C cancels child processes. Each invocation/fetch has a 20-second timeout.
HTML and reference text are each capped at 20 MiB per page, compressed snapshot
reads and JSON inputs at 32 MiB, and aggregate HTML/reference payload at 1 GiB.
The current payload is 134.32 MiB. These are input admission limits, **not a hard
RAM limit**. The corpus loader retains only metadata and word counts; source
validation reads one page at a time, and each worker loads and rechecks its page's
hashes when needed. DOM parsing, grading and subprocesses consume additional
memory, which grows with page size and worker count. See the measured observations
in [resources500.json](reports/resources500.json).
Child stdout/stderr is capped at
32 MiB/64 KiB. Worker count
is bounded to 1–32. Processes receive empty temporary config and a temporary
`KETCH_TAGS_PATH`; ambient `KETCH_*` overrides are removed and update checks are
disabled. `extract` uses no page cache or browser, so isolation does not rely
on XDG behavior on Windows or macOS. The installed binary/config are untouched.

`live` is an explicit, serial HTTP fetch check. It reports every status,
latency, error and byte-level source change in `live.json`, and saves candidate
snapshots under its output directory. It does not score changed source against
old gold text or replace the pinned corpus/reference/baseline. A changed hash
can reflect a nonce or widget rather than a changed article; review the source.

## Add pages without weakening the test

1. Select independent sites and underrepresented structures before inspecting
   extractor output. Include more languages, lists, callouts, nested tables,
   highlighter variants, and multiple articles. Keep browser/PDF work in a
   separately specified track instead of mixing incomparable timings.
2. Save the original public HTTP response as `testdata/<id>.html.gz`. Record
   source/final URLs and capture time; keep notices and provenance. Existing
   cases can use `live` candidate captures. Do not silently replace a page that
   is difficult to fetch or extract.
3. Read the source. Record the content root and exclusions; produce the reference
   text and annotate important facts plus code/link/table/noise checks. **Never
   use ketch output as the reference.** Have a second person review scope,
   ambiguity, code indentation and table relationships where possible. Expandable
   article panels, disclosure-button headings and substantive asides belong in
   the reference; hidden menus and controls do not. MDN availability notices and
   NPS `hidden="until-found"` panels are retained explicitly in this corpus.
4. Add the case and hashes to `corpus.json`. Hash the *uncompressed* HTML and
   exact reference bytes. Run `setup` and `go test ./bench`.
5. Run both the previous and candidate binaries against the same new corpus. Preserve a timestamped manifest hash before the first run.
   Review every failure and artifact, then explicitly update the baseline. A
   corpus change invalidates comparisons to the old baseline by design.

The harness imports no ketch extraction implementation for scoring. Goldmark
parses emitted Markdown independently of ketch's HTML-to-Markdown converter.
Tests damage negation, operators, indentation, links and table associations,
and exercise timeout, oversized output, corrupt fixtures, hidden regressions,
cancellation and nondeterminism. Run `go test ./bench ./extract` when changing it.

## Contract ledger

| Surface | Change | Risk | Compatibility | Verification |
| --- | --- | --- | --- | --- |
| Repository `bench/` commands and JSON reports | Add developer harness, schema 1 | R0 | No installed ketch command/MCP changes | Runner, scorer, source and gate tests; real corpus runs |
| Code normalization | Narrow inline-style matching; cross-language fixtures | R2 | Same extraction API/flags; no host rules | Extract tests; before/after 20-site replay |
| Corpus and baseline | Expand to 500 pages; archive the 100-page manifest/baseline/reports | R0 | Original 100 records and fixture hashes preserved exactly | Source validation, frozen hashes and preservation test |
| Manifest/report cohorts | Add `scale` and `scale-new-sites` values | R0 | Existing fields, labels and scoring unchanged; custom manifests may omit cohort | Group, coverage and host-separation tests |
| Source validation | Apply declared exclusions to positive assertion evidence; retain original DOM for noise | R0 | Scoring unchanged; invalid annotations fail setup | Excluded-content and noise-provenance tests |
| Fixture loading | Read per worker; 1 GiB aggregate input admission with per-file bounds | R0 | Same commands/report schema; fixtures rechecked on use | Changed-source rejection, byte-limit tests, resource and concurrency measurements |

Caller profiles: human-operator, script. Obligations: bounded and cancellable
execution, repeatable sources, independent expectations, honest denominators,
separate assisted/live results, and explicit baseline acceptance. Independent human gold
auditing, fresh blind evaluation sites, and browser/PDF tracks remain future work.
