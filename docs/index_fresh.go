package docs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// An index checks what it cites (design/docs-evidence.md, Storage). Before
// an answer from an indexed library prints, every page it cites (the
// pages of its groups) last read longer ago than cache_ttl is read again,
// all at once, within freshCheckTimeout in all. A page whose content is
// unchanged (its hash) only has its fetch time updated; one that changed
// is sectioned again in the index, and the answer is ranked once more.
// When any could not be read (no network, a timeout, the page gone), the
// answer prints as it is, marked stale ("unverified, built <date>").
// --offline (LocalOptions.Offline) reads nothing and marks the answer stale
// when a cited page is older than the TTL. Pages younger than the TTL are
// never read. A --section read from the index follows the same rule for
// its one page.
//
// A whole refresh stays `ketch docs index` again: pages added to a site
// since the build are not seen until then, and the build date says how
// old the index is.

// freshCheckTimeout bounds the whole check of one answer's cited pages.
const freshCheckTimeout = 3 * time.Second

// answerIndexed answers query from lib, an indexed library, once its
// cited pages are checked (verify), with note (other indexed versions).
func (s *SearchFirst) answerIndexed(ctx context.Context, lib docsindex.Library, query string, opts LocalOptions, note string) Outcome {
	rank := func() Outcome { return s.local.answer(ctx, lib, query, opts) }
	o := rank()
	o = s.verify(ctx, lib, o, citedPages(lib, o.Results), rank)
	return withVersionNote(o, note)
}

// citedPages are the stored addresses of the pages results cite, each
// once, in order.
func citedPages(lib docsindex.Library, results []Result) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range results {
		page := pageOf(r)
		if lib.DisplayRoot != "" {
			if rest, ok := strings.CutPrefix(page, lib.DisplayRoot); ok {
				page = lib.Root + rest
			}
		}
		if !seen[page] {
			seen[page] = true
			out = append(out, page)
		}
	}
	return out
}

// verify checks the pages o (an answer from lib) cites: see the top of
// this file. again answers once more, after a cited page changed.
func (s *SearchFirst) verify(ctx context.Context, lib docsindex.Library, o Outcome, pages []string, again func() Outcome) Outcome {
	if !answered(o) || s.ttl <= 0 || s.local.store == nil || len(pages) == 0 {
		return o
	}
	stale, err := s.stalePages(lib, pages)
	if err != nil || len(stale) == 0 {
		return o
	}
	if opts, _ := localOptionsFrom(ctx); opts.Offline {
		o.Stale = staleNote(lib)
		return o
	}
	changed, failed := s.recheck(ctx, lib, stale)
	if changed {
		o = again()
	}
	if failed {
		o.Stale = staleNote(lib)
	}
	return o
}

// staleNote is an unverified answer's stale line: "unverified, built
// 2026-11-10".
func staleNote(lib docsindex.Library) string {
	if lib.Built.IsZero() {
		return "unverified"
	}
	return "unverified, built " + lib.Built.UTC().Format("2006-01-02")
}

// stalePages are the stored records of pages last read longer ago than
// the TTL (or at no recorded time).
func (s *SearchFirst) stalePages(lib docsindex.Library, pages []string) ([]docsindex.Page, error) {
	stored, err := s.local.store.PagesOf(lib.Key, pages)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var out []docsindex.Page
	for _, u := range pages {
		if p, ok := stored[u]; ok && (p.Fetched.IsZero() || now.Sub(p.Fetched) > s.ttl) {
			out = append(out, p)
		}
	}
	return out, nil
}

// recheck reads the stale pages again, all at once, within
// s.checkTimeout: an unchanged page has its fetch time updated, a changed
// one is sectioned again in lib's index. It reports whether any changed
// and whether any could not be read or stored.
func (s *SearchFirst) recheck(ctx context.Context, lib docsindex.Library, stale []docsindex.Page) (changed, failed bool) {
	root, err := parseWebRoot(lib.Root)
	if err != nil {
		return false, true
	}
	cctx, cancel := context.WithTimeout(ctx, s.checkTimeout)
	defer cancel()
	got := make([]reread, len(stale))
	var wg sync.WaitGroup
	for i, p := range stale {
		wg.Go(func() { got[i] = s.reread(cctx, root, p) })
	}
	wg.Wait()
	now := s.now().UTC()
	b := &indexBuild{store: s.local.store, key: lib.Key, version: lib.Version, title: lib.Title, at: now}
	var same []string
	var todo []docsindex.PageData
	delta := 0
	for i, r := range got {
		switch {
		case r.page == nil:
			failed = true
		case r.same:
			same = append(same, stale[i].URL)
		default:
			pd := b.pageData(*r.page, pageHash(*r.page), adoptLinkedAnchors(splitSections(*r.page, 0)))
			delta += pd.Page.Sections - stale[i].Sections
			todo = append(todo, pd)
		}
	}
	if err := s.local.store.TouchPages(lib.Key, same, now); err != nil {
		failed = true
	}
	if len(todo) == 0 {
		return false, failed
	}
	if err := s.storeChanged(lib, todo, delta); err != nil {
		return false, true
	}
	return true, failed
}

// reread is a stored page read again: its text at the address the index
// holds it under (nil when it could not be read), and whether that is the
// text stored.
type reread struct {
	page *webPage
	same bool
}

// reread reads a stored page again, as a build reads it: through the page
// pipeline, and when that text is not the stored one, from its Markdown
// variant (x.md), which a build reads instead for a site whose llms.txt
// lists it, or whose llms-full.txt the stored text came from. A page is
// unchanged when either text is the stored one; changed, it takes the
// Markdown variant's text when there is one.
func (s *SearchFirst) reread(ctx context.Context, root webRoot, p docsindex.Page) reread {
	u, err := url.Parse(p.URL)
	if err != nil {
		return reread{}
	}
	read := func(u *url.URL) *webPage {
		wp, err := s.web.fetchOne(ctx, root, webLink{url: u, title: p.Title})
		if err != nil || unreadShell(wp) {
			return nil
		}
		page := *wp
		page.URL = p.URL // the address the index holds it under
		return &page
	}
	page := read(u)
	if page != nil && pageHash(*page) == p.Hash {
		return reread{page: page, same: true}
	}
	if mu, ok := markdownVariant(u); ok {
		if md := read(mu); md != nil && md.markdown {
			return reread{page: md, same: pageHash(*md) == p.Hash}
		}
	}
	return reread{page: page}
}

// markdownVariant is the Markdown source llms.txt generators publish
// beside a page (x.md, a directory's index.md), for a page address with no
// extension.
func markdownVariant(u *url.URL) (*url.URL, bool) {
	if path.Ext(u.Path) != "" {
		return nil, false
	}
	v := *u
	v.RawPath, v.RawQuery, v.Fragment = "", "", ""
	if strings.HasSuffix(v.Path, "/") {
		v.Path += "index.md"
	} else {
		v.Path += ".md"
	}
	return &v, true
}

// storeChanged writes pages that changed since lib was built, and lib's
// section count with them; its build date stays.
func (s *SearchFirst) storeChanged(lib docsindex.Library, pages []docsindex.PageData, delta int) error {
	if err := putPageBatches(s.local.store, lib.Key, pages); err != nil {
		return err
	}
	stored, found, err := s.local.store.Library(lib.Key)
	if err != nil || !found {
		return err
	}
	stored.Sections = max(stored.Sections+delta, 0)
	return s.local.store.PutLibrary(stored)
}

// SectionSelect reads one section (see Local.SectionSelect): from the
// index, its page checked as an answer's cited pages are (verify); else,
// for a page no indexed library holds (the pages a question read), from
// the page itself, through the page cache (readSection).
func (s *SearchFirst) SectionSelect(ctx context.Context, library, ref string, tokens, page int) Outcome {
	read := func() (Outcome, docsindex.Library, string) {
		return s.local.sectionRead(ctx, library, ref, tokens, page)
	}
	o, lib, stored := read()
	if lib.Key != "" {
		return s.verify(ctx, lib, o, []string{stored}, func() Outcome { o, _, _ := read(); return o })
	}
	if err := o.Err(); errors.Is(err, ErrSectionNotFound) || errors.Is(err, ErrNotIndexed) {
		return s.readSection(ctx, library, ref, tokens, page)
	}
	return o
}

// readSection reads a section of a page no indexed library holds, as a
// question read it (pageRead): the page through the page cache (read
// again from the site when its entry expired), or the text the question
// had in hand, sectioned on the fly. A page that cannot be read (no
// network and no cache entry) fails with the reason.
func (s *SearchFirst) readSection(ctx context.Context, library, ref string, tokens, pageNo int) Outcome {
	page, anchor, err := parseSectionRef(ref)
	if err != nil {
		return Answer(SearchBackend, nil, err)
	}
	u, err := url.Parse(page)
	if err != nil {
		return Answer(SearchBackend, nil, fmt.Errorf("search: %q is not a section reference: %w", ref, errSectionRef))
	}
	root := docsTree(u)
	link := s.pageRead(u)
	wp := link.page
	if wp == nil {
		cctx, cancel := context.WithTimeout(ctx, sfPageTimeout)
		defer cancel()
		if wp, err = s.web.fetchOne(cctx, root, link); err != nil {
			return Answer(SearchBackend, nil, fmt.Errorf("search: %s is in no indexed library, and reading it failed: %w", page, err))
		}
	}
	b := &indexBuild{key: root.String()}
	secs := b.pageData(*wp, "", adoptLinkedAnchors(splitSections(*wp, 0))).Sections
	if len(secs) == 0 {
		return Answer(SearchBackend, nil, fmt.Errorf("search: %s has no sections: %w", page, ErrSectionNotFound))
	}
	opts, _ := localOptionsFrom(ctx)
	lib := docsindex.Library{Key: root.String(), Root: root.String()}
	return fetchSection(lib, secs, anchor, library, tokens, pageNo, opts.Hint)
}
