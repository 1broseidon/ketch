// Package failure is the one place a ketch error is classified (ADR-0002).
//
// Every failure the CLI or the MCP server reports is in exactly one Kind:
// validation (fix the input), not_found (no such resource or match),
// upstream (a backend or network failure; retrying may help), precondition
// (the operator must configure something) or cancelled (the caller stopped
// the call). The CLI maps a Kind to its exit code and the MCP server to its
// "[kind]" prefix; neither decides a Kind itself.
//
// A package marks its sentinel errors with their Kind where it defines them
// (New), so classification lives beside the condition, not at the surfaces.
// Classify walks an error chain and returns the Kind and a short Reason.
package failure

import (
	"context"
	"errors"

	bolterrors "go.etcd.io/bbolt/errors"
)

// Kind is the class of a failure. The zero value is not a valid Kind.
type Kind int

const (
	Validation   Kind = iota + 1 // bad input: fix the arguments
	NotFound                     // resource or match absent: not retryable
	Upstream                     // backend/network failure: retrying may help
	Precondition                 // configuration missing: the operator must act
	Cancelled                    // the caller cancelled the call
)

// String is the stable machine-readable name: the MCP error prefix.
func (k Kind) String() string {
	switch k {
	case Validation:
		return "validation"
	case NotFound:
		return "not_found"
	case Upstream:
		return "upstream"
	case Precondition:
		return "precondition"
	case Cancelled:
		return "cancelled"
	}
	return "unknown"
}

// ExitCode is the CLI's documented exit code for k (1 for an invalid Kind).
func (k Kind) ExitCode() int {
	switch k {
	case Validation:
		return 2
	case NotFound:
		return 3
	case Upstream:
		return 4
	case Precondition:
		return 5
	case Cancelled:
		return 6
	}
	return 1
}

// Kinds lists every valid Kind, for tests and documentation.
func Kinds() []Kind { return []Kind{Validation, NotFound, Upstream, Precondition, Cancelled} }

// Error is an error whose Kind is known where it is made. Reason is a short
// snake_case word that says which condition within the Kind (timeout,
// rate_limited, ambiguous, no_browser, ...); it is stable for callers.
type Error struct {
	Kind   Kind
	Reason string
	Err    error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// New makes a sentinel error of the given Kind. Compare against it with
// errors.Is exactly as with errors.New.
func New(kind Kind, reason, msg string) *Error {
	return &Error{Kind: kind, Reason: reason, Err: errors.New(msg)}
}

// Wrap attaches a Kind and Reason to err. A nil err stays nil.
func Wrap(kind Kind, reason string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: kind, Reason: reason, Err: err}
}

// Class is the result of Classify.
type Class struct {
	Kind   Kind
	Reason string
}

// Classify returns the Kind and Reason of err, in this order:
//
//  1. context.Canceled anywhere in the chain: Cancelled. The caller stopped
//     the call, whatever else went wrong on the way out.
//  2. The first *Error in the chain: its Kind and Reason.
//  3. context.DeadlineExceeded: Upstream, reason "timeout". A deadline is a
//     budget ketch or a client library set, never the caller cancelling, so
//     it reads "retry" on both surfaces.
//  4. bbolt's lock timeout: Upstream, reason "busy". Another process holds
//     the cache or tag file; trying again is the fix.
//  5. Otherwise fallback, with no reason. Callers pass Upstream for a failed
//     backend call and Precondition for a failed backend constructor.
//
// A nil err classifies as the zero Class.
func Classify(err error, fallback Kind) Class {
	if err == nil {
		return Class{}
	}
	if errors.Is(err, context.Canceled) {
		return Class{Kind: Cancelled, Reason: "cancelled"}
	}
	var fe *Error
	if errors.As(err, &fe) {
		return Class{Kind: fe.Kind, Reason: fe.Reason}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Class{Kind: Upstream, Reason: "timeout"}
	}
	if errors.Is(err, bolterrors.ErrTimeout) {
		return Class{Kind: Upstream, Reason: "busy"}
	}
	return Class{Kind: fallback}
}
