# Broad open set

`broad.json`: 95 cases (90 with facts, 5 `not_found` controls) on 34 docs sites, drawn from ketch-bench's gold cases (`docs/cases.json`, `docs/general/cases-dev.json`) on 2026-11-10. Open, never sealed: report rules on it, do not tune to single cases. It measures Sort, Rank and Output on a built index (`--library`), not site or page picking.

The set grew from 82 to 95 on 2026-11-10 (round 4), when the runner's normalisation began to strip Markdown emphasis and link syntax on both sides: 13 of the 14 cases dropped for markup now verify, and two kept cases gained an alternative that had been dropped for the same reason (`react-effect-cleanup`, `electron-expose-api-preload`).

## Build

```sh
python3 -I internal/docsqa/broad_cases.py ~/Projects/personal/ketch-bench > candidates.json
ketch docs index <root> --max-pages 400            # each root in broad_cases.py SITES, in the sandbox
go run ./internal/docsqa -sandbox <dir> -cases candidates.json -verify internal/docsqa/cases/broad.json
```

`-verify` checks every quote against the index with the runner's normalisation (backticks, emphasis, link syntax and escapes out, whitespace collapsed, lower case), resolves each quote's `source` to the indexed section that holds it, keeps the alternatives whose quotes are all found, and drops a case when a fact has none left. A quote counts wherever the library serves it (the bench's rule), its gold page first.

## Run and compare

```sh
go run ./internal/docsqa -ketch <binary> -sandbox /tmp/ketch-evidence-broad -traces /tmp/ketch-evidence-broad/traces/<label> \
  -cases internal/docsqa/cases/broad.json -split tune -json <results>.json -no-fail
go run ./internal/docsqa -compare internal/docsqa/cases/broad.old.json,internal/docsqa/cases/broad.round4.json -labels old,round4 -split confirm
```

`-split tune|confirm|all` (default `all`) restricts a run or a comparison to one half. Results: `broad.old.json` (old ranking), `broad.round1.json`, `broad.round2.json` (82 cases, older runner), `broad.round3.json` and `broad.round4.json`; old, round 1, round 3 and round 4 were run on all 95 cases with the round 4 runner, on one shared index.

| Build | All (90) | Tune (46) | Confirm (44) |
|---|---|---|---|
| old | 51 | 26 | 25 |
| round 1 | 46 | 23 | 23 |
| round 3 | 27 | 16 | 11 |
| round 4 | 65 | 34 | 31 |

## Split

The set is split by site into two halves of similar size and mix of kinds. Rules and values are chosen on `tune`; `confirm` is run once per candidate build and never tuned on. Each case's `split` field names its half. The Duo and Click cases in `duo.json` and `click.json` (no `split` field) count as tune: they have been looked at too much to confirm anything.

| Half | Cases (with facts + controls) | Kinds | Sites |
|---|---|---|---|
| tune | 48 (46 + 2) | library 33, saas-product 5, infra-ops 5, cloud-ci 3, spec 2 | duo.com/docs (3), docs.github.com/en/actions (2 + 1), nginx.org/en/docs (4 + 1), developer.okta.com/docs (2), openid.net/specs (2), react.dev (5), fastapi.tiangolo.com (4), ai-sdk.dev/docs (3), bun.uptrace.dev (3), python.useinstructor.com (3), prefect.io/docs/v3 (3), svelte.dev/docs/kit (3), textual.textualize.io (3), docusaurus.io/docs (2), sea-ql.org/SeaORM (2), tanstack.com/query/latest (2) |
| confirm | 47 (44 + 3) | library 34, infra-ops 7, saas-product 4, cloud-ci 2 | docs.aws.amazon.com/lambda (2), kubernetes.io/docs (3 + 1), postgresql.org/docs/current (2 + 1), docs.stripe.com (1 + 1), docs.slack.dev (2), nextjs.org/docs (4), docs.astro.build/en (3), electronjs.org/docs/latest (3), pillow.readthedocs.io (3), psycopg.org/psycopg3/docs (3), docs.temporal.io (3), tokio.rs (3), hono.dev/docs (2), docs.scrapy.org (2), docs.streamlit.io (2), golang.testcontainers.org (2), testing-library.com/docs (2), router.vuejs.org (2) |

## Format

A fact is `{quote, source}` or `{alternatives: [[{quote, source}, ...], ...]}`: it passes a check when every quote of any one alternative passes it, as in the bench. `kind` labels the case for `-compare`; `split` names its half.

## Selection

| Kind | Sites (cases kept) |
|---|---|
| saas-product | duo (3), stripe (1 + 1 control), okta developer (2), slack (2) |
| infra-ops | nginx (4 + 1 control), kubernetes (3 + 1), postgresql current (2 + 1) |
| cloud-ci | GitHub Actions (2 + 1), AWS Lambda (2) |
| spec | openid.net specs (2) |
| library | react.dev 5, next.js 4, fastapi 4, tokio.rs 3, electron 3, instructor 3, prefect v3 3, psycopg 3, pillow 3, textual 3, bun 3, temporal 3, astro 3, sveltekit 3, ai-sdk 3, hono 2, tanstack query 2, docusaurus 2, streamlit 2, scrapy 2, seaorm 2, testcontainers-go 2, vue router 2, testing-library 2 |

Generators (for the first 82 cases): custom Next.js 17, Docusaurus 12, MkDocs Material 10, Sphinx 8, nginx XSLT 5, VuePress 4, Hugo 4, other custom 4, VitePress 3, Mintlify 3, DocBook 3, Starlight 2, SvelteKit 2, TanStack 2, xml2rfc 2, AWS 1.

18 of the first 82 questions do not name their library ("bearer auth", "insert many rows at once"). They are kept as written: `--library` supplies it.

## Excluded

| Class | Count | Why |
|---|---|---|
| Version-pinned cases | 35 in the bench | need a version other than current; not tried |
| Package-set negatives | 10 | test library or version resolution (unknown package, future version), which `--library` bypasses |
| systemd man, Cloudflare Workers | 2 sites, 3 cases (1 a control) | dry run: the gold pages are not among the first 400 |
| Sites not picked | the rest | mostly 1 to 2 cases each; also GitHub-only roots, the whole RFC series, AWS S3 and IAM guides |

Of 110 candidates on the 36 sites indexed, `-verify` dropped 15:

| Reason | Cases |
|---|---|
| Gold page not indexed: past the 400-page cap (duo, okta, stripe, slack, postgresql, docker) or not in the 7 pages of GitHub's llms.txt (2) | 8 |
| Quote not on the indexed page: docker (2) and pydantic AI (3) indexes hold the page description only; streamlit (1) | 6 |
| Quote differs from the index in its words, not its markup (stripe: "the exact UTF-8 string Stripe sent") | 1 |

Docker (400 pages, 400 sections of about 170 bytes) and pydantic AI (254 of 361 pages under 300 bytes, from `llms-full.txt`) index without page bodies; neither contributes a case.

## Code-live

`code-live.json`: 11 bare coding questions from the live web-search run of 2026-11-10 (`/tmp/ketch-code-check/run.py`), on the tune side (`split: tune`, kind `code-live`). Each is run with `--library` against the partial library that run stored (about 24 pages each, read for that one question), copied into a sandbox of its own with the libraries' `resolved: search` mark taken off, so that `--library` answers from the index and never searches. Quotes verified against the stored pages with `-verify`. Dropped: `polars-group-by` ("Simply list all of the expressions you want inside the function agg."), which is in the Polars user guide, not in the 24 API reference pages stored.
