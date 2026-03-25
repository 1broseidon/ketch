package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/1broseidon/ketch/internal/scrape"
)

// Cache provides TTL-based page caching on the local filesystem.
type Cache struct {
	dir string
	ttl time.Duration
}

// New creates a cache at the platform-appropriate cache directory.
// Returns nil if the cache directory cannot be determined.
func New(ttl time.Duration) *Cache {
	dir, err := Dir()
	if err != nil {
		return nil
	}
	return &Cache{dir: dir, ttl: ttl}
}

// Dir returns the cache directory path.
func Dir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ketch", "pages"), nil
}

// Get looks up a cached page by URL. Returns nil if missing or expired.
func (c *Cache) Get(url string) *scrape.Page {
	if c == nil {
		return nil
	}

	path := c.path(url)
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	if time.Since(info.ModTime()) > c.ttl {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var page scrape.Page
	if err := json.Unmarshal(data, &page); err != nil {
		return nil
	}
	return &page
}

// Put writes a page to the cache.
func (c *Cache) Put(url string, page *scrape.Page) {
	if c == nil {
		return
	}

	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return
	}

	data, err := json.Marshal(page)
	if err != nil {
		return
	}

	_ = os.WriteFile(c.path(url), data, 0o644)
}

// Stats returns cache entry count and total size in bytes.
func (c *Cache) Stats() (entries int, bytes int64) {
	if c == nil {
		return 0, 0
	}

	entries, bytes = 0, 0
	es, err := os.ReadDir(c.dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range es {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		entries++
		bytes += info.Size()
	}
	return entries, bytes
}

// Clear removes all cached pages.
func (c *Cache) Clear() error {
	if c == nil {
		return nil
	}
	return os.RemoveAll(c.dir)
}

func (c *Cache) path(url string) string {
	h := sha256.Sum256([]byte(url))
	name := hex.EncodeToString(h[:8]) + ".json"
	return filepath.Join(c.dir, name)
}
