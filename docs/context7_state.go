package docs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// context7StateTrust bounds how long a spent quota another process saw
// stops requests here: past it, one request asks Context7 again. The
// anonymous quota is per IP address, and the address can change.
const context7StateTrust = 24 * time.Hour

// context7StateMax bounds the state file: the keyless entry and a few keys.
const context7StateMax = 8

// context7StateFile is the state file's name in ketch's cache directory,
// beside the page cache (cache.db) and the crawl status files.
const context7StateFile = "context7-quota.json"

// context7State is the last rate limit Context7 reported, per key, kept in
// a small JSON file in ketch's cache directory so it outlives the process:
// `ketch doctor` reports the keyless quota from it without spending a
// request, and a quota one process saw spent stops the next from asking.
//
// It follows the crawl status files rather than the page cache (ADR-0006):
// a few hundred bytes, written whole to a temporary file and renamed over
// the old one, so a reader never sees half a file and nothing holds a
// lock. Two processes writing at once can lose one update; the next
// response writes it again. Keys are stored only as a hash.
type context7State struct {
	path string
}

// context7StateRecord is one entry of the state file.
type context7StateRecord struct {
	Limit      int       `json:"limit,omitempty"`
	Remaining  int       `json:"remaining"`
	Reset      time.Time `json:"reset,omitzero"`
	ObservedAt time.Time `json:"observed_at"`
}

// defaultContext7State is the state file in the user's cache directory, or
// nil when there is none.
func defaultContext7State() *context7State {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	return &context7State{path: filepath.Join(base, "ketch", context7StateFile)}
}

// context7StateEntry names a key's entry: "keyless", or a hash of the key.
func context7StateEntry(apiKey string) string {
	if apiKey == "" {
		return "keyless"
	}
	sum := sha256.Sum256([]byte(apiKey))
	return "key:" + hex.EncodeToString(sum[:8])
}

func (s *context7State) read() map[string]context7StateRecord {
	records := map[string]context7StateRecord{}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return records
	}
	if json.Unmarshal(data, &records) != nil {
		return map[string]context7StateRecord{}
	}
	return records
}

// load is the entry's last quota; false when none was saved.
func (s *context7State) load(entry string) (context7Quota, bool) {
	if s == nil {
		return context7Quota{}, false
	}
	r, ok := s.read()[entry]
	if !ok || r.ObservedAt.IsZero() {
		return context7Quota{}, false
	}
	return context7Quota{known: true, limit: r.Limit, remaining: r.Remaining, reset: r.Reset, observedAt: r.ObservedAt}, true
}

// save writes the entry's quota, best effort: a write that fails costs
// only the next process a request.
func (s *context7State) save(entry string, q context7Quota) {
	if s == nil || !q.known {
		return
	}
	records := s.read()
	var reset time.Time
	if !q.reset.IsZero() {
		reset = q.reset.UTC()
	}
	records[entry] = context7StateRecord{Limit: q.limit, Remaining: q.remaining, Reset: reset, ObservedAt: q.observedAt.UTC()}
	for len(records) > context7StateMax {
		delete(records, oldestContext7Entry(records))
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, context7StateFile+".*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), s.path) != nil {
		_ = os.Remove(tmp.Name())
	}
}

func oldestContext7Entry(records map[string]context7StateRecord) string {
	names := make([]string, 0, len(records))
	for name := range records {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return records[names[i]].ObservedAt.Before(records[names[j]].ObservedAt) })
	return names[0]
}
