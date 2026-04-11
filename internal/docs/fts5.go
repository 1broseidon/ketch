package docs

import (
	"context"
	"fmt"
)

// FTS5Local is a planned local SQLite FTS5 docs backend. Not yet implemented.
type FTS5Local struct{}

// NewFTS5Local creates a new FTS5Local backend stub.
func NewFTS5Local() *FTS5Local { return &FTS5Local{} }

// Search returns an error because the local FTS5 backend is not yet implemented.
func (f *FTS5Local) Search(_ context.Context, _ string, _ int) ([]Result, error) {
	return nil, fmt.Errorf("local fts5 backend not yet implemented")
}
