package cmd

import (
	ketchmcp "github.com/1broseidon/ketch/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run ketch as an MCP (Model Context Protocol) server",
	Long:  `Expose ketch's search, code, docs, scrape, crawl and tag capabilities as MCP tools, using the same config and backends as the CLI.`,
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server over stdio",
	Args:  cobra.NoArgs,
	RunE:  runMCPServe,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.AddCommand(mcpServeCmd)
}

func runMCPServe(cmd *cobra.Command, _ []string) error {
	v, _, _ := versionInfo()
	server, err := ketchmcp.NewServer(&cfg, v)
	if err != nil {
		return exitErrf(ExitPrecondition, "mcp server init failed: %w", err)
	}
	// The server holds shared, process-lifetime resources (headless browser,
	// bbolt cache handle); release them when Run returns — client disconnect
	// or ctx cancellation (SIGINT/SIGTERM via main.go's signal context).
	defer server.Close()
	if err := server.Run(cmd.Context(), &mcpsdk.StdioTransport{}); err != nil {
		return exitErrf(ExitUpstream, "mcp server failed: %w", err)
	}
	return nil
}
