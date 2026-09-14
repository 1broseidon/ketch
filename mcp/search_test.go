package mcp

import (
	"context"
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
