package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/1broseidon/ketch/internal/crawl"
	"github.com/spf13/cobra"
)

var crawlStatusCmd = &cobra.Command{
	Use:   "status [id]",
	Short: "Show background crawl status",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCrawlStatusCmd,
}

var crawlStopCmd = &cobra.Command{
	Use:   "stop <id>",
	Short: "Stop a running background crawl",
	Args:  cobra.ExactArgs(1),
	RunE:  runCrawlStopCmd,
}

func init() {
	crawlCmd.AddCommand(crawlStatusCmd)
	crawlCmd.AddCommand(crawlStopCmd)
}

func runCrawlBackground(args []string) error {
	id := crawl.GenerateCrawlID()

	status := &crawl.CrawlStatus{
		ID:        id,
		Seed:      args[0],
		Status:    "starting",
		StartedAt: time.Now(),
	}
	if err := crawl.WriteStatus(status); err != nil {
		return fmt.Errorf("write status: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable: %w", err)
	}

	childCmd := exec.Command(exe, os.Args[1:]...)
	childCmd.Env = append(os.Environ(), "KETCH_CRAWL_WORKER="+id)
	setDetached(childCmd)

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open devnull: %w", err)
	}
	childCmd.Stdin = devNull
	childCmd.Stdout = devNull
	childCmd.Stderr = devNull

	if err := childCmd.Start(); err != nil {
		status.Status = "failed"
		status.Error = err.Error()
		_ = crawl.WriteStatus(status)
		return fmt.Errorf("failed to start background crawl: %w", err)
	}

	fmt.Printf("crawl_id: %s\n", id)
	fmt.Fprintf(os.Stderr, "Background crawl started. Check status with: ketch crawl status %s\n", id)
	return nil
}

func runCrawlWorker(cmd *cobra.Command, args []string, crawlID string) error {
	seed := args[0]
	depth, _ := cmd.Flags().GetInt("depth")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	allow, _ := cmd.Flags().GetStringSlice("allow")
	deny, _ := cmd.Flags().GetStringSlice("deny")
	sitemap, _ := cmd.Flags().GetBool("sitemap")
	noCache, _ := cmd.Flags().GetBool("no-cache")

	status := &crawl.CrawlStatus{
		ID:        crawlID,
		PID:       os.Getpid(),
		Seed:      seed,
		Status:    "running",
		StartedAt: time.Now(),
	}
	_ = crawl.WriteStatus(status)

	// Signal handling for graceful stop: SIGTERM/SIGINT cancels the crawl ctx.
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	notifyStop(sigCh)
	go func() {
		<-sigCh
		cancel()
	}()

	pc := newCrawlCache(noCache)
	defer pc.Close()
	opts := crawl.Options{
		Depth:       depth,
		Concurrency: concurrency,
		Allow:       allow,
		Deny:        deny,
		BrowserBin:  cfg.Browser,
	}

	var mu sync.Mutex
	fn := func(r crawl.Result) {
		mu.Lock()
		defer mu.Unlock()
		status.Pages++
		if r.Error != "" {
			status.Errors++
		} else {
			switch r.Status {
			case "new":
				status.New++
			case "changed":
				status.Changed++
			case "unchanged":
				status.Unchanged++
			}
		}
		// Update status file every 10 pages
		if status.Pages%10 == 0 {
			_ = crawl.WriteStatus(status)
		}
	}

	crawlErr := crawl.Crawl(ctx, seed, opts, pc, sitemap, fn)

	// Write final status. Cancellation wins over the returned error: a
	// ctx-cancelled crawl returns context.Canceled, which is "stopped",
	// not "failed".
	mu.Lock()
	switch {
	case ctx.Err() != nil:
		status.Status = "stopped"
	case crawlErr != nil:
		status.Status = "failed"
		status.Error = crawlErr.Error()
	default:
		status.Status = "completed"
	}
	_ = crawl.WriteStatus(status)
	mu.Unlock()

	return nil
}

func runCrawlStatusCmd(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	if len(args) == 1 {
		return showCrawlStatus(args[0], asJSON)
	}
	return listCrawlStatuses(asJSON)
}

func showCrawlStatus(id string, asJSON bool) error {
	status, err := crawl.ReadStatus(id)
	if err != nil {
		return fmt.Errorf("crawl %s not found: %w", id, err)
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(status)
	}

	fmt.Println("---")
	fmt.Printf("id: %s\n", status.ID)
	fmt.Printf("seed: %s\n", status.Seed)
	fmt.Printf("status: %s\n", status.Status)
	fmt.Printf("pages: %d\n", status.Pages)
	fmt.Printf("new: %d\n", status.New)
	fmt.Printf("changed: %d\n", status.Changed)
	fmt.Printf("unchanged: %d\n", status.Unchanged)
	fmt.Printf("errors: %d\n", status.Errors)
	fmt.Printf("started_at: %s\n", status.StartedAt.Format(time.RFC3339))
	fmt.Printf("updated_at: %s\n", status.UpdatedAt.Format(time.RFC3339))
	if status.Error != "" {
		fmt.Printf("error: %s\n", status.Error)
	}
	fmt.Println("---")
	return nil
}

func listCrawlStatuses(asJSON bool) error {
	statuses, err := crawl.ListStatuses()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		fmt.Fprintln(os.Stderr, "No crawls found.")
		return nil
	}

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(statuses)
	}

	for _, s := range statuses {
		fmt.Printf("%s  %-9s  %-40s  pages=%-5d  %s\n",
			s.ID, s.Status, s.Seed, s.Pages, s.UpdatedAt.Format(time.RFC3339))
	}
	return nil
}

func runCrawlStopCmd(cmd *cobra.Command, args []string) error {
	id := args[0]
	status, err := crawl.ReadStatus(id)
	if err != nil {
		return fmt.Errorf("crawl %s not found: %w", id, err)
	}
	if status.Status != "running" {
		return fmt.Errorf("crawl %s is not running (status: %s)", id, status.Status)
	}

	proc, err := os.FindProcess(status.PID)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", status.PID, err)
	}
	if err := sendStop(proc); err != nil {
		return fmt.Errorf("failed to stop crawl: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Sent stop signal to crawl %s (pid %d)\n", id, status.PID)
	return nil
}
