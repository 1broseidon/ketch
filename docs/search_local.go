package docs

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
)

// When the local index answers a search-first question without
// searching: the request names a library indexed in full (by
// `docs index`, not by search), as -b local would answer; or a stored
// section is close to the question (its best window's cosine at least
// localFloor), from the library holding it. A library a search built
// holds only the pages earlier questions read, so naming it is not
// enough: its sections must answer.

// fromIndex answers from the local index when it covers the request.
func (s *SearchFirst) fromIndex(ctx context.Context, req sfRequest, tr *sfTrace) (Outcome, bool) {
	libs, err := s.local.libraries()
	if err != nil || len(libs) == 0 {
		return Outcome{}, false
	}
	var full, cands []docsindex.Library
	for _, l := range libs {
		if l.Current() && l.Resolved != resolvedBySearch {
			full = append(full, l)
		}
	}
	if found := s.named(full, req); len(found) > 0 {
		lib, note := newestOf(found)
		tr.local = lib.Root
		return withVersionNote(s.local.answer(ctx, lib, req.query, req.opts), note), true
	}
	cands = libs
	if req.library != "" {
		cands = MatchIndexed(libs, req.library)
	}
	lib, cos, ok := s.closest(ctx, cands, req.query)
	tr.localCos, tr.local = cos, lib.Root
	if !ok || cos < s.localFloor {
		return Outcome{}, false
	}
	o := s.local.answer(ctx, lib, req.query, req.opts)
	return o, o.Status == Answered || o.Status == Partial
}

// named is the fully indexed libraries a request names: its library, or
// the library its question names.
func (s *SearchFirst) named(full []docsindex.Library, req sfRequest) []docsindex.Library {
	if req.library != "" {
		return MatchIndexed(full, req.library)
	}
	found, _ := libraryInQuery(full, req.query)
	return found
}

// closest is the library holding the stored section closest to the
// question, and that section's cosine; ok is false without a usable
// model or vectors.
func (s *SearchFirst) closest(ctx context.Context, libs []docsindex.Library, query string) (docsindex.Library, float64, bool) {
	if s.local.model == nil || len(libs) == 0 {
		return docsindex.Library{}, 0, false
	}
	emb, err := s.local.model()
	if err != nil || emb == nil {
		return docsindex.Library{}, 0, false
	}
	var qv []float32
	var best docsindex.Library
	bestCos, found := float32(0), false
	for _, l := range libs {
		if !l.Current() || l.Model != emb.ID() || l.Vectors == 0 {
			continue
		}
		if qv == nil {
			if qv, err = emb.EmbedQuery(ctx, query); err != nil {
				return docsindex.Library{}, 0, false
			}
		}
		secs, err := s.local.store.Sections(l.Key, l.Dims, true)
		if err != nil {
			continue
		}
		if c, ok := bestWindow(secs, qv); ok && (!found || c > bestCos) {
			best, bestCos, found = l, c, true
		}
	}
	return best, float64(bestCos), found
}

// bestWindow is the highest cosine of any section window with qv.
func bestWindow(secs []docsindex.Section, qv []float32) (float32, bool) {
	best, ok := float32(0), false
	for _, s := range secs {
		for _, v := range s.Vectors {
			if len(v) == len(qv) {
				if c := embed.Dot(v, qv); !ok || c > best {
					best, ok = c, true
				}
			}
		}
	}
	return best, ok
}

// partialLibrary marks the local backend's answer from a library a search
// built: it holds only the pages earlier questions read.
func partialLibrary(o Outcome, lib docsindex.Library) Outcome {
	if lib.Resolved != resolvedBySearch || o.Status != Answered {
		return o
	}
	note := fmt.Sprintf("answered from %d pages of %s read for earlier questions; the whole site: ketch docs index %s", lib.Pages, lib.Root, lib.Root)
	return asPartial(o, o.Backend, note)
}

// sfTrace is one search-first call's path, written when KETCH_DOCS_TRACE
// is set: for measuring, never part of the answer.
type sfTrace struct {
	path, local, site, cands                                  string
	localCos, answerCos                                       float64
	hits, docsHits, siteHits, sites, planned, fetched, failed int
	stored                                                    sfStored
}

func (t *sfTrace) write(w io.Writer, req sfRequest, d time.Duration) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "trace: search-first path=%s local=%s local_cos=%.4f answer_cos=%.4f hits=%d docs_hits=%d site=%s site_hits=%d sites=%d planned=%d fetched=%d failed=%d new=%d changed=%d unchanged=%d embedded=%d embed_ms=%d ms=%d library=%q cands=%q\n",
		t.path, t.local, t.localCos, t.answerCos, t.hits, t.docsHits, t.site, t.siteHits, t.sites, t.planned, t.fetched, t.failed,
		t.stored.newPages, t.stored.changed, t.stored.unchanged, t.stored.embedded, t.stored.embedTime.Milliseconds(), d.Milliseconds(), req.library, t.cands)
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
