package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/1broseidon/ketch/internal/updatecheck"
	"github.com/spf13/cobra"
)

type updateNoticeContextKey struct{}

var interactiveTerminalFn = isInteractiveTerminal

func currentVersion() string {
	v, _, _ := versionInfo()
	return v
}

// prepareUpdateNotice attaches an updatecheck.Status to the command
// context when a passive notice would be shown. A PersistentPostRun
// hook then reads it and prints. Splitting the two halves means the
// notice is only printed AFTER the command's real output, regardless
// of whether the command writes to stdout or stderr.
func prepareUpdateNotice(cmd *cobra.Command) {
	if shouldSkipPassiveUpdateNotice(cmd) {
		return
	}
	status, err := updatecheck.GetStatus(cmd.Context(), updatecheck.Options{
		CurrentVersion: currentVersion(),
		AllowNetwork:   true,
		Timeout:        400 * time.Millisecond,
	})
	if err != nil || !status.Available || !updatecheck.ShouldNotify(status) {
		return
	}
	baseCtx := cmd.Context()
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	cmd.SetContext(context.WithValue(baseCtx, updateNoticeContextKey{}, status))
}

func emitUpdateNotice(cmd *cobra.Command) {
	if cmd.Context() == nil {
		return
	}
	v := cmd.Context().Value(updateNoticeContextKey{})
	status, ok := v.(updatecheck.Status)
	if !ok {
		return
	}
	msg := updatecheck.FormatNotice(status)
	if msg == "" {
		return
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msg)
	_ = updatecheck.MarkNotified(status)
}

func shouldSkipPassiveUpdateNotice(cmd *cobra.Command) bool {
	if cmd == nil || updatecheck.Disabled() || !interactiveTerminalFn(os.Stderr) {
		return true
	}
	// `version` prints its own update line in-band; `help` is informational;
	// machine-readable output (--json) must not carry chatter on stderr of
	// the same magnitude either way, so skip.
	if cmd.Name() == "version" || cmd.Name() == "help" {
		return true
	}
	if !cmd.Runnable() {
		return true
	}
	if asJSON, _ := cmd.Root().PersistentFlags().GetBool("json"); asJSON {
		return true
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv("CI")))
	return v == "1" || v == "true"
}

func isInteractiveTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
