package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/ketch/doctor"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the health of every backend, the browser, and the cache",
	Long: `Run cheap live health checks against every ketch surface: search backends
(brave/ddg/searxng/exa/firecrawl/keenable/tavily/parallel/serpbase/youcom), code backends
(grepapp/sourcegraph/github), docs (context7), the configured browser binary,
and the page cache.

Probes run concurrently with a per-check timeout, are read-only (nothing is
written to the cache), and each reports one of: ok, no_key, unreachable,
misconfigured (with a fix hint), or skipped.

Exit codes:
  0  every applicable check is ok or cleanly skipped
  5  at least one configured surface is broken — the default backend of a
     surface, a backend with an API key explicitly set, the configured
     browser, or the cache

Optional backends that merely lack an API key (no_key) do not fail the run.`,
	Args: exitArgs(cobra.NoArgs),
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	checks := doctor.Run(cmd.Context(), &cfg, doctor.DefaultTimeout)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(checks); err != nil {
			return err
		}
	} else {
		printDoctorReport(checks)
	}

	if n := blockingCount(checks); n > 0 {
		return exitErrf(ExitPrecondition, "doctor: %d blocking problem(s) found", n)
	}
	return nil
}

// blockingCount counts failing checks that gate the exit code.
func blockingCount(checks []doctor.Check) int {
	n := 0
	for _, c := range checks {
		if c.Required && c.Bad() {
			n++
		}
	}
	return n
}

func printDoctorReport(checks []doctor.Check) {
	var ok, skipped, problems, blocking int
	for _, c := range checks {
		switch c.Status {
		case doctor.StatusOK:
			ok++
		case doctor.StatusSkipped:
			skipped++
		default:
			problems++
			if c.Required {
				blocking++
			}
		}
	}

	fmt.Println("---")
	fmt.Printf("checks: %d\n", len(checks))
	fmt.Printf("ok: %d\n", ok)
	if skipped > 0 {
		fmt.Printf("skipped: %d\n", skipped)
	}
	if problems > 0 {
		fmt.Printf("problems: %d (%d blocking)\n", problems, blocking)
	}
	fmt.Println("---")

	for _, c := range checks {
		line := fmt.Sprintf("%-8s %-22s %-14s %6dms  %s",
			c.Surface, doctorBackendLabel(c), c.Status, c.LatencyMS, c.Detail)
		fmt.Println(strings.TrimRight(line, " "))
	}
}

// doctorBackendLabel marks the configured default backend of each search
// surface, mirroring the root summary's "(default)" annotation.
func doctorBackendLabel(c doctor.Check) string {
	switch {
	case c.Surface == "search" && c.Backend == cfg.Backend,
		c.Surface == "code" && c.Backend == cfg.CodeBackend,
		c.Surface == "docs" && c.Backend == cfg.DocsBackend:
		return c.Backend + " (default)"
	}
	return c.Backend
}
