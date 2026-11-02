// Owner: chunk B (resolve core).

package docs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/publicsuffix"

	"github.com/1broseidon/ketch/httpx"
)

// webMaxInline caps the inline script text a profile keeps.
const webMaxInline = 64 << 10

// profile fetches a candidate docs root and reads it (plan §4 step 5):
// FetchRaw HTML through parseProfile, falling back to FetchPage Markdown
// links for a JS shell. Both fetches share one probeTimeout, so a site
// that hangs costs one probe, not one per attempt. A redirect is kept in
// served, not judged here. The error is the fetch's, ErrNotFound-wrapped
// for a 404 or 410.
func (w *Web) profile(ctx context.Context, u *url.URL) (*siteProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	defer cancel()
	p, err := w.rawProfile(ctx, u)
	if err != nil && (errors.Is(err, ErrNotFound) || errors.Is(err, errRefusedRedirect) || ctx.Err() != nil) {
		return nil, err
	}
	if err == nil && !isShell(p) {
		return p, nil
	}
	// A JS shell, or a raw fetch that failed (bot protection may still let
	// the browser through): read the rendered page instead.
	md, mdErr := w.markdownProfile(ctx, u, p)
	switch {
	case mdErr == nil:
		return md, nil
	case err == nil:
		return p, nil // the shell is all there is
	}
	return nil, err
}

// rawProfile reads u through FetchRaw, following one same-site meta
// refresh from a stub page with no anchors (docs.astro.build/ refreshes to
// /en/getting-started/).
func (w *Web) rawProfile(ctx context.Context, u *url.URL) (*siteProfile, error) {
	body, ct, served, err := w.fetchRawServed(ctx, u)
	if err != nil {
		return nil, err
	}
	if !looksLikeHTML(body, ct) {
		return markdownSiteProfile(string(body), "", u, served), nil
	}
	p := parseProfile(body, u, served)
	if len(p.links) > 0 {
		return p, nil
	}
	target := metaRefreshTarget(body, served)
	if target == nil || scopeHost(target) != scopeHost(served) || linkKey(target) == linkKey(served) {
		return p, nil
	}
	nbody, nct, nserved, err := w.fetchRawServed(ctx, target)
	if err != nil || !looksLikeHTML(nbody, nct) {
		return p, nil
	}
	next := parseProfile(nbody, u, nserved)
	next.title = firstNonEmpty(next.title, p.title)
	next.description = firstNonEmpty(next.description, p.description)
	return next, nil
}

// fetchRawServed is FetchRaw, with the URL the response was served from.
// It runs under the caller's deadline: profile's one probe budget.
func (w *Web) fetchRawServed(ctx context.Context, u *url.URL) ([]byte, string, *url.URL, error) {
	raw, err := w.fetcher.FetchRaw(ctx, u.String())
	if err != nil {
		return nil, "", nil, err
	}
	served, err := servedFrom(u, raw.URL, func(*url.URL) bool { return true })
	if err != nil {
		return nil, "", nil, err
	}
	return raw.Body, raw.ContentType, served, nil
}

// markdownProfile reads u through FetchPage (the scrape pipeline, which
// renders JS shells) and takes its Markdown links, within what is left of
// profile's probe budget. raw, when the raw fetch worked, supplies the
// title and description.
func (w *Web) markdownProfile(ctx context.Context, u *url.URL, raw *siteProfile) (*siteProfile, error) {
	page, err := w.fetcher.FetchPage(ctx, u.String())
	if err != nil {
		return nil, err
	}
	served, err := servedFrom(u, page.URL, func(*url.URL) bool { return true })
	if err != nil {
		return nil, err
	}
	p := markdownSiteProfile(page.Markdown, page.Title, u, served)
	if raw != nil {
		if len(p.links) == 0 {
			return nil, errors.New("the rendered page has no links either")
		}
		p.title = firstNonEmpty(raw.title, p.title)
		p.description = raw.description
	}
	return p, nil
}

// isShell reports a page with no link to anything else on its own site,
// as a JS shell is before it renders. Cloudflare's injected /cdn-cgi/
// links do not count.
func isShell(p *siteProfile) bool {
	page := p.served
	if page == nil {
		page = p.requested
	}
	for _, l := range p.links {
		if scopeHost(l.url) == scopeHost(page) && !strings.HasPrefix(l.url.Path, "/cdn-cgi/") && linkKey(l.url) != linkKey(page) {
			return false
		}
	}
	return true
}

// profileMDLinkRe matches a Markdown link's text and target.
var profileMDLinkRe = regexp.MustCompile(`\[([^\]\n]*)\]\(\s*<?([^)\s>]+)>?(?:\s+["'][^"'\n]*["'])?\s*\)`)

// markdownSiteProfile builds a profile from a Markdown page: its links,
// and title as given (else its first H1).
func markdownSiteProfile(md, title string, requested, served *url.URL) *siteProfile {
	p := &siteProfile{requested: requested, served: served, fromMarkdown: true, meta: map[string]string{}}
	p.title = firstNonEmpty(title, firstH1(md))
	seen := map[string]bool{}
	for _, m := range profileMDLinkRe.FindAllStringSubmatchIndex(md, -1) {
		if m[0] > 0 && md[m[0]-1] == '!' {
			continue
		}
		u, ok := resolveRef(served, md[m[4]:m[5]])
		if !ok || !isWebScheme(u.Scheme) || u.Hostname() == "" || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		p.links = append(p.links, siteLink{url: u, text: collapseSpace(cleanInline(md[m[2]:m[3]]))})
	}
	return p
}

// errRefusedRedirect is a redirect publicHops stopped: one into non-public
// address space. The site it came from is no docs site, and the fetch is
// not retried another way (FetchPage, a browser) that would follow it.
var errRefusedRedirect = errors.New("redirects to a non-public address")

// publicHops returns ctx with a redirect check (httpx.WithRedirectCheck)
// that refuses any hop isPublicWebURL rejects. Fetches for name resolution,
// and for the docs of a root it found, run under it: their URLs come from
// third-party metadata, so a homepage that redirects to
// http://169.254.169.254/ is never followed there. A browser render
// (FetchPage on a JS shell) navigates through Chrome's own network stack,
// which this check does not reach; the scope check on the URL it ends on
// still drops what it returns.
func publicHops(ctx context.Context) context.Context {
	return httpx.WithRedirectCheck(ctx, func(u *url.URL) error {
		if !isPublicWebURL(u) {
			return fmt.Errorf("%w: %s", errRefusedRedirect, u.Redacted())
		}
		return nil
	})
}

// metaRefreshTarget returns a page's <meta http-equiv="refresh"> target,
// resolved against base, or nil.
func metaRefreshTarget(body []byte, base *url.URL) *url.URL {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var target *url.URL
	doc.Find("meta[http-equiv]").EachWithBreak(func(_ int, m *goquery.Selection) bool {
		if !strings.EqualFold(m.AttrOr("http-equiv", ""), "refresh") {
			return true
		}
		if u, ok := resolveRef(base, metaRefreshURL(m.AttrOr("content", ""))); ok && isWebScheme(u.Scheme) {
			target = u
		}
		return false
	})
	return target
}

// parseProfile reads an HTML page fetched from requested and served from
// served: title, description, meta, every anchor, scripts and inline
// script text. Links resolve against served (or a <base href>).
func parseProfile(body []byte, requested, served *url.URL) *siteProfile {
	p := &siteProfile{requested: requested, served: served, meta: map[string]string{}}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return p
	}
	base := served
	if base == nil {
		base = requested
	}
	if href, ok := doc.Find("base[href]").First().Attr("href"); ok {
		if b, err := base.Parse(strings.TrimSpace(href)); err == nil {
			base = b
		}
	}
	p.title = collapseSpace(doc.Find("title").First().Text())
	doc.Find("meta").Each(func(_ int, m *goquery.Selection) {
		key := strings.ToLower(firstNonEmpty(m.AttrOr("name", ""), m.AttrOr("property", "")))
		if _, ok := p.meta[key]; key != "" && !ok {
			p.meta[key] = strings.TrimSpace(m.AttrOr("content", ""))
		}
	})
	p.description = firstNonEmpty(p.meta["description"], p.meta["og:description"])
	p.links = anchorLinks(doc, base)
	var inline strings.Builder
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok {
			if u, ok := resolveRef(base, src); ok {
				p.scripts = append(p.scripts, u.String())
			}
			return
		}
		if inline.Len() < webMaxInline {
			inline.WriteString(s.Text())
			inline.WriteByte('\n')
		}
	})
	p.inline = capText(inline.String(), webMaxInline)
	return p
}

// anchorLinks returns a document's web anchors, one per URL, in order.
func anchorLinks(doc *goquery.Document, base *url.URL) []siteLink {
	var links []siteLink
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		u, ok := resolveRef(base, a.AttrOr("href", ""))
		if !ok || !isWebScheme(u.Scheme) || u.Hostname() == "" || seen[u.String()] {
			return
		}
		seen[u.String()] = true
		text := a.Clone()
		text.Find("style, script").Remove()
		links = append(links, siteLink{url: u, text: collapseSpace(firstNonEmpty(text.Text(), a.AttrOr("aria-label", ""), a.AttrOr("title", "")))})
	})
	return links
}

// githubReserved are github.com first path segments that are not owners.
var githubReserved = map[string]bool{
	"about": true, "account": true, "advisories": true, "apps": true, "codespaces": true, "collections": true,
	"contact": true, "customer-stories": true, "dashboard": true, "enterprise": true, "events": true,
	"explore": true, "features": true, "github-copilot": true, "issues": true, "join": true, "login": true,
	"marketplace": true, "new": true, "notifications": true, "organizations": true, "orgs": true,
	"pricing": true, "pulls": true, "readme": true, "resources": true, "search": true, "security": true,
	"settings": true, "signup": true, "site": true, "solutions": true, "sponsors": true, "team": true,
	"topics": true, "trending": true, "users": true,
}

// githubRepoOf returns the repository a github.com URL points into.
func githubRepoOf(u *url.URL) (repoKey, bool) {
	if u == nil {
		return repoKey{}, false
	}
	if host := strings.ToLower(u.Hostname()); host != "github.com" && host != "www.github.com" {
		return repoKey{}, false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) < 2 {
		return repoKey{}, false
	}
	owner, name := strings.ToLower(segs[0]), strings.ToLower(strings.TrimSuffix(segs[1], ".git"))
	if owner == "" || name == "" || githubReserved[owner] {
		return repoKey{}, false
	}
	return repoKey{owner: owner, name: name}, true
}

// repoLinks returns the GitHub repositories the page links to, in page
// order, one each.
func repoLinks(p *siteProfile) []repoKey {
	var out []repoKey
	seen := map[repoKey]bool{}
	for _, l := range p.links {
		if k, ok := githubRepoOf(l.url); ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// docsNavTexts are the link texts that name a site's docs.
var docsNavTexts = map[string]bool{
	"docs": true, "doc": true, "documentation": true, "guide": true, "guides": true, "learn": true,
	"get started": true, "getting started": true, "api": true, "api reference": true, "api docs": true,
	"reference": true, "read the docs": true, "user guide": true, "manual": true,
}

// docsSegments are the first path segments that hold a site's docs.
var docsSegments = map[string]bool{"docs": true, "doc": true, "documentation": true, "manual": true}

// docsSegment returns the path's first segment when it is a docs segment.
func docsSegment(p string) string {
	first, _, _ := strings.Cut(strings.TrimPrefix(p, "/"), "/")
	if docsSegments[strings.ToLower(first)] {
		return first
	}
	return ""
}

// navText is a link text reduced for comparison: lower case, with the
// arrows and punctuation around it dropped.
func navText(s string) string {
	s = strings.TrimFunc(strings.ToLower(collapseSpace(s)), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return collapseSpace(s)
}

// docsNavLinks returns the page's links that lead to its docs: text Docs,
// Documentation, Guide, Learn, Get started, API or Reference; a docs. host
// on the page's registrable domain; or a /docs path on that domain.
func docsNavLinks(p *siteProfile) []siteLink {
	page := p.served
	if page == nil {
		page = p.requested
	}
	domain := registrableDomain(page.Hostname())
	var out []siteLink
	seen := map[string]bool{}
	for _, l := range p.links {
		host := strings.ToLower(l.url.Hostname())
		if isNonDocsHost(host) || !isPublicWebURL(l.url) || linkKey(l.url) == linkKey(page) || seen[linkKey(l.url)] {
			continue
		}
		related := registrableDomain(host) == domain
		ok := docsNavTexts[navText(l.text)] ||
			related && strings.HasPrefix(host, "docs.") && scopeHost(l.url) != scopeHost(page) ||
			related && docsSegment(l.url.Path) != ""
		if ok {
			seen[linkKey(l.url)] = true
			out = append(out, l)
		}
	}
	return out
}

// docsRootsFor turns a homepage and its profile into candidate docs roots
// (plan §4 step 4d): each docs nav link's root (another host's "/", a
// same-host docs/doc/documentation/manual first segment, else the host
// root), and the homepage itself when it looks like docs. Links to the
// registries' docs hosts are not roots.
//
// Roots on another host or under a docs segment are preferred: only when
// a page has none do its other nav links (react.dev's /learn) make the
// host root a candidate, so nextjs.org's /learn course does not stand
// beside its /docs.
func docsRootsFor(homepage *url.URL, p *siteProfile) []*url.URL {
	page := p.served
	if page == nil {
		page = homepage
	}
	var specific, general []*url.URL
	for _, l := range docsNavLinks(p) {
		u := l.url
		switch seg := docsSegment(u.Path); {
		case isRegistryHost(u):
			// docs.rs or pkg.go.dev: only a package's own registry docs
			// count, never a link to the registry.
		case scopeHost(u) != scopeHost(page):
			specific = append(specific, hostRootFor(u))
		case seg != "":
			specific = append(specific, &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/" + seg + "/"})
		default:
			general = append(general, &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"})
		}
	}
	if looksLikeDocs(page, p) {
		// The page is docs itself: its section links (API, Guide) are its
		// own navigation, not a pointer to the host's root.
		if self, ok := homeRoot(homepage, page); ok {
			return uniqueRoots(append([]*url.URL{self.u}, specific...))
		}
	}
	if len(specific) == 0 {
		return uniqueRoots(general)
	}
	return uniqueRoots(specific)
}

// homeRoot is the root a homepage stands for as a docs site itself: the
// homepage, unless it was served from outside it (a redirect elsewhere),
// then where it was served from. A refresh to a page inside it
// (saurvs.github.io/astro-rust/ to astro/index.html) keeps the homepage.
func homeRoot(homepage, served *url.URL) (webRoot, bool) {
	if home, err := parseWebRoot(homepage.String()); err == nil && (served == nil || home.contains(served)) {
		return home, true
	}
	if served == nil {
		return webRoot{}, false
	}
	r, err := parseWebRoot(served.String())
	return r, err == nil
}

// hostRootFor is the docs root a link to another host stands for: the
// host's root, or the project directory of a GitHub or GitLab Pages site.
func hostRootFor(u *url.URL) *url.URL {
	host := strings.ToLower(u.Hostname())
	root := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}
	if strings.HasSuffix(host, ".github.io") || strings.HasSuffix(host, ".gitlab.io") {
		first, _, more := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
		if first != "" && (more || !isPageFile(first)) {
			root.Path = "/" + first + "/"
		}
	}
	return root
}

// uniqueRoots drops repeated roots, compared as linkKey does.
func uniqueRoots(roots []*url.URL) []*url.URL {
	var out []*url.URL
	seen := map[string]bool{}
	for _, r := range roots {
		if k := linkKey(r); !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out
}

// docsGenerators are generator meta values (lower case, any part) of
// documentation tools.
var docsGenerators = []string{"sphinx", "mkdocs", "docusaurus", "vitepress", "starlight", "mintlify", "gitbook",
	"docsify", "nextra", "fumadocs", "zensical", "antora", "typedoc", "rustdoc", "read the docs"}

// looksLikeDocs reports a page that is itself documentation: a docs
// generator or Read the Docs meta, Sphinx's options script, a docs. host
// or docs path, or a title that says so.
func looksLikeDocs(page *url.URL, p *siteProfile) bool {
	gen := strings.ToLower(p.meta["generator"])
	for _, g := range docsGenerators {
		if strings.Contains(gen, g) {
			return true
		}
	}
	if p.meta["readthedocs-project-slug"] != "" {
		return true
	}
	for _, s := range p.scripts {
		if strings.Contains(s, "documentation_options.js") {
			return true
		}
	}
	if strings.HasPrefix(strings.ToLower(page.Hostname()), "docs.") || docsSegment(page.Path) != "" {
		return true
	}
	for _, t := range strings.FieldsFunc(strings.ToLower(p.title), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if t == "docs" || t == "documentation" {
			return true
		}
	}
	return false
}

// registrableDomain is host's eTLD+1 by the public suffix list, private
// section included (github.io and readthedocs.io are suffixes); host
// itself when it has none.
func registrableDomain(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if host == "" || net.ParseIP(host) != nil {
		return host
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return d
}

// nonDocsDomains are hosts (with their subdomains) that never serve a
// project's docs site: code forges, package registries, chat and social
// sites.
var nonDocsDomains = []string{
	"github.com", "githubusercontent.com", "gitlab.com", "bitbucket.org", "codeberg.org", "sourceforge.net", "sr.ht",
	"npmjs.com", "npmjs.org", "npm.im", "pypi.org", "pypi.python.org", "crates.io", "lib.rs", "libraries.io",
	"rubygems.org", "packagist.org", "nuget.org",
	"discord.gg", "discord.com", "discordapp.com", "gitter.im", "slack.com", "t.me", "matrix.to",
	"twitter.com", "x.com", "facebook.com", "linkedin.com", "youtube.com", "youtu.be", "reddit.com",
	"instagram.com", "mastodon.social", "bsky.app", "medium.com", "stackoverflow.com",
	"opencollective.com", "patreon.com", "ko-fi.com", "buymeacoffee.com", "liberapay.com",
}

// isNonDocsHost reports a host that is never a project's docs site: code
// forges, package registries, chat and social sites.
func isNonDocsHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, d := range nonDocsDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// hostKind names what a non-docs host is, for a rejection reason.
func hostKind(host string) string {
	host = strings.ToLower(host)
	switch {
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return "a GitHub page"
	case strings.Contains(host, "gitlab") || strings.Contains(host, "bitbucket") || strings.Contains(host, "codeberg") || strings.Contains(host, "sourceforge"):
		return "a code forge page"
	case strings.Contains(host, "npmjs") || strings.Contains(host, "pypi") || host == "crates.io" || host == "lib.rs" || host == "libraries.io":
		return "a package registry page"
	}
	return "a chat or social site"
}

// privateSuffixes are name suffixes that are never public web hosts.
var privateSuffixes = []string{".localhost", ".local", ".internal", ".lan", ".home.arpa", ".intranet", ".corp"}

// isPublicWebURL reports an absolute http(s) URL with a public-looking
// host: no IP literal (an IPv6 one with a zone, [::ffff:169.254.169.254%25eth0],
// included: no host name holds a ':' or '%'), no localhost or localhost.*
// name (localhost.localdomain), no single-label host, and no .local,
// .internal or .lan name. Syntactic only; it never resolves DNS. Every URL
// taken from third-party metadata passes it before it is fetched, and
// every redirect hop of a fetch for name resolution too (publicHops).
func isPublicWebURL(u *url.URL) bool {
	if u == nil || !isWebScheme(u.Scheme) || u.User != nil || u.Opaque != "" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || strings.ContainsAny(host, ":%[]") || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return false
	}
	if host == "localhost" || strings.HasPrefix(host, "localhost.") {
		return false
	}
	for _, s := range privateSuffixes {
		if strings.HasSuffix(host, s) {
			return false
		}
	}
	// A numeric or hex last label is an IP address in disguise (127.1,
	// 0x7f.1) as some resolvers and browsers read it; no TLD looks so.
	last := host[strings.LastIndexByte(host, '.')+1:]
	return last != "" && !strings.HasPrefix(last, "0x") && strings.Trim(last, "0123456789") != ""
}
