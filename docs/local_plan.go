package docs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Planning a local index (ADR-0007): which of a docs root's pages to
// index. Discovery is the web backend's own, run in full rather than for
// one query: llms.txt (with llms-full.txt), then the sitemaps and the root
// page's nav whatever llms.txt listed (a hand-written llms.txt names a few
// pages), then the root page's links only when neither listed any, and
// the pages the site's search index names. The pages kept are those a
// result could be: in scope, not an asset or a source listing, in the
// indexed version's tree alone and one copy per page across locales (and
// across a page's .html twin or llms.txt text mirror), allowed by
// robots.txt, at most a cap of them in priority order (planTier: the
// pages llms.txt or a page seeder names, then guides, then the rest, API
// reference leaves and release notes last).

// PageSeeder names pages the web points to for a library, best first: a
// web search's hits for what the library's docs teach. A build indexes the
// seeded pages under its root first (after the robots, version and locale
// checks every page passes). Seeds must come from library-level questions
// (the library's guides, tutorials, examples), never from an evaluation's.
type PageSeeder interface {
	Seeds(ctx context.Context, lib SeedLibrary) ([]SeedPage, error)
}

// SeedLibrary is the library a seeder is asked about.
type SeedLibrary struct {
	Name, Title, Root, Version string
	Packages                   []string
}

// SeedPage is a page a seeder names, with its weight (0 to 1) and where it
// came from (a search provider's name).
type SeedPage struct {
	URL    string
	Weight float64
	Source string
}

// seedPages asks the seeder for the target's pages; none without a seeder
// or when it fails (seeds only order a build, so a failure says so and the
// build goes on).
func (w *Web) seedPages(ctx context.Context, t indexTarget, seeder PageSeeder, say func(string, ...any)) []webLink {
	if seeder == nil {
		return nil
	}
	name := t.spec.name
	if name == "" {
		name = t.title
	}
	got, err := seeder.Seeds(ctx, SeedLibrary{Name: name, Title: t.title, Root: t.root.String(), Version: t.version, Packages: t.packages})
	if err != nil {
		say("page seeds: %v (indexing without them)", err)
		return nil
	}
	var out []webLink
	for _, sp := range got {
		if u, err := url.Parse(sp.URL); err == nil && u.Host != "" {
			out = append(out, webLink{url: u})
		}
	}
	return out
}

// indexSite is what discovery found for an index.
type indexSite struct {
	site    webSite
	sources []string // which discovery sources listed pages
	// curated are the pages llms.txt (or llms-full.txt) listed, by
	// displayKey: a site's own pick, indexed first.
	curated map[string]bool
}

// enumerate discovers every page of root it can, cheapest source first.
func (w *Web) enumerate(ctx context.Context, root webRoot) (indexSite, error) {
	var s indexSite
	if w.discoverLLMS(ctx, root, &s.site) {
		s.curated = curatedKeys(s.site)
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
	// were other versions' or locales' copies, otherVersions how many of
	// those were other versions'; disallowed how many robots.txt kept out;
	// capped whether the cap left some out.
	listed, variants, otherVersions, disallowed int
	capped                                      bool
	// tiers counts the pages kept by planTier; seeds the seeded pages kept.
	tiers [planTiers]int
	seeds int
	// curated are the pages llms.txt and the seeder named, by displayKey.
	curated map[string]bool
	// dropped counts the pages left out in each top-level version tree
	// below the root (versionTrees), for re-rooting a root whose pages all
	// sit in versioned trees.
	dropped map[string]int
}

// plan picks the pages to index from what discovery found; others are
// the labels of the site's other versions (otherVersions) and seeds pages
// a page seeder named, which lead the plan.
func (w *Web) plan(ctx context.Context, root webRoot, s indexSite, maxPages int, others map[string]bool, seeds []webLink) indexPlan {
	links := uniquePages(append(seedLinks(seeds, root), candidateLinks(s.site, root)...))
	links = dropMirrors(links, root)
	p := indexPlan{listed: len(links)}
	links, p.otherVersions, p.dropped = keepOneVariant(links, root, others)
	p.variants = p.listed - len(links)
	rules := w.robots(ctx, root)
	kept := links[:0:0]
	for _, l := range links {
		if rules.allowed(l.url) {
			kept = append(kept, l)
		}
	}
	p.disallowed = len(links) - len(kept)
	curated := maps.Clone(s.curated)
	if curated == nil {
		curated = map[string]bool{}
	}
	for _, l := range seeds {
		curated[displayKey(l.url)] = true
	}
	p.curated = curated
	kept = prioritize(kept, root, curated)
	if maxPages > 0 && len(kept) > maxPages {
		kept, p.capped = kept[:maxPages], true
	}
	seeded := map[string]bool{}
	for _, l := range seeds {
		seeded[displayKey(l.url)] = true
	}
	for _, l := range kept {
		p.tiers[planTier(l.url, root, curated)]++
		if seeded[displayKey(l.url)] {
			p.seeds++
		}
	}
	p.links = kept
	return p
}

// seedLinks are the seeded pages under root, as links.
func seedLinks(seeds []webLink, root webRoot) []webLink {
	var out []webLink
	for _, l := range seeds {
		if l.url != nil && root.contains(l.url) && isResultLink(l.url) && isPageURL(l.url) {
			out = append(out, l)
		}
	}
	return out
}

// curatedKeys are the pages llms.txt and llms-full.txt listed.
func curatedKeys(site webSite) map[string]bool {
	out := map[string]bool{}
	for _, l := range site.links {
		if l.url != nil {
			out[displayKey(l.url)] = true
		}
	}
	for _, p := range site.pages {
		if u, err := url.Parse(p.URL); err == nil {
			out[displayKey(u)] = true
		}
	}
	return out
}

// Plan tiers, first indexed first.
const (
	tierCurated   = iota // the root, pages llms.txt or a seeder names
	tierGuide            // guides, tutorials, concepts, user docs
	tierOther            // the rest, examples and galleries included
	tierReference        // API reference leaves, release notes, changelogs, blog posts
	planTiers
)

// guideSegs are path segments that name a docs site's guides.
var guideSegs = map[string]bool{
	"guide": true, "guides": true, "tutorial": true, "tutorials": true, "user": true, "users": true,
	"user-guide": true, "userguide": true, "user_guide": true, "usage": true, "learn": true, "concepts": true,
	"getting-started": true, "get-started": true, "getting_started": true, "quickstart": true, "start": true,
	"howto": true, "how-to": true, "howtos": true, "topics": true, "essentials": true, "basics": true,
	"intro": true, "introduction": true, "overview": true, "fundamentals": true, "core-concepts": true,
}

// referenceSegs are path segments of generated API reference leaves and
// of release notes.
var referenceSegs = map[string]bool{
	"_as_gen": true, "generated": true, "autoapi": true, "_autosummary": true, "_modules": true,
	"classes": true, "interfaces": true, "functions": true, "type-aliases": true, "variables": true, "enums": true,
	"changelog": true, "changelogs": true, "changes": true, "release-notes": true, "releases": true, "news": true,
	"whatsnew": true, "whats-new": true, "history": true, "prev_api_changes": true, "next_api_changes": true, "blog": true,
}

// planTier classes a page for the cap: curated pages (and the root) first,
// then guides, then the rest, then API reference leaves (a generated
// reference tree, TypeDoc's classes/functions, or two levels and more
// below an api or reference directory) and release notes.
func planTier(u *url.URL, root webRoot, curated map[string]bool) int {
	if curated[displayKey(u)] || linkKey(u) == linkKey(root.u) {
		return tierCurated
	}
	segs := pathSegments(strings.ToLower(strings.TrimPrefix(u.Path, root.prefix)))
	for i, seg := range segs {
		base := strings.TrimSuffix(strings.TrimSuffix(seg, ".html"), ".md")
		if referenceSegs[base] || (seg == "api" || seg == "reference") && len(segs)-i > 2 {
			return tierReference
		}
	}
	for _, seg := range segs {
		if guideSegs[strings.TrimSuffix(seg, ".html")] {
			return tierGuide
		}
	}
	return tierOther
}

// prioritize orders links by planTier, keeping discovery order within a
// tier except that its top-level directories take turns, so one large
// directory does not fill the cap alone.
func prioritize(links []webLink, root webRoot, curated map[string]bool) []webLink {
	var tiers [planTiers][]webLink
	for _, l := range links {
		t := planTier(l.url, root, curated)
		tiers[t] = append(tiers[t], l)
	}
	out := make([]webLink, 0, len(links))
	for t, ls := range tiers {
		if t == tierCurated {
			out = append(out, ls...)
			continue
		}
		out = append(out, roundRobin(ls, root)...)
	}
	return out
}

// roundRobin interleaves links by their first path segment below root,
// directories in the order they first appear.
func roundRobin(links []webLink, root webRoot) []webLink {
	var dirs []string
	byDir := map[string][]webLink{}
	for _, l := range links {
		segs := pathSegments(strings.TrimPrefix(l.url.Path, root.prefix))
		d := ""
		if len(segs) > 1 {
			d = segs[0]
		}
		if _, ok := byDir[d]; !ok {
			dirs = append(dirs, d)
		}
		byDir[d] = append(byDir[d], l)
	}
	out := make([]webLink, 0, len(links))
	for i := 0; len(out) < len(links); i++ {
		for _, d := range dirs {
			if i < len(byDir[d]) {
				out = append(out, byDir[d][i])
			}
		}
	}
	return out
}

// dropMirrors leaves out a site's llms.txt text mirrors (pages under an
// llms directory right below the root, mantine.dev/llms/…) when it lists
// rendered pages too: the same docs a second time.
func dropMirrors(links []webLink, root webRoot) []webLink {
	mirror := func(l webLink) bool {
		segs := pathSegments(strings.TrimPrefix(l.url.Path, root.prefix))
		return len(segs) > 1 && strings.EqualFold(segs[0], "llms")
	}
	if !slices.ContainsFunc(links, func(l webLink) bool { return !mirror(l) }) {
		return links
	}
	return slices.DeleteFunc(links, mirror)
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

// uniquePages keeps one link per displayed page (x, x.md and x.html are
// one), the first listed, except that llms-full.txt's chunks of one page
// are joined into one page in hand.
func uniquePages(links []webLink) []webLink {
	at := map[string]int{}
	var out []webLink
	for _, l := range links {
		k := strings.Replace(displayKey(l.url), ".html?", "?", 1)
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

// keepOneVariant keeps the indexed version's tree and one copy of each
// page across locales, generically by path. Pages outside the root (other
// hosts, such as v3.router.vuejs.org beside router.vuejs.org, and paths
// outside a versioned root, such as /en/1.26.x/ beside /en/stable/ or
// docs.rs's other releases) never get here. Below the root, a page whose
// path, once a locale or version segment is set aside (variantParts), is
// another page's keeps only the preferred variant (English or no locale;
// the root's own version, else the current one), as collapseVariants does
// for the live path. A whole variant tree other than the preferred one
// (/es/…, /v2/…, /docs/2.x/…) is then dropped:
//
//   - when at least half of its pages were such copies: a translation or
//     an old release of the docs, whose pages with no counterpart are
//     pages the current docs no longer have;
//   - when one of its version segments is a version the site's own list
//     names other than the indexed one (others: mike's versions.json, a
//     Read the Docs or switcher list, the version links on its pages);
//   - when it sits right below the root (after a locale) and its version
//     segment is a release (2.x, 1.8, 4x, v3) or a release channel (next,
//     latest, stable, canary, nightly, unstable, master): Docusaurus's
//     /docs/<version>/ copies and mike's version directories, which docs
//     keep at the top of their tree.
//
// A tree with few counterparts that is none of these is not a variant at
// all (an /api/v1/ path in a REST reference, release notes under
// /releases/4.2/) and stays. It returns the links kept, how many were
// dropped as other versions', and how many of those each version tree
// held (by its first version segment as written).
func keepOneVariant(links []webLink, root webRoot, others map[string]bool) ([]webLink, int, map[string]int) {
	cands := variantCands(links, root)
	best := map[string]int{}  // key -> index of its preferred variant
	trees := map[string]int{} // key -> how many trees hold it
	seen := map[[2]string]bool{}
	for i, c := range cands {
		if !seen[[2]string{c.key, c.tree}] {
			seen[[2]string{c.key, c.tree}] = true
			trees[c.key]++
		}
		if j, ok := best[c.key]; !ok || c.pref.better(cands[j].pref) {
			best[c.key] = i
		}
	}
	total, copies := map[string]int{}, map[string]int{}
	for _, c := range cands {
		total[c.tree]++
		if trees[c.key] > 1 {
			copies[c.tree]++
		}
	}
	preferred := preferredTree(cands)
	var out []webLink
	otherVersions := 0
	dropped := map[string]int{}
	for i, c := range cands {
		var drop bool
		switch {
		case best[c.key] != i:
			drop = true // another variant of this page is preferred
		case c.tree == "" || c.tree == preferred:
		default:
			listed := slices.ContainsFunc(c.labels, func(l string) bool { return others[normLabel(l)] })
			drop = 2*copies[c.tree] >= total[c.tree] || listed || c.topVers
		}
		switch {
		case !drop:
			out = append(out, c.link)
		case len(c.labels) > 0:
			otherVersions++
			dropped[c.labels[0]]++
		}
	}
	return out, otherVersions, dropped
}

// variantCand is a link as keepOneVariant weighs it.
type variantCand struct {
	link    webLink
	key     string   // its path with locale and version segments set aside
	tree    string   // those segments, joined
	labels  []string // the tree's version segments (not locales)
	pref    variantPref
	topVers bool // a release or channel segment right below the root
}

func variantCands(links []webLink, root webRoot) []variantCand {
	rootLabel := rootVersionLabel(root)
	cands := make([]variantCand, len(links))
	for i, l := range links {
		key, tree, pref := variantParts(l.url, root, rootLabel)
		c := variantCand{link: l, key: key, tree: strings.Join(tree, "/"), pref: pref, topVers: topVersionTree(l.url, root, rootLabel)}
		for _, seg := range tree {
			if !localeRe.MatchString(seg) || versionLabelRe.MatchString(seg) {
				c.labels = append(c.labels, seg)
			}
		}
		cands[i] = c
	}
	return cands
}

// preferredTree is the tree of the most wanted variant: the indexed
// version's.
func preferredTree(cands []variantCand) string {
	if len(cands) == 0 {
		return ""
	}
	top := 0
	for i, c := range cands {
		if c.pref.better(cands[top].pref) {
			top = i
		}
	}
	return cands[top].tree
}

// releaseChannels are the version aliases a docs tree right below the root
// is named for only when it is a copy of the docs: dev, main and current
// are left out, as words a docs section may be called.
var releaseChannels = map[string]bool{
	"next": true, "latest": true, "stable": true, "canary": true, "nightly": true,
	"unstable": true, "master": true, "devel": true, "devdocs": true,
}

// topVersionTree reports whether u sits in a version tree right below the
// root: its first segment below the root (after a locale) is a release
// with a dot, an x or a v (1.8, 2.x, 4x, v3) or a release channel, other
// than the root's own version, with a page below it.
func topVersionTree(u *url.URL, root webRoot, rootLabel string) bool {
	segs := pathSegments(strings.TrimPrefix(u.Path, root.prefix))
	if len(segs) > 1 && localeRe.MatchString(segs[0]) && !versionLabelRe.MatchString(segs[0]) {
		segs = segs[1:]
	}
	if len(segs) == 0 || len(segs) == 1 && !strings.HasSuffix(u.Path, "/") {
		return false
	}
	label, ok := variantLabel(segs[0])
	if !ok || rootLabel != "" && normLabel(label) == normLabel(rootLabel) {
		return false
	}
	return releaseChannels[label] || strings.ContainsAny(label, ".xv") && strings.ContainsAny(label, "0123456789")
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
