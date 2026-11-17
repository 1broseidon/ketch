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
//	distinct source sites that link to it
//	        │
//	a host followMinSources or more sites link to: the most sites, then a
//	docs-looking host or path, then the most links; ONE
//	        │
//	read as --library <root> reads it (its search, robots.txt, page
//	limits); the pages printed labelled by the page pass
//	        │
//	official docs: its answer, read lines "linked from N of the pages
//	read", the search's pages passed over; otherwise the answer as it was,
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

// linkedCandidate is the site the pages read link to that a decided
// answer reads: of the hosts followMinSources or more source sites link
// to (never a source page's own host), the one the most sites link to,
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
	}
	by := map[string]*tally{}
	for _, p := range pages {
		if p.page == nil {
			continue
		}
		src := registrableDomain(p.parsedURL().Hostname())
		for _, u := range pageLinks(p.page.Markdown) {
			host := scopeHost(u)
			if own[host] || neverLinked(u) {
				continue
			}
			t := by[host]
			if t == nil {
				t = &tally{sources: map[string]bool{}}
				by[host] = t
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
// provider labels a page of it official docs; otherwise today with a
// clause in its note saying what was tried, or as it is when no site
// qualifies.
func (c *dCall) follow(ctx context.Context, req sfRequest, pages []*dPage, today Outcome, tr *sfTrace) Outcome {
	site, ok := linkedCandidate(pages)
	if !ok {
		return today
	}
	if _, ok := c.s.indexedOver(site.root); ok {
		return today // an indexed answer is never sent to a provider
	}
	read := 0
	for _, p := range pages {
		if p.page != nil {
			read++
		}
	}
	what := fmt.Sprintf("%s, which %d of the %d pages read link to", site.root, site.sources, read)
	str := &sfTrace{}
	o := c.s.fromSearch(ctx, sfRequest{query: req.query, library: site.root.String(), opts: req.opts}, str)
	tr.planned, tr.fetched, tr.failed = tr.planned+str.planned, tr.fetched+str.fetched, tr.failed+str.failed
	if why := unfollowed(o); why != "" {
		return c.tried(today, what, why)
	}
	labelled, err := c.labelRead(ctx, o)
	if err != nil {
		why := err.Error()
		if !errors.Is(err, decide.ErrUnavailable) {
			why = decide.ErrUnavailable.Error() + ": " + why
		}
		return c.tried(today, what, oneLine(why, 200))
	}
	var primary *dPage
	for _, p := range labelled {
		if p != nil && p.tier == tierA {
			primary = p
			break
		}
	}
	if primary == nil {
		label := labelUnlabelled
		if len(labelled) > 0 && labelled[0] != nil {
			label = labelled[0].label
		}
		return c.tried(today, what, "labelled "+label+", not the makers' docs")
	}
	return c.followed(o, req, site, read, pages, labelled, primary, today)
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

// tried is today with a clause in its note saying the linked site (what)
// was tried and why it was not used, and the decisions every request
// counted.
func (c *dCall) tried(today Outcome, what, why string) Outcome {
	today.Note = joinNote(today.Note, "tried "+what+": "+why)
	use := c.use
	today.Decisions = &use
	return today
}

// labelRead asks the page pass of the first sfExactPages pages o printed
// from, at once: the pages labelled, by o.Read's order (nil past them or
// for a page not in hand).
func (c *dCall) labelRead(ctx context.Context, o Outcome) ([]*dPage, error) {
	dctx, cancel := context.WithTimeout(ctx, decideTimeout)
	defer cancel()
	out := make([]*dPage, len(o.Read))
	errs := make([]error, len(o.Read))
	var wg sync.WaitGroup
	for i, r := range o.Read[:min(len(o.Read), sfExactPages)] {
		k := slices.IndexFunc(o.readPages, func(pg webPage) bool { return pg.URL == r.URL })
		u, err := url.Parse(r.URL)
		if k < 0 || err != nil {
			continue
		}
		p := &dPage{hit: sfHit{u: u}, page: &o.readPages[k]}
		wg.Go(func() {
			var resp decide.Response
			if resp, errs[i] = c.ask(dctx, c.pageRequest(p)); errs[i] == nil {
				c.label(p, resp, c.releaseNamed)
				out[i] = p
			}
		})
	}
	wg.Wait()
	return out, firstError(errs)
}

// followed is the linked site's answer, o, as a decided answer: its read
// lines say how the site was found, its pages' labels, and the confidence
// of a page whose passages were matched by the question's words, not
// judged (at most medium); the search's pages read are passed over, the
// other results and the decisions are the call's, and no next step is
// given (those were about the page the search read).
func (c *dCall) followed(o Outcome, req sfRequest, site linkedSite, read int, pages, labelled []*dPage, primary *dPage, today Outcome) Outcome {
	via := fmt.Sprintf("linked from %d of the pages read", site.sources)
	conf := map[string]*dPage{}
	for i := range o.Read {
		o.Read[i].Rank, o.Read[i].Via = 0, via
		if p := labelled[i]; p != nil {
			o.Read[i].Label, o.Read[i].Tier, o.Read[i].Judgments = p.label, p.tier, p.judged
			o.Read[i].Confidence = lower(tierBand[p.tier], confMedium)
			conf[o.Read[i].URL] = p
		}
	}
	for i := range o.Results {
		if p := conf[pageOf(o.Results[i])]; p != nil {
			o.Results[i].SourceLabel, o.Results[i].Confidence = p.label, lower(tierBand[p.tier], confMedium)
		}
	}
	o.Note = joinNote(fmt.Sprintf("no search result read is documentation from the makers; read %s instead, which %d of the %d pages read link to", site.root, site.sources, read), o.Note)
	o.PassedOver = nil
	for _, p := range pages {
		entry := PagePassed{URL: p.pageURL(), Rank: p.hit.rank, Reason: passedReason(p, primary), Context: p.context, Judgments: p.judged}
		if p.page != nil && !p.stub {
			entry.Label, entry.Tier = p.label, p.tier
		}
		o.PassedOver = append(o.PassedOver, entry)
	}
	// The caller named no library: no origin line says one names this site.
	o.OtherResults, o.Next, o.Origin = today.OtherResults, nil, ""
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
