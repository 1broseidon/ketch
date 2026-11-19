package decide

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// poster sends one JSON request to a provider's endpoint: each attempt
// within timeout, a rate-limit or overload status (retry) and a failed
// connection tried again after backoff, then 2x backoff; a timeout is
// final. Errors name the provider and the status, never the key.
type poster struct {
	client   *http.Client
	endpoint string
	key      string
	name     string
	timeout  time.Duration
	attempts int
	backoff  time.Duration
	retry    func(status int) bool
}

const (
	// attemptTimeout bounds one HTTP attempt; a timeout is not retried.
	attemptTimeout = 30 * time.Second
	// attempts is how many times a request is sent at most.
	attempts = 3
	// firstBackoff is the wait before the second attempt; it doubles.
	firstBackoff = time.Second
	// maxRetryAfter caps how long a Retry-After header can make a retry wait.
	maxRetryAfter = 10 * time.Second
	// maxResponseBytes bounds a response read.
	maxResponseBytes = 16 << 20
)

// marshal encodes v as JSON with no HTML escaping: the text the model
// reads is the text the page has.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// post sends body and decodes a 200 response into out.
func (p *poster) post(ctx context.Context, body []byte, out any) error {
	if p.key == "" {
		return fmt.Errorf("%s: API key not set", p.name)
	}
	wait := p.backoff
	var last error
	for attempt := 1; attempt <= p.attempts; attempt++ {
		status, data, after, err := p.once(ctx, body)
		switch {
		case err == nil && status == http.StatusOK:
			if jerr := json.Unmarshal(data, out); jerr != nil {
				return fmt.Errorf("%s: malformed response: %v", p.name, jerr)
			}
			return nil
		case ctx.Err() != nil:
			return fmt.Errorf("%s: %w", p.name, ctx.Err())
		case errors.Is(err, context.DeadlineExceeded):
			return fmt.Errorf("%s: no answer within %s", p.name, p.timeout)
		case err != nil:
			last = fmt.Errorf("%s: request failed: %s", p.name, p.scrub(err.Error()))
		case !p.retry(status):
			return fmt.Errorf("%s: HTTP %d%s", p.name, status, p.detail(data))
		default:
			last = fmt.Errorf("%s: HTTP %d%s", p.name, status, p.detail(data))
		}
		if attempt == p.attempts {
			break
		}
		if err := sleep(ctx, max(wait, min(after, maxRetryAfter))); err != nil {
			return fmt.Errorf("%s: %w", p.name, err)
		}
		wait *= 2
	}
	return last
}

// once sends one attempt: its status, body and Retry-After.
func (p *poster) once(ctx context.Context, body []byte) (int, []byte, time.Duration, error) {
	actx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		if actx.Err() != nil && ctx.Err() == nil {
			return 0, nil, 0, context.DeadlineExceeded
		}
		return 0, nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if actx.Err() != nil && ctx.Err() == nil {
			return 0, nil, 0, context.DeadlineExceeded
		}
		return 0, nil, 0, err
	}
	return resp.StatusCode, data, retryAfter(resp.Header.Get("Retry-After")), nil
}

// detail is an error body as a short suffix, the key never in it.
func (p *poster) detail(data []byte) string {
	d := strings.Join(strings.Fields(string(data)), " ")
	if d == "" {
		return ""
	}
	if len(d) > 200 {
		d = d[:200] + "…"
	}
	return ": " + p.scrub(d)
}

// scrub removes the key from text a server or transport echoed.
func (p *poster) scrub(s string) string {
	if p.key == "" {
		return s
	}
	return strings.ReplaceAll(s, p.key, "[redacted]")
}

// retryAfter reads a Retry-After header given in seconds; 0 otherwise.
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// statusIn returns a retry predicate for the given statuses.
func statusIn(statuses ...int) func(int) bool {
	return func(s int) bool {
		for _, x := range statuses {
			if s == x {
				return true
			}
		}
		return false
	}
}

// estimateTokens is a conservative token estimate for packing requests
// under a provider's limits: three bytes a token (JSON text runs nearer
// three and a half).
func estimateTokens(n int) int { return (n + 2) / 3 }

// Estimate is req's input tokens as the TypeSafe client measures them to
// pack its requests (estimateTokens over the encoded state and each
// encoded question): the shared input's, and each question's, in order.
// It is one measure for every provider, for a caller to budget what it
// sends; the shared input is counted once, though a request split into
// parts sends it with each. 0 for a value that does not encode.
func Estimate(req Request) (input int, questions []int) {
	questions = make([]int, len(req.Questions))
	state, err := value(req.Input)
	if err != nil {
		return 0, questions
	}
	for i, q := range req.Questions {
		if body, err := typesafeQuestion(q); err == nil {
			questions[i] = estimateTokens(len(body) + len(q.Name) + 4)
		}
	}
	return estimateTokens(len(state) + len(typesafeModel) + 64), questions
}

// checkAnswered reports the first question of qs without an answer in
// answers, or answered in the wrong shape.
func checkAnswered(name string, qs []Question, answers map[string]Answer) error {
	for _, q := range qs {
		a, ok := answers[q.Name]
		if !ok {
			return fmt.Errorf("%s: %w: %s", name, ErrIncomplete, q.Name)
		}
		if q.Kind == Choice && !hasOption(q, a.Choice) {
			return fmt.Errorf("%s: %w: %s has no valid choice", name, ErrIncomplete, q.Name)
		}
	}
	return nil
}

func hasOption(q Question, name string) bool {
	for _, o := range q.Options {
		if o.Name == name {
			return true
		}
	}
	return false
}
