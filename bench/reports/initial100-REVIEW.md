# Extraction review: 100 pages across 50 sites

Historical 100-page snapshot; the current corpus and commands are documented in [the benchmark guide](../README.md).

This expansion measures content preservation on 100 pinned HTML pages and 24 content types. It adds 80 source-reviewed cases and 577 assertions to the original corpus, for **692 assertions, 380 critical**. No production extractor changes were made. The candidate binary hash is identical to the original 20-page evaluation; all original HTML, references and assertions are preserved.

## Accuracy on identical input

| Run | Checks | Critical | Macro token recall | Macro token precision | Execution/nondeterminism failures |
| --- | ---: | ---: | ---: | ---: | ---: |
| Before code-preservation fix, default | 610/692 | 346/380 | 93.98% | 98.17% | 0 |
| Current, default | 616/692 | 352/380 | 94.38% | 98.17% | 0 |
| Current, assisted selector | 651/692 | 361/380 | 94.97% | 91.19% | 5 |
| Current, four-worker consistency | 616/692 | 352/380 | 94.38% | 98.17% | 0 |

**These percentages describe token coverage, not semantic accuracy.** The explicit checks separately test selected phrases, code, headings, links, table associations and unwanted text. Macro averages weight every page equally; they are not a population estimate of the web.

The code-preservation change gains six checks without losing a previously passing check: the original npm YAML and two Go checks, plus JSON examples on two further npm pages and a SQL join example on PostgreSQL. No per-page recall or precision drop exceeds the 0.005 regression tolerance. The npm package.json page has a small precision reduction despite recovering more content; do not describe this as zero metric movement.

## Cohorts

| Cohort | Pages | Checks | Critical | Recall | Precision |
| --- | ---: | ---: | ---: | ---: | ---: |
| regression | 20 | 101/115 | 72/79 | 92.98% | 99.71% |
| expansion | 60 | 391/429 | 214/227 | 96.09% | 97.94% |
| heldout | 20 | 124/148 | 66/74 | 90.66% | 97.33% |

The 20 evaluation pages come from 20 sites absent from the other cohorts. Their reference scope and assertions were frozen before evaluating either binary, and this task made no extractor changes. Their lower recall is a useful warning against judging extraction only on familiar documentation templates. After this review they are regression evidence, not an untouched future test set.

## Findings worth fixing next

- **Git rebase:** only 29.5% of reference tokens survive default extraction. The opening, a mid-document caveat, closing text and command synopsis all fail their checks. Assisted selection recovers them.
- **React state tutorial:** only 37.0% survives, with opening/middle/closing anchors and the first code example missing. This is already server-rendered HTML, so it is not explained by the absence of browser rendering.
- **Recipes and repair guides:** the King Arthur recipe loses an ingredient quantity inside its substantive ingredient aside; the iFixit guide loses introductory content. These broaden the content-selection problem beyond documentation sites.
- **Footnotes and introductory facts:** Python footnotes, MDN definition/availability text, Angular introduction, and some science-page introductions are lost. Existing npm prerequisite and NPS accordion omissions remain.
- **Grouped CSS selectors:** the CLI validates `--select` with `cascadia.Parse`, while the extraction library/source validator accepts selector groups. Five valid grouped roots fail with exit 2: Yellow Wallpaper, BGS earthquakes, King Arthur bread, iFixit moped and Hacker News. See [cmd/extract.go](../../cmd/extract.go). This is a generic validation mismatch, not five site-specific extraction problems.

Default extraction has **zero process failures**. Selector mode has **five validation failures**, retained in the denominator with all their checks failed. On the remaining 95 pages, its 361 critical checks pass. The all-100 assisted score remains **361/380**; it must not be presented as a perfect result. Selector mode also admits more navigation/control text. No source expectation or failing case was removed after evaluating the output.

Content selection and selector-group validation are follow-up implementation work. This task expands and validates the measurement harness; it does not introduce site exceptions or repair extraction to fit the new samples.

## Safety and consistency

The pinned HTML plus reference payload is 30,151,913 bytes (28.76 MiB), below the new 128 MiB admission limit. The runner retains the corpus in memory; DOM parsing, grading and child processes use additional memory. GNU time reported maximum RSS of 143.1 MiB for the serial default run and 136.7 MiB for the four-worker run. These are OS process-usage observations, **not a bound on aggregate simultaneous worker memory**. See [resources100.json](resources100.json).

The default workload is 700 measured invocations plus 100 warmups. Its report wall time is 33.90s. The consistency workload is 500 measured invocations plus 100 warmups across four workers; its report wall time is 8.91s. All 100 output hashes and all accuracy results match the serial run, with no execution failures or nondeterminism. Timing is secondary to content fidelity and is not an extraction-accuracy score.

The runner now rejects duplicate final URLs, duplicate HTML payloads, overlapping reference roots and oversized aggregate payloads. Reports include an optional page cohort and `by_cohort` summaries. Installed ketch commands/MCP contracts, real cache/config and the installed binary remain unchanged.

## Review limits

References and checks were curated and reviewed by an agent directly from the captured source. Independent human gold review remains outstanding. Source validation catches unsupported assertions and contradictory noise checks, but cannot prove that a reference scope is complete or that a numerical statement is true. Asides, code wrappers, disclosure headings and footnotes were reviewed explicitly before the first extraction run.

The HTTP corpus excludes blocked/unavailable captures and two JavaScript article shells, with those attempts retained in [capture100.json](capture100.json). It covers English HTML text and selected structures, not browser execution, PDFs, image/chart pixels, full mathematical equivalence, all spanning-table relationships, freshness or an agent’s final answer. [CORPUS.md](initial100-CORPUS.md) records every source and the scope decisions.

## Reproduce

Verification passed for the harness and branch. Strict/selector failures below
are measured extractor limitations, retained deliberately in the results.

| Check | Command | Result |
| --- | --- | --- |
| Corpus and source evidence | `go run ./bench setup` | Pass, 100 pages / 692 assertions |
| Full tests | `CGO_ENABLED=0 go test ./...` | Pass |
| Harness race checks | `go test -race ./bench` | Pass |
| Static checks | `go vet ./...`; `golangci-lint run` | Pass; zero lint issues |
| Regression gate | `bench check` against the new baseline | Pass |
| Strict content gate | `bench run -strict -iterations 1 -warmup 0 -workers 4` | Expected exit 1, 28 critical omissions |
| Assisted selector run | `bench run -mode selector` | Exit 1, five selector-group validation failures |
| Portability | Pure-Go bench builds for Windows/amd64 and macOS/arm64 | Pass; native execution not tested |

Exact observations are recorded in [verification100.json](verification100.json).

```sh
go run ./bench setup
go run ./bench run
go run ./bench check
go run ./bench run -workers 4 -iterations 5 -warmup 1 -out bench/.runs/stress100

# Expected to fail on the 28 recorded critical content omissions.
go run ./bench run -strict -iterations 1 -warmup 0 -workers 4

# Expected to fail on five currently unsupported selector groups.
go run ./bench run -mode selector
```

The new baseline deliberately records 76 known assertion failures, including 28 critical checks. A passing regression check means no new loss against that baseline; it is not release approval. [RESULTS.md](initial100-RESULTS.md) lists every per-page omission.

- [Default measurement](default100.json), [previous binary](before100.json), [assisted selector](selector100.json), [consistency run](stress100.json).
- [Frozen corpus record](corpus100-freeze.json), [current baseline](initial100-baseline.json), [source inventory](initial100-CORPUS.md).
- [Original 20-page review](initial20-review.md), [original baseline](initial20-baseline.json), [original manifest](initial20-corpus.json).

Corpus SHA256: `501016bcba5a3675c5e2107dc3462be0df725bc1be1e072cd9a4509b37ef80f0`.  
Candidate SHA256: `53432e7ddd71749efa89f0b798a8709744c19f2ad0feaa0e2539eb4bccd6ba75`.  
Previous SHA256: `4b1ec2e0925b22b0cc68002dd843ecb54930ebc85238d83e4f9a9a9b9905ccd6`.
