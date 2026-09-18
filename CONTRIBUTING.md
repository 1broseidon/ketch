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

## Publishing to npm (maintainers)

ketch ships to npm as `ketch-cli` — the bare `ketch` name belongs to an
unrelated package. The binary is delivered through six per-platform packages
under the `@ketch-cli` scope, declared as `optionalDependencies`, so npm
installs only the one that matches and nothing is downloaded during install:

| Package | For |
|---|---|
| `ketch-cli` | The one users install; carries the launcher and the bin names |
| `@ketch-cli/darwin-arm64` · `@ketch-cli/darwin-x64` | macOS |
| `@ketch-cli/linux-arm64` · `@ketch-cli/linux-x64` | Linux |
| `@ketch-cli/win32-arm64` · `@ketch-cli/win32-x64` | Windows |

Only the root is unscoped, because it is the name people type. The binaries
are scoped so six machine-specific packages don't sit at the registry root —
the same split esbuild uses.

Releases publish through **npm trusted publishing**: the `npm` job in
`.github/workflows/release.yml` mints a GitHub OIDC token and exchanges it for
a short-lived credential. There is no `NPM_TOKEN` secret, and provenance
attestations are generated automatically.

Trusted publishing is configured per package and a package must exist before it
can be configured, so the seven packages need one manual bootstrap publish:

```sh
npm login                      # browser flow; no token stored anywhere
node npm/build.mjs v0.17.0     # verifies every archive against checksums.txt
for d in npm/platforms/*/; do npm publish "$d" --access public; done
npm publish npm/ --access public
```

`--access public` matters: scoped packages publish as restricted by default.

Then, once for each of the seven packages, on npmjs.com → Packages →
`<package>` → Settings → Trusted publishing:

| Field | Value |
|---|---|
| Publisher | GitHub Actions |
| Organization or user | `1broseidon` |
| Repository | `ketch` |
| Workflow filename | `release.yml` |

Afterwards, set Settings → Publishing access to **"Require two-factor
authentication and disallow tokens"** on each package. Trusted publishing keeps
working — that setting only closes the long-lived-token path — and `npm logout`
locally once the bootstrap is done.

From then on, tagging a release publishes all seven packages with no secret in
the repository.
