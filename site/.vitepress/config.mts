// @ts-ignore — VitePress supports async config at runtime

async function getLatestVersion(repo: string): Promise<string | null> {
  try {
    const res = await fetch(`https://api.github.com/repos/1broseidon/${repo}/releases/latest`)
    if (!res.ok) return null
    const data = await res.json() as { tag_name: string }
    return data.tag_name ?? null
  } catch {
    return null
  }
}

export default (async () => {
  const version = await getLatestVersion('ketch')

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
      nav: [
        { text: 'Manual', link: '/' },
        { text: 'Changelog', link: '/changelog' },
      ],
      sidebar: false,
      socialLinks: [
        { icon: 'github', link: 'https://github.com/1broseidon/ketch' },
      ],
      outline: { level: [2, 3] },
    },
  }
})
