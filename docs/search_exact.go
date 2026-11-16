package docs

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Exact pages first (design/docs-evidence.md, Final shape). Once the site
// is chosen, the search hits on it are read at their exact addresses, in
// search rank order, before anything else: never a site root, a sibling
// or a discovered page at this stage. At most sfExactPages are read, every
// one after the first of the same release tree as the first (a /v7/ page
// is never read beside a /v6/ hit). A hit that cannot be read is skipped,
// with its cause, and the next one read.
//
// The answer fills from the best page first, in search rank order. Only
// when an identifier the question writes is on none of the exact pages,
// or nothing on them matches, are more pages read (plan: hits, nav
// siblings and discovery, up to sfMaxPages) in the release tree of the
// first; their text is walked after the exact pages', with what is left,
// and the answer says it widened, and for which terms.

// sfExactPages is how many of a site's search hits are read.
const sfExactPages = 3

// sfCand is a page to read at its exact address: a search hit with its
// rank, or a page the request names (via).
type sfCand struct {
	link webLink
	rank int
	via  string
}

// sfPages are the pages an answer from a search reads: the exact pages
// (in search rank order, then best first once ordered) and, when it
// widened, the pages read beside them.
type sfPages struct {
	root        webRoot // the release tree read: the library's root
	title, name string
	exact       []webPage
	exactBy     []webLink
	reads       []PageRead // the exact pages' read lines, at the same places
	wide        []webPage
	wideBy      []webLink
	widened     *Widening
	skipped     []PageSkipped
}

// exactCandidates are the pages of site to read at their exact address,
// in order: the page the library URL names, then the site's search hits
// (every docs tree of it) by rank, then the root resolve found when no
// hit is there.
func exactCandidates(site sfSite) []sfCand {
	var out []sfCand
	seen := map[string]bool{}
	add := func(c sfCand) {
		if k := linkKey(c.link.url); !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	var hits []sfHit
	for _, t := range append([]sfSite{site}, site.alts...) {
		hits = append(hits, t.hits...)
	}
	slices.SortStableFunc(hits, func(a, b sfHit) int { return a.rank - b.rank })
	if site.page != nil {
		c := sfCand{link: webLink{url: site.page}, via: firstNonEmpty(site.pageVia, "the library URL")}
		for _, h := range hits {
			if linkKey(h.u) == linkKey(site.page) {
				c.rank, c.via, c.link.title = h.rank, "", h.title
			}
		}
		add(c)
	}
	for _, h := range hits {
		add(sfCand{link: webLink{url: h.u, title: h.title, description: h.desc}, rank: h.rank})
	}
	if site.fallback != nil {
		add(sfCand{link: webLink{url: site.fallback}, via: "the docs root resolve found"})
	}
	return out
}

// releaseOf is the release tree a page's address names: its host, a
// leading locale, and every version label among its directories.
func releaseOf(u *url.URL) string {
	parts := []string{scopeHost(u)}
	segs := pathSegments(u.Path)
	for i, seg := range segs {
		if i == len(segs)-1 && !strings.HasSuffix(u.Path, "/") {
			break // the page's own name
		}
		if l, ok := variantLabel(seg); ok {
			parts = append(parts, l)
		} else if i == 0 && localeRe.MatchString(seg) {
			parts = append(parts, strings.ToLower(seg))
		}
	}
	return strings.Join(parts, "/")
}

// sameRelease reports two pages of one release tree (releaseOf).
func sameRelease(a, b *url.URL) bool { return releaseOf(a) == releaseOf(b) }

// hostRoot is the root of u's host: the scope a page read at its exact
// address may be served from.
func hostRoot(u *url.URL) webRoot {
	r, err := parseWebRoot(u.Scheme + "://" + u.Host + "/")
	if err != nil {
		return webRoot{u: u, prefix: "/"}
	}
	return r
}

// readExact reads site's exact pages (exactCandidates): at most
// sfExactPages, the first readable one and those of its release tree, in
// waves so that one unreadable hit costs one more fetch, not a refusal.
// It returns the pages, the read failures (skipped, with robots.txt's
// refusals counted in disallowed), and the release tree to widen in.
func (s *SearchFirst) readExact(ctx context.Context, site sfSite, tr *sfTrace) (*sfPages, readFails, int, sfSite) {
	x := &exactRead{s: s, p: &sfPages{root: site.root}, cands: exactCandidates(site), robots: map[string]robotsRules{},
		seen: map[string]bool{}, texts: map[[32]byte]bool{}}
	for len(x.p.exact) < sfExactPages {
		wave := x.wave(ctx)
		if len(wave) == 0 {
			break
		}
		x.read(ctx, wave, tr)
	}
	tree := site
	if x.served != nil {
		tree = releaseSite(site, x.served)
	}
	x.p.root = tree.root
	return x.p, x.fails, x.disallowed, tree
}

// exactRead is the state of readExact.
type exactRead struct {
	s          *SearchFirst
	p          *sfPages
	cands      []sfCand
	next       int // the next candidate
	robots     map[string]robotsRules
	seen       map[string]bool
	texts      map[[32]byte]bool
	fails      readFails
	disallowed int
	// first is the first page read, served where it was served from.
	first, served *url.URL
}

// wave is the next candidates to read at once: as many as pages are
// still wanted, of the first page's release (or the wave's first's), each
// page once, robots.txt's refusals skipped.
func (x *exactRead) wave(ctx context.Context) []sfCand {
	var wave []sfCand
	for ; x.next < len(x.cands) && len(wave) < sfExactPages-len(x.p.exact); x.next++ {
		c := x.cands[x.next]
		ref := x.first
		if ref == nil && len(wave) > 0 {
			ref = wave[0].link.url
		}
		k := pageIdentity(c.link.url, hostRoot(c.link.url), true)
		if ref != nil && !sameRelease(ref, c.link.url) || x.seen[k] || !isResultLink(c.link.url) {
			continue
		}
		x.seen[k] = true
		if !x.allowed(ctx, c.link.url) {
			x.disallowed++
			x.p.skipped = append(x.p.skipped, PageSkipped{URL: c.link.url.String(), Cause: "robots.txt disallows it"})
			continue
		}
		wave = append(wave, c)
	}
	return wave
}

// allowed reports u allowed by its host's robots.txt.
func (x *exactRead) allowed(ctx context.Context, u *url.URL) bool {
	host := scopeHost(u)
	if _, ok := x.robots[host]; !ok {
		x.robots[host] = x.s.web.robots(ctx, hostRoot(u))
	}
	return x.robots[host].allowed(u)
}

// read fetches wave and keeps the pages that came back, each text once
// and of the first page's release, the failures as skipped pages.
func (x *exactRead) read(ctx context.Context, wave []sfCand, tr *sfTrace) {
	links := make([]webLink, len(wave))
	for j, c := range wave {
		links[j] = c.link
	}
	pages, by, f := x.s.fetchIn(ctx, hostRoot(wave[0].link.url), onSiteOf(wave[0].link.url), links)
	tr.planned, tr.fetched, tr.failed = tr.planned+len(links), tr.fetched+len(pages), tr.failed+f.n()
	for _, rf := range f.list {
		x.fails.add(rf.page, rf.cause, rf.detail)
		x.p.skipped = append(x.p.skipped, PageSkipped{URL: rf.page, Cause: causeText(rf)})
	}
	for j, pg := range pages {
		c := wave[slices.IndexFunc(wave, func(c sfCand) bool { return c.link.url == by[j].url })]
		if t := contentKey(pg); x.texts[t] || x.first != nil && !sameRelease(x.first, c.link.url) {
			continue
		}
		x.texts[contentKey(pg)] = true
		if x.first == nil {
			x.first = c.link.url
			x.served, _ = url.Parse(pg.URL)
		}
		x.p.exact, x.p.exactBy = append(x.p.exact, pg), append(x.p.exactBy, by[j])
		x.p.reads = append(x.p.reads, PageRead{URL: pg.URL, Rank: c.rank, Via: c.via})
	}
}

// onSiteOf is the scope a page read at its exact address may be served
// from: any host of its registrable domain (docs.x.dev moving its pages
// to x.dev/docs/), never another site.
func onSiteOf(u *url.URL) func(*url.URL) bool {
	dom := registrableDomain(u.Hostname())
	return func(v *url.URL) bool { return isPublicWebURL(v) && registrableDomain(v.Hostname()) == dom }
}

// causeText is a read failure as a skipped line says it.
func causeText(f readFail) string {
	if f.detail != "" {
		return f.cause + ", " + f.detail
	}
	return f.cause
}

// releaseSite is the docs tree of site (or of its other trees) that holds
// page, narrowed to the release directory the page names when that is
// deeper than the tree's root: the root the answer's pages are held under
// and where widening reads, so it never reads another release.
func releaseSite(site sfSite, page *url.URL) sfSite {
	tree := site
	tree.alts, tree.page, tree.fallback = nil, nil, nil
	for _, t := range append([]sfSite{site}, site.alts...) {
		if t.root.contains(page) && (!tree.root.contains(page) || len(t.root.prefix) > len(tree.root.prefix)) {
			tree.root = t.root
		}
	}
	if !tree.root.contains(page) {
		tree.root = docsTree(page)
	}
	segs := pathSegments(page.Path)
	depth := len(pathSegments(tree.root.prefix))
	for i := depth; i < len(segs)-1; i++ {
		if _, ok := variantLabel(segs[i]); ok {
			if r, err := parseWebRoot(tree.root.origin() + segmentsPath(segs[:i+1])); err == nil {
				tree.root = r
			}
		}
	}
	var hits []sfHit
	for _, t := range append([]sfSite{site}, site.alts...) {
		for _, h := range t.hits {
			if tree.root.contains(h.u) {
				hits = append(hits, h)
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b sfHit) int { return a.rank - b.rank })
	tree.hits = hits
	return tree
}

// widener is the widening of an answer from p: read the pages plan lists
// in tree (the release tree of the first exact page) beside those read,
// for the terms missing from them (none: no exact page could be read),
// its read failures added to failed. It reports whether it read any.
func (s *SearchFirst) widener(ctx context.Context, tree sfSite, prof *siteProfile, req sfRequest, p *sfPages, failed *readFails, tr *sfTrace) func([]string) bool {
	return func(missing []string) bool {
		links, _ := s.plan(ctx, tree, prof, req.query)
		read := map[string]bool{}
		for _, pg := range p.exact {
			if u, err := url.Parse(pg.URL); err == nil {
				read[pageIdentity(u, tree.root, true)] = true
			}
		}
		for _, l := range p.exactBy {
			read[pageIdentity(l.url, tree.root, true)] = true
		}
		for _, sk := range p.skipped {
			if u, err := url.Parse(sk.URL); err == nil {
				read[pageIdentity(u, tree.root, true)] = true
			}
		}
		links = slices.DeleteFunc(links, func(l webLink) bool { return read[pageIdentity(l.url, tree.root, true)] })
		if len(links) == 0 {
			return false
		}
		pages, by, fails := s.fetch(ctx, tree.root, links)
		tr.planned, tr.fetched, tr.failed = tr.planned+len(links), tr.fetched+len(pages), tr.failed+fails.n()
		for _, f := range fails.list {
			failed.add(f.page, f.cause, f.detail)
		}
		texts := map[[32]byte]bool{}
		for _, pg := range p.exact {
			texts[contentKey(pg)] = true
		}
		for i, pg := range pages {
			if !texts[contentKey(pg)] {
				p.wide, p.wideBy = append(p.wide, pg), append(p.wideBy, by[i])
			}
		}
		if len(p.wide) == 0 {
			return false
		}
		tr.widened = true
		TraceFrom(ctx).fallback("widened")
		p.widened = &Widening{Pages: len(p.wide), Root: tree.root.String(), Terms: missing}
		return true
	}
}

// widenNeed is what an answer from the exact pages held in lib lacks: the
// identifiers the question writes that are on none of them, and whether
// to widen: some are missing, or none of the question's words is on the
// pages at all. A plain word missing from a few pages is the docs' other
// wording, not a reason to read more (Final shape).
func (l *Local) widenNeed(lib docsindex.Library, query string) ([]string, bool, error) {
	g, err := l.gather(lib, query, nil)
	if err != nil {
		return nil, false, err
	}
	markSearchRead(g.written)
	v := verdict(g.written, g.sections)
	var missing []string
	for _, wt := range g.written {
		if wt.ident && wt.sections == 0 {
			missing = append(missing, wt.text)
		}
	}
	return missing, len(missing) > 0 || v.refused || len(g.pool) == 0, nil
}

// otherResults are the search hits an answer did not read (read and
// skipped name the pages it read or tried), each a result with a title,
// at most sfOtherResults, in rank order.
func otherResults(hits []sfHit, read []PageRead, wide []webPage, skipped []PageSkipped) []OtherResult {
	done := map[string]bool{}
	mark := func(raw string) {
		if u, err := url.Parse(raw); err == nil {
			done[samePageKey(u)] = true
		}
	}
	for _, r := range read {
		mark(r.URL)
	}
	for _, p := range wide {
		mark(p.URL)
	}
	for _, sk := range skipped {
		mark(sk.URL)
	}
	var out []OtherResult
	for _, h := range hits {
		if len(out) == sfOtherResults {
			break
		}
		if strings.TrimSpace(h.title) == "" || done[samePageKey(h.u)] {
			continue
		}
		done[samePageKey(h.u)] = true
		out = append(out, OtherResult{Title: oneLine(h.title, sfTitleChars), URL: h.u.String(), Snippet: oneLine(h.desc, sfSnippetChars), Rank: h.rank})
	}
	return out
}

const (
	// sfOtherResults is how many other search results an answer lists.
	sfOtherResults = 5
	// sfTitleChars and sfSnippetChars cap an other result's title and
	// snippet: the list stays within about 250 tokens.
	sfTitleChars   = 60
	sfSnippetChars = 70
)

// oneLine is s on one line, cut to about n bytes at a word, with "…".
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1] // never half a character
	}
	if i := strings.LastIndexByte(cut, ' '); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:.") + "…"
}

// resultsOnly is the answer to a question whose search returned results
// but no page could be printed from them: what was tried and why it gave
// nothing (note, and the skipped pages), and the search results. It is an
// answer, not a refusal: the results are what search alone would give.
func resultsOnly(o Outcome, hits []sfHit) Outcome {
	others := otherResults(hits, nil, nil, o.Skipped)
	if len(others) == 0 {
		return o
	}
	note := "no page was printed: " + strings.TrimSuffix(strings.TrimPrefix(errText(o.err), "search: "), ": "+ErrNotFound.Error())
	out := Outcome{Status: Partial, Reason: "results_only", Backend: SearchBackend, Results: []Result{}, Note: note,
		Skipped: o.Skipped, OtherResults: others, IndexedNear: o.IndexedNear, named: o.named}
	out.Attempts = []Attempt{{Backend: SearchBackend, Status: Partial, Reason: "results_only"}}
	return out
}

// errText is err's message, "" for none.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// pagesOf are the addresses of pages.
func pagesOf(pages []webPage) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = p.URL
	}
	return out
}

// noPageErr is the error of a site whose exact pages and lists gave no
// text: its pages' causes, counted, and what to do.
func noPageErr(root webRoot, n int, fails readFails) error {
	why := readFailNote(root, fails.list, fails.details)
	return fmt.Errorf("search: none of the %d pages of %s could be read: %s: %w", n, root, why, ErrNotFound)
}
