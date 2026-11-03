package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
	"github.com/1broseidon/ketch/internal/failure"
)

// The local docs backend answers from the local index (ADR-0007), with no
// network: it embeds the question, ranks the library's sections by cosine
// (brute force; a library is thousands of sections), groups the nearest
// by page and returns about ten page results, each its best section with
// the page's other near sections as parts. The agent calling ketch picks
// among them. Without a usable model it ranks with the web backend's BM25
// and says so in the outcome.

// ErrNotIndexed is a library the local docs index does not hold: a
// refusal, so the auto chain moves on to the live backends, and the caller
// can index it.
var ErrNotIndexed = failure.New(failure.NotFound, "not_indexed", "library not indexed (ketch docs index <library|docs-url>)")

// ReasonKeywordRanking is an answered outcome's reason when the local
// backend ranked by keywords: no embedding model installed, or the index
// was built with another one or with none. Outcome.Note says which.
const ReasonKeywordRanking = "keyword_ranking"

const (
	// localTopSections is how many of the nearest sections are grouped
	// into page results as their parts; a page's best section may rank
	// lower, down to localPageSections, so that a library whose nearest
	// sections crowd into a few pages (a reference page, release notes)
	// still offers about localMaxPages pages.
	localTopSections  = 30
	localPageSections = 2 * localTopSections
	// localMaxPages is how many page results are returned at most: enough
	// that the answer is among them (89% at ten on full sites with
	// bge-base), few enough for an agent to read.
	localMaxPages = 10
)

// Local answers from the local docs index.
type Local struct {
	store *docsindex.Store // nil when there is no cache directory
	// model is the embedding model, located and opened (lazily) once.
	model func() (embed.Embedder, error)
}

// NewLocal returns the backend over store, embedding questions with the
// model model returns (an error: rank by keywords).
func NewLocal(store *docsindex.Store, model func() (embed.Embedder, error)) *Local {
	return &Local{store: store, model: model}
}

// newLocalFromConfig is the backend over the default index file and the
// model model_path names, or the installed one. It does no I/O.
func newLocalFromConfig(c *config.Config) *Local {
	modelPath := ""
	if c != nil {
		modelPath = c.String("model_path")
	}
	var store *docsindex.Store
	if path, err := docsindex.DefaultPath(); err == nil {
		store = docsindex.Open(path)
	}
	return NewLocal(store, sync.OnceValues(func() (embed.Embedder, error) {
		m, err := embed.Locate(modelPath)
		if err != nil {
			return nil, err
		}
		return embed.Open(m), nil
	}))
}

// Search answers a bare query; see SearchSelect.
func (l *Local) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	o := l.SearchSelect(ctx, query, limit)
	return o.Results, o.Err()
}

// GetDocs answers for a named library; see GetDocsSelect.
func (l *Local) GetDocs(ctx context.Context, libraryID, query string, tokens int) ([]Result, error) {
	o := l.GetDocsSelect(ctx, libraryID, query, tokens)
	return o.Results, o.Err()
}

// ResolveLibrary lists the indexed libraries a name matches; see
// ResolveSelect.
func (l *Local) ResolveLibrary(ctx context.Context, name string, limit int) ([]LibraryMatch, error) {
	o := l.ResolveSelect(ctx, name, limit)
	return o.Matches, o.Err()
}

// SearchSelect answers a bare query from the indexed library it names:
// the first of the query's readings (splitLibraryQuery) that names an
// indexed library, unless a firmer reading before it names a library the
// index does not hold, which the live backends should answer.
func (l *Local) SearchSelect(ctx context.Context, query string, limit int) Outcome {
	libs, err := l.libraries()
	if err != nil {
		return Answer("local", nil, err)
	}
	lib, ok := libraryInQuery(libs, query)
	if !ok {
		return Answer("local", nil, fmt.Errorf("local: the query names no indexed library: %w", ErrNotIndexed))
	}
	o := l.answer(ctx, lib, query, webDefaultTokens)
	o.Results = Truncate(o.Results, limit)
	return o
}

// GetDocsSelect answers for a library given as its docs root URL (or a
// page under it), its spec or a name it was indexed by.
func (l *Local) GetDocsSelect(ctx context.Context, libraryID, query string, tokens int) Outcome {
	libs, err := l.libraries()
	if err != nil {
		return Answer("local", nil, err)
	}
	lib, err := pickIndexed(libs, libraryID)
	if err != nil {
		return Answer("local", nil, err)
	}
	return l.answer(ctx, lib, query, tokens)
}

// ResolveSelect lists the indexed libraries name matches, each with its
// root as the ID.
func (l *Local) ResolveSelect(_ context.Context, name string, limit int) Outcome {
	libs, err := l.libraries()
	if err != nil {
		return Resolved("local", nil, err)
	}
	var matches []LibraryMatch
	for _, lib := range MatchIndexed(libs, name) {
		matches = append(matches, indexedMatch(lib))
	}
	if len(matches) == 0 {
		err = fmt.Errorf("local: %q %w", name, ErrNotIndexed)
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return Resolved("local", matches, err)
}

// indexedMatch is an indexed library as a resolve match.
func indexedMatch(lib docsindex.Library) LibraryMatch {
	about := fmt.Sprintf("indexed %s: %d pages, %d sections", lib.Built.Format("2006-01-02"), lib.Pages, lib.Sections)
	if lib.Model != "" {
		about += ", embedded with " + lib.Model
	}
	return LibraryMatch{ID: lib.Root, Title: lib.Title, Version: lib.Version, Packages: lib.Packages, TrustScore: lib.Trust, Evidence: append([]string{about}, lib.Evidence...)}
}

// libraries lists the indexed libraries; none when the index file does not
// exist (a stat, no open).
func (l *Local) libraries() ([]docsindex.Library, error) {
	if l.store == nil || !l.store.Exists() {
		return nil, nil
	}
	libs, err := l.store.Libraries()
	if err != nil {
		return nil, fmt.Errorf("local: reading the docs index: %w", err)
	}
	return libs, nil
}

// answer ranks lib's sections for query and shapes the page results.
func (l *Local) answer(ctx context.Context, lib docsindex.Library, query string, tokens int) Outcome {
	emb, note := l.usableModel(lib)
	secs, err := l.store.Sections(lib.Key, lib.Dims, emb != nil)
	if err != nil {
		return Answer("local", nil, fmt.Errorf("local: reading the docs index: %w", err))
	}
	var ranked []scoredSection
	if emb != nil {
		qv, err := emb.EmbedQuery(ctx, query)
		if err != nil {
			return Answer("local", nil, fmt.Errorf("local: embedding the query: %w", err))
		}
		ranked = denseRank(secs, qv)
	} else {
		ranked = relevant(rankSections(webSections(secs), query))
	}
	ranked = dedupe(ranked)
	if len(ranked) > localPageSections {
		ranked = ranked[:localPageSections]
	}
	results := pageResults(lib, ranked, query, tokens)
	if len(results) == 0 {
		return Answer("local", nil, fmt.Errorf("local: nothing in the index of %s matched: %w", lib.Root, ErrNoResults))
	}
	o := Answer("local", results, nil)
	if note != "" {
		o.Reason, o.Note = ReasonKeywordRanking, note
		o.Attempts[0].Reason = ReasonKeywordRanking
	}
	return o
}

// usableModel is the embedder that can rank lib's vectors, or nil and why
// not: no model installed, an index built with another model, or one
// built with none.
func (l *Local) usableModel(lib docsindex.Library) (embed.Embedder, string) {
	if lib.Model == "" {
		return nil, "ranked by keywords: this library was indexed without an embedding model (ketch model install, then ketch docs index --reembed " + lib.Root + ")"
	}
	var emb embed.Embedder
	err := embed.ErrNotInstalled
	if l.model != nil {
		emb, err = l.model()
	}
	switch {
	case errors.Is(err, embed.ErrNotInstalled):
		return nil, "ranked by keywords: no embedding model installed (ketch model install)"
	case err != nil:
		return nil, "ranked by keywords: " + err.Error()
	case emb.ID() != lib.Model:
		return nil, fmt.Sprintf("ranked by keywords: this library was indexed with %s, not the installed %s (ketch docs index --reembed %s)", lib.Model, emb.ID(), lib.Root)
	}
	return emb, ""
}

// denseRank scores each section by its best window's cosine with the
// question, best first; sections without vectors are left out.
func denseRank(secs []docsindex.Section, qv []float32) []scoredSection {
	out := make([]scoredSection, 0, len(secs))
	for i, s := range secs {
		best, ok := float32(0), false
		for _, v := range s.Vectors {
			if len(v) == len(qv) {
				if c := embed.Dot(v, qv); !ok || c > best {
					best, ok = c, true
				}
			}
		}
		if ok {
			out = append(out, scoredSection{webSection: webSectionOf(s, i), score: float64(best)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

// webSections are stored sections in the shape the web backend ranks.
func webSections(secs []docsindex.Section) []webSection {
	out := make([]webSection, len(secs))
	for i, s := range secs {
		out[i] = webSectionOf(s, i)
	}
	return out
}

// webSectionOf is a stored section as the web backend's section. Its
// anchor was settled when it was indexed, so ranking never re-slugs it.
func webSectionOf(s docsindex.Section, i int) webSection {
	return webSection{page: i, order: s.Order, pageURL: s.Page, heading: s.Heading, crumbs: s.Crumbs, anchor: s.Anchor, source: anchorFixed, text: s.Text, links: s.Text}
}

// pageResults groups ranked sections by page, pages in the order of their
// best section, at most localMaxPages (their best sections among the
// nearest localPageSections, their other parts among the nearest
// localTopSections): each page is one result, its best
// section first under that section's title and address, and its other
// ranked sections after it as "### heading" parts, best first (as round
// 3's byPage grouped the web backend's sections). The token budget (about
// webCharsPerToken bytes a token) is shared fairly among the pages' best
// sections first, so every page gets room to say what it is, and what is
// left goes to the other parts in rank order. A section cut to fit keeps
// its blocks that name the query (trimForQuery).
func pageResults(lib docsindex.Library, ranked []scoredSection, query string, tokens int) []Result {
	if tokens <= 0 {
		tokens = webDefaultTokens
	}
	budget := tokens * webCharsPerToken
	maxPages := min(localMaxPages, max(1, budget/(2*webMinTrimBytes)))
	var order []string
	lead := map[string]int{} // page -> index in ranked of its best section
	for i, s := range ranked {
		if _, ok := lead[s.pageURL]; !ok && len(order) < maxPages {
			lead[s.pageURL] = i
			order = append(order, s.pageURL)
		}
	}
	terms := queryTerms(query)
	sizes := make([]int, len(order))
	for i, p := range order {
		sizes[i] = len(ranked[lead[p]].text)
	}
	shares := fairShares(sizes, budget)
	results := make([]Result, len(order))
	at := map[string]int{}
	remaining := budget
	for i, p := range order {
		s := ranked[lead[p]]
		snippet := s.text
		if len(snippet) > shares[i] {
			snippet = trimForQuery(snippet, shares[i], terms)
		}
		remaining -= len(snippet)
		at[p] = i
		results[i] = Result{Library: lib.Root, Version: lib.Version, Title: s.heading, Breadcrumb: strings.Join(s.crumbs, " > "), Snippet: snippet, URL: s.URL(), Source: "local"}
	}
	for i, s := range ranked[:min(len(ranked), localTopSections)] {
		j, kept := at[s.pageURL]
		if !kept || lead[s.pageURL] == i {
			continue
		}
		head := partHeading(s.heading)
		room := min(remaining-len(head), budget/4)
		text := s.text
		if len(text) > room {
			if room < webMinTrimBytes {
				continue
			}
			if text = trimForQuery(text, room, terms); text == "" {
				continue
			}
		}
		results[j].Snippet += head + text
		remaining -= len(head) + len(text)
	}
	return results
}

// fairShares splits budget among items of the given sizes max-min fairly:
// an item smaller than an equal share gets its size, and the rest is split
// equally among the larger ones, never below webMinTrimBytes.
func fairShares(sizes []int, budget int) []int {
	idx := make([]int, len(sizes))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return sizes[idx[a]] < sizes[idx[b]] })
	shares := make([]int, len(sizes))
	left := budget
	for k, i := range idx {
		equal := left / (len(idx) - k)
		shares[i] = max(min(sizes[i], equal), webMinTrimBytes)
		left -= min(shares[i], left)
	}
	return shares
}

// libraryInQuery finds the indexed library a bare query names. Readings
// are tried in the order splitLibraryQuery gives them; a reading that
// names no indexed library ends the search when it is a firm one (not a
// guess from joined words, not a common word), since that library is
// better answered live than another one answered locally.
func libraryInQuery(libs []docsindex.Library, query string) (docsindex.Library, bool) {
	if len(libs) == 0 {
		return docsindex.Library{}, false
	}
	for _, s := range splitLibraryQuery(query) {
		if found := matchSpec(libs, s.spec); len(found) > 0 {
			return preferred(found), true
		}
		if !s.strict && !s.common {
			return docsindex.Library{}, false
		}
	}
	return docsindex.Library{}, false
}

// pickIndexed is the indexed library a library value names, or a
// not_indexed refusal.
func pickIndexed(libs []docsindex.Library, value string) (docsindex.Library, error) {
	found := MatchIndexed(libs, value)
	if len(found) == 0 {
		return docsindex.Library{}, fmt.Errorf("local: %q %w", value, ErrNotIndexed)
	}
	return preferred(found), nil
}

// MatchIndexed returns the indexed libraries a library value names: a
// docs URL at or under an indexed root (or an indexed root under it), a
// root key, or a name or spec one was indexed by ([eco:]name[@version];
// with a version, only libraries indexed at a version it names).
func MatchIndexed(libs []docsindex.Library, value string) []docsindex.Library {
	value = strings.TrimSpace(value)
	for _, l := range libs {
		if l.Key == value || l.Root == value {
			return []docsindex.Library{l}
		}
	}
	if isHTTPURL(value) {
		return matchSpec(libs, libSpec{raw: value, url: value})
	}
	spec, err := parseLibSpec(value)
	if err != nil {
		// Not a spec ("tanstack query"): a name it was indexed by, read
		// as a comparison key.
		spec = libSpec{raw: value, name: value}
	}
	return matchSpec(libs, spec)
}

// matchSpec returns the libraries spec names.
func matchSpec(libs []docsindex.Library, spec libSpec) []docsindex.Library {
	var out []docsindex.Library
	if spec.url != "" {
		u, err := parseWebRoot(spec.url)
		if err != nil {
			return nil
		}
		for _, l := range libs {
			r, err := parseWebRoot(l.Root)
			if err == nil && (r.contains(u.u) || u.contains(r.u)) {
				out = append(out, l)
			}
		}
		return out
	}
	keys := map[string]bool{libraryKey(spec.name): true}
	if i := strings.LastIndex(spec.name, "/"); i >= 0 {
		keys[libraryKey(spec.name[i+1:])] = true
	}
	delete(keys, "")
	for _, l := range libs {
		if spec.version != "" && !versionNames(spec.version, l.Version) {
			continue
		}
		if anyNamed(l.Names, keys) || hasPackage(l.Packages, spec) {
			out = append(out, l)
		}
	}
	return out
}

// anyNamed reports whether any of names is a key in keys.
func anyNamed(names []string, keys map[string]bool) bool {
	for _, n := range names {
		if keys[n] {
			return true
		}
	}
	return false
}

// hasPackage reports a package spec of the library's (pypi:click) that
// spec names exactly, versions aside.
func hasPackage(pkgs []string, spec libSpec) bool {
	want, _, _ := strings.Cut(spec.raw, "@")
	if spec.eco == ecoAny || want == "" {
		return false
	}
	for _, p := range pkgs {
		if have, _, _ := strings.Cut(p, "@"); strings.EqualFold(have, want) {
			return true
		}
	}
	return false
}

// versionNames reports whether a requested version names an indexed one:
// the same label, or a prefix of its release (5 names 5.2).
func versionNames(want, have string) bool {
	if have == "" {
		return false
	}
	want, have = strings.TrimPrefix(strings.ToLower(want), "v"), strings.TrimPrefix(strings.ToLower(have), "v")
	return want == have || strings.HasPrefix(have, want+".")
}

// preferred picks one of several indexed libraries a value names: the
// newest release, then the most recently built.
func preferred(libs []docsindex.Library) docsindex.Library {
	best := libs[0]
	for _, l := range libs[1:] {
		a, _ := releaseNumbers(l.Version)
		b, _ := releaseNumbers(best.Version)
		if c := compareRelease(a, b); c > 0 || c == 0 && l.Built.After(best.Built) {
			best = l
		}
	}
	return best
}

// probeLocal reports what the index holds, without the network.
func probeLocal(_ context.Context, _ *http.Client, c *config.Config) (health.Status, string) {
	path, err := docsindex.DefaultPath()
	if err != nil {
		return health.StatusMisconfigured, err.Error()
	}
	store := docsindex.Open(path)
	if !store.Exists() {
		return health.StatusSkipped, "nothing indexed (ketch docs index <library|docs-url>)"
	}
	libs, err := store.Libraries()
	if err != nil {
		return health.StatusMisconfigured, fmt.Sprintf("cannot read %s: %v", path, err)
	}
	ranking := "embeddings"
	if _, err := embed.Locate(c.String("model_path")); err != nil {
		ranking = "keywords (no model installed)"
	}
	return health.StatusOK, fmt.Sprintf("%d libraries indexed in %s; ranking by %s", len(libs), path, ranking)
}

func localProvider() Provider {
	return Provider{
		ID:          "local",
		Name:        "Local index",
		LibraryHelp: "an indexed library's name or docs root URL (ketch docs index --list)",
		// model_path points at an embedding model directory; unset, ketch
		// uses the one `ketch model install` put in its cache directory.
		Settings: []config.Setting{config.Scalar("model_path")},
		Usable:   func(*config.Config) bool { return true },
		New:      func(c *config.Config, _ Deps) (Searcher, error) { return newLocalFromConfig(c), nil },
		Probe:    probeLocal,
	}
}
