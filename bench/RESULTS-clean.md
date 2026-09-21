# Ketch extraction benchmark

2026-09-21T06:10:51Z · linux/amd64 · 24 logical CPUs

Binary build: go1.27.1, CGO_ENABLED=0.

Mode: **default**, extract mode **clean**. 7 measured runs + 1 warmups per page, 1 workers. Wall time 144.75s.

Corpus SHA256: `14c3ccc4f5eb5b6ef0bec2ca0727871d682d5845560ff7bce8bc70f8b719f441`  
Binary SHA256: `783051919c700cbc625b785048dbec314d46122d339e0bc38696cfd37fd27c7c`

**3468/3573 checks passed; 1822/1831 critical checks; 0 failed or nondeterministic pages.**

Token coverage compares independently annotated source text with parsed Markdown. It is a multiset measure, not semantic correctness. Code, headings, table associations, links and exclusions are checked separately. Scores include every page; pages have equal weight in macro averages. Per-invocation timing includes CLI startup and excludes build, source loading, network and grading. Wall time includes warmups, grading and artifact writes. Selector mode is assisted recovery, not default extraction.

Macro token recall **99.0%**, precision **99.4%**, F1 **99.2%**.

| Page | Kind | Recall | Precision | Checks | Critical | Median | p95 | Output |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| [npm](https://docs.npmjs.com/trusted-publishers/) | docs | 99.7% | 100.0% | 6/6 | 4/4 | 39.29ms | 41.69ms | 20172 B |
| [go](https://pkg.go.dev/context) | api | 100.0% | 98.3% | 7/7 | 5/5 | 22.02ms | 23.38ms | 26801 B |
| [python](https://docs.python.org/3/tutorial/errors.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 20.09ms | 20.52ms | 26313 B |
| [kubernetes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) | configuration | 97.3% | 100.0% | 6/6 | 4/4 | 37.50ms | 40.49ms | 19931 B |
| [postgres](https://www.postgresql.org/docs/current/transaction-iso.html) | database | 100.0% | 100.0% | 6/6 | 4/4 | 22.80ms | 24.01ms | 25062 B |
| [mdn](https://developer.mozilla.org/en-US/docs/Web/API/AbortController) | api | 95.7% | 100.0% | 7/7 | 4/4 | 12.64ms | 13.71ms | 2614 B |
| [rust](https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 18.67ms | 19.21ms | 28278 B |
| [sqlite](https://www.sqlite.org/lang_transaction.html) | database | 100.0% | 100.0% | 6/6 | 3/3 | 11.56ms | 13.16ms | 9515 B |
| [git](https://git-scm.com/docs/git-reset) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 26.40ms | 28.26ms | 19815 B |
| [gnu](https://www.gnu.org/software/coreutils/manual/html_node/Exit-status.html) | manual | 100.0% | 100.0% | 5/5 | 3/3 | 6.42ms | 6.87ms | 777 B |
| [terraform](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle) | configuration | 99.1% | 99.4% | 5/5 | 4/4 | 25.49ms | 26.94ms | 19244 B |
| [docker](https://docs.docker.com/build/building/multi-stage/) | tutorial | 100.0% | 100.0% | 5/5 | 4/4 | 37.33ms | 39.90ms | 6914 B |
| [cloudflare](https://blog.cloudflare.com/rfc-9457-agent-error-pages/) | blog | 99.8% | 99.7% | 7/7 | 5/5 | 38.87ms | 40.02ms | 18089 B |
| [danluu](https://danluu.com/slow-device/) | essay | 100.0% | 100.0% | 5/5 | 3/3 | 29.98ms | 31.09ms | 72798 B |
| [joel](https://www.joelonsoftware.com/2000/04/06/things-you-should-never-do-part-i/) | blog | 100.0% | 99.0% | 4/4 | 4/4 | 11.18ms | 11.97ms | 8770 B |
| [rfc](https://www.rfc-editor.org/rfc/rfc8259.html) | standard | 100.0% | 100.0% | 4/4 | 4/4 | 13.39ms | 15.26ms | 28586 B |
| [wikipedia](https://en.wikipedia.org/wiki/HTTP) | encyclopedia | 99.8% | 99.8% | 6/6 | 4/4 | 93.50ms | 98.35ms | 106256 B |
| [gutenberg](https://www.gutenberg.org/files/11/11-h/11-h.htm) | book | 100.0% | 99.6% | 4/4 | 3/3 | 53.72ms | 55.48ms | 152803 B |
| [nasa](https://science.nasa.gov/mars/facts/) | science | 92.5% | 99.4% | 5/5 | 4/4 | 21.45ms | 22.57ms | 9994 B |
| [nps](https://www.nps.gov/yell/planyourvisit/safety.htm) | visitor-guide | 99.4% | 100.0% | 9/9 | 5/5 | 23.82ms | 27.52ms | 25369 B |
| [npm-scripts](https://docs.npmjs.com/cli/v11/using-npm/scripts/) | docs | 100.0% | 99.6% | 8/8 | 4/4 | 44.28ms | 45.90ms | 15326 B |
| [npm-package-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-json/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 68.91ms | 73.17ms | 38256 B |
| [go-io](https://pkg.go.dev/io) | api | 100.0% | 98.1% | 8/8 | 4/4 | 36.78ms | 38.59ms | 41338 B |
| [go-strings](https://pkg.go.dev/strings) | api | 100.0% | 99.1% | 7/8 | 4/4 | 52.19ms | 52.75ms | 58885 B |
| [python-data](https://docs.python.org/3/tutorial/datastructures.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 23.21ms | 25.37ms | 26461 B |
| [python-classes](https://docs.python.org/3/tutorial/classes.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 24.68ms | 25.23ms | 38897 B |
| [kubernetes-deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 51.10ms | 52.61ms | 61534 B |
| [kubernetes-configmaps](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 45.54ms | 46.25ms | 35667 B |
| [postgres-constraints](https://www.postgresql.org/docs/current/ddl-constraints.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 20.55ms | 20.84ms | 23853 B |
| [postgres-queries](https://www.postgresql.org/docs/current/queries-table-expressions.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 29.31ms | 30.85ms | 35404 B |
| [mdn-promise](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Promise) | api | 99.9% | 100.0% | 9/9 | 5/5 | 25.53ms | 26.16ms | 35051 B |
| [mdn-fetch](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch) | api | 100.0% | 100.0% | 8/8 | 4/4 | 24.52ms | 25.13ms | 30351 B |
| [rust-enums](https://doc.rust-lang.org/book/ch06-01-defining-an-enum.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 13.80ms | 14.24ms | 17267 B |
| [rust-result](https://doc.rust-lang.org/book/ch09-02-recoverable-errors-with-result.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 19.01ms | 19.39ms | 28175 B |
| [sqlite-select](https://www.sqlite.org/lang_select.html) | database | 94.4% | 100.0% | 7/7 | 4/4 | 154.05ms | 157.53ms | 39004 B |
| [sqlite-foreignkeys](https://www.sqlite.org/foreignkeys.html) | database | 99.0% | 100.0% | 7/7 | 4/4 | 20.99ms | 21.58ms | 38101 B |
| [git-rebase](https://git-scm.com/docs/git-rebase) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 58.04ms | 58.58ms | 57762 B |
| [git-restore](https://git-scm.com/docs/git-restore) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 17.33ms | 17.66ms | 7806 B |
| [gnu-copy](https://www.gnu.org/software/coreutils/manual/html_node/cp-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 25.05ms | 25.76ms | 22373 B |
| [gnu-timeout](https://www.gnu.org/software/coreutils/manual/html_node/timeout-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 10.13ms | 10.43ms | 5420 B |
| [terraform-foreach](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each) | configuration | 100.0% | 99.7% | 8/8 | 4/4 | 22.25ms | 23.13ms | 15748 B |
| [terraform-count](https://developer.hashicorp.com/terraform/language/meta-arguments/count) | configuration | 100.0% | 99.4% | 8/8 | 4/4 | 18.53ms | 22.16ms | 7140 B |
| [docker-volumes](https://docs.docker.com/engine/storage/volumes/) | docs | 100.0% | 94.6% | 9/9 | 5/5 | 50.59ms | 52.05ms | 27138 B |
| [docker-bridge](https://docs.docker.com/engine/network/drivers/bridge/) | docs | 100.0% | 97.3% | 9/9 | 5/5 | 46.26ms | 47.18ms | 25782 B |
| [cloudflare-pingora](https://blog.cloudflare.com/how-we-built-pingora-the-proxy-that-connects-cloudflare-to-the-internet/) | blog | 99.9% | 99.7% | 7/7 | 3/3 | 27.37ms | 28.03ms | 16846 B |
| [cloudflare-opensource](https://blog.cloudflare.com/pingora-open-source/) | blog | 99.8% | 99.7% | 8/8 | 4/4 | 32.12ms | 33.15ms | 12380 B |
| [danluu-branch](https://danluu.com/branch-prediction/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 16.04ms | 17.25ms | 36462 B |
| [danluu-malloc](https://danluu.com/malloc-tutorial/) | essay | 100.0% | 100.0% | 6/6 | 4/4 | 12.07ms | 12.67ms | 21111 B |
| [joel-test](https://www.joelonsoftware.com/2000/08/09/the-joel-test-12-steps-to-better-code/) | blog | 100.0% | 99.6% | 5/5 | 3/3 | 13.85ms | 13.97ms | 21694 B |
| [joel-leaky](https://www.joelonsoftware.com/2002/11/11/the-law-of-leaky-abstractions/) | blog | 100.0% | 99.4% | 5/5 | 3/3 | 10.99ms | 11.99ms | 12962 B |
| [rfc-http](https://www.rfc-editor.org/rfc/rfc9110.html) | standard | 99.9% | 100.0% | 7/7 | 4/4 | 335.74ms | 342.03ms | 721134 B |
| [rfc-uri](https://www.rfc-editor.org/rfc/rfc3986.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 30.23ms | 30.43ms | 142714 B |
| [wikipedia-photosynthesis](https://en.wikipedia.org/wiki/Photosynthesis) | encyclopedia | 99.6% | 99.9% | 8/8 | 4/4 | 119.18ms | 127.48ms | 205015 B |
| [wikipedia-binary](https://en.wikipedia.org/wiki/Binary_search) | encyclopedia | 92.3% | 99.9% | 8/8 | 4/4 | 113.01ms | 115.32ms | 127166 B |
| [gutenberg-wallpaper](https://www.gutenberg.org/files/1952/1952-h/1952-h.htm) | book | 100.0% | 99.8% | 4/4 | 3/3 | 15.61ms | 17.06ms | 32151 B |
| [gutenberg-metamorphosis](https://www.gutenberg.org/files/5200/5200-h/5200-h.htm) | book | 100.0% | 99.8% | 3/3 | 3/3 | 28.68ms | 30.56ms | 120067 B |
| [nasa-earth](https://science.nasa.gov/earth/facts/) | science | 97.8% | 100.0% | 7/7 | 3/3 | 21.88ms | 22.60ms | 10395 B |
| [nasa-jupiter](https://science.nasa.gov/jupiter/jupiter-facts/) | science | 99.1% | 100.0% | 7/7 | 3/3 | 19.10ms | 19.61ms | 12956 B |
| [nps-hiking](https://www.nps.gov/grca/planyourvisit/hiking-faq.htm) | visitor-guide | 99.8% | 100.0% | 6/6 | 3/3 | 21.53ms | 22.12ms | 28983 B |
| [nps-canyon](https://www.nps.gov/grca/faqs.htm) | visitor-guide | 100.0% | 100.0% | 7/7 | 3/3 | 21.05ms | 21.23ms | 25687 B |
| [pandas-indexing](https://pandas.pydata.org/docs/user_guide/indexing.html) | api | 99.7% | 100.0% | 9/9 | 5/5 | 59.56ms | 60.63ms | 98766 B |
| [pandas-merge](https://pandas.pydata.org/docs/user_guide/merging.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 36.08ms | 37.80ms | 54464 B |
| [django-queries](https://docs.djangoproject.com/en/5.2/topics/db/queries/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 55.12ms | 55.61ms | 85748 B |
| [django-transactions](https://docs.djangoproject.com/en/5.2/topics/db/transactions/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 25.10ms | 28.86ms | 35970 B |
| [curl-manpage](https://curl.se/docs/manpage.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 201.62ms | 203.39ms | 334470 B |
| [curl-http](https://curl.se/docs/httpscripting.html) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 17.41ms | 17.66ms | 28331 B |
| [w3c-tables](https://www.w3.org/WAI/tutorials/tables/one-header/) | accessibility | 100.0% | 94.5% | 9/9 | 5/5 | 10.95ms | 11.16ms | 4734 B |
| [w3c-irregular](https://www.w3.org/WAI/tutorials/tables/irregular/) | accessibility | 100.0% | 96.7% | 8/8 | 4/4 | 12.02ms | 12.58ms | 7013 B |
| [owid-life](https://ourworldindata.org/life-expectancy) | data-article | 99.4% | 100.0% | 6/8 | 4/4 | 30.82ms | 32.04ms | 40559 B |
| [owid-population](https://ourworldindata.org/population-growth) | data-article | 99.0% | 100.0% | 6/8 | 4/4 | 25.87ms | 26.30ms | 30235 B |
| [eff-passwords](https://ssd.eff.org/module/creating-strong-passwords) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 11.81ms | 12.00ms | 8678 B |
| [eff-plan](https://ssd.eff.org/module/your-security-plan) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 12.04ms | 12.43ms | 9438 B |
| [paulgraham-lisp](https://www.paulgraham.com/avg.html) | essay | 99.3% | 100.0% | 3/4 | 3/3 | 15.38ms | 15.60ms | 25543 B |
| [paulgraham-schedule](https://www.paulgraham.com/makersschedule.html) | essay | 98.5% | 100.0% | 3/4 | 3/3 | 9.46ms | 10.19ms | 6665 B |
| [fowler-microservices](https://martinfowler.com/articles/microservices.html) | essay | 90.0% | 96.4% | 6/7 | 3/3 | 21.33ms | 22.70ms | 46917 B |
| [fowler-debt](https://martinfowler.com/bliki/TechnicalDebt.html) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 9.54ms | 10.92ms | 7491 B |
| [letsencrypt-challenges](https://letsencrypt.org/docs/challenge-types/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 9.36ms | 9.69ms | 7690 B |
| [letsencrypt-compatibility](https://letsencrypt.org/docs/certificate-compatibility/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 9.10ms | 9.72ms | 5133 B |
| [noaa-salt](https://oceanservice.noaa.gov/facts/whysalty.html) | science | 100.0% | 100.0% | 6/6 | 3/3 | 9.72ms | 12.72ms | 3082 B |
| [noaa-blue](https://oceanservice.noaa.gov/facts/oceanblue.html) | science | 93.0% | 100.0% | 5/6 | 3/3 | 8.89ms | 10.48ms | 743 B |
| [bgs-earthquakes](https://www.bgs.ac.uk/discovering-geology/earth-hazards/earthquakes/how-are-earthquakes-detected/) | science | 97.7% | 99.6% | 8/8 | 4/4 | 19.85ms | 20.44ms | 17380 B |
| [medlineplus-handwashing](https://medlineplus.gov/ency/patientinstructions/000972.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.11ms | 10.37ms | 5154 B |
| [who-resistance](https://www.who.int/news-room/fact-sheets/detail/antimicrobial-resistance) | health | 100.0% | 97.6% | 7/7 | 3/3 | 15.27ms | 15.59ms | 12102 B |
| [un-climate](https://www.un.org/en/climatechange/what-is-climate-change) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 26.54ms | 27.04ms | 11847 B |
| [computerhistory-1969](https://www.computerhistory.org/timeline/1969/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 13.38ms | 13.76ms | 6033 B |
| [nhm-meteorites](https://www.nhm.ac.uk/discover/types-of-meteorites.html) | science | 87.6% | 99.9% | 7/7 | 3/3 | 18.63ms | 19.02ms | 9531 B |
| [standardebooks-austen](https://standardebooks.org/ebooks/jane-austen/pride-and-prejudice) | catalogue | 94.5% | 100.0% | 7/7 | 3/3 | 10.05ms | 10.26ms | 5095 B |
| [archives-declaration](https://www.archives.gov/founding-docs/declaration-transcript) | historical-document | 100.0% | 100.0% | 5/5 | 3/3 | 14.05ms | 15.52ms | 9865 B |
| [wikivoyage-paris](https://en.wikivoyage.org/wiki/Paris) | visitor-guide | 99.7% | 100.0% | 8/8 | 4/4 | 227.41ms | 230.01ms | 227834 B |
| [kingarthur-bread](https://www.kingarthurbaking.com/recipes/classic-sandwich-bread-recipe) | recipe | 95.4% | 100.0% | 10/10 | 6/6 | 22.11ms | 23.42ms | 7161 B |
| [seriouseats-cookies](https://www.seriouseats.com/the-food-lab-best-chocolate-chip-cookie-recipe) | recipe | 97.9% | 100.0% | 9/9 | 5/5 | 47.50ms | 48.93ms | 52645 B |
| [ifixit-moped](https://www.ifixit.com/Guide/Suzuki+FA50+Moped+Exhaust+Cleaning/3926) | repair-guide | 100.0% | 97.4% | 7/7 | 3/3 | 28.03ms | 30.07ms | 6449 B |
| [discourse-guide](https://meta.discourse.org/t/understanding-discourse-for-new-users/96331) | discussion | 100.0% | 96.4% | 8/8 | 4/4 | 18.85ms | 19.08ms | 15673 B |
| [hackernews-startup](https://news.ycombinator.com/item?id=8863) | discussion | 100.0% | 99.7% | 5/5 | 3/3 | 40.07ms | 44.00ms | 34613 B |
| [julia-arrays](https://docs.julialang.org/en/v1/manual/arrays/) | manual | 100.0% | 100.0% | 9/9 | 5/5 | 28.87ms | 29.75ms | 48421 B |
| [elixir-enum](https://elixir.hexdocs.pm/enum-cheat.html) | api | 99.8% | 99.6% | 8/8 | 4/4 | 32.54ms | 34.78ms | 22538 B |
| [ffmpeg-filters](https://ffmpeg.org/ffmpeg-filters.html) | manual | 97.0% | 100.0% | 5/7 | 4/4 | 921.09ms | 936.77ms | 919390 B |
| [angular-signals](https://angular.dev/guide/signals) | docs | 99.7% | 100.0% | 8/8 | 4/4 | 20.06ms | 20.70ms | 12489 B |
| [react-state](https://react.dev/learn/state-a-components-memory) | tutorial | 99.8% | 99.6% | 8/8 | 4/4 | 29.46ms | 31.43ms | 21608 B |
| [ncat-guide](https://nmap.org/ncat/guide/index.html) | manual | 99.5% | 100.0% | 6/6 | 3/3 | 9.26ms | 9.52ms | 4978 B |
| [more-docs-npmjs-com-commands-npm-install](https://docs.npmjs.com/cli/v11/commands/npm-install/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 56.31ms | 57.65ms | 36424 B |
| [more-docs-npmjs-com-commands-npm-ci](https://docs.npmjs.com/cli/v11/commands/npm-ci/) | docs | 100.0% | 99.6% | 8/8 | 4/4 | 36.65ms | 37.55ms | 14366 B |
| [more-docs-npmjs-com-commands-npm-publish](https://docs.npmjs.com/cli/v11/commands/npm-publish/) | docs | 100.0% | 99.3% | 8/8 | 4/4 | 33.08ms | 35.83ms | 8726 B |
| [more-docs-npmjs-com-commands-npm-audit](https://docs.npmjs.com/cli/v11/commands/npm-audit/) | docs | 100.0% | 99.7% | 8/8 | 4/4 | 40.56ms | 42.54ms | 16307 B |
| [more-docs-npmjs-com-commands-npm-exec](https://docs.npmjs.com/cli/v11/commands/npm-exec/) | docs | 100.0% | 99.6% | 8/8 | 4/4 | 35.27ms | 36.94ms | 13605 B |
| [more-docs-npmjs-com-commands-npm-run](https://docs.npmjs.com/cli/v11/commands/npm-run/) | docs | 100.0% | 99.3% | 8/8 | 4/4 | 30.28ms | 31.15ms | 7839 B |
| [more-docs-npmjs-com-configuring-npm-package-lock-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/) | docs | 100.0% | 99.5% | 7/7 | 3/3 | 26.08ms | 28.22ms | 11037 B |
| [more-docs-npmjs-com-using-npm-workspaces](https://docs.npmjs.com/cli/v11/using-npm/workspaces/) | docs | 100.0% | 99.2% | 8/8 | 4/4 | 29.51ms | 31.32ms | 6840 B |
| [more-pkg-go-dev-bytes](https://pkg.go.dev/bytes) | api | 100.0% | 99.3% | 7/8 | 4/4 | 61.86ms | 62.69ms | 75546 B |
| [more-pkg-go-dev-errors](https://pkg.go.dev/errors) | api | 100.0% | 97.6% | 7/8 | 4/4 | 19.21ms | 20.23ms | 16958 B |
| [more-pkg-go-dev-fmt](https://pkg.go.dev/fmt) | api | 100.0% | 99.3% | 7/8 | 4/4 | 32.83ms | 34.44ms | 51134 B |
| [more-pkg-go-dev-sync](https://pkg.go.dev/sync) | api | 99.9% | 98.0% | 8/8 | 4/4 | 30.55ms | 31.41ms | 33963 B |
| [more-pkg-go-dev-time](https://pkg.go.dev/time) | api | 99.9% | 99.3% | 8/8 | 4/4 | 62.24ms | 63.36ms | 94611 B |
| [more-pkg-go-dev-encoding-json](https://pkg.go.dev/encoding/json) | api | 100.0% | 97.9% | 7/8 | 4/4 | 50.48ms | 51.51ms | 77430 B |
| [more-pkg-go-dev-net-http](https://pkg.go.dev/net/http) | api | 99.9% | 98.7% | 7/8 | 4/4 | 103.42ms | 104.84ms | 199228 B |
| [more-pkg-go-dev-os](https://pkg.go.dev/os) | api | 99.8% | 98.8% | 8/8 | 4/4 | 69.28ms | 72.79ms | 93932 B |
| [more-docs-python-org-tutorial-controlflow](https://docs.python.org/3/tutorial/controlflow.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 30.05ms | 31.28ms | 40934 B |
| [more-docs-python-org-tutorial-modules](https://docs.python.org/3/tutorial/modules.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 19.48ms | 20.32ms | 25859 B |
| [more-docs-python-org-tutorial-inputoutput](https://docs.python.org/3/tutorial/inputoutput.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 19.60ms | 19.87ms | 23054 B |
| [more-docs-python-org-tutorial-stdlib](https://docs.python.org/3/tutorial/stdlib.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 15.56ms | 15.79ms | 15475 B |
| [more-docs-python-org-tutorial-stdlib2](https://docs.python.org/3/tutorial/stdlib2.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 15.89ms | 16.40ms | 17559 B |
| [more-docs-python-org-tutorial-venv](https://docs.python.org/3/tutorial/venv.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 11.17ms | 11.73ms | 7778 B |
| [more-docs-python-org-tutorial-floatingpoint](https://docs.python.org/3/tutorial/floatingpoint.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 14.40ms | 15.78ms | 13632 B |
| [more-docs-python-org-tutorial-interpreter](https://docs.python.org/3/tutorial/interpreter.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 11.22ms | 12.48ms | 6915 B |
| [more-kubernetes-io-workloads-pods](https://kubernetes.io/docs/concepts/workloads/pods/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 39.76ms | 41.31ms | 30823 B |
| [more-kubernetes-io-controllers-statefulset](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 41.32ms | 43.65ms | 32748 B |
| [more-kubernetes-io-services-networking-service](https://kubernetes.io/docs/concepts/services-networking/service/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 58.62ms | 60.09ms | 49167 B |
| [more-kubernetes-io-services-networking-ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 44.84ms | 45.78ms | 34332 B |
| [more-kubernetes-io-storage-persistent-volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 50.55ms | 52.12ms | 51294 B |
| [more-kubernetes-io-configuration-secret](https://kubernetes.io/docs/concepts/configuration/secret/) | configuration | 100.0% | 100.0% | 9/9 | 5/5 | 44.33ms | 46.08ms | 38565 B |
| [more-kubernetes-io-scheduling-eviction-taint-and-toleration](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 38.04ms | 39.86ms | 20720 B |
| [more-kubernetes-io-working-with-objects-labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) | configuration | 100.0% | 100.0% | 8/8 | 4/4 | 35.05ms | 36.95ms | 15851 B |
| [more-postgresql-org-current-ddl-default](https://www.postgresql.org/docs/current/ddl-default.html) | database | 100.0% | 100.0% | 7/7 | 4/4 | 10.75ms | 12.23ms | 1913 B |
| [more-postgresql-org-current-ddl-alter](https://www.postgresql.org/docs/current/ddl-alter.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 12.08ms | 13.46ms | 6502 B |
| [more-postgresql-org-current-ddl-inherit](https://www.postgresql.org/docs/current/ddl-inherit.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 15.31ms | 17.40ms | 11708 B |
| [more-postgresql-org-current-ddl-partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 30.31ms | 31.56ms | 44403 B |
| [more-postgresql-org-current-indexes-intro](https://www.postgresql.org/docs/current/indexes-intro.html) | database | 100.0% | 100.0% | 7/7 | 4/4 | 10.79ms | 11.21ms | 4420 B |
| [more-postgresql-org-current-queries-with](https://www.postgresql.org/docs/current/queries-with.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 19.63ms | 20.91ms | 20470 B |
| [more-postgresql-org-current-queries-order](https://www.postgresql.org/docs/current/queries-order.html) | database | 100.0% | 100.0% | 6/6 | 4/4 | 11.53ms | 12.23ms | 3325 B |
| [more-postgresql-org-current-sql-select](https://www.postgresql.org/docs/current/sql-select.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 49.11ms | 50.71ms | 67788 B |
| [more-developer-mozilla-org-global-objects-map](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Map) | api | 100.0% | 100.0% | 8/8 | 4/4 | 22.93ms | 25.54ms | 23235 B |
| [more-developer-mozilla-org-global-objects-set](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Set) | api | 100.0% | 100.0% | 9/9 | 5/5 | 22.35ms | 22.82ms | 22452 B |
| [more-developer-mozilla-org-guide-iterators-and-generators](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Iterators_and_generators) | api | 100.0% | 100.0% | 8/8 | 4/4 | 19.78ms | 21.79ms | 11626 B |
| [more-developer-mozilla-org-grid-layout-basic-concepts](https://developer.mozilla.org/en-US/docs/Web/CSS/Guides/Grid_layout/Basic_concepts) | api | 100.0% | 100.0% | 8/8 | 4/4 | 33.44ms | 36.99ms | 24565 B |
| [more-developer-mozilla-org-elements-details](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/details) | api | 100.0% | 100.0% | 9/9 | 5/5 | 19.63ms | 20.87ms | 10302 B |
| [more-developer-mozilla-org-api-intersection-observer-api](https://developer.mozilla.org/en-US/docs/Web/API/Intersection_Observer_API) | api | 99.8% | 100.0% | 9/9 | 5/5 | 27.34ms | 29.77ms | 44946 B |
| [more-developer-mozilla-org-guides-overview](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Overview) | api | 100.0% | 100.0% | 8/8 | 4/4 | 22.01ms | 23.81ms | 17726 B |
| [more-developer-mozilla-org-accessibility-html](https://developer.mozilla.org/en-US/docs/Learn_web_development/Core/Accessibility/HTML) | api | 100.0% | 99.9% | 8/8 | 4/4 | 27.35ms | 28.17ms | 39287 B |
| [more-doc-rust-lang-org-book-ch03-01-variables-and-mutability](https://doc.rust-lang.org/book/ch03-01-variables-and-mutability.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 10.88ms | 11.23ms | 10258 B |
| [more-doc-rust-lang-org-book-ch03-02-data-types](https://doc.rust-lang.org/book/ch03-02-data-types.html) | tutorial | 100.0% | 100.0% | 8/8 | 5/5 | 14.43ms | 14.87ms | 17168 B |
| [more-doc-rust-lang-org-book-ch04-02-references-and-borrowing](https://doc.rust-lang.org/book/ch04-02-references-and-borrowing.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 11.29ms | 13.18ms | 13651 B |
| [more-doc-rust-lang-org-book-ch05-01-defining-structs](https://doc.rust-lang.org/book/ch05-01-defining-structs.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 13.02ms | 13.60ms | 14644 B |
| [more-doc-rust-lang-org-book-ch08-01-vectors](https://doc.rust-lang.org/book/ch08-01-vectors.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 11.02ms | 12.26ms | 12925 B |
| [more-doc-rust-lang-org-book-ch10-02-traits](https://doc.rust-lang.org/book/ch10-02-traits.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 16.50ms | 17.11ms | 23109 B |
| [more-doc-rust-lang-org-book-ch13-02-iterators](https://doc.rust-lang.org/book/ch13-02-iterators.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 11.97ms | 12.10ms | 12193 B |
| [more-doc-rust-lang-org-book-ch16-01-threads](https://doc.rust-lang.org/book/ch16-01-threads.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 12.02ms | 12.51ms | 14839 B |
| [more-sqlite-org-lang-insert](https://www.sqlite.org/lang_insert.html) | database | 93.6% | 100.0% | 3/4 | 3/3 | 28.29ms | 31.81ms | 5072 B |
| [more-sqlite-org-lang-update](https://www.sqlite.org/lang_update.html) | database | 93.1% | 100.0% | 6/7 | 4/4 | 51.04ms | 53.12ms | 8724 B |
| [more-sqlite-org-lang-delete](https://www.sqlite.org/lang_delete.html) | database | 90.4% | 100.0% | 3/4 | 3/3 | 35.22ms | 36.29ms | 5862 B |
| [more-sqlite-org-lang-createtable](https://www.sqlite.org/lang_createtable.html) | database | 94.6% | 100.0% | 8/8 | 5/5 | 68.05ms | 69.28ms | 20839 B |
| [more-sqlite-org-lang-with](https://www.sqlite.org/lang_with.html) | database | 97.7% | 100.0% | 7/7 | 4/4 | 39.63ms | 41.89ms | 27190 B |
| [more-sqlite-org-windowfunctions](https://www.sqlite.org/windowfunctions.html) | database | 96.8% | 100.0% | 8/8 | 5/5 | 84.88ms | 88.03ms | 35478 B |
| [more-sqlite-org-datatype3](https://www.sqlite.org/datatype3.html) | database | 97.8% | 100.0% | 8/8 | 5/5 | 19.69ms | 20.05ms | 30285 B |
| [more-sqlite-org-isolation](https://www.sqlite.org/isolation.html) | database | 100.0% | 99.7% | 6/6 | 3/3 | 10.38ms | 11.18ms | 14282 B |
| [more-git-scm-com-docs-git-merge](https://git-scm.com/docs/git-merge) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 44.89ms | 46.51ms | 37035 B |
| [more-git-scm-com-docs-git-cherry-pick](https://git-scm.com/docs/git-cherry-pick) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 19.04ms | 21.27ms | 10265 B |
| [more-git-scm-com-docs-git-bisect](https://git-scm.com/docs/git-bisect) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 23.39ms | 23.66ms | 16725 B |
| [more-git-scm-com-docs-git-stash](https://git-scm.com/docs/git-stash) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 26.86ms | 27.98ms | 17260 B |
| [more-git-scm-com-docs-git-reflog](https://git-scm.com/docs/git-reflog) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 14.84ms | 15.35ms | 5674 B |
| [more-git-scm-com-docs-git-worktree](https://git-scm.com/docs/git-worktree) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 30.37ms | 31.86ms | 23644 B |
| [more-git-scm-com-docs-git-fetch](https://git-scm.com/docs/git-fetch) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 52.08ms | 53.27ms | 47366 B |
| [more-git-scm-com-docs-git-log](https://git-scm.com/docs/git-log) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 120.47ms | 126.31ms | 116066 B |
| [more-gnu-org-html-node-ls-invocation](https://www.gnu.org/software/coreutils/manual/html_node/ls-invocation.html) | manual | 100.0% | 87.3% | 6/6 | 4/4 | 8.27ms | 8.55ms | 2859 B |
| [more-gnu-org-html-node-mv-invocation](https://www.gnu.org/software/coreutils/manual/html_node/mv-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 13.23ms | 14.29ms | 8882 B |
| [more-gnu-org-html-node-rm-invocation](https://www.gnu.org/software/coreutils/manual/html_node/rm-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 10.96ms | 11.36ms | 5695 B |
| [more-gnu-org-html-node-dd-invocation](https://www.gnu.org/software/coreutils/manual/html_node/dd-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 24.31ms | 24.66ms | 22695 B |
| [more-gnu-org-html-node-sort-invocation](https://www.gnu.org/software/coreutils/manual/html_node/sort-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 22.87ms | 24.44ms | 27407 B |
| [more-gnu-org-html-node-uniq-invocation](https://www.gnu.org/software/coreutils/manual/html_node/uniq-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 11.80ms | 12.16ms | 6818 B |
| [more-gnu-org-html-node-date-invocation](https://www.gnu.org/software/coreutils/manual/html_node/date-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 6.87ms | 8.26ms | 1603 B |
| [more-gnu-org-html-node-chmod-invocation](https://www.gnu.org/software/coreutils/manual/html_node/chmod-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 12.22ms | 12.58ms | 6756 B |
| [more-developer-hashicorp-com-values-variables](https://developer.hashicorp.com/terraform/language/values/variables) | configuration | 100.0% | 99.7% | 8/8 | 4/4 | 19.93ms | 21.81ms | 12410 B |
| [more-developer-hashicorp-com-values-outputs](https://developer.hashicorp.com/terraform/language/values/outputs) | configuration | 100.0% | 99.1% | 8/8 | 4/4 | 16.20ms | 16.80ms | 4627 B |
| [more-developer-hashicorp-com-values-locals](https://developer.hashicorp.com/terraform/language/values/locals) | configuration | 100.0% | 98.8% | 8/8 | 4/4 | 14.96ms | 15.33ms | 3338 B |
| [more-developer-hashicorp-com-expressions-types](https://developer.hashicorp.com/terraform/language/expressions/types) | configuration | 100.0% | 99.5% | 8/8 | 4/4 | 18.48ms | 20.10ms | 8030 B |
| [more-developer-hashicorp-com-expressions-conditionals](https://developer.hashicorp.com/terraform/language/expressions/conditionals) | configuration | 100.0% | 99.5% | 8/8 | 4/4 | 19.39ms | 22.76ms | 7637 B |
| [more-developer-hashicorp-com-expressions-for](https://developer.hashicorp.com/terraform/language/expressions/for) | configuration | 100.0% | 99.5% | 8/8 | 4/4 | 17.91ms | 18.57ms | 6737 B |
| [more-developer-hashicorp-com-block-module](https://developer.hashicorp.com/terraform/language/block/module) | configuration | 100.0% | 99.9% | 8/8 | 4/4 | 41.79ms | 43.53ms | 37439 B |
| [more-developer-hashicorp-com-meta-arguments-depends-on](https://developer.hashicorp.com/terraform/language/meta-arguments/depends_on) | configuration | 100.0% | 99.6% | 8/8 | 4/4 | 18.51ms | 19.04ms | 8964 B |
| [more-docs-docker-com-building-best-practices](https://docs.docker.com/build/building/best-practices/) | docs | 100.0% | 95.9% | 8/8 | 4/4 | 50.17ms | 51.93ms | 32980 B |
| [more-docs-docker-com-build-cache](https://docs.docker.com/build/cache/) | docs | 100.0% | 98.9% | 8/8 | 4/4 | 31.66ms | 33.55ms | 2075 B |
| [more-docs-docker-com-building-secrets](https://docs.docker.com/build/building/secrets/) | docs | 100.0% | 99.8% | 8/8 | 4/4 | 38.43ms | 40.12ms | 9483 B |
| [more-docs-docker-com-storage-bind-mounts](https://docs.docker.com/engine/storage/bind-mounts/) | docs | 100.0% | 98.6% | 9/9 | 5/5 | 42.88ms | 45.03ms | 17842 B |
| [more-docs-docker-com-drivers-host](https://docs.docker.com/engine/network/drivers/host/) | docs | 100.0% | 92.5% | 8/8 | 4/4 | 35.04ms | 35.90ms | 5879 B |
| [more-docs-docker-com-containers-resource-constraints](https://docs.docker.com/engine/containers/resource_constraints/) | docs | 100.0% | 98.1% | 9/9 | 5/5 | 36.36ms | 39.04ms | 13877 B |
| [more-docs-docker-com-how-tos-startup-order](https://docs.docker.com/compose/how-tos/startup-order/) | docs | 100.0% | 99.2% | 8/8 | 4/4 | 32.28ms | 32.81ms | 2867 B |
| [more-docs-docker-com-environment-variables-set-environment-variables](https://docs.docker.com/compose/how-tos/environment-variables/set-environment-variables/) | docs | 100.0% | 93.4% | 8/8 | 4/4 | 35.89ms | 38.56ms | 5459 B |
| [more-blog-cloudflare-com-how-pingora-keeps-count](https://blog.cloudflare.com/how-pingora-keeps-count/) | blog | 99.8% | 99.6% | 9/9 | 5/5 | 32.69ms | 35.08ms | 12133 B |
| [more-blog-cloudflare-com-cloudflare-workers-unleashed](https://blog.cloudflare.com/cloudflare-workers-unleashed/) | blog | 99.4% | 99.5% | 8/8 | 4/4 | 30.79ms | 32.37ms | 9452 B |
| [more-blog-cloudflare-com-introducing-cloudflare-workers](https://blog.cloudflare.com/introducing-cloudflare-workers/) | blog | 99.8% | 99.8% | 8/8 | 4/4 | 36.88ms | 37.83ms | 19160 B |
| [more-blog-cloudflare-com-introducing-cache-reserve](https://blog.cloudflare.com/introducing-cache-reserve/) | blog | 99.6% | 99.6% | 7/7 | 3/3 | 25.64ms | 26.54ms | 12009 B |
| [more-blog-cloudflare-com-the-sad-state-of-linux-socket-balancing](https://blog.cloudflare.com/the-sad-state-of-linux-socket-balancing/) | blog | 99.8% | 99.6% | 8/8 | 4/4 | 33.29ms | 35.00ms | 15454 B |
| [more-blog-cloudflare-com-keepalives-considered-harmful](https://blog.cloudflare.com/keepalives-considered-harmful/) | blog | 100.0% | 99.7% | 8/8 | 4/4 | 35.92ms | 38.47ms | 16850 B |
| [more-blog-cloudflare-com-road-to-grpc](https://blog.cloudflare.com/road-to-grpc/) | blog | 100.0% | 99.4% | 7/7 | 3/3 | 25.93ms | 27.92ms | 13516 B |
| [more-blog-cloudflare-com-the-road-to-quic](https://blog.cloudflare.com/the-road-to-quic/) | blog | 99.8% | 99.7% | 7/7 | 3/3 | 28.08ms | 28.89ms | 18690 B |
| [more-danluu-com-testing](https://danluu.com/testing/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 14.71ms | 14.92ms | 32543 B |
| [more-danluu-com-latency-mitigation](https://danluu.com/latency-mitigation/) | essay | 100.0% | 100.0% | 5/5 | 4/4 | 13.94ms | 14.28ms | 33575 B |
| [more-danluu-com-input-lag](https://danluu.com/input-lag/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 18.51ms | 18.77ms | 40532 B |
| [more-danluu-com-file-consistency](https://danluu.com/file-consistency/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 19.80ms | 20.05ms | 32942 B |
| [more-danluu-com-postmortem-lessons](https://danluu.com/postmortem-lessons/) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 10.39ms | 10.63ms | 16401 B |
| [more-danluu-com-keyboard-latency](https://danluu.com/keyboard-latency/) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 14.14ms | 15.62ms | 29956 B |
| [more-danluu-com-cpu-bugs](https://danluu.com/cpu-bugs/) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 13.12ms | 14.56ms | 28004 B |
| [more-danluu-com-simple-architectures](https://danluu.com/simple-architectures/) | essay | 100.0% | 100.0% | 5/5 | 3/3 | 9.55ms | 9.87ms | 13769 B |
| [more-joelonsoftware-com-02-painless-functional-specifications-part-1-why-bother](https://www.joelonsoftware.com/2000/10/02/painless-functional-specifications-part-1-why-bother/) | blog | 100.0% | 99.3% | 5/5 | 3/3 | 11.26ms | 11.97ms | 14860 B |
| [more-joelonsoftware-com-22-three-wrong-ideas-from-computer-science](https://www.joelonsoftware.com/2000/08/22/three-wrong-ideas-from-computer-science/) | blog | 100.0% | 98.9% | 7/7 | 3/3 | 10.03ms | 10.87ms | 8340 B |
| [more-joelonsoftware-com-11-back-to-basics](https://www.joelonsoftware.com/2001/12/11/back-to-basics/) | blog | 100.0% | 99.5% | 6/6 | 4/4 | 13.62ms | 14.17ms | 19518 B |
| [more-joelonsoftware-com-29-test-yourself](https://www.joelonsoftware.com/2005/12/29/test-yourself/) | blog | 100.0% | 97.6% | 6/6 | 4/4 | 9.90ms | 10.45ms | 3049 B |
| [more-joelonsoftware-com-12-strategy-letter-v](https://www.joelonsoftware.com/2002/06/12/strategy-letter-v/) | blog | 100.0% | 99.5% | 5/5 | 3/3 | 11.54ms | 12.01ms | 17680 B |
| [more-joelonsoftware-com-29-the-perils-of-javaschools-2](https://www.joelonsoftware.com/2005/12/29/the-perils-of-javaschools-2/) | blog | 100.0% | 99.5% | 5/5 | 3/3 | 11.68ms | 13.69ms | 15479 B |
| [more-joelonsoftware-com-19-two-stories](https://www.joelonsoftware.com/2000/03/19/two-stories/) | blog | 100.0% | 99.3% | 4/4 | 3/3 | 10.61ms | 11.24ms | 11057 B |
| [more-joelonsoftware-com-08-painless-bug-tracking](https://www.joelonsoftware.com/2000/11/08/painless-bug-tracking/) | blog | 100.0% | 99.4% | 7/7 | 3/3 | 12.64ms | 12.80ms | 14108 B |
| [more-rfc-editor-org-rfc-rfc8446](https://www.rfc-editor.org/rfc/rfc8446.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 61.40ms | 62.29ms | 340131 B |
| [more-rfc-editor-org-rfc-rfc9000](https://www.rfc-editor.org/rfc/rfc9000.html) | standard | 99.8% | 100.0% | 7/7 | 4/4 | 206.93ms | 211.82ms | 505711 B |
| [more-rfc-editor-org-rfc-rfc9111](https://www.rfc-editor.org/rfc/rfc9111.html) | standard | 100.0% | 100.0% | 7/7 | 4/4 | 69.64ms | 76.06ms | 125166 B |
| [more-rfc-editor-org-rfc-rfc9457](https://www.rfc-editor.org/rfc/rfc9457.html) | standard | 97.8% | 100.0% | 7/7 | 4/4 | 28.35ms | 29.77ms | 42887 B |
| [more-rfc-editor-org-rfc-rfc7519](https://www.rfc-editor.org/rfc/rfc7519.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 18.62ms | 19.36ms | 63487 B |
| [more-rfc-editor-org-rfc-rfc6455](https://www.rfc-editor.org/rfc/rfc6455.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 33.40ms | 35.53ms | 163129 B |
| [more-rfc-editor-org-rfc-rfc6902](https://www.rfc-editor.org/rfc/rfc6902.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 12.35ms | 13.32ms | 26662 B |
| [more-rfc-editor-org-rfc-rfc3339](https://www.rfc-editor.org/rfc/rfc3339.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 13.01ms | 13.50ms | 35318 B |
| [more-en-wikipedia-org-wiki-bicycle](https://en.wikipedia.org/wiki/Bicycle) | encyclopedia | 100.0% | 99.6% | 7/7 | 3/3 | 111.12ms | 112.07ms | 170791 B |
| [more-en-wikipedia-org-wiki-solar-system](https://en.wikipedia.org/wiki/Solar_System) | encyclopedia | 99.5% | 99.8% | 6/7 | 3/3 | 231.87ms | 235.26ms | 395489 B |
| [more-en-wikipedia-org-wiki-black-hole](https://en.wikipedia.org/wiki/Black_hole) | encyclopedia | 99.4% | 99.9% | 7/8 | 4/4 | 193.76ms | 198.45ms | 334341 B |
| [more-en-wikipedia-org-wiki-ada-lovelace](https://en.wikipedia.org/wiki/Ada_Lovelace) | encyclopedia | 99.9% | 99.9% | 6/7 | 3/3 | 100.57ms | 102.39ms | 159068 B |
| [more-en-wikipedia-org-wiki-fermentation](https://en.wikipedia.org/wiki/Fermentation) | encyclopedia | 99.6% | 99.8% | 7/7 | 3/3 | 64.11ms | 65.71ms | 92676 B |
| [more-en-wikipedia-org-wiki-silk-road](https://en.wikipedia.org/wiki/Silk_Road) | encyclopedia | 99.9% | 99.8% | 7/7 | 3/3 | 138.78ms | 141.74ms | 229471 B |
| [more-en-wikipedia-org-wiki-fibonacci-sequence](https://en.wikipedia.org/wiki/Fibonacci_sequence) | encyclopedia | 83.7% | 99.9% | 6/7 | 2/3 | 128.73ms | 129.97ms | 133465 B |
| [more-en-wikipedia-org-wiki-coral-reef](https://en.wikipedia.org/wiki/Coral_reef) | encyclopedia | 100.0% | 99.9% | 7/7 | 3/3 | 177.33ms | 185.67ms | 294908 B |
| [more-science-nasa-gov-mercury-facts](https://science.nasa.gov/mercury/facts/) | science | 98.4% | 100.0% | 6/6 | 3/3 | 18.80ms | 19.38ms | 6726 B |
| [more-science-nasa-gov-venus-venus-facts](https://science.nasa.gov/venus/venus-facts/) | science | 99.3% | 100.0% | 7/7 | 3/3 | 21.98ms | 23.74ms | 16129 B |
| [more-science-nasa-gov-saturn-facts](https://science.nasa.gov/saturn/facts/) | science | 98.8% | 100.0% | 7/7 | 3/3 | 19.51ms | 22.24ms | 9114 B |
| [more-science-nasa-gov-uranus-facts](https://science.nasa.gov/uranus/facts/) | science | 98.4% | 100.0% | 6/6 | 3/3 | 19.01ms | 21.43ms | 6941 B |
| [more-science-nasa-gov-neptune-neptune-facts](https://science.nasa.gov/neptune/neptune-facts/) | science | 98.7% | 100.0% | 8/8 | 4/4 | 19.81ms | 20.79ms | 9428 B |
| [more-science-nasa-gov-moon-facts](https://science.nasa.gov/moon/facts/) | science | 97.9% | 99.5% | 7/7 | 3/3 | 23.17ms | 25.45ms | 12392 B |
| [more-science-nasa-gov-sun-facts](https://science.nasa.gov/sun/facts/) | science | 85.9% | 99.7% | 7/7 | 3/3 | 25.43ms | 28.01ms | 20816 B |
| [more-science-nasa-gov-asteroids-facts](https://science.nasa.gov/solar-system/asteroids/facts/) | science | 98.1% | 100.0% | 7/7 | 3/3 | 24.39ms | 25.77ms | 16421 B |
| [more-nps-gov-planyourvisit-hiking](https://www.nps.gov/yose/planyourvisit/hiking.htm) | visitor-guide | 100.0% | 100.0% | 6/6 | 3/3 | 10.23ms | 10.86ms | 2078 B |
| [more-nps-gov-planyourvisit-hiking-088184](https://www.nps.gov/grsm/planyourvisit/hiking.htm) | visitor-guide | 100.0% | 100.0% | 7/7 | 3/3 | 12.26ms | 12.74ms | 6582 B |
| [more-nps-gov-planyourvisit-hiking-36d538](https://www.nps.gov/acad/planyourvisit/hiking.htm) | visitor-guide | 99.7% | 100.0% | 7/7 | 3/3 | 15.29ms | 16.34ms | 8046 B |
| [more-nps-gov-planyourvisit-safety](https://www.nps.gov/zion/planyourvisit/safety.htm) | visitor-guide | 100.0% | 100.0% | 5/5 | 3/3 | 14.71ms | 15.47ms | 9812 B |
| [more-nps-gov-planyourvisit-wilderness-safety](https://www.nps.gov/zion/planyourvisit/wilderness-safety.htm) | visitor-guide | 99.6% | 100.0% | 7/7 | 3/3 | 16.15ms | 17.82ms | 17586 B |
| [more-nps-gov-planyourvisit-safety-297315](https://www.nps.gov/grte/planyourvisit/safety.htm) | visitor-guide | 100.0% | 100.0% | 7/7 | 3/3 | 19.83ms | 24.45ms | 19471 B |
| [more-nps-gov-planyourvisit-winter-safety](https://www.nps.gov/yell/planyourvisit/winter-safety.htm) | visitor-guide | 94.3% | 100.0% | 6/7 | 2/3 | 11.06ms | 11.94ms | 4926 B |
| [more-nps-gov-planyourvisit-bearsafety](https://www.nps.gov/grte/planyourvisit/bearsafety.htm) | visitor-guide | 100.0% | 100.0% | 7/7 | 3/3 | 15.22ms | 15.90ms | 11551 B |
| [more-pandas-pydata-org-user-guide-groupby](https://pandas.pydata.org/docs/user_guide/groupby.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 54.53ms | 56.40ms | 88280 B |
| [more-pandas-pydata-org-user-guide-missing-data](https://pandas.pydata.org/docs/user_guide/missing_data.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 28.67ms | 29.67ms | 34722 B |
| [more-pandas-pydata-org-user-guide-reshaping](https://pandas.pydata.org/docs/user_guide/reshaping.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 31.23ms | 32.94ms | 47667 B |
| [more-pandas-pydata-org-user-guide-categorical](https://pandas.pydata.org/docs/user_guide/categorical.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 38.33ms | 43.92ms | 49270 B |
| [more-pandas-pydata-org-user-guide-timeseries](https://pandas.pydata.org/docs/user_guide/timeseries.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 86.77ms | 89.40ms | 152382 B |
| [more-pandas-pydata-org-user-guide-text](https://pandas.pydata.org/docs/user_guide/text.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 37.35ms | 41.57ms | 44533 B |
| [more-pandas-pydata-org-user-guide-duplicates](https://pandas.pydata.org/docs/user_guide/duplicates.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 15.48ms | 16.68ms | 14871 B |
| [more-pandas-pydata-org-user-guide-options](https://pandas.pydata.org/docs/user_guide/options.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 22.01ms | 23.61ms | 38365 B |
| [more-docs-djangoproject-com-db-models](https://docs.djangoproject.com/en/5.2/topics/db/models/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 45.66ms | 47.35ms | 75855 B |
| [more-docs-djangoproject-com-db-aggregation](https://docs.djangoproject.com/en/5.2/topics/db/aggregation/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 23.93ms | 24.49ms | 25666 B |
| [more-docs-djangoproject-com-http-views](https://docs.djangoproject.com/en/5.2/topics/http/views/) | docs | 100.0% | 99.7% | 8/8 | 4/4 | 14.96ms | 16.36ms | 10241 B |
| [more-docs-djangoproject-com-http-urls](https://docs.djangoproject.com/en/5.2/topics/http/urls/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 27.66ms | 31.97ms | 36634 B |
| [more-docs-djangoproject-com-topics-forms](https://docs.djangoproject.com/en/5.2/topics/forms/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 29.53ms | 32.06ms | 37874 B |
| [more-docs-djangoproject-com-auth-default](https://docs.djangoproject.com/en/5.2/topics/auth/default/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 58.47ms | 60.84ms | 100806 B |
| [more-docs-djangoproject-com-topics-cache](https://docs.djangoproject.com/en/5.2/topics/cache/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 43.31ms | 44.15ms | 63122 B |
| [more-docs-djangoproject-com-testing-overview](https://docs.djangoproject.com/en/5.2/topics/testing/overview/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 18.67ms | 20.41ms | 19281 B |
| [more-curl-se-docs-http-cookies](https://curl.se/docs/http-cookies.html) | manual | 99.3% | 100.0% | 7/7 | 3/3 | 9.87ms | 11.17ms | 6885 B |
| [more-curl-se-docs-sslcerts](https://curl.se/docs/sslcerts.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 10.40ms | 11.03ms | 6249 B |
| [more-curl-se-docs-alt-svc](https://curl.se/docs/alt-svc.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 8.25ms | 8.94ms | 1325 B |
| [more-curl-se-docs-hsts](https://curl.se/docs/hsts.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 9.50ms | 12.01ms | 1546 B |
| [more-curl-se-docs-http3](https://curl.se/docs/http3.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 11.95ms | 12.97ms | 11931 B |
| [more-curl-se-docs-url-syntax](https://curl.se/docs/url-syntax.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 14.30ms | 16.74ms | 15432 B |
| [more-curl-se-docs-ssl-ciphers](https://curl.se/docs/ssl-ciphers.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 11.68ms | 12.89ms | 11375 B |
| [more-curl-se-docs-ssl-compared](https://curl.se/docs/ssl-compared.html) | manual | 100.0% | 100.0% | 7/7 | 3/3 | 12.34ms | 14.07ms | 8231 B |
| [more-w3-org-images-decorative](https://www.w3.org/WAI/tutorials/images/decorative/) | accessibility | 100.0% | 95.4% | 8/8 | 4/4 | 11.91ms | 13.80ms | 5659 B |
| [more-w3-org-images-informative](https://www.w3.org/WAI/tutorials/images/informative/) | accessibility | 100.0% | 96.4% | 8/8 | 4/4 | 12.52ms | 13.57ms | 7736 B |
| [more-w3-org-images-functional](https://www.w3.org/WAI/tutorials/images/functional/) | accessibility | 99.6% | 95.5% | 8/8 | 4/4 | 11.67ms | 13.79ms | 5690 B |
| [more-w3-org-images-complex](https://www.w3.org/WAI/tutorials/images/complex/) | accessibility | 100.0% | 97.5% | 8/8 | 4/4 | 12.33ms | 14.04ms | 9399 B |
| [more-w3-org-forms-labels](https://www.w3.org/WAI/tutorials/forms/labels/) | accessibility | 99.3% | 97.8% | 8/8 | 4/4 | 14.19ms | 16.01ms | 10896 B |
| [more-w3-org-forms-instructions](https://www.w3.org/WAI/tutorials/forms/instructions/) | accessibility | 98.8% | 97.0% | 8/8 | 4/4 | 12.96ms | 14.25ms | 8688 B |
| [more-w3-org-tables-multi-level](https://www.w3.org/WAI/tutorials/tables/multi-level/) | accessibility | 100.0% | 95.8% | 8/8 | 4/4 | 13.41ms | 15.41ms | 6095 B |
| [more-w3-org-page-structure-headings](https://www.w3.org/WAI/tutorials/page-structure/headings/) | accessibility | 100.0% | 93.7% | 7/7 | 3/3 | 10.00ms | 10.46ms | 5654 B |
| [more-ourworldindata-org-co2-emissions](https://ourworldindata.org/co2-emissions) | data-article | 98.9% | 99.9% | 8/8 | 4/4 | 28.38ms | 29.65ms | 28709 B |
| [more-ourworldindata-org-energy-mix](https://ourworldindata.org/energy-mix) | data-article | 100.0% | 100.0% | 8/8 | 4/4 | 25.48ms | 28.91ms | 19258 B |
| [more-ourworldindata-org-plastic-pollution](https://ourworldindata.org/plastic-pollution) | data-article | 98.9% | 100.0% | 6/8 | 4/4 | 25.52ms | 28.14ms | 22689 B |
| [more-ourworldindata-org-literacy](https://ourworldindata.org/literacy) | data-article | 99.0% | 100.0% | 8/8 | 4/4 | 41.07ms | 72.70ms | 27461 B |
| [more-ourworldindata-org-economic-growth](https://ourworldindata.org/economic-growth) | data-article | 98.6% | 100.0% | 7/8 | 4/4 | 27.65ms | 38.15ms | 27011 B |
| [more-ourworldindata-org-child-mortality](https://ourworldindata.org/child-mortality) | data-article | 99.4% | 100.0% | 7/8 | 4/4 | 33.03ms | 38.53ms | 36185 B |
| [more-ourworldindata-org-vaccination](https://ourworldindata.org/vaccination) | data-article | 99.1% | 100.0% | 7/8 | 4/4 | 40.40ms | 51.32ms | 52970 B |
| [more-ourworldindata-org-renewable-energy](https://ourworldindata.org/renewable-energy) | data-article | 100.0% | 99.9% | 8/8 | 4/4 | 21.28ms | 28.92ms | 11992 B |
| [more-ssd-eff-org-module-how-to-use-signal](https://ssd.eff.org/module/how-to-use-signal) | guide | 98.0% | 100.0% | 7/7 | 3/3 | 30.39ms | 35.47ms | 36784 B |
| [more-ssd-eff-org-module-how-to-use-tor](https://ssd.eff.org/module/how-to-use-tor) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 14.69ms | 16.16ms | 13857 B |
| [more-ssd-eff-org-module-how-encrypt-your-windows-device](https://ssd.eff.org/module/how-encrypt-your-windows-device) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 14.03ms | 14.85ms | 13313 B |
| [more-ssd-eff-org-module-how-enable-two-factor-authentication](https://ssd.eff.org/module/how-enable-two-factor-authentication) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 13.57ms | 20.34ms | 14594 B |
| [more-ssd-eff-org-module-what-should-i-know-about-encryption](https://ssd.eff.org/module/what-should-i-know-about-encryption) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 21.24ms | 24.48ms | 17913 B |
| [more-ssd-eff-org-module-choosing-vpn-thats-right-you](https://ssd.eff.org/module/choosing-vpn-thats-right-you) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 23.13ms | 33.21ms | 18122 B |
| [more-ssd-eff-org-module-keeping-your-data-safe](https://ssd.eff.org/module/keeping-your-data-safe) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 25.32ms | 30.54ms | 9834 B |
| [more-ssd-eff-org-module-protecting-yourself-social-networks](https://ssd.eff.org/module/protecting-yourself-social-networks) | guide | 99.9% | 100.0% | 7/7 | 3/3 | 26.42ms | 32.13ms | 10121 B |
| [more-paulgraham-com-hs](https://www.paulgraham.com/hs.html) | essay | 99.8% | 100.0% | 3/4 | 3/3 | 23.76ms | 28.79ms | 28028 B |
| [more-paulgraham-com-greatwork](https://www.paulgraham.com/greatwork.html) | essay | 100.0% | 100.0% | 3/3 | 3/3 | 32.37ms | 40.91ms | 69852 B |
| [more-paulgraham-com-startupideas](https://www.paulgraham.com/startupideas.html) | essay | 99.9% | 100.0% | 3/4 | 3/3 | 29.34ms | 42.84ms | 42432 B |
| [more-paulgraham-com-procrastination](https://www.paulgraham.com/procrastination.html) | essay | 99.2% | 100.0% | 3/4 | 3/3 | 15.02ms | 20.64ms | 10244 B |
| [more-paulgraham-com-nerds](https://www.paulgraham.com/nerds.html) | essay | 99.6% | 100.0% | 3/4 | 3/3 | 18.06ms | 20.86ms | 32014 B |
| [more-paulgraham-com-wealth](https://www.paulgraham.com/wealth.html) | essay | 99.8% | 100.0% | 3/4 | 3/3 | 24.39ms | 24.79ms | 51543 B |
| [more-paulgraham-com-cities](https://www.paulgraham.com/cities.html) | essay | 99.8% | 100.0% | 3/4 | 3/3 | 18.47ms | 44.02ms | 20764 B |
| [more-paulgraham-com-good](https://www.paulgraham.com/good.html) | essay | 99.9% | 100.0% | 4/4 | 3/3 | 24.00ms | 36.27ms | 17092 B |
| [more-martinfowler-com-articles-feature-toggles](https://martinfowler.com/articles/feature-toggles.html) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 41.08ms | 53.32ms | 50929 B |
| [more-martinfowler-com-articles-injection](https://martinfowler.com/articles/injection.html) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 22.93ms | 31.90ms | 42508 B |
| [more-martinfowler-com-bliki-stranglerfigapplication](https://martinfowler.com/bliki/StranglerFigApplication.html) | essay | 100.0% | 91.0% | 6/6 | 3/3 | 9.81ms | 10.69ms | 7438 B |
| [more-martinfowler-com-bliki-monolithfirst](https://martinfowler.com/bliki/MonolithFirst.html) | essay | 100.0% | 90.3% | 7/7 | 3/3 | 9.59ms | 10.67ms | 7940 B |
| [more-martinfowler-com-bliki-twohardthings](https://martinfowler.com/bliki/TwoHardThings.html) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 8.14ms | 8.61ms | 2048 B |
| [more-martinfowler-com-bliki-cqrs](https://martinfowler.com/bliki/CQRS.html) | essay | 99.3% | 100.0% | 7/7 | 3/3 | 9.52ms | 10.22ms | 8639 B |
| [more-martinfowler-com-articles-practical-test-pyramid](https://martinfowler.com/articles/practical-test-pyramid.html) | essay | 96.7% | 100.0% | 8/8 | 4/4 | 32.17ms | 34.58ms | 82253 B |
| [more-martinfowler-com-bliki-boundedcontext](https://martinfowler.com/bliki/BoundedContext.html) | essay | 99.1% | 99.4% | 7/7 | 3/3 | 8.73ms | 9.50ms | 5663 B |
| [more-letsencrypt-org-docs-rate-limits](https://letsencrypt.org/docs/rate-limits/) | docs | 100.0% | 100.0% | 9/9 | 5/5 | 13.75ms | 14.60ms | 15102 B |
| [more-letsencrypt-org-docs-staging-environment](https://letsencrypt.org/docs/staging-environment/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 11.32ms | 12.69ms | 9308 B |
| [more-letsencrypt-org-docs-faq](https://letsencrypt.org/docs/faq/) | docs | 100.0% | 100.0% | 7/7 | 3/3 | 9.94ms | 10.54ms | 8418 B |
| [more-letsencrypt-org-docs-integration-guide](https://letsencrypt.org/docs/integration-guide/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 10.66ms | 11.76ms | 13185 B |
| [more-letsencrypt-org-docs-account-id](https://letsencrypt.org/docs/account-id/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 7.23ms | 8.39ms | 1377 B |
| [more-letsencrypt-org-docs-ipv6-support](https://letsencrypt.org/docs/ipv6-support/) | docs | 100.0% | 100.0% | 7/7 | 3/3 | 8.18ms | 9.15ms | 2820 B |
| [more-letsencrypt-org-docs-revoking](https://letsencrypt.org/docs/revoking/) | docs | 100.0% | 100.0% | 6/6 | 4/4 | 9.79ms | 10.64ms | 6944 B |
| [more-letsencrypt-org-docs-ct-logs](https://letsencrypt.org/docs/ct-logs/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 9.85ms | 11.82ms | 5983 B |
| [more-oceanservice-noaa-gov-facts-tides](https://oceanservice.noaa.gov/facts/tides.html) | science | 96.9% | 100.0% | 5/6 | 3/3 | 9.05ms | 9.37ms | 1238 B |
| [more-oceanservice-noaa-gov-facts-current](https://oceanservice.noaa.gov/facts/current.html) | science | 94.2% | 100.0% | 5/6 | 2/3 | 9.41ms | 9.88ms | 1957 B |
| [more-oceanservice-noaa-gov-facts-coral](https://oceanservice.noaa.gov/facts/coral.html) | science | 100.0% | 100.0% | 4/4 | 3/3 | 10.00ms | 10.58ms | 2947 B |
| [more-oceanservice-noaa-gov-facts-acidification](https://oceanservice.noaa.gov/facts/acidification.html) | science | 97.5% | 100.0% | 4/5 | 3/3 | 9.34ms | 10.49ms | 1702 B |
| [more-oceanservice-noaa-gov-facts-tsunami](https://oceanservice.noaa.gov/facts/tsunami.html) | science | 100.0% | 100.0% | 4/4 | 2/2 | 9.62ms | 10.17ms | 1155 B |
| [more-oceanservice-noaa-gov-facts-estuary](https://oceanservice.noaa.gov/facts/estuary.html) | science | 96.3% | 91.0% | 5/7 | 2/3 | 10.18ms | 11.19ms | 3386 B |
| [more-oceanservice-noaa-gov-facts-mangroves](https://oceanservice.noaa.gov/facts/mangroves.html) | science | 100.0% | 100.0% | 5/5 | 3/3 | 9.09ms | 9.23ms | 1283 B |
| [more-oceanservice-noaa-gov-facts-kelp](https://oceanservice.noaa.gov/facts/kelp.html) | science | 97.5% | 100.0% | 5/6 | 3/3 | 9.15ms | 10.23ms | 2069 B |
| [more-medlineplus-gov-article-000076](https://medlineplus.gov/ency/article/000076.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 13.01ms | 13.35ms | 10244 B |
| [more-medlineplus-gov-article-000468](https://medlineplus.gov/ency/article/000468.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 15.70ms | 16.39ms | 17003 B |
| [more-medlineplus-gov-article-000279](https://medlineplus.gov/ency/article/000279.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 12.94ms | 14.20ms | 10737 B |
| [more-medlineplus-gov-article-000313](https://medlineplus.gov/ency/article/000313.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 19.14ms | 19.49ms | 26112 B |
| [more-medlineplus-gov-article-000545](https://medlineplus.gov/ency/article/000545.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.91ms | 11.14ms | 3584 B |
| [more-medlineplus-gov-article-000639](https://medlineplus.gov/ency/article/000639.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 11.58ms | 12.08ms | 6727 B |
| [more-medlineplus-gov-article-000158](https://medlineplus.gov/ency/article/000158.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 15.23ms | 16.05ms | 14910 B |
| [more-medlineplus-gov-article-000285](https://medlineplus.gov/ency/article/000285.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 11.37ms | 12.64ms | 6548 B |
| [more-who-int-detail-malaria](https://www.who.int/news-room/fact-sheets/detail/malaria) | health | 100.0% | 99.6% | 7/7 | 3/3 | 15.36ms | 16.57ms | 14208 B |
| [more-who-int-detail-tuberculosis](https://www.who.int/news-room/fact-sheets/detail/tuberculosis) | health | 100.0% | 97.8% | 6/6 | 3/3 | 13.84ms | 14.32ms | 11187 B |
| [more-who-int-detail-diabetes](https://www.who.int/news-room/fact-sheets/detail/diabetes) | health | 100.0% | 99.3% | 7/7 | 3/3 | 14.48ms | 14.78ms | 8420 B |
| [more-who-int-detail-asthma](https://www.who.int/news-room/fact-sheets/detail/asthma) | health | 100.0% | 99.2% | 7/7 | 3/3 | 13.60ms | 14.71ms | 7892 B |
| [more-who-int-detail-hypertension](https://www.who.int/news-room/fact-sheets/detail/hypertension) | health | 100.0% | 99.4% | 7/7 | 3/3 | 14.28ms | 14.88ms | 9559 B |
| [more-who-int-detail-dengue-and-severe-dengue](https://www.who.int/news-room/fact-sheets/detail/dengue-and-severe-dengue) | health | 100.0% | 98.6% | 7/7 | 3/3 | 15.60ms | 16.56ms | 12302 B |
| [more-who-int-detail-measles](https://www.who.int/news-room/fact-sheets/detail/measles) | health | 100.0% | 99.4% | 7/7 | 3/3 | 13.77ms | 14.34ms | 10104 B |
| [more-who-int-detail-physical-activity](https://www.who.int/news-room/fact-sheets/detail/physical-activity) | health | 100.0% | 99.7% | 7/7 | 3/3 | 15.55ms | 16.74ms | 10787 B |
| [more-un-org-science-causes-effects-climate-change](https://www.un.org/en/climatechange/science/causes-effects-climate-change) | explainer | 95.9% | 100.0% | 4/7 | 2/3 | 23.24ms | 23.63ms | 8627 B |
| [more-un-org-climatechange-what-is-renewable-energy](https://www.un.org/en/climatechange/what-is-renewable-energy) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 12.72ms | 14.12ms | 8761 B |
| [more-un-org-raising-ambition-renewable-energy](https://www.un.org/en/climatechange/raising-ambition/renewable-energy) | explainer | 86.1% | 100.0% | 5/7 | 2/3 | 24.23ms | 24.69ms | 12657 B |
| [more-un-org-climate-issues-greenwashing](https://www.un.org/en/climatechange/science/climate-issues/greenwashing) | explainer | 79.7% | 100.0% | 5/7 | 2/3 | 24.22ms | 25.12ms | 10387 B |
| [more-un-org-climate-issues-food](https://www.un.org/en/climatechange/science/climate-issues/food) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 25.47ms | 27.65ms | 12298 B |
| [more-un-org-climatechange-climate-adaptation](https://www.un.org/en/climatechange/climate-adaptation) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 23.27ms | 26.01ms | 11119 B |
| [more-un-org-climatechange-net-zero-coalition](https://www.un.org/en/climatechange/net-zero-coalition) | explainer | 96.1% | 100.0% | 5/7 | 2/3 | 27.34ms | 27.70ms | 8764 B |
| [more-un-org-climatechange-paris-agreement](https://www.un.org/en/climatechange/paris-agreement) | explainer | 100.0% | 98.1% | 7/7 | 3/3 | 23.20ms | 24.50ms | 6825 B |
| [more-computerhistory-org-timeline-1940](https://www.computerhistory.org/timeline/1940/) | history | 98.8% | 83.3% | 4/4 | 1/1 | 15.05ms | 15.58ms | 1047 B |
| [more-computerhistory-org-timeline-1946](https://www.computerhistory.org/timeline/1946/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 12.43ms | 12.87ms | 4474 B |
| [more-computerhistory-org-timeline-1951](https://www.computerhistory.org/timeline/1951/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 14.63ms | 15.19ms | 7652 B |
| [more-computerhistory-org-timeline-1956](https://www.computerhistory.org/timeline/1956/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 14.28ms | 15.04ms | 5641 B |
| [more-computerhistory-org-timeline-1964](https://www.computerhistory.org/timeline/1964/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 17.29ms | 18.30ms | 12589 B |
| [more-computerhistory-org-timeline-1971](https://www.computerhistory.org/timeline/1971/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 16.11ms | 16.47ms | 10578 B |
| [more-computerhistory-org-timeline-1984](https://www.computerhistory.org/timeline/1984/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 17.43ms | 17.79ms | 12694 B |
| [more-computerhistory-org-timeline-1991](https://www.computerhistory.org/timeline/1991/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 14.73ms | 15.25ms | 7430 B |
| [more-nhm-ac-uk-discover-what-is-biodiversity](https://www.nhm.ac.uk/discover/what-is-biodiversity.html) | science | 99.8% | 94.7% | 7/7 | 3/3 | 19.02ms | 19.68ms | 12113 B |
| [more-nhm-ac-uk-discover-insect-pollination](https://www.nhm.ac.uk/discover/insect-pollination.html) | science | 99.9% | 96.4% | 7/7 | 3/3 | 19.47ms | 20.41ms | 15572 B |
| [more-nhm-ac-uk-discover-how-are-fossils-formed](https://www.nhm.ac.uk/discover/how-are-fossils-formed.html) | science | 99.6% | 91.1% | 7/7 | 3/3 | 18.61ms | 19.12ms | 9012 B |
| [more-nhm-ac-uk-discover-meet-the-monsters-of-the-jurassic-seas](https://www.nhm.ac.uk/discover/meet-the-monsters-of-the-jurassic-seas.html) | science | 99.9% | 97.6% | 7/7 | 3/3 | 17.47ms | 18.02ms | 8227 B |
| [more-nhm-ac-uk-discover-what-is-natural-selection](https://www.nhm.ac.uk/discover/what-is-natural-selection.html) | science | 98.1% | 90.3% | 6/7 | 3/3 | 21.08ms | 21.55ms | 15000 B |
| [more-nhm-ac-uk-discover-convergent-evolution](https://www.nhm.ac.uk/discover/convergent-evolution.html) | science | 98.5% | 99.5% | 7/7 | 3/3 | 27.61ms | 29.75ms | 24103 B |
| [more-nhm-ac-uk-discover-dinosaur-extinction](https://www.nhm.ac.uk/discover/dinosaur-extinction.html) | science | 96.7% | 89.5% | 6/7 | 3/3 | 14.99ms | 15.65ms | 2900 B |
| [more-nhm-ac-uk-discover-what-is-climate-change-why-does-it-matter](https://www.nhm.ac.uk/discover/what-is-climate-change-why-does-it-matter.html) | science | 97.9% | 97.8% | 7/7 | 3/3 | 26.53ms | 28.95ms | 23880 B |
| [more-docs-julialang-org-manual-variables](https://docs.julialang.org/en/v1/manual/variables/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 12.01ms | 12.41ms | 10201 B |
| [more-docs-julialang-org-manual-integers-and-floating-point-numbers](https://docs.julialang.org/en/v1/manual/integers-and-floating-point-numbers/) | manual | 100.0% | 100.0% | 9/9 | 5/5 | 20.69ms | 21.16ms | 29103 B |
| [more-docs-julialang-org-manual-strings](https://docs.julialang.org/en/v1/manual/strings/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 26.39ms | 26.65ms | 48647 B |
| [more-docs-julialang-org-manual-functions](https://docs.julialang.org/en/v1/manual/functions/) | manual | 100.0% | 100.0% | 9/9 | 5/5 | 25.00ms | 25.64ms | 43241 B |
| [more-docs-julialang-org-manual-control-flow](https://docs.julialang.org/en/v1/manual/control-flow/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 21.38ms | 22.75ms | 32499 B |
| [more-docs-julialang-org-manual-types](https://docs.julialang.org/en/v1/manual/types/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 36.35ms | 39.53ms | 74679 B |
| [more-docs-julialang-org-manual-methods](https://docs.julialang.org/en/v1/manual/methods/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 24.66ms | 25.04ms | 47728 B |
| [more-docs-julialang-org-manual-performance-tips](https://docs.julialang.org/en/v1/manual/performance-tips/) | manual | 98.1% | 100.0% | 8/8 | 4/4 | 39.07ms | 39.54ms | 80300 B |
| [more-elixir-hexdocs-pm-enum](https://elixir.hexdocs.pm/Enum.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 91.98ms | 93.30ms | 91153 B |
| [more-elixir-hexdocs-pm-map](https://elixir.hexdocs.pm/Map.html) | api | 100.0% | 100.0% | 7/7 | 4/4 | 40.00ms | 41.65ms | 31897 B |
| [more-elixir-hexdocs-pm-string](https://elixir.hexdocs.pm/String.html) | api | 100.0% | 100.0% | 7/7 | 4/4 | 62.14ms | 63.63ms | 68062 B |
| [more-elixir-hexdocs-pm-list](https://elixir.hexdocs.pm/List.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 40.77ms | 41.92ms | 32209 B |
| [more-elixir-hexdocs-pm-genserver](https://elixir.hexdocs.pm/GenServer.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 39.40ms | 40.46ms | 52327 B |
| [more-elixir-hexdocs-pm-task](https://elixir.hexdocs.pm/Task.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 34.09ms | 35.91ms | 43517 B |
| [more-elixir-hexdocs-pm-introduction](https://elixir.hexdocs.pm/introduction.html) | api | 100.0% | 99.5% | 7/7 | 4/4 | 8.61ms | 9.38ms | 2694 B |
| [more-elixir-hexdocs-pm-pattern-matching](https://elixir.hexdocs.pm/pattern-matching.html) | api | 100.0% | 99.8% | 7/7 | 4/4 | 10.62ms | 11.09ms | 5853 B |
| [more-ffmpeg-org-ffmpeg](https://ffmpeg.org/ffmpeg.html) | manual | 99.1% | 100.0% | 6/8 | 4/4 | 101.08ms | 102.16ms | 148142 B |
| [more-ffmpeg-org-ffmpeg-formats](https://ffmpeg.org/ffmpeg-formats.html) | manual | 97.2% | 100.0% | 6/8 | 4/4 | 146.69ms | 151.56ms | 196445 B |
| [more-ffmpeg-org-ffmpeg-codecs](https://ffmpeg.org/ffmpeg-codecs.html) | manual | 96.7% | 100.0% | 6/8 | 4/4 | 161.71ms | 164.36ms | 177082 B |
| [more-ffmpeg-org-ffmpeg-protocols](https://ffmpeg.org/ffmpeg-protocols.html) | manual | 98.1% | 100.0% | 6/8 | 4/4 | 63.94ms | 64.85ms | 78009 B |
| [more-ffmpeg-org-ffmpeg-utils](https://ffmpeg.org/ffmpeg-utils.html) | manual | 98.2% | 100.0% | 6/8 | 4/4 | 35.89ms | 36.64ms | 24138 B |
| [more-ffmpeg-org-ffplay](https://ffmpeg.org/ffplay.html) | manual | 98.7% | 100.0% | 6/8 | 4/4 | 26.27ms | 26.73ms | 23634 B |
| [more-ffmpeg-org-ffprobe](https://ffmpeg.org/ffprobe.html) | manual | 98.8% | 100.0% | 6/8 | 4/4 | 33.33ms | 34.54ms | 34178 B |
| [more-ffmpeg-org-faq](https://ffmpeg.org/faq.html) | manual | 86.4% | 100.0% | 6/8 | 4/4 | 26.08ms | 27.32ms | 24930 B |
| [more-angular-dev-guide-components](https://angular.dev/guide/components) | docs | 99.3% | 100.0% | 8/8 | 4/4 | 17.53ms | 18.98ms | 4936 B |
| [more-angular-dev-components-inputs](https://angular.dev/guide/components/inputs) | docs | 99.8% | 100.0% | 7/8 | 4/4 | 21.32ms | 23.73ms | 14277 B |
| [more-angular-dev-guide-templates](https://angular.dev/guide/templates) | docs | 99.1% | 100.0% | 8/8 | 4/4 | 12.56ms | 13.03ms | 4212 B |
| [more-angular-dev-guide-di](https://angular.dev/guide/di) | docs | 99.4% | 100.0% | 8/8 | 4/4 | 17.24ms | 18.02ms | 7016 B |
| [more-angular-dev-guide-routing](https://angular.dev/guide/routing) | docs | 98.6% | 100.0% | 7/7 | 3/3 | 11.87ms | 13.22ms | 1853 B |
| [more-angular-dev-guide-forms](https://angular.dev/guide/forms) | docs | 99.8% | 100.0% | 8/9 | 5/5 | 29.05ms | 30.09ms | 17987 B |
| [more-angular-dev-forms-typed-forms](https://angular.dev/guide/forms/typed-forms) | docs | 99.6% | 100.0% | 7/8 | 4/4 | 18.44ms | 18.93ms | 8880 B |
| [more-angular-dev-guide-http](https://angular.dev/guide/http) | docs | 90.8% | 100.0% | 5/6 | 2/2 | 9.95ms | 12.29ms | 935 B |
| [more-react-dev-learn-describing-the-ui](https://react.dev/learn/describing-the-ui) | tutorial | 99.3% | 98.6% | 7/8 | 4/4 | 26.15ms | 27.08ms | 14229 B |
| [more-react-dev-learn-writing-markup-with-jsx](https://react.dev/learn/writing-markup-with-jsx) | tutorial | 99.7% | 99.5% | 8/8 | 4/4 | 20.89ms | 22.47ms | 10568 B |
| [more-react-dev-learn-conditional-rendering](https://react.dev/learn/conditional-rendering) | tutorial | 99.5% | 99.0% | 7/8 | 4/4 | 26.93ms | 27.76ms | 13449 B |
| [more-react-dev-learn-rendering-lists](https://react.dev/learn/rendering-lists) | tutorial | 99.8% | 99.5% | 8/8 | 4/4 | 25.11ms | 26.78ms | 12288 B |
| [more-react-dev-learn-responding-to-events](https://react.dev/learn/responding-to-events) | tutorial | 99.5% | 99.0% | 8/9 | 5/5 | 30.71ms | 32.85ms | 17321 B |
| [more-react-dev-learn-updating-objects-in-state](https://react.dev/learn/updating-objects-in-state) | tutorial | 99.7% | 99.4% | 8/8 | 4/4 | 32.84ms | 35.29ms | 21324 B |
| [more-react-dev-learn-synchronizing-with-effects](https://react.dev/learn/synchronizing-with-effects) | tutorial | 99.9% | 99.7% | 8/8 | 4/4 | 49.47ms | 52.20ms | 44113 B |
| [more-react-dev-learn-you-might-not-need-an-effect](https://react.dev/learn/you-might-not-need-an-effect) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 42.14ms | 44.33ms | 40449 B |
| [more-nmap-org-book-man-examples](https://nmap.org/book/man-examples.html) | manual | 98.0% | 100.0% | 5/6 | 3/3 | 7.30ms | 8.28ms | 2452 B |
| [more-nmap-org-book-man-port-scanning-basics](https://nmap.org/book/man-port-scanning-basics.html) | manual | 98.1% | 100.0% | 5/6 | 3/3 | 8.87ms | 11.13ms | 4059 B |
| [more-nmap-org-book-man-host-discovery](https://nmap.org/book/man-host-discovery.html) | manual | 99.7% | 100.0% | 6/6 | 3/3 | 21.03ms | 23.28ms | 23315 B |
| [more-nmap-org-book-man-port-scanning-techniques](https://nmap.org/book/man-port-scanning-techniques.html) | manual | 99.7% | 100.0% | 6/6 | 3/3 | 20.17ms | 20.49ms | 24780 B |
| [more-nmap-org-book-man-version-detection](https://nmap.org/book/man-version-detection.html) | manual | 98.8% | 100.0% | 5/6 | 3/3 | 9.82ms | 10.96ms | 6640 B |
| [more-nmap-org-book-man-os-detection](https://nmap.org/book/man-os-detection.html) | manual | 98.7% | 100.0% | 6/6 | 3/3 | 9.75ms | 10.39ms | 4945 B |
| [more-nmap-org-book-man-nse](https://nmap.org/book/man-nse.html) | manual | 99.1% | 100.0% | 6/6 | 3/3 | 12.08ms | 12.60ms | 9648 B |
| [more-nmap-org-book-man-performance](https://nmap.org/book/man-performance.html) | manual | 99.7% | 100.0% | 6/6 | 3/3 | 19.02ms | 19.34ms | 22363 B |
| [more-numpy-org-user-basics-broadcasting](https://numpy.org/doc/stable/user/basics.broadcasting.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 15.47ms | 17.65ms | 12248 B |
| [more-numpy-org-user-basics-indexing](https://numpy.org/doc/stable/user/basics.indexing.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 27.48ms | 29.17ms | 34892 B |
| [more-numpy-org-user-basics-copies](https://numpy.org/doc/stable/user/basics.copies.html) | api | 100.0% | 100.0% | 8/8 | 4/4 | 11.02ms | 11.76ms | 5599 B |
| [more-numpy-org-user-basics-types](https://numpy.org/doc/stable/user/basics.types.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 20.02ms | 20.81ms | 21198 B |
| [more-docs-scipy-org-tutorial-integrate](https://docs.scipy.org/doc/scipy/tutorial/integrate.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 25.31ms | 26.47ms | 34836 B |
| [more-docs-scipy-org-tutorial-optimize](https://docs.scipy.org/doc/scipy/tutorial/optimize.html) | tutorial | 100.0% | 100.0% | 9/9 | 5/5 | 57.73ms | 58.46ms | 94642 B |
| [more-docs-scipy-org-tutorial-interpolate](https://docs.scipy.org/doc/scipy/tutorial/interpolate.html) | tutorial | 100.0% | 100.0% | 6/6 | 3/3 | 15.43ms | 16.09ms | 14347 B |
| [more-docs-scipy-org-tutorial-fft](https://docs.scipy.org/doc/scipy/tutorial/fft.html) | tutorial | 99.1% | 100.0% | 8/8 | 4/4 | 31.46ms | 33.24ms | 45667 B |
| [more-docs-github-com-workflows-and-actions-workflow-syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax) | configuration | 99.6% | 100.0% | 7/9 | 5/5 | 110.01ms | 114.63ms | 173409 B |
| [more-docs-github-com-security-secure-use](https://docs.github.com/en/actions/reference/security/secure-use) | configuration | 100.0% | 100.0% | 7/8 | 4/4 | 31.05ms | 31.91ms | 38775 B |
| [more-docs-github-com-choose-what-workflows-do-run-job-variations](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/run-job-variations) | configuration | 90.8% | 100.0% | 7/8 | 4/4 | 23.17ms | 23.27ms | 9237 B |
| [more-docs-github-com-reuse-automations-reuse-workflows](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows) | configuration | 89.9% | 100.0% | 7/8 | 4/4 | 26.58ms | 28.53ms | 18134 B |
| [more-learn-microsoft-com-operators-null-coalescing-operator](https://learn.microsoft.com/en-us/dotnet/csharp/language-reference/operators/null-coalescing-operator) | docs | 100.0% | 98.9% | 8/8 | 4/4 | 12.70ms | 14.13ms | 6144 B |
| [more-learn-microsoft-com-operators-patterns](https://learn.microsoft.com/en-us/dotnet/csharp/language-reference/operators/patterns) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 23.65ms | 24.04ms | 43237 B |
| [more-learn-microsoft-com-asynchronous-programming-async-return-types](https://learn.microsoft.com/en-us/dotnet/csharp/asynchronous-programming/async-return-types) | docs | 100.0% | 99.6% | 8/8 | 4/4 | 14.87ms | 15.31ms | 17927 B |
| [more-learn-microsoft-com-exceptions-exception-handling](https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/exceptions/exception-handling) | docs | 100.0% | 99.3% | 8/8 | 4/4 | 12.15ms | 13.06ms | 7976 B |
| [more-developer-chrome-com-devtools-network](https://developer.chrome.com/docs/devtools/network/) | docs | 98.5% | 100.0% | 7/7 | 3/3 | 20.88ms | 21.91ms | 14358 B |
| [more-developer-chrome-com-devtools-performance](https://developer.chrome.com/docs/devtools/performance/) | docs | 99.0% | 100.0% | 7/7 | 3/3 | 19.67ms | 20.04ms | 14150 B |
| [more-developer-chrome-com-devtools-console](https://developer.chrome.com/docs/devtools/console/) | docs | 97.9% | 100.0% | 8/8 | 4/4 | 18.64ms | 19.93ms | 4935 B |
| [more-developer-chrome-com-devtools-memory-problems](https://developer.chrome.com/docs/devtools/memory-problems/) | docs | 99.1% | 100.0% | 8/8 | 4/4 | 22.40ms | 23.59ms | 13092 B |
| [more-jvns-ca-01-a-dns-resolver-in-80-lines-of-go](https://jvns.ca/blog/2022/02/01/a-dns-resolver-in-80-lines-of-go/) | blog | 99.7% | 100.0% | 8/8 | 4/4 | 12.09ms | 13.29ms | 17402 B |
| [more-jvns-ca-05-some-blogging-myths](https://jvns.ca/blog/2023/06/05/some-blogging-myths/) | blog | 97.3% | 100.0% | 7/7 | 3/3 | 9.90ms | 10.39ms | 11146 B |
| [more-jvns-ca-01-learning-skills-you-can-practice](https://jvns.ca/blog/2018/09/01/learning-skills-you-can-practice/) | blog | 99.4% | 100.0% | 7/7 | 3/3 | 9.30ms | 10.11ms | 9295 B |
| [more-jvns-ca-10-how-does-gdb-work](https://jvns.ca/blog/2016/08/10/how-does-gdb-work/) | blog | 99.6% | 100.0% | 8/8 | 4/4 | 10.02ms | 10.19ms | 9969 B |
| [more-eli-thegreenplace-net-2020-implementing-raft-part-1-elections](https://eli.thegreenplace.net/2020/implementing-raft-part-1-elections/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 17.71ms | 18.58ms | 28936 B |
| [more-eli-thegreenplace-net-2020-implementing-raft-part-2-commands-and-log-replication](https://eli.thegreenplace.net/2020/implementing-raft-part-2-commands-and-log-replication/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 19.67ms | 20.21ms | 27620 B |
| [more-eli-thegreenplace-net-2020-implementing-raft-part-3-persistence-and-optimizations](https://eli.thegreenplace.net/2020/implementing-raft-part-3-persistence-and-optimizations/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 15.19ms | 15.55ms | 19719 B |
| [more-eli-thegreenplace-net-2023-preview-ranging-over-functions-in-go](https://eli.thegreenplace.net/2023/preview-ranging-over-functions-in-go/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 16.71ms | 17.78ms | 18809 B |
| [more-brendangregg-com-usemethod](https://www.brendangregg.com/usemethod.html) | guide | 100.0% | 100.0% | 7/7 | 4/4 | 18.59ms | 18.94ms | 26572 B |
| [more-brendangregg-com-methodology](https://www.brendangregg.com/methodology.html) | guide | 100.0% | 100.0% | 6/6 | 3/3 | 14.64ms | 14.89ms | 12285 B |
| [more-brendangregg-com-flamegraphs](https://www.brendangregg.com/flamegraphs.html) | guide | 100.0% | 100.0% | 6/6 | 3/3 | 26.63ms | 27.67ms | 45080 B |
| [more-brendangregg-com-offcpuanalysis](https://www.brendangregg.com/offcpuanalysis.html) | guide | 100.0% | 100.0% | 7/7 | 4/4 | 22.26ms | 22.73ms | 41630 B |
| [more-developer-android-com-activities-activity-lifecycle](https://developer.android.com/guide/components/activities/activity-lifecycle) | docs | 99.7% | 100.0% | 8/8 | 4/4 | 32.50ms | 35.53ms | 36153 B |
| [more-developer-android-com-background-work-services](https://developer.android.com/develop/background-work/services) | docs | 99.8% | 100.0% | 7/8 | 4/4 | 32.76ms | 34.22ms | 34848 B |
| [more-developer-android-com-background-tasks-broadcasts](https://developer.android.com/develop/background-work/background-tasks/broadcasts) | docs | 99.7% | 100.0% | 7/8 | 4/4 | 32.69ms | 34.04ms | 36302 B |
| [more-developer-android-com-manifest-manifest-intro](https://developer.android.com/guide/topics/manifest/manifest-intro) | docs | 99.5% | 100.0% | 8/9 | 5/5 | 28.85ms | 29.47ms | 18379 B |
| [more-redis-io-data-types-strings](https://redis.io/docs/latest/develop/data-types/strings/) | database | 99.0% | 97.5% | 8/8 | 4/4 | 179.72ms | 182.67ms | 104846 B |
| [more-redis-io-data-types-hashes](https://redis.io/docs/latest/develop/data-types/hashes/) | database | 99.4% | 97.7% | 9/9 | 5/5 | 449.91ms | 452.31ms | 308717 B |
| [more-redis-io-data-types-lists](https://redis.io/docs/latest/develop/data-types/lists/) | database | 99.2% | 97.2% | 8/8 | 4/4 | 867.62ms | 879.58ms | 538204 B |
| [more-redis-io-data-types-sets](https://redis.io/docs/latest/develop/data-types/sets/) | database | 99.0% | 96.8% | 8/8 | 4/4 | 414.42ms | 421.12ms | 265684 B |
| [more-nhs-uk-conditions-asthma](https://www.nhs.uk/conditions/asthma/) | health | 98.3% | 100.0% | 7/7 | 3/3 | 12.35ms | 13.28ms | 10467 B |
| [more-nhs-uk-conditions-type-2-diabetes](https://www.nhs.uk/conditions/type-2-diabetes/) | health | 100.0% | 100.0% | 3/3 | 1/1 | 7.87ms | 8.20ms | 652 B |
| [more-nhs-uk-conditions-high-blood-pressure](https://www.nhs.uk/conditions/high-blood-pressure/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.96ms | 11.62ms | 6904 B |
| [more-nhs-uk-conditions-dehydration](https://www.nhs.uk/conditions/dehydration/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.29ms | 11.15ms | 5225 B |
| [more-epa-gov-recycle-recycling-basics-and-benefits](https://www.epa.gov/recycle/recycling-basics-and-benefits) | science | 96.3% | 100.0% | 7/7 | 3/3 | 11.71ms | 12.11ms | 10256 B |
| [more-epa-gov-acidrain-effects-acid-rain](https://www.epa.gov/acidrain/effects-acid-rain) | science | 95.5% | 100.0% | 7/7 | 3/3 | 11.12ms | 11.67ms | 7081 B |
| [more-epa-gov-acidrain-what-acid-rain](https://www.epa.gov/acidrain/what-acid-rain) | science | 96.2% | 100.0% | 7/7 | 3/3 | 10.06ms | 10.85ms | 4991 B |
| [more-epa-gov-acidrain-acid-rain-program](https://www.epa.gov/acidrain/acid-rain-program) | science | 96.6% | 100.0% | 7/7 | 3/3 | 13.02ms | 14.18ms | 11097 B |
| [more-allrecipes-com-20144-banana-banana-bread](https://www.allrecipes.com/recipe/20144/banana-banana-bread/) | recipe | 90.8% | 99.6% | 7/7 | 3/3 | 34.38ms | 35.02ms | 9457 B |
| [more-allrecipes-com-10549-best-brownies](https://www.allrecipes.com/recipe/10549/best-brownies/) | recipe | 81.4% | 99.0% | 7/7 | 3/3 | 32.73ms | 33.66ms | 3512 B |
| [more-allrecipes-com-10813-best-chocolate-chip-cookies](https://www.allrecipes.com/recipe/10813/best-chocolate-chip-cookies/) | recipe | 91.6% | 99.6% | 6/7 | 3/3 | 36.44ms | 39.06ms | 10899 B |
| [more-allrecipes-com-16354-easy-meatloaf](https://www.allrecipes.com/recipe/16354/easy-meatloaf/) | recipe | 87.3% | 99.4% | 7/7 | 3/3 | 34.29ms | 35.79ms | 6811 B |
| [more-weather-gov-safety-lightning](https://www.weather.gov/safety/lightning) | safety | 94.8% | 100.0% | 4/4 | 3/3 | 11.73ms | 12.29ms | 3024 B |
| [more-weather-gov-safety-tornado](https://www.weather.gov/safety/tornado) | safety | 94.9% | 100.0% | 3/4 | 3/3 | 10.39ms | 12.53ms | 1284 B |
| [more-weather-gov-safety-flood](https://www.weather.gov/safety/flood) | safety | 93.9% | 100.0% | 3/3 | 2/2 | 10.93ms | 11.91ms | 1596 B |
| [more-weather-gov-safety-heat](https://www.weather.gov/safety/heat) | safety | 95.9% | 100.0% | 4/4 | 3/3 | 12.04ms | 13.50ms | 2320 B |
| [more-redcross-org-types-of-emergencies-earthquake](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/earthquake.html) | safety | 96.2% | 98.2% | 6/7 | 3/3 | 26.37ms | 26.82ms | 14023 B |
| [more-redcross-org-types-of-emergencies-flood](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/flood.html) | safety | 96.4% | 97.0% | 7/7 | 3/3 | 24.59ms | 25.90ms | 10824 B |
| [more-redcross-org-types-of-emergencies-hurricane](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/hurricane.html) | safety | 94.5% | 98.1% | 6/7 | 3/3 | 29.25ms | 34.04ms | 15502 B |
| [more-redcross-org-types-of-emergencies-tornado](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/tornado.html) | safety | 96.8% | 97.3% | 6/7 | 3/3 | 26.76ms | 28.16ms | 12810 B |
| [more-rspb-org-uk-birds-and-wildlife-robin](https://www.rspb.org.uk/birds-and-wildlife/robin) | nature | 98.7% | 100.0% | 7/7 | 3/3 | 17.25ms | 18.75ms | 5555 B |
| [more-rspb-org-uk-birds-and-wildlife-blackbird](https://www.rspb.org.uk/birds-and-wildlife/blackbird) | nature | 98.8% | 100.0% | 7/7 | 3/3 | 16.89ms | 18.74ms | 5964 B |
| [more-rspb-org-uk-birds-and-wildlife-blue-tit](https://www.rspb.org.uk/birds-and-wildlife/blue-tit) | nature | 93.6% | 100.0% | 5/7 | 2/3 | 16.24ms | 17.56ms | 4661 B |
| [more-rspb-org-uk-birds-and-wildlife-house-sparrow](https://www.rspb.org.uk/birds-and-wildlife/house-sparrow) | nature | 98.8% | 100.0% | 7/7 | 3/3 | 16.90ms | 18.16ms | 6379 B |
| [more-britannica-com-science-earthquake-geology](https://www.britannica.com/science/earthquake-geology) | encyclopedia | 91.8% | 100.0% | 7/7 | 3/3 | 19.90ms | 20.50ms | 14777 B |
| [more-britannica-com-science-plate-tectonics](https://www.britannica.com/science/plate-tectonics) | encyclopedia | 90.3% | 100.0% | 7/7 | 3/3 | 18.00ms | 18.64ms | 12401 B |
| [more-nhs-uk-medicines-antibiotics](https://www.nhs.uk/medicines/antibiotics/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.19ms | 10.54ms | 8031 B |
| [more-britannica-com-science-continental-drift-geology](https://www.britannica.com/science/continental-drift-geology) | encyclopedia | 93.4% | 100.0% | 5/6 | 3/3 | 20.12ms | 20.34ms | 19171 B |
| [more-worldhistory-org-silk-road](https://www.worldhistory.org/Silk_Road/) | history | 100.0% | 97.5% | 7/7 | 3/3 | 20.45ms | 21.25ms | 17901 B |
| [more-worldhistory-org-egypt](https://www.worldhistory.org/egypt/) | history | 100.0% | 99.0% | 7/7 | 3/3 | 26.91ms | 27.31ms | 36228 B |
| [more-worldhistory-org-roman-republic](https://www.worldhistory.org/Roman_Republic/) | history | 96.7% | 98.1% | 7/7 | 3/3 | 22.36ms | 23.22ms | 23289 B |
| [more-worldhistory-org-mesopotamia](https://www.worldhistory.org/Mesopotamia/) | history | 100.0% | 99.0% | 7/7 | 3/3 | 27.85ms | 28.50ms | 38933 B |
| [more-fda-gov-nutrition-food-labeling-and-critical-foods-changes-nutrition-facts-label](https://www.fda.gov/food/nutrition-food-labeling-and-critical-foods/changes-nutrition-facts-label?scrlybrkr=) | consumer-guide | 99.4% | 100.0% | 7/7 | 3/3 | 16.89ms | 17.62ms | 21661 B |
| [more-fda-gov-buy-store-serve-safe-food-safe-food-handling](https://www.fda.gov/food/buy-store-serve-safe-food/safe-food-handling) | consumer-guide | 98.5% | 100.0% | 8/8 | 4/4 | 10.10ms | 10.47ms | 5797 B |
| [more-nhs-uk-antibiotics-side-effects](https://www.nhs.uk/medicines/antibiotics/side-effects/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 8.96ms | 9.98ms | 3062 B |
| [more-fda-gov-consumer-updates-it-really-fda-approved](https://www.fda.gov/consumers/consumer-updates/it-really-fda-approved) | consumer-guide | 99.0% | 100.0% | 7/7 | 3/3 | 12.13ms | 12.30ms | 18311 B |
| [more-plato-stanford-edu-entries-ethics-virtue](https://plato.stanford.edu/entries/ethics-virtue/) | reference | 99.9% | 99.6% | 5/6 | 3/3 | 31.20ms | 31.87ms | 90396 B |
| [more-plato-stanford-edu-entries-consciousness](https://plato.stanford.edu/entries/consciousness/) | reference | 100.0% | 99.8% | 6/6 | 3/3 | 47.13ms | 49.87ms | 155073 B |
| [more-plato-stanford-edu-entries-scientific-method](https://plato.stanford.edu/entries/scientific-method/) | reference | 99.9% | 99.7% | 6/6 | 3/3 | 34.42ms | 41.35ms | 99549 B |
| [more-plato-stanford-edu-entries-logic-classical](https://plato.stanford.edu/entries/logic-classical/) | reference | 99.9% | 99.7% | 6/6 | 3/3 | 41.40ms | 44.09ms | 121932 B |

## By content type

| Kind | Pages | Recall | Precision | Checks |
|---|---:|---:|---:|---:|
| accessibility | 10 | 99.8% | 96.0% | 80/80 |
| api | 45 | 99.9% | 99.6% | 351/361 |
| blog | 30 | 99.8% | 99.6% | 205/205 |
| book | 3 | 100.0% | 99.7% | 11/11 |
| catalogue | 1 | 94.5% | 100.0% | 7/7 |
| configuration | 26 | 99.1% | 99.8% | 204/209 |
| consumer-guide | 3 | 99.0% | 100.0% | 22/22 |
| data-article | 10 | 99.2% | 100.0% | 71/80 |
| database | 26 | 98.2% | 99.6% | 184/187 |
| discussion | 2 | 100.0% | 98.1% | 13/13 |
| docs | 62 | 99.7% | 99.4% | 472/479 |
| encyclopedia | 14 | 96.4% | 99.9% | 94/99 |
| essay | 31 | 99.4% | 99.3% | 176/185 |
| explainer | 9 | 95.3% | 99.8% | 54/63 |
| guide | 14 | 99.8% | 100.0% | 96/96 |
| health | 24 | 99.9% | 99.6% | 163/163 |
| historical-document | 1 | 100.0% | 100.0% | 5/5 |
| history | 13 | 99.6% | 98.2% | 88/88 |
| manual | 59 | 99.3% | 99.8% | 408/429 |
| nature | 4 | 97.5% | 100.0% | 26/28 |
| recipe | 6 | 90.7% | 99.6% | 46/47 |
| reference | 4 | 99.9% | 99.7% | 23/24 |
| repair-guide | 1 | 100.0% | 97.4% | 7/7 |
| safety | 8 | 95.4% | 98.8% | 39/43 |
| science | 35 | 97.2% | 98.5% | 219/228 |
| standard | 11 | 99.8% | 100.0% | 56/62 |
| tutorial | 36 | 99.9% | 99.8% | 266/269 |
| visitor-guide | 12 | 99.4% | 100.0% | 82/83 |

## By corpus cohort

Held-out sites were selected and annotated before this evaluation, without tuning extraction on their output. Once inspected, they become regression evidence; future blind evaluations need fresh sites.

| Cohort | Pages | Recall | Precision | Checks | Critical |
|---|---:|---:|---:|---:|---:|---:|
| expansion | 60 | 99.3% | 99.5% | 419/429 | 227/227 |
| heldout | 20 | 98.4% | 99.5% | 146/148 | 74/74 |
| regression | 20 | 99.2% | 99.8% | 115/115 | 79/79 |
| scale | 320 | 99.3% | 99.4% | 2233/2309 | 1167/1175 |
| scale-new-sites | 80 | 97.8% | 99.6% | 555/572 | 275/276 |

## Failures and omissions

- **go-strings/source-link** (link, critical=false)
- **rfc-uri/source-link** (link, critical=false)
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
