package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// Trace is how one docs answer was made: what `ketch docs --trace <file>`
// writes as JSON, for the QA runner (internal/docsqa) and for a person
// debugging a bad answer. It is never part of the answer, and nothing is
// recorded without one (every method is a no-op on nil). Its top level
// follows the evidence pipeline's stages (design/docs-evidence.md), each
// recorded by the stage itself, not by running it again:
//
//	query, library, backend   the question, the library root answered from, the backend named in the output
//	path                      how search answered: "local" (the index) or "search" (pages a web search read)
//	status, reason, note      the outcome, as printed; error, the error a refused or failed call exits with
//	index                     the library: root, resolved (how it was built), built, pages, sections
//	terms                     sections (the library's count) and list: each term of the question's one
//	                          term list, its uses ("word": queryTerms without filler; "ident": an
//	                          identifier the question writes, weighing double), the library's sections
//	                          whose full-text row holds it, its weight in Rank, and whether it is
//	                          distinctive (an identifier, or in under a tenth of the sections; every
//	                          term when none is)
//	gather                    fts_hits (the whole question's full-text candidates) and pool: each section
//	                          in pool order with url, page, anchor, heading_path, via ("question" or the
//	                          term whose own list brought it), rank in that list, score (the whole
//	                          question's, relative to the best section's: 0 to 1, 0 for a section
//	                          the whole question's ranking leaves out), order (its place in that
//	                          ranking, 0 for none) and text, cleaned
//	sort                      pieces: id, section (url#anchor), kind, role (its page's), text (cleaned,
//	                          verbatim), tokens (estimated, separator included), folded (the ids folded
//	                          into it) or folded_into (the id it was folded into)
//	rank                      budget (the tokens planned, frontmatter and margin out), page_tokens (the
//	                          page size: a longer section is never printed whole; 0 for none), used, stop
//	                          (reason "budget" when a piece was passed over for room, else "end"),
//	                          sections (index for index with gather's pool: section, order (its rank
//	                          in Gather, as fusion uses it), best_piece and best_rank (its best piece
//	                          by score and that piece's rank), fused (that piece's fused value), plan
//	                          (1 the first section opened), printed ("whole" or "cut"), cost (what its
//	                          group renders to), passed (why the walk passed it over: budget, limit),
//	                          dropped (printed, then dropped to fit the rendered answer)), and pieces
//	                          (index for index with sort's): score and its parts (role_factor; sum,
//	                          the terms' weights as they count; terms: each term held with its
//	                          weight, where (text, heading_path or both) and what it counts; rank,
//	                          dense, by score), fused (1/(k + rank) + 1/(k + its section's order)),
//	                          walk (its place in fused order, 1 the first), chosen (printed, whole or
//	                          cut), order (its section's plan), dropped
//	                          written: the terms as the question writes them (the frontmatter's), each
//	                          with ident (an identifier the question writes), sections (holding any
//	                          of its forms: match, the full-text query) and distinctive; not_shown:
//	                          the distinctive ones the library holds that no printed group names in
//	                          its text or heading path (a measurement: the status does not read it);
//	                          defined: each term the frontmatter lists, with the pool sections that
//	                          define it (sections), whether one of them is printed, and the section
//	                          its defined_in entry names (line, empty for none)
//	output                    text exactly as printed to stdout, its estimated tokens (and the body's,
//	                          after the frontmatter), and the budget the answer was fitted to; index,
//	                          terms, not_found and defined_in as the frontmatter states them; groups: each printed
//	                          group in printed order with section (url#anchor), page_title,
//	                          heading_path (below the title), cut, and for a cut group shown_tokens
//	                          and section_tokens (its text's tokens, its whole section's)
//
// When a call answers more than once (a remembered question that no longer
// answers, a second site), the stages hold the last answer.
type Trace struct {
	Query   string       `json:"query"`
	Library string       `json:"library,omitempty"`
	Backend string       `json:"backend,omitempty"`
	Path    string       `json:"path,omitempty"`
	Status  Status       `json:"status,omitempty"`
	Reason  string       `json:"reason,omitempty"`
	Note    string       `json:"note,omitempty"`
	Error   string       `json:"error,omitempty"`
	Index   *traceIndex  `json:"index,omitempty"`
	Terms   *traceTerms  `json:"terms,omitempty"`
	Gather  *traceGather `json:"gather,omitempty"`
	Sort    *traceSort   `json:"sort,omitempty"`
	Rank    *traceRank   `json:"rank,omitempty"`
	Output  traceOutput  `json:"output"`

	printed bytes.Buffer
}

type traceIndex struct {
	Root     string    `json:"root"`
	Resolved string    `json:"resolved,omitempty"`
	Built    time.Time `json:"built,omitzero"`
	Pages    int       `json:"pages"`
	Sections int       `json:"sections"`
}

type traceTerms struct {
	Sections int                `json:"sections"`
	List     []traceTerm        `json:"list"`
	Written  []traceWrittenTerm `json:"written"`
	NotShown []string           `json:"not_shown"`
	Defined  []traceDefined     `json:"defined"`
}

type traceDefined struct {
	Term     string   `json:"term"`
	Sections []string `json:"sections"`
	Printed  bool     `json:"printed"`
	Line     string   `json:"line,omitempty"`
}

type traceWrittenTerm struct {
	Term        string `json:"term"`
	Ident       bool   `json:"ident,omitempty"`
	Match       string `json:"match"`
	Sections    int    `json:"sections"`
	Distinctive bool   `json:"distinctive"`
}

type traceTerm struct {
	Term        string   `json:"term"`
	Uses        []string `json:"uses"`
	Sections    int      `json:"sections"`
	Weight      float64  `json:"weight"`
	Distinctive bool     `json:"distinctive"`
}

type traceGather struct {
	FTSHits int               `json:"fts_hits"`
	Pool    []traceSectionHit `json:"pool"`
}

type traceSectionHit struct {
	URL         string   `json:"url"`
	Page        string   `json:"page"`
	Anchor      string   `json:"anchor"`
	HeadingPath []string `json:"heading_path"`
	Via         string   `json:"via"`
	Rank        int      `json:"rank"`
	Score       float64  `json:"score"`
	Order       int      `json:"order"`
	Text        string   `json:"text"`
}

type traceSort struct {
	Pieces []tracePiece `json:"pieces"`
}

type tracePiece struct {
	ID         int    `json:"id"`
	Section    string `json:"section"`
	Kind       string `json:"kind"`
	Role       string `json:"role"`
	Text       string `json:"text"`
	Tokens     int    `json:"tokens"`
	Folded     []int  `json:"folded,omitempty"`
	FoldedInto *int   `json:"folded_into,omitempty"`
}

type traceRank struct {
	Budget int `json:"budget"`
	// PageTokens is the page size: a section longer is never printed
	// whole (0: none).
	PageTokens int                `json:"page_tokens"`
	Used       int                `json:"used"`
	Stop       traceStop          `json:"stop"`
	Sections   []traceSectionRank `json:"sections"`
	Pieces     []traceChoice      `json:"pieces"`
}

type traceStop struct {
	Reason string `json:"reason"`
}

type traceSectionRank struct {
	Section   string  `json:"section"`
	Order     int     `json:"order"`
	BestPiece *int    `json:"best_piece,omitempty"`
	BestRank  int     `json:"best_rank,omitempty"`
	Fused     float64 `json:"fused"`
	Printed   string  `json:"printed,omitempty"`
	Plan      int     `json:"plan,omitempty"`
	Cost      int     `json:"cost,omitempty"`
	Passed    string  `json:"passed,omitempty"`
	Dropped   bool    `json:"dropped,omitempty"`
}

type traceChoice struct {
	ID      int         `json:"id"`
	Score   float64     `json:"score"`
	Parts   *traceParts `json:"parts,omitempty"`
	Fused   float64     `json:"fused,omitempty"`
	Walk    int         `json:"walk,omitempty"`
	Chosen  bool        `json:"chosen"`
	Order   int         `json:"order,omitempty"`
	Dropped bool        `json:"dropped,omitempty"`
}

type traceParts struct {
	RoleFactor float64        `json:"role_factor"`
	Sum        float64        `json:"sum"`
	Terms      []traceTermHit `json:"terms"`
	Rank       int            `json:"rank,omitempty"`
}

type traceTermHit struct {
	Term   string  `json:"term"`
	Weight float64 `json:"weight"`
	Where  string  `json:"where"`
	Counts float64 `json:"counts"`
}

type traceOutput struct {
	Text       string       `json:"text"`
	Tokens     int          `json:"tokens"`
	BodyTokens int          `json:"body_tokens"`
	Budget     int          `json:"budget"`
	Index      *IndexInfo   `json:"index,omitempty"`
	Terms      []TermCount  `json:"terms,omitempty"`
	DefinedIn  []DefinedIn  `json:"defined_in,omitempty"`
	NotFound   []string     `json:"not_found,omitempty"`
	Groups     []traceGroup `json:"groups,omitempty"`
}

type traceGroup struct {
	Section       string   `json:"section"`
	PageTitle     string   `json:"page_title"`
	HeadingPath   []string `json:"heading_path,omitempty"`
	Cut           bool     `json:"cut"`
	ShownTokens   int      `json:"shown_tokens,omitempty"`
	SectionTokens int      `json:"section_tokens,omitempty"`
}

// EstimateTokens is the token estimate the trace's output tokens and every
// budget use, for a reader of the trace (the QA runner) to count a part
// of the output alike.
func EstimateTokens(s string) int { return estimateTokens(s) }

// NewTrace starts the trace of one question.
func NewTrace(query string) *Trace { return &Trace{Query: query} }

type traceKey struct{}

// WithTrace carries t to the docs backend's answer.
func WithTrace(ctx context.Context, t *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, t)
}

// TraceFrom is the trace ctx carries, nil for none.
func TraceFrom(ctx context.Context) *Trace {
	t, _ := ctx.Value(traceKey{}).(*Trace)
	return t
}

// Stdout is w, also copied into the trace's output when there is one.
func (t *Trace) Stdout(w io.Writer) io.Writer {
	if t == nil {
		return w
	}
	return io.MultiWriter(w, &t.printed)
}

// SetOutcome records the outcome the output reports, under the backend it
// names.
func (t *Trace) SetOutcome(backend string, o Outcome) {
	if t == nil {
		return
	}
	t.Backend, t.Status, t.Reason, t.Note = backend, o.Status, o.Reason, o.Note
	if err := o.Err(); err != nil {
		t.Error = err.Error()
	}
	t.Output.Index, t.Output.Terms, t.Output.NotFound, t.Output.Groups = o.Index, o.Terms, o.NotFound, nil
	t.Output.DefinedIn = o.DefinedIn
	for _, r := range o.Results {
		t.Output.Groups = append(t.Output.Groups, traceGroup{Section: r.URL, PageTitle: r.PageTitle, HeadingPath: r.HeadingPath, Cut: r.Truncated,
			ShownTokens: r.ShownTokens, SectionTokens: r.SectionTokens})
	}
}

// Write writes the trace to path as JSON, with what was printed and runErr,
// the error the call exits with (nil for none).
func (t *Trace) Write(path string, runErr error) error {
	if t == nil {
		return nil
	}
	if runErr != nil && t.Error == "" {
		t.Error = runErr.Error()
	}
	t.Output.Text = t.printed.String()
	t.Output.Tokens = estimateTokens(t.Output.Text)
	t.Output.BodyTokens = t.Output.Tokens
	if rest, ok := strings.CutPrefix(t.Output.Text, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			t.Output.BodyTokens = estimateTokens(body)
		}
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// setPath records how the search backend answered.
func (t *Trace) setPath(path string) {
	if t != nil {
		t.Path = path
	}
}

// ranking starts the record of one answer from lib: the library and the
// budget, the stages after it cleared.
func (t *Trace) ranking(lib docsindex.Library, opts LocalOptions) {
	if t == nil {
		return
	}
	t.Library = lib.Root
	t.Index = &traceIndex{Root: lib.Root, Resolved: lib.Resolved, Built: lib.Built, Pages: lib.Pages, Sections: lib.Sections}
	t.Terms, t.Gather, t.Sort, t.Rank = nil, nil, nil, nil
	t.Output.Budget = opts.Tokens
	if opts.Tokens <= 0 {
		t.Output.Budget = localTokens
	}
}

// gathered records Gather's terms with their counts and its pool.
func (t *Trace) gathered(g *gathered) {
	if t == nil {
		return
	}
	t.Terms = &traceTerms{Sections: g.sections, List: []traceTerm{}}
	for _, term := range g.terms {
		var uses []string
		if term.word {
			uses = append(uses, "word")
		}
		if term.ident {
			uses = append(uses, "ident")
		}
		t.Terms.List = append(t.Terms.List, traceTerm{Term: term.term, Uses: uses, Sections: term.sections})
	}
	t.Terms.Written = []traceWrittenTerm{}
	for _, wt := range g.written {
		t.Terms.Written = append(t.Terms.Written, traceWrittenTerm{Term: wt.text, Ident: wt.ident, Match: wt.match, Sections: wt.sections, Distinctive: wt.distinctive})
	}
	t.Gather = &traceGather{FTSHits: g.ftsHits, Pool: []traceSectionHit{}}
	for _, p := range g.pool {
		t.Gather.Pool = append(t.Gather.Pool, traceSectionHit{
			URL: webSectionOf(p.Section, 0).URL(), Page: p.Page, Anchor: p.Anchor, HeadingPath: p.Crumbs,
			Via: p.via, Rank: p.rank, Score: p.score, Order: p.order, Text: p.Text,
		})
	}
}

// notShown records the distinctive terms the library holds that nothing
// printed names.
func (t *Trace) notShown(terms []string) {
	if t == nil || t.Terms == nil {
		return
	}
	t.Terms.NotShown = append([]string{}, terms...)
}

// defined records where the listed terms are defined in the pool, and
// the section each defined_in entry names.
func (t *Trace) defined(s *sorted, defs []definedTerm) {
	if t == nil || t.Terms == nil {
		return
	}
	t.Terms.Defined = []traceDefined{}
	for _, d := range defs {
		td := traceDefined{Term: d.term, Sections: []string{}, Printed: d.printed}
		for _, i := range d.defs {
			td.Sections = append(td.Sections, webSectionOf(s.secs[i].Section, 0).URL())
		}
		if d.target >= 0 {
			td.Line = webSectionOf(s.secs[d.target].Section, 0).URL()
		}
		t.Terms.Defined = append(t.Terms.Defined, td)
	}
}

// sorted records Sort's pieces.
func (t *Trace) sorted(s *sorted) {
	if t == nil {
		return
	}
	t.Sort = &traceSort{Pieces: make([]tracePiece, len(s.pieces))}
	for i, p := range s.pieces {
		sec := s.secs[p.sec]
		tp := tracePiece{ID: p.id, Section: webSectionOf(sec.Section, 0).URL(), Kind: p.kind.String(), Role: sec.role.String(),
			Text: p.text, Tokens: p.tokens, Folded: p.folded}
		if p.into >= 0 {
			tp.FoldedInto = &s.pieces[i].into
		}
		t.Sort.Pieces[i] = tp
	}
}

// ranked records Rank's scores, the walk and what each section printed:
// the plans that stand, and those fitting the rendered answer dropped.
func (t *Trace) ranked(s *sorted, g *gathered, r *ranked) {
	if t == nil {
		return
	}
	for i := range t.Terms.List {
		t.Terms.List[i].Weight, t.Terms.List[i].Distinctive = r.weights[i], r.distinct[i]
	}
	t.Rank = &traceRank{Budget: r.budget, PageTokens: answerPageTokens, Used: r.used, Stop: traceStop{Reason: r.stop},
		Sections: make([]traceSectionRank, len(s.secs)), Pieces: make([]traceChoice, len(s.pieces))}
	for id, sc := range r.scores {
		t.Rank.Pieces[id] = traceScore(g, r, id, sc)
	}
	for n, id := range r.walk {
		t.Rank.Pieces[id].Walk = n + 1
	}
	for i, sec := range s.secs {
		ts := traceSectionRank{Section: webSectionOf(sec.Section, 0).URL(), Order: max(sec.order, 1), Passed: r.passed[i]}
		for _, id := range sec.pieces {
			if rk := r.scores[id].rank; rk > 0 && (ts.BestRank == 0 || rk < ts.BestRank) {
				best := id
				ts.BestPiece, ts.BestRank, ts.Fused = &best, rk, r.fused[id]
			}
		}
		t.Rank.Sections[i] = ts
	}
	for n, pl := range r.plans {
		t.plan(s, n, pl, false)
	}
	for k, pl := range r.dropped {
		t.plan(s, len(r.plans)+len(r.dropped)-1-k, pl, true)
	}
}

// traceScore is piece id's score with its parts.
func traceScore(g *gathered, r *ranked, id int, sc scored) traceChoice {
	c := traceChoice{ID: id, Score: sc.score, Fused: r.fused[id]}
	if len(sc.hits) == 0 {
		return c
	}
	c.Parts = &traceParts{RoleFactor: sc.role, Sum: sc.sum, Rank: sc.rank}
	for _, h := range sc.hits {
		where := "text"
		switch {
		case h.inText && h.inPath:
			where = "text, heading_path"
		case h.inPath:
			where = "heading_path"
		}
		c.Parts.Terms = append(c.Parts.Terms, traceTermHit{Term: g.terms[h.term].term, Weight: r.weights[h.term], Where: where, Counts: h.count})
	}
	return c
}

// plan records plan n (0 the first) as printed, or dropped.
func (t *Trace) plan(s *sorted, n int, pl plan, dropped bool) {
	ts := &t.Rank.Sections[pl.sec]
	ts.Plan, ts.Cost, ts.Dropped, ts.Printed = n+1, pl.cost, dropped, "cut"
	ids := pl.pieces
	if pl.whole {
		ts.Printed, ids = "whole", s.secs[pl.sec].pieces
	}
	for _, id := range ids {
		p := &t.Rank.Pieces[id]
		p.Chosen, p.Order, p.Dropped = !dropped, n+1, dropped
	}
}
