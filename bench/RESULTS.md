# Ketch extraction benchmark

2026-09-20T17:20:57Z · linux/amd64 · 24 logical CPUs

Binary build: go1.27.1, CGO_ENABLED=1.

Mode: **default**, extract mode **complete**. 7 measured runs + 1 warmups per page, 1 workers. Wall time 137.98s.

Corpus SHA256: `14c3ccc4f5eb5b6ef0bec2ca0727871d682d5845560ff7bce8bc70f8b719f441`  
Binary SHA256: `b4c38e6c2591bac8899fe3642f31f8c3e1aa0d04bfff80b0ce198934758715a5`

**3472/3573 checks passed; 1822/1831 critical checks; 0 failed or nondeterministic pages.**

Token coverage compares independently annotated source text with parsed Markdown. It is a multiset measure, not semantic correctness. Code, headings, table associations, links and exclusions are checked separately. Scores include every page; pages have equal weight in macro averages. Per-invocation timing includes CLI startup and excludes build, source loading, network and grading. Wall time includes warmups, grading and artifact writes. Selector mode is assisted recovery, not default extraction.

Macro token recall **99.3%**, precision **98.1%**, F1 **98.7%**.

| Page | Kind | Recall | Precision | Checks | Critical | Median | p95 | Output |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| [npm](https://docs.npmjs.com/trusted-publishers/) | docs | 99.8% | 99.3% | 6/6 | 4/4 | 45.24ms | 46.92ms | 21849 B |
| [go](https://pkg.go.dev/context) | api | 100.0% | 96.1% | 7/7 | 5/5 | 21.96ms | 23.37ms | 28019 B |
| [python](https://docs.python.org/3/tutorial/errors.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 19.84ms | 20.76ms | 26313 B |
| [kubernetes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) | configuration | 100.0% | 100.0% | 6/6 | 4/4 | 39.33ms | 41.26ms | 20688 B |
| [postgres](https://www.postgresql.org/docs/current/transaction-iso.html) | database | 100.0% | 100.0% | 6/6 | 4/4 | 22.86ms | 24.80ms | 25062 B |
| [mdn](https://developer.mozilla.org/en-US/docs/Web/API/AbortController) | api | 97.3% | 87.1% | 7/7 | 4/4 | 13.08ms | 15.34ms | 3984 B |
| [rust](https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 18.60ms | 19.17ms | 28278 B |
| [sqlite](https://www.sqlite.org/lang_transaction.html) | database | 100.0% | 100.0% | 6/6 | 3/3 | 11.54ms | 12.37ms | 9515 B |
| [git](https://git-scm.com/docs/git-reset) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 24.89ms | 25.15ms | 19815 B |
| [gnu](https://www.gnu.org/software/coreutils/manual/html_node/Exit-status.html) | manual | 100.0% | 88.0% | 4/5 | 3/3 | 6.49ms | 7.93ms | 1330 B |
| [terraform](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle) | configuration | 99.1% | 99.4% | 5/5 | 4/4 | 25.09ms | 27.94ms | 19244 B |
| [docker](https://docs.docker.com/build/building/multi-stage/) | tutorial | 100.0% | 100.0% | 5/5 | 4/4 | 36.19ms | 37.15ms | 6914 B |
| [cloudflare](https://blog.cloudflare.com/rfc-9457-agent-error-pages/) | blog | 99.9% | 99.2% | 6/7 | 5/5 | 38.93ms | 42.38ms | 18463 B |
| [danluu](https://danluu.com/slow-device/) | essay | 100.0% | 100.0% | 5/5 | 3/3 | 28.93ms | 30.07ms | 72798 B |
| [joel](https://www.joelonsoftware.com/2000/04/06/things-you-should-never-do-part-i/) | blog | 100.0% | 91.6% | 4/4 | 4/4 | 11.67ms | 12.11ms | 9788 B |
| [rfc](https://www.rfc-editor.org/rfc/rfc8259.html) | standard | 100.0% | 100.0% | 4/4 | 4/4 | 13.56ms | 15.81ms | 28586 B |
| [wikipedia](https://en.wikipedia.org/wiki/HTTP) | encyclopedia | 99.8% | 97.8% | 6/6 | 4/4 | 83.88ms | 87.45ms | 123735 B |
| [gutenberg](https://www.gutenberg.org/files/11/11-h/11-h.htm) | book | 100.0% | 99.6% | 4/4 | 3/3 | 49.73ms | 50.75ms | 152803 B |
| [nasa](https://science.nasa.gov/mars/facts/) | science | 92.5% | 99.4% | 5/5 | 4/4 | 20.22ms | 20.32ms | 9998 B |
| [nps](https://www.nps.gov/yell/planyourvisit/safety.htm) | visitor-guide | 99.6% | 99.4% | 9/9 | 5/5 | 23.02ms | 23.81ms | 25983 B |
| [npm-scripts](https://docs.npmjs.com/cli/v11/using-npm/scripts/) | docs | 100.0% | 98.7% | 8/8 | 4/4 | 47.48ms | 51.83ms | 16613 B |
| [npm-package-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-json/) | docs | 100.0% | 99.5% | 8/8 | 4/4 | 69.32ms | 70.30ms | 40805 B |
| [go-io](https://pkg.go.dev/io) | api | 100.0% | 96.5% | 8/8 | 4/4 | 33.20ms | 35.14ms | 42556 B |
| [go-strings](https://pkg.go.dev/strings) | api | 100.0% | 97.9% | 7/8 | 4/4 | 48.47ms | 51.99ms | 60103 B |
| [python-data](https://docs.python.org/3/tutorial/datastructures.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 22.38ms | 23.09ms | 26461 B |
| [python-classes](https://docs.python.org/3/tutorial/classes.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 23.66ms | 23.97ms | 38897 B |
| [kubernetes-deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) | configuration | 100.0% | 99.2% | 8/8 | 4/4 | 51.33ms | 53.35ms | 62253 B |
| [kubernetes-configmaps](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/) | configuration | 100.0% | 98.6% | 8/8 | 4/4 | 44.85ms | 46.83ms | 36411 B |
| [postgres-constraints](https://www.postgresql.org/docs/current/ddl-constraints.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 20.64ms | 21.05ms | 23853 B |
| [postgres-queries](https://www.postgresql.org/docs/current/queries-table-expressions.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 29.05ms | 31.80ms | 35404 B |
| [mdn-promise](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Promise) | api | 99.9% | 99.3% | 9/9 | 5/5 | 25.31ms | 30.02ms | 36605 B |
| [mdn-fetch](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch) | api | 100.0% | 99.1% | 8/8 | 4/4 | 24.44ms | 26.68ms | 31771 B |
| [rust-enums](https://doc.rust-lang.org/book/ch06-01-defining-an-enum.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 14.23ms | 14.89ms | 17267 B |
| [rust-result](https://doc.rust-lang.org/book/ch09-02-recoverable-errors-with-result.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 19.39ms | 19.98ms | 28175 B |
| [sqlite-select](https://www.sqlite.org/lang_select.html) | database | 94.4% | 100.0% | 7/7 | 4/4 | 159.38ms | 162.04ms | 38996 B |
| [sqlite-foreignkeys](https://www.sqlite.org/foreignkeys.html) | database | 98.9% | 100.0% | 7/7 | 4/4 | 21.50ms | 22.15ms | 38073 B |
| [git-rebase](https://git-scm.com/docs/git-rebase) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 53.30ms | 55.06ms | 57762 B |
| [git-restore](https://git-scm.com/docs/git-restore) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 16.92ms | 17.71ms | 7806 B |
| [gnu-copy](https://www.gnu.org/software/coreutils/manual/html_node/cp-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 25.32ms | 26.93ms | 22373 B |
| [gnu-timeout](https://www.gnu.org/software/coreutils/manual/html_node/timeout-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 10.92ms | 12.00ms | 5420 B |
| [terraform-foreach](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each) | configuration | 100.0% | 99.7% | 8/8 | 4/4 | 22.61ms | 25.20ms | 15748 B |
| [terraform-count](https://developer.hashicorp.com/terraform/language/meta-arguments/count) | configuration | 100.0% | 99.4% | 8/8 | 4/4 | 18.18ms | 18.77ms | 7140 B |
| [docker-volumes](https://docs.docker.com/engine/storage/volumes/) | docs | 100.0% | 94.8% | 9/9 | 5/5 | 48.11ms | 50.13ms | 27132 B |
| [docker-bridge](https://docs.docker.com/engine/network/drivers/bridge/) | docs | 100.0% | 97.3% | 9/9 | 5/5 | 46.26ms | 47.92ms | 25782 B |
| [cloudflare-pingora](https://blog.cloudflare.com/how-we-built-pingora-the-proxy-that-connects-cloudflare-to-the-internet/) | blog | 100.0% | 99.1% | 7/7 | 3/3 | 27.40ms | 27.86ms | 17143 B |
| [cloudflare-opensource](https://blog.cloudflare.com/pingora-open-source/) | blog | 99.9% | 98.7% | 8/8 | 4/4 | 32.55ms | 34.85ms | 12836 B |
| [danluu-branch](https://danluu.com/branch-prediction/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 16.39ms | 16.62ms | 36462 B |
| [danluu-malloc](https://danluu.com/malloc-tutorial/) | essay | 100.0% | 100.0% | 6/6 | 4/4 | 12.41ms | 13.12ms | 21111 B |
| [joel-test](https://www.joelonsoftware.com/2000/08/09/the-joel-test-12-steps-to-better-code/) | blog | 100.0% | 96.5% | 5/5 | 3/3 | 13.98ms | 14.34ms | 22712 B |
| [joel-leaky](https://www.joelonsoftware.com/2002/11/11/the-law-of-leaky-abstractions/) | blog | 100.0% | 94.5% | 5/5 | 3/3 | 11.91ms | 13.25ms | 13980 B |
| [rfc-http](https://www.rfc-editor.org/rfc/rfc9110.html) | standard | 100.0% | 100.0% | 7/7 | 4/4 | 301.91ms | 304.38ms | 721662 B |
| [rfc-uri](https://www.rfc-editor.org/rfc/rfc3986.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 31.07ms | 34.75ms | 142714 B |
| [wikipedia-photosynthesis](https://en.wikipedia.org/wiki/Photosynthesis) | encyclopedia | 99.6% | 98.0% | 8/8 | 4/4 | 111.19ms | 115.34ms | 226154 B |
| [wikipedia-binary](https://en.wikipedia.org/wiki/Binary_search) | encyclopedia | 92.4% | 98.8% | 8/8 | 4/4 | 94.53ms | 103.85ms | 139714 B |
| [gutenberg-wallpaper](https://www.gutenberg.org/files/1952/1952-h/1952-h.htm) | book | 100.0% | 99.8% | 4/4 | 3/3 | 15.94ms | 16.89ms | 32151 B |
| [gutenberg-metamorphosis](https://www.gutenberg.org/files/5200/5200-h/5200-h.htm) | book | 100.0% | 99.8% | 3/3 | 3/3 | 28.65ms | 31.02ms | 120067 B |
| [nasa-earth](https://science.nasa.gov/earth/facts/) | science | 99.8% | 95.5% | 7/7 | 3/3 | 21.39ms | 22.60ms | 13087 B |
| [nasa-jupiter](https://science.nasa.gov/jupiter/jupiter-facts/) | science | 99.1% | 99.3% | 7/7 | 3/3 | 20.11ms | 21.05ms | 13722 B |
| [nps-hiking](https://www.nps.gov/grca/planyourvisit/hiking-faq.htm) | visitor-guide | 99.8% | 99.5% | 6/6 | 3/3 | 22.11ms | 24.14ms | 29327 B |
| [nps-canyon](https://www.nps.gov/grca/faqs.htm) | visitor-guide | 94.4% | 99.4% | 7/7 | 3/3 | 20.42ms | 22.73ms | 23951 B |
| [pandas-indexing](https://pandas.pydata.org/docs/user_guide/indexing.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 56.03ms | 57.66ms | 98846 B |
| [pandas-merge](https://pandas.pydata.org/docs/user_guide/merging.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 34.38ms | 37.07ms | 54477 B |
| [django-queries](https://docs.djangoproject.com/en/5.2/topics/db/queries/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 49.71ms | 51.83ms | 85748 B |
| [django-transactions](https://docs.djangoproject.com/en/5.2/topics/db/transactions/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 26.40ms | 28.14ms | 35970 B |
| [curl-manpage](https://curl.se/docs/manpage.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 193.04ms | 197.29ms | 334470 B |
| [curl-http](https://curl.se/docs/httpscripting.html) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 18.34ms | 18.69ms | 28331 B |
| [w3c-tables](https://www.w3.org/WAI/tutorials/tables/one-header/) | accessibility | 100.0% | 94.5% | 9/9 | 5/5 | 12.36ms | 12.73ms | 4734 B |
| [w3c-irregular](https://www.w3.org/WAI/tutorials/tables/irregular/) | accessibility | 100.0% | 96.7% | 8/8 | 4/4 | 12.71ms | 14.04ms | 7013 B |
| [owid-life](https://ourworldindata.org/life-expectancy) | data-article | 99.6% | 100.0% | 6/8 | 4/4 | 27.84ms | 30.26ms | 40730 B |
| [owid-population](https://ourworldindata.org/population-growth) | data-article | 99.4% | 100.0% | 6/8 | 4/4 | 23.39ms | 23.94ms | 30410 B |
| [eff-passwords](https://ssd.eff.org/module/creating-strong-passwords) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 12.46ms | 12.63ms | 8739 B |
| [eff-plan](https://ssd.eff.org/module/your-security-plan) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 12.37ms | 13.64ms | 9499 B |
| [paulgraham-lisp](https://www.paulgraham.com/avg.html) | essay | 99.3% | 100.0% | 3/4 | 3/3 | 15.86ms | 16.74ms | 25543 B |
| [paulgraham-schedule](https://www.paulgraham.com/makersschedule.html) | essay | 98.5% | 100.0% | 3/4 | 3/3 | 10.60ms | 11.27ms | 6665 B |
| [fowler-microservices](https://martinfowler.com/articles/microservices.html) | essay | 99.6% | 90.4% | 7/7 | 3/3 | 22.24ms | 23.57ms | 55132 B |
| [fowler-debt](https://martinfowler.com/bliki/TechnicalDebt.html) | essay | 100.0% | 90.3% | 7/7 | 3/3 | 9.98ms | 10.16ms | 8721 B |
| [letsencrypt-challenges](https://letsencrypt.org/docs/challenge-types/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 10.19ms | 10.92ms | 7690 B |
| [letsencrypt-compatibility](https://letsencrypt.org/docs/certificate-compatibility/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 10.08ms | 10.60ms | 5133 B |
| [noaa-salt](https://oceanservice.noaa.gov/facts/whysalty.html) | science | 100.0% | 95.9% | 6/6 | 3/3 | 11.03ms | 11.42ms | 4003 B |
| [noaa-blue](https://oceanservice.noaa.gov/facts/oceanblue.html) | science | 94.7% | 80.0% | 5/6 | 3/3 | 10.29ms | 11.01ms | 1227 B |
| [bgs-earthquakes](https://www.bgs.ac.uk/discovering-geology/earth-hazards/earthquakes/how-are-earthquakes-detected/) | science | 98.7% | 92.6% | 7/8 | 4/4 | 24.66ms | 25.91ms | 21374 B |
| [medlineplus-handwashing](https://medlineplus.gov/ency/patientinstructions/000972.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.23ms | 10.78ms | 5154 B |
| [who-resistance](https://www.who.int/news-room/fact-sheets/detail/antimicrobial-resistance) | health | 100.0% | 97.6% | 7/7 | 3/3 | 16.11ms | 16.29ms | 12105 B |
| [un-climate](https://www.un.org/en/climatechange/what-is-climate-change) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 27.27ms | 28.33ms | 11847 B |
| [computerhistory-1969](https://www.computerhistory.org/timeline/1969/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 13.44ms | 13.86ms | 6033 B |
| [nhm-meteorites](https://www.nhm.ac.uk/discover/types-of-meteorites.html) | science | 99.4% | 93.3% | 7/7 | 3/3 | 19.77ms | 20.40ms | 12196 B |
| [standardebooks-austen](https://standardebooks.org/ebooks/jane-austen/pride-and-prejudice) | catalogue | 95.4% | 100.0% | 7/7 | 3/3 | 10.64ms | 11.52ms | 6443 B |
| [archives-declaration](https://www.archives.gov/founding-docs/declaration-transcript) | historical-document | 100.0% | 92.5% | 5/5 | 3/3 | 15.63ms | 15.98ms | 11824 B |
| [wikivoyage-paris](https://en.wikivoyage.org/wiki/Paris) | visitor-guide | 100.0% | 99.9% | 8/8 | 4/4 | 217.98ms | 220.56ms | 236997 B |
| [kingarthur-bread](https://www.kingarthurbaking.com/recipes/classic-sandwich-bread-recipe) | recipe | 96.3% | 98.5% | 10/10 | 6/6 | 22.01ms | 23.81ms | 7817 B |
| [seriouseats-cookies](https://www.seriouseats.com/the-food-lab-best-chocolate-chip-cookie-recipe) | recipe | 99.2% | 98.9% | 9/9 | 5/5 | 42.68ms | 45.60ms | 42187 B |
| [ifixit-moped](https://www.ifixit.com/Guide/Suzuki+FA50+Moped+Exhaust+Cleaning/3926) | repair-guide | 100.0% | 79.7% | 7/7 | 3/3 | 28.67ms | 31.92ms | 7530 B |
| [discourse-guide](https://meta.discourse.org/t/understanding-discourse-for-new-users/96331) | discussion | 100.0% | 92.4% | 8/8 | 4/4 | 18.38ms | 18.89ms | 16812 B |
| [hackernews-startup](https://news.ycombinator.com/item?id=8863) | discussion | 100.0% | 99.7% | 5/5 | 3/3 | 38.55ms | 39.46ms | 34613 B |
| [julia-arrays](https://docs.julialang.org/en/v1/manual/arrays/) | manual | 100.0% | 100.0% | 9/9 | 5/5 | 27.64ms | 32.46ms | 48421 B |
| [elixir-enum](https://elixir.hexdocs.pm/enum-cheat.html) | api | 99.8% | 99.6% | 8/8 | 4/4 | 30.71ms | 33.84ms | 22538 B |
| [ffmpeg-filters](https://ffmpeg.org/ffmpeg-filters.html) | manual | 97.0% | 100.0% | 5/7 | 4/4 | 871.37ms | 882.58ms | 919390 B |
| [angular-signals](https://angular.dev/guide/signals) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 20.95ms | 22.76ms | 12515 B |
| [react-state](https://react.dev/learn/state-a-components-memory) | tutorial | 99.8% | 99.6% | 8/8 | 4/4 | 31.29ms | 33.80ms | 21608 B |
| [ncat-guide](https://nmap.org/ncat/guide/index.html) | manual | 88.3% | 100.0% | 5/6 | 3/3 | 8.46ms | 9.10ms | 3154 B |
| [more-docs-npmjs-com-commands-npm-install](https://docs.npmjs.com/cli/v11/commands/npm-install/) | docs | 100.0% | 99.5% | 8/8 | 4/4 | 59.34ms | 61.41ms | 37166 B |
| [more-docs-npmjs-com-commands-npm-ci](https://docs.npmjs.com/cli/v11/commands/npm-ci/) | docs | 100.0% | 98.8% | 7/8 | 4/4 | 43.69ms | 44.58ms | 14825 B |
| [more-docs-npmjs-com-commands-npm-publish](https://docs.npmjs.com/cli/v11/commands/npm-publish/) | docs | 100.0% | 97.9% | 7/8 | 4/4 | 37.85ms | 40.43ms | 9622 B |
| [more-docs-npmjs-com-commands-npm-audit](https://docs.npmjs.com/cli/v11/commands/npm-audit/) | docs | 100.0% | 98.9% | 7/8 | 4/4 | 46.08ms | 48.12ms | 17227 B |
| [more-docs-npmjs-com-commands-npm-exec](https://docs.npmjs.com/cli/v11/commands/npm-exec/) | docs | 100.0% | 98.7% | 7/8 | 4/4 | 42.97ms | 43.41ms | 14143 B |
| [more-docs-npmjs-com-commands-npm-run](https://docs.npmjs.com/cli/v11/commands/npm-run/) | docs | 100.0% | 97.8% | 7/8 | 4/4 | 38.29ms | 41.01ms | 8209 B |
| [more-docs-npmjs-com-configuring-npm-package-lock-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/) | docs | 100.0% | 98.5% | 6/7 | 3/3 | 32.29ms | 36.10ms | 11751 B |
| [more-docs-npmjs-com-using-npm-workspaces](https://docs.npmjs.com/cli/v11/using-npm/workspaces/) | docs | 100.0% | 97.3% | 7/8 | 4/4 | 36.76ms | 38.85ms | 7624 B |
| [more-pkg-go-dev-bytes](https://pkg.go.dev/bytes) | api | 100.0% | 98.4% | 7/8 | 4/4 | 54.97ms | 57.84ms | 76764 B |
| [more-pkg-go-dev-errors](https://pkg.go.dev/errors) | api | 100.0% | 94.3% | 7/8 | 4/4 | 18.37ms | 19.66ms | 18176 B |
| [more-pkg-go-dev-fmt](https://pkg.go.dev/fmt) | api | 100.0% | 98.3% | 7/8 | 4/4 | 30.99ms | 33.30ms | 52352 B |
| [more-pkg-go-dev-sync](https://pkg.go.dev/sync) | api | 99.9% | 96.3% | 8/8 | 4/4 | 29.58ms | 32.80ms | 35181 B |
| [more-pkg-go-dev-time](https://pkg.go.dev/time) | api | 99.9% | 98.7% | 8/8 | 4/4 | 57.12ms | 58.87ms | 95829 B |
| [more-pkg-go-dev-encoding-json](https://pkg.go.dev/encoding/json) | api | 100.0% | 97.1% | 7/8 | 4/4 | 47.36ms | 47.81ms | 78648 B |
| [more-pkg-go-dev-net-http](https://pkg.go.dev/net/http) | api | 99.9% | 98.4% | 7/8 | 4/4 | 93.26ms | 93.82ms | 200446 B |
| [more-pkg-go-dev-os](https://pkg.go.dev/os) | api | 99.8% | 98.1% | 8/8 | 4/4 | 61.99ms | 64.67ms | 95150 B |
| [more-docs-python-org-tutorial-controlflow](https://docs.python.org/3/tutorial/controlflow.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 29.22ms | 31.86ms | 40934 B |
| [more-docs-python-org-tutorial-modules](https://docs.python.org/3/tutorial/modules.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 18.88ms | 20.10ms | 25859 B |
| [more-docs-python-org-tutorial-inputoutput](https://docs.python.org/3/tutorial/inputoutput.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 19.87ms | 20.92ms | 23054 B |
| [more-docs-python-org-tutorial-stdlib](https://docs.python.org/3/tutorial/stdlib.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 15.76ms | 16.39ms | 15475 B |
| [more-docs-python-org-tutorial-stdlib2](https://docs.python.org/3/tutorial/stdlib2.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 16.14ms | 17.03ms | 17559 B |
| [more-docs-python-org-tutorial-venv](https://docs.python.org/3/tutorial/venv.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 11.76ms | 12.26ms | 7778 B |
| [more-docs-python-org-tutorial-floatingpoint](https://docs.python.org/3/tutorial/floatingpoint.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 13.45ms | 14.12ms | 13632 B |
| [more-docs-python-org-tutorial-interpreter](https://docs.python.org/3/tutorial/interpreter.html) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 11.42ms | 12.18ms | 6915 B |
| [more-kubernetes-io-workloads-pods](https://kubernetes.io/docs/concepts/workloads/pods/) | configuration | 100.0% | 98.2% | 8/8 | 4/4 | 41.56ms | 42.89ms | 31558 B |
| [more-kubernetes-io-controllers-statefulset](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) | configuration | 100.0% | 98.5% | 9/9 | 5/5 | 44.18ms | 46.50ms | 33483 B |
| [more-kubernetes-io-services-networking-service](https://kubernetes.io/docs/concepts/services-networking/service/) | configuration | 100.0% | 99.1% | 9/9 | 5/5 | 57.83ms | 58.85ms | 49886 B |
| [more-kubernetes-io-services-networking-ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) | configuration | 100.0% | 98.5% | 9/9 | 5/5 | 44.51ms | 46.41ms | 35037 B |
| [more-kubernetes-io-storage-persistent-volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) | configuration | 100.0% | 99.1% | 9/9 | 5/5 | 49.75ms | 50.29ms | 51997 B |
| [more-kubernetes-io-configuration-secret](https://kubernetes.io/docs/concepts/configuration/secret/) | configuration | 100.0% | 98.7% | 9/9 | 5/5 | 44.31ms | 44.79ms | 39281 B |
| [more-kubernetes-io-scheduling-eviction-taint-and-toleration](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) | configuration | 100.0% | 97.9% | 8/8 | 4/4 | 39.60ms | 40.40ms | 21439 B |
| [more-kubernetes-io-working-with-objects-labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) | configuration | 100.0% | 97.0% | 8/8 | 4/4 | 36.97ms | 38.83ms | 16575 B |
| [more-postgresql-org-current-ddl-default](https://www.postgresql.org/docs/current/ddl-default.html) | database | 100.0% | 81.2% | 7/7 | 4/4 | 10.55ms | 12.15ms | 3070 B |
| [more-postgresql-org-current-ddl-alter](https://www.postgresql.org/docs/current/ddl-alter.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 12.10ms | 12.76ms | 6502 B |
| [more-postgresql-org-current-ddl-inherit](https://www.postgresql.org/docs/current/ddl-inherit.html) | database | 100.0% | 96.6% | 8/8 | 4/4 | 16.05ms | 17.30ms | 12951 B |
| [more-postgresql-org-current-ddl-partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 31.19ms | 32.09ms | 44403 B |
| [more-postgresql-org-current-indexes-intro](https://www.postgresql.org/docs/current/indexes-intro.html) | database | 100.0% | 92.6% | 7/7 | 4/4 | 11.28ms | 12.83ms | 5542 B |
| [more-postgresql-org-current-queries-with](https://www.postgresql.org/docs/current/queries-with.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 19.39ms | 20.41ms | 20470 B |
| [more-postgresql-org-current-queries-order](https://www.postgresql.org/docs/current/queries-order.html) | database | 100.0% | 89.5% | 6/6 | 4/4 | 11.81ms | 13.35ms | 4577 B |
| [more-postgresql-org-current-sql-select](https://www.postgresql.org/docs/current/sql-select.html) | database | 100.0% | 100.0% | 8/8 | 4/4 | 47.87ms | 48.52ms | 67788 B |
| [more-developer-mozilla-org-global-objects-map](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Map) | api | 100.0% | 98.7% | 8/8 | 4/4 | 23.02ms | 23.95ms | 24761 B |
| [more-developer-mozilla-org-global-objects-set](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Set) | api | 100.0% | 98.6% | 9/9 | 5/5 | 22.86ms | 23.48ms | 23978 B |
| [more-developer-mozilla-org-guide-iterators-and-generators](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Iterators_and_generators) | api | 100.0% | 97.6% | 8/8 | 4/4 | 19.74ms | 20.87ms | 13158 B |
| [more-developer-mozilla-org-grid-layout-basic-concepts](https://developer.mozilla.org/en-US/docs/Web/CSS/Guides/Grid_layout/Basic_concepts) | api | 100.0% | 99.1% | 8/8 | 4/4 | 33.49ms | 35.86ms | 26077 B |
| [more-developer-mozilla-org-elements-details](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/details) | api | 100.0% | 97.5% | 9/9 | 5/5 | 20.11ms | 20.92ms | 11772 B |
| [more-developer-mozilla-org-api-intersection-observer-api](https://developer.mozilla.org/en-US/docs/Web/API/Intersection_Observer_API) | api | 99.8% | 99.5% | 9/9 | 5/5 | 26.93ms | 27.53ms | 46385 B |
| [more-developer-mozilla-org-guides-overview](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Overview) | api | 100.0% | 98.7% | 8/8 | 4/4 | 22.38ms | 23.01ms | 19111 B |
| [more-developer-mozilla-org-accessibility-html](https://developer.mozilla.org/en-US/docs/Learn_web_development/Core/Accessibility/HTML) | api | 100.0% | 99.3% | 8/8 | 4/4 | 28.84ms | 30.30ms | 40819 B |
| [more-doc-rust-lang-org-book-ch03-01-variables-and-mutability](https://doc.rust-lang.org/book/ch03-01-variables-and-mutability.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 11.63ms | 12.11ms | 10258 B |
| [more-doc-rust-lang-org-book-ch03-02-data-types](https://doc.rust-lang.org/book/ch03-02-data-types.html) | tutorial | 100.0% | 100.0% | 8/8 | 5/5 | 15.06ms | 15.54ms | 17168 B |
| [more-doc-rust-lang-org-book-ch04-02-references-and-borrowing](https://doc.rust-lang.org/book/ch04-02-references-and-borrowing.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 11.92ms | 12.95ms | 13651 B |
| [more-doc-rust-lang-org-book-ch05-01-defining-structs](https://doc.rust-lang.org/book/ch05-01-defining-structs.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 13.95ms | 14.29ms | 14644 B |
| [more-doc-rust-lang-org-book-ch08-01-vectors](https://doc.rust-lang.org/book/ch08-01-vectors.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 11.82ms | 13.45ms | 12925 B |
| [more-doc-rust-lang-org-book-ch10-02-traits](https://doc.rust-lang.org/book/ch10-02-traits.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 16.78ms | 17.23ms | 23109 B |
| [more-doc-rust-lang-org-book-ch13-02-iterators](https://doc.rust-lang.org/book/ch13-02-iterators.html) | tutorial | 100.0% | 100.0% | 6/6 | 4/4 | 12.91ms | 14.30ms | 12193 B |
| [more-doc-rust-lang-org-book-ch16-01-threads](https://doc.rust-lang.org/book/ch16-01-threads.html) | tutorial | 100.0% | 100.0% | 7/7 | 4/4 | 13.27ms | 13.83ms | 14839 B |
| [more-sqlite-org-lang-insert](https://www.sqlite.org/lang_insert.html) | database | 93.6% | 100.0% | 3/4 | 3/3 | 29.79ms | 31.84ms | 5072 B |
| [more-sqlite-org-lang-update](https://www.sqlite.org/lang_update.html) | database | 93.1% | 100.0% | 6/7 | 4/4 | 52.36ms | 53.52ms | 8716 B |
| [more-sqlite-org-lang-delete](https://www.sqlite.org/lang_delete.html) | database | 90.3% | 100.0% | 3/4 | 3/3 | 35.83ms | 38.32ms | 5854 B |
| [more-sqlite-org-lang-createtable](https://www.sqlite.org/lang_createtable.html) | database | 94.5% | 100.0% | 8/8 | 5/5 | 69.14ms | 70.57ms | 20825 B |
| [more-sqlite-org-lang-with](https://www.sqlite.org/lang_with.html) | database | 97.3% | 99.9% | 6/7 | 3/4 | 42.36ms | 43.26ms | 26833 B |
| [more-sqlite-org-windowfunctions](https://www.sqlite.org/windowfunctions.html) | database | 96.8% | 100.0% | 8/8 | 5/5 | 86.22ms | 86.96ms | 35460 B |
| [more-sqlite-org-datatype3](https://www.sqlite.org/datatype3.html) | database | 97.8% | 100.0% | 7/8 | 4/5 | 19.78ms | 20.97ms | 29993 B |
| [more-sqlite-org-isolation](https://www.sqlite.org/isolation.html) | database | 100.0% | 99.7% | 6/6 | 3/3 | 11.08ms | 13.14ms | 14282 B |
| [more-git-scm-com-docs-git-merge](https://git-scm.com/docs/git-merge) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 42.07ms | 43.87ms | 37035 B |
| [more-git-scm-com-docs-git-cherry-pick](https://git-scm.com/docs/git-cherry-pick) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 18.29ms | 19.12ms | 10265 B |
| [more-git-scm-com-docs-git-bisect](https://git-scm.com/docs/git-bisect) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 22.86ms | 25.05ms | 16725 B |
| [more-git-scm-com-docs-git-stash](https://git-scm.com/docs/git-stash) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 26.15ms | 28.71ms | 17260 B |
| [more-git-scm-com-docs-git-reflog](https://git-scm.com/docs/git-reflog) | manual | 99.9% | 100.0% | 8/8 | 4/4 | 15.43ms | 17.25ms | 5674 B |
| [more-git-scm-com-docs-git-worktree](https://git-scm.com/docs/git-worktree) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 30.49ms | 31.83ms | 23644 B |
| [more-git-scm-com-docs-git-fetch](https://git-scm.com/docs/git-fetch) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 48.59ms | 50.77ms | 47366 B |
| [more-git-scm-com-docs-git-log](https://git-scm.com/docs/git-log) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 109.94ms | 115.41ms | 116066 B |
| [more-gnu-org-html-node-ls-invocation](https://www.gnu.org/software/coreutils/manual/html_node/ls-invocation.html) | manual | 100.0% | 87.3% | 6/6 | 4/4 | 9.34ms | 10.00ms | 2859 B |
| [more-gnu-org-html-node-mv-invocation](https://www.gnu.org/software/coreutils/manual/html_node/mv-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 13.46ms | 14.56ms | 8882 B |
| [more-gnu-org-html-node-rm-invocation](https://www.gnu.org/software/coreutils/manual/html_node/rm-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 11.62ms | 12.96ms | 5695 B |
| [more-gnu-org-html-node-dd-invocation](https://www.gnu.org/software/coreutils/manual/html_node/dd-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 24.97ms | 27.68ms | 22695 B |
| [more-gnu-org-html-node-sort-invocation](https://www.gnu.org/software/coreutils/manual/html_node/sort-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 24.26ms | 25.49ms | 27407 B |
| [more-gnu-org-html-node-uniq-invocation](https://www.gnu.org/software/coreutils/manual/html_node/uniq-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 12.22ms | 13.07ms | 6818 B |
| [more-gnu-org-html-node-date-invocation](https://www.gnu.org/software/coreutils/manual/html_node/date-invocation.html) | manual | 100.0% | 93.8% | 6/6 | 4/4 | 7.40ms | 8.65ms | 2037 B |
| [more-gnu-org-html-node-chmod-invocation](https://www.gnu.org/software/coreutils/manual/html_node/chmod-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 12.68ms | 13.07ms | 6756 B |
| [more-developer-hashicorp-com-values-variables](https://developer.hashicorp.com/terraform/language/values/variables) | configuration | 100.0% | 99.7% | 8/8 | 4/4 | 20.12ms | 20.64ms | 12410 B |
| [more-developer-hashicorp-com-values-outputs](https://developer.hashicorp.com/terraform/language/values/outputs) | configuration | 100.0% | 99.1% | 8/8 | 4/4 | 16.86ms | 17.85ms | 4627 B |
| [more-developer-hashicorp-com-values-locals](https://developer.hashicorp.com/terraform/language/values/locals) | configuration | 100.0% | 98.8% | 8/8 | 4/4 | 15.46ms | 18.93ms | 3338 B |
| [more-developer-hashicorp-com-expressions-types](https://developer.hashicorp.com/terraform/language/expressions/types) | configuration | 100.0% | 99.5% | 8/8 | 4/4 | 18.17ms | 19.24ms | 8030 B |
| [more-developer-hashicorp-com-expressions-conditionals](https://developer.hashicorp.com/terraform/language/expressions/conditionals) | configuration | 100.0% | 99.5% | 8/8 | 4/4 | 19.20ms | 19.70ms | 7637 B |
| [more-developer-hashicorp-com-expressions-for](https://developer.hashicorp.com/terraform/language/expressions/for) | configuration | 100.0% | 99.5% | 8/8 | 4/4 | 18.88ms | 19.39ms | 6737 B |
| [more-developer-hashicorp-com-block-module](https://developer.hashicorp.com/terraform/language/block/module) | configuration | 97.9% | 99.9% | 8/8 | 4/4 | 38.06ms | 40.43ms | 36686 B |
| [more-developer-hashicorp-com-meta-arguments-depends-on](https://developer.hashicorp.com/terraform/language/meta-arguments/depends_on) | configuration | 100.0% | 99.6% | 8/8 | 4/4 | 19.46ms | 20.46ms | 8964 B |
| [more-docs-docker-com-building-best-practices](https://docs.docker.com/build/building/best-practices/) | docs | 100.0% | 95.9% | 8/8 | 4/4 | 47.91ms | 49.29ms | 32980 B |
| [more-docs-docker-com-build-cache](https://docs.docker.com/build/cache/) | docs | 100.0% | 98.9% | 8/8 | 4/4 | 32.62ms | 35.15ms | 2075 B |
| [more-docs-docker-com-building-secrets](https://docs.docker.com/build/building/secrets/) | docs | 100.0% | 99.8% | 8/8 | 4/4 | 40.48ms | 41.90ms | 9483 B |
| [more-docs-docker-com-storage-bind-mounts](https://docs.docker.com/engine/storage/bind-mounts/) | docs | 100.0% | 98.6% | 9/9 | 5/5 | 43.06ms | 43.24ms | 17842 B |
| [more-docs-docker-com-drivers-host](https://docs.docker.com/engine/network/drivers/host/) | docs | 100.0% | 92.5% | 8/8 | 4/4 | 36.61ms | 37.56ms | 5879 B |
| [more-docs-docker-com-containers-resource-constraints](https://docs.docker.com/engine/containers/resource_constraints/) | docs | 100.0% | 98.1% | 9/9 | 5/5 | 38.15ms | 39.47ms | 13877 B |
| [more-docs-docker-com-how-tos-startup-order](https://docs.docker.com/compose/how-tos/startup-order/) | docs | 100.0% | 99.2% | 8/8 | 4/4 | 33.91ms | 36.02ms | 2867 B |
| [more-docs-docker-com-environment-variables-set-environment-variables](https://docs.docker.com/compose/how-tos/environment-variables/set-environment-variables/) | docs | 100.0% | 93.4% | 8/8 | 4/4 | 37.02ms | 37.53ms | 5459 B |
| [more-blog-cloudflare-com-how-pingora-keeps-count](https://blog.cloudflare.com/how-pingora-keeps-count/) | blog | 99.9% | 98.8% | 9/9 | 5/5 | 34.75ms | 35.41ms | 12442 B |
| [more-blog-cloudflare-com-cloudflare-workers-unleashed](https://blog.cloudflare.com/cloudflare-workers-unleashed/) | blog | 99.8% | 98.1% | 8/8 | 4/4 | 31.91ms | 35.64ms | 10067 B |
| [more-blog-cloudflare-com-introducing-cloudflare-workers](https://blog.cloudflare.com/introducing-cloudflare-workers/) | blog | 99.9% | 99.2% | 8/8 | 4/4 | 36.52ms | 37.33ms | 19626 B |
| [more-blog-cloudflare-com-introducing-cache-reserve](https://blog.cloudflare.com/introducing-cache-reserve/) | blog | 99.9% | 98.6% | 7/7 | 3/3 | 26.54ms | 29.38ms | 12385 B |
| [more-blog-cloudflare-com-the-sad-state-of-linux-socket-balancing](https://blog.cloudflare.com/the-sad-state-of-linux-socket-balancing/) | blog | 99.9% | 98.7% | 8/8 | 4/4 | 34.63ms | 37.93ms | 15962 B |
| [more-blog-cloudflare-com-keepalives-considered-harmful](https://blog.cloudflare.com/keepalives-considered-harmful/) | blog | 100.0% | 99.1% | 8/8 | 4/4 | 37.91ms | 40.29ms | 17098 B |
| [more-blog-cloudflare-com-road-to-grpc](https://blog.cloudflare.com/road-to-grpc/) | blog | 100.0% | 98.6% | 7/7 | 3/3 | 26.36ms | 26.91ms | 13656 B |
| [more-blog-cloudflare-com-the-road-to-quic](https://blog.cloudflare.com/the-road-to-quic/) | blog | 99.9% | 99.2% | 7/7 | 3/3 | 28.45ms | 29.86ms | 19095 B |
| [more-danluu-com-testing](https://danluu.com/testing/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 15.00ms | 15.28ms | 32543 B |
| [more-danluu-com-latency-mitigation](https://danluu.com/latency-mitigation/) | essay | 100.0% | 100.0% | 5/5 | 4/4 | 14.36ms | 15.75ms | 33575 B |
| [more-danluu-com-input-lag](https://danluu.com/input-lag/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 18.53ms | 19.28ms | 40532 B |
| [more-danluu-com-file-consistency](https://danluu.com/file-consistency/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 18.92ms | 20.83ms | 32942 B |
| [more-danluu-com-postmortem-lessons](https://danluu.com/postmortem-lessons/) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 10.34ms | 11.83ms | 16401 B |
| [more-danluu-com-keyboard-latency](https://danluu.com/keyboard-latency/) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 14.29ms | 15.38ms | 29956 B |
| [more-danluu-com-cpu-bugs](https://danluu.com/cpu-bugs/) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 13.29ms | 13.67ms | 28004 B |
| [more-danluu-com-simple-architectures](https://danluu.com/simple-architectures/) | essay | 100.0% | 100.0% | 5/5 | 3/3 | 10.14ms | 10.20ms | 13769 B |
| [more-joelonsoftware-com-02-painless-functional-specifications-part-1-why-bother](https://www.joelonsoftware.com/2000/10/02/painless-functional-specifications-part-1-why-bother/) | blog | 100.0% | 94.8% | 5/5 | 3/3 | 11.86ms | 13.92ms | 15878 B |
| [more-joelonsoftware-com-22-three-wrong-ideas-from-computer-science](https://www.joelonsoftware.com/2000/08/22/three-wrong-ideas-from-computer-science/) | blog | 100.0% | 90.2% | 7/7 | 3/3 | 10.58ms | 11.98ms | 9358 B |
| [more-joelonsoftware-com-11-back-to-basics](https://www.joelonsoftware.com/2001/12/11/back-to-basics/) | blog | 100.0% | 96.0% | 6/6 | 4/4 | 14.97ms | 15.86ms | 20536 B |
| [more-joelonsoftware-com-29-test-yourself](https://www.joelonsoftware.com/2005/12/29/test-yourself/) | blog | 100.0% | 77.3% | 6/6 | 4/4 | 10.66ms | 11.01ms | 4067 B |
| [more-joelonsoftware-com-12-strategy-letter-v](https://www.joelonsoftware.com/2002/06/12/strategy-letter-v/) | blog | 100.0% | 95.5% | 5/5 | 3/3 | 12.76ms | 13.11ms | 18702 B |
| [more-joelonsoftware-com-29-the-perils-of-javaschools-2](https://www.joelonsoftware.com/2005/12/29/the-perils-of-javaschools-2/) | blog | 100.0% | 95.1% | 5/5 | 3/3 | 12.31ms | 12.83ms | 16497 B |
| [more-joelonsoftware-com-19-two-stories](https://www.joelonsoftware.com/2000/03/19/two-stories/) | blog | 100.0% | 93.4% | 4/4 | 3/3 | 11.32ms | 11.90ms | 12075 B |
| [more-joelonsoftware-com-08-painless-bug-tracking](https://www.joelonsoftware.com/2000/11/08/painless-bug-tracking/) | blog | 100.0% | 94.8% | 7/7 | 3/3 | 13.38ms | 13.97ms | 15126 B |
| [more-rfc-editor-org-rfc-rfc8446](https://www.rfc-editor.org/rfc/rfc8446.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 62.84ms | 63.77ms | 340131 B |
| [more-rfc-editor-org-rfc-rfc9000](https://www.rfc-editor.org/rfc/rfc9000.html) | standard | 100.0% | 100.0% | 7/7 | 4/4 | 192.72ms | 196.50ms | 506591 B |
| [more-rfc-editor-org-rfc-rfc9111](https://www.rfc-editor.org/rfc/rfc9111.html) | standard | 100.0% | 100.0% | 7/7 | 4/4 | 65.24ms | 68.40ms | 125198 B |
| [more-rfc-editor-org-rfc-rfc9457](https://www.rfc-editor.org/rfc/rfc9457.html) | standard | 100.0% | 100.0% | 7/7 | 4/4 | 27.19ms | 27.92ms | 43761 B |
| [more-rfc-editor-org-rfc-rfc7519](https://www.rfc-editor.org/rfc/rfc7519.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 19.78ms | 21.23ms | 63487 B |
| [more-rfc-editor-org-rfc-rfc6455](https://www.rfc-editor.org/rfc/rfc6455.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 35.89ms | 37.58ms | 163129 B |
| [more-rfc-editor-org-rfc-rfc6902](https://www.rfc-editor.org/rfc/rfc6902.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 13.74ms | 16.18ms | 26662 B |
| [more-rfc-editor-org-rfc-rfc3339](https://www.rfc-editor.org/rfc/rfc3339.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 13.93ms | 15.44ms | 35318 B |
| [more-en-wikipedia-org-wiki-bicycle](https://en.wikipedia.org/wiki/Bicycle) | encyclopedia | 100.0% | 97.3% | 7/7 | 3/3 | 101.02ms | 102.90ms | 189989 B |
| [more-en-wikipedia-org-wiki-solar-system](https://en.wikipedia.org/wiki/Solar_System) | encyclopedia | 99.4% | 98.3% | 6/7 | 3/3 | 205.96ms | 212.15ms | 424720 B |
| [more-en-wikipedia-org-wiki-black-hole](https://en.wikipedia.org/wiki/Black_hole) | encyclopedia | 99.4% | 98.7% | 7/8 | 4/4 | 173.88ms | 175.00ms | 355883 B |
| [more-en-wikipedia-org-wiki-ada-lovelace](https://en.wikipedia.org/wiki/Ada_Lovelace) | encyclopedia | 99.8% | 98.4% | 6/7 | 3/3 | 89.23ms | 92.09ms | 171941 B |
| [more-en-wikipedia-org-wiki-fermentation](https://en.wikipedia.org/wiki/Fermentation) | encyclopedia | 99.6% | 96.5% | 7/7 | 3/3 | 59.90ms | 61.72ms | 108272 B |
| [more-en-wikipedia-org-wiki-silk-road](https://en.wikipedia.org/wiki/Silk_Road) | encyclopedia | 99.9% | 98.6% | 7/7 | 3/3 | 127.84ms | 128.60ms | 245686 B |
| [more-en-wikipedia-org-wiki-fibonacci-sequence](https://en.wikipedia.org/wiki/Fibonacci_sequence) | encyclopedia | 83.7% | 98.5% | 6/7 | 2/3 | 108.10ms | 112.83ms | 152574 B |
| [more-en-wikipedia-org-wiki-coral-reef](https://en.wikipedia.org/wiki/Coral_reef) | encyclopedia | 100.0% | 99.2% | 7/7 | 3/3 | 152.52ms | 158.71ms | 306584 B |
| [more-science-nasa-gov-mercury-facts](https://science.nasa.gov/mercury/facts/) | science | 98.4% | 98.7% | 6/6 | 3/3 | 18.36ms | 19.95ms | 7492 B |
| [more-science-nasa-gov-venus-venus-facts](https://science.nasa.gov/venus/venus-facts/) | science | 99.3% | 99.4% | 7/7 | 3/3 | 22.33ms | 24.54ms | 16895 B |
| [more-science-nasa-gov-saturn-facts](https://science.nasa.gov/saturn/facts/) | science | 98.8% | 99.0% | 7/7 | 3/3 | 18.93ms | 19.60ms | 9880 B |
| [more-science-nasa-gov-uranus-facts](https://science.nasa.gov/uranus/facts/) | science | 98.4% | 98.8% | 6/6 | 3/3 | 18.62ms | 19.37ms | 7707 B |
| [more-science-nasa-gov-neptune-neptune-facts](https://science.nasa.gov/neptune/neptune-facts/) | science | 98.7% | 99.1% | 8/8 | 4/4 | 19.59ms | 20.31ms | 10194 B |
| [more-science-nasa-gov-moon-facts](https://science.nasa.gov/moon/facts/) | science | 97.9% | 99.5% | 7/7 | 3/3 | 21.16ms | 21.45ms | 12396 B |
| [more-science-nasa-gov-sun-facts](https://science.nasa.gov/sun/facts/) | science | 99.3% | 99.3% | 7/7 | 3/3 | 24.31ms | 25.62ms | 25057 B |
| [more-science-nasa-gov-asteroids-facts](https://science.nasa.gov/solar-system/asteroids/facts/) | science | 98.1% | 99.5% | 7/7 | 3/3 | 23.38ms | 24.91ms | 17187 B |
| [more-nps-gov-planyourvisit-hiking](https://www.nps.gov/yose/planyourvisit/hiking.htm) | visitor-guide | 100.0% | 94.2% | 6/6 | 3/3 | 10.27ms | 10.82ms | 2364 B |
| [more-nps-gov-planyourvisit-hiking-088184](https://www.nps.gov/grsm/planyourvisit/hiking.htm) | visitor-guide | 100.0% | 97.1% | 7/7 | 3/3 | 12.12ms | 12.85ms | 6941 B |
| [more-nps-gov-planyourvisit-hiking-36d538](https://www.nps.gov/acad/planyourvisit/hiking.htm) | visitor-guide | 99.7% | 97.2% | 7/7 | 3/3 | 13.40ms | 14.47ms | 8345 B |
| [more-nps-gov-planyourvisit-safety](https://www.nps.gov/zion/planyourvisit/safety.htm) | visitor-guide | 100.0% | 93.6% | 5/5 | 3/3 | 12.70ms | 13.22ms | 10458 B |
| [more-nps-gov-planyourvisit-wilderness-safety](https://www.nps.gov/zion/planyourvisit/wilderness-safety.htm) | visitor-guide | 99.6% | 97.1% | 7/7 | 3/3 | 15.38ms | 16.88ms | 18232 B |
| [more-nps-gov-planyourvisit-safety-297315](https://www.nps.gov/grte/planyourvisit/safety.htm) | visitor-guide | 100.0% | 97.7% | 7/7 | 3/3 | 17.98ms | 18.81ms | 19958 B |
| [more-nps-gov-planyourvisit-winter-safety](https://www.nps.gov/yell/planyourvisit/winter-safety.htm) | visitor-guide | 94.4% | 96.9% | 6/7 | 2/3 | 11.04ms | 11.91ms | 5288 B |
| [more-nps-gov-planyourvisit-bearsafety](https://www.nps.gov/grte/planyourvisit/bearsafety.htm) | visitor-guide | 100.0% | 97.0% | 7/7 | 3/3 | 13.49ms | 14.63ms | 12038 B |
| [more-pandas-pydata-org-user-guide-groupby](https://pandas.pydata.org/docs/user_guide/groupby.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 49.34ms | 49.75ms | 88293 B |
| [more-pandas-pydata-org-user-guide-missing-data](https://pandas.pydata.org/docs/user_guide/missing_data.html) | api | 100.0% | 99.9% | 8/8 | 4/4 | 26.54ms | 27.03ms | 34736 B |
| [more-pandas-pydata-org-user-guide-reshaping](https://pandas.pydata.org/docs/user_guide/reshaping.html) | api | 100.0% | 99.9% | 8/8 | 4/4 | 28.20ms | 32.61ms | 47680 B |
| [more-pandas-pydata-org-user-guide-categorical](https://pandas.pydata.org/docs/user_guide/categorical.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 36.56ms | 37.47ms | 49283 B |
| [more-pandas-pydata-org-user-guide-timeseries](https://pandas.pydata.org/docs/user_guide/timeseries.html) | api | 100.0% | 100.0% | 9/9 | 5/5 | 77.67ms | 78.40ms | 152395 B |
| [more-pandas-pydata-org-user-guide-text](https://pandas.pydata.org/docs/user_guide/text.html) | api | 100.0% | 99.9% | 9/9 | 5/5 | 34.60ms | 37.57ms | 44547 B |
| [more-pandas-pydata-org-user-guide-duplicates](https://pandas.pydata.org/docs/user_guide/duplicates.html) | api | 100.0% | 99.9% | 8/8 | 4/4 | 15.70ms | 15.99ms | 14885 B |
| [more-pandas-pydata-org-user-guide-options](https://pandas.pydata.org/docs/user_guide/options.html) | api | 100.0% | 99.9% | 8/8 | 4/4 | 21.77ms | 22.26ms | 38379 B |
| [more-docs-djangoproject-com-db-models](https://docs.djangoproject.com/en/5.2/topics/db/models/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 40.00ms | 41.49ms | 75855 B |
| [more-docs-djangoproject-com-db-aggregation](https://docs.djangoproject.com/en/5.2/topics/db/aggregation/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 22.53ms | 27.32ms | 25666 B |
| [more-docs-djangoproject-com-http-views](https://docs.djangoproject.com/en/5.2/topics/http/views/) | docs | 100.0% | 99.7% | 8/8 | 4/4 | 14.05ms | 15.32ms | 10241 B |
| [more-docs-djangoproject-com-http-urls](https://docs.djangoproject.com/en/5.2/topics/http/urls/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 25.82ms | 26.83ms | 36634 B |
| [more-docs-djangoproject-com-topics-forms](https://docs.djangoproject.com/en/5.2/topics/forms/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 26.77ms | 27.24ms | 37874 B |
| [more-docs-djangoproject-com-auth-default](https://docs.djangoproject.com/en/5.2/topics/auth/default/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 52.60ms | 55.25ms | 100806 B |
| [more-docs-djangoproject-com-topics-cache](https://docs.djangoproject.com/en/5.2/topics/cache/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 39.38ms | 39.66ms | 63122 B |
| [more-docs-djangoproject-com-testing-overview](https://docs.djangoproject.com/en/5.2/topics/testing/overview/) | docs | 100.0% | 99.9% | 8/8 | 4/4 | 16.91ms | 17.29ms | 19281 B |
| [more-curl-se-docs-http-cookies](https://curl.se/docs/http-cookies.html) | manual | 99.3% | 100.0% | 7/7 | 3/3 | 10.18ms | 10.69ms | 6885 B |
| [more-curl-se-docs-sslcerts](https://curl.se/docs/sslcerts.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 10.19ms | 10.44ms | 6249 B |
| [more-curl-se-docs-alt-svc](https://curl.se/docs/alt-svc.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 7.67ms | 9.04ms | 1325 B |
| [more-curl-se-docs-hsts](https://curl.se/docs/hsts.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 8.60ms | 9.11ms | 1546 B |
| [more-curl-se-docs-http3](https://curl.se/docs/http3.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 12.27ms | 12.93ms | 11931 B |
| [more-curl-se-docs-url-syntax](https://curl.se/docs/url-syntax.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 13.78ms | 14.10ms | 15432 B |
| [more-curl-se-docs-ssl-ciphers](https://curl.se/docs/ssl-ciphers.html) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 12.19ms | 13.41ms | 11375 B |
| [more-curl-se-docs-ssl-compared](https://curl.se/docs/ssl-compared.html) | manual | 100.0% | 100.0% | 7/7 | 3/3 | 11.91ms | 12.94ms | 8231 B |
| [more-w3-org-images-decorative](https://www.w3.org/WAI/tutorials/images/decorative/) | accessibility | 100.0% | 95.4% | 8/8 | 4/4 | 11.24ms | 12.14ms | 5659 B |
| [more-w3-org-images-informative](https://www.w3.org/WAI/tutorials/images/informative/) | accessibility | 100.0% | 96.4% | 8/8 | 4/4 | 12.07ms | 12.93ms | 7736 B |
| [more-w3-org-images-functional](https://www.w3.org/WAI/tutorials/images/functional/) | accessibility | 99.6% | 95.5% | 8/8 | 4/4 | 11.26ms | 11.89ms | 5690 B |
| [more-w3-org-images-complex](https://www.w3.org/WAI/tutorials/images/complex/) | accessibility | 100.0% | 97.5% | 8/8 | 4/4 | 12.31ms | 12.77ms | 9399 B |
| [more-w3-org-forms-labels](https://www.w3.org/WAI/tutorials/forms/labels/) | accessibility | 99.3% | 97.8% | 8/8 | 4/4 | 14.30ms | 16.88ms | 10896 B |
| [more-w3-org-forms-instructions](https://www.w3.org/WAI/tutorials/forms/instructions/) | accessibility | 98.8% | 97.0% | 8/8 | 4/4 | 12.59ms | 13.59ms | 8688 B |
| [more-w3-org-tables-multi-level](https://www.w3.org/WAI/tutorials/tables/multi-level/) | accessibility | 100.0% | 95.8% | 8/8 | 4/4 | 12.79ms | 13.43ms | 6095 B |
| [more-w3-org-page-structure-headings](https://www.w3.org/WAI/tutorials/page-structure/headings/) | accessibility | 100.0% | 93.7% | 7/7 | 3/3 | 10.27ms | 11.30ms | 5654 B |
| [more-ourworldindata-org-co2-emissions](https://ourworldindata.org/co2-emissions) | data-article | 100.0% | 99.9% | 8/8 | 4/4 | 21.75ms | 22.19ms | 29221 B |
| [more-ourworldindata-org-energy-mix](https://ourworldindata.org/energy-mix) | data-article | 100.0% | 100.0% | 8/8 | 4/4 | 17.19ms | 17.76ms | 19258 B |
| [more-ourworldindata-org-plastic-pollution](https://ourworldindata.org/plastic-pollution) | data-article | 100.0% | 100.0% | 6/8 | 4/4 | 20.26ms | 21.09ms | 22910 B |
| [more-ourworldindata-org-literacy](https://ourworldindata.org/literacy) | data-article | 100.0% | 100.0% | 8/8 | 4/4 | 20.51ms | 21.49ms | 27696 B |
| [more-ourworldindata-org-economic-growth](https://ourworldindata.org/economic-growth) | data-article | 99.2% | 100.0% | 7/8 | 4/4 | 22.14ms | 22.78ms | 27182 B |
| [more-ourworldindata-org-child-mortality](https://ourworldindata.org/child-mortality) | data-article | 99.7% | 100.0% | 7/8 | 4/4 | 26.38ms | 28.35ms | 36356 B |
| [more-ourworldindata-org-vaccination](https://ourworldindata.org/vaccination) | data-article | 100.0% | 100.0% | 7/8 | 4/4 | 30.29ms | 31.39ms | 53206 B |
| [more-ourworldindata-org-renewable-energy](https://ourworldindata.org/renewable-energy) | data-article | 100.0% | 99.9% | 8/8 | 4/4 | 14.42ms | 14.73ms | 11992 B |
| [more-ssd-eff-org-module-how-to-use-signal](https://ssd.eff.org/module/how-to-use-signal) | guide | 98.1% | 100.0% | 7/7 | 3/3 | 25.99ms | 26.34ms | 36855 B |
| [more-ssd-eff-org-module-how-to-use-tor](https://ssd.eff.org/module/how-to-use-tor) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 14.62ms | 15.16ms | 13928 B |
| [more-ssd-eff-org-module-how-encrypt-your-windows-device](https://ssd.eff.org/module/how-encrypt-your-windows-device) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 13.57ms | 14.43ms | 13384 B |
| [more-ssd-eff-org-module-how-enable-two-factor-authentication](https://ssd.eff.org/module/how-enable-two-factor-authentication) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 14.10ms | 14.63ms | 14665 B |
| [more-ssd-eff-org-module-what-should-i-know-about-encryption](https://ssd.eff.org/module/what-should-i-know-about-encryption) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 16.17ms | 17.58ms | 17974 B |
| [more-ssd-eff-org-module-choosing-vpn-thats-right-you](https://ssd.eff.org/module/choosing-vpn-thats-right-you) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 17.29ms | 20.45ms | 18203 B |
| [more-ssd-eff-org-module-keeping-your-data-safe](https://ssd.eff.org/module/keeping-your-data-safe) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 12.52ms | 12.82ms | 9895 B |
| [more-ssd-eff-org-module-protecting-yourself-social-networks](https://ssd.eff.org/module/protecting-yourself-social-networks) | guide | 100.0% | 100.0% | 7/7 | 3/3 | 12.33ms | 14.14ms | 10202 B |
| [more-paulgraham-com-hs](https://www.paulgraham.com/hs.html) | essay | 99.8% | 100.0% | 3/4 | 3/3 | 17.05ms | 18.15ms | 28028 B |
| [more-paulgraham-com-greatwork](https://www.paulgraham.com/greatwork.html) | essay | 100.0% | 100.0% | 3/3 | 3/3 | 32.15ms | 35.21ms | 69852 B |
| [more-paulgraham-com-startupideas](https://www.paulgraham.com/startupideas.html) | essay | 99.9% | 100.0% | 3/4 | 3/3 | 22.31ms | 23.19ms | 42432 B |
| [more-paulgraham-com-procrastination](https://www.paulgraham.com/procrastination.html) | essay | 99.2% | 100.0% | 3/4 | 3/3 | 11.88ms | 12.60ms | 10244 B |
| [more-paulgraham-com-nerds](https://www.paulgraham.com/nerds.html) | essay | 99.6% | 100.0% | 3/4 | 3/3 | 18.30ms | 18.97ms | 32014 B |
| [more-paulgraham-com-wealth](https://www.paulgraham.com/wealth.html) | essay | 99.8% | 100.0% | 3/4 | 3/3 | 24.01ms | 24.72ms | 51543 B |
| [more-paulgraham-com-cities](https://www.paulgraham.com/cities.html) | essay | 99.8% | 100.0% | 3/4 | 3/3 | 14.53ms | 15.31ms | 20764 B |
| [more-paulgraham-com-good](https://www.paulgraham.com/good.html) | essay | 99.9% | 100.0% | 4/4 | 3/3 | 13.41ms | 14.10ms | 17092 B |
| [more-martinfowler-com-articles-feature-toggles](https://martinfowler.com/articles/feature-toggles.html) | essay | 100.0% | 99.1% | 8/8 | 4/4 | 22.12ms | 22.33ms | 51536 B |
| [more-martinfowler-com-articles-injection](https://martinfowler.com/articles/injection.html) | essay | 99.9% | 100.0% | 8/8 | 4/4 | 19.54ms | 20.69ms | 42398 B |
| [more-martinfowler-com-bliki-stranglerfigapplication](https://martinfowler.com/bliki/StranglerFigApplication.html) | essay | 100.0% | 91.0% | 6/6 | 3/3 | 8.16ms | 10.27ms | 7438 B |
| [more-martinfowler-com-bliki-monolithfirst](https://martinfowler.com/bliki/MonolithFirst.html) | essay | 100.0% | 85.8% | 7/7 | 3/3 | 9.92ms | 10.47ms | 8665 B |
| [more-martinfowler-com-bliki-twohardthings](https://martinfowler.com/bliki/TwoHardThings.html) | essay | 100.0% | 100.0% | 7/7 | 3/3 | 7.39ms | 9.47ms | 2048 B |
| [more-martinfowler-com-bliki-cqrs](https://martinfowler.com/bliki/CQRS.html) | essay | 99.3% | 100.0% | 7/7 | 3/3 | 9.70ms | 11.02ms | 8639 B |
| [more-martinfowler-com-articles-practical-test-pyramid](https://martinfowler.com/articles/practical-test-pyramid.html) | essay | 100.0% | 99.4% | 8/8 | 4/4 | 31.74ms | 33.88ms | 85421 B |
| [more-martinfowler-com-bliki-boundedcontext](https://martinfowler.com/bliki/BoundedContext.html) | essay | 99.1% | 99.4% | 7/7 | 3/3 | 9.08ms | 9.28ms | 5663 B |
| [more-letsencrypt-org-docs-rate-limits](https://letsencrypt.org/docs/rate-limits/) | docs | 100.0% | 100.0% | 9/9 | 5/5 | 13.55ms | 14.00ms | 15102 B |
| [more-letsencrypt-org-docs-staging-environment](https://letsencrypt.org/docs/staging-environment/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 11.29ms | 12.86ms | 9308 B |
| [more-letsencrypt-org-docs-faq](https://letsencrypt.org/docs/faq/) | docs | 100.0% | 100.0% | 7/7 | 3/3 | 10.95ms | 11.32ms | 8418 B |
| [more-letsencrypt-org-docs-integration-guide](https://letsencrypt.org/docs/integration-guide/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 11.61ms | 12.23ms | 13185 B |
| [more-letsencrypt-org-docs-account-id](https://letsencrypt.org/docs/account-id/) | docs | 100.0% | 100.0% | 5/5 | 3/3 | 7.18ms | 8.10ms | 1377 B |
| [more-letsencrypt-org-docs-ipv6-support](https://letsencrypt.org/docs/ipv6-support/) | docs | 100.0% | 100.0% | 7/7 | 3/3 | 8.78ms | 9.99ms | 2820 B |
| [more-letsencrypt-org-docs-revoking](https://letsencrypt.org/docs/revoking/) | docs | 100.0% | 100.0% | 6/6 | 4/4 | 10.82ms | 11.39ms | 6944 B |
| [more-letsencrypt-org-docs-ct-logs](https://letsencrypt.org/docs/ct-logs/) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 10.21ms | 10.73ms | 5983 B |
| [more-oceanservice-noaa-gov-facts-tides](https://oceanservice.noaa.gov/facts/tides.html) | science | 97.9% | 82.3% | 5/6 | 3/3 | 10.58ms | 11.31ms | 1921 B |
| [more-oceanservice-noaa-gov-facts-current](https://oceanservice.noaa.gov/facts/current.html) | science | 94.5% | 94.8% | 5/6 | 2/3 | 10.49ms | 11.05ms | 2887 B |
| [more-oceanservice-noaa-gov-facts-coral](https://oceanservice.noaa.gov/facts/coral.html) | science | 100.0% | 96.3% | 4/4 | 3/3 | 10.90ms | 11.74ms | 3868 B |
| [more-oceanservice-noaa-gov-facts-acidification](https://oceanservice.noaa.gov/facts/acidification.html) | science | 98.6% | 84.9% | 4/5 | 3/3 | 10.94ms | 12.95ms | 2414 B |
| [more-oceanservice-noaa-gov-facts-tsunami](https://oceanservice.noaa.gov/facts/tsunami.html) | science | 100.0% | 90.0% | 4/4 | 2/2 | 10.18ms | 10.68ms | 2076 B |
| [more-oceanservice-noaa-gov-facts-estuary](https://oceanservice.noaa.gov/facts/estuary.html) | science | 96.3% | 91.0% | 5/7 | 2/3 | 10.88ms | 11.25ms | 4015 B |
| [more-oceanservice-noaa-gov-facts-mangroves](https://oceanservice.noaa.gov/facts/mangroves.html) | science | 100.0% | 89.8% | 5/5 | 3/3 | 10.47ms | 11.34ms | 2232 B |
| [more-oceanservice-noaa-gov-facts-kelp](https://oceanservice.noaa.gov/facts/kelp.html) | science | 99.4% | 82.2% | 5/6 | 3/3 | 10.91ms | 11.43ms | 3266 B |
| [more-medlineplus-gov-article-000076](https://medlineplus.gov/ency/article/000076.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 12.88ms | 14.67ms | 10244 B |
| [more-medlineplus-gov-article-000468](https://medlineplus.gov/ency/article/000468.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 15.50ms | 17.19ms | 17003 B |
| [more-medlineplus-gov-article-000279](https://medlineplus.gov/ency/article/000279.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 12.58ms | 13.06ms | 10737 B |
| [more-medlineplus-gov-article-000313](https://medlineplus.gov/ency/article/000313.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 18.44ms | 19.77ms | 26112 B |
| [more-medlineplus-gov-article-000545](https://medlineplus.gov/ency/article/000545.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.29ms | 11.28ms | 3584 B |
| [more-medlineplus-gov-article-000639](https://medlineplus.gov/ency/article/000639.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 11.40ms | 13.50ms | 6727 B |
| [more-medlineplus-gov-article-000158](https://medlineplus.gov/ency/article/000158.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 14.07ms | 15.09ms | 14910 B |
| [more-medlineplus-gov-article-000285](https://medlineplus.gov/ency/article/000285.htm) | health | 100.0% | 100.0% | 7/7 | 3/3 | 11.29ms | 12.17ms | 6548 B |
| [more-who-int-detail-malaria](https://www.who.int/news-room/fact-sheets/detail/malaria) | health | 100.0% | 99.6% | 7/7 | 3/3 | 15.59ms | 15.72ms | 14211 B |
| [more-who-int-detail-tuberculosis](https://www.who.int/news-room/fact-sheets/detail/tuberculosis) | health | 100.0% | 97.0% | 6/6 | 3/3 | 13.83ms | 14.97ms | 11445 B |
| [more-who-int-detail-diabetes](https://www.who.int/news-room/fact-sheets/detail/diabetes) | health | 100.0% | 99.3% | 7/7 | 3/3 | 14.70ms | 15.27ms | 8423 B |
| [more-who-int-detail-asthma](https://www.who.int/news-room/fact-sheets/detail/asthma) | health | 100.0% | 99.2% | 7/7 | 3/3 | 13.74ms | 15.63ms | 7895 B |
| [more-who-int-detail-hypertension](https://www.who.int/news-room/fact-sheets/detail/hypertension) | health | 100.0% | 99.4% | 7/7 | 3/3 | 15.09ms | 17.15ms | 9562 B |
| [more-who-int-detail-dengue-and-severe-dengue](https://www.who.int/news-room/fact-sheets/detail/dengue-and-severe-dengue) | health | 100.0% | 98.6% | 7/7 | 3/3 | 15.55ms | 17.39ms | 12305 B |
| [more-who-int-detail-measles](https://www.who.int/news-room/fact-sheets/detail/measles) | health | 100.0% | 99.4% | 7/7 | 3/3 | 13.68ms | 14.80ms | 10107 B |
| [more-who-int-detail-physical-activity](https://www.who.int/news-room/fact-sheets/detail/physical-activity) | health | 100.0% | 99.7% | 7/7 | 3/3 | 15.39ms | 15.95ms | 10787 B |
| [more-un-org-science-causes-effects-climate-change](https://www.un.org/en/climatechange/science/causes-effects-climate-change) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 25.08ms | 26.29ms | 9887 B |
| [more-un-org-climatechange-what-is-renewable-energy](https://www.un.org/en/climatechange/what-is-renewable-energy) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 12.56ms | 13.95ms | 8761 B |
| [more-un-org-raising-ambition-renewable-energy](https://www.un.org/en/climatechange/raising-ambition/renewable-energy) | explainer | 87.6% | 100.0% | 5/7 | 2/3 | 24.92ms | 26.94ms | 13259 B |
| [more-un-org-climate-issues-greenwashing](https://www.un.org/en/climatechange/science/climate-issues/greenwashing) | explainer | 79.7% | 100.0% | 5/7 | 2/3 | 24.93ms | 26.15ms | 10387 B |
| [more-un-org-climate-issues-food](https://www.un.org/en/climatechange/science/climate-issues/food) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 25.99ms | 26.69ms | 12298 B |
| [more-un-org-climatechange-climate-adaptation](https://www.un.org/en/climatechange/climate-adaptation) | explainer | 100.0% | 100.0% | 7/7 | 3/3 | 24.89ms | 25.68ms | 11119 B |
| [more-un-org-climatechange-net-zero-coalition](https://www.un.org/en/climatechange/net-zero-coalition) | explainer | 87.2% | 100.0% | 6/7 | 2/3 | 27.51ms | 29.66ms | 7924 B |
| [more-un-org-climatechange-paris-agreement](https://www.un.org/en/climatechange/paris-agreement) | explainer | 100.0% | 98.1% | 7/7 | 3/3 | 23.91ms | 25.46ms | 6825 B |
| [more-computerhistory-org-timeline-1940](https://www.computerhistory.org/timeline/1940/) | history | 98.8% | 83.3% | 4/4 | 1/1 | 15.59ms | 15.95ms | 1047 B |
| [more-computerhistory-org-timeline-1946](https://www.computerhistory.org/timeline/1946/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 12.95ms | 14.46ms | 4474 B |
| [more-computerhistory-org-timeline-1951](https://www.computerhistory.org/timeline/1951/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 14.95ms | 15.82ms | 7652 B |
| [more-computerhistory-org-timeline-1956](https://www.computerhistory.org/timeline/1956/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 13.61ms | 14.29ms | 5641 B |
| [more-computerhistory-org-timeline-1964](https://www.computerhistory.org/timeline/1964/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 16.60ms | 17.77ms | 12589 B |
| [more-computerhistory-org-timeline-1971](https://www.computerhistory.org/timeline/1971/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 15.64ms | 16.08ms | 10578 B |
| [more-computerhistory-org-timeline-1984](https://www.computerhistory.org/timeline/1984/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 16.89ms | 17.63ms | 12694 B |
| [more-computerhistory-org-timeline-1991](https://www.computerhistory.org/timeline/1991/) | history | 100.0% | 100.0% | 7/7 | 3/3 | 14.27ms | 14.83ms | 7430 B |
| [more-nhm-ac-uk-discover-what-is-biodiversity](https://www.nhm.ac.uk/discover/what-is-biodiversity.html) | science | 99.9% | 85.6% | 7/7 | 3/3 | 20.00ms | 21.48ms | 14093 B |
| [more-nhm-ac-uk-discover-insect-pollination](https://www.nhm.ac.uk/discover/insect-pollination.html) | science | 99.9% | 94.0% | 7/7 | 3/3 | 20.69ms | 21.39ms | 15974 B |
| [more-nhm-ac-uk-discover-how-are-fossils-formed](https://www.nhm.ac.uk/discover/how-are-fossils-formed.html) | science | 99.9% | 74.6% | 7/7 | 3/3 | 19.96ms | 20.54ms | 11616 B |
| [more-nhm-ac-uk-discover-meet-the-monsters-of-the-jurassic-seas](https://www.nhm.ac.uk/discover/meet-the-monsters-of-the-jurassic-seas.html) | science | 99.9% | 83.2% | 7/7 | 3/3 | 18.52ms | 19.22ms | 10363 B |
| [more-nhm-ac-uk-discover-what-is-natural-selection](https://www.nhm.ac.uk/discover/what-is-natural-selection.html) | science | 98.5% | 87.7% | 7/7 | 3/3 | 21.96ms | 22.92ms | 15484 B |
| [more-nhm-ac-uk-discover-convergent-evolution](https://www.nhm.ac.uk/discover/convergent-evolution.html) | science | 99.3% | 90.8% | 7/7 | 3/3 | 29.82ms | 31.44ms | 26949 B |
| [more-nhm-ac-uk-discover-dinosaur-extinction](https://www.nhm.ac.uk/discover/dinosaur-extinction.html) | science | 99.0% | 52.8% | 7/7 | 3/3 | 16.43ms | 16.84ms | 5327 B |
| [more-nhm-ac-uk-discover-what-is-climate-change-why-does-it-matter](https://www.nhm.ac.uk/discover/what-is-climate-change-why-does-it-matter.html) | science | 98.0% | 92.4% | 7/7 | 3/3 | 27.24ms | 27.91ms | 25863 B |
| [more-docs-julialang-org-manual-variables](https://docs.julialang.org/en/v1/manual/variables/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 12.82ms | 13.18ms | 10201 B |
| [more-docs-julialang-org-manual-integers-and-floating-point-numbers](https://docs.julialang.org/en/v1/manual/integers-and-floating-point-numbers/) | manual | 100.0% | 100.0% | 9/9 | 5/5 | 20.50ms | 21.50ms | 29103 B |
| [more-docs-julialang-org-manual-strings](https://docs.julialang.org/en/v1/manual/strings/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 26.24ms | 27.95ms | 48647 B |
| [more-docs-julialang-org-manual-functions](https://docs.julialang.org/en/v1/manual/functions/) | manual | 100.0% | 100.0% | 9/9 | 5/5 | 24.94ms | 26.77ms | 43241 B |
| [more-docs-julialang-org-manual-control-flow](https://docs.julialang.org/en/v1/manual/control-flow/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 21.08ms | 21.87ms | 32499 B |
| [more-docs-julialang-org-manual-types](https://docs.julialang.org/en/v1/manual/types/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 35.39ms | 36.90ms | 74679 B |
| [more-docs-julialang-org-manual-methods](https://docs.julialang.org/en/v1/manual/methods/) | manual | 100.0% | 100.0% | 8/8 | 4/4 | 25.17ms | 26.57ms | 47728 B |
| [more-docs-julialang-org-manual-performance-tips](https://docs.julialang.org/en/v1/manual/performance-tips/) | manual | 98.1% | 100.0% | 8/8 | 4/4 | 37.91ms | 39.75ms | 80300 B |
| [more-elixir-hexdocs-pm-enum](https://elixir.hexdocs.pm/Enum.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 82.48ms | 84.63ms | 91153 B |
| [more-elixir-hexdocs-pm-map](https://elixir.hexdocs.pm/Map.html) | api | 100.0% | 100.0% | 7/7 | 4/4 | 35.91ms | 38.44ms | 31897 B |
| [more-elixir-hexdocs-pm-string](https://elixir.hexdocs.pm/String.html) | api | 100.0% | 100.0% | 7/7 | 4/4 | 56.63ms | 59.15ms | 68062 B |
| [more-elixir-hexdocs-pm-list](https://elixir.hexdocs.pm/List.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 37.23ms | 39.15ms | 32209 B |
| [more-elixir-hexdocs-pm-genserver](https://elixir.hexdocs.pm/GenServer.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 37.62ms | 38.97ms | 52327 B |
| [more-elixir-hexdocs-pm-task](https://elixir.hexdocs.pm/Task.html) | api | 100.0% | 100.0% | 6/7 | 4/4 | 32.54ms | 33.91ms | 43517 B |
| [more-elixir-hexdocs-pm-introduction](https://elixir.hexdocs.pm/introduction.html) | api | 100.0% | 99.5% | 7/7 | 4/4 | 8.93ms | 9.82ms | 2694 B |
| [more-elixir-hexdocs-pm-pattern-matching](https://elixir.hexdocs.pm/pattern-matching.html) | api | 100.0% | 99.8% | 7/7 | 4/4 | 11.13ms | 11.34ms | 5853 B |
| [more-ffmpeg-org-ffmpeg](https://ffmpeg.org/ffmpeg.html) | manual | 99.1% | 100.0% | 6/8 | 4/4 | 96.57ms | 97.30ms | 148142 B |
| [more-ffmpeg-org-ffmpeg-formats](https://ffmpeg.org/ffmpeg-formats.html) | manual | 97.2% | 100.0% | 6/8 | 4/4 | 140.17ms | 143.07ms | 196445 B |
| [more-ffmpeg-org-ffmpeg-codecs](https://ffmpeg.org/ffmpeg-codecs.html) | manual | 96.7% | 100.0% | 6/8 | 4/4 | 151.17ms | 153.67ms | 177082 B |
| [more-ffmpeg-org-ffmpeg-protocols](https://ffmpeg.org/ffmpeg-protocols.html) | manual | 98.1% | 100.0% | 6/8 | 4/4 | 61.46ms | 62.67ms | 78009 B |
| [more-ffmpeg-org-ffmpeg-utils](https://ffmpeg.org/ffmpeg-utils.html) | manual | 98.2% | 100.0% | 6/8 | 4/4 | 35.10ms | 36.53ms | 24138 B |
| [more-ffmpeg-org-ffplay](https://ffmpeg.org/ffplay.html) | manual | 98.7% | 100.0% | 6/8 | 4/4 | 25.66ms | 30.07ms | 23634 B |
| [more-ffmpeg-org-ffprobe](https://ffmpeg.org/ffprobe.html) | manual | 98.8% | 100.0% | 6/8 | 4/4 | 33.79ms | 35.56ms | 34178 B |
| [more-ffmpeg-org-faq](https://ffmpeg.org/faq.html) | manual | 86.4% | 100.0% | 6/8 | 4/4 | 25.59ms | 27.51ms | 24930 B |
| [more-angular-dev-guide-components](https://angular.dev/guide/components) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 18.30ms | 19.03ms | 4965 B |
| [more-angular-dev-components-inputs](https://angular.dev/guide/components/inputs) | docs | 100.0% | 100.0% | 7/8 | 4/4 | 21.38ms | 22.01ms | 14306 B |
| [more-angular-dev-guide-templates](https://angular.dev/guide/templates) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 13.13ms | 14.13ms | 4240 B |
| [more-angular-dev-guide-di](https://angular.dev/guide/di) | docs | 100.0% | 100.0% | 8/8 | 4/4 | 17.91ms | 18.14ms | 7055 B |
| [more-angular-dev-guide-routing](https://angular.dev/guide/routing) | docs | 100.0% | 100.0% | 7/7 | 3/3 | 13.00ms | 14.06ms | 1879 B |
| [more-angular-dev-guide-forms](https://angular.dev/guide/forms) | docs | 100.0% | 100.0% | 8/9 | 5/5 | 29.38ms | 31.77ms | 18011 B |
| [more-angular-dev-forms-typed-forms](https://angular.dev/guide/forms/typed-forms) | docs | 100.0% | 100.0% | 7/8 | 4/4 | 18.90ms | 19.30ms | 8904 B |
| [more-angular-dev-guide-http](https://angular.dev/guide/http) | docs | 96.6% | 100.0% | 5/6 | 2/2 | 11.00ms | 11.75ms | 965 B |
| [more-react-dev-learn-describing-the-ui](https://react.dev/learn/describing-the-ui) | tutorial | 99.3% | 98.6% | 7/8 | 4/4 | 24.40ms | 25.50ms | 14229 B |
| [more-react-dev-learn-writing-markup-with-jsx](https://react.dev/learn/writing-markup-with-jsx) | tutorial | 99.7% | 99.5% | 8/8 | 4/4 | 19.96ms | 21.07ms | 10568 B |
| [more-react-dev-learn-conditional-rendering](https://react.dev/learn/conditional-rendering) | tutorial | 99.5% | 99.0% | 7/8 | 4/4 | 25.29ms | 28.83ms | 13449 B |
| [more-react-dev-learn-rendering-lists](https://react.dev/learn/rendering-lists) | tutorial | 99.8% | 99.5% | 8/8 | 4/4 | 23.32ms | 24.69ms | 12288 B |
| [more-react-dev-learn-responding-to-events](https://react.dev/learn/responding-to-events) | tutorial | 99.5% | 99.0% | 8/9 | 5/5 | 28.45ms | 30.79ms | 17321 B |
| [more-react-dev-learn-updating-objects-in-state](https://react.dev/learn/updating-objects-in-state) | tutorial | 99.7% | 99.4% | 8/8 | 4/4 | 31.28ms | 31.89ms | 21324 B |
| [more-react-dev-learn-synchronizing-with-effects](https://react.dev/learn/synchronizing-with-effects) | tutorial | 99.9% | 99.7% | 8/8 | 4/4 | 45.68ms | 45.81ms | 44113 B |
| [more-react-dev-learn-you-might-not-need-an-effect](https://react.dev/learn/you-might-not-need-an-effect) | tutorial | 100.0% | 100.0% | 8/8 | 4/4 | 37.88ms | 40.65ms | 40449 B |
| [more-nmap-org-book-man-examples](https://nmap.org/book/man-examples.html) | manual | 100.0% | 100.0% | 6/6 | 3/3 | 7.60ms | 8.38ms | 2593 B |
| [more-nmap-org-book-man-port-scanning-basics](https://nmap.org/book/man-port-scanning-basics.html) | manual | 100.0% | 100.0% | 6/6 | 3/3 | 8.71ms | 9.12ms | 4212 B |
| [more-nmap-org-book-man-host-discovery](https://nmap.org/book/man-host-discovery.html) | manual | 100.0% | 100.0% | 6/6 | 3/3 | 19.26ms | 20.13ms | 23462 B |
| [more-nmap-org-book-man-port-scanning-techniques](https://nmap.org/book/man-port-scanning-techniques.html) | manual | 100.0% | 100.0% | 6/6 | 3/3 | 20.75ms | 21.18ms | 24937 B |
| [more-nmap-org-book-man-version-detection](https://nmap.org/book/man-version-detection.html) | manual | 100.0% | 100.0% | 6/6 | 3/3 | 10.27ms | 11.06ms | 6802 B |
| [more-nmap-org-book-man-os-detection](https://nmap.org/book/man-os-detection.html) | manual | 100.0% | 100.0% | 6/6 | 3/3 | 9.95ms | 11.48ms | 5090 B |
| [more-nmap-org-book-man-nse](https://nmap.org/book/man-nse.html) | manual | 100.0% | 100.0% | 6/6 | 3/3 | 12.53ms | 13.15ms | 9808 B |
| [more-nmap-org-book-man-performance](https://nmap.org/book/man-performance.html) | manual | 100.0% | 100.0% | 6/6 | 3/3 | 19.40ms | 20.58ms | 22518 B |
| [more-numpy-org-user-basics-broadcasting](https://numpy.org/doc/stable/user/basics.broadcasting.html) | api | 100.0% | 99.8% | 8/8 | 4/4 | 14.56ms | 15.97ms | 12262 B |
| [more-numpy-org-user-basics-indexing](https://numpy.org/doc/stable/user/basics.indexing.html) | api | 100.0% | 99.9% | 8/8 | 4/4 | 25.36ms | 27.62ms | 34906 B |
| [more-numpy-org-user-basics-copies](https://numpy.org/doc/stable/user/basics.copies.html) | api | 100.0% | 99.6% | 8/8 | 4/4 | 11.26ms | 12.09ms | 5613 B |
| [more-numpy-org-user-basics-types](https://numpy.org/doc/stable/user/basics.types.html) | api | 100.0% | 99.9% | 9/9 | 5/5 | 19.54ms | 20.23ms | 21212 B |
| [more-docs-scipy-org-tutorial-integrate](https://docs.scipy.org/doc/scipy/tutorial/integrate.html) | tutorial | 100.0% | 99.9% | 8/8 | 4/4 | 24.14ms | 25.04ms | 34850 B |
| [more-docs-scipy-org-tutorial-optimize](https://docs.scipy.org/doc/scipy/tutorial/optimize.html) | tutorial | 100.0% | 100.0% | 9/9 | 5/5 | 53.45ms | 54.26ms | 94656 B |
| [more-docs-scipy-org-tutorial-interpolate](https://docs.scipy.org/doc/scipy/tutorial/interpolate.html) | tutorial | 100.0% | 99.4% | 6/6 | 3/3 | 14.60ms | 15.55ms | 14361 B |
| [more-docs-scipy-org-tutorial-fft](https://docs.scipy.org/doc/scipy/tutorial/fft.html) | tutorial | 99.1% | 100.0% | 8/8 | 4/4 | 30.85ms | 31.81ms | 45681 B |
| [more-docs-github-com-workflows-and-actions-workflow-syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax) | configuration | 99.6% | 100.0% | 8/9 | 5/5 | 106.14ms | 107.94ms | 173429 B |
| [more-docs-github-com-security-secure-use](https://docs.github.com/en/actions/reference/security/secure-use) | configuration | 100.0% | 99.9% | 8/8 | 4/4 | 31.14ms | 35.25ms | 38795 B |
| [more-docs-github-com-choose-what-workflows-do-run-job-variations](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/run-job-variations) | configuration | 90.8% | 99.7% | 8/8 | 4/4 | 23.75ms | 24.76ms | 9257 B |
| [more-docs-github-com-reuse-automations-reuse-workflows](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows) | configuration | 89.9% | 99.9% | 8/8 | 4/4 | 27.45ms | 30.55ms | 18154 B |
| [more-learn-microsoft-com-operators-null-coalescing-operator](https://learn.microsoft.com/en-us/dotnet/csharp/language-reference/operators/null-coalescing-operator) | docs | 100.0% | 96.5% | 8/8 | 4/4 | 12.44ms | 13.90ms | 6376 B |
| [more-learn-microsoft-com-operators-patterns](https://learn.microsoft.com/en-us/dotnet/csharp/language-reference/operators/patterns) | docs | 100.0% | 99.5% | 8/8 | 4/4 | 23.25ms | 25.53ms | 43453 B |
| [more-learn-microsoft-com-asynchronous-programming-async-return-types](https://learn.microsoft.com/en-us/dotnet/csharp/asynchronous-programming/async-return-types) | docs | 100.0% | 98.8% | 8/8 | 4/4 | 15.48ms | 16.17ms | 18149 B |
| [more-learn-microsoft-com-exceptions-exception-handling](https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/exceptions/exception-handling) | docs | 100.0% | 97.6% | 8/8 | 4/4 | 12.81ms | 13.46ms | 8197 B |
| [more-developer-chrome-com-devtools-network](https://developer.chrome.com/docs/devtools/network/) | docs | 98.7% | 96.5% | 7/7 | 3/3 | 21.29ms | 23.33ms | 14825 B |
| [more-developer-chrome-com-devtools-performance](https://developer.chrome.com/docs/devtools/performance/) | docs | 99.1% | 97.2% | 7/7 | 3/3 | 19.40ms | 20.33ms | 14617 B |
| [more-developer-chrome-com-devtools-console](https://developer.chrome.com/docs/devtools/console/) | docs | 98.2% | 91.3% | 8/8 | 4/4 | 19.22ms | 20.10ms | 5402 B |
| [more-developer-chrome-com-devtools-memory-problems](https://developer.chrome.com/docs/devtools/memory-problems/) | docs | 99.2% | 97.1% | 8/8 | 4/4 | 22.98ms | 24.53ms | 13559 B |
| [more-jvns-ca-01-a-dns-resolver-in-80-lines-of-go](https://jvns.ca/blog/2022/02/01/a-dns-resolver-in-80-lines-of-go/) | blog | 99.7% | 100.0% | 8/8 | 4/4 | 12.54ms | 13.63ms | 17402 B |
| [more-jvns-ca-05-some-blogging-myths](https://jvns.ca/blog/2023/06/05/some-blogging-myths/) | blog | 97.3% | 100.0% | 7/7 | 3/3 | 10.88ms | 12.29ms | 11146 B |
| [more-jvns-ca-01-learning-skills-you-can-practice](https://jvns.ca/blog/2018/09/01/learning-skills-you-can-practice/) | blog | 99.4% | 100.0% | 7/7 | 3/3 | 10.22ms | 10.89ms | 9295 B |
| [more-jvns-ca-10-how-does-gdb-work](https://jvns.ca/blog/2016/08/10/how-does-gdb-work/) | blog | 99.6% | 100.0% | 8/8 | 4/4 | 10.79ms | 11.46ms | 9969 B |
| [more-eli-thegreenplace-net-2020-implementing-raft-part-1-elections](https://eli.thegreenplace.net/2020/implementing-raft-part-1-elections/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 19.02ms | 21.29ms | 28936 B |
| [more-eli-thegreenplace-net-2020-implementing-raft-part-2-commands-and-log-replication](https://eli.thegreenplace.net/2020/implementing-raft-part-2-commands-and-log-replication/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 18.77ms | 20.08ms | 27620 B |
| [more-eli-thegreenplace-net-2020-implementing-raft-part-3-persistence-and-optimizations](https://eli.thegreenplace.net/2020/implementing-raft-part-3-persistence-and-optimizations/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 16.30ms | 20.45ms | 19719 B |
| [more-eli-thegreenplace-net-2023-preview-ranging-over-functions-in-go](https://eli.thegreenplace.net/2023/preview-ranging-over-functions-in-go/) | blog | 100.0% | 100.0% | 8/8 | 4/4 | 16.67ms | 17.39ms | 18809 B |
| [more-brendangregg-com-usemethod](https://www.brendangregg.com/usemethod.html) | guide | 100.0% | 100.0% | 7/7 | 4/4 | 19.07ms | 19.74ms | 26572 B |
| [more-brendangregg-com-methodology](https://www.brendangregg.com/methodology.html) | guide | 100.0% | 100.0% | 6/6 | 3/3 | 15.10ms | 15.48ms | 12285 B |
| [more-brendangregg-com-flamegraphs](https://www.brendangregg.com/flamegraphs.html) | guide | 100.0% | 100.0% | 6/6 | 3/3 | 26.82ms | 27.33ms | 45080 B |
| [more-brendangregg-com-offcpuanalysis](https://www.brendangregg.com/offcpuanalysis.html) | guide | 100.0% | 100.0% | 7/7 | 4/4 | 22.32ms | 23.27ms | 41630 B |
| [more-developer-android-com-activities-activity-lifecycle](https://developer.android.com/guide/components/activities/activity-lifecycle) | docs | 99.8% | 99.3% | 8/8 | 4/4 | 33.64ms | 34.16ms | 36415 B |
| [more-developer-android-com-background-work-services](https://developer.android.com/develop/background-work/services) | docs | 99.8% | 99.3% | 7/8 | 4/4 | 32.99ms | 36.71ms | 35110 B |
| [more-developer-android-com-background-tasks-broadcasts](https://developer.android.com/develop/background-work/background-tasks/broadcasts) | docs | 99.8% | 99.1% | 7/8 | 4/4 | 35.36ms | 36.53ms | 36564 B |
| [more-developer-android-com-manifest-manifest-intro](https://developer.android.com/guide/topics/manifest/manifest-intro) | docs | 99.6% | 98.6% | 8/9 | 5/5 | 29.71ms | 31.75ms | 18640 B |
| [more-redis-io-data-types-strings](https://redis.io/docs/latest/develop/data-types/strings/) | database | 99.0% | 97.5% | 8/8 | 4/4 | 152.95ms | 155.04ms | 104846 B |
| [more-redis-io-data-types-hashes](https://redis.io/docs/latest/develop/data-types/hashes/) | database | 99.4% | 97.7% | 9/9 | 5/5 | 383.60ms | 389.48ms | 308717 B |
| [more-redis-io-data-types-lists](https://redis.io/docs/latest/develop/data-types/lists/) | database | 99.2% | 97.2% | 8/8 | 4/4 | 736.68ms | 748.09ms | 538204 B |
| [more-redis-io-data-types-sets](https://redis.io/docs/latest/develop/data-types/sets/) | database | 99.0% | 96.8% | 8/8 | 4/4 | 353.74ms | 361.09ms | 265684 B |
| [more-nhs-uk-conditions-asthma](https://www.nhs.uk/conditions/asthma/) | health | 98.3% | 100.0% | 7/7 | 3/3 | 12.12ms | 12.41ms | 10467 B |
| [more-nhs-uk-conditions-type-2-diabetes](https://www.nhs.uk/conditions/type-2-diabetes/) | health | 100.0% | 100.0% | 3/3 | 1/1 | 7.72ms | 9.20ms | 652 B |
| [more-nhs-uk-conditions-high-blood-pressure](https://www.nhs.uk/conditions/high-blood-pressure/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 11.31ms | 11.79ms | 6904 B |
| [more-nhs-uk-conditions-dehydration](https://www.nhs.uk/conditions/dehydration/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.84ms | 11.63ms | 5225 B |
| [more-epa-gov-recycle-recycling-basics-and-benefits](https://www.epa.gov/recycle/recycling-basics-and-benefits) | science | 98.6% | 100.0% | 7/7 | 3/3 | 12.64ms | 12.96ms | 10635 B |
| [more-epa-gov-acidrain-effects-acid-rain](https://www.epa.gov/acidrain/effects-acid-rain) | science | 98.1% | 100.0% | 7/7 | 3/3 | 11.50ms | 13.10ms | 7406 B |
| [more-epa-gov-acidrain-what-acid-rain](https://www.epa.gov/acidrain/what-acid-rain) | science | 100.0% | 100.0% | 7/7 | 3/3 | 10.83ms | 11.23ms | 5316 B |
| [more-epa-gov-acidrain-acid-rain-program](https://www.epa.gov/acidrain/acid-rain-program) | science | 99.0% | 100.0% | 7/7 | 3/3 | 13.86ms | 14.50ms | 11422 B |
| [more-allrecipes-com-20144-banana-banana-bread](https://www.allrecipes.com/recipe/20144/banana-banana-bread/) | recipe | 97.4% | 88.1% | 7/7 | 3/3 | 34.42ms | 35.64ms | 10250 B |
| [more-allrecipes-com-10549-best-brownies](https://www.allrecipes.com/recipe/10549/best-brownies/) | recipe | 94.9% | 80.0% | 7/7 | 3/3 | 31.23ms | 33.56ms | 6315 B |
| [more-allrecipes-com-10813-best-chocolate-chip-cookies](https://www.allrecipes.com/recipe/10813/best-chocolate-chip-cookies/) | recipe | 97.7% | 88.8% | 7/7 | 3/3 | 35.01ms | 36.13ms | 11429 B |
| [more-allrecipes-com-16354-easy-meatloaf](https://www.allrecipes.com/recipe/16354/easy-meatloaf/) | recipe | 96.4% | 86.1% | 7/7 | 3/3 | 31.54ms | 35.86ms | 8343 B |
| [more-weather-gov-safety-lightning](https://www.weather.gov/safety/lightning) | safety | 100.0% | 100.0% | 4/4 | 3/3 | 12.57ms | 13.26ms | 3389 B |
| [more-weather-gov-safety-tornado](https://www.weather.gov/safety/tornado) | safety | 94.9% | 100.0% | 3/4 | 3/3 | 12.07ms | 12.71ms | 1284 B |
| [more-weather-gov-safety-flood](https://www.weather.gov/safety/flood) | safety | 100.0% | 100.0% | 3/3 | 2/2 | 12.33ms | 12.55ms | 1939 B |
| [more-weather-gov-safety-heat](https://www.weather.gov/safety/heat) | safety | 100.0% | 100.0% | 4/4 | 3/3 | 12.34ms | 13.18ms | 3104 B |
| [more-redcross-org-types-of-emergencies-earthquake](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/earthquake.html) | safety | 97.7% | 95.8% | 6/7 | 3/3 | 23.19ms | 23.45ms | 14432 B |
| [more-redcross-org-types-of-emergencies-flood](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/flood.html) | safety | 96.4% | 93.8% | 7/7 | 3/3 | 21.16ms | 22.01ms | 11069 B |
| [more-redcross-org-types-of-emergencies-hurricane](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/hurricane.html) | safety | 94.5% | 95.6% | 6/7 | 3/3 | 24.63ms | 26.56ms | 15747 B |
| [more-redcross-org-types-of-emergencies-tornado](https://www.redcross.org/get-help/how-to-prepare-for-emergencies/types-of-emergencies/tornado.html) | safety | 96.8% | 94.3% | 6/7 | 3/3 | 22.60ms | 24.23ms | 13055 B |
| [more-rspb-org-uk-birds-and-wildlife-robin](https://www.rspb.org.uk/birds-and-wildlife/robin) | nature | 99.7% | 100.0% | 7/7 | 3/3 | 16.76ms | 18.90ms | 5806 B |
| [more-rspb-org-uk-birds-and-wildlife-blackbird](https://www.rspb.org.uk/birds-and-wildlife/blackbird) | nature | 99.7% | 100.0% | 7/7 | 3/3 | 17.07ms | 23.52ms | 6228 B |
| [more-rspb-org-uk-birds-and-wildlife-blue-tit](https://www.rspb.org.uk/birds-and-wildlife/blue-tit) | nature | 99.6% | 100.0% | 7/7 | 3/3 | 16.63ms | 18.05ms | 5393 B |
| [more-rspb-org-uk-birds-and-wildlife-house-sparrow](https://www.rspb.org.uk/birds-and-wildlife/house-sparrow) | nature | 99.7% | 100.0% | 7/7 | 3/3 | 16.93ms | 19.35ms | 6679 B |
| [more-britannica-com-science-earthquake-geology](https://www.britannica.com/science/earthquake-geology) | encyclopedia | 99.8% | 92.9% | 7/7 | 3/3 | 18.71ms | 20.72ms | 16874 B |
| [more-britannica-com-science-plate-tectonics](https://www.britannica.com/science/plate-tectonics) | encyclopedia | 99.6% | 100.0% | 7/7 | 3/3 | 17.87ms | 19.25ms | 13784 B |
| [more-nhs-uk-medicines-antibiotics](https://www.nhs.uk/medicines/antibiotics/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 10.81ms | 11.12ms | 8031 B |
| [more-britannica-com-science-continental-drift-geology](https://www.britannica.com/science/continental-drift-geology) | encyclopedia | 97.4% | 99.5% | 5/6 | 3/3 | 19.53ms | 21.13ms | 20718 B |
| [more-worldhistory-org-silk-road](https://www.worldhistory.org/Silk_Road/) | history | 100.0% | 86.3% | 7/7 | 3/3 | 19.28ms | 19.67ms | 21216 B |
| [more-worldhistory-org-egypt](https://www.worldhistory.org/egypt/) | history | 100.0% | 93.0% | 7/7 | 3/3 | 27.44ms | 30.12ms | 40080 B |
| [more-worldhistory-org-roman-republic](https://www.worldhistory.org/Roman_Republic/) | history | 100.0% | 90.0% | 7/7 | 3/3 | 22.86ms | 24.92ms | 27500 B |
| [more-worldhistory-org-mesopotamia](https://www.worldhistory.org/Mesopotamia/) | history | 100.0% | 93.5% | 7/7 | 3/3 | 29.39ms | 32.47ms | 42784 B |
| [more-fda-gov-nutrition-food-labeling-and-critical-foods-changes-nutrition-facts-label](https://www.fda.gov/food/nutrition-food-labeling-and-critical-foods/changes-nutrition-facts-label?scrlybrkr=) | consumer-guide | 99.4% | 100.0% | 7/7 | 3/3 | 17.38ms | 17.80ms | 21661 B |
| [more-fda-gov-buy-store-serve-safe-food-safe-food-handling](https://www.fda.gov/food/buy-store-serve-safe-food/safe-food-handling) | consumer-guide | 98.5% | 100.0% | 8/8 | 4/4 | 10.68ms | 12.35ms | 5797 B |
| [more-nhs-uk-antibiotics-side-effects](https://www.nhs.uk/medicines/antibiotics/side-effects/) | health | 100.0% | 100.0% | 7/7 | 3/3 | 9.16ms | 10.01ms | 3062 B |
| [more-fda-gov-consumer-updates-it-really-fda-approved](https://www.fda.gov/consumers/consumer-updates/it-really-fda-approved) | consumer-guide | 99.0% | 100.0% | 7/7 | 3/3 | 12.87ms | 13.77ms | 18311 B |
| [more-plato-stanford-edu-entries-ethics-virtue](https://plato.stanford.edu/entries/ethics-virtue/) | reference | 99.9% | 99.5% | 5/6 | 3/3 | 31.46ms | 34.21ms | 90893 B |
| [more-plato-stanford-edu-entries-consciousness](https://plato.stanford.edu/entries/consciousness/) | reference | 100.0% | 99.6% | 6/6 | 3/3 | 47.53ms | 49.46ms | 156376 B |
| [more-plato-stanford-edu-entries-scientific-method](https://plato.stanford.edu/entries/scientific-method/) | reference | 99.9% | 98.4% | 6/6 | 3/3 | 33.90ms | 36.53ms | 104037 B |
| [more-plato-stanford-edu-entries-logic-classical](https://plato.stanford.edu/entries/logic-classical/) | reference | 99.9% | 99.5% | 6/6 | 3/3 | 43.20ms | 44.59ms | 123262 B |

## By content type

| Kind | Pages | Recall | Precision | Checks |
|---|---:|---:|---:|---:|
| accessibility | 10 | 99.8% | 96.0% | 80/80 |
| api | 45 | 99.9% | 98.7% | 351/361 |
| blog | 30 | 99.8% | 96.9% | 204/205 |
| book | 3 | 100.0% | 99.7% | 11/11 |
| catalogue | 1 | 95.4% | 100.0% | 7/7 |
| configuration | 26 | 99.1% | 99.2% | 208/209 |
| consumer-guide | 3 | 99.0% | 100.0% | 22/22 |
| data-article | 10 | 99.8% | 100.0% | 71/80 |
| database | 26 | 98.2% | 98.0% | 182/187 |
| discussion | 2 | 100.0% | 96.0% | 13/13 |
| docs | 62 | 99.8% | 98.8% | 465/479 |
| encyclopedia | 14 | 97.9% | 98.0% | 94/99 |
| essay | 31 | 99.8% | 98.6% | 177/185 |
| explainer | 9 | 94.9% | 99.8% | 58/63 |
| guide | 14 | 99.9% | 100.0% | 96/96 |
| health | 24 | 99.9% | 99.6% | 163/163 |
| historical-document | 1 | 100.0% | 92.5% | 5/5 |
| history | 13 | 99.9% | 95.9% | 88/88 |
| manual | 59 | 99.2% | 99.5% | 409/429 |
| nature | 4 | 99.7% | 100.0% | 28/28 |
| recipe | 6 | 97.0% | 90.1% | 47/47 |
| reference | 4 | 99.9% | 99.3% | 23/24 |
| repair-guide | 1 | 100.0% | 79.7% | 7/7 |
| safety | 8 | 97.5% | 97.4% | 39/43 |
| science | 35 | 98.6% | 92.1% | 220/228 |
| standard | 11 | 100.0% | 100.0% | 56/62 |
| tutorial | 36 | 99.9% | 99.8% | 266/269 |
| visitor-guide | 12 | 99.0% | 97.4% | 82/83 |

## By corpus cohort

Held-out sites were selected and annotated before this evaluation, without tuning extraction on their output. Once inspected, they become regression evidence; future blind evaluations need fresh sites.

| Cohort | Pages | Recall | Precision | Checks | Critical |
|---|---:|---:|---:|---:|---:|---:|
| expansion | 60 | 99.5% | 98.4% | 420/429 | 227/227 |
| heldout | 20 | 98.7% | 97.2% | 144/148 | 74/74 |
| regression | 20 | 99.4% | 97.9% | 113/115 | 79/79 |
| scale | 320 | 99.4% | 98.2% | 2233/2309 | 1166/1175 |
| scale-new-sites | 80 | 99.0% | 97.9% | 562/572 | 276/276 |

## Failures and omissions

- **gnu/exclude-chrome** (absent, critical=false)
- **cloudflare/exclude-chrome** (absent, critical=false)
- **go-strings/source-link** (link, critical=false)
- **rfc-uri/source-link** (link, critical=false)
- **owid-life/section-heading** (heading, critical=false)
- **owid-life/later-heading** (heading, critical=false)
- **owid-population/section-heading** (heading, critical=false)
- **owid-population/source-link** (link, critical=false)
- **paulgraham-lisp/source-link** (link, critical=false)
- **paulgraham-schedule/source-link** (link, critical=false)
- **noaa-blue/source-link** (link, critical=false)
- **bgs-earthquakes/navigation-noise** (absent, critical=false)
- **ffmpeg-filters/section-heading** (heading, critical=false)
- **ffmpeg-filters/later-heading** (heading, critical=false)
- **ncat-guide/source-link** (link, critical=false)
- **more-docs-npmjs-com-commands-npm-ci/navigation-noise** (absent, critical=false)
- **more-docs-npmjs-com-commands-npm-publish/navigation-noise** (absent, critical=false)
- **more-docs-npmjs-com-commands-npm-audit/navigation-noise** (absent, critical=false)
- **more-docs-npmjs-com-commands-npm-exec/navigation-noise** (absent, critical=false)
- **more-docs-npmjs-com-commands-npm-run/navigation-noise** (absent, critical=false)
- **more-docs-npmjs-com-configuring-npm-package-lock-json/navigation-noise** (absent, critical=false)
- **more-docs-npmjs-com-using-npm-workspaces/navigation-noise** (absent, critical=false)
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
- **more-un-org-raising-ambition-renewable-energy/closing** (text, critical=true)
- **more-un-org-raising-ambition-renewable-energy/later-heading** (heading, critical=false)
- **more-un-org-climate-issues-greenwashing/closing** (text, critical=true)
- **more-un-org-climate-issues-greenwashing/later-heading** (heading, critical=false)
- **more-un-org-climatechange-net-zero-coalition/closing** (text, critical=true)
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
- **more-docs-github-com-workflows-and-actions-workflow-syntax/source-link** (link, critical=false)
- **more-developer-android-com-background-work-services/source-link** (link, critical=false)
- **more-developer-android-com-background-tasks-broadcasts/source-link** (link, critical=false)
- **more-developer-android-com-manifest-manifest-intro/source-link** (link, critical=false)
- **more-weather-gov-safety-tornado/source-link** (link, critical=false)
- **more-redcross-org-types-of-emergencies-earthquake/source-link** (link, critical=false)
- **more-redcross-org-types-of-emergencies-hurricane/source-link** (link, critical=false)
- **more-redcross-org-types-of-emergencies-tornado/source-link** (link, critical=false)
- **more-britannica-com-science-continental-drift-geology/section-heading** (heading, critical=false)
- **more-plato-stanford-edu-entries-ethics-virtue/source-link** (link, critical=false)
