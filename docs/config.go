package docs

import (
	"errors"
	"fmt"
	"strings"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// ErrUnknownBackend identifies an unknown docs provider; unavailable providers are preconditions.
var ErrUnknownBackend = errors.New("unknown docs backend")

// NewFromConfig constructs a backend through its provider-owned descriptor,
// or the auto chain (AutoBackend).
func NewFromConfig(c *config.Config, backend string, deps Deps) (Searcher, error) {
	if backend == AutoBackend {
		return newAutoFromConfig(c, deps), nil
	}
	p, ok := Lookup(backend)
	if !ok {
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownBackend, backend, strings.Join(SelectableBackends(), ", "))
	}
	return p.Build(c, deps)
}
