package cmd

import (
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/spf13/cobra"
)

// newTestSearchCmd builds an isolated command carrying only the flags
// runMultiSearch inspects, so each test controls Changed() independently of
// the shared searchCmd.
func newTestSearchCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().StringP("backend", "b", "brave", "")
	c.Flags().String("searxng-url", "", "")
	c.Flags().String("multi", "", "")
	c.Flags().Lookup("multi").NoOptDefVal = "all"
	c.Flags().String("random", "", "")
	c.Flags().Lookup("random").NoOptDefVal = "all"
	return c
}

// withDefaultConfig swaps the package cfg for key-less defaults so backend
// resolution is deterministic regardless of the operator's real config.
func withDefaultConfig(t *testing.T) {
	t.Helper()
	prev := cfg
	cfg = config.Defaults()
	t.Cleanup(func() { cfg = prev })
}

func TestParseMultiNames(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"all", []string{"all"}},
		{"", nil},
		{"brave,exa", []string{"brave", "exa"}},
		{" brave , exa ,brave", []string{"brave", "exa"}}, // trim + dedup
	}
	for _, tc := range cases {
		if got := parseMultiNames(tc.in); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("parseMultiNames(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRunMultiSearchFlagValidation(t *testing.T) {
	cases := []struct {
		name       string
		multi      string
		setBackend bool
		wantCode   int
		wantSubstr string
	}{
		{"multi and backend conflict", "brave", true, ExitValidation, "mutually exclusive"},
		{"all combined with a name", "all,brave", false, ExitValidation, `"all" cannot be combined`},
		{"unknown backend", "bogus", false, ExitValidation, "unknown search backend"},
		{"named but unconfigured", "firecrawl", false, ExitPrecondition, "firecrawl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDefaultConfig(t)
			c := newTestSearchCmd()
			if err := c.Flags().Set("multi", tc.multi); err != nil {
				t.Fatalf("set multi: %v", err)
			}
			if tc.setBackend {
				if err := c.Flags().Set("backend", "ddg"); err != nil {
					t.Fatalf("set backend: %v", err)
				}
			}
			err := runMultiSearch(c, "query", 5, false, false, false, 0, false)
			exitErr := asExitError(t, err)
			if exitErr.Code != tc.wantCode {
				t.Errorf("exit code = %d, want %d (err: %v)", exitErr.Code, tc.wantCode, err)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestResolveRandomNamesValidation(t *testing.T) {
	t.Run("random and backend", func(t *testing.T) {
		cmd := newTestSearchCmd()
		if err := cmd.Flags().Set("random", "all"); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Flags().Set("backend", "ddg"); err != nil {
			t.Fatal(err)
		}
		_, err := resolveRandomNames(cmd, "query")
		exitErr := asExitError(t, err)
		if exitErr.Code != ExitValidation || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("random and multi", func(t *testing.T) {
		cmd := newTestSearchCmd()
		if err := cmd.Flags().Set("random", "all"); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Flags().Set("multi", "all"); err != nil {
			t.Fatal(err)
		}
		_, err := resolveRandomNames(cmd, "query")
		exitErr := asExitError(t, err)
		if exitErr.Code != ExitValidation || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing equals hint", func(t *testing.T) {
		cmd := newTestSearchCmd()
		if err := cmd.Flags().Set("random", "all"); err != nil {
			t.Fatal(err)
		}
		_, err := resolveRandomNames(cmd, "brave,ddg")
		exitErr := asExitError(t, err)
		if exitErr.Code != ExitValidation || !strings.Contains(err.Error(), "--random=brave,ddg") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("bare random means all", func(t *testing.T) {
		cmd := newTestSearchCmd()
		if err := cmd.Flags().Set("random", "all"); err != nil {
			t.Fatal(err)
		}
		names, err := resolveRandomNames(cmd, "query")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(names, ",") != "all" {
			t.Fatalf("names = %v, want [all]", names)
		}
	})
}

func TestRunRandomSearchBackendValidation(t *testing.T) {
	cases := []struct {
		name       string
		random     string
		wantCode   int
		wantSubstr string
	}{
		{name: "unknown backend", random: "bogus", wantCode: ExitValidation, wantSubstr: "unknown search backend"},
		{name: "named but unconfigured", random: "firecrawl", wantCode: ExitPrecondition, wantSubstr: "firecrawl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDefaultConfig(t)
			cmd := newTestSearchCmd()
			if err := cmd.Flags().Set("random", tc.random); err != nil {
				t.Fatal(err)
			}
			err := runRandomSearch(cmd, "query", 5, false, false, false, 0, false)
			exitErr := asExitError(t, err)
			if exitErr.Code != tc.wantCode || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error = %v (exit %d), want exit %d containing %q", err, exitErr.Code, tc.wantCode, tc.wantSubstr)
			}
		})
	}
}

// TestLooksLikeBackendList pins the NoOptDefVal trap guard: a query that is
// exactly a comma-list of known backend names is flagged (the operator meant
// --multi=<list>); anything else — real queries with commas included — is not.
func TestLooksLikeBackendList(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"brave,exa", true},
		{"brave, exa", true},
		{"brave,ddg,searxng,exa,firecrawl,keenable,tavily", true},
		{"brave", false},             // single name: no comma, plausible query
		{"brave,notabackend", false}, // unknown member: real query
		{"go generics, explained", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeBackendList(tc.in); got != tc.want {
			t.Errorf("looksLikeBackendList(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
