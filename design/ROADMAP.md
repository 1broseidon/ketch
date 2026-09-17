# Ketch — Roadmap

This is a set of **possible directions**, not a schedule or a list of
commitments. It exists to give contributors (human or agent) a sense of where
the design *wants* to go, so independent work can pull in a compatible
direction. Anything here can change, be reordered, or be dropped. There are no
dates.

Every item is checked against the boundaries in
[`DESIGN.md` → Non-Goals & Scope](./DESIGN.md#non-goals--scope). Directions that
would cross those lines are not listed; that's the point of having them.

## Guiding design goals

These are the standing goals every direction should serve — the "north star"
against which a proposal is measured:

- **Stay a single stateless binary.** New capability should not require a
  service, a daemon, or state beyond a deterministic cache.
- **Keep output predictable.** Additions should preserve existing output shapes
  and the exit-code / error-prefix contract.
- **Widen coverage through interfaces, not special cases.** Prefer a new backend
  behind an existing interface over a bespoke code path.
- **Pure Go, cross-compiles everywhere.** Honour `CGO_ENABLED=0`; a new
  dependency must earn its size.
- **Serve humans and agents equally.** If a feature only helps one audience,
  make sure it doesn't cost the other.

## Directions, roughly in dependency order

The ordering reflects what unblocks what, not priority. Earlier items tend to
enable later ones.

### 1. Local, offline docs backend (`ketch docs -b local`) — landed, growing

The first cut shipped: `ketch docs add <name> <url>` pulls a documentation
site into a per-user SQLite FTS5 library (see
[ADR-0004](adr/0004-local-docs-corpus.md)), `ketch docs -b local` searches it
offline with no API key, and `.ketch/docs.json` scopes a project to the
libraries it uses. What remains is depth rather than plumbing: fresher
`sync` (conditional requests, changed-page detection instead of a full
re-pull), a per-library `llms.txt`-style export agents can read whole, tuning
the BM25 column weights and section size on more real-world sites, and an
MCP surface for `docs add` so an agent talking MCP can build a library the
way a CLI agent can (the MCP `docs` tool already searches local libraries).

*Enables:* everything that benefits from durable, queryable local content.

### 2. Richer, still-deterministic result fusion

Federated `--multi` search fuses provider rankings with Reciprocal Rank Fusion
today. There's room to improve *how* results are merged and deduplicated —
better canonicalisation of near-duplicate URLs, smarter tie-breaking — while
keeping the fusion strictly deterministic and model-free. The line to hold:
improvements stay arithmetic, not learned ranking (that's a Non-Goal).

*Depends on:* the existing multi-backend and canonicalisation machinery.

### 3. Curated backend coverage across all three surfaces

Additional search, code, and docs providers can give operators useful choices.
Proposals follow the [admission criteria](../CONTRIBUTING.md#proposing-a-provider),
with established use, competitive results, and a stable public API.

Each admitted provider uses its surface's interface and registry, keeping
provider-specific changes out of shared production consumers. The
[provider guide](../AGENTS.md#adding-a-provider) covers implementation and tests.

*Depends on:* a provider meeting the admission criteria; the registries are
already in place.

### 4. Extraction fidelity on more of the long tail

The JS-shell detector and the readability + markdown pipeline are where scrape
quality lives. Directions include recognising more client-rendered frameworks,
sharpening the "is this a JS shell?" heuristic to reduce both false browser
launches and missed escalations, and improving markdown fidelity for awkward
document structures. The operator escape hatches (`spa_markers`,
`--force-browser`) stay the pressure-relief valve while the built-in heuristics
improve.

*Depends on:* the existing detector and extract pipeline; benefits every surface
that fetches (`scrape`, `search --scrape`, `crawl`).

### 5. Deeper agent-integration ergonomics

Ketch is already agent-friendly (JSON everywhere, exit-code control flow, one
discovery call, an MCP server). Directions here refine that contract: keeping
the MCP tool surface in lockstep with the CLI, sharpening error taxonomies so
agents can branch more precisely, and making capability discovery
(`ketch config`) as self-describing as possible — so an agent can learn what's
available and how to react to failure without out-of-band knowledge.

*Depends on:* the stable error taxonomy ([ADR-0002](./adr/0002-error-code-taxonomy.md))
and the shared CLI/MCP pipeline staying unified.

### 6. Cache and storage flexibility

The page cache sits behind a `cache.Store` interface with a bbolt default. The
interface is ready for alternative backends where an operator's environment
calls for it, without changing how any command behaves. This is intentionally
low on the list: the default is good, and the seam exists precisely so this can
happen when there's real demand, not speculatively.

*Depends on:* the `Store` interface (already in place).

## What is deliberately *not* on this roadmap

To keep the direction honest, these are named as out of scope rather than
left as "someday" — see [`DESIGN.md`](./DESIGN.md#non-goals--scope) for the
reasoning:

- A hosted service, daemon, or persistent server mode.
- General browser automation (form-filling, click-through flows, screenshots).
- Anti-bot evasion (CAPTCHA solving, proxy rotation, fingerprint spoofing).
- A web-scale or archival crawler.
- Built-in summarisation, embeddings, or learned/semantic ranking.

If you want to work on one of these, ketch is probably not the right home for
it — and that's by design.
