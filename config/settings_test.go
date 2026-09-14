package config

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestProviderSettingsPreserveRequestIsolation(t *testing.T) {
	original := Defaults()
	keys := []string{"first", "second"}
	original.SetProvider("brave_api_keys", keys)
	keys[0] = "caller mutation"
	override := original
	override.SetProvider("searxng_url", "https://override.example")
	override.SetProvider("brave_api_keys", []string{"override"})
	read := original.Strings("brave_api_keys")
	read[0] = "reader mutation"
	if !slices.Equal(original.Strings("brave_api_keys"), []string{"first", "second"}) {
		t.Fatal("a request or caller mutated another request's credential pool")
	}
	if original.String("searxng_url") != "http://localhost:8081" {
		t.Fatal("per-request URL override mutated shared config")
	}
}

func TestProviderSettingsSchemaRejectsInvalidTypes(t *testing.T) {
	for _, input := range []string{`{"brave_api_key":42}`, `{"brave_api_keys":"key"}`, `{"context7_api_key":false}`, `{"sourcegraph_url":[]}`} {
		cfg := Defaults()
		if err := json.Unmarshal([]byte(input), &cfg); err == nil {
			t.Errorf("invalid provider value accepted: %s", input)
		}
	}
}

func TestProviderSettingsHaveUniqueKeysAndRedactedDiscovery(t *testing.T) {
	cfg := Defaults()
	seen := make(map[string]bool)
	for _, setting := range ProviderSettings() {
		for _, key := range []string{setting.Key, setting.Plural} {
			if key == "" {
				continue
			}
			if seen[key] {
				t.Fatalf("duplicate provider setting %q", key)
			}
			seen[key] = true
		}
		if setting.Secret {
			cfg.SetProvider(setting.Key, "never-disclose-this-fixture")
		}
	}
	data, err := json.Marshal(ProviderDiscovery(&cfg))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "never-disclose-this-fixture") {
		t.Fatal("provider discovery exposed a secret")
	}
}

func TestZeroConfigDoesNotTreatEmptyCoreFieldsAsProviderSettings(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"browser":"","spa_markers":[],"mcp_tools":[],"brave_api_key":"fixture"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"browser", "spa_markers", "mcp_tools"} {
		if _, exists := cfg.ProviderSettings[key]; exists {
			t.Errorf("core setting %q leaked into provider storage", key)
		}
	}
}
