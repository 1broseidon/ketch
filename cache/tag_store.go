package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

var tagBucketName = []byte("tags")
var sourceBucketName = []byte("sources")

const tagSep = "\x00"

// tagDB owns a path, never a process-lifetime handle. Even an idle MCP server
// releases the file lock between operations. No transaction calls the cache.
type tagDB struct {
	path string
	err  error
}

// TagsPath returns the durable index location. UserConfigDir is native on
// Windows (AppData), macOS (Application Support), and Unix (XDG config/home).
// KETCH_TAGS_PATH overrides the complete filename, including in isolated labs.
func TagsPath() (string, error) {
	if path := os.Getenv("KETCH_TAGS_PATH"); path != "" {
		return filepath.Abs(path)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ketch", "tags.db"), nil
}

func defaultTagDB() *tagDB {
	path, err := TagsPath()
	return &tagDB{path: path, err: err}
}

// defaultOpenTimeout is the lock-wait budget for explicit tag operations
// (add/show/list/remove): a human or agent is directly waiting on the
// result, so it is worth a real wait for a concurrent writer to finish.
const defaultOpenTimeout = time.Second

func (s *tagDB) transaction(write bool, fn func(*bolt.Tx) error) error {
	return s.transactionTimeout(write, defaultOpenTimeout, fn)
}

// transactionTimeout is transaction with an explicit lock-wait budget.
// Backfill uses a much shorter one: it runs on every page Put whether or
// not the URL is tagged, so it must not make an ordinary fetch wait behind
// an unrelated `ketch tag` writer.
func (s *tagDB) transactionTimeout(write bool, timeout time.Duration, fn func(*bolt.Tx) error) error {
	if s.err != nil {
		return s.err
	}
	info, statErr := os.Stat(s.path)
	if statErr == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("tag index path is not a regular file: %s", s.path)
	}
	if !write {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
	} else if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("prepare tag index: %w", err)
	}
	if err := tightenDBPermissions(s.path); err != nil {
		return err
	}
	db, err := bolt.Open(s.path, 0o600, &bolt.Options{ReadOnly: !write, Timeout: timeout})
	if err != nil {
		return fmt.Errorf("open tag index: %w", err)
	}
	defer db.Close() //nolint:errcheck // transactions report their own commit errors
	if !write {
		return db.View(fn)
	}
	return db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{tagBucketName, sourceBucketName} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return fn(tx)
	})
}

func (s *tagDB) PutTagEntry(tag, _ string, value []byte) error {
	if err := ValidateTagName(tag); err != nil {
		return err
	}
	var entry TagEntry
	if err := json.Unmarshal(value, &entry); err != nil {
		return err
	}
	if entry.URL == "" {
		return errors.New("bookmark URL must not be empty")
	}
	return s.transaction(true, func(tx *bolt.Tx) error {
		key := []byte(tag + tagSep + cacheKey(entry.URL))
		b := tx.Bucket(tagBucketName)
		if old := b.Get(key); old != nil {
			var previous TagEntry
			if err := json.Unmarshal(old, &previous); err != nil {
				return err
			}
			fillTagMetadata(&entry, previous.Title, previous.Description)
		}
		if err := putTagEntry(b, key, entry); err != nil {
			return err
		}
		return tx.Bucket(sourceBucketName).Put([]byte(cacheKey(entry.URL)+tagSep+tag), key)
	})
}

func putTagEntry(b *bolt.Bucket, key []byte, entry TagEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return b.Put(key, data)
}

func fillTagMetadata(entry *TagEntry, title, description string) {
	if entry.Title == "" {
		entry.Title = title
	}
	if entry.Description == "" {
		entry.Description = description
	}
}

func (s *tagDB) TagEntries(tag string) ([][]byte, error) {
	var out [][]byte
	err := s.transaction(false, func(tx *bolt.Tx) error {
		b := tx.Bucket(tagBucketName)
		if b == nil {
			return nil
		}
		prefix := []byte(tag + tagSep)
		cursor := b.Cursor()
		for k, v := cursor.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = cursor.Next() {
			out = append(out, bytes.Clone(v))
		}
		return nil
	})
	return out, err
}

func (s *tagDB) TagNames() ([]string, error) {
	var names []string
	err := s.transaction(false, func(tx *bolt.Tx) error {
		b := tx.Bucket(tagBucketName)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			name, _, found := strings.Cut(string(k), tagSep)
			if found && (len(names) == 0 || names[len(names)-1] != name) {
				names = append(names, name)
			}
			return nil
		})
	})
	return names, err
}

func deleteTagEntry(tx *bolt.Tx, key []byte) error {
	b := tx.Bucket(tagBucketName)
	var entry TagEntry
	if err := json.Unmarshal(b.Get(key), &entry); err != nil {
		return err
	}
	tag, _, _ := strings.Cut(string(key), tagSep)
	if err := tx.Bucket(sourceBucketName).Delete([]byte(cacheKey(entry.URL) + tagSep + tag)); err != nil {
		return err
	}
	return b.Delete(key)
}

func (s *tagDB) DeleteTag(tag string) (int, error) {
	removed := 0
	err := s.transaction(true, func(tx *bolt.Tx) error {
		prefix := []byte(tag + tagSep)
		cursor := tx.Bucket(tagBucketName).Cursor()
		var keys [][]byte
		for k, _ := cursor.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = cursor.Next() {
			keys = append(keys, bytes.Clone(k))
		}
		for _, key := range keys {
			if err := deleteTagEntry(tx, key); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	return removed, err
}

func (s *tagDB) DeleteTagEntry(tag, sourceKey string) (bool, error) {
	found := false
	err := s.transaction(true, func(tx *bolt.Tx) error {
		key := []byte(tag + tagSep + sourceKey)
		if tx.Bucket(tagBucketName).Get(key) == nil {
			return nil
		}
		found = true
		return deleteTagEntry(tx, key)
	})
	return found, err
}

// backfillOpenTimeout bounds Backfill's own opens (both the membership check
// and, when needed, the write). It is deliberately far shorter than
// defaultOpenTimeout: Backfill runs on every page Put regardless of whether
// the URL is tagged, so a lock held by a concurrent `ketch tag` writer must
// not stall an ordinary scrape — this is a best-effort refresh, not a
// request anyone is waiting on.
const backfillOpenTimeout = 100 * time.Millisecond

// Backfill fills missing metadata for existing bookmarks of url. It never
// creates a membership and, for the common case of an untagged URL, never
// even opens the database for writing: a read-only lookup first confirms
// there is something to update. Both opens use backfillOpenTimeout, and a
// lock timeout on either is treated as a clean skip rather than an error —
// contention with another tag operation is expected and self-clearing, and
// Backfill runs unconditionally on every cache Put, tagged or not. Reads
// never write, so they cannot resurrect a removal.
func (s *tagDB) Backfill(url, cacheKeyValue, title, description string) error {
	tagged, err := s.hasMembership(url)
	if err != nil {
		if errors.Is(err, bolterrors.ErrTimeout) {
			return nil
		}
		return err
	}
	if !tagged {
		return nil
	}
	err = s.transactionTimeout(true, backfillOpenTimeout, func(tx *bolt.Tx) error {
		prefix := []byte(cacheKey(url) + tagSep)
		cursor := tx.Bucket(sourceBucketName).Cursor()
		b := tx.Bucket(tagBucketName)
		for k, entryKey := cursor.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, entryKey = cursor.Next() {
			var entry TagEntry
			if err := json.Unmarshal(b.Get(entryKey), &entry); err != nil {
				return err
			}
			previous := entry
			fillTagMetadata(&entry, title, description)
			entry.Key = cacheKeyValue
			if entry == previous {
				continue
			}
			if err := putTagEntry(b, entryKey, entry); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, bolterrors.ErrTimeout) {
		return nil
	}
	return err
}

// hasMembership reports whether any tag currently references url. It opens
// the database read-only (or not at all, when the file does not exist yet),
// so it is safe to call for every fetch regardless of whether anything has
// ever been tagged.
func (s *tagDB) hasMembership(url string) (bool, error) {
	found := false
	err := s.transactionTimeout(false, backfillOpenTimeout, func(tx *bolt.Tx) error {
		b := tx.Bucket(sourceBucketName)
		if b == nil {
			return nil
		}
		prefix := []byte(cacheKey(url) + tagSep)
		k, _ := b.Cursor().Seek(prefix)
		found = k != nil && bytes.HasPrefix(k, prefix)
		return nil
	})
	return found, err
}
