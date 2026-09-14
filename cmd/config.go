package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/cookies"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/scrape"
	"github.com/1broseidon/ketch/search"
	"github.com/1broseidon/ketch/urlrewrite"
	"github.com/spf13/cobra"
)

// configInfo is the discovery payload returned by `ketch config`.
// The *_set booleans report key presence only — key values are never printed.
// github_token_set follows the same resolution chain as github_token_source
// (config → $GITHUB_TOKEN/$GH_TOKEN → gh CLI): it is true iff the source is
// not "none".
type configInfo struct {
	ConfigPath                         string             `json:"config_path" order:"0"`
	Backend                            string             `json:"backend" order:"1"`
	Limit                              int                `json:"limit" order:"16"`
	CacheTTL                           string             `json:"cache_ttl" order:"17"`
	Browser                            string             `json:"browser,omitempty" order:"18"`
	CookieFile                         string             `json:"cookie_file,omitempty" order:"19"`
	UserAgent                          string             `json:"user_agent,omitempty" order:"20"`
	CodeBackend                        string             `json:"code_backend" order:"21"`
	DocsBackend                        string             `json:"docs_backend" order:"22"`
	URLRewrites                        []urlrewrite.Rule  `json:"url_rewrites,omitempty" order:"27"`
	SPAMarkers                         []string           `json:"spa_markers,omitempty" order:"28"`
	MCPTools                           []string           `json:"mcp_tools" order:"29"` // effective set `ketch mcp serve` will publish
	ExternalPDFToMDConverterCommand    string             `json:"external_pdf_to_md_converter_command,omitempty" order:"30"`
	ExternalPDFToMDConverterTimeoutSec int                `json:"external_pdf_to_md_converter_timeout_sec" order:"31"`
	EnvOverrides                       []config.Override  `json:"env_overrides,omitempty" order:"32"`
	AvailableBackends                  []string           `json:"available_backends" order:"33"`
	AvailableCodeBackends              []string           `json:"available_code_backends" order:"34"`
	AvailableDocBackends               []string           `json:"available_doc_backends" order:"35"`
	ProviderFields                     []configbase.Field `json:"-"`
	Providers                          map[string]any     `json:"-"`
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
	info := configInfo{
		ConfigPath:                         path,
		Backend:                            c.Backend,
		Limit:                              c.Limit,
		CacheTTL:                           c.CacheTTL,
		Browser:                            c.Browser,
		CookieFile:                         c.CookieFile,
		UserAgent:                          effectiveUserAgent(c),
		CodeBackend:                        c.CodeBackend,
		DocsBackend:                        c.DocsBackend,
		URLRewrites:                        c.URLRewrites,
		SPAMarkers:                         c.SPAMarkers,
		MCPTools:                           effectiveMCPTools(c),
		ExternalPDFToMDConverterCommand:    c.ExternalPDFToMDConverterCommand,
		ExternalPDFToMDConverterTimeoutSec: c.ExternalPDFToMDConverterTimeoutSec,
		AvailableBackends:                  config.AvailableBackends(),
		AvailableCodeBackends:              config.AvailableCodeBackends(),
		AvailableDocBackends:               config.AvailableDocBackends(),
	}

	info.Providers = make(map[string]any)
	info.ProviderFields = config.ProviderDiscovery(&c)
	for _, field := range info.ProviderFields {
		info.Providers[field.Name] = field.Value
	}

	return info
}

func (info configInfo) MarshalJSON() ([]byte, error) {
	return configbase.MarshalFields(append(configbase.StructFields(info), info.ProviderFields...))
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
	for _, setting := range config.ProviderSettings() {
		if !setting.Secret || (key != setting.Key && key != setting.Plural) {
			continue
		}
		if setting.Plural != "" {
			return len(setting.Keys(&c)), "key", true
		}
		label := "key"
		if setting.Token {
			label = "token"
		}
		return boolCount(c.String(setting.Key) != ""), label, true
	}
	return 0, "", false
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
	for _, setting := range config.ProviderSettings() {
		if handled, err := setting.Set(c, key, value); handled {
			if err != nil {
				return exitErrf(ExitValidation, "%s", err)
			}
			return nil
		}
	}
	switch key {
	case "backend":
		return setBackend(&c.Backend, "search", value, func(id string) bool { _, ok := search.Lookup(id); return ok }, config.AvailableBackends())
	case "limit":
		return setLimit(c, value)
	case "cache_ttl":
		return setCacheTTL(c, value)
	case "browser":
		c.Browser = value
	case "code_backend":
		return setBackend(&c.CodeBackend, "code", value, func(id string) bool { _, ok := code.Lookup(id); return ok }, config.AvailableCodeBackends())
	case "docs_backend":
		return setBackend(&c.DocsBackend, "docs", value, func(id string) bool { _, ok := docs.Lookup(id); return ok }, config.AvailableDocBackends())
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
		return exitErrf(ExitValidation, "unknown key: %s (valid: %s)", key, strings.Join(validConfigKeys(), ", "))
	}
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
		return config.MCPToolNames() // already a private copy
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

// setBackend persists a default backend for one surface after checking the
// name against that surface's registry. The check uses the registry lookup
// rather than the advertised list so a hidden provider (the planned local
// docs backend) is accepted exactly when the command itself would accept it;
// the error lists only advertised names. Names are exact: the commands
// resolve them case-sensitively, so "Brave" would fail at run time.
func setBackend(dst *string, surface, value string, known func(string) bool, available []string) error {
	name := strings.TrimSpace(value)
	if name == "" {
		return exitErrf(ExitValidation, "%sbackend cannot be empty (valid: %s)", surfaceKeyPrefix(surface), strings.Join(available, ", "))
	}
	if !known(name) {
		return exitErrf(ExitValidation, "unknown %s backend %q (valid: %s)", surface, name, strings.Join(available, ", "))
	}
	*dst = name
	return nil
}

// surfaceKeyPrefix maps a surface to its config key stem: the search key is
// the historical bare "backend", the others carry the surface name.
func surfaceKeyPrefix(surface string) string {
	if surface == "search" {
		return ""
	}
	return surface + "_"
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

func validConfigKeys() []string {
	fields := []configbase.Field{{Name: "backend", Order: 0}, {Name: "limit", Order: 15}, {Name: "cache_ttl", Order: 16}, {Name: "browser", Order: 17}, {Name: "code_backend", Order: 18}, {Name: "docs_backend", Order: 19}, {Name: "url_rewrites", Order: 23}, {Name: "spa_markers", Order: 24}, {Name: "mcp_tools", Order: 25}, {Name: "cookie_file", Order: 26}, {Name: "user_agent", Order: 27}, {Name: "external_pdf_to_md_converter_command", Order: 28}, {Name: "external_pdf_to_md_converter_timeout_sec", Order: 29}}
	for _, s := range config.ProviderSettings() {
		fields = append(fields, configbase.Field{Name: s.Key, Order: s.ValidationOrder})
		if s.Plural != "" {
			fields = append(fields, configbase.Field{Name: s.Plural, Order: s.ValidationOrder})
		}
	}
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].Order < fields[j].Order })
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.Name
	}
	return keys
}
