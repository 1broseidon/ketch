// Package decide asks a decision model for judgments: the probability that
// a statement holds, or which of a few named options fits. ketch docs uses
// it, when a provider is configured, to label the sources a search returns
// and to pick the passages of a page that answer a question; code makes
// every decision from the probabilities.
//
// A provider is one Go file holding its descriptor and its client, and one
// line in the providers list (registry.go):
//
//	func exampleProvider() Provider {
//		return Provider{
//			ID: "example", Name: "Example", Model: "example-1", Rank: 30,
//			Setup:    "example: API key not set (ketch config set example_api_key <key>)",
//			Settings: []config.Setting{SecretKey("example_api_key")},
//			Cutoffs:  DefaultCutoffs,
//			Usable:   func(c *config.Config) bool { return c.String("example_api_key") != "" },
//			New:      func(c *config.Config) (Decider, error) { return newExample(c.String("example_api_key")), nil },
//			Probe:    probeExample,
//		}
//	}
//
// The client implements Decider: it shapes a Request into the vendor's
// requests, splits it at the vendor's limits, retries rate-limit and
// overload statuses, bounds each attempt with a timeout, and answers every
// question or returns an error. Nothing in the docs code names a provider.
package decide

import (
	"context"
	"errors"
)

// Kind is what a question asks for.
type Kind int

const (
	// YesNo asks for the probability that the instruction holds.
	YesNo Kind = iota
	// Choice asks which one of the named options fits.
	Choice
)

// Option is one answer a Choice question offers.
type Option struct {
	Name        string
	Description string
}

// Item is structured data that belongs to one question (a page, a
// passage, a search result), under the name its instructions refer to it
// by (`passage`).
type Item struct {
	Name  string
	Value any
}

// Question is one judgment asked about a request's shared input.
type Question struct {
	// Name identifies the answer: letters, digits, _ and -.
	Name         string
	Kind         Kind
	Instructions string
	// Item is the data the question is about when it is not in the shared
	// input; nil for none.
	Item *Item
	// TrueMeans and FalseMeans say when a yes/no question holds and when
	// it does not; either may be empty.
	TrueMeans, FalseMeans string
	// Options are a Choice question's answers, in order.
	Options []Option
}

// Request is a set of questions about one shared input, a value that
// encodes to a JSON object (field order is kept).
type Request struct {
	Input     any
	Questions []Question
}

// Answer is one question's answer: the probability for a yes/no question;
// the option chosen and every option's probability for a Choice.
type Answer struct {
	Probability   float64            `json:"probability,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Response holds every question's answer by name, the input tokens the
// provider counted for the request (all its parts), and how many HTTP
// requests it took.
type Response struct {
	Answers     map[string]Answer
	InputTokens int
	Requests    int
}

// Decider answers a request: every question, or an error. A refused or
// missing answer is an error for the whole request.
type Decider interface {
	Decide(ctx context.Context, req Request) (Response, error)
}

// Cutoffs are where a provider's probabilities are read as low (at or
// below Low), high (at or above High), the makers' own (Official) and very
// high (VeryHigh). Calibration differs by model, so each provider carries
// its own.
type Cutoffs struct {
	Low, High, Official, VeryHigh float64
}

// DefaultCutoffs are the cut-offs both example providers were tested with.
var DefaultCutoffs = Cutoffs{Low: 0.3, High: 0.7, Official: 0.8, VeryHigh: 0.9}

// ErrRefused is a provider declining to answer a question.
var ErrRefused = errors.New("the provider refused a question")

// ErrIncomplete is a response that leaves a question unanswered or answers
// it in the wrong shape.
var ErrIncomplete = errors.New("the response does not answer every question")

// ErrTooLarge is a request that does not fit the provider's limits even
// split into parts.
var ErrTooLarge = errors.New("the request is larger than the provider takes")
