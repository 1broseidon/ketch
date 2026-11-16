package docs

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	"golang.org/x/net/publicsuffix"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/search"
)

// A question that names no library (design/docs-evidence.md, Final shape,
// Third pass) is answered from the plain question's search, in its order:
// what `ketch search` returns for it, then the first result that is a
// readable document read, as a caller would read it. No site vote, no
// weights, no second query reordering the first, no addresses mined from
// the results' text and no name guessed from the question: the search
// decides, and the answer's read line shows its rank.
//
//	the plain question's search, in order
//	        │       (Q&A and forum threads, issues, pull requests, commits,
//	        │       source files, videos and social posts left out)
//	the first result that reads: one that fails is skipped with its cause,
//	and the next result read, on any site
//	        │
//	up to two more results of the same site and release, in search order
//	        │       (none readable at all: the search results alone)
//	answered through the evidence pipeline, best page first
//
// The site and resolve machinery serves only a request that names a
// library (--library, or a name@version the question writes that resolve
// confirms).

// bareRequest reports a request that names no library.
func bareRequest(req sfRequest) bool { return req.library == "" }

// answerBare answers a question that names no library (bareRequest). A
// name@version the question writes is resolved beside the search and, when
// resolve confirms its version, answered as a pin; otherwise it is
// ordinary words.
func (s *SearchFirst) answerBare(ctx, cctx context.Context, req sfRequest, tr *sfTrace) Outcome {
	hits, prefs, err := s.bareSearch(cctx, req)
	if req.guessed {
		if m := prefs.resolved.match; m != nil && m.version != "" {
			req.hits = hits
			tr.hits = len(hits)
			return s.readPinned(ctx, cctx, req, hits, prefs, tr)
		}
		req.spec, req.guessed = libSpec{}, false
	}
	req.hits = hits
	tr.hits = len(hits)
	if err != nil {
		return s.failed(ctx, cctx, err)
	}
	o, read := s.readBare(ctx, cctx, req, hits, tr)
	if answered(o) || printableRefusal(o) {
		return s.keep(req, read, o)
	}
	return resultsOnly(dressed(o, req, nil), req.hits)
}

// bareSearch runs the plain question's search (as `ketch search` runs it)
// and, for a name@version the question writes, its resolution beside it.
func (s *SearchFirst) bareSearch(ctx context.Context, req sfRequest) ([]sfHit, sitePrefs, error) {
	if s.web.sites == nil {
		return nil, sitePrefs{}, fmt.Errorf("%w: the search-first backend needs a web search", ErrLibraryRequired)
	}
	var prefs sitePrefs
	done := make(chan struct{})
	go func() {
		defer close(done)
		if req.guessed {
			prefs = s.prefs(ctx, req)
		}
	}()
	sctx, cancel := context.WithTimeout(ctx, sfSearchTimeout)
	defer cancel()
	list, err := s.web.sites.Search(sctx, strings.Join(strings.Fields(req.query), " "), sfSearchResults)
	<-done
	if err != nil {
		return nil, prefs, fmt.Errorf("search: web search failed: %w", err)
	}
	hits := plainHits(list, 0)
	if len(hits) == 0 {
		return nil, prefs, fmt.Errorf("search: %w", errSearchEmpty)
	}
	return hits, prefs, nil
}

// plainHits are a search's results as hits in the search's order, each
// ranked by its place in the list (after offset), one per page; an address
// that is no well-formed public web page is left out.
func plainHits(list []search.Result, offset int) []sfHit {
	var out []sfHit
	seen := map[string]bool{}
	for i, res := range list {
		u, err := url.Parse(strings.TrimSpace(res.URL))
		if err != nil || !wellFormedHost(u) {
			continue
		}
		u.Fragment, u.RawFragment = "", ""
		if k := linkKey(u); !seen[k] {
			seen[k] = true
			out = append(out, sfHit{u: u, title: res.Title, desc: resultSnippet(res.Description, res.Content), rank: offset + i + 1, pos: len(out)})
		}
	}
	return out
}

// wellFormedHost reports a public web address whose host ends in a known
// suffix: a top-level domain (ICANN's list) or a private registry's
// (github.io), or one reserved for testing and examples (RFC 2606). A host
// cut short in a result's text (docs.python.or) has none of these.
func wellFormedHost(u *url.URL) bool {
	if !isPublicWebURL(u) {
		return false
	}
	suffix, icann := publicsuffix.PublicSuffix(strings.TrimSuffix(strings.ToLower(u.Hostname()), "."))
	return icann || strings.Contains(suffix, ".") || suffix == "test" || suffix == "example" || suffix == "invalid"
}

// bareCandidates are the hits to read, in the search's order, the ones that
// are no document (notDocument) left out.
func bareCandidates(hits []sfHit) []sfHit {
	var out []sfHit
	for _, h := range hits {
		if !notDocument(h.u) {
			out = append(out, h)
		}
	}
	return out
}

// bareRead is the state of readBare.
type bareRead struct {
	s       *SearchFirst
	cands   []sfHit
	tried   []bool
	seen    map[string]bool
	texts   map[[32]byte]bool
	p       *sfPages
	fails   readFails
	first   *url.URL // the first page read, as the search gave it
	served  *url.URL // and where it was served from
	skipped int      // candidates passed over before the first page
}

// readBare reads the first readable document among hits, then up to
// sfExactPages-1 more of its site and release, in search order, and
// answers from them. It returns the read a repeat within cache_ttl answers
// from.
func (s *SearchFirst) readBare(ctx, cctx context.Context, req sfRequest, hits []sfHit, tr *sfTrace) (Outcome, sfRead) {
	cands := bareCandidates(hits)
	x := &bareRead{s: s, cands: cands, tried: make([]bool, len(cands)),
		seen: map[string]bool{}, texts: map[[32]byte]bool{}, p: &sfPages{}}
	tr.docsHits = len(cands)
	if lib, ok := x.readFirst(cctx, tr); ok {
		// A site the caller indexed answers from the index.
		tr.local, tr.site = lib.Root, lib.Root
		return s.answerIndexed(ctx, lib, req.query, req.opts, ""), sfRead{Root: lib.Root}
	}
	x.readMore(cctx, tr)
	tr.fellThrough, tr.exact = x.skipped, len(x.p.exact)
	if len(x.p.exact) == 0 {
		return x.unread(ctx, cctx, s), sfRead{}
	}
	x.p.root = x.root()
	x.p.title, x.p.name = readTitle(x.p.root, nil)
	tr.sites++
	tr.site = x.p.root.String()
	o := s.answerRead(ctx, x.p, req, sfOrigin{}, nil)
	r := readOf(x.p)
	r.Tried = otherReleaseNote(x.p)
	o.Note = joinNote(r.Tried, o.Note)
	return o, r
}

// readFirst reads the candidates in order until one reads: that is the
// first page. Each is read with those right after it of its release, at
// once, which are its next pages when it reads. It reports the indexed
// library that holds a candidate's site, met before any page read.
func (x *bareRead) readFirst(ctx context.Context, tr *sfTrace) (docsindex.Library, bool) {
	for i := 0; i < len(x.cands) && x.first == nil && ctx.Err() == nil; {
		if lib, ok := x.s.indexedOver(docsTree(x.cands[i].u)); ok {
			return lib, true
		}
		wave := []int{i}
		for j := i + 1; j < len(x.cands) && len(wave) < sfExactPages && sameRelease(x.cands[i].u, x.cands[j].u); j++ {
			wave = append(wave, j)
		}
		i = wave[len(wave)-1] + 1
		x.read(ctx, wave, tr)
		if x.first == nil {
			x.skipped += len(wave)
		}
	}
	return docsindex.Library{}, false
}

// readMore reads the next results of the first page's site and release, in
// search order, until sfExactPages are read or none is left.
func (x *bareRead) readMore(ctx context.Context, tr *sfTrace) {
	for len(x.p.exact) < sfExactPages && x.first != nil && ctx.Err() == nil {
		var wave []int
		for j := range x.cands {
			if !x.tried[j] && sameRelease(x.first, x.cands[j].u) && len(wave) < sfExactPages-len(x.p.exact) {
				wave = append(wave, j)
			}
		}
		if len(wave) == 0 {
			return
		}
		x.read(ctx, wave, tr)
	}
}

// read fetches the candidates at wave, each page once, robots.txt's
// refusals and failed reads skipped with their causes; a page of another
// release than the first page's is not kept.
func (x *bareRead) read(ctx context.Context, wave []int, tr *sfTrace) {
	var links []webLink
	var at []int
	for _, j := range wave {
		x.tried[j] = true
		h := x.cands[j]
		k := pageIdentity(h.u, hostRoot(h.u), true)
		if x.seen[k] {
			continue
		}
		x.seen[k] = true
		links = append(links, webLink{url: h.u, title: h.title, description: h.desc})
		at = append(at, j)
	}
	if len(links) == 0 {
		return
	}
	pages, by, f := x.s.fetchIn(ctx, hostRoot(links[0].url), isPublicWebURL, links)
	tr.planned, tr.fetched, tr.failed = tr.planned+len(links), tr.fetched+len(pages), tr.failed+f.n()
	for _, rf := range f.list {
		x.fails.add(rf.page, rf.cause, rf.detail)
		x.p.skipped = append(x.p.skipped, PageSkipped{URL: rf.page, Cause: causeText(rf)})
	}
	// Pages in the search's order: fetchIn keeps the links' order.
	for k, pg := range pages {
		h := x.cands[at[slices.IndexFunc(links, func(l webLink) bool { return l.url == by[k].url })]]
		if t := contentKey(pg); x.texts[t] || x.first != nil && !sameRelease(x.first, h.u) {
			continue
		}
		x.texts[contentKey(pg)] = true
		if x.first == nil {
			x.first = h.u
			x.served, _ = url.Parse(pg.URL)
		}
		x.p.exact, x.p.exactBy = append(x.p.exact, pg), append(x.p.exactBy, by[k])
		x.p.reads = append(x.p.reads, PageRead{URL: pg.URL, Rank: h.rank})
	}
}

// root is the docs tree of the first page as served, narrowed to its
// release: the root the pages read are held under.
func (x *bareRead) root() webRoot {
	at := x.served
	if at == nil {
		at = x.first
	}
	return releaseSite(sfSite{root: docsTree(at)}, at).root
}

// unread is the outcome of a read that found no readable page: the causes,
// and every page tried as skipped.
func (x *bareRead) unread(ctx, cctx context.Context, s *SearchFirst) Outcome {
	var err error
	switch {
	case len(x.cands) == 0:
		err = fmt.Errorf("search: none of the search results is a document to read: %w", ErrNoDocsFound)
	default:
		err = fmt.Errorf("search: none of the %d results tried could be read: %s: %w", len(x.p.skipped), failCounts(x.fails), ErrNotFound)
	}
	o := s.failed(ctx, cctx, err)
	o.Skipped = x.p.skipped
	return o
}

// failCounts is reads that failed counted by cause: "2 not found (HTTP
// 404), 1 timed out".
func failCounts(f readFails) string {
	counts := map[string]int{}
	var order []string
	for _, rf := range f.list {
		if counts[rf.cause] == 0 {
			order = append(order, rf.cause)
		}
		counts[rf.cause]++
	}
	parts := make([]string, 0, len(order))
	for _, c := range order {
		part := fmt.Sprintf("%d %s", counts[c], c)
		if d := f.details[c]; d != "" {
			part += " (" + d + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// otherReleaseNote says, when a page skipped before the first page read is
// the same page in another release (docs.python.org/3.10/… refused,
// /3/… read), that the page read is of another release than the one the
// search ranked first; "" otherwise.
func otherReleaseNote(p *sfPages) string {
	if len(p.exact) == 0 {
		return ""
	}
	read, err := url.Parse(p.exact[0].URL)
	if err != nil {
		return ""
	}
	for _, sk := range p.skipped {
		u, err := url.Parse(sk.URL)
		if err != nil || sameRelease(u, read) || scopeHost(u) != scopeHost(read) || releaselessPath(u) != releaselessPath(read) {
			continue
		}
		return fmt.Sprintf("%s was skipped (%s); %s, read instead, is the same page of another release", sk.URL, sk.Cause, p.exact[0].URL)
	}
	return ""
}

// releaselessPath is u's path without its version labels and a trailing
// slash: /3.10/library/datetime.html and /3/library/datetime.html are one.
func releaselessPath(u *url.URL) string {
	var keep []string
	for _, seg := range pathSegments(u.Path) {
		if _, ok := variantLabel(seg); !ok {
			keep = append(keep, seg)
		}
	}
	return strings.Join(keep, "/")
}

// codeExts are file extensions of source code: a forge's view of one is no
// document.
var codeExts = map[string]bool{
	".go": true, ".py": true, ".rs": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true, ".jsx": true,
	".java": true, ".kt": true, ".kts": true, ".scala": true, ".groovy": true, ".c": true, ".h": true, ".cc": true, ".cpp": true,
	".cxx": true, ".hpp": true, ".hh": true, ".cs": true, ".fs": true, ".rb": true, ".php": true, ".swift": true, ".m": true,
	".mm": true, ".ex": true, ".exs": true, ".erl": true, ".hs": true, ".clj": true, ".lua": true, ".pl": true, ".pm": true,
	".r": true, ".dart": true, ".zig": true, ".nim": true, ".sh": true, ".bash": true, ".ps1": true, ".sql": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true, ".gradle": true, ".lock": true, ".mod": true, ".sum": true,
}

// forgeHosts are code forges and their raw-file hosts.
var forgeHosts = []string{
	"github.com", "githubusercontent.com", "gitlab.com", "bitbucket.org", "codeberg.org", "sr.ht", "googlesource.com", "sourceforge.net",
}

// threadHosts are hosts whose every page is a question, a thread or a post.
var threadHosts = []string{
	"stackoverflow.com", "stackexchange.com", "superuser.com", "serverfault.com", "askubuntu.com", "mathoverflow.net", "quora.com",
	"reddit.com", "news.ycombinator.com", "twitter.com", "x.com", "facebook.com", "instagram.com", "threads.net", "bsky.app",
	"mastodon.social", "youtube.com", "youtu.be", "vimeo.com", "tiktok.com", "twitch.tv",
}

// threadHostLabels are first host labels of forums.
var threadHostLabels = map[string]bool{
	"forum": true, "forums": true, "community": true, "discuss": true, "discourse": true, "answers": true,
}

// threadSegments are directories under which a site keeps threads,
// issues, changes and videos: /questions/123/x, /issues/45, /pull/6.
var threadSegments = map[string]bool{
	"questions": true, "question": true, "forum": true, "forums": true, "discussions": true, "discussion": true,
	"threads": true, "thread": true, "topic": true, "topics": true, "issues": true, "issue": true, "pull": true, "pulls": true,
	"merge_requests": true, "commit": true, "commits": true, "compare": true, "watch": true,
}

// notDocument reports a search result that is no document by its address's
// shape alone: a Q&A or forum thread, an issue, a pull request or commit, a
// source file, a video or a social post. A Markdown document on a forge (a
// README, a .md file, a wiki page) is a document; so is any page of any
// other site, a blog or a tutorial included.
func notDocument(u *url.URL) bool {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	first, _, _ := strings.Cut(strings.TrimPrefix(host, "www."), ".")
	if onDomains(host, threadHosts) || threadHostLabels[first] {
		return true
	}
	segs := pathSegments(u.Path)
	for i, seg := range segs {
		low := strings.ToLower(seg)
		if threadSegments[low] && i < len(segs)-1 {
			return true
		}
		if low == "t" && i < len(segs)-1 && isDigits(segs[len(segs)-1]) {
			return true // a Discourse topic: /t/slug/123
		}
	}
	return forgeSource(u)
}

// forgeSource reports a source file on a code forge, or a docs.rs source
// view.
func forgeSource(u *url.URL) bool {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	segs := pathSegments(u.Path)
	if scopeHost(u) == "docs.rs" && isDocsRSSource(segs) {
		return true
	}
	if !onDomains(host, forgeHosts) || len(segs) == 0 {
		return false
	}
	return codeExts[strings.ToLower(path.Ext(segs[len(segs)-1]))]
}
