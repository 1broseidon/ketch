# Ketch — Design & Philosophy

This document explains **why** ketch is built the way it is: the mental model,
the core abstractions, and the reasoning behind the choices you'll see in the
code. It is the companion to [`AGENTS.md`](../AGENTS.md), which describes *what*
each package is and *where* it lives. Read this when you want to understand
intent — before proposing a change, adding a backend, or extending a command —
so your contribution pulls in the same direction as the rest of the codebase.

For the boundaries that keep ketch focused, jump to [Non-Goals & Scope](#non-goals--scope).
For where things might go next, see [`ROADMAP.md`](./ROADMAP.md).

---

## The one-sentence mental model

**Ketch is a stateless translator: it turns a URL or a query into clean,
predictable text, and then it exits.**

Everything else follows from taking that sentence literally. There is no server
to keep alive, no session to resume, no state to reconcile between calls. A
single invocation does one job and returns one result. If you find yourself
reaching for something that outlives a single process — a connection pool that
must persist, a queue, a background reconciler — stop and check it against this
model first. It is almost always the wrong shape for ketch.

Ketch serves two audiences from one binary, and the design constantly balances
them:

- **Humans** at a terminal, who want a fast replacement for
  `curl | pandoc` or a browser tab.
- **AI agents**, who want output they can parse without heuristics, exit codes
  they can branch on, and one discovery call to learn what's available.

When a design choice would help one audience and hurt the other, the tie-breaker
is: *does it make the output more predictable?* Predictability serves both.

---

## Core principles and the reasoning behind them

[`AGENTS.md`](../AGENTS.md) lists the design principles as terse bullets. This
section gives each one its *why* — the failure mode it avoids and the
alternative it rejects.

### Stateless: no daemon, no queue

**The choice:** call → result → done. Background crawls are detached
*processes*, not a server; the page cache is an embedded [bbolt](https://github.com/etcd-io/bbolt)
file, not a service.

**Why:** statelessness is what makes ketch safe to invoke thousands of times
from an agent loop without operational overhead. There is nothing to start,
nothing to monitor, nothing to leak across invocations. A crash affects exactly
one call. An operator installs a binary, not a service. The rejected
alternative — a long-lived daemon that holds a browser and a cache warm — would
be faster on paper but would trade ketch's entire operational story (a single
binary you can drop anywhere) for marginal latency. The cache already recovers
most of that latency without the daemon.

**What it constrains:** any feature that "remembers" across calls must justify
itself as either (a) an on-disk cache keyed deterministically, or (b) an
explicitly-detached process the user manages by ID. There is no third category.

### Fast path first

**The choice:** scrape does a plain HTTP fetch by default; the headless browser
engages *only* when the extract pipeline detects a JavaScript shell (an empty
mount node, a framework marker, a script-payload-dwarfs-text ratio — see
[`extract/detect.go`](../extract/detect.go)).

**Why:** the overwhelming majority of pages worth scraping render their content
server-side. Paying the cost of a headless Chrome launch on every fetch to
handle the minority would make the common case slow and heavy for no benefit.
Detecting the JS case and escalating only then keeps the common path cheap and
the hard path correct — the agent never has to know which path ran, because the
output shape is identical either way. The rejected alternative, *always
browser*, is simpler to reason about but pessimises the 90% case; the
`--force-browser` flag exists for the rare time you need to override the
heuristic, so the default stays fast without trapping anyone.

This decision has its own record: [ADR-0003](./adr/0003-fast-path-first-scrape.md).

### Interface-driven backends

**The choice:** each surface is defined by a small interface —
[`search.Searcher`](../search/search.go), [`code.Searcher`](../code/code.go),
[`docs.Searcher`](../docs/docs.go), [`cache.Store`](../cache/cache.go),
[`scrape.BrowserConn`](../scrape/browser_iface.go) — and concrete backends
implement it. A single `NewFromConfig` per package owns the backend switch.

**Why:** backends are the part of ketch most likely to change. Search providers
come and go, rate-limit, and change response shapes; the browser could one day
be something other than Rod; the cache could be Redis instead of bbolt. Pinning
each to a one- or two-method interface means adding a provider is a local,
self-contained change that touches one new file and one line of the switch —
never the command layer, never the output format. The interfaces are
deliberately *tiny* (`search.Searcher` is a single `Search` method) because a
small interface is easy to implement correctly and hard to leak provider
specifics through.

**What it asks of you:** when you add a backend, implement the interface and
register it in `NewFromConfig`. Do not thread provider-specific options up into
the command or the output. If a provider needs a credential, it comes from
config, not from a CLI flag (see *Operator configures, agent consumes*).

### Three research surfaces that never share backends

**The choice:** `ketch search` (web pages), `ketch code` (real OSS source), and
`ketch docs` (library documentation) each have their *own* `Searcher` interface
and their *own* `Result` type. They do not share backends or result shapes.

**Why:** these three jobs look similar ("search for X") but their results are
structurally different — a web result is a URL and a snippet; a code result is a
repo, a file path, and a line; a docs result is a curated, version-aware
passage. Forcing them through one interface would produce a lowest-common-
denominator result type that serves none of them well. Keeping them separate
lets each surface's output be exactly as rich as its domain requires, and lets
an agent route by intent ("is this a web question, a code question, or a docs
question?") instead of guessing.

### Operator configures, agent consumes

**The choice:** infrastructure decisions — which backend, which browser binary,
cache TTL, API keys — live in config (`~/.config/ketch/config.json` or `KETCH_*`
env). Per-invocation intent — the query, the URL, output shape — lives in flags.
Credentials are *never* flags.

**Why:** this split is what lets an agent's prompt say `ketch search "query"`
and never mention Brave, SearXNG, or an API key. The operator sets up the
environment once; every downstream invocation is provider-agnostic. It also
keeps secrets off the command line (out of process listings and shell history)
and out of tool-call parameters an agent might log. The dividing line is a
useful test for any new option: *would an agent reasonably know or care about
this value?* If not, it's config.

### Predictable output as a contract

**The choice:** default output is YAML frontmatter + markdown; `--json` is
available everywhere; exit codes are documented and stable; MCP tool errors
carry a machine-readable prefix mirroring those exit codes.

**Why:** an agent should be able to branch on ketch's behaviour without parsing
prose. A stable exit code (`5` = missing precondition, e.g. no API key) or a
stable error prefix (`[upstream]`) is control flow the agent can act on
directly: retry, reconfigure, or give up. This is why the error taxonomy is
treated as an API surface, not an implementation detail — changing an exit code
or prefix is a breaking change. See
[ADR-0002](./adr/0002-error-code-taxonomy.md).

### Smart input detection

**The choice:** `ketch scrape` figures out its input mode — single URL, multiple
args, a JSON array string, a file of URLs, or a stdin pipe — with no `--batch`
flag.

**Why:** an agent (or a human) shouldn't have to declare *how* it's passing URLs
before passing them. The tool can tell a URL from a file path from a JSON array
by looking. Every flag an agent must remember is a chance to get the call wrong;
detecting intent from the shape of the input removes a whole class of mistakes.
The principle generalises: prefer inferring intent from unambiguous input over
adding a mode flag.

### Context-aware everywhere

**The choice:** every `Searcher.Search` takes a `context.Context` as its first
parameter; fetches, browser navigation, and crawls all honour cancellation and
timeouts.

**Why:** ketch is meant to run *inside* other programs and agent loops, where
the caller owns the deadline. Threading `context.Context` through every network
boundary means a caller can cancel a slow search or bound a crawl without ketch
having its own opinion about timeouts it can't know. It's cheap to add up front
and impossible to retrofit cleanly, so it's non-negotiable for any new
network-touching code.

---

## The shape of a request

Most commands share the same skeleton. Understanding it once tells you where any
new behaviour belongs:

```
parse args/flags  →  load config (+ env overlay)  →  construct backend via
NewFromConfig  →  run the interface method (Search / Fetch / …)  →  format
output (frontmatter+markdown or JSON)  →  map error to exit code
```

- **Parsing and flags** live in `cmd/` (Cobra). This layer should contain no
  provider logic — it wires flags to a backend and a formatter.
- **Config + env overlay** is resolved once, with a fixed precedence:
  `CLI flag > KETCH_* env > config file > built-in default`
  (see [ADR-0001](./adr/0001-env-var-config.md)).
- **The backend** is chosen by `NewFromConfig` and hidden behind its interface.
- **The scrape pipeline** (`scrape/pipeline.go`) is the one place that owns
  cache lookup, fetch, JS detection, browser fallback, and extraction — shared
  verbatim by both the CLI and the MCP server so they can never drift.
- **Output formatting** is uniform across surfaces; `--json` is a formatter
  choice, not a code path fork.
- **Errors** are mapped to the stable exit-code taxonomy at the boundary.

The MCP server (`ketch mcp serve`) is not a second implementation — it calls the
*same* `NewFromConfig` constructors and the *same* scrape pipeline as the CLI,
so an agent talking MCP sees exactly what a human at the terminal sees. Keeping
one implementation behind two front-ends is a deliberate invariant: never fork
behaviour between them.

---

## Non-Goals & Scope

The fastest way to understand a focused tool is to know what it refuses to do.
These are deliberate boundaries, not missing features or todos. If a proposed
change lands on the wrong side of one of these lines, it belongs in a different
tool — or behind a very explicit, opt-in seam — not in ketch's default path.

### Ketch is not a server or a service

No daemon, no long-running process, no API you host and keep alive. If a feature
only makes sense with a warm, persistent process, it's out of scope. Background
crawls are the one concession, and they are *detached processes managed by ID*,
not a server — deliberately the minimum that "let a long crawl outlive my shell"
requires.

### Ketch does not manage state you didn't ask it to

The only persistent state ketch keeps is a deterministic on-disk page cache and
crawl status files. It does not maintain a search index of your history, a
profile, telemetry, or any cross-invocation memory. A given input produces a
given output; the cache only makes that faster, never different in kind.

### Ketch is not a general-purpose browser-automation framework

The headless browser exists for exactly one reason: to render a JavaScript shell
into HTML so the extract pipeline can read it. Clicking through flows, filling
forms, driving multi-step interactions, taking screenshots, or scripting a page
are explicitly out of scope. When you need those, you need a browser-automation
library, not a scraper. Ketch's browser is an *implementation detail of fetch*,
and it should stay invisible.

### Ketch does not try to defeat anti-bot systems

Ketch honours what a site chooses to serve to a well-behaved client, optionally
with the operator's *own* cookies attached. It does not solve CAPTCHAs, rotate
residential proxies, spoof fingerprints, or otherwise engineer its way past
access controls. Respecting a site's Terms of Service is the operator's
responsibility, and the tool is built to make honest requests, not evasive ones.

### Ketch is not a crawler-at-scale / data-warehouse

`ketch crawl` is a bounded, single-host BFS or sitemap walk for pulling a
readable slice of a site. It is not a distributed web-scale crawler, not an
archival pipeline, and not a store you query later. The MCP `crawl` tool is
capped hard (page count and wall-clock) precisely to keep it a *research*
primitive rather than an ingestion engine.

### Ketch does not embed an LLM or make ranking "smart"

Ketch fetches, extracts, and returns text; it does not summarise, re-rank by
semantic similarity, embed, or otherwise apply a model to your results. Federated
search fuses provider rankings with Reciprocal Rank Fusion — deterministic
arithmetic, not a model. The judgement about what the text *means* belongs to
whatever agent or human called ketch. Staying model-free keeps ketch cheap,
offline-friendly, deterministic, and free of a whole category of dependency and
cost.

### Ketch avoids configuration frameworks and heavy dependencies

Config is plain JSON parsed with the standard library — no config framework, no
schema DSL. The binary is `CGO_ENABLED=0` pure Go so it cross-compiles
everywhere. A new dependency has to earn its place against the cost of every
user carrying it in a single static binary; a pure-Go library that removes a
real burden is welcome, but reaching for a framework to avoid writing twenty
lines is not.

### Ketch does not gate pure-Go features behind build tags

Capabilities that are just Go code — the browser fallback, PDF extraction —
ship in the one binary, always available, no build tags. Build tags are reserved
for genuine binary-size or platform concerns, not for making optional features
feel optional. One binary should be able to do everything ketch does.

---

## How these ideas constrain a change

When you're about to add or change something, run it past the model:

1. **Does it survive a single stateless invocation?** If it needs to persist
   across calls, it must be a deterministic cache or an explicit detached
   process — nothing else.
2. **Does it keep the output predictable?** New behaviour should not change the
   shape of existing output, add a non-deterministic ordering, or break an exit
   code / error prefix.
3. **Is it behind the right interface?** Backend-specific logic goes behind the
   `Searcher`/`Store`/`BrowserConn` interface, not in the command layer.
4. **Is it config or a flag?** Infrastructure and secrets are config; per-call
   intent is a flag. Credentials are never flags.
5. **Does it stay inside a Non-Goal boundary?** If it turns ketch into a server,
   a browser-automation framework, a scale crawler, or an LLM wrapper, it's the
   wrong tool — reconsider.

If a change genuinely warrants crossing one of these lines, that's an
[ADR](./adr/)-worthy decision: write down the context and the trade-off so the
next contributor understands why the boundary moved.
