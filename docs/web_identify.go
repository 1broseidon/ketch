package docs

import (
	"context"
	"net/url"
	"slices"
	"strings"
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
	for _, a := range p.authoritative() {
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
	evs = w.siteLinkEvidence(ctx, root, prof, p, evs)
	identity := siteIdentity(root.u, prof)
	if oneShort(evs) || c.search && keylessVerified(evs) && identity != "framework" {
		if llms := w.cachedLLMS(ctx, root); llms.url != nil {
			evs = append(evs, manifestEvidenceFrom(llms, root, p, evs)...)
			identity = "llms.txt"
		}
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
// host is one site. Not on a shared host where the root page belongs to
// someone else than the project below it (a GitHub or GitLab Pages user
// site).
func (w *Web) homeLinkEvidence(ctx context.Context, root webRoot, p *project) []evidence {
	if root.prefix == "/" || indexHosted(root.u) {
		return nil
	}
	host := strings.ToLower(root.u.Hostname())
	if strings.HasSuffix(host, ".github.io") || strings.HasSuffix(host, ".gitlab.io") {
		return nil
	}
	home := &url.URL{Scheme: root.u.Scheme, Host: root.u.Host, Path: "/"}
	prof, err := w.cachedProfile(ctx, home)
	if err != nil || prof.served == nil || scopeHost(prof.served) != scopeHost(root.u) {
		return nil
	}
	for _, k := range repoLinks(prof) {
		if p.isRepo(k) {
			return []evidence{{kind: evLinksRepo, detail: "its home page " + scopeHost(home) + " links to github.com/" + k.String()}}
		}
	}
	return packageEvidence(root, profileLinkURLs(prof), p, "its home page "+scopeHost(home))
}
