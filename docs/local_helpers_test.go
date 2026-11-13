package docs

import (
	"context"
	"fmt"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/search"
)

// Test entry points over the local index's live internals. The search
// backend reaches the same ranking through fromIndex; these keep the
// index's ranking, naming and version tests independent of search's site
// picking.

// SearchSelect answers a bare query from the indexed library it names:
// the first of the query's readings (splitLibraryQuery) that names an
// indexed library, unless a firmer reading before it names a library the
// index does not hold, which the live backends should answer.
func (l *Local) SearchSelect(ctx context.Context, query string, limit int) Outcome {
	libs, err := l.libraries()
	if err != nil {
		return Answer(SearchBackend, nil, err)
	}
	found, ok := libraryInQuery(libs, query)
	if !ok {
		return Answer(SearchBackend, nil, fmt.Errorf("local: the query names no indexed library: %w", ErrNotIndexed))
	}
	lib, note := newestOf(found)
	opts, _ := localOptionsFrom(ctx)
	if opts.Limit <= 0 {
		opts.Limit = limit
	}
	o := l.answer(ctx, lib, query, opts)
	o.Results = Truncate(o.Results, limit)
	return withVersionNote(o, note)
}

// GetDocsSelect answers for a library given as its docs root URL (or a
// page under it), its spec or a name it was indexed by.
func (l *Local) GetDocsSelect(ctx context.Context, libraryID, query string, tokens int) Outcome {
	libs, err := l.libraries()
	if err != nil {
		return Answer(SearchBackend, nil, err)
	}
	lib, note, err := pickVersion(libs, libraryID)
	if err != nil {
		return Answer(SearchBackend, nil, err)
	}
	opts, ok := localOptionsFrom(ctx)
	if !ok {
		opts.Tokens = tokens // a direct caller's budget is its choice
	}
	return withVersionNote(l.answer(ctx, lib, query, opts), note)
}

// pickIndexed is the indexed library a library value names (the newest
// version of several), or a not_indexed refusal.
func pickIndexed(libs []docsindex.Library, value string) (docsindex.Library, error) {
	lib, _, err := pickVersion(libs, value)
	return lib, err
}

// fuseHits merges search result lists into one ranking of pages, each
// weighed by its rank in every list it is in, best first.
func fuseHits(lists [][]search.Result) []sfHit {
	return fuseWeighted(lists, nil)
}

// resolve is resolveLogged with a fresh log: every verified match for
// spec, best first, at most limit when limit is positive.
func (w *Web) resolve(ctx context.Context, spec libSpec, limit int) ([]webMatch, error) {
	return w.resolveLogged(ctx, spec, limit, &resolveLog{})
}
