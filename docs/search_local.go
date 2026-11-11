package docs

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// When the local index answers a search-first question without
// searching: the request names a library indexed by `docs index`, or the
// question's search, read before within cache_ttl (fromRead), found a
// docs root an indexed library holds. A library an earlier ketch stored
// for a question is never read (Readable).

// fromIndex answers from the local index when the request names an
// indexed library.
func (s *SearchFirst) fromIndex(ctx context.Context, req sfRequest, tr *sfTrace) (Outcome, bool) {
	libs, err := s.local.libraries()
	if err != nil || len(libs) == 0 {
		return Outcome{}, false
	}
	var current []docsindex.Library
	for _, l := range libs {
		if l.Current() {
			current = append(current, l)
		}
	}
	found := s.named(current, req)
	if len(found) == 0 {
		return Outcome{}, false
	}
	lib, note := newestOf(found)
	tr.local = lib.Root
	opts := req.opts
	opts.scope = scopeOf(lib, req.library)
	return s.answerIndexed(ctx, lib, req.query, opts, note), true
}

// named is the indexed libraries a request names: its library, or the
// library its question names.
func (s *SearchFirst) named(libs []docsindex.Library, req sfRequest) []docsindex.Library {
	if req.library != "" {
		return MatchIndexed(libs, req.library)
	}
	found, _ := libraryInQuery(libs, req.query)
	return found
}

// near is the indexed libraries near the library value a request gives
// that names none (nearIndexed); none without a library value.
func (s *SearchFirst) near(req sfRequest) []NearLibrary {
	if req.library == "" {
		return nil
	}
	libs, err := s.local.libraries()
	if err != nil {
		return nil
	}
	var current []docsindex.Library
	for _, l := range libs {
		if l.Current() {
			current = append(current, l)
		}
	}
	return nearIndexed(current, req.library)
}

// sfTrace is one search-first call's path, written when KETCH_DOCS_TRACE
// is set: for measuring, never part of the answer.
type sfTrace struct {
	path, local, site, cands                                  string
	cached                                                    bool // the question's read came from the question cache
	hits, docsHits, siteHits, sites, planned, fetched, failed int
	domains                                                   int // the sites (domains) read; sites counts docs trees
}

func (t *sfTrace) write(w io.Writer, req sfRequest, d time.Duration) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "trace: search-first path=%s local=%s cached=%t hits=%d docs_hits=%d site=%s site_hits=%d sites=%d planned=%d fetched=%d failed=%d ms=%d library=%q cands=%q\n",
		t.path, t.local, t.cached, t.hits, t.docsHits, t.site, t.siteHits, t.sites, t.planned, t.fetched, t.failed,
		d.Milliseconds(), req.library, t.cands)
}

// siteList names the leading sites with their weight, for the trace.
func siteList(sites []sfSite) string {
	var parts []string
	for _, s := range sites[:min(len(sites), 6)] {
		mark := ""
		if s.named {
			mark = "*"
		}
		parts = append(parts, fmt.Sprintf("%s%s:%d:%.3f", mark, s.root, len(s.hits), s.score))
	}
	return strings.Join(parts, " ")
}
