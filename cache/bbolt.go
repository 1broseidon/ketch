package cache

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	bolt "go.etcd.io/bbolt"
)

var bucketName = []byte("pages")

var freshnessBucket = []byte("freshness")

// BBoltStore implements Store using an embedded bbolt database.
type BBoltStore struct {
	db   *bolt.DB
	path string
	tags *tagDB
}

// NewBBoltStore opens or creates a bbolt database at the given path.
func NewBBoltStore(path string) (*BBoltStore, error) {
	return openBBolt(path, false)
}

// NewBBoltStoreReadOnly opens a bbolt database for reading.
// Use when another process may hold the write lock.
func NewBBoltStoreReadOnly(path string) (*BBoltStore, error) {
	return openBBolt(path, true)
}

func openBBolt(path string, readOnly bool) (*BBoltStore, error) {
	if err := tightenDBPermissions(path); err != nil {
		return nil, err
	}
	opts := &bolt.Options{Timeout: 1 * time.Second, ReadOnly: readOnly}
	db, err := bolt.Open(path, 0o600, opts)
	if err != nil {
		return nil, fmt.Errorf("open cache db: %w", err)
	}
	if err := tightenDBPermissions(path); err != nil {
		_ = db.Close()
		return nil, err
	}
	if !readOnly {
		err = db.Update(func(tx *bolt.Tx) error {
			if _, err := tx.CreateBucketIfNotExists(bucketName); err != nil {
				return err
			}
			_, err := tx.CreateBucketIfNotExists(freshnessBucket)
			return err
		})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("create cache bucket: %w", err)
		}
	}
	return &BBoltStore{db: db, path: path, tags: &tagDB{path: filepath.Join(filepath.Dir(path), "tags.db")}}, nil
}

// tightenDBPermissions protects both newly-created and pre-existing cache
// databases. It is deliberately enforced on read-only opens too: a cache may
// contain authenticated page content even when the current command only
// inspects statistics.
func tightenDBPermissions(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := os.Chmod(path, 0o600); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("secure cache db permissions: %w", err)
	}
	return nil
}

func (s *BBoltStore) Get(key string) ([]byte, error) {
	var val []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketName).Get([]byte(key))
		if v == nil {
			return fmt.Errorf("not found")
		}
		val = make([]byte, len(v))
		copy(val, v)
		return nil
	})
	return val, err
}

func (s *BBoltStore) Put(key string, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bucketName).Put([]byte(key), value); err != nil {
			return err
		}
		var metadata struct {
			CachedAt int64 `json:"t"`
		}
		if err := json.Unmarshal(value, &metadata); err != nil {
			return tx.Bucket(freshnessBucket).Delete([]byte(key))
		}
		var stamp [8]byte
		binary.BigEndian.PutUint64(stamp[:], uint64(metadata.CachedAt))
		return tx.Bucket(freshnessBucket).Put([]byte(key), stamp[:])
	})
}

func (s *BBoltStore) Stats() (entries int, sizeBytes int64) {
	_ = s.db.View(func(tx *bolt.Tx) error {
		entries = tx.Bucket(bucketName).Stats().KeyN
		return nil
	})
	if info, err := os.Stat(s.path); err == nil {
		sizeBytes = info.Size()
	}
	return entries, sizeBytes
}

// Clear frees page storage for reuse. It does not shrink the file; bookmarks live separately.
func (s *BBoltStore) Clear() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bucketName); err != nil {
			return err
		}
		if _, err := tx.CreateBucket(bucketName); err != nil {
			return err
		}
		if err := tx.DeleteBucket(freshnessBucket); err != nil {
			return err
		}
		_, err := tx.CreateBucket(freshnessBucket)
		return err
	})
}

func (s *BBoltStore) Close() error {
	return s.db.Close()
}

// PutTagEntry writes to the separate index, opening it only for this operation.
func (s *BBoltStore) PutTagEntry(tag, pageKey string, value []byte) error {
	return s.tags.PutTagEntry(tag, pageKey, value)
}

// TagEntries reads one tag from the separate index.
func (s *BBoltStore) TagEntries(tag string) ([][]byte, error) { return s.tags.TagEntries(tag) }

// TagNames lists tags from the separate index.
func (s *BBoltStore) TagNames() ([]string, error) { return s.tags.TagNames() }

// DeleteTag removes a tag from the separate index.
func (s *BBoltStore) DeleteTag(tag string) (int, error) { return s.tags.DeleteTag(tag) }

// DeleteTagEntry removes one stable source identity from the separate index.
func (s *BBoltStore) DeleteTagEntry(tag, sourceKey string) (bool, error) {
	return s.tags.DeleteTagEntry(tag, sourceKey)
}

// cachedKeys checks timestamps without decoding page bodies. Legacy page
// entries without timestamp metadata are inspected without allocating Page.
func (s *BBoltStore) cachedKeys(keys []string, ttl time.Duration) (map[string]bool, error) {
	out := make(map[string]bool, len(keys))
	err := s.db.View(func(tx *bolt.Tx) error {
		pages, stamps := tx.Bucket(bucketName), tx.Bucket(freshnessBucket)
		for _, key := range keys {
			hashed := []byte(cacheKey(key))
			var at int64
			if stamps != nil && len(stamps.Get(hashed)) == 8 {
				at = int64(binary.BigEndian.Uint64(stamps.Get(hashed)))
			} else if pages != nil {
				var meta struct {
					CachedAt int64 `json:"t"`
				}
				if err := json.Unmarshal(pages.Get(hashed), &meta); err == nil {
					at = meta.CachedAt
				}
			}
			out[key] = at != 0 && time.Since(time.Unix(at, 0)) <= ttl
		}
		return nil
	})
	return out, err
}

// Backfill updates metadata for already-bookmarked source URLs.
func (s *BBoltStore) Backfill(url, key, title, description string) error {
	return s.tags.Backfill(url, key, title, description)
}
