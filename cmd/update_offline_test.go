package cmd

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func TestOfflineSkipsUpdateNotice(t *testing.T) {
	t.Setenv("KETCH_NO_UPDATE_NOTIFIER", "")
	t.Setenv("CI", "")
	before := interactiveTerminalFn
	interactiveTerminalFn = func(*os.File) bool { return true }
	t.Cleanup(func() { interactiveTerminalFn = before })
	cmd := &cobra.Command{Use: "docs", Run: func(*cobra.Command, []string) {}}
	cmd.Flags().Bool("offline", true, "")
	if !shouldSkipPassiveUpdateNotice(cmd) {
		t.Fatal("offline would perform an update check")
	}
}
