# ketch

A stateless command-line tool for web search, OSS code search, library docs,
and scraping — one binary, no daemon, nothing to keep running.

For people, it replaces `curl | pandoc` or a browser tab. For agents, it gives
predictable output, `--json` everywhere, and documented exit codes to branch on.

```console title="Install"
$ curl -fsSL https://ketch.run/install | sh
```

```console title="Or hand it to your agent"
Install ketch and configure it for me.
1. Run: curl -fsSL https://ketch.run/install | sh
2. Run `ketch doctor` and show me what is healthy vs missing.
3. Run `ketch config` to show the active backends.
4. Ask me which providers I want keys for, then set each one
   with `ketch config set <provider>_api_key <key>`.
5. Re-run `ketch doctor` to confirm everything resolves.
Do not change any setting that is already configured and working.
```

## Overview

Most research tooling for agents means wiring up several provider SDKs, each
with its own auth and response shape. ketch collapses that into one binary with
three research surfaces and two fetch surfaces. Any of them can `--tag`
what it returns, and `ketch tag` brings those sources back in a later session.

```
ketch search  "query"   # web pages
ketch code    "query"   # real OSS source
ketch docs    "query"   # library documentation
ketch scrape  <url>     # HTML or PDF → markdown
ketch crawl   <url>     # BFS or sitemap walk
ketch tag     show docs # sources saved with --tag, session to session
```

Output is YAML frontmatter followed by content, so it stays readable in a
terminal and parseable by a program. Add `--json` to any command for a
structured object instead.

An operator configures the backend once — `ketch config set backend searxng` —
and every later `ketch search` call runs without knowing which provider serves
it.

> **It works before it is configured.** The default `auto` backend is a
> fallback chain over the keyless providers, so `ketch search` answers on a
> fresh install, and `ketch docs` finds a library's docs site through it. Configuration raises limits and picks favourites; it is never
> the price of a first result.

## Install

The install script:

- Picks the build for your OS and architecture
- Verifies it against the release's `checksums.txt`
- Installs to `/usr/local/bin` if writable, otherwise `~/.local/bin`
- Requires no sudo
- Never half-overwrites a running binary

To avoid piping a URL into a shell, read
[install.sh](https://github.com/1broseidon/ketch/blob/main/install.sh) or
download a prebuilt binary from the
[releases page](https://github.com/1broseidon/ketch/releases). Pin a version or
redirect the target with `sh -s -- --version v0.17.1 --bin-dir ~/bin`.

`doctor` and `config` let an agent discover state without probing your
environment.

#### Homebrew — macOS and Linux

```console
$ brew install ketch
```

#### npm — or skip the install with npx

```console
$ npm install -g ketch-cli
$ npx -y ketch-cli <command>
```

#### Go — go install

```console
$ go install github.com/1broseidon/ketch@latest
```

#### pi — as a pi package

```console
$ pi install git:github.com/1broseidon/ketch
```

Adds the skill and `ketch_*` tools to [pi](https://github.com/badlogic/pi-mono);
uses `ketch` from PATH, or `npx -y ketch-cli` when it isn't installed. See
[pi package](#pi-package).

#### chain — the rest of the toolkit

ketch is one of the [chain.sh](https://chain.sh) tools. One command installs
the set:

```console
$ curl -fsSL https://chain.sh/bootstrap.sh | sh
```

## Quickstart

No API key needed. `auto` falls through the keyless providers until one
answers, and reports which one served.

```console
$ ketch search "golang error handling"
---
query: golang error handling
backend: parallel
result_count: 5
---
Error handling and Go - The Go Programming Language
  https://go.dev/blog/error-handling-and-go
  The language's design and conventions encourage you to explicitly check...

Best Practices for Error Handling in Go
  https://www.jetbrains.com/guide/go/tutorials/handle_errors_in_go/
  How can a reader see that any of these functions might observe an error?
```

```console
$ ketch scrape https://go.dev/blog/error-handling-and-go
---
url: https://go.dev/blog/error-handling-and-go
title: Error handling and Go
words: 1693
---
## Introduction

If you have written any Go code you have probably encountered the built-in
`error` type...
```

Accepted input: one URL, several URLs, a JSON array, a file of URLs, or stdin.
ketch detects the shape, so there's no batch flag. PDFs are detected by MIME
type or signature and parsed to text. JS-shell pages are re-fetched through
headless Chrome automatically, with the same output either way.

## Choosing a command

First match wins. The Not column names the most common mistake for each row.

| The question needs | Use | Not |
| --- | --- | --- |
| Current pages, opinions, news, comparisons | `search` | `docs` — that's library docs only |
| How real projects call an API | `code` | `search` — blogs talk *about* code; `code` greps the source |
| A library's own documentation, version-aware | `docs` | `scrape` of the docs site — `docs` is already extracted and budgeted |
| The content of a URL you already hold | `scrape` | `search` — never re-find a known URL |
| Many pages from one site | `crawl` | looped `scrape` — crawl dedupes, bounds, and streams |
| Sources you want back in a later session | `tag` | searching again — `--tag` on any surface, then `tag show` |

In reverse: `search` finds URLs, `scrape` reads them, `crawl` reads a site,
`code` reads public source, `docs` reads library docs, `tag` keeps what any of
them returned. `search --scrape` fuses
the two when you already want full content from every hit. Budget it like a
scrape.

## Commands

Thirteen commands. `--json` is the only flag global to all of them; everything
else is per-command. Expand a row for its full flag list.

#### search — Web search across thirteen providers

```console
$ ketch search "query" --limit 10
$ ketch search "query" --scrape          # fetch full content per result
$ ketch search "query" -b brave
$ ketch search "query" --multi           # federate, rank-fuse
$ ketch search "query" --multi=brave,exa # a specific set
```

| Flag | Meaning |
| --- | --- |
| `--backend, -b` | Provider, default `auto` |
| `--limit, -l` | Max results, default 5 |
| `--scrape` | Fetch full content for every result |
| `--multi` | Federate across backends, RRF-fused; mutually exclusive with `-b` |
| `--random` | Shuffle backends, try one, fall back to the rest |
| `--searxng-url` | SearXNG instance, default `http://localhost:8081` |
| `--minimal` | One result per line, tab-separated, no frontmatter |
| `--max-chars` | Truncate scraped markdown (with `--scrape`) |
| `--trim` | Strip markdown syntax, keep content text |
| `--cookie-file` | Cookie jar for `--scrape` fetches |
| `--user-agent` | User-Agent override for `--scrape` fetches |
| `--tag <name>` | Bookmark every result URL under a tag |

`--multi` fuses rankings with Reciprocal Rank Fusion — a page several engines
rank highly floats to the top — deduplicating by URL and tagging each result
with the engines that returned it.

#### code — Grep real source in public repos

```console
$ ketch code "http.NewRequestWithContext" --lang go --limit 2
---
query: http.NewRequestWithContext
lang: go
backend: grepapp
result_count: 2
---
harness/harness  registry/app/remote/clients/registry/client.go  (line 207)
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildPingURL(c.url), nil)
  https://github.com/harness/harness/blob/main/...
```

| Flag | Meaning |
| --- | --- |
| `--backend, -b` | `grepapp` (default), `sourcegraph`, `github` |
| `--lang` | Language qualifier, appended to the query |
| `--repo` | One repository, `owner/name` or a GitHub URL; exact on every backend |
| `--limit, -l` | Max results |
| `--minimal` | One result per line |
| `--tag <name>` | Bookmark every result URL under a tag |

Regex support is per-backend: grepapp and sourcegraph accept it, github rejects
it with a pointer to the other two.

`--repo` means the same repository on every backend. grep.app and Sourcegraph
filter more loosely (name substrings, an unanchored pattern), so ketch keeps
only exact matches: `--repo golang/go` never returns `golang/gofrontend`.
grepapp searches the query as literal code, so a `repo:` or `lang:` typed into
the query is matched as text; ketch warns and points at the flags. A missing
repository is never a silent empty result: sourcegraph and github exit 3 and
name the other backends, and grepapp, which indexes a subset of public
repositories, warns when a repository search comes back empty.

#### docs — Library documentation, from the docs site itself or Context7

```console
$ ketch docs "pydantic field alias for validation"                  # search (the default)
$ ketch docs --library django@5.2 "middleware ordering"              # a library, at a version
$ ketch docs --library https://docs.astro.build/en/ "content collections schema"   # within one docs site
$ ketch docs --section https://www.python-httpx.org/api/#client      # a cut group, in full
$ ketch docs --resolve click                                         # a name → its docs root, with evidence
$ ketch docs --resolve                                               # every indexed library, offline
$ ketch docs index fastapi                                           # index a whole site, for offline answers
$ ketch docs -b context7 --library /vercel/next.js "routing"         # Context7's curated snippets
$ ketch docs -b context7 --resolve "next.js"                         # a name → Context7 IDs
```

| Flag | Meaning |
| --- | --- |
| `--backend, -b` | `search` (default) or `context7`, for this call. The `docs_backend` config key is retired |
| `--library` | `search`: a docs URL, or a library name or spec (see Naming a library). `context7`: a library ID from `--resolve` |
| `--tokens` | Budget. `search`: about 1,800 tokens, frontmatter included. `context7`: 4000. With `--section`: the page size, 4000 |
| `--limit, -l` | Max results. `search`: no cap unless set. `context7`: the configured `limit` (5), none with `--library` unless set |
| `--resolve` | Resolve the query as a library name: docs roots (`search`; with no name, every indexed library) or Context7 IDs |
| `--section <url#anchor>` | Return one section, with no query and no ranking (see Full sections) |
| `--page N` | With `--section`: the page of a long section to return (default 1) |
| `--offline` | Use only the local index, with no network requests; a missing library exits 3 |
| `--minimal` | One result per line: URL, library, first line; the note, `stale` and `not_found` go to stderr |
| `--tag <name>` | Bookmark every result URL under a tag |

`search` answers from the documentation itself, with no key, on one of two
paths. A question that names a library indexed with `ketch docs index`, in its
words or with `--library`, answers from the index with no search, in tens of
milliseconds. Any other question takes the live path: a web search, the docs
site the results agree on, and up to 24 of its pages, within 45 seconds.

A question saves nothing. The pages it read stay in the page cache for
`cache_ttl` (72h by default). The same question again within that time, with
the same terms after filler words and the same `--library`, does no search and
no fetch, answered or refused alike, and its `source:` line ends `, cached`.
Only `ketch docs index` adds libraries or pages to the index.

##### Answers

An answer is a frontmatter verdict, then evidence: the pieces of sections that
name the question, verbatim.

```console
$ ketch docs "httpx timeout configuration"
---
query: httpx timeout configuration
backend: search
library: https://www.python-httpx.org/
index: full, 23 pages, built 2026-11-12
status: found
terms: {timeout: 19}
not_found: []
---
## Timeouts

### Fine tuning the configuration

HTTPX also allows you to specify the timeout behavior in more fine grained detail.
…
source: https://www.python-httpx.org/advanced/timeouts/#fine-tuning-the-configuration
…
## Developer Interface

### Client
same text in 1 other section: AsyncClient
…
source: https://www.python-httpx.org/api/#client (cut, 397 of 1,524 tokens)
…
## HTTPX

### Features

- Strict timeouts everywhere.

…

- Connection Timeouts

source: https://www.python-httpx.org/#features (cut, 11 of 182 tokens)

3 groups above are cut and their hidden text is unread: run ketch docs --section <its source> before stating what it says
```

The frontmatter prints these lines, in this order, each only when it applies:

| Field | Says |
| --- | --- |
| `query`, `backend` | The question, and the backend that answered |
| `library` | The docs root the answer came from |
| `scope` | The URL below that root the answer is limited to, when `--library` named one |
| `version` | The indexed version, when the library has one |
| `index` or `source` | `index: full, N pages, built <date>` for an indexed library (`capped` when `--max-pages` left pages out); `source: search, N pages read` for a live answer, ending `, cached` on a repeat |
| `origin` | Live answers only: whether the site is the documentation of what was asked (see Live answers) |
| `stale` | `unverified, built <date>` when a cited page could not be checked (see The index) |
| `status` | `found`, `partial (<reason>)` or `refused (<reason>)` |
| `terms` | The question's distinctive terms as written, each with the number of sections holding it |
| `not_found` | The terms no section holds |
| `outside_scope` | With `scope`: the terms the library holds only outside it |
| `defined_in` | `{term: url#anchor}`: for a listed term that is the first cell of a table row in a section the answer does not print, that section; at most three |
| `indexed_near` | `{title: root}`: when `--library` named no indexed library, the indexed ones near it; the answer ends with a line saying to pass one as `--library` |
| `note` | The status in words, other indexed versions of the library, and on a live answer the pages read and the `ketch docs index` command for the docs subtree the answer came from (left out on a host many projects share) |

Distinctive terms are the identifiers the question writes and its rare words.
On an indexed library a word is rare when it is in under 1% of the library's
sections or in at most 25 of them. A live answer reads too few pages to
measure rarity, so it lists identifiers and uncommon words only: common English
and programming words (`whose`, `message`, `timeout`) appear in neither `terms`
nor `not_found`, and `terms: {}` is normal there. A number of four or more
digits matches its grouped form in the docs: `10000` finds `10,000`.

The status is one of:

- `found`: the docs hold evidence matching the question. It is not a promise that the evidence answers it.
- `partial (terms_not_found)`: a term you wrote is in no section; `not_found` lists it.
- `partial (terms_outside_scope)`: with `scope`, some terms are only outside it; `outside_scope` lists them.
- `refused (<reason>)`: exit 3, with the frontmatter printed and an empty body (see Exit codes).

The body prints each page's groups under `## <page title>`, the page with the
best group first and its groups in rank order. A group is its heading path
(`### <heading> > <subheading>`), the pieces of the section that name the
question (paragraphs, list items, code blocks, table rows under their header),
and `source: url#anchor`. A page's lead section printed after another of its
groups is headed `### (top of page)`. The whole answer is about 1,800 tokens,
frontmatter included: `--tokens` changes that, and only an explicit `--limit`
caps the number of groups.

A group that shows part of its section ends its source line with
`(cut, N of M tokens)`, and `…` marks text left out. A section over 400 tokens
is never printed whole. A cut group may print `also in this section:` with what
it left out by name, and a line under its heading such as `option rows, 2 of 17`
or `same text in 1 other section: AsyncClient`. When any group is cut, the
answer ends with the line in the example above: the hidden text is unread until
`--section` reads it.

##### Full sections

`--section <url#anchor>` returns one section and its subsections, with no
ranking: from the index, or for a page a live answer read, from that page
through the page cache (fetched again once it has expired). Pass a group's
`source`, a `defined_in` target, or a page URL with no anchor for the page's
sections in order. `--library` is optional. A section that fits `--tokens`
(4000 by default) comes whole. A longer one comes a page at a time, broken at a
block boundary and never inside a code block, and its last line is the command
for the next page:

```console
$ ketch docs --section "https://www.python-httpx.org/api/#client" --tokens 600
---
backend: search
library: https://www.python-httpx.org/
index: full, 23 pages, built 2026-11-12
page: 1 of 4
---
### Developer Interface > Client
…
source: https://www.python-httpx.org/api/#client

next page: ketch docs --section https://www.python-httpx.org/api/#client --tokens 600 --page 2
```

##### Naming a library

`--library`, `--resolve` and `docs index` take one spec:

| Spec | Example | Means |
| --- | --- | --- |
| Docs URL | `https://docs.astro.build` | That site, as given; the URL already names its version, so no `@` |
| Package name | `click`, `next@14`, `django@5.2` | Looked up in npm, PyPI and crates.io |
| Ecosystem prefix | `pypi:click`, `npm:@astrojs/starlight@0.30` | Only that registry: `npm:`, `pypi:`, `crates:` (or `cargo:`), `go:`, `github:` |
| Repository | `pallets/click`, `github:pallets/click` | A GitHub repository |
| Go module path | `github.com/spf13/cobra` | The module, and the repository it names |

`@version` asks for that version's docs: an exact label, an alias (`latest`,
`stable`, `dev`, `main`), or a prefix that picks the newest release under it
(`5` → the newest `5.x`), never across a major version. A site that publishes
no version list answers only for the package's current major. In a question,
a spec of its own (`django@5.2`, `pypi:click`) is preferred the same way.

A name an indexed library answers to (see The index) answers from the index. A
URL below an indexed root limits the answer to the pages under it: `library:`
is the root and `scope:` the URL. Any other value takes the live path.

`--resolve` shows the docs root a name resolves to, and why. With no name it
lists every indexed library, offline, and a name an indexed library answers to
lists that one:

```console
$ ketch docs --resolve
https://www.python-httpx.org/  HTTPX  (indexed: yes, full, 23 pages, built 2026-11-12)
```

Any other name is resolved on the web, and becomes a docs site only on
evidence: the package's registry entry or GitHub repository names the site, the
homepage they name links to it as its docs, or it is the package's own docs on
docs.rs or pkg.go.dev. A site found any other way counts only when it links
back to the repository (and, when search found it, sits on the project's own
domain), so a mirror never does. A repository counts only when it backs the
package's claim, so `click` is PyPI's Click, not npm's or crates.io's unrelated
`click`.

```console
$ ketch docs --resolve click
https://click.palletsprojects.com/en/stable/  Click  (trust: 10.0, version: 8.6.x, versions: 8.6.x)
  packages: pypi:click, github:pallets/click
  evidence: pypi click lists it as Documentation; links to github.com/pallets/click; the site links to the pypi click package page; the site's title names click; on the domain of pypi click project URL palletsprojects.com; on the domain of pypi click documentation click.palletsprojects.com
```

The ID is the docs root URL; for a version, that version's root. Trust is on
Context7's 0–10 scale but scored from the evidence listed, and `packages` are
specs you can pass as `--library`. With `--json`, each match carries
`version`, `versions`, `evidence` and `packages`, and an indexed one
`indexed: true` and `index`. A name more than one verified project answers to
is listed in full, and `docs index` refuses it so you pass the one you mean.
When a registry or GitHub failed, each match's evidence begins
`this list may be incomplete:`. Resolving a name asks GitHub about its
repository, which allows 60 requests an hour without a token; `gh auth login`
or `github_token` lifts it.

##### Live answers

ketch searches the web for the question, and for the question plus
"documentation", through the `search` surface's providers (the `auto` chain, or
your `search_order`). Forums, Q&A, blog posts and mirrors are dropped, and the
site most of the remaining hits agree on is read. A site the question names
(`duo.com` for a question about Duo) goes first, as does a standards body's
site for a question about a spec, and a site's main docs go ahead of an
enterprise edition, a legacy tree or a pinned release unless the question names
one. ketch reads up to 24 pages, the hits on that site and their neighbours in
its page lists, through the page cache, honouring `robots.txt`, and answers
from them as from an indexed library. When the site gives nothing readable, the
domain's next docs tree is tried, then the next site: two sites and four trees
at most. The note then says `could not read <root> (<cause>), so this answer is
from the next site`.

With `--library`, a docs URL is the site read (a page URL, that page first). A
name or spec is resolved while the search runs: the docs root resolve verifies
is read, unless the site the search agrees on is that project's own. On a host
that serves many projects, such as pkg.go.dev, only the named project's path is
read (`net/http` is `https://pkg.go.dev/net/http/`).

A live answer says whether the site is the documentation of what was asked:
`origin: verified, <evidence>` (a second signal beyond the name: the root
resolve found and the site the search agrees on are one project, the site is
the project's own, a standard-library page of the language's docs, or the
library names the site); `origin: by name, …` when only a name ties the site to
what was asked (a registry package of the name, with the site the search
results favour when that is another; a domain carrying the name); or
`origin: unverified, ketch could not confirm <host> as the official documentation of <what>`,
which ends `for this question` when nothing shows the question names a thing.
The last two end with a line saying to name the site you cite or pass the
official docs with `--library` (for by name, both roots). A name resolved
earlier reads `origin: verified, resolved earlier and cached (…)`.

When resolve and the search disagree, both sites can be read in one call and
the better answer is kept: `found` over `partial` over refused. A name taken
from the question, tied to a registry package by name only, gives way to the
site the search favours when that site carries the name. A root read first
that lacks some of the question's terms falls over to the favoured site, and
the origin then ends `…lacks the question's terms <terms>, so this answer is
from the site the search results favour` (`holds none of the question's terms`
when it had none). When the other site could not be read, the note says so:
`could not read <root> (<cause>), the site the search results favour`.

##### The index

```console
$ ketch docs index https://docs.astro.build/en/    # a docs URL, taken as the root
$ ketch docs index fastapi                         # or a name or spec, resolved as --resolve shows
$ ketch docs index django@5.2 --max-pages 800
$ ketch docs index vue-router@3                    # another version, indexed beside the current one
$ ketch docs index --dry-run numpy                 # the pages a build would index, in order
$ ketch docs index --list
$ ketch docs index --remove fastapi
```

`docs index` does the slow part of a docs query once per library and version,
and stores it in `docs-index.sqlite` in ketch's cache directory (SQLite through
a pure-Go driver; `KETCH_DOCS_INDEX_PATH` overrides it). An index an earlier
ketch built (`docs-index.db`, bbolt) is not read: `docs index` and
`ketch doctor` say so, and you rebuild what you need. A docs URL is the root. A
name or spec is resolved as `--resolve` resolves one: an ambiguous name exits
**2** listing the candidates, one nothing verifies exits **3**, and you pass the
docs URL you mean instead.

It reads the site's own page lists (`llms.txt` and `llms-full.txt`, sitemaps,
its search index and nav links), keeps the pages under the root in its locale
and version, honours `robots.txt`, fetches up to `--max-pages` pages (1,500)
`--concurrency` at a time (4) through the page cache, and splits them into
heading sections, never inside a code block. A section over 1,500 tokens on a
page whose anchors are known is split further at an RFC's numbered headings or
at the entry anchors the page marks (an nginx directive, a Sphinx API entry).
The sections are indexed for full-text search with their identifiers split
into parts (`useForm` is also `use` and `form`) and whole
(`client_max_body_size`). A 735-page site indexes from the page cache in
seconds. Progress goes to stderr, the summary to stdout or, with `--json`, as
JSON.

**Which pages.** Over `--max-pages`, the pages `llms.txt` lists come first, in
its order, then guides (`guide/`, `tutorial/`, `concepts/`, …), then everything
else, and last API reference leaves (Sphinx's `generated/`, TypeDoc's
`classes/`, pages two levels below `api/` or `reference/`) and release notes;
within a group, top-level directories take turns. Blog, news, announcement,
showcase and community sections under the root, and dated posts, are left out
whatever the cap. `x` and `x.html` are one page, an `llms.txt` text mirror
under `/llms/` is left out when the rendered pages are listed, and a fetched
page whose text is another's (the same page at a second address, a missing page
answered with the home page) is dropped. `--dry-run` prints the pages a build
would index, in order, with their group (`curated`, `guide`, `other`,
`reference`), without fetching a page or writing the index.

**Where the docs are.** A root that stands for a release (it redirects,
refreshes or points as canonical to a numbered release: duckdb.org's
`/docs/stable/` is `/docs/2.0/`) is indexed at that release. So is an
unversioned root whose page lists or `llms.txt` sit almost wholly in release
directories right below it (`docs.crewai.com` and `/v1.15.35/`): at the release
`llms.txt` lists, else the newest, with results at the unversioned addresses
when the release names them canonical. A root whose pages sit in several
release directories within two levels (sequelize.org's `/docs/v6/` and
`/docs/v7/`) is indexed at the one its home page links to most, else the newest
final release. A version's own host whose page lists are an app shell
(`v18.angular.dev`) is given the current host's paths to try. The summary says
when any of this happened.

**One index is one version.** `name@version` (or `--version`) indexes that
version's docs root, found from the site's own version list (`vue-router@3` is
`v3.router.vuejs.org`, `django@5.2` is `/en/5.2/`); with no version, the
current docs. Only that version's tree is kept: other hosts, paths outside a
versioned root, trees the version list names as other versions, release or
channel directories right below the root (Docusaurus's `/docs/2.x/` and
`/docs/next/`, mike's `/1.0/`), and trees made mostly of copies of the current
pages are left out. Trees that are no version, such as a REST reference's
`/api/v1/`, stay. Several versions of a library sit side by side.
`--library name@version` answers from that version's index; a version that is
not indexed takes the live path unless `--offline` is set. The live path
must verify the requested release and read its docs; it cannot substitute
another version or an unrelated site.
Without a version the newest indexed one answers (the current docs before any
release): `version:` says which, and a note names the others. A page in the
indexed tree about a later major, such as a v3 page "Migrating to v4", is kept,
labelled with that version, and ranks lower unless the question is about
migrating or names that version. Running `docs index` again refreshes the
library: pages come back through the page cache, only those whose content
changed are sectioned again, and pages the site no longer lists are dropped.

**Names.** `--name` adds a name queries may call the library by. Otherwise a
library answers only to its full identity: the name it was indexed by, its
package names with or without prefix and separators (`vue-router`,
`npm:vue-router`, `vuerouter`), its repositories (`vuejs/router`), its docs
site's own name (Vue Router, Beautiful Soup), the last part of a scoped package
or repository, its root's distinctive path segment (`bs4`) and its host's
distinctive label, also without a language word it joins (`httpx` for
python-httpx.org). One word of a longer name is no name (`vue` or `router` do
not name Vue Router). A host label yields to a library whose full name or path
segment it is (svelte.dev's `/docs/svelte/` and `/docs/kit/` are two
libraries), and a package named for its major release finds docs named without
the number (`beautifulsoup4` is Beautiful Soup's `/BeautifulSoup/`). A library of one page,
such as a specification, also answers to its document's title, the acronym the
document spells out for it, and a name in it with its version (`jwt`, `pkce`,
`oauth 2.0`, `rfc7519`). A name without a version answers from the current docs
of exactly that project, even when only an older version's index was built by
that name: with `angular@18` and
`https://angular.dev/` indexed, `--library angular` answers from angular.dev,
and `--library svelte` from Svelte's docs, never SvelteKit's.

**Ranking.** The index ranks by keywords alone and never runs a model: SQLite
FTS5's bm25 over each section, a heading counting three times the text, then
what the question's terms say about each section (how many it names, a heading
or page title that is the question, the terms in its code, a guide page ahead
of an API reference leaf). An embedding model fused with the keywords was
measured and left out: on 63 questions it changed no score by more than four,
at six times the query time (ADR-0007, amendments 5 to 7).

**Freshness.** Before an indexed answer prints, every page it cites that was
last read longer ago than `cache_ttl` is read again, the way it was stored
(the page, its Markdown source or its `llms-full.txt` chunk), within 3 seconds
in all. A page that changed is sectioned again and the answer ranked once
more. When a page cannot be read that way, or its check failed in the last 15
minutes, the answer prints as it is with `stale: unverified, built <date>`.
`--offline` makes no network requests: questions, resolution and section
lookups use only the local index. An unindexed library or section exits 3;
the page cache alone does not qualify. It cannot be combined with
`-b context7` (exit 2). An answer is marked stale when a cited page is older
than `cache_ttl`. Pages added to the site since the build appear only after
`docs index` runs again.

##### Context7

`-b context7` serves curated snippets and needs no key. Without one it runs on
Context7's anonymous quota: about 200 requests a month per IP address, shared
by everything on that address, and a docs search spends two (resolve, then
fetch). A spent quota exits **4** naming when it resets and the free key that
lifts it (`ketch config set context7_api_key <key>`). A library Context7 has
moved (`/facebook/react` is now `/react/react`) is followed once, and
`library:` shows the new ID. Its text answers keep 0.20's layout: a
`result_count` line, then each snippet under its title in brackets, with its
`source:`. With Context7, resolve the name, *vet the matches*, then fetch by ID:
its resolve never returns empty, and a bad name still gets confident fuzzy
matches, so check the name, not just the trust score.

##### Exit codes

A refusal for want of the question's terms prints its frontmatter with the
reason; every other row prints an error that names the cause.

| Exit | Reason | When |
| --- | --- | --- |
| 0 | | `found`, or `partial (terms_not_found)` or `partial (terms_outside_scope)` |
| 2 | `ambiguous` | A `--library` name several libraries share, or one resolve knows whose search agrees on no site: each library is listed |
| 2 | `ambiguous_site` | With no name resolve knows, the search results split between sites with none ahead: pass the docs URL as `--library` |
| 2 | `context7_id` | `--library /org/repo` without `-b context7` |
| 2 | `invalid_section`, `invalid_page` | A malformed `--section` reference, or `--page` past the last page |
| 2 | | `--page` without `--section`; `--section` with `-b context7` or `--resolve`; `--tag` with `--resolve` |
| 3 | `terms_not_found` | None of the question's distinctive terms is in the docs |
| 3 | `terms_outside_scope` | The library holds them, but none under `scope` |
| 3 | `scope_empty` | No indexed page is under the `--library` URL |
| 3 | `no_docs_found` | The search results hold no documentation |
| 3 | `not_found` | None of the site's pages could be read (the error counts the causes, such as `2 blocked (HTTP 403)`, and names the next step); an unknown `--section` page or anchor; a name nothing verifies, or a version its docs lack |
| 3 | `disallowed` | The site's `robots.txt` closes the pages |
| 3 | `no_results` | Nothing matched: rare on search, Context7's empty answer |
| 3 | `not_indexed` | `--resolve` with no name and nothing indexed, or a name of several words no indexed library matches |
| 4 | `search_empty` | Every web search came back empty: the providers are throttling |
| 4 | `timeout` | No answer within 45 seconds |
| 4 | | Every web search failed; a registry or GitHub down during `--resolve`; Context7's quota spent |
| 5 | `index_outdated` | A library indexed in an older format: `ketch docs index <library>` rebuilds it |

##### JSON and MCP

`--json` prints one object on every backend: the frontmatter's fields (the note
among them), `hint` (what the closing lines say), then `results`. A terms
refusal prints it with empty `results` and exits 3:

```console
$ ketch docs --library httpx --json "kerberos gssapi"
{"query":"kerberos gssapi","backend":"search","library":"https://www.python-httpx.org/","index":{"kind":"full","pages":23,"built":"2026-11-12"},"status":"refused","reason":"terms_not_found","terms":[],"not_found":["kerberos","gssapi"],"results":[]}
```

`index` is `{kind, pages, built}` or, on a live answer,
`{source, pages_read, cached}`. `terms` is a list of `{term, sections}`,
`defined_in` of `{term, section}`, `indexed_near` of `{title, library}`. Each
result has `library`, `version`, `title`, `breadcrumb`, `snippet` and `url`;
search's also have `anchors`, `section` (the reference `--section` takes),
`page_title`, `heading_path`, `contents`, `rows` and `same_text`, and a cut one
`truncated`, `shown_tokens` and `section_tokens`. A section fetch's result adds
`page`, `pages` and `next`.

The MCP `docs` tool takes `query`, `backend`, `library`, `tokens`, `limit`,
`resolve`, `section`, `page`, `offline` and `tag`, and returns the same fields
with `backend`, `matches` for `resolve`, and `warnings`. Its `hint` gives the
`docs(section="…")` call, and for a section fetch the call for the next page.
A refusal for want of the question's terms is a result with `status: refused`;
every other refusal or failure is a tool error carrying its exit code's prefix.

#### scrape — URL(s) → clean markdown

```console
$ ketch scrape <url>
$ ketch scrape <url1> <url2> <url3>      # concurrent batch
$ ketch scrape urls.txt                 # one URL per line
$ ketch scrape '["url1","url2"]'        # JSON array
$ echo "url1\nurl2" | ketch scrape      # stdin
```

| Flag | Meaning |
| --- | --- |
| `--raw` | Raw HTML instead of markdown |
| `--select <css>` | Extract only matching elements, skipping content selection |
| `--max-chars` | Truncate output, appending `[truncated]` |
| `--trim` | Strip markdown formatting, keep content text |
| `--no-llms-txt` | Disable `/llms.txt` detection for bare domains |
| `--force-browser` | Always render via the browser, skipping auto-detection |
| `--concurrency` | Max concurrent requests, default 5 |
| `--no-cache` | Bypass the page cache |
| `--cookie-file` | Netscape `cookies.txt` jar |
| `--user-agent` | User-Agent override; empty restores the default |
| `--tag <name>` | Bookmark each fetched URL under a tag; composes with `--no-cache` |

After configured URL rewrites, `scrape` and `crawl` fetch only absolute
`http://` and `https://` URLs. Other final schemes, including `file://`, are
rejected. Private or internal HTTP(S) addresses are still fetched when
reachable from the machine running ketch.

Content selection reads the page's own structure: the landmark it declares
(`main`, `article`), a document assembled from uniform sections, or the
smallest element holding its prose. Site furniture goes by what it is —
navigation, hidden and collapsed controls, link rails, tables of contents —
and readability is the fallback for a page that declares no structure. The
`extract_mode` config key sets what pruning may drop: `clean` (the default)
also drops blocks by name and phrase — related-post rails, comment threads,
share bars, "was this helpful?" boxes — for the leanest markdown; `complete`
keeps everything the structure does not condemn, trading a little precision
for the last fraction of recall.

#### extract — Piped HTML → markdown, no fetch

```console
$ curl -L https://example.com | ketch extract
$ cat page.html | ketch extract --select article --max-chars 4000
```

| Flag | Meaning |
| --- | --- |
| `--url` | Source URL for metadata and relative-link resolution |
| `--select <css>` | CSS selector to extract |
| `--trim` | Strip markdown formatting |
| `--max-chars` | Truncate output |

No fetch, no cache, no browser — just content selection and the markdown
pipeline, in the configured `extract_mode`. Useful when you already have the
bytes.

#### crawl — BFS or sitemap walk, foreground or detached

```console
$ ketch crawl https://docs.example.com --depth 2
$ ketch crawl https://docs.example.com --sitemap
$ ketch crawl https://docs.example.com --background
$ ketch crawl status [id]
$ ketch crawl stop <id>
```

| Flag | Meaning |
| --- | --- |
| `--depth` | Max BFS depth, default 3 |
| `--concurrency` | Worker pool size, default 8 |
| `--sitemap` | Treat the seed URL as a sitemap |
| `--background` | Detach and return a crawl id |
| `--allow` | Path substring filters |
| `--deny` | Regex deny patterns |
| `--cookie-file` | Netscape `cookies.txt` jar |
| `--user-agent` | User-Agent override |
| `--tag <name>` | Bookmark every crawled page under a tag |

A foreground crawl interrupted by SIGINT exits **0** with partial results, by
design.

#### tag — Bookmark sources and revisit them

```console
$ ketch search "guacamole ldap authentication" --scrape --tag remote-access
$ ketch tag add remote-access https://example.com/read-later   # no network
$ ketch tag show remote-access               # newest 50, with totals
$ ketch tag show remote-access --limit 0     # every entry
$ ketch tag list                             # every tag, with entry counts
$ ketch tag remove remote-access <url>...    # no URLs: drop the whole tag
```

| Flag | Meaning |
| --- | --- |
| `--limit` | `show`: newest entries to return, default 50; 0 returns all |
| `--minimal` | `show`: one page per line, tab-separated |

A bookmark keeps the source URL, a bounded title and description, and the
time it was tagged; full bodies stay in the page cache. With `--json`, `show`
reports `entries` and `cached` totals for the whole tag, `shown` for the
returned pages, and `cache_status` — `unavailable` when the page cache cannot
be read, in which case `cached: false` is unverified rather than wrong.
`tag add` takes absolute `http(s)` URLs and fetches nothing; anything else is
bad input, exit **2**. `tag remove` with URLs names the ones that were not
under the tag as `missing`; without URLs it drops the tag and says how many
entries went with it. Bookmarks survive cache expiry and `cache clear`, and
nothing expires them.

The index is its own `tags.db` beside the config file — `KETCH_TAGS_PATH`
overrides the filename — opened only for short operations, so an idle MCP
server or a background crawl never blocks it.

#### browser — Headless Chrome for JS-shell pages

```console
$ ketch browser status
$ ketch browser install    # download Chromium to ketch's cache dir
```

Scraping is fast path first: plain HTTP by default, with the browser used only
when a JS shell is detected. `--force-browser` overrides the detection.

A page that answers the plain fetch with HTTP 403 is retried through the
browser once, since bot walls often refuse ketch's HTTP client but let a real
browser through. This applies to `scrape` (including `--raw` and `--select`),
`search --scrape` and `crawl`, only when a browser is configured, and only for
403: a 429 is a request to back off and fails as before, as does every other
error status. When the browser gets the page, ketch prints
`notice: HTTP 403 for <url>; fetched with the browser` to stderr, never to
stdout or `--json` output, and the page is cached as a browser render. If the
browser fails too, or is itself answered with an HTTP error status (a wall
that refuses Chromium as well), the scrape fails with the original `HTTP 403 for <url>`
error and the browser's failure goes to stderr as a `warn:` line. Without a
browser configured, a 403 fails exactly as it always has.

This also covers `crawl --sitemap` seeds and child sitemaps. The browser
returns the original XML response, not its rendered XML viewer or stylesheet
output. A failed retry retains the original 403; sitemap fetches are not
added to the page cache.

#### config — Show, init, set, path

```console
$ ketch config                  # effective config + active backends, as JSON
$ ketch config init
$ ketch config set backend searxng
$ ketch config path
```

Plain `ketch config` is the discovery call: one invocation returns everything
an agent needs to know about what's active, including `*_key_set` presence
booleans that never reveal the key itself.

#### cache — Page-cache and tag-index stats, or clear the page cache

```console
$ ketch cache
$ ketch cache clear
```

bbolt-backed, 72-hour default TTL. Repeat scrapes and crawls read from cache
when their required data is present. Crawl entries also store every resolved
outgoing HTTP(S) link, including external URLs and query strings, beside the
page for the same TTL. This link metadata does not require or store full HTML,
and is not sent elsewhere. A deeper crawl fetches a still-fresh
page when its link metadata is unknown, and a later ordinary scrape refresh can
make the metadata unknown again. If that backfill fetch fails, crawl returns the
cached page and reports that link discovery is incomplete: on the CLI the
diagnostic goes to stderr while JSON page output stays on stdout; MCP returns
the page in `pages` and the diagnostic in `errors`; background status counts
the returned page and error, without retaining the individual diagnostic text.
The file is opened only for each read or write, so the CLI, background crawls
and any number of MCP servers share it. Expired pages are swept automatically.
`ketch cache` also reports the tag index — path, tag and entry counts.
`cache clear` deletes the page cache, returning its space, and leaves bookmarks
alone.

#### doctor — Live health check of every surface

```console
$ ketch doctor
```

Concurrent read-only probes against every backend (the `search` docs row
says how many libraries the local index holds, without the network), plus the browser,
the cache, and an informational `tags` row for the bookmark index that never
affects the exit code.
Each comes back `ok`, `no_key`, `unreachable`, `misconfigured`, or `skipped`.
Context7 without a key is `ok` and `keyless`, and doctor sends it nothing: it
reports the quota the last real Context7 response left (`~187 of 200 left as of
…, resets …`, or `quota not yet seen`), kept in a small file in ketch's cache
directory, so running doctor never spends the anonymous quota. A spent keyless
quota is `no_key`, since a key is the fix. With a key, doctor probes Context7 as
before.
Exits **0** when healthy and **5** when a configured surface is broken — so it
works in CI.

#### mcp — Run as an MCP server over stdio

```console
$ ketch mcp serve
```

The five surfaces plus `tag` as MCP tools, on the same config and backends as
the CLI. Every research tool accepts a `tag` option, and `tag` itself is the
one tool that writes locally and never touches the network. The `mcp_tools`
config key narrows the published set. `config`, `cache`, and `doctor` are
deliberately *not* tools — they're operator actions, not research surfaces.

#### version — Version, commit, build date

```console
$ ketch version
ketch v0.17.0
  commit: 8f41359
  built:  2026-09-16T22:51:07Z
  go:     go1.26.10 linux/amd64
```

## Workflows

### Bounded research query

Search, then fetch full content for every hit — bounded, because an unguarded
page can cost ~25k tokens.

```console
$ ketch search "raft consensus implementation tradeoffs" \
    --scrape --limit 5 --max-chars 6000 --trim
```

### Crawl a docs site into the cache

Crawl once in the background; every page lands in the cache, so later scrapes
are local reads.

```console
$ ketch crawl https://docs.example.com --background
→ crawl_id: c_a1b2c3d4

$ ketch crawl status c_a1b2c3d4
→ {"status": "running", "pages": 847, ...}

# once complete, individual pages come from cache — no refetch
$ ketch scrape https://docs.example.com/guide/auth
```

### Keep sources across sessions

Tag what a session finds; a later session lists the URLs, titles and
descriptions without searching again.

```console
$ ketch search "guacamole ldap authentication" --scrape --tag remote-access
$ ketch code "guacamole ldap" --tag remote-access
$ ketch tag show remote-access --minimal
```

### Federated search across providers

Rank fusion surfaces what several engines agree on, and tags each result with
who returned it.

```console
$ ketch search "postgres connection pooling pgbouncer vs pgcat" --multi
```

### Convert existing HTML

```console
$ curl -L https://example.com/post | ketch extract --trim --max-chars 4000
```

### Pages behind a session or consent wall

Export a Netscape `cookies.txt` from your browser, then attach it to any fetch.

```console
$ ketch scrape <url> --cookie-file ~/cookies.txt
$ ketch config set cookie_file ~/cookies.txt   # persist as a default
$ ketch scrape <url> --cookie-file ""          # disable for one run
```

Only cookies whose domain, path, and secure scope match are sent, re-evaluated
on every redirect. Values are never printed — not in output, JSON, errors, or
doctor. Respecting a site's terms and using only your own session cookies is
the operator's responsibility.

### Exit-code branching in scripts

```console
$ ketch search "$q" --json > out.json
$ case $? in
    0) jq -r '.results[].url' out.json ;;
    4) echo "upstream down, retry later" ;;
    5) echo "needs configuration — run ketch doctor"; exit 1 ;;
  esac
```

## Research playbook

These are the disciplines the bundled skill enforces, for agents and terminal
users alike.

- **Bound every fetch.** `--max-chars 4000–8000` plus `--trim` on any page you haven't seen. Skipping the cap should come with a reason.
- **Cite every claim.** A synthesis without source URLs isn't a deliverable.
- **Treat exit codes as control flow.** Classify before reacting; never retry a `2` or `3` unchanged.
- **Propose, then mutate.** `config set`, `browser install`, and installs are operator actions — confirm the exact command first, and never touch a value that's already working.

### Token budgets

| Call | Bound with | Measured cost |
| --- | --- | --- |
| `search, limit 5` | `--limit` | ~1.4 KB |
| `code, limit 3` | `--limit` | ~0.7 KB |
| `docs, default (search)` | `--tokens` (about 1,800 unless set) | ~7 KB; `--tokens 880` about 3.5 KB |
| `docs -b context7` | `--tokens` (4000) | ~3.3 KB |
| `scrape, unknown page` | `--max-chars` + `--trim` | crit: ~100 KB unguarded |
| `any list` | `--minimal` | roughly halves it |

### Worked session

Two queries, three scrapes, one corroboration — including a rate limit and a
source that never came back.

```console
$ ketch search "Go iter.Seq real-world gotchas" --limit 5
→ exit 4: [upstream] ddg rate limited
  # an explicit backend failed — rotate, don't retry unchanged

$ ketch search "Go iter.Seq real-world gotchas" -b brave --limit 5
→ 5 results, 4 unique hosts → picked 3 primary sources

$ ketch scrape <u1> <u2> <u3> --max-chars 6000 --trim
→ u1, u2 ok; u3 failed (503) — named in the synthesis, not dropped silently

$ ketch code "iter.Seq" --lang go --limit 3
→ 3 repos with file and line URLs, to corroborate real usage
```

Five claims, each cited to its URL. The unretrieved source is listed as
unretrieved. Where two sources conflict, the conflict is stated and attributed
rather than averaged away.

### Common mistakes

- **Bad** `ketch scrape https://docs.example.com` — no bound. You get llms.txt or ~25k tokens, whichever is worse.
- **Good** `ketch scrape https://docs.example.com/quickstart --max-chars 6000 --trim` — plus `--no-llms-txt` when you want the page itself.
- **Bad** `[upstream]` from ddg, so retry the identical call three times.
- **Good** Rotate to another provider from `available_backends`, retry once, and note the swap. When `auto` itself fails it has already tried every usable provider — retry once, then report the outage.
- **Bad** Fetch docs from resolve's first match because its trust score is high, even though the name isn't the library you asked about.
- **Good** Vet name, snippet count, and trust together. If no match names the intended library, say so instead of fetching junk docs.
- **Bad** Rerun `ketch docs` with more `--tokens` because a group is marked `(cut, N of M tokens)`.
- **Good** `ketch docs --section <its source>` — it returns that section, a page at a time when it is long.
- **Bad** Ask `ketch docs` the same library questions every session.
- **Good** `ketch docs index <library|docs-url>` once; later questions naming it answer offline.

## Backends

| Surface | Default | Also available |
| --- | --- | --- |
| `search` | `auto` | brave, ddg, searxng, exa, firecrawl, keenable, tavily, tinyfish, parallel, serpbase, degoog, serply, youcom |
| `code` | `grepapp` | sourcegraph, github |
| `docs` | `search` | context7 |

> **`auto` is a chain, not a provider.** It falls through
> `parallel → exa → keenable → youcom → firecrawl → ddg`, none of which need a
> key, and reports which one served.
>
> Set a key and `auto` promotes that provider ahead of the chain — you don't
> also have to set `backend`. Self-hosted SearXNG and Degoog instances are
> preferred over hosted APIs once configured.

#### Your own order — `search_order` pins the chain

```console
$ ketch config set search_order exa,tinyfish,brave
$ KETCH_SEARCH_ORDER=exa,brave ketch search "query"
```

With `search_order` set, `auto` tries only those backends, in exactly that
order: no promotion, no default ranking, nothing unlisted. Any provider may be
listed, including a keyed one or SearXNG on its localhost default. A listed
provider that can't run — its key or instance isn't set — drops out, and if
none is left `auto` fails naming `search_order`. The value is a comma list or a
JSON array; `auto`, unknown names and duplicates are rejected. An empty value
or `[]` restores the default order. `--multi`, `--random` and `-b <provider>`
ignore it, MCP `search` follows it, and `ketch doctor` reports the chain in
this order.

#### Keyless — Nothing to configure

`parallel`, `exa`, `keenable`, `youcom`, `firecrawl`, and `ddg` answer with no
setup. Keys for parallel, exa, keenable, youcom, and firecrawl are optional and
lift the hosted caps. ddg rate-limits readily under fan-out.

#### Keyed — Free key, then promoted by auto

```console
$ ketch config set brave_api_key <key>
$ ketch config set tavily_api_key <key>
$ ketch config set tinyfish_api_key <key>
$ ketch config set serpbase_api_key <key>
$ ketch config set serply_api_key <key>
```

Providers accept multiple keys for rotation — `brave_api_keys` takes a list,
and one is picked at random per request to spread rate limits.

#### Self-hosted — Your own instance, preferred once set

```console
$ ketch config set searxng_url http://my-searxng:8080
$ ketch config set degoog_url http://my-degoog:8090
```

An instance behind an auth proxy, such as Cloudflare Access, takes headers
keyed by its origin. Every backend with a configurable instance URL — SearXNG,
Firecrawl (`firecrawl_url`), Degoog, and Sourcegraph (`sourcegraph_url`) —
sends them, on searches and on `ketch doctor` probes:

```console
$ ketch config set http_headers '{"https://searx.example.com":{"CF-Access-Client-Id":"<id>","CF-Access-Client-Secret":"<secret>"}}'
```

Headers go only to requests whose scheme, host, and port match the key, so a
redirect to another origin, or a `--searxng-url` pointing elsewhere, never
carries them. A header the provider sets itself, such as Firecrawl's
`Authorization`, wins. `ketch config` lists the header names, never the
values, and neither `ketch doctor` nor an error message ever prints one —
anything an instance echoes back, in a response body, a redirect, or the URL of
a failed request, is redacted. Setting `http_headers` replaces the whole map; `'{}'` clears it.

When an instance with headers configured answers 401 or 403, or redirects to
another origin such as a login page, `ketch doctor` reports it as
misconfigured and names the origin and its header names, so an expired or
mistyped token is not mistaken for a backend problem. A 401 or 403 can also
come from the instance itself, so the backend's own advice follows the hint.

#### Code and docs — grepapp, sourcegraph, github, search, context7

Grep and Sourcegraph need nothing. GitHub uses `gh auth login`,
`$GITHUB_TOKEN`, or `ketch config set github_token <tok>`. The `search` docs
backend needs nothing: it finds docs sites through the `search` surface's
providers (so a key or `search_order` there helps it too), and answers from
libraries `ketch docs index` indexed with no search. Resolving a library name asks GitHub
about its repository, and the same GitHub token lifts the 60-requests-an-hour
limit an unauthenticated install gets; rate-limited, it still resolves from the
registries, and says so in the match's evidence. Context7 works
with no key on its anonymous quota (about 200 requests a month per IP);
`ketch doctor` shows it as `keyless` with the requests left as of the last
Context7 response and when they renew (without spending one), and a free key via
`ketch config set context7_api_key <key>` lifts the cap. Context7 answers a
call that names it (`-b context7`, MCP `backend: "context7"`). The
`docs_backend` config key is retired: a stored value (often `context7`, the
old default) is left in the file and ignored, and `ketch config set
docs_backend` exits 2 saying so.

## Configuration

Defaults live in `~/.config/ketch/config.json`.

```console
$ ketch config init                    # write a default config file
$ ketch config set backend searxng
$ ketch config set browser chrome      # JS-rendered page fallback
$ ketch config                         # effective config + active backends
```

Every scalar key can also come from the environment as `KETCH_` plus the
upper-snake key name — useful in containers and CI where writing a file is
awkward.

```console
$ KETCH_BRAVE_API_KEY=<key> ketch search "query"
$ KETCH_BACKEND=ddg KETCH_LIMIT=10 ketch search "query"
$ KETCH_CONFIG=/etc/ketch/config.json ketch config
```

> **Precedence:** CLI flag → `KETCH_*` env → config file → built-in default.

#### Environment details — Lists, tokens, and what's file-only

- Per-provider key vars accept a comma-separated list and replace the provider's whole key pool — there are no plural `*_API_KEYS` vars.
- `KETCH_SEARCH_ORDER` and `KETCH_MCP_TOOLS` take a comma-separated list.
- `KETCH_HTTP_HEADERS` takes the same JSON object as `http_headers` and replaces it whole.
- `KETCH_GITHUB_TOKEN` beats the config file, which beats an ambient `$GITHUB_TOKEN`.
- `url_rewrites` and `spa_markers` are file-only — their JSON and regex values don't survive env quoting.
- `ketch config` reports an `env_overrides` section, so you can always see which values came from the environment.
- Invalid values fail loudly, naming the offending variable. Secret `KETCH_*` vars are stripped from spawned subprocesses.
- A config file that isn't valid JSON, or gives a key the wrong type, fails the same way (exit 2), naming the key and the type it needs. `ketch config set` won't overwrite it; fix it by hand.

#### Page cache — bbolt, 72h TTL, shared across processes

```console
$ ketch cache          # stats
$ ketch cache clear
$ ketch scrape <url> --no-cache
```

The page cache opens bbolt only for individual operations, so an idle MCP
server holds no cache lock and can share the cache with CLI and background
crawls. Bookmarks are separate: `tags.db` is also opened only for short index
operations.

#### Browser rendering — Fast path first, Chrome on detection

```console
$ ketch config set browser chrome       # from PATH
$ ketch config set browser /usr/bin/google-chrome-stable
$ ketch browser install                 # download Chromium
$ ketch browser status
```

#### Other keys — Rewrites, SPA markers, user agent, extraction mode, PDF converter, instance headers

- `url_rewrites` — regex rewrites applied before fetch
- `spa_markers` — extra tokens for JS-shell detection
- `cache_ttl` — page cache lifetime; docs also use it for repeated questions and to decide when an indexed page an answer cites is read again
- `user_agent` — User-Agent override for HTTP and browser fetches
- `extract_mode` — `clean` (default) or `complete`; a non-default mode scopes cached pages, so a page cached under one mode is never reused under the other
- `mcp_tools` — allowlist of MCP tools to publish; unset publishes all six
- `search_order` — the `auto` chain, exactly: backend IDs tried in this order; unset keeps the default (see Backends)
- `external_pdf_to_md_converter_command` — external PDF-to-Markdown converter; must contain exactly one `{input}` placeholder. Once set it is authoritative, with no silent fallback
- `http_headers` — headers for self-hosted SearXNG, Firecrawl, Degoog, and Sourcegraph instances, keyed by origin (see Self-hosted under Backends); values are secrets

## Exit status

Every failure mode has a number, so a script or an agent can branch on the
outcome instead of pattern-matching an error string.

| Code | Meaning | What to do |
| --- | --- | --- |
| ok: 0 | Success | Read the result |
| warn: 2 | Bad input | Fix the call — retrying unchanged can never succeed |
| warn: 3 | Nothing matched | Change the query or selector; not an outage |
| crit: 4 | Upstream or network failure, or a timeout | Rotate to another provider, or retry once |
| crit: 5 | Missing precondition | Stop and configure — run `ketch doctor` |
| warn: 6 | Cancelled | Rerun with a smaller scope |

The MCP server carries the same taxonomy, as stable message prefixes:
`[validation]`, `[not_found]`, `[upstream]`, `[precondition]`, `[cancelled]`.
One asymmetry: a CLI crawl interrupted by SIGINT exits **0** with partial
results, by design.

## For agents

Instead of teaching an agent a search API, a code API, and a docs API — each
with its own auth and response shape — give it one binary and five surfaces.

```console title="System prompt snippet"
Use `ketch` for external research — web pages, OSS code, library docs.
- `ketch search "query"` / `--scrape` for results with full content
- `ketch scrape <url> [url...]` for clean markdown from one or more URLs
- `ketch extract` for already-fetched HTML piped in
- `ketch code "query" --lang go` for real OSS code with line context; `--repo owner/name` searches one repository
- `ketch docs "question"` for library docs from the docs site itself (`--library django@5.2` or a docs URL names it); text in a group marked `(cut)` is unread until `ketch docs --section <its source>` reads it
- `--tag <name>` on any of them keeps the sources; `ketch tag show <name>` brings them back
All commands support `--json`. `ketch config` reports active backends.
Bound every scrape with --max-chars and --trim. Cite every claim.
```

### Bundled skill

The repo ships a fuller playbook as a skill, covering surface routing, token
budgets, error-code control flow, a deep-research recipe, and guided backend
setup. Any agent that loads `SKILL.md`-style files can use it.

Read it at [skills/ketch/](https://github.com/1broseidon/ketch/tree/main/skills/ketch).
Most of this page's playbook and notes come from it.

### MCP server

For agents that speak MCP rather than shelling out, the same five surfaces,
plus `tag`, run as tools over stdio, on the same config and backends as the CLI.

```console
$ claude mcp add ketch -- ketch mcp serve

# or with no install step at all
$ claude mcp add ketch -- npx -y ketch-cli mcp serve
```

The npm package carries the binary in a per-platform dependency, so `npx` runs
it without a postinstall download. That keeps the server's cold start quick
when a client relaunches it.

> **Network posture matters.** After configured URL rewrites, MCP `scrape` and
> `crawl` fetch only absolute HTTP(S) URLs; schemes such as `file://` are
> rejected. Private or internal HTTP(S) addresses remain fetchable when
> reachable from the server. Give it
> the network posture you'd give the agent itself.

### Claude Code plugin

ketch only needs the CLI on PATH. The repo also works as a plugin marketplace
that installs the MCP server and skill together.

```console
$ claude plugin marketplace add 1broseidon/ketch
$ claude plugin install ketch@ketch
```

### pi package

pi can connect to `ketch mcp serve` like any MCP client, but the repo is also a
pi package with a tighter fit: the bundled skill plus `ketch_search`, `ketch_code`,
`ketch_docs`, `ketch_scrape`, `ketch_crawl` and `ketch_tag` tools that shell
out to `ketch … --json`, with the same per-call options and `[validation]`
… `[cancelled]` error prefixes as the MCP tools, and a `/ketch-doctor` command.

```console
$ pi install git:github.com/1broseidon/ketch
```

Pin a release with `pi install git:github.com/1broseidon/ketch@v0.20.2`. Results
render as compact summaries, with the full response behind Ctrl+O.

It never bundles the binary. It uses `ketch` from PATH and otherwise falls back
to the pinned `npx -y ketch-cli`, so Node alone is enough; set
`KETCH_PI_NO_NPX=1` to turn that off, and `KETCH_PI_BIN` to name a specific
binary. On Windows it runs the native `ketch.exe` behind npm's `.cmd` shim,
because Bun cannot pass an argument holding a cmd.exe metacharacter to a batch
file. The crawl tool is
bounded like the MCP one (`max_pages` default 30, cap 100, 3 minutes). Details
in [plugins/pi/](https://github.com/1broseidon/ketch/tree/main/plugins/pi).

### Bootstrap files

Two files on this domain are written for agents rather than people:
[/llms.txt](/llms.txt) is the short index and [/llms-full.txt](/llms-full.txt)
is this manual as plain Markdown.

## Notes

- Scraping a **bare domain** auto-probes `/llms.txt` and may return that instead of the homepage. The `title` field reveals the swap; `--no-llms-txt` opts out.
- **Batch scrape reports per-URL failures inside a successful call.** The command exits 0 with per-result errors set — check every entry, don't just check the exit code.
- **Context7's resolve never returns empty.** Garbage in gets confident fuzzy matches out, so vet the name rather than trusting the score. `search`'s `--resolve` returns only verified docs roots, with their evidence, and exits 3 when nothing verifies.
- **Regex is per-backend.** grepapp and sourcegraph accept it; github rejects it with a pointer to the other two.
- **Background crawls are CLI-only.** The MCP `crawl` tool is synchronous and capped at 30 pages by default, 100 hard, three minutes wall clock.
- **The page cache is shared across processes.** An idle MCP server holds no page-cache lock; individual operations open and close the bbolt file.
- **Bookmarks are not cache entries.** `cached: false` in `tag show` means no fresh local body, not a dead link; the bookmark stays until `tag remove`.
