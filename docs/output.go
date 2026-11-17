package docs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Output (design/docs-evidence.md, Final shape): the frontmatter says
// what was read and what is missing, never a verdict; the body is the
// docs' own text, then what was not printed and the other search results,
// then one line of next steps. An answer from the local index (an indexed
// library, or the pages a search just read) prints:
//
//	---
//	query: <the question>
//	library: <root>   (an indexed answer, or a library the request named)
//	scope: <a URL below the root the answer is limited to, when library named one>
//	version: <the version, when the library or release read has one>
//	index: <full|capped>, <N> pages, built <date>   (indexed answers)
//	read:   (pages a search read, best first)
//	  - <url> (search rank <n>)
//	skipped:   (search hits that could not be read)
//	  - <url> (<cause>)
//	widened: read <n> more pages of <root> for: <terms>   (only when it widened)
//	not_on_pages: [<terms on none of the pages read>]   (not_in_index for an indexed library)
//	outside_scope: [<term>, ...]   (only with scope: terms the library holds, none of them under it)
//	origin: verified, <evidence> | unverified, …   (pages a search read; unverified only for a named library)
//	stale: unverified, built <date>   (a cited page older than cache_ttl could not be read again)
//	defined_in: {<term as written>: <url#anchor of a section defining it>, ...}
//	note: <why only search results print; other indexed versions>
//	---
//	## <page title>
//	<page url>
//
//	### <heading path below the page title>
//	<group line: rows shown of a table, where else the same text sits>
//
//	<verbatim text>
//
//	<contents line: what a cut group left out>
//	source: <url#anchor> (cut, <tokens shown> of <the section's> tokens)
//
//	## Not printed
//	- <heading path> — <url#anchor> (<N> tokens)
//	(+<n> more: ketch docs --section <page url>)
//
//	## Other results
//	- <title> — <url> — <snippet>
//
//	<one line: how to read what is unread>
//
// Groups of one page print together under its title and address, pages
// best first, a page's groups in page order. defined_in names, for each
// rare term that no printed section defines, a section that does
// (evidence_defined.go). The budget covers the passages; the rest is
// bounded (about 600 tokens).

// Index kinds, as the index line names them.
const (
	IndexFull   = "full"   // indexed by ketch docs index, every page discovery listed
	IndexCapped = "capped" // indexed by ketch docs index, the page cap leaving some out
)

// IndexInfo is the indexed library an answer was answered from.
type IndexInfo struct {
	Kind  string `json:"kind,omitempty"`
	Pages int    `json:"pages,omitempty"`
	Built string `json:"built,omitempty"` // the build date, 2006-01-02
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

// Summary is what an indexed library holds, as its index line says it:
// "full, 734 pages, built 2026-11-10".
func (i *IndexInfo) Summary() string {
	s := fmt.Sprintf("%s, %d pages", i.Kind, i.Pages)
	if i.Built != "" {
		s += ", built " + i.Built
	}
	return s
}

// PageRead is a page an answer from a search read: its address and its
// rank among the search results (1 the first), or, for a page that is no
// search hit, why it was read (Via).
type PageRead struct {
	URL  string `json:"url"`
	Rank int    `json:"rank,omitempty"`
	Via  string `json:"via,omitempty"`
	// A decided answer (decide_flow.go) says what each page is and how far
	// to trust what it prints: its source label and confidence in words,
	// and in JSON the tier, the page's context from its passages and the
	// raw judgments behind them.
	Label      string         `json:"label,omitempty"`
	Confidence string         `json:"confidence,omitempty"`
	Tier       string         `json:"tier,omitempty"`
	Context    string         `json:"context,omitempty"`
	Judgments  *PageJudgments `json:"judgments,omitempty"`
}

// line is the page as its read line names it.
func (p PageRead) line() string {
	switch {
	case p.Label != "" && p.Rank == 0 && p.Via != "":
		return fmt.Sprintf("%s (%s, confidence %s, %s)", p.URL, p.Label, p.Confidence, p.Via)
	case p.Label != "":
		return fmt.Sprintf("%s (%s, confidence %s, search rank %d)", p.URL, p.Label, p.Confidence, p.Rank)
	case p.Rank > 0:
		return fmt.Sprintf("%s (search rank %d)", p.URL, p.Rank)
	case p.Via != "":
		return p.URL + " (" + p.Via + ")"
	}
	return p.URL
}

// PageSkipped is a search hit an answer could not read, and why.
type PageSkipped struct {
	URL   string `json:"url"`
	Cause string `json:"cause"`
}

// Widening is what an answer read beside the exact pages when they held
// none of some of the question's terms: how many more pages, of which
// docs root, for which terms.
type Widening struct {
	Pages int      `json:"pages"`
	Root  string   `json:"root"`
	Terms []string `json:"terms"`
}

// line is the widening as its frontmatter line says it: beside the exact
// pages (exact), for the terms they lack; else in their place.
func (w Widening) line(exact bool) string {
	if !exact || len(w.Terms) == 0 {
		return fmt.Sprintf("read %s of %s, as no search hit on it could be read", plural(w.Pages, "page"), w.Root)
	}
	return fmt.Sprintf("read %s of %s for: %s", plural(w.Pages, "more page"), w.Root, strings.Join(w.Terms, ", "))
}

// OtherResult is a search hit an answer did not read.
type OtherResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Rank    int    `json:"rank"`
	// Label is a decided answer's source label for the result, from
	// Judgments.
	Label     string           `json:"label,omitempty"`
	Judgments *ResultJudgments `json:"judgments,omitempty"`
}

// Frontmatter is an answer's frontmatter: what the CLI prints before the
// results and what --json and the docs tool carry as fields, in this
// order.
type Frontmatter struct {
	Query string `json:"query,omitempty"`
	// Backend is printed for a backend other than search (Context7).
	Backend string `json:"backend,omitempty"`
	Library string `json:"library,omitempty"`
	// Scope is the URL below the library's root the answer was limited
	// to: a library URL deeper than an indexed root.
	Scope   string        `json:"scope,omitempty"`
	Version string        `json:"version,omitempty"`
	Index   *IndexInfo    `json:"index,omitempty"`
	Read    []PageRead    `json:"read,omitempty"`
	Skipped []PageSkipped `json:"skipped,omitempty"`
	Widened *Widening     `json:"widened,omitempty"`
	// NotOnPages (pages a search read) and NotInIndex (an indexed
	// library) are the question's distinctive terms the answer's source
	// holds nowhere: only the pages read were checked for not_on_pages.
	NotOnPages []string `json:"not_on_pages,omitzero"`
	NotInIndex []string `json:"not_in_index,omitzero"`
	// OutsideScope are the distinctive terms the library holds that no
	// section under Scope does.
	OutsideScope []string `json:"outside_scope,omitempty"`
	// Origin is printed when it says something (ShownOrigin).
	Origin string `json:"origin,omitempty"`
	Stale  string `json:"stale,omitempty"`
	// DefinedIn names, for a rare term that no printed section defines,
	// a section that does (evidence_defined.go): the defined_in line.
	DefinedIn []DefinedIn `json:"defined_in,omitempty"`
	// IndexedNear are the indexed libraries near a library value that
	// named none: the indexed_near line.
	IndexedNear []NearLibrary `json:"indexed_near,omitempty"`
	Note        string        `json:"note,omitempty"`
	// Decisions names the decision provider a decided answer used, with
	// its requests' input tokens and cut-offs; PassedOver the results read
	// or tried and not printed, each with why; Next the calls that read a
	// better source or a cut section on the question.
	Decisions  *DecisionUse `json:"decisions,omitempty"`
	PassedOver []PagePassed `json:"passed_over,omitempty"`
	Next       []NextStep   `json:"next,omitempty"`
	// section marks a section fetch's frontmatter (SectionFrontmatter),
	// which prints its page line ("page: 2 of 4", page).
	section bool
	page    string
}

// NewFrontmatter is the frontmatter of o, an answer to query from
// backend; library is the library the caller named, printed when no
// result names the one answered from.
func NewFrontmatter(query, backend, library string, o Outcome) Frontmatter {
	f := Frontmatter{Query: query, Library: library, Scope: o.Scope, Index: o.Index, Stale: o.Stale, Note: o.Note, IndexedNear: o.IndexedNear}
	if backend != SearchBackend {
		f.Backend = backend
	}
	if len(o.Results) > 0 && o.Results[0].Library != "" && (o.Index != nil || library != "" || backend != SearchBackend) {
		f.Library = o.Results[0].Library
	}
	if o.Library != "" && (o.Index != nil || o.named != "") {
		f.Library = o.Library // the root an answer was made from, not the value named
	}
	if backend == SearchBackend && len(o.Results) > 0 {
		f.Version = o.Results[0].Version
	}
	f.Read, f.Skipped, f.Widened, f.DefinedIn, f.OutsideScope = o.Read, o.Skipped, o.Widened, o.DefinedIn, o.OutsideScope
	f.Decisions, f.PassedOver, f.Next = o.Decisions, o.PassedOver, o.Next
	f.Origin = ShownOrigin(o)
	switch {
	case o.Index != nil:
		f.NotInIndex = nonNil(o.NotFound)
	case len(o.Read) > 0 || o.Widened != nil:
		f.NotOnPages = nonNil(identTerms(o.NotFound))
	}
	return f
}

// identTerms are the terms of terms that are identifiers as the question
// writes them: code-like names (queryIdents: snake_case, dotted, CamelCase,
// calls), flags (--force-with-lease, -e) and host names. A plain word
// missing from a few pages is the docs' other wording; an identifier
// missing is worth a caller's next call.
func identTerms(terms []string) []string {
	var out []string
	for _, t := range terms {
		if len(queryIdents(t)) > 0 || isHostName(strings.ToLower(t)) || flagWord(t) {
			out = append(out, t)
		}
	}
	return out
}

// flagWord reports a command-line flag as written: -e, --force-with-lease.
func flagWord(w string) bool {
	rest := strings.TrimPrefix(strings.TrimPrefix(w, "-"), "-")
	return rest != w && rest != "" && (rest[0] >= 'a' && rest[0] <= 'z' || rest[0] >= 'A' && rest[0] <= 'Z')
}

// ShownOrigin is o's origin line when it says something: verified, with
// its evidence, or unverified when the request named a library; "" when
// only a name ties the site to the question, or nothing names one.
func ShownOrigin(o Outcome) string {
	switch {
	case o.Origin == "":
		return ""
	case OriginUnverified(o.Origin) && o.named == "":
		return ""
	}
	return o.Origin
}

// SectionFrontmatter is the frontmatter of o, a section fetch (--section,
// the docs tool's section) from backend: what an answer's says of its
// source (library, version, index, stale) and, for a section read a page
// at a time, its page line; library is the library the caller named,
// printed when no result names the one read from.
func SectionFrontmatter(backend, library string, o Outcome) Frontmatter {
	f := Frontmatter{Library: library, Index: o.Index, Stale: o.Stale, Note: o.Note, section: true}
	if backend != SearchBackend {
		f.Backend = backend
	}
	if len(o.Results) > 0 && o.Results[0].Library != "" {
		f.Library = o.Results[0].Library
	}
	if o.Library != "" {
		f.Library = o.Library
	}
	if len(o.Results) > 0 {
		f.Version = o.Results[0].Version
	}
	if len(o.Results) == 1 {
		f.page = PageLine(o.Results[0])
	}
	return f
}

// AnswerJSON is an answer as ketch docs --json prints it: the
// frontmatter's fields, the results in printed order, the outline of what
// was not printed, the other search results, and the hint: how to read
// what is unread (Hint).
type AnswerJSON struct {
	Frontmatter
	Results      []Result       `json:"results"`
	Outline      []OutlineEntry `json:"outline,omitempty"`
	OutlineMore  []OutlineMore  `json:"outline_more,omitempty"`
	OtherResults []OtherResult  `json:"other_results,omitempty"`
	Hint         string         `json:"hint,omitempty"`
}

// NewAnswerJSON is o as --json prints it.
func NewAnswerJSON(query, backend, library string, o Outcome) AnswerJSON {
	return AnswerJSON{Frontmatter: NewFrontmatter(query, backend, library, o), Results: o.Results, Outline: o.Outline,
		OutlineMore: o.OutlineMore, OtherResults: o.OtherResults, Hint: Hint(HintCLI, o)}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Render is the frontmatter as printed, its two fences included.
func (f Frontmatter) Render() string {
	var b strings.Builder
	line := func(key, value string) {
		if value != "" {
			b.WriteString(key + ": " + value + "\n")
		}
	}
	b.WriteString("---\n")
	line("query", f.Query)
	line("backend", f.Backend)
	line("library", f.Library)
	line("scope", f.Scope)
	line("version", f.Version)
	if f.Index != nil {
		line("index", f.Index.Summary())
	}
	if f.Decisions != nil {
		line("decisions", f.Decisions.Provider)
	}
	if f.section {
		line("stale", f.Stale)
		line("note", f.Note)
		if f.page != "" {
			b.WriteString(f.page + "\n")
		}
		b.WriteString("---\n")
		return b.String()
	}
	b.WriteString(f.pageLists())
	if w := f.Widened; w != nil {
		line("widened", w.line(len(f.Read) > 0))
	}
	if f.NotOnPages != nil {
		line("not_on_pages", listFlow(f.NotOnPages))
	}
	if f.NotInIndex != nil {
		line("not_in_index", listFlow(f.NotInIndex))
	}
	if len(f.OutsideScope) > 0 {
		line("outside_scope", listFlow(f.OutsideScope))
	}
	line("origin", f.Origin)
	line("stale", f.Stale)
	b.WriteString(definedInLine(f.DefinedIn))
	b.WriteString(nearLine(f.IndexedNear))
	line("note", f.Note)
	b.WriteString(listBlock("next", f.Next, NextStep.line))
	b.WriteString("---\n")
	return b.String()
}

// pageLists are the read, skipped and passed_over lists, each only when
// it has entries.
func (f Frontmatter) pageLists() string {
	return listBlock("read", f.Read, PageRead.line) +
		listBlock("skipped", f.Skipped, func(p PageSkipped) string { return p.URL + " (" + p.Cause + ")" }) +
		listBlock("passed_over", f.PassedOver, PagePassed.line)
}

// listBlock is a YAML block list under key, one line per item; "" for
// none.
func listBlock[T any](key string, items []T, line func(T) string) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(key + ":\n")
	for _, it := range items {
		b.WriteString("  - " + line(it) + "\n")
	}
	return b.String()
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

// Hint is the last line of an answer, in style's words: how to read what
// it left unread (a cut or unprinted section, another search result), and
// that it is unread until then; for an answer whose library value named no
// indexed library while some are near, which to pass. "" when there is
// nothing to say, or for HintNone.
func Hint(style HintStyle, o Outcome) string {
	if style == HintNone {
		return ""
	}
	section, other, library := "ketch docs --section <url#anchor>", `ketch docs --library <url> "<your question>"`, "--library"
	if style == HintMCP {
		section, other, library = `docs(section="<url#anchor>")`, `docs(library="<url>", query="<your question>")`, "library"
	}
	var reads []string
	if CutCount(o.Results) > 0 || len(o.Outline) > 0 || len(o.OutlineMore) > 0 {
		reads = append(reads, "a cut or unprinted section with "+section)
	}
	if len(o.OtherResults) > 0 {
		reads = append(reads, "another result with "+other)
	}
	var parts []string
	if len(reads) > 0 {
		parts = append(parts, "unread until you read it: "+strings.Join(reads, "; "))
	}
	if len(o.IndexedNear) > 0 {
		parts = append(parts, "library not indexed under that name: pass one of the indexed_near roots as "+library)
	}
	return strings.Join(parts, "; ")
}

// CutCount is how many of results were cut.
func CutCount(results []Result) int {
	n := 0
	for _, r := range results {
		if r.Truncated {
			n++
		}
	}
	return n
}

// RenderAnswer is an answer's body as printed: its groups under their
// pages' titles (results in printed order: a page's groups together), the
// outline of what was not printed, the other search results, and the
// hint line in style's words.
func RenderAnswer(o Outcome, style HintStyle) string {
	var b strings.Builder
	b.WriteString(renderPassages(o.Results))
	if len(o.Outline) > 0 || len(o.OutlineMore) > 0 {
		b.WriteString(sep(b.Len()) + "## Not printed\n")
		for _, e := range o.Outline {
			b.WriteString(outlineLine(e))
		}
		for _, m := range o.OutlineMore {
			call := SectionCall(m.Page, style)
			if style == HintNone {
				call = m.Page
			}
			fmt.Fprintf(&b, "(+%d more: %s)\n", m.Count, call)
		}
	}
	if len(o.OtherResults) > 0 {
		b.WriteString(sep(b.Len()) + "## Other results\n")
		for _, r := range o.OtherResults {
			b.WriteString(otherLine(r))
		}
	}
	if h := Hint(style, o); h != "" {
		b.WriteString(sep(b.Len()) + h + "\n")
	}
	return b.String()
}

// sep is the blank line between parts of the body, none before the first.
func sep(n int) string {
	if n == 0 {
		return ""
	}
	return "\n"
}

// otherLine is another search result as printed.
func otherLine(r OtherResult) string {
	line := "- " + firstNonEmpty(r.Title, r.URL) + " — " + r.URL
	if r.Label != "" {
		line += " (" + r.Label + ")"
	}
	if r.Snippet != "" {
		line += " — " + r.Snippet
	}
	return line + "\n"
}

// renderPassages is the groups of an answer under their pages' titles and
// addresses (results in printed order: a page's groups together): what
// the budget covers.
func renderPassages(results []Result) string {
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

// pageHeading is the lines a page's groups print under: its title and
// its address, with the blank line after them.
func pageHeading(r Result) string {
	title := r.PageTitle
	if title == "" {
		title = pageOf(r)
	}
	if r.SourceLabel != "" {
		return "## " + title + "\n" + pageOf(r) + " (" + r.SourceLabel + ")\n\n"
	}
	return "## " + title + "\n" + pageOf(r) + "\n\n"
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
	switch {
	case r.Confidence != "" && r.Truncated:
		b.WriteString(" (confidence " + r.Confidence + ", " + strings.TrimPrefix(cutNote(r), " ("))
	case r.Confidence != "":
		b.WriteString(" (confidence " + r.Confidence + ")")
	case r.Truncated:
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
