package docs

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/failure"
)

// Follow-up fetch (ADR-0007, amendment 4). An answer prints pieces of its
// sections, and the text it leaves out can be the one example a how-to
// question needs. The full section is in the local index, so reading it
// costs no quota: SectionSelect returns one section by the reference a
// result carries (url#anchor), with no ranking and no model. A result that
// was cut ends with a pointer to it (SectionHint). The search backend
// checks the page as an answer's cited pages are checked, and reads a page
// no index holds (one a question read) through the page cache
// (SearchFirst.SectionSelect).

// ErrSectionNotFound is a section reference the local docs index does not
// hold: a page it has no sections for, or an anchor that page lacks.
var ErrSectionNotFound = failure.New(failure.NotFound, "not_found", "section not found in the local docs index")

// errSectionRef is a section reference that is no http(s) URL.
var errSectionRef = failure.New(failure.Validation, "invalid_section", "a section is a URL with an optional #anchor, as a result's source carries it")

// localSectionTokens is the default cap of a fetched section: room for a
// long guide section, well under a page.
const localSectionTokens = 4000

// HintStyle is how a surface words the pointer from a cut result to its
// full section.
type HintStyle int

const (
	HintNone HintStyle = iota // no pointer, and no tokens spent on it
	HintCLI                   // a ketch docs command line
	HintMCP                   // the arguments of a docs tool call
)

// SectionCall is the call that fetches the section ref in style's words
// (a command line for HintNone as well).
func SectionCall(ref string, style HintStyle) string {
	if style == HintMCP {
		return `docs(section="` + ref + `")`
	}
	return "ketch docs --section " + ref
}

// SectionHint is the pointer a result ends with when it was cut:
// one line, "" for a result shown whole or when style asks for none.
func SectionHint(r Result, style HintStyle) string {
	if style == HintNone || !r.Truncated || r.Section == "" {
		return ""
	}
	return "full section: " + SectionCall(r.Section, style)
}

// RenderResult is a result as RenderSnippet renders it, with its pointer to
// the full section after the source line when it was cut.
func RenderResult(r Result, style HintStyle) string {
	text := RenderSnippet(r)
	if h := SectionHint(r, style); h != "" {
		text += h + "\n"
	}
	return text
}

// SectionFetcher is a backend that returns one section in full.
type SectionFetcher interface {
	// SectionSelect returns the section ref names (url#anchor; a page URL
	// alone names the page's sections): whole when it fits within tokens,
	// else its page'th page of that size (0 is the first). library names
	// the indexed library, or is empty when ref's URL says which.
	SectionSelect(ctx context.Context, library, ref string, tokens, page int) Outcome
}

// SectionSelect reads one section of an indexed library from the index:
// the section ref names with the subsections under it (an H2 is its
// family), or the page's sections in order when ref has no anchor, whole
// when it fits within tokens (localSectionTokens when 0), else read in
// pages of that size (fetchSection), pageNo the one returned (0 the
// first). A reference the index does not hold is not_found; a library it
// does not hold, not_indexed; a page past the last, a validation error.
func (l *Local) SectionSelect(ctx context.Context, library, ref string, tokens, pageNo int) Outcome {
	o, _, _ := l.sectionRead(ctx, library, ref, tokens, pageNo)
	return o
}

// sectionRead is SectionSelect, with the library that holds the page and
// the address it stores the page under; a zero library when none holds
// it.
func (l *Local) sectionRead(ctx context.Context, library, ref string, tokens, pageNo int) (Outcome, docsindex.Library, string) {
	page, anchor, err := parseSectionRef(ref)
	if err != nil {
		return Answer(SearchBackend, nil, err), docsindex.Library{}, ""
	}
	libs, err := l.libraries()
	if err != nil {
		return Answer(SearchBackend, nil, err), docsindex.Library{}, ""
	}
	cands, note := librariesHolding(libs, page), ""
	if strings.TrimSpace(library) != "" {
		var lib docsindex.Library
		if lib, note, err = pickVersion(libs, library); err != nil {
			return Answer(SearchBackend, nil, err), docsindex.Library{}, ""
		}
		cands = []docsindex.Library{lib}
	}
	opts, _ := localOptionsFrom(ctx)
	for _, lib := range cands {
		if !lib.Current() {
			return Answer(SearchBackend, nil, outdatedErr(lib)), docsindex.Library{}, ""
		}
		secs, err := l.pageSections(lib, page)
		if err != nil {
			return Answer(SearchBackend, nil, fmt.Errorf("local: reading the docs index: %w", err)), docsindex.Library{}, ""
		}
		if len(secs) == 0 {
			continue
		}
		o := fetchSection(lib, secs, anchor, library, tokens, pageNo, opts.Hint)
		if o.Err() == nil {
			o.Library, o.Index = lib.Root, indexInfo(lib)
		}
		return withVersionNote(o, note), lib, secs[0].Page
	}
	if strings.TrimSpace(library) != "" {
		return Answer(SearchBackend, nil, fmt.Errorf("local: the index of %s has no page %s: %w", cands[0].Root, page, ErrSectionNotFound)), docsindex.Library{}, ""
	}
	return Answer(SearchBackend, nil, fmt.Errorf("local: no indexed library holds %s (ketch docs index --list): %w", page, ErrSectionNotFound)), docsindex.Library{}, ""
}

// parseSectionRef splits a section reference into its page URL and its
// anchor (unescaped, "" for a page).
func parseSectionRef(ref string) (page, anchor string, err error) {
	ref = strings.TrimSpace(ref)
	page, frag, _ := strings.Cut(ref, "#")
	if !isHTTPURL(page) {
		return "", "", fmt.Errorf("local: %q is not a section reference: %w", ref, errSectionRef)
	}
	if anchor, err = url.PathUnescape(frag); err != nil {
		anchor = frag
	}
	return page, anchor, nil
}

// librariesHolding lists the indexed libraries whose root (or the
// unversioned address it is shown under) contains page, the most specific
// root first.
func librariesHolding(libs []docsindex.Library, page string) []docsindex.Library {
	u, err := url.Parse(page)
	if err != nil {
		return nil
	}
	type held struct {
		lib  docsindex.Library
		root int
	}
	var out []held
	for _, l := range libs {
		for _, root := range []string{l.Root, l.DisplayRoot} {
			if r, err := parseWebRoot(root); root != "" && err == nil && r.contains(u) {
				out = append(out, held{l, len(root)})
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].root > out[j].root })
	var libsOut []docsindex.Library
	for _, h := range out {
		libsOut = append(libsOut, h.lib)
	}
	return libsOut
}

// pageSections reads the sections of page from lib's index: under the
// address given, the one it is stored under when the library is shown at
// unversioned addresses, then with a trailing slash or .html toggled.
func (l *Local) pageSections(lib docsindex.Library, page string) ([]docsindex.Section, error) {
	bases := []string{page}
	if rest, ok := strings.CutPrefix(page, lib.DisplayRoot); ok && lib.DisplayRoot != "" {
		bases = append(bases, lib.Root+rest)
	}
	tries := slices.Clone(bases)
	for _, b := range bases {
		t := strings.TrimSuffix(b, "/")
		tries = append(tries, t, t+"/", strings.TrimSuffix(t, ".html"), t+".html")
	}
	seen := map[string]bool{}
	for _, p := range tries {
		if seen[p] {
			continue
		}
		seen[p] = true
		if secs, err := l.store.PageSections(lib.Key, p); err != nil || len(secs) > 0 {
			return secs, err
		}
	}
	return nil, nil
}

// shownURL is u (an address under lib's root) as results show it: under
// the unversioned address a re-rooted current release is shown at.
func shownURL(lib docsindex.Library, u string) string {
	if lib.DisplayRoot != "" {
		if rest, ok := strings.CutPrefix(u, lib.Root); ok {
			return lib.DisplayRoot + rest
		}
	}
	return u
}

// sectionRef is a stored section's reference as results show it.
func sectionRef(lib docsindex.Library, s docsindex.Section) string {
	return shownURL(lib, webSectionOf(s, 0).URL())
}

// sectionUnit is what an anchor names among a page's sections (in order):
// the section and the subsections under it (an H2 is its family), a page's
// lead alone, or the whole page for no anchor. ok is false for an anchor
// the page lacks.
func sectionUnit(secs []docsindex.Section, anchor string) (unit []docsindex.Section, ok bool) {
	if anchor == "" {
		return secs, true
	}
	i := slices.IndexFunc(secs, func(s docsindex.Section) bool {
		return s.Anchor == anchor || (&url.URL{Fragment: s.Anchor}).EscapedFragment() == anchor
	})
	if i < 0 {
		return nil, false
	}
	depth := len(secs[i].Crumbs)
	end := i + 1
	for depth >= 2 && end < len(secs) && len(secs[end].Crumbs) > depth {
		end++
	}
	return secs[i:end], true
}

// fetchSection renders the unit anchor names as one result: whole when
// it fits within tokens, else the page'th (from 1; 0 is the first) of the
// pages it reads in at that size, in reading order, each ending at a block
// boundary (never inside a code block or a table row; a table continued
// from the page before repeats its header), with the call that reads the
// next one (Result.Next, in style's words; library and tokens as the
// caller gave them). A page past the last is a validation error naming
// how many there are.
func fetchSection(lib docsindex.Library, secs []docsindex.Section, anchor, library string, tokens, page int, style HintStyle) Outcome {
	unit, ok := sectionUnit(secs, anchor)
	if !ok {
		return Answer(SearchBackend, nil, fmt.Errorf("local: %s has no section #%s (sections: %s): %w", shownURL(lib, secs[0].Page), anchor, anchorList(secs), ErrSectionNotFound))
	}
	// Printed as an answer prints its pieces: cleaned (cleanSection).
	unit = slices.Clone(unit)
	for i := range unit {
		unit[i] = cleanSection(unit[i])
	}
	asked := tokens
	if tokens <= 0 {
		tokens = localSectionTokens
	}
	page = max(page, 1)
	head := unit[0]
	ref := shownURL(lib, secs[0].Page)
	if anchor != "" {
		ref = sectionRef(lib, head)
	}
	title, crumbs := head.Heading, strings.Join(distinctCrumbs(head.Crumbs), " > ")
	r := Result{Library: lib.Root, Version: lib.Version, Title: title, Breadcrumb: crumbs, URL: ref, Section: ref, Source: SearchBackend}
	// A page's room: the cap with the answers' margin, less what is
	// printed around the text (heading, source, page and next-page lines).
	room := tokens - tokens/localTokenMargin - estimateTokens(RenderSnippet(r)) - estimateTokens(pageLine(99, 99)+"\n"+nextPageLine(PageCall(ref, library, asked, 99, style))+"\n")
	chunks := sectionChunks(unit, title, room)
	pages := pageBreaks(chunks, room)
	whole := estimateTokens(RenderSnippet(r)) - 1
	for _, c := range chunks {
		whole += c.cost
	}
	if whole <= tokens {
		pages = [][]int{allChunks(chunks)}
	}
	if page > len(pages) {
		return Answer(SearchBackend, nil, fmt.Errorf("local: %s: page %d of %s at %d tokens a page: %w", ref, page, plural(len(pages), "page"), tokens, errSectionPage))
	}
	shown := pages[page-1]
	r.Snippet = joinPage(chunks, shown)
	for _, c := range shown {
		r.Anchors = appendAnchor(r.Anchors, unit[chunks[c].part].Anchor)
	}
	if len(pages) > 1 {
		r.Truncated, r.Page, r.Pages = true, page, len(pages)
		if page < len(pages) {
			r.Next = PageCall(ref, library, asked, page+1, style)
		}
	}
	return Answer(SearchBackend, []Result{r}, nil)
}

// distinctCrumbs is a heading path without a heading that repeats the
// one before it: a page whose title is its first heading's text
// ("4.1. Configuration file syntax") stores that text twice.
func distinctCrumbs(crumbs []string) []string {
	var out []string
	for _, c := range crumbs {
		if len(out) == 0 || out[len(out)-1] != c {
			out = append(out, c)
		}
	}
	return out
}

// errSectionPage is a page of a section past its last.
var errSectionPage = failure.New(failure.Validation, "invalid_page", "past the last page")

// plural is n with its noun: "1 page", "3 pages", "2 libraries".
func plural(n int, noun string) string {
	switch {
	case n == 1:
		return "1 " + noun
	case strings.HasSuffix(noun, "y") && !strings.ContainsAny(noun[max(len(noun)-2, 0):len(noun)-1], "aeiou"):
		return strconv.Itoa(n) + " " + noun[:len(noun)-1] + "ies"
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// PageCall is the call that reads page of the section ref in style's words
// (a command line for HintNone as well), with library and tokens when the
// caller gave them (tokens 0: the default).
func PageCall(ref, library string, tokens, page int, style HintStyle) string {
	p := strconv.Itoa(page)
	if style == HintMCP {
		call := `docs(section="` + ref + `"`
		if library != "" {
			call += `, library="` + library + `"`
		}
		if tokens > 0 && tokens != localSectionTokens {
			call += ", tokens=" + strconv.Itoa(tokens)
		}
		return call + ", page=" + p + ")"
	}
	call := "ketch docs --section " + ref
	if library != "" {
		call += " --library " + library
	}
	if tokens > 0 && tokens != localSectionTokens {
		call += " --tokens " + strconv.Itoa(tokens)
	}
	return call + " --page " + p
}

// pageLine is the line a page of a section fetch is headed by: "page: 2 of 4".
func pageLine(page, pages int) string {
	return "page: " + strconv.Itoa(page) + " of " + strconv.Itoa(pages)
}

// PageLine is r's page line ("page: 2 of 4"), "" for a section fetch of
// one page.
func PageLine(r Result) string {
	if r.Pages <= 1 {
		return ""
	}
	return pageLine(r.Page, r.Pages)
}

// nextPageLine is the last line of a page with one after it.
func nextPageLine(call string) string { return "next page: " + call }

// NextPageLine is r's last line: the call for its next page; "" for the
// last page or a section of one.
func NextPageLine(r Result) string {
	if r.Next == "" {
		return ""
	}
	return nextPageLine(r.Next)
}

// appendAnchor adds a section's anchor to a result's, once, in order.
func appendAnchor(anchors []string, a string) []string {
	if len(anchors) > 0 && anchors[len(anchors)-1] == a {
		return anchors
	}
	return append(anchors, a)
}

// anchorList names up to eight of a page's anchors, to help a caller that
// asked for one the page lacks.
func anchorList(secs []docsindex.Section) string {
	var out []string
	for _, s := range secs {
		if s.Anchor != "" {
			out = append(out, s.Anchor)
		}
	}
	if len(out) > 8 {
		out = append(out[:8], "…")
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

// chunk is a block of a unit's text a page can end after: a paragraph,
// a list, a code block whole, or a part's heading with its first block;
// a block longer than a page is cut into its lines (a table into its
// rows, a list into its items), code blocks whole.
type chunk struct {
	part   int // the unit's section it belongs to
	text   string
	sep    string // what joins it to the chunk before: a blank line, or a line break inside a block
	header string // a table row's header and delimiter rows, repeated when a page opens on it
	cost   int
}

// sectionChunks cuts a unit into chunks in order; a part's heading
// ("#### ", but the title's own) leads its first block. A block over room
// tokens is cut into its lines (blockLines).
func sectionChunks(unit []docsindex.Section, title string, room int) []chunk {
	var out []chunk
	for p, s := range unit {
		blocks := textBlocks(s.Text)
		lead := ""
		if p > 0 || s.Heading != title {
			if len(blocks) == 0 {
				blocks = []string{""}
			}
			lead = "#### " + s.Heading + "\n\n"
		}
		for i, b := range blocks {
			parts, header := []string{b}, ""
			if estimateTokens(b) > room {
				parts, header = blockLines(b)
			}
			for k, t := range parts {
				c := chunk{part: p, text: t, sep: "\n\n"}
				if k > 0 {
					c.sep, c.header = "\n", header
				}
				if i == 0 && k == 0 {
					c.text = strings.TrimRight(lead+t, "\n")
				}
				c.cost = estimateTokens(c.text) + 1
				out = append(out, c)
			}
		}
	}
	return out
}

// blockLines cuts a block into the runs of lines a page may end between,
// joined by line breaks: a line not indented starts a run (a list item,
// a table row, a paragraph's line), an indented one continues the run
// before it, and a fenced code block is one run with whatever leads into
// it. A table's header and delimiter rows lead its first row, and are
// returned as its header.
func blockLines(b string) (runs []string, header string) {
	lines := strings.Split(b, "\n")
	if len(lines) > 2 && strings.HasPrefix(strings.TrimSpace(lines[0]), "|") && delimiterRowRe.MatchString(lines[1]) {
		header = lines[0] + "\n" + lines[1]
		lines = append([]string{header + "\n" + lines[2]}, lines[3:]...)
	}
	var fence fenceState
	for i, line := range lines {
		inFence := fence.n > 0
		fence.step(line)
		continues := inFence || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") && !isListLine(line)
		if i == 0 || continues {
			if i == 0 {
				runs = append(runs, line)
			} else {
				runs[len(runs)-1] += "\n" + line
			}
			continue
		}
		runs = append(runs, line)
	}
	return runs, header
}

// pageBreaks parts the chunks into pages of at most room tokens each, in
// order, each holding at least one chunk; a page that opens on a table's
// row counts its repeated header.
func pageBreaks(chunks []chunk, room int) [][]int {
	var pages [][]int
	used := 0
	for i, c := range chunks {
		cost := c.cost
		if len(pages) == 0 || used+cost > room {
			if c.header != "" {
				cost += estimateTokens(c.header) + 1
			}
			if len(pages) == 0 || len(pages[len(pages)-1]) > 0 {
				pages = append(pages, nil)
				used = 0
			}
		}
		pages[len(pages)-1] = append(pages[len(pages)-1], i)
		used += cost
	}
	return pages
}

// allChunks is every chunk's index, in order: a section on one page.
func allChunks(chunks []chunk) []int {
	out := make([]int, len(chunks))
	for i := range out {
		out[i] = i
	}
	return out
}

// joinPage is the text of the chunks of one page (ids, in order): each
// joined to the one before as the text joins them, the first with its
// table's header when the page continues a table.
func joinPage(chunks []chunk, ids []int) string {
	var b strings.Builder
	for k, i := range ids {
		c := chunks[i]
		switch {
		case k > 0:
			b.WriteString(c.sep)
		case c.header != "":
			b.WriteString(c.header + "\n")
		}
		b.WriteString(c.text)
	}
	return b.String()
}
