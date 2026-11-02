package docs

import (
	"context"
	"errors"
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
