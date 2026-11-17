package docs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/decide"
)

// A linked docs site (a decided answer only): when no page read is the
// makers' documentation (notMakers), the pages read often link to it. The
// site most of them link to is read as --library reads a docs URL, and is
// the answer when the provider labels a page of it official docs.
//
//	the pages read, their links to other sites (links that are never to a
//	library's docs dropped: neverLinked), each target host counted by the
//	distinct source sites that link to it (on a host many owners publish
//	on, each owner's repository: linkTarget)
//	        │
//	a host followMinSources or more sites link to: the most sites, then a
//	docs-looking host or path, then the most links; ONE
//	        │
//	read as --library <root> reads it (its search, robots.txt, page
//	limits); the pages printed labelled by the page pass, those labelled
//	official docs asked the passage pass
//	        │
//	official docs on the question (context high: a site reached by links
//	clears a higher bar than a search result): those pages filled as a
//	decided answer, read lines "linked from N of the pages read", its other
//	documentation pages read listed as other results, the search's pages
//	passed over; otherwise the answer as it was,
//	its note saying what was tried and why it was not used
//
// No candidate costs nothing: the links are in the pages already read.

// followMinSources is how many distinct source sites must link to a host
// for it to be read.
const followMinSources = 2

// linkedSite is a host the pages read link to: the root to read, how many
// distinct source sites (registrable domains) link to it, the links in
// all, and whether it looks like docs.
type linkedSite struct {
	host    string
	root    webRoot
	sources int
	links   int
	docs    bool
}

// pageLinks are the absolute links of a page's Markdown (discovery's
// mdLinkRe), images left out, without fragments.
func pageLinks(md string) []*url.URL {
	var out []*url.URL
	for _, m := range mdLinkRe.FindAllStringSubmatchIndex(md, -1) {
		if m[0] > 0 && md[m[0]-1] == '!' {
			continue
		}
		u, err := url.Parse(md[m[4]:m[5]])
		if err != nil || !u.IsAbs() {
			continue
		}
		u.Fragment, u.RawFragment = "", ""
		out = append(out, u)
	}
	return out
}

// neverLinkedHosts are hosts (with their subdomains) a link to which is
// never to a library's docs, beside those notDocsHit and notDocument pass
// over (forges, registries, social, video, Q&A, blog platforms, CDNs of
// packages): encyclopaedias and paper archives, image and asset hosts,
// link shorteners.
var neverLinkedHosts = []string{
	"wikipedia.org", "wikimedia.org", "wiktionary.org", "arxiv.org",
	"imgur.com", "gravatar.com", "shields.io", "cloudfront.net", "googleusercontent.com", "gstatic.com",
	"bit.ly", "t.co", "tinyurl.com", "goo.gl", "ow.ly", "buff.ly", "lnkd.in",
}

// assetHostLabels are first host labels of image, asset and CDN hosts.
var assetHostLabels = map[string]bool{"cdn": true, "static": true, "assets": true, "img": true, "images": true, "media": true}

// neverLinked reports a link that can never lead to a library's docs: no
// well-formed public page, a host or page no search result would be read
// from as docs (notDocsHit, notDocument: forges, social and video sites,
// threads, blog hosts and posts, assets), or one of neverLinkedHosts or
// assetHostLabels.
func neverLinked(u *url.URL) bool {
	if !wellFormedHost(u) || notDocsHit(u) || notDocument(u) {
		return true
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	first, _, _ := strings.Cut(strings.TrimPrefix(host, "www."), ".")
	return onDomains(host, neverLinkedHosts) || assetHostLabels[first]
}

// tenantHosts are hosts where many unrelated owners publish, as on a code
// forge (which neverLinked drops): a link there counts toward its owner's
// repository, never the host; the host's own /docs/ is a docs directory.
var tenantHosts = []string{"huggingface.co", "hf.co"}

// tenantPrefixes are the path segments before owner/repo on a tenant host.
var tenantPrefixes = map[string]bool{"spaces": true, "datasets": true, "collections": true}

// linkTarget is what a link counts toward: its host; on a tenant host, the
// host's /docs/ or an owner's repository (/owner/repo, /spaces/owner/repo),
// root that repository. "" for a tenant link never followed: an API or
// paper address, a file (blob, raw, resolve), a page above a repository.
func linkTarget(u *url.URL) (key, root string) {
	host := scopeHost(u)
	if !onDomains(host, tenantHosts) {
		return host, ""
	}
	segs := pathSegments(u.Path)
	if len(segs) > 0 && strings.EqualFold(segs[0], "docs") {
		return host + "/docs", ""
	}
	n := 2
	if len(segs) > 0 && tenantPrefixes[segs[0]] {
		n = 3
	}
	if len(segs) < n || segs[0] == "api" || segs[0] == "papers" {
		return "", ""
	}
	for _, seg := range segs {
		if forgeFileSegments[seg] || seg == "resolve" {
			return "", ""
		}
	}
	repo := segmentsPath(segs[:n])
	return host + strings.TrimSuffix(repo, "/"), u.Scheme + "://" + hostKey(u) + repo
}

// linkedCandidate is the site the pages read link to that a decided
// answer reads: of the hosts (on a tenant host, the repositories:
// linkTarget) followMinSources or more source sites link to (never a
// source page's own host), the one the most sites link to,
// then a docs-looking one (a docs or developer(s) host, or a link under
// /docs), then the one with the most links. false for none.
func linkedCandidate(pages []*dPage) (linkedSite, bool) {
	own := map[string]bool{}
	for _, p := range pages {
		if p.page != nil {
			own[scopeHost(p.parsedURL())] = true
			own[scopeHost(p.hit.u)] = true
		}
	}
	type tally struct {
		sources map[string]bool
		links   []*url.URL
		root    string
	}
	by := map[string]*tally{}
	for _, p := range pages {
		if p.page == nil {
			continue
		}
		src := registrableDomain(p.parsedURL().Hostname())
		for _, u := range pageLinks(p.page.Markdown) {
			key, root := linkTarget(u)
			if key == "" || own[scopeHost(u)] || neverLinked(u) {
				continue
			}
			t := by[key]
			if t == nil {
				t = &tally{sources: map[string]bool{}, root: root}
				by[key] = t
			}
			t.sources[src] = true
			t.links = append(t.links, u)
		}
	}
	var cands []linkedSite
	for host, t := range by {
		if len(t.sources) < followMinSources {
			continue
		}
		root, err := linkedRoot(t.links)
		if t.root != "" {
			root, err = parseWebRoot(t.root)
		}
		if err != nil {
			continue
		}
		cands = append(cands, linkedSite{host: host, root: root, sources: len(t.sources), links: len(t.links), docs: docsLinked(t.links)})
	}
	if len(cands) == 0 {
		return linkedSite{}, false
	}
	slices.SortFunc(cands, func(a, b linkedSite) int {
		docs := 0
		if a.docs != b.docs {
			docs = map[bool]int{true: -1, false: 1}[a.docs]
		}
		return cmp.Or(cmp.Compare(b.sources, a.sources), docs, cmp.Compare(b.links, a.links), cmp.Compare(a.host, b.host))
	})
	return cands[0], true
}

// docsLinked reports links to a docs-looking site: a docs, developer or
// developers host, or a page under /docs.
func docsLinked(links []*url.URL) bool {
	for _, u := range links {
		first, _, _ := strings.Cut(scopeHost(u), ".")
		segs := pathSegments(u.Path)
		if first == "docs" || first == "developer" || first == "developers" || len(segs) > 0 && strings.EqualFold(segs[0], "docs") {
			return true
		}
	}
	return false
}

// linkedRoot is the root to read of one host's links: the directory all
// of them share (a link's directory is its path without its last segment,
// unless it ends in "/"); when they share none, the docs-looking first
// directory (linkDocsDir) the most of them sit under, a link to the host's
// root no veto; else the host's root. https when any link is.
func linkedRoot(links []*url.URL) (webRoot, error) {
	scheme := "http"
	var common []string
	under := map[string]int{}
	best := ""
	for i, u := range links {
		if strings.EqualFold(u.Scheme, "https") {
			scheme = "https"
		}
		segs := pathSegments(u.Path)
		dir := segs
		if len(dir) > 0 && !strings.HasSuffix(u.Path, "/") {
			dir = dir[:len(dir)-1]
		}
		if len(dir) > 0 && linkDocsDir(dir[0]) {
			under[dir[0]]++
			if best == "" || under[dir[0]] > under[best] {
				best = dir[0]
			}
		}
		if i == 0 {
			common = dir
			continue
		}
		n := 0
		for n < len(common) && n < len(dir) && common[n] == dir[n] {
			n++
		}
		common = common[:n]
	}
	if len(common) == 0 && best != "" {
		common = []string{best}
	}
	return parseWebRoot(scheme + "://" + hostKey(links[0]) + segmentsPath(common))
}

// linkDocsDir reports a first path segment a site keeps its docs under:
// docsTree's (docs, doc, documentation, manual), guide(s) or reference.
func linkDocsDir(seg string) bool {
	seg = strings.ToLower(seg)
	return docsSegments[seg] || seg == "guide" || seg == "guides" || seg == "reference"
}

// follow is a decided answer, today, whose pages read are not the makers'
// docs, answered instead from the site most of them link to when the
// provider labels a page of it official docs and judges it on the
// question; otherwise today with a clause in its note saying what was
// tried, or as it is when no site qualifies.
func (c *dCall) follow(ctx context.Context, req sfRequest, pages []*dPage, today Outcome, tr *sfTrace) Outcome {
	fs := c.followRead(ctx, req, pages, tr)
	if fs == nil {
		return today
	}
	defer fs.close()
	c.followJudge(ctx, fs)
	return c.followOutcome(req, pages, fs, today)
}

// followState is a follow under way: the site, how many pages read link
// to it, its pages labelled, those labelled official docs and their
// library, those on the question; note is why it is not used (a note
// clause), "" while it may be.
type followState struct {
	site     linkedSite
	read     int
	what     string
	note     string
	labelled []*dPage
	docs     []*dPage
	dl       *dLib
	on       []*dPage
}

// reject sets why the site is not used.
func (fs *followState) reject(why string) *followState {
	fs.note = "tried " + fs.what + ": " + why
	return fs
}

// close releases the site's library.
func (fs *followState) close() {
	if fs != nil && fs.dl != nil {
		fs.dl.l.store.Close()
	}
}

// followRead is the first half of a follow: the linked site read as
// --library reads it and the page pass of the pages it printed from, then
// those labelled official docs held as a library. nil when no site
// qualifies; its note set when the site cannot be used.
func (c *dCall) followRead(ctx context.Context, req sfRequest, pages []*dPage, tr *sfTrace) *followState {
	site, ok := linkedCandidate(pages)
	if !ok {
		return nil
	}
	if _, ok := c.s.indexedOver(site.root); ok {
		return nil // an indexed answer is never sent to a provider
	}
	read := 0
	for _, p := range pages {
		if p.page != nil {
			read++
		}
	}
	fs := &followState{site: site, read: read, what: fmt.Sprintf("%s, which %d of the %d pages read link to", site.root, site.sources, read)}
	if c.left() < cost(c.pageRequest(&dPage{page: &webPage{}})) {
		c.budget.follows = true
		fs.note = fmt.Sprintf("did not read %s: %s", site.root, budgetSpent)
		return fs
	}
	str := &sfTrace{}
	o := c.s.fromSearch(ctx, sfRequest{query: req.query, library: site.root.String(), opts: req.opts}, str)
	tr.planned, tr.fetched, tr.failed = tr.planned+str.planned, tr.fetched+str.fetched, tr.failed+str.failed
	if why := unfollowed(o); why != "" {
		return fs.reject(why)
	}
	dctx, cancel := context.WithTimeout(ctx, decideTimeout)
	defer cancel()
	labelled, err := c.labelRead(dctx, o)
	switch {
	case errors.Is(err, errBudget):
		c.budget.follows = true
		return fs.reject(budgetSpent)
	case errors.Is(err, errCookies):
		return fs.reject(readCookies)
	case err != nil:
		return fs.reject(unavailable(err))
	}
	fs.labelled = labelled
	for _, p := range labelled {
		if p != nil && p.tier == tierA {
			fs.docs = append(fs.docs, p)
		}
	}
	if len(fs.docs) == 0 {
		label := labelUnlabelled
		if len(labelled) > 0 && labelled[0] != nil {
			label = labelled[0].label
		}
		return fs.reject("labelled " + label + ", not the makers' docs")
	}
	if fs.dl, err = buildDecidedLib(req.query, fs.docs); err != nil {
		return fs.reject(oneLine(err.Error(), 200))
	}
	return fs
}

// followJudge is the second half: the passage pass of the site's official
// docs pages, within what the budget has left, and those on the question.
func (c *dCall) followJudge(ctx context.Context, fs *followState) {
	if fs.note != "" {
		return
	}
	dctx, cancel := context.WithTimeout(ctx, decideTimeout)
	defer cancel()
	if err := c.judgeRead(dctx, fs.dl, fs.docs); errors.Is(err, errBudget) {
		c.budget.follows = true
		fs.reject(budgetSpent)
		return
	} else if err != nil {
		fs.reject(unavailable(err))
		return
	}
	for _, p := range fs.docs {
		if p.context == confHigh {
			fs.on = append(fs.on, p)
		}
	}
	if len(fs.on) == 0 {
		fs.reject("documentation, but not on the question")
	}
}

// followOutcome is the followed site's answer, or today with the note
// saying why it was not used.
func (c *dCall) followOutcome(req sfRequest, pages []*dPage, fs *followState, today Outcome) Outcome {
	if fs.note != "" {
		return c.noted(today, fs.note)
	}
	return c.followed(req, fs.site, fs.read, pages, fs.dl, fs.on, fs.labelled, today)
}

// unavailable is a provider error as a note says it.
func unavailable(err error) string {
	why := err.Error()
	if !errors.Is(err, decide.ErrUnavailable) {
		why = decide.ErrUnavailable.Error() + ": " + why
	}
	return oneLine(why, 200)
}

// unfollowed is why a linked site's read gives no answer to use: "" when
// it printed docs text.
func unfollowed(o Outcome) string {
	switch {
	case errors.Is(o.err, errDisallowed):
		return "its robots.txt disallows the pages found"
	case !answered(o) || o.Reason == "results_only" || len(o.readPages) == 0:
		return "no page of it could be read"
	case len(o.Results) == 0:
		return "none of its pages holds the question's terms"
	}
	return ""
}

// noted is today with a clause in its note saying the linked site was
// tried or not read, and why it was not used, and the decisions every
// request counted.
func (c *dCall) noted(today Outcome, clause string) Outcome {
	today.Note = joinNote(today.Note, clause)
	use := c.use
	today.Decisions = &use
	return today
}

// labelRead asks the page pass of the first sfExactPages pages o printed
// from, at once: the pages labelled, by o.Read's order (nil past them or
// for a page not in hand or read with the user's cookies). errBudget, and
// nothing asked, when they do not fit what the decisions budget has left;
// errCookies when every one was read with the user's cookies.
func (c *dCall) labelRead(ctx context.Context, o Outcome) ([]*dPage, error) {
	out := make([]*dPage, len(o.Read))
	asked := make([]*dPage, len(o.Read))
	need, cookies := 0, false
	for i, r := range o.Read[:min(len(o.Read), sfExactPages)] {
		k := slices.IndexFunc(o.readPages, func(pg webPage) bool { return pg.URL == r.URL })
		u, err := url.Parse(r.URL)
		if k < 0 || err != nil {
			continue
		}
		if o.readPages[k].cookies {
			cookies = true
			continue
		}
		asked[i] = &dPage{hit: sfHit{u: u}, page: &o.readPages[k]}
		need += cost(c.pageRequest(asked[i]))
	}
	if need == 0 && cookies {
		return nil, errCookies
	}
	if !c.fits(need) {
		return nil, errBudget
	}
	errs := make([]error, len(o.Read))
	var wg sync.WaitGroup
	for i, p := range asked {
		if p == nil {
			continue
		}
		wg.Go(func() {
			var resp decide.Response
			if resp, errs[i] = c.ask(ctx, c.pageRequest(p)); errs[i] == nil {
				c.label(p, resp, c.releaseNamed)
				out[i] = p
			}
		})
	}
	wg.Wait()
	return out, firstError(errs)
}

// judgeRead asks the passage pass of the pages labelled official docs, at
// once, within shares of what the decisions budget has left: each page's
// passages' probabilities and its context, as a search result's.
// errBudget, and nothing asked, when no passage fits.
func (c *dCall) judgeRead(ctx context.Context, dl *dLib, docs []*dPage) error {
	send := c.share(dl, docs)
	none := true
	for i, p := range docs {
		none = none && len(p.passages) > 0 && send[i] != nil && len(send[i]) == 0
	}
	if none {
		return errBudget
	}
	errs := make([]error, len(docs))
	var wg sync.WaitGroup
	for i, p := range docs {
		wg.Go(func() { errs[i] = c.passagePass(ctx, dl, p, send[i]) })
	}
	wg.Wait()
	setPassageJudgments(docs)
	return firstError(errs)
}

// followed is the linked site's answer: its pages on the question (on),
// filled as a decided answer fills the search's (the primary by context,
// its passages by probability, then strong passages of the others), its
// read lines saying how the site was found; the search's pages read are
// passed over, the other results are the site's documentation pages read
// and not printed (unprinted), then the call's not already listed or
// printed; the decisions are the
// call's, and no next step is given (those were about the page the search
// read).
func (c *dCall) followed(req sfRequest, site linkedSite, read int, pages []*dPage, dl *dLib, on, labelled []*dPage, today Outcome) Outcome {
	primary := c.choose(on)
	budget := req.opts.Tokens
	if budget <= 0 {
		budget = localTokens
	}
	f := newFill(c, dl, budget)
	f.primary(primary)
	f.extras(primary, on)
	o := c.printed(f, req)
	o.Library = site.root.String()
	via := fmt.Sprintf("linked from %d of the pages read", site.sources)
	for i := range o.Read {
		o.Read[i].Via = via
	}
	o.Note = joinNote(fmt.Sprintf("no search result read is documentation from the makers; read %s instead, which %d of the %d pages read link to", site.root, site.sources, read), o.Note)
	for _, p := range pages {
		entry := PagePassed{URL: p.pageURL(), Rank: p.hit.rank, Reason: passedReason(p, primary), Context: p.context, Judgments: p.judged}
		if p.page != nil && !p.stub {
			entry.Label, entry.Tier = p.label, p.tier
		}
		o.PassedOver = append(o.PassedOver, entry)
	}
	o.OtherResults = unprinted(labelled, o.Read)
	for _, r := range today.OtherResults {
		listed := func(x OtherResult) bool { return x.URL == r.URL }
		if !slices.ContainsFunc(o.OtherResults, listed) && !slices.ContainsFunc(o.Read, func(p PageRead) bool { return p.URL == r.URL }) {
			o.OtherResults = append(o.OtherResults, r)
		}
	}
	use := c.use
	o.Decisions = &use
	if r, ok := questionRelease(req.query); ok && answered(o) {
		capped := releaseUnmarked(o, r)
		o = markRelease(o, r)
		if capped {
			capConfidence(&o)
		}
	}
	return o
}

// followedUnprinted marks a page of the followed site read, labelled
// documentation and not printed, among the other results.
const followedUnprinted = "from the followed site, not printed"

// unprinted are the pages of the followed site labelled documentation
// (tier A or B) that the answer did not print (read), as other results
// with no rank, so the hint's calls read them.
func unprinted(labelled []*dPage, read []PageRead) []OtherResult {
	var out []OtherResult
	for _, p := range labelled {
		if p == nil || p.tier != tierA && p.tier != tierB ||
			slices.ContainsFunc(read, func(r PageRead) bool { return r.URL == p.page.URL }) {
			continue
		}
		out = append(out, OtherResult{Title: firstNonEmpty(p.page.Title, p.page.URL), URL: p.page.URL, Label: p.label, Snippet: followedUnprinted})
	}
	return out
}
