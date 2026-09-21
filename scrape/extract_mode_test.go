package scrape

import (
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/extract"
)

// extract_mode reaches the scraper's extractor, and a non-default mode
// namespaces the page cache: the two modes produce different markdown for
// the same fetch, while the default keeps existing entries valid.
func TestNewFromConfigExtractMode(t *testing.T) {
	u := "http://127.0.0.1:9999/page"

	t.Run("unset is clean and keeps the bare key", func(t *testing.T) {
		s := scraperFromConfig(t, &config.Config{})
		if s.extractor.Mode() != extract.ModeClean || s.CacheKey(u) != u {
			t.Fatalf("mode = %q, key = %q", s.extractor.Mode(), s.CacheKey(u))
		}
	})

	t.Run("clean spelled out keeps the bare key", func(t *testing.T) {
		s := scraperFromConfig(t, &config.Config{ExtractMode: "clean"})
		if s.CacheKey(u) != u {
			t.Fatalf("key = %q, want %q", s.CacheKey(u), u)
		}
	})

	t.Run("complete diverges by name", func(t *testing.T) {
		s := scraperFromConfig(t, &config.Config{ExtractMode: "Complete"})
		key := s.CacheKey(u)
		if s.extractor.Mode() != extract.ModeComplete || !strings.HasSuffix(key, "\x00extract:complete") {
			t.Fatalf("mode = %q, key = %q", s.extractor.Mode(), key)
		}
	})

	t.Run("unknown fails loud", func(t *testing.T) {
		_, err := NewFromConfig(&config.Config{ExtractMode: "fast"})
		if err == nil || !strings.Contains(err.Error(), "invalid extract_mode") {
			t.Fatalf("err = %v", err)
		}
	})
}
