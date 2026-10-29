# ketch for pi

The official [pi](https://github.com/badlogic/pi-mono) package for the ketch
CLI. It gives a pi user what the Claude Code plugin gives a Claude Code user:
the bundled [ketch research skill](../../skills/ketch/) plus first-class tools
over the five research surfaces and `tag`.

pi can also connect to MCP servers (`ketch mcp serve` works there too), but
this package gives pi a tighter fit: the extension shells out to
`ketch <command> … --json` for every call, adds compact result rendering and
the crawl bounds, and ships the research skill. Same binary, same config, same
backends as the CLI and the MCP server.

## Install

```sh
pi install git:github.com/1broseidon/ketch          # tracks main
pi install git:github.com/1broseidon/ketch@v0.20.1  # pin a release tag
```

The package never bundles the binary. `ketch` must be on PATH:

```sh
brew install ketch
# or
npm i -g ketch-cli
# or
go install github.com/1broseidon/ketch@latest
```

A missing binary does not break pi startup. When `ketch` is not on PATH the
tools fall back to the published npm wrapper, `npx -y ketch-cli@<pinned>`, so
they work with Node alone (the first call downloads the platform binary and
gets extra time for it). Set `KETCH_PI_NO_NPX=1` to disable the fallback; the
call then returns `[precondition] ketch not on PATH: …` with install options
(`brew install ketch`, `npm i -g ketch-cli`, `go install …`). The same
`[precondition]` error appears if neither `ketch` nor `npx` exists.

On Windows, an npm install puts a `ketch.cmd` shim on PATH, and Bun refuses to
hand an argument containing a cmd.exe metacharacter (a quote, `&`, `^`) to a
batch file — which is what a multi-URL scrape sends. The tools therefore
resolve the native `ketch.exe` behind the shim and run it directly. Set
`KETCH_PI_BIN` to name a binary explicitly and skip that resolution.

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

### Tool output

On pi 1.3.0, all six tools use compact call and result cards rather than a raw
JSON dump. Search shows titles, domains and snippets; scrape shows page titles,
URLs and a short content preview; code/docs retain repository or library context.
Crawl reports page counts, errors and stop reasons; tags show counts and write
confirmations. Warnings and per-page failures remain visible. Previews show at
most four entries, with bounded one-line snippets.

Use **Ctrl+O** (pi’s default expand-tool-output binding) for the full response
and structured details. These display-only previews do not truncate or change
the content or details supplied to the model. Terminal control sequences are
stripped from displayed external text, including expanded output.

After editing a locally linked package, run **`/reload`** in the pi session
(or restart pi). There is no binary rebuild needed for renderer changes.

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

The unit tests stub `pi.exec` and run without pi or ketch installed:

```sh
npm install --legacy-peer-deps  # typebox and pi-tui for local tests; pi supplies host modules
npm test      # node --test, Node >= 22.18 (native type stripping)
```

They cover argument construction per tool (`--json` always, flags only when
set), exit-code → prefix mapping, the missing binary, the abort signal, and the
crawl reshaping. Keep the option sets in step with `mcp/*.go` when either side
changes.

Tool inputs never reach the CLI as options. Every flag value is bound with `=`,
and the query, URL or tag name follows a `--`, so a query of `--help` is
searched rather than run. Scrape URLs always go as one JSON-array argument,
because the CLI treats a lone argument that names an existing path as a file
of URLs. Crawl `allow`/`deny` patterns are CSV-quoted, because Cobra parses
those flags as CSV and would split a regex such as `[0-9]{1,4}` at its comma.
`ketch.cli.test.ts` checks these rules against the real parser. It runs a
binary built from this checkout (or `KETCH_BIN`) against a local HTTP server,
skips itself when Go isn't installed, and fails if Go is present but the build breaks. Inherited `KETCH_*` variables are dropped so an operator's settings never leak into the tests.

For a checked CLI run, build explicitly and remove inherited `KETCH_*` settings
before running `npm test` with `KETCH_BIN` pointing to that build. The CLI test
fixture isolates HOME, config and tags, but otherwise inherits the environment.
An explicit successful build avoids mistaking an automatically skipped CLI suite
for a passing integration test. Renderer tests also exercise the real pi-tui Text
component at narrow widths; they need neither a model nor network services.
