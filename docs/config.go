package docs

import (
	"errors"
	"fmt"
	"strings"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// ErrUnknownBackend identifies an unknown docs provider; unavailable providers are preconditions.
var ErrUnknownBackend = errors.New("unknown docs backend")

// NewFromConfig constructs a backend through its provider-owned descriptor.
func NewFromConfig(c *config.Config, backend string) (Searcher, error) {
	p, ok := Lookup(backend)
	if !ok {
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownBackend, backend, strings.Join(AvailableBackends(), ", "))
	}
	return p.Build(c)
}
