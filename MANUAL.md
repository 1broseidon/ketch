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
> fresh install. Configuration raises limits and picks favourites; it is never
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

#### docs — Version-aware library documentation, curated or from the docs site

```console
$ ketch docs "routing" --library /vercel/next.js          # Context7
$ ketch docs --resolve "next.js"                          # name → Context7 IDs
$ ketch docs -b web --resolve click                       # name → its docs site, no key
$ ketch docs -b web --library django@5.2 "middleware"     # one version's docs
$ ketch docs -b web "how do I set retries in requests"    # library named in the query
$ ketch docs -b auto "how do I set retries in requests"   # local index, then web, then Context7
$ ketch docs index fastapi                                # index a library once…
$ ketch docs -b local --library fastapi "dependencies with yield"  # …then answer offline
$ ketch docs -b local --section https://fastapi.tiangolo.com/tutorial/dependencies/#what-is-dependency-injection  # read a result in full
```

| Flag | Meaning |
| --- | --- |
| `--backend, -b` | `context7` (default), `local` (the local index), `web`, `auto` (local, then web, then Context7) |
| `--library` | A library ID from `--resolve`, or for `web` any library spec (below), or for `local` an indexed library's name, spec or docs root; skips the resolve step |
| `--tokens` | Token budget, default 4000 (`local`: about 1,800 for sections, 1,200 for snippets; set, `local` fits its sections to it, compactly; with `--section`, the cap of the section returned) |
| `--resolve` | Resolve the query as a library name or spec instead of searching |
| `--limit, -l` | Max results; with `--library`, only `--tokens` bounds them unless set |
| `--snippets` | `local`: code-first snippets (a code block with the sentences before it) instead of whole sections |
| `--section <url#anchor>` | `local`: return one section in full, from the index, given as the `source` a result carries; no query needed (below) |
| `--minimal` | One result per line |
| `--tag <name>` | Bookmark every result URL under a tag |

Context7 serves curated snippets and needs no key: without one it runs on
Context7's anonymous quota, which is small — about 200 requests a month per IP
address, shared by everything on that address, and a docs search spends two
(resolve, then fetch). When it runs out, `docs` exits **4** (`[upstream]`) and
says when the quota resets and how to lift it with a free key
(`ketch config set context7_api_key <key>`). A library Context7 has moved
(`/facebook/react` is now `/react/react`) is followed once, and the output's
`library:` line shows the new ID. `web` needs no key either: it
reads the library's own documentation site — its `llms.txt`, `llms-full.txt`,
sitemaps, or the links on its docs root — fetches the few pages that best match
the query, and returns their matching sections, each with its heading
breadcrumb and an anchor link, within `--tokens`. It never leaves the docs
root: the same host, and paths under it. Pages go through the scrape pipeline,
so URL rewrites, cookies, the browser fallback and the page cache apply.

A `web` library is one spec:

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
no version list answers only for the package's current major.

A name only becomes a docs site on evidence: the package's registry entry or
its GitHub repository names the site, or the homepage they name links to it as
its docs, or it is the package's own docs on docs.rs or pkg.go.dev. A site
found any other way, or linked as the docs from a homepage on another domain,
counts only when it links back to the repository, and a site found by search
only on the project's own domain too, so a mirror never does. A package's
repository counts only when the repository backs the claim: its GitHub homepage
is the package's site or registry page, or it is named like the package and
written in the registry's language with no conflicting site of the package's
own (a Go module's always counts). So a package that merely claims a popular
repository gets neither its stars nor its place. A same-named package with no such evidence is dropped, so
`click` is PyPI's Click, not npm's or crates.io's unrelated `click`. A bare
query names its library itself: `web` reads it from the query (`retries in
requests`, `react router loaders`) and searches the rest. It looks for the
name only where the query's wording puts one: in a question, after a closing
`in`, `with`, `using` or `for` (`how do I set retries in requests`), after
`use`, before `library` or `package`, or right after `does` or `is` (`how
does httpx follow redirects`); in a keyword query, its first word (`flask
blueprint url prefix`) unless an article follows it (`parse a date with
dayjs` is about dayjs). A language (`in python`) or an API name (`useState`)
is never the library, and a common word (`click`, `set`, `best`) names one only
when that library is widely used, since registries hold a package for nearly
every word. When two words of the query each name a different library, one
must far outweigh the other in downloads, or the query is ambiguous; a
question that names no library is refused rather than guessed.

```console
$ ketch docs -b web --resolve click
https://click.palletsprojects.com/en/stable/  Click  (trust: 10.0, version: 8.5.x, versions: stable)
  packages: pypi:click, github:pallets/click
  evidence: pypi click lists it as Documentation; github pallets/click homepage; links to github.com/pallets/click
```

The ID is the docs root URL; for a version, that version's root. Trust is on
Context7's 0–10 scale but scored from the evidence listed, and `packages` are
specs you can pass as `--library`. With `--json`, each match carries
`version`, `versions`, `evidence` and `packages`; Context7's matches have none
of them and print as they always have.

**Resolve once, then pass the ID.** A name is resolved again on every call:
for `web` that is a round of registry, GitHub and site requests before the
docs fetch even starts. `--library` with the ID from `--resolve` skips it.

`web` exits **2** for a malformed spec, when a bare query names no library it
can verify, and when a name is ambiguous: more than one verified project
answers to it, none far ahead of the rest on GitHub stars, and the error lists
each one's ID and package specs to pass as `--library` instead (`--resolve`
lists them all rather than failing). A same-named package whose only docs a
package index hosts (docs.rs, pkg.go.dev, pythonhosted.org), or whose
repository is gone, does not stand in a popular library's way unless it is at
least as popular; and a docs.rs or pkg.go.dev page never answers for a name
whose far more popular package in another registry has no docs site (npm's
`commander`, documented in its README): that name is ambiguous. It exits **3**
when the library you name is in no registry or repository, when nothing found
for it verifies (the error gives each candidate's reason), or when the version
you ask for is not offered (the error lists the versions that are). It exits
**4** when nothing verified and a registry, GitHub or the docs site failed or
was too slow, and, where one library is needed (`--library` with a name, or a
bare query), when a registry or GitHub's search failed at all: the library
that did not answer could be the one meant, so the error names the docs root
that would have been picked, to retry or pass as `--library`. A bare query
stops there too, rather than reading a weaker library out of the query, when
the failure could have changed which library it picked; a failed search for a
word no registry knows does not stop it. `--resolve` still lists what answered,
and each match's evidence then begins `this list may be incomplete:` with what
failed. When GitHub's rate limit was the cause, the error says how to set a
token.

With Context7, `docs` is a two-step: resolve the name, *vet the matches*, then
fetch by ID. Its resolve never returns empty. A bad name still returns
confident fuzzy matches, so check the name, not just the trust score.

**`auto` chains them, like `search`'s auto.** When the local index holds the
library (by its docs root, a name or spec it was indexed under, or the library
a bare query names), `auto` answers from it first, with no network at all; a
library the index does not hold is not even an attempt, and when the index
holds it but nothing matched, the chain carries on as below with the local
attempt reported. Next it asks `web`, which has
no quota, and asks Context7 only when `web` refuses: it identified no library,
the one named does not exist, a source failed, or the name is ambiguous. It
never replaces an answer `web` gave, an empty one included. An ambiguous name
is answered from Context7 only when Context7's pick is one of the candidates
`web` listed (the same GitHub repository, or the same docs site); otherwise the
ambiguity error stands, naming Context7's pick, since docs for the wrong
project are worse than none. For any other refusal Context7's pick must be a
library the query names — its title or repository name is a word of the query
(`react`, `next.js`) or two words run together (`react router`) — because
Context7 reads a whole question fuzzily: `useState in react` can come back as
some React Native library's docs. A library pinned to an ecosystem or a version
(`npm:react`, `react@18`, `@tanstack/query`) or given as a docs URL stays
`web`'s, because Context7's matches carry neither; an `owner/repo` goes to
Context7 only if it has exactly that ID. A Context7 ID passed as `--library`
(`/reactjs/react.dev`) goes straight to Context7, and `--resolve` lists
Context7's matches only when `web` has none. The output's `backend:` line, and
every result's `source` in `--json`, say which provider answered; when it was
Context7, `web`'s refusal is printed to stderr as a warning. When both fail,
the error is `web`'s, with its exit code, followed by Context7's. `auto` runs
only when you select it: an explicit `-b web` or `-b context7` never chains.
It is not the default yet; the default stays Context7 until keyless `web`
passes its accuracy gates.

##### The local index — index once, answer offline

```console
$ ketch model install                              # optional: bge-base (438 MB) for semantic ranking
$ ketch docs index https://docs.astro.build/en/    # a docs URL, taken as the root
$ ketch docs index fastapi                         # or a name or spec, resolved like web resolves one
$ ketch docs index django@5.2 --max-pages 800
$ ketch docs index vue-router@3                    # another version, indexed beside the current one
$ ketch docs -b local --library fastapi "how do I run code after the response is sent"
$ ketch docs -b local --library vue-router@3 "router.push onComplete onAbort"
$ ketch docs -b local "content collections schema in astro"   # a bare query names its library
$ ketch docs -b local --snippets --library fastapi "background tasks"   # code-first snippets
$ ketch docs index --dry-run numpy                 # the pages a build would index, in order
$ ketch docs index --list
$ ketch docs index --remove fastapi
```

`docs index` does the slow, judgment-heavy part of a docs query once per
library and version, and stores the result in `docs-index.db` in ketch's cache
directory (separate from the page cache and the bookmarks; `KETCH_DOCS_INDEX_PATH`
overrides it). A docs URL is taken as the root; a name or spec is resolved
exactly as `-b web` resolves one, so an ambiguous name exits **2** listing the
candidates and a name nothing verifies exits **3**, and you pass the docs URL
you mean instead. It then reads the site's own page lists (`llms.txt` and
`llms-full.txt`, its sitemaps, its search index and nav links), keeps the
pages under the root in its locale and version, honours `robots.txt`, fetches up to
`--max-pages` pages (1,500 by default) `--concurrency` at a time (4) through
the scrape pipeline and page cache, splits them into heading sections (never
inside a code block), cuts each section into snippet units (below), and embeds
each section with the installed model; a long section is embedded in several
windows so none of it falls past the model's 512-token window. Progress goes
to stderr; the summary (pages, sections, snippets, vectors, time, index size)
to stdout, or as JSON with `--json`.

**Which pages, when there are more than `--max-pages`.** The pages `llms.txt`
lists come first, in its order, then guides (paths such as `guide/`,
`tutorial/`, `user/`, `concepts/`, `getting-started/`), then everything else
(examples and galleries included), and last API reference leaves (a generated
reference such as Sphinx's `generated/` or Matplotlib's `_as_gen/`, TypeDoc's
`classes/`, pages two levels below `api/` or `reference/`) and release notes,
changelogs and blog posts; within a group, top-level directories take turns.
Nothing is left out for its group, only by the cap. One page listed as `x` and
`x.html` is one page, an `llms.txt` text mirror under `/llms/` is left out when
the rendered pages are listed, a translation's `x.html` is a copy of the
`x` that `llms.txt` lists (and is left out with its locale), and after fetching a page whose text is
another's (the same page at a second address, or a missing page a site
answers with its home page) is dropped. `--dry-run` plans the build and prints
the pages it would index, in order, each with its group (`curated`, `guide`,
`other`, `reference`), without fetching a page or writing the index.

**Where the docs are.** A docs root that stands for a release (its page
redirects, refreshes or names as canonical the same path at a numbered
release: duckdb.org's `/docs/stable/` is `/docs/2.0/`) is indexed at that
release. An unversioned root whose page lists sit almost wholly in release
directories right below it, or whose `llms.txt` lists mostly one of them
(`docs.crewai.com` and `/v1.15.35/`; Hugging Face's `/docs/transformers/` and
`/v5.22.1/`), is indexed at the release `llms.txt` lists, else the newest;
when that release's pages name the unversioned address as canonical, results
show the unversioned address. An unversioned root whose pages sit in two or
more release directories within two levels of it (sequelize.org's
`/docs/v6/` and `/docs/v7/`, with `/v5/` landing pages) is indexed at the one
its home page links to most, the site's default docs (else the newest final
release), and the others are left out as other versions. A version's own host whose page lists are an app
shell (`v18.angular.dev` serves its home page for `sitemap.xml`) is given the
current host's paths to try. The summary says when any of this happened.

**One index is one version.** `name@version` (or `--version`) indexes that
version's docs root, found from the site's own version list as `-b web` finds
it (`vue-router@3` is `v3.router.vuejs.org`, `django@5.2` is `/en/5.2/`); with
no version, the current docs. Only that version's tree is kept: other hosts
and paths outside a versioned root never qualify, and below the root a tree
the site's version list names as another version, a release or channel
directory right below the root (Docusaurus's `/docs/2.x/` and `/docs/next/`,
mike's `/1.0/`, `/latest/`), and a tree made mostly of copies of the current
pages are left out (the summary counts them). Trees that are no version, such
as a REST reference's `/api/v1/`, stay. The version is stored with the index,
and several versions of a library sit side by side, each under its own docs
root. `--library name@version` answers from that version alone; a version
that is not indexed exits **3** with reason `not_indexed`, naming the versions
that are. Without a version, the newest indexed one answers (the current docs
before any release): `version:` in the output and each result's `version`
say which, and a note names the others. A page in the indexed tree about a
later major, such as a v3 page "Migrating to v4", is kept, labelled with that
version, and ranks lower unless the question is about migrating or names that
version. Running it
again refreshes the library: pages come back through the page cache and only
those whose content changed are sectioned and embedded again, and pages the
site no longer lists are dropped. `--name` adds a name queries may call the
library by. Otherwise a library answers only to its full identity: the name
it was indexed by, its package names (with or without the ecosystem prefix
and separators: `vue-router`, `npm:vue-router`, `vuerouter`), its
repositories (`vuejs/router`), its docs site's own name (from `og:site_name`
or its home page's title: Vue Router, Beautiful Soup, SvelteKit), the last
part of a scoped package or repository, its root's distinctive path segment
(`bs4`) and its docs host's distinctive label, also without a language word
it joins (`httpx` for python-httpx.org, `poetry` for python-poetry.org),
never to one word of a longer name it goes by (`vue` or `router` do not name
Vue Router, `query` does not name TanStack Query); a host label yields to a
library whose full name or path segment it is (two projects documented on
one host, svelte.dev's `/docs/svelte/` and `/docs/kit/`, are not versions of
one library), and a package named for its major release finds docs named
without the number (`beautifulsoup4` is Beautiful Soup's `/BeautifulSoup/`). With only Vue Router indexed, `--library vue` or "vue
v-model on a custom component" is `not_indexed`, not Vue Router's docs. A name
without a version answers from the current docs of exactly that project, even
when only an older version's index was built by that name: with `angular@18`
and `https://angular.dev/` indexed, `--library angular` answers from
angular.dev, and `--library svelte` from Svelte's docs, never SvelteKit's.

`-b local` answers with no network, as **whole sections**. A question is
ranked against every section of the library twice, by cosine similarity (each
section's best window) and by keywords (BM25 over the section's heading, which
counts three times its text, its breadcrumb and its text), and the two
rankings are fused (reciprocal rank fusion). Sections are then grouped under
their second-level heading: a section that ranks brings the H2 it sits under,
and one result shows that H2's own text and the subsections of it that
ranked. The best five such results are returned (`--limit` changes it), never
two copies of one page (nor one page at two addresses, `x` and `x.html` or an
`/llms/` text mirror), at most two from one page while other pages rank next
(one from an API reference page), within about 1,800 tokens and none over
about 600. A section that fits is shown whole; a longer one keeps its lead
paragraphs, its first code block (cut, its fence closed, when even that does
not fit) and the blocks that name the question's words, `[…]` marking what
was left out.

**With `--tokens` (MCP `tokens`) the answer is fitted to that budget**,
counted as an LLM's tokenizer counts them (o200k; ketch's estimate is within
about 4% on a result), and rendered compactly so that four or five results
fit even at an agent's 880 tokens: each result gets a share of the budget
weighted by its rank (the first about 38% of it, the fifth about 10%; fewer
results when the fifth's share would be under about 90 tokens), and a
section family that does not fit its share keeps its heading path, its first
sentence, the code block that names the question best (else its first),
whole, with the sentence leading into it, the sentences that name the
question best, then whatever else names it; subsections that did not rank
are left out. Code is never cut; `…` joins sentences of one paragraph that
skip some, and one `[…]` ends a result that leaves anything out. What a
result leaves of its share goes to the others, best first. A generous
budget shows whole sections as the default does. Text is the docs' own,
code blocks inline. Each is printed as

````
### Page > Section
The section's text, its code inline.

```ts
the code, verbatim
```

#### A subsection that ranked

Its text.

source: https://site/page#anchor
````

and with `--json` (or over MCP) as a result whose `breadcrumb` is the heading
path, `snippet` the text, `url` the address of its best-ranked section,
`section` the same address as the reference `--section` takes, `truncated`
(`true` when the result leaves text of its section out) and `anchors` the
anchors of every section it shows (`""` for a page's lead).

**Reading the rest of a cut result: `--section`.** A compact answer can trim
away the one example a how-to question needs. The full section is in the
index, so reading it costs no network and no quota: in a compact answer
shown as text (`--tokens` set, not `--json` or `--minimal`), every result
that ends in `[…]` ends with a pointer after its `source:` line,

````
source: https://site/page#anchor
full section: ketch docs -b local --section https://site/page#anchor
````

and a result shown whole has none. `--section` takes that `url#anchor` (the
`source`, or `--json`'s `section`, which every local result carries, and
`truncated: true` marks a result that was cut) and returns the section as a
result of its own: its heading path, its text with every code block intact,
and `source:`, with no ranking, no embedding model and no network, in tens of
milliseconds. `--library` is optional (the URL names the indexed library; give
it, as a root or `name@version`, to be explicit). An anchor on a second-level
heading returns the whole family, that heading's text and the subsections under
it; on a deeper one, that section and what is under it; a URL with no anchor,
the page's sections in order. `--tokens` caps the answer (default 4000); a
longer section is cut at a block boundary, never inside a code block, and ends
with `[…] cut at 4000 tokens; next section: ketch docs -b local --section …`
naming the first section it did not show whole, or, when the cut falls in the
first, how large the section is and to raise `--tokens`. An unknown page or
anchor exits **3** with reason `not_found` (the message lists the page's
anchors), a malformed reference **2**, and `--section` with another backend
**2**. Over MCP the `docs` tool takes `section` the same way, `query` is not
required with it, and a compact answer's cut results end with the equivalent
call, `full section: docs(backend="local", section="https://site/page#anchor")`,
at the end of their `snippet`. The pointer is counted in the budget, so a
compact answer stays within it, at the cost of a result or a little text
(about 25 tokens per cut result).

`--snippets` (MCP `snippets: true`) returns **code-first snippets** instead.
At index time each section is cut into units: every fenced code block
(consecutive blocks, such as a TypeScript and a JavaScript tab, together) with
the one to three sentences just before it, and the prose no code block claims
(a section with no code gives its first ~600 characters, cut at a sentence
end). Units are the docs' own words, never rewritten; `…` marks a sentence cut
short. A query ranks the units by their section's cosine and the share of the
question's words they name, nudged up by the share of the identifiers the
question writes (`useEffect`, `res.json()`, `onComplete`) that a unit names
and for code (a single listing over 2,000 bytes is nudged down instead), and
returns the best ones, never two near-duplicates, at most two from one section
and three from one page, within about 1,200 tokens and 8 snippets (`--tokens`
and `--limit` change both), each printed as `### heading path`, the
explanation, the code and `source: url#anchor`. Every
result's `source` is `local`. A library the index does not hold exits
**3** with reason `not_indexed`; nothing matched exits **3** with
`no_results`. A library indexed by an older ketch (index format 1) is not
read: the query fails with reason `index_outdated` (exit 5) until `ketch docs
index <library>` rebuilds it, which it does on its own.

The embedding model is optional. Without one (`ketch model install` was never
run), `docs index` stores the text alone and `-b local` ranks it by keywords
(the same ranking `web` uses), printing `note: ranked by keywords: …` (on
stderr with `--json`; the MCP `docs` tool's `reason` is `keyword_ranking` and
its `note` says why). The same happens for a library indexed with a different
model: vectors are only compared with vectors of the model that made them.
`ketch docs index --reembed <library>` re-embeds a library's stored sections
with the current model, without the network.

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

#### model — The embedding model for the local docs index

```console
$ ketch model install            # bge-base-en-v1.5, 438 MB, into ketch's cache dir
$ ketch model install --small    # bge-small-en-v1.5, 133 MB
$ ketch model status
```

The local docs index ranks sections with an embedding model that runs inside
ketch on the CPU, in pure Go: nothing else to install. `install` downloads the
model's files from Hugging Face at a pinned revision, checks each against its
pinned size and SHA-256, and prints where it went; re-running it finishes an
interrupted download. With both installed, bge-base is used. `model_path`
points ketch at another model directory instead (a BERT-family sentence
transformer as published on Hugging Face). Indexes built before a model change
keep working, ranked by keywords, until `ketch docs index --reembed`.

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

Concurrent read-only probes against every backend, plus the browser, the
embedding model (`ok`, or `skipped` when none is installed; never the network),
the cache, and an informational `tags` row for the bookmark index that never
affects the exit code.
Each comes back `ok`, `no_key`, `unreachable`, `misconfigured`, or `skipped`.
Context7 without a key is `ok` and `keyless`, and doctor sends it nothing: it
reports the quota the last real Context7 response left (`~187 of 200 left as of
…, resets …`, or `quota not yet seen`), kept in a small file in ketch's cache
directory, so running doctor never spends the anonymous quota. A spent keyless
quota is `no_key`, since a key is the fix. With a key, doctor probes Context7 as
before.
With `docs_backend` set to `auto`, a docs `auto` row sums up `web` and
Context7 from their own rows, without probing again.
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
| `docs, default` | `--tokens` (4000) | ~3.3 KB |
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
- **Bad** `ketch docs -b web --library django "…"` on every call of a session — each one resolves the name again.
- **Good** `ketch docs -b web --resolve django` once, then `--library https://docs.djangoproject.com/en/6.1/` from its output.

## Backends

| Surface | Default | Also available |
| --- | --- | --- |
| `search` | `auto` | brave, ddg, searxng, exa, firecrawl, keenable, tavily, tinyfish, parallel, serpbase, degoog, serply, youcom |
| `code` | `grepapp` | sourcegraph, github |
| `docs` | `context7` | web, auto (web, then Context7) |

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

#### Code and docs — grepapp, sourcegraph, github, context7, local, web

Grep and Sourcegraph need nothing. GitHub uses `gh auth login`,
`$GITHUB_TOKEN`, or `ketch config set github_token <tok>`. Context7 works
with no key on its anonymous quota (about 200 requests a month per IP);
`ketch doctor` shows it as `keyless` with the requests left as of the last
Context7 response and when they renew (without spending one), and a free key via
`ketch config set context7_api_key <key>` lifts the cap. The `web` docs backend
needs nothing either; resolving a library name asks GitHub about its
repository, and the same GitHub token lifts the 60-requests-an-hour limit an
unauthenticated install gets. Rate-limited, `web` still answers from the
registries, and says so in the match's evidence. The `local` docs backend needs
no key or network: it answers from the libraries `ketch docs index` indexed.

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
- `cache_ttl` — cache lifetime
- `user_agent` — User-Agent override for HTTP and browser fetches
- `extract_mode` — `clean` (default) or `complete`; a non-default mode scopes cached pages, so a page cached under one mode is never reused under the other
- `mcp_tools` — allowlist of MCP tools to publish; unset publishes all six
- `search_order` — the `auto` chain, exactly: backend IDs tried in this order; unset keeps the default (see Backends)
- `external_pdf_to_md_converter_command` — external PDF-to-Markdown converter; must contain exactly one `{input}` placeholder. Once set it is authoritative, with no silent fallback
- `http_headers` — headers for self-hosted SearXNG, Firecrawl, Degoog, and Sourcegraph instances, keyed by origin (see Self-hosted under Backends); values are secrets
- `model_path` — an embedding model directory for the local docs index; unset, ketch uses the one `ketch model install` downloaded

## Exit status

Every failure mode has a number, so a script or an agent can branch on the
outcome instead of pattern-matching an error string.

| Code | Meaning | What to do |
| --- | --- | --- |
| ok: 0 | Success | Read the result |
| warn: 2 | Bad input | Fix the call — retrying unchanged can never succeed |
| warn: 3 | Nothing matched | Change the query or selector; not an outage |
| crit: 4 | Upstream or network failure | Rotate to another provider, or retry once |
| crit: 5 | Missing precondition | Stop and configure — run `ketch doctor` |
| warn: 6 | Cancelled or timed out | Rerun with a smaller scope |

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
- `ketch docs "query" --library /org/repo` for version-aware docs; `-b web --library django@5.2` reads the library's own docs site, no key
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
- **Context7's resolve never returns empty.** Garbage in gets confident fuzzy matches out, so vet the name rather than trusting the score. The `web` backend's returns only verified matches, with their evidence, and exits 3 when nothing verifies.
- **Regex is per-backend.** grepapp and sourcegraph accept it; github rejects it with a pointer to the other two.
- **Background crawls are CLI-only.** The MCP `crawl` tool is synchronous and capped at 30 pages by default, 100 hard, three minutes wall clock.
- **The page cache is shared across processes.** An idle MCP server holds no page-cache lock; individual operations open and close the bbolt file.
- **Bookmarks are not cache entries.** `cached: false` in `tag show` means no fresh local body, not a dead link; the bookmark stays until `tag remove`.
