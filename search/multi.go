package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/config"
)

const (
	// rrfK is the Reciprocal Rank Fusion constant. k=60 is the published
	// default (Cormack, Clarke & Büttcher, SIGIR 2009): it dampens the gap
	// between rank 1 and rank 2 (1/61 vs 1/62) so a single engine's top hit
	// cannot outvote two engines' mid-list agreement. Not a tuning knob.
	rrfK = 60

	// multiBackendTimeout bounds each backend's live query under federated
	// search. Fan-out is parallel, so total wall clock ≈ the slowest backend,
	// not the sum. 10s clears DDG's worst path (3 attempts + ~1s of sleeps)
	// and exa's livecrawl fallback while bounding the whole call.
	multiBackendTimeout = 10 * time.Second

	// fetchFloor / fetchCap clamp per-backend fetch depth. The floor gives
	// fusion overlap evidence below the final cutoff; the cap is Brave's hard
	// API max (deeper tails add noise, not signal).
	fetchFloor = 10
	fetchCap   = 20

	// scoreEpsilon treats near-equal RRF scores as tied so the explicit
	// tiebreak chain (backend count, best rank, canonical URL) decides order.
	scoreEpsilon = 1e-9
)

// BackendError records a single backend's failure during multi or random
// provider search. The caller can report partial/fallback failures without
// hiding which providers were attempted.
type BackendError struct {
	Backend string
	Err     error
}

// namedSearcher pairs a resolved backend name with its constructed Searcher,
// preserving the caller's requested order.
type namedSearcher struct {
	name     string
	searcher Searcher
}

// Multi fans a query out across several backends and fuses their ranked
// results with Reciprocal Rank Fusion. Construct it with NewMultiFromConfig.
type Multi struct {
	backends []namedSearcher
	// timeout bounds each backend's live query; zero means multiBackendTimeout.
	// NewMultiFromConfig sets the production default; tests override it to keep
	// timeout/degradation cases fast (the value is never surfaced as a flag).
	timeout time.Duration
}

// NewMultiFromConfig resolves a federated backend set. names is either the
// single sentinel []string{"all"} (every usable backend, in AvailableBackends
// order) or an explicit ordered list. Resolution reuses NewFromConfig, so
// "usable" means exactly what it means everywhere else in ketch (config/key
// presence). For the "all" set, unconfigured backends are silently skipped;
// for an explicit list, an unusable name fails loudly (unknown → wraps
// ErrUnknownBackend; unconfigured → the constructor's precondition error), so
// callers can classify with backendErr/backendErrf just like NewFromConfig.
func NewMultiFromConfig(cfg *config.Config, names []string, searxngURL, degoogURL string) (*Multi, error) {
	backends, err := resolveCandidates(cfg, names, searxngURL, degoogURL)
	if err != nil {
		return nil, err
	}
	if len(backends) == 0 {
		return nil, fmt.Errorf("no usable search backends for --multi")
	}
	return &Multi{backends: backends, timeout: multiBackendTimeout}, nil
}

// resolveCandidates constructs the usable providers shared by multi and
// random search. The "all" sentinel skips providers whose preconditions are
// not met; explicit names surface unknown/unconfigured errors.
func resolveCandidates(cfg *config.Config, names []string, searxngURL, degoogURL string) ([]namedSearcher, error) {
	all := len(names) == 1 && names[0] == "all"
	var candidates []string
	if all {
		candidates = config.AvailableBackends()
	} else {
		candidates = uniqueBackendNames(names)
	}

	backends := make([]namedSearcher, 0, len(candidates))
	for _, name := range candidates {
		searcher, err := NewFromConfig(cfg, name, searxngURL, degoogURL)
		if err != nil {
			if all {
				continue
			}
			return nil, err
		}
		backends = append(backends, namedSearcher{name: name, searcher: searcher})
	}
	return backends, nil
}

func uniqueBackendNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
	}
	return unique
}

// Names returns the resolved backend names in fan-out order.
func (m *Multi) Names() []string {
	names := make([]string, len(m.backends))
	for i, b := range m.backends {
		names[i] = b.name
	}
	return names
}

// Search fans out to every resolved backend concurrently (each bounded by
// multiBackendTimeout), fuses the successful lists with RRF (k=60), dedups by
// canonical URL, and cuts the fused list to limit. errs carries per-backend
// failures (timeouts and errors); a backend that succeeds with zero results is
// a success, not a failure. The returned error is non-nil only when every
// backend failed.
func (m *Multi) Search(ctx context.Context, query string, limit int) ([]Result, []BackendError, error) {
	fetchLimit := limit
	if fetchLimit < fetchFloor {
		fetchLimit = fetchFloor
	}
	if fetchLimit > fetchCap {
		fetchLimit = fetchCap
	}

	timeout := m.timeout
	if timeout <= 0 {
		timeout = multiBackendTimeout
	}

	type outcome struct {
		results []Result
		err     error
	}
	outcomes := make([]outcome, len(m.backends))

	var wg sync.WaitGroup
	for i, b := range m.backends {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			res, err := b.searcher.Search(bctx, query, fetchLimit)
			outcomes[i] = outcome{results: res, err: err}
		}()
	}
	wg.Wait()

	var lists []backendResults
	var errs []BackendError
	for i, b := range m.backends {
		if outcomes[i].err != nil {
			errs = append(errs, BackendError{Backend: b.name, Err: outcomes[i].err})
			continue
		}
		lists = append(lists, backendResults{name: b.name, results: outcomes[i].results})
	}

	if len(lists) == 0 {
		// If the parent context was cancelled/expired, wrap it so the surfaces
		// map to [cancelled]/exit 6; otherwise report the aggregated failures.
		if cerr := ctx.Err(); cerr != nil {
			return nil, errs, fmt.Errorf("all %d backends failed: %w", len(m.backends), cerr)
		}
		return nil, errs, fmt.Errorf("all %d backends failed (%s)", len(m.backends), formatBackendErrors(errs))
	}

	return fuse(lists, limit), errs, nil
}

// formatBackendErrors renders "brave: msg; ddg: msg" in fan-out order.
func formatBackendErrors(errs []BackendError) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = fmt.Sprintf("%s: %v", e.Backend, e.Err)
	}
	return strings.Join(parts, "; ")
}

// backendResults is one backend's ranked list (rank = index+1) feeding fusion.
type backendResults struct {
	name    string
	results []Result
}

// fusedEntry is one backend's contribution to a fused document.
type fusedEntry struct {
	backendIndex int
	backendName  string
	rank         int
	result       Result
}

// scored is a fused document with the fields the total order sorts on.
type scored struct {
	result      Result
	key         string
	score       float64
	numBackends int
	bestRank    int
}

// fuse rank-fuses the backend lists and cuts to limit (limit <= 0 keeps all).
func fuse(lists []backendResults, limit int) []Result {
	ranked := rankFuse(lists)
	out := make([]Result, 0, len(ranked))
	for _, s := range ranked {
		out = append(out, s.result)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// rankFuse computes the fused, fully ordered result set. It is pure (no I/O),
// so the fusion arithmetic and tiebreak chain are unit-tested directly.
//
// score(d) = Σ over backends b that returned d of 1 / (k + rank_b(d)).
// A backend contributes at most one term per document (first occurrence wins),
// and an absent backend contributes exactly 0 (no imputation).
func rankFuse(lists []backendResults) []scored {
	type agg struct {
		key     string
		score   float64
		entries []fusedEntry
	}
	docs := make(map[string]*agg)
	var order []string // first-seen order, for a stable pre-sort baseline

	for bi, list := range lists {
		seen := make(map[string]bool, len(list.results))
		for ri, r := range list.results {
			key := canonicalURL(r.URL)
			if seen[key] {
				continue // one term per backend per document
			}
			seen[key] = true
			rank := ri + 1

			d := docs[key]
			if d == nil {
				d = &agg{key: key}
				docs[key] = d
				order = append(order, key)
			}
			d.score += 1.0 / float64(rrfK+rank)
			d.entries = append(d.entries, fusedEntry{
				backendIndex: bi,
				backendName:  list.name,
				rank:         rank,
				result:       r,
			})
		}
	}

	ranked := make([]scored, 0, len(docs))
	for _, key := range order {
		d := docs[key]

		// Order entries by (rank asc, backend order asc): entry[0] is the
		// representative, and this is the walk order for field fallbacks.
		sort.Slice(d.entries, func(a, b int) bool {
			if d.entries[a].rank != d.entries[b].rank {
				return d.entries[a].rank < d.entries[b].rank
			}
			return d.entries[a].backendIndex < d.entries[b].backendIndex
		})

		merged, numBackends := mergeEntries(d.entries)
		ranked = append(ranked, scored{
			result:      merged,
			key:         d.key,
			score:       d.score,
			numBackends: numBackends,
			bestRank:    d.entries[0].rank,
		})
	}

	sort.Slice(ranked, func(a, b int) bool { return lessScored(ranked[a], ranked[b]) })
	return ranked
}

// mergeEntries collapses one document's per-backend entries: the first entry
// (best rank, then backend order) is the representative supplying URL and
// Title; Description and Content fall back down the same walk; Backends is
// the sorted set of contributing engine names.
func mergeEntries(entries []fusedEntry) (Result, int) {
	merged := entries[0].result
	if merged.Description == "" {
		for _, e := range entries {
			if e.result.Description != "" {
				merged.Description = e.result.Description
				break
			}
		}
	}
	if merged.Content == "" {
		for _, e := range entries {
			if e.result.Content != "" {
				merged.Content = e.result.Content
				break
			}
		}
	}

	names := make([]string, 0, len(entries))
	nameSeen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !nameSeen[e.backendName] {
			nameSeen[e.backendName] = true
			names = append(names, e.backendName)
		}
	}
	sort.Strings(names)
	merged.Backends = names
	return merged, len(names)
}

// lessScored is the fusion tiebreak chain: RRF score desc, backend count
// desc, best (min) rank asc, canonical URL asc — a total order, so output is
// deterministic for any input.
func lessScored(x, y scored) bool {
	if math.Abs(x.score-y.score) > scoreEpsilon {
		return x.score > y.score
	}
	if x.numBackends != y.numBackends {
		return x.numBackends > y.numBackends
	}
	if x.bestRank != y.bestRank {
		return x.bestRank < y.bestRank
	}
	return x.key < y.key
}
