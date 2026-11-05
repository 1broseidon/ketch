package docs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/ketch/search"
)

// Keyless resolution (design/docs-keyless.md, Pipeline 1-4): resolution
// without a GitHub token. GitHub is never asked; the web search is a
// candidate source from the start; a root needs two independent edges to
// its project (keylessEvidence); and same-name projects are told apart by
// registry popularity and the ecosystem hint, under the same dominance
// rule as stars.

// Caps on what search hits' links make projects of.
const (
	webMaxHitPackages     = 3 // packages looked up
	webMaxKeylessHitRepos = 2 // repositories whose packages are looked up
)

// runKeyless is run without GitHub. The search starts before the
// registries are asked, and its hits are verified, against every project
// the registries name and those the hits' own links name, while the
// registries' roots are: a hit is a candidate, never evidence.
func (r *resolution) runKeyless(ctx context.Context) []webMatch {
	var hits chan []search.Result
	if r.searches() && r.w.sites != nil {
		hits = make(chan []search.Result, 1)
		go func() {
			results, err := r.w.searchHits(ctx, r.searchQuery(ctx))
			if err != nil {
				r.log.reject("web search: %v", err)
			}
			hits <- results
		}()
	}
	var goNames chan []*project
	if r.searches() && r.spec.eco == ecoAny {
		// Go has no registry of names: pkg.go.dev's search stands in, and
		// a cold search can take seconds, so it runs beside everything
		// else and its modules are verified when they arrive.
		goNames = make(chan []*project, 1)
		go func() { goNames <- r.w.goNameProjects(ctx, r.spec, r.log) }()
	}
	projects := r.w.gather(ctx, r.spec, r.log, r.searches())
	r.log.consider(projects...)
	var hitMatches, goMatches []webMatch
	var hitProjects, goProjects []*project
	var wg sync.WaitGroup
	if hits != nil {
		wg.Go(func() { hitMatches, hitProjects = r.keylessHits(ctx, projects, <-hits) })
	}
	if goNames != nil {
		wg.Go(func() {
			goProjects = <-goNames
			sub := &resolution{w: r.w, spec: r.spec, log: r.log, keyless: true}
			goMatches = sub.verifyAll(ctx, goProjects)
		})
	}
	matches := r.verifyAll(ctx, projects)
	wg.Wait()
	hitMatches, hitProjects = withoutGoTwins(hitMatches, hitProjects, goProjects)
	r.log.consider(goProjects...)
	r.log.consider(hitProjects...)
	matches = append(append(matches, goMatches...), hitMatches...)
	projects = append(append(projects, goProjects...), hitProjects...)
	r.readTieBreak(ctx, matches)
	return r.assemble(projects, matches)
}

// withoutGoTwins drops the projects a search hit made of a Go module that
// pkg.go.dev's search gave too (they were made concurrently), and their
// matches: one module is one project, with the count its search gave.
func withoutGoTwins(matches []webMatch, hitProjects, goProjects []*project) ([]webMatch, []*project) {
	goSpecs := map[string]bool{}
	for _, p := range goProjects {
		for _, pkg := range p.pkgs {
			goSpecs[pkg.spec()] = true
		}
	}
	twin := map[string]bool{}
	var kept []*project
	for _, p := range hitProjects {
		if len(p.pkgs) > 0 && goSpecs[p.pkgs[0].spec()] {
			twin[p.key] = true
			continue
		}
		kept = append(kept, p)
	}
	matches = slices.DeleteFunc(matches, func(m webMatch) bool { return m.proj != nil && twin[m.proj.key] && m.proj.fromHits })
	return matches, kept
}

// searchQuery is what the web search is asked: "<name> documentation",
// with the ecosystem the spec or the query's own words name ("npm",
// "python", "rust", "golang"). The working directory's ecosystem is not
// added: it is a guess about the caller, and steering the search with it
// fills the candidates with that ecosystem's namesakes (go-react for react
// from a Go project).
func (r *resolution) searchQuery(ctx context.Context) string {
	q := searchName(r.spec) + " documentation"
	words, _ := ctx.Value(ecoHintKey{}).([]ecosystem)
	if w := ecoSearchWord(r.spec.eco); w != "" {
		q += " " + w
	} else if len(words) == 1 {
		q += " " + ecoSearchWord(words[0])
	}
	return q
}

// ecoSearchWord is the word that steers a web search to an ecosystem.
func ecoSearchWord(eco ecosystem) string {
	switch eco {
	case ecoNPM:
		return "npm"
	case ecoPyPI:
		return "python"
	case ecoCrates:
		return "rust"
	case ecoGo:
		return "golang"
	}
	return ""
}

// keylessHits verifies the search hits' roots against the projects, and
// against projects made from the packages and repositories the hits name
// (projectsFromHitsKeyless). Each root counts for the first project it
// verifies for; most verify for none.
func (r *resolution) keylessHits(ctx context.Context, projects []*project, results []search.Result) ([]webMatch, []*project) {
	cands := hitRoots(results, searchName(r.spec))
	extra := r.projectsFromHitsKeyless(ctx, results, cands, projects)
	targets := append(append([]*project{}, projects...), extra...)
	if len(targets) > webMaxProjects+len(extra) {
		targets = append(targets[:webMaxProjects:webMaxProjects], extra...)
	}
	found := make([]*webMatch, len(cands))
	var own []webMatch
	var wg sync.WaitGroup
	if len(extra) > 0 {
		// The projects the hits named are projects like any other: their
		// own roots (registry docs, homepages) are verified too, from a
		// budget of their own, as the registries' are in verifyAll.
		sub := &resolution{w: r.w, spec: r.spec, log: r.log, keyless: true}
		wg.Go(func() { own = sub.verifyAll(ctx, extra) })
	}
	for i, c := range cands {
		wg.Go(func() { found[i] = r.verifyHit(ctx, targets, c) })
	}
	wg.Wait()
	out := own
	for _, m := range found {
		if m != nil {
			out = append(out, *m)
		}
	}
	return out, extra
}

// projectsFromHitsKeyless makes projects of what the search hits name as
// the library: a registry package page or registry docs (npmjs.com's
// solid-js, pkg.go.dev's github.com/gin-gonic/gin) that a hit is or that a
// hit's page links to, and a GitHub repository named like the library that
// a hit is or links to, taken as its Go module or its same-named packages
// that name it back (addRepoPackages). Only names like the library's
// (nameSet) count, packages the registries already gave are skipped, and
// a repository with no package is dropped: nothing would tie it to a
// registry. These projects verify a root the same way as any other, by two
// edges, one of which must come from their own metadata.
func (r *resolution) projectsFromHitsKeyless(ctx context.Context, results []search.Result, cands []rootCandidate, existing []*project) []*project {
	if r.w.pkgs == nil {
		return nil
	}
	names := newHitNames(searchName(r.spec), existing)
	for _, res := range results {
		if u, err := url.Parse(strings.TrimSpace(res.URL)); err == nil {
			names.note(u)
		}
	}
	for _, c := range cands {
		if prof, err := r.w.cachedProfile(ctx, c.url); err == nil {
			for _, l := range prof.links {
				names.note(l.url)
			}
		}
	}
	found := make([]*project, len(names.refs)+len(names.repos))
	var wg sync.WaitGroup
	for i, ref := range names.refs {
		wg.Go(func() { found[i] = r.hitPackageProject(ctx, ref) })
	}
	for i, k := range names.repos {
		wg.Go(func() {
			p := &project{key: k.spec(), repo: k, fromSearch: true, fromHits: true}
			r.w.addRepoPackages(ctx, r.spec, p, r.log)
			found[len(names.refs)+i] = p
		})
	}
	wg.Wait()
	var out []*project
	seen := map[string]bool{}
	for _, p := range found {
		if p == nil {
			continue
		}
		// A repository's packages the registries already gave are theirs.
		p.pkgs = slices.DeleteFunc(p.pkgs, func(pkg pkgInfo) bool { return names.known[pkg.spec()] || seen[pkg.spec()] })
		for _, pkg := range p.pkgs {
			seen[pkg.spec()] = true
		}
		if len(p.pkgs) > 0 {
			out = append(out, p)
		}
	}
	return claimedProjects(r.spec, out)
}

// hitNames collects what search hits name like the library: registry
// packages and GitHub repositories not already known.
type hitNames struct {
	name      string
	variants  map[string]bool
	known     map[string]bool // package specs the registries gave
	knownRepo map[repoKey]bool
	noted     map[string]bool
	refs      []pkgRef
	repos     []repoKey
}

func newHitNames(name string, existing []*project) *hitNames {
	h := &hitNames{name: name, variants: nameSet(name), known: map[string]bool{}, knownRepo: map[repoKey]bool{}, noted: map[string]bool{}}
	for _, p := range existing {
		for _, pkg := range p.pkgs {
			h.known[pkg.spec()] = true
		}
		if !p.repo.isZero() {
			h.knownRepo[p.repo] = true
		}
	}
	return h
}

// note takes u when it is a registry page of a package named like the
// library, or a GitHub repository named like it. A repository of an
// organization named like the library (go-chi's docs) stands for that
// organization's repository named like the library, if a registry has it.
func (h *hitNames) note(u *url.URL) {
	if ref := registryPackageOf(u); ref.eco != "" {
		short := ref.name
		if ref.eco == ecoGo {
			short = moduleName(ref.name)
		}
		short = strings.ToLower(short[strings.LastIndexByte(short, '/')+1:])
		spec := string(ref.eco) + ":" + ref.name
		if h.variants[short] && !h.known[spec] && !h.noted[spec] && len(h.refs) < webMaxHitPackages {
			h.noted[spec] = true
			h.refs = append(h.refs, ref)
		}
		return
	}
	k, ok := githubRepoOf(u)
	if !ok {
		return
	}
	if !h.variants[k.name] && h.variants[k.owner] {
		k = repoKey{owner: k.owner, name: strings.ToLower(h.name)}
	}
	if h.variants[k.name] && !h.knownRepo[k] && len(h.repos) < webMaxKeylessHitRepos {
		h.knownRepo[k] = true
		h.repos = append(h.repos, k)
	}
}

// hitPackageProject looks up a package a search hit named; nil when the
// registry does not have it or did not answer (the hit only suggested it,
// so its absence is no failed source).
func (r *resolution) hitPackageProject(ctx context.Context, ref pkgRef) *project {
	actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	pkg, err := r.w.pkgs.lookup(actx, ref.eco, ref.name)
	if err != nil && ref.eco == ecoGo {
		// A pkg.go.dev link names a package; its module may be a prefix.
		if segs := strings.Split(ref.name, "/"); len(segs) > 3 && strings.EqualFold(segs[0], "github.com") {
			pkg, err = r.w.pkgs.lookup(actx, ref.eco, strings.Join(segs[:3], "/"))
		}
	}
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			r.log.reject("%s %s, named by a search hit: %v", ref.eco, ref.name, err)
		}
		return nil
	}
	return &project{key: pkg.spec(), pkgs: []pkgInfo{pkg}, repo: pkg.repo, exact: exactName(r.spec, pkg), fromSearch: true, fromHits: true}
}

// readTieBreak reads registry popularity for the projects a single-library
// pick may have to decide between: every project that verified a root,
// every one whose site did not answer (a rival answerPastFailures weighs),
// and every package of exactly the queried name, verified or not
// (keylessRivals), once there are two or more. One project alone needs no
// count.
func (r *resolution) readTieBreak(ctx context.Context, matches []webMatch) {
	seen := map[string]bool{}
	var projects []*project
	add := func(p *project) {
		if p != nil && !seen[p.key] {
			seen[p.key] = true
			projects = append(projects, p)
		}
	}
	for _, m := range matches {
		add(m.proj)
	}
	_, partial := r.log.missedBeside(seen, true)
	for _, m := range partial {
		if m.proj != nil && (m.proj.exact || !m.proj.fromHits) {
			add(m.proj) // a hit's project under another name is no rival (rivals)
		}
	}
	weighRivalOnly := len(indexOnlyProjects(matches)) > 0
	for _, p := range r.log.considered() {
		if tieBreakRival(p, weighRivalOnly) {
			add(p)
		}
	}
	if len(projects) < 2 {
		return
	}
	r.w.readPopularity(ctx, projects)
	for _, p := range projects {
		if p.popErr != nil {
			r.log.failFor(p, p.popErr)
		}
	}
}

// tieBreakRival reports a considered project readTieBreak weighs: a
// package of exactly the name; one weighed as a rival only (deprecated)
// only when some pick's docs only a package index hosts (rivalOf).
func tieBreakRival(p *project, rivalOnlyToo bool) bool {
	return p.exact && len(p.pkgs) > 0 && (!p.rivalOnly || rivalOnlyToo)
}

// keylessRivals checks a keyless pick against the packages of exactly the
// queried name that verified nothing: dominance weighs only projects that
// verified, so without this a squatter's registry docs would answer for a
// name whose real, far more popular package has a site that failed the
// edge rule (docs.rs's numpy for PyPI's numpy), or has no docs site at all
// (docs.rs's commander for npm's commander, whose docs are its README).
//
//   - A package whose metadata put forward a docs site of its own that did
//     not verify blocks the pick unless the pick has dominanceRatio times
//     its weekly downloads.
//   - A package that put forward no docs, or only docs a package index
//     hosts (docs.rs, pkg.go.dev), shows that it was published, not that
//     the name means it: it blocks the pick only when it is at least as
//     popular (decision-15). npm's clap, with a GitHub page and a few
//     downloads, does not block crates' clap; npm's commander, with
//     millions, blocks crates' commander.
//
// A blocked pick is an ambiguity listing it and the package specs to pass
// instead. A count that could not be read is a keyless source that did
// not answer: an upstream error naming the pick, except a weak rival's
// (the second kind) beside a pick with its own docs site, which only an
// already popular rival could outweigh. Without a popularity source (a
// backend built for tests) there is nothing to weigh.
//
// A package outside the ecosystem the query or working directory names
// (hint) is no rival to a pick inside it, as applyHint has it.
func (w *Web) keylessRivals(spec libSpec, m webMatch, matches []webMatch, log *resolveLog, hint []ecosystem) error {
	if w.popularity == nil || m.proj == nil {
		return nil
	}
	matched := map[string]bool{}
	for _, x := range matches {
		matched[x.project] = true
	}
	var stronger []*project
	var unread []error
	seen := map[string]bool{}
	for _, p := range log.considered() {
		if seen[p.key] || !rivalOf(p, m, matched, hint) {
			continue
		}
		seen[p.key] = true
		switch blocks, err := rivalBlocks(p, m, !log.hasOfferedSite(p.key)); {
		case err != nil:
			unread = append(unread, err)
		case blocks:
			stronger = append(stronger, p)
		}
	}
	if len(unread) > 0 {
		slices.SortFunc(unread, func(a, b error) int { return cmp.Compare(a.Error(), b.Error()) })
		err := log.upstreamErr(fmt.Sprintf("web: %q resolved to %s, but %s, so a more popular package of that name may be the library meant; retry, or pass the library you mean as a docs URL or package spec",
			spec.raw, m.root, joinErrors(unread)), false)
		err.picked = true
		return err
	}
	if len(stronger) == 0 {
		return nil
	}
	// The projects were considered as concurrent sources answered: list
	// them in a fixed order, most popular first.
	slices.SortFunc(stronger, func(a, b *project) int { return cmp.Or(cmp.Compare(b.pop, a.pop), cmp.Compare(a.key, b.key)) })
	amb := &AmbiguousLibraryError{Library: spec.raw, Matches: []LibraryMatch{m.libraryMatch()}}
	for _, p := range stronger {
		amb.Matches = append(amb.Matches, LibraryMatch{
			ID: p.pkgs[0].spec(), Title: "docs not verified, " + popularityText(p.pop), Packages: p.specs(),
		})
	}
	return amb
}

// rivalBlocks reports rival p keeping the pick m from answering, or why
// the two could not be weighed. weak marks a rival that put forward no
// docs site of its own: it blocks only when at least as popular, and its
// count unread (or a modest count beside the pick's unread one) does not
// count against a pick with a site of its own.
func rivalBlocks(p *project, m webMatch, weak bool) (bool, error) {
	err := outweighs(p, m)
	switch {
	case err != nil && weak && !m.indexOnly && (p.popErr != nil || p.pop < dominanceStars):
		return false, nil
	case err != nil:
		return false, err
	case !p.popKnown || p.pop <= 0:
		return false, nil
	case weak:
		return int64(m.stars) <= p.pop, nil
	}
	return int64(m.stars) < dominanceRatio*p.pop, nil
}

// outweighs is why rival p and the pick m cannot be weighed: a count that
// could not be read (the rival's, or the pick's beside a rival with
// downloads); nil when both are known or the rival has none.
func outweighs(p *project, m webMatch) error {
	switch {
	case p.popErr != nil:
		return p.popErr
	case !p.popKnown || p.pop == 0:
		return nil
	case m.proj.popErr != nil:
		return m.proj.popErr
	case !m.proj.popKnown:
		return fmt.Errorf("%s: popularity not read", m.proj.label())
	}
	return nil
}

// rivalOf reports p as a package keylessRivals weighs against the pick m:
// one of exactly the queried name, not the pick's, that verified nothing
// (matched), and that is not outside the ecosystem a hint names for a
// pick inside it, unless it outweighs the pick by the dominance margin (a
// hint breaks ties, it does not overrule).
func rivalOf(p *project, m webMatch, matched map[string]bool, hint []ecosystem) bool {
	switch {
	case !p.exact || len(p.pkgs) == 0 || matched[p.key] || p == m.proj || ownedBy(p, m.proj):
		return false
	case p.rivalOnly && !m.indexOnly:
		return false // a deprecated namesake does not stand in a docs site's way (registryProjects)
	case !p.repo.isZero() && m.proj.isRepo(p.repo):
		return false // another registry's package of the pick's own repository
	case len(hint) > 0 && hinted(m.proj, hint) && !hinted(p, hint) && (!p.popKnown || !outweighsBy(p.pop, int64(m.stars))):
		return false // the hint settles a contest the rival does not dominate
	}
	return true
}

// applyHint narrows a single-library contest to the projects in the
// ecosystems the query or the working directory names (resolution.hint),
// when at least one of them verified: a Python project asking about a name
// two comparable libraries share means PyPI's. A hint is a tie-break, never
// a veto of the popularity evidence: when a project outside the hint
// outweighs every hinted one by the dominance margin (express beside a Go
// module named express, from a Go working directory), or a weight is
// unknown so nothing can be compared, the contest is left whole for the
// dominance rule to settle or call ambiguous. Projects whose ecosystem is
// unknown (no package) stay.
func (r *resolution) applyHint(groups []matchGroup) []matchGroup {
	if len(r.hint) == 0 || len(groups) < 2 {
		return groups
	}
	in, top := 0, -1
	for _, g := range groups {
		if hinted(g.proj, r.hint) {
			in++
			top = max(top, g.best().stars)
		}
	}
	if in == 0 || in == len(groups) {
		return groups
	}
	var out []matchGroup
	for _, g := range groups {
		if hinted(g.proj, r.hint) || len(projectEcosystems(g.proj)) == 0 {
			out = append(out, g)
			continue
		}
		if w := g.best().stars; w < 0 || top < 0 || outweighsBy(int64(w), int64(top)) {
			r.log.reject("ecosystem hint (%s) not applied: %s (%s) cannot be set aside for the hinted projects (%s at most)",
				joinEcos(r.hint), g.best().project, weightText(g.proj, w), weightText(nil, top))
			return groups
		}
		r.log.reject("%s: outside the ecosystem the query or working directory names (%s)", g.best().project, joinEcos(r.hint))
	}
	return out
}

// outweighsBy reports a weight that dominates another by the dominance
// rule's margin: at least dominanceStars and dominanceRatio times it.
func outweighsBy(a, b int64) bool {
	return a >= dominanceStars && a >= dominanceRatio*max(b, 0)
}

// joinEcos lists ecosystems for a message.
func joinEcos(ecos []ecosystem) string {
	var s []string
	for _, e := range ecos {
		s = append(s, string(e))
	}
	return strings.Join(s, ", ")
}

// llmsCall is one llms.txt read shared across a resolution.
type llmsCall struct {
	done chan struct{}
	l    llmsTxt
}

// cachedLLMS is findLLMSTxt, read once per root per resolution: keyless
// verification may read a root's manifest for several projects, and
// describe reads it again for the match.
func (w *Web) cachedLLMS(ctx context.Context, root webRoot) llmsTxt {
	memo, _ := ctx.Value(profileMemoKey{}).(*profileMemo)
	if memo == nil {
		return w.findLLMSTxt(ctx, root, &[]string{})
	}
	key := "llms " + root.String()
	memo.mu.Lock()
	if memo.llms == nil {
		memo.llms = map[string]*llmsCall{}
	}
	if c, ok := memo.llms[key]; ok {
		memo.mu.Unlock()
		select {
		case <-c.done:
			return c.l
		case <-ctx.Done():
			return llmsTxt{}
		}
	}
	c := &llmsCall{done: make(chan struct{})}
	memo.llms[key] = c
	memo.mu.Unlock()
	c.l = w.findLLMSTxt(ctx, root, &[]string{})
	close(c.done)
	return c.l
}

// webMaxGoNames caps the Go modules a bare name is taken as;
// webGoSearchTimeout bounds pkg.go.dev's search, which can take seconds
// for a name it has not been asked lately.
//
// goNameMinImporters is how many importing packages a module needs to be
// taken as a bare name's Go library. pkg.go.dev's search is a search, not
// a namespace: any module may end in tailwind or react (two and a hundred
// importers), while the Go libraries people ask for by name are imported
// by tens of thousands (gin, cobra, chi, echo).
const (
	webMaxGoNames      = 2
	webGoSearchTimeout = 15 * time.Second
	goNameMinImporters = 1000
)

// goModuleSearcher is the optional pkgSource method goNameProjects asks.
type goModuleSearcher interface {
	searchGoPage(ctx context.Context, name string, n int) ([]goModule, error)
}

// goNameProjects are the Go modules a bare name names, for a keyless
// resolution: pkg.go.dev's search page, keeping the most imported modules whose
// last path element (a /vN suffix aside) is the name, and only modules
// widely imported (goNameMinImporters). GitHub's repository search did this with a token; Go
// has no registry of names, so without one cobra would never reach
// spf13/cobra. Their popularity comes with them. pkg.go.dev's search
// failing is a registry that could not be asked about the name.
func (w *Web) goNameProjects(ctx context.Context, spec libSpec, log *resolveLog) []*project {
	gs, ok := w.pkgs.(goModuleSearcher)
	if !ok || !goNameRe.MatchString(strings.ToLower(spec.name)) {
		return nil
	}
	actx, cancel := context.WithTimeout(ctx, webGoSearchTimeout)
	defer cancel()
	mods, err := gs.searchGoPage(actx, spec.name, 0)
	if err != nil {
		log.failSource(fmt.Errorf("pkg.go.dev search %q: %w", spec.name, err), true)
		return nil
	}
	var picked []goModule
	for _, m := range mods {
		if m.importers >= goNameMinImporters && strings.EqualFold(moduleName(m.path), spec.name) && moduleRootPath(m.path) && len(picked) < webMaxGoNames {
			picked = append(picked, m)
		}
	}
	found := make([]*project, len(picked))
	var wg sync.WaitGroup
	for i, m := range picked {
		wg.Go(func() {
			pkg, err := w.pkgs.lookup(actx, ecoGo, m.path)
			switch {
			case errors.Is(err, ErrNotFound):
				log.reject("go %s: %v", m.path, err)
				return
			case err != nil:
				log.failSource(fmt.Errorf("go %s: %w", m.path, err), true)
				return
			}
			log.knowName()
			key := pkg.spec()
			if !pkg.repo.isZero() {
				key = pkg.repo.spec()
			}
			found[i] = &project{key: key, pkgs: []pkgInfo{pkg}, repo: pkg.repo, exact: true, pop: m.importers * goImporterWeight, popKnown: true}
		})
	}
	wg.Wait()
	var out []*project
	for _, p := range found {
		if p != nil {
			out = append(out, p)
		}
	}
	return out
}

// moduleRootPath reports a path that can be a module's root rather than a
// package inside one: on a code forge, owner/repo (and a /vN suffix).
func moduleRootPath(path string) bool {
	segs := strings.Split(path, "/")
	switch segs[0] {
	case "github.com", "gitlab.com", "bitbucket.org", "codeberg.org":
		n := len(segs)
		if n == 4 && len(segs[3]) > 1 && segs[3][0] == 'v' && isBareInt(segs[3][1:]) {
			n = 3
		}
		return n == 3
	}
	return true
}

// goNameRe is a name that can be a Go module path's last element.
var goNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// strictMissError is a bare query's reading (react-router in "react
// router loaders") that verified no docs although a registry has a
// package of exactly that name: the query may well be about it, so
// another reading (react) answers only if it outweighs that package
// (pastStrictMisses). Its popularity is read only then (weigh). Keyless
// resolutions only.
type strictMissError struct {
	err   error
	exact []*project // the packages of exactly the reading's name

	weighed bool
	spec    string  // the most popular package, once weighed
	repo    repoKey // the repository it names; zero for none
	pop     int64   // its weekly downloads; -1 when they could not be read
}

func (e *strictMissError) Error() string { return e.err.Error() }
func (e *strictMissError) Unwrap() error { return e.err }

// strictMiss is err for a keyless reading that failed as not found, as a
// *strictMissError when a registry has a package of exactly its name,
// else err.
func (w *Web) strictMiss(err error, log *resolveLog) error {
	if w.popularity == nil || !errors.Is(err, ErrNotFound) {
		return err
	}
	var exact []*project
	for _, p := range log.considered() {
		if p.exact && len(p.pkgs) > 0 {
			exact = append(exact, p)
		}
	}
	if len(exact) == 0 {
		return err
	}
	return &strictMissError{err: err, exact: exact}
}

// weigh reads the miss's packages' popularity, once: the most popular
// package stands for the miss, and one whose count could not be read
// makes the miss's unknown.
func (e *strictMissError) weigh(ctx context.Context, w *Web) {
	if e.weighed {
		return
	}
	e.weighed = true
	w.readPopularity(ctx, e.exact)
	for _, p := range e.exact {
		switch {
		case p.popErr != nil:
			e.spec, e.repo, e.pop = p.pkgs[0].spec(), p.repo, -1
			return
		case p.pop > e.pop:
			e.spec, e.repo, e.pop = p.pkgs[0].spec(), p.repo, p.pop
		}
	}
}

// readingMiss is a reading's miss, and whether its name is a common word.
type readingMiss struct {
	miss   *strictMissError
	common bool
}

// pastStrictMisses checks a bare query's pick against the readings that
// named a real package but verified no docs: the pick stands only when it
// has dominanceRatio times the downloads of each (react's 245M a week
// against react-router's 20M does not), else the query is ambiguous
// between them. A package from the pick's own repository (clap_derive for
// clap) is part of it. A miss whose downloads could not be read is a
// source that did not answer, except a common word's, which is no
// evidence that the query is about it.
func (w *Web) pastStrictMisses(ctx context.Context, query string, m webMatch, missed []readingMiss) error {
	if len(missed) == 0 {
		return nil
	}
	weight := w.pickWeight(ctx, m)
	amb := &AmbiguousLibraryError{Library: query, Matches: []LibraryMatch{m.libraryMatch()}}
	for _, rm := range missed {
		miss := rm.miss
		miss.weigh(ctx, w)
		switch {
		case miss.pop == 0, miss.pop < 0 && rm.common:
		case m.proj != nil && !miss.repo.isZero() && m.proj.isRepo(miss.repo):
			// The same repository's other package (clap_derive beside
			// clap): one project, not a rival.
		case miss.pop < 0:
			return fmt.Errorf("web: reading %q as %s, but the downloads of %s, a package of the query's own words, could not be read; retry, or pass the library you mean", query, m.root, miss.spec)
		case weight < 0 && miss.pop >= dominanceStars, weight >= 0 && weight < dominanceRatio*miss.pop:
			amb.Matches = append(amb.Matches, LibraryMatch{ID: miss.spec, Title: "docs not verified, " + popularityText(miss.pop), Packages: []string{miss.spec}})
		}
	}
	if len(amb.Matches) > 1 {
		return amb
	}
	return nil
}

// pickWeight is the pick's weight, its popularity read first when a
// keyless resolution has not read it; -1 when unknown.
func (w *Web) pickWeight(ctx context.Context, m webMatch) int64 {
	if m.proj != nil && !m.proj.popKnown && m.proj.info == nil {
		w.readPopularity(ctx, []*project{m.proj})
		return int64(m.proj.stars())
	}
	return int64(m.stars)
}
