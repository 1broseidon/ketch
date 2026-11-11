package docs

import (
	"github.com/1broseidon/ketch/internal/failure"
)

// Status is where a docs request ended. Every docs call ends in exactly one.
type Status string

const (
	// Answered: results the caller can use.
	Answered Status = "answered"
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
	// Note explains an answered outcome's reason in words (the local index
	// ranked by keywords, and why); empty otherwise.
	Note string `json:"note,omitempty"`
	// An answer from the local index (an indexed library, or the pages a
	// search just read) also says what it was answered from (Library,
	// Index), and the question's distinctive terms as written: the rare
	// ones the library holds (identifiers the question writes, words in
	// under 1% of its sections), with how many of its sections hold each
	// (Terms), and those it holds nowhere (NotFound). A refusal for want
	// of any of them carries them too.
	Library  string      `json:"library,omitempty"`
	Index    *IndexInfo  `json:"index,omitempty"`
	Terms    []TermCount `json:"terms,omitempty"`
	NotFound []string    `json:"not_found,omitempty"`
	err      error
}

// Err is the error behind a refused or failed outcome, classified by
// internal/failure, and nil when the outcome has results.
func (o Outcome) Err() error {
	if o.Status == Answered || o.Status == Partial {
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
// are answered, no results a refusal (ErrNoResults), and an error refused or
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
		return Attempt{Backend: backend, Status: Answered}
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
	if final.Status == Answered || final.Status == Partial {
		o.Backend = final.Backend
		o.Reason = ""
	}
	return o
}
