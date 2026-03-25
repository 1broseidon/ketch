package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/1broseidon/ketch/internal/config"
	"github.com/spf13/cobra"
)

// configInfo is the discovery payload returned by `ketch config`.
type configInfo struct {
	ConfigPath        string   `json:"config_path"`
	Backend           string   `json:"backend"`
	SearxngURL        string   `json:"searxng_url"`
	Limit             int      `json:"limit"`
	CacheTTL          string   `json:"cache_ttl"`
	AvailableBackends []string `json:"available_backends"`
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or manage configuration",
	Long:  `Display effective configuration as JSON, or manage the config file. The default output is a discovery payload showing all effective settings and available backends.`,
	RunE:  runConfigShow,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a default config file",
	RunE:  runConfigInit,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config value",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	RunE:  runConfigPath,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configPathCmd)
}

func runConfigShow(_ *cobra.Command, _ []string) error {
	c := config.Load()
	path, _ := config.Path()

	info := configInfo{
		ConfigPath:        path,
		Backend:           c.Backend,
		SearxngURL:        c.SearxngURL,
		Limit:             c.Limit,
		CacheTTL:          c.CacheTTL,
		AvailableBackends: config.AvailableBackends(),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

func runConfigInit(_ *cobra.Command, _ []string) error {
	path, err := config.Path()
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config already exists: %s", path)
	}

	if err := config.Save(config.Defaults()); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "created %s\n", path)
	return nil
}

func runConfigSet(_ *cobra.Command, args []string) error {
	c := config.Load()
	key, value := args[0], args[1]

	switch key {
	case "backend":
		c.Backend = value
	case "searxng_url":
		c.SearxngURL = value
	case "brave_api_key":
		c.BraveAPIKey = value
	case "limit":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("limit must be an integer: %w", err)
		}
		c.Limit = n
	case "cache_ttl":
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("cache_ttl must be a duration (e.g. 1h, 30m): %w", err)
		}
		c.CacheTTL = value
	default:
		return fmt.Errorf("unknown key: %s (valid: backend, searxng_url, brave_api_key, limit, cache_ttl)", key)
	}

	if err := config.Save(c); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "set %s = %s\n", key, value)
	return nil
}

func runConfigPath(_ *cobra.Command, _ []string) error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}
