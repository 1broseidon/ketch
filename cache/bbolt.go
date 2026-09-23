package cache

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

var bucketName = []byte("pages")

var freshnessBucket = []byte("freshness")

// metaBucket holds store bookkeeping: the time of the last expiry sweep.
var metaBucket = []byte("meta")

var sweptKey = []byte("swept")

// sweepNextKey holds the page key an unfinished sweep pass resumes from.
var sweepNextKey = []byte("sweep_next")

var pageBuckets = [][]byte{bucketName, freshnessBucket, metaBucket}

// pageOpenTimeout bounds how long a page-cache operation waits for another
// process's transaction. Transactions last milliseconds, so a second is a
// generous wait; past it the operation behaves as a miss, never an outage.
const pageOpenTimeout = time.Second

// The expiry sweep runs at most once per sweepInterval across every process
// sharing the file. One write visits at most sweepScan keys and removes at
// most sweepBudget of them, so no fetch pays for a large cache or a large
// backlog; a pass that stops early records where it stopped and the next
// write continues from there.
const (
	sweepInterval = time.Hour
	sweepScan     = 5000
	sweepBudget   = 1000
)

var (
	errNotFound = errors.New("not found")
	errReadOnly = errors.New("cache store is read-only")
)

// BBoltStore implements Store over an embedded bbolt file. It owns a path,
// never an open handle: each operation opens the file for one transaction
// and closes it, so a long-lived process never holds the cache locked.
type BBoltStore struct {
	path     string
	readOnly bool
	tags     *tagDB
}

// NewBBoltStore returns a store for the bbolt file at path, creating the file
// if it does not exist. The file is not held open afterwards. Lock contention
// while creating it is not an error; the next write creates it instead.
func NewBBoltStore(path string) (*BBoltStore, error) {
	s := newBBoltStore(path, false)
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		err = s.tx(true, func(*bolt.Tx) error { return nil })
		if err != nil && !errors.Is(err, bolterrors.ErrTimeout) {
			return nil, err
		}
	case err != nil:
		return nil, fmt.Errorf("open cache db: %w", err)
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("cache db path is not a regular file: %s", path)
	default:
		if err := tightenDBPermissions(path); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// NewBBoltStoreReadOnly returns a store that reads the file at path and
// refuses writes. It never creates the file.
func NewBBoltStoreReadOnly(path string) (*BBoltStore, error) {
	return newBBoltStore(path, true), nil
}

func newBBoltStore(path string, readOnly bool) *BBoltStore {
	return &BBoltStore{path: path, readOnly: readOnly, tags: &tagDB{path: filepath.Join(filepath.Dir(path), "tags.db")}}
}

func (s *BBoltStore) tx(write bool, fn func(*bolt.Tx) error) error {
	return boltTx(s.path, "cache db", write, pageOpenTimeout, pageBuckets, fn)
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
	err := s.tx(false, func(tx *bolt.Tx) error {
		if b := tx.Bucket(bucketName); b != nil {
			if v := b.Get([]byte(key)); v != nil {
				val = bytes.Clone(v)
			}
		}
		return nil
	})
	if err == nil && val == nil {
		err = errNotFound
	}
	return val, err
}

func (s *BBoltStore) Put(key string, value []byte) error {
	return s.put(key, value, 0)
}

// put writes one entry. A positive ttl also runs the expiry sweep inside the
// same transaction, so pruning never costs an extra open.
func (s *BBoltStore) put(key string, value []byte, ttl time.Duration) error {
	if s.readOnly {
		return errReadOnly
	}
	return s.tx(true, func(tx *bolt.Tx) error {
		if ttl > 0 {
			sweepExpired(tx, ttl, time.Now())
		}
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

// sweepExpired removes entries older than ttl, and entries with no freshness
// stamp (written before stamps existed, so older than any current entry).
//
// A pass starts at most once per sweepInterval across all processes. Each
// call visits at most sweepScan keys and removes at most sweepBudget; when it
// stops early it stores the next key, and every later write resumes there
// until the pass reaches the end and stamps its completion time. Removal is
// best effort: a failure here must not fail the write it rides on, so errors
// are dropped.
func sweepExpired(tx *bolt.Tx, ttl time.Duration, now time.Time) {
	meta := tx.Bucket(metaBucket)
	next := meta.Get(sweepNextKey)
	if last := meta.Get(sweptKey); next == nil && len(last) == 8 && now.Sub(time.Unix(int64(binary.BigEndian.Uint64(last)), 0)) < sweepInterval {
		return
	}
	pages, stamps := tx.Bucket(bucketName), tx.Bucket(freshnessBucket)
	cutoff := now.Add(-ttl).Unix()
	var stale [][]byte
	cursor := pages.Cursor()
	k, _ := cursor.First()
	if next != nil {
		k, _ = cursor.Seek(next)
	}
	for visited := 0; k != nil && visited < sweepScan && len(stale) < sweepBudget; visited++ {
		if stamp := stamps.Get(k); len(stamp) != 8 || int64(binary.BigEndian.Uint64(stamp)) < cutoff {
			stale = append(stale, bytes.Clone(k))
		}
		k, _ = cursor.Next()
	}
	var resume []byte
	if k != nil {
		resume = bytes.Clone(k)
	}
	for _, key := range stale {
		_ = pages.Delete(key)
		_ = stamps.Delete(key)
	}
	if resume != nil {
		_ = meta.Put(sweepNextKey, resume)
		return
	}
	_ = meta.Delete(sweepNextKey)
	var stamp [8]byte
	binary.BigEndian.PutUint64(stamp[:], uint64(now.Unix()))
	_ = meta.Put(sweptKey, stamp[:])
}

// Stats reports the entry count and file size; see Usage for the error.
func (s *BBoltStore) Stats() (entries int, sizeBytes int64) {
	entries, sizeBytes, _ = s.Usage()
	return entries, sizeBytes
}

// Usage is Stats with the read error reported, so a caller can tell an empty
// cache from one another process kept locked past the timeout.
func (s *BBoltStore) Usage() (entries int, sizeBytes int64, err error) {
	err = s.tx(false, func(tx *bolt.Tx) error {
		if b := tx.Bucket(bucketName); b != nil {
			entries = b.Stats().KeyN
		}
		return nil
	})
	if info, statErr := os.Stat(s.path); statErr == nil {
		sizeBytes = info.Size()
	}
	return entries, sizeBytes, err
}

// Clear removes every page and returns the space to the filesystem.
// Bookmarks live separately and are untouched.
//
// The buckets are emptied first, which is what makes Clear correct on every
// platform; deleting the file afterwards is what returns the space, since
// bbolt never shrinks a file. That delete is best effort: on Windows it fails
// while another process has the file open, and the emptied file stays. A
// write that lands between the two steps is lost with the file, which is
// what a clear running at the same moment means.
func (s *BBoltStore) Clear() error {
	if s.readOnly {
		return errReadOnly
	}
	err := s.tx(true, func(tx *bolt.Tx) error {
		for _, name := range pageBuckets {
			if err := tx.DeleteBucket(name); err != nil && !errors.Is(err, bolterrors.ErrBucketNotFound) {
				return err
			}
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	lock := pathLock(s.path)
	lock.Lock()
	_ = os.Remove(s.path)
	lock.Unlock()
	removeLegacyPageFiles(filepath.Join(filepath.Dir(s.path), "pages"))
	return nil
}

// legacyPageFile matches the one-file-per-page cache ketch used before v0.2.
var legacyPageFile = regexp.MustCompile(`^[0-9a-f]{16}\.json$`)

// removeLegacyPageFiles deletes the page files a pre-v0.2 ketch left beside
// the database, then the directory if that left it empty. Only files named
// the way that cache named them are touched.
func removeLegacyPageFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && legacyPageFile.MatchString(entry.Name()) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	_ = os.Remove(dir)
}

// Close is a no-op: the store holds nothing open between operations.
func (s *BBoltStore) Close() error {
	return nil
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
	err := s.tx(false, func(tx *bolt.Tx) error {
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
