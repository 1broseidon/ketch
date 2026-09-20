# Extraction review: initial 20-site corpus

Captured 2026-09-19 UTC. The review covers the extraction adapter and benchmark;
it does not certify every source fact or approve a release.

## Generic implementation

No hostname rules, Go grammar checks, known URL lists or per-site selectors were
added to production extraction. The adapter operates on `pre`/`code`, inline
syntax-token wrappers, block/BR line boundaries and language-class prefixes.
The same changes run through default, raw and selector conversion. The only
site-specific selectors are independently annotated benchmark content roots;
default runs never pass them to ketch.

The review narrowed one overly broad rule: an inline CSS custom property
containing `display:none` could incorrectly hide actual code. Visibility now
matches properties, follows later simple declarations and `!important`, and
retains complex CSS rather than guessing. Inert template contents are excluded.
Regression fixtures cover Python, JavaScript, JSON, shell, YAML and Go across all
three conversion paths, plus hidden controls, indentation, literal HTML and
idempotence. Ordinary reader-comment filtering remains active outside code.

## Results on identical saved HTML

| Run | Checks | Critical | Macro token recall | Macro token precision | Failed/nondeterministic pages |
| --- | ---: | ---: | ---: | ---: | ---: |
| Previous binary, default | 98/115 | 69/79 | 92.0% | 99.7% | 0 |
| Candidate, default | 101/115 | 72/79 | 93.0% | 99.7% | 0 |
| Candidate, assisted selector | 114/115 | 79/79 | 99.9% | 95.5% | 0 |
| Candidate, four-worker stress | 101/115 | 72/79 | 93.0% | 99.7% | 0 |

The candidate recovers three failed checks: npm YAML indentation, Go code
comments and a linked API signature. **No previously passing assertion, token
recall or token precision regressed on any of the 20 pages.** This supports the
HTML-level fix across this sample; it is not proof of universal extraction.

Both compared binaries use go1.27.1, `CGO_ENABLED=0`,
linux/amd64, on the same 24-logical-CPU machine. Default timings
use seven measured invocations after one warmup per page. Candidate per-page
medians span 5.7–96.7 ms;
the largest per-page p95 is 99.0 ms.
These are CLI invocation timings, not pure parser microbenchmarks or a service SLA.

| Page | Checks before → after | Token recall before → after | Median ms before → after |
| --- | ---: | ---: | ---: |
| npm | 2 → 3 / 6 | 80.1% → 82.3% | 33.65 → 39.35 |
| go | 4 → 6 / 7 | 79.2% → 97.4% | 18.81 → 21.84 |
| python | 6 → 6 / 6 | 99.9% → 99.9% | 22.29 → 20.53 |
| kubernetes | 6 → 6 / 6 | 94.4% → 94.4% | 38.20 → 47.04 |
| postgres | 6 → 6 / 6 | 99.9% → 99.9% | 15.68 → 17.11 |
| mdn | 7 → 7 / 7 | 95.2% → 95.2% | 15.94 → 17.02 |
| rust | 5 → 5 / 6 | 100.0% → 100.0% | 14.13 → 15.63 |
| sqlite | 6 → 6 / 6 | 100.0% → 100.0% | 9.72 → 10.08 |
| git | 6 → 6 / 6 | 99.3% → 99.3% | 20.16 → 23.11 |
| gnu | 5 → 5 / 5 | 100.0% → 100.0% | 5.31 → 5.69 |
| terraform | 5 → 5 / 5 | 99.0% → 99.0% | 30.45 → 33.21 |
| docker | 5 → 5 / 5 | 100.0% → 100.0% | 34.91 → 47.36 |
| cloudflare | 7 → 7 / 7 | 99.0% → 99.0% | 36.38 → 41.99 |
| danluu | 5 → 5 / 5 | 100.0% → 100.0% | 26.67 → 26.62 |
| joel | 4 → 4 / 4 | 100.0% → 100.0% | 9.79 → 9.80 |
| rfc | 4 → 4 / 4 | 100.0% → 100.0% | 11.25 → 10.68 |
| wikipedia | 5 → 5 / 6 | 98.5% → 98.5% | 79.17 → 96.69 |
| gutenberg | 4 → 4 / 4 | 100.0% → 100.0% | 40.44 → 42.18 |
| nasa | 5 → 5 / 5 | 92.2% → 92.2% | 22.13 → 23.61 |
| nps | 1 → 1 / 9 | 2.4% → 2.4% | 14.67 → 15.89 |

Code preservation adds work: several code-heavy pages are slower, although no
page exceeds the benchmark's 2× median regression threshold. The extra
normalization parses and walks HTML before Readability; recovering previously
removed code also increases conversion work. The table records this tradeoff
instead of treating a speed gate as proof that extraction became faster.

The stress run used **four workers, 25 measured runs and two warmups per page**:
500 measured invocations, 40 warmups, 5.53 seconds of wall time,
no process errors, no output variation, and identical accuracy to the serial
run. Its maximum per-page p95 was 105.2 ms.
Wall time includes warmups, scoring and report artifacts.

## Remaining failures

- **npm:** default selection omits two prerequisite version requirements and
  their introductory heading. This was already missing in the previous binary.
- **NPS:** only **2.4%** of the annotated article text survives by default. The
  source has meaningful content under `accordion-list-header`, an emergency
  aside, and expandable `hidden="until-found"` panels. Readability's debug log
  explicitly removes the headers as unlikely candidates and the panels as
  hidden elements. This loses five critical checks plus headings. Selector
  extraction recovers the content. See [the diagnostic log](nps-readability.log).
- **Go/Wikipedia:** selected headings disappear while surrounding content is
  retained. **Rust:** keyboard-help text leaks into the article.
- **Selector mode:** all 79 critical checks pass, but GNU navigation remains,
  and some pages include other extra content. Its precision is 95.5% versus
  99.7% in default mode. Do not mix assisted recovery into default scores.

The next extraction task is generic content selection: distinguish authored
sections and disclosures from controls, and avoid deleting substantive content
because a class name contains `header` or a disclosure is initially collapsed.
Evaluate that work against this corpus and new held-out sites. Do not add an NPS,
Go or npm hostname exception to make these checks pass.

## Benchmark review findings

Ground truth needs review too. Before the final measurement, source auditing
corrected an outdated link, a wrong JSON status value, a noise word that also
appears in article prose, and an HCL example's exact whitespace. A subsequent
source-scope audit caught two larger issues: the NPS reference must retain its
expandable panels, heading buttons and emergency aside; MDN's browser
availability notice is substantive content. Those references were expanded and
four assertions added, producing this final **115-check, 79-critical** corpus.
The source audit examined hidden controls/asides across all 20 pages. Earlier
provisional scores used a different corpus and must not be compared with this
baseline. No expectation was removed because the extractor failed it.

The scorer parses Markdown with Goldmark, independently of ketch's converter.
Tests demonstrate failure on lost negation, reversed comparison operators,
flattened code, damaged indentation, relative links and swapped table columns.
Hash/provenance checks reject changed snapshots or unsupported annotations.
Per-check gates prevent a new pass from masking a lost pass. Failed or
nondeterministic invocations remain failed, including noise-exclusion checks.
Timeouts, output caps, cancellation, corrupt fixtures and live HTTP failures are
tested. Build metadata prevents comparing different Go/CGO settings silently.

The 20-site live check returned HTTP 200 for every site. Four byte hashes
changed (Cloudflare, Joel, NASA, NPS); those are candidate captures for review,
not automatic ground-truth updates. This is an HTTP availability/latency check,
not browser-rendering or end-to-end scrape accuracy.

Annotations are agent-curated from source and have not had independent human
review. Token overlap ignores word order and semantic relationships; the
explicit checks cover selected facts and structures. The corpus does not yet
cover rendered SPAs, PDFs, multilingual pages or visual content. Accuracy claims
must retain those limits when expanding toward 50 sites.

## Reproduce and inspect

- [Accepted default report](initial20-RESULTS.md), [baseline JSON](initial20-baseline.json).
- [Previous binary measurements](before.json),
  [assisted selector measurements](selector.json),
  [stress measurements](stress.json), [live fetch results](live.json).
- [Commands, scoring, annotation workflow and regression policy](../README.md).
- Previous binary SHA256: `4b1ec2e0925b22b0cc68002dd843ecb54930ebc85238d83e4f9a9a9b9905ccd6`.
- Candidate binary SHA256: `53432e7ddd71749efa89f0b798a8709744c19f2ad0feaa0e2539eb4bccd6ba75`.
- Corpus SHA256: `aba86c1ff23774dfc74ab5914955c80fc4fb816b9740f95064f202f153b1ac99`.

The previous binary used in this review is locally preserved at
`/tmp/ketch-extraction-audit/ketch-before`. Current runs build the worktree;
`-binary` allows an explicit comparison build. Initial acceptance records 14
known failed checks. `check` tests regressions; `check -strict` also rejects all
seven missing critical checks. Neither baseline acceptance nor a passing
regression check is a release approval.
