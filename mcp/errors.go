package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/docs"
)

// Error kinds mirror the CLI's documented exit codes (cmd/exit.go: 2
// validation, 3 not-found, 4 upstream, 5 precondition, 6 cancelled) so an
// agent can tell "fix your input" from "retry later" from "the operator must
// configure something". Every tool error message starts with a stable
// machine-readable prefix: "[validation] ...", "[not_found] ...",
// "[upstream] ...", "[precondition] ...", or "[cancelled] ...".
//
// The go-sdk carries tool-handler errors back to the client as a
// CallToolResult with IsError=true and the error string as text content —
// there is no structured error-data field on tool results in MCP, so the
// prefix convention is the contract.
const (
	kindValidation   = "validation"   // bad tool input — fix the arguments
	kindNotFound     = "not_found"    // resource or match absent
	kindUpstream     = "upstream"     // backend/network failure — retry may help
	kindPrecondition = "precondition" // server-side config missing (API key, browser, ...)
	kindCancelled    = "cancelled"    // call cancelled or timed out
)

// errTaxonomy is appended to every tool description so agents know how to
// read failures.
const errTaxonomy = " Errors carry a machine-readable prefix: [validation] fix the input; [not_found] no such resource/match; [upstream] backend or network failure, retrying may help; [precondition] server-side configuration missing; [cancelled] the call was cancelled or timed out."

// errf builds an error whose message starts with the "[kind] " prefix.
// Supports %w wrapping like fmt.Errorf.
func errf(kind, format string, args ...any) error {
	return fmt.Errorf("["+kind+"] "+format, args...)
}

// warnf formats a warning with the same "[kind] " prefix as errf, for a
// call that succeeded but whose output carries advice.
func warnf(kind, format string, args ...any) string {
	return "[" + kind + "] " + fmt.Sprintf(format, args...)
}

// upstreamErrf wraps a failure from a backend/network call under [upstream],
// or [cancelled] when the underlying cause is context cancellation or a
// deadline (client cancelled the tool call, or a server-side timeout fired),
// or [not_found] when the shared layer flagged the resource as permanently
// absent (docs.ErrNotFound, e.g. a Context7 404 for a bad library ID;
// code.ErrRepoNotFound, a repository the backend does not have) — the same
// rule the CLI applies in cmd's upstreamErr, so the surfaces cannot diverge.
func upstreamErrf(err error, format string, args ...any) error {
	kind := kindUpstream
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		kind = kindCancelled
	case errors.Is(err, docs.ErrNotFound) || errors.Is(err, code.ErrRepoNotFound):
		kind = kindNotFound
	}
	return errf(kind, format+": %w", append(args, err)...)
}

// backendErrf classifies a backend-constructor error (search/code/docs
// NewFromConfig): errors wrapping the package's unknown-backend sentinel are
// [validation]; anything else (missing API key/token, unimplemented backend)
// is [precondition].
func backendErrf(err, unknown error) error {
	if errors.Is(err, unknown) {
		return errf(kindValidation, "%w", err)
	}
	return errf(kindPrecondition, "%w", err)
}
