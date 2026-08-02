# Configuration

ketch reads defaults from `~/.config/ketch/config.json`. Flags always override config values.

## Setup

```sh
# Create a default config file
ketch config init

# View effective config + available backends
ketch config
```

The discovery payload:

```json
{
  "config_path": "/home/user/.config/ketch/config.json",
  "backend": "brave",
  "searxng_url": "http://localhost:8081",
  "brave_api_key_set": false,
  "brave_api_keys_count": 0,
  "exa_api_key_set": false,
  "exa_api_keys_count": 0,
  "firecrawl_api_key_set": false,
  "firecrawl_api_keys_count": 0,
  "firecrawl_url": "https://api.firecrawl.dev",
  "keenable_api_key_set": false,
  "keenable_api_keys_count": 0,
  "tavily_api_key_set": false,
  "tavily_api_keys_count": 0,
  "limit": 5,
  "cache_ttl": "72h",
  "code_backend": "grepapp",
  "docs_backend": "context7",
  "context7_api_key_set": false,
  "sourcegraph_url": "https://sourcegraph.com",
  "github_token_source": "none",
  "github_token_set": false,
  "external_pdf_to_md_converter_timeout_sec": 300,
  "available_backends": ["brave", "ddg", "searxng", "exa", "firecrawl", "keenable", "tavily", "parallel"],
  "available_code_backends": ["grepapp", "sourcegraph", "github"],
  "available_doc_backends": ["context7"]
}
```

The `*_set` booleans report key presence only — key values are never printed.
The `*_keys_count` fields report the size of each provider's effective
(de-duplicated) key pool. When any `KETCH_*` environment variable overrides a
config value, an `env_overrides` array appears listing, per key, the variable
applied and the value it replaced (redacted for secrets) — see
[Environment Variables](#environment-variables).
`github_token_source` reports where the
GitHub token was resolved from (`config`, `env`, `gh-cli`, or `none`), and
`github_token_set` is true whenever that chain resolved a token.
`browser`, `cookie_file`, `url_rewrites`, `spa_markers`, and `external_pdf_to_md_converter_command` appear only when configured. `cookie_file` reports only the jar path, never cookie names or values. The external PDF converter timeout is always reported.

## Setting Values

```sh
ketch config set backend searxng
ketch config set brave_api_key BSA...
ketch config set brave_api_keys '["BSA-key-1","BSA-key-2"]'   # multi-key pool (JSON array)
ketch config set searxng_url http://my-searxng:8080
ketch config set exa_api_key exa...
ketch config set firecrawl_api_key fc-...
ketch config set firecrawl_url http://localhost:3002
ketch config set keenable_api_key keen_...
ketch config set tavily_api_key tvly-...
ketch config set limit 10
ketch config set cache_ttl 4h
ketch config set browser chrome
ketch config set cookie_file ~/cookies.txt
ketch config set external_pdf_to_md_converter_command 'pdftotext "{input}" -'
ketch config set external_pdf_to_md_converter_timeout_sec 300
ketch config set code_backend sourcegraph
ketch config set docs_backend context7
ketch config set context7_api_key ctx7...
ketch config set github_token ghp_...
```

## Config Keys

### Web Search

| Key | Default | Description |
|-----|---------|-------------|
| `backend` | `brave` | Default search backend: `brave`, `ddg`, `searxng`, `exa`, `firecrawl`, `keenable`, `tavily`, `parallel` |
| `brave_api_key` | — | Brave Search API key ([get one free](https://brave.com/search/api/)) |
| `brave_api_keys` | — | Additional Brave keys (JSON array) — see [multiple keys](#multiple-api-keys-per-provider) |
| `exa_api_key` | — | Optional Exa API key for authenticated hosted MCP usage |
| `exa_api_keys` | — | Additional Exa keys (JSON array) |
| `firecrawl_api_key` | — | [Firecrawl](https://docs.firecrawl.dev) API key (required for hosted `-b firecrawl`; optional for self-hosted) |
| `firecrawl_api_keys` | — | Additional Firecrawl keys (JSON array) |
| `firecrawl_url` | `https://api.firecrawl.dev` | Firecrawl API base URL (self-hosted override; ketch appends `/v2/search`, and strips it if you paste the full endpoint) |
| `keenable_api_key` | — | Optional Keenable API key; keyless by default, a key lifts the rate limit ([console](https://keenable.ai/console)) |
| `keenable_api_keys` | — | Additional Keenable keys (JSON array) |
| `tavily_api_key` | — | [Tavily](https://app.tavily.com) API key (required for `-b tavily`) |
| `tavily_api_keys` | — | Additional Tavily keys (JSON array) |
| `searxng_url` | `http://localhost:8081` | SearXNG instance URL |
| `limit` | `5` | Default max results (shared by `search`, `code`, `docs`) |

#### Multiple API keys per provider

Each keyed search provider takes an optional plural key pool alongside its
singular key: `brave_api_keys`, `exa_api_keys`, `firecrawl_api_keys`,
`keenable_api_keys`, `tavily_api_keys`. The effective pool is the singular key plus the list,
trimmed and de-duplicated. Per request, ketch picks one key from the pool at
random to spread rate limits; when the pool holds more than one key, a
`401`/`429` response (`402` for Firecrawl) gets one retry with a different key.

```sh
ketch config set brave_api_keys '["BSA-key-1","BSA-key-2"]'
```

`config set` takes a JSON array of strings (`[]` clears the pool). `ketch
config` reports pool sizes as `*_api_keys_count`; key values are never printed,
and `ketch doctor` probes the effective pool.

### Code & Docs Search

| Key | Default | Description |
|-----|---------|-------------|
| `code_backend` | `grepapp` | Default `ketch code` backend: `grepapp`, `sourcegraph`, `github` |
| `docs_backend` | `context7` | Default `ketch docs` backend: `context7`, `local` |
| `sourcegraph_url` | `https://sourcegraph.com` | Sourcegraph instance URL (for self-hosted) |
| `context7_api_key` | — | Context7 API key (required for `ketch docs`) |
| `github_token` | — | GitHub token for `ketch code -b github` (or use `$GITHUB_TOKEN` / `gh auth`) |

### Scraping & Cache

| Key | Default | Description |
|-----|---------|-------------|
| `cache_ttl` | `72h` | How long scraped pages stay cached (Go duration, e.g. `30m`, `4h`) |
| `browser` | — | Browser for JS-rendered pages: `chrome`, `chromium`, or absolute path |
| `url_rewrites` | — | Ordered regex rewrite rules applied before every fetch (see below) |
| `spa_markers` | — | Extra JS-shell detection substrings (JSON array); pages containing one are treated as JS-rendered and re-fetched via the browser |
| `cookie_file` | — | Path to a Netscape `cookies.txt` jar used by `scrape`, `search --scrape`, `crawl`, and the MCP server |
| `external_pdf_to_md_converter_command` | — | Optional shlex-parsed PDF-to-Markdown command; must contain exactly one `{input}` placeholder and write Markdown to stdout (capped at 10 MiB) |
| `external_pdf_to_md_converter_timeout_sec` | `300` | Positive timeout in seconds for the external PDF converter |

When no external converter is configured, ketch uses its built-in pure-Go PDF text extractor. A PDF without a text layer returns a precondition error (exit 5 / `[precondition]`) with a hint to configure an OCR-capable converter. `ketch config set` validates the converter command before saving it. Once configured, the converter is authoritative: command failures, timeouts, empty output, and output over 10 MiB are returned without falling back to the built-in extractor. The command is executed directly, not through a shell, and the temporary `.pdf` input is removed after conversion.

PDFs do not support `--raw` or `--select`; these return validation errors (exit 2 / `[validation]`). With `--force-browser`, normal markdown output still uses PDF text extraction and never opens Chromium's PDF viewer.

Secrets (the per-provider `*_api_key`/`*_api_keys` values, `context7_api_key`, `github_token`) are stored in
plaintext in `config.json`; ketch writes the file with mode `0600`, protect it accordingly.

## Environment Variables

Every config key except three file-only cases (below) can also be set through
the environment — handy for containers and CI where writing a config file is
awkward. The variable name is mechanical: `KETCH_` + the upper-snake config
key. `limit` → `KETCH_LIMIT`, `brave_api_key` → `KETCH_BRAVE_API_KEY`,
`external_pdf_to_md_converter_timeout_sec` →
`KETCH_EXTERNAL_PDF_TO_MD_CONVERTER_TIMEOUT_SEC`.

```sh
KETCH_BRAVE_API_KEY=<key> ketch search "query"      # inject a key without touching disk
KETCH_BACKEND=ddg KETCH_LIMIT=10 ketch search "query"
KETCH_CONFIG=/etc/ketch/config.json ketch config    # point at an alternate config file
```

Precedence is **CLI flag > `KETCH_*` env > config file > built-in default**.
An env var set to the empty string is treated as unset.

**File-only keys.** Three kinds of keys deliberately have no env override:

- `url_rewrites` — regex JSON doesn't survive shell quoting.
- `spa_markers` — arbitrary HTML substrings have no safe delimiter.
- The plural `*_api_keys` pools — instead, the **singular** per-provider var
  accepts a comma-separated list: `KETCH_BRAVE_API_KEY=key1,key2` replaces the
  provider's whole effective key pool. There are no `KETCH_*_API_KEYS` plural
  vars.

**Special cases:**

- `KETCH_CONFIG=<path>` redirects every config read *and* write (`config set`,
  `config path`) to an alternate file.
- `KETCH_GITHUB_TOKEN` sits at the top of the GitHub token chain:
  `KETCH_GITHUB_TOKEN` > config file `github_token` > ambient
  `$GITHUB_TOKEN`/`$GH_TOKEN` > `gh auth token`.

**Behavior:**

- `ketch config` reports an `env_overrides` section listing each overridden
  key, the variable applied, and the value it replaced (redacted for secrets),
  so you can always see why the effective config differs from the file.
- `ketch config set` reads and writes the file only — env-derived values are
  never persisted into `config.json`.
- Invalid env values (e.g. `KETCH_LIMIT=abc`) fail loudly, naming the offending
  variable — but only on commands that consume config; `ketch version`,
  `help`, `completion`, and `config init/set/path` keep working under a broken
  environment.
- Secret `KETCH_*` vars (API keys, `KETCH_GITHUB_TOKEN`) are stripped from the
  environment of spawned subprocesses (the headless browser and the external
  PDF converter), so injected credentials don't leak.

## Cookies

Some pages only show their real content once a session or consent cookie is
present — a logged-in dashboard, docs behind SSO, or a consent-banner wall that
serves nothing but the banner to an anonymous client. ketch can attach your own
cookies to every fetch, on both the plain-HTTP and headless-browser paths.

Supply a jar in the **Netscape `cookies.txt` format** — the same format that
browser "cookies.txt export" extensions produce and that `curl` and `yt-dlp`
consume. A typical workflow for a login-gated page:

```sh
# 1. Log in to the site in your browser, then export its cookies with a
#    cookies.txt extension (Netscape format) to ~/cookies.txt.
chmod 600 ~/cookies.txt

# 2. Scrape the gated page with the jar attached
ketch scrape https://internal.example.com/dashboard --cookie-file ~/cookies.txt

# 3. Or persist it so scrape / search --scrape / crawl / MCP all use it
ketch config set cookie_file ~/cookies.txt

# Override the configured jar for one command
ketch scrape https://example.com/private --cookie-file /path/to/other-cookies.txt

# Explicitly disable configured cookies for one command
ketch crawl https://example.com --cookie-file ""
```

The `--cookie-file` flag is available on `scrape`, `search` (for `--scrape` fetches), and `crawl`; it overrides `cookie_file`. The configured jar also applies to MCP scrape and crawl calls. ketch rejects cookies with malformed domain, include-subdomain, path, or Secure fields. For every request and redirect, it re-matches Domain, HostOnly, Path, and Secure scope; Secure cookies are not sent on HTTPS-to-HTTP redirects. Matching cookies are also used for `/llms.txt`, sitemap, and nested sitemap-index fetches.

Cookie values are never printed — not in frontmatter, `--json`, errors, or `ketch doctor` (which reports only `configured (N cookies, M expired)`). Keep the jar private (`chmod 600`); ketch warns when it is group/world-readable on POSIX systems.

Respecting a site's Terms of Service and using only your own session cookies is the operator's responsibility.

## URL Rewrites

`url_rewrites` is an ordered list of `{match, replace}` regex rules applied
transparently before any fetch in `scrape`, `search --scrape`, and `crawl`.
Use it to redirect URLs without touching the agent surface — for example,
routing Reddit links to the old UI:

```sh
ketch config set url_rewrites '[{"match":"www.reddit.com","replace":"old.reddit.com"}]'
```

The original URL is preserved in output as `url:`; the fetched URL appears as
`fetched_url:` when it differs. Rules are applied in order.

## Browser Rendering

JS-rendered pages are automatically detected and re-fetched via headless Chrome. Configure once, then scrape and crawl commands use it transparently.

```sh
# Use Chrome from PATH
ketch config set browser chrome

# Or use an absolute path
ketch config set browser /usr/bin/google-chrome-stable

# Download Chromium to ketch's cache dir
ketch browser install

# Check browser config and availability
ketch browser status
```

If `ketch browser install` fails, just rerun it — each attempt clears the download
directory first. When the download can't succeed at all (restricted network, no
disk space), configure an already-installed Chrome with
`ketch config set browser <path>` and verify with `ketch browser status`.

When a browser is configured, ketch automatically detects JS-rendered pages (React SPAs, Angular apps, Salesforce Lightning, etc.) and falls back to headless rendering. Static pages are always fetched via plain HTTP for speed.

## Page Cache

Scraped and crawled pages are cached in a single bbolt database at the platform cache directory. On POSIX systems, ketch creates the cache directory with mode `0700` and the database with mode `0600`, tightening an older database when it is opened.

| OS | Path |
|----|------|
| Linux | `~/.cache/ketch/cache.db` |
| macOS | `~/Library/Caches/ketch/cache.db` |
| Windows | `%LocalAppData%/ketch/cache.db` |

```sh
# View cache stats
ketch cache

# Clear all cached pages
ketch cache clear

# Bypass cache for a single scrape
ketch scrape https://example.com --no-cache

# Bypass cache for a crawl (force re-fetch everything)
ketch crawl https://example.com --no-cache
```

When the configured jar has live cookies, ketch uses a jar-specific cache namespace even if only a redirect target matches cookie scope. Because the cache can contain private authenticated content, treat the entire cache directory as sensitive; use `--no-cache` when authenticated content must not be stored.
