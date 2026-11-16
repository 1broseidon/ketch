package docs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// The pages a question reads (design/docs-evidence.md, Storage). A
// question saves nothing: the pages it read are held as a library in
// memory for the call (readLibrary) and answered through the same
// pipeline as an indexed library, and they live in the page cache under
// cache_ttl and nowhere else. Only `ketch docs index` writes the docs
// index.
//
// What a repeat of the question needs to skip the web search, the site
// pick and discovery is kept beside the pages, in the page cache under the
// same TTL (through ResolutionCache): the docs root read and the pages
// read, with the text of those the page cache does not hold (llms-full.txt
// chunks, Markdown sources). The key is the question's terms and what the
// request names (sfReadKey). A repeat within the TTL reads that entry and
// the pages from the page cache: no web search and no page fetch.

// resolvedBySearch is a library's Resolved when an earlier ketch stored
// the pages a question read in the index (a by-product library). Such a
// library is never read, and the next `docs index` build removes it.
const resolvedBySearch = "search"

// BuiltBySearch reports a library an earlier ketch stored as a question's
// by-product (Resolved "search"): neither read nor listed, removed by the
// next build.
func BuiltBySearch(l docsindex.Library) bool { return l.Resolved == resolvedBySearch }

// Readable is libs without the ones BuiltBySearch reports.
func Readable(libs []docsindex.Library) []docsindex.Library {
	return slices.DeleteFunc(slices.Clone(libs), BuiltBySearch)
}

// readLibrary holds pages read under root as a library in memory, with
// the tables, sections and full-text rows an indexed library has, so the
// evidence pipeline answers from it as from the index. Each page is
// sectioned from itself alone (indexBuild.alone), as --section on it
// sections it again (readSection), so every reference an answer prints
// resolves. The caller closes the Local's store.
func readLibrary(root webRoot, title, siteName string, pages []webPage) (*Local, docsindex.Library, error) {
	store, err := docsindex.OpenMemory(ftsTokenizer{})
	if err != nil {
		return nil, docsindex.Library{}, fmt.Errorf("search: %w", err)
	}
	key := root.String()
	label, _ := rootVersion(root)
	b := &indexBuild{store: store, key: key, version: label, title: title, source: resolvedBySearch, alone: true}
	var report IndexReport
	todo := b.diff(pages, nil, &report)
	if err := putPageBatches(store, key, todo); err != nil {
		store.Close()
		return nil, docsindex.Library{}, fmt.Errorf("search: %w", err)
	}
	lib := docsindex.Library{
		Key: key, Library: key, Root: key, Version: label, Title: title, SiteName: siteName,
		Format: docsindex.FormatVersion, Built: time.Now().UTC(), Capped: true, Pages: len(todo), Discovered: len(todo),
		Sources: []string{"web search"},
	}
	for _, pd := range todo {
		lib.Sections += pd.Page.Sections
	}
	return NewLocal(store), lib, nil
}

// answerRead answers req from the pages p holds, read and held in memory
// with the origin siteOrigin gave the site: from the exact pages, best
// first, and, when widen (nil for a repeat, which has its pages) reads
// more for the identifiers they lack or p already widened, the pages
// widened to after them.
func (s *SearchFirst) answerRead(ctx context.Context, p *sfPages, req sfRequest, origin sfOrigin, widen func([]string) bool) Outcome {
	pages := p.exact
	if p.widened != nil {
		pages = append(slices.Clone(p.exact), p.wide...)
	}
	l, lib, err := readLibrary(p.root, p.title, p.name, pages)
	if err != nil {
		return Answer(SearchBackend, nil, err)
	}
	defer func() { l.store.Close() }()
	if p.widened == nil && len(p.exact) > 0 && widen != nil {
		if l, lib, err = s.widened(l, lib, p, req.query, widen); err != nil {
			return Answer(SearchBackend, nil, fmt.Errorf("search: %w", err))
		}
	}
	if req.version != "" {
		lib.Version = req.version
	}
	opts := req.opts
	opts.live, opts.origin = true, origin.text
	opts.pages = pagesOf(p.exact)
	o := l.answer(ctx, lib, req.query, opts)
	o.Read, o.Skipped, o.Widened = p.reads, p.skipped, p.widened
	for i := range o.Results {
		o.Results[i].Source = SearchBackend
	}
	o.Backend = SearchBackend
	for i := range o.Attempts {
		o.Attempts[i].Backend = SearchBackend
	}
	return o
}

// readTitle is the title and name of the site at root, from its top
// hit's profile when it was read.
func readTitle(root webRoot, prof *siteProfile) (title, name string) {
	if prof != nil {
		name = prof.meta["og:site_name"]
	}
	return firstNonEmpty(name, scopeHost(root.u)), name
}

// widened widens an answer from p's exact pages, held in l, when they
// lack an identifier of query (widenNeed): then the library holds every
// page read.
func (s *SearchFirst) widened(l *Local, lib docsindex.Library, p *sfPages, query string, widen func([]string) bool) (*Local, docsindex.Library, error) {
	missing, need, err := l.widenNeed(lib, query)
	if err != nil || !need || !widen(missing) {
		return l, lib, err
	}
	l.store.Close()
	return readLibrary(p.root, p.title, p.name, append(slices.Clone(p.exact), p.wide...))
}

// sfReadVersion versions the question cache's key and value.
const sfReadVersion = "docs-question/v7"

// sfRead is what the question cache keeps of a read: the docs root and,
// unless an indexed library answered for it, the pages read in search
// rank order (the exact pages, then those a widening read), what was
// skipped and the search results not read: all a repeat needs to print
// the same answer without the network.
type sfRead struct {
	Root    string `json:"root"`
	Version string `json:"version,omitempty"`
	// Pin is the request resolution verified. Keep it separately from the
	// site's label: SQLAlchemy calls 2.0 "20", Express calls 4 "4x".
	Pin      string        `json:"pin,omitempty"`
	Title    string        `json:"title,omitempty"`
	SiteName string        `json:"site_name,omitempty"`
	Origin   string        `json:"origin,omitempty"`
	Pages    []sfReadPage  `json:"pages,omitempty"`
	Widened  *Widening     `json:"widened,omitempty"`
	Skipped  []PageSkipped `json:"skipped,omitempty"`
	Others   []OtherResult `json:"others,omitempty"`
	// Tried names the sites tried before this one that gave no text, and
	// why, as the answer's note said it (keepTried, keepAfter).
	Tried string `json:"tried,omitempty"`
}

// sfReadPage is one page read: the link it was read by, the address the
// answer cites it at, for a page the page cache does not hold (its text
// came from llms-full.txt or a Markdown source) its text, and for an
// exact page its read line's rank or why it was read; Wide marks a page a
// widening read.
type sfReadPage struct {
	Link     string `json:"link"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Markdown string `json:"md,omitempty"`
	Rank     int    `json:"rank,omitempty"`
	Via      string `json:"via,omitempty"`
	Wide     bool   `json:"wide,omitempty"`
}

// widePages are the pages of r a widening read, as their addresses.
func (r sfRead) widePages() []webPage {
	var out []webPage
	for _, p := range r.Pages {
		if p.Wide {
			out = append(out, webPage{URL: p.URL})
		}
	}
	return out
}

// sfReadKey keys a request's read: the question's terms as the ranking
// normalizes them (stopwords and question filler out, inflections folded,
// as a set: "how do I configure X" and "configuring X" are one question),
// and what the request names (its library, a docs root by its identity,
// and a pinned spec). "" for a question with no terms.
func sfReadKey(req sfRequest) string {
	terms := slices.Clone(withoutFiller(queryTerms(req.query)))
	if len(terms) == 0 {
		return ""
	}
	slices.Sort(terms)
	terms = slices.Compact(terms)
	scope := strings.ToLower(strings.TrimSpace(req.library))
	if r, err := parseWebRoot(req.library); err == nil && isHTTPURL(req.library) {
		scope = linkKey(r.u)
	}
	return strings.Join([]string{sfReadVersion, scope, strings.ToLower(req.spec.raw), strings.Join(terms, " ")}, "|")
}

// readOf is the read to keep of the pages p holds.
func readOf(p *sfPages) sfRead {
	r := sfRead{Root: p.root.String(), Title: p.title, SiteName: p.name, Widened: p.widened, Skipped: p.skipped}
	add := func(pg webPage, by webLink, rp sfReadPage) {
		rp.Link, rp.Title, rp.URL = by.url.String(), by.title, pg.URL
		if pg.markdown {
			rp.Title, rp.Markdown = pg.Title, pg.Markdown
		}
		r.Pages = append(r.Pages, rp)
	}
	for i, pg := range p.exact {
		add(pg, p.exactBy[i], sfReadPage{Rank: p.reads[i].Rank, Via: p.reads[i].Via})
	}
	for i, pg := range p.wide {
		add(pg, p.wideBy[i], sfReadPage{Wide: true})
	}
	return r
}

// saveRead keeps req's read in the question cache, and for each page a
// --section follow-up could not find in the page cache by the address the
// answer cites (it was read at another, or its text came from elsewhere),
// how it was read (sfPageKey); nothing without a cache.
func (s *SearchFirst) saveRead(req sfRequest, r sfRead) {
	rc := s.web.resolutionCache()
	key := sfReadKey(req)
	if rc == nil || key == "" {
		return
	}
	if b, err := json.Marshal(r); err == nil {
		rc.PutResolution(key, string(b))
	}
	for _, p := range r.Pages {
		if p.Markdown == "" && p.URL == p.Link {
			continue
		}
		if b, err := json.Marshal(p); err == nil {
			rc.PutResolution(sfPageKey(p.URL), string(b))
		}
	}
}

// sfPageVersion versions the page entries of the question cache.
const sfPageVersion = "docs-page/v1"

// sfPageKey keys how a page a question read was read, by the page as an
// answer cites it (samePageKey: a scheme, "www.", a trailing slash or
// .html aside).
func sfPageKey(page string) string {
	if u, err := url.Parse(page); err == nil {
		page = samePageKey(u)
	}
	return sfPageVersion + "|" + page
}

// pageRead is how a question within the TTL read the page at u: the link
// to read it by through the page cache, or the text it had in hand; the
// link u itself when no entry says otherwise.
func (s *SearchFirst) pageRead(u *url.URL) webLink {
	link := webLink{url: u}
	rc := s.web.resolutionCache()
	if rc == nil {
		return link
	}
	raw, ok := rc.GetResolution(sfPageKey(u.String()))
	if !ok {
		return link
	}
	var p sfReadPage
	if json.Unmarshal([]byte(raw), &p) != nil {
		return link
	}
	if p.Markdown != "" {
		link.page = &webPage{URL: p.URL, Title: p.Title, Markdown: p.Markdown, markdown: true}
		return link
	}
	if l, err := url.Parse(p.Link); err == nil && sameHostURL(l, u) {
		link.url, link.title = l, p.Title
	}
	return link
}

// sameHostURL reports two URLs on one host, "www." aside.
func sameHostURL(a, b *url.URL) bool { return scopeHost(a) == scopeHost(b) }

// loadRead is req's read from the question cache, while it lasts.
func (s *SearchFirst) loadRead(req sfRequest) (sfRead, webRoot, bool) {
	rc := s.web.resolutionCache()
	key := sfReadKey(req)
	if rc == nil || key == "" {
		return sfRead{}, webRoot{}, false
	}
	raw, ok := rc.GetResolution(key)
	if !ok {
		return sfRead{}, webRoot{}, false
	}
	var r sfRead
	if json.Unmarshal([]byte(raw), &r) != nil {
		return sfRead{}, webRoot{}, false
	}
	root, err := parseWebRoot(r.Root)
	if err != nil || !isPublicWebURL(root.u) {
		return sfRead{}, webRoot{}, false
	}
	return r, root, true
}

// fromRead answers a question read before, within the TTL, as it was
// answered: from the indexed library that holds its docs root, else from
// the same pages, read again through the page cache (no web search, no
// site pick, no discovery), with what it skipped and the search results
// it did not read. It reports false when there is no read to repeat or it
// no longer gives an outcome the cache keeps (kept), and the question is
// searched.
func (s *SearchFirst) fromRead(ctx context.Context, req sfRequest, tr *sfTrace) (Outcome, bool) {
	r, root, ok := s.loadRead(req)
	if !ok {
		return Outcome{}, false
	}
	if req.spec.version != "" && (r.Pin != req.spec.version || r.Version == "") {
		return Outcome{}, false
	}
	req.version = r.Version
	if lib, ok := s.indexedOver(root); ok && req.spec.version == "" {
		tr.cached, tr.local = true, lib.Root
		o := s.answerIndexed(ctx, lib, req.query, req.opts, "")
		return o, kept(o)
	}
	if len(r.Pages) == 0 {
		return Outcome{}, false
	}
	links := make([]webLink, 0, len(r.Pages))
	for _, p := range r.Pages {
		u, err := url.Parse(p.Link)
		if err != nil {
			return Outcome{}, false
		}
		l := webLink{url: u, title: p.Title}
		if p.Markdown != "" {
			l.page = &webPage{URL: p.URL, Title: p.Title, Markdown: p.Markdown, markdown: true}
		}
		links = append(links, l)
	}
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	pages, by, fails := s.fetch(cctx, hostRoot(root.u), links)
	tr.cached, tr.site, tr.fetched, tr.failed = true, root.String(), len(pages), fails.n()
	if len(pages) != len(r.Pages) {
		return Outcome{}, false // a page no longer reads as it did: search again
	}
	p := &sfPages{root: root, title: r.Title, name: r.SiteName, widened: r.Widened, skipped: r.Skipped}
	for i, rp := range r.Pages {
		if rp.Wide {
			p.wide, p.wideBy = append(p.wide, pages[i]), append(p.wideBy, by[i])
			continue
		}
		p.exact, p.exactBy = append(p.exact, pages[i]), append(p.exactBy, by[i])
		p.reads = append(p.reads, PageRead{URL: pages[i].URL, Rank: rp.Rank, Via: rp.Via})
	}
	o := s.answerRead(ctx, p, req, sfOrigin{text: r.Origin}, nil)
	o = withTried(dressed(o, req, r.Others), r.Tried)
	return o, kept(o)
}

// answered reports an outcome with results to print.
func answered(o Outcome) bool { return o.Status == Found || o.Status == Partial }

// kept reports an outcome the question cache keeps the read of, and a
// repeat within cache_ttl gives again without searching: an answer, or
// the evidence pipeline's refusal (none of the question's distinctive
// terms in the docs, or nothing in them matched).
func kept(o Outcome) bool {
	err := o.Err()
	return answered(o) || o.Status == Refused && (errors.Is(err, ErrTermsNotFound) || errors.Is(err, ErrNoResults))
}

// indexedOver is the indexed library (one `docs index` built, in the
// current format) whose docs root holds root, the narrowest of several.
func (s *SearchFirst) indexedOver(root webRoot) (docsindex.Library, bool) {
	libs, err := s.local.libraries()
	if err != nil {
		return docsindex.Library{}, false
	}
	var best *docsindex.Library
	for i, l := range libs {
		r, err := parseWebRoot(l.Root)
		if err != nil || !l.Current() || !r.contains(root.u) {
			continue
		}
		if best == nil || len(l.Root) > len(best.Root) {
			best = &libs[i]
		}
	}
	if best == nil {
		return docsindex.Library{}, false
	}
	return *best, true
}
