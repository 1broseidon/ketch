package cache

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/1broseidon/ketch/scrape"
	bolt "go.etcd.io/bbolt"
)

// The store must not hold the file between operations: after any mix of
// calls, an exclusive open from outside (another process, in effect) gets
// the lock immediately.
func TestStoreHoldsNoLockBetweenOperations(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	c.Put("https://example.test/a", &scrape.Page{URL: "https://example.test/a", Markdown: "a"}, scrape.SourceHTTP)
	if page, _ := c.Get("https://example.test/a"); page == nil {
		t.Fatal("put then get missed")
	}
	c.Stats()

	path := c.store.(*BBoltStore).path
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("store still holds the file between operations: %v", err)
	}
	_ = db.Close()
}

// While something else holds the file, operations degrade to a miss and a
// dropped write, and recover as soon as it lets go. This is the property
// the MCP server needs: a lock held elsewhere costs one call, never the
// server's lifetime.
func TestStoreRecoversWhenAnotherHolderReleases(t *testing.T) {
	c := newTestCache(t, time.Hour)
	url := "https://example.test/held"
	path := c.store.(*BBoltStore).path
	holder, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Put(url, &scrape.Page{URL: url, Markdown: "held"}, scrape.SourceHTTP)
	if _, _, err := c.Usage(); err == nil {
		t.Fatal("Usage against a held file should report the lock")
	}
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}

	c.Put(url, &scrape.Page{URL: url, Markdown: "after"}, scrape.SourceHTTP)
	page, _ := c.Get(url)
	if page == nil || page.Markdown != "after" {
		t.Fatalf("store did not recover after the holder released: %+v", page)
	}
}

// Goroutines sharing one store, as concurrent MCP tool calls do, must not
// lose writes or time out against each other.
func TestStoreConcurrentUseInOneProcess(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 20 {
				url := fmt.Sprintf("https://example.test/%d/%d", g, i)
				c.Put(url, &scrape.Page{URL: url, Markdown: url}, scrape.SourceHTTP)
				if page, _ := c.Get(url); page == nil || page.Markdown != url {
					t.Errorf("lost %s", url)
				}
			}
		}()
	}
	wg.Wait()
	if entries, _, err := c.Usage(); err != nil || entries != 160 {
		t.Fatalf("entries = %d, err = %v; want 160", entries, err)
	}
}

// Expired entries are swept inside a write, at most once per interval, and
// unstamped entries (written before stamps existed) go with them.
func TestPutSweepsExpiredEntries(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	store := c.store.(*BBoltStore)
	old := time.Now().Add(-2 * time.Hour).Unix()
	err := store.tx(true, func(tx *bolt.Tx) error {
		var stamp [8]byte
		binary.BigEndian.PutUint64(stamp[:], uint64(old))
		if err := tx.Bucket(bucketName).Put([]byte("stale"), []byte(`{"t":1}`)); err != nil {
			return err
		}
		if err := tx.Bucket(freshnessBucket).Put([]byte("stale"), stamp[:]); err != nil {
			return err
		}
		return tx.Bucket(bucketName).Put([]byte("legacy"), []byte(`{"t":1}`))
	})
	if err != nil {
		t.Fatal(err)
	}

	c.Put("https://example.test/fresh", &scrape.Page{URL: "https://example.test/fresh", Markdown: "fresh"}, scrape.SourceHTTP)
	for _, key := range []string{"stale", "legacy"} {
		if _, err := store.Get(key); !errors.Is(err, errNotFound) {
			t.Errorf("%s survived the sweep: %v", key, err)
		}
	}
	if page, _ := c.Get("https://example.test/fresh"); page == nil {
		t.Fatal("the write that ran the sweep was lost")
	}

	// A second expired entry written straight after is left for the next
	// interval: the sweep runs at most hourly.
	if err := store.tx(true, func(tx *bolt.Tx) error { return tx.Bucket(bucketName).Put([]byte("later"), []byte(`{"t":1}`)) }); err != nil {
		t.Fatal(err)
	}
	c.Put("https://example.test/again", &scrape.Page{URL: "https://example.test/again", Markdown: "again"}, scrape.SourceHTTP)
	if _, err := store.Get("later"); err != nil {
		t.Fatalf("sweep ran twice within the interval: %v", err)
	}
}

// Clear empties the cache, returns the space by removing the file, removes
// the page files a pre-v0.2 ketch left beside it, and leaves the store
// usable.
func TestClearRemovesTheFileAndLegacyPages(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	path := c.store.(*BBoltStore).path
	legacy := filepath.Join(filepath.Dir(path), "pages")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "0212020afcb855ba.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Put("https://example.test/a", &scrape.Page{URL: "https://example.test/a", Markdown: "a"}, scrape.SourceHTTP)

	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cache file still on disk after clear: %v", err)
	}
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("legacy pages directory survived clear: %v", err)
	}
	if entries, _, err := c.Usage(); err != nil || entries != 0 {
		t.Fatalf("after clear: entries = %d, err = %v", entries, err)
	}
	c.Put("https://example.test/b", &scrape.Page{URL: "https://example.test/b", Markdown: "b"}, scrape.SourceHTTP)
	if page, _ := c.Get("https://example.test/b"); page == nil {
		t.Fatal("store unusable after clear")
	}
}

// Legacy cleanup touches only files named the way the old cache named them.
func TestClearLeavesUnrelatedFilesInAPagesDirectory(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	dir := filepath.Join(filepath.Dir(c.store.(*BBoltStore).path), "pages")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("clear removed a file it does not own: %v", err)
	}
}

// A read-only store never creates the file and refuses writes; reading a
// store that does not exist yet is an empty cache, not an error.
func TestReadOnlyStore(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cache.db")
	store, err := NewBBoltStoreReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("k"); !errors.Is(err, errNotFound) {
		t.Fatalf("get on a missing file = %v, want not found", err)
	}
	if entries, _, err := store.Usage(); err != nil || entries != 0 {
		t.Fatalf("usage on a missing file = %d, %v", entries, err)
	}
	if err := store.Put("k", []byte("v")); !errors.Is(err, errReadOnly) {
		t.Fatalf("put = %v, want read-only", err)
	}
	if err := store.Clear(); !errors.Is(err, errReadOnly) {
		t.Fatalf("clear = %v, want read-only", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only store created the file")
	}
}

// A file that exists without the page buckets (created by an older or
// foreign opener) reads as empty instead of crashing.
func TestStoreReadsAFileWithoutBuckets(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cache.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	store, _ := NewBBoltStoreReadOnly(path)
	if entries, _, err := store.Usage(); err != nil || entries != 0 {
		t.Fatalf("usage = %d, %v", entries, err)
	}
	if _, err := store.cachedKeys([]string{"https://example.test/"}, time.Hour); err != nil {
		t.Fatalf("cachedKeys on a bucketless file: %v", err)
	}
}

// One sweep call visits at most sweepScan keys even when almost nothing is
// stale, records where it stopped, and the next call resumes there; only a
// pass that reaches the end stamps its completion.
func TestSweepScanIsBoundedAndResumes(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	store := c.store.(*BBoltStore)
	now := time.Now()
	fresh := make([]byte, 8)
	binary.BigEndian.PutUint64(fresh, uint64(now.Unix()))
	err := store.tx(true, func(tx *bolt.Tx) error {
		pages, stamps := tx.Bucket(bucketName), tx.Bucket(freshnessBucket)
		for i := range sweepScan + 10 {
			key := []byte(fmt.Sprintf("a-%06d", i))
			if err := pages.Put(key, []byte(`{}`)); err != nil {
				return err
			}
			if err := stamps.Put(key, fresh); err != nil {
				return err
			}
		}
		return pages.Put([]byte("z-stale"), []byte(`{}`)) // sorts last, no stamp
	})
	if err != nil {
		t.Fatal(err)
	}

	sweep := func() (next, swept []byte) {
		if err := store.tx(true, func(tx *bolt.Tx) error {
			sweepExpired(tx, time.Hour, now)
			meta := tx.Bucket(metaBucket)
			next, swept = bytes.Clone(meta.Get(sweepNextKey)), bytes.Clone(meta.Get(sweptKey))
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return next, swept
	}

	next, swept := sweep()
	if next == nil || swept != nil {
		t.Fatalf("first pass should stop at the scan cap: next=%q swept=%v", next, swept)
	}
	if _, err := store.Get("z-stale"); err != nil {
		t.Fatal("the capped pass reached a key past its scan limit")
	}
	next, swept = sweep()
	if next != nil || swept == nil {
		t.Fatalf("second pass should finish: next=%q swept=%v", next, swept)
	}
	if _, err := store.Get("z-stale"); !errors.Is(err, errNotFound) {
		t.Fatalf("resumed pass missed the stale entry: %v", err)
	}
	if entries, _, _ := store.Usage(); entries != sweepScan+10 {
		t.Fatalf("fresh entries lost: %d", entries)
	}
}
