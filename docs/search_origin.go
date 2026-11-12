package docs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Right source or say so (design/docs-evidence.md, Build 1). A live
// answer (pages a search read) that came from the wrong site looks exactly
// like one from the right site. So the search backend:
//
//   - resolves the library a request names (--library, or a firm reading
//     of a bare question) while it searches, and answers from the docs root
//     resolve verifies, unless the site the search agrees on is that
//     project's own (siteOfProject); several roots resolve verifies, none
//     ahead, refuse the question with them listed;
//   - reads a host that serves many unrelated projects (a package index's
//     docs host, a module path's host) as one library per project path
//     (sharedProjectRoot);
//   - says where an answer came from (Outcome.Origin): verified, with the
//     evidence, or unverified, naming the host and what was asked.

// Origin values: an answer from a site ketch has evidence for, and one
// from a site it has none for.
const (
	originVerified   = "verified"
	originByName     = "by name"
	originUnverified = "unverified"
)

// OriginUnverified reports an origin line that says ketch could not
// confirm the site as the official docs of what was asked.
func OriginUnverified(origin string) bool { return strings.HasPrefix(origin, originUnverified) }

// OriginByName reports an origin line whose only support is a registry
// package of the name asked: no second signal ties the site to it.
func OriginByName(origin string) bool { return strings.HasPrefix(origin, originByName) }

// sfOrigin is an answer's origin line and, for one by name, the docs roots
// the caller can choose between (the resolved one, the one the search
// results favour).
type sfOrigin struct {
	text  string
	roots []string
}

func verifiedOrigin(evidence string) sfOrigin {
	return sfOrigin{text: originVerified + ", " + evidence}
}

func unverifiedOrigin(host, what string) sfOrigin {
	return sfOrigin{text: fmt.Sprintf("%s, ketch could not confirm %s as the official documentation of %s", originUnverified, host, what)}
}

// byNameOrigin is the origin of an answer from the root resolve found for
// m when only the registry name supports it: "<host> documents the
// <ecosystem> package <name>", and the site the search results favour
// when it is another (lead; nil for none).
func byNameOrigin(m *webMatch, lead *webRoot) sfOrigin {
	what := "the docs root resolve found"
	for _, p := range m.packages {
		if eco, name, ok := strings.Cut(p, ":"); ok && eco != string(ecoGitHub) {
			what = fmt.Sprintf("the %s package %s", eco, name)
			break
		}
	}
	o := sfOrigin{text: fmt.Sprintf("%s, %s documents %s", originByName, scopeHost(m.root.u), what), roots: []string{m.root.String()}}
	if lead != nil {
		o.text += "; the search results favour " + lead.String()
		o.roots = append(o.roots, lead.String())
	}
	return o
}

// resolvedOrigin is the origin of an answer read from the root resolve
// verified for m: verified for a standard-library module the language's
// own docs document, a pick the search's agreement made, or when the
// site the search results agree on (lead, chosen without resolve's
// preference) is the same project; else by name, naming lead when there
// is one.
func resolvedOrigin(m *webMatch, lead *webRoot) sfOrigin {
	if m.std || m.agreed {
		return verifiedOrigin(matchEvidence(m))
	}
	if lead != nil {
		if _, ok := siteOfProject(*lead, m); ok {
			return verifiedOrigin(matchEvidence(m) + ", and the search results agree")
		}
	}
	return byNameOrigin(m, lead)
}

// sfResolved is what resolve made of the library a request names, found
// while the question was searched.
type sfResolved struct {
	name      string                 // the name resolved, as written
	match     *webMatch              // the one docs root resolve verified
	ambiguous *AmbiguousLibraryError // several verified, none ahead
	bare      bool                   // a reading of a bare question (resolved strictly)
	took      time.Duration
}

// goStdRe is a name shaped like a Go standard-library import path of two
// elements or more (net/http, encoding/json): lower-case letters and
// digits, no dot. A one-word name (context, fmt) names too many other
// things to be read as one.
var goStdRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:/[a-z][a-z0-9]*)+$`)

// resolveGoStd verifies a standard-library package by the Go package
// index's own page for it: pkg.go.dev serves an import path whose first
// element has no dot only for the standard library, at that path, titled
// "<name> package - <path> - Go Packages". One page read; no registry.
func (w *Web) resolveGoStd(ctx context.Context, name string) (webMatch, bool) {
	name = strings.TrimPrefix(name, "go:")
	if !goStdRe.MatchString(name) {
		return webMatch{}, false
	}
	u, err := url.Parse("https://pkg.go.dev/" + name)
	if err != nil {
		return webMatch{}, false
	}
	prof, err := w.cachedProfile(ctx, u)
	if err != nil || prof == nil || prof.served == nil || strings.Trim(prof.served.Path, "/") != name ||
		!strings.Contains(prof.title, " - "+name+" - ") {
		return webMatch{}, false
	}
	root, err := parseWebRoot(u.String() + "/")
	if err != nil {
		return webMatch{}, false
	}
	return webMatch{
		root: root, title: strings.TrimSpace(prof.title), trust: 8,
		evidence: []evidence{{kind: evRegistryDocs, detail: "pkg.go.dev documents " + name + " in the Go standard library"}},
		packages: []string{"go:" + name}, project: "go:" + name, registry: true, indexOnly: true, stars: -1, prof: prof, std: true,
	}, true
}

// pyStdRe is a name shaped like a Python standard-library module: a
// lower-case identifier, or two joined by a dot (os.path).
var pyStdRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*)?$`)

// resolvePyStd verifies a Python standard-library module by the
// language's own docs page for it, docs.python.org/3/library/<name>.html,
// titled "<name> — … — Python … documentation". The root is that
// directory, the page read first. One page read; no registry.
func (w *Web) resolvePyStd(ctx context.Context, name string) (webMatch, bool) {
	if !pyStdRe.MatchString(name) {
		return webMatch{}, false
	}
	u, err := url.Parse("https://docs.python.org/3/library/" + name + ".html")
	if err != nil {
		return webMatch{}, false
	}
	prof, err := w.cachedProfile(ctx, u)
	if err != nil || prof == nil || prof.served == nil || !strings.HasSuffix(prof.served.Path, "/library/"+name+".html") ||
		!strings.HasPrefix(strings.TrimSpace(prof.title), name+" ") || !strings.Contains(prof.title, "Python") {
		return webMatch{}, false
	}
	root, err := parseWebRoot(u.String())
	if err != nil {
		return webMatch{}, false
	}
	return webMatch{
		root: root, page: u, title: strings.TrimSpace(prof.title), trust: 8, std: true,
		evidence: []evidence{{kind: evRegistryDocs, detail: "docs.python.org documents " + name + " in the Python standard library"}},
		project:  "python:" + name, stars: -1, prof: prof,
	}, true
}

// onlyPyPI reports a match whose packages are PyPI's (and its
// repository's) alone: a same-named package the standard library's
// module goes ahead of.
func onlyPyPI(packages []string) bool {
	for _, p := range packages {
		if !strings.HasPrefix(p, "pypi:") && !strings.HasPrefix(p, "github:") {
			return false
		}
	}
	return true
}

// resolveNamed resolves the library req names, for prefs: its --library
// name or spec (resolve's own pick: one verified root, or an ambiguity),
// else the first firm reading of a bare question (a name its syntax puts
// there, not a guess from joined words, not a common word), which must
// meet resolve's strict bar: an exact package name, its own docs site and
// trust 5. A question's words that name a language pick among an
// ambiguity's candidates.
func (s *SearchFirst) resolveNamed(ctx context.Context, req sfRequest) (out sfResolved) {
	start := time.Now()
	spec, bare := req.spec, false
	if req.library == "" {
		var ok bool
		if spec, ok = bareReading(req.query); !ok {
			return sfResolved{}
		}
		bare = true
	}
	out = sfResolved{name: spec.raw, bare: bare}
	defer func() { out.took = time.Since(start) }()
	std := make(chan *webMatch, 1)
	py := make(chan *webMatch, 1)
	goName := goStdRe.MatchString(strings.TrimPrefix(spec.raw, "go:"))
	pyName := spec.eco == ecoAny && spec.version == "" && pyStdRe.MatchString(spec.raw)
	if !bare && (goName || pyName) {
		go func() {
			defer close(std)
			defer close(py)
			if goName {
				if m, ok := s.web.resolveGoStd(ctx, spec.raw); ok {
					std <- &m
				}
			}
			if pyName {
				if m, ok := s.web.resolvePyStd(ctx, spec.raw); ok {
					py <- &m
				}
			}
		}()
	} else {
		close(std)
		close(py)
	}
	m, err := s.web.resolveOne(ctx, spec, bare)
	if sm := <-std; sm != nil {
		out.match = sm // the standard library's package, whatever else goes by the name
		return out
	}
	var amb *AmbiguousLibraryError
	if pm := <-py; pm != nil {
		// The language's own module, ahead of a same-named PyPI package
		// (an old backport); a library of another ecosystem keeps the name.
		switch {
		case err == nil && m.root.u != nil && !onlyPyPI(m.packages):
		case errors.As(err, &amb) && slices.ContainsFunc(amb.Matches, func(c LibraryMatch) bool { return isHTTPURL(c.ID) && !onlyPyPI(c.Packages) }):
		default:
			out.match = pm
			return out
		}
	}
	switch {
	case err == nil && m.root.u != nil:
		out.match = &m
	case errors.As(err, &amb) && !bare:
		if i, ok := languagePick(amb.Matches, req.query); ok {
			out.match = &webMatch{root: rootOf(amb.Matches[i].ID), title: amb.Matches[i].Title, trust: amb.Matches[i].TrustScore,
				evidence: evidenceOf(amb.Matches[i].Evidence), packages: amb.Matches[i].Packages, stars: -1}
			break
		}
		out.ambiguous = amb
	}
	return out
}

// bareReading is the first firm reading of a bare question, as
// splitLibraryQuery orders them: never a guess from joined words, a
// common word or a docs URL.
func bareReading(query string) (libSpec, bool) {
	for _, sp := range splitLibraryQuery(query) {
		if sp.strict || sp.common || sp.spec.url != "" || sp.spec.name == "" {
			continue
		}
		return sp.spec, true
	}
	return libSpec{}, false
}

// languagePick is the one candidate whose ecosystem a language word of
// the question names ("sqlx connection pool in rust": crates' sqlx), when
// exactly one is.
func languagePick(ms []LibraryMatch, query string) (int, bool) {
	var ecos []ecosystem
	for _, w := range strings.Fields(strings.ToLower(query)) {
		w = strings.Trim(w, ".,;:!?()[]\"'")
		if w == "golang" {
			w = "go"
		}
		if eco := languageRegistry(w); eco != ecoAny && !slices.Contains(ecos, eco) {
			ecos = append(ecos, eco)
		}
	}
	if len(ecos) != 1 {
		return 0, false
	}
	found := -1
	for i, m := range ms {
		if !slices.ContainsFunc(m.Packages, func(p string) bool { return strings.HasPrefix(p, string(ecos[0])+":") }) {
			continue
		}
		if found >= 0 {
			return 0, false
		}
		found = i
	}
	return found, found >= 0
}

func rootOf(raw string) webRoot {
	r, _ := parseWebRoot(raw)
	return r
}

func evidenceOf(details []string) []evidence {
	out := make([]evidence, 0, len(details))
	for _, d := range details {
		out = append(out, evidence{kind: evNote, detail: d})
	}
	return out
}

// matchEvidence is the evidence an origin line gives for a root resolve
// verified: its first finding and its trust.
func matchEvidence(m *webMatch) string {
	ev := ""
	for _, e := range m.evidence {
		if e.kind != evNote {
			ev = e.detail
			break
		}
		if ev == "" && !strings.HasPrefix(e.detail, "not checked") {
			ev = e.detail
		}
	}
	if ev == "" {
		ev = "resolve verified " + m.root.String()
	}
	return fmt.Sprintf("%s (resolve, trust %.1f)", ev, m.trust)
}

// settleAmbiguity decides a library name resolve found several libraries
// for, given the sites the search agrees on (best first):
//
//   - the leading site is the own docs site (not a package index's) of
//     exactly one candidate, by its root or its Go module's host: that
//     one, read from the leading site (gorm: gorm.io is go:gorm.io/gorm's,
//     beside an older module and a namesake crate);
//   - two or more candidates have a verified docs root: a refusal listing
//     them, each with its packages (ecosystem) and title (sqlx: a crate and
//     a Go module, docs a package index hosts for both);
//   - one or none has: resolve was not sure, and the search's sites
//     answer as if it had found nothing (homebrew: a namesake crate's
//     docs beside packages with none).
//
// It sets p.resolved.match for the first and clears the ambiguity for
// the last; it returns the refusal's error for the second.
func settleAmbiguity(p *sitePrefs, sites []sfSite) *AmbiguousLibraryError {
	amb := p.resolved.ambiguous
	var verified []LibraryMatch
	for _, m := range amb.Matches {
		if isHTTPURL(m.ID) {
			verified = append(verified, m)
		}
	}
	p.resolved.ambiguous = nil
	if len(sites) > 0 {
		found := -1
		for i, m := range verified {
			if !ownSiteOf(sites[0].root, m) {
				continue
			}
			if found >= 0 {
				found = -2
				break
			}
			found = i
		}
		if found >= 0 {
			m := verified[found]
			p.resolved.match = &webMatch{root: sites[0].root, title: m.Title, trust: m.TrustScore, packages: m.Packages, stars: -1, agreed: true,
				evidence: []evidence{{kind: evNote, detail: fmt.Sprintf("the search agrees on the own site of %s, one of %d libraries named %s", strings.Join(m.Packages, ", "), len(amb.Matches), amb.Library)}}}
			return nil
		}
	}
	if len(verified) < 2 {
		return nil
	}
	return &AmbiguousLibraryError{Library: amb.Library, Matches: verified}
}

// ownSiteOf reports a docs tree that is candidate m's own docs site, not
// a package index's page for it: at, under or over its root when that is
// not on a package index, or on the host of its Go module path.
func ownSiteOf(root webRoot, m LibraryMatch) bool {
	if indexHosted(root.u) {
		return false
	}
	if r, err := parseWebRoot(m.ID); err == nil && !indexHosted(r.u) && (r.contains(root.u) || root.contains(r.u)) {
		return true
	}
	dom := registrableDomain(root.u.Hostname())
	for _, pkg := range m.Packages {
		mod, ok := strings.CutPrefix(pkg, "go:")
		if !ok {
			continue
		}
		host, _, _ := strings.Cut(mod, "/")
		if strings.Contains(host, ".") && !isNonDocsHost(host) && registrableDomain(host) == dom {
			return true
		}
	}
	return false
}

// siteOfProject reports a docs tree on the site of the project resolve
// verified m for, and why: at, under or over m's root; on the same
// registrable domain, when m's root is not on a host many projects share;
// or on a domain the project's own metadata publishes (a homepage, a docs
// URL, a Go module's host) or its repository's owner is named by.
func siteOfProject(root webRoot, m *webMatch) (string, bool) {
	if m == nil || m.root.u == nil || root.u == nil {
		return "", false
	}
	if m.root.contains(root.u) || root.contains(m.root.u) {
		return matchEvidence(m), true
	}
	host := root.u.Hostname()
	dom := registrableDomain(host)
	if _, shared := sharedProjectRoot(m.root.u); !shared && !indexHosted(m.root.u) && registrableDomain(m.root.u.Hostname()) == dom {
		return fmt.Sprintf("on %s, the site of %s", dom, matchEvidence(m)), true
	}
	p := m.proj
	if p == nil || isNonDocsHost(host) || indexHosted(root.u) {
		return "", false
	}
	for _, a := range ownerURLs(p) {
		if !isNonDocsHost(a.url.Hostname()) && !indexHosted(a.url) && registrableDomain(a.url.Hostname()) == dom {
			return fmt.Sprintf("on %s, the domain of %s %s", dom, a.src, scopeHost(a.url)), true
		}
	}
	for _, pkg := range p.pkgs {
		if pkg.eco != ecoGo {
			continue
		}
		first, _, _ := strings.Cut(pkg.name, "/")
		if strings.Contains(first, ".") && !isNonDocsHost(first) && registrableDomain(first) == dom {
			return fmt.Sprintf("on %s, the host of go module %s", dom, pkg.name), true
		}
	}
	if owner := libraryKey(p.repo.owner); len(owner) >= 3 {
		label, _, _ := strings.Cut(dom, ".")
		if libraryKey(label) == owner {
			return fmt.Sprintf("on %s, the domain of %s, owner of github.com/%s", dom, p.repo.owner, p.repo.String()), true
		}
	}
	return "", false
}

// sharedProjectRoot is, for a page on a host that serves many unrelated
// projects, the root of the project the page belongs to; false for any
// other page. Two structural marks, no list of hosts:
//
//   - a path whose first element is itself a host name (a module path:
//     pkg.go.dev/github.com/owner/repo/..., a module proxy, a godoc
//     mirror): the project is that host and its owner and repository on a
//     code forge, else the host and one element (go.uber.org/zap);
//   - a package index's own docs host, as resolve knows them
//     (indexHosted): on pkg.go.dev a path with no host first is a
//     standard-library package, the project its whole import path
//     (pkg.go.dev/net/http/); docs.rs and pythonhosted.org keep one
//     project per first element.
func sharedProjectRoot(u *url.URL) (webRoot, bool) {
	segs := pathSegments(u.Path)
	if len(segs) == 0 {
		return webRoot{}, false
	}
	for i, s := range segs {
		if v, _, ok := strings.Cut(s, "@"); ok {
			segs[i] = v // pkg.go.dev/net/http@go1.22.0 is net/http's page
		}
	}
	keep := 0
	switch {
	case hostLikeSegment(segs[0]):
		keep = 2
		if isNonDocsHost(segs[0]) {
			keep = 3
		}
	case indexHosted(u) && scopeHost(u) == "pkg.go.dev":
		keep = len(segs)
		if !strings.HasSuffix(u.Path, "/") && isPageFile(segs[len(segs)-1]) {
			keep--
		}
	case indexHosted(u):
		if scopeHost(u) == "docs.rs" {
			return webRoot{}, false // docsTree's version segment roots each crate already
		}
		keep = 1
	default:
		return webRoot{}, false
	}
	if keep <= 0 || keep > len(segs) {
		return webRoot{}, false
	}
	r, err := parseWebRoot(u.Scheme + "://" + u.Host + segmentsPath(segs[:keep]))
	if err != nil {
		return webRoot{}, false
	}
	return r, true
}

// hostLikeRe is a path element that is a host name: labels with a dot and
// a final label of letters (github.com, go.uber.org), not a version (v1.2)
// or a page file.
var hostLikeRe = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)

func hostLikeSegment(s string) bool {
	s = strings.ToLower(s)
	return hostLikeRe.MatchString(s) && !isPageFile(s) && !isLLMSFile(s)
}

// namedProjectRoot is the root the library name, written as a path, gives
// a page on a host that serves several projects: the name's elements as
// the page path's leading ones (net/http for pkg.go.dev/net/http/httputil),
// or a single name as its first element (phoenix in hexdocs.pm/phoenix/...)
// when the hits show the host serving several projects (shared: their
// first elements differ) and the name is no label of the host itself
// (react on react.dev/reference/react/ is the site's own name).
func namedProjectRoot(u *url.URL, name string, shared bool) (webRoot, bool) {
	segs := pathSegments(u.Path)
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" || len(segs) == 0 {
		return webRoot{}, false
	}
	parts := strings.Split(strings.ToLower(name), "/")
	keep := 0
	switch {
	case len(parts) > 1 && len(parts) <= len(segs):
		ok := true
		for i, p := range parts {
			seg, _, _ := strings.Cut(strings.ToLower(segs[i]), "@")
			ok = ok && seg == p
		}
		if ok {
			keep = len(parts)
		}
	case len(parts) == 1 && shared && len(segs) > 1:
		k := libraryKey(parts[0])
		if len(k) >= 3 && libraryKey(segs[0]) == k && !named(&url.URL{Host: u.Host}, []string{k}) {
			keep = 1
		}
	}
	if keep == 0 {
		return webRoot{}, false
	}
	r, err := parseWebRoot(u.Scheme + "://" + u.Host + segmentsPath(segs[:keep]))
	if err != nil {
		return webRoot{}, false
	}
	return r, true
}

// registryNames reports a shared host's project root whose project the
// library name is: the import path itself (net/http), or the last element
// of its path (docs.rs/chrono/latest/, pkg.go.dev/github.com/owner/viper/).
func registryNames(root webRoot, name string) bool {
	if name == "" || root.u == nil {
		return false
	}
	_, shared := sharedProjectRoot(root.u)
	if !shared && !indexHosted(root.u) {
		return false
	}
	segs := pathSegments(root.prefix)
	if len(segs) == 0 {
		return false
	}
	if strings.EqualFold(strings.Join(segs, "/"), strings.Trim(name, "/")) {
		return true
	}
	if scopeHost(root.u) == "docs.rs" {
		return libraryKey(segs[0]) == libraryKey(lastPart(name))
	}
	k := libraryKey(lastPart(name))
	return len(k) >= 2 && libraryKey(segs[len(segs)-1]) == k
}

// siteOrigin is the origin of an answer from site, read for req:
//
//   - verified when the library value is a URL the site is at or under;
//     when the site is the root resolve found and the search results
//     agree on it (resolvedOrigin), or another site that is that
//     project's own (siteOfProject: a domain its metadata, Go module path
//     or repository owner names); when the search results agree on a
//     package index's page for the very name asked (registryNames); or
//     when the library name, or for a question with no library one of its
//     words, is its domain and its address says it is docs (docsSite);
//   - by name when only a registry package of the name ties the site to
//     it (resolvedOrigin);
//   - otherwise unverified, naming the host and what was asked.
//
// prefs.lead is the site the search results agree on, chosen without
// resolve's preference: agreement must not come from that preference.
func siteOrigin(site sfSite, req sfRequest, prefs sitePrefs) sfOrigin {
	if isHTTPURL(req.library) {
		if r, err := parseWebRoot(req.library); err == nil && (r.contains(site.root.u) || site.root.contains(r.u)) {
			return verifiedOrigin("the library value names this site")
		}
	}
	if m := prefs.resolved.match; m != nil && m.root.u != nil {
		if m.root.contains(site.root.u) || site.root.contains(m.root.u) {
			return resolvedOrigin(m, prefs.lead)
		}
		if why, ok := siteOfProject(site.root, m); ok {
			return verifiedOrigin(why)
		}
	}
	name := req.spec.name
	if name == "" && prefs.resolved.bare {
		name = prefs.resolved.name
	}
	if registryNames(site.root, name) {
		return verifiedOrigin(fmt.Sprintf("the search results agree on %s's page for %s", scopeHost(site.root.u), name))
	}
	if _, shared := sharedProjectRoot(site.root.u); !shared && !indexHosted(site.root.u) && docsSite(site) {
		key := registrableDomain(site.root.u.Hostname())
		if ownsDomain(key, prefs.names) || namedWithDescriptor(key, prefs.names) {
			return verifiedOrigin("the library name is its domain " + key)
		}
		if req.library == "" && ownsDomain(key, prefs.words) {
			return verifiedOrigin("the question names its domain " + key)
		}
	}
	return unverifiedOrigin(scopeHost(site.root.u), askedAbout(req))
}

// namedWithDescriptor reports a domain whose first label is a name with a
// descriptor word after it (jqlang.org for jq, djangoproject.com for
// django): descriptorWords, and lang for a language's or tool's own site.
func namedWithDescriptor(domain string, names []string) bool {
	label, _, _ := strings.Cut(domain, ".")
	label = libraryKey(label)
	for _, n := range names {
		rest, ok := strings.CutPrefix(label, n)
		if ok && len(n) >= 2 && rest != "" && (rest == "lang" || descriptorWords[rest]) {
			return true
		}
	}
	return false
}

// docsSite reports a site that says it is documentation by its address:
// a docs host or package docs host, or a docs directory (strongDocs), at
// its root or at one of its hits. A title is not enough: a tutorial
// site's posts are titled guides too. A domain named like the thing
// asked about is the thing's own docs only with this; a fan site's
// domain names the thing as well (raspberry.tips).
func docsSite(site sfSite) bool {
	if strongDocs(site.root.u) {
		return true
	}
	return slices.ContainsFunc(site.hits, func(h sfHit) bool { return strongDocs(h.u) })
}

// askedAbout names what a request asks about, for an unverified origin:
// its library value; else the question's first firm reading (a name
// its syntax puts there, not a guess or a common word); else the
// question.
func askedAbout(req sfRequest) string {
	if req.library != "" {
		return req.library
	}
	words := strings.Fields(req.query)
	for _, sp := range splitLibraryQuery(req.query) {
		if !sp.strict && !sp.common && sp.spec.url == "" && sp.from < sp.to && sp.to <= len(words) {
			return strings.Trim(strings.Join(words[sp.from:sp.to], " "), ".,;:!?\"'()")
		}
	}
	return fmt.Sprintf("%q", strings.Join(words, " "))
}

// readFail is one planned page that gave no text, and why.
type readFail struct {
	page  string
	cause string // readNotFound, readBlocked, ...
}

// Causes of a page read that gave no text.
const (
	readNotFound   = "not found"
	readBlocked    = "blocked"
	readJS         = "needs JavaScript"
	readEmpty      = "empty after extraction"
	readTimeout    = "timed out"
	readRedirected = "redirected outside the root"
	readOther      = "failed"
)

// httpStatuser is a fetch error that carries its HTTP status
// (scrape.HTTPStatusError).
type httpStatuser interface{ HTTPStatus() int }

// readCause classifies a page fetch that failed.
func readCause(err error) (cause, detail string) {
	var st httpStatuser
	var se *scopeError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return readTimeout, ""
	case errors.As(err, &st):
		switch code := st.HTTPStatus(); {
		case code == 404 || code == 410:
			return readNotFound, fmt.Sprintf("HTTP %d", code)
		case code == 401 || code == 403 || code == 407 || code == 429 || code == 451:
			return readBlocked, fmt.Sprintf("HTTP %d", code)
		default:
			return readOther, fmt.Sprintf("HTTP %d", code)
		}
	case errors.As(err, &se):
		return readRedirected, se.reason()
	case errors.Is(err, ErrNotFound):
		return readNotFound, ""
	}
	return readOther, ""
}

// readFailNote says why none of the pages of root could be read, cause by
// cause with counts, and what the caller can do next: ""-free, always
// one clause per cause, then the next steps.
func readFailNote(root webRoot, fails []readFail, details map[string]string) string {
	counts := map[string]int{}
	var order []string
	for _, f := range fails {
		if counts[f.cause] == 0 {
			order = append(order, f.cause)
		}
		counts[f.cause]++
	}
	var parts, next []string
	for _, c := range order {
		label := c
		if c == readJS && counts[c] > 1 {
			label = "need JavaScript"
		}
		part := fmt.Sprintf("%d %s", counts[c], label)
		if d := details[c]; d != "" {
			part += " (" + d + ")"
		}
		parts = append(parts, part)
		switch c {
		case readBlocked:
			next = append(next, "the site refuses automated readers: configure browser to retry its pages in a browser, or pass another docs root as --library")
		case readJS:
			next = append(next, "the pages need JavaScript: configure browser for full content")
		case readNotFound:
			next = appendNext(next, root)
		case readRedirected:
			next = append(next, "pass the address they redirect to as --library")
		case readEmpty:
			next = append(next, "the pages hold no text ketch can extract: pass another docs root as --library")
		case readTimeout:
			next = append(next, "the site did not answer in time: retry")
		default:
			next = append(next, "pass another docs root as --library")
		}
	}
	return strings.Join(parts, ", ") + "; " + strings.Join(uniqueStrings(next...), "; ")
}

// appendNext is the next step for pages that are gone: the site's parent
// tree, and indexing, which reads the site's own page lists.
func appendNext(next []string, root webRoot) []string {
	if p := parentRoot(root); p != "" {
		return append(next, "pass the site's parent "+p+" as --library, or ketch docs index "+p+" to read its page lists")
	}
	return append(next, "ketch docs index "+root.String()+" reads its page lists")
}

// parentRoot is the docs tree above root ("" at a host's root).
func parentRoot(root webRoot) string {
	segs := pathSegments(root.prefix)
	if len(segs) == 0 {
		return ""
	}
	if t := docsTree(root.u); t.String() != root.String() && t.contains(root.u) {
		return t.String()
	}
	return root.origin() + segmentsPath(segs[:len(segs)-1])
}
