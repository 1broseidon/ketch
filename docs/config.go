package docs

import (
	"fmt"
	"strings"

	"github.com/1broseidon/ketch/internal/failure"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// ErrUnknownBackend identifies an unknown docs provider; unavailable providers are preconditions.
var ErrUnknownBackend = failure.New(failure.Validation, "unknown_backend", "unknown docs backend")

// NewFromConfig constructs a backend through its provider-owned
// descriptor. A legacy name is read as its replacement (CanonicalBackend).
func NewFromConfig(c *config.Config, backend string, deps Deps) (Searcher, error) {
	p, ok := Lookup(CanonicalBackend(backend))
	if !ok {
		return nil, fmt.Errorf("%w %q (available: %s; search is the default, -b context7 asks Context7 for one call)",
			ErrUnknownBackend, backend, strings.Join(AvailableBackends(), ", "))
	}
	return p.Build(c, deps)
}
