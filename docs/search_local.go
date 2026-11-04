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
// `docs index`, not by search), as -b local would answer; or the same
// question (its terms, sfAskedOverlap) was answered before from a
// library, the first site its search read (an answer from a fallback
// site is not remembered); or, under hybrid ranking only, a stored section is close to
// the question (its best window's cosine at least localFloor), from the
// library holding it. A library a search built holds only the pages
// earlier questions read, so naming it is not enough: its sections must
// answer, and keywords alone cannot say that they do.

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
	qv := s.queryVector(ctx, req.query)
	if o, ok := s.fromAsked(ctx, libs, req, qv, tr); ok {
		return o, true
	}
	lib, cos, ok := s.closest(ctx, cands, qv)
	tr.localCos = cos
	if tr.local == "" {
		tr.local = lib.Root
	}
	if !ok || cos < s.localFloor {
		return Outcome{}, false
	}
	o := s.local.answer(ctx, lib, req.query, req.opts)
	return o, o.Status == Answered || o.Status == Partial
}

// fromAsked answers from the library the same question was answered
// from before (askedBefore), when it was.
func (s *SearchFirst) fromAsked(ctx context.Context, libs []docsindex.Library, req sfRequest, qv []float32, tr *sfTrace) (Outcome, bool) {
	lib, sim, ok := askedBefore(libs, req.library, req.query, qv)
	if !ok {
		return Outcome{}, false
	}
	tr.askedCos, tr.local = sim, lib.Root
	if qv == nil && sim < sfAskedOverlap || qv != nil && sim < sfAskedFloor {
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

// sfAskedFloor is the cosine between two questions above which they are
// one question in other words (a repeat is 1): hybrid ranking only.
const sfAskedFloor = 0.92

// sfAskedOverlap is the share of their terms (normalized as the ranking
// normalizes them: no stopwords or question filler, inflections folded)
// two questions must have in common to be one: all of them. "how do I
// configure X" and "configuring X" are one; a follow-up that asks
// something more specific (one more term) is not, nor is a question
// about another part of the same site.
const sfAskedOverlap = 1.0

// queryVector embeds the question with the installed model; nil without
// one.
func (s *SearchFirst) queryVector(ctx context.Context, query string) []float32 {
	emb := s.model()
	if emb == nil {
		return nil
	}
	qv, err := emb.EmbedQuery(ctx, query)
	if err != nil {
		return nil
	}
	return qv
}

// model is the installed embedding model, nil without one.
func (s *SearchFirst) model() embed.Embedder {
	if s.local.model == nil {
		return nil
	}
	emb, err := s.local.model()
	if err != nil {
		return nil
	}
	return emb
}

// askedBefore is the library the question closest to this one was
// answered from, among those asked of the same library value (none for
// a bare question), and how close: with a query vector the two
// questions' cosine, else the overlap of their terms (termOverlap).
func askedBefore(libs []docsindex.Library, library, query string, qv []float32) (docsindex.Library, float64, bool) {
	var best docsindex.Library
	bestSim, found := 0.0, false
	terms := withoutFiller(queryTerms(query))
	for _, l := range libs {
		for _, a := range l.Asked {
			if a.Library != library {
				continue
			}
			var sim float64
			if qv != nil {
				v := embed.FromHalf(nil, a.Vector)
				if len(v) != len(qv) || len(v) == 0 {
					continue
				}
				sim = float64(embed.Dot(v, qv))
			} else {
				sim = termOverlap(terms, withoutFiller(queryTerms(a.Question)))
			}
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
// MaxAsked questions), with its vector under hybrid ranking.
func (s *SearchFirst) remember(lib docsindex.Library, library, query string, qv []float32) {
	if s.local.store == nil {
		return
	}
	stored, found, err := s.local.store.Library(lib.Key)
	if err != nil || !found {
		return
	}
	a := docsindex.Asked{Question: query, Library: library}
	if len(qv) > 0 && len(qv) == lib.Dims {
		a.Vector = embed.AppendHalf(nil, qv)
	}
	stored.Asked = append(stored.Asked, a)
	if n := len(stored.Asked); n > docsindex.MaxAsked {
		stored.Asked = stored.Asked[n-docsindex.MaxAsked:]
	}
	_ = s.local.store.PutLibrary(stored)
}

// closest is the library holding the stored section closest to the
// question (qv), and that section's cosine; ok is false without a
// usable model or vectors.
func (s *SearchFirst) closest(_ context.Context, libs []docsindex.Library, qv []float32) (docsindex.Library, float64, bool) {
	emb := s.model()
	if emb == nil || len(libs) == 0 || len(qv) == 0 {
		return docsindex.Library{}, 0, false
	}
	var best docsindex.Library
	bestCos, found := float32(0), false
	for _, l := range libs {
		if !l.Current() || l.Model != emb.ID() || l.Vectors == 0 {
			continue
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
	localCos, answerCos, askedCos                             float64
	hits, docsHits, siteHits, sites, planned, fetched, failed int
	stored                                                    sfStored
}

func (t *sfTrace) write(w io.Writer, req sfRequest, d time.Duration) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "trace: search-first path=%s local=%s local_cos=%.4f asked_cos=%.4f answer_cos=%.4f hits=%d docs_hits=%d site=%s site_hits=%d sites=%d planned=%d fetched=%d failed=%d new=%d changed=%d unchanged=%d embedded=%d embed_ms=%d ms=%d library=%q cands=%q\n",
		t.path, t.local, t.localCos, t.askedCos, t.answerCos, t.hits, t.docsHits, t.site, t.siteHits, t.sites, t.planned, t.fetched, t.failed,
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
