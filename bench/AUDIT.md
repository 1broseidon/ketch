# Live extraction audit: Ketch versus curl

Audited September 19, 2026. Seven URLs were chosen from known benchmark failures.
This is a targeted diagnostic sample, not an estimate of general extraction quality.

The live comparison confirms four clear content-selection failures and one clear
clutter failure. It also corrects the earlier interpretation of the Redis lists
benchmark: the language examples survive; the reference scope needs review.

## Method and binary identity

The main binary is the exact candidate from the 500-page benchmark:
`/tmp/ketch-bench500/ketch-current`, SHA256
`53432e7ddd71749efa89f0b798a8709744c19f2ad0feaa0e2539eb4bccd6ba75`.
The installed `~/.local/bin/ketch` is the older pre-fix build,
SHA256 `4b1ec2e0925b22b0cc68002dd843ecb54930ebc85238d83e4f9a9a9b9905ccd6`.
It was also run against curl's saved HTML without replacing the installation.

For each URL, capture curl HTML, Ketch raw HTTP HTML and Ketch's default Markdown.
Replay each successful response through the actual `ketch extract` binary, then
use the benchmark's content selector as an assisted diagnostic. Responses are
untruncated and saved to files. Temporary config/cache/tag paths isolate the
operator's state; cache, llms.txt, cookies and browser configuration are absent.
No network request requires JavaScript rendering for the demonstrated content.

Five URLs returned HTTP 200 to both tools. For four, curl and Ketch raw HTML were
byte-identical. Redis varied by 805 bytes, but produced identical Markdown.
**For all five, curl → ketch extract was byte-identical to live ketch scrape.**
Ketch raw → ketch extract also matched live output for all six Ketch successes.
That separates these content failures from fetching, caches and truncation.
Single-request timings are preserved as observations, not a speed comparison.

## What the reader actually receives

| Page | In the fetched source | Default Ketch output | Assisted diagnosis |
| --- | --- | --- | --- |
| [Yellowstone safety](https://www.nps.gov/yell/planyourvisit/safety.htm) | Emergency callout, thermal/wildlife warnings and expandable guidance | 934 bytes: video keyboard-help link, related links and a short summary; all five sampled critical passages missing | `.ColumnMain` yields 25,916 bytes and recovers all five passages, but also video/embed controls |
| [Git rebase manual](https://git-scm.com/docs/git-rebase) | Complete manual, synopsis, explanations and 37 code blocks | 20,665 bytes focused on options; zero headings and two code blocks. Starts mid-manual at `--onto`; all four critical checks' text missing | `#main` yields 68,216 bytes, all 37 blocks and all four sampled passages |
| [React state tutorial](https://react.dev/learn/state-a-components-memory) | Introduction, explanations, warnings, recap/challenges and 12 code blocks | 8,082 bytes consisting of one long implementation example, including its sculpture dataset; zero headings and all four sampled critical passages missing | `main` yields 25,151 bytes, all 12 blocks and all four passages |
| [W3C decorative images](https://www.w3.org/WAI/tutorials/images/decorative/) | Introduction and five HTML code examples | 1,557 bytes of selected explanatory prose and headings; no code blocks. Three of four sampled critical passages missing | `main` yields 6,485 bytes and all five blocks, with extra page-maintenance text |
| [AllRecipes banana bread](https://www.allrecipes.com/recipe/20144/banana-banana-bread/) | Complete recipe in Ketch's raw response; curl received 403 | 30,733 bytes: recipe retained alongside site menus, search validation messages, promotions, repeated signup prompts and cookie notice | `main > article` reduces output to 11,781 bytes and keeps sampled recipe facts; photo/review controls remain |
| [Redis lists](https://redis.io/docs/latest/develop/data-types/lists/) | Tutorial, language panels, full-source templates and hundreds of auxiliary API-signature panels | 199,845 bytes; sampled prose and all 191 code-block texts survive. Low benchmark coverage cannot be interpreted as lost language examples | Broad selection grows output to 654,340 bytes; it also admits a large amount of auxiliary detail |

Word counts used in this manual inspection use whitespace splitting, not the
benchmark's tokenizer. Byte counts show output volume, not semantic accuracy.
Phrase presence is checked in parsed Markdown text; code-format fidelity is
reported separately below. Selecting a root demonstrates recovery but does not
by itself establish clean output.

## Redis correction and benchmark implications

Both the Ketch raw HTML and default Markdown for Redis match the pinned benchmark
hashes. This is therefore a correction to the interpretation of that recorded case,
not an explanation based on a changed webpage.

- All **191 source code-block texts** and their multiplicities are preserved after
  whitespace normalization. There are 186 distinct block texts.
- **188/191** also match with indentation and internal newlines preserved, allowing
  only CRLF and outer-newline normalization. The three formatting differences are
  in full-file examples inside HTML templates. All 180 non-template blocks match
  under the stricter comparison.
- The annotated source region includes **497 hidden API-signature panels**, with
  only 111 distinct normalized texts; one panel repeats 22 times. These panels
  contribute heavily to reference word counts. Ketch removes much of that detail.
- The reference excludes the 11 inert full-source templates, yet their code appears
  in Ketch's output. This contributes output outside the declared reference scope.

The earlier statement that the Redis lists score demonstrated missing language
examples was incorrect. The 34.43% recall / 63.83% precision result still describes
word overlap against the existing reference, but does not directly measure how
much of the useful tutorial is missing. Decide which auxiliary panels and complete
examples belong in the desired product output, then independently review the
annotations. Other Redis pages were not manually audited here. **No reference,
assertion, baseline or benchmark score was changed.**

## Access failures and reproducibility limits

[Red Cross flood safety](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/flood.html)
returned HTTP 403 through both curl and Ketch. Its older benchmark capture was not
presented as a fresh successful fetch. AllRecipes returned 403 to curl while
Ketch succeeded; the extraction observations use Ketch's saved HTTP body. These
are fetch/access differences, separate from the demonstrated extraction losses.
No attempt was made to bypass a restriction or change authentication.

The installed older binary reproduces the content-selection failures on curl's
five successful pages. React and Redis Markdown differ from the candidate's in
code formatting; the known selection problems remain. No software was installed
or replaced during this audit.

## Next changes supported by this evidence

1. Recover complete article/manual/tutorial sections, including important
   introductory and disclosure content. The same-source selector recoveries give
   concrete before/after examples across unrelated sites.
2. Keep recovery within the intended content region and explicitly remove controls
   and promotion blocks. AllRecipes shows why a full-page conversion can preserve
   facts while producing a poor reading experience.
3. Review reference boundaries for auxiliary panels, templates and duplicate text
   before optimizing word-overlap scores. Redis is the concrete case requiring
   that review; removing real clutter must not be scored as loss of critical code.

## Evidence and replay

[Machine-readable observations](reports/live-audit500.json) contain exact command
arguments, statuses, timings, hashes, source/Markdown inventories and passage checks.
Full HTTP responses, Markdown, comparison scripts and DOM inspection artifacts are
in the ignored [local audit directory](.runs/live-audit-20260919/).

With temporary config/cache/tag environment variables set as recorded in the JSON:

```sh
# Save the entire HTTP response. Do not pipe truncated content into this test.
curl -L --compressed --max-time 30 -o page.html https://git-scm.com/docs/git-rebase

# Feed identical bytes to default and assisted extraction.
/tmp/ketch-bench500/ketch-current extract --url https://git-scm.com/docs/git-rebase --json < page.html
/tmp/ketch-bench500/ketch-current extract --url https://git-scm.com/docs/git-rebase --select '#main' --json < page.html

# Compare with a normal live fetch, bypassing the page cache.
/tmp/ketch-bench500/ketch-current scrape https://git-scm.com/docs/git-rebase --no-cache --no-llms-txt --json
```

No production code, pinned corpus, annotation or baseline was changed. No release
was cut. The audit report corrects interpretation while preserving the original
measurement evidence.
