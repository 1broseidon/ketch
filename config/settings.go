package config

import (
	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/search"
)

// Setting is a provider-owned operator setting.
type Setting = configbase.Setting

// ProviderSettings returns a fresh schema assembled from the three registries.
func ProviderSettings() []Setting {
	var settings []Setting
	for _, p := range search.Providers() {
		settings = append(settings, p.Settings...)
	}
	for _, p := range code.Providers() {
		settings = append(settings, p.Settings...)
	}
	for _, p := range docs.Providers() {
		if !p.Hidden {
			settings = append(settings, p.Settings...)
		}
	}
	return settings
}

// ProviderDiscovery returns the ordered, redacted provider fields used by ketch config.
func ProviderDiscovery(c *Config) []configbase.Field {
	var fields []configbase.Field
	for _, s := range ProviderSettings() {
		fields = append(fields, s.Discovery(c)...)
	}
	return fields
}
