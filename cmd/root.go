package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/ketch/internal/config"
	"github.com/spf13/cobra"
)

var cfg = config.Load()

var rootCmd = &cobra.Command{
	Use:   "ketch",
	Short: "Fast web search and scrape for agents",
	Long:  `ketch is a blazing fast CLI for agentic search and scrape workflows. Search the web, scrape pages to clean markdown, or do both in one shot.`,
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		prepareUpdateNotice(cmd)
	},
	PersistentPostRun: func(cmd *cobra.Command, _ []string) {
		emitUpdateNotice(cmd)
	},
	RunE:         runRoot,
	SilenceUsage: true,
}

func Execute() error {
	return rootCmd.Execute()
}

// ExecuteContext runs the root command with a caller-supplied context.
// main.go passes a context that cancels on SIGINT/SIGTERM so foreground
// commands (notably crawl) can shut down gracefully.
func ExecuteContext(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.PersistentFlags().Bool("json", false, "output as JSON")
	rootCmd.PersistentFlags().StringP("backend", "b", cfg.Backend,
		fmt.Sprintf("search backend: %s", strings.Join(config.AvailableBackends(), ", ")))
}

func runRoot(cmd *cobra.Command, _ []string) error {
	// Print a compact, generated summary — derived entirely from the live
	// command tree and config backend lists so it can never drift.
	w := cmd.OutOrStdout()
	p := func(format string, a ...any) { fmt.Fprintf(w, format, a...) } //nolint:errcheck

	p("ketch — web search, code search, docs, and scrape in one binary.\n\n")

	p("Commands:\n")
	// Surface-first ordering: main research commands first, then supporting ones.
	order := []string{"search", "code", "docs", "scrape", "crawl", "browser", "cache", "config"}
	byName := make(map[string]*cobra.Command, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		byName[sub.Name()] = sub
	}
	seen := make(map[string]bool)
	for _, name := range order {
		if sub, ok := byName[name]; ok {
			p("  %-10s  %s\n", sub.Name(), sub.Short)
			seen[name] = true
		}
	}
	// Any commands added later that aren't in the priority list appear at the end.
	for _, sub := range cmd.Commands() {
		if !seen[sub.Name()] && sub.Name() != "help" && sub.Name() != "completion" {
			p("  %-10s  %s\n", sub.Name(), sub.Short)
		}
	}

	p("\nBackends:\n")
	p("  %-10s  %s\n", "search", joinWithDefault(config.AvailableBackends(), cfg.Backend))
	p("  %-10s  %s\n", "code", joinWithDefault(config.AvailableCodeBackends(), cfg.CodeBackend))
	p("  %-10s  %s\n", "docs", joinWithDefault(config.AvailableDocBackends(), cfg.DocsBackend))

	p("\nRun 'ketch <command> --help' for flags and examples.\n")
	return nil
}

// joinWithDefault formats a backend list, marking the active one with "(default)".
func joinWithDefault(backends []string, active string) string {
	out := make([]string, len(backends))
	for i, b := range backends {
		if b == active {
			out[i] = b + " (default)"
		} else {
			out[i] = b
		}
	}
	return strings.Join(out, ", ")
}
