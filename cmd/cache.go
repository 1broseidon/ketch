package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/1broseidon/ketch/cache"
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

// cacheStatsInfo is the stable JSON payload for `ketch cache --json`.
// Entries is null and Locked is true when another process holds the cache
// lock — entry counts can't be read then, but file size still can.
type cacheStatsInfo struct {
	Path      string        `json:"path"`
	Entries   *int          `json:"entries"`
	SizeBytes int64         `json:"size_bytes"`
	Size      string        `json:"size"`
	TTL       string        `json:"ttl"`
	Locked    bool          `json:"locked"`
	Tags      tagIndexStats `json:"tags"`
}

// tagIndexStats is the tag-index portion of `ketch cache`'s output. tags.db
// is a separate bbolt file from the page cache (see ADR-0004), so it gets
// its own path and its own locked/count fields rather than sharing the page
// cache's. Count/Entries are null (and Locked true) only when another
// process holds the tags.db lock; a tag index that has simply never been
// created reports zero, not null.
type tagIndexStats struct {
	Path    string `json:"path"`
	Count   *int   `json:"count"`
	Entries *int   `json:"entries"`
	Locked  bool   `json:"locked"`
}

func runCacheStats(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	dbPath, _ := cache.DBPath()

	info := cacheStatsInfo{Path: dbPath, TTL: cfg.CacheTTL}
	// Try read-only open; falls back to file stats if DB is locked by
	// another process (e.g. a background crawl).
	if c := cache.NewReadOnly(); c != nil {
		defer c.Close()
		entries, bytes := c.Stats()
		info.Entries = &entries
		info.SizeBytes = bytes
	} else {
		info.Locked = true
		if st, err := os.Stat(dbPath); err == nil {
			info.SizeBytes = st.Size()
		}
	}
	info.Size = formatBytes(info.SizeBytes)
	info.Tags = tagIndexInfo()

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(info)
	}

	fmt.Println("---")
	fmt.Printf("path: %s\n", info.Path)
	if info.Entries != nil {
		fmt.Printf("entries: %d\n", *info.Entries)
	}
	fmt.Printf("size: %s\n", info.Size)
	fmt.Printf("ttl: %s\n", info.TTL)
	if info.Locked {
		fmt.Println("note: cache in use by another process")
	}
	fmt.Printf("tags_path: %s\n", info.Tags.Path)
	if info.Tags.Count != nil {
		fmt.Printf("tags: %d\n", *info.Tags.Count)
		fmt.Printf("tag_entries: %d\n", *info.Tags.Entries)
	}
	if info.Tags.Locked {
		fmt.Println("note: tag index in use by another process")
	}
	fmt.Println("---")
	return nil
}

// tagIndexInfo reads the tag index read-only, the same as the page-cache
// stats above: a nonexistent file is zero tags (not an error), and a
// locked file reports Locked rather than blocking `ketch cache` on it.
func tagIndexInfo() tagIndexStats {
	path, err := cache.TagsPath()
	out := tagIndexStats{Path: path}
	if err != nil {
		return out
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		zero := 0
		out.Count, out.Entries = &zero, &zero
		return out
	}
	tags, entries, err := cache.NewTagIndex(0, nil).TagCounts()
	if err != nil {
		out.Locked = true
		return out
	}
	out.Count, out.Entries = &tags, &entries
	return out
}

func runCacheClear(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	ttl, err := time.ParseDuration(cfg.CacheTTL)
	if err != nil {
		ttl = time.Hour
	}
	c := cache.New(ttl)
	if c == nil {
		return exitErrf(ExitPrecondition, "cannot open cache (may be in use by another process)")
	}
	defer c.Close()
	if err := c.Clear(); err != nil {
		return err
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Cleared bool `json:"cleared"`
		}{true})
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
