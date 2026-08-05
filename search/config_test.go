package search

import (
	"testing"

	"github.com/1broseidon/ketch/config"
)

func TestNewFromConfigBraveKeyCompatibility(t *testing.T) {
	t.Run("singular only", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.BraveAPIKey = "legacy"
		searcher, err := NewFromConfig(&cfg, "brave", "")
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
		searcher, err := NewFromConfig(&cfg, "brave", "")
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
		if _, err := NewFromConfig(&cfg, "brave", ""); err == nil {
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
	cfg.TavilyAPIKey, cfg.TavilyAPIKeys = "tavily-legacy", []string{"tavily-new"}
	cfg.SerpBaseAPIKey, cfg.SerpBaseAPIKeys = "serpbase-legacy", []string{"serpbase-new"}

	for _, backend := range []string{"brave", "exa", "firecrawl", "keenable", "tavily", "serpbase"} {
		searcher, err := NewFromConfig(&cfg, backend, "")
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
		case *Tavily:
			size = candidate.keys.size()
		case *SerpBase:
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
		{name: "tavily", size: NewTavily("tavily").keys.size()},
		{name: "serpbase", size: NewSerpBase("serpbase").keys.size()},
	}
	for _, tc := range tests {
		if tc.size != 1 {
			t.Errorf("%s constructor pool size = %d, want 1", tc.name, tc.size)
		}
	}
}

func TestNewFromConfigTavilyRequiresKey(t *testing.T) {
	cfg := config.Defaults()
	if _, err := NewFromConfig(&cfg, "tavily", ""); err == nil {
		t.Fatal("expected missing-key error for tavily")
	}
}

func TestNewFromConfigSerpBaseRequiresKey(t *testing.T) {
	cfg := config.Defaults()
	if _, err := NewFromConfig(&cfg, "serpbase", ""); err == nil {
		t.Fatal("expected missing-key error for serpbase")
	}
}

func TestNewFromConfigFirecrawlURL(t *testing.T) {
	t.Run("cloud requires key", func(t *testing.T) {
		cfg := config.Defaults()
		if _, err := NewFromConfig(&cfg, "firecrawl", ""); err == nil {
			t.Fatal("expected missing-key error for hosted Firecrawl")
		}
	})

	t.Run("self-hosted allows empty key", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.FirecrawlURL = "http://localhost:3002"
		searcher, err := NewFromConfig(&cfg, "firecrawl", "")
		if err != nil {
			t.Fatal(err)
		}
		backend, ok := searcher.(*Firecrawl)
		if !ok {
			t.Fatalf("unexpected type %T", searcher)
		}
		if backend.keys.size() != 0 {
			t.Fatalf("keys = %d, want 0", backend.keys.size())
		}
		if got := backend.endpoint; got != "http://localhost:3002/v2/search" {
			t.Fatalf("endpoint = %q, want local /v2/search", got)
		}
	})

	t.Run("custom base with key", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.FirecrawlURL = "https://fc.example.com/"
		cfg.FirecrawlAPIKey = "k"
		searcher, err := NewFromConfig(&cfg, "firecrawl", "")
		if err != nil {
			t.Fatal(err)
		}
		backend := searcher.(*Firecrawl)
		if got := backend.endpoint; got != "https://fc.example.com/v2/search" {
			t.Fatalf("endpoint = %q", got)
		}
	})
}
