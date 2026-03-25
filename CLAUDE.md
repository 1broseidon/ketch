## Ketch

Fast, stateless CLI for agentic web search and scrape. Single Go binary, no daemon.

### Architecture

See [AGENTS.md](AGENTS.md) for full module layout and design principles.

- `cmd/` — Cobra CLI (root, search, scrape, config, cache)
- `internal/search/` — `Searcher` interface with Brave (default), DDG, and SearXNG backends
- `internal/scrape/` — HTTP fetch chain, returns `Page{URL, Title, Markdown}`
- `internal/extract/` — readability + html-to-markdown pipeline
- `internal/config/` — JSON config at `~/.config/ketch/config.json`
- `internal/cache/` — TTL-based page cache at platform cache dir

### Build & Test

```bash
make build          # builds ./ketch
make lint           # golangci-lint (gocyclo max 15)
make test           # go test ./...
```

Pre-commit hook (`.githooks/pre-commit`) runs gofmt, vet, lint, and tests. Git is configured to use `.githooks/` as hooks path.

### Output Format

Default output uses YAML frontmatter + markdown (cymbal style):
- `ketch scrape` — frontmatter (url, title, words) + markdown body
- `ketch search` — frontmatter (query, backend, result_count) + result list
- `--json` flag available on all commands for structured JSON output

### Search Backends

| Backend | Setup | Notes |
|---------|-------|-------|
| `brave` (default) | Free API key from brave.com/search/api | Stable JSON API |
| `ddg` | Zero config | Rate-limited by DDG currently |
| `searxng` | Self-hosted instance | Most reliable for heavy use |

### Configuration

`~/.config/ketch/config.json` — JSON config, `encoding/json` from stdlib (no external config libs).

```bash
ketch config              # discovery payload (JSON)
ketch config init         # create default config
ketch config set key val  # update a value
```

### Page Cache

Scraped pages cached at platform cache dir (`os.UserCacheDir()`), keyed by `sha256(url)[:16]`.

```bash
ketch cache               # stats
ketch cache clear         # wipe
ketch scrape --no-cache   # bypass
```

Default TTL: 1h. Configure via `ketch config set cache_ttl 4h`.

### Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/PuerkitoBio/goquery` — HTML parsing (DDG scraping)
- `github.com/JohannesKaufmann/html-to-markdown/v2` — HTML→markdown
- `codeberg.org/readeck/go-readability/v2` — Mozilla readability content extraction
- CGO_ENABLED=0, pure Go, cross-compiles everywhere

### Release

GoReleaser + GitHub Actions (`.goreleaser.yaml`, `.github/workflows/release.yml`). Publishes to `1broseidon/homebrew-tap`.

### What's Next

1. Unit tests for extract, search, and cache packages
2. `--raw` flag implementation in scrape command
3. Browser fallback via `rod` behind build tag for JS-heavy pages
