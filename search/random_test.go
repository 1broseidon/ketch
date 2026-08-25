package search

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/config"
)

func newTestRandom(order []int, backends ...namedSearcher) *Random {
	return &Random{
		backends: backends,
		timeout:  time.Second,
		perm: func(int) []int {
			return append([]int(nil), order...)
		},
	}
}

func TestRandomSearchWinnerAndSelectedBackend(t *testing.T) {
	want := []Result{{Title: "winner", URL: docURL("winner")}}
	random := newTestRandom([]int{1, 0},
		namedSearcher{name: "first", searcher: &fakeSearcher{err: errors.New("must not run")}},
		namedSearcher{name: "winner", searcher: &fakeSearcher{results: want}},
	)

	results, selected, failures, err := random.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if selected != "winner" {
		t.Fatalf("selected = %q, want winner", selected)
	}
	if !reflect.DeepEqual(results, want) || len(failures) != 0 {
		t.Fatalf("results=%v failures=%v", results, failures)
	}
}

func TestRandomSearchFallsBackOnError(t *testing.T) {
	random := newTestRandom([]int{0, 1},
		namedSearcher{name: "broken", searcher: &fakeSearcher{err: errors.New("down")}},
		namedSearcher{name: "empty-success", searcher: &fakeSearcher{results: nil}},
	)

	results, selected, failures, err := random.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if results != nil || selected != "empty-success" {
		t.Fatalf("results=%v selected=%q", results, selected)
	}
	if len(failures) != 1 || failures[0].Backend != "broken" {
		t.Fatalf("failures = %+v", failures)
	}
}

func TestRandomSearchFallsBackAfterBackendTimeout(t *testing.T) {
	random := newTestRandom([]int{0, 1},
		namedSearcher{name: "slow", searcher: &fakeSearcher{delay: 500 * time.Millisecond}},
		namedSearcher{name: "winner", searcher: &fakeSearcher{results: []Result{{URL: docURL("winner")}}}},
	)
	random.timeout = 20 * time.Millisecond

	start := time.Now()
	results, selected, failures, err := random.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("search took %v; slow provider should time out at ~20ms", elapsed)
	}
	if selected != "winner" || len(results) != 1 {
		t.Fatalf("selected=%q results=%v", selected, results)
	}
	if len(failures) != 1 || failures[0].Backend != "slow" || !errors.Is(failures[0].Err, context.DeadlineExceeded) {
		t.Fatalf("failures = %+v, want slow deadline failure", failures)
	}
}

func TestRandomSearchAllFail(t *testing.T) {
	random := newTestRandom([]int{1, 0},
		namedSearcher{name: "one", searcher: &fakeSearcher{err: errors.New("one down")}},
		namedSearcher{name: "two", searcher: &fakeSearcher{err: errors.New("two down")}},
	)

	results, selected, failures, err := random.Search(context.Background(), "q", 5)
	if err == nil {
		t.Fatal("expected aggregate error")
	}
	if results != nil || selected != "" || len(failures) != 2 {
		t.Fatalf("results=%v selected=%q failures=%v", results, selected, failures)
	}
	if message := err.Error(); !strings.Contains(message, "all 2 backends failed") || !strings.Contains(message, "one") || !strings.Contains(message, "two") {
		t.Fatalf("aggregate error = %q", message)
	}
}

func TestRandomSearchPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	random := newTestRandom([]int{0}, namedSearcher{name: "one", searcher: &fakeSearcher{}})

	_, _, failures, err := random.Search(ctx, "q", 5)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(failures) != 0 {
		t.Fatalf("cancelled search attempted providers: %v", failures)
	}
}

func TestNewRandomFromConfigAllUsesEveryUsableBackend(t *testing.T) {
	cfg := config.Defaults()
	random, err := NewRandomFromConfig(&cfg, []string{"all"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ddg", "searxng", "degoog", "exa", "keenable", "parallel"}
	if got := random.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved backends = %v, want %v", got, want)
	}
}

func TestRandomSearchShufflesEveryCall(t *testing.T) {
	calls := 0
	random := &Random{
		backends: []namedSearcher{{name: "one", searcher: &fakeSearcher{}}},
		timeout:  time.Second,
		perm: func(n int) []int {
			calls++
			return []int{0}
		},
	}
	for range 2 {
		if _, _, _, err := random.Search(context.Background(), "q", 5); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("permutation source called %d times, want 2", calls)
	}
}
