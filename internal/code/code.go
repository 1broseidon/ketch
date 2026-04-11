package code

// Result is a single code search result.
type Result struct {
	Repo     string `json:"repo"`
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	Snippet  string `json:"snippet"`
	Language string `json:"language,omitempty"`
	URL      string `json:"url"`
	Source   string `json:"source"` // "sourcegraph"
}

// Searcher is the interface for code search backends.
type Searcher interface {
	Search(query string, limit int) ([]Result, error)
}
