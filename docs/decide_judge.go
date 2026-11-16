package docs

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/decide"
	"github.com/1broseidon/ketch/internal/docsindex"
)

// dLib is the pages a decided answer may print from, held in memory as a
// library: sectioned as an answer from pages a search read sections them,
// sorted into pieces, and gathered for the question's terms (the outline's
// order, not_on_pages, a clip's lines).
type dLib struct {
	l   *Local
	lib docsindex.Library
	s   *sorted
	g   *gathered
}

// buildDecidedLib holds cands' pages as one library and gives each page its
// sections (in page order) and passages.
func buildDecidedLib(query string, cands []*dPage) (*dLib, error) {
	pages := make([]webPage, len(cands))
	urls := make([]string, len(cands))
	at := map[string]int{}
	for i, p := range cands {
		pages[i], urls[i] = *p.page, p.page.URL
		at[p.page.URL] = i
	}
	root := hostRoot(cands[0].parsedURL())
	title, name := readTitle(root, nil)
	l, lib, err := readLibrary(root, title, name, pages)
	if err != nil {
		return nil, err
	}
	dl := &dLib{l: l, lib: lib}
	if err := dl.sort(urls, at, cands); err != nil {
		l.store.Close()
		return nil, fmt.Errorf("search: %w", err)
	}
	if dl.g, err = l.gather(lib, query, nil); err != nil {
		l.store.Close()
		return nil, err
	}
	markSearchRead(dl.g.written)
	return dl, nil
}

// sort sorts every section of the pages, in page and section order, into
// pieces, and gives each page its sections and passages.
func (dl *dLib) sort(urls []string, at map[string]int, cands []*dPage) error {
	secs, err := dl.l.store.SectionsOfPages(dl.lib.Key, urls)
	if err != nil {
		return err
	}
	for i := range secs {
		secs[i] = cleanSection(secs[i])
	}
	slices.SortStableFunc(secs, func(a, b docsindex.Section) int {
		return cmp.Or(cmp.Compare(at[a.Page], at[b.Page]), cmp.Compare(a.Order, b.Order))
	})
	dl.s = sortPool(secs, dl.lib.Root)
	if err := dl.l.outline(dl.lib, dl.s); err != nil {
		return err
	}
	for i, sec := range dl.s.secs {
		if k, ok := at[sec.Page]; ok {
			cands[k].secs = append(cands[k].secs, i)
		}
	}
	for _, p := range cands {
		p.passages = passagesOf(dl.s, p.secs)
	}
	return nil
}

// passagesOf are the passages of secs: in each section, consecutive pieces
// up to passageTokens, a piece larger than that one passage on its own,
// and a piece over bigBlockTokens cut into runs of its lines (pieceRuns).
func passagesOf(s *sorted, secs []int) []dPassage {
	var out []dPassage
	for _, i := range secs {
		var cur []int
		size := 0
		for _, id := range s.secs[i].pieces {
			t := s.pieces[id].tokens
			if runs := pieceRuns(s.pieces[id]); len(runs) > 0 {
				if len(cur) > 0 {
					out = append(out, dPassage{sec: i, pieces: cur})
					cur, size = nil, 0
				}
				for k := range runs {
					out = append(out, dPassage{sec: i, pieces: []int{id}, run: &runs[k]})
				}
				continue
			}
			if len(cur) > 0 && size+t > passageTokens {
				out = append(out, dPassage{sec: i, pieces: cur})
				cur, size = nil, 0
			}
			cur, size = append(cur, id), size+t
		}
		if len(cur) > 0 {
			out = append(out, dPassage{sec: i, pieces: cur})
		}
	}
	return out
}

// judge asks the page pass of every candidate and the passage pass of the
// first firstPassagePages together, labels the pages, then asks the
// passage pass of the other tier A and B pages.
func (c *dCall) judge(ctx context.Context, dl *dLib, cands []*dPage) error {
	first := cands[:min(len(cands), firstPassagePages)]
	answers := make([]decide.Response, len(cands))
	var wg sync.WaitGroup
	errs := make([]error, len(cands)+len(first))
	for i, p := range cands {
		wg.Go(func() { answers[i], errs[i] = c.ask(ctx, c.pageRequest(p)) })
	}
	for i, p := range first {
		wg.Go(func() { errs[len(cands)+i] = c.passagePass(ctx, dl, p) })
	}
	wg.Wait()
	if err := firstError(errs); err != nil {
		return err
	}
	releaseNamed := c.cut.hi(median(releaseAnswers(answers)))
	for i, p := range cands {
		c.label(p, answers[i], releaseNamed)
	}
	var later []*dPage
	for _, p := range cands[len(first):] {
		if p.tier == tierA || p.tier == tierB {
			later = append(later, p)
		}
	}
	errs = make([]error, len(later))
	for i, p := range later {
		wg.Go(func() { errs[i] = c.passagePass(ctx, dl, p) })
	}
	wg.Wait()
	for _, p := range cands {
		if p.judged != nil && p.probs != nil {
			p.judged.Passages, p.judged.TopPassages = len(p.probs), topProbs(p.probs)
		}
	}
	return firstError(errs)
}

func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func releaseAnswers(answers []decide.Response) []float64 {
	out := make([]float64, len(answers))
	for i, a := range answers {
		out[i] = a.Answers[qNamesRelease].Probability
	}
	return out
}

// median is the middle of xs, the mean of the two middle ones for an
// even count; 0 for none.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// label gives p its judgments, source label and tier from its page pass.
func (c *dCall) label(p *dPage, a decide.Response, releaseNamed bool) {
	kind := a.Answers[qKind]
	j := &PageJudgments{
		Documentation: a.Answers[qDocumentation].Probability, Makers: a.Answers[qMakers].Probability,
		NamesRelease: a.Answers[qNamesRelease].Probability, OtherRelease: a.Answers[qOtherRelease].Probability,
		Kind: kind.Choice, KindProbability: kind.Probabilities[kind.Choice],
	}
	p.judged = j
	base := c.cut.sourceLabel(j.Documentation, j.Makers, j.Kind)
	p.tier, p.label = c.cut.tier(base, releaseNamed, j.OtherRelease)
	p.otherRel = releaseNamed && c.cut.hi(j.OtherRelease)
}

// pageInput is the page pass's shared input: the question and the page.
type pageInput struct {
	DocsQuestion string    `json:"docs_question"`
	Page         pageState `json:"page"`
}

// pageState is a page as the page pass sends it: its address, title,
// outline (at most outlineMax headings, indented by level) and text from
// the top, about 8,000 tokens cut at a line.
type pageState struct {
	URL     string   `json:"url"`
	Title   string   `json:"title"`
	Outline []string `json:"outline"`
	Text    string   `json:"text"`
}

// pageRequest is the page pass of p.
func (c *dCall) pageRequest(p *dPage) decide.Request {
	pg := p.page
	outline := markdownOutline(pg.Markdown)
	if len(outline) > outlineMax {
		outline = outline[:outlineMax]
	}
	st := pageState{URL: pg.URL, Title: firstNonEmpty(pg.Title, pg.URL), Outline: outline, Text: cutAtLine(pg.Markdown, pageTextChars)}
	return decide.Request{Input: pageInput{DocsQuestion: c.query, Page: st}, Questions: pageQuestions}
}

// cutAtLine is text cut to at most n bytes at its last line break within
// them (at n when there is none).
func cutAtLine(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := strings.ToValidUTF8(text[:n], "")
	if i := strings.LastIndexByte(cut, '\n'); i >= 0 {
		return cut[:i]
	}
	return cut
}

// mdHeadingRe matches a Markdown heading line; mdHeadingLinkRe a link or
// image in it.
var (
	mdHeadingRe     = regexp.MustCompile(`^(#{1,6})\s+(.*\S)\s*$`)
	mdHeadingLinkRe = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
)

// markdownOutline is the headings of a page's Markdown outside code
// blocks, each indented two spaces a level below the first.
func markdownOutline(md string) []string {
	var out []string
	fence := false
	for line := range strings.SplitSeq(md, "\n") {
		t := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
		}
		if fence {
			continue
		}
		m := mdHeadingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if h := cleanOutlineHeading(m[2]); h != "" {
			out = append(out, strings.Repeat("  ", len(m[1])-1)+h)
		}
	}
	return out
}

// cleanOutlineHeading is a heading's text: links as their text, no
// escapes, pilcrows or bold marks, spaces collapsed.
func cleanOutlineHeading(h string) string {
	h = mdHeadingLinkRe.ReplaceAllString(h, "$1")
	h = strings.NewReplacer(`\`, "", "¶", "", "**", "").Replace(h)
	return strings.Trim(strings.Join(strings.Fields(h), " "), " #")
}

// passageInput is the passage pass's shared input: the question and the
// page's title and address.
type passageInput struct {
	DocsQuestion string      `json:"docs_question"`
	Page         passagePage `json:"page"`
}

type passagePage struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// passageItem is a passage as its question carries it.
type passageItem struct {
	Section string `json:"section"`
	Text    string `json:"text"`
}

// passagePass asks of every passage of p whether it helps answer the
// question, and sets p's probabilities and context.
func (c *dCall) passagePass(ctx context.Context, dl *dLib, p *dPage) error {
	if len(p.passages) == 0 {
		p.probs, p.context, p.unscored = []float64{}, c.cut.context(nil), "could not be scored: no passage to ask about"
		return nil
	}
	req := decide.Request{Input: passageInput{DocsQuestion: c.query, Page: passagePage{Title: firstNonEmpty(p.page.Title, p.page.URL), URL: p.page.URL}}}
	whole, unsent := 0, 0
	for k, ps := range p.passages {
		text := passageText(dl.s, ps)
		whole += len(text)
		if len(text) > passageSendChars {
			unsent += len(text) - passageSendChars
			text = sentText(text)
		}
		item := &decide.Item{Name: "passage", Value: passageItem{Section: sectionLabel(dl.s.secs[ps.sec], p.page.Title), Text: text}}
		req.Questions = append(req.Questions, decide.Question{Name: fmt.Sprintf("p%d", k), Kind: decide.YesNo,
			Instructions: helpsAsk, Item: item, TrueMeans: helpsTrue, FalseMeans: helpsFalse})
	}
	resp, err := c.ask(ctx, req)
	if err != nil {
		return err
	}
	probs := make([]float64, len(p.passages))
	for k := range p.passages {
		probs[k] = resp.Answers[fmt.Sprintf("p%d", k)].Probability
	}
	p.probs, p.context = probs, c.cut.context(probs)
	if unsent*unscoredShare > whole {
		p.unscored = fmt.Sprintf("could not be scored: %d%% of its text did not fit the passage questions", 100*unsent/max(whole, 1))
	}
	return nil
}

// unscoredShare: a page more than 1/unscoredShare of whose passage text
// was cut from the questions is not scored.
const unscoredShare = 10

// sectionLabel is a section's heading path, the page's title for a page
// without headings.
func sectionLabel(sec sortedSection, title string) string {
	var path []string
	for _, h := range append(slices.Clone(sec.Crumbs), sec.Heading) {
		if h = strings.TrimSpace(h); h != "" && (len(path) == 0 || path[len(path)-1] != h) {
			path = append(path, h)
		}
	}
	if len(path) == 0 {
		return title
	}
	return strings.Join(path, " > ")
}

// passageText is a passage's text as printed: its pieces (a table's rows
// under its header), or its run of lines inside the block's fences.
func passageText(s *sorted, ps dPassage) string {
	if ps.run != nil {
		return runsText(s.pieces[ps.pieces[0]].text, []lineRun{*ps.run})
	}
	return joinPieces(s, s.secs[ps.sec], ps.pieces)
}

// sentText is a passage's text cut at passageSendChars, at a space.
func sentText(t string) string {
	if len(t) <= passageSendChars {
		return t
	}
	cut := strings.ToValidUTF8(t[:passageSendChars], "")
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return cut + " …"
}
