package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/1broseidon/ketch/internal/updatecheck"
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
}
