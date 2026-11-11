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
// searching: the request names a library indexed in full (by
// `docs index`, not by search), with whole sections; or the same
// question (its terms, sfAskedOverlap) was answered before from a
// library, the first site its search read (an answer from a fallback
// site is not remembered). A library a search built holds only the pages
// earlier questions read, so naming it is not enough: its sections must
// answer, and keywords alone cannot say that they do, so a new question
// about it is searched again.

// fromIndex answers from the local index when it covers the request.
func (s *SearchFirst) fromIndex(ctx context.Context, req sfRequest, tr *sfTrace) (Outcome, bool) {
	libs, err := s.local.libraries()
	if err != nil || len(libs) == 0 {
		return Outcome{}, false
	}
	var full []docsindex.Library
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
	return s.fromAsked(ctx, libs, req, tr)
}

// fromAsked answers from the library the same question was answered
// from before (askedBefore), when it was.
func (s *SearchFirst) fromAsked(ctx context.Context, libs []docsindex.Library, req sfRequest, tr *sfTrace) (Outcome, bool) {
	lib, sim, ok := askedBefore(libs, req.library, req.query)
	if !ok {
		return Outcome{}, false
	}
	tr.asked, tr.local = sim, lib.Root
	if sim < sfAskedOverlap {
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

// sfAskedOverlap is the share of their terms (normalized as the ranking
// normalizes them: no stopwords or question filler, inflections folded)
// two questions must have in common to be one: all of them. "how do I
// configure X" and "configuring X" are one; a follow-up that asks
// something more specific (one more term) is not, nor is a question
// about another part of the same site.
const sfAskedOverlap = 1.0

// askedBefore is the library the question closest to this one was
// answered from, among those asked of the same library value (none for
// a bare question), and how close: the overlap of their terms
// (termOverlap).
func askedBefore(libs []docsindex.Library, library, query string) (docsindex.Library, float64, bool) {
	var best docsindex.Library
	bestSim, found := 0.0, false
	terms := withoutFiller(queryTerms(query))
	for _, l := range libs {
		for _, a := range l.Asked {
			if a.Library != library {
				continue
			}
			sim := termOverlap(terms, withoutFiller(queryTerms(a.Question)))
			if !found || sim > bestSim {
				best, bestSim, found = l, sim, true
			}
		}
	}
	return best, bestSim, found
}

// termOverlap is the Jaccard overlap of two term lists.
func termOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, t := range a {
		set[t] = true
	}
	both := 0
	union := len(set)
	seen := map[string]bool{}
	for _, t := range b {
		if seen[t] {
			continue
		}
		seen[t] = true
		if set[t] {
			both++
		} else {
			union++
		}
	}
	return float64(both) / float64(union)
}

// remember records the question as answered from the library (its last
// MaxAsked questions).
func (s *SearchFirst) remember(lib docsindex.Library, library, query string) {
	if s.local.store == nil {
		return
	}
	stored, found, err := s.local.store.Library(lib.Key)
	if err != nil || !found {
		return
	}
	stored.Asked = append(stored.Asked, docsindex.Asked{Question: query, Library: library})
	if n := len(stored.Asked); n > docsindex.MaxAsked {
		stored.Asked = stored.Asked[n-docsindex.MaxAsked:]
	}
	_ = s.local.store.PutLibrary(stored)
}

// partialLibrary marks the local backend's answer from a library a search
// built: it holds only the pages earlier questions read.
func partialLibrary(o Outcome, lib docsindex.Library) Outcome {
	if lib.Resolved != resolvedBySearch || o.Status != Answered && o.Status != Partial {
		return o
	}
	return asPartial(o, o.Backend, partialNote(lib))
}

// partialNote is the note an answer from a library a search built
// carries.
func partialNote(lib docsindex.Library) string {
	return fmt.Sprintf("answered from %d pages of %s read for earlier questions; the whole site: ketch docs index %s", lib.Pages, lib.Root, lib.Root)
}

// sfTrace is one search-first call's path, written when KETCH_DOCS_TRACE
// is set: for measuring, never part of the answer.
type sfTrace struct {
	path, local, site, cands                                  string
	asked                                                     float64 // term overlap with the closest remembered question
	hits, docsHits, siteHits, sites, planned, fetched, failed int
	domains                                                   int // the sites (domains) read; sites counts docs trees
	stored                                                    sfStored
}

func (t *sfTrace) write(w io.Writer, req sfRequest, d time.Duration) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "trace: search-first path=%s local=%s asked=%.4f hits=%d docs_hits=%d site=%s site_hits=%d sites=%d planned=%d fetched=%d failed=%d new=%d changed=%d unchanged=%d ms=%d library=%q cands=%q\n",
		t.path, t.local, t.asked, t.hits, t.docsHits, t.site, t.siteHits, t.sites, t.planned, t.fetched, t.failed,
		t.stored.newPages, t.stored.changed, t.stored.unchanged, d.Milliseconds(), req.library, t.cands)
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
