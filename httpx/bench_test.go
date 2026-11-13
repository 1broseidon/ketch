//go:build bench

package httpx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// withBench points the hook at env for one test. benchOnce is process-wide,
// so tests reset it.
func withBench(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{envRecord, envReplay, envBodies, envLog} {
		t.Setenv(k, env[k])
	}
	benchOnce = sync.OnceValue(loadBench)
	t.Cleanup(func() { benchOnce = sync.OnceValue(loadBench) })
}

func benchDo(t *testing.T, c *http.Client, method, u, body string) (int, string, error) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, u, r)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data), err
}

func countingServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/page", http.StatusFound)
		case "/counter":
			fmt.Fprintf(w, "call %d", n)
		default:
			fmt.Fprintf(w, "%s %s body=%s", r.Method, r.URL.Path, body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestBenchRecordThenReplay(t *testing.T) {
	srv, hits := countingServer(t)
	dir := t.TempDir()
	logFile := filepath.Join(dir, "log.jsonl")
	withBench(t, map[string]string{envRecord: filepath.Join(dir, "world"), envLog: logFile})
	c := New(0, 4)
	type call struct{ method, path, body string }
	calls := []call{{"GET", "/page", ""}, {"GET", "/redirect", ""}, {"POST", "/search", "q=a"}, {"POST", "/search", "q=b"},
		{"GET", "/counter", ""}, {"GET", "/counter", ""}}
	var recorded []string
	for _, k := range calls {
		status, body, err := benchDo(t, c, k.method, srv.URL+k.path, k.body)
		if err != nil {
			t.Fatal(err)
		}
		recorded = append(recorded, fmt.Sprintf("%d %s", status, body))
	}
	if hits.Load() != 7 { // the redirect is two exchanges
		t.Fatalf("record hit the server %d times, want 7", hits.Load())
	}

	withBench(t, map[string]string{envReplay: filepath.Join(dir, "world"), envLog: logFile})
	srv.Close() // replay must not need the network
	c = New(0, 4)
	for i, k := range calls {
		status, body, err := benchDo(t, c, k.method, srv.URL+k.path, k.body)
		if err != nil {
			t.Fatalf("replay %v: %v", k, err)
		}
		if got := fmt.Sprintf("%d %s", status, body); got != recorded[i] {
			t.Fatalf("replay %v = %q, recorded %q", k, got, recorded[i])
		}
	}
	// Repeated identical requests keep their recorded order, then reuse the last.
	if _, body, _ := benchDo(t, c, "GET", srv.URL+"/counter", ""); body != "call 7" {
		t.Fatalf("third /counter = %q, want the last recorded response", body)
	}
	_, _, err := benchDo(t, c, "GET", srv.URL+"/never", "")
	if !errors.Is(err, ErrUnrecorded) {
		t.Fatalf("unrecorded request: %v, want ErrUnrecorded", err)
	}
	log, _ := os.ReadFile(logFile)
	for _, want := range []string{`"event":"recorded"`, `"event":"served"`, `"event":"reused"`, `"event":"miss"`} {
		if !bytes.Contains(log, []byte(want)) {
			t.Errorf("log lacks %s", want)
		}
	}
}

func TestBenchReplaysTransportErrorsAndDefaultClient(t *testing.T) {
	dir := t.TempDir()
	withBench(t, map[string]string{envRecord: dir})
	// A closed port: the refusal is recorded as an error exchange.
	srv := httptest.NewServer(http.NotFoundHandler())
	dead := srv.URL
	srv.Close()
	if _, _, err := benchDo(t, http.DefaultClient, "GET", dead+"/x", ""); err == nil {
		t.Fatal("expected a connection error")
	}
	withBench(t, map[string]string{envReplay: dir})
	_, _, err := benchDo(t, http.DefaultClient, "GET", dead+"/x", "")
	if err == nil || errors.Is(err, ErrUnrecorded) || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("replayed error = %v, want the recorded refusal", err)
	}
}

func TestBenchConcurrentIdenticalRequestsAreDeterministic(t *testing.T) {
	srv, _ := countingServer(t)
	dir := t.TempDir()
	withBench(t, map[string]string{envRecord: dir})
	c := New(0, 8)
	for range 3 {
		if _, _, err := benchDo(t, c, "GET", srv.URL+"/same", ""); err != nil {
			t.Fatal(err)
		}
	}
	withBench(t, map[string]string{envReplay: dir})
	var wg sync.WaitGroup
	bodies := make([]string, 16)
	for i := range bodies {
		wg.Go(func() { _, bodies[i], _ = benchDo(t, c, "GET", srv.URL+"/same", "") })
	}
	wg.Wait()
	for _, b := range bodies {
		if b != "GET /same body=" {
			t.Fatalf("concurrent replay served %q", b)
		}
	}
}

func TestBenchReplayFromArchive(t *testing.T) {
	srv, _ := countingServer(t)
	dir := t.TempDir()
	withBench(t, map[string]string{envRecord: dir})
	if _, _, err := benchDo(t, New(0, 1), "GET", srv.URL+"/a", ""); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "world.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		_ = tw.WriteHeader(&tar.Header{Name: "case/" + filepath.ToSlash(rel), Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(data)
		return nil
	})
	_ = tw.Close()
	_ = gz.Close()
	_ = os.WriteFile(archive, buf.Bytes(), 0o644)
	srv.Close()
	withBench(t, map[string]string{envReplay: archive})
	if _, body, err := benchDo(t, New(0, 1), "GET", srv.URL+"/a", ""); err != nil || body != "GET /a body=" {
		t.Fatalf("archive replay = %q, %v", body, err)
	}
}

func TestBenchRejectsBothModesAndTamperedBodies(t *testing.T) {
	withBench(t, map[string]string{envRecord: t.TempDir(), envReplay: t.TempDir()})
	if _, _, err := benchDo(t, New(0, 1), "GET", "http://127.0.0.1:1/", ""); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("both modes: %v", err)
	}
	srv, _ := countingServer(t)
	dir := t.TempDir()
	withBench(t, map[string]string{envRecord: dir})
	if _, _, err := benchDo(t, New(0, 1), "GET", srv.URL+"/a", ""); err != nil {
		t.Fatal(err)
	}
	bodies, _ := filepath.Glob(filepath.Join(dir, "bodies", "*"))
	_ = os.WriteFile(bodies[0], []byte("tampered"), 0o644)
	withBench(t, map[string]string{envReplay: dir})
	if _, _, err := benchDo(t, New(0, 1), "GET", srv.URL+"/a", ""); !errors.Is(err, ErrUnrecorded) {
		t.Fatalf("tampered body: %v, want ErrUnrecorded", err)
	}
}

func TestBenchReplayHonoursCancelledContext(t *testing.T) {
	withBench(t, map[string]string{envReplay: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.invalid/", nil)
	if _, err := New(0, 1).Do(req); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled replay: %v", err)
	}
}

// A recorded failure takes its recorded time in replay, so a caller's
// budget runs out as it did live: a recorded context deadline ends at the
// caller's deadline, however long it took live, and anything else after
// its recorded elapsed time.
func TestBenchReplaysFailuresAfterTheirRecordedTime(t *testing.T) {
	dir := t.TempDir()
	put := func(ex Exchange) {
		data, _ := json.Marshal(ex)
		if err := os.WriteFile(filepath.Join(dir, ExchangeKey("GET", ex.URL, nil)+"-000.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deadline := Exchange{Method: "GET", URL: "http://example.invalid/slow", Error: "Get \"http://example.invalid/slow\": context deadline exceeded",
		ErrTimeout: true, ElapsedMS: 5}
	refused := Exchange{Method: "GET", URL: "http://example.invalid/refused", Error: "dial tcp: connection refused", ElapsedMS: 150}
	put(deadline)
	put(refused)
	withBench(t, map[string]string{envReplay: dir})
	do := func(u string, timeout time.Duration) (time.Duration, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		start := time.Now()
		_, err := New(0, 1).Do(req)
		return time.Since(start), err
	}
	if took, err := do(deadline.URL, 200*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || took < 200*time.Millisecond {
		t.Fatalf("recorded deadline = %v after %s, want the caller's deadline after 200ms", err, took)
	}
	if took, err := do(refused.URL, 5*time.Second); err == nil || !strings.Contains(err.Error(), "refused") || took < 150*time.Millisecond || took > 3*time.Second {
		t.Fatalf("recorded refusal = %v after %s, want it after its recorded 150ms", err, took)
	}
	if took, err := do(refused.URL, 20*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || took > 140*time.Millisecond {
		t.Fatalf("refusal under a shorter deadline = %v after %s, want the caller's deadline", err, took)
	}
}
