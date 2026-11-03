package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/health"
)

func tempContext7State(t *testing.T) *context7State {
	t.Helper()
	return &context7State{path: filepath.Join(t.TempDir(), "ketch", context7StateFile)}
}

// A quota saved by one process is loaded by the next, and the file never
// holds a key, only its hash.
func TestContext7StateRoundTrip(t *testing.T) {
	st := tempContext7State(t)
	seen := time.Date(2026, 11, 2, 13, 30, 0, 0, time.UTC)
	q := context7Quota{known: true, limit: 200, remaining: 187, reset: time.Unix(1796083200, 0), observedAt: seen}
	st.save(context7StateEntry(""), q)
	st.save(context7StateEntry("secret-key-value"), q)

	got, ok := st.load(context7StateEntry(""))
	if !ok || got.remaining != 187 || got.limit != 200 || !got.reset.Equal(q.reset) || !got.observedAt.Equal(seen) {
		t.Fatalf("load = %+v %v", got, ok)
	}
	data, err := os.ReadFile(st.path)
	if err != nil || strings.Contains(string(data), "secret-key-value") || !strings.Contains(string(data), `"keyless"`) {
		t.Errorf("state file:\n%s (%v)", data, err)
	}
	if _, ok := tempContext7State(t).load("keyless"); ok {
		t.Error("a missing file loaded an entry")
	}
}

// The file stays bounded: the oldest entries go first.
func TestContext7StateBounded(t *testing.T) {
	st := tempContext7State(t)
	base := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	for i := range context7StateMax + 3 {
		st.save(fmt.Sprintf("key:%d", i), context7Quota{known: true, remaining: i, observedAt: base.Add(time.Duration(i) * time.Minute)})
	}
	records := st.read()
	if len(records) != context7StateMax {
		t.Fatalf("%d entries, want %d", len(records), context7StateMax)
	}
	if _, ok := records["key:0"]; ok {
		t.Error("the oldest entry survived")
	}
	if _, ok := records[fmt.Sprintf("key:%d", context7StateMax+2)]; !ok {
		t.Error("the newest entry was dropped")
	}
}

// A garbled file reads as empty, and the next save replaces it.
func TestContext7StateGarbled(t *testing.T) {
	st := tempContext7State(t)
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.load("keyless"); ok {
		t.Fatal("garbled file loaded an entry")
	}
	st.save("keyless", context7Quota{known: true, remaining: 3, observedAt: time.Now()})
	if q, ok := st.load("keyless"); !ok || q.remaining != 3 {
		t.Errorf("after save: %+v %v", q, ok)
	}
}

// A quota spent in one process stops the next from asking until it
// resets, through the state file; a record older than the trust window is
// asked about again.
func TestContext7SpentQuotaAcrossProcesses(t *testing.T) {
	st := tempContext7State(t)
	reset := time.Now().Add(time.Hour)
	var calls int
	handler := func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("ratelimit-limit", "200")
		w.Header().Set("ratelimit-remaining", "0")
		w.Header().Set("ratelimit-reset", strconv.FormatInt(reset.Unix(), 10))
		w.WriteHeader(http.StatusTooManyRequests)
	}
	process := func() *Context7 {
		c := newTestContext7(t, handler)
		c.apiKey = ""
		c.quota = &context7QuotaTracker{state: func() *context7State { return st }, entry: context7StateEntry("")}
		return c
	}

	var rl *RateLimitError
	if _, err := process().ResolveLibrary(context.Background(), "react", 1); !errors.As(err, &rl) || calls != 1 {
		t.Fatalf("first process: %v after %d requests", err, calls)
	}
	if _, err := process().ResolveLibrary(context.Background(), "react", 1); !errors.As(err, &rl) || calls != 1 {
		t.Fatalf("second process: %v after %d requests, want refused without a request", err, calls)
	}

	q, _ := st.load("keyless")
	q.observedAt = time.Now().Add(-context7StateTrust - time.Minute)
	st.save("keyless", q)
	if _, err := process().ResolveLibrary(context.Background(), "react", 1); !errors.As(err, &rl) || calls != 2 {
		t.Fatalf("stale record: %v after %d requests, want one more request", err, calls)
	}
}

// Keyless doctor reports from the state file alone.
func TestContext7KeylessStatus(t *testing.T) {
	now := time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC)
	seen := time.Date(2026, 11, 2, 13, 30, 0, 0, time.UTC)
	reset := time.Unix(1796083200, 0)
	for _, tc := range []struct {
		name   string
		q      *context7Quota
		status health.Status
		detail string
	}{
		{"not seen", nil, health.StatusOK, "keyless (quota not yet seen; Context7 allows about 200 anonymous requests a month per IP; a key lifts the cap: ketch config set context7_api_key <key>)"},
		{"seen", &context7Quota{known: true, limit: 200, remaining: 187, reset: reset, observedAt: seen}, health.StatusOK,
			"keyless (~187 of 200 left as of 2026-11-02 13:30 UTC, resets 2026-12-01 00:00 UTC; a key lifts the cap: ketch config set context7_api_key <key>)"},
		{"spent", &context7Quota{known: true, limit: 200, remaining: 0, reset: reset, observedAt: seen}, health.StatusNoKey,
			"context7: rate limited (the keyless quota of 200 requests is used up; resets 2026-12-01 00:00 UTC). Set a key to lift it: ketch config set context7_api_key <key> (as of 2026-11-02 13:30 UTC)"},
		{"renewed", &context7Quota{known: true, limit: 200, remaining: 0, reset: now.Add(-time.Minute), observedAt: seen}, health.StatusOK,
			"keyless (quota renewed 2026-11-02 13:59 UTC since last seen; a key lifts the cap: ketch config set context7_api_key <key>)"},
	} {
		st := tempContext7State(t)
		if tc.q != nil {
			st.save("keyless", *tc.q)
		}
		status, detail := context7KeylessStatus(st, now)
		if status != tc.status || detail != tc.detail {
			t.Errorf("%s: %q %q\nwant %q %q", tc.name, status, detail, tc.status, tc.detail)
		}
	}
}
