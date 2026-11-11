package docs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Output (design/docs-evidence.md): the frontmatter carries the verdict,
// the body the evidence, verbatim. An answer from the local index (an
// indexed library, or the pages a search just read) prints:
//
//	---
//	query: <the question>
//	backend: search
//	library: <root>
//	scope: <a URL below the root the answer is limited to, when library named one>
//	version: <the indexed version, when the library has one>
//	index: <full|capped>, <N> pages, built <date>   (or, for pages a search read: source: search, N pages read[, cached])
//	stale: unverified, built <date>   (only when a cited page older than cache_ttl could not be read again)
//	status: found | partial (<reason>) | refused (<reason>)
//	terms: {<term as written>: <sections holding it>, ...}
//	not_found: [<term as written>, ...]
//	outside_scope: [<term as written>, ...]   (only with scope: terms the library holds, none of them under it)
//	defined_in: {<term as written>: <url#anchor of a section defining it>, ...}   (only when there is one)
//	note: <what a partial answer says in words, other indexed versions>
//	---
//	## <page title>
//
//	### <heading path below the page title>
//	<group line: rows shown of a table, where else the same text sits>
//
//	<verbatim text>
//
//	<contents line: what a cut group left out>
//	source: <url#anchor> (cut, <tokens shown> of <the section's> tokens)
//
//	full text of a group marked (cut): <the call that returns it>
//
// Groups of one page print together under its title, pages in the order
// of their best group, a page's groups in rank order. The last line
// is printed once, when a group was cut, in the surface's words
// (HintStyle). defined_in names, for each listed term that no printed
// section defines, a section that does (evidence_defined.go): a lead the
// caller steers by, so it is in the frontmatter. The budget covers
// everything printed.

// Index kinds, as the index line names them.
const (
	IndexFull   = "full"   // indexed by ketch docs index, every page discovery listed
	IndexCapped = "capped" // indexed by ketch docs index, the page cap leaving some out
)

// IndexInfo is what an answer was answered from: an indexed library
// (Kind, Pages, Built), or the pages a search read (Source "search",
// PagesRead, and Cached for a repeat answered from the question cache).
type IndexInfo struct {
	Kind      string `json:"kind,omitempty"`
	Pages     int    `json:"pages,omitempty"`
	Built     string `json:"built,omitempty"` // the build date, 2006-01-02
	Source    string `json:"source,omitempty"`
	PagesRead int    `json:"pages_read,omitempty"`
	Cached    bool   `json:"cached,omitempty"`
}

// indexInfo is lib as the index line names it.
func indexInfo(lib docsindex.Library) *IndexInfo {
	kind := IndexFull
	if lib.Capped {
		kind = IndexCapped
	}
	info := &IndexInfo{Kind: kind, Pages: lib.Pages}
	if !lib.Built.IsZero() {
		info.Built = lib.Built.UTC().Format("2006-01-02")
	}
	return info
}

// searchRead is the source of an answer from the pages a search read,
// cached when a repeat read them from the question cache.
func searchRead(pages int, cached bool) *IndexInfo {
	return &IndexInfo{Source: SearchBackend, PagesRead: pages, Cached: cached}
}

// line is the frontmatter line naming what was read: "index: full, 734
// pages, built 2026-11-10", "source: search, 12 pages read", or for a
// repeat from the question cache "source: search, 12 pages read, cached".
func (i *IndexInfo) line() string {
	if i.Source != "" {
		s := fmt.Sprintf("source: %s, %s read", i.Source, plural(i.PagesRead, "page"))
		if i.Cached {
			s += ", cached"
		}
		return s
	}
	return "index: " + i.Summary()
}

// Summary is what an indexed library holds, as its index line says it:
// "full, 734 pages, built 2026-11-10".
func (i *IndexInfo) Summary() string {
	s := fmt.Sprintf("%s, %d pages", i.Kind, i.Pages)
	if i.Built != "" {
		s += ", built " + i.Built
	}
	return s
}

// Frontmatter is an answer's frontmatter: what the CLI prints before the
// results and what --json and the docs tool carry as fields, in this
// order.
type Frontmatter struct {
	Query   string `json:"query,omitempty"`
	Backend string `json:"backend"`
	Library string `json:"library,omitempty"`
	// Scope is the URL below the library's root the answer was limited
	// to: a library URL deeper than an indexed root.
	Scope   string      `json:"scope,omitempty"`
	Version string      `json:"version,omitempty"`
	Index   *IndexInfo  `json:"index,omitempty"`
	Stale   string      `json:"stale,omitempty"`
	Status  Status      `json:"status,omitempty"`
	Reason  string      `json:"reason,omitempty"`
	Terms   []TermCount `json:"terms,omitzero"`
	// DefinedIn names, for a listed term that no printed section defines,
	// a section that does (evidence_defined.go): the defined_in line.
	DefinedIn []DefinedIn `json:"defined_in,omitempty"`
	NotFound  []string    `json:"not_found,omitzero"`
	// OutsideScope are the distinctive terms the library holds that no
	// section under Scope does: not in not_found, which means nowhere in
	// the library.
	OutsideScope []string `json:"outside_scope,omitempty"`
	// IndexedNear are the indexed libraries near a library value that
	// named none: the indexed_near line.
	IndexedNear []NearLibrary `json:"indexed_near,omitempty"`
	Note        string        `json:"note,omitempty"`
	// section marks a section fetch's frontmatter (SectionFrontmatter),
	// which prints its page line ("page: 2 of 4", page) where an answer's
	// prints its verdict.
	section bool
	page    string
}

// NewFrontmatter is the frontmatter of o, an answer to query from
// backend; library is the library the caller named, printed when no
// result names the one answered from.
func NewFrontmatter(query, backend, library string, o Outcome) Frontmatter {
	f := Frontmatter{Query: query, Backend: backend, Library: library, Scope: o.Scope, Index: o.Index, Stale: o.Stale, Status: o.Status, Reason: o.Reason, Note: o.Note, IndexedNear: o.IndexedNear}
	if len(o.Results) > 0 && o.Results[0].Library != "" {
		f.Library = o.Results[0].Library
	}
	if o.Library != "" && (f.Library == "" || o.Index != nil) {
		f.Library = o.Library // the root an answer from the index was made from, not the value named
	}
	if backend == SearchBackend && len(o.Results) > 0 {
		f.Version = o.Results[0].Version
	}
	if o.Index != nil {
		f.Terms, f.NotFound, f.DefinedIn, f.OutsideScope = nonNil(o.Terms), o.NotFound, o.DefinedIn, o.OutsideScope
		if f.NotFound == nil {
			f.NotFound = []string{}
		}
	}
	return f
}

// SectionFrontmatter is the frontmatter of o, a section fetch (--section,
// the docs tool's section) from backend: what an answer's says of its
// source (backend, library, version, index or source, stale) and, for a
// section read a page at a time, its page line in place of the verdict;
// library is the library the caller named, printed when no result names
// the one read from. Its JSON fields are an answer's, without terms or
// not_found.
func SectionFrontmatter(backend, library string, o Outcome) Frontmatter {
	f := NewFrontmatter("", backend, library, o)
	f.Terms, f.NotFound, f.DefinedIn, f.OutsideScope, f.Reason = nil, nil, nil, nil, ""
	f.section = true
	if len(o.Results) == 1 {
		f.page = PageLine(o.Results[0])
	}
	return f
}

// AnswerJSON is an answer as ketch docs --json prints it: the
// frontmatter's fields, then the results in printed order.
type AnswerJSON struct {
	Frontmatter
	Results []Result `json:"results"`
}

func nonNil(t []TermCount) []TermCount {
	if t == nil {
		return []TermCount{}
	}
	return t
}

// Render is the frontmatter as printed, its two fences included. An
// answer from the local index (one with Index) has every line; any other
// has the lines it had before the evidence pipeline: no index, terms or
// not_found, the status only when partial.
func (f Frontmatter) Render() string {
	var b strings.Builder
	b.WriteString("---\n")
	if f.Query != "" {
		b.WriteString("query: " + f.Query + "\n")
	}
	b.WriteString("backend: " + f.Backend + "\n")
	if f.Library != "" {
		b.WriteString("library: " + f.Library + "\n")
	}
	if f.Scope != "" {
		b.WriteString("scope: " + f.Scope + "\n")
	}
	if f.Version != "" {
		b.WriteString("version: " + f.Version + "\n")
	}
	if f.Index != nil {
		b.WriteString(f.Index.line() + "\n")
	}
	if f.Stale != "" {
		b.WriteString("stale: " + f.Stale + "\n")
	}
	if f.section {
		if f.page != "" {
			b.WriteString(f.page + "\n")
		}
	} else {
		f.writeVerdict(&b)
	}
	b.WriteString(nearLine(f.IndexedNear))
	if f.Note != "" {
		b.WriteString("note: " + f.Note + "\n")
	}
	b.WriteString("---\n")
	return b.String()
}

// writeVerdict writes an answer's verdict lines: its status (an answer
// from the local index, or a partial one), then for an answer from the
// index its terms, not_found, outside_scope and defined_in.
func (f Frontmatter) writeVerdict(b *strings.Builder) {
	if f.Index != nil || f.Status == Partial {
		b.WriteString("status: " + string(f.Status))
		if f.Reason != "" {
			b.WriteString(" (" + f.Reason + ")")
		}
		b.WriteString("\n")
	}
	if f.Index == nil {
		return
	}
	b.WriteString("terms: " + termsFlow(f.Terms) + "\n")
	b.WriteString("not_found: " + listFlow(f.NotFound) + "\n")
	if len(f.OutsideScope) > 0 {
		b.WriteString("outside_scope: " + listFlow(f.OutsideScope) + "\n")
	}
	b.WriteString(definedInLine(f.DefinedIn))
}

// termsFlow is terms as a YAML flow mapping: {failmode: 74, pam_duo: 48}.
func termsFlow(terms []TermCount) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = yamlScalar(t.Term) + ": " + strconv.Itoa(t.Sections)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// listFlow is a YAML flow sequence: [pam_sss, PAM_SERVICE_ERR].
func listFlow(items []string) string {
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = yamlScalar(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// yamlScalar is s as a plain YAML scalar inside a flow collection, or
// double-quoted when it holds what would end or change one.
func yamlScalar(s string) string {
	if s == "" || strings.ContainsAny(s, ",[]{}:#\"'&*!|>%@`\\") || strings.TrimSpace(s) != s {
		return strconv.Quote(s)
	}
	return s
}

// nearLine is the frontmatter's indexed_near line, a YAML flow mapping
// from each near library's title to its root: "indexed_near: {Django
// Project: https://docs.djangoproject.com/en/6.1/}"; a library without a
// title, or one whose title another shares, goes by its root without the
// scheme. "" for none.
func nearLine(near []NearLibrary) string {
	if len(near) == 0 {
		return ""
	}
	titles := map[string]int{}
	for _, n := range near {
		titles[n.Title]++
	}
	parts := make([]string, len(near))
	for i, n := range near {
		key := n.Title
		if key == "" || titles[key] > 1 {
			key = strings.TrimPrefix(strings.TrimPrefix(n.Library, "https://"), "http://")
		}
		parts[i] = yamlScalar(key) + ": " + refScalar(n.Library)
	}
	return "indexed_near: {" + strings.Join(parts, ", ") + "}\n"
}

// NearHint is the line an answer ends with when its library value named
// no indexed library and some are near (indexed_near), in style's words;
// "" for HintNone.
func NearHint(style HintStyle) string {
	switch style {
	case HintCLI:
		return "library not indexed under that name: pass one of the indexed_near roots as --library"
	case HintMCP:
		return "library not indexed under that name: pass one of the indexed_near roots as library"
	}
	return ""
}

// AnswerHint is the last line of an answer with a group cut, in style's
// words: how to get a cut group's full text; "" for HintNone.
func AnswerHint(style HintStyle) string {
	switch style {
	case HintCLI:
		return "full text of a group marked (cut): ketch docs --section <its source>"
	case HintMCP:
		return `full text of a group marked (cut): docs(section="<its source>")`
	}
	return ""
}

// AnyCut reports a result that was cut: an answer that ends with its
// hint line (AnswerHint).
func AnyCut(results []Result) bool {
	for _, r := range results {
		if r.Truncated {
			return true
		}
	}
	return false
}

// RenderAnswer is an answer's body as printed: its groups under their
// pages' titles (results in printed order: a page's groups together),
// then the hint line in style's words when a group was cut.
func RenderAnswer(results []Result, style HintStyle) string {
	var b strings.Builder
	page := ""
	for i, r := range results {
		first := i == 0 || pageOf(r) != page
		if first {
			if i > 0 {
				b.WriteString("\n")
			}
			page = pageOf(r)
			b.WriteString(pageHeading(r))
		} else {
			b.WriteString("\n")
		}
		b.WriteString(RenderGroup(r, first))
	}
	if h := AnswerHint(style); h != "" && AnyCut(results) {
		b.WriteString("\n" + h + "\n")
	}
	return b.String()
}

// definedInLine is the frontmatter's defined_in line for defs, a YAML
// flow mapping from each term as written to its section's url#anchor:
// "defined_in: {failmode: https://duo.com/docs/duounix#duo-configuration-options}";
// "" for none.
func definedInLine(defs []DefinedIn) string {
	if len(defs) == 0 {
		return ""
	}
	parts := make([]string, len(defs))
	for i, d := range defs {
		parts[i] = yamlScalar(d.Term) + ": " + refScalar(d.Section)
	}
	return "defined_in: {" + strings.Join(parts, ", ") + "}\n"
}

// refScalar is a url#anchor as a plain YAML scalar inside a flow
// collection (its colon and hash are not followed or preceded by a
// space), or double-quoted when it holds what would end one.
func refScalar(s string) string {
	if s == "" || strings.ContainsAny(s, ",[]{}\"' \t") {
		return strconv.Quote(s)
	}
	return s
}

// pageOf is a result's page: its URL without the anchor.
func pageOf(r Result) string {
	page, _, _ := strings.Cut(r.URL, "#")
	return page
}

// pageHeading is the line a page's groups print under, with the blank
// line after it.
func pageHeading(r Result) string {
	title := r.PageTitle
	if title == "" {
		title = pageOf(r)
	}
	return "## " + title + "\n\n"
}

// topOfPage heads a page's lead section printed after another group of
// the page: it has no heading below the page's title.
const topOfPage = "(top of page)"

// RenderGroup is one group as printed under its page's title: its
// heading path below the title ("### a > b"; a page's lead section has
// none, or topOfPage when it is not the page's first group), its group
// line, its text, its contents line and its source line, with how much
// of its section it shows when it was cut (cutNote).
func RenderGroup(r Result, firstOfPage bool) string {
	var b strings.Builder
	switch {
	case len(r.HeadingPath) > 0:
		b.WriteString("### " + strings.Join(r.HeadingPath, " > ") + "\n")
	case !firstOfPage:
		b.WriteString("### " + topOfPage + "\n")
	}
	if line := r.GroupLine(); line != "" {
		b.WriteString(line + "\n")
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString(r.Snippet + "\n\n")
	if r.Contents != "" {
		b.WriteString(r.Contents + "\n")
	}
	b.WriteString("source: " + r.URL)
	if r.Truncated {
		b.WriteString(cutNote(r))
	}
	b.WriteString("\n")
	return b.String()
}

// cutNote is what a cut group's source line ends with: " (cut, 310 of
// 1,640 tokens)", how much of its section it shows; " (cut)" when that is
// not known.
func cutNote(r Result) string {
	if r.SectionTokens <= 0 {
		return " (cut)"
	}
	return " (cut, " + groupDigits(r.ShownTokens) + " of " + groupDigits(r.SectionTokens) + " tokens)"
}

// groupDigits is n with its thousands set apart by commas: 1,640.
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + groupDigits(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// RowCount is how many of a table's rows a cut group shows: "option
// rows, 2 of 17".
type RowCount struct {
	Kind  string `json:"kind"` // "option row" or "row"
	Shown int    `json:"shown"`
	Of    int    `json:"of"`
}

// SameText is where else a group's text sits: how many other sections,
// and up to three of them by heading.
type SameText struct {
	Count    int      `json:"count"`
	Sections []string `json:"sections"`
}

// GroupLine is the line under a group's heading: its RowCounts and its
// SameText, joined by " · "; "" for none.
func (r Result) GroupLine() string {
	var parts []string
	for _, rc := range r.Rows {
		parts = append(parts, fmt.Sprintf("%ss, %d of %d", rc.Kind, rc.Shown, rc.Of))
	}
	if st := r.SameText; st != nil && st.Count > 0 {
		noun := "sections"
		if st.Count == 1 {
			noun = "section"
		}
		parts = append(parts, fmt.Sprintf("same text in %d other %s: %s", st.Count, noun, strings.Join(st.Sections, "; ")))
	}
	return strings.Join(parts, " · ")
}
