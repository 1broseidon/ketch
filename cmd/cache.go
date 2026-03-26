package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/1broseidon/ketch/internal/cache"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Show cache stats",
	RunE:  runCacheStats,
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove all cached pages",
	RunE:  runCacheClear,
}

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheClearCmd)
}

func runCacheStats(_ *cobra.Command, _ []string) error {
	dbPath, _ := cache.DBPath()

	// Try read-only open; falls back to file stats if DB is locked
	c := cache.NewReadOnly()
	if c != nil {
		defer c.Close()
		entries, bytes := c.Stats()
		fmt.Println("---")
		fmt.Printf("path: %s\n", dbPath)
		fmt.Printf("entries: %d\n", entries)
		fmt.Printf("size: %s\n", formatBytes(bytes))
		fmt.Printf("ttl: %s\n", cfg.CacheTTL)
		fmt.Println("---")
		return nil
	}

	// DB locked by another process (e.g. background crawl)
	fmt.Println("---")
	fmt.Printf("path: %s\n", dbPath)
	if info, err := os.Stat(dbPath); err == nil {
		fmt.Printf("size: %s\n", formatBytes(info.Size()))
	}
	fmt.Printf("ttl: %s\n", cfg.CacheTTL)
	fmt.Println("note: cache in use by another process")
	fmt.Println("---")
	return nil
}

func runCacheClear(_ *cobra.Command, _ []string) error {
	ttl, err := time.ParseDuration(cfg.CacheTTL)
	if err != nil {
		ttl = time.Hour
	}
	c := cache.New(ttl)
	if c == nil {
		return fmt.Errorf("cannot open cache (may be in use by another process)")
	}
	defer c.Close()
	if err := c.Clear(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "cache cleared")
	return nil
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
