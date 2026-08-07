# ADR 0003: Fast-path-first scraping (HTTP before browser)

**Status:** Accepted · **Date:** 2026-08-04

## Context

Some web pages render their content server-side and are readable straight from
an HTTP response. Others ship an almost-empty HTML shell and build the real
content in the browser with JavaScript (React/Vue/Svelte SPAs, streaming
hydration frameworks). A scraper has to handle both, and the two cases have very
different costs: a plain HTTP fetch is cheap and fast; launching a headless
Chrome, waiting for load and for the DOM to settle, is expensive.

The question is when to pay the browser cost.

## Decision

Scrape takes the **fast path by default and escalates only on evidence.**

1. Do a plain HTTP fetch first.
2. Run the fetched HTML through a JS-shell detector (`extract/detect.go`) that
   looks for the signatures of client-rendered pages: near-empty mount nodes,
   known framework markers, a script-payload-to-visible-text ratio that implies
   the content isn't in the HTML yet, and operator-supplied `spa_markers`.
3. Only if the page is judged a JS shell, re-fetch it through the configured
   headless browser and extract from the rendered HTML.

The output shape is identical regardless of which path ran — the caller never
has to know a browser was involved. A `--force-browser` flag overrides the
heuristic for the rare page the detector misjudges, so the default can stay fast
without trapping anyone.

The rejected alternative is *always browser*: simpler to reason about (one code
path) but it pays the expensive cost on every fetch, including the large
majority of pages that never needed it.

## Consequences

- The common case (server-rendered pages) stays cheap; the browser cost is paid
  only when detection says it's necessary.
- Correctness for JS-rendered pages is preserved transparently — agents and
  humans get clean markdown either way, with no flag to set.
- The detector is now load-bearing: a false negative silently returns a shell's
  worth of empty content, and a false positive wastes a browser launch. This is
  a deliberate trade — the heuristic is tuned to be improvable over time
  (see [`ROADMAP.md`](../ROADMAP.md)), with `spa_markers` and `--force-browser`
  as operator escape hatches while it improves.
- A headless browser must be available for the escalation path; without one, a
  genuine JS shell can't be rendered. The fast path still works with no browser
  configured at all.
- The same pipeline (`scrape/pipeline.go`) is shared by the CLI and the MCP
  server, so both inherit this behaviour identically.
