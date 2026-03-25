package cmd

import (
	"fmt"
	"strings"

	"github.com/1broseidon/ketch/internal/config"
	"github.com/spf13/cobra"
)

var cfg config.Config

var rootCmd = &cobra.Command{
	Use:   "ketch",
	Short: "Fast web search and scrape for agents",
	Long:  `ketch is a blazing fast CLI for agentic search and scrape workflows. Search the web, scrape pages to clean markdown, or do both in one shot.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cfg = config.Load()
	rootCmd.PersistentFlags().Bool("json", false, "output as JSON")
	rootCmd.PersistentFlags().StringP("backend", "b", cfg.Backend,
		fmt.Sprintf("search backend: %s", strings.Join(config.AvailableBackends(), ", ")))
}
