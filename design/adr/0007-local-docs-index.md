# ADR 0007: A local docs index, built once per library and queried offline

**Status:** Proposed · **Date:** 2026-11-02 · Supersedes the local-corpus
rejection in [ADR-0004](./0004-tagged-cache-corpus.md) (tags themselves are
unchanged)

## Context

The keyless `web` docs backend answers every query live: it identifies the
library, verifies its docs site, finds the version, picks about six of the
site's pages and ranks their sections, all within about two seconds and
against rate-limited registries, search providers and the site itself. Three
rounds of rules moved the sealed holdout from 27% to 33%. Almost every miss
is in the first three steps, which are judgments made from scratch on every
query, and safe answers mean refusing when unsure, so refusals were about
half of all misses.

Two offline measurements changed the picture:

- **Retrieval.** On 13 fully crawled docs sites (about 2,400 sections each,
  27 scorable dev cases), dense cosine with bge-base-en-v1.5 put the answer
  in the top 10 sections 89% of the time and in the top 3 70%; BM25 over the
  same sections managed 70% and 37%. bge-small scored 81%/63%, nomic 81%/70%,
  mxbai-large 85%/67% at three times bge-base's size. Keyword fusion (RRF),
  page-first two-stage ranking and a cross-encoder reranker added nothing at
  full-site scale. The remaining misses came from the quick crawl (other
  versions' and locales' copies of a page; long sections cut before their
  key part), not the model.
- **Runtime.** `github.com/rostamlabs/rembed` v0.3.0 runs bge-base in pure Go
  (CGO_ENABLED=0, cross-compiles to linux and windows) at parity with the
  Python reference: mean cosine 0.99999 over 24,000 texts and identical
  retrieval. On an 18-core arm64 laptop it embeds 33 sections/s (bge-small
  101/s), a single query in 6.5 ms at the median, and loads in 0.2 s. Its
  int8 mode is slower on arm64 (26/s) and is not used.

ADR-0004 rejected an earlier SQLite FTS5 corpus with `docs add/list/remove/
sync`: it was document management where the need was remembering sources.
That remains true for bookmarks. It is not true for retrieval: the
judgments that fail live are cheap to make once and expensive to make on
every call.

## Decision

**Index once per library@version; answer every query locally.**

- `ketch docs index <library|docs-url> [--version v]` resolves the docs root
  with the web backend's existing machinery (a docs URL skips resolution; an
  ambiguous or unverified name refuses with its candidates exactly as a
  query would, and the caller passes the URL instead), enumerates the site
  through the existing discovery (llms.txt, llms-full.txt, sitemaps scoped to
  the root, the site's search index, nav links), keeps only pages under the
  resolved version root and in the root's locale, caps the page count
  (1,500 by default), fetches through the scrape pipeline and page cache at a
  polite concurrency (robots.txt disallows are honoured), splits pages with
  the web backend's sectioning (never inside fenced code), embeds and stores.
  Re-running refreshes only pages whose content hash changed.
- A `local` docs backend answers from the index: it embeds the question
  (with bge's query prefix; passages get none), takes the cosine top 30
  sections by brute force, groups them by page, trims long sections to the
  blocks that name the query, and returns about 8–10 page results with page
  URL, anchor and breadcrumb. The agent calling ketch picks; there is no
  decision model.
- The auto chain becomes local → web → Context7. Local answers when the
  library is indexed; otherwise, or when it finds nothing, the existing chain
  runs unchanged. The default docs backend stays Context7 (decision-10).
- **The model is optional.** `ketch model install` downloads bge-base
  (`--small` for bge-small) into ketch's cache directory, pinned to a
  Hugging Face revision and verified by size and SHA-256; `model_path`
  points elsewhere. Without a model, indexing stores text only and the local
  backend ranks with the web backend's BM25, saying so in the outcome's
  reason (`keyword_ranking`). The model only embeds text: everything returned
  is a verbatim section of the indexed site.

### Why the lifecycle stays light

What ADR-0004 rejected was a corpus someone curates. Phase 2 syncs the index
from the project's lockfiles (package-lock.json, go.mod, Cargo.lock, uv.lock,
poetry.lock), so the index follows the dependencies already declared and
nobody keeps a list. `docs index` by hand is the exception, and `--list` and
`--remove` are its only management verbs. There are no tags, collections or
per-document edits, and the index never replaces the page cache: pages are
fetched and cached exactly as for `scrape`.

### Storage and versioning

- One bbolt file, `docs-index.db`, in ketch's cache directory, separate from
  `cache.db` and `tags.db` and opened per operation (ADR-0006), so queries
  from MCP servers and CLI calls share it with an indexing run. A build
  writes in short batches, never one long transaction.
- Per library: metadata (the library as given, the names it answers to, docs
  root, version, how it was resolved and the evidence, the model id and
  dimensions, the index format version, build time, page and section counts,
  discovery sources, whether the page cap cut it short), pages (URL, title,
  content hash) and sections (page URL, anchor, heading path, text, and one
  or more vectors).
- A long section is embedded as several windows of about 1,200 characters,
  each prefixed by its heading path, split at block boundaries, so nothing
  falls past the 512-token window; the section scores as its best window.
- Vectors are stored as float16 (768 dimensions: 1.5 KB per window). Cosine
  error from float16 is below 1e-3, far under any ranking margin.
- Changing the model id invalidates that library's vectors: the next
  `docs index` (or `--reembed`, without the network) re-embeds the stored
  text; nothing is re-crawled. A different index format version asks for a
  rebuild.

### Fallbacks

local (embeddings, or BM25 with no model) → web (the live keyless path,
unchanged) → Context7 (keyless quota or key, decision-13's gates). Every
attempt is recorded in the docs Outcome (decision-20): `not_indexed` when the
index does not hold the library, `no_results` when it holds it and nothing
matched. An index that holds the library but has no usable vectors (no model,
or another model) answers with BM25 rather than refusing.

## Measurement

Two suites replace fighting the live web, both in ketch-bench:

- **Index accuracy:** for each benchmark library, did `docs index` pick the
  right site and version? Gate: zero wrong roots; refusals are logged.
- **Offline retrieval:** frozen indexes, every dev and holdout case,
  deterministic. Primary metric: the answer is in what ketch returns (top
  10); top 3 reported too. The sealed holdout stays sealed.

The proposed target is the answer in the top 10 on the sealed holdout with
zero wrong roots. A refusal floor (a calibrated minimum cosine) is phase 2;
phase 1 returns the nearest sections.

## Consequences

- ketch gains a second durable store and a model download of 438 MB
  (bge-base, float32 safetensors as published) or 133 MB (bge-small). The
  binary stays static and CGO-free; the runtime adds about 1 MB.
- Indexing a 600-page site takes minutes (fetching at concurrency 4, then
  embedding); queries take tens of milliseconds and no network.
- Freshness is per build: pinned versions never change, and a re-run
  re-fetches through the page cache and re-embeds only changed pages.
- The live web path keeps its role as the fallback and as the source of the
  index's judgments; improvements there still pay off twice.
- The MCP `docs` tool can query the local backend; building an index is
  CLI-only for now, since it runs for minutes.

## Amendment 1 (2026-11-03): one version per index, code-first snippets

A blinded two-judge comparison on 68 validation questions (ketch-embed
`runs/fair-judge`, board task-30) put Context7 with version-pinned IDs ahead
of the local results: top-5 "fully answers" 85% against 82–84%, "an agent
could write correct code" 95% against 84–88%, and at an equal ~880-token
budget 87% against 75–77%. The local index found the right material about as
often; it lost on two things. Version-pinned answers mixed the asked version
with others in 36–45% of cases, and how-to answers (57–73% against 83–87%)
were long raw sections where Context7 gave a title, a sentence or two and
the code.

**Decision.**

- **An index holds exactly one version.** `docs index name@version`
  resolves that version's docs root with the web backend's version
  discovery; with no version, the current docs, labelled with the package's
  latest release when the site names none. Only the root's tree is kept:
  other hosts and paths outside a versioned root never qualify, and below the
  root a tree is dropped when the site's version list names it as another
  version, when it is a release or channel directory right below the root
  (`/docs/2.x/`, `/next/`, mike's `/1.0/`), or when it is mostly copies of
  the current pages (the earlier rule). The key stays the docs root, so each
  version (each its own root) sits beside the others; the metadata records
  the version and the version asked for. `--library name@version` selects one
  and refuses an unindexed version as `not_indexed`, naming those indexed;
  no version answers from the newest (current docs first) and says which.
  A section in the tree about a later major (its page or headings name a
  migration or upgrade and a higher major, e.g. "Migrating to Vue Router 4"
  in the v3 docs) is kept, labelled, and demoted by 0.05 cosine unless the
  question is about migrating or names that version. The detection is
  deliberately narrow: no number, or an earlier one, means no label.
- **Snippets are the default answer.** At index time each section is cut
  into units that are byte ranges of its text, so they stay verbatim: every
  run of fenced code (tabs together) with the one to three sentences right
  before it, and the prose no code claims (a section with no code: its first
  ~600 characters, cut at a sentence). Each unit is embedded (heading path,
  explanation, code, within the window) beside the section windows. A query
  scores a unit as half its own cosine and half its section's best window,
  plus up to 0.06 by the share of the identifiers the question writes that
  the unit names, plus 0.03 for code (minus 0.03 for a single block over
  2,000 bytes, a listing more often than an example), and returns the best
  units within ~1,200 tokens and 8 snippets, never two near-duplicates, at
  most two from one section and three from one page, each rendered as
  `### heading path`, explanation, code, `source: url#anchor`. The grouped
  section view stays behind `--sections` (MCP `sections`).
- **Index format 2.** Units, unit vectors and the cross-version label
  change the stored schema. A format-1 library is never read: queries fail
  with `index_outdated` naming the rebuild, `--reembed` refuses, and the next
  `docs index` rebuilds it.

**Consequences.** A build embeds roughly 2.3 to 2.6 times as many passages
as before (hono: 819 windows and 1,093 units; astro: 5,009 and 6,488). On 12
live how-to and version questions (hono, fastapi, astro, vue-router at two
versions) answers ran 296–1,123 o200k tokens, 3–8 snippets each, with no
cross-version URL either way between the two Vue Router indexes. The weights
above were set by reading those answers, not fitted on an evaluation set. Nothing is rewritten, so where a
page puts its explanation after the code, or the code under a list item the
sectioning does not read as a fence, a unit carries less context than a
curated snippet would. Measuring the change against the fair-judge packets
is the next step.

## Amendment 2 (2026-11-03): whole sections again, ranked by section, fused with keywords; coverage and names

The fair-judge round 3 (68 cases, blinded) put the code-first snippets of
amendment 1 at 66% top-5 "fully answers", against 86% for the earlier
section output and 85% for Context7. Version purity worked (no version
conflicts, from 45%). A retrieval check over the 63 gold-labelled dev cases
showed why the snippets lost: ranking small units let an API stub with code
outrank the prose that states the rule, and one section came back split or
twice. It also showed what the index missed: pages a 1,500-page cap left out
(NumPy's and Matplotlib's user guides behind thousands of generated
reference pages), roots that were not where the docs are (DuckDB's
`/docs/stable/`, CrewAI's and Hugging Face's release directories, an Angular
version host that serves its app shell for `sitemap.xml`), and names that
picked an older version's index (`angular`, `vue`) or a sibling (`svelte`).

**Decision.**

- **Sections are the default answer again, ranked at section level.** Each
  section is scored by its best window's cosine and, separately, by BM25F
  over its heading (three times its text), breadcrumb and text; the two
  rankings are fused by reciprocal rank fusion (k = 60). Ranked sections are
  grouped into families, a section with the others under the same H2 of its
  page, and each of the best five families is one result: the H2's own text
  and the subsections of it that ranked, verbatim with their code, within
  about 1,800 tokens and 600 per result. A section that does not fit keeps
  its lead prose, its first code block (cut, fence closed) and the blocks
  that name the question. Two families with the same heading and text (a
  page at two addresses) are one, and a page gives at most two families
  while other pages rank next. Results carry `anchors`, the sections they
  show. Code-first snippets stay behind `--snippets` (MCP `snippets`); the
  page-grouped view is retired. Units are still cut at index time but no
  longer embedded (about 2.3-2.6 times fewer passages per build): the
  snippet view scores a unit by its section's cosine and the share of the
  question's words it names. No index format change.
- **Fewer knobs than were tried.** On the 63 cases, section-level ranking,
  families and the keyword fusion each moved the gold section into the top
  five more often; a page-level aggregate, a penalty for thin API stubs or
  reference paths, and a prior for a page's first sections each moved it by
  one case or less, and are not used.
- **Coverage.** Under the page cap, pages are taken in priority order: the
  pages llms.txt lists (and a page seeder names, below), then guides, then
  the rest (examples and galleries included), then generated API reference
  leaves and release notes; directories take turns within a tier. `x` and
  `x.html` are one page, `/llms/` text mirrors are dropped beside rendered
  pages, and a fetched page whose text is another's is dropped (a missing
  page served as the home page among them). A root that stands for a
  numbered release (redirect, meta refresh or canonical) is indexed at that
  release; an unversioned root whose pages or llms.txt sit in one release
  directory is indexed there, and shown under the unversioned addresses
  when the release's pages name them canonical (never for an older
  release). A version host whose lists are an app shell probes the current
  host's paths. `docs index --dry-run` prints the plan.
- **Names.** A build records the docs site's own name (og:site_name or the
  root page's title) and the registry packages its root links to under
  that name; both are names the library answers to, as are its root's
  distinctive path segments (secondary). A name that matches any index of
  a project brings in that project's other versions (same host without a
  version label, same path without version segments), so `angular` with
  `angular@18` and angular.dev indexed answers from angular.dev.
- **Seeds.** `IndexRequest.Seeder` (a `PageSeeder`) names pages the web
  points to for a library; they enter the plan first. Nothing implements
  it yet: the provider is chosen by a separate search scorecard, and seeds
  must come from library-level questions, never an evaluation's.

**Measurement** (ketch-embed `scripts/local_retrieval.py`, 63 cases, the
library given as its docs root; "fair" counts a page's lead section for its
H1's anchor, the anchors a result shows, and, for unpinned cases, a page
under another release segment):

| | page@5 fair (strict) | section@5 fair (strict) | answer@5 | tokens, top 5 |
|---|---|---|---|---|
| snippets (amendment 1), round-3 index | 51 (48) | 40 (36) | 28 | 670 |
| section families, round-3 index | 55 (52) | 51 (38) | 44 | 1,576 |
| section families, 6 sites rebuilt | 59 (57) | 54 (41) | 46 | 1,593 |

answer@5 counts the case's required quotes in the five results' text. The
rebuilt sites were NumPy and Matplotlib (user guides and gallery under the
cap), Angular (guides before API leaves), angular.dev v18 (its current host's
paths), CrewAI (`/v1.15.35/`, English only) and Transformers (unversioned
addresses); 62 of the 63 gold pages are now in their library's plan (the
63rd is Chart.js 3, which robots.txt keeps out). Ranking costs one BM25 pass
per question over the library's sections, counting only the question's
terms and tokenizing on every CPU: 40 to 170 ms on the largest indexes
(angular.dev's 10,115 sections, the 3,220 long API sections of
v18.angular.dev), against 320 ms on one CPU before; a whole CLI query ran
in 251 ms at the median and 612 ms at p95.

The judge round that decides the default is the next step.

## Amendment 3 (2026-11-04): compact answers within an explicit budget; addresses, releases and names

The fair-judge round 4 (68 cases, blinded) put the whole-section answer level
with Context7 at top 5 (87% against 88% "fully answers") and behind it at an
880-token budget (77% against 86%; 9 wins, 42 ties, 17 losses; how-to
questions worst). The ranking was not the cause: the gold section was in the
top five for 54 of the 63 gold-labelled cases, and a cross-encoder reranker
over the top 20 (ketch-embed `runs/rerank-exp`) moved it by a case or two.
Size was: whole sections are large, so 2.7 to 3.3 of them fit in 880 o200k
tokens where Context7 fits four or five snippets; and `--tokens 880` asked
of the section view returned up to 1,113 tokens, because its budget was
bytes divided by four, which undercounts code.

**Decision.**

- **An explicit budget is fitted compactly** (`--tokens`, MCP `tokens`; the
  default, unset, is unchanged). Tokens are estimated as o200k counts them
  (`estimateTokens`: an approximation of its pre-tokenizer with per-piece
  weights fitted by least squares on 2,651 rendered local results, half the
  libraries held out). Each of the ranked families gets a share of the
  budget weighted by rank, 1/(1+r/1.5) (about 38, 23, 16, 13 and 10% for
  five), water-filled so a family smaller than its share takes its size and
  passes the rest on; a fifth result is shown only when its share is at
  least 90 tokens. A family that fits its share is shown whole. One that
  does not is cut into pieces (sentences, list items, each fenced code block
  with its tab label, tables and quotes whole) of the sections familyParts
  shows (subsections that did not rank are left out) and keeps, within its
  share: its first sentence; the code block naming the question best
  (a block within 75% of its score stands in when it does not fit; else the
  first block), whole, with the sentence leading into it; up to three
  sentences naming the question best; then the rest by score. A piece
  scores the IDF, over all the candidates' pieces, of the query terms it
  names (identifiers the query writes twice, prefix matches at half or a
  quarter, letter-digit parts: `load_in_8bit` names 8 and bit), plus 0.5 for
  a section's first sentence that names any. What the shares leave goes to
  the cut results, best first. The answer is planned to 1/32 under the
  budget and pieces are dropped from the last result until the estimate
  fits. Code is never cut; "…" joins sentences of one paragraph that skip
  some, and one "[…]" ends a result that leaves anything out.
- **One page at two addresses is one result.** Families of `x` and
  `x.html`, or of an `/llms/` mirror and the rendered page, with the same H2
  are collapsed at query time (indexes built before the planner merged
  them hold both: Vue, Vue Router, Mantine), and count as one page for the
  two-families-a-page rule. An API reference leaf (planTier's reference
  tier) gives one family while other pages rank next: its description and
  its signature are not two answers.
- **Builds.** A locale's `x.html` is a copy of the `x` llms.txt lists
  (variantParts sets `.html` aside), so Vue Router's `/zh/` pages are left
  out. An unversioned root whose plan holds two or more numbered release
  trees within two levels of it (each at least 10 pages, together at least
  two thirds of the plan) is re-rooted at the tree its home page links to
  most, else the newest final release: sequelize.org is indexed at
  `/docs/v6/`, which its home page links to, not v7's alpha docs mixed with
  v6 and v4/v5 landing pages.
- **Names.** Secondary names are two tiers: a library's own (its root's path
  segments, a package's or repository's last part) before its host's
  labels, so svelte.dev's `/docs/svelte/` is `svelte` and `/docs/kit/` is
  not; a hyphenated host label also names the library without a language
  word (python-httpx.org is `httpx`); and a name ending in one digit with
  five letters before it that names nothing finds the library by the stem
  (`beautifulsoup4`). These are worked out from stored metadata, so no
  rebuild is needed.

**Not done.** The default budget keeps whole sections: the compact view at
an explicit 1,800 tokens was better within 1,800 (49 against 46 answers, 79
against 75 facts, never over budget against up to 2,276 tokens) but worse
when that output is packed into 880 tokens (35 against 40 answers), so it is
not a clear win at both. A guide-over-reference prior for how-to questions
was not added: the one-family rule for reference leaves fixed the case it
was for (angular.dev's interceptors guide, from rank 7 to 5) at no cost on
the other 62, and a reference-path penalty moved a case or less in
amendment 2. Taking the best-scoring sentence before the code block, all of
a family's sections rather than those that ranked, and a slower rank decay
(five results at 880) each cost two or three facts of 97; five results at 880
cost about three facts against four results. The snippet view's budget still uses
its own estimate (SnippetTokens).

**Measurement** (ketch-embed `scripts/local_retrieval.py`, 63 gold-labelled
dev cases, the library given as its docs root; "in 880" is what fits in 880
o200k tokens of the rendered results by the fair judge's packing rule;
facts are the required-fact quotes, 97 over 63 cases; before is the round-4
binary and index, after this change on a copy with sequelize rebuilt):

| | gold@1/3/5 | gold in 880 | answer@5 | answer in 880 | facts in 880 | results in 880 | tokens returned mean/max | p50/p95 ms |
|---|---|---|---|---|---|---|---|---|
| before, default | 31/49/54 | 43 | 46 | 40 | 66 | 2.70 | 1,593/2,276 | 223/475 |
| before, `--tokens 880` | 31/49/54 | 54 | 30 | 29 | 55 | 4.41 | 875/1,113 | 222/493 |
| after, default | 31/49/55 | 44 | 46 | 40 | 67 | 2.71 | 1,595/2,276 | 221/480 |
| after, `--tokens 880` | 31/49/55 | 55 | 39 | 39 | 66 | 4.41 | 795/877 | 221/472 |

The estimate on the after answers at 880: a result off by 4.2% on average
(95th percentile 9.7%, worst 15% under; bytes/4: 11.4%, 27.7%), an answer by
3.6% (worst 7.7% under); none over 880 by o200k. The default output is the
same for 56 of the 63 cases; the other seven differ only by the fixes
above. The weights were set on these 63 cases, which is the risk this
amendment carries; the judge round at top 5 and 880 tokens decides.

## Amendment 4 (2026-11-04): follow-up fetch, so a compact answer can point at the rest

Round 5 put the compact answer at four or five results in 880 tokens, and
the loss that remained was a how-to question whose one code example the
compaction cut away (Mantine `useForm`'s `validate` rules, Angular's
`computed(() => ...)`, Transformers' `add_generation_prompt=True`). The text
is in the index, so reading it costs no network and no quota; what was
missing was a way to ask for it and a pointer telling the agent to.

**Decision.**

- **`--section <url#anchor>` (MCP `section`) returns one section in full.**
  The reference is the `source` a result already carries (`Result.URL`, now
  also `Result.Section`). No query, no ranking, no model, no network: one
  prefix scan of the page's keys (`Store.PageSections`), tens of
  milliseconds at the largest indexes (8 to 10 ms for the whole process in
  the demo below). The library is optional: the URL names it (a root, or the
  unversioned address a re-rooted release is shown under), and the page's
  sections decide which of several versions holds it; `--library` names it
  explicitly. An anchor on a second-level heading returns its family (the
  heading's text and the subsections under it, as the ranking groups
  them), a deeper anchor that section and what is under it, a page's lead
  itself, and a URL with no anchor the page's sections in order. `x` and
  `x/`, `x.html` are one page. A page or anchor the index lacks is the
  existing `not_found` failure (`ErrSectionNotFound`, exit 3, MCP
  `[not_found]`, a refused Outcome whose message lists the page's anchors);
  a library it lacks stays `not_indexed`; a reference that is no URL is a
  validation error (`invalid_section`, exit 2); `--section` with another
  backend is exit 2. There is no new error path.
- **A cap, cut at a block boundary.** `--tokens` (MCP `tokens`) caps the
  result, default 4,000 (a long guide section, well under a page). Text is
  cut between blocks (paragraphs, lists, a code block whole), never inside a
  code block, planned 1/32 under the cap as compact answers are, and ends
  with `[…] cut at N tokens;` and where to read on: the first section of
  the unit not shown whole, as a command that fetches it, or, when the cut
  is inside the first, how large the section is and to raise the cap. A
  single block larger than the cap is cut with its fence closed.
- **The pointer.** In a compact answer (`--tokens` set, text output; MCP
  `tokens` set) every result that ends in `[…]` ends with one line after
  its `source:` line, `full section: ketch docs -b local --section <ref>`;
  over MCP, appended to the result's `snippet` because that is all an agent
  reads there, `full section: docs(backend="local", section="<ref>")`. A
  result shown whole has none; `--json`, `--minimal` and the default view
  have none. The command carries the reference and `-b local`, not
  `--library`: the URL is enough, so each pointer is about 20 to 30 tokens
  rather than 30 to 40, and a pointer an agent copies verbatim works.
  `Result.Section` (the reference to pass) is on every local result and
  `Result.Truncated` on a result that left text out, in every view, so JSON
  consumers build their own.
- **The budget counts the pointer.** A result that will be cut reserves its
  pointer's cost with the heading path and source line, and a result needs
  `localMinShare` tokens beyond that to be shown: the budget buys a little
  fewer, fuller results rather than thin ones. The estimator undercounts a
  pointer's URL and flags by up to a quarter (o200k splits slugs finely), so
  the pointer is charged a quarter more.

**Measurement** (67 of the 68 round-3 questions answered by a copy of the
round-5 index, `--tokens 880`, real o200k counts of the rendered text):

| | results | tokens mean/max | pointers |
|---|---|---|---|
| before (no pointers) | 4.51 | 791 / 877 | 0 |
| after | 3.93 | 787 / 876 | 2.73 |
| after, the pointer not reserved in `localMinShare` | 4.46 | 786 / 878 | 3.36 |

None is over 880. The pointer costs 0.6 results on average (39 of 67
answers show one fewer); amendment 3 measured that going from five results
to four at 880 costs about three facts of 97, and the larger shares of the
results that remain buy some of it back, but whether a fuller four beats a
thinner fifth with a pointer is for the judge round. The three lost cases,
by their planned roots (rendered answer and follow-up, o200k tokens):

| case | answer | cut results | the pointer to the missing example | follow-up |
|---|---|---|---|---|
| angular-computed-signal | 774 | 3 | `guide/signals#computed-signals` | 582, holds `computed(() => count() * 2)` |
| transformers-apply-chat-template | 782 | 3 | `chat_templating` (a page's lead has no anchor: the page) | 2,622, holds `tokenizer.apply_chat_template(messages, tokenize=True, add_generation_prompt=True` |
| mantine-form-validation-rules | 737 | 3 | `form/validation/#validate-fields-on-change` | 1,002, **does not** hold the rules example |

Mantine's missing example is in a section the answer did not rank at all
(`#validation-with-rules-object`, the page's first); the pointer leads to the
section that did rank. Reading the page (`--section https://mantine.dev/form/validation/`,
the anchor dropped) finds it in the first 1,200 tokens (707 by o200k).

**Not done.** A pointer to the page for every cut result (a page is 2,000
to 3,500 tokens); a pointer to the result's whole family when the best
section is a subsection (the follow-up would then hold the H2's own text, at
the price of more tokens); a "next" pointer on the section a result did not
show; inferring the library from the query for a bare `--section`.

## Amendment 5 (2026-11-04): SQLite FTS5 store, keyword-first, embedding optional and off

Indexing a 735-page site (duo.com/docs, 19,357 section windows) pegged 16
of 18 cores for 13 to 14 minutes: bge-base in pure Go embeds about 20 to 25
windows a second. A cold search-first question cost about 8 s of wall
time and 53 s of CPU, nearly all of it embedding the sections it had just
read. Searching was already cheap; computing vectors was the cost. The rule
for this amendment is that simpler and faster at the same or similar
accuracy beats the model.

**Decision.**

- **One SQLite file, through a pure-Go driver.** The index moves from bbolt
  to `docs-index.sqlite` (modernc.org/sqlite; `CGO_ENABLED=0` still builds
  every release target). Tables: libraries (the metadata as JSON), pages
  (URL, content hash, version, fetch time, source: a build or a search),
  sections (page, order, anchor, heading, breadcrumb, H2 family, text,
  snippet units, cross-version label), vectors (optional), and one FTS5
  table per library, so term statistics are the library's own. The FTS5
  table is contentless: its rows are made by the docs package's tokenizer
  and not stored again. It is still opened per operation (ADR-0006), with
  the write-ahead log so queries read while a build writes. Format 3; the
  bbolt index never shipped and is not read: `docs index` and `doctor` name
  an old `docs-index.db` beside the new file, and a non-SQLite file at
  `KETCH_DOCS_INDEX_PATH` is refused (`ErrNotSQLite`). The page cache is
  unchanged.
- **Tokenization for prose and code.** Each section's row is its heading
  (with the camel-case words of an API entry's name), its breadcrumb above
  it, the parts of the identifiers it writes, and its body, every word
  normalized by the ranking's own normalization (rankTokens: light stemming,
  -ing/-ed/-ize/-ation folding), so the table and a question meet on the
  same terms. The identifier column holds each code identifier's whole name
  and its words: `useForm` is `useform`, `use`, `form`;
  `client_max_body_size` is `clientmaxbodysize` (its words are body terms
  already); `form.validateField` is `form`, `validatefield`, `validate`,
  `field`; `add_generation_prompt=True` is `addgenerationprompt`. A query
  adds the whole names of the identifiers it writes. The normalization is
  versioned (`ftsVersion`): a library whose rows another version made has
  them rebuilt from its stored sections on first use, without the network.
- **Keyword ranking is the default and runs no model.** bm25 over the
  table (heading 3, breadcrumb 1, identifiers 1, body 1) picks 200
  candidates matching any of the question's terms, question filler left
  out ("use", "need", "work", "actually", ...); they are ordered by bm25
  times the web ranker's boosts (the share of the question's terms named,
  a heading or page title that is the question, an API entry the question
  names, the terms in code and side by side), times 0.6 for an API
  reference leaf and 1.2 for a guide (planTier, where the page sits in the
  site), times up to 1.5 for a page title naming the question's terms, and
  times up to 2 for a page whose candidates' scores sum high (a page about
  the question over a stray section elsewhere). Only the candidates' pages
  are read, every section of them, so families and the existing rendering
  (one result per H2, page dedupe, compact answers, pointers, `--section`)
  are unchanged. Without hybrid ranking keyword ranking is not a fallback,
  so it carries no `keyword_ranking` reason.
- **Embedding is optional and off** (`docs_hybrid`, default false). With it
  on and a model installed, builds embed every section, `-b local` fuses
  cosine with the keyword ranking by reciprocal rank fusion, search-first
  embeds its 96-window budget and checks stored sections' cosine before
  searching, and `--reembed` works; a library without usable vectors is
  ranked by keywords with reason `keyword_ranking`. Without it, no command
  loads the model.
- **Search-first remembers questions by their terms.** A question whose
  normalized terms overlap a remembered one's by at least 0.8 (Jaccard), for
  the same library value, is answered from the library that answered it,
  with no search; hybrid keeps the query-vector cosine (0.92). The other
  local check, a stored section within cosine 0.80 of the question, has no
  keyword equivalent that can say a partly read site answers: without the
  model, a new question about a search-built library is searched again.
- **Determinism.** Pages are written in URL order and full-text ties break
  by row id (pages fetched concurrently arrive in any order), and compact
  answers sum a piece's term weights in term order (a float sum in map
  order differed in its last bits between runs, flipping ties): the same
  question now gives the same answer every run, which the measurements
  below needed.

**Driver.** Both pure-Go SQLites ship SQLite 3.54 with FTS5. modernc.org/sqlite
compiles FTS5 in and adds 3.9 MB to the stripped binary;
github.com/ncruces/go-sqlite3 (wasm2go) needs its FTS5 module registered per
connection and adds 5.8 MB. On angular.dev's 10,115 sections both inserted
in about 0.1 s and answered a 200-row bm25 query in under a millisecond
(ncruces about a quarter faster: 0.65 against 0.85 ms). The difference is
below anything a query spends elsewhere; the smaller binary and the
simpler setup decide for modernc.

**Measurement** (dev sets only. Library set: the 63 gold-labelled cases,
`scripts/local_retrieval.py` with each case's planned root; the 44 round-5
libraries rebuilt from a copy of the page cache, and the hybrid index the
same store with round 5's vectors copied in for every section whose text
is unchanged and the rest embedded. General set: the 60 dev questions plus 6
negatives through `-b search`, `scripts/search_first_eval.py`, replaying the
prototype's recorded cold runs, so every mode sees the same search results
and pages; replay leaves the network out of wall times.)

| library set, fair matcher | page@5 | section@1/3/5 | answer@5 | answer in 880 | facts in 880 | query p50/p95 |
|---|---|---|---|---|---|---|
| round 5 (bbolt, hybrid) | 60 | 31/49/55 | 46 | 40 | 67 | 221/480 ms |
| FTS5, keywords only | 60 | 38/48/51 | 46 | 43 | 68 | 32/65 ms |
| FTS5, hybrid | 60 | 37/51/55 | 47 | 42 | 70 | 202/348 ms |

| general set, search-first, cold | site | page@5 | section@1/3/5 | answer@5 | wall p50/p95 | CPU p50/p95 | RSS p50 |
|---|---|---|---|---|---|---|---|
| prototype (bbolt, hybrid) | 51 | 36 | 15/27/31 | 19 | 4.54/6.27 s | 52.7/74.6 s | 1,168 MB |
| FTS5, keywords only | 51 | 35 | 16/26/29 | 19 | 0.28/1.14 s | 0.31/1.42 s | 62 MB |
| FTS5, hybrid | 51 | 37 | 18/28/30 | 23 | 4.75/8.10 s | 53.0/75.3 s | 1,183 MB |

The prototype's live cold runs took 7.96 s at the median; the 3.4 s over
its replay is network time every mode pays alike.

| duo.com/docs (733 pages, 13,871 sections), `docs index` from the page cache | wall | CPU (user+sys) | peak RSS | index file |
|---|---|---|---|---|
| FTS5, keywords only | 14–19 s (waiting on a few live fetches) | 2.4 s | 188 MB | 28 MB |
| FTS5, hybrid (19,357 windows embedded) | 779 s | 8,979 s | 1,635 MB | 67 MB |

| a repeated search-first question (all 66, over the cold run's index) | answered locally | same results | wall p50/p95 |
|---|---|---|---|
| FTS5, keywords only (term-overlap memory) | 61/66 (the other 5 were refusals, not remembered) | 66/66 | 0.02/0.05 s |
| FTS5, hybrid (query-vector memory) | 61/66 | 66/66 | 0.20/0.25 s |

The term-overlap memory never fired falsely: the 66 distinct questions run
in order over one shared index all went to search.

The keyword ranker's steps, each measured on both sets (section@5 and
answer@5, library / general):

| step | library | general |
|---|---|---|
| bm25 + the web ranker's boosts (start) | 46, 43 | 28, 17 |
| + guide 1.2 / reference leaf 0.6 | 49, 45 | 28, 17 |
| + page title coverage | 49, 45 (section@1 27 → 32) | 28, 17 |
| + page-level aggregate | 49, 45 (section@1 32 → 38) | 28, 19 |
| + question filler dropped | 51, 46 | 28, 19 |
| page aggregate at 1.0 instead of 0.6 (adopted) | 51, 46 | 29, 19 |

Tried and not used (each within a case of the step before, or worse on one
set): column weights (heading 1 or 5, breadcrumb 0.5 or 2, identifiers 0.5
or 2: ±1); candidate depth 50 or 500 and the relevance cut (no change);
the identifier column off (no change on these sets; kept for identifier
queries, below); FTS5's Porter stemmer over the raw text instead of the
ranking's normalization (library 50/44, general 28/19); prefix matching
(-1/-1); NEAR phrase clauses (-0/-1 and fewer facts in 880); dropping the
library's own name from the question (-1 section or answer); a length prior either
way (-11 and -8 sections); a penalty for API entries the question does not
name (-3); pseudo-relevance feedback (+1 section, -2 to -3 answers); family-
instead of page-level aggregation (-3 at 0.6); llms.txt descriptions
indexed with each page's lead section (no change) or every section (-1); a
prior for the pages the web search ranked (general: section@1 +3 to +4,
section@5 +0 to +1, answer@5 -1).

The identifier column is kept although these sets do not move with it: no
dev question writes an identifier whose words alone miss it. A unit test
holds `validateField`, `validate field` and `form.validateField` to the same
section.

**Where keywords lose to hybrid.** Library set, gold section in the top 5
with hybrid but not keywords: crewai-tools ("give an agent custom tools"),
chartjs-tree-shaking ("reduce bundle size by importing only the components
I use": the gold section says "tree-shaking"; keywords find the tree-shaking
section of another page, and still answer), ai-sdk-usechat, poetry-18-
dependencies ("declare dependencies": the gold section says "specifying"),
poetry-install-without-group ("without dev group": the section is
"installing group dependencies" with `--without`), textual-work-decorator
("run a slow request in the background without freezing the UI": the
section is about workers). Keywords win sveltekit-form-actions and
instructor-stream-partial back. General set, answered by hybrid only:
okta-rate-limit-429-reset, aws-iam-explicit-deny-overrides-allow ("deny
overrides allow" against "explicit deny ... evaluation logic"),
aws-s3-bucket-naming-rules, cloudflare-tunnel-create-locally-managed,
kubernetes-liveness-vs-readiness-probe ("k8s"), postgresql-wal-level-
logical-replication, tokio-mutexguard-not-send-across-await; by keywords
only: aws-lambda-max-timeout-and-env-var-size, kubernetes-pod-disruption-
budget, android-play-target-sdk-requirement-api-36. Most hybrid-only wins are
paraphrases: the question and the section name the same thing in other
words.

**Consequences.** A build costs what fetching costs; the 44 round-5
libraries (10,680 pages, 77,355 sections) rebuild from the page cache in
about 2.5 minutes and 40 CPU-seconds, into 233 MB where round 5's bbolt index
with vectors was 1.2 GB. A local query is 32 ms at the median, a cold
search-first question 0.3 s of CPU, and nothing loads a 438 MB model. The
binary grows 4.2 MB (23.7 to 27.8 MB stripped). Keywords alone lose four
gold sections in the top five on the library set and four answers on the
general set to hybrid ranking, all paraphrases; they equal or beat it at
the top (section@1 38 against 37) and within 880 tokens (43 answers against
42). The weights were set on these dev sets, which is this amendment's
risk; the sealed holdouts decide.

## Amendment 6 (2026-11-04): search-first site picking, hidden code, legacy and troubleshooting priors, stricter memory

A hands-on bench (an agent asking eight questions through `-b search` with
a fresh index) found two classes of failure. Site picking: a question about
Duo's Admin API was answered from third-party "integrate Duo with X" pages
although six of ten hits were on duo.com, because help.duo.com and
duo.com/docs were one site whose help tree (JS shells, no browser) was read
first and gave nothing, and the next candidate was another domain; GitHub
Actions came from the enterprise-server@3.22 copy of the docs; PKCE from a
vendor rather than RFC 7636; `python requests ... backoff` was refused
`ambiguous_site`. Dropped code: AWS's JSON lifecycle examples were missing.
The rule stays: simple general rules, no model, no CGO, no site names in
code (category lists such as nonDocsDomains are fine).

**Decision.**

- **The site a question owns goes first.** A domain whose first label (or
  that label without its language word) is a word of the question owns it:
  duo.com for "duo", requests.readthedocs.io for "requests",
  python-poetry.org for "poetry". When one is among the candidates with at
  least a quarter of the leader's weight (sfOwnerShare), it goes first
  (ownerFirst); when the question owns none, a standards body's site does
  (specDomains: rfc-editor.org, ietf.org, w3.org, whatwg.org, openid.net,
  ecma-international.org, tc39.es, unicode.org, oasis-open.org, iso.org),
  and a spec site counts as docs. An owner or spec site is never an
  `ambiguous_site` refusal.
- **A domain's docs trees are candidates in turn.** Each tree (host and
  docs path) of a domain is kept, best first, as the site's alternatives;
  a tree that gives nothing (no page readable, robots-closed, nothing
  matching) is followed by the domain's next tree before another domain
  (sfMaxSites 2 domains, sfMaxReads 4 trees in all, within the 45 s
  budget). A tree whose top hit cannot be read is tried last. An unrendered
  JS shell with fewer than 50 words is no page (unreadShell), so a help
  centre of shells reads as nothing instead of as "Loading". The hit pages
  of the tree are still read first (plan).
- **The main docs over a variant.** treeBetter ranks a tree whose path
  marks an edition, deployment or age (variantSegment: enterprise, legacy,
  archive(d), deprecated, obsolete, old, previous, self-hosted,
  self-managed, on-prem, govcloud, or a name pinned to a release such as
  enterprise-server@3.22) below the main docs unless the question names
  the word; when a domain's hits are all in a variant, its main docs'
  copies of them (the variant segment removed) are tried first, and the
  variant is the alternative (mainTree).
- **A top hit served from another domain** (terraform.io redirecting to
  developer.hashicorp.com) takes along the hits of the candidates whose
  hits all sit under the served tree.
- **Hidden code is content when the page shows it nowhere else** (extract).
  A hidden block (display:none, `hidden`, aria-hidden) holding a code block
  whose text no other code block on the page has is kept: tabbed samples
  (AWS's `<awsdocs-tabs>` XML/JSON pairs in `<dl style="display: none">`,
  one language of several). An ARIA tab's label is moved onto its panel as
  a bold line and the strip of labels dropped. A copy button's empty line
  before the code is trimmed. On the scrape bench's 500 pages no check
  changes and 16 pages' text does (tab labels, hidden code, blank lines).
- **Ranking priors** (keyword ranker): a legacy page the question does not
  ask about weighs 0.6: its path or title says legacy, deprecated,
  archived or a variant word, or its last path segment is a numbered
  edition (adminapi-v1) beside the unnumbered page or a higher edition
  among the candidates. A question describing a failure (error, fails,
  keeps, crash, not working, ...) weighs sections whose path or headings
  say troubleshooting, debugging, errors or problems 1.2. Sections under a
  docs platform's boilerplate heading (agent instructions, querying this
  documentation, was this page helpful, edit this page, cookie notices)
  are dropped when pages are sectioned and skipped when ranked. The
  phrase boost's token sequence leaves identifiers' parts out, so one
  identifier whose parts spell the question (adminapi_read_log) is not
  the question's words side by side.
- **Question memory is stricter.** A remembered question answers a new one
  only when their terms after filler are the same set (sfAskedOverlap 1),
  and only answers from the first site read are remembered (any of its
  docs trees; never a fallback domain's).

**Measurement** (dev sets only). Library set: 63 gold cases,
`scripts/local_retrieval.py` over a copy of amendment 5's index (the
extraction change does not reach a built index). General set: 60 dev
questions and 6 negatives replayed with `scripts/search_first_eval.py`.
Because the site changes read pages an older recording lacks, two live
cold runs were recorded back to back (the baseline binary, then the new
one, keyless auto chain, 6 s pacing) and merged: the new run's exchanges
(its search results) plus the baseline run's for every request the new
run did not make. Both binaries replay that union on the same search
results. Rows are cumulative.

| library set | page@5 | section@1/3/5 | answer@5 | answer in 880 | facts in 880 |
|---|---|---|---|---|---|
| amendment 5 | 60 | 38/48/51 | 46 | 43 | 68 |
| + this amendment | 60 | 38/48/52 | 46 | 43 | 68 |

| general set, union recording | site | page@5 | section@1/3/5 | answer@5 | refused | negatives refused |
|---|---|---|---|---|---|---|
| amendment 5 | 48 | 35 | 16/27/30 | 17 | 8 (6 ambiguous_site) | 1 |
| + hidden code | 48 | 36 | 16/27/31 | 18 | 8 | 1 |
| + site picking | 52 | 40 | 17/28/32 | 18 | 3 (2 ambiguous_site) | 0 |
| + ranking priors | 52 | 40 | 18/28/32 | 18 | 3 | 0 |

On amendment 5's own recording (cold7) the new binary gives site 54
against 51, page@5 36 against 35, section@1/5 17/30 against 16/29, answer@5
19 against 19; its four refusals there are pages the old recording never
fetched. The negative no longer refused (a GitHub Actions option that does
not exist) was refused only because its sites tied. Live, the two cold runs
measured site 52/47, page@5 40/31, section@5 32/23, answer@5 18/17 (new/
old), on search results that differed between them.

Cost: cold CPU p50/p95 0.31/1.18 s on the union replay (0.30/1.18 before),
live 0.44/1.33 s (0.41/1.17); live wall time p50/p95 2.1/5.6 s, all
network. A repeated question (each case asked again over its cold run's
index): 62 of 66 answered from the index with the same results (the
other four are three refusals and one answer from a fallback domain, not
remembered by design; amendment 5's rule answered 61); the 66 distinct
questions over one shared index never answered from memory.

Tried and not used: identifier-part matches counting half toward a
section's coverage (library section@1 -2, answer in 880 -1; at two or more
such terms only, -1/-1); a boost for a section whose second-level heading
is made of the question's words, not its page title's ("Logs" for "read
logs from the duo api": library section@1 -3 to -4 at 1.5 and 2, general
answer@5 +1 to +2; 1.2 and 1.3 do not move the Duo case); a heading-path
coverage boost (no effect on the Duo case, library mixed).

**Consequences.** The Duo question is answered from duo.com/docs, with the
legacy v1 pages behind the current ones; the Admin API's Logs family is
still tenth on the whole duo.com/docs index (Integrations, whose
permission list says "read Logs information", stays first). The GitHub
YAML was never lost by extraction: the examples are H3 sections under the
`paths` H2, and a compact answer leaves them behind its `full section:`
pointer. Owner and spec preferences can pick a third-party site that is
named like the product (k8s.info for "k8s") as the vote did before them,
and a spec site's landing page (rfc-editor.org/info/rfc7636) rather than
the RFC text when that is the hit. Several bench misses are the search
results themselves (no kubernetes.io or requests docs page among the hits
when the providers throttle).

## Amendment 7 (2026-11-04): no model

Amendment 5 made embedding optional and off (`docs_hybrid`). This amendment
removes it. The measurements that decide it are amendment 5's, taken side by
side on the same index, recordings and pages:

- **Accuracy is a wash.** Library set: keywords alone 38/48/51 at
  section@1/3/5, 46 answers in the top five and 43 within 880 tokens;
  hybrid 37/51/55, 47 and 42. Keywords win at the top and within the
  budget an agent reads; hybrid finds four more gold sections lower in the
  top five, all paraphrases. General set (search-first, cold): section@5
  29 against 30, answer@5 19 against 23, the hybrid-only wins again
  paraphrases. Amendment 6 then moved keywords to section@5 52 without the
  model.
- **The cost is not.** A local query: 32 against 202 ms at the median. A cold
  search-first question: 0.31 against 53.0 CPU-seconds at the median (about
  1/170th), 62 against 1,183 MB resident. Indexing duo.com/docs: 2.4 against
  8,979 CPU-seconds, 28 against 67 MB on disk. A repeated question: 0.02
  against 0.20 s, answered locally as often (61/66) with term overlap as with
  query-vector cosine.

Off by default, the model still cost what an option costs: a dependency
(github.com/rostamlabs/rembed), a 438 MB download with pinned hashes, a
command, two flags and two config keys, a vector table, and a second ranking
path through `-b local`, `docs index` and the search-first backend that every
ranking change had to keep working and measuring. The rule amendment 5 set
(simpler and faster at the same or similar accuracy beats the model) now
applies to the option too.

**Decision.** The local index has one ranking, keywords (amendment 5's, with
amendment 6's priors). Removed:

- `internal/embed` and the rembed dependency; `ketch model install` and
  `ketch model status`; the `model_path` config key (`KETCH_MODEL_PATH`);
  doctor's `model` row. Nothing is downloaded. Installed model files in
  ketch's cache directory are left where they are; nothing reads them.
- `docs_hybrid` and every path behind it: window embedding at build time,
  `docs index --reembed` and `--no-model`, cosine ranking and its fusion with
  keywords, the `keyword_ranking` reason (it only said that hybrid ranking
  could not use its model), the vectors a search-first question embedded
  (`sfEmbedWindows`), the query vector a remembered question kept, and the
  search-first check that a stored section was within cosine 0.80 of the
  question (`sfLocalFloor`, `KETCH_DOCS_LOCAL_FLOOR`). As amendment 5 said,
  that check has no keyword equivalent: a new question about a library a
  search built is searched again. Question memory stays, by terms
  (amendment 6).
- Snippet units and the `--snippets` view (MCP `snippets: true`, amendment
  1): its ranking leaned on section cosine, and amendments 3 and 4 serve its
  purpose (compact answers, four or five results at 880 tokens, and
  `--section` for the rest). A build no longer cuts units; the summary no
  longer counts them.
- The `docs.FTS5Local` stub, which never answered.

Kept: the SQLite FTS5 store and its tokenizer, the keyword ranker and its
boosts and priors, families, compact rendering, `--section` and its
pointers, search-first site picking and question memory, version purity,
cross-version labels and coverage planning.

**Index format.** Still 3: no layout a query reads changed. A new file has no
vectors table and no units columns; a file an earlier format-3 build wrote
keeps them, unread, and is read and written as it is (the store names its
columns, and the old ones have defaults). Library metadata loses its model,
dimensions and vector and unit counts; old records' fields are ignored.

**Measurement.** Library set (63 gold cases, `scripts/local_retrieval.py`,
dev cases only) over two copies of amendment 5's index, the build before
this amendment and the build after: page@5 60, section@1/3/5 38/48/52,
answer@5 46, answer in 880 43, facts in 880 68 for both, every case's
results identical. The stripped binary goes from 27.9 to 27.3 MB (rembed is
pure Go and small; the cost was never the binary).

**Consequences.** One ranking to tune and measure, and no setup beyond
`docs index`. Paraphrase questions whose words the docs do not use stay the
known weakness (amendment 5 lists them); a fix for them has to be cheap
enough to run by default, or it stays out.
