/* Refreshes src/stars.json from the GitHub API at build time.
 * Never fails the build: on any error the committed count stands. */

import { readFile, writeFile } from 'node:fs/promises'
import SITE from '../site.config.mjs'

const OUT = new URL('../src/stars.json', import.meta.url)

try {
  const res = await fetch(`https://api.github.com/repos/${SITE.repo}`, {
    headers: {
      accept: 'application/vnd.github+json',
      ...(process.env.GITHUB_TOKEN
        ? { authorization: `Bearer ${process.env.GITHUB_TOKEN}` }
        : {}),
    },
    signal: AbortSignal.timeout(10_000),
  })
  if (!res.ok) throw new Error(`GitHub API ${res.status}`)

  const repo = await res.json()
  if (typeof repo.stargazers_count !== 'number') throw new Error('no star count')

  const current = JSON.parse(await readFile(OUT, 'utf8').catch(() => '{}'))
  await writeFile(OUT, `${JSON.stringify({ ...current, [SITE.name]: repo.stargazers_count }, null, 2)}\n`)
  console.log(`stars: refreshed ${SITE.name}=${repo.stargazers_count}`)
} catch (err) {
  console.warn(`stars: keeping committed count (${err.message})`)
}
