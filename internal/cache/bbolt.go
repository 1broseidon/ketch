package cache

import (
	"fmt"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
)

var bucketName = []byte("pages")

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
	opts := &bolt.Options{Timeout: 1 * time.Second, ReadOnly: readOnly}
	db, err := bolt.Open(path, 0o644, opts)
	if err != nil {
		return nil, fmt.Errorf("open cache db: %w", err)
	}
	if !readOnly {
		err = db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(bucketName)
			return err
		})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("create cache bucket: %w", err)
		}
	}
	return &BBoltStore{db: db, path: path}, nil
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
