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

// answerRead answers req from pages read under root, held in memory,
// with the note that names the pages read and the command indexing the
// docs the answer cites (indexRoot; wide: the directories too large for
// one index), and the origin siteOrigin gave the site.
func (s *SearchFirst) answerRead(ctx context.Context, root webRoot, title, siteName string, pages []webPage, req sfRequest, origin sfOrigin, wide []string) Outcome {
	l, lib, err := readLibrary(root, title, siteName, pages)
	if err != nil {
		return Answer(SearchBackend, nil, err)
	}
	defer l.store.Close()
	opts := req.opts
	// The budget is planned with the note naming root; the note printed
	// names what the answer cites, a few tokens either way.
	opts.note, opts.read, opts.origin = searchNote(root, len(pages), root, true), len(pages), origin.text
	opts.scope = readScope(root, req.library)
	o := l.answer(ctx, lib, req.query, opts)
	var cited []string
	for _, r := range o.Results {
		cited = append(cited, r.URL)
	}
	idx, ok := indexRoot(root, cited, wide)
	return searchAnswered(o, searchNote(root, len(pages), idx, ok), origin)
}

// indexRoot is the docs root the note of an answer from pages read under
// root suggests indexing: root, unless it is too wide for one index (its
// site's own lists name more pages under it than an index holds: wide, or
// it is a package index's whole host). Then it is the deepest directory
// holding every page the answer cites, the project's root on a host that
// serves many (sharedProjectRoot), when that is narrower, no site's root
// (a host's or its locale's) and not too wide itself; else root when root
// is a docs tree, and none (false) when root is a site's root.
func indexRoot(root webRoot, cited []string, wide []string) (webRoot, bool) {
	tooWide := func(r webRoot) bool {
		return slices.Contains(wide, r.String()) || r.prefix == "/" && indexHosted(r.u)
	}
	if !tooWide(root) {
		return root, true
	}
	common := commonDir(root, cited)
	if narrow, err := parseWebRoot(root.origin() + segmentsPath(common)); err == nil {
		if pr, ok := sharedProjectRoot(narrow.u); ok && pr.contains(narrow.u) {
			narrow = pr
		}
		if narrow.String() != root.String() && !siteRoot(narrow) && !tooWide(narrow) {
			return narrow, true
		}
	}
	if siteRoot(root) {
		return webRoot{}, false
	}
	return root, true
}

// commonDir is the path segments of the deepest directory holding every
// page of cited under root, a page's directory being the one above it
// (a page served as x/ is in its parent, not a tree of its own).
func commonDir(root webRoot, cited []string) []string {
	var common []string
	first := true
	for _, c := range cited {
		u, err := url.Parse(c)
		if err != nil || !root.contains(u) {
			continue
		}
		dir := pathSegments(u.Path)
		if len(dir) > 0 {
			dir = dir[:len(dir)-1]
		}
		if first {
			common, first = dir, false
			continue
		}
		n := 0
		for n < len(common) && n < len(dir) && common[n] == dir[n] {
			n++
		}
		common = common[:n]
	}
	return common
}

// siteRoot reports a root that is a host's root or its locale's (/en-us/):
// a whole site, as far as its path says.
func siteRoot(r webRoot) bool {
	segs := pathSegments(r.prefix)
	return len(segs) == 0 || len(segs) == 1 && localeSegRe.MatchString(segs[0])
}

// readScope is the scope of an answer from pages read under root, for a
// request whose library is a docs URL below the docs tree it is on (a
// page or a section of a site): root, the URL as the site read, whose
// pages are all the answer was made from. "" for any other request: the
// root read is then the docs tree itself.
func readScope(root webRoot, library string) string {
	if !isHTTPURL(library) {
		return ""
	}
	if tree := docsTree(root.u); tree.String() != root.String() && tree.contains(root.u) {
		return root.String()
	}
	return ""
}

// readTitle is the title and name of the site at root, from its top
// hit's profile when it was read.
func readTitle(root webRoot, prof *siteProfile) (title, name string) {
	if prof != nil {
		name = prof.meta["og:site_name"]
	}
	return firstNonEmpty(name, scopeHost(root.u)), name
}

// sfReadVersion versions the question cache's key and value.
const sfReadVersion = "docs-question/v4"

// sfRead is what the question cache keeps of a read: the docs root and,
// unless an indexed library answered for it, the pages read in order.
type sfRead struct {
	Root        string       `json:"root"`
	Title       string       `json:"title,omitempty"`
	SiteName    string       `json:"site_name,omitempty"`
	Origin      string       `json:"origin,omitempty"`
	OriginRoots []string     `json:"origin_roots,omitempty"`
	Pages       []sfReadPage `json:"pages,omitempty"`
	// Wide are the directories the site's own lists named too many pages
	// under for one index (wideDirs, indexRoot).
	Wide []string `json:"wide,omitempty"`
}

// sfReadPage is one page read: the link it was read by, the address the
// answer cites it at, and for a page the page cache does not hold (its
// text came from llms-full.txt or a Markdown source) its text.
type sfReadPage struct {
	Link     string `json:"link"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Markdown string `json:"md,omitempty"`
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

// readOf is the read to keep of pages read under root (each read by the
// link at the same place in by).
func readOf(root webRoot, title, siteName string, pages []webPage, by []webLink) sfRead {
	r := sfRead{Root: root.String(), Title: title, SiteName: siteName}
	for i, p := range pages {
		rp := sfReadPage{Link: by[i].url.String(), Title: by[i].title, URL: p.URL}
		if p.markdown {
			rp.Title, rp.Markdown = p.Title, p.Markdown
		}
		r.Pages = append(r.Pages, rp)
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
// answered or refused: from the indexed library that holds its docs root,
// else from the same pages, read again through the page cache (no web
// search, no site pick, no discovery), the source line saying cached. It
// reports false when there is no read to repeat or it no longer gives an
// outcome the cache keeps (kept), and the question is searched.
func (s *SearchFirst) fromRead(ctx context.Context, req sfRequest, tr *sfTrace) (Outcome, bool) {
	r, root, ok := s.loadRead(req)
	if !ok {
		return Outcome{}, false
	}
	if lib, ok := s.indexedOver(root); ok {
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
		if err != nil || !root.contains(u) {
			continue
		}
		l := webLink{url: u, title: p.Title}
		if p.Markdown != "" {
			l.page = &webPage{URL: p.URL, Title: p.Title, Markdown: p.Markdown, markdown: true}
		}
		links = append(links, l)
	}
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	pages, _, fails := s.fetch(cctx, root, links)
	tr.cached, tr.site, tr.fetched, tr.failed = true, root.String(), len(pages), fails.n()
	if len(pages) == 0 {
		return Outcome{}, false
	}
	req.opts.cached = true
	o := s.answerRead(ctx, root, r.Title, r.SiteName, pages, req, sfOrigin{text: r.Origin, roots: r.OriginRoots}, r.Wide)
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
