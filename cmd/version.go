package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/1broseidon/ketch/scrape"
	"github.com/1broseidon/ketch/updatecheck"
	"github.com/spf13/cobra"
)

// Build-time version info. Set via -ldflags "-X github.com/1broseidon/ketch/cmd.version=..."
// by goreleaser/CI. Defaults below are used for `go install` / dev builds,
// where we fall back to debug.ReadBuildInfo() to surface the module version
// and VCS stamp.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// versionInfo resolves version/commit/date, preferring linker-provided
// values and falling back to runtime build info so
// `go install github.com/1broseidon/ketch@vX.Y.Z` still reports correctly.
func versionInfo() (v, c, d string) {
	v, c, d = version, commit, date
	if v != "dev" && v != "" {
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		v = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if c == "" {
				c = s.Value
			}
		case "vcs.time":
			if d == "" {
				d = s.Value
			}
		}
	}
	return
}

func shortVersion() string {
	v, c, _ := versionInfo()
	if c != "" && len(c) >= 7 && (v == "dev" || v == "") {
		return fmt.Sprintf("dev (%s)", c[:7])
	}
	return v
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print ketch version information",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		v, c, d := versionInfo()
		asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
		status, _ := updatecheck.GetStatus(cmd.Context(), updatecheck.Options{
			CurrentVersion: v,
			AllowNetwork:   true,
			Timeout:        time.Second,
		})
		if asJSON {
			payload := map[string]any{
				"version": v,
				"commit":  c,
				"date":    d,
				"go":      runtime.Version(),
				"os":      runtime.GOOS,
				"arch":    runtime.GOARCH,
				"update":  status,
			}
			return json.NewEncoder(os.Stdout).Encode(payload)
		}
		fmt.Printf("ketch %s\n", v)
		if c != "" {
			fmt.Printf("  commit: %s\n", c)
		}
		if d != "" {
			fmt.Printf("  built:  %s\n", d)
		}
		fmt.Printf("  go:     %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
		if status.Available {
			fmt.Printf("\nUpdate available: %s\n", status.LatestVersion)
			if status.Command != "" {
				fmt.Printf("  command: %s\n", status.Command)
			} else if status.ReleaseURL != "" {
				fmt.Printf("  command: %s\n", status.ReleaseURL)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.Version = shortVersion()
	// Cobra emits "ketch version X" for --version by default; keep it short.
	rootCmd.SetVersionTemplate("ketch {{.Version}}\n")
	rootCmd.AddCommand(versionCmd)

	// Seed the scrape package's User-Agent version from build-time info so
	// the honest default UA stays in sync with the binary without scrape
	// depending on cmd. Local/dirty builds collapse to "dev" — the long
	// module pseudo-version is honest but uselessly noisy in a UA.
	scrape.Version = userAgentVersion()
}

// userAgentVersion returns the version token embedded in the default HTTP
// User-Agent. Release builds (ldflags) and clean tagged `go install`s get a
// semver; everything else is "dev".
func userAgentVersion() string {
	if version != "" && version != "dev" {
		return strings.TrimPrefix(version, "v")
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	mv := bi.Main.Version
	if mv == "" || mv == "(devel)" || strings.Contains(mv, "+") {
		return "dev"
	}
	// Go pseudo-versions look like vX.Y.Z-0.<timestamp>-<rev> (or
	// vX.0.0-<timestamp>-<rev>); real pre-releases like v1.2.3-rc.1 keep
	// their hyphenated form.
	trimmed := strings.TrimPrefix(mv, "v")
	if isPseudoVersion(trimmed) {
		return "dev"
	}
	return trimmed
}

func isPseudoVersion(v string) bool {
	_, rest, ok := strings.Cut(v, "-")
	if !ok {
		return false
	}
	// After the first hyphen a pseudo-version has either "0.<14 digits>" or
	// a bare 14-digit timestamp, then another hyphen and a revision.
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) != 2 {
		return false
	}
	ts := parts[0]
	if head, tail, cut := strings.Cut(ts, "."); cut {
		if head != "0" {
			return false
		}
		ts = tail
	}
	if len(ts) != 14 {
		return false
	}
	for _, c := range ts {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
