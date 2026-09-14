package code

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// Provider owns the wiring and health policy for one code backend.
type Provider struct {
	ID       string
	CLIName  string
	Regexp   bool
	Setup    string
	Name     string
	Hidden   bool
	Usable   func(*config.Config) bool
	Settings []config.Setting
	New      func(*config.Config) (Searcher, error)
	Probe    func(context.Context, *http.Client, *config.Config) (health.Status, string)
}

// Required reports whether a failing health check must fail doctor. Selection
// and explicitly configured credentials gate health independently of usability.
func (p Provider) Required(cfg *config.Config) bool {
	if cfg.CodeBackend == p.ID {
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
	grepappProvider(),
	sourcegraphProvider(),
	githubProvider(),
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

// Lookup returns a descriptor without constructing a client.
func Lookup(id string) (Provider, bool) {
	for _, p := range providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Build checks the descriptor's usability before constructing a client.
func (p Provider) Build(c *config.Config) (Searcher, error) {
	if !p.Usable(c) {
		return nil, errors.New(p.Setup)
	}
	return p.New(c)
}

// DescriptionNames returns ordered display names for CLI or MCP descriptions.
func DescriptionNames(cli bool) string {
	var names []string
	for _, p := range providers {
		if p.Hidden {
			continue
		}
		name := p.Name
		if cli && p.CLIName != "" {
			name = p.CLIName
		}
		names = append(names, name)
	}
	return config.JoinNames(names)
}

// RegexpBackends reports providers implementing the existing regex query option.
func RegexpBackends() []string {
	var names []string
	for _, p := range providers {
		if p.Regexp {
			names = append(names, p.ID)
		}
	}
	return names
}
