package cmd

import (
	"fmt"

	"github.com/1broseidon/ketch/internal/failure"
	"github.com/spf13/cobra"
)

// Anvil · target: cmd/ · kind: cli · boundary: tool
// callers: agent,script · risk: R1 (additive exit-code map)
// contracts: exit-codes documented in conventions.yaml (forthcoming)

// Exit codes follow the table in references/cli-conventions.md and are
// surfaced to scripts/agents via os.Exit in main.go.
// The codes come from failure.Kind so the CLI and the MCP server cannot
// disagree on a class.
var (
	ExitValidation   = failure.Validation.ExitCode()   // 2: bad flag, missing/invalid arg, unknown key, unparseable value
	ExitNotFound     = failure.NotFound.ExitCode()     // 3: resource (URL, crawl id, selector match) absent
	ExitUpstream     = failure.Upstream.ExitCode()     // 4: network/fetch/backend failure outside our control
	ExitPrecondition = failure.Precondition.ExitCode() // 5: missing API key, already-exists, locked
	ExitCancelled    = failure.Cancelled.ExitCode()    // 6: SIGINT/SIGTERM observed
)

// ExitError carries a numeric exit code alongside the wrapped error.
// main.go inspects this via errors.As to translate RunE failures into the
// process exit code. Unwrapped errors continue to exit 1.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// exitErrf builds a freshly-formatted ExitError.
func exitErrf(code int, format string, args ...any) error {
	return &ExitError{Code: code, Err: fmt.Errorf(format, args...)}
}

// backendErr classifies a backend-constructor error (search/code/docs
// NewFromConfig) with failure.Classify: an unknown backend is validation
// (exit 2, the sentinel says so); anything else (missing API key/token,
// unimplemented backend) is a precondition failure (exit 5).
func backendErr(err error) error {
	return classified(err, failure.Precondition, "%w", err)
}

// upstreamErr classifies a failure from a live backend call with
// failure.Classify, the rule the MCP server applies too: the sentinels carry
// their own kind (library required → 2, not found → 3, ...), a deadline is
// an upstream timeout (4), cancellation is 6, and anything else is an
// upstream failure (4).
func upstreamErr(err error, format string, args ...any) error {
	return classified(err, failure.Upstream, format+": %w", append(args, err)...)
}

// classified formats an ExitError whose code is err's failure.Kind, or
// fallback when err carries none.
func classified(err error, fallback failure.Kind, format string, args ...any) error {
	c := failure.Classify(err, fallback)
	return &ExitError{Code: c.Kind.ExitCode(), Err: fmt.Errorf(format, args...)}
}

// exitArgs wraps a cobra arg validator so its rejection becomes exit 2 instead
// of the default exit 1. Apply once per command that uses MinimumNArgs/ExactArgs.
func exitArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if err := v(c, args); err != nil {
			return &ExitError{Code: ExitValidation, Err: err}
		}
		return nil
	}
}
