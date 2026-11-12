package docs

import (
	"github.com/1broseidon/ketch/internal/failure"
)

// Status is where a docs request ended. Every docs call ends in exactly one.
type Status string

const (
	// Found: results the caller can use: evidence matching the question
	// was found (the backend cannot know that it answers).
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
	// Note explains a found outcome's reason in words (the local index
	// ranked by keywords, and why); empty otherwise.
	Note string `json:"note,omitempty"`
	// An answer from the local index (an indexed library, or the pages a
	// search just read) also says what it was answered from (Library,
	// Index), and the question's distinctive terms as written: the rare
	// ones the library holds (identifiers the question writes, words in
	// under 1% of its sections or in at most 25), with how many of its sections hold each
	// (Terms), and those it holds nowhere (NotFound). A refusal for want
	// of any of them carries them too.
	Library string `json:"library,omitempty"`
	// Scope is the URL below the library's root the answer was limited to
	// (a library URL deeper than an indexed root), and OutsideScope the
	// distinctive terms the library holds but no section under it does.
	Scope        string      `json:"scope,omitempty"`
	Index        *IndexInfo  `json:"index,omitempty"`
	Terms        []TermCount `json:"terms,omitempty"`
	NotFound     []string    `json:"not_found,omitempty"`
	OutsideScope []string    `json:"outside_scope,omitempty"`
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
	// "verified, <evidence>"; "by name, <host> documents the <ecosystem>
	// package <name>[; the search results favour <root>]" when only a
	// registry package of the name supports it; or "unverified, ketch could
	// not confirm <host> as the official documentation of <what was asked>"
	// ("for this question" when nothing says the question names a thing).
	// Empty for an answer from an indexed library, which the caller chose by
	// indexing it.
	Origin string `json:"origin,omitempty"`
	err    error
	// originRoots are, for an origin by name, the docs roots the caller
	// can pass to choose: the resolved one, and the one the search results
	// favour (OriginHint).
	originRoots []string
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
