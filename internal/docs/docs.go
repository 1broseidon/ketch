package docs

import "context"

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
