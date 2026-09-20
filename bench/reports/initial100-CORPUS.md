# Corpus: 100 pages, 50 sites

Historical 100-page snapshot; the current corpus and commands are documented in [the benchmark guide](../README.md).

The benchmark freezes public HTML and source-derived reference text. Every URL below is a distinct saved page; original HTTP bytes and reference bytes have separate SHA256 hashes in [corpus.json](initial100-corpus.json). All 692 assertions pass source-evidence validation. This establishes provenance, not semantic correctness of either the source or the extractor.

## Sampling and review

- **20 regression pages:** original snapshots, reference files and 115 assertions preserved.
- **60 expansion pages:** two extra pages from each original site and two pages from each of ten additional sites.
- **20 evaluation pages:** one each from twenty further sites, absent from the other cohorts. Expectations were frozen before running either binary; extractor code was not changed during expansion.
- **24 content types:** accessibility, api, blog, book, catalogue, configuration, data-article, database, discussion, docs, encyclopedia, essay, explainer, guide, health, historical-document, history, manual, recipe, repair-guide, science, standard, tutorial, visitor-guide.

The source review checked content roots, start/middle/end anchors, important conditions, code whitespace, selected table rows, links and navigation exclusions. Assertions include 380 critical text/code/table checks. Additional numerical checks cover recipe quantities, oven temperatures and resting/baking times. Broad text coverage comes from the complete annotated source region, not just the assertion snippets.

**Annotation status: agent-curated and agent-reviewed; independent human audit pending.** These are diagnostic regression fixtures, not a representative sample of the entire web or a certified gold standard. Site choices are intentionally varied but convenience-sampled from accessible public pages. The three-page sites receive three times the weight of the one-page sites in the overall page average. Reporting by cohort makes that visible.

The evaluation cohort is held out from the earlier extraction work and this expansion made no extractor changes. Once outputs have been inspected, these pages are regression evidence. Re-running them does not create a fresh blind evaluation.

## Source scope decisions

- Keep substantive asides: King Arthur ingredients, Fowler architectural sidebars and NPS guidance.
- Keep article disclosures and accordion content: NPS and BGS seismic tables. Button markup alone is not evidence of unwanted prose.
- Keep source code: Docker code wrappers use `not-prose`; only non-code control wrappers were excluded. npm code assertions preserve the actual logical lines in its syntax-highlighter markup.
- Keep authored footnotes and data/source notes. Remove duplicated inline Fowler footnote popups while retaining the footnote list.
- Keep all discussion posts/comments present in the captured Discourse and Hacker News HTML. These are snapshot contents, not a guarantee of the entire historical thread.
- Keep full narrative sections for Gutenberg. The Yellow Wallpaper has paragraphs directly under `body`; the reference includes those roots themselves.
- Treat icons, charts, image/video pixels, browser interaction and hydrated content as outside this HTML text track. Math representations and complex spanning-table semantics are not fully normalized or graded; selected table checks cover literal headers and row associations.

The reference and assertion review finished before the first new-corpus extraction run. [Freeze record](corpus100-freeze.json) records its hash. Default extraction receives only raw HTML and source URL. Reference selectors/exclusions are annotation data; assisted selector mode uses the root selector and is reported separately.

## Capture limitations

Three first-choice sites returned HTTP 403 (CDC, Smithsonian and Library of Congress), USGS timed out and then returned HTTP 504, and the initial Natural History Museum URL returned HTTP 404. Accessible replacements were chosen before scoring. Two DigitalOcean HTTP 200 snapshots lacked the article in their server HTML, so Fowler articles replaced them for this HTTP-only track. These outcomes remain recorded in [capture100.json](capture100.json). They do not count as extraction failures or successes. The iFixit URL resolved to a different repair guide; the fixture records the actual moped guide and final URL.

No captured page was discarded because its extraction score was poor. Existing omissions, such as NPS disclosure content, remain in the benchmark.

## Page inventory

| ID / source | Site | Content type | Cohort | Checks | Critical |
| --- | --- | --- | --- | ---: | ---: |
| [npm](https://docs.npmjs.com/trusted-publishers/) | docs.npmjs.com | docs | regression | 6 | 4 |
| [go](https://pkg.go.dev/context) | pkg.go.dev | api | regression | 7 | 5 |
| [python](https://docs.python.org/3/tutorial/errors.html) | docs.python.org | tutorial | regression | 6 | 4 |
| [kubernetes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) | kubernetes.io | configuration | regression | 6 | 4 |
| [postgres](https://www.postgresql.org/docs/current/transaction-iso.html) | www.postgresql.org | database | regression | 6 | 4 |
| [mdn](https://developer.mozilla.org/en-US/docs/Web/API/AbortController) | developer.mozilla.org | api | regression | 7 | 4 |
| [rust](https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html) | doc.rust-lang.org | tutorial | regression | 6 | 4 |
| [sqlite](https://www.sqlite.org/lang_transaction.html) | www.sqlite.org | database | regression | 6 | 3 |
| [git](https://git-scm.com/docs/git-reset) | git-scm.com | manual | regression | 6 | 4 |
| [gnu](https://www.gnu.org/software/coreutils/manual/html_node/Exit-status.html) | www.gnu.org | manual | regression | 5 | 3 |
| [terraform](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle) | developer.hashicorp.com | configuration | regression | 5 | 4 |
| [docker](https://docs.docker.com/build/building/multi-stage/) | docs.docker.com | tutorial | regression | 5 | 4 |
| [cloudflare](https://blog.cloudflare.com/rfc-9457-agent-error-pages/) | blog.cloudflare.com | blog | regression | 7 | 5 |
| [danluu](https://danluu.com/slow-device/) | danluu.com | essay | regression | 5 | 3 |
| [joel](https://www.joelonsoftware.com/2000/04/06/things-you-should-never-do-part-i/) | www.joelonsoftware.com | blog | regression | 4 | 4 |
| [rfc](https://www.rfc-editor.org/rfc/rfc8259.html) | www.rfc-editor.org | standard | regression | 4 | 4 |
| [wikipedia](https://en.wikipedia.org/wiki/HTTP) | en.wikipedia.org | encyclopedia | regression | 6 | 4 |
| [gutenberg](https://www.gutenberg.org/files/11/11-h/11-h.htm) | www.gutenberg.org | book | regression | 4 | 3 |
| [nasa](https://science.nasa.gov/mars/facts/) | science.nasa.gov | science | regression | 5 | 4 |
| [nps](https://www.nps.gov/yell/planyourvisit/safety.htm) | www.nps.gov | visitor-guide | regression | 9 | 5 |
| [npm-scripts](https://docs.npmjs.com/cli/v11/using-npm/scripts/) | docs.npmjs.com | docs | expansion | 8 | 4 |
| [npm-package-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-json/) | docs.npmjs.com | docs | expansion | 8 | 4 |
| [go-io](https://pkg.go.dev/io) | pkg.go.dev | api | expansion | 8 | 4 |
| [go-strings](https://pkg.go.dev/strings) | pkg.go.dev | api | expansion | 8 | 4 |
| [python-data](https://docs.python.org/3/tutorial/datastructures.html) | docs.python.org | tutorial | expansion | 8 | 4 |
| [python-classes](https://docs.python.org/3/tutorial/classes.html) | docs.python.org | tutorial | expansion | 8 | 4 |
| [kubernetes-deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) | kubernetes.io | configuration | expansion | 8 | 4 |
| [kubernetes-configmaps](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/) | kubernetes.io | configuration | expansion | 8 | 4 |
| [postgres-constraints](https://www.postgresql.org/docs/current/ddl-constraints.html) | www.postgresql.org | database | expansion | 8 | 4 |
| [postgres-queries](https://www.postgresql.org/docs/current/queries-table-expressions.html) | www.postgresql.org | database | expansion | 8 | 4 |
| [mdn-promise](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Promise) | developer.mozilla.org | api | expansion | 9 | 5 |
| [mdn-fetch](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch) | developer.mozilla.org | api | expansion | 8 | 4 |
| [rust-enums](https://doc.rust-lang.org/book/ch06-01-defining-an-enum.html) | doc.rust-lang.org | tutorial | expansion | 7 | 4 |
| [rust-result](https://doc.rust-lang.org/book/ch09-02-recoverable-errors-with-result.html) | doc.rust-lang.org | tutorial | expansion | 7 | 4 |
| [sqlite-select](https://www.sqlite.org/lang_select.html) | www.sqlite.org | database | expansion | 7 | 4 |
| [sqlite-foreignkeys](https://www.sqlite.org/foreignkeys.html) | www.sqlite.org | database | expansion | 7 | 4 |
| [git-rebase](https://git-scm.com/docs/git-rebase) | git-scm.com | manual | expansion | 8 | 4 |
| [git-restore](https://git-scm.com/docs/git-restore) | git-scm.com | manual | expansion | 8 | 4 |
| [gnu-copy](https://www.gnu.org/software/coreutils/manual/html_node/cp-invocation.html) | www.gnu.org | manual | expansion | 6 | 4 |
| [gnu-timeout](https://www.gnu.org/software/coreutils/manual/html_node/timeout-invocation.html) | www.gnu.org | manual | expansion | 6 | 4 |
| [terraform-foreach](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each) | developer.hashicorp.com | configuration | expansion | 8 | 4 |
| [terraform-count](https://developer.hashicorp.com/terraform/language/meta-arguments/count) | developer.hashicorp.com | configuration | expansion | 8 | 4 |
| [docker-volumes](https://docs.docker.com/engine/storage/volumes/) | docs.docker.com | docs | expansion | 9 | 5 |
| [docker-bridge](https://docs.docker.com/engine/network/drivers/bridge/) | docs.docker.com | docs | expansion | 9 | 5 |
| [cloudflare-pingora](https://blog.cloudflare.com/how-we-built-pingora-the-proxy-that-connects-cloudflare-to-the-internet/) | blog.cloudflare.com | blog | expansion | 7 | 3 |
| [cloudflare-opensource](https://blog.cloudflare.com/pingora-open-source/) | blog.cloudflare.com | blog | expansion | 8 | 4 |
| [danluu-branch](https://danluu.com/branch-prediction/) | danluu.com | essay | expansion | 8 | 4 |
| [danluu-malloc](https://danluu.com/malloc-tutorial/) | danluu.com | essay | expansion | 6 | 4 |
| [joel-test](https://www.joelonsoftware.com/2000/08/09/the-joel-test-12-steps-to-better-code/) | www.joelonsoftware.com | blog | expansion | 5 | 3 |
| [joel-leaky](https://www.joelonsoftware.com/2002/11/11/the-law-of-leaky-abstractions/) | www.joelonsoftware.com | blog | expansion | 5 | 3 |
| [rfc-http](https://www.rfc-editor.org/rfc/rfc9110.html) | www.rfc-editor.org | standard | expansion | 7 | 4 |
| [rfc-uri](https://www.rfc-editor.org/rfc/rfc3986.html) | www.rfc-editor.org | standard | expansion | 5 | 4 |
| [wikipedia-photosynthesis](https://en.wikipedia.org/wiki/Photosynthesis) | en.wikipedia.org | encyclopedia | expansion | 8 | 4 |
| [wikipedia-binary](https://en.wikipedia.org/wiki/Binary_search) | en.wikipedia.org | encyclopedia | expansion | 8 | 4 |
| [gutenberg-wallpaper](https://www.gutenberg.org/files/1952/1952-h/1952-h.htm) | www.gutenberg.org | book | expansion | 4 | 3 |
| [gutenberg-metamorphosis](https://www.gutenberg.org/files/5200/5200-h/5200-h.htm) | www.gutenberg.org | book | expansion | 3 | 3 |
| [nasa-earth](https://science.nasa.gov/earth/facts/) | science.nasa.gov | science | expansion | 7 | 3 |
| [nasa-jupiter](https://science.nasa.gov/jupiter/jupiter-facts/) | science.nasa.gov | science | expansion | 7 | 3 |
| [nps-hiking](https://www.nps.gov/grca/planyourvisit/hiking-faq.htm) | www.nps.gov | visitor-guide | expansion | 6 | 3 |
| [nps-canyon](https://www.nps.gov/grca/faqs.htm) | www.nps.gov | visitor-guide | expansion | 7 | 3 |
| [pandas-indexing](https://pandas.pydata.org/docs/user_guide/indexing.html) | pandas.pydata.org | api | expansion | 9 | 5 |
| [pandas-merge](https://pandas.pydata.org/docs/user_guide/merging.html) | pandas.pydata.org | api | expansion | 9 | 5 |
| [django-queries](https://docs.djangoproject.com/en/5.2/topics/db/queries/) | docs.djangoproject.com | docs | expansion | 8 | 4 |
| [django-transactions](https://docs.djangoproject.com/en/5.2/topics/db/transactions/) | docs.djangoproject.com | docs | expansion | 8 | 4 |
| [curl-manpage](https://curl.se/docs/manpage.html) | curl.se | manual | expansion | 8 | 4 |
| [curl-http](https://curl.se/docs/httpscripting.html) | curl.se | manual | expansion | 8 | 4 |
| [w3c-tables](https://www.w3.org/WAI/tutorials/tables/one-header/) | www.w3.org | accessibility | expansion | 9 | 5 |
| [w3c-irregular](https://www.w3.org/WAI/tutorials/tables/irregular/) | www.w3.org | accessibility | expansion | 8 | 4 |
| [owid-life](https://ourworldindata.org/life-expectancy) | ourworldindata.org | data-article | expansion | 8 | 4 |
| [owid-population](https://ourworldindata.org/population-growth) | ourworldindata.org | data-article | expansion | 8 | 4 |
| [eff-passwords](https://ssd.eff.org/module/creating-strong-passwords) | ssd.eff.org | guide | expansion | 7 | 3 |
| [eff-plan](https://ssd.eff.org/module/your-security-plan) | ssd.eff.org | guide | expansion | 7 | 3 |
| [paulgraham-lisp](https://www.paulgraham.com/avg.html) | www.paulgraham.com | essay | expansion | 4 | 3 |
| [paulgraham-schedule](https://www.paulgraham.com/makersschedule.html) | www.paulgraham.com | essay | expansion | 4 | 3 |
| [fowler-microservices](https://martinfowler.com/articles/microservices.html) | martinfowler.com | essay | expansion | 7 | 3 |
| [fowler-debt](https://martinfowler.com/bliki/TechnicalDebt.html) | martinfowler.com | essay | expansion | 7 | 3 |
| [letsencrypt-challenges](https://letsencrypt.org/docs/challenge-types/) | letsencrypt.org | docs | expansion | 5 | 3 |
| [letsencrypt-compatibility](https://letsencrypt.org/docs/certificate-compatibility/) | letsencrypt.org | docs | expansion | 5 | 3 |
| [noaa-salt](https://oceanservice.noaa.gov/facts/whysalty.html) | oceanservice.noaa.gov | science | expansion | 6 | 3 |
| [noaa-blue](https://oceanservice.noaa.gov/facts/oceanblue.html) | oceanservice.noaa.gov | science | expansion | 6 | 3 |
| [bgs-earthquakes](https://www.bgs.ac.uk/discovering-geology/earth-hazards/earthquakes/how-are-earthquakes-detected/) | www.bgs.ac.uk | science | heldout | 8 | 4 |
| [medlineplus-handwashing](https://medlineplus.gov/ency/patientinstructions/000972.htm) | medlineplus.gov | health | heldout | 7 | 3 |
| [who-resistance](https://www.who.int/news-room/fact-sheets/detail/antimicrobial-resistance) | www.who.int | health | heldout | 7 | 3 |
| [un-climate](https://www.un.org/en/climatechange/what-is-climate-change) | www.un.org | explainer | heldout | 7 | 3 |
| [computerhistory-1969](https://www.computerhistory.org/timeline/1969/) | www.computerhistory.org | history | heldout | 7 | 3 |
| [nhm-meteorites](https://www.nhm.ac.uk/discover/types-of-meteorites.html) | www.nhm.ac.uk | science | heldout | 7 | 3 |
| [standardebooks-austen](https://standardebooks.org/ebooks/jane-austen/pride-and-prejudice) | standardebooks.org | catalogue | heldout | 7 | 3 |
| [archives-declaration](https://www.archives.gov/founding-docs/declaration-transcript) | www.archives.gov | historical-document | heldout | 5 | 3 |
| [wikivoyage-paris](https://en.wikivoyage.org/wiki/Paris) | en.wikivoyage.org | visitor-guide | heldout | 8 | 4 |
| [kingarthur-bread](https://www.kingarthurbaking.com/recipes/classic-sandwich-bread-recipe) | www.kingarthurbaking.com | recipe | heldout | 10 | 6 |
| [seriouseats-cookies](https://www.seriouseats.com/the-food-lab-best-chocolate-chip-cookie-recipe) | www.seriouseats.com | recipe | heldout | 9 | 5 |
| [ifixit-moped](https://www.ifixit.com/Guide/Suzuki+FA50+Moped+Exhaust+Cleaning/3926) | www.ifixit.com | repair-guide | heldout | 7 | 3 |
| [discourse-guide](https://meta.discourse.org/t/understanding-discourse-for-new-users/96331) | meta.discourse.org | discussion | heldout | 8 | 4 |
| [hackernews-startup](https://news.ycombinator.com/item?id=8863) | news.ycombinator.com | discussion | heldout | 5 | 3 |
| [julia-arrays](https://docs.julialang.org/en/v1/manual/arrays/) | docs.julialang.org | manual | heldout | 9 | 5 |
| [elixir-enum](https://elixir.hexdocs.pm/enum-cheat.html) | elixir.hexdocs.pm | api | heldout | 8 | 4 |
| [ffmpeg-filters](https://ffmpeg.org/ffmpeg-filters.html) | ffmpeg.org | manual | heldout | 7 | 4 |
| [angular-signals](https://angular.dev/guide/signals) | angular.dev | docs | heldout | 8 | 4 |
| [react-state](https://react.dev/learn/state-a-components-memory) | react.dev | tutorial | heldout | 8 | 4 |
| [ncat-guide](https://nmap.org/ncat/guide/index.html) | nmap.org | manual | heldout | 6 | 3 |

## Rights and storage

Source HTML/text retains its original rights, notices and attribution. It is not relicensed under the repository MIT license. Captures remain unchanged; only the benchmark references exclude navigation and controls.

The combined uncompressed HTML/reference payload is 30,151,913 bytes. The runner rejects more than 128 MiB of aggregate payload, duplicate final URLs (ignoring fragments), duplicate HTML, and nested/overlapping reference roots. Runtime DOM/scoring/subprocess memory is additional.
