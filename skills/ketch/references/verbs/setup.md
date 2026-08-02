# ketch setup

Setup is a configuration assistant, not a configuration script. It reads the current state, probes what actually works, proposes exact commands, and mutates **only after the operator confirms**. It never touches a value that is already configured and working.

---

## The state problem — be honest about it

Prefer `ketch doctor --json` when available (check that `ketch doctor --help` exits 0): it probes every configured surface in one call, detects known traps (e.g. a searxng instance that rejects `format=json`), and exits 0 when everything configured is healthy, 5 when a configured thing is broken. Newer builds' `ketch config` also reports key presence as booleans (`brave_api_key_set`, …).

On older ketch versions without `doctor`, configured-state detection is imperfect: `ketch config` shows effective settings and available backends, but does **not** report whether search/docs API keys are set (the one exception: `github_token_source`), and it never reports reachability. Read the config, then send one probe per surface the operator cares about, and classify the result by error class.

---

## Flow

1. **Read** (read-only): `ketch config` and `ketch browser status`. Note the effective backend per surface.
2. **Probe** each surface in play — `ketch doctor --json` when available; on older versions, one cheap read-only call each:

   | Surface | Probe | Healthy | Needs setup | Unreachable |
   | --- | --- | --- | --- | --- |
   | search | `ketch search "test" -l 1` | exit 0 | exit 5 `[precondition]` | exit 4 `[upstream]` |
   | code | `ketch code "test" -l 1` | exit 0 | exit 5 | exit 4 |
   | docs | `ketch docs --resolve "react" -l 1` | exit 0 | exit 5 | exit 4 |
   | scrape | `ketch scrape https://example.com --max-chars 200` | exit 0 | — | exit 4 |
   | browser | `ketch browser status` | `status: ok` | not configured | — |

   Exit 4 means reachability (instance down, network, rate limit), not configuration — investigate before proposing any `config set`.
3. **Propose.** State per surface: current state, what you recommend changing, what you will *not* touch because its probe is healthy, and the exact commands. Then wait. No `config set`, `browser install`, or `docker run` before an explicit yes.
4. **Apply** exactly the confirmed commands, nothing extra.
5. **Verify.** Re-run the probe for each changed surface; report exit code / error prefix.

---

## Search backend decision tree (present in this order)

1. **ddg — the zero-setup default.** No key, works immediately. Rate-limits readily under research fan-out (`[upstream]` / exit 4). Right for: trying ketch out, light use.
2. **brave — best quality per unit effort. Recommend for regular use.** Free API key.
   ```sh
   ketch config set backend brave
   ketch config set brave_api_key <key>
   ```
   The missing-key error itself contains the signup URL and the exact fix command — surface it to the operator rather than paraphrasing.
3. **searxng — the self-hosted option.** No key, no third-party rate limits. **Stock searxng in docker rejects `format=json`**, which ketch requires — mount a `settings.yml` with the json format enabled:

   ```yaml
   # settings.yml — minimal for ketch
   use_default_settings: true
   search:
     formats:
       - html
       - json
   ```

   ```sh
   docker run -d --name searxng \
     -p 8081:8080 \
     -v "$(pwd)/settings.yml:/etc/searxng/settings.yml:ro" \
     searxng/searxng
   ketch config set backend searxng
   ketch config set searxng_url http://localhost:8081
   ketch search "test" -l 1    # verify: exit 0
   ```

   Right for: self-hosting preference, heavy volume, privacy.
4. **exa — hosted alternative.** Works with zero config; `exa_api_key` exists for keyed use.
5. **firecrawl — same provider as scrape/crawl.** Hosted needs a key: `ketch config set firecrawl_api_key <key>` (get one at firecrawl.dev), then `ketch config set backend firecrawl`. Self-hosted: `ketch config set firecrawl_url http://localhost:3002` (key optional if your instance skips auth).
6. **keenable — keyless by default.** Works with no key against the public endpoint (rate-limited); an optional `ketch config set keenable_api_key <key>` lifts the cap. Select with `ketch config set backend keenable`.
7. **tavily — agent search with extracted content.** Needs a key: `ketch config set tavily_api_key <key>` (get one free at app.tavily.com), then `ketch config set backend tavily`. Results include richer extracted text in `content` (basic depth by default).

---

## Code, docs, browser

- **code:** `grepapp` is the default and keyless — a healthy probe means leave it alone. `sourcegraph` is keyless too (`sourcegraph_url` can point at another instance). `github` needs auth by any of: `gh auth login`, `$GITHUB_TOKEN`, or `ketch config set github_token <tok>`.
- **docs:** `context7` needs a free key: `ketch config set context7_api_key <key>`. The `local` backend is planned but unimplemented — selecting it is `[precondition]` / exit 5, not a bug in your call.
- **browser** (JS-rendered pages): check `ketch browser status`. Either point at an installed binary — `ketch config set browser <binary>` — or download Chromium with `ketch browser install`. Both are mutations: propose, confirm.

---

## Rules

- Propose-then-confirm on every mutation; the proposal quotes the exact command.
- Never modify a surface whose probe is healthy.
- Exit 4 from a probe is a reachability problem — do not "fix" it with `config set`.
- Report the end state as a table: `surface | backend | probe result`.
- `ketch doctor --json` replaces the hand-rolled probes when available (`ketch doctor --help` exits 0); the propose→confirm→verify loop stays either way.
