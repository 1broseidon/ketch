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

Frontmatter carries the verdict. The body carries the evidence, verbatim.

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

## Storage

Agreed on 2026-11-10, the check in point 4 included.

Today a question answered through web search stores the pages it read as a library in the index, marked partial. That library only grows, nothing expires, and it produced both storage bugs below.

1. **A question saves nothing.** Pages read for a question live in the page cache under `cache_ttl` and nowhere else. The search by-product library is removed. A repeat inside the TTL is still fast, because the pages and the site pick come from the cache.
2. **Saving is explicit.** `ketch docs index <site>` is the one command that writes the index. The answer to a question that read pages names that command, as it does today.
3. **An index states its age.** The `index:` line carries the build date on every answer.
4. **An index checks what it cites.** Before printing, any cited page last fetched longer ago than `cache_ttl` is fetched again, at most the handful an answer cites, at once. A changed page is split again and the answer is ranked once more. With no network the answer prints with `stale: unverified, built <date>`. `--offline` skips the check.
5. **A whole refresh stays manual.** `ketch docs index <site>` again, which already re-sections only changed pages. Pages added to a site since the build are not seen until then; the build date is how a caller knows.

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
