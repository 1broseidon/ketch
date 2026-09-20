# Ketch extraction benchmark

2026-09-19T04:11:25Z · linux/amd64 · 24 logical CPUs

Binary build: go1.27.1, CGO_ENABLED=0.

Mode: **default**. 7 measured runs + 1 warmups per page, 1 workers. Wall time 4.68s.

Corpus SHA256: `aba86c1ff23774dfc74ab5914955c80fc4fb816b9740f95064f202f153b1ac99`  
Binary SHA256: `53432e7ddd71749efa89f0b798a8709744c19f2ad0feaa0e2539eb4bccd6ba75`

**101/115 checks passed; 72/79 critical checks; 0 failed or nondeterministic pages.**

Token coverage compares independently annotated source text with parsed Markdown. It is a multiset measure, not semantic correctness. Code, headings, table associations, links and exclusions are checked separately. Scores include every page; pages have equal weight in macro averages. Per-invocation timing includes CLI startup and excludes build, source loading, network and grading. Wall time includes warmups, grading and artifact writes. Selector mode is assisted recovery, not default extraction.

Macro token recall **93.0%**, precision **99.7%**, F1 **94.0%**.

| Page | Kind | Recall | Precision | Checks | Critical | Median | p95 | Output |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| [npm](https://docs.npmjs.com/trusted-publishers/) | docs | 82.3% | 100.0% | 3/6 | 2/4 | 39.35ms | 41.08ms | 16912 B |
| [go](https://pkg.go.dev/context) | api | 97.4% | 100.0% | 6/7 | 5/5 | 21.84ms | 22.43ms | 21866 B |
| [python](https://docs.python.org/3/tutorial/errors.html) | tutorial | 99.9% | 100.0% | 6/6 | 4/4 | 20.53ms | 21.95ms | 26827 B |
| [kubernetes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) | configuration | 94.4% | 100.0% | 6/6 | 4/4 | 47.04ms | 49.98ms | 18167 B |
| [postgres](https://www.postgresql.org/docs/current/transaction-iso.html) | database | 99.9% | 100.0% | 6/6 | 4/4 | 17.11ms | 18.10ms | 25133 B |
| [mdn](https://developer.mozilla.org/en-US/docs/Web/API/AbortController) | api | 95.2% | 100.0% | 7/7 | 4/4 | 17.02ms | 18.25ms | 2663 B |
| [rust](https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html) | tutorial | 100.0% | 99.4% | 5/6 | 4/4 | 15.63ms | 16.18ms | 28851 B |
| [sqlite](https://www.sqlite.org/lang_transaction.html) | database | 100.0% | 98.9% | 6/6 | 3/3 | 10.08ms | 10.66ms | 9640 B |
| [git](https://git-scm.com/docs/git-reset) | manual | 99.3% | 100.0% | 6/6 | 4/4 | 23.11ms | 23.48ms | 21431 B |
| [gnu](https://www.gnu.org/software/coreutils/manual/html_node/Exit-status.html) | manual | 100.0% | 100.0% | 5/5 | 3/3 | 5.69ms | 6.04ms | 705 B |
| [terraform](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle) | configuration | 99.0% | 99.6% | 5/5 | 4/4 | 33.21ms | 34.51ms | 20903 B |
| [docker](https://docs.docker.com/build/building/multi-stage/) | tutorial | 100.0% | 96.8% | 5/5 | 4/4 | 47.36ms | 49.71ms | 7544 B |
| [cloudflare](https://blog.cloudflare.com/rfc-9457-agent-error-pages/) | blog | 99.0% | 100.0% | 7/7 | 5/5 | 41.99ms | 42.49ms | 17599 B |
| [danluu](https://danluu.com/slow-device/) | essay | 100.0% | 100.0% | 5/5 | 3/3 | 26.62ms | 27.73ms | 72851 B |
| [joel](https://www.joelonsoftware.com/2000/04/06/things-you-should-never-do-part-i/) | blog | 100.0% | 100.0% | 4/4 | 4/4 | 9.80ms | 10.56ms | 8548 B |
| [rfc](https://www.rfc-editor.org/rfc/rfc8259.html) | standard | 100.0% | 100.0% | 4/4 | 4/4 | 10.68ms | 11.09ms | 28586 B |
| [wikipedia](https://en.wikipedia.org/wiki/HTTP) | encyclopedia | 98.5% | 99.8% | 5/6 | 4/4 | 96.69ms | 99.03ms | 97757 B |
| [gutenberg](https://www.gutenberg.org/files/11/11-h/11-h.htm) | book | 100.0% | 99.6% | 4/4 | 3/3 | 42.18ms | 45.06ms | 152453 B |
| [nasa](https://science.nasa.gov/mars/facts/) | science | 92.2% | 100.0% | 5/5 | 4/4 | 23.61ms | 25.27ms | 9286 B |
| [nps](https://www.nps.gov/yell/planyourvisit/safety.htm) | visitor-guide | 2.4% | 100.0% | 1/9 | 0/5 | 15.89ms | 18.38ms | 934 B |

## By content type

| Kind | Pages | Recall | Precision | Checks |
|---|---:|---:|---:|---:|
| api | 2 | 96.3% | 100.0% | 13/14 |
| blog | 2 | 99.5% | 100.0% | 11/11 |
| book | 1 | 100.0% | 99.6% | 4/4 |
| configuration | 2 | 96.7% | 99.8% | 11/11 |
| database | 2 | 99.9% | 99.4% | 12/12 |
| docs | 1 | 82.3% | 100.0% | 3/6 |
| encyclopedia | 1 | 98.5% | 99.8% | 5/6 |
| essay | 1 | 100.0% | 100.0% | 5/5 |
| manual | 2 | 99.6% | 100.0% | 11/11 |
| science | 1 | 92.2% | 100.0% | 5/5 |
| standard | 1 | 100.0% | 100.0% | 4/4 |
| tutorial | 3 | 100.0% | 98.7% | 16/17 |
| visitor-guide | 1 | 2.4% | 100.0% | 1/9 |

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
