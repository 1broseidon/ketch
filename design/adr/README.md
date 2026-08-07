# Architecture Decision Records

This directory holds **ADRs** — short, immutable records of significant
architectural decisions: the context that forced a choice, the choice itself,
and the consequences of living with it. They capture *why* the code is the way
it is, so a decision doesn't have to be re-litigated from scratch later.

An ADR is warranted when a decision is hard to reverse, shapes more than one
package, or would otherwise be surprising to a contributor who wasn't there when
it was made. For the broader design rationale that individual ADRs slot into,
see [`../DESIGN.md`](../DESIGN.md).

## Records

| # | Title | Status |
|---|-------|--------|
| [0001](./0001-env-var-config.md) | Environment-variable configuration overlay | Accepted |
| [0002](./0002-error-code-taxonomy.md) | Stable exit-code / error-prefix taxonomy | Accepted |
| [0003](./0003-fast-path-first-scrape.md) | Fast-path-first scraping (HTTP before browser) | Accepted |

## Writing a new ADR

- Copy the shape of an existing record: a header with **Status** and **Date**
  (link an issue if there is one), then **Context**, **Decision**, and
  **Consequences**.
- Number sequentially (`NNNN-short-kebab-title.md`) and add a row to the table
  above.
- ADRs are append-only. Don't rewrite an accepted record to reflect a new
  decision — write a new ADR that supersedes it, and mark the old one
  `Superseded by NNNN`.
- Keep it factual and self-contained. An ADR should make sense to a reader who
  has only the public repository in front of them.
