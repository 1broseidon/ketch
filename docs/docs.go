package docs

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound reports a docs resource that is permanently absent upstream —
// typically a Context7 library ID that resolves to a 404. It is detected in
// this package (never by string matching at the surfaces) so both the CLI and
// the MCP server classify it as not-found (exit 3 / [not_found]) rather than
// a retryable upstream failure.
var ErrNotFound = errors.New("not found")

// ErrLibraryRequired reports a docs request the backend cannot answer
// without knowing which library's docs to search — for the web backend, a
// docs URL passed as the library. Both surfaces classify it as a validation
// error (CLI exit 2 / MCP [validation]): the caller fixes it by adding one.
var ErrLibraryRequired = errors.New("library required")

// ErrAmbiguousLibrary reports a library name that more than one verified
// project answers to, where exactly one is needed (a search, or docs for a
// named library). It comes as an *AmbiguousLibraryError, which also wraps
// ErrLibraryRequired: the caller fixes it by passing one of the candidates
// as the library, so both surfaces classify it as a validation error (CLI
// exit 2 / MCP [validation]) without a rule of their own.
var ErrAmbiguousLibrary = errors.New("ambiguous library")

// AmbiguousLibraryError is a library name that more than one project
// answers to. It carries the candidates so a caller can list or pick one,
// and its message names each candidate's ID, title and package specs.
type AmbiguousLibraryError struct {
	Library string         // the name as given
	Matches []LibraryMatch // the candidates, best first
}

func (e *AmbiguousLibraryError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q: %d libraries match; pass one with --library (CLI) or library (MCP): ", ErrAmbiguousLibrary, e.Library, len(e.Matches))
	for i, m := range e.Matches {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(m.ID)
		var about []string
		if m.Title != "" {
			about = append(about, m.Title)
		}
		if len(m.Packages) > 0 {
			about = append(about, strings.Join(m.Packages, ", "))
		}
		if len(about) > 0 {
			b.WriteString(" (" + strings.Join(about, "; ") + ")")
		}
	}
	return b.String()
}

// Unwrap makes the error match both ErrAmbiguousLibrary and
// ErrLibraryRequired under errors.Is.
func (e *AmbiguousLibraryError) Unwrap() []error {
	return []error{ErrAmbiguousLibrary, ErrLibraryRequired}
}

// Truncate returns at most limit results when limit is positive. A zero or
// negative limit leaves results unbounded; callers use that for the library
// path, where the token budget is the only bound unless a limit is explicit.
func Truncate(results []Result, limit int) []Result {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

// Result is a single docs search result.
type Result struct {
	Library    string `json:"library"`
	Version    string `json:"version,omitempty"`
	Title      string `json:"title"`
	Breadcrumb string `json:"breadcrumb,omitempty"`
	Snippet    string `json:"snippet"`
	URL        string `json:"url"`
	Source     string `json:"source"` // "context7" | "web"
}

// Searcher is the interface for docs backends.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}
