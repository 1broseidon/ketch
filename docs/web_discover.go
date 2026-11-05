package docs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

// webSite is what discovery found for one root.
type webSite struct {
	pages []webPage // full content from llms-full.txt; no per-page fetches needed
	links []webLink // candidate pages, in discovery order
	tried []string  // sources consulted, for the not-found message
	llms  bool      // the links are llms.txt's
	// index is the site's own full-text search index, as sections, when
	// its generator publishes one (web_searchindex.go): a second ranking
	// of the candidates, by what the pages say.
	index []webSection
}

// discover finds the site's pages, cheapest source first:
//
//  1. llms.txt under the root path, then at the origin. A usable sibling
//     llms-full.txt (at most maxFullBytes) supplies the content outright
//     (collect then chooses its pages: fullTextPages).
//  2. Sitemaps: robots.txt Sitemap: lines, else sitemap.xml and
//     sitemap-index.xml under the root path and at the origin; indexes are
//     followed webSitemapDepth levels deep, at most webMaxSitemaps files
//     and webMaxURLs URLs, skipping other locales' sitemaps, all within
//     sitemapBudget. The root page's raw HTML then titles the sitemap's
//     pages from its nav and adds the ones it left out, and names the
//     site's search index when its generator publishes one (site.index).
//  3. Links on the root page (and the root page itself, following one meta
//     refresh), the last resort, merged with every page of the Sphinx
//     build the root page belongs to (its searchindex.js). A root page that
//     is only a hub (a landing page linking to a few sections) has the
//     links of up to webMaxHubs of those sections read too: their
//     sidebars list the pages. The search index it names is read too.
//
// An llms.txt that lists only index files (.txt sets of pages, such as
// expressjs.com's llms/guides-5x.txt) has those read like llms-full.txt:
// they are discovery input, never results. A sitemap that lists only the
// root or version roots (Read the Docs lists /en/latest/ and /en/stable/)
// does not stop discovery, and registry docs hosts (pkg.go.dev, docs.rs)
// go straight to the root page. Only in-scope pages are kept from any
// source. Discovery files are probes: their absence is expected and never
// an error. The root page is not, so its fetch error is returned when
// nothing else turned up. An llms.txt that names little of the query is
// widened later with step 2's sources (widenLLMS).
func (w *Web) discover(ctx context.Context, root webRoot) (webSite, error) {
	var site webSite
	if w.discoverLLMS(ctx, root, &site) {
		return site, nil
	}
	if w.discoverSitemap(ctx, root, &site) {
		return site, nil
	}
	return site, w.discoverRootLinks(ctx, root, &site)
}

// discoverRootLinks is discovery's last resort (step 3 of discover): the
// root page's links, with the Sphinx build's pages and a hub's sections'
// sidebars, and the search index the root page names.
func (w *Web) discoverRootLinks(ctx context.Context, root webRoot, site *webSite) error {
	site.tried = append(site.tried, "links on "+root.String())
	info, err := w.rootPage(ctx, root)
	if err != nil {
		return rootPageFailure(ctx, root, site, err)
	}
	links := w.withSphinxLinks(ctx, root, info, &site.tried)
	if len(links) == 1 && info.page != nil && strings.TrimSpace(info.page.Markdown) == "" {
		return nil // an empty root page with no links: nothing to search
	}
	site.links = w.withHubLinks(ctx, root, links, &site.tried)
	site.index = w.searchIndex(ctx, root, info.indexLocs, &site.tried)
	return nil
}

// discoverSitemap reads the site's sitemaps, when its host is worth
// probing, and reports whether they list pages. The root page's nav, when
// its HTML carries one, titles them and adds what the sitemap left out,
// and the search index it names ranks them (site.index).
func (w *Web) discoverSitemap(ctx context.Context, root webRoot, site *webSite) bool {
	if !hostDiscovery(root).sitemaps {
		return false
	}
	sitemap := w.sitemapLinks(ctx, root, &site.tried)
	if !usefulSitemap(sitemap, root) {
		return false
	}
	site.links = mergeLinks(site.links, sitemap)
	site.tried = append(site.tried, "nav on "+root.String())
	if info, ok := w.rootNav(ctx, root); ok {
		site.links = mergeLinks(site.links, info.links)
		site.index = w.searchIndex(ctx, root, info.indexLocs, &site.tried)
	}
	return true
}

// discoverLLMS tries llms.txt (with llms-full.txt), as far as root's host
// is worth probing, and reports whether it answered with pages.
func (w *Web) discoverLLMS(ctx context.Context, root webRoot, site *webSite) bool {
	probes := hostDiscovery(root)
	if probes.llms {
		llms := w.findLLMSTxt(ctx, root, &site.tried)
		if llms.url != nil {
			var index []webLink
			llms.links, index = splitIndexLinks(llms.links)
			site.pages = w.fullText(ctx, root, llms, &site.tried)
			if len(site.pages) == 0 && len(llms.links) == 0 && len(index) > 0 {
				site.pages, llms.links = w.indexFiles(ctx, root, llms, index, &site.tried)
			}
			if len(site.pages) > 0 || len(llms.links) > 0 {
				site.links, site.llms = llms.links, true
				return true
			}
		}
	}
	return false
}

// rootPageFailure is discover's error for a root page that could not be
// read: the caller's cancellation; nil for a missing page (the caller
// reports what was tried); else the failure, upstream, naming what else
// was tried. A root that redirected away says where, in site.tried.
func rootPageFailure(ctx context.Context, root webRoot, site *webSite, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var scope *scopeError
	if errors.As(err, &scope) {
		// Say where the root went, so the caller can pass that URL instead.
		site.tried[len(site.tried)-1] += " (" + scope.reason() + ")"
	}
	if errors.Is(err, ErrNotFound) {
		return nil // nothing at all (a 404 or 410)
	}
	// Any other failure (a 5xx, a refused connection) stays upstream, but
	// still says what else was tried.
	return fmt.Errorf("web: fetching %s failed (tried %s): %w", root, strings.Join(site.tried[:len(site.tried)-1], ", "), err)
}

// discoveryProbes says which discovery files a host is worth asking for.
type discoveryProbes struct {
	llms, sitemaps bool
}

// registryDocsHosts serve generated API docs with no llms.txt, and a
// robots.txt naming global sitemap indexes that list nothing under one
// package's root: probing them costs up to a dozen large fetches for
// nothing.
var registryDocsHosts = map[string]bool{"pkg.go.dev": true, "docs.rs": true}

// hostDiscovery returns the probes discovery makes for root's host.
func hostDiscovery(root webRoot) discoveryProbes {
	if registryDocsHosts[scopeHost(root.u)] {
		return discoveryProbes{}
	}
	return discoveryProbes{llms: true, sitemaps: true}
}

// usefulSitemap reports sitemap links that include a page: something
// other than the root or a version root (a path below the root of at most
// two segments, each a locale or a version label). Read the Docs sitemaps
// list only /en/latest/, /en/stable/ and the like.
func usefulSitemap(links []webLink, root webRoot) bool {
	return slices.ContainsFunc(links, func(l webLink) bool { return !isVersionRootPath(l.url, root) })
}

// isVersionRootPath reports u as the root itself or a version root below
// it (/en/stable/, /latest/).
func isVersionRootPath(u *url.URL, root webRoot) bool {
	rel := pathSegments(strings.TrimPrefix(u.Path, root.prefix))
	if len(rel) > 2 {
		return false
	}
	for _, s := range rel {
		if _, ok := segmentLabel(s); !ok && !localeRe.MatchString(s) {
			return false
		}
	}
	return true
}

// withSphinxLinks returns the root page's links, merged with the pages of
// the Sphinx build it belongs to when it is one and its search index
// answers.
func (w *Web) withSphinxLinks(ctx context.Context, root webRoot, info rootInfo, tried *[]string) []webLink {
	s, ok := detectSphinx(info.at, info.generator, info.scripts)
	if !ok {
		return info.links
	}
	*tried = append(*tried, s.root.String()+"searchindex.js")
	links, err := w.sphinxLinks(ctx, root, s)
	if err != nil {
		return info.links
	}
	return mergeLinks(info.links, links)
}

// mergeLinks appends extra's links to links, in order, filling in the
// title or description a link already present lacks.
func mergeLinks(links, extra []webLink) []webLink {
	out := slices.Clone(links)
	pos := make(map[string]int, len(out)+len(extra))
	for i, l := range out {
		pos[linkKey(l.url)] = i
	}
	for _, l := range extra {
		i, ok := pos[linkKey(l.url)]
		if !ok {
			pos[linkKey(l.url)] = len(out)
			out = append(out, l)
			continue
		}
		out[i].title = firstNonEmpty(out[i].title, l.title)
		out[i].description = firstNonEmpty(out[i].description, l.description)
	}
	return out
}

// Hub expansion bounds. A root page with more in-scope links than
// webHubMaxLinks lists its pages itself; one with fewer is a hub, and up to
// webMaxHubs of its section links are read for theirs, concurrently, each
// a probe-sized raw fetch: it costs no more than the sitemap files a site
// with sitemaps would have had read.
const (
	webHubMaxLinks = 30
	webMaxHubs     = 2
)

// withHubLinks returns links (the root page first, then its links), merged
// with the links of the hub's sections when the root page is only a hub:
// tokio.rs links to /tokio/tutorial and a blog post, v2.vuejs.org to
// /v2/guide/ and /v2/api/, reactrouter.com to /home, and the pages are
// listed in those sections' sidebars. A section that cannot be read is
// skipped.
func (w *Web) withHubLinks(ctx context.Context, root webRoot, links []webLink, tried *[]string) []webLink {
	if len(links) == 0 || len(links)-1 > webHubMaxLinks || registryDocsHosts[scopeHost(root.u)] {
		return links
	}
	hubs := pickHubs(links[1:], root, webMaxHubs)
	found := make([][]webLink, len(hubs))
	var wg sync.WaitGroup
	for i, h := range hubs {
		*tried = append(*tried, "links on "+h.url.String())
		wg.Add(1)
		go func() {
			defer wg.Done()
			if info, err := w.readPage(ctx, root, h.url); err == nil {
				found[i] = info.links
			}
		}()
	}
	wg.Wait()
	for _, f := range found {
		links = mergeLinks(links, f)
	}
	return links
}

// hubWords name a docs section: a link titled or pathed with one leads to
// a page whose sidebar lists that section's pages.
var hubWords = map[string]bool{
	"doc": true, "docs": true, "documentation": true, "guide": true, "tutorial": true, "learn": true,
	"manual": true, "handbook": true, "book": true, "reference": true, "api": true, "topic": true,
	"started": true, "introduction": true, "intro": true, "overview": true, "concept": true, "cookbook": true,
}

// notHubWords name sections that are not docs: a blog's sidebar lists posts.
var notHubWords = map[string]bool{
	"blog": true, "news": true, "changelog": true, "release": true, "community": true, "sponsor": true,
	"support": true, "eol": true, "about": true, "team": true, "job": true, "career": true, "event": true,
	"showcase": true, "pricing": true, "login": true, "signup": true, "search": true, "tag": true, "author": true,
	"archive": true, "privacy": true, "term": true, "contact": true, "download": true, "upgrading": true,
}

// pickHubs returns up to n of links that look like docs sections, best
// first, ties in link order: a path segment that is a hub word counts 2, a
// hub word in the link's title 1; a link naming a non-docs section, or the
// root itself, is never one.
func pickHubs(links []webLink, root webRoot, n int) []webLink {
	type hub struct {
		link  webLink
		score int
	}
	var hubs []hub
	for _, l := range links {
		if linkKey(l.url) == linkKey(root.u) {
			continue
		}
		score, skip := 0, false
		for _, seg := range pathSegments(strings.TrimPrefix(l.url.Path, root.prefix)) {
			for _, t := range tokenize(seg) {
				skip = skip || notHubWords[t]
			}
			if hubWords[stem(strings.ToLower(strings.TrimSuffix(seg, path.Ext(seg))))] {
				score += 2
			}
		}
		for _, t := range tokenize(l.title) {
			skip = skip || notHubWords[t]
			if hubWords[t] {
				score++
			}
		}
		if score > 0 && !skip {
			hubs = append(hubs, hub{l, score})
		}
	}
	sort.SliceStable(hubs, func(i, j int) bool { return hubs[i].score > hubs[j].score })
	out := make([]webLink, 0, n)
	for _, h := range hubs[:min(n, len(hubs))] {
		out = append(out, h.link)
	}
	return out
}

// webMaxIndexFiles caps the index files read from one llms.txt.
const webMaxIndexFiles = 3

// splitIndexLinks separates llms.txt's page links from its index files:
// text files that collect several pages (expressjs.com's
// llms/guides-5x.txt, a Starlight site's _llms-txt/ sets). An index file
// is no answer itself (isResultLink), but it may name the pages it holds.
func splitIndexLinks(links []webLink) (pages, index []webLink) {
	for _, l := range links {
		if strings.EqualFold(path.Ext(l.url.Path), ".txt") {
			index = append(index, l)
		} else {
			pages = append(pages, l)
		}
	}
	return pages, index
}

// indexFiles reads an llms.txt's index files as discovery input: one that
// says it holds everything ("full" in its name or title), else the first
// webMaxIndexFiles, within maxFullBytes in all. Each is split like
// llms-full.txt; a chunk whose URL line names an in-scope page becomes that
// page, and the file's own page links are kept. A chunk naming no page is
// dropped: the index file is not a page to show. Version and locale copies
// of one page (guides for 5.x and 4.x) collapse as candidate links do.
func (w *Web) indexFiles(ctx context.Context, root webRoot, llms llmsTxt, index []webLink, tried *[]string) ([]webPage, []webLink) {
	files := index[:min(len(index), webMaxIndexFiles)]
	if i := slices.IndexFunc(index, isFullIndex); i >= 0 {
		files = index[i : i+1]
	}
	bodies := make([][]byte, len(files))
	served := make([]*url.URL, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		*tried = append(*tried, f.url.String())
		wg.Add(1)
		go func() {
			defer wg.Done()
			if body, from, ok := w.probe(ctx, root, f.url.String()); ok {
				bodies[i], served[i] = body, from
			}
		}()
	}
	wg.Wait()
	var pages []webPage
	var links []webLink
	seenPage, seenLink := map[string]bool{}, map[string]bool{}
	total := 0
	for i, body := range bodies {
		if body == nil || total+len(body) > w.maxFullBytes {
			continue
		}
		total += len(body)
		for _, chunk := range splitFullText(string(body)) {
			pageURL, how := fullPageURL(chunk, served[i], root, llms.pageTitled(chunk.title, nil))
			if how != chunkInScope || strings.TrimSpace(chunk.body) == "" || seenPage[pageURL] {
				continue
			}
			seenPage[pageURL] = true
			pages = append(pages, webPage{URL: pageURL, Title: chunk.title, Markdown: capText(chunk.body, w.maxPageBytes), markdown: true})
		}
		for _, l := range markdownLinks(string(body), root, served[i]) {
			if isResultLink(l.url) && !seenLink[linkKey(l.url)] {
				seenLink[linkKey(l.url)] = true
				links = append(links, l)
			}
		}
	}
	return collapsePages(pages, root), links
}

// isFullIndex reports an index file that says it holds all the docs.
func isFullIndex(l webLink) bool {
	name := strings.ToLower(strings.TrimSuffix(path.Base(l.url.Path), path.Ext(l.url.Path)))
	title := strings.ToLower(l.title)
	return strings.Contains(name, "full") || strings.Contains(title, "full") || strings.Contains(title, "complete")
}

// collapsePages keeps one page per collapseVariants group, in order.
func collapsePages(pages []webPage, root webRoot) []webPage {
	ranked := make([]scoredLink, 0, len(pages))
	byURL := map[string]webPage{}
	for _, p := range pages {
		u, err := url.Parse(p.URL)
		if err != nil {
			continue
		}
		byURL[u.String()] = p
		ranked = append(ranked, scoredLink{link: webLink{url: u}})
	}
	var out []webPage
	for _, r := range collapseVariants(ranked, root) {
		out = append(out, byURL[r.link.url.String()])
	}
	return out
}

// llmsTxt is a parsed llms.txt.
type llmsTxt struct {
	url         *url.URL // where it was served from; nil when absent
	title       string   // the H1
	description string   // the first blockquote
	links       []webLink
	// identity are the manifest's links to a code forge repository or a
	// package registry page, in any scope: what the site says it is
	// (repoLinks, packageLinked).
	identity []*url.URL
	// titled maps each lower-cased link title to the page it names, in
	// scope or not, and to nil when links to different pages share it.
	titled map[string]*url.URL
}

// pageTitled returns the one page llms.txt gives title, or nil when it
// gives none, or several (two products' "Introduction"), or when more than
// one llms-full.txt chunk carries the title (chunkTitles counts them): then
// a title cannot say which page a chunk is.
func (l llmsTxt) pageTitled(title string, chunkTitles map[string]int) *url.URL {
	k := strings.ToLower(title)
	if k == "" || chunkTitles[k] > 1 {
		return nil
	}
	return l.titled[k]
}

// findLLMSTxt probes llms.txt under the root path, then at the origin. The
// first file with in-scope links wins; a file with none still supplies the
// site's title and description and stays the llms-full.txt anchor.
func (w *Web) findLLMSTxt(ctx context.Context, root webRoot, tried *[]string) llmsTxt {
	var found llmsTxt
	for _, loc := range uniqueStrings(root.at("llms.txt"), root.origin()+"/llms.txt") {
		*tried = append(*tried, loc)
		body, from, ok := w.probe(ctx, root, loc)
		if !ok {
			continue
		}
		parsed := parseLLMSTxt(string(body), from, root)
		if found.url == nil {
			found = parsed
		} else if found.title == "" {
			found.title, found.description = parsed.title, parsed.description
		}
		if len(parsed.links) > 0 {
			parsed.title, parsed.description = firstNonEmpty(found.title, parsed.title), firstNonEmpty(found.description, parsed.description)
			return parsed
		}
	}
	return found
}

var (
	// mdLinkRe matches [text](href "optional title"); images are skipped by
	// the caller.
	mdLinkRe = regexp.MustCompile(`\[([^\]\n]*)\]\(\s*<?([^)\s>]+)>?(?:\s+["'][^"'\n]*["'])?\s*\)`)
	// linkDescRe takes the description an llms.txt entry puts after the link.
	linkDescRe = regexp.MustCompile(`^\s*[:–—-]\s*(.*)$`)
)

// parseLLMSTxt reads the llms.txt format: an H1 title, a blockquote
// summary, and Markdown link lists ("- [Title](url): description") under H2
// sections. Links resolve against the file's URL; off-scope, asset and
// llms*.txt links are dropped, though every page link's title is noted for
// pageTitled. The H2 a link sits under is folded into its description, so
// "## API reference" helps match an API query.
func parseLLMSTxt(body string, base *url.URL, root webRoot) llmsTxt {
	out := llmsTxt{url: base, titled: map[string]*url.URL{}}
	seen := map[string]bool{}
	var section string
	quote := 0 // 0 before the first blockquote, 1 inside it, 2 after
	var fence fenceState
	for _, line := range strings.Split(body, "\n") {
		if fence.step(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case out.title == "" && strings.HasPrefix(trimmed, "# "):
			out.title = cleanInline(trimmed[2:])
			continue
		case strings.HasPrefix(trimmed, "## "):
			section = cleanInline(trimmed[3:])
		case strings.HasPrefix(trimmed, ">") && quote < 2:
			quote = 1
			out.description = strings.TrimSpace(out.description + " " + strings.TrimSpace(strings.TrimPrefix(trimmed, ">")))
			continue
		}
		if quote == 1 {
			quote = 2
		}
		out.links = appendMarkdownLinks(out.links, line, base, root, section, seen, out.titled)
		out.identity = appendIdentityLinks(out.identity, line, base)
		if len(out.links) >= webMaxURLs {
			break
		}
	}
	return out
}

// appendMarkdownLinks adds the in-scope page links on one Markdown line,
// noting every page link's title in titled when it is non-nil.
func appendMarkdownLinks(links []webLink, line string, base *url.URL, root webRoot, section string, seen map[string]bool, titled map[string]*url.URL) []webLink {
	matches := mdLinkRe.FindAllStringSubmatchIndex(line, -1)
	for i, m := range matches {
		if m[0] > 0 && line[m[0]-1] == '!' {
			continue
		}
		u, ok := resolveRef(base, line[m[4]:m[5]])
		if !ok || !isPageURL(u) {
			continue
		}
		if titled != nil {
			noteTitle(titled, cleanInline(line[m[2]:m[3]]), u)
		}
		if !root.contains(u) || seen[linkKey(u)] {
			continue
		}
		seen[linkKey(u)] = true
		desc := ""
		if i == 0 {
			rest := line[m[1]:]
			if len(matches) > 1 {
				rest = line[m[1]:matches[1][0]]
			}
			if m := linkDescRe.FindStringSubmatch(rest); m != nil {
				desc = strings.TrimSpace(m[1])
			}
		}
		if section != "" {
			desc = strings.TrimSpace(desc + " (" + section + ")")
		}
		links = append(links, webLink{url: u, title: cleanInline(line[m[2]:m[3]]), description: desc})
	}
	return links
}

// webMaxIdentityLinks caps the identity links kept from one manifest.
const webMaxIdentityLinks = 32

// appendIdentityLinks adds the Markdown links on one line that point at a
// GitHub repository or a package registry's page for a package.
func appendIdentityLinks(out []*url.URL, line string, base *url.URL) []*url.URL {
	for _, m := range mdLinkRe.FindAllStringSubmatch(line, -1) {
		if len(out) >= webMaxIdentityLinks {
			break
		}
		u, ok := resolveRef(base, m[2])
		if !ok {
			continue
		}
		if _, repo := githubRepoOf(u); repo || registryPackageOf(u) != (pkgRef{}) {
			out = append(out, u)
		}
	}
	return out
}

// noteTitle records that title names u, or that it is ambiguous (nil) when
// it already names another page; x.md and x are the same page.
func noteTitle(titled map[string]*url.URL, title string, u *url.URL) {
	k := strings.ToLower(title)
	if k == "" {
		return
	}
	if prev, ok := titled[k]; !ok {
		titled[k] = u
	} else if prev != nil && displayURL(prev) != displayURL(u) {
		titled[k] = nil
	}
}

// linkKey identifies a page regardless of a trailing slash or "www.".
func linkKey(u *url.URL) string {
	return scopeHost(u) + strings.TrimSuffix(u.EscapedPath(), "/") + "?" + u.RawQuery
}

var assetExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".webp": true, ".avif": true, ".ico": true,
	".css": true, ".js": true, ".mjs": true, ".json": true, ".xml": true, ".rss": true, ".atom": true,
	".pdf": true, ".zip": true, ".gz": true, ".tgz": true, ".tar": true,
	".mp4": true, ".webm": true, ".mp3": true, ".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
}

// isPageURL drops assets, feeds and the discovery files themselves.
func isPageURL(u *url.URL) bool {
	base := strings.ToLower(path.Base(u.Path))
	return !assetExts[path.Ext(base)] && !isLLMSFile(base) && base != "robots.txt"
}

// fullText returns llms-full.txt (the sibling of the llms.txt found) split
// into pages, or nil when it is absent, over maxFullBytes, yields no
// in-scope page, or mostly cannot say which page a chunk came from while
// llms.txt has page links to fetch instead.
func (w *Web) fullText(ctx context.Context, root webRoot, llms llmsTxt, tried *[]string) []webPage {
	loc := llms.url.ResolveReference(&url.URL{Path: "llms-full.txt"}).String()
	*tried = append(*tried, loc)
	body, full, ok := w.probe(ctx, root, loc)
	if !ok || len(body) > w.maxFullBytes {
		return nil
	}
	chunks := splitFullText(string(body))
	chunkTitles := map[string]int{}
	for _, c := range chunks {
		chunkTitles[strings.ToLower(c.title)]++
	}
	var pages []webPage
	attributed, unknown := 0, 0
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.body) == "" {
			continue
		}
		pageURL, how := fullPageURL(chunk, full, root, llms.pageTitled(chunk.title, chunkTitles))
		switch how {
		case chunkInScope:
			attributed++
		case chunkUnattributed:
			unknown++
		}
		if pageURL == "" {
			continue
		}
		title := firstNonEmpty(chunk.title, llms.title, pathTitle(full.Path))
		pages = append(pages, webPage{URL: pageURL, Title: title, Markdown: capText(chunk.body, w.maxPageBytes), markdown: true})
	}
	// Results should link to pages. When most chunks name none (Hono's file
	// has no per-page URLs, or its titles are shared) and llms.txt lists
	// pages, fetching the best of those beats pointing results at
	// llms-full.txt or losing the chunks outright. Chunks that belong to
	// pages outside the root do not count either way.
	if attributed < unknown && len(llms.links) > 0 {
		return nil
	}
	return pages
}

// chunkAttribution is how fullPageURL placed an llms-full.txt chunk.
type chunkAttribution int

const (
	chunkUnattributed chunkAttribution = iota // no usable URL line or title
	chunkInScope                              // a page in the docs root
	chunkOffScope                             // a page outside it: dropped
)

// fullPageURL attributes an llms-full.txt chunk to a page: the first of its
// URL/Source lines that is in scope (Fumadocs pairs a site URL with an
// off-site GitHub Source, so scope picks the right one). A chunk that names
// only off-scope pages is dropped. One that names none takes titled, the
// llms.txt page its title names unambiguously (dropped when that page is out
// of scope), else llms-full.txt itself, where its text came from, when that
// is in scope. "" drops the chunk.
func fullPageURL(chunk fullChunk, full *url.URL, root webRoot, titled *url.URL) (string, chunkAttribution) {
	for _, ref := range chunk.refs {
		if u, ok := root.resolve(full, ref); ok {
			return displayURL(u), chunkInScope
		}
	}
	switch {
	case len(chunk.refs) > 0:
		return "", chunkOffScope
	case titled != nil && root.contains(titled):
		return displayURL(titled), chunkInScope
	case titled != nil:
		return "", chunkOffScope
	case root.contains(full):
		return full.String(), chunkUnattributed
	}
	return "", chunkUnattributed
}

// fullChunk is one page of an llms-full.txt.
type fullChunk struct {
	title string
	refs  []string // URL/Source metadata, best first
	body  string
}

// metaLineRe matches the per-page metadata generators put by the H1 or in
// a page's frontmatter: "Source: https://…" (Mintlify), "URL: /docs/x"
// (Fumadocs), url: /x.md (VitePress), url: "https://…" (YAML-quoted).
var metaLineRe = regexp.MustCompile(`(?i)^\s*(url|source|source_url|canonical_url|link)\s*:\s*["']?<?([^\s"'<>]+?)>?["']?\s*$`)

var (
	// frontmatterKeyRe matches a YAML key line in a frontmatter block.
	frontmatterKeyRe = regexp.MustCompile(`^[A-Za-z_][\w.-]*\s*:(?:\s|$)`)
	// frontmatterTitleRe takes a frontmatter block's title value.
	frontmatterTitleRe = regexp.MustCompile(`(?i)^title\s*:\s*(.+?)\s*$`)
	// linkedHeadingRe matches a heading that is only a link: "# [Aliases](https://…)".
	linkedHeadingRe = regexp.MustCompile(`^\[([^\]]+)\]\(\s*<?([^)\s>]+)>?\s*\)$`)
	// pathTitleRe matches a heading that ends with its page's address in
	// parentheses: "Choose a setup path (/docs/getting-started)".
	pathTitleRe = regexp.MustCompile(`^(.+?)\s+\((/[^\s()]*|https?://[^\s()]+)\)$`)
)

// webMaxFrontmatterLines bounds how far a frontmatter block is looked for
// its closing "---"; a longer run is not frontmatter.
const webMaxFrontmatterLines = 40

// fullSplitter is splitFullText's state: the page being read.
type fullSplitter struct {
	chunks []fullChunk
	cur    fullChunk
	lines  []string
	// open: the page was opened by a delimiter that does not title it
	// (a frontmatter block, a heading that is only a link), so the H1 that
	// follows before any text is its title, not another page.
	open bool
}

// hasText reports body text in the page being read, metadata lines aside.
func (s *fullSplitter) hasText() bool {
	return slices.ContainsFunc(s.lines, func(l string) bool {
		return strings.TrimSpace(l) != "" && !metaLineRe.MatchString(l)
	})
}

func (s *fullSplitter) flush() {
	s.cur.body = takeLeadingMeta(&s.cur, s.lines)
	if s.cur.title != "" || s.cur.body != "" || len(s.cur.refs) > 0 {
		s.chunks = append(s.chunks, s.cur)
	}
	s.cur, s.lines, s.open = fullChunk{}, nil, false
}

// startPage begins a page, unless the page being read is still waiting
// for its title and has no text: then that page goes on.
func (s *fullSplitter) startPage() {
	if !s.open || s.hasText() {
		s.flush()
	}
}

// splitFullText splits llms-full.txt into pages. A page starts at an H1
// outside code fences; at a YAML frontmatter block (---, key: value lines,
// ---), which generators put before each page (VitePress, Next.js docs),
// with or without an H1 after it; and at a deeper heading followed by a
// URL line naming an absolute or root-relative address (an index file's
// "## Error Handling" then "URL: https://expressjs.com/en/5x/guide/error-handling").
//
// A frontmatter block, or a heading that is only a link to the page
// ("# [Aliases](https://…)" before "# Aliases"), opens a page that the
// next H1 titles when no text came between: one page, not an empty one
// and an unattributed one. The page's URL comes from its frontmatter's
// url/source keys, the heading's link, or metadata lines right after the
// heading, all removed from the body; its title from the H1, else the
// frontmatter's title.
func splitFullText(body string) []fullChunk {
	s := &fullSplitter{}
	var fence fenceState
	all := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i := 0; i < len(all); i++ {
		line := all[i]
		if fence.step(line) {
			s.lines = append(s.lines, line)
			continue
		}
		if end, ok := frontmatterEnd(all, i); ok {
			s.startPage()
			for _, l := range all[i+1 : end] {
				if m := metaLineRe.FindStringSubmatch(l); m != nil {
					s.cur.refs = append(s.cur.refs, m[2])
				} else if m := frontmatterTitleRe.FindStringSubmatch(l); m != nil && s.cur.title == "" {
					s.cur.title = strings.Trim(m[1], `"'`)
				}
			}
			s.open, i = true, end
			continue
		}
		h, ok := parseATX(line)
		if !ok || (h.level != 1 && !pageURLFollows(all[i+1:])) {
			s.lines = append(s.lines, line)
			continue
		}
		s.startPage()
		title, ref := h.text, ""
		if m := linkedHeadingRe.FindStringSubmatch(strings.TrimSpace(h.raw)); m != nil {
			title, ref = cleanInline(m[1]), m[2]
			s.cur.refs = append(s.cur.refs, ref)
		} else if m := pathTitleRe.FindStringSubmatch(h.text); m != nil {
			// "# Title (/docs/path)": the page's address after its title
			// (Fumadocs' default llms-full.txt). It titles the page itself.
			title = m[1]
			s.cur.refs = append(s.cur.refs, m[2])
		}
		if title != "" {
			s.cur.title = title
		}
		s.open = ref != ""
	}
	s.flush()
	return s.chunks
}

// frontmatterEnd reports a YAML frontmatter block opening at line i: "---"
// then key: value lines (with indented continuations, list items and
// blank lines) and a closing "---" within webMaxFrontmatterLines, holding
// at least one key. end is the closing line.
func frontmatterEnd(lines []string, i int) (end int, ok bool) {
	if strings.TrimRight(lines[i], " \t") != "---" {
		return 0, false
	}
	keys := 0
	for j := i + 1; j < len(lines) && j <= i+webMaxFrontmatterLines; j++ {
		l := lines[j]
		switch t := strings.TrimSpace(l); {
		case t == "---":
			return j, keys > 0
		case frontmatterKeyRe.MatchString(l):
			keys++
		case t == "" || l[0] == ' ' || l[0] == '\t' || strings.HasPrefix(t, "- "):
			if keys == 0 {
				return 0, false // a rule, then text: not frontmatter
			}
		default:
			return 0, false
		}
	}
	return 0, false
}

// pageURLFollows reports a URL metadata line, blank lines aside, at the
// start of lines whose value is an absolute http(s) URL or a root-relative
// path: what makes a heading below H1 the start of a page.
func pageURLFollows(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		m := metaLineRe.FindStringSubmatch(l)
		return m != nil && (isHTTPURL(m[2]) || strings.HasPrefix(m[2], "/") && !strings.HasPrefix(m[2], "//"))
	}
	return false
}

// takeLeadingMeta moves metadata lines at the top of a chunk into its refs
// and returns the remaining body.
func takeLeadingMeta(c *fullChunk, lines []string) string {
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	for n := 0; i < len(lines) && n < 4; i, n = i+1, n+1 {
		m := metaLineRe.FindStringSubmatch(lines[i])
		if m == nil {
			break
		}
		c.refs = append(c.refs, m[2])
	}
	end := len(lines)
	for end > i && (strings.TrimSpace(lines[end-1]) == "" || isThematicBreak(lines[end-1])) {
		end-- // the rule a generator puts between pages
	}
	return strings.TrimSpace(strings.Join(lines[i:end], "\n"))
}

// isThematicBreak reports a Markdown thematic break: three or more of one
// of -, * or _, spaces aside.
func isThematicBreak(line string) bool {
	t := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(line), " ", ""), "\t", "")
	return len(t) >= 3 && (strings.Trim(t, "-") == "" || strings.Trim(t, "*") == "" || strings.Trim(t, "_") == "")
}

// parseSitemap streams a urlset or sitemapindex and returns whether it is
// an index and at most limit of its <loc> values: those directly inside
// <url> or <sitemap>, so image:loc and the like are skipped. A malformed or
// truncated tail ends the parse with what was read, and a huge sitemap
// (Astro's lists every page in every locale) costs no more than the cap.
func parseSitemap(body []byte, limit int) (index bool, locs []string) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var stack []string
	var loc strings.Builder
	for len(locs) < limit {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				if t.Name.Local != "urlset" && t.Name.Local != "sitemapindex" {
					return false, nil
				}
				index = t.Name.Local == "sitemapindex"
			}
			stack = append(stack, t.Name.Local)
			loc.Reset()
		case xml.CharData:
			loc.Write(t)
		case xml.EndElement:
			if n := len(stack); n >= 2 && stack[n-1] == "loc" && (stack[n-2] == "url" || stack[n-2] == "sitemap") {
				if v := strings.TrimSpace(loc.String()); v != "" {
					locs = append(locs, v)
				}
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return index, locs
}

// sitemapLinks collects in-scope page URLs from the site's sitemaps:
// every robots.txt Sitemap: entry, or, when those yield nothing, the first
// of sitemap.xml and sitemap-index.xml (under the root path, then at the
// origin) that does. The walk gets sitemapBudget in all, however many
// files it reads, and keeps what it has when that runs out: the files are
// read one after another, and a slow host's (Django's, uncached, took 2 to
// 10s each) would otherwise spend the call's whole budget, where the root
// page's links still answer.
func (w *Web) sitemapLinks(ctx context.Context, root webRoot, tried *[]string) []webLink {
	if w.sitemapBudget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.sitemapBudget)
		defer cancel()
	}
	robots := root.origin() + "/robots.txt"
	*tried = append(*tried, robots+" sitemaps")
	var listed []string
	if body, _, ok := w.probe(ctx, root, robots); ok {
		listed = robotsSitemaps(body, root)
	}
	walk := &sitemapWalk{w: w, root: root, seen: map[string]bool{}, files: map[string]bool{}}
	for _, loc := range listed {
		walk.fetch(ctx, loc, 0)
	}
	// The conventional locations, sitemap-index.xml being where Astro's
	// sitemap integration writes one robots.txt often does not mention.
	defaults := []string{root.at("sitemap.xml"), root.origin() + "/sitemap.xml", root.at("sitemap-index.xml"), root.origin() + "/sitemap-index.xml"}
	for _, loc := range uniqueStrings(defaults...) {
		if len(walk.links) > 0 {
			break
		}
		*tried = append(*tried, loc)
		walk.fetch(ctx, loc, 0)
	}
	return walk.links
}

// robotsSitemaps returns the Sitemap: URLs in a robots.txt that are on the
// root's host: a sitemap elsewhere lists another site's pages.
func robotsSitemaps(body []byte, root webRoot) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "sitemap") {
			continue
		}
		if u, err := root.u.Parse(strings.TrimSpace(value)); err == nil && root.sameHost(u) {
			out = append(out, u.String())
		}
	}
	return uniqueStrings(out...)
}

// sitemapWalk follows sitemaps and indexes on the root's host within the
// file, depth and URL caps.
type sitemapWalk struct {
	w     *Web
	root  webRoot
	seen  map[string]bool
	files map[string]bool
	links []webLink
}

func (s *sitemapWalk) fetch(ctx context.Context, loc string, depth int) {
	if s.files[loc] || len(s.files) >= webMaxSitemaps || len(s.links) >= webMaxURLs || ctx.Err() != nil {
		return
	}
	s.files[loc] = true
	body, base, ok := s.w.probe(ctx, s.root, loc) // refuses other hosts, before and after redirects
	if !ok {
		return
	}
	index, locs := parseSitemap(gunzipMaybe(body), 2*webMaxURLs)
	if index {
		if depth < webSitemapDepth {
			for _, child := range preferScoped(ownLocale(sameHostLocs(locs, base, s.root), s.root), s.root) {
				s.fetch(ctx, child, depth+1)
			}
		}
		return
	}
	for _, entry := range locs {
		u, ok := s.root.resolve(base, entry)
		if !ok || !isPageURL(u) || s.seen[linkKey(u)] {
			continue
		}
		s.seen[linkKey(u)] = true
		s.links = append(s.links, webLink{url: u})
		if len(s.links) >= webMaxURLs {
			return
		}
	}
}

// sameHostLocs resolves a sitemap index's children against the index and
// keeps those on the root's host.
func sameHostLocs(locs []string, base *url.URL, root webRoot) []string {
	var out []string
	for _, loc := range locs {
		if u, err := base.Parse(strings.TrimSpace(loc)); err == nil && root.sameHost(u) {
			u.Fragment, u.RawFragment = "", ""
			out = append(out, u.String())
		}
	}
	return out
}

// sitemapLocaleRe matches the locale ending a sitemap file's name:
// sitemap-en.xml, sitemap-pt-br.xml, sitemap_zh-hans.xml.gz.
var sitemapLocaleRe = regexp.MustCompile(`(?i)[-_.]([a-z]{2}(?:[-_][a-z]{2,4})?)\.xml(?:\.gz)?$`)

// sitemapLocale is the locale a sitemap is for, lower case: the one its
// file name ends with, else its directory's (/fr/sitemap.xml); "" for none.
func sitemapLocale(loc string) string {
	u, err := url.Parse(loc)
	if err != nil {
		return ""
	}
	if m := sitemapLocaleRe.FindStringSubmatch(path.Base(u.Path)); m != nil {
		return strings.ToLower(m[1])
	}
	if dir := path.Base(path.Dir(u.Path)); localeRe.MatchString(dir) {
		return strings.ToLower(dir)
	}
	return ""
}

// ownLocale drops an index's sitemaps for other locales when the root sits
// under a locale and the index has one for it: Django's lists
// sitemap-el.xml to sitemap-zh-hans.xml beside sitemap-en.xml, eleven
// files and 1.2 MB a root under /en/ has no use for.
func ownLocale(children []string, root webRoot) []string {
	segs := pathSegments(root.prefix)
	if len(segs) == 0 || !localeRe.MatchString(segs[0]) {
		return children
	}
	want := strings.ToLower(segs[0])
	if !slices.ContainsFunc(children, func(c string) bool { return sitemapLocale(c) == want }) {
		return children
	}
	return slices.DeleteFunc(slices.Clone(children), func(c string) bool {
		l := sitemapLocale(c)
		return l != "" && l != want
	})
}

// preferScoped orders an index's children so ones naming the root's last
// path segment (sitemap-docs.xml for /docs/) are fetched first under the cap.
func preferScoped(children []string, root webRoot) []string {
	hint := strings.ToLower(path.Base(strings.TrimSuffix(root.prefix, "/")))
	var first, rest []string
	for _, loc := range children {
		if hint != "/" && hint != "." && strings.Contains(strings.ToLower(loc), hint) {
			first = append(first, loc)
		} else {
			rest = append(rest, loc)
		}
	}
	return append(first, rest...)
}

// gunzipMaybe inflates a gzip-compressed sitemap (sitemap.xml.gz), bounded.
func gunzipMaybe(body []byte) []byte {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return body
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return body
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, webMaxRawBytes))
	if err != nil {
		return nil
	}
	return out
}

// rootInfo is what the root page says about the site.
type rootInfo struct {
	title, description string
	at                 *url.URL  // where the page was served from
	links              []webLink // in-scope links; rootPage puts the page itself first
	page               *webPage  // the page's content when already in hand
	refresh            *url.URL  // in-scope <meta http-equiv="refresh"> target
	// indexLocs are where the page says its site's search index is
	// (searchIndexLocs), best first.
	indexLocs []string
	// generator is the page's generator meta and scripts the absolute URLs
	// of its <script src>, from the raw HTML: how a framework build (Sphinx)
	// is recognised.
	generator string
	scripts   []string
}

// rootPage reads the root page for the site's title, description and
// links, following one in-scope meta refresh (Astro's docs root is a stub
// that refreshes to /en/getting-started/). The page itself leads the links,
// at the URL it was served from and carrying its content when in hand.
func (w *Web) rootPage(ctx context.Context, root webRoot) (rootInfo, error) {
	info, err := w.readPage(ctx, root, root.u)
	if err != nil {
		return info, err
	}
	if info.refresh != nil && len(info.links) == 0 {
		if next, err := w.readPage(ctx, root, info.refresh); err == nil {
			next.title = firstNonEmpty(next.title, info.title)
			next.description = firstNonEmpty(next.description, info.description)
			info = next
		}
	}
	self := webLink{url: info.at, title: info.title, description: info.description, page: info.page}
	info.links = append([]webLink{self}, dropLink(dropLink(info.links, info.at), root.u)...)
	return info, nil
}

// rootNav reads the root page's raw HTML for its links and search index,
// without rendering it: ok is false when the page cannot be read that way
// (a failed fetch, a redirect out of the root, Markdown, a JS shell).
func (w *Web) rootNav(ctx context.Context, root webRoot) (rootInfo, bool) {
	pctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	raw, err := w.fetcher.FetchRaw(pctx, root.u.String())
	cancel()
	if err != nil || !looksLikeHTML(raw.Body, raw.ContentType) {
		return rootInfo{}, false
	}
	at, err := servedFrom(root.u, raw.URL, root.contains)
	if err != nil {
		return rootInfo{}, false
	}
	info := htmlRootInfo(raw.Body, root, at)
	return info, len(info.links) > 0
}

// readPage reads one page for discovery. Raw HTML is parsed for title,
// meta description, refresh target and anchors; raw Markdown is the page.
// HTML with no in-scope anchors (a JS shell), or a failed raw fetch, is
// retried through FetchPage, whose Markdown links are used instead; when
// both fail, FetchPage's error stands, since it ran under the call's
// deadline rather than the probe's. A page served from outside the docs
// root is a scopeError, and one whose raw fetch redirected away is not
// retried.
func (w *Web) readPage(ctx context.Context, root webRoot, u *url.URL) (rootInfo, error) {
	var info rootInfo
	pctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	raw, rawErr := w.fetcher.FetchRaw(pctx, u.String())
	cancel()
	if rawErr == nil {
		info.at, rawErr = servedFrom(u, raw.URL, root.contains)
		var scope *scopeError
		if errors.As(rawErr, &scope) && scope.served != "" {
			return info, rawErr
		}
	}
	if errors.Is(rawErr, errRefusedRedirect) {
		return info, rawErr // FetchPage would only follow it
	}
	switch {
	case rawErr != nil:
	case looksLikeHTML(raw.Body, raw.ContentType):
		info = htmlRootInfo(raw.Body, root, info.at)
	default:
		info = markdownRootInfo(string(raw.Body), "", root, info.at, w.maxPageBytes)
	}
	if rawErr == nil && (info.page != nil || info.refresh != nil || len(info.links) > 0) {
		return info, nil
	}
	page, err := w.fetcher.FetchPage(ctx, u.String())
	var at *url.URL
	if err == nil {
		at, err = servedFrom(u, page.URL, root.contains)
	}
	switch {
	case err == nil:
		md := markdownRootInfo(page.Markdown, firstNonEmpty(info.title, cleanPageTitle(page.Title)), root, at, w.maxPageBytes)
		md.description, md.generator, md.scripts = info.description, info.generator, info.scripts
		md.page.Anchors, md.page.markdown = page.Anchors, false
		return md, nil
	case rawErr != nil:
		return info, err
	}
	return info, nil
}

// markdownRootInfo reads a Markdown page's title, links and content; u is
// where it was served from.
func markdownRootInfo(md, title string, root webRoot, u *url.URL, maxBytes int) rootInfo {
	fmTitle, md := stripFrontmatter(capText(md, maxBytes))
	info := rootInfo{title: firstNonEmpty(title, fmTitle, firstH1(md)), at: u, links: markdownLinks(md, root, u)}
	info.page = &webPage{URL: u.String(), Title: info.title, Markdown: md, markdown: true}
	return info
}

// markdownLinks returns the in-scope page links in a Markdown document
// served from base.
func markdownLinks(md string, root webRoot, base *url.URL) []webLink {
	var links []webLink
	seen := map[string]bool{}
	var fence fenceState
	for _, line := range strings.Split(md, "\n") {
		if fence.step(line) {
			continue
		}
		links = appendMarkdownLinks(links, line, base, root, "", seen, nil)
	}
	return links
}

// htmlRootInfo extracts title, description, refresh target and in-scope
// anchors from an HTML page at u.
func htmlRootInfo(body []byte, root webRoot, u *url.URL) rootInfo {
	info := rootInfo{at: u}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return info
	}
	info.title = cleanPageTitle(collapseSpace(doc.Find("title").First().Text()))
	info.description = collapseSpace(doc.Find(`meta[name="description"]`).AttrOr("content", ""))
	info.generator = metaNamed(doc, "generator")
	base := u
	if href, ok := doc.Find("base[href]").Attr("href"); ok {
		if b, err := u.Parse(href); err == nil {
			base = b
		}
	}
	info.scripts = scriptURLs(doc, base)
	info.indexLocs = searchIndexLocs(body, info.generator, base, root)
	doc.Find("meta[http-equiv]").Each(func(_ int, m *goquery.Selection) {
		if strings.EqualFold(m.AttrOr("http-equiv", ""), "refresh") && info.refresh == nil {
			if target, ok := root.resolve(base, metaRefreshURL(m.AttrOr("content", ""))); ok && linkKey(target) != linkKey(u) {
				info.refresh = target
			}
		}
	})
	seen := map[string]int{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		link, ok := root.resolve(base, a.AttrOr("href", ""))
		if !ok || !isPageURL(link) {
			return
		}
		title := firstNonEmpty(collapseSpace(a.Text()), a.AttrOr("aria-label", ""), a.AttrOr("title", ""))
		if i, ok := seen[linkKey(link)]; ok {
			// A logo or icon link first, the titled nav entry after it.
			info.links[i].title = firstNonEmpty(info.links[i].title, title)
			return
		}
		seen[linkKey(link)] = len(info.links)
		info.links = append(info.links, webLink{url: link, title: title})
	})
	return info
}

// metaNamed returns the space-collapsed content of the first <meta> whose
// name is name, in any case.
func metaNamed(doc *goquery.Document, name string) string {
	content := ""
	doc.Find("meta[name]").EachWithBreak(func(_ int, m *goquery.Selection) bool {
		if strings.EqualFold(strings.TrimSpace(m.AttrOr("name", "")), name) {
			content = collapseSpace(m.AttrOr("content", ""))
			return false
		}
		return true
	})
	return content
}

// scriptURLs returns the absolute web URLs of a page's <script src>
// elements, resolved against base.
func scriptURLs(doc *goquery.Document, base *url.URL) []string {
	var out []string
	doc.Find("script[src]").Each(func(_ int, s *goquery.Selection) {
		if u, err := base.Parse(strings.TrimSpace(s.AttrOr("src", ""))); err == nil && isWebScheme(u.Scheme) {
			out = append(out, u.String())
		}
	})
	return out
}

// metaRefreshURL returns the target of a refresh content value such as
// "0;url=/en/getting-started/" or "0; URL='next.html'", or "".
func metaRefreshURL(content string) string {
	_, rest, ok := strings.Cut(content, ";")
	if !ok {
		return ""
	}
	rest = strings.TrimSpace(rest)
	if len(rest) >= 4 && strings.EqualFold(rest[:4], "url=") {
		rest = rest[4:]
	}
	return strings.Trim(strings.TrimSpace(rest), `'"`)
}

// dropLink removes target (by page key) from links.
func dropLink(links []webLink, target *url.URL) []webLink {
	key := linkKey(target)
	out := links[:0:0]
	for _, l := range links {
		if linkKey(l.url) != key {
			out = append(out, l)
		}
	}
	return out
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func uniqueStrings(values ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
