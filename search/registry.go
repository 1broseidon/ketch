package search

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// Provider owns the wiring and health policy for one search backend.
type Provider struct {
	MinProbeTimeout time.Duration
	ID              string
	Setup           string
	Name            string
	Hidden          bool
	Usable          func(*config.Config) bool
	Settings        []config.Setting
	New             func(*config.Config) (Searcher, error)
	Probe           func(context.Context, *http.Client, *config.Config) (health.Status, string)
}

// Required reports whether a failing health check must fail doctor. Selection
// and explicitly configured credentials gate health independently of usability.
func (p Provider) Required(cfg *config.Config) bool {
	if cfg.Backend == p.ID {
		return true
	}
	for _, setting := range p.Settings {
		if setting.Configured(cfg) {
			return true
		}
	}
	return false
}

var providers = []Provider{
	braveProvider(),
	ddgProvider(),
	searxngProvider(),
	exaProvider(),
	firecrawlProvider(),
	keenableProvider(),
	tavilyProvider(),
	parallelProvider(),
	serpbaseProvider(),
	degoogProvider(),
	serplyProvider(),
	youcomProvider(),
}

// Providers returns the descriptors in their stable presentation order.
func Providers() []Provider {
	snapshot := slices.Clone(providers)
	for i := range snapshot {
		snapshot[i].Settings = slices.Clone(snapshot[i].Settings)
	}
	return snapshot
}

// AvailableBackends returns the implemented provider IDs in registry order.
func AvailableBackends() []string {
	var names []string
	for _, p := range providers {
		if !p.Hidden {
			names = append(names, p.ID)
		}
	}
	return names
}

// ProviderNames returns human-readable names in registry order.
func ProviderNames() []string {
	var names []string
	for _, p := range providers {
		if !p.Hidden {
			names = append(names, p.Name)
		}
	}
	return names
}

// Lookup finds a provider without constructing it or accessing the network.
func Lookup(id string) (Provider, bool) {
	for _, p := range providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Build applies the provider's one usability predicate before construction.
func (p Provider) Build(cfg *config.Config) (Searcher, error) {
	if !p.Usable(cfg) {
		return nil, errors.New(p.Setup)
	}
	return p.New(cfg)
}

// DescriptionNames formats provider display names for CLI and MCP help.
func DescriptionNames() string {
	names := ProviderNames()
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}
