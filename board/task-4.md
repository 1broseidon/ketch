---
id: task-4
title: "Bench corpus follow-ups: re-scope annotation-only failures, fill missing page kinds, prune reports"
column: todo
position: 2
priority: low
tags:
  - improvement
  - ketch-bench
  - bro-91
createdAt: "2026-10-11T21:01:30.509Z"
updatedAt: "2026-10-11T21:19:51.020Z"
---

## Description
From the corpus half of the RC review. Not gating: nothing here blocks 0.18 unless a change moves the baseline numbers quoted in the changelog.

## Failing critical checks that are annotation problems, not extractor holes

Of the critical failures (9 in complete, 12 in clean), three are extractor holes tracked in the extraction issues (sqlite ×2: blockquote+pre; nasa-earth in clean: the `banner` token). The other nine assert on things the extractor is right to drop or cannot recover:

* teaser cards on section fronts ([un.org](<http://un.org>), RSPB, clean mode)
* video fallback text
* an `aria-hidden` tab pane
* MathML reference text that is garbled in the source

Re-scope those checks (make them non-critical, or change the expected text) so the critical count means "content a reader would miss".

## Composition gaps

500 pages, 70 sites, 28 kinds, 3,573 checks (1,831 critical). Cohorts: scale 320, scale-new-sites 80, expansion 60, regression 20, heldout 20. Kinds are dominated by docs, manual, api and tutorial. Missing entirely: news articles, non-Latin pages (the live set used ar/ja/zh Wikipedia and German pages), e-commerce, line-numbered code. Thin: 2 discussion threads, 11 en.wikipedia pages, 3 caption checks.

Add at least: a news article and a news front page, a non-Latin Wikipedia article, a product page, a Sphinx or Pygments page with line numbers, a blog index with several `<article>`s, a MathJax page, and figure captions. Each extraction issue under BRO-83 names the shape it needs a fixture for.

## Housekeeping

* `bench/reports/` is 11 MB across 43 files; only `initial100-corpus.json` is read by code. Prune or move the rest out of the tree.
* Pinned HTML contains live tokens: Springer `author_access_token` / `sharing_token` query strings, Fastly tokens in Wikipedia and NHS pages. Decide whether to scrub them from `bench/testdata`; they are in the public page source, but they sit in our repo forever.
* Boilerplate critical checks repeat across many pages of the same site, so one site's chrome weighs more than one article's body. Consider a per-site cap, or a macro-level gate (per-kind recall/precision) next to the per-check gate.
* The timing gate is covered in the bench mechanics issue.

Migrated from Linear BRO-91: https://linear.app/1broseidon/issue/BRO-91/bench-corpus-follow-ups-re-scope-annotation-only-failures-fill-missing (2026-10-11).

## Status (2026-10-11 review)

The benchmark moved to 1broseidon/ketch-bench (ec55c1c); reports pruning no longer applies. Remaining items (annotation re-scope, missing page kinds, per-site cap) belong in ketch-bench.
