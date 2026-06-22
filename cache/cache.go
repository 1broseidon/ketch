package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/1broseidon/ketch/scrape"
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

type cacheEntry struct {
	CachedAt int64       `json:"t"`
	Source   string      `json:"s,omitempty"`
	Page     scrape.Page `json:"p"`
	// RawHTML is the post-fetch (possibly browser-rendered) HTML that produced
	// Page. Stored as a sibling to Page — never on Page itself, which is the
	// extracted representation shared with every JSON/crawl consumer. Persisted
	// lazily: only --raw requests back-fill this, so the common markdown-only
	// path pays no 20 MiB-body cost. omitempty keeps pre-raw entries decodable.
	RawHTML string `json:"r,omitempty"`
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

// NewWithStore builds a cache over a caller-supplied Store. Used by tests to
// isolate the cache (temp bbolt path) from the user's real cache dir.
func NewWithStore(store Store, ttl time.Duration) *Cache {
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
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "ketch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache.db"), nil
}

// Get looks up a cached page by URL. Returns (nil, "") if missing or expired.
// The second return is the fetch source recorded at Put time (scrape.SourceHTTP
// or scrape.SourceBrowser); empty for entries written before source tracking.
func (c *Cache) Get(url string) (*scrape.Page, string) {
	if c == nil {
		return nil, ""
	}
	data, err := c.store.Get(cacheKey(url))
	if err != nil {
		return nil, ""
	}
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, ""
	}
	if time.Since(time.Unix(e.CachedAt, 0)) > c.ttl {
		return nil, ""
	}
	return &e.Page, e.Source
}

// Put writes a page to the cache with the fetch source that produced it.
func (c *Cache) Put(url string, page *scrape.Page, source string) {
	if c == nil {
		return
	}
	e := cacheEntry{
		CachedAt: time.Now().Unix(),
		Source:   source,
		Page:     *page,
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	_ = c.store.Put(cacheKey(url), data)
}

// GetRaw looks up a cached entry's raw HTML by URL. Returns (rawHTML, source,
// page). It is a hit only when the entry exists, is fresh, AND has non-empty
// RawHTML — a markdown-only entry must not poison a --raw request (serving
// empty HTML). On miss or empty raw, returns ("", "", nil) so the caller
// refetches and back-fills.
func (c *Cache) GetRaw(url string) (string, string, *scrape.Page) {
	if c == nil {
		return "", "", nil
	}
	data, err := c.store.Get(cacheKey(url))
	if err != nil {
		return "", "", nil
	}
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return "", "", nil
	}
	if time.Since(time.Unix(e.CachedAt, 0)) > c.ttl {
		return "", "", nil
	}
	if e.RawHTML == "" {
		return "", "", nil
	}
	return e.RawHTML, e.Source, &e.Page
}

// PutRaw writes a page plus its raw HTML to the cache with the fetch source.
// Used by --raw to persist both representations from a single fetch. The
// markdown-only Put path is unchanged and keeps omitting RawHTML.
func (c *Cache) PutRaw(url string, page *scrape.Page, source, rawHTML string) {
	if c == nil {
		return
	}
	e := cacheEntry{
		CachedAt: time.Now().Unix(),
		Source:   source,
		Page:     *page,
		RawHTML:  rawHTML,
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
