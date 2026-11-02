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
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/net/publicsuffix"

	"github.com/1broseidon/ketch/httpx"
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
	notes    []evidence
	exact    bool // a package's normalized name is the queried name
	// fromSearch marks a project found by GitHub repository search rather
	// than a registry.
	fromSearch bool
	// fromHits marks a project made from a repository the web search's hits
	// link to: the hits named it, so they cannot vouch for themselves
	// through it (relatedEvidence).
	fromHits bool
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
func (p *project) stars() int {
	switch {
	case p.info != nil:
		return p.info.stars
	case p.repoGone:
		return 0
	}
	return -1
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

	registry bool         // registry-hosted docs
	proj     *project     // nil for a docs URL the caller passed
	prof     *siteProfile // the root's profile
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
}

// missedSource is a failure that may have kept a library from matching.
type missedSource struct {
	project string // the project it concerns; "" for one that could have added any
	// whole marks a source whose failure may have hidden whole projects (a
	// registry, GitHub search), as opposed to one project's site or
	// repository not answering.
	whole bool
	err   error
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
	// time.
	registryFailed, nameKnown, siteFailed bool
}

func (e *upstreamError) Error() string { return e.msg }

// upstreamErr is msg as an upstreamError carrying log's registry outcome;
// timedOut counts as a site failure.
func (l *resolveLog) upstreamErr(msg string, timedOut bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return &upstreamError{msg: msg, registryFailed: l.registryFailed, nameKnown: l.nameKnown, siteFailed: l.siteFailed || timedOut}
}

// failFor records err as a failure for project: its homepage or a docs
// root did not answer.
func (l *resolveLog) failFor(project string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.missed = append(l.missed, missedSource{project: project, err: err})
	l.failed = append(l.failed, err)
	l.siteFailed = true
}

// failRepo records err as a failure for project's GitHub repository: a
// rate limit, an outage. It hid the repository's homepage and stars only.
func (l *resolveLog) failRepo(project string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.missed = append(l.missed, missedSource{project: project, err: err})
	l.failed = append(l.failed, err)
}

// missedBeside sorts the failures that may have hidden a library other
// than the matched projects: sources down (whole), and projects whose
// site or repository did not answer (partial).
func (l *resolveLog) missedBeside(matched map[string]bool) (whole, partial []error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.missed {
		switch {
		case m.whole:
			whole = append(whole, m.err)
		case !matched[m.project]:
			partial = append(partial, m.err)
		}
	}
	return whole, partial
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

// logFetch records a failed fetch of what, for project: a rejection when
// the site is gone, else a failed source.
func logFetch(log *resolveLog, project, what string, err error) {
	if isGone(err) {
		log.reject("%s: %v", what, err)
		return
	}
	log.failFor(project, fmt.Errorf("%s: %w", what, err))
}

// profileMemo shares site profiles across one resolution: a homepage read
// for its nav links is often the root verified next.
type profileMemo struct {
	mu    sync.Mutex
	calls map[string]*profileCall
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

// cachedProfile is profile, fetched once per resolution per page.
func (w *Web) cachedProfile(ctx context.Context, u *url.URL) (*siteProfile, error) {
	memo, _ := ctx.Value(profileMemoKey{}).(*profileMemo)
	if memo == nil {
		return w.profile(ctx, u)
	}
	key := linkKey(u)
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
	c.p, c.err = w.profile(ctx, u)
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
	return w.resolveRun(ctx, spec, limit, log, false)
}

// resolveRun is resolveLogged, strict for a bare query's guessed reading
// (resolution.strict).
func (w *Web) resolveRun(ctx context.Context, spec libSpec, limit int, log *resolveLog, strict bool) ([]webMatch, error) {
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
	r := &resolution{w: w, spec: spec, log: log, strict: strict}
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
func (r *resolution) run(ctx context.Context) []webMatch {
	projects := r.w.gather(ctx, r.spec, r.log, !r.strict)
	matches := r.verifyAll(ctx, projects)
	if r.strict {
		return r.assemble(projects, matches) // searches find no exact package
	}
	if r.spec.eco == ecoAny && anyExact(projects) && !hasSite(matches) {
		// gatherProjects searched GitHub only when no package matched
		// exactly; exact packages that verified nothing leave room too.
		extra := r.w.searchProjects(ctx, r.spec, projects, r.log)
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

// errNoNameSources is the error for a name given to a backend built
// without resolution sources (NewWeb).
func errNoNameSources(name string) error {
	return fmt.Errorf("%w: this web backend takes the docs site URL as the library, not a name (got %q), e.g. https://docs.astro.build", ErrLibraryRequired, name)
}

// resolveOne resolves spec where exactly one library is needed (GetDocs
// with a name, Search): resolve, then pickUnique. strict applies a strict
// query split's bar (an exact registry name and strictMinTrust).
func (w *Web) resolveOne(ctx context.Context, spec libSpec, strict bool) (webMatch, error) {
	log := &resolveLog{}
	matches, err := w.resolveRun(ctx, spec, 0, log, strict)
	if err != nil {
		return webMatch{}, err
	}
	m, err := pickUnique(spec.raw, matches)
	if err != nil {
		return webMatch{}, err
	}
	if m, err = answerPastFailures(spec, m, matches, log); err != nil {
		return webMatch{}, err
	}
	if strict && (m.proj == nil || !m.proj.exact || m.registry || m.trust < strictMinTrust) {
		return webMatch{}, fmt.Errorf("web: %q is a guess at a library name and %s (trust %.1f) does not prove it: a guess needs an exact package name, its own docs site and trust %.0f: %w",
			spec.raw, m.root, m.trust, strictMinTrust, ErrNotFound)
	}
	return m, nil
}

// answerPastFailures checks the one library picked for spec against the
// sources that failed along the way. A registry or GitHub search that
// failed may have hidden whole projects of that name, which would have
// made it ambiguous or outweighed the pick: the answer is then an upstream
// error naming the pick, to retry or pass as the library, rather than a
// silent guess. Another project whose homepage, docs root or repository
// did not answer (a dead site, a rate limit) is known, just unverified:
// the pick stands, with a note saying what was not checked, so a junk
// package's dead homepage does not sink the name for good.
func answerPastFailures(spec libSpec, m webMatch, matches []webMatch, log *resolveLog) (webMatch, error) {
	matched := map[string]bool{}
	for _, x := range matches {
		matched[x.project] = true
	}
	whole, partial := log.missedBeside(matched)
	if len(whole) > 0 {
		msg := fmt.Sprintf("web: %q resolved to %s, but %s, so another library of that name may have been missed; retry, or pass %s as the library",
			spec.raw, m.root, joinErrors(whole), m.root)
		if limit := log.rateLimited(); limit != nil && !limit.authed && !strings.Contains(msg, "github_token") {
			msg += " (set github_token or gh auth login)"
		}
		return webMatch{}, log.upstreamErr(msg, false) // plain: upstream, whatever deadline the failures carry
	}
	if len(partial) > 0 {
		m.evidence = append(m.evidence, evidence{kind: evNote, detail: "not checked, so another library of this name may be missing: " + joinErrors(partial)})
	}
	return m, nil
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
	whole, partial := log.missedBeside(matched)
	if len(whole)+len(partial) == 0 {
		return evidence{}, false
	}
	return evidence{kind: evNote, detail: "this list may be incomplete: " + joinErrors(append(whole, partial...))}, true
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
	projects = corroborateClaims(spec, w.enrich(ctx, projects, log), log)
	if search && spec.eco == ecoAny && !anyExact(projects) {
		projects = append(projects, w.searchProjects(ctx, spec, projects, log)...)
	}
	return projects
}

// registryProjects looks spec up in the registries it allows and groups
// what they have by repository. Deprecated npm packages are dropped unless
// the spec named npm.
func (w *Web) registryProjects(ctx context.Context, spec libSpec, log *resolveLog) []*project {
	if w.pkgs == nil || spec.eco == ecoGitHub {
		return nil
	}
	actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	var pkgs []pkgInfo
	for _, l := range w.pkgs.lookupAll(actx, spec) {
		label := string(l.eco) + " " + l.name
		switch {
		case errors.Is(l.err, ErrNotFound):
			log.reject("%s: no such package", label)
		case l.err != nil:
			log.failSource(fmt.Errorf("%s: %w", label, l.err), true)
		case l.info.deprecated && l.eco == ecoNPM && spec.eco == ecoAny:
			log.knowName()
			log.reject("%s: deprecated", label)
		default:
			log.knowName()
			pkgs = append(pkgs, l.info)
		}
	}
	return groupPackages(spec, pkgs)
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

// standaloneProject is pkg on its own, its claim to p's repository set
// aside: what GitHub said of the repository (that it does not exist, a
// rate limit) carries over.
func standaloneProject(spec libSpec, pkg pkgInfo, p *project) *project {
	q := &project{key: pkg.spec(), pkgs: []pkgInfo{pkg}, exact: exactName(spec, pkg), repoGone: p.repoGone, notes: slices.Clone(p.notes)}
	if p.info != nil {
		q.notes = append(q.notes, evidence{kind: evNote, detail: fmt.Sprintf("%s names github.com/%s, whose homepage does not name it back, so the claim is not counted", pkgLabel(pkg), p.repo)})
	}
	return q
}

// exactName reports a package whose normalized name is the queried one.
func exactName(spec libSpec, pkg pkgInfo) bool {
	n := normalizeName(pkg.eco, pkg.name)
	return n != "" && n == normalizeName(pkg.eco, spec.name)
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
	if w.repos == nil {
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
		log.failRepo(p.key, fmt.Errorf("github %s: not asked: %w", p.repo, limit))
		return
	}
	actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	info, err := w.repos.repo(actx, p.repo)
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
		log.failRepo(p.key, fmt.Errorf("github %s: %w", p.repo, err))
	case errors.Is(err, ErrNotFound):
		p.repoGone = true
		log.reject("%s: repo %s not found", p.label(), p.repo)
	default:
		p.notes = append(p.notes, evidence{kind: evNote, detail: "github " + p.repo.String() + ": " + err.Error()})
		log.failRepo(p.key, fmt.Errorf("github %s: %w", p.repo, err))
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
		}
	}
	return out
}

// searchProjects asks GitHub for repositories named spec's name (or a
// variant, nameVariants) and makes projects of the most-starred ones not
// already known: forks and archived repositories are skipped.
func (w *Web) searchProjects(ctx context.Context, spec libSpec, existing []*project, log *resolveLog) []*project {
	if w.repos == nil || spec.eco != ecoAny || log.rateLimited() != nil {
		return nil
	}
	actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	hits, err := w.repos.searchRepos(actx, spec.name, webSearchRepos)
	if err != nil {
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
	r.log.failFor(p.key, fmt.Errorf("%s: %w", p.label(), err))
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
	root, err := parseWebRoot(u.String())
	if err != nil {
		return nil
	}
	if s.byKey == nil {
		s.byKey = map[string]*rootCandidate{}
	}
	k := scopeHost(root.u) + root.prefix
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
		listed = listed || pkg.registryDocs != "" || pkg.docsURL != "" || pkg.homepage != ""
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
	return set.sorted()
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
		}
	}
	if pkg.homepage != "" {
		w.homepageCandidates(ctx, p, pkg.homepage, src, evPkgHomepage, set, log)
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
		logFetch(log, p.key, src+": homepage "+u.String(), err)
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
		if sameRoot(root, home) || sameRoot(root, u) {
			set.add(root, false, false, &evidence{kind: kind, detail: src + " homepage"})
		} else {
			set.addNav(root, home, &evidence{kind: evNavChain, detail: src + " homepage " + scopeHost(home) + " links to it"})
		}
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
	prof, err := w.cachedProfile(ctx, root.u)
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
	evs, err := rootEvidence(root, prof, p, c)
	if err != nil {
		return webMatch{}, err
	}
	trust, ok := score(evs)
	if !ok {
		return webMatch{}, rejectf("%s is not verified (trust %.1f)", root, trust)
	}
	return webMatch{
		root: root, title: siteTitle(prof, root), description: prof.description, trust: trust, evidence: evs,
		packages: p.specs(), project: p.key, stars: p.stars(), registry: c.registry, proj: p, prof: prof,
	}, nil
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

// siteRootOf is the docs root a page URL most likely sits under: through a
// docs or version segment (after a leading locale), else the host root.
func siteRootOf(u *url.URL) *url.URL {
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	keep := 0
	for i, seg := range segs {
		switch {
		case docsSegments[strings.ToLower(seg)] || versionLabelRe.MatchString(seg):
			keep = i + 1
		case i == 0 && localeSegRe.MatchString(seg):
			continue
		}
		break
	}
	p := "/"
	if keep > 0 {
		p = "/" + strings.Join(segs[:keep], "/") + "/"
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: p}
}

// searchRootOf is the root a web search hit stands for: a docs. host's
// root, a /docs/ prefix, a GitHub Pages project, else the host root.
func searchRootOf(u *url.URL) *url.URL {
	switch {
	case strings.HasPrefix(strings.ToLower(u.Hostname()), "docs."):
		return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}
	case docsSegment(u.Path) != "":
		return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/" + docsSegment(u.Path) + "/"}
	}
	return hostRootFor(u)
}

// rootEvidence is what ties root to p: the candidate's own evidence, its
// back-links, the project's standing and whether the site names it. A
// root that links only to a same-named repository of another owner, or a
// search hit or guessed root without V4's support, is rejected.
func rootEvidence(root webRoot, prof *siteProfile, p *project, c rootCandidate) ([]evidence, error) {
	evs := slices.Clone(c.evidence)
	links, err := linkEvidence(root, prof, p)
	if err != nil {
		return nil, err
	}
	evs = append(evs, links...)
	if c.offsite && !hasEvidence(links, evLinksRepo) {
		return nil, rejectf("%s is on another domain than the homepage linking to it as its docs, and does not link back to %s", root, p.label())
	}
	if c.v4Only() {
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
// value: "title", "Click Documentation") with a word that is one of p's
// names. Words keep their dots, so "Next.js" does not name next.
func siteNamesEvidence(p *project, pairs ...string) (evidence, bool) {
	if p == nil {
		return evidence{}, false
	}
	names := p.names()
	for i := 0; i+1 < len(pairs); i += 2 {
		for _, tok := range nameTokens(pairs[i+1]) {
			if slices.Contains(names, tok) {
				return evidence{kind: evSiteNames, detail: "the site's " + pairs[i] + " names " + tok}, true
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
	if perr == nil {
		if canon, ok := canonicalRoot(root, prof.served); ok {
			root = canon
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
	return m, nil
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
		evs = append(evs, evidence{kind: evRepoHomepage, detail: "github " + p.repo.String() + " homepage"})
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
	if w.repos == nil {
		return nil, nil
	}
	links := repoLinks(prof)
	for _, k := range links[:min(len(links), webMaxURLRepos)] {
		actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
		info, err := w.repos.repo(actx, k)
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
func (r *resolution) verifyHit(ctx context.Context, targets []*project, c rootCandidate) *webMatch {
	for _, p := range targets {
		m, err := r.w.verifyRoot(ctx, p, c)
		if err == nil {
			return &m
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
	// hit, and then only through its own homepage (relatedEvidence).
	return slices.DeleteFunc(r.w.enrich(ctx, out, r.log), func(p *project) bool {
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
	if w.sites == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	sctx, cancel := context.WithTimeout(httpx.WithoutRedirectCheck(ctx), webSearchTimeout)
	defer cancel()
	results, err := w.sites.Search(sctx, name+" documentation", webSearchHits)
	if err != nil {
		return nil
	}
	set := &candidateSet{}
	for _, res := range results {
		u, err := url.Parse(strings.TrimSpace(res.URL))
		if err != nil || !isPublicWebURL(u) || isNonDocsHost(u.Hostname()) || isRegistryHost(u) {
			continue
		}
		set.add(searchRootOf(u), false, true, nil)
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
	groups = r.pruneDominated(groups)
	var out []webMatch
	for _, g := range groups {
		out = append(out, g.matches...)
	}
	return out
}

// pickRoots keeps a project's best site and its best registry docs, the
// site first; registry docs lead only when there is no site.
func pickRoots(ms []webMatch) []webMatch {
	slices.SortStableFunc(ms, func(a, b webMatch) int { return cmp.Compare(b.trust, a.trust) })
	site := slices.IndexFunc(ms, func(m webMatch) bool { return !m.registry })
	reg := slices.IndexFunc(ms, func(m webMatch) bool { return m.registry })
	var out []webMatch
	for _, i := range []int{site, reg} {
		if i >= 0 {
			out = append(out, ms[i])
		}
	}
	out[0].primary = true
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
//     PyPI's Clap on pythonhosted.org beside clap-rs/clap).
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
		case m.stars >= 0:
			if t.stars < dominanceRatio*m.stars {
				return 0, false
			}
		case !m.indexOnly:
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
	llms := llmsTxt{}
	if !m.registry {
		llms = r.w.findLLMSTxt(ctx, m.root, &[]string{})
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

// pickVersion moves m to the version the spec asks for (plan §6).
func (r *resolution) pickVersion(ctx context.Context, m *webMatch, vs versionSet) error {
	want := r.spec.version
	if m.registry {
		return r.registryVersion(ctx, m, want)
	}
	if len(vs.versions) == 0 {
		return r.unversioned(m, want)
	}
	sv, ok := matchVersion(want, vs.versions)
	switch {
	case ok:
		return r.adoptVersion(ctx, m, sv, want, vs.labels())
	case vs.versions[0].label == labelCurrent && r.assumeLatest(m, want):
		return nil // the current docs carry no release of their own
	}
	return r.versionNotFound(want, vs.labels(), "")
}

// registryVersion moves registry docs to the requested version.
func (r *resolution) registryVersion(ctx context.Context, m *webMatch, want string) error {
	i := slices.IndexFunc(m.proj.pkgs, func(p pkgInfo) bool { return p.registryDocs != "" })
	if i < 0 {
		return r.versionNotFound(want, nil, "")
	}
	pkg := m.proj.pkgs[i]
	var goVersions []string
	if pkg.eco == ecoGo && r.w.pkgs != nil {
		actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
		goVersions, _ = r.w.pkgs.goVersions(actx, pkg.name)
		cancel()
	}
	sv, ok := registryVersionRoot(pkg, want, goVersions)
	if !ok {
		return r.versionNotFound(want, goVersions, "")
	}
	return r.adoptVersion(ctx, m, sv, want, nil)
}

// adoptVersion moves m to sv's root after the post-check: a version root
// that carries a label must still carry one want matches once served.
func (r *resolution) adoptVersion(ctx context.Context, m *webMatch, sv siteVersion, want string, labels []string) error {
	root, err := parseWebRoot(sv.root)
	if err != nil {
		return r.versionNotFound(want, labels, "")
	}
	prof := m.prof
	if linkKey(root.u) != linkKey(m.root.u) {
		if prof, err = r.w.cachedProfile(ctx, root.u); err != nil {
			if isGone(err) {
				return r.versionNotFound(want, labels, fmt.Sprintf("%s: %v", root, err))
			}
			return fmt.Errorf("web: fetching %s failed: %w", root, err)
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

// unversioned accepts want on a site that publishes no versions only when
// it shares a major version with the package's latest release.
func (r *resolution) unversioned(m *webMatch, want string) error {
	if r.assumeLatest(m, want) {
		return nil
	}
	return fmt.Errorf("web: %s publishes no version list, so %s@%s cannot be checked; pass the versioned docs URL: %w", m.root, r.spec.name, want, ErrNotFound)
}

// assumeLatest takes m's current docs for want when want shares a major
// version with the package's latest release, noting the assumption.
func (r *resolution) assumeLatest(m *webMatch, want string) bool {
	latest := ""
	if m.proj != nil {
		for _, pkg := range m.proj.pkgs {
			if latest = pkg.version; latest != "" {
				break
			}
		}
	}
	if latest == "" || !sameMajor(want, latest) {
		return false
	}
	m.version = latest
	m.evidence = append(m.evidence, evidence{kind: evNote, detail: fmt.Sprintf("the docs name no release; taken as %s, the latest, which shares %s's major version", latest, want)})
	return true
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
