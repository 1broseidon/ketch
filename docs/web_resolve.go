// Owner: chunk B (resolve core).

package docs

import (
	"cmp"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/net/publicsuffix"

	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/search"
)

// Resolution budgets and caps (plan §4).
const (
	webSearchTimeout     = 10 * time.Second // the last-resort web search
	webMaxProjects       = 5                // projects verified
	webMaxRoots          = 6                // candidate roots verified
	webMinSearchRoots    = 3                // roots left for GitHub search hits, whatever the registries used
	webMaxVersioned      = 3                // matches whose versions are computed
	webSearchRepos       = 10               // GitHub search results read
	webMaxSearchProjects = 3                // projects taken from GitHub search
	webSearchHits        = 8                // last-resort web search results read
	webMaxSearchRoots    = 4                // roots taken from them
	webMaxHitRepos       = 2                // repositories looked up from their back-links
	webMaxURLRepos       = 2                // repositories a docs URL's links are checked against
	webMaxReasons        = 12               // rejection reasons an error lists
	webCandidateWait     = 4 * time.Second  // how long verification waits for every project's candidate roots
	dominanceStars       = 1000
	dominanceRatio       = 50
)

// project is one library as the registries and GitHub see it: the packages
// that name one repository, or a lone package that names none.
type project struct {
	// key identifies the project: repo.spec() when it has a GitHub repo,
	// else the package's spec ("pypi:click").
	key  string
	pkgs []pkgInfo
	// repo is the GitHub repository the packages name, canonical once
	// GitHub answered; zero for none.
	repo    repoKey
	aliases []repoKey // other keys for repo (renamed from), for back-link matching
	info    *repoInfo // nil when GitHub was not asked or did not answer
	// repoGone marks a repository GitHub says does not exist (a 404): the
	// project has no stars to count, as opposed to stars unknown.
	repoGone bool
	// repoErr is why GitHub could not read the repository the packages
	// claim (a rate limit, an outage), nil when it answered or was not
	// asked. Such a project is unresolved rather than absent: GitHub
	// decides whether its claim stands, its homepage and its stars.
	repoErr error
	notes   []evidence
	exact   bool // a package's normalized name is the queried name
	// fromSearch marks a project found by GitHub repository search rather
	// than a registry.
	fromSearch bool
	// fromHits marks a project made from a repository the web search's hits
	// link to: the hits named it, so they cannot vouch for themselves
	// through it (relatedEvidence).
	fromHits bool
	// pop is the project's registry popularity in weekly downloads (or
	// their equivalent, popularity.go), the keyless tie-break between
	// same-name projects; popKnown says it was read, popErr why not.
	pop      int64
	popKnown bool
	popErr   error
	// rivalOnly marks a project weighed against a pick but never verified
	// itself: deprecated npm packages of the name (registryProjects).
	rivalOnly bool
}

// label names the project in reasons: its first package ("pypi click"),
// else its repository ("github solidjs/solid").
func (p *project) label() string {
	if len(p.pkgs) > 0 {
		return pkgLabel(p.pkgs[0])
	}
	return "github " + p.repo.String()
}

// pkgLabel is "pypi click".
func pkgLabel(pkg pkgInfo) string { return string(pkg.eco) + " " + pkg.name }

// isRepo reports k as the project's repository or one of its aliases.
func (p *project) isRepo(k repoKey) bool {
	return !p.repo.isZero() && (k == p.repo || slices.Contains(p.aliases, k))
}

// isOwner reports owner as the owner of the repository or an alias.
func (p *project) isOwner(owner string) bool {
	if p.repo.owner == owner {
		return true
	}
	return slices.ContainsFunc(p.aliases, func(a repoKey) bool { return a.owner == owner })
}

// isRepoName reports a repository name the project's repository goes by.
func (p *project) isRepoName(name string) bool {
	if p.repo.name == name {
		return true
	}
	return slices.ContainsFunc(p.aliases, func(a repoKey) bool { return a.name == name })
}

// names are the names a site would call the project by: its packages'
// (the last path element of a scoped or module name), else its
// repository's. Lower case.
func (p *project) names() []string {
	var out []string
	for _, pkg := range p.pkgs {
		n := strings.ToLower(pkg.name)
		out = append(out, n[strings.LastIndexByte(n, '/')+1:])
	}
	if len(out) == 0 && !p.repo.isZero() {
		out = append(out, p.repo.name)
	}
	return uniqueStrings(out...)
}

// specs are the project's package specs, then its repository's.
func (p *project) specs() []string {
	var out []string
	for _, pkg := range p.pkgs {
		out = append(out, pkg.spec())
	}
	if !p.repo.isZero() {
		out = append(out, p.repo.spec())
	}
	return uniqueStrings(out...)
}

// stars is the project's GitHub star count, the popularity dominance
// weighs (dominant). It is 0 when GitHub answered that the repository the
// packages name does not exist: there is nothing to count. It is -1,
// unknown, whenever no count was read: the packages name no repository
// (old PyPI packages often list only a homepage), it is hosted elsewhere
// (GitLab), or GitHub was not asked, refused (a rate limit) or failed.
//
// In a keyless resolution, where GitHub is never asked, it is the
// project's registry popularity in weekly downloads instead (popKnown,
// web_popularity.go): the same role, decided the same way.
func (p *project) stars() int {
	switch {
	case p.info != nil:
		return p.info.stars
	case p.repoGone:
		return 0
	case p.popKnown:
		return int(min(p.pop, int64(maxWeight)))
	}
	return -1
}

// maxWeight caps a weight so it fits an int anywhere.
const maxWeight = 1<<31 - 1

// weightUnread reports a weight a source failed to report: GitHub's stars
// (a rate limit, an outage), or, keyless, a registry's download count.
func (p *project) weightUnread() bool {
	return p.info == nil && (p.repoErr != nil || p.popErr != nil)
}

// weightText is a project's weight as an error states it: stars, or
// weekly downloads in a keyless resolution.
func weightText(p *project, n int) string {
	if p != nil && (p.popKnown || p.popErr != nil) {
		return popularityText(int64(n))
	}
	return starsText(n)
}

// authURL is a URL the project's own metadata publishes.
type authURL struct {
	url *url.URL
	src string // "npm next homepage"
}

// authoritative lists the project's own published URLs that could be a
// docs site: package docs URLs and homepages, and a non-fork repository's
// homepage.
func (p *project) authoritative() []authURL {
	var out []authURL
	add := func(raw, src string) {
		if u, reason := checkMetaURL(raw); reason == "" {
			out = append(out, authURL{url: u, src: src})
		}
	}
	for _, pkg := range p.pkgs {
		add(pkg.docsURL, pkgLabel(pkg)+" documentation")
		add(pkg.homepage, pkgLabel(pkg)+" homepage")
	}
	if p.info != nil && !p.info.fork {
		add(p.info.homepage, "github "+p.repo.String()+" homepage")
	}
	return out
}

// rootCandidate is a docs root to verify for a project, with the evidence
// that put it forward.
type rootCandidate struct {
	url      *url.URL
	evidence []evidence
	registry bool // registry-hosted docs (docs.rs, pkg.go.dev)
	search   bool // a last-resort web search hit, verifiable only by V4
	// guess marks a root guessed beside a homepage that shows no docs link
	// (guessedDocsRoot), verifiable only by V4 like a search hit.
	guess bool
	// offsite marks a homepage's docs link to another registrable domain
	// (foo.org's "Documentation" to foo.readthedocs.io, or to a Google Doc):
	// it verifies only when the root links back to the repository.
	offsite bool
	// page is a page of the root the homepage's nav links to, read when
	// the root has no page of its own (playwright.dev/docs/ is a 404, its
	// nav's /docs/intro is not); nil for none.
	page *url.URL
	// namedBy are the owner-published URLs (linkKey) that put the root
	// forward: a docs URL naming it, a homepage that is it or links to it.
	// They are the metadata edge itself, so none of them is also the DNS
	// edge (relatedHostEvidence).
	namedBy []string
}

// v4Only reports a candidate that only V4 can verify: on the project's
// own domain and linking back to its repository.
func (c rootCandidate) v4Only() bool { return c.search || c.guess }

// webMatch is a verified docs root for a project; libraryMatch gives its
// public form.
type webMatch struct {
	root        webRoot
	title       string
	description string
	trust       float64
	evidence    []evidence
	packages    []string // the project's specs, usable as the library
	version     string   // the requested version's label, else the current one
	versions    []string // labels, current first
	// project is the project's key: matches sharing it are one library (a
	// guide and an API reference), never an ambiguity.
	project string
	stars   int  // the project's GitHub stars; -1 when unknown
	primary bool // the project's best non-registry root, not a secondary match
	// indexOnly marks a project whose every verified root is hosted by a
	// package index (indexHosted): it has no docs site of its own.
	indexOnly bool
	// starsUnread marks unknown stars GitHub failed to report (a rate
	// limit, an outage), as opposed to a project with nothing to count.
	starsUnread bool

	registry bool // registry-hosted docs
	fromHit  bool // a web search hit's root, which no metadata named
	// oldRepo marks a root that links to its repository only at an address
	// the repository has since moved from (withMovedRepos): docs left
	// behind when the project moved, such as an archive of old majors.
	oldRepo bool
	proj    *project     // nil for a docs URL the caller passed
	prof    *siteProfile // the root's profile
}

// resolveLog collects, across one resolution, why candidates were dropped
// and which sources failed, for the final error's text and classification
// (plan §8). It is safe for concurrent use.
type resolveLog struct {
	rejected []string // "npm click: repo uggedal/click not found"
	failed   []error  // a 5xx, a network error, a rate limit
	// missed are the failures that may have hidden a library: what a
	// single-library context must not answer past (resolveOne).
	missed []missedSource
	// registryFailed records a registry that could not be asked about the
	// name, nameKnown a registry that has a package of that name, and
	// siteFailed a homepage or docs root that did not answer (not GitHub):
	// whether a bare query's reading could have been a library
	// (upstreamError).
	registryFailed, nameKnown, siteFailed bool

	mu    sync.Mutex
	limit *ghRateLimitError // GitHub's first refusal for its rate limit: ask it nothing more
	// offeredSite are the projects whose own metadata put forward a docs
	// root of their own (candidateRoots), not only docs a package index
	// hosts, verified or not, by key.
	offeredSite map[string]bool
	// projects are every project the resolution considered, verified or
	// not: where one library is needed, the pick is checked against those
	// GitHub left unresolved (unreadBeside).
	projects []*project
}

// missedSource is a failure that may have kept a library from matching.
type missedSource struct {
	project string   // the project it concerns; "" for one that could have added any
	proj    *project // that project; nil for a whole source
	// whole marks a source whose failure may have hidden whole projects (a
	// registry, GitHub search), as opposed to one project's site or
	// repository not answering.
	whole bool
	// github marks a project's repository GitHub did not read, as opposed
	// to its site not answering.
	github bool
	err    error
}

func (l *resolveLog) reject(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rejected = append(l.rejected, fmt.Sprintf(format, args...))
}

// failSource records err as the failure of a source that may have hidden
// whole projects: a registry (registry), GitHub search.
func (l *resolveLog) failSource(err error, registry bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.missed = append(l.missed, missedSource{whole: true, err: err})
	l.failed = append(l.failed, err)
	l.registryFailed = l.registryFailed || registry
}

// knowName records that a registry has a package of the name.
func (l *resolveLog) knowName() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nameKnown = true
}

// upstreamError is a resolution that failed for a source, not for want of
// a library: plain text (exit 4, [upstream]), and what the registries said,
// which decides whether a bare query's reading of a library in its words
// stops the query (vetoes) or gives way to the next.
type upstreamError struct {
	msg string
	// registryFailed: a registry could not be asked about the name;
	// nameKnown: a registry has a package of that name; siteFailed: a
	// homepage or docs root did not answer, or the resolution ran out of
	// time; picked: a library verified, but a failed source may have
	// hidden another (answerPastFailures).
	registryFailed, nameKnown, siteFailed, picked bool
}

func (e *upstreamError) Error() string { return e.msg }

// upstreamErr is msg as an upstreamError carrying log's registry outcome;
// timedOut counts as a site failure.
func (l *resolveLog) upstreamErr(msg string, timedOut bool) *upstreamError {
	l.mu.Lock()
	defer l.mu.Unlock()
	return &upstreamError{msg: msg, registryFailed: l.registryFailed, nameKnown: l.nameKnown, siteFailed: l.siteFailed || timedOut}
}

// failFor records err as a failure for p: its homepage, its own package
// or a docs root did not answer.
//
// A project weighed as a rival only (project.rivalOnly) is never the
// library meant: its count not answering hides no library, and
// keylessRivals decides what it means for the pick.
func (l *resolveLog) failFor(p *project, err error) {
	if p.rivalOnly {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.missed = append(l.missed, missedSource{project: p.key, proj: p, err: err})
	l.failed = append(l.failed, err)
	l.siteFailed = true
}

// failRepo records err as a failure for project's GitHub repository: a
// rate limit, an outage. It hid whether the project's claim to the
// repository stands, the repository's homepage and its stars.
func (l *resolveLog) failRepo(project string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.missed = append(l.missed, missedSource{project: project, github: true, err: err})
	l.failed = append(l.failed, err)
}

// missedBeside sorts the failures that may have hidden a library other
// than the matched projects: sources down (whole), and projects whose
// site or repository did not answer (partial). sites leaves out the
// repositories GitHub did not read, which unreadBeside judges instead.
func (l *resolveLog) missedBeside(matched map[string]bool, sites bool) (whole []error, partial []missedSource) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.missed {
		switch {
		case m.whole:
			whole = append(whole, m.err)
		case sites && m.github:
		case !matched[m.project]:
			partial = append(partial, m)
		}
	}
	return whole, partial
}

// rivals returns the failures in partial (sites, roots or packages of
// other projects that did not answer) whose project could have outranked
// or tied pick had it answered, so pick cannot stand past them. When
// pick's docs are only index-hosted (indexOnly), the squatter signal, that
// is every one: such a pick stands past nothing unchecked. Otherwise it is
// each project whose GitHub stars are at least pick's, what decides
// between projects of one name (dominant): gin-gonic/gin's 89k beside
// codegangsta/gin's 4k. A count unknown for want of a repository (a
// package naming none, or one on GitLab) ranks below any known count and
// level with another unknown one, so a junk package's dead homepage beside
// a pick with stars is no rival: the pick stands, with a note. Keyless, a
// project whose download count could not be read (popErr) is a rival
// whatever its count, as one GitHub did not answer for is.
func rivals(pick webMatch, partial []missedSource) []missedSource {
	var out []missedSource
	for _, m := range partial {
		if m.proj != nil && m.proj.fromHits && !m.proj.exact && m.proj.info == nil {
			// Keyless, a project a search hit named under another name
			// (go-django beside django) is no library of the pick's name.
			continue
		}
		if pick.indexOnly || m.proj == nil || m.proj.stars() >= pick.stars || m.proj.popErr != nil {
			out = append(out, m)
		}
	}
	return out
}

// missedErrs are the failures' errors.
func missedErrs(ms []missedSource) []error {
	out := make([]error, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.err)
	}
	return out
}

// consider records projects the resolution looked at.
func (l *resolveLog) consider(projects ...*project) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.projects = append(l.projects, projects...)
}

// unreadBeside returns the projects, other than pick's, whose repository
// GitHub did not read (project.repoErr): each may be the library meant or
// outweigh the pick, had GitHub answered. One whose packages are all the
// pick's (a claim split off its repository) is the pick's own.
func (l *resolveLog) unreadBeside(pick *project) []*project {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []*project
	seen := map[string]bool{}
	for _, p := range l.projects {
		if p.repoErr == nil || seen[p.key] || pick != nil && (p == pick || p.key == pick.key || ownedBy(p, pick)) {
			continue
		}
		seen[p.key] = true
		out = append(out, p)
	}
	return out
}

// ownedBy reports a project with packages, all of them pick's.
func ownedBy(p, pick *project) bool {
	if len(p.pkgs) == 0 {
		return false
	}
	for _, pkg := range p.pkgs {
		if !slices.ContainsFunc(pick.pkgs, func(q pkgInfo) bool { return q.spec() == pkg.spec() }) {
			return false
		}
	}
	return true
}

// offer records that p's metadata put docs roots forward; site that one
// of them is not docs a package index hosts.
func (l *resolveLog) offer(key string, site bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.offeredSite == nil {
		l.offeredSite = map[string]bool{}
	}
	l.offeredSite[key] = l.offeredSite[key] || site
}

// hasOfferedSite reports a project whose metadata put forward a docs root
// of its own, not only docs a package index hosts.
func (l *resolveLog) hasOfferedSite(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.offeredSite[key]
}

// considered is every project the resolution looked at (consider).
func (l *resolveLog) considered() []*project {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.projects)
}

func (l *resolveLog) setLimited(err *ghRateLimitError) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.limit == nil {
		l.limit = err
	}
}

// rateLimited is GitHub's rate-limit refusal, nil while it answers.
func (l *resolveLog) rateLimited() *ghRateLimitError {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

func (l *resolveLog) snapshot() ([]string, []error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.rejected), slices.Clone(l.failed)
}

// rejectError is a candidate turned down for a reason: not a failed
// source. It wraps ErrNotFound.
type rejectError struct{ reason string }

func (e *rejectError) Error() string { return e.reason }
func (e *rejectError) Unwrap() error { return ErrNotFound }

func rejectf(format string, args ...any) error {
	return &rejectError{reason: fmt.Sprintf(format, args...)}
}

// isGone reports a fetch error that says the site is not there (a 404, a
// name that does not resolve, a certificate for some other site, a
// redirect into non-public space), as opposed to one worth retrying.
func isGone(err error) bool {
	var dns *net.DNSError
	var verify *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.Is(err, ErrNotFound) || errors.Is(err, errRefusedRedirect) || errors.As(err, &dns) && dns.IsNotFound ||
		errors.As(err, &verify) || errors.As(err, &unknownCA) || errors.As(err, &hostname) || errors.As(err, &invalid)
}

// logFetch records a failed fetch of what, for p: a rejection when the
// site is gone, else a failed source.
func logFetch(log *resolveLog, p *project, what string, err error) {
	if isGone(err) {
		log.reject("%s: %v", what, err)
		return
	}
	log.failFor(p, fmt.Errorf("%s: %w", what, err))
}

// profileMemo shares site profiles across one resolution: a homepage read
// for its nav links is often the root verified next.
type profileMemo struct {
	mu    sync.Mutex
	calls map[string]*profileCall
	llms  map[string]*llmsCall  // cachedLLMS
	links map[string]*linksCall // memoLinks
}

// linksCall is one memoLinks read.
type linksCall struct {
	done  chan struct{}
	links []string
}

// memoLinks is read(), what u says (a README's links, where a repository
// moved), read once per resolution per URL: every root a project verifies
// may ask.
func (w *Web) memoLinks(ctx context.Context, u *url.URL, read func() []string) []string {
	memo, _ := ctx.Value(profileMemoKey{}).(*profileMemo)
	if memo == nil {
		return read()
	}
	key := u.String()
	memo.mu.Lock()
	if memo.links == nil {
		memo.links = map[string]*linksCall{}
	}
	if c, ok := memo.links[key]; ok {
		memo.mu.Unlock()
		select {
		case <-c.done:
			return c.links
		case <-ctx.Done():
			return nil
		}
	}
	c := &linksCall{done: make(chan struct{})}
	memo.links[key] = c
	memo.mu.Unlock()
	c.links = read()
	close(c.done)
	return c.links
}

type profileCall struct {
	done chan struct{}
	p    *siteProfile
	err  error
}

type profileMemoKey struct{}

// withProfileMemo gives ctx a profile memo, unless it has one: the query
// splits of one Search share theirs.
func withProfileMemo(ctx context.Context) context.Context {
	if _, ok := ctx.Value(profileMemoKey{}).(*profileMemo); ok {
		return ctx
	}
	return context.WithValue(ctx, profileMemoKey{}, &profileMemo{calls: map[string]*profileCall{}})
}

// cachedProfile is profile, fetched once per resolution per URL. The memo
// is keyed by the URL exactly as spelled (its fragment aside), never by a
// looser identity such as linkKey's: concurrent callers spelling one page
// differently (a search hit's http://x.org/docs/ beside a homepage's
// https://www.x.org/docs/) would otherwise race, and whichever arrived
// first would decide which URL was fetched and what every caller read.
func (w *Web) cachedProfile(ctx context.Context, u *url.URL) (*siteProfile, error) {
	memo, _ := ctx.Value(profileMemoKey{}).(*profileMemo)
	if memo == nil {
		return w.profile(ctx, u)
	}
	exact := *u
	exact.Fragment, exact.RawFragment = "", ""
	key := exact.String()
	memo.mu.Lock()
	if c, ok := memo.calls[key]; ok {
		memo.mu.Unlock()
		select {
		case <-c.done:
			return c.p, c.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &profileCall{done: make(chan struct{})}
	memo.calls[key] = c
	memo.mu.Unlock()
	c.p, c.err = w.profile(ctx, &exact)
	close(c.done)
	return c.p, c.err
}

// resolution is one name's resolution in progress.
type resolution struct {
	w     *Web
	spec  libSpec
	log   *resolveLog
	roots int // candidate roots verified so far
	// strict resolves a bare query's guessed reading (resolveOne's
	// strict): only an exact package's own site can answer it, so GitHub
	// and the web are never searched for it.
	strict bool
	// lean resolves a common word's reading beside a reading that names a
	// library outright (querySplit.lean): the registries only, tried once.
	lean bool
	// keyless marks a resolution without GitHub (Web.gh): projects keep
	// their packages' claimed repositories, roots need two independent
	// edges, and registry popularity stands in for stars.
	keyless bool
	// hint are the ecosystems the query or the working directory names
	// (ecoHint), for a single-library context only; nil for none.
	hint []ecosystem
}

// resolve finds the docs for spec (plan §4): every verified match, best
// first, at most limit when limit is positive. With nothing verified it
// returns the §8 error: ErrNotFound with per-source reasons when every
// source answered, a plain upstream error when one failed, and an
// ErrNotFound listing the versions when spec.version is not offered.
func (w *Web) resolve(ctx context.Context, spec libSpec, limit int) ([]webMatch, error) {
	return w.resolveLogged(ctx, spec, limit, &resolveLog{})
}

// resolveLogged is resolve recording into log.
func (w *Web) resolveLogged(ctx context.Context, spec libSpec, limit int, log *resolveLog) ([]webMatch, error) {
	return w.resolveRun(ctx, spec, limit, log, false, false)
}

// resolveRun is resolveLogged, strict for a bare query's guessed reading
// (resolution.strict).
//
// pick marks a single-library context (resolveOne), where an ecosystem
// hint may narrow a same-name contest; a listing (--resolve) keeps every
// project.
func (w *Web) resolveRun(ctx context.Context, spec libSpec, limit int, log *resolveLog, strict, pick bool) ([]webMatch, error) {
	if spec.url != "" {
		m, err := w.resolveURL(ctx, spec.url)
		if err != nil {
			return nil, err
		}
		return []webMatch{m}, nil
	}
	if !w.resolvesNames() {
		return nil, errNoNameSources(spec.raw)
	}
	rctx, cancel := context.WithTimeout(publicHops(ctx), w.resolveTimeout)
	defer cancel()
	rctx = withProfileMemo(rctx)
	lean := isLean(ctx)
	if strict || lean {
		rctx = withSpeculative(rctx)
	}
	r := &resolution{w: w, spec: spec, log: log, strict: strict, lean: lean, keyless: w.gh() == nil}
	if pick {
		r.hint = w.ecoHint(ctx)
	}
	matches := r.run(rctx)
	if len(matches) == 0 {
		return nil, r.failure(ctx, rctx)
	}
	matches, err := r.finish(rctx, matches)
	if err != nil {
		return nil, finishErr(ctx, err)
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

// searches reports a resolution that may search (the web, GitHub,
// pkg.go.dev's names) beside asking the registries: not a guessed or a
// lean reading's.
func (r *resolution) searches() bool { return !r.strict && !r.lean }

type leanKey struct{}

// withLean marks ctx as a lean reading's resolution (resolution.lean).
func withLean(ctx context.Context) context.Context { return context.WithValue(ctx, leanKey{}, true) }

func isLean(ctx context.Context) bool {
	v, _ := ctx.Value(leanKey{}).(bool)
	return v
}

// finishErr classifies a failure to finish the matches (a version root
// that could not be read), as callErr does for docs fetches: the caller's
// cancellation as such, and any deadline short of it (the resolution's,
// a probe's) as a plain upstream failure, its text kept but not its chain,
// which would read as cancellation. A version not offered stays not-found.
func finishErr(parent context.Context, err error) error {
	switch {
	case parent.Err() != nil:
		return fmt.Errorf("web: %w", parent.Err())
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return errors.New(err.Error())
	}
	return err
}

// run gathers and verifies, widening the search only while nothing has
// verified: GitHub repository search when the registries came up short,
// then the web search.
//
// A keyless resolution (no GitHub) makes the web search a candidate source
// of its own: it runs beside the registries from the start, and its hits
// are verified beside the registries' roots (keylessHits), never trusted
// for being hits.
func (r *resolution) run(ctx context.Context) []webMatch {
	if r.keyless {
		return r.runKeyless(ctx)
	}
	projects := r.w.gather(ctx, r.spec, r.log, r.searches())
	r.log.consider(projects...)
	matches := r.verifyAll(ctx, projects)
	if !r.searches() {
		return r.assemble(projects, matches) // searches find no exact package
	}
	if r.spec.eco == ecoAny && anyExact(projects) && (!hasSite(matches) || !settled(matches)) {
		// gatherProjects searched GitHub only when no package matched
		// exactly; exact packages that verified nothing but registry docs,
		// or that leave the name unsettled (cobra: COBRApy beside a crate,
		// with spf13/cobra in no registry asked), leave room too.
		extra := r.w.searchProjects(ctx, r.spec, projects, r.log)
		r.log.consider(extra...)
		r.roots = min(r.roots, webMaxRoots-webMinSearchRoots)
		matches = append(matches, r.verifyAll(ctx, extra)...)
		projects = append(projects, extra...)
	}
	if !hasSite(matches) {
		more, extra := r.lastResort(ctx, projects)
		matches = append(matches, more...)
		projects = append(projects, extra...)
	}
	return r.assemble(projects, matches)
}

// settled reports matches that answer a single-library call without
// asking: one project, or one that dominates the rest (dominant).
func settled(matches []webMatch) bool {
	var primaries []webMatch
	at := map[string]int{}
	for _, m := range matches {
		i, ok := at[m.project]
		if !ok {
			at[m.project] = len(primaries)
			m.indexOnly = true
			primaries = append(primaries, m)
			i = len(primaries) - 1
		}
		primaries[i].indexOnly = primaries[i].indexOnly && indexHosted(m.root.u)
	}
	if len(primaries) < 2 {
		return true
	}
	_, ok := dominant(primaries)
	return ok
}

// anyExact reports a project with a package of exactly the queried name.
func anyExact(projects []*project) bool {
	return slices.ContainsFunc(projects, func(p *project) bool { return p.exact })
}

// hasSite reports a match that is not registry-hosted docs.
func hasSite(matches []webMatch) bool {
	return slices.ContainsFunc(matches, func(m webMatch) bool { return !m.registry })
}

// resolvesNames reports whether the backend has any source to resolve a
// library name with.
func (w *Web) resolvesNames() bool { return w.pkgs != nil || w.repos != nil || w.sites != nil }

// gh is the GitHub source when it may be asked (githubOn: a token is
// configured), else nil: keyless resolution never calls GitHub, so its
// anonymous quota can neither slow a resolution down nor veto a pick.
func (w *Web) gh() repoSource {
	if w.repos == nil || w.githubOn != nil && !w.githubOn() {
		return nil
	}
	return w.repos
}

// errNoNameSources is the error for a name given to a backend built
// without resolution sources (NewWeb).
func errNoNameSources(name string) error {
	return fmt.Errorf("%w: this web backend takes the docs site URL as the library, not a name (got %q), e.g. https://docs.astro.build", ErrLibraryRequired, name)
}

// resolveOne resolves spec where exactly one library is needed (GetDocs
// with a name, Search): resolve, then pickUnique. strict applies a strict
// query split's bar (an exact registry name and strictMinTrust).
//
// A pick is kept in the resolution cache (web_rescache.go) when the
// fetcher has one, and a repeat of the same spec, mode and hint answers
// from it without resolving.
func (w *Web) resolveOne(ctx context.Context, spec libSpec, strict bool) (webMatch, error) {
	return w.resolveOneAs(ctx, spec, strict, false, false)
}

// resolveReading is resolveOne for one reading of a bare query: a common
// word's pick is weighed (commonWordBar compares its weight), and a
// reading that verifies nothing reports the packages of exactly its name
// (strictMiss), so another reading cannot answer in its place unless it
// outweighs them.
func (w *Web) resolveReading(ctx context.Context, s querySplit) (webMatch, error) {
	if s.lean {
		ctx = withLean(ctx)
	}
	return w.resolveOneAs(ctx, s.spec, s.strict, true, s.common)
}

// resolveOneAs is resolveOne, or resolveReading when reading is set; weigh
// asks for the pick's weight, and a cached pick without one is resolved
// again.
func (w *Web) resolveOneAs(ctx context.Context, spec libSpec, strict, reading, weigh bool) (webMatch, error) {
	var key string
	if spec.url == "" && w.resolvesNames() {
		key = resolutionKey(spec, strict, isLean(ctx), w.gh() == nil, w.ecoHint(ctx))
		if m, ok := w.loadResolution(key); ok && (!weigh || m.stars >= 0) {
			return m, nil
		}
	}
	m, cacheable, err := w.resolveOnce(ctx, spec, strict, reading, weigh)
	if err == nil && cacheable && key != "" {
		w.storeResolution(key, m)
	}
	return m, err
}

// resolveOnce is resolveOne without the cache. cacheable reports a pick
// made with every source answering: one that stood past a source that
// failed (with a note) is not kept, so the next call asks again.
func (w *Web) resolveOnce(ctx context.Context, spec libSpec, strict, reading, weigh bool) (m webMatch, cacheable bool, err error) {
	log := &resolveLog{}
	matches, err := w.resolveRun(ctx, spec, 0, log, strict, true)
	if err != nil {
		if (strict || reading) && w.gh() == nil {
			err = w.strictMiss(err, log)
		}
		return webMatch{}, false, err
	}
	m, err = pickUnique(spec.raw, matches)
	if err != nil {
		return webMatch{}, false, err
	}
	if m, err = answerPastFailures(spec, m, matches, log); err != nil {
		return webMatch{}, false, err
	}
	if w.gh() == nil {
		if err := w.keylessRivals(spec, m, matches, log, w.ecoHint(ctx)); err != nil {
			return webMatch{}, false, err
		}
	}
	if strict && (m.proj == nil || !m.proj.exact || m.registry || m.trust < strictMinTrust) {
		return webMatch{}, false, w.guessUnproven(ctx, spec, m, log)
	}
	if weigh {
		w.weigh(ctx, &m)
	}
	_, failed := log.snapshot()
	return m, len(failed) == 0, nil
}

// guessUnproven is the error for a guessed reading whose pick does not
// prove it (an exact package name, its own docs site and strictMinTrust).
func (w *Web) guessUnproven(ctx context.Context, spec libSpec, m webMatch, log *resolveLog) error {
	err := fmt.Errorf("web: %q is a guess at a library name and %s (trust %.1f) does not prove it: a guess needs an exact package name, its own docs site and trust %.0f: %w",
		spec.raw, m.root, m.trust, strictMinTrust, ErrNotFound)
	if w.gh() == nil {
		err = w.strictMiss(err, log)
	}
	return err
}

// weigh reads the pick's weight when it is not known yet: popularity
// is read keyless only for a contest (readTieBreak), and a bare query's
// readings compare it.
func (w *Web) weigh(ctx context.Context, m *webMatch) {
	if m.stars < 0 && m.proj != nil {
		w.readPopularity(ctx, []*project{m.proj})
		m.stars = m.proj.stars()
	}
}

// answerPastFailures checks the one library picked for spec against the
// sources that failed along the way. A registry or GitHub search that
// failed (or that GitHub cut short) may have hidden whole projects of that
// name, which would have made it ambiguous or outweighed the pick: the
// answer is then an upstream error naming the pick, to retry or pass as
// the library, rather than a silent guess. So is another project whose
// repository GitHub did not read (a rate limit, an outage): GitHub decides
// whether its packages' claim stands, the docs its homepage names and its
// stars, so it may be the library meant (express beside a docs.rs
// squatter) or outweigh the pick, and the pick depended on GitHub. Another
// project whose homepage, docs root or own package did not answer is
// known, just unverified. When it could have outranked or tied the pick
// had it answered (rivals: at least the pick's stars, or any at all beside
// a pick whose docs only a package index hosts, the squatter signal), the
// pick is no answer either: a lesser project must not win because a
// stronger one's site was briefly down. Otherwise the pick stands, with a
// note saying what was not checked, so a junk package's dead homepage does
// not sink the name for good.
func answerPastFailures(spec libSpec, m webMatch, matches []webMatch, log *resolveLog) (webMatch, error) {
	matched := map[string]bool{}
	for _, x := range matches {
		matched[x.project] = true
	}
	whole, partial := log.missedBeside(matched, true)
	var msg string
	switch unread, rival := log.unreadBeside(m.proj), rivals(m, partial); {
	case len(whole) > 0:
		advice := fmt.Sprintf("pass %s as the library", m.root)
		if m.indexOnly {
			advice = "pass the library you mean as a docs URL or package spec" // not a squatter's
		}
		msg = fmt.Sprintf("web: %q resolved to %s, but %s, so another library of that name may have been missed; retry, or %s",
			spec.raw, m.root, joinErrors(whole), advice)
	case len(unread) > 0:
		var names []string
		var errs []error
		for _, p := range unread {
			names = append(names, p.label())
			errs = append(errs, p.repoErr)
		}
		msg = fmt.Sprintf("web: %q resolved to %s, but GitHub did not read the repository of %s (%s), which may be the library meant or outweigh it; retry, or pass the library you mean as a docs URL or package spec",
			spec.raw, m.root, strings.Join(uniqueStrings(names...), ", "), joinErrors(errs))
	case len(rival) > 0 && m.indexOnly:
		// Docs a package index hosts are the squatter signal (dominant):
		// with another project of the name unchecked, they are no answer.
		msg = fmt.Sprintf("web: %q resolved only to docs a package index hosts, %s, but %s, which may be the library meant; retry, or pass the library you mean as a docs URL or package spec",
			spec.raw, m.root, joinErrors(missedErrs(rival)))
	case len(rival) > 0:
		msg = fmt.Sprintf("web: %q resolved to %s (%s), but %s did not answer (%s), which may be the library meant or outweigh it; retry, or pass the library you mean as a docs URL or package spec",
			spec.raw, m.root, weightText(m.proj, m.stars), rivalNames(rival), joinErrors(missedErrs(rival)))
	default:
		if len(partial) > 0 {
			m.evidence = append(m.evidence, evidence{kind: evNote, detail: "not checked, so another library of this name may be missing: " + joinErrors(missedErrs(partial))})
		}
		return m, nil
	}
	if limit := log.rateLimited(); limit != nil && !limit.authed && !strings.Contains(msg, "github_token") {
		msg += " (set github_token or gh auth login)"
	}
	err := log.upstreamErr(msg, false) // plain: upstream, whatever deadline the failures carry
	err.picked = true
	return webMatch{}, err
}

// rivalNames names the rivals' projects with their stars: "github
// gin-gonic/gin (89439 stars)".
func rivalNames(rival []missedSource) string {
	var names []string
	for _, r := range rival {
		if r.proj == nil {
			names = append(names, r.project)
			continue
		}
		names = append(names, fmt.Sprintf("%s (%s)", r.proj.label(), weightText(r.proj, r.proj.stars())))
	}
	return strings.Join(uniqueStrings(names...), ", ")
}

// starsText is a star count as an error states it.
func starsText(n int) string {
	if n < 0 {
		return "stars unknown"
	}
	return strconv.Itoa(n) + " stars"
}

// incompleteNote is the evidence note a list of matches carries when a
// source failed that may have kept another library off it: a registry or
// GitHub search down, or another project's site or repository not
// answering. ResolveLibrary puts it on every match, so --resolve and the
// MCP matches say the list may be incomplete and why.
func incompleteNote(matches []webMatch, log *resolveLog) (evidence, bool) {
	matched := map[string]bool{}
	for _, m := range matches {
		matched[m.project] = true
	}
	whole, partial := log.missedBeside(matched, false)
	if len(whole)+len(partial) == 0 {
		return evidence{}, false
	}
	return evidence{kind: evNote, detail: "this list may be incomplete: " + joinErrors(append(whole, missedErrs(partial)...))}, true
}

// gatherProjects looks spec up in the registries, groups the packages by
// repository, enriches each group from GitHub and, when the registries
// come up short, adds GitHub search hits (plan §4 steps 1-3).
func (w *Web) gatherProjects(ctx context.Context, spec libSpec, log *resolveLog) []*project {
	return w.gather(ctx, spec, log, true)
}

// gather is gatherProjects, searching GitHub only when search is set.
func (w *Web) gather(ctx context.Context, spec libSpec, log *resolveLog, search bool) []*project {
	projects := w.registryProjects(ctx, spec, log)
	if spec.eco == ecoGitHub {
		projects = githubProject(spec)
	}
	if w.gh() == nil {
		projects = claimedProjects(spec, projects)
	} else {
		projects = corroborateClaims(spec, w.enrich(ctx, projects, log), log)
	}
	if spec.eco == ecoGitHub {
		w.repoPackages(ctx, spec, projects, log)
	}
	if search && spec.eco == ecoAny && !anyExact(projects) {
		projects = append(projects, w.searchProjects(ctx, spec, projects, log)...)
	}
	return projects
}

// languageRegistry maps a repository's primary language to the registry
// its packages are published in (registryLanguages reversed); ecoAny for
// none.
func languageRegistry(language string) ecosystem {
	for _, eco := range []ecosystem{ecoNPM, ecoPyPI, ecoCrates, ecoGo} {
		if languageFits(eco, language) {
			return eco
		}
	}
	return ecoAny
}

// repoPackages gives each repository project with no package (an
// owner/repo spec, a GitHub search hit) its own package in the registry
// of its language: the package named like the repository whose
// repository field names it back (tokio-rs/axum is crates' axum, whose
// docs.rs docs it has when it names no homepage), or for Go the module at
// its path (github.com/go-chi/chi, which the proxy fetched from it). Both
// sides agree, so the claim is corroborated. A search hit's same-named
// package in another registry was looked up by name already; a fork's is
// its upstream's.
func (w *Web) repoPackages(ctx context.Context, spec libSpec, projects []*project, log *resolveLog) {
	if w.pkgs == nil {
		return
	}
	var wg sync.WaitGroup
	for _, p := range projects {
		if w.gh() == nil && len(p.pkgs) == 0 && !p.repo.isZero() {
			// Keyless: GitHub cannot name the repository's language, so
			// each registry is asked for a package named like it; only one
			// that names the repository back is taken.
			wg.Go(func() { w.addRepoPackages(ctx, spec, p, log) })
			continue
		}
		if eco, name, ok := repoPackageName(spec, p); ok {
			wg.Go(func() { w.addRepoPackage(ctx, spec, p, eco, name, log) })
		}
	}
	wg.Wait()
}

// addRepoPackages is addRepoPackage in every registry, for a keyless
// resolution: the Go module at the repository's path, and the npm, PyPI
// and crates packages named like the repository, each kept only when it
// names the repository back. They are looked up concurrently and kept in
// registry order.
func (w *Web) addRepoPackages(ctx context.Context, spec libSpec, p *project, log *resolveLog) {
	tries := repoPackageTries(spec, p)
	found := make([]*project, len(tries))
	var wg sync.WaitGroup
	for i, t := range tries {
		wg.Go(func() {
			q := &project{key: p.key, repo: p.repo, aliases: p.aliases, fromHits: p.fromHits}
			w.addRepoPackage(ctx, spec, q, t.eco, t.name, log)
			if len(q.pkgs) > 0 && t.eco == ecoGo && !w.realGoModule(ctx, t.name, log) {
				return
			}
			if len(q.pkgs) > 0 {
				found[i] = q
			}
		})
	}
	wg.Wait()
	for _, q := range found {
		if q != nil {
			p.pkgs = append(p.pkgs, q.pkgs...)
			p.exact = p.exact || q.exact
		}
	}
}

// repoPackageName is the package repoPackages looks up for p: in the
// registry of its repository's language, named like it. ok is false for a
// project with packages, no repository read, a fork, a language no
// registry holds, or a bare name's same-named package, looked up already.
func repoPackageName(spec libSpec, p *project) (ecosystem, string, bool) {
	if p.info == nil || p.info.fork || len(p.pkgs) > 0 || p.repo.isZero() {
		return ecoAny, "", false
	}
	switch eco := languageRegistry(p.info.language); {
	case eco == ecoAny:
		return eco, "", false
	case eco == ecoGo:
		return eco, "github.com/" + p.repo.String(), true
	case spec.eco == ecoAny && normalizeName(eco, p.repo.name) == normalizeName(eco, spec.name):
		return eco, "", false
	default:
		return eco, p.repo.name, true
	}
}

// addRepoPackage looks up p's own package and adds it when it names p's
// repository back (a Go module at the repository's path always does).
func (w *Web) addRepoPackage(ctx context.Context, spec libSpec, p *project, eco ecosystem, name string, log *resolveLog) {
	actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	pkg, err := w.pkgs.lookup(actx, eco, name)
	switch {
	case errors.Is(err, ErrNotFound):
		log.reject("github %s: no %s package %s", p.repo, eco, name)
		return
	case err != nil && p.fromHits:
		// A search hit only suggested the repository: its packages not
		// answering hides nothing a registry was asked about.
		log.reject("github %s: its %s package %s: %v", p.repo, eco, name, err)
		return
	case err != nil:
		log.failFor(p, fmt.Errorf("github %s: its %s package %s: %w", p.repo, eco, name, err))
		return
	case eco == ecoGo && pkg.repo.isZero():
		pkg.repo = p.repo // the module path is the repository's
	case !p.isRepo(pkg.repo):
		log.reject("github %s: %s %s names github.com/%s, not it", p.repo, eco, name, pkg.repo)
		return
	}
	p.pkgs = append(p.pkgs, pkg)
	p.exact = p.exact || exactName(spec, pkg)
}

// goModChecker is the optional pkgSource method realGoModule asks.
type goModChecker interface {
	hasGoMod(ctx context.Context, module string) (bool, error)
}

// realGoModule reports a module found at a repository's path that is a Go
// library: pkg.go.dev records its go.mod (hasGoMod). A source that cannot
// say (a test double) is taken at its word.
func (w *Web) realGoModule(ctx context.Context, module string, log *resolveLog) bool {
	gm, ok := w.pkgs.(goModChecker)
	if !ok {
		return true
	}
	has, err := gm.hasGoMod(ctx, module)
	if err != nil || !has {
		log.reject("go %s: no go.mod on pkg.go.dev, not a Go module (%v)", module, err)
		return false
	}
	return true
}

// pkgTry is one package addRepoPackages looks up.
type pkgTry struct {
	eco  ecosystem
	name string
}

// repoPackageTries are the packages addRepoPackages looks up for p's
// repository: the Go module at its path, and the packages named like it
// (repoPackageNames). A search hit's repository counts as a Go module only
// when it is named as the library itself, not a namesake (go-react for
// react), and a bare name's own packages, looked up already, are skipped.
func repoPackageTries(spec libSpec, p *project) []pkgTry {
	var tries []pkgTry
	if !p.fromHits || strings.EqualFold(p.repo.name, searchName(spec)) {
		tries = append(tries, pkgTry{ecoGo, "github.com/" + p.repo.String()})
	}
	for _, eco := range []ecosystem{ecoNPM, ecoPyPI, ecoCrates} {
		for _, name := range repoPackageNames(eco, p.repo.name) {
			if normalizeName(eco, name) == normalizeName(eco, spec.name) && spec.eco == ecoAny && len(p.pkgs) == 0 && p.fromHits {
				continue
			}
			tries = append(tries, pkgTry{eco, name})
		}
	}
	return tries
}

// repoPackageNames are the package names in eco a repository called repo
// may publish under: the name itself, and on npm, where a short name is
// often taken, solid-js and solidjs for solidjs/solid. Only valid names
// for eco are listed.
func repoPackageNames(eco ecosystem, repo string) []string {
	n := strings.ToLower(repo)
	names := []string{n}
	if eco == ecoNPM && !strings.HasSuffix(n, "js") {
		names = append(names, n+"-js", n+"js")
	}
	var out []string
	for _, name := range names {
		switch {
		case eco == ecoNPM && !npmNameRe.MatchString(name),
			eco == ecoPyPI && !pypiNameRe.MatchString(name),
			eco == ecoCrates && !crateNameRe.MatchString(name):
			continue
		}
		out = append(out, name)
	}
	return out
}

// registryProjects looks spec up in the registries it allows and groups
// what they have by repository. Deprecated npm packages are not verified
// unless the spec named npm: their docs are not what the name means now.
// They are still considered (resolveLog.consider) as the rivals they are:
// a deprecated package of exactly the name, still in wide use, shows that
// the name means something popular (npm's angular, AngularJS, half a
// million downloads a week), so keylessRivals weighs it against a pick
// whose only docs a package index hosts, and a namesake it outweighs
// (docs.rs's angular, a handful) is refused, not answered. A pick with a
// docs site of its own is not weighed against it: the site is the
// evidence that the name means it now, and the deprecated package is what
// it meant before.
func (w *Web) registryProjects(ctx context.Context, spec libSpec, log *resolveLog) []*project {
	if w.pkgs == nil || spec.eco == ecoGitHub {
		return nil
	}
	actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	var pkgs, deprecated []pkgInfo
	for _, l := range w.lookupRegistries(actx, spec) {
		label := string(l.eco) + " " + l.name
		switch {
		case errors.Is(l.err, ErrNotFound):
			log.reject("%s: no such package", label)
		case l.err != nil:
			log.failSource(fmt.Errorf("%s: %w", label, l.err), true)
		case l.info.deprecated && l.eco == ecoNPM && spec.eco == ecoAny:
			log.knowName()
			log.reject("%s: deprecated", label)
			deprecated = append(deprecated, l.info)
		default:
			log.knowName()
			pkgs = append(pkgs, l.info)
		}
	}
	rivals := groupPackages(spec, deprecated)
	for _, p := range rivals {
		p.rivalOnly = true
	}
	log.consider(rivals...)
	return groupPackages(spec, pkgs)
}

// lookupRegistries is pkgSource.lookupAll, except that a guessed reading
// (speculative) of a bare query with an ecosystem hint is looked up only
// in the hinted registries: the hint settles the ecosystem, and probing
// the others would only spend their rate limits on a guess.
func (w *Web) lookupRegistries(ctx context.Context, spec libSpec) []pkgLookup {
	hint := w.ecoHint(ctx)
	if !speculative(ctx) || spec.eco != ecoAny || len(hint) == 0 {
		return w.pkgs.lookupAll(ctx, spec)
	}
	ecos := slices.DeleteFunc(registriesFor(spec), func(e ecosystem) bool { return !slices.Contains(hint, e) })
	out := make([]pkgLookup, len(ecos))
	var wg sync.WaitGroup
	for i, eco := range ecos {
		out[i] = pkgLookup{eco: eco, name: lookupName(eco, spec.name)}
		wg.Go(func() { out[i].info, out[i].err = w.pkgs.lookup(ctx, eco, out[i].name) })
	}
	wg.Wait()
	return out
}

// groupPackages makes one project per repository the packages name, and
// one per package that names none.
func groupPackages(spec libSpec, pkgs []pkgInfo) []*project {
	byKey := map[string]*project{}
	var out []*project
	for _, pkg := range pkgs {
		key := pkg.spec()
		if !pkg.repo.isZero() {
			key = pkg.repo.spec()
		}
		p := byKey[key]
		if p == nil {
			p = &project{key: key, repo: pkg.repo}
			byKey[key] = p
			out = append(out, p)
		}
		p.pkgs = append(p.pkgs, pkg)
		if exactName(spec, pkg) {
			p.exact = true
		}
	}
	return out
}

// corroborateClaims keeps in each repository's project only the packages
// whose claim to the repository it backs (corroborated); every other
// package becomes a project of its own, with no repository: anyone can
// write any repository into a package's metadata, so an uncorroborated
// claim neither merges a package (and its docs URL) into the real
// project nor lends it the repository's stars or back-links. Such a
// package keeps its own docs, and a note says why the claim does not
// count; GitHub search can still find the repository by its name.
func corroborateClaims(spec libSpec, projects []*project, log *resolveLog) []*project {
	var out []*project
	for _, p := range projects {
		if p.repo.isZero() || len(p.pkgs) == 0 {
			out = append(out, p)
			continue
		}
		var kept []pkgInfo
		var alone []*project
		for _, pkg := range p.pkgs {
			if corroborated(pkg, p) {
				kept = append(kept, pkg)
				continue
			}
			if p.info != nil {
				log.reject("%s: github.com/%s does not name it back, so its claim to that repository is not counted", pkgLabel(pkg), p.repo)
			}
			alone = append(alone, standaloneProject(spec, pkg, p))
		}
		if len(kept) > 0 {
			p.pkgs = kept
			p.exact = slices.ContainsFunc(kept, func(pkg pkgInfo) bool { return exactName(spec, pkg) })
			out = append(out, p)
		}
		out = append(out, alone...)
	}
	return out
}

// claimedProjects is corroborateClaims for a keyless resolution, where
// GitHub cannot be asked whether a repository backs the packages naming
// it: every package is a project of its own that keeps the repository its
// metadata names, and nothing merges on that claim alone. The claim is
// evidence only where the docs site agrees with it, by linking to that
// repository (linkEvidence), never a reason to fold one package into
// another's project; two packages that verify the same docs root still
// become one library (mergeSameRoot).
func claimedProjects(spec libSpec, projects []*project) []*project {
	var out []*project
	for _, p := range projects {
		if len(p.pkgs) < 2 {
			out = append(out, p)
			continue
		}
		for _, pkg := range p.pkgs {
			key := pkg.spec()
			if pkg.eco == ecoGo {
				key = p.key
			}
			out = append(out, &project{key: key, pkgs: []pkgInfo{pkg}, repo: pkg.repo, exact: exactName(spec, pkg), notes: slices.Clone(p.notes)})
		}
	}
	return out
}

// standaloneProject is pkg on its own, its claim to p's repository set
// aside: what GitHub said of the repository (that it does not exist, a
// rate limit) carries over.
func standaloneProject(spec libSpec, pkg pkgInfo, p *project) *project {
	q := &project{key: pkg.spec(), pkgs: []pkgInfo{pkg}, exact: exactName(spec, pkg), repoGone: p.repoGone, repoErr: p.repoErr, notes: slices.Clone(p.notes)}
	if p.info != nil {
		q.notes = append(q.notes, evidence{kind: evNote, detail: fmt.Sprintf("%s names github.com/%s, whose homepage does not name it back, so the claim is not counted", pkgLabel(pkg), p.repo)})
	}
	return q
}

// exactName reports a package whose normalized name is the queried one.
func exactName(spec libSpec, pkg pkgInfo) bool {
	if pkg.eco == ecoGo && spec.eco != ecoGo {
		// A bare name has no Go registry entry: a module whose last
		// element, its major version suffix aside, is the name is exact.
		return spec.name != "" && strings.EqualFold(moduleName(pkg.name), spec.name)
	}
	n := normalizeName(pkg.eco, pkg.name)
	return n != "" && n == normalizeName(pkg.eco, spec.name)
}

// moduleName is a Go module path's last element, a /vN suffix aside:
// chi for github.com/go-chi/chi/v5.
func moduleName(module string) string {
	elems := strings.Split(strings.TrimSuffix(module, "/"), "/")
	last := elems[len(elems)-1]
	if len(elems) > 1 && len(last) > 1 && last[0] == 'v' && isBareInt(last[1:]) {
		last = elems[len(elems)-2]
	}
	return last
}

// corroborated reports a package's repository claim the repository side
// backs. A claim GitHub could not be asked about is not corroborated. One
// is when:
//
//   - the package is a Go module, whose source the module proxy fetched
//     from the repository itself;
//   - the repository's GitHub homepage, which only its owners set, is the
//     package's registry page (crates.io/crates/rand) or on the site of
//     one of the package's own URLs (its homepage, its docs URL, or the
//     very package on docs.rs or pkg.go.dev);
//   - or the repository is named like the package (namedLike) and written
//     in the registry's language (languageFits), and the package names no
//     site of its own that the repository's homepage contradicts. That is
//     the common case of a homepage the repository leaves empty (anyhow,
//     pyyaml) or a package whose homepage is its README (webpack, whose
//     repository's is webpack.js.org). A package that merely claims a
//     popular repository fails it: npm's widget is not react/react, a
//     PyPI package is not a TypeScript repository, and one listing its own
//     docs elsewhere is contradicted by the repository's homepage.
func corroborated(pkg pkgInfo, p *project) bool {
	if pkg.eco == ecoGo && !pkg.repo.isZero() {
		return true
	}
	if p.info == nil {
		return false
	}
	home, hasHome := normalizeMetaURL(p.info.homepage)
	if hasHome && homepageNames(home, pkg) {
		return true
	}
	return namedLike(pkg, p.repo) && languageFits(pkg.eco, p.info.language) && (!hasHome || !hasOwnSite(pkg))
}

// homepageNames reports a repository homepage that is pkg's registry page
// or on the site of one of pkg's own URLs.
func homepageNames(home *url.URL, pkg pkgInfo) bool {
	if isRegistryPage(home, pkg) {
		return true
	}
	for _, raw := range []string{pkg.homepage, pkg.docsURL, pkg.registryDocs} {
		if u, ok := normalizeMetaURL(raw); ok && sameProjectSite(home, u) {
			return true
		}
	}
	return false
}

// hasOwnSite reports a package that names a site of its own: a homepage or
// docs URL that is not on a code forge or a registry.
func hasOwnSite(pkg pkgInfo) bool {
	for _, raw := range []string{pkg.homepage, pkg.docsURL} {
		if u, ok := normalizeMetaURL(raw); ok && !isNonDocsHost(u.Hostname()) && !isRegistryHost(u) {
			return true
		}
	}
	return false
}

// namedLike reports a repository named like the package: the same name
// (in any of nameVariants' spellings either way: solid-js and solid,
// commander and commander.js; or node-semver for semver), an npm scope
// that is the repository (@babel/core and babel/babel), or the owner's
// stem joined to the repository (serde_json and serde-rs/json). Names
// compare in lower case with "_" as "-".
func namedLike(pkg pkgInfo, repo repoKey) bool {
	canon := func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "_", "-") }
	repoName := canon(repo.name)
	repoVariants := map[string]bool{}
	for v := range nameSet(repo.name) {
		repoVariants[canon(v)] = true
	}
	stem := canon(repo.owner)
	for _, suffix := range []string{"-rs", "-js", "-py", "-lang", "-org", "-dev"} {
		stem = strings.TrimSuffix(stem, suffix)
	}
	for _, n := range packageNames(pkg) {
		n = canon(n)
		if repoVariants[n] || repoName == "node-"+n || n == stem+"-"+repoName {
			return true
		}
		for v := range nameSet(n) {
			if canon(v) == repoName {
				return true
			}
		}
	}
	return false
}

// packageNames are the names a package goes by for namedLike: its name,
// the last element of a scoped or module name, and an npm scope.
func packageNames(pkg pkgInfo) []string {
	name := strings.ToLower(pkg.name)
	out := []string{name[strings.LastIndexByte(name, '/')+1:]}
	if scope, _, ok := strings.Cut(name, "/"); ok && strings.HasPrefix(scope, "@") {
		out = append(out, strings.TrimPrefix(scope, "@"))
	}
	return out
}

// registryLanguages are the GitHub primary languages a registry's packages
// are written in.
var registryLanguages = map[ecosystem][]string{
	ecoNPM:    {"javascript", "typescript", "coffeescript", "livescript", "vue", "svelte", "astro"},
	ecoPyPI:   {"python", "cython", "jupyter notebook"},
	ecoCrates: {"rust"},
	ecoGo:     {"go"},
}

// languageFits reports a repository language a package of eco is written
// in; an unknown language fits nothing.
func languageFits(eco ecosystem, language string) bool {
	return slices.Contains(registryLanguages[eco], strings.ToLower(strings.TrimSpace(language)))
}

// sameProjectSite reports two published URLs on one project's site: the
// same registrable domain (docs.djangoproject.com and
// www.djangoproject.com), or, on a host serving many projects (docs.rs,
// pkg.go.dev), the same site down to the path (sameSite). Code forges and
// other non-docs hosts never match.
func sameProjectSite(a, b *url.URL) bool {
	switch {
	case isNonDocsHost(a.Hostname()) || isNonDocsHost(b.Hostname()):
		return false
	case isRegistryHost(a) || isRegistryHost(b):
		return sameSite(a, b)
	}
	return registrableDomain(a.Hostname()) == registrableDomain(b.Hostname())
}

// isRegistryPage reports u as pkg's own page on its registry (npmjs.com,
// pypi.org, crates.io or lib.rs).
func isRegistryPage(u *url.URL, pkg pkgInfo) bool {
	segs := pathSegments(u.Path)
	if len(segs) < 2 {
		return false
	}
	name := ""
	switch host := scopeHost(u); {
	case pkg.eco == ecoNPM && (host == "npmjs.com" || host == "npmjs.org") && segs[0] == "package":
		name = strings.Join(segs[1:min(len(segs), 3)], "/")
		if !strings.HasPrefix(name, "@") {
			name = segs[1]
		}
	case pkg.eco == ecoPyPI && host == "pypi.org" && segs[0] == "project",
		pkg.eco == ecoCrates && (host == "crates.io" || host == "lib.rs") && segs[0] == "crates":
		name = segs[1]
	default:
		return false
	}
	return normalizeName(pkg.eco, name) == normalizeName(pkg.eco, pkg.name)
}

// githubProject is the project an owner/repo spec names.
func githubProject(spec libSpec) []*project {
	owner, name, ok := strings.Cut(strings.ToLower(strings.TrimSuffix(spec.name, "/")), "/")
	if !ok || owner == "" || name == "" {
		return nil
	}
	k := repoKey{owner: owner, name: strings.TrimSuffix(name, ".git")}
	return []*project{{key: k.spec(), repo: k, exact: true}}
}

// enrich reads each project's repository from GitHub (canonical name after
// renames, homepage, fork, archived, stars) and merges projects whose
// repositories turn out to be one.
func (w *Web) enrich(ctx context.Context, projects []*project, log *resolveLog) []*project {
	if w.gh() == nil {
		return projects
	}
	var wg sync.WaitGroup
	for _, p := range projects {
		if p.repo.isZero() || p.info != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.enrichOne(ctx, p, log)
		}()
	}
	wg.Wait()
	return mergeProjects(projects)
}

// enrichOne reads p's repository.
func (w *Web) enrichOne(ctx context.Context, p *project, log *resolveLog) {
	if limit := log.rateLimited(); limit != nil {
		p.notes = append(p.notes, evidence{kind: evNote, detail: limit.Error()})
		p.repoErr = fmt.Errorf("github %s: not asked: %w", p.repo, limit)
		log.failRepo(p.key, p.repoErr)
		return
	}
	actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	info, err := w.gh().repo(actx, p.repo)
	if err != nil {
		repoFailed(p, err, log)
		return
	}
	if !info.key.isZero() && info.key != p.repo {
		p.aliases = append(p.aliases, p.repo)
		p.repo = info.key
		p.key = info.key.spec()
	}
	p.info = &info
}

// repoFailed records why p's repository could not be read.
func repoFailed(p *project, err error, log *resolveLog) {
	var limit *ghRateLimitError
	switch {
	case errors.As(err, &limit):
		log.setLimited(limit)
		p.notes = append(p.notes, evidence{kind: evNote, detail: limit.Error()})
		p.repoErr = fmt.Errorf("github %s: %w", p.repo, err)
		log.failRepo(p.key, p.repoErr)
	case errors.Is(err, ErrNotFound):
		p.repoGone = true
		log.reject("%s: repo %s not found", p.label(), p.repo)
	default:
		p.notes = append(p.notes, evidence{kind: evNote, detail: "github " + p.repo.String() + ": " + err.Error()})
		p.repoErr = fmt.Errorf("github %s: %w", p.repo, err)
		log.failRepo(p.key, p.repoErr)
	}
}

// mergeProjects joins projects that share a key, as renamed repositories
// make them (danielakhterov/solid is janaakhterov/solid).
func mergeProjects(projects []*project) []*project {
	byKey := map[string]*project{}
	var out []*project
	for _, p := range projects {
		q := byKey[p.key]
		if q == nil {
			byKey[p.key] = p
			out = append(out, p)
			continue
		}
		q.pkgs = append(q.pkgs, p.pkgs...)
		q.aliases = append(q.aliases, p.aliases...)
		q.notes = append(q.notes, p.notes...)
		q.exact = q.exact || p.exact
		if q.info == nil {
			q.info = p.info
			q.repoGone = q.repoGone || p.repoGone
			q.repoErr = cmp.Or(q.repoErr, p.repoErr)
		}
		if q.info != nil {
			q.repoErr = nil
		}
	}
	return out
}

// searchProjects asks GitHub for repositories named spec's name (or a
// variant, nameVariants) and makes projects of the most-starred ones not
// already known: forks and archived repositories are skipped.
func (w *Web) searchProjects(ctx context.Context, spec libSpec, existing []*project, log *resolveLog) []*project {
	if w.gh() == nil || spec.eco != ecoAny {
		return nil
	}
	if limit := log.rateLimited(); limit != nil {
		// Not asked is not "no such repository": the search may have found
		// the library meant.
		log.failSource(fmt.Errorf("github search %q: not asked: %w", spec.name, limit), false)
		return nil
	}
	actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	hits, err := w.gh().searchRepos(actx, spec.name, webSearchRepos)
	switch {
	case errors.Is(err, errIncompleteSearch):
		// GitHub cut the search short and answered with what it had: the
		// hits are real, but the library meant may be missing from them
		// (gin-gonic/gin from gin's, leaving codegangsta/gin), so the
		// search failed all the same.
		log.failSource(fmt.Errorf("github search %q: %w", spec.name, err), false)
	case err != nil:
		var limit *ghRateLimitError
		if errors.As(err, &limit) {
			log.setLimited(limit)
		}
		log.failSource(fmt.Errorf("github search %q: %w", spec.name, err), false)
		return nil
	}
	known := map[repoKey]bool{}
	for _, p := range existing {
		known[p.repo] = true
		for _, a := range p.aliases {
			known[a] = true
		}
	}
	variants := nameSet(spec.name)
	room := min(webMaxSearchProjects, max(1, webMaxProjects-len(existing)))
	var out []*project
	for _, h := range hits {
		switch {
		case len(out) == room:
			w.repoPackages(ctx, spec, out, log)
			return out
		case known[h.key] || h.fork || !variants[h.key.name]:
			continue
		case h.archived:
			log.reject("github %s: archived", h.key)
			continue
		}
		known[h.key] = true
		info := h
		out = append(out, &project{key: h.key.spec(), repo: h.key, info: &info, fromSearch: true})
	}
	w.repoPackages(ctx, spec, out, log)
	return out
}

// nameSet is name and its repository-name variants, lower case.
func nameSet(name string) map[string]bool {
	set := map[string]bool{strings.ToLower(name): true}
	for _, v := range nameVariants(name) {
		set[strings.ToLower(v)] = true
	}
	return set
}

// verifyAll lists each project's candidate roots and verifies them,
// strongest first, round-robin across projects, within the root budget.
// The projects whose candidates are ready within candidateWait are
// scheduled together; one that takes longer (a homepage that hangs) is
// verified when it arrives, from a share of the budget kept for it, so it
// never holds up the others.
func (r *resolution) verifyAll(ctx context.Context, projects []*project) []webMatch {
	if len(projects) > webMaxProjects {
		projects = projects[:webMaxProjects]
	}
	if len(projects) == 0 {
		return nil
	}
	type listed struct {
		i     int
		cands []rootCandidate
	}
	arrived := make(chan listed, len(projects))
	for i, p := range projects {
		go func() { arrived <- listed{i, r.w.candidateRoots(ctx, p, r.log)} }()
	}
	cands := make([][]rootCandidate, len(projects))
	got := 0
	timer := time.NewTimer(r.w.candidateWait)
	defer timer.Stop()
wait:
	for got < len(projects) {
		select {
		case l := <-arrived:
			cands[l.i], got = l.cands, got+1
		case <-timer.C:
			break wait
		}
	}
	v := &verifier{r: r}
	share := max(1, (webMaxRoots-r.roots)/len(projects))
	v.start(ctx, r.schedule(projects, cands, webMaxRoots-min((len(projects)-got)*share, webMaxRoots-r.roots)))
	for ; got < len(projects); got++ {
		l := <-arrived // candidateRoots ends with ctx at the latest
		v.start(ctx, r.schedule(projects[l.i:l.i+1], [][]rootCandidate{l.cands}, min(r.roots+share, webMaxRoots)))
	}
	return v.wait()
}

// verifier verifies roots concurrently, keeping the matches in the order
// their jobs started.
type verifier struct {
	r     *resolution
	wg    sync.WaitGroup
	mu    sync.Mutex
	found []*webMatch
}

// start verifies jobs, each in its own goroutine.
func (v *verifier) start(ctx context.Context, jobs []rootJob) {
	v.mu.Lock()
	base := len(v.found)
	v.found = append(v.found, make([]*webMatch, len(jobs))...)
	v.mu.Unlock()
	for i, j := range jobs {
		v.wg.Add(1)
		go func() {
			defer v.wg.Done()
			m, err := v.r.w.verifyRoot(ctx, j.p, j.c)
			if err != nil {
				v.r.noteRejection(j.p, err)
				return
			}
			v.mu.Lock()
			v.found[base+i] = &m
			v.mu.Unlock()
		}()
	}
}

// wait returns the matches once every job is done.
func (v *verifier) wait() []webMatch {
	v.wg.Wait()
	var out []webMatch
	for _, m := range v.found {
		if m != nil {
			out = append(out, *m)
		}
	}
	return out
}

// rootJob is one root to verify for one project.
type rootJob struct {
	p *project
	c rootCandidate
}

// schedule picks the roots to verify: each project's strongest first,
// taking turns, until limit roots have been verified in all.
func (r *resolution) schedule(projects []*project, cands [][]rootCandidate, limit int) []rootJob {
	var jobs []rootJob
	for depth := 0; r.roots < limit; depth++ {
		added := false
		for i, p := range projects {
			if depth < len(cands[i]) && r.roots < limit {
				jobs = append(jobs, rootJob{p: p, c: cands[i][depth]})
				r.roots++
				added = true
			}
		}
		if !added {
			break
		}
	}
	return jobs
}

// noteRejection records why a root did not verify for p.
func (r *resolution) noteRejection(p *project, err error) {
	var rej *rejectError
	if errors.As(err, &rej) {
		r.log.reject("%s: %s", p.label(), rej.reason)
		return
	}
	r.log.failFor(p, fmt.Errorf("%s: %w", p.label(), err))
}

// candidateSet collects candidate roots, merging evidence for the same root.
type candidateSet struct {
	list  []*rootCandidate
	byKey map[string]*rootCandidate
	navs  map[string]bool // nav chains added, by root and homepage (addNav)
}

// add puts u forward as a root with ev. A root guessed before, or linked
// from another domain, is no longer once something names it.
func (s *candidateSet) add(u *url.URL, registry, search bool, ev *evidence) {
	c := s.candidate(u, rootCandidate{registry: registry, search: search})
	if c == nil {
		return
	}
	c.guess, c.offsite = false, false
	c.addEvidence(ev)
}

// addOffsite puts u forward as a root a homepage links to on another
// registrable domain (rootCandidate.offsite), with ev.
func (s *candidateSet) addOffsite(u *url.URL, ev *evidence) {
	if c := s.candidate(u, rootCandidate{offsite: true}); c != nil {
		c.addEvidence(ev)
	}
}

// addNav adds a nav chain from homepage home to root, once per homepage
// URL: a package and its repository naming one homepage are one chain,
// not two.
func (s *candidateSet) addNav(root, home *url.URL, ev *evidence) {
	k := linkKey(root) + " " + linkKey(home)
	if s.navs[k] {
		return
	}
	if s.navs == nil {
		s.navs = map[string]bool{}
	}
	s.navs[k] = true
	if registrableDomain(root.Hostname()) != registrableDomain(home.Hostname()) {
		s.addOffsite(root, ev)
		return
	}
	s.add(root, false, false, ev)
}

// namedBy records the owner-published URLs that put root forward
// (rootCandidate.namedBy).
func (s *candidateSet) namedBy(root *url.URL, by ...*url.URL) {
	c := s.byKey[candidateKey(root)]
	if c == nil {
		return
	}
	for _, u := range by {
		if u != nil && !slices.Contains(c.namedBy, linkKey(u)) {
			c.namedBy = append(c.namedBy, linkKey(u))
		}
	}
}

// candidateKey is the key candidateSet files u's root under; "" when u is
// not a docs root.
func candidateKey(u *url.URL) string {
	root, err := parseWebRoot(docsRootOf(u).String())
	if err != nil {
		return ""
	}
	return scopeHost(root.u) + root.prefix
}

// setPage records page as where root is read when it has no page of its
// own (rootCandidate.page), unless one is recorded.
func (s *candidateSet) setPage(root, page *url.URL) {
	if page == nil {
		return
	}
	if c := s.candidate(root, rootCandidate{}); c != nil && c.page == nil {
		c.page = page
	}
}

func (c *rootCandidate) addEvidence(ev *evidence) {
	if ev != nil && !slices.Contains(c.evidence, *ev) {
		c.evidence = append(c.evidence, *ev)
	}
}

// addGuess puts u forward as a guessed root (rootCandidate.guess), unless
// it already is a candidate.
func (s *candidateSet) addGuess(u *url.URL) {
	s.candidate(u, rootCandidate{guess: true})
}

// candidate returns the candidate for u's root, adding it as like (its
// flags) when it is new; nil when u is not a docs root.
func (s *candidateSet) candidate(u *url.URL, like rootCandidate) *rootCandidate {
	root, err := parseWebRoot(docsRootOf(u).String())
	if err != nil {
		return nil
	}
	if s.byKey == nil {
		s.byKey = map[string]*rootCandidate{}
	}
	k := candidateKey(u)
	c := s.byKey[k]
	if c == nil {
		like.url = root.u
		c = &like
		s.byKey[k] = c
		s.list = append(s.list, c)
	}
	return c
}

// sorted returns the candidates, most evidence points first.
func (s *candidateSet) sorted() []rootCandidate {
	out := make([]rootCandidate, 0, len(s.list))
	for _, c := range s.list {
		out = append(out, *c)
	}
	slices.SortStableFunc(out, func(a, b rootCandidate) int { return cmp.Compare(points(b.evidence), points(a.evidence)) })
	return out
}

// points sums evidence points, unclamped.
func points(evs []evidence) float64 {
	sum := 0.0
	for _, e := range evs {
		sum += trustPoints[e.kind]
	}
	return sum
}

// candidateRoots lists the docs roots worth verifying for p (plan §4
// step 4a-d), deduplicated, strongest evidence first.
func (w *Web) candidateRoots(ctx context.Context, p *project, log *resolveLog) []rootCandidate {
	set := &candidateSet{}
	listed := false
	for _, pkg := range p.pkgs {
		listed = listed || pkg.registryDocs != "" || pkg.docsURL != "" || pkg.homepage != "" || pkg.pageURL != ""
		w.pkgCandidates(ctx, p, pkg, set, log)
	}
	if p.info != nil && p.info.homepage != "" {
		listed = true
		src := "github " + p.repo.String()
		if p.info.fork {
			log.reject("%s: a fork of %s, so its homepage is not evidence", src, p.info.parent)
		} else {
			w.homepageCandidates(ctx, p, p.info.homepage, src, evRepoHomepage, set, log)
		}
	}
	if !listed {
		log.reject("%s: lists no docs URL or homepage", p.label())
	}
	if w.gh() == nil && !slices.ContainsFunc(set.list, func(c *rootCandidate) bool { return !indexHosted(c.url) }) {
		readmeCandidates(p, set)
	}
	out := set.sorted()
	if len(out) > 0 {
		log.offer(p.key, slices.ContainsFunc(out, func(c rootCandidate) bool { return !indexHosted(c.url) }))
	}
	return out
}

// pkgCandidates adds one package's roots: its registry-hosted docs, its
// documentation URL and its homepage's.
func (w *Web) pkgCandidates(ctx context.Context, p *project, pkg pkgInfo, set *candidateSet, log *resolveLog) {
	src := pkgLabel(pkg)
	if u, reason := checkRegistryURL(pkg.registryDocs); pkg.registryDocs != "" && reason == "" {
		set.add(u, true, false, &evidence{kind: evRegistryDocs, detail: u.Hostname() + " hosts the docs of " + src})
	}
	if pkg.docsURL != "" {
		u, reason := checkMetaURL(pkg.docsURL)
		switch {
		case reason != "":
			log.reject("%s: documentation URL %s", src, reason)
		case !isRegistryHost(u):
			set.add(u, false, false, &evidence{kind: evPkgDocs, detail: src + " lists it as Documentation"})
			set.namedBy(u, u)
		}
	}
	if pkg.homepage != "" {
		w.homepageCandidates(ctx, p, pkg.homepage, src, evPkgHomepage, set, log)
	}
	if pkg.docsURL == "" && pkg.pageURL != "" {
		pageCandidate(pkg.pageURL, src, set)
	}
}

// pageCandidate puts forward the docs root above a page the metadata
// publishes besides its docs URL (a changelog), when the page sits in a
// versioned docs tree (marshmallow's Changelog at
// marshmallow.readthedocs.io/en/latest/changelog.html): the owner names a
// page of those docs, as good as naming the root. A page elsewhere (a
// CHANGELOG.md on GitHub, a site's /changes) names no docs root.
func pageCandidate(raw, src string, set *candidateSet) {
	u, reason := checkMetaURL(raw)
	if reason != "" || isRegistryHost(u) || isNonDocsHost(u.Hostname()) {
		return
	}
	if root := docsRootOf(u); root.String() != u.String() {
		set.add(root, false, false, &evidence{kind: evPkgDocs, detail: src + " lists a page under it (" + u.String() + ")"})
		set.namedBy(root, u)
	}
}

// homepageCandidates reads a published homepage and adds the roots it
// leads to (docsRootsFor): the homepage itself as kind's evidence (V1),
// another root as a nav chain (V2). A homepage with no docs link that is
// not docs itself counts only when it links back to the repository.
func (w *Web) homepageCandidates(ctx context.Context, p *project, raw, src string, kind evidenceKind, set *candidateSet, log *resolveLog) {
	u, reason := checkMetaURL(raw)
	if reason != "" {
		log.reject("%s: homepage %s", src, reason)
		return
	}
	if isRegistryHost(u) {
		return // registry-hosted docs are their own candidate
	}
	prof, err := w.cachedProfile(ctx, u)
	if err != nil {
		logFetch(log, p, src+": homepage "+u.String(), err)
		return
	}
	home := prof.served
	roots := docsRootsFor(u, prof)
	if len(roots) == 0 && linksTo(prof, p) {
		if self, ok := homeRoot(u, home); ok {
			roots = []*url.URL{self.u}
		}
	}
	if len(roots) == 0 {
		log.reject("%s: homepage %s has no docs link and is not a docs site", src, u)
		if g, ok := guessedDocsRoot(cmp.Or(home, u)); ok && !p.repo.isZero() {
			set.addGuess(g)
		}
		return
	}
	for _, root := range roots {
		set.namedBy(root, u, home)
		if sameRoot(root, home) || sameRoot(root, u) {
			set.add(root, false, false, &evidence{kind: kind, detail: src + " homepage"})
		} else {
			set.addNav(root, home, &evidence{kind: evNavChain, detail: src + " homepage " + scopeHost(home) + " links to it"})
		}
		set.setPage(root, navPage(prof, root))
	}
}

// guessedDocsRoot is the root worth trying beside a homepage that shows no
// docs link, as a JS-rendered one (solidjs.com) does over plain HTTP: the
// docs. host of its registrable domain (docs.solidjs.com). It is no
// evidence: it verifies only by V4, on the homepage's domain and linking
// back to the project's repository, and when it does not answer that is a
// rejection, not a failed source. The homepage's own /docs/ is not tried:
// on a JS-rendered site it is the same shell. A homepage on a hosting
// suffix (github.io, vercel.app) gets no guess: that domain is one user's
// site, not a project's.
func guessedDocsRoot(home *url.URL) (*url.URL, bool) {
	if home == nil || !isPublicWebURL(home) {
		return nil, false
	}
	host := strings.TrimSuffix(strings.ToLower(home.Hostname()), ".")
	domain := registrableDomain(host)
	if _, icann := publicsuffix.PublicSuffix(host); !icann || domain == "" || "docs."+domain == host {
		return nil, false
	}
	return &url.URL{Scheme: "https", Host: "docs." + domain, Path: "/"}, true
}

// linksTo reports a page that links to p's repository.
func linksTo(prof *siteProfile, p *project) bool {
	return slices.ContainsFunc(repoLinks(prof), p.isRepo)
}

// sameRoot reports two URLs that are the same docs root.
func sameRoot(a, b *url.URL) bool {
	ra, errA := parseWebRoot(a.String())
	rb, errB := parseWebRoot(b.String())
	return errA == nil && errB == nil && scopeHost(ra.u) == scopeHost(rb.u) && ra.prefix == rb.prefix
}

// normalizeMetaURL reads a URL as registries and GitHub publish it: maybe
// without a scheme (docs.rs/clap), maybe with a #readme fragment.
func normalizeMetaURL(raw string) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + strings.TrimPrefix(raw, "//")
	}
	u, err := url.Parse(raw)
	if err != nil || !isWebScheme(u.Scheme) || u.Hostname() == "" || !cleanDots(u) {
		return nil, false
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path, u.RawPath = "/", ""
	}
	return u, true
}

// checkMetaURL normalizes a third-party URL and says why it cannot be a
// docs site, "" when it can.
func checkMetaURL(raw string) (*url.URL, string) {
	u, ok := normalizeMetaURL(raw)
	switch {
	case !ok:
		return nil, fmt.Sprintf("%q is not a web URL", raw)
	case !isPublicWebURL(u):
		return u, u.String() + " is not a public web address"
	case isNonDocsHost(u.Hostname()):
		return u, u.String() + " is " + hostKind(u.Hostname())
	}
	return u, ""
}

// checkRegistryURL is checkMetaURL for a registry docs URL.
func checkRegistryURL(raw string) (*url.URL, string) {
	u, reason := checkMetaURL(raw)
	if reason == "" && !isRegistryHost(u) {
		reason = u.String() + " is not a registry docs host"
	}
	return u, reason
}

// isRegistryHost reports the registries' own docs hosts.
func isRegistryHost(u *url.URL) bool {
	switch scopeHost(u) {
	case "docs.rs", "pkg.go.dev":
		return true
	}
	return false
}

// indexHosted reports docs a package index hosts rather than the project:
// the registries' generated API docs (docs.rs, pkg.go.dev), and
// pythonhosted.org, where PyPI hosted uploaded docs until 2018.
func indexHosted(u *url.URL) bool {
	return isRegistryHost(u) || scopeHost(u) == "pythonhosted.org"
}

// verifyRoot fetches c's profile and checks it against p (plan §5). A
// non-nil error is the reason the root was rejected or unreachable, for
// the caller to log.
func (w *Web) verifyRoot(ctx context.Context, p *project, c rootCandidate) (webMatch, error) {
	root, err := parseWebRoot(c.url.String())
	switch {
	case err != nil:
		return webMatch{}, rejectf("%s is not a docs root", c.url)
	case !c.registry && isNonDocsHost(root.u.Hostname()):
		return webMatch{}, rejectf("%s is %s, not a docs site", root, hostKind(root.u.Hostname()))
	case !isPublicWebURL(root.u):
		return webMatch{}, rejectf("%s is not a public web address", root)
	}
	prof, err := w.rootProfile(ctx, root, c.page)
	if err != nil {
		if isGone(err) || c.guess {
			// A guess that does not answer was never a source.
			return webMatch{}, rejectf("%s: %v", root, err)
		}
		return webMatch{}, fmt.Errorf("%s: %w", root, err)
	}
	if root, err = adoptServed(root, prof.served, c.registry); err != nil {
		return webMatch{}, err
	}
	// A home page's docs are its docs section, not the site with its blog
	// and news.
	root, prof, c.evidence = w.docsSection(ctx, root, prof, c)
	if c.registry {
		root, prof = w.latestMajor(ctx, root, prof)
	}
	keyless := w.gh() == nil
	ep := p // the project as the evidence sees it
	if keyless {
		ep = w.withMovedRepos(ctx, root, prof, p, c)
	}
	evs, err := rootEvidence(root, prof, ep, c, keyless)
	if err != nil {
		return webMatch{}, err
	}
	if keyless {
		if evs, err = w.keylessEvidence(ctx, root, prof, ep, c, evs); err != nil {
			return webMatch{}, err
		}
	}
	trust, ok := score(evs)
	if !ok {
		return webMatch{}, rejectf("%s is not verified (trust %.1f)", root, trust)
	}
	return webMatch{
		root: root, title: siteTitle(prof, root), description: prof.description, trust: trust, evidence: evs,
		packages: p.specs(), project: p.key, stars: p.stars(), starsUnread: p.weightUnread(),
		registry: c.registry, fromHit: c.search, oldRepo: ep != p, proj: p, prof: prof,
	}, nil
}

// latestMajor moves pkg.go.dev docs of a module to its highest major
// version when the page links it (pkg.go.dev's "The highest tagged major
// version is v5" banner): github.com/go-chi/chi is v1, its users import
// github.com/go-chi/chi/v5. Anything else stays as it is.
func (w *Web) latestMajor(ctx context.Context, root webRoot, prof *siteProfile) (webRoot, *siteProfile) {
	if scopeHost(root.u) != "pkg.go.dev" {
		return root, prof
	}
	base := strings.TrimSuffix(root.prefix, "/")
	best, bestN := (*url.URL)(nil), 1
	for _, l := range prof.links {
		rest, ok := strings.CutPrefix(l.url.Path, base+"/v")
		if n, err := strconv.Atoi(rest); ok && err == nil && scopeHost(l.url) == "pkg.go.dev" && n > bestN && rest == strconv.Itoa(n) {
			best, bestN = l.url, n
		}
	}
	if best == nil {
		return root, prof
	}
	next, err := parseWebRoot(best.String())
	if err != nil {
		return root, prof
	}
	np, err := w.cachedProfile(ctx, next.u)
	if err != nil || !next.contains(np.served) {
		return root, prof
	}
	return withOrigin(next, np.served), np
}

// rootProfile reads root's page, or, when root has none (a 404), page, a
// page under it that its homepage's nav links to, which must be served
// from within root.
func (w *Web) rootProfile(ctx context.Context, root webRoot, page *url.URL) (*siteProfile, error) {
	prof, err := w.cachedProfile(ctx, root.u)
	if err == nil || page == nil || !errors.Is(err, ErrNotFound) || !root.contains(page) {
		return prof, err
	}
	if p, perr := w.cachedProfile(ctx, page); perr == nil && root.contains(p.served) {
		return p, nil
	}
	return prof, err
}

// siteTitle names a site from its profile: the page title without its
// site suffix, else the site name, else the root's address.
func siteTitle(prof *siteProfile, root webRoot) string {
	return firstNonEmpty(cleanPageTitle(prof.title), prof.meta["og:site_name"], root.u.Host+strings.TrimSuffix(root.prefix, "/"))
}

// adoptServed is the root a candidate stands for once its redirects are
// followed: the canonical version root (canonicalRoot), the same root when
// it was served from within it, else a new root on the served site.
// Registry docs that redirect away (docs.rs's build-failure page) have no
// docs there.
func adoptServed(root webRoot, served *url.URL, registry bool) (webRoot, error) {
	if served == nil {
		return root, nil
	}
	if canon, ok := canonicalRoot(root, served); ok {
		return canon, nil
	}
	if root.contains(served) {
		if v, ok := versionRootBelow(root, served); ok && !registry {
			return v, nil
		}
		return withOrigin(root, served), nil
	}
	if registry {
		return webRoot{}, rejectf("%s redirects to %s: no docs there", root, served)
	}
	if !isPublicWebURL(served) || isNonDocsHost(served.Hostname()) {
		return webRoot{}, rejectf("%s redirects to %s", root, served)
	}
	next, err := parseWebRoot(siteRootOf(served).String())
	if err != nil {
		return webRoot{}, rejectf("%s redirects to %s", root, served)
	}
	return next, nil
}

// versionRootBelow is the version root a root redirects into when it
// lands on a page of a version tree below itself (pydantic.dev's
// /docs/validation/ to /docs/validation/latest/get-started/ stands for
// /docs/validation/latest/): a root ends at its version segment.
func versionRootBelow(root webRoot, served *url.URL) (webRoot, bool) {
	if served == nil || !root.contains(served) {
		return webRoot{}, false
	}
	segs := pathSegments(served.Path)
	v := versionSegment(segs, strings.HasSuffix(served.Path, "/"))
	if v < len(pathSegments(root.prefix)) {
		return webRoot{}, false
	}
	next, err := parseWebRoot((&url.URL{Scheme: served.Scheme, Host: served.Host, Path: segmentsPath(segs[:v+1])}).String())
	return next, err == nil
}

// withOrigin moves root to served's scheme and host (http to https, a
// www. twin), keeping its prefix.
func withOrigin(root webRoot, served *url.URL) webRoot {
	if strings.EqualFold(served.Scheme, root.u.Scheme) && hostKey(served) == root.u.Host {
		return root
	}
	u := *root.u
	u.Scheme, u.Host = strings.ToLower(served.Scheme), hostKey(served)
	return webRoot{u: &u, prefix: root.prefix}
}

// localeSegRe matches a locale path segment: en, pt-br, zh_CN.
var localeSegRe = regexp.MustCompile(`^[a-z]{2}(?:[-_][a-zA-Z]{2,4})?$`)

// siteRootOf is the docs root a page URL sits under, as when a root
// redirects into one of its pages: through its version segment, which
// ends a docs root and keeps the product segments before it
// (/docs/validation/latest/ for /docs/validation/latest/get-started/,
// not /docs/, where pydantic.dev keeps other products' docs too); else
// through a docs segment after an optional locale; else the host root.
func siteRootOf(u *url.URL) *url.URL {
	segs := pathSegments(u.Path)
	keep := versionSegment(segs, strings.HasSuffix(u.Path, "/")) + 1
	if keep == 0 {
		i := 0
		if len(segs) > 1 && localeSegRe.MatchString(segs[0]) {
			i = 1
		}
		if i < len(segs) && docsSegments[strings.ToLower(segs[i])] {
			keep = i + 1
		}
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: segmentsPath(segs[:keep])}
}

// versionSegment is the index of the first version label among a page
// path's segments, -1 for none. The last segment counts only as a
// directory (dir), not a page name; a number with a leading zero (a
// date's 05) is no version.
func versionSegment(segs []string, dir bool) int {
	for i, seg := range segs {
		if i == len(segs)-1 && (!dir || isPageFile(seg)) {
			break
		}
		if l, ok := segmentLabel(seg); ok && (!isBareInt(l) || len(l) == 1 || l[0] != '0') {
			return i
		}
	}
	return -1
}

// docsRootOf is the docs root a published docs URL stands for: the URL's
// directory, cut after its version segment when it names a page inside a
// version tree (PyPI's pydantic lists .../validation/latest/get-started/).
// Registry hosts keep their own layout (docs.rs puts the crate after the
// version).
func docsRootOf(u *url.URL) *url.URL {
	if isRegistryHost(u) {
		return u
	}
	segs := pathSegments(u.Path)
	dir := strings.HasSuffix(u.Path, "/") || !strings.Contains(path.Base(u.Path), ".")
	if v := versionSegment(segs, dir); v >= 0 && v < len(segs)-1 {
		out := *u
		out.Path, out.RawPath, out.RawQuery, out.Fragment = segmentsPath(segs[:v+1]), "", "", ""
		return &out
	}
	return u
}

// searchRootFor is the root a web search hit for the library name stands
// for: a docs. host's root, a /docs/ prefix, a GitHub Pages project, else
// the host root. Below a /docs/ prefix, a segment that is the name (one of
// nameSet's variants)
// is that library's tree on a host documenting several
// (huggingface.co/docs/transformers/ beside /docs/diffusers/), and the hit
// stands for it, not for every library under /docs/.
func searchRootFor(u *url.URL, name string) *url.URL {
	switch {
	case strings.HasPrefix(strings.ToLower(u.Hostname()), "docs."):
		return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}
	case docsSegment(u.Path) != "":
		seg := docsSegment(u.Path)
		segs := pathSegments(u.Path)
		if name != "" && len(segs) >= 2 && nameSet(name)[strings.ToLower(segs[1])] {
			return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/" + seg + "/" + segs[1] + "/"}
		}
		return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/" + seg + "/"}
	}
	return hostRootFor(u)
}

// rootEvidence is what ties root to p: the candidate's own evidence, its
// back-links, the project's standing and whether the site names it. A
// root that links only to a same-named repository of another owner, or a
// search hit or guessed root without V4's support, is rejected.
//
// keyless (no GitHub) leaves V4 to keylessEvidence's two-edge rule, which
// a mirror (a back-link alone) fails just the same, and lets a link to
// the package's own registry page stand in for the repository back-link
// an offsite docs link needs.
func rootEvidence(root webRoot, prof *siteProfile, p *project, c rootCandidate, keyless bool) ([]evidence, error) {
	evs := slices.Clone(c.evidence)
	links, err := linkEvidence(root, prof, p)
	if err != nil {
		return nil, err
	}
	evs = append(evs, links...)
	if keyless && !c.registry {
		links = append(links, packageEvidence(root, profileLinkURLs(prof), p, "the site")...)
		evs = append(evs, links...)
	}
	if c.offsite && !hasEvidence(links, evLinksRepo) && !hasEvidence(links, evLinksPackage) {
		return nil, rejectf("%s is on another domain than the homepage linking to it as its docs, and does not link back to %s", root, p.label())
	}
	if c.v4Only() && keyless {
		evs = append(evs, relatedEvidence(root, p)...)
	} else if c.v4Only() {
		rel := relatedEvidence(root, p)
		if err := v4Verdict(root, p, c, rel, links); err != nil {
			return nil, err
		}
		evs = append(evs, rel...)
	}
	evs = append(evs, projectEvidence(p)...)
	if ev, ok := siteNamesEvidence(p, "title", prof.title, "name", prof.meta["og:site_name"]); ok {
		evs = append(evs, ev)
	}
	return uniqueEvidence(evs), nil
}

// linkEvidence reads the root's links to GitHub: to p's repository (or an
// alias) and to another repository of its owner. Links to a same-named
// repository of another owner, and to nothing of p's, reject the root:
// it documents someone else's project (or p is a fork of it).
func linkEvidence(root webRoot, prof *siteProfile, p *project) ([]evidence, error) {
	if p.repo.isZero() {
		return nil, nil
	}
	var evs []evidence
	var other repoKey
	repo, owner := false, false
	for _, k := range repoLinks(prof) {
		switch {
		case p.isRepo(k):
			if !repo {
				repo = true
				evs = append(evs, evidence{kind: evLinksRepo, detail: "links to github.com/" + k.String()})
			}
		case p.isOwner(k.owner):
			if !owner {
				owner = true
				evs = append(evs, evidence{kind: evLinksOwner, detail: "links to github.com/" + k.String() + ", another " + k.owner + " repository"})
			}
		case other.isZero() && p.isRepoName(k.name):
			other = k
		}
	}
	if len(evs) == 0 && !other.isZero() {
		return nil, rejectf("%s links to github.com/%s, not %s", root, other, p.repo)
	}
	return evs, nil
}

// relatedEvidence is V4's first half for a search hit: root shares a
// registrable domain with one of p's own URLs, or is its owner's GitHub
// Pages site. For a project the hits themselves named (fromHits), only
// the repository's own homepage counts: a page linking to a repository
// says nothing of the repository's own about that page.
func relatedEvidence(root webRoot, p *project) []evidence {
	host := strings.ToLower(root.u.Hostname())
	if !p.repo.isZero() && !p.fromHits && host == p.repo.owner+".github.io" {
		return []evidence{{kind: evRelatedDomain, detail: host + " is the GitHub Pages site of " + p.repo.owner}}
	}
	domain := registrableDomain(host)
	for _, a := range p.authoritative() {
		if registrableDomain(a.url.Hostname()) == domain {
			return []evidence{{kind: evRelatedDomain, detail: "on the domain of " + a.src + " " + scopeHost(a.url)}}
		}
	}
	return nil
}

// v4Verdict applies V4 to a search hit or a guessed root: it needs both a
// related domain and a link back to p's repository. A back-link alone is a
// mirror.
func v4Verdict(root webRoot, p *project, c rootCandidate, related, links []evidence) error {
	what := "search hit"
	if c.guess {
		what = "guessed docs root"
	}
	linked := hasEvidence(links, evLinksRepo)
	switch {
	case len(related) > 0 && linked:
		return nil
	case linked:
		return rejectf("%s %s links back to %s but is not on the project's own domain: a mirror", what, root, p.repo)
	case len(related) > 0:
		return rejectf("%s %s does not link to %s", what, root, p.label())
	}
	return rejectf("%s %s has nothing tying it to %s", what, root, p.label())
}

// projectEvidence is the project's standing, the same for each of its
// roots: a package's repository agreeing with the canonical one (a claim
// the repository corroborated, since corroborateClaims set the others
// apart), an archived repository, deprecated packages, and notes (a rate
// limit, a claim not counted).
func projectEvidence(p *project) []evidence {
	var evs []evidence
	if p.info != nil {
		for _, pkg := range p.pkgs {
			if pkg.repo == p.repo {
				evs = append(evs, evidence{kind: evRepoAgrees, detail: pkgLabel(pkg) + " repository is github.com/" + p.repo.String()})
				break
			}
		}
		if p.info.archived {
			evs = append(evs, evidence{kind: evArchived, detail: "github " + p.repo.String() + " is archived"})
		}
	}
	for _, pkg := range p.pkgs {
		if pkg.deprecated {
			evs = append(evs, evidence{kind: evDeprecated, detail: pkgLabel(pkg) + " is deprecated"})
		}
	}
	return append(evs, p.notes...)
}

// siteNamesEvidence reports a declared site name (pairs of source and
// value: "title", "Click Documentation") with a word, or two adjacent
// words run together, that is one of p's names, a trailing major version
// aside ("Beautiful Soup" names beautifulsoup4, "Jinja" jinja2). Words
// keep their dots, so "Next.js" does not name next.
func siteNamesEvidence(p *project, pairs ...string) (evidence, bool) {
	if p == nil {
		return evidence{}, false
	}
	names := p.names()
	for _, n := range p.names() {
		if stem := strings.TrimRight(n, "0123456789"); stem != n && len(stem) >= 3 {
			names = append(names, stem)
		}
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		toks := nameTokens(pairs[i+1])
		for j, tok := range toks {
			words := []string{tok}
			if j+1 < len(toks) {
				words = append(words, tok+toks[j+1])
			}
			for _, w := range words {
				if slices.Contains(names, w) {
					return evidence{kind: evSiteNames, detail: "the site's " + pairs[i] + " names " + w}, true
				}
			}
		}
	}
	return evidence{}, false
}

// nameTokens splits a title into lower-case words, keeping the dots,
// hyphens and underscores inside them and dropping a trailing 's.
func nameTokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(".-_'", r)
	})
	var out []string
	for _, f := range fields {
		f = strings.Trim(strings.TrimSuffix(strings.Trim(f, ".-_'"), "'s"), ".-_'")
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// resolveURL is ResolveLibrary for a docs URL the caller passed (plan §4
// step 0): phase 1's title and description, with the root moved to the
// version root it redirects to, the site's versions, and whatever evidence
// ties it to a repository. A URL the caller typed is never refused for
// want of evidence: trust may be 0.
func (w *Web) resolveURL(ctx context.Context, raw string) (webMatch, error) {
	root, err := parseWebRoot(raw)
	if err != nil {
		return webMatch{}, err
	}
	opCtx, cancel := context.WithTimeout(ctx, w.opTimeout)
	defer cancel()
	prof, perr := w.profile(opCtx, root.u)
	var moved []evidence
	if perr == nil {
		if canon, ok := canonicalRoot(root, prof.served); ok {
			root = canon
		} else if next, note, ok := w.movedRoot(opCtx, root, nil); ok { // re-read under publicHops
			root, moved = next, []evidence{note}
			if prof, perr = w.profile(publicHops(opCtx), root.u); perr != nil {
				return webMatch{}, w.callErr(ctx, opCtx, root, fmt.Errorf("web: fetching %s failed: %w", root, perr))
			}
		}
	}
	m := webMatch{root: root, project: root.String(), stars: -1, primary: true}
	var tried []string
	llms := w.findLLMSTxt(opCtx, root, &tried)
	m.title, m.description = llms.title, llms.description
	if m.title == "" {
		info, err := w.rootPage(opCtx, root)
		if err != nil {
			return webMatch{}, w.callErr(ctx, opCtx, root, fmt.Errorf("web: fetching %s failed: %w", root, err))
		}
		m.title = info.title
		m.description = firstNonEmpty(m.description, info.description)
	}
	if m.title == "" {
		m.title = root.u.Host + strings.TrimSuffix(root.prefix, "/")
	}
	if perr == nil {
		w.describeURL(opCtx, &m, prof, llms)
	}
	m.evidence = append(moved, m.evidence...)
	return m, nil
}

// movedRoot follows a docs URL the caller passed that redirects to another
// site (docs.python-requests.org to requests.readthedocs.io): the root the
// page it lands on sits under (siteRootOf), when that site is corroborated
// as a project's docs, as a docs URL's evidence is (linkedProject): it
// links to a repository whose GitHub homepage is that site, and the
// redirect is public all the way. prof is the root's profile when the
// caller has it. ok is false for anything else, so the redirect is refused
// as before.
func (w *Web) movedRoot(ctx context.Context, root webRoot, prof *siteProfile) (webRoot, evidence, bool) {
	ctx = publicHops(ctx)
	if prof == nil {
		p, err := w.profile(ctx, root.u)
		if err != nil {
			return webRoot{}, evidence{}, false
		}
		prof = p
	}
	served := prof.served
	if served == nil || root.contains(served) || !isPublicWebURL(served) || isNonDocsHost(served.Hostname()) {
		return webRoot{}, evidence{}, false
	}
	if _, ok := canonicalRoot(root, served); ok {
		return webRoot{}, evidence{}, false
	}
	next, err := parseWebRoot(siteRootOf(served).String())
	if err != nil || !next.contains(served) || scopeHost(next.u) == scopeHost(root.u) {
		return webRoot{}, evidence{}, false
	}
	p, _ := w.linkedProject(ctx, next, prof)
	if p == nil {
		return webRoot{}, evidence{}, false
	}
	if links, err := linkEvidence(next, prof, p); err != nil || !hasEvidence(links, evLinksRepo) {
		return webRoot{}, evidence{}, false
	}
	return next, evidence{kind: evNote, detail: fmt.Sprintf("%s moved to %s, %s", root, next, movedWhy(p))}, true
}

// movedWhy says what vouches for a moved docs root: the repository's
// GitHub homepage, or keyless, the package naming it.
func movedWhy(p *project) string {
	if p.info == nil && len(p.notes) > 0 {
		return "the docs " + p.notes[0].detail
	}
	return fmt.Sprintf("the docs github %s names as its homepage", p.repo)
}

// describeURL adds a docs URL's versions and evidence: a repository the
// site links to whose GitHub homepage is this site, and what follows from
// it (back-links, the site naming it, Read the Docs agreeing).
func (w *Web) describeURL(ctx context.Context, m *webMatch, prof *siteProfile, llms llmsTxt) {
	m.prof = prof
	vs := w.siteVersions(ctx, m.root, prof)
	m.versions, m.version = vs.labels(), currentLabel(vs, prof.title)
	p, notes := w.linkedProject(ctx, m.root, prof)
	evs := notes
	if p != nil {
		m.proj, m.packages, m.stars = p, p.specs(), p.stars()
		if p.info != nil {
			evs = append(evs, evidence{kind: evRepoHomepage, detail: "github " + p.repo.String() + " homepage"})
		} else {
			evs = append(evs, p.notes...) // keyless: the package naming the site
		}
		links, _ := linkEvidence(m.root, prof, p)
		evs = append(evs, links...)
		evs = append(evs, projectEvidence(p)...)
		if ev, ok := siteNamesEvidence(p, "title", prof.title, "name", prof.meta["og:site_name"], "llms.txt", llms.title, "build", vs.project); ok {
			evs = append(evs, ev)
		}
		if k, ok := repoOfURL(vs.repoURL); ok && p.isRepo(k) {
			evs = append(evs, evidence{kind: evRTDRepo, detail: "Read the Docs records github.com/" + k.String()})
		}
	}
	m.evidence = uniqueEvidence(evs)
	m.trust, _ = score(m.evidence)
}

// linkedProject finds the repository a docs site belongs to: among the
// first repositories it links to, one whose GitHub homepage is this site.
// notes report a GitHub that could not be asked.
func (w *Web) linkedProject(ctx context.Context, root webRoot, prof *siteProfile) (*project, []evidence) {
	gh := w.gh()
	if gh == nil {
		return w.linkedProjectKeyless(ctx, root, prof), nil
	}
	links := repoLinks(prof)
	for _, k := range links[:min(len(links), webMaxURLRepos)] {
		actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
		info, err := gh.repo(actx, k)
		cancel()
		var limit *ghRateLimitError
		switch {
		case errors.As(err, &limit):
			return nil, []evidence{{kind: evNote, detail: limit.Error()}}
		case err != nil:
			continue
		}
		if home, reason := checkMetaURL(info.homepage); reason == "" && sameSite(home, root.u) {
			p := &project{key: info.key.spec(), repo: info.key, info: &info, exact: true}
			if info.key != k {
				p.aliases = []repoKey{k}
			}
			return p, nil
		}
	}
	return nil, nil
}

// linkedProjectKeyless is linkedProject without GitHub: among the first
// repositories the site links to, one with a package named like it that
// names the repository back (addRepoPackages) and names this site as its
// documentation or homepage. Both sides are owner-set, so the site and
// the package agree on each other. The project carries that as a note,
// with the package's evidence kind.
func (w *Web) linkedProjectKeyless(ctx context.Context, root webRoot, prof *siteProfile) *project {
	if w.pkgs == nil {
		return nil
	}
	links := repoLinks(prof)
	for _, k := range links[:min(len(links), webMaxURLRepos)] {
		p := &project{key: k.spec(), repo: k, exact: true}
		w.addRepoPackages(ctx, libSpec{raw: k.spec(), eco: ecoGitHub, name: k.String()}, p, &resolveLog{})
		for _, pkg := range p.pkgs {
			for _, f := range []struct {
				raw  string
				kind evidenceKind
				what string
			}{{pkg.docsURL, evPkgDocs, " lists it as Documentation"}, {pkg.homepage, evPkgHomepage, " homepage"}} {
				if u, reason := checkMetaURL(f.raw); reason == "" && sameSite(u, root.u) {
					p.notes = append(p.notes, evidence{kind: f.kind, detail: pkgLabel(pkg) + f.what})
					return p
				}
			}
		}
	}
	return nil
}

// sameSite is V1's test: the same host, "www." aside, and one path a
// prefix of the other.
func sameSite(a, b *url.URL) bool {
	ra, errA := parseWebRoot(a.String())
	rb, errB := parseWebRoot(b.String())
	return errA == nil && errB == nil && scopeHost(ra.u) == scopeHost(rb.u) &&
		(strings.HasPrefix(ra.prefix, rb.prefix) || strings.HasPrefix(rb.prefix, ra.prefix))
}

// lastResort is the web search (plan §4 step 4e), run when nothing but
// registry docs verified: each hit's root is tried against the projects
// with a repository, or, with none, against the repositories the hits
// link to. It returns the matches and any projects it made.
func (r *resolution) lastResort(ctx context.Context, projects []*project) ([]webMatch, []*project) {
	cands := r.w.lastResortSearch(ctx, searchName(r.spec))
	if len(cands) == 0 {
		return nil, nil
	}
	var targets, extra []*project
	for _, p := range projects {
		if !p.repo.isZero() {
			targets = append(targets, p)
		}
	}
	if len(targets) == 0 {
		extra = r.projectsFromHits(ctx, cands)
		targets = extra
	}
	found := make([]*webMatch, len(cands))
	var wg sync.WaitGroup
	for i, c := range cands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			found[i] = r.verifyHit(ctx, targets, c)
		}()
	}
	wg.Wait()
	var out []webMatch
	for _, m := range found {
		if m != nil {
			out = append(out, *m)
		}
	}
	return out, extra
}

// verifyHit verifies a search hit for the first target it belongs to.
//
// Keyless, a hit is a candidate of the search's, never of a project's: a
// hit that does not answer (a 403, a timeout) is a rejection, not a site of
// the project failing, so it can neither block a pick (rivals) nor turn a
// resolution into an upstream error.
func (r *resolution) verifyHit(ctx context.Context, targets []*project, c rootCandidate) *webMatch {
	for _, p := range targets {
		m, err := r.w.verifyRoot(ctx, p, c)
		if err == nil {
			return &m
		}
		var rej *rejectError
		if r.keyless && !errors.As(err, &rej) {
			r.log.reject("%s: search hit %s did not answer: %v", p.label(), c.url, err)
			return nil // it answers for no target either
		}
		r.noteRejection(p, err)
	}
	return nil
}

// projectsFromHits makes projects of the repositories, named like the
// library, that the search hits link to.
func (r *resolution) projectsFromHits(ctx context.Context, cands []rootCandidate) []*project {
	variants := nameSet(searchName(r.spec))
	seen := map[repoKey]bool{}
	var keys []repoKey
	for _, c := range cands {
		prof, err := r.w.cachedProfile(ctx, c.url)
		if err != nil {
			continue
		}
		for _, k := range repoLinks(prof) {
			if variants[k.name] && !seen[k] && len(keys) < webMaxHitRepos {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	var out []*project
	for _, k := range keys {
		out = append(out, &project{key: k.spec(), repo: k, fromSearch: true, fromHits: true})
	}
	// Only a repository GitHub knows, and not a fork, can stand behind a
	// hit, and then only through its own homepage (relatedEvidence). One
	// GitHub did not read is still considered: it may have been the one.
	out = r.w.enrich(ctx, out, r.log)
	r.log.consider(out...)
	return slices.DeleteFunc(out, func(p *project) bool {
		switch {
		case p.info == nil:
			return true
		case p.info.fork:
			r.log.reject("github %s: a fork of %s, linked from a search hit", p.repo, p.info.parent)
			return true
		}
		return false
	})
}

// searchName is the name a web search asks about: a repository's or Go
// module's last element, else the name as given.
func searchName(spec libSpec) string {
	if spec.eco == ecoGitHub || spec.eco == ecoGo {
		return spec.name[strings.LastIndexByte(spec.name, '/')+1:]
	}
	return spec.name
}

// lastResortSearch asks the site searcher for "<name> documentation" and
// turns each hit into a candidate root (plan §4 step 4e), within its own
// sub-budget. Nil when there is no searcher or it fails. The search runs
// without the resolution's redirect check (publicHops): the search
// backends are the operator's own configuration, and a self-hosted one
// (SearXNG on localhost) may redirect within its own network; the hits'
// pages are fetched under the check as usual.
func (w *Web) lastResortSearch(ctx context.Context, name string) []rootCandidate {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	results, _ := w.searchHits(ctx, name+" documentation")
	return hitRoots(results, name)
}

// searchHits asks the site searcher for query within its own sub-budget;
// nil with no searcher. The error is the search's own.
func (w *Web) searchHits(ctx context.Context, query string) ([]search.Result, error) {
	if w.sites == nil {
		return nil, nil
	}
	sctx, cancel := context.WithTimeout(httpx.WithoutRedirectCheck(ctx), webSearchTimeout)
	defer cancel()
	return w.sites.Search(sctx, query, webSearchHits)
}

// hitRoots turns search hits for the library name into candidate roots
// (searchRootFor), at most webMaxSearchRoots, skipping code forges,
// registries and other non-docs hosts.
func hitRoots(results []search.Result, name string) []rootCandidate {
	set := &candidateSet{}
	for _, res := range results {
		u, err := url.Parse(strings.TrimSpace(res.URL))
		if err != nil || !isPublicWebURL(u) || isNonDocsHost(u.Hostname()) || isRegistryHost(u) {
			continue
		}
		set.add(searchRootFor(u, name), false, true, nil)
		if len(set.list) == webMaxSearchRoots {
			break
		}
	}
	return set.sorted()
}

// matchGroup is one project's verified matches.
type matchGroup struct {
	proj    *project
	matches []webMatch
}

// best is the group's primary match.
func (g matchGroup) best() webMatch { return g.matches[0] }

// assemble orders the matches (plan §4 step 7): per project a primary root
// (its best site) and its registry docs as a secondary match; projects by
// name-match strength, then stars, then trust. A project that dominates
// the rest by stars leaves only itself.
func (r *resolution) assemble(projects []*project, matches []webMatch) []webMatch {
	var groups []matchGroup
	index := map[string]int{}
	for _, m := range matches {
		if m.proj != nil {
			// The weight as it stands now: a keyless resolution reads
			// popularity only once verification is over (readTieBreak).
			m.stars, m.starsUnread = m.proj.stars(), m.proj.weightUnread()
		}
		i, ok := index[m.project]
		if !ok {
			i = len(groups)
			index[m.project] = i
			groups = append(groups, matchGroup{proj: m.proj})
		}
		groups[i].matches = append(groups[i].matches, m)
	}
	for i := range groups {
		indexOnly := !slices.ContainsFunc(groups[i].matches, func(m webMatch) bool { return !indexHosted(m.root.u) })
		for j := range groups[i].matches {
			groups[i].matches[j].indexOnly = indexOnly
		}
		groups[i].matches = pickRoots(groups[i].matches)
	}
	groups = mergeSameRoot(groups)
	slices.SortStableFunc(groups, func(a, b matchGroup) int { return r.compareGroups(a, b) })
	groups = r.applyHint(groups)
	groups = r.pruneDominated(groups)
	var out []webMatch
	for _, g := range groups {
		out = append(out, g.matches...)
	}
	return out
}

// pickRoots keeps a project's best site and its best registry docs, the
// site first; registry docs lead only when there is no site.
//
// A root the project's own metadata led to comes before one only a web
// search found: the search finds docs the registries do not name, it does
// not overrule the docs they do (svelte.dev's /docs/ over the svelte.dev
// a hit stands for). Among those, a root the metadata names itself (a
// documentation URL, a homepage that is the docs) comes before a separate
// one it reaches only through a homepage's links (namedDirectly): a
// homepage is the front door of everything its owner publishes, previous
// majors and sister projects included (psycopg.org links psycopg2's /docs/
// as well as psycopg 3's /psycopg3/docs/, which psycopg's Documentation
// URL names), while a documentation URL is this package's own. Evidence a
// search hit added to the other root does not lift it past the one the
// metadata names. One root inside the other is the same docs at two
// scopes (docs.pola.rs, and the API reference below it that its
// Documentation URL names): trust decides, as for the rest. A root that
// links to the repository only at the address it moved from
// (webMatch.oldRepo: jest's archive.jestjs.io, still linking facebook/jest,
// beside jestjs.io/docs linking jestjs/jest) comes after the project's
// other sites: it was left behind by the move. A root only the package's
// README ties to it (readmeOnly) comes after every other root, registry-
// hosted docs included: a README links other projects' sites too, and
// the docs the metadata or the registry names are the package's own.
func pickRoots(ms []webMatch) []webMatch {
	ms = mergeRootEvidence(ms)
	slices.SortStableFunc(ms, rootOrder)
	site := slices.IndexFunc(ms, func(m webMatch) bool { return !m.registry })
	if site >= 0 && !namedDirectly(ms[site].evidence) {
		top := ms[site]
		named := slices.IndexFunc(ms, func(m webMatch) bool {
			return !m.registry && m.fromHit == top.fromHit && m.oldRepo == top.oldRepo && namedDirectly(m.evidence) &&
				!top.root.contains(m.root.u) && !m.root.contains(top.root.u)
		})
		if named >= 0 {
			site = named
		}
	}
	reg := slices.IndexFunc(ms, func(m webMatch) bool { return m.registry })
	order := []int{site, reg}
	if site >= 0 && reg >= 0 && readmeOnly(ms[site].evidence) {
		order = []int{reg, site}
	}
	var out []webMatch
	for _, i := range order {
		if i >= 0 {
			out = append(out, ms[i])
		}
	}
	out[0].primary = true
	return out
}

// indexOnlyProjects are the keys of the projects among matches whose
// every verified root a package index hosts (webMatch.indexOnly).
func indexOnlyProjects(matches []webMatch) map[string]bool {
	site := map[string]bool{}
	for _, m := range matches {
		site[m.project] = site[m.project] || !indexHosted(m.root.u)
	}
	out := map[string]bool{}
	for k, s := range site {
		if !s {
			out[k] = true
		}
	}
	return out
}

// rootOrder is pickRoots' order: README-only roots last, then search
// hits, then roots left behind by a repository move, each by trust.
func rootOrder(a, b webMatch) int {
	after := func(aLast bool) int {
		if aLast {
			return 1
		}
		return -1
	}
	switch ra, rb := readmeOnly(a.evidence), readmeOnly(b.evidence); {
	case ra != rb:
		return after(ra)
	case a.fromHit != b.fromHit:
		return after(a.fromHit)
	case a.oldRepo != b.oldRepo:
		return after(a.oldRepo)
	}
	return cmp.Compare(b.trust, a.trust)
}

// readmeOnly reports evidence whose only metadata edge is the package's
// README (evPkgReadme): no documentation URL, homepage, nav chain or
// repository homepage names the root.
func readmeOnly(evs []evidence) bool {
	return hasEvidence(evs, evPkgReadme) && !namedDirectly(evs) && !hasEvidence(evs, evNavChain)
}

// namedDirectly reports evidence that the project's own metadata names the
// root itself: a documentation URL, a homepage that is the docs, the
// repository's homepage. A nav chain (a homepage linking to the root) is
// not.
func namedDirectly(evs []evidence) bool {
	return hasEvidence(evs, evPkgDocs) || hasEvidence(evs, evPkgHomepage) || hasEvidence(evs, evRepoHomepage)
}

// mergeRootEvidence joins one project's matches that verification moved to
// one root (a homepage and a docs URL both redirecting into /en/stable/):
// their evidence is about the same docs, so it adds up.
func mergeRootEvidence(ms []webMatch) []webMatch {
	var out []webMatch
	at := map[string]int{}
	for _, m := range ms {
		k := linkKey(m.root.u) + " " + strconv.FormatBool(m.registry)
		i, ok := at[k]
		if !ok {
			at[k] = len(out)
			out = append(out, m)
			continue
		}
		out[i].evidence = uniqueEvidence(append(slices.Clone(out[i].evidence), m.evidence...))
		out[i].trust, _ = score(out[i].evidence)
		out[i].fromHit = out[i].fromHit && m.fromHit
	}
	return out
}

// mergeSameRoot folds a project whose primary root another project already
// has into that one: two packages that name one site are one library.
func mergeSameRoot(groups []matchGroup) []matchGroup {
	var out []matchGroup
	seen := map[string]int{}
	for _, g := range groups {
		k := linkKey(g.best().root.u)
		if i, ok := seen[k]; ok {
			out[i].matches[0].packages = uniqueStrings(append(out[i].matches[0].packages, g.best().packages...)...)
			continue
		}
		seen[k] = len(out)
		out = append(out, g)
	}
	return out
}

// nameRank is how closely a project's name matches the query: an exact
// package name, then a repository of that name.
func (r *resolution) nameRank(p *project) int {
	switch {
	case p == nil:
		return 0
	case p.exact:
		return 2
	case strings.EqualFold(p.repo.name, searchName(r.spec)):
		return 1
	}
	return 0
}

func (r *resolution) compareGroups(a, b matchGroup) int {
	if c := cmp.Compare(r.nameRank(b.proj), r.nameRank(a.proj)); c != 0 {
		return c
	}
	if c := cmp.Compare(b.best().stars, a.best().stars); c != 0 {
		return c
	}
	// A project with a docs site of its own before one whose docs only a
	// package index hosts, the squatter signal (dominant): flask's site,
	// not crates' flask, when GitHub could not say which has the stars.
	if a.best().indexOnly != b.best().indexOnly {
		if a.best().indexOnly {
			return 1
		}
		return -1
	}
	if c := cmp.Compare(b.best().trust, a.best().trust); c != 0 {
		return c
	}
	return cmp.Compare(a.best().project, b.best().project)
}

// pruneDominated keeps only the top project when it dominates the rest
// (dominant): the junk same-name crate beside Next.js.
func (r *resolution) pruneDominated(groups []matchGroup) []matchGroup {
	best := make([]webMatch, len(groups))
	for i, g := range groups {
		best[i] = g.best()
	}
	top, ok := dominant(best)
	if !ok {
		return groups
	}
	t := best[top]
	for i, m := range best {
		switch {
		case i == top:
		case m.stars < 0:
			r.log.reject("%s: outweighed by %s (%d stars, to docs only a package index hosts and no star count)", m.project, t.project, t.stars)
		default:
			r.log.reject("%s: outweighed by %s (%d stars to %d)", m.project, t.project, t.stars, m.stars)
		}
	}
	return []matchGroup{groups[top]}
}

// dominant returns the index of the project, among each project's primary
// match, that dominates the others, so single-library contexts pick it
// without asking (decision-3):
//
//   - The top project's GitHub stars are known and at least dominanceStars.
//   - Every other project whose count is known has at most
//     1/dominanceRatio of them. A repository GitHub says does not exist
//     counts 0 (project.stars): docs.rs/fastapi, whose repo nxpkg/fastapi
//     is gone, cannot block FastAPI.
//   - A project whose count is unknown blocks dominance, since it could be
//     the popular one (an old PyPI package that lists only its homepage, a
//     GitLab project), unless its only docs are hosted by a package index
//     (indexOnly: docs.rs, pkg.go.dev, pythonhosted.org). Such docs show
//     that a package was published, not that it is the library meant: the
//     squatter signal (crates' requests on GitLab beside psf/requests,
//     PyPI's Clap on pythonhosted.org beside clap-rs/clap). Not when
//     GitHub failed to report the count (starsUnread): a rate limit says
//     nothing about the project, which may be the popular one.
//   - A project whose only docs a package index hosts, with a known count,
//     blocks a top project with a docs site of its own only when it is at
//     least as popular (decision-15): every published crate and module has
//     such docs, so they show publication, not that the name means it. A
//     popular library is not refused for a namesake it outweighs (arrow's
//     site beside crates' arrow on docs.rs).
//
// Two projects with known, modest counts (900 and 300) stay ambiguous.
func dominant(ms []webMatch) (int, bool) {
	if len(ms) < 2 {
		return 0, false
	}
	top := 0
	for i, m := range ms {
		if m.stars > ms[top].stars {
			top = i
		}
	}
	t := ms[top]
	if t.stars < dominanceStars {
		return 0, false
	}
	for i, m := range ms {
		switch {
		case i == top:
		case m.stars >= 0 && m.indexOnly && !t.indexOnly:
			if m.stars >= t.stars {
				return 0, false
			}
		case m.stars >= 0:
			if t.stars < dominanceRatio*m.stars {
				return 0, false
			}
		case !m.indexOnly || m.starsUnread:
			return 0, false
		}
	}
	return top, true
}

// finish describes the first matches (title, versions, the site's own
// naming), applies a requested version and drops the matches that do not
// offer it.
func (r *resolution) finish(ctx context.Context, matches []webMatch) ([]webMatch, error) {
	n := min(len(matches), webMaxVersioned)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = r.describe(ctx, &matches[i])
		}()
	}
	wg.Wait()
	if r.spec.version == "" {
		return matches, nil
	}
	var kept []webMatch
	for i := range n {
		if errs[i] == nil {
			kept = append(kept, matches[i])
		}
	}
	if len(kept) == 0 {
		return nil, errs[0]
	}
	return kept, nil
}

// describe completes a match: llms.txt's title, the site's versions, the
// evidence they add (the site naming the project, Read the Docs recording
// its repository), and the requested version.
func (r *resolution) describe(ctx context.Context, m *webMatch) error {
	vs := r.w.siteVersions(ctx, m.root, m.prof)
	if child, ok := indexChild(m.root, vs); ok && !m.registry && r.enterIndex(ctx, m, child) {
		vs = r.w.siteVersions(ctx, m.root, m.prof)
	}
	llms := llmsTxt{}
	if !m.registry {
		llms = r.w.cachedLLMS(ctx, m.root)
	}
	m.title = firstNonEmpty(llms.title, vs.project, m.title)
	m.description = firstNonEmpty(m.description, llms.description)
	m.evidence = append(m.evidence, siteEvidence(m, vs, llms)...)
	m.versions, m.version = vs.labels(), currentLabel(vs, m.prof.title)
	var err error
	if r.spec.version != "" {
		err = r.pickVersion(ctx, m, vs)
	}
	m.evidence = uniqueEvidence(m.evidence)
	m.trust, _ = score(m.evidence)
	return err
}

// enterIndex moves m from a version index to the current docs it links
// (indexChild), when they are served from within their own root. The
// project's evidence stands: the docs are below the root it verified.
func (r *resolution) enterIndex(ctx context.Context, m *webMatch, child siteVersion) bool {
	root, err := parseWebRoot(child.root)
	if err != nil {
		return false
	}
	prof, err := r.w.cachedProfile(ctx, root.u)
	if err != nil {
		return false
	}
	if canon, ok := canonicalRoot(root, prof.served); ok {
		root = canon
	} else if !root.contains(prof.served) {
		return false
	}
	from := m.root
	m.root, m.prof, m.title = withOrigin(root, prof.served), prof, siteTitle(prof, root)
	m.description = firstNonEmpty(prof.description, m.description)
	m.evidence = append(m.evidence, evidence{kind: evNote, detail: fmt.Sprintf("%s is a version index; its %s docs are at %s", from, child.label, m.root)})
	return true
}

// siteEvidence is what the site's llms.txt and version sources add.
func siteEvidence(m *webMatch, vs versionSet, llms llmsTxt) []evidence {
	var evs []evidence
	if !hasEvidence(m.evidence, evSiteNames) {
		if ev, ok := siteNamesEvidence(m.proj, "llms.txt", llms.title, "build", vs.project); ok {
			evs = append(evs, ev)
		}
	}
	if k, ok := repoOfURL(vs.repoURL); ok && m.proj != nil && m.proj.isRepo(k) {
		evs = append(evs, evidence{kind: evRTDRepo, detail: "Read the Docs records github.com/" + k.String()})
	}
	return evs
}

// repoOfURL parses a repository URL as published (Read the Docs' record).
func repoOfURL(raw string) (repoKey, bool) {
	if raw == "" {
		return repoKey{}, false
	}
	if k, ok := parseRepoURL(raw); ok {
		return k, true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return repoKey{}, false
	}
	return githubRepoOf(u)
}

// currentLabel is the site's current version label, concrete where any
// source says which release it is ("2.4" over "stable"): an alias of the
// current version, another version served from the same root, or a listed
// release the current page's title names ("Widget 2.4 documentation", as
// Sphinx titles pages); else the label as it is, "" when the site lists
// none.
func currentLabel(vs versionSet, title string) string {
	if len(vs.versions) == 0 {
		return ""
	}
	cur := vs.versions[0] // tidyVersions puts the current one first
	for _, v := range vs.versions {
		if v.current {
			cur = v
			break
		}
	}
	for _, l := range versionLabels(cur) {
		if isNumericLabel(l) {
			return l
		}
	}
	if root, err := url.Parse(cur.root); err == nil && cur.root != "" {
		for _, v := range vs.versions {
			if isNumericLabel(v.label) && sameRootURL(v.root, webRoot{u: root}) {
				return v.label
			}
		}
	}
	for _, tok := range nameTokens(title) {
		for _, v := range vs.versions {
			if isNumericLabel(v.label) && hasVersionLabel(v, tok) {
				return v.label
			}
		}
	}
	return cur.label
}

// pickVersion moves m to the version the spec asks for (plan §6): a
// version the site lists (matchVersion, then its dotless labels read as
// releases: SQLAlchemy's 14 is 1.4); else the current docs when they are
// the release asked for (assumeLatest); else where the site's own layout
// puts that version's docs (probeVersion: 18.react.dev, /docs/13/,
// /en/4x/ beside /en/5x/).
func (r *resolution) pickVersion(ctx context.Context, m *webMatch, vs versionSet) error {
	want := r.spec.version
	if m.registry {
		return r.registryVersion(ctx, m, want)
	}
	latest := latestRelease(m.proj)
	sv, ok := matchVersion(want, vs.versions)
	if !ok {
		sv, ok = matchVersion(want, dottedLabels(vs.versions, latest))
	}
	switch {
	case ok:
		return r.adoptVersion(ctx, m, sv, want, vs.labels())
	case (len(vs.versions) == 0 || vs.versions[0].label == labelCurrent) && r.assumeLatest(m, want, latest):
		return nil // the current docs carry no release of their own
	}
	if found, err := r.probeVersion(ctx, m, want, vs); found || err != nil {
		return err
	}
	if len(vs.versions) == 0 {
		return fmt.Errorf("web: %s publishes no version list, so %s@%s cannot be checked; pass the versioned docs URL: %w", m.root, r.spec.name, want, ErrNotFound)
	}
	return r.versionNotFound(want, vs.labels(), "")
}

// registryVersion moves registry docs to the requested version.
func (r *resolution) registryVersion(ctx context.Context, m *webMatch, want string) error {
	i := slices.IndexFunc(m.proj.pkgs, func(p pkgInfo) bool { return p.registryDocs != "" })
	if i < 0 {
		return r.versionNotFound(want, nil, "")
	}
	pkg, goVersions := r.w.goModuleVersions(ctx, m.proj.pkgs[i], want)
	sv, ok := registryVersionRoot(pkg, want, goVersions)
	if !ok {
		return r.versionNotFound(want, goVersions, "")
	}
	return r.adoptVersion(ctx, m, sv, want, nil)
}

// adoptVersion moves m to sv's root after the post-check: a version root
// that carries a label must still carry one want matches once served. A
// version root with no page of its own (express's /en/4x/, pydantic's
// /docs/validation/1.10/) is read through the page the current docs land
// on, at the same place in that version's tree. docs.rs docs whose build
// failed (served as the crate's page) give way to the nearest release of
// the version that built (docsRSBuilt).
func (r *resolution) adoptVersion(ctx context.Context, m *webMatch, sv siteVersion, want string, labels []string) error {
	root, err := parseWebRoot(sv.root)
	if err != nil {
		return r.versionNotFound(want, labels, "")
	}
	prof := m.prof
	if linkKey(root.u) != linkKey(m.root.u) {
		if prof, err = r.versionProfile(ctx, m, root); err != nil {
			if isGone(err) {
				return r.versionNotFound(want, labels, fmt.Sprintf("%s: %v", root, err))
			}
			return fmt.Errorf("web: fetching %s failed: %w", root, err)
		}
	}
	if m.registry && scopeHost(root.u) == "docs.rs" && !root.contains(prof.served) {
		if _, ok := canonicalRoot(root, prof.served); !ok {
			return r.docsRSBuilt(ctx, m, root, prof, want)
		}
	}
	if !versionRootHolds(want, sv, prof.served) {
		return r.versionNotFound(want, labels, fmt.Sprintf("%s serves %s", root, prof.served))
	}
	label := sv.label
	if canon, ok := canonicalRoot(root, prof.served); ok {
		// Labelled as served: docs.rs's ~1.38 is 1.38.2.
		if l, _ := rootVersion(canon); isNumericLabel(l) {
			label = l
		}
		root = canon
	}
	m.root, m.prof, m.version = root, prof, label
	return nil
}

// versionProfile reads a version root: its own page, else, when it has
// none (a 404), the page the current docs were served from, at the same
// place below this root, which must be served from within it.
func (r *resolution) versionProfile(ctx context.Context, m *webMatch, root webRoot) (*siteProfile, error) {
	prof, err := r.w.cachedProfile(ctx, root.u)
	if err == nil || !errors.Is(err, ErrNotFound) || m.prof == nil || m.prof.served == nil || !m.root.contains(m.prof.served) {
		return prof, err
	}
	tail := strings.TrimPrefix(m.prof.served.Path, m.root.prefix)
	if tail == "" || tail == m.prof.served.Path {
		return prof, err
	}
	page := *root.u
	page.Path, page.RawPath = root.prefix+tail, ""
	alt, aerr := r.w.cachedProfile(ctx, &page)
	if aerr != nil || !root.contains(alt.served) {
		return prof, err
	}
	return alt, nil
}

// latestRelease is the newest release a project's packages name, "" for
// none.
func latestRelease(p *project) string {
	if p == nil {
		return ""
	}
	for _, pkg := range p.pkgs {
		if pkg.version != "" {
			return pkg.version
		}
	}
	return ""
}

// assumeLatest takes m's current docs for want when they are the release
// asked for: want names the package's latest release on dot boundaries
// (7 or 7.1 for 7.1.2), noting the assumption. A different minor of the
// same major (tokio 1.38 beside the latest 1.54.1) is not: the current
// docs document the latest.
func (r *resolution) assumeLatest(m *webMatch, want, latest string) bool {
	if latest == "" || !namesRelease(want, latest) {
		return false
	}
	m.version = latest
	m.evidence = append(m.evidence, evidence{kind: evNote, detail: fmt.Sprintf("the docs name no release; taken as %s, the latest, which %s names", latest, want)})
	return true
}

// webMaxVersionProbes caps the version roots probeVersion tries.
const webMaxVersionProbes = 4

// versionProbe is a version root probeVersion tries: page is fetched (a
// page of that version's tree, or the root itself), root must hold it.
type versionProbe struct {
	v    siteVersion
	page *url.URL
}

// probeVersion looks for want's docs where the site's own layout puts
// them when its version list does not name them (versionProbes): in the
// URL scheme its listed versions use, else a version host label or path
// segment beside the current docs. A probe counts only when it is served
// from within the version root it stands for, on the site's registrable
// domain, and links to another page there, so a catch-all page or a
// redirect to the current docs is not taken for a version. found is
// false, with a nil error, when no probe held.
func (r *resolution) probeVersion(ctx context.Context, m *webMatch, want string, vs versionSet) (found bool, err error) {
	probes := versionProbes(m.root, m.prof, vs, want)
	probes = append(probes, r.w.releaseProbes(ctx, m.root, m.proj, want, probes)...)
	if len(probes) == 0 {
		return false, nil
	}
	type held struct {
		root webRoot
		prof *siteProfile
	}
	results := make([]*held, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Go(func() {
			root, err := parseWebRoot(p.v.root)
			if err != nil || registrableDomain(root.u.Hostname()) != registrableDomain(m.root.u.Hostname()) {
				return
			}
			prof, err := r.w.cachedProfile(ctx, p.page)
			if err != nil && linkKey(p.page) == linkKey(root.u) {
				prof, err = r.versionProfile(ctx, m, root)
			}
			if err != nil || !root.contains(prof.served) || !linksWithin(prof, root) || !versionRootHolds(want, p.v, prof.served) {
				return
			}
			results[i] = &held{root: withOrigin(root, prof.served), prof: prof}
		})
	}
	wg.Wait()
	for i, h := range results {
		if h == nil {
			continue
		}
		from := m.root
		m.root, m.prof, m.version = h.root, h.prof, probes[i].v.label
		m.evidence = append(m.evidence, evidence{kind: evNote, detail: fmt.Sprintf("%s lists no %s; its docs for it are at %s", from, want, h.root)})
		return true, nil
	}
	if ctx.Err() != nil {
		return false, fmt.Errorf("web: looking for %s@%s beside %s: %w", r.spec.name, want, m.root, ctx.Err())
	}
	return false, nil
}

// versionProbes lists where want's docs may be beside root, best first, at
// most webMaxVersionProbes: in the style and place of a release the site
// lists (express's /en/5x/ gives /en/4x/, reached through the page of the
// 5x tree the root links to); else, with no release listed, the root's own
// version segment or host label swapped for want, or for an unversioned
// root a version host label (18.react.dev, v3.tailwindcss.com) or a version
// segment below it (nextjs.org/docs/13/), as N and vN.
func versionProbes(root webRoot, prof *siteProfile, vs versionSet, want string) []versionProbe {
	w, ok := parseVerNum(want)
	if !ok || root.u == nil {
		return nil
	}
	for _, v := range vs.versions {
		if label, ok := styledLabel(v.label, want); ok && !v.current {
			if p, ok := siblingProbe(root, prof, v, label); ok {
				return []versionProbe{p}
			}
		}
	}
	major := strconv.Itoa(w.parts[0])
	majors := []string{major, "v" + major}
	var out []versionProbe
	add := func(u *url.URL, label string) {
		if len(out) < webMaxVersionProbes && linkKey(u) != linkKey(root.u) {
			out = append(out, versionProbe{v: siteVersion{label: label, root: u.String()}, page: u})
		}
	}
	segs := pathSegments(root.prefix)
	switch label, seg := rootVersion(root); {
	case seg >= 0:
		for _, l := range uniqueStrings(append([]string{normLabel(want)}, majors...)...) {
			s := slices.Clone(segs)
			s[seg] = l
			add(&url.URL{Scheme: root.u.Scheme, Host: root.u.Host, Path: segmentsPath(s)}, l)
		}
	case label != "":
		_, rest, _ := strings.Cut(root.u.Host, ".")
		for _, l := range majors {
			add(&url.URL{Scheme: root.u.Scheme, Host: l + "." + rest, Path: root.prefix}, l)
		}
	default:
		host := strings.TrimPrefix(root.u.Host, "www.")
		for _, l := range majors {
			add(&url.URL{Scheme: root.u.Scheme, Host: l + "." + host, Path: root.prefix}, l)
		}
		for _, l := range majors {
			add(&url.URL{Scheme: root.u.Scheme, Host: root.u.Host, Path: root.prefix + l + "/"}, l)
		}
	}
	return out
}

// siblingProbe puts label where v's own label is in v's root (a path
// segment or the first host label), and probes it through a page of v's
// tree the root links to, moved the same way: a version root need not be a
// page itself.
func siblingProbe(root webRoot, prof *siteProfile, v siteVersion, label string) (versionProbe, bool) {
	vu, err := url.Parse(v.root)
	if err != nil {
		return versionProbe{}, false
	}
	vroot, ok := swapLabel(root, vu, v.label, label)
	if !ok {
		return versionProbe{}, false
	}
	page := vroot
	if vr, err := parseWebRoot(v.root); err == nil && prof != nil {
		for _, l := range prof.links {
			if vr.contains(l.url) && linkKey(l.url) != linkKey(vr.u) {
				if p, ok := swapLabel(root, l.url, v.label, label); ok {
					page = p
				}
				break
			}
		}
	}
	return versionProbe{v: siteVersion{label: label, root: vroot.String()}, page: page}, true
}

// swapLabel replaces the version label from in u with to: its first host
// label when u is on another host than root (v2.vite.dev), else its first
// path segment carrying from. ok is false when u does not carry from.
func swapLabel(root webRoot, u *url.URL, from, to string) (*url.URL, bool) {
	out := *u
	out.Fragment, out.RawFragment, out.RawQuery = "", "", ""
	if scopeHost(u) != scopeHost(root.u) {
		first, rest, ok := strings.Cut(u.Host, ".")
		if l, isLabel := segmentLabel(first); !ok || !isLabel || normLabel(l) != normLabel(from) {
			return nil, false
		}
		out.Host = to + "." + rest
		return &out, true
	}
	segs := pathSegments(u.Path)
	for i, s := range segs {
		if l, ok := segmentLabel(s); ok && normLabel(l) == normLabel(from) {
			segs[i] = to
			out.Path, out.RawPath = segmentsPath(segs), ""
			if !strings.HasSuffix(u.Path, "/") {
				out.Path = strings.TrimSuffix(out.Path, "/")
			}
			return &out, true
		}
	}
	return nil, false
}

// linksWithin reports a page linking to another page under root.
func linksWithin(prof *siteProfile, root webRoot) bool {
	self := ""
	if prof.served != nil {
		self = linkKey(prof.served)
	}
	for _, l := range prof.links {
		if root.contains(l.url) && linkKey(l.url) != self && linkKey(l.url) != linkKey(root.u) {
			return true
		}
	}
	return false
}

// docsRSMaxBuilds caps the releases docsRSBuilt tries.
const docsRSMaxBuilds = 3

// docsRSBuilt answers a docs.rs version root whose build failed (docs.rs
// serves the crate's page instead, prof) with the nearest release that
// built (builtCandidates), at most docsRSMaxBuilds tried. A note says
// which release stands in.
func (r *resolution) docsRSBuilt(ctx context.Context, m *webMatch, root webRoot, prof *siteProfile, want string) error {
	segs := pathSegments(prof.served.Path)
	if len(segs) < 3 || segs[0] != "crate" {
		return r.versionNotFound(want, nil, fmt.Sprintf("%s serves %s", root, prof.served))
	}
	why := fmt.Sprintf("docs.rs failed to build %s %s", segs[1], segs[2])
	cands := builtCandidates(prof, segs[1], segs[2], want)
	for _, label := range cands[:min(len(cands), docsRSMaxBuilds)] {
		s := pathSegments(root.prefix)
		s[1] = label
		u := *root.u
		u.Path, u.RawPath = segmentsPath(s), ""
		built, err := parseWebRoot(u.String())
		if err != nil {
			continue
		}
		bp, err := r.w.cachedProfile(ctx, built.u)
		if err != nil || !built.contains(bp.served) {
			continue
		}
		m.root, m.prof, m.version = withOrigin(built, bp.served), bp, label
		m.evidence = append(m.evidence, evidence{kind: evNote, detail: fmt.Sprintf("%s; %s is the nearest release of %s that built", why, label, want)})
		return nil
	}
	return r.versionNotFound(want, nil, why)
}

// crateReleases lists the releases a docs.rs crate page links
// (/crate/<crate>/<release>), once each, in page order.
func crateReleases(prof *siteProfile, crate string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range prof.links {
		ls := pathSegments(l.url.Path)
		if scopeHost(l.url) == "docs.rs" && len(ls) == 3 && ls[0] == "crate" && ls[1] == crate && !seen[ls[2]] {
			seen[ls[2]] = true
			out = append(out, ls[2])
		}
	}
	return out
}

// builtCandidates lists the releases the crate page prof lists that may
// stand in for failed: of want (its minor when want names a full
// release), no pre-release, those of failed's own minor first, newest
// first.
func builtCandidates(prof *siteProfile, crate, failed, want string) []string {
	f, okF := parseVerNum(failed)
	scope, okW := parseVerNum(want)
	if !okF || !okW {
		return nil
	}
	if len(scope.parts) >= 3 {
		scope = verNum{parts: scope.parts[:2]}
	}
	type release struct {
		label string
		n     verNum
	}
	var cands []release
	for _, label := range crateReleases(prof, crate) {
		if n, ok := parseVerNum(label); ok && label != failed && n.pre == "" && verPrefixMatch(scope, n) {
			cands = append(cands, release{label, n})
		}
	}
	own := func(n verNum) int {
		if len(n.parts) > 1 && len(f.parts) > 1 && n.parts[0] == f.parts[0] && n.parts[1] == f.parts[1] {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(cands, func(a, b release) int {
		return cmp.Or(cmp.Compare(own(a.n), own(b.n)), compareVer(b.n, a.n))
	})
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.label
	}
	return out
}

// versionNotFound is the error for a version the docs do not offer.
func (r *resolution) versionNotFound(want string, labels []string, why string) error {
	msg := fmt.Sprintf("web: no docs for %s@%s", r.spec.name, want)
	if len(labels) > 0 {
		msg += " (versions: " + strings.Join(labels, ", ") + ")"
	}
	if why != "" {
		msg += ": " + why
	}
	return fmt.Errorf("%s: %w", msg, ErrNotFound)
}

// failure is the §8 error for a resolution that verified nothing: the
// caller's cancellation, a plain upstream error when a source failed or
// the budget ran out (with the GitHub token hint when GitHub refused),
// else ErrNotFound with every reason.
func (r *resolution) failure(parent, rctx context.Context) error {
	if err := parent.Err(); err != nil {
		return fmt.Errorf("web: %w", err)
	}
	rejected, failed := r.log.snapshot()
	reasons := joinReasons(rejected)
	if len(failed) == 0 && rctx.Err() == nil {
		if reasons == "" {
			reasons = "no source knows it"
		}
		return fmt.Errorf("web: no docs site verified for %q: %s: %w", r.spec.raw, reasons, ErrNotFound)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "web: could not resolve %q", r.spec.raw)
	if rctx.Err() != nil {
		fmt.Fprintf(&b, " within %s", r.w.resolveTimeout)
	}
	if len(failed) > 0 {
		b.WriteString(": " + joinErrors(failed))
	}
	if reasons != "" {
		b.WriteString("; also: " + reasons)
	}
	if limit := r.log.rateLimited(); limit != nil && !limit.authed && !strings.Contains(b.String(), "github_token") {
		b.WriteString(" (set github_token or gh auth login)")
	}
	// Plain text: a failed call's deadline must not read as cancellation.
	return r.log.upstreamErr(b.String(), rctx.Err() != nil)
}

// joinReasons lists distinct reasons, capped.
func joinReasons(reasons []string) string {
	reasons = uniqueStrings(reasons...)
	if len(reasons) > webMaxReasons {
		reasons = append(reasons[:webMaxReasons], fmt.Sprintf("%d more", len(reasons)-webMaxReasons))
	}
	return strings.Join(reasons, "; ")
}

// joinErrors lists distinct error texts, capped.
func joinErrors(errs []error) string {
	var texts []string
	for _, err := range errs {
		texts = append(texts, err.Error())
	}
	return joinReasons(texts)
}

// pickUnique returns the one library matches name, for single-library
// contexts: the top project's primary match when it is the only project or
// dominates the rest (dominant: >= 1000 stars and >= 50x every other
// project's), else an *AmbiguousLibraryError listing each project's
// primary match.
func pickUnique(library string, matches []webMatch) (webMatch, error) {
	var primaries []webMatch
	seen := map[string]bool{}
	for _, m := range matches {
		if !seen[m.project] {
			seen[m.project] = true
			primaries = append(primaries, m)
		}
	}
	switch len(primaries) {
	case 0:
		return webMatch{}, fmt.Errorf("web: no docs found for %q: %w", library, ErrNotFound)
	case 1:
		return primaries[0], nil
	}
	if top, ok := dominant(primaries); ok {
		return primaries[top], nil
	}
	amb := &AmbiguousLibraryError{Library: library}
	for _, m := range primaries {
		amb.Matches = append(amb.Matches, m.libraryMatch())
	}
	return webMatch{}, amb
}

// libraryMatch is m in the public shape, evidence as text.
func (m webMatch) libraryMatch() LibraryMatch {
	lm := LibraryMatch{
		Title: m.title, Description: m.description, TrustScore: m.trust,
		Versions: m.versions, Version: m.version, Packages: m.packages,
	}
	if m.root.u != nil {
		lm.ID = m.root.String()
	}
	for _, e := range uniqueEvidence(m.evidence) {
		lm.Evidence = append(lm.Evidence, e.detail)
	}
	return lm
}
