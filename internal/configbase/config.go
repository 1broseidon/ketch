// Package configbase holds configuration values without importing provider implementations.
package configbase

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/urlrewrite"
)

// Config holds user-configurable defaults for ketch.
type Config struct {
	Backend                            string            `json:"backend" order:"0"`
	Limit                              int               `json:"limit" order:"15"`
	CacheTTL                           string            `json:"cache_ttl" order:"16"`
	Browser                            string            `json:"browser,omitempty" order:"17"` // "chrome", "chromium", or absolute path; empty = disabled
	CodeBackend                        string            `json:"code_backend,omitempty" order:"18"`
	DocsBackend                        string            `json:"docs_backend,omitempty" order:"19"`
	URLRewrites                        []urlrewrite.Rule `json:"url_rewrites,omitempty" order:"23"`
	SPAMarkers                         []string          `json:"spa_markers,omitempty" order:"24"`
	MCPTools                           []string          `json:"mcp_tools,omitempty" order:"25"`   // allowlist of tools `ketch mcp serve` publishes; empty = all five
	CookieFile                         string            `json:"cookie_file,omitempty" order:"26"` // Netscape cookies.txt path; empty = disabled
	UserAgent                          string            `json:"user_agent,omitempty" order:"27"`  // HTTP User-Agent override; empty = built-in honest default
	ExternalPDFToMDConverterCommand    string            `json:"external_pdf_to_md_converter_command,omitempty" order:"28"`
	ExternalPDFToMDConverterTimeoutSec int               `json:"external_pdf_to_md_converter_timeout_sec" order:"29"`
	ProviderSettings                   map[string]any    `json:"-"`
	providerSchema                     []Setting
	providerOrder                      map[string]int
}

// mergeKeys builds an effective key pool with the legacy singular key first.
// It trims whitespace, drops blank entries, de-duplicates while preserving
// order, and returns a fresh slice.
func MergeKeys(single string, list []string) []string {
	merged := make([]string, 0, len(list)+1)
	seen := make(map[string]struct{}, len(list)+1)
	for _, key := range append([]string{single}, list...) {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, key)
	}
	return merged
}

// BraveKeys returns an immutable copy of the effective Brave API key pool.
func (c Config) BraveKeys() []string {
	return MergeKeys(c.String("brave_api_key"), c.Strings("brave_api_keys"))
}

// ExaKeys returns an immutable copy of the effective Exa API key pool.
func (c Config) ExaKeys() []string {
	return MergeKeys(c.String("exa_api_key"), c.Strings("exa_api_keys"))
}

// FirecrawlKeys returns an immutable copy of the effective Firecrawl API key pool.
func (c Config) FirecrawlKeys() []string {
	return MergeKeys(c.String("firecrawl_api_key"), c.Strings("firecrawl_api_keys"))
}

// KeenableKeys returns an immutable copy of the effective Keenable API key pool.
func (c Config) KeenableKeys() []string {
	return MergeKeys(c.String("keenable_api_key"), c.Strings("keenable_api_keys"))
}

// TavilyKeys returns an immutable copy of the effective Tavily API key pool.
func (c Config) TavilyKeys() []string {
	return MergeKeys(c.String("tavily_api_key"), c.Strings("tavily_api_keys"))
}

// SerpBaseKeys returns an immutable copy of the effective SerpBase API key pool.
func (c Config) SerpBaseKeys() []string {
	return MergeKeys(c.String("serpbase_api_key"), c.Strings("serpbase_api_keys"))
}

// ResolveGithubToken returns a token and the source it came from, walking the
// resolution chain: $KETCH_GITHUB_TOKEN → explicit config → $GITHUB_TOKEN →
// $GH_TOKEN → `gh auth token`. github_token is deliberately excluded from the
// generic env overlay so this chain stays the single owner of its precedence.
// Source is one of: "config", "env", "gh-cli", "none". The token is never logged.
func (c Config) ResolveGithubToken() (token, source string) {
	if t := os.Getenv("KETCH_GITHUB_TOKEN"); t != "" {
		return t, "env"
	}
	if c.String("github_token") != "" {
		return c.String("github_token"), "config"
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t, "env"
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t, "env"
	}
	if t := ghCLIToken(); t != "" {
		return t, "gh-cli"
	}
	return "", "none"
}

// ghCLITokenTTL bounds how long one `gh auth token` answer is reused. A single
// CLI invocation resolves the token more than once (eligibility, construction,
// discovery, doctor) and must see one consistent answer without paying for a
// subprocess each time; a long-lived MCP server still notices a re-login
// within this window.
const ghCLITokenTTL = 30 * time.Second

var ghCLI struct {
	sync.Mutex
	token   string
	checked time.Time
	run     func() (string, error) // swapped by tests
}

func runGHAuthToken() (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ghCLIToken returns the gh CLI's token, memoised for ghCLITokenTTL. Failures
// are cached too, so a missing or logged-out gh costs one subprocess per
// window rather than one per call.
func ghCLIToken() string {
	ghCLI.Lock()
	defer ghCLI.Unlock()
	if !ghCLI.checked.IsZero() && time.Since(ghCLI.checked) < ghCLITokenTTL {
		return ghCLI.token
	}
	run := ghCLI.run
	if run == nil {
		run = runGHAuthToken
	}
	token, err := run()
	if err != nil {
		token = ""
	}
	ghCLI.token, ghCLI.checked = token, time.Now()
	return token
}

// ResetGHCLICache forgets the memoised gh token. Exported for tests that swap
// PATH or fake the gh binary between cases.
func ResetGHCLICache() {
	ghCLI.Lock()
	defer ghCLI.Unlock()
	ghCLI.token, ghCLI.checked = "", time.Time{}
}

// DefaultFirecrawlURL is the hosted Firecrawl API base. Self-hosted
// instances override it via firecrawl_url (ketch appends /v2/search).
const DefaultFirecrawlURL = "https://api.firecrawl.dev"

// firecrawlSearchPath is appended to the Firecrawl API base to reach the v2
// search endpoint. See https://docs.firecrawl.dev/api-reference/endpoint/search.
const firecrawlSearchPath = "/v2/search"

// Defaults returns the built-in default configuration.

// normalizeFirecrawlBase reduces a configured firecrawl_url to a bare API
// base. A trailing /v2/search is stripped: operators reasonably paste the full
// endpoint they call, and appending the search path to that would request
// .../v2/search/v2/search.
func normalizeFirecrawlBase(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	base = strings.TrimSuffix(base, firecrawlSearchPath)
	return strings.TrimRight(base, "/")
}

// EffectiveFirecrawlURL returns the Firecrawl API base URL, falling back to
// DefaultFirecrawlURL when unset.
func (c Config) EffectiveFirecrawlURL() string {
	if u := normalizeFirecrawlBase(c.String("firecrawl_url")); u != "" {
		return u
	}
	return DefaultFirecrawlURL
}

// IsDefaultFirecrawlURL reports whether the effective Firecrawl base is the
// hosted cloud API (which requires an API key).
func (c Config) IsDefaultFirecrawlURL() bool {
	return strings.EqualFold(c.EffectiveFirecrawlURL(), DefaultFirecrawlURL)
}

// FirecrawlSearchURL joins a Firecrawl API base with the v2 search path. The
// base is normalized first, and an empty base uses the hosted default, so
// callers can pass a raw config value straight through.
func FirecrawlSearchURL(base string) string {
	base = normalizeFirecrawlBase(base)
	if base == "" {
		base = DefaultFirecrawlURL
	}
	return base + firecrawlSearchPath
}

func Defaults() Config {
	return Config{
		Backend: "brave",

		Limit:       5,
		CacheTTL:    "72h",
		CodeBackend: "grepapp",
		DocsBackend: "context7",

		ExternalPDFToMDConverterTimeoutSec: 300,
	}
}
