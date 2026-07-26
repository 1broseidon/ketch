package search

import (
	"testing"

	"github.com/1broseidon/ketch/config"
)

func TestNewFromConfigBraveKeyCompatibility(t *testing.T) {
	t.Run("singular only", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.BraveAPIKey = "legacy"
		searcher, err := NewFromConfig(&cfg, "brave", "", "")
		if err != nil {
			t.Fatal(err)
		}
		backend, ok := searcher.(*Brave)
		if !ok || backend.keys.size() != 1 || backend.keys.keys[0] != "legacy" {
			t.Fatalf("backend = %#v", searcher)
		}
	})

	t.Run("plural only", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.BraveAPIKeys = []string{"one", "two"}
		searcher, err := NewFromConfig(&cfg, "brave", "", "")
		if err != nil {
			t.Fatal(err)
		}
		backend, ok := searcher.(*Brave)
		if !ok || backend.keys.size() != 2 {
			t.Fatalf("backend = %#v", searcher)
		}
	})

	t.Run("neither", func(t *testing.T) {
		cfg := config.Defaults()
		if _, err := NewFromConfig(&cfg, "brave", "", ""); err == nil {
			t.Fatal("expected a missing-key precondition error")
		}
	})
}

func TestNewFromConfigBuildsEveryEffectiveKeyPool(t *testing.T) {
	cfg := config.Defaults()
	cfg.BraveAPIKey, cfg.BraveAPIKeys = "brave-legacy", []string{"brave-new"}
	cfg.ExaAPIKey, cfg.ExaAPIKeys = "exa-legacy", []string{"exa-new"}
	cfg.FirecrawlAPIKey, cfg.FirecrawlAPIKeys = "firecrawl-legacy", []string{"firecrawl-new"}
	cfg.KeenableAPIKey, cfg.KeenableAPIKeys = "keenable-legacy", []string{"keenable-new"}

	for _, backend := range []string{"brave", "exa", "firecrawl", "keenable"} {
		searcher, err := NewFromConfig(&cfg, backend, "", "")
		if err != nil {
			t.Fatalf("%s: %v", backend, err)
		}
		var size int
		switch candidate := searcher.(type) {
		case *Brave:
			size = candidate.keys.size()
		case *EXA:
			size = candidate.keys.size()
		case *Firecrawl:
			size = candidate.keys.size()
		case *Keenable:
			size = candidate.keys.size()
		default:
			t.Fatalf("%s: unexpected searcher %T", backend, searcher)
		}
		if size != 2 {
			t.Errorf("%s pool size = %d, want 2", backend, size)
		}
	}
}

func TestExportedBackendConstructorsKeepSingleKeyCompatibility(t *testing.T) {
	exaKey := "exa"
	keenableKey := "keenable"
	tests := []struct {
		name string
		size int
	}{
		{name: "brave", size: NewBrave("brave").keys.size()},
		{name: "exa", size: NewEXA(&exaKey).keys.size()},
		{name: "firecrawl", size: NewFirecrawl("firecrawl").keys.size()},
		{name: "keenable", size: NewKeenable(&keenableKey).keys.size()},
	}
	for _, tc := range tests {
		if tc.size != 1 {
			t.Errorf("%s constructor pool size = %d, want 1", tc.name, tc.size)
		}
	}
}
