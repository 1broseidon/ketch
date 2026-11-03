package docs

import (
	"cmp"
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Keyless identification and evidence (design/docs-keyless.md, Pipeline 2
// and 3): whether a candidate is a documentation site at all, judged from
// what the site publishes itself, and the links a site carries to the
// project's own repository and registry pages, which tie it to a project
// without asking GitHub.

// pkgRef names a package by registry and name.
type pkgRef struct {
	eco  ecosystem
	name string
}

// registryPackageOf reads a link to a package's own registry page (npm,
// PyPI, crates.io, lib.rs) or registry-hosted docs (docs.rs, pkg.go.dev):
// the zero pkgRef for anything else.
func registryPackageOf(u *url.URL) pkgRef {
	if u == nil || !isWebScheme(u.Scheme) {
		return pkgRef{}
	}
	segs := pathSegments(u.Path)
	if len(segs) == 0 {
		return pkgRef{}
	}
	if read := registryPages[scopeHost(u)]; read != nil {
		return read(segs)
	}
	return pkgRef{}
}

// registryPages read a registry host's page path (its segments, at least
// one) as the package it is about.
var registryPages = map[string]func(segs []string) pkgRef{
	"npmjs.com": npmPage, "npmjs.org": npmPage,
	"pypi.org": pypiPage, "pypi.python.org": pypiPage,
	"crates.io": cratesPage, "lib.rs": cratesPage,
	"docs.rs": func(segs []string) pkgRef {
		if segs[0] == "crate" && len(segs) > 1 {
			return pkgRef{ecoCrates, normalizeName(ecoCrates, segs[1])}
		}
		return pkgRef{ecoCrates, normalizeName(ecoCrates, segs[0])}
	},
	"pkg.go.dev": func(segs []string) pkgRef {
		module, _, _ := strings.Cut(strings.Join(segs, "/"), "@")
		if !isModulePath(module) {
			return pkgRef{}
		}
		return pkgRef{ecoGo, module}
	},
}

func npmPage(segs []string) pkgRef {
	if segs[0] != "package" || len(segs) < 2 {
		return pkgRef{}
	}
	name := segs[1]
	if strings.HasPrefix(name, "@") && len(segs) > 2 {
		name += "/" + segs[2]
	}
	return pkgRef{ecoNPM, strings.ToLower(name)}
}

func pypiPage(segs []string) pkgRef {
	if (segs[0] != "project" && segs[0] != "pypi") || len(segs) < 2 {
		return pkgRef{}
	}
	return pkgRef{ecoPyPI, normalizeName(ecoPyPI, segs[1])}
}

func cratesPage(segs []string) pkgRef {
	if segs[0] != "crates" || len(segs) < 2 {
		return pkgRef{}
	}
	return pkgRef{ecoCrates, normalizeName(ecoCrates, segs[1])}
}

// linksPackage reports a link among links to the registry page of one of
// p's packages: the site names the package as its own. A Go package's
// pkg.go.dev page counts for the module or any package below it.
func linksPackage(links []*url.URL, p *project) (pkgInfo, bool) {
	for _, u := range links {
		ref := registryPackageOf(u)
		if ref.eco == "" {
			continue
		}
		for _, pkg := range p.pkgs {
			if pkg.eco != ref.eco {
				continue
			}
			switch {
			case pkg.eco == ecoGo && (ref.name == pkg.name || strings.HasPrefix(ref.name, pkg.name+"/")):
				return pkg, true
			case pkg.eco == ecoNPM && ref.name == strings.ToLower(pkg.name):
				return pkg, true
			case pkg.eco != ecoGo && pkg.eco != ecoNPM && ref.name == normalizeName(pkg.eco, pkg.name):
				return pkg, true
			}
		}
	}
	return pkgInfo{}, false
}

// profileLinkURLs are a profile's link targets.
func profileLinkURLs(prof *siteProfile) []*url.URL {
	out := make([]*url.URL, 0, len(prof.links))
	for _, l := range prof.links {
		out = append(out, l.url)
	}
	return out
}

// packageEvidence is the edge from a site to one of p's packages: the
// page (or its llms.txt manifest, manifest) links to the package's own
// registry page. A link to registry-hosted docs from a site that is not
// itself registry-hosted counts the same: tokio.rs links docs.rs/tokio.
func packageEvidence(root webRoot, links []*url.URL, p *project, where string) []evidence {
	if indexHosted(root.u) {
		return nil
	}
	pkg, ok := linksPackage(links, p)
	if !ok {
		return nil
	}
	return []evidence{{kind: evLinksPackage, detail: where + " links to the " + pkgLabel(pkg) + " package page"}}
}

// manifestRepoLinks are the GitHub repositories an llms.txt manifest
// links to, in order, one each.
func manifestRepoLinks(llms llmsTxt) []repoKey {
	var out []repoKey
	seen := map[repoKey]bool{}
	for _, u := range llms.identity {
		if k, ok := githubRepoOf(u); ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// manifestEvidenceFrom is what a root's llms.txt manifest adds for a keyless
// resolution that the root's page did not show: a link to p's repository
// or to one of its packages' registry pages, and the manifest's H1 naming
// the project (weak, never an edge). It is read only while the root still
// lacks a second edge, so most roots cost no extra request.
func manifestEvidenceFrom(llms llmsTxt, root webRoot, p *project, have []evidence) []evidence {
	if indexHosted(root.u) || llms.url == nil {
		return nil
	}
	var evs []evidence
	if !hasEvidence(have, evLinksRepo) {
		for _, k := range manifestRepoLinks(llms) {
			if p.isRepo(k) {
				evs = append(evs, evidence{kind: evLinksRepo, detail: "its llms.txt links to github.com/" + k.String()})
				break
			}
		}
	}
	if !hasEvidence(have, evLinksPackage) {
		evs = append(evs, packageEvidence(root, llms.identity, p, "its llms.txt")...)
	}
	if ev, ok := siteNamesEvidence(p, "llms.txt", llms.title); ok {
		evs = append(evs, ev)
	}
	return evs
}

// relatedHostEvidence is the DNS edge for a root the metadata named, in a
// keyless resolution: two different URLs the owner published (a
// documentation URL, a homepage) lead to the root or sit on its registrable
// domain. numpy.org/doc/ is PyPI numpy's documentation and the docs link
// of its homepage numpy.org; docs.pydantic.dev is beside pydantic.dev.
// One URL alone (a homepage that is the docs, or links to them) is the
// metadata edge only, however many registries repeat it.
func relatedHostEvidence(root webRoot, p *project, c rootCandidate) []evidence {
	domain := registrableDomain(root.u.Hostname())
	if isNonDocsHost(root.u.Hostname()) || indexHosted(root.u) {
		return nil
	}
	witnesses := map[string]bool{}
	var witness authURL
	for _, a := range ownerURLs(p) {
		k := linkKey(a.url)
		switch {
		case isRegistryHost(a.url):
		case slices.Contains(c.namedBy, k):
			witnesses[k] = true
		case registrableDomain(a.url.Hostname()) == domain:
			witnesses[k] = true
			witness = a
		}
	}
	if len(witnesses) < 2 {
		return nil
	}
	if witness.url == nil {
		return []evidence{{kind: evRelatedDomain, detail: "named by two of " + p.label() + "'s own URLs"}}
	}
	return []evidence{{kind: evRelatedDomain, detail: "on the domain of " + witness.src + " " + scopeHost(witness.url)}}
}

// ownerURLs are the URLs p's metadata publishes, for the DNS edge: its
// docs URLs and homepages (project.authoritative), then the others it
// lists (a PyPI Download or Tracker URL), each the owner's word that its
// host is theirs. A README's links are not among them: a README links to
// dependencies and sponsors too.
func ownerURLs(p *project) []authURL {
	out := p.authoritative()
	for _, pkg := range p.pkgs {
		for _, raw := range pkg.otherURLs() {
			if u, reason := checkMetaURL(raw); reason == "" {
				out = append(out, authURL{url: u, src: pkgLabel(pkg) + " project URL"})
			}
		}
	}
	return out
}

// docsGeneratorMarkers are asset and script markers of documentation
// frameworks that publish no generator meta (or whose meta a site strips).
var docsGeneratorMarkers = []string{
	"/assets/js/runtime~main", "docusaurus", // Docusaurus
	"/_static/documentation_options.js", "/_static/searchtools.js", "searchindex.js", // Sphinx
	"/search/search_index.json", "mkdocs", "/assets/javascripts/bundle.", // MkDocs / Material
	"mintlify", "/_mintlify/", // Mintlify
	"/_astro/", "pagefind", // Starlight
	"/assets/chunks/", "vitepress", // VitePress
	"gitbook",            // GitBook
	"fumadocs", "nextra", // Fumadocs, Nextra
	"/static.files/", "rustdoc", // rustdoc
	"docsearch", "algolia", // a docs search widget
}

// siteIdentity says how a page identifies itself as documentation: by an
// llms.txt manifest (strongest, checked by the caller), a docs framework's
// fingerprint (generator meta, Read the Docs or Sphinx markers, framework
// assets), or a docs structure (a docs host or path, a docs title; weak,
// not enough alone). "" when it shows nothing of the sort.
func siteIdentity(page *url.URL, prof *siteProfile) string {
	gen := strings.ToLower(prof.meta["generator"])
	for _, g := range docsGenerators {
		if strings.Contains(gen, g) {
			return "framework"
		}
	}
	if prof.meta["readthedocs-project-slug"] != "" || prof.meta["docsearch:language"] != "" || prof.meta["docsearch:version"] != "" {
		return "framework"
	}
	if _, ok := detectSphinx(page, gen, prof.scripts); ok {
		return "framework"
	}
	marked := 0
	for _, s := range prof.scripts {
		ls := strings.ToLower(s)
		for _, m := range docsGeneratorMarkers {
			if strings.Contains(ls, m) {
				marked++
				break
			}
		}
	}
	if marked > 0 && looksLikeDocs(page, prof) {
		return "framework"
	}
	if looksLikeDocs(page, prof) || len(docsNavLinks(prof)) > 0 && docsSegment(page.Path) != "" {
		return "structure"
	}
	return ""
}

// keylessEvidence completes a root's evidence for a keyless resolution and
// applies its rule (keylessVerified): registry-hosted docs for the exact
// package stand alone; anything else needs two independent edges. The DNS
// edge is added here for a root the metadata named (relatedHostEvidence);
// while a root is one edge short, the site's home page, Read the Docs'
// record and the root's llms.txt are read for the site-link edge, in that
// order, each only if still needed. A web search hit must also identify
// itself as documentation, by an llms.txt manifest or a docs framework's
// fingerprint: a blog, a Q&A page or an aggregator ranking first in search
// fails here.
func (w *Web) keylessEvidence(ctx context.Context, root webRoot, prof *siteProfile, p *project, c rootCandidate, evs []evidence) ([]evidence, error) {
	if c.registry {
		return evs, nil
	}
	if c.search {
		if a, ok := ownURL(root, p); ok {
			// The hit is the project's own homepage, which its metadata
			// already led to docs from (or found to be docs itself).
			return nil, rejectf("search hit %s is %s, already followed", root, a.src)
		}
	} else {
		evs = append(evs, relatedHostEvidence(root, p, c)...)
	}
	evs = w.readmeFirst(ctx, root, p, evs)
	evs = w.siteLinkEvidence(ctx, root, prof, p, evs)
	identity := siteIdentity(root.u, prof)
	if oneShort(evs) || c.search && keylessVerified(evs) && identity != "framework" {
		if llms := w.cachedLLMS(ctx, root); llms.url != nil {
			evs = append(evs, manifestEvidenceFrom(llms, root, p, evs)...)
			identity = "llms.txt"
		}
	}
	if oneShort(evs) && !hasEdge(evs, edgeMeta) {
		// A README read with a request only for a root it would verify.
		evs = append(evs, w.readmeEvidence(ctx, root, p, true)...)
	}
	evs = uniqueEvidence(evs)
	if !keylessVerified(evs) {
		n, _ := edges(evs)
		return nil, rejectf("%s has %d of the %d independent edges to %s a keyless resolution needs (metadata naming it, a link to its repository or package, a shared domain)", root, n, minKeylessEdges, p.label())
	}
	if c.search && identity != "framework" && identity != "llms.txt" {
		return nil, rejectf("search hit %s does not identify itself as documentation (no llms.txt, no docs framework)", root)
	}
	return evs, nil
}

// hasEdge reports evidence of edge class c.
func hasEdge(evs []evidence, c edgeClass) bool {
	return slices.ContainsFunc(evs, func(e evidence) bool { return edgeOf[e.kind] == c })
}

// readmeFirst adds the README edge (readmeEvidence) a root without the
// metadata edge can have from a README the registry already sent, before
// the site-link edge is sought: no request.
func (w *Web) readmeFirst(ctx context.Context, root webRoot, p *project, evs []evidence) []evidence {
	if keylessVerified(evs) || hasEdge(evs, edgeMeta) {
		return evs
	}
	return append(evs, w.readmeEvidence(ctx, root, p, false)...)
}

// ownerNames reports a candidate root the project's owner names: its
// metadata (directly or through its homepage), or a README the registry
// sent.
func (w *Web) ownerNames(ctx context.Context, root webRoot, p *project, c rootCandidate) bool {
	return hasEdge(c.evidence, edgeMeta) || len(w.readmeEvidence(ctx, root, p, false)) > 0
}

// oneShort reports evidence one edge short of keylessVerified.
func oneShort(evs []evidence) bool {
	n, registry := edges(evs)
	return !registry && n == minKeylessEdges-1
}

// siteLinkEvidence adds the site-link edge a root one edge short lacks,
// from the site's home page, else Read the Docs' record.
func (w *Web) siteLinkEvidence(ctx context.Context, root webRoot, prof *siteProfile, p *project, evs []evidence) []evidence {
	if oneShort(evs) && !hasEvidence(evs, evLinksRepo) && !hasEvidence(evs, evLinksPackage) {
		evs = append(evs, w.homeLinkEvidence(ctx, root, p)...)
	}
	if oneShort(evs) {
		evs = append(evs, w.rtdRepoEvidence(ctx, root, prof, p)...)
	}
	return evs
}

// ownURL is the URL p's metadata publishes that is root itself.
func ownURL(root webRoot, p *project) (authURL, bool) {
	for _, a := range p.authoritative() {
		if sameRoot(a.url, root.u) {
			return a, true
		}
	}
	return authURL{}, false
}

// rtdRepoEvidence is the site-link edge Read the Docs gives a site it
// builds: the repository its project records, when that is p's.
func (w *Web) rtdRepoEvidence(ctx context.Context, root webRoot, prof *siteProfile, p *project) []evidence {
	if prof.meta["readthedocs-project-slug"] == "" {
		return nil
	}
	vs, err := w.rtdVersions(ctx, root, prof)
	if err != nil {
		return nil
	}
	if k, ok := repoOfURL(vs.repoURL); ok && p.isRepo(k) {
		return []evidence{{kind: evRTDRepo, detail: "Read the Docs records github.com/" + k.String()}}
	}
	return nil
}

// homeLinkEvidence is the site-link edge read from the site's own home
// page when the docs root is below it and does not link back itself
// (svelte.dev/docs/ under svelte.dev, which links to sveltejs/svelte): one
// host is one site. Failing that, from the home page of the registrable
// domain a docs subdomain belongs to (docs.cypress.io, a JS shell with no
// links, under cypress.io, which links to cypress-io/cypress): whoever
// holds a domain holds its subdomains. Not on a shared host where the root
// page belongs to someone else than the project below it (a GitHub or
// GitLab Pages user site); the public suffix list makes each such user
// site a registrable domain of its own (user.github.io), so a domain's
// home is never the hosting service's.
func (w *Web) homeLinkEvidence(ctx context.Context, root webRoot, p *project) []evidence {
	if indexHosted(root.u) {
		return nil
	}
	host := strings.ToLower(root.u.Hostname())
	var homes []*url.URL
	if root.prefix != "/" && !strings.HasSuffix(host, ".github.io") && !strings.HasSuffix(host, ".gitlab.io") {
		homes = append(homes, &url.URL{Scheme: root.u.Scheme, Host: root.u.Host, Path: "/"})
	}
	if domain := registrableDomain(host); domain != "" && domain != strings.TrimPrefix(host, "www.") && strings.Contains(domain, ".") {
		homes = append(homes, &url.URL{Scheme: "https", Host: domain, Path: "/"})
	}
	for _, home := range homes {
		if evs := w.homePageLinks(ctx, root, home, p); evs != nil {
			return evs
		}
	}
	return nil
}

// homePageLinks is the site-link edge home's page gives root: a link to
// p's repository or package page, read only when the page is served from
// home's host, or, for a domain's home, from within that domain.
func (w *Web) homePageLinks(ctx context.Context, root webRoot, home *url.URL, p *project) []evidence {
	prof, err := w.cachedProfile(ctx, home)
	if err != nil || prof.served == nil {
		return nil
	}
	if scopeHost(prof.served) != scopeHost(home) && registrableDomain(prof.served.Hostname()) != registrableDomain(home.Hostname()) {
		return nil
	}
	where := "its home page " + scopeHost(home)
	if scopeHost(home) != scopeHost(root.u) {
		where = "the home page of its domain " + scopeHost(home)
	}
	for _, k := range repoLinks(prof) {
		if p.isRepo(k) {
			return []evidence{{kind: evLinksRepo, detail: where + " links to github.com/" + k.String()}}
		}
	}
	return packageEvidence(root, profileLinkURLs(prof), p, where)
}

// readmeURLRe matches an absolute web URL in README text, whether
// Markdown, reStructuredText or HTML.
var readmeURLRe = regexp.MustCompile("https?://[^\\s<>\"'()\\[\\]{}`|\\\\]+")

// readmeMaxLinks caps the links read from one README.
const readmeMaxLinks = 400

// readmeLinks are the distinct web URLs a README's text links to, in
// order, with the punctuation and markup that end a sentence or a reST
// link ("`_") trimmed; nil for none.
func readmeLinks(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range readmeURLRe.FindAllString(text, -1) {
		m = strings.TrimRight(m, ".,;:!?*_>`")
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
		if len(out) == readmeMaxLinks {
			break
		}
	}
	return out
}

// readmeEvidence is the edge from the package's own README to root, for a
// keyless resolution whose root the metadata does not name: the README
// the registry publishes with the package links to the root, to a page
// in it, or to the site or docs tree it sits in. The README is the
// owner's own text, published through the registry the same way as the
// metadata, so the edge is the metadata edge (edgeMeta), never a second
// one: a README links to dependencies' and sponsors' sites too, and only
// the site linking back to the project's repository or package (the
// site-link edge) makes such a link the project's docs. Its links never
// count toward the DNS edge either (relatedHostEvidence), for the same
// reason.
//
// Without fetch, only a README the registry already sent is read (PyPI's
// description). With it, npm's, whose metadata carries none, is read from
// the package's published README.md (npmReadme): one request per package
// per resolution, which the caller makes only for a root the edge would
// verify (one edge short, the other being the site's link back).
func (w *Web) readmeEvidence(ctx context.Context, root webRoot, p *project, fetch bool) []evidence {
	if indexHosted(root.u) || isNonDocsHost(root.u.Hostname()) {
		return nil
	}
	names := p.names()
	if !p.repo.isZero() {
		names = append(names, strings.ToLower(p.repo.name))
	}
	for _, pkg := range p.pkgs {
		var links []string
		switch {
		case pkg.readme != "" && !fetch:
			links = readmeLinks(pkg.readme)
		case pkg.readme == "" && pkg.eco == ecoNPM && fetch:
			links = w.npmReadme(ctx, pkg)
		}
		if u, ok := readmeNames(links, root, names); ok {
			return []evidence{{kind: evPkgReadme, detail: "the " + pkgLabel(pkg) + " README links to " + u.String()}}
		}
	}
	return nil
}

// readmeNames returns the first of links that names root: on root's host
// ("www." aside), either the site or a docs tree root sits in (an
// ancestor of root's prefix, or the prefix itself), or a page below root
// whose first segment below it is not one of the project's names. A link
// into /docs/transformers/ on a host serving many projects' docs under
// /docs/ names /docs/transformers/, not /docs/.
func readmeNames(links []string, root webRoot, names []string) (*url.URL, bool) {
	prefix := strings.TrimSuffix(root.prefix, "/")
	var onHost []*url.URL
	intoDocs := false
	for _, raw := range links {
		u, ok := normalizeMetaURL(raw)
		if !ok || !root.sameHost(u) || hasDotSegment(u) || !isPageURL(u) {
			continue
		}
		onHost = append(onHost, u)
		intoDocs = intoDocs || docsSegment(u.Path) != ""
	}
	if intoDocs && root.prefix == "/" && !strings.HasPrefix(strings.ToLower(root.u.Hostname()), "docs.") {
		// The README points into the site's docs tree (electronjs.org/docs):
		// that names the tree, not the whole site its home link names.
		return nil, false
	}
	for _, u := range onHost {
		if linkNames(u, root, prefix, names) {
			return u, true
		}
	}
	return nil, false
}

// linkNames is readmeNames for one link on root's host; prefix is root's
// without its trailing slash.
func linkNames(u *url.URL, root webRoot, prefix string, names []string) bool {
	p := strings.TrimSuffix(u.Path, "/")
	if base := p[strings.LastIndexByte(p, '/')+1:]; base == "index" || strings.HasPrefix(strings.ToLower(base), "index.") {
		p = strings.TrimSuffix(p[:len(p)-len(base)], "/")
	}
	if p == prefix || strings.HasPrefix(prefix+"/", p+"/") {
		return true // the root, or the site or docs tree it sits in
	}
	if !root.contains(u) {
		return false
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(p, prefix+"/"), "/")
	return !slices.Contains(names, strings.ToLower(first))
}

// npmReadme is the links of the README.md an npm package publishes, read
// once per package per resolution from a CDN that serves the published
// package's files as they are (npm's own metadata endpoints carry no
// README short of the whole multi-megabyte document of every release).
// nil when it has none or it did not answer: a README is extra evidence,
// never a source whose failure counts.
func (w *Web) npmReadme(ctx context.Context, pkg pkgInfo) []string {
	if pkg.name == "" {
		return nil
	}
	version := cmp.Or(pkg.version, "latest")
	u := &url.URL{Scheme: "https", Host: npmFilesHost, Path: "/npm/" + pkg.name + "@" + version + "/README.md"}
	return w.memoLinks(ctx, u, func() []string {
		rctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
		defer cancel()
		body, _, _, err := w.fetchRawServed(rctx, u)
		if err != nil {
			return nil
		}
		return readmeLinks(string(body))
	})
}

// npmFilesHost serves the files of published npm packages by name and
// version.
const npmFilesHost = "cdn.jsdelivr.net"

// withMovedRepos is p as a root's links should be read for a keyless
// resolution: when the page links to a repository of p's repository's
// name under another owner, and one of the two moved to the other (GitHub
// redirects a transferred or renamed repository's address to its new
// one: jxnl/instructor to 567-labs/instructor), the page's link is p's
// own repository under another address, not someone else's project. The
// move is read from where GitHub's own address for the old repository
// lands (movedRepo), at most once per repository per resolution; only the
// owner of a repository can move it. Only a repository the registry
// metadata names is followed (one a search hit named is no project's
// claim), and only on a site the owner names (ownerNames): a namesake's
// site linking its own repository costs no request. p is not changed: a
// copy is returned.
func (w *Web) withMovedRepos(ctx context.Context, root webRoot, prof *siteProfile, p *project, c rootCandidate) *project {
	if p.repo.isZero() || p.fromHits || linksTo(prof, p) || !w.ownerNames(ctx, root, p, c) {
		return p
	}
	var aliases []repoKey
	for _, k := range repoLinks(prof) {
		if k.name != p.repo.name || k.owner == p.repo.owner {
			continue
		}
		if to, ok := w.movedRepo(ctx, k); ok && to == p.repo {
			aliases = append(aliases, k) // the site links the old address
		} else if to, ok := w.movedRepo(ctx, p.repo); ok && to == k {
			aliases = append(aliases, k) // the metadata names the old address
		}
		break
	}
	if len(aliases) == 0 {
		return p
	}
	cp := *p
	cp.aliases = append(slices.Clone(p.aliases), aliases...)
	return &cp
}

// movedRepo is the repository GitHub's address for k now serves, when it
// redirects elsewhere: one plain request for the repository's address,
// read only for where it lands (no API, nothing parsed from the page).
func (w *Web) movedRepo(ctx context.Context, k repoKey) (repoKey, bool) {
	u := &url.URL{Scheme: "https", Host: "github.com", Path: "/" + k.owner + "/" + k.name}
	got := w.memoLinks(ctx, u, func() []string {
		rctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
		defer cancel()
		raw, err := w.fetcher.FetchRaw(rctx, u.String())
		if err != nil || raw.URL == "" {
			return nil
		}
		return []string{raw.URL}
	})
	if len(got) == 0 {
		return repoKey{}, false
	}
	served, err := url.Parse(got[0])
	if err != nil {
		return repoKey{}, false
	}
	to, ok := githubRepoOf(served)
	return to, ok && to != k
}

// readmeMaxCandidates caps the roots put forward from a README.
const readmeMaxCandidates = 2

// readmeCandidates puts forward, for a project whose metadata leads to no
// docs site of its own (PyPI's instructor lists only its repository), the
// docs its README links to most: links on a docs. host, under a docs
// path, or on a host named after the package, by how often the README
// links there, at most readmeMaxCandidates. They are guesses
// (rootCandidate.guess), no evidence: each verifies only by two edges
// like any root, the README's link (readmeEvidence) and the site's link
// back to the repository or package, and one that does not answer is a
// rejection, not a failed source. A README linking its dependencies' docs
// costs a request for each, never a pick.
func readmeCandidates(p *project, set *candidateSet) {
	count := map[string]int{}
	var order []string
	roots := map[string]*url.URL{}
	for _, pkg := range p.pkgs {
		if pkg.readme == "" {
			continue
		}
		for _, raw := range readmeLinks(pkg.readme) {
			u, reason := checkMetaURL(raw)
			if reason != "" || isRegistryHost(u) || !isPageURL(u) || !readmeDocsLink(u, pkg) {
				continue
			}
			root := searchRootFor(u, pkg.name)
			k := linkKey(root)
			if count[k] == 0 {
				order = append(order, k)
				roots[k] = root
			}
			count[k]++
		}
	}
	slices.SortStableFunc(order, func(a, b string) int { return cmp.Compare(count[b], count[a]) })
	for i, k := range order {
		if i == readmeMaxCandidates {
			break
		}
		set.addGuess(roots[k])
	}
}

// readmeDocsLink reports a README link that looks like the package's
// docs: on a docs. host, under a docs path, or on a host one of whose
// labels holds the package's name (python.useinstructor.com).
func readmeDocsLink(u *url.URL, pkg pkgInfo) bool {
	host := strings.ToLower(u.Hostname())
	if strings.HasPrefix(host, "docs.") || docsSegment(u.Path) != "" {
		return true
	}
	name := strings.ToLower(pkg.name)
	name = name[strings.LastIndexByte(name, '/')+1:]
	return len(name) >= 3 && strings.Contains(strings.TrimSuffix(host, "."+publicSuffixOf(host)), name)
}

// publicSuffixOf is host's public suffix (com, co.uk, github.io).
func publicSuffixOf(host string) string {
	s, _ := publicsuffix.PublicSuffix(host)
	return s
}
