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
> fresh install, and `ketch docs` searches through it. Configuration raises limits and picks favourites; it is never
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

#### docs — Library documentation, read from the web or Context7

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
| `--tokens` | Budget. `search`: 4,000 tokens of docs text, a hard bound; the frontmatter and lists add about 600. `context7`: 4000. With `--section`: the page size, 4000 |
| `--limit, -l` | Max results. `search`: no cap unless set. `context7`: the configured `limit` (5), none with `--library` unless set |
| `--resolve` | Resolve the query as a library name: docs roots (`search`; with no name, every indexed library) or Context7 IDs |
| `--section <url#anchor>` | Return one section, with no query and no ranking (see Full sections) |
| `--page N` | With `--section`: the page of a long section to return (default 1) |
| `--offline` | Use only the local index, with no network requests; a missing library exits 3 |
| `--decisions` | `typesafe`, `openai`, `auto` or `off`: the decision provider for a question that names no library, for this call (default: `docs_decisions`; see Decision providers) |
| `--minimal` | One result per line: URL, library, first line; `note`, `stale` and `not_on_pages` (or `not_in_index`) go to stderr |
| `--tag <name>` | Bookmark every result URL under a tag |

`search` answers from the documentation itself, with no key, on one of two
paths. A question that names a library indexed with `ketch docs index`, in its
words or with `--library`, answers from the index with no search, in tens of
milliseconds. Any other question takes the live path: a web search, then the
first result that is a readable document and up to two more pages of its site,
within 45 seconds (see Live answers). With a decision provider's key set, a
question that names no library reads the top five results instead, and the
provider labels them and picks the passages (see Decision providers).

A question saves nothing. The pages it read stay in the page cache for
`cache_ttl` (72h by default). The same question again within that time, with
the same terms after filler words and the same `--library`, prints the same
answer with no search and no fetch. An answer made with a decision provider is
the exception: each call asks the provider again. Only `ketch docs index` adds
libraries or pages to the index.

##### Answers

An answer is a frontmatter that says what was read and what is missing, then
the docs' own text, verbatim. It carries no verdict: whether the text answers
the question is for the reader to judge. With no decision provider configured:

```console
$ ketch docs "httpx timeout configuration"
---
query: httpx timeout configuration
read:
  - https://www.python-httpx.org/advanced/timeouts/ (search rank 1)
  - https://www.python-httpx.org/quickstart/ (search rank 6)
not_on_pages: []
---
## Timeouts
https://www.python-httpx.org/advanced/timeouts/

HTTPX is careful to enforce timeouts everywhere by default.

The default behavior is to raise a `TimeoutException` after 5 seconds of network inactivity.

source: https://www.python-httpx.org/advanced/timeouts/

### Setting and disabling timeouts
…
### Fine tuning the configuration
…
source: https://www.python-httpx.org/advanced/timeouts/#fine-tuning-the-configuration

## QuickStart
https://www.python-httpx.org/quickstart/
…
### Timeouts
…
source: https://www.python-httpx.org/quickstart/#timeouts
…

## Not printed
- Sending Binary Request Data — https://www.python-httpx.org/quickstart/#sending-binary-request-data (78 tokens)
- Sending JSON Encoded Data — https://www.python-httpx.org/quickstart/#sending-json-encoded-data (139 tokens)
…

## Other results
- How to Use httpx for Async HTTP Requests — https://oneuptime.com/blog/post/2026-02-03-python-httpx-async-requests/view — import httpx # Single timeout for everything client = httpx.AsyncClient(timeout=10.0) # Granular timeouts timeout_config = httpx.Timeout( connect=5.0, # Time…
…

unread until you read it: a cut or unprinted section with ketch docs --section <url#anchor>; another result with ketch docs --library <url> "<your question>"
```

The frontmatter prints these lines, in this order, each only when it applies:

| Field | Says |
| --- | --- |
| `query` | The question |
| `library` | The docs root of an indexed answer, or of the library `--library` named. A question that names no library has none: `read` says where the text is from |
| `scope` | The URL below that root the answer is limited to, when `--library` named one |
| `version` | An indexed library's version. On a live answer, only a `--library` pin that matched, or a release the question names in words (`Vue 2`) that a page read is marked with |
| `index` | `full, N pages, built <date>` for an indexed library (`capped` when `--max-pages` left pages out) |
| `decisions` | The decision provider that labelled the sources, and what the token cap let it judge when the cap cut something (see Decision providers) |
| `read` | The pages read, best first, each with its search rank, or why a page that is no search result was read. With a decision provider, each also has its label and confidence |
| `skipped` | Results that could not be read, each with its cause: `not found, HTTP 404`, `blocked, HTTP 403`, `timed out`, `needs JavaScript`, `almost no text`, … |
| `passed_over` | With a decision provider: the top five results not printed, each with its label and why |
| `widened` | With `--library` only: `read N more pages of <root> for: <identifiers>` (see Live answers) |
| `not_on_pages` | The identifiers the question writes (code names, flags, option and host names) on none of the pages read. Only those pages were checked, so it never says the docs lack them. Plain words are not listed: `[]` is the common case |
| `not_in_index` | For an indexed library, the question's distinctive terms it holds nowhere |
| `outside_scope` | With `scope`: the terms the library holds only outside it |
| `origin` | A live answer for a named library: whether the site is its documentation (see Live answers) |
| `stale` | `unverified, built <date>` when a cited page could not be checked (see The index) |
| `defined_in` | `{term: url#anchor}`: for a term that is the first cell of a table row in a section the answer does not print, that section; at most three |
| `indexed_near` | `{title: root}`: when `--library` named no indexed library, the indexed ones near it; the answer ends with a line saying to pass one as `--library` |
| `note` | What the fields do not say: that a second search ran, that no page was printed and why, that pages read hold none of the question's terms, a release the question names that no page read is marked with, other indexed versions of the library; with a decision provider, that no page read is the makers' docs, a linked docs site tried, or why the answer was made without the provider |
| `next` | With a decision provider: the call worth making next, with why |

Distinctive terms are the identifiers the question writes and its rare words.
On an indexed library a word is rare when it is in under 1% of the library's
sections or in at most 25 of them. A number of four or more digits matches its
grouped form in the docs: `10000` finds `10,000`.

The body prints each page's groups under `## <page title>` and the page's
address, pages best first, a page's groups in page order. A group is its
heading path below the page title (`### <heading> > <subheading>`; a page's
lead section has none, or `### (top of page)` when it is not the page's first
group), the section's text, and `source: url#anchor`.

`--tokens` (4,000 by default, live and indexed alike) bounds the docs text.
The frontmatter, the two lists and the last line are outside it and add about
600 tokens, rarely 800. Sections print whole while they fit, the best page's
first. A section or block larger than what is left is cut: its source line ends
`(cut, N of M tokens)` and `…` marks text left out. A block too long for the
budget on its own (a man page served as one block, a long code block) keeps the
lines that hold the question's terms, each with two lines of context; a code
block keeps its fences and a table its header. A cut group may print
`also in this section:` with what it left out by name, and a line under its
heading such as `option rows, 2 of 17` or
`same text in 1 other section: AsyncClient`. Only an explicit `--limit` caps
the number of groups.

`## Not printed` lists the other sections of the pages read (for an indexed
library, of the pages printed from), those holding the question's terms first,
with their size, in about 200 tokens; `(+n more: …)` gives the call that lists
the rest of a page. `## Other results` lists up to five search results not
read. The last line says what is unread and how to read it: text that is cut,
behind `…` or only listed is unread until `--section` reads it, and another
result until `ketch docs --library <url> "<your question>"` asks it the
question. A `--library` URL with nothing under it, such as a blog post, is
read as that one page.

##### Full sections

`--section <url#anchor>` returns one section and its subsections, with no
ranking: from the index, or for a page a live answer read, from that page
through the page cache (fetched again once it has expired). Pass a group's
`source`, an entry under Not printed, a `defined_in` target, or a page URL with
no anchor for the page's sections in order. `--library` is optional. A section
that fits `--tokens` (4000 by default) comes whole. A longer one comes a page at
a time, broken between blocks; a block longer than a page is broken at its
lines (a code block into fenced blocks), a line longer than a page at words.
Its last line is the command for the next page:

```console
$ ketch docs --section "https://www.python-httpx.org/api/#client" --tokens 600
---
library: https://www.python-httpx.org/
page: 1 of 4
---
### Developer Interface > Client

*class* `httpx.Client`(*\**, *auth=None*, *params=None*, *headers=None*, …)
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
a `name@version` of its own (`django@5.2`) is used only when resolve confirms
that version; otherwise it is read as words.

A name an indexed library answers to (see The index) answers from the index. A
URL below an indexed root limits the answer to the pages under it: `library:`
is the root and `scope:` the URL. Any other value takes the live path.

`--resolve` shows the docs root a name resolves to, and why. With no name it
lists every indexed library, offline, and a name an indexed library answers to
lists that one:

```console
$ ketch docs --resolve
https://www.python-httpx.org/  HTTPX  (indexed: yes, full, 23 pages, built 2026-11-16)
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

A question that names no library runs one web search, the plain question, as
`ketch search` runs it, through the `search` surface's providers (the `auto`
chain, or your `search_order`). ketch reads the first result that is a readable
document, then up to two more results of the same site and release, in search
order, and answers from them as from an indexed library.

- **Not documents.** Q&A and forum threads, issues, pull requests, commits,
  videos and social posts are passed over by their address. On a code forge, a
  repository's file is a document only when it is Markdown, reStructuredText,
  AsciiDoc or `.txt`, a README or a wiki page; source code, a schema or a config
  example is passed over. A blog post or a tutorial is a document.
- **Unreadable results.** A result that fails, or reads to almost no text with
  none of what the question asks about, is listed under `skipped` with its
  cause, and the next result is read, on any site.
- **A second search.** When the search returns no results, or none that is a
  document that reads, one more search runs, the question + "documentation",
  read the same way, and the note says so. When neither gives a page to read,
  the answer is the search results alone, under `## Other results`, with a note
  saying why (exit 0). No results from either search exits 3.
- **robots.txt.** The pages a search returned are read as `ketch scrape` reads
  them, through the page cache, without consulting `robots.txt`. Widening, a
  site's page lists and `ketch docs index` honour it.
- **Indexed sites.** A result on a site you indexed answers from the index.
- **Pins.** A `name@version` the question writes is resolved beside the search
  and read as a pin when resolve confirms that version.

ketch reads what the search ranks first, which may be a blog post, a tutorial
or a third-party reference rather than the official documentation. `read`
shows each page and its rank; `--library` names the docs you mean. A release
the question names in words (`Spring Boot 2.7`) that no page read is marked
with, and that the printed text does not name, gets a note:
`the question names Spring Boot 2.7 and no page read is marked 2.7: they may document another release; pass that release's docs URL as --library`.

With `--library`, ketch searches the question together with the library, and
for its documentation. A docs URL is the site read (a page URL, that page
first). A name or spec is resolved while the search runs, and the docs root
resolve verifies is read, unless the site the search ranks first is that
project's own. On a host that serves many projects, such as pkg.go.dev, only
the named project's path is read (`net/http` is
`https://pkg.go.dev/net/http/`). The site's search results are read at their
exact addresses, up to three of one release. Only when an identifier the
question writes is on none of them, or nothing on them matches, are more pages
of that release read (the site's other results and their neighbours in its
page lists, up to 24, honouring `robots.txt`), printed after the exact pages;
`widened` says how many and for which identifiers. When the site gives nothing
readable, the domain's next docs tree is tried, then the next site, two sites
at most, and the note says which could not be read and why. When the root
resolve found holds none or only some of the question's terms and the search
favours another site, that site is read in the same call, the better answer is
kept, and `origin` says so.

An answer for a named library says whether the site is its documentation:
`origin: verified, <evidence>` (the library value names the site; the root
resolve verified, with the search agreeing; the project's own site by its
registry metadata or repository; a standard-library page of the language's
docs), or
`origin: unverified, ketch could not confirm <host> as the official documentation of <library>`,
which says only that: the site may still be official. When only a name ties
the site to the library, no origin line prints. A question that names no
library has none; `read` says where its text is from.

##### Decision providers

A decision provider is a model API that labels the sources a search returns
and picks the passages that answer the question. It is opt-in: with no key
set, nothing above changes and no model is involved.

```console
$ ketch config set typesafe_api_key <key>        # or openai_api_key
$ ketch docs "httpx timeout configuration"       # auto: the provider with a key
$ ketch docs --decisions off "…"                 # this call without one
$ ketch config set docs_decisions off            # every call without one
```

`docs_decisions` (`KETCH_DOCS_DECISIONS`) is `auto` (the default), `off`, or a
provider: `typesafe` (TypeSafe's System One API, model `jev-1.14.0`) or
`openai` (OpenAI's Decisions API, model `gpt-6-luna`). `auto` uses TypeSafe
when its key is set, else OpenAI when its key is set, else none.
`--decisions`, and `decisions` on the MCP and pi `docs` tools, choose for one
call, except that the operator's `off` (in the file or `KETCH_DOCS_DECISIONS`)
is final: a call asking for a provider then gets the plain answer and
`note: decisions are off in the operator's config (docs_decisions)`. A
per-call `off` always works. An unknown value exits 2. A provider named
without its key answers without it, and the note says which key to set.

Only a question that names no library goes to a provider. `--library`, an
indexed library (named, or one of the top five results on an indexed site),
`--offline`, and a `name@version` that resolve confirms are answered as
described above, and nothing is sent. Nor is a page read with your
`cookie_file` cookies: it gets no label, and its `passed_over` line says
`(unlabelled, search rank N): read with your cookies: not sent to the decision provider`.
When every page read was, the answer is the plain one with
`note: decisions unavailable: every page read was read with your cookies, and none is sent to the decision provider; answered without them`.

With a provider, the search runs as above, then its top five results are read
at once (a result that is no document by its address is not read). Each page
read goes to the provider with the question: its address, title, headings and
up to about 8,000 tokens of its text. The provider gives probabilities: is the
page documentation, is it from the makers of what the question asks about,
does the question name a release, does the page document another one, and
which kind of page it is. Then each passage of the first three readable pages,
and of the other documentation pages, is asked whether it helps answer the
question. The results after the top five are labelled from their title,
address and snippet. ketch reads every probability against the provider's
cut-offs (both providers: `low` 0.3, `high` 0.7, `official` 0.8, `very_high`
0.9) and decides by its own rules:

- **Labels.** Documentation is `official docs` (makers at or above `official`),
  `third-party docs` (makers at or below `low`) or `docs`. Any other page is
  its kind: `reference page`, `guide`, `article`, `forum thread`,
  `discussion`, `source code`, `marketing page` or `other`, as
  `official <kind>` when it is the makers'. `, other release` is added when
  the question names a release and the page documents another.
- **What prints.** The page printed first is one with a passage rated `high`
  on the question, else `medium`. Among those, official docs come first
  (unless they rank below third and other docs rank above them), then other
  docs, then any other page, each by search rank. Its passages print best
  first within `--tokens`, then strong passages of other pages while room is
  left.
- **Confidence.** On a `read` line, the lower of the label's band (official
  docs `high`, docs and third-party docs `medium`, any other page `low`) and
  the page's best passage; on a `source:` line, the lower of the label's band
  and that passage's rating. When the question names a release that no page
  read is marked with, nothing is above `medium`: that release check is
  ketch's own, as above.
- **`passed_over`.** `also on the question, not printed`, `weaker source than
  the page read`, `nothing on the question`, `documents another release`,
  `same page as the one read`, `not needed`, `almost empty`, `not a document
  by its address`, `read with your cookies: not sent to the decision
  provider`, `not labelled: the decisions budget was spent`, or why the page
  could not be read.
- **`next`.** `ketch docs --library <url> "<your question>"` for a better kind
  of source (read and not printed, or a result not read labelled official
  docs) or for a page on the question that the search ranked above the one
  printed; `ketch docs --section <url#anchor>` for a cut section with more on
  the question.
- **Notes.** `no result read is documentation from the makers or a reference;
  the page read is an article`; `the question names a release; the page read
  documents another one`; `no page read has a passage on the question; printed
  the first readable result, as the search ranks it`.

```console
$ ketch docs "sqlite WAL mode checkpoint starvation"
---
query: sqlite WAL mode checkpoint starvation
decisions: typesafe
read:
  - https://sqlite.org/wal.html?trk=public_post_comment-text (official docs, confidence high, search rank 2)
passed_over:
  - https://motitory.com/blog/sqlite-wal-checkpoint (article, search rank 1): also on the question, not printed
  - https://dev.to/nobody_4fb7fd10637d3b88a3/why-your-sqlite-wal-file-never-shrinks-304f (article, search rank 3): also on the question, not printed
  - https://zylos.ai/research/2026-10-16-sqlite-wal-reader-lifetimes/ (article, search rank 4): weaker source than the page read
  - https://sqlite.org/wal.html (official docs, search rank 5): same page as the one read
not_on_pages: []
next:
  - ketch docs --library https://motitory.com/blog/sqlite-wal-checkpoint "<your question>" (article, search rank 1, ranked above the page read, also on the question)
---
## 1. Overview
https://sqlite.org/wal.html?trk=public_post_comment-text (official docs)

### 6. Avoiding Excessively Large WAL Files
…
source: https://sqlite.org/wal.html?trk=public_post_comment-text#avoiding_excessively_large_wal_files (confidence high)
…
## Other results
- light/sqlite relay stalls under WAL checkpoint starvation (Socket… — https://github.com/happier-dev/happier/issues/189 (discussion) — …
- Why a long reader keeps SQLite WAL files growing - mubibai.com — https://mubibai.com/sqlite-wal-long-readers/ (article) — …
…
```

**A linked docs site.** When the page printed is not documentation, ketch
looks at the links on the pages read. A docs site linked from pages of two or
more of the sites read is read as `--library` reads a docs URL, and labelled.
It answers only when the provider labels a page of it official docs with a
passage rated `high` on the question: its `read` lines say
`linked from N of the pages read`, the search's pages move to `passed_over`,
and its other documentation pages read are listed under Other results as
`from the followed site, not printed`. Otherwise the answer is unchanged and
the note says `tried <site>, which N of the M pages read link to: <why>`. On
Hugging Face a link counts toward its owner's repository, never the host; code
forges, registries, social and video sites are never followed. This is new
and narrow: in testing it changed the answer on a minority of questions.

**Failures.** Any provider error, refusal or timeout (25 seconds for a call's
decisions) gives the answer as it would be without a provider, with
`note: decisions unavailable: <why>; answered without them`. Rate-limit and
overload statuses are retried within that time.

**Token cap.** One call sends the provider at most `docs_decisions_tokens`
input tokens: 80,000 unless set, a positive integer, or `unlimited`
(`KETCH_DOCS_DECISIONS_TOKENS`; `0` or a word exits 2). There is no flag or
tool parameter for it, so an agent cannot lift it. ketch counts the cap with
its own estimate before each request; `decisions.input_tokens` is the
provider's count afterwards. When everything fits, the cap changes nothing.
When it does not:

1. The results after the top five are labelled first, if their one request
   fits; otherwise they stay unlabelled.
2. Pages are labelled in search order while they fit; a page that does not
   fit, and every page after it, is `unlabelled`
   (`not labelled: the decisions budget was spent`).
3. The pages due a passage pass share what is left: each gets an equal share,
   and a page that needs less gives the rest to the others. Within a page the
   passages that best match the question's words go first, each whole or not
   at all.
4. When no page read is documentation and a linked docs site qualifies, that
   site is judged before the articles' passages.

A passage that was not judged is never printed; read its section with
`--section`. A cut call says what it judged:

```console
$ ketch docs "how do I write a custom lint rule for biome"
---
query: how do I write a custom lint rule for biome
decisions: typesafe (budget 80000 tokens, judged 179 of 232 passages)
read:
  - https://biomejs.dev/reference/configuration/ (official docs, confidence high, search rank 3)
passed_over:
  - https://github.com/biomejs/biome/blob/main/.claude/skills/lint-rule-development/SKILL.md (search rank 1): not found, HTTP 404
  - https://github.com/biomejs/biome/blob/main/crates/biome_analyze/CONTRIBUTING.md (search rank 2): failed, HTTP 503
  - https://biomejs.dev/linter/ (official docs, search rank 4): not needed
  - https://deepwiki.com/biomejs/biome/3.2-rule-configuration-and-options (search rank 5): blocked, HTTP 429
not_on_pages: []
note: decisions judged 179 of 232 passages (budget 80000 tokens; ketch config set docs_decisions_tokens to raise it)
---
…
```

The `decisions` line also says `labelled N of M pages` when pages went
unlabelled, and the note `did not label the other results` when those were
cut. `--json` carries it as `decisions.budget`:

```console
$ ketch docs --json "how do I write a custom lint rule for biome" | jq -c '.decisions'
{"provider":"typesafe","model":"jev-1.14.0","requests":6,"input_tokens":61337,"cutoffs":{"low":0.3,"high":0.7,"official":0.8,"very_high":0.9},"budget":{"tokens":80000,"judged_passages":179,"passages":232,"labelled_pages":2,"pages":2}}
```

A call the cap did not cut has no `budget`. A cap too small for one page's
labels answers without decisions:
`note: decisions unavailable: the decisions budget is smaller than one page's labels (1000 tokens); answered without them`.

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

Every answer exits 0, whatever it found. Every other row prints an error that
names the cause.

| Exit | Reason | When |
| --- | --- | --- |
| 0 | | An answer: docs text; pages or an index that hold none of the question's identifiers (`not_on_pages`, `not_in_index`, an empty body); or only the search results |
| 2 | `ambiguous` | A `--library` name several libraries share, or one resolve knows whose search found no docs site: each library is listed |
| 2 | `context7_id` | `--library /org/repo` without `-b context7` |
| 2 | `invalid_section`, `invalid_page` | A malformed `--section` reference, or `--page` past the last page |
| 2 | | `--page` without `--section`; `--section` with `-b context7` or `--resolve`; `--tag` with `--resolve`; `--offline` with `-b context7`; an unknown `--decisions` value |
| 3 | `search_empty` | No search returned results: for a question that names no library, neither its own search nor the second |
| 3 | `not_found` | A `--library` pin resolve cannot confirm, or whose docs cannot be read; an unknown `--section` page or anchor; `--resolve` or `docs index` with a name nothing verifies |
| 3 | `not_indexed` | `--offline` with no indexed library for the question; `--resolve` with no name and nothing indexed, or a name of several words no indexed library matches |
| 3 | `scope_empty` | No indexed page is under the `--library` URL |
| 3 | `terms_outside_scope` | The library holds the question's terms, but none under `scope` |
| 3 | `no_results` | Context7's empty answer |
| 4 | | The web search failed; no answer within 45 seconds; a registry or GitHub down during `--resolve`; Context7 failed or its quota is spent |
| 5 | `index_outdated` | A library indexed in an older format: `ketch docs index <library>` rebuilds it |

##### JSON and MCP

`--json` prints one object on every backend: the frontmatter's fields (the note
among them), `results` (the groups, in printed order), `outline` and
`outline_more` (Not printed), `other_results`, and `hint` (the last line). An
indexed library that holds none of the question's terms, exit 0:

```console
$ ketch docs --offline --library httpx --json "kerberos gssapi"
{"query":"kerberos gssapi","library":"https://www.python-httpx.org/","index":{"kind":"full","pages":23,"built":"2026-11-16"},"not_in_index":["kerberos","gssapi"],"results":[]}
```

`index` is `{kind, pages, built}`, `read` a list of `{url, rank}` (`via` for a
page that is no search result), `skipped` of `{url, cause}`, `widened`
`{pages, root, terms}`. `not_on_pages` and `not_in_index` are lists, `[]` when
nothing is missing. `defined_in` is a list of `{term, section}`,
`indexed_near` of `{title, library}`, `outline` of `{heading, section, tokens}`,
`outline_more` of `{page, count}`, `other_results` of
`{title, url, snippet, rank}`. Each result has `library`, `version`, `title`,
`breadcrumb`, `snippet` and `url`; search's also have `anchors`, `section` (the
reference `--section` takes), `page_title`, `heading_path`, `contents`, `rows`
and `same_text`, and a cut one `truncated`, `shown_tokens` and
`section_tokens`. A section fetch's result adds `page`, `pages` and `next`.
`backend` is present only for Context7.

An answer made with a decision provider adds `decisions`, `passed_over` and
`next`, and the probabilities behind every label:

```console
$ ketch docs --json "httpx timeout configuration" | jq -c '.decisions, .read[0]'
{"provider":"typesafe","model":"jev-1.14.0","requests":11,"input_tokens":43045,"cutoffs":{"low":0.3,"high":0.7,"official":0.8,"very_high":0.9}}
{"url":"https://www.python-httpx.org/advanced/timeouts/","rank":1,"label":"official docs","confidence":"high","tier":"A","context":"high","judgments":{"documentation":0.98,"makers":0.96,"names_release":0.05,"other_release":0.05,"kind":"reference_documentation","kind_probability":0.59,"passages":5,"top_passages":[0.97,0.97,0.96,0.95,0.95]}}
```

`decisions` gains `budget`, `{tokens, judged_passages, passages,
labelled_pages, pages}`, when the token cap cut something. `read` entries add
`label`, `confidence`, `tier` (`A` official docs, `B` docs
and third-party docs, `C` the rest), `context` (the page's best passage:
`high`, `medium`, `low`) and `judgments`; `passed_over` is a list of
`{url, rank, label, tier, context, reason, judgments}`, `next` of
`{command, why, library, section, label, rank, read}`. Each result adds
`source_label`, `confidence` and `probability`; each other result `label` and
`judgments` (`{makers, kind, kind_probability}`).

The MCP `docs` tool takes `query`, `backend`, `library`, `tokens`, `limit`,
`resolve`, `section`, `page`, `offline`, `tag` and `decisions`, and returns the
same fields with `matches` for `resolve`, and `warnings`. Its `hint` gives the
`docs(section="…")` and `docs(library="<url>", query="<your question>")`
calls, and for a section fetch the call for the next page. Every answer is a
result; every refusal or failure is a
tool error carrying its exit code's prefix.

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

A raw Markdown page (served as `text/markdown`, or as plain text at a `.md`
address) is kept as written: its title is its frontmatter's title, else its
first `#` heading, and the frontmatter is left out of the body.

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
before. Each decision provider has a `decision` row, `no_key` until its key is
set.
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
doctor. A page `ketch docs` read with them is never sent to a decision
provider. Respecting a site's terms and using only your own session cookies is
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
| `docs, default (search)` | `--tokens` (4,000 of docs text unless set; the frontmatter and lists add about 600) | ~16 KB |
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
backend needs nothing: it searches the web through the `search` surface's
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

#### Decision providers — typesafe, openai

```console
$ ketch config set typesafe_api_key <key>     # https://console.typesafe.ai
$ ketch config set openai_api_key <key>       # https://platform.openai.com/api-keys
$ ketch config set docs_decisions off         # keys kept, no call asks a provider
$ ketch config set docs_decisions_tokens unlimited   # lift the per-call cap (80000)
```

Optional, and used only by `ketch docs` for a question that names no library
(see Decision providers under docs). Setting a key turns it on:
`docs_decisions` is `auto` by default, which uses TypeSafe when its key is set,
else OpenAI. The keys can also come from `KETCH_TYPESAFE_API_KEY` and
`KETCH_OPENAI_API_KEY`; `ketch config` shows `typesafe_api_key_set` and
`openai_api_key_set`, never the key. `ketch doctor` gives each a `decision`
row: `no_key` without its key, otherwise a check of the key against the
provider's model list, which asks for no decision. A provider with a key set,
or named by `docs_decisions`, fails doctor when broken.

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
- `docs_decisions` — `auto` (default), `off`, `typesafe` or `openai`: the decision provider `ketch docs` asks; `off` holds against any per-call value (see Decision providers under Backends)
- `docs_decisions_tokens` — the input tokens one `ketch docs` call may send to its decision provider: a positive integer, or `unlimited`; 80000 unless set (see Token cap under docs)

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
- `ketch docs "question"` for library docs, read verbatim from the pages a web search ranks first (`--library django@5.2` or a docs URL names the docs); text in a group marked `(cut)` or listed under Not printed is unread until `ketch docs --section <its source>` reads it
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
