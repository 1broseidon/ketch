/* Everything inkcap needs to print this site. The rest of the build is
 * github.com/1broseidon/inkcap, shared with the other chain.sh manuals. */
export default {
  name: 'ketch',
  url: 'https://ketch.run',
  repo: '1broseidon/ketch',
  tagline: 'web search, code search, docs and scraping for agents',
  built: 'Built in Go, one static binary',
  accent: {
    light: { accent: '#0B6A72', soft: '#DFEDEE' },
    dark: { accent: '#5FBAC2', soft: '#14282B' },
    terminal: { prompt: '#4FA3AB', key: '#8FC9CE' },
  },
  changelog: '../CHANGELOG.md',
  /* Section ids follow the headings now; the ids the page had before still
   * land on the right section. */
  legacyAnchors: {
    '#what': '#overview',
    '#surfaces': '#choosing-a-command',
    '#playbook': '#research-playbook',
    '#config': '#configuration',
    '#exit': '#exit-status',
    '#gotchas': '#notes',
    '#agents': '#for-agents',
  },
}
