#!/usr/bin/env node
'use strict'

// Launcher for the ketch binary.
//
// The real executable ships in a per-platform package declared as an optional
// dependency, so npm installs exactly one of them and nothing is downloaded at
// install time. This shim resolves that package and execs the binary with stdio
// inherited — which is what makes `npx ketch-cli mcp serve` usable as an MCP
// server: the child speaks the protocol directly over the inherited pipes.

const { spawn } = require('child_process')
const path = require('path')

const PACKAGES = {
  'darwin-arm64': '@ketch-cli/darwin-arm64',
  'darwin-x64': '@ketch-cli/darwin-x64',
  'linux-arm64': '@ketch-cli/linux-arm64',
  'linux-x64': '@ketch-cli/linux-x64',
  'win32-arm64': '@ketch-cli/win32-arm64',
  'win32-x64': '@ketch-cli/win32-x64',
}

const target = `${process.platform}-${process.arch}`
const pkg = PACKAGES[target]

if (!pkg) {
  console.error(`ketch: no prebuilt binary for ${target}.`)
  console.error(`Supported platforms: ${Object.keys(PACKAGES).join(', ')}`)
  console.error('Build from source instead: go install github.com/1broseidon/ketch@latest')
  process.exit(1)
}

const exe = process.platform === 'win32' ? 'ketch.exe' : 'ketch'

let binary
try {
  // Resolve via package.json rather than the binary itself: the file has no
  // extension, and package "exports" would otherwise block a direct resolve.
  binary = path.join(path.dirname(require.resolve(`${pkg}/package.json`)), 'bin', exe)
} catch {
  console.error(`ketch: the ${pkg} package is not installed.`)
  console.error('Optional dependencies were probably skipped during install.')
  console.error('Reinstall with: npm install ketch-cli --include=optional')
  process.exit(1)
}

const child = spawn(binary, process.argv.slice(2), {
  stdio: 'inherit',
  windowsHide: true,
})

// Relay termination so `ketch mcp serve` goes down with the client that started it.
for (const signal of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
  process.on(signal, () => {
    if (!child.killed) child.kill(signal)
  })
}

child.on('error', (err) => {
  console.error(`ketch: could not run ${binary}: ${err.message}`)
  process.exit(1)
})

child.on('exit', (code, signal) => {
  process.exit(code !== null ? code : signal ? 1 : 0)
})
