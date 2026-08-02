package config

import "testing"

func TestEffectiveFirecrawlURL(t *testing.T) {
	d := Defaults()
	if got := d.EffectiveFirecrawlURL(); got != DefaultFirecrawlURL {
		t.Fatalf("default = %q, want %q", got, DefaultFirecrawlURL)
	}
	if !d.IsDefaultFirecrawlURL() {
		t.Fatal("Defaults should report hosted Firecrawl URL")
	}

	d.FirecrawlURL = "http://localhost:3002/"
	if got := d.EffectiveFirecrawlURL(); got != "http://localhost:3002" {
		t.Fatalf("trimmed = %q", got)
	}
	if d.IsDefaultFirecrawlURL() {
		t.Fatal("custom URL should not be treated as hosted default")
	}

	d.FirecrawlURL = "  "
	if got := d.EffectiveFirecrawlURL(); got != DefaultFirecrawlURL {
		t.Fatalf("blank falls back = %q", got)
	}

	// Pasting the full endpoint must reduce to its base, not double the path.
	d.FirecrawlURL = "http://localhost:3002/v2/search"
	if got := d.EffectiveFirecrawlURL(); got != "http://localhost:3002" {
		t.Fatalf("endpoint reduced to base = %q", got)
	}

	// The hosted endpoint must still count as hosted, so it keeps requiring a key.
	d.FirecrawlURL = DefaultFirecrawlURL + "/v2/search"
	if !d.IsDefaultFirecrawlURL() {
		t.Fatalf("hosted endpoint should report as default, got %q", d.EffectiveFirecrawlURL())
	}
}

func TestFirecrawlSearchURL(t *testing.T) {
	hosted := DefaultFirecrawlURL + "/v2/search"
	tests := []struct {
		base string
		want string
	}{
		{"", hosted},
		{DefaultFirecrawlURL, hosted},
		{DefaultFirecrawlURL + "/", hosted},
		{hosted, hosted},
		{"http://localhost:3002", "http://localhost:3002/v2/search"},
		{"  http://fc.local/  ", "http://fc.local/v2/search"},
		{"http://localhost:3002/v2/search", "http://localhost:3002/v2/search"},
		{"http://localhost:3002/v2/search/", "http://localhost:3002/v2/search"},
	}
	for _, tc := range tests {
		if got := FirecrawlSearchURL(tc.base); got != tc.want {
			t.Errorf("FirecrawlSearchURL(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}
