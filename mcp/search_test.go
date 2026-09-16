package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
)

// These cases all fail during input validation or backend resolution — before
// any network call — so a bare Server with just a config is enough to exercise
// the multi/backend prefix contract.
func TestRunSearchMultiValidation(t *testing.T) {
	cfg := config.Defaults() // no API keys set
	s := &Server{cfg: &cfg}

	cases := []struct {
		name       string
		in         SearchInput
		wantPrefix string
		wantSubstr string
	}{
		{
			name:       "empty query",
			in:         SearchInput{},
			wantPrefix: "[validation]",
			wantSubstr: "query is required",
		},
		{
			name:       "multi and backend mutually exclusive",
			in:         SearchInput{Query: "q", Multi: []string{"brave"}, Backend: "ddg"},
			wantPrefix: "[validation]",
			wantSubstr: "mutually exclusive",
		},
		{
			name:       "all combined with a name",
			in:         SearchInput{Query: "q", Multi: []string{"all", "brave"}},
			wantPrefix: "[validation]",
			wantSubstr: `"all" cannot be combined`,
		},
		{
			name:       "unknown backend name",
			in:         SearchInput{Query: "q", Multi: []string{"bogus"}},
			wantPrefix: "[validation]",
			wantSubstr: "unknown search backend",
		},
		{
			name:       "named but unconfigured backend",
			in:         SearchInput{Query: "q", Multi: []string{"tavily"}},
			wantPrefix: "[precondition]",
			wantSubstr: "tavily",
		},
		{
			name:       "random and backend mutually exclusive",
			in:         SearchInput{Query: "q", Random: []string{"brave"}, Backend: "ddg"},
			wantPrefix: "[validation]",
			wantSubstr: "mutually exclusive",
		},
		{
			name:       "random and multi mutually exclusive",
			in:         SearchInput{Query: "q", Random: []string{"brave"}, Multi: []string{"ddg"}},
			wantPrefix: "[validation]",
			wantSubstr: "mutually exclusive",
		},
		{
			name:       "random all combined with name",
			in:         SearchInput{Query: "q", Random: []string{"all", "brave"}},
			wantPrefix: "[validation]",
			wantSubstr: `"all" cannot be combined`,
		},
		{
			name:       "random unknown backend",
			in:         SearchInput{Query: "q", Random: []string{"bogus"}},
			wantPrefix: "[validation]",
			wantSubstr: "unknown search backend",
		},
		{
			name:       "random named but unconfigured backend",
			in:         SearchInput{Query: "q", Random: []string{"tavily"}},
			wantPrefix: "[precondition]",
			wantSubstr: "tavily",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.runSearch(context.Background(), tc.in)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.HasPrefix(err.Error(), tc.wantPrefix+" ") {
				t.Errorf("error = %q, want prefix %q", err.Error(), tc.wantPrefix)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

// TestCleanMultiNames covers the trim/dedup normalization the handler applies.
func TestRunSearchRandomAllPreservesCancellation(t *testing.T) {
	cfg := config.Defaults()
	s := &Server{cfg: &cfg}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.runSearch(ctx, SearchInput{Query: "q", Random: []string{"all"}})
	if err == nil || !strings.HasPrefix(err.Error(), "[cancelled] ") {
		t.Fatalf("error = %v, want [cancelled]", err)
	}
}

func TestCleanMultiNames(t *testing.T) {
	got := cleanMultiNames([]string{" brave ", "ddg", "brave", "", "exa"})
	if strings.Join(got, ",") != "brave,ddg,exa" {
		t.Errorf("cleanMultiNames = %v, want [brave ddg exa]", got)
	}
}

// An agent that only sees "auto" cannot tell a healthy install from one
// limping on its last fallback, so the tool result names the provider that
// actually served and reports the ones it fell through.
func TestRunSearchAutoReportsServingBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"title":"Served","url":"https://example.com/served","content":"body"}]}`)
	}))
	defer server.Close()

	// A configured instance is promoted to the front of the chain, so this
	// resolves without reaching any hosted provider.
	cfg := config.Defaults()
	cfg.SetProvider("searxng_url", server.URL)
	s := &Server{cfg: &cfg}

	out, err := s.runSearch(context.Background(), SearchInput{Query: "q", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if out.Backend != "searxng" {
		t.Fatalf("Backend = %q, want the serving provider, not %q", out.Backend, cfg.Backend)
	}
	if len(out.Results) != 1 || out.Results[0].URL != "https://example.com/served" {
		t.Fatalf("Results = %+v", out.Results)
	}
	// Nothing failed ahead of it, so the additive errors map stays absent.
	if len(out.Errors) != 0 {
		t.Fatalf("Errors = %v, want none", out.Errors)
	}
}

// An explicitly named provider is not a chain, so its output shape is
// unchanged: no backend field, no errors map.
func TestRunSearchExplicitBackendOmitsChainMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"title":"Served","url":"https://example.com/served"}]}`)
	}))
	defer server.Close()

	cfg := config.Defaults()
	s := &Server{cfg: &cfg}

	out, err := s.runSearch(context.Background(), SearchInput{Query: "q", Limit: 1, Backend: "searxng", SearxngURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if out.Backend != "" || len(out.Errors) != 0 {
		t.Fatalf("Backend=%q Errors=%v, want both empty for a single provider", out.Backend, out.Errors)
	}
	if len(out.Results) != 1 {
		t.Fatalf("Results = %+v", out.Results)
	}
}
