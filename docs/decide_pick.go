package docs

import (
	"cmp"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// choose is the primary page among the pages with a passage pass: context
// high first, else medium. Within a context: the best-ranked tier A page,
// unless it ranks below topRanks and a tier B page ranks above it (then
// the best-ranked tier B page); then tier B by rank; tier C only when the
// context has no tier A or B page, the makers' pages first. nil when no
// page has context high or medium.
func (c *dCall) choose(pages []*dPage) *dPage {
	for _, ctx := range []string{confHigh, confMedium} {
		var in []*dPage
		for _, p := range pages {
			if p.context == ctx {
				in = append(in, p)
			}
		}
		if len(in) == 0 {
			continue
		}
		slices.SortStableFunc(in, func(a, b *dPage) int {
			return cmp.Or(cmp.Compare(c.cOrder(a), c.cOrder(b)), cmp.Compare(a.hit.rank, b.hit.rank))
		})
		a, b := ofTier(in, tierA), ofTier(in, tierB)
		if len(a) > 0 && (a[0].hit.rank <= topRanks || len(b) == 0 || b[0].hit.rank >= a[0].hit.rank) {
			return a[0]
		}
		if len(b) > 0 {
			return b[0]
		}
		return in[0]
	}
	return nil
}

func ofTier(pages []*dPage, tier string) []*dPage {
	var out []*dPage
	for _, p := range pages {
		if p.tier == tier {
			out = append(out, p)
		}
	}
	return out
}

// cOrder puts the makers' pages (makers at or above Official) first
// within a tier.
func (c *dCall) cOrder(p *dPage) int {
	if p.judged != nil && p.judged.Makers >= c.cut.Official {
		return 0
	}
	return 1
}

// tierKey orders pages among tiers: A, B, the makers' tier C, the rest.
func (c *dCall) tierKey(p *dPage) int {
	switch p.tier {
	case tierA:
		return 0
	case tierB:
		return 1
	}
	return 2 + c.cOrder(p)
}

// passedReason is why page p, not printed, was passed over, primary being
// the page read (nil for none).
func passedReason(p, primary *dPage) string {
	switch {
	case p.page == nil:
		return p.cause
	case p.stub:
		return passedStub
	case primary != nil && urlKey(p.parsedURL()) == urlKey(primary.parsedURL()):
		return passedSame
	case p.unscored != "":
		return p.unscored
	case p.otherRel:
		return passedRelease
	case p.context == confLow:
		return passedNothing
	case p.context == confHigh:
		return passedAlso
	case primary != nil && tierAfter(p.tier, primary.tier):
		return passedWeaker
	}
	return passedNeedless
}

// tierAfter reports tier a worse than tier b ("" is tier C).
func tierAfter(a, b string) bool { return cmp.Or(a, tierC) > cmp.Or(b, tierC) }

// twinLocaleRe matches a locale path segment: en-us, ca, id_id, us_en.
var twinLocaleRe = regexp.MustCompile(`^[a-z]{2}([-_][a-z]{2,4})?$`)

// urlKey is an address compared for twins: the host without www., the
// path without locale segments, the last without .md, .html or .rst.
func urlKey(u *url.URL) string {
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	var segs []string
	for _, s := range strings.Split(strings.ToLower(u.Path), "/") {
		if s != "" && !twinLocaleRe.MatchString(s) {
			segs = append(segs, s)
		}
	}
	if n := len(segs); n > 0 {
		for _, ext := range []string{".md", ".html", ".htm", ".rst"} {
			segs[n-1] = strings.TrimSuffix(segs[n-1], ext)
		}
	}
	return host + "/" + strings.Join(segs, "/")
}

// normWordsRe drops a Markdown link's target, keeping its text.
var (
	normLinkRe  = regexp.MustCompile(`\]\([^)]*\)`)
	normWordsRe = regexp.MustCompile(`[a-z0-9]+`)
)

// normWords are text's words, lower case, link targets left out.
func normWords(text string) []string {
	return normWordsRe.FindAllString(strings.ToLower(normLinkRe.ReplaceAllString(text, "]")), -1)
}

// shingles are text's runs of shingleWords words.
func shingles(text string) map[string]bool {
	w := normWords(text)
	out := map[string]bool{}
	for i := 0; i+shingleWords <= len(w); i++ {
		out[strings.Join(w[i:i+shingleWords], " ")] = true
	}
	return out
}

// printedText is what an answer has printed so far, for the repeat rule.
type printedText struct {
	sh   map[string]bool
	norm string
}

func (t *printedText) add(text string) {
	if t.sh == nil {
		t.sh = map[string]bool{}
	}
	for k := range shingles(text) {
		t.sh[k] = true
	}
	t.norm += " " + strings.Join(normWords(text), " ")
}

// share is how much of text is printed already: the share of its
// shingles printed; a text under shingleWords words is 1 when its words
// appear in that order in the printed text, else 0.
func (t *printedText) share(text string) float64 {
	sh := shingles(text)
	if len(sh) > 0 {
		n := 0
		for k := range sh {
			if t.sh[k] {
				n++
			}
		}
		return float64(n) / float64(len(sh))
	}
	w := strings.Join(normWords(text), " ")
	if w != "" && strings.Contains(t.norm, w) {
		return 1
	}
	return 0
}

// textShare is the share of a's shingles that are b's.
func textShare(a, b string) float64 {
	sa, sb := shingles(a), shingles(b)
	if len(sa) == 0 {
		return 0
	}
	n := 0
	for k := range sa {
		if sb[k] {
			n++
		}
	}
	return float64(n) / float64(len(sa))
}

// nextCand is a source better than the page read that next may name.
type nextCand struct {
	tier, url, label, read string
	rank                   int
}

// nextSteps are at most three calls: a better kind of source than the
// primary (a tier strictly better: a page read, not printed, with a
// passage on the question and under dupShare of its text on the page read,
// or a result not read labelled official docs), the cut section of the
// primary page holding its best strong passage not printed, and the page
// the primary displaced: one the search ranked higher, also on the
// question (higherRanked).
func (c *dCall) nextSteps(primary *dPage, pages []*dPage, rest []sfHit, others []otherLabel, printed map[string]bool, f *dFill) []NextStep {
	if primary == nil {
		return nil
	}
	var out []NextStep
	best := betterSource(primary, pages, rest, others, printed)
	if best != nil {
		out = append(out, libraryStep(*best, nextBetter))
	}
	if sec := f.cutSection(primary); sec != "" {
		out = append(out, NextStep{Command: "ketch docs --section " + sec, Why: nextSection, Section: sec})
	}
	if up := higherRanked(primary, pages, printed); up != nil && (best == nil || best.url != up.url) {
		out = append(out, libraryStep(*up, nextHigher))
	}
	return out
}

// libraryStep is the call that asks the question of source n.
func libraryStep(n nextCand, why string) NextStep {
	return NextStep{Command: `ketch docs --library ` + n.url + ` "<your question>"`, Why: why,
		Library: n.url, Label: n.label, Rank: n.rank, Read: n.read}
}

// higherRanked is the best-ranked page read and passed over as also on
// the question (context high) that the search ranked above the primary:
// the page the primary displaced, which may be the closer one when a name
// is ambiguous. Never the primary under another address (passedSame) or
// a copy of its text (dupShare or more on the page read), never a printed
// address. nil when the primary is the best-ranked such page.
func higherRanked(primary *dPage, pages []*dPage, printed map[string]bool) *nextCand {
	var best *nextCand
	for _, p := range pages {
		if p.reason != passedAlso || p.context != confHigh || p.page == nil || p.hit.rank >= primary.hit.rank ||
			printed[urlKey(p.parsedURL())] || textShare(p.page.Markdown, primary.page.Markdown) >= dupShare {
			continue
		}
		if best == nil || p.hit.rank < best.rank {
			best = &nextCand{p.tier, p.pageURL(), p.label, "read, not printed", p.hit.rank}
		}
	}
	return best
}

// betterSource is the best source of a better tier than the primary's:
// a page read, not printed, on the question, with under dupShare of its
// text on the page read; or a result not read labelled official docs;
// never at a printed page's address. nil for none.
func betterSource(primary *dPage, pages []*dPage, rest []sfHit, others []otherLabel, printed map[string]bool) *nextCand {
	var cands []nextCand
	for _, p := range pages {
		if p.reason == passedAlso && textShare(p.page.Markdown, primary.page.Markdown) < dupShare {
			cands = append(cands, nextCand{p.tier, p.pageURL(), p.label, "read, not printed", p.hit.rank})
		}
	}
	for i, h := range rest {
		if i < len(others) && others[i].label == labelOfficialDocs {
			cands = append(cands, nextCand{cmp.Or(tierOf[others[i].label], tierC), h.u.String(), others[i].label, "not read", h.rank})
		}
	}
	var best *nextCand
	for i, n := range cands {
		u, err := url.Parse(n.url)
		if err != nil || printed[urlKey(u)] || !tierAfter(primary.tier, n.tier) {
			continue
		}
		if best == nil || n.tier < best.tier || n.tier == best.tier && n.rank < best.rank {
			best = &cands[i]
		}
	}
	return best
}
