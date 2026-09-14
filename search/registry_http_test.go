package search_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/search"
)

type providerFixtureTransport struct {
	t                                  *testing.T
	backend, method, endpoint, payload string
}

func (f providerFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != f.method || r.URL.Scheme+"://"+r.URL.Host+r.URL.Path != f.endpoint {
		f.t.Error("registry factory selected the wrong method or endpoint")
	}
	if f.backend == "serpbase" {
		if r.Header.Get("X-API-Key") != "registry-key" {
			f.t.Error("registry settings did not reach SerpBase's X-API-Key header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["q"] != "registry query" {
			f.t.Error("registry factory did not preserve the query")
		}
	} else {
		if r.Header.Get("Authorization") != "Bearer registry-key" {
			f.t.Error("registry settings did not reach the authorization header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["query"] != "registry query" {
			f.t.Error("registry factory did not preserve the query")
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(f.payload))}, nil
}

// These keyed providers cannot be live-tested without operator credentials.
// Start at the public factory and exercise the actual request and result parser.
func TestRegistryKeyedProvidersThroughHTTP(t *testing.T) {
	cases := []struct{ backend, method, endpoint, payload string }{
		{"firecrawl", "POST", "https://api.firecrawl.dev/v2/search", `{"success":true,"data":{"web":[{"title":"Fixture","url":"https://example.com/result","description":"Result text"}]}}`},
		{"tavily", "POST", "https://api.tavily.com/search", `{"results":[{"title":"Fixture","url":"https://example.com/result","content":"Result text"}]}`},
		{"serpbase", "POST", "https://api.serpbase.dev/google/search", `{"status":0,"organic":[{"title":"Fixture","link":"https://example.com/result","snippet":"Result text"}]}`},
	}
	client := httpx.Default()
	previous := client.Transport
	defer func() { client.Transport = previous }()
	for _, tc := range cases {
		t.Run(tc.backend, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.SetProvider(tc.backend+"_api_keys", []string{"registry-key"})
			client.Transport = providerFixtureTransport{t, tc.backend, tc.method, tc.endpoint, tc.payload}
			backend, err := search.NewFromConfig(&cfg, tc.backend, "")
			if err != nil {
				t.Fatal(err)
			}
			results, err := backend.Search(context.Background(), "registry query", 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].URL != "https://example.com/result" || results[0].Description != "Result text" {
				t.Fatalf("registry result mapping changed: %+v", results)
			}
		})
	}
}

// youcom is keyless by default, so its registry path is exercised through the
// public factory against the free-profile endpoint, with and without a key.
func TestRegistryYoucomThroughHTTP(t *testing.T) {
	client := httpx.Default()
	previous := client.Transport
	defer func() { client.Transport = previous }()

	for _, tc := range []struct {
		name     string
		keyed    bool
		endpoint string
	}{
		{name: "keyless free profile", keyed: false, endpoint: "https://api.you.com/mcp?profile=free"},
		{name: "authenticated", keyed: true, endpoint: "https://api.you.com/mcp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			if tc.keyed {
				cfg.SetProvider("youcom_api_keys", []string{"registry-key"})
			}
			client.Transport = youcomFixtureTransport{t, tc.keyed, tc.endpoint}
			backend, err := search.NewFromConfig(&cfg, "youcom", "")
			if err != nil {
				t.Fatal(err)
			}
			results, err := backend.Search(context.Background(), "registry query", 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].URL != "https://example.com/result" || results[0].Description != "Result text" {
				t.Fatalf("registry result mapping changed: %+v", results)
			}
		})
	}
}

type youcomFixtureTransport struct {
	t        *testing.T
	keyed    bool
	endpoint string
}

func (f youcomFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodPost || r.URL.Scheme+"://"+r.URL.Host+r.URL.Path != "https://api.you.com/mcp" || r.URL.RawQuery != youcomFixtureQuery(f.keyed) {
		f.t.Errorf("youcom request did not target %s", f.endpoint)
	}
	wantAuth := ""
	if f.keyed {
		wantAuth = "Bearer registry-key"
	}
	if got := r.Header.Get("Authorization"); got != wantAuth {
		f.t.Errorf("Authorization = %q, want %q", got, wantAuth)
	}
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil || req["method"] != "tools/call" {
		f.t.Error("youcom request is not a tools/call call")
	}
	params, _ := req["params"].(map[string]any)
	arguments, _ := params["arguments"].(map[string]any)
	if arguments["query"] != "registry query" {
		f.t.Errorf("query = %v, want registry query", arguments["query"])
	}
	text := `{"results":{"web":[{"title":"Fixture","url":"https://example.com/result","description":"Result text"}]}}`
	encoded, _ := json.Marshal(text)
	frames := fmt.Sprintf("data: {\"result\":{\"content\":[{\"type\":\"text\",\"text\":%s}]}}\n", encoded)
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(frames))}, nil
}

func youcomFixtureQuery(keyed bool) string {
	if keyed {
		return ""
	}
	return "profile=free"
}
