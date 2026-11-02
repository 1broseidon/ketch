# Keyless docs: design to close epic-2

**Status:** Proposed · **Date:** 2026-11-02 · Supersedes the default-backend
question left open in task-15

## Outcome

> `ketch docs` keyless via web searches web sources and identifies docs pages
> through llms.txt manifests and framework signals, and keeps accuracy in the
> high 90s, as search does. The difference from web search: docs returns
> targeted documentation so coding assistants use correct information.

"Keyless" means the measured configuration has no `GITHUB_TOKEN`, no Context7
key and no search-provider key. A token may improve results. It may never be
required for the gates to pass.

## Where we are

Phases 1–2 work but are uncommitted on `feat/web-docs`. Round 1 passed the old
retrieval gates (decision-6) *with* a GitHub token. Without one, it answered 27.5%:
resolution verifies sites and breaks same-name ties through the GitHub API,
whose unauthenticated quota is 60 requests an hour. Under grading v2
(decision-9) the known-case calibration is 0/8 strict passes, though 4/8 contain the full
answer in the top three. The failures are whole-response precision, buried
answers and citations, not wrong projects.

So two things block the outcome:

1. **Resolution needs GitHub.** That's the keyless problem.
2. **Responses are not yet targeted.** That's the coding-assistant problem.

## Accuracy, defined before any run

"High 90s" needs one headline number with a fixed meaning.

**Answer accuracy:** the share of answerable cases where the default keyless
response (a) comes entirely from the right project and version, (b) contains
every predeclared required fact, including code where the case declares it, in
results 1–3, and (c) cites each supporting result with a page URL and anchor that
exist. Refusing an answerable case counts as a miss.

| Gate | Threshold | Why |
|---|---|---|
| Answer accuracy, keyless | **≥ 97%** on the replay suite and on the sealed holdout | the outcome |
| Wrong project / wrong version / misleading | **0 observed**, over ≥ 300 answered cases | rule of three: 0/300 bounds the true rate below 1% at 95% |
| Negative controls (nonexistent, unofficial-only) | 100% refused | a wrong answer is worse than none |
| Strict whole-response quality (decision-9 rubric) | ≥ 90% overall and per stratum | targeted, not just correct |
| Latency, cold cache | p50 ≤ 4 s, p95 ≤ 12 s | unchanged |
| Repeatability | 2 fresh-cache + 1 warm live runs, no best-of | unchanged |

For comparison: ketch-bench measures extraction at 99.0% recall and 99.4%
precision in clean mode. Search itself has no accuracy benchmark. Docs will be
the first surface gated on end-to-end answer correctness.

A token run and, when a key exists, a Context7 run on the same cases are
reported beside the keyless run. They are not gates.

## Pipeline

```
spec ─▶ candidates ─▶ identify docs site ─▶ verify ─▶ pick or refuse ─▶ retrieve ─▶ shape response
        (keyless)     (llms.txt, framework)   (≥2 edges)  (dominance)       (existing)   (for agents)
```

### 1. Candidates, all keyless and in parallel

- **Package registries** (existing): npm, PyPI, crates.io, Go proxy and
  pkg.go.dev. Keyless and generous. Package metadata is set by the owner, so it
  is the strongest single source.
- **Web search, promoted from last resort to a first-class source.**
  `search.Auto` with `"<name> documentation"`, plus the ecosystem hint when the
  spec or query has one (`npm`, `python`, `rust`, `go`). It finds docs sites that
  registries don't name, such as custom domains and monorepo docs. A search hit
  is a *candidate only*. Under decision-8 it never verifies anything by itself.
- **GitHub API** only when a token is present, as an extra source of evidence.
  Without a token, nothing waits on it and nothing is vetoed because it's
  missing.

### 2. Identify docs pages

A candidate URL becomes a docs root only when the site *is* documentation. That
is decided from evidence the site publishes itself:

1. **llms.txt / llms-full.txt manifest** (strongest). The H1 names the project,
   linked pages define the docs set and scope, and repo or package links found in
   the manifest count as evidence edges.
2. **Framework fingerprint**: generator meta and asset or search-index markers
   for Docusaurus, Sphinx/RTD, MkDocs/Material, Mintlify, Starlight, VitePress,
   GitBook, Fumadocs, Nextra, Hugo docs themes, rustdoc and pkgsite. The adapters
   exist; this promotes them from retrieval helpers to identification.
3. **Docs structure**: a sidebar nav tree, a version switcher, API reference
   pages. This is the weakest signal and can't identify a site on its own.

Blogs, Q&A sites, aggregators and mirrors fail this step even when they rank
first in search.

### 3. Verify without GitHub: evidence edges

A docs root counts for a project when **two independent edges** link them.
Keyless edges:

| Edge | Source | Strength |
|---|---|---|
| Registry metadata names the root (homepage, documentation, project_urls) | owner-set | strong |
| Root's HTML or llms.txt links to the repo the registry names (edit-on-GitHub, header repo link) | read from the page, no API | strong |
| Root shares a registrable domain with an owner-set URL | DNS ownership | medium |
| Registry-hosted docs for the exact package (docs.rs, pkg.go.dev) | registry | strong, single edge suffices |
| llms.txt H1 or site title equals the normalized project name | site | weak, tie-break only |
| Search hit | third party | never an edge |

The GitHub API homepage field becomes one more strong edge when a token exists.
Decision-8's rules carry over unchanged in spirit: no fork metadata, no
same-name repos under another owner, no unanswered stronger rival. "Unanswered"
now means a keyless source that failed, not GitHub's quota running out.

### 4. Pick or refuse without stars

Same-name projects (`click`, `solid`, `gin`, `next`) were ranked by GitHub
stars. Keyless replacements:

- **Registry popularity**: npm's downloads API, crates.io `downloads`, the Go
  proxy's importer count via pkg.go.dev, and pypistats for PyPI. All keyless.
- **Ecosystem hint** from the spec, the query, or the project files in the
  working directory (`package.json`, `pyproject.toml`, `Cargo.toml`, `go.mod`).
  An assistant asking about `click` from a Python repo means pallets/click.
- **Dominance rule unchanged**: a pick stands only when it dominates. Otherwise
  the response is an ambiguity error listing the candidates with their
  `--library` values. A refusal costs one round trip. A wrong project costs the
  assistant writing wrong code.

### 5. Resolution cache

Verified `library[@version] → docs root` mappings go into the page cache with
the normal TTL. Repeat queries, which is how assistants work, skip resolution
entirely. That covers most keyless latency and request budget. There's no
persisted index and no curated seed list (ADR-0004 still holds).

### 6. Shape the response for coding assistants

This is what makes docs different from search. Grading v2 found the failures,
and each maps to a general rule:

- **Relevance cutoff, not a fixed count.** Stop returning sections once scores
  fall below a gap from the top result, so a Basic Auth chunk can't fill out a
  bearer-auth answer. `--limit` stays a ceiling, not a target.
- **Answer-first ordering.** A section that has the query's API in a code block
  outranks prose that only mentions it.
- **Whole sections with intact code.** Never split an example. Remove MDX
  scaffolding, `[source]` and pilcrow noise, and token-per-line code.
- **Dedupe** across version and locale copies and llms-full chunks.
- **Every result labeled** with project, version, page URL and an anchor that
  was checked to exist on the fetched page.

## Benchmark: ketch-bench gains a `docs` suite

Extraction already lives in ketch-bench with a pinned corpus, baselines and
offline gates. Docs moves there too (`cmd/docsbench` and
`design/docs-benchmark` leave the ketch repo), with the same shape:

```sh
go run . docs setup            # fetch + verify the pinned world archive
go run . docs check            # offline replay vs baseline: deterministic, CI-able
go run . docs live             # keyless, 2 fresh + 1 warm, real network
go run . docs record           # refresh the world from a live run
go run . docs review           # emit judgment templates for changed chunks only
```

**Pinned world (record/replay).** Docs can't be benchmarked offline like
extraction unless the network is pinned. A `bench` build tag in ketch adds an
`httpx` transport hook (`KETCH_HTTP_RECORD=<dir>` / `KETCH_HTTP_REPLAY=<archive>`).
Release binaries don't carry it. ketch-bench builds `../ketch` with
`-tags bench`. A live keyless run is recorded once, including registry, search
backend, llms.txt and docs page responses. It's published as a release asset
pinned by sha256, like corpus-500. In replay, an unrecorded request makes the
case `INCOMPLETE`, never `PASS`, so the world has to be re-recorded rather than
silently drifting. Search-engine churn then can't move a regression check.

**Grading, mostly mechanical.** Cases keep the v2 contract (intent, accepted
roots and versions, required facts, prohibited confusables, sources). Required
facts are written as exact evidence anchors (quotes or patterns taken from the
official source), so project, version, facts-in-top-3, citations (anchor ids
checked inside the recorded world) and negatives score automatically on every
run. Semantic labels (relevance, waste, readability) are cached by chunk hash.
In replay, unchanged output keeps its reviewed labels and only changed chunks
need review. That keeps strict v2 grading affordable on every change rather
than once per release.

**Corpus.** 300 answerable cases plus negatives.

- **Dev set (200):** today's 83, then expanded.
- **Sealed holdout (100):** in a private `ketch-bench-holdout` repo. Only an
  evaluation agent reads it, never the agent fixing the backend. It is used at
  the release gate and refreshed after each use.

Strata, each reported separately:

- **Ecosystem:** npm, PyPI, crates, Go, other.
- **Discovery path:** llms.txt present / framework only / custom HTML.
- **Query form:** bare name, `--library` URL, version pin, natural-language
  how-to.
- **Ambiguity:** same-name projects, confusable APIs.

ketch-bench `check` gates: no case that passed in the baseline may regress, plus
the aggregate thresholds above.

## Default

When every gate passes keyless on replay, on three live runs and on the holdout,
`ketch docs` defaults to `web`. Context7 stays one config line away
(`ketch config set docs_backend context7`). This ADR then closes task-15.
Until the gates pass, the default stays Context7. Since task-18, Context7
needs no key: without one it runs on Context7's anonymous quota (about 200
requests a month per IP), and a spent quota says when it resets and how to set
a key. The `auto` backend (web first, Context7 only when web refuses; an
ambiguous name only from one of web's candidates, decision-13) is selectable
but not the default; it becomes a candidate for the default under the same
gates.

## Work plan

| Phase | Work | Done when |
|---|---|---|
| 0 | Commit phases 1–2 and grading v2 on `feat/web-docs` | history exists before anything moves |
| 1 | ketch-bench docs suite: `bench` tag record/replay, move harness and cases, mechanical grader, label cache | today's 83 cases replay deterministically; `check` green against a recorded baseline |
| 2 | Keyless resolution: search as a source, identification, evidence edges, popularity tie-break, resolution cache | keyless wrong project 0, keyless answer rate within 3 points of the token run on dev |
| 3 | Response shaping: relevance cutoff, ordering, dedupe, anchors, code integrity | strict v2 ≥ 90% on dev |
| 4 | Corpus to 200 dev + 100 sealed holdout, with source-checked gold | cases verified by a second agent; human spot-check of 10% |
| 5 | Evaluation and default: replay + 3 live + holdout, Context7 comparison if a key is available, doctor, MANUAL, README, skill | all gates pass, or this ADR records why not |

## Rejected

- **A curated library → docs-root registry.** It would make keyless resolution
  trivially accurate for popular libraries. But it drifts, it is what Context7
  already is, and it hides resolution bugs from the benchmark. The resolution
  cache gives most of the speed without that drift.
- **GitHub HTML scraping in place of the API.** Fragile, against GitHub's terms
  for automation at volume, and unnecessary given registry metadata and on-page
  repo links.
- **LLM judging in the regression loop.** Not reproducible. Mechanical facts plus
  hash-cached human or agent labels give the same rigor with stable results.
