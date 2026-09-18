package cache

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

var bucketName = []byte("pages")

// tagBucketName holds the durable tag index, separate from the page bodies in
// bucketName. Two buckets rather than one keyspace is what lets Clear drop
// every body while the index survives, and what keeps the Store interface —
// which has no enumeration — unchanged.
var tagBucketName = []byte("tags")

// tagSep separates a tag name from a page key inside tagBucketName. NUL
// cannot occur in a tag name (callers validate) so the split is unambiguous
// and a cursor can seek a tag's entries by prefix.
const tagSep = "\x00"

// BBoltStore implements Store using an embedded bbolt database.
type BBoltStore struct {
	db   *bolt.DB
	path string
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
			_, err := tx.CreateBucketIfNotExists(tagBucketName)
			return err
		})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("create cache bucket: %w", err)
		}
	}
	return &BBoltStore{db: db, path: path}, nil
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
		return tx.Bucket(bucketName).Put([]byte(key), value)
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

// Clear removes every cached page body and deliberately leaves the tag index
// alone: it reclaims the disk and keeps the map. A tag entry holds its own
// URL, title and description, so the pages it lists survive as cold entries
// the agent can re-fetch.
func (s *BBoltStore) Clear() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bucketName); err != nil {
			return err
		}
		_, err := tx.CreateBucket(bucketName)
		return err
	})
}

func (s *BBoltStore) Close() error {
	return s.db.Close()
}

// PutTagEntry writes one page's index entry under a tag, replacing any entry
// already there for that page (re-tagging refreshes title and description).
func (s *BBoltStore) PutTagEntry(tag, pageKey string, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(tagBucketName)
		if err != nil {
			return err
		}
		return b.Put([]byte(tag+tagSep+pageKey), value)
	})
}

// TagEntries returns the raw entries under a tag. An absent bucket or tag
// yields no entries and no error: a read-only open never creates the bucket,
// and asking about an unused tag is a fair question.
func (s *BBoltStore) TagEntries(tag string) ([][]byte, error) {
	var out [][]byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tagBucketName)
		if b == nil {
			return nil
		}
		prefix := []byte(tag + tagSep)
		c := b.Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			val := make([]byte, len(v))
			copy(val, v)
			out = append(out, val)
		}
		return nil
	})
	return out, err
}

// TagNames returns every distinct tag, in bbolt's byte order.
func (s *BBoltStore) TagNames() ([]string, error) {
	var names []string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tagBucketName)
		if b == nil {
			return nil
		}
		seen := ""
		first := true
		return b.ForEach(func(k, _ []byte) error {
			name, _, found := strings.Cut(string(k), tagSep)
			if !found {
				return nil
			}
			// Keys are sorted, so a tag's entries are contiguous and one
			// comparison against the previous name is enough to dedupe.
			if first || name != seen {
				names = append(names, name)
				seen = name
				first = false
			}
			return nil
		})
	})
	return names, err
}

// DeleteTag drops a whole tag and reports how many entries it held.
func (s *BBoltStore) DeleteTag(tag string) (int, error) {
	removed := 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tagBucketName)
		if b == nil {
			return nil
		}
		prefix := []byte(tag + tagSep)
		// Collect before deleting: mutating the bucket under its own cursor
		// is not safe.
		var keys [][]byte
		c := b.Cursor()
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			key := make([]byte, len(k))
			copy(key, k)
			keys = append(keys, key)
		}
		for _, k := range keys {
			if err := b.Delete(k); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	return removed, err
}

// DeleteTagEntry drops one page from a tag, reporting whether it was there.
func (s *BBoltStore) DeleteTagEntry(tag, pageKey string) (bool, error) {
	found := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tagBucketName)
		if b == nil {
			return nil
		}
		k := []byte(tag + tagSep + pageKey)
		if b.Get(k) == nil {
			return nil
		}
		found = true
		return b.Delete(k)
	})
	return found, err
}
