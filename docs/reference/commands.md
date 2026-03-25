# Commands

## ketch search

Search the web and return results.

```sh
ketch search <query> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--limit, -l` | `5` | Max number of results |
| `--scrape` | `false` | Fetch full content from each result |
| `--searxng-url` | `http://localhost:8081` | SearXNG instance URL |

**Global flags** (`--json`, `--backend`) also apply.

**Examples:**

```sh
ketch search "golang error handling"
ketch search "rust async" --limit 10
ketch search "python web scraping" --scrape
ketch search "query" --backend searxng
ketch search "query" --json
```

## ketch scrape

Fetch URLs and extract clean markdown.

```sh
ketch scrape <url> [urls...] [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--raw` | `false` | Output raw HTML instead of markdown |
| `--no-cache` | `false` | Bypass the page cache |

**Examples:**

```sh
ketch scrape https://go.dev/doc/effective_go
ketch scrape https://example.com https://go.dev
ketch scrape https://example.com --json
ketch scrape https://example.com --no-cache
```

Multiple URLs are scraped concurrently.

## ketch config

Show or manage configuration.

```sh
ketch config              # show effective config as JSON
ketch config init         # create default config file
ketch config set <k> <v>  # set a config value
ketch config path         # print config file path
```

## ketch cache

Show or manage the page cache.

```sh
ketch cache               # show cache stats
ketch cache clear         # remove all cached pages
```

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output as JSON instead of YAML frontmatter + markdown |
| `--backend, -b` | `brave` | Search backend: `brave`, `ddg`, `searxng` |
