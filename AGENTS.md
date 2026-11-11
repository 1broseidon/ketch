# Ketch — Architecture

Fast, stateless CLI for agentic web search and scrape.

## Module Layout

```
main.go                      Thin entry point → cmd.Execute()
cmd/
  root.go                    Cobra root, global flag (--json)
  search.go                  Search command: query → results, optional --scrape
  scrape.go                  Scrape command: URLs → markdown, concurrent batch
  extract.go                 Extract command: piped HTML → markdown (no fetch/cache/browser)
  crawl.go                   Crawl command: BFS/sitemap crawl with streaming output
  crawl_bg.go                Background crawl: status, stop subcommands, worker mode
  code.go                    Code search command: query → snippet results, --lang/--repo filters, literal-qualifier warnings
  docs.go                    Docs search command: query → docs results (search by default, context7 with -b), --library (a docs URL, a library spec, or a Context7 ID), --resolve (one line per match; the evidence form when a match carries evidence), --section (one section of the local index in full by the url#anchor a result carries; no query; refused only for an explicit -b context7); a legacy docs_backend name read through docs.CanonicalBackend; help text from each provider's LibraryHelp
  config.go                  Config command: discovery, init, set, path
  cache.go                   Cache command: stats (page cache and tag index), clear
  tag.go                     Tag command: add, show, list, remove over the durable tag index
  docs_index.go              Docs index command: `docs index <library|docs-url>` builds or refreshes a library's local index, which `ketch docs` then answers from offline (progress on stderr; a note when an earlier ketch's bbolt docs-index.db sits beside it), --list, --remove, --dry-run (print the plan: pages in order with their tier)
  browser.go                 Browser command: install, status
  doctor.go                  Doctor command: report formatting + exit-code gating over doctor.Run
  mcp.go                     MCP command: `mcp serve` runs the MCP server over stdio
  proc_unix.go               Unix process management (detach, signals)
  proc_windows.go            Windows process management stub
search/                      Searcher interface + Brave/DDG/SearXNG/EXA/Firecrawl/Keenable/Tavily/TinyFish/Parallel/SerpBase/Degoog/Serply/Youcom backends; NewFromConfig resolves the ordered provider registry for cmd/ and mcp/. auto.go is the default `auto` backend (keyless fallback chain, AutoRank-ordered; the search_order config key pins it to an operator-chosen list), multi.go adds federated --multi search (RRF fusion, NewMultiFromConfig), random.go shuffled fallback, canonical.go the URL dedup keys
code/                        code.Searcher interface + GrepApp/Sourcegraph/GitHub backends; NewFromConfig resolves the ordered provider registry. Query.Repo (owner/name, filter.go) is exact on every backend: each translates it and narrows a looser native filter itself
docs/                        docs.Searcher interface + two providers: search (search_first.go, the default; the local index, else the docs site a web search finds) and Context7; NewFromConfig resolves the ordered provider registry (CanonicalBackend reads ketch 0.20's "local" as search) and hands providers a docs.Deps (fetch.go: the injected Fetcher, which reports each response's final URL). The web_*.go files are search's and `docs index`'s internals (no backend of their own): site profiles, discovery, fetch, sectioning, and name@version resolution
  web.go                     The Web internals search and docs index share: the docs root (webRoot) and its scope (same host, www. twin, path prefix, dot segments cleaned), ResolveLibrary (search's --resolve: a library spec to its verified docs roots), timeouts as upstream errors
  web_types.go               Name-resolution contract: libSpec, querySplit, pkgInfo, repoKey/repoInfo, siteProfile, siteVersion/versionSet, evidence; the pkgSource, repoSource and siteSearcher interfaces
  web_spec.go                Library spec grammar (docs URL, [eco:]name[@version], owner/repo, Go module path), a question's library readings by syntax (decision-15: a sentence's closing in/with/using/for X, use X, X library, X's, does X; a keyword query's first word unless an article or particle follows; never a language word or a camel-case API name), used to find an indexed library a question names; per-registry name normalization and name variants
  web_words.go               Common English and programming words: a reading named by one names a library only when that library is in common use (commonWordBar)
  web_pkg.go                 Package registries: npm, PyPI (streams only "info"), crates.io (User-Agent required; one request at a time, a second apart, per its crawler policy), Go module proxy and pkg.go.dev (its search page, module pages and go.mod check); getPatient/turns: one request at a time with Retry-After-aware backoff, tried once for a bare query's guessed reading
  web_github.go              GitHub repository metadata and search: renames followed, token resolved lazily and never echoed, rate-limit errors, a search GitHub cut short (incomplete_results) returned with errIncompleteSearch, repository URL parsing
  web_site.go                Site profiles (title, meta, links, repository back-links; one probe budget per profile), docs nav links, registrable domains, non-docs and non-public hosts, publicHops (no redirect into non-public space)
  web_trust.go               Evidence kinds, their points and the 0-10 trust score (a match is verified with trust >= 3); the keyless edge classes (metadata, site link, DNS, GitHub homepage, registry-hosted docs) and keylessVerified
  web_resolve.go             Name resolution: registries → projects (repository claims kept only when corroborated; a repository with no package takes its own language's package that names it back, or the Go module at its path) → GitHub (renames, stars; repository search when no package matches exactly or the name stays unsettled) → candidate roots (package docs and homepages, nav chains, a docs. guess) → verification → web search as the last resort (with a token; keyless resolution is web_keyless.go) → per project a site and its registry docs → versions (listed labels, dotless 14 as 1.4, layout probes such as 18.react.dev and /docs/13/, the nearest built docs.rs release); pickUnique (dominance: stars; an index-hosted-only namesake blocks a site-owning library only when at least as popular; else AmbiguousLibraryError); answerPastFailures: a single-library pick past a failed or incomplete registry or GitHub search, a repository GitHub did not read, or the unanswered site of a project that could outrank it is an upstream error; docs URLs (resolveURL, movedRoot to a corroborated new host)
  web_keyless.go             Keyless resolution (no GitHub token: GitHub is never called): web search beside the registries from the start, projects from what hits name (registry package pages, repositories named like the library, an organization named like it), Go modules by name from pkg.go.dev's search page, popularity read only for a contest (readTieBreak), keylessRivals (a pick must outweigh by 50x a same-name package whose own docs site did not verify, and be more popular than one with no docs site or only index-hosted docs), strict-reading misses (react-router keeps react from answering for it), applyHint, the per-resolution llms.txt memo
  web_identify.go            Keyless edges and identification: registry page links (registryPackageOf), the site-link edge from the root, its home page, its llms.txt or Read the Docs' record; the DNS edge (two owner URLs on one domain); keylessEvidence (two independent edges, registry-hosted docs alone; a search hit must also be a docs framework or publish llms.txt; a hit that is the project's own homepage is skipped); siteIdentity
  web_popularity.go          Registry popularity on one weekly scale (npm downloads API, pypistats.org, crates.io recent/13, pkg.go.dev importers x1000), memoized per process; pypistats and pkg.go.dev asked one request at a time with retries (getPatient)
  web_hint.go                Ecosystem hint: the working directory's manifests (package.json, pyproject.toml, Cargo.toml, go.mod): a tie-break in single-library contests only, never setting aside a project that outweighs the hinted ones by the dominance margin; the working directory never steers the web search
  web_rescache.go            Resolution cache: a verified single-library pick under spec, mode and hint, kept through docs.ResolutionCache (fetch.go) in the page cache with its normal TTL; only picks made with every source answering
  web_version.go             Version labels and aliases (stable, latest, dev); the site's versions from the first source that answers (Read the Docs addons, mike versions.json, pydata switcher, sibling links by path or host label, the Read the Docs sitemap, the Sphinx build); indexChild (a version index's current docs); matchVersion (prefix match, never across a major) and dotted readings of dotless labels; registry version roots (docs.rs, pkg.go.dev); canonical roots; Result.Version from each result's URL
  web_sphinx.go              Sphinx adapter: searchindex.js pages (html/dirhtml), objects.inv and documentation_options.js headers
  web_discover.go            Discovery, cheapest first: llms.txt (the .txt index files it lists read as input, never results), llms-full.txt (chunks attributed to their pages), on-host sitemaps (the root's locale only, within sitemapBudget), then root-page links merged with the Sphinx build's searchindex.js pages and, for a root page that is only a hub (tokio.rs, v2.vuejs.org), up to webMaxHubs sections' sidebars; registry docs hosts go straight to the root page; only in-scope pages are kept
  web_fetch.go               Fallback fetcher (no injected Fetcher), probes, bounded page fetches; a response served from outside the scope is dropped
  web_sections.go            Heading-tree sections: H1–H3 always split, deeper headings when they are API entries (docs.rs methods, pkg.go.dev funcs, serde.rs attributes) or sit in an oversized section, with Parameters/Returns-style subsections kept on their entry; breadcrumbs; anchors as the site makes them (explicit {#id}, MDX {/*id*/} and permalink ids, then the generator's rule: rustdoc, pkg.go.dev, Sphinx, VitePress, else github-slugger), corrected by the ids the pages' own links use (adoptLinkedAnchors, which also re-slugs a host by a rule its links prove, such as Hexo's); permalink and [source] decorations stripped; docs platforms' boilerplate sections (boilerplateSection: agent instructions, was this page helpful, edit this page, cookie notices) dropped
  web_rank.go                Query terms (stopwords, light stemming, prefix matches); candidate picking by link title and path with rarity-weighted terms (source listings and index files are never results); locale/version copy collapse (variantParts names a page's variant tree)
  outcome.go                 docs.Outcome: answered/partial/refused/failed, reason, note (decision-20); one Answer/Resolved per call, the CLI and MCP render it
  local.go                   The local index (ADR-0007), part of search: the indexed library a value or bare query names (MatchIndexed, libraryInQuery; names derived from stored metadata by libraryNames/libraryNameTiers in local_index.go: full identities first, then the library's own path segments and package tails, then host labels (with a language word dropped: python-httpx is httpx), never one word of a multi-word name; matchNames takes the closest tier; releaseStems (beautifulsoup4 is beautifulsoup) when nothing matched; withProject adds the same project's other versions so newestOf picks its current docs), the version it asks for or the newest indexed (pickVersion for --section, newestOf), format check (index_outdated); answers through the evidence pipeline (evidence.go) over the full-text table (local_fts.go; no model: ADR-0007 amendment 7); a re-rooted current release shown under its unversioned addresses (showUnder)
  local_fts.go               Keyword ranking (ADR-0007 amendment 5): ftsTokenizer (each section's FTS5 row: heading, breadcrumb, identifier parts (identParts: camelCase, snake_case, dotted names whole and split), body, normalized by rankTokens as queries are; ftsVersion, a change rebuilds rows from stored text), OpenIndex, the whole question's ordering for Gather's pool (bm25 top ftsCandidates with ftsWeights, question filler dropped, then rerankHits: sectionBoost, sitePrior (guide over reference leaf by planTier), page title coverage, legacyPages (legacy/deprecated/archived or an x-v1 beside x: 0.6), troubleSection for a troubleQuery (1.2), boilerplateSection skipped, pageBoosts (the page's summed candidate scores); the phrase sequence leaves identifiers' parts out)
  evidence.go                The evidence pipeline (design/docs-evidence.md) the local index answers through, from an indexed library or pages search just stored: gather → sort → rank → merge, one view fitted to the budget (localTokens 1,800 unless set, frontmatter included; --limit caps the groups, else the budget decides); fitResults drops the last choice while the rendered answer runs over
  evidence_gather.go         Gather: one term list (queryTerms without filler, a host name one term, plus queryIdents, which weigh double), each term's count of the library's sections (Store.Counts) and the library's; the pool: the whole question's best 40 (rerankHits order) and each term's own best 20 (Store.SearchEach, the term in heading/ident/body), at most 200, no relevance cut, family or per-page cap; every pool section's whole-question score relative to the best and its place in that ordering (scoredBeyond for those past the candidates); cleanSection as sections enter
  evidence_clean.go          Sort's cleaning, a pure function: leaked HTML tags keep their text (prose: known inline/block elements; code: tags with class/style, not in a markup-language block), <br></br> a line break (<br> in a table row), badge/tracking image lines dropped, no-op Markdown escapes outside code dropped (pam\_duo)
  evidence_sort.go           Sort: splitText (paragraphs, sentences only past 120 tokens, list items as item/step, code blocks with their tab label, table body rows carrying their header, notes; labels and headings out; the paragraph leading into a list; a paragraph, the code block after it and the blocks right after that one as one unit: lead and then), page role (pageRoleOf: release notes, troubleshooting, FAQ, reference by planTier, guide), fold (same text after whitespace and case: best role, then earliest; folded ids kept)
  evidence_rank.go           Rank: scorePieces (termWeight rarity from the library's counts, identifiers double; heading path only 0.5, defining position x1.5 (a row's first cell, a definition term opening an item or paragraph, a section's heading for its first piece only), role factor 1/0.9/0.8/0.6, Together: the share of the question's distinctive weight matched, heading path included), fuse (rank by score, ties shared, fused with the section's rank in Gather: 1/(10+rank) + 1/(10+section rank), a section Gather's ordering leaves out after all it holds), choosePieces (walk the fused order; take each at or above half the best score that fits, not a repeat (every term its text holds already in two chosen pieces' text) and within --limit; cost = the unit (paragraph and code both ways, item and its lead) + group heading/source/pointer/contents lines + table header; an example's output while the budget holds it; a folded piece once; stop reason budget/floor/end with the floor and the count at it)
  evidence_merge.go          Merge: mergeGroups (chosen pieces by section, best first; a section more than half chosen printed whole when the budget left holds it), renderGroup (page order, neighbours joined verbatim, " … " / "…" for gaps, table rows under one header; Result.GroupLine: "option rows, 2 of 18", "same text in N other sections: …"; Result.Contents on a cut group)
  evidence_contents.go       Contents: a cut group's one line (about 40 tokens) naming what it left out: its option tables' other keys and its section's direct subsections (Store.PageOutlines) with their option tables' keys, lists cut with "… +N"; counted in the group's cost
  local_section.go           Follow-up fetch (ADR-0007 amendment 4): SectionSelect (the SectionFetcher interface) reads one section by the url#anchor a result carries, no ranking, through Store.PageSections (a prefix scan of one page's keys): an H2 is its family (the section and the subsections under it), a deeper anchor that section and its subsections, a page URL its sections in order, a library inferred from the URL (librariesHolding, the display root included) when none is named; fitChunks cuts at a block boundary within the cap (the estimator counts the notice) and cutNotice names the next section or says to raise the cap; unknown is ErrSectionNotFound (not_found, exit 3), a non-URL invalid_section (exit 2); HintStyle/SectionHint/RenderResult: the one-line pointer a cut result ends with (CLI command, or the MCP docs call appended to the snippet), counted in the budget (groupCost) and set by the surface via LocalOptions.Hint; Result.Section and Result.Truncated
  local_tokens.go            estimateTokens: an o200k token estimate from an approximation of its pre-tokenizer (words, digit runs, punctuation runs, whitespace, symbols), weights fitted against o200k_base on rendered local results
  local_text.go              Text helpers shared by build and answer: LocalOptions (context: Tokens, Limit, Hint), textSpans (code blocks and paragraphs, labels), sentenceStarts (pieces are cut at sentences), crossVersion/asksAbout (a section about a later major, labelled at build time, skipped by ranking unless asked), RenderSnippet (a local result as Markdown, its group line under the heading path)
  local_index.go             Index build: resolve like GetDocs (a docs URL as given; name@version that version's root; current docs labelled with the latest release when the site names none), planIndex (alias and release re-rooting, a version host borrowing its current host's paths; IndexRequest.DryRun stops at the plan), fetch the plan through the injected Fetcher, drop pages whose text is another's, section as web does, label cross-version sections, store (putPageBatches: URL order, so row ids and ranking ties are the same every run) with the site's name and own packages (siteIdentityNames); refresh by page hash
  local_plan.go              Index planning: full discovery (llms.txt/llms-full.txt, sitemaps and nav, root links, search index), in-scope result pages, .html twins and /llms/ mirrors as one, the indexed version's tree only and one copy per page across locales (keepOneVariant: trees the site's version list names, release/channel trees right below the root, mostly-copy trees dropped), robots.txt (parseRobots), page cap in priority order (planTier: llms.txt and seeded pages, guides, the rest, API reference leaves and release notes last; directories round-robin), PageSeeder (search-seeded priority; none wired yet)
  local_reroot.go            Where the docs are: aliasRoot (a channel or unversioned root whose page redirects, refreshes or names canonical a numbered release), rerootTree (an unversioned root whose pages or llms.txt sit in one release tree), splitReleases (an unversioned root whose plan holds two or more release trees within two levels: the one the root page links to most, else the newest final release), displayRootFor (unversioned addresses when the release names them canonical), siblingHost (a version host's current host), uniqueContent, siteName
  search_first.go            The search backend (`search`, the default; ADR-0007 amendments 6 and 8): the local index answers when it covers the question (a library indexed in full that the request names, with whole sections unless a budget is set; or the same question answered before), else the question and the question + "documentation" are searched (the auto chain), the site the docs hits agree on is read (hits, then siblings), kept in the local index and ranked by the local ranker; answers are partial (partial_site) with the command that indexes the whole site; refusals no_docs_found and ambiguous_site, search_empty a failure; up to sfMaxSites domains and sfMaxReads docs trees, a domain's next tree before the next domain when one gives nothing readable; a library (--library or a pinned spec in the question) is a preference, never a gate; KETCH_DOCS_TRACE writes one trace line per call
  search_site.go             Site choice: fuseHits (rank-weighted, both searches, and the pages hits' text cites at half weight), docsHits (forges, registries, Q&A, blog platforms, docs mirrors, forum and blog hosts, post and discussion paths dropped; a docs host, path or title weighs twice), docsTree (version segment, docs segment, leading locale, a Pages project), sites by registrable domain (a package index's docs by tree), boosts (the library's resolved root or name; the question's words naming a host or a docs directory), ownerFirst (a site whose domain the question names, else a standards body's, specDomains, first at a quarter of the leader's weight), each domain's trees ordered by treeBetter (English, then main docs over a variant the question does not ask for, variantSegment: enterprise-server@x, legacy, archive; then the version asked, else current docs over a numbered release) with the rest as alts, mainTree (a variant's hits alone try the main docs' copies first), pickSite (a lone hit needs a 1.5x lead, unless the site is an alt, an owner or a spec, or the question names exactly one leading site)
  search_pages.go            Pages a search-first answer reads: the site's hits, then siblings from its own lists (discovery within a budget, the top hit's nav) by pickCandidates and beside a hit in the nav, robots.txt honoured, other releases' copies left out; fetch within per-page and overall deadlines, an unrendered JS shell with next to no text counted as failed (unreadShell)
  search_store.go            The index as a by-product: pages go to the indexed library whose root holds the site's root, else a new one (Resolved "search"); nothing is removed
  search_local.go            The local check before searching (fromIndex: a fully indexed library the request names; the same question asked before, askedBefore: the same term set after filler (sfAskedOverlap 1), remembered only from the first site read; a new question about a search-built library is searched again), partialLibrary (answers from a search-built library are partial), the trace
internal/testutil/           Test helpers: isolated config home, hostile instances for redaction tests
  fixturetransport.go        FixtureTransport: an http.RoundTripper serving recorded fixtures under real hostnames (a request with no fixture is an error, never the network); SwapDefaultTransport puts it behind httpx.Default for in-process cmd and MCP tests
internal/docsindex/          Local docs index store (format 3, ADR-0007 amendment 5): one SQLite file via modernc.org/sqlite (pure Go, no CGO), docs-index.sqlite (KETCH_DOCS_INDEX_PATH; a non-SQLite file is ErrNotSQLite, LegacyNote names an old bbolt docs-index.db beside it), opened per operation with WAL; tables libraries (JSON metadata), pages, sections, and an FTS5 table per library (contentless, rows from the Tokenizer; Search with bm25 column weights, EnsureFTS/RebuildFTS from stored text when the tokenizer version changes); per library (keyed by docs root, one version each) metadata (root, version and version asked, resolution evidence, format version, counts, site name, display root), pages with content hashes, version, fetch time and source, sections with their H2 (family) and cross-version labels (an earlier format-3 file's vectors table and units columns are left unread)
internal/boltfile/           Open-per-operation bbolt transactions (ADR-0006), shared by the page cache and the tag index
internal/docsfetch/          docs.Fetcher over the shared scraper and page cache, built by cmd/ and mcp/: URL rewrites, cookies, browser fallback and caching apply to docs pages; reports final URLs in the caller's terms and wraps docs.ErrNotFound for 404/410; docs.ResolutionCache over the page cache (its own key prefix and source)
internal/docsqa/             Docs QA runner (a dev tool, `go run ./internal/docsqa`, not in the binary): runs cases/*.json against a ketch binary in a sandbox directory (its own HOME, config and indexes), indexes each case's library once, and grades each fact at gather, sort, rank and output from `ketch docs --trace` (hidden flag; docs.Trace in docs/trace.go); design/docs-evidence.md
mcp/                         MCP server (search/code/docs/scrape/crawl/tag tools; the mcp_tools config key is an allowlist over the published set) over the go-sdk mcp package; Server struct holds the shared scraper + cache, tools call the same NewFromConfig constructors as the CLI
scrape/                      HTTP fetch + Page type, JS detection fallback, Rod browser; pipeline.go has the cache-aware scrape pipeline (CachedScrape*, ScrapeSelector, FetchLLMSTxt) shared by cmd/ and mcp/
extract/                     structural extraction (landmark → uniform sections → prose root, chrome pruned by what it is; config extract_mode clean|complete) with readability fallback and html-to-markdown, charset decoding, hidden code kept when the page shows it nowhere else (a tab of a code sample) and ARIA tab labels moved onto their panels, JS shell detection (Detector: built-in + config spa_markers, modern hydration/streaming frameworks)
crawl/                       BFS crawler, work queue + worker pool, background status
cookies/                     Netscape cookies.txt jar loader + RFC 6265 domain/path matching (Jar.For); nil-safe, values never logged
config/                      JSON config loading/saving (~/.config/ketch/)
doctor/                      Health checks: concurrent read-only probes per backend + browser + cache + tag index (informational, never gates the exit code), status classification (ok/no_key/unreachable/misconfigured/skipped)
health/                      Shared bounded provider health checks (Status classification, HTTP probe helpers) used by doctor and the search/code/docs provider probes
cache/                       TTL page cache (Store interface, BBoltStore backend) and durable bookmarks in a separate tags.db (tags.go/tag_store.go). Both open their bbolt file per transaction through internal/boltfile and never hold it between operations (ADR-0006); writes sweep expired pages at most hourly, and cache clear deletes the file without touching bookmarks
httpx/                       Shared tuned *http.Transport for all HTTP backends; WithRedirectCheck puts a per-request redirect check on the context, which every httpx client's CheckRedirect applies; bench.go (build tag `bench`, never in release builds) records and replays every exchange (KETCH_HTTP_RECORD/KETCH_HTTP_REPLAY) for ketch-bench's docs suite
updatecheck/                 "new release available" probe + throttled stderr hint
site/                        Builds ketch.run from MANUAL.md with inkcap (GitHub Pages, docs.yml)
design/docs-keyless.md        The keyless web docs design and accuracy gates (decision-10), from before search replaced the web backend (ADR-0007 amendment 8); the docs benchmark itself lives in ketch-bench (`go run . docs check`)
skills/ketch/                Bundled research skill (SKILL.md + references), loaded by Claude Code and pi
plugins/ketch/               Claude Code plugin (.mcp.json → `ketch mcp serve`, symlinked skill); listed by .claude-plugin/marketplace.json
plugins/pi/                  pi package extension (ketch_* tools over `ketch … --json`) + node:test suite; manifest is the root package.json, `npm test` runs it
```

Reusable packages live at the module root so external programs can `import "github.com/1broseidon/ketch/<pkg>"`. Shared implementation helpers, including the config model used by provider packages, live under `internal/`.

## Design Principles

- **Stateless**: no daemon, no queue. Call → result → done. Background crawls are detached processes, not a server.
- **Fast path first**: plain HTTP fetch; browser rendering kicks in automatically when JS shell is detected.
- **Interface-driven backends**: `Searcher` for search engines, `Store` for cache backends, `BrowserConn` for browser rendering.
- **Concurrent by default**: multiple URLs scraped in parallel via goroutines.
- **Operator configures, agent consumes**: config sets defaults (backend, browser, cache TTL) so agents don't need to know infrastructure.
- **Works before it is configured**: `ketch search` and `ketch docs` answer on a fresh install. Search's default `auto` backend is a fallback chain over the keyless providers, docs' default `search` backend finds docs sites through it, and Context7 runs on its anonymous quota without a key; and configuring a key or an instance promotes that provider rather than requiring `backend` to be set too. Configuration raises limits and picks favourites; it is never the price of a first result.
- **Three search surfaces**: `ketch search` finds web pages, `ketch code` greps real OSS code, `ketch docs` fetches library documentation. Each has its own backend interface and Result type — they never share backends. (docs' `search` backend asks the search surface for the docs site, through `search.NewAutoFromConfig`, as a client of it.)
- **Smart input detection on scrape**: single URL, multiple positional args, JSON array string, file path, or stdin pipe all work — ketch routes automatically. No --batch flag needed.
- **Context-aware interfaces**: all three Searcher interfaces (`search`, `code`, `docs`) take `context.Context` as first param for cancellation and timeout propagation.

The reasoning behind each principle — and what ketch deliberately does *not* do — is in [design/DESIGN.md](design/DESIGN.md) ([Non-Goals & Scope](design/DESIGN.md#non-goals--scope)). Possible directions: [design/ROADMAP.md](design/ROADMAP.md). Decisions already made: [design/adr/](design/adr/).

## MCP Server

`ketch mcp serve` runs an MCP (Model Context Protocol) server over stdio, exposing six tools: `search`, `code`, `docs`, `scrape`, `crawl`, and `tag`. The `mcp_tools` config key is an allowlist over that set (JSON array or comma-separated; unset or `[]` publishes all six) — unlisted tools are never registered, and `serverInstructions` is generated from the enabled set so a pruned server never advertises what it won't answer. Values are validated fail-loud at `config set`, on the `KETCH_MCP_TOOLS` env override, and at server startup. Tool handlers call the same packages as the Cobra commands, through the same config-driven constructors (`search.NewFromConfig` etc.), and resolve backends/API keys from the same `~/.config/ketch/` config — an agent talking MCP sees exactly what a human using the CLI sees.

- **Lifecycle**: the go-sdk dispatches tool calls concurrently, so process-lifetime resources — the headless-browser scraper and the compiled URL rewriter — are constructed once in `mcp.NewServer`, shared by all calls, and released by `Server.Close` when `serve` exits. Never construct these per call. The page cache and the tag index are the opposite: the server keeps their paths and opens each bbolt file only for one transaction, so an idle server holds no lock and the CLI, crawls and other servers share the cache (ADR-0006). Never hold a bbolt handle across calls, and release the tag index before checking page warmth.
- **Option parity**: each tool exposes the per-invocation options of its CLI command (`scrape` gets `selector`/`raw`/`force_browser`/`no_llms_txt`/`trim`/`max_chars`/`no_cache` plus a `urls` batch input; `search` gets `searxng_url`, `scrape`, and `multi` (federated RRF search, with an additive `errors` map for per-backend failures); `crawl` gets `depth`/`sitemap`/`allow`/`deny`/`max_pages`; `code` gets `lang`/`repo`/`regexp`). Config-level settings (API keys, cache TTL, browser binary) stay operator-configured and are never tool params.
- **Error taxonomy**: every tool error starts with a stable machine-readable prefix mirroring the CLI exit codes — `[validation]` (exit 2), `[not_found]` (3), `[upstream]` (4), `[precondition]` (5), `[cancelled]` (6) — so agents can tell "fix your input" from "retry later". MCP has no structured tool-error field; the prefix is the contract.
- **Bounded crawl**: the `crawl` tool is synchronous and capped (`max_pages` default 30, hard cap 100, 3-minute wall clock); partial results return with `stopped: "max_pages" | "timeout"`. Detached background crawls (`ketch crawl --background`, status/stop) remain CLI-only.
- **Docs**: the `docs` tool's `backend` is `search` (default) or `context7`; `search` reads the local index (ranked by keywords, no model) when it covers the question, else the docs site a web search finds (`status: partial`, a `note` naming the site and the `ketch docs index` that indexes it). Answers are the pieces of sections that name the question (paragraphs, list items, code blocks, table rows under their header), grouped by section, at most 1,800 tokens or `tokens` (choosing stops earlier once nothing left adds enough); `limit` caps the groups. Each result cut short carries `contents` (what its section left out, by name) and ends with the `docs(section="url#anchor")` call that reads it in full; `section: "url#anchor"` reads one section from the index (no query, no network; an H2 is its family, a page URL its sections in order, capped by `tokens`, unknown is `[not_found]`, `backend: "context7"` with it is `[validation]`). `library: "name@version"` picks one indexed version. Output adds `status`, `reason`, `note` and `backend` to `results`/`matches`. Building or removing an index (`ketch docs index`) is CLI-only, since a build runs for minutes and writes durable state.
- **CLI-only operator commands**: `config`, `cache`, and `doctor` are deliberately not MCP tools. They are operator actions (change credentials, clear state, diagnose the installation), not research surfaces — an agent that needs to know whether a backend is ready reads `ketch config`'s `*_set` booleans or the operator runs `ketch doctor`. Don't add them to the server.
- **Annotations**: the five fetching tools are read-only network fetchers and declare `readOnlyHint: true` and `openWorldHint: true`. `tag` is the exception in both directions — it mutates the local index and never touches the network — so it declares `readOnlyHint: false` and `openWorldHint: false`.
- **Tagging**: `tag` supports add/show/list/remove, plus a `tag` option on all five research tools. Show defaults to 50 newest entries (`limit: 0` for all), with `entries`/`cached` totals, `shown`, and `cache_status`. The independent index uses `os.UserConfigDir()/ketch/tags.db` or `KETCH_TAGS_PATH`; labs must override the tag path as well as the page cache. Membership follows the source URL, never cookies/UA/rewrite keys. Expired bodies leave durable bookmarks; an unavailable page cache returns `cached: false` with `cache_status: unavailable`. Optional write failures retain research output with MCP `warnings` or CLI stderr warnings. See [ADR-0004](design/adr/0004-tagged-cache-corpus.md).
- **Security note**: after configured URL rewrites, `scrape` and `crawl` fetch only absolute HTTP(S) URLs; non-web schemes such as `file://` are rejected. They do not filter private or internal HTTP(S) addresses, which remain fetchable when reachable from the server. Run the server with the network posture you'd give the agent itself; don't point an untrusted agent at a server inside a sensitive network.
- **Smoke test**: `go test -tags mcpsmoke ./mcp/... -v` exercises the real binary over stdio (live network; not part of `go test ./...`).

## Quality Standards

- `golangci-lint run` must pass (gocyclo max 15)
- `go test ./...` must pass
- Extraction changes also run the [ketch-bench](https://github.com/1broseidon/ketch-bench) regression check in both modes (`go run . check` and `go run . check -extract-mode clean` from a sibling checkout); it is a separate repository so its corpus never weighs on this one
- Pre-commit hook enforces both
- CGO_ENABLED=0 — pure Go, cross-compile everywhere

## CLI Usage

```
ketch search "query"                        # search, return results (auto backend, no key needed)
ketch search "query" --scrape               # search + fetch full content
ketch search "query" -b auto                # keyless fallback chain (the default)
ketch search "query" -b searxng             # use SearXNG backend
ketch search "query" -b exa                 # use Exa hosted MCP backend
ketch search "query" -b firecrawl           # use Firecrawl v2 search API (keyless by default)
ketch search "query" -b keenable            # use Keenable backend (keyless by default)
ketch search "query" -b tavily              # use Tavily search API (keyed; basic depth)
ketch search "query" -b tinyfish            # use TinyFish Search API (keyed; free tier)
ketch search "query" -b parallel            # use Parallel Search MCP (keyless by default)
ketch search "query" -b serpbase            # use SerpBase Google Search API (keyed)
ketch search "query" -b degoog              # use a self-hosted Degoog instance (degoog_url)
ketch search "query" -b serply              # use Serply's Google Search API (keyed)
ketch search "query" -b youcom              # use You.com web search (keyless by default)
ketch search "query" --multi                # federate across every usable backend, RRF-fused
ketch search "query" --multi=brave,ddg,exa  # federate across a specific set (use the = form)
ketch scrape <url>                          # single URL → markdown
ketch scrape <url1> <url2> <url3>           # concurrent batch scrape
ketch scrape urls.txt                       # file with one URL per line
ketch scrape '["url1","url2"]'              # JSON array of URLs
echo "url1\nurl2" | ketch scrape            # stdin pipe
curl -L https://example.com | ketch extract # piped HTML → markdown (no fetch)
ketch crawl <url>                           # BFS crawl
ketch crawl <url> --sitemap                 # sitemap-based crawl
ketch crawl <url> --background              # run in background
ketch crawl status [id]                     # check crawl progress
ketch crawl stop <id>                       # stop a background crawl
ketch browser status                        # check browser config
ketch browser install                       # download Chromium
ketch code "query"                          # code search (grepapp)
ketch code "query" --lang go               # with language filter
ketch code "query" --repo owner/name       # one repository, exact on every backend
ketch docs "question"                       # docs search (search: the local index, else the docs site a web search finds; no key)
ketch docs "q" --library https://docs.astro.build  # search within one docs site
ketch docs "q" --library django@5.2        # prefer a library spec: [eco:]name[@version], owner/repo, Go module path
ketch docs "q" --tokens 880                 # fitted to a budget (default 1,800)
ketch docs --section <url#anchor>           # one section in full from the index: the follow-up to a cut result (the `full section:` pointer)
ketch docs --resolve click                  # name → docs root ID, with version, versions, packages, evidence
ketch docs index fastapi                    # index a library's docs site locally (name, spec or docs URL; --list, --remove, --dry-run); later questions naming it answer offline
ketch docs "query" -b context7 --library /org/repo  # Context7: fetch a library ID directly
ketch docs -b context7 --resolve "library name"     # resolve library name → Context7 IDs
ketch config                                # show effective config + backends (incl. *_key_set presence booleans)
ketch code "query" --tag remote-access      # record results under a tag (any surface)
ketch scrape <url> --tag docs               # fetch and record the page under a tag
ketch tag add docs <url>...                 # tag URLs directly, cached or not (no network)
ketch tag show docs                         # newest 50 bookmarks, with total counts
ketch tag show docs --limit 0               # all bookmarks under a tag
ketch tag list                              # every tag, with entry counts
ketch tag remove docs [url...]              # drop a tag, or just those pages from it
ketch cache                                 # show page-cache and tag-index stats
ketch doctor                                # live health check of every backend + browser + cache + tag index (exit 5 if a configured surface is broken)
ketch mcp serve                             # run as an MCP server over stdio (search/code/docs/scrape/crawl/tag tools)
```

## Flags

| Flag | Scope | Default | Description |
|------|-------|---------|-------------|
| --json | global | false | JSON output |
| --backend, -b | search | auto | Search backend (auto/brave/ddg/searxng/exa/firecrawl/keenable/tavily/tinyfish/parallel/serpbase/degoog/serply/youcom); `auto` is a fallback chain, not a provider |
| --multi | search | — | Federated search: comma list or bare/`=all` for every usable backend; RRF-fused, dedup'd, mutually exclusive with --backend (use the `=` form for a list) |
| --limit, -l | search | 5 | Max results |
| --scrape | search | false | Fetch full content |
| --searxng-url | search | http://localhost:8081 | SearXNG URL |
| --raw | scrape | false | Raw HTML output |
| --no-cache | scrape, crawl | false | Bypass page cache |
| --depth | crawl | 3 | Max BFS depth |
| --concurrency | crawl | 8 | Worker pool size |
| --sitemap | crawl | false | Treat seed URL as sitemap |
| --background | crawl | false | Run in background |
| --allow | crawl | — | Path substring filters |
| --deny | crawl | — | Regex deny patterns |
| --backend, -b | code | grepapp | Code backend (grepapp/sourcegraph/github) |
| --backend, -b | docs | search | Docs backend: search (the local index, else any docs a web search finds, kept in the local index) or context7. config `docs_backend` sets the default; ketch 0.20's `local` reads as search |
| --lang | code | — | Language qualifier (appended to query) |
| --repo | code | — | One repository, owner/name or a GitHub URL; exact on every backend. A backend that lacks it exits 3 (sourcegraph, github) or warns on an empty result (grepapp, partial index) |
| --library | docs | — | search: a docs URL (that site is read) or `[eco:]name[@version]` (npm:/pypi:/crates:/go:/github:), owner/repo or a Go module path, preferred among the sites search finds; an indexed library answers offline. context7: a library ID from --resolve |
| --tokens | docs | 4000 | Token budget (context7: 4000; search: 1,800 unless set, frontmatter included; with --section caps the section returned) |
| --section \<url#anchor\> | docs | — | Return one section in full from the local index (the `source` a result carries; no query, no ranking, no network); an H2 returns its family, a page URL its sections in order, capped by --tokens at a block boundary; unknown exits 3 (not_found), malformed 2 |
| --resolve | docs | false | Resolve the query as a library name or spec instead of searching: Context7 IDs, or for search docs roots with version, versions, packages and evidence (text and JSON) |
| --max-chars N | scrape, search --scrape | 0 (off) | Truncate markdown output to N chars, appends `[truncated]` |
| --trim | scrape, search --scrape | false | Strip markdown formatting syntax, keep content text only |
| --minimal | search, code, docs | false | One result per line, tab-separated, no frontmatter (a 4th backends column is appended under `search --multi`, plain search only) |
| --select \<css\> | scrape | — | Extract only elements matching CSS selector (skips content selection) |
| --url | extract | — | Source URL for metadata and relative-link resolution (no fetch) |
| --select \<css\> | extract | — | CSS selector to extract (skips content selection) |
| --trim | extract | false | Strip markdown formatting, keep content text only |
| --max-chars N | extract | 0 (off) | Truncate markdown output to N chars, appends `[truncated]` |
| --no-llms-txt | scrape | false | Disable automatic /llms.txt detection for bare domains |
| --concurrency | scrape | 5 | Max concurrent requests for multi-URL scraping |
| --force-browser | scrape | false | Always render via the configured browser, skipping JS-shell auto-detection (composes with --raw/--select; errors without a browser) |
| --tag <name> | search, code, docs, scrape, crawl | — | Bookmark each source URL with a bounded title and description derived from the result or fetched page. Composes with --no-cache; bodies remain in the separate page cache |
| --limit | tag show | 50 | Newest entries to return; 0 returns all. Reports returned and total counts |
| --minimal | tag show | false | One page per line, tab-separated (url/title/description) |
| --cookie-file <path> | scrape, search --scrape, crawl | config `cookie_file` or off | Netscape cookies.txt jar; flag overrides config and an explicit empty value disables cookies |
| --user-agent <ua> | scrape, search --scrape, crawl | config `user_agent` or built-in default | User-Agent override applied to HTTP and browser fetches; flag overrides config and an explicit empty value restores each fetch path's default. A configured UA is folded into the page-cache key, so pages cached under one UA are not reused under another |


## Adding a provider

Use the provider registry for new search, code, and docs backends. Read
[CONTRIBUTING.md](CONTRIBUTING.md#proposing-a-provider) for admission criteria.

Place the implementation, descriptor, and health probe in one Go file under
`search/`, `code/`, or `docs/`, with tests alongside it. Append its descriptor
call to the package's ordered
`providers` slice in `registry.go`; register explicitly, without `init()`.
Config discovery, CLI/MCP descriptions, doctor, and search auto/multi/random
eligibility follow the descriptor. Keep provider-specific branches and config
fields out of shared consumers.

- Define `ID`, `Name`, `Settings`, `Usable`, `New`, and `Probe`.
- Search providers set `AutoRank` to join the default `auto` chain: zero keeps
  the provider out, and lower non-zero ranks are tried first. Rank is only the
  tiebreak — a provider with configured credentials is promoted ahead of the
  rest. Rank a keyless provider by how well it serves an unconfigured install;
  leave a provider out if it cannot answer without setup. Use `AutoEligible`
  only when `Usable` is deliberately laxer than chain membership should be
  (SearXNG's localhost default is the one case). An operator's `search_order`
  replaces this ordering outright: listed providers are tried in the listed
  order whatever their rank, and only `Usable` is checked.
- `Usable` checks configuration without network I/O. `Build` checks it before
  calling `New`.
- `New` only constructs a client and must accept empty credentials. `Probe`
  performs the health check; construction must not contact the service.
- Import `internal/configbase` as `config` to avoid an import cycle with the
  public config facade. Read values with `String`/`Strings`; use `SetProvider`
  for overrides so per-call changes do not mutate shared MCP config.
- Declare settings on the descriptor: `config.KeyPool` for rotating credentials,
  `config.Scalar` for URLs, or `config.Setting` for custom secret/token behavior.
  Keep existing order values; new settings use the helpers' default ordering.
  Existing provider-specific accessors are compatibility helpers, not a pattern
  to extend.
- Doctor checks are required when the provider is selected or a setting marked
  `GateDoctor` is configured. Missing credentials or an instance URL must fail
  a selected provider's check. Set `MinProbeTimeout` for slower search probes.
- Code providers declare regex support in the descriptor, set `Qualifiers`
  when they apply `repo:`/`lang:` written into the query (without it, callers
  warn that such tokens were searched as text), and set `PartialIndex` when
  they index only some repositories. `Query.Repo` names exactly one
  repository: translate it into the provider's filter, narrow a looser match
  (substring, unanchored pattern) yourself, and return `code.ErrRepoNotFound`
  when the service says the repository is missing. Docs providers can
  implement `docs.LibraryResolver` for library operations.

Test requests, result mapping, authentication, cancellation, and relevant retry
and error behavior without live services. Run `make lint` and `make test`.
Regenerate config/doctor fixtures with
`UPDATE_REGISTRY_GOLDENS=1 go test ./cmd/ ./doctor/`; preserve existing entries
and add only the new provider's entries. Refactors must leave fixtures unchanged.
See [the registration test](search/registry_test.go) for a provider flowing
through shared consumers. Include documentation and changelog updates as
described in [CONTRIBUTING.md](CONTRIBUTING.md#implementing-a-provider).
