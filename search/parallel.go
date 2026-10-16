package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

const (
	parallelEndpoint            = "https://search.parallel.ai/mcp"
	parallelDescriptionMaxRunes = 300
)

// Parallel searches through Parallel's hosted Search MCP. It is keyless by
// default (rate-limited); an optional API key, sent as a Bearer token, lifts
// the hosted limits.
type Parallel struct {
	keys     keyPool
	redact   *httpx.Redactor // API key values, kept out of errors
	client   *http.Client
	endpoint string
}

// NewParallel creates a keyless Parallel Search backend.
func NewParallel() *Parallel {
	return newParallelWithKeys(nil)
}

func newParallelWithKeys(keys []string) *Parallel {
	pool := newKeyPool(keys)
	return &Parallel{keys: pool, redact: httpx.NewValueRedactor(pool.keys...), client: httpx.Default(), endpoint: parallelEndpoint}
}

type parallelRPCResponse struct {
	Result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type parallelSearchPayload struct {
	Results []struct {
		URL      string   `json:"url"`
		Title    string   `json:"title"`
		Excerpts []string `json:"excerpts"`
	} `json:"results"`
}

// Search calls Parallel's hosted Search MCP and maps its ranked results into
// Ketch's provider-neutral result shape. Parallel does not expose a result
// limit argument, so the requested limit is applied locally.
//
// Every error is passed through the key redactor: the service, or a hop it
// redirects to, can echo a credential into a status body, a JSON-RPC message,
// or a Location that net/http quotes. Keyless backends have no redactor and
// return errors unchanged.
func (p *Parallel) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	results, err := p.search(ctx, query, limit)
	if err != nil {
		return nil, p.redact.Error(err)
	}
	return results, nil
}

func (p *Parallel) search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "web_search",
			"arguments": map[string]any{
				"objective":      query,
				"search_queries": []string{query},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	resp, key, err := p.post(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.statusError(resp, key)
	}

	var rpc parallelRPCResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rpc); err != nil {
		return nil, fmt.Errorf("failed to decode parallel response: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("parallel JSON-RPC error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	if rpc.Result.IsError {
		return nil, fmt.Errorf("parallel search tool returned an error")
	}

	results := make([]Result, 0, limit)
	foundText := false
	for _, content := range rpc.Result.Content {
		if content.Type != "text" || strings.TrimSpace(content.Text) == "" {
			continue
		}
		foundText = true
		results, err = appendParallelResults(results, content.Text, limit)
		if err != nil {
			return nil, err
		}
		if len(results) >= limit {
			break
		}
	}
	if !foundText {
		return nil, fmt.Errorf("parallel response contained no text results")
	}
	return results, nil
}

// appendParallelResults decodes one MCP text block and maps its valid results.
// Parallel can return full excerpts, so Description is bounded for listings
// while Content retains the complete normalized text.
func appendParallelResults(results []Result, text string, limit int) ([]Result, error) {
	var payload parallelSearchPayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, fmt.Errorf("failed to decode parallel search results: %w", err)
	}
	for _, raw := range payload.Results {
		if len(results) >= limit {
			break
		}
		if strings.TrimSpace(raw.Title) == "" || strings.TrimSpace(raw.URL) == "" {
			continue
		}
		excerpts := nonEmptyStrings(raw.Excerpts)
		description := ""
		if len(excerpts) > 0 {
			description = boundedParallelDescription(excerpts[0])
		}
		results = append(results, Result{
			Title:       collapseParallelWhitespace(raw.Title),
			URL:         raw.URL,
			Description: description,
			Content:     strings.Join(excerpts, "\n"),
		})
	}
	return results, nil
}

// collapseParallelWhitespace flattens a value onto a single line, collapsing
// every run of whitespace to one space. Parallel returns extracted page text,
// so titles and excerpts can carry newlines and indentation. Results are
// printed one per line — `--minimal` emits "url\ttitle\tdescription\n" — so an
// embedded newline or tab in either field would break that row contract.
func collapseParallelWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func boundedParallelDescription(excerpt string) string {
	excerpt = collapseParallelWhitespace(excerpt)
	runes := []rune(excerpt)
	if len(runes) <= parallelDescriptionMaxRunes {
		return excerpt
	}
	return strings.TrimSpace(string(runes[:parallelDescriptionMaxRunes-1])) + "…"
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

// post sends one request with a key from the pool, retrying once with a
// different key when the first is rejected or rate limited. Keyless backends
// send a single unauthenticated request.
func (p *Parallel) post(ctx context.Context, body []byte) (*http.Response, string, error) {
	key := p.keys.pick()
	resp, err := p.request(ctx, body, key)
	if err != nil {
		return nil, key, err
	}
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests) && p.keys.size() > 1 {
		closeSearchResponse(resp)
		key = p.keys.pickDifferent(key)
		resp, err = p.request(ctx, body, key)
	}
	return resp, key, err
}

func (p *Parallel) request(ctx context.Context, body []byte, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("parallel request failed: %w", err)
	}
	return resp, nil
}

// statusError maps credential failures on a keyed request to actionable
// errors that name the key by position, never by value. Keyless requests keep
// the generic status error.
func (p *Parallel) statusError(resp *http.Response, key string) error {
	if key != "" {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("parallel: invalid API key (%s; set via: ketch config set parallel_api_key <key>)", p.keys.keyLabel(key))
		case http.StatusPaymentRequired:
			return fmt.Errorf("parallel: payment required (%s; check your Parallel plan)", p.keys.keyLabel(key))
		case http.StatusTooManyRequests:
			return fmt.Errorf("parallel: rate limited (%s)", p.keys.keyLabel(key))
		}
	}
	return parallelStatusError(resp, p.redact)
}

// parallelStatusError quotes up to 4096 bytes of the response body. Key
// values are redacted before the body is cut, so a key at the cut is not left
// half-visible; a nil redactor quotes the body as it is.
func parallelStatusError(resp *http.Response, redact *httpx.Redactor) error {
	if detail := strings.TrimSpace(redact.Excerpt(resp.Body, 4096)); detail != "" {
		return fmt.Errorf("parallel returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("parallel returned status %d", resp.StatusCode)
}

// ProbeParallel checks the provider using a caller-supplied client and
// endpoint. Without a key it is the generic MCP reachability check; with one
// it sends the key, reports a rejected key as misconfigured, and redacts the
// key from the detail.
func ProbeParallel(ctx context.Context, client *http.Client, endpoint, apiKey string) (health.Status, string) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return health.ProbeMCP(ctx, client, endpoint, "parallel")
	}
	status, detail := probeParallelKeyed(ctx, client, endpoint, key)
	return status, httpx.NewValueRedactor(key).String(detail)
}

func probeParallelKeyed(ctx context.Context, client *http.Client, endpoint, key string) (health.Status, string) {
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return health.StatusMisconfigured, "API key rejected (ketch config set parallel_api_key <key>)"
	case http.StatusPaymentRequired:
		return health.StatusMisconfigured, "payment required for this API key (check your Parallel plan)"
	case http.StatusTooManyRequests:
		return health.StatusOK, "reachable, key accepted (rate limited)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("parallel returned status %d", resp.StatusCode)
	}
}

func parallelProvider() Provider {
	keys := config.KeyPool("parallel_api_key", "parallel_api_keys")
	return Provider{
		Settings: []config.Setting{keys},
		AutoRank: 70,
		ID:       "parallel",
		Name:     "Parallel",
		Usable:   func(*config.Config) bool { return true },
		New:      func(c *config.Config) (Searcher, error) { return newParallelWithKeys(keys.Keys(c)), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeKeyPool(keys.Keys(c), func(key string) (health.Status, string) {
				return ProbeParallel(ctx, client, parallelEndpoint, key)
			})
		},
	}
}
