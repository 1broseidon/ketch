# Ketch extraction benchmark

2026-09-19T09:08:14Z · linux/amd64 · 24 logical CPUs

Binary build: go1.27.1, CGO_ENABLED=0.

Mode: **default**. 7 measured runs + 1 warmups per page, 1 workers. Wall time 33.90s.

Corpus SHA256: `501016bcba5a3675c5e2107dc3462be0df725bc1be1e072cd9a4509b37ef80f0`  
Binary SHA256: `53432e7ddd71749efa89f0b798a8709744c19f2ad0feaa0e2539eb4bccd6ba75`

**616/692 checks passed; 352/380 critical checks; 0 failed or nondeterministic pages.**

Token coverage compares independently annotated source text with parsed Markdown. It is a multiset measure, not semantic correctness. Code, headings, table associations, links and exclusions are checked separately. Scores include every page; pages have equal weight in macro averages. Per-invocation timing includes CLI startup and excludes build, source loading, network and grading. Wall time includes warmups, grading and artifact writes. Selector mode is assisted recovery, not default extraction.

Macro token recall **94.4%**, precision **98.2%**, F1 **95.2%**.

| Page | Kind | Recall | Precision | Checks | Critical | Median | p95 | Output |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| [npm](https://docs.npmjs.com/trusted-publishers/) | docs | 82.3% | 100.0% | 3/6 | 2/4 | 41.45ms | 43.13ms | 16912 B |
| [go](https://pkg.go.dev/context) | api | 97.4% | 100.0% | 6/7 | 5/5 | 21.97ms | 22.24ms | 21866 B |
| [python](https://docs.python.org/3/tutorial/errors.html) | tutorial | 99.9% | 100.0% | 6/6 | 4/4 | 20.87ms | 21.18ms | 26827 B |
| [kubernetes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) | configuration | 94.4% | 100.0% | 6/6 | 4/4 | 47.60ms | 48.63ms | 18167 B |
| [postgres](https://www.postgresql.org/docs/current/transaction-iso.html) | database | 99.9% | 100.0% | 6/6 | 4/4 | 17.17ms | 17.33ms | 25133 B |
| [mdn](https://developer.mozilla.org/en-US/docs/Web/API/AbortController) | api | 95.2% | 100.0% | 7/7 | 4/4 | 17.46ms | 19.36ms | 2663 B |
| [rust](https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html) | tutorial | 100.0% | 99.4% | 5/6 | 4/4 | 15.43ms | 15.65ms | 28851 B |
| [sqlite](https://www.sqlite.org/lang_transaction.html) | database | 100.0% | 98.9% | 6/6 | 3/3 | 9.93ms | 10.15ms | 9640 B |
| [git](https://git-scm.com/docs/git-reset) | manual | 99.3% | 100.0% | 6/6 | 4/4 | 22.73ms | 23.02ms | 21431 B |
| [gnu](https://www.gnu.org/software/coreutils/manual/html_node/Exit-status.html) | manual | 100.0% | 100.0% | 5/5 | 3/3 | 5.38ms | 5.56ms | 705 B |
| [terraform](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle) | configuration | 99.0% | 99.6% | 5/5 | 4/4 | 32.79ms | 34.22ms | 20903 B |
| [docker](https://docs.docker.com/build/building/multi-stage/) | tutorial | 100.0% | 96.8% | 5/5 | 4/4 | 47.28ms | 49.10ms | 7544 B |
| [cloudflare](https://blog.cloudflare.com/rfc-9457-agent-error-pages/) | blog | 99.0% | 100.0% | 7/7 | 5/5 | 42.15ms | 42.32ms | 17599 B |
| [danluu](https://danluu.com/slow-device/) | essay | 100.0% | 100.0% | 5/5 | 3/3 | 27.65ms | 28.70ms | 72851 B |
| [joel](https://www.joelonsoftware.com/2000/04/06/things-you-should-never-do-part-i/) | blog | 100.0% | 100.0% | 4/4 | 4/4 | 10.26ms | 10.98ms | 8548 B |
| [rfc](https://www.rfc-editor.org/rfc/rfc8259.html) | standard | 100.0% | 100.0% | 4/4 | 4/4 | 10.47ms | 11.63ms | 28586 B |
| [wikipedia](https://en.wikipedia.org/wiki/HTTP) | encyclopedia | 98.5% | 99.8% | 5/6 | 4/4 | 96.24ms | 98.40ms | 97757 B |
| [gutenberg](https://www.gutenberg.org/files/11/11-h/11-h.htm) | book | 100.0% | 99.6% | 4/4 | 3/3 | 44.46ms | 48.01ms | 152453 B |
| [nasa](https://science.nasa.gov/mars/facts/) | science | 92.2% | 100.0% | 5/5 | 4/4 | 23.54ms | 24.94ms | 9286 B |
| [nps](https://www.nps.gov/yell/planyourvisit/safety.htm) | visitor-guide | 2.4% | 100.0% | 1/9 | 0/5 | 15.92ms | 16.98ms | 934 B |
| [npm-scripts](https://docs.npmjs.com/cli/v11/using-npm/scripts/) | docs | 100.0% | 96.8% | 8/8 | 4/4 | 46.36ms | 47.64ms | 16958 B |
| [npm-package-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-json/) | docs | 100.0% | 98.9% | 8/8 | 4/4 | 67.95ms | 70.23ms | 39994 B |
| [go-io](https://pkg.go.dev/io) | api | 100.0% | 86.3% | 7/8 | 4/4 | 51.15ms | 51.88ms | 62118 B |
| [go-strings](https://pkg.go.dev/strings) | api | 93.4% | 100.0% | 5/8 | 4/4 | 49.98ms | 50.96ms | 40626 B |
| [python-data](https://docs.python.org/3/tutorial/datastructures.html) | tutorial | 99.4% | 100.0% | 7/8 | 3/4 | 23.04ms | 25.28ms | 26756 B |
| [python-classes](https://docs.python.org/3/tutorial/classes.html) | tutorial | 98.7% | 100.0% | 7/8 | 3/4 | 25.18ms | 25.91ms | 38930 B |
| [kubernetes-deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) | configuration | 99.9% | 100.0% | 8/8 | 4/4 | 61.49ms | 61.71ms | 61231 B |
| [kubernetes-configmaps](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/) | configuration | 98.7% | 100.0% | 7/8 | 4/4 | 54.74ms | 55.34ms | 34124 B |
| [postgres-constraints](https://www.postgresql.org/docs/current/ddl-constraints.html) | database | 99.9% | 100.0% | 7/8 | 4/4 | 16.84ms | 17.28ms | 24064 B |
| [postgres-queries](https://www.postgresql.org/docs/current/queries-table-expressions.html) | database | 99.9% | 100.0% | 7/8 | 4/4 | 24.21ms | 25.34ms | 35714 B |
| [mdn-promise](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Promise) | api | 98.0% | 100.0% | 7/9 | 3/5 | 29.54ms | 31.58ms | 34434 B |
| [mdn-fetch](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch) | api | 90.5% | 100.0% | 7/8 | 3/4 | 24.82ms | 27.54ms | 27795 B |
| [rust-enums](https://doc.rust-lang.org/book/ch06-01-defining-an-enum.html) | tutorial | 100.0% | 99.1% | 7/7 | 4/4 | 12.46ms | 13.29ms | 17677 B |
| [rust-result](https://doc.rust-lang.org/book/ch09-02-recoverable-errors-with-result.html) | tutorial | 100.0% | 99.4% | 7/7 | 4/4 | 16.52ms | 17.46ms | 28230 B |
| [sqlite-select](https://www.sqlite.org/lang_select.html) | database | 93.8% | 99.9% | 7/7 | 4/4 | 144.61ms | 146.80ms | 37694 B |
| [sqlite-foreignkeys](https://www.sqlite.org/foreignkeys.html) | database | 99.0% | 100.0% | 7/7 | 4/4 | 15.13ms | 15.84ms | 38075 B |
| [git-rebase](https://git-scm.com/docs/git-rebase) | manual | 29.5% | 100.0% | 2/8 | 0/4 | 40.57ms | 42.11ms | 20665 B |
| [git-restore](https://git-scm.com/docs/git-restore) | manual | 98.6% | 100.0% | 8/8 | 4/4 | 15.51ms | 16.75ms | 9051 B |
| [gnu-copy](https://www.gnu.org/software/coreutils/manual/html_node/cp-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 15.79ms | 17.04ms | 20687 B |
| [gnu-timeout](https://www.gnu.org/software/coreutils/manual/html_node/timeout-invocation.html) | manual | 100.0% | 100.0% | 6/6 | 4/4 | 8.66ms | 9.04ms | 5180 B |
| [terraform-foreach](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each) | configuration | 99.8% | 100.0% | 8/8 | 4/4 | 28.39ms | 29.94ms | 17149 B |
| [terraform-count](https://developer.hashicorp.com/terraform/language/meta-arguments/count) | configuration | 99.8% | 100.0% | 8/8 | 4/4 | 22.76ms | 23.36ms | 8000 B |
| [docker-volumes](https://docs.docker.com/engine/storage/volumes/) | docs | 100.0% | 92.0% | 8/9 | 5/5 | 69.92ms | 72.69ms | 29639 B |
| [docker-bridge](https://docs.docker.com/engine/network/drivers/bridge/) | docs | 100.0% | 95.0% | 9/9 | 5/5 | 66.02ms | 68.47ms | 27740 B |
| [cloudflare-pingora](https://blog.cloudflare.com/how-we-built-pingora-the-proxy-that-connects-cloudflare-to-the-internet/) | blog | 98.5% | 100.0% | 7/7 | 3/3 | 28.04ms | 28.21ms | 15928 B |
| [cloudflare-opensource](https://blog.cloudflare.com/pingora-open-source/) | blog | 97.4% | 100.0% | 8/8 | 4/4 | 32.88ms | 34.05ms | 11085 B |
| [danluu-branch](https://danluu.com/branch-prediction/) | essay | 100.0% | 100.0% | 8/8 | 4/4 | 14.18ms | 14.75ms | 36472 B |
| [danluu-malloc](https://danluu.com/malloc-tutorial/) | essay | 100.0% | 100.0% | 6/6 | 4/4 | 11.01ms | 12.84ms | 21104 B |
| [joel-test](https://www.joelonsoftware.com/2000/08/09/the-joel-test-12-steps-to-better-code/) | blog | 100.0% | 100.0% | 5/5 | 3/3 | 13.05ms | 13.20ms | 21487 B |
| [joel-leaky](https://www.joelonsoftware.com/2002/11/11/the-law-of-leaky-abstractions/) | blog | 100.0% | 100.0% | 5/5 | 3/3 | 11.31ms | 12.94ms | 12746 B |
| [rfc-http](https://www.rfc-editor.org/rfc/rfc9110.html) | standard | 100.0% | 97.9% | 7/7 | 4/4 | 435.65ms | 439.07ms | 816245 B |
| [rfc-uri](https://www.rfc-editor.org/rfc/rfc3986.html) | standard | 100.0% | 100.0% | 4/5 | 4/4 | 26.34ms | 27.21ms | 142714 B |
| [wikipedia-photosynthesis](https://en.wikipedia.org/wiki/Photosynthesis) | encyclopedia | 100.0% | 86.9% | 7/8 | 4/4 | 179.61ms | 181.35ms | 300845 B |
| [wikipedia-binary](https://en.wikipedia.org/wiki/Binary_search) | encyclopedia | 83.0% | 99.8% | 7/8 | 4/4 | 111.22ms | 113.57ms | 135281 B |
| [gutenberg-wallpaper](https://www.gutenberg.org/files/1952/1952-h/1952-h.htm) | book | 100.0% | 99.8% | 4/4 | 3/3 | 12.54ms | 13.44ms | 32127 B |
| [gutenberg-metamorphosis](https://www.gutenberg.org/files/5200/5200-h/5200-h.htm) | book | 100.0% | 99.8% | 3/3 | 3/3 | 24.25ms | 25.03ms | 120052 B |
| [nasa-earth](https://science.nasa.gov/earth/facts/) | science | 96.5% | 95.6% | 6/7 | 2/3 | 25.97ms | 27.29ms | 11214 B |
| [nasa-jupiter](https://science.nasa.gov/jupiter/jupiter-facts/) | science | 98.8% | 99.6% | 7/7 | 3/3 | 23.41ms | 25.44ms | 12081 B |
| [nps-hiking](https://www.nps.gov/grca/planyourvisit/hiking-faq.htm) | visitor-guide | 100.0% | 95.6% | 5/6 | 3/3 | 23.37ms | 25.58ms | 34476 B |
| [nps-canyon](https://www.nps.gov/grca/faqs.htm) | visitor-guide | 72.9% | 100.0% | 6/7 | 3/3 | 17.32ms | 17.59ms | 16867 B |
| [pandas-indexing](https://pandas.pydata.org/docs/user_guide/indexing.html) | api | 99.7% | 100.0% | 9/9 | 5/5 | 62.51ms | 63.55ms | 100175 B |
| [pandas-merge](https://pandas.pydata.org/docs/user_guide/merging.html) | api | 99.9% | 100.0% | 9/9 | 5/5 | 38.06ms | 39.70ms | 55211 B |
| [django-queries](https://docs.djangoproject.com/en/5.2/topics/db/queries/) | docs | 99.8% | 100.0% | 8/8 | 4/4 | 53.14ms | 53.64ms | 88004 B |
| [django-transactions](https://docs.djangoproject.com/en/5.2/topics/db/transactions/) | docs | 99.9% | 100.0% | 8/8 | 4/4 | 23.97ms | 25.22ms | 33867 B |
| [curl-manpage](https://curl.se/docs/manpage.html) | manual | 100.0% | 98.7% | 8/8 | 4/4 | 113.85ms | 116.89ms | 299959 B |
| [curl-http](https://curl.se/docs/httpscripting.html) | manual | 99.5% | 100.0% | 8/8 | 4/4 | 14.02ms | 14.53ms | 28168 B |
| [w3c-tables](https://www.w3.org/WAI/tutorials/tables/one-header/) | accessibility | 100.0% | 61.9% | 8/9 | 5/5 | 15.91ms | 17.09ms | 12805 B |
| [w3c-irregular](https://www.w3.org/WAI/tutorials/tables/irregular/) | accessibility | 100.0% | 73.7% | 7/8 | 4/4 | 18.05ms | 18.76ms | 14971 B |
| [owid-life](https://ourworldindata.org/life-expectancy) | data-article | 85.8% | 99.8% | 6/8 | 4/4 | 26.91ms | 28.02ms | 24139 B |
| [owid-population](https://ourworldindata.org/population-growth) | data-article | 79.0% | 99.7% | 5/8 | 4/4 | 22.72ms | 22.92ms | 17896 B |
| [eff-passwords](https://ssd.eff.org/module/creating-strong-passwords) | guide | 99.3% | 100.0% | 7/7 | 3/3 | 10.40ms | 11.10ms | 8673 B |
| [eff-plan](https://ssd.eff.org/module/your-security-plan) | guide | 99.4% | 100.0% | 7/7 | 3/3 | 10.16ms | 10.64ms | 9379 B |
| [paulgraham-lisp](https://www.paulgraham.com/avg.html) | essay | 100.0% | 100.0% | 4/4 | 3/3 | 16.16ms | 16.67ms | 28959 B |
| [paulgraham-schedule](https://www.paulgraham.com/makersschedule.html) | essay | 100.0% | 100.0% | 4/4 | 3/3 | 11.29ms | 11.84ms | 8641 B |
| [fowler-microservices](https://martinfowler.com/articles/microservices.html) | essay | 77.3% | 100.0% | 4/7 | 1/3 | 17.57ms | 18.14ms | 36911 B |
| [fowler-debt](https://martinfowler.com/bliki/TechnicalDebt.html) | essay | 99.0% | 100.0% | 7/7 | 3/3 | 8.94ms | 9.81ms | 7123 B |
| [letsencrypt-challenges](https://letsencrypt.org/docs/challenge-types/) | docs | 99.2% | 100.0% | 5/5 | 3/3 | 9.69ms | 10.28ms | 7588 B |
| [letsencrypt-compatibility](https://letsencrypt.org/docs/certificate-compatibility/) | docs | 97.2% | 100.0% | 5/5 | 3/3 | 9.24ms | 9.78ms | 5020 B |
| [noaa-salt](https://oceanservice.noaa.gov/facts/whysalty.html) | science | 95.7% | 100.0% | 4/6 | 2/3 | 9.08ms | 10.24ms | 2963 B |
| [noaa-blue](https://oceanservice.noaa.gov/facts/oceanblue.html) | science | 88.6% | 100.0% | 5/6 | 3/3 | 8.27ms | 9.00ms | 717 B |
| [bgs-earthquakes](https://www.bgs.ac.uk/discovering-geology/earth-hazards/earthquakes/how-are-earthquakes-detected/) | science | 97.9% | 100.0% | 8/8 | 4/4 | 25.59ms | 26.10ms | 17377 B |
| [medlineplus-handwashing](https://medlineplus.gov/ency/patientinstructions/000972.htm) | health | 86.9% | 100.0% | 5/7 | 3/3 | 9.62ms | 10.03ms | 4247 B |
| [who-resistance](https://www.who.int/news-room/fact-sheets/detail/antimicrobial-resistance) | health | 100.0% | 100.0% | 7/7 | 3/3 | 16.86ms | 18.05ms | 12309 B |
| [un-climate](https://www.un.org/en/climatechange/what-is-climate-change) | explainer | 96.2% | 100.0% | 7/7 | 3/3 | 13.50ms | 15.46ms | 10963 B |
| [computerhistory-1969](https://www.computerhistory.org/timeline/1969/) | history | 97.7% | 97.2% | 7/7 | 3/3 | 13.75ms | 14.27ms | 5514 B |
| [nhm-meteorites](https://www.nhm.ac.uk/discover/types-of-meteorites.html) | science | 83.4% | 100.0% | 7/7 | 3/3 | 20.58ms | 21.46ms | 8774 B |
| [standardebooks-austen](https://standardebooks.org/ebooks/jane-austen/pride-and-prejudice) | catalogue | 81.8% | 100.0% | 6/7 | 2/3 | 9.35ms | 9.94ms | 4664 B |
| [archives-declaration](https://www.archives.gov/founding-docs/declaration-transcript) | historical-document | 90.2% | 100.0% | 5/5 | 3/3 | 10.79ms | 11.24ms | 8575 B |
| [wikivoyage-paris](https://en.wikivoyage.org/wiki/Paris) | visitor-guide | 100.0% | 96.1% | 7/8 | 4/4 | 178.71ms | 182.11ms | 266640 B |
| [kingarthur-bread](https://www.kingarthurbaking.com/recipes/classic-sandwich-bread-recipe) | recipe | 79.1% | 97.3% | 7/10 | 5/6 | 30.90ms | 32.45ms | 4805 B |
| [seriouseats-cookies](https://www.seriouseats.com/the-food-lab-best-chocolate-chip-cookie-recipe) | recipe | 100.0% | 91.9% | 8/9 | 5/5 | 76.03ms | 76.70ms | 58838 B |
| [ifixit-moped](https://www.ifixit.com/Guide/Suzuki+FA50+Moped+Exhaust+Cleaning/3926) | repair-guide | 66.7% | 100.0% | 3/7 | 2/3 | 24.07ms | 26.79ms | 2093 B |
| [discourse-guide](https://meta.discourse.org/t/understanding-discourse-for-new-users/96331) | discussion | 100.0% | 69.5% | 7/8 | 4/4 | 25.99ms | 28.30ms | 27693 B |
| [hackernews-startup](https://news.ycombinator.com/item?id=8863) | discussion | 100.0% | 96.1% | 4/5 | 3/3 | 60.49ms | 61.68ms | 61086 B |
| [julia-arrays](https://docs.julialang.org/en/v1/manual/arrays/) | manual | 99.9% | 100.0% | 9/9 | 5/5 | 24.53ms | 25.67ms | 49217 B |
| [elixir-enum](https://elixir.hexdocs.pm/enum-cheat.html) | api | 99.7% | 100.0% | 8/8 | 4/4 | 25.83ms | 29.50ms | 24712 B |
| [ffmpeg-filters](https://ffmpeg.org/ffmpeg-filters.html) | manual | 99.3% | 100.0% | 5/7 | 4/4 | 464.96ms | 479.98ms | 945691 B |
| [angular-signals](https://angular.dev/guide/signals) | docs | 97.8% | 100.0% | 7/8 | 3/4 | 23.06ms | 24.62ms | 12913 B |
| [react-state](https://react.dev/learn/state-a-components-memory) | tutorial | 37.0% | 100.0% | 1/8 | 0/4 | 26.32ms | 26.70ms | 8082 B |
| [ncat-guide](https://nmap.org/ncat/guide/index.html) | manual | 99.7% | 98.5% | 6/6 | 3/3 | 8.90ms | 9.40ms | 5228 B |

## By content type

| Kind | Pages | Recall | Precision | Checks |
|---|---:|---:|---:|---:|
| accessibility | 2 | 100.0% | 67.8% | 15/17 |
| api | 9 | 97.1% | 98.5% | 65/73 |
| blog | 6 | 99.2% | 100.0% | 36/36 |
| book | 3 | 100.0% | 99.8% | 11/11 |
| catalogue | 1 | 81.8% | 100.0% | 6/7 |
| configuration | 6 | 98.6% | 99.9% | 42/43 |
| data-article | 2 | 82.4% | 99.7% | 11/16 |
| database | 6 | 98.8% | 99.8% | 40/42 |
| discussion | 2 | 100.0% | 82.8% | 11/13 |
| docs | 10 | 97.6% | 98.3% | 69/74 |
| encyclopedia | 3 | 93.8% | 95.5% | 19/22 |
| essay | 7 | 96.6% | 100.0% | 38/41 |
| explainer | 1 | 96.2% | 100.0% | 7/7 |
| guide | 2 | 99.4% | 100.0% | 14/14 |
| health | 2 | 93.5% | 100.0% | 12/14 |
| historical-document | 1 | 90.2% | 100.0% | 5/5 |
| history | 1 | 97.7% | 97.2% | 7/7 |
| manual | 11 | 93.2% | 99.7% | 69/77 |
| recipe | 2 | 89.6% | 94.6% | 15/19 |
| repair-guide | 1 | 66.7% | 100.0% | 3/7 |
| science | 7 | 93.3% | 99.3% | 42/46 |
| standard | 3 | 100.0% | 99.3% | 15/16 |
| tutorial | 8 | 91.9% | 99.3% | 45/55 |
| visitor-guide | 4 | 68.8% | 97.9% | 19/30 |

## By corpus cohort

Held-out sites were selected and annotated before this evaluation, without tuning extraction on their output. Once inspected, they become regression evidence; future blind evaluations need fresh sites.

| Cohort | Pages | Recall | Precision | Checks | Critical |
|---|---:|---:|---:|---:|---:|---:|
| expansion | 60 | 96.1% | 97.9% | 391/429 | 214/227 |
| heldout | 20 | 90.7% | 97.3% | 124/148 | 66/74 |
| regression | 20 | 93.0% | 99.7% | 101/115 | 72/79 |

## Failures and omissions

- **npm/npm-minimum** (text, critical=true)
- **npm/node-minimum** (text, critical=true)
- **npm/heading-how-trusted-publishing-works** (heading, critical=false)
- **go/heading-variables** (heading, critical=false)
- **rust/exclude-chrome** (absent, critical=false)
- **wikipedia/heading-message-format** (heading, critical=false)
- **nps/thermal-danger** (text, critical=true)
- **nps/thin-crust** (text, critical=true)
- **nps/wildlife** (text, critical=true)
- **nps/heading-thermal-areas** (heading, critical=false)
- **nps/heading-bison** (heading, critical=false)
- **nps/expanded-boardwalk-guidance** (text, critical=true)
- **nps/emergency-callout** (text, critical=true)
- **nps/accordion-label** (heading, critical=false)
- **go-io/navigation-noise** (absent, critical=false)
- **go-strings/section-heading** (heading, critical=false)
- **go-strings/later-heading** (heading, critical=false)
- **go-strings/source-link** (link, critical=false)
- **python-data/closing** (text, critical=true)
- **python-classes/closing** (text, critical=true)
- **kubernetes-configmaps/source-link** (link, critical=false)
- **postgres-constraints/section-heading** (heading, critical=false)
- **postgres-queries/section-heading** (heading, critical=false)
- **mdn-promise/opening** (text, critical=true)
- **mdn-promise/availability** (text, critical=true)
- **mdn-fetch/opening** (text, critical=true)
- **git-rebase/opening** (text, critical=true)
- **git-rebase/middle** (text, critical=true)
- **git-rebase/closing** (text, critical=true)
- **git-rebase/section-heading** (heading, critical=false)
- **git-rebase/later-heading** (heading, critical=false)
- **git-rebase/code-example** (code, critical=true)
- **docker-volumes/later-heading** (heading, critical=false)
- **rfc-uri/source-link** (link, critical=false)
- **wikipedia-photosynthesis/navigation-noise** (absent, critical=false)
- **wikipedia-binary/section-heading** (heading, critical=false)
- **nasa-earth/opening** (text, critical=true)
- **nps-hiking/navigation-noise** (absent, critical=false)
- **nps-canyon/source-link** (link, critical=false)
- **w3c-tables/navigation-noise** (absent, critical=false)
- **w3c-irregular/navigation-noise** (absent, critical=false)
- **owid-life/section-heading** (heading, critical=false)
- **owid-life/later-heading** (heading, critical=false)
- **owid-population/section-heading** (heading, critical=false)
- **owid-population/later-heading** (heading, critical=false)
- **owid-population/source-link** (link, critical=false)
- **fowler-microservices/opening** (text, critical=true)
- **fowler-microservices/closing** (text, critical=true)
- **fowler-microservices/later-heading** (heading, critical=false)
- **noaa-salt/opening** (text, critical=true)
- **noaa-salt/section-heading** (heading, critical=false)
- **noaa-blue/source-link** (link, critical=false)
- **medlineplus-handwashing/section-heading** (heading, critical=false)
- **medlineplus-handwashing/later-heading** (heading, critical=false)
- **standardebooks-austen/opening** (text, critical=true)
- **wikivoyage-paris/navigation-noise** (absent, critical=false)
- **kingarthur-bread/section-heading** (heading, critical=false)
- **kingarthur-bread/source-link** (link, critical=false)
- **kingarthur-bread/ingredient-quantity** (text, critical=true)
- **seriouseats-cookies/navigation-noise** (absent, critical=false)
- **ifixit-moped/opening** (text, critical=true)
- **ifixit-moped/section-heading** (heading, critical=false)
- **ifixit-moped/later-heading** (heading, critical=false)
- **ifixit-moped/source-link** (link, critical=false)
- **discourse-guide/navigation-noise** (absent, critical=false)
- **hackernews-startup/navigation-noise** (absent, critical=false)
- **ffmpeg-filters/section-heading** (heading, critical=false)
- **ffmpeg-filters/later-heading** (heading, critical=false)
- **angular-signals/opening** (text, critical=true)
- **react-state/opening** (text, critical=true)
- **react-state/middle** (text, critical=true)
- **react-state/closing** (text, critical=true)
- **react-state/section-heading** (heading, critical=false)
- **react-state/later-heading** (heading, critical=false)
- **react-state/code-example** (code, critical=true)
- **react-state/source-link** (link, critical=false)
