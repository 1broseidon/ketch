# ketch-cli

Fast, stateless CLI for web search, OSS code search, library docs, scraping, and
crawling — with an MCP server for agents.

This is the npm distribution of [ketch](https://github.com/1broseidon/ketch).
The `ketch` name on npm was already taken, so the package is `ketch-cli`; the
installed binary is still `ketch`.

Full documentation: **[ketch.run](https://ketch.run)**

## Use it without installing

```sh
npx -y ketch-cli search "golang error handling"
npx -y ketch-cli scrape https://go.dev/doc/effective_go
```

## As an MCP server

`ketch mcp serve` exposes five tools — `search`, `code`, `docs`, `scrape`, and
`crawl` — over stdio. To register it with Claude Code:

```sh
claude mcp add ketch -- npx -y ketch-cli mcp serve
```

Or in an MCP client's config file:

```json
{
  "mcpServers": {
    "ketch": {
      "command": "npx",
      "args": ["-y", "ketch-cli", "mcp", "serve"]
    }
  }
}
```

The binary ships in a per-platform package selected through
`optionalDependencies`, so nothing is downloaded during install and no
postinstall script runs. That keeps startup fast when a client relaunches the
server, and it works in environments that set `--ignore-scripts`.

## Install it properly

```sh
npm install -g ketch-cli   # then: ketch search "query"
```

Other install routes — Homebrew, `go install`, or a checksum-verified shell
installer — are covered at [ketch.run](https://ketch.run/#install).

## Supported platforms

macOS, Linux, and Windows on x64 and arm64. On anything else, build from source:

```sh
go install github.com/1broseidon/ketch@latest
```

## License

MIT
