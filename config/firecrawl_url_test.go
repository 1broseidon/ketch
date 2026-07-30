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
}
