package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// The You.com MCP server hosts the you-search tool. Without a key the free
// profile (https://api.you.com/mcp?profile=free) is keyless like keenable and
// hosted Firecrawl: always usable, rate-limited. An API key lifts the limits
// and is sent as a Bearer header — never a query param — so transport errors
// cannot leak the credential through a URL.
const (
	youcomAuthenticatedEndpoint = "https://api.you.com/mcp"
	youcomFreeProfileParam      = "profile=free"
)

// Youcom searches the web through You.com's hosted MCP server.
type Youcom struct {
	keys   keyPool
	client *http.Client
}

// NewYoucom creates a new Youcom search backend; apiKey may be empty for the
// keyless free profile.
func NewYoucom(apiKey string) *Youcom {
	return newYoucomWithKeys([]string{apiKey})
}

func newYoucomWithKeys(keys []string) *Youcom {
	return &Youcom{keys: newKeyPool(keys), client: httpx.Default()}
}

// YoucomEndpoint returns the MCP endpoint for the given key: the authenticated
// server with a key, the keyless free profile without.
func YoucomEndpoint(apiKey string) string {
	if strings.TrimSpace(apiKey) != "" {
		return youcomAuthenticatedEndpoint
	}
	return youcomAuthenticatedEndpoint + "?" + youcomFreeProfileParam
}

type youcomRPCResponse struct {
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

type youcomSearchPayload struct {
	Results struct {
		Web []struct {
			URL         string   `json:"url"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Snippets    []string `json:"snippets"`
		} `json:"web"`
	} `json:"results"`
}

// Search calls You.com's you-search tool and maps its ranked web results into
// ketch's provider-neutral result shape. Description carries the page summary
// (first snippet as fallback); Content joins the keyword-centered snippets.
func (y *Youcom) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "you-search",
			"arguments": map[string]any{
				"query":      query,
				"count":      limit,
				"extraction": "none",
			},
		},
	})
	if err != nil {
		return nil, err
	}

	resp, key, err := y.response(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read youcom response: %w", err)
	}
	payload, err := extractYoucomSSEPayload(raw)
	if err != nil {
		return nil, err
	}
	return decodeYoucomResults(payload, limit, y.keys.keyLabel(key))
}

func (y *Youcom) response(ctx context.Context, body []byte) (*http.Response, string, error) {
	key := y.keys.pick()
	resp, err := y.request(ctx, body, key)
	if err != nil {
		return nil, "", err
	}
	if youcomRetryable(resp.StatusCode) && y.keys.size() > 1 {
		closeSearchResponse(resp)
		key = y.keys.pickDifferent(key)
		resp, err = y.request(ctx, body, key)
		if err != nil {
			return nil, "", err
		}
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, "", youcomStatusError(resp, y.keys.keyLabel(key))
	}
	return resp, key, nil
}

func decodeYoucomResults(payload string, limit int, keyLabel string) ([]Result, error) {
	var rpc youcomRPCResponse
	if err := json.Unmarshal([]byte(payload), &rpc); err != nil {
		return nil, fmt.Errorf("failed to decode youcom response: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("youcom JSON-RPC error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	if err := youcomToolError(rpc, keyLabel); err != nil {
		return nil, err
	}
	return collectYoucomResults(rpc, limit)
}

// The hosted MCP server reports a rejected key as a tool-level error with
// HTTP 200, so surface it with the same guidance as a 401.
func youcomToolError(rpc youcomRPCResponse, keyLabel string) error {
	if !rpc.Result.IsError {
		return nil
	}
	detail := youcomErrorDetail(rpc.Result.Content)
	if strings.Contains(detail, "401") {
		return fmt.Errorf("youcom: API key rejected (%s; get one at https://you.com/platform/api-keys then: ketch config set youcom_api_key <key>)", keyLabel)
	}
	if detail != "" {
		return fmt.Errorf("youcom search tool returned an error: %s", detail)
	}
	return errors.New("youcom search tool returned an error")
}

func collectYoucomResults(rpc youcomRPCResponse, limit int) ([]Result, error) {
	results := make([]Result, 0, limit)
	foundText := false
	for _, content := range rpc.Result.Content {
		if content.Type != "text" || strings.TrimSpace(content.Text) == "" {
			continue
		}
		foundText = true
		var err error
		results, err = appendYoucomResults(results, content.Text, limit)
		if err != nil {
			return nil, err
		}
		if len(results) >= limit {
			break
		}
	}
	if !foundText {
		return nil, fmt.Errorf("youcom response contained no text results")
	}
	return results, nil
}

// youcomErrorDetail extracts a bounded message from the tool's error content
// blocks for surfacing in errors.
func youcomErrorDetail(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	for _, block := range content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			return block.Text
		}
	}
	return ""
}

// appendYoucomResults decodes one MCP text block and maps its valid results.
func appendYoucomResults(results []Result, text string, limit int) ([]Result, error) {
	var payload youcomSearchPayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, fmt.Errorf("failed to decode youcom search results: %w", err)
	}
	for _, raw := range payload.Results.Web {
		if len(results) >= limit {
			break
		}
		if strings.TrimSpace(raw.URL) == "" {
			continue
		}
		description := raw.Description
		if description == "" && len(raw.Snippets) > 0 {
			description = raw.Snippets[0]
		}
		content := strings.Join(raw.Snippets, "\n")
		if content == "" {
			content = description
		}
		results = append(results, Result{
			Title:       raw.Title,
			URL:         raw.URL,
			Description: description,
			Content:     content,
		})
	}
	return results, nil
}

func (y *Youcom) request(ctx context.Context, body []byte, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, YoucomEndpoint(key), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("youcom request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := y.client.Do(req)
	if err != nil {
		return nil, safeYoucomRequestError(err)
	}
	return resp, nil
}

// youcomRetryable reports whether a status should trigger a retry with a
// different key from the pool.
func youcomRetryable(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusTooManyRequests
}

// youcomStatusError maps known You.com statuses to actionable errors; unknown
// statuses include a bounded body snippet for diagnosis.
func youcomStatusError(resp *http.Response, keyLabel string) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("youcom: invalid API key (%s; get one at https://you.com/platform/api-keys then: ketch config set youcom_api_key <key>)", keyLabel)
	case http.StatusTooManyRequests:
		return fmt.Errorf("youcom: rate limited (%s; set a key to lift the cap: ketch config set youcom_api_key <key>)", keyLabel)
	case http.StatusPaymentRequired:
		return fmt.Errorf("youcom: credits exhausted (%s; see your you.com plan)", keyLabel)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if detail := strings.TrimSpace(string(body)); detail != "" {
			return fmt.Errorf("youcom returned status %d: %s", resp.StatusCode, detail)
		}
		return fmt.Errorf("youcom returned status %d", resp.StatusCode)
	}
}

// safeYoucomRequestError deliberately drops the original transport error, as
// exa does: net/http includes req.URL in Client.Do errors, and credential
// lifecycles must never surface a keyed URL in error text. Cancellation and
// timeout sentinels remain classifiable.
func safeYoucomRequestError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("youcom: request failed: %w", context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("youcom: request failed: %w", context.DeadlineExceeded)
	}
	return errors.New("youcom: request failed: transport error")
}

// extractYoucomSSEPayload scans raw SSE bytes and returns the last non-empty
// data: payload, as exa does for the same hosted-MCP transport.
func extractYoucomSSEPayload(raw []byte) (string, error) {
	var payload string
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			if v := strings.TrimSpace(strings.TrimPrefix(line, "data:")); v != "" {
				payload = v
			}
		}
	}
	if payload == "" {
		return "", errors.New("youcom response contained no SSE data payload")
	}
	return payload, nil
}

// ProbeYoucom checks the provider using a caller-supplied client and endpoint.
// The probe is the same tools/list handshake health.ProbeMCP performs for
// other hosted MCP providers; a keyed 401/403 is misconfigured, a keyless 429
// is reachable-but-throttled (the free profile's steady state under fan-out).
func ProbeYoucom(ctx context.Context, client *http.Client, endpoint, apiKey string) (health.Status, string) {
	key := strings.TrimSpace(apiKey)
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		if key != "" {
			return health.StatusMisconfigured, "API key rejected (ketch config set youcom_api_key <key>)"
		}
		return health.StatusUnreachable, fmt.Sprintf("youcom returned status %d", resp.StatusCode)
	case http.StatusTooManyRequests:
		return health.StatusOK, "reachable (rate limited; set a key to lift the cap)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("youcom returned status %d", resp.StatusCode)
	}
}

func youcomProvider() Provider {
	keys := config.KeyPool("youcom_api_key", "youcom_api_keys", 15, 16, 9, 15)
	return Provider{
		Settings: []config.Setting{keys},
		ID:       "youcom",
		Name:     "You.com",
		Usable:   func(*config.Config) bool { return true },
		New:      func(c *config.Config) (Searcher, error) { return newYoucomWithKeys(keys.Keys(c)), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeKeyPool(keys.Keys(c), func(key string) (health.Status, string) {
				return ProbeYoucom(ctx, client, YoucomEndpoint(key), key)
			})
		},
	}
}
