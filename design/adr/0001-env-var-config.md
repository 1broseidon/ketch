# ADR 0001: Environment-variable configuration overlay

**Status:** Accepted · **Date:** 2026-07-16 · **Issue:** #26

## Context

ketch reads its configuration from a single JSON file
(`~/.config/ketch/config.json`). Containerized and CI deployments want to
inject settings — above all API keys like `brave_api_key` — through the
environment instead of writing a file into the image (#26).

## Decision

Add a small environment overlay on top of the existing file loader. No config
framework; a table-driven overlay in `config/config.go`.

### Naming convention

Each overridable JSON key maps mechanically to `KETCH_` + UPPER_SNAKE of the
key: `limit` → `KETCH_LIMIT`, `brave_api_key` → `KETCH_BRAVE_API_KEY`,
`external_pdf_to_md_converter_timeout_sec` →
`KETCH_EXTERNAL_PDF_TO_MD_CONVERTER_TIMEOUT_SEC`. Predictable: given a key
from `ketch config set`, the env var name needs no lookup table.

### Precedence

```
CLI flag  >  KETCH_* env  >  config file  >  built-in default
```

Flags already default to the loaded config value, so applying env between
file and flag parsing preserves that chain. An env var set to the empty
string is treated as unset.

### File-only exceptions

Three kinds of keys have **no** env override, on purpose:

- `url_rewrites` — a JSON array of regex `{match, replace}` rules; regexes
  and JSON quoting inside shell-quoted env values are an escaping minefield.
- `spa_markers` — arbitrary HTML substrings; no delimiter is safe to reserve
  (markers may contain commas, spaces, or quotes).
- `*_api_keys` (plural pools) — instead, the **singular** per-provider var
  accepts a comma-separated list: `KETCH_BRAVE_API_KEY=key1,key2` replaces
  the whole effective Brave pool. Keys never contain commas, so the split is
  safe, and one var per provider stays predictable.

### github_token exception

`github_token` stays out of the generic overlay. It already has a dedicated
resolution chain in `Config.ResolveGithubToken()`, which now resolves:
`KETCH_GITHUB_TOKEN` > config file > ambient `$GITHUB_TOKEN` / `$GH_TOKEN` >
`gh auth token`. Routing it through the generic overlay would silently invert
the config-file-vs-ambient-env ordering that chain already guarantees.

### Load split and fail-loud timing

- `config.LoadFile()` — file only. Used by `ketch config set` / `init`, so
  env-derived values are never persisted back into the config file.
- `config.Load()` — file + env overlay. Returns a `LoadResult{Config,
  Overrides}` plus an error for invalid env values (e.g. `KETCH_LIMIT=abc`).

Invalid env values fail loud with the offending var named — but only when a
command actually consumes config. The root command records the load error at
init and surfaces it from `PersistentPreRunE`; commands that never read
config (`version`, `help`, `completion`, and the file-only `config
init/set/path`) still work under a broken environment.

### Provenance

`LoadResult.Overrides` records, per overridden key, the env var applied and
the value it replaced (redacted for secrets). `ketch config show` prints this
as an `env_overrides` section so an operator can see *why* the effective
config differs from the file.

### Escape hatch

`KETCH_CONFIG=<path>` points every read *and* write (`config set`, `config
path`) at an alternate config file.

### Secret hygiene for subprocesses

ketch spawns a headless browser (rod launcher) and an optional external PDF
converter. Both now receive an environment scrubbed of the `KETCH_*` secret
vars (`KETCH_*_API_KEY`, `KETCH_GITHUB_TOKEN`), so injected credentials do
not leak into child processes. The background-crawl re-exec of ketch itself
keeps the full environment — it *is* ketch and needs the same config.

## Consequences

- Seventeen keys are env-overridable; the full set is the `config set` key list
  minus `url_rewrites`, `spa_markers`, the four `*_api_keys` plurals, and
  `github_token`.
- `ketch config set` round-trips the file untouched by the environment.
- A bad env var can no longer break `ketch version` (previously
  `config.Load()` ran at package init for every command).
