package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1broseidon/ketch/httpx"
)

// youcomEndpoint is the You.com Web Search API. Auth is X-API-Key
// header-only — never a query param or body field — so transport errors
// cannot leak the key via a URL (same discipline as Tavily's Bearer).
const youcomEndpoint = "https://ydc-index.io/v1/search"

// Youcom searches the web via the You.com Web Search API.
type Youcom struct {
	keys   keyPool
	client *http.Client
}

// NewYoucom creates a new Youcom search backend.
func NewYoucom(apiKey string) *Youcom {
	return newYoucomWithKeys([]string{apiKey})
}

func newYoucomWithKeys(keys []string) *Youcom {
	return &Youcom{keys: newKeyPool(keys), client: httpx.Default()}
}

type youcomRequest struct {
	Query string `json:"query"`
	Count int    `json:"count"`
}

type youcomResponse struct {
	Results youcomResults `json:"results"`
}

type youcomResults struct {
	Web []youcomResult `json:"web"`
}

type youcomResult struct {
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Snippets    []string `json:"snippets"`
}

// Search queries You.com and returns up to limit results. Description is
// filled from You.com's page summary, with the first keyword-centered
// snippet as a fallback; Content carries the joined snippets (richer than
// a SERP snippet), with Description as the fallback.
func (y *Youcom) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	body, err := json.Marshal(youcomRequest{
		Query: query,
		Count: limit,
	})
	if err != nil {
		return nil, err
	}

	key := y.keys.pick()
	resp, err := y.request(ctx, body, key)
	if err != nil {
		return nil, err
	}
	if youcomRetryableStatus(resp.StatusCode) && y.keys.size() > 1 {
		closeSearchResponse(resp)
		key = y.keys.pickDifferent(key)
		resp, err = y.request(ctx, body, key)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, youcomResponseError(resp, y.keys.keyLabel(key))
	}

	var yr youcomResponse
	if err := json.NewDecoder(resp.Body).Decode(&yr); err != nil {
		return nil, fmt.Errorf("failed to decode youcom response: %w", err)
	}
	return mapYoucomResults(yr.Results.Web, limit), nil
}

// youcomResponseError maps known You.com statuses to actionable errors;
// unknown statuses include a bounded body snippet for diagnosis.
func youcomResponseError(resp *http.Response, keyLabel string) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("youcom: invalid API key (%s; get one at https://you.com/platform/api-keys then: ketch config set youcom_api_key <key>)", keyLabel)
	case http.StatusTooManyRequests:
		return fmt.Errorf("youcom: rate limited (%s)", keyLabel)
	case http.StatusPaymentRequired:
		return fmt.Errorf("youcom: credits exhausted (%s; see your you.com plan)", keyLabel)
	default:
		return youcomStatusError(resp)
	}
}

func mapYoucomResults(raw []youcomResult, limit int) []Result {
	results := make([]Result, 0, limit)
	for _, r := range raw {
		if len(results) >= limit {
			break
		}
		if r.URL == "" {
			continue
		}
		description := r.Description
		if description == "" && len(r.Snippets) > 0 {
			description = r.Snippets[0]
		}
		content := strings.Join(r.Snippets, "\n")
		if content == "" {
			content = description
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: description,
			Content:     content,
		})
	}
	return results
}

func (y *Youcom) request(ctx context.Context, body []byte, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, youcomEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := y.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("youcom request failed: %w", err)
	}
	return resp, nil
}

func youcomRetryableStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusTooManyRequests
}

func youcomStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if detail := strings.TrimSpace(string(body)); detail != "" {
		return fmt.Errorf("youcom returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("youcom returned status %d", resp.StatusCode)
}
