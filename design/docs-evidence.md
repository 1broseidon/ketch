# Evidence pipeline

Status: agreed on 2026-11-10, Storage included.
Branch: `feat/docs-evidence`, from `release/v0.21.0` (489973c). Nothing here ships until the gate in QA passes.

## Goal

One `ketch docs` call at the default budget ends one of two ways:

1. the caller can quote the answer from the output, or
2. the output says the docs do not cover it, and names the question's terms that have no hits.

Anything else is a failure, however relevant the sections look. The measure is the path: calls to the answer.

One call is the target, not a promise. The caller is the smart part: when the question's words are not the docs' words, the first call must give it what it needs to make the second call exact (the Contents line, `not_found`), and that counts as doing the job. No model is added to close that gap (decided 2026-11-10).

A call answers from one source. A caller who gets the right lines in one call can spend three calls on three sources (Duo's docs, linux-pam's, SSSD's) and combine them. So every call must be self-contained, cheap to read, and clear about what this source does not say.

## Evidence

The case that set the goal, run on 2026-11-10 against the `release/v0.21.0` build. Question: telling a user's failed login from Duo being unreachable, with `pam_duo`.

| Fact | Value |
|---|---|
| Calls it took to reach the answer | about 18 |
| The answer | the `failmode` row of the options table, `https://duo.com/docs/duounix#duo-configuration-options` |
| Was its section a candidate? | yes, in the family ranked 5th of 6 |
| Was the row printed at the default budget? | no |
| Budget needed to print it | about 40,000 tokens |
| What took the budget | 14 distro subsections of one family, each repeating one SELinux sentence |
| `pam_sss` in the Duo docs | 0 of 13,883 sections |
| `PAM_SERVICE_ERR` in the Duo docs | 0 of 13,883 sections |
| `failmode` in the Duo docs | 74 of 13,883 sections |
| Time, cold through web search | 4.3 s and 7.3 s |
| Time, indexed | about 0.15 s |

Two causes in the code, both in the rank stage:

- The budget is shared among families by family rank (`compactShares`, `rankWeight`: 38, 23, 16, 13 and 10%). A piece is only as visible as its family's rank.
- A table is one piece, shown whole or not at all (`pieceBlock`). A 17-row options table never fits a share, so its rows are never shown.

The step 1 baseline (see QA) found three more cuts that lose a fact before any share is given:

- a relevance cut: a section scoring under 0.3 of the best never becomes a ranked member of its family, so it is never split into pieces (the options section scored 0.2997);
- a limit of five families;
- a cap of two families per page while other pages rank next (`localFamiliesPerPage`).

Nothing told the caller that two of the question's terms appear nowhere in the source. That was half the real answer.

## Decisions

- Nothing ships until the gate passes (decided 2026-11-10).
- Pieces are made in memory at query time. No schema change, no reindex.
- No model. FTS5 and plain rules, as in ADR-0007 amendment 7.
- One pipeline. A question answered from an index and one answered from pages just read differ only in Gather.
- One view. The section view (no budget) and the compact view (a budget) become one output, at a default budget of 1,800 tokens. `--section` still returns a section whole.
- Rules are general. No rule names a site, a product or a layout seen in one case.

## Stages

Each stage has a contract and a check. A QA case names a fact; the checks say which stage lost it.

### Gather

Collect widely and cheaply. Precision is Rank's job.

- **Terms.** The question's terms as `queryTerms` and `queryIdents` give them today.
- **Pool.** The union of: the best sections for the whole question (as today, 40), and for each term on its own, the best 20 sections that contain it. Capped at 200 sections. Every section in the pool is split into pieces: there is no relevance cut, no family limit and no per-page cap. Those three cuts and the family shares are removed, not tuned.
- **One term list.** Gather, Rank and the header use the same terms, filler words dropped. Today ranking drops filler and piece scoring keeps it.
- **Counts.** For each term, the number of the library's sections that contain it, and the library's section count. These feed Rank's term weights and the header.
- **Source.** An indexed library, or the pages a search just read, held as a library in memory. Everything after is the same code.

Check: the fact's section is in the pool.

### Sort

Turn sections into clean, labelled, deduplicated pieces.

- **Split.** A piece is a paragraph (its sentences only when it runs past about 120 tokens), a list item, a code block, or a table row. A table is never one piece: each body row is a piece and carries its header row as context. Each piece keeps its section, heading path, anchor, page and order.
- **Label.** Kind, from structure only:

  | Kind | Rule |
  |---|---|
  | option row | a table row whose first cell is an identifier or code |
  | row | any other table row |
  | step | an item of an ordered list |
  | item | an item of an unordered list |
  | example | a code block |
  | note | an admonition or a line opening with Note, Warning or Important |
  | prose | a sentence |

  Page role, from what the planner already knows (`planTier`, and amendment 6's legacy and troubleshooting priors): reference, guide, FAQ, troubleshooting, release notes.
- **Clean.** Remove leaked HTML tags and keep their text (`<span class="toreplace">X</span>` is `X`; `<br></br>` is a line break), lines that are only badge or tracking images, and Markdown escapes that change nothing outside code (`pam\_duo` is `pam_duo`). Otherwise text stays verbatim. Cleaning is one pure function applied as sections enter the pool, so an index built earlier is cleaned too.
- **Fold.** Pieces with the same text after whitespace and case are folded into one: the copy from the best page role, then the earliest. The folded piece lists where the others sit. Exact matches only in this round.

Check: the fact is one piece's text, intact.

### Rank

Score pieces, then choose under the budget so that each choice adds something.

**Score.** For a piece:

- each query term it contains counts by its rarity in the library (from Gather's counts, not from the candidates), an identifier the question writes twice as much, as today;
- a term found only in the piece's heading path counts at half: the `failmode` row does not say `pam_duo`, its page title does;
- a term in the piece's defining position counts half as much again: the first cell of a table row, the piece's own heading, a definition term;
- the page role scales the sum: reference and guide 1.0, FAQ 0.9, troubleshooting 0.8, release notes 0.6 unless the question asks about a version or a change.

The four numbers above are starting values. Each is tuned on the open QA cases only, and each rule's effect is reported on its own.

- the piece's section scales it too: by 1 plus the section's whole-question score from Gather, relative to the best section's (0 to 1). A piece in a section that is about the question outranks the same words in one that is not.

**Choose.** Repeatedly take the piece with the best score for its cost, where:

- a term counts in full the first time a chosen piece covers it, half the second time, and nothing after that;
- a piece's cost includes the context it brings: its section's heading line if it is the first from that section, its table's header row if it is the first row of that table;
- a paragraph that leads into a code block and that code block travel together, whichever is chosen;
- a folded piece is chosen once.

**Stop.** The budget is a ceiling, not a target. Choosing stops at the budget, or when the best remaining piece is worth less than a quarter of the first choice. A short answer that is all evidence beats a full budget.

**Merge.** Chosen pieces of one section are printed together in page order under one heading. Neighbours join; `…` marks a gap. Rows of one table share one header. A section whose chosen pieces are most of it is printed whole. Groups are ordered by their best piece.

**Contents.** A group that is cut says what it left out, by name, in one line of at most about 40 tokens: the keys of an option table's other rows, and the headings of the section's direct subsections, with the keys of an option table one level down. When the question's words are not the docs' words ("cannot be reached" against "service errors"), this line is what makes the next call exact.

Round 3 showed that small standalone units lose the prose that explains them. Paragraph-sized prose, the pairing of prose and code, the section factor, Merge and the whole-section rule are the answer to that: pieces are ranked alone and never printed alone.

### Round 2

The first build of Sort and Rank (2026-11-10) moved sort from 2 to 5 of 5 and rank from 2 to 3 of 5, and showed where this section was wrong. It first said "stop at the budget" and ranked pieces on their own words alone.

| Finding | Evidence | Rule above |
|---|---|---|
| The budget was always filled | 9 to 20% of the Duo answers and 45% of one Click answer was filler; the same Click answer went from 952 to 6,512 bytes | Stop; a term counts twice, then nothing |
| Prose was shredded | sentence pieces, `…` between most of them, "After:" printed without "Before:" | paragraph pieces; prose and code travel together |
| A how-to fact was the 23rd choice | "place `pam_duo` directly after `pam_unix`", in the section Gather ranked 2nd | the section factor |
| Prose that answers lost its example | a chosen paragraph did not bring the code after it | prose and code travel together |
| A question in other words finds nothing to quote | duo-fallback: the rows name none of `fallback`, `cannot`, `reach` | Contents |
| `duosecurity.com` made `com` an identifier | doubled weight on `com` | a host name is one term |

Kept from that build: heading-path terms count once per group; exact term matching; rarity from the full-text index's own inverse document frequency; a budget too small for any piece still gets the best one.

### Round 3

Round 2 (2026-11-10) cut the mean answer from 1,395 tokens to 585 and led duo-fallback, but answered 5 of 7 where round 1 answered 6, and it needed two rules this section did not have to get there. The cause is one design choice, not the values: pieces were taken by gain per token, each term discounted as it was covered.

| Finding | Evidence |
|---|---|
| Cheap pieces that name one rare term go first | "2. Build and install `duo_unix` with PAM support (`pam_duo`)." is in three of four pasted answers |
| Covering a term is not answering the question | duo-pam-order: the fact was the 4th best piece of 902, and three cheaper pieces had already "covered" `pam` and `pam_duo` |
| The stop was measured on discounted gain | the same fact fell under the floor only after the discount |
| A heading made every piece under it a definition | every piece under "Install pam_duo" counted as defining `pam_duo` |
| The section factor did nothing | the top 12 sections scored within 0.91 to 1.0 of the best |
| The answer came last | the `failmode` row printed 504 tokens in |

So Choose is replaced. Pieces are taken best first, and a piece is good when it matches much of the question at once.

**Score**, replacing the rules above where they differ:

- **Together.** The sum of a piece's matched term weights is multiplied by the share of the question's distinctive weight that the piece matches, heading path included. A piece naming five of six terms is worth far more than five pieces naming one each.
- **Definitions.** A term is in a defining position only in the first cell of a table row, in a definition-list term, or in the first piece under a heading that names it. A heading does not make the rest of its section a definition.
- **Section, by rank.** The section signal is rank fusion, not a factor on relative scores: a piece's final order comes from `1/(k + its own rank) + 1/(k + its section's rank in Gather)`, with `k` 10 to start. Bunched scores no longer hide a section that Gather ranks 2nd.

**Choose.** Take pieces in that order. There is no gain per token and no discount on order.

- A piece is skipped when every term it matches has already been matched by two chosen pieces: it repeats.
- A unit (a paragraph with its code) and context costs are as before.

**Stop.** At the budget, or when a piece's score, before fusion, falls under half the best piece's score. When many pieces are close to the best, the question has no sharp match and the answer runs longer; when one or two stand out, it is short.

**Order.** Groups print by their best piece, as before; with definitions and Together, a defining row no longer sits behind a mention.

**Measure.** Eight cases cannot arbitrate between rules. From this round on, every rule is also reported on the broad open set (see QA).

### Round 4

Round 3 was built as written and lost. On the broad set (77 cases with facts):

| Build | Answered | Mean tokens printed |
|---|---|---|
| old | 42 | 1,619 |
| round 1 (pieces by summed weight, budget filled) | 40 | 1,578 |
| round 2 (gain per token, coverage discount) | 28 | 897 |
| round 3 (together, fusion, skip repeats, stop at half) | 24 | 726 |
| round 3 without the skip rule | 36 | 1,302 |

Three designs written from eight cases all lost to the code they replace. What the numbers do say:

- **Answers track tokens.** Every rule that shortened answers by counting covered terms lost answers. A question has three to six terms and every relevant piece holds the same ones, so "this term is covered" says nothing about content.
- **Old and round 1 answer different cases.** Old answers 14 that round 1 misses, and round 1 answers 12 that old misses. Either one answers 54 of 77.
- **There are two kinds of question.** A how-to is answered by the body of the section that is about it, and that body rarely repeats the question's words: ranking whole sections finds it and ranking pieces by matched terms does not. A lookup is answered by one row or item of a long reference section, which ranks low as a whole (the options section was 35th) and never fits: ranking pieces finds it and ranking sections does not.

Withdrawn: gain per token, coverage discounts, the skip rule, Together as a requirement, stopping at a fixed share of the best score, and brevity as a goal in itself. A short answer is good only when nothing was lost to get it.

**Direction.** Sections are chosen; pieces score long sections and cut them.

- **Order.** Sections, by fusing each section's rank in Gather with the rank of its best piece. A long reference section gets credit for its best row.
- **Print.** In that order, a section is printed whole when it fits its cap and the budget left. One that does not fit is cut around its best pieces: its opening, the best pieces with what travels with them (a paragraph and its code, a row and its header), in page order, and a Contents line for the rest.
- **Fold.** Sections with the same text are one group, as pieces already are.
- **Stop.** The budget. Any earlier stop has to earn its place under the protocol below.

**Protocol.** This round is found by measurement, not written ahead.

- **Objective, in order:** cases answered or led; then tokens before the first fact; then tokens printed. A change that loses answers to save tokens is rejected.
- **Two halves.** The broad set is split by site into a tune half and a confirm half. Rules and values are chosen on tune. Confirm is run once per candidate build and never iterated on.
- **Every rule earns its place:** it stays only if removing it loses on tune and does not gain on confirm. On a tie, fewer rules win.
- **Floor:** the build must beat the old build on both halves before Output starts.
- **General rules only**, as before: structure, never a site, a product or a layout.

**Result**, 2026-11-10. The floor is met. With the runner made fair the set is 95 cases (90 with facts), 48 tune and 47 confirm, plus the Duo and Click cases on the tune side.

| Half | Build | Answered | Median tokens before the first fact | Mean tokens printed |
|---|---|---|---|---|
| tune, with Duo and Click | old | 31 of 53 | 202 | 1,598 |
| tune, with Duo and Click | round 4 | 39 of 53, 1 led | 259 | 1,720 |
| confirm | old | 25 of 44 | 208 | 1,624 |
| confirm | round 4 | 31 of 44 | 103 | 1,712 |

Confirm was run once: 6 gained, none lost. Over the whole set round 4 answers 17 cases the old build misses and loses 2.

What Rank is now, each rule with what removing it costs on tune:

- **Score.** A piece's score is the summed rarity of the question's terms it holds, a term counting once for the piece's text and once more for its heading path; an identifier the question writes counts double; the page role scales the sum. Counting the heading path at half instead: 3 answers lost.
- **Fusion.** Pieces are ordered by `1/(10 + piece rank) + 1/(10 + section rank in Gather)`, tied scores sharing a rank. Without the section rank: 6 lost. Without the piece rank: 7 lost.
- **Walk.** Pieces are taken in that order. The first piece of a section opens it: the section prints whole if it fits the budget left, otherwise it is cut to that piece, and later pieces of it join when they fit. What does not fit is passed over, not stopped at. Without whole sections: 3 lost.
- **No cap** per section: every cap tried lost answers.
- **Sort.** A quote is its paragraphs; a block of one entry per line is its lines.

Dropped because removing them changed nothing or gained: Together, the defining-position weight, a cut section's opening, prose and code travelling together, folding same-text sections, any stop before the budget.

Still wrong, and next:

- **Near copies.** duo-failmode is still not answered: three distro sections print whole, about 1,650 tokens of near-identical text, and the options row is passed over for budget. Folding catches identical pieces, and whole sections walk straight past it. Rule to measure: a section most of whose pieces are copies of text already printed is not printed again; it is named on the group it copies.
- **The budget is always filled.** Mean 1,716 tokens. Every earlier stop lost answers, so none is adopted; the near-copy rule and the lighter group layout in Output are the honest ways to cut noise.
- **Markup residue.** MDX component tags and comments are printed (`<Sandpack>`, `</DeepDive>`, `{/*parameters*/}`).

Check: the fact is among the chosen pieces.

### Output

Frontmatter carries the verdict. The body carries the evidence, verbatim. An answer from pages a search read prints `source: search, N pages read` in place of `index:`, then `origin:` (Build 1); its status is judged like an indexed answer's.

```markdown
---
query: pam_duo failmode safe secure when Duo service is unreachable
backend: search
library: https://duo.com/docs/
index: full, 734 pages, built 2026-11-10
status: answered
terms: {failmode: 74, pam_duo: N, unreachable: N}
not_found: []
---
### Duo Unix (pam_duo) > Duo Configuration > Duo Configuration Options
option rows, 2 of 17 · source: https://duo.com/docs/duounix#duo-configuration-options

| Key | Required? | Description |
|---|---|---|
| `failmode` | Optional | On service or configuration errors that prevent Duo authentication, fail "safe" (allow access) or "secure" (deny access). The default is "safe". |
| `https_timeout` | Optional | … |

full section: ketch docs --section https://duo.com/docs/duounix#duo-configuration-options
```

`N` stands for counts not measured yet; the rest of the block is real text from the page.

Refined on 2026-11-10, after Rank settled; where this differs from the example above, this holds:

- **One title per page.** Groups of one page print together under `## <page title>`, pages in the order of their best group, groups in rank order within a page. A group's heading is its path below the page title. The page title is no longer repeated on every group.
- **One source line.** `source: <url#anchor>`, with ` (cut)` when the group was cut. The command for the rest is said once, on a last line of the answer, not on every group.
- **Terms as written.** `terms` and `not_found` print each term as the question wrote it (`pam_sss`, not its stems or parts), once.
- **Status.** `refused` when no distinctive term has a hit, with an empty body. `partial` when `not_found` is not empty, or for the reasons a partial answer has today (a capped or search-built library). Otherwise `answered`. If this marks many answered cases partial on the broad set, `not_found` is narrowed to identifiers and names the question writes.
- **Dropped fields.** `result_count` means nothing now and goes.

- **`terms`.** Each distinctive term and the number of the library's sections containing it. Distinctive: an identifier the question writes, or a term in under a tenth of the library's sections.
- **`not_found`.** The distinctive terms with no section at all.
- **`status`.** The existing outcomes (`docs.Outcome`: answered, partial, refused, failed), decided by evidence and not by result count:
  - answered: every distinctive term is in a chosen piece;
  - partial: some distinctive term is in `not_found`, or was found but not chosen within the budget; the reason says which;
  - refused: no distinctive term has a hit. The body is empty and `not_found` is the answer.
- **`index`.** `full` or `capped` with its page count and build date for an indexed library; for pages just read, `source: search, N pages read`.
- **Group line.** Kind and count (`option rows, 2 of 17`), source `url#anchor`, and for a folded piece `same text in N other sections` with up to three of their headings.
- **Pointer.** `full section:` only on a group that was cut, and it must resolve.
- **Noise.** No warning lines above the frontmatter. Fetch warnings become one stderr line with a count.
- `--json` carries the same fields.

- **Budget.** It covers everything printed, frontmatter included: it is what the caller reads.
- **`--limit`.** When given, the most groups printed. When not given, the budget decides.

Check: the fact's text is in what was printed, and the printed tokens are within the budget.

### Round 5

Output is built (2026-11-10) as refined above. On the whole set, 97 cases with the Duo and Click ones:

| Build | Answered | Led | `not_found` checks | Mean tokens printed |
|---|---|---|---|---|
| old | 56 | 0 | 0 of 7 | 1,610 |
| round 4 | 70 | 1 | 0 of 7 | 1,716 |
| round 5 | 70 | 1 | 7 of 7 | 1,682 |

No case was gained or lost between rounds 4 and 5, on either half. Of 65 answered broad cases, 1 is marked partial.

What was measured and settled:

- **Near copies by piece identity: rejected.** Every form tied on answers. The distro sections differ by a few words per paragraph, so identical-piece counts do not see them as copies, and fuzzier matching would touch 2% of printed tokens.
- **The full budget earns its place.** Of 34 answered tune cases, the fact starts within 400 tokens in 20, within 1,000 in 30, and later in 4. A shorter default would give up a third of the answers.
- **The layout saved tokens but won no case,** and the frontmatter grew from about 35 to 75 tokens.

Still wrong:

- **duo-failmode.** Not answered in one call. A second call worded "Duo Unix configuration options failmode" prints the `failmode` and `https_timeout` rows first; one worded "pam_duo.conf failmode option" does not print them at all. Fourteen sibling sections under "PAM Examples" match the question the same way and take the top of both rankings; the options section is 35th in Gather.
- **`answered` overclaims.** 25 of the 27 cases whose fact is not printed say `status: answered`. `partial` and `refused` are reliable.
- **`terms` carries common words** (`cannot: 409`, `value: 1347`, `go: 941`). They tell the caller nothing and are most of the frontmatter's growth.
- **The same-text line** can name a section printed just below it.

Next, measured under the Protocol:

- **Siblings.** Sections under one parent heading that match the same question terms are alternatives of one another. They share one place in the section ranking, the best of them is walked first, and the others wait until everything else has had its turn; the group printed names them. This uses structure and the match, not text similarity.
- **Status.** Measure whether "a distinctive term that the library has is in nothing printed" separates unanswered cases from answered ones. If it does, that is a second partial reason (`terms_not_shown`). The rename of `answered` is decided in Round 7.
- **`terms`** lists identifiers the question writes and words in under 1% of the library's sections.

### Round 6

Measured 2026-11-10. No case changed between rounds 5 and 6 on any of the 97.

- **Siblings: rejected.** Every form tied or lost. Siblings that match the same terms are the common case, not the edge: on tune, 317 clusters formed and 32 of the members set aside held a gold quote. A question that names a page's topic matches every subsection of that page. The one confirm run lost a case. Even where the options section rose from 35th to 10th, duo-failmode stayed unanswered: the room freed went to other sections printed whole (602 tokens for one of them).
- **`terms_not_shown`: rejected.** It marked 5 of 14 unanswered tune cases and 4 of 39 answered ones. Nothing measured separates the cases whose fact is printed from the rest, which is the reason for the rename in Round 7.
- **`terms` at 1% is too tight.** 31 of 55 tune answers print `terms: {}`; `password`, in 3 of 292 sections, is not listed. It becomes: an identifier the question writes, or a word in under 1% of the library's sections or in at most 25 of them.
- **Kept:** the same-text line no longer names a section printed in the same answer. The frontmatter's median cost fell from 73.5 to 63.5 tokens.

What three rounds agree on: duo-failmode is lost to whole sections taking the budget, not to where its row ranks. That is the case for Pages.

### Round 7

Decided on 2026-11-10, after a live run of 12 coding questions through web search (old build 8 answered, round 5 build 9, none lost; results in `/tmp/ketch-code-check`).

**`found`, not `answered`.** The tool cannot know that it answered, only that it found evidence matching the question. The status is renamed everywhere it shows (frontmatter, `--json`, MCP, the outcome type). It is unreleased, so there is nothing to migrate.

**One page, one address.** A page served at two addresses (`modules` and `modules.html`, a trailing slash, an `llms/` mirror) is one page. The live run printed the same Vitest sections twice. The old build keyed pages for this (`indexedPageKey`); the rewrite lost it. Sections of the same page key are one section, at the address the index lists first.

**Pages.** A long section is read in pages; the caller decides how far to read.

- **In an answer.** A section over the page size is never printed whole. Its group shows its best-matching pieces up to the page size, in page order, with the Contents line, and its source line says how much of it that is: `(cut, 310 of 1,640 tokens)`. The live run showed why: a 1,600-token section printed whole, and the section that answered got five lines.
- **Reading on.** `--section <ref>` returns the section in reading order one page at a time, says `page 1 of 4`, and ends with the command for the next page. A section that fits one page is unchanged.
- **Measure.** Page sizes 400, 600, 800 and 1,200 tokens on the tune half, with the 12 live coding questions added to it. A fact that sits in a group printed cut, with its source, counts as led. Reported per size: answered or led, answered alone, tokens before the first fact, tokens printed. Earlier caps lost one or two answers on tune and were dropped; this asks the question again with led counted and the new cases in.

**Order within a page.** When a child section outranks its parent, the fact can land late (Celery: 97 tokens in on the old build, 1,139 on round 5). Measure printing a page's groups in page order against rank order.

**Result**, 2026-11-10. `found`, one page one address, the `terms` rule and `--section` paging (`--page N`, `page: N of M`, a `next page:` line, table headers repeated) are built. Eleven of the 12 live coding questions joined the tune half, which is now 64 cases; with confirm's 44 the set is 108.

| Half | Build | Answered | Led by a cut group | Answered or led | Median tokens before the first fact |
|---|---|---|---|---|---|
| tune | old | 39 | 0 | 39 | 187 |
| tune | round 6 | 48 | 3 | 52 | 258 |
| tune | round 7, page 400 | 46 | 8 | 55 | 292 |
| confirm | old | 25 | 0 | 25 | 161 |
| confirm | round 6 | 31 | 1 | 32 | 127 |
| confirm | round 7, page 400 | 29 | 3 | 32 | 263 |

Page sizes on tune, answered and answered-or-led: none 48 and 52; 300, 43 and 55; 400, 46 and 55; 600, 46 and 53; 800, 47 and 52; 1,200, 49 and 52.

- **The page size is provisional.** At 400 a page trades answers for leads: on tune two fewer answered and three more answered or led; on confirm two fewer answered and a tie. Every lost answer is led by a cut group with its size, one exact call away. Whether that trade is good is not a question the quote checks can settle. It is settled by the flow tests, where a model makes the calls and the count is calls and tokens to a right answer, run against a build with no page size as well.
- **Rank order within a page, not page order.** Page order was earlier on tune (287 against 336 tokens before the first fact) and later on confirm (263 against 124). It goes back to rank order.
- **Pages did not answer duo-failmode** at any size, and the answer printed gives no exact next call: every pointer leads to another distribution's section.
- **The search path still stores one page at two addresses** (`docs/search_pages.go` tells pages apart by `displayKey`). Gather now treats them as one; the store should not hold both.

### Round 8

**Defined in.** For a rare term the question writes, the place that defines it is worth a line even when its section did not win the budget. A term is defined where it is the first cell of a table row, a definition-list term, or in a section's own heading. When such a section is not among the groups printed, the answer says where it is:

```
defined in: failmode → https://duo.com/docs/duounix#duo-configuration-options
```

One line per term at most, for the terms `terms` lists; among several defining sections, the one on a page already printed, else the one Gather ranks highest. This is a lead, not a ranking rule: it adds no section to the walk. Measured under the Protocol with a fact counted as led when its source section is named on such a line.

**Also in this round:** rank order within a page; one address per page at the source, in the search path's store; a build with no page size kept beside the default for the flow tests.

**Result**, 2026-11-11. Over all 108 cases: answered 75, led 13 (1 by words, 11 by a cut group, 1 by a defined-in line), answered or led 88; the old build 64. Rank order within a page restored confirm's median tokens before the first fact to 124.

- **A definition is an exact first cell.** A table row whose first cell is the term, markup and case aside. The wider rule (a definition-list term, the section's own heading, loose matching) printed lines that were wrong about half the time: `safe` to an RDP safe-mode FAQ, `stack` to a Stack Overflow SSO page. The exact rule printed 3 lines in 108 answers: duo-failmode's (right, and the case is now led), a `kubectl drain` row (plausible), and one wrong.
- **duo-failmode is led.** The line's target read with `--section` is one page of 2,111 tokens holding the `failmode` and `https_timeout` rows. Two calls, the second exact.
- **The line goes in the frontmatter**, as `defined_in: {failmode: <url#anchor>}`. The build printed it after the body to save 3 tokens before the first fact on confirm; a caller steers by the frontmatter, and a lead that sits after 1,700 tokens of noise is in the wrong place.
- **Candidates come from the pool only**, and the pool misses most exact definitions (4 of the 7 tune terms that have one). No case suffers yet. A store query for row keys would fix it and waits for evidence that it matters.
- **Page size**, both builds from one tree: over 108 cases, 400 answers 75 and leads 13; none answers 79 and leads 6. Still provisional.

## Flows

The model runs the flows; ketch gives it the signals. So the flows are taught where the model reads: the ketch skill file and the MCP tool description. The design note with the diagrams is `ketch-flows` (2026-11-10).

### Recipes

This is the text for the skill file. The MCP description carries the same three recipes in a few lines each, with `docs(...)` calls in place of the commands.

```markdown
## Docs flows

`ketch docs` prints evidence and a verdict. It has no model: you decide what to do next. Read the frontmatter first.

| You see | It means | Do |
|---|---|---|
| `status: found` | evidence matching the question is printed; not a promise that it answers | read the groups |
| `status: partial (terms_not_found)` | some of your terms are nowhere in this source | read `not_found`; ask another source for those terms |
| `status: refused` | nothing here matches | stop asking this source |
| `origin: by name, …` or `origin: unverified, …` | a search read these pages; only a package name supports the site, or ketch could not confirm it as the official docs (it may still be) | name the site you cite, or name the official docs with `--library` |
| `defined_in: {term: ref}` | where a term is defined, when that section was not printed | `ketch docs --section <ref>` |
| `(cut, N of M tokens)` on a `source:` line | the group shows part of its section | `ketch docs --section <that source>` if you need the rest |
| `also in this section: …` | what a cut group left out, by name | use one of those names in your next call |
| `index: …, built <date>` or `stale: unverified` | how old the evidence is | say so if the answer depends on it being current |

### Lookup

One question about one library or product.

1. `ketch docs "<question>" --library <name or docs URL>`
2. The fact is printed: answer, citing each `source:`.
3. The group you need is cut, or `defined_in` names it: `ketch docs --section <ref>` once, then answer.
4. Otherwise, Follow.

### Follow

The docs use other words than the question.

1. Take one thing the last call printed: a name from an `also in this section:` line, a heading, a `defined_in` target.
2. Ask again in those words, or read that section with `--section`.
3. After three calls to one source, stop. Say what you tried and that this source does not cover it.

Every call after the first uses something the last one printed. Do not rephrase blind.

### Survey

The question spans products.

1. Split it by owner. A first call's `not_found` shows which terms belong to another source.
2. A docs site you do not know: `ketch docs --resolve <name>`.
3. One Lookup or Follow per source. The calls are independent: run them together.
4. Combine. Cite each claim's `source:`. Say what no source covered.
5. A source you will ask again: `ketch docs index <site>` once. It then answers offline.
```

### Flow tests

The quote checks ask whether one call printed the fact. A flow test asks what the caller needed to get the answer right.

- **Caller.** A model given only the recipes and the `docs` tool of ketch's own MCP server, in a sandbox with built indexes. Sonnet runs every scenario; Haiku runs a sample, as a weaker caller.
- **Scenario.** A question, the sources it may use, the facts a right answer states, and for some, the fact that no source covers a part of it. About ten per flow, drawn from the open cases and the live coding questions; Survey scenarios need two or three sources.
- **Measures.** Right or wrong, judged against the scenario's facts; calls made; tokens of tool output read; whether it stopped when the recipes say to stop; whether every claim cites a source that holds it.
- **Builds.** The default build and the build with no page size, on the same scenarios. This is where the page size is settled.
- **Gate.** Reported per flow. No release before the Lookup and Follow scenarios are right at a rate we would put in the README, with the median calls beside it.

### First run

2026-11-10, build `d54c6d3`. 34 scenarios (10 Lookup, 16 Follow of which 3 are "not covered", 8 Survey). The caller had only the MCP `docs` tool, the ketch process had no network, and an Opus judge graded each answer. "Facts" means every required fact was stated and every "not covered" was said; "right" also requires that nothing was claimed that no tool result supports.

| Flow | Caller | Facts | Right | Median calls | Median tokens read |
|---|---|---|---|---|---|
| Lookup | Sonnet | 10 of 10 | 6 of 10 | 1 | 2,562 |
| Follow | Sonnet | 16 of 16 | 14 of 16 | 1 | 2,518 |
| Survey | Sonnet | 6 of 8 | 5 of 8 | 2 | 4,928 |
| sample of 12 | Haiku | 12 of 12 | 9 of 12 | 1.5 | 2,619 |

No run made more than three calls to one source. Survey calls ran in parallel in 31 of 32 runs. The founding question took two calls: the question, then the section `defined_in` named.

What the transcripts show:

- **The facts arrive.** Every Lookup and Follow scenario had its facts stated. Where an answer was not right, the caller had added something from memory, most often into the `…` of a cut group: 5 of 9 repeated Lookup runs on this build, 1 of 9 with no page size.
- **`defined_in` is followed**: 8 of the 9 Sonnet runs that were offered one, 5 of 6 for Haiku.
- **The other leads are not used.** In 104 Sonnet runs no caller read a cut section, used a name from a Contents line, paged, or asked another source for a `not_found` term. They were rarely needed for the facts; they would have prevented the invented claims and the one Survey that stopped at the first source.
- **The Survey that stopped** was the three-source one. The caller asked Duo, read `not_found: [pam_sss]`, and never asked the Linux-PAM or SSSD sources that were indexed beside it. Nothing in the tool tells a caller which sources it has.
- **A page URL as `library`** was passed six times and silently widened to the whole site.

**The page size stays at 400.** With three runs per build on the nine scenarios where the builds disagreed: facts stated in 26 of 27 runs with pages against 20 of 27 without; invented claims in 9 against 8. One scenario with byte-identical tool output on both builds went 3 of 3 against 1 of 3, so much of the difference is the caller and the judge. The edge on facts is with pages, and the invented claims are a wording problem.

### Round 10

Decided from the first run.

- **Say what `…` means.** The tool description and the skill: `…` marks text left out; do not fill it in; read it with `section`, or say it was cut.
- **`library` is a root; a deeper URL is a scope.** A `library` URL below an indexed root answers only from pages under that URL, and the frontmatter says `scope: <url>`. If no indexed page is under it, the answer is refused and the note names the root. Never a silent widening.
- **A caller can ask what is indexed.** `--resolve` answers from the local index first, with no network: the libraries whose name, title or root match, each with its root, title, page count and build date; with an empty query, all of them. Survey step 2 reads: ask what is indexed before saying a source is unknown.
- **`not_found` routes.** Survey: for every `not_found` term, ask the indexed source that owns it before saying it is not covered.
- **Leave `tokens` unset** unless the result is too long to read. Callers lowered it in 8 calls for no reason.
- **Escapes must not hide a term.** `not_found: [service_err]` was printed for a section whose text holds `service\_err`. Stored text is cleaned before it is tokenised, and an index built before this re-tokenises from its stored text without fetching.
- **`--section` prints the same frontmatter as an answer** (`library`, `index` or `source`, `stale`, `page`), without `result_count`, and its heading is not repeated.
- **A result has no `source: search`.** In `--json` and MCP the per-result backend name goes; the answer's `backend` says it once.

Then the flow tests again on the default build, three runs per scenario, so the rates come with their spread.

**Result**, 2026-11-10, three runs per scenario. A scenario counts when at least two of its three runs do.

| Flow | Caller | Facts stated | Right | Median calls | Median tokens read |
|---|---|---|---|---|---|
| Lookup | Sonnet | 10 of 10 | 9 of 10 | 1 | 2,546 |
| Follow | Sonnet | 15 of 16 | 14 of 16 | 1 | 2,478 |
| Survey | Sonnet | 4 of 8 | 3 of 8 | 2 | 5,016 |
| sample of 12 | Haiku | 12 of 12 | 9 of 12 | 1 | 2,591 |

- **The `…` wording worked where there is one source.** Lookup went from 6 right of 10 to 9. It also turned one first-run "right" into "missing", correctly: the fact had never been printed.
- **Survey did not improve.** Invented claims rose there (7 of 24 runs). Eight scenarios is too few to read more into it.
- **No caller asked what is indexed, and none routed a `not_found` term**, in 138 runs. The harness lists the sources in the system prompt, so `resolve` had nothing to add; and a caller whose first query does not write `pam_sss` gets no `not_found` to route.
- **A deeper URL as `library`** was passed in 8 runs and scoped every time, with no silent widening.
- **Cleaning before tokenising cost one QA case in each half** (tune 46 to 45 answered, confirm 29 to 28). The counts are now right: `pam_duo` is in 50 sections, not 48, because two distribution sections wrote it `pam\_duo`. Those sections then lead the pool again. The count stays right; the loss is the near-copy problem, which is still open.
- **Re-tokenising an older index** takes about a second on a 13,883-section library, once.

Eight to sixteen scenarios per flow cannot carry a rate. The flow runs move to `ketch-bench` with a set of about 300.

### Round 11: library names

**The problem.** In the `ketch-bench` flow run of `f67ea6d` (306 cases, a pin of 135 libraries), Sonnet callers passed a `library` value that meant an indexed library and got a web search instead in 85 calls. Two shapes. Product names: the index stores Django as `Django Project`, Gin as `Gin Web Framework`, Ansible as `Ansible Community`; the primary name was the whole name squashed (`djangoproject`), and the words of a multi-word name were kept out of every tier, so `ansible` did not even match its own host label. URLs without a scheme: `auth0.com/docs` was read as a Go module path and never compared with the indexed roots. A wrong library answered silently is worse than a miss, so every rule below was measured for what it gets wrong as well as what it gets right.

**Measurement set.** Every `library` and `resolve` value the Sonnet and Haiku callers passed in that run (959 calls, Context7 calls left out), paired with the indexed root it means, read from the case's roots; a value naming no indexed library (an MDN URL, `jenkins`) means none. 369 distinct values: 312 `library`, 57 `resolve`. They are split by the site they mean, tune or confirm, as the cases are. It is `docs/testdata/names/pinned.json` (values, the roots they mean, and the pinned libraries' stored metadata), run by `TestLibraryNamesMeasured`. Right: the library the question path picks is one the value means; wrong: it picks another; none: it picks nothing. A `resolve` value is right when the listing holds the root. The twin sites of one library (requests at two hosts, tokio's site and its docs.rs docs) both count as right.

| | Tune right | Tune wrong | Tune none | Confirm right | Confirm wrong | Confirm none |
|---|---|---|---|---|---|---|
| `library`, before | 138 | 2 | 28 (1 means none) | 106 | 1 | 37 (5 mean none) |
| `library`, after | 156 | 0 | 12 (1) | 117 | 0 | 27 (5) |
| `resolve`, before | 29 | 0 | 5 | 21 | 0 | 2 |
| `resolve`, after | 30 | 0 | 4 | 21 | 0 | 2 |

By calls rather than distinct values, `library` right went from 425 to 474 on tune and 278 to 307 on confirm, wrong from 6 to 0. Of the 85 Sonnet calls that fell through, 55 now answer from the library they meant. The three wrong ones before: `okta` and `Okta` picked Okta's end-user help site over its developer docs, and `@testing-library/react` picked React's docs.

**Negatives**, 31 written by hand, each with the pinned libraries as they are or with one taken out or put in: `react` with only React Router, `vue` with only Vue Router, `router`, `django` and `rest` with only Django REST framework, `spring` with only Spring Boot, `google` and `cloud` with only Google Cloud Storage, `redis` with only Go Redis, `@tanstack/react-query` with TanStack Query out, and against the pin as it is `api`, `docs`, `testing`, `rust`, `tanstack`, `cloudflare`, `hashicorp`, `aws`, `amazon`, `android`, `atlassian`, `github`, `cisco`, `ietf`, `freedesktop`, `hugging face`, `uptrace`, `palletsprojects`, `tiangolo`, `python` and `go`. None may name a library; each may list some as near. Before: 13 named one (`rust` a docs.rs crate, `tanstack` TanStack Query, `github` GitHub Actions, `python` Instructor, `uptrace` Bun, and so on). After: none. `TestLibraryNamesNegatives` holds them.

**Rules kept.** All from the stored metadata at question time; no index is rebuilt. "Alone" is the rule over the old matcher, "out" is all rules but this one; counts are distinct `library` values right on tune / confirm, or negatives named.

| Rule | Alone | Out |
|---|---|---|
| a. Descriptor words: a site name or title left with one word once its descriptor words go is a primary name (`django`, `gin`, `ansible`, `heroku`, `grafana`, `temporal`, `polars`, `rocket`). `Vue Router` keeps two words. | +4 / +8 | −2 / −6 |
| b. The word of a multi-word site name that is the host's one distinctive label, when the root's path names nothing, is a secondary name (`duo` of Cisco Duo at duo.com/docs/, `langchain`, `encore`, `slack`) | +5 / +2 | −3 / 0 |
| c. A value shaped `host[/path]` that no name or package matches is compared as `https://` + value with the indexed roots, at, under or over; under is a scope, as with a URL. `go.uber.org/zap` still matches by its name. | +7 / +3 | −7 / −3 |
| Site name joined to a path name is a primary name (`githubactions` for GitHub at /en/actions/, `tanstackquery`, `reduxtoolkit`) | +4 / 0 | −4 / 0 |
| f. A `resolve` name is compared as one key too (`vercel ai sdk` holds AI SDK); a name with spaces none matches is `[not_found]` from the index instead of the registries' `[validation]` | `resolve` +1 / 0 | −1 / 0 |
| Guard: a site-wide name (the site name, a host label, the descriptor name) names a library documented under a path of its own only when the path names it too: `hashicorp` is not Terraform, `tanstack` not TanStack Query, `rust` not a docs.rs crate, `okta` not the help site at /oie/ | fixes the 2 tune wrongs; 8 negatives | 8 negatives named |
| Guard: a host label several projects' libraries share names none of them (`palletsprojects`, `uptrace`, `tiangolo`) | 5 negatives | 3 named |
| Guard: a module path's host and owner segments are not names (`redis` in /github.com/redis/go-redis/) | 1 negative | 1 named |
| Guard: a language label beside another distinctive label is not a name (`python` in python.useinstructor.com) | 1 negative | 1 named |
| Guard: the last part of a scoped package (`react` of `@testing-library/react`) names only a library that goes by the scope too (`astro` of `@astrojs/starlight`) | the 1 confirm wrong | 1 wrong |

The scoped-package guard was found by the wrong it fixes on the confirm half, so confirm does not test it independently. `spec` and `specs` joined the path words that name nothing (`docs`, `api`), so that openid.net/specs/ keeps its host's name under the first guard.

Descriptor words: doc, docs, documentation, guide, guides, reference, project, community, web, framework, dev, developer, developers, center, labs, platform, user, official. It is a list of its own beside `genericNames`, because `genericNames` holds library, sdk and api, and stripping those makes `testing` name Testing Library and `ai` name AI SDK.

**Rules dropped.** Rule b as first written, the first word of a multi-word name that is also a host label or a path segment, named `query` for TanStack Query and `rest` for Django REST framework through their path segments; only the host-label half was kept. A containment test for the guard (a path name holding the site name) let `cloudflare` name Cloudflare One and `amazon` name Amazon S3; it now needs the same name, the name with a release number (`psycopg3`), or the end of the name (`kit` of SvelteKit).

**d.** The live path resolves a name through a registry only for a pinned spec (an ecosystem prefix or a version), in parallel with the web search, and uses the root as a site preference. When the site it then reads is under an indexed root, it already answers from the index. It never resolves before searching, so with the network down it never runs. Resolving first would add registry latency to every unmatched name; not added.

**e. `indexed_near`.** When a library value names no indexed library, the indexed libraries near it are listed: for a URL, with its scheme or without, those on its host; else, or when none is, those `resolve`'s loose match finds (the value whole in a field, then as a key, then one of its words). One to five, never more, and none of them is answered from. The answer from elsewhere carries them in the frontmatter, `indexed_near: {Cisco Duo: https://duo.com/docs/}` (a title two share, or none, gives way to the root without its scheme), the same in `--json` and MCP as `[{title, library}]`, and ends with a line saying to pass one of those roots as `library` (the MCP `hint`). A refusal or failure, with the network down or the search empty, names them at the end of its error. Of the 33 `library` values still unmatched that mean an indexed library, 29 list it (tune 9 of 11, confirm 20 of 22). The four that do not: `ai` and `regex rust` (no near list), and two module paths, for which other modules or GitHub's docs are listed.

**Still unmatched**, by design or for now: versioned or moved URLs (`docs.djangoproject.com/en/stable/…` against /en/6.1/, `docs.ansible.com/ansible/latest/` against /projects/ansible/latest/, `docs.sqlalchemy.org/en/20/`), which `indexed_near` lists; product names inside a vendor's docs (`duo unix`, `Duo Authentication Proxy`, `stripe cli`); package names unlike the site's (`mem0ai`, `testcontainers-go`); concepts (`JWT`, `PKCE`, `oauth` for RFCs). Two guards cannot hold on the pin: `gitlab` names GitLab CI docs, whose root /ci/ reads as a locale and so names nothing; `query` names TanStack Query, whose stored title is TanStack alone, through its path.

**Bare questions.** The same names decide which indexed library a question with no `library` names. Over the 460 question-only calls of the same run: the library the question means 286 before, 347 after; another library 18 before, 3 after (all three the twin site of the library meant); none 156 before, 110 after. None that named the right library before names another or none now.

**Check tier.** `docs check` with this build against the pin: 190 of 291 answered, 214 answered or led, 20 of 30 not covered, 0 cases changed from `baseline-docs-check.json`.

**Flows.** Sonnet caller, one run each of the 63 cases where a run of `f67ea6d` passed a `library` value that fell through to a web search, Opus judge, network denied:

| Build | Facts stated | Right | Median calls | Calls in all | Median tokens read | Runs that reached a web search |
|---|---|---|---|---|---|---|
| `f67ea6d` | 56 of 63 | 48 of 63 | 3 | 202 | 2,897 | 63 (69 calls) |
| this round | 58 of 63 | 54 of 63 | 1 | 109 | 2,667 | 22 (23 calls) |

With the network denied, a web search fails, so `indexed_near` reached the callers only in the refusal's error: 19 of the 23 errors named near libraries, and in 15 the caller's next call passed one of their roots (`duo unix` to duo.com/docs/, `mem0ai` to docs.mem0.ai, `https://docs.sqlalchemy.org/en/20/` to /en/21/). The four without one were the MDN URL twice, `jenkins` and a call with no library. The MCP tool description used in this run did not yet name `indexed_near`.

### Round 12: cuts and guidance

**The problem.** In the `ketch-bench` flow run of `694b112`, callers state the required facts in about 90% of runs and are right in about 75%: the gap is a claim the printed text does not support. All 107 runs with facts stated and right false were classified:

| Class | Runs | What happened |
|---|---|---|
| Filled a gap | 33 | The claim is about text the output visibly cut. 23: a `…` cut the sentence or row that explains a printed code block or row. 9: a huge section shown as a sliver (an RFC indexed as one section, nginx's 15,919-token Directives section at 400 tokens; one snippet ends mid-sentence). 1: an `also in this section` name not followed. In all 33 the caller had calls left and the hint to read the cut group, and did not use it. |
| Misread | 16 | 5 at a cut: the cut removed what the next printed line depends on (`…` right before "*or* an object that implements…"; a dropped signature, so the next description reads as the function above's). 3: near-identical sibling sections merged. 8: plain misreading. |
| Outside knowledge | 41 | Something from memory, mostly true, stated as if the docs said it; 16 weakly invited by a heading, breadcrumb or index entry naming a concept without its text, or a code example with no explanation. |
| Judge too strict | 17 | Out of scope. |

**Cuts that keep what explains the printed text.** Pieces are still ranked alone; some are no longer printed alone. A piece brings the piece right before it in its section, and a cut group that takes it takes both (at most four pieces, transitively):

- **A code block brings its lead-in**: the piece right before it when that piece ends in a colon ("…using the `terraform providers lock` command:").
- **A connective brings what it depends on**: a piece opening with `or`, `otherwise`, `alternatively`, `in that case`, `in this case`, `this`, `these` or `the above` (after a list marker or emphasis, case aside) brings the piece before it.
- **A definition head is one piece with its description**: a one-line paragraph of at most 80 tokens that is not code, a list, a table, a quote or a heading and does not end like a sentence (a signature such as `PIL.ImageOps.fit(image, size, …) → Image`, a term with its type such as `mode string (Optional, default: single)`, a term in code alone on its line) opens the first piece of the prose paragraph after it. A label line before prose ("Menu") is now part of that paragraph's first piece too: requiring a code mark, a parenthesis or an identifier in the head tied on every answer and put the first fact four tokens later on confirm, so the simpler rule stayed.
- **What a piece brings is charged to the budget, not to the page.** A long section is still cut to 400 tokens of chosen pieces; the lead-ins and heads they bring come on top.

Measured on the docsqa sets (answered, answered or led; tune of 64, confirm of 44) and the bench check tier (tune of 170, confirm of 121, against `baseline-docs-check.json`):

| Build | docsqa tune | docsqa confirm | check tune | check confirm |
|---|---|---|---|---|
| `694b112` | 45, 55 | 28, 31 | 116, 135 | 74, 79 |
| code brings its lead-in, outside the page | 46, 55 | 28, 31 | 119, 135 | 74, 79 |
| connectives | 45, 55 | 29, 31 | 115, 135 | 75, 79 |
| definition heads | 46, 55 | 28, 31 | 119, 136 | 74, 79 |
| all three, outside the page | **48, 55** | **29, 31** | **122, 136** | **75, 79** |
| all three, charged to the page | 46, 55 | 27, 31 | | |
| all but the lead-in rule | 47, 55 | 29, 31 | 119, 136 | 75, 79 |
| all but connectives | 47, 55 | 28, 31 | 122, 136 | 74, 79 |
| all but heads | 47, 55 | 29, 31 | 119, 135 | 75, 79 |
| connectives without `this`/`these` | 47, 55 | 29, 31 | 122, 136 | 75, 79 |

Each kept rule loses when taken out. Dropped, each for a loss on a half:

- **A lead-in brings its code**, both directions: docsqa confirm 28 to 27 answered, as a long block paid for by its lead-in pushed the fact out of a 400-token page. With the code outside the page: tune 45 to 44 answered and one case no longer led.
- **A colon lead-in and whatever block follows travel together**, both directions: confirm 28 to 25. One direction only (any block brings its lead-in, lists and quotes too): check confirm 74 to 73.
- **A lead-in is never printed alone**, with the kept rules: docsqa tune 48 to 44, check 197 to 186 answered. Lead-ins often hold the fact.
- **A piece that does not fit with what it brings prints alone**: confirm 29 to 28 with connectives, 27 to 26 with the lead-in rule.

Cost in tokens. Answers fill the budget either way: mean tokens printed moved by one or two (docsqa 1,687 to 1,685 tune, 1,675 to 1,673 confirm; check 1,899 to 1,898). What the rules spend shows as fewer groups: the check tier printed 2,175 groups in 376 answers before and 2,137 with the three rules (the lead-in rule alone 2,152, connectives 2,174, heads 2,180), and 182 cut groups show more than 400 tokens, against 40. Median tokens before the first fact: docsqa 312 to 312.5 on tune, 125 to 126 on confirm; check 250 to 259 on tune, 148 on confirm before and after.

**Mid-sentence cuts.** Only one cause was found: an RFC's HTML is a series of preformatted blocks, one per printed page, so its text is a run of code blocks with a page footer and header between them, and a page break falls inside a sentence. Each page is one piece. Joining adjacent blocks at question time would make the whole RFC one piece; the fix belongs to index time, with the coarse sections below. A scan of the 1,340 `…` gaps in the check tier found no other sentence cut in two: the gaps that end without punctuation follow code, table rows or labels, and those that start in lower case are descriptions whose term line was a label dropped as no piece (nginx's njs reference: `r.requestText`, then "client request body if…"), which the head rule now prints together.

**Replay.** For each run of the classification whose cut is not a huge section (24 filled a gap, 5 misread at a cut), the caller's first call was replayed through the MCP server against the pinned index, network denied, on `694b112` and on this round's build. One misread run (sibling PAM examples relabelled) has no single explaining text; of the other 28, the explaining sentence or row is printed now in 7 and before in none: a code block's lead-in in 5, a connective's antecedent in 1, a definition head in 1. In 10 of those runs the old output printed a piece without what it depends on (code without its lead-in, "This object…" without its object, a description under another function's signature, a bare `Default:`); in the new output that never happens: 7 print the context, 3 no longer print the piece. The 9 huge-section runs print it in neither build, nor do the two Flask runs among the 28 (84 tokens shown of a 5,662-token API section). The kept rules cannot reach the rest: a sentence after a code block, a row of another table, text of another section.

**Guidance.** Two rules of use, in the MCP description, the skill's Docs flows and pi's tool description:

1. Text behind `…`, or in a truncated result, is unread: read it with `section` before stating what it says, or say you did not read it.
2. Whatever the answer says that no result prints is the caller's own knowledge: label it so, never cite the docs for it.

The second replaces "state only what a result prints; say anything else is not documented here", which asked callers to deny what they know instead of labelling it. The hint states the fact instead of offering an option, once per answer, only when something was cut, with the count:

```
2 groups above are cut and their hidden text is unread: run ketch docs --section <its source> before stating what it says
```

MCP `hint`: `2 results are truncated and their hidden text is unread: read docs(section="<its section>") before stating what it says`. `--json` now carries a `hint` too, in the CLI's words with `<its section>`. With `indexed_near`, MCP and `--json` add its line after `; ` as before. The description is 686 estimated tokens (691 before): it dropped "to read one page, use section" (the `section` argument says it), the index line's "or source search" and the Context7 quota's reset detail.

**Bugs.**

- **HTML in code was stripped at question time.** Sort's cleaning removed tags with a class or style attribute inside code blocks (meant for leaked `<span class="toreplace">`), so a Beautiful Soup example printed `# []` where the docs show `# [<p class="body strikeout"></p>]`. The stored text is right. Code blocks are no longer cleaned at all: HTML in them is the example's, and a tag an extractor leaked cannot be told from one. The leaked spans in Duo's code blocks (454 sections of the pin) now print as stored; that leak is an index-time extraction bug, not fixed here. Cost: bs4-mirror goes from answered to led in the check tier, because the real HTML makes its section 786 tokens instead of 496 and it is now cut at 400.
- **`indexed_near` through a code host's name.** `github.com/testcontainers/testcontainers-go` listed GitHub's own docs through the keyed match on "github". A URL or module path on a host no indexed library is on is now near what its path names first: a library one of whose names, title or site name the path's key holds (four letters or more) or one of its segments is (three letters: `bun` in `uptrace/bun`, not `man` in `slash-commands`). Only when the path names nothing does the loose match run on the whole value, host included, so `go.temporal.io/sdk` still lists Temporal's docs. Dropping the host from the loose match altogether, as first proposed, lost 3 of the 33 unmatched values that mean a library (Slack's API URLs, Temporal's module path) and added 12 listings for values that mean none, through generic path words (`http`, `status`); `socket` sent a Slack URL to a WebSocket library. On the Round 11 measurement set: the library meant is listed for 29 of 33 values before, 30 now; other libraries listed beside it 18 before, 9 now; listings for values that mean none 3 before and after (one AWS URL listing the AWS guides on its host). A repository path that names no indexed library still lists the host's docs.
- **`resolve` with free text** that matches nothing now ends: "resolve with no name lists every indexed library". It does: 135 libraries on the pin, CLI and MCP.
- **`defined_in` at an unrelated section.** The term `shutdown` was the first cell of a table of restricted Valkey commands; the exact-first-cell rule has no notion of what the section is about. A defining section must now also name another of the question's distinctive terms, in its text or heading path, when the question has one. Every docsqa and check-tier answer is unchanged. The line is gone from the Heroku question and from 5 check-tier answers (`context` to a Svelte options table twice, `dark` to a Tailwind quick reference, `React` to a Vite template table, `retries` to a RADIUS page); `am` moved from one Stripe tax table to another, still wrong; `duo-failmode`'s and `kubectl drain`'s lines stay, and three wrong ones with them (`silences`, `page`, `Deployment`), whose sections hold another of the question's terms. Requiring a description cell in the defining row instead (three words or more) also removed the Heroku line but kept the Svelte lines.

**Coarse sections**, report only. Sections are cut at index time by `splitSections` (`docs/web_sections.go`), at ATX headings in the extracted Markdown only: H1 to H3 always, deeper headings for an API entry or inside a section over 4,000 bytes. nginx marks each directive with an anchor before a table, with no heading, so `ngx_http_core_module`'s Directives is one section of 62,863 bytes; an RFC's numbered headings are lines inside preformatted text, so each RFC is one section (163,948 bytes for RFC 6749). The pin holds 414 sections over 16,000 bytes, about 4,000 tokens, of 248,158.

A section with no headings past a size could be split at two structural marks the page carries:

- inner anchors whose id names the entry after them: nginx's `client_max_body_size` anchor, with no text, precedes the table whose first code is `client_max_body_size size;`;
- numbered headings in plain text: a line opening with `4.1.4.` that matches the text of the page's anchor `section-4.1.4` (`4.1.4. "exp" (Expiration Time) Claim`). Joining an RFC's page blocks and dropping the page footers and headers between them belongs to the same pass and removes the mid-sentence cuts.

Each new section takes the anchor's id, so `--section url#anchor` reaches it. Nothing needs fetching: the pinned index's `pages` table holds no page bodies, but the pin's page cache does. It holds, for each fetched page, the extracted Markdown and the page's anchors (id and text), not the raw HTML: 15,056 entries; for nginx's core module 91,280 bytes of Markdown and 159 anchors, its directives among them; for RFC 7519, 63,227 bytes and 127 anchors with section numbers and titles. A re-split is a rebuild from that cache into a new pin, not a change to the current one.

**Tests changed on purpose.** `TestSplitText` and `TestSplitParagraphs`: a label line before prose heads that paragraph instead of being dropped. `TestCleanText`: code blocks keep their HTML. `TestEvidenceDefinedIn`: its options row now names the question's other term. The hint tests: the new wording, with a count.

### Round 13: coarse sections and spec names

**The problems.** Two, both at index time. In the `ketch-bench` flow run of `7f57617`, 9 of 33 "filled a gap" failures came from a huge section printed as a sliver (an RFC indexed as one section, nginx's 15,919-token Directives section at 400 tokens). And 6 of 16 runs that lost facts were spec questions asked with no `library`: the question went to web search and `resolve "oauth"`, `"jwt"`, `"oidc"` to the package registries, though RFC 6749, 7519 and 7636 are indexed.

**Coarse sections.** A section longer than 1,500 estimated tokens, on a page whose anchors are known, is split further at one of two structural marks; failing both it stays whole. Nothing is split by size.

- **Numbered headings in preformatted text** (an RFC on datatracker). The page's code blocks separated only by blank lines are one run: joined, with the running header and footer (the first and last line of a block, digit runs aside, when at least three blocks and half of them share it) and the blank lines of the page break dropped; one blank line stays where the line before the break ends a sentence. A line at the left margin opening with a number whose title agrees with the page's anchor of that number (`section-4.1.4`, `4.1.4. "exp" (Expiration Time) Claim`) starts a part; its path is the numbering (4, 4.1, 4.1.4). Each paragraph of the joined text is a code block of its own, so an answer can print one. No sentence is cut by a page break any more.
- **Entries the page's anchors name.** A block (after a blank line) whose first code span, or in a table row the first cell's past its label cells, is an anchor's id followed by no identifier character (nginx: `client_max_body_size size;` under the `<a name>` before it); or whose whole line is a labelled anchor's text (Sphinx's definition terms). The part's heading is the anchor's text, else its id; its path is the section's plus the entry.

Each part takes its anchor (`source:` lands on the entry, `--section` reads it alone); the text before the first mark keeps the section's own heading and anchor.

| Guard | What it prevents |
|---|---|
| A list item, a quote or a block opening with a link is no mark | a table of contents or a footnote taken for the entry it points to |
| An id with no letter, a footnote's or citation's id (`fn1`, `cite_note-3`), an anchor whose text is a citation label (`[RFC2616]`) | a bibliography split into one section per reference (RFC 9110's references) |
| A permalink is no mark | xml2rfc puts a pilcrow on every paragraph (openid.net drafts split per paragraph); a signature that wraps carries it on its last line (Polars: the signature cut in two) |
| Anchors met in page order, each once; anchors the page's headings took are left alone | a back-reference or a repeated name marking a second entry |
| One kind of anchor per section, labelled or not, whichever marks more | nginx's `reuseport` and `multipath` (labelled parameters) cutting the `listen` directive (marked by bare anchors) |
| Numbered headings at the left margin, without dot leaders, titles agreeing with the anchor's | an RFC's table of contents and its indented numbered lists taken for headings |
| An entry under the smallest section a page keeps joins the one before | text lost: a near-empty section is dropped |
| At least two marks; the entries' median at least 100 tokens | a back-of-book index (PostgreSQL's, 1,537 entries of about 11 tokens) or a list of one-line members split into sections that each print whole and crowd the guide sections out |

**Measured on the pin's page cache** (the build's own sectioning, run over the cached pages): 14,818 of the pin's 23,962 pages are in the cache with their anchors; 14,813 of them section exactly as stored. 3,477 of the pin's 248,158 sections are over 1,500 tokens: 2,155 on those pages, 1,322 on pages read from Markdown sources or `llms-full.txt`, whose anchors are unknown and which no rule touches. Of the 2,155, 166 are split: 3 by numbered headings (RFC 6749, 7519, 7636), 163 by entries, on 130 pages of 22 libraries; the rest have no mark that passes the guards. The pin goes from 248,158 sections to 252,698; over 4,000 tokens from 414 to 353; over 1,500 from 3,477 to 3,356. On the split pages, 3,526 sections become 8,066, median 158 tokens. A sample of 15 split sections from 15 sites (nginx, PostgreSQL, an OpenID draft, Pillow, Click, RFC 6749, pytest, Django, SQLAlchemy, Flask, Matplotlib, attrs, redis-py, websockets, Ansible), read whole with the section after each: no wrong split. An earlier sample, before the median guard, found the last entry of a definition list taking the section's closing prose (PostgreSQL's `MAINTAIN` privilege); that shape remains possible.

**Check tier**, same binary on the first pin and on the re-split one (answered, answered or led):

| Variant | tune of 170 | confirm of 121 |
|---|---|---|
| first pin | 123, 136 | 74, 80 |
| entries' median 40 | 123, 133 | 80, 85 |
| median 60 | 124, 134 | 81, 85 |
| **median 100, threshold 1,500** | **126, 136** | **81, 85** |
| median 150 | 125, 136 | 79, 84 |
| median 100, threshold 1,000 | 126, 135 | 81, 85 |
| median 100, threshold 3,000 | 126, 136 | 79, 84 |
| median 100, threshold 4,000 | 126, 136 | 77, 82 |

At median 40 Flask's and redis-py's API members, each printed whole as a group of its own, displaced their guides (`flask-blueprint`, `redis-py-pipeline`, tune). The tune half put the floor between 75 (Flask's members) and 115 (nginx's directives); 100 kept both halves. Thresholds from 1,500 to 4,000 tie on tune; 1,500 is the start the plan gave, and confirm agrees. The rules touch disjoint libraries: numbered headings gained 4 cases (RFC 6749, RFC 7519, `nc-jwt-recommended-lifetime`, `nc-pkce-plain-oauth21`), entries 6 (nginx twice, PostgreSQL three times, Pillow) and lost the led of `pillow-draw-text-font`, which came from a 5,909-token Methods section printed as 400 tokens that did not hold the fact. All: 197 to 207 answered, 216 to 221 answered or led; `verify` unchanged.

**Rebuilds.** A page whose content is unchanged is not sectioned again by a rebuild, so an existing index would have stayed coarse. A page that this build sections into a different number of sections than the store holds now counts as changed and is stored again. No format change: an index built before keeps working, coarser.

**Spec names.** The cause: a one-page library's stored title and site name are its host's (`IETF Datatracker`); the document's own title is only its page's title, which naming never read. Round 11's guard keeps the shared host label from naming any of the four RFCs, rightly. A library of one page now also answers to its document's names. `docsindex` reads the page's title and the start of its first section with the libraries (never stored, so an index built before gets them too):

- the title, and the title without a leading article or a parenthesis (`jsonwebtoken`);
- an acronym the title or the lead spells out for the title's first words: `JSON Web Token (JWT)`; "Proof Key for Code Exchange (PKCE" in the abstract, for the title `Proof Key for Code Exchange by OAuth Public Clients`. A word may give its first letter or its capitals (`OpenID` gives `OID`); a function word may give its letter or none;
- a name with its version in the title, whole and by major: `oauth20`, `oauth2`;
- not a name the root's path rules out (`redis`, the page title of go-redis's package page, is a module path's owner, as Round 11 decided).

`resolve` matches the document's title and shows it in place of the host's. A keyword question opening with a name and a number reads them as one name too (`RFC 7636 PKCE …` is `rfc7636`), a guess, so a miss reads on.

`oauth` is no name: it is a word of two titles. `resolve "oauth"` lists both RFCs; `library: "oauth"` names neither and lists both as `indexed_near`. Had a value named two documents, the rule for several matches would answer from the newer by build time with a note calling the other another version; no value does now. OpenID Connect Core is one page among 399 at openid.net/specs/, a library of many documents: `resolve "openid connect"` lists it through the host label, `oidc` names nothing.

Measured: on the Round 11 set the 369 values are unchanged on `library` (156/0/12 tune, 117/0/27 confirm); on `resolve`, right goes from 30 to 32 on tune and 21 to 23 on confirm (`oauth`, `PKCE`, `JWT`), wrong stays 0. Eleven values were added for the documents' names (`rfc7519`, `RFC 6749`, `oauth 2.0`, `oauth2`, `jwt`, `json web token`, `pkce`, `RFC 7636`; `resolve` `oauth2`, `json web token`, `pkce`): all right. Nine negatives were added (`oauth`, `rfc`, `token`, `json`, `authorization`, `exchange`, `oauth 2.1`, `http`, `ietf datatracker`): none names a library. Over the 385 question-only calls of the `7f57617` flow run, 12 now name an indexed library, each the RFC the case is about; none changes to another library.

**A second pin.** `.docs-index/pin2/` in the bench, built from a copy of the first pin's page cache with the network denied. `ketch docs index` cannot rebuild offline: discovery reads `llms.txt` and sitemaps raw, which the page cache never holds, and 9,140 of the 23,962 pages (Markdown sources, `llms-full.txt` chunks) have no cache entry. So the pin was copied and every page whose cached text reproduces its stored sections and which the new splitter sections differently was sectioned again through the build's own write path: 130 pages of 22 libraries; every other row is as it was. 135 libraries, 23,962 pages as in the first pin, 252,698 sections. Of the nine huge-section runs, the caller's first call (for the multi-library run, its nginx call) now prints the fact the caller guessed in all nine, against none on the first pin. The two Flask runs print it in neither: it is in `register_blueprint`'s entry, which the question does not reach. Of the six spec runs that lost facts with no `library`, five now reach the index on their first call (RFC 7636 twice, RFC 6749, RFC 7519, the PKCE question about OAuth 2.1); the OIDC run does not. The OAuth 2.1 draft is not indexed.

**Not fixed here.** Duo's `<span class=…>` leaks are in Duo's own Markdown sources (`duo.com/docs/*.md`, read raw), not in the HTML extractor, which flattens highlighted `<pre>` and has tests for it; a fix belongs to the Markdown-source read (`fetchOne`, and the freshness check's `readMarkdown`). On xml2rfc pages a figure's pilcrow line before a heading is taken as that heading's permalink (`takePermalinkLine`), so RFC 9110's `4. Identifiers in HTTP` cites `#section-3.9-5`.

### Build 1: right source or say so

**What real use showed.** Until this build every round was measured on a benchmark that denies ketch the network, so only the indexed path was tested. Two agents then used a build on topics the benchmark never saw. The indexed path was instant and right. The live path (nothing indexed: search the web, pick a docs site, read pages) answered 8 of 13 questions on the first call, and every failure was about the source. Four classes:

1. A `--library` name the resolver verifies was not used by the question. `--resolve sqlx` lists a crate's docs on docs.rs and a Go module's on pkg.go.dev, trust 8 each; the question refused with four blogs as "documentation sites".
2. A shared host was read as one library. `--library net/http` answered from `library: https://pkg.go.dev/`, every section from a stranger's tutorial repository on that host.
3. A third-party site answered with nothing to tell it from official docs (a tutorial site for a Raspberry Pi question), and the official site's failure said only "not found".
4. A library URL naming one archived man page returned nothing and no reason.

A wrong-source answer looks exactly like a right one. Every rule below is general: none names a site, product or question.

**A. A library name uses what resolve knows.** The cause: `prefs` resolved a name only for a pinned spec (a version or an ecosystem prefix), and even then used the root as a boost among the hits' sites. Now a `--library` name no indexed library matches is resolved beside the web search (within 6 s; the Round 11 rules stand, so a name that matches an indexed library never reaches a registry):

- One verified docs root: that root is read first (from the index if it is indexed, else its own page, its hits and its lists), unless the site the search agrees on is that project's own (below), which then answers.
- Several: the search's leading site settles it when it is the own docs site, not a package index's page, of exactly one candidate (gorm.io is `go:gorm.io/gorm`'s, beside an older module and a namesake crate). Otherwise two or more candidates with a verified root refuse the question, `[validation]`, exit 2, listing each root with its packages (the ecosystem) and title: `ambiguous library "sqlx": 2 libraries match; pass one with --library (CLI) or library (MCP): https://pkg.go.dev/github.com/<owner>/sqlx/ (sqlx package - github.com/<owner>/sqlx - Go Packages; go:github.com/<owner>/sqlx, github:<owner>/sqlx), https://docs.rs/sqlx/latest/sqlx/ (sqlx - Rust; crates:sqlx, github:<org>/sqlx)`. One verified among unverified packages means resolve was not sure: the search answers as before (homebrew: a namesake crate beside two packages with no docs). A language word in the question picks the candidate of its ecosystem (`… in rust`).
- A Go standard-library path of two elements or more (`net/http`, `encoding/json`) is verified by pkg.go.dev's own page at that path (`pkg.go.dev documents net/http in the Go standard library`), beside resolve; `--resolve net/http` lists it too.
- A bare question's first firm reading (a name its syntax puts there; not a guess from joined words, not a common word) is resolved the same way, strictly: an exact package name, its own docs site, trust 5. An ambiguity there is ignored.
- A verified root that gives no text gives way to the search's sites, and the answer's note says `could not read <root> (…), so this answer is from the next site`. Its docs not holding the terms is the answer.

"The project's own site" (`siteOfProject`): at, under or over the verified root; on its registrable domain when that root is not on a shared host; on a domain the project's metadata publishes (homepage, docs URL), its Go module's host, or its repository owner's name (`developer.hashicorp.com` for `github.com/hashicorp/nomad`).

Latency: resolving runs beside the search, so it costs what it takes past the search. Over the 19 `--library` probes below, resolve took a median 1.45 s (p90 3.6 s, max 5.6 s) beside searches of about 1 to 1.5 s; the calls' median went from 2.14 to 2.24 s, the mean from 3.24 to 3.31 s. A repeat answers from the resolution cache. Bare questions: 0 to 0.5 s of strict resolution, median call 2.68 s before, 1.99 s after (the searches differ run to run).

**B. A shared host is not a library.** A host that serves many unrelated projects is read one project at a time, decided by structure, not a list of hosts (`sharedProjectRoot`):

- a path whose first element is itself a host name (a module path: pkg.go.dev, a Go docs mirror, a module proxy) is one project per module: the host and owner and repository on a code forge, else the host and one element (`go.uber.org/zap`);
- a package index's docs host as resolve already knows them (`indexHosted`: docs.rs, pkg.go.dev, pythonhosted.org): on pkg.go.dev a path with no host first is a standard-library package, its whole import path; docs.rs keeps its version segment's per-crate root;
- the library name written as a path (`net/http`) is the root on such a host, and a single name is a project path on any host whose hits sit under two or more different first path elements, unless the name is the host's own label (`react` on react.dev).

Each project root is a site of its own, so strangers' paths neither add weight to it nor get read. `library:` prints the root used: `https://pkg.go.dev/net/http/`. The only list involved is resolve's existing three registry docs hosts; code forges, npm and PyPI were already dropped as never being docs.

**C. Say where the answer came from.** A new frontmatter line on every answer from pages a search read, after `source:`, the same field `origin` in `--json` and MCP. Three values, because a wrong `verified` is worse than no label:

```
origin: verified, docs.rs hosts the docs of crates chrono (resolve, trust 8.0), and the search results agree
origin: verified, pkg.go.dev documents encoding/json in the Go standard library (resolve, trust 8.0)
origin: by name, the domain tmux.test carries the name tmux; nothing else confirms it as the official documentation
origin: by name, docs.rs documents the crates package esp32; the search results favour https://docs.espressif.com/projects/esp-idf/en/stable/
origin: unverified, ketch could not confirm sqlite.org as the official documentation of sqlite
```

- **verified** needs evidence beyond a name, and nothing else counts: registry evidence the search agrees with (the root resolve found and the site the search results agree on are one project, the search lead taken without resolve's boost, by `pickSite` over sites chosen with no resolved root; or a package index's docs of the very package resolve found, `docs.rs/tracing/` for crates' tracing); the project's own site by registry or repository metadata (a domain its homepage, docs URL or Go module path names); a standard-library page of the language's own docs, including a pkg.go.dev page at a path with no host first; the search's agreement settling an ambiguity on one candidate's own site; a library URL the caller passed.
- **by name**: only a name ties the site to what was asked. A registry package of the name (`<host> documents the <ecosystem> package <name>`, and `; the search results favour <root>` when the search agreed on another site; the answer still comes from the resolved root, never labelled verified); a package index's page for a package of that name that resolve did not find; a domain that carries the name, alone or with a descriptor word, for a library value (`fly.io` included) or a question word alike, whatever its address says (`the domain <host> carries the name <name>; nothing else confirms it as the official documentation`); a domain named by the repository owner (`… carries the name of <owner>, owner of github.com/<repo>`). The site is still read when the rules for reading choose it.
- **unverified**: nothing ketch gathered confirms the site. It says only that; the site may well be official.

What was asked is the library value, else the question's first firm reading, else the question. An answer from the index has no origin line: the caller chose that source by indexing it, and the bench check tier's budgets stay as they were.

The last line of a by-name or unverified answer says what to do, never that the source is third-party:

```
origin unverified: ketch could not confirm this site as the official documentation; name the site you cite, or pass the official docs with --library <docs URL>
origin by name: only a name ties this site to what you asked; name the site you cite, or pass the docs you mean with --library https://docs.rs/esp32/latest/esp32/ or --library https://docs.espressif.com/projects/esp-idf/en/stable/
```

MCP `hint` gives the same with `library="…"`, after the cut hint and `; `; `--json` `hint` the CLI's words.

**Python's standard library**, in the Go path's shape: a bare name shaped like a module (`pathlib`, `os.path`) is checked against `docs.python.org/3/library/<name>.html` beside resolve (one page read; its title must open with the name). The root is `https://docs.python.org/3/library/`, the module's page read first, ahead of a PyPI package of the name; a match resolve found in another ecosystem (an npm `uuid`) keeps the name, as does an ambiguity with a verified candidate outside PyPI. `--resolve pathlib` lists it first. A standard-library page is read first even when the search leads with another site of the language's domain (peps.python.org beside docs.python.org, which the same-domain rule would have taken as the project's own).

**Status of a live answer.** Every answer from a search said `partial (partial_site)`, so the word warned about nothing. The reason is gone: a live answer is `found` when its distinctive terms are there, `partial (terms_not_found)` when some are missing, judged as an indexed answer is. The `source: search, N pages read` line and the note (`read N pages of <root> found by web search; the whole site: ketch docs index <root>`) still say where the pages came from. The question cache's version went to `docs-question/v4`, so no read cached by an earlier build (some from a wrong site, some with an older origin) is repeated.

**D. A read failure gives its reason and the next step.** Pages that gave no text are counted per cause: `not found` (404, 410), `blocked` (401, 403, 407, 429, 451, with the status), `need JavaScript` (an unrendered shell), `empty after extraction`, `timed out`, `redirected outside the root`, else `failed` with the status. Each cause names the next step: configure `browser` (blocked, JavaScript), the site's parent tree as `--library` or `ketch docs index` to read its page lists (not found), the address they redirect to (redirected), another docs root. Example: `search: none of the 1 pages of https://www.raspberrypi.com/ could be read: 1 blocked (HTTP 403); the site refuses automated readers: configure browser to retry its pages in a browser, or pass another docs root as --library: not found`.

Causes found:

- **The Raspberry Pi documentation** answers every automated reader with a bot challenge (HTTP 403, `cf-mitigated: challenge`), which was reported as "not found". Not a bug to work around; the cause is now said, and a configured browser is the existing 403 retry.
- **The archived man page**: a library URL naming a page became its directory's root, and only hits under it or the site's lists were read, so with no hit there nothing was planned: "none of the 0 pages … could be read". A library URL that names a page (`isPageFile`) now reads that page first; a root with nothing to read says `found no page of <root> to read: no search hit is under it, and its llms.txt, sitemap and links list none; pass the site's parent … as --library, or ketch docs index …`. The page itself, one `<pre>` block, sections and answers like any other.
- **A moved site's old address** (found in the probes: an old jq manual answering with a script, a canonical link and a meta refresh inside `<noscript>`, read as an unrendered shell): a page under 4 KB with no links but to one place on another host is a redirect stub, and the site is read where it went, after any HTTP redirect there, as an HTTP redirect would have sent it.
- **A code forge's API host** (`api.github.com`) passed as docs because its first label, `api`, exempts a forge's docs host (`docs.github.com`). An API host of a forge is no docs.
- A live read that matched nothing said "nothing in the index of …"; it now says `nothing in the N pages read of <root> matched`.

**E. The stored Context7 default is retired** (decided). `docs_backend: context7` in a config file came from an older default, not a choice. It no longer chooses anything: search is every call's backend, Context7 answers a call that names it (`-b context7`, MCP `backend`). The value stays in the file and is read, never acted on. `ketch config set docs_backend …` exits 2 with `docs_backend is retired: every docs call uses search; ask for Context7 per call with -b context7 (MCP: backend "context7"); a stored value is left in the file and ignored`, and writes nothing; `ketch config` shows `docs_backend: search` and that sentence as `docs_backend_note`; `--backend` defaults to `search` whatever the file says, and `ketch doctor` and the root summary mark search as the default. `KETCH_DOCS_BACKEND` is ignored the same way. Tests cover a config with `context7` stored and one with none.

**Probe set.** 26 questions of our own, none from the benchmark's libraries or the examples above: 19 with `--library` (crates, Go modules, npm, PyPI, three Go and two Python standard-library names, products, hardware) and 7 with none. Run once on `7bbf467` and once on this build, each in its own state directory. "Own docs" is a judgement: the project's or product's own documentation, or a package index's docs for the exact package.

| Probe | Before: source, own | After: source, own | After origin | Status before → after |
|---|---|---|---|---|
| chrono | docs.rs/chrono, yes | docs.rs/chrono, yes | verified | partial → found |
| viper | failed (third-party mirror unreadable) | pkg.go.dev, the module's page, yes | verified | failed → found |
| lodash | lodash.com/docs, yes | lodash.com/docs, yes | verified | partial → found |
| marshmallow | readthedocs, yes | readthedocs, yes | verified | partial → found |
| rayon | docs.rs/rayon, yes | docs.rs/rayon, yes | verified | partial → found |
| boto3 | AWS boto3 docs, yes | AWS boto3 docs, yes | verified | partial → found |
| axios | a GitHub Pages mirror, no | axios.rest (npm's homepage), yes | verified | partial → found |
| gorm | gorm.io/docs, yes | gorm.io/docs, yes | verified (settled ambiguity) | partial → found |
| thiserror | doc.rust-lang.org, no | docs.rs/thiserror, yes | verified | partial → found |
| encoding/json | pkg.go.dev/, mixed projects | pkg.go.dev/encoding/json/, yes | verified | partial → found |
| os/exec | pkg.go.dev/, mixed projects | pkg.go.dev/os/exec/, yes | verified | partial → found |
| asyncio | refused, four junk sites | docs.python.org/3/, yes | verified | refused → found |
| pathlib | docs.python.org/3.13/, yes | pathlib.readthedocs.io, the obsolete PyPI backport, no | verified | partial → found |
| nomad | developer.hashicorp.com, yes | the same, yes | verified | partial → found |
| homebrew | docs.brew.sh, yes | docs.brew.sh, yes | unverified | partial → found |
| jq | refused, ambiguous_site | failed (api.github.com), fixed after the run: jqlang.org/manual, yes | unverified, then verified by the descriptor rule | refused → found |
| ollama | docs.ollama.com, yes | the same, yes | verified | partial → found |
| arduino | docs.arduino.cc, yes | the same, yes | verified | partial (terms) → partial (terms) |
| esp32 | electropeak.com, no | docs.rs/esp32, a Rust crate, no | verified | partial → partial (terms) |
| AirPods reset | support.apple.com, yes | the same, yes | unverified | partial → found |
| Synology SSH | failed (official KB needs JavaScript) | docs.synocommunity.com, no, with a note that the official KB needs JavaScript | unverified | failed → found |
| sqlite json_extract | sqlite.org, yes | the same, yes | unverified | partial → found |
| kotlin launch/async | kotlinlang.org, yes | the same, yes | unverified | partial → found |
| flutter ListView | docs.flutter.dev, yes | api.flutter.dev, yes | verified | partial → found |
| gorm preload | gorm.io/docs, yes | the same, yes | verified | partial → found |
| boto3 presigned url | AWS boto3 docs, yes | the same, yes | verified | partial → found |

Own docs: 17 of 26 before (2 more mixed with strangers' pages, 3 wrong sources, 4 no answer); 22 of 26 after (1 of them the jq rerun), 3 wrong. Origin on that first pass, with two values: 20 verified, 6 unverified; wrong either way: 2 verified but not the docs asked about (pathlib's backport, esp32's crate), 5 unverified that are the official docs.

**Relabelled**, with the three values and Python's standard library: all 26 again in a fresh state directory (the search returns other sites run to run), plus one rerun each of `pathlib` and `encoding/json` after the two fixes the run found (Go's standard-library matches lacked the standard-library mark; a sibling site of python.org displaced the module's page).

| Probe | Source | Own | Origin |
|---|---|---|---|
| chrono, lodash, marshmallow, rayon, boto3, axios, thiserror, ollama, boto3 (bare) | the resolved root | yes | verified, and the search results agree |
| gorm | gorm.io/docs | yes | verified (the search settled the ambiguity) |
| nomad | developer.hashicorp.com | yes | verified (owner's domain) |
| encoding/json, os/exec | pkg.go.dev's package page | yes | verified (Go standard library) |
| asyncio, pathlib | docs.python.org/3/library/, the module's page | yes | verified (Python standard library) |
| viper | pkg.go.dev's module page | yes | by name; the search results favour a third-party primer |
| esp32 | docs.rs's esp32 crate | no | by name; the search results favour docs.espressif.com |
| arduino | docs.arduino.cc | yes | verified (the library name is its domain) |
| flutter, gorm (bare) | api.flutter.dev, gorm.io/docs | yes | verified (the question names the domain) |
| homebrew, sqlite, AirPods | docs.brew.sh, sqlite.org, support.apple.com | yes | unverified, with no third-party advice |
| kotlin | developer.android.com this time | yes (Android's Kotlin docs) | unverified |
| jq | no docs among the hits this run (jqplay.org only) | | refused, no_docs_found |
| Synology SSH | the official KB, every page needs JavaScript | | failed, with the cause; the first pass answered from a community site |

No `verified` answer is from a source other than the one asked about: pathlib now answers from Python's own docs, and esp32 is by name with Espressif's docs named beside it. Of the correct answers that had `verified` from resolve, one lost it: viper (by name; the search led with a third-party primer). jq and Synology gave no answer this run because the search returned other results (one docs hit; a second site no clearer than the third), not because of these changes.

The three examples, once before and once after: `sqlx` is refused with both libraries listed; `net/http` answers from `https://pkg.go.dev/net/http/`, verified; the Raspberry Pi root's error says `blocked (HTTP 403)` and what to do; the archived man page answers from the page.

**Guards.** `go build ./... && go vet ./... && go test ./...` pass. The Round 11 and 13 name tests: wrong 0 on every half. The bench check tier with this build on the second pin: 207 of 291 answered, 221 answered or led, 20 of 30 not covered, 0 cases changed from `baseline-docs-check.json` (the sandbox denies the network, so only the indexed path runs there).

**Rules dropped.**

- The verified root always wins: nomad's verified root is pkg.go.dev's page for the `nomad` command, while the search agreed on HashiCorp's product docs. The project's-own-site test came from that.
- Refuse every ambiguity: gorm and homebrew went from answered to refused; the search's agreement on one candidate's own site, and "one verified among unverified", came from those.
- Refuse only when candidates span ecosystems: gorm's three span Go and a namesake crate.
- Domain naming alone as verification: `raspberry.tips` was verified for a Raspberry Pi question.
- A 10 s resolve budget: chrono once took 9.4 s; 6 s keeps the worst case near the search's own timeouts.
- Resolve's verification alone as `verified`: a registry package that only shares the name (PyPI's obsolete `pathlib`, the `esp32` crate) came out verified. It is `by name` now unless the search agrees.
- Agreement measured on the sites ranked with resolve's boost: the boost makes the resolved root lead the hits it wants to be confirmed by.
- `unverified` advising "present it as a third-party source": sqlite.org, docs.brew.sh and support.apple.com are official.

**Second pass on real use.** Fourteen questions nobody had tuned to, on the committed build: seven clean, seven with four faults.

- **A domain carrying the name was verification.** A third-party site whose domain is the tool's name answered labelled `verified, the question names its domain …`, while official sites without a docs path came out unverified. The rule now gives `by name` whatever the address says, and the docs-address condition is gone; so does the repository owner's name in a domain (nomad's HashiCorp docs are read as before, labelled by name). What stays verified is listed above.
- **The resolved root could not answer and the call refused.** A crate whose homepage is its organisation's site held none of the question's terms, while the origin line said the search favoured the crate's docs.rs page. Now, when the resolved root's pages hold none of the question's distinctive terms (or nothing matched) and the search results favour another site that is not the same project, that site is read in the same call. Its origin is its own evidence, with `; <root>, which resolve found for <name>, holds none of the question's terms, so this answer is from the site the search results favour`. Only when neither holds them is the question refused, the error adding `; nor does <site>, the site the search results favour`. A package index's docs of the very package resolve found now count as that project's (registry evidence the search agrees with), so a lead on them is read first.
- **One-element Go standard-library names.** `goStdRe` takes one element too (`context`, `fmt`, `errors`, `sync`, `time`), verified the same way by pkg.go.dev's page, whose title must end in `Go Packages`. Resolve's answer and the standard-library pages a name is (Go's, Python's) combine in `combineStd`: a page goes ahead of a namesake in its own language's registry (a Go module whose last element is the word, a PyPI-only package); beside a library of another ecosystem, or beside the other language's page (`time`), it is one candidate of an ambiguity, which a language word of the question picks, the search's agreement on the page settles, or else a refusal lists. `--resolve` lists the pages first.
- **A named library got a list of arbitrary sites.** When a library name is given and the search agrees on no site, the refusal lists every library resolve knows by the name, verified or not, with titles and packages: `search: the search results agree on no site for "<name>"; resolve knows it as: <id> (<title>; <packages>), …; pass one as the library (--library)` (`[validation]`, exit 2). Only when resolve knows nothing of the name are the search's sites listed, and no longer as documentation sites: `the search results are split between several sites, none ahead and none tied to what was asked: …; pass the docs you mean as the library (--library <docs URL>)`.
- **A sibling subdomain over the main docs.** Within one registrable domain, a tree on the main host (the domain, `www.`, `docs.`) now goes ahead of one on a sibling subdomain whose first label is no docs host label, when the question or library names the domain and not the sibling, and no version is asked (`mainHostOver` in `treeBetter`). A sibling the question names stays a project of its own (a project on its foundation's subdomain), and a docs subdomain is not displaced by a marketing apex.

Found while measuring, and fixed the same way: a hit on a host that serves many projects, re-rooted after a redirect, took the whole host as its tree (`servedSite` now keeps the project's path); a package index's whole host held one package's path as "the same tree" (`sameTree` excludes it); a standard-library page gave way to a tree over it or to a sibling site of its domain, and agreed only with its own tree, though the search often lands on another version of the same docs (`stdAgrees`: any tree of docs.python.org, the package's own path on pkg.go.dev); a package index's whole host could be "the site the search results favour" (`searchLead` skips it).

**Probes, second pass.** Fourteen new questions, none from the two earlier sets or the examples: a tool's own domain against third-party sites (vim, ffmpeg, rclone, a bare git question), crates and PyPI packages whose resolved root is a project site (actix-web, pytest-asyncio, uvicorn), one-element Go standard-library names (fmt, errors, strings, sync), and names several ecosystems and the Python standard library share (json, logging, yaml). Before is the committed Build 1, after this pass, each in a fresh state directory.

| Probe | Before: source, own | After: source, own | After origin | Status before → after |
|---|---|---|---|---|
| vim | refused, three blogs as "documentation sites" | refused, the same sites as "sites, none tied to what was asked" | | refused → refused |
| ffmpeg | ffmpeg.org, yes | ffmpeg.org, yes | by name (domain) | found → found |
| rclone | rclone.org, yes | rclone.org, yes | by name (owner's domain); was verified | found → found |
| git (bare) | a tutorial site, no | the same, no | unverified | found → found |
| actix-web | actix.rs, yes | actix.rs, yes | by name (crate) | found → found |
| pytest-asyncio | readthedocs, yes | the same, yes | verified, search agrees | found → found |
| uvicorn | uvicorn.dev, yes | the same, yes | verified, search agrees | found → found |
| fmt | docs.rs's `fmt` crate, no | pkg.go.dev/fmt/, yes | verified (Go std) | refused → found |
| errors | refused, sites listed | pkg.go.dev/errors/, yes | verified (Go std) | refused → found |
| strings | learn.microsoft.com, no | refused: Go's `strings` and a crate listed | | found → refused |
| json | docs.python.org, yes | docs.python.org, yes | verified (Python std) | found → found |
| logging | a stranger's Go module, no | docs.python.org, yes | verified (Python std) | found → found |
| yaml | the npm package's site, yes | the same, yes | by name (npm package) | partial → partial |
| sync | refused, sites listed | pkg.go.dev/sync/, yes | verified (Go std) | refused → found |

Own docs: 7 of 14 before (4 wrong sources, 3 refused); 11 of 14 after (1 wrong source, labelled unverified; 2 refused with what is known). No `verified` answer is from another source. `strings` went from a wrong answer to a refusal listing Go's package and a crate, as the question names no language; `sync`'s first run after hit pkg.go.dev's rate limit (HTTP 429) and answered from a tutorial site, labelled unverified with the note `could not read https://pkg.go.dev/sync/ (1 blocked (HTTP 429)), so this answer is from the next site`; the rerun answered from Go's page.

The coordinator's examples, once after: the tmux tutorial domain is `by name`; `zigbee2mqtt.io`, `docs.fly.io` (library `fly.io`) and `htmx.org/docs/` (the main host, not the pre-release subdomain) are `by name`; `context` answers from `pkg.go.dev/context/`, verified; `dotenv` is refused listing the crate, the npm and the PyPI package; `sqlite3` is refused listing Python's module and a crate (no language word, and the search did not land on Python's docs); `tracing` answered `partial` from the crate's homepage (some terms were there, so no fall-over), by name.

**The first 26 under these labels**, from the second pass's run (the rules are the labels' only input that changed; nomad, arduino and sqlite re-run live to confirm): 14 verified (chrono, lodash, marshmallow, rayon, boto3 twice, axios, thiserror, ollama, os/exec, encoding/json, asyncio, pathlib, gorm), 7 by name (viper, esp32, nomad, arduino, sqlite, flutter, gorm with no library), 3 unverified (homebrew, AirPods, kotlin); verified answers that are not the project's own docs: 0.

**Still wrong, or open.**

- A hardware name a crate took (`esp32`) is still answered from the crate, now labelled by name with Espressif's docs named; ketch cannot tell an unrelated namesake from the library meant, only that nothing but the name ties them.
- Official sites named otherwise stay unverified (support.apple.com, kotlinlang.org, docs.brew.sh); those named like the thing are by name, as a fan site is. Telling them apart needs evidence ketch does not gather.
- A name several ecosystems share with no language word refuses (`strings`, `sqlite3`), unless the search lands on one candidate's own docs.
- An answer routed by the search to an indexed library has no origin line; it is the caller's own index.
- Bot challenges (HTTP 403) need a configured browser; nothing else gets through, by design.

## Storage

Agreed on 2026-11-10, the check in point 4 included.

Today a question answered through web search stores the pages it read as a library in the index, marked partial. That library only grows, nothing expires, and it produced both storage bugs below.

1. **A question saves nothing.** Pages read for a question live in the page cache under `cache_ttl` and nowhere else. The search by-product library is removed. A repeat inside the TTL is still fast, because the pages and the site pick come from the cache.
2. **Saving is explicit.** `ketch docs index <site>` is the one command that writes the index. The answer to a question that read pages names that command, as it does today.
3. **An index states its age.** The `index:` line carries the build date on every answer.
4. **An index checks what it cites.** Before printing, any cited page last fetched longer ago than `cache_ttl` is fetched again, at most the handful an answer cites, at once. A changed page is split again and the answer is ranked once more. With no network the answer prints with `stale: unverified, built <date>`. `--offline` skips the check.
5. **A whole refresh stays manual.** `ketch docs index <site>` again, which already re-sections only changed pages. Pages added to a site since the build are not seen until then; the build date is how a caller knows.

**Built** on 2026-11-11 (branch `feat/docs-storage`), with these measurements from live runs: a first question through web search 1.9 to 2.0 s; the same question again with the network blocked 0.02 to 0.08 s, byte-identical; nothing in the index afterwards; the freshness check added about 0.22 s for three cited pages; with the network blocked the answer printed `stale: unverified, built 2026-11-11`.

How the open points were settled:

- **The pages a question reads** are held in an in-memory store with the index's own schema for the length of the call, so Gather, Sort, Rank and Output are the same code as for an indexed library.
- **A repeat** is served from a question entry in the page cache (the docs root and the page list) under `cache_ttl`. A refused question is cached too, so a caller repeating it does not search again.
- **`--section` on a page no index holds** reads it through the page cache and sections it on the fly.
- **The check writes.** It updates `fetched_at`, and re-sections a changed page. So the rule is worded: only `ketch docs index` adds libraries or pages.
- **Same source, or no verdict.** A page is checked the way it was stored. A page stored from Markdown or from an `llms-full.txt` chunk is not replaced by its HTML text; if it cannot be read the same way again, it counts as unverified.
- **A failed check backs off.** A page whose check failed or timed out is not tried again for 15 minutes; answers citing it say `stale: unverified` meanwhile. Otherwise a throttled site would be fetched on every answer.
- **`cached`.** A repeat says so: `source: search, 24 pages read, cached`.
- **Libraries a search once built** are ignored when reading and removed by the next build. No release shipped them.

## QA

A case is a real question with a known answer.

```json
{
  "id": "duo-failmode",
  "question": "pam_duo failmode safe secure when Duo service is unreachable",
  "library": "https://duo.com/docs/",
  "facts": [
    {"quote": "fail \"safe\" (allow access) or \"secure\" (deny access)", "source": "https://duo.com/docs/duounix#duo-configuration-options"}
  ],
  "not_found": [],
  "budget": 1800
}
```

- **Runner.** A development tool, not part of the `ketch` binary. It builds each case's index once in its own HOME, runs the question once, and reports per case and per stage: gather, sort, rank, output.
- **Trace.** The stages report through a hidden `--trace <file>` flag that writes JSON: the terms and counts, the pool's section addresses, the pieces with kind, score and whether chosen, and the status with its reason. The runner reads it. So can a person debugging a bad answer.
- **Leads.** A case may name `leads`: words the first call must print so that the next call is exact, for a question whose words are not the docs' words. A case is answered when its facts are printed, and led when its leads are.
- **Reading cost.** For every case the runner also reports the tokens printed and how many tokens come before the first fact. Fewer is better at the same pass rate; a pass buried under filler is reviewed as a failure.
- **Gate.** Every open case is answered or led at output, the share answered in one call is reported beside it, and the sealed cases are run once before a release.
- **How-to cases**, added 2026-11-10 so that lookups and how-tos pull against each other (Click docs, 38 pages; the old build passes all three):

  | Id | Question | Fact |
  |---|---|---|
  | click-password | click password option hide input | `this can also be replaced with the `password_option()` decorator` |
  | click-confirm | click option prompt with confirmation | `this can also be replaced with the `confirmation_option()` decorator` |
  | click-invoke | click group invoke without command | `pass `invoke_without_command=True` to the group` |
- **First cases**, from the 2026-11-10 run, quotes checked against the page:

  | Id | Question | Fact | `not_found` |
  |---|---|---|---|
  | duo-failmode | pam_duo failmode safe secure when Duo service is unreachable | `fail "safe" (allow access) or "secure" (deny access)` | |
  | duo-fallback | Duo Unix pam_duo PAM configuration pam_sss fallback when duosecurity.com cannot be reached | the same row, and `the configured failmode is triggered`; leads: `failmode` | `pam_sss` |
  | duo-service-err | pam_duo PAM_SERVICE_ERR return value | none | `PAM_SERVICE_ERR` |
  | duo-verified-push | enable Verified Duo Push for Duo Unix | `Set `verified_push=yes` in your local Duo configuration` | |
  | duo-pam-order | where does pam_duo go in the PAM stack | `place `pam_duo` directly after `pam_unix`` | |

- **Baseline**, step 1, 2026-11-10 (`internal/docsqa/cases/duo.baseline.json`), on today's code at 1,800 tokens:

  | Case | Check | Gather | Sort | Rank | Output |
  |---|---|---|---|---|---|
  | duo-failmode | `failmode` row | pass, 35th in the pool | fail | fail | fail |
  | duo-fallback | `failmode` row | pass, 41st | fail | fail | fail |
  | duo-fallback | `https_timeout` row | pass, 41st | fail | fail | fail |
  | duo-fallback | `not_found: pam_sss` | | | | fail |
  | duo-service-err | `not_found: PAM_SERVICE_ERR` | | | | fail |
  | duo-verified-push | `verified_push=yes` | pass, 1st | pass | pass | pass |
  | duo-pam-order | after `pam_unix` | pass, 2nd | pass | pass | pass |
  | Totals | | 5 of 5 | 2 of 5 | 2 of 5 | 2 of 7 |

  Each answer took 90 to 160 ms. The options table, had it been split at all, is one piece of 2,031 tokens against a budget of 1,800.
- **Broad open set.** The gold cases from earlier rounds in `ketch-bench` (`docs/cases.json`, 205 cases, and `docs/general/cases-dev.json`, 66), which carry a source and exact quotes per required fact. They were used to tune site and page picking, so they are open, never sealed; they are run with `--library` on a built index, so they measure Sort, Rank and Output only. Rules are reported on this set, not tuned to single cases in it.
- **Broad baseline**, 2026-11-10: 82 cases on 34 sites (77 with facts, 5 `not_found` controls), indexes capped at 400 pages, in `/tmp/ketch-evidence-broad`. Results in `internal/docsqa/cases/broad.*.json`.

  | Build | Answered | Median tokens printed | Median tokens before the first fact | Gather | Sort | Rank |
  |---|---|---|---|---|---|---|
  | old | 42 of 77 | 1,694 | 211 | 137 of 141 | 105 of 141 | 92 of 141 |
  | round 1 | 40 of 77 | 1,589 | 240 | 136 of 141 | 135 of 141 | 88 of 141 |
  | round 2 | 28 of 77 | 858 | 118 | 136 of 141 | 138 of 141 | 69 of 141 |

  Gather and Sort are nearly complete; Rank is where answers are lost. Round 2 lost 19 cases the old build answers, 17 of them to its stop on discounted gain, 13 with the answer among its five best pieces. The old build answers 55% at the default budget: that is the number to beat, at fewer tokens.
- **Runner fairness**, to fix before the next comparison: strip Markdown emphasis and link syntax when comparing quotes (14 good cases were dropped for `*memoized*` against `memoized`); pass the gather check when any pool section holds the quote, not only the first section found.
- **Next round.** After this round passes: cases across platforms and kinds of documentation, not only code and not only Duo. Each is built backwards from a fact read on the page in a browser, never through ketch, with the question worded as a person would ask it. Half are sealed outside the repository. Some need two or three sources and are graded per call.

## Bugs

Found in the same run. The first two go away with Storage.

- `http://duo.com/docs/` and `https://duo.com/docs/` are stored as two libraries.
- A bare question answers from the 41-page partial library when the full 734-page one exists.
- A printed `full section:` reference failed: `has no section #duo-configuration-options (sections: none)`.
- `--library https://duo.com/docs/duounix` did not scope to that page.
- `--section <page URL>` returned one section, not the page's sections in order.
- `<span class="toreplace">` and `<br></br>` inside code blocks (Sort cleans these).
- About a dozen `appears JS-rendered` warnings per search (Output folds these).

Found building the broad set:

- Choosing stops at the first piece that does not fit the budget instead of passing over it: 17 of 82 answers stopped that way, one at 272 tokens behind a 1,514-token code block.
- A page of one line per entry is one piece: nginx's directive index is a single 5,614-token "paragraph". Lines of such a block are pieces.
- An index can be built with no page text: docker's 400 sections hold only the `llms.txt` descriptions, and most of pydantic AI's pages from `llms-full.txt` likewise. A build like that should fail or fall back to fetching the pages.
- With an `llms.txt` present, docs.github.com indexes only the 7 pages it lists, though the sitemap has more.
- The stemmer turns `stripe` into `strip` and `title` into `titl`, and `query_cache_size` becomes the single term `querycaches`.

## Order

1. Trace, runner and the first cases, with a baseline. Done 2026-11-10.
2. Sort.
3. Rank. Built with Sort: today only ranked sections are ever split, so neither can be measured alone.
4. Output.
5. Storage.
6. The remaining bugs.

Each step reports the stage pass rates before and after, and the existing docs tests. Golden output in those tests changes with Output; that is expected and reviewed, not suppressed.
