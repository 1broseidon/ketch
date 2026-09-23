package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/1broseidon/ketch/config"
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
	store      Store
	ttl        time.Duration
	index      *tagDB
	pages      *Cache
	onTagError func(error)
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

// New creates a cache with the default bbolt backend. The file is opened only
// for each operation, so the returned Cache holds no lock while idle.
// Returns nil if the cache location cannot be prepared.
func New(ttl time.Duration) *Cache {
	path, err := DBPath()
	if err != nil {
		return nil
	}
	store, err := NewBBoltStore(path)
	if err != nil {
		return nil
	}
	store.tags = defaultTagDB()
	return &Cache{store: store, ttl: ttl}
}

// NewWithStore builds a cache over a caller-supplied Store. Used by tests to
// isolate the cache (temp bbolt path) from the user's real cache dir.
func NewWithStore(store Store, ttl time.Duration) *Cache {
	return &Cache{store: store, ttl: ttl}
}

// NewFromConfig creates a cache with the TTL parsed from cfg.CacheTTL,
// falling back to one hour if the value doesn't parse. Returns nil (a valid,
// no-op cache — all methods are nil-safe) if the cache cannot be initialized.
func NewFromConfig(cfg *config.Config) *Cache {
	ttl, err := time.ParseDuration(cfg.CacheTTL)
	if err != nil {
		ttl = time.Hour
	}
	return New(ttl)
}

// NewReadOnly returns a cache that reads the default store and refuses
// writes, for inspection commands that must never create or modify it.
func NewReadOnly() *Cache {
	path, err := DBPath()
	if err != nil {
		return nil
	}
	store, err := NewBBoltStoreReadOnly(path)
	if err != nil {
		return nil
	}
	store.tags = defaultTagDB()
	return &Cache{store: store}
}

// DBPath returns the default cache database path.
func DBPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "ketch")
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache.db"), nil
}

func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return os.Chmod(dir, 0o700)
}

// Get looks up a cached page by URL. Returns (nil, "") if missing or expired.
// The second return is the fetch source recorded at Put time (scrape.SourceHTTP
// or scrape.SourceBrowser); empty for entries written before source tracking.
func (c *Cache) Get(url string) (*scrape.Page, string) {
	if c == nil || c.store == nil {
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
	if c == nil || c.store == nil {
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
	c.storePut(cacheKey(url), data)
	if err := c.Backfill(url, page.URL, page); err != nil {
		if c.onTagError != nil {
			c.onTagError(err)
		} else {
			fmt.Fprintf(os.Stderr, "warn: tag metadata backfill: %v\n", err)
		}
	}
}

// GetRaw looks up a cached entry's raw HTML by URL. Returns (rawHTML, source,
// page). It is a hit only when the entry exists, is fresh, AND has non-empty
// RawHTML — a markdown-only entry must not poison a --raw request (serving
// empty HTML). On miss or empty raw, returns ("", "", nil) so the caller
// refetches and back-fills.
func (c *Cache) GetRaw(url string) (string, string, *scrape.Page) {
	if c == nil || c.store == nil {
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
	if c == nil || c.store == nil {
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
	c.storePut(cacheKey(url), data)
	if err := c.Backfill(url, page.URL, page); err != nil {
		if c.onTagError != nil {
			c.onTagError(err)
		} else {
			fmt.Fprintf(os.Stderr, "warn: tag metadata backfill: %v\n", err)
		}
	}
}

// storePut writes one encoded entry. The bbolt store also sweeps expired
// entries inside the same write, bounded and at most hourly.
func (c *Cache) storePut(key string, data []byte) {
	if s, ok := c.store.(*BBoltStore); ok {
		_ = s.put(key, data, c.ttl)
		return
	}
	_ = c.store.Put(key, data)
}

// Usage returns the entry count and file size, with the error when the store
// could not be read (another process held it past the wait). Stats is the
// same without the error.
func (c *Cache) Usage() (entries int, bytes int64, err error) {
	if c == nil || c.store == nil {
		return 0, 0, nil
	}
	if s, ok := c.store.(*BBoltStore); ok {
		return s.Usage()
	}
	entries, bytes = c.store.Stats()
	return entries, bytes, nil
}

// Stats returns cache entry count and total size in bytes.
func (c *Cache) Stats() (entries int, bytes int64) {
	if c == nil || c.store == nil {
		return 0, 0
	}
	return c.store.Stats()
}

// Clear removes all cached pages.
func (c *Cache) Clear() error {
	if c == nil || c.store == nil {
		return nil
	}
	return c.store.Clear()
}

// Close releases cache resources. The bbolt store holds none between
// operations, so this is a no-op for it; it stays for other Store backends.
func (c *Cache) Close() {
	if c == nil || c.store == nil {
		return
	}
	_ = c.store.Close()
}

func cacheKey(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:8])
}
