package docs

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
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
var ErrAmbiguousSite = failure.New(failure.Validation, "ambiguous_site", "the search results are split between several sites")

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
	hits  []sfHit  // under root, best first
	score float64  // all the domain's hits
	docs  bool     // some hit under root has a docs signal
	named bool     // the request names its host or docs directory
	owner bool     // the question names its domain (ownsDomain)
	spec  bool     // a standards body's site (specDomains)
	alts  []sfSite // the domain's other docs trees, best first
	retry bool     // an alternative tree of a site read before
	// page is a page to read before any other: the page a library URL
	// names, or a verified root's own page.
	page *url.URL
}

// fuseWeighted is fuseHits with each list's weights scaled (1 when scale
// is nil or short).
func fuseWeighted(lists [][]search.Result, scale []float64) []sfHit {
	var out []sfHit
	at := map[string]int{}
	for li, list := range lists {
		sc := 1.0
		if li < len(scale) {
			sc = scale[li]
		}
		for r, res := range list {
			u, err := url.Parse(strings.TrimSpace(res.URL))
			if err != nil || !isPublicWebURL(u) {
				continue
			}
			u.Fragment, u.RawFragment = "", ""
			k := linkKey(u)
			w := sc / (sfRankK + float64(r))
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

// sfCitedWeight is what a page a hit's text links to weighs, against
// the hit itself: third-party answers cite the docs they paraphrase
// (nginx.org's directive reference in a hosting company's 413 guide).
const sfCitedWeight = 0.5

// sfMaxCited caps the links one hit's text contributes.
const sfMaxCited = 4

// citedRe matches an absolute http(s) URL in a result's text.
var citedRe = regexp.MustCompile(`https?://[^\s<>"'()\[\]{}|\\^` + "`" + `]+`)

// citedLists are, for each search list, the pages its results' text
// (description and excerpts) links to, ranked as the citing result: a
// search that returns third-party pages about a thing often cites the
// thing's own docs.
func citedLists(lists [][]search.Result) [][]search.Result {
	var out [][]search.Result
	for _, list := range lists {
		cited := make([]search.Result, 0, len(list))
		for _, r := range list {
			own, _ := url.Parse(r.URL)
			n := 0
			for _, raw := range citedRe.FindAllString(r.Description+"\n"+r.Content, -1) {
				raw = strings.TrimRight(raw, ".,;:!?*_")
				u, err := url.Parse(raw)
				if err != nil || own != nil && registrableDomain(u.Hostname()) == registrableDomain(own.Hostname()) {
					continue // a page's links to its own site add nothing to its vote
				}
				cited = append(cited, search.Result{URL: raw})
				if n++; n == sfMaxCited {
					break
				}
			}
		}
		out = append(out, cited)
	}
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
	"jsr.io", "mintlify.wiki",
}

// previewHosts are deployment platforms whose branch previews
// (branch.project.pages.dev, deploy-preview-12--site.netlify.app) copy a
// site's docs at some unreleased commit.
var previewHosts = []string{"pages.dev", "netlify.app", "vercel.app"}

// isPreviewHost reports a deployment platform's branch preview: two or
// more labels before the platform's domain, or one joined by "--".
func isPreviewHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, d := range previewHosts {
		if rest, ok := strings.CutSuffix(host, "."+d); ok {
			return strings.Contains(rest, ".") || strings.Contains(rest, "--")
		}
	}
	return false
}

// notDocsHostLabels are first host labels of sites that hold
// discussions or posts, not docs: forum.x.org, community.x.com, blog.x.dev.
var notDocsHostLabels = map[string]bool{
	"forum": true, "forums": true, "community": true, "discuss": true, "discourse": true, "answers": true,
	"blog": true, "blogs": true, "news": true, "status": true, "careers": true, "jobs": true, "shop": true, "store": true,
	"mailman": true, "lists": true, "groups": true, "chat": true,
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
	first, _, _ := strings.Cut(strings.TrimPrefix(host, "www."), ".")
	if isNonDocsHost(host) && (!docsHostLabels[first] || first == "api") || onDomains(host, thirdPartyDomains) || isPreviewHost(host) {
		return true // a forge's own docs host (docs.github.com) is docs; its API host (api.github.com) serves JSON
	}
	if notDocsHostLabels[first] {
		return true
	}
	if !isPageURL(u) || !isResultLink(u) || isPostPath(u.Path) {
		return true
	}
	return notDocsPath(u.Path)
}

// notDocsPath reports a path under a segment no docs page is under
// (notDocsSegments) or a Discourse topic's.
func notDocsPath(p string) bool {
	segs := pathSegments(p)
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
	if d := docsDirOver(u, base); d != "" {
		base = &url.URL{Scheme: u.Scheme, Host: u.Host, Path: d}
	}
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

// docsDirOver is the docs directory (docs, doc, documentation, manual)
// leading to a version segment that sits further down than right below
// it: /docs/guides/x/main/ is a guide's variant in the /docs/ tree, not a
// version root; /docs/3.9.1/ is one. "" when there is none.
func docsDirOver(u *url.URL, base *url.URL) string {
	segs, below := pathSegments(u.Path), pathSegments(base.Path)
	for i, seg := range below {
		if docsSegments[strings.ToLower(seg)] {
			if len(below) > i+2 {
				return segmentsPath(segs[:i+1])
			}
			return ""
		}
	}
	return ""
}

// siteKey groups hits into sites: the registrable domain, or, for docs a
// package index hosts or any host that serves many projects (one host,
// many projects: sharedProjectRoot), the docs tree.
func siteKey(u *url.URL, tree webRoot) string {
	if indexHosted(u) {
		return tree.String()
	}
	if _, ok := sharedProjectRoot(u); ok {
		return tree.String()
	}
	return registrableDomain(u.Hostname())
}

// projectTree is the docs tree a hit sits in on a host that serves many
// projects: the project root the library's name, as a path, gives it
// (namedProjectRoot), else its own project's (sharedProjectRoot); false on any
// other host. shared says the hits show its domain serving several
// projects (sharedDomains). perProject reports a tree that is one
// project's, which the site key keeps apart from the host's others.
func projectTree(u *url.URL, name string, shared bool) (tree webRoot, perProject bool) {
	pr, isProject := sharedProjectRoot(u)
	if r, ok := namedProjectRoot(u, name, shared && docsTree(u).prefix == "/"); ok && r.contains(u) {
		return r, true
	}
	if isProject {
		return pr, true
	}
	return docsTree(u), false
}

// sharedDomains are the registrable domains whose hits sit under two or
// more different first path elements: a host the search shows serving
// several projects, where a library name that is one of those elements
// is that project's path.
func sharedDomains(hits []sfHit) map[string]bool {
	firsts := map[string]map[string]bool{}
	for _, h := range hits {
		segs := pathSegments(h.u.Path)
		if len(segs) < 2 {
			continue
		}
		d := registrableDomain(h.u.Hostname())
		if firsts[d] == nil {
			firsts[d] = map[string]bool{}
		}
		firsts[d][strings.ToLower(segs[0])] = true
	}
	out := map[string]bool{}
	for d, f := range firsts {
		out[d] = len(f) >= 2
	}
	return out
}

// sitePrefs are what a request prefers: docs roots (a docs URL given, or
// what the library it names resolves to) and library names a site's host
// may carry; and the version it asks for. words are the question's own
// words: a site whose host or docs directory one of them names (nginx
// in nginx.org, lambda in docs.aws.amazon.com/lambda/) is more likely
// the thing's own docs than a page that discusses it, which wins a
// search on the question's other words.
type sitePrefs struct {
	roots   []webRoot
	names   []string // comparison keys (libraryKey)
	words   []string // comparison keys of the question's words
	version string
	// library is the library name as written ("net/http"), which on a
	// host that serves many projects is that project's path; "" for a
	// docs URL or none.
	library string
	// resolved is what resolve made of the name the request gives.
	resolved sfResolved
	// lead is the site the search results agree on without resolve's
	// preference (pickSite over sites chosen with no resolved root): what
	// an origin compares the resolved root with. nil for none.
	lead *webRoot
}

// sfWordBoost multiplies the weight of a hit on a site the question's
// words name.
const sfWordBoost = 2.0

// questionWords are a question's words, and each two adjacent ones run
// together, as comparison keys; stopwords, language names, generic docs
// words and words under three letters left out.
func questionWords(query string) []string {
	var out []string
	add := func(k string) {
		if len(k) >= 3 && !genericNames[k] && !languageWords[k] && !isDigits(k) && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	prev := ""
	for _, f := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '-' && r != '_'
	}) {
		f = strings.Trim(f, ".-_")
		if webStopwords[f] {
			prev = ""
			continue
		}
		add(libraryKey(f))
		if prev != "" {
			add(libraryKey(prev + f)) // two words a name runs together: redux toolkit, vue router
		}
		prev = f
	}
	return out
}

// named reports a hit whose host labels one of keys names, or, on a docs
// host or below a docs directory (strongDocs), one of its leading
// directories: /software/systemd/man/, docs.aws.amazon.com/lambda/, but
// not a tutorial site's /guides/nginx/.
func named(u *url.URL, keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	for _, label := range hostNames(u) {
		if slices.Contains(keys, libraryKey(label)) {
			return true
		}
	}
	if !strongDocs(u) {
		return false // a guide's directory on a site about something else is no name
	}
	segs := pathSegments(u.Path)
	for i := 0; i < min(len(segs)-1, 3); i++ {
		if slices.Contains(keys, libraryKey(segs[i])) {
			return true
		}
	}
	return false
}

// preferred reports a hit the request prefers: under one of its roots,
// or on a host one of its names labels.
func (p sitePrefs) preferred(u *url.URL) bool {
	for _, r := range p.roots {
		if r.contains(u) {
			return true
		}
	}
	return named(u, p.names)
}

// chooseSites groups the docs hits into sites, best first; each site's
// root is the docs tree most of its weight sits under (the version the
// request asks for first, when hits name it), and its other trees are
// the alternatives read when that one gives nothing (alts). Preferred hits
// weigh sfBoost times as much. A site the question names by its domain
// (owner), or else a standards body's (spec), goes first when its weight
// is a fair share of the leader's (ownerFirst).
func chooseSites(hits []sfHit, prefs sitePrefs) []sfSite {
	type tree struct {
		root  webRoot
		hits  []sfHit
		score float64
	}
	trees := map[string][]*tree{}
	var order []string
	scores := map[string]float64{}
	shared := sharedDomains(hits)
	for _, h := range hits {
		switch {
		case prefs.preferred(h.u):
			h.weight *= sfBoost
		case named(h.u, prefs.words):
			h.weight *= sfWordBoost
		}
		t, perProject := projectTree(h.u, prefs.library, shared[registrableDomain(h.u.Hostname())])
		for _, r := range prefs.roots {
			if r.contains(h.u) && t.contains(r.u) {
				t = r // the preferred root is the tree, however deep
			}
		}
		k := siteKey(h.u, t)
		if perProject {
			k = t.String() // one project of a host that serves many is a site of its own
		}
		if trees[k] == nil {
			order = append(order, k)
		}
		i := slices.IndexFunc(trees[k], func(x *tree) bool { return x.root.String() == t.String() })
		if i < 0 {
			trees[k] = append(trees[k], &tree{root: t})
			i = len(trees[k]) - 1
		}
		tr := trees[k][i]
		tr.hits = append(tr.hits, h)
		tr.score += h.weight
		scores[k] += h.weight
	}
	var out []sfSite
	for _, k := range order {
		ts := trees[k]
		named := append(slices.Clone(prefs.words), prefs.names...)
		sort.SliceStable(ts, func(i, j int) bool {
			return treeBetter(ts[i].score, ts[j].score, ts[i].root, ts[j].root, prefs.version, named)
		})
		var alts []sfSite
		for _, t := range ts {
			alts = append(alts, treeSite(k, t.root, t.hits, scores[k], prefs))
		}
		out = append(out, domainSite(alts, prefs))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return ownerFirst(out)
}

// treeSite is one docs tree of a domain (key) as a site.
func treeSite(key string, root webRoot, hits []sfHit, score float64, prefs sitePrefs) sfSite {
	s := sfSite{key: key, root: root, hits: hits, score: score}
	s.docs = slices.ContainsFunc(hits, func(h sfHit) bool { return h.docs })
	s.named = slices.ContainsFunc(hits, func(h sfHit) bool { return prefs.preferred(h.u) || named(h.u, prefs.words) })
	return s
}

// domainSite is a domain's best tree as its site, the others its
// alternatives, a variant's main docs (mainTree) first when its hits
// hold none; the question names the domain (owner), or it is a
// standards body's (spec, which is the protocol's docs).
func domainSite(trees []sfSite, prefs sitePrefs) sfSite {
	if main, ok := mainTree(trees[0], prefs.words); ok && !slices.ContainsFunc(trees, func(s sfSite) bool { return s.root.String() == main.root.String() }) {
		trees = append([]sfSite{main}, trees...)
	}
	s := trees[0]
	s.alts = trees[1:]
	s.owner = ownsDomain(s.key, prefs.words)
	s.spec = onDomains(s.root.u.Hostname(), specDomains)
	s.named = s.named || s.owner
	s.docs = s.docs || s.spec
	return s
}

// sfOwnerShare is the share of the leading site's weight a site the
// question names by its domain (or a standards body's site) needs to go
// first: a page about a product on another site loses to the product's
// own docs when both are found, but not to a single stray link.
const sfOwnerShare = 0.25

// ownerFirst moves to the front the best site the question names by its
// domain (duo.com for a question about duo), when its weight is at least
// sfOwnerShare of the leader's; when the question names none of the
// sites, a standards body's site (the RFC a vendor implements) the same
// way.
func ownerFirst(sites []sfSite) []sfSite {
	if len(sites) < 2 {
		return sites
	}
	pick := func(ok func(sfSite) bool) bool {
		for i, s := range sites {
			if !ok(s) {
				continue
			}
			if i > 0 && s.score >= sfOwnerShare*sites[0].score {
				s := sites[i]
				copy(sites[1:i+1], sites[:i])
				sites[0] = s
			}
			return true
		}
		return false
	}
	if !pick(func(s sfSite) bool { return s.owner }) {
		pick(func(s sfSite) bool { return s.spec })
	}
	return sites
}

// ownsDomain reports a site key (a registrable domain, or a hosted
// project's docs tree) whose first label one of the question's words is,
// whole or without the language word it joins: duo.com for "duo",
// requests.readthedocs.io for "requests", python-poetry.org for "poetry".
func ownsDomain(key string, words []string) bool {
	host := key
	if u, err := url.Parse(key); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(strings.ToLower(host), "www."), ".")
	for _, l := range []string{first, withoutLanguageWord(first)} {
		if k := libraryKey(l); len(k) >= 3 && slices.Contains(words, k) {
			return true
		}
	}
	return false
}

// specDomains are the sites of standards bodies, which publish the
// protocols and formats vendors' docs implement: for a question about the
// protocol itself the spec is the docs.
var specDomains = []string{
	"rfc-editor.org", "ietf.org", "w3.org", "whatwg.org", "openid.net", "ecma-international.org", "tc39.es",
	"unicode.org", "oasis-open.org", "iso.org",
}

// variantWords are path words that mark a copy of a site's docs for one
// edition, deployment or age (an enterprise server's, a self-managed
// install's, an archived release's) beside its main docs.
var variantWords = map[string]bool{
	"enterprise": true, "legacy": true, "archive": true, "archived": true, "archives": true, "deprecated": true,
	"obsolete": true, "old": true, "previous": true, "selfhosted": true, "selfmanaged": true, "onprem": true,
	"onpremise": true, "onpremises": true, "govcloud": true,
}

// variantSegment is the index of the first segment of a docs root's path
// that marks a variant of the main docs (variantWords, run together
// across hyphens: self-hosted; or an edition pinned to a release:
// enterprise-server@3.22), unless the question's words ask for it; -1
// for none.
func variantSegment(r webRoot, words []string) int {
	for i, seg := range pathSegments(r.prefix) {
		low := strings.ToLower(seg)
		name, _, pinned := strings.Cut(low, "@")
		parts := strings.FieldsFunc(name, func(c rune) bool { return c == '-' || c == '_' || c == '.' })
		marked := pinned && len(parts) > 0
		for j, p := range parts {
			if variantWords[p] || j > 0 && variantWords[parts[j-1]+p] {
				marked = true
			}
		}
		if !marked {
			continue
		}
		asked := false
		for _, p := range parts {
			asked = asked || slices.Contains(words, libraryKey(p))
		}
		if !asked {
			return i
		}
	}
	return -1
}

// mainTree is, for a variant tree (variantSegment), the main docs tree it
// copies: the root and its hits without the variant's segment
// (docs.x.com/en/enterprise-server@3.22/actions/ is docs.x.com/en/actions/).
func mainTree(s sfSite, words []string) (sfSite, bool) {
	i := variantSegment(s.root, words)
	if i < 0 {
		return sfSite{}, false
	}
	segs := pathSegments(s.root.prefix)
	strip := func(u *url.URL) *url.URL {
		p := pathSegments(u.Path)
		if len(p) <= i || !strings.EqualFold(p[i], segs[i]) {
			return nil
		}
		c := *u
		c.Path = "/" + strings.Join(append(append([]string{}, p[:i]...), p[i+1:]...), "/")
		if strings.HasSuffix(u.Path, "/") && c.Path != "/" {
			c.Path += "/"
		}
		c.RawPath = ""
		return &c
	}
	ru := strip(s.root.u)
	if ru == nil {
		return sfSite{}, false
	}
	root, err := parseWebRoot(ru.String())
	if err != nil {
		return sfSite{}, false
	}
	m := s
	m.root, m.hits, m.alts = root, nil, nil
	for _, h := range s.hits {
		if u := strip(h.u); u != nil {
			h.u = u
			m.hits = append(m.hits, h)
		}
	}
	return m, len(m.hits) > 0
}

// treeBetter reports whether a tree (score a, root ra) is a better root
// for its site than another (b, rb): not a translation, not a variant the
// question does not ask for (variantSegment), then the one whose root names the version
// asked for (with none asked, one that names no numbered release), else
// the one with more weight, else the shorter root.
func treeBetter(a, b float64, ra, rb webRoot, version string, words []string) bool {
	if la, lb := otherLocale(ra), otherLocale(rb); la != lb {
		return lb // English or unmarked docs over a translation
	}
	if va, vb := variantSegment(ra, words) >= 0, variantSegment(rb, words) >= 0; va != vb {
		return vb // the main docs over an enterprise, legacy or archived copy
	}
	if version == "" {
		if ma, mb := mainHostOver(ra, rb, words), mainHostOver(rb, ra, words); ma != mb {
			return ma // the domain's main host over a sibling the question does not name
		}
	}
	if version != "" {
		va, vb := rootNamesVersion(ra, version), rootNamesVersion(rb, version)
		if va != vb {
			return va
		}
	} else if na, nb := releaseRank(ra), releaseRank(rb); na != nb {
		return na < nb // no version asked: the current docs, then a release's, then development docs
	} else if na == 1 {
		if c := compareRelease(rootRelease(ra), rootRelease(rb)); c != 0 {
			return c > 0 // two releases: the newer
		}
	}
	if a != b {
		return a > b
	}
	return len(ra.prefix) < len(rb.prefix)
}

// mainHostOver reports a tree on its domain's main host (the domain
// itself, www. or docs.) beside one on a sibling subdomain of the same
// domain whose first label is no docs host label (four.htmx.org: a
// pre-release, a playground), when the words name the domain and not the
// sibling's label. A sibling the question names (kafka.apache.org for a
// Kafka question) is a project of its own.
func mainHostOver(a, b webRoot, words []string) bool {
	ha, hb := scopeHost(a.u), scopeHost(b.u)
	dom := registrableDomain(ha)
	if ha == hb || registrableDomain(hb) != dom || ha != dom && ha != "docs."+dom {
		return false
	}
	sibling, _, _ := strings.Cut(strings.TrimSuffix(hb, "."+dom), ".")
	if sibling == "" || hb == dom || hb == "docs."+dom || docsHostLabels[sibling] || localeSegRe.MatchString(sibling) {
		return false
	}
	return ownsDomain(dom, words) && !slices.Contains(words, libraryKey(sibling))
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
// mean anything (ErrAmbiguousSite, naming the candidates), unless the
// site is another tree of a site read before, the question names its
// domain, it is the spec (ownerFirst put those first), or the question
// names exactly one of the leading sites (onlyNamed).
func pickSite(sites []sfSite) (sfSite, error) {
	if len(sites) == 0 {
		return sfSite{}, fmt.Errorf("search: %w", ErrNoDocsFound)
	}
	top := sites[0]
	if top.retry || top.owner || top.spec {
		return top, nil // another tree of the site read before; a site the question names; the spec
	}
	if len(sites) > 1 && len(top.hits) < 2 && top.score < sfLead*sites[1].score {
		if i, ok := onlyNamed(sites); ok {
			return sites[i], nil
		}
		var names []string
		for _, s := range sites[:min(len(sites), 4)] {
			names = append(names, s.root.String())
		}
		return sfSite{}, fmt.Errorf("search: %w, none ahead and none tied to what was asked: %s; pass the docs you mean as the library (--library <docs URL>)", ErrAmbiguousSite, strings.Join(names, ", "))
	}
	return top, nil
}

// hostReleaseRe matches a host label naming a numbered release: v1-32,
// v3, 18.
var hostReleaseRe = regexp.MustCompile(`^v?\d+(?:[.-]\d+)*$`)

// numberedRoot reports a root in a numbered release's tree: a numbered
// version segment (/docs/15/, /en/5.2/), or a host labelled with one
// (v1-32.docs.kubernetes.io); latest, stable and current are not.
func numberedRoot(r webRoot) bool {
	if l := rootVersionLabel(r); l != "" && isNumericLabel(l) {
		return true
	}
	first, _, _ := strings.Cut(scopeHost(r.u), ".")
	return hostReleaseRe.MatchString(first)
}

// sfNamedDepth is how many of the leading sites onlyNamed looks among.
const sfNamedDepth = 4

// onlyNamed is the one site among the leading ones whose host or docs
// directory the request names (its library, or a word of its question),
// when exactly one is.
func onlyNamed(sites []sfSite) (int, bool) {
	found := -1
	for i, s := range sites[:min(len(sites), sfNamedDepth)] {
		if !s.named {
			continue
		}
		if found >= 0 {
			return 0, false
		}
		found = i
	}
	return found, found >= 0
}

// strongDocsSegments are path segments only docs trees have.
var strongDocsSegments = map[string]bool{
	"docs": true, "doc": true, "documentation": true, "manual": true, "man": true, "reference": true,
	"api": true, "apidocs": true,
}

// strongDocs reports a page on a docs host or hosting service, or below
// a docs directory.
func strongDocs(u *url.URL) bool {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	first, _, _ := strings.Cut(host, ".")
	if docsHostLabels[first] || onDomains(host, docsHostSuffixes) {
		return true
	}
	for _, seg := range pathSegments(u.Path) {
		if strongDocsSegments[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// releaseRank orders docs trees when no version is asked for: 0 the
// current docs (no version, or latest, stable, current), 1 a numbered
// release's, 2 development docs (dev, devdocs, main, next, nightly).
func releaseRank(r webRoot) int {
	for _, seg := range pathSegments(r.prefix) {
		if versionAliasClass[strings.ToLower(seg)] == "dev" {
			return 2
		}
	}
	if numberedRoot(r) {
		return 1
	}
	return 0
}

// otherLocale reports a root in a locale other than English: a leading
// locale path segment (/zh-cn/docs/) or host label (fr.lib.test).
func otherLocale(r webRoot) bool {
	labels := pathSegments(r.prefix)
	if len(labels) > 0 {
		labels = labels[:1]
	}
	if first, _, ok := strings.Cut(scopeHost(r.u), "."); ok && strings.Count(scopeHost(r.u), ".") > 1 {
		labels = append(labels, first)
	}
	for _, l := range labels {
		if localeSegRe.MatchString(l) && !strings.HasPrefix(strings.ToLower(l), "en") && !docsHostLabels[strings.ToLower(l)] {
			return true
		}
	}
	return false
}

// rootRelease is the release numbers a root's version label names (v3 is
// [3], /docs/15/ [15]); nil for none.
func rootRelease(r webRoot) []int {
	label := rootVersionLabel(r)
	if label == "" {
		label, _, _ = strings.Cut(scopeHost(r.u), ".")
		label = strings.ReplaceAll(label, "-", ".")
	}
	n, _ := releaseNumbers(normLabel(label))
	return n
}
