/* Everything that differs between ketch.run, cymbal.sh and brainfile.md. */
export default {
  name: 'ketch',
  url: 'https://ketch.run',
  repo: '1broseidon/ketch',
  tagline: 'web search, code search, docs and scraping for agents',
  built: 'Built in Go, one static binary',
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
