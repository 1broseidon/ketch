# ketch research

Research takes one question and runs the full pipeline: search fan-out → scrape the top hits under a token budget → corroborate load-bearing claims with code or docs → synthesize a cited answer. This is the point of ketch: search, scrape, code, and docs live in one binary, so the whole loop runs without any other research tool. The output is an answer with sources, not a pile of page dumps.

---

## Preconditions

- Transport is decided (SKILL.md): the CLI with `--json` by default; the MCP tools when the operator has wired them into the session.
- If any call returns `[precondition]` / exit 5, pause the run, switch to `ketch setup`, resume after the surface is configured.

---

## Flow

1. **Plan — one line, load-bearing.** Decompose the question into 1–3 search queries with *distinct angles* (different vocabulary, different likely sources — not paraphrases). State the plan before the first call: `queries · scrape cap (3–5) · per-page budget (max_chars 4000–8000 + trim) · total call cap (default 8)`. Every later step is checked against this line.
2. **Fan-out.** Run each query, limit 5. On the CLI, add `--minimal` for triage. On `[upstream]` (ddg rate limit, DNS failure, 5xx): if you named a backend, rotate to another provider from `available_backends` in `ketch config` (skip `auto`, which is a chain, not a provider), retry that query once, note the rotation. If `auto` failed, it already fell through every usable provider: retry once, no rotation. Two backends failing → report the outage honestly; do not grind.
3. **Select.** Across all results, pick the top 3–5 URLs. Prefer primary sources (official docs or blog, the project's repo or issue tracker, the actual paper) over aggregators. Dedupe hosts. Skip a URL whose snippet already fully answers its part of the question.
4. **Fetch.** Scrape the selected URLs as one batch with `max_chars` and `trim` set. Then check `results[]` entry by entry — a batch succeeds (`isError=false`) even when individual URLs failed. A failed entry is a dropped source: name it in the synthesis, and pull the next candidate only if the lost content was load-bearing. A known-small page may skip the cap with a stated one-line reason.
5. **Corroborate — conditional, not ritual.** A claim about a library's API → `docs` (resolve → vet the match name → fetch by `library` ID). A claim that "people do X in practice" → `code` with `lang`, limit 3. Skip this step when the scraped sources already settle the question; corroboration is for load-bearing or contested claims.
6. **Synthesize.** Answer first, then evidence. Every claim carries its source URL inline. Conflicts between sources are stated and attributed, not averaged into fake consensus; resolve one only when a primary source settles it. Close with sources used, sources dropped (with error class), and what remains unverified.

---

## Budget rules

- Default cap: **8 research calls per run** (searches + scrapes + corroborations combined). Only the user asking to go deeper raises it.
- Scrape at most 5 pages per run; select first, never scrape every search result.
- Every scrape of an unknown page carries `max_chars` 4000–8000 and `trim`.
- Stop early when two independent sources agree and nothing contests them.
- Exceeding any cap requires a one-line stated reason the user can see.

---

## Output shape

```markdown
## <One-line answer to the question>

<2–5 paragraphs of synthesis. Every claim cited inline:
"the fix landed in 1.24 ([go.dev](https://go.dev/blog/...))">

**Conflicts / open points:** <stated, attributed — or "none">

**Sources:** used: <urls> · dropped: <url> ([upstream] 503) · unverified: <claim or "none">
```

---

## Rules

- No uncited claims. If a claim's source was dropped, either re-source it or mark it unverified.
- Rotate backends on `[upstream]`; never retry `[validation]` or `[not_found]` unchanged.
- When two sources disagree, the primary source outranks the aggregator.
- The plan line is a contract: fan-out, scrape cap, and call cap are checked against it, and any overrun is stated in one line.
