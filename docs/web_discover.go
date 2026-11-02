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
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// webSite is what discovery found for one root.
type webSite struct {
	pages []webPage // full content from llms-full.txt; no per-page fetches needed
	links []webLink // candidate pages, in discovery order
	tried []string  // sources consulted, for the not-found message
}

// discover finds the site's pages, cheapest source first:
//
//  1. llms.txt under the root path, then at the origin. A usable sibling
//     llms-full.txt (at most maxFullBytes) supplies the content outright.
//  2. Sitemaps: robots.txt Sitemap: lines, else sitemap.xml and
//     sitemap-index.xml under the root path and at the origin; indexes are
//     followed webSitemapDepth levels deep, at most webMaxSitemaps files
//     and webMaxURLs URLs, skipping other locales' sitemaps, all within
//     sitemapBudget.
//  3. Links on the root page (and the root page itself, following one meta
//     refresh), the last resort, merged with every page of the Sphinx
//     build the root page belongs to (its searchindex.js).
//
// A sitemap that lists only the root or version roots (Read the Docs lists
// /en/latest/ and /en/stable/) does not stop discovery, and registry docs
// hosts (pkg.go.dev, docs.rs) go straight to the root page. Only in-scope
// pages are kept from any source. Discovery files are probes: their
// absence is expected and never an error. The root page is not, so its
// fetch error is returned when nothing else turned up.
func (w *Web) discover(ctx context.Context, root webRoot) (webSite, error) {
	var site webSite
	if w.discoverListed(ctx, root, &site) {
		return site, nil
	}
	site.tried = append(site.tried, "links on "+root.String())
	info, err := w.rootPage(ctx, root)
	if err != nil {
		return site, rootPageFailure(ctx, root, &site, err)
	}
	links := w.withSphinxLinks(ctx, root, info, &site.tried)
	if len(links) == 1 && info.page != nil && strings.TrimSpace(info.page.Markdown) == "" {
		return site, nil // an empty root page with no links: nothing to search
	}
	site.links = links
	return site, nil
}

// discoverListed tries the sources that list a site's pages, llms.txt
// (with llms-full.txt) then sitemaps, as far as root's host is worth
// probing, and reports whether one answered with pages.
func (w *Web) discoverListed(ctx context.Context, root webRoot, site *webSite) bool {
	probes := hostDiscovery(root)
	if probes.llms {
		llms := w.findLLMSTxt(ctx, root, &site.tried)
		if llms.url != nil {
			site.pages = w.fullText(ctx, root, llms, &site.tried)
			if len(site.pages) > 0 || len(llms.links) > 0 {
				site.links = llms.links
				return true
			}
		}
	}
	if !probes.sitemaps {
		return false
	}
	if links := w.sitemapLinks(ctx, root, &site.tried); usefulSitemap(links, root) {
		site.links = links
		return true
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

// llmsTxt is a parsed llms.txt.
type llmsTxt struct {
	url         *url.URL // where it was served from; nil when absent
	title       string   // the H1
	description string   // the first blockquote
	links       []webLink
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
		pages = append(pages, webPage{URL: pageURL, Title: title, Markdown: capText(chunk.body, w.maxPageBytes)})
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

// metaLineRe matches the per-page metadata generators put by the H1:
// "Source: https://…" (Mintlify), "URL: /docs/x" (Fumadocs), "url: /x.md"
// (VitePress frontmatter).
var metaLineRe = regexp.MustCompile(`(?i)^\s*(url|source|source_url|link)\s*:\s*<?(\S+?)>?\s*$`)

// splitFullText splits llms-full.txt into pages at H1 headings outside code
// fences. Metadata lines right after an H1, or in a --- block right before
// it, name the page's URL and are removed from the body.
func splitFullText(body string) []fullChunk {
	var chunks []fullChunk
	cur := &fullChunk{}
	var lines []string
	var fence fenceState
	flush := func() {
		cur.body = takeLeadingMeta(cur, lines)
		chunks = append(chunks, *cur)
	}
	for _, line := range strings.Split(body, "\n") {
		if fence.step(line) {
			lines = append(lines, line)
			continue
		}
		h, ok := parseATX(line)
		if !ok || h.level != 1 {
			lines = append(lines, line)
			continue
		}
		var pre []string
		lines, pre = popFrontmatter(lines)
		flush()
		cur, lines = &fullChunk{title: h.text}, nil
		for _, l := range pre {
			if m := metaLineRe.FindStringSubmatch(l); m != nil {
				cur.refs = append(cur.refs, m[2])
			}
		}
	}
	flush()
	if chunks[0].title == "" && chunks[0].body == "" {
		chunks = chunks[1:]
	}
	return chunks
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
	return strings.TrimSpace(strings.Join(lines[i:], "\n"))
}

// popFrontmatter removes a trailing "---\nkey: value\n---" block (blank
// lines aside) from lines and returns it.
func popFrontmatter(lines []string) ([]string, []string) {
	end := len(lines) - 1
	for end >= 0 && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	if end < 1 || strings.TrimSpace(lines[end]) != "---" {
		return lines, nil
	}
	for start := end - 1; start >= 0 && end-start <= 12; start-- {
		if strings.TrimSpace(lines[start]) == "---" {
			return lines[:start], lines[start+1 : end]
		}
		if !strings.Contains(lines[start], ":") {
			return lines, nil
		}
	}
	return lines, nil
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
	info.page = &webPage{URL: u.String(), Title: info.title, Markdown: md}
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
	doc.Find("meta[http-equiv]").Each(func(_ int, m *goquery.Selection) {
		if strings.EqualFold(m.AttrOr("http-equiv", ""), "refresh") && info.refresh == nil {
			if target, ok := root.resolve(base, metaRefreshURL(m.AttrOr("content", ""))); ok && linkKey(target) != linkKey(u) {
				info.refresh = target
			}
		}
	})
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		link, ok := root.resolve(base, a.AttrOr("href", ""))
		if !ok || !isPageURL(link) || seen[linkKey(link)] {
			return
		}
		seen[linkKey(link)] = true
		title := firstNonEmpty(collapseSpace(a.Text()), a.AttrOr("aria-label", ""), a.AttrOr("title", ""))
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
