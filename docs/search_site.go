package docs

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/1broseidon/ketch/internal/failure"
	"github.com/1broseidon/ketch/search"
)

// Choosing the docs site a question's search hits agree on (the
// search-first backend, search_first.go). Each hit is judged from its URL
// and title alone: pages that are never docs (code forges, registries,
// social and Q&A sites, forums, blog posts, documentation mirrors) are
// dropped, and the rest weigh by rank, a hit with a docs signal (a docs
// host, a docs path, a title that says so) twice one without. Hits are
// grouped by registrable domain (a package index's hosted docs by their
// docs tree, since one index hosts many projects), and the domain with
// the most weight is the site, its docs tree the root the most of its
// weight sits under. A domain that is not clearly ahead of the next is no
// answer: the question is about several sites' docs, or none.

// ErrNoDocsFound is a question whose search hits hold no documentation
// page: a refusal, since searching again finds the same.
var ErrNoDocsFound = failure.New(failure.NotFound, "no_docs_found", "no documentation among the search results")

// ErrAmbiguousSite is a question whose docs hits are split between sites
// with none clearly ahead: the caller names one (a docs URL as the
// library).
var ErrAmbiguousSite = failure.New(failure.Validation, "ambiguous_site", "the search results point to several documentation sites")

const (
	// sfRankK is the rank constant hits are weighed by: 1/(sfRankK+rank).
	sfRankK = 3.0
	// sfPlainWeight is what a hit with no docs signal weighs, against 1
	// for one with a signal.
	sfPlainWeight = 0.5
	// sfLead is how far ahead of the next domain the chosen one must be
	// when it has a single hit: two sites with one hit each say nothing.
	sfLead = 1.5
	// sfBoost multiplies the weight of a site the request prefers (the
	// library it names resolves to it, or its host names the library).
	sfBoost = 3.0
)

// sfHit is one search hit that may be a docs page.
type sfHit struct {
	u      *url.URL
	title  string
	desc   string
	weight float64
	docs   bool // a docs signal: host, path or title
}

// sfSite is the hits of one site (registrable domain) and the docs tree
// most of their weight sits under.
type sfSite struct {
	key   string
	root  webRoot
	hits  []sfHit // under root, best first
	score float64 // all the domain's hits
	docs  bool    // some hit under root has a docs signal
}

// fuseHits merges search result lists into one ranking of pages, each
// weighed by its rank in every list it is in, best first.
func fuseHits(lists [][]search.Result) []sfHit {
	var out []sfHit
	at := map[string]int{}
	for _, list := range lists {
		for r, res := range list {
			u, err := url.Parse(strings.TrimSpace(res.URL))
			if err != nil || !isPublicWebURL(u) {
				continue
			}
			u.Fragment, u.RawFragment = "", ""
			k := linkKey(u)
			w := 1 / (sfRankK + float64(r))
			if i, ok := at[k]; ok {
				out[i].weight += w
				out[i].title = firstNonEmpty(out[i].title, res.Title)
				out[i].desc = firstNonEmpty(out[i].desc, res.Description)
				continue
			}
			at[k] = len(out)
			out = append(out, sfHit{u: u, title: res.Title, desc: res.Description, weight: w})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].weight > out[j].weight })
	return out
}

// docsHits keeps the hits that may be docs pages, each marked with
// whether it carries a docs signal and weighed down when it does not.
func docsHits(hits []sfHit) []sfHit {
	var out []sfHit
	for _, h := range hits {
		if notDocsHit(h.u) {
			continue
		}
		h.docs = docsSignal(h.u, h.title)
		if !h.docs {
			h.weight *= sfPlainWeight
		}
		out = append(out, h)
	}
	return out
}

// thirdPartyDomains are hosts (with their subdomains) whose pages are
// about other people's software but are never its documentation: Q&A
// sites, blog platforms, tutorial farms, and documentation mirrors and
// AI-generated summaries of other sites' docs and code.
var thirdPartyDomains = []string{
	"stackexchange.com", "superuser.com", "serverfault.com", "askubuntu.com", "quora.com",
	"dev.to", "hashnode.dev", "hashnode.com", "substack.com", "hackernoon.com", "wordpress.com", "blogspot.com",
	"geeksforgeeks.org", "w3schools.com", "tutorialspoint.com", "javatpoint.com",
	"deepwiki.com", "context7.com", "devdocs.io", "runebook.dev", "w3cubdocs.com", "jsdocs.io", "tessl.io",
	"snyk.io", "socket.dev", "npmtrends.com", "bundlephobia.com", "unpkg.com", "jsdelivr.com", "cdnjs.com",
}

// notDocsHostLabels are first host labels of sites that hold
// discussions or posts, not docs: forum.x.org, community.x.com, blog.x.dev.
var notDocsHostLabels = map[string]bool{
	"forum": true, "forums": true, "community": true, "discuss": true, "discourse": true, "answers": true,
	"blog": true, "blogs": true, "news": true, "status": true, "careers": true, "jobs": true, "shop": true, "store": true,
}

// notDocsSegments are path segments under which a site keeps
// discussions, issues, media or marketing rather than docs.
var notDocsSegments = map[string]bool{
	"questions": true, "question": true, "forum": true, "forums": true, "discussions": true, "discussion": true,
	"threads": true, "thread": true, "topic": true, "topics": true, "issues": true, "pull": true, "pulls": true,
	"answers": true, "comments": true, "videos": true, "video": true, "watch": true, "podcast": true, "podcasts": true,
	"webinar": true, "webinars": true, "events": true, "pricing": true, "careers": true, "jobs": true, "press": true,
	"customers": true, "case-studies": true,
}

// notDocsHit reports a hit that is no docs page: a host that never serves
// docs (code forges, registries, chat and social sites, Q&A, blog
// platforms, mirrors), a forum or blog host, a post or discussion path, a
// Discourse topic (/t/slug/123), or a file that is no page.
func notDocsHit(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	if isNonDocsHost(host) || onDomains(host, thirdPartyDomains) {
		return true
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(host, "www."), ".")
	if notDocsHostLabels[first] {
		return true
	}
	if !isPageURL(u) || !isResultLink(u) || isPostPath(u.Path) {
		return true
	}
	segs := pathSegments(u.Path)
	for i, s := range segs {
		low := strings.ToLower(s)
		if notDocsSegments[low] && i < len(segs)-1 {
			return true
		}
		if low == "t" && i < len(segs)-1 && isDigits(segs[len(segs)-1]) {
			return true // a Discourse topic: /t/slug/123
		}
	}
	return false
}

// onDomains reports host as one of domains or a subdomain of one.
func onDomains(host string, domains []string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// docsHostLabels are first host labels of docs sites.
var docsHostLabels = map[string]bool{
	"docs": true, "doc": true, "documentation": true, "developer": true, "developers": true, "dev": true,
	"devdocs": true, "learn": true, "help": true, "support": true, "manual": true, "guide": true, "guides": true,
	"reference": true, "api": true, "apidocs": true, "wiki": true, "kb": true,
}

// docsPathSegments are path segments of docs trees.
var docsPathSegments = map[string]bool{
	"docs": true, "doc": true, "documentation": true, "manual": true, "guide": true, "guides": true,
	"reference": true, "api": true, "tutorial": true, "tutorials": true, "learn": true, "handbook": true,
	"user-guide": true, "userguide": true, "howto": true, "how-to": true, "getting-started": true, "kb": true,
	"help": true, "concepts": true, "latest": true, "stable": true,
}

// docsHostSuffixes are hosting services whose sites are mostly docs.
var docsHostSuffixes = []string{"readthedocs.io", "readthedocs.org", "gitbook.io", "github.io", "docs.rs", "pkg.go.dev"}

// docsTitleWords are title words that say a page is docs.
var docsTitleWords = map[string]bool{
	"docs": true, "documentation": true, "guide": true, "reference": true, "manual": true, "api": true,
	"tutorial": true, "handbook": true, "docs.rs": true,
}

// docsSignal reports a hit that says it is docs: a docs host or hosting
// service, a docs segment in its path, or a docs word in its title.
func docsSignal(u *url.URL, title string) bool {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	first, _, _ := strings.Cut(host, ".")
	if docsHostLabels[first] || onDomains(host, docsHostSuffixes) {
		return true
	}
	for _, s := range pathSegments(u.Path) {
		if docsPathSegments[strings.ToLower(strings.TrimSuffix(s, path.Ext(s)))] {
			return true
		}
	}
	for _, t := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) && r != '.' }) {
		if docsTitleWords[strings.Trim(t, ".")] {
			return true
		}
	}
	return false
}

// docsTree is the docs tree a page sits in: its path up to its first
// version segment, or its locale and a docs segment (siteRootOf); else up
// to a docs segment among its first four directories
// (/psycopg3/docs/basic/x is /psycopg3/docs/), or its leading locale; a GitHub or GitLab Pages
// project's directory; else the host's root.
func docsTree(u *url.URL) webRoot {
	base := siteRootOf(u)
	if base.Path == "" || base.Path == "/" {
		segs := pathSegments(u.Path)
		dirs := len(segs)
		if !strings.HasSuffix(u.Path, "/") && dirs > 0 {
			dirs--
		}
		for i := 0; i < min(dirs, 4); i++ {
			if docsSegments[strings.ToLower(segs[i])] {
				base = &url.URL{Scheme: u.Scheme, Host: u.Host, Path: segmentsPath(segs[:i+1])}
				break
			}
		}
		if (base.Path == "" || base.Path == "/") && dirs > 0 && localeSegRe.MatchString(segs[0]) {
			base = &url.URL{Scheme: u.Scheme, Host: u.Host, Path: segmentsPath(segs[:1])} // /en/: the locale's docs
		}
	}
	if base.Path == "" || base.Path == "/" {
		base = hostRootFor(u)
	}
	r, err := parseWebRoot(base.String())
	if err != nil {
		r, _ = parseWebRoot(u.Scheme + "://" + u.Host + "/")
	}
	return r
}

// siteKey groups hits into sites: the registrable domain, or, for docs a
// package index hosts (one host, many projects), the docs tree.
func siteKey(u *url.URL, tree webRoot) string {
	if indexHosted(u) {
		return tree.String()
	}
	return registrableDomain(u.Hostname())
}

// sitePrefs are what a request prefers: docs roots (a docs URL given, or
// what the library it names resolves to) and library names a site's host
// may carry; and the version it asks for.
type sitePrefs struct {
	roots   []webRoot
	names   []string // comparison keys (libraryKey)
	version string
}

// preferred reports a hit the request prefers: under one of its roots,
// or on a host one of its names labels.
func (p sitePrefs) preferred(u *url.URL) bool {
	for _, r := range p.roots {
		if r.contains(u) {
			return true
		}
	}
	if len(p.names) == 0 {
		return false
	}
	for _, label := range hostNames(u) {
		if slices.Contains(p.names, libraryKey(label)) {
			return true
		}
	}
	return false
}

// chooseSites groups the docs hits into sites, best first; each site's
// root is the docs tree most of its weight sits under (the version the
// request asks for first, when hits name it). Preferred hits weigh
// sfBoost times as much.
func chooseSites(hits []sfHit, prefs sitePrefs) []sfSite {
	type tree struct {
		root  webRoot
		hits  []sfHit
		score float64
	}
	trees := map[string]map[string]*tree{}
	var order []string
	scores := map[string]float64{}
	for _, h := range hits {
		if prefs.preferred(h.u) {
			h.weight *= sfBoost
		}
		t := docsTree(h.u)
		for _, r := range prefs.roots {
			if r.contains(h.u) && t.contains(r.u) {
				t = r // the preferred root is the tree, however deep
			}
		}
		k := siteKey(h.u, t)
		if trees[k] == nil {
			trees[k] = map[string]*tree{}
			order = append(order, k)
		}
		tr := trees[k][t.String()]
		if tr == nil {
			tr = &tree{root: t}
			trees[k][t.String()] = tr
		}
		tr.hits = append(tr.hits, h)
		tr.score += h.weight
		scores[k] += h.weight
	}
	var out []sfSite
	for _, k := range order {
		var best *tree
		for _, t := range trees[k] {
			if best == nil || treeBetter(t.score, best.score, t.root, best.root, prefs.version) {
				best = t
			}
		}
		s := sfSite{key: k, root: best.root, hits: best.hits, score: scores[k]}
		s.docs = slices.ContainsFunc(best.hits, func(h sfHit) bool { return h.docs })
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

// treeBetter reports whether a tree (score a, root ra) is a better root
// for its site than another (b, rb): the one whose root names the version
// asked for, else the one with more weight, else the shorter root.
func treeBetter(a, b float64, ra, rb webRoot, version string) bool {
	if version != "" {
		va, vb := rootNamesVersion(ra, version), rootNamesVersion(rb, version)
		if va != vb {
			return va
		}
	}
	if a != b {
		return a > b
	}
	return len(ra.prefix) < len(rb.prefix)
}

// rootNamesVersion reports a root whose host or path names version: a
// label of its own that the version is (3 names 3.9.1 and v3).
func rootNamesVersion(r webRoot, version string) bool {
	labels := []string{rootVersionLabel(r)}
	if first, _, ok := strings.Cut(scopeHost(r.u), "."); ok {
		labels = append(labels, first)
	}
	labels = append(labels, pathSegments(r.prefix)...)
	for _, l := range labels {
		if l != "" && versionNames(version, l) {
			return true
		}
	}
	return false
}

// pickSite is the site the hits agree on, or why there is none: no docs
// hit at all (ErrNoDocsFound), or a lead over the next site too small to
// mean anything (ErrAmbiguousSite, naming the candidates).
func pickSite(sites []sfSite) (sfSite, error) {
	if len(sites) == 0 {
		return sfSite{}, fmt.Errorf("search: %w", ErrNoDocsFound)
	}
	top := sites[0]
	if len(sites) > 1 && len(top.hits) < 2 && top.score < sfLead*sites[1].score {
		var names []string
		for _, s := range sites[:min(len(sites), 4)] {
			names = append(names, s.root.String())
		}
		return sfSite{}, fmt.Errorf("search: %w: %s; pass one as the library (--library <docs URL>)", ErrAmbiguousSite, strings.Join(names, ", "))
	}
	return top, nil
}
