package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/search"
)

func TestParallelBackendIsAppendedWithoutChangingDefault(t *testing.T) {
	if got := Defaults().Backend; got != "brave" {
		t.Fatalf("default backend = %q, want brave", got)
	}
	// The list is the registry's, in registry order; this facade must not keep
	// its own copy. Parallel is appended somewhere after the default.
	if got := AvailableBackends(); !reflect.DeepEqual(got, search.AvailableBackends()) {
		t.Fatalf("available backends = %v, want registry order %v", got, search.AvailableBackends())
	}
	if got := AvailableBackends(); got[0] != "brave" || !slices.Contains(got, "parallel") {
		t.Fatalf("available backends = %v, want brave first and parallel present", got)
	}
}

func TestMergeKeys(t *testing.T) {
	tests := []struct {
		name   string
		single string
		list   []string
		want   []string
	}{
		{name: "singular only", single: "one", want: []string{"one"}},
		{name: "plural only", list: []string{"one", "two"}, want: []string{"one", "two"}},
		{name: "singular first", single: "one", list: []string{"two", "three"}, want: []string{"one", "two", "three"}},
		{name: "deduplicates", single: "one", list: []string{"two", "one", "two"}, want: []string{"one", "two"}},
		{name: "trims and drops blanks", single: " one ", list: []string{"", "  ", " two "}, want: []string{"one", "two"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeKeys(tc.single, tc.list); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mergeKeys(%q, %v) = %v, want %v", tc.single, tc.list, got, tc.want)
			}
		})
	}
}

func TestEffectiveKeysReturnCopies(t *testing.T) {
	cfg := Config{ProviderSettings: map[string]any{"brave_api_key": "one", "brave_api_keys": []string{"two"}}}
	first := cfg.BraveKeys()
	first[0] = "changed"
	if got := cfg.BraveKeys(); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("BraveKeys was mutated through a returned slice: %v", got)
	}
}

func TestSaveEnforcesPrivateMode(t *testing.T) {
	testutil.SetIsolatedConfigHome(t)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Save(Config{ProviderSettings: map[string]any{"brave_api_key": "secret"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
}
