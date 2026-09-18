// @ts-ignore — VitePress supports async config at runtime
import { execSync } from 'node:child_process'

const REPO = '1broseidon/ketch'

// The release tag comes from git, not the API. The docs checkout uses
// fetch-depth: 0, so every tag is already local — deterministic, offline, and
// immune to the unauthenticated 60/hr GitHub rate limit that a shared CI
// runner can exhaust. The API is only a fallback for a shallow checkout.
function versionFromGit(): string | null {
  try {
    const out = execSync('git describe --tags --abbrev=0', {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    })
    return out.trim() || null
  } catch {
    return null
  }
}

async function gh<T>(path: string): Promise<T | null> {
  try {
    const headers: Record<string, string> = { accept: 'application/vnd.github+json' }
    // Authenticated in CI (60/hr -> 1000/hr); anonymous locally.
    if (process.env.GITHUB_TOKEN) headers.authorization = `Bearer ${process.env.GITHUB_TOKEN}`
    const res = await fetch(`https://api.github.com/${path}`, { headers })
    if (!res.ok) return null
    return (await res.json()) as T
  } catch {
    return null
  }
}

export default (async () => {
  const version =
    process.env.KETCH_VERSION?.trim() ||
    versionFromGit() ||
    (await gh<{ tag_name: string }>(`repos/${REPO}/releases/latest`))?.tag_name ||
    null

  // Rendered at build time so the count is in the first paint and in the HTML
  // for crawlers; the page refreshes it client-side on mount.
  const stars = (await gh<{ stargazers_count: number }>(`repos/${REPO}`))?.stargazers_count ?? null

  return {
    title: 'ketch',
    description: 'Fast web search and scrape for agents',
    base: '/',
    appearance: false,
    cleanUrls: true,
    head: [
      ['link', { rel: 'preconnect', href: 'https://fonts.googleapis.com' }],
      ['link', { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' }],
      ['link', { href: 'https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600;700&family=IBM+Plex+Serif:ital,wght@0,400;1,400&display=swap', rel: 'stylesheet' }],
    ],
    themeConfig: {
      version,
      stars,
      repo: REPO,
      nav: [
        { text: 'Manual', link: '/' },
        { text: 'Changelog', link: '/changelog' },
      ],
      sidebar: false,
      socialLinks: [
        { icon: 'github', link: `https://github.com/${REPO}` },
      ],
      outline: { level: [2, 3] },
    },
  }
})
