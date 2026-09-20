# Extraction review: 500 pages across 70 sites

The corpus now contains **500 distinct HTML pages, 28 content types and 3,573 source-backed assertions**, including 1,831 critical checks. The original 100 records and fixtures are unchanged. The expansion adds 320 pages from existing sites and 80 pages from 20 new sites. No production extractor changes were made; the candidate binary is identical to the earlier 20- and 100-page evaluations.

## Accuracy on identical input

| Run | Checks | Critical | Macro word recall | Macro word precision | Execution/nondeterminism failures |
| --- | ---: | ---: | ---: | ---: | ---: |
| Before extraction fixes, default | 3,148/3,573 | 1,671/1,831 | 93.95% | 98.07% | 0 |
| Current, default | 3,176/3,573 | 1,692/1,831 | 94.13% | 98.07% | 0 |
| Current, assisted selector | 3,320/3,573 | 1,735/1,831 | 93.95% | 90.28% | 30 |
| Current, four-worker consistency | 3,176/3,573 | 1,692/1,831 | 94.13% | 98.07% | 0 |
| Structural extraction (0.18), `extract_mode` complete | 3,472/3,573 | 1,822/1,831 | 99.33% | 98.11% | 0 |
| Structural extraction (0.18), `extract_mode` clean | 3,465/3,573 | 1,819/1,831 | 98.96% | 99.45% | 0 |

The two structural rows were added on 2026-09-20 after the readability-based selection was replaced by structural content selection (see [RESULTS.md](RESULTS.md) and [RESULTS-clean.md](RESULTS-clean.md)); the rows above them describe the readability extractor the rest of this review was written against. These are content-fidelity measurements against annotated source text, **not semantic accuracy**. Word overlap cannot detect every contradiction or ordering error. Assertions independently test selected phrases, headings, code, links, table associations and unwanted text. Pages receive equal weight; sites and content types are not equally represented.

The earlier generic extraction fixes gain **28 passing checks, including 21 critical checks**, without losing any previously passing check. Gains include npm YAML/JSON/shell, Go code, PostgreSQL SQL, Angular/React examples, and links on Pandas, SciPy and Stanford Philosophy pages. No per-page recall or precision drop exceeds the 0.005 regression tolerance; small metric changes still exist. See [comparison500.json](reports/comparison500.json). These gains come from the previous code and URL fixes, not changes made to fit this expansion.

## Cohorts

| Cohort | Pages | Checks | Critical | Recall | Precision |
| --- | ---: | ---: | ---: | ---: | ---: |
| regression | 20 | 101/115 | 72/79 | 92.98% | 99.71% |
| expansion | 60 | 391/429 | 214/227 | 96.09% | 97.94% |
| heldout | 20 | 124/148 | 66/74 | 90.66% | 97.33% |
| scale | 320 | 2045/2309 | 1081/1175 | 94.86% | 98.71% |
| scale-new-sites | 80 | 515/572 | 259/276 | 90.87% | 95.40% |

The 80 new-site pages have no host overlap with the original 100. Their lower coverage reinforces the need to evaluate beyond familiar documentation templates. Historical `heldout` labels describe the earlier 100-page evaluation; those hosts may now also occur in `scale`. All original 100 output hashes and accuracy results remain identical. After review, every case is regression evidence, not a fresh blind test set.

## Source review and annotation errata

References come from original source HTML with explicit content roots and exclusions. A separate inspection script proposed prose anchors and structural checks; an agent reviewed scope and provenance. This is **mechanically assisted, agent-reviewed annotation**, with independent human gold review still outstanding. Three prose anchors cannot certify every factual statement on a long page. No candidate extraction output was used as the reference.

After the provisional runs, eight references were found to contradict their intended scope: four AllRecipes pages retained photo-gallery reader comments and controls, and four Red Cross pages retained newsletter copy. The exclusions, affected reference text and sampled checks were corrected against unchanged HTML. Both binaries were rerun against the same corrected corpus. No page was removed and the other 492 records were unchanged. This is a post-evaluation annotation repair, so the result must not be described as an untouched blind experiment.

The [erratum](reports/annotation-errata500.json) retains the old/new hashes, checks, exclusions and chronology, alongside provisional manifests, results and the eight old reference files. Source validation now applies declared exclusions before checking positive assertion evidence; it retains the original DOM for noise evidence. All 3,573 assertions pass source validation.

[Capture accounting](reports/capture500.json) records 476 distinct observations: 400 selected sources, 61 HTTP failures and 15 successful responses replaced before scoring. Identical repeated observations are collapsed, so these are not request totals or an availability rate. Replacements addressed unavailable URLs, redirects, duplicates and non-article shells; no page was dropped for poor extraction. See [CORPUS.md](CORPUS.md) for scopes and every source URL.

## Findings worth fixing next

- **Content selection remains the main weakness.** Default extraction retains only 2.38% of the original Yellowstone safety reference, 3.32% of another NPS hiking page, 10.68% of Red Cross flood guidance, about 29% of several Git manuals, and 31.83–47.26% of three W3C image tutorials. These are separate templates and content types, not a Go documentation issue.
- **Redis coverage requires reference-scope review.** These pages pass their sampled critical checks while retaining only 34.43–45.39% of reference words. A subsequent [binary-versus-curl audit](AUDIT.md) found all 191 code-block texts present on Redis lists, alongside a reference containing 497 hidden API-signature panels and excluding full-source templates that appear in output. The lists score does not demonstrate missing language examples. Other Redis pages still need manual review; combine word coverage with source inspection and structural checks.
- **Noise remains measurable.** Recipe and discussion pages can retain substantial unwanted content. The recipe category has 94.59% recall but only 72.42% precision. Enlarging a selected root can recover content while admitting widgets and unrelated text.
- **Grouped selectors have a generic CLI validation defect.** `cmd/extract.go` uses a single-selector parser while the library and source validator accept groups. Thirty valid grouped roots fail with exit 2; they stay in the assisted score with all checks failed. These include WHO, UN, World History and Natural History Museum pages as well as five earlier cases.
- **Assisted selection is not complete either.** Of its 470 successfully executed pages, two SQLite code assertions still fail. The all-500 assisted score is 1,735/1,831 critical checks, with 30 execution failures and 90.28% word precision. It receives the annotated root but not annotation exclusions; its scores cannot stand in for default extraction.

Default extraction has **397 assertion misses, including 139 critical misses on 88 pages**, despite zero process failures. Missing checks comprise 140 headings, 124 text phrases, 93 links, 25 noise-removal checks and 15 code checks. [RESULTS.md](RESULTS.md) lists per-page failures. No hostname, site selector or Go-specific production rule was added for this expansion.

## Runtime and resources

The default run measured 3,500 invocations after 500 warmups, with one worker. The median of per-page median CLI latencies was **19.86 ms**. Whole-harness elapsed time was **148.59 seconds**; reported workload wall time was 137.32 seconds. This is a local Linux/amd64 observation, not a performance guarantee. Seven samples per page make p95 the slowest observation; more samples are needed for a stable tail estimate.

The four-worker run performed 1,500 measured invocations in **26.11 seconds** including source validation. Every output hash and accuracy result matched the serial run. Strict mode returned exit 1 for the same 139 critical misses, with no execution failures or nondeterminism. The subsequent serial regression check also matched all 500 outputs and passed its accuracy and latency gates.

| Measurement | Workers | Samples/page + warmups/page | Whole elapsed | Max individual RSS | Sampled tree peak RSS |
| --- | ---: | ---: | ---: | ---: | ---: |
| default | 1 | 7 + 1 | 148.59 s | 143.8 MiB | 166.4 MiB |
| before | 4 | 1 + 0 | 16.86 s | 143.4 MiB | 310.8 MiB |
| selector | 4 | 1 + 0 | 16.06 s | 140.1 MiB | 313.6 MiB |
| stress | 4 | 3 + 0 | 26.11 s | 139.9 MiB | 320.6 MiB |
| check | 1 | 7 + 1 | 148.56 s | 141.5 MiB | 170.3 MiB |

The earlier binary and selector runs used one sample and four workers for accuracy comparison. Their timings are not comparable to the seven-sample serial baseline. Runs were sequential. CLI latency includes process startup, stdin and extraction; fixture loading, networking and scoring are excluded. Workload wall time includes on-demand reads, warmups, scoring and writes; whole elapsed additionally includes initial validation.

The 134.32 MiB source/reference payload is loaded per worker rather than retained for the whole corpus. Input admission is bounded to 1 GiB aggregate, with separate per-file limits and hashes rechecked at use. This is not a hard RAM limit: DOMs, scoring and subprocesses need additional memory. Linux tree RSS was sampled every 20 ms, counts shared pages more than once, and may miss brief peaks. OS maximum individual RSS is a different measure. Exact commands and observations are in [resources500.json](reports/resources500.json).

## Verification and use

| Check | Result |
| --- | --- |
| Corpus hashes and source evidence | Pass, 500 pages / 3,573 assertions |
| Original 100 preservation | Exact manifest records, fixture hashes, output hashes and accuracy preserved |
| `CGO_ENABLED=0 go test ./...` | Pass |
| `go test -race ./bench` | Pass |
| `go vet ./...`; `golangci-lint run` | Pass; zero lint issues |
| Regression check against accepted baseline | Pass, all 500 outputs/accuracy match |
| Strict content gate, four workers | Expected exit 1, 139 critical misses; no execution failures |
| Assisted selector run | Exit 1, 30 grouped-selector validation failures |
| Pure-Go bench builds for Windows/amd64 and macOS/arm64 | Pass; native execution not tested |
| Formatting and whitespace | Pass |

Exact validation is recorded in [verification500.json](reports/verification500.json). The benchmark builds temporary binaries and uses isolated config/tag paths. This task did not replace the installed binary, edit operator config/cache, push, commit or cut a release. Work remains on `feat/tag-cache-index`.

```sh
go run ./bench setup
go run ./bench run
go run ./bench check

# Same content results under concurrency; exit 1 for known critical misses.
go run ./bench run -workers 4 -iterations 3 -warmup 0 -strict -out bench/.runs/stress500

# Assisted diagnosis; exit 1 for the 30 grouped-selector cases.
go run ./bench run -mode selector -iterations 1 -warmup 0 -workers 4 -out bench/.runs/selector500
```

The accepted baseline records the 397 known misses; a passing regression check means no new loss against that snapshot, **not release approval or complete extraction**. Independent human annotation review, fresh evaluation sites, multilingual/encoding coverage and separate browser/PDF tracks remain future work.

- [Default measurement](reports/default500.json), [previous binary](reports/before500.json), [assisted selector](reports/selector500.json), [concurrency/strict](reports/stress500.json), [regression check](reports/check500.json).
- [Freeze record](reports/corpus500-freeze.json), [baseline acceptance](reports/acceptance500.json), [source inventory](CORPUS.md), [annotation errata](reports/annotation-errata500.json).
- Historical [100-page review](reports/initial100-REVIEW.md), [100-page baseline](reports/initial100-baseline.json), [100-page manifest](reports/initial100-corpus.json), and [20-page review](reports/initial20-review.md).

Corpus SHA256: `14c3ccc4f5eb5b6ef0bec2ca0727871d682d5845560ff7bce8bc70f8b719f441`.  
Candidate SHA256: `53432e7ddd71749efa89f0b798a8709744c19f2ad0feaa0e2539eb4bccd6ba75`.  
Previous SHA256: `4b1ec2e0925b22b0cc68002dd843ecb54930ebc85238d83e4f9a9a9b9905ccd6`.
