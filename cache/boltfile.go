package cache

import (
	"sync"
	"time"

	"github.com/1broseidon/ketch/internal/boltfile"
	bolt "go.etcd.io/bbolt"
)

// Every ketch store opens its bbolt file for exactly one transaction and
// closes it straight after (ADR-0006); internal/boltfile does it for the
// page cache, the tag index and the docs index alike.

func pathLock(path string) *sync.RWMutex { return boltfile.Lock(path) }

// boltTx runs fn in one transaction on the file at path (boltfile.Tx).
func boltTx(path, what string, write bool, timeout time.Duration, buckets [][]byte, fn func(*bolt.Tx) error) error {
	return boltfile.Tx(path, what, write, timeout, buckets, fn)
}
