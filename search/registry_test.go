package search_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/doctor"
	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/testutil"
	ketchmcp "github.com/1broseidon/ketch/mcp"
	"github.com/1broseidon/ketch/search"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type registrySearch struct{}

func (registrySearch) Search(context.Context, string, int) ([]search.Result, error) {
	return []search.Result{{Title: "Registry proof", URL: "https://example.com/registry"}}, nil
}

func testProvider(calls *atomic.Int32) search.Provider {
	key := configbase.KeyPool("registryproof_api_key", "registryproof_api_keys")
	return search.Provider{
		ID: "registryproof", Name: "Registry Proof", Settings: []config.Setting{key},
		Setup:  "registryproof: API key not set",
		Usable: func(c *config.Config) bool { return len(key.Keys(c)) > 0 },
		New:    func(*config.Config) (search.Searcher, error) { return registrySearch{}, nil },
		Probe: func(_ context.Context, _ *http.Client, c *config.Config) (health.Status, string) {
			calls.Add(1)
			if len(key.Keys(c)) == 0 {
				return health.StatusNoKey, "API key not set"
			}
			return health.StatusOK, "fixture reached"
		},
	}
}

// This test deliberately imports real consumers. The fake provider and this
// one registry entry must be sufficient; no production consumer knows its ID.
func TestProviderRegistrationFlowsThroughEverySurface(t *testing.T) {
	testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_CONFIG", "")
	t.Setenv("PATH", "") // Do not resolve the operator's gh credentials.
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("KETCH_GITHUB_TOKEN", "")
	var calls atomic.Int32
	before := config.AvailableBackends()
	t.Cleanup(search.RegisterTestProvider(testProvider(&calls))) // the only registration
	if got := config.AvailableBackends(); !slices.Equal(got, append(before, "registryproof")) {
		t.Fatalf("registry order = %v", got)
	}
	cfg := config.Defaults()
	assertRegistryPools(t, &cfg, false)
	assertRegistryDiscovery(t, &cfg, false, 0)
	t.Setenv("KETCH_REGISTRYPROOF_API_KEY", "fixture-one,fixture-two,fixture-one")
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg = loaded.Config
	assertRegistryDiscovery(t, &cfg, true, 2)
	assertRegistryPools(t, &cfg, true)
	assertRegistrySecrets(t, &cfg)
	assertRegistryDoctor(t, &cfg, &calls)
	assertRegistryMCP(t, &cfg)
}

func assertRegistryDiscovery(t *testing.T, cfg *config.Config, set bool, count int) {
	t.Helper()
	fields := make(map[string]any)
	for _, field := range config.ProviderDiscovery(cfg) {
		fields[field.Name] = field.Value
	}
	if fields["registryproof_api_key_set"] != set || fields["registryproof_api_keys_count"] != count {
		t.Fatalf("provider discovery does not reflect configured keys")
	}
	data, err := json.Marshal(fields)
	if err != nil || strings.Contains(string(data), "fixture-one") || strings.Contains(string(data), "fixture-two") {
		t.Fatal("discovery leaked a fixture credential or failed to encode")
	}
}

func assertRegistryPools(t *testing.T, cfg *config.Config, want bool) {
	t.Helper()
	multi, err := search.NewMultiFromConfig(cfg, []string{"all"}, "")
	if err != nil {
		t.Fatal(err)
	}
	random, err := search.NewRandomFromConfig(cfg, []string{"all"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, names := range [][]string{multi.Names(), random.Names()} {
		if slices.Contains(names, "registryproof") != want {
			t.Fatalf("candidate pool = %v, want registered provider present=%v", names, want)
		}
	}
}

func assertRegistrySecrets(t *testing.T, cfg *config.Config) {
	t.Helper()
	for _, entry := range config.ScrubbedEnviron() {
		if strings.HasPrefix(entry, "KETCH_REGISTRYPROOF_API_KEY=") {
			t.Fatal("registered secret was inherited by a subprocess")
		}
	}
	if err := config.Save(*cfg); err != nil {
		t.Fatal(err)
	}
	loaded := config.LoadFile()
	if loaded.String("registryproof_api_key") != "fixture-one" || !slices.Equal(loaded.Strings("registryproof_api_keys"), []string{"fixture-two"}) {
		t.Fatal("registered settings did not survive the config-file round trip")
	}
}

type registryTransport struct{}

func (registryTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[]}`))}, nil
}

func assertRegistryDoctor(t *testing.T, cfg *config.Config, calls *atomic.Int32) {
	t.Helper()
	client := httpx.Default()
	previous := client.Transport
	client.Transport = registryTransport{}
	defer func() { client.Transport = previous }()
	checks := doctor.Run(context.Background(), cfg, time.Second)
	for _, check := range checks {
		if check.Backend == "registryproof" {
			if check.Status != health.StatusOK || !check.Required || calls.Load() != 1 {
				t.Fatalf("registered doctor probe = %+v, calls=%d", check, calls.Load())
			}
			return
		}
	}
	t.Fatal("doctor omitted the registered provider")
}

func assertRegistryMCP(t *testing.T, cfg *config.Config) {
	t.Helper()
	cfg.MCPTools = []string{"search"}
	server, err := ketchmcp.NewServer(cfg, "registry-test")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "registry-test", Version: "0"}, nil)
	ct, st := mcpsdk.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, st) }()
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || !strings.Contains(list.Tools[0].Description, "Registry Proof") {
		t.Fatal("MCP search description omitted the registered provider")
	}
	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "search", Arguments: map[string]any{"query": "registry", "backend": "registryproof"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) == 0 {
		t.Fatalf("MCP could not call the registered provider: %+v", result)
	}
}
