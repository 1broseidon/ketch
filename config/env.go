package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// EnvPrefix is the prefix for every ketch environment variable.
const EnvPrefix = "KETCH_"

// Override records one config key whose value came from the environment.
type Override struct {
	Key      string `json:"key"`                // JSON config key, e.g. "limit"
	Var      string `json:"var"`                // env var applied, e.g. "KETCH_LIMIT"
	Previous string `json:"previous,omitempty"` // value replaced (file or default); redacted for secrets
}

// LoadResult is the outcome of Load: the effective config plus the
// provenance of every env-overridden key.
type LoadResult struct {
	Config    Config
	Overrides []Override
}

// envSpec describes one env-overridable config key. The mapping from JSON
// key to env var is mechanical: "KETCH_" + UPPER_SNAKE(key). Three kinds of
// keys are deliberately file-only (see docs/adr/0001-env-var-config.md):
// url_rewrites (unquotable regex JSON), spa_markers (no safe delimiter), and
// the plural *_api_keys pools (the singular var takes a comma-separated
// list instead). github_token is handled by ResolveGithubToken, not here.
type envSpec struct {
	key    string
	secret bool
	prev   func(c *Config) string
	apply  func(c *Config, v string) error
}

func stringSpec(key string, field func(c *Config) *string) envSpec {
	return envSpec{
		key:  key,
		prev: func(c *Config) string { return *field(c) },
		apply: func(c *Config, v string) error {
			*field(c) = v
			return nil
		},
	}
}

// keyPoolSpec maps a singular *_api_key var onto the provider's whole
// effective key pool: a comma-separated value replaces both the singular
// field and the plural list.
func keyPoolSpec(key string, single func(c *Config) *string, list func(c *Config) *[]string) envSpec {
	return envSpec{
		key:    key,
		secret: true,
		prev:   func(c *Config) string { return "" }, // secret: never reported
		apply: func(c *Config, v string) error {
			keys := splitCommaList(v)
			if len(keys) == 0 {
				return fmt.Errorf("must contain at least one non-blank key")
			}
			*single(c) = keys[0]
			*list(c) = keys[1:]
			return nil
		},
	}
}

func splitCommaList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func envSpecs() []envSpec {
	return []envSpec{
		stringSpec("backend", func(c *Config) *string { return &c.Backend }),
		stringSpec("searxng_url", func(c *Config) *string { return &c.SearxngURL }),
		keyPoolSpec("brave_api_key",
			func(c *Config) *string { return &c.BraveAPIKey },
			func(c *Config) *[]string { return &c.BraveAPIKeys }),
		keyPoolSpec("exa_api_key",
			func(c *Config) *string { return &c.ExaAPIKey },
			func(c *Config) *[]string { return &c.ExaAPIKeys }),
		keyPoolSpec("firecrawl_api_key",
			func(c *Config) *string { return &c.FirecrawlAPIKey },
			func(c *Config) *[]string { return &c.FirecrawlAPIKeys }),
		stringSpec("firecrawl_url", func(c *Config) *string { return &c.FirecrawlURL }),
		keyPoolSpec("keenable_api_key",
			func(c *Config) *string { return &c.KeenableAPIKey },
			func(c *Config) *[]string { return &c.KeenableAPIKeys }),
		keyPoolSpec("tavily_api_key",
			func(c *Config) *string { return &c.TavilyAPIKey },
			func(c *Config) *[]string { return &c.TavilyAPIKeys }),
		{
			key:  "limit",
			prev: func(c *Config) string { return strconv.Itoa(c.Limit) },
			apply: func(c *Config, v string) error {
				n, err := strconv.Atoi(v)
				if err != nil || n <= 0 {
					return fmt.Errorf("must be a positive integer, got %q", v)
				}
				c.Limit = n
				return nil
			},
		},
		{
			key:  "cache_ttl",
			prev: func(c *Config) string { return c.CacheTTL },
			apply: func(c *Config, v string) error {
				if _, err := time.ParseDuration(v); err != nil {
					return fmt.Errorf("must be a duration (e.g. 1h, 30m), got %q", v)
				}
				c.CacheTTL = v
				return nil
			},
		},
		stringSpec("browser", func(c *Config) *string { return &c.Browser }),
		stringSpec("code_backend", func(c *Config) *string { return &c.CodeBackend }),
		stringSpec("docs_backend", func(c *Config) *string { return &c.DocsBackend }),
		{
			key:    "context7_api_key",
			secret: true,
			prev:   func(c *Config) string { return "" }, // secret: never reported
			apply: func(c *Config, v string) error {
				c.Context7APIKey = v
				return nil
			},
		},
		stringSpec("sourcegraph_url", func(c *Config) *string { return &c.SourcegraphURL }),
		stringSpec("cookie_file", func(c *Config) *string { return &c.CookieFile }),
		stringSpec("user_agent", func(c *Config) *string { return &c.UserAgent }),
		stringSpec("external_pdf_to_md_converter_command",
			func(c *Config) *string { return &c.ExternalPDFToMDConverterCommand }),
		{
			key:  "external_pdf_to_md_converter_timeout_sec",
			prev: func(c *Config) string { return strconv.Itoa(c.ExternalPDFToMDConverterTimeoutSec) },
			apply: func(c *Config, v string) error {
				n, err := strconv.Atoi(v)
				if err != nil || n <= 0 {
					return fmt.Errorf("must be a positive integer, got %q", v)
				}
				c.ExternalPDFToMDConverterTimeoutSec = n
				return nil
			},
		},
	}
}

// EnvVar returns the environment variable name for a config key:
// "KETCH_" + UPPER_SNAKE of the JSON key.
func EnvVar(key string) string { return EnvPrefix + strings.ToUpper(key) }

// applyEnv overlays KETCH_* environment variables onto cfg. Empty values are
// treated as unset. Invalid values are reported (joined, one line per var)
// after all valid vars have been applied, so cfg stays best-effort usable.
func applyEnv(cfg *Config) ([]Override, error) {
	var overrides []Override
	var errs []error
	for _, spec := range envSpecs() {
		name := EnvVar(spec.key)
		value, ok := os.LookupEnv(name)
		if !ok || value == "" {
			continue
		}
		previous := spec.prev(cfg)
		if err := spec.apply(cfg, value); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		overrides = append(overrides, Override{Key: spec.key, Var: name, Previous: previous})
	}
	return overrides, errors.Join(errs...)
}

// secretEnvVars lists the KETCH_* vars that carry credentials.
func secretEnvVars() map[string]bool {
	vars := map[string]bool{EnvVar("github_token"): true}
	for _, spec := range envSpecs() {
		if spec.secret {
			vars[EnvVar(spec.key)] = true
		}
	}
	return vars
}

// ScrubbedEnviron returns os.Environ() with every KETCH_* secret variable
// (API keys, tokens) removed. Use it for spawned subprocesses (headless
// browser, external PDF converter) so injected credentials don't leak.
func ScrubbedEnviron() []string {
	secrets := secretEnvVars()
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if secrets[name] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
