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
    base: '/ketch/',
    appearance: false,
    cleanUrls: true,
    head: [
      ['link', { rel: 'preconnect', href: 'https://fonts.googleapis.com' }],
      ['link', { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' }],
      ['link', { href: 'https://fonts.googleapis.com/css2?family=Work+Sans:wght@300;400;700&family=JetBrains+Mono:wght@400;500&display=swap', rel: 'stylesheet' }],
    ],
    themeConfig: {
      version,
      nav: [
        { text: 'Guide', link: '/guide/getting-started' },
        { text: 'Reference', link: '/reference/commands' },
        { text: 'Changelog', link: '/changelog' },
      ],
      sidebar: [
        {
          text: 'Guide',
          items: [
            { text: 'Getting Started', link: '/guide/getting-started' },
            { text: 'Configuration', link: '/guide/configuration' },
            { text: 'Agent Integration', link: '/guide/agent-integration' },
          ],
        },
        {
          text: 'Reference',
          items: [
            { text: 'Commands', link: '/reference/commands' },
            { text: 'Backends', link: '/reference/backends' },
          ],
        },
        {
          text: 'Changelog',
          link: '/changelog',
        },
      ],
      socialLinks: [
        { icon: 'github', link: 'https://github.com/1broseidon/ketch' },
      ],
      outline: { level: [2, 3] },
    },
  }
})
