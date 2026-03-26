package cmd

import (
	"fmt"
	"os"

	"github.com/1broseidon/ketch/internal/scrape"
	"github.com/spf13/cobra"
)

var browserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Manage browser for JS-rendered page support",
}

var browserInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Download Chromium for headless rendering",
	RunE:  runBrowserInstall,
}

var browserStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check browser configuration and availability",
	RunE:  runBrowserStatus,
}

func init() {
	rootCmd.AddCommand(browserCmd)
	browserCmd.AddCommand(browserInstallCmd)
	browserCmd.AddCommand(browserStatusCmd)
}

func runBrowserInstall(cmd *cobra.Command, args []string) error {
	fmt.Fprintln(os.Stderr, "Downloading Chromium...")
	path, err := scrape.InstallBrowser()
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Installed to: %s\n", path)
	fmt.Fprintf(os.Stderr, "Configure with: ketch config set browser %s\n", path)
	return nil
}

func runBrowserStatus(cmd *cobra.Command, args []string) error {
	if cfg.Browser == "" {
		fmt.Println("browser_config: (not set)")
		fmt.Println("status: disabled")
		return nil
	}

	fmt.Printf("browser_config: %s\n", cfg.Browser)

	bin, err := scrape.ResolveBrowserBin(cfg.Browser)
	if err != nil {
		fmt.Printf("status: error (%v)\n", err)
		return nil
	}
	fmt.Printf("browser_path: %s\n", bin)
	fmt.Println("status: ok")
	return nil
}
