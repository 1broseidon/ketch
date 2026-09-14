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
	Source     string `json:"source"` // "context7" | "local"
}

// Searcher is the interface for docs backends.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}
