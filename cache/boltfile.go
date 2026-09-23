package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// bbolt allows one process at a time to hold a database file, and holds it
// for as long as the handle stays open. Every ketch store therefore opens its
// file for exactly one transaction and closes it straight after: a
// long-running process (the MCP server, a background crawl) holds nothing
// between calls, so it can never lock the CLI or another server out. An open
// costs tens of microseconds; the fetch it sits beside costs hundreds of
// milliseconds.

// pathLocks serialises transactions on one file within this process. Two
// handles in one process contend through the OS file lock exactly as two
// processes do, and bbolt waits on that lock by polling every 50ms; an
// in-process lock turns that poll into a direct hand-off. Readers share,
// writers are exclusive, matching bbolt's own locking.
var pathLocks sync.Map // cleaned path -> *sync.RWMutex

func pathLock(path string) *sync.RWMutex {
	lock, _ := pathLocks.LoadOrStore(filepath.Clean(path), &sync.RWMutex{})
	return lock.(*sync.RWMutex)
}

// boltTx opens the bbolt file at path, runs fn in one transaction and closes
// the file. what names the store in errors ("tag index", "cache db").
//
// A read of a file that does not exist yet calls nothing and returns nil:
// an absent store is an empty one. A write creates the file, its private
// directory and the named buckets first. timeout bounds the wait for another
// process's lock; exceeding it returns an error wrapping bolt's ErrTimeout.
func boltTx(path, what string, write bool, timeout time.Duration, buckets [][]byte, fn func(*bolt.Tx) error) error {
	info, statErr := os.Stat(path)
	if statErr == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("%s path is not a regular file: %s", what, path)
	}
	if !write {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare %s: %w", what, err)
	}

	lock := pathLock(path)
	if write {
		lock.Lock()
		defer lock.Unlock()
	} else {
		lock.RLock()
		defer lock.RUnlock()
	}

	if err := tightenDBPermissions(path); err != nil {
		return err
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{ReadOnly: !write, Timeout: timeout})
	if err != nil {
		return fmt.Errorf("open %s: %w", what, err)
	}
	defer db.Close() //nolint:errcheck // transactions report their own commit errors
	if !write {
		return db.View(fn)
	}
	return db.Update(func(tx *bolt.Tx) error {
		for _, name := range buckets {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return fn(tx)
	})
}
