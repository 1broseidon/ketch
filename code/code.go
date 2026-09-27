package code

import (
	"context"
	"errors"
)

// Result is a single code search result.
type Result struct {
	Repo     string `json:"repo"`
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	Snippet  string `json:"snippet"`
	Language string `json:"language,omitempty"`
	Stars    int    `json:"stars,omitempty"`
	URL      string `json:"url"`
	Source   string `json:"source"` // "sourcegraph" | "grepapp" | "github"
}

// Query describes a code search request. Required fields are Term and Limit;
// the rest are optional refinements. Backends that do not support a given
// option should ignore it or, where it would silently change semantics
// (e.g. Regexp), return ErrRegexpUnsupported. Repo is never ignored: it names
// exactly one repository, and a backend whose native filter is looser (a
// substring or an unanchored pattern) narrows its own results to it.
type Query struct {
	Term   string // the search string (literal, or a regex when Regexp is set)
	Lang   string // language filter ("" = any)
	Repo   string // one repository, owner/name ("" = any); see NormalizeRepo
	Limit  int    // max results
	Regexp bool   // interpret Term as a regular expression
}

// ErrRegexpUnsupported is returned by a backend that was asked for a regular
// expression search it cannot perform. The CLI maps this to a precondition
// error so the user can pick a different backend.
var ErrRegexpUnsupported = errors.New("regular expression search not supported by this backend")

// ErrInvalidRepo is returned for a Repo that is not an owner/name repository.
// Callers map it to a validation error.
var ErrInvalidRepo = errors.New("repository must be owner/name")

// ErrRepoNotFound is returned by a backend that reports it has no repository
// matching Query.Repo. Another backend may index it, so callers map it to a
// not-found error that names the alternatives.
var ErrRepoNotFound = errors.New("repository not found")

// Searcher is the interface for code search backends. Each backend owns its
// own query dialect — language and repository filtering and safety qualifiers
// (archived/fork exclusion) are applied internally so that callers pass plain
// user input.
type Searcher interface {
	Search(ctx context.Context, q Query) ([]Result, error)
}
