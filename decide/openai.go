package decide

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// OpenAI's Decisions API (POST /v1/decisions): predicates (a probability)
// and choices about one input, up to 400 questions a request.
const (
	openaiEndpoint = "https://api.openai.com/v1/decisions"
	openaiModels   = "https://api.openai.com/v1/models"
	openaiModel    = "gpt-6-luna"
	openaiKey      = "openai_api_key"
	// openaiMaxQuestions is the most questions one request takes.
	openaiMaxQuestions = 400
	// openaiFold puts a predicate's true and false meanings after its
	// instructions: Decisions has no criteria field.
	openaiFoldTrue  = "True if: "
	openaiFoldFalse = "False if: "
)

func openaiProvider() Provider {
	setup := "openai: API key not set (get one at https://platform.openai.com/api-keys then: ketch config set " + openaiKey + " <key>)"
	return Provider{
		ID: "openai", Name: "OpenAI", Model: openaiModel, Rank: 20, Setup: setup,
		Cutoffs:  DefaultCutoffs,
		Settings: []config.Setting{SecretKey(openaiKey)},
		Usable:   func(c *config.Config) bool { return c.String(openaiKey) != "" },
		New:      func(c *config.Config) (Decider, error) { return NewOpenAI(c.String(openaiKey), openaiEndpoint), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return probeModels(ctx, client, openaiModels, c.String(openaiKey), openaiKey)
		},
	}
}

// OpenAI is the Decisions client.
type OpenAI struct {
	post         poster
	model        string
	maxQuestions int
}

// NewOpenAI is a Decisions client with key, posting to endpoint (the
// provider's own when empty).
func NewOpenAI(key, endpoint string) *OpenAI {
	if endpoint == "" {
		endpoint = openaiEndpoint
	}
	return &OpenAI{
		post: poster{client: httpx.Default(), endpoint: endpoint, key: key, name: "openai", timeout: attemptTimeout,
			attempts: attempts, backoff: firstBackoff, retry: statusIn(http.StatusTooManyRequests, 500, 503, 529)},
		model: openaiModel, maxQuestions: openaiMaxQuestions,
	}
}

// openaiResponse is the Decisions response: answers in question order.
type openaiResponse struct {
	Answers []openaiAnswer `json:"answers"`
	Usage   struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
}

// openaiAnswer is one answer: a predicate's probability, a choice with its
// options' probabilities, or a refusal.
type openaiAnswer struct {
	Type          string   `json:"type"`
	Name          string   `json:"name"`
	Probability   *float64 `json:"probability"`
	Choice        string   `json:"choice"`
	Probabilities []struct {
		Value       string  `json:"value"`
		Probability float64 `json:"probability"`
	} `json:"probabilities"`
}

// answer is a as q's answer, or why it is none.
func (a openaiAnswer) answer(q Question) (Answer, error) {
	switch {
	case q.Kind == YesNo && (a.Type != "predicate" || a.Probability == nil):
		return Answer{}, fmt.Errorf("openai: %w: %s has no probability", ErrIncomplete, q.Name)
	case q.Kind == YesNo:
		return Answer{Probability: *a.Probability}, nil
	case a.Type != "choice":
		return Answer{}, fmt.Errorf("openai: %w: %s has no choice", ErrIncomplete, q.Name)
	}
	probs := make(map[string]float64, len(a.Probabilities))
	for _, p := range a.Probabilities {
		probs[p.Value] = p.Probability
	}
	return Answer{Choice: a.Choice, Probabilities: probs}, nil
}

// Decide sends req as one request, or in parts of at most maxQuestions,
// one after another, each with the whole input.
func (o *OpenAI) Decide(ctx context.Context, req Request) (Response, error) {
	data, err := value(req.Input)
	if err != nil {
		return Response{}, fmt.Errorf("openai: %w", err)
	}
	n := (len(req.Questions) + o.maxQuestions - 1) / o.maxQuestions
	if n > maxParts {
		return Response{}, fmt.Errorf("openai: %w: %d requests (at most %d)", ErrTooLarge, n, maxParts)
	}
	input := array([][]byte{object(field{"role", str("user")}, field{"content", array([][]byte{inputJSON(data)})})})
	out := Response{Answers: map[string]Answer{}}
	for from := 0; from < len(req.Questions); from += o.maxQuestions {
		part := req.Questions[from:min(from+o.maxQuestions, len(req.Questions))]
		tokens, err := o.send(ctx, input, part, out.Answers)
		if err != nil {
			return Response{}, err
		}
		out.InputTokens += tokens
		out.Requests++
	}
	return out, checkAnswered("openai", req.Questions, out.Answers)
}

// inputJSON is an input_json content part holding data.
func inputJSON(data []byte) []byte {
	return object(field{"type", str("input_json")}, field{"data", data})
}

// openaiQuestion is q as Decisions takes it: a predicate with its meanings
// folded into its instructions, or a choice with its options; with an
// item, the instructions are the item as input_json and the text.
func openaiQuestion(q Question) ([]byte, error) {
	text := q.Instructions
	kind := "choice"
	if q.Kind == YesNo {
		kind, text = "predicate", fold(q)
	}
	instructions := str(text)
	if q.Item != nil {
		item, err := value(q.Item.Value)
		if err != nil {
			return nil, err
		}
		instructions = array([][]byte{inputJSON(object(field{q.Item.Name, item})),
			object(field{"type", str("input_text")}, field{"text", str(text)})})
	}
	fields := []field{{"type", str(kind)}, {"name", str(q.Name)}, {"instructions", instructions}}
	if q.Kind == Choice {
		choices := make([][]byte, len(q.Options))
		for i, opt := range q.Options {
			choices[i] = object(field{"value", str(opt.Name)}, field{"description", str(opt.Description)})
		}
		fields = append(fields, field{"choices", array(choices)})
	}
	return object(fields...), nil
}

// fold is a predicate's instructions with its true and false meanings
// after a blank line.
func fold(q Question) string {
	var lines []string
	if q.TrueMeans != "" {
		lines = append(lines, openaiFoldTrue+q.TrueMeans)
	}
	if q.FalseMeans != "" {
		lines = append(lines, openaiFoldFalse+q.FalseMeans)
	}
	if len(lines) == 0 {
		return q.Instructions
	}
	return q.Instructions + "\n\n" + strings.Join(lines, "\n")
}

// send posts one part and adds its answers to into.
func (o *OpenAI) send(ctx context.Context, input []byte, part []Question, into map[string]Answer) (int, error) {
	qs := make([][]byte, len(part))
	for i, q := range part {
		b, err := openaiQuestion(q)
		if err != nil {
			return 0, fmt.Errorf("openai: %w", err)
		}
		qs[i] = b
	}
	body := object(field{"model", str(o.model)}, field{"input", input}, field{"questions", array(qs)})
	var resp openaiResponse
	if err := o.post.post(ctx, body, &resp); err != nil {
		return 0, err
	}
	if len(resp.Answers) != len(part) {
		return 0, fmt.Errorf("openai: %w: %d answers for %d questions", ErrIncomplete, len(resp.Answers), len(part))
	}
	var refused []string
	for i, q := range part {
		a := resp.Answers[i]
		if a.Name != "" && a.Name != q.Name {
			return 0, fmt.Errorf("openai: %w: answer %d is %s, not %s", ErrIncomplete, i+1, a.Name, q.Name)
		}
		if a.Type == "refusal" {
			refused = append(refused, q.Name)
			continue
		}
		ans, err := a.answer(q)
		if err != nil {
			return 0, err
		}
		into[q.Name] = ans
	}
	if len(refused) > 0 {
		return 0, fmt.Errorf("openai: %w: %d of %d (%s)", ErrRefused, len(refused), len(part), strings.Join(refused[:min(len(refused), 5)], ", "))
	}
	return resp.Usage.InputTokens, nil
}
