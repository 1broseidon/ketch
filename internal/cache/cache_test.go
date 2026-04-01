package cache

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/scrape"
)

type stubStore struct {
	values map[string][]byte
}

func (s *stubStore) Get(key string) ([]byte, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, errNotFound
	}
	return value, nil
}

func (s *stubStore) Put(key string, value []byte) error {
	s.values[key] = value
	return nil
}

func (s *stubStore) Stats() (entries int, sizeBytes int64) {
	return len(s.values), 0
}

func (s *stubStore) Clear() error {
	s.values = map[string][]byte{}
	return nil
}

func (s *stubStore) Close() error {
	return nil
}

var errNotFound = &stubError{message: "not found"}

type stubError struct {
	message string
}

func (e *stubError) Error() string {
	return e.message
}

func TestLookupReturnsFreshEntry(t *testing.T) {
	store := &stubStore{values: map[string][]byte{}}
	page := scrape.Page{URL: "https://example.com", Title: "Example"}
	entry := cacheEntry{CachedAt: time.Now().Unix(), Page: page}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	store.values[cacheKey(page.URL)] = data

	cache := &Cache{store: store, ttl: time.Hour}
	lookup := cache.Lookup(page.URL)
	if lookup == nil {
		t.Fatal("Lookup() = nil, want cached entry")
	}
	if !lookup.Fresh {
		t.Fatal("Lookup().Fresh = false, want true")
	}
	if lookup.Page == nil || lookup.Page.Title != page.Title {
		t.Fatalf("Lookup().Page = %#v", lookup.Page)
	}
}

func TestLookupReturnsExpiredEntry(t *testing.T) {
	store := &stubStore{values: map[string][]byte{}}
	page := scrape.Page{URL: "https://example.com/stale", Title: "Example"}
	entry := cacheEntry{CachedAt: time.Now().Add(-2 * time.Hour).Unix(), Page: page}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	store.values[cacheKey(page.URL)] = data

	cache := &Cache{store: store, ttl: time.Hour}
	lookup := cache.Lookup(page.URL)
	if lookup == nil {
		t.Fatal("Lookup() = nil, want cached entry")
	}
	if lookup.Fresh {
		t.Fatal("Lookup().Fresh = true, want false")
	}
	if got := cache.Get(page.URL); got != nil {
		t.Fatalf("Get() = %#v, want nil for expired entry", got)
	}
}
