package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/cookies"
	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/scrape"
	"github.com/1broseidon/ketch/urlrewrite"
	"github.com/spf13/cobra"
)

// configInfo is the discovery payload returned by `ketch config`.
// The *_set booleans report key presence only — key values are never printed.
// github_token_set follows the same resolution chain as github_token_source
// (config → $GITHUB_TOKEN/$GH_TOKEN → gh CLI): it is true iff the source is
// not "none".
type configInfo struct {
	ConfigPath                         string            `json:"config_path"`
	Backend                            string            `json:"backend"`
	SearxngURL                         string            `json:"searxng_url"`
	BraveAPIKeySet                     bool              `json:"brave_api_key_set"`
	BraveAPIKeysCount                  int               `json:"brave_api_keys_count"`
	ExaAPIKeySet                       bool              `json:"exa_api_key_set"`
	ExaAPIKeysCount                    int               `json:"exa_api_keys_count"`
	FirecrawlAPIKeySet                 bool              `json:"firecrawl_api_key_set"`
	FirecrawlAPIKeysCount              int               `json:"firecrawl_api_keys_count"`
	FirecrawlURL                       string            `json:"firecrawl_url"`
	KeenableAPIKeySet                  bool              `json:"keenable_api_key_set"`
	KeenableAPIKeysCount               int               `json:"keenable_api_keys_count"`
	TavilyAPIKeySet                    bool              `json:"tavily_api_key_set"`
	TavilyAPIKeysCount                 int               `json:"tavily_api_keys_count"`
	SerpBaseAPIKeySet                  bool              `json:"serpbase_api_key_set"`
	SerpBaseAPIKeysCount               int               `json:"serpbase_api_keys_count"`
	Limit                              int               `json:"limit"`
	CacheTTL                           string            `json:"cache_ttl"`
	Browser                            string            `json:"browser,omitempty"`
	CookieFile                         string            `json:"cookie_file,omitempty"`
	UserAgent                          string            `json:"user_agent,omitempty"`
	CodeBackend                        string            `json:"code_backend"`
	DocsBackend                        string            `json:"docs_backend"`
	Context7APIKeySet                  bool              `json:"context7_api_key_set"`
	SourcegraphURL                     string            `json:"sourcegraph_url"`
	GithubTokenSource                  string            `json:"github_token_source"`
	GithubTokenSet                     bool              `json:"github_token_set"`
	URLRewrites                        []urlrewrite.Rule `json:"url_rewrites,omitempty"`
	SPAMarkers                         []string          `json:"spa_markers,omitempty"`
	MCPTools                           []string          `json:"mcp_tools"` // effective set `ketch mcp serve` will publish
	ExternalPDFToMDConverterCommand    string            `json:"external_pdf_to_md_converter_command,omitempty"`
	ExternalPDFToMDConverterTimeoutSec int               `json:"external_pdf_to_md_converter_timeout_sec"`
	EnvOverrides                       []config.Override `json:"env_overrides,omitempty"`
	AvailableBackends                  []string          `json:"available_backends"`
	AvailableCodeBackends              []string          `json:"available_code_backends"`
	AvailableDocBackends               []string          `json:"available_doc_backends"`
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or manage configuration",
	Long:  `Display effective configuration as JSON, or manage the config file. The default output is a discovery payload showing all effective settings and available backends.`,
	RunE:  runConfigShow,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a default config file",
	RunE:  runConfigInit,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config value",
	Args:  exitArgs(cobra.ExactArgs(2)),
	RunE:  runConfigSet,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	RunE:  runConfigPath,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configPathCmd)
}

func runConfigShow(_ *cobra.Command, _ []string) error {
	path, _ := config.Path()
	// cfg/cfgResult were loaded at init (file + KETCH_* env overlay); a bad
	// env value already errored in PersistentPreRunE before reaching here.
	info := buildConfigInfo(cfg, path)
	info.EnvOverrides = cfgResult.Overrides

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

func buildConfigInfo(c config.Config, path string) configInfo {
	_, ghSource := c.ResolveGithubToken()
	braveKeys := c.BraveKeys()
	exaKeys := c.ExaKeys()
	firecrawlKeys := c.FirecrawlKeys()
	keenableKeys := c.KeenableKeys()
	tavilyKeys := c.TavilyKeys()
	serpbaseKeys := c.SerpBaseKeys()
	return configInfo{
		ConfigPath:                         path,
		Backend:                            c.Backend,
		SearxngURL:                         c.SearxngURL,
		BraveAPIKeySet:                     len(braveKeys) > 0,
		BraveAPIKeysCount:                  len(braveKeys),
		ExaAPIKeySet:                       len(exaKeys) > 0,
		ExaAPIKeysCount:                    len(exaKeys),
		FirecrawlAPIKeySet:                 len(firecrawlKeys) > 0,
		FirecrawlAPIKeysCount:              len(firecrawlKeys),
		FirecrawlURL:                       c.EffectiveFirecrawlURL(),
		KeenableAPIKeySet:                  len(keenableKeys) > 0,
		KeenableAPIKeysCount:               len(keenableKeys),
		TavilyAPIKeySet:                    len(tavilyKeys) > 0,
		TavilyAPIKeysCount:                 len(tavilyKeys),
		SerpBaseAPIKeySet:                  len(serpbaseKeys) > 0,
		SerpBaseAPIKeysCount:               len(serpbaseKeys),
		Limit:                              c.Limit,
		CacheTTL:                           c.CacheTTL,
		Browser:                            c.Browser,
		CookieFile:                         c.CookieFile,
		UserAgent:                          effectiveUserAgent(c),
		CodeBackend:                        c.CodeBackend,
		DocsBackend:                        c.DocsBackend,
		Context7APIKeySet:                  c.Context7APIKey != "",
		SourcegraphURL:                     c.SourcegraphURL,
		GithubTokenSource:                  ghSource,
		GithubTokenSet:                     ghSource != "none",
		URLRewrites:                        c.URLRewrites,
		SPAMarkers:                         c.SPAMarkers,
		MCPTools:                           effectiveMCPTools(c),
		ExternalPDFToMDConverterCommand:    c.ExternalPDFToMDConverterCommand,
		ExternalPDFToMDConverterTimeoutSec: c.ExternalPDFToMDConverterTimeoutSec,
		AvailableBackends:                  config.AvailableBackends(),
		AvailableCodeBackends:              config.AvailableCodeBackends(),
		AvailableDocBackends:               config.AvailableDocBackends(),
	}
}

func runConfigInit(_ *cobra.Command, _ []string) error {
	path, err := config.Path()
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return exitErrf(ExitPrecondition, "config already exists: %s", path)
	}

	if err := config.Save(config.Defaults()); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "created %s\n", path)
	return nil
}

func runConfigSet(_ *cobra.Command, args []string) error {
	// LoadFile, not Load: `config set` must round-trip the file as-is and
	// never persist env-derived (KETCH_*) values into it.
	c := config.LoadFile()
	key, value := args[0], args[1]

	if err := applyConfigSet(&c, key, value); err != nil {
		return err
	}

	if err := config.Save(c); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, configSetAcknowledgement(c, key, value))
	return nil
}

func configSetAcknowledgement(c config.Config, key, value string) string {
	if count, item, ok := configSecretCount(c, key); ok {
		if count != 1 {
			item += "s"
		}
		return fmt.Sprintf("set %s (%d %s)", key, count, item)
	}
	return fmt.Sprintf("set %s = %s", key, value)
}

// configSecretCount recognizes every secret accepted by config set. API-key
// counts use the effective de-duplicated pools, not the raw field lengths.
func configSecretCount(c config.Config, key string) (int, string, bool) {
	switch key {
	case "brave_api_key", "brave_api_keys":
		return len(c.BraveKeys()), "key", true
	case "exa_api_key", "exa_api_keys":
		return len(c.ExaKeys()), "key", true
	case "firecrawl_api_key", "firecrawl_api_keys":
		return len(c.FirecrawlKeys()), "key", true
	case "keenable_api_key", "keenable_api_keys":
		return len(c.KeenableKeys()), "key", true
	case "tavily_api_key", "tavily_api_keys":
		return len(c.TavilyKeys()), "key", true
	case "serpbase_api_key", "serpbase_api_keys":
		return len(c.SerpBaseKeys()), "key", true
	case "context7_api_key":
		return boolCount(c.Context7APIKey != ""), "key", true
	case "github_token":
		return boolCount(c.GithubToken != ""), "token", true
	default:
		return 0, "", false
	}
}

func boolCount(set bool) int {
	if set {
		return 1
	}
	return 0
}

// applyConfigSet is a flat key→field dispatch; its cyclomatic complexity scales
// with the number of config keys, not with any real branching depth.
//
//nolint:gocyclo // one arm per config key; splitting it would obscure, not clarify
func applyConfigSet(c *config.Config, key, value string) error {
	switch key {
	case "backend":
		c.Backend = value
	case "searxng_url":
		c.SearxngURL = value
	case "brave_api_key":
		c.BraveAPIKey = value
	case "brave_api_keys":
		return setAPIKeys(&c.BraveAPIKeys, key, value)
	case "exa_api_key":
		c.ExaAPIKey = value
	case "exa_api_keys":
		return setAPIKeys(&c.ExaAPIKeys, key, value)
	case "firecrawl_api_key":
		c.FirecrawlAPIKey = value
	case "firecrawl_api_keys":
		return setAPIKeys(&c.FirecrawlAPIKeys, key, value)
	case "firecrawl_url":
		c.FirecrawlURL = value
	case "keenable_api_key":
		c.KeenableAPIKey = value
	case "keenable_api_keys":
		return setAPIKeys(&c.KeenableAPIKeys, key, value)
	case "tavily_api_key":
		c.TavilyAPIKey = value
	case "tavily_api_keys":
		return setAPIKeys(&c.TavilyAPIKeys, key, value)
	case "serpbase_api_key":
		c.SerpBaseAPIKey = value
	case "serpbase_api_keys":
		return setAPIKeys(&c.SerpBaseAPIKeys, key, value)
	case "limit":
		return setLimit(c, value)
	case "cache_ttl":
		return setCacheTTL(c, value)
	case "browser":
		c.Browser = value
	case "code_backend":
		c.CodeBackend = value
	case "docs_backend":
		c.DocsBackend = value
	case "context7_api_key":
		c.Context7APIKey = value
	case "sourcegraph_url":
		c.SourcegraphURL = value
	case "github_token":
		c.GithubToken = value
	case "url_rewrites":
		return setURLRewrites(c, value)
	case "spa_markers":
		return setSPAMarkers(c, value)
	case "mcp_tools":
		return setMCPTools(c, value)
	case "cookie_file":
		return setCookieFile(c, value)
	case "user_agent":
		return setUserAgent(c, value)
	case "external_pdf_to_md_converter_command":
		return setExternalPDFConverterCommand(c, value)
	case "external_pdf_to_md_converter_timeout_sec":
		return setExternalPDFConverterTimeout(c, value)
	default:
		return exitErrf(ExitValidation, "unknown key: %s (valid: backend, searxng_url, brave_api_key, brave_api_keys, exa_api_key, exa_api_keys, firecrawl_api_key, firecrawl_api_keys, firecrawl_url, keenable_api_key, keenable_api_keys, tavily_api_key, tavily_api_keys, serpbase_api_key, serpbase_api_keys, limit, cache_ttl, browser, code_backend, docs_backend, context7_api_key, sourcegraph_url, github_token, url_rewrites, spa_markers, mcp_tools, cookie_file, user_agent, external_pdf_to_md_converter_command, external_pdf_to_md_converter_timeout_sec)", key)
	}
	return nil
}

func setAPIKeys(destination *[]string, key, value string) error {
	var keys []string
	if err := json.Unmarshal([]byte(value), &keys); err != nil {
		return exitErrf(ExitValidation, "%s must be a JSON array of strings: %w", key, err)
	}
	if keys == nil {
		return exitErrf(ExitValidation, "%s must be a JSON array of strings, not null", key)
	}
	*destination = keys
	return nil
}

func setLimit(c *config.Config, value string) error {
	n, err := strconv.Atoi(value)
	if err != nil {
		return exitErrf(ExitValidation, "limit must be an integer: %w", err)
	}
	c.Limit = n
	return nil
}

func setCacheTTL(c *config.Config, value string) error {
	if _, err := time.ParseDuration(value); err != nil {
		return exitErrf(ExitValidation, "cache_ttl must be a duration (e.g. 1h, 30m): %w", err)
	}
	c.CacheTTL = value
	return nil
}

func setExternalPDFConverterCommand(c *config.Config, value string) error {
	if value == "" {
		c.ExternalPDFToMDConverterCommand = ""
		return nil
	}
	if _, err := extract.NewExternalPDFExtractor(value, time.Second); err != nil {
		return exitErrf(ExitValidation, "invalid external_pdf_to_md_converter_command: %w", err)
	}
	c.ExternalPDFToMDConverterCommand = value
	return nil
}

func setExternalPDFConverterTimeout(c *config.Config, value string) error {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return exitErrf(ExitValidation, "external_pdf_to_md_converter_timeout_sec must be a positive integer")
	}
	c.ExternalPDFToMDConverterTimeoutSec = n
	return nil
}

// setCookieFile validates the jar parses before persisting. Empty clears it.
func setCookieFile(c *config.Config, value string) error {
	if value == "" {
		c.CookieFile = ""
		return nil
	}
	if _, err := cookies.Load(value); err != nil {
		return exitErrf(ExitValidation, "invalid cookie_file: %w", err)
	}
	c.CookieFile = value
	return nil
}

// setUserAgent persists an HTTP User-Agent override. Empty clears it so the
// built-in honest default is used. Control characters are rejected — they
// cannot appear in an HTTP header value.
func setUserAgent(c *config.Config, value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return exitErrf(ExitValidation, "user_agent must not contain control characters")
	}
	c.UserAgent = strings.TrimSpace(value)
	return nil
}

// effectiveUserAgent returns the User-Agent ketch will send: an operator
// override when set, otherwise the built-in honest default. Surfaced in
// `ketch config` so agents can diagnose bot-filter 403s without guessing.
func effectiveUserAgent(c config.Config) string {
	if ua := strings.TrimSpace(c.UserAgent); ua != "" {
		return ua
	}
	return scrape.DefaultUserAgent()
}

// effectiveMCPTools returns the tools `ketch mcp serve` will publish: the
// operator's mcp_tools allowlist when set, otherwise every tool. A list the
// normalizer rejects is possible only in a hand-edited config file; it is
// reported as-is — `ketch mcp serve` fails loud on it with the valid names.
func effectiveMCPTools(c config.Config) []string {
	tools, err := config.NormalizeMCPTools(c.MCPTools)
	switch {
	case err != nil:
		return c.MCPTools
	case len(tools) == 0:
		return append([]string(nil), config.MCPToolNames...)
	default:
		return tools
	}
}

func setURLRewrites(c *config.Config, value string) error {
	var rules []urlrewrite.Rule
	if err := json.Unmarshal([]byte(value), &rules); err != nil {
		return exitErrf(ExitValidation, "url_rewrites must be a JSON array of {match, replace}: %w", err)
	}
	if _, err := urlrewrite.NewRewriter(rules); err != nil {
		return exitErrf(ExitValidation, "%w", err)
	}
	c.URLRewrites = rules
	return nil
}

// setSPAMarkers parses a JSON array of substrings that, when found in a page's
// HTML, mark it as a JS-rendered shell needing browser rendering (matched
// alongside the built-in markers). An empty array ([]) clears the list. Blank
// markers are rejected — a "" marker would match every page.
func setSPAMarkers(c *config.Config, value string) error {
	var markers []string
	if err := json.Unmarshal([]byte(value), &markers); err != nil {
		return exitErrf(ExitValidation, "spa_markers must be a JSON array of strings: %w", err)
	}
	for i, m := range markers {
		if strings.TrimSpace(m) == "" {
			return exitErrf(ExitValidation, "spa_markers[%d] is blank; markers must be non-empty substrings", i)
		}
	}
	c.SPAMarkers = markers
	return nil
}

// setMCPTools persists the allowlist of tools `ketch mcp serve` publishes.
// Accepts a JSON array (e.g. ["search","scrape"]) or a comma-separated list
// (search, scrape); an empty value or [] clears the key so the server
// publishes every tool. Names are validated against the known tool set.
func setMCPTools(c *config.Config, value string) error {
	trimmed := strings.TrimSpace(value)
	var raw []string
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			return exitErrf(ExitValidation, "mcp_tools must be a JSON array of strings or a comma-separated list: %w", err)
		}
	} else if trimmed != "" {
		raw = strings.Split(trimmed, ",")
	}
	tools, err := config.NormalizeMCPTools(raw)
	if err != nil {
		return exitErrf(ExitValidation, "invalid mcp_tools: %w", err)
	}
	c.MCPTools = tools
	return nil
}

func runConfigPath(_ *cobra.Command, _ []string) error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}
