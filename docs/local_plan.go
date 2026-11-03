package docs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Planning a local index (ADR-0007): which of a docs root's pages to
// index. Discovery is the web backend's own, run in full rather than for
// one query: llms.txt (with llms-full.txt), then the sitemaps and the root
// page's nav whatever llms.txt listed (a hand-written llms.txt names a few
// pages), then the root page's links only when neither listed any, and
// the pages the site's search index names. The pages kept are those a
// result could be: in scope, not an asset or a source listing, one copy
// per page across versions and locales, allowed by robots.txt, at most a
// cap of them in discovery order.

// indexSite is what discovery found for an index.
type indexSite struct {
	site    webSite
	sources []string // which discovery sources listed pages
}

// enumerate discovers every page of root it can, cheapest source first.
func (w *Web) enumerate(ctx context.Context, root webRoot) (indexSite, error) {
	var s indexSite
	if w.discoverLLMS(ctx, root, &s.site) {
		s.sources = append(s.sources, "llms.txt")
		if len(s.site.pages) > 0 {
			s.sources = append(s.sources, "llms-full.txt")
		}
	}
	if w.discoverSitemap(ctx, root, &s.site) {
		s.sources = append(s.sources, "sitemap")
	}
	if len(s.site.links) == 0 && len(s.site.pages) == 0 {
		if err := w.discoverRootLinks(ctx, root, &s.site); err != nil {
			return s, err
		}
		if len(s.site.links) > 0 {
			s.sources = append(s.sources, "links")
		}
	}
	if len(s.site.index) > 0 {
		s.sources = append(s.sources, "search index")
	}
	if len(s.site.links) == 0 && len(s.site.pages) == 0 && len(s.site.index) == 0 {
		return s, fmt.Errorf("web: no docs pages under %s: %w (tried %s)", root, ErrNotFound, strings.Join(s.site.tried, ", "))
	}
	return s, nil
}

// indexPlan is the pages an index build fetches, in discovery order.
type indexPlan struct {
	links []webLink // a link with page set is already in hand (llms-full.txt)
	// listed is how many distinct pages discovery listed; variants how many
	// were other versions' or locales' copies; disallowed how many
	// robots.txt kept out; capped whether the cap left some out.
	listed, variants, disallowed int
	capped                       bool
}

// plan picks the pages to index from what discovery found.
func (w *Web) plan(ctx context.Context, root webRoot, s indexSite, maxPages int) indexPlan {
	links := uniquePages(candidateLinks(s.site, root))
	p := indexPlan{listed: len(links)}
	links = keepOneVariant(links, root)
	p.variants = p.listed - len(links)
	rules := w.robots(ctx, root)
	kept := links[:0:0]
	for _, l := range links {
		if rules.allowed(l.url) {
			kept = append(kept, l)
		}
	}
	p.disallowed = len(links) - len(kept)
	if maxPages > 0 && len(kept) > maxPages {
		kept, p.capped = kept[:maxPages], true
	}
	p.links = kept
	return p
}

// candidateLinks lists every page discovery named, pages in hand first:
// llms-full.txt's pages, then the listed links, then the pages the search
// index holds sections of. Only pages a result could be are kept.
func candidateLinks(site webSite, root webRoot) []webLink {
	var out []webLink
	add := func(l webLink) {
		if l.url != nil && root.contains(l.url) && isResultLink(l.url) && isPageURL(l.url) {
			out = append(out, l)
		}
	}
	for i := range site.pages {
		if u, err := url.Parse(site.pages[i].URL); err == nil {
			add(webLink{url: u, title: site.pages[i].Title, page: &site.pages[i]})
		}
	}
	for _, l := range site.links {
		add(l)
	}
	for _, s := range site.index {
		if u, err := url.Parse(s.pageURL); err == nil {
			add(webLink{url: u, title: s.crumbs[0]})
		}
	}
	return out
}

// uniquePages keeps one link per displayed page (x and x.md are one), the
// first listed, except that llms-full.txt's chunks of one page are joined
// into one page in hand.
func uniquePages(links []webLink) []webLink {
	at := map[string]int{}
	var out []webLink
	for _, l := range links {
		k := displayKey(l.url)
		i, seen := at[k]
		switch {
		case !seen:
			at[k] = len(out)
			if l.page != nil {
				page := *l.page
				l.page = &page
			}
			out = append(out, l)
		case l.page != nil && out[i].page != nil:
			out[i].page.Markdown += "\n\n" + l.page.Markdown
		case l.page != nil:
			page := *l.page
			out[i].page = &page
		}
	}
	return out
}

// keepOneVariant drops other versions' and locales' copies of the pages,
// generically by path: a page whose path, once a locale or version segment
// is set aside (variantParts), is another page's keeps only the preferred
// variant (English or no locale; the root's own version, else the current
// one), as collapseVariants does for the live path. A whole variant tree
// (/es/…, /v2/…) is then dropped when at least half of its pages were such
// copies: it is a translation or an old release of the docs, and its
// pages with no counterpart are pages the current docs no longer have. A
// tree with few counterparts is not a variant at all (an /api/v1/ path in
// a REST reference) and stays.
func keepOneVariant(links []webLink, root webRoot) []webLink {
	rootLabel := rootVersionLabel(root)
	type cand struct {
		link webLink
		key  string
		tree string
		pref variantPref
	}
	cands := make([]cand, len(links))
	trees := map[string]map[string]bool{} // key -> trees holding it
	for i, l := range links {
		key, tree, pref := variantParts(l.url, root, rootLabel)
		cands[i] = cand{link: l, key: key, tree: strings.Join(tree, "/"), pref: pref}
		if trees[key] == nil {
			trees[key] = map[string]bool{}
		}
		trees[key][cands[i].tree] = true
	}
	best := map[string]int{} // key -> index of its preferred variant
	total, copies := map[string]int{}, map[string]int{}
	for i, c := range cands {
		total[c.tree]++
		if len(trees[c.key]) > 1 {
			copies[c.tree]++
		}
		if j, ok := best[c.key]; !ok || c.pref.better(cands[j].pref) {
			best[c.key] = i
		}
	}
	preferred := ""
	if len(cands) > 0 {
		top := 0
		for i, c := range cands {
			if c.pref.better(cands[top].pref) {
				top = i
			}
		}
		preferred = cands[top].tree
	}
	var out []webLink
	for i, c := range cands {
		if best[c.key] != i {
			continue // another variant of this page is preferred
		}
		if c.tree != preferred && c.tree != "" && 2*copies[c.tree] >= total[c.tree] {
			continue // a translation or another release of the docs
		}
		out = append(out, c.link)
	}
	return out
}

// robotsRules are robots.txt's Allow and Disallow rules for ketch: the
// group naming ketch, else the group for every agent.
type robotsRules struct {
	allow, disallow []robotsRule
}

// robotsRule is one path pattern: a prefix, where * is any run of
// characters and a trailing $ anchors the end.
type robotsRule struct {
	pattern string
	re      *regexp.Regexp
}

func newRobotsRule(pattern string) robotsRule {
	anchored := strings.HasSuffix(pattern, "$")
	parts := strings.Split(strings.TrimSuffix(pattern, "$"), "*")
	for i, part := range parts {
		parts[i] = regexp.QuoteMeta(part)
	}
	expr := "^" + strings.Join(parts, ".*")
	if anchored {
		expr += "$"
	}
	return robotsRule{pattern: pattern, re: regexp.MustCompile(expr)}
}

// allowed reports whether a path may be fetched: the longest matching
// rule decides, Allow winning a tie; no rule allows.
func (r robotsRules) allowed(u *url.URL) bool {
	p := u.EscapedPath()
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	best, allow := -1, true
	for _, rule := range r.disallow {
		if len(rule.pattern) > best && rule.re.MatchString(p) {
			best, allow = len(rule.pattern), false
		}
	}
	for _, rule := range r.allow {
		if len(rule.pattern) >= best && rule.re.MatchString(p) {
			best, allow = len(rule.pattern), true
		}
	}
	return allow
}

// robots reads the root host's robots.txt (already read by the sitemap
// discovery, so served from the call's fetch memo). None, or one that
// cannot be read, allows everything.
func (w *Web) robots(ctx context.Context, root webRoot) robotsRules {
	body, _, ok := w.probe(ctx, root, root.origin()+"/robots.txt")
	if !ok {
		return robotsRules{}
	}
	return parseRobots(body, "ketch")
}

// parseRobots reads the rules of the group naming agent, else of the
// group for "*".
func parseRobots(body []byte, agent string) robotsRules {
	groups := map[string]*robotsRules{}
	var current []string // the user agents the lines apply to
	inRules := false
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if inRules {
				current, inRules = nil, false
			}
			current = append(current, strings.ToLower(value))
		case "allow", "disallow":
			inRules = true
			if value == "" {
				continue // an empty Disallow allows everything
			}
			for _, a := range current {
				if groups[a] == nil {
					groups[a] = &robotsRules{}
				}
				if key == "allow" {
					groups[a].allow = append(groups[a].allow, newRobotsRule(value))
				} else {
					groups[a].disallow = append(groups[a].disallow, newRobotsRule(value))
				}
			}
		}
	}
	if g := groups[agent]; g != nil {
		return *g
	}
	if g := groups["*"]; g != nil {
		return *g
	}
	return robotsRules{}
}
