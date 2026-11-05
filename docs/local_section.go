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

// Follow-up fetch (ADR-0007, amendment 4). A compact answer trims its
// results, and the text it drops can be the one example a how-to question
// needs. The full section is in the local index, so reading it costs no
// network and no quota: SectionSelect returns one section by the reference
// a result carries (url#anchor), with no ranking and no model. A compact
// result that was cut ends with a pointer to it (SectionHint).

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

// SectionHint is the pointer a compact result ends with when it was cut:
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
	// alone names the page's sections), capped at tokens. library names
	// the indexed library, or is empty when ref's URL says which.
	SectionSelect(ctx context.Context, library, ref string, tokens int) Outcome
}

// SectionSelect reads one section of an indexed library from the index:
// the section ref names with the subsections under it (an H2 is its
// family), or the page's sections in order when ref has no anchor, all cut
// at a block boundary within tokens (localSectionTokens when 0). A
// reference the index does not hold is not_found; a library it does not
// hold, not_indexed.
func (l *Local) SectionSelect(ctx context.Context, library, ref string, tokens int) Outcome {
	page, anchor, err := parseSectionRef(ref)
	if err != nil {
		return Answer(SearchBackend, nil, err)
	}
	libs, err := l.libraries()
	if err != nil {
		return Answer(SearchBackend, nil, err)
	}
	cands, note := librariesHolding(libs, page), ""
	if strings.TrimSpace(library) != "" {
		var lib docsindex.Library
		if lib, note, err = pickVersion(libs, library); err != nil {
			return Answer(SearchBackend, nil, err)
		}
		cands = []docsindex.Library{lib}
	}
	opts, _ := localOptionsFrom(ctx)
	for _, lib := range cands {
		if !lib.Current() {
			return Answer(SearchBackend, nil, outdatedErr(lib))
		}
		secs, err := l.pageSections(lib, page)
		if err != nil {
			return Answer(SearchBackend, nil, fmt.Errorf("local: reading the docs index: %w", err))
		}
		if len(secs) == 0 {
			continue
		}
		return withVersionNote(fetchSection(lib, secs, anchor, tokens, opts.Hint), note)
	}
	if strings.TrimSpace(library) != "" {
		return Answer(SearchBackend, nil, fmt.Errorf("local: the index of %s has no page %s: %w", cands[0].Root, page, ErrSectionNotFound))
	}
	return Answer(SearchBackend, nil, fmt.Errorf("local: no indexed library holds %s (ketch docs index --list): %w", page, ErrSectionNotFound))
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

// fetchSection renders the unit anchor names as one result, cut at a block
// boundary within tokens.
func fetchSection(lib docsindex.Library, secs []docsindex.Section, anchor string, tokens int, style HintStyle) Outcome {
	unit, ok := sectionUnit(secs, anchor)
	if !ok {
		return Answer(SearchBackend, nil, fmt.Errorf("local: %s has no section #%s (sections: %s): %w", shownURL(lib, secs[0].Page), anchor, anchorList(secs), ErrSectionNotFound))
	}
	if tokens <= 0 {
		tokens = localSectionTokens
	}
	head := unit[0]
	ref := shownURL(lib, secs[0].Page)
	if anchor != "" {
		ref = sectionRef(lib, head)
	}
	title, crumbs := head.Heading, strings.Join(head.Crumbs, " > ")
	r := Result{Library: lib.Root, Version: lib.Version, Title: title, Breadcrumb: crumbs, URL: ref, Section: ref, Source: SearchBackend}
	chunks := sectionChunks(unit, title)
	shown, cut := fitChunks(lib, r, unit, chunks, tokens, style)
	r.Snippet = joinChunks(chunks[:shown], cut)
	for _, c := range chunks[:shown] {
		r.Anchors = appendAnchor(r.Anchors, unit[c.part].Anchor)
	}
	r.Truncated = cut != ""
	return Answer(SearchBackend, []Result{r}, nil)
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

// chunk is a block of a unit's text a fetched section can end after: a
// paragraph, a list, a code block whole, or a part's heading with its
// first block.
type chunk struct {
	part int // the unit's section it belongs to
	text string
	cost int
}

// sectionChunks cuts a unit into chunks in order; a part's heading
// ("#### ", but the title's own) leads its first block.
func sectionChunks(unit []docsindex.Section, title string) []chunk {
	var out []chunk
	for p, s := range unit {
		blocks := textBlocks(s.Text)
		if p > 0 || s.Heading != title {
			if len(blocks) == 0 {
				blocks = []string{""}
			}
			blocks[0] = strings.TrimRight("#### "+s.Heading+"\n\n"+blocks[0], "\n")
		}
		for _, b := range blocks {
			out = append(out, chunk{part: p, text: b, cost: estimateTokens(b) + 1})
		}
	}
	return out
}

// fitChunks is how many chunks fit within tokens with r's rendering around
// them, and the notice that ends a cut section ("" when all fit). The
// planning keeps localTokenMargin under the cap, as compact answers do.
func fitChunks(lib docsindex.Library, r Result, unit []docsindex.Section, chunks []chunk, tokens int, style HintStyle) (int, string) {
	base := estimateTokens(RenderSnippet(r))
	total := base
	for _, c := range chunks {
		total += c.cost
	}
	if total <= tokens {
		return len(chunks), ""
	}
	limit := tokens - tokens/localTokenMargin
	whole := total - base
	used := base + estimateTokens(cutNotice(lib, unit, len(unit)-1, tokens, whole, style)) + 1
	n := 0
	for n < len(chunks) && used+chunks[n].cost <= limit {
		used += chunks[n].cost
		n++
	}
	if n == 0 {
		// One block larger than the whole cap: cut it, fence closed.
		room := max((limit-used)*3, webMinTrimBytes)
		chunks[0].text = strings.TrimSuffix(trimSnippet(chunks[0].text, room), truncatedMarker)
		n = 1
	}
	notice := func(n int) string {
		return cutNotice(lib, unit, chunks[min(n, len(chunks)-1)].part, tokens, whole, style)
	}
	// The per-chunk sums run a little under the joined text's.
	for n > 1 {
		r.Snippet = joinChunks(chunks[:n], notice(n))
		if estimateTokens(RenderSnippet(r)) <= tokens {
			break
		}
		n--
	}
	return n, notice(n)
}

// joinChunks is the chunks as paragraphs, then the cut notice if any.
func joinChunks(chunks []chunk, notice string) string {
	parts := make([]string, 0, len(chunks)+1)
	for _, c := range chunks {
		parts = append(parts, c.text)
	}
	if notice != "" {
		parts = append(parts, notice)
	}
	return strings.Join(parts, "\n\n")
}

// cutNotice ends a cut section: "[…]", the cap, and where to read on, the
// unit's next section not shown whole (its reference) or, when the cut is
// in the first, a larger cap.
func cutNotice(lib docsindex.Library, unit []docsindex.Section, part, tokens, whole int, style HintStyle) string {
	if part > 0 {
		ref := sectionRef(lib, unit[part])
		if unit[part].Anchor == "" {
			ref = shownURL(lib, unit[part].Page)
		}
		return elidedMarker + " cut at " + strconv.Itoa(tokens) + " tokens; next section: " + SectionCall(ref, style)
	}
	flag := "--tokens"
	if style == HintMCP {
		flag = "tokens"
	}
	return elidedMarker + " cut at " + strconv.Itoa(tokens) + " tokens; the section is about " + strconv.Itoa(whole) + ": raise " + flag
}
