package search

import (
	"errors"
	"fmt"
	"strings"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// ErrUnknownBackend identifies an unknown provider; missing prerequisites do not wrap it.
var ErrUnknownBackend = errors.New("unknown search backend")

// NewFromConfig constructs a registered search provider, or the auto fallback
// chain. The existing SearXNG per-call override is applied to a copy,
// preserving the shared configuration.
func NewFromConfig(cfg *config.Config, backend, searxngURL string) (Searcher, error) {
	if backend == AutoBackend {
		return NewAutoFromConfig(cfg, searxngURL)
	}
	p, ok := Lookup(backend)
	if !ok {
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownBackend, backend, strings.Join(SelectableBackends(), ", "))
	}
	c := *cfg
	if searxngURL != "" {
		c.SetProvider("searxng_url", searxngURL)
	}
	return p.Build(&c)
}

// SelectableBackends returns every value accepted by --backend and the backend
// config key: the auto chain first, then the providers in registry order.
func SelectableBackends() []string {
	return append([]string{AutoBackend}, AvailableBackends()...)
}

// IsBackend reports whether id names a selectable backend.
func IsBackend(id string) bool {
	if id == AutoBackend {
		return true
	}
	_, ok := Lookup(id)
	return ok
}
