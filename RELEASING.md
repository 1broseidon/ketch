# Releasing ketch

Maintainer runbook. Contributors don't need any of this —
see [CONTRIBUTING.md](CONTRIBUTING.md).

Pushing a `v*` tag runs [`.github/workflows/release.yml`](.github/workflows/release.yml),
which does everything below. Cutting a release is:

```sh
# update CHANGELOG.md, then
git commit -m "chore(release): vX.Y.Z"
git push origin main
git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z
```

The workflow has three jobs: `ref` resolves the tag, `release` runs GoReleaser
(binaries, checksums, the GitHub release, and the winget PR), and `npm` builds
and publishes the npm packages. `npm` depends on `release`, so a GoReleaser
failure stops it.

If the GitHub release went out but `npm` did not run (a later step of the
`release` job failed, as winget did for v0.18.0), do not re-run GoReleaser:
it refuses to overwrite the assets on an existing release (`422 Validation
Failed`). Publish npm on its own instead:

```sh
gh workflow run release.yml --ref main -f tag=vX.Y.Z -f npm_only=true
```

## Distribution channels

| Channel | How it updates |
|---|---|
| GitHub release | GoReleaser, from the tag |
| Install script | Resolves `/releases/latest` at run time — nothing to publish |
| Homebrew | homebrew-core autobumps within a few hours; no action, no tap |
| npm | The `npm` job, via trusted publishing |
| winget | GoReleaser opens a PR against `microsoft/winget-pkgs` |

## npm

ketch publishes as `ketch-cli` — the bare `ketch` name belongs to an unrelated
package. The binary is delivered through six per-platform packages under the
`@ketch-cli` scope, declared as `optionalDependencies`, so npm installs only
the one that matches and nothing is downloaded during install:

| Package | For |
|---|---|
| `ketch-cli` | The one users install; carries the launcher and the bin names |
| `@ketch-cli/darwin-arm64` · `@ketch-cli/darwin-x64` | macOS |
| `@ketch-cli/linux-arm64` · `@ketch-cli/linux-x64` | Linux |
| `@ketch-cli/win32-arm64` · `@ketch-cli/win32-x64` | Windows |

Only the root is unscoped, because it is the name people type. The binaries are
scoped so six machine-specific packages don't sit at the registry root — the
same split esbuild uses.

Publishing goes through **npm trusted publishing**: the `npm` job mints a
GitHub OIDC token and exchanges it for a short-lived credential. There is no
`NPM_TOKEN` secret, and provenance attestations are generated automatically —
which is why `--provenance` is deliberately absent from the publish commands.

### The propagation gate

The job publishes the six platform packages, **waits until all six are readable
from the public registry**, and only then publishes the root.

That wait is not optional. Every package is cached at the registry CDN under
its own ~5 minute TTL, so they become publicly readable at independent times no
matter what order they were published in. While a binary is still invisible,
npm treats the root's unresolvable `optionalDependency` as absent rather than
failing: it installs a `ketch-cli` with no binary, and npx then caches that
broken tree indefinitely. v0.17.1 left a ~150s window of exactly this before
the gate existed.

Two consequences worth remembering when debugging an install:

- **Checking whether a package name exists before publishing poisons the
  cache.** npm's CDN negative-caches a 404 for ~5 minutes, so a name you probed
  beforehand appears missing after you publish it.
- **`npm install --include=optional` does not fix an npx install.** npx
  resolves out of its own tree under `~/.npm/_npx/<hash>/` and reuses it even
  with `-y`. Delete that directory instead; the launcher's error message prints
  the exact path.

### Bootstrapping a new package

Trusted publishing is configured per package and a package must exist before it
can be configured, so a genuinely new package needs one manual publish:

```sh
npm login                      # browser flow; no token stored anywhere
node npm/build.mjs vX.Y.Z      # verifies every archive against checksums.txt
for d in npm/platforms/*/; do npm publish "$d" --access public; done
npm publish npm/ --access public
```

`--access public` matters: scoped packages publish as restricted by default.

Then, on npmjs.com → Packages → `<package>` → Settings → Trusted publishing:

| Field | Value |
|---|---|
| Publisher | GitHub Actions |
| Organization or user | `1broseidon` |
| Repository | `ketch` |
| Workflow filename | `release.yml` |
| Environment | *(leave blank)* |

Leave Environment empty — the `npm` job declares no `environment:`, and a value
that doesn't match fails the OIDC exchange at publish time. Check **Allow
`npm publish`** in the same form: without it the publisher may only *stage*, and
the job's direct `npm publish` is rejected.

Set Publishing access to "Require two-factor authentication and disallow
tokens" only *after* a release has proven the OIDC path. Trusted publishing
keeps working under it, but if the config is wrong, that setting removes the
manual fallback at the same moment you find out.

## winget

GoReleaser's `winget` block generates three manifests, commits them to a branch
on the `1broseidon/winget-pkgs` fork, and opens a cross-repo PR against
`microsoft/winget-pkgs`. This needs:

- the fork to exist, and
- a `WINGET_TOKEN` secret with push access to it. The default `GITHUB_TOKEN` is
  scoped to this repository and can neither push to the fork nor open the PR.

The Windows archives are already zip, which winget consumes as a portable
nested in the archive (`NestedInstallerType: portable`, with
`PortableCommandAlias: ketch`), so there is no MSI to build and no code signing
to arrange.

`skip_upload: auto` keeps prereleases out of winget entirely.

**Currently skipped.** `release.yml` passes `--skip=winget` since v0.18.0: that
run pushed the manifests to the fork and then failed with `403 Resource not
accessible by personal access token` opening the PR on `microsoft/winget-pkgs`,
which took the dependent `npm` job down with it. A fine-grained PAT cannot be
granted on a repository you do not own, so opening the cross-repo PR needs a
classic PAT with the `public_repo` scope. Re-enable by replacing `WINGET_TOKEN`
with such a token and dropping the `--skip=winget` flag once the first package
PR (#437139) is approved; until then, open version PRs by hand from the
`ketch-<version>` branch GoReleaser leaves on the fork.

Notes:

- **The first submission of a new package waits on a human moderator.** Later
  versions usually merge automatically once validation passes.
- **Microsoft's pipeline runs the installer on a VM, with Defender scanning.**
  An unsigned binary from an unfamiliar publisher can draw extra scrutiny.
- **The manifest schema version comes from GoReleaser, not from us.** The
  workflow pins `version: latest`, so the schema tracks whatever the current
  GoReleaser emits. A local `goreleaser` that has drifted behind will generate
  a different schema version than CI does — upgrade before trusting a local
  `--snapshot` to tell you what will ship.
- The fork drifts behind upstream over time. That is harmless, since the PR
  only touches `manifests/1/1broseidon/ketch/`, but
  `gh repo sync 1broseidon/winget-pkgs` is the fix if a release ever fails on a
  branch conflict.

### Testing a manifest before submitting

`winget validate` and `winget install` need Windows. On a test machine, once:

```powershell
winget settings --enable LocalManifestFiles
```

then, against a directory holding only the three manifest files:

```powershell
winget validate --manifest <dir>
winget install --manifest <dir>
Get-Command ketch -All | Select-Object Source
ketch version
```

Check `Source` rather than trusting `ketch version`: a `ketch.exe` from
`go install` sitting in `$(go env GOPATH)\bin` shadows the winget one and
reports the same version string, which turns the install test into a false
pass. The manifest is only proven if the resolved path is under
`%LOCALAPPDATA%\Microsoft\WinGet\Links`.
