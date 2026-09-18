# Contributing to ketch

Thanks for contributing to ketch. Bug fixes, documentation, and improvements
within the [project's scope](design/DESIGN.md#non-goals--scope) are welcome.

## Proposing a provider

Ketch maintains a curated set of supported providers. Please open an issue
before implementing a new provider so we can discuss its fit.

Providers must meet these criteria:

- **Search is the primary product:** web search, code search, or documentation
  search.
- **Established use:** an active community or users beyond the provider's own
  team, with a demonstrated maintenance history.
- **Competitive results:** useful results and substantive snippets, comparable
  to existing backends on the same search surface and queries. Maintainers must
  be able to evaluate the service without purchasing a plan.
- **A public API:** stable, documented, versioned JSON endpoints with published
  terms and self-service access. New integrations must use APIs; the existing
  DuckDuckGo integration is a legacy exception.

Please disclose any affiliation with the provider. Inclusion means ongoing
support and documentation, so meeting these criteria does not guarantee
acceptance.

## Implementing a provider

Follow the [provider guide](AGENTS.md#adding-a-provider). Keep the implementation,
descriptor, and health probe together, add tests, and register the descriptor
in `registry.go`.

Shared production code must remain independent of provider names. Fixture
updates should only add the provider's entries. Include its README and site
listings and a changelog entry in the same PR.

## Submitting a pull request

Keep changes focused, include relevant tests, and preserve existing output
formats and error contracts. Explain any necessary breaking changes or new
dependencies. Use the standard library for HTTP and JSON integrations.

Use the Go version specified in [go.mod](go.mod), keep the build compatible with
`CGO_ENABLED=0`, and run `make build`, `make lint`, and `make test`.

## Reporting a problem

Include the command, expected and actual behavior, and `ketch version`. For
backend issues, include `ketch doctor` output.
