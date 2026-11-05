package docs

import (
	"context"
	"errors"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// Posts are not docs (task-27). A project's site keeps its blog, news,
// announcements, showcase and community posts beside its docs, often in
// the same sitemap and under the same generator; a post about a feature
// names the query's words more often than the page documenting it, and
// answered for it. A docs root therefore never contains a post section
// below it (webRoot.contains, which discovery, ranking, a web search's
// pages and the local index all go through), and a root that lands on a
// site's home page moves to the documentation section the page leads to
// (docsSection).

// postSections are path segments that hold posts rather than docs, as
// directories: /blog/2021/hello, /news/release-3, /community/meetup.
var postSections = map[string]bool{
	"blog": true, "blogs": true, "weblog": true, "news": true, "posts": true,
	"announcements": true, "showcase": true, "community": true,
}

// isPostPath reports rel, a path below a docs root, as a post or a post
// section: a postSections segment as a directory among its leading
// segments (after any locale or version segments: /en/blog/hello), or, at
// the very top, as a page (/blog, /community); or a dated post anywhere
// (/2019/08/slug: a year, a month, then the post). A page merely named
// like a section (/api/news.html, /guide/community.md) and a section
// deeper in the docs tree are docs.
func isPostPath(rel string) bool {
	segs := pathSegments(rel)
	dir := strings.HasSuffix(rel, "/")
	lead := true
	for i, seg := range segs {
		low := strings.ToLower(seg)
		isDir := i < len(segs)-1 || dir
		if lead && postSections[low] && (isDir || i == 0 && path.Ext(low) == "") {
			return true
		}
		if i+2 < len(segs) && isPostYear(seg) && isPostMonth(segs[i+1]) {
			return true
		}
		if _, ok := variantLabel(seg); ok && isDir {
			continue
		}
		if localeRe.MatchString(seg) && isDir {
			continue
		}
		lead = false
	}
	return false
}

// isPostYear reports a four-digit year a dated post path starts with.
func isPostYear(seg string) bool {
	n, err := strconv.Atoi(seg)
	return err == nil && len(seg) == 4 && n >= 1990 && n <= 2099
}

// isPostMonth reports a month segment of a dated post path: 1 to 12, one
// or two digits.
func isPostMonth(seg string) bool {
	n, err := strconv.Atoi(seg)
	return err == nil && len(seg) <= 2 && n >= 1 && n <= 12
}

// docsSection is the documentation section a candidate's root at a
// site's home page stands for, when the page leads to one on its own
// registrable domain (docsRootsFor: a landing page's Docs link into
// /docs/, a docs. host) and is not docs itself: the section's root, its
// profile and the candidate's evidence as the section's (movedEvidence).
// Otherwise root, prof and the evidence as they were, as for registry
// docs. A home page is the front door of everything its owner publishes
// (a blog, news, a showcase); its docs are where its docs nav leads. A
// section that does not answer from within itself is not taken.
func (w *Web) docsSection(ctx context.Context, root webRoot, prof *siteProfile, c rootCandidate) (webRoot, *siteProfile, []evidence) {
	if c.registry || root.prefix != "/" || prof == nil || indexHosted(root.u) {
		return root, prof, c.evidence
	}
	roots := docsRootsFor(root.u, prof)
	if len(roots) == 0 || sameRoot(roots[0], root.u) {
		return root, prof, c.evidence // the home page is docs itself, or leads nowhere else
	}
	domain := registrableDomain(root.u.Hostname())
	for _, u := range roots {
		if isRegistryHost(u) || registrableDomain(u.Hostname()) != domain || sameRoot(u, root.u) {
			continue
		}
		if next, np, ok := w.sectionAt(ctx, u, prof); ok {
			return next, np, movedEvidence(c.evidence, root)
		}
	}
	return root, prof, c.evidence
}

// sectionAt reads the docs section at u that home's page links to: its
// own page, or, when it has none, the page under it the docs nav leads to
// (navPage), served from within it.
func (w *Web) sectionAt(ctx context.Context, u *url.URL, home *siteProfile) (webRoot, *siteProfile, bool) {
	next, err := parseWebRoot(u.String())
	if err != nil {
		return webRoot{}, nil, false
	}
	np, err := w.cachedProfile(ctx, next.u)
	if page := navPage(home, next.u); err != nil && errors.Is(err, ErrNotFound) && page != nil {
		np, err = w.cachedProfile(ctx, page)
	}
	if err != nil || np.served == nil || !next.contains(np.served) {
		return webRoot{}, nil, false
	}
	return withOrigin(next, np.served), np, true
}

// movedEvidence is a candidate's evidence for the docs section its root
// moved to (docsSection): what named the home page names the section
// through the home page's docs link, a nav chain.
func movedEvidence(evs []evidence, home webRoot) []evidence {
	out := make([]evidence, 0, len(evs))
	for _, e := range evs {
		switch e.kind {
		case evPkgDocs, evPkgHomepage, evRepoHomepage:
			e = evidence{kind: evNavChain, detail: e.detail + "; its home page " + scopeHost(home.u) + " links to it"}
		}
		out = append(out, e)
	}
	return out
}

// pathBelow is u's path below root's prefix.
func pathBelow(root webRoot, u *url.URL) string {
	if root.prefix == "/" {
		return u.Path
	}
	return strings.TrimPrefix(u.Path, root.prefix)
}
