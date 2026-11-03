package mcp

import (
	"fmt"

	"github.com/1broseidon/ketch/internal/failure"
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
//
// The names come from failure.Kind, which the CLI's exit codes use too.
// Upstream and cancelled are never chosen by hand: only failure.Classify
// decides them.
var (
	kindValidation   = failure.Validation.String()   // bad tool input — fix the arguments
	kindNotFound     = failure.NotFound.String()     // resource or match absent
	kindPrecondition = failure.Precondition.String() // server-side config missing (API key, browser, ...)
)

// errTaxonomy is appended to every tool description so agents know how to
// read failures.
const errTaxonomy = " Errors carry a machine-readable prefix: [validation] fix the input; [not_found] no such resource/match; [upstream] backend or network failure or a timeout, retrying may help; [precondition] server-side configuration missing; [cancelled] the call was cancelled."

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

// upstreamErrf classifies a failure from a backend/network call with
// failure.Classify, the rule the CLI's upstreamErr applies too: the
// sentinels carry their own kind, cancellation is [cancelled], a deadline is
// an [upstream] timeout, and anything else is [upstream].
func upstreamErrf(err error, format string, args ...any) error {
	return classifiedf(err, failure.Upstream, format+": %w", append(args, err)...)
}

// backendErrf classifies a backend-constructor error (search/code/docs
// NewFromConfig): an unknown backend is [validation] (its sentinel says so);
// anything else (missing API key/token, unimplemented backend) is
// [precondition].
func backendErrf(err error) error {
	return classifiedf(err, failure.Precondition, "%w", err)
}

// classifiedf formats an error prefixed with err's failure.Kind, or
// fallback's when err carries none.
func classifiedf(err error, fallback failure.Kind, format string, args ...any) error {
	return errf(failure.Classify(err, fallback).Kind.String(), format, args...)
}
