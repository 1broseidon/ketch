package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/1broseidon/ketch/internal/appdirs"
	"github.com/1broseidon/ketch/internal/scrape"
)

// Store is the interface for cache storage backends.
// Default is bbolt; future backends (redis, etc.) implement this.
type Store interface {
	Get(key string) ([]byte, error)
	Put(key string, value []byte) error
	Stats() (entries int, sizeBytes int64)
	Clear() error
	Close() error
}

// Cache provides TTL-based page caching backed by a Store.
type Cache struct {
	store Store
	ttl   time.Duration
}

// LookupResult describes a cached page and whether it is still fresh.
type LookupResult struct {
	Page     *scrape.Page
	CachedAt time.Time
	Fresh    bool
}

type cacheEntry struct {
	CachedAt int64       `json:"t"`
	Page     scrape.Page `json:"p"`
}

// New creates a cache with the default bbolt backend.
// Returns nil if the cache cannot be initialized.
func New(ttl time.Duration) *Cache {
	path, err := DBPath()
	if err != nil {
		return nil
	}
	store, err := NewBBoltStore(path)
	if err != nil {
		return nil
	}
	return &Cache{store: store, ttl: ttl}
}

// NewReadOnly opens the cache for reading only.
// Use for stats/inspection when another process may hold the write lock.
func NewReadOnly() *Cache {
	path, err := DBPath()
	if err != nil {
		return nil
	}
	store, err := NewBBoltStoreReadOnly(path)
	if err != nil {
		return nil
	}
	return &Cache{store: store}
}

// DBPath returns the default cache database path.
func DBPath() (string, error) {
	base, err := appdirs.CacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(base, "cache.db"), nil
}

// Get looks up a cached page by URL. Returns nil if missing or expired.
func (c *Cache) Get(url string) *scrape.Page {
	lookup := c.Lookup(url)
	if lookup == nil || !lookup.Fresh {
		return nil
	}
	return lookup.Page
}

// Lookup returns a cached page regardless of freshness along with its cache metadata.
func (c *Cache) Lookup(url string) *LookupResult {
	if c == nil {
		return nil
	}
	data, err := c.store.Get(cacheKey(url))
	if err != nil {
		return nil
	}
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil
	}
	cachedAt := time.Unix(e.CachedAt, 0)
	pageCopy := e.Page
	return &LookupResult{
		Page:     &pageCopy,
		CachedAt: cachedAt,
		Fresh:    time.Since(cachedAt) <= c.ttl,
	}
}

// Put writes a page to the cache.
func (c *Cache) Put(url string, page *scrape.Page) {
	if c == nil {
		return
	}
	e := cacheEntry{
		CachedAt: time.Now().Unix(),
		Page:     *page,
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	_ = c.store.Put(cacheKey(url), data)
}

// Stats returns cache entry count and total size in bytes.
func (c *Cache) Stats() (entries int, bytes int64) {
	if c == nil {
		return 0, 0
	}
	return c.store.Stats()
}

// Clear removes all cached pages.
func (c *Cache) Clear() error {
	if c == nil {
		return nil
	}
	return c.store.Clear()
}

// Close releases cache resources.
func (c *Cache) Close() {
	if c == nil {
		return
	}
	_ = c.store.Close()
}

func cacheKey(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:8])
}
