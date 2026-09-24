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
| Current pages, opinions, news, comparisons | `search` | `docs` — that's curated library docs only |
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

#### search — Web search across twelve providers

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
repository is never a silent empty result: sourcegraph exits 3 and names the
other backends, and grepapp, which indexes a subset of public repositories,
warns when a repository search comes back empty.

#### docs — Curated, version-aware library documentation

```console
$ ketch docs "routing" --library /vercel/next.js
$ ketch docs --resolve "next.js"     # name → Context7 IDs
```

| Flag | Meaning |
| --- | --- |
| `--backend, -b` | `context7` (default) |
| `--library` | Context7 library ID; skips the resolve step |
| `--tokens` | Token budget, default 4000 |
| `--resolve` | Resolve a library name instead of searching |
| `--tag <name>` | Bookmark every result URL under a tag |

`docs` is a two-step: resolve the name, *vet the matches*, then fetch by ID.
Resolve never returns empty. A bad name still returns confident fuzzy matches,
so check the name, not just the trust score.

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

bbolt-backed, 72-hour default TTL. Repeat scrapes and crawls read from cache,
with no refetch. The file is opened only for each read or write, so the CLI,
background crawls and any number of MCP servers share it. Expired pages are
swept automatically. `ketch cache` also reports the tag index — path, tag and
entry counts. `cache clear` deletes the page cache, returning its space, and
leaves bookmarks alone.

#### doctor — Live health check of every surface

```console
$ ketch doctor
```

Concurrent read-only probes against every backend, plus the browser, the
cache, and an informational `tags` row for the bookmark index that never
affects the exit code.
Each comes back `ok`, `no_key`, `unreachable`, `misconfigured`, or `skipped`.
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
  go:     go1.25.7 linux/amd64
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

## Backends

| Surface | Default | Also available |
| --- | --- | --- |
| `search` | `auto` | brave, ddg, searxng, exa, firecrawl, keenable, tavily, parallel, serpbase, degoog, serply, youcom |
| `code` | `grepapp` | sourcegraph, github |
| `docs` | `context7` | — |

> **`auto` is a chain, not a provider.** It falls through
> `parallel → exa → keenable → youcom → firecrawl → ddg`, none of which need a
> key, and reports which one served.
>
> Set a key and `auto` promotes that provider ahead of the chain — you don't
> also have to set `backend`. Self-hosted SearXNG and Degoog instances are
> preferred over hosted APIs once configured.

#### Keyless — Nothing to configure

`parallel`, `exa`, `keenable`, `youcom`, `firecrawl`, and `ddg` answer with no
setup. Keys for exa, keenable, youcom, and firecrawl are optional and lift the
hosted caps. ddg rate-limits readily under fan-out.

#### Keyed — Free key, then promoted by auto

```console
$ ketch config set brave_api_key <key>
$ ketch config set tavily_api_key <key>
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

#### Code and docs — grepapp, sourcegraph, github, context7

Grep and Sourcegraph need nothing. GitHub uses `gh auth login`,
`$GITHUB_TOKEN`, or `ketch config set github_token <tok>`. Context7 takes a
free key via `ketch config set context7_api_key <key>`.

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
- `KETCH_GITHUB_TOKEN` beats the config file, which beats an ambient `$GITHUB_TOKEN`.
- `url_rewrites` and `spa_markers` are file-only — their JSON and regex values don't survive env quoting.
- `ketch config` reports an `env_overrides` section, so you can always see which values came from the environment.
- Invalid values fail loudly, naming the offending variable. Secret `KETCH_*` vars are stripped from spawned subprocesses.

#### Page cache — bbolt, 72h TTL, single-process

```console
$ ketch cache          # stats
$ ketch cache clear
$ ketch scrape <url> --no-cache
```

The cache is single-process. A long-running MCP server holds the lock, so
concurrent CLI scrapes silently run cache-disabled — `ketch doctor` reports the
cache as locked by another process. Bookmarks are separate: `tags.db` is opened
only for short index operations, so a server holding `cache.db` never blocks
`ketch tag`.

#### Browser rendering — Fast path first, Chrome on detection

```console
$ ketch config set browser chrome       # from PATH
$ ketch config set browser /usr/bin/google-chrome-stable
$ ketch browser install                 # download Chromium
$ ketch browser status
```

#### Other keys — Rewrites, SPA markers, user agent, extraction mode, PDF converter

- `url_rewrites` — regex rewrites applied before fetch
- `spa_markers` — extra tokens for JS-shell detection
- `cache_ttl` — cache lifetime
- `user_agent` — User-Agent override for HTTP and browser fetches
- `extract_mode` — `clean` (default) or `complete`; a non-default mode scopes cached pages, so a page cached under one mode is never reused under the other
- `mcp_tools` — allowlist of MCP tools to publish; unset publishes all six
- `external_pdf_to_md_converter_command` — external PDF-to-Markdown converter; must contain exactly one `{input}` placeholder. Once set it is authoritative, with no silent fallback

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
- `ketch docs "query" --library /org/repo` for version-aware docs
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

> **Network posture matters.** The server performs no URL filtering — it
> fetches whatever URL the client gives it, including private or internal
> addresses reachable from wherever it runs. Give it the network posture you'd
> give the agent itself.

### Claude Code plugin

ketch only needs the CLI on PATH. The repo also works as a plugin marketplace
that installs the MCP server and skill together.

```console
$ claude plugin marketplace add 1broseidon/ketch
$ claude plugin install ketch@ketch
```

### Bootstrap files

Two files on this domain are written for agents rather than people:
[/llms.txt](/llms.txt) is the short index and [/llms-full.txt](/llms-full.txt)
is this manual as plain Markdown.

## Notes

- Scraping a **bare domain** auto-probes `/llms.txt` and may return that instead of the homepage. The `title` field reveals the swap; `--no-llms-txt` opts out.
- **Batch scrape reports per-URL failures inside a successful call.** The command exits 0 with per-result errors set — check every entry, don't just check the exit code.
- **`docs` resolve never returns empty.** Garbage in gets confident fuzzy matches out, so vet the name rather than trusting the score.
- **Regex is per-backend.** grepapp and sourcegraph accept it; github rejects it with a pointer to the other two.
- **Background crawls are CLI-only.** The MCP `crawl` tool is synchronous and capped at 30 pages by default, 100 hard, three minutes wall clock.
- **The page cache is single-process.** Running the MCP server long-term degrades concurrent CLI scrapes to uncached.
- **Bookmarks are not cache entries.** `cached: false` in `tag show` means no fresh local body, not a dead link; the bookmark stays until `tag remove`.
