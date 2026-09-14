package search

import (
	"errors"
	"fmt"
	"strings"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// ErrUnknownBackend identifies an unknown provider; missing prerequisites do not wrap it.
var ErrUnknownBackend = errors.New("unknown search backend")

// NewFromConfig constructs a registered search provider. The existing SearXNG
// per-call override is applied to a copy, preserving the shared configuration.
func NewFromConfig(cfg *config.Config, backend, searxngURL string) (Searcher, error) {
	p, ok := Lookup(backend)
	if !ok {
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownBackend, backend, strings.Join(AvailableBackends(), ", "))
	}
	c := *cfg
	if searxngURL != "" {
		c.SetProvider("searxng_url", searxngURL)
	}
	return p.Build(&c)
}
