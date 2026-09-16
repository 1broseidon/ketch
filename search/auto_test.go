package search

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	config "github.com/1broseidon/ketch/internal/configbase"
)

func newTestAuto(budget time.Duration, backends ...namedSearcher) *Auto {
	return &Auto{backends: backends, timeout: 50 * time.Millisecond, budget: budget}
}

// A zero-config install must resolve a chain of keyless providers and nothing
// else: the keyed providers have no credentials, Degoog has no instance, and
// SearXNG's built-in localhost default is not a service anybody configured.
func TestAutoChainDefaultsToKeylessProviders(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()

	want := []string{"parallel", "exa", "keenable", "youcom", "firecrawl", "ddg"}
	if got := AutoChainNames(&cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("AutoChainNames() = %v, want %v", got, want)
	}
}

// Flipping the default to auto must not downgrade an install that already had
// a key: a configured provider outranks the keyless fallbacks, so a user who
// set brave_api_key and never touched `backend` keeps searching Brave.
func TestAutoChainPromotesConfiguredProviders(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.SetProvider("brave_api_key", "configured")

	got := AutoChainNames(&cfg)
	if len(got) == 0 || got[0] != "brave" {
		t.Fatalf("AutoChainNames() = %v, want brave first", got)
	}
	// Promotion reorders; it must not drop the keyless fallbacks behind it.
	if !reflect.DeepEqual(got[1:], []string{"parallel", "exa", "keenable", "youcom", "firecrawl", "ddg"}) {
		t.Fatalf("fallbacks after brave = %v", got[1:])
	}
}

// A keyless provider the operator gave a key to is a deliberate choice too, so
// it is promoted on the same rule rather than staying at its keyless rank.
func TestAutoChainPromotesKeyedKeylessProvider(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.SetProvider("firecrawl_api_key", "configured")

	got := AutoChainNames(&cfg)
	if len(got) == 0 || got[0] != "firecrawl" {
		t.Fatalf("AutoChainNames() = %v, want firecrawl promoted to first", got)
	}
}

// An operator's own instance outranks every hosted API, keyed or not — it is
// the one backend with no third-party rate limit and no shared quota.
func TestAutoChainPrefersSelfHostedInstances(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.SetProvider("searxng_url", "http://searx.internal:8888")
	cfg.SetProvider("degoog_url", "http://degoog.internal:4444")
	cfg.SetProvider("brave_api_key", "configured")

	want := []string{"searxng", "degoog", "brave"}
	got := AutoChainNames(&cfg)
	if len(got) < 3 || !reflect.DeepEqual(got[:3], want) {
		t.Fatalf("AutoChainNames() = %v, want %v first", got, want)
	}
}

// `-b searxng` still works against the localhost default, but the chain must
// not attempt an instance nobody configured: on a fresh install that is a
// guaranteed failure and a warning line on every single search.
func TestAutoChainExcludesUnconfiguredSearxng(t *testing.T) {
	t.Parallel()
	for _, url := range []string{"", DefaultSearxngURL} {
		cfg := config.Defaults()
		if url != "" {
			cfg.SetProvider("searxng_url", url)
		}
		for _, name := range AutoChainNames(&cfg) {
			if name == "searxng" {
				t.Fatalf("searxng_url=%q put searxng in the chain", url)
			}
		}
	}
}

func TestAutoSearchReportsServingBackend(t *testing.T) {
	t.Parallel()
	want := []Result{{Title: "served", URL: docURL("served")}}
	auto := newTestAuto(time.Second,
		namedSearcher{name: "first", searcher: &fakeSearcher{results: want}},
		namedSearcher{name: "second", searcher: &fakeSearcher{err: errors.New("must not run")}},
	)

	results, served, failures, err := auto.SearchSelect(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if served != "first" {
		t.Fatalf("served = %q, want first", served)
	}
	if !reflect.DeepEqual(results, want) || len(failures) != 0 {
		t.Fatalf("results=%v failures=%v", results, failures)
	}
}

// The whole point of the chain: a rate-limited provider must not fail the
// command when a working one sits behind it.
func TestAutoSearchFallsBackOnError(t *testing.T) {
	t.Parallel()
	want := []Result{{Title: "second", URL: docURL("second")}}
	auto := newTestAuto(time.Second,
		namedSearcher{name: "limited", searcher: &fakeSearcher{err: errors.New("rate limited")}},
		namedSearcher{name: "healthy", searcher: &fakeSearcher{results: want}},
	)

	results, served, failures, err := auto.SearchSelect(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if served != "healthy" || !reflect.DeepEqual(results, want) {
		t.Fatalf("served=%q results=%v", served, results)
	}
	if len(failures) != 1 || failures[0].Backend != "limited" {
		t.Fatalf("failures = %+v, want the limited provider reported", failures)
	}
}

// Zero results is an answer, not a failure. Falling through on empty would
// turn every legitimately-unmatched query into a sweep of every provider —
// which is exactly why a provider must never report an error as empty.
func TestAutoSearchStopsOnEmptyResults(t *testing.T) {
	t.Parallel()
	auto := newTestAuto(time.Second,
		namedSearcher{name: "empty", searcher: &fakeSearcher{results: nil}},
		namedSearcher{name: "next", searcher: &fakeSearcher{err: errors.New("must not run")}},
	)

	results, served, failures, err := auto.SearchSelect(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if served != "empty" || len(results) != 0 || len(failures) != 0 {
		t.Fatalf("served=%q results=%v failures=%v", served, results, failures)
	}
}

func TestAutoSearchAllBackendsFail(t *testing.T) {
	t.Parallel()
	auto := newTestAuto(time.Second,
		namedSearcher{name: "one", searcher: &fakeSearcher{err: errors.New("down")}},
		namedSearcher{name: "two", searcher: &fakeSearcher{err: errors.New("also down")}},
	)

	_, _, failures, err := auto.SearchSelect(context.Background(), "q", 5)
	if err == nil {
		t.Fatal("expected an error when every backend fails")
	}
	if len(failures) != 2 {
		t.Fatalf("failures = %+v, want both reported", failures)
	}
	// The error has to name what was tried; "search failed" alone leaves an
	// operator with nowhere to start.
	for _, want := range []string{"all 2 backends failed", "one", "two"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

// Attempts are sequential, so without a chain-wide budget a row of slow
// providers multiplies the per-attempt timeout by the chain's length.
func TestAutoSearchStopsAtTotalBudget(t *testing.T) {
	t.Parallel()
	slow := func(name string) namedSearcher {
		return namedSearcher{name: name, searcher: &fakeSearcher{delay: time.Hour}}
	}
	auto := newTestAuto(120*time.Millisecond, slow("one"), slow("two"), slow("three"), slow("four"), slow("five"))

	start := time.Now()
	_, _, failures, err := auto.SearchSelect(context.Background(), "q", 5)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the budget is exhausted")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Errorf("error = %q, want it to mention the budget", err)
	}
	// Five 50ms attempts would be 250ms; the 120ms budget must cut it short.
	if elapsed > 200*time.Millisecond {
		t.Errorf("elapsed = %v, want the budget to stop the chain early", elapsed)
	}
	if len(failures) == 0 || len(failures) == 5 {
		t.Errorf("failures = %d, want a partial chain", len(failures))
	}
}

// A cancelled caller is exit code 6, an exhausted budget is an upstream
// failure — the CLI branches on the difference, so they must not collapse.
func TestAutoSearchDistinguishesCancellationFromBudget(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	auto := newTestAuto(time.Second, namedSearcher{name: "one", searcher: &fakeSearcher{results: nil}})
	_, _, _, err := auto.SearchSelect(ctx, "q", 5)
	if err == nil {
		t.Fatal("expected an error for a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want it to wrap context.Canceled", err)
	}
	if strings.Contains(err.Error(), "budget") {
		t.Errorf("error = %q, want cancellation reported as cancellation", err)
	}
}

// NewFromConfig is the single constructor the CLI and MCP both go through, so
// the chain has to arrive as a plain Searcher and be recognisable as one that
// can report which provider served.
func TestNewFromConfigBuildsAutoChain(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()

	searcher, err := NewFromConfig(&cfg, AutoBackend, "")
	if err != nil {
		t.Fatal(err)
	}
	auto, ok := searcher.(*Auto)
	if !ok {
		t.Fatalf("searcher = %T, want *Auto", searcher)
	}
	if _, ok := searcher.(SelectingSearcher); !ok {
		t.Fatal("*Auto must implement SelectingSearcher so callers can report the serving provider")
	}
	if got := auto.Names(); !reflect.DeepEqual(got, AutoChainNames(&cfg)) {
		t.Fatalf("Names() = %v, want the resolved chain %v", got, AutoChainNames(&cfg))
	}
}

// The per-call SearXNG override has to reach chain resolution, not just
// single-provider construction.
func TestNewFromConfigAutoAppliesSearxngOverride(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()

	searcher, err := NewFromConfig(&cfg, AutoBackend, "http://searx.internal:8888")
	if err != nil {
		t.Fatal(err)
	}
	names := searcher.(*Auto).Names()
	if len(names) == 0 || names[0] != "searxng" {
		t.Fatalf("Names() = %v, want the overridden searxng instance first", names)
	}
	// The override must not leak into the caller's shared configuration.
	if got := cfg.String("searxng_url"); got == "http://searx.internal:8888" {
		t.Fatal("per-call override mutated the shared config")
	}
}

// Nesting a chain inside a fan-out would hide which provider answered and
// double up on backends the caller already listed.
func TestAutoIsNotAFederationMember(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()

	if _, err := NewMultiFromConfig(&cfg, []string{AutoBackend, "ddg"}, ""); err == nil {
		t.Fatal("expected --multi to reject auto")
	}
	if _, err := NewRandomFromConfig(&cfg, []string{AutoBackend}, ""); err == nil {
		t.Fatal("expected --random to reject auto")
	}
	// "all" resolves from the provider registry, which auto is not part of.
	for _, name := range AvailableBackends() {
		if name == AutoBackend {
			t.Fatal("auto leaked into AvailableBackends, so --multi=all would nest chains")
		}
	}
}

func TestSelectableBackendsLeadsWithAuto(t *testing.T) {
	t.Parallel()
	got := SelectableBackends()
	if len(got) != len(AvailableBackends())+1 || got[0] != AutoBackend {
		t.Fatalf("SelectableBackends() = %v, want auto followed by every provider", got)
	}
	if !IsBackend(AutoBackend) || !IsBackend("brave") || IsBackend("nope") {
		t.Fatal("IsBackend must accept auto and every provider, and nothing else")
	}
}
