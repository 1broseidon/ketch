package docs

import (
	"net/url"
	"slices"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/failure"
)

// Scope (design/docs-evidence.md, Round 10): library is a docs root as
// the index lists it; a library URL below an indexed root narrows the
// answer to the pages at or under that URL, a page URL to that page. The
// pool, the pieces and defined_in come from those pages only; the terms'
// counts, and so terms and not_found, stay the library's (not_found
// means nowhere in the library), and outside_scope names the distinctive
// terms the library holds but no section under the URL does. A URL with
// no indexed page under it is refused, never widened.

// Reasons a scoped answer gives.
const (
	// ReasonScopeEmpty: no indexed page is at or under the scope URL.
	ReasonScopeEmpty = "scope_empty"
	// ReasonTermsOutsideScope: a distinctive term the library holds is in
	// no section under the scope URL (refused when none is).
	ReasonTermsOutsideScope = "terms_outside_scope"
)

var (
	// ErrScopeEmpty is a library URL below an indexed root that no indexed
	// page is at or under.
	ErrScopeEmpty = failure.New(failure.NotFound, ReasonScopeEmpty, "no indexed page is under the library URL")
	// ErrTermsOutsideScope is a scoped question whose distinctive terms
	// the library holds only outside the scope.
	ErrTermsOutsideScope = failure.New(failure.NotFound, ReasonTermsOutsideScope, "the question's distinctive terms are only outside the library URL's pages")
)

// scopeOf is the scope a library value gives lib: the value (its
// fragment dropped) when it is a docs URL below lib's root or the address
// lib is shown under, with its scheme or without; "" for the root itself,
// a name or a spec.
func scopeOf(lib docsindex.Library, value string) string {
	value = strings.TrimSpace(value)
	if !isHTTPURL(value) {
		u, ok := bareURL(value) // a docs URL without its scheme
		if !ok {
			return ""
		}
		value = u
	}
	page, _, _ := strings.Cut(value, "#")
	u, err := url.Parse(page)
	if err != nil {
		return ""
	}
	for _, raw := range []string{lib.Root, lib.DisplayRoot} {
		r, err := parseWebRoot(raw)
		if raw == "" || err != nil {
			continue
		}
		if under(scopePath(u, r), scopePath(r.u, r)) {
			return page
		}
	}
	return ""
}

// scopePath is a page as scopes compare them: its identity as one page
// of the library at root (pageIdentity: host without www., no scheme,
// trailing slash, .html, .md or /index), without its query.
func scopePath(u *url.URL, root webRoot) string {
	p, _, _ := strings.Cut(pageIdentity(u, root, true), "?")
	return p
}

// under reports a path strictly below dir, at a segment boundary.
func under(path, dir string) bool {
	return strings.HasPrefix(path, dir+"/")
}

// atOrUnder reports a path that is dir or below it.
func atOrUnder(path, dir string) bool {
	return path == dir || under(path, dir)
}

// scopeSections is the scope of lib's sections the URL scope names: the
// sections of every stored page at or under it (a URL under the address
// lib is shown at is read under its root). None when no page is.
func (l *Local) scopeSections(lib docsindex.Library, scope string) (docsindex.Scope, error) {
	if rest, ok := strings.CutPrefix(scope, lib.DisplayRoot); ok && lib.DisplayRoot != "" {
		scope = lib.Root + rest
	}
	root, err := parseWebRoot(lib.Root)
	su, uerr := url.Parse(scope)
	if err != nil || uerr != nil || l.store == nil {
		return docsindex.Scope{}, nil
	}
	want := scopePath(su, root)
	stored, err := l.store.Pages(lib.Key)
	if err != nil {
		return docsindex.Scope{}, err
	}
	var pages []string
	for p := range stored {
		if u, err := url.Parse(p); err == nil && atOrUnder(scopePath(u, root), want) {
			pages = append(pages, p)
		}
	}
	slices.Sort(pages)
	secs, err := l.store.PageOutlines(lib.Key, pages)
	if err != nil {
		return docsindex.Scope{}, err
	}
	ids := make([]int64, len(secs))
	for i, s := range secs {
		ids[i] = s.ID
	}
	return docsindex.Scope{IDs: ids}, nil
}

// outsideScope is what the written terms say of a scope: the distinctive
// terms the library holds that no section under it does, and whether
// any distinctive term is under it.
func outsideScope(wts []writtenTerm) (outside []string, hit bool) {
	for _, wt := range wts {
		switch {
		case !wt.distinctive || wt.sections == 0:
		case wt.inScope > 0:
			hit = true
		default:
			outside = append(outside, wt.text)
		}
	}
	return outside, hit
}
