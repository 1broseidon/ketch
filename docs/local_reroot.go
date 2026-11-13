package docs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Index roots that are not where a site keeps its current docs (ADR-0007,
// amendment 2):
//
//   - an alias: a root whose page redirects, refreshes or names as
//     canonical the same path at a numbered release (duckdb.org's
//     /docs/stable/ is /docs/2.0/), indexed at that release;
//   - an unversioned root whose page lists sit almost wholly in release
//     trees right below it, or whose llms.txt lists mostly one of them
//     (docs.crewai.com lists /v1.15.35/…, /edge/… and older releases, its
//     llms.txt only /v1.15.35/; Hugging Face's llms.txt only /v5.22.1/),
//     indexed at the release llms.txt lists, else the newest; when that
//     release's pages name the unversioned address as canonical, results
//     show it;
//   - an unversioned root whose pages sit in several numbered release
//     trees within two levels of it (sequelize.org lists /docs/v6/… and
//     /docs/v7/…, and /v5/ landing pages): indexed at the release the
//     root page links to most (the site's default docs), else the newest
//     final release, the others left out as other versions;
//   - a version's own host whose lists are an app shell (v18.angular.dev
//     serves its home page for sitemap.xml and llms.txt): the current
//     host's pages are probed at the same paths.

// rerootShare is the share of the listed pages below which a plan is
// taken to have missed where the docs are.
const rerootShare = 10

// releaseTreeMin is how many planned pages a release tree holds for
// splitReleases to count it, and releaseTreeShare the share of the plan
// (in percent) the release trees must hold together: a REST reference's
// /api/v1/ and /api/v2/ beside the guides are not the docs' releases.
const (
	releaseTreeMin   = 10
	releaseTreeShare = 67
)

// siblingThin is how few planned pages send a version host to its
// current host's page list.
const siblingThin = 50

// aliasRoot is the numbered release root's own page redirects, refreshes
// or points to as canonical, for a root that names no version or only a
// channel (stable, latest, current): ok is false for any other root, or
// when the page points nowhere else. A root at a numbered version is never
// moved: Read the Docs names the default version canonical on every
// version's pages.
func (w *Web) aliasRoot(ctx context.Context, root webRoot) (webRoot, bool) {
	if l := rootVersionLabel(root); l != "" && versionAliasClass[l] != "current" {
		return webRoot{}, false
	}
	pctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	defer cancel()
	raw, err := w.fetcher.FetchRaw(pctx, root.String())
	if err != nil || raw.URL == "" {
		return webRoot{}, false
	}
	served, err := url.Parse(raw.URL)
	if err != nil {
		return webRoot{}, false
	}
	cands := []*url.URL{served}
	if looksLikeHTML(raw.Body, raw.ContentType) {
		refresh, canonical := pagePointers(raw.Body, served)
		cands = append(cands, refresh, canonical)
	}
	for _, c := range cands {
		if c == nil || linkKey(c) == linkKey(root.u) {
			continue
		}
		if r, ok := canonicalRoot(root, c); ok && linkKey(r.u) != linkKey(root.u) {
			if nums, _ := releaseNumbers(normLabel(rootVersionLabel(r))); len(nums) > 0 {
				return r, true
			}
		}
	}
	return webRoot{}, false
}

// pagePointers reads a page's meta refresh target and canonical link.
func pagePointers(body []byte, base *url.URL) (refresh, canonical *url.URL) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, nil
	}
	resolve := func(href string) *url.URL {
		if href = strings.TrimSpace(href); href == "" {
			return nil
		}
		u, err := base.Parse(href)
		if err != nil {
			return nil
		}
		u.Fragment = ""
		return u
	}
	doc.Find("meta[http-equiv]").EachWithBreak(func(_ int, m *goquery.Selection) bool {
		if strings.EqualFold(m.AttrOr("http-equiv", ""), "refresh") {
			refresh = resolve(metaRefreshURL(m.AttrOr("content", "")))
			return false
		}
		return true
	})
	doc.Find("link[rel]").EachWithBreak(func(_ int, l *goquery.Selection) bool {
		if strings.EqualFold(strings.TrimSpace(l.AttrOr("rel", "")), "canonical") {
			canonical = resolve(l.AttrOr("href", ""))
			return false
		}
		return true
	})
	return refresh, canonical
}

// rerootTree is the release tree right below an unversioned (or channel)
// root that its pages sit in: when the plan kept under 1/rerootShare of
// the pages listed, or left out most of the pages llms.txt listed, the
// tree holding most of those llms.txt pages, else the newest numbered
// release left out with at least ten pages. ok is false otherwise; byList
// says llms.txt chose it (else the plan's thinness did, and the caller
// keeps the move only when the release holds more pages than were kept).
func rerootTree(root webRoot, links []webLink, p indexPlan, curated map[string]bool) (moved webRoot, byList, ok bool) {
	if l := rootVersionLabel(root); l != "" && versionAliasClass[l] != "current" || p.listed == 0 {
		return webRoot{}, false, false
	}
	inTree := map[string]int{} // llms.txt's pages by release tree
	listed := 0
	for _, l := range links {
		if curated[displayKey(l.url)] {
			listed++
			inTree[treeOf(l.url, root)]++
		}
	}
	best := bestTree(p.dropped, inTree)
	thin := len(p.links)*rerootShare < p.listed
	curatedOut := inTree[best] >= 10 && 2*inTree[best] >= listed
	if best == "" || !thin && !curatedOut {
		return webRoot{}, false, false
	}
	for _, l := range links {
		if treeOf(l.url, root) == best {
			prefix := l.url.Path[:strings.Index(l.url.Path, "/"+best+"/")+len(best)+2]
			r, err := parseWebRoot(root.origin() + prefix)
			return r, curatedOut, err == nil
		}
	}
	return webRoot{}, false, false
}

// bestTree is the numbered release among the trees left out (with at
// least ten pages) that holds the most llms.txt pages, the newest on a
// tie; "" when none is numbered.
func bestTree(dropped, inTree map[string]int) string {
	var best string
	var bestNums []int
	for label, n := range dropped {
		nums, _ := releaseNumbers(normLabel(label))
		if len(nums) == 0 || n < 10 {
			continue
		}
		if best == "" || inTree[label] > inTree[best] || inTree[label] == inTree[best] && compareRelease(nums, bestNums) > 0 {
			best, bestNums = label, nums
		}
	}
	return best
}

// treeOf is the version segment right below root (after a locale) that u
// sits in, as written, or "".
func treeOf(u *url.URL, root webRoot) string {
	segs := pathSegments(strings.TrimPrefix(u.Path, root.prefix))
	if len(segs) > 1 && localeRe.MatchString(segs[0]) && !versionLabelRe.MatchString(segs[0]) {
		segs = segs[1:]
	}
	if len(segs) < 2 {
		return ""
	}
	if _, ok := variantLabel(segs[0]); ok {
		return segs[0]
	}
	return ""
}

// displayRootFor is the unversioned root to show a re-rooted current
// release's pages under: from, when a page of the release (sample) names
// as canonical its address without the release segment; "" otherwise.
func (w *Web) displayRootFor(ctx context.Context, from, to webRoot, sample *url.URL) string {
	if sample == nil || !strings.HasPrefix(sample.Path, to.prefix) {
		return ""
	}
	pctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	defer cancel()
	raw, err := w.fetcher.FetchRaw(pctx, sample.String())
	if err != nil || !looksLikeHTML(raw.Body, raw.ContentType) {
		return ""
	}
	_, canonical := pagePointers(raw.Body, sample)
	want := from.String() + strings.TrimPrefix(sample.Path, to.prefix)
	if canonical != nil && strings.TrimSuffix(canonical.String(), "/") == strings.TrimSuffix(want, "/") {
		return from.String()
	}
	return ""
}

// siblingHost is the current docs' root beside a version's own host
// (angular.dev beside v18.angular.dev), at the same path; ok is false for
// a root whose host names no version.
func siblingHost(root webRoot) (webRoot, bool) {
	first, rest, ok := strings.Cut(root.u.Host, ".")
	if !ok || !strings.Contains(rest, ".") {
		return webRoot{}, false
	}
	if l, ok := variantLabel(first); !ok || !strings.ContainsAny(l, "0123456789") {
		return webRoot{}, false
	}
	u := *root.u
	u.Host = rest
	r, err := parseWebRoot(u.String())
	return r, err == nil
}

// atHost are links moved to host, the same paths (siblingHost's pages
// probed on the version's host).
func atHost(links []webLink, host string) []webLink {
	out := make([]webLink, 0, len(links))
	for _, l := range links {
		u := *l.url
		u.Host = host
		out = append(out, webLink{url: &u, title: l.title})
	}
	return out
}

// uniqueContent drops pages whose Markdown is another's, the first kept:
// a page served at two addresses, and a missing page a site answers with
// its home page (an app shell's 200 for any path).
func uniqueContent(pages []webPage) ([]webPage, int) {
	seen := map[[32]byte]bool{}
	out := pages[:0:0]
	for _, p := range pages {
		k := contentKey(p)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out, len(pages) - len(out)
}

// contentKey identifies a page's text, whitespace aside: two pages with
// one key are one text (uniqueContent).
func contentKey(p webPage) [32]byte {
	return sha256.Sum256([]byte(strings.Join(strings.Fields(p.Markdown), " ")))
}

// siteNameSeps split a page title into its parts ("Home • Angular",
// "Vue Router | The official Router for Vue.js").
var siteNameSeps = []string{" | ", " • ", " · ", " — ", " – ", " - "}

// siteNameNoise is what trails a site's name in a title part: the word
// documentation, docs and a release ("Beautiful Soup 4.14.3
// documentation", "SvelteKit Docs").
var siteNameNoise = regexp.MustCompile(`(?i)(?:\s+(?:v?\d+(?:\.\d+)*\S*|docs?|documentation|manual|reference))+$`)

// siteName is a docs site's own name, from og:site_name or its root page's
// title: the title part that repeats ("Chart.js | Chart.js"), else the last
// part when the title has several and it is short ("Home • Angular"), else
// a short first part ("Vue Router | The official Router for Vue.js"), each
// without a trailing "documentation" or release; "" when nothing is short
// enough to be a name.
func siteName(title, ogSiteName string) string {
	clean := func(s string) string {
		return strings.TrimSpace(siteNameNoise.ReplaceAllString(strings.TrimSpace(s), ""))
	}
	short := func(s string) bool { return s != "" && len(strings.Fields(s)) <= 3 }
	if n := clean(ogSiteName); short(n) {
		return n
	}
	parts := []string{strings.TrimSpace(strings.Map(dropInvisible, title))}
	for _, sep := range siteNameSeps {
		var next []string
		for _, p := range parts {
			next = append(next, strings.Split(p, sep)...)
		}
		parts = next
	}
	seen := map[string]int{}
	for i, p := range parts {
		parts[i] = clean(p)
		seen[strings.ToLower(parts[i])]++
	}
	for _, p := range parts {
		if short(p) && seen[strings.ToLower(p)] > 1 {
			return p
		}
	}
	if last := parts[len(parts)-1]; len(parts) > 1 && short(last) {
		return last
	}
	if short(parts[0]) {
		return parts[0]
	}
	return ""
}

// siteIdentityNames are a docs site's name (siteName) and the registry
// packages its root page links to that go by that name or by one of the
// root's host labels (a site links to its own package page; its
// dependencies' are other names).
func (w *Web) siteIdentityNames(ctx context.Context, root webRoot) (string, []string) {
	prof, err := w.cachedProfile(ctx, root.u)
	if err != nil || prof == nil {
		return "", nil
	}
	name := siteName(prof.title, prof.meta["og:site_name"])
	own := map[string]bool{}
	if k := libraryKey(name); k != "" {
		own[k] = true
	}
	for _, h := range hostNames(root.u) {
		own[libraryKey(h)] = true
	}
	var pkgs []string
	for _, l := range prof.links {
		ref := registryPackageOf(l.url)
		if ref.name == "" || !own[libraryKey(ref.name)] {
			continue
		}
		if spec := string(ref.eco) + ":" + ref.name; !slices.Contains(pkgs, spec) {
			pkgs = append(pkgs, spec)
		}
	}
	return name, pkgs
}

// splitReleases is the release tree an unversioned root's docs should be
// indexed at when the plan holds pages of two or more numbered release
// trees (each at least releaseTreeMin pages, together releaseTreeShare of
// the plan) within two levels of the root: the one the root page links to
// most, else the newest final release. ok is false otherwise.
func splitReleases(root webRoot, links []webLink, rootLinks []siteLink) (webRoot, bool) {
	if rootVersionLabel(root) != "" {
		return webRoot{}, false
	}
	count := map[string]int{}
	label := map[string]string{}
	for _, l := range links {
		if prefix, lab := releaseTreeOf(l.url, root); prefix != "" {
			count[prefix]++
			label[prefix] = lab
		}
	}
	var trees []string
	held := 0
	for prefix, n := range count {
		if n >= releaseTreeMin {
			trees = append(trees, prefix)
			held += n
		}
	}
	if len(trees) < 2 || held*100 < len(links)*releaseTreeShare {
		return webRoot{}, false
	}
	linked := linksByTree(root, rootLinks)
	slices.SortFunc(trees, func(a, b string) int {
		if linked[a] != linked[b] {
			return linked[b] - linked[a]
		}
		na, preA := releaseNumbers(normLabel(label[a]))
		nb, preB := releaseNumbers(normLabel(label[b]))
		if preA != preB {
			if preA {
				return 1
			}
			return -1
		}
		if c := compareRelease(na, nb); c != 0 {
			return -c
		}
		return strings.Compare(a, b)
	})
	r, err := parseWebRoot(root.origin() + root.prefix + trees[0] + "/")
	return r, err == nil
}

// linksByTree counts the root page's links into each release tree.
func linksByTree(root webRoot, links []siteLink) map[string]int {
	out := map[string]int{}
	for _, sl := range links {
		if sl.url != nil && root.contains(sl.url) {
			if prefix, _ := releaseTreeOf(sl.url, root); prefix != "" {
				out[prefix]++
			}
		}
	}
	return out
}

// releaseTreeOf is the release tree u sits in within two levels of root
// (after a locale): the path below the root up to a numbered version
// segment (docs/v6, v5, 2.x) with a page below it, and that segment; ""
// when there is none.
func releaseTreeOf(u *url.URL, root webRoot) (string, string) {
	segs := pathSegments(strings.TrimPrefix(u.Path, root.prefix))
	skip := 0
	if len(segs) > 1 && localeRe.MatchString(segs[0]) && !versionLabelRe.MatchString(segs[0]) {
		skip = 1
	}
	for i := skip; i < min(skip+2, len(segs)-1); i++ {
		if lab, ok := variantLabel(segs[i]); ok {
			if nums, _ := releaseNumbers(normLabel(lab)); len(nums) > 0 {
				return strings.Join(segs[:i+1], "/"), segs[i]
			}
			return "", ""
		}
	}
	return "", ""
}
