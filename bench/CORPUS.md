# Corpus: 500 pages across 70 sites

This is a pinned regression corpus for **extraction content fidelity**. It contains
28 content types, 3,573 source-backed assertions and 1,831 critical checks. Every
page has a distinct final URL and HTML hash. It is not a random or representative
sample of the web; documentation and English technical prose remain prominent.

## Selection and review

The original 100 records, HTML snapshots, references and assertions are unchanged.
The 400 additions comprise 320 pages from 40 existing sites and 80 pages from
20 new sites. Most new sites contribute four pages; NHS contributes six, and
Britannica and FDA contribute three each. Original cohort labels describe the
historical 100-page evaluation; their sites may now also appear in `scale`.
`scale-new-sites` has no host overlap with the original 100.

Initial source scopes were reviewed before evaluating either extractor binary. Reference
text comes from the selected source DOM after explicit exclusions. A separate
source inspection script proposed opening/middle/closing prose anchors, headings,
preformatted examples, links, simple table associations and navigation exclusions.
Those candidates were reviewed against the source and validated before the initial
freeze; the eight later scope corrections are recorded below.
This is **agent-reviewed, mechanically assisted annotation**, without an independent
human gold audit. Three prose anchors do not certify every fact on a long page.

The [freeze record](reports/corpus500-freeze.json) pins the manifest and unchanged
candidate binary. Default extraction sees only the original HTML and source URL.
Selectors and exclusions are oracle annotation data; only the separately reported
assisted mode receives the selector. An [eight-page annotation erratum](reports/annotation-errata500.json) followed
the provisional run: photo-gallery reader comments/controls and newsletter copy
had not been excluded as intended. Corrected references and affected checks were
reviewed against the unchanged source, then both binaries were rerun. Provisional
results and the eight old references are retained; no page was dropped. This
post-evaluation oracle repair means this is not an untouched blind experiment.
After inspection, these evaluation pages become regression evidence, not a fresh
blind test set.

## Capture accounting

[Capture observations](reports/capture500.json) retain 476 distinct observations:
400 selected sources, 61 HTTP failures, and 15 successful responses replaced
before scoring. Repeated identical observations are collapsed; this is not a
request-count or network-availability benchmark. The public HTTP capture used four
workers, serial requests per site with 0.6-second spacing, a 20-second timeout and
a 20 MiB per-response bound. No browser, credentials, real cache or bot-defense
bypass was used.

Replacements include malformed/obsolete URLs, an NPS page-in-progress response,
Angular homepage redirects, an OWID graph page, a duplicate Pingora URL, short
link/PDF hubs and a World History disambiguation page. Inaccessible energy.gov and
open.edu sources were replaced with recipe and consumer content. These source
selection decisions preceded extraction evaluation; low-scoring pages were retained.
The eight later annotation corrections are accounted for separately above.

## Scope decisions worth auditing

- Elixir references include the full API: introduction, summary, types, functions
  and examples. `#top-content` alone would silently omit most of a module.
- Redis references retain every code language panel, including initially hidden
  examples. Tabs, sidebar and feedback controls are excluded. This creates large
  source cases rather than rewarding extraction of only the default tab.
- NPS safety disclosures, recipe ingredients, substantive asides, source notes,
  and W3C rendered teaching forms/buttons belong in the reference.
- Microsoft authorization/editor UI, DevSite bookmark controls, Britannica
  navigation/citation/AI widgets, and Natural History Museum newsletters and
  related-story rows are excluded explicitly. Substantive introductory text is kept.
- Stanford Philosophy includes the title and introduction before `#main-text`,
  full main text, bibliography and acknowledgments. Formula equivalence is not graded.
- Red Cross keeps before/during/after advice; duplicate mobile hero text and forms
  are excluded. Recipe references retain quantities, method and nutrition, with
  review widgets and related recipes excluded.
- Legacy HTML, preformatted RFCs, syntax-highlighted examples, long manuals and
  simple data tables are included. Original HTTP bytes and notices remain intact.

These boundaries are reviewable judgments, not proof that every intended passage
has been annotated perfectly. References score captured HTML text; unseen continuation
pages, videos, images, charts and client-generated facts are outside this track.
Some English legacy pages use non-UTF-8 bytes; this corpus does not establish
multilingual or character-encoding accuracy.

Third-party source material retains its original rights and notices; it is not
relicensed under ketch's MIT license. URLs below and in the manifest provide
attribution. The corpus is for development measurement, not content republication.

## Inventory

Each source URL opens the captured page's canonical destination. The manifest
contains capture time, hashes, source selector, exclusions, notes and every assertion.


### regression

| Page | Kind | Checks | Critical |
| --- | --- | ---: | ---: |
| [npm](https://docs.npmjs.com/trusted-publishers/) | docs | 6 | 4 |
| [go](https://pkg.go.dev/context) | api | 7 | 5 |
| [python](https://docs.python.org/3/tutorial/errors.html) | tutorial | 6 | 4 |
| [kubernetes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) | configuration | 6 | 4 |
| [postgres](https://www.postgresql.org/docs/current/transaction-iso.html) | database | 6 | 4 |
| [mdn](https://developer.mozilla.org/en-US/docs/Web/API/AbortController) | api | 7 | 4 |
| [rust](https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html) | tutorial | 6 | 4 |
| [sqlite](https://www.sqlite.org/lang_transaction.html) | database | 6 | 3 |
| [git](https://git-scm.com/docs/git-reset) | manual | 6 | 4 |
| [gnu](https://www.gnu.org/software/coreutils/manual/html_node/Exit-status.html) | manual | 5 | 3 |
| [terraform](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle) | configuration | 5 | 4 |
| [docker](https://docs.docker.com/build/building/multi-stage/) | tutorial | 5 | 4 |
| [cloudflare](https://blog.cloudflare.com/rfc-9457-agent-error-pages/) | blog | 7 | 5 |
| [danluu](https://danluu.com/slow-device/) | essay | 5 | 3 |
| [joel](https://www.joelonsoftware.com/2000/04/06/things-you-should-never-do-part-i/) | blog | 4 | 4 |
| [rfc](https://www.rfc-editor.org/rfc/rfc8259.html) | standard | 4 | 4 |
| [wikipedia](https://en.wikipedia.org/wiki/HTTP) | encyclopedia | 6 | 4 |
| [gutenberg](https://www.gutenberg.org/files/11/11-h/11-h.htm) | book | 4 | 3 |
| [nasa](https://science.nasa.gov/mars/facts/) | science | 5 | 4 |
| [nps](https://www.nps.gov/yell/planyourvisit/safety.htm) | visitor-guide | 9 | 5 |

### expansion

| Page | Kind | Checks | Critical |
| --- | --- | ---: | ---: |
| [npm-scripts](https://docs.npmjs.com/cli/v11/using-npm/scripts/) | docs | 8 | 4 |
| [npm-package-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-json/) | docs | 8 | 4 |
| [go-io](https://pkg.go.dev/io) | api | 8 | 4 |
| [go-strings](https://pkg.go.dev/strings) | api | 8 | 4 |
| [python-data](https://docs.python.org/3/tutorial/datastructures.html) | tutorial | 8 | 4 |
| [python-classes](https://docs.python.org/3/tutorial/classes.html) | tutorial | 8 | 4 |
| [kubernetes-deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) | configuration | 8 | 4 |
| [kubernetes-configmaps](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/) | configuration | 8 | 4 |
| [postgres-constraints](https://www.postgresql.org/docs/current/ddl-constraints.html) | database | 8 | 4 |
| [postgres-queries](https://www.postgresql.org/docs/current/queries-table-expressions.html) | database | 8 | 4 |
| [mdn-promise](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Promise) | api | 9 | 5 |
| [mdn-fetch](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch) | api | 8 | 4 |
| [rust-enums](https://doc.rust-lang.org/book/ch06-01-defining-an-enum.html) | tutorial | 7 | 4 |
| [rust-result](https://doc.rust-lang.org/book/ch09-02-recoverable-errors-with-result.html) | tutorial | 7 | 4 |
| [sqlite-select](https://www.sqlite.org/lang_select.html) | database | 7 | 4 |
| [sqlite-foreignkeys](https://www.sqlite.org/foreignkeys.html) | database | 7 | 4 |
| [git-rebase](https://git-scm.com/docs/git-rebase) | manual | 8 | 4 |
| [git-restore](https://git-scm.com/docs/git-restore) | manual | 8 | 4 |
| [gnu-copy](https://www.gnu.org/software/coreutils/manual/html_node/cp-invocation.html) | manual | 6 | 4 |
| [gnu-timeout](https://www.gnu.org/software/coreutils/manual/html_node/timeout-invocation.html) | manual | 6 | 4 |
| [terraform-foreach](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each) | configuration | 8 | 4 |
| [terraform-count](https://developer.hashicorp.com/terraform/language/meta-arguments/count) | configuration | 8 | 4 |
| [docker-volumes](https://docs.docker.com/engine/storage/volumes/) | docs | 9 | 5 |
| [docker-bridge](https://docs.docker.com/engine/network/drivers/bridge/) | docs | 9 | 5 |
| [cloudflare-pingora](https://blog.cloudflare.com/how-we-built-pingora-the-proxy-that-connects-cloudflare-to-the-internet/) | blog | 7 | 3 |
| [cloudflare-opensource](https://blog.cloudflare.com/pingora-open-source/) | blog | 8 | 4 |
| [danluu-branch](https://danluu.com/branch-prediction/) | essay | 8 | 4 |
| [danluu-malloc](https://danluu.com/malloc-tutorial/) | essay | 6 | 4 |
| [joel-test](https://www.joelonsoftware.com/2000/08/09/the-joel-test-12-steps-to-better-code/) | blog | 5 | 3 |
| [joel-leaky](https://www.joelonsoftware.com/2002/11/11/the-law-of-leaky-abstractions/) | blog | 5 | 3 |
| [rfc-http](https://www.rfc-editor.org/rfc/rfc9110.html) | standard | 7 | 4 |
| [rfc-uri](https://www.rfc-editor.org/rfc/rfc3986.html) | standard | 5 | 4 |
| [wikipedia-photosynthesis](https://en.wikipedia.org/wiki/Photosynthesis) | encyclopedia | 8 | 4 |
| [wikipedia-binary](https://en.wikipedia.org/wiki/Binary_search) | encyclopedia | 8 | 4 |
| [gutenberg-wallpaper](https://www.gutenberg.org/files/1952/1952-h/1952-h.htm) | book | 4 | 3 |
| [gutenberg-metamorphosis](https://www.gutenberg.org/files/5200/5200-h/5200-h.htm) | book | 3 | 3 |
| [nasa-earth](https://science.nasa.gov/earth/facts/) | science | 7 | 3 |
| [nasa-jupiter](https://science.nasa.gov/jupiter/jupiter-facts/) | science | 7 | 3 |
| [nps-hiking](https://www.nps.gov/grca/planyourvisit/hiking-faq.htm) | visitor-guide | 6 | 3 |
| [nps-canyon](https://www.nps.gov/grca/faqs.htm) | visitor-guide | 7 | 3 |
| [pandas-indexing](https://pandas.pydata.org/docs/user_guide/indexing.html) | api | 9 | 5 |
| [pandas-merge](https://pandas.pydata.org/docs/user_guide/merging.html) | api | 9 | 5 |
| [django-queries](https://docs.djangoproject.com/en/5.2/topics/db/queries/) | docs | 8 | 4 |
| [django-transactions](https://docs.djangoproject.com/en/5.2/topics/db/transactions/) | docs | 8 | 4 |
| [curl-manpage](https://curl.se/docs/manpage.html) | manual | 8 | 4 |
| [curl-http](https://curl.se/docs/httpscripting.html) | manual | 8 | 4 |
| [w3c-tables](https://www.w3.org/WAI/tutorials/tables/one-header/) | accessibility | 9 | 5 |
| [w3c-irregular](https://www.w3.org/WAI/tutorials/tables/irregular/) | accessibility | 8 | 4 |
| [owid-life](https://ourworldindata.org/life-expectancy) | data-article | 8 | 4 |
| [owid-population](https://ourworldindata.org/population-growth) | data-article | 8 | 4 |
| [eff-passwords](https://ssd.eff.org/module/creating-strong-passwords) | guide | 7 | 3 |
| [eff-plan](https://ssd.eff.org/module/your-security-plan) | guide | 7 | 3 |
| [paulgraham-lisp](https://www.paulgraham.com/avg.html) | essay | 4 | 3 |
| [paulgraham-schedule](https://www.paulgraham.com/makersschedule.html) | essay | 4 | 3 |
| [fowler-microservices](https://martinfowler.com/articles/microservices.html) | essay | 7 | 3 |
| [fowler-debt](https://martinfowler.com/bliki/TechnicalDebt.html) | essay | 7 | 3 |
| [letsencrypt-challenges](https://letsencrypt.org/docs/challenge-types/) | docs | 5 | 3 |
| [letsencrypt-compatibility](https://letsencrypt.org/docs/certificate-compatibility/) | docs | 5 | 3 |
| [noaa-salt](https://oceanservice.noaa.gov/facts/whysalty.html) | science | 6 | 3 |
| [noaa-blue](https://oceanservice.noaa.gov/facts/oceanblue.html) | science | 6 | 3 |

### heldout

| Page | Kind | Checks | Critical |
| --- | --- | ---: | ---: |
| [bgs-earthquakes](https://www.bgs.ac.uk/discovering-geology/earth-hazards/earthquakes/how-are-earthquakes-detected/) | science | 8 | 4 |
| [medlineplus-handwashing](https://medlineplus.gov/ency/patientinstructions/000972.htm) | health | 7 | 3 |
| [who-resistance](https://www.who.int/news-room/fact-sheets/detail/antimicrobial-resistance) | health | 7 | 3 |
| [un-climate](https://www.un.org/en/climatechange/what-is-climate-change) | explainer | 7 | 3 |
| [computerhistory-1969](https://www.computerhistory.org/timeline/1969/) | history | 7 | 3 |
| [nhm-meteorites](https://www.nhm.ac.uk/discover/types-of-meteorites.html) | science | 7 | 3 |
| [standardebooks-austen](https://standardebooks.org/ebooks/jane-austen/pride-and-prejudice) | catalogue | 7 | 3 |
| [archives-declaration](https://www.archives.gov/founding-docs/declaration-transcript) | historical-document | 5 | 3 |
| [wikivoyage-paris](https://en.wikivoyage.org/wiki/Paris) | visitor-guide | 8 | 4 |
| [kingarthur-bread](https://www.kingarthurbaking.com/recipes/classic-sandwich-bread-recipe) | recipe | 10 | 6 |
| [seriouseats-cookies](https://www.seriouseats.com/the-food-lab-best-chocolate-chip-cookie-recipe) | recipe | 9 | 5 |
| [ifixit-moped](https://www.ifixit.com/Guide/Suzuki+FA50+Moped+Exhaust+Cleaning/3926) | repair-guide | 7 | 3 |
| [discourse-guide](https://meta.discourse.org/t/understanding-discourse-for-new-users/96331) | discussion | 8 | 4 |
| [hackernews-startup](https://news.ycombinator.com/item?id=8863) | discussion | 5 | 3 |
| [julia-arrays](https://docs.julialang.org/en/v1/manual/arrays/) | manual | 9 | 5 |
| [elixir-enum](https://elixir.hexdocs.pm/enum-cheat.html) | api | 8 | 4 |
| [ffmpeg-filters](https://ffmpeg.org/ffmpeg-filters.html) | manual | 7 | 4 |
| [angular-signals](https://angular.dev/guide/signals) | docs | 8 | 4 |
| [react-state](https://react.dev/learn/state-a-components-memory) | tutorial | 8 | 4 |
| [ncat-guide](https://nmap.org/ncat/guide/index.html) | manual | 6 | 3 |

### scale

| Page | Kind | Checks | Critical |
| --- | --- | ---: | ---: |
| [npm-install \| npm Docs](https://docs.npmjs.com/cli/v11/commands/npm-install/) | docs | 8 | 4 |
| [npm-ci \| npm Docs](https://docs.npmjs.com/cli/v11/commands/npm-ci/) | docs | 8 | 4 |
| [npm-publish \| npm Docs](https://docs.npmjs.com/cli/v11/commands/npm-publish/) | docs | 8 | 4 |
| [npm-audit \| npm Docs](https://docs.npmjs.com/cli/v11/commands/npm-audit/) | docs | 8 | 4 |
| [npm-exec \| npm Docs](https://docs.npmjs.com/cli/v11/commands/npm-exec/) | docs | 8 | 4 |
| [npm-run \| npm Docs](https://docs.npmjs.com/cli/v11/commands/npm-run/) | docs | 8 | 4 |
| [package-lock.json \| npm Docs](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/) | docs | 7 | 3 |
| [Workspaces \| npm Docs](https://docs.npmjs.com/cli/v11/using-npm/workspaces/) | docs | 8 | 4 |
| [bytes package - bytes - Go Packages](https://pkg.go.dev/bytes) | api | 8 | 4 |
| [errors package - errors - Go Packages](https://pkg.go.dev/errors) | api | 8 | 4 |
| [fmt package - fmt - Go Packages](https://pkg.go.dev/fmt) | api | 8 | 4 |
| [sync package - sync - Go Packages](https://pkg.go.dev/sync) | api | 8 | 4 |
| [time package - time - Go Packages](https://pkg.go.dev/time) | api | 8 | 4 |
| [json package - encoding/json - Go Packages](https://pkg.go.dev/encoding/json) | api | 8 | 4 |
| [http package - net/http - Go Packages](https://pkg.go.dev/net/http) | api | 8 | 4 |
| [os package - os - Go Packages](https://pkg.go.dev/os) | api | 8 | 4 |
| [4. More Control Flow Tools — Python 3.14.7 documentation](https://docs.python.org/3/tutorial/controlflow.html) | tutorial | 8 | 4 |
| [6. Modules — Python 3.14.7 documentation](https://docs.python.org/3/tutorial/modules.html) | tutorial | 8 | 4 |
| [7. Input and Output — Python 3.14.7 documentation](https://docs.python.org/3/tutorial/inputoutput.html) | tutorial | 8 | 4 |
| [10. Brief tour of the standard library — Python 3.14.7 documentation](https://docs.python.org/3/tutorial/stdlib.html) | tutorial | 8 | 4 |
| [11. Brief tour of the standard library — part II — Python 3.14.7 documentation](https://docs.python.org/3/tutorial/stdlib2.html) | tutorial | 8 | 4 |
| [12. Virtual Environments and Packages — Python 3.14.7 documentation](https://docs.python.org/3/tutorial/venv.html) | tutorial | 8 | 4 |
| [15. Floating-Point Arithmetic: Issues and Limitations — Python 3.14.7 documentation](https://docs.python.org/3/tutorial/floatingpoint.html) | tutorial | 7 | 4 |
| [2. Using the Python Interpreter — Python 3.14.7 documentation](https://docs.python.org/3/tutorial/interpreter.html) | tutorial | 8 | 4 |
| [Pods \| Kubernetes](https://kubernetes.io/docs/concepts/workloads/pods/) | configuration | 8 | 4 |
| [StatefulSets \| Kubernetes](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) | configuration | 9 | 5 |
| [Service \| Kubernetes](https://kubernetes.io/docs/concepts/services-networking/service/) | configuration | 9 | 5 |
| [Ingress \| Kubernetes](https://kubernetes.io/docs/concepts/services-networking/ingress/) | configuration | 9 | 5 |
| [Persistent Volumes \| Kubernetes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) | configuration | 9 | 5 |
| [Secrets \| Kubernetes](https://kubernetes.io/docs/concepts/configuration/secret/) | configuration | 9 | 5 |
| [Taints and Tolerations \| Kubernetes](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) | configuration | 8 | 4 |
| [Labels and Selectors \| Kubernetes](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) | configuration | 8 | 4 |
| [PostgreSQL: Documentation: 18: 5.2. Default Values](https://www.postgresql.org/docs/current/ddl-default.html) | database | 7 | 4 |
| [PostgreSQL: Documentation: 18: 5.7. Modifying Tables](https://www.postgresql.org/docs/current/ddl-alter.html) | database | 8 | 4 |
| [PostgreSQL: Documentation: 18: 5.11. Inheritance](https://www.postgresql.org/docs/current/ddl-inherit.html) | database | 8 | 4 |
| [PostgreSQL: Documentation: 18: 5.12. Table Partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html) | database | 8 | 4 |
| [PostgreSQL: Documentation: 18: 11.1. Introduction](https://www.postgresql.org/docs/current/indexes-intro.html) | database | 7 | 4 |
| [PostgreSQL: Documentation: 18: 7.8. WITH Queries (Common Table Expressions)](https://www.postgresql.org/docs/current/queries-with.html) | database | 8 | 4 |
| [PostgreSQL: Documentation: 18: 7.5. Sorting Rows (ORDER BY)](https://www.postgresql.org/docs/current/queries-order.html) | database | 6 | 4 |
| [PostgreSQL: Documentation: 18: SELECT](https://www.postgresql.org/docs/current/sql-select.html) | database | 8 | 4 |
| [Map - JavaScript \| MDN](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Map) | api | 8 | 4 |
| [Set - JavaScript \| MDN](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Set) | api | 9 | 5 |
| [Iterators and generators - JavaScript \| MDN](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Iterators_and_generators) | api | 8 | 4 |
| [Basic concepts of grid layout - CSS \| MDN](https://developer.mozilla.org/en-US/docs/Web/CSS/Guides/Grid_layout/Basic_concepts) | api | 8 | 4 |
| [<details> HTML details disclosure element - HTML \| MDN](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/details) | api | 9 | 5 |
| [Intersection Observer API - Web APIs \| MDN](https://developer.mozilla.org/en-US/docs/Web/API/Intersection_Observer_API) | api | 9 | 5 |
| [Overview of HTTP - HTTP \| MDN](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Overview) | api | 8 | 4 |
| [HTML: A good basis for accessibility - Learn web development \| MDN](https://developer.mozilla.org/en-US/docs/Learn_web_development/Core/Accessibility/HTML) | api | 8 | 4 |
| [Variables and Mutability - The Rust Programming Language](https://doc.rust-lang.org/book/ch03-01-variables-and-mutability.html) | tutorial | 7 | 4 |
| [Data Types - The Rust Programming Language](https://doc.rust-lang.org/book/ch03-02-data-types.html) | tutorial | 8 | 5 |
| [References and Borrowing - The Rust Programming Language](https://doc.rust-lang.org/book/ch04-02-references-and-borrowing.html) | tutorial | 6 | 4 |
| [Defining and Instantiating Structs - The Rust Programming Language](https://doc.rust-lang.org/book/ch05-01-defining-structs.html) | tutorial | 7 | 4 |
| [Storing Lists of Values with Vectors - The Rust Programming Language](https://doc.rust-lang.org/book/ch08-01-vectors.html) | tutorial | 7 | 4 |
| [Defining Shared Behavior with Traits - The Rust Programming Language](https://doc.rust-lang.org/book/ch10-02-traits.html) | tutorial | 7 | 4 |
| [Processing a Series of Items with Iterators - The Rust Programming Language](https://doc.rust-lang.org/book/ch13-02-iterators.html) | tutorial | 6 | 4 |
| [Using Threads to Run Code Simultaneously - The Rust Programming Language](https://doc.rust-lang.org/book/ch16-01-threads.html) | tutorial | 7 | 4 |
| [INSERT](https://www.sqlite.org/lang_insert.html) | database | 4 | 3 |
| [UPDATE](https://www.sqlite.org/lang_update.html) | database | 7 | 4 |
| [DELETE](https://www.sqlite.org/lang_delete.html) | database | 4 | 3 |
| [CREATE TABLE](https://www.sqlite.org/lang_createtable.html) | database | 8 | 5 |
| [The WITH Clause](https://www.sqlite.org/lang_with.html) | database | 7 | 4 |
| [Window Functions](https://www.sqlite.org/windowfunctions.html) | database | 8 | 5 |
| [Datatypes In SQLite](https://www.sqlite.org/datatype3.html) | database | 8 | 5 |
| [Isolation In SQLite](https://www.sqlite.org/isolation.html) | database | 6 | 3 |
| [Git - git-merge Documentation](https://git-scm.com/docs/git-merge) | manual | 8 | 4 |
| [Git - git-cherry-pick Documentation](https://git-scm.com/docs/git-cherry-pick) | manual | 8 | 4 |
| [Git - git-bisect Documentation](https://git-scm.com/docs/git-bisect) | manual | 8 | 4 |
| [Git - git-stash Documentation](https://git-scm.com/docs/git-stash) | manual | 8 | 4 |
| [Git - git-reflog Documentation](https://git-scm.com/docs/git-reflog) | manual | 8 | 4 |
| [Git - git-worktree Documentation](https://git-scm.com/docs/git-worktree) | manual | 8 | 4 |
| [Git - git-fetch Documentation](https://git-scm.com/docs/git-fetch) | manual | 8 | 4 |
| [Git - git-log Documentation](https://git-scm.com/docs/git-log) | manual | 8 | 4 |
| [ls invocation (GNU Coreutils 9.12)](https://www.gnu.org/software/coreutils/manual/html_node/ls-invocation.html) | manual | 6 | 4 |
| [mv invocation (GNU Coreutils 9.12)](https://www.gnu.org/software/coreutils/manual/html_node/mv-invocation.html) | manual | 6 | 4 |
| [rm invocation (GNU Coreutils 9.12)](https://www.gnu.org/software/coreutils/manual/html_node/rm-invocation.html) | manual | 6 | 4 |
| [dd invocation (GNU Coreutils 9.12)](https://www.gnu.org/software/coreutils/manual/html_node/dd-invocation.html) | manual | 6 | 4 |
| [sort invocation (GNU Coreutils 9.12)](https://www.gnu.org/software/coreutils/manual/html_node/sort-invocation.html) | manual | 6 | 4 |
| [uniq invocation (GNU Coreutils 9.12)](https://www.gnu.org/software/coreutils/manual/html_node/uniq-invocation.html) | manual | 6 | 4 |
| [date invocation (GNU Coreutils 9.12)](https://www.gnu.org/software/coreutils/manual/html_node/date-invocation.html) | manual | 6 | 4 |
| [chmod invocation (GNU Coreutils 9.12)](https://www.gnu.org/software/coreutils/manual/html_node/chmod-invocation.html) | manual | 6 | 4 |
| [Use input variables to add module arguments \| Terraform \| HashiCorp Developer](https://developer.hashicorp.com/terraform/language/values/variables) | configuration | 8 | 4 |
| [Use outputs to expose module data \| Terraform \| HashiCorp Developer](https://developer.hashicorp.com/terraform/language/values/outputs) | configuration | 8 | 4 |
| [Use locals to reuse expressions \| Terraform \| HashiCorp Developer](https://developer.hashicorp.com/terraform/language/values/locals) | configuration | 8 | 4 |
| [Types and Values - Configuration Language \| Terraform \| HashiCorp Developer](https://developer.hashicorp.com/terraform/language/expressions/types) | configuration | 8 | 4 |
| [Conditional Expressions - Configuration Language \| Terraform \| HashiCorp Developer](https://developer.hashicorp.com/terraform/language/expressions/conditionals) | configuration | 8 | 4 |
| [For Expressions - Configuration Language \| Terraform \| HashiCorp Developer](https://developer.hashicorp.com/terraform/language/expressions/for) | configuration | 8 | 4 |
| [module block reference \| Terraform \| HashiCorp Developer](https://developer.hashicorp.com/terraform/language/block/module) | configuration | 8 | 4 |
| [depends_on meta-argument reference \| Terraform \| HashiCorp Developer](https://developer.hashicorp.com/terraform/language/meta-arguments/depends_on) | configuration | 8 | 4 |
| [Building best practices \| Docker Docs](https://docs.docker.com/build/building/best-practices/) | docs | 8 | 4 |
| [Docker build cache \| Docker Docs](https://docs.docker.com/build/cache/) | docs | 8 | 4 |
| [Build secrets \| Docker Docs](https://docs.docker.com/build/building/secrets/) | docs | 8 | 4 |
| [Bind mounts \| Docker Docs](https://docs.docker.com/engine/storage/bind-mounts/) | docs | 9 | 5 |
| [Host network driver \| Docker Docs](https://docs.docker.com/engine/network/drivers/host/) | docs | 8 | 4 |
| [Resource constraints \| Docker Docs](https://docs.docker.com/engine/containers/resource_constraints/) | docs | 9 | 5 |
| [Control startup and shutdown order in Compose \| Docker Docs](https://docs.docker.com/compose/how-tos/startup-order/) | docs | 8 | 4 |
| [Set environment variables within your container's environment \| Docker Docs](https://docs.docker.com/compose/how-tos/environment-variables/set-environment-variables/) | docs | 8 | 4 |
| [How Pingora keeps count \| Cloudflare Blog](https://blog.cloudflare.com/how-pingora-keeps-count/) | blog | 9 | 5 |
| [Everyone can now run JavaScript on Cloudflare with Workers \| Cloudflare Blog](https://blog.cloudflare.com/cloudflare-workers-unleashed/) | blog | 8 | 4 |
| [Introducing Cloudflare Workers: Run JavaScript Service Workers at the Edge \| Cloudflare Blog](https://blog.cloudflare.com/introducing-cloudflare-workers/) | blog | 8 | 4 |
| [Introducing Cache Reserve: massively extending Cloudflare’s cache \| Cloudflare Blog](https://blog.cloudflare.com/introducing-cache-reserve/) | blog | 7 | 3 |
| [Why does one NGINX worker take all the load? \| Cloudflare Blog](https://blog.cloudflare.com/the-sad-state-of-linux-socket-balancing/) | blog | 8 | 4 |
| [Keepalives considered harmful \| Cloudflare Blog](https://blog.cloudflare.com/keepalives-considered-harmful/) | blog | 8 | 4 |
| [Road to gRPC \| Cloudflare Blog](https://blog.cloudflare.com/road-to-grpc/) | blog | 7 | 3 |
| [The Road to QUIC \| Cloudflare Blog](https://blog.cloudflare.com/the-road-to-quic/) | blog | 7 | 3 |
| [Given that we spend little effort on testing, how should we test software?](https://danluu.com/testing/) | essay | 8 | 4 |
| [Latency mitigation strategies (by John Carmack)](https://danluu.com/latency-mitigation/) | essay | 5 | 4 |
| [Computer latency: 1977-2017](https://danluu.com/input-lag/) | essay | 8 | 4 |
| [Files are hard](https://danluu.com/file-consistency/) | essay | 8 | 4 |
| [Reading postmortems](https://danluu.com/postmortem-lessons/) | essay | 7 | 3 |
| [Keyboard latency](https://danluu.com/keyboard-latency/) | essay | 7 | 3 |
| [We saw some really bad Intel CPU bugs in 2015 and we should expect to see more in the future](https://danluu.com/cpu-bugs/) | essay | 7 | 3 |
| [In defense of simple architectures](https://danluu.com/simple-architectures/) | essay | 5 | 3 |
| [Painless Functional Specifications – Part 1: Why Bother? – Joel on Software](https://www.joelonsoftware.com/2000/10/02/painless-functional-specifications-part-1-why-bother/) | blog | 5 | 3 |
| [Three Wrong Ideas From Computer Science – Joel on Software](https://www.joelonsoftware.com/2000/08/22/three-wrong-ideas-from-computer-science/) | blog | 7 | 3 |
| [Back to Basics – Joel on Software](https://www.joelonsoftware.com/2001/12/11/back-to-basics/) | blog | 6 | 4 |
| [Test Yourself – Joel on Software](https://www.joelonsoftware.com/2005/12/29/test-yourself/) | blog | 6 | 4 |
| [Strategy Letter V – Joel on Software](https://www.joelonsoftware.com/2002/06/12/strategy-letter-v/) | blog | 5 | 3 |
| [The Perils of JavaSchools – Joel on Software](https://www.joelonsoftware.com/2005/12/29/the-perils-of-javaschools-2/) | blog | 5 | 3 |
| [Two Stories – Joel on Software](https://www.joelonsoftware.com/2000/03/19/two-stories/) | blog | 4 | 3 |
| [Painless Bug Tracking – Joel on Software](https://www.joelonsoftware.com/2000/11/08/painless-bug-tracking/) | blog | 7 | 3 |
| [more-rfc-editor-org-rfc-rfc8446](https://www.rfc-editor.org/rfc/rfc8446.html) | standard | 5 | 4 |
| [RFC 9000: QUIC: A UDP-Based Multiplexed and Secure Transport](https://www.rfc-editor.org/rfc/rfc9000.html) | standard | 7 | 4 |
| [RFC 9111: HTTP Caching](https://www.rfc-editor.org/rfc/rfc9111.html) | standard | 7 | 4 |
| [RFC 9457: Problem Details for HTTP APIs](https://www.rfc-editor.org/rfc/rfc9457.html) | standard | 7 | 4 |
| [more-rfc-editor-org-rfc-rfc7519](https://www.rfc-editor.org/rfc/rfc7519.html) | standard | 5 | 4 |
| [more-rfc-editor-org-rfc-rfc6455](https://www.rfc-editor.org/rfc/rfc6455.html) | standard | 5 | 4 |
| [more-rfc-editor-org-rfc-rfc6902](https://www.rfc-editor.org/rfc/rfc6902.html) | standard | 5 | 4 |
| [more-rfc-editor-org-rfc-rfc3339](https://www.rfc-editor.org/rfc/rfc3339.html) | standard | 5 | 4 |
| [Bicycle - Wikipedia](https://en.wikipedia.org/wiki/Bicycle) | encyclopedia | 7 | 3 |
| [Solar System - Wikipedia](https://en.wikipedia.org/wiki/Solar_System) | encyclopedia | 7 | 3 |
| [Black hole - Wikipedia](https://en.wikipedia.org/wiki/Black_hole) | encyclopedia | 8 | 4 |
| [Ada Lovelace - Wikipedia](https://en.wikipedia.org/wiki/Ada_Lovelace) | encyclopedia | 7 | 3 |
| [Fermentation - Wikipedia](https://en.wikipedia.org/wiki/Fermentation) | encyclopedia | 7 | 3 |
| [Silk Road - Wikipedia](https://en.wikipedia.org/wiki/Silk_Road) | encyclopedia | 7 | 3 |
| [Fibonacci sequence - Wikipedia](https://en.wikipedia.org/wiki/Fibonacci_sequence) | encyclopedia | 7 | 3 |
| [Coral reef - Wikipedia](https://en.wikipedia.org/wiki/Coral_reef) | encyclopedia | 7 | 3 |
| [Mercury: Facts](https://science.nasa.gov/mercury/facts/) | science | 6 | 3 |
| [Venus: Facts - NASA Science](https://science.nasa.gov/venus/venus-facts/) | science | 7 | 3 |
| [Saturn: Facts - NASA Science](https://science.nasa.gov/saturn/facts/) | science | 7 | 3 |
| [Uranus: Facts - NASA Science](https://science.nasa.gov/uranus/facts/) | science | 6 | 3 |
| [Neptune: Facts - NASA Science](https://science.nasa.gov/neptune/neptune-facts/) | science | 8 | 4 |
| [Moon Facts - NASA Science](https://science.nasa.gov/moon/facts/) | science | 7 | 3 |
| [Sun: Facts - NASA Science](https://science.nasa.gov/sun/facts/) | science | 7 | 3 |
| [Asteroid Facts](https://science.nasa.gov/solar-system/asteroids/facts/) | science | 7 | 3 |
| [Hiking - Yosemite National Park (U.S. National Park Service)](https://www.nps.gov/yose/planyourvisit/hiking.htm) | visitor-guide | 6 | 3 |
| [Hiking - Great Smoky Mountains National Park (U.S. National Park Service)](https://www.nps.gov/grsm/planyourvisit/hiking.htm) | visitor-guide | 7 | 3 |
| [Hiking - Acadia National Park (U.S. National Park Service)](https://www.nps.gov/acad/planyourvisit/hiking.htm) | visitor-guide | 7 | 3 |
| [Safety - Zion National Park (U.S. National Park Service)](https://www.nps.gov/zion/planyourvisit/safety.htm) | visitor-guide | 5 | 3 |
| [Wilderness Safety - Zion National Park (U.S. National Park Service)](https://www.nps.gov/zion/planyourvisit/wilderness-safety.htm) | visitor-guide | 7 | 3 |
| [Safety - Grand Teton National Park (U.S. National Park Service)](https://www.nps.gov/grte/planyourvisit/safety.htm) | visitor-guide | 7 | 3 |
| [Winter Safety - Yellowstone National Park (U.S. National Park Service)](https://www.nps.gov/yell/planyourvisit/winter-safety.htm) | visitor-guide | 7 | 3 |
| [Safety in Bear Country - Grand Teton National Park (U.S. National Park Service)](https://www.nps.gov/grte/planyourvisit/bearsafety.htm) | visitor-guide | 7 | 3 |
| [Group by: split-apply-combine — pandas 3.0.6 documentation](https://pandas.pydata.org/docs/user_guide/groupby.html) | api | 9 | 5 |
| [Working with missing data — pandas 3.0.6 documentation](https://pandas.pydata.org/docs/user_guide/missing_data.html) | api | 8 | 4 |
| [Reshaping and pivot tables — pandas 3.0.6 documentation](https://pandas.pydata.org/docs/user_guide/reshaping.html) | api | 8 | 4 |
| [Categorical data — pandas 3.0.6 documentation](https://pandas.pydata.org/docs/user_guide/categorical.html) | api | 9 | 5 |
| [Time series / date functionality — pandas 3.0.6 documentation](https://pandas.pydata.org/docs/user_guide/timeseries.html) | api | 9 | 5 |
| [Working with text data — pandas 3.0.6 documentation](https://pandas.pydata.org/docs/user_guide/text.html) | api | 9 | 5 |
| [Duplicate Labels — pandas 3.0.6 documentation](https://pandas.pydata.org/docs/user_guide/duplicates.html) | api | 8 | 4 |
| [Options and settings — pandas 3.0.6 documentation](https://pandas.pydata.org/docs/user_guide/options.html) | api | 8 | 4 |
| [Models \| Django documentation \| Django](https://docs.djangoproject.com/en/5.2/topics/db/models/) | docs | 8 | 4 |
| [Aggregation \| Django documentation \| Django](https://docs.djangoproject.com/en/5.2/topics/db/aggregation/) | docs | 8 | 4 |
| [Writing views \| Django documentation \| Django](https://docs.djangoproject.com/en/5.2/topics/http/views/) | docs | 8 | 4 |
| [URL dispatcher \| Django documentation \| Django](https://docs.djangoproject.com/en/5.2/topics/http/urls/) | docs | 8 | 4 |
| [Working with forms \| Django documentation \| Django](https://docs.djangoproject.com/en/5.2/topics/forms/) | docs | 8 | 4 |
| [Using the Django authentication system \| Django documentation \| Django](https://docs.djangoproject.com/en/5.2/topics/auth/default/) | docs | 8 | 4 |
| [Django’s cache framework \| Django documentation \| Django](https://docs.djangoproject.com/en/5.2/topics/cache/) | docs | 8 | 4 |
| [Writing and running tests \| Django documentation \| Django](https://docs.djangoproject.com/en/5.2/topics/testing/overview/) | docs | 8 | 4 |
| [curl - HTTP Cookies](https://curl.se/docs/http-cookies.html) | manual | 7 | 3 |
| [curl - SSL CA Certificates](https://curl.se/docs/sslcerts.html) | manual | 8 | 4 |
| [curl - Alt-Svc cache](https://curl.se/docs/alt-svc.html) | manual | 8 | 4 |
| [curl - HTTP Strict-Transport-Security (HSTS)](https://curl.se/docs/hsts.html) | manual | 8 | 4 |
| [HTTP/3 with curl](https://curl.se/docs/http3.html) | manual | 8 | 4 |
| [curl - URL syntax](https://curl.se/docs/url-syntax.html) | manual | 8 | 4 |
| [SSL ciphers](https://curl.se/docs/ssl-ciphers.html) | manual | 8 | 4 |
| [curl - SSL libraries compared](https://curl.se/docs/ssl-compared.html) | manual | 7 | 3 |
| [Decorative Images \| Web Accessibility Initiative (WAI) \| W3C](https://www.w3.org/WAI/tutorials/images/decorative/) | accessibility | 8 | 4 |
| [Informative Images \| Web Accessibility Initiative (WAI) \| W3C](https://www.w3.org/WAI/tutorials/images/informative/) | accessibility | 8 | 4 |
| [Functional Images \| Web Accessibility Initiative (WAI) \| W3C](https://www.w3.org/WAI/tutorials/images/functional/) | accessibility | 8 | 4 |
| [Complex Images \| Web Accessibility Initiative (WAI) \| W3C](https://www.w3.org/WAI/tutorials/images/complex/) | accessibility | 8 | 4 |
| [Labeling Controls \| Web Accessibility Initiative (WAI) \| W3C](https://www.w3.org/WAI/tutorials/forms/labels/) | accessibility | 8 | 4 |
| [Form Instructions \| Web Accessibility Initiative (WAI) \| W3C](https://www.w3.org/WAI/tutorials/forms/instructions/) | accessibility | 8 | 4 |
| [Tables with Multi-Level Headers \| Web Accessibility Initiative (WAI) \| W3C](https://www.w3.org/WAI/tutorials/tables/multi-level/) | accessibility | 8 | 4 |
| [Headings \| Web Accessibility Initiative (WAI) \| W3C](https://www.w3.org/WAI/tutorials/page-structure/headings/) | accessibility | 7 | 3 |
| [CO₂ emissions \| Our World in Data](https://ourworldindata.org/co2-emissions) | data-article | 8 | 4 |
| [Energy Mix \| Our World in Data](https://ourworldindata.org/energy-mix) | data-article | 8 | 4 |
| [Plastic Pollution \| Our World in Data](https://ourworldindata.org/plastic-pollution) | data-article | 8 | 4 |
| [Literacy \| Our World in Data](https://ourworldindata.org/literacy) | data-article | 8 | 4 |
| [Economic Growth \| Our World in Data](https://ourworldindata.org/economic-growth) | data-article | 8 | 4 |
| [Child and Infant Mortality \| Our World in Data](https://ourworldindata.org/child-mortality) | data-article | 8 | 4 |
| [Vaccination \| Our World in Data](https://ourworldindata.org/vaccination) | data-article | 8 | 4 |
| [Renewable Energy \| Our World in Data](https://ourworldindata.org/renewable-energy) | data-article | 8 | 4 |
| [How to: Use Signal \| Surveillance Self-Defense](https://ssd.eff.org/module/how-to-use-signal) | guide | 7 | 3 |
| [How to: Use Tor \| Surveillance Self-Defense](https://ssd.eff.org/module/how-to-use-tor) | guide | 7 | 3 |
| [How to: Encrypt Your Windows, Mac, or Linux Computer \| Surveillance Self-Defense](https://ssd.eff.org/module/how-encrypt-your-windows-device) | guide | 7 | 3 |
| [How to: Enable Two-factor Authentication \| Surveillance Self-Defense](https://ssd.eff.org/module/how-enable-two-factor-authentication) | guide | 7 | 3 |
| [What Should I Know About Encryption? \| Surveillance Self-Defense](https://ssd.eff.org/module/what-should-i-know-about-encryption) | guide | 7 | 3 |
| [Choosing the VPN That's Right for You \| Surveillance Self-Defense](https://ssd.eff.org/module/choosing-vpn-thats-right-you) | guide | 7 | 3 |
| [Keeping Your Data Safe \| Surveillance Self-Defense](https://ssd.eff.org/module/keeping-your-data-safe) | guide | 7 | 3 |
| [Protecting Yourself on Social Networks \| Surveillance Self-Defense](https://ssd.eff.org/module/protecting-yourself-social-networks) | guide | 7 | 3 |
| [What You'll Wish You'd Known](https://www.paulgraham.com/hs.html) | essay | 4 | 3 |
| [How to Do Great Work](https://www.paulgraham.com/greatwork.html) | essay | 3 | 3 |
| [How to Get Startup Ideas](https://www.paulgraham.com/startupideas.html) | essay | 4 | 3 |
| [Good and Bad Procrastination](https://www.paulgraham.com/procrastination.html) | essay | 4 | 3 |
| [Why Nerds are Unpopular](https://www.paulgraham.com/nerds.html) | essay | 4 | 3 |
| [How to Make Wealth](https://www.paulgraham.com/wealth.html) | essay | 4 | 3 |
| [Cities and Ambition](https://www.paulgraham.com/cities.html) | essay | 4 | 3 |
| [Be Good](https://www.paulgraham.com/good.html) | essay | 4 | 3 |
| [Feature Toggles (aka Feature Flags)](https://martinfowler.com/articles/feature-toggles.html) | essay | 8 | 4 |
| [Inversion of Control Containers and the Dependency Injection pattern](https://martinfowler.com/articles/injection.html) | essay | 8 | 4 |
| [Strangler Fig](https://martinfowler.com/bliki/StranglerFigApplication.html) | essay | 6 | 3 |
| [Monolith First](https://martinfowler.com/bliki/MonolithFirst.html) | essay | 7 | 3 |
| [Two Hard Things](https://martinfowler.com/bliki/TwoHardThings.html) | essay | 7 | 3 |
| [CQRS](https://martinfowler.com/bliki/CQRS.html) | essay | 7 | 3 |
| [The Practical Test Pyramid](https://martinfowler.com/articles/practical-test-pyramid.html) | essay | 8 | 4 |
| [Bounded Context](https://martinfowler.com/bliki/BoundedContext.html) | essay | 7 | 3 |
| [Rate Limits - Let's Encrypt](https://letsencrypt.org/docs/rate-limits/) | docs | 9 | 5 |
| [Staging Environment - Let's Encrypt](https://letsencrypt.org/docs/staging-environment/) | docs | 8 | 4 |
| [FAQ - Let's Encrypt](https://letsencrypt.org/docs/faq/) | docs | 7 | 3 |
| [Integration Guide - Let's Encrypt](https://letsencrypt.org/docs/integration-guide/) | docs | 5 | 3 |
| [Finding Account IDs - Let's Encrypt](https://letsencrypt.org/docs/account-id/) | docs | 5 | 3 |
| [IPv6 Support - Let's Encrypt](https://letsencrypt.org/docs/ipv6-support/) | docs | 7 | 3 |
| [Revoking Certificates - Let's Encrypt](https://letsencrypt.org/docs/revoking/) | docs | 6 | 4 |
| [Certificate Transparency (CT) Logs - Let's Encrypt](https://letsencrypt.org/docs/ct-logs/) | docs | 8 | 4 |
| [What are tides?](https://oceanservice.noaa.gov/facts/tides.html) | science | 6 | 3 |
| [What is a current?](https://oceanservice.noaa.gov/facts/current.html) | science | 6 | 3 |
| [Are corals animals or plants?](https://oceanservice.noaa.gov/facts/coral.html) | science | 4 | 3 |
| [What is Ocean Acidification?](https://oceanservice.noaa.gov/facts/acidification.html) | science | 5 | 3 |
| [What is a tsunami?](https://oceanservice.noaa.gov/facts/tsunami.html) | science | 4 | 2 |
| [What is an estuary?](https://oceanservice.noaa.gov/facts/estuary.html) | science | 7 | 3 |
| [What is a "mangrove" forest?](https://oceanservice.noaa.gov/facts/mangroves.html) | science | 5 | 3 |
| [What is a kelp forest?](https://oceanservice.noaa.gov/facts/kelp.html) | science | 6 | 3 |
| [Sarcoidosis: MedlinePlus Medical Encyclopedia](https://medlineplus.gov/ency/article/000076.htm) | health | 7 | 3 |
| [High blood pressure in adults - hypertension: MedlinePlus Medical Encyclopedia](https://medlineplus.gov/ency/article/000468.htm) | health | 7 | 3 |
| [Hepatitis B: MedlinePlus Medical Encyclopedia](https://medlineplus.gov/ency/article/000279.htm) | health | 7 | 3 |
| [Type 2 diabetes: MedlinePlus Medical Encyclopedia](https://medlineplus.gov/ency/article/000313.htm) | health | 7 | 3 |
| [Factor XII (Hageman factor) deficiency: MedlinePlus Medical Encyclopedia](https://medlineplus.gov/ency/article/000545.htm) | health | 7 | 3 |
| [Strep throat: MedlinePlus Medical Encyclopedia](https://medlineplus.gov/ency/article/000639.htm) | health | 7 | 3 |
| [Heart failure: MedlinePlus Medical Encyclopedia](https://medlineplus.gov/ency/article/000158.htm) | health | 7 | 3 |
| [Sclerosing cholangitis: MedlinePlus Medical Encyclopedia](https://medlineplus.gov/ency/article/000285.htm) | health | 7 | 3 |
| [Malaria](https://www.who.int/news-room/fact-sheets/detail/malaria) | health | 7 | 3 |
| [Tuberculosis](https://www.who.int/news-room/fact-sheets/detail/tuberculosis) | health | 6 | 3 |
| [Diabetes](https://www.who.int/news-room/fact-sheets/detail/diabetes) | health | 7 | 3 |
| [Asthma](https://www.who.int/news-room/fact-sheets/detail/asthma) | health | 7 | 3 |
| [Hypertension](https://www.who.int/news-room/fact-sheets/detail/hypertension) | health | 7 | 3 |
| [Dengue](https://www.who.int/news-room/fact-sheets/detail/dengue-and-severe-dengue) | health | 7 | 3 |
| [Measles](https://www.who.int/news-room/fact-sheets/detail/measles) | health | 7 | 3 |
| [Physical activity](https://www.who.int/news-room/fact-sheets/detail/physical-activity) | health | 7 | 3 |
| [Causes and Effects of Climate Change \| United Nations](https://www.un.org/en/climatechange/science/causes-effects-climate-change) | explainer | 7 | 3 |
| [What is renewable energy? \| United Nations](https://www.un.org/en/climatechange/what-is-renewable-energy) | explainer | 7 | 3 |
| [Renewable energy – powering a safer future \| United Nations](https://www.un.org/en/climatechange/raising-ambition/renewable-energy) | explainer | 7 | 3 |
| [Greenwashing – the deceptive tactics behind environmental claims \| United Nations](https://www.un.org/en/climatechange/science/climate-issues/greenwashing) | explainer | 7 | 3 |
| [Food and Climate Change: Healthy diets for a healthier planet \| United Nations](https://www.un.org/en/climatechange/science/climate-issues/food) | explainer | 7 | 3 |
| [Adapting to the impacts of climate change \| United Nations](https://www.un.org/en/climatechange/climate-adaptation) | explainer | 7 | 3 |
| [Net Zero Coalition \| United Nations](https://www.un.org/en/climatechange/net-zero-coalition) | explainer | 7 | 3 |
| [The Paris Agreement \| United Nations](https://www.un.org/en/climatechange/paris-agreement) | explainer | 7 | 3 |
| [1940 \| Timeline of Computer History \| Computer History Museum](https://www.computerhistory.org/timeline/1940/) | history | 4 | 1 |
| [1946 \| Timeline of Computer History \| Computer History Museum](https://www.computerhistory.org/timeline/1946/) | history | 7 | 3 |
| [1951 \| Timeline of Computer History \| Computer History Museum](https://www.computerhistory.org/timeline/1951/) | history | 7 | 3 |
| [1956 \| Timeline of Computer History \| Computer History Museum](https://www.computerhistory.org/timeline/1956/) | history | 7 | 3 |
| [1964 \| Timeline of Computer History \| Computer History Museum](https://www.computerhistory.org/timeline/1964/) | history | 7 | 3 |
| [1971 \| Timeline of Computer History \| Computer History Museum](https://www.computerhistory.org/timeline/1971/) | history | 7 | 3 |
| [1984 \| Timeline of Computer History \| Computer History Museum](https://www.computerhistory.org/timeline/1984/) | history | 7 | 3 |
| [1991 \| Timeline of Computer History \| Computer History Museum](https://www.computerhistory.org/timeline/1991/) | history | 7 | 3 |
| [What is biodiversity and why does its loss matter? \| Natural History Museum](https://www.nhm.ac.uk/discover/what-is-biodiversity.html) | science | 7 | 3 |
| [Seven insect heroes of pollination \| Natural History Museum](https://www.nhm.ac.uk/discover/insect-pollination.html) | science | 7 | 3 |
| [How are dinosaur fossils formed? \| Natural History Museum](https://www.nhm.ac.uk/discover/how-are-fossils-formed.html) | science | 7 | 3 |
| [Meet the monsters of the Jurassic seas \| Natural History Museum](https://www.nhm.ac.uk/discover/meet-the-monsters-of-the-jurassic-seas.html) | science | 7 | 3 |
| [What is natural selection? \| Natural History Museum](https://www.nhm.ac.uk/discover/what-is-natural-selection.html) | science | 7 | 3 |
| [Convergent evolution explained with 13 examples \| Natural History Museum](https://www.nhm.ac.uk/discover/convergent-evolution.html) | science | 7 | 3 |
| [What killed the dinosaurs? \| Natural History Museum](https://www.nhm.ac.uk/discover/dinosaur-extinction.html) | science | 7 | 3 |
| [What is climate change and why does it matter? \| Natural History Museum](https://www.nhm.ac.uk/discover/what-is-climate-change-why-does-it-matter.html) | science | 7 | 3 |
| [Variables · The Julia Language](https://docs.julialang.org/en/v1/manual/variables/) | manual | 8 | 4 |
| [Integers and Floating-Point Numbers · The Julia Language](https://docs.julialang.org/en/v1/manual/integers-and-floating-point-numbers/) | manual | 9 | 5 |
| [Strings · The Julia Language](https://docs.julialang.org/en/v1/manual/strings/) | manual | 8 | 4 |
| [Functions · The Julia Language](https://docs.julialang.org/en/v1/manual/functions/) | manual | 9 | 5 |
| [Control Flow · The Julia Language](https://docs.julialang.org/en/v1/manual/control-flow/) | manual | 8 | 4 |
| [Types · The Julia Language](https://docs.julialang.org/en/v1/manual/types/) | manual | 8 | 4 |
| [Methods · The Julia Language](https://docs.julialang.org/en/v1/manual/methods/) | manual | 8 | 4 |
| [Performance Tips · The Julia Language](https://docs.julialang.org/en/v1/manual/performance-tips/) | manual | 8 | 4 |
| [Enum — Elixir v1.20.4](https://elixir.hexdocs.pm/Enum.html) | api | 7 | 4 |
| [Map — Elixir v1.20.4](https://elixir.hexdocs.pm/Map.html) | api | 7 | 4 |
| [String — Elixir v1.20.4](https://elixir.hexdocs.pm/String.html) | api | 7 | 4 |
| [List — Elixir v1.20.4](https://elixir.hexdocs.pm/List.html) | api | 7 | 4 |
| [GenServer — Elixir v1.20.4](https://elixir.hexdocs.pm/GenServer.html) | api | 7 | 4 |
| [Task — Elixir v1.20.4](https://elixir.hexdocs.pm/Task.html) | api | 7 | 4 |
| [Introduction — Elixir v1.20.4](https://elixir.hexdocs.pm/introduction.html) | api | 7 | 4 |
| [Pattern matching — Elixir v1.20.4](https://elixir.hexdocs.pm/pattern-matching.html) | api | 7 | 4 |
| [ffmpeg Documentation](https://ffmpeg.org/ffmpeg.html) | manual | 8 | 4 |
| [FFmpeg Formats Documentation](https://ffmpeg.org/ffmpeg-formats.html) | manual | 8 | 4 |
| [FFmpeg Codecs Documentation](https://ffmpeg.org/ffmpeg-codecs.html) | manual | 8 | 4 |
| [FFmpeg Protocols Documentation](https://ffmpeg.org/ffmpeg-protocols.html) | manual | 8 | 4 |
| [FFmpeg Utilities Documentation](https://ffmpeg.org/ffmpeg-utils.html) | manual | 8 | 4 |
| [ffplay Documentation](https://ffmpeg.org/ffplay.html) | manual | 8 | 4 |
| [ffprobe Documentation](https://ffmpeg.org/ffprobe.html) | manual | 8 | 4 |
| [FFmpeg FAQ](https://ffmpeg.org/faq.html) | manual | 8 | 4 |
| [Anatomy of components • Angular](https://angular.dev/guide/components) | docs | 8 | 4 |
| [Accepting data with input properties • Angular](https://angular.dev/guide/components/inputs) | docs | 8 | 4 |
| [Templates • Overview • Angular](https://angular.dev/guide/templates) | docs | 8 | 4 |
| [Dependency Injection • Overview • Angular](https://angular.dev/guide/di) | docs | 8 | 4 |
| [Routing • Overview • Angular](https://angular.dev/guide/routing) | docs | 7 | 3 |
| [Forms • Overview • Angular](https://angular.dev/guide/forms) | docs | 9 | 5 |
| [Strictly typed reactive forms • Angular](https://angular.dev/guide/forms/typed-forms) | docs | 8 | 4 |
| [HTTP Client • Overview • Angular](https://angular.dev/guide/http) | docs | 6 | 2 |
| [Describing the UI – React](https://react.dev/learn/describing-the-ui) | tutorial | 8 | 4 |
| [Writing Markup with JSX – React](https://react.dev/learn/writing-markup-with-jsx) | tutorial | 8 | 4 |
| [Conditional Rendering – React](https://react.dev/learn/conditional-rendering) | tutorial | 8 | 4 |
| [Rendering Lists – React](https://react.dev/learn/rendering-lists) | tutorial | 8 | 4 |
| [Responding to Events – React](https://react.dev/learn/responding-to-events) | tutorial | 9 | 5 |
| [Updating Objects in State – React](https://react.dev/learn/updating-objects-in-state) | tutorial | 8 | 4 |
| [Synchronizing with Effects – React](https://react.dev/learn/synchronizing-with-effects) | tutorial | 8 | 4 |
| [You Might Not Need an Effect – React](https://react.dev/learn/you-might-not-need-an-effect) | tutorial | 8 | 4 |
| [Examples \| Nmap Network Scanning](https://nmap.org/book/man-examples.html) | manual | 6 | 3 |
| [Port Scanning Basics \| Nmap Network Scanning](https://nmap.org/book/man-port-scanning-basics.html) | manual | 6 | 3 |
| [Host Discovery \| Nmap Network Scanning](https://nmap.org/book/man-host-discovery.html) | manual | 6 | 3 |
| [Port Scanning Techniques \| Nmap Network Scanning](https://nmap.org/book/man-port-scanning-techniques.html) | manual | 6 | 3 |
| [Service and Version Detection \| Nmap Network Scanning](https://nmap.org/book/man-version-detection.html) | manual | 6 | 3 |
| [OS Detection \| Nmap Network Scanning](https://nmap.org/book/man-os-detection.html) | manual | 6 | 3 |
| [Nmap Scripting Engine (NSE) \| Nmap Network Scanning](https://nmap.org/book/man-nse.html) | manual | 6 | 3 |
| [Timing and Performance \| Nmap Network Scanning](https://nmap.org/book/man-performance.html) | manual | 6 | 3 |

### scale-new-sites

| Page | Kind | Checks | Critical |
| --- | --- | ---: | ---: |
| [Broadcasting — NumPy v2.5 Manual](https://numpy.org/doc/stable/user/basics.broadcasting.html) | api | 8 | 4 |
| [Indexing on ndarrays — NumPy v2.5 Manual](https://numpy.org/doc/stable/user/basics.indexing.html) | api | 8 | 4 |
| [Copies and views — NumPy v2.5 Manual](https://numpy.org/doc/stable/user/basics.copies.html) | api | 8 | 4 |
| [Data types — NumPy v2.5 Manual](https://numpy.org/doc/stable/user/basics.types.html) | api | 9 | 5 |
| [Integration (scipy.integrate) — SciPy v1.18.0 Manual](https://docs.scipy.org/doc/scipy/tutorial/integrate.html) | tutorial | 8 | 4 |
| [Optimization (scipy.optimize) — SciPy v1.18.0 Manual](https://docs.scipy.org/doc/scipy/tutorial/optimize.html) | tutorial | 9 | 5 |
| [Interpolation (scipy.interpolate) — SciPy v1.18.0 Manual](https://docs.scipy.org/doc/scipy/tutorial/interpolate.html) | tutorial | 6 | 3 |
| [Discrete Fourier Transforms (scipy.fft) — SciPy v1.18.0 Manual](https://docs.scipy.org/doc/scipy/tutorial/fft.html) | tutorial | 8 | 4 |
| [Workflow syntax for GitHub Actions - GitHub Docs](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax) | configuration | 9 | 5 |
| [Secure use reference - GitHub Docs](https://docs.github.com/en/actions/reference/security/secure-use) | configuration | 8 | 4 |
| [Running variations of jobs in a workflow - GitHub Docs](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/run-job-variations) | configuration | 8 | 4 |
| [Reuse workflows - GitHub Docs](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows) | configuration | 8 | 4 |
| [?? and ??= operators - null-coalescing operators - C# reference \| Microsoft Learn](https://learn.microsoft.com/en-us/dotnet/csharp/language-reference/operators/null-coalescing-operator) | docs | 8 | 4 |
| [Patterns - Pattern matching using the is and switch expressions. - C# reference \| Microsoft Learn](https://learn.microsoft.com/en-us/dotnet/csharp/language-reference/operators/patterns) | docs | 8 | 4 |
| [Async return types - C# \| Microsoft Learn](https://learn.microsoft.com/en-us/dotnet/csharp/asynchronous-programming/async-return-types) | docs | 8 | 4 |
| [Exception Handling - C# \| Microsoft Learn](https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/exceptions/exception-handling) | docs | 8 | 4 |
| [Inspect network activity \| Chrome DevTools \| Chrome for Developers](https://developer.chrome.com/docs/devtools/network/) | docs | 7 | 3 |
| [Analyze runtime performance \| Chrome DevTools \| Chrome for Developers](https://developer.chrome.com/docs/devtools/performance/) | docs | 7 | 3 |
| [Console overview \| Chrome DevTools \| Chrome for Developers](https://developer.chrome.com/docs/devtools/console/) | docs | 8 | 4 |
| [Fix memory problems \| Chrome DevTools \| Chrome for Developers](https://developer.chrome.com/docs/devtools/memory-problems/) | docs | 8 | 4 |
| [A toy DNS resolver](https://jvns.ca/blog/2022/02/01/a-dns-resolver-in-80-lines-of-go/) | blog | 8 | 4 |
| [Some blogging myths](https://jvns.ca/blog/2023/06/05/some-blogging-myths/) | blog | 7 | 3 |
| [How to teach yourself hard things](https://jvns.ca/blog/2018/09/01/learning-skills-you-can-practice/) | blog | 7 | 3 |
| [How does gdb work?](https://jvns.ca/blog/2016/08/10/how-does-gdb-work/) | blog | 8 | 4 |
| [Implementing Raft: Part 1 - Elections - Eli Bendersky's website](https://eli.thegreenplace.net/2020/implementing-raft-part-1-elections/) | blog | 8 | 4 |
| [Implementing Raft: Part 2 - Commands and Log Replication - Eli Bendersky's website](https://eli.thegreenplace.net/2020/implementing-raft-part-2-commands-and-log-replication/) | blog | 8 | 4 |
| [Implementing Raft: Part 3 - Persistence and Optimizations - Eli Bendersky's website](https://eli.thegreenplace.net/2020/implementing-raft-part-3-persistence-and-optimizations/) | blog | 8 | 4 |
| [Preview: ranging over functions in Go - Eli Bendersky's website](https://eli.thegreenplace.net/2023/preview-ranging-over-functions-in-go/) | blog | 8 | 4 |
| [The USE Method](https://www.brendangregg.com/usemethod.html) | guide | 7 | 4 |
| [Performance Analysis Methodology](https://www.brendangregg.com/methodology.html) | guide | 6 | 3 |
| [Flame Graphs](https://www.brendangregg.com/flamegraphs.html) | guide | 6 | 3 |
| [Off-CPU Analysis](https://www.brendangregg.com/offcpuanalysis.html) | guide | 7 | 4 |
| [The activity lifecycle \| App architecture \| Android Developers](https://developer.android.com/guide/components/activities/activity-lifecycle) | docs | 8 | 4 |
| [Services overview \| Background work \| Android Developers](https://developer.android.com/develop/background-work/services) | docs | 8 | 4 |
| [Broadcasts overview \| Background work \| Android Developers](https://developer.android.com/develop/background-work/background-tasks/broadcasts) | docs | 8 | 4 |
| [App manifest overview \| App architecture \| Android Developers](https://developer.android.com/guide/topics/manifest/manifest-intro) | docs | 9 | 5 |
| [Redis Strings \| Docs](https://redis.io/docs/latest/develop/data-types/strings/) | database | 8 | 4 |
| [Redis hashes \| Docs](https://redis.io/docs/latest/develop/data-types/hashes/) | database | 9 | 5 |
| [Redis lists \| Docs](https://redis.io/docs/latest/develop/data-types/lists/) | database | 8 | 4 |
| [Redis sets \| Docs](https://redis.io/docs/latest/develop/data-types/sets/) | database | 8 | 4 |
| [Asthma - NHS](https://www.nhs.uk/conditions/asthma/) | health | 7 | 3 |
| [Type 2 diabetes - NHS](https://www.nhs.uk/conditions/type-2-diabetes/) | health | 3 | 1 |
| [High blood pressure - NHS](https://www.nhs.uk/conditions/high-blood-pressure/) | health | 7 | 3 |
| [Dehydration - NHS](https://www.nhs.uk/conditions/dehydration/) | health | 7 | 3 |
| [Recycling Basics and Benefits \| US EPA](https://www.epa.gov/recycle/recycling-basics-and-benefits) | science | 7 | 3 |
| [Effects of Acid Rain \| US EPA](https://www.epa.gov/acidrain/effects-acid-rain) | science | 7 | 3 |
| [What is Acid Rain? \| US EPA](https://www.epa.gov/acidrain/what-acid-rain) | science | 7 | 3 |
| [Acid Rain Program \| US EPA](https://www.epa.gov/acidrain/acid-rain-program) | science | 7 | 3 |
| [Banana Banana Bread Recipe (with Video)](https://www.allrecipes.com/recipe/20144/banana-banana-bread/) | recipe | 7 | 3 |
| [Best Brownies Recipe (with Video)](https://www.allrecipes.com/recipe/10549/best-brownies/) | recipe | 7 | 3 |
| [Best Chocolate Chip Cookies Recipe (with Video)](https://www.allrecipes.com/recipe/10813/best-chocolate-chip-cookies/) | recipe | 7 | 3 |
| [Easy Meatloaf Recipe (with Video)](https://www.allrecipes.com/recipe/16354/easy-meatloaf/) | recipe | 7 | 3 |
| [Lightning Safety Tips and Resources](https://www.weather.gov/safety/lightning) | safety | 4 | 3 |
| [Tornado Safety](https://www.weather.gov/safety/tornado) | safety | 4 | 3 |
| [Flood Safety Tips and Resources](https://www.weather.gov/safety/flood) | safety | 3 | 2 |
| [Heat Safety Tips and Resources](https://www.weather.gov/safety/heat) | safety | 4 | 3 |
| [Earthquake Safety \| Earthquake Preparedness \| Red Cross](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/earthquake.html) | safety | 7 | 3 |
| [Flood Safety \| Flood Preparedness \| American Red Cross](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/flood.html) | safety | 7 | 3 |
| [Hurricane Preparedness \| Red Cross](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/hurricane.html) | safety | 7 | 3 |
| [Tornado Safety Tips \| Tornado Preparedness \| Red Cross](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/tornado.html) | safety | 7 | 3 |
| [Robin Red Breast Bird Facts \| Erithacus rubecula](https://www.rspb.org.uk/birds-and-wildlife/robin) | nature | 7 | 3 |
| [Blackbirds \| Facts about Male & Female Blackbirds](https://www.rspb.org.uk/birds-and-wildlife/blackbird) | nature | 7 | 3 |
| [Blue Tit Bird Facts \| Cyanistes Caeruleus](https://www.rspb.org.uk/birds-and-wildlife/blue-tit) | nature | 7 | 3 |
| [House Sparrow Bird Facts \| Passer Domesticus](https://www.rspb.org.uk/birds-and-wildlife/house-sparrow) | nature | 7 | 3 |
| [Earthquake \| Definition, Causes, Effects, & Facts \| Britannica](https://www.britannica.com/science/earthquake-geology) | encyclopedia | 7 | 3 |
| [Plate tectonics \| Definition, Theory, Facts, & Evidence \| Britannica](https://www.britannica.com/science/plate-tectonics) | encyclopedia | 7 | 3 |
| [Antibiotics - NHS](https://www.nhs.uk/medicines/antibiotics/) | health | 7 | 3 |
| [Continental drift \| Definition, Evidence, Diagram, & Facts \| Britannica](https://www.britannica.com/science/continental-drift-geology) | encyclopedia | 6 | 3 |
| [Silk Road - World History Encyclopedia](https://www.worldhistory.org/Silk_Road/) | history | 7 | 3 |
| [Ancient Egypt: The Land of the Gods of Balance and Harmony - World History Encyclopedia](https://www.worldhistory.org/egypt/) | history | 7 | 3 |
| [Roman Republic - World History Encyclopedia](https://www.worldhistory.org/Roman_Republic/) | history | 7 | 3 |
| [Mesopotamia: The Beginning of Beginnings - World History Encyclopedia](https://www.worldhistory.org/Mesopotamia/) | history | 7 | 3 |
| [Changes to the Nutrition Facts Label \| FDA](https://www.fda.gov/food/nutrition-food-labeling-and-critical-foods/changes-nutrition-facts-label?scrlybrkr=) | consumer-guide | 7 | 3 |
| [Safe Food Handling \| FDA](https://www.fda.gov/food/buy-store-serve-safe-food/safe-food-handling) | consumer-guide | 8 | 4 |
| [Antibiotics - Side effects - NHS](https://www.nhs.uk/medicines/antibiotics/side-effects/) | health | 7 | 3 |
| [Is It Really 'FDA Approved'? \| FDA](https://www.fda.gov/consumers/consumer-updates/it-really-fda-approved) | consumer-guide | 7 | 3 |
| [Virtue Ethics (Stanford Encyclopedia of Philosophy)](https://plato.stanford.edu/entries/ethics-virtue/) | reference | 6 | 3 |
| [Consciousness (Stanford Encyclopedia of Philosophy)](https://plato.stanford.edu/entries/consciousness/) | reference | 6 | 3 |
| [Scientific Method (Stanford Encyclopedia of Philosophy)](https://plato.stanford.edu/entries/scientific-method/) | reference | 6 | 3 |
| [Classical Logic (Stanford Encyclopedia of Philosophy)](https://plato.stanford.edu/entries/logic-classical/) | reference | 6 | 3 |
