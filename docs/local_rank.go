package docs

import (
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Section answers: the local index's default view (ADR-0007, amendment 2).
// Sections ranked by keywords (keywordRank, local_fts.go) are grouped into
// families: a section and the others under the same second-level heading
// of its page (an H2 with its H3s; a page's lead section is a family of
// its own). Each family is one result, carrying the family's own text and
// the sections of it that ranked, verbatim, code blocks inline, trimmed to
// a share of the budget that keeps each section's lead prose and first
// code block. Two families with the same heading and text (a page served
// at two addresses) are one.
//
// The weights are few on purpose. On 63 gold-labelled dev questions over
// frozen indexes, ranking sections instead of snippet units and families
// each moved the gold section into the top five more often; a penalty for
// thin API stubs or reference paths and a prior for a page's first
// sections did not.

const (
	// localMaxResults is how many family results are returned without a
	// limit: what an agent reads, and where the measurements were taken.
	localMaxResults = 5
	// localTokens is the section view's default budget and
	// localResultTokens one result's most, at about webCharsPerToken bytes
	// a token.
	localTokens       = 1800
	localResultTokens = 600
	// localRankedDepth is how deep in the ranking a section must sit
	// for its family to show it beyond the family's own text.
	localRankedDepth = 40
	// localFamilyParts is the most sections one result shows when its
	// family does not fit whole.
	localFamilyParts = 3
	// localFamiliesPerPage is how many families of one page the results
	// hold before families of other pages go first.
	localFamiliesPerPage = 2
	// localLeadBytes bounds the lead prose a trimmed section keeps before
	// its first code block.
	localLeadBytes = 600
)

// rankedSection is a section (an index into the library's sections) and
// its score.
type rankedSection struct {
	sec   int
	score float64
}

// family is the sections of one page under one second-level heading.
type family struct {
	key     string
	page    string
	members []int           // every section of the family, in page order
	ranked  map[int]float64 // the members that ranked, by score
	best    int             // the best-ranked member
}

// familyKey is the family a section belongs to: its page and the second
// entry of its breadcrumb (the H2 it sits under, or is), or its page alone
// for the page's lead.
func familyKey(s docsindex.Section) string {
	if len(s.Crumbs) >= 2 {
		return s.Page + "\x00" + s.Crumbs[1]
	}
	return s.Page + "\x00"
}

// families groups the ranked sections into families, best first: at most
// limit of them, at most familiesPerPage of one page while other pages'
// rank next, and never two with the same heading and text, or the same H2 on
// one page at two addresses (indexedPageKey: x and x.html, an llms/
// mirror),
// which an index built before its planner knew them as one may hold.
func families(secs []docsindex.Section, ranked []rankedSection, limit int, root string) []*family {
	all := map[string][]int{}
	for i, s := range secs {
		k := familyKey(s)
		all[k] = append(all[k], i)
	}
	var order []*family
	byKey := map[string]*family{}
	for r, rs := range ranked {
		k := familyKey(secs[rs.sec])
		f, ok := byKey[k]
		if !ok {
			members := all[k]
			sort.SliceStable(members, func(a, b int) bool { return secs[members[a]].Order < secs[members[b]].Order })
			f = &family{key: k, page: secs[rs.sec].Page, members: members, ranked: map[int]float64{}, best: rs.sec}
			byKey[k] = f
			order = append(order, f)
		}
		if r < localRankedDepth || rs.sec == f.best {
			f.ranked[rs.sec] = rs.score
		}
	}
	var out, deferred []*family
	seen := map[string]bool{}
	r, rerr := parseWebRoot(root)
	perPage := map[string]int{}
	for _, f := range order {
		page := indexedPageKey(f.page, root)
		ck, hk := familyContent(secs, f), page+"\x00"+familyHeading(secs, f)
		if seen[ck] || seen[hk] {
			continue
		}
		seen[ck], seen[hk] = true, true
		if perPage[page] >= familiesPerPage(f.page, r, rerr == nil) {
			deferred = append(deferred, f)
			continue
		}
		perPage[page]++
		out = append(out, f)
		if len(out) >= limit {
			return out
		}
	}
	out = append(out, deferred...)
	return out[:min(limit, len(out))]
}

// indexedPageKey is pageKey (host without www., no trailing slash or
// .html) with an index page and an llms directory right below the root
// set aside too (mantine.dev/llms/core/modal is mantine.dev/core/modal/).
func indexedPageKey(page, root string) string {
	if root != "" {
		if rest, ok := strings.CutPrefix(page, strings.TrimSuffix(root, "/")+"/llms/"); ok {
			page = strings.TrimSuffix(root, "/") + "/" + rest
		}
	}
	k := strings.TrimSuffix(strings.TrimSuffix(pageKey(page), ".md"), "/index")
	return strings.ToLower(k)
}

// familyHeading is the heading a family stands under: its H2, or its
// page's title for a page's lead.
func familyHeading(secs []docsindex.Section, f *family) string {
	c := secs[f.members[0]].Crumbs
	if len(c) >= 2 {
		return strings.ToLower(strings.TrimSpace(c[1]))
	}
	return "\x00lead"
}

// familiesPerPage is how many families of a page the results hold before
// other pages' go first: localFamiliesPerPage, but one for an API
// reference leaf (planTier), whose page is one entry: its description and
// its signature are not two answers.
func familiesPerPage(page string, root webRoot, rootOK bool) int {
	if u, err := url.Parse(page); err == nil && rootOK && planTier(u, root, nil) == tierReference {
		return 1
	}
	return localFamiliesPerPage
}

// familyContent identifies a family by its first section's heading and
// the start of its text, so copies of a page at two addresses are one.
func familyContent(secs []docsindex.Section, f *family) string {
	s := secs[f.members[0]]
	text := strings.Join(strings.Fields(s.Text), " ")
	if len(text) > 300 {
		text = text[:300]
	}
	return s.Heading + "\x00" + text
}

// familyResults renders the families within the token budget (about
// webCharsPerToken bytes a token), shared max-min fairly among them and
// none over localResultTokens: a family that fits is shown whole, else
// its own section and its best-ranked ones (at most localFamilyParts), in
// page order, each trimmed to its share by trimSection.
func familyResults(lib docsindex.Library, secs []docsindex.Section, fams []*family, query string, tokens int, hint HintStyle) []Result {
	if tokens > 0 {
		return compactResults(lib, secs, fams, query, tokens, hint)
	}
	tokens = localTokens
	budget := tokens * webCharsPerToken
	maxResult := localResultTokens * webCharsPerToken
	sizes := make([]int, len(fams))
	for i, f := range fams {
		for _, m := range f.members {
			sizes[i] += len(secs[m].Text)
		}
		sizes[i] = min(sizes[i], maxResult)
	}
	shares := fairShares(sizes, budget)
	terms := queryTerms(query)
	out := make([]Result, 0, len(fams))
	for i, f := range fams {
		out = append(out, familyResult(lib, secs, f, min(shares[i], maxResult), terms))
	}
	return out
}

// familyResult is one family as a result within share bytes, under the
// address of its best-ranked section and the title of the family.
func familyResult(lib docsindex.Library, secs []docsindex.Section, f *family, share int, terms []string) Result {
	shown := familyParts(secs, f, share)
	sizes := make([]int, len(shown))
	for i, m := range shown {
		sizes[i] = len(secs[m].Text)
	}
	parts := fairShares(sizes, share)
	head := secs[f.members[0]]
	title := head.Heading
	crumbs := head.Crumbs
	if len(crumbs) > 2 {
		crumbs = crumbs[:2]
		title = crumbs[1]
	}
	var b strings.Builder
	var anchors []string
	cut := len(shown) < len(f.members)
	for i, m := range shown {
		s := secs[m]
		text := s.Text
		if len(text) > parts[i] {
			text = trimSection(text, parts[i], terms)
			cut = true
		}
		if b.Len() > 0 || s.Heading != title {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString("#### " + s.Heading + "\n\n")
		}
		b.WriteString(text)
		anchors = append(anchors, s.Anchor)
	}
	return Result{
		Library: lib.Root, Version: lib.Version, Title: title, Breadcrumb: strings.Join(crumbs, " > "),
		Snippet: b.String(), URL: webSectionOf(secs[f.best], 0).URL(), Anchors: anchors, Source: SearchBackend, Truncated: cut,
	}
}

// familyParts are the sections a family result shows, in page order: all
// of them when they fit share, else its own section (the first, when it
// is the family's heading) and its best-ranked members, at most
// localFamilyParts in all.
func familyParts(secs []docsindex.Section, f *family, share int) []int {
	total := 0
	for _, m := range f.members {
		total += len(secs[m].Text)
	}
	if total <= share {
		return f.members
	}
	pick := map[int]bool{f.best: true}
	first := f.members[0]
	if len(secs[first].Crumbs) <= 2 && strings.TrimSpace(secs[first].Text) != "" {
		pick[first] = true // the H2's own text, or the page's lead
	}
	byScore := make([]int, 0, len(f.ranked))
	for m := range f.ranked {
		byScore = append(byScore, m)
	}
	sort.Slice(byScore, func(a, b int) bool {
		if f.ranked[byScore[a]] != f.ranked[byScore[b]] {
			return f.ranked[byScore[a]] > f.ranked[byScore[b]]
		}
		return byScore[a] < byScore[b]
	})
	for _, m := range byScore {
		if len(pick) >= localFamilyParts {
			break
		}
		pick[m] = true
	}
	var out []int
	for _, m := range f.members {
		if pick[m] {
			out = append(out, m)
		}
	}
	return out
}

// trimSection cuts a section's text to about n bytes by whole blocks: its
// lead prose (the paragraphs before its first code block, up to
// localLeadBytes, the first always), its first code block (cut, fence
// closed, when it does not fit whole), then the blocks that name the most
// query terms, in their order, with elidedMarker where blocks were left
// out. A lead that does not fit alone is cut as trimSnippet cuts.
func trimSection(s string, n int, terms []string) string {
	if len(s) <= n {
		return s
	}
	blocks := textBlocks(s)
	keep := map[int]bool{}
	fits := func() bool { return len(joinKept(blocks, keep)) <= n }
	keepLead(blocks, keep, fits)
	if len(keep) == 0 {
		return trimSnippet(s, n)
	}
	keepFirstCode(blocks, keep, n)
	for _, i := range blocksByTerms(blocks, terms) {
		if keep[i] {
			continue
		}
		keep[i] = true
		if !fits() {
			delete(keep, i)
		}
	}
	return joinKept(blocks, keep)
}

// keepLead keeps the prose blocks before the first code block, up to
// localLeadBytes (the first always, when it fits).
func keepLead(blocks []string, keep map[int]bool, fits func() bool) {
	lead := 0
	for i, b := range blocks {
		if isCodeBlock(b) || i > 0 && lead+len(b) > localLeadBytes {
			return
		}
		keep[i] = true
		if !fits() {
			delete(keep, i)
			return
		}
		lead += len(b)
	}
}

// keepFirstCode keeps the first code block, cut to what is left of n
// (its fence closed) when it does not fit whole.
func keepFirstCode(blocks []string, keep map[int]bool, n int) {
	i := slices.IndexFunc(blocks, isCodeBlock)
	if i < 0 {
		return
	}
	keep[i] = true
	if len(joinKept(blocks, keep)) <= n {
		return
	}
	delete(keep, i)
	room := n - len(joinKept(blocks, keep)) - len("\n\n"+elidedMarker+"\n\n")
	if room < webMinTrimBytes {
		return
	}
	if cut := trimSnippet(blocks[i], room); cut != "" {
		blocks[i] = cut
		keep[i] = true
	}
}

// isCodeBlock reports a block (textBlocks) that is fenced code.
func isCodeBlock(b string) bool {
	var f fenceState
	return f.step(lineOf(b))
}
