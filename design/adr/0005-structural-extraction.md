# ADR 0005: Structural content selection instead of readability scoring

**Status:** Accepted · **Date:** 2026-09-20

## Context

Scrape quality is the value of every fetching surface (`scrape`,
`search --scrape`, `crawl`, `extract`, the MCP tools). Through 0.17 content
selection was go-readability: score every candidate element by text length,
link density and class-name hints, keep the best-scoring subtree, then convert
it to markdown. The 500-page benchmark ([ketch-bench](https://github.com/1broseidon/ketch-bench), 70 sites, 3,573
source-backed checks) put that pipeline at 3,176 passing checks, 1,692 of
1,831 critical, macro token recall 94.1%: whole sections of documentation
dropped, infobox tables lost, listing pages reduced to one entry, and every
fix was a guess at a score threshold that moved other pages.

Three ways out were considered:

- **Per-site rules** — hostname selectors for the sites that matter. Rejected:
  ketch serves the long tail an agent lands on, the rules rot, and a scraper
  that works only where it has been taught is not stateless in spirit.
- **A model or API in the loop** — hand the HTML to an LLM or a hosted
  extraction service. Rejected: it adds a key, a cost and a network dependency
  to the one command that must work on a fresh install, and it makes output
  nondeterministic, which the benchmark cannot gate.
- **Read the structure the page declares.** HTML already says what its parts
  are: a `main` or `article` landmark, `nav`/`aside`/`footer` furniture,
  `hidden` and `aria-hidden` controls, figure captions, data tables,
  definition lists, disclosure buttons and the panels they control.

## Decision

Content selection reads the page's own structure, in `extract/semantic.go`,
and readability stays only as the fallback for a page that declares nothing.

1. **Root selection**, first rule that applies: the wordiest content landmark
   (`main`, `[role=main]`, else an `article` that is not one entry of a
   listing); a container of uniform heading-bearing sections; the smallest
   element holding the page's prose.
2. **Pruning by what things are**, never by hostname: embedded landmarks,
   forms, spacers, hidden and collapsed controls (a panel a control names is
   content someone folded away), asides, tables of contents, link rails and
   teaser grids, layout tables, orphan headings. A page that is made of links
   — a section front, a topic index — keeps them; that decision is made once,
   the same way in both modes.
3. **Two modes** through the `extract_mode` config key. `clean` (default)
   also drops blocks by class and id name and by phrase — share bars, cookie
   notices, "was this helpful?" boxes, and listing cards that sit beside a
   story — for the leanest markdown. Names classify a block as furniture or
   as a listing; listing blocks that together hold the page are the page and
   stay. `complete` keeps everything the structure does not condemn, for a
   site whose own content is named like furniture.
4. **No site-specific code.** A rule must be stated in terms of HTML
   structure or a name's meaning and must hold across the corpus; a fix for
   one site that costs another is rejected.
5. **The benchmark is the gate.** Every change runs the corpus in both modes
   against accepted baselines (`go run . check` in ketch-bench); baselines are
   updated deliberately, from the harness's own `CGO_ENABLED=0` build, after
   every moved check is reviewed. `extract.PruneLog` attributes each removed
   element to its rule so a loss can be explained before it is fixed.

## Consequences

- On the same corpus the structural extractor passes 3,475 of 3,573 checks
  (1,825 of 1,831 critical), macro recall 99.4% at precision 98.1%; `clean`
  passes 3,468 (1,822 critical) at recall 99.0%, precision 99.4%. The
  remaining misses are annotation questions and long-tail structures tracked
  in the benchmark's ledger.
- Output for most pages changes between 0.17 and 0.18. Pages cached under
  0.17 are served from the cache until they expire; `ketch cache clear`
  re-extracts them on the next fetch. A non-default `extract_mode` is folded
  into the cache key so the two modes never share an entry.
- Regressions are fixed structurally: a new shape (a highlighter's
  line-number gutter, a lazily loaded image, a listing of `<article>`s) gets a
  rule and a fixture, not a selector. This keeps the extractor small enough
  to reason about — one file, one pruning order — at the cost of the
  occasional page whose markup lies about itself, which readability handled no
  better.
- The corpus must be maintained: new page shapes need pinned snapshots and
  checks before a rule for them is trusted, and a corpus change invalidates
  comparison with the old baseline by design.
