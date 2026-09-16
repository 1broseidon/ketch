package search

import (
	"context"
	"fmt"
	"slices"
	"time"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// AutoBackend is the default backend id. It is not a provider: it owns no
// endpoint, credentials, or settings of its own, and dispatches to the
// registered providers that are usable for the current configuration.
const AutoBackend = "auto"

// autoTotalBudget bounds the whole fallback chain. Attempts are sequential, so
// without it a chain of slow providers would multiply the per-attempt timeout
// by its own length and leave a search hanging for a minute. On a healthy
// install the first provider answers in a second or two and this never binds;
// it exists to cap the bad day, and the error names every provider tried.
const autoTotalBudget = 30 * time.Second

// SelectingSearcher is a Searcher that dispatches to one of several providers.
// Callers use it to report which provider actually answered and which ones
// failed on the way there; a plain Searcher has nowhere to put that.
type SelectingSearcher interface {
	Searcher
	SearchSelect(ctx context.Context, query string, limit int) ([]Result, string, []BackendError, error)
}

// Auto tries each provider in the chain in order and returns the first
// successful response, so a zero-config install searches without an API key
// and a rate-limited provider falls through to the next one instead of
// failing the command. Unlike Multi it is sequential and never fuses results;
// unlike Random the order is fixed, so repeat queries are reproducible.
type Auto struct {
	backends []namedSearcher
	// timeout bounds a single provider attempt; budget bounds the whole chain.
	// Zero means the package defaults. NewAutoFromConfig sets the production
	// values; tests override them to keep fallback cases fast (neither is
	// surfaced as a flag).
	timeout time.Duration
	budget  time.Duration
}

// AutoChain returns the providers that may serve the auto backend, in the
// order they will be tried: providers the operator has deliberately configured
// first (their own SearXNG or Degoog instance, then any keyed API), then the
// keyless providers by descriptor rank. Providers whose preconditions are not
// met drop out, so the chain is always usable as returned.
func AutoChain(cfg *config.Config) []Provider {
	var chain []Provider
	for _, p := range providers {
		if p.InAuto(cfg) {
			chain = append(chain, p)
		}
	}
	slices.SortStableFunc(chain, func(a, b Provider) int {
		if promoted := boolCompare(autoPromoted(b, cfg), autoPromoted(a, cfg)); promoted != 0 {
			return promoted
		}
		return a.AutoRank - b.AutoRank
	})
	return chain
}

// autoPromoted reports a provider the operator deliberately set up — explicit
// credentials, or an instance URL that made it eligible in the first place.
// Deliberate configuration outranks the keyless defaults, so flipping the
// default backend to auto never downgrades an install that already has a key.
func autoPromoted(p Provider, cfg *config.Config) bool {
	return p.Configured(cfg) || (p.AutoEligible != nil && p.AutoEligible(cfg))
}

func boolCompare(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

// AutoChainNames returns the chain's provider ids in the order they are tried.
func AutoChainNames(cfg *config.Config) []string {
	chain := AutoChain(cfg)
	names := make([]string, len(chain))
	for i, p := range chain {
		names[i] = p.ID
	}
	return names
}

// NewAutoFromConfig builds the fallback chain for cfg. The SearXNG per-call
// override is applied to a copy, preserving the shared configuration.
func NewAutoFromConfig(cfg *config.Config, searxngURL string) (*Auto, error) {
	c := *cfg
	if searxngURL != "" {
		c.SetProvider("searxng_url", searxngURL)
	}

	chain := AutoChain(&c)
	backends := make([]namedSearcher, 0, len(chain))
	for _, p := range chain {
		searcher, err := p.New(&c)
		if err != nil {
			continue
		}
		backends = append(backends, namedSearcher{name: p.ID, searcher: searcher})
	}
	if len(backends) == 0 {
		return nil, fmt.Errorf("no usable search backends for %q", AutoBackend)
	}
	return &Auto{backends: backends, timeout: multiBackendTimeout, budget: autoTotalBudget}, nil
}

// Names returns a copy of the resolved provider names in fallback order.
func (a *Auto) Names() []string {
	names := make([]string, len(a.backends))
	for i, backend := range a.backends {
		names[i] = backend.name
	}
	return names
}

// Search returns the first successful provider's results, discarding which
// provider served them. Callers that want to report the selection use
// SearchSelect; this method exists so Auto satisfies Searcher and drops into
// every existing single-backend call site unchanged.
func (a *Auto) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	results, _, _, err := a.SearchSelect(ctx, query, limit)
	return results, err
}

// SearchSelect tries each provider in chain order, each bounded by its own
// timeout, and returns the first success along with the provider that served
// it and the failures that preceded it. A response with zero results is still
// a success — falling through on an empty result set would turn every
// legitimately-unmatched query into a full sweep of every provider.
func (a *Auto) SearchSelect(ctx context.Context, query string, limit int) ([]Result, string, []BackendError, error) {
	timeout := a.timeout
	if timeout <= 0 {
		timeout = multiBackendTimeout
	}
	budget := a.budget
	if budget <= 0 {
		budget = autoTotalBudget
	}

	chainCtx, cancelChain := context.WithTimeout(ctx, budget)
	defer cancelChain()

	errs := make([]BackendError, 0, len(a.backends))
	for _, backend := range a.backends {
		if err := a.stop(ctx, chainCtx, errs); err != nil {
			return nil, "", errs, err
		}

		backendCtx, cancel := context.WithTimeout(chainCtx, timeout)
		results, err := backend.searcher.Search(backendCtx, query, limit)
		cancel()
		if err == nil {
			return results, backend.name, errs, nil
		}
		errs = append(errs, BackendError{Backend: backend.name, Err: err})
		if stopErr := a.stop(ctx, chainCtx, errs); stopErr != nil {
			return nil, "", errs, stopErr
		}
	}

	return nil, "", errs, fmt.Errorf("all %d backends failed (%s)", len(a.backends), formatBackendErrors(errs))
}

// stop reports why the chain must not try another provider, distinguishing the
// caller cancelling (which the CLI maps to the cancelled exit code) from the
// chain spending its own budget (an upstream failure). Nil means keep going.
func (a *Auto) stop(ctx, chainCtx context.Context, errs []BackendError) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("auto search cancelled after %d backend failures: %w", len(errs), err)
	}
	if chainCtx.Err() == nil {
		return nil
	}
	if len(errs) == 0 {
		return fmt.Errorf("auto search exceeded its %s budget before any backend answered", a.budgetOrDefault())
	}
	return fmt.Errorf("auto search exceeded its %s budget after %d backend failures (%s)",
		a.budgetOrDefault(), len(errs), formatBackendErrors(errs))
}

func (a *Auto) budgetOrDefault() time.Duration {
	if a.budget <= 0 {
		return autoTotalBudget
	}
	return a.budget
}
