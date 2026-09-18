#!/usr/bin/env node
// Builds the npm packages for a released version.
//
//   node npm/build.mjs v0.17.0
//
// Downloads the release archives from GitHub, verifies each against
// checksums.txt, and writes one package per platform under npm/platforms/,
// stamping the version into the root package and its optionalDependencies.
// Nothing is published here — see `npm publish` in the release workflow.

import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync, readFileSync, copyFileSync, chmodSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const REPO = '1broseidon/ketch'
const ROOT = path.dirname(fileURLToPath(import.meta.url))

// node platform-arch -> the goreleaser archive's os/arch spelling
const TARGETS = {
  'darwin-arm64': { os: 'darwin', arch: 'arm64', ext: 'tar.gz' },
  'darwin-x64': { os: 'darwin', arch: 'x86_64', ext: 'tar.gz' },
  'linux-arm64': { os: 'linux', arch: 'arm64', ext: 'tar.gz' },
  'linux-x64': { os: 'linux', arch: 'x86_64', ext: 'tar.gz' },
  'win32-arm64': { os: 'windows', arch: 'arm64', ext: 'zip' },
  'win32-x64': { os: 'windows', arch: 'x86_64', ext: 'zip' },
}

const raw = process.argv[2]
if (!raw) {
  console.error('usage: node npm/build.mjs <version>   (e.g. v0.17.0)')
  process.exit(2)
}
// Accept v0.17.0, 0.17.0, or the raw refs/tags/v0.17.0 that a tag push gives us.
const cleaned = raw.replace(/^refs\/tags\//, '')
const tag = cleaned.startsWith('v') ? cleaned : `v${cleaned}`
const version = tag.slice(1)
if (!/^\d+\.\d+\.\d+/.test(version)) {
  console.error(`could not read a version out of "${raw}"`)
  process.exit(2)
}

const base = `https://github.com/${REPO}/releases/download/${tag}`
const tmp = mkdtempSync(path.join(tmpdir(), 'ketch-npm-'))

async function download(url) {
  const res = await fetch(url, { redirect: 'follow' })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText} for ${url}`)
  return Buffer.from(await res.arrayBuffer())
}

function sha256(buf) {
  return createHash('sha256').update(buf).digest('hex')
}

console.log(`building npm packages for ${tag}`)

// checksums.txt lines are "<sha256>  <filename>"
const checksums = new Map(
  (await download(`${base}/checksums.txt`))
    .toString('utf8')
    .split('\n')
    .filter(Boolean)
    .map((line) => {
      const [hash, name] = line.trim().split(/\s+/)
      return [name, hash]
    })
)

const platformsDir = path.join(ROOT, 'platforms')
rmSync(platformsDir, { recursive: true, force: true })

for (const [target, { os, arch, ext }] of Object.entries(TARGETS)) {
  const asset = `ketch_${version}_${os}_${arch}.${ext}`
  const expected = checksums.get(asset)
  if (!expected) throw new Error(`${asset} is not listed in checksums.txt for ${tag}`)

  const buf = await download(`${base}/${asset}`)
  const actual = sha256(buf)
  if (actual !== expected) {
    throw new Error(`checksum mismatch for ${asset}\n  expected ${expected}\n  actual   ${actual}`)
  }

  const archive = path.join(tmp, asset)
  writeFileSync(archive, buf)

  const unpack = path.join(tmp, target)
  mkdirSync(unpack, { recursive: true })
  if (ext === 'zip') {
    execFileSync('unzip', ['-q', '-o', archive, '-d', unpack])
  } else {
    execFileSync('tar', ['-xzf', archive, '-C', unpack])
  }

  const exe = os === 'windows' ? 'ketch.exe' : 'ketch'
  const pkgDir = path.join(platformsDir, target)
  mkdirSync(path.join(pkgDir, 'bin'), { recursive: true })
  copyFileSync(path.join(unpack, exe), path.join(pkgDir, 'bin', exe))
  if (os !== 'windows') chmodSync(path.join(pkgDir, 'bin', exe), 0o755)

  const [nodeOs, nodeCpu] = target.split('-')
  writeFileSync(
    path.join(pkgDir, 'package.json'),
    JSON.stringify(
      {
        name: `@ketch-cli/${target}`,
        version,
        description: `The ${target} binary for ketch-cli.`,
        homepage: 'https://ketch.run',
        repository: { type: 'git', url: `git+https://github.com/${REPO}.git` },
        bugs: { url: `https://github.com/${REPO}/issues` },
        license: 'MIT',
        author: '1broseidon',
        os: [nodeOs],
        cpu: [nodeCpu],
        files: ['bin/', 'README.md'],
      },
      null,
      2
    ) + '\n'
  )

  // A real README on every package: it is what a human lands on from the
  // registry, and a bare package.json plus one binary reads as spam to npm.
  writeFileSync(
    path.join(pkgDir, 'README.md'),
    `# @ketch-cli/${target}

The ${nodeOs}/${nodeCpu} binary for [**ketch**](https://ketch.run) — a fast,
stateless CLI for web search, OSS code search, library docs, scraping, and
crawling, with an MCP server for agents.

This package is not meant to be installed directly. It is one of the
platform-specific binaries that [\`ketch-cli\`](https://www.npmjs.com/package/ketch-cli)
declares as optional dependencies; npm installs only the one matching your
machine, so nothing is downloaded by a postinstall script.

Install the real package instead:

\`\`\`sh
npm install -g ketch-cli
\`\`\`

Or run it without installing:

\`\`\`sh
npx -y ketch-cli search "your query"
npx -y ketch-cli mcp serve
\`\`\`

Documentation: [ketch.run](https://ketch.run) ·
Source: [github.com/${REPO}](https://github.com/${REPO}) ·
License: MIT
`
  )

  console.log(`  ${target.padEnd(14)} ${asset}  ${(buf.length / 1048576).toFixed(1)} MiB  verified`)
}

// Stamp the version into the root package and pin every optional dependency.
const rootPkgPath = path.join(ROOT, 'package.json')
const rootPkg = JSON.parse(readFileSync(rootPkgPath, 'utf8'))
rootPkg.version = version
for (const target of Object.keys(TARGETS)) {
  rootPkg.optionalDependencies[`@ketch-cli/${target}`] = version
}
writeFileSync(rootPkgPath, JSON.stringify(rootPkg, null, 2) + '\n')

rmSync(tmp, { recursive: true, force: true })
console.log(`\nready: npm/ (root) and ${Object.keys(TARGETS).length} platform packages at ${version}`)
