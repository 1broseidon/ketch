package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

type EXA struct {
	keys   keyPool
	client *http.Client
}

func NewEXA(apiKey *string) *EXA {
	if apiKey == nil {
		return newEXAWithKeys(nil)
	}
	return newEXAWithKeys([]string{*apiKey})
}

func newEXAWithKeys(keys []string) *EXA {
	return &EXA{keys: newKeyPool(keys), client: httpx.Default()}
}

func (e *EXA) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	// Step 1 : Build request body:
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "web_search_exa",
			"arguments": map[string]any{
				"query":                query,
				"numResults":           limit,
				"type":                 "auto",
				"livecrawl":            "fallback",
				"contextMaxCharacters": 3000,
			},
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	// Step 2 : Send this request to EXA
	resp, err := e.response(ctx, encoded)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Step 3 : Parse the SSE-like response
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read exa response: %w", err)
	}
	payload, err := extractSSEPayload(raw)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode exa response: %w", err)
	}

	// Step 4 : Populate response and return
	results := make([]Result, 0, limit)
	for _, content := range parsed.Result.Content {
		if content.Type != "text" || len(results) >= limit {
			continue
		}
		remaining := limit - len(results)
		results = append(results, parseContent(content.Text, remaining)...)
	}

	return results, nil
}

func (e *EXA) response(ctx context.Context, body []byte) (*http.Response, error) {
	key := e.keys.pick()
	resp, err := e.request(ctx, body, key)
	if err != nil {
		return nil, err
	}
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests) && e.keys.size() > 1 {
		closeSearchResponse(resp)
		key = e.keys.pickDifferent(key)
		resp, err = e.request(ctx, body, key)
		if err != nil {
			return nil, err
		}
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusUnauthorized:
		closeSearchResponse(resp)
		return nil, fmt.Errorf("exa: invalid API key (%s; set via: ketch config set exa_api_key <key>)", e.keys.keyLabel(key))
	case http.StatusTooManyRequests:
		closeSearchResponse(resp)
		return nil, fmt.Errorf("exa: rate limited (%s)", e.keys.keyLabel(key))
	default:
		status := resp.StatusCode
		closeSearchResponse(resp)
		return nil, fmt.Errorf("exa returned status %d", status)
	}
}

func (e *EXA) request(ctx context.Context, body []byte, key string) (*http.Response, error) {
	endpoint := "https://mcp.exa.ai/mcp"
	if key != "" {
		v := url.Values{}
		v.Set("exaApiKey", key)
		endpoint += "?" + v.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, safeEXARequestError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, safeEXARequestError(err)
	}
	return resp, nil
}

// safeEXARequestError deliberately drops the original transport error because
// net/http includes req.URL in errors from Client.Do, and Exa credentials live
// in that URL's query string. Cancellation sentinels remain discoverable for
// CLI/MCP classification, but no URL or lower-level error text is retained.
func safeEXARequestError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("exa: request failed: %w", context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("exa: request failed: %w", context.DeadlineExceeded)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errors.New("exa: request failed: timed out")
	}
	return errors.New("exa: request failed: transport error")
}

// extractSSEPayload scans raw SSE bytes and returns the last non-empty data:
// payload. Exa sends a single frame with the full result, but earlier lines
// may carry keep-alive or event-type markers with no JSON content.
func extractSSEPayload(raw []byte) (string, error) {
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
		return "", fmt.Errorf("exa response contained no data payload")
	}
	return payload, nil
}

// knownEXAPrefix returns true for metadata lines that Exa emits as labels
// rather than content (e.g. "Title:", "URL:", "Highlights:", "Published date:").
func knownEXAPrefix(line string) bool {
	for _, prefix := range []string{"Title:", "URL:", "Highlights:", "Published date:", "Author:", "Score:"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// parseContent converts Exa's text-formatted MCP search output into structured
// results. Exa returns result blocks separated by "---", with metadata lines
// such as "Title:" and "URL:" followed by highlight text. This parser extracts
// the title, URL, highlight text as content, and the first plain highlight
// line as the description.
func parseContent(rawContent string, limit int) []Result {
	results := make([]Result, 0, limit)
	for block := range strings.SplitSeq(rawContent, "\n---\n") {
		if len(results) >= limit {
			break
		}
		var result Result
		var contentLines []string
		for line := range strings.SplitSeq(block, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "Title:"):
				result.Title = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
			case strings.HasPrefix(line, "URL:"):
				result.URL = strings.TrimSpace(strings.TrimPrefix(line, "URL:"))
			case line != "" && !knownEXAPrefix(line):
				contentLines = append(contentLines, line)
				if result.Description == "" {
					result.Description = line
				}
			}
		}
		result.Content = strings.Join(contentLines, "\n")
		if result.Title != "" && result.URL != "" {
			results = append(results, result)
		}
	}
	return results
}

const exaMCPEndpoint = "https://mcp.exa.ai/mcp"

// ExaEndpoint checks the provider using a caller-supplied client and endpoint.
func ExaEndpoint(apiKey string) string {
	endpoint := exaMCPEndpoint
	if strings.TrimSpace(apiKey) != "" {
		v := url.Values{}
		v.Set("exaApiKey", strings.TrimSpace(apiKey))
		endpoint += "?" + v.Encode()
	}
	return endpoint
}

// ProbeExa checks the provider using a caller-supplied client and endpoint.
func ProbeExa(ctx context.Context, client *http.Client, endpoint string, keyed bool) (health.Status, string) {
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return health.StatusUnreachable, exaProbeErrDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return health.StatusUnreachable, exaProbeErrDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		if keyed {
			return health.StatusMisconfigured, "API key rejected (ketch config set exa_api_key <key>)"
		}
		return health.StatusUnreachable, fmt.Sprintf("exa returned status %d", resp.StatusCode)
	case http.StatusTooManyRequests:
		return health.StatusOK, "reachable, key accepted (rate limited)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("exa returned status %d", resp.StatusCode)
	}
}

// exaProbeErrDetail checks the provider using a caller-supplied client and endpoint.
func exaProbeErrDetail(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "request failed: cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "request failed: timed out"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "request failed: timed out"
	}
	return "request failed: transport error"
}

func exaProvider() Provider {
	return Provider{
		Settings: []config.Setting{config.KeyPool("exa_api_key", "exa_api_keys", 4, 5, 3, 4)},
		ID:       "exa",
		Name:     "Exa",
		Usable:   func(*config.Config) bool { return true },
		New:      func(c *config.Config) (Searcher, error) { return newEXAWithKeys(c.ExaKeys()), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeKeyPool(c.ExaKeys(), func(key string) (health.Status, string) { return ProbeExa(ctx, client, ExaEndpoint(key), key != "") })
		},
	}
}
