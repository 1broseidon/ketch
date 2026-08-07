# ADR 0002: Stable exit-code / error-prefix taxonomy

**Status:** Accepted · **Date:** 2026-08-04

## Context

Ketch is designed to run inside scripts and AI-agent loops, not just at an
interactive prompt. A caller that only gets free-form text on stderr has to
parse prose to decide what to do next — and prose changes. Two questions come up
constantly in that setting and must be answerable without guessing:

- *Is this my fault or the network's?* "Bad input" and "upstream is down" call
  for opposite reactions: fix the request versus retry later.
- *Can I branch on it programmatically?* An agent needs a signal it can switch
  on, not a sentence it has to pattern-match.

The CLI and the MCP server both need to answer these, and MCP has no structured
error-category field — a tool error is just a message string.

## Decision

Define a small, fixed taxonomy of failure categories and expose it two ways.

**As CLI exit codes:**

| Code | Meaning | Typical cause |
|------|---------|---------------|
| `0` | success | — |
| `2` | validation | malformed input, bad flag combination |
| `3` | not found | query/URL yielded nothing |
| `4` | upstream | network or provider failure |
| `5` | precondition | missing prerequisite (e.g. no API key) |
| `6` | cancelled | SIGINT/SIGTERM, context cancelled |

**As MCP tool-error prefixes:** every tool error message begins with a stable,
machine-readable prefix mirroring the same categories — `[validation]`,
`[not_found]`, `[upstream]`, `[precondition]`, `[cancelled]`. Because MCP
exposes no structured error field, the prefix *is* the contract.

The taxonomy is treated as a **public API surface**. Changing what a code or
prefix means — or which category a given failure maps to — is a breaking change,
handled with the same care as changing output format.

## Consequences

- Callers can implement control flow directly: retry on `4`/`[upstream]`,
  reconfigure on `5`/`[precondition]`, fix-and-resubmit on `2`/`[validation]`,
  stop on `3`/`[not_found]`.
- The CLI and MCP surfaces stay in lockstep: the same underlying failure yields
  the matching exit code and prefix, because both derive from one internal
  classification.
- New failure modes must be mapped onto an existing category rather than
  inventing a new code casually; adding a category is a deliberate, documented
  change.
- The categories are intentionally coarse. Finer-grained diagnostics still go in
  the human-readable message; the code/prefix is only the *class* of failure, so
  the branching contract stays small and stable.
