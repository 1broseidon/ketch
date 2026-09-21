# Extraction accuracy investigation

The September 2026 bookmark trials exposed content loss in HTML extraction,
independent of tag storage. Correct agent answers did not demonstrate complete
source retrieval: agents could use other paragraphs, follow links, or select
sections explicitly.

## Observed causes

The default pipeline in `extract/extract.go` passes HTML through
`go-readability/v2` v2.1.1, then `html-to-markdown/v2` v2.5.0. The selector path
bypasses Readability. Replaying saved HTML isolates these stages from network,
page-cache, rendering, and output-limit effects.

1. **Code comments mistaken for website comments.** pkg.go.dev uses
   `<span class="comment">` inside `<pre>`. Readability's unlikely-candidate
   filter removes these spans; its ancestor exemption covers nearby `code`
   elements, not bare `pre` elements. Debug logs explicitly report removal of
   these spans before markdown conversion. npm syntax comments suffer the same
   problem.
2. **Code layout stored in elements, not newline characters.** npm's Prism
   markup wraps lines in `<div class="token-line">`. Reading only text nodes
   concatenates adjacent lines. Some conversion paths instead introduce extra
   blank lines. YAML can become invalid even though all the words survive.
3. **Reference declarations treated as low-value links.** Readability's logs
   report removal of several Go declaration containers for link density or
   short content. Type names in signatures are hyperlinks. Normalizing code
   to literal text also recovered these standalone signature blocks.
4. **Whole-section selection loss.** npm's prerequisites are present in the
   fetched HTML but absent from Readability's intermediate HTML. Its chosen
   content container and sibling filtering omit introductory material. This
   happens before markdown conversion and persists after code normalization.
   That page's intended main container is actually `div[as="main"]`, so the
   conventional `main` selector does not match it.
5. **Missing or incomplete URL context.** Selector scraping supplied no source
   URL. Raw fallback conversion supplied only scheme and host, resolving
   directory-relative references against the wrong directory. The stdin
   `extract --select --url` path resolved attributes before matching selectors,
   breaking selectors that depend on the original relative attribute values.

## Changes on the branch

`extract/code.go` normalizes preformatted blocks before Readability or direct
conversion. It preserves literal code, indentation, blank lines and visible
block/BR boundaries, while removing highlighted token wrappers and hidden
controls. Code language classes survive Readability. This is a local adapter;
no dependency upgrade or fork is involved.

Selector conversion now accepts URL context and resolves links **after**
matching against the original DOM. Both CLI extraction and shared scraping use
this path. Scraping uses the rewritten fetch URL as the base while preserving
the original source identity in its result. The raw fallback uses the full
page URL. Empty anchors stay empty.

### Contract ledger

| Surface | Change | Risk | Compatibility | Verification |
| --- | --- | --- | --- | --- |
| `Extractor.Extract` / returned markdown | Preserve code and correct fallback links | R2 | Same signature and result fields; corrected markdown can be longer | Code/links regression tests; five-page replay |
| `ExtractSelector` | Preserve code formatting | R2 | Same signature and no-URL link behavior | Existing selector tests plus whitespace fixtures |
| `ExtractSelectorWithURL(pageURL, rawHTML, selector)` | Add URL-aware conversion | R1 | Additive package function; no-match still returns empty text | Attribute selection and link tests |
| CLI/MCP scraping | Resolve selector links | R2 | Same flags, tool inputs, JSON fields, errors and source identity | Rewritten-URL HTTP fixture; CLI and MCP suites |

Target: shared `extract` package. Caller profiles: agent, script, sdk-client.
Obligations: stable package signatures and machine output, retained source
identity, no network I/O in extraction, reproducible offline regressions.

## Saved-page replay

These results compare the previous installed binary with the candidate using
the **same saved HTML**, through `ketch extract --url ... --json`, without
output truncation. Expected code text comes from source `pre` elements, with
HTML block line boundaries reconstructed and leading/trailing newlines ignored.
A block passes when its complete text appears in a fenced markdown block.
This is a code-preservation check, not a whole-document accuracy score, code
execution test, or measurement of CSS-generated content.

| Source | Intact code blocks before | After |
| --- | ---: | ---: |
| Python errors tutorial | 26/26 | 26/26 |
| Go context reference | 25/34 | 34/34 |
| Kubernetes probe configuration | 24/24 | 24/24 |
| PostgreSQL transaction isolation | 7/7 | 7/7 |
| npm trusted publishing | 2/7 | 7/7 |
| **Total** | **84/98** | **98/98** |

The extracted npm GitHub Actions workflow fails YAML parsing before the change
and parses afterward. Default Python and PostgreSQL markdown is byte-identical
before and after; Kubernetes retains its code text and gains fence languages.
Go's missing interface comments and signature blocks return. npm's prerequisite
versions remain missing in default mode; selecting its content container
recovers them. API headings and other content can still be removed.

Ten alternating runs per binary on saved npm, Go and Kubernetes pages measured
median CLI extraction times of 35.3→42.0 ms, 19.3→22.6 ms and 39.0→47.5 ms.
These include process startup, exclude networking, and describe only this
machine and sample. They do not establish a general performance bound.

Local investigation artifacts are in `/tmp/ketch-extraction-audit/`: source
HTML, Readability intermediate HTML/debug logs, before/after markdown,
`replay.py`, `replay-results.json`, and `timings.json`. The candidate binary is
`ketch-after`; the installed binary was not replaced during this investigation.
Reduced regression fixtures live in the repository tests and need no network.

## Verification

| Check | Result |
| --- | --- |
| New reproduction tests against original code | Failed on Go comments, npm YAML, code whitespace and selector URLs as expected |
| `CGO_ENABLED=0 go test ./...` | Pass |
| `go test -race ./extract ./scrape ./cmd ./mcp` | Pass |
| `go vet ./...` | Pass |
| `golangci-lint run` | Pass, zero issues |
| `go fmt ./...`; `git diff --check` | Pass |
| Standalone `staticcheck ./...` | Installed analyzer cannot decode this Go toolchain's export format; lint's configured analysis passes |
| Standalone `gocyclo -over 15 .` | Existing findings in `cmd.applyConfigSet` and `mcp_test.TestMCPServerSmoke`; none in changed functions |

No release, push, config edit or real-cache mutation was performed. Previously
cached markdown is not reprocessed automatically; testing the new extractor on
a fetched page requires `--no-cache` or an isolated fresh cache.

## Next improvement: content selection

Do not treat the code-block result as proof that Readability preserves all
documentation. The remaining npm failure needs a selection strategy, not a
different markdown serializer.

Evaluate a documentation path that converts a complete content container after
removing identified navigation and controls. Prefer explicit document landmarks
when available; do not assume every site emits valid semantic `main` markup.
Compare its output with Readability using annotated critical facts, headings,
callouts, tables, code blocks, and unwanted navigation. A fallback should recover
specific missing content without defaulting to the entire body.

The [500-page, 70-site benchmark](../bench/README.md) now covers 28 content
types, with 3,573 source-backed assertions and 1,831 critical checks. The
[review](../bench/REVIEW.md) records comparisons of both binaries on identical
pinned input, separate new-site cohorts, per-page omissions and concurrent
consistency checks. Original 100-page source records and output accuracy remain
available for direct comparison. These measurements describe selected content
fidelity, not semantic truth or a population estimate of web accuracy.

The larger sample confirms content-selection losses across Git manuals, W3C
accessibility tutorials and safety pages. A subsequent [live audit](../bench/AUDIT.md)
corrected the Redis lists interpretation: its code examples survive, while the
reference contains hundreds of auxiliary API-signature panels. Review annotation
scope alongside full-text coverage and explicit structural assertions. Generic landmarks,
introductory sections, substantive asides and disclosure panels remain the next
extraction work. No production extractor changes were made for the expansion.

Assisted selection is reported separately. The CLI's single-selector parser
still rejects valid comma-separated selector groups; failed cases remain in
its denominator. Fix that generic validation mismatch separately from content
selection, and do not use an assisted score to conceal default-path losses.

An [eight-page source-scope erratum](../bench/reports/annotation-errata500.json)
records recipe photo-gallery comments/controls and safety newsletter copy that
had survived their intended exclusions. Both binaries were rerun on corrected
references; provisional evidence is retained. Source validation now applies the
declared exclusions to positive assertion evidence while retaining the original
DOM for noise checks. Independent human annotation review remains outstanding.

The extraction adapter still has no hostname, Go-language or site-selector
rules. Review narrowed its inline-style check so custom CSS properties do not
erase code, retained simple declaration precedence, and excluded inert
templates. Cross-language regression fixtures cover Python, JavaScript, JSON,
shell, YAML and Go. Existing extractor dependencies are unchanged; Goldmark is
used only by the new benchmark's independent Markdown scorer.

Before enabling an automatic content-selection strategy, evaluate it on fresh
held-out sites and rendered documentation. Gate critical facts and structural fidelity
separately from text recall and precision. Keep selector recovery separate so
successful assisted extraction cannot conceal silent default loss. The
benchmark guide documents pinned corpus expansion, source annotation review,
bounded per-worker fixture loading and explicit regression-baseline acceptance.

## Resolution: structural content selection

The content-selection strategy proposed above shipped as the default in
`extract/semantic.go`. Selection reads the page's own structure, in order: the
landmark the page declares (`main`, `[role=main]`, or an `article` carrying a
real share of the page), a document assembled from uniform sibling sections
(asciidoc, DocBook, hand-rolled `section` per chapter), or the smallest element
holding the page's paragraphs, found by descending from `body` and leaving a
sibling behind only when it looks like chrome. Readability remains the fallback
for a page that declares none of these, and the raw conversion the fallback for
readability.

Site furniture is then removed by what it is, in a fixed order: elements that
render as nothing, controls, hidden elements, then — on a fresh word count —
asides, in-page tables of contents, teaser grids and blocks made of links, with
a hub page (a topic index made of links) recognised and kept whole. Collapsed
content stays: an accordion panel behind a disclosure button, a code sample on
an unselected tab, a `hidden=until-found` section. Layout tables are unwrapped;
data tables keep their shape. There are no hostname, language or per-site
selector rules, and vocabulary that matched a single publisher was removed.

The `extract_mode` config key decides what the pruning may drop. `complete`,
the default, uses only the structural rules. `clean` also drops blocks by name
and phrase — related-post rails, comment threads, share bars, cookie notices,
"was this page helpful?" boxes. Readability's own candidate scoring already
dropped by name (its unlikely-candidates regex), so `complete` is stricter
about names than the extractor it replaces, not looser.

Measured on the 500-page benchmark with the same corpus and assertions:

| Run | Checks | Critical | Macro recall | Macro precision |
| --- | ---: | ---: | ---: | ---: |
| Readability (0.17) | 3,176/3,573 | 1,692/1,831 | 94.13% | 98.07% |
| Structural, `complete` | 3,475/3,573 | 1,825/1,831 | 99.39% | 98.11% |
| Structural, `clean` | 3,468/3,573 | 1,822/1,831 | 99.00% | 99.44% |

Both modes are gated by `bench check` against their own baselines
(`bench/baseline.json`, `bench/baseline-clean.json`). The remaining misses are
recorded in the per-mode `RESULTS` snapshots. The earlier decision stands: no
model or API intermediary sits in the extraction path.

