package docs

import (
	"context"
	"fmt"
)

func offline(ctx context.Context) bool {
	opts, _ := localOptionsFrom(ctx)
	return opts.Offline
}

func offlineMiss(name string) error {
	return fmt.Errorf("search: %q has no matching local index; offline makes no network requests; index its docs with ketch docs index <docs-url> first: %w", name, ErrNotIndexed)
}
