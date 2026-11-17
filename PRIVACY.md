# Privacy Policy

_Last updated: 17 November 2026_

ketch is a command-line tool that runs entirely on your own machine. The ketch
project runs no servers, offers no accounts, and collects no data about you or
how you use ketch. There is no telemetry, analytics, or crash reporting.

## What leaves your machine

ketch makes network requests only to do what you ask, and it sends them
directly from your machine to third-party services. Nothing passes through
ketch's maintainers.

- **Web search** (`ketch search`): your query goes to the search provider you
  configured. With no provider set, ketch uses its keyless default (DuckDuckGo).
- **Code search** (`ketch code`): your query goes to grep.app by default, or to
  Sourcegraph or GitHub if you choose them.
- **Docs** (`ketch docs`): library names and queries go to Context7 by default.
  To find documentation, ketch may also contact package registries (npm, PyPI,
  crates.io, the Go module proxy, pkg.go.dev), GitHub, and the library's own
  documentation site.
- **Scrape and crawl** (`ketch scrape`, `ketch crawl`): ketch fetches the URLs
  you give it, like a browser would. If you configure a cookies file
  (`cookie_file`) or custom headers (`http_headers`), they are sent only to
  the matching sites.
- **Update check:** about once a day, an interactive ketch run asks the GitHub
  API for the latest release number (`ketch version` checks too). No usage
  data is sent. To turn this off,
  set `KETCH_NO_UPDATE_NOTIFIER=1`. It is also off when `CI` is set.
- **Browser download** (`ketch browser install`): runs only when you ask, and
  downloads Chromium into ketch's cache directory.

Each of these services handles your request under its own privacy policy. ketch
does not control what they log. If you supply an API key for a provider, ketch
sends it only to that provider.

## What stays on your machine

Your configuration (including any API keys and tokens), the response cache,
crawl status, and tags are stored in your user's config and cache directories.
They never leave your machine except as the requests described above.
`ketch cache clear` empties the page cache, and removing those directories
deletes everything.

## MCP and agent use

`ketch mcp serve` runs locally over stdio. It makes the same requests as the
CLI, and sends nothing anywhere else.

## Contact

Questions about this policy: open an issue at
https://github.com/1broseidon/ketch/issues.
