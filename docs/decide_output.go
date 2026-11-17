package docs

import "fmt"

// What a decided answer adds to an answer's fields. Text output says it in
// words (labels, confidence, reasons); --json and MCP also carry the raw
// probabilities, tiers and the provider's token use.

// DecisionUse is the decision provider an answer used, the requests it
// sent, the input tokens they counted, and the cut-offs the probabilities
// were read with.
type DecisionUse struct {
	Provider    string      `json:"provider"`
	Model       string      `json:"model,omitempty"`
	Requests    int         `json:"requests"`
	InputTokens int         `json:"input_tokens"`
	Cutoffs     CutoffsJSON `json:"cutoffs"`
}

// CutoffsJSON are a provider's cut-offs as --json names them.
type CutoffsJSON struct {
	Low      float64 `json:"low"`
	High     float64 `json:"high"`
	Official float64 `json:"official"`
	VeryHigh float64 `json:"very_high"`
}

// PageJudgments are the page pass's raw answers about one page.
type PageJudgments struct {
	Documentation   float64 `json:"documentation"`
	Makers          float64 `json:"makers"`
	NamesRelease    float64 `json:"names_release"`
	OtherRelease    float64 `json:"other_release"`
	Kind            string  `json:"kind"`
	KindProbability float64 `json:"kind_probability"`
	// Passages and TopPassages are the passage pass's: how many passages
	// were asked about and their best probabilities (at most eight).
	Passages    int       `json:"passages,omitempty"`
	TopPassages []float64 `json:"top_passages,omitempty"`
}

// ResultJudgments are the raw answers about a search result not read.
type ResultJudgments struct {
	Makers          float64 `json:"makers"`
	Kind            string  `json:"kind"`
	KindProbability float64 `json:"kind_probability"`
}

// PagePassed is a result read or tried and not printed, and why.
type PagePassed struct {
	URL       string         `json:"url"`
	Rank      int            `json:"rank"`
	Label     string         `json:"label,omitempty"`
	Tier      string         `json:"tier,omitempty"`
	Context   string         `json:"context,omitempty"`
	Reason    string         `json:"reason"`
	Judgments *PageJudgments `json:"judgments,omitempty"`
}

// line is the page as its passed_over line names it.
func (p PagePassed) line() string {
	if p.Label != "" {
		return fmt.Sprintf("%s (%s, search rank %d): %s", p.URL, p.Label, p.Rank, p.Reason)
	}
	return fmt.Sprintf("%s (search rank %d): %s", p.URL, p.Rank, p.Reason)
}

// NextStep is a call worth making next: a better kind of source than the
// page read or a page the search ranked above it, also on the question
// (Library set), or a cut section of it on the question (Section set).
type NextStep struct {
	Command string `json:"command"`
	// Why is "better kind of source", "ranked higher in search" or "cut
	// section".
	Why     string `json:"why"`
	Library string `json:"library,omitempty"`
	Section string `json:"section,omitempty"`
	Label   string `json:"label,omitempty"`
	Rank    int    `json:"rank,omitempty"`
	// Read says whether the better source was read ("read, not printed")
	// or is a search result not read ("not read").
	Read string `json:"read,omitempty"`
}

// The kinds of next step.
const (
	nextBetter  = "better kind of source"
	nextHigher  = "ranked higher in search"
	nextSection = "cut section"
)

// line is the step as its next line says it.
func (n NextStep) line() string {
	if n.Why == nextHigher {
		return fmt.Sprintf("%s (%s, search rank %d, ranked above the page read, also on the question)", n.Command, n.Label, n.Rank)
	}
	if n.Library != "" {
		return fmt.Sprintf("%s (%s, search rank %d, %s)", n.Command, n.Label, n.Rank, n.Read)
	}
	return n.Command + " (cut, more on the question in this section)"
}
