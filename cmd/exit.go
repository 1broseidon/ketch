package cmd

import (
	"errors"
	"fmt"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/docs"
	"github.com/spf13/cobra"
)

// Anvil · target: cmd/ · kind: cli · boundary: tool
// callers: agent,script · risk: R1 (additive exit-code map)
// contracts: exit-codes documented in conventions.yaml (forthcoming)

// Exit codes follow the table in references/cli-conventions.md and are
// surfaced to scripts/agents via os.Exit in main.go.
const (
	ExitValidation   = 2 // bad flag, missing/invalid arg, unknown key, unparseable value
	ExitNotFound     = 3 // resource (URL, crawl id, selector match) absent
	ExitUpstream     = 4 // network/fetch/backend failure outside our control
	ExitPrecondition = 5 // missing API key, already-exists, locked
	ExitCancelled    = 6 // SIGINT/SIGTERM observed
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
// NewFromConfig): errors wrapping the package's unknown-backend sentinel are
// validation failures (exit 2); anything else (missing API key/token,
// unimplemented backend) is a precondition failure (exit 5).
func backendErr(err, unknown error) error {
	if errors.Is(err, unknown) {
		return &ExitError{Code: ExitValidation, Err: err}
	}
	return &ExitError{Code: ExitPrecondition, Err: err}
}

// upstreamErr classifies a failure from a live backend call: errors wrapping
// docs.ErrNotFound or code.ErrRepoNotFound are permanently absent resources
// (exit 3, not retryable); everything else is an upstream failure (exit 4).
// The sentinels are detected in the docs and code packages themselves, so the
// CLI and the MCP server (which applies the same rule in its upstreamErrf)
// cannot diverge.
func upstreamErr(err error, format string, args ...any) error {
	exit := ExitUpstream
	if errors.Is(err, docs.ErrNotFound) || errors.Is(err, code.ErrRepoNotFound) {
		exit = ExitNotFound
	}
	return &ExitError{Code: exit, Err: fmt.Errorf(format+": %w", append(args, err)...)}
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
