# Ketch extraction benchmark

2026-09-20T17:23:27Z · linux/amd64 · 24 logical CPUs

Binary build: go1.27.1, CGO_ENABLED=1.

Mode: **default**, extract mode **clean**. 7 measured runs + 1 warmups per page, 1 workers. Wall time 142.50s.

Corpus SHA256: `14c3ccc4f5eb5b6ef0bec2ca0727871d682d5845560ff7bce8bc70f8b719f441`  
Binary SHA256: `b4c38e6c2591bac8899fe3642f31f8c3e1aa0d04bfff80b0ce198934758715a5`

**3465/3573 checks passed; 1819/1831 critical checks; 0 failed or nondeterministic pages.**

Token coverage compares independently annotated source text with parsed Markdown. It is a multiset measure, not semantic correctness. Code, headings, table associations, links and exclusions are checked separately. Scores include every page; pages have equal weight in macro averages. Per-invocation timing includes CLI startup and excludes build, source loading, network and grading. Wall time includes warmups, grading and artifact writes. Selector mode is assisted recovery, not default extraction.

Macro token recall **99.0%**, precision **99.4%**, F1 **99.2%**.

| Page | Kind | Recall | Precision | Checks | Critical | Median | p95 | Output |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| [npm](https://docs.npmjs.com/trusted-publishers/) | docs | 99.7% | 100.0% | 6/6 | 4/4 | 41.02ms | 41.71ms | 20172 B |
| [go](https://pkg.go.dev/context) | api | 100.0% | 98.3% | 7/7 | 5/5 | 22.42ms | 23.64ms | 26801 B |
| [python](https://docs.python.org/3/tutorial/errors.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 20.70ms | 21.51ms | 26313 B |
| [kubernetes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) | configuration | 97.3% | 100.0% | 6/6 | 4/4 | 39.48ms | 41.63ms | 19931 B |
| [postgres](https://www.postgresql.org/docs/current/transaction-iso.html) | database | 100.0% | 100.0% | 6/6 | 4/4 | 22.76ms | 23.48ms | 25062 B |
| [mdn](https://developer.mozilla.org/en-US/docs/Web/API/AbortController) | api | 95.7% | 100.0% | 7/7 | 4/4 | 13.01ms | 13.55ms | 2614 B |
| [rust](https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 18.37ms | 20.15ms | 28278 B |
| [sqlite](https://www.sqlite.org/lang_transaction.html) | database | 100.0% | 100.0% | 6/6 | 3/3 | 11.87ms | 13.33ms | 9515 B |
| [git](https://git-scm.com/docs/git-reset) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 27.13ms | 30.52ms | 19815 B |
| [gnu](https://www.gnu.org/software/coreutils/manual/html_node/Exit-status.html) | manual | 100.0% | 100.0% | 5/5 | 3/3 | 6.32ms | 6.61ms | 777 B |
| [terraform](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle) | configuration | 99.1% | 99.4% | 5/5 | 4/4 | 25.68ms | 29.34ms | 19244 B |
| [docker](https://docs.docker.com/build/building/multi-stage/) | tutorial | 100.0% | 100.0% | 5/5 | 4/4 | 37.14ms | 37.52ms | 6914 B |
| [cloudflare](https://blog.cloudflare.com/rfc-9457-agent-error-pages/) | blog | 99.8% | 99.7% | 7/7 | 5/5 | 38.57ms | 41.40ms | 18089 B |
| [danluu](https://danluu.com/slow-device/) | essay | 100.0% | 100.0% | 5/5 | 3/3 | 29.07ms | 29.59ms | 72798 B |
| [joel](https://www.joelonsoftware.com/2000/04/06/things-you-should-never-do-part-i/) | blog | 100.0% | 99.0% | 4/4 | 4/4 | 10.70ms | 11.18ms | 8770 B |
| [rfc](https://www.rfc-editor.org/rfc/rfc8259.html) | standard | 100.0% | 100.0% | 4/4 | 4/4 | 13.46ms | 13.61ms | 28586 B |
| [wikipedia](https://en.wikipedia.org/wiki/HTTP) | encyclopedia | 99.8% | 99.8% | 6/6 | 4/4 | 84.97ms | 91.25ms | 106256 B |
| [gutenberg](https://www.gutenberg.org/files/11/11-h/11-h.htm) | book | 100.0% | 99.6% | 4/4 | 3/3 | 51.02ms | 51.90ms | 152803 B |
| [nasa](https://science.nasa.gov/mars/facts/) | science | 92.5% | 99.4% | 5/5 | 4/4 | 21.29ms | 22.29ms | 9994 B |
| [nps](https://www.nps.gov/yell/planyourvisit/safety.htm) | visitor-guide | 99.4% | 100.0% | 9/9 | 5/5 | 23.69ms | 25.09ms | 25369 B |
| [npm-scripts](https://docs.npmjs.com/cli/v11/using-npm/scripts/) | docs | 100.0% | 99.6% | 8/8 | 4/4 | 44.50ms | 45.38ms | 15326 B |
| [npm-package-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-json/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 66.54ms | 69.04ms | 38256 B |
| [go-io](https://pkg.go.dev/io) | api | 100.0% | 98.1% | 8/8 | 4/4 | 37.62ms | 39.22ms | 41338 B |
| [go-strings](https://pkg.go.dev/strings) | api | 100.0% | 99.1% | 7/8 | 4/4 | 50.72ms | 51.29ms | 58885 B |
| [python-data](https://docs.python.org/3/tutorial/datastructures.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 23.24ms | 24.20ms | 26461 B |
| [python-classes](https://docs.python.org/3/tutorial/classes.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 25.51ms | 28.73ms | 38897 B |
| [kubernetes-deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 49.60ms | 53.37ms | 61534 B |
| [kubernetes-configmaps](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 46.33ms | 47.70ms | 35667 B |
| [postgres-constraints](https://www.postgresql.org/docs/current/ddl-constraints.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 21.16ms | 22.10ms | 23853 B |
| [postgres-queries](https://www.postgresql.org/docs/current/queries-table-expressions.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 30.30ms | 31.27ms | 35404 B |
| [mdn-promise](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Promise) | api | 99.9% | 100.0% | 9/9 | 5/5 | 25.24ms | 25.99ms | 35051 B |
| [mdn-fetch](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch) | api | 100.0% | 100.0% | 8/8 | 4/4 | 24.67ms | 26.04ms | 30351 B |
| [rust-enums](https://doc.rust-lang.org/book/ch06-01-defining-an-enum.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 14.78ms | 15.20ms | 17267 B |
| [rust-result](https://doc.rust-lang.org/book/ch09-02-recoverable-errors-with-result.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 19.68ms | 21.96ms | 28175 B |
| [sqlite-select](https://www.sqlite.org/lang_select.html) | database | 94.4% | 100.0% | 7/7 | 4/4 | 159.40ms | 163.06ms | 39004 B |
| [sqlite-foreignkeys](https://www.sqlite.org/foreignkeys.html) | database | 99.0% | 100.0% | 7/7 | 4/4 | 21.70ms | 23.50ms | 38101 B |
| [git-rebase](https://git-scm.com/docs/git-rebase) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 58.57ms | 60.53ms | 57762 B |
| [git-restore](https://git-scm.com/docs/git-restore) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 18.04ms | 19.44ms | 7806 B |
| [gnu-copy](https://www.gnu.org/software/coreutils/manual/html_node/cp-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 28.16ms | 28.41ms | 22373 B |
| [gnu-timeout](https://www.gnu.org/software/coreutils/manual/html_node/timeout-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 11.31ms | 11.79ms | 5420 B |
| [terraform-foreach](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each) | configuration | 100.0% | 99.7% | 8/8 | 4/4 | 23.63ms | 26.80ms | 15748 B |
| [terraform-count](https://developer.hashicorp.com/terraform/language/meta-arguments/count) | configuration | 100.0% | 99.4% | 8/8 | 4/4 | 19.41ms | 19.57ms | 7140 B |
| [docker-volumes](https://docs.docker.com/engine/storage/volumes/) | docs | 100.0% | 94.8% | 9/9 | 5/5 | 52.15ms | 52.98ms | 27132 B |
| [docker-bridge](https://docs.docker.com/engine/network/drivers/bridge/) | docs | 100.0% | 97.3% | 9/9 | 5/5 | 47.72ms | 49.06ms | 25782 B |
| [cloudflare-pingora](https://blog.cloudflare.com/how-we-built-pingora-the-proxy-that-connects-cloudflare-to-the-internet/) | blog | 99.9% | 99.7% | 7/7 | 3/3 | 28.29ms | 30.89ms | 16846 B |
| [cloudflare-opensource](https://blog.cloudflare.com/pingora-open-source/) | blog | 99.8% | 99.7% | 8/8 | 4/4 | 33.04ms | 35.71ms | 12380 B |
| [danluu-branch](https://danluu.com/branch-prediction/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 16.69ms | 17.80ms | 36462 B |
| [danluu-malloc](https://danluu.com/malloc-tutorial/) | essay | 100.0% | 100.0% | 6/6 | 4/4 | 12.74ms | 13.70ms | 21111 B |
| [joel-test](https://www.joelonsoftware.com/2000/08/09/the-joel-test-12-steps-to-better-code/) | blog | 100.0% | 99.6% | 5/5 | 3/3 | 14.26ms | 14.69ms | 21694 B |
| [joel-leaky](https://www.joelonsoftware.com/2002/11/11/the-law-of-leaky-abstractions/) | blog | 100.0% | 99.4% | 5/5 | 3/3 | 11.47ms | 11.74ms | 12962 B |
| [rfc-http](https://www.rfc-editor.org/rfc/rfc9110.html) | standard | 99.9% | 100.0% | 7/7 | 4/4 | 324.39ms | 330.46ms | 721134 B |
| [rfc-uri](https://www.rfc-editor.org/rfc/rfc3986.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 30.64ms | 32.76ms | 142714 B |
| [wikipedia-photosynthesis](https://en.wikipedia.org/wiki/Photosynthesis) | encyclopedia | 99.6% | 99.9% | 8/8 | 4/4 | 112.29ms | 114.06ms | 205015 B |
| [wikipedia-binary](https://en.wikipedia.org/wiki/Binary_search) | encyclopedia | 92.3% | 99.9% | 8/8 | 4/4 | 97.43ms | 99.16ms | 127463 B |
| [gutenberg-wallpaper](https://www.gutenberg.org/files/1952/1952-h/1952-h.htm) | book | 100.0% | 99.8% | 4/4 | 3/3 | 16.45ms | 17.62ms | 32151 B |
| [gutenberg-metamorphosis](https://www.gutenberg.org/files/5200/5200-h/5200-h.htm) | book | 100.0% | 99.8% | 3/3 | 3/3 | 29.36ms | 29.68ms | 120067 B |
| [nasa-earth](https://science.nasa.gov/earth/facts/) | science | 96.1% | 100.0% | 6/7 | 2/3 | 21.70ms | 22.32ms | 10145 B |
| [nasa-jupiter](https://science.nasa.gov/jupiter/jupiter-facts/) | science | 99.1% | 100.0% | 7/7 | 3/3 | 19.72ms | 19.99ms | 12956 B |
| [nps-hiking](https://www.nps.gov/grca/planyourvisit/hiking-faq.htm) | visitor-guide | 99.8% | 100.0% | 6/6 | 3/3 | 22.17ms | 23.91ms | 28983 B |
| [nps-canyon](https://www.nps.gov/grca/faqs.htm) | visitor-guide | 94.4% | 100.0% | 7/7 | 3/3 | 20.24ms | 21.50ms | 23607 B |
| [pandas-indexing](https://pandas.pydata.org/docs/user_guide/indexing.html) | api | 99.7% | 100.0% | 9/9 | 5/5 | 58.75ms | 59.77ms | 98569 B |
| [pandas-merge](https://pandas.pydata.org/docs/user_guide/merging.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 36.09ms | 38.01ms | 54464 B |
| [django-queries](https://docs.djangoproject.com/en/5.2/topics/db/queries/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 52.46ms | 53.29ms | 85748 B |
| [django-transactions](https://docs.djangoproject.com/en/5.2/topics/db/transactions/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 25.23ms | 26.06ms | 35970 B |
| [curl-manpage](https://curl.se/docs/manpage.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 200.43ms | 204.43ms | 334470 B |
| [curl-http](https://curl.se/docs/httpscripting.html) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 18.57ms | 20.22ms | 28331 B |
| [w3c-tables](https://www.w3.org/WAI/tutorials/tables/one-header/) | accessibility | 100.0% | 94.5% | 9/9 | 5/5 | 11.79ms | 12.36ms | 4734 B |
| [w3c-irregular](https://www.w3.org/WAI/tutorials/tables/irregular/) | accessibility | 100.0% | 96.7% | 8/8 | 4/4 | 13.54ms | 15.29ms | 7013 B |
| [owid-life](https://ourworldindata.org/life-expectancy) | data-article | 99.4% | 100.0% | 6/8 | 4/4 | 30.15ms | 31.38ms | 40559 B |
| [owid-population](https://ourworldindata.org/population-growth) | data-article | 99.0% | 100.0% | 6/8 | 4/4 | 25.74ms | 26.58ms | 30235 B |
| [eff-passwords](https://ssd.eff.org/module/creating-strong-passwords) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 12.69ms | 13.13ms | 8678 B |
| [eff-plan](https://ssd.eff.org/module/your-security-plan) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 12.46ms | 13.20ms | 9438 B |
| [paulgraham-lisp](https://www.paulgraham.com/avg.html) | essay | 99.3% | 100.0% | 3/4 | 3/3 | 15.85ms | 16.15ms | 25543 B |
| [paulgraham-schedule](https://www.paulgraham.com/makersschedule.html) | essay | 98.5% | 100.0% | 3/4 | 3/3 | 10.10ms | 10.36ms | 6665 B |
| [fowler-microservices](https://martinfowler.com/articles/microservices.html) | essay | 90.0% | 96.4% | 6/7 | 3/3 | 22.01ms | 26.07ms | 46917 B |
| [fowler-debt](https://martinfowler.com/bliki/TechnicalDebt.html) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 9.91ms | 11.07ms | 7491 B |
| [letsencrypt-challenges](https://letsencrypt.org/docs/challenge-types/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 10.31ms | 10.87ms | 7690 B |
| [letsencrypt-compatibility](https://letsencrypt.org/docs/certificate-compatibility/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 9.74ms | 10.43ms | 5133 B |
| [noaa-salt](https://oceanservice.noaa.gov/facts/whysalty.html) | science | 100.0% | 100.0% | 6/6 | 3/3 | 10.46ms | 10.82ms | 3082 B |
| [noaa-blue](https://oceanservice.noaa.gov/facts/oceanblue.html) | science | 93.0% | 100.0% | 5/6 | 3/3 | 9.83ms | 10.94ms | 743 B |
| [bgs-earthquakes](https://www.bgs.ac.uk/discovering-geology/earth-hazards/earthquakes/how-are-earthquakes-detected/) | science | 97.6% | 100.0% | 8/8 | 4/4 | 19.41ms | 19.83ms | 17288 B |
| [medlineplus-handwashing](https://medlineplus.gov/ency/patientinstructions/000972.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.92ms | 12.26ms | 5154 B |
| [who-resistance](https://www.who.int/news-room/fact-sheets/detail/antimicrobial-resistance) | health | 100.0% | 97.6% | 7/7 | 3/3 | 16.11ms | 17.83ms | 12102 B |
| [un-climate](https://www.un.org/en/climatechange/what-is-climate-change) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 28.06ms | 29.18ms | 11847 B |
| [computerhistory-1969](https://www.computerhistory.org/timeline/1969/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 13.37ms | 14.57ms | 6033 B |
| [nhm-meteorites](https://www.nhm.ac.uk/discover/types-of-meteorites.html) | science | 87.6% | 99.9% | 7/7 | 3/3 | 18.51ms | 19.07ms | 9531 B |
| [standardebooks-austen](https://standardebooks.org/ebooks/jane-austen/pride-and-prejudice) | catalogue | 94.5% | 100.0% | 7/7 | 3/3 | 10.46ms | 11.04ms | 5095 B |
| [archives-declaration](https://www.archives.gov/founding-docs/declaration-transcript) | historical-document | 100.0% | 100.0% | 5/5 | 3/3 | 14.36ms | 15.39ms | 9865 B |
| [wikivoyage-paris](https://en.wikivoyage.org/wiki/Paris) | visitor-guide | 99.7% | 100.0% | 8/8 | 4/4 | 217.58ms | 222.51ms | 227825 B |
| [kingarthur-bread](https://www.kingarthurbaking.com/recipes/classic-sandwich-bread-recipe) | recipe | 95.4% | 100.0% | 10/10 | 6/6 | 22.08ms | 23.20ms | 7161 B |
| [seriouseats-cookies](https://www.seriouseats.com/the-food-lab-best-chocolate-chip-cookie-recipe) | recipe | 97.9% | 100.0% | 9/9 | 5/5 | 44.12ms | 45.07ms | 40204 B |
| [ifixit-moped](https://www.ifixit.com/Guide/Suzuki+FA50+Moped+Exhaust+Cleaning/3926) | repair-guide | 99.1% | 97.4% | 7/7 | 3/3 | 28.18ms | 29.40ms | 3720 B |
| [discourse-guide](https://meta.discourse.org/t/understanding-discourse-for-new-users/96331) | discussion | 100.0% | 96.4% | 8/8 | 4/4 | 18.60ms | 19.18ms | 15673 B |
| [hackernews-startup](https://news.ycombinator.com/item?id=8863) | discussion | 100.0% | 99.7% | 5/5 | 3/3 | 39.62ms | 39.76ms | 34613 B |
| [julia-arrays](https://docs.julialang.org/en/v1/manual/arrays/) | manual | 100.0% | 100.0% | 9/9 | 5/5 | 27.58ms | 29.00ms | 48421 B |
| [elixir-enum](https://elixir.hexdocs.pm/enum-cheat.html) | api | 99.8% | 99.6% | 8/8 | 4/4 | 32.44ms | 35.11ms | 22538 B |
| [ffmpeg-filters](https://ffmpeg.org/ffmpeg-filters.html) | manual | 97.0% | 100.0% | 5/7 | 4/4 | 896.29ms | 923.52ms | 919390 B |
| [angular-signals](https://angular.dev/guide/signals) | docs | 99.7% | 100.0% | 8/8 | 4/4 | 20.92ms | 23.73ms | 12489 B |
| [react-state](https://react.dev/learn/state-a-components-memory) | tutorial | 99.8% | 99.6% | 8/8 | 4/4 | 30.35ms | 32.74ms | 21608 B |
| [ncat-guide](https://nmap.org/ncat/guide/index.html) | manual | 99.5% | 100.0% | 6/6 | 3/3 | 9.60ms | 10.37ms | 4978 B |
| [more-docs-npmjs-com-commands-npm-install](https://docs.npmjs.com/cli/v11/commands/npm-install/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 56.86ms | 57.80ms | 36424 B |
| [more-docs-npmjs-com-commands-npm-ci](https://docs.npmjs.com/cli/v11/commands/npm-ci/) | docs | 100.0% | 99.6% | 8/8 | 4/4 | 38.74ms | 39.20ms | 14366 B |
| [more-docs-npmjs-com-commands-npm-publish](https://docs.npmjs.com/cli/v11/commands/npm-publish/) | docs | 100.0% | 99.3% | 8/8 | 4/4 | 33.16ms | 38.15ms | 8726 B |
| [more-docs-npmjs-com-commands-npm-audit](https://docs.npmjs.com/cli/v11/commands/npm-audit/) | docs | 100.0% | 99.7% | 8/8 | 4/4 | 41.00ms | 42.68ms | 16307 B |
| [more-docs-npmjs-com-commands-npm-exec](https://docs.npmjs.com/cli/v11/commands/npm-exec/) | docs | 100.0% | 99.6% | 8/8 | 4/4 | 37.89ms | 39.06ms | 13605 B |
| [more-docs-npmjs-com-commands-npm-run](https://docs.npmjs.com/cli/v11/commands/npm-run/) | docs | 100.0% | 99.3% | 8/8 | 4/4 | 33.25ms | 35.69ms | 7839 B |
| [more-docs-npmjs-com-configuring-npm-package-lock-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/) | docs | 100.0% | 99.5% | 7/7 | 3/3 | 26.68ms | 27.48ms | 11037 B |
| [more-docs-npmjs-com-using-npm-workspaces](https://docs.npmjs.com/cli/v11/using-npm/workspaces/) | docs | 100.0% | 99.2% | 8/8 | 4/4 | 32.75ms | 33.56ms | 6840 B |
| [more-pkg-go-dev-bytes](https://pkg.go.dev/bytes) | api | 100.0% | 99.3% | 7/8 | 4/4 | 58.51ms | 60.92ms | 75546 B |
| [more-pkg-go-dev-errors](https://pkg.go.dev/errors) | api | 100.0% | 97.6% | 7/8 | 4/4 | 18.94ms | 20.28ms | 16958 B |
| [more-pkg-go-dev-fmt](https://pkg.go.dev/fmt) | api | 100.0% | 99.3% | 7/8 | 4/4 | 30.65ms | 33.04ms | 51134 B |
| [more-pkg-go-dev-sync](https://pkg.go.dev/sync) | api | 99.9% | 98.0% | 8/8 | 4/4 | 30.27ms | 31.98ms | 33963 B |
| [more-pkg-go-dev-time](https://pkg.go.dev/time) | api | 99.9% | 99.3% | 8/8 | 4/4 | 58.05ms | 62.08ms | 94611 B |
| [more-pkg-go-dev-encoding-json](https://pkg.go.dev/encoding/json) | api | 100.0% | 97.9% | 7/8 | 4/4 | 48.28ms | 49.13ms | 77430 B |
| [more-pkg-go-dev-net-http](https://pkg.go.dev/net/http) | api | 99.9% | 98.7% | 7/8 | 4/4 | 97.69ms | 100.66ms | 199228 B |
| [more-pkg-go-dev-os](https://pkg.go.dev/os) | api | 99.8% | 98.8% | 8/8 | 4/4 | 66.34ms | 68.68ms | 93932 B |
| [more-docs-python-org-tutorial-controlflow](https://docs.python.org/3/tutorial/controlflow.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 30.83ms | 31.82ms | 40934 B |
| [more-docs-python-org-tutorial-modules](https://docs.python.org/3/tutorial/modules.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 19.85ms | 23.37ms | 25859 B |
| [more-docs-python-org-tutorial-inputoutput](https://docs.python.org/3/tutorial/inputoutput.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 20.16ms | 20.90ms | 23054 B |
| [more-docs-python-org-tutorial-stdlib](https://docs.python.org/3/tutorial/stdlib.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 16.74ms | 17.56ms | 15475 B |
| [more-docs-python-org-tutorial-stdlib2](https://docs.python.org/3/tutorial/stdlib2.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 17.28ms | 17.74ms | 17559 B |
| [more-docs-python-org-tutorial-venv](https://docs.python.org/3/tutorial/venv.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 12.87ms | 13.49ms | 7778 B |
| [more-docs-python-org-tutorial-floatingpoint](https://docs.python.org/3/tutorial/floatingpoint.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 14.36ms | 15.45ms | 13632 B |
| [more-docs-python-org-tutorial-interpreter](https://docs.python.org/3/tutorial/interpreter.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 12.33ms | 13.03ms | 6915 B |
| [more-kubernetes-io-workloads-pods](https://kubernetes.io/docs/concepts/workloads/pods/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 41.48ms | 42.38ms | 30823 B |
| [more-kubernetes-io-controllers-statefulset](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 44.07ms | 46.09ms | 32748 B |
| [more-kubernetes-io-services-networking-service](https://kubernetes.io/docs/concepts/services-networking/service/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 58.04ms | 60.46ms | 49167 B |
| [more-kubernetes-io-services-networking-ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 46.11ms | 47.93ms | 34332 B |
| [more-kubernetes-io-storage-persistent-volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 51.71ms | 55.70ms | 51294 B |
| [more-kubernetes-io-configuration-secret](https://kubernetes.io/docs/concepts/configuration/secret/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 43.69ms | 45.41ms | 38565 B |
| [more-kubernetes-io-scheduling-eviction-taint-and-toleration](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 39.58ms | 41.14ms | 20720 B |
| [more-kubernetes-io-working-with-objects-labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 36.70ms | 38.80ms | 15851 B |
| [more-postgresql-org-current-ddl-default](https://www.postgresql.org/docs/current/ddl-default.html) | database | 100.0% | 100.0% | 7/7 | 4/4 | 10.69ms | 11.22ms | 1913 B |
| [more-postgresql-org-current-ddl-alter](https://www.postgresql.org/docs/current/ddl-alter.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 12.66ms | 13.49ms | 6502 B |
| [more-postgresql-org-current-ddl-inherit](https://www.postgresql.org/docs/current/ddl-inherit.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 16.45ms | 17.23ms | 11708 B |
| [more-postgresql-org-current-ddl-partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 31.97ms | 33.69ms | 44403 B |
| [more-postgresql-org-current-indexes-intro](https://www.postgresql.org/docs/current/indexes-intro.html) | database | 100.0% | 100.0% | 7/7 | 4/4 | 11.58ms | 11.87ms | 4420 B |
| [more-postgresql-org-current-queries-with](https://www.postgresql.org/docs/current/queries-with.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 20.03ms | 20.81ms | 20470 B |
| [more-postgresql-org-current-queries-order](https://www.postgresql.org/docs/current/queries-order.html) | database | 100.0% | 100.0% | 6/6 | 4/4 | 11.82ms | 12.69ms | 3325 B |
| [more-postgresql-org-current-sql-select](https://www.postgresql.org/docs/current/sql-select.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 49.50ms | 50.51ms | 67788 B |
| [more-developer-mozilla-org-global-objects-map](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Map) | api | 100.0% | 100.0% | 8/8 | 4/4 | 24.43ms | 25.41ms | 23235 B |
| [more-developer-mozilla-org-global-objects-set](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Set) | api | 100.0% | 100.0% | 9/9 | 5/5 | 22.81ms | 27.27ms | 22452 B |
| [more-developer-mozilla-org-guide-iterators-and-generators](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Iterators_and_generators) | api | 100.0% | 100.0% | 8/8 | 4/4 | 20.62ms | 20.79ms | 11626 B |
| [more-developer-mozilla-org-grid-layout-basic-concepts](https://developer.mozilla.org/en-US/docs/Web/CSS/Guides/Grid_layout/Basic_concepts) | api | 100.0% | 100.0% | 8/8 | 4/4 | 34.24ms | 36.57ms | 24565 B |
| [more-developer-mozilla-org-elements-details](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/details) | api | 100.0% | 100.0% | 9/9 | 5/5 | 20.96ms | 21.31ms | 10302 B |
| [more-developer-mozilla-org-api-intersection-observer-api](https://developer.mozilla.org/en-US/docs/Web/API/Intersection_Observer_API) | api | 99.8% | 100.0% | 9/9 | 5/5 | 27.50ms | 30.84ms | 44946 B |
| [more-developer-mozilla-org-guides-overview](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Overview) | api | 100.0% | 100.0% | 8/8 | 4/4 | 22.66ms | 24.97ms | 17726 B |
| [more-developer-mozilla-org-accessibility-html](https://developer.mozilla.org/en-US/docs/Learn_web_development/Core/Accessibility/HTML) | api | 100.0% | 99.9% | 8/8 | 4/4 | 27.65ms | 29.75ms | 39287 B |
| [more-doc-rust-lang-org-book-ch03-01-variables-and-mutability](https://doc.rust-lang.org/book/ch03-01-variables-and-mutability.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 11.72ms | 12.16ms | 10258 B |
| [more-doc-rust-lang-org-book-ch03-02-data-types](https://doc.rust-lang.org/book/ch03-02-data-types.html) | tutorial | 100.0% | 100.0% | 8/8 | 5/5 | 15.51ms | 16.47ms | 17168 B |
| [more-doc-rust-lang-org-book-ch04-02-references-and-borrowing](https://doc.rust-lang.org/book/ch04-02-references-and-borrowing.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 12.31ms | 12.96ms | 13651 B |
| [more-doc-rust-lang-org-book-ch05-01-defining-structs](https://doc.rust-lang.org/book/ch05-01-defining-structs.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 13.84ms | 13.99ms | 14644 B |
| [more-doc-rust-lang-org-book-ch08-01-vectors](https://doc.rust-lang.org/book/ch08-01-vectors.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 11.94ms | 12.69ms | 12925 B |
| [more-doc-rust-lang-org-book-ch10-02-traits](https://doc.rust-lang.org/book/ch10-02-traits.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 17.81ms | 18.93ms | 23109 B |
| [more-doc-rust-lang-org-book-ch13-02-iterators](https://doc.rust-lang.org/book/ch13-02-iterators.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 12.88ms | 13.20ms | 12193 B |
| [more-doc-rust-lang-org-book-ch16-01-threads](https://doc.rust-lang.org/book/ch16-01-threads.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 13.36ms | 13.70ms | 14839 B |
| [more-sqlite-org-lang-insert](https://www.sqlite.org/lang_insert.html) | database | 93.6% | 100.0% | 3/4 | 3/3 | 30.09ms | 30.67ms | 5072 B |
| [more-sqlite-org-lang-update](https://www.sqlite.org/lang_update.html) | database | 93.1% | 100.0% | 6/7 | 4/4 | 52.36ms | 54.75ms | 8724 B |
| [more-sqlite-org-lang-delete](https://www.sqlite.org/lang_delete.html) | database | 90.4% | 100.0% | 3/4 | 3/3 | 36.73ms | 38.77ms | 5862 B |
| [more-sqlite-org-lang-createtable](https://www.sqlite.org/lang_createtable.html) | database | 94.6% | 100.0% | 8/8 | 5/5 | 69.26ms | 70.52ms | 20839 B |
| [more-sqlite-org-lang-with](https://www.sqlite.org/lang_with.html) | database | 97.4% | 99.9% | 6/7 | 3/4 | 41.42ms | 44.93ms | 26850 B |
| [more-sqlite-org-windowfunctions](https://www.sqlite.org/windowfunctions.html) | database | 96.8% | 100.0% | 8/8 | 5/5 | 84.80ms | 88.10ms | 35478 B |
| [more-sqlite-org-datatype3](https://www.sqlite.org/datatype3.html) | database | 97.8% | 100.0% | 7/8 | 4/5 | 20.19ms | 21.82ms | 30014 B |
| [more-sqlite-org-isolation](https://www.sqlite.org/isolation.html) | database | 100.0% | 99.7% | 6/6 | 3/3 | 10.73ms | 11.28ms | 14282 B |
| [more-git-scm-com-docs-git-merge](https://git-scm.com/docs/git-merge) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 43.67ms | 45.63ms | 37035 B |
| [more-git-scm-com-docs-git-cherry-pick](https://git-scm.com/docs/git-cherry-pick) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 19.48ms | 20.40ms | 10265 B |
| [more-git-scm-com-docs-git-bisect](https://git-scm.com/docs/git-bisect) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 23.95ms | 25.98ms | 16725 B |
| [more-git-scm-com-docs-git-stash](https://git-scm.com/docs/git-stash) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 27.16ms | 27.77ms | 17260 B |
| [more-git-scm-com-docs-git-reflog](https://git-scm.com/docs/git-reflog) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 16.12ms | 16.47ms | 5674 B |
| [more-git-scm-com-docs-git-worktree](https://git-scm.com/docs/git-worktree) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 31.17ms | 34.22ms | 23644 B |
| [more-git-scm-com-docs-git-fetch](https://git-scm.com/docs/git-fetch) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 51.85ms | 53.00ms | 47366 B |
| [more-git-scm-com-docs-git-log](https://git-scm.com/docs/git-log) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 116.17ms | 118.93ms | 116066 B |
| [more-gnu-org-html-node-ls-invocation](https://www.gnu.org/software/coreutils/manual/html_node/ls-invocation.html) | manual | 100.0% | 87.3% | 6/6 | 4/4 | 9.06ms | 9.66ms | 2859 B |
| [more-gnu-org-html-node-mv-invocation](https://www.gnu.org/software/coreutils/manual/html_node/mv-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 14.05ms | 15.84ms | 8882 B |
| [more-gnu-org-html-node-rm-invocation](https://www.gnu.org/software/coreutils/manual/html_node/rm-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 12.12ms | 13.33ms | 5695 B |
| [more-gnu-org-html-node-dd-invocation](https://www.gnu.org/software/coreutils/manual/html_node/dd-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 25.45ms | 25.77ms | 22695 B |
| [more-gnu-org-html-node-sort-invocation](https://www.gnu.org/software/coreutils/manual/html_node/sort-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 24.02ms | 24.75ms | 27407 B |
| [more-gnu-org-html-node-uniq-invocation](https://www.gnu.org/software/coreutils/manual/html_node/uniq-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 12.87ms | 13.22ms | 6818 B |
| [more-gnu-org-html-node-date-invocation](https://www.gnu.org/software/coreutils/manual/html_node/date-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 7.81ms | 9.46ms | 1603 B |
| [more-gnu-org-html-node-chmod-invocation](https://www.gnu.org/software/coreutils/manual/html_node/chmod-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 12.75ms | 13.86ms | 6756 B |
| [more-developer-hashicorp-com-values-variables](https://developer.hashicorp.com/terraform/language/values/variables) | configuration | 100.0% | 99.7% | 8/8 | 4/4 | 21.18ms | 22.50ms | 12410 B |
| [more-developer-hashicorp-com-values-outputs](https://developer.hashicorp.com/terraform/language/values/outputs) | configuration | 100.0% | 99.1% | 8/8 | 4/4 | 16.91ms | 17.69ms | 4627 B |
| [more-developer-hashicorp-com-values-locals](https://developer.hashicorp.com/terraform/language/values/locals) | configuration | 100.0% | 98.8% | 8/8 | 4/4 | 15.66ms | 16.35ms | 3338 B |
| [more-developer-hashicorp-com-expressions-types](https://developer.hashicorp.com/terraform/language/expressions/types) | configuration | 100.0% | 99.5% | 8/8 | 4/4 | 18.83ms | 19.27ms | 8030 B |
| [more-developer-hashicorp-com-expressions-conditionals](https://developer.hashicorp.com/terraform/language/expressions/conditionals) | configuration | 100.0% | 99.5% | 8/8 | 4/4 | 20.60ms | 22.05ms | 7637 B |
| [more-developer-hashicorp-com-expressions-for](https://developer.hashicorp.com/terraform/language/expressions/for) | configuration | 100.0% | 99.5% | 8/8 | 4/4 | 19.03ms | 19.67ms | 6737 B |
| [more-developer-hashicorp-com-block-module](https://developer.hashicorp.com/terraform/language/block/module) | configuration | 97.9% | 99.9% | 8/8 | 4/4 | 42.53ms | 45.93ms | 36686 B |
| [more-developer-hashicorp-com-meta-arguments-depends-on](https://developer.hashicorp.com/terraform/language/meta-arguments/depends_on) | configuration | 100.0% | 99.6% | 8/8 | 4/4 | 19.75ms | 20.54ms | 8964 B |
| [more-docs-docker-com-building-best-practices](https://docs.docker.com/build/building/best-practices/) | docs | 100.0% | 95.9% | 8/8 | 4/4 | 50.36ms | 51.35ms | 32980 B |
| [more-docs-docker-com-build-cache](https://docs.docker.com/build/cache/) | docs | 100.0% | 98.9% | 8/8 | 4/4 | 34.35ms | 38.77ms | 2075 B |
| [more-docs-docker-com-building-secrets](https://docs.docker.com/build/building/secrets/) | docs | 100.0% | 99.8% | 8/8 | 4/4 | 39.70ms | 40.81ms | 9483 B |
| [more-docs-docker-com-storage-bind-mounts](https://docs.docker.com/engine/storage/bind-mounts/) | docs | 100.0% | 98.6% | 9/9 | 5/5 | 44.05ms | 44.85ms | 17842 B |
| [more-docs-docker-com-drivers-host](https://docs.docker.com/engine/network/drivers/host/) | docs | 100.0% | 92.5% | 8/8 | 4/4 | 37.62ms | 38.51ms | 5879 B |
| [more-docs-docker-com-containers-resource-constraints](https://docs.docker.com/engine/containers/resource_constraints/) | docs | 100.0% | 98.1% | 9/9 | 5/5 | 38.60ms | 40.16ms | 13877 B |
| [more-docs-docker-com-how-tos-startup-order](https://docs.docker.com/compose/how-tos/startup-order/) | docs | 100.0% | 99.2% | 8/8 | 4/4 | 33.68ms | 35.92ms | 2867 B |
| [more-docs-docker-com-environment-variables-set-environment-variables](https://docs.docker.com/compose/how-tos/environment-variables/set-environment-variables/) | docs | 100.0% | 93.4% | 8/8 | 4/4 | 37.30ms | 38.98ms | 5459 B |
| [more-blog-cloudflare-com-how-pingora-keeps-count](https://blog.cloudflare.com/how-pingora-keeps-count/) | blog | 99.8% | 99.6% | 9/9 | 5/5 | 34.11ms | 36.80ms | 12133 B |
| [more-blog-cloudflare-com-cloudflare-workers-unleashed](https://blog.cloudflare.com/cloudflare-workers-unleashed/) | blog | 99.4% | 99.5% | 8/8 | 4/4 | 31.29ms | 34.92ms | 9452 B |
| [more-blog-cloudflare-com-introducing-cloudflare-workers](https://blog.cloudflare.com/introducing-cloudflare-workers/) | blog | 99.8% | 99.8% | 8/8 | 4/4 | 36.49ms | 37.32ms | 19160 B |
| [more-blog-cloudflare-com-introducing-cache-reserve](https://blog.cloudflare.com/introducing-cache-reserve/) | blog | 99.6% | 99.6% | 7/7 | 3/3 | 25.98ms | 28.26ms | 12009 B |
| [more-blog-cloudflare-com-the-sad-state-of-linux-socket-balancing](https://blog.cloudflare.com/the-sad-state-of-linux-socket-balancing/) | blog | 99.8% | 99.6% | 8/8 | 4/4 | 35.08ms | 37.62ms | 15454 B |
| [more-blog-cloudflare-com-keepalives-considered-harmful](https://blog.cloudflare.com/keepalives-considered-harmful/) | blog | 100.0% | 99.7% | 8/8 | 4/4 | 37.06ms | 38.76ms | 16850 B |
| [more-blog-cloudflare-com-road-to-grpc](https://blog.cloudflare.com/road-to-grpc/) | blog | 100.0% | 99.4% | 7/7 | 3/3 | 26.40ms | 28.83ms | 13516 B |
| [more-blog-cloudflare-com-the-road-to-quic](https://blog.cloudflare.com/the-road-to-quic/) | blog | 99.8% | 99.7% | 7/7 | 3/3 | 29.29ms | 31.45ms | 18690 B |
| [more-danluu-com-testing](https://danluu.com/testing/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 15.59ms | 16.32ms | 32543 B |
| [more-danluu-com-latency-mitigation](https://danluu.com/latency-mitigation/) | essay | 100.0% | 100.0% | 5/5 | 4/4 | 14.72ms | 15.56ms | 33575 B |
| [more-danluu-com-input-lag](https://danluu.com/input-lag/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 18.74ms | 19.51ms | 40532 B |
| [more-danluu-com-file-consistency](https://danluu.com/file-consistency/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 19.40ms | 20.44ms | 32942 B |
| [more-danluu-com-postmortem-lessons](https://danluu.com/postmortem-lessons/) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 10.64ms | 11.11ms | 16401 B |
| [more-danluu-com-keyboard-latency](https://danluu.com/keyboard-latency/) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 14.72ms | 14.95ms | 29956 B |
| [more-danluu-com-cpu-bugs](https://danluu.com/cpu-bugs/) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 14.00ms | 14.57ms | 28004 B |
| [more-danluu-com-simple-architectures](https://danluu.com/simple-architectures/) | essay | 100.0% | 100.0% | 5/5 | 3/3 | 10.13ms | 10.91ms | 13769 B |
| [more-joelonsoftware-com-02-painless-functional-specifications-part-1-why-bother](https://www.joelonsoftware.com/2000/10/02/painless-functional-specifications-part-1-why-bother/) | blog | 100.0% | 99.3% | 5/5 | 3/3 | 11.84ms | 13.11ms | 14860 B |
| [more-joelonsoftware-com-22-three-wrong-ideas-from-computer-science](https://www.joelonsoftware.com/2000/08/22/three-wrong-ideas-from-computer-science/) | blog | 100.0% | 98.9% | 7/7 | 3/3 | 10.80ms | 11.38ms | 8340 B |
| [more-joelonsoftware-com-11-back-to-basics](https://www.joelonsoftware.com/2001/12/11/back-to-basics/) | blog | 100.0% | 99.5% | 6/6 | 4/4 | 14.51ms | 15.57ms | 19518 B |
| [more-joelonsoftware-com-29-test-yourself](https://www.joelonsoftware.com/2005/12/29/test-yourself/) | blog | 100.0% | 97.6% | 6/6 | 4/4 | 10.56ms | 10.76ms | 3049 B |
| [more-joelonsoftware-com-12-strategy-letter-v](https://www.joelonsoftware.com/2002/06/12/strategy-letter-v/) | blog | 100.0% | 99.5% | 5/5 | 3/3 | 11.94ms | 12.91ms | 17680 B |
| [more-joelonsoftware-com-29-the-perils-of-javaschools-2](https://www.joelonsoftware.com/2005/12/29/the-perils-of-javaschools-2/) | blog | 100.0% | 99.5% | 5/5 | 3/3 | 12.27ms | 12.67ms | 15479 B |
| [more-joelonsoftware-com-19-two-stories](https://www.joelonsoftware.com/2000/03/19/two-stories/) | blog | 100.0% | 99.3% | 4/4 | 3/3 | 11.24ms | 11.96ms | 11057 B |
| [more-joelonsoftware-com-08-painless-bug-tracking](https://www.joelonsoftware.com/2000/11/08/painless-bug-tracking/) | blog | 100.0% | 99.4% | 7/7 | 3/3 | 13.18ms | 14.55ms | 14108 B |
| [more-rfc-editor-org-rfc-rfc8446](https://www.rfc-editor.org/rfc/rfc8446.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 63.24ms | 66.87ms | 340131 B |
| [more-rfc-editor-org-rfc-rfc9000](https://www.rfc-editor.org/rfc/rfc9000.html) | standard | 99.8% | 100.0% | 7/7 | 4/4 | 199.09ms | 201.96ms | 505711 B |
| [more-rfc-editor-org-rfc-rfc9111](https://www.rfc-editor.org/rfc/rfc9111.html) | standard | 100.0% | 100.0% | 7/7 | 4/4 | 67.59ms | 69.77ms | 125166 B |
| [more-rfc-editor-org-rfc-rfc9457](https://www.rfc-editor.org/rfc/rfc9457.html) | standard | 97.8% | 100.0% | 7/7 | 4/4 | 28.47ms | 29.73ms | 42887 B |
| [more-rfc-editor-org-rfc-rfc7519](https://www.rfc-editor.org/rfc/rfc7519.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 20.23ms | 21.46ms | 63487 B |
| [more-rfc-editor-org-rfc-rfc6455](https://www.rfc-editor.org/rfc/rfc6455.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 35.59ms | 36.91ms | 163129 B |
| [more-rfc-editor-org-rfc-rfc6902](https://www.rfc-editor.org/rfc/rfc6902.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 13.07ms | 14.04ms | 26662 B |
| [more-rfc-editor-org-rfc-rfc3339](https://www.rfc-editor.org/rfc/rfc3339.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 14.14ms | 14.70ms | 35318 B |
| [more-en-wikipedia-org-wiki-bicycle](https://en.wikipedia.org/wiki/Bicycle) | encyclopedia | 100.0% | 99.6% | 7/7 | 3/3 | 106.99ms | 109.34ms | 170791 B |
| [more-en-wikipedia-org-wiki-solar-system](https://en.wikipedia.org/wiki/Solar_System) | encyclopedia | 99.4% | 99.8% | 6/7 | 3/3 | 215.10ms | 222.26ms | 393727 B |
| [more-en-wikipedia-org-wiki-black-hole](https://en.wikipedia.org/wiki/Black_hole) | encyclopedia | 99.4% | 99.9% | 7/8 | 4/4 | 181.84ms | 184.20ms | 334382 B |
| [more-en-wikipedia-org-wiki-ada-lovelace](https://en.wikipedia.org/wiki/Ada_Lovelace) | encyclopedia | 99.8% | 99.9% | 6/7 | 3/3 | 94.11ms | 95.82ms | 158623 B |
| [more-en-wikipedia-org-wiki-fermentation](https://en.wikipedia.org/wiki/Fermentation) | encyclopedia | 99.6% | 99.8% | 7/7 | 3/3 | 61.39ms | 63.35ms | 92676 B |
| [more-en-wikipedia-org-wiki-silk-road](https://en.wikipedia.org/wiki/Silk_Road) | encyclopedia | 99.9% | 99.8% | 7/7 | 3/3 | 130.34ms | 131.37ms | 229471 B |
| [more-en-wikipedia-org-wiki-fibonacci-sequence](https://en.wikipedia.org/wiki/Fibonacci_sequence) | encyclopedia | 83.7% | 99.9% | 6/7 | 2/3 | 111.56ms | 116.45ms | 134747 B |
| [more-en-wikipedia-org-wiki-coral-reef](https://en.wikipedia.org/wiki/Coral_reef) | encyclopedia | 100.0% | 99.9% | 7/7 | 3/3 | 162.80ms | 165.59ms | 294908 B |
| [more-science-nasa-gov-mercury-facts](https://science.nasa.gov/mercury/facts/) | science | 98.4% | 100.0% | 6/6 | 3/3 | 19.07ms | 19.89ms | 6726 B |
| [more-science-nasa-gov-venus-venus-facts](https://science.nasa.gov/venus/venus-facts/) | science | 99.3% | 100.0% | 7/7 | 3/3 | 21.33ms | 21.90ms | 16129 B |
| [more-science-nasa-gov-saturn-facts](https://science.nasa.gov/saturn/facts/) | science | 98.8% | 100.0% | 7/7 | 3/3 | 19.56ms | 21.10ms | 9114 B |
| [more-science-nasa-gov-uranus-facts](https://science.nasa.gov/uranus/facts/) | science | 98.4% | 100.0% | 6/6 | 3/3 | 18.34ms | 20.19ms | 6941 B |
| [more-science-nasa-gov-neptune-neptune-facts](https://science.nasa.gov/neptune/neptune-facts/) | science | 98.7% | 100.0% | 8/8 | 4/4 | 20.02ms | 20.38ms | 9428 B |
| [more-science-nasa-gov-moon-facts](https://science.nasa.gov/moon/facts/) | science | 97.9% | 99.5% | 7/7 | 3/3 | 21.93ms | 23.15ms | 12392 B |
| [more-science-nasa-gov-sun-facts](https://science.nasa.gov/sun/facts/) | science | 85.9% | 99.7% | 7/7 | 3/3 | 23.97ms | 24.69ms | 20816 B |
| [more-science-nasa-gov-asteroids-facts](https://science.nasa.gov/solar-system/asteroids/facts/) | science | 98.1% | 100.0% | 7/7 | 3/3 | 24.25ms | 24.70ms | 16421 B |
| [more-nps-gov-planyourvisit-hiking](https://www.nps.gov/yose/planyourvisit/hiking.htm) | visitor-guide | 100.0% | 100.0% | 6/6 | 3/3 | 10.09ms | 11.26ms | 2078 B |
| [more-nps-gov-planyourvisit-hiking-088184](https://www.nps.gov/grsm/planyourvisit/hiking.htm) | visitor-guide | 100.0% | 100.0% | 7/7 | 3/3 | 12.41ms | 12.75ms | 6582 B |
| [more-nps-gov-planyourvisit-hiking-36d538](https://www.nps.gov/acad/planyourvisit/hiking.htm) | visitor-guide | 99.7% | 100.0% | 7/7 | 3/3 | 14.74ms | 15.42ms | 8046 B |
| [more-nps-gov-planyourvisit-safety](https://www.nps.gov/zion/planyourvisit/safety.htm) | visitor-guide | 100.0% | 100.0% | 5/5 | 3/3 | 12.72ms | 13.15ms | 9812 B |
| [more-nps-gov-planyourvisit-wilderness-safety](https://www.nps.gov/zion/planyourvisit/wilderness-safety.htm) | visitor-guide | 99.6% | 100.0% | 7/7 | 3/3 | 15.70ms | 16.14ms | 17586 B |
| [more-nps-gov-planyourvisit-safety-297315](https://www.nps.gov/grte/planyourvisit/safety.htm) | visitor-guide | 100.0% | 100.0% | 7/7 | 3/3 | 18.71ms | 19.35ms | 19471 B |
| [more-nps-gov-planyourvisit-winter-safety](https://www.nps.gov/yell/planyourvisit/winter-safety.htm) | visitor-guide | 94.3% | 100.0% | 6/7 | 2/3 | 11.48ms | 12.63ms | 4926 B |
| [more-nps-gov-planyourvisit-bearsafety](https://www.nps.gov/grte/planyourvisit/bearsafety.htm) | visitor-guide | 100.0% | 100.0% | 7/7 | 3/3 | 14.46ms | 15.10ms | 11551 B |
| [more-pandas-pydata-org-user-guide-groupby](https://pandas.pydata.org/docs/user_guide/groupby.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 51.56ms | 54.42ms | 88280 B |
| [more-pandas-pydata-org-user-guide-missing-data](https://pandas.pydata.org/docs/user_guide/missing_data.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 27.39ms | 28.36ms | 34722 B |
| [more-pandas-pydata-org-user-guide-reshaping](https://pandas.pydata.org/docs/user_guide/reshaping.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 29.59ms | 32.06ms | 47667 B |
| [more-pandas-pydata-org-user-guide-categorical](https://pandas.pydata.org/docs/user_guide/categorical.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 38.28ms | 40.75ms | 49270 B |
| [more-pandas-pydata-org-user-guide-timeseries](https://pandas.pydata.org/docs/user_guide/timeseries.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 82.20ms | 84.60ms | 152382 B |
| [more-pandas-pydata-org-user-guide-text](https://pandas.pydata.org/docs/user_guide/text.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 37.01ms | 38.51ms | 44533 B |
| [more-pandas-pydata-org-user-guide-duplicates](https://pandas.pydata.org/docs/user_guide/duplicates.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 15.86ms | 16.59ms | 14871 B |
| [more-pandas-pydata-org-user-guide-options](https://pandas.pydata.org/docs/user_guide/options.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 21.86ms | 22.51ms | 38365 B |
| [more-docs-djangoproject-com-db-models](https://docs.djangoproject.com/en/5.2/topics/db/models/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 42.91ms | 44.43ms | 75855 B |
| [more-docs-djangoproject-com-db-aggregation](https://docs.djangoproject.com/en/5.2/topics/db/aggregation/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 22.90ms | 26.09ms | 25666 B |
| [more-docs-djangoproject-com-http-views](https://docs.djangoproject.com/en/5.2/topics/http/views/) | docs | 100.0% | 99.7% | 8/8 | 4/4 | 15.28ms | 16.92ms | 10241 B |
| [more-docs-djangoproject-com-http-urls](https://docs.djangoproject.com/en/5.2/topics/http/urls/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 27.50ms | 29.73ms | 36634 B |
| [more-docs-djangoproject-com-topics-forms](https://docs.djangoproject.com/en/5.2/topics/forms/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 29.80ms | 30.65ms | 37874 B |
| [more-docs-djangoproject-com-auth-default](https://docs.djangoproject.com/en/5.2/topics/auth/default/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 56.62ms | 57.89ms | 100806 B |
| [more-docs-djangoproject-com-topics-cache](https://docs.djangoproject.com/en/5.2/topics/cache/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 40.84ms | 42.07ms | 63122 B |
| [more-docs-djangoproject-com-testing-overview](https://docs.djangoproject.com/en/5.2/topics/testing/overview/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 17.48ms | 17.52ms | 19281 B |
| [more-curl-se-docs-http-cookies](https://curl.se/docs/http-cookies.html) | manual | 99.3% | 100.0% | 7/7 | 3/3 | 10.27ms | 10.85ms | 6885 B |
| [more-curl-se-docs-sslcerts](https://curl.se/docs/sslcerts.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 10.44ms | 11.38ms | 6249 B |
| [more-curl-se-docs-alt-svc](https://curl.se/docs/alt-svc.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 8.66ms | 9.33ms | 1325 B |
| [more-curl-se-docs-hsts](https://curl.se/docs/hsts.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 9.40ms | 9.69ms | 1546 B |
| [more-curl-se-docs-http3](https://curl.se/docs/http3.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 12.36ms | 13.22ms | 11931 B |
| [more-curl-se-docs-url-syntax](https://curl.se/docs/url-syntax.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 13.98ms | 16.60ms | 15432 B |
| [more-curl-se-docs-ssl-ciphers](https://curl.se/docs/ssl-ciphers.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 11.97ms | 13.11ms | 11375 B |
| [more-curl-se-docs-ssl-compared](https://curl.se/docs/ssl-compared.html) | manual | 100.0% | 100.0% | 7/7 | 3/3 | 13.48ms | 15.44ms | 8231 B |
| [more-w3-org-images-decorative](https://www.w3.org/WAI/tutorials/images/decorative/) | accessibility | 100.0% | 95.4% | 8/8 | 4/4 | 11.90ms | 12.90ms | 5659 B |
| [more-w3-org-images-informative](https://www.w3.org/WAI/tutorials/images/informative/) | accessibility | 100.0% | 96.4% | 8/8 | 4/4 | 12.59ms | 13.79ms | 7736 B |
| [more-w3-org-images-functional](https://www.w3.org/WAI/tutorials/images/functional/) | accessibility | 99.6% | 95.5% | 8/8 | 4/4 | 11.79ms | 12.27ms | 5690 B |
| [more-w3-org-images-complex](https://www.w3.org/WAI/tutorials/images/complex/) | accessibility | 100.0% | 97.5% | 8/8 | 4/4 | 12.45ms | 13.08ms | 9399 B |
| [more-w3-org-forms-labels](https://www.w3.org/WAI/tutorials/forms/labels/) | accessibility | 99.3% | 97.8% | 8/8 | 4/4 | 14.22ms | 14.63ms | 10896 B |
| [more-w3-org-forms-instructions](https://www.w3.org/WAI/tutorials/forms/instructions/) | accessibility | 98.8% | 97.0% | 8/8 | 4/4 | 13.11ms | 17.02ms | 8688 B |
| [more-w3-org-tables-multi-level](https://www.w3.org/WAI/tutorials/tables/multi-level/) | accessibility | 100.0% | 95.8% | 8/8 | 4/4 | 14.08ms | 15.40ms | 6095 B |
| [more-w3-org-page-structure-headings](https://www.w3.org/WAI/tutorials/page-structure/headings/) | accessibility | 100.0% | 93.7% | 7/7 | 3/3 | 10.93ms | 11.58ms | 5654 B |
| [more-ourworldindata-org-co2-emissions](https://ourworldindata.org/co2-emissions) | data-article | 98.9% | 99.9% | 8/8 | 4/4 | 28.82ms | 30.14ms | 28709 B |
| [more-ourworldindata-org-energy-mix](https://ourworldindata.org/energy-mix) | data-article | 100.0% | 100.0% | 8/8 | 4/4 | 25.55ms | 26.39ms | 19258 B |
| [more-ourworldindata-org-plastic-pollution](https://ourworldindata.org/plastic-pollution) | data-article | 98.9% | 100.0% | 6/8 | 4/4 | 24.25ms | 24.74ms | 22689 B |
| [more-ourworldindata-org-literacy](https://ourworldindata.org/literacy) | data-article | 99.0% | 100.0% | 8/8 | 4/4 | 29.85ms | 31.54ms | 27461 B |
| [more-ourworldindata-org-economic-growth](https://ourworldindata.org/economic-growth) | data-article | 98.6% | 100.0% | 7/8 | 4/4 | 23.77ms | 25.10ms | 27011 B |
| [more-ourworldindata-org-child-mortality](https://ourworldindata.org/child-mortality) | data-article | 99.4% | 100.0% | 7/8 | 4/4 | 29.51ms | 30.71ms | 36185 B |
| [more-ourworldindata-org-vaccination](https://ourworldindata.org/vaccination) | data-article | 99.1% | 100.0% | 7/8 | 4/4 | 37.62ms | 39.73ms | 52970 B |
| [more-ourworldindata-org-renewable-energy](https://ourworldindata.org/renewable-energy) | data-article | 100.0% | 99.9% | 8/8 | 4/4 | 20.16ms | 20.98ms | 11992 B |
| [more-ssd-eff-org-module-how-to-use-signal](https://ssd.eff.org/module/how-to-use-signal) | guide | 98.0% | 100.0% | 7/7 | 3/3 | 26.81ms | 31.73ms | 36784 B |
| [more-ssd-eff-org-module-how-to-use-tor](https://ssd.eff.org/module/how-to-use-tor) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 15.04ms | 15.62ms | 13857 B |
| [more-ssd-eff-org-module-how-encrypt-your-windows-device](https://ssd.eff.org/module/how-encrypt-your-windows-device) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 14.21ms | 15.07ms | 13313 B |
| [more-ssd-eff-org-module-how-enable-two-factor-authentication](https://ssd.eff.org/module/how-enable-two-factor-authentication) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 13.73ms | 14.14ms | 14594 B |
| [more-ssd-eff-org-module-what-should-i-know-about-encryption](https://ssd.eff.org/module/what-should-i-know-about-encryption) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 16.11ms | 16.68ms | 17913 B |
| [more-ssd-eff-org-module-choosing-vpn-thats-right-you](https://ssd.eff.org/module/choosing-vpn-thats-right-you) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 16.10ms | 17.29ms | 18122 B |
| [more-ssd-eff-org-module-keeping-your-data-safe](https://ssd.eff.org/module/keeping-your-data-safe) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 12.69ms | 13.78ms | 9834 B |
| [more-ssd-eff-org-module-protecting-yourself-social-networks](https://ssd.eff.org/module/protecting-yourself-social-networks) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 12.47ms | 13.14ms | 10121 B |
| [more-paulgraham-com-hs](https://www.paulgraham.com/hs.html) | essay | 99.8% | 100.0% | 3/4 | 3/3 | 17.60ms | 17.98ms | 28028 B |
| [more-paulgraham-com-greatwork](https://www.paulgraham.com/greatwork.html) | essay | 100.0% | 100.0% | 3/3 | 3/3 | 32.77ms | 33.94ms | 69852 B |
| [more-paulgraham-com-startupideas](https://www.paulgraham.com/startupideas.html) | essay | 99.9% | 100.0% | 3/4 | 3/3 | 22.26ms | 22.85ms | 42432 B |
| [more-paulgraham-com-procrastination](https://www.paulgraham.com/procrastination.html) | essay | 99.2% | 100.0% | 3/4 | 3/3 | 11.73ms | 12.45ms | 10244 B |
| [more-paulgraham-com-nerds](https://www.paulgraham.com/nerds.html) | essay | 99.6% | 100.0% | 3/4 | 3/3 | 18.52ms | 19.01ms | 32014 B |
| [more-paulgraham-com-wealth](https://www.paulgraham.com/wealth.html) | essay | 99.8% | 100.0% | 3/4 | 3/3 | 24.18ms | 25.18ms | 51543 B |
| [more-paulgraham-com-cities](https://www.paulgraham.com/cities.html) | essay | 99.8% | 100.0% | 3/4 | 3/3 | 14.57ms | 15.32ms | 20764 B |
| [more-paulgraham-com-good](https://www.paulgraham.com/good.html) | essay | 99.9% | 100.0% | 4/4 | 3/3 | 13.57ms | 14.29ms | 17092 B |
| [more-martinfowler-com-articles-feature-toggles](https://martinfowler.com/articles/feature-toggles.html) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 22.72ms | 23.36ms | 50929 B |
| [more-martinfowler-com-articles-injection](https://martinfowler.com/articles/injection.html) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 20.80ms | 22.15ms | 42508 B |
| [more-martinfowler-com-bliki-stranglerfigapplication](https://martinfowler.com/bliki/StranglerFigApplication.html) | essay | 100.0% | 91.0% | 6/6 | 3/3 | 9.92ms | 10.61ms | 7438 B |
| [more-martinfowler-com-bliki-monolithfirst](https://martinfowler.com/bliki/MonolithFirst.html) | essay | 100.0% | 90.3% | 7/7 | 3/3 | 10.36ms | 11.42ms | 7940 B |
| [more-martinfowler-com-bliki-twohardthings](https://martinfowler.com/bliki/TwoHardThings.html) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 9.08ms | 9.41ms | 2048 B |
| [more-martinfowler-com-bliki-cqrs](https://martinfowler.com/bliki/CQRS.html) | essay | 99.3% | 100.0% | 7/7 | 3/3 | 10.53ms | 12.61ms | 8639 B |
| [more-martinfowler-com-articles-practical-test-pyramid](https://martinfowler.com/articles/practical-test-pyramid.html) | essay | 96.7% | 100.0% | 8/8 | 4/4 | 31.80ms | 33.49ms | 82253 B |
| [more-martinfowler-com-bliki-boundedcontext](https://martinfowler.com/bliki/BoundedContext.html) | essay | 99.1% | 99.4% | 7/7 | 3/3 | 9.45ms | 10.95ms | 5663 B |
| [more-letsencrypt-org-docs-rate-limits](https://letsencrypt.org/docs/rate-limits/) | docs | 100.0% | 100.0% | 9/9 | 5/5 | 14.44ms | 15.15ms | 15102 B |
| [more-letsencrypt-org-docs-staging-environment](https://letsencrypt.org/docs/staging-environment/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 11.62ms | 12.30ms | 9308 B |
| [more-letsencrypt-org-docs-faq](https://letsencrypt.org/docs/faq/) | docs | 100.0% | 100.0% | 7/7 | 3/3 | 11.04ms | 11.37ms | 8418 B |
| [more-letsencrypt-org-docs-integration-guide](https://letsencrypt.org/docs/integration-guide/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 11.45ms | 12.39ms | 13185 B |
| [more-letsencrypt-org-docs-account-id](https://letsencrypt.org/docs/account-id/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 8.59ms | 8.68ms | 1377 B |
| [more-letsencrypt-org-docs-ipv6-support](https://letsencrypt.org/docs/ipv6-support/) | docs | 100.0% | 100.0% | 7/7 | 3/3 | 9.35ms | 9.96ms | 2820 B |
| [more-letsencrypt-org-docs-revoking](https://letsencrypt.org/docs/revoking/) | docs | 100.0% | 100.0% | 6/6 | 4/4 | 10.82ms | 12.81ms | 6944 B |
| [more-letsencrypt-org-docs-ct-logs](https://letsencrypt.org/docs/ct-logs/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 10.14ms | 10.69ms | 5983 B |
| [more-oceanservice-noaa-gov-facts-tides](https://oceanservice.noaa.gov/facts/tides.html) | science | 96.9% | 100.0% | 5/6 | 3/3 | 9.90ms | 10.49ms | 1238 B |
| [more-oceanservice-noaa-gov-facts-current](https://oceanservice.noaa.gov/facts/current.html) | science | 94.2% | 100.0% | 5/6 | 2/3 | 10.01ms | 11.07ms | 1957 B |
| [more-oceanservice-noaa-gov-facts-coral](https://oceanservice.noaa.gov/facts/coral.html) | science | 100.0% | 100.0% | 4/4 | 3/3 | 10.40ms | 10.74ms | 2947 B |
| [more-oceanservice-noaa-gov-facts-acidification](https://oceanservice.noaa.gov/facts/acidification.html) | science | 97.5% | 100.0% | 4/5 | 3/3 | 10.13ms | 11.09ms | 1702 B |
| [more-oceanservice-noaa-gov-facts-tsunami](https://oceanservice.noaa.gov/facts/tsunami.html) | science | 100.0% | 100.0% | 4/4 | 2/2 | 10.03ms | 10.61ms | 1155 B |
| [more-oceanservice-noaa-gov-facts-estuary](https://oceanservice.noaa.gov/facts/estuary.html) | science | 96.3% | 91.0% | 5/7 | 2/3 | 11.39ms | 12.10ms | 3386 B |
| [more-oceanservice-noaa-gov-facts-mangroves](https://oceanservice.noaa.gov/facts/mangroves.html) | science | 100.0% | 100.0% | 5/5 | 3/3 | 10.02ms | 11.69ms | 1283 B |
| [more-oceanservice-noaa-gov-facts-kelp](https://oceanservice.noaa.gov/facts/kelp.html) | science | 97.5% | 100.0% | 5/6 | 3/3 | 10.33ms | 11.33ms | 2069 B |
| [more-medlineplus-gov-article-000076](https://medlineplus.gov/ency/article/000076.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 13.53ms | 14.95ms | 10244 B |
| [more-medlineplus-gov-article-000468](https://medlineplus.gov/ency/article/000468.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 17.09ms | 18.10ms | 17003 B |
| [more-medlineplus-gov-article-000279](https://medlineplus.gov/ency/article/000279.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 13.18ms | 15.74ms | 10737 B |
| [more-medlineplus-gov-article-000313](https://medlineplus.gov/ency/article/000313.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 19.28ms | 19.96ms | 26112 B |
| [more-medlineplus-gov-article-000545](https://medlineplus.gov/ency/article/000545.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 11.06ms | 11.79ms | 3584 B |
| [more-medlineplus-gov-article-000639](https://medlineplus.gov/ency/article/000639.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 12.17ms | 13.07ms | 6727 B |
| [more-medlineplus-gov-article-000158](https://medlineplus.gov/ency/article/000158.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 15.48ms | 15.70ms | 14910 B |
| [more-medlineplus-gov-article-000285](https://medlineplus.gov/ency/article/000285.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 12.63ms | 13.47ms | 6548 B |
| [more-who-int-detail-malaria](https://www.who.int/news-room/fact-sheets/detail/malaria) | health | 100.0% | 99.6% | 7/7 | 3/3 | 16.21ms | 16.81ms | 14208 B |
| [more-who-int-detail-tuberculosis](https://www.who.int/news-room/fact-sheets/detail/tuberculosis) | health | 100.0% | 97.8% | 6/6 | 3/3 | 13.81ms | 14.65ms | 11187 B |
| [more-who-int-detail-diabetes](https://www.who.int/news-room/fact-sheets/detail/diabetes) | health | 100.0% | 99.3% | 7/7 | 3/3 | 14.93ms | 15.19ms | 8420 B |
| [more-who-int-detail-asthma](https://www.who.int/news-room/fact-sheets/detail/asthma) | health | 100.0% | 99.2% | 7/7 | 3/3 | 14.41ms | 15.36ms | 7892 B |
| [more-who-int-detail-hypertension](https://www.who.int/news-room/fact-sheets/detail/hypertension) | health | 100.0% | 99.4% | 7/7 | 3/3 | 15.06ms | 15.52ms | 9559 B |
| [more-who-int-detail-dengue-and-severe-dengue](https://www.who.int/news-room/fact-sheets/detail/dengue-and-severe-dengue) | health | 100.0% | 98.6% | 7/7 | 3/3 | 16.16ms | 16.55ms | 12302 B |
| [more-who-int-detail-measles](https://www.who.int/news-room/fact-sheets/detail/measles) | health | 100.0% | 99.4% | 7/7 | 3/3 | 13.91ms | 14.90ms | 10104 B |
| [more-who-int-detail-physical-activity](https://www.who.int/news-room/fact-sheets/detail/physical-activity) | health | 100.0% | 99.7% | 7/7 | 3/3 | 15.18ms | 15.53ms | 10787 B |
| [more-un-org-science-causes-effects-climate-change](https://www.un.org/en/climatechange/science/causes-effects-climate-change) | explainer | 95.9% | 100.0% | 4/7 | 2/3 | 24.47ms | 25.96ms | 8627 B |
| [more-un-org-climatechange-what-is-renewable-energy](https://www.un.org/en/climatechange/what-is-renewable-energy) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 14.53ms | 15.11ms | 8761 B |
| [more-un-org-raising-ambition-renewable-energy](https://www.un.org/en/climatechange/raising-ambition/renewable-energy) | explainer | 86.1% | 100.0% | 5/7 | 2/3 | 25.57ms | 26.20ms | 12657 B |
| [more-un-org-climate-issues-greenwashing](https://www.un.org/en/climatechange/science/climate-issues/greenwashing) | explainer | 79.7% | 100.0% | 5/7 | 2/3 | 25.14ms | 25.49ms | 10387 B |
| [more-un-org-climate-issues-food](https://www.un.org/en/climatechange/science/climate-issues/food) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 26.28ms | 28.13ms | 12298 B |
| [more-un-org-climatechange-climate-adaptation](https://www.un.org/en/climatechange/climate-adaptation) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 25.16ms | 28.14ms | 11119 B |
| [more-un-org-climatechange-net-zero-coalition](https://www.un.org/en/climatechange/net-zero-coalition) | explainer | 85.0% | 100.0% | 5/7 | 2/3 | 28.83ms | 31.98ms | 7453 B |
| [more-un-org-climatechange-paris-agreement](https://www.un.org/en/climatechange/paris-agreement) | explainer | 100.0% | 98.1% | 7/7 | 3/3 | 23.79ms | 24.38ms | 6825 B |
| [more-computerhistory-org-timeline-1940](https://www.computerhistory.org/timeline/1940/) | history | 98.8% | 83.3% | 4/4 | 1/1 | 15.89ms | 16.69ms | 1047 B |
| [more-computerhistory-org-timeline-1946](https://www.computerhistory.org/timeline/1946/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 12.90ms | 13.42ms | 4474 B |
| [more-computerhistory-org-timeline-1951](https://www.computerhistory.org/timeline/1951/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 14.95ms | 15.77ms | 7652 B |
| [more-computerhistory-org-timeline-1956](https://www.computerhistory.org/timeline/1956/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 14.31ms | 14.58ms | 5641 B |
| [more-computerhistory-org-timeline-1964](https://www.computerhistory.org/timeline/1964/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 18.24ms | 19.62ms | 12589 B |
| [more-computerhistory-org-timeline-1971](https://www.computerhistory.org/timeline/1971/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 16.49ms | 17.25ms | 10578 B |
| [more-computerhistory-org-timeline-1984](https://www.computerhistory.org/timeline/1984/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 17.40ms | 18.41ms | 12694 B |
| [more-computerhistory-org-timeline-1991](https://www.computerhistory.org/timeline/1991/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 14.89ms | 15.96ms | 7430 B |
| [more-nhm-ac-uk-discover-what-is-biodiversity](https://www.nhm.ac.uk/discover/what-is-biodiversity.html) | science | 99.8% | 94.7% | 7/7 | 3/3 | 19.07ms | 19.23ms | 12113 B |
| [more-nhm-ac-uk-discover-insect-pollination](https://www.nhm.ac.uk/discover/insect-pollination.html) | science | 99.9% | 96.4% | 7/7 | 3/3 | 20.66ms | 21.90ms | 15572 B |
| [more-nhm-ac-uk-discover-how-are-fossils-formed](https://www.nhm.ac.uk/discover/how-are-fossils-formed.html) | science | 99.6% | 91.1% | 7/7 | 3/3 | 18.68ms | 19.29ms | 9012 B |
| [more-nhm-ac-uk-discover-meet-the-monsters-of-the-jurassic-seas](https://www.nhm.ac.uk/discover/meet-the-monsters-of-the-jurassic-seas.html) | science | 99.9% | 97.6% | 7/7 | 3/3 | 18.52ms | 20.15ms | 8227 B |
| [more-nhm-ac-uk-discover-what-is-natural-selection](https://www.nhm.ac.uk/discover/what-is-natural-selection.html) | science | 98.1% | 90.3% | 6/7 | 3/3 | 22.45ms | 23.07ms | 15000 B |
| [more-nhm-ac-uk-discover-convergent-evolution](https://www.nhm.ac.uk/discover/convergent-evolution.html) | science | 98.5% | 99.5% | 7/7 | 3/3 | 27.03ms | 27.61ms | 24103 B |
| [more-nhm-ac-uk-discover-dinosaur-extinction](https://www.nhm.ac.uk/discover/dinosaur-extinction.html) | science | 96.7% | 89.5% | 6/7 | 3/3 | 15.66ms | 16.65ms | 2900 B |
| [more-nhm-ac-uk-discover-what-is-climate-change-why-does-it-matter](https://www.nhm.ac.uk/discover/what-is-climate-change-why-does-it-matter.html) | science | 97.9% | 97.8% | 7/7 | 3/3 | 25.87ms | 27.22ms | 23880 B |
| [more-docs-julialang-org-manual-variables](https://docs.julialang.org/en/v1/manual/variables/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 13.03ms | 13.50ms | 10201 B |
| [more-docs-julialang-org-manual-integers-and-floating-point-numbers](https://docs.julialang.org/en/v1/manual/integers-and-floating-point-numbers/) | manual | 100.0% | 100.0% | 9/9 | 5/5 | 20.64ms | 21.21ms | 29103 B |
| [more-docs-julialang-org-manual-strings](https://docs.julialang.org/en/v1/manual/strings/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 26.28ms | 27.52ms | 48647 B |
| [more-docs-julialang-org-manual-functions](https://docs.julialang.org/en/v1/manual/functions/) | manual | 100.0% | 100.0% | 9/9 | 5/5 | 25.46ms | 27.15ms | 43241 B |
| [more-docs-julialang-org-manual-control-flow](https://docs.julialang.org/en/v1/manual/control-flow/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 21.90ms | 23.97ms | 32499 B |
| [more-docs-julialang-org-manual-types](https://docs.julialang.org/en/v1/manual/types/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 37.06ms | 38.66ms | 74679 B |
| [more-docs-julialang-org-manual-methods](https://docs.julialang.org/en/v1/manual/methods/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 25.55ms | 27.91ms | 47728 B |
| [more-docs-julialang-org-manual-performance-tips](https://docs.julialang.org/en/v1/manual/performance-tips/) | manual | 98.1% | 100.0% | 8/8 | 4/4 | 38.86ms | 40.22ms | 80300 B |
| [more-elixir-hexdocs-pm-enum](https://elixir.hexdocs.pm/Enum.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 89.04ms | 90.63ms | 91153 B |
| [more-elixir-hexdocs-pm-map](https://elixir.hexdocs.pm/Map.html) | api | 100.0% | 100.0% | 7/7 | 4/4 | 38.14ms | 40.31ms | 31897 B |
| [more-elixir-hexdocs-pm-string](https://elixir.hexdocs.pm/String.html) | api | 100.0% | 100.0% | 7/7 | 4/4 | 59.46ms | 61.19ms | 68062 B |
| [more-elixir-hexdocs-pm-list](https://elixir.hexdocs.pm/List.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 39.55ms | 40.55ms | 32209 B |
| [more-elixir-hexdocs-pm-genserver](https://elixir.hexdocs.pm/GenServer.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 39.44ms | 40.38ms | 52327 B |
| [more-elixir-hexdocs-pm-task](https://elixir.hexdocs.pm/Task.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 34.87ms | 36.54ms | 43517 B |
| [more-elixir-hexdocs-pm-introduction](https://elixir.hexdocs.pm/introduction.html) | api | 100.0% | 99.5% | 7/7 | 4/4 | 9.29ms | 9.51ms | 2694 B |
| [more-elixir-hexdocs-pm-pattern-matching](https://elixir.hexdocs.pm/pattern-matching.html) | api | 100.0% | 99.8% | 7/7 | 4/4 | 11.32ms | 12.12ms | 5853 B |
| [more-ffmpeg-org-ffmpeg](https://ffmpeg.org/ffmpeg.html) | manual | 99.1% | 100.0% | 6/8 | 4/4 | 100.31ms | 101.11ms | 148142 B |
| [more-ffmpeg-org-ffmpeg-formats](https://ffmpeg.org/ffmpeg-formats.html) | manual | 97.2% | 100.0% | 6/8 | 4/4 | 145.29ms | 148.25ms | 196445 B |
| [more-ffmpeg-org-ffmpeg-codecs](https://ffmpeg.org/ffmpeg-codecs.html) | manual | 96.7% | 100.0% | 6/8 | 4/4 | 159.57ms | 163.68ms | 177082 B |
| [more-ffmpeg-org-ffmpeg-protocols](https://ffmpeg.org/ffmpeg-protocols.html) | manual | 98.1% | 100.0% | 6/8 | 4/4 | 63.92ms | 65.58ms | 78009 B |
| [more-ffmpeg-org-ffmpeg-utils](https://ffmpeg.org/ffmpeg-utils.html) | manual | 98.2% | 100.0% | 6/8 | 4/4 | 37.20ms | 37.66ms | 24138 B |
| [more-ffmpeg-org-ffplay](https://ffmpeg.org/ffplay.html) | manual | 98.7% | 100.0% | 6/8 | 4/4 | 26.58ms | 29.12ms | 23634 B |
| [more-ffmpeg-org-ffprobe](https://ffmpeg.org/ffprobe.html) | manual | 98.8% | 100.0% | 6/8 | 4/4 | 34.05ms | 36.81ms | 34178 B |
| [more-ffmpeg-org-faq](https://ffmpeg.org/faq.html) | manual | 86.4% | 100.0% | 6/8 | 4/4 | 26.23ms | 27.73ms | 24930 B |
| [more-angular-dev-guide-components](https://angular.dev/guide/components) | docs | 99.3% | 100.0% | 8/8 | 4/4 | 18.23ms | 18.95ms | 4936 B |
| [more-angular-dev-components-inputs](https://angular.dev/guide/components/inputs) | docs | 99.8% | 100.0% | 7/8 | 4/4 | 21.67ms | 22.21ms | 14277 B |
| [more-angular-dev-guide-templates](https://angular.dev/guide/templates) | docs | 99.1% | 100.0% | 8/8 | 4/4 | 13.88ms | 15.14ms | 4212 B |
| [more-angular-dev-guide-di](https://angular.dev/guide/di) | docs | 99.4% | 100.0% | 8/8 | 4/4 | 18.24ms | 18.72ms | 7016 B |
| [more-angular-dev-guide-routing](https://angular.dev/guide/routing) | docs | 98.6% | 100.0% | 7/7 | 3/3 | 12.27ms | 13.23ms | 1853 B |
| [more-angular-dev-guide-forms](https://angular.dev/guide/forms) | docs | 99.8% | 100.0% | 8/9 | 5/5 | 29.75ms | 31.27ms | 17987 B |
| [more-angular-dev-forms-typed-forms](https://angular.dev/guide/forms/typed-forms) | docs | 99.6% | 100.0% | 7/8 | 4/4 | 19.40ms | 20.25ms | 8880 B |
| [more-angular-dev-guide-http](https://angular.dev/guide/http) | docs | 90.8% | 100.0% | 5/6 | 2/2 | 10.86ms | 12.67ms | 935 B |
| [more-react-dev-learn-describing-the-ui](https://react.dev/learn/describing-the-ui) | tutorial | 99.3% | 98.6% | 7/8 | 4/4 | 26.41ms | 28.01ms | 14229 B |
| [more-react-dev-learn-writing-markup-with-jsx](https://react.dev/learn/writing-markup-with-jsx) | tutorial | 99.7% | 99.5% | 8/8 | 4/4 | 21.36ms | 24.82ms | 10568 B |
| [more-react-dev-learn-conditional-rendering](https://react.dev/learn/conditional-rendering) | tutorial | 99.5% | 99.0% | 7/8 | 4/4 | 27.94ms | 31.51ms | 13449 B |
| [more-react-dev-learn-rendering-lists](https://react.dev/learn/rendering-lists) | tutorial | 99.8% | 99.5% | 8/8 | 4/4 | 25.87ms | 28.42ms | 12288 B |
| [more-react-dev-learn-responding-to-events](https://react.dev/learn/responding-to-events) | tutorial | 99.5% | 99.0% | 8/9 | 5/5 | 30.77ms | 33.88ms | 17321 B |
| [more-react-dev-learn-updating-objects-in-state](https://react.dev/learn/updating-objects-in-state) | tutorial | 99.7% | 99.4% | 8/8 | 4/4 | 33.89ms | 35.08ms | 21324 B |
| [more-react-dev-learn-synchronizing-with-effects](https://react.dev/learn/synchronizing-with-effects) | tutorial | 99.9% | 99.7% | 8/8 | 4/4 | 49.14ms | 51.14ms | 44113 B |
| [more-react-dev-learn-you-might-not-need-an-effect](https://react.dev/learn/you-might-not-need-an-effect) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 41.06ms | 42.27ms | 40449 B |
| [more-nmap-org-book-man-examples](https://nmap.org/book/man-examples.html) | manual | 98.0% | 100.0% | 5/6 | 3/3 | 7.45ms | 8.04ms | 2452 B |
| [more-nmap-org-book-man-port-scanning-basics](https://nmap.org/book/man-port-scanning-basics.html) | manual | 98.1% | 100.0% | 5/6 | 3/3 | 9.49ms | 10.57ms | 4059 B |
| [more-nmap-org-book-man-host-discovery](https://nmap.org/book/man-host-discovery.html) | manual | 99.7% | 100.0% | 6/6 | 3/3 | 20.33ms | 22.08ms | 23315 B |
| [more-nmap-org-book-man-port-scanning-techniques](https://nmap.org/book/man-port-scanning-techniques.html) | manual | 99.7% | 100.0% | 6/6 | 3/3 | 20.67ms | 21.90ms | 24780 B |
| [more-nmap-org-book-man-version-detection](https://nmap.org/book/man-version-detection.html) | manual | 98.8% | 100.0% | 5/6 | 3/3 | 10.22ms | 11.62ms | 6640 B |
| [more-nmap-org-book-man-os-detection](https://nmap.org/book/man-os-detection.html) | manual | 98.7% | 100.0% | 6/6 | 3/3 | 9.82ms | 10.51ms | 4945 B |
| [more-nmap-org-book-man-nse](https://nmap.org/book/man-nse.html) | manual | 99.1% | 100.0% | 6/6 | 3/3 | 12.74ms | 13.86ms | 9648 B |
| [more-nmap-org-book-man-performance](https://nmap.org/book/man-performance.html) | manual | 99.7% | 100.0% | 6/6 | 3/3 | 19.53ms | 20.04ms | 22363 B |
| [more-numpy-org-user-basics-broadcasting](https://numpy.org/doc/stable/user/basics.broadcasting.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 15.79ms | 16.80ms | 12248 B |
| [more-numpy-org-user-basics-indexing](https://numpy.org/doc/stable/user/basics.indexing.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 26.78ms | 27.70ms | 34892 B |
| [more-numpy-org-user-basics-copies](https://numpy.org/doc/stable/user/basics.copies.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 12.07ms | 12.52ms | 5599 B |
| [more-numpy-org-user-basics-types](https://numpy.org/doc/stable/user/basics.types.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 19.93ms | 21.07ms | 21198 B |
| [more-docs-scipy-org-tutorial-integrate](https://docs.scipy.org/doc/scipy/tutorial/integrate.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 25.60ms | 26.17ms | 34836 B |
| [more-docs-scipy-org-tutorial-optimize](https://docs.scipy.org/doc/scipy/tutorial/optimize.html) | tutorial | 100.0% | 100.0% | 9/9 | 5/5 | 56.84ms | 59.40ms | 94642 B |
| [more-docs-scipy-org-tutorial-interpolate](https://docs.scipy.org/doc/scipy/tutorial/interpolate.html) | tutorial | 100.0% | 100.0% | 6/6 | 3/3 | 14.99ms | 15.52ms | 14347 B |
| [more-docs-scipy-org-tutorial-fft](https://docs.scipy.org/doc/scipy/tutorial/fft.html) | tutorial | 99.1% | 100.0% | 8/8 | 4/4 | 32.51ms | 33.64ms | 45667 B |
| [more-docs-github-com-workflows-and-actions-workflow-syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax) | configuration | 99.6% | 100.0% | 7/9 | 5/5 | 108.63ms | 114.43ms | 173409 B |
| [more-docs-github-com-security-secure-use](https://docs.github.com/en/actions/reference/security/secure-use) | configuration | 100.0% | 100.0% | 7/8 | 4/4 | 33.81ms | 34.27ms | 38775 B |
| [more-docs-github-com-choose-what-workflows-do-run-job-variations](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/run-job-variations) | configuration | 90.8% | 100.0% | 7/8 | 4/4 | 23.68ms | 26.71ms | 9237 B |
| [more-docs-github-com-reuse-automations-reuse-workflows](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows) | configuration | 89.9% | 100.0% | 7/8 | 4/4 | 27.28ms | 29.05ms | 18134 B |
| [more-learn-microsoft-com-operators-null-coalescing-operator](https://learn.microsoft.com/en-us/dotnet/csharp/language-reference/operators/null-coalescing-operator) | docs | 100.0% | 98.9% | 8/8 | 4/4 | 13.36ms | 14.69ms | 6144 B |
| [more-learn-microsoft-com-operators-patterns](https://learn.microsoft.com/en-us/dotnet/csharp/language-reference/operators/patterns) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 23.76ms | 24.16ms | 43237 B |
| [more-learn-microsoft-com-asynchronous-programming-async-return-types](https://learn.microsoft.com/en-us/dotnet/csharp/asynchronous-programming/async-return-types) | docs | 100.0% | 99.6% | 8/8 | 4/4 | 15.94ms | 16.74ms | 17927 B |
| [more-learn-microsoft-com-exceptions-exception-handling](https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/exceptions/exception-handling) | docs | 100.0% | 99.3% | 8/8 | 4/4 | 12.72ms | 13.27ms | 7976 B |
| [more-developer-chrome-com-devtools-network](https://developer.chrome.com/docs/devtools/network/) | docs | 98.5% | 100.0% | 7/7 | 3/3 | 22.31ms | 23.82ms | 14358 B |
| [more-developer-chrome-com-devtools-performance](https://developer.chrome.com/docs/devtools/performance/) | docs | 99.0% | 100.0% | 7/7 | 3/3 | 19.81ms | 21.87ms | 14150 B |
| [more-developer-chrome-com-devtools-console](https://developer.chrome.com/docs/devtools/console/) | docs | 97.9% | 100.0% | 8/8 | 4/4 | 19.25ms | 20.43ms | 4935 B |
| [more-developer-chrome-com-devtools-memory-problems](https://developer.chrome.com/docs/devtools/memory-problems/) | docs | 99.1% | 100.0% | 8/8 | 4/4 | 23.31ms | 23.51ms | 13092 B |
| [more-jvns-ca-01-a-dns-resolver-in-80-lines-of-go](https://jvns.ca/blog/2022/02/01/a-dns-resolver-in-80-lines-of-go/) | blog | 99.7% | 100.0% | 8/8 | 4/4 | 13.44ms | 14.00ms | 17402 B |
| [more-jvns-ca-05-some-blogging-myths](https://jvns.ca/blog/2023/06/05/some-blogging-myths/) | blog | 97.3% | 100.0% | 7/7 | 3/3 | 10.79ms | 11.39ms | 11146 B |
| [more-jvns-ca-01-learning-skills-you-can-practice](https://jvns.ca/blog/2018/09/01/learning-skills-you-can-practice/) | blog | 99.4% | 100.0% | 7/7 | 3/3 | 10.38ms | 11.21ms | 9295 B |
| [more-jvns-ca-10-how-does-gdb-work](https://jvns.ca/blog/2016/08/10/how-does-gdb-work/) | blog | 99.6% | 100.0% | 8/8 | 4/4 | 10.64ms | 10.84ms | 9969 B |
| [more-eli-thegreenplace-net-2020-implementing-raft-part-1-elections](https://eli.thegreenplace.net/2020/implementing-raft-part-1-elections/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 18.33ms | 20.74ms | 28936 B |
| [more-eli-thegreenplace-net-2020-implementing-raft-part-2-commands-and-log-replication](https://eli.thegreenplace.net/2020/implementing-raft-part-2-commands-and-log-replication/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 19.91ms | 22.14ms | 27620 B |
| [more-eli-thegreenplace-net-2020-implementing-raft-part-3-persistence-and-optimizations](https://eli.thegreenplace.net/2020/implementing-raft-part-3-persistence-and-optimizations/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 16.03ms | 20.38ms | 19719 B |
| [more-eli-thegreenplace-net-2023-preview-ranging-over-functions-in-go](https://eli.thegreenplace.net/2023/preview-ranging-over-functions-in-go/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 17.47ms | 18.68ms | 18809 B |
| [more-brendangregg-com-usemethod](https://www.brendangregg.com/usemethod.html) | guide | 100.0% | 100.0% | 7/7 | 4/4 | 19.70ms | 20.41ms | 26572 B |
| [more-brendangregg-com-methodology](https://www.brendangregg.com/methodology.html) | guide | 100.0% | 100.0% | 6/6 | 3/3 | 15.39ms | 15.55ms | 12285 B |
| [more-brendangregg-com-flamegraphs](https://www.brendangregg.com/flamegraphs.html) | guide | 100.0% | 100.0% | 6/6 | 3/3 | 26.57ms | 27.83ms | 45080 B |
| [more-brendangregg-com-offcpuanalysis](https://www.brendangregg.com/offcpuanalysis.html) | guide | 100.0% | 100.0% | 7/7 | 4/4 | 23.57ms | 24.44ms | 41630 B |
| [more-developer-android-com-activities-activity-lifecycle](https://developer.android.com/guide/components/activities/activity-lifecycle) | docs | 99.7% | 100.0% | 8/8 | 4/4 | 33.39ms | 35.36ms | 36153 B |
| [more-developer-android-com-background-work-services](https://developer.android.com/develop/background-work/services) | docs | 99.8% | 100.0% | 7/8 | 4/4 | 34.39ms | 35.87ms | 34848 B |
| [more-developer-android-com-background-tasks-broadcasts](https://developer.android.com/develop/background-work/background-tasks/broadcasts) | docs | 99.7% | 100.0% | 7/8 | 4/4 | 34.41ms | 36.53ms | 36302 B |
| [more-developer-android-com-manifest-manifest-intro](https://developer.android.com/guide/topics/manifest/manifest-intro) | docs | 99.5% | 100.0% | 8/9 | 5/5 | 30.42ms | 32.00ms | 18379 B |
| [more-redis-io-data-types-strings](https://redis.io/docs/latest/develop/data-types/strings/) | database | 99.0% | 97.5% | 8/8 | 4/4 | 170.81ms | 175.82ms | 104846 B |
| [more-redis-io-data-types-hashes](https://redis.io/docs/latest/develop/data-types/hashes/) | database | 99.4% | 97.7% | 9/9 | 5/5 | 423.09ms | 427.58ms | 308717 B |
| [more-redis-io-data-types-lists](https://redis.io/docs/latest/develop/data-types/lists/) | database | 99.2% | 97.2% | 8/8 | 4/4 | 827.99ms | 858.16ms | 538204 B |
| [more-redis-io-data-types-sets](https://redis.io/docs/latest/develop/data-types/sets/) | database | 99.0% | 96.8% | 8/8 | 4/4 | 391.55ms | 400.43ms | 265684 B |
| [more-nhs-uk-conditions-asthma](https://www.nhs.uk/conditions/asthma/) | health | 98.3% | 100.0% | 7/7 | 3/3 | 12.70ms | 13.57ms | 10467 B |
| [more-nhs-uk-conditions-type-2-diabetes](https://www.nhs.uk/conditions/type-2-diabetes/) | health | 100.0% | 100.0% | 3/3 | 1/1 | 8.74ms | 9.83ms | 652 B |
| [more-nhs-uk-conditions-high-blood-pressure](https://www.nhs.uk/conditions/high-blood-pressure/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 11.74ms | 12.09ms | 6904 B |
| [more-nhs-uk-conditions-dehydration](https://www.nhs.uk/conditions/dehydration/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 11.24ms | 12.19ms | 5225 B |
| [more-epa-gov-recycle-recycling-basics-and-benefits](https://www.epa.gov/recycle/recycling-basics-and-benefits) | science | 96.3% | 100.0% | 7/7 | 3/3 | 12.53ms | 13.06ms | 10256 B |
| [more-epa-gov-acidrain-effects-acid-rain](https://www.epa.gov/acidrain/effects-acid-rain) | science | 95.5% | 100.0% | 7/7 | 3/3 | 11.84ms | 12.15ms | 7081 B |
| [more-epa-gov-acidrain-what-acid-rain](https://www.epa.gov/acidrain/what-acid-rain) | science | 96.2% | 100.0% | 7/7 | 3/3 | 10.85ms | 11.27ms | 4991 B |
| [more-epa-gov-acidrain-acid-rain-program](https://www.epa.gov/acidrain/acid-rain-program) | science | 96.6% | 100.0% | 7/7 | 3/3 | 13.08ms | 13.90ms | 11097 B |
| [more-allrecipes-com-20144-banana-banana-bread](https://www.allrecipes.com/recipe/20144/banana-banana-bread/) | recipe | 90.8% | 99.6% | 7/7 | 3/3 | 34.64ms | 35.22ms | 7201 B |
| [more-allrecipes-com-10549-best-brownies](https://www.allrecipes.com/recipe/10549/best-brownies/) | recipe | 81.4% | 99.0% | 7/7 | 3/3 | 31.47ms | 33.90ms | 3246 B |
| [more-allrecipes-com-10813-best-chocolate-chip-cookies](https://www.allrecipes.com/recipe/10813/best-chocolate-chip-cookies/) | recipe | 91.6% | 99.6% | 6/7 | 3/3 | 34.72ms | 36.38ms | 8013 B |
| [more-allrecipes-com-16354-easy-meatloaf](https://www.allrecipes.com/recipe/16354/easy-meatloaf/) | recipe | 87.3% | 99.4% | 7/7 | 3/3 | 34.04ms | 36.36ms | 5404 B |
| [more-weather-gov-safety-lightning](https://www.weather.gov/safety/lightning) | safety | 94.8% | 100.0% | 4/4 | 3/3 | 12.63ms | 13.00ms | 3024 B |
| [more-weather-gov-safety-tornado](https://www.weather.gov/safety/tornado) | safety | 94.9% | 100.0% | 3/4 | 3/3 | 11.54ms | 12.69ms | 1284 B |
| [more-weather-gov-safety-flood](https://www.weather.gov/safety/flood) | safety | 93.9% | 100.0% | 3/3 | 2/2 | 11.82ms | 12.29ms | 1596 B |
| [more-weather-gov-safety-heat](https://www.weather.gov/safety/heat) | safety | 95.9% | 100.0% | 4/4 | 3/3 | 12.28ms | 12.69ms | 2320 B |
| [more-redcross-org-types-of-emergencies-earthquake](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/earthquake.html) | safety | 96.2% | 98.2% | 6/7 | 3/3 | 26.51ms | 27.64ms | 14023 B |
| [more-redcross-org-types-of-emergencies-flood](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/flood.html) | safety | 96.4% | 97.0% | 7/7 | 3/3 | 24.74ms | 27.61ms | 10824 B |
| [more-redcross-org-types-of-emergencies-hurricane](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/hurricane.html) | safety | 94.5% | 98.1% | 6/7 | 3/3 | 28.71ms | 31.93ms | 15502 B |
| [more-redcross-org-types-of-emergencies-tornado](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/tornado.html) | safety | 96.8% | 97.3% | 6/7 | 3/3 | 25.74ms | 29.22ms | 12810 B |
| [more-rspb-org-uk-birds-and-wildlife-robin](https://www.rspb.org.uk/birds-and-wildlife/robin) | nature | 98.7% | 100.0% | 7/7 | 3/3 | 17.71ms | 18.35ms | 5555 B |
| [more-rspb-org-uk-birds-and-wildlife-blackbird](https://www.rspb.org.uk/birds-and-wildlife/blackbird) | nature | 98.8% | 100.0% | 7/7 | 3/3 | 17.75ms | 19.15ms | 5964 B |
| [more-rspb-org-uk-birds-and-wildlife-blue-tit](https://www.rspb.org.uk/birds-and-wildlife/blue-tit) | nature | 93.6% | 100.0% | 5/7 | 2/3 | 17.06ms | 18.23ms | 4661 B |
| [more-rspb-org-uk-birds-and-wildlife-house-sparrow](https://www.rspb.org.uk/birds-and-wildlife/house-sparrow) | nature | 98.8% | 100.0% | 7/7 | 3/3 | 17.58ms | 18.99ms | 6379 B |
| [more-britannica-com-science-earthquake-geology](https://www.britannica.com/science/earthquake-geology) | encyclopedia | 91.8% | 100.0% | 7/7 | 3/3 | 18.91ms | 20.05ms | 14777 B |
| [more-britannica-com-science-plate-tectonics](https://www.britannica.com/science/plate-tectonics) | encyclopedia | 90.3% | 100.0% | 7/7 | 3/3 | 17.62ms | 18.16ms | 12401 B |
| [more-nhs-uk-medicines-antibiotics](https://www.nhs.uk/medicines/antibiotics/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.78ms | 11.21ms | 8031 B |
| [more-britannica-com-science-continental-drift-geology](https://www.britannica.com/science/continental-drift-geology) | encyclopedia | 93.4% | 100.0% | 5/6 | 3/3 | 19.55ms | 20.11ms | 19171 B |
| [more-worldhistory-org-silk-road](https://www.worldhistory.org/Silk_Road/) | history | 100.0% | 97.5% | 7/7 | 3/3 | 19.09ms | 19.85ms | 17901 B |
| [more-worldhistory-org-egypt](https://www.worldhistory.org/egypt/) | history | 100.0% | 99.0% | 7/7 | 3/3 | 27.49ms | 31.62ms | 36228 B |
| [more-worldhistory-org-roman-republic](https://www.worldhistory.org/Roman_Republic/) | history | 96.7% | 98.1% | 7/7 | 3/3 | 22.63ms | 24.98ms | 23289 B |
| [more-worldhistory-org-mesopotamia](https://www.worldhistory.org/Mesopotamia/) | history | 100.0% | 99.0% | 7/7 | 3/3 | 27.81ms | 30.87ms | 38933 B |
| [more-fda-gov-nutrition-food-labeling-and-critical-foods-changes-nutrition-facts-label](https://www.fda.gov/food/nutrition-food-labeling-and-critical-foods/changes-nutrition-facts-label?scrlybrkr=) | consumer-guide | 99.4% | 100.0% | 7/7 | 3/3 | 17.85ms | 19.27ms | 21661 B |
| [more-fda-gov-buy-store-serve-safe-food-safe-food-handling](https://www.fda.gov/food/buy-store-serve-safe-food/safe-food-handling) | consumer-guide | 98.5% | 100.0% | 8/8 | 4/4 | 10.75ms | 11.17ms | 5797 B |
| [more-nhs-uk-antibiotics-side-effects](https://www.nhs.uk/medicines/antibiotics/side-effects/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 8.95ms | 9.48ms | 3062 B |
| [more-fda-gov-consumer-updates-it-really-fda-approved](https://www.fda.gov/consumers/consumer-updates/it-really-fda-approved) | consumer-guide | 99.0% | 100.0% | 7/7 | 3/3 | 12.69ms | 13.19ms | 18311 B |
| [more-plato-stanford-edu-entries-ethics-virtue](https://plato.stanford.edu/entries/ethics-virtue/) | reference | 99.9% | 99.6% | 5/6 | 3/3 | 32.30ms | 34.14ms | 90396 B |
| [more-plato-stanford-edu-entries-consciousness](https://plato.stanford.edu/entries/consciousness/) | reference | 100.0% | 99.8% | 6/6 | 3/3 | 48.11ms | 49.35ms | 155073 B |
| [more-plato-stanford-edu-entries-scientific-method](https://plato.stanford.edu/entries/scientific-method/) | reference | 99.9% | 99.7% | 6/6 | 3/3 | 33.78ms | 35.72ms | 99549 B |
| [more-plato-stanford-edu-entries-logic-classical](https://plato.stanford.edu/entries/logic-classical/) | reference | 99.9% | 99.7% | 6/6 | 3/3 | 42.27ms | 44.69ms | 121932 B |

## By content type

| Kind | Pages | Recall | Precision | Checks |
|---|---:|---:|---:|---:|
| accessibility | 10 | 99.8% | 96.0% | 80/80 |
| api | 45 | 99.9% | 99.6% | 351/361 |
| blog | 30 | 99.8% | 99.6% | 205/205 |
| book | 3 | 100.0% | 99.7% | 11/11 |
| catalogue | 1 | 94.5% | 100.0% | 7/7 |
| configuration | 26 | 99.0% | 99.8% | 204/209 |
| consumer-guide | 3 | 99.0% | 100.0% | 22/22 |
| data-article | 10 | 99.2% | 100.0% | 71/80 |
| database | 26 | 98.2% | 99.6% | 182/187 |
| discussion | 2 | 100.0% | 98.1% | 13/13 |
| docs | 62 | 99.7% | 99.4% | 472/479 |
| encyclopedia | 14 | 96.4% | 99.9% | 94/99 |
| essay | 31 | 99.4% | 99.3% | 176/185 |
| explainer | 9 | 94.1% | 99.8% | 54/63 |
| guide | 14 | 99.8% | 100.0% | 96/96 |
| health | 24 | 99.9% | 99.6% | 163/163 |
| historical-document | 1 | 100.0% | 100.0% | 5/5 |
| history | 13 | 99.6% | 98.2% | 88/88 |
| manual | 59 | 99.3% | 99.8% | 408/429 |
| nature | 4 | 97.5% | 100.0% | 26/28 |
| recipe | 6 | 90.7% | 99.6% | 46/47 |
| reference | 4 | 99.9% | 99.7% | 23/24 |
| repair-guide | 1 | 99.1% | 97.4% | 7/7 |
| safety | 8 | 95.4% | 98.8% | 39/43 |
| science | 35 | 97.1% | 98.5% | 218/228 |
| standard | 11 | 99.8% | 100.0% | 56/62 |
| tutorial | 36 | 99.9% | 99.8% | 266/269 |
| visitor-guide | 12 | 98.9% | 100.0% | 82/83 |

## By corpus cohort

Held-out sites were selected and annotated before this evaluation, without tuning extraction on their output. Once inspected, they become regression evidence; future blind evaluations need fresh sites.

| Cohort | Pages | Recall | Precision | Checks | Critical |
|---|---:|---:|---:|---:|---:|---:|
| expansion | 60 | 99.2% | 99.6% | 418/429 | 226/227 |
| heldout | 20 | 98.4% | 99.5% | 146/148 | 74/74 |
| regression | 20 | 99.2% | 99.8% | 115/115 | 79/79 |
| scale | 320 | 99.2% | 99.4% | 2231/2309 | 1165/1175 |
| scale-new-sites | 80 | 97.8% | 99.6% | 555/572 | 275/276 |

## Failures and omissions

- **go-strings/source-link** (link, critical=false)
- **rfc-uri/source-link** (link, critical=false)
- **nasa-earth/opening** (text, critical=true)
- **owid-life/section-heading** (heading, critical=false)
- **owid-life/later-heading** (heading, critical=false)
- **owid-population/section-heading** (heading, critical=false)
- **owid-population/source-link** (link, critical=false)
- **paulgraham-lisp/source-link** (link, critical=false)
- **paulgraham-schedule/source-link** (link, critical=false)
- **fowler-microservices/later-heading** (heading, critical=false)
- **noaa-blue/source-link** (link, critical=false)
- **ffmpeg-filters/section-heading** (heading, critical=false)
- **ffmpeg-filters/later-heading** (heading, critical=false)
- **more-pkg-go-dev-bytes/source-link** (link, critical=false)
- **more-pkg-go-dev-errors/source-link** (link, critical=false)
- **more-pkg-go-dev-fmt/source-link** (link, critical=false)
- **more-pkg-go-dev-encoding-json/source-link** (link, critical=false)
- **more-pkg-go-dev-net-http/source-link** (link, critical=false)
- **more-sqlite-org-lang-insert/source-link** (link, critical=false)
- **more-sqlite-org-lang-update/source-link** (link, critical=false)
- **more-sqlite-org-lang-delete/source-link** (link, critical=false)
- **more-sqlite-org-lang-with/code-example** (code, critical=true)
- **more-sqlite-org-datatype3/code-example** (code, critical=true)
- **more-rfc-editor-org-rfc-rfc8446/source-link** (link, critical=false)
- **more-rfc-editor-org-rfc-rfc7519/source-link** (link, critical=false)
- **more-rfc-editor-org-rfc-rfc6455/source-link** (link, critical=false)
- **more-rfc-editor-org-rfc-rfc6902/source-link** (link, critical=false)
- **more-rfc-editor-org-rfc-rfc3339/source-link** (link, critical=false)
- **more-en-wikipedia-org-wiki-solar-system/source-link** (link, critical=false)
- **more-en-wikipedia-org-wiki-black-hole/source-link** (link, critical=false)
- **more-en-wikipedia-org-wiki-ada-lovelace/source-link** (link, critical=false)
- **more-en-wikipedia-org-wiki-fibonacci-sequence/middle** (text, critical=true)
- **more-nps-gov-planyourvisit-winter-safety/closing** (text, critical=true)
- **more-ourworldindata-org-plastic-pollution/section-heading** (heading, critical=false)
- **more-ourworldindata-org-plastic-pollution/source-link** (link, critical=false)
- **more-ourworldindata-org-economic-growth/section-heading** (heading, critical=false)
- **more-ourworldindata-org-child-mortality/section-heading** (heading, critical=false)
- **more-ourworldindata-org-vaccination/later-heading** (heading, critical=false)
- **more-paulgraham-com-hs/source-link** (link, critical=false)
- **more-paulgraham-com-startupideas/source-link** (link, critical=false)
- **more-paulgraham-com-procrastination/source-link** (link, critical=false)
- **more-paulgraham-com-nerds/source-link** (link, critical=false)
- **more-paulgraham-com-wealth/source-link** (link, critical=false)
- **more-paulgraham-com-cities/source-link** (link, critical=false)
- **more-oceanservice-noaa-gov-facts-tides/source-link** (link, critical=false)
- **more-oceanservice-noaa-gov-facts-current/opening** (text, critical=true)
- **more-oceanservice-noaa-gov-facts-acidification/source-link** (link, critical=false)
- **more-oceanservice-noaa-gov-facts-estuary/opening** (text, critical=true)
- **more-oceanservice-noaa-gov-facts-estuary/source-link** (link, critical=false)
- **more-oceanservice-noaa-gov-facts-kelp/source-link** (link, critical=false)
- **more-un-org-science-causes-effects-climate-change/closing** (text, critical=true)
- **more-un-org-science-causes-effects-climate-change/later-heading** (heading, critical=false)
- **more-un-org-science-causes-effects-climate-change/source-link** (link, critical=false)
- **more-un-org-raising-ambition-renewable-energy/closing** (text, critical=true)
- **more-un-org-raising-ambition-renewable-energy/later-heading** (heading, critical=false)
- **more-un-org-climate-issues-greenwashing/closing** (text, critical=true)
- **more-un-org-climate-issues-greenwashing/later-heading** (heading, critical=false)
- **more-un-org-climatechange-net-zero-coalition/closing** (text, critical=true)
- **more-un-org-climatechange-net-zero-coalition/later-heading** (heading, critical=false)
- **more-nhm-ac-uk-discover-what-is-natural-selection/later-heading** (heading, critical=false)
- **more-nhm-ac-uk-discover-dinosaur-extinction/section-heading** (heading, critical=false)
- **more-elixir-hexdocs-pm-enum/source-link** (link, critical=false)
- **more-elixir-hexdocs-pm-list/source-link** (link, critical=false)
- **more-elixir-hexdocs-pm-genserver/source-link** (link, critical=false)
- **more-elixir-hexdocs-pm-task/source-link** (link, critical=false)
- **more-ffmpeg-org-ffmpeg/section-heading** (heading, critical=false)
- **more-ffmpeg-org-ffmpeg/later-heading** (heading, critical=false)
- **more-ffmpeg-org-ffmpeg-formats/section-heading** (heading, critical=false)
- **more-ffmpeg-org-ffmpeg-formats/later-heading** (heading, critical=false)
- **more-ffmpeg-org-ffmpeg-codecs/section-heading** (heading, critical=false)
- **more-ffmpeg-org-ffmpeg-codecs/later-heading** (heading, critical=false)
- **more-ffmpeg-org-ffmpeg-protocols/section-heading** (heading, critical=false)
- **more-ffmpeg-org-ffmpeg-protocols/later-heading** (heading, critical=false)
- **more-ffmpeg-org-ffmpeg-utils/section-heading** (heading, critical=false)
- **more-ffmpeg-org-ffmpeg-utils/later-heading** (heading, critical=false)
- **more-ffmpeg-org-ffplay/section-heading** (heading, critical=false)
- **more-ffmpeg-org-ffplay/later-heading** (heading, critical=false)
- **more-ffmpeg-org-ffprobe/section-heading** (heading, critical=false)
- **more-ffmpeg-org-ffprobe/later-heading** (heading, critical=false)
- **more-ffmpeg-org-faq/section-heading** (heading, critical=false)
- **more-ffmpeg-org-faq/later-heading** (heading, critical=false)
- **more-angular-dev-components-inputs/source-link** (link, critical=false)
- **more-angular-dev-guide-forms/source-link** (link, critical=false)
- **more-angular-dev-forms-typed-forms/source-link** (link, critical=false)
- **more-angular-dev-guide-http/later-heading** (heading, critical=false)
- **more-react-dev-learn-describing-the-ui/source-link** (link, critical=false)
- **more-react-dev-learn-conditional-rendering/source-link** (link, critical=false)
- **more-react-dev-learn-responding-to-events/source-link** (link, critical=false)
- **more-nmap-org-book-man-examples/source-link** (link, critical=false)
- **more-nmap-org-book-man-port-scanning-basics/source-link** (link, critical=false)
- **more-nmap-org-book-man-version-detection/source-link** (link, critical=false)
- **more-docs-github-com-workflows-and-actions-workflow-syntax/section-heading** (heading, critical=false)
- **more-docs-github-com-workflows-and-actions-workflow-syntax/source-link** (link, critical=false)
- **more-docs-github-com-security-secure-use/section-heading** (heading, critical=false)
- **more-docs-github-com-choose-what-workflows-do-run-job-variations/section-heading** (heading, critical=false)
- **more-docs-github-com-reuse-automations-reuse-workflows/section-heading** (heading, critical=false)
- **more-developer-android-com-background-work-services/source-link** (link, critical=false)
- **more-developer-android-com-background-tasks-broadcasts/source-link** (link, critical=false)
- **more-developer-android-com-manifest-manifest-intro/source-link** (link, critical=false)
- **more-allrecipes-com-10813-best-chocolate-chip-cookies/source-link** (link, critical=false)
- **more-weather-gov-safety-tornado/source-link** (link, critical=false)
- **more-redcross-org-types-of-emergencies-earthquake/source-link** (link, critical=false)
- **more-redcross-org-types-of-emergencies-hurricane/source-link** (link, critical=false)
- **more-redcross-org-types-of-emergencies-tornado/source-link** (link, critical=false)
- **more-rspb-org-uk-birds-and-wildlife-blue-tit/closing** (text, critical=true)
- **more-rspb-org-uk-birds-and-wildlife-blue-tit/source-link** (link, critical=false)
- **more-britannica-com-science-continental-drift-geology/section-heading** (heading, critical=false)
- **more-plato-stanford-edu-entries-ethics-virtue/source-link** (link, critical=false)
