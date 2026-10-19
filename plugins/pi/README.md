# ketch for pi

The official [pi](https://github.com/badlogic/pi-mono) package for the ketch
CLI. It gives a pi user what the Claude Code plugin gives a Claude Code user:
the bundled [ketch research skill](../../skills/ketch/) plus first-class tools
over the five research surfaces and `tag`.

pi does not speak MCP, so instead of `ketch mcp serve` the package ships an
extension that shells out to `ketch <command> … --json` for every call. Same
binary, same config, same backends as the CLI and the MCP server.

## Install

```sh
pi install git:github.com/1broseidon/ketch          # tracks main
pi install git:github.com/1broseidon/ketch@v0.20.0  # pin a release tag
```

The package never bundles the binary. `ketch` must be on PATH:

```sh
brew install ketch
# or
go install github.com/1broseidon/ketch@latest
```

A missing binary does not break pi startup; the first tool call returns
`[precondition] ketch not on PATH: …` so the agent can tell you.

## What it registers

The manifest is the repo's root `package.json` (`"pi"` key). pi git installs
take a repository root, which is why it lives there rather than in this
directory.

**Skill** — `skills/ketch/SKILL.md`, the same playbook the Claude Code plugin
installs: surface routing, token budgets, error-prefix control flow, a
deep-research verb and guided backend setup.

**Tools** — `plugins/pi/extensions/ketch.ts`, one per surface, with the same
per-call options as the MCP tools in [`mcp/`](../../mcp/):

| Tool | Runs | Options |
| --- | --- | --- |
| `ketch_search` | `ketch search` | `query`, `backend`, `multi`, `random`, `limit`, `searxng_url`, `scrape`, `trim`, `max_chars`, `tag` |
| `ketch_code` | `ketch code` | `query`, `backend`, `lang`, `repo`, `regexp`, `limit`, `tag` |
| `ketch_docs` | `ketch docs` | `query`, `backend`, `library`, `tokens`, `limit`, `resolve`, `tag` |
| `ketch_scrape` | `ketch scrape` | `url` or `urls`, `selector`, `raw`, `force_browser`, `no_llms_txt`, `trim`, `max_chars`, `no_cache`, `concurrency`, `tag` |
| `ketch_crawl` | `ketch crawl` | `url`, `depth`, `sitemap`, `allow`, `deny`, `max_pages`, `max_chars`, `no_cache`, `tag` |
| `ketch_tag` | `ketch tag …` | `operation` (add/show/list/remove), `tag`, `urls`, `limit` |

Config-level settings — API keys, cache TTL, browser binary, cookies, User-Agent
— are not tool params, exactly as over MCP. The operator sets them once with
`ketch config set`; the agent consumes them.

**Command** — `/ketch-doctor` runs `ketch doctor` and shows the report as a
notification. Operator actions (`config`, `cache`, `browser install`,
background crawls) stay CLI-only, same as the MCP server.

### Errors

Tool errors carry the same stable prefixes as the MCP server, mapped from the
CLI's exit codes, and are returned with `isError: true` so the model sees them:

| Exit | Prefix | Meaning |
| --- | --- | --- |
| 2 | `[validation]` | fix the input; retrying unchanged cannot succeed |
| 3 | `[not_found]` | nothing matched |
| 4 | `[upstream]` | backend or network failure; retrying may help |
| 5 | `[precondition]` | ketch is not installed, or the operator must configure something |
| 6 | `[cancelled]` | cancelled or timed out |
| other | `[upstream]` | with ketch's stderr |

### Crawl bounds

`ketch_crawl` is synchronous and bounded like the MCP `crawl` tool: `max_pages`
defaults to 30 and is capped at 100, inside a 3-minute budget. The CLI has no
page cap, so the extension runs the crawl to completion or the deadline and
keeps the first `max_pages` pages, reporting `stopped: "max_pages"` or
`"timeout"` with whatever was collected. Per-URL crawl failures come back in
`errors`. Larger crawls belong to `ketch crawl --background` on the CLI.

## Relation to the CLI and MCP server

- **CLI** is the primary interface and the only prerequisite. The skill tells
  the agent to prefer whatever transport the operator wired up.
- **MCP server** (`ketch mcp serve`) is for clients that speak MCP; the Claude
  Code plugin at [`plugins/ketch/`](../ketch/) registers it.
- **pi package** (this) is the pi equivalent: skill + tools, no server.

## Development

Tests stub `pi.exec` and run without pi or ketch installed:

```sh
npm install   # dev-only typebox; host packages are peerDependencies and never installed here
npm test      # node --test, Node >= 22.18 (native type stripping)
```

They cover argument construction per tool (`--json` always, flags only when
set, batch `urls` as positional args), exit-code → prefix mapping, the missing
binary, the abort signal, and the crawl reshaping. Keep the option sets in step
with `mcp/*.go` when either side changes.
