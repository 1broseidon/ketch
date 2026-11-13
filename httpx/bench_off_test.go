//go:build !bench

package httpx

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A release build (no bench tag) carries no record/replay hook: clients keep
// the plain *http.Transport, and the KETCH_HTTP_* variables change nothing.
func TestReleaseBuildIgnoresBenchHook(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KETCH_HTTP_RECORD", filepath.Join(dir, "record"))
	t.Setenv("KETCH_HTTP_REPLAY", filepath.Join(dir, "replay"))
	t.Setenv("KETCH_HTTP_LOG", filepath.Join(dir, "log"))
	for _, c := range []*http.Client{New(0, 1), Default()} {
		if _, ok := c.Transport.(*http.Transport); !ok {
			t.Fatalf("release client transport is %T, want *http.Transport", c.Transport)
		}
	}
	if _, ok := http.DefaultTransport.(*http.Transport); !ok {
		t.Fatalf("http.DefaultTransport is %T in a release build", http.DefaultTransport)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "live") }))
	defer srv.Close()
	resp, err := New(0, 1).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "live" {
		t.Fatalf("body %q", body)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("release build wrote bench files: %v", entries)
	}
}
