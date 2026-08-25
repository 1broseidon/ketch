package search

import (
	"context"
	"fmt"
	rand "math/rand/v2"
	"time"

	"github.com/1broseidon/ketch/config"
)

// Random tries a freshly shuffled sequence of search providers and returns the
// first successful response. Unlike Multi it is sequential and never fuses
// results or calls providers after one succeeds.
type Random struct {
	backends []namedSearcher
	timeout  time.Duration
	perm     func(int) []int
}

// NewRandomFromConfig resolves the candidate providers using the same
// "all"/explicit-name rules as NewMultiFromConfig.
func NewRandomFromConfig(cfg *config.Config, names []string, searxngURL, degoogURL string) (*Random, error) {
	backends, err := resolveCandidates(cfg, names, searxngURL, degoogURL)
	if err != nil {
		return nil, err
	}
	if len(backends) == 0 {
		return nil, fmt.Errorf("no usable search backends for --random")
	}
	return &Random{backends: backends, timeout: multiBackendTimeout, perm: rand.Perm}, nil
}

// Names returns a copy of the resolved provider names.
func (r *Random) Names() []string {
	names := make([]string, len(r.backends))
	for i, backend := range r.backends {
		names[i] = backend.name
	}
	return names
}

// Search shuffles providers for this call, then tries each once with a 10s
// timeout. A response with zero results is still success. It falls back only
// after an error and returns the selected provider plus prior failures.
func (r *Random) Search(ctx context.Context, query string, limit int) ([]Result, string, []BackendError, error) {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = multiBackendTimeout
	}
	perm := r.perm
	if perm == nil {
		perm = rand.Perm
	}

	order := perm(len(r.backends))
	errs := make([]BackendError, 0, len(r.backends))
	for _, index := range order {
		if err := ctx.Err(); err != nil {
			return nil, "", errs, fmt.Errorf("random search cancelled after %d backend failures: %w", len(errs), err)
		}

		backend := r.backends[index]
		backendCtx, cancel := context.WithTimeout(ctx, timeout)
		results, err := backend.searcher.Search(backendCtx, query, limit)
		cancel()
		if err == nil {
			return results, backend.name, errs, nil
		}
		errs = append(errs, BackendError{Backend: backend.name, Err: err})
		if parentErr := ctx.Err(); parentErr != nil {
			return nil, "", errs, fmt.Errorf("random search cancelled after %d backend failures: %w", len(errs), parentErr)
		}
	}

	return nil, "", errs, fmt.Errorf("all %d backends failed (%s)", len(r.backends), formatBackendErrors(errs))
}
