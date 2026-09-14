package docs

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// Provider owns the wiring and health policy for one docs backend.
type Provider struct {
	ID           string
	Setup        string
	LibrarySetup string
	Name         string
	Hidden       bool
	Usable       func(*config.Config) bool
	Settings     []config.Setting
	New          func(*config.Config) (Searcher, error)
	Probe        func(context.Context, *http.Client, *config.Config) (health.Status, string)
}

// Required reports whether a failing health check must fail doctor. Selection
// and explicitly configured credentials gate health independently of usability.
func (p Provider) Required(cfg *config.Config) bool {
	if cfg.DocsBackend == p.ID {
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
	context7Provider(),
	localProvider(),
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

// LibraryResolver is the optional library capability of a docs provider.
// It preserves Context7's existing result shapes without capability bitflags.
type LibraryResolver interface {
	ResolveLibrary(context.Context, string, int) ([]LibraryMatch, error)
	GetDocs(context.Context, string, string, int) ([]Result, error)
}

// LibraryBackends lists providers whose side-effect-free factory implements
// library operations. Factories do not validate credentials or perform I/O.
func LibraryBackends() []string {
	var names []string
	for _, p := range providers {
		if p.Hidden {
			continue
		}
		client, err := p.New(&config.Config{})
		if err != nil {
			continue
		}
		if _, ok := client.(LibraryResolver); ok {
			names = append(names, p.ID)
		}
	}
	return names
}

// SupportsLibraries reports whether a registered provider has library operations.
func SupportsLibraries(id string) bool { return slices.Contains(LibraryBackends(), id) }

// ResolveBackend retains the legacy resolve fallback while allowing additional
// library-capable providers to handle an explicitly selected backend.
func ResolveBackend(selected string) string {
	if SupportsLibraries(selected) {
		return selected
	}
	names := LibraryBackends()
	if len(names) > 0 {
		return names[0]
	}
	return selected
}

// LibraryProviderNames returns display names for providers with library operations.
func LibraryProviderNames() []string {
	var names []string
	for _, id := range LibraryBackends() {
		p, _ := Lookup(id)
		names = append(names, p.Name)
	}
	return names
}
