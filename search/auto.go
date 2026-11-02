package search

import (
	"context"
	"fmt"
	"slices"
	"strings"
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
// order they will be tried. With the operator's search_order set, that is
// exactly the listed providers in the listed order. Otherwise it is providers
// the operator has deliberately configured first (their own SearXNG or Degoog
// instance, then any keyed API), then the keyless providers by descriptor
// rank. Providers whose preconditions are not met drop out, so the chain is
// always usable as returned. An invalid search_order, possible only in a
// hand-edited config file, yields an empty chain; NewAutoFromConfig reports why.
func AutoChain(cfg *config.Config) []Provider {
	chain, _ := autoChain(cfg)
	return chain
}

func autoChain(cfg *config.Config) ([]Provider, error) {
	if len(cfg.SearchOrder) > 0 {
		return orderedChain(cfg)
	}
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
	return chain, nil
}

// orderedChain resolves the operator's search_order. The operator's order is
// the whole order: nothing is promoted or re-ranked, and a provider outside
// the default chain (a keyed one with AutoRank 0, or SearXNG on its localhost
// default) may be listed. Only a provider that cannot run at all — its key or
// instance is missing — drops out, as it would from the default chain.
func orderedChain(cfg *config.Config) ([]Provider, error) {
	ids, err := NormalizeSearchOrder(cfg.SearchOrder)
	if err != nil {
		return nil, fmt.Errorf("invalid search_order: %w", err)
	}
	chain := make([]Provider, 0, len(ids))
	for _, id := range ids {
		if p, ok := Lookup(id); ok && p.Usable(cfg) {
			chain = append(chain, p)
		}
	}
	return chain, nil
}

// NormalizeSearchOrder validates an operator-configured search_order: entries
// are trimmed and lowercased; blank, unknown, duplicate and "auto" entries are
// rejected (with the valid names in the error). Unlike an allowlist the
// operator's order is the point, so it is preserved. A nil or empty list means
// "no override" and returns nil.
func NormalizeSearchOrder(list []string) ([]string, error) {
	if len(list) == 0 {
		return nil, nil
	}
	valid := strings.Join(AvailableBackends(), ", ")
	out := make([]string, 0, len(list))
	for _, raw := range list {
		id := strings.ToLower(strings.TrimSpace(raw))
		switch _, known := Lookup(id); {
		case id == "":
			return nil, fmt.Errorf("backend name is blank (valid: %s)", valid)
		case id == AutoBackend:
			return nil, fmt.Errorf("%q is the chain itself, not a backend to list in it (valid: %s)", AutoBackend, valid)
		case slices.Contains(out, id):
			return nil, fmt.Errorf("duplicate backend %q (valid: %s)", id, valid)
		case !known:
			return nil, fmt.Errorf("unknown backend %q (valid: %s)", id, valid)
		}
		out = append(out, id)
	}
	return out, nil
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

	chain, err := autoChain(&c)
	if err != nil {
		return nil, fmt.Errorf("no usable search backends for %q: %w", AutoBackend, err)
	}
	backends := make([]namedSearcher, 0, len(chain))
	for _, p := range chain {
		searcher, err := p.New(&c)
		if err != nil {
			continue
		}
		backends = append(backends, namedSearcher{name: p.ID, searcher: searcher})
	}
	if len(backends) == 0 && len(c.SearchOrder) > 0 {
		return nil, fmt.Errorf("no usable search backends for %q: none of search_order (%s) is usable",
			AutoBackend, strings.Join(c.SearchOrder, ", "))
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
