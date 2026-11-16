package decide

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// TypeSafe's System One API (https://docs.typesafe.ai): Jev answers
// yes/no questions ("noul", a probability) and choices about one state.
const (
	typesafeEndpoint = "https://api.typesafe.ai/v1/systemone"
	typesafeModels   = "https://api.typesafe.ai/v1/models"
	typesafeModel    = "jev-1.14.0"
	typesafeKey      = "typesafe_api_key"
	// typesafeRequestTokens is the estimated size a request is packed to:
	// the per-request context is 64k tokens, and the estimate (three bytes
	// a token) runs high.
	typesafeRequestTokens = 40000
	// typesafeQuestionTokens is the per-question context (32k): the state
	// and any one question together.
	typesafeQuestionTokens = 32000
	// maxParts is the most requests one Request is split into; more is
	// ErrTooLarge.
	maxParts = 16
	// typesafeConcurrency is how many parts are sent at once.
	typesafeConcurrency = 8
)

func typesafeProvider() Provider {
	setup := "typesafe: API key not set (get one at https://console.typesafe.ai then: ketch config set " + typesafeKey + " <key>)"
	return Provider{
		ID: "typesafe", Name: "TypeSafe", Model: typesafeModel, Rank: 10, Setup: setup,
		Cutoffs:  DefaultCutoffs,
		Settings: []config.Setting{SecretKey(typesafeKey)},
		Usable:   func(c *config.Config) bool { return c.String(typesafeKey) != "" },
		New: func(c *config.Config) (Decider, error) {
			return NewTypeSafe(c.String(typesafeKey), typesafeEndpoint), nil
		},
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return probeModels(ctx, client, typesafeModels, c.String(typesafeKey), typesafeKey)
		},
	}
}

// TypeSafe is the System One client.
type TypeSafe struct {
	post                    poster
	model                   string
	requestTokens, perQuest int
	concurrency             int
}

// NewTypeSafe is a System One client with key, posting to endpoint (the
// provider's own when empty).
func NewTypeSafe(key, endpoint string) *TypeSafe {
	if endpoint == "" {
		endpoint = typesafeEndpoint
	}
	return &TypeSafe{
		post: poster{client: httpx.Default(), endpoint: endpoint, key: key, name: "typesafe", timeout: attemptTimeout,
			attempts: attempts, backoff: firstBackoff, retry: statusIn(http.StatusTooManyRequests, 529)},
		model: typesafeModel, requestTokens: typesafeRequestTokens, perQuest: typesafeQuestionTokens, concurrency: typesafeConcurrency,
	}
}

// typesafeResponse is System One's response.
type typesafeResponse struct {
	Answers map[string]struct {
		Type          string             `json:"type"`
		Noul          *float64           `json:"noul"`
		Choice        string             `json:"choice"`
		Probabilities map[string]float64 `json:"probabilities"`
	} `json:"answers"`
	Usage struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
}

// Decide sends req as one request, or as parts of at most the request
// size, each with the whole state, at most typesafeConcurrency at once.
func (t *TypeSafe) Decide(ctx context.Context, req Request) (Response, error) {
	parts, state, err := t.parts(req)
	if err != nil {
		return Response{}, err
	}
	out := Response{Answers: map[string]Answer{}}
	var mu sync.Mutex
	var first error
	sem := make(chan struct{}, t.concurrency)
	var wg sync.WaitGroup
	for _, part := range parts {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			answers, tokens, err := t.send(ctx, state, part)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				first = firstErr(first, err)
				return
			}
			for k, v := range answers {
				out.Answers[k] = v
			}
			out.InputTokens += tokens
			out.Requests++
		})
	}
	wg.Wait()
	if first != nil {
		return Response{}, first
	}
	return out, checkAnswered("typesafe", req.Questions, out.Answers)
}

// tsQuestion is a question with its encoding.
type tsQuestion struct {
	q    Question
	body []byte
}

// parts packs req's questions, in order, into requests of at most
// requestTokens (estimated) with the state; a question that does not fit
// the per-question context with the state is ErrTooLarge.
func (t *TypeSafe) parts(req Request) ([][]tsQuestion, []byte, error) {
	state, err := value(req.Input)
	if err != nil {
		return nil, nil, fmt.Errorf("typesafe: %w", err)
	}
	base := len(state) + len(t.model) + 64
	var parts [][]tsQuestion
	var cur []tsQuestion
	size := base
	for _, q := range req.Questions {
		body, err := typesafeQuestion(q)
		if err != nil {
			return nil, nil, fmt.Errorf("typesafe: %w", err)
		}
		n := len(body) + len(q.Name) + 4
		if estimateTokens(base+n) > t.perQuest {
			return nil, nil, fmt.Errorf("typesafe: %w: question %s with the state is about %d tokens", ErrTooLarge, q.Name, estimateTokens(base+n))
		}
		if len(cur) > 0 && estimateTokens(size+n) > t.requestTokens {
			parts, cur, size = append(parts, cur), nil, base
		}
		cur, size = append(cur, tsQuestion{q, body}), size+n
	}
	if len(cur) > 0 {
		parts = append(parts, cur)
	}
	if len(parts) > maxParts {
		return nil, nil, fmt.Errorf("typesafe: %w: %d requests (at most %d)", ErrTooLarge, len(parts), maxParts)
	}
	return parts, state, nil
}

// typesafeQuestion is q as System One takes it: a noul or a choice, its
// instructions a string or, with an item, the item and the question in
// one object, its criteria the true/false meanings or the options.
func typesafeQuestion(q Question) ([]byte, error) {
	instructions := str(q.Instructions)
	if q.Item != nil {
		item, err := value(q.Item.Value)
		if err != nil {
			return nil, err
		}
		instructions = object(field{q.Item.Name, item}, field{"question", str(q.Instructions)})
	}
	kind := "noul"
	var criteria []field
	if q.Kind == Choice {
		kind = "choice"
		for _, o := range q.Options {
			criteria = append(criteria, field{o.Name, str(o.Description)})
		}
	} else {
		if q.TrueMeans != "" {
			criteria = append(criteria, field{"true", str(q.TrueMeans)})
		}
		if q.FalseMeans != "" {
			criteria = append(criteria, field{"false", str(q.FalseMeans)})
		}
	}
	fields := []field{{"type", str(kind)}, {"instructions", instructions}}
	if len(criteria) > 0 {
		fields = append(fields, field{"criteria", object(criteria...)})
	}
	return object(fields...), nil
}

// send posts one part and reads its answers.
func (t *TypeSafe) send(ctx context.Context, state []byte, part []tsQuestion) (map[string]Answer, int, error) {
	qs := make([]field, len(part))
	for i, q := range part {
		qs[i] = field{q.q.Name, q.body}
	}
	body := object(field{"model", str(t.model)}, field{"state", state}, field{"questions", object(qs...)})
	var resp typesafeResponse
	if err := t.post.post(ctx, body, &resp); err != nil {
		return nil, 0, err
	}
	out := make(map[string]Answer, len(part))
	for _, q := range part {
		a, ok := resp.Answers[q.q.Name]
		switch {
		case !ok:
			return nil, 0, fmt.Errorf("typesafe: %w: %s", ErrIncomplete, q.q.Name)
		case q.q.Kind == YesNo && a.Noul == nil:
			return nil, 0, fmt.Errorf("typesafe: %w: %s has no probability", ErrIncomplete, q.q.Name)
		case q.q.Kind == YesNo:
			out[q.q.Name] = Answer{Probability: *a.Noul}
		default:
			out[q.q.Name] = Answer{Choice: a.Choice, Probabilities: a.Probabilities}
		}
	}
	return out, resp.Usage.InputTokens, nil
}

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// probeModels checks a key against a provider's model list: no request is
// billed.
func probeModels(ctx context.Context, client *http.Client, endpoint, key, setting string) (health.Status, string) {
	if key == "" {
		return health.StatusNoKey, "API key not set (ketch config set " + setting + " <key>)"
	}
	resp, err := health.Get(ctx, client, endpoint, map[string]string{"Authorization": "Bearer " + key, "Accept": "application/json"})
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)
	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return health.StatusMisconfigured, "API key rejected (ketch config set " + setting + " <key>)"
	case http.StatusTooManyRequests:
		return health.StatusOK, "reachable, key accepted (rate limited)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}
