package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveCaptureKeepsFailuresAndDetectsDrift(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
		}
		_, _ = w.Write([]byte("<main>Changed source</main>"))
	}))
	defer server.Close()
	fixtures := []fixture{
		{Spec: pageSpec{ID: "changed", URL: server.URL, HTMLSHA: digest([]byte("old"))}},
		{Spec: pageSpec{ID: "missing", URL: server.URL + "/missing"}},
	}
	dir := t.TempDir()
	err := runLive(context.Background(), fixtures, options{Out: dir, Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "1/2") {
		t.Fatalf("failed fetch disappeared: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "live.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Pages []liveResult `json:"pages"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Pages) != 2 || !result.Pages[0].SourceChanged || result.Pages[1].Status != 404 || result.Pages[1].Error == "" {
		t.Fatalf("bad live report: %+v", result.Pages)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing.html.gz")); !os.IsNotExist(err) {
		t.Fatal("error page stored as valid snapshot")
	}
}

func TestLiveCaptureRejectsNonHTMLAndEmptyBodies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ contentType, body string }{
		{"application/json", "{}"}, {"text/html", "   "},
	} {
		resp := &http.Response{StatusCode: 200, Header: make(http.Header)}
		resp.Header.Set("Content-Type", tc.contentType)
		if err := validateResponse(resp, []byte(tc.body), nil); err == nil {
			t.Fatalf("invalid snapshot accepted: %+v", tc)
		}
	}
}

func TestLiveCannotOverwritePinnedSnapshots(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "testdata")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	err := runLive(context.Background(), nil, options{Dir: dir, Out: path})
	if err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("live capture allowed to overwrite fixtures: %v", err)
	}
}
