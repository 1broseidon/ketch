package testutil

import (
	"cmp"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/httpx"
)

// writeFixtures lays files out under a temporary directory.
func writeFixtures(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func get(t *testing.T, c *http.Client, url string) (*http.Response, string) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func TestFixtureTransportDirectory(t *testing.T) {
	dir := writeFixtures(t, map[string]string{
		"pypi.org/pypi/click/json":                                          `{"info": {"name": "click"}}`,
		"docs.example.com/__index__":                                        `<!DOCTYPE html><a href="{{ORIGIN}}/guide/">Guide</a>`,
		"docs.example.com/guide/page.html":                                  "<p>page</p>",
		"docs.example.com/objects.inv":                                      "# Sphinx inventory version 2\n",
		"docs.example.com/en/stable/__index__.meta.json":                    `{"location": "{{ORIGIN}}/en/6.1/"}`,
		"docs.example.com/en/6.1/__index__":                                 "<title>6.1</title>",
		"docs.example.com/gone.meta.json":                                   `{"status": 404}`,
		"docs.example.com/llms.txt":                                         "# Example",
		"docs.example.com/llms.txt.meta.json":                               `{"headers": {"Content-Type": "text/markdown", "X-Origin": "{{ORIGIN}}"}}`,
		"api.github.com/search/repositories%3Fq=solid+in%3Aname&sort=stars": `{"items": []}`,
		"crates.io/api/v1/crates/serde%3Finclude=":                          `{"crate": {"id": "serde"}}`,
		"docs.example.com/.DS_Store":                                        "junk",
	})
	ft := LoadFixtures(t, dir)
	c := ft.Client()

	cases := []struct {
		name, url  string
		status     int
		body       string
		header     map[string]string // header values the response must carry
		servedFrom string            // where the client ended up; "" for url
	}{
		{name: "json by content", url: "https://pypi.org/pypi/click/json", status: 200, body: `{"info": {"name": "click"}}`, header: map[string]string{"Content-Type": "application/json"}},
		{name: "root index, origin templated", url: "https://docs.example.com/", status: 200, body: `<!DOCTYPE html><a href="https://docs.example.com/guide/">Guide</a>`, header: map[string]string{"Content-Type": "text/html; charset=utf-8"}},
		{name: "type by extension", url: "https://docs.example.com/guide/page.html", status: 200, body: "<p>page</p>", header: map[string]string{"Content-Type": "text/html; charset=utf-8"}},
		{name: "binary by extension", url: "https://docs.example.com/objects.inv", status: 200, body: "# Sphinx inventory version 2\n", header: map[string]string{"Content-Type": "application/octet-stream"}},
		{name: "meta-only redirect", url: "https://docs.example.com/en/stable/", status: 200, body: "<title>6.1</title>", servedFrom: "https://docs.example.com/en/6.1/"},
		{name: "meta-only 404", url: "https://docs.example.com/gone", status: 404, body: ""},
		{name: "meta headers", url: "https://docs.example.com/llms.txt", status: 200, body: "# Example", header: map[string]string{"Content-Type": "text/markdown", "X-Origin": "https://docs.example.com"}},
		// Query parameters match in any order and encoding.
		{name: "query", url: "https://api.github.com/search/repositories?sort=stars&q=solid+in:name", status: 200, body: `{"items": []}`},
		{name: "empty query value", url: "https://crates.io/api/v1/crates/serde?include=", status: 200, body: `{"crate": {"id": "serde"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := get(t, c, tc.url)
			if resp.StatusCode != tc.status || body != tc.body {
				t.Errorf("got %d %q, want %d %q", resp.StatusCode, body, tc.status, tc.body)
			}
			for k, v := range tc.header {
				if got := resp.Header.Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
			if want := cmp.Or(tc.servedFrom, tc.url); resp.Request.URL.String() != want {
				t.Errorf("served from %s, want %s", resp.Request.URL, want)
			}
		})
	}
	if _, err := c.Get("https://docs.example.com/.DS_Store"); err == nil {
		t.Error("dotfiles must not become fixtures")
	}
	if got := ft.Unmatched(); !slices.Equal(got, []string{"GET https://docs.example.com/.DS_Store"}) {
		t.Errorf("unmatched = %v", got)
	}
}

func TestFixtureTransportUnmatchedNamesURL(t *testing.T) {
	ft := NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://registry.npmjs.org/react/latest", Fixture{Body: "{}"})
	_, err := ft.Client().Get("https://registry.npmjs.org/solid/latest")
	if err == nil || !strings.Contains(err.Error(), "no fixture for GET https://registry.npmjs.org/solid/latest") {
		t.Fatalf("err = %v", err)
	}
	// The method is part of the key.
	req, _ := http.NewRequest(http.MethodHead, "https://registry.npmjs.org/react/latest", nil)
	if _, err := ft.RoundTrip(req); err == nil || !strings.Contains(err.Error(), "HEAD https://registry.npmjs.org/react/latest") {
		t.Errorf("HEAD without a fixture: %v", err)
	}
	calls := ft.Calls()
	if len(calls) != 2 || calls[0].Matched || calls[0].String() != "GET https://registry.npmjs.org/solid/latest" {
		t.Errorf("calls = %v", calls)
	}
}

func TestFixtureTransportRecordsHeaders(t *testing.T) {
	ft := NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://api.github.com/repos/pallets/click", Fixture{Status: 403, Body: `{"message": "rate limited"}`, Header: http.Header{"X-Ratelimit-Remaining": {"0"}}})
	req, _ := http.NewRequest(http.MethodGet, "https://API.github.com/repos/pallets/click#readme", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := ft.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 || resp.Status != "403 Forbidden" || resp.Header.Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("response = %s %v", resp.Status, resp.Header)
	}
	calls := ft.Calls()
	if len(calls) != 1 || !calls[0].Matched || calls[0].Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestFixtureTransportErrAndDelay(t *testing.T) {
	ft := NewFixtureTransport()
	boom := errors.New("connection refused")
	ft.Handle(http.MethodGet, "https://down.example.com/", Fixture{Err: boom})
	ft.Handle(http.MethodGet, "https://slow.example.com/", Fixture{Delay: time.Minute, Body: "late"})
	if _, err := ft.Client().Get("https://down.example.com/"); !errors.Is(err, boom) {
		t.Errorf("Err fixture: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://slow.example.com/", nil)
	start := time.Now()
	if _, err := ft.Client().Do(req); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Delay must end with the context: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("Delay ignored the context")
	}
}

func TestFixtureTransportLoadErrors(t *testing.T) {
	if err := NewFixtureTransport().LoadDir(writeFixtures(t, map[string]string{"x.meta.json": `{"status": 200}`})); err == nil {
		t.Error("a file with no host directory must be rejected")
	}
	if err := NewFixtureTransport().LoadDir(writeFixtures(t, map[string]string{"h.example/a.meta.json": `{"statu": 200}`})); err == nil {
		t.Error("an unknown meta field must be rejected")
	}
}

func TestSwapDefaultTransport(t *testing.T) {
	before := httpx.Default().Transport
	t.Run("swapped", func(t *testing.T) {
		ft := NewFixtureTransport()
		ft.Handle(http.MethodGet, "https://context7.com/api/v1/search?query=react", Fixture{Body: `{"results": []}`})
		SwapDefaultTransport(t, ft)
		if _, body := get(t, httpx.Default(), "https://context7.com/api/v1/search?query=react"); body != `{"results": []}` {
			t.Errorf("body = %q", body)
		}
		if _, err := httpx.Default().Get("https://example.com/"); err == nil {
			t.Error("the swapped client must not reach the network")
		}
	})
	if httpx.Default().Transport != before {
		t.Error("cleanup must restore the transport")
	}
}
