package docs

import (
	"github.com/1broseidon/ketch/internal/failure"
)

// Status is where a docs request ended. Every docs call ends in exactly one.
type Status string

const (
	// Found: results the caller can use: evidence matching the question
	// was found (the backend cannot know that it answers). Statuses are
	// internal: no answer prints one (design/docs-evidence.md, Final
	// shape); they decide exit codes and which of two reads is kept.
	Found Status = "found"
	// Partial: results, but the backend knows it could not finish (a budget
	// cut discovery short, the best candidate page failed). The results are
	// real; there may be better ones.
	Partial Status = "partial"
	// Refused: the backend worked and declined to answer (no library named,
	// ambiguous, nothing verified, no version, nothing matched). The caller
	// changes the request; retrying the same one gives the same refusal.
	Refused Status = "refused"
	// Failed: the backend could not do its work (a source down, rate
	// limited, timed out, cancelled, not configured). Retrying or
	// configuring may help.
	Failed Status = "failed"
)

// ErrNoResults is a backend that worked and found nothing for the query: a
// refusal, not an empty answer, so it reaches the next backend in a chain
// and the caller sees why.
var ErrNoResults = failure.New(failure.NotFound, "no_results", "no documentation matched the query")

// Attempt is one backend's part in a docs request.
type Attempt struct {
	Backend string `json:"backend"`
	Status  Status `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Error   string `json:"error,omitempty"`
	err     error
}

// Outcome is the whole of a docs request: where it ended, why, which
// backend answered, and every backend tried, in order. The CLI and the MCP
// server render it; neither decides it.
type Outcome struct {
	Status   Status         `json:"status"`
	Reason   string         `json:"reason,omitempty"`
	Backend  string         `json:"backend,omitempty"`
	Results  []Result       `json:"results"`
	Matches  []LibraryMatch `json:"matches,omitempty"`
	Attempts []Attempt      `json:"attempts,omitempty"`
	// Note says in words what the fields do not: why a call that read no
	// page printed only search results, other indexed versions.
	Note string `json:"note,omitempty"`
	// An answer from the local index (an indexed library, or the pages a
	// search just read) also says what it was answered from (Library,
	// Index for an indexed library) and the question's distinctive terms
	// as written that it holds nowhere (NotFound: not_in_index for an
	// indexed library, not_on_pages for pages a search read, which say
	// nothing of the rest of the docs). A refusal for want of any of them
	// carries them too.
	Library string `json:"library,omitempty"`
	// Scope is the URL below the library's root the answer was limited to
	// (a library URL deeper than an indexed root), and OutsideScope the
	// distinctive terms the library holds but no section under it does.
	Scope        string     `json:"scope,omitempty"`
	Index        *IndexInfo `json:"index,omitempty"`
	NotFound     []string   `json:"not_found,omitempty"`
	OutsideScope []string   `json:"outside_scope,omitempty"`
	// DefinedIn names where listed terms no printed result defines are
	// defined (design/docs-evidence.md, Round 8).
	DefinedIn []DefinedIn `json:"defined_in,omitempty"`
	// Stale says an answer from an indexed library could not check what it
	// cites: a cited page last read longer ago than cache_ttl could not be
	// read again (no network, a timeout, --offline). "unverified, built
	// <date>"; empty when every cited page is fresh or was checked.
	Stale string `json:"stale,omitempty"`
	// IndexedNear are the indexed libraries near a library value that
	// named none, when the outcome came from elsewhere (design/docs-evidence.md,
	// Round 11): what to pass as library next. Never answered from.
	IndexedNear []NearLibrary `json:"indexed_near,omitempty"`
	// Origin says whether an answer from pages a search read is from the
	// documentation of what was asked (design/docs-evidence.md, Build 1):
	// "verified, <evidence>"; none when only a name supports it;
	// "unverified, …" when nothing ketch gathered confirms it. Empty for
	// an answer from an indexed library, which the caller chose by
	// indexing it. The answer prints it only when it says something
	// (ShownOrigin).
	Origin string `json:"origin,omitempty"`
	// Read are the pages an answer from a search read, in the order it
	// printed them from (best first), each with its search rank; Skipped
	// the search hits it could not read and why; Widened, when the exact
	// pages lacked a term of the question, the pages read beside them
	// (search_exact.go). OtherResults are the search hits it did not read.
	Read         []PageRead    `json:"read,omitempty"`
	Skipped      []PageSkipped `json:"skipped,omitempty"`
	Widened      *Widening     `json:"widened,omitempty"`
	OtherResults []OtherResult `json:"other_results,omitempty"`
	// Outline names the sections of the pages read (or printed from) that
	// the answer did not print; OutlineMore counts, per page, those it had
	// no room to name (evidence_outline.go).
	Outline     []OutlineEntry `json:"outline,omitempty"`
	OutlineMore []OutlineMore  `json:"outline_more,omitempty"`
	// Decisions, PassedOver and Next are a decided answer's
	// (decide_flow.go): the provider used, the results read or tried and
	// not printed, and the calls to read a better source or a cut section.
	Decisions  *DecisionUse `json:"decisions,omitempty"`
	PassedOver []PagePassed `json:"passed_over,omitempty"`
	Next       []NextStep   `json:"next,omitempty"`
	err        error
	// named is the library the request named ("" for none): an unverified
	// origin is printed only for one.
	named string
	// unread says why a site gave no text (its pages' causes, counted),
	// when that is how the outcome ended: the next site's answer names it.
	unread string
}

// Err is the error behind a refused or failed outcome, classified by
// internal/failure, and nil when the outcome has results.
func (o Outcome) Err() error {
	if o.Status == Found || o.Status == Partial {
		return nil
	}
	return o.err
}

// Evidence reports an answer of the evidence pipeline: from an indexed
// library, or from what a search found (the pages read, the results).
// Its body prints as RenderAnswer renders it.
func (o Outcome) Evidence() bool {
	return o.Index != nil || len(o.Read) > 0 || o.Widened != nil || len(o.OtherResults) > 0
}

// Kind is the failure class of a refused or failed outcome.
func (o Outcome) Kind() failure.Kind {
	if err := o.Err(); err != nil {
		return failure.Classify(err, failure.Upstream).Kind
	}
	return 0
}

// Failures are the attempts that did not answer, in order.
func (o Outcome) Failures() []Attempt {
	var out []Attempt
	for _, a := range o.Attempts {
		if a.Status == Refused || a.Status == Failed {
			out = append(out, a)
		}
	}
	return out
}

// Answer records one backend's search or docs call as an Outcome: results
// are found, no results a refusal (ErrNoResults), and an error refused or
// failed by its failure class.
func Answer(backend string, results []Result, err error) Outcome {
	a := attempt(backend, len(results), err)
	return finish(a, results, nil, []Attempt{a})
}

// Resolved records one backend's resolve call as an Outcome, the way Answer
// does for results.
func Resolved(backend string, matches []LibraryMatch, err error) Outcome {
	a := attempt(backend, len(matches), err)
	return finish(a, nil, matches, []Attempt{a})
}

// attempt classifies one backend call. Validation and not-found failures
// are refusals (the request must change); anything else, cancellation
// included, is a failure (the call could not finish).
func attempt(backend string, n int, err error) Attempt {
	if err == nil && n > 0 {
		return Attempt{Backend: backend, Status: Found}
	}
	if err == nil {
		err = ErrNoResults
	}
	c := failure.Classify(err, failure.Upstream)
	reason := c.Reason
	if reason == "" {
		reason = c.Kind.String()
	}
	status := Failed
	if c.Kind == failure.Validation || c.Kind == failure.NotFound {
		status = Refused
	}
	return Attempt{Backend: backend, Status: status, Reason: reason, Error: err.Error(), err: err}
}

// finish builds the Outcome a final attempt decides, keeping every attempt.
func finish(final Attempt, results []Result, matches []LibraryMatch, attempts []Attempt) Outcome {
	if results == nil {
		results = []Result{}
	}
	o := Outcome{Status: final.Status, Reason: final.Reason, Results: results, Matches: matches, Attempts: attempts, err: final.err}
	if final.Status == Found || final.Status == Partial {
		o.Backend = final.Backend
		o.Reason = ""
	}
	return o
}
