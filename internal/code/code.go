package code

import "context"

// Result is a single code search result.
type Result struct {
	Repo     string `json:"repo"`
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	Snippet  string `json:"snippet"`
	Language string `json:"language,omitempty"`
	Stars    int    `json:"stars,omitempty"`
	URL      string `json:"url"`
	Source   string `json:"source"` // "sourcegraph" | "github"
}

// Searcher is the interface for code search backends. Each backend owns its
// own query dialect — language filtering and safety qualifiers (archived/fork
// exclusion) are applied internally so that callers pass plain user input.
type Searcher interface {
	Search(ctx context.Context, query, lang string, limit int) ([]Result, error)
}
