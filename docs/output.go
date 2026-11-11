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
//	version: <the indexed version, when the library has one>
//	index: <full|capped|search>, <N> pages, built <date>   (or, for pages a search just read: source: search, N pages read)
//	stale: unverified, built <date>   (only when a cited page older than cache_ttl could not be read again)
//	status: found | partial (<reason>) | refused (<reason>)
//	terms: {<term as written>: <sections holding it>, ...}
//	not_found: [<term as written>, ...]
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
//	defined in: <term as written> → <url#anchor of a section defining it>
//
// Groups of one page print together under its title, pages in the order
// of their best group, a page's groups in rank order. The last line
// is printed once, when a group was cut, in the surface's words
// (HintStyle). The defined-in lines follow, one per listed term that no
// printed section defines (evidence_defined.go): pointers, like the hint
// line, so they come after the evidence and cost nothing before it. The
// budget covers everything printed.

// Index kinds, as the index line names them.
const (
	IndexFull   = "full"   // indexed by ketch docs index, every page discovery listed
	IndexCapped = "capped" // indexed by ketch docs index, the page cap leaving some out
	IndexSearch = "search" // pages earlier questions' web searches read, kept
)

// IndexInfo is what an answer was answered from: an indexed library
// (Kind, Pages, Built), or the pages a search just read (Source "search",
// PagesRead).
type IndexInfo struct {
	Kind      string `json:"kind,omitempty"`
	Pages     int    `json:"pages,omitempty"`
	Built     string `json:"built,omitempty"` // the build date, 2006-01-02
	Source    string `json:"source,omitempty"`
	PagesRead int    `json:"pages_read,omitempty"`
}

// indexInfo is lib as the index line names it.
func indexInfo(lib docsindex.Library) *IndexInfo {
	kind := IndexFull
	switch {
	case lib.Resolved == resolvedBySearch:
		kind = IndexSearch
	case lib.Capped:
		kind = IndexCapped
	}
	info := &IndexInfo{Kind: kind, Pages: lib.Pages}
	if !lib.Built.IsZero() {
		info.Built = lib.Built.UTC().Format("2006-01-02")
	}
	return info
}

// searchRead is the source of an answer from the pages a search just read.
func searchRead(pages int) *IndexInfo {
	return &IndexInfo{Source: SearchBackend, PagesRead: pages}
}

// line is the frontmatter line naming what was read: "index: full, 734
// pages, built 2026-11-10" or "source: search, 12 pages read".
func (i *IndexInfo) line() string {
	if i.Source != "" {
		return fmt.Sprintf("source: %s, %d pages read", i.Source, i.PagesRead)
	}
	s := fmt.Sprintf("index: %s, %d pages", i.Kind, i.Pages)
	if i.Built != "" {
		s += ", built " + i.Built
	}
	return s
}

// Frontmatter is an answer's frontmatter: what the CLI prints before the
// results and what --json and the docs tool carry as fields, in this
// order.
type Frontmatter struct {
	Query   string      `json:"query,omitempty"`
	Backend string      `json:"backend"`
	Library string      `json:"library,omitempty"`
	Version string      `json:"version,omitempty"`
	Index   *IndexInfo  `json:"index,omitempty"`
	Stale   string      `json:"stale,omitempty"`
	Status  Status      `json:"status,omitempty"`
	Reason  string      `json:"reason,omitempty"`
	Terms   []TermCount `json:"terms,omitzero"`
	// DefinedIn names, for a listed term that no printed section defines,
	// a section that does (evidence_defined.go). --json carries it here;
	// the text prints it after the body (DefinedInLines), not in the
	// frontmatter.
	DefinedIn []DefinedIn `json:"defined_in,omitempty"`
	NotFound  []string    `json:"not_found,omitzero"`
	Note      string      `json:"note,omitempty"`
}

// NewFrontmatter is the frontmatter of o, an answer to query from
// backend; library is the library the caller named, printed when no
// result names the one answered from.
func NewFrontmatter(query, backend, library string, o Outcome) Frontmatter {
	f := Frontmatter{Query: query, Backend: backend, Library: library, Index: o.Index, Stale: o.Stale, Status: o.Status, Reason: o.Reason, Note: o.Note}
	if len(o.Results) > 0 && o.Results[0].Library != "" {
		f.Library = o.Results[0].Library
	}
	if o.Library != "" && f.Library == "" {
		f.Library = o.Library
	}
	if backend == SearchBackend && len(o.Results) > 0 {
		f.Version = o.Results[0].Version
	}
	if o.Index != nil {
		f.Terms, f.NotFound, f.DefinedIn = nonNil(o.Terms), o.NotFound, o.DefinedIn
		if f.NotFound == nil {
			f.NotFound = []string{}
		}
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
	if f.Version != "" {
		b.WriteString("version: " + f.Version + "\n")
	}
	if f.Index != nil {
		b.WriteString(f.Index.line() + "\n")
	}
	if f.Stale != "" {
		b.WriteString("stale: " + f.Stale + "\n")
	}
	if f.Index != nil || f.Status == Partial {
		b.WriteString("status: " + string(f.Status))
		if f.Reason != "" {
			b.WriteString(" (" + f.Reason + ")")
		}
		b.WriteString("\n")
	}
	if f.Index != nil {
		b.WriteString("terms: " + termsFlow(f.Terms) + "\n")
		b.WriteString("not_found: " + listFlow(f.NotFound) + "\n")
	}
	if f.Note != "" {
		b.WriteString("note: " + f.Note + "\n")
	}
	b.WriteString("---\n")
	return b.String()
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

// DefinedInLines is what an answer prints after its body for defs: a
// blank line, then "defined in: <term> → <url#anchor>" for each; "" for
// none.
func DefinedInLines(defs []DefinedIn) string {
	if len(defs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, d := range defs {
		b.WriteString("defined in: " + d.Term + " → " + d.Section + "\n")
	}
	return b.String()
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
