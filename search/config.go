package search

import (
	"errors"
	"fmt"
	"strings"

	"github.com/1broseidon/ketch/config"
)

// ErrUnknownBackend reports a backend name that is not a known search backend.
// Callers classify errors wrapping it as validation failures (bad input);
// any other NewFromConfig error is a missing precondition (e.g. API key).
var ErrUnknownBackend = errors.New("unknown search backend")

// NewFromConfig builds the Searcher for backend, resolving API keys and
// instance URLs from cfg exactly as the `ketch search` CLI does. searxngURL
// overrides cfg.SearxngURL when non-empty (the --searxng-url flag / MCP
// searxng_url param). Both the CLI and the MCP server call this — it is the
// single owner of the backend switch.
func NewFromConfig(cfg *config.Config, backend, searxngURL string) (Searcher, error) {
	switch backend {
	case "brave", "exa", "firecrawl", "keenable", "tavily", "serpbase", "synthetic":
		return newCredentialAwareBackend(cfg, backend)
	case "searxng":
		if searxngURL == "" {
			searxngURL = cfg.SearxngURL
		}
		return NewSearXNG(searxngURL), nil
	case "ddg":
		return NewDDG(), nil
	case "parallel":
		return NewParallel(), nil
	default:
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownBackend, backend, strings.Join(config.AvailableBackends(), ", "))
	}
}

// newCredentialAwareBackend owns each provider's key policy. Keeping these
// checks together prevents the shared CLI/MCP factory from growing in
// complexity whenever another credential-capable provider is added.
func newCredentialAwareBackend(cfg *config.Config, backend string) (Searcher, error) {
	switch backend {
	case "brave":
		keys := cfg.BraveKeys()
		if len(keys) == 0 {
			return nil, fmt.Errorf("brave: API key not set (get one free at https://brave.com/search/api/ then: ketch config set brave_api_key <key>)")
		}
		return newBraveWithKeys(keys), nil
	case "exa":
		return newEXAWithKeys(cfg.ExaKeys()), nil
	case "firecrawl":
		keys := cfg.FirecrawlKeys()
		// Hosted Firecrawl requires a key; self-hosted instances often run
		// without auth, so a custom firecrawl_url may omit the key.
		if len(keys) == 0 && cfg.IsDefaultFirecrawlURL() {
			return nil, fmt.Errorf("firecrawl: API key not set (get one free at https://firecrawl.dev then: ketch config set firecrawl_api_key <key>)")
		}
		return newFirecrawlWithKeys(keys, cfg.EffectiveFirecrawlURL()), nil
	case "keenable":
		return newKeenableWithKeys(cfg.KeenableKeys()), nil
	case "tavily":
		keys := cfg.TavilyKeys()
		if len(keys) == 0 {
			return nil, fmt.Errorf("tavily: API key not set (get one free at https://app.tavily.com then: ketch config set tavily_api_key <key>)")
		}
		return newTavilyWithKeys(keys), nil
	case "serpbase":
		keys := cfg.SerpBaseKeys()
		if len(keys) == 0 {
			return nil, fmt.Errorf("serpbase: API key not set (get a free key at https://serpbase.dev then: ketch config set serpbase_api_key <key>)")
		}
		return newSerpBaseWithKeys(keys), nil
	case "synthetic":
		keys := cfg.SyntheticKeys()
		if len(keys) == 0 {
			return nil, fmt.Errorf("synthetic: API key not set (get one at https://synthetic.new then: ketch config set synthetic_api_key <key>)")
		}
		return newSyntheticWithKeys(keys), nil
	default:
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownBackend, backend, strings.Join(config.AvailableBackends(), ", "))
	}
}
