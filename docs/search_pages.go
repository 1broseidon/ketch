package docs

import (
	"context"
	"net/url"
	"path"
	"sync"
	"time"
)

// The pages a search-first answer reads: the site's search hits, then
// close siblings from the site's own lists (its llms.txt, sitemap or root
// page links, discovered within sfDiscoverBudget, and the top hit's nav),
// the ones whose titles and paths name the question first, then those
// beside a hit in the nav; at most maxPages, robots.txt honoured, other
// releases' copies of a page left out.

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
		k := displayKey(l.url)
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
	return inHand(out, disc.pages), disallowed
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
func inHand(links []webLink, pages []webPage) []webLink {
	if len(pages) == 0 {
		return links
	}
	held := map[string]*webPage{}
	for i := range pages {
		if u, err := url.Parse(pages[i].URL); err == nil {
			if _, ok := held[displayKey(u)]; !ok {
				held[displayKey(u)] = &pages[i]
			}
		}
	}
	for i, l := range links {
		if p, ok := held[displayKey(l.url)]; ok {
			links[i].page = p
		}
	}
	return links
}

// fetch reads the planned pages, sfConcurrency at a time, each within
// sfPageTimeout and all within ctx; the pages that arrived in plan order
// (copies of another's text dropped) and how many failed.
func (s *SearchFirst) fetch(ctx context.Context, root webRoot, links []webLink) ([]webPage, int) {
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
	failed := 0
	seen := map[string]bool{}
	for _, p := range pages {
		switch {
		case p == nil:
			failed++
		case !seen[p.URL]:
			seen[p.URL] = true
			out = append(out, *p)
		}
	}
	out, _ = uniqueContent(out)
	return out, failed
}

// sfStored is what one search-first call added to the index.
type sfStored struct {
	newPages, changed, unchanged, embedded int
	embedTime                              time.Duration
}
