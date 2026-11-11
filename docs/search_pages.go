package docs

import (
	"context"
	"net/url"
	"path"
	"strings"
	"sync"
)

// The pages a search-first answer reads: the site's search hits, then
// close siblings from the site's own lists (its llms.txt, sitemap or root
// page links, discovered within sfDiscoverBudget, and the top hit's nav),
// the ones whose titles and paths name the question first, then those
// beside a hit in the nav; at most maxPages, robots.txt honoured, other
// releases' copies of a page left out. A page is told apart as the index
// planner tells it (pageIdentity: x, x/, x.html, x.md, x/index and an llms/
// mirror are one page), so it is read once, at the address listed first.

// plan lists the pages to read, hits first.
// It reports how many pages robots.txt kept it from.
func (s *SearchFirst) plan(ctx context.Context, site sfSite, prof *siteProfile, query string) ([]webLink, int) {
	root := site.root
	robots := s.web.robots(ctx, root)
	rootLabel := rootVersionLabel(root)
	var out []webLink
	seen := map[string]bool{}
	disallowed := 0
	add := func(l webLink) bool {
		k := pageIdentity(l.url, root, true)
		if seen[k] || !root.contains(l.url) || !isResultLink(l.url) {
			return false
		}
		if !robots.allowed(l.url) {
			seen[k] = true
			disallowed++
			return false
		}
		if _, pref := variantKey(l.url, root, rootLabel); pref.class > 2 && rootLabel == "" {
			return false // another release's copy of the page
		}
		seen[k] = true
		out = append(out, l)
		return true
	}
	for _, h := range site.hits {
		if len(out) >= s.maxHits {
			break
		}
		add(webLink{url: h.u, title: h.title, description: h.desc})
	}
	dctx, cancel := context.WithTimeout(ctx, sfDiscoverBudget)
	disc, _ := s.web.discover(dctx, root)
	cancel()
	nav := profileLinks(prof, root)
	pool := mergeLinks(disc.links, nav)
	picked := pickCandidates(pool, query, root, s.maxPages)
	near := nearLinks(nav, out, sfNearPages)
	for i := 0; len(out) < s.maxPages && (i < len(picked) || i < len(near)); i++ {
		if i < len(picked) {
			add(picked[i])
		}
		if i < len(near) && len(out) < s.maxPages {
			add(near[i])
		}
	}
	return inHand(out, disc.pages, root), disallowed
}

// profileLinks are a page profile's links into root, as candidates.
func profileLinks(prof *siteProfile, root webRoot) []webLink {
	if prof == nil {
		return nil
	}
	var out []webLink
	for _, l := range prof.links {
		if root.contains(l.url) && isPageURL(l.url) {
			out = append(out, webLink{url: l.url, title: l.text})
		}
	}
	return out
}

// nearLinks are links in the same directory as one of the hits, in nav
// order, at most n.
func nearLinks(nav, hits []webLink, n int) []webLink {
	dirs := map[string]bool{}
	for _, h := range hits {
		dirs[scopeHost(h.url)+path.Dir(h.url.Path)] = true
	}
	var out []webLink
	for _, l := range nav {
		if len(out) >= n {
			break
		}
		if dirs[scopeHost(l.url)+path.Dir(l.url.Path)] {
			out = append(out, l)
		}
	}
	return out
}

// inHand gives each link the page llms-full.txt already holds for it.
func inHand(links []webLink, pages []webPage, root webRoot) []webLink {
	if len(pages) == 0 {
		return links
	}
	held := map[string]*webPage{}
	for i := range pages {
		if u, err := url.Parse(pages[i].URL); err == nil {
			if k := pageIdentity(u, root, true); held[k] == nil {
				held[k] = &pages[i]
			}
		}
	}
	for i, l := range links {
		if p, ok := held[pageIdentity(l.url, root, true)]; ok {
			links[i].page = p
		}
	}
	return links
}

// fetch reads the planned pages, sfConcurrency at a time, each within
// sfPageTimeout and all within ctx: the pages that arrived in plan order,
// one per page (pageIdentity) and none whose text another's is, each with
// the link it was read by, and how many failed.
func (s *SearchFirst) fetch(ctx context.Context, root webRoot, links []webLink) ([]webPage, []webLink, int) {
	pages := make([]*webPage, len(links))
	sem := make(chan struct{}, sfConcurrency)
	var wg sync.WaitGroup
	for i, l := range links {
		if l.page != nil {
			pages[i] = l.page
			continue
		}
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			pctx, cancel := context.WithTimeout(ctx, sfPageTimeout)
			defer cancel()
			if p, err := s.web.fetchOne(pctx, root, l); err == nil {
				pages[i] = p
			}
		})
	}
	wg.Wait()
	var out []webPage
	var by []webLink
	failed := 0
	seen := map[string]bool{}
	texts := map[[32]byte]bool{}
	for i, p := range pages {
		if p == nil || unreadShell(p) {
			failed++
			continue
		}
		k := p.URL
		if u, err := url.Parse(p.URL); err == nil {
			k = pageIdentity(u, root, true)
		}
		if t := contentKey(*p); !seen[k] && !texts[t] {
			seen[k], texts[t] = true, true
			out = append(out, *p)
			by = append(by, links[i])
		}
	}
	return out, by, failed
}

// sfShellWords is how many words an unrendered JS shell's text must have
// to be read as a page: below it the text is the shell's placeholder
// ("Loading"), not the page.
const sfShellWords = 50

// unreadShell reports a page that is an unrendered JS shell with next to
// no text: no page at all to a reader without a browser.
func unreadShell(p *webPage) bool {
	return p.shell && len(strings.Fields(p.Markdown)) < sfShellWords
}
