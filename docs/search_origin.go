package docs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
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

// unverifiedOrigin is the origin of an answer from host that nothing ties
// to what was asked: what, or this question when what is "".
func unverifiedOrigin(host, what string) sfOrigin {
	if what == "" {
		return sfOrigin{text: fmt.Sprintf("%s, ketch could not confirm %s as the official documentation for this question", originUnverified, host)}
	}
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
	if lead != nil && ownSiteStrong(*lead, m) {
		return verifiedOrigin(matchEvidence(m) + ", and the search results agree")
	}
	return byNameOrigin(m, lead)
}

// sfResolved is what resolve made of the library a request names, found
// while the question was searched.
type sfResolved struct {
	name      string                 // the name resolved, as written
	match     *webMatch              // the one docs root resolve verified
	ambiguous *AmbiguousLibraryError // several verified, none ahead
	// std are the standard-library pages the name is (Go's, Python's),
	// candidates of an ambiguity that a search agreeing on one settles.
	std []*webMatch
	// candidates are every library resolve knows by the name, verified or
	// not: what a refusal lists when the search agrees on no site.
	candidates []LibraryMatch
	bare       bool // a reading of a bare question (resolved strictly)
	took       time.Duration
}

// goStdRe is a name shaped like a Go standard-library import path
// (context, net/http): lower-case letters and digits, no dot. pkg.go.dev's
// own page decides whether it is one.
var goStdRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:/[a-z][a-z0-9]*)*$`)

// resolveGoStd verifies a standard-library package by the Go package
// index's own page for it: pkg.go.dev serves an import path whose first
// element has no dot only for the standard library, at that path, titled
// "<name> package - <path> - Go Packages" (one element too: context, fmt).
// One page read; no registry.
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
		!strings.Contains(prof.title, " - "+name+" - ") || !strings.HasSuffix(strings.TrimSpace(prof.title), "Go Packages") {
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
	var stds []*webMatch
	var wg sync.WaitGroup
	if !bare && spec.version == "" {
		goName := goStdRe.MatchString(strings.TrimPrefix(spec.raw, "go:")) && (spec.eco == ecoAny || spec.eco == ecoGitHub || strings.HasPrefix(spec.raw, "go:"))
		pyName := spec.eco == ecoAny && pyStdRe.MatchString(spec.raw)
		found := make([]*webMatch, 2)
		if goName {
			wg.Go(func() {
				if m, ok := s.web.resolveGoStd(ctx, spec.raw); ok {
					found[0] = &m
				}
			})
		}
		if pyName {
			wg.Go(func() {
				if m, ok := s.web.resolvePyStd(ctx, spec.raw); ok {
					found[1] = &m
				}
			})
		}
		m, err := s.web.resolveOne(ctx, spec, bare)
		wg.Wait()
		for _, f := range found {
			if f != nil {
				stds = append(stds, f)
			}
		}
		return combineStd(out, stds, m, err, req.query)
	}
	m, err := s.web.resolveOne(ctx, spec, bare)
	return combineStd(out, nil, m, err, req.query)
}

// combineStd is what resolve's answer (m, err) and the standard-library
// pages the name is (stds) make of a library name. A standard-library
// page goes ahead of a namesake in its own language's registry (a Go
// module whose last element is the word, a PyPI package such as an old
// backport); beside a library of another ecosystem it is one candidate
// of an ambiguity, which a language word of the question or the search's
// agreement settles. Every library resolve knows by the name is kept as
// a candidate for a refusal.
func combineStd(out sfResolved, stds []*webMatch, m webMatch, err error, query string) sfResolved {
	out.std = stds
	var amb *AmbiguousLibraryError
	var others []LibraryMatch
	switch {
	case err == nil && m.root.u != nil:
		out.candidates = []LibraryMatch{m.libraryMatch()}
		if len(stds) == 0 {
			out.match = &m
			return out
		}
		others = out.candidates
	case errors.As(err, &amb):
		out.candidates = slices.Clone(amb.Matches)
		others = amb.Matches
	}
	all, others := stdAhead(stds, others)
	if len(stds) > 0 {
		out.candidates = append(slices.Clone(all[:len(stds)]), out.candidates...)
	}
	switch {
	case len(stds) == 1 && len(others) == 0:
		out.match = stds[0] // the standard library's, whatever else goes by the name
		return out
	case len(stds) == 0 && amb == nil:
		return out
	case out.bare:
		return out
	}
	if i, ok := languagePick(all, query); ok {
		if i < len(stds) {
			out.match = stds[i]
		} else {
			c := all[i]
			out.match = &webMatch{root: rootOf(c.ID), title: c.Title, trust: c.TrustScore, evidence: evidenceOf(c.Evidence), packages: c.Packages, stars: -1}
		}
		return out
	}
	name := out.name
	if amb != nil {
		name = amb.Library
	}
	out.ambiguous = &AmbiguousLibraryError{Library: name, Matches: all}
	if len(stds) == 0 {
		out.ambiguous = amb
	}
	return out
}

// stdAhead is the standard-library pages as candidates ahead of the
// others, and those others left once a web root's namesakes of the pages
// go (stdNamesake).
func stdAhead(stds []*webMatch, others []LibraryMatch) ([]LibraryMatch, []LibraryMatch) {
	if len(stds) > 0 {
		others = slices.DeleteFunc(slices.Clone(others), func(c LibraryMatch) bool { return !isHTTPURL(c.ID) || stdNamesake(c, stds) })
	}
	var all []LibraryMatch
	for _, sm := range stds {
		all = append(all, sm.libraryMatch())
	}
	all = append(all, others...)
	return all, others
}

// stdNamesake reports a candidate that a standard-library page found for
// the name goes ahead of: a Go module beside Go's package, a PyPI-only
// package beside Python's module.
func stdNamesake(c LibraryMatch, stds []*webMatch) bool {
	for _, sm := range stds {
		switch {
		case sm.root.u != nil && scopeHost(sm.root.u) == "pkg.go.dev" && slices.ContainsFunc(c.Packages, func(p string) bool { return strings.HasPrefix(p, "go:") }):
			return true
		case sm.root.u != nil && scopeHost(sm.root.u) == "docs.python.org" && onlyPyPI(c.Packages):
			return true
		}
	}
	return false
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
		python := ecos[0] == ecoPyPI && strings.HasPrefix(m.ID, "https://docs.python.org/")
		if !python && !slices.ContainsFunc(m.Packages, func(p string) bool { return strings.HasPrefix(p, string(ecos[0])+":") }) {
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
		for _, sm := range p.resolved.std {
			if stdAgrees(sm, sites[0].root) {
				p.resolved.match = sm // the search agrees on the language's own page
				return nil
			}
		}
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
	if sameTree(root, m.root) {
		return matchEvidence(m), true
	}
	host := root.u.Hostname()
	dom := registrableDomain(host)
	if _, shared := sharedProjectRoot(m.root.u); !shared && !indexHosted(m.root.u) && registrableDomain(m.root.u.Hostname()) == dom {
		return fmt.Sprintf("on %s, the site of %s", dom, matchEvidence(m)), true
	}
	if pkg, ok := registryDocsOf(root, m.packages); ok {
		return fmt.Sprintf("%s's docs of %s, the package resolve found", scopeHost(root.u), pkg), true
	}
	p := m.proj
	if p == nil || isNonDocsHost(host) || indexHosted(root.u) {
		return "", false
	}
	return projectDomain(dom, p)
}

// projectDomain reports dom as a domain the project's own metadata
// publishes (a homepage, a docs URL, a Go module's host) or its
// repository's owner is named by, and why.
func projectDomain(dom string, p *project) (string, bool) {
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
	if ownerNamesDomain(dom, p) {
		return fmt.Sprintf("the domain %s carries the name of %s, owner of github.com/%s", dom, p.repo.owner, p.repo.String()), true
	}
	return "", false
}

// sameTree reports two docs roots one of which holds the other, except
// a package index's whole host over one project's path: pkg.go.dev/ is
// not pkg.go.dev/fmt/'s tree.
func sameTree(a, b webRoot) bool {
	over := func(x, y webRoot) bool {
		return x.contains(y.u) && (x.prefix != "/" || y.prefix == "/" || !indexHosted(x.u))
	}
	return over(a, b) || over(b, a)
}

// stdAgrees reports a site the search agrees on that is the
// standard-library page's own docs: any tree of docs.python.org for
// Python's (the search often lands on another version's), the package's
// path or under it on pkg.go.dev.
func stdAgrees(sm *webMatch, lead webRoot) bool {
	if sm.root.u == nil || lead.u == nil {
		return false
	}
	if scopeHost(sm.root.u) == "docs.python.org" {
		return scopeHost(lead.u) == "docs.python.org"
	}
	return sm.root.contains(lead.u)
}

// ownerNamesDomain reports a domain whose first label is the name of the
// project's repository owner (hashicorp.com for github.com/hashicorp/...):
// a name match, so siteOfProject's weakest tie, never verification alone.
func ownerNamesDomain(dom string, p *project) bool {
	owner := libraryKey(p.repo.owner)
	label, _, _ := strings.Cut(dom, ".")
	return len(owner) >= 3 && libraryKey(label) == owner
}

// ownSiteStrong reports siteOfProject's tie of root to m when it rests on
// more than a name: anything but the owner's name in the domain.
func ownSiteStrong(root webRoot, m *webMatch) bool {
	if _, ok := siteOfProject(root, m); !ok {
		return false
	}
	if sameTree(root, m.root) {
		return true
	}
	dom := registrableDomain(root.u.Hostname())
	if _, shared := sharedProjectRoot(m.root.u); !shared && !indexHosted(m.root.u) && registrableDomain(m.root.u.Hostname()) == dom {
		return true
	}
	if _, ok := registryDocsOf(root, m.packages); ok {
		return true
	}
	return m.proj != nil && metadataNamesDomain(dom, m.proj)
}

// registryDocsOf reports a docs tree that is a package index's docs of
// one of packages (docs.rs/<crate>/ for crates:<crate>, pkg.go.dev/<module>
// for go:<module>), and which.
func registryDocsOf(root webRoot, packages []string) (string, bool) {
	segs := pathSegments(root.prefix)
	if root.u == nil || len(segs) == 0 {
		return "", false
	}
	norm := func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "_", "-") }
	for _, p := range packages {
		eco, name, ok := strings.Cut(p, ":")
		switch {
		case !ok:
		case eco == string(ecoCrates) && scopeHost(root.u) == "docs.rs" && norm(segs[0]) == norm(name):
			return p, true
		case eco == string(ecoGo) && scopeHost(root.u) == "pkg.go.dev" && strings.HasPrefix(strings.Join(segs, "/")+"/", name+"/"):
			return p, true
		}
	}
	return "", false
}

// metadataNamesDomain reports a domain the project's own metadata
// publishes: a homepage or docs URL, a Go module path's host.
func metadataNamesDomain(dom string, p *project) bool {
	for _, a := range ownerURLs(p) {
		if !isNonDocsHost(a.url.Hostname()) && !indexHosted(a.url) && registrableDomain(a.url.Hostname()) == dom {
			return true
		}
	}
	for _, pkg := range p.pkgs {
		first, _, _ := strings.Cut(pkg.name, "/")
		if pkg.eco == ecoGo && strings.Contains(first, ".") && !isNonDocsHost(first) && registrableDomain(first) == dom {
			return true
		}
	}
	return false
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
		if nameLeads(parts, segs) {
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

// nameLeads reports the name's elements (parts) as the path's leading
// segments, a segment's @version aside.
func nameLeads(parts, segs []string) bool {
	ok := true
	for i, p := range parts {
		seg, _, _ := strings.Cut(strings.ToLower(segs[i]), "@")
		ok = ok && seg == p
	}
	return ok
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
//   - otherwise unverified, naming the host and, when something says the
//     question names it, what was asked (askedAbout).
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
		if sameTree(site.root, m.root) {
			return resolvedOrigin(m, prefs.lead)
		}
		if why, ok := siteOfProject(site.root, m); ok {
			if ownSiteStrong(site.root, m) {
				return verifiedOrigin(why)
			}
			return byNameText(why + "; nothing else confirms it as the official documentation")
		}
	}
	name := req.spec.name
	if name == "" && prefs.resolved.bare {
		name = prefs.resolved.name
	}
	if registryNames(site.root, name) {
		return registryOrigin(site, name)
	}
	if org, ok := domainOrigin(site, req, prefs); ok {
		return org
	}
	return unverifiedOrigin(scopeHost(site.root.u), askedAbout(site, req, prefs))
}

// registryOrigin is the origin of a package index's page for the very
// name asked (registryNames): verified for Go's standard library on
// pkg.go.dev, by name otherwise.
func registryOrigin(site sfSite, name string) sfOrigin {
	if segs := pathSegments(site.root.prefix); scopeHost(site.root.u) == "pkg.go.dev" && len(segs) > 0 && !strings.Contains(segs[0], ".") && strings.Join(segs, "/") == strings.Trim(name, "/") {
		// pkg.go.dev serves a path with no host first for the standard
		// library only: resolveGoStd's evidence, from the search's hit.
		return verifiedOrigin(fmt.Sprintf("pkg.go.dev documents %s in the Go standard library", name))
	}
	return byNameText(fmt.Sprintf("%s documents a package named %s at its own path; nothing else confirms it is the one meant", scopeHost(site.root.u), name))
}

// domainOrigin reports the by-name origin of a site on a domain of its own
// that carries the name asked.
func domainOrigin(site sfSite, req sfRequest, prefs sitePrefs) (sfOrigin, bool) {
	if _, shared := sharedProjectRoot(site.root.u); !shared && !indexHosted(site.root.u) {
		key := registrableDomain(site.root.u.Hostname())
		var by string
		switch {
		case ownsDomain(key, prefs.names) || namedWithDescriptor(key, prefs.names):
			by = req.spec.name
		case hostValue(req.library) && registrableDomain(strings.ToLower(req.library)) == key:
			by = req.library // a library value written as a host (fly.io)
		case req.library == "" && ownsDomain(key, prefs.words):
			by = domainWord(key, prefs.words)
		}
		if by != "" {
			return byNameText(fmt.Sprintf("the domain %s carries the name %s; nothing else confirms it as the official documentation", scopeHost(site.root.u), by)), true
		}
	}
	return sfOrigin{}, false
}

// hostValue reports a library value written as a bare host name: a dot,
// no scheme, path or space.
func hostValue(v string) bool {
	v = strings.TrimSpace(v)
	return strings.Contains(v, ".") && !strings.ContainsAny(v, " /:@") && hostLikeSegment(v)
}

func byNameText(why string) sfOrigin { return sfOrigin{text: originByName + ", " + why} }

// domainWord is the question word a domain's first label is (ownsDomain's
// match), for the origin line; the label itself when none is found.
func domainWord(domain string, words []string) string {
	label, _, _ := strings.Cut(strings.TrimPrefix(domain, "www."), ".")
	for _, l := range []string{label, withoutLanguageWord(label)} {
		if k := libraryKey(l); slices.Contains(words, k) {
			return l
		}
	}
	return label
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

// askedAbout names what a request asks about, for an unverified origin
// read from site: its library value; else the question's first firm
// reading (a name its syntax puts there, not a guess or a common word)
// when something says the question names it: resolve found a project by
// it, or the site carries it in its host or docs directory (named) or in
// the title of a search hit on it (its domain's other trees' hits too).
// Else "": the line names nothing rather than a word of the question that
// only looks like a name.
func askedAbout(site sfSite, req sfRequest, prefs sitePrefs) string {
	if req.library != "" {
		return req.library
	}
	name := firmReading(req.query)
	if name == "" {
		return ""
	}
	key := libraryKey(name)
	hits := site.hits
	for _, a := range site.alts {
		hits = slices.Concat(hits, a.hits)
	}
	resolved := prefs.resolved.match != nil && libraryKey(prefs.resolved.name) == key
	if resolved || named(site.root.u, []string{key}) || titlesCarry(hits, key) {
		return name
	}
	return ""
}

// firmReading is the question's first firm reading as written ("" for
// none): a name its syntax puts there, not a guess or a common word.
func firmReading(query string) string {
	words := strings.Fields(query)
	for _, sp := range splitLibraryQuery(query) {
		if !sp.strict && !sp.common && sp.spec.url == "" && sp.from < sp.to && sp.to <= len(words) {
			return strings.Trim(strings.Join(words[sp.from:sp.to], " "), ".,;:!?\"'()")
		}
	}
	return ""
}

// titlesCarry reports a hit whose title has key (libraryKey) as a word of
// its own, or two adjacent words run together: "Amazon S3 multipart upload
// limits" carries s3, "Cross-document view transitions are opt-in" no opt.
func titlesCarry(hits []sfHit, key string) bool {
	if len(key) < 2 {
		return false
	}
	for _, h := range hits {
		words := strings.FieldsFunc(h.title, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.'
		})
		for i, w := range words {
			if libraryKey(w) == key || i+1 < len(words) && libraryKey(w+words[i+1]) == key {
				return true
			}
		}
	}
	return false
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
		switch code := st.HTTPStatus(); code {
		case 404, 410:
			return readNotFound, fmt.Sprintf("HTTP %d", code)
		case 401, 403, 407, 429, 451:
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
