package code

import (
	"fmt"
	"strings"

	"github.com/1broseidon/ketch/internal/failure"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// ErrUnknownBackend identifies an unknown code provider; unavailable providers are preconditions.
var ErrUnknownBackend = failure.New(failure.Validation, "unknown_backend", "unknown code backend")

// NewFromConfig constructs a backend through its provider-owned descriptor.
func NewFromConfig(c *config.Config, backend string) (Searcher, error) {
	p, ok := Lookup(backend)
	if !ok {
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownBackend, backend, strings.Join(AvailableBackends(), ", "))
	}
	return p.Build(c)
}
