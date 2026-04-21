package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/1broseidon/ketch/cmd"
)

func main() {
	// Cancel the root context on SIGINT/SIGTERM so foreground commands
	// (notably `ketch crawl`) can shut down gracefully: workers exit,
	// in-flight HTTP requests abort, and the process returns instead of
	// being hard-killed by the default signal handler.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
